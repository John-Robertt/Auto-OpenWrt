package main

import (
	"fmt"
	"os"

	"github.com/John-Robertt/Auto-OpenWrt/internal/docscheck"
)

func main() {
	if len(os.Args) > 1 {
		fmt.Fprintf(os.Stderr, "error: doccheck does not accept arguments\n")
		os.Exit(2)
	}

	result, err := docscheck.Check("docs")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if len(result.Issues) > 0 {
		for _, issue := range result.Issues {
			fmt.Fprintf(os.Stderr, "%s: %s\n", issue.File, issue.Message)
		}
		os.Exit(1)
	}

	fmt.Fprintln(os.Stdout, "docs check passed")
}
