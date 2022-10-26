package mockup_itfc_manager

import (
	"encoding/json"
	"errors"
	"fmt"
	cm_interfaces "github.com/braintree/heckler/change_management/interfaces"
	mockup_cms_client "github.com/braintree/heckler/mockups/change_management_system/client"
	"log"
	"time"
)

const SN_TIME_LAYOUT = "2006-01-02 15:04:05"
const DESC_PRE_FIX = "mockup change request for "

/*
MockupCMITFCManager is implementation class for cm_interfaces.ChangeManagementInterface which exposes minimum set of APIS to manage Changes
This class is dependeing upon your change manage system either SNOW/JIRA/Internal CMS system ..need to interact with agent of that system.
We had mockup_cms_client to imitate  like ServiceNow/JIRA client
*/
type MockupCMITFCManager struct {
	cmsClient mockup_cms_client.MockupCMSClient
	isValid   bool
}

func _newMockupCMITFCManager(cmsClient mockup_cms_client.MockupCMSClient) cm_interfaces.ChangeManagementInterface {
	var cmITFCMGR cm_interfaces.ChangeManagementInterface = MockupCMITFCManager{cmsClient: cmsClient,
		isValid: true}

	return cmITFCMGR
}

func GetMockupCMITFCManager(cmCFGPath string) (MockupCMITFCManager, error) {
	var emptyWrapper MockupCMITFCManager

	cmsClient, err1 := mockup_cms_client.GetMockupCMSClient()
	if err1 == nil {
		cmITFCMGR := MockupCMITFCManager{cmsClient: cmsClient, isValid: true}
		return cmITFCMGR, nil
	} else {
		return emptyWrapper, err1
	}
}
func (cmITFCMGR MockupCMITFCManager) SearchChangeRequest(hecklerEnv, tag string) (string, error) {

	isActive := "true"

	description := GetDescriptions(DESC_PRE_FIX, hecklerEnv, tag)
	log.Printf("SearchChangeRequestDetails hecklerEnv::%s isActive::%s description::%s tag::%s \n", hecklerEnv, isActive, description, tag)
	changes, searchError := cmITFCMGR.cmsClient.SearchChangeRequestDetails(hecklerEnv, isActive, description, tag)
	if searchError != nil {
		log.Println("SearchChangeRequestDetails returns searchError", searchError)
		return "", searchError
	} else if len(changes) > 0 {
		log.Println("change Requests", changes)
		// IMP:: Changes are sorted by ORDERBYDESCsys_created_on so first record is latest changeRequest

		crNumber := changes[0]["Number"]
		if crNumber == nil {
			return "", errors.New("CR Number is empty")
		} else {
			return fmt.Sprintf("%v", crNumber), nil
		}
	}
	log.Printf("No of changes is Zero(%d) hence Returning empty changeRequestID and nil as error\n", len(changes))
	return "", nil

}
func (cmITFCMGR MockupCMITFCManager) SearchAndCreateChangeRequest(hecklerEnv, tag string) (string, error) {
	changeRequestID, err1 := cmITFCMGR.SearchChangeRequest(hecklerEnv, tag)
	if err1 != nil {
		return "", err1
	} else if changeRequestID != "" {
		return changeRequestID, nil
	} else {
		return cmITFCMGR.CreateChangeRequest(hecklerEnv, tag)
	}

}
func (cmITFCMGR MockupCMITFCManager) CreateChangeRequest(hecklerEnv, tag string) (string, error) {
	createTicketData := make(map[string]interface{})
	createTicketData["env"] = hecklerEnv
	createTicketData["tag"] = tag

	isTicketCreated, changeRequestID, createError := cmITFCMGR.cmsClient.CreateChangeRequest(createTicketData)
	log.Println("CreateChangeRequest returned", isTicketCreated, changeRequestID, createError)
	return changeRequestID, createError

}

func (cmITFCMGR MockupCMITFCManager) CommentChangeRequest(changeRequestID, comments string) (bool, error) {
	if changeRequestID == "" {
		log.Println("changeRequestID is empty, so CommentChangeRequest return false ")
		return false, nil
	}

	return cmITFCMGR._updateChangeRequest(changeRequestID, comments)
}

func (cmITFCMGR MockupCMITFCManager) CheckInChangeRequest(changeRequestID, comments string) (bool, error) {
	if changeRequestID == "" {
		log.Println("changeRequestID is empty, so CommentChangeRequest return false ")
		return false, nil
	}
	if comments == "" {
		comments = "moving/checking Change Request to Scheduled state"
	}
	isTicketScheduled, scheduleError := cmITFCMGR._scheduleChangeRequest(changeRequestID, comments)

	if scheduleError == nil {
		log.Print("isTicketScheduled is  ", isTicketScheduled, " and sleeping 60 seconds before moving CR to Implement state\n")
		time.Sleep(time.Minute * 1)
		reviewComments := "moving change Request to Implement state"
		isTicketMovedToImplement, implementError := cmITFCMGR._moveChangeRequestToImplement(changeRequestID, reviewComments)

		if implementError == nil {
			log.Print("isTicketMovedToImplement is  ", isTicketMovedToImplement, "\n")

			msg := "As there are no changeTask lets move the ChangeRequest to review state Directly"
			log.Println(msg)
			isChangeRequestReviewed, changeRequestReviewError := cmITFCMGR._moveChangeRequestToReview(changeRequestID, msg)
			if changeRequestReviewError == nil {
				log.Println("Successfully Moved changeRequest To Reviewed", isChangeRequestReviewed, changeRequestReviewError)
			} else {
				msg := fmt.Sprintf("Unable to invoke _MoveChangeRequestToReview::%v\n", changeRequestReviewError)
				log.Println(msg)
				return false, errors.New(msg)
			}

		} else {
			log.Printf("Unable to invoke _moveChangeRequestToImplement::%v\n", implementError)
			return false, implementError
		}

	} else {
		log.Printf("Unable to invoke _scheduleChangeRequest::%v\n", scheduleError)
		return false, scheduleError
	}
	return true, nil
}

func (cmITFCMGR MockupCMITFCManager) ReviewChangeRequest(changeRequestID, reviewComments string) (bool, error) {
	if changeRequestID == "" {
		log.Println("changeRequestID is empty, so CommentChangeRequest return false ")
		return false, nil
	}
	if reviewComments == "" {
		reviewComments = "moving change Request to Review state"
	}
	isReviewed, reviewError := cmITFCMGR._moveChangeRequestToReview(changeRequestID, reviewComments)
	return isReviewed, reviewError
}

func (cmITFCMGR MockupCMITFCManager) SignOffChangeRequest(changeRequestID, comments string) (bool, error) {
	if changeRequestID == "" {
		log.Println("changeRequestID is empty, so CommentChangeRequest return false ")
		return false, nil
	}
	if comments == "" {
		comments = "moving change Request to Close state by Heckler"
	}
	closeCode := "Successful"
	closeNotes := "Closed Ticket by Heckler"
	state := "Closed"

	isSignedOff, err2 := cmITFCMGR.cmsClient.SignOffChangeRequest(changeRequestID, comments, closeNotes, closeCode, state)
	return isSignedOff, err2

}

func (cmITFCMGR MockupCMITFCManager) GetChangeRequestDetails(changeRequestID string) (string, error) {
	if changeRequestID == "" {
		log.Println("changeRequestID is empty, so CommentChangeRequest return false ")
		return "", nil
	}
	changeRequestData, err2 := cmITFCMGR.cmsClient.GetChangeRequestDetails(changeRequestID)

	if err2 != nil {
		log.Println("err2::", err2)
		return "", err2
	} else {
		log.Println("changeRequestData is ", changeRequestData)
		crDetailsStr, marshalError := json.Marshal(changeRequestData)
		if marshalError != nil {
			log.Printf("error while Marshalling %v", marshalError)
			return "", marshalError
		}
		return string(crDetailsStr), nil
	}

}

func (cmITFCMGR MockupCMITFCManager) Validate() (bool, error) {
	return cmITFCMGR.isValid, nil
}

func (cmITFCMGR MockupCMITFCManager) _moveChangeRequestToImplement(changeRequestID, comments string) (bool, error) {
	if comments == "" {
		comments = "moving change Request to Implement state"
	}
	state := "Implement"
	return cmITFCMGR._checkInChangeRequest(changeRequestID, comments, state)
}
func (cmITFCMGR MockupCMITFCManager) _scheduleChangeRequest(changeRequestID, comments string) (bool, error) {
	state := "Scheduled"
	return cmITFCMGR._checkInChangeRequest(changeRequestID, comments, state)

}

func (cmITFCMGR MockupCMITFCManager) _moveChangeRequestToReview(changeRequestID, comments string) (bool, error) {

	return cmITFCMGR.cmsClient.ReviewChangeRequest(changeRequestID, comments)

}
func (cmITFCMGR MockupCMITFCManager) _updateChangeRequest(changeRequestID, comments string) (bool, error) {
	isUpdated, err2 := cmITFCMGR.cmsClient.UpdateChangeRequest(changeRequestID, comments)
	return isUpdated, err2

}

func (cmITFCMGR MockupCMITFCManager) _checkInChangeRequest(changeRequestID, comments, state string) (bool, error) {
	isCheckedIn, err2 := cmITFCMGR.cmsClient.CheckInChangeRequest(changeRequestID, comments, state)
	return isCheckedIn, err2

}

func (cmITFCMGR MockupCMITFCManager) _signOffChangeTask(parentID, taskNo, workNotes string) (bool, error) {

	state := "Closed Complete"
	isUpdated, err2 := cmITFCMGR.cmsClient.SignoffChangeTask(parentID, taskNo, workNotes, state)
	return isUpdated, err2

}

func GetDescriptions(description_prefix, hecklerEnv, tag string) string {
	return fmt.Sprintf("%s %s::%s", description_prefix, hecklerEnv, tag)
}
