package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"runtime/pprof"
	"sort"
	"strings"
	"text/template"
	"time"

	"./gitutil"
	"./puppetutil"
	"github.com/Masterminds/sprig"
	"github.com/bradleyfalzon/ghinstallation"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-github/github"
	git "github.com/libgit2/git2go"
	"google.golang.org/grpc"
)

var Debug = false
var RegexDefineType = regexp.MustCompile(`^[A-Z][a-zA-Z0-9_:]*\[[^\]]+\]$`)

const GitHubEnterpriseURL = "https://github.braintreeps.com/api/v3"

type hostFlags []string

type Node struct {
	host                 string
	commitReports        map[git.Oid]*puppetutil.PuppetReport
	commitDeltaResources map[git.Oid]map[ResourceTitle]*deltaResource
	rizzoClient          puppetutil.RizzoClient
}

type deltaResource struct {
	Title      ResourceTitle
	Type       string
	DefineType string
	Events     []*puppetutil.Event
	Logs       []*puppetutil.Log
}

type groupedResource struct {
	Title      ResourceTitle
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

type ResourceTitle string

func prettyPrint(i interface{}) string {
	s, _ := json.MarshalIndent(i, "", "\t")
	return string(s)
}

func normalizeLogs(Logs []*puppetutil.Log) []*puppetutil.Log {
	var newSource string
	var origSource string
	var newLogs []*puppetutil.Log

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

func commitToMarkdown(c *git.Commit) string {
	var body strings.Builder
	var err error

	// XXX better way to not duplicate this code?
	tpl := template.Must(template.New("base").Funcs(sprig.TxtFuncMap()).ParseGlob("*.tmpl"))

	err = tpl.ExecuteTemplate(&body, "commit.tmpl", c)
	if err != nil {
		log.Fatal(err)
	}
	return body.String()
}

func groupedResourcesToMarkdown(groupedResources []*groupedResource) string {
	var body strings.Builder
	var err error

	sort.Slice(groupedResources, func(i, j int) bool { return string(groupedResources[i].Title) < string(groupedResources[j].Title) })

	// XXX better way to not duplicate this code?
	tpl := template.Must(template.New("base").Funcs(sprig.TxtFuncMap()).ParseGlob("*.tmpl"))

	err = tpl.ExecuteTemplate(&body, "groupedResource.tmpl", groupedResources)
	if err != nil {
		log.Fatal(err)
	}
	return body.String()
}

func resourceDefineType(res *puppetutil.ResourceStatus) string {
	var defineType string

	cplen := len(res.ContainmentPath)
	if cplen > 2 {
		possibleDefineType := res.ContainmentPath[cplen-2]
		if RegexDefineType.MatchString(possibleDefineType) {
			defineType = possibleDefineType
		}
	}
	return defineType
}

func priorEvent(event *puppetutil.Event, resourceTitleStr string, priorCommitNoops []*puppetutil.PuppetReport) bool {
	for _, priorCommitNoop := range priorCommitNoops {
		if priorResourceStatuses, ok := priorCommitNoop.ResourceStatuses[resourceTitleStr]; ok {
			for _, priorEvent := range priorResourceStatuses.Events {
				if *event == *priorEvent {
					return true
				}
			}
		}
	}
	return false
}

func priorLog(log *puppetutil.Log, priorCommitNoops []*puppetutil.PuppetReport) bool {
	for _, priorCommitNoop := range priorCommitNoops {
		for _, priorLog := range priorCommitNoop.Logs {
			if *log == *priorLog {
				return true
			}
		}
	}
	return false
}

func initDeltaResource(resourceTitle ResourceTitle, r *puppetutil.ResourceStatus, deltaEvents []*puppetutil.Event, deltaLogs []*puppetutil.Log) *deltaResource {
	deltaRes := new(deltaResource)
	deltaRes.Title = resourceTitle
	deltaRes.Type = r.ResourceType
	deltaRes.Events = deltaEvents
	deltaRes.Logs = deltaLogs
	deltaRes.DefineType = resourceDefineType(r)
	return deltaRes
}

func deltaNoop(commitNoop *puppetutil.PuppetReport, priorCommitNoops []*puppetutil.PuppetReport) map[ResourceTitle]*deltaResource {
	var deltaEvents []*puppetutil.Event
	var deltaLogs []*puppetutil.Log
	var deltaResources map[ResourceTitle]*deltaResource
	var resourceTitle ResourceTitle

	deltaResources = make(map[ResourceTitle]*deltaResource)

	for resourceTitleStr, r := range commitNoop.ResourceStatuses {
		deltaEvents = nil
		deltaLogs = nil

		for _, event := range r.Events {
			if priorEvent(event, resourceTitleStr, priorCommitNoops) == false {
				deltaEvents = append(deltaEvents, event)
			}
		}

		for _, log := range commitNoop.Logs {
			if log.Source == resourceTitleStr {
				if priorLog(log, priorCommitNoops) == false {
					deltaLogs = append(deltaLogs, log)
				}
			}
		}

		if len(deltaEvents) > 0 || len(deltaLogs) > 0 {
			resourceTitle = ResourceTitle(resourceTitleStr)
			deltaResources[resourceTitle] = initDeltaResource(resourceTitle, r, deltaEvents, deltaLogs)
		}
	}

	return deltaResources
}

func groupResources(commitLogId git.Oid, targetDeltaResource *deltaResource, nodes map[string]*Node) *groupedResource {
	var nodeList []string
	var desiredValue string
	// XXX Remove this hack, only needed for old versions of puppet 4.5?
	var regexRubySym = regexp.MustCompile(`^:`)
	var gr *groupedResource
	var ge *groupEvent
	var gl *groupLog

	for nodeName, node := range nodes {
		if nodeDeltaResource, ok := node.commitDeltaResources[commitLogId][targetDeltaResource.Title]; ok {
			if cmp.Equal(targetDeltaResource, nodeDeltaResource) {
				nodeList = append(nodeList, nodeName)
				delete(node.commitDeltaResources[commitLogId], targetDeltaResource.Title)
			}
		}
	}

	gr = new(groupedResource)
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
	log.Printf("Appending groupedResource %v\n", gr.Title)
	return gr
}

func (i *hostFlags) String() string {
	return fmt.Sprint(*i)
}

func (i *hostFlags) Set(value string) error {
	*i = append(*i, value)
	return nil
}

func fetchRepo() (*git.Repository, error) {

	// Shared transport to reuse TCP connections.
	tr := http.DefaultTransport

	// Wrap the shared transport for use with the app ID 7 authenticating with
	// installation ID 11.
	itr, err := ghinstallation.NewKeyFromFile(tr, 7, 11, "heckler.2019-10-30.private-key.pem")
	if err != nil {
		return nil, err
	}
	itr.BaseURL = GitHubEnterpriseURL

	// Use installation transport with github.com/google/go-github
	_, err = github.NewEnterpriseClient(GitHubEnterpriseURL, GitHubEnterpriseURL, &http.Client{Transport: itr})
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	tok, err := itr.Token(ctx)
	if err != nil {
		return nil, err
	}

	cloneDir := "/data/muppetshow"
	cloneOptions := &git.CloneOptions{}
	remoteUrl := fmt.Sprintf("https://x-access-token:%s@github.braintreeps.com/lollipopman/muppetshow", tok)
	repo, err := gitutil.CloneOrOpen(remoteUrl, cloneDir, cloneOptions)
	if err != nil {
		return nil, err
	}
	err = gitutil.FastForward(repo, nil)
	if err != nil {
		return nil, err
	}
	return repo, nil
}

func hecklerApply(rc puppetutil.RizzoClient, c chan<- puppetutil.PuppetReport, par puppetutil.PuppetApplyRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*300)
	defer cancel()
	r, err := rc.PuppetApply(ctx, &par)
	if err != nil {
		c <- puppetutil.PuppetReport{}
	}
	c <- *r
}

func grpcConnect(node *Node, clientConnChan chan *Node) {
	var conn *grpc.ClientConn
	address := node.host + ":50051"
	log.Printf("Dialing: %v", node.host)
	conn, err := grpc.Dial(address, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Fatalf("Unable to connect to: %v, %v", node.host, err)
	}
	log.Printf("Connected: %v", node.host)
	node.rizzoClient = puppetutil.NewRizzoClient(conn)
	clientConnChan <- node
}

func commitLogIdList(repo *git.Repository, beginRev string, endRev string) ([]git.Oid, map[git.Oid]*git.Commit, error) {
	var commitLogIds []git.Oid
	var commits map[git.Oid]*git.Commit

	commits = make(map[git.Oid]*git.Commit)

	log.Printf("Walk begun: %s..%s\n", beginRev, endRev)
	rv, err := repo.Walk()
	if err != nil {
		return nil, nil, err
	}

	// We what to sort by the topology of the date of the commits. Also, reverse
	// the sort so the first commit in the array is the earliest commit or oldest
	// ancestor in the topology.
	rv.Sorting(git.SortTopological | git.SortReverse)

	// XXX only tags???
	err = rv.PushRef("refs/tags/" + endRev)
	if err != nil {
		return nil, nil, err
	}
	err = rv.HideRef("refs/tags/" + beginRev)
	if err != nil {
		return nil, nil, err
	}

	var c *git.Commit
	var gi git.Oid
	for rv.Next(&gi) == nil {
		commitLogIds = append(commitLogIds, gi)
		c, err = repo.LookupCommit(&gi)
		if err != nil {
			return nil, nil, err
		}
		commits[gi] = c
		log.Printf("commit: %s\n", gi.String())
	}
	log.Printf("Walk Complete\n")

	return commitLogIds, commits, nil
}

func githubCreate(githubMilestone string, commitLogIds []git.Oid, groupedCommits map[git.Oid][]*groupedResource, commits map[git.Oid]*git.Commit) {
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
		Title: github.String(githubMilestone),
	}
	nm, _, err := client.Issues.CreateMilestone(ctx, "lollipopman", "muppetshow", m)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Successfully created new milestone: %v\n", *nm.Title)

	for _, gi := range commitLogIds {
		if len(groupedCommits[gi]) == 0 {
			log.Printf("Skipping %s, no noop output\n", gi.String())
			continue
		}
		githubIssue := &github.IssueRequest{
			Title:     github.String(fmt.Sprintf("Puppet noop output for commit: '%v'", commits[gi].Summary())),
			Assignee:  github.String(commits[gi].Author().Name),
			Body:      github.String(commitToMarkdown(commits[gi]) + groupedResourcesToMarkdown(groupedCommits[gi])),
			Milestone: nm.Number,
		}
		ni, _, err := client.Issues.Create(ctx, "lollipopman", "muppetshow", githubIssue)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Successfully created new issue: %v\n", *ni.Title)
	}
}

func commitParentReports(commit *git.Commit, commitReports map[git.Oid]*puppetutil.PuppetReport) []*puppetutil.PuppetReport {
	var parentReports []*puppetutil.PuppetReport

	parentCount := commit.ParentCount()
	for i := uint(0); i < parentCount; i++ {
		parentReports = append(parentReports, commitReports[*commit.ParentId(i)])
	}
	return parentReports
}

func markdownOutput(commitLogIds []git.Oid, commits map[git.Oid]*git.Commit, groupedCommits map[git.Oid][]*groupedResource) {
	for _, gi := range commitLogIds {
		if len(groupedCommits[gi]) == 0 {
			log.Printf("Skipping %s, no noop output\n", gi.String())
			continue
		}
		fmt.Printf("## Puppet noop output for commit: '%v'\n\n", commits[gi].Summary())
		fmt.Printf("%s", commitToMarkdown(commits[gi]))
		fmt.Printf("%s", groupedResourcesToMarkdown(groupedCommits[gi]))
	}
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	var hosts hostFlags
	var beginRev string
	var endRev string
	var rev string
	var noop bool
	var markdownOut bool
	var githubMilestone string
	var data []byte
	var nodes map[string]*Node
	var puppetReportChan chan puppetutil.PuppetReport
	var node *Node

	puppetReportChan = make(chan puppetutil.PuppetReport)

	var cpuprofile = flag.String("cpuprofile", "", "write cpu profile to `file`")
	var memprofile = flag.String("memprofile", "", "write memory profile to `file`")
	flag.Var(&hosts, "node", "node hostnames to group")
	flag.StringVar(&beginRev, "beginrev", "", "begin rev")
	flag.StringVar(&endRev, "endrev", "", "end rev")
	flag.StringVar(&rev, "rev", "", "rev to apply or noop")
	flag.BoolVar(&noop, "noop", false, "noop")
	flag.BoolVar(&markdownOut, "md", false, "Generate markdown output")
	flag.StringVar(&githubMilestone, "github", "", "Github milestone to create")
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

	if rev != "" && (beginRev != "" || endRev != "") {
		fmt.Printf("The -rev flag cannot be combined with the -beginrev or the -endrev\n")
		flag.Usage()
		os.Exit(1)
	}

	if len(hosts) == 0 {
		fmt.Printf("ERROR: You must supply one or more nodes\n")
		flag.Usage()
		os.Exit(1)
	}

	repo, err := fetchRepo()
	if err != nil {
		log.Fatalf("Unable to fetch repo: %v", err)
	}

	var clientConnChan chan *Node
	clientConnChan = make(chan *Node)

	nodes = make(map[string]*Node)
	for _, host := range hosts {
		nodes[host] = new(Node)
		nodes[host].host = host
	}

	for _, node := range nodes {
		go grpcConnect(node, clientConnChan)
	}

	for range nodes {
		node = <-clientConnChan
		log.Printf("Conn %s\n", node.host)
		nodes[node.host] = node
	}

	if rev != "" {
		par := puppetutil.PuppetApplyRequest{Rev: rev, Noop: noop}
		for _, node := range nodes {
			go hecklerApply(node.rizzoClient, puppetReportChan, par)
		}

		for range hosts {
			r := <-puppetReportChan
			log.Printf("Applied: %s@%s", r.Host, r.ConfigurationVersion)
		}
		os.Exit(0)
	}

	if beginRev == "" || endRev == "" {
		fmt.Printf("ERROR: You must supply -beginrev & -endrev or -rev\n")
		flag.Usage()
		os.Exit(1)
	}

	// Make dir structure
	// e.g. /var/heckler/v1..v2//oid.json

	revdir := fmt.Sprintf("/var/heckler/%s..%s", beginRev, endRev)

	os.MkdirAll(revdir, 077)
	for host, _ := range nodes {
		os.Mkdir(revdir+"/"+host, 077)
	}

	var groupedCommits map[git.Oid][]*groupedResource

	groupedCommits = make(map[git.Oid][]*groupedResource)

	// XXX Should or can this be done in new(Node)?
	for _, node := range nodes {
		node.commitReports = make(map[git.Oid]*puppetutil.PuppetReport)
		node.commitDeltaResources = make(map[git.Oid]map[ResourceTitle]*deltaResource)
	}

	commitLogIds, commits, err := commitLogIdList(repo, beginRev, endRev)
	if err != nil {
		log.Fatalf("Unable to get commit id list", err)
	}

	var noopRequests int
	var reportPath string
	var file *os.File
	var rprt *puppetutil.PuppetReport

	for i, commitLogId := range commitLogIds {
		log.Printf("Nooping: %s (%d of %d)", commitLogId.String(), i, len(commitLogIds))
		par := puppetutil.PuppetApplyRequest{Rev: commitLogId.String(), Noop: true}
		noopRequests = 0
		for host, node := range nodes {
			reportPath = revdir + "/" + host + "/" + commitLogId.String() + ".json"
			if _, err := os.Stat(reportPath); err == nil {
				file, err = os.Open(reportPath)
				if err != nil {
					log.Fatal(err)
				}
				defer file.Close()

				data, err = ioutil.ReadAll(file)
				if err != nil {
					log.Fatalf("cannot read report: %v", err)
				}
				rprt = new(puppetutil.PuppetReport)
				err = json.Unmarshal([]byte(data), rprt)
				if err != nil {
					log.Fatalf("cannot unmarshal report: %v", err)
				}
				if host != rprt.Host {
					log.Fatalf("Host mismatch %s != %s", host, rprt.Host)
				}
				log.Printf("Found serialized noop: %s@%s", rprt.Host, rprt.ConfigurationVersion)
				nodes[rprt.Host].commitReports[commitLogId] = rprt
			} else {
				go hecklerApply(node.rizzoClient, puppetReportChan, par)
				noopRequests++
			}
		}

		for j := 0; j < noopRequests; j++ {
			rprt := <-puppetReportChan
			log.Printf("Received noop: %s@%s", rprt.Host, rprt.ConfigurationVersion)
			nodes[rprt.Host].commitReports[commitLogId] = &rprt
			nodes[rprt.Host].commitReports[commitLogId].Logs = normalizeLogs(nodes[rprt.Host].commitReports[commitLogId].Logs)

			reportPath = revdir + "/" + rprt.Host + "/" + commitLogId.String() + ".json"
			data, err = json.Marshal(rprt)
			if err != nil {
				log.Fatalf("Cannot marshal report: %v", err)
			}
			err = ioutil.WriteFile(reportPath, data, 0644)
			if err != nil {
				log.Fatalf("Cannot write report: %v", err)
			}

		}
	}

	for host, node := range nodes {
		for i, gi := range commitLogIds {
			if i == 0 {
				node.commitDeltaResources[gi] = deltaNoop(node.commitReports[gi], []*puppetutil.PuppetReport{new(puppetutil.PuppetReport)})
			} else {
				log.Printf("Creating delta resource: %s@%s", host, gi.String())
				node.commitDeltaResources[gi] = deltaNoop(node.commitReports[gi], commitParentReports(commits[gi], node.commitReports))
			}
		}
	}

	for _, gi := range commitLogIds {
		log.Printf("Grouping: %s", gi.String())
		for _, node := range nodes {
			for _, nodeDeltaRes := range node.commitDeltaResources[gi] {
				groupedCommits[gi] = append(groupedCommits[gi], groupResources(gi, nodeDeltaRes, nodes))
			}
		}
	}

	if markdownOut {
		markdownOutput(commitLogIds, commits, groupedCommits)
	}

	if githubMilestone != "" {
		githubCreate(githubMilestone, commitLogIds, groupedCommits, commits)
	}

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
