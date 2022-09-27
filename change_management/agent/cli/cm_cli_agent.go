package snow_plugin

import (
	"bytes"
	"errors"
	"fmt"
	cm_interfaces "github.com/braintree/heckler/change_management/interfaces"
	temp_file_util "github.com/braintree/heckler/util/temp_file"
	"log"
	"os"
	"os/exec"
)

const DEFAULT_CLI_PATH = "/usr/bin/hecklerd_snow_cli"

type CMCLIAgent struct {
	cliPath string
	verbose bool
}

func GetCMCLIAgent(cliPath string, verbose bool) (CMCLIAgent, error) {
	if cliPath == "" {
		log.Println("cliPath is empty hence using default Path::", DEFAULT_CLI_PATH)
	}
	var emptyCMCLIAgent CMCLIAgent
	log.Printf(" cliPath::%s verbose::%t\n", cliPath, verbose)
	cmCLIAgent := CMCLIAgent{cliPath: cliPath, verbose: verbose}

	isValidated, err := cmCLIAgent.Validate()
	if err != nil || isValidated == false {

		if isValidated == false {
			msg := fmt.Sprintf("Unable  validate CLI ::%s", cliPath)
			log.Println("error:msg", msg)
			err = errors.New(msg)
		} else {
			log.Printf("cli.Open error::%v \n", err)
		}
		return emptyCMCLIAgent, err
	} else {
		log.Println("Cli is validated", isValidated)
	}

	return cmCLIAgent, nil
}

func (cmCLIAgent CMCLIAgent) process(arguments ...string) (int, cm_interfaces.CMResponsePayLoad, error) {
	fmt.Println("inside process")
	var emtpyResponse cm_interfaces.CMResponsePayLoad
	path, err := exec.LookPath(cmCLIAgent.cliPath)
	fmt.Println(path, err)
	var outputFile string
	jsonFile, fileError := temp_file_util.GetTempJsonFile("heckler_cm_cli_output")
	if fileError != nil {
		log.Println(fileError)
		return LOCAL_RUNTIME_ERROR_CODE, emtpyResponse, fileError
	} else {
		defer os.Remove(jsonFile.Name())
		outputFile = jsonFile.Name()
	}
	arguments = append(arguments, getOutputFileFlag(outputFile))
	arguments = append(arguments, getVerboseFlag(true))
	cmd := exec.Command(cmCLIAgent.cliPath, arguments...)
	// 	 getOutputFileFlag(outputFile), getVerboseFlag(true))
	// 	stdout, err := cmd.Output()
	var outb, errb bytes.Buffer
	cmd.Stdout = &outb
	cmd.Stderr = &errb
	err = cmd.Run()
	log.Println("output", outb.String())
	log.Println("Stderr...", cmd.Stderr)
	responseJson, responseError := getResponseJson(outputFile, outb)
	log.Println(responseJson, responseError)
	exitCode := getExitCodeOrPathError(err)
	log.Println("ExitCode::", exitCode)

	if exitCode == 0 {
		log.Println("exit code is zero,Call is success")
	} else if exitCode == 1 {
		log.Println("empty Response ERROR Code::", exitCode)
	} else if exitCode == 3 {
		log.Println("output generation ERROR Code::", exitCode)
	} else if exitCode == 2 {
		log.Println("GSNOW ERROR Code::", exitCode)
	} else if exitCode == -1 || exitCode == -2 {
		log.Println("Unknown Exist Code::", exitCode)
	}

	return exitCode, responseJson, responseError
}
func (cmCLIAgent CMCLIAgent) Validate() (bool, error) {
	arguments := []string{getCommandFlag("Validate")}
	exitCode, responseJson, responseError := cmCLIAgent.process(arguments...)
	if exitCode == 0 {
		log.Println("exit code is zero,Call is success")
		return true, nil
	} else {
		log.Println("responseJson, responseError", responseJson, responseError)
		return false, nil
	}
}

func (cmCLIAgent CMCLIAgent) SearchChangeRequest(envPrefix, tagValue string) (string, error) {

	log.Println("inside SearchChangeRequest", envPrefix, tagValue)
	arguments := []string{getCommandFlag("SearchChangeRequest"), getEnvPrefixFlag(envPrefix), getTagFlag(tagValue), getVerboseFlag(cmCLIAgent.verbose)}
	exitCode, responseJson, responseError := cmCLIAgent.process(arguments...)
	if exitCode == 0 {
		log.Println("exit code is zero,Call is success", responseJson.CRResult.ChangeRequestID)
		if responseJson.Status == cm_interfaces.STATUS_SUCCESS {
			return responseJson.CRResult.ChangeRequestID, nil
		} else {
			log.Println("responseJson.Error.Message..", responseJson.Error.Message)
			return "", errors.New(responseJson.Error.Message)
		}
	} else {
		log.Println("responseJson, responseError", responseJson, responseError)
		if responseJson.Status == cm_interfaces.STATUS_FAILURE {
			log.Println("responseJson.Error.Message..", responseJson.Error.Message)
			return "", errors.New(responseJson.Error.Message)
		} else {
			msg := fmt.Sprintf("Exit Code(%d) is not Zero", exitCode)
			log.Println("msg...", msg)
			return "", errors.New(msg)
		}
	}
}

func (cmCLIAgent CMCLIAgent) SearchAndCreateChangeRequest(envPrefix, tagValue string) (string, error) {

	log.Println("inside SearchAndCreateChangeRequest", envPrefix, tagValue)
	arguments := []string{getCommandFlag("SearchAndCreateChangeRequest"), getEnvPrefixFlag(envPrefix), getTagFlag(tagValue), getVerboseFlag(cmCLIAgent.verbose)}
	exitCode, responseJson, responseError := cmCLIAgent.process(arguments...)
	if exitCode == 0 {
		log.Println("exit code is zero,Call is success", responseJson.CRResult.ChangeRequestID)
		if responseJson.Status == cm_interfaces.STATUS_SUCCESS {
			return responseJson.CRResult.ChangeRequestID, nil
		} else {
			log.Println("responseJson.Error.Message..", responseJson.Error.Message)
			return "", errors.New(responseJson.Error.Message)
		}
	} else {
		log.Println("responseJson, responseError", responseJson, responseError)
		if responseJson.Status == cm_interfaces.STATUS_FAILURE {
			log.Println("responseJson.Error.Message..", responseJson.Error.Message)
			return "", errors.New(responseJson.Error.Message)
		} else {
			msg := fmt.Sprintf("Exit Code(%d) is not Zero", exitCode)
			log.Println("msg...", msg)
			return "", errors.New(msg)
		}
	}
}

func (cmCLIAgent CMCLIAgent) CreateChangeRequest(envPrefix, tagValue string) (string, error) {

	log.Println("inside CreateChangeRequest", envPrefix, tagValue)
	arguments := []string{getCommandFlag("CreateChangeRequest"), getEnvPrefixFlag(envPrefix), getTagFlag(tagValue), getVerboseFlag(cmCLIAgent.verbose)}
	exitCode, responseJson, responseError := cmCLIAgent.process(arguments...)
	if exitCode == 0 {
		log.Println("exit code is zero,Call is success", responseJson.CRResult.ChangeRequestID)
		if responseJson.Status == cm_interfaces.STATUS_SUCCESS {
			return responseJson.CRResult.ChangeRequestID, nil
		} else {
			log.Println("responseJson.Error.Message..", responseJson.Error.Message)
			return "", errors.New(responseJson.Error.Message)
		}
	} else {
		log.Println("responseJson, responseError", responseJson, responseError)
		if responseJson.Status == cm_interfaces.STATUS_FAILURE {
			log.Println("responseJson.Error.Message..", responseJson.Error.Message)
			return "", errors.New(responseJson.Error.Message)
		} else {
			msg := fmt.Sprintf("Exit Code(%d) is not Zero", exitCode)
			log.Println("msg...", msg)
			return "", errors.New(msg)
		}
	}
}
func (cmCLIAgent CMCLIAgent) GetChangeRequestDetails(changeRequestID string) (string, error) {

	log.Println("inside GetChangeRequestDetails", changeRequestID)

	arguments := []string{getCommandFlag("GetChangeRequestDetails"), getChangeRequestIDFlag(changeRequestID), getVerboseFlag(cmCLIAgent.verbose)}
	exitCode, responseJson, responseError := cmCLIAgent.process(arguments...)
	if exitCode == 0 {
		log.Println("exit code is zero,Call is success")
		if responseJson.Status == cm_interfaces.STATUS_SUCCESS {
			return responseJson.CRResult.CRDetails, nil
		} else {
			log.Println("responseJson.Error.Message..", responseJson.Error.Message)
			return "", errors.New(responseJson.Error.Message)
		}
	} else {
		log.Println("responseJson, responseError", responseJson, responseError)
		if responseJson.Status == cm_interfaces.STATUS_FAILURE {
			log.Println("responseJson.Error.Message..", responseJson.Error.Message)
			return "", errors.New(responseJson.Error.Message)
		} else {
			msg := fmt.Sprintf("Exit Code(%d) is not Zero", exitCode)
			log.Println("msg...", msg)
			return "", errors.New(msg)
		}
	}

}

func (cmCLIAgent CMCLIAgent) CommentChangeRequest(changeRequestID, comments string) (bool, error) {

	log.Println("inside CommentChangeRequest", changeRequestID, comments)

	arguments := []string{getCommandFlag("CommentChangeRequest"), getChangeRequestIDFlag(changeRequestID), getCommentsFlag(comments), getVerboseFlag(cmCLIAgent.verbose)}
	exitCode, responseJson, responseError := cmCLIAgent.process(arguments...)
	if exitCode == 0 {
		log.Println("exit code is zero,Call is success", responseJson.CRActionResult.Message)
		if responseJson.Status == cm_interfaces.STATUS_SUCCESS {
			return true, nil
		} else {
			log.Println("responseJson.Error.Message..", responseJson.Error.Message)
			return false, errors.New(responseJson.Error.Message)
		}
	} else {
		log.Println("responseJson, responseError", responseJson, responseError)
		if responseJson.Status == cm_interfaces.STATUS_FAILURE {
			log.Println("responseJson.Error.Message..", responseJson.Error.Message)
			return false, errors.New(responseJson.Error.Message)
		} else {
			msg := fmt.Sprintf("Exit Code(%d) is not Zero", exitCode)
			log.Println("msg...", msg)
			return false, errors.New(msg)
		}
	}

}

func (cmCLIAgent CMCLIAgent) CheckInChangeRequest(changeRequestID, comments string) (bool, error) {

	log.Println("inside CheckInChangeRequest", changeRequestID, comments)

	arguments := []string{getCommandFlag("CheckInChangeRequest"), getChangeRequestIDFlag(changeRequestID), getCommentsFlag(comments), getVerboseFlag(cmCLIAgent.verbose)}
	exitCode, responseJson, responseError := cmCLIAgent.process(arguments...)
	if exitCode == 0 {
		log.Println("exit code is zero,Call is success", responseJson.CRActionResult.Message)
		if responseJson.Status == cm_interfaces.STATUS_SUCCESS {
			return true, nil
		} else {
			log.Println("responseJson.Error.Message..", responseJson.Error.Message)
			return false, errors.New(responseJson.Error.Message)
		}
	} else {
		log.Println("responseJson, responseError", responseJson, responseError)
		if responseJson.Status == cm_interfaces.STATUS_FAILURE {
			log.Println("responseJson.Error.Message..", responseJson.Error.Message)
			return false, errors.New(responseJson.Error.Message)
		} else {
			msg := fmt.Sprintf("Exit Code(%d) is not Zero", exitCode)
			log.Println("msg...", msg)
			return false, errors.New(msg)
		}
	}

}

func (cmCLIAgent CMCLIAgent) ReviewChangeRequest(changeRequestID, comments string) (bool, error) {
	log.Println("inside ReviewChangeRequest", changeRequestID, comments)

	arguments := []string{getCommandFlag("ReviewChangeRequest"), getChangeRequestIDFlag(changeRequestID), getCommentsFlag(comments), getVerboseFlag(cmCLIAgent.verbose)}
	exitCode, responseJson, responseError := cmCLIAgent.process(arguments...)
	if exitCode == 0 {
		log.Println("exit code is zero,Call is success", responseJson.CRActionResult.Message)
		if responseJson.Status == cm_interfaces.STATUS_SUCCESS {
			return true, nil
		} else {
			log.Println("responseJson.Error.Message..", responseJson.Error.Message)
			return false, errors.New(responseJson.Error.Message)
		}
	} else {
		log.Println("responseJson, responseError", responseJson, responseError)
		if responseJson.Status == cm_interfaces.STATUS_FAILURE {
			log.Println("responseJson.Error.Message..", responseJson.Error.Message)
			return false, errors.New(responseJson.Error.Message)
		} else {
			msg := fmt.Sprintf("Exit Code(%d) is not Zero", exitCode)
			log.Println("msg...", msg)
			return false, errors.New(msg)
		}
	}
}
func (cmCLIAgent CMCLIAgent) SignOffChangeRequest(changeRequestID, comments string) (bool, error) {

	log.Println("inside SignOffChangeRequest", changeRequestID, comments)

	arguments := []string{getCommandFlag("SignOffChangeRequest"), getChangeRequestIDFlag(changeRequestID), getCommentsFlag(comments), getVerboseFlag(cmCLIAgent.verbose)}
	exitCode, responseJson, responseError := cmCLIAgent.process(arguments...)
	if exitCode == 0 {
		log.Println("exit code is zero,Call is success", responseJson.CRActionResult.Message)
		if responseJson.Status == cm_interfaces.STATUS_SUCCESS {
			return true, nil
		} else {
			log.Println("responseJson.Error.Message..", responseJson.Error.Message)
			return false, errors.New(responseJson.Error.Message)
		}
	} else {
		log.Println("responseJson, responseError", responseJson, responseError)
		if responseJson.Status == cm_interfaces.STATUS_FAILURE {
			log.Println("responseJson.Error.Message..", responseJson.Error.Message)
			return false, errors.New(responseJson.Error.Message)
		} else {
			msg := fmt.Sprintf("Exit Code(%d) is not Zero", exitCode)
			log.Println("msg...", msg)
			return false, errors.New(msg)
		}
	}
}

func getEnvPrefixFlag(envPrefix string) string   { return "-env_prefix=" + envPrefix }
func getTagFlag(tagValue string) string          { return "-tag=" + tagValue }
func getCommentsFlag(comments string) string     { return "-comments=" + comments }
func getCommandFlag(command string) string       { return "-command=" + command }
func getOutputFileFlag(outputFile string) string { return "-output_file=" + outputFile }
func getChangeRequestIDFlag(changeRequestID string) string {
	return "-change_request_id=" + changeRequestID
}
func getVerboseFlag(verbose bool) string {
	if verbose {
		return "-verbose=true"
	} else {
		return "-verbose=false"
	}
}
