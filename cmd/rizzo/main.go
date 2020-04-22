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
	"os/signal"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.braintreeps.com/lollipopman/heckler/internal/gitutil"
	"github.braintreeps.com/lollipopman/heckler/internal/rizzopb"
	"github.com/lollipopman/luser"
	"gopkg.in/yaml.v3"

	"google.golang.org/grpc"
)

var Version string

type lockStatus int

const (
	lockUnknown lockStatus = iota
	lockedByUser
	lockedByAnother
	unlocked
)

const (
	port     = ":50051"
	stateDir = "/var/lib/rizzo"
	repoDir  = stateDir + "/repo/puppetcode"
	lockPath = "/var/tmp/puppet.lock"
)

type lockState struct {
	lockStatus
	user    string
	comment string
}

// server is used to implement rizzo.RizzoServer.
type rizzoServer struct {
	rizzopb.UnimplementedRizzoServer
	conf *RizzoConf
}

// PuppetApply implements rizzo.RizzoServer
func (s *rizzoServer) PuppetApply(ctx context.Context, req *rizzopb.PuppetApplyRequest) (*rizzopb.PuppetReport, error) {
	var err error
	var oid string

	log.Printf("Received: %v", req.Rev)

	repoUrl := "http://" + s.conf.HecklerHost + ":8080/puppetcode"
	repo, err := gitutil.PullBranch(repoUrl, "master", repoDir)
	if err != nil {
		return &rizzopb.PuppetReport{}, err
	}
	log.Println("PullBranch Complete")

	// checkout
	oid, err = gitutil.Checkout(req.Rev, repo)
	if err != nil {
		log.Printf("Checkout error, rev: %s: %v", req.Rev, err)
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
func (s *rizzoServer) PuppetLastApply(ctx context.Context, req *rizzopb.PuppetLastApplyRequest) (*rizzopb.PuppetReport, error) {
	var err error
	log.Printf("PuppetLastApply: request received, %v", req)

	file, err := os.Open(s.conf.PuppetReportDir + "/heckler/heckler_last_apply.json")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := ioutil.ReadAll(file)
	if err != nil {
		return nil, err
	}
	pr := new(rizzopb.PuppetReport)
	err = json.Unmarshal([]byte(data), pr)
	if err != nil {
		return nil, err
	}
	log.Printf("PuppetLastApply: status@%s", pr.ConfigurationVersion)
	return pr, nil
}

func (s *rizzoServer) PuppetLock(ctx context.Context, req *rizzopb.PuppetLockRequest) (*rizzopb.PuppetLockReport, error) {
	log.Printf("PuppetLock: request received, %v", req)
	var li lockState
	var err error
	switch req.Type {
	case rizzopb.LockReqType_lock:
		li, err = puppetLock(req.User, req.Comment, req.Force)
	case rizzopb.LockReqType_unlock:
		li, err = puppetUnlock(req.User, req.Force)
	case rizzopb.LockReqType_state:
		li, err = puppetLockState(req.User)
	default:
		log.Fatal("Unknown lock request!")
	}
	if err != nil {
		return &rizzopb.PuppetLockReport{
			LockStatus: rizzopb.LockStatus_lock_unknown,
			Error:      err.Error(),
		}, nil
	}
	res := lockStateToReport(li)
	log.Printf("PuppetLock: reply, %v", res)
	return res, nil
}

func lockStateToReport(lockState lockState) *rizzopb.PuppetLockReport {
	res := new(rizzopb.PuppetLockReport)
	switch lockState.lockStatus {
	case lockUnknown:
		res.LockStatus = rizzopb.LockStatus_lock_unknown
	case lockedByUser:
		res.LockStatus = rizzopb.LockStatus_locked_by_user
	case lockedByAnother:
		res.LockStatus = rizzopb.LockStatus_locked_by_another
	case unlocked:
		res.LockStatus = rizzopb.LockStatus_unlocked
	default:
		log.Fatal("Unknown lockStatus!")
	}
	res.Comment = lockState.comment
	res.User = lockState.user
	return res
}

func puppetLockState(reqUser string) (lockState, error) {
	var li lockState
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		li.lockStatus = unlocked
		return li, nil
	}
	lockOwner, err := lockOwner()
	if err != nil {
		return li, nil
	}
	li.user = lockOwner.Username
	li.comment, err = lockComment()
	if err != nil {
		return li, nil
	}
	if reqUser == lockOwner.Username {
		li.lockStatus = lockedByUser
	} else {
		li.lockStatus = lockedByAnother
	}
	return li, nil
}

func puppetLock(locker string, comment string, forceLock bool) (lockState, error) {
	var li lockState
	tmpfile, err := ioutil.TempFile("/var/tmp", "rizzoPuppetLockTmpFile.*")
	if err != nil {
		return li, err
	}
	err = tmpfile.Chmod(0644)
	if err != nil {
		return li, err
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.WriteString(strings.TrimRight(comment, "\n") + "\n"); err != nil {
		return li, err
	}
	if err := tmpfile.Close(); err != nil {
		return li, err
	}

	lu, err := luser.Lookup(locker)
	if err != nil {
		return li, err
	}

	uid, err := strconv.Atoi(lu.Uid)
	if err != nil {
		return li, err
	}
	gid, err := strconv.Atoi(lu.Gid)
	if err != nil {
		return li, err
	}
	// Chown lock file so it is owned by locker user
	os.Chown(tmpfile.Name(), uid, gid)

	if forceLock {
		err = os.Rename(tmpfile.Name(), lockPath)
		if err != nil {
			return li, err
		} else {
			li.lockStatus = lockedByUser
			return li, nil
		}
	}

	err = renameAndCheck(tmpfile.Name(), lockPath)
	if errors.Is(err, os.ErrExist) {
		li.lockStatus = lockedByAnother
		lockOwner, err := lockOwner()
		if err != nil {
			return li, err
		}
		li.user = lockOwner.Username
		li.comment, err = lockComment()
		if err != nil {
			return li, err
		}
		return li, nil
	} else {
		li.user = locker
		li.comment = comment
		li.lockStatus = lockedByUser
		return li, nil
	}
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

func puppetUnlock(unlocker string, forceUnlock bool) (lockState, error) {
	var li lockState
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		li.lockStatus = unlocked
		return li, nil
	}
	lockOwner, err := lockOwner()
	if err != nil {
		return li, err
	}
	if unlocker == lockOwner.Username || forceUnlock {
		err = os.Remove(lockPath)
		if err != nil {
			return li, err
		}
		li.lockStatus = unlocked
	} else {
		li.user = lockOwner.Username
		li.comment, err = lockComment()
		if err != nil {
			return li, nil
		}
		li.lockStatus = lockedByAnother
	}
	return li, nil
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
	puppetArgs = append(puppetArgs, "--diff=diff")
	puppetArgs = append(puppetArgs, "--diff_args='--unified --color=never")
	if noop {
		puppetArgs = append(puppetArgs, "--noop")
	}
	// nice to lowest priority to avoid interferring with other apps, this could
	// be done in Go via the syscall package but it is a bit cumbersome
	puppetArgs = append([]string{"-n", "19", "puppet"}, puppetArgs...)
	if path, ok := conf.Env["PATH"]; ok {
		oldPath = os.Getenv("PATH")
		os.Setenv("PATH", path)
	}
	cmd := exec.Command("nice", puppetArgs...)
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
	Env  map[string]string `yaml:"env"`
	Args []string          `yaml:"args"`
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
	var conf *RizzoConf
	var clearState bool
	var printVersion bool
	var cpuprofile = flag.String("cpuprofile", "", "write cpu profile to `file`")
	var memprofile = flag.String("memprofile", "", "write memory profile to `file`")

	flag.BoolVar(&clearState, "clear", false, "Clear local state, e.g. puppet code repo")
	flag.BoolVar(&printVersion, "version", false, "print version")
	flag.Parse()

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal("could not create CPU profile: ", err)
		}
		defer f.Close() // error handling omitted for example
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Fatal("could not start CPU profile: ", err)
		}
		defer pprof.StopCPUProfile()
	}

	if printVersion {
		fmt.Printf("v%s\n", Version)
		os.Exit(0)
	}

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
	data, err = ioutil.ReadAll(file)
	if err != nil {
		log.Fatalf("Cannot read config: %v", err)
	}
	conf = new(RizzoConf)
	err = yaml.Unmarshal([]byte(data), conf)
	if err != nil {
		log.Fatalf("Cannot unmarshal config: %v", err)
	}
	file.Close()

	if clearState {
		log.Printf("Removing state directory: %v", stateDir)
		os.RemoveAll(stateDir)
		os.Exit(0)
	}

	// Clone a copy of the repo on startup so that future fetches are quicker.
	repoUrl := "http://" + conf.HecklerHost + ":8080/puppetcode"
	repoPulled := false
	for repoPulled == false {
		log.Printf("Pulling: %s", repoUrl)
		_, err := gitutil.PullBranch(repoUrl, "master", repoDir)
		if err != nil {
			sleepDur := 10 * time.Second
			log.Printf("Pull error, trying again after sleeping for %s: %v", sleepDur, err)
			time.Sleep(sleepDur)
			continue
		}
		repoPulled = true
		log.Println("Pull Complete")
	}

	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	grpcServer := grpc.NewServer()
	rizzoServer := new(rizzoServer)
	go func() {
		rizzoServer.conf = conf
		rizzopb.RegisterRizzoServer(grpcServer, rizzoServer)
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()

	done := make(chan bool, 1)
	go func() {
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
		log.Printf("Received %s", <-sigs)
		grpcServer.GracefulStop()
		log.Println("Rizzo Shutdown")
		done <- true
	}()

	<-done

	if *memprofile != "" {
		f, err := os.Create(*memprofile)
		if err != nil {
			log.Fatal("could not create memory profile: ", err)
		}
		defer f.Close() // error handling omitted for example
		runtime.GC()    // get up-to-date statistics
		if err := pprof.WriteHeapProfile(f); err != nil {
			log.Fatal("could not write memory profile: ", err)
		}
	}
}
