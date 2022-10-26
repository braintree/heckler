package change_management

const STATUS_FAILURE = "failure"
const STATUS_SUCCESS = "success"

/* CM CLI provider returns/writes output of interaction in json/yaml format and below types are models
for Error/Result types
*/
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

/*
Config for Change Management Adapter, these configs are part of hecklerd_conf.yaml
*/
type ChangeManagementAdapterConfig struct {
	OnErrorStop  bool   `default:"false" yaml:"on_error_stop" json:"on_error_stop"`
	IsMandatory  bool   `default:"false" yaml:"is_mandatory" json:"is_mandatory"`
	CMConfigPath string `yaml:"cm_config_path" json:"cm_config_path"`
	CLIAgentPath string `yaml:"cli_agent_path" json:"cli_agent_path"`
	Verbose      bool   `default:"true" yaml:"verbose" json:"verbose"`
}
