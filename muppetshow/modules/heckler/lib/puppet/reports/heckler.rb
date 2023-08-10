require "puppet"
require "fileutils"
require "puppet/util"

Puppet::Reports.register_report(:heckler) do
  desc "The Heckler report is identical to the standard puppet yaml report,
        except resources that have neither events nor logs associated with them are
        removed, i.e. it only includes resources which are changing. Also, this report
        is serilized as json so that it is compatible with the Go structs generated
        from the protobuf definitions for Heckler."

  Regex_resource_property_tail = %r{/[a-z][a-z0-9_]*$}
  Regex_resource_tail = %r{[^\/]+\[[^\[\]]+\]$}
  Regex_resource = %r{^/Stage}

  def resource_log_map(report)
    log_map = {}

    report["logs"].each do |log|
      if log["source"] !~ Regex_resource
        next
      end
      log_source = log["source"].sub(Regex_resource_property_tail, "")
      log_source = log_source[Regex_resource_tail]
      if !log_map.has_key?(log_source)
        log_map[log_source] = true
      end
    end

    return log_map
  end

  # Stolen from upstream puppet 6.9, as puppet's 4.5 version does not turn all
  # parts of the object into hashes
  def heckler_to_data_hash
    hash = {
      "host" => @host,
      "time" => @time.iso8601(9),
      # HACK: Turn into a string, necessary for v4.5.2
      "configuration_version" => @configuration_version.to_s,
      # not in the original Puppet report, but useful to strip the source file
      # prefix when using a monorepo for puppet
      "confdir" => Puppet.settings[:confdir],
      "transaction_uuid" => @transaction_uuid,
      "report_format" => @report_format,
      "puppet_version" => @puppet_version,
      "status" => @status,
      "transaction_completed" => @transaction_completed,
      # HACK: For lack of @noop, necessary for v4.5.2
      "noop" => Puppet[:noop],
      "environment" => @environment,
      "logs" => @logs.map { |log| log.to_data_hash },
      "metrics" => Hash[@metrics.map { |key, metric| [key, metric.to_data_hash] }],
      "corrective_change" => @corrective_change,
    }

    # The following is include only when set
    hash["master_used"] = @master_used unless @master_used.nil?
    hash["catalog_uuid"] = @catalog_uuid unless @catalog_uuid.nil?
    hash["code_id"] = @code_id unless @code_id.nil?
    hash["job_id"] = @job_id unless @job_id.nil?
    hash["cached_catalog_status"] = @cached_catalog_status unless @cached_catalog_status.nil?
    hash
  end

  def process
    report = heckler_to_data_hash
    resource_logs = resource_log_map(report)

    report["resource_statuses"] = self.resource_statuses.select do |_, resource|
      if resource.events.length > 0 || resource_logs.has_key?(resource.resource)
        true
      else
        false
      end
    end

    report["resource_statuses"] =
      Hash[
        report["resource_statuses"].map { |key, rs|
          # HACK: coerce previous_value & desired_value into strings, necessary
          # for v4.5.2
          if !rs.nil?
            rs.events.each { |event|
              if event.previous_value
                event.previous_value = event.previous_value.to_s
              end
              if event.desired_value
                event.desired_value = event.desired_value.to_s
              end
            }
          end
          [key, rs.nil? ? nil : rs.to_data_hash]
        }
      ]

    dir = File.join(Puppet[:reportdir], "heckler")

    if !Puppet::FileSystem.exist?(dir)
      FileUtils.mkdir_p(dir)
      FileUtils.chmod_R(0750, dir)
    end

    last_apply_name = "heckler_last_apply.json"
    last_apply_path = File.join(dir, last_apply_name)

    # We expect a git sha as the config version
    if !(report.has_key?("configuration_version") &&
         report["configuration_version"].length > 0)
      Puppet.crit("Unable to write report: invalid configuration_version")
      return
    end

    # For a noop set the last_apply_version so we know what applied version the
    # noop was generated from.
    if report["noop"] == true
      if Puppet::FileSystem.exist?(last_apply_path)
        last_apply_report = JSON.parse(File.read(last_apply_path))
        report["last_apply_version"] = last_apply_report["configuration_version"]
      else
        report["last_apply_version"] = ""
      end
    end

    # If an apply failed the state of the box is unknown, since some
    # resources may have applied fully or partially, consequently mark the
    # configuration version as dirty, if it is not already.
    if report["noop"] == false && report["status"] == "failed"
      if report["configuration_version"] !~ /-dirty$/
        report["configuration_version"] = "#{report["configuration_version"]}-dirty"
      end
    end

    name = "heckler_" + report["configuration_version"] + ".json"
    file = File.join(dir, name)

    begin
      Puppet::Util.replace_file(file, 0644) do |fh|
        fh.print report.to_json
      end
    rescue => detail
      Puppet.log_exception(detail, "Could not write report for #{host} at #{file}: #{detail}")
    end

    if report["noop"] == false
      begin
        Puppet::Util.replace_file(last_apply_path, 0644) do |fh|
          fh.print report.to_json
        end
      rescue => detail
        Puppet.log_exception(detail, "Could not write apply report for #{host} at #{last_apply_path}: #{detail}")
      end
    end

    # Only testing cares about the return value
    file
  end
end
