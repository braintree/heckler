package gitutil

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	git "github.com/libgit2/git2go"
)

func CloneOrOpen(remoteUrl string, cloneDir string, cloneOptions *git.CloneOptions) (*git.Repository, error) {
	var repo *git.Repository
	var err error

	if _, err = os.Stat(cloneDir); os.IsNotExist(err) {
		repo, err = git.Clone(remoteUrl, cloneDir, cloneOptions)
		if err != nil {
			// A failed initial clone sometimes leaves the respository in a state
			// that is difficult to recover from, so rather then solving those corner
			// cases, just remove the failed clone directory
			os.RemoveAll(cloneDir)
			return nil, fmt.Errorf("Clone failed: %w", err)
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

func FastForward(repo *git.Repository, fetchOptions *git.FetchOptions) error {
	var err error

	remote, err := repo.Remotes.Lookup("origin")
	if err != nil {
		return err
	}

	// XXX only fetch specific branch?
	err = remote.Fetch([]string{}, fetchOptions, "")
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

	if (analysis & git.MergeAnalysisUpToDate) != 0 {
		return nil
	}

	if (analysis & git.MergeAnalysisFastForward) == 0 {
		return errors.New("Not a fast forward, bailing")
	}

	branchRef, err := repo.References.Lookup("refs/heads/master")
	if err != nil {
		return err
	}

	remoteBranchID := remoteBranch.Target()
	// Point branch to the same object as the remote branch
	_, err = branchRef.SetTarget(remoteBranchID, "")
	if err != nil {
		return err
	}

	if repo.IsBare() == false {
		remoteBranchCommit, err := repo.LookupCommit(remoteBranchID)
		if err != nil {
			return err
		}
		// Get remote tree to checkout
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
		index, err := repo.Index()
		if err != nil {
			return err
		}
		err = index.ReadTree(remoteTree)
		if err != nil {
			return err
		}
		err = index.Write()
		if err != nil {
			return err
		}
	}

	err = repo.SetHead("refs/heads/master")
	if err != nil {
		return err
	}

	return nil
}

func Walk(repo *git.Repository) error {
	var err error

	rv, err := repo.Walk()
	rv.Sorting(git.SortTopological)
	if err != nil {
		return err
	}
	err = rv.PushRef("refs/tags/v2")
	err = rv.HideRef("refs/tags/v1")
	if err != nil {
		return err
	}

	var gi git.Oid
	for rv.Next(&gi) == nil {
		fmt.Printf("%v\n", gi.String())
	}

	return nil
}

func Pull(url string, destDir string) (*git.Repository, error) {
	cloneOptions := &git.CloneOptions{
		FetchOptions: &git.FetchOptions{
			UpdateFetchhead: true,
			DownloadTags:    git.DownloadTagsAll,
		},
	}
	err := os.MkdirAll(filepath.Dir(destDir), 0775)
	if err != nil {
		return nil, err
	}
	repo, err := CloneOrOpen(url, destDir, cloneOptions)
	if err != nil {
		return nil, fmt.Errorf("CloneOrOpen failed: %w", err)
	}
	err = FastForward(repo, cloneOptions.FetchOptions)
	if err != nil {
		return nil, fmt.Errorf("FastForward failed: %w", err)
	}
	return repo, nil
}

func PullBranch(url string, branch string, destDir string) (*git.Repository, error) {
	cloneOptions := &git.CloneOptions{
		FetchOptions: &git.FetchOptions{
			UpdateFetchhead: true,
			DownloadTags:    git.DownloadTagsAll,
		},
	}
	err := os.MkdirAll(filepath.Dir(destDir), 0775)
	if err != nil {
		return nil, err
	}
	repo, err := CloneOrOpen(url, destDir, cloneOptions)
	if err != nil {
		return nil, fmt.Errorf("CloneOrOpen failed: %w", err)
	}
	_, err = Checkout(branch, repo)
	if err != nil {
		return nil, fmt.Errorf("Checkout failed: %w", err)
	}
	err = FastForward(repo, cloneOptions.FetchOptions)
	if err != nil {
		return nil, fmt.Errorf("FastForward failed: %w", err)
	}
	return repo, nil
}

func RevparseToCommit(rev string, repo *git.Repository) (*git.Commit, error) {
	obj, err := repo.RevparseSingle(rev)
	if err != nil {
		return nil, err
	}

	// Annotated tags are separate git objects, so we need to find the target
	// commit object of the tag.
	if obj.Type() == git.ObjectTag {
		tagObj, err := obj.AsTag()
		if err != nil {
			return nil, err
		}
		obj = tagObj.Target()
	}

	commit, err := obj.AsCommit()
	if err != nil {
		return nil, err
	}
	return commit, nil
}

func Checkout(rev string, repo *git.Repository) (string, error) {
	var err error

	commit, err := RevparseToCommit(rev, repo)
	if err != nil {
		log.Fatal(err)
	}

	tree, err := repo.LookupTree(commit.TreeId())
	if err != nil {
		log.Fatal(err)
	}

	checkoutOpts := git.CheckoutOpts{
		Strategy: git.CheckoutSafe,
	}
	err = repo.CheckoutTree(tree, &checkoutOpts)
	if err != nil {
		return "", err
	}
	index, err := repo.Index()
	if err != nil {
		return "", err
	}
	err = index.ReadTree(tree)
	if err != nil {
		return "", err
	}
	err = index.Write()
	if err != nil {
		return "", err
	}
	commitId := commit.Id()
	err = repo.SetHeadDetached(commitId)
	if err != nil {
		return "", err
	}

	return commitId.String(), nil
}
