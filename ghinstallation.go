package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/bradleyfalzon/ghinstallation"
	"github.com/google/go-github/github"
)

const GitHubEnterpriseURL = "https://github.braintreeps.com/api/v3"

func main() {
	// Shared transport to reuse TCP connections.
	tr := http.DefaultTransport

	// Wrap the shared transport for use with the app ID 1 authenticating with installation ID 99.
	itr, err := ghinstallation.NewKeyFromFile(tr, 7, 11, "heckler.2019-10-30.private-key.pem")
	if err != nil {
		log.Fatal(err)
	}
	itr.BaseURL = GitHubEnterpriseURL

	// Use installation transport with github.com/google/go-github
	client, err := github.NewEnterpriseClient(GitHubEnterpriseURL, GitHubEnterpriseURL, &http.Client{Transport: itr})
	if err != nil {
		log.Fatal(err)
	}

	orgs, _, err := client.Organizations.List(context.Background(), "lollipopman", nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("orgs %v\n", orgs)

	// list public repositories for org "github"
	opt := &github.RepositoryListByOrgOptions{Type: "public"}
	repos, _, err := client.Repositories.ListByOrg(context.Background(), "braintree", opt)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("repos %v\n", repos)
}
