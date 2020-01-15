package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
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

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	var hosts hostFlags
	var beginRev string
	var noop bool
	var rizzoClients []puppetutil.RizzoClient
	var c chan puppetutil.PuppetReport

	flag.Var(&hosts, "node", "node hostnames to group")
	flag.StringVar(&beginRev, "begin", "", "begin rev")
	flag.BoolVar(&noop, "noop", false, "noop")
	flag.Parse()

	if len(hosts) == 0 {
		log.Fatalf("must supply node list")
	}

	_, err := fetchRepo()
	if err != nil {
		log.Fatalf("Unable to fetch repo: %v", err)
	}

	// Set up a connection to the server.
	for _, host := range hosts {
		address := host + ":50051"
		log.Printf("Dialing: %v", address)
		conn, err := grpc.Dial(host+":50051", grpc.WithInsecure(), grpc.WithBlock())
		if err != nil {
			log.Fatalf("did not connect: %v", err)
		}
		defer conn.Close()
		rizzoClients = append(rizzoClients, puppetutil.NewRizzoClient(conn))
	}

	par := puppetutil.PuppetApplyRequest{Rev: beginRev, Noop: noop}
	c = make(chan puppetutil.PuppetReport, len(rizzoClients))
	for _, rc := range rizzoClients {
		go hecklerApply(rc, c, par)
	}

	for r := range c {
		jpr, err := json.MarshalIndent(r, "", "\t")
		if err != nil {
			log.Fatalf("could not apply: %v", err)
		}
		log.Printf("%s", jpr)
	}
}
