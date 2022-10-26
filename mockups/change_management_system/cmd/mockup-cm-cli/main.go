package main

import (
	"flag"
	"fmt"
	cm_cli_provider "github.com/braintree/heckler/mockups/change_management_system/provider/cli"
	"io/ioutil"
	"log"
	"os"
	"strings"
)

/*
mockup-cm-cli/main.go is used to build  CLI binary application.
Its wrapper on top of cm_cli_provider.MockupCMCLIProvider with flags and validate command and inputs passed.
Commands/Input fields are valid, it will call respective cm_cli_provider functions. Respective function will log output in stdout or to a file.
*/
func main() {

	var command string
	var changeRequestID string
	var outputFile string
	var envPrefix string
	var tag string
	var comments string
	var cmConfigPath string
	var verbose bool
	cmConfigPath = os.Getenv("CM_CONFIG_PATH")
	flag.StringVar(&command, "command", "", "Please provide command URL for mockup")
	flag.StringVar(&outputFile, "output_file", "", "Please provide output file path")
	flag.StringVar(&envPrefix, "env_prefix", "", "Please provide env")
	flag.StringVar(&tag, "tag", "", "Please provide tag")
	flag.StringVar(&comments, "comments", "", "Please provide comments")
	flag.StringVar(&changeRequestID, "change_request_id", "", "Please provide mockup change request id")
	flag.StringVar(&cmConfigPath, "cm_config_path", cmConfigPath, "Please provide cm config Path to override")
	flag.BoolVar(&verbose, "verbose", false, "Please provide verbose as true/false")

	flag.Parse()
	command = strings.Trim(command, " ")
	outputFile = strings.Trim(outputFile, " ")
	envPrefix = strings.Trim(envPrefix, " ")
	tag = strings.Trim(tag, " ")
	comments = strings.Trim(comments, " ")
	if verbose == false {
		log.SetOutput(ioutil.Discard)
	} else {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
		log.SetOutput(os.Stderr)
	}

	validateFlagsAndExit(command, envPrefix, tag, changeRequestID, comments)

	cliProvider, initError := cm_cli_provider.GetMockupCMCLIProvider(cmConfigPath)
	if initError != nil {
		log.Printf("Error GetBTCMManager err::%v", initError)
		os.Exit(1)
	}

	var response string
	var respError, outputError error
	switch command {
	case "SearchChangeRequest":
		response, respError, outputError = cliProvider.SearchChangeRequest(envPrefix, tag, outputFile)
	case "SearchAndCreateChangeRequest":
		response, respError, outputError = cliProvider.SearchAndCreateChangeRequest(envPrefix, tag, outputFile)
	case "CreateChangeRequest":
		response, respError, outputError = cliProvider.CreateChangeRequest(envPrefix, tag, outputFile)
	case "CommentChangeRequest":
		response, respError, outputError = cliProvider.CommentChangeRequest(changeRequestID, comments, outputFile)
	case "CheckInChangeRequest":
		response, respError, outputError = cliProvider.CheckInChangeRequest(changeRequestID, comments, outputFile)
	case "SignOffChangeRequest":
		response, respError, outputError = cliProvider.SignOffChangeRequest(changeRequestID, comments, outputFile)
	case "ReviewChangeRequest":
		response, respError, outputError = cliProvider.ReviewChangeRequest(changeRequestID, comments, outputFile)
	case "GetChangeRequestDetails":
		response, respError, outputError = cliProvider.GetChangeRequestDetails(changeRequestID, outputFile)

	case "Validate":
		response, respError, outputError = cliProvider.Validate(outputFile)
	}
	log.Println("response::", response, "respError::", respError, "outputError::", outputError)
	if respError != nil {
		os.Exit(2)
	} else if outputError != nil {
		os.Exit(3)
	} else if response == "" {
		os.Exit(1)
	} else {
		os.Exit(0)
	}

}
func validateFlagsAndExit(command, envPrefix, tag, changeRequestID, comments string) {
	switch command {
	case "":
		fmt.Println("command cannot be empty")
	case "Validate":
		fmt.Println("command is Validation ")
		return
	case "SearchChangeRequest", "SearchAndCreateChangeRequest", "CreateChangeRequest":
		if tag == "" {
			fmt.Printf("For command::%s, Env/Tag cannot be empty. env_prefix::'%s' and tag:: '%s'\n", command, envPrefix, tag)
		} else {
			return
		}
	case "GetChangeRequestDetails":
		if changeRequestID == "" {
			fmt.Printf("For command::%s, changeRequestID cannot be empty. changeRequestID::'%s'\n", command, changeRequestID)
		} else {
			return
		}

	case "CommentChangeRequest", "CheckInChangeRequest", "SignOffChangeRequest", "ReviewChangeRequest":
		if changeRequestID == "" || comments == "" {
			fmt.Printf("For command::%s, changeRequestID/comments cannot be empty. changeRequestID::'%s' and comments:: '%s'\n", command, changeRequestID, comments)
		} else {
			return
		}

	}

	fmt.Println("validation is failed and exiting")
	flag.Usage()
	os.Exit(1)
}
