package main

import (
	"fmt"
	cm_adapter "github.com/braintree/heckler/change_management/cm_adapter"
	cm_itfc "github.com/braintree/heckler/change_management/interfaces"
	"log"
	"testing"
)

const CLI_CMD_PATH = "/usr/local/bin/cli_cm_main"
const TEST_DESC_PREFIX = "Heckler Testing for Tag::"
const TAG_VALUE = "v99"
const ENV_PREFIX = "sandbox"

var cmAdapter cm_adapter.ChangeManagementAdapter
var cmAdapterError error

func init() {

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	// 	var emptyMGR cm_itfc.ChangeManagementInterface
	cmAdapterConfig := cm_itfc.ChangeManagementAdapterConfig{
		OnErrorStop:     true,
		IsMandatory:     true,
		CMConfigPath:    "",
		PluginAgentPath: "",
		CLIAgentPath:    "/usr/local/bin/cli_cm_main",
	}
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
	var changeRequestID, crDetails string
	var crError error
	changeRequestID, searchError := _SearchChangeRequest(t)
	if searchError != nil {
		t.Errorf(fmt.Sprintf("%v", searchError))
	}
	if searchError != nil || changeRequestID == "" {

		log.Println("Going to invoke _createChangeRequest")
		changeRequestID, crError = _createChangeRequest(t)
		if crError != nil {
			t.Errorf(fmt.Sprintf("%v", crError))
			return
		}
	}
	if changeRequestID != "" {
		crDetails, crError = _GetChangeRequestDetails(t, changeRequestID)
		if crError != nil && crDetails != "" {
			log.Println("crDetails..", crDetails)
		} else {
			log.Println("crDetails, crError ", crDetails, crError)
			if crError != nil {
				t.Errorf(fmt.Sprintf("%v", crError))
				return
			} else if crDetails == "" {
				t.Errorf("unable to fetch ChangeRequest Details")
				return
			}
		}

		isCommented, commentError := _commentChangeRequest(t, changeRequestID)
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
func testCommentChangeRequest(t *testing.T) {
	changeRequestID := "CHG55857622"
	isCommented, commentError := _commentChangeRequest(t, changeRequestID)
	log.Println("isCommented", isCommented)
	if commentError != nil {
		t.Errorf(fmt.Sprintf("%v", commentError))
	}
}
func _GetChangeRequestDetails(t *testing.T, changeRequestID string) (string, error) {
	fmt.Println("inside _GetChangeRequestDetails")

	return cmAdapter.GetChangeRequestDetails(changeRequestID)
}

/*
func _SearchAndCreateChangeRequest(t *testing.T) (string, error) {
	fmt.Println("inside _SearchAndCreateChangeRequest")
	if cmAdapterError != nil {
		t.Errorf(fmt.Sprintf("%v", cmAdapterError))
		return "", cmAdapterError
	}
	return cmAdapter.SearchAndCreateChangeRequest(ENV_PREFIX, TAG_VALUE)
}
*/
func _SearchChangeRequest(t *testing.T) (string, error) {
	fmt.Println("inside _SearchChangeRequest")
	if cmAdapterError != nil {
		t.Errorf(fmt.Sprintf("%v", cmAdapterError))
		return "", cmAdapterError
	}
	return cmAdapter.SearchChangeRequest(ENV_PREFIX, TAG_VALUE)
}
func _createChangeRequest(t *testing.T) (string, error) {
	fmt.Println("inside TestCommentChangeRequest")
	if cmAdapterError != nil {
		t.Errorf(fmt.Sprintf("%v", cmAdapterError))
		return "", cmAdapterError
	}
	return cmAdapter.CreateChangeRequest(ENV_PREFIX, TAG_VALUE)
}

func _commentChangeRequest(t *testing.T, changeRequestID string) (bool, error) {
	fmt.Println("inside TestCommentChangeRequest")
	comments := "comments by cm adatper-cli"
	if cmAdapterError != nil {
		t.Errorf(fmt.Sprintf("%v", cmAdapterError))
		return false, cmAdapterError
	}
	return cmAdapter.CommentChangeRequest(changeRequestID, comments)
}
func _checkInChangeRequest(t *testing.T, changeRequestID string) (bool, error) {
	fmt.Println("inside TestCommentChangeRequest")
	comments := "_checkInChangeRequest by cm adatper-cli"
	if cmAdapterError != nil {
		t.Errorf(fmt.Sprintf("%v", cmAdapterError))
		return false, cmAdapterError
	}
	return cmAdapter.CheckInChangeRequest(changeRequestID, comments)
}
func _signOffChangeRequest(t *testing.T, changeRequestID string) (bool, error) {
	fmt.Println("inside TestCommentChangeRequest")
	comments := "_signOffChangeRequest by cm adatper-cli"
	if cmAdapterError != nil {
		t.Errorf(fmt.Sprintf("%v", cmAdapterError))
		return false, cmAdapterError
	}
	return cmAdapter.SignOffChangeRequest(changeRequestID, comments)
}
