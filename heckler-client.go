package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"./gitutil"
	"./puppetutil"
	"github.com/bradleyfalzon/ghinstallation"
	"github.com/google/go-github/github"
	git "github.com/libgit2/git2go"
	"google.golang.org/grpc"
)

const GitHubEnterpriseURL = "https://github.braintreeps.com/api/v3"

type hostFlags []string

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

func grpcConnect(host string, clientConnChan chan *grpc.ClientConn) {
	var conn *grpc.ClientConn
	address := host + ":50051"
	log.Printf("Dialing: %v", host)
	conn, err := grpc.Dial(address, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Fatalf("Unable to connect to: %v, %v", host, err)
	}
	log.Printf("Connected: %v", host)
	clientConnChan <- conn
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	var hosts hostFlags
	var beginRev string
	var endRev string
	var rev string
	var noop bool
	var rizzoClients []puppetutil.RizzoClient
	var puppetReportChan chan puppetutil.PuppetReport
	puppetReportChan = make(chan puppetutil.PuppetReport, len(rizzoClients))

	flag.Var(&hosts, "node", "node hostnames to group")
	flag.StringVar(&beginRev, "beginrev", "", "begin rev")
	flag.StringVar(&endRev, "endrev", "", "end rev")
	flag.StringVar(&rev, "rev", "", "rev to apply or noop")
	flag.BoolVar(&noop, "noop", false, "noop")
	flag.Parse()

	if rev != "" && (beginRev != "" || endRev != "") {
		fmt.Printf("The -rev flag cannot be combined with the -beginrev or the -endrev\n")
		flag.Usage()
		os.Exit(1)
	}

	if len(hosts) == 0 {
		fmt.Printf("You must supply one or more nodes\n")
		flag.Usage()
		os.Exit(1)
	}

	repo, err := fetchRepo()
	if err != nil {
		log.Fatalf("Unable to fetch repo: %v", err)
	}

	var clientConnChan chan *grpc.ClientConn
	var conn *grpc.ClientConn
	clientConnChan = make(chan *grpc.ClientConn)

	for _, host := range hosts {
		go grpcConnect(host, clientConnChan)
	}

	for range hosts {
		conn = <-clientConnChan
		rizzoClients = append(rizzoClients, puppetutil.NewRizzoClient(conn))
	}

	if rev != "" {
		par := puppetutil.PuppetApplyRequest{Rev: rev, Noop: noop}
		for _, rc := range rizzoClients {
			go hecklerApply(rc, puppetReportChan, par)
		}

		for range hosts {
			r := <-puppetReportChan
			log.Printf("Applied: %s@%s", r.Host, r.ConfigurationVersion)
		}
	}

	if beginRev != "" && endRev != "" {

		log.Printf("Walk begun nooping: %s..%s\n", beginRev, endRev)
		rv, err := repo.Walk()
		if err != nil {
			log.Fatal(err)
		}
		rv.Sorting(git.SortTopological)
		err = rv.PushRef("refs/tags/v2")
		if err != nil {
			log.Fatal(err)
		}
		err = rv.HideRef("refs/tags/v1")
		if err != nil {
			log.Fatal(err)
		}

		var gi git.Oid
		for rv.Next(&gi) == nil {
			log.Printf("Nooping commit: %v\n", gi.String())
			par := puppetutil.PuppetApplyRequest{Rev: gi.String(), Noop: true}
			for _, rc := range rizzoClients {
				go hecklerApply(rc, puppetReportChan, par)
			}

			for range hosts {
				r := <-puppetReportChan
				log.Printf("Received noop: %s@%s", r.Host, r.ConfigurationVersion)
			}
		}
		log.Println("Walk Successful")
	}
}
