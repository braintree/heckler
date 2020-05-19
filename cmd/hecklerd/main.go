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
	"runtime"
	"runtime/pprof"
	"sort"
	"strings"
	"syscall"
	"text/template"
	"time"

	"github.braintreeps.com/lollipopman/heckler/internal/gitutil"
	"github.braintreeps.com/lollipopman/heckler/internal/hecklerpb"
	"github.braintreeps.com/lollipopman/heckler/internal/puppetutil"
	"github.braintreeps.com/lollipopman/heckler/internal/rizzopb"
	"github.com/Masterminds/semver/v3"
	"github.com/Masterminds/sprig"
	"github.com/bradleyfalzon/ghinstallation"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/go-github/v29/github"
	codeowners "github.com/hairyhenderson/go-codeowners"
	git "github.com/libgit2/git2go"
	gitcgiserver "github.com/lollipopman/git-cgi-server"
	"github.com/robfig/cron/v3"
	"github.com/square/grange"
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
	workRepo        = "/var/lib/hecklerd" + "/work_repo/puppetcode"
	noopDir         = stateDir + "/noops"
)

var Debug = false

// TODO: this regex also matches, Node[__node_regexp__fozzie], which causes
// resources in node blocks to be mistaken for define types. Is there a more
// robust way to match for define types?
var RegexDefineType = regexp.MustCompile(`^[A-Z][a-zA-Z0-9_:]*\[[^\]]+\]$`)

type HecklerdConf struct {
	Repo                 string             `yaml:"repo"`
	RepoOwner            string             `yaml:"repo_owner"`
	GitHubDomain         string             `yaml:"github_domain"`
	GitHubPrivateKeyPath string             `yaml:"github_private_key_path"`
	GitHubAppSlug        string             `yaml:"github_app_slug"`
	GitHubAppId          int64              `yaml:"github_app_id"`
	GitHubAppInstallId   int64              `yaml:"github_app_install_id"`
	NodeSets             map[string]NodeSet `yaml:"node_sets"`
	AutoTagCronSchedule  string             `yaml:"auto_tag_cron_schedule"`
	AutoCloseIssues      bool               `yaml:"auto_close_issues"`
	EnvPrefix            string             `yaml:"env_prefix"`
	MaxThresholds        Thresholds         `yaml:"max_thresholds"`
	GitServerMaxClients  int                `yaml:"git_server_max_clients"`
	ManualMode           bool               `yaml:"manual_mode"`
	LockMessage          string             `yaml:"lock_message"`
}

type NodeSet struct {
	Cmd       []string `yaml:"cmd"`
	Blacklist []string `yaml:"blacklist"`
}

type Thresholds struct {
	ErrNodes    int `yaml:"err_nodes"`
	LockedNodes int `yaml:"locked_nodes"`
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
	grpcConn             *grpc.ClientConn
	lastApply            git.Oid
}

type nodeResult struct {
	node *Node
	err  error
}

type applyResult struct {
	host   string
	report rizzopb.PuppetReport
	err    error
}

type deltaResource struct {
	Title      ResourceTitle
	Type       string
	DefineType string
	File       string
	Line       int64
	Events     []*rizzopb.Event
	Logs       []*rizzopb.Log
}

type groupedResource struct {
	Title           ResourceTitle
	Type            string
	DefineType      string
	Diff            string
	File            string
	Line            int64
	Nodes           []string
	CompressedNodes string
	Events          []*groupEvent
	Logs            []*groupLog
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

func commitParentReports(commit git.Commit, lastApply git.Oid, commitReports map[git.Oid]*rizzopb.PuppetReport, repo *git.Repository, logger *log.Logger) []*rizzopb.PuppetReport {
	var parentReport *rizzopb.PuppetReport
	parentReports := make([]*rizzopb.PuppetReport, 0)
	parentCount := commit.ParentCount()
	for i := uint(0); i < parentCount; i++ {
		// PERMA-DIFF: If our parent is already applied we substitute the last
		// applied commit noop so that we can subtract away any perma-diffs from
		// the noop
		if commitAlreadyApplied(lastApply, *commit.ParentId(i), repo) {
			logger.Printf("Parent:%s already applied substituting lastApply:%s", commit.ParentId(i).String(), lastApply.String())
			parentReport = commitReports[lastApply]
		} else {
			parentReport = commitReports[*commit.ParentId(i)]
		}
		if parentReport == nil {
			logger.Fatalf("Parent report not found %s", commit.ParentId(i).String())
		} else {
			parentReports = append(parentReports, parentReport)
		}
	}
	return parentReports
}

func grpcConnect(ctx context.Context, node *Node, clientConnChan chan nodeResult) {
	address := node.host + ":50051"
	ctx, cancel := context.WithTimeout(ctx, time.Duration(5)*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, address, grpc.WithInsecure(), grpc.WithBlock(), grpc.FailOnNonTempDialError(true))
	if err != nil {
		clientConnChan <- nodeResult{node, err}
	} else {
		node.rizzoClient = rizzopb.NewRizzoClient(conn)
		node.grpcConn = conn
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
	}
	return commitLogIds, commits, nil
}

func loadNoop(commit git.Oid, node *Node, noopDir string, repo *git.Repository, logger *log.Logger) (*rizzopb.PuppetReport, error) {
	emptyReport := new(rizzopb.PuppetReport)
	// PERMA-DIFF: Substitute an empty puppet noop report if the commit is
	// already applied, however for the node's last applied commit we do want to
	// use the noop report so we can use the noop diff to subtract away
	// perma-diffs from children.
	descendant, err := repo.DescendantOf(&node.lastApply, &commit)
	if err != nil {
		logger.Fatalf("Cannot determine descendant status: %v", err)
	}
	if descendant {
		logger.Printf("Commit already applied, substituting an empty noop: %s@%s", node.host, commit.String())
		return emptyReport, nil
	}

	reportPath := noopDir + "/" + node.host + "/" + commit.String() + ".json"
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
		return rprt, nil
	}
}

func normalizeReport(rprt rizzopb.PuppetReport) rizzopb.PuppetReport {
	for _, resourceStatus := range rprt.ResourceStatuses {
		// Strip off the puppet confdir prefix, so we are left with the relative
		// path of the source file in the code repo
		if resourceStatus.File != "" {
			resourceStatus.File = strings.TrimPrefix(resourceStatus.File, rprt.Confdir+"/")
		}
	}
	rprt.Logs = normalizeLogs(rprt.Logs)
	return rprt
}

func marshalReport(rprt rizzopb.PuppetReport, noopDir string, commit git.Oid) error {
	reportPath := noopDir + "/" + rprt.Host + "/" + commit.String() + ".json"
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

func noopCommit(reqNodes map[string]*Node, commit *git.Commit, deltaNoop bool, repo *git.Repository, logger *log.Logger) ([]*groupedResource, map[string]error, error) {
	var err error

	nodes := make(map[string]*Node)
	for k, v := range reqNodes {
		nodes[k] = v
	}
	noopDir := stateDir + "/noops"
	os.MkdirAll(noopDir, 0755)
	for host, _ := range nodes {
		os.Mkdir(noopDir+"/"+host, 0755)
	}

	// TODO: if we are only requesting a single noop via heckler, we probably
	// always want to noop, rather than returning an empty noop if the commit is
	// not part of a nodes lineage.
	if !commitInAllNodeLineages(*commit.Id(), nodes, repo) {
		return make([]*groupedResource, 0), make(map[string]error), nil
	}

	commitsToNoop := make([]git.Oid, 0)
	commitsToNoop = append(commitsToNoop, *commit.Id())

	parentCount := commit.ParentCount()
	for i := uint(0); i < parentCount; i++ {
		if deltaNoop && commitInAllNodeLineages(*commit.ParentId(i), nodes, repo) {
			commitsToNoop = append(commitsToNoop, *commit.ParentId(i))
		} else {
			for _, node := range nodes {
				node.commitReports[*commit.ParentId(i)] = &rizzopb.PuppetReport{}
				node.commitDeltaResources[*commit.ParentId(i)] = make(map[ResourceTitle]*deltaResource)
			}
		}
	}

	var nodeCommitToNoop git.Oid
	var rprt *rizzopb.PuppetReport
	errNoopNodes := make(map[string]error)
	puppetReportChan := make(chan applyResult)
	for i, commitToNoop := range commitsToNoop {
		logger.Printf("Nooping: %s (%d of %d)", commitToNoop.String(), i+1, len(commitsToNoop))
		noopHosts := make(map[string]bool)
		for _, node := range nodes {
			// PERMA-DIFF: We need a noop report of the last applied commit so we can
			// use it to subtract away perma-diffs from children.
			if commitToNoop != *commit.Id() && commitAlreadyApplied(node.lastApply, commitToNoop, repo) {
				nodeCommitToNoop = node.lastApply
			} else {
				nodeCommitToNoop = commitToNoop
			}
			if rprt, err = loadNoop(nodeCommitToNoop, node, noopDir, repo, logger); err == nil {
				nodes[node.host].commitReports[nodeCommitToNoop] = rprt
			} else if os.IsNotExist(err) {
				par := rizzopb.PuppetApplyRequest{Rev: nodeCommitToNoop.String(), Noop: true}
				go hecklerApply(node, puppetReportChan, par)
				noopHosts[node.host] = true
			} else {
				logger.Fatalf("Unable to load noop: %v", err)
			}
		}
		noopRequests := len(noopHosts)
		if noopRequests > 0 {
			logger.Printf("Requesting noops for %s: %s", nodeCommitToNoop.String(), compressHostsMap(noopHosts))
		}
		for j := 0; j < noopRequests; j++ {
			logger.Printf("Waiting for (%d) outstanding noop requests: %s", noopRequests-j, compressHostsMap(noopHosts))
			r := <-puppetReportChan
			if r.err != nil {
				errNoopNodes[r.host] = fmt.Errorf("Noop failed for %s: %w", r.host, r.err)
				logger.Println(errNoopNodes[r.host])
				delete(nodes, r.host)
				delete(noopHosts, r.host)
				continue
			}
			newRprt := normalizeReport(r.report)
			logger.Printf("Received noop: %s@%s", newRprt.Host, newRprt.ConfigurationVersion)
			delete(noopHosts, newRprt.Host)
			commitId, err := git.NewOid(newRprt.ConfigurationVersion)
			if err != nil {
				logger.Fatalf("Unable to marshal report: %v", err)
			}
			nodes[newRprt.Host].commitReports[*commitId] = &newRprt
			err = marshalReport(newRprt, noopDir, *commitId)
			if err != nil {
				logger.Fatalf("Unable to marshal report: %v", err)
			}
		}
	}

	for _, node := range nodes {
		// PERMA-DIFF: If the commit is already applied we can assume that the
		// delta of resources is empty. NOTE: Ideally we would not need this
		// special case as the noop for an already applied commit should be empty,
		// but we purposefully substitute the noop of the last applied commit in
		// loadNoop() to subtract away perma-diffs, so those would show up without
		// this special case.
		logger.Printf("Creating delta resource for commit %s@%s", node.host, commit.Id().String())
		if commitAlreadyApplied(node.lastApply, *commit.Id(), repo) {
			node.commitDeltaResources[*commit.Id()] = make(map[ResourceTitle]*deltaResource)
		} else {
			node.commitDeltaResources[*commit.Id()] = subtractNoops(node.commitReports[*commit.Id()], commitParentReports(*commit, node.lastApply, node.commitReports, repo, logger))
		}
	}

	logger.Printf("Grouping: %s", commit.Id().String())
	groupedCommit := make([]*groupedResource, 0)
	for _, node := range nodes {
		for _, nodeDeltaRes := range node.commitDeltaResources[*commit.Id()] {
			groupedCommit = append(groupedCommit, groupResources(*commit.Id(), nodeDeltaRes, nodes))
		}
	}
	return groupedCommit, errNoopNodes, nil
}

func priorEvent(event *rizzopb.Event, resourceTitleStr string, priorCommitNoops []*rizzopb.PuppetReport) bool {
	for _, priorCommitNoop := range priorCommitNoops {
		if priorCommitNoop == nil {
			log.Fatalf("Error: prior commit noop was nil!")
		}
		if priorCommitNoop.ResourceStatuses == nil {
			continue
		}
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

func priorLog(curLog *rizzopb.Log, priorCommitNoops []*rizzopb.PuppetReport) bool {
	for _, priorCommitNoop := range priorCommitNoops {
		if priorCommitNoop == nil {
			log.Fatalf("Error: prior commit noop was nil!")
		}
		if priorCommitNoop.Logs == nil {
			continue
		}
		for _, priorLog := range priorCommitNoop.Logs {
			if *curLog == *priorLog {
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
	deltaRes.File = r.File
	deltaRes.Line = r.Line
	return deltaRes
}

func subtractNoops(commitNoop *rizzopb.PuppetReport, priorCommitNoops []*rizzopb.PuppetReport) map[ResourceTitle]*deltaResource {
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

func compressHosts(hosts []string) string {
	interfaceHosts := make([]interface{}, len(hosts))
	for i, v := range hosts {
		interfaceHosts[i] = v
	}
	res := grange.NewResult(interfaceHosts...)
	return grange.Compress(&res)
}

func compressHostsMap(hostsMap map[string]bool) string {
	hosts := make([]string, len(hostsMap))
	i := 0
	for k, _ := range hostsMap {
		hosts[i] = k
		i++
	}
	return compressHosts(hosts)
}

func compressErrorHosts(hostErr map[string]error) map[string]error {
	var errType string
	errHosts := make(map[string][]string)
	for host, err := range hostErr {
		// If we don't have a custom error, than don't compress, this is a bit of a
		// wack a mole approach.
		if fmt.Sprintf("%T", err) == "*errors.errorString" ||
			fmt.Sprintf("%T", err) == "*fmt.wrapError" {
			errType = fmt.Sprintf("%v", err)
		} else {
			errType = fmt.Sprintf("%T", err)
		}
		if hosts, ok := errHosts[errType]; ok {
			errHosts[errType] = append(hosts, host)
		} else {
			errHosts[errType] = []string{host}
		}
	}
	compressedHostErr := make(map[string]error)
	for errType, hosts := range errHosts {
		compressedHostErr[compressHosts(hosts)] = fmt.Errorf("Error: %s", errType)
	}
	return compressedHostErr
}

func compressHostsStr(hostsStr map[string]string) map[string]string {
	strHosts := make(map[string][]string)
	for host, str := range hostsStr {
		if hosts, ok := strHosts[str]; ok {
			strHosts[str] = append(hosts, host)
		} else {
			strHosts[str] = []string{host}
		}
	}
	compressedHostStr := make(map[string]string)
	for str, hosts := range strHosts {
		compressedHostStr[compressHosts(hosts)] = str
	}
	return compressedHostStr
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
			// TODO: cmp is not recommended to be used in production, because it
			// panics on any errors. It would probably be better to write a custom
			// compare function which can account for skipping fields which are not
			// relavant to the desired measure of equality, e.g. source file line
			// number
			if cmp.Equal(targetDeltaResource, nodeDeltaResource, cmpopts.IgnoreFields(deltaResource{}, "Line")) {
				nodeList = append(nodeList, nodeName)
				delete(node.commitDeltaResources[commitLogId], targetDeltaResource.Title)
			}
		}
	}

	gr = new(groupedResource)
	gr.Title = targetDeltaResource.Title
	gr.Type = targetDeltaResource.Type
	gr.DefineType = targetDeltaResource.DefineType
	gr.File = targetDeltaResource.File
	gr.Line = targetDeltaResource.Line
	sort.Strings(nodeList)
	gr.Nodes = nodeList
	gr.CompressedNodes = compressHosts(nodeList)

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
			// TODO: If we wrote a custom equality function, rather than using cmp,
			// we could ignore source code line numbers in logs, but since we are using
			// cmp at present, just set it equal to 0 for all logs.
			l.Line = 0
			newLogs = append(newLogs, l)
		} else {
			log.Printf("Unaccounted for Log: %v: %v", l.Source, l.Message)
			newLogs = append(newLogs, l)
		}
	}

	return newLogs
}

func hecklerApply(node *Node, c chan<- applyResult, par rizzopb.PuppetApplyRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*300)
	defer cancel()
	r, err := node.rizzoClient.PuppetApply(ctx, &par)
	if err != nil {
		c <- applyResult{
			host:   node.host,
			report: rizzopb.PuppetReport{},
			err:    fmt.Errorf("hecklerApply error from %s, returning any empty report: %w", node.host, err),
		}
		return
	}
	if ctx.Err() != nil {
		c <- applyResult{
			host:   node.host,
			report: rizzopb.PuppetReport{},
			err:    fmt.Errorf("hecklerApply context error from %s, returning any empty report: %w", node.host, ctx.Err()),
		}
		return
	}
	c <- applyResult{
		host:   node.host,
		report: *r,
		err:    nil,
	}
	return
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

func reqNodes(conf *HecklerdConf, nodes []string, nodeSetName string, logger *log.Logger) ([]string, error) {
	if len(nodes) > 0 {
		return nodes, nil
	}
	return nodesFromSet(conf, nodeSetName, logger)
}

func nodesFromSet(conf *HecklerdConf, nodeSetName string, logger *log.Logger) ([]string, error) {
	if nodeSetName == "" {
		return nil, errors.New("Empty nodeSetName provided")
	}
	var nodeSet NodeSet
	var ok bool
	if nodeSet, ok = conf.NodeSets[nodeSetName]; !ok {
		return nil, errors.New(fmt.Sprintf("nodeSetName '%s' not found in hecklerd config", nodeSetName))
	}
	// Change to code dir, so hiera relative paths resolve
	cmd := exec.Command(nodeSet.Cmd[0], nodeSet.Cmd[1:]...)
	cmd.Dir = stateDir + "/work_repo/puppetcode"
	stdout, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var nodes []string
	err = json.Unmarshal(stdout, &nodes)
	if err != nil {
		return nil, err
	}

	regexes := make([]*regexp.Regexp, 0)
	for _, sregex := range nodeSet.Blacklist {
		regex, err := regexp.Compile(sregex)
		if err != nil {
			return nil, err
		}
		regexes = append(regexes, regex)
	}

	filteredNodes := make([]string, 0)
	blacklistedNodes := make([]string, 0)
	for _, node := range nodes {
		blacklisted := false
		for _, regex := range regexes {
			if regex.MatchString(node) {
				blacklisted = true
				break
			}
		}
		if blacklisted {
			blacklistedNodes = append(blacklistedNodes, node)
		} else {
			filteredNodes = append(filteredNodes, node)
		}
	}

	if len(filteredNodes) == 0 {
		return nil, errors.New(fmt.Sprintf("Node set '%s': '%v' produced zero nodes", nodeSetName, nodeSet))
	}
	logger.Printf("Node set '%s' loaded, nodes (%d), blacklisted nodes (%d): %s", nodeSetName, len(filteredNodes), len(blacklistedNodes), compressHosts(blacklistedNodes))
	return filteredNodes, nil
}

func (hs *hecklerServer) HecklerApply(ctx context.Context, req *hecklerpb.HecklerApplyRequest) (*hecklerpb.HecklerApplyReport, error) {
	logger := log.New(os.Stdout, "[HecklerApply] ", log.Lshortfile)
	commit, err := gitutil.RevparseToCommit(req.Rev, hs.repo)
	if err != nil {
		return nil, err
	}
	nodesToDial, err := reqNodes(hs.conf, req.Nodes, req.NodeSet, logger)
	if err != nil {
		return nil, err
	}
	dialedNodes, errDialNodes := dialNodes(ctx, nodesToDial)
	defer closeNodes(dialedNodes)
	lockedNodes, lockedByAnotherNodes, errLockNodes := lockNodes(req.User, hs.conf.LockMessage, false, dialedNodes)
	defer unlockNodes(req.User, false, lockedNodes, logger)
	errNodes := make(map[string]error)
	for host, err := range errDialNodes {
		errNodes[host] = err
	}
	for host, err := range errLockNodes {
		errNodes[host] = err
	}
	har := new(hecklerpb.HecklerApplyReport)
	if req.Noop {
		lastApplyNodes, errUnknownRevNodes := nodeLastApply(lockedNodes, hs.repo, logger)
		for host, err := range errUnknownRevNodes {
			errNodes[host] = err
		}
		for _, node := range lastApplyNodes {
			node.commitReports = make(map[git.Oid]*rizzopb.PuppetReport)
			node.commitDeltaResources = make(map[git.Oid]map[ResourceTitle]*deltaResource)
		}
		groupedCommit, errNoopNodes, err := noopCommit(lastApplyNodes, commit, req.DeltaNoop, hs.repo, logger)
		if err != nil {
			return nil, err
		}
		for host, err := range errNoopNodes {
			errNodes[host] = err
		}
		har.Output = commitToMarkdown(hs.conf, commit, groupedCommit, hs.templates)
	} else {
		appliedNodes, beyondRevNodes, errApplyNodes := applyNodes(lockedNodes, req.Force, req.Noop, req.Rev, hs.repo, logger)
		for host, err := range errApplyNodes {
			errNodes[host] = err
		}
		if req.Force {
			har.Output = fmt.Sprintf("Applied nodes: (%d); Error nodes: (%d)", len(appliedNodes), len(errNodes))
		} else {
			har.Output = fmt.Sprintf("Applied nodes: (%d); Beyond rev nodes: (%d); Error nodes: (%d)", len(appliedNodes), len(beyondRevNodes), len(errNodes))
		}
	}
	har.NodeErrors = make(map[string]string)
	for host, err := range errNodes {
		har.NodeErrors[host] = err.Error()
	}
	for host, lockState := range lockedByAnotherNodes {
		har.NodeErrors[host] = lockState
	}
	return har, nil
}

func applyNodes(lockedNodes map[string]*Node, forceApply bool, noop bool, rev string, repo *git.Repository, logger *log.Logger) (appliedNodes []string, beyondRevNodes []string, errNodes map[string]error) {
	var nodesToApply map[string]*Node
	beyondRevNodes = make([]string, 0)
	lastApplyNodes := make(map[string]*Node)
	errUnknownRevNodes := make(map[string]error)
	// No need to check node revision if force applying
	if forceApply {
		nodesToApply = lockedNodes
	} else {
		lastApplyNodes, errUnknownRevNodes = nodeLastApply(lockedNodes, repo, logger)
		obj, err := gitutil.RevparseToCommit(rev, repo)
		if err != nil {
			logger.Fatalf("Unable to parse rev: '%s'", rev)
		}
		revId := *obj.Id()
		nodesToApply = make(map[string]*Node)
		for host, node := range lastApplyNodes {
			if commitAlreadyApplied(node.lastApply, revId, repo) {
				beyondRevNodes = append(beyondRevNodes, host)
			} else {
				nodesToApply[host] = node
			}
		}
	}
	par := rizzopb.PuppetApplyRequest{Rev: rev, Noop: noop}
	puppetReportChan := make(chan applyResult)
	for _, node := range nodesToApply {
		go hecklerApply(node, puppetReportChan, par)
	}

	errApplyNodes := make(map[string]error)
	appliedNodes = make([]string, 0)
	for range nodesToApply {
		r := <-puppetReportChan
		if r.err != nil {
			errApplyNodes[r.host] = fmt.Errorf("Apply failed: %w", r.err)
		} else if r.report.Status == "failed" {
			errApplyNodes[r.report.Host] = fmt.Errorf("Apply status=failed, %s@%s", r.report.Host, r.report.ConfigurationVersion)
		} else {
			if noop {
				logger.Printf("Nooped: %s@%s", r.report.Host, r.report.ConfigurationVersion)
			} else {
				logger.Printf("Applied: %s@%s", r.report.Host, r.report.ConfigurationVersion)
			}
			appliedNodes = append(appliedNodes, r.report.Host)
		}
	}
	errNodes = make(map[string]error)
	for host, err := range errUnknownRevNodes {
		errNodes[host] = err
	}
	for host, err := range errApplyNodes {
		errNodes[host] = err
	}
	return appliedNodes, beyondRevNodes, errNodes
}

func (hs *hecklerServer) HecklerNoopRange(ctx context.Context, req *hecklerpb.HecklerNoopRangeRequest) (*hecklerpb.HecklerNoopRangeReport, error) {
	logger := log.New(os.Stdout, "[HecklerNoopRange] ", log.Lshortfile)
	nodesToDial, err := reqNodes(hs.conf, req.Nodes, req.NodeSet, logger)
	if err != nil {
		return nil, err
	}
	dialedNodes, errDialNodes := dialNodes(ctx, nodesToDial)
	defer closeNodes(dialedNodes)
	lastApplyNodes, errUnknownRevNodes := nodeLastApply(dialedNodes, hs.repo, logger)
	lockedNodes, lockedByAnotherNodes, errLockNodes := lockNodes("root", hs.conf.LockMessage, false, lastApplyNodes)
	commitLogIds, commits, err := commitLogIdList(hs.repo, req.BeginRev, req.EndRev)
	if err != nil {
		return nil, err
	}
	for _, node := range lockedNodes {
		node.commitReports = make(map[git.Oid]*rizzopb.PuppetReport)
		node.commitDeltaResources = make(map[git.Oid]map[ResourceTitle]*deltaResource)
	}
	rprt := new(hecklerpb.HecklerNoopRangeReport)
	rprt.NodeErrors = make(map[string]string)
	groupedCommits := make(map[git.Oid][]*groupedResource)
	for gi, commit := range commits {
		groupedCommit, errNoopNodes, err := noopCommit(lockedNodes, commit, true, hs.repo, logger)
		if err != nil {
			unlockNodes("root", false, lockedNodes, logger)
			return nil, err
		}
		for host, err := range errNoopNodes {
			rprt.NodeErrors[host] = err.Error()
		}
		groupedCommits[gi] = groupedCommit
	}
	unlockNodes("root", false, lockedNodes, logger)
	if req.OutputFormat == hecklerpb.OutputFormat_markdown {
		rprt.Output = commitRangeToMarkdown(hs.conf, commitLogIds, commits, groupedCommits, hs.templates)
	}
	for host, err := range errDialNodes {
		rprt.NodeErrors[host] = err.Error()
	}
	for host, err := range errLockNodes {
		rprt.NodeErrors[host] = err.Error()
	}
	for host, err := range errUnknownRevNodes {
		rprt.NodeErrors[host] = err.Error()
	}
	for host, lockState := range lockedByAnotherNodes {
		rprt.NodeErrors[host] = fmt.Sprintf("lockedByAnother: %s, %v", host, lockState)
	}
	return rprt, nil
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
		defer file.Close()
		privateKey, err = ioutil.ReadAll(file)
	} else if _, err := os.Stat("github-private-key.pem"); err == nil {
		file, err = os.Open("github-private-key.pem")
		if err != nil {
			return nil, nil, err
		}
		defer file.Close()
		privateKey, err = ioutil.ReadAll(file)
	} else {
		return nil, nil, errors.New("Unable to load github-private-key.pem in /etc/hecklerd or .")
	}
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

func createMilestone(milestone string, ghclient *github.Client, conf *HecklerdConf) (*github.Milestone, error) {
	ms := &github.Milestone{
		Title: github.String(milestone),
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	nms, _, err := ghclient.Issues.CreateMilestone(ctx, conf.RepoOwner, conf.Repo, ms)
	if err != nil {
		return nil, err
	}
	return nms, nil
}

func closeMilestone(milestone string, ghclient *github.Client, conf *HecklerdConf) error {
	ms, err := milestoneFromTag(milestone, ghclient, conf)
	if err != nil {
		return err
	}
	ms.State = github.String("closed")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	_, _, err = ghclient.Issues.EditMilestone(ctx, conf.RepoOwner, conf.Repo, *ms.Number, ms)
	if err != nil {
		return err
	}
	return nil
}

func milestoneFromTag(milestone string, ghclient *github.Client, conf *HecklerdConf) (*github.Milestone, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	milestoneOpts := &github.MilestoneListOptions{
		State: "all",
	}
	allMilestones, _, err := ghclient.Issues.ListMilestones(ctx, conf.RepoOwner, conf.Repo, milestoneOpts)
	if err != nil {
		return nil, err
	}
	for _, ms := range allMilestones {
		if *ms.Title == milestone {
			return ms, nil
		}
	}
	return nil, nil
}

// Given a git oid this function returns the associated github issue, if it
// exists
func githubIssueFromCommit(ghclient *github.Client, oid git.Oid, conf *HecklerdConf) (*github.Issue, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	prefix := conf.EnvPrefix
	query := fmt.Sprintf("%s in:title", oid.String())
	if prefix != "" {
		query += fmt.Sprintf(" %sin:title", issuePrefix(prefix))
	}
	query += fmt.Sprintf(" author:app/%s", conf.GitHubAppSlug)
	searchResults, _, err := ghclient.Search.Issues(ctx, query, nil)
	if err != nil {
		log.Fatal(err)
	}
	if searchResults.GetTotal() == 0 {
		return nil, nil
	} else if searchResults.GetTotal() == 1 {
		return &searchResults.Issues[0], nil
	} else {
		return nil, errors.New("More than one issue exists for a single commit")
	}
}

func clearMilestones(ghclient *github.Client, conf *HecklerdConf) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*100)
	defer cancel()
	milestoneOpts := &github.MilestoneListOptions{
		State: "all",
	}
	milestones, _, err := ghclient.Issues.ListMilestones(ctx, conf.RepoOwner, conf.Repo, milestoneOpts)
	if err != nil {
		log.Fatal(err)
	}
	var msTitle string
	for _, ms := range milestones {
		msTitle = *ms.Title
		if *ms.Creator.Type == "Bot" &&
			*ms.Creator.Login == fmt.Sprintf("%s[bot]", conf.GitHubAppSlug) &&
			strings.HasPrefix(*ms.Title, tagPrefix(conf.EnvPrefix)) {
			_, err := ghclient.Issues.DeleteMilestone(ctx, conf.RepoOwner, conf.Repo, *ms.Number)
			if err != nil {
				log.Fatal(err)
			}
			log.Printf("Deleted milestone: '%s'", msTitle)
		}
	}
	return nil
}

func issuePrefix(prefix string) string {
	if prefix == "" {
		return ""
	} else {
		return fmt.Sprintf("[%s-env] ", prefix)
	}
}

func clearIssues(ghclient *github.Client, conf *HecklerdConf) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*100)
	defer cancel()
	query := fmt.Sprintf("author:app/%s noop in:title", conf.GitHubAppSlug)
	if conf.EnvPrefix != "" {
		query += fmt.Sprintf(" %sin:title", issuePrefix(conf.EnvPrefix))
	}
	searchResults, _, err := ghclient.Search.Issues(ctx, query, nil)
	if err != nil {
		log.Fatal(err)
	}
	// The rest API does not support deletion,
	// https://github.community/t5/GitHub-API-Development-and/Delete-Issues-programmatically/td-p/29524,
	// so for now just close the issue and change the title to deleted & remove the milestone
	issuePatch := &github.IssueRequest{
		Title:     github.String("SoftDeleted"),
		Milestone: nil,
		State:     github.String("closed"),
	}
	for _, issue := range searchResults.Issues {
		if *issue.Title != "SoftDeleted" {
			_, _, err := ghclient.Issues.Edit(ctx, conf.RepoOwner, conf.Repo, *issue.Number, issuePatch)
			if err != nil {
				log.Fatal(err)
			}
			log.Printf("Soft deleted issue: '%s'", *issue.Title)
		}
	}
	return nil
}

func updateIssueMilestone(ghclient *github.Client, conf *HecklerdConf, issue *github.Issue, ms *github.Milestone) error {
	issuePatch := &github.IssueRequest{
		Milestone: ms.Number,
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	_, _, err := ghclient.Issues.Edit(ctx, conf.RepoOwner, conf.Repo, *issue.Number, issuePatch)
	return err
}

func closeIssue(ghclient *github.Client, conf *HecklerdConf, issue *github.Issue, reason string) error {
	comment := &github.IssueComment{
		Body: github.String(reason),
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	_, _, err := ghclient.Issues.CreateComment(ctx, conf.RepoOwner, conf.Repo, *issue.Number, comment)
	if err != nil {
		return err
	}
	issuePatch := &github.IssueRequest{
		State: github.String("closed"),
	}
	ctx, cancel = context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	_, _, err = ghclient.Issues.Edit(ctx, conf.RepoOwner, conf.Repo, *issue.Number, issuePatch)
	return err
}

func githubCreateIssue(ghclient *github.Client, conf *HecklerdConf, commit *git.Commit, groupedCommit []*groupedResource, templates *template.Template, logger *log.Logger) error {
	ctx := context.Background()
	prefix := conf.EnvPrefix

	githubIssue := &github.IssueRequest{
		Title: github.String(noopTitle(commit, prefix)),
		// TODO need to enforce github user IDs for commits, so that we always
		// have a valid github user.
		Assignee: github.String("lollipopman"),
		Body:     github.String(commitBodyToMarkdown(commit, conf, templates) + groupedResourcesToMarkdown(groupedCommit, commit, conf, templates)),
	}
	ni, _, err := ghclient.Issues.Create(ctx, conf.RepoOwner, conf.Repo, githubIssue)
	if err != nil {
		logger.Fatal(err)
	}
	logger.Printf("Successfully created new issue: '%v'", *ni.Title)
	if len(groupedCommit) == 0 {
		err := closeIssue(ghclient, conf, ni, "No noop output marking issue as 'closed'")
		if err != nil {
			logger.Fatal(err)
		}
	} else if conf.AutoCloseIssues {
		err := closeIssue(ghclient, conf, ni, "Auto close set, marking issue as 'closed'")
		if err != nil {
			logger.Fatal(err)
		}
	} else {
		err = notifyApprovers(ghclient, conf, ni, groupedCommit, logger)
		if err != nil {
			logger.Fatal(err)
		}
	}
	return nil
}

func notifyApprovers(ghclient *github.Client, conf *HecklerdConf, issue *github.Issue, groupedCommit []*groupedResource, logger *log.Logger) error {
	// Unique nodes
	nodes := make(map[string]bool)
	for _, gr := range groupedCommit {
		for _, node := range gr.Nodes {
			nodes[node] = true
		}
	}
	nodeList := make([]string, len(nodes))
	i := 0
	for node := range nodes {
		nodeList[i] = node
		i++
	}
	approvers, err := approversFromNodes(nodeList)
	if err != nil {
		return err
	}
	body := "Please approve: " + strings.Join(approvers, ", ")
	comment := &github.IssueComment{
		Body: github.String(body),
	}
	ctx := context.Background()
	_, _, err = ghclient.Issues.CreateComment(ctx, conf.RepoOwner, conf.Repo, *issue.Number, comment)
	if err != nil {
		return err
	}
	return nil
}

func nodeFile(node string, nodeFileRegexes map[string][]*regexp.Regexp) (string, error) {
	for file, regexes := range nodeFileRegexes {
		for _, regex := range regexes {
			if regex.MatchString(node) {
				return strings.TrimPrefix(file, workRepo+"/"), nil
			}
		}
	}
	return "", fmt.Errorf("Unable to find node file for node: %v", node)
}

func approversFromNodes(nodes []string) ([]string, error) {
	co, err := codeowners.NewCodeowners(workRepo)
	if err != nil {
		return []string{}, err
	}
	nodeFileRegexes, err := puppetutil.NodeFileRegexes(workRepo + "/nodes")
	if err != nil {
		return []string{}, err
	}
	nodesToFile := make(map[string]string)
	for _, node := range nodes {
		nodesToFile[node], err = nodeFile(node, nodeFileRegexes)
		if err != nil {
			return []string{}, err
		}
	}
	uniqueApprovers := make(map[string]bool)
	for _, file := range nodesToFile {
		owners := co.Owners(file)
		for _, owner := range owners {
			uniqueApprovers[owner] = true
		}
	}
	approversList := make([]string, 0)
	for approver := range uniqueApprovers {
		approversList = append(approversList, approver)
	}
	return approversList, nil
}

func noopTitle(commit *git.Commit, prefix string) string {
	return fmt.Sprintf("%sPuppet noop output for commit: %s - %s", issuePrefix(prefix), commit.Id().String(), commit.Summary())
}

func commitRangeToMarkdown(conf *HecklerdConf, commitLogIds []git.Oid, commits map[git.Oid]*git.Commit, groupedCommits map[git.Oid][]*groupedResource, templates *template.Template) string {
	var output string
	for _, gi := range commitLogIds {
		output += commitToMarkdown(conf, commits[gi], groupedCommits[gi], templates)
	}
	return output
}

func commitToMarkdown(conf *HecklerdConf, commit *git.Commit, groupedCommit []*groupedResource, templates *template.Template) string {
	var output string
	output += fmt.Sprintf("## %s\n\n", noopTitle(commit, conf.EnvPrefix))
	output += commitBodyToMarkdown(commit, conf, templates)
	output += groupedResourcesToMarkdown(groupedCommit, commit, conf, templates)
	return output
}

func commitBodyToMarkdown(commit *git.Commit, conf *HecklerdConf, templates *template.Template) string {
	var body strings.Builder
	var err error

	data := struct {
		Commit *git.Commit
		Conf   *HecklerdConf
	}{
		commit,
		conf,
	}
	err = templates.ExecuteTemplate(&body, "commit.tmpl", data)
	if err != nil {
		log.Fatal(err)
	}
	return body.String()
}

func groupedResourcesToMarkdown(groupedResources []*groupedResource, commit *git.Commit, conf *HecklerdConf, templates *template.Template) string {
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

	data := struct {
		GroupedResources []*groupedResource
		Commit           *git.Commit
		Conf             *HecklerdConf
	}{
		groupedResources,
		commit,
		conf,
	}
	err = templates.ExecuteTemplate(&body, "groupedResource.tmpl", data)
	if err != nil {
		log.Fatal(err)
	}
	return body.String()
}

func (hs *hecklerServer) HecklerStatus(ctx context.Context, req *hecklerpb.HecklerStatusRequest) (*hecklerpb.HecklerStatusReport, error) {
	logger := log.New(os.Stdout, "[HecklerStatus] ", log.Lshortfile)
	nodesToDial, err := reqNodes(hs.conf, req.Nodes, req.NodeSet, logger)
	if err != nil {
		return nil, err
	}
	dialedNodes, errNodes := dialNodes(ctx, nodesToDial)
	defer closeNodes(dialedNodes)
	lastApplyNodes, errUnknownRevNodes := nodeLastApply(dialedNodes, hs.repo, logger)
	hsr := new(hecklerpb.HecklerStatusReport)
	hsr.NodeStatuses = make(map[string]string)
	var tagStr string
	for _, node := range lastApplyNodes {
		tagStr, err = describeCommit(node.lastApply, hs.conf.EnvPrefix, hs.repo)
		if err != nil {
			tagStr = "NONE"
		}
		hsr.NodeStatuses[node.host] = "commit: " + node.lastApply.String() + ", last-tag: " + tagStr
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
	logger := log.New(os.Stdout, "[HecklerUnlock] ", log.Lshortfile)
	nodesToDial, err := reqNodes(hs.conf, req.Nodes, req.NodeSet, logger)
	if err != nil {
		return nil, err
	}
	dialedNodes, errNodes := dialNodes(ctx, nodesToDial)
	defer closeNodes(dialedNodes)
	unlockedNodes, lockedByAnotherNodes, errUnlockNodes := unlockNodes(req.User, req.Force, dialedNodes, logger)
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
	for host, str := range lockedByAnotherNodes {
		res.NodeErrors[host] = "Locked by another, " + str
	}
	return res, nil
}

func closeNodes(nodes map[string]*Node) {
	for _, node := range nodes {
		if node.grpcConn != nil {
			node.grpcConn.Close()
		}
	}
}

func (hs *hecklerServer) HecklerLock(ctx context.Context, req *hecklerpb.HecklerLockRequest) (*hecklerpb.HecklerLockReport, error) {
	logger := log.New(os.Stdout, "[HecklerLock] ", log.Lshortfile)
	nodesToDial, err := reqNodes(hs.conf, req.Nodes, req.NodeSet, logger)
	if err != nil {
		return nil, err
	}
	dialedNodes, errNodes := dialNodes(ctx, nodesToDial)
	defer closeNodes(dialedNodes)
	lockedNodes, lockedByAnother, errLockNodes := lockNodes(req.User, req.Comment, req.Force, dialedNodes)
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
	// TODO: not technically an error?
	for host, lockState := range lockedByAnother {
		res.NodeErrors[host] = lockState
	}
	return res, nil
}

func lockNodes(user string, comment string, force bool, nodes map[string]*Node) (map[string]*Node, map[string]string, map[string]error) {
	lockReq := rizzopb.PuppetLockRequest{
		Type:    rizzopb.LockReqType_lock,
		User:    user,
		Comment: comment,
		Force:   force,
	}
	lockedNodes, _, lockedByAnotherNodes, errLockNodes := rizzoLockNodes(lockReq, nodes)
	return lockedNodes, lockedByAnotherNodes, errLockNodes
}

func unlockNodes(user string, force bool, nodes map[string]*Node, logger *log.Logger) (map[string]*Node, map[string]string, map[string]error) {
	lockReq := rizzopb.PuppetLockRequest{
		Type:  rizzopb.LockReqType_unlock,
		User:  user,
		Force: force,
	}
	_, unlockedNodes, lockedByAnotherNodes, errLockNodes := rizzoLockNodes(lockReq, nodes)
	compressedLockedByAnotherNodes := compressHostsStr(lockedByAnotherNodes)
	if len(unlockedNodes) == len(nodes) {
		logger.Printf("Unlocked all %d requested nodes", len(unlockedNodes))
	} else {
		logger.Printf("Tried to unlock %d nodes, but only succeeded in unlocking, %d", len(nodes), len(unlockedNodes))
	}
	for host, str := range compressedLockedByAnotherNodes {
		logger.Printf("Unlock requested, but locked by another: %s, %s", host, str)
	}
	compressedErrNodes := compressErrorHosts(errLockNodes)
	for host, err := range compressedErrNodes {
		logger.Printf("Unlock failed, errNodes: %s, %v", host, err)
	}
	return unlockedNodes, lockedByAnotherNodes, errLockNodes
}

func nodesLockState(user string, nodes map[string]*Node) (map[string]*Node, map[string]*Node, map[string]string, map[string]error) {
	lockReq := rizzopb.PuppetLockRequest{
		Type: rizzopb.LockReqType_state,
		User: user,
	}
	lockedNodes, unlockedNodes, lockedByAnotherNodes, errLockNodes := rizzoLockNodes(lockReq, nodes)
	return lockedNodes, unlockedNodes, lockedByAnotherNodes, errLockNodes
}

func rizzoLockNodes(req rizzopb.PuppetLockRequest, nodes map[string]*Node) (map[string]*Node, map[string]*Node, map[string]string, map[string]error) {
	reportChan := make(chan rizzopb.PuppetLockReport)
	for _, node := range nodes {
		go rizzoLock(node.host, node.rizzoClient, req, reportChan)
	}

	lockedNodes := make(map[string]*Node)
	unlockedNodes := make(map[string]*Node)
	lockedByAnotherNodes := make(map[string]string)
	errNodes := make(map[string]error)
	for i := 0; i < len(nodes); i++ {
		r := <-reportChan
		switch r.LockStatus {
		case rizzopb.LockStatus_unlocked:
			unlockedNodes[r.Host] = nodes[r.Host]
		case rizzopb.LockStatus_locked_by_user:
			lockedNodes[r.Host] = nodes[r.Host]
		case rizzopb.LockStatus_locked_by_another:
			lockedByAnotherNodes[r.Host] = fmt.Sprintf("%s: %s", r.User, r.Comment)
		case rizzopb.LockStatus_lock_unknown:
			errNodes[r.Host] = errors.New(r.Error)
		default:
			log.Fatal("Unknown lockStatus!")
		}
	}

	return lockedNodes, unlockedNodes, lockedByAnotherNodes, errNodes
}

func rizzoLock(host string, rc rizzopb.RizzoClient, req rizzopb.PuppetLockRequest, c chan<- rizzopb.PuppetLockReport) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	res, err := rc.PuppetLock(ctx, &req)
	if err != nil {
		c <- rizzopb.PuppetLockReport{
			Host:       host,
			LockStatus: rizzopb.LockStatus_lock_unknown,
			Error:      err.Error(),
		}
		return
	}
	res.Host = host
	c <- *res
	return
}

func hecklerLastApply(node *Node, c chan<- rizzopb.PuppetReport, logger *log.Logger) {
	plar := rizzopb.PuppetLastApplyRequest{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	r, err := node.rizzoClient.PuppetLastApply(ctx, &plar)
	if err != nil {
		logger.Printf("Puppet lastApply error on %s substituting an empty report, %v", node.host, err)
		c <- rizzopb.PuppetReport{
			Host: node.host,
		}
		return
	}
	c <- *r
	return
}

func nodeLastApply(nodes map[string]*Node, repo *git.Repository, logger *log.Logger) (map[string]*Node, map[string]error) {
	var err error
	errNodes := make(map[string]error)
	lastApplyNodes := make(map[string]*Node)

	puppetReportChan := make(chan rizzopb.PuppetReport)
	for _, node := range nodes {
		go hecklerLastApply(node, puppetReportChan, logger)
	}

	var obj *git.Object
	for range nodes {
		r := <-puppetReportChan
		if r.ConfigurationVersion == "" {
			errNodes[r.Host] = ErrLastApplyUnknown
			continue
		}
		obj, err = repo.RevparseSingle(r.ConfigurationVersion)
		if err != nil {
			errNodes[r.Host] = fmt.Errorf("Unable to revparse ConfigurationVersion, %s@%s: %v %w", r.ConfigurationVersion, r.Host, err, ErrLastApplyUnknown)
			continue
		}
		if node, ok := nodes[r.Host]; ok {
			node.lastApply = *obj.Id()
			lastApplyNodes[r.Host] = node
		} else {
			logger.Fatalf("No Node struct found for report from: %s", r.Host)
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
	cloneOptions := &git.CloneOptions{
		FetchOptions: &git.FetchOptions{
			UpdateFetchhead: true,
			DownloadTags:    git.DownloadTagsAll,
		},
		Bare: true,
	}
	remoteUrl := fmt.Sprintf("https://x-access-token:%s@%s/%s/%s", tok, conf.GitHubDomain, conf.RepoOwner, conf.Repo)
	bareRepo, err := gitutil.CloneOrOpen(remoteUrl, cloneDir, cloneOptions)
	if err != nil {
		return nil, err
	}
	err = gitutil.FastForward(bareRepo, cloneOptions.FetchOptions)
	if err != nil {
		return nil, err
	}
	return bareRepo, nil
}

// Is there a newer release tag than our common lastApply tag across "all"
// nodes?
//   If yes
//     Is there a milestone created for that version?
//       If no, do nothing
//       If yes
//         Get a list of all commits between tags
//         Does a github issue exist for each issue?
//          If no, do nothing
//          If yes
//            Are all issues assigned to the milestone & closed?
//             If yes
//               Close milestone
//               Apply new tag across all nodes
//             If no, do nothing
//   If no, do nothing
func applyLoop(conf *HecklerdConf, repo *git.Repository) {
	logger := log.New(os.Stdout, "[applyLoop] ", log.Lshortfile)
	prefix := conf.EnvPrefix
	for {
		time.Sleep(10 * time.Second)
		nodesToDial, err := nodesFromSet(conf, "all", logger)
		if err != nil {
			logger.Fatalf("Unable to load 'all' node set: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
		defer cancel()
		dialedNodes, errDialNodes := dialNodes(ctx, nodesToDial)
		eligibleNodes, lockedByAnotherNodes, errEligibleNodes := eligibleNodes("root", dialedNodes)
		compressedLockedByAnotherNodes := compressHostsStr(lockedByAnotherNodes)
		for host, str := range compressedLockedByAnotherNodes {
			logger.Printf("Locked by another: %s, %s", host, str)
		}
		lastApplyNodes, errUnknownRevNodes := nodeLastApply(eligibleNodes, repo, logger)
		errNodes := make(map[string]error)
		for host, err := range errDialNodes {
			errNodes[host] = err
		}
		for host, err := range errEligibleNodes {
			errNodes[host] = err
		}
		for host, err := range errUnknownRevNodes {
			errNodes[host] = err
		}
		commonTag, err := commonAncestorTag(lastApplyNodes, prefix, repo, logger)
		if err != nil {
			logger.Printf("Unable to find common tag, sleeping: %v", err)
			closeNodes(dialedNodes)
			continue
		}
		if commonTag == "" {
			logger.Println("No common tag found, sleeping")
			closeNodes(dialedNodes)
			continue
		}
		logger.Printf("Found common tag: %s", commonTag)
		ghclient, _, err := githubConn(conf)
		if err != nil {
			logger.Fatalf("Unable to connect to github: %v", err)
		}
		nextTag, err := nextTag(commonTag, conf.EnvPrefix, repo)
		if err != nil {
			logger.Fatalf("Unable to find next tag: %v", err)
		}
		if nextTag == "" {
			logger.Println("No nextTag found, sleeping")
			closeNodes(dialedNodes)
			continue
		}
		priorTag := commonTag
		tagIssuesReviewed, err := tagIssuesReviewed(repo, ghclient, conf, priorTag, nextTag)
		if err != nil {
			logger.Fatalf("Unable to find next tag: %v", err)
		}
		if tagIssuesReviewed {
			err = closeMilestone(nextTag, ghclient, conf)
			if err != nil {
				logger.Fatalf("Unable to close miletstone: %v", err)
			}
		} else {
			logger.Printf("Tag '%s' is not ready to apply, sleeping", nextTag)
			closeNodes(dialedNodes)
			continue
		}
		logger.Printf("Tag '%s' is ready to apply, applying...", nextTag)
		lockedNodes, lockedByAnotherNodes, errLockNodes := lockNodes("root", conf.LockMessage, false, lastApplyNodes)
		appliedNodes, beyondRevNodes, errApplyNodes := applyNodes(lockedNodes, false, false, nextTag, repo, logger)
		unlockNodes("root", false, lockedNodes, logger)
		for host, err := range errLockNodes {
			errNodes[host] = err
		}
		for host, err := range errApplyNodes {
			errNodes[host] = err
		}
		for host, lockState := range lockedByAnotherNodes {
			logger.Printf("lockedByAnother: %s, %v", host, lockState)
		}
		compressedErrNodes := compressErrorHosts(errNodes)
		for host, err := range compressedErrNodes {
			logger.Printf("errNodes: %s, %v", host, err)
		}
		logger.Printf("Applied nodes: (%d); Beyond rev nodes: (%d); Error nodes: (%d)", len(appliedNodes), len(beyondRevNodes), len(errNodes))
		logger.Println("Apply complete, sleeping")
		closeNodes(dialedNodes)
	}
}

func unlockAll(conf *HecklerdConf, logger *log.Logger) {
	nodesToDial, err := nodesFromSet(conf, "all", logger)
	if err != nil {
		logger.Fatalf("Unable to load 'all' node set: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	dialedNodes, errDialNodes := dialNodes(ctx, nodesToDial)
	unlockedNodes, lockedByAnotherNodes, errUnlockNodes := unlockNodes("root", false, dialedNodes, logger)
	errNodes := make(map[string]error)
	unlockedHosts := make([]string, 0)
	for host, _ := range unlockedNodes {
		unlockedHosts = append(unlockedHosts, host)
	}
	logger.Printf("Unlocked: %s", compressHosts(unlockedHosts))
	for host, err := range errDialNodes {
		errNodes[host] = err
	}
	for host, err := range errUnlockNodes {
		errNodes[host] = err
	}
	compressedLockedByAnotherNodes := compressHostsStr(lockedByAnotherNodes)
	for host, str := range compressedLockedByAnotherNodes {
		logger.Printf("Locked by another: %s, %s", host, str)
	}
	compressedErrNodes := compressErrorHosts(errNodes)
	for host, err := range compressedErrNodes {
		logger.Printf("Unlock errNodes: %s, %v", host, err)
	}
}

// Are their commits beyond the last tag?
// 	If yes, create a new tag
// If no, do nothing
func tagNewCommits(conf *HecklerdConf, repo *git.Repository) {
	logger := log.New(os.Stdout, "[tagNewCommits] ", log.Lshortfile)
	headCommit, err := gitutil.RevparseToCommit("master", repo)
	if err != nil {
		logger.Fatalf("Unable to revparse: %v", err)
	}
	logger.Printf("Master commit, '%s'", headCommit.AsObject().Id().String())
	describeOpts, err := git.DefaultDescribeOptions()
	if err != nil {
		logger.Fatalf("Unable to set describe opts: %v", err)
	}
	prefix := conf.EnvPrefix
	describeOpts.Pattern = fmt.Sprintf("%sv*", tagPrefix(prefix))
	result, err := headCommit.Describe(&describeOpts)
	if err != nil {
		logger.Fatalf("Unable to describe: %v", err)
	}
	formatOpts, err := git.DefaultDescribeFormatOptions()
	formatOpts.AbbreviatedSize = 0
	if err != nil {
		logger.Fatalf("Unable to set format opts: %v", err)
	}
	mostRecentTag, err := result.Format(&formatOpts)
	if err != nil {
		logger.Fatalf("Unable to format describe output: %v", err)
	}
	logger.Printf("Most recent tag from commit, %s tag: '%s'", headCommit.AsObject().Id().String(), mostRecentTag)
	commitLogIds, _, err := commitLogIdList(repo, mostRecentTag, "master")
	if err != nil {
		logger.Fatalf("Unable to obtain commit log ids: %v", err)
	}
	if len(commitLogIds) == 0 {
		logger.Printf("No new commits since last tag: '%s'", mostRecentTag)
		return
	}
	ghclient, _, err := githubConn(conf)
	if err != nil {
		logger.Fatalf("Unable to connect to github: %v", err)
	}
	mostRecentVersion, err := tagToSemver(mostRecentTag, prefix)
	if err != nil {
		logger.Fatalf("Unable to parse version: %v", err)
	}
	newVersion := mostRecentVersion.IncMajor()
	newTag := fmt.Sprintf("%sv%d", tagPrefix(prefix), newVersion.Major())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	// Check if tag ref already exists
	refs, _, err := ghclient.Git.GetRefs(ctx, conf.RepoOwner, conf.Repo, fmt.Sprintf("refs/tags/%s", newTag))
	if len(refs) == 1 {
		logger.Printf("New tag ref already exists on github, skipping creation, '%s'", newTag)
		return
	}
	err = createTag(newTag, conf, ghclient, repo)
	if err != nil {
		logger.Fatalf("Unable to create new tag: %v", err)
	}
}

func createTag(newTag string, conf *HecklerdConf, ghclient *github.Client, repo *git.Repository) error {
	timeNow := time.Now()
	tagger := &github.CommitAuthor{
		Date:  &timeNow,
		Name:  github.String("Heckler"),
		Email: github.String("hathaway@paypal.com"),
	}
	commit, err := gitutil.RevparseToCommit("master", repo)
	if err != nil {
		return err
	}
	commitObj := &github.GitObject{
		Type: github.String("commit"),
		SHA:  github.String(commit.AsObject().Id().String()),
	}
	tagReq := &github.Tag{
		Tag:     github.String(newTag),
		Message: github.String("Auto Tagged by Heckler"),
		Tagger:  tagger,
		Object:  commitObj,
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	tag, _, err := ghclient.Git.CreateTag(ctx, conf.RepoOwner, conf.Repo, tagReq)
	if err != nil {
		return err
	}
	tagObj := &github.GitObject{
		Type: github.String("tag"),
		SHA:  github.String(tag.GetSHA()),
	}
	refReq := &github.Reference{
		Ref:    github.String(fmt.Sprintf("refs/tags/%s", newTag)),
		Object: tagObj,
	}
	_, _, err = ghclient.Git.CreateRef(ctx, conf.RepoOwner, conf.Repo, refReq)
	if err != nil {
		return err
	}
	return nil
}

func tagIssuesReviewed(repo *git.Repository, ghclient *github.Client, conf *HecklerdConf, priorTag string, nextTag string) (bool, error) {
	var nextTagMilestone *github.Milestone
	nextTagMilestone, err := milestoneFromTag(nextTag, ghclient, conf)
	if err != nil {
		return false, err
	}
	if nextTagMilestone == nil {
		return false, nil
	}
	// TODO support other branches?
	commitLogIds, _, err := commitLogIdList(repo, priorTag, nextTag)
	if err != nil {
		return false, err
	}
	// No new commits
	if len(commitLogIds) == 0 {
		return false, errors.New(fmt.Sprintf("No commits between versions: %s..%s", priorTag, nextTag))
	}
	for _, gi := range commitLogIds {
		issue, err := githubIssueFromCommit(ghclient, gi, conf)
		if err != nil {
			return false, err
		}
		if issue == nil {
			return false, nil
		}
		if issue.GetMilestone() == nil || *issue.GetMilestone().Title != nextTag {
			return false, nil
		}
		if issue.GetState() != "closed" {
			return false, nil
		}
	}
	return true, nil
}

func eligibleNodes(user string, nodes map[string]*Node) (map[string]*Node, map[string]string, map[string]error) {
	lockedNodes, unlockedNodes, lockedByAnotherNodes, errLockNodes := nodesLockState(user, nodes)
	eligibleNodes := make(map[string]*Node)
	for host, node := range lockedNodes {
		eligibleNodes[host] = node
	}
	for host, node := range unlockedNodes {
		eligibleNodes[host] = node
	}
	return eligibleNodes, lockedByAnotherNodes, errLockNodes
}

// Is there a newer release tag than our common lastApply tag across "all"
// nodes?
//   If yes
//     Is there a milestone created for that version?
//       If no, create the milestone
//       If yes, do nothing
//     Get a list of all commits between tags
//     Does a github issue exist for each issue?
//       If yes, associate issue with milestone
//       If no, do nothing
//   If no, do nothing
func milestoneLoop(conf *HecklerdConf, repo *git.Repository) {
	logger := log.New(os.Stdout, "[milestoneLoop] ", log.Lshortfile)
	prefix := conf.EnvPrefix
	for {
		time.Sleep(10 * time.Second)
		nodesToDial, err := nodesFromSet(conf, "all", logger)
		if err != nil {
			logger.Fatalf("Unable to load 'all' node set: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
		defer cancel()
		dialedNodes, errDialNodes := dialNodes(ctx, nodesToDial)
		eligibleNodes, lockedByAnotherNodes, errEligibleNodes := eligibleNodes("root", dialedNodes)
		compressedLockedByAnotherNodes := compressHostsStr(lockedByAnotherNodes)
		for host, str := range compressedLockedByAnotherNodes {
			logger.Printf("Locked by another: %s, %s", host, str)
		}
		lastApplyNodes, errUnknownRevNodes := nodeLastApply(eligibleNodes, repo, logger)
		errNodes := make(map[string]error)
		for host, err := range errEligibleNodes {
			errNodes[host] = err
		}
		for host, err := range errDialNodes {
			errNodes[host] = err
		}
		for host, err := range errUnknownRevNodes {
			errNodes[host] = err
		}
		compressedErrNodes := compressErrorHosts(errNodes)
		for host, err := range compressedErrNodes {
			logger.Printf("errNodes: %s, %v", host, err)
		}
		commonTag, err := commonAncestorTag(lastApplyNodes, prefix, repo, logger)
		if err != nil {
			logger.Printf("Unable to find common tag, sleeping: %v", err)
			closeNodes(dialedNodes)
			continue
		}
		if commonTag == "" {
			logger.Println("No common tag found, sleeping")
			closeNodes(dialedNodes)
			continue
		}
		logger.Printf("Found common tag: %s", commonTag)
		ghclient, _, err := githubConn(conf)
		if err != nil {
			logger.Fatalf("Unable to connect to github: %v", err)
		}
		nextTag, err := nextTag(commonTag, prefix, repo)
		if err != nil {
			logger.Fatalf("Unable to find next tag: %v", err)
		}
		if nextTag == "" {
			logger.Println("No nextTag found, sleeping")
			closeNodes(dialedNodes)
			continue
		}
		priorTag := commonTag
		var nextTagMilestone *github.Milestone
		nextTagMilestone, err = milestoneFromTag(nextTag, ghclient, conf)
		if err == context.DeadlineExceeded {
			logger.Println("Timeout reaching github for milestone, sleeping")
			closeNodes(dialedNodes)
			continue
		} else if err != nil {
			logger.Fatalf("Unable to find milestone from tag, '%s': %v", nextTag, err)
		}
		if nextTagMilestone == nil {
			nextTagMilestone, err = createMilestone(nextTag, ghclient, conf)
			if err != nil {
				logger.Fatalf("Unable to create new milestone for tag '%s': %v", nextTag, err)
			}
			logger.Printf("Successfully created new milestone: %v", *nextTagMilestone.Title)
		}
		// TODO support other branches?
		commitLogIds, _, err := commitLogIdList(repo, priorTag, nextTag)
		if err != nil {
			logger.Fatalf("Unable to obtain commit log ids: %v", err)
		}
		// No new commits
		if len(commitLogIds) == 0 {
			logger.Fatalf("No commits between versions: %s..%s", priorTag, nextTag)
		}
		for _, gi := range commitLogIds {
			issue, err := githubIssueFromCommit(ghclient, gi, conf)
			if err != nil {
				logger.Fatalf("Unable to determine if issues exists: %s", gi.String())
			}
			if issue != nil {
				issueMilestone := issue.GetMilestone()
				if issueMilestone == nil || *issueMilestone != *nextTagMilestone {
					err = updateIssueMilestone(ghclient, conf, issue, nextTagMilestone)
					if err != nil {
						logger.Fatalf("Unable to update issue milestone: %v", err)
					}
				}
			}
		}
		logger.Println("All issues updated with milestone, sleeping")
		closeNodes(dialedNodes)
	}
}

func thresholdExceeded(cur Thresholds, max Thresholds) (string, bool) {
	if cur.ErrNodes > max.ErrNodes {
		return fmt.Sprintf("Error nodes(%d) exceeds the threshold(%d)", cur.ErrNodes, max.ErrNodes), true
	} else if cur.LockedNodes > max.LockedNodes {
		return fmt.Sprintf("Locked by another nodes(%d) exceeds the threshold(%d)", cur.LockedNodes, max.LockedNodes), true
	}
	return "", false
}

//  Are there newer commits than our common last applied commit across "all"
//  nodes?
//    If No, do nothing
//    If Yes, do all commits have issues created on github?
//      If Yes, do nothing
//      If No, run a noop per commit & create a github issue
func noopLoop(conf *HecklerdConf, repo *git.Repository, templates *template.Template) {
	logger := log.New(os.Stdout, "[noopLoop] ", log.Lshortfile)
	prefix := conf.EnvPrefix
	var curThresholds Thresholds
	for {
		time.Sleep(10 * time.Second)
		curThresholds.ErrNodes = 0
		curThresholds.LockedNodes = 0
		nodesToDial, err := nodesFromSet(conf, "all", logger)
		if err != nil {
			logger.Fatalf("Unable to load 'all' node set: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
		defer cancel()
		dialedNodes, errDialNodes := dialNodes(ctx, nodesToDial)
		curThresholds.ErrNodes += len(errDialNodes)
		if msg, ok := thresholdExceeded(curThresholds, conf.MaxThresholds); ok {
			logger.Printf("%s, sleeping", msg)
			closeNodes(dialedNodes)
			continue
		}
		eligibleNodes, lockedByAnotherNodes, errEligibleNodes := eligibleNodes("root", dialedNodes)
		curThresholds.LockedNodes += len(lockedByAnotherNodes)
		curThresholds.ErrNodes += len(errEligibleNodes)
		if msg, ok := thresholdExceeded(curThresholds, conf.MaxThresholds); ok {
			logger.Printf("%s, sleeping", msg)
			closeNodes(dialedNodes)
			continue
		}
		lastApplyNodes, errUnknownRevNodes := nodeLastApply(eligibleNodes, repo, logger)
		curThresholds.ErrNodes += len(errUnknownRevNodes)
		if msg, ok := thresholdExceeded(curThresholds, conf.MaxThresholds); ok {
			logger.Printf("%s, sleeping", msg)
			closeNodes(dialedNodes)
			continue
		}
		errNodes := make(map[string]error)
		for host, err := range errDialNodes {
			errNodes[host] = err
		}
		for host, err := range errEligibleNodes {
			errNodes[host] = err
		}
		for host, err := range errUnknownRevNodes {
			errNodes[host] = err
		}
		compressedErrNodes := compressErrorHosts(errNodes)
		for host, err := range compressedErrNodes {
			logger.Printf("errNodes: %s, %v", host, err)
		}
		compressedLockedByAnotherNodes := compressHostsStr(lockedByAnotherNodes)
		for host, str := range compressedLockedByAnotherNodes {
			logger.Printf("Locked by another: %s, %s", host, str)
		}
		commonTag, err := commonAncestorTag(lastApplyNodes, prefix, repo, logger)
		if err != nil {
			logger.Printf("Unable to find common tag, sleeping: %v", err)
			closeNodes(dialedNodes)
			continue
		}
		if commonTag == "" {
			logger.Println("No common tag found, sleeping")
			closeNodes(dialedNodes)
			continue
		}
		logger.Printf("Found common tag: %s", commonTag)
		// TODO support other branches?
		_, commits, err := commitLogIdList(repo, commonTag, "master")
		if err != nil {
			logger.Fatalf("Unable to obtain commit log ids: %v", err)
		}
		if len(commits) == 0 {
			logger.Println("No new commits, sleeping")
			closeNodes(dialedNodes)
			continue
		}
		ghclient, _, err := githubConn(conf)
		if err != nil {
			closeNodes(dialedNodes)
			logger.Printf("Unable to connect to github, sleeping: %v", err)
			continue
		}
		for _, node := range lastApplyNodes {
			node.commitReports = make(map[git.Oid]*rizzopb.PuppetReport)
			node.commitDeltaResources = make(map[git.Oid]map[ResourceTitle]*deltaResource)
		}
		for gi, commit := range commits {
			issue, err := githubIssueFromCommit(ghclient, gi, conf)
			if err != nil {
				logger.Printf("Unable to determine if issue for %s exists, sleeping: %v", gi.String(), err)
				break
			}
			if issue != nil {
				continue
			}
			perNoopThresholds := Thresholds{
				ErrNodes:    curThresholds.ErrNodes,
				LockedNodes: curThresholds.LockedNodes,
			}
			lockedNodes, lockedByAnotherNodes, errLockNodes := lockNodes("root", conf.LockMessage, false, lastApplyNodes)
			perNoopThresholds.LockedNodes += len(lockedByAnotherNodes)
			perNoopThresholds.ErrNodes += len(errLockNodes)
			if msg, ok := thresholdExceeded(perNoopThresholds, conf.MaxThresholds); ok {
				logger.Printf("%s, sleeping", msg)
				unlockNodes("root", false, lockedNodes, logger)
				break
			}
			groupedCommit, errNoopCommitNodes, err := noopCommit(lockedNodes, commit, true, repo, logger)
			if err != nil {
				logger.Printf("Unable to noop commit: %s, sleeping, %v", gi.String(), err)
				unlockNodes("root", false, lockedNodes, logger)
				break
			}
			perNoopThresholds.ErrNodes += len(errNoopCommitNodes)
			if msg, ok := thresholdExceeded(perNoopThresholds, conf.MaxThresholds); ok {
				compressedErrNodes := compressErrorHosts(errNoopCommitNodes)
				for host, err := range compressedErrNodes {
					logger.Printf("errNodes: %s, %v", host, err)
				}
				logger.Printf("%s, sleeping", msg)
				unlockNodes("root", false, lockedNodes, logger)
				break
			}
			unlockNodes("root", false, lockedNodes, logger)
			err = githubCreateIssue(ghclient, conf, commit, groupedCommit, templates, logger)
			if err != nil {
				logger.Printf("Unable to create github issue, sleeping: %v", err)
				break
			}
		}
		closeNodes(dialedNodes)
		logger.Println("Nooping complete, sleeping")
	}
}

func describeCommit(gi git.Oid, prefix string, repo *git.Repository) (string, error) {
	describeOpts, err := git.DefaultDescribeOptions()
	if err != nil {
		return "", err
	}
	describeOpts.Pattern = fmt.Sprintf("%sv*", tagPrefix(prefix))
	formatOpts, err := git.DefaultDescribeFormatOptions()
	formatOpts.AbbreviatedSize = 0
	if err != nil {
		return "", err
	}

	commit, err := repo.LookupCommit(&gi)
	if err != nil {
		return "", err
	}
	result, err := commit.Describe(&describeOpts)
	if err != nil {
		return "", err
	}
	tagStr, err := result.Format(&formatOpts)
	if err != nil {
		return "", err
	}
	return tagStr, nil
}

// If a commit is not, equal, an ancestor, or a descendant of a node's last
// applied commit, then we cannot noop that commit accurately, because the last
// applied commit has children which we do not have in our graph and
// consequently changes which we do not have.
func commitInAllNodeLineages(commit git.Oid, nodes map[string]*Node, repo *git.Repository) bool {
	commitInLineages := true
	for _, node := range nodes {
		if node.lastApply.Equal(&commit) {
			continue
		}
		descendant, err := repo.DescendantOf(&node.lastApply, &commit)
		if err != nil {
			log.Fatalf("Cannot determine descendant status: %v", err)
		}
		if descendant {
			continue
		}
		descendant, err = repo.DescendantOf(&commit, &node.lastApply)
		if err != nil {
			log.Fatalf("Cannot determine descendant status: %v", err)
		}
		if descendant {
			continue
		}
		commitInLineages = false
		break
	}
	return commitInLineages
}

func commonAncestorTag(nodes map[string]*Node, prefix string, repo *git.Repository, logger *log.Logger) (string, error) {
	// get set of tags
	tagNodes := make(map[string][]string)
	for _, node := range nodes {
		tagStr, err := describeCommit(node.lastApply, prefix, repo)
		if err != nil {
			return "", errors.New(fmt.Sprintf("Unable to describe %s@%s, %v", node.host, node.lastApply.String(), err))
		}
		if _, ok := tagNodes[tagStr]; ok {
			tagNodes[tagStr] = append(tagNodes[tagStr], node.host)
		} else {
			tagNodes[tagStr] = []string{node.host}
		}
	}
	if len(tagNodes) == 0 {
		logger.Println("No common tag found!")
		for tag, nodes := range tagNodes {
			logger.Printf("Tag: %s, Hosts: %s", tag, compressHosts(nodes))
		}
		return "", nil
	}

	tags := make([]string, 0, len(tagNodes))
	for tag := range tagNodes {
		tags = append(tags, tag)
	}
	tagSet, err := sortedSemVers(tags, prefix)
	if err != nil {
		return "", err
	}

	// return the earliest version from the set, which should be the common
	// ancestor tag for all nodes
	return semverToOrig(tagSet[0], prefix), nil
}

func semverToOrig(tag *semver.Version, prefix string) string {
	return fmt.Sprintf("%s%s", tagPrefix(prefix), tag.Original())
}

func tagToSemver(tag string, prefix string) (*semver.Version, error) {
	return semver.NewVersion(strings.TrimPrefix(tag, tagPrefix(prefix)))
}

func tagPrefix(prefix string) string {
	if prefix == "" {
		return ""
	} else {
		return fmt.Sprintf("%s/", prefix)
	}
}

func sortedSemVers(tags []string, prefix string) ([]*semver.Version, error) {
	tagSet := make([]*semver.Version, 0, len(tags))
	for _, tag := range tags {
		v, err := tagToSemver(tag, prefix)
		if err != nil {
			return []*semver.Version{}, err
		}
		tagSet = append(tagSet, v)
	}

	sort.Sort(semver.Collection(tagSet))
	return tagSet, nil
}

func nextTag(priorTag string, prefix string, repo *git.Repository) (string, error) {
	tagMatch := fmt.Sprintf("%sv*", tagPrefix(prefix))
	tags, err := repo.Tags.ListWithMatch(tagMatch)
	if err != nil {
		return "", err
	}
	annotatedTags := make([]string, 0)
	for _, tag := range tags {
		obj, err := repo.RevparseSingle(tag)
		if err != nil {
			return "", err
		}
		if obj.Type() == git.ObjectTag {
			annotatedTags = append(annotatedTags, tag)
		}
	}
	tagSet, err := sortedSemVers(tags, prefix)

	priorTagFound := false
	var priorTagIndex int
	for i := range tagSet {
		if semverToOrig(tagSet[i], prefix) == priorTag {
			priorTagFound = true
			priorTagIndex = i
		}
	}
	if !priorTagFound {
		return "", errors.New(fmt.Sprintf("Prior tag not found in repo! '%s'", priorTag))
	}
	nextTagIndex := priorTagIndex + 1
	if len(tagSet) > nextTagIndex {
		nextTag := semverToOrig(tagSet[nextTagIndex], prefix)
		return nextTag, nil
	} else {
		return "", nil
	}
}

func clearGithub(conf *HecklerdConf) error {
	ghclient, _, err := githubConn(conf)
	if err != nil {
		return err
	}
	err = clearIssues(ghclient, conf)
	if err != nil {
		return err
	}
	err = clearMilestones(ghclient, conf)
	if err != nil {
		return err
	}
	return nil
}

func main() {
	// add filename and line number to log output
	log.SetFlags(log.Lshortfile)
	var err error
	var hecklerdConfPath string
	var conf *HecklerdConf
	var file *os.File
	var data []byte
	var clearState bool
	var clearGitHub bool
	var printVersion bool
	var cpuprofile = flag.String("cpuprofile", "", "write cpu profile to `file`")
	var memprofile = flag.String("memprofile", "", "write memory profile to `file`")

	templates := parseTemplates()
	logger := log.New(os.Stdout, "[Main] ", log.Lshortfile)

	flag.BoolVar(&clearState, "clear", false, "Clear local state, e.g. puppet code repo")
	flag.BoolVar(&clearGitHub, "ghclear", false, "Clear remote github state, e.g. issues & milestones")
	flag.BoolVar(&printVersion, "version", false, "print version")
	flag.Parse()

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			logger.Fatal("could not create CPU profile: ", err)
		}
		defer f.Close() // error handling omitted for example
		if err := pprof.StartCPUProfile(f); err != nil {
			logger.Fatal("could not start CPU profile: ", err)
		}
		defer pprof.StopCPUProfile()
	}

	if printVersion {
		fmt.Printf("v%s\n", Version)
		os.Exit(0)
	}

	if _, err := os.Stat("/etc/hecklerd/hecklerd_conf.yaml"); err == nil {
		hecklerdConfPath = "/etc/hecklerd/hecklerd_conf.yaml"
	} else if _, err := os.Stat("hecklerd_conf.yaml"); err == nil {
		hecklerdConfPath = "hecklerd_conf.yaml"
	} else {
		logger.Fatal("Unable to load hecklerd_conf.yaml from /etc/hecklerd or .")
	}
	file, err = os.Open(hecklerdConfPath)
	if err != nil {
		logger.Fatal(err)
	}
	data, err = ioutil.ReadAll(file)
	if err != nil {
		logger.Fatalf("Cannot read config: %v", err)
	}
	conf = new(HecklerdConf)
	err = yaml.Unmarshal([]byte(data), conf)
	if err != nil {
		logger.Fatalf("Cannot unmarshal config: %v", err)
	}
	file.Close()

	if clearState && clearGitHub {
		logger.Println("clear & ghclear are mutually exclusive")
		os.Exit(1)
	}

	if clearState {
		logger.Printf("Removing state directory: %v", stateDir)
		os.RemoveAll(stateDir)
		os.Exit(0)
	}

	if clearGitHub {
		err = clearGithub(conf)
		if err != nil {
			logger.Fatalf("Unable to clear GitHub: %v", err)
		}
		os.Exit(0)
	}

	logger.Printf("hecklerd: v%s\n", Version)
	repo, err := fetchRepo(conf)
	if err != nil {
		logger.Fatalf("Unable to fetch repo to serve: %v", err)
	}

	gitServer := &gitcgiserver.GitCGIServer{}
	gitServer.ExportAll = true
	gitServer.ProjectRoot = stateDir + "/served_repo"
	gitServer.Addr = defaultAddr
	gitServer.ShutdownTimeout = shutdownTimeout
	gitServer.MaxClients = conf.GitServerMaxClients

	idleConnsClosed := make(chan struct{})
	done := make(chan bool, 1)

	// background polling git fetch
	go func() {
		logger := log.New(os.Stdout, "[GitFetchBare] ", log.Lshortfile)
		for {
			time.Sleep(5 * time.Second)
			if Debug {
				logger.Println("Updating repo..")
			}
			_, err = fetchRepo(conf)
			if err != nil {
				logger.Fatalf("Unable to fetch repo: %v", err)
			}
		}
	}()

	// git server
	go func() {
		logger := log.New(os.Stdout, "[GitServer] ", log.Lshortfile)
		logger.Printf("Starting Git HTTP server on %s", gitServer.Addr)
		if err := gitServer.Serve(); err != nil && err != http.ErrServerClosed {
			logger.Println("Git HTTP server error:", err)
		}
		<-idleConnsClosed
	}()

	lis, err := net.Listen("tcp", port)
	if err != nil {
		logger.Fatalf("failed to listen: %v", err)
	}
	grpcServer := grpc.NewServer()
	hecklerServer := new(hecklerServer)
	hecklerServer.conf = conf
	hecklerServer.templates = templates
	hecklerServer.repo = repo
	hecklerpb.RegisterHecklerServer(grpcServer, hecklerServer)

	// grpc server
	go func() {
		logger := log.New(os.Stdout, "[GrpcServer] ", log.Lshortfile)
		logger.Printf("Starting GRPC HTTP server on %v", port)
		if err := grpcServer.Serve(lis); err != nil {
			logger.Fatalf("failed to serve: %v", err)
		}
	}()

	// TODO hack to ensure our git server is up, ideally we would pass in a tcp
	// listener to the git server, so we know it is available, as we did with the
	// grpc server.
	time.Sleep(1 * time.Second)
	_, err = gitutil.Pull("http://localhost:8080/puppetcode", stateDir+"/work_repo/puppetcode")
	if err != nil {
		logger.Fatalf("Unable to fetch repo: %v", err)
	}
	go func() {
		logger := log.New(os.Stdout, "[GitFetchWork] ", log.Lshortfile)
		for {
			_, err := gitutil.Pull("http://localhost:8080/puppetcode", stateDir+"/work_repo/puppetcode")
			if err != nil {
				logger.Fatalf("Unable to fetch repo: %v", err)
			}
			time.Sleep(5 * time.Second)
		}
	}()

	if conf.ManualMode {
		logger.Println("Manual mode: not starting noop, milestone, apply, & tag loops")
	} else {
		logger.Println("Unlocking 'all' in case they are locked, from a segfault")
		unlockAll(conf, logger)
		logger.Println("Starting noop, milestone, & apply loops")
		go noopLoop(conf, repo, templates)
		go milestoneLoop(conf, repo)
		go applyLoop(conf, repo)
		if conf.AutoTagCronSchedule != "" {
			logger.Println("Starting tag loop")
			c := cron.New()
			c.AddFunc(
				conf.AutoTagCronSchedule,
				func() {
					tagNewCommits(conf, repo)
				},
			)
			c.Start()
			defer c.Stop()
		}
	}

	// XXX any reason to make this a separate goroutine?
	go func() {
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
		logger.Printf("Received %s", <-sigs)
		if !conf.ManualMode {
			unlockAll(conf, logger)
		}
		if err := gitServer.Shutdown(context.Background()); err != nil {
			logger.Printf("HTTP server shutdown error: %v", err)
		}
		close(idleConnsClosed)
		grpcServer.GracefulStop()
		logger.Println("Heckler Shutdown")
		done <- true
	}()

	<-done

	if *memprofile != "" {
		f, err := os.Create(*memprofile)
		if err != nil {
			logger.Fatal("could not create memory profile: ", err)
		}
		defer f.Close() // error handling omitted for example
		runtime.GC()    // get up-to-date statistics
		if err := pprof.WriteHeapProfile(f); err != nil {
			logger.Fatal("could not write memory profile: ", err)
		}
	}
}
