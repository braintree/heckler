require "puppet"
require "fileutils"
require "puppet/util"

Puppet::Reports.register_report(:heckler) do
  desc "This is identical to the yaml report, except resources that have
        neither events nor logs associated with them are removed, i.e. only
        resources which are changing are kept."

  def found_resource_in_logs(report, resource)
    regex_resource_property_tail = %r{/[a-z][a-z0-9_]*$}
    regex_resource_tail = %r{[^\/]+\[[^\[\]]+\]$}

    report["logs"].each do |log|
      log_source = log["source"].sub(regex_resource_property_tail, "")
      log_source = log_source[regex_resource_tail]
      if log_source == resource
        return true
      end
    end

    false
  end

  def process
    report = to_data_hash
    report["resource_statuses"] = report["resource_statuses"].select do |_, resource|
      if resource["events"] != [] || found_resource_in_logs(report, resource["resource"])
        true
      else
        false
      end
    end

    dir = File.join(Puppet[:reportdir], host)

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

    name = "heckler_" + report["configuration_version"] + ".yaml"
    file = File.join(dir, name)

    begin
      Puppet::Util.replace_file(file, 0640) do |fh|
        fh.print report.to_yaml
      end
    rescue => detail
      Puppet.crit("Unable to write report: invalid configuration_version")
      Puppet.log_exception(detail, "Could not write report for #{host} at #{file}: #{detail}")
    end

    # Only testing cares about the return value
    file
  end
end
