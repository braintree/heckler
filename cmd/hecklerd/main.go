package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"text/template"
	"time"

	"github.braintreeps.com/lollipopman/heckler/internal/gitutil"
	"github.braintreeps.com/lollipopman/heckler/internal/hecklerpb"
	"github.braintreeps.com/lollipopman/heckler/internal/rizzopb"
	"github.com/Masterminds/sprig"
	"github.com/bradleyfalzon/ghinstallation"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-github/github"
	git "github.com/libgit2/git2go"
	gitcgiserver "github.com/lollipopman/git-cgi-server"
	"google.golang.org/grpc"
	"gopkg.in/yaml.v3"
)

var Version string
var ErrLastApplyUnknown = errors.New("Unable to determine lastApply commit, use force flag to update")

const (
	ApplicationName = "git-cgi-server"

	defaultAddr     = ":8080"
	shutdownTimeout = time.Second * 5
	port            = ":50052"
	stateDir        = "/var/lib/hecklerd"
)

var Debug = false
var RegexDefineType = regexp.MustCompile(`^[A-Z][a-zA-Z0-9_:]*\[[^\]]+\]$`)

type HecklerdConf struct {
	Repo                 string             `yaml:"repo"`
	RepoOwner            string             `yaml:"repo_owner"`
	GitHubDomain         string             `yaml:"github_domain"`
	GitHubPrivateKeyPath string             `yaml:"github_private_key_path"`
	GitHubAppId          int64              `yaml:"github_app_id"`
	GitHubAppInstallId   int64              `yaml:"github_app_install_id"`
	NodeSets             map[string]NodeSet `yaml:"node_sets"`
}

type NodeSet struct {
	Cmd []string `yaml:"cmd"`
}

// hecklerServer is used to implement heckler.HecklerServer
type hecklerServer struct {
	hecklerpb.UnimplementedHecklerServer
	conf      *HecklerdConf
	repo      *git.Repository
	templates *template.Template
}

type Node struct {
	host                 string
	commitReports        map[git.Oid]*rizzopb.PuppetReport
	commitDeltaResources map[git.Oid]map[ResourceTitle]*deltaResource
	rizzoClient          rizzopb.RizzoClient
	lastApply            git.Oid
}

type nodeResult struct {
	node *Node
	err  error
}

type deltaResource struct {
	Title      ResourceTitle
	Type       string
	DefineType string
	Events     []*rizzopb.Event
	Logs       []*rizzopb.Log
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

func commitParentReports(commit git.Commit, lastApply git.Oid, commitReports map[git.Oid]*rizzopb.PuppetReport, repo *git.Repository) []*rizzopb.PuppetReport {
	var parentReports []*rizzopb.PuppetReport
	var parentReport *rizzopb.PuppetReport

	parentCount := commit.ParentCount()
	for i := uint(0); i < parentCount; i++ {
		// perma-diff: If our parent is already applied we substitute the lastApply
		// noop so that we can subtract away any perma-diffs from the noop
		if commitAlreadyApplied(lastApply, *commit.ParentId(i), repo) {
			parentReport = commitReports[lastApply]
		} else {
			parentReport = commitReports[*commit.ParentId(i)]
		}
		if parentReport != nil {
			parentReports = append(parentReports, commitReports[*commit.ParentId(i)])
		} else {
			log.Fatalf("Parent report not found %s", commit.ParentId(i).String())
		}
	}
	return parentReports
}

func grpcConnect(ctx context.Context, node *Node, clientConnChan chan nodeResult) {
	address := node.host + ":50051"
	log.Printf("Dialing: %v", node.host)
	ctx, cancel := context.WithTimeout(ctx, time.Duration(5)*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, address, grpc.WithInsecure(), grpc.WithBlock(), grpc.FailOnNonTempDialError(true))
	if errors.Is(err, context.DeadlineExceeded) {
		clientConnChan <- nodeResult{node, errors.New(fmt.Sprintf("Timeout connecting to %s", address))}
	} else if err != nil {
		clientConnChan <- nodeResult{node, err}
	} else {
		node.rizzoClient = rizzopb.NewRizzoClient(conn)
		clientConnChan <- nodeResult{node, nil}
	}
}

func dialNodes(ctx context.Context, hosts []string) (map[string]*Node, map[string]error) {
	var node *Node
	clientConnChan := make(chan nodeResult)
	for _, host := range hosts {
		node = new(Node)
		node.host = host
		go grpcConnect(ctx, node, clientConnChan)
	}

	nodes := make(map[string]*Node)
	errNodes := make(map[string]error)
	var nr nodeResult
	for i := 0; i < len(hosts); i++ {
		nr = <-clientConnChan
		if nr.err != nil {
			errNodes[nr.node.host] = nr.err
		} else {
			nodes[nr.node.host] = nr.node
		}
	}
	return nodes, errNodes
}

func commitLogIdList(repo *git.Repository, beginRev string, endRev string) ([]git.Oid, map[git.Oid]*git.Commit, error) {
	var commitLogIds []git.Oid
	var commits map[git.Oid]*git.Commit

	commits = make(map[git.Oid]*git.Commit)

	log.Printf("Walk begun: %s..%s", beginRev, endRev)
	rv, err := repo.Walk()
	if err != nil {
		return nil, nil, err
	}

	// We what to sort by the topology of the date of the commits. Also, reverse
	// the sort so the first commit in the array is the earliest commit or oldest
	// ancestor in the topology.
	rv.Sorting(git.SortTopological | git.SortReverse)

	endObj, err := gitutil.RevparseToCommit(endRev, repo)
	if err != nil {
		return nil, nil, err
	}
	err = rv.Push(endObj.Id())
	if err != nil {
		return nil, nil, err
	}
	beginObj, err := gitutil.RevparseToCommit(beginRev, repo)
	if err != nil {
		return nil, nil, err
	}
	err = rv.Hide(beginObj.Id())
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
		log.Printf("commit: %s", gi.String())
	}
	log.Printf("Walk Complete")

	return commitLogIds, commits, nil
}

func loadNoop(commit git.Oid, node *Node, revdir string, repo *git.Repository) (*rizzopb.PuppetReport, error) {
	// perma-diff: Substitute an empty puppet noop report if the commit is
	// already applied, however for the lastApplied commit we do want to use the
	// noop report so we can use the noop diff to subtract away perma-diffs from
	// children.
	descendant, err := repo.DescendantOf(&node.lastApply, &commit)
	if err != nil {
		log.Fatalf("Cannot determine descendant status: %v", err)
	}
	if descendant {
		log.Printf("Commit already applied, substituting an empty noop: %s@%s", node.host, commit.String())
		return new(rizzopb.PuppetReport), nil
	}

	reportPath := revdir + "/" + node.host + "/" + commit.String() + ".json"
	if _, err := os.Stat(reportPath); err != nil {
		return nil, err
	} else {
		file, err := os.Open(reportPath)
		if err != nil {
			return nil, err
		}
		defer file.Close()

		data, err := ioutil.ReadAll(file)
		if err != nil {
			return nil, err
		}
		rprt := new(rizzopb.PuppetReport)
		err = json.Unmarshal([]byte(data), rprt)
		if err != nil {
			return nil, err
		}
		if node.host != rprt.Host {
			return nil, errors.New(fmt.Sprintf("Host mismatch %s != %s", node.host, rprt.Host))
		}
		log.Printf("Found serialized noop: %s@%s", rprt.Host, rprt.ConfigurationVersion)
		return rprt, nil
	}
}
func normalizeReport(rprt rizzopb.PuppetReport) rizzopb.PuppetReport {
	rprt.Logs = normalizeLogs(rprt.Logs)
	return rprt
}

func marshalReport(rprt rizzopb.PuppetReport, revdir string, commit git.Oid) error {
	reportPath := revdir + "/" + rprt.Host + "/" + commit.String() + ".json"
	data, err := json.Marshal(rprt)
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(reportPath, data, 0644)
	if err != nil {
		return err
	}
	return nil
}

func noopCommitRange(nodes map[string]*Node, beginRev, endRev string, commitLogIds []git.Oid, commits map[git.Oid]*git.Commit, repo *git.Repository) (map[git.Oid][]*groupedResource, error) {
	var err error
	puppetReportChan := make(chan rizzopb.PuppetReport)
	var commitsToNoop []git.Oid

	revdir := fmt.Sprintf(stateDir+"/noops/%s..%s", beginRev, endRev)

	os.MkdirAll(revdir, 077)
	for host, _ := range nodes {
		os.Mkdir(revdir+"/"+host, 077)
	}

	var groupedCommits map[git.Oid][]*groupedResource

	groupedCommits = make(map[git.Oid][]*groupedResource)

	for _, node := range nodes {
		node.commitReports = make(map[git.Oid]*rizzopb.PuppetReport)
		node.commitDeltaResources = make(map[git.Oid]map[ResourceTitle]*deltaResource)
	}

	var noopRequests int
	var rprt *rizzopb.PuppetReport

	beginCommit, err := gitutil.RevparseToCommit(beginRev, repo)
	if err != nil {
		return nil, err
	}
	beginCommitId := *beginCommit.Id()

	commitsToNoop = make([]git.Oid, len(commitLogIds))
	copy(commitsToNoop, commitLogIds)
	commitsToNoop = append(commitsToNoop, beginCommitId)

	for i, commitToNoop := range commitsToNoop {
		log.Printf("Nooping: %s (%d of %d)", commitToNoop.String(), i, len(commitsToNoop))
		par := rizzopb.PuppetApplyRequest{Rev: commitToNoop.String(), Noop: true}
		noopRequests = 0
		for _, node := range nodes {
			// perma-diff: We need a noop report of the lastApply so we can use it to
			// subtract away perma-diffs from children. Typically the lastApply is
			// the beginRev, but if someone has applied a newer commit on the host we
			// need to use the noop from the lastApplied commit.
			if commitToNoop == beginCommitId {
				commitToNoop = node.lastApply
			}
			if rprt, err = loadNoop(commitToNoop, node, revdir, repo); err == nil {
				nodes[node.host].commitReports[commitToNoop] = rprt
			} else if os.IsNotExist(err) {
				go hecklerApply(node.rizzoClient, puppetReportChan, par)
				noopRequests++
			} else {
				log.Fatalf("Unable to load noop: %v", err)
			}
		}

		for j := 0; j < noopRequests; j++ {
			newRprt := normalizeReport(<-puppetReportChan)
			log.Printf("Received noop: %s@%s", newRprt.Host, newRprt.ConfigurationVersion)
			commitId, err := git.NewOid(newRprt.ConfigurationVersion)
			if err != nil {
				log.Fatalf("Unable to marshal report: %v", err)
			}
			nodes[newRprt.Host].commitReports[*commitId] = &newRprt
			err = marshalReport(newRprt, revdir, *commitId)
			if err != nil {
				log.Fatalf("Unable to marshal report: %v", err)
			}
		}
	}

	for host, node := range nodes {
		for _, gi := range commitLogIds {
			log.Printf("Creating delta resource: %s@%s", host, gi.String())
			// perma-diff: If the commit is already applied we can assume that the
			// diff is empty. Ideally we would not need this special case as the noop
			// for an already applied commit should be empty, but we purposefully use
			// the noop of the lastApply to subtract away perma-diffs, so those would
			// show up without this special case. TODO: Assign perma-diffs to server
			// owners?
			if commitAlreadyApplied(node.lastApply, gi, repo) {
				node.commitDeltaResources[gi] = make(map[ResourceTitle]*deltaResource)
			} else {
				node.commitDeltaResources[gi] = deltaNoop(node.commitReports[gi], commitParentReports(*commits[gi], node.lastApply, node.commitReports, repo))
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
	return groupedCommits, nil
}

func priorEvent(event *rizzopb.Event, resourceTitleStr string, priorCommitNoops []*rizzopb.PuppetReport) bool {
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

func priorLog(log *rizzopb.Log, priorCommitNoops []*rizzopb.PuppetReport) bool {
	for _, priorCommitNoop := range priorCommitNoops {
		for _, priorLog := range priorCommitNoop.Logs {
			if *log == *priorLog {
				return true
			}
		}
	}
	return false
}

func initDeltaResource(resourceTitle ResourceTitle, r *rizzopb.ResourceStatus, deltaEvents []*rizzopb.Event, deltaLogs []*rizzopb.Log) *deltaResource {
	deltaRes := new(deltaResource)
	deltaRes.Title = resourceTitle
	deltaRes.Type = r.ResourceType
	deltaRes.Events = deltaEvents
	deltaRes.Logs = deltaLogs
	deltaRes.DefineType = resourceDefineType(r)
	return deltaRes
}

func deltaNoop(commitNoop *rizzopb.PuppetReport, priorCommitNoops []*rizzopb.PuppetReport) map[ResourceTitle]*deltaResource {
	var deltaEvents []*rizzopb.Event
	var deltaLogs []*rizzopb.Log
	var deltaResources map[ResourceTitle]*deltaResource
	var resourceTitle ResourceTitle

	deltaResources = make(map[ResourceTitle]*deltaResource)

	if commitNoop.ResourceStatuses == nil {
		return deltaResources
	}

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

// Determine if a commit is already applied based on the last appliedCommit.
// If the potentialCommit is an ancestor of the appliedCommit or equal to the
// appliedCommit then we know the potentialCommit has already been applied.
func commitAlreadyApplied(appliedCommit git.Oid, potentialCommit git.Oid, repo *git.Repository) bool {
	if appliedCommit.Equal(&potentialCommit) {
		return true
	}
	descendant, err := repo.DescendantOf(&appliedCommit, &potentialCommit)
	if err != nil {
		log.Fatalf("Cannot determine descendant status: %v", err)
	}
	return descendant
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

func resourceDefineType(res *rizzopb.ResourceStatus) string {
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
	return gr
}

func normalizeLogs(Logs []*rizzopb.Log) []*rizzopb.Log {
	var newSource string
	var origSource string
	var newLogs []*rizzopb.Log

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
				log.Printf("Dropping Log: %v: %v", l.Source, l.Message)
			}
			continue
		} else if regexClass.MatchString(l.Source) ||
			regexStage.MatchString(l.Source) ||
			RegexDefineType.MatchString(l.Source) {
			if Debug {
				log.Printf("Dropping Log: %v: %v", l.Source, l.Message)
			}
			continue
		} else if (!regexResource.MatchString(l.Source)) && regexRefreshMsg.MatchString(l.Message) {
			if Debug {
				log.Printf("Dropping Log: %v: %v", l.Source, l.Message)
			}
			continue
		} else if regexResource.MatchString(l.Source) {
			origSource = l.Source
			newSource = regexResourcePropertyTail.ReplaceAllString(l.Source, "")
			newSource = regexResourceTail.FindString(newSource)
			if newSource == "" {
				log.Printf("newSource is empty!")
				log.Printf("Log: '%v' -> '%v': %v", origSource, newSource, l.Message)
				os.Exit(1)
			}

			if reFileContent.MatchString(l.Source) && reDiff.MatchString(l.Message) {
				l.Message = normalizeDiff(l.Message)
			}
			l.Source = newSource
			if Debug {
				log.Printf("Adding Log: '%v' -> '%v': %v", origSource, newSource, l.Message)
			}
			newLogs = append(newLogs, l)
		} else {
			log.Printf("Unaccounted for Log: %v: %v", l.Source, l.Message)
			newLogs = append(newLogs, l)
		}
	}

	return newLogs
}

func hecklerApply(rc rizzopb.RizzoClient, c chan<- rizzopb.PuppetReport, par rizzopb.PuppetApplyRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*300)
	defer cancel()
	r, err := rc.PuppetApply(ctx, &par)
	if err != nil {
		c <- rizzopb.PuppetReport{}
	}
	if ctx.Err() != nil {
		log.Printf("ERROR: Context error: %v", ctx.Err())
		c <- rizzopb.PuppetReport{}
	}
	c <- *r
}

func parseTemplates() *template.Template {
	var templatesPath string
	if _, err := os.Stat("/usr/share/hecklerd/templates"); err == nil {
		templatesPath = "/usr/share/hecklerd/templates" + "/*.tmpl"
	} else {
		templatesPath = "*.tmpl"
	}
	return template.Must(template.New("base").Funcs(sprig.TxtFuncMap()).ParseGlob(templatesPath))
}

func reqNodes(conf *HecklerdConf, nodes []string, nodeSetName string) ([]string, error) {
	if len(nodes) > 0 {
		return nodes, nil
	}
	if nodeSetName == "" {
		return nil, errors.New("Empty nodeSetName provided")
	}
	var cmdArgs []string
	if nodeSet, ok := conf.NodeSets[nodeSetName]; ok {
		cmdArgs = nodeSet.Cmd
	} else {
		return nil, errors.New(fmt.Sprintf("nodeSetName '%s' not found in hecklerd config", nodeSetName))
	}
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	log.Printf("Executing cmd '%s' for node set '%s'", cmd, nodeSetName)
	stdout, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(stdout, &nodes)
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, errors.New(fmt.Sprintf("Cmd '%s' produced zero nodes", cmd))
	}
	log.Printf("Cmd for node set '%s' succeeded, node count: %d", nodeSetName, len(nodes))
	return nodes, nil
}

func (hs *hecklerServer) HecklerApply(ctx context.Context, req *hecklerpb.HecklerApplyRequest) (*hecklerpb.HecklerApplyReport, error) {
	nodesToDial, err := reqNodes(hs.conf, req.Nodes, req.NodeSet)
	if err != nil {
		return nil, err
	}
	nodes, errDialNodes := dialNodes(ctx, nodesToDial)
	lockReq := hecklerpb.HecklerLockRequest{
		User:    req.User,
		Comment: "Applying with Heckler",
	}
	lockedNodes, errLockNodes := nodeLock(&lockReq, nodes)
	unlockReq := hecklerpb.HecklerUnlockRequest{
		User: req.User,
	}
	defer nodeUnlock(&unlockReq, lockedNodes)

	var nodesToApply map[string]*Node
	beyondRevNodes := make([]string, 0)
	lastApplyNodes := make(map[string]*Node)
	errUnknownRevNodes := make(map[string]error)
	// No need to check node revision if force applying
	if req.Force {
		nodesToApply = lockedNodes
	} else {
		lastApplyNodes, errUnknownRevNodes = nodeLastApply(lockedNodes, hs.repo)
		obj, err := gitutil.RevparseToCommit(req.Rev, hs.repo)
		if err != nil {
			return nil, err
		}
		revId := *obj.Id()
		nodesToApply = make(map[string]*Node)
		for host, node := range lastApplyNodes {
			if commitAlreadyApplied(node.lastApply, revId, hs.repo) {
				beyondRevNodes = append(beyondRevNodes, host)
			} else {
				nodesToApply[host] = node
			}
		}
	}
	par := rizzopb.PuppetApplyRequest{Rev: req.Rev, Noop: req.Noop}
	puppetReportChan := make(chan rizzopb.PuppetReport)
	for _, node := range nodesToApply {
		go hecklerApply(node.rizzoClient, puppetReportChan, par)
	}

	errApplyNodes := make(map[string]error)
	appliedNodes := make([]string, 0)
	for range nodesToApply {
		r := <-puppetReportChan
		if cmp.Equal(r, rizzopb.PuppetReport{}) {
			log.Fatalf("Received an empty report")
		} else if r.Status == "failed" {
			log.Printf("ERROR: Apply failed, %s@%s", r.Host, r.ConfigurationVersion)
			errApplyNodes[r.Host] = errors.New("Apply failed")
		} else {
			if req.Noop {
				log.Printf("Nooped: %s@%s", r.Host, r.ConfigurationVersion)
			} else {
				log.Printf("Applied: %s@%s", r.Host, r.ConfigurationVersion)
			}
			appliedNodes = append(appliedNodes, r.Host)
		}
	}
	errNodes := make(map[string]error)
	for host, err := range errDialNodes {
		errNodes[host] = err
	}
	for host, err := range errLockNodes {
		errNodes[host] = err
	}
	for host, err := range errUnknownRevNodes {
		errNodes[host] = err
	}
	for host, err := range errApplyNodes {
		errNodes[host] = err
	}
	har := new(hecklerpb.HecklerApplyReport)
	har.NodeErrors = make(map[string]string)
	for host, err := range errNodes {
		har.NodeErrors[host] = err.Error()
	}
	if req.Force {
		har.Output = fmt.Sprintf("Applied nodes: %d; Error nodes: %d", len(appliedNodes), len(errNodes))
	} else {
		har.Output = fmt.Sprintf("Applied nodes: %d; Beyond rev nodes: %d; Error nodes: %d", len(appliedNodes), len(beyondRevNodes), len(errNodes))
	}
	return har, nil
}

func (hs *hecklerServer) HecklerNoopRange(ctx context.Context, req *hecklerpb.HecklerNoopRangeRequest) (*hecklerpb.HecklerNoopRangeReport, error) {
	nodesToDial, err := reqNodes(hs.conf, req.Nodes, req.NodeSet)
	if err != nil {
		return nil, err
	}
	nodes, errNodes := dialNodes(ctx, nodesToDial)
	lockReq := hecklerpb.HecklerLockRequest{
		User:    req.User,
		Comment: "Nooping with Heckler",
	}
	lockedNodes, errLockNodes := nodeLock(&lockReq, nodes)
	unlockReq := hecklerpb.HecklerUnlockRequest{
		User: req.User,
	}
	defer nodeUnlock(&unlockReq, lockedNodes)
	lastApplyNodes, errUnknownRevNodes := nodeLastApply(lockedNodes, hs.repo)
	commitLogIds, commits, err := commitLogIdList(hs.repo, req.BeginRev, req.EndRev)
	if err != nil {
		return nil, err
	}
	groupedCommits, err := noopCommitRange(lastApplyNodes, req.BeginRev, req.EndRev, commitLogIds, commits, hs.repo)
	if err != nil {
		return nil, err
	}
	if req.GithubMilestone != "" {
		ghclient, _, err := githubConn(hs.conf)
		if err != nil {
			return nil, err
		}
		githubCreate(ghclient, req.GithubMilestone, hs.conf.RepoOwner, hs.conf.Repo, commitLogIds, groupedCommits, commits, hs.templates)
	}
	hnrr := new(hecklerpb.HecklerNoopRangeReport)
	if req.OutputFormat == hecklerpb.OutputFormat_markdown {
		hnrr.Output = markdownOutput(commitLogIds, commits, groupedCommits, hs.templates)
	}
	hnrr.NodeErrors = make(map[string]string)
	for host, err := range errNodes {
		hnrr.NodeErrors[host] = err.Error()
	}
	for host, err := range errLockNodes {
		hnrr.NodeErrors[host] = err.Error()
	}
	for host, err := range errUnknownRevNodes {
		hnrr.NodeErrors[host] = err.Error()
	}
	return hnrr, nil
}

func githubConn(conf *HecklerdConf) (*github.Client, *ghinstallation.Transport, error) {
	var privateKey []byte
	var file *os.File
	var err error

	// Shared transport to reuse TCP connections.
	tr := http.DefaultTransport

	if conf.GitHubPrivateKeyPath != "" {
		file, err = os.Open(conf.GitHubPrivateKeyPath)
		if err != nil {
			return nil, nil, err
		}
		privateKey, err = ioutil.ReadAll(file)
	} else if _, err := os.Stat("github-private-key.pem"); err == nil {
		file, err = os.Open("github-private-key.pem")
		if err != nil {
			return nil, nil, err
		}
		privateKey, err = ioutil.ReadAll(file)
	} else {
		return nil, nil, errors.New("Unable to load github-private-key.pem in /etc/hecklerd or .")
	}
	defer file.Close()
	itr, err := ghinstallation.New(tr, conf.GitHubAppId, conf.GitHubAppInstallId, privateKey)
	if err != nil {
		return nil, nil, err
	}
	githubUrl := "https://" + conf.GitHubDomain + "/api/v3"
	itr.BaseURL = githubUrl

	// Use installation transport with github.com/google/go-github
	client, err := github.NewEnterpriseClient(githubUrl, githubUrl, &http.Client{Transport: itr})
	if err != nil {
		return nil, nil, err
	}
	return client, itr, nil
}

func githubCreate(ghclient *github.Client, repoOwner string, repo string, githubMilestone string, commitLogIds []git.Oid, groupedCommits map[git.Oid][]*groupedResource, commits map[git.Oid]*git.Commit, templates *template.Template) {
	ctx := context.Background()
	m := &github.Milestone{
		Title: github.String(githubMilestone),
	}
	nm, _, err := ghclient.Issues.CreateMilestone(ctx, repoOwner, repo, m)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Successfully created new milestone: %v", *nm.Title)

	for _, gi := range commitLogIds {
		if len(groupedCommits[gi]) == 0 {
			log.Printf("Skipping %s, no noop output", gi.String())
			continue
		}
		githubIssue := &github.IssueRequest{
			Title:     github.String(fmt.Sprintf("Puppet noop output for commit: '%v'", commits[gi].Summary())),
			Assignee:  github.String(commits[gi].Author().Name),
			Body:      github.String(commitToMarkdown(commits[gi], templates) + groupedResourcesToMarkdown(groupedCommits[gi], templates)),
			Milestone: nm.Number,
		}
		ni, _, err := ghclient.Issues.Create(ctx, repoOwner, repo, githubIssue)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("Successfully created new issue: %v", *ni.Title)
	}
}

func markdownOutput(commitLogIds []git.Oid, commits map[git.Oid]*git.Commit, groupedCommits map[git.Oid][]*groupedResource, templates *template.Template) string {
	var output string
	for _, gi := range commitLogIds {
		if len(groupedCommits[gi]) == 0 {
			log.Printf("Skipping %s, no noop output", gi.String())
			continue
		}
		output += fmt.Sprintf("## Puppet noop output for commit: '%v'\n\n", commits[gi].Summary())
		output += commitToMarkdown(commits[gi], templates)
		output += groupedResourcesToMarkdown(groupedCommits[gi], templates)
	}
	return output
}

func commitToMarkdown(c *git.Commit, templates *template.Template) string {
	var body strings.Builder
	var err error

	err = templates.ExecuteTemplate(&body, "commit.tmpl", c)
	if err != nil {
		log.Fatal(err)
	}
	return body.String()
}

func groupedResourcesToMarkdown(groupedResources []*groupedResource, templates *template.Template) string {
	var body strings.Builder
	var err error

	sort.Slice(
		groupedResources,
		func(i, j int) bool {
			if string(groupedResources[i].Title) == string(groupedResources[j].Title) {
				// if the resources titles are equal sort by the list of nodes affected
				return strings.Join(groupedResources[i].Nodes[:], ",") < strings.Join(groupedResources[j].Nodes[:], ",")
			} else {
				return string(groupedResources[i].Title) < string(groupedResources[j].Title)
			}
		})

	err = templates.ExecuteTemplate(&body, "groupedResource.tmpl", groupedResources)
	if err != nil {
		log.Fatal(err)
	}
	return body.String()
}

func (hs *hecklerServer) HecklerStatus(ctx context.Context, req *hecklerpb.HecklerStatusRequest) (*hecklerpb.HecklerStatusReport, error) {
	nodesToDial, err := reqNodes(hs.conf, req.Nodes, req.NodeSet)
	if err != nil {
		return nil, err
	}
	nodes, errNodes := dialNodes(ctx, nodesToDial)
	lastApplyNodes, errUnknownRevNodes := nodeLastApply(nodes, hs.repo)
	hsr := new(hecklerpb.HecklerStatusReport)
	hsr.NodeStatuses = make(map[string]string)
	for _, node := range lastApplyNodes {
		hsr.NodeStatuses[node.host] = node.lastApply.String()
	}
	hsr.NodeErrors = make(map[string]string)
	for host, err := range errNodes {
		hsr.NodeErrors[host] = err.Error()
	}
	for host, err := range errUnknownRevNodes {
		hsr.NodeErrors[host] = err.Error()
	}
	return hsr, nil
}
func (hs *hecklerServer) HecklerUnlock(ctx context.Context, req *hecklerpb.HecklerUnlockRequest) (*hecklerpb.HecklerUnlockReport, error) {
	nodesToDial, err := reqNodes(hs.conf, req.Nodes, req.NodeSet)
	if err != nil {
		return nil, err
	}
	nodes, errNodes := dialNodes(ctx, nodesToDial)
	unlockedNodes, errUnlockNodes := nodeUnlock(req, nodes)
	res := new(hecklerpb.HecklerUnlockReport)
	res.UnlockedNodes = make([]string, 0, len(unlockedNodes))
	for k := range unlockedNodes {
		res.UnlockedNodes = append(res.UnlockedNodes, k)
	}
	res.NodeErrors = make(map[string]string)
	for host, err := range errNodes {
		res.NodeErrors[host] = err.Error()
	}
	for host, err := range errUnlockNodes {
		res.NodeErrors[host] = err.Error()
	}
	return res, nil
}

func nodeUnlock(req *hecklerpb.HecklerUnlockRequest, nodes map[string]*Node) (map[string]*Node, map[string]error) {
	reportChan := make(chan rizzopb.PuppetUnlockReport)
	puppetReq := rizzopb.PuppetUnlockRequest{
		User:  req.User,
		Force: req.Force,
	}
	for _, node := range nodes {
		go hecklerUnlock(node.host, node.rizzoClient, puppetReq, reportChan)
	}

	unlockedNodes := make(map[string]*Node)
	errNodes := make(map[string]error)
	for i := 0; i < len(nodes); i++ {
		r := <-reportChan
		if r.Unlocked {
			unlockedNodes[r.Host] = nodes[r.Host]
		} else {
			errNodes[r.Host] = errors.New(r.Error)
		}
	}

	return unlockedNodes, errNodes
}

func hecklerUnlock(host string, rc rizzopb.RizzoClient, req rizzopb.PuppetUnlockRequest, c chan<- rizzopb.PuppetUnlockReport) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	res, err := rc.PuppetUnlock(ctx, &req)
	if err != nil {
		c <- rizzopb.PuppetUnlockReport{
			Host:     host,
			Unlocked: false,
			Error:    err.Error(),
		}
		return
	}
	res.Host = host
	c <- *res
	return
}

func (hs *hecklerServer) HecklerLock(ctx context.Context, req *hecklerpb.HecklerLockRequest) (*hecklerpb.HecklerLockReport, error) {
	nodesToDial, err := reqNodes(hs.conf, req.Nodes, req.NodeSet)
	if err != nil {
		return nil, err
	}
	nodes, errNodes := dialNodes(ctx, nodesToDial)
	lockedNodes, errLockNodes := nodeLock(req, nodes)
	res := new(hecklerpb.HecklerLockReport)
	res.LockedNodes = make([]string, 0, len(lockedNodes))
	for k := range lockedNodes {
		res.LockedNodes = append(res.LockedNodes, k)
	}
	res.NodeErrors = make(map[string]string)
	for host, err := range errNodes {
		res.NodeErrors[host] = err.Error()
	}
	for host, err := range errLockNodes {
		res.NodeErrors[host] = err.Error()
	}
	return res, nil
}

func nodeLock(req *hecklerpb.HecklerLockRequest, nodes map[string]*Node) (map[string]*Node, map[string]error) {
	reportChan := make(chan rizzopb.PuppetLockReport)
	puppetReq := rizzopb.PuppetLockRequest{
		User:    req.User,
		Comment: req.Comment,
		Force:   req.Force,
	}
	for _, node := range nodes {
		go hecklerLock(node.host, node.rizzoClient, puppetReq, reportChan)
	}

	lockedNodes := make(map[string]*Node)
	errNodes := make(map[string]error)
	for i := 0; i < len(nodes); i++ {
		r := <-reportChan
		if r.Locked {
			lockedNodes[r.Host] = nodes[r.Host]
		} else {
			errNodes[r.Host] = errors.New(r.Error)
		}
	}

	return lockedNodes, errNodes
}

func hecklerLock(host string, rc rizzopb.RizzoClient, req rizzopb.PuppetLockRequest, c chan<- rizzopb.PuppetLockReport) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	res, err := rc.PuppetLock(ctx, &req)
	if err != nil {
		c <- rizzopb.PuppetLockReport{
			Host:   host,
			Locked: false,
			Error:  err.Error(),
		}
		return
	}
	res.Host = host
	c <- *res
	return
}

func hecklerLastApply(node *Node, c chan<- rizzopb.PuppetReport) {
	plar := rizzopb.PuppetLastApplyRequest{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	r, err := node.rizzoClient.PuppetLastApply(ctx, &plar)
	if err != nil {
		log.Printf("Puppet lastApply error substituting an empty report, %v", err)
		c <- rizzopb.PuppetReport{
			Host: node.host,
		}
		return
	}
	c <- *r
	return
}

func nodeLastApply(nodes map[string]*Node, repo *git.Repository) (map[string]*Node, map[string]error) {
	var err error
	errNodes := make(map[string]error)
	lastApplyNodes := make(map[string]*Node)

	puppetReportChan := make(chan rizzopb.PuppetReport)
	for _, node := range nodes {
		go hecklerLastApply(node, puppetReportChan)
	}

	var obj *git.Object
	for range nodes {
		r := <-puppetReportChan
		if r.ConfigurationVersion == "" {
			log.Printf("ConfigurationVersion empty, for host %s", r.Host)
			errNodes[r.Host] = ErrLastApplyUnknown
			continue
		}
		obj, err = repo.RevparseSingle(r.ConfigurationVersion)
		if err != nil {
			log.Fatalf("Unable to revparse ConfigurationVersion, '%s', for host %s: %v", r.ConfigurationVersion, r.Host, err)
		}
		if node, ok := nodes[r.Host]; ok {
			node.lastApply = *obj.Id()
			lastApplyNodes[r.Host] = node
		} else {
			log.Fatalf("No Node struct found for report from: %s", r.Host)
		}
	}

	return lastApplyNodes, errNodes
}

func fetchRepo(conf *HecklerdConf) (*git.Repository, error) {
	_, itr, err := githubConn(conf)
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	tok, err := itr.Token(ctx)
	if err != nil {
		return nil, err
	}

	cloneDir := stateDir + "/served_repo/puppetcode"
	cloneOptions := &git.CloneOptions{}
	cloneOptions.Bare = true
	remoteUrl := fmt.Sprintf("https://x-access-token:%s@%s/%s/%s", tok, conf.GitHubDomain, conf.RepoOwner, conf.Repo)
	bareRepo, err := gitutil.CloneOrOpen(remoteUrl, cloneDir, cloneOptions)
	if err != nil {
		return nil, err
	}
	err = gitutil.FastForward(bareRepo, nil)
	if err != nil {
		return nil, err
	}
	return bareRepo, nil
}

func main() {
	// add filename and linenumber to log output
	log.SetFlags(log.Lshortfile)
	var err error
	var hecklerdConfPath string
	var hecklerdConf *HecklerdConf
	var file *os.File
	var data []byte
	var clearState bool
	var printVersion bool
	templates := parseTemplates()

	flag.BoolVar(&clearState, "clear", false, "Clear local state, e.g. puppet code repo")
	flag.BoolVar(&printVersion, "version", false, "print version")
	flag.Parse()

	if printVersion {
		fmt.Printf("v%s\n", Version)
		os.Exit(0)
	}

	if _, err := os.Stat("/etc/hecklerd/hecklerd_conf.yaml"); err == nil {
		hecklerdConfPath = "/etc/hecklerd/hecklerd_conf.yaml"
	} else if _, err := os.Stat("hecklerd_conf.yaml"); err == nil {
		hecklerdConfPath = "hecklerd_conf.yaml"
	} else {
		log.Fatal("Unable to load hecklerd_conf.yaml from /etc/hecklerd or .")
	}
	file, err = os.Open(hecklerdConfPath)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	data, err = ioutil.ReadAll(file)
	if err != nil {
		log.Fatalf("Cannot read config: %v", err)
	}
	hecklerdConf = new(HecklerdConf)
	err = yaml.Unmarshal([]byte(data), hecklerdConf)
	if err != nil {
		log.Fatalf("Cannot unmarshal config: %v", err)
	}

	if clearState {
		log.Printf("Removing state directory: %v", stateDir)
		os.RemoveAll(stateDir)
		os.Exit(0)
	}

	repo, err := fetchRepo(hecklerdConf)
	if err != nil {
		log.Fatalf("Unable to fetch repo to serve: %v", err)
	}

	gitServer := &gitcgiserver.GitCGIServer{}
	gitServer.ExportAll = true
	gitServer.ProjectRoot = stateDir + "/served_repo"
	gitServer.Addr = defaultAddr
	gitServer.ShutdownTimeout = shutdownTimeout

	idleConnsClosed := make(chan struct{})
	done := make(chan bool, 1)

	// background polling git fetch
	go func() {
		for {
			time.Sleep(5 * time.Second)
			if Debug {
				log.Println("Updating repo..")
			}
			_, err = fetchRepo(hecklerdConf)
			if err != nil {
				log.Fatalf("Unable to fetch repo: %v", err)
			}
		}
	}()

	// git server
	go func() {
		log.Printf("Starting Git HTTP server on %s", gitServer.Addr)
		if err := gitServer.Serve(); err != nil && err != http.ErrServerClosed {
			log.Println("Git HTTP server error:", err)
		}
		<-idleConnsClosed
	}()

	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	grpcServer := grpc.NewServer()
	hecklerServer := new(hecklerServer)
	hecklerServer.conf = hecklerdConf
	hecklerServer.templates = templates
	hecklerServer.repo = repo
	hecklerpb.RegisterHecklerServer(grpcServer, hecklerServer)

	// grpc server
	go func() {
		log.Printf("Starting GRPC HTTP server on %v", port)
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()

	// XXX any reason to make this a separate goroutine?
	go func() {
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
		log.Printf("Received %s", <-sigs)
		if err := gitServer.Shutdown(context.Background()); err != nil {
			log.Printf("HTTP server shutdown error: %v", err)
		}
		close(idleConnsClosed)
		grpcServer.GracefulStop()
		log.Println("Heckler Shutdown")
		done <- true
	}()

	<-done
}
