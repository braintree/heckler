package snow_plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	cm_models "github.com/braintree/heckler/change_management/models"
	"log"
	"os"
	"os/exec"
)

const PATH_ERROR_CODE = -1
const LOCAL_RUNTIME_ERROR_CODE = -99

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
		return PATH_ERROR_CODE
	} else if err != nil {
		log.Println("Unknown Error, Not able to determine ExitCode,return ExitCode as -2")
		return -2
	} else {
		return 0
	}
}

func getResponseJson(outputFile string, outb bytes.Buffer) (cm_models.CMResponsePayLoad, error) {
	var responseJson string
	var emptyCMResponse cm_models.CMResponsePayLoad
	cmResponse := new(cm_models.CMResponsePayLoad)
	if outputFile == "" {
		responseJson = outb.String()
	} else {
		outputBytes, readError := os.ReadFile(outputFile)
		if readError == nil {
			responseJson = string(outputBytes)
		} else {
			return emptyCMResponse, readError
		}
	}

	err := json.Unmarshal([]byte(responseJson), cmResponse)
	return *cmResponse, err
}
