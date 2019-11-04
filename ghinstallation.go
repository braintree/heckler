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
	ctx := context.Background()
	//r := &github.Repository{Name: github.String("potto"), Private: github.Bool(false), Description: github.String("description")}
	//func (s *IssuesService) Create(ctx context.Context, owner string, repo string, issue *IssueRequest) (*Issue, *Response, error)
	//func (s *IssuesService) CreateMilestone(ctx context.Context, owner string, repo string, milestone *Milestone) (*Milestone, *Response, error)
	//
	m := &github.Milestone{
		Title: github.String("v1.1"),
	}
	nm, _, err := client.Issues.CreateMilestone(ctx, "lollipopman", "muppetshow", m)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Successfully created new milestone: %v\n", *nm.Title)

	i := &github.IssueRequest{
		Title:     github.String("gonzo3"),
		Assignee:  github.String("lollipopman"),
		Body:      github.String("```bash\n$ ps -ef\n```\n"),
		Milestone: nm.Number,
	}
	ni, _, err := client.Issues.Create(ctx, "lollipopman", "muppetshow", i)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Successfully created new issue: %v\n", *ni.Title)
}
