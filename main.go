package main

import (
	"fmt"
	"os"

	"github.com/ersinkoc/SimpleDeploy/internal/cli"
	"github.com/ersinkoc/SimpleDeploy/internal/config"
	"github.com/ersinkoc/SimpleDeploy/internal/state"
)

// osExit is a variable so it can be overridden in tests.
var osExit = os.Exit

func main() {
	osExit(run(os.Args))
}

func run(args []string) int {
	config.Init()
	state.InitState(config.HomeDataDir())

	if len(args) < 2 {
		cli.PrintUsage()
		return 0
	}

	if err := cli.Route(args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}
