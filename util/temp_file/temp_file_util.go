package temp_file_util

import (
	"io/ioutil"
	"os"
)

func ReadFile(fileName string) ([]byte, error) {
	return os.ReadFile(fileName)

}
func GetTempJsonFile(prefix string) (*os.File, error) {
	// file, err := ioutil.TempFile("dir", "myname.*.yaml")
	jsonFile, fileErr := ioutil.TempFile("/tmp", prefix+".*.json")
	return jsonFile, fileErr
}
