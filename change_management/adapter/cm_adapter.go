package cm_adapter

import (
	"encoding/json"
	"errors"
	"fmt"
	cm_cli "github.com/braintree/heckler/change_management/agent/cli"
	cm_itfc "github.com/braintree/heckler/change_management/interfaces"
	cm_models "github.com/braintree/heckler/change_management/models"
	"log"
)

/*
Converting Generic Interface to ChangeManagementInterface If not through Error
*/
func convertIntoCMInterface(cmITFCMGR interface{}) (cm_itfc.ChangeManagementInterface, error) {
	var emptyItfcMGR cm_itfc.ChangeManagementInterface
	switch iTypeValue := cmITFCMGR.(type) {
	case cm_itfc.ChangeManagementInterface:

		fmt.Println("type cm_itfc.ChangeManagementInterface ")
		vResp, vErr := iTypeValue.Validate()
		fmt.Println("vResp,vErr", vResp, vErr)
		return iTypeValue, nil
	default:
		msg := fmt.Sprintf("%v is not a cm_itfc.ChangeManagementInterface", iTypeValue)
		log.Println(msg)
		return emptyItfcMGR, errors.New(msg)

	}

}

/* This function takes an interface or nil interface, If nil interface is faced it look for CLI AGENT path from config file and check is that cli exists and is valid? */
func getCMAgent(adapterConfig cm_models.ChangeManagementAdapterConfig, cmITFCMGR interface{}) (cm_itfc.ChangeManagementInterface, error) {

	var emptyCMAgent cm_itfc.ChangeManagementInterface
	var cmAgent cm_itfc.ChangeManagementInterface
	var cmAgentError error
	log.Println("cmITFCMGR is None?::", cmITFCMGR == nil)
	log.Println("adapterConfig is ::", adapterConfig)

	if cmITFCMGR != nil {
		log.Println("cmITFCMGR is not None::", adapterConfig.CMConfigPath)
		cmAgent, cmAgentError = convertIntoCMInterface(cmITFCMGR)
	} else if adapterConfig.CLIAgentPath != "" {
		log.Println("CLIAgentPath::", adapterConfig.CLIAgentPath)
		cmAgent, cmAgentError = cm_cli.GetCMCLIAgent(adapterConfig.CLIAgentPath, adapterConfig.Verbose)
	}
	if cmAgentError != nil {
		log.Println("cmAgentError::", cmAgentError)
		return emptyCMAgent, cmAgentError
	}
	if cmAgent == nil {
		if adapterConfig.IsMandatory == true {
			msg := "CM IsMandatory is true but Unable to get ChangeManagement Agent"
			log.Println(msg)
			return emptyCMAgent, errors.New(msg)
		} else {
			return emptyCMAgent, nil
		}

	} else {
		isValidated, vError := cmAgent.Validate()
		if vError == nil {
			if isValidated {
				return cmAgent, nil
			} else {
				msg := fmt.Sprintf("cmAgent %v and Validate returns %t", cmAgent, isValidated)
				log.Println(msg)
				return emptyCMAgent, errors.New(msg)
			}
		} else {
			msg := fmt.Sprintf("cmAgent %v Not able to call Validate method %t", cmAgent, vError)
			log.Println(msg)
			return emptyCMAgent, errors.New(msg)
		}
	}

}

/*convert Generic Map into ChangeManagementAdapterConfig struct*/
func convertAdapterConfig(adpaterConfig map[string]interface{}) (cm_models.ChangeManagementAdapterConfig, error) {

	var emptyConfig cm_models.ChangeManagementAdapterConfig
	data, dError := json.Marshal(adpaterConfig)
	if dError != nil {
		log.Println("DError", dError)
		return emptyConfig, dError
	}
	log.Println("cmAdatperConfig...", string(data))
	cmAdatperConfig := new(cm_models.ChangeManagementAdapterConfig)
	cmAdatperConfigError := json.Unmarshal(data, &cmAdatperConfig)
	log.Println("cmAdatperConfigError...and cmAdatperConfig...", cmAdatperConfigError, cmAdatperConfig)
	return *cmAdatperConfig, cmAdatperConfigError

}

/*
CM Adapter struct/class has encapsulated functions to have a loose coupling with(miminum code changes in) hecklerd/main.go
*/
type ChangeManagementAdapter struct {
	cmAgent       cm_itfc.ChangeManagementInterface
	adapterConfig cm_models.ChangeManagementAdapterConfig
}

func NewCMAdapter(config map[string]interface{}, cmITFCMGR interface{}) (ChangeManagementAdapter, error) {
	var cmAdapter ChangeManagementAdapter

	adapterConfig, adapterConfigError := convertAdapterConfig(config)
	if adapterConfigError != nil {
		log.Println("adapterConfigError...", adapterConfigError)
		return cmAdapter, adapterConfigError
	}
	agent, agentError := getCMAgent(adapterConfig, cmITFCMGR)
	if agentError != nil {
		log.Println("agentError", agentError)
		return cmAdapter, agentError
	}
	cmAdapter = ChangeManagementAdapter{adapterConfig: adapterConfig, cmAgent: agent}
	return cmAdapter, nil

}

/*
Get change Request in Json format for a given changeRequest ID
*/
func (cmAdapter ChangeManagementAdapter) getChangeRequestDetails(changeRequestID string) (string, error) {

	if changeRequestID == "" {
		log.Println("GetChangeRequestDetails::changeRequestID is empty")
		return "", nil
	}
	crDetails, crError := cmAdapter.cmAgent.GetChangeRequestDetails(changeRequestID)
	if crError != nil && cmAdapter.isMandatory() == false {
		return "", nil
	}
	return crDetails, crError
}

/*
This function creates a ChangeRequest for given envPrefix and Tag value
and CheckIn the ChangeRequest with checkin comments
returns ChangeRequest if Success, proper error in case of error
*/
func (cmAdapter ChangeManagementAdapter) CreateAndCheckInCR(envPrefix, nextTag, checkINComments string) (string, bool) {

	if cmAdapter.cmAgent == nil && cmAdapter.isMandatory() == false {
		log.Println("AS CM is not Mandatory and CMAGent is not configured,..returning empty CR ID")
		return "", false
	}
	changeRequestID, createError := cmAdapter.createChangeRequest(envPrefix, nextTag)
	log.Printf("CreateChangeRequest Status for %s:: : changeRequestID::%s and  createError::'%v'", nextTag, changeRequestID, createError)

	if createError != nil || changeRequestID == "" {
		log.Println("had a createCRError or changeRequestID is empty")
		pauseExecutuion := cmAdapter.getPauseExecutionValue()
		log.Println("Returns PauseExecution as", pauseExecutuion, " and changeRequestID is Empty")
		return "", pauseExecutuion
	}
	log.Printf("Going for CheckIn the ChangeRequest %s %s", changeRequestID, checkINComments)
	isCheckedIN, checkinError := cmAdapter.checkInChangeRequest(changeRequestID, checkINComments)
	log.Printf("CheckInChangeRequest Status: isCheckedIN::%t and  checkinError::'%v'", isCheckedIN, checkinError)

	if checkinError != nil || isCheckedIN == false {
		log.Println("had a checkinError or isCheckedIN is false")
		pauseExecutuion := cmAdapter.getPauseExecutionValue()
		log.Println("Returns PauseExecution as", pauseExecutuion, " and changeRequestID is Empty")
		return "", pauseExecutuion
	}

	return changeRequestID, false

}

/* This function decided whether hecklerd/main.go has to skip current puppet apply steps or not
based upon IsMandatory/onErrorStop config values
*/
func (cmAdapter ChangeManagementAdapter) getPauseExecutionValue() bool {
	if cmAdapter.isMandatory() {
		log.Println("CM IsMandatory is True,hence getPauseExecutionValue is true")
		return true
	} else if cmAdapter.onErrorStop() == true {
		log.Println("onErrorStop is True so getPauseExecutionValue is true")
		return true
	} else {
		log.Println("onErrorStop is false")
		return false
	}
}

/*
Create a ChangeRequest, Passing minimum info like
*/
func (cmAdapter ChangeManagementAdapter) createChangeRequest(envPrefix, tag string) (string, error) {
	crID, crError := cmAdapter.cmAgent.CreateChangeRequest(envPrefix, tag)
	if crError != nil && cmAdapter.isMandatory() == false {
		log.Println("CreateChangeRequest::IsMandatory is false", crError)
		return "", nil
	}
	return crID, crError

}

/*
Add a note/comment to a given Change Request
*/
func (cmAdapter ChangeManagementAdapter) CommentChangeRequest(changeRequestID, comments string) (bool, error) {
	if changeRequestID == "" {
		log.Println("CommentChangeRequest::changeRequestID is empty")
		return true, nil
	}
	log.Printf("changeRequestID %s snowComments %s", changeRequestID, comments)
	updated, crError := cmAdapter.cmAgent.CommentChangeRequest(changeRequestID, comments)

	if crError != nil && cmAdapter.isMandatory() == false {
		log.Println("CommentChangeRequest::IsMandatory is false", crError)
		return true, nil
	}
	return updated, crError
}

/*
CheckIn the ChangeRequest
*/
func (cmAdapter ChangeManagementAdapter) checkInChangeRequest(changeRequestID, comments string) (bool, error) {
	if changeRequestID == "" {
		log.Println("CheckInChangeRequest::changeRequestID is empty")
		return true, nil
	}
	updated, crError := cmAdapter.cmAgent.CheckInChangeRequest(changeRequestID, comments)
	if crError != nil && cmAdapter.isMandatory() == false {
		log.Println("CheckInChangeRequest::IsMandatory is false", crError)
		return true, nil
	}
	return updated, crError
}

/*
Close/SignOff change request
*/
func (cmAdapter ChangeManagementAdapter) SignOffChangeRequest(changeRequestID, comments string) (bool, error) {
	if changeRequestID == "" {
		log.Println("SignOffChangeRequest::changeRequestID is empty,so no Need of SignOffChangeRequest")
		return true, nil
	}
	isSignedOff, signOffError := cmAdapter.cmAgent.SignOffChangeRequest(changeRequestID, comments)
	log.Printf("SignOffChangeRequest Status: isSignedOff::%t and  signOffError::'%v'", isSignedOff, signOffError)
	if signOffError != nil && cmAdapter.isMandatory() == false {
		log.Println("SignOffChangeRequest::IsMandatory is false", signOffError)
		return true, nil
	}
	return isSignedOff, signOffError
}

/*
Check Underlaying Agent is configured properly or not.
*/
func (cmAdapter ChangeManagementAdapter) Validate() (bool, error) {
	if cmAdapter.isMandatory() == false {
		log.Println("Validate::IsMandatory is false")
		return true, nil
	}
	return cmAdapter.cmAgent.Validate()

}

/*
If isMandatory is true return true.
If isMandatory is false then return OnErrorStop config value
*/
func (cmAdapter ChangeManagementAdapter) onErrorStop() bool {
	if cmAdapter.isMandatory() == true {
		return true
	} else {
		log.Println("OnErrorStop::IsMandatory is false so returning ONErrorFail returning configured value")

		return cmAdapter.adapterConfig.OnErrorStop
	}

}

func (cmAdapter ChangeManagementAdapter) isMandatory() bool {
	return cmAdapter.adapterConfig.IsMandatory

}
