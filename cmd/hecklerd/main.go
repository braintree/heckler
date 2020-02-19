package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.braintreeps.com/lollipopman/heckler/internal/gitutil"
	"github.com/bradleyfalzon/ghinstallation"
	"github.com/google/go-github/github"
	git "github.com/libgit2/git2go"
	gitcgiserver "github.com/lollipopman/git-cgi-server"
	"gopkg.in/yaml.v3"
)

const GitHubEnterpriseURL = "https://github.braintreeps.com/api/v3"

const (
	ApplicationName = "git-cgi-server"

	defaultAddr     = ":8080"
	shutdownTimeout = time.Second * 5
)

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
		log.Fatalf("Unable to fetch repo: %v", err)
	}

	server := &gitcgiserver.GitCGIServer{}
	server.ExportAll = true
	server.ProjectRoot = "/var/lib/hecklerd/served_repo"
	server.Addr = defaultAddr
	server.ShutdownTimeout = shutdownTimeout

	idleConnsClosed := make(chan struct{})
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		log.Printf("Shutting down HTTP server by signal '%s' received\n", <-sigCh)

		if err := server.Shutdown(context.Background()); err != nil {
			log.Printf("HTTP server shutdown error: %v", err)
		}
		close(idleConnsClosed)
	}()

	go func() {
		for {
			time.Sleep(5 * time.Second)
			_, err = fetchRepo(hecklerdConf)
			if err != nil {
				log.Fatalf("Unable to fetch repo: %v", err)
			}
		}
	}()

	log.Printf("Starting HTTP server on %s (PID=%d)\n", server.Addr, os.Getpid())
	if err := server.Serve(); err != nil && err != http.ErrServerClosed {
		log.Println("HTTP server error:", err)
	}

	<-idleConnsClosed
	log.Println("HTTP server stopped")
}
