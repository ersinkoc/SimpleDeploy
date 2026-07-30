package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ersinkoc/SimpleDeploy/internal/applock"
	cfgpkg "github.com/ersinkoc/SimpleDeploy/internal/config"
	"github.com/ersinkoc/SimpleDeploy/internal/state"
)

// These tests cover the WIRING: that each command actually takes the per-app
// lock. The lock mechanics themselves live in internal/applock and are tested
// there.

func lockTestDir(t *testing.T) string {
	t.Helper()
	old := cfgpkg.BaseDir
	t.Cleanup(func() {
		cfgpkg.BaseDir = old
		applock.ReleaseAll()
	})
	cfgpkg.BaseDir = filepath.Join(t.TempDir(), "opt", "simpledeploy")
	return cfgpkg.BaseDir
}

func lockPathFor(appName string) string {
	return filepath.Join(cfgpkg.AppsDir(), "."+appName+".deploy.lock")
}

// TestCommandsRefuseWhileDeployLockHeld pins that every command which mutates a
// deployment participates in the lock.
//
// stop and restart matter as much as the deploy commands: without the lock, a
// concurrent redeploy would `compose up` the container back up — defeating a
// stop outright — and then overwrite the status this command recorded, so the
// operator's action vanished with no error anywhere.
func TestCommandsRefuseWhileDeployLockHeld(t *testing.T) {
	commands := map[string]func(string) error{
		"redeploy": func(app string) error { return RunRedeploy([]string{app}) },
		"remove":   func(app string) error { return RunRemove([]string{app}) },
		"stop":     func(app string) error { return RunStop([]string{app}) },
		"restart":  func(app string) error { return RunRestart([]string{app}) },
	}

	for name, run := range commands {
		t.Run(name, func(t *testing.T) {
			lockTestDir(t)
			state.InitState(t.TempDir())

			release, err := applock.Acquire("locked")
			if err != nil {
				t.Fatalf("acquire: %v", err)
			}
			defer release()

			err = run("locked")
			if err == nil {
				t.Fatalf("%s should refuse to run while the app's deploy lock is held", name)
			}
			if !strings.Contains(err.Error(), "in progress") {
				t.Errorf("%s error should report the in-progress operation, got: %v", name, err)
			}
		})
	}
}

// TestDeployLock_ReleasedAfterRedeployFails pins that a failed command does not
// leave the lock behind — otherwise one failure would block the app for
// applock.StaleAfter (90 minutes) and the operator would have to clear a lock
// file by hand to retry.
func TestDeployLock_ReleasedAfterRedeployFails(t *testing.T) {
	lockTestDir(t)
	state.InitState(t.TempDir())

	// No such app, so RunRedeploy fails right after taking the lock.
	if err := RunRedeploy([]string{"nosuchapp"}); err == nil {
		t.Fatal("redeploy of an unknown app should fail")
	}

	if _, err := os.Stat(lockPathFor("nosuchapp")); !os.IsNotExist(err) {
		t.Errorf("the lock must be released when a command fails, stat err = %v", err)
	}

	release, err := applock.Acquire("nosuchapp")
	if err != nil {
		t.Fatalf("lock should be free after the failed command: %v", err)
	}
	release()
}
