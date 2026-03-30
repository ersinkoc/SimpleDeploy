package main

import (
	"fmt"
	"os"

	"github.com/ersinkoc/SimpleDeploy/internal/cli"
	"github.com/ersinkoc/SimpleDeploy/internal/config"
	"github.com/ersinkoc/SimpleDeploy/internal/state"
)

func main() {
	config.Init()
	state.InitState(config.HomeDataDir())

	if len(os.Args) < 2 {
		cli.PrintUsage()
		os.Exit(0)
	}

	if err := cli.Route(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
