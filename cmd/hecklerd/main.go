package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.braintreeps.com/lollipopman/heckler/gitutil"
	"github.com/bradleyfalzon/ghinstallation"
	"github.com/google/go-github/github"
	git "github.com/libgit2/git2go"
	gitcgiserver "github.com/pasela/git-cgi-server"
)

const GitHubEnterpriseURL = "https://github.braintreeps.com/api/v3"

const (
	ApplicationName = "git-cgi-server"

	defaultAddr     = ":8080"
	shutdownTimeout = time.Second * 5
)

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

	cloneDir := "/var/lib/hecklerd/repos/muppetshow"
	cloneOptions := &git.CloneOptions{}
	cloneOptions.Bare = true
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

func main() {
	var err error

	_, err = fetchRepo()
	if err != nil {
		log.Fatalf("Unable to fetch repo: %v", err)
	}

	server := &gitcgiserver.GitCGIServer{}
	server.ExportAll = true
	server.ProjectRoot = "/var/lib/hecklerd/repos"
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

	log.Printf("Starting HTTP server on %s (PID=%d)\n", server.Addr, os.Getpid())
	if err := server.Serve(); err != nil && err != http.ErrServerClosed {
		log.Println("HTTP server error:", err)
	}

	<-idleConnsClosed
	log.Println("HTTP server stopped")
}
