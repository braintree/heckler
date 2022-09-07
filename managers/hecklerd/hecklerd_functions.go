package hecklerd

import (
	"gopkg.in/yaml.v3"
	"io/ioutil"
	"log"
	"os"
	"time"
)

func loadConfig(logger *log.Logger) (*HecklerdConf, error) {
	var hecklerdConfPath string
	if _, err := os.Stat("/etc/hecklerd/hecklerd_conf.yaml"); err == nil {
		hecklerdConfPath = "/etc/hecklerd/hecklerd_conf.yaml"
	} else if _, err := os.Stat("hecklerd_conf.yaml"); err == nil {
		hecklerdConfPath = "hecklerd_conf.yaml"
	} else {
		logger.Fatal("Unable to load hecklerd_conf.yaml from /etc/hecklerd or .")
	}
	file, err := os.Open(hecklerdConfPath)
	if err != nil {
		logger.Fatal(err)
	}
	data, err := ioutil.ReadAll(file)
	if err != nil {
		logger.Fatalf("Cannot read config: %v", err)
	}
	conf := new(HecklerdConf)
	// Set some defaults
	conf.StateDir = "/var/lib/hecklerd"
	conf.WorkRepo = conf.StateDir + "/work_repo/puppetcode"
	conf.ServedRepo = conf.StateDir + "/served_repo/puppetcode"
	conf.NoopDir = conf.StateDir + "/noops"
	conf.GroupedNoopDir = conf.NoopDir + "/grouped"
	conf.LoopNoopSleepSeconds = 10
	conf.LoopMilestoneSleepSeconds = 10
	conf.LoopApprovalSleepSeconds = 10
	conf.LoopCleanSleepSeconds = 10
	conf.Timezone = "America/Chicago"
	conf.HoundWait = "8h"
	conf.AutoTagCronSchedule = "*/10 13-15 * * mon-fri"
	conf.HoundCronSchedule = "0 9-16 * * mon-fri"
	conf.ApplyCronSchedule = "* 9-15 * * mon-fri"
	conf.ApplySetOrder = []string{"all"}
	conf.ModulesPaths = []string{"modules", "vendor/modules"}
	err = yaml.Unmarshal([]byte(data), conf)
	file.Close()
	return conf, err

}

func validateConfig(logger *log.Logger, conf HecklerdConf, clearState, clearGitHub bool) {

	if conf.RepoBranch == "" {
		logger.Println("No branch specified in config, please add RepoBranch")
		os.Exit(1)
	}

	if len(conf.AdminOwners) == 0 {
		logger.Println("You must supply at least one GitHub AdminOwner")
		os.Exit(1)
	}

	// Ensure we have a valid timezone
	_, err := time.LoadLocation(conf.Timezone)
	if err != nil {
		logger.Fatalf("Unable to load timezone, '%s': %v", conf.Timezone, err)
	}
	// Set the timezone for the entire program
	os.Setenv("TZ", conf.Timezone)
	// 	t := time.Now()
	// 	abbrevZone, _ := t.Zone()

	ok, msg := ignoredResourcesValid(conf.IgnoredResources)
	if !ok {
		logger.Printf("IgnoredResources invalid, %v", msg)
		os.Exit(1)
	}

	if clearState && clearGitHub {
		logger.Println("clear & ghclear are mutually exclusive")
		os.Exit(1)
	}

	if clearState {
		logger.Printf("Removing state directory: %v", conf.StateDir)
		os.RemoveAll(conf.StateDir)
		os.Exit(0)
	}

	if clearGitHub {
		err = clearGithub(conf)
		if err != nil {
			logger.Fatalf("Unable to clear GitHub: %v", err)
		}
		os.Exit(0)
	}
}
