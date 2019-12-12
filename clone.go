package main

import (
	"context"
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

	localRepo := "/home/admin/tmp/muppetshow"

	var repo *git.Repository

	remoteUrl := fmt.Sprintf("https://x-access-token:%s@github.braintreeps.com/lollipopman/muppetshow", tok)
	if _, err = os.Stat(path.Join(localRepo, ".git")); os.IsNotExist(err) {
		fmt.Println("cloning")
		cloneOptions := &git.CloneOptions{}
		repo, err = git.Clone(remoteUrl, localRepo, cloneOptions)
		if err != nil {
			log.Fatal(err)
		}
	} else {
		repo, err = git.OpenRepository(localRepo)
		if err != nil {
			log.Fatal(err)
		}
	}

	err = repo.Remotes.SetUrl("origin", remoteUrl)
	if err != nil {
		log.Fatal(err)
	}

	remote, err := repo.Remotes.Lookup("origin")
	if err != nil {
		log.Fatal(err)
	}

	// XXX only fetch specific branch?
	err = remote.Fetch([]string{}, nil, "")
	if err != nil {
		log.Fatal(err)
	}
	remoteBranch, err := repo.References.Lookup("refs/remotes/origin/master")
	if err != nil {
		log.Fatal(err)
	}
	annotatedCommit, err := repo.AnnotatedCommitFromRef(remoteBranch)
	if err != nil {
		log.Fatal(err)
	}

	// Do the merge analysis
	mergeHeads := make([]*git.AnnotatedCommit, 1)
	mergeHeads[0] = annotatedCommit
	analysis, _, err := repo.MergeAnalysis(mergeHeads)
	if err != nil {
		log.Fatal(err)
	}
	remoteBranchID := remoteBranch.Target()
	remoteBranchObj, err := repo.Lookup(remoteBranchID)
	if err != nil {
		log.Fatal(err)
	}

	remoteBranchCommit, err := remoteBranchObj.AsCommit()
	if err != nil {
		log.Fatal(err)
	}

	if analysis&git.MergeAnalysisUpToDate != 0 {
		fmt.Println("Up to date")
		os.Exit(0)
	} else if analysis&git.MergeAnalysisFastForward != 0 {
		fmt.Println("fast forward")
		// Fast-forward changes
		// Get remote tree
		remoteTree, err := repo.LookupTree(remoteBranchCommit.TreeId())
		if err != nil {
			log.Fatal(err)
		}

		// Checkout
		checkoutOpts := git.CheckoutOpts{
			Strategy: git.CheckoutSafe,
		}
		err = repo.CheckoutTree(remoteTree, &checkoutOpts)
		if err != nil {
			log.Fatal(err)
		}

		branchRef, err := repo.References.Lookup("refs/heads/master")
		if err != nil {
			log.Fatal(err)
		}

		// Point branch to the object
		_, err = branchRef.SetTarget(remoteBranchID, "")
		if err != nil {
			log.Fatal(err)
		}

		// if _, err := head.SetTarget(remoteBranchID, ""); err != nil {
		// 	log.Fatal(err)
		// }

		fmt.Println("Success")
	} else {
		fmt.Println("not up to date")
		os.Exit(0)
	}

}
