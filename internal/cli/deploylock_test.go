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

// lockTestDir isolates both directories a locked command touches: BaseDir (app
// files) and the state directory (where lock files live).
func lockTestDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	old := cfgpkg.BaseDir
	t.Cleanup(func() {
		cfgpkg.BaseDir = old
		applock.ReleaseAll()
	})
	cfgpkg.BaseDir = filepath.Join(root, "opt", "simpledeploy")
	t.Setenv(cfgpkg.StateDirEnv, filepath.Join(root, "state"))
	return cfgpkg.BaseDir
}

func lockPathFor(appName string) string {
	return filepath.Join(cfgpkg.HomeDataDir(), "."+appName+".deploy.lock")
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

			// The app must EXIST: these commands check existence before taking
			// the lock, so that a typo'd name answers "not found" rather than a
			// lock error (see TestUnknownAppReportsNotFound_NotALockError).
			if err := state.SaveConfig(&state.GlobalConfig{
				Proxy: "traefik", BaseDomain: "example.com",
				AcmeEmail: "a@example.com", WebhookPort: 9000, WebhookSecret: "s",
			}); err != nil {
				t.Fatalf("SaveConfig: %v", err)
			}
			app := state.NewAppConfig()
			app.Name = "locked"
			app.Branch = "main"
			app.Domain = "locked.example.com"
			app.Port = 3000
			if err := state.SaveApp(app); err != nil {
				t.Fatalf("SaveApp: %v", err)
			}

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

// TestUnknownAppReportsNotFound_NotALockError pins the ordering that a Linux CI
// run caught: the lock was taken BEFORE the app was looked up, so a typo'd name
// answered with whatever the lock had to say about a directory it needed to
// create. As a non-root user that read
// "mkdir /opt/simpledeploy: permission denied" instead of "app not found" —
// invisible on Windows, where that path resolves somewhere writable.
//
// Deliberately does NOT isolate BaseDir: the point is that these commands must
// not depend on /opt/simpledeploy being creatable just to reject an unknown
// app.
func TestUnknownAppReportsNotFound_NotALockError(t *testing.T) {
	state.InitState(t.TempDir())
	t.Cleanup(applock.ReleaseAll)

	commands := map[string]func(string) error{
		"redeploy": func(app string) error { return RunRedeploy([]string{app}) },
		"remove":   func(app string) error { return RunRemove([]string{app}) },
		"stop":     func(app string) error { return RunStop([]string{app}) },
		"restart":  func(app string) error { return RunRestart([]string{app}) },
	}

	for name, run := range commands {
		t.Run(name, func(t *testing.T) {
			err := run("ghostapp")
			if err == nil {
				t.Fatalf("%s of an unknown app should fail", name)
			}
			msg := err.Error()
			if strings.Contains(msg, "mkdir") || strings.Contains(msg, "permission denied") {
				t.Errorf("%s reported a lock/filesystem problem instead of the real one: %v", name, err)
			}
			if !strings.Contains(msg, "not found") {
				t.Errorf("%s error should say the app was not found, got: %v", name, err)
			}
		})
	}
}
