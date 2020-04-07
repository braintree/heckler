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
	"github.com/Masterminds/semver/v3"
	"github.com/Masterminds/sprig"
	"github.com/bradleyfalzon/ghinstallation"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-github/v29/github"
	git "github.com/libgit2/git2go"
	gitcgiserver "github.com/lollipopman/git-cgi-server"
	"github.com/robfig/cron/v3"
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
	Repo                       string             `yaml:"repo"`
	RepoOwner                  string             `yaml:"repo_owner"`
	GitHubDomain               string             `yaml:"github_domain"`
	GitHubPrivateKeyPath       string             `yaml:"github_private_key_path"`
	GitHubAppSlug              string             `yaml:"github_app_slug"`
	GitHubAppId                int64              `yaml:"github_app_id"`
	GitHubAppInstallId         int64              `yaml:"github_app_install_id"`
	NodeSets                   map[string]NodeSet `yaml:"node_sets"`
	AutoTagCronSchedule        string             `yaml:"auto_tag_cron_schedule"`
	AutoCloseIssues            bool               `yaml:"auto_close_issues"`
	EnvPrefix                  string             `yaml:"env_prefix"`
	AllowedNumberOfErrorNodes  int                `yaml:"allowed_number_of_error_nodes"`
	AllowedNumberOfLockedNodes int                `yaml:"allowed_number_of_locked_nodes"`
	GitServerMaxClients        int                `yaml:"git_server_max_clients"`
	ManualMode                 bool               `yaml:"manual_mode"`
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
	grpcConn             *grpc.ClientConn
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

func commitParentReports(commit git.Commit, lastApply git.Oid, commitReports map[git.Oid]*rizzopb.PuppetReport, repo *git.Repository, logger *log.Logger) []*rizzopb.PuppetReport {
	var parentReports []*rizzopb.PuppetReport
	var parentReport *rizzopb.PuppetReport

	parentCount := commit.ParentCount()
	for i := uint(0); i < parentCount; i++ {
		// perma-diff: If our parent is already applied we substitute the lastApply
		// noop so that we can subtract away any perma-diffs from the noop
		if commitAlreadyApplied(lastApply, *commit.ParentId(i), repo) {
			logger.Printf("Parent already applied substituting lastApply, %s", commit.ParentId(i).String())
			parentReport = commitReports[lastApply]
		} else {
			parentReport = commitReports[*commit.ParentId(i)]
		}
		if parentReport != nil {
			parentReports = append(parentReports, commitReports[*commit.ParentId(i)])
		} else {
			logger.Fatalf("Parent report not found %s", commit.ParentId(i).String())
		}
	}
	return parentReports
}

func grpcConnect(ctx context.Context, node *Node, clientConnChan chan nodeResult) {
	address := node.host + ":50051"
	ctx, cancel := context.WithTimeout(ctx, time.Duration(5)*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, address, grpc.WithInsecure(), grpc.WithBlock(), grpc.FailOnNonTempDialError(true))
	if errors.Is(err, context.DeadlineExceeded) {
		clientConnChan <- nodeResult{node, errors.New(fmt.Sprintf("Timeout connecting to %s", address))}
	} else if err != nil {
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

func loadNoop(commit git.Oid, node *Node, revdir string, repo *git.Repository, logger *log.Logger) (*rizzopb.PuppetReport, error) {
	// perma-diff: Substitute an empty puppet noop report if the commit is
	// already applied, however for the lastApplied commit we do want to use the
	// noop report so we can use the noop diff to subtract away perma-diffs from
	// children.
	descendant, err := repo.DescendantOf(&node.lastApply, &commit)
	if err != nil {
		logger.Fatalf("Cannot determine descendant status: %v", err)
	}
	if descendant {
		logger.Printf("Commit already applied, substituting an empty noop: %s@%s", node.host, commit.String())
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
		logger.Printf("Found serialized noop: %s@%s", rprt.Host, rprt.ConfigurationVersion)
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

func noopCommitRange(nodes map[string]*Node, beginRev, endRev string, commitLogIds []git.Oid, commits map[git.Oid]*git.Commit, repo *git.Repository, logger *log.Logger) (map[git.Oid][]*groupedResource, error) {
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

	var nodeCommitToNoop git.Oid
	for i, commitToNoop := range commitsToNoop {
		logger.Printf("Nooping: %s (%d of %d)", commitToNoop.String(), i+1, len(commitsToNoop))
		noopRequests = 0
		for _, node := range nodes {
			// perma-diff: We need a noop report of the lastApply so we can use it to
			// subtract away perma-diffs from children. Typically the lastApply is
			// the beginRev, but if someone has applied a newer commit on the host we
			// need to use the noop from the lastApplied commit.
			if commitToNoop == beginCommitId {
				nodeCommitToNoop = node.lastApply
			} else {
				nodeCommitToNoop = commitToNoop
			}
			if rprt, err = loadNoop(nodeCommitToNoop, node, revdir, repo, logger); err == nil {
				nodes[node.host].commitReports[nodeCommitToNoop] = rprt
			} else if os.IsNotExist(err) {
				logger.Printf("Requesting noop %s@%s", node.host, nodeCommitToNoop.String())
				par := rizzopb.PuppetApplyRequest{Rev: nodeCommitToNoop.String(), Noop: true}
				go hecklerApply(node.rizzoClient, puppetReportChan, par)
				noopRequests++
			} else {
				logger.Fatalf("Unable to load noop: %v", err)
			}
		}

		if noopRequests > 0 {
			logger.Printf("Waiting for %d outstanding noop requests", noopRequests)
		}
		for j := 0; j < noopRequests; j++ {
			newRprt := normalizeReport(<-puppetReportChan)
			logger.Printf("Received noop: %s@%s", newRprt.Host, newRprt.ConfigurationVersion)
			commitId, err := git.NewOid(newRprt.ConfigurationVersion)
			if err != nil {
				logger.Fatalf("Unable to marshal report: %v", err)
			}
			nodes[newRprt.Host].commitReports[*commitId] = &newRprt
			err = marshalReport(newRprt, revdir, *commitId)
			if err != nil {
				logger.Fatalf("Unable to marshal report: %v", err)
			}
		}
	}

	for host, node := range nodes {
		for _, gi := range commitLogIds {
			logger.Printf("Creating delta resource: %s@%s", host, gi.String())
			// perma-diff: If the commit is already applied we can assume that the
			// diff is empty. Ideally we would not need this special case as the noop
			// for an already applied commit should be empty, but we purposefully
			// substitute the noop of the lastApply to subtract away perma-diffs, so
			// those would show up without this special case.
			// TODO: Assign perma-diffs to server owners?
			if commitAlreadyApplied(node.lastApply, gi, repo) {
				node.commitDeltaResources[gi] = make(map[ResourceTitle]*deltaResource)
			} else {
				node.commitDeltaResources[gi] = deltaNoop(node.commitReports[gi], commitParentReports(*commits[gi], node.lastApply, node.commitReports, repo, logger))
			}
		}
	}

	for _, gi := range commitLogIds {
		logger.Printf("Grouping: %s", gi.String())
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
	return nodesFromSet(conf, nodeSetName)
}

func nodesFromSet(conf *HecklerdConf, nodeSetName string) ([]string, error) {
	if nodeSetName == "" {
		return nil, errors.New("Empty nodeSetName provided")
	}
	var cmdArgs []string
	if nodeSet, ok := conf.NodeSets[nodeSetName]; ok {
		cmdArgs = nodeSet.Cmd
	} else {
		return nil, errors.New(fmt.Sprintf("nodeSetName '%s' not found in hecklerd config", nodeSetName))
	}
	// Change to code dir, so hiera relative paths resolve
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
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
	if len(nodes) == 0 {
		return nil, errors.New(fmt.Sprintf("Cmd '%s' produced zero nodes", cmd))
	}
	return nodes, nil
}

func (hs *hecklerServer) HecklerApply(ctx context.Context, req *hecklerpb.HecklerApplyRequest) (*hecklerpb.HecklerApplyReport, error) {
	logger := log.New(os.Stdout, "[HecklerApply] ", log.Lshortfile)
	nodesToDial, err := reqNodes(hs.conf, req.Nodes, req.NodeSet)
	if err != nil {
		return nil, err
	}
	dialedNodes, errDialNodes := dialNodes(ctx, nodesToDial)
	defer closeNodes(dialedNodes)
	lockReq := hecklerpb.HecklerLockRequest{
		User:    req.User,
		Comment: "Applying with Heckler",
	}
	lockedNodes, lockedByAnotherNodes, errLockNodes := nodeLock(&lockReq, dialedNodes)
	unlockReq := hecklerpb.HecklerUnlockRequest{
		User: req.User,
	}
	defer nodeUnlock(&unlockReq, lockedNodes)
	appliedNodes, beyondRevNodes, errApplyNodes := applyNodes(lockedNodes, req.Force, req.Noop, req.Rev, hs.repo, logger)
	errNodes := make(map[string]error)
	for host, err := range errDialNodes {
		errNodes[host] = err
	}
	for host, err := range errLockNodes {
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
	for host, lockState := range lockedByAnotherNodes {
		har.NodeErrors[host] = lockState
	}
	if req.Force {
		har.Output = fmt.Sprintf("Applied nodes: %d; Error nodes: %d", len(appliedNodes), len(errNodes))
	} else {
		har.Output = fmt.Sprintf("Applied nodes: %d; Beyond rev nodes: %d; Error nodes: %d", len(appliedNodes), len(beyondRevNodes), len(errNodes))
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
	puppetReportChan := make(chan rizzopb.PuppetReport)
	for _, node := range nodesToApply {
		go hecklerApply(node.rizzoClient, puppetReportChan, par)
	}

	errApplyNodes := make(map[string]error)
	appliedNodes = make([]string, 0)
	for range nodesToApply {
		r := <-puppetReportChan
		if cmp.Equal(r, rizzopb.PuppetReport{}) {
			logger.Fatalf("Received an empty report")
		} else if r.Status == "failed" {
			logger.Printf("ERROR: Apply failed, %s@%s", r.Host, r.ConfigurationVersion)
			errApplyNodes[r.Host] = errors.New("Apply failed")
		} else {
			if noop {
				logger.Printf("Nooped: %s@%s", r.Host, r.ConfigurationVersion)
			} else {
				logger.Printf("Applied: %s@%s", r.Host, r.ConfigurationVersion)
			}
			appliedNodes = append(appliedNodes, r.Host)
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
	nodesToDial, err := reqNodes(hs.conf, req.Nodes, req.NodeSet)
	if err != nil {
		return nil, err
	}
	dialedNodes, errDialNodes := dialNodes(ctx, nodesToDial)
	defer closeNodes(dialedNodes)
	lastApplyNodes, errUnknownRevNodes := nodeLastApply(dialedNodes, hs.repo, logger)
	lockReq := hecklerpb.HecklerLockRequest{
		User:    req.User,
		Comment: "Nooping with Heckler",
	}
	lockedNodes, lockedByAnotherNodes, errLockNodes := nodeLock(&lockReq, lastApplyNodes)
	unlockReq := hecklerpb.HecklerUnlockRequest{
		User: req.User,
	}

	commitLogIds, commits, err := commitLogIdList(hs.repo, req.BeginRev, req.EndRev)
	if err != nil {
		return nil, err
	}
	groupedCommits, err := noopCommitRange(lockedNodes, req.BeginRev, req.EndRev, commitLogIds, commits, hs.repo, logger)
	if err != nil {
		nodeUnlock(&unlockReq, lockedNodes)
		return nil, err
	}
	nodeUnlock(&unlockReq, lockedNodes)
	rprt := new(hecklerpb.HecklerNoopRangeReport)
	if req.OutputFormat == hecklerpb.OutputFormat_markdown {
		rprt.Output = markdownOutput(hs.conf, commitLogIds, commits, groupedCommits, hs.templates)
	}
	rprt.NodeErrors = make(map[string]string)
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

// TODO: add comment with reason for closing
func closeIssue(ghclient *github.Client, conf *HecklerdConf, issue *github.Issue) error {
	issuePatch := &github.IssueRequest{
		State: github.String("closed"),
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	_, _, err := ghclient.Issues.Edit(ctx, conf.RepoOwner, conf.Repo, *issue.Number, issuePatch)
	return err
}

func githubCreateIssues(ghclient *github.Client, conf *HecklerdConf, commitLogIds []git.Oid, groupedCommits map[git.Oid][]*groupedResource, commits map[git.Oid]*git.Commit, templates *template.Template, logger *log.Logger) error {
	ctx := context.Background()
	prefix := conf.EnvPrefix

	for _, gi := range commitLogIds {
		issue, err := githubIssueFromCommit(ghclient, gi, conf)
		if err != nil {
			return err
		}
		if issue != nil {
			logger.Printf("Issue already exists for commit %s, skipping create", gi.String())
			continue
		}
		githubIssue := &github.IssueRequest{
			Title: github.String(noopTitle(gi, prefix)),
			// TODO need to enforce github user IDs for commits, so that we always
			// have a valid github user.
			Assignee: github.String("lollipopman"),
			Body:     github.String(commitToMarkdown(commits[gi], templates) + groupedResourcesToMarkdown(groupedCommits[gi], templates)),
		}
		ni, _, err := ghclient.Issues.Create(ctx, conf.RepoOwner, conf.Repo, githubIssue)
		if err != nil {
			logger.Fatal(err)
		}
		logger.Printf("Successfully created new issue: '%v'", *ni.Title)
		if len(groupedCommits[gi]) == 0 {
			logger.Println("No noop output marking issue as 'closed'")
			err := closeIssue(ghclient, conf, ni)
			if err != nil {
				logger.Fatal(err)
			}
		} else if conf.AutoCloseIssues {
			logger.Println("Auto close set, marking issue as 'closed'")
			err := closeIssue(ghclient, conf, ni)
			if err != nil {
				logger.Fatal(err)
			}
		}
	}
	return nil
}

func noopTitle(gi git.Oid, prefix string) string {
	return fmt.Sprintf("%sPuppet noop output for commit: %s", issuePrefix(prefix), gi.String())
}

func markdownOutput(conf *HecklerdConf, commitLogIds []git.Oid, commits map[git.Oid]*git.Commit, groupedCommits map[git.Oid][]*groupedResource, templates *template.Template) string {
	var output string
	for _, gi := range commitLogIds {
		if len(groupedCommits[gi]) == 0 {
			log.Printf("Skipping %s, no noop output", gi.String())
			continue
		}
		output += fmt.Sprintf("## %s\n\n", noopTitle(gi, conf.EnvPrefix))
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
	logger := log.New(os.Stdout, "[HecklerStatus] ", log.Lshortfile)
	nodesToDial, err := reqNodes(hs.conf, req.Nodes, req.NodeSet)
	if err != nil {
		return nil, err
	}
	dialedNodes, errNodes := dialNodes(ctx, nodesToDial)
	defer closeNodes(dialedNodes)
	lastApplyNodes, errUnknownRevNodes := nodeLastApply(dialedNodes, hs.repo, logger)
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
	dialedNodes, errNodes := dialNodes(ctx, nodesToDial)
	defer closeNodes(dialedNodes)
	unlockedNodes, errUnlockNodes := nodeUnlock(req, dialedNodes)
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

func closeNodes(nodes map[string]*Node) {
	for _, node := range nodes {
		if node.grpcConn != nil {
			node.grpcConn.Close()
		}
	}
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
	dialedNodes, errNodes := dialNodes(ctx, nodesToDial)
	defer closeNodes(dialedNodes)
	lockedNodes, lockedByAnother, errLockNodes := nodeLock(req, dialedNodes)
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

func nodeLock(req *hecklerpb.HecklerLockRequest, nodes map[string]*Node) (map[string]*Node, map[string]string, map[string]error) {
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
	lockedByAnother := make(map[string]string)
	errNodes := make(map[string]error)
	for i := 0; i < len(nodes); i++ {
		r := <-reportChan
		switch r.LockStatus {
		case rizzopb.LockStatus_locked_by_user:
			lockedNodes[r.Host] = nodes[r.Host]
		case rizzopb.LockStatus_locked_by_another:
			lockedByAnother[r.Host] = fmt.Sprintf("%s: %s", r.User, r.Comment)
		case rizzopb.LockStatus_lock_unknown:
			errNodes[r.Host] = errors.New(r.Error)
		default:
			log.Fatal("Unknown lockStatus!")
		}
	}

	return lockedNodes, lockedByAnother, errNodes
}

func hecklerLock(host string, rc rizzopb.RizzoClient, req rizzopb.PuppetLockRequest, c chan<- rizzopb.PuppetLockReport) {
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
		nodesToDial, err := nodesFromSet(conf, "all")
		if err != nil {
			logger.Fatalf("Unable to load 'all' node set: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
		defer cancel()
		dialedNodes, errDialNodes := dialNodes(ctx, nodesToDial)
		lastApplyNodes, errUnknownRevNodes := nodeLastApply(dialedNodes, repo, logger)
		errNodes := make(map[string]error)
		for host, err := range errDialNodes {
			errNodes[host] = err
		}
		for host, err := range errUnknownRevNodes {
			errNodes[host] = err
		}
		commonTag, err := commonAncestorTag(lastApplyNodes, prefix, repo, logger)
		if err != nil {
			logger.Fatalf("Unable to find common tag: %v", err)
		}
		if commonTag == "" {
			logger.Println("No common tag found, sleeping")
			closeNodes(dialedNodes)
			continue
		}
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
		lockReq := hecklerpb.HecklerLockRequest{
			User:    "root",
			Comment: "Applying with Heckler",
		}
		lockedNodes, lockedByAnotherNodes, errLockNodes := nodeLock(&lockReq, dialedNodes)
		unlockReq := hecklerpb.HecklerUnlockRequest{
			User: "root",
		}
		appliedNodes, beyondRevNodes, errApplyNodes := applyNodes(lockedNodes, false, false, nextTag, repo, logger)
		nodeUnlock(&unlockReq, lockedNodes)
		for host, err := range errLockNodes {
			errNodes[host] = err
		}
		for host, err := range errApplyNodes {
			errNodes[host] = err
		}
		for host, lockState := range lockedByAnotherNodes {
			logger.Printf("lockedByAnother: %s, %v", host, lockState)
		}
		for host, err := range errNodes {
			logger.Printf("errNodes: %s, %v", host, err)
		}
		logger.Printf("Applied nodes: %d; Beyond rev nodes: %d; Error nodes: %d", len(appliedNodes), len(beyondRevNodes), len(errNodes))
		logger.Println("Apply complete, sleeping")
		closeNodes(dialedNodes)
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
		nodesToDial, err := nodesFromSet(conf, "all")
		if err != nil {
			logger.Fatalf("Unable to load 'all' node set: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
		defer cancel()
		dialedNodes, errDialNodes := dialNodes(ctx, nodesToDial)
		lastApplyNodes, errUnknownRevNodes := nodeLastApply(dialedNodes, repo, logger)
		errNodes := make(map[string]error)
		for host, err := range errDialNodes {
			errNodes[host] = err
		}
		for host, err := range errUnknownRevNodes {
			errNodes[host] = err
		}
		for host, err := range errNodes {
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
		if err != nil {
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

//  Are there newer commits than our common lastApply across "all" nodes?
//  If yes
//    check if all commits have issues on github
//      If no, run noop range, needs to be idempotent
//      If yes, do nothing
//  If no, do nothing
func noopLoop(conf *HecklerdConf, repo *git.Repository, templates *template.Template) {
	logger := log.New(os.Stdout, "[noopLoop] ", log.Lshortfile)
	prefix := conf.EnvPrefix
	errorThresh := conf.AllowedNumberOfErrorNodes
	lockedThresh := conf.AllowedNumberOfLockedNodes
	for {
		time.Sleep(10 * time.Second)
		nodesToDial, err := nodesFromSet(conf, "all")
		if err != nil {
			logger.Fatalf("Unable to load 'all' node set: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
		defer cancel()
		dialedNodes, errDialNodes := dialNodes(ctx, nodesToDial)
		if len(errDialNodes) > errorThresh {
			logger.Printf("Nodes which could not be dialed(%d) exceeds the threshold(%d), sleeping", len(errDialNodes), errorThresh)
			closeNodes(dialedNodes)
			continue
		}
		lastApplyNodes, errUnknownRevNodes := nodeLastApply(dialedNodes, repo, logger)
		if len(errUnknownRevNodes) > errorThresh {
			logger.Printf("Unknown rev nodes(%d) exceeds the threshold(%d), sleeping", len(errUnknownRevNodes), errorThresh)
			closeNodes(dialedNodes)
			continue
		}
		commonTag, err := commonAncestorTag(lastApplyNodes, prefix, repo, logger)
		if err != nil {
			logger.Fatalf("Call to commonAncestorTag failed: %v", err)
		}
		if commonTag == "" {
			logger.Println("No common tag found, sleeping")
			closeNodes(dialedNodes)
			continue
		}
		logger.Printf("Found common tag: %s", commonTag)
		errNodes := make(map[string]error)
		for host, err := range errDialNodes {
			errNodes[host] = err
		}
		for host, err := range errUnknownRevNodes {
			errNodes[host] = err
		}
		for host, err := range errNodes {
			logger.Printf("errNodes: %s, %v", host, err)
		}
		// TODO support other branches?
		commitLogIds, commits, err := commitLogIdList(repo, commonTag, "master")
		if err != nil {
			logger.Fatalf("Unable to obtain commit log ids: %v", err)
		}
		if len(commitLogIds) == 0 {
			logger.Println("No new commits, sleeping")
			closeNodes(dialedNodes)
			continue
		}
		ghclient, _, err := githubConn(conf)
		if err != nil {
			logger.Fatalf("Unable to connect to github: %v", err)
		}
		allIssuesExist := true
		for _, gi := range commitLogIds {
			issue, err := githubIssueFromCommit(ghclient, gi, conf)
			if err != nil {
				logger.Fatalf("Unable to determine if issues exists: %s", gi.String())
			}
			if issue == nil {
				allIssuesExist = false
				break
			}
		}
		if allIssuesExist {
			logger.Println("All issues exist, sleeping")
			closeNodes(dialedNodes)
			continue
		}
		lockReq := hecklerpb.HecklerLockRequest{
			User:    "root",
			Comment: "Applying with Heckler",
		}
		lockedNodes, lockedByAnotherNodes, errLockNodes := nodeLock(&lockReq, lastApplyNodes)
		unlockReq := hecklerpb.HecklerUnlockRequest{
			User: "root",
		}
		if len(errLockNodes) > errorThresh {
			logger.Printf("Nodes with errors when locking(%d) exceeds the threshold(%d), sleeping", len(errLockNodes), errorThresh)
			nodeUnlock(&unlockReq, lockedNodes)
			closeNodes(dialedNodes)
			continue
		}
		if len(lockedByAnotherNodes) > lockedThresh {
			logger.Printf("Locked by another nodes(%d) exceeds the threshold(%d), sleeping", len(lockedByAnotherNodes), lockedThresh)
			nodeUnlock(&unlockReq, lockedNodes)
			closeNodes(dialedNodes)
			continue
		}
		for host, lockState := range lockedByAnotherNodes {
			log.Printf("lockedByAnother: %s, %v", host, lockState)
		}
		groupedCommits, err := noopCommitRange(lockedNodes, commonTag, "master", commitLogIds, commits, repo, logger)
		if err != nil {
			nodeUnlock(&unlockReq, lockedNodes)
			logger.Fatalf("Unable to group commits: %v", err)
		}
		nodeUnlock(&unlockReq, lockedNodes)
		err = githubCreateIssues(ghclient, conf, commitLogIds, groupedCommits, commits, templates, logger)
		if err != nil {
			logger.Fatalf("Unable to create github issues: %v", err)
		}
		logger.Println("Nooping complete, sleeping")
		closeNodes(dialedNodes)
	}
}

func commonAncestorTag(nodes map[string]*Node, prefix string, repo *git.Repository, logger *log.Logger) (string, error) {
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

	// get set of tags
	tagNodes := make(map[string][]string)
	for _, node := range nodes {
		commit, err := repo.LookupCommit(&node.lastApply)
		if err != nil {
			return "", err
		}
		result, err := commit.Describe(&describeOpts)
		if err != nil {
			return "", errors.New(fmt.Sprintf("Unable to describe %s@%s, %v", node.host, node.lastApply.String(), err))
		}
		resultStr, err := result.Format(&formatOpts)
		if err != nil {
			return "", err
		}
		if _, ok := tagNodes[resultStr]; ok {
			tagNodes[resultStr] = append(tagNodes[resultStr], node.host)
		} else {
			tagNodes[resultStr] = []string{node.host}
		}
	}
	if len(tagNodes) == 0 {
		logger.Printf("No common tag found, tags to nodes: %v", tagNodes)
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
	templates := parseTemplates()

	flag.BoolVar(&clearState, "clear", false, "Clear local state, e.g. puppet code repo")
	flag.BoolVar(&clearGitHub, "ghclear", false, "Clear remote github state, e.g. issues & milestones")
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
	conf = new(HecklerdConf)
	err = yaml.Unmarshal([]byte(data), conf)
	if err != nil {
		log.Fatalf("Cannot unmarshal config: %v", err)
	}

	if clearState && clearGitHub {
		log.Println("clear & ghclear are mutually exclusive")
		os.Exit(1)
	}

	if clearState {
		log.Printf("Removing state directory: %v", stateDir)
		os.RemoveAll(stateDir)
		os.Exit(0)
	}

	if clearGitHub {
		err = clearGithub(conf)
		if err != nil {
			log.Fatalf("Unable to clear GitHub: %v", err)
		}
		os.Exit(0)
	}

	repo, err := fetchRepo(conf)
	if err != nil {
		log.Fatalf("Unable to fetch repo to serve: %v", err)
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
		for {
			time.Sleep(5 * time.Second)
			if Debug {
				log.Println("Updating repo..")
			}
			_, err = fetchRepo(conf)
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
	hecklerServer.conf = conf
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

	// TODO hack to ensure our git server is up, ideally we would pass in a tcp
	// listener to the git server, so we know it is available, as we did with the
	// grpc server.
	time.Sleep(1 * time.Second)
	_, err = gitutil.Pull("http://localhost:8080/puppetcode", stateDir+"/work_repo/puppetcode")
	if err != nil {
		log.Fatalf("Unable to fetch repo: %v", err)
	}
	go func() {
		for {
			_, err := gitutil.Pull("http://localhost:8080/puppetcode", stateDir+"/work_repo/puppetcode")
			if err != nil {
				log.Fatalf("Unable to fetch repo: %v", err)
			}
			time.Sleep(5 * time.Second)
		}
	}()

	if conf.ManualMode {
		log.Println("Manual mode: not starting noop, milestone, apply, & tag loops")
	} else {
		log.Println("Starting noop, milestone, & apply loops")
		go noopLoop(conf, repo, templates)
		go milestoneLoop(conf, repo)
		go applyLoop(conf, repo)
		if conf.AutoTagCronSchedule != "" {
			log.Println("Starting tag loop")
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
