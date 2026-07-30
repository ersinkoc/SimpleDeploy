package cli

import (
	"fmt"
	"os"
	"testing"
	"time"

	cfgpkg "github.com/ersinkoc/SimpleDeploy/internal/config"
)

// TestMain sets up two things for the whole package.
//
// 1. It shortens the container stability window. waitForContainer requires a
// container to hold "running" for containerStableFor before believing it — 5 s
// in production, which is nothing next to an image build but is dead weight
// multiplied across every Docker-backed test here (it took the suite from ~12 s
// to ~148 s). Tests that actually exercise the window set their own value;
// everything else only needs it to be non-zero, because zero would
// short-circuit the very check that catches a crash-looping deploy.
//
// 2. It redirects the STATE directory, which is where per-app lock files live.
// Tests call state.InitState(t.TempDir()) to isolate state.json, but that is a
// different mechanism from config.HomeDataDir() — so without this, every test
// that runs a locked command (deploy, redeploy, stop, restart, remove) would
// drop lock files into the developer's real ~/.simpledeploy, where they could
// collide with an actual running deploy.
func TestMain(m *testing.M) {
	containerStableFor = 10 * time.Millisecond

	stateDir, err := os.MkdirTemp("", "simpledeploy-cli-tests-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not create a temp state dir: %v\n", err)
		os.Exit(1)
	}
	if err := os.Setenv(cfgpkg.StateDirEnv, stateDir); err != nil {
		fmt.Fprintf(os.Stderr, "could not set %s: %v\n", cfgpkg.StateDirEnv, err)
		os.Exit(1)
	}

	code := m.Run()

	// Explicit cleanup: os.Exit does not run deferred functions.
	os.RemoveAll(stateDir)
	os.Exit(code)
}
