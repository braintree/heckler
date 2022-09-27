package hecklerd

import (
	cm_adapter "github.com/braintree/heckler/change_management/cm_adapter"
	"log"
)

func createChangeRequest(logger *log.Logger, env, nextTag string, cmAdapter cm_adapter.ChangeManagementAdapter) (string, bool) {

	changeRequestID, createError := cmAdapter.SearchAndCreateChangeRequest(env, nextTag)
	logger.Printf("CreateChangeRequest Status for %s:: : changeRequestID::%s and  createError::'%v'", nextTag, changeRequestID, createError)

	if cmAdapter.IsMandatory() && (createError != nil || changeRequestID == "") {
		logger.Println("CM IsMandatory, had a createCRError or changeRequestID is empty")
		logger.Println("Returns PauseExecution as true changeRequestID is Empty and   IsMandatory true")
		return "", true
	} else if cmAdapter.OnErrorFail() == false {
		logger.Println("Returns empty changeRequestID as onErrorFail is false")
		return "", false
	} else {
		logger.Println("Returns PauseExecution as true changeRequestID is Empty and  onErrorFail is true")
		return "", true
	}
	logger.Printf("Going for CheckIn the ChangeRequest %s", changeRequestID)
	isCheckedIN, checkinError := cmAdapter.CheckInChangeRequest(changeRequestID)
	logger.Printf("CheckInChangeRequest Status: isCheckedIN::%t and  checkinError::'%v'", isCheckedIN, checkinError)

	if cmAdapter.IsMandatory() && (checkinError != nil || isCheckedIN == false) {
		logger.Println("CM IsMandatory, had a checkinError or isCheckedIN is false")
		logger.Println("Returns PauseExecution as true isCheckedIN is false/checkinError and   IsMandatory true")
		return changeRequestID, true
	} else if cmAdapter.OnErrorFail() == false {
		logger.Println("Returns  changeRequestID and PauseExecution as false for onErrorFail is false")
		return changeRequestID, false
	} else {
		logger.Println("Returns PauseExecution as true changeRequestID is Empty for onErrorFail is true")
		return changeRequestID, true
	}

	if checkinError != nil || isCheckedIN == false {
		if cmAdapter.OnErrorFail() == true {
			logger.Printf(changeRequestID, "is not checked, returning from apply")
			return changeRequestID, true
		} else {
			logger.Printf(changeRequestID, "is not checked, returning from apply")
			return "", false
		}
	} else {
		return changeRequestID, false
	}

}
