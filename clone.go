package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"./gitutil"

	"github.com/bradleyfalzon/ghinstallation"
	"github.com/google/go-github/github"
	git "github.com/libgit2/git2go"
)

const GitHubEnterpriseURL = "https://github.braintreeps.com/api/v3"

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// Shared transport to reuse TCP connections.
	tr := http.DefaultTransport

	// Wrap the shared transport for use with the app ID 7 authenticating with
	// installation ID 11.
	itr, err := ghinstallation.NewKeyFromFile(tr, 7, 11, "heckler.2019-10-30.private-key.pem")
	if err != nil {
		log.Fatal(err)
	}
	itr.BaseURL = GitHubEnterpriseURL

	// Use installation transport with github.com/google/go-github
	_, err = github.NewEnterpriseClient(GitHubEnterpriseURL, GitHubEnterpriseURL, &http.Client{Transport: itr})
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	tok, err := itr.Token(ctx)
	if err != nil {
		log.Fatal(err)
	}

	cloneDir := "/data/muppetshow"
	cloneOptions := &git.CloneOptions{}
	remoteUrl := fmt.Sprintf("https://x-access-token:%s@github.braintreeps.com/lollipopman/muppetshow", tok)
	repo, err := gitutil.Clone(remoteUrl, cloneDir, cloneOptions)
	if err != nil {
		log.Fatal(err)
	}
	err = gitutil.FastForward(repo)
	if err != nil {
		log.Fatal(err)
	}
	err = gitutil.Walk(repo)
	if err != nil {
		log.Fatal(err)
	}
}
