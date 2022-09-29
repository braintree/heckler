package cm_adapter

import (
	"errors"
	"fmt"
	cm_cli "github.com/braintree/heckler/change_management/agent/cli"
	cm_plugin "github.com/braintree/heckler/change_management/agent/plugin"
	cm_itfc "github.com/braintree/heckler/change_management/interfaces"
	cm_models "github.com/braintree/heckler/change_management/models"
	"log"
)

func getCMAgent(adapterConfig cm_models.ChangeManagementAdapterConfig, getCMITFCManager func(string) (cm_itfc.ChangeManagementInterface, error)) (cm_itfc.ChangeManagementInterface, error) {
	var emptyCMAgent cm_itfc.ChangeManagementInterface
	var cmAgent cm_itfc.ChangeManagementInterface
	var cmAgentError error
	log.Println("getCMITFCManager is None?::", getCMITFCManager == nil, adapterConfig.CMConfigPath)
	if getCMITFCManager != nil {
		log.Println("getCMITFCManager is not None::", adapterConfig.CMConfigPath)
		cmAgent, cmAgentError = getCMITFCManager(adapterConfig.CMConfigPath)
	} else if adapterConfig.CLIAgentPath != "" {
		log.Println("CLIAgentPath::", adapterConfig.CLIAgentPath)
		cmAgent, cmAgentError = cm_cli.GetCMCLIAgent(adapterConfig.CLIAgentPath, adapterConfig.Verbose)
	} else if adapterConfig.PluginAgentPath != "" {
		log.Println("PluginAgentPath::", adapterConfig.PluginAgentPath)
		cmAgent, cmAgentError = cm_plugin.GetCMPluginAgent(adapterConfig.CMConfigPath)
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
				msg := fmt.Sprintf("cmAgent %t Validate returns %t", cmAgent, isValidated)
				log.Println(msg)
				return emptyCMAgent, errors.New(msg)
			}
		} else {
			msg := fmt.Sprintf("cmAgent %t Not able to call Validate method %v", cmAgent, vError)
			log.Println(msg)
			return emptyCMAgent, errors.New(msg)
		}
	}

}

type ChangeManagementAdapter struct {
	cmAgent       cm_itfc.ChangeManagementInterface
	adapterConfig cm_models.ChangeManagementAdapterConfig
}

func NewCMAdapter(adapterConfig cm_models.ChangeManagementAdapterConfig, getCMITFCManager func(string) (cm_itfc.ChangeManagementInterface, error)) (ChangeManagementAdapter, error) {
	var cmAdapter ChangeManagementAdapter
	agent, agentError := getCMAgent(adapterConfig, getCMITFCManager)
	if agentError != nil {
		log.Println("agentError", agentError)
		return cmAdapter, agentError
	}
	cmAdapter = ChangeManagementAdapter{adapterConfig: adapterConfig, cmAgent: agent}
	return cmAdapter, nil

}
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
func (cmAdapter ChangeManagementAdapter) CreateAndCheckInCR(env, nextTag, checkINComments string) (string, bool) {

	if cmAdapter.cmAgent == nil && cmAdapter.isMandatory() == false {
		log.Println("AS CM is not Mandatory and CMAGent is not configured,..returning empty CR ID")
		return "", false
	}
	changeRequestID, createError := cmAdapter.createChangeRequest(env, nextTag)
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
func (cmAdapter ChangeManagementAdapter) searchChangeRequest(gsnowEnv, tag string) (string, error) {

	crID, searchError := cmAdapter.cmAgent.SearchChangeRequest(gsnowEnv, tag)
	if searchError != nil && cmAdapter.isMandatory() == false {
		log.Println("SearchChangeRequest::IsMandatory is false", searchError)
		return "", nil
	}
	return crID, searchError
}

/*
func (cmAdapter ChangeManagementAdapter) SearchAndCreateChangeRequest(hecklerEnv, tag string) (string, error) {

	crID, searchError := cmAdapter.cmAgent.SearchAndCreateChangeRequest(hecklerEnv, tag)
	if searchError != nil && cmAdapter.isMandatory() == false {
		log.Println("SearchAndCreateChangeRequest::IsMandatory is false", searchError)
		return "", nil
	}
	return crID, searchError
}
*/
func (cmAdapter ChangeManagementAdapter) createChangeRequest(gsnowEnv, tag string) (string, error) {
	crID, crError := cmAdapter.cmAgent.CreateChangeRequest(gsnowEnv, tag)
	if crError != nil && cmAdapter.isMandatory() == false {
		log.Println("CreateChangeRequest::IsMandatory is false", crError)
		return "", nil
	}
	return crID, crError

}
func (cmAdapter ChangeManagementAdapter) CommentChangeRequest(changeRequestID, comments string) (bool, error) {
	if changeRequestID == "" {
		log.Println("CommentChangeRequest::changeRequestID is empty")
		return true, nil
	}
	updated, crError := cmAdapter.cmAgent.CommentChangeRequest(changeRequestID, comments)

	if crError != nil && cmAdapter.isMandatory() == false {
		log.Println("CommentChangeRequest::IsMandatory is false", crError)
		return true, nil
	}
	return updated, crError
}
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

func (cmAdapter ChangeManagementAdapter) reviewChangeRequest(changeRequestID, comments string) (bool, error) {
	if changeRequestID == "" {
		log.Println("ReviewChangeRequest::changeRequestID is empty")
		return true, nil
	}
	updated, crError := cmAdapter.cmAgent.ReviewChangeRequest(changeRequestID, comments)
	if crError != nil && cmAdapter.isMandatory() == false {
		log.Println("ReviewChangeRequest::IsMandatory is false", crError)
		return true, nil
	}
	return updated, crError
}

func (cmAdapter ChangeManagementAdapter) SignOffChangeRequest(changeRequestID, comments string) (bool, error) {
	if changeRequestID == "" {
		log.Println("SignOffChangeRequest::changeRequestID is empty")
		return true, nil
	}
	updated, crError := cmAdapter.cmAgent.SignOffChangeRequest(changeRequestID, comments)
	if crError != nil && cmAdapter.isMandatory() == false {
		log.Println("SignOffChangeRequest::IsMandatory is false", crError)
		return true, nil
	}
	return updated, crError
}

func (cmAdapter ChangeManagementAdapter) Validate() (bool, error) {
	if cmAdapter.isMandatory() == false {
		log.Println("Validate::IsMandatory is false")
		return true, nil
	}
	return cmAdapter.cmAgent.Validate()

}

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
