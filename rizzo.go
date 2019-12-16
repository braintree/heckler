package main

import (
	"log"

	git "github.com/libgit2/git2go"
)

func main() {
	// log.SetFlags(log.LstdFlags | log.Lshortfile)
	//
	// // Shared transport to reuse TCP connections.
	// tr := http.DefaultTransport
	//
	// // Wrap the shared transport for use with the app ID 7 authenticating with
	// // installation ID 11.
	// itr, err := ghinstallation.NewKeyFromFile(tr, 7, 11, "heckler.2019-10-30.private-key.pem")
	// if err != nil {
	// 	log.Fatal(err)
	// }
	// itr.BaseURL = GitHubEnterpriseURL
	//
	// // Use installation transport with github.com/google/go-github
	// _, err = github.NewEnterpriseClient(GitHubEnterpriseURL, GitHubEnterpriseURL, &http.Client{Transport: itr})
	// if err != nil {
	// 	log.Fatal(err)
	// }
	// ctx := context.Background()
	// tok, err := itr.Token(ctx)
	// if err != nil {
	// 	log.Fatal(err)
	// }
	//
	// cloneDir := "/home/admin/tmp/muppetshow"
	// cloneOptions := &git.CloneOptions{}
	// remoteUrl := fmt.Sprintf("https://x-access-token:%s@github.braintreeps.com/lollipopman/muppetshow", tok)
	// repo, err := gitutil.Clone(remoteUrl, cloneDir, cloneOptions)
	// if err != nil {
	// 	log.Fatal(err)
	// }
	// err = gitutil.FastForward(repo)
	// if err != nil {
	// 	log.Fatal(err)
	// }
	// err = gitutil.Walk(repo)
	// if err != nil {
	// 	log.Fatal(err)
	// }
	cloneOptions := &git.CloneOptions{}
	// use FetchOptions instead of directly RemoteCallbacks
	// https://github.com/libgit2/git2go/commit/36e0a256fe79f87447bb730fda53e5cbc90eb47c
	cloneOptions.FetchOptions = &git.FetchOptions{
		RemoteCallbacks: git.RemoteCallbacks{
			CredentialsCallback:      makeCredentialsCallback(),
			CertificateCheckCallback: certificateCheckCallback,
		},
	}

	_, err := git.Clone("root@heckler:/data/muppetshow", "/data/muppetshow", cloneOptions)
	if err != nil {
		log.Panic(err)
	}
	log.Print("SUCESS")
}

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
		// if allowedTypes&git.CredTypeSshKey != 0 {
		errCode, cred := git.NewCredSshKey(username, homeDir+"/.ssh/id_ecdsa.pub", homeDir+"/.ssh/id_ecdsa", "")
		return git.ErrorCode(errCode), &cred
	}
}

// Made this one just return 0 during troubleshooting...
func certificateCheckCallback(cert *git.Certificate, valid bool, hostname string) git.ErrorCode {
	return git.ErrOk
}
