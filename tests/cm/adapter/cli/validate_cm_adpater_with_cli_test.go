package main

import (
	"fmt"
	cm_adapter "github.com/braintree/heckler/change_management/adapter"
	"log"
	"testing"
)

/*
This test validates cm_adapter.ChangeManagementAdapter (change_management/adapter/cm_adapter.go[cm_adapter.NewCMAdapter]) CLI application built using mockups/change_manage_system/cmd/mockup-cm-cli/main.go
cm_adapter.NewCMAdapter(cmAdapterConfig, nil)[interfaceManager is passed as nil]
cmAdapterConfig has a config with valid CLI CMD PATH[cmAdapterConfig["cli_agent_path"] = CLI_CMD_PATH]


cmAgent inside CMADapter is CLI binary application.
*/
const CLI_CMD_PATH = "../../../../mockup-cm-cli"
const TEST_DESC_PREFIX = "Heckler Testing for Tag::"
const TAG_VALUE = "v99"
const ENV_PREFIX = "MOCKTESTING"

var cmAdapter cm_adapter.ChangeManagementAdapter
var cmAdapterError error

func init() {

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	cmAdapterConfig := make(map[string]interface{})
	cmAdapterConfig["plugin_agent_path"] = ""
	cmAdapterConfig["cli_agent_path"] = CLI_CMD_PATH
	cmAdapterConfig["cm_config_path"] = "/etc/hecklerd/cm_config_conf.yaml"
	cmAdapterConfig["is_mandatory"] = true
	cmAdapterConfig["on_error_stop"] = true
	cmAdapterConfig["verbose"] = true
	log.Println("cmAdapterConfig...", cmAdapterConfig)
	cmAdapter, cmAdapterError = cm_adapter.NewCMAdapter(cmAdapterConfig, nil)
}
func getEnvPrefixFlag() string { return "-env_prefix=" + ENV_PREFIX }
func getTagFlag() string       { return "-tag=" + TAG_VALUE }
func getVerboseFlag(verbose bool) string {
	if verbose {
		return "-verbose=true"
	} else {
		return ""
	}
}
func getCommandFlag(command string) string       { return "-command=" + command }
func getOutputFileFlag(outputFile string) string { return "-output_file=" + outputFile }
func getChangeRequestIDFlag(changeRequestID string) string {
	return "-change_request_id=" + changeRequestID
}
func TestAllAPIS(t *testing.T) {
	fmt.Println("inside TestAllAPIS")
	if cmAdapterError != nil {
		t.Errorf(fmt.Sprintf("%v", cmAdapterError))
		return
	}
	var changeRequestID string

	log.Println("Going to invoke _createChangeRequest")
	checkInComments := "checkin comments by CM adapter test cases"
	changeRequestID, pauseExeuction := _CreateAndCheckInCR(t, checkInComments)
	if pauseExeuction == true {
		t.Errorf("got pauseExeuction as true from _CreateAndCheckInCR")
		return
	}
	if changeRequestID != "" {

		isCommented, commentError := _commentChangeRequest(t, changeRequestID)
		log.Println("isCommented", isCommented)
		if commentError != nil {
			t.Errorf(fmt.Sprintf("%v", commentError))
		}
		isSignOff, signOffError := _signOffChangeRequest(t, changeRequestID)
		log.Println("isSignOff", isSignOff)
		if signOffError != nil {
			t.Errorf(fmt.Sprintf("%v", signOffError))
		}
	}

}
func testCommentChangeRequest(t *testing.T, changeRequestID string) {
	isCommented, commentError := _commentChangeRequest(t, changeRequestID)
	log.Println("isCommented", isCommented)
	if commentError != nil {
		t.Errorf(fmt.Sprintf("%v", commentError))
	}
}

func _CreateAndCheckInCR(t *testing.T, checkInComments string) (string, bool) {
	fmt.Println("inside _CreateAndCheckInCR")
	if cmAdapterError != nil {
		t.Errorf(fmt.Sprintf("%v", cmAdapterError))
		return "", true
	}
	return cmAdapter.CreateAndCheckInCR(ENV_PREFIX, TAG_VALUE, checkInComments)
}

func _commentChangeRequest(t *testing.T, changeRequestID string) (bool, error) {
	fmt.Println("inside _commentChangeRequest")
	comments := "comments by cm adatper-cli"
	if cmAdapterError != nil {
		t.Errorf(fmt.Sprintf("%v", cmAdapterError))
		return false, cmAdapterError
	}
	return cmAdapter.CommentChangeRequest(changeRequestID, comments)
}
func _signOffChangeRequest(t *testing.T, changeRequestID string) (bool, error) {
	fmt.Println("inside _signOffChangeRequest")
	comments := "_signOffChangeRequest by cm adatper-cli"
	if cmAdapterError != nil {
		t.Errorf(fmt.Sprintf("%v", cmAdapterError))
		return false, cmAdapterError
	}
	return cmAdapter.SignOffChangeRequest(changeRequestID, comments)
}
