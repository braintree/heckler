package hecklerd

import (
	// 	"github.com/rickar/cal/v2"
	// 	"github.com/rickar/cal/v2/us"
	// 	"github.com/robfig/cron/v3"
	// 	git "github.com/libgit2/git2go/v31"
	"context"
	"fmt"
	"github.com/bradleyfalzon/ghinstallation"
	"github.com/braintree/heckler/internal/gitutil"
	"github.com/google/go-github/v29/github"
	git "github.com/libgit2/git2go/v31"
	"io/ioutil"
	"log"
	// 	"net"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
	"text/template"
	"time"
)

func clearGithub(conf HecklerdConf) error {
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

func githubConn(conf HecklerdConf) (*github.Client, *ghinstallation.Transport, error) {
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

	var privateKeyPath string
	if conf.GitHubPrivateKeyPath != "" {
		privateKeyPath = conf.GitHubPrivateKeyPath
	} else {
		privateKeyPath = "./github-private-key.pem"
	}
	if _, err := os.Stat(privateKeyPath); err == nil {
		file, err = os.Open(privateKeyPath)
		if err != nil {
			return nil, nil, err
		}
		defer file.Close()
		privateKey, err = ioutil.ReadAll(file)
		if err != nil {
			return nil, nil, err
		}
	} else {
		return nil, nil, fmt.Errorf("Unable to load GitHub private key from '%s'", privateKeyPath)
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

func clearIssues(ghclient *github.Client, conf HecklerdConf) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*100)
	defer cancel()
	query := fmt.Sprintf("author:app/%s noop in:title", conf.GitHubAppSlug)
	if conf.EnvPrefix != "" {
		query += fmt.Sprintf(" %sin:title", issuePrefix(conf.EnvPrefix))
	}
	searchOpts := &github.SearchOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}
	var allIssues []github.Issue
	for {
		searchResults, resp, err := ghclient.Search.Issues(ctx, query, searchOpts)
		if err != nil {
			return fmt.Errorf("Unable to search GitHub Issues: %w", err)
		}
		if searchResults.GetIncompleteResults() == true {
			return fmt.Errorf("Incomplete results returned from GitHub")
		}
		allIssues = append(allIssues, searchResults.Issues...)
		if resp.NextPage == 0 {
			break
		}
		searchOpts.Page = resp.NextPage
	}
	for _, issue := range allIssues {
		if issue.GetTitle() != "SoftDeleted" {
			err := deleteIssue(ghclient, conf, &issue)
			if err != nil {
				log.Fatal(err)
			}
		}
	}
	return nil
}

// The rest API does not support deletion,
// https://github.community/t5/GitHub-API-Development-and/Delete-Issues-programmatically/td-p/29524,
// so for now just close the issue and change the title to deleted & remove the milestone
func deleteIssue(ghclient *github.Client, conf HecklerdConf, issue *github.Issue) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	// We need to craft our own request, due to the inability to delete the milestone:
	// https://github.com/google/go-github/issues/236
	u := fmt.Sprintf("repos/%v/%v/issues/%d", conf.RepoOwner, conf.Repo, issue.GetNumber())
	req, err := ghclient.NewRequest("PATCH", u, &struct {
		Title     *string   `json:"title"`
		Body      *string   `json:"body"`
		Labels    *[]string `json:"labels"`
		State     *string   `json:"state"`
		Milestone *int      `json:"milestone"`
		Assignees *[]string `json:"assignees"`
	}{
		Title:     github.String("SoftDeleted"),
		Milestone: nil,
		Body:      github.String(""),
		Labels:    &[]string{},
		Assignees: &[]string{},
		State:     github.String("closed"),
	})
	if err != nil {
		return err
	}
	_, err = ghclient.Do(ctx, req, nil)
	if err != nil {
		return err
	}
	log.Printf("Soft deleted issue #%d: '%s'", issue.GetNumber(), issue.GetTitle())
	return nil
}

func updateIssueMilestone(ghclient *github.Client, conf HecklerdConf, issue *github.Issue, ms *github.Milestone) error {
	issuePatch := &github.IssueRequest{
		Milestone: ms.Number,
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	_, _, err := ghclient.Issues.Edit(ctx, conf.RepoOwner, conf.Repo, *issue.Number, issuePatch)
	return err
}

func closeIssue(ghclient *github.Client, conf HecklerdConf, issue *github.Issue, reason string) error {
	var err error
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	if reason != "" {
		comment := &github.IssueComment{
			Body: github.String(reason),
		}
		_, _, err := ghclient.Issues.CreateComment(ctx, conf.RepoOwner, conf.Repo, *issue.Number, comment)
		if err != nil {
			return err
		}
	}
	issuePatch := &github.IssueRequest{
		State: github.String("closed"),
	}
	ctx, cancel = context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	_, _, err = ghclient.Issues.Edit(ctx, conf.RepoOwner, conf.Repo, *issue.Number, issuePatch)
	return err
}

func openIssue(ghclient *github.Client, conf HecklerdConf, issue *github.Issue, reason string) error {
	var err error
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	if reason != "" {
		comment := &github.IssueComment{
			Body: github.String(reason),
		}
		_, _, err := ghclient.Issues.CreateComment(ctx, conf.RepoOwner, conf.Repo, *issue.Number, comment)
		if err != nil {
			return err
		}
	}
	issuePatch := &github.IssueRequest{
		State: github.String("open"),
	}
	ctx, cancel = context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	_, _, err = ghclient.Issues.Edit(ctx, conf.RepoOwner, conf.Repo, *issue.Number, issuePatch)
	return err
}

func githubCreateApplyFailureIssue(ghclient *github.Client, nodeErrors []error, nodeFile string, conf HecklerdConf, templates *template.Template) (*github.Issue, error) {
	title, body, err := applyFailuresToMarkdown(nodeErrors, nodeFile, conf.EnvPrefix, templates)
	if err != nil {
		return nil, err
	}
	authors, err := nodeOwners(ghclient, nodeFile, conf)
	if err != nil {
		return nil, err
	}
	return githubCreateIssue(ghclient, conf, title, body, authors)
}

func githubCreateCleanFailureIssue(ghclient *github.Client, nodeErrors []error, nodeFile string, conf HecklerdConf, templates *template.Template) (*github.Issue, error) {
	title, body, err := cleanFailuresToMarkdown(nodeErrors, nodeFile, conf.EnvPrefix, templates)
	if err != nil {
		return nil, err
	}
	authors, err := nodeOwners(ghclient, nodeFile, conf)
	if err != nil {
		return nil, err
	}
	return githubCreateIssue(ghclient, conf, title, body, authors)
}

func githubIssueForCleanFailure(ghclient *github.Client, nodeFile string, conf HecklerdConf) (*github.Issue, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	prefix := conf.EnvPrefix
	query := fmt.Sprintf("%s in:title", path.Base(nodeFile))
	if prefix != "" {
		query += fmt.Sprintf(" %sin:title", issuePrefix(prefix))
	}
	query += fmt.Sprintf(" %sin:title", "PuppetCleanFailed")
	query += " state:open"
	query += fmt.Sprintf(" author:app/%s", conf.GitHubAppSlug)
	searchResults, _, err := ghclient.Search.Issues(ctx, query, nil)
	if err != nil {
		return nil, fmt.Errorf("Unable to search GitHub Issues: %w", err)
	}
	if searchResults.GetIncompleteResults() == true {
		return nil, fmt.Errorf("Incomplete results returned from GitHub")
	}
	if searchResults.GetTotal() == 0 {
		return nil, nil
	} else if searchResults.GetTotal() == 1 {
		return &searchResults.Issues[0], nil
	} else {
		return nil, errors.New("More than one issue exists for a single commit")
	}
}

func githubOpenIssues(ghclient *github.Client, conf HecklerdConf, searchTerm string) ([]github.Issue, error) {
	if searchTerm == "" {
		return nil, fmt.Errorf("Empty searchTerm provided")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	prefix := conf.EnvPrefix
	query := "state:open"
	if prefix != "" {
		query += fmt.Sprintf(" %sin:title", issuePrefix(prefix))
	}
	query += fmt.Sprintf(" %s in:title", searchTerm)
	query += fmt.Sprintf(" author:app/%s", conf.GitHubAppSlug)
	searchResults, _, err := ghclient.Search.Issues(ctx, query, nil)
	if err != nil {
		return nil, err
	}
	if searchResults.GetIncompleteResults() == true {
		return nil, fmt.Errorf("Incomplete results returned from GitHub")
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

func clearMilestones(ghclient *github.Client, conf HecklerdConf) error {
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

func createMilestone(milestone string, ghclient *github.Client, conf HecklerdConf) (*github.Milestone, error) {
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

func closeMilestone(milestone string, ghclient *github.Client, conf HecklerdConf) error {
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

func milestoneFromTag(milestone string, ghclient *github.Client, conf HecklerdConf) (*github.Milestone, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	milestoneOpts := &github.MilestoneListOptions{
		State:       "all",
		ListOptions: github.ListOptions{PerPage: 100},
	}
	var allMilestones []*github.Milestone
	for {
		milestones, resp, err := ghclient.Issues.ListMilestones(ctx, conf.RepoOwner, conf.Repo, milestoneOpts)
		if err != nil {
			return nil, err
		}
		allMilestones = append(allMilestones, milestones...)
		if resp.NextPage == 0 {
			break
		}
		milestoneOpts.Page = resp.NextPage
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
func githubIssueFromCommit(ghclient *github.Client, oid git.Oid, conf HecklerdConf) (*github.Issue, error) {
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
	if searchResults.GetIncompleteResults() == true {
		return nil, fmt.Errorf("Incomplete results returned from GitHub")
	}
	if searchResults.GetTotal() == 0 {
		return nil, nil
	} else if searchResults.GetTotal() == 1 {
		return &searchResults.Issues[0], nil
	} else {
		return nil, errors.New("More than one issue exists for a single commit")
	}
}

// Given a git oid, search for the associated GitHub issues, then delete any
// duplicates, keeping the earliest issue by creation date.
func githubIssueDeleteDups(ghclient *github.Client, oid git.Oid, conf HecklerdConf) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	prefix := conf.EnvPrefix
	query := fmt.Sprintf("%s in:title", oid.String())
	if prefix != "" {
		query += fmt.Sprintf(" %sin:title", issuePrefix(prefix))
	}
	query += fmt.Sprintf(" author:app/%s", conf.GitHubAppSlug)
	searchOpts := &github.SearchOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}
	var allIssues []github.Issue
	for {
		searchResults, resp, err := ghclient.Search.Issues(ctx, query, searchOpts)
		if err != nil {
			return fmt.Errorf("Unable to search GitHub Issues: %w", err)
		}
		if searchResults.GetIncompleteResults() == true {
			return fmt.Errorf("Incomplete results returned from GitHub")
		}
		allIssues = append(allIssues, searchResults.Issues...)
		if resp.NextPage == 0 {
			break
		}
		searchOpts.Page = resp.NextPage
	}
	if len(allIssues) > 1 {
		earliest := allIssues[0]
		for _, issue := range allIssues {
			if issue.GetCreatedAt().Before(earliest.GetCreatedAt()) {
				earliest = issue
			}
		}
		for _, issue := range allIssues {
			if issue.GetNumber() == earliest.GetNumber() {
				continue
			}
			err := deleteIssue(ghclient, conf, &issue)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func githubCreateCommitIssue(ghclient *github.Client, conf HecklerdConf, commit *git.Commit, gr groupedReport, templates *template.Template) (*github.Issue, error) {
	// Only assign commit issues to their authors if the noop generated changes
	// or eval errors, i.e. don't assign authors to empty noops so we avoid
	// notifying authors for commits which have no effect or have already been
	// applied.
	var err error
	var authors []string
	if noopRequiresApproval(gr) {
		authors, err = commitAuthorsLogins(ghclient, commit)
		if err != nil {
			return nil, err
		}
		authors, err = filterSuspendedUsers(ghclient, authors)
		if err != nil {
			return nil, err
		}
		// If we can't determine the commit authors, assign the issue to the
		// admins as a fallback
		if len(authors) == 0 {
			authors = append(authors, conf.AdminOwners...)
			authors, err = filterSuspendedUsers(ghclient, authors)
			if err != nil {
				return nil, err
			}
		}
	}

	title, body, err := noopToMarkdown(conf, commit, gr, templates)
	if err != nil {
		return nil, err
	}
	return githubCreateIssue(ghclient, conf, title, body, authors)
}

func githubCreateIssue(ghclient *github.Client, conf HecklerdConf, title, body string, authors []string) (*github.Issue, error) {
	if conf.GitHubDisableNotifications {
		strippedAuthors := strings.Join(stripAtSignsSlice(authors), ", ")
		body = fmt.Sprintf("*Notifications Disabled to Assignees:* %s\n\n", strippedAuthors) + body
	}
	// GitHub has a max issue body size of 65536
	if len(body) >= 65536 {
		notice := fmt.Sprintf("## Noop Output Trimmed!\n\n")
		notice += fmt.Sprintf("Output has been trimmed because it is too long for the GitHub issue\n\n")
		body = notice + body
		runeBody := []rune(body)
		// trim to less then the max, just to be sure it will fit
		trimmedRunes := runeBody[0:65000]
		body = string(trimmedRunes)
	}
	githubIssue := &github.IssueRequest{
		Title: github.String(title),
		Body:  github.String(body),
	}
	// Assignees must not be prefixed with '@'
	trimmedAuthors := make([]string, len(authors))
	for i, author := range authors {
		trimmedAuthors[i] = strings.TrimPrefix(author, "@")
	}
	if !conf.GitHubDisableNotifications {
		githubIssue.Assignees = &trimmedAuthors
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
	if githubUser != nil {
		authors = append(authors, "@"+githubUser.GetLogin())
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
		if githubUser != nil {
			authors = append(authors, "@"+githubUser.GetLogin())
		}
		// TODO: handle nil case?
	}
	return authors, nil
}

func githubIssueComment(ghclient *github.Client, conf HecklerdConf, issue *github.Issue, msg string) error {
	var err error
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	comment := &github.IssueComment{
		Body: github.String(msg),
	}
	_, _, err = ghclient.Issues.CreateComment(ctx, conf.RepoOwner, conf.Repo, issue.GetNumber(), comment)
	return err
}

func createTag(newTag string, conf HecklerdConf, ghclient *github.Client, repo *git.Repository) error {
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

