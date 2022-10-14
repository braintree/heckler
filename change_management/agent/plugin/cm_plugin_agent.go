package snow_plugin

import (
	"fmt"
	"log"
	"plugin"
)

const DEFAULT_PLUGIN_PATH = "/etc/hecklerd/snow_plugin.so"

type CMPluginAgent struct {
	gsnowPlugin *plugin.Plugin
}

func GetCMPluginAgent(pluginPath string) (CMPluginAgent, error) {
	if pluginPath == "" {
		log.Println("pluginPath is empty hence using default Path::", DEFAULT_PLUGIN_PATH)
	}
	var emptyCMPluginAgent CMPluginAgent
	log.Printf(" plugin_path::%s \n", pluginPath)
	gsnowPlugin, err := plugin.Open(pluginPath)
	if err != nil {
		fmt.Printf("plugin.Open error::%v\n", err)
		return emptyCMPluginAgent, err
	}

	cmPluginAgent := CMPluginAgent{gsnowPlugin: gsnowPlugin}
	return cmPluginAgent, nil
}

func (cmPluginAgent CMPluginAgent) SearchAndCreateChangeRequest(env, tag string) (string, error) {
	f_SNCreateCR, f_SNCreateCRErr := cmPluginAgent.gsnowPlugin.Lookup("SearchAndCreateChangeRequestFunc")
	if f_SNCreateCRErr != nil {
		fmt.Printf("gsnowPlugin.Lookup for SearchAndCreateChangeRequestFunc error::%v\n", f_SNCreateCRErr)
		return "", f_SNCreateCRErr
	}

	changeRequestID, changeRequestError := f_SNCreateCR.(func(string, string) (string, error))(env, tag)
	log.Println("this is result from f_CreateCR", changeRequestID, changeRequestError)
	return changeRequestID, changeRequestError
}
func (cmPluginAgent CMPluginAgent) SearchChangeRequest(env, tag string) (string, error) {
	f_SCR, f_SErr := cmPluginAgent.gsnowPlugin.Lookup("SearchChangeRequestFunc")
	if f_SErr != nil {
		fmt.Printf("gsnowPlugin.Lookup for SearchChangeRequestFunc error::%v\n", f_SErr)
		return "", f_SErr
	}

	changeRequestID, changeRequestError := f_SCR.(func(string, string) (string, error))(env, tag)
	log.Println("this is result from f_SCR", changeRequestID, changeRequestError)
	return changeRequestID, changeRequestError
}
func (cmPluginAgent CMPluginAgent) CreateChangeRequest(env, tag string) (string, error) {
	f_CreateCR, f_CreateCRErr := cmPluginAgent.gsnowPlugin.Lookup("CreateChangeRequestFunc")
	if f_CreateCRErr != nil {
		fmt.Printf("gsnowPlugin.Lookup for CreateChangeRequestFunc error::%v\n", f_CreateCRErr)
		return "", f_CreateCRErr
	}

	changeRequestID, changeRequestError := f_CreateCR.(func(string, string) (string, error))(env, tag)
	log.Println("this is result from f_CreateCR", changeRequestID, changeRequestError)
	return changeRequestID, changeRequestError
}
func (cmPluginAgent CMPluginAgent) GetChangeRequestDetails(changeRequestID string) (string, error) {
	f_GetCR, f_GetCRErr := cmPluginAgent.gsnowPlugin.Lookup("GetChangeRequestDetailsFunc")
	if f_GetCRErr != nil {
		fmt.Printf("gsnowPlugin.Lookup for GetChangeRequestDetailsFunc error::%v\n", f_GetCRErr)
		return "", f_GetCRErr
	}
	crJson, crError := f_GetCR.(func(string) (string, error))(changeRequestID)
	log.Println("this is result from f_GetCR", crJson, crError)
	return crJson, crError
}

func (cmPluginAgent CMPluginAgent) CommentChangeRequest(changeRequestID, comment string) (bool, error) {
	f_CommentCR, f_CommentCRErr := cmPluginAgent.gsnowPlugin.Lookup("CommentChangeRequestFunc")
	if f_CommentCRErr != nil {
		fmt.Printf("gsnowPlugin.Lookup for CommentChangeRequestFunc error::%v\n", f_CommentCRErr)
		return false, f_CommentCRErr
	}
	isCommented, commentError := f_CommentCR.(func(string, string) (bool, error))(changeRequestID, comment)
	log.Println("this is result from f_CommentCR", isCommented, commentError)
	return isCommented, commentError

}

func (cmPluginAgent CMPluginAgent) CheckInChangeRequest(changeRequestID, comments string) (bool, error) {

	f_CheckinCR, f_CheckinCRError := cmPluginAgent.gsnowPlugin.Lookup("CheckInChangeRequestFunc")
	if f_CheckinCRError != nil {
		fmt.Printf("gsnowPlugin.Lookup for CheckInChangeRequestFunc error::%v\n", f_CheckinCRError)
		return false, f_CheckinCRError
	}
	isCheckedin, checkinError := f_CheckinCR.(func(string) (bool, error))(changeRequestID)
	log.Println("this is result from f_CheckinCR", isCheckedin, checkinError)
	return isCheckedin, checkinError

}

func (cmPluginAgent CMPluginAgent) ReviewChangeRequest(changeRequestID, comments string) (bool, error) {

	f_reviewCR, f_reviewCRError := cmPluginAgent.gsnowPlugin.Lookup("ReviewChangeRequestFunc")
	if f_reviewCRError != nil {
		fmt.Printf("gsnowPlugin.Lookup for ReviewChangeRequestFunc error::%v\n", f_reviewCRError)
		return false, f_reviewCRError
	}
	isReviewed, reviewError := f_reviewCR.(func(string, string) (bool, error))(changeRequestID, comments)
	log.Println("this is result from f_CheckinCR", isReviewed, reviewError)
	return isReviewed, reviewError

}
func (cmPluginAgent CMPluginAgent) SignOffChangeRequest(changeRequestID, comments string) (bool, error) {
	f_signoffCR, f_signoffCRerr := cmPluginAgent.gsnowPlugin.Lookup("SignOffChangeRequestFunc")
	if f_signoffCRerr != nil {
		fmt.Printf("gsnowPlugin.Lookup for SignOffChangeRequestFunc SignOffChangeRequestFunc::%v\n", f_signoffCRerr)
		return false, f_signoffCRerr
	}
	isSignedOff, signedError := f_signoffCR.(func(string, string) (bool, error))(changeRequestID, comments)
	log.Println("this is result from f_signoffCR", isSignedOff, signedError)
	return isSignedOff, signedError

}

func (cmPluginAgent CMPluginAgent) Validate() (bool, error) {
	f_validate, f_verr := cmPluginAgent.gsnowPlugin.Lookup("ValidateFunc")
	if f_verr != nil {
		fmt.Printf("gsnowPlugin.Lookup for SignOffChangeRequestFunc ValidateFunc::%v\n", f_verr)
		return false, f_verr
	}
	isValid, validError := f_validate.(func() (bool, error))()
	log.Println("this is result from f_validate", f_validate, validError)
	return isValid, validError

}
