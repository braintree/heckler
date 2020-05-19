package puppetutil

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/lyraproj/puppet-parser/parser"
)

func NodeFileRegexes(nodeDir string) (map[string][]*regexp.Regexp, error) {
	fileToRegexes := make(map[string][]*regexp.Regexp)
	fileNames, err := filepath.Glob(nodeDir + "/*.pp")
	if err != nil {
		return nil, err
	}
	for _, fileName := range fileNames {
		regexes, err := parseNodeRegexes(fileName)
		if err != nil {
			return nil, err
		}
		fileToRegexes[fileName] = regexes
	}
	files := make([]string, 0, len(fileToRegexes))
	for file := range fileToRegexes {
		files = append(files, file)
	}
	sort.Strings(files)
	return fileToRegexes, nil
}

// parseNodeRegexes takes a Puppet file and uses the puppet parser to extract
// all the node regexes from the file, returning a slice of regexes
func parseNodeRegexes(fileName string) ([]*regexp.Regexp, error) {
	content, err := ioutil.ReadFile(fileName)
	if err != nil {
		return []*regexp.Regexp{}, err
	}
	expr, err := parser.CreateParser().Parse(fileName, string(content), false)
	if err != nil {
		return []*regexp.Regexp{}, err
	}
	// Parse the puppet code returning the data representation:
	// https://github.com/puppetlabs/puppet-specifications/blob/master/models/pn.md#pn-represented-as-data
	result := expr.ToPN().ToData().(map[string]interface{})
	nodes, err := findNestedCalls(result, "node", make([]interface{}, 0))
	if err != nil {
		return []*regexp.Regexp{}, err
	}
	nodeRegexes := make([]*regexp.Regexp, 0)
	for _, node := range nodes {
		regexes, err := findNodeRegexes(node.(map[string]interface{}))
		if err != nil {
			return []*regexp.Regexp{}, err
		}
		nodeRegexes = append(nodeRegexes, regexes...)
	}
	return nodeRegexes, nil
}

// Looks for a Puppet call named s in map m. findNestedCalls looks into the
// maps recursively and returns a result list of all the calls found. This
// function is based off of:
// https://eli.thegreenplace.net/2019/go-json-cookbook/
func findNestedCalls(m map[string]interface{}, s string, priorResults []interface{}) ([]interface{}, error) {
	var err error
	newResults := make([]interface{}, len(priorResults))
	copy(newResults, priorResults)
	for k, iv := range m {
		// Try to find call type at this level
		if k == "^" {
			if m, ok := iv.([]interface{}); ok {
				if m[0] == s {
					newResults = append(newResults, m[1])
				}
			}
		}
		// Not found on this level, so try to find it nested
		switch v := iv.(type) {
		case map[string]interface{}:
			newResults, err = findNestedCalls(v, s, newResults)
			if err != nil {
				return []interface{}{}, err
			}
		case []interface{}:
			for _, vv := range v {
				if m, ok := vv.(map[string]interface{}); ok {
					newResults, err = findNestedCalls(m, s, newResults)
					if err != nil {
						return []interface{}{}, err
					}
				}
			}
		default:
			return []interface{}{}, fmt.Errorf("Unknown type: %T\n", v)
		}
	}
	return newResults, nil
}

// Given a node call in Puppet return the list of regexes for the node
func findNodeRegexes(v map[string]interface{}) ([]*regexp.Regexp, error) {
	matches := v["#"].([]interface{})[1].([]interface{})
	regexes := make([]*regexp.Regexp, 0)
	for _, imatch := range matches {
		switch match := imatch.(type) {
		case string:
			// HACK: to convert equality match to regex
			regex := fmt.Sprintf("^%s", match)
			regexes = append(regexes, regexp.MustCompile(regex))
		case map[string]interface{}:
			foundRegexes, err := findNestedCalls(match, "regexp", make([]interface{}, 0))
			if err != nil {
				return []*regexp.Regexp{}, err
			}
			for _, iregex := range foundRegexes {
				// HACK: to ensure regex matches hostname, without needed to drop
				// subdomains as Puppet does
				regex := strings.TrimRight(iregex.(string), "$")
				regexes = append(regexes, regexp.MustCompile(regex))
			}
		default:
			return []*regexp.Regexp{}, fmt.Errorf("Unknown type: %T\n", v)
		}
	}
	return regexes, nil
}
