package hecklerd

import (
	cm_adapter "github.com/braintree/heckler/change_management/cm_adapter"
	"log"
)

func signOffChangeRequest(logger *log.Logger, changeRequestID string, cmAdapter cm_adapter.ChangeManagementAdapter) {
	if changeRequestID != "" {
		signOffComments := "SigningOff the changeRequest by Hecklerd(apply)"
		isSignedOff, signOffError := cmAdapter.SignOffChangeRequest(changeRequestID, signOffComments)
		logger.Printf("SignOffChangeRequest Status: isSignedOff::%t and  signOffError::'%v'", isSignedOff, signOffError)
	} else {
		logger.Printf("changeRequestID is empty no Need of SignOffChangeRequest")
	}
}
