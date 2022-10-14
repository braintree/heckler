package response_util

import (
	"bytes"
	"encoding/json"
	"errors"
	cm_models "github.com/braintree/heckler/change_management/models"
	"os"
)

/*
writes Json string to a file
*/
func GetResponseJsonFromFile(outputFile string, outb bytes.Buffer) (cm_models.CMResponsePayLoad, error) {
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

func GetResponseJson(outputJson string) (cm_models.CMResponsePayLoad, error) {
	var emptyCMResponse cm_models.CMResponsePayLoad
	cmResponse := new(cm_models.CMResponsePayLoad)
	if outputJson == "" {
		return emptyCMResponse, errors.New("output is Empty")
	}
	err := json.Unmarshal([]byte(outputJson), cmResponse)
	return *cmResponse, err
}
