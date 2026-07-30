package main

import (
	"fmt"
	"os"

	"github.com/ersinkoc/SimpleDeploy/internal/applock"
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

	// Release any per-app lock this process holds if the operator interrupts.
	// Go's default SIGINT handling terminates without running deferred
	// functions, and the deploy wizard holds a lock while blocking on roughly a
	// dozen interactive prompts — so Ctrl-C there used to leave a lock behind
	// with a fresh mtime, blocking both a retry and `remove` of that app until
	// the file was deleted by hand or applock.StaleAfter (90 min) elapsed.
	applock.InstallSignalCleanup(osExit)

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
