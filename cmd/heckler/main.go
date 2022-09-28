package main

import (
	// 	"flag"
	// "fmt"
	"log"
	// 	"os"

	heckler "github.com/braintree/heckler/apps/heckler"
)

func main() {
	hCliMgr := heckler.NewHecklerCLIManager()
	hCliMgr.Run()
}
func nomain() {
	// add filename and linenumber to log output
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	/*
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

		hCliMgr:=heckler.NewHecklerCLIManager()
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

		hCliMgr.Run()

		flag.Usage()
	*/
}
