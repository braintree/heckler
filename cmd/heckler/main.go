package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.braintreeps.com/lollipopman/heckler/internal/hecklerpb"
	"google.golang.org/grpc"
)

var Version string

type hostFlags []string

func (i *hostFlags) String() string {
	return fmt.Sprint(*i)
}

func (i *hostFlags) Set(value string) error {
	*i = append(*i, value)
	return nil
}

func main() {
	// add filename and linenumber to log output
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	var hosts hostFlags
	var beginRev string
	var endRev string
	var rev string
	var noop bool
	var status bool
	var printVersion bool
	var markdownOut bool
	var githubMilestone string

	flag.Var(&hosts, "node", "node hostnames to group")
	flag.StringVar(&beginRev, "beginrev", "", "begin rev")
	flag.StringVar(&endRev, "endrev", "", "end rev")
	flag.StringVar(&rev, "rev", "", "rev to apply or noop")
	flag.BoolVar(&status, "status", false, "Query node apply status")
	flag.BoolVar(&noop, "noop", false, "noop")
	flag.BoolVar(&markdownOut, "md", false, "Generate markdown output")
	flag.StringVar(&githubMilestone, "github", "", "Github milestone to create")
	flag.BoolVar(&printVersion, "version", false, "print version")
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

	if len(hosts) == 0 {
		fmt.Printf("ERROR: You must supply one or more nodes\n")
		flag.Usage()
		os.Exit(1)
	}

	hecklerdConn, err := grpc.Dial("localhost:50051", grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		// XXX support running heckler client remotely
		log.Fatalf("Unable to connect to: %v, %v", "localhost:50051", err)
	}
	hc := hecklerpb.NewHecklerClient(hecklerdConn)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*300)
	defer cancel()

	if status {
		hsr := hecklerpb.HecklerStatusRequest{Nodes: hosts}
		rprt, err := hc.HecklerStatus(ctx, &hsr)
		if err != nil {
			log.Fatalf("Unable to retreive heckler statuses: %v", err)
		}
		for node, nodeStatus := range rprt.NodeStatuses {
			fmt.Printf("Status: %s@%s\n", node, nodeStatus)
		}
		for node, nodeError := range rprt.NodeErrors {
			fmt.Printf("Error: %s, %s\n", node, nodeError)
		}
		os.Exit(0)
	}

	if rev != "" {
		har := hecklerpb.HecklerApplyRequest{
			Rev:   rev,
			Noop:  noop,
			Nodes: hosts,
		}
		rprt, err := hc.HecklerApply(ctx, &har)
		if err != nil {
			log.Fatalf("Unable to retreive heckler apply report: %v", err)
		}
		if rprt.Output != "" {
			fmt.Printf("%s", rprt.Output)
		}
		for node, nodeError := range rprt.NodeErrors {
			fmt.Printf("Error: %s, %s\n", node, nodeError)
		}
		os.Exit(0)
	}

	if beginRev != "" && endRev != "" {
		hnr := hecklerpb.HecklerNoopRangeRequest{
			BeginRev: beginRev,
			EndRev:   endRev,
			Nodes:    hosts,
		}
		if githubMilestone != "" {
			hnr.GithubMilestone = githubMilestone
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
