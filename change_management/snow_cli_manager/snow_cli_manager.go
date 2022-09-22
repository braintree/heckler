package snow_plugin

import (
	"errors"
	"log"
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
		log.Println("cliPath is empty hence using default Path::", DEFAULT_PLUGIN_PATH)
	}
	var emptySNowCLIManager SNowCLIManager
	log.Printf(" cliPath::%s \n", cliPath)
	exists, err := checkCliExists(cliPath)
	if err != nil || exists == false {

		if exists == false {
			msg := fmt.Sprintf("Invalid CLI Path::%s", cliPath)
			log.Println("error:msg", msg)
			err = errors.New(msg)
		} else {
			log.Printf("cli.Open error::%v \n", err)
		}
		return emptySNowCLIManager, err
	} else {
		log.Println("Cli is found", exists)
	}

	snowCLIMGR := SNowCLIManager{cliPath: cliPath}
	return snowCLIMGR, nil
}

func (snowCLIMGR SNowCLIManager) exec() {

}

func (snowCLIMGR SNowCLIManager) Validate() (bool, error) {
	return true, nil

}

func invokeSearchAndCreateCR() {
	log.Println("inside invokeSearchAndCreateCR")
	path, err := exec.LookPath(CLI_CMD_PATH)
	log.Println(path, err)
	var outb, errb bytes.Buffer
	cmd := exec.Command(CLI_CMD_PATH, "-command=SearchAndCreateChangeRequest", "-env=prod", "-tag=v123", "-verbose=true")
	cmd.Stdout = &outb
	cmd.Stderr = &errb
	err = cmd.Run()
	log.Println("output", outb.String())
	log.Println("Stderr...", cmd.Stderr)
	if err != nil {
		log.Println(err)
	}

}
func (snowCLIMGR SNowCLIManager) SearchChangeRequest(env, tag string) (string, error) {

	log.Println("inside invokeSearchChangeRequest")
	path, err := exec.LookPath(CLI_CMD_PATH)
	log.Println(path, err)
	var outb, errb bytes.Buffer
	cmd := exec.Command(CLI_CMD_PATH, "-command=SearchChangeRequest", "-env=prod", "-tag=v123", "-verbose=true")
	cmd.Stdout = &outb
	cmd.Stderr = &errb
	err = cmd.Run()
	log.Println("output", outb.String())
	log.Println("Stderr...", cmd.Stderr)
	if err != nil {
		log.Println(err)
	}
	return "", nil
}

func (snowCLIMGR SNowCLIManager) SearchAndCreateChangeRequest(env, tag string) (string, error) {

	log.Println("inside invokeSearchAndCreateChangeRequest")
	path, err := exec.LookPath(CLI_CMD_PATH)
	log.Println(path, err)
	var outb, errb bytes.Buffer
	cmd := exec.Command(CLI_CMD_PATH, "-command=SearchAndCreateChangeRequest", "-env=prod", "-tag=v123", "-verbose=true")
	cmd.Stdout = &outb
	cmd.Stderr = &errb
	err = cmd.Run()
	log.Println("output", outb.String())
	log.Println("Stderr...", cmd.Stderr)
	if err != nil {
		log.Println(err)
	}

	return "", nil
}

func (snowCLIMGR SNowCLIManager) CreateChangeRequest(env, tag string) (string, error) {

	log.Println("inside invokeCreateChangeRequest")
	path, err := exec.LookPath(CLI_CMD_PATH)
	log.Println(path, err)
	var outb, errb bytes.Buffer
	cmd := exec.Command(CLI_CMD_PATH, "-command=CreateChangeRequest", "-env=prod", "-tag=v123", "-verbose=true")
	cmd.Stdout = &outb
	cmd.Stderr = &errb
	err = cmd.Run()
	log.Println("output", outb.String())
	log.Println("Stderr...", cmd.Stderr)
	if err != nil {
		log.Println(err)
	}
	return "", nil
}
func (snowCLIMGR SNowCLIManager) GetChangeRequestDetails(changeRequestID string) (string, error) {

	log.Println("inside invokeGetChangeRequestDetails", changeRequestID)
	path, err := exec.LookPath(CLI_CMD_PATH)
	log.Println(path, err)
	var outb, errb bytes.Buffer
	cmd := exec.Command(CLI_CMD_PATH, "-command=GetChangeRequestDetails", "-change_request_id="+changeRequestID, "-env=prod", "-tag=v123", "-verbose=true")
	cmd.Stdout = &outb
	cmd.Stderr = &errb
	err = cmd.Run()
	log.Println("output", outb.String())
	log.Println("Stderr...", cmd.Stderr)
	if err != nil {
		log.Println(err)
	}
}

func (snowCLIMGR SNowCLIManager) CommentChangeRequest(changeRequestID, comment string) (bool, error) {

	changeRequestID := "CHG55856108"
	log.Println("inside invokeCommentChangeRequest", changeRequestID)
	path, err := exec.LookPath(CLI_CMD_PATH)
	log.Println(path, err)
	var outb, errb bytes.Buffer
	cmd := exec.Command(CLI_CMD_PATH, "-command=CommentChangeRequest", "-change_request_id="+changeRequestID, "-comments=testing using cli", "-env=prod", "-tag=v123", "-verbose=true")
	cmd.Stdout = &outb
	cmd.Stderr = &errb
	err = cmd.Run()
	log.Println("output", outb.String())
	log.Println("Stderr...", cmd.Stderr)
	if err != nil {
		log.Println(err)
	}
	return false, nil
}

func (snowCLIMGR SNowCLIManager) CheckInChangeRequest(changeRequestID string) (bool, error) {

	log.Println("inside invokeCheckInChangeRequest", changeRequestID)
	path, err := exec.LookPath(CLI_CMD_PATH)
	log.Println(path, err)
	var outb, errb bytes.Buffer
	cmd := exec.Command(CLI_CMD_PATH, "-command=CheckInChangeRequest", "-change_request_id="+changeRequestID, "-env=prod", "-tag=v123", "-verbose=true")
	cmd.Stdout = &outb
	cmd.Stderr = &errb
	err = cmd.Run()
	log.Println("output", outb.String())
	log.Println("Stderr...", cmd.Stderr)
	if err != nil {
		log.Println(err)
	}
	return false, nil
}
func (snowCLIMGR SNowCLIManager) SignOffChangeRequest(changeRequestID string) (bool, error) {

	log.Println("inside invokeSignOffChangeRequest", changeRequestID)
	path, err := exec.LookPath(CLI_CMD_PATH)
	log.Println(path, err)
	var outb, errb bytes.Buffer
	cmd := exec.Command(CLI_CMD_PATH, "-command=SignOffChangeRequest", "-change_request_id="+changeRequestID, "-env=prod", "-tag=v123", "-verbose=true")
	cmd.Stdout = &outb
	cmd.Stderr = &errb
	err = cmd.Run()
	log.Println("output", outb.String())
	log.Println("Stderr...", cmd.Stderr)
	if err != nil {
		log.Println(err)
	}
	return false, nil
}
