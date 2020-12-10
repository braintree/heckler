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
	"net/url"
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

	"github.com/Masterminds/semver/v3"
	"github.com/Masterminds/sprig"
	"github.com/bradleyfalzon/ghinstallation"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/go-github/v29/github"
	codeowners "github.com/hairyhenderson/go-codeowners"
	git "github.com/libgit2/git2go/v31"
	gitcgiserver "github.com/lollipopman/git-cgi-server"
	"github.com/lollipopman/heckler/internal/gitutil"
	"github.com/lollipopman/heckler/internal/heckler"
	"github.com/lollipopman/heckler/internal/hecklerpb"
	"github.com/lollipopman/heckler/internal/puppetutil"
	"github.com/lollipopman/heckler/internal/rizzopb"
	"github.com/robfig/cron/v3"
	"github.com/square/grange"
	"google.golang.org/grpc"
	"gopkg.in/yaml.v3"
)

var Version string
var ErrLastApplyUnknown = errors.New("Unable to determine lastApply commit, use force flag to update")
var ErrThresholdExceeded = errors.New("Threshold for err nodes or lock nodes exceeded")

type lastApplyStatus int
type noopApproverType int

const (
	lastApplyClean lastApplyStatus = iota
	lastApplyDirty
	lastApplyErrored
)

const (
	notApproved noopApproverType = iota
	codeownersApproved
	adminApproved
)

const (
	ApplicationName = "git-cgi-server"
	shutdownTimeout = time.Second * 5
	// HACK: Bind to ipv4
	// TODO: move to HecklerdConf
	defaultAddr = "0.0.0.0:8080"
	port        = ":50052"
)

var Debug = false

// TODO: this regex also matches, Node[__node_regexp__fozzie], which causes
// resources in node blocks to be mistaken for define types. Is there a more
// robust way to match for define types?
var RegexDefineType = regexp.MustCompile(`^[A-Z][a-zA-Z0-9_:]*\[[^\]]+\]$`)

type HecklerdConf struct {
	Repo                       string                `yaml:"repo"`
	RepoOwner                  string                `yaml:"repo_owner"`
	RepoBranch                 string                `yaml:"repo_branch"`
	GitHubDomain               string                `yaml:"github_domain"`
	GitHubPrivateKeyPath       string                `yaml:"github_private_key_path"`
	GitHubAppSlug              string                `yaml:"github_app_slug"`
	GitHubAppId                int64                 `yaml:"github_app_id"`
	GitHubAppEmail             string                `yaml:"github_app_email"`
	GitHubAppInstallId         int64                 `yaml:"github_app_install_id"`
	GitHubDisableNotifications bool                  `yaml:"github_disable_notifications"`
	GitHubHttpProxy            string                `yaml:"github_http_proxy"`
	IgnoredResources           []string              `yaml:"ignored_resources"`
	NodeSets                   map[string]NodeSetCfg `yaml:"node_sets"`
	AutoTagCronSchedule        string                `yaml:"auto_tag_cron_schedule"`
	AutoCloseIssues            bool                  `yaml:"auto_close_issues"`
	EnvPrefix                  string                `yaml:"env_prefix"`
	MaxNodeThresholds          NodeThresholds        `yaml:"max_node_thresholds"`
	GitServerMaxClients        int                   `yaml:"git_server_max_clients"`
	ManualMode                 bool                  `yaml:"manual_mode"`
	LockMessage                string                `yaml:"lock_message"`
	LoopNoopSleepSeconds       int                   `yaml:"loop_noop_sleep_seconds"`
	LoopMilestoneSleepSeconds  int                   `yaml:"loop_milestone_sleep_seconds"`
	LoopApplySleepSeconds      int                   `yaml:"loop_apply_sleep_seconds"`
	LoopApprovalSleepSeconds   int                   `yaml:"loop_approval_sleep_seconds"`
	LoopDirtySleepSeconds      int                   `yaml:"loop_dirty_sleep_seconds"`
	ApplySetOrder              []string              `yaml:"apply_set_order"`
	ApplySetSleepSeconds       int                   `yaml:"apply_set_sleep_seconds"`
	StateDir                   string                `yaml:"state_dir"`
	WorkRepo                   string                `yaml:"work_repo"`
	ServedRepo                 string                `yaml:"served_repo"`
	NoopDir                    string                `yaml:"noop_dir"`
	GroupedNoopDir             string                `yaml:"grouped_noop_dir"`
	AdminOwners                []string              `yaml:"admin_owners"`
	ModulesPaths               []string              `yaml:"module_paths"`
}

type NodeSetCfg struct {
	Cmd       []string `yaml:"cmd"`
	Blacklist []string `yaml:"blacklist"`
}

type NodeSet struct {
	name           string
	commonTag      string
	nodeThresholds NodeThresholds
	nodes          Nodes
}

type Nodes struct {
	active          map[string]*Node
	dialed          map[string]*Node
	errored         map[string]*Node
	locked          map[string]*Node
	lockedByAnother map[string]*Node
}

type NodeThresholds struct {
	Errored         int `yaml:"errored"`
	LockedByAnother int `yaml:"locked_by_another"`
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

type dirtyNoops struct {
	rev       git.Oid
	commitIds map[git.Oid]bool
}

type cleanNodeResult struct {
	host  string
	clean bool
	dn    dirtyNoops
	err   error
}

type deltaResource struct {
	Title           ResourceTitle
	Type            string
	DefineType      string
	File            string
	Line            int64
	ContainmentPath []string
	Events          []*rizzopb.Event
	Logs            []*rizzopb.Log
}

type groupedReport struct {
	CommitNotInAllNodeLineages bool
	ParentNoopFailures         bool
	Failures                   []*groupedFailure
	Resources                  []*groupedResource
	CompressedErrored          map[string]string
	CompressedBeyondRev        map[string]string
	CompressedLockedByAnother  map[string]string
}

type groupedFailure struct {
	Source          string
	Nodes           []string
	CompressedNodes string
	Log             groupLog
}

type groupedResource struct {
	Title           ResourceTitle
	Type            string
	DefineType      string
	Diff            string
	File            string
	Line            int64
	ContainmentPath []string
	Nodes           []string
	NodeFiles       []string
	CompressedNodes string
	Events          []*groupEvent
	Logs            []*groupLog
	Owners          groupedResourceOwners
	Approvals       groupedResourceApprovals
	AdminApprovals  []string
}

type groupedResourceOwners struct {
	File      []string
	Module    []string
	NodeFiles map[string][]string
}

type groupedResourceApprovals struct {
	File      []string
	Module    []string
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

func commitParentReports(commit git.Commit, lastApply git.Oid, commitReports map[git.Oid]*rizzopb.PuppetReport, host string, repo *git.Repository, logger *log.Logger) (bool, []*rizzopb.PuppetReport) {
	var parentReport *rizzopb.PuppetReport
	parentReports := make([]*rizzopb.PuppetReport, 0)
	parentCount := commit.ParentCount()
	parentFailures := false
	for i := uint(0); i < parentCount; i++ {
		parentReport = commitReports[*commit.ParentId(i)]
		if parentReport == nil {
			logger.Fatalf("Parent report not found %s for commit %s@%s", commit.ParentId(i).String(), host, commit.Id().String())
		} else {
			parentReports = append(parentReports, parentReport)
			if parentReport.Status == "failed" {
				parentFailures = true
			}
		}
	}
	return parentFailures, parentReports
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
	descendant, err := repo.DescendantOf(&node.lastApply, &commit)
	if err != nil {
		logger.Fatalf("Cannot determine descendant status: %v", err)
	}
	if descendant || node.lastApply.Equal(&commit) {
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

func normalizeReport(rprt rizzopb.PuppetReport, logger *log.Logger) rizzopb.PuppetReport {
	for _, resourceStatus := range rprt.ResourceStatuses {
		// Strip off the puppet confdir prefix, so we are left with the relative
		// path of the source file in the code repo
		if resourceStatus.File != "" {
			resourceStatus.File = strings.TrimPrefix(resourceStatus.File, rprt.Confdir+"/")
		}
	}
	rprt.Logs = normalizeLogs(rprt.Logs, logger)
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
func marshalGroupedReport(oid *git.Oid, gr groupedReport, groupedNoopDir string) error {
	groupedReportPath := groupedNoopDir + "/" + oid.String() + ".json"
	data, err := json.Marshal(gr)
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(groupedReportPath, data, 0644)
	if err != nil {
		return err
	}
	return nil
}

func unmarshalGroupedReport(oid *git.Oid, groupedNoopDir string) (groupedReport, error) {
	groupedReportPath := groupedNoopDir + "/" + oid.String() + ".json"
	file, err := os.Open(groupedReportPath)
	if err != nil {
		return groupedReport{}, err
	}
	data, err := ioutil.ReadAll(file)
	if err != nil {
		return groupedReport{}, err
	}
	var gr groupedReport
	err = json.Unmarshal([]byte(data), &gr)
	if err != nil {
		return groupedReport{}, err
	}
	return gr, nil
}

func noopNodeSet(ns *NodeSet, commitId git.Oid, repo *git.Repository, noopDir string, conf *HecklerdConf, logger *log.Logger) error {
	var err error
	var rprt *rizzopb.PuppetReport
	errNoopNodes := make(map[string]*Node)
	lockedByAnotherNoopNodes := make(map[string]*Node)
	puppetReportChan := make(chan applyResult)
	noopHosts := make(map[string]bool)
	for host, node := range ns.nodes.active {
		if rprt, err = loadNoop(commitId, node, noopDir, repo, logger); err == nil {
			ns.nodes.active[node.host].commitReports[commitId] = rprt
		} else if os.IsNotExist(err) {
			rizzoLockNode(
				rizzopb.PuppetLockRequest{
					Type:    rizzopb.LockReqType_lock,
					User:    "root",
					Comment: conf.LockMessage,
					Force:   false,
				},
				node)
			switch node.lockState.LockStatus {
			case heckler.LockedByAnother:
				lockedByAnotherNoopNodes[host] = node
				delete(ns.nodes.active, host)
				continue
			case heckler.LockUnknown:
				errNoopNodes[host] = node
				delete(ns.nodes.active, host)
				logger.Println(errNoopNodes[host].err)
				continue
			case heckler.LockedByUser:
				par := rizzopb.PuppetApplyRequest{Rev: commitId.String(), Noop: true}
				go hecklerApply(node, puppetReportChan, par)
				noopHosts[node.host] = true
			}
		} else {
			logger.Fatalf("Unable to load noop: %v", err)
		}
	}
	noopRequests := len(noopHosts)
	if noopRequests > 0 {
		logger.Printf("Requesting noops for %s: %s", commitId.String(), compressHostsMap(noopHosts))
	}
	for j := 0; j < noopRequests; j++ {
		logger.Printf("Waiting for (%d) outstanding noop requests: %s", noopRequests-j, compressHostsMap(noopHosts))
		r := <-puppetReportChan
		rizzoLockNode(
			rizzopb.PuppetLockRequest{
				Type:  rizzopb.LockReqType_unlock,
				User:  "root",
				Force: false,
			}, ns.nodes.active[r.host])
		if ns.nodes.active[r.host].lockState.LockStatus != heckler.Unlocked {
			logger.Printf("Unlock of %s failed", r.host)
		}
		if r.err != nil {
			ns.nodes.active[r.host].err = fmt.Errorf("Noop failed: %w", r.err)
			errNoopNodes[r.host] = ns.nodes.active[r.host]
			logger.Println(errNoopNodes[r.host].err)
			delete(ns.nodes.active, r.host)
			delete(noopHosts, r.host)
			continue
		}
		newRprt := normalizeReport(r.report, logger)
		// Failed reports are created by rizzod, so they lack the Host field
		// which is set by Puppet
		if newRprt.Status == "failed" {
			newRprt.Host = r.host
		}
		logger.Printf("Received noop: %s@%s", newRprt.Host, newRprt.ConfigurationVersion)
		delete(noopHosts, newRprt.Host)
		commitId, err := git.NewOid(newRprt.ConfigurationVersion)
		if err != nil {
			logger.Fatalf("Unable to convert ConfigurationVersion to a git oid: %v", err)
		}
		ns.nodes.active[newRprt.Host].commitReports[*commitId] = &newRprt
		err = marshalReport(newRprt, noopDir, *commitId)
		if err != nil {
			logger.Fatalf("Unable to marshal report: %v", err)
		}
	}
	ns.nodes.errored = mergeNodeMaps(ns.nodes.errored, errNoopNodes)
	ns.nodes.lockedByAnother = mergeNodeMaps(ns.nodes.lockedByAnother, lockedByAnotherNoopNodes)
	if ok := thresholdExceededNodeSet(ns, logger); ok {
		return ErrThresholdExceeded
	}
	return nil
}

func groupReportNodeSet(ns *NodeSet, commit *git.Commit, deltaNoop bool, repo *git.Repository, ignoredResources []string, noopDir string, conf *HecklerdConf, logger *log.Logger) (groupedReport, error) {
	var err error
	for host, _ := range ns.nodes.active {
		os.Mkdir(noopDir+"/"+host, 0755)
	}

	// If the commit is not part of every nodes lineage we are unable to create a
	// deltaNoop, since we can't subtract the parents as the parents would not
	// necessarily include changes from the parents children
	//
	// If some node's lastApply is commit B we can't subtract commit A's noop from C since
	// it would not include the changes introduced by commit B.
	//
	// * commit D
	// |\
	// | * commit C
	// * | commit B
	// |/
	// * commit A
	//
	if deltaNoop && !commitInAllNodeLineages(*commit.Id(), ns.nodes.active, repo, logger) {
		return groupedReport{CommitNotInAllNodeLineages: true}, nil
	}

	commitIdsToNoop := make([]git.Oid, 0)
	commitIdsToNoop = append(commitIdsToNoop, *commit.Id())

	parentCount := commit.ParentCount()
	for i := uint(0); i < parentCount; i++ {
		if deltaNoop {
			commitIdsToNoop = append(commitIdsToNoop, *commit.ParentId(i))
		} else {
			for _, node := range ns.nodes.active {
				node.commitReports[*commit.ParentId(i)] = &rizzopb.PuppetReport{}
				node.commitDeltaResources[*commit.ParentId(i)] = make(map[ResourceTitle]*deltaResource)
			}
		}
	}

	for _, commitId := range commitIdsToNoop {
		err = noopNodeSet(ns, commitId, repo, noopDir, conf, logger)
		if err != nil {
			return groupedReport{}, err
		}
	}

	parentNoopFailures := false
	for _, node := range ns.nodes.active {
		logger.Printf("Creating delta resource for commit %s@%s", node.host, commit.Id().String())
		parentFailures, parentReports := commitParentReports(*commit, node.lastApply, node.commitReports, node.host, repo, logger)
		if parentFailures {
			parentNoopFailures = true
		}
		node.commitDeltaResources[*commit.Id()] = subtractNoops(node.commitReports[*commit.Id()], parentReports, ignoredResources)
	}

	logger.Printf("Grouping: %s", commit.Id().String())
	groupedResources := make([]*groupedResource, 0)
	for _, node := range ns.nodes.active {
		for _, nodeDeltaRes := range node.commitDeltaResources[*commit.Id()] {
			groupedResources = append(groupedResources, groupResources(*commit.Id(), nodeDeltaRes, ns.nodes.active))
		}
	}
	groupedFailures := make([]*groupedFailure, 0)
	for _, node := range ns.nodes.active {
		if node.commitReports[*commit.Id()].Status != "failed" {
			continue
		}
		for _, puppetLog := range node.commitReports[*commit.Id()].Logs {
			if puppetLog.Source == "EvalError" {
				groupedFailures = append(groupedFailures, groupFailures(*commit.Id(), puppetLog, ns.nodes.active))
			}
		}
	}
	compressedErrStrNodes := make(map[string]string)
	compressedErrNodes := compressErrorNodes(ns.nodes.errored)
	for host, err := range compressedErrNodes {
		compressedErrStrNodes[host] = err.Error()
	}
	beyondRevNodes := make(map[string]string)
	for host, node := range ns.nodes.active {
		if commitAlreadyApplied(node.lastApply, *commit.Id(), repo) {
			beyondRevNodes[host] = node.lastApply.String()
		}
	}
	gr := groupedReport{
		ParentNoopFailures:        parentNoopFailures,
		Resources:                 groupedResources,
		Failures:                  groupedFailures,
		CompressedErrored:         compressedErrStrNodes,
		CompressedBeyondRev:       compressHostsStr(beyondRevNodes),
		CompressedLockedByAnother: compressLockNodes(ns.nodes.lockedByAnother),
	}
	return gr, nil
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
	deltaRes.ContainmentPath = r.ContainmentPath
	return deltaRes
}

func subtractNoops(commitNoop *rizzopb.PuppetReport, priorCommitNoops []*rizzopb.PuppetReport, ignoredResources []string) map[ResourceTitle]*deltaResource {
	var deltaEvents []*rizzopb.Event
	var deltaLogs []*rizzopb.Log
	var deltaResources map[ResourceTitle]*deltaResource
	var resourceTitle ResourceTitle

	deltaResources = make(map[ResourceTitle]*deltaResource)
	ignoredResourcesMap := make(map[string]bool)
	for _, resource := range ignoredResources {
		ignoredResourcesMap[resource] = true
	}

	if commitNoop.ResourceStatuses == nil {
		return deltaResources
	}

	for resourceTitleStr, r := range commitNoop.ResourceStatuses {
		if _, ok := ignoredResourcesMap[resourceTitleStr]; ok {
			continue
		}
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

func compressNodesMap(nodesMap map[string]*Node) string {
	hosts := make([]string, len(nodesMap))
	i := 0
	for k, _ := range nodesMap {
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
		compressedHostErr[compressHosts(hosts)] = fmt.Errorf("%s", errType)
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
		compressedHostStr[compressHosts(hosts)] = fmt.Sprintf("User: '%s' Comment: '%s'", ls.User, ls.Comment)
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

func groupFailures(gi git.Oid, targetPuppetLog *rizzopb.Log, nodes map[string]*Node) *groupedFailure {
	nodeList := make([]string, 0)
	for nodeName, node := range nodes {
		unmatched := make([]*rizzopb.Log, 0)
		for _, puppetLog := range node.commitReports[gi].Logs {
			if cmp.Equal(targetPuppetLog, puppetLog) {
				nodeList = append(nodeList, nodeName)
			} else {
				unmatched = append(unmatched, puppetLog)
			}
		}
		node.commitReports[gi].Logs = unmatched
	}

	gf := new(groupedFailure)
	gf.Source = targetPuppetLog.Source
	sort.Strings(nodeList)
	gf.Nodes = nodeList
	gf.CompressedNodes = compressHosts(nodeList)
	gf.Log = groupLog{
		Level:   targetPuppetLog.Level,
		Message: targetPuppetLog.Message,
	}
	return gf
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

func normalizeLogs(puppetLogs []*rizzopb.Log, logger *log.Logger) []*rizzopb.Log {
	var newSource string
	var origSource string
	var newPuppetLogs []*rizzopb.Log

	// extract resource from log source
	regexResourcePropertyTail := regexp.MustCompile(`/[a-z][a-z0-9_]*$`)
	regexResourceTail := regexp.MustCompile(`[^\/]+\[[^\[\]]+\]$`)

	// normalize diff
	regexFileContent := regexp.MustCompile(`File\[.*content$`)
	regexDiff := regexp.MustCompile(`(?s)^.---`)

	// Log referring to a puppet resource
	regexResource := regexp.MustCompile(`^/Stage`)

	// Log msg values to drop
	regexCurValMsg := regexp.MustCompile(`^current_value`)
	regexApplyMsg := regexp.MustCompile(`^Applied catalog`)
	regexRefreshMsg := regexp.MustCompile(`^Would have triggered 'refresh'`)
	// This message content is duplicated by the current and desired states of a
	// File resource
	regexContentChanged := regexp.MustCompile(`^content changed `)

	// Log sources to drop
	regexClass := regexp.MustCompile(`^Class\[`)
	regexStage := regexp.MustCompile(`^Stage\[`)

	// Eval Error Message, strip node name from the message which prevents
	// grouping the errors
	regexEvalMsg := regexp.MustCompile(` on node .*`)

	for _, puppetLog := range puppetLogs {
		origSource = ""
		newSource = ""
		if regexCurValMsg.MatchString(puppetLog.Message) ||
			regexApplyMsg.MatchString(puppetLog.Message) {
			if Debug {
				logger.Printf("Dropping Log: %v: %v", puppetLog.Source, puppetLog.Message)
			}
			continue
		} else if regexClass.MatchString(puppetLog.Source) ||
			regexStage.MatchString(puppetLog.Source) ||
			RegexDefineType.MatchString(puppetLog.Source) {
			if Debug {
				logger.Printf("Dropping Log: %v: %v", puppetLog.Source, puppetLog.Message)
			}
			continue
		} else if (!regexResource.MatchString(puppetLog.Source)) && regexRefreshMsg.MatchString(puppetLog.Message) {
			if Debug {
				logger.Printf("Dropping Log: %v: %v", puppetLog.Source, puppetLog.Message)
			}
			continue
		} else if regexResource.MatchString(puppetLog.Source) && regexContentChanged.MatchString(puppetLog.Message) {
			if Debug {
				logger.Printf("Dropping Log: %v: %v", puppetLog.Source, puppetLog.Message)
			}
			continue
		} else if puppetLog.Source == "EvalError" {
			puppetLog.Message = regexEvalMsg.ReplaceAllString(puppetLog.Message, "")
			newPuppetLogs = append(newPuppetLogs, puppetLog)
		} else if regexResource.MatchString(puppetLog.Source) {
			origSource = puppetLog.Source
			newSource = regexResourcePropertyTail.ReplaceAllString(puppetLog.Source, "")
			newSource = regexResourceTail.FindString(newSource)
			if newSource == "" {
				logger.Printf("newSource is empty!")
				logger.Printf("Log: '%v' -> '%v': %v", origSource, newSource, puppetLog.Message)
				os.Exit(1)
			}

			if regexFileContent.MatchString(puppetLog.Source) && regexDiff.MatchString(puppetLog.Message) {
				puppetLog.Message = normalizeDiff(puppetLog.Message)
			}
			puppetLog.Source = newSource
			if Debug {
				logger.Printf("Adding Log: '%v' -> '%v': %v", origSource, newSource, puppetLog.Message)
			}
			// TODO: If we wrote a custom equality function, rather than using cmp,
			// we could ignore source code line numbers in logs, but since we are using
			// cmp at present, just set it equal to 0 for all logs.
			puppetLog.Line = 0
			newPuppetLogs = append(newPuppetLogs, puppetLog)
		} else {
			logger.Printf("Unaccounted for Log: %v: %v", puppetLog.Source, puppetLog.Message)
			newPuppetLogs = append(newPuppetLogs, puppetLog)
		}
	}

	return newPuppetLogs
}

func hecklerApply(node *Node, c chan<- applyResult, par rizzopb.PuppetApplyRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*10)
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
	ns := &NodeSet{
		// Disable thresholds for a heckler client request
		nodeThresholds: NodeThresholds{
			Errored:         -1,
			LockedByAnother: -1,
		},
	}
	if len(hosts) > 0 {
		hostsToDial = hosts
	} else {
		ns.name = nodeSetName
		hostsToDial, err = setNameToNodes(conf, nodeSetName, logger)
		if err != nil {
			return nil, err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	ns.nodes.dialed, ns.nodes.errored = dialNodes(ctx, hostsToDial)
	ns.nodes.active = copyNodeMap(ns.nodes.dialed)
	return ns, nil
}

func setNameToNodes(conf *HecklerdConf, nodeSetName string, logger *log.Logger) ([]string, error) {
	if nodeSetName == "" {
		return nil, errors.New("Empty nodeSetName provided")
	}
	var setCfg NodeSetCfg
	var ok bool
	if setCfg, ok = conf.NodeSets[nodeSetName]; !ok {
		return nil, errors.New(fmt.Sprintf("nodeSetName '%s' not found in hecklerd config", nodeSetName))
	}
	// Change to code dir, so hiera relative paths resolve
	cmd := exec.Command(setCfg.Cmd[0], setCfg.Cmd[1:]...)
	cmd.Dir = conf.WorkRepo
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
	for _, sregex := range setCfg.Blacklist {
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
		return nil, errors.New(fmt.Sprintf("Node set '%s': '%v' produced zero nodes", nodeSetName, setCfg))
	}
	logger.Printf("Node set '%s' loaded, nodes (%d), blacklisted nodes (%d)", nodeSetName, len(filteredNodes), len(blacklistedNodes))
	if len(blacklistedNodes) > 0 {
		logger.Printf("Blacklisted nodes: %s", compressHosts(blacklistedNodes))
	}
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

// Given a node map, return a copy
func copyNodeMap(nodeMap map[string]*Node) map[string]*Node {
	nodeMapCopy := make(map[string]*Node)
	for k, v := range nodeMap {
		nodeMapCopy[k] = v
	}
	return nodeMapCopy
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
	defer closeNodeSet(ns, logger)
	har := new(hecklerpb.HecklerApplyReport)
	if req.Noop {
		err := lastApplyNodeSet(ns, hs.repo, logger)
		if err != nil {
			return nil, err
		}
		err = lockNodeSet(req.User, hs.conf.LockMessage, false, ns, logger)
		if err != nil {
			logger.Printf("Unable to lock nodes, sleeping, %v", err)
			closeNodeSet(ns, logger)
			return nil, err
		}
		defer unlockNodeSet(req.User, false, ns, logger)
		for _, node := range ns.nodes.active {
			node.commitReports = make(map[git.Oid]*rizzopb.PuppetReport)
			node.commitDeltaResources = make(map[git.Oid]map[ResourceTitle]*deltaResource)
		}
		groupedReport, err := groupReportNodeSet(ns, commit, req.DeltaNoop, hs.repo, hs.conf.IgnoredResources, hs.conf.NoopDir, hs.conf, logger)
		if err != nil {
			return nil, err
		}
		har.Output, err = commitToMarkdown(hs.conf, commit, groupedReport, hs.templates)
		if err != nil {
			return nil, err
		}
	} else {
		appliedNodes, beyondRevNodes, err := applyNodeSet(ns, req.Force, req.Noop, req.Rev, hs.repo, hs.conf.LockMessage, logger)
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
	for host, node := range ns.nodes.lockedByAnother {
		har.NodeErrors[host] = fmt.Sprintf("%s: %s", node.lockState.User, node.lockState.Comment)
	}
	return har, nil
}

func applyNodeSet(ns *NodeSet, forceApply bool, noop bool, rev string, repo *git.Repository, lockMsg string, logger *log.Logger) (map[string]*Node, map[string]*Node, error) {
	var err error
	beyondRevNodes := make(map[string]*Node)
	appliedNodes := make(map[string]*Node)

	// Check node revision if not force applying
	if !forceApply {
		err = lastApplyNodeSet(ns, repo, logger)
		if err != nil {
			return nil, nil, err
		}
		obj, err := gitutil.RevparseToCommit(rev, repo)
		if err != nil {
			return nil, nil, err
		}
		revId := *obj.Id()
		for host, node := range ns.nodes.active {
			if commitAlreadyApplied(node.lastApply, revId, repo) {
				beyondRevNodes[host] = node
				delete(ns.nodes.active, host)
			}
		}
	}
	err = lockNodeSet("root", lockMsg, false, ns, logger)
	if err != nil {
		closeNodeSet(ns, logger)
		return nil, nil, err
	}
	par := rizzopb.PuppetApplyRequest{Rev: rev, Noop: noop}
	puppetReportChan := make(chan applyResult)
	for _, node := range ns.nodes.active {
		go hecklerApply(node, puppetReportChan, par)
	}

	errApplyNodes := make(map[string]*Node)
	for range ns.nodes.active {
		r := <-puppetReportChan
		if r.err != nil {
			ns.nodes.active[r.host].err = fmt.Errorf("Apply failed: %w", r.err)
			errApplyNodes[r.host] = ns.nodes.active[r.host]
		} else if r.report.Status == "failed" {
			ns.nodes.active[r.host].err = fmt.Errorf("Apply status: '%s', %s", r.report.Status, r.report.ConfigurationVersion)
			errApplyNodes[r.host] = ns.nodes.active[r.host]
		} else {
			if noop {
				logger.Printf("Nooped: %s@%s", r.report.Host, r.report.ConfigurationVersion)
			} else {
				logger.Printf("Applied: %s@%s", r.report.Host, r.report.ConfigurationVersion)
			}
			appliedNodes[r.report.Host] = ns.nodes.active[r.report.Host]
		}
	}
	unlockNodeSet("root", false, ns, logger)
	ns.nodes.active = mergeNodeMaps(appliedNodes, beyondRevNodes)
	ns.nodes.errored = mergeNodeMaps(ns.nodes.errored, errApplyNodes)
	return appliedNodes, beyondRevNodes, nil
}

func (hs *hecklerServer) HecklerNoopRange(ctx context.Context, req *hecklerpb.HecklerNoopRangeRequest) (*hecklerpb.HecklerNoopRangeReport, error) {
	var err error
	logger := log.New(os.Stdout, "[HecklerNoopRange] ", log.Lshortfile)
	ns, err := dialReqNodes(hs.conf, req.Nodes, req.NodeSet, logger)
	if err != nil {
		return nil, err
	}
	defer closeNodeSet(ns, logger)
	err = lastApplyNodeSet(ns, hs.repo, logger)
	if err != nil {
		return nil, err
	}
	commitLogIds, commits, err := commitLogIdList(hs.repo, req.BeginRev, req.EndRev)
	if err != nil {
		return nil, err
	}
	for _, node := range ns.nodes.active {
		node.commitReports = make(map[git.Oid]*rizzopb.PuppetReport)
		node.commitDeltaResources = make(map[git.Oid]map[ResourceTitle]*deltaResource)
	}
	rprt := new(hecklerpb.HecklerNoopRangeReport)
	var md string
	for _, gi := range commitLogIds {
		groupedReport, err := groupReportNodeSet(ns, commits[gi], true, hs.repo, hs.conf.IgnoredResources, hs.conf.NoopDir, hs.conf, logger)
		if err != nil {
			return nil, err
		}
		if req.OutputFormat == hecklerpb.OutputFormat_markdown {
			md, err = commitToMarkdown(hs.conf, commits[gi], groupedReport, hs.templates)
			if err != nil {
				return nil, err
			}
			rprt.Output += md
		}
	}
	rprt.NodeErrors = make(map[string]string)
	for host, node := range ns.nodes.errored {
		rprt.NodeErrors[host] = node.err.Error()
	}
	for host, node := range ns.nodes.lockedByAnother {
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

	if conf.GitHubHttpProxy != "" {
		proxyUrl, err := url.Parse(conf.GitHubHttpProxy)
		if err != nil {
			return nil, nil, err
		}
		tr.(*http.Transport).Proxy = http.ProxyURL(proxyUrl)
	}

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
		return nil, fmt.Errorf("Unable to search GitHub Issues: %w", err)
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
		return fmt.Sprintf("[%s_env] ", prefix)
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

func noopToMarkdown(conf *HecklerdConf, commit *git.Commit, gr groupedReport, templates *template.Template) (string, error) {
	var body string
	if gr.ParentNoopFailures {
		body += fmt.Sprintf("## WARNING: Unable to noop some parents of this commit!\n\n")
		body += fmt.Sprintf("Some noops failed, so their changes could not be subtracted from the changes specific to this commit.\n\n")
	}
	if gr.CommitNotInAllNodeLineages {
		body += fmt.Sprintf("## Notice: No noop produced for this commit!\n\n")
		body += fmt.Sprintf("This commit was not in the lineage of all nodes in the environment. ")
		body += fmt.Sprintf("Which means the commit is on a branch of the repository and some nodes ")
		body += fmt.Sprintf("are ahead on a separate branch, so we are only able to noop the merge ")
		body += fmt.Sprintf("commit of this branch.\n\n")
	}
	body += commitMsgToMarkdown(commit, conf, templates)
	body += groupedFailuresToMarkdown(gr.Failures, templates)
	body += erroredNodesToMarkdown(gr.CompressedErrored, templates)
	body += lockedNodesToMarkdown(gr.CompressedLockedByAnother, templates)
	body += beyondRevNodesToMarkdown(gr.CompressedBeyondRev, templates)
	body += groupedResourcesToMarkdown(gr.Resources, commit, conf, templates)
	if len(gr.Resources) > 0 {
		noopOwnersMarkdown, err := noopOwnersToMarkdown(conf, commit, gr.Resources, templates)
		if err != nil {
			return "", err
		}
		body += noopOwnersMarkdown
	}
	return body, nil
}

func githubCreateIssue(ghclient *github.Client, conf *HecklerdConf, commit *git.Commit, gr groupedReport, templates *template.Template) (*github.Issue, error) {
	authors, err := commitAuthorsLogins(ghclient, commit)
	if err != nil {
		return nil, err
	}

	body, err := noopToMarkdown(conf, commit, gr, templates)
	if err != nil {
		return nil, err
	}

	// GitHub has a max issue body size of 65536
	if len(body) >= 65536 {
		notice := fmt.Sprintf("## Noop Output Trimmed!\n\nOutput has been trimmed because it is too long for the GitHub issue\n\n")
		body = notice + body
		runeBody := []rune(body)
		// trim to less then the max, just to be sure it will fit
		trimmedRunes := runeBody[0:65000]
		body = string(trimmedRunes)
	}
	githubIssue := &github.IssueRequest{
		Title: github.String(noopTitle(commit, conf.EnvPrefix)),
		Body:  github.String(body),
	}
	if !conf.GitHubDisableNotifications {
		githubIssue.Assignees = &authors
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
	groupedResources, err = groupedResourcesNodeFiles(groupedResources, conf.WorkRepo)
	if err != nil {
		return "", err
	}
	groupedResources, err = groupedResourcesOwners(groupedResources, conf.WorkRepo, conf.ModulesPaths)
	if err != nil {
		return "", err
	}
	no := groupedResourcesUniqueOwners(groupedResources)
	if conf.GitHubDisableNotifications {
		stripAtSigns(&no)
	}
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

func commitToMarkdown(conf *HecklerdConf, commit *git.Commit, gr groupedReport, templates *template.Template) (string, error) {
	body, err := noopToMarkdown(conf, commit, gr, templates)
	if err != nil {
		return "", err
	}
	md := fmt.Sprintf("## %s\n\n", noopTitle(commit, conf.EnvPrefix))
	md += body
	return md, nil
}

func commitMsgToMarkdown(commit *git.Commit, conf *HecklerdConf, templates *template.Template) string {
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

func groupedFailuresToMarkdown(gf []*groupedFailure, templates *template.Template) string {
	var body strings.Builder
	var err error

	if len(gf) == 0 {
		return ""
	}

	data := struct {
		GroupedFailures []*groupedFailure
	}{
		gf,
	}
	err = templates.ExecuteTemplate(&body, "groupedFailure.tmpl", data)
	if err != nil {
		log.Fatal(err)
	}
	return body.String()
}

func erroredNodesToMarkdown(errored map[string]string, templates *template.Template) string {
	var body strings.Builder
	var err error

	if len(errored) == 0 {
		return ""
	}

	data := struct {
		Errored map[string]string
	}{
		errored,
	}
	err = templates.ExecuteTemplate(&body, "erroredNodes.tmpl", data)
	if err != nil {
		log.Fatal(err)
	}
	return body.String()
}

func lockedNodesToMarkdown(locked map[string]string, templates *template.Template) string {
	var body strings.Builder
	var err error

	if len(locked) == 0 {
		return ""
	}

	data := struct {
		Locked map[string]string
	}{
		locked,
	}
	err = templates.ExecuteTemplate(&body, "lockedNodes.tmpl", data)
	if err != nil {
		log.Fatal(err)
	}
	return body.String()
}

func beyondRevNodesToMarkdown(beyondRev map[string]string, templates *template.Template) string {
	var body strings.Builder
	var err error

	if len(beyondRev) == 0 {
		return ""
	}

	data := struct {
		BeyondRev map[string]string
	}{
		beyondRev,
	}
	err = templates.ExecuteTemplate(&body, "beyondRevNodes.tmpl", data)
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
	defer closeNodeSet(ns, logger)
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
	defer closeNodeSet(ns, logger)
	ns.nodes.locked = copyNodeMap(ns.nodes.dialed)
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
	for host, node := range ns.nodes.lockedByAnother {
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

func closeNodeSet(ns *NodeSet, logger *log.Logger) {
	logger.Printf("Closing connections for node set: '%s'", ns.name)
	closeNodes(ns.nodes.dialed)
}

func (hs *hecklerServer) HecklerLock(ctx context.Context, req *hecklerpb.HecklerLockRequest) (*hecklerpb.HecklerLockReport, error) {
	var err error
	logger := log.New(os.Stdout, "[HecklerLock] ", log.Lshortfile)
	ns, err := dialReqNodes(hs.conf, req.Nodes, req.NodeSet, logger)
	if err != nil {
		return nil, err
	}
	defer closeNodeSet(ns, logger)
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
	for host, node := range ns.nodes.lockedByAnother {
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
	ns.nodes.active = copyNodeMap(lockedNodes)
	ns.nodes.locked = lockedNodes
	ns.nodes.errored = mergeNodeMaps(ns.nodes.errored, errLockNodes)
	ns.nodes.lockedByAnother = lockedByAnotherNodes
	if ok := thresholdExceededNodeSet(ns, logger); ok {
		unlockNodeSet(user, false, ns, logger)
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
	_, unlockedNodes, lockedByAnotherNodes, errLockNodes := rizzoLockNodes(lockReq, ns.nodes.locked)
	if len(unlockedNodes) == len(ns.nodes.locked) {
		logger.Printf("Unlocked all %d requested nodes", len(unlockedNodes))
	} else {
		logger.Printf("Tried to unlock %d nodes, but only succeeded in unlocking, %d", len(ns.nodes.locked), len(unlockedNodes))
	}
	for host, str := range compressLockNodes(lockedByAnotherNodes) {
		logger.Printf("Unlock requested, but locked by another: %s, %s", host, str)
	}
	compressedErrNodes := compressErrorNodes(errLockNodes)
	for host, err := range compressedErrNodes {
		logger.Printf("Unlock failed, errNodes: %s, Error: %v", host, err)
	}
	ns.nodes.active = unlockedNodes
	ns.nodes.errored = mergeNodeMaps(ns.nodes.errored, errLockNodes)
	ns.nodes.locked = nil
	ns.nodes.lockedByAnother = mergeNodeMaps(ns.nodes.lockedByAnother, lockedByAnotherNodes)
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

func rizzoLockNode(req rizzopb.PuppetLockRequest, node *Node) {
	reportChan := make(chan rizzopb.PuppetLockReport)
	go rizzoLock(node.host, node.rizzoClient, req, reportChan)
	r := <-reportChan
	node.lockState = heckler.LockReportToLockState(r)
	if node.lockState.LockStatus == heckler.LockUnknown {
		node.err = errors.New(r.Error)
	}
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

func hecklerLastApply(node *Node, c chan<- applyResult, logger *log.Logger) {
	plar := rizzopb.PuppetLastApplyRequest{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	r, err := node.rizzoClient.PuppetLastApply(ctx, &plar)
	if err != nil {
		c <- applyResult{
			host:   node.host,
			report: rizzopb.PuppetReport{},
			err:    fmt.Errorf("Rizzo lastApply error from %s, returning any empty report: %w", node.host, err),
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

func lastApplyNodeSet(ns *NodeSet, repo *git.Repository, logger *log.Logger) error {
	var err error
	var applyStatus lastApplyStatus
	var oidPtr *git.Oid
	errNodes := make(map[string]*Node)
	lastApplyNodes := make(map[string]*Node)

	eligibleNodeSet("root", ns)
	if ok := thresholdExceededNodeSet(ns, logger); ok {
		return ErrThresholdExceeded
	}

	puppetReportChan := make(chan applyResult)
	for _, node := range ns.nodes.active {
		go hecklerLastApply(node, puppetReportChan, logger)
	}

	for range ns.nodes.active {
		r := <-puppetReportChan
		if _, ok := ns.nodes.active[r.host]; !ok {
			return fmt.Errorf("No Node struct found for report from: %s", r.host)
		}
		if r.err != nil {
			errNodes[r.host] = ns.nodes.active[r.host]
			errNodes[r.host].err = fmt.Errorf("Unable to obtain lastApply: %w", r.err)
			continue
		}
		applyStatus, oidPtr, err = parseLastApply(r.report.ConfigurationVersion, repo)
		switch applyStatus {
		case lastApplyClean:
			lastApplyNodes[r.host] = ns.nodes.active[r.host]
			lastApplyNodes[r.host].lastApply = *oidPtr
		case lastApplyDirty:
			errNodes[r.host] = ns.nodes.active[r.host]
			errNodes[r.host].err = fmt.Errorf("Node is dirty '%s-dirty'", oidPtr.String())
		case lastApplyErrored:
			errNodes[r.host] = ns.nodes.active[r.host]
			errNodes[r.host].err = fmt.Errorf("Unable to revparse ConfigurationVersion, %s: %w", r.report.ConfigurationVersion, err)
		default:
			log.Fatal("Unknown lastApplyStatus!")
		}
	}
	ns.nodes.active = lastApplyNodes
	ns.nodes.errored = mergeNodeMaps(ns.nodes.errored, errNodes)
	if ok := thresholdExceededNodeSet(ns, logger); ok {
		return ErrThresholdExceeded
	}
	return nil
}

func parseLastApply(confVer string, repo *git.Repository) (lastApplyStatus, *git.Oid, error) {
	var oidPtr *git.Oid
	var err error

	if confVer == "" {
		return lastApplyErrored, nil, ErrLastApplyUnknown
	}

	regexDirtyRev := regexp.MustCompile(`^([^-]*)-dirty$`)
	if regexDirtyRev.MatchString(confVer) {
		rev := regexDirtyRev.FindStringSubmatch(confVer)
		if len(rev) != 2 || rev[1] == "" {
			return lastApplyErrored, nil, ErrLastApplyUnknown
		}
		oidPtr, err = git.NewOid(rev[1])
		if err != nil {
			return lastApplyErrored, nil, ErrLastApplyUnknown
		}
		return lastApplyDirty, oidPtr, nil
	} else {
		oidPtr, err = git.NewOid(confVer)
		if err != nil {
			return lastApplyErrored, nil, ErrLastApplyUnknown
		}
		_, err = repo.LookupCommit(oidPtr)
		if err != nil {
			// We have an oid, but it is not in the repo, so either someone rebased
			// and changed the oid, or hasn't pushed yet, so we treat it as dirty.
			return lastApplyDirty, oidPtr, nil
		}
		return lastApplyClean, oidPtr, nil
	}
}

func dirtyNodeSet(ns *NodeSet, repo *git.Repository, logger *log.Logger) error {
	var err error
	var applyStatus lastApplyStatus
	var oidPtr *git.Oid

	errNodes := make(map[string]*Node)
	dirtyNodes := make(map[string]*Node)

	eligibleNodeSet("root", ns)
	if ok := thresholdExceededNodeSet(ns, logger); ok {
		return ErrThresholdExceeded
	}

	puppetReportChan := make(chan applyResult)
	for _, node := range ns.nodes.active {
		go hecklerLastApply(node, puppetReportChan, logger)
	}

	for range ns.nodes.active {
		r := <-puppetReportChan
		if _, ok := ns.nodes.active[r.host]; !ok {
			return fmt.Errorf("No Node struct found for report from: %s", r.host)
		}
		if r.err != nil {
			errNodes[r.host] = ns.nodes.active[r.host]
			errNodes[r.host].err = fmt.Errorf("Unable to obtain lastApply: %w", r.err)
			continue
		}
		applyStatus, oidPtr, err = parseLastApply(r.report.ConfigurationVersion, repo)
		switch applyStatus {
		case lastApplyClean:
			continue
		case lastApplyDirty:
			dirtyNodes[r.host] = ns.nodes.active[r.host]
			dirtyNodes[r.host].lastApply = *oidPtr
		case lastApplyErrored:
			errNodes[r.host] = ns.nodes.active[r.host]
			errNodes[r.host].err = fmt.Errorf("Unable to revparse ConfigurationVersion, %s: %w", r.report.ConfigurationVersion, err)
		default:
			log.Fatal("Unknown lastApplyStatus!")
		}
	}
	ns.nodes.active = dirtyNodes
	ns.nodes.errored = mergeNodeMaps(ns.nodes.errored, errNodes)
	if ok := thresholdExceededNodeSet(ns, logger); ok {
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

	cloneOptions := &git.CloneOptions{
		FetchOptions: &git.FetchOptions{
			UpdateFetchhead: true,
			DownloadTags:    git.DownloadTagsAll,
		},
		Bare: true,
	}
	if conf.GitHubHttpProxy != "" {
		cloneOptions.ProxyOptions = git.ProxyOptions{
			Type: git.ProxyTypeSpecified,
			Url:  conf.GitHubHttpProxy,
		}
	}
	remoteUrl := fmt.Sprintf("https://x-access-token:%s@%s/%s/%s", tok, conf.GitHubDomain, conf.RepoOwner, conf.Repo)
	bareRepo, err := gitutil.CloneOrOpen(remoteUrl, conf.ServedRepo, cloneOptions)
	if err != nil {
		return nil, err
	}
	err = gitutil.FastForward(bareRepo, cloneOptions.FetchOptions)
	if err != nil {
		return nil, err
	}
	return bareRepo, nil
}

func sleepAndLog(sleepDur, logDur time.Duration, msg string, logger *log.Logger) {
	sleepDurRem := sleepDur
	for i := 0; i < int(sleepDur/logDur); i++ {
		sleepDurRem = sleepDurRem - logDur
		logger.Printf("%s, sleeping for %v, %v remaining\n", msg, sleepDur, sleepDurRem)
		time.Sleep(logDur)
	}
	time.Sleep(sleepDurRem)
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
	var ns *NodeSet
	var perApply *NodeSet
	logger := log.New(os.Stdout, "[applyLoop] ", log.Lshortfile)
	applySetSleep := (time.Duration(conf.ApplySetSleepSeconds) * time.Second)
	loopSleep := (time.Duration(conf.LoopApplySleepSeconds) * time.Second)
	logger.Printf("Started, looping every %v", loopSleep)
	for {
		time.Sleep(loopSleep)
		ns = &NodeSet{
			name:           "all",
			nodeThresholds: conf.MaxNodeThresholds,
		}
		err = dialNodeSet(conf, ns, logger)
		if err != nil {
			logger.Printf("Error: unable to dial node set: %v", err)
			continue
		}
		err = commonTagNodeSet(conf, ns, repo, logger)
		if err != nil {
			logger.Printf("Error: unable to query for commonTag: %v", err)
			closeNodeSet(ns, logger)
			continue
		}
		logger.Printf("Found common tag: %s", ns.commonTag)
		priorTag := ns.commonTag
		nextTag, err := nextTag(priorTag, conf.EnvPrefix, repo)
		if err != nil {
			logger.Printf("Error: unable to query for nextTag after '%s', sleeping: %v", priorTag, err)
			closeNodeSet(ns, logger)
			continue
		}
		if nextTag == "" {
			logger.Printf("No nextTag found after tag '%s', sleeping", priorTag)
			closeNodeSet(ns, logger)
			continue
		}
		ghclient, _, err := githubConn(conf)
		if err != nil {
			logger.Printf("Error: unable to connect to GitHub, sleeping: %v", err)
			closeNodeSet(ns, logger)
			continue
		}
		nextTagMilestone, err := milestoneFromTag(nextTag, ghclient, conf)
		if err != nil {
			logger.Printf("Error: unable to query GitHub for milestone, sleeping: %v", err)
			closeNodeSet(ns, logger)
			continue
		}
		if nextTagMilestone == nil {
			logger.Printf("Milestone for next tag '%s', not created, sleeping", nextTag)
			closeNodeSet(ns, logger)
			continue
		}
		approved, err := tagApproved(repo, ghclient, conf, priorTag, nextTag, logger)
		if err != nil {
			logger.Printf("Error: unable to determine if tag has been approved, sleeping: %v", err)
			closeNodeSet(ns, logger)
			continue
		}
		if !approved {
			logger.Printf("Tag '%s' is not approved, sleeping", nextTag)
			closeNodeSet(ns, logger)
			continue
		}
		if nextTagMilestone.GetState() != "closed" {
			err = closeMilestone(nextTag, ghclient, conf)
			if err != nil {
				logger.Printf("Error: unable to close milestone, sleeping: %v", err)
				closeNodeSet(ns, logger)
				continue
			}
		}
		closeNodeSet(ns, logger)
		logger.Printf("Tag '%s' is ready to apply, applying with set order: %v", nextTag, conf.ApplySetOrder)
		for nodeSetIndex, nodeSetName := range conf.ApplySetOrder {
			logger.Printf("Applying Set '%s' (%d of %d sets)", nodeSetName, nodeSetIndex+1, len(conf.ApplySetOrder))
			perApply = &NodeSet{
				name:           nodeSetName,
				nodeThresholds: conf.MaxNodeThresholds,
			}
			err = dialNodeSet(conf, perApply, logger)
			if err != nil {
				logger.Printf("Error: unable to dial node set: %v", err)
				break
			}
			appliedNodes, beyondRevNodes, err := applyNodeSet(perApply, false, false, nextTag, repo, conf.LockMessage, logger)
			if err != nil {
				logger.Printf("Error: unable to apply nodes, sleeping: %v", err)
				closeNodeSet(perApply, logger)
				break
			}
			closeNodeSet(perApply, logger)
			compressedErrNodes := compressErrorNodes(perApply.nodes.errored)
			for host, err := range compressedErrNodes {
				logger.Printf("errNodes: %s, Error: %v", host, err)
			}
			logger.Printf("Applied Set '%s': (%d); Beyond rev nodes: (%d); Error nodes: (%d)", nodeSetName, len(appliedNodes), len(beyondRevNodes), len(perApply.nodes.errored))
			if (nodeSetIndex + 1) < len(conf.ApplySetOrder) {
				if applySetSleep > 0 {
					logger.Printf("Sleeping for %v, before applying next set '%s'...", applySetSleep, conf.ApplySetOrder[nodeSetIndex+1])
					sleepAndLog(applySetSleep, time.Duration(10)*time.Second, "Waiting to apply next node set", logger)
				}
			}
		}
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
func approvalLoop(conf *HecklerdConf, repo *git.Repository) {
	var err error
	var ns *NodeSet
	logger := log.New(os.Stdout, "[approvalLoop] ", log.Lshortfile)
	loopSleep := time.Duration(conf.LoopApprovalSleepSeconds) * time.Second
	logger.Printf("Started, looping every %v", loopSleep)
	for {
		time.Sleep(loopSleep)
		ns = &NodeSet{
			name:           "all",
			nodeThresholds: conf.MaxNodeThresholds,
		}
		err = dialNodeSet(conf, ns, logger)
		if err != nil {
			logger.Printf("Error: unable to dial node set: %v", err)
			continue
		}
		err = commonTagNodeSet(conf, ns, repo, logger)
		if err != nil {
			logger.Printf("Error: unable to query for commonTag: %v", err)
			closeNodeSet(ns, logger)
			continue
		}
		logger.Printf("Found common tag: %s", ns.commonTag)
		closeNodeSet(ns, logger)
		_, commits, err := commitLogIdList(repo, ns.commonTag, conf.RepoBranch)
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
				logger.Printf("Error: unable to determine if issue for commit %s exists: %v", gi.String(), err)
				continue
			}
			if issue == nil {
				continue
			}
			gr, err := unmarshalGroupedReport(commit.Id(), conf.GroupedNoopDir)
			if os.IsNotExist(err) {
				continue
			} else if err != nil {
				logger.Fatalf("Error: unable to unmarshal groupedCommit: %v", err)
			}
			if issue.GetState() == "closed" {
				continue
			}
			if len(gr.Resources) == 0 {
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
			noopApproved, err := noopApproved(ghclient, conf, gr.Resources, commit, issue)
			if err != nil {
				logger.Printf("Error: unable to determine if issue(%d) is approved: %v", issue.Number, err)
				continue
			}
			switch noopApproved {
			case notApproved:
				continue
			case codeownersApproved, adminApproved:
				err := closeIssue(ghclient, conf, issue, approvedComment(noopApproved, gr.Resources, logger))
				if err != nil {
					logger.Printf("Error: unable to close approved issue(%d): %v", issue.GetNumber(), err)
					continue
				}
			}
		}
		logger.Println("Noop approval complete, sleeping")
	}
}

// Given a commit, the associated github issue, and the groupedResources check
// if each grouped resource has been approved by a valid approver, exclude
// authors of the commit in the approvers set.
func noopApproved(ghclient *github.Client, conf *HecklerdConf, groupedResources []*groupedResource, commit *git.Commit, issue *github.Issue) (noopApproverType, error) {
	var err error
	groupedResources, err = groupedResourcesNodeFiles(groupedResources, conf.WorkRepo)
	if err != nil {
		return notApproved, err
	}
	groupedResources, err = groupedResourcesOwners(groupedResources, conf.WorkRepo, conf.ModulesPaths)
	if err != nil {
		return notApproved, err
	}
	groups, err := githubGroupsForGroupedResources(ghclient, groupedResources)
	if err != nil {
		return notApproved, err
	}
	noopApprovers, err := githubNoopApprovals(ghclient, conf, issue)
	if err != nil {
		return notApproved, err
	}
	commitAuthors, err := commitAuthorsLogins(ghclient, commit)
	if err != nil {
		return notApproved, err
	}
	// Remove commit authors if they approved the noop, since we do not want
	// authors approving their own noops.
	// TODO make this configurable
	approvers := setDifferenceStrSlice(noopApprovers, commitAuthors)
	// TODO Use the populated groupedResources to provide helpful debug messages
	// on what a noop still needs for approval, need to determine the best way to
	// present the information.
	codeownersHaveApproved := resourcesApproved(groupedResources, groups, approvers)
	adminHasApproved := adminOwnerApproved(groupedResources, conf.AdminOwners, noopApprovers)
	if codeownersHaveApproved {
		return codeownersApproved, nil
	} else if adminHasApproved {
		return adminApproved, nil
	} else {
		return notApproved, nil
	}
}

func adminOwnerApproved(groupedResources []*groupedResource, adminOwnersList []string, approversList []string) bool {
	validApprovers := setIntersectionStrSlice(adminOwnersList, approversList)
	if len(validApprovers) > 0 {
		for _, gr := range groupedResources {
			gr.AdminApprovals = validApprovers
		}
		return true
	} else {
		return false
	}
}

func approvedComment(noopApproved noopApproverType, groupedResources []*groupedResource, logger *log.Logger) string {
	msg := "Issue has been approved, marking issue as 'closed'\n\n"
	switch noopApproved {
	case codeownersApproved:
		msg += "CODEOWNERS:\n"
		for _, gr := range groupedResources {
			if len(gr.Approvals.File) > 0 {
				msg += "- [" + string(gr.Title) + "]: " + strings.Join(gr.Approvals.File, ",") + "\n"
			} else {
				msg += "- [" + string(gr.Title) + "]:\n"
				for nf, nfa := range gr.Approvals.NodeFiles {
					msg += "  - " + nf + ": " + strings.Join(nfa, ",") + "\n"
				}
			}
		}
	case adminApproved:
		if len(groupedResources) > 0 && len(groupedResources[0].AdminApprovals) > 0 {
			msg += "Admins: " + strings.Join(groupedResources[0].AdminApprovals, ",") + "\n"
		} else {
			log.Fatal("Unexpected groupedResources!")
		}
	default:
		log.Fatal("Unexpected noopApproverType!")
	}
	return msg
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

// Given two string slice sets return the intersection of a and b
func setIntersectionStrSlice(a []string, b []string) []string {
	c := make([]string, 0)
	mapB := make(map[string]bool)
	for _, i := range b {
		mapB[i] = true
	}
	for _, i := range a {
		if _, ok := mapB[i]; ok {
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
func groupedResourcesOwners(groupedResources []*groupedResource, puppetCodePath string, modulesPaths []string) ([]*groupedResource, error) {
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
		for _, module := range containmentPathModules(gr.ContainmentPath) {
			for _, modulesPath := range modulesPaths {
				modulePath := fmt.Sprintf("%s/%s", modulesPath, module)
				fullPath := fmt.Sprintf("%s/%s", puppetCodePath, modulePath)
				if _, err := os.Stat(fullPath); err == nil {
					gr.Owners.Module = append(gr.Owners.Module, co.Owners(modulePath)...)
				}
			}
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

func containmentPathModules(containmentPath []string) []string {
	var modules []string
	if len(containmentPath) == 0 {
		return nil
	}
	regexPuppetStage := regexp.MustCompile(`^Stage\[`)
	for i, path := range containmentPath {
		if i == len(containmentPath)-1 {
			// Skip the last element which is the resource itself
			continue
		}
		if regexPuppetStage.MatchString(path) {
			// Skip Puppet Stages
			continue
		}
		// TODO: allow ownership of nested modules
		// Grab only the name of the first module
		baseModule := strings.Split(path, "::")[0]
		modules = append(modules, strings.ToLower(baseModule))
	}
	return modules
}

// Strips the `@` prefix from users and groups so that they are not notified on
// GitHub
func stripAtSigns(no *noopOwners) {
	for _, owners := range no.OwnedSourceFiles {
		for i, owner := range owners {
			owners[i] = strings.TrimPrefix(owner, "@")
		}
	}
	for _, owners := range no.OwnedNodeFiles {
		for i, owner := range owners {
			owners[i] = strings.TrimPrefix(owner, "@")
		}
	}
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
func resourcesApproved(groupedResources []*groupedResource, groups map[string][]string, approvers []string) bool {
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
		if len(gr.Owners.Module) > 0 {
			gr.Approvals.Module = intersectionOwnersApprovers(gr.Owners.Module, approvers, groups)
			if len(gr.Approvals.Module) > 0 {
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
		return true
	} else {
		return false
	}
}

// Given the owners of a resource, the mapping of group name to members, and
// the approvers of the resource; return the intersection of the owners and
// approvers, i.e. the valid approvers.
func intersectionOwnersApprovers(owners []string, approvers []string, groups map[string][]string) []string {
	expandedOwners := make([]string, 0)
	for _, owner := range owners {
		if _, ok := groups[owner]; ok {
			for _, user := range groups[owner] {
				expandedOwners = append(expandedOwners, user)
			}
		} else {
			expandedOwners = append(expandedOwners, owner)
		}
	}
	return setIntersectionStrSlice(expandedOwners, approvers)
}

func unlockAll(conf *HecklerdConf, logger *log.Logger) error {
	var err error
	ns := &NodeSet{
		name: "all",
		// Disable thresholds when unlocking all
		nodeThresholds: NodeThresholds{
			Errored:         -1,
			LockedByAnother: -1,
		},
	}
	err = dialNodeSet(conf, ns, logger)
	if err != nil {
		return err
	}
	defer closeNodeSet(ns, logger)
	ns.nodes.locked = copyNodeMap(ns.nodes.dialed)
	unlockNodeSet("root", false, ns, logger)
	unlockedHosts := make([]string, 0)
	for host, _ := range ns.nodes.active {
		unlockedHosts = append(unlockedHosts, host)
	}
	if len(unlockedHosts) > 0 {
		logger.Printf("Unlocked: %s", compressHosts(unlockedHosts))
	}
	compressedErrNodes := compressErrorNodes(ns.nodes.errored)
	for host, err := range compressedErrNodes {
		logger.Printf("Unlock errNodes: %s, Error: %v", host, err)
	}
	return nil
}

// Does our common tag across "all" nodes equal our latest tag?
//   no, do nothing, the latest tag has not been applied, yet
//   yes,
//     Are there new commits beyond are common tag?
//       yes, create a new version
//       no, do nothing
func autoTag(conf *HecklerdConf, repo *git.Repository) {
	logger := log.New(os.Stdout, "[autoTag] ", log.Lshortfile)
	var err error
	var ns *NodeSet
	ns = &NodeSet{
		name:           "all",
		nodeThresholds: conf.MaxNodeThresholds,
	}
	err = dialNodeSet(conf, ns, logger)
	if err != nil {
		logger.Printf("Unable to dial node set: %v", err)
		return
	}
	err = commonTagNodeSet(conf, ns, repo, logger)
	if err != nil {
		logger.Printf("Unable to query for commonTag: %v", err)
		closeNodeSet(ns, logger)
		return
	}
	closeNodeSet(ns, logger)
	logger.Printf("Found common tag: %s", ns.commonTag)
	semverCommonTag, err := tagToSemver(ns.commonTag, conf.EnvPrefix)
	if err != nil {
		logger.Printf("Unable to convert to semver tag: %v", err)
		return
	}
	tags, err := repoTags(conf.EnvPrefix, repo)
	if err != nil {
		logger.Printf("Unable to query for repoTags: %v", err)
		return
	}
	tagSet, err := sortedSemVers(tags, conf.EnvPrefix)
	if err != nil {
		logger.Printf("Unable to sort tags: %v", err)
		return
	}
	latestTag := tagSet[len(tagSet)-1]
	if !semverCommonTag.Equal(latestTag) {
		logger.Printf("Latest tag '%s' not applied, common tag is '%s'", semverToOrig(latestTag, conf.EnvPrefix), semverToOrig(semverCommonTag, conf.EnvPrefix))
		return
	}
	commitLogIds, _, err := commitLogIdList(repo, semverToOrig(latestTag, conf.EnvPrefix), conf.RepoBranch)
	if err != nil {
		logger.Fatalf("Unable to obtain commit log ids: %v", err)
	}
	if len(commitLogIds) == 0 {
		logger.Printf("No new commits since latest tag: '%s'", semverToOrig(latestTag, conf.EnvPrefix))
		return
	}
	newVersion := latestTag.IncMajor()
	newTag := fmt.Sprintf("%sv%d", tagPrefix(conf.EnvPrefix), newVersion.Major())
	ghclient, _, err := githubConn(conf)
	if err != nil {
		logger.Printf("Error: unable to connect to GitHub, returning: %v", err)
		return
	}
	// Check if tag ref already exists on GitHub
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	ref, resp, err := ghclient.Git.GetRef(ctx, conf.RepoOwner, conf.Repo, fmt.Sprintf("refs/tags/%s", newTag))
	if ref == nil && resp.StatusCode == 404 {
		err = createTag(newTag, conf, ghclient, repo)
		if err != nil {
			logger.Printf("Error: unable to create new tag '%s' on GitHub, returning: %v", newTag, err)
			return
		}
		logger.Printf("Created new tag '%s' on GitHub", newTag)
		return
	} else if err != nil {
		logger.Printf("Error: unable to get ref for tag '%s' on GitHub, returning: %v", newTag, err)
		return
	} else {
		logger.Printf("New tag ref already exists on GitHub, skipping creation, '%s'", newTag)
		return
	}
}

func createTag(newTag string, conf *HecklerdConf, ghclient *github.Client, repo *git.Repository) error {
	timeNow := time.Now()
	tagger := &github.CommitAuthor{
		Date:  &timeNow,
		Name:  github.String("Heckler"),
		Email: github.String(conf.GitHubAppEmail),
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
func tagApproved(repo *git.Repository, ghclient *github.Client, conf *HecklerdConf, priorTag string, nextTag string, logger *log.Logger) (bool, error) {
	_, commits, err := commitLogIdList(repo, priorTag, nextTag)
	if err != nil {
		return false, err
	}
	if len(commits) == 0 {
		return false, fmt.Errorf("No commits between versions: %s..%s", priorTag, nextTag)
	}
	approved := make(map[git.Oid]string)
	unapproved := make(map[git.Oid]string)
	for gi, commit := range commits {
		issue, err := githubIssueFromCommit(ghclient, gi, conf)
		if err != nil {
			return false, err
		}
		if issue == nil {
			unapproved[gi] = "Unapproved, GitHub issue does not exist, yet"
			continue
		}
		if issue.GetMilestone() == nil || *issue.GetMilestone().Title != nextTag {
			unapproved[gi] = fmt.Sprintf("Unapproved, GitHub issue not assigned to milestone '%s', yet", nextTag)
			continue
		}
		gr, err := unmarshalGroupedReport(commit.Id(), conf.GroupedNoopDir)
		if os.IsNotExist(err) {
			unapproved[gi] = "Unapproved, Noop not found on disk, yet"
			continue
		} else if err != nil {
			return false, fmt.Errorf("Unable to unmarshal groupedCommit: %w", err)
		}
		if len(gr.Resources) == 0 {
			approved[gi] = "Approved, no changes in noop"
			continue
		}
		if conf.AutoCloseIssues {
			approved[gi] = "Approved, AutoClose enabled"
			continue
		}
		noopApproved, err := noopApproved(ghclient, conf, gr.Resources, commit, issue)
		if err != nil {
			return false, fmt.Errorf("Unable to determine if issue(%d) is approved: %w", issue.Number, err)
		}
		switch noopApproved {
		case notApproved:
			unapproved[gi] = "Unapproved, noop issue awaiting approval from CODEOWNERS"
		case codeownersApproved:
			approved[gi] = "Approved, noop issue approved by CODEOWNERS"
		case adminApproved:
			approved[gi] = "Approved, noop issue approved by admin"
		default:
			log.Fatal("Unknown noopApproverType!")
		}
	}
	logger.Printf("Tag range %s..%s has %d commits, approved(%d), unapproved(%d)", priorTag, nextTag, len(commits), len(approved), len(unapproved))
	for gi, reason := range unapproved {
		logger.Printf("Commit: %s - '%s'", gi.String(), reason)
	}
	for gi, reason := range approved {
		logger.Printf("Commit: %s - '%s'", gi.String(), reason)
	}
	if len(commits) == len(approved) {
		return true, nil
	} else {
		return false, nil
	}
}

// Given a node set and a user this function returns the combined set of
// unlocked nodes and nodes locked by the provided user as the active set.
// These combined nodes are eligible to be queried by us for their status,
// since they are not locked by another user, or returning an error.
func eligibleNodeSet(user string, ns *NodeSet) {
	lockedNodes, unlockedNodes, lockedByAnotherNodes, errLockNodes := nodesLockState(user, ns.nodes.active)
	ns.nodes.active = mergeNodeMaps(lockedNodes, unlockedNodes)
	ns.nodes.errored = mergeNodeMaps(ns.nodes.errored, errLockNodes)
	ns.nodes.lockedByAnother = lockedByAnotherNodes
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
	var ns *NodeSet
	logger := log.New(os.Stdout, "[milestoneLoop] ", log.Lshortfile)
	loopSleep := time.Duration(conf.LoopMilestoneSleepSeconds) * time.Second
	logger.Printf("Started, looping every %v", loopSleep)
	for {
		time.Sleep(loopSleep)
		ns = &NodeSet{
			name:           "all",
			nodeThresholds: conf.MaxNodeThresholds,
		}
		err = dialNodeSet(conf, ns, logger)
		if err != nil {
			logger.Printf("Unable to dial node set: %v", err)
			continue
		}
		err = commonTagNodeSet(conf, ns, repo, logger)
		if err != nil {
			logger.Printf("Unable to query for commonTag: %v", err)
			closeNodeSet(ns, logger)
			continue
		}
		logger.Printf("Found common tag: %s", ns.commonTag)
		priorTag := ns.commonTag
		nextTag, err := nextTag(priorTag, conf.EnvPrefix, repo)
		if err != nil {
			logger.Printf("Error: unable to query for nextTag after '%s', sleeping: %v", priorTag, err)
			closeNodeSet(ns, logger)
			continue
		}
		if nextTag == "" {
			logger.Printf("No nextTag found after tag '%s', sleeping", priorTag)
			closeNodeSet(ns, logger)
			continue
		}
		closeNodeSet(ns, logger)
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
				logger.Printf("Error: unable to determine if issue for commit %s exists: %v", gi.String(), err)
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
// whether the first threshold exceeded the second. If a threshold is less than
// zero than it is disabled or infinite.
func thresholdExceededNodeSet(ns *NodeSet, logger *log.Logger) bool {
	if ns.nodeThresholds.Errored > -1 && len(ns.nodes.errored) > ns.nodeThresholds.Errored {
		logger.Printf("Error nodes(%d) exceeds the threshold(%d)", len(ns.nodes.errored), ns.nodeThresholds.Errored)
		for host, err := range compressErrorNodes(ns.nodes.errored) {
			logger.Printf("errNodes: %s, Error: %v", host, err)
		}
		return true
	} else if ns.nodeThresholds.LockedByAnother > -1 && len(ns.nodes.lockedByAnother) > ns.nodeThresholds.LockedByAnother {
		logger.Printf("Locked by another nodes(%d) exceeds the threshold(%d)", len(ns.nodes.lockedByAnother), ns.nodeThresholds.LockedByAnother)
		for host, str := range compressLockNodes(ns.nodes.lockedByAnother) {
			logger.Printf("Locked by another: %s, %s", host, str)
		}
		return true
	}
	return false
}

func dialNodeSet(conf *HecklerdConf, ns *NodeSet, logger *log.Logger) error {
	nodesToDial, err := setNameToNodes(conf, ns.name, logger)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	ns.nodes.dialed, ns.nodes.errored = dialNodes(ctx, nodesToDial)
	ns.nodes.active = copyNodeMap(ns.nodes.dialed)
	if ok := thresholdExceededNodeSet(ns, logger); ok {
		closeNodes(ns.nodes.dialed)
		return ErrThresholdExceeded
	}
	return nil
}

// Return the most recent tag across all nodes in an environment
func commonTagNodeSet(conf *HecklerdConf, ns *NodeSet, repo *git.Repository, logger *log.Logger) error {
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

// Users often apply a dirty branch and commit there changes after applying.
// This unfortunately leaves nodes in a dirty state which are actually clean.
// Attempt to clean up dirty nodes by nooping them with their own dirty commit
// as well as children of their dirty commit which hopefully will include the
// applied changes.
//
//  Are there any nodes dirty?
//    If No, do nothing
//    If Yes,
//      For each dirty node:
//        - Grab threshold commits after dirty commit
//        - Noop each un-nooped commit, from earliest to latest
//          Does the commit noop clean?
//            If No, mark failure
//            If Yes, apply commit and stop nooping node
func dirtyLoop(conf *HecklerdConf, repo *git.Repository) {
	var err error
	var ns *NodeSet
	logger := log.New(os.Stdout, "[dirtyLoop] ", log.Lshortfile)
	loopSleep := time.Duration(conf.LoopDirtySleepSeconds) * time.Second
	logger.Printf("Started, looping every %v", loopSleep)
	cleanChan := make(chan cleanNodeResult)
	nodeDirtyNoops := make(map[string]dirtyNoops)
	for {
		time.Sleep(loopSleep)
		ns = &NodeSet{
			name:           "all",
			nodeThresholds: conf.MaxNodeThresholds,
		}
		err = dialNodeSet(conf, ns, logger)
		if err != nil {
			logger.Printf("Unable to dial node set: %v", err)
			continue
		}
		err := dirtyNodeSet(ns, repo, logger)
		if err != nil {
			logger.Printf("Unable to obtain dirty node set: %v", err)
			closeNodeSet(ns, logger)
			continue
		}
		if len(ns.nodes.active) > 0 {
			logger.Printf("Dirty Nodes: %s\n", compressNodesMap(ns.nodes.active))
		}
		for host, node := range ns.nodes.active {
			if dn, ok := nodeDirtyNoops[host]; !ok || dn.rev != node.lastApply {
				nodeDirtyNoops[host] = dirtyNoops{
					rev:       node.lastApply,
					commitIds: make(map[git.Oid]bool),
				}
			}
			go cleanNode(node, nodeDirtyNoops[host], cleanChan, repo, conf, logger)
		}
		for range ns.nodes.active {
			cr := <-cleanChan
			if cr.err != nil {
				logger.Printf("Clean failed for %s: %v", cr.host, cr.err)
			} else {
				nodeDirtyNoops[cr.host] = cr.dn
				if cr.clean {
					logger.Printf("Cleaned %s", cr.host)
				} else {
					logger.Printf("Unable to clean %s, no diffless noops found", cr.host)
				}
			}
		}
		closeNodeSet(ns, logger)
	}
}

// Noop a dirty node with its dirty commit and also childThreshold number of
// child commits. If a diffless noop is found, apply the commit, which marks
// the node as clean.
func cleanNode(node *Node, dn dirtyNoops, c chan<- cleanNodeResult, repo *git.Repository, conf *HecklerdConf, logger *log.Logger) {
	// Threshold for the number of child commits from the dirty commit to consider
	childThreshold := 10
	headCommit, err := gitutil.RevparseToCommit(conf.RepoBranch, repo)
	if err != nil {
		c <- cleanNodeResult{
			host:  node.host,
			clean: false,
			err:   err,
		}
		return
	}
	var beginRev string
	var commitList []git.Oid
	if commitInNodeLineage(*headCommit.Id(), node, repo, logger) {
		beginRev = node.lastApply.String()
		commitList = []git.Oid{node.lastApply}
	} else {
		logger.Printf("Commit %s@%s is not in the lineage of branch: %s", node.lastApply.String(), node.host, conf.RepoBranch)
		beginRev = fmt.Sprintf("%s~%d", conf.RepoBranch, childThreshold)
		commitList = make([]git.Oid, 0)
	}
	newCommitList, _, err := commitLogIdList(repo, beginRev, conf.RepoBranch)
	if err != nil {
		c <- cleanNodeResult{
			host:  node.host,
			clean: false,
			err:   err,
		}
		return
	}
	commitList = append(commitList, newCommitList...)
	var commitsToConsider []git.Oid
	if len(commitList) >= childThreshold {
		commitsToConsider = commitList[:childThreshold]
	} else {
		commitsToConsider = commitList
	}
	commitsToNoop := make([]git.Oid, 0)
	for _, id := range commitsToConsider {
		if _, ok := dn.commitIds[id]; !ok {
			commitsToNoop = append(commitsToNoop, id)
		}
	}
	if len(commitsToNoop) == 0 {
		c <- cleanNodeResult{
			host:  node.host,
			clean: false,
			dn:    dn,
		}
		return
	}
	rizzoLockNode(
		rizzopb.PuppetLockRequest{
			Type:    rizzopb.LockReqType_lock,
			User:    "root",
			Comment: conf.LockMessage,
			Force:   false,
		}, node)
	if node.lockState.LockStatus != heckler.LockedByUser {
		c <- cleanNodeResult{
			host:  node.host,
			clean: false,
			err:   errors.New("Unable to lock node"),
		}
		return
	}
	applyResults := make(chan applyResult)
	clean := false
	for _, id := range commitsToNoop {
		logger.Printf("Nooping commit: %s@%s", node.host, id.String())
		par := rizzopb.PuppetApplyRequest{Rev: id.String(), Noop: true}
		go hecklerApply(node, applyResults, par)
		r := <-applyResults
		if r.err != nil {
			logger.Printf("Noop of %s@%s failed: %v", node.host, id.String(), r.err)
			continue
		}
		newRprt := normalizeReport(r.report, logger)
		if newRprt.Status == "failed" {
			newRprt.Host = r.host
		}
		dn.commitIds[id] = true
		logger.Printf("Received noop: %s@%s", newRprt.Host, newRprt.ConfigurationVersion)
		// if newRprt.ResourceStatuses is length zero than the noop reported no
		// changes were needed, i.e. the node is clean
		if len(newRprt.ResourceStatuses) == 0 {
			logger.Printf("Applying %s@%s", node.host, id.String())
			par := rizzopb.PuppetApplyRequest{Rev: id.String(), Noop: false}
			go hecklerApply(node, applyResults, par)
			r := <-applyResults
			if r.err != nil {
				logger.Printf("Apply failed: %s@%s, %v", node.host, id.String(), r.err)
			} else if r.report.Status == "failed" {
				logger.Printf("Apply status: '%s', %s@%s", r.report.Status, node.host, r.report.ConfigurationVersion)
			} else {
				logger.Printf("Applied: %s@%s", r.report.Host, r.report.ConfigurationVersion)
				clean = true
				break
			}
		}
	}
	rizzoLockNode(
		rizzopb.PuppetLockRequest{
			Type:  rizzopb.LockReqType_unlock,
			User:  "root",
			Force: false,
		}, node)
	if node.lockState.LockStatus != heckler.Unlocked {
		logger.Printf("Unlock of %s failed", node.host)
	}
	c <- cleanNodeResult{
		host:  node.host,
		clean: clean,
		dn:    dn,
	}
	return
}

//  Are there newer commits than our common last applied tag across the "all"
//  node set?
//    If No, do nothing
//    If Yes,
//      - noop each commit or load serialized copy
//      - create github issue, if it does not exist
func noopLoop(conf *HecklerdConf, repo *git.Repository, templates *template.Template) {
	var err error
	var ns *NodeSet
	logger := log.New(os.Stdout, "[noopLoop] ", log.Lshortfile)
	loopSleep := time.Duration(conf.LoopNoopSleepSeconds) * time.Second
	logger.Printf("Started, looping every %v", loopSleep)
	for {
		time.Sleep(loopSleep)
		ns = &NodeSet{
			name:           "all",
			nodeThresholds: conf.MaxNodeThresholds,
		}
		err = dialNodeSet(conf, ns, logger)
		if err != nil {
			logger.Printf("Unable to dial node set: %v", err)
			continue
		}
		err = commonTagNodeSet(conf, ns, repo, logger)
		if err != nil {
			logger.Printf("Unable to query for commonTag: %v", err)
			closeNodeSet(ns, logger)
			continue
		}
		logger.Printf("Found common tag: %s", ns.commonTag)
		_, commits, err := commitLogIdList(repo, ns.commonTag, conf.RepoBranch)
		if err != nil {
			logger.Fatalf("Unable to obtain commit log ids: %v", err)
		}
		if len(commits) == 0 {
			logger.Println("No new commits, sleeping")
			closeNodeSet(ns, logger)
			continue
		}
		closeNodeSet(ns, logger)
		var gr groupedReport
		var perNoop *NodeSet
		for gi, commit := range commits {
			gr, err = unmarshalGroupedReport(commit.Id(), conf.GroupedNoopDir)
			if os.IsNotExist(err) {
				perNoop = &NodeSet{
					name:           "all",
					nodeThresholds: conf.MaxNodeThresholds,
				}
				err = dialNodeSet(conf, perNoop, logger)
				if err != nil {
					logger.Printf("Unable to dial node set: %v", err)
					break
				}
				err = lastApplyNodeSet(perNoop, repo, logger)
				if err != nil {
					logger.Printf("Unable to get last apply for node set: %v", err)
					closeNodeSet(perNoop, logger)
					break
				}
				for _, node := range perNoop.nodes.active {
					node.commitReports = make(map[git.Oid]*rizzopb.PuppetReport)
					node.commitDeltaResources = make(map[git.Oid]map[ResourceTitle]*deltaResource)
				}
				gr, err = groupReportNodeSet(perNoop, commit, true, repo, conf.IgnoredResources, conf.NoopDir, conf, logger)
				if err != nil {
					logger.Printf("Unable to noop commit: %s, sleeping, %v", gi.String(), err)
					closeNodeSet(perNoop, logger)
					continue
				}
				closeNodeSet(perNoop, logger)
				err = marshalGroupedReport(commit.Id(), gr, conf.GroupedNoopDir)
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
				issue, err = githubCreateIssue(ghclient, conf, commit, gr, templates)
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
	for _, node := range nodes {
		if !commitInNodeLineage(commit, node, repo, logger) {
			return false
		}
	}
	return true
}

func commitInNodeLineage(commit git.Oid, node *Node, repo *git.Repository, logger *log.Logger) bool {
	var err error

	if node.lastApply.IsZero() {
		logger.Fatalf("lastApply for node, '%s', is zero", node.host)
	}
	// Check if oid is in the repo at all
	_, err = repo.LookupCommit(&node.lastApply)
	if err != nil {
		return false
	}
	if node.lastApply.Equal(&commit) {
		return true
	}
	descendant, err := repo.DescendantOf(&node.lastApply, &commit)
	if err != nil {
		logger.Fatalf("Cannot determine descendant status: %v", err)
	}
	if descendant {
		return true
	}
	descendant, err = repo.DescendantOf(&commit, &node.lastApply)
	if err != nil {
		logger.Fatalf("Cannot determine descendant status: %v", err)
	}
	if descendant {
		return true
	}
	return false
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
			return "", fmt.Errorf("Unable to describe %s@%s with tag prefix '%s', %w", node.host, node.lastApply.String(), prefix, err)
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

func repoTags(prefix string, repo *git.Repository) ([]string, error) {
	tagMatch := fmt.Sprintf("%sv*", tagPrefix(prefix))
	tags, err := repo.Tags.ListWithMatch(tagMatch)
	if err != nil {
		return nil, err
	}
	annotatedTags := make([]string, 0)
	for _, tag := range tags {
		obj, err := repo.RevparseSingle(tag)
		if err != nil {
			return nil, err
		}
		if obj.Type() == git.ObjectTag {
			annotatedTags = append(annotatedTags, tag)
		}
	}
	return tags, nil
}

func nextSemVerTag(priorTag string, prefix string, tags []string) (string, error) {
	tagSet, err := sortedSemVers(tags, prefix)
	if err != nil {
		return "", err
	}

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

func nextTag(priorTag string, prefix string, repo *git.Repository) (string, error) {
	tags, err := repoTags(prefix, repo)
	if err != nil {
		return "", err
	}
	nextTag, err := nextSemVerTag(priorTag, prefix, tags)
	if err != nil {
		return "", err
	}
	return nextTag, nil
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
	// Set some defaults
	conf.StateDir = "/var/lib/hecklerd"
	conf.WorkRepo = conf.StateDir + "/work_repo/puppetcode"
	conf.ServedRepo = conf.StateDir + "/served_repo/puppetcode"
	conf.NoopDir = conf.StateDir + "/noops"
	conf.GroupedNoopDir = conf.NoopDir + "/grouped"
	conf.LoopNoopSleepSeconds = 10
	conf.LoopMilestoneSleepSeconds = 10
	conf.LoopApplySleepSeconds = 10
	conf.LoopApprovalSleepSeconds = 10
	conf.LoopDirtySleepSeconds = 10
	conf.ApplySetOrder = []string{"all"}
	conf.ModulesPaths = []string{"modules", "vendor/modules"}
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
		logger.Printf("Removing state directory: %v", conf.StateDir)
		os.RemoveAll(conf.StateDir)
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
	os.MkdirAll(conf.NoopDir, 0755)
	os.MkdirAll(conf.GroupedNoopDir, 0755)
	repo, err := fetchRepo(conf)
	if err != nil {
		logger.Fatalf("Unable to fetch repo to serve: %v", err)
	}

	gitServer := &gitcgiserver.GitCGIServer{}
	gitServer.ExportAll = true
	gitServer.ProjectRoot = conf.StateDir + "/served_repo"
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
	_, err = gitutil.Pull("http://localhost:8080/puppetcode", conf.WorkRepo)
	if err != nil {
		logger.Fatalf("Unable to fetch repo: %v", err)
	}
	go func() {
		logger := log.New(os.Stdout, "[GitFetchWork] ", log.Lshortfile)
		for {
			_, err := gitutil.Pull("http://localhost:8080/puppetcode", conf.WorkRepo)
			if err != nil {
				logger.Fatalf("Unable to fetch repo: %v", err)
			}
			time.Sleep(5 * time.Second)
		}
	}()

	logger.Println("Unlocking 'all' in case they are locked, from a segfault")
	unlockAll(conf, logger)
	if conf.ManualMode {
		logger.Println("Manual mode, not starting loops")
	} else {
		if conf.LoopNoopSleepSeconds == 0 {
			logger.Println("noopLoop disabled")
		} else {
			go noopLoop(conf, repo, templates)
		}
		if conf.LoopMilestoneSleepSeconds == 0 {
			logger.Println("milestoneLoop disabled")
		} else {
			go milestoneLoop(conf, repo)
		}
		if conf.LoopApplySleepSeconds == 0 {
			logger.Println("applyLoop disabled")
		} else {
			go applyLoop(conf, repo)
		}
		if conf.LoopApprovalSleepSeconds == 0 {
			logger.Println("approvalLoop disabled")
		} else {
			go approvalLoop(conf, repo)
		}
		if conf.LoopDirtySleepSeconds == 0 {
			logger.Println("dirtyLoop disabled")
		} else {
			go dirtyLoop(conf, repo)
		}
		hecklerdCron := cron.New()
		if conf.AutoTagCronSchedule == "" {
			logger.Println("autoTag cron schedule disabled")
		} else {
			logger.Printf("autoTag enabled with cron schedule of '%s'", conf.AutoTagCronSchedule)
			hecklerdCron.AddFunc(
				conf.AutoTagCronSchedule,
				func() {
					autoTag(conf, repo)
				},
			)
		}
		hecklerdCron.AddFunc(
			"0 12 * * *",
			func() {
				gitutil.Gc(conf.WorkRepo, logger)
			},
		)
		hecklerdCron.AddFunc(
			"0 14 * * *",
			func() {
				gitutil.Gc(conf.ServedRepo, logger)
			},
		)
		hecklerdCron.Start()
		defer hecklerdCron.Stop()
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
