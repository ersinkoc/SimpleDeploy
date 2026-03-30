package main

import (
	"os"
	"testing"

	"github.com/ersinkoc/SimpleDeploy/internal/state"
)

func captureExit(f func()) int {
	code := -1
	old := osExit
	osExit = func(c int) { code = c }
	defer func() { osExit = old }()
	f()
	return code
}

func TestMain_NoArgs(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("SIMPLEDEPLOY_DIR", dir)
	t.Cleanup(func() { os.Unsetenv("SIMPLEDEPLOY_DIR") })
	state.InitState(dir)

	oldArgs := os.Args
	os.Args = []string{"simpledeploy"}
	t.Cleanup(func() { os.Args = oldArgs })

	code := captureExit(main)
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
}

func TestMain_Version(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("SIMPLEDEPLOY_DIR", dir)
	t.Cleanup(func() { os.Unsetenv("SIMPLEDEPLOY_DIR") })
	state.InitState(dir)

	oldArgs := os.Args
	os.Args = []string{"simpledeploy", "version"}
	t.Cleanup(func() { os.Args = oldArgs })

	code := captureExit(main)
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
}

func TestMain_Error(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("SIMPLEDEPLOY_DIR", dir)
	t.Cleanup(func() { os.Unsetenv("SIMPLEDEPLOY_DIR") })
	state.InitState(dir)

	oldArgs := os.Args
	os.Args = []string{"simpledeploy", "nonexistent-command"}
	t.Cleanup(func() { os.Args = oldArgs })

	code := captureExit(main)
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}

func TestRun_NoArgs(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("SIMPLEDEPLOY_DIR", dir)
	t.Cleanup(func() { os.Unsetenv("SIMPLEDEPLOY_DIR") })
	state.InitState(dir)

	code := run([]string{"simpledeploy"})
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
}

func TestRun_Version(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("SIMPLEDEPLOY_DIR", dir)
	t.Cleanup(func() { os.Unsetenv("SIMPLEDEPLOY_DIR") })
	state.InitState(dir)

	code := run([]string{"simpledeploy", "version"})
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
}

func TestRun_Error(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("SIMPLEDEPLOY_DIR", dir)
	t.Cleanup(func() { os.Unsetenv("SIMPLEDEPLOY_DIR") })
	state.InitState(dir)

	code := run([]string{"simpledeploy", "nonexistent-command"})
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}
