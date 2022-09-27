package cm_interfaces

type ChangeRequestConfig struct {
	Template           string `yaml:"template"`
	Brand              string `yaml:"brand"`
	SnowAPIID          string `yaml:"snow_api_id"`
	AssignedGroup      string `yaml:"assigned_group"`
	DeploymentVehicle  string `yaml:"deployment_vehicle"`
	DescriptionPrefix  string `yaml:"description_prefix"`
	Site               string `yaml:"site"`
	SiteComponents     string `yaml:"site_components"`
	SiteImpact         string `yaml:"site_impact"`
	ImpactedSite       string `yaml:"impacted_site"`
	ServiceCategory    string `yaml:"service_category"`
	CategoryType       string `yaml:"category_type"`
	CategorySubType    string `yaml:"category_sub_type"`
	DeploymentCategory string `yaml:"deployment_category"`
	CRType             string `yaml:"cr_type"`
	Risk               string `yaml:"risk"`
	Priority           string `yaml:"priority"`
	BackupPlan         string `yaml:"backup_plan"`
	ImplementationPlan string `yaml:"implementation_plan"`
	TestPlan           string `yaml:"test_plan"`
	Justifications     string `yaml:"justifications"`
	CRDuration         int    `yaml:"duration"`
	ATBCustImpact      int    `yaml:"atb_cust_impact"`
	Environment        string `yaml:"environment"`
}
type CMClientConfig struct {
	Token    string `yaml:"token"`
	Url      string `yaml:"url"`
	AuthUser string `yaml:"auth_user"`
	ProxyURL string `yaml:"proxy_url"`
}

func String(cm *CMClientConfig) string {
	return "will NOt print CMClientConfig values"
}

type ChangeManagementAdapterConfig struct {
	OnErrorStop     bool   `yaml:"on_error_stop"`
	IsMandatory     bool   `yaml:"is_mandatory"`
	CMConfigPath    string `yaml:"cm_config_path"`
	PluginAgentPath string `yaml:"plugin_agent_path"`
	CLIAgentPath    string `yaml:"cm_cli_path"`
	Verbose         bool   `yaml:"verbose"`
}

type ChangeManagementConfig struct {
	CRConfig       ChangeRequestConfig `yaml:"change_request_config"`
	CMClientConfig CMClientConfig      `yaml:"cm_client_config"`
}

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
