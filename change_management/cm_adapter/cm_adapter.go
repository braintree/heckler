package cm_adapter

import (
	"errors"
	"fmt"
	cm_cli "github.com/braintree/heckler/change_management/agent/cli"
	cm_plugin "github.com/braintree/heckler/change_management/agent/plugin"
	cm_itfc "github.com/braintree/heckler/change_management/interfaces"
	"log"
)

func getCMAgent(adapterConfig cm_itfc.ChangeManagementAdapterConfig, cmITFCMGR cm_itfc.ChangeManagementInterface) (cm_itfc.ChangeManagementInterface, error) {
	var emptyCMProvider cm_itfc.ChangeManagementInterface
	var cmAgent cm_itfc.ChangeManagementInterface
	var cmAgentError error
	if cmITFCMGR != nil {
		log.Println("cmITFCMGR is passed::", cmITFCMGR)
		cmAgent, cmAgentError = cmITFCMGR, nil

	}
	if adapterConfig.CLIAgentPath != "" {
		log.Println("CLIAgentPath::", adapterConfig.CLIAgentPath)
		cmAgent, cmAgentError = cm_cli.GetCMCLIAgent(adapterConfig.CLIAgentPath, adapterConfig.Verbose)
	}
	if adapterConfig.PluginAgentPath != "" {
		log.Println("PluginAgentPath::", adapterConfig.PluginAgentPath)
		cmAgent, cmAgentError = cm_plugin.GetCMPluginAgent(adapterConfig.PluginAgentPath)
	}
	if cmAgentError != nil {
		log.Println("cmAgentError::", cmAgentError)
		return emptyCMProvider, cmAgentError
	}
	isValidated, vError := cmAgent.Validate()
	if vError == nil {
		if isValidated {
			return cmAgent, nil
		} else {
			msg := fmt.Sprintf("cmAgent %t Validate returns %t", cmAgent, isValidated)
			log.Println(msg)
			return emptyCMProvider, errors.New(msg)
		}
	} else {
		msg := fmt.Sprintf("cmAgent %t Not able to call Validate method %v", cmAgent, vError)
		log.Println(msg)
		return emptyCMProvider, errors.New(msg)
	}

}

type ChangeManagementAdapter struct {
	// 	cmConfig    cm_itfc.ChangeManagementConfig
	cmAgent       cm_itfc.ChangeManagementInterface
	adapterConfig cm_itfc.ChangeManagementAdapterConfig
}

func NewCMAdapter(adapterConfig cm_itfc.ChangeManagementAdapterConfig, cm_itfcMGR cm_itfc.ChangeManagementInterface) (ChangeManagementAdapter, error) {
	var cmAdapter ChangeManagementAdapter
	agent, pError := getCMAgent(adapterConfig, cm_itfcMGR)
	if pError != nil {
		return cmAdapter, pError
	}
	// 	var cmConfig cm_itfc.ChangeManagementConfig
	cmAdapter = ChangeManagementAdapter{adapterConfig: adapterConfig, cmAgent: agent}
	return cmAdapter, nil

}
func (cmAdapter ChangeManagementAdapter) GetChangeRequestDetails(changeRequestID string) (string, error) {
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
func (cmAdapter ChangeManagementAdapter) SearchChangeRequest(gsnowEnv, tag string) (string, error) {

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
func (cmAdapter ChangeManagementAdapter) CreateChangeRequest(gsnowEnv, tag string) (string, error) {
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
func (cmAdapter ChangeManagementAdapter) CheckInChangeRequest(changeRequestID, comments string) (bool, error) {
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

func (cmAdapter ChangeManagementAdapter) ReviewChangeRequest(changeRequestID, comments string) (bool, error) {
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
