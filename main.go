package main

import (
	"fmt"
	"os"

	"github.com/ersinkoc/SimpleDeploy/internal/cli"
)

func main() {
	if len(os.Args) < 2 {
		cli.PrintUsage()
		os.Exit(0)
	}

	if err := cli.Route(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
