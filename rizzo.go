package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"

	"./gitutil"
	git "github.com/libgit2/git2go"
)

func makeCredentialsCallback() git.CredentialsCallback {
	called := false
	return func(url string, username string, allowedTypes git.CredType) (git.ErrorCode, *git.Cred) {
		// libssh2 calls the credentials callback in a loop until successful, but
		// nothing will change by calling this function again, so just return an
		// error.
		if called {
			return git.ErrUser, nil
		}
		called = true
		// homeDir := os.UserHomeDir() need golang 1.12
		homeDir := "/root"
		// XXX do we need to check git.CredType? allowedTypes&git.CredTypeSshKey != 0
		errCode, cred := git.NewCredSshKey(username, homeDir+"/.ssh/id_ecdsa.pub", homeDir+"/.ssh/id_ecdsa", "")
		return git.ErrorCode(errCode), &cred
	}
}

// Made this one just return 0 during troubleshooting...
func certificateCheckCallback(cert *git.Certificate, valid bool, hostname string) git.ErrorCode {
	// XXX implement host key checking
	return git.ErrOk
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	var err error

	cloneOptions := &git.CloneOptions{}
	cloneOptions.FetchOptions = &git.FetchOptions{
		RemoteCallbacks: git.RemoteCallbacks{
			CredentialsCallback:      makeCredentialsCallback(),
			CertificateCheckCallback: certificateCheckCallback,
		},
	}

	repo, err := gitutil.CloneOrOpen("root@heckler:/data/muppetshow", "/data/muppetshow", cloneOptions)
	if err != nil {
		log.Panic(err)
	}
	err = gitutil.FastForward(repo, cloneOptions.FetchOptions)
	if err != nil {
		log.Fatal(err)
	}

	rv, err := repo.Walk()
	if err != nil {
		log.Fatal(err)
	}
	rv.Sorting(git.SortTopological)
	err = rv.PushHead()
	if err != nil {
		log.Fatal(err)
	}
	err = rv.HideRef("refs/tags/v1")
	if err != nil {
		log.Fatal(err)
	}

	var gi git.Oid
	for rv.Next(&gi) == nil {
		fmt.Printf("Commit: %v\n", gi.String())
		commit, err := repo.LookupCommit(&gi)
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
			log.Fatal(err)
		}
		index, err := repo.Index()
		if err != nil {
			log.Fatal(err)
		}
		err = index.ReadTree(tree)
		if err != nil {
			log.Fatal(err)
		}
		err = index.Write()
		if err != nil {
			log.Fatal(err)
		}
		err = repo.SetHeadDetached(&gi)
		if err != nil {
			log.Fatal(err)
		}

		// puppet
		repoDir := "/data/muppetshow"
		puppetArgs := []string{
			"apply",
			"--noop",
			"--confdir",
			repoDir,
			"--vardir",
			"/var/tmp",
			"--config_version",
			"/heckler/git-head-sha",
			repoDir + "/nodes.pp",
		}
		cmd := exec.Command("puppet", puppetArgs...)
		hostname, err := os.Hostname()
		if err != nil {
			log.Fatal(err)
		}
		cmd.Env = append(os.Environ(),
			"FACTER_nodename="+hostname,
			"FACTER_cwd=/root",
			"PUPPET_COMMIT="+gi.String(),
		)
		stdoutStderr, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Printf("%s\n", stdoutStderr)
			log.Fatal(err)
		}
	}
	fmt.Println("Success")
}
