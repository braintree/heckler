package cm_interfaces

type ChangeManagementInterface interface {
	SearchChangeRequest(gsnowEnv, tag string) (string, error)
	// 	SearchAndCreateChangeRequest(hecklerEnv, tag string) (string, error)
	CreateChangeRequest(gsnowEnv, tag string) (string, error)
	GetChangeRequestDetails(changeRequestID string) (string, error)
	CommentChangeRequest(changeRequestID, comments string) (bool, error)
	CheckInChangeRequest(changeRequestID, comments string) (bool, error)
	SignOffChangeRequest(changeRequestID, comments string) (bool, error)
	ReviewChangeRequest(changeRequestID, comments string) (bool, error)
	Validate() (bool, error)
}
