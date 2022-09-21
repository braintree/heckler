package snow_plugin

import (
	"errors"
	"fmt"
)

const DEFAULT_PLUGIN_PATH = "/usr/bin/hecklerd_snow_cli"

type SNowCLIManager struct {
	cliPath string
}

func checkCliExists(cliPath string) (bool, error) {
	return true, errors.New("Please implement checkCliExists function" + cliPath)
}
func GetSNowCLIManager(cliPath string) (SNowCLIManager, error) {
	if cliPath == "" {
		fmt.Println("cliPath is empty hence using default Path::", DEFAULT_PLUGIN_PATH)
	}
	var emptySNowCLIManager SNowCLIManager
	fmt.Printf(" cliPath::%s \n", cliPath)
	exists, err := checkCliExists(cliPath)
	if err != nil || exists == false {

		if exists == false {
			msg := fmt.Sprintf("Invalid CLI Path::%s", cliPath)
			fmt.Println("error:msg", msg)
			err = errors.New(msg)
		} else {
			fmt.Printf("cli.Open error::%v \n", err)
		}
		return emptySNowCLIManager, err
	} else {
		fmt.Println("Cli is found", exists)
	}

	snowCLIMGR := SNowCLIManager{cliPath: cliPath}
	return snowCLIMGR, nil
}

func (snowCLIMGR SNowCLIManager) exec() {

}
func (snowCLIMGR SNowCLIManager) SearchAndCreateChangeRequest(env, tag string) (string, error) {
	return "", nil
}
func (snowCLIMGR SNowCLIManager) SearchChangeRequest(env, tag string) (string, error) {
	return "", nil
}
func (snowCLIMGR SNowCLIManager) CreateChangeRequest(env, tag string) (string, error) {
	return "", nil
}
func (snowCLIMGR SNowCLIManager) GetChangeRequestDetails(changeRequestID string) (string, error) {
	return "", nil
}

func (snowCLIMGR SNowCLIManager) CommentChangeRequest(changeRequestID, comment string) (bool, error) {
	return false, nil
}

func (snowCLIMGR SNowCLIManager) CheckInChangeRequest(changeRequestID string) (bool, error) {

	return false, nil
}

func (snowCLIMGR SNowCLIManager) SignOffChangeRequest(changeRequestID string) (bool, error) {
	return false, nil

}

func (snowCLIMGR SNowCLIManager) Validate() (bool, error) {
	return false, nil

}
