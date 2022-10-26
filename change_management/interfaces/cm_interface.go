package cm_interfaces

/*
This is interface with set of functions of a ChangeRequest
ChangeManagement implementation class (cli/structs/plugin) should have these methods.

*/
type ChangeManagementInterface interface {
	/* Takes 2 inputs env_prefix from hecklerd_conf.yaml and tag/release value
	and findouts any existing active ChangeRequest is available to resuse it */
	SearchChangeRequest(env_prefix, tag string) (string, error)
	/* Takes 2 inputs env_prefix from hecklerd_conf.yaml and tag/release value
	and creates a new ChangeRequest  */
	CreateChangeRequest(env_prefix, tag string) (string, error)
	/* Takes changeRequestID as input and returns ChangeRequest details(json format)  */
	GetChangeRequestDetails(changeRequestID string) (string, error)
	/* Takes changeRequestID and a comment as input and update the change request with given comments ,returns updated/or not  */
	CommentChangeRequest(changeRequestID, comments string) (bool, error)
	/* Takes changeRequestID and a checkin comment as input and check ins the change request with given comments ,returns is_checkedIn/or not  */
	CheckInChangeRequest(changeRequestID, comments string) (bool, error)
	/* Takes changeRequestID and a sign Off comment as input and signOff the change request with given comments ,returns is_signedOff/or not  */
	SignOffChangeRequest(changeRequestID, comments string) (bool, error)
	/* Takes changeRequestID and a review comment as input and moves the change request to review state with given comments ,returns is_status moved to review state/or not  */
	ReviewChangeRequest(changeRequestID, comments string) (bool, error)
	/* self validate method to check implementation class/structs is configured properly and good to use it */
	Validate() (bool, error)
}
