package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"runtime/pprof"
	"sort"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig"
	"github.com/bradleyfalzon/ghinstallation"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-github/github"

	"gopkg.in/yaml.v3"
)

const GitHubEnterpriseURL = "https://github.braintreeps.com/api/v3"

var RegexDefineType = regexp.MustCompile(`^[A-Z][a-zA-Z0-9_:]*\[[^\]]+\]$`)
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
	//
	// Removing these for now, as it breaks grouping for resources that are
	// defined in diffent places in the source code.
	//
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
	//
	// Removing these for now, as it breaks grouping for resources that are
	// defined in diffent places in the source code.
	//
	// File             string   `yaml:"file"`
	// Line             int      `yaml:"line"`
	OutOfSync      bool   `yaml:"out_of_sync"`
	OutOfSyncCount int    `yaml:"out_of_sync_count"`
	ProviderUsed   string `yaml:"provider_used"`
	Resource       string `yaml:"resource"`
	ResourceType   string `yaml:"resource_type"`
	Skipped        bool   `yaml:"skipped"`
	// Tags           []string `yaml:"tags"`
	Title  string  `yaml:"title"`
	Events []Event `yaml:"events"`
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

type groupResource struct {
	Title      string
	Type       string
	DefineType string
	Diff       string
	Nodes      []string
	Events     []*groupEvent
	Logs       []*groupLog
}

type groupEvent struct {
	PreviousValue string
	DesiredValue  string
}

type groupLog struct {
	Level   string
	Message string
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

func groupResources(commit string, targetDeltaResource *deltaResource, nodes map[string]*Node, groupedCommits map[string][]*groupResource) {
	var nodeList []string
	var desiredValue string
	// XXX Remove this hack, only needed for old versions of puppet 4.5?
	var regexRubySym = regexp.MustCompile(`^:`)
	var gr *groupResource
	var ge *groupEvent
	var gl *groupLog

	for nodeName, node := range nodes {
		if nodeDeltaResource, ok := node.commitDeltaResources[commit][targetDeltaResource.Title]; ok {
			// fmt.Printf("grouping %v\n", targetDeltaResource.Title)
			if cmp.Equal(targetDeltaResource, nodeDeltaResource) {
				nodeList = append(nodeList, nodeName)
				delete(node.commitDeltaResources[commit], targetDeltaResource.Title)
			} else {
				// fmt.Printf("Diff:\n %v", cmp.Diff(targetDeltaResource, nodeDeltaResource))
			}
		}
	}

	gr = new(groupResource)
	gr.Title = targetDeltaResource.Title
	gr.Type = targetDeltaResource.Type
	gr.DefineType = targetDeltaResource.DefineType
	sort.Strings(nodeList)
	gr.Nodes = nodeList

	for _, e := range targetDeltaResource.Events {
		ge = new(groupEvent)

		ge.PreviousValue = regexRubySym.ReplaceAllString(e.PreviousValue, "")
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
		} else {
			desiredValue = regexRubySym.ReplaceAllString(e.DesiredValue, "")
		}
		ge.DesiredValue = desiredValue
		gr.Events = append(gr.Events, ge)
	}
	regexDiff := regexp.MustCompile(`^@@ `)
	for _, l := range targetDeltaResource.Logs {
		if regexDiff.MatchString(l.Message) {
			gr.Diff = strings.TrimSuffix(l.Message, "\n")
		} else {

			gl = new(groupLog)
			gl.Level = l.Level
			gl.Message = strings.TrimRight(l.Message, "\n")
			gr.Logs = append(gr.Logs, gl)
		}
	}
	groupedCommits[commit] = append(groupedCommits[commit], gr)
}

func printGroupResource(gr *groupResource) {
	fmt.Printf("Resource: %v\n", gr.Title)
	if gr.DefineType != "" {
		fmt.Printf("Define: %v\n", gr.DefineType)
	}
	fmt.Printf("Nodes: %v\n", gr.Nodes)
	for _, ge := range gr.Events {
		fmt.Printf("Current State: %v\n", ge.PreviousValue)
		fmt.Printf("Desired State: %v\n", ge.DesiredValue)
	}
	for _, gl := range gr.Logs {
		fmt.Printf("Log:\n%v\n", gl.Message)
	}
	fmt.Printf("\n")
}

func normalizeLogs(Logs []Log) []Log {
	var newSource string
	var origSource string
	var newLogs []Log

	// extract resource from log source
	regexResourcePropertyTail := regexp.MustCompile(`/[a-z][a-z0-9_]*$`)
	regexResourceTail := regexp.MustCompile(`[^\/]+\[[^\[\]]+\]$`)

	// normalize diff
	reFileContent := regexp.MustCompile(`File\[.*content$`)
	reDiff := regexp.MustCompile(`(?s)^.---`)

	// Log referring to a puppet resource
	regexResource := regexp.MustCompile(`^/Stage`)

	// Log msg values to drop
	regexCurValMsg := regexp.MustCompile(`^current_value`)
	regexApplyMsg := regexp.MustCompile(`^Applied catalog`)
	regexRefreshMsg := regexp.MustCompile(`^Would have triggered 'refresh'`)

	// Log sources to drop
	regexClass := regexp.MustCompile(`^Class\[`)
	regexStage := regexp.MustCompile(`^Stage\[`)

	for _, l := range Logs {
		origSource = ""
		newSource = ""
		if regexCurValMsg.MatchString(l.Message) ||
			regexApplyMsg.MatchString(l.Message) {
			if Debug {
				fmt.Fprintf(os.Stderr, "Dropping Log: %v: %v\n", l.Source, l.Message)
			}
			continue
		} else if regexClass.MatchString(l.Source) ||
			regexStage.MatchString(l.Source) ||
			RegexDefineType.MatchString(l.Source) {
			if Debug {
				fmt.Fprintf(os.Stderr, "Dropping Log: %v: %v\n", l.Source, l.Message)
			}
			continue
		} else if (!regexResource.MatchString(l.Source)) && regexRefreshMsg.MatchString(l.Message) {
			if Debug {
				fmt.Fprintf(os.Stderr, "Dropping Log: %v: %v\n", l.Source, l.Message)
			}
			continue
		} else if regexResource.MatchString(l.Source) {
			origSource = l.Source
			newSource = regexResourcePropertyTail.ReplaceAllString(l.Source, "")
			newSource = regexResourceTail.FindString(newSource)
			if newSource == "" {
				fmt.Fprintf(os.Stderr, "newSource is empty!\n")
				fmt.Fprintf(os.Stderr, "Log: '%v' -> '%v': %v\n", origSource, newSource, l.Message)
				os.Exit(1)
			}

			if reFileContent.MatchString(l.Source) && reDiff.MatchString(l.Message) {
				l.Message = normalizeDiff(l.Message)
			}
			l.Source = newSource
			if Debug {
				fmt.Fprintf(os.Stderr, "Adding Log: '%v' -> '%v': %v\n", origSource, newSource, l.Message)
			}
			newLogs = append(newLogs, l)
		} else {
			fmt.Fprintf(os.Stderr, "Unaccounted for Log: %v: %v\n", l.Source, l.Message)
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

func githubCreate(endTree string, commits []string, groupedCommits map[string][]*groupResource) {
	// Shared transport to reuse TCP connections.
	tr := http.DefaultTransport

	// Wrap the shared transport for use with the app ID 7 authenticating with
	// installation ID 11.
	itr, err := ghinstallation.NewKeyFromFile(tr, 7, 11, "heckler.2019-10-30.private-key.pem")
	if err != nil {
		log.Fatal(err)
	}
	itr.BaseURL = GitHubEnterpriseURL

	// Use installation transport with github.com/google/go-github
	client, err := github.NewEnterpriseClient(GitHubEnterpriseURL, GitHubEnterpriseURL, &http.Client{Transport: itr})
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	m := &github.Milestone{
		Title: github.String(endTree),
	}
	nm, _, err := client.Issues.CreateMilestone(ctx, "lollipopman", "muppetshow", m)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Successfully created new milestone: %v\n", *nm.Title)

	var c string
	var gc []*groupResource
	for i := 1; i < len(commits); i++ {
		c = commits[i]
		gc = groupedCommits[c]
		i := &github.IssueRequest{
			Title:     github.String(c),
			Assignee:  github.String("lollipopman"),
			Body:      github.String(groupResourcesToMarkdown(gc)),
			Milestone: nm.Number,
		}
		ni, _, err := client.Issues.Create(ctx, "lollipopman", "muppetshow", i)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Successfully created new issue: %v\n", *ni.Title)
	}
}

func groupResourcesToMarkdown(groupedResources []*groupResource) string {
	var body strings.Builder
	var err error

	tpl := template.Must(template.New("base").Funcs(sprig.TxtFuncMap()).ParseGlob("*.tmpl"))

	err = tpl.ExecuteTemplate(&body, "groupResource.tmpl", groupedResources)
	if err != nil {
		log.Fatal(err)
	}
	return body.String()
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
	var groupedCommits map[string][]*groupResource
	var cpuprofile = flag.String("cpuprofile", "", "write cpu profile to `file`")
	var memprofile = flag.String("memprofile", "", "write memory profile to `file`")
	var c string
	var gc []*groupResource

	flag.Var(&hosts, "node", "node hostnames to group")
	flag.StringVar(&reportDir, "report", "", "report dir")
	flag.StringVar(&puppetDir, "puppet", "", "puppet repo")
	flag.StringVar(&beginTree, "begin", "", "begin treeish")
	flag.StringVar(&endTree, "end", "", "end treeish")
	flag.BoolVar(&Debug, "debug", false, "enable debugging")
	flag.Parse()

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal("could not create CPU profile: ", err)
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Fatal("could not start CPU profile: ", err)
		}
		defer pprof.StopCPUProfile()
	}

	nodes = make(map[string]*Node)
	groupedCommits = make(map[string][]*groupResource)

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
			if err != nil {
				log.Fatalf("cannot read report: %v", err)
			}

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
		for _, node := range nodes {
			for _, r := range node.commitDeltaResources[commits[i]] {
				groupResources(commits[i], r, nodes, groupedCommits)
			}
		}
	}

	// print
	// for i := 1; i < len(commits); i++ {
	// 	c = commits[i]
	// 	fmt.Printf("\n# Commit %v: %v\n\n", i, c)
	// 	gc = groupedCommits[c]
	// 	sort.Slice(gc, func(i, j int) bool { return gc[i].Title < gc[j].Title })
	// 	for _, r := range gc {
	// 		printGroupResource(r)
	// 	}
	// }

	for i := 1; i < len(commits); i++ {
		c = commits[i]
		fmt.Printf("\n# Commit %v: %v\n\n", i, c)
		gc = groupedCommits[c]
		fmt.Printf("%s", groupResourcesToMarkdown(gc))
	}

	// GitHub
	githubCreate("v8", commits, groupedCommits)

	// cleanup
	if *memprofile != "" {
		f, err := os.Create(*memprofile)
		if err != nil {
			log.Fatal("could not create memory profile: ", err)
		}
		defer f.Close()
		runtime.GC() // get up-to-date statistics
		if err := pprof.WriteHeapProfile(f); err != nil {
			log.Fatal("could not write memory profile: ", err)
		}
	}
}
