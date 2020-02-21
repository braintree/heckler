package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.braintreeps.com/lollipopman/heckler/internal/gitutil"
	"github.braintreeps.com/lollipopman/heckler/internal/hecklerpb"
	"github.braintreeps.com/lollipopman/heckler/internal/puppetutil"
	"github.com/bradleyfalzon/ghinstallation"
	"github.com/google/go-github/github"
	git "github.com/libgit2/git2go"
	gitcgiserver "github.com/lollipopman/git-cgi-server"
	"google.golang.org/grpc"
	"gopkg.in/yaml.v3"
)

const GitHubEnterpriseURL = "https://github.braintreeps.com/api/v3"

const (
	ApplicationName = "git-cgi-server"

	defaultAddr     = ":8080"
	shutdownTimeout = time.Second * 5
	port            = ":50051"
)

// hecklerServer is used to implement heckler.HecklerServer
type hecklerServer struct {
	hecklerpb.UnimplementedHecklerServer
	conf *HecklerdConf
	repo *git.Repository
}

type Node struct {
	host                 string
	commitReports        map[git.Oid]*puppetutil.PuppetReport
	commitDeltaResources map[git.Oid]map[ResourceTitle]*deltaResource
	rizzoClient          puppetutil.RizzoClient
	lastApply            *git.Oid
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

func grpcConnect(node *Node, clientConnChan chan *Node) {
	address := node.host + ":50051"
	log.Printf("Dialing: %v", node.host)
	conn, err := grpc.Dial(address, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Fatalf("Unable to connect to: %v, %v", address, err)
	}
	log.Printf("Connected: %v", node.host)
	node.rizzoClient = puppetutil.NewRizzoClient(conn)
	clientConnChan <- node
}

func dialNodes(hosts []string) map[string]*Node {
	nodes := make(map[string]*Node)
	clientConnChan := make(chan *Node)
	for _, host := range hosts {
		nodes[host] = new(Node)
		nodes[host].host = host
	}

	for _, node := range nodes {
		go grpcConnect(node, clientConnChan)
	}

	var node *Node
	for range nodes {
		node = <-clientConnChan
		nodes[node.host] = node
	}
	return nodes
}

func (hs *hecklerServer) HecklerStatus(ctx context.Context, req *hecklerpb.HecklerStatusRequest) (*hecklerpb.HecklerStatusReport, error) {
	nodes := dialNodes(req.Nodes)
	err := nodeLastApply(nodes, hs.repo)
	if err != nil {
		return nil, err
	}
	hsr := new(hecklerpb.HecklerStatusReport)
	hsr.NodeStatuses = make(map[string]string)
	for _, node := range nodes {
		hsr.NodeStatuses[node.host] = node.lastApply.String()
	}
	return hsr, nil
}

func hecklerLastApply(rc puppetutil.RizzoClient, c chan<- puppetutil.PuppetReport) {
	plar := puppetutil.PuppetLastApplyRequest{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*300)
	defer cancel()
	r, err := rc.PuppetLastApply(ctx, &plar)
	if err != nil {
		c <- puppetutil.PuppetReport{}
	}
	c <- *r
}

func nodeLastApply(nodes map[string]*Node, repo *git.Repository) error {
	var err error

	puppetReportChan := make(chan puppetutil.PuppetReport)
	for _, node := range nodes {
		go hecklerLastApply(node.rizzoClient, puppetReportChan)
	}

	var obj *git.Object
	for range nodes {
		r := <-puppetReportChan
		obj, err = repo.RevparseSingle(r.ConfigurationVersion)
		if err != nil {
			return err
		}
		if node, ok := nodes[r.Host]; ok {
			node.lastApply = obj.Id()
		} else {
			log.Fatalf("No Node struct found for report from: %s\n", r.Host)
		}
	}

	return nil
}

func fetchRepo(conf *HecklerdConf) (*git.Repository, error) {

	// Shared transport to reuse TCP connections.
	tr := http.DefaultTransport

	// XXX dedup with heckler
	var privateKeyPath string
	if _, err := os.Stat("/etc/heckler/github-private-key.pem"); err == nil {
		privateKeyPath = "/etc/heckler/github-private-key.pem"
	} else if _, err := os.Stat("github-private-key.pem"); err == nil {
		privateKeyPath = "github-private-key.pem"
	} else {
		log.Fatal("Unable to load github-private-key.pem in /etc/heckler or .")
	}
	// Wrap the shared transport for use with the app ID 7 authenticating with
	// installation ID 11.
	itr, err := ghinstallation.NewKeyFromFile(tr, 7, 11, privateKeyPath)
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

	cloneDir := "/var/lib/hecklerd/served_repo/puppetcode"
	cloneOptions := &git.CloneOptions{}
	cloneOptions.Bare = true
	remoteUrl := fmt.Sprintf("https://x-access-token:%s@"+conf.PuppetCodeGitURL, tok)
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

type HecklerdConf struct {
	PuppetCodeGitURL string `yaml:"puppet_code_git_url"`
}

func main() {
	var err error
	var hecklerdConfPath string
	var hecklerdConf *HecklerdConf
	var file *os.File
	var data []byte

	if _, err := os.Stat("/etc/heckler/hecklerd_conf.yaml"); err == nil {
		hecklerdConfPath = "/etc/heckler/hecklerd_conf.yaml"
	} else if _, err := os.Stat("hecklerd_conf.yaml"); err == nil {
		hecklerdConfPath = "hecklerd_conf.yaml"
	} else {
		log.Fatal("Unable to load hecklerd_conf.yaml from /etc/heckler or .")
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

	_, err = fetchRepo(hecklerdConf)
	if err != nil {
		log.Fatalf("Unable to fetch repo to serve: %v", err)
	}

	gitServer := &gitcgiserver.GitCGIServer{}
	gitServer.ExportAll = true
	gitServer.ProjectRoot = "/var/lib/hecklerd/served_repo"
	gitServer.Addr = defaultAddr
	gitServer.ShutdownTimeout = shutdownTimeout

	idleConnsClosed := make(chan struct{})
	done := make(chan bool, 1)

	// background polling git fetch
	go func() {
		for {
			time.Sleep(5 * time.Second)
			_, err = fetchRepo(hecklerdConf)
			if err != nil {
				log.Fatalf("Unable to fetch repo: %v", err)
			}
		}
	}()

	// git server
	go func() {
		log.Printf("Starting Git HTTP server on %s (PID=%d)\n", gitServer.Addr, os.Getpid())
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
	// HACK
	time.Sleep(1 * time.Second)
	repo, err := gitutil.Pull("http://localhost:8080/puppetcode", "/var/lib/hecklerd/work_repo/puppetcode")
	if err != nil {
		log.Fatalf("Unable to fetch repo: %v", err)
	}
	hecklerServer.repo = repo
	hecklerpb.RegisterHecklerServer(grpcServer, hecklerServer)

	// grpc server
	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()

	// XXX any reason to make this a separate goroutine?
	go func() {
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
		log.Printf("Received %s\n", <-sigs)
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
