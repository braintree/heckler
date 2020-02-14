package main

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"

	"github.braintreeps.com/lollipopman/heckler/gitutil"
	"github.braintreeps.com/lollipopman/heckler/puppetutil"
	"gopkg.in/yaml.v3"

	"google.golang.org/grpc"
)

const (
	port = ":50051"
)

// server is used to implement rizzo.RizzoServer.
type server struct {
	puppetutil.UnimplementedRizzoServer
	conf *RizzoConf
}

// PuppetApply implements rizzo.RizzoServer
func (s *server) PuppetApply(ctx context.Context, req *puppetutil.PuppetApplyRequest) (*puppetutil.PuppetReport, error) {
	var err error
	var oid string

	log.Printf("Received: %v", req.Rev)

	// pull
	repo, err := gitutil.Pull("http://heckler:8080/puppetcode", "/var/lib/rizzo/repo/puppetcode")
	if err != nil {
		log.Printf("Pull error: %v", err)
		return &puppetutil.PuppetReport{}, err
	}
	log.Printf("Pull Complete: %v", req.Rev)

	// checkout
	oid, err = gitutil.Checkout(req.Rev, repo)
	if err != nil {
		log.Printf("Checkout error: %v", err)
		return &puppetutil.PuppetReport{}, err
	}
	log.Printf("Checkout Complete: %v", oid)

	// apply
	pr, err := puppetApply(oid, req.Noop, s.conf)
	if err != nil {
		log.Printf("Apply error: %v", err)
		return &puppetutil.PuppetReport{}, err
	}
	log.Printf("Done: %v", req.Rev)
	return pr, nil
}

// PuppetLastApply implements rizzo.RizzoServer
func (s *server) PuppetLastApply(ctx context.Context, req *puppetutil.PuppetLastApplyRequest) (*puppetutil.PuppetReport, error) {
	var err error

	log.Printf("PuppetLastApply: request received")
	file, err := os.Open(s.conf.PuppetReportDir + "/heckler/heckler_last_apply.json")
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

func puppetApply(oid string, noop bool, conf *RizzoConf) (*puppetutil.PuppetReport, error) {
	var oldPath string

	if noop {
		log.Printf("Nooping: %v", oid)
	} else {
		log.Printf("Applying: %v", oid)
	}
	puppetArgs := make([]string, len(conf.PuppetCmd.Args))
	copy(puppetArgs, conf.PuppetCmd.Args)
	if noop {
		puppetArgs = append(puppetArgs, "--noop")
	}
	if path, ok := conf.Env["PATH"]; ok {
		oldPath = os.Getenv("PATH")
		os.Setenv("PATH", path)
	}
	cmd := exec.Command("puppet", puppetArgs...)
	// Change to code dir, so hiera relative paths resolve
	cmd.Dir = "/var/lib/rizzo/repo/puppetcode"
	env := os.Environ()
	for k, v := range conf.Env {
		env = append(env, k+"="+v)
	}
	cmd.Env = env
	stdoutStderr, err := cmd.CombinedOutput()
	log.Printf("%s", stdoutStderr)
	if err != nil {
		return &puppetutil.PuppetReport{}, err
	}
	if oldPath != "" {
		os.Setenv("PATH", oldPath)
	}
	file, err := os.Open(conf.PuppetReportDir + "/heckler/heckler_" + oid + ".json")
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

type PuppetCmd struct {
	Env  map[string]string `yaml:env`
	Args []string          `yaml:args`
}

type RizzoConf struct {
	PuppetCmd       `yaml:"puppet_cmd"`
	PuppetReportDir string `yaml:"puppet_reportdir"`
}

func main() {
	var err error
	var rizzoConfPath string
	var file *os.File
	var data []byte
	var rizzoConf *RizzoConf

	if _, err := os.Stat("/etc/rizzo/rizzo_conf.yaml"); err == nil {
		rizzoConfPath = "/etc/rizzo/rizzo_conf.yaml"
	} else if _, err := os.Stat("rizzo_conf.yaml"); err == nil {
		rizzoConfPath = "rizzo_conf.yaml"
	} else {
		log.Fatal("Unable to load rizzo_conf.yaml from /etc/rizzo or .")
	}
	file, err = os.Open(rizzoConfPath)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	data, err = ioutil.ReadAll(file)
	if err != nil {
		log.Fatalf("Cannot read config: %v", err)
	}
	rizzoConf = new(RizzoConf)
	err = yaml.Unmarshal([]byte(data), rizzoConf)
	if err != nil {
		log.Fatalf("Cannot unmarshal config: %v", err)
	}

	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	server := new(server)
	server.conf = rizzoConf
	puppetutil.RegisterRizzoServer(s, server)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
