package mockup_cli_cm

import (
	"encoding/json"
	"errors"
	"fmt"
	mockup_itfc_manager "github.com/braintree/heckler/mockups/change_management_system/provider/manager"

	cm_models "github.com/braintree/heckler/change_management/models"
	"gopkg.in/yaml.v3"
	"log"
	"os"
)

/*
MockupCMCLIProvider has actual logic calling CM Interface Manager
It provides option to write output to temporary text file
*/
type MockupCMCLIProvider struct {
	cmITFCMGR mockup_itfc_manager.MockupCMITFCManager
	isValid   bool
}

func GetMockupCMCLIProvider(cmCFGPath string) (MockupCMCLIProvider, error) {
	var emptyMockupCMCLIProvider MockupCMCLIProvider
	cmITFCMGR, initError := mockup_itfc_manager.GetMockupCMITFCManager(cmCFGPath)
	if initError == nil {
		return MockupCMCLIProvider{cmITFCMGR: cmITFCMGR, isValid: true}, nil
	} else {
		return emptyMockupCMCLIProvider, initError
	}

}

func (cliProvider MockupCMCLIProvider) SearchChangeRequest(envPrefix, tag, outputFile string) (string, error, error) {
	var response string
	var outputError error
	changeRequestID, searchError := cliProvider.cmITFCMGR.SearchChangeRequest(envPrefix, tag)
	if searchError != nil {
		response, outputError = cliProvider.generateErrorMessage(searchError, outputFile)
	} else {
		if changeRequestID == "" {
			response, outputError = cliProvider.generateErrorMessage(errors.New("ChangeRequestID is None"), outputFile)

		} else {
			response, outputError = cliProvider.generateCRResultMessage(changeRequestID, "", outputFile)
		}
	}
	return response, searchError, outputError
}
func (cliProvider MockupCMCLIProvider) SearchAndCreateChangeRequest(envPrefix, tag, outputFile string) (string, error, error) {
	var response string
	var outputError error
	changeRequestID, searchError := cliProvider.cmITFCMGR.SearchAndCreateChangeRequest(envPrefix, tag)
	if searchError != nil {
		response, outputError = cliProvider.generateErrorMessage(searchError, outputFile)
	} else {
		if changeRequestID == "" {
			response, outputError = cliProvider.generateErrorMessage(errors.New("ChangeRequestID is None"), outputFile)

		} else {
			response, outputError = cliProvider.generateCRResultMessage(changeRequestID, "", outputFile)
		}
	}

	return response, searchError, outputError
}

func (cliProvider MockupCMCLIProvider) CreateChangeRequest(envPrefix, tag, outputFile string) (string, error, error) {
	var response string
	var outputError error
	changeRequestID, createError := cliProvider.cmITFCMGR.CreateChangeRequest(envPrefix, tag)
	log.Println("changeRequestID is ", changeRequestID)
	log.Println("createError::", createError)
	if createError != nil {
		response, outputError = cliProvider.generateErrorMessage(createError, outputFile)
	} else {
		if changeRequestID == "" {
			response, outputError = cliProvider.generateErrorMessage(errors.New("ChangeRequestID is None"), outputFile)

		} else {
			response, outputError = cliProvider.generateCRResultMessage(changeRequestID, "", outputFile)
		}
	}
	return response, createError, outputError
}
func (cliProvider MockupCMCLIProvider) GetChangeRequestDetails(changeRequestID, outputFile string) (string, error, error) {
	var response string
	var outputError error
	crDetails, crErr := cliProvider.cmITFCMGR.GetChangeRequestDetails(changeRequestID)
	log.Println("crErr::", crErr)
	log.Println("crDetails is ", crDetails)
	if crErr != nil {
		response, outputError = cliProvider.generateErrorMessage(crErr, outputFile)
	} else {
		if crDetails == "" {
			response, outputError = cliProvider.generateErrorMessage(errors.New("ChangeRequest Details is None"), outputFile)

		} else {
			response, outputError = cliProvider.generateCRResultMessage(changeRequestID, crDetails, outputFile)
		}
	}

	return response, crErr, outputError
}
func (cliProvider MockupCMCLIProvider) CommentChangeRequest(changeRequestID, comments, outputFile string) (string, error, error) {
	var response string
	var outputError error
	isUpdated, commentError := cliProvider.cmITFCMGR.CommentChangeRequest(changeRequestID, comments)
	log.Println("isUpdated is ", isUpdated)
	log.Println("commentError::", commentError)
	if commentError != nil {
		response, outputError = cliProvider.generateErrorMessage(commentError, outputFile)
	} else {
		if isUpdated {
			msg := fmt.Sprintf("'%s' comments added to ChangeRequest::%s", comments, changeRequestID)
			response, outputError = cliProvider.generateCRActionResultMessage(changeRequestID, msg, outputFile)
		} else {
			response, outputError = cliProvider.generateErrorMessage(errors.New("Unable provide comments to the Change Request"), outputFile)
		}
	}
	return response, commentError, outputError
}

func (cliProvider MockupCMCLIProvider) CheckInChangeRequest(changeRequestID, comments, outputFile string) (string, error, error) {
	var response string
	var outputError error
	isCheckedIN, checkINError := cliProvider.cmITFCMGR.CheckInChangeRequest(changeRequestID, comments)
	log.Println("isCheckedIN is ", isCheckedIN)
	log.Println("checkINError::", checkINError)
	if checkINError != nil {
		response, outputError = cliProvider.generateErrorMessage(checkINError, outputFile)
	} else {
		if isCheckedIN {
			msg := fmt.Sprintf("ChangeRequest::%s is CheckedIn", changeRequestID)
			response, outputError = cliProvider.generateCRActionResultMessage(changeRequestID, msg, outputFile)
		} else {
			response, outputError = cliProvider.generateErrorMessage(errors.New("Unable CheckIN the Change Request"), outputFile)
		}
	}
	return response, checkINError, outputError
}
func (cliProvider MockupCMCLIProvider) ReviewChangeRequest(changeRequestID, comments, outputFile string) (string, error, error) {
	var response string
	var outputError error
	isReviewed, reivewError := cliProvider.cmITFCMGR.ReviewChangeRequest(changeRequestID, comments)
	log.Println("isCheckedIN is ", isReviewed)
	log.Println("reivewError::", reivewError)
	if reivewError != nil {
		response, outputError = cliProvider.generateErrorMessage(reivewError, outputFile)
	} else {
		if isReviewed {
			msg := fmt.Sprintf("ChangeRequest::%s is Reviewed", changeRequestID)
			response, outputError = cliProvider.generateCRActionResultMessage(changeRequestID, msg, outputFile)
		} else {
			response, outputError = cliProvider.generateErrorMessage(errors.New("Unable move the Change Request to Review state"), outputFile)
		}
	}
	return response, reivewError, outputError
}

func (cliProvider MockupCMCLIProvider) SignOffChangeRequest(changeRequestID, comments, outputFile string) (string, error, error) {
	var response string
	var outputError error
	isSignedOff, signedOffErr := cliProvider.cmITFCMGR.SignOffChangeRequest(changeRequestID, comments)
	log.Println("isSignedOff is ", isSignedOff)
	log.Println("signedOffErr::", signedOffErr)
	if signedOffErr != nil {
		response, outputError = cliProvider.generateErrorMessage(signedOffErr, outputFile)
	} else {
		if isSignedOff {
			msg := fmt.Sprintf("ChangeRequest::%s is SignedOff", changeRequestID)
			response, outputError = cliProvider.generateCRActionResultMessage(changeRequestID, msg, outputFile)
		} else {
			response, outputError = cliProvider.generateErrorMessage(errors.New("Unable SignOff the Change Request"), outputFile)
		}
	}
	return response, signedOffErr, outputError
}
func (cliProvider MockupCMCLIProvider) Validate(outputFile string) (string, error, error) {
	var response string
	var outputError error

	log.Println("cliProvider.isValid::", cliProvider.isValid)

	if cliProvider.isValid {
		msg := fmt.Sprintf("CLI Validation is fine")
		changeRequestID := ""
		response, outputError = cliProvider.generateCRActionResultMessage(changeRequestID, msg, outputFile)
	} else {
		response, outputError = cliProvider.generateErrorMessage(errors.New("Unable Validate CLI"), outputFile)
	}

	return response, nil, outputError
}

func (cliMGR MockupCMCLIProvider) generateCRActionResultMessage(changeRequestID, message, outputFile string) (string, error) {
	crActionResult := cm_models.CRActionResultType{
		ChangeRequestID: changeRequestID,
		Message:         message,
	}

	crActionResultPayLoad := cm_models.CMResponsePayLoad{
		CRActionResult: crActionResult,
		Status:         cm_models.STATUS_SUCCESS,
	}
	return writeAndReturnJson(outputFile, crActionResultPayLoad)
}

func (cliMGR MockupCMCLIProvider) generateErrorMessage(err error, outputFile string) (string, error) {
	message := fmt.Sprintf("%v", err)
	errorType := cm_models.ErrorType{
		Message: message,
	}
	errorPaypLoad := cm_models.CMResponsePayLoad{
		Error:  errorType,
		Status: cm_models.STATUS_FAILURE,
	}
	return writeAndReturnJson(outputFile, errorPaypLoad)

}

func (cliMGR MockupCMCLIProvider) generateCRResultMessage(changeRequestID, crDetails, outputFile string) (string, error) {

	crResult := cm_models.CRResultType{
		ChangeRequestID: changeRequestID,
		CRDetails:       crDetails,
	}
	crResultPaypLoad := cm_models.CMResponsePayLoad{
		CRResult: crResult,
		Status:   cm_models.STATUS_SUCCESS,
	}
	return writeAndReturnJson(outputFile, crResultPaypLoad)

}

func writeAndReturnYaml(outputFile string, payLoad interface{}) (string, error) {
	result, err1 := yaml.Marshal(payLoad)
	if err1 != nil {
		return string(result), err1
	} else {
		writeFile(outputFile, result)
		// ***IMP*** THIS FMT.PRINTLN IS REQUIRED TO  TO READ THE OUTPUT FROM CLI CLIENT
		fmt.Println(string(result))
		return string(result), nil
	}
}
func writeAndReturnJson(outputFile string, payLoad interface{}) (string, error) {
	result, err1 := json.MarshalIndent(payLoad, "", "")
	if err1 != nil {
		return string(result), err1
	} else {
		writeFile(outputFile, result)
		// ***IMP*** THIS FMT.PRINTLN IS REQUIRED TO  TO READ THE OUTPUT FROM CLI CLIENT
		fmt.Println(string(result))
		return string(result), nil
	}

}
func writeFile(outputFile string, data []byte) error {
	if outputFile == "" {
		return nil
	}
	err := os.WriteFile(outputFile, data, 0644)
	return err
}
