package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cfgpkg "github.com/ersinkoc/SimpleDeploy/internal/config"
	"github.com/ersinkoc/SimpleDeploy/internal/state"
)

// lockTestDir points BaseDir at a temp directory so the lock files these tests
// create never touch a real install.
func lockTestDir(t *testing.T) string {
	t.Helper()
	old := cfgpkg.BaseDir
	t.Cleanup(func() { cfgpkg.BaseDir = old })
	cfgpkg.BaseDir = filepath.Join(t.TempDir(), "opt", "simpledeploy")
	return cfgpkg.BaseDir
}

func lockPathFor(appName string) string {
	return filepath.Join(cfgpkg.AppsDir(), "."+appName+deployLockSuffix)
}

func TestAcquireDeployLock_ExclusiveThenReusable(t *testing.T) {
	lockTestDir(t)

	release, err := acquireDeployLock("myapp")
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}

	// A second acquisition — this is what a CLI redeploy racing a
	// webhook-triggered deploy of the same app looks like — must be refused,
	// not silently allowed to run concurrently.
	if _, err := acquireDeployLock("myapp"); err == nil {
		t.Fatal("a second acquire while the lock is held must fail")
	} else {
		if !strings.Contains(err.Error(), "already in progress") {
			t.Errorf("error should say a deploy is in progress, got: %v", err)
		}
		if !strings.Contains(err.Error(), lockPathFor("myapp")) {
			t.Errorf("error should name the lock file so it can be cleared, got: %v", err)
		}
	}

	// A different app is independent.
	otherRelease, err := acquireDeployLock("otherapp")
	if err != nil {
		t.Fatalf("locking a different app must be independent: %v", err)
	}
	otherRelease()

	release()

	// Released, so it can be taken again.
	release2, err := acquireDeployLock("myapp")
	if err != nil {
		t.Fatalf("acquire after release failed: %v", err)
	}
	release2()

	if _, err := os.Stat(lockPathFor("myapp")); !os.IsNotExist(err) {
		t.Errorf("lock file should be gone after release, stat err = %v", err)
	}
}

// TestAcquireDeployLock_StealsStaleLock covers the crashed-holder case: a lock
// older than deployLockStale must not block deploys forever.
func TestAcquireDeployLock_StealsStaleLock(t *testing.T) {
	lockTestDir(t)

	if err := os.MkdirAll(cfgpkg.AppsDir(), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	lockPath := lockPathFor("crashed")
	if err := os.WriteFile(lockPath, []byte("99999 1\n"), 0600); err != nil {
		t.Fatalf("seed lock: %v", err)
	}
	old := time.Now().Add(-2 * deployLockStale)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	release, err := acquireDeployLock("crashed")
	if err != nil {
		t.Fatalf("a lock older than deployLockStale should be recovered: %v", err)
	}
	release()
}

// TestReleaseDeployLock_LeavesForeignLock pins the token check. Removing by
// path alone can delete a lock a DIFFERENT live process created after ours was
// recovered as stale — the same defect the state lock had.
func TestReleaseDeployLock_LeavesForeignLock(t *testing.T) {
	lockTestDir(t)

	release, err := acquireDeployLock("myapp")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}

	lockPath := lockPathFor("myapp")
	foreign := []byte("4242 1700000000\n")
	if err := os.WriteFile(lockPath, foreign, 0600); err != nil {
		t.Fatalf("overwrite lock: %v", err)
	}

	release()

	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("the foreign lock must survive our release, but it is gone: %v", err)
	}
	if string(data) != string(foreign) {
		t.Errorf("lock content = %q, want the foreign holder's %q", data, foreign)
	}
}

// TestRunRedeploy_RefusedWhileLockHeld checks the lock is actually wired into
// the redeploy entry point, not merely available.
func TestRunRedeploy_RefusedWhileLockHeld(t *testing.T) {
	lockTestDir(t)

	release, err := acquireDeployLock("locked")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer release()

	err = RunRedeploy([]string{"locked"})
	if err == nil {
		t.Fatal("redeploy should refuse to run while the app's deploy lock is held")
	}
	if !strings.Contains(err.Error(), "already in progress") {
		t.Errorf("error should report the in-progress deploy, got: %v", err)
	}
}

// TestDeployLock_ReleasedAfterRedeployFails pins that a failed deploy does not
// leave the lock behind — otherwise one broken deploy would block the app for
// deployLockStale (90 minutes) and the operator would have to clear a lock file
// by hand to retry.
func TestDeployLock_ReleasedAfterRedeployFails(t *testing.T) {
	lockTestDir(t)
	state.InitState(t.TempDir())

	// No such app, so RunRedeploy fails right after taking the lock.
	if err := RunRedeploy([]string{"nosuchapp"}); err == nil {
		t.Fatal("redeploy of an unknown app should fail")
	}

	if _, err := os.Stat(lockPathFor("nosuchapp")); !os.IsNotExist(err) {
		t.Errorf("the lock must be released when a deploy fails, stat err = %v", err)
	}

	// And the app is immediately retryable rather than locked out.
	release, err := acquireDeployLock("nosuchapp")
	if err != nil {
		t.Fatalf("lock should be free after the failed deploy: %v", err)
	}
	release()
}

// TestDeployLock_RemoveIsSerializedToo pins that `remove` participates in the
// same lock: tearing containers down and deleting the app directory while a
// deploy of that app is mid-build leaves the deploy writing into a directory
// being removed under it.
func TestDeployLock_RemoveIsSerializedToo(t *testing.T) {
	lockTestDir(t)

	release, err := acquireDeployLock("busyapp")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer release()

	err = RunRemove([]string{"busyapp"})
	if err == nil {
		t.Fatal("remove should refuse to run while a deploy of that app holds the lock")
	}
	if !strings.Contains(err.Error(), "already in progress") {
		t.Errorf("error should report the in-progress deploy, got: %v", err)
	}
}
