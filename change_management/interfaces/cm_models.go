package cm_interfaces

type ChangeRequestConfig struct {
	Env           string `yaml:"env"`
	Template      string `yaml:"template"`
	AssignedGroup string `yaml:"assignedGroup"`
	AssignedTo    string `yaml:"assignedTo"`
	Brand         string `yaml:"brand"`
}
type SNOWConfig struct {
	Token string `yaml:"token"`
	Url   string `yaml:"url"`
}
type ChangeManagementConfig struct {
	SNOWPluginPath string              `yaml:"snow_plugin_path"`
	SNOWCLIPath    string              `yaml:"snow_cli_path"`
	OnErrorFail    bool                `yaml:"on_error_fail"`
	IsMandatory    bool                `yaml:"is_mandatory"`
	CRConfig       ChangeRequestConfig `yaml:"change_request_config"`
	SNConfig       SNOWConfig          `yaml:"snow_config"`
}
