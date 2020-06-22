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
	"github.braintreeps.com/lollipopman/heckler/internal/heckler"
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
	git "github.com/libgit2/git2go/v30"
	gitcgiserver "github.com/lollipopman/git-cgi-server"
	"github.com/robfig/cron/v3"
	"github.com/square/grange"
	"google.golang.org/grpc"
	"gopkg.in/yaml.v3"
)

var Version string
var ErrLastApplyUnknown = errors.New("Unable to determine lastApply commit, use force flag to update")
var ErrThresholdExceeded = errors.New("Threshold for err nodes or lock nodes exceeded")

const (
	ApplicationName = "git-cgi-server"

	defaultAddr     = ":8080"
	shutdownTimeout = time.Second * 5
	// TODO move to HecklerdConf
	port           = ":50052"
	stateDir       = "/var/lib/hecklerd"
	workRepo       = "/var/lib/hecklerd" + "/work_repo/puppetcode"
	noopDir        = stateDir + "/noops"
	groupedNoopDir = noopDir + "/grouped"
)

var Debug = false

// TODO: this regex also matches, Node[__node_regexp__fozzie], which causes
// resources in node blocks to be mistaken for define types. Is there a more
// robust way to match for define types?
var RegexDefineType = regexp.MustCompile(`^[A-Z][a-zA-Z0-9_:]*\[[^\]]+\]$`)

type HecklerdConf struct {
	Repo                 string                `yaml:"repo"`
	RepoOwner            string                `yaml:"repo_owner"`
	RepoBranch           string                `yaml:"repo_branch"`
	GitHubDomain         string                `yaml:"github_domain"`
	GitHubPrivateKeyPath string                `yaml:"github_private_key_path"`
	GitHubAppSlug        string                `yaml:"github_app_slug"`
	GitHubAppId          int64                 `yaml:"github_app_id"`
	GitHubAppInstallId   int64                 `yaml:"github_app_install_id"`
	NodeSets             map[string]NodeSetCfg `yaml:"node_sets"`
	AutoTagCronSchedule  string                `yaml:"auto_tag_cron_schedule"`
	AutoCloseIssues      bool                  `yaml:"auto_close_issues"`
	EnvPrefix            string                `yaml:"env_prefix"`
	MaxThresholds        Thresholds            `yaml:"max_thresholds"`
	GitServerMaxClients  int                   `yaml:"git_server_max_clients"`
	ManualMode           bool                  `yaml:"manual_mode"`
	LockMessage          string                `yaml:"lock_message"`
}

type NodeSetCfg struct {
	Cmd       []string `yaml:"cmd"`
	Blacklist []string `yaml:"blacklist"`
}

type NodeSet struct {
	name       string
	commonTag  string
	thresholds Thresholds
	nodes      Nodes
}

type Nodes struct {
	active  map[string]*Node
	dialed  map[string]*Node
	errored map[string]*Node
	locked  map[string]*Node
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
	err                  error
	lockState            heckler.LockState
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
	NodeFiles       []string
	CompressedNodes string
	Events          []*groupEvent
	Logs            []*groupLog
	Owners          groupedResourceOwners
	Approvals       groupedResourceApprovals
}

type groupedResourceOwners struct {
	File      []string
	NodeFiles map[string][]string
}

type groupedResourceApprovals struct {
	File      []string
	NodeFiles map[string][]string
}

type noopOwners struct {
	OwnedNodeFiles     map[string][]string
	UnownedNodeFiles   map[string][]string
	OwnedSourceFiles   map[string][]string
	UnownedSourceFiles map[string][]string
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

func grpcConnect(ctx context.Context, node *Node, clientConnChan chan *Node) {
	address := node.host + ":50051"
	ctx, cancel := context.WithTimeout(ctx, time.Duration(5)*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, address, grpc.WithInsecure(), grpc.WithBlock(), grpc.FailOnNonTempDialError(true))
	if err != nil {
		node.err = err
		clientConnChan <- node
	} else {
		node.rizzoClient = rizzopb.NewRizzoClient(conn)
		node.grpcConn = conn
		clientConnChan <- node
	}
}

func dialNodes(ctx context.Context, hosts []string) (map[string]*Node, map[string]*Node) {
	var node *Node
	clientConnChan := make(chan *Node)
	for _, host := range hosts {
		node = new(Node)
		node.host = host
		go grpcConnect(ctx, node, clientConnChan)
	}

	nodes := make(map[string]*Node)
	errNodes := make(map[string]*Node)
	for i := 0; i < len(hosts); i++ {
		node = <-clientConnChan
		if node.err != nil {
			errNodes[node.host] = node
		} else {
			nodes[node.host] = node
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
func marshalGroupedCommit(oid *git.Oid, groupedCommit []*groupedResource, groupedNoopDir string) error {
	groupedCommitPath := groupedNoopDir + "/" + oid.String() + ".json"
	data, err := json.Marshal(groupedCommit)
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(groupedCommitPath, data, 0644)
	if err != nil {
		return err
	}
	return nil
}

func unmarshalGroupedCommit(oid *git.Oid, groupedNoopDir string) ([]*groupedResource, error) {
	groupedCommitPath := groupedNoopDir + "/" + oid.String() + ".json"
	file, err := os.Open(groupedCommitPath)
	if err != nil {
		return []*groupedResource{}, err
	}
	data, err := ioutil.ReadAll(file)
	if err != nil {
		return []*groupedResource{}, err
	}
	var groupedCommit []*groupedResource
	err = json.Unmarshal([]byte(data), &groupedCommit)
	if err != nil {
		return []*groupedResource{}, err
	}
	return groupedCommit, nil
}

func noopNodeSet(ns *NodeSet, commit *git.Commit, deltaNoop bool, repo *git.Repository, logger *log.Logger) ([]*groupedResource, error) {
	var err error

	for host, _ := range ns.nodes.active {
		os.Mkdir(noopDir+"/"+host, 0755)
	}

	// TODO: if we are only requesting a single noop via heckler, we probably
	// always want to noop, rather than returning an empty noop if the commit is
	// not part of a nodes lineage.
	if !commitInAllNodeLineages(*commit.Id(), ns.nodes.active, repo, logger) {
		return make([]*groupedResource, 0), nil
	}

	commitsToNoop := make([]git.Oid, 0)
	commitsToNoop = append(commitsToNoop, *commit.Id())

	parentCount := commit.ParentCount()
	for i := uint(0); i < parentCount; i++ {
		if deltaNoop && commitInAllNodeLineages(*commit.ParentId(i), ns.nodes.active, repo, logger) {
			commitsToNoop = append(commitsToNoop, *commit.ParentId(i))
		} else {
			for _, node := range ns.nodes.active {
				node.commitReports[*commit.ParentId(i)] = &rizzopb.PuppetReport{}
				node.commitDeltaResources[*commit.ParentId(i)] = make(map[ResourceTitle]*deltaResource)
			}
		}
	}

	var nodeCommitToNoop git.Oid
	var rprt *rizzopb.PuppetReport
	errNoopNodes := make(map[string]*Node)
	puppetReportChan := make(chan applyResult)
	for i, commitToNoop := range commitsToNoop {
		logger.Printf("Nooping: %s (%d of %d)", commitToNoop.String(), i+1, len(commitsToNoop))
		noopHosts := make(map[string]bool)
		for _, node := range ns.nodes.active {
			// PERMA-DIFF: We need a noop report of the last applied commit so we can
			// use it to subtract away perma-diffs from children.
			if commitToNoop != *commit.Id() && commitAlreadyApplied(node.lastApply, commitToNoop, repo) {
				nodeCommitToNoop = node.lastApply
			} else {
				nodeCommitToNoop = commitToNoop
			}
			if rprt, err = loadNoop(nodeCommitToNoop, node, noopDir, repo, logger); err == nil {
				ns.nodes.active[node.host].commitReports[nodeCommitToNoop] = rprt
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
				ns.nodes.active[r.host].err = fmt.Errorf("Noop failed for %s: %w", r.host, r.err)
				errNoopNodes[r.host] = ns.nodes.active[r.host]
				logger.Println(errNoopNodes[r.host].err)
				delete(ns.nodes.active, r.host)
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
			ns.nodes.active[newRprt.Host].commitReports[*commitId] = &newRprt
			err = marshalReport(newRprt, noopDir, *commitId)
			if err != nil {
				logger.Fatalf("Unable to marshal report: %v", err)
			}
		}
	}

	for _, node := range ns.nodes.active {
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
	for _, node := range ns.nodes.active {
		for _, nodeDeltaRes := range node.commitDeltaResources[*commit.Id()] {
			groupedCommit = append(groupedCommit, groupResources(*commit.Id(), nodeDeltaRes, ns.nodes.active))
		}
	}
	ns.nodes.errored = mergeNodeMaps(ns.nodes.errored, errNoopNodes)
	if msg, ok := thresholdExceededNodeSet(ns); ok {
		logger.Printf("%s", msg)
		return nil, ErrThresholdExceeded
	}
	return groupedCommit, nil
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

func compressErrorNodes(nodes map[string]*Node) map[string]error {
	var errType string
	errHosts := make(map[string][]string)
	for host, node := range nodes {
		// If we don't have a custom error, than don't compress, this is a bit of a
		// wack a mole approach.
		if fmt.Sprintf("%T", node.err) == "*errors.errorString" ||
			fmt.Sprintf("%T", node.err) == "*fmt.wrapError" {
			errType = fmt.Sprintf("%v", node.err)
		} else {
			errType = fmt.Sprintf("%T", node.err)
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

func compressLockNodes(nodes map[string]*Node) map[string]string {
	lockedHosts := make(map[heckler.LockState][]string)
	for host, node := range nodes {
		if hosts, ok := lockedHosts[node.lockState]; ok {
			lockedHosts[node.lockState] = append(hosts, host)
		} else {
			lockedHosts[node.lockState] = []string{host}
		}
	}
	compressedHostStr := make(map[string]string)
	for ls, hosts := range lockedHosts {
		compressedHostStr[compressHosts(hosts)] = fmt.Sprintf("%s: %s", ls.User, ls.Comment)
	}
	return compressedHostStr
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

func uniqueStrSlice(strSlice []string) []string {
	uniqueMap := make(map[string]bool)
	for _, v := range strSlice {
		uniqueMap[v] = true
	}
	uniqueList := make([]string, len(uniqueMap))
	i := 0
	for v := range uniqueMap {
		uniqueList[i] = v
		i++
	}
	return uniqueList
}

func groupResources(commitLogId git.Oid, targetDeltaResource *deltaResource, nodes map[string]*Node) *groupedResource {
	var nodeList []string
	var desiredValue string
	// TODO Remove this hack, only needed for old versions of puppet 4.5?
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
		// TODO move base64 decode somewhere else
		// also yell at puppet for this inconsistency!!!
		if targetDeltaResource.Type == "File" && e.Property == "content" {
			data, err := base64.StdEncoding.DecodeString(e.DesiredValue)
			if err != nil {
				// TODO nasty, fix?
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

func dialReqNodes(conf *HecklerdConf, hosts []string, nodeSetName string, logger *log.Logger) (*NodeSet, error) {
	var hostsToDial []string
	var err error
	ns := &NodeSet{}
	if len(hosts) > 0 {
		hostsToDial = hosts
	} else {
		ns.name = nodeSetName
		hostsToDial, err = setNameToNodes(conf, nodeSetName, logger)
		if err != nil {
			return nil, err
		}
	}
	// Disable thresholds for a heckler client request
	ns.thresholds.ErrNodes = len(hostsToDial)
	ns.thresholds.LockedNodes = len(hostsToDial)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	ns.nodes.dialed, ns.nodes.errored = dialNodes(ctx, hostsToDial)
	ns.nodes.active = ns.nodes.dialed
	return ns, nil
}

func setNameToNodes(conf *HecklerdConf, nodeSetName string, logger *log.Logger) ([]string, error) {
	if nodeSetName == "" {
		return nil, errors.New("Empty nodeSetName provided")
	}
	var nodeSet NodeSetCfg
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

// Given any number of node maps return a merged map, assumes map keys are
// distinct
func mergeNodeMaps(nodeMaps ...map[string]*Node) map[string]*Node {
	merged := make(map[string]*Node)
	for _, nodes := range nodeMaps {
		for k, v := range nodes {
			merged[k] = v
		}
	}
	return merged
}

func (hs *hecklerServer) HecklerApply(ctx context.Context, req *hecklerpb.HecklerApplyRequest) (*hecklerpb.HecklerApplyReport, error) {
	var err error
	logger := log.New(os.Stdout, "[HecklerApply] ", log.Lshortfile)
	commit, err := gitutil.RevparseToCommit(req.Rev, hs.repo)
	if err != nil {
		return nil, err
	}
	ns, err := dialReqNodes(hs.conf, req.Nodes, req.NodeSet, logger)
	if err != nil {
		return nil, err
	}
	defer closeNodeSet(ns)
	err = lockNodeSet(req.User, hs.conf.LockMessage, false, ns, logger)
	if err != nil {
		return nil, err
	}
	defer unlockNodeSet(req.User, false, ns, logger)
	har := new(hecklerpb.HecklerApplyReport)
	if req.Noop {
		err := lastApplyNodeSet(ns, hs.repo, logger)
		if err != nil {
			return nil, err
		}
		for _, node := range ns.nodes.active {
			node.commitReports = make(map[git.Oid]*rizzopb.PuppetReport)
			node.commitDeltaResources = make(map[git.Oid]map[ResourceTitle]*deltaResource)
		}
		groupedCommit, err := noopNodeSet(ns, commit, req.DeltaNoop, hs.repo, logger)
		if err != nil {
			return nil, err
		}
		har.Output = commitToMarkdown(hs.conf, commit, groupedCommit, hs.templates)
	} else {
		appliedNodes, beyondRevNodes, err := applyNodeSet(ns, req.Force, req.Noop, req.Rev, hs.repo, logger)
		if err != nil {
			return nil, err
		}
		if req.Force {
			har.Output = fmt.Sprintf("Applied nodes: (%d); Error nodes: (%d)", len(appliedNodes), len(ns.nodes.errored))
		} else {
			har.Output = fmt.Sprintf("Applied nodes: (%d); Beyond rev nodes: (%d); Error nodes: (%d)", len(appliedNodes), len(beyondRevNodes), len(ns.nodes.errored))
		}
	}
	har.NodeErrors = make(map[string]string)
	for host, node := range ns.nodes.errored {
		har.NodeErrors[host] = node.err.Error()
	}
	for host, node := range ns.nodes.locked {
		har.NodeErrors[host] = fmt.Sprintf("%s: %s", node.lockState.User, node.lockState.Comment)
	}
	return har, nil
}

func applyNodeSet(ns *NodeSet, forceApply bool, noop bool, rev string, repo *git.Repository, logger *log.Logger) (map[string]*Node, map[string]*Node, error) {
	var nodesToApply map[string]*Node
	var err error
	beyondRevNodes := make(map[string]*Node)
	lastApplyNodes := make(map[string]*Node)
	errUnknownRevNodes := make(map[string]*Node)
	appliedNodes := make(map[string]*Node)
	// No need to check node revision if force applying
	if forceApply {
		nodesToApply = ns.nodes.active
	} else {
		err = lastApplyNodeSet(ns, repo, logger)
		if err != nil {
			return nil, nil, err
		}
		obj, err := gitutil.RevparseToCommit(rev, repo)
		if err != nil {
			return nil, nil, err
		}
		revId := *obj.Id()
		nodesToApply = make(map[string]*Node)
		for host, node := range lastApplyNodes {
			if commitAlreadyApplied(node.lastApply, revId, repo) {
				beyondRevNodes[host] = node
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

	errApplyNodes := make(map[string]*Node)
	for range nodesToApply {
		r := <-puppetReportChan
		if r.err != nil {
			nodesToApply[r.host].err = fmt.Errorf("Apply failed: %w", r.err)
			errApplyNodes[r.host] = nodesToApply[r.host]
		} else if r.report.Status == "failed" {
			nodesToApply[r.host].err = fmt.Errorf("Apply status=failed, %s@%s", r.report.Host, r.report.ConfigurationVersion)
			errApplyNodes[r.host] = nodesToApply[r.host]
		} else {
			if noop {
				logger.Printf("Nooped: %s@%s", r.report.Host, r.report.ConfigurationVersion)
			} else {
				logger.Printf("Applied: %s@%s", r.report.Host, r.report.ConfigurationVersion)
			}
			appliedNodes[r.report.Host] = nodesToApply[r.report.Host]
		}
	}
	ns.nodes.active = mergeNodeMaps(appliedNodes, beyondRevNodes)
	ns.nodes.errored = mergeNodeMaps(ns.nodes.errored, errUnknownRevNodes, errApplyNodes)
	return appliedNodes, beyondRevNodes, nil
}

func (hs *hecklerServer) HecklerNoopRange(ctx context.Context, req *hecklerpb.HecklerNoopRangeRequest) (*hecklerpb.HecklerNoopRangeReport, error) {
	var err error
	logger := log.New(os.Stdout, "[HecklerNoopRange] ", log.Lshortfile)
	ns, err := dialReqNodes(hs.conf, req.Nodes, req.NodeSet, logger)
	if err != nil {
		return nil, err
	}
	defer closeNodeSet(ns)
	err = lastApplyNodeSet(ns, hs.repo, logger)
	if err != nil {
		return nil, err
	}
	err = lockNodeSet(req.User, hs.conf.LockMessage, false, ns, logger)
	if err != nil {
		return nil, err
	}
	defer unlockNodeSet(req.User, false, ns, logger)
	commitLogIds, commits, err := commitLogIdList(hs.repo, req.BeginRev, req.EndRev)
	if err != nil {
		return nil, err
	}
	for _, node := range ns.nodes.active {
		node.commitReports = make(map[git.Oid]*rizzopb.PuppetReport)
		node.commitDeltaResources = make(map[git.Oid]map[ResourceTitle]*deltaResource)
	}
	rprt := new(hecklerpb.HecklerNoopRangeReport)
	groupedCommits := make(map[git.Oid][]*groupedResource)
	for gi, commit := range commits {
		groupedCommit, err := noopNodeSet(ns, commit, true, hs.repo, logger)
		if err != nil {
			return nil, err
		}
		groupedCommits[gi] = groupedCommit
	}
	if req.OutputFormat == hecklerpb.OutputFormat_markdown {
		rprt.Output = commitRangeToMarkdown(hs.conf, commitLogIds, commits, groupedCommits, hs.templates)
	}
	rprt.NodeErrors = make(map[string]string)
	for host, node := range ns.nodes.errored {
		rprt.NodeErrors[host] = node.err.Error()
	}
	for host, node := range ns.nodes.locked {
		rprt.NodeErrors[host] = fmt.Sprintf("%s: %s", node.lockState.User, node.lockState.Comment)
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

func githubOpenIssues(ghclient *github.Client, conf *HecklerdConf) ([]github.Issue, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	prefix := conf.EnvPrefix
	query := "is:issue is:open"
	if prefix != "" {
		query += fmt.Sprintf(" %sin:title", issuePrefix(prefix))
	}
	query += fmt.Sprintf(" author:app/%s", conf.GitHubAppSlug)
	searchResults, _, err := ghclient.Search.Issues(ctx, query, nil)
	if err != nil {
		return nil, err
	}
	return searchResults.Issues, nil
}

// Given an email address, return the github user associated with the provided
// email address
func githubUserFromEmail(ghclient *github.Client, email string) (*github.User, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	query := fmt.Sprintf("%s in:email", email)
	searchResults, _, err := ghclient.Search.Users(ctx, query, nil)
	if err != nil {
		return nil, err
	}
	if searchResults.GetTotal() == 0 {
		return nil, nil
	} else if searchResults.GetTotal() == 1 {
		return &searchResults.Users[0], nil
	} else {
		return nil, errors.New("More than one users exists for a single email address")
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

func githubCreateIssue(ghclient *github.Client, conf *HecklerdConf, commit *git.Commit, groupedCommit []*groupedResource, templates *template.Template) (*github.Issue, error) {
	prefix := conf.EnvPrefix
	authors, err := commitAuthorsLogins(ghclient, commit)
	if err != nil {
		return nil, err
	}
	body := commitBodyToMarkdown(commit, conf, templates)
	body += groupedResourcesToMarkdown(groupedCommit, commit, conf, templates)
	noopOwnersMarkdown, err := noopOwnersToMarkdown(conf, commit, groupedCommit, templates)
	if err != nil {
		return nil, err
	}
	body += noopOwnersMarkdown
	githubIssue := &github.IssueRequest{
		Title:     github.String(noopTitle(commit, prefix)),
		Assignees: &authors,
		Body:      github.String(body),
	}
	ctx := context.Background()
	ni, _, err := ghclient.Issues.Create(ctx, conf.RepoOwner, conf.Repo, githubIssue)
	if err != nil {
		return nil, err
	}
	return ni, nil
}

// Given a git commit return a slice of GitHub logins associated with the
// commit author as well as any co-authors found in the commit message
// trailers.
func commitAuthorsLogins(ghclient *github.Client, commit *git.Commit) ([]string, error) {
	githubUser, err := githubUserFromEmail(ghclient, commit.Author().Email)
	if err != nil {
		return []string{}, err
	}
	authors := make([]string, 0)
	if githubUser == nil {
		return nil, fmt.Errorf("Unable to find GitHub user for commit author email: '%s'", commit.Author().Email)
	} else {
		authors = append(authors, *githubUser.Login)
	}
	trailers, err := git.MessageTrailers(commit.Message())
	if err != nil {
		return []string{}, err
	}
	regexCoAuthor := regexp.MustCompile(`^[Cc]o-authored-by$`)
	regexEmailCapture := regexp.MustCompile(`<([^>]*)>`)
	for _, trailer := range trailers {
		if !regexCoAuthor.MatchString(trailer.Key) {
			continue
		}
		email := regexEmailCapture.FindStringSubmatch(trailer.Value)
		if len(email) < 2 || email[1] == "" {
			continue
		}
		githubUser, err := githubUserFromEmail(ghclient, email[1])
		if err != nil {
			return []string{}, err
		}
		if githubUser == nil {
			return nil, fmt.Errorf("Unable to find GitHub user for commit author email: '%s'", email[1])
		} else {
			authors = append(authors, *githubUser.Login)
		}
	}
	return authors, nil
}

// Given a commit and groupedResources returns a markdown string showing the
// owners of noop
func noopOwnersToMarkdown(conf *HecklerdConf, commit *git.Commit, groupedResources []*groupedResource, templates *template.Template) (string, error) {
	var err error
	groupedResources, err = groupedResourcesNodeFiles(groupedResources, workRepo)
	if err != nil {
		return "", err
	}
	groupedResources, err = groupedResourcesOwners(groupedResources, workRepo)
	if err != nil {
		return "", err
	}
	no := groupedResourcesUniqueOwners(groupedResources)
	data := struct {
		Commit     *git.Commit
		Conf       *HecklerdConf
		NoopOwners noopOwners
	}{
		commit,
		conf,
		no,
	}
	var body strings.Builder
	err = templates.ExecuteTemplate(&body, "noopOwners.tmpl", data)
	if err != nil {
		return "", err
	}
	return body.String(), nil
}

// nodeFile takes node name and a map of a puppet node source file to its node
// regexes contained in the source file. Then nodeFile returns the node source
// file path which matches the node.
func nodeFile(node string, nodeFileRegexes map[string][]*regexp.Regexp, puppetCodePath string) (string, error) {
	for file, regexes := range nodeFileRegexes {
		for _, regex := range regexes {
			if regex.MatchString(node) {
				return strings.TrimPrefix(file, puppetCodePath+"/"), nil
			}
		}
	}
	return "", fmt.Errorf("Unable to find node file for node: %v", node)
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
	var err error
	logger := log.New(os.Stdout, "[HecklerStatus] ", log.Lshortfile)
	ns, err := dialReqNodes(hs.conf, req.Nodes, req.NodeSet, logger)
	if err != nil {
		return nil, err
	}
	defer closeNodeSet(ns)
	err = lastApplyNodeSet(ns, hs.repo, logger)
	if err != nil {
		return nil, err
	}
	hsr := new(hecklerpb.HecklerStatusReport)
	hsr.NodeStatuses = make(map[string]string)
	var tagStr string
	for _, node := range ns.nodes.active {
		tagStr, err = describeCommit(node.lastApply, hs.conf.EnvPrefix, hs.repo)
		if err != nil {
			tagStr = "NONE"
		}
		hsr.NodeStatuses[node.host] = "commit: " + node.lastApply.String() + ", last-tag: " + tagStr
	}
	hsr.NodeErrors = make(map[string]string)
	for host, node := range ns.nodes.errored {
		hsr.NodeErrors[host] = node.err.Error()
	}
	return hsr, nil
}

func (hs *hecklerServer) HecklerUnlock(ctx context.Context, req *hecklerpb.HecklerUnlockRequest) (*hecklerpb.HecklerUnlockReport, error) {
	logger := log.New(os.Stdout, "[HecklerUnlock] ", log.Lshortfile)
	ns, err := dialReqNodes(hs.conf, req.Nodes, req.NodeSet, logger)
	if err != nil {
		return nil, err
	}
	defer closeNodeSet(ns)
	unlockNodeSet(req.User, req.Force, ns, logger)
	res := new(hecklerpb.HecklerUnlockReport)
	res.UnlockedNodes = make([]string, 0)
	for node := range ns.nodes.active {
		res.UnlockedNodes = append(res.UnlockedNodes, node)
	}
	res.NodeErrors = make(map[string]string)
	for host, node := range ns.nodes.errored {
		res.NodeErrors[host] = node.err.Error()
	}
	for host, node := range ns.nodes.locked {
		res.NodeErrors[host] = fmt.Sprintf("%s: %s", node.lockState.User, node.lockState.Comment)
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

func closeNodeSet(ns *NodeSet) {
	closeNodes(ns.nodes.dialed)
}

func (hs *hecklerServer) HecklerLock(ctx context.Context, req *hecklerpb.HecklerLockRequest) (*hecklerpb.HecklerLockReport, error) {
	var err error
	logger := log.New(os.Stdout, "[HecklerLock] ", log.Lshortfile)
	ns, err := dialReqNodes(hs.conf, req.Nodes, req.NodeSet, logger)
	if err != nil {
		return nil, err
	}
	defer closeNodeSet(ns)
	err = lockNodeSet(req.User, req.Comment, req.Force, ns, logger)
	if err != nil {
		return nil, err
	}
	res := new(hecklerpb.HecklerLockReport)
	res.LockedNodes = make([]string, 0)
	for node := range ns.nodes.active {
		res.LockedNodes = append(res.LockedNodes, node)
	}
	res.NodeErrors = make(map[string]string)
	for host, node := range ns.nodes.errored {
		res.NodeErrors[host] = node.err.Error()
	}
	for host, node := range ns.nodes.locked {
		res.NodeErrors[host] = fmt.Sprintf("%s: %s", node.lockState.User, node.lockState.Comment)
	}
	return res, nil
}

func lockNodeSet(user string, comment string, force bool, ns *NodeSet, logger *log.Logger) error {
	lockReq := rizzopb.PuppetLockRequest{
		Type:    rizzopb.LockReqType_lock,
		User:    user,
		Comment: comment,
		Force:   force,
	}
	lockedNodes, _, lockedByAnotherNodes, errLockNodes := rizzoLockNodes(lockReq, ns.nodes.active)
	ns.nodes.active = lockedNodes
	ns.nodes.errored = mergeNodeMaps(ns.nodes.errored, errLockNodes)
	ns.nodes.locked = lockedByAnotherNodes
	if msg, ok := thresholdExceededNodeSet(ns); ok {
		logger.Printf("%s", msg)
		return ErrThresholdExceeded
	}
	return nil
}

func unlockNodeSet(user string, force bool, ns *NodeSet, logger *log.Logger) {
	lockReq := rizzopb.PuppetLockRequest{
		Type:  rizzopb.LockReqType_unlock,
		User:  user,
		Force: force,
	}
	_, unlockedNodes, lockedByAnotherNodes, errLockNodes := rizzoLockNodes(lockReq, ns.nodes.active)
	ns.nodes.active = unlockedNodes
	ns.nodes.errored = mergeNodeMaps(ns.nodes.errored, errLockNodes)
	ns.nodes.locked = mergeNodeMaps(ns.nodes.locked, lockedByAnotherNodes)
	if len(unlockedNodes) == len(ns.nodes.active) {
		logger.Printf("Unlocked all %d requested nodes", len(unlockedNodes))
	} else {
		logger.Printf("Tried to unlock %d nodes, but only succeeded in unlocking, %d", len(ns.nodes.active), len(unlockedNodes))
	}
	for host, str := range compressLockNodes(lockedByAnotherNodes) {
		logger.Printf("Unlock requested, but locked by another: %s, %s", host, str)
	}
	compressedErrNodes := compressErrorNodes(errLockNodes)
	for host, err := range compressedErrNodes {
		logger.Printf("Unlock failed, errNodes: %s, %v", host, err)
	}
}

func nodesLockState(user string, nodes map[string]*Node) (map[string]*Node, map[string]*Node, map[string]*Node, map[string]*Node) {
	lockReq := rizzopb.PuppetLockRequest{
		Type: rizzopb.LockReqType_state,
		User: user,
	}
	lockedNodes, unlockedNodes, lockedByAnotherNodes, errLockNodes := rizzoLockNodes(lockReq, nodes)
	return lockedNodes, unlockedNodes, lockedByAnotherNodes, errLockNodes
}

func rizzoLockNodes(req rizzopb.PuppetLockRequest, nodes map[string]*Node) (map[string]*Node, map[string]*Node, map[string]*Node, map[string]*Node) {
	reportChan := make(chan rizzopb.PuppetLockReport)
	for _, node := range nodes {
		go rizzoLock(node.host, node.rizzoClient, req, reportChan)
	}

	lockedNodes := make(map[string]*Node)
	unlockedNodes := make(map[string]*Node)
	lockedByAnotherNodes := make(map[string]*Node)
	errNodes := make(map[string]*Node)
	var node *Node
	for i := 0; i < len(nodes); i++ {
		r := <-reportChan
		node = nodes[r.Host]
		node.lockState = heckler.LockReportToLockState(r)
		switch node.lockState.LockStatus {
		case heckler.Unlocked:
			unlockedNodes[r.Host] = node
		case heckler.LockedByUser:
			lockedNodes[r.Host] = node
		case heckler.LockedByAnother:
			lockedByAnotherNodes[r.Host] = node
		case heckler.LockUnknown:
			node.err = errors.New(r.Error)
			errNodes[r.Host] = node
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

func lastApplyNodeSet(ns *NodeSet, repo *git.Repository, logger *log.Logger) error {
	var err error
	errNodes := make(map[string]*Node)
	lastApplyNodes := make(map[string]*Node)

	puppetReportChan := make(chan rizzopb.PuppetReport)
	for _, node := range ns.nodes.active {
		go hecklerLastApply(node, puppetReportChan, logger)
	}

	var obj *git.Object
	for range ns.nodes.active {
		r := <-puppetReportChan
		if _, ok := ns.nodes.active[r.Host]; !ok {
			return fmt.Errorf("No Node struct found for report from: %s", r.Host)
		}
		if r.ConfigurationVersion == "" {
			errNodes[r.Host] = ns.nodes.active[r.Host]
			errNodes[r.Host].err = ErrLastApplyUnknown
			continue
		}
		obj, err = repo.RevparseSingle(r.ConfigurationVersion)
		if err != nil {
			errNodes[r.Host] = ns.nodes.active[r.Host]
			errNodes[r.Host].err = fmt.Errorf("Unable to revparse ConfigurationVersion, %s@%s: %v %w", r.ConfigurationVersion, r.Host, err, ErrLastApplyUnknown)
			continue
		}
		lastApplyNodes[r.Host] = ns.nodes.active[r.Host]
		lastApplyNodes[r.Host].lastApply = *obj.Id()
	}
	ns.nodes.active = lastApplyNodes
	ns.nodes.errored = mergeNodeMaps(ns.nodes.errored, errNodes)
	if msg, ok := thresholdExceededNodeSet(ns); ok {
		logger.Printf("%s", msg)
		return ErrThresholdExceeded
	}
	return nil
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
	var err error
	var allNodes *NodeSet
	logger := log.New(os.Stdout, "[applyLoop] ", log.Lshortfile)
	for {
		time.Sleep(10 * time.Second)
		allNodes = &NodeSet{
			name:       "all",
			thresholds: conf.MaxThresholds,
		}
		err = dialNodeSet(conf, allNodes, logger)
		if err != nil {
			logger.Printf("Error: unable to dial node set: %v", err)
			continue
		}
		err = commonTagNodeSet(conf, allNodes, repo, logger)
		if err != nil {
			logger.Printf("Error: unable to query for commonTag: %v", err)
			closeNodeSet(allNodes)
			continue
		}
		logger.Printf("Found common tag: %s", allNodes.commonTag)
		priorTag := allNodes.commonTag
		nextTag, err := nextTag(priorTag, conf.EnvPrefix, repo)
		if err != nil {
			logger.Printf("Error: unable to query for nextTag after '%s', sleeping: %v", priorTag, err)
			closeNodeSet(allNodes)
			continue
		}
		if nextTag == "" {
			logger.Printf("No nextTag found after tag '%s', sleeping", priorTag)
			closeNodeSet(allNodes)
			continue
		}
		ghclient, _, err := githubConn(conf)
		if err != nil {
			logger.Printf("Error: unable to connect to GitHub, sleeping: %v", err)
			closeNodeSet(allNodes)
			continue
		}
		tagIssuesReviewed, err := tagIssuesReviewed(repo, ghclient, conf, priorTag, nextTag)
		if err != nil {
			logger.Printf("Error: unable to determine if noops have been reviewed, sleeping: %v", err)
			closeNodeSet(allNodes)
			continue
		}
		if tagIssuesReviewed {
			err = closeMilestone(nextTag, ghclient, conf)
			if err != nil {
				logger.Printf("Error: unable to close milestone, sleeping: %v", err)
				closeNodeSet(allNodes)
				continue
			}
		} else {
			logger.Printf("Tag '%s' is not ready to apply, sleeping", nextTag)
			closeNodeSet(allNodes)
			continue
		}
		logger.Printf("Tag '%s' is ready to apply, applying...", nextTag)
		err = lockNodeSet("root", conf.LockMessage, false, allNodes, logger)
		if err != nil {
			logger.Printf("Error: unable to lock nodes, sleeping: %v", err)
			closeNodeSet(allNodes)
			continue
		}
		appliedNodes, beyondRevNodes, err := applyNodeSet(allNodes, false, false, nextTag, repo, logger)
		if err != nil {
			logger.Printf("Error: unable to apply nodes, sleeping: %v", err)
			closeNodeSet(allNodes)
			continue
		}
		unlockNodeSet("root", false, allNodes, logger)
		closeNodeSet(allNodes)
		compressedErrNodes := compressErrorNodes(allNodes.nodes.errored)
		for host, err := range compressedErrNodes {
			logger.Printf("errNodes: %s, %v", host, err)
		}
		logger.Printf("Applied nodes: (%d); Beyond rev nodes: (%d); Error nodes: (%d)", len(appliedNodes), len(beyondRevNodes), len(allNodes.nodes.errored))
		logger.Println("Apply complete, sleeping")
	}
}

//  Are there newer commits than our common last applied tag across "all"
//  nodes?
//    If No, do nothing
//    If Yes, check each commit issue for approval
//      Is the issue approved?
//        If No, do nothing
//        If Yes, note approval and close the issue
func noopApprovalLoop(conf *HecklerdConf, repo *git.Repository) {
	var err error
	var allNodes *NodeSet
	logger := log.New(os.Stdout, "[noopApprovalLoop] ", log.Lshortfile)
	for {
		time.Sleep(10 * time.Second)
		allNodes = &NodeSet{
			name:       "all",
			thresholds: conf.MaxThresholds,
		}
		err = dialNodeSet(conf, allNodes, logger)
		if err != nil {
			logger.Printf("Error: unable to dial node set: %v", err)
			continue
		}
		err = commonTagNodeSet(conf, allNodes, repo, logger)
		if err != nil {
			logger.Printf("Error: unable to query for commonTag: %v", err)
			closeNodeSet(allNodes)
			continue
		}
		logger.Printf("Found common tag: %s", allNodes.commonTag)
		closeNodeSet(allNodes)
		_, commits, err := commitLogIdList(repo, allNodes.commonTag, conf.RepoBranch)
		if err != nil {
			logger.Printf("Error: unable to obtain commit log ids: %v", err)
			continue
		}
		if len(commits) == 0 {
			logger.Println("No new commits, sleeping")
			continue
		}
		ghclient, _, err := githubConn(conf)
		if err != nil {
			logger.Printf("Error: unable to connect to GitHub, sleeping: %v", err)
			continue
		}
		for gi, commit := range commits {
			issue, err := githubIssueFromCommit(ghclient, gi, conf)
			if err != nil {
				logger.Printf("Error: unable to determine if issue exists: %s", gi.String())
				continue
			}
			if issue == nil {
				continue
			}
			groupedResources, err := unmarshalGroupedCommit(commit.Id(), groupedNoopDir)
			if os.IsNotExist(err) {
				continue
			} else if err != nil {
				logger.Fatalf("Error: unable to unmarshal groupedCommit: %v", err)
			}
			if issue.GetState() == "closed" {
				continue
			}
			if len(groupedResources) == 0 {
				err := closeIssue(ghclient, conf, issue, "No noop output marking issue as 'closed'")
				if err != nil {
					logger.Printf("Error: unable to close approved issue(%d): %v", issue.GetNumber(), err)
					continue
				}
				continue
			}
			if conf.AutoCloseIssues {
				err := closeIssue(ghclient, conf, issue, "Auto close set, marking issue as 'closed'")
				if err != nil {
					logger.Printf("Error: unable to close approved issue(%d): %v", issue.GetNumber(), err)
					continue
				}
				continue
			}
			noopApproved, err := noopApproved(ghclient, conf, groupedResources, commit, issue)
			if err != nil {
				logger.Printf("Error: unable to determine if issue(%d) is approved: %v", issue.Number, err)
				continue
			}
			if noopApproved {
				err := closeIssue(ghclient, conf, issue, "Issue has been approved, marking issue as 'closed'")
				if err != nil {
					logger.Printf("Error: unable to close approved issue(%d): %v", issue.GetNumber(), err)
					continue
				}
			}
		}
	}
}

// Given a commit, the associated github issue, and the groupedResources check
// if each grouped resource has been approved by a valid approver, exclude
// authors of the commit in the approvers set.
func noopApproved(ghclient *github.Client, conf *HecklerdConf, groupedResources []*groupedResource, commit *git.Commit, issue *github.Issue) (bool, error) {
	var err error
	groupedResources, err = groupedResourcesNodeFiles(groupedResources, workRepo)
	if err != nil {
		return false, err
	}
	groupedResources, err = groupedResourcesOwners(groupedResources, workRepo)
	if err != nil {
		return false, err
	}
	groups, err := githubGroupsForGroupedResources(ghclient, groupedResources)
	if err != nil {
		return false, err
	}
	noopApprovers, err := githubNoopApprovals(ghclient, conf, issue)
	if err != nil {
		return false, err
	}
	commitAuthors, err := commitAuthorsLogins(ghclient, commit)
	if err != nil {
		return false, err
	}
	// Remove commit authors if they approved the noop, since we do not want
	// authors approving their own noops.
	// TODO make this configurable
	approvers := setDifferenceStrSlice(noopApprovers, commitAuthors)
	approved, groupedResources := resourcesApproved(groupedResources, groups, approvers)
	return approved, nil
}

// Given two string slice sets return the elements that are only in a and not
// in b
func setDifferenceStrSlice(a []string, b []string) []string {
	c := make([]string, 0)
	mapB := make(map[string]bool)
	for _, i := range b {
		mapB[i] = true
	}
	for _, i := range a {
		if _, ok := mapB[i]; !ok {
			c = append(c, i)
		}
	}
	return c
}

// Given a slice of groupedResources populate the NodeFiles slice of each
// groupedResource with the set of node files applicable to that
// groupedResource and return the populated groupedResources
func groupedResourcesNodeFiles(groupedResources []*groupedResource, puppetCodePath string) ([]*groupedResource, error) {
	nodeFileRegexes, err := puppetutil.NodeFileRegexes(puppetCodePath + "/nodes")
	if err != nil {
		return nil, err
	}
	// Get node source file for each node in groupedResource
	nodesToFile := make(map[string]string)
	for _, gr := range groupedResources {
		for _, node := range gr.Nodes {
			if _, ok := nodesToFile[node]; !ok {
				nodesToFile[node], err = nodeFile(node, nodeFileRegexes, puppetCodePath)
				if err != nil {
					return nil, err
				}
			}
		}
	}
	// Add unique node files set to groupedResource
	for _, gr := range groupedResources {
		nodeFiles := make([]string, 0)
		for _, node := range gr.Nodes {
			nodeFiles = append(nodeFiles, nodesToFile[node])
		}
		gr.NodeFiles = uniqueStrSlice(nodeFiles)
	}
	return groupedResources, nil
}

// Given a slice of groupedResources populate the groupedResourceOwners struct
// on each grouped resource with the groups & users from the CODEOWNERS file
// who are assigned to the source code files and node files of the grouped
// resource. Return the populated groupedResource slice.
func groupedResourcesOwners(groupedResources []*groupedResource, puppetCodePath string) ([]*groupedResource, error) {
	co, err := codeowners.NewCodeowners(puppetCodePath)
	if err != nil {
		return nil, err
	}
	// Add node file & file approvers
	for _, gr := range groupedResources {
		nodeFilesOwners := make(map[string][]string)
		for _, nodeFile := range gr.NodeFiles {
			nodeFilesOwners[nodeFile] = co.Owners(nodeFile)
		}
		gr.Owners = groupedResourceOwners{
			NodeFiles: nodeFilesOwners,
		}
		if gr.File != "" {
			gr.Owners.File = co.Owners(gr.File)
		}
	}
	return groupedResources, nil
}

// Given a slice of groupedResources return a noopOwners struct with a unique
// set of owned node files and source code files. As well as the complementary
// unique set of unowned node files and source code files.
func groupedResourcesUniqueOwners(groupedResources []*groupedResource) noopOwners {
	no := noopOwners{}
	no.OwnedSourceFiles = make(map[string][]string)
	no.OwnedNodeFiles = make(map[string][]string)
	no.UnownedSourceFiles = make(map[string][]string)
	no.UnownedNodeFiles = make(map[string][]string)
	for _, gr := range groupedResources {
		for nodeFile, owners := range gr.Owners.NodeFiles {
			if len(owners) > 0 {
				if _, ok := no.OwnedNodeFiles[nodeFile]; !ok {
					no.OwnedNodeFiles[nodeFile] = owners
				}
			} else {
				if _, ok := no.UnownedNodeFiles[nodeFile]; !ok {
					no.UnownedNodeFiles[nodeFile] = owners
				}
			}
		}
		if gr.File != "" {
			if len(gr.Owners.File) > 0 {
				if _, ok := no.OwnedSourceFiles[gr.File]; !ok {
					no.OwnedSourceFiles[gr.File] = gr.Owners.File
				}
			} else {
				if _, ok := no.UnownedSourceFiles[gr.File]; !ok {
					no.UnownedSourceFiles[gr.File] = gr.Owners.File
				}
			}
		}
	}
	return no
}

// Given a github issue return the set of github logins which have approved the issue
func githubNoopApprovals(ghclient *github.Client, conf *HecklerdConf, issue *github.Issue) ([]string, error) {
	approvers := make([]string, 0)
	ctx := context.Background()
	comments, _, err := ghclient.Issues.ListComments(ctx, conf.RepoOwner, conf.Repo, issue.GetNumber(), nil)
	if err != nil {
		return nil, err
	}
	regexApproved := regexp.MustCompile(`^[aA]pproved$`)
	for _, comment := range comments {
		commentAuthor := comment.GetUser().GetLogin()
		if commentAuthor == conf.GitHubAppSlug+"[bot]" {
			continue
		}
		if regexApproved.MatchString(comment.GetBody()) {
			approvers = append(approvers, "@"+comment.GetUser().GetLogin())
		}
	}
	return uniqueStrSlice(approvers), nil
}

// Given a slice of groupedResources return a map of group to github logins for
// any resources or nodes owned by groups.
func githubGroupsForGroupedResources(ghclient *github.Client, groupedResources []*groupedResource) (map[string][]string, error) {
	var err error
	groups := make(map[string][]string)
	regexGithubGroup := regexp.MustCompile(`^@.*/.*$`)
	for _, gr := range groupedResources {
		for _, owner := range gr.Owners.File {
			if regexGithubGroup.MatchString(owner) {
				groups[owner] = nil
			}
		}
		for _, nodeFileOwners := range gr.Owners.NodeFiles {
			for _, owner := range nodeFileOwners {
				if regexGithubGroup.MatchString(owner) {
					groups[owner] = nil
				}
			}
		}
	}
	for group := range groups {
		groups[group], err = githubGroupMembersUsernames(ghclient, group)
		if err != nil {
			return nil, err
		}
	}
	return groups, nil
}

// Returns the members of a github group as a slice of github logins
func githubGroupMembersUsernames(ghclient *github.Client, group string) ([]string, error) {
	regexGithubGroupCapture := regexp.MustCompile(`^@(.*)/(.*)$`)
	groupComponents := regexGithubGroupCapture.FindStringSubmatch(group)
	if len(groupComponents) != 3 {
		return nil, fmt.Errorf("Unable to parse group name: '%s'", group)
	}
	org := groupComponents[1]
	slug := groupComponents[2]
	ctx := context.Background()
	// TODO: Once github.com/bradleyfalzon/ghinstallation supports
	// go-github > v30 we can use ListTeamMembersBySlug and avoid
	// two calls to github
	team, _, err := ghclient.Teams.GetTeamBySlug(ctx, org, slug)
	if err != nil {
		return nil, err
	}
	users, _, err := ghclient.Teams.ListTeamMembers(ctx, team.GetID(), nil)
	if err != nil {
		return nil, err
	}
	usernames := make([]string, len(users))
	for i, user := range users {
		usernames[i] = "@" + user.GetLogin()
	}
	return usernames, nil
}

// Given a slice of groupedResources and a slice of approvers, populate the
// groupedResourceApprovals struct on each grouped resource with the valid
// approver if any. Also, keep track of whether each resource was approved.
// Return the populated groupedResource slice as well as a bool set to true if all
// resources were approved.
func resourcesApproved(groupedResources []*groupedResource, groups map[string][]string, approvers []string) (bool, []*groupedResource) {
	approvedResources := 0
	var grApproved bool
	var approvedNodeFiles int
	for _, gr := range groupedResources {
		grApproved = false
		if gr.File != "" {
			gr.Approvals.File = intersectionOwnersApprovers(gr.Owners.File, approvers, groups)
			if len(gr.Approvals.File) > 0 {
				grApproved = true
			}
		}
		nodeFilesApprovers := make(map[string][]string)
		approvedNodeFiles = 0
		for _, nodeFile := range gr.NodeFiles {
			nodeFilesApprovers[nodeFile] = intersectionOwnersApprovers(gr.Owners.NodeFiles[nodeFile], approvers, groups)
			if len(nodeFilesApprovers[nodeFile]) > 0 {
				approvedNodeFiles++
			}
		}
		gr.Approvals.NodeFiles = nodeFilesApprovers
		if grApproved {
			approvedResources++
		} else if (len(gr.NodeFiles) > 0) && (len(gr.NodeFiles) == approvedNodeFiles) {
			approvedResources++
		}
	}
	if len(groupedResources) == approvedResources {
		return true, groupedResources
	} else {
		return false, groupedResources
	}
}

// Given the owners of a resource, the mapping of group name to members, and
// the approvers of the resource; return the intersection of the owners and
// approvers, i.e. the valid approvers.
func intersectionOwnersApprovers(owners []string, approvers []string, groups map[string][]string) []string {
	intersection := make([]string, 0)
	mapOwners := make(map[string]bool)
	for _, owner := range owners {
		if _, ok := groups[owner]; ok {
			for _, user := range groups[owner] {
				mapOwners[user] = true
			}
		} else {
			mapOwners[owner] = true
		}
	}
	for _, i := range approvers {
		if _, ok := mapOwners[i]; ok {
			intersection = append(intersection, i)
		}
	}
	return intersection
}

func unlockAll(conf *HecklerdConf, logger *log.Logger) error {
	var err error
	allNodes := &NodeSet{
		name: "all",
	}
	err = dialNodeSet(conf, allNodes, logger)
	if err != nil {
		return err
	}
	unlockNodeSet("root", false, allNodes, logger)
	unlockedHosts := make([]string, 0)
	for host, _ := range allNodes.nodes.active {
		unlockedHosts = append(unlockedHosts, host)
	}
	logger.Printf("Unlocked: %s", compressHosts(unlockedHosts))
	compressedErrNodes := compressErrorNodes(allNodes.nodes.errored)
	for host, err := range compressedErrNodes {
		logger.Printf("Unlock errNodes: %s, %v", host, err)
	}
	return nil
}

// Are their commits beyond the last tag?
// 	If yes, create a new tag
// If no, do nothing
func tagNewCommits(conf *HecklerdConf, repo *git.Repository) {
	logger := log.New(os.Stdout, "[tagNewCommits] ", log.Lshortfile)
	headCommit, err := gitutil.RevparseToCommit(conf.RepoBranch, repo)
	if err != nil {
		logger.Fatalf("Unable to revparse: %v", err)
	}
	logger.Printf("Commit on branch '%s', '%s'", conf.RepoBranch, headCommit.AsObject().Id().String())
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
	commitLogIds, _, err := commitLogIdList(repo, mostRecentTag, conf.RepoBranch)
	if err != nil {
		logger.Fatalf("Unable to obtain commit log ids: %v", err)
	}
	if len(commitLogIds) == 0 {
		logger.Printf("No new commits since last tag: '%s'", mostRecentTag)
		return
	}
	mostRecentVersion, err := tagToSemver(mostRecentTag, prefix)
	if err != nil {
		logger.Fatalf("Unable to parse version: %v", err)
	}
	newVersion := mostRecentVersion.IncMajor()
	newTag := fmt.Sprintf("%sv%d", tagPrefix(prefix), newVersion.Major())
	ghclient, _, err := githubConn(conf)
	if err != nil {
		logger.Printf("Error: unable to connect to GitHub, returning: %v", err)
		return
	}
	// Check if tag ref already exists on GitHub
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	refs, _, err := ghclient.Git.GetRefs(ctx, conf.RepoOwner, conf.Repo, fmt.Sprintf("refs/tags/%s", newTag))
	if err != nil {
		logger.Printf("Error: unable to get refs from github, returning: %v", err)
		return
	}
	if len(refs) == 1 {
		logger.Printf("New tag ref already exists on github, skipping creation, '%s'", newTag)
		return
	}
	err = createTag(newTag, conf, ghclient, repo)
	if err != nil {
		logger.Printf("Error: unable to create new tag, returning: %v", err)
		return
	}
}

func createTag(newTag string, conf *HecklerdConf, ghclient *github.Client, repo *git.Repository) error {
	timeNow := time.Now()
	tagger := &github.CommitAuthor{
		Date:  &timeNow,
		Name:  github.String("Heckler"),
		Email: github.String("hathaway@paypal.com"),
	}
	commit, err := gitutil.RevparseToCommit(conf.RepoBranch, repo)
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

// Given two git tags, a git repo, and a github client, this function returns
// true if the noops for each commit have been approved.
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

// Given a node set and a user this function returns the combined set of
// unlocked nodes and nodes locked by the provided user as the active set.
// These combined nodes are eligible to be queried by us for their status,
// since they are not locked by another user, or returning an error.
func eligibleNodeSet(user string, ns *NodeSet) {
	lockedNodes, unlockedNodes, lockedByAnotherNodes, errLockNodes := nodesLockState(user, ns.nodes.active)
	ns.nodes.active = mergeNodeMaps(lockedNodes, unlockedNodes)
	ns.nodes.errored = mergeNodeMaps(ns.nodes.errored, errLockNodes)
	ns.nodes.locked = lockedByAnotherNodes
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
	var err error
	var allNodes *NodeSet
	logger := log.New(os.Stdout, "[milestoneLoop] ", log.Lshortfile)
	for {
		time.Sleep(10 * time.Second)
		allNodes = &NodeSet{
			name:       "all",
			thresholds: conf.MaxThresholds,
		}
		err = dialNodeSet(conf, allNodes, logger)
		if err != nil {
			logger.Printf("Unable to dial node set: %v", err)
			continue
		}
		err = commonTagNodeSet(conf, allNodes, repo, logger)
		if err != nil {
			logger.Printf("Unable to query for commonTag: %v", err)
			closeNodeSet(allNodes)
			continue
		}
		logger.Printf("Found common tag: %s", allNodes.commonTag)
		priorTag := allNodes.commonTag
		nextTag, err := nextTag(priorTag, conf.EnvPrefix, repo)
		if err != nil {
			logger.Printf("Error: unable to query for nextTag after '%s', sleeping: %v", priorTag, err)
			closeNodeSet(allNodes)
			continue
		}
		if nextTag == "" {
			logger.Printf("No nextTag found after tag '%s', sleeping", priorTag)
			closeNodeSet(allNodes)
			continue
		}
		closeNodeSet(allNodes)
		ghclient, _, err := githubConn(conf)
		if err != nil {
			logger.Printf("Error: unable to connect to GitHub, sleeping: %v", err)
			continue
		}
		var nextTagMilestone *github.Milestone
		nextTagMilestone, err = milestoneFromTag(nextTag, ghclient, conf)
		if err == context.DeadlineExceeded {
			logger.Println("Error: timeout reaching GitHub for milestone, sleeping")
			continue
		} else if err != nil {
			logger.Printf("Error: unable to find milestone from tag, '%s', sleeping: %v", nextTag, err)
			continue
		}
		if nextTagMilestone == nil {
			nextTagMilestone, err = createMilestone(nextTag, ghclient, conf)
			if err != nil {
				logger.Printf("Error: unable to create new milestone for tag '%s': %v", nextTag, err)
				continue
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
			logger.Printf("Error: No commits between versions: %s..%s, sleeping", priorTag, nextTag)
			continue
		}
		for _, gi := range commitLogIds {
			issue, err := githubIssueFromCommit(ghclient, gi, conf)
			if err != nil {
				logger.Printf("Error: unable to determine if issue exists, %s", gi.String())
				continue
			}
			if issue == nil {
				logger.Printf("No issue exists for, %s", gi.String())
				continue
			}
			issueMilestone := issue.GetMilestone()
			if issueMilestone == nil || *issueMilestone != *nextTagMilestone {
				err = updateIssueMilestone(ghclient, conf, issue, nextTagMilestone)
				if err != nil {
					logger.Printf("Error: unable to update issue milestone: %v", err)
					continue
				}
			}
		}
		logger.Println("All issues updated with milestone, sleeping")
	}
}

// Given two node Threshold structs return a bool and a message indicating
// whether the first threshold exceeded the second.
func thresholdExceeded(cur Thresholds, max Thresholds) (string, bool) {
	if cur.ErrNodes > max.ErrNodes {
		return fmt.Sprintf("Error nodes(%d) exceeds the threshold(%d)", cur.ErrNodes, max.ErrNodes), true
	} else if cur.LockedNodes > max.LockedNodes {
		return fmt.Sprintf("Locked by another nodes(%d) exceeds the threshold(%d)", cur.LockedNodes, max.LockedNodes), true
	}
	return "Thresholds not exceeded", false
}

// Given two node Threshold structs return a bool and a message indicating
// whether the first threshold exceeded the second.
func thresholdExceededNodeSet(ns *NodeSet) (string, bool) {
	if len(ns.nodes.errored) > ns.thresholds.ErrNodes {
		return fmt.Sprintf("Error nodes(%d) exceeds the threshold(%d)", len(ns.nodes.errored), ns.thresholds.ErrNodes), true
	} else if len(ns.nodes.locked) > ns.thresholds.LockedNodes {
		return fmt.Sprintf("Locked by another nodes(%d) exceeds the threshold(%d)", len(ns.nodes.locked), ns.thresholds.LockedNodes), true
	}
	return "Thresholds not exceeded", false
}

func dialNodeSet(conf *HecklerdConf, ns *NodeSet, logger *log.Logger) error {
	nodesToDial, err := setNameToNodes(conf, ns.name, logger)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	ns.nodes.dialed, ns.nodes.errored = dialNodes(ctx, nodesToDial)
	ns.nodes.active = ns.nodes.dialed
	if msg, ok := thresholdExceededNodeSet(ns); ok {
		logger.Printf("%s", msg)
		return ErrThresholdExceeded
	}
	return nil
}

// Return the most recent tag across all nodes in an environment
func commonTagNodeSet(conf *HecklerdConf, ns *NodeSet, repo *git.Repository, logger *log.Logger) error {
	eligibleNodeSet("root", ns)
	if msg, ok := thresholdExceededNodeSet(ns); ok {
		logger.Printf("%s", msg)
		return ErrThresholdExceeded
	}
	err := lastApplyNodeSet(ns, repo, logger)
	if err != nil {
		return err
	}
	commonTag, err := commonAncestorTag(ns.nodes.active, conf.EnvPrefix, repo, logger)
	if err != nil {
		return err
	}
	if commonTag == "" {
		return errors.New("No common tag found, sleeping")
	}
	ns.commonTag = commonTag
	return nil
}

//  Are there newer commits than our common last applied tag across the "all"
//  node set?
//    If No, do nothing
//    If Yes,
//      - noop each commit or load serialzed copy
//      - create github issue, if it does not exist
func noopLoop(conf *HecklerdConf, repo *git.Repository, templates *template.Template) {
	var err error
	var allNodes *NodeSet
	logger := log.New(os.Stdout, "[noopLoop] ", log.Lshortfile)
	for {
		time.Sleep(10 * time.Second)
		allNodes = &NodeSet{
			name:       "all",
			thresholds: conf.MaxThresholds,
		}
		err = dialNodeSet(conf, allNodes, logger)
		if err != nil {
			logger.Printf("Unable to dial node set: %v", err)
			continue
		}
		err = commonTagNodeSet(conf, allNodes, repo, logger)
		if err != nil {
			logger.Printf("Unable to query for commonTag: %v", err)
			closeNodeSet(allNodes)
			continue
		}
		logger.Printf("Found common tag: %s", allNodes.commonTag)
		_, commits, err := commitLogIdList(repo, allNodes.commonTag, conf.RepoBranch)
		if err != nil {
			logger.Fatalf("Unable to obtain commit log ids: %v", err)
		}
		if len(commits) == 0 {
			logger.Println("No new commits, sleeping")
			closeNodeSet(allNodes)
			continue
		}
		closeNodeSet(allNodes)
		var groupedCommit []*groupedResource
		var perNoop *NodeSet
		for gi, commit := range commits {
			groupedCommit, err = unmarshalGroupedCommit(commit.Id(), groupedNoopDir)
			if os.IsNotExist(err) {
				perNoop = &NodeSet{
					name:       "all",
					thresholds: conf.MaxThresholds,
				}
				err = dialNodeSet(conf, perNoop, logger)
				if err != nil {
					logger.Printf("Unable to dial node set: %v", err)
					break
				}
				err = lastApplyNodeSet(perNoop, repo, logger)
				if err != nil {
					logger.Printf("Unable to get last apply for node set: %v", err)
					closeNodeSet(perNoop)
					break
				}
				err = lockNodeSet("root", conf.LockMessage, false, perNoop, logger)
				if err != nil {
					logger.Printf("Unable to lock nodes, sleeping, %v", err)
					unlockNodeSet("root", false, perNoop, logger)
					break
				}
				for _, node := range perNoop.nodes.active {
					node.commitReports = make(map[git.Oid]*rizzopb.PuppetReport)
					node.commitDeltaResources = make(map[git.Oid]map[ResourceTitle]*deltaResource)
				}
				groupedCommit, err = noopNodeSet(perNoop, commit, true, repo, logger)
				if err != nil {
					logger.Printf("Unable to noop commit: %s, sleeping, %v", gi.String(), err)
					unlockNodeSet("root", false, perNoop, logger)
					closeNodeSet(perNoop)
					continue
				}
				unlockNodeSet("root", false, perNoop, logger)
				closeNodeSet(perNoop)
				err = marshalGroupedCommit(commit.Id(), groupedCommit, groupedNoopDir)
				if err != nil {
					logger.Fatalf("Error: unable to marshal groupedCommit: %v", err)
				}
			} else if err != nil {
				logger.Fatalf("Error: unable to unmarshal groupedCommit: %v", err)
			}
			ghclient, _, err := githubConn(conf)
			if err != nil {
				logger.Printf("Error: unable to connect to GitHub, sleeping: %v", err)
				continue
			}
			issue, err := githubIssueFromCommit(ghclient, gi, conf)
			if err != nil {
				logger.Printf("Error: unable to determine if issue for commit %s exists: %v", gi.String(), err)
				continue
			}
			if issue == nil {
				issue, err = githubCreateIssue(ghclient, conf, commit, groupedCommit, templates)
				if err != nil {
					logger.Printf("Error: unable to create github issue: %v", err)
					continue
				}
				logger.Printf("Successfully created new issue: '%v'", issue.GetTitle())
			}
		}
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

// Takes repo, a commit in the repo, and a list of nodes with their lastApply
// populated. Returns true if all the nodes lastApplies are in the lineage of
// the provided commit.
//
// If a commit is not, equal, an ancestor, or a descendant of a node's last
// applied commit, then we cannot noop that commit accurately, because the last
// applied commit has children which we do not have in our graph and those
// children may have source code changes which we do not have.
func commitInAllNodeLineages(commit git.Oid, nodes map[string]*Node, repo *git.Repository, logger *log.Logger) bool {
	commitInLineages := true
	for _, node := range nodes {
		if node.lastApply.IsZero() {
			logger.Fatalf("lastApply for node, '%s', is zero", node.host)
		}
		if node.lastApply.Equal(&commit) {
			continue
		}
		descendant, err := repo.DescendantOf(&node.lastApply, &commit)
		if err != nil {
			logger.Fatalf("Cannot determine descendant status: %v", err)
		}
		if descendant {
			continue
		}
		descendant, err = repo.DescendantOf(&commit, &node.lastApply)
		if err != nil {
			logger.Fatalf("Cannot determine descendant status: %v", err)
		}
		if descendant {
			continue
		}
		commitInLineages = false
		break
	}
	return commitInLineages
}

// Given a set of Node structs with their lastApply value populated, an
// environment prefix, and a git repository. Determine if there is a common git
// tag among the Nodes. If there is a common tag return the most recent one.
func commonAncestorTag(nodes map[string]*Node, prefix string, repo *git.Repository, logger *log.Logger) (string, error) {
	// Calculate the set of tags to Node slice
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

	// Return the earliest version from the set, which should be the common
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

	if conf.RepoBranch == "" {
		logger.Println("No branch specified in config, please add RepoBranch")
		os.Exit(1)
	}

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
	os.MkdirAll(noopDir, 0755)
	os.MkdirAll(groupedNoopDir, 0755)
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
				logger.Printf("Unable to fetch repo, sleeping: %v", err)
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
		go noopApprovalLoop(conf, repo)
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

	// TODO any reason to make this a separate goroutine?
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
