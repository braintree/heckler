package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"

	"github.com/bradleyfalzon/ghinstallation"
	"github.com/google/go-github/github"
	git "github.com/libgit2/git2go"
)

const GitHubEnterpriseURL = "https://github.braintreeps.com/api/v3"

func clone(remoteUrl string, cloneDir string, cloneOptions *git.CloneOptions) (*git.Repository, error) {
	var repo *git.Repository
	var err error

	if _, err = os.Stat(path.Join(cloneDir, ".git")); os.IsNotExist(err) {
		fmt.Println("cloning")
		repo, err = git.Clone(remoteUrl, cloneDir, cloneOptions)
		if err != nil {
			return nil, err
		}
	} else {
		repo, err = git.OpenRepository(cloneDir)
		if err != nil {
			return nil, err
		}
	}
	err = repo.Remotes.SetUrl("origin", remoteUrl)
	if err != nil {
		return nil, err
	}
	return repo, nil
}

func fastForward(repo *git.Repository) error {
	remote, err := repo.Remotes.Lookup("origin")
	if err != nil {
		return err
	}

	// XXX only fetch specific branch?
	err = remote.Fetch([]string{}, nil, "")
	if err != nil {
		return err
	}

	// open master branch of remote
	remoteBranch, err := repo.References.Lookup("refs/remotes/origin/master")
	if err != nil {
		return err
	}

	// grab commit from head of branch, use annotated variety which
	// includes what ref it was looked up from
	annotatedCommit, err := repo.AnnotatedCommitFromRef(remoteBranch)
	if err != nil {
		return err
	}

	// Do the merge analysis
	mergeHeads := make([]*git.AnnotatedCommit, 1)
	mergeHeads[0] = annotatedCommit
	analysis, _, err := repo.MergeAnalysis(mergeHeads)
	if err != nil {
		return err
	}
	remoteBranchID := remoteBranch.Target()
	remoteBranchObj, err := repo.Lookup(remoteBranchID)
	if err != nil {
		return err
	}

	remoteBranchCommit, err := remoteBranchObj.AsCommit()
	if err != nil {
		return err
	}

	if (analysis & git.MergeAnalysisUpToDate) != 0 {
		fmt.Println("Already up to date")
		return nil
	}

	if (analysis & git.MergeAnalysisFastForward) == 0 {
		fmt.Println("Not a fast forward, bailing")
		return errors.New("Not a fast forward, bailing")
	}

	fmt.Printf("Fast forward...")
	// Fast-forward changes
	// Get remote tree
	remoteTree, err := repo.LookupTree(remoteBranchCommit.TreeId())
	if err != nil {
		return err
	}

	// Checkout
	checkoutOpts := git.CheckoutOpts{
		Strategy: git.CheckoutSafe,
	}
	err = repo.CheckoutTree(remoteTree, &checkoutOpts)
	if err != nil {
		return err
	}

	branchRef, err := repo.References.Lookup("refs/heads/master")
	if err != nil {
		return err
	}

	// Point branch to the same object as the remote branch
	_, err = branchRef.SetTarget(remoteBranchID, "")
	if err != nil {
		return err
	}

	fmt.Println("Success")
	return nil
}

func walk(repo *git.Repository) error {
	var err error

	rv, err := repo.Walk()
	if err != nil {
		return err
	}
	err = rv.PushRange("HEAD~10..HEAD")
	if err != nil {
		return err
	}

	var gi git.Oid
	for rv.Next(&gi) == nil {
		fmt.Printf("%v\n", gi.String())
	}

	return nil
}

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

	cloneDir := "/home/admin/tmp/muppetshow"
	cloneOptions := &git.CloneOptions{}
	remoteUrl := fmt.Sprintf("https://x-access-token:%s@github.braintreeps.com/lollipopman/muppetshow", tok)
	repo, err := clone(remoteUrl, cloneDir, cloneOptions)
	if err != nil {
		log.Fatal(err)
	}
	err = fastForward(repo)
	if err != nil {
		log.Fatal(err)
	}
	err = walk(repo)
	if err != nil {
		log.Fatal(err)
	}
}
