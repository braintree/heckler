package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"github.braintreeps.com/lollipopman/heckler/internal/gitutil"
	"github.braintreeps.com/lollipopman/heckler/internal/rizzopb"
	"github.com/lollipopman/luser"
	"gopkg.in/yaml.v3"

	"google.golang.org/grpc"
)

const (
	port     = ":50051"
	stateDir = "/var/lib/rizzo"
	repoDir  = stateDir + "/repo/puppetcode"
	lockPath = "/var/tmp/puppet.lock"
)

// server is used to implement rizzo.RizzoServer.
type server struct {
	rizzopb.UnimplementedRizzoServer
	conf *RizzoConf
}

// PuppetApply implements rizzo.RizzoServer
func (s *server) PuppetApply(ctx context.Context, req *rizzopb.PuppetApplyRequest) (*rizzopb.PuppetReport, error) {
	var err error
	var oid string

	log.Printf("Received: %v", req.Rev)

	// pull
	repo, err := gitutil.Pull("http://"+s.conf.HecklerHost+":8080/puppetcode", repoDir)
	if err != nil {
		log.Printf("Pull error: %v", err)
		return &rizzopb.PuppetReport{}, err
	}
	log.Printf("Pull Complete: %v", req.Rev)

	// checkout
	oid, err = gitutil.Checkout(req.Rev, repo)
	if err != nil {
		log.Printf("Checkout error: %v", err)
		return &rizzopb.PuppetReport{}, err
	}
	log.Printf("Checkout Complete: %v", oid)

	// apply
	pr, err := puppetApply(oid, req.Noop, s.conf)
	if err != nil {
		log.Printf("Apply error: %v", err)
		return &rizzopb.PuppetReport{}, err
	}
	log.Printf("Done: %v", req.Rev)
	return pr, nil
}

// PuppetLastApply implements rizzo.RizzoServer
func (s *server) PuppetLastApply(ctx context.Context, req *rizzopb.PuppetLastApplyRequest) (*rizzopb.PuppetReport, error) {
	var err error

	log.Printf("PuppetLastApply: request received")
	file, err := os.Open(s.conf.PuppetReportDir + "/heckler/heckler_last_apply.json")
	if err != nil {
		return &rizzopb.PuppetReport{}, err
	}
	defer file.Close()
	data, err := ioutil.ReadAll(file)
	if err != nil {
		return &rizzopb.PuppetReport{}, err
	}
	pr := new(rizzopb.PuppetReport)
	err = json.Unmarshal([]byte(data), pr)
	if err != nil {
		return &rizzopb.PuppetReport{}, err
	}
	log.Printf("PuppetLastApply: status@%s", pr.ConfigurationVersion)
	return pr, nil
}

func (s *server) PuppetLock(ctx context.Context, req *rizzopb.PuppetLockRequest) (*rizzopb.PuppetLockReport, error) {
	log.Printf("PuppetLock: request received, %v", req)
	var res *rizzopb.PuppetLockReport
	res = new(rizzopb.PuppetLockReport)
	err := puppetLock(req.User, req.Comment, req.Force)
	if err != nil {
		res.Locked = false
		res.Error = err.Error()
	} else {
		res.Locked = true
	}
	log.Printf("PuppetLock: reply, %v", res)
	return res, nil
}

func puppetLock(locker string, comment string, forceLock bool) error {
	tmpfile, err := ioutil.TempFile("/var/tmp", "rizzoPuppetLockTmpFile.*")
	if err != nil {
		return err
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.WriteString(comment); err != nil {
		return err
	}
	if err := tmpfile.Close(); err != nil {
		return err
	}

	lu, err := luser.Lookup(locker)
	if err != nil {
		return err
	}

	uid, err := strconv.Atoi(lu.Uid)
	if err != nil {
		return err
	}
	gid, err := strconv.Atoi(lu.Gid)
	if err != nil {
		return err
	}
	// Chown lock file so it is owned by locker user
	os.Chown(tmpfile.Name(), uid, gid)

	if forceLock {
		return os.Rename(tmpfile.Name(), lockPath)
	}

	err = renameAndCheck(tmpfile.Name(), lockPath)
	if errors.Is(err, os.ErrExist) {
		buf, err := ioutil.ReadFile(lockPath)
		if err != nil {
			return err
		}
		comment := strings.TrimRight(string(buf), "\n")
		lockOwner, err := lockOwner()
		if err != nil {
			return err
		}
		return errors.New(fmt.Sprintf("Already locked by %s, '%s'", lockOwner.Username, comment))
	}
	return err
}

func lockOwner() (*luser.User, error) {
	var stat syscall.Stat_t
	if err := syscall.Stat(lockPath, &stat); err != nil {
		return nil, err
	}
	u, err := luser.LookupId(strconv.Itoa(int(stat.Uid)))
	if err != nil {
		return nil, err
	}
	return u, nil
}

// os.Rename overwrites, which we do not want to do, but hard linking returns
// the correct error.
func renameAndCheck(src, dst string) error {
	err := os.Link(src, dst)
	if err != nil {
		return err
	}

	return os.Remove(src)
}

func (s *server) PuppetUnlock(ctx context.Context, req *rizzopb.PuppetUnlockRequest) (*rizzopb.PuppetUnlockReport, error) {
	log.Printf("PuppetUnlock: request received, %v", req)
	var res *rizzopb.PuppetUnlockReport
	res = new(rizzopb.PuppetUnlockReport)
	err := puppetUnlock(req.User, req.Force)
	if err != nil {
		res.Unlocked = false
		res.Error = err.Error()
	} else {
		res.Unlocked = true
	}
	log.Printf("PuppetUnlock: reply, %v", res)
	return res, nil
}

func puppetUnlock(unlocker string, forceUnlock bool) error {
	// if we are already unlocked don't return an error
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		return nil
	}
	lockOwner, err := lockOwner()
	if err != nil {
		return err
	}
	if unlocker == lockOwner.Username || forceUnlock {
		err = os.Remove(lockPath)
		if err != nil {
			return err
		}
		return nil
	} else {
		comment, err := lockComment()
		if err != nil {
			return err
		}
		return errors.New(fmt.Sprintf("Already locked by %s, '%s'", lockOwner.Username, comment))
	}
}

func lockComment() (string, error) {
	buf, err := ioutil.ReadFile(lockPath)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(buf), "\n"), nil
}

func puppetApply(oid string, noop bool, conf *RizzoConf) (*rizzopb.PuppetReport, error) {
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
	cmd.Dir = repoDir
	env := os.Environ()
	for k, v := range conf.Env {
		env = append(env, k+"="+v)
	}
	cmd.Env = env
	stdoutStderr, err := cmd.CombinedOutput()
	log.Printf("%s", stdoutStderr)
	if err != nil {
		return &rizzopb.PuppetReport{}, err
	}
	if oldPath != "" {
		os.Setenv("PATH", oldPath)
	}
	file, err := os.Open(conf.PuppetReportDir + "/heckler/heckler_" + oid + ".json")
	if err != nil {
		return &rizzopb.PuppetReport{}, err
	}
	defer file.Close()
	data, err := ioutil.ReadAll(file)
	if err != nil {
		return &rizzopb.PuppetReport{}, err
	}
	pr := new(rizzopb.PuppetReport)
	err = json.Unmarshal([]byte(data), pr)
	if err != nil {
		return &rizzopb.PuppetReport{}, err
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
	HecklerHost     string `yaml:"heckler_host"`
}

func main() {
	// add filename and linenumber to log output
	log.SetFlags(log.Lshortfile)
	var err error
	var rizzoConfPath string
	var file *os.File
	var data []byte
	var rizzoConf *RizzoConf
	var clearState bool

	flag.BoolVar(&clearState, "clear", false, "Clear local state, e.g. puppet code repo")
	flag.Parse()

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

	if clearState {
		log.Printf("Removing state directory: %v", stateDir)
		os.RemoveAll(stateDir)
		os.Exit(0)
	}

	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	server := new(server)
	server.conf = rizzoConf
	rizzopb.RegisterRizzoServer(s, server)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
