package main

import (
	"errors"
	"fmt"
	cm_models "github.com/braintree/heckler/change_management/models"
	cm_cli_provider "github.com/braintree/heckler/mockups/change_management_system/provider/cli"
	response_util "github.com/braintree/heckler/mockups/utils/response"
	"log"
	"testing"
)

/*
This test validates MockupCMCLIProvider (which  is actual logic calling CM Interface Manager)
*/

const TEST_DESC_PREFIX = "Heckler Testing for Tag::"
const TAG_VALUE = "v99"
const ENV_PREFIX = "MOCKTESTING"
const EMPTY_OUTPUT_FILE_NAME = ""

var CM_CONFI_PATH = "/etc/hecklerd/cm_config_conf.yaml"
var mockupcmCLIProvider cm_cli_provider.MockupCMCLIProvider
var providerError error

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	mockupcmCLIProvider, providerError = cm_cli_provider.GetMockupCMCLIProvider(CM_CONFI_PATH)
}

func TestAllMethods(t *testing.T) {
	fmt.Println("inside TestAllMethods")
	if providerError != nil {
		t.Errorf(fmt.Sprintf("%t", providerError))
		return
	}
	isValid, validError := invokeValidate(t)
	if validError != nil {
		t.Errorf(fmt.Sprintf("%t", validError))
		return
	}
	if isValid == false {
		t.Errorf("Provider is not valid")
		return
	}
	changeRequestID, createExeuction := invokeCreateChangeRequest(t)
	if createExeuction != nil {
		t.Errorf(fmt.Sprintf("%t", createExeuction))
		return
	}

	if changeRequestID != "" {

		isCommented, commentError := _invokeCommentChangeRequest(t, changeRequestID)
		log.Println("isCommented", isCommented)
		if commentError != nil {
			t.Errorf(fmt.Sprintf("%v", commentError))
		}
		isCheckIn, checkInError := _checkInChangeRequest(t, changeRequestID)
		log.Println("isCheckIn", isCheckIn)
		if checkInError != nil {
			t.Errorf(fmt.Sprintf("%v", checkInError))
		}

		isSignOff, signOffError := _signOffChangeRequest(t, changeRequestID)
		log.Println("isSignOff", isSignOff)
		if signOffError != nil {
			t.Errorf(fmt.Sprintf("%v", signOffError))
		}
	}

}

func invokeSearchAndCreateCR(t *testing.T) {
	log.Println("inside invokeSearchAndCreateCR")

	mockupcmCLIProvider.SearchAndCreateChangeRequest(ENV_PREFIX, TAG_VALUE, EMPTY_OUTPUT_FILE_NAME)

}

func invokeSearchChangeRequest() (string, error) {
	log.Println("inside invokeSearchChangeRequest")

	mockupcmCLIProvider.SearchChangeRequest(ENV_PREFIX, TAG_VALUE, EMPTY_OUTPUT_FILE_NAME)
	return "", nil
}
func invokeSearchAndCreateChangeRequest() (string, error) {
	log.Println("inside invokeSearchAndCreateChangeRequest")

	mockupcmCLIProvider.SearchAndCreateChangeRequest(ENV_PREFIX, TAG_VALUE, EMPTY_OUTPUT_FILE_NAME)
	return "", nil
}
func invokeCreateChangeRequest(t *testing.T) (string, error) {
	log.Println("inside invokeCreateChangeRequest")

	response, createError, outputError := mockupcmCLIProvider.CreateChangeRequest(ENV_PREFIX, TAG_VALUE, EMPTY_OUTPUT_FILE_NAME)

	if createError != nil {
		t.Errorf(fmt.Sprintf("%t", createError))
		return "", createError
	}
	if outputError != nil {
		t.Errorf(fmt.Sprintf("%t", outputError))
		return "", outputError
	}
	responseJson, responseError := response_util.GetResponseJson(response)
	_checkResponseJson(t, responseJson)

	if responseError != nil {
		return "", responseError
	} else {
		if responseJson.Status == cm_models.STATUS_FAILURE {
			return "", errors.New("Create CR is failed")
		} else {
			return responseJson.CRResult.ChangeRequestID, nil
		}

	}

}
func invokeGetChangeRequestDetails(t *testing.T, changeRequestID string) (string, error) {
	log.Println("inside invokeGetChangeRequestDetails", changeRequestID)

	response, createError, outputError := mockupcmCLIProvider.GetChangeRequestDetails(changeRequestID, EMPTY_OUTPUT_FILE_NAME)

	if createError != nil {
		t.Errorf(fmt.Sprintf("%t", createError))
		return "", createError
	}
	if outputError != nil {
		t.Errorf(fmt.Sprintf("%t", outputError))
		return "", outputError
	}
	responseJson, responseError := response_util.GetResponseJson(response)
	_checkResponseJson(t, responseJson)

	if responseError != nil {
		return "", responseError
	} else {
		if responseJson.Status == cm_models.STATUS_FAILURE {
			return "", errors.New("Create CR is failed")
		} else {
			return responseJson.CRResult.ChangeRequestID, nil
		}

	}
}

func _invokeCommentChangeRequest(t *testing.T, changeRequestID string) (bool, error) {
	log.Println("inside invokeCommentChangeRequest", changeRequestID)

	response, createError, outputError := mockupcmCLIProvider.CommentChangeRequest(changeRequestID, "testing using cli", EMPTY_OUTPUT_FILE_NAME)

	if createError != nil {
		t.Errorf(fmt.Sprintf("%t", createError))
		return false, createError
	}
	if outputError != nil {
		t.Errorf(fmt.Sprintf("%t", outputError))
		return false, outputError
	}
	responseJson, responseError := response_util.GetResponseJson(response)
	_checkResponseJson(t, responseJson)

	if responseError != nil {
		return false, responseError
	} else {
		if responseJson.Status == cm_models.STATUS_FAILURE {
			return false, errors.New("CR is not commented")
		} else {
			return true, nil
		}

	}
}
func _checkInChangeRequest(t *testing.T, changeRequestID string) (bool, error) {
	response, createError, outputError := mockupcmCLIProvider.CheckInChangeRequest(changeRequestID, "checkin by testing -invokeCheckInChangeRequest", EMPTY_OUTPUT_FILE_NAME)

	if createError != nil {
		t.Errorf(fmt.Sprintf("%t", createError))
		return false, createError
	}
	if outputError != nil {
		t.Errorf(fmt.Sprintf("%t", outputError))
		return false, outputError
	}
	responseJson, responseError := response_util.GetResponseJson(response)
	_checkResponseJson(t, responseJson)

	if responseError != nil {
		return false, responseError
	} else {
		if responseJson.Status == cm_models.STATUS_FAILURE {
			return false, errors.New("CR is not checkedin")
		} else {
			return true, nil
		}

	}
}
func _signOffChangeRequest(t *testing.T, changeRequestID string) (bool, error) {
	log.Println("inside invokeSignOffChangeRequest", changeRequestID)

	response, createError, outputError := mockupcmCLIProvider.SignOffChangeRequest(changeRequestID, "signoff by testing -invokeCheckInChangeRequest", EMPTY_OUTPUT_FILE_NAME)

	if createError != nil {
		t.Errorf(fmt.Sprintf("%t", createError))
		return false, createError
	}
	if outputError != nil {
		t.Errorf(fmt.Sprintf("%t", outputError))
		return false, outputError
	}
	responseJson, responseError := response_util.GetResponseJson(response)
	_checkResponseJson(t, responseJson)

	if responseError != nil {
		return false, responseError
	} else {
		if responseJson.Status == cm_models.STATUS_FAILURE {
			return false, errors.New("CR is not signedoff")
		} else {
			return true, nil
		}

	}
}
func _checkResponseJson(t *testing.T, responseJson cm_models.CMResponsePayLoad) {
	if responseJson.Status == cm_models.STATUS_FAILURE {
		log.Println("responseJson.Error...", responseJson.Error.Message, responseJson.Error.Detail)
		t.Errorf(responseJson.Error.Message)
		t.Errorf(responseJson.Error.Detail)
	}
}

func invokeValidate(t *testing.T) (bool, error) {
	response, respError, outputError := mockupcmCLIProvider.Validate(EMPTY_OUTPUT_FILE_NAME)
	if respError != nil {
		t.Errorf(fmt.Sprintf("%t", respError))
		return false, respError
	}
	if outputError != nil {
		t.Errorf(fmt.Sprintf("%t", outputError))
		return false, outputError
	}
	responseJson, responseError := response_util.GetResponseJson(response)
	_checkResponseJson(t, responseJson)
	if responseError != nil {
		t.Errorf(fmt.Sprintf("%t", responseError))
		return false, responseError
	} else {
		if responseJson.Status == cm_models.STATUS_FAILURE {
			log.Println("responseJson.Error.Message..", responseJson.Error.Message)
			log.Println("responseJson.Error.Detail..", responseJson.Error.Detail)
			t.Errorf("mockupcmCLIProvider is not valid")
			return false, errors.New(responseJson.Error.Message)
		} else {
			return true, nil
		}

	}
}
