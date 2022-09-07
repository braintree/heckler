package change_management

const STATUS_FAILURE = "failure"
const STATUS_SUCCESS = "success"

type ErrorType struct {
	Message string `yaml:"message" json:"message,omitempty"`
	Detail  string `yaml:"detail" json:"detail,omitempty"`
}

type CRActionResultType struct {
	ChangeRequestID string `yaml:"change_request_id" json:"change_request_id,omitempty"`
	Message         string `yaml:"message" json:"message,omitempty"`
}
type CRResultType struct {
	ChangeRequestID string `yaml:"change_request_id" json:"change_request_id,omitempty"`
	CRDetails       string `yaml:"cr_details" json:"cr_details,omitempty"`
}
type CMResponsePayLoad struct {
	CRResult       CRResultType       `yaml:"change_request_result" json:"change_request_result,omitempty"`
	CRActionResult CRActionResultType `yaml:"change_request_action_result" json:"change_request_action_result,omitempty"`
	Error          ErrorType          `yaml:"error" json:"error,omitempty"`
	Status         string             `yaml:"status" json:"status,omitempty"`
}

type ChangeManagementAdapterConfig struct {
	OnErrorStop     bool   `yaml:"on_error_stop" json:"on_error_stop"`
	IsMandatory     bool   `yaml:"is_mandatory" json:"is_mandatory"`
	CMConfigPath    string `yaml:"cm_config_path" json:"cm_config_path"`
	PluginAgentPath string `yaml:"plugin_agent_path" json:"plugin_agent_path"`
	CLIAgentPath    string `yaml:"cli_agent_path" json:"cli_agent_path"`
	Verbose         bool   `yaml:"verbose" json:"verbose"`
}
