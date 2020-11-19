package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"time"

	"github.com/lollipopman/heckler/internal/hecklerpb"
	"github.com/lollipopman/luser"
	"google.golang.org/grpc"
)

var Version string

type nodeFlag []string

func (i *nodeFlag) String() string {
	return fmt.Sprint(*i)
}

func (i *nodeFlag) Set(value string) error {
	*i = append(*i, value)
	return nil
}

func readNodeFile(nodeFile string) ([]string, error) {
	var nodes []string
	var err error
	var file *os.File

	if nodeFile == "-" {
		file = os.Stdin
	} else {
		file, err = os.Open(nodeFile)
		if err != nil {
			return nil, err
		}
		defer file.Close()
	}
	data, err := ioutil.ReadAll(file)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal([]byte(data), &nodes)
	if err != nil {
		return nil, err
	}
	return nodes, nil
}

func main() {
	// add filename and linenumber to log output
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	var nodeFlags nodeFlag
	var nodes []string
	var beginRev string
	var endRev string
	var rev string
	var nodeFile string
	var noop bool
	var deltaNoop bool
	var lock bool
	var unlock bool
	var force bool
	var status bool
	var printVersion bool
	var markdownOut bool
	var nodeSet string

	flag.BoolVar(&force, "force", false, "force unlock, lock, or apply of nodes")
	flag.BoolVar(&lock, "lock", false, "lock nodes")
	flag.BoolVar(&markdownOut, "md", false, "Generate markdown output")
	flag.BoolVar(&noop, "noop", false, "noop")
	flag.BoolVar(&deltaNoop, "delta", false, "Show the delta of the noop minus its parents")
	flag.BoolVar(&printVersion, "version", false, "print version")
	flag.BoolVar(&status, "status", false, "Query node apply status")
	flag.BoolVar(&unlock, "unlock", false, "unlock nodes")
	flag.StringVar(&beginRev, "beginrev", "", "begin rev")
	flag.StringVar(&endRev, "endrev", "", "end rev")
	flag.StringVar(&nodeFile, "file", "", "file with json array of nodes, use - to read from stdin")
	flag.StringVar(&rev, "rev", "", "rev to apply or noop")
	flag.StringVar(&nodeSet, "set", "", "node set defined on hecklerd, e.g. 'all'")
	flag.Var(&nodeFlags, "node", "node hostnames to group, may be repeated")
	flag.Parse()

	if printVersion {
		fmt.Printf("v%s\n", Version)
		os.Exit(0)
	}

	if status && (rev != "" || beginRev != "" || endRev != "") {
		fmt.Printf("The -status flag cannot be combined with: rev, beginrev, or endrev\n")
		flag.Usage()
		os.Exit(1)
	}

	if rev != "" && (beginRev != "" || endRev != "") {
		fmt.Printf("The -rev flag cannot be combined with the -beginrev or the -endrev\n")
		flag.Usage()
		os.Exit(1)
	}

	if len(nodeFlags) != 0 && nodeSet != "" {
		fmt.Printf("ERROR: flag `-set` cannot be combined with `-node`\n")
		flag.Usage()
		os.Exit(1)
	}

	if nodeFile != "" && nodeSet != "" {
		fmt.Printf("ERROR: flag `-set` cannot be combined with `-file`\n")
		flag.Usage()
		os.Exit(1)
	}

	var err error
	if nodeFile != "" {
		nodeSet = ""
		nodes, err = readNodeFile(nodeFile)
		if err != nil {
			panic(err)
		}
	} else if len(nodeFlags) != 0 {
		nodeSet = ""
		nodes = make([]string, len(nodeFlags))
		copy(nodes, nodeFlags)
	} else {
		if nodeSet == "" {
			nodeSet = "all"
		}
	}

	curUserInf, err := luser.Current()
	if err != nil {
		panic(err)
	}

	hecklerdConn, err := grpc.Dial("localhost:50052", grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		// XXX support running heckler client remotely
		log.Fatalf("Unable to connect to: %v, %v", "localhost:50051", err)
	}
	hc := hecklerpb.NewHecklerClient(hecklerdConn)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*10)
	defer cancel()

	if lock {
		req := hecklerpb.HecklerLockRequest{
			User:    curUserInf.Username,
			Comment: "Locked by Heckler",
			Force:   force,
			NodeSet: nodeSet,
			Nodes:   nodes,
		}
		rprt, err := hc.HecklerLock(ctx, &req)
		if err != nil {
			log.Fatalf("Unable to lock nodes: %v", err)
		}
		for _, node := range rprt.LockedNodes {
			fmt.Printf("Locked: %s\n", node)
		}
		for node, nodeError := range rprt.NodeErrors {
			fmt.Printf("Error: %s, %s\n", node, nodeError)
		}
		os.Exit(0)
	}

	if unlock {
		req := hecklerpb.HecklerUnlockRequest{
			User:    curUserInf.Username,
			Force:   force,
			NodeSet: nodeSet,
			Nodes:   nodes,
		}
		rprt, err := hc.HecklerUnlock(ctx, &req)
		if err != nil {
			log.Fatalf("Unable to unlock nodes: %v", err)
		}
		for _, node := range rprt.UnlockedNodes {
			fmt.Printf("Unlocked: %s\n", node)
		}
		for node, nodeError := range rprt.NodeErrors {
			fmt.Printf("Error: %s, %s\n", node, nodeError)
		}
		os.Exit(0)
	}

	if status {
		hsr := hecklerpb.HecklerStatusRequest{
			NodeSet: nodeSet,
			Nodes:   nodes,
		}
		rprt, err := hc.HecklerStatus(ctx, &hsr)
		if err != nil {
			log.Fatalf("Unable to retreive heckler statuses: %v", err)
		}
		for node, nodeStatus := range rprt.NodeStatuses {
			fmt.Printf("Status: %s, %s\n", node, nodeStatus)
		}
		for node, nodeError := range rprt.NodeErrors {
			fmt.Printf("Error: %s, %s\n", node, nodeError)
		}
		os.Exit(0)
	}

	if rev != "" {
		har := hecklerpb.HecklerApplyRequest{
			User:      curUserInf.Username,
			Rev:       rev,
			Noop:      noop,
			DeltaNoop: deltaNoop,
			Force:     force,
			NodeSet:   nodeSet,
			Nodes:     nodes,
		}
		rprt, err := hc.HecklerApply(ctx, &har)
		if err != nil {
			log.Fatalf("Unable to retreive heckler apply report: %v", err)
		}
		if rprt.Output != "" {
			fmt.Printf("%s\n", rprt.Output)
		}
		for node, nodeError := range rprt.NodeErrors {
			fmt.Printf("Error: %s, %s\n", node, nodeError)
		}
		os.Exit(0)
	}

	if beginRev != "" && endRev != "" {
		hnr := hecklerpb.HecklerNoopRangeRequest{
			User:     curUserInf.Username,
			BeginRev: beginRev,
			EndRev:   endRev,
			NodeSet:  nodeSet,
			Nodes:    nodes,
		}
		if markdownOut {
			hnr.OutputFormat = hecklerpb.OutputFormat_markdown
		}
		rprt, err := hc.HecklerNoopRange(ctx, &hnr)
		if err != nil {
			log.Fatalf("Unable to retreive heckler noop report: %v", err)
		}
		if rprt.Output != "" {
			fmt.Printf("%s", rprt.Output)
		}
		for node, nodeError := range rprt.NodeErrors {
			fmt.Printf("Error: %s, %s\n", node, nodeError)
		}
		os.Exit(0)
	}

	flag.Usage()
}
