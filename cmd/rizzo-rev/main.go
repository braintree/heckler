package main

import (
	"fmt"
	"os"

	git "github.com/libgit2/git2go"
)

func main() {
	repo, err := git.OpenRepository("/var/lib/rizzo/repo/puppetcode")
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Unable to get rev: %v\n", err)
		os.Exit(1)
	}
	obj, err := repo.RevparseSingle("HEAD")
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Unable to get rev: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("%s\n", obj.Id())
}