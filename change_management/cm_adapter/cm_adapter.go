package cm_adapter

import (
	"errors"
	cmIFC "github.com/braintree/heckler/change_management/interfaces"
	snow_cli_manager "github.com/braintree/heckler/change_management/snow_cli_manager"
	snow_plugin_manager "github.com/braintree/heckler/change_management/snow_plugin_manager"
	"log"
)

func getCMProvider(cmConfig cmIFC.ChangeManagementConfig, cmIFCMGR cmIFC.ChangeManagementInterface) (cmIFC.ChangeManagementInterface, error) {
	var emptyCMProvider cmIFC.ChangeManagementInterface
	var cmProvider cmIFC.ChangeManagementInterface
	var cmProviderError error
	if cmIFCMGR != nil {
		log.Println("cmIFCMGR is passed::", cmIFCMGR)
		cmProvider, cmProviderError = cmIFCMGR, nil

	}
	if cmConfig.SNOWCLIPath != "" {
		log.Println("SNOWCLIPath::", cmConfig.SNOWCLIPath)
		cmProvider, cmProviderError = snow_cli_manager.GetSNowCLIManager(cmConfig.SNOWCLIPath)
	}
	if cmConfig.SNOWPluginPath != "" {
		log.Println("SNOWPluginPath::", cmConfig.SNOWPluginPath)
		cmProvider, cmProviderError = snow_plugin_manager.GetSNowPluginManager(cmConfig.SNOWPluginPath)
	}
	if cmProviderError != nil {
		log.Println("cmProviderError::", cmProviderError)
		return emptyCMProvider, cmProviderError
	}
	isValidated, vError := cmProvider.Validate()
	if vError == nil {
		if isValidated {
			return cmProvider, nil
		} else {
			msg := fmt.Sprintf("cmProvider %t Validate returns %t", cmProvider, isValidated)
			log.Println()
			return emptyCMProvider, errors.New(msg)
		}
	} else {
		msg := fmt.Sprintf("cmProvider %t Not able to call Validate method %v", cmProvider, vError)
		log.Println(msg)
		return emptyCMProvider, errors.New(msg)
	}

}

type ChangeManagementAdapter struct {
	cmConfig   cmIFC.ChangeManagementConfig
	cmProvider cmIFC.ChangeManagementInterface
}

func NewCMAdapter(cmConfig cmIFC.ChangeManagementConfig, cmIFCMGR cmIFC.ChangeManagementInterface) (ChangeManagementAdapter, error) {
	var cmAdapter ChangeManagementAdapter
	provider, pError := getCMProvider(cmConfig, cmIFCMGR)
	if pError != nil {
		return cmAdapter, pError
	}
	cmAdapter = ChangeManagementAdapter{cmConfig: cmConfig, cmProvider: provider}
	return cmAdapter, nil

}
func (cmAdapter ChangeManagementAdapter) GetChangeRequestDetails(changeRequestID string) (string, error) {
	if changeRequestID == "" {
		fmt.Printf("GetChangeRequestDetails::changeRequestID is empty")
		return "", nil
	}
	crDetails, crError := cmAdapter.cmProvider.GetChangeRequestDetails(changeRequestID)
	if crError != nil && cmAdapter.cmConfig.IsMandatory == false {
		return "", nil
	}
	return crDetails, crError
}
func (cmAdapter ChangeManagementAdapter) SearchChangeRequest(gsnowEnv, tag string) (string, error) {

	crID, searchError := cmAdapter.cmProvider.SearchChangeRequest(gsnowEnv, tag)
	if searchError != nil && cmAdapter.cmConfig.IsMandatory == false {
		fmt.Printf("SearchChangeRequest::IsMandatory is false", searchError)
		return "", nil
	}
	return crID, searchError
}
func (cmAdapter ChangeManagementAdapter) SearchAndCreateChangeRequest(hecklerEnv, tag string) (string, error) {

	crID, searchError := cmAdapter.cmProvider.SearchAndCreateChangeRequest(hecklerEnv, tag)
	if searchError != nil && cmAdapter.cmConfig.IsMandatory == false {
		fmt.Printf("SearchAndCreateChangeRequest::IsMandatory is false", searchError)
		return "", nil
	}
	return crID, searchError
}
func (cmAdapter ChangeManagementAdapter) CreateChangeRequest(gsnowEnv, tag string) (string, error) {
	crID, crError := cmAdapter.cmProvider.CreateChangeRequest(gsnowEnv, tag)
	if crError != nil && cmAdapter.cmConfig.IsMandatory == false {
		fmt.Printf("CreateChangeRequest::IsMandatory is false", crError)
		return "", nil
	}
	return crID, crError

}
func (cmAdapter ChangeManagementAdapter) CommentChangeRequest(changeRequestID, comments string) (bool, error) {
	if changeRequestID == "" {
		fmt.Printf("CommentChangeRequest::changeRequestID is empty")
		return true, nil
	}
	updated, crError := cmAdapter.cmProvider.CommentChangeRequest(changeRequestID, comments)

	if crError != nil && cmAdapter.cmConfig.IsMandatory == false {
		fmt.Printf("CommentChangeRequest::IsMandatory is false", crError)
		return true, nil
	}
	return updated, crError
}
func (cmAdapter ChangeManagementAdapter) CheckInChangeRequest(changeRequestID string) (bool, error) {
	if changeRequestID == "" {
		fmt.Printf("CheckInChangeRequest::changeRequestID is empty")
		return true, nil
	}
	updated, crError := cmAdapter.cmProvider.CheckInChangeRequest(changeRequestID)
	if crError != nil && cmAdapter.cmConfig.IsMandatory == false {
		fmt.Printf("CheckInChangeRequest::IsMandatory is false", crError)
		return true, nil
	}
	return updated, crError
}
func (cmAdapter ChangeManagementAdapter) SignOffChangeRequest(changeRequestID string) (bool, error) {
	if changeRequestID == "" {
		fmt.Printf("SignOffChangeRequest::changeRequestID is empty")
		return true, nil
	}
	updated, crError := cmAdapter.cmProvider.SignOffChangeRequest(changeRequestID)
	if crError != nil && cmAdapter.cmConfig.IsMandatory == false {
		fmt.Printf("SignOffChangeRequest::IsMandatory is false", crError)
		return true, nil
	}
	return updated, crError
}

func (cmAdapter ChangeManagementAdapter) Validate() (bool, error) {
	if cmAdapter.cmConfig.IsMandatory == false {
		fmt.Printf("Validate::IsMandatory is false")
		return true, nil
	}
	return cmAdapter.cmProvider.Validate()

}

func (cmAdapter ChangeManagementAdapter) OnErrorFail() bool {
	if cmAdapter.cmConfig.IsMandatory == true {
		return true
	} else {
		fmt.Printf("OnErrorFail::IsMandatory is false so returning ONErrorFail returning configured value")

		return cmAdapter.cmConfig.OnErrorFail
	}

}

func (cmAdapter ChangeManagementAdapter) IsMandatory() bool {
	return cmAdapter.cmConfig.IsMandatory

}
