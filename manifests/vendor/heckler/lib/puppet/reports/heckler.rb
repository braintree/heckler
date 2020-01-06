require "puppet"
require "fileutils"
require "puppet/util"

Puppet::Reports.register_report(:heckler) do
  desc "This is identical to the standard puppet yaml report, except resources
       that have neither events nor logs associated with them are removed, i.e. it
       only includes resources which are changing, also this report is serilized as
       json so that it is compatible with the Go structs generated from the protobuf
       definitions."

  def resource_log_map(report)
    regex_resource_property_tail = %r{/[a-z][a-z0-9_]*$}
    regex_resource_tail = %r{[^\/]+\[[^\[\]]+\]$}
    regex_resource = %r{^/Stage}

    log_map = {}

    report["logs"].each do |log|
      if log["source"] !~ regex_resource
        next
      end
      log_source = log["source"].sub(regex_resource_property_tail, "")
      log_source = log_source[regex_resource_tail]
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
      "configuration_version" => @configuration_version,
      "transaction_uuid" => @transaction_uuid,
      "report_format" => @report_format,
      "puppet_version" => @puppet_version,
      "status" => @status,
      "transaction_completed" => @transaction_completed,
      "noop" => @noop,
      "noop_pending" => @noop_pending,
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

    report["resource_statuses"] = Hash[report["resource_statuses"].map { |key, rs| [key, rs.nil? ? nil : rs.to_data_hash] }]

    dir = File.join(Puppet[:reportdir], 'heckler')

    if !Puppet::FileSystem.exist?(dir)
      FileUtils.mkdir_p(dir)
      FileUtils.chmod_R(0750, dir)
    end

    # We expect a git sha as the config version
    if !(report.has_key?("configuration_version") &&
         report["configuration_version"].class == String &&
         report["configuration_version"].length > 0)
      Puppet.crit("Unable to write report: invalid configuration_version")
      return
    end

    name = "heckler_" + report["configuration_version"] + ".json"
    file = File.join(dir, name)

    begin
      Puppet::Util.replace_file(file, 0640) do |fh|
        fh.print report.to_json
      end
    rescue => detail
      Puppet.log_exception(detail, "Could not write report for #{host} at #{file}: #{detail}")
    end

    # Only testing cares about the return value
    file
  end
end

