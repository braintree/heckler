package cm_interfaces

type ChangeManagementInterface interface {
	SearchChangeRequest(gsnowEnv, tag string) (string, error)
	SearchAndCreateChangeRequest(hecklerEnv, tag string) (string, error)
	CreateChangeRequest(gsnowEnv, tag string) (string, error)
	CommentChangeRequest(changeRequestID, comments string) (bool, error)
	CheckInChangeRequest(changeRequestID string) (bool, error)
	SignOffChangeRequest(changeRequestID string) (bool, error)
	GetChangeRequestDetails(changeRequestID string) (string, error)
	Validate() (bool, error)
}
