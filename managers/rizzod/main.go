package rizzod

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"syscall"

	"github.com/braintree/heckler/internal/gitutil"
	"github.com/braintree/heckler/internal/heckler"
	"github.com/braintree/heckler/internal/rizzopb"
	"github.com/lollipopman/luser"
	"github.com/robfig/cron/v3"
	"gopkg.in/yaml.v3"

	"google.golang.org/grpc"
)

var Version string

const (
	port     = ":50051"
	lockPath = "/var/tmp/puppet.lock"
)

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
	repo, err := gitutil.PullBranch(repoUrl, s.conf.RepoBranch, s.conf.RepoDir)
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
	var li heckler.LockState
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

func lockStateToReport(lockState heckler.LockState) *rizzopb.PuppetLockReport {
	res := new(rizzopb.PuppetLockReport)
	switch lockState.LockStatus {
	case heckler.LockUnknown:
		res.LockStatus = rizzopb.LockStatus_lock_unknown
	case heckler.LockedByUser:
		res.LockStatus = rizzopb.LockStatus_locked_by_user
	case heckler.LockedByAnother:
		res.LockStatus = rizzopb.LockStatus_locked_by_another
	case heckler.Unlocked:
		res.LockStatus = rizzopb.LockStatus_unlocked
	default:
		log.Fatal("Unknown LockStatus!")
	}
	res.Comment = lockState.Comment
	res.User = lockState.User
	return res
}

func puppetLockState(reqUser string) (heckler.LockState, error) {
	var li heckler.LockState
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		li.LockStatus = heckler.Unlocked
		return li, nil
	}
	lockOwner, err := lockOwner()
	if err != nil {
		return li, nil
	}
	li.User = lockOwner.Username
	li.Comment, err = lockComment()
	if err != nil {
		return li, nil
	}
	if reqUser == lockOwner.Username {
		li.LockStatus = heckler.LockedByUser
	} else {
		li.LockStatus = heckler.LockedByAnother
	}
	return li, nil
}

func puppetLock(locker string, comment string, forceLock bool) (heckler.LockState, error) {
	var li heckler.LockState
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
			li.LockStatus = heckler.LockedByUser
			return li, nil
		}
	}

	err = renameAndCheck(tmpfile.Name(), lockPath)
	if errors.Is(err, os.ErrExist) {
		li.LockStatus = heckler.LockedByAnother
		lockOwner, err := lockOwner()
		if err != nil {
			return li, err
		}
		li.User = lockOwner.Username
		li.Comment, err = lockComment()
		if err != nil {
			return li, err
		}
		return li, nil
	} else {
		li.User = locker
		li.Comment = comment
		li.LockStatus = heckler.LockedByUser
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

func puppetUnlock(unlocker string, forceUnlock bool) (heckler.LockState, error) {
	var li heckler.LockState
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		li.LockStatus = heckler.Unlocked
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
		li.LockStatus = heckler.Unlocked
	} else {
		li.User = lockOwner.Username
		li.Comment, err = lockComment()
		if err != nil {
			return li, nil
		}
		li.LockStatus = heckler.LockedByAnother
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

func parseStderr(oid string, stderr string) (*rizzopb.PuppetReport, error) {
	var regexEvalErr = regexp.MustCompile(`^Error: Evaluation Error:`)
	scanner := bufio.NewScanner(strings.NewReader(stderr))
	for scanner.Scan() {
		if regexEvalErr.MatchString(scanner.Text()) {
			stderrLogs := []*rizzopb.Log{
				&rizzopb.Log{
					Source:  "EvalError",
					Message: stderr,
				},
			}
			evalErrRprt := &rizzopb.PuppetReport{
				ConfigurationVersion: oid,
				Status:               "failed",
				Logs:                 stderrLogs,
			}
			return evalErrRprt, nil
		}
	}
	return nil, fmt.Errorf("Unknown error in stderr output: '%s'", stderr)
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
	// Ensure we use `diff` and no color
	puppetArgs = append(puppetArgs, "--diff=diff")
	puppetArgs = append(puppetArgs, "--diff_args='--unified --color=never")
	// Ensure no color output in log messages, which breaks regex matching
	puppetArgs = append(puppetArgs, "--color=false")
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
	var stdoutBuf, stderrBuf bytes.Buffer
	// Capture stderr & stdout as well as output them
	cmd.Stdout = io.MultiWriter(os.Stdout, &stdoutBuf)
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)
	// Change to code dir, so hiera relative paths resolve
	cmd.Dir = conf.RepoDir
	env := os.Environ()
	for k, v := range conf.Env {
		env = append(env, k+"="+v)
	}
	cmd.Env = env
	err := cmd.Run()
	if exitError, ok := err.(*exec.ExitError); ok {
		stderrRprt, err := parseStderr(oid, string(stderrBuf.Bytes()))
		if err != nil {
			log.Printf("Apply exited: %d and stderr was not pareseable, %v", exitError.ExitCode(), err)
			return &rizzopb.PuppetReport{}, fmt.Errorf("Apply exited: %d and stderr was not pareseable, %w", exitError.ExitCode(), err)
		}
		log.Printf("Error apply exited: %d, stderr parsed successfully", exitError.ExitCode())
		return stderrRprt, err
	} else if err != nil {
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
	RepoBranch      string `yaml:"repo_branch"`
	StateDir        string `yaml:"state_dir"`
	RepoDir         string `yaml:"repo_dir"`
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

	logger := log.New(os.Stdout, "[Main] ", log.Lshortfile)

	flag.BoolVar(&clearState, "clear", false, "Clear local state, e.g. puppet code repo")
	flag.BoolVar(&printVersion, "version", false, "print version")
	flag.Parse()

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			logger.Fatal("could not create CPU profile: ", err)
		}
		defer f.Close() // error handling omitted for example
		if err := pprof.StartCPUProfile(f); err != nil {
			logger.Fatal("could not start CPU profile: ", err)
		}
		defer pprof.StopCPUProfile()
	}

	if printVersion {
		fmt.Printf("v%s\n", Version)
		os.Exit(0)
	}

	if _, err := os.Stat("/etc/rizzod/rizzod_conf.yaml"); err == nil {
		rizzoConfPath = "/etc/rizzod/rizzod_conf.yaml"
	} else if _, err := os.Stat("rizzod_conf.yaml"); err == nil {
		rizzoConfPath = "rizzod_conf.yaml"
	} else {
		logger.Fatal("Unable to load rizzod_conf.yaml from /etc/rizzo or .")
	}
	file, err = os.Open(rizzoConfPath)
	if err != nil {
		logger.Fatal(err)
	}
	data, err = ioutil.ReadAll(file)
	if err != nil {
		logger.Fatalf("Cannot read config: %v", err)
	}
	conf = new(RizzoConf)
	// Set some defaults
	conf.StateDir = "/var/lib/rizzod"
	conf.RepoDir = conf.StateDir + "/repo/puppetcode"
	err = yaml.Unmarshal([]byte(data), conf)
	if err != nil {
		logger.Fatalf("Cannot unmarshal config: %v", err)
	}
	file.Close()

	if conf.RepoBranch == "" {
		logger.Fatalf("No branch specified in config, please add RepoBranch")
	}

	if clearState {
		logger.Printf("Removing state directory: %v", conf.StateDir)
		os.RemoveAll(conf.StateDir)
		os.Exit(0)
	}

	logger.Printf("rizzod: v%s\n", Version)

	// Open the port earlier than needed, to ensure we are the only process
	// running. This ensures the gitutil.ResetRepo command is safe to run.
	lis, err := net.Listen("tcp", port)
	if err != nil {
		logger.Fatalf("failed to listen: %v", err)
	}

	err = gitutil.ResetRepo(conf.RepoDir, logger)
	if err != nil {
		logger.Fatalf("Error: unable to reset repo, exiting: %v", err)
	}

	// Clone a copy of the repo on startup so that future fetches are quicker.
	repoUrl := "http://" + conf.HecklerHost + ":8080/puppetcode"
	logger.Printf("Pulling: %s", repoUrl)
	_, err = gitutil.PullBranch(repoUrl, conf.RepoBranch, conf.RepoDir)
	if err != nil {
		logger.Fatalf("Error: unable to pull repo, exiting: %v", err)
	}

	grpcServer := grpc.NewServer()
	rizzoServer := new(rizzoServer)
	go func() {
		rizzoServer.conf = conf
		rizzopb.RegisterRizzoServer(grpcServer, rizzoServer)
		logger.Printf("Starting GRPC HTTP server on %v", port)
		if err := grpcServer.Serve(lis); err != nil {
			logger.Fatalf("failed to serve: %v", err)
		}
	}()

	rizzodCron := cron.New()
	rizzodCron.AddFunc(
		"0 12 * * *",
		func() {
			gitutil.Gc(conf.RepoDir, logger)
		},
	)
	rizzodCron.Start()
	defer rizzodCron.Stop()

	done := make(chan bool, 1)
	go func() {
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
		logger.Printf("Received %s", <-sigs)
		grpcServer.GracefulStop()
		logger.Println("Rizzo Shutdown")
		done <- true
	}()

	<-done

	if *memprofile != "" {
		f, err := os.Create(*memprofile)
		if err != nil {
			logger.Fatal("could not create memory profile: ", err)
		}
		defer f.Close() // error handling omitted for example
		runtime.GC()    // get up-to-date statistics
		if err := pprof.WriteHeapProfile(f); err != nil {
			logger.Fatal("could not write memory profile: ", err)
		}
	}
}
