package applock

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ersinkoc/SimpleDeploy/internal/config"
)

// lockTestDir points BaseDir at a temp directory so the lock files these tests
// create never touch a real install.
func lockTestDir(t *testing.T) string {
	t.Helper()
	old := config.BaseDir
	t.Cleanup(func() {
		config.BaseDir = old
		// Locks are tracked process-wide; do not leak them into other tests.
		ReleaseAll()
	})
	config.BaseDir = filepath.Join(t.TempDir(), "opt", "simpledeploy")
	return config.BaseDir
}

func lockPathFor(appName string) string {
	return filepath.Join(config.AppsDir(), "."+appName+lockSuffix)
}

func TestAcquire_ExclusiveThenReusable(t *testing.T) {
	lockTestDir(t)

	release, err := Acquire("myapp")
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}

	// A second acquisition — what a CLI redeploy racing a webhook-triggered
	// deploy of the same app looks like — must be refused, and must be
	// recognisable as contention so the webhook can retry rather than drop the
	// push.
	_, err = Acquire("myapp")
	if err == nil {
		t.Fatal("a second acquire while the lock is held must fail")
	}
	if !errors.Is(err, ErrHeld) {
		t.Errorf("contention must wrap ErrHeld so callers can retry; got %v", err)
	}
	if !strings.Contains(err.Error(), lockPathFor("myapp")) {
		t.Errorf("error should name the lock file, got: %v", err)
	}

	// A different app is independent.
	otherRelease, err := Acquire("otherapp")
	if err != nil {
		t.Fatalf("locking a different app must be independent: %v", err)
	}
	otherRelease()

	release()

	release2, err := Acquire("myapp")
	if err != nil {
		t.Fatalf("acquire after release failed: %v", err)
	}
	release2()

	if _, err := os.Stat(lockPathFor("myapp")); !os.IsNotExist(err) {
		t.Errorf("lock file should be gone after release, stat err = %v", err)
	}
}

// TestAcquire_ErrorMessageDoesNotInviteDeletingALiveLock pins that the
// contention message does not present a pid as something to look up. A
// webhook-triggered deploy runs inside the qd-service container, so the
// recorded pid belongs to that container's namespace and is typically 1 — an
// operator who checked `ps 1` on the host would see systemd, conclude the lock
// was a crash leftover, delete it, and re-enable the concurrent-deploy
// corruption this package prevents.
func TestAcquire_ErrorMessageDoesNotInviteDeletingALiveLock(t *testing.T) {
	lockTestDir(t)

	release, err := Acquire("myapp")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer release()

	_, err = Acquire("myapp")
	if err == nil {
		t.Fatal("expected contention")
	}
	msg := err.Error()
	if strings.Contains(msg, "pid") {
		t.Errorf("message should not present a pid as authoritative: %q", msg)
	}
	if !strings.Contains(msg, "qd-service") {
		t.Errorf("message should warn the holder may be the containerised service: %q", msg)
	}
	if !strings.Contains(msg, "can corrupt") {
		t.Errorf("message should warn about deleting a live lock: %q", msg)
	}
}

func TestAcquire_StealsStaleLock(t *testing.T) {
	lockTestDir(t)

	if err := os.MkdirAll(config.AppsDir(), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	lockPath := lockPathFor("crashed")
	if err := os.WriteFile(lockPath, []byte("99999 1\n"), 0600); err != nil {
		t.Fatalf("seed lock: %v", err)
	}
	old := time.Now().Add(-2 * StaleAfter)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	release, err := Acquire("crashed")
	if err != nil {
		t.Fatalf("a lock older than StaleAfter should be recovered: %v", err)
	}
	release()
}

// TestRelease_LeavesForeignLock pins the token check: removing by path alone
// can delete a lock a DIFFERENT live process created after ours was recovered
// as stale.
func TestRelease_LeavesForeignLock(t *testing.T) {
	lockTestDir(t)

	release, err := Acquire("myapp")
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

func TestRelease_Idempotent(t *testing.T) {
	lockTestDir(t)

	release, err := Acquire("myapp")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	release()
	release() // must not panic, and must not remove a lock taken since

	again, err := Acquire("myapp")
	if err != nil {
		t.Fatalf("re-acquire: %v", err)
	}
	release() // the FIRST release, called after someone else took the lock
	if _, err := os.Stat(lockPathFor("myapp")); err != nil {
		t.Errorf("a stale release must not remove the new holder's lock: %v", err)
	}
	again()
}

// TestReleaseAll_ReleasesEveryHeldLock covers what the signal handler does: on
// Ctrl-C the process dies without running defers, so the handler is the only
// thing that can free the locks.
func TestReleaseAll_ReleasesEveryHeldLock(t *testing.T) {
	lockTestDir(t)

	for _, app := range []string{"one", "two", "three"} {
		if _, err := Acquire(app); err != nil {
			t.Fatalf("acquire %s: %v", app, err)
		}
	}

	ReleaseAll()

	for _, app := range []string{"one", "two", "three"} {
		if _, err := os.Stat(lockPathFor(app)); !os.IsNotExist(err) {
			t.Errorf("lock for %s should be released, stat err = %v", app, err)
		}
	}
}

// TestInstallSignalCleanup_ReleasesOnSignal drives the real signal path: it
// installs the handler, sends SIGTERM to this process, and checks the lock is
// gone. exit is captured rather than terminating the test binary.
func TestInstallSignalCleanup_ReleasesOnSignal(t *testing.T) {
	lockTestDir(t)

	var wg sync.WaitGroup
	wg.Add(1)
	var gotCode int
	InstallSignalCleanup(func(code int) {
		gotCode = code
		wg.Done()
	})

	if _, err := Acquire("interrupted"); err != nil {
		t.Fatalf("acquire: %v", err)
	}

	proc, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("FindProcess: %v", err)
	}
	if err := proc.Signal(os.Interrupt); err != nil {
		t.Skipf("cannot deliver SIGINT on this platform: %v", err)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("signal handler did not run")
	}

	if gotCode != 130 {
		t.Errorf("exit code = %d, want 130 (conventional for SIGINT)", gotCode)
	}
	if _, err := os.Stat(lockPathFor("interrupted")); !os.IsNotExist(err) {
		t.Errorf("the lock must be released on interrupt, stat err = %v", err)
	}
}
