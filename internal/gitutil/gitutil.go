package gitutil

import (
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	git "github.com/libgit2/git2go/v31"
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
	// If the repo is empty, or returns an error, we assume it was a bad clone,
	// so delete the cloned directory.
	empty, err := repo.IsEmpty()
	if err != nil {
		os.RemoveAll(cloneDir)
		return nil, err
	}
	if empty {
		os.RemoveAll(cloneDir)
		return nil, errors.New("Repo is empty, removing repo dir!")
	}
	err = repo.Remotes.SetUrl("origin", remoteUrl)
	if err != nil {
		return nil, err
	}
	return repo, nil
}

func FastForward(repo *git.Repository, fetchOptions *git.FetchOptions) error {
	var err error

	headRef, err := repo.Head()
	if err != nil {
		return err
	}

	headBranchName, err := headRef.Branch().Name()
	if err != nil {
		return err
	} else if headBranchName == "" {
		return errors.New("HEAD branch name is empty, ''")
	}

	remote, err := repo.Remotes.Lookup("origin")
	if err != nil {
		return err
	}

	err = remote.Fetch([]string{}, fetchOptions, "")
	if err != nil {
		return err
	}

	// Open HEAD branch of remote
	remoteBranch, err := repo.References.Lookup("refs/remotes/origin/" + headBranchName)
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

	// Repo is up to date
	if (analysis & git.MergeAnalysisUpToDate) != 0 {
		return nil
	}

	if (analysis & git.MergeAnalysisFastForward) == 0 {
		return errors.New("Not a fast forward, bailing")
	}

	remoteBranchID := remoteBranch.Target()
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
	}

	branchRef, err := repo.References.Lookup("refs/heads/" + headBranchName)
	if err != nil {
		return err
	}

	// Point branch to the same object as the remote branch
	_, err = branchRef.SetTarget(remoteBranchID, "")
	if err != nil {
		return err
	}

	err = repo.SetHead("refs/heads/" + headBranchName)
	if err != nil {
		return err
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

	// Annotated tags are separate git objects, so we need to peel the tag until
	// we get to the target commit object of the tag.
	if obj.Type() == git.ObjectTag {
		obj, err = obj.Peel(git.ObjectCommit)
		if err != nil {
			return nil, err
		}
	}

	commit, err := obj.AsCommit()
	if err != nil {
		return nil, err
	}
	return commit, nil
}

func Checkout(rev string, repo *git.Repository) (string, error) {
	var err error
	var branchRef bool

	ref, err := repo.References.Dwim(rev)
	if err == nil {
		branchRef = ref.IsBranch()
	}

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
	if branchRef {
		err = repo.SetHead(ref.Name())
		if err != nil {
			return "", err
		}
	} else {
		err = repo.SetHeadDetached(commitId)
		if err != nil {
			return "", err
		}
	}
	return commitId.String(), nil
}

// Run git gc to pack the repository, this is necessary because libgit2 has no
// ability at present to perform house keeping tasks
func Gc(repoDir string, logger *log.Logger) error {
	// Spread out run over an hour to avoid VMs on the same host using I/O
	// simultaneously
	sleepSpread, err := rand.Int(rand.Reader, big.NewInt(60))
	if err != nil {
		logger.Printf("Cannot read random number: %v", err)
		return err
	}
	time.Sleep(time.Duration(sleepSpread.Int64()) * time.Minute)
	cmd := exec.Command("git", "-C", repoDir, "gc", "--auto")
	logger.Printf("Running git gc: %v", cmd.Args)
	out, err := cmd.Output()
	if err != nil {
		logger.Printf("Error: git gc failed, %v, '%s'", err, out)
		return err
	}
	logger.Printf("Completed git gc of: '%s'", repoDir)
	return nil
}

// Cleanup a git repo that is in an interim state from a crashed process, or
// remove if unable to cleanup
func ResetRepo(repoDir string, logger *log.Logger) error {
	if _, err := os.Stat(repoDir); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	// If we don't have a git directory, delete our repo
	if _, err := os.Stat(repoDir + "/.git"); os.IsNotExist(err) {
		os.RemoveAll(repoDir)
		return nil
	} else if err != nil {
		return err
	}
	// Remove stale git lock files left by any crashed processes
	gitLockFile := repoDir + "/.git/index.lock"
	if _, err := os.Stat(gitLockFile); err == nil {
		err = os.Remove(gitLockFile)
		if err != nil {
			return err
		}
	}
	// Remove untracked files from interrupted checkout
	gitClean := exec.Command("git", "-C", repoDir, "clean", "-f", "-d", "-q")
	logger.Printf("Running git clean: %v", gitClean.Args)
	cleanOut, err := gitClean.Output()
	if err != nil {
		logger.Printf("Error: git clean failed, %v, '%s'", err, cleanOut)
		return err
	}
	// Reset any modified files from an interrupted checkout
	gitReset := exec.Command("git", "-C", repoDir, "reset", "--hard", "-q")
	logger.Printf("Running git reset: %v", gitReset.Args)
	resetOut, err := gitReset.Output()
	if err != nil {
		logger.Printf("Error: git clean failed, %v, '%s'", err, resetOut)
		return err
	}
	return nil
}
