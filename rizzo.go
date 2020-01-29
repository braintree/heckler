package main

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"

	"./gitutil"
	"./puppetutil"
	git "github.com/libgit2/git2go"

	"google.golang.org/grpc"
)

const (
	port = ":50051"
)

// server is used to implement rizzo.RizzoServer.
type server struct {
	puppetutil.UnimplementedRizzoServer
}

func pull() error {
	cloneOptions := &git.CloneOptions{}
	cloneOptions.FetchOptions = &git.FetchOptions{
		RemoteCallbacks: git.RemoteCallbacks{
			CredentialsCallback:      makeCredentialsCallback(),
			CertificateCheckCallback: certificateCheckCallback,
		},
	}

	repo, err := gitutil.CloneOrOpen("root@heckler:/data/muppetshow", "/data/muppetshow", cloneOptions)
	if err != nil {
		return err
	}
	err = gitutil.FastForward(repo, cloneOptions.FetchOptions)
	if err != nil {
		return err
	}
	return nil
}

func checkout(rev string) (string, error) {
	var err error

	log.Printf("Rev: %v\n", rev)
	repo, err := git.OpenRepository("/data/muppetshow")
	if err != nil {
		return "", err
	}
	obj, err := repo.RevparseSingle(rev)
	if err != nil {
		return "", err
	}

	commit, err := obj.AsCommit()
	if err != nil {
		return "", err
	}

	tree, err := repo.LookupTree(commit.TreeId())
	if err != nil {
		log.Fatal(err)
		return "", err
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
	err = repo.SetHeadDetached(obj.Id())
	if err != nil {
		return "", err
	}

	return (obj.Id()).String(), nil
}

// PuppetApply implements rizzo.RizzoServer
func (s *server) PuppetApply(ctx context.Context, req *puppetutil.PuppetApplyRequest) (*puppetutil.PuppetReport, error) {
	var err error
	var oid string

	log.Printf("Received: %v", req.Rev)
	// pull
	err = pull()
	if err != nil {
		return &puppetutil.PuppetReport{}, err
	}
	log.Printf("Pull Complete: %v", req.Rev)
	// checkout
	oid, err = checkout(req.Rev)
	if err != nil {
		return &puppetutil.PuppetReport{}, err
	}
	log.Printf("Checkout Complete: %v", oid)
	// apply
	log.Printf("Applying: %v", oid)
	pr, err := puppetApply(oid, req.Noop)
	if err != nil {
		return &puppetutil.PuppetReport{}, err
	}
	log.Printf("Done: %v", req.Rev)
	return pr, nil
}

// PuppetLastApply implements rizzo.RizzoServer
func (s *server) PuppetLastApply(ctx context.Context, req *puppetutil.PuppetLastApplyRequest) (*puppetutil.PuppetReport, error) {
	var err error

	log.Printf("PuppetLastApply: request received")
	file, err := os.Open("/var/tmp/reports/heckler/heckler_last_apply.json")
	if err != nil {
		return &puppetutil.PuppetReport{}, err
	}
	defer file.Close()
	data, err := ioutil.ReadAll(file)
	if err != nil {
		return &puppetutil.PuppetReport{}, err
	}
	pr := new(puppetutil.PuppetReport)
	err = json.Unmarshal([]byte(data), pr)
	if err != nil {
		return &puppetutil.PuppetReport{}, err
	}
	log.Printf("PuppetLastApply: status@%s", pr.ConfigurationVersion)
	return pr, nil
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

func puppetApply(oid string, noop bool) (*puppetutil.PuppetReport, error) {
	repoDir := "/data/muppetshow"
	puppetArgs := []string{
		"apply",
		"--confdir",
		repoDir,
		"--vardir",
		"/var/tmp",
		"--config_version",
		"/heckler/git-head-sha",
		repoDir + "/nodes.pp",
	}
	if noop {
		puppetArgs = append(puppetArgs, "--noop")
	}
	cmd := exec.Command("puppet", puppetArgs...)
	cmd.Dir = repoDir
	stdoutStderr, err := cmd.CombinedOutput()
	log.Printf("%s", stdoutStderr)
	if err != nil {
		return &puppetutil.PuppetReport{}, err
	}
	file, err := os.Open("/var/tmp/reports/heckler/heckler_" + oid + ".json")
	if err != nil {
		return &puppetutil.PuppetReport{}, err
	}
	defer file.Close()
	data, err := ioutil.ReadAll(file)
	if err != nil {
		return &puppetutil.PuppetReport{}, err
	}
	pr := new(puppetutil.PuppetReport)
	err = json.Unmarshal([]byte(data), pr)
	if err != nil {
		return &puppetutil.PuppetReport{}, err
	}
	return pr, nil
}

func main() {
	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	puppetutil.RegisterRizzoServer(s, &server{})
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
