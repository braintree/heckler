package main

import (
	"bytes"
	"errors"
	"fmt"
	cm_models "github.com/braintree/heckler/change_management/models"
	response_util "github.com/braintree/heckler/mockups/utils/response"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"testing"
)

/*
This test validates mockup-cm-cli binary CLI application built using mockups/change_manage_system/cmd/mockup-cm-cli/main.go
It uses cmd := exec.Command() and cmd.Run() to invoke this binary CLI application
*/

const CLI_CMD_PATH = "../../../mockup-cm-cli"
const TEST_DESC_PREFIX = "Heckler Testing for Tag::"
const TAG_VALUE = "v99"
const ENV_PREFIX = "MOCKTESTING"

func init() {

	log.SetFlags(log.LstdFlags | log.Lshortfile)
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
func getCommentsFlag(command string) string      { return "-comments=" + command }
func getOutputFileFlag(outputFile string) string { return "-output_file=" + outputFile }
func getChangeRequestIDFlag(changeRequestID string) string {
	return "-change_request_id=" + changeRequestID
}

func getExitCodeOrPathError(err error) int {
	var exitError *exec.ExitError
	var pathError *os.PathError
	if errors.As(err, &exitError) {
		if exitError.ExitCode() == 1 {
			return 1
		} else if exitError.ExitCode() == 2 {
			return 2
		} else {
			return exitError.ExitCode()
		}
	} else if errors.As(err, &pathError) {
		log.Println("pathError ,return ExitCode as -1", pathError)
		return -1
	} else if err != nil {
		log.Println("Unknown Error, Not able to determine ExitCode,return ExitCode as -2")
		return -2
	} else {
		return 0
	}
}

func verifyCLIPath(t *testing.T) error {
	log.Println("inside verifyCLIPath")
	_, err := exec.LookPath(CLI_CMD_PATH)
	if err != nil {
		t.Errorf(fmt.Sprintf("%t", err))
	}
	return err

}
func _getTempFile(t *testing.T) (*os.File, error) {

	jsonFile, fileError := ioutil.TempFile("/tmp", "validate_cli_cm_test_output"+".*.json")
	if fileError != nil {
		t.Errorf(fmt.Sprintf("%s", fileError))
	}
	return jsonFile, fileError
}

func testCreateCR(t *testing.T) {
	fmt.Println("inside TestCreateCR")
	if verifyCLIPath(t) != nil {
		return
	}
	var outputFile string
	jsonFile, fileError := _getTempFile(t)
	if fileError != nil {
		return
	}
	defer os.Remove(jsonFile.Name())
	outputFile = jsonFile.Name()

	cmd := exec.Command(CLI_CMD_PATH, getCommandFlag("CreateChangeRequest"), getEnvPrefixFlag(), getTagFlag(), getOutputFileFlag(outputFile), getVerboseFlag(true))
	// 	stdout, err := cmd.Output()
	var outb, errb bytes.Buffer
	cmd.Stdout = &outb
	cmd.Stderr = &errb
	err := cmd.Run()
	log.Println("output", outb.String())
	log.Println("Stderr...", cmd.Stderr)
	responseJson, responseError := response_util.GetResponseJsonFromFile(outputFile, outb)
	log.Println(responseJson, responseError)
	exitCode := getExitCodeOrPathError(err)
	log.Println("ExitCode::", exitCode)
	_checkExitCode(t, exitCode)

	_checkResponseJson(t, responseJson)

}
func _checkResponseJson(t *testing.T, responseJson cm_models.CMResponsePayLoad) {
	if responseJson.Status == cm_models.STATUS_FAILURE {
		log.Println("responseJson.Error...", responseJson.Error.Message, responseJson.Error.Detail)
		t.Errorf(responseJson.Error.Message)
		t.Errorf(responseJson.Error.Detail)
	}
}
func _checkExitCode(t *testing.T, exitCode int) {
	if exitCode == 0 {
		log.Println("exit code is zero,Call is success")
	} else if exitCode == 1 {
		t.Errorf("empty Response ERROR Code::%d", exitCode)
	} else if exitCode == 3 {
		t.Errorf("output generation ERROR Code::%d", exitCode)
	} else if exitCode == 2 {
		t.Errorf("CM ERROR Code::%d", exitCode)
	} else if exitCode == -1 || exitCode == -2 {
		t.Errorf("Unknown Exist Code::%d", exitCode)
	}
}
func TestAllmethods(t *testing.T) {
	if verifyCLIPath(t) != nil {
		return
	}
	changeRequestID, err := invokeCreateChangeRequest(t)
	if err != nil {
		t.Errorf(fmt.Sprintf("%t", err))
		return
	} else if changeRequestID == "" {
		t.Errorf("changeRequestID is none from invokeCheckInChangeRequest")
		return
	}
	log.Println("..invokeCommentChangeRequest with", changeRequestID)
	invokeCommentChangeRequest(t, changeRequestID)

	invokeCheckInChangeRequest(t, changeRequestID)
	invokeSignOffChangeRequest(t, changeRequestID)

}

func invokeCommand(t *testing.T, outputFileName string, arguments ...string) (cm_models.CMResponsePayLoad, error) {
	log.Println("inside invokeCommand")
	var outb, errb bytes.Buffer
	var emptyResponse cm_models.CMResponsePayLoad
	arguments = append(arguments, getOutputFileFlag(outputFileName))
	arguments = append(arguments, getVerboseFlag(true))

	cmd := exec.Command(CLI_CMD_PATH, arguments...)
	cmd.Stdout = &outb
	cmd.Stderr = &errb
	cmdErr := cmd.Run()
	log.Println("output", outb.String())
	log.Println("Stderr...", cmd.Stderr)
	if cmdErr != nil {
		t.Errorf(fmt.Sprintf("%t", cmdErr))

	}
	exitCode := getExitCodeOrPathError(cmdErr)
	log.Println("ExitCode::", exitCode)
	_checkExitCode(t, exitCode)
	if exitCode != 0 {
		return emptyResponse, cmdErr
	} else {
		responseJson, responseErr := response_util.GetResponseJsonFromFile(outputFileName, outb)
		log.Println(responseJson, responseErr)

		_checkResponseJson(t, responseJson)
		return responseJson, responseErr
	}

}
func invokeCreateChangeRequest(t *testing.T) (string, error) {
	var arguments []string
	arguments = append(arguments, getCommandFlag("CreateChangeRequest"))
	arguments = append(arguments, getEnvPrefixFlag())
	arguments = append(arguments, getTagFlag())
	jsonFile, fileError := _getTempFile(t)
	if fileError != nil {
		return "", fileError
	}
	defer os.Remove(jsonFile.Name())
	outputFileName := jsonFile.Name()

	responseJson, responseError := invokeCommand(t, outputFileName, arguments...)
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

func invokeCommentChangeRequest(t *testing.T, changeRequestID string) (string, error) {
	log.Println("inside invokeCommentChangeRequest", changeRequestID)

	var arguments []string
	arguments = append(arguments, getCommandFlag("CommentChangeRequest"))
	arguments = append(arguments, getEnvPrefixFlag())
	arguments = append(arguments, getTagFlag())
	arguments = append(arguments, getChangeRequestIDFlag(changeRequestID))
	arguments = append(arguments, getCommentsFlag("comments by invokeCommentChangeRequest by testing"))
	jsonFile, fileError := _getTempFile(t)
	if fileError != nil {
		return "", fileError
	}
	defer os.Remove(jsonFile.Name())
	outputFileName := jsonFile.Name()

	responseJson, responseError := invokeCommand(t, outputFileName, arguments...)
	if responseError != nil {
		return "", responseError
	} else {
		return responseJson.CRActionResult.Message, nil
	}
}

func invokeCheckInChangeRequest(t *testing.T, changeRequestID string) (string, error) {
	log.Println("inside invokeCheckInChangeRequest", changeRequestID)

	var arguments []string
	arguments = append(arguments, getCommandFlag("CheckInChangeRequest"))
	arguments = append(arguments, getEnvPrefixFlag())
	arguments = append(arguments, getTagFlag())
	arguments = append(arguments, getChangeRequestIDFlag(changeRequestID))
	arguments = append(arguments, getCommentsFlag("comments by invokeCommentChangeRequest by testing"))
	jsonFile, fileError := _getTempFile(t)
	if fileError != nil {
		return "", fileError
	}
	defer os.Remove(jsonFile.Name())
	outputFileName := jsonFile.Name()

	responseJson, responseError := invokeCommand(t, outputFileName, arguments...)
	if responseError != nil {
		return "", responseError
	} else {
		return responseJson.CRActionResult.Message, nil
	}
}

func invokeSignOffChangeRequest(t *testing.T, changeRequestID string) (string, error) {
	log.Println("inside invokeSignOffChangeRequest", changeRequestID)

	var arguments []string
	arguments = append(arguments, getCommandFlag("SignOffChangeRequest"))
	arguments = append(arguments, getEnvPrefixFlag())
	arguments = append(arguments, getTagFlag())
	arguments = append(arguments, getChangeRequestIDFlag(changeRequestID))
	arguments = append(arguments, getCommentsFlag("comments by invokeCommentChangeRequest by testing"))
	jsonFile, fileError := _getTempFile(t)
	if fileError != nil {
		return "", fileError
	}
	defer os.Remove(jsonFile.Name())
	outputFileName := jsonFile.Name()

	responseJson, responseError := invokeCommand(t, outputFileName, arguments...)
	if responseError != nil {
		return "", responseError
	} else {
		return responseJson.CRActionResult.Message, nil
	}
}
