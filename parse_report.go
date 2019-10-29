package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/google/go-cmp/cmp"

	"gopkg.in/yaml.v3"
)

var RegexDefineType = regexp.MustCompile(`^[A-Z][a-z0-9_]*::[A-Z][a-z0-9_]*\[[^\]]+\]$`)
var Debug = false

type PuppetReport struct {
	Host                 string                    `yaml:"host"`
	ConfigurationVersion int                       `yaml:"configuration_version"`
	ReportFormat         int                       `yaml:"report_format"`
	PuppetVersion        string                    `yaml:"puppet_version"`
	Status               string                    `yaml:"status"`
	TransactionCompleted bool                      `yaml:"transaction_completed"`
	Noop                 bool                      `yaml:"noop"`
	NoopPending          bool                      `yaml:"noop_pending"`
	Environment          string                    `yaml:"environment"`
	Logs                 []Log                     `yaml:"logs"`
	ResourceStatuses     map[string]ResourceStatus `yaml:"resource_statuses"`
	CorrectiveChange     bool                      `yaml:"corrective_change"`
	CachedCatalogStatus  string                    `yaml:"cached_catalog_status"`
}

type Log struct {
	Level   string `yaml:"level"`
	Message string `yaml:"message"`
	Source  string `yaml:"source"`
	// removing for now, as it breaks crunch
	// if resources are spread across source code
	//File    string `yaml:"file"`
	//Line    int    `yaml:"line"`
}

type Event struct {
	Property         string `yaml:"property"`
	PreviousValue    string `yaml:"previous_value"`
	DesiredValue     string `yaml:"desired_value"`
	Message          string `yaml:"message"`
	Name             string `yaml:"name"`
	Status           string `yaml:"status"`
	CorrectiveChange bool   `yaml:"corrective_change"`
}

type ResourceStatus struct {
	ChangeCount      int      `yaml:"change_count"`
	Changed          bool     `yaml:"changed"`
	ContainmentPath  []string `yaml:"containment_path"`
	CorrectiveChange bool     `yaml:"corrective_change"`
	Failed           bool     `yaml:"failed"`
	FailedToRestart  bool     `yaml:"failed_to_restart"`
	// removing for now, as it breaks crunch
	// if resources are spread across source code
	// File             string   `yaml:"file"`
	// Line             int      `yaml:"line"`
	OutOfSync      bool     `yaml:"out_of_sync"`
	OutOfSyncCount int      `yaml:"out_of_sync_count"`
	ProviderUsed   string   `yaml:"provider_used"`
	Resource       string   `yaml:"resource"`
	ResourceType   string   `yaml:"resource_type"`
	Skipped        bool     `yaml:"skipped"`
	Tags           []string `yaml:"tags"`
	Title          string   `yaml:"title"`
	Events         []Event  `yaml:"events"`
}

type Node struct {
	commitReports        map[string]*PuppetReport
	commitDeltaResources map[string]map[string]*deltaResource
}

type deltaResource struct {
	Title      string
	Type       string
	DefineType string
	Events     []Event
	Logs       []Log
}

func printSlice(s []string) {
	fmt.Printf("len=%d cap=%d %v\n", len(s), cap(s), s)
}

// 2. For each Node
//   1. Create minimal noop for commit by subtracting Node commit noop from
//      previous commit according to git history
//    map commit -> noop
//    map commitMin -> event_list
// 3. Collect all minimal minimal noops for a given commit
//    event list with count?
// 4. Compress noops, a la puppet crunch
// 5. Create Github issue against project version, include compressed noop output
// 6. Assign issue to authors of commit & team
// 7. Create a single issue for infrastructure for any nonaccounted for noop
//    outputs

// return list of commits as a sorted array
func commitList(repoDir string, beginTree string, endTree string) []string {
	var commits []string
	var s string

	//git log -n 1 --pretty=tformat:%h d1501a4
	// XXX dedup
	cmd := exec.Command("git", "log", "-n", "1", "--pretty=tformat:%h", beginTree)
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		log.Fatal(err)
	}
	r := bytes.NewReader(out)
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		s = scanner.Text()
		commits = append(commits, s)
	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}

	cmd = exec.Command("git", "log", "--no-merges", "--pretty=tformat:%h", "--reverse", "^"+beginTree, endTree)
	cmd.Dir = repoDir
	out, err = cmd.Output()
	if err != nil {
		log.Fatal(err)
	}
	r = bytes.NewReader(out)
	scanner = bufio.NewScanner(r)
	for scanner.Scan() {
		s = scanner.Text()
		commits = append(commits, s)
	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}

	return commits
}

func deltaNoop(priorCommitNoop *PuppetReport, commitNoop *PuppetReport) map[string]*deltaResource {
	var foundPrior bool
	var deltaEvents []Event
	var deltaLogs []Log
	var dr map[string]*deltaResource
	var partOfDefine bool
	var defineType string

	dr = make(map[string]*deltaResource)

	for resourceTitle, r := range commitNoop.ResourceStatuses {
		partOfDefine = false
		deltaEvents = nil
		deltaLogs = nil
		defineType = ""

		cplen := len(r.ContainmentPath)
		if cplen > 2 {
			possibleDefineType := r.ContainmentPath[cplen-2]
			if RegexDefineType.MatchString(possibleDefineType) {
				partOfDefine = true
				defineType = possibleDefineType
			}
		}

		for _, e := range r.Events {
			foundPrior = false
			for _, pe := range priorCommitNoop.ResourceStatuses[resourceTitle].Events {
				if e == pe {
					foundPrior = true
					break
				}
			}
			if foundPrior == false {
				deltaEvents = append(deltaEvents, e)
			}
		}

		for _, l := range commitNoop.Logs {
			if l.Source == resourceTitle {
				foundPrior = false
				for _, pl := range priorCommitNoop.Logs {
					if l == pl {
						foundPrior = true
						break
					}
				}
				if foundPrior == false {
					deltaLogs = append(deltaLogs, l)
				}
			}
		}

		if len(deltaEvents) > 0 || len(deltaLogs) > 0 {
			dr[resourceTitle] = new(deltaResource)
			dr[resourceTitle].Title = resourceTitle
			dr[resourceTitle].Type = r.ResourceType
			dr[resourceTitle].Events = deltaEvents
			dr[resourceTitle].Logs = deltaLogs
			if partOfDefine {
				dr[resourceTitle].DefineType = defineType
			}
		}
	}

	return dr
}

func crunch(commit string, targetDeltaResource *deltaResource, nodes map[string]*Node) {
	var nodeList []string
	var desiredValue string

	for nodeName, node := range nodes {
		if nodeDeltaResource, ok := node.commitDeltaResources[commit][targetDeltaResource.Title]; ok {
			// fmt.Printf("crunching %v\n", targetDeltaResource.Title)
			if cmp.Equal(targetDeltaResource, nodeDeltaResource) {
				nodeList = append(nodeList, nodeName)
				delete(node.commitDeltaResources[commit], targetDeltaResource.Title)
			} else {
				// fmt.Printf("Diff:\n %v", cmp.Diff(targetDeltaResource, nodeDeltaResource))
			}
		}
	}
	fmt.Printf("Resource: %v\n", targetDeltaResource.Title)
	if targetDeltaResource.DefineType != "" {
		fmt.Printf("Define: %v\n", targetDeltaResource.DefineType)
	}
	fmt.Printf("Nodes: %v\n", nodeList)
	for _, e := range targetDeltaResource.Events {
		fmt.Printf("Current State: %v\n", e.PreviousValue)
		// XXX move base64 decode somewhere else
		// also yell at puppet for this inconsistency!!!
		if targetDeltaResource.Type == "File" && e.Property == "content" {
			data, err := base64.StdEncoding.DecodeString(e.DesiredValue)
			if err != nil {
				// XXX nasty, fix?
				desiredValue = e.DesiredValue
			} else {
				desiredValue = string(data[:])
			}
			fmt.Printf("Desired State: %v\n", desiredValue)
		} else {
			fmt.Printf("Desired State: %v\n", e.DesiredValue)
		}
	}
	for _, l := range targetDeltaResource.Logs {
		fmt.Printf("Log:\n%v\n", strings.TrimRight(l.Message, "\n"))
	}
	fmt.Printf("\n")
}

func normalizeLogs(Logs []Log) []Log {
	var newSource string
	var newLogs []Log

	for _, l := range Logs {
		// Log referring to a puppet resource
		regexResource := regexp.MustCompile(`^/`)

		// Log msg values to drop
		regexCurValMsg := regexp.MustCompile(`^current_value`)
		regexApplyMsg := regexp.MustCompile(`^Applied catalog`)

		// Log sources to drop
		regexClass := regexp.MustCompile(`^Class\[`)
		regexStage := regexp.MustCompile(`^Stage\[`)

		if regexCurValMsg.MatchString(l.Message) ||
			regexApplyMsg.MatchString(l.Message) {
			continue
		} else if regexClass.MatchString(l.Source) ||
			regexStage.MatchString(l.Source) ||
			RegexDefineType.MatchString(l.Source) {
			continue
		} else if regexResource.MatchString(l.Source) {
			regexResourcePropertyTail := regexp.MustCompile(`/[^/]*$`)
			newSource = regexResourcePropertyTail.ReplaceAllString(l.Source, "")

			regexResource := regexp.MustCompile(`[^\/]+\[[^\[\]]+\]$`)
			newSource = regexResource.FindString(newSource)

			reFileContent := regexp.MustCompile(`File\[.*content$`)
			reDiff := regexp.MustCompile(`(?s)^.---`)
			if reFileContent.MatchString(l.Source) && reDiff.MatchString(l.Message) {
				l.Message = normalizeDiff(l.Message)
			}
			l.Source = newSource
			if Debug {
				fmt.Fprintf(os.Stderr, "log: \n%v\n", l)
			}
			newLogs = append(newLogs, l)
		} else {
			fmt.Printf("Unaccounted for log: %v\n", l.Message)
			fmt.Printf("Source: %v\n", l.Source)
			newLogs = append(newLogs, l)
		}
	}

	return newLogs
}

func normalizeDiff(msg string) string {
	var newMsg string
	var s string
	var line int

	scanner := bufio.NewScanner(strings.NewReader(msg))
	line = 0
	for scanner.Scan() {
		s = scanner.Text()
		if line > 2 {
			newMsg += s + "\n"
		}
		line++
	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}
	return newMsg
}

type hostFlags []string

func (i *hostFlags) String() string {
	return fmt.Sprint(*i)
}

func (i *hostFlags) Set(value string) error {
	*i = append(*i, value)
	return nil
}

func main() {
	var err error
	var file *os.File
	var data []byte
	var nodes map[string]*Node
	var hosts hostFlags
	var reportDir string
	var puppetDir string
	var beginTree string
	var endTree string

	flag.Var(&hosts, "node", "node hostnames to crunch")
	flag.StringVar(&reportDir, "report", "", "report dir")
	flag.StringVar(&puppetDir, "puppet", "", "puppet repo")
	flag.StringVar(&beginTree, "begin", "", "begin treeish")
	flag.StringVar(&endTree, "end", "", "end treeish")
	flag.BoolVar(&Debug, "debug", false, "enable debugging")
	flag.Parse()

	nodes = make(map[string]*Node)

	for _, node := range hosts {
		nodes[node] = new(Node)
	}

	commits := commitList(puppetDir, beginTree, endTree)
	for hostname, node := range nodes {
		node.commitReports = make(map[string]*PuppetReport)
		node.commitDeltaResources = make(map[string]map[string]*deltaResource)
		for i, commit := range commits {
			file, err = os.Open(reportDir + "/" + hostname + "/" + commit + ".yaml")
			if err != nil {
				log.Fatal(err)
			}
			defer file.Close()

			data, err = ioutil.ReadAll(file)

			node.commitReports[commit] = new(PuppetReport)
			err = yaml.Unmarshal([]byte(data), node.commitReports[commit])
			if err != nil {
				log.Fatalf("cannot unmarshal data: %v", err)
			}
			node.commitReports[commit].Logs = normalizeLogs(node.commitReports[commit].Logs)
			if i > 0 {
				node.commitDeltaResources[commit] = deltaNoop(node.commitReports[commits[i-1]], node.commitReports[commit])
			}
		}
	}

	for i := 1; i < len(commits); i++ {
		fmt.Printf("\n# Commit %v: %v\n\n", i, commits[i])
		for _, node := range nodes {
			for _, r := range node.commitDeltaResources[commits[i]] {
				crunch(commits[i], r, nodes)
			}
		}
	}
	os.Exit(0)
}
