// Package applock provides a cross-process, per-application lock held for the
// duration of a deploy, redeploy, stop, restart or remove.
//
// Why it exists: nothing used to serialize these operations BETWEEN PROCESSES.
// The webhook server's in-memory map only covers deploys it starts itself, so a
// hand-run `simpledeploy redeploy X` ran fully concurrently with a
// webhook-triggered deploy of X. Both `git pull`ed the same source tree (git's
// own index.lock fails one at random), the loser's rollback restored a compose
// snapshot taken before the winner's changes — reverting a SUCCESSFUL deploy
// while state.json claimed the new image was live — and the winner's image
// prune could delete the image the loser had just built.
//
// It lives in its own package, rather than inside internal/cli, so the webhook
// server can recognise contention via errors.Is(err, ErrHeld) without importing
// cli (which imports webhook).
package applock

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/ersinkoc/SimpleDeploy/internal/config"
	"github.com/ersinkoc/SimpleDeploy/internal/lockfile"
)

// ErrHeld reports that another process holds the lock. Callers that can
// usefully wait (the unattended webhook path) test for it with errors.Is and
// retry; interactive callers surface it to the operator.
var ErrHeld = errors.New("another operation is in progress for this application")

const (
	// StaleAfter is when a lock is assumed to belong to a crashed process.
	// Generous on purpose: one deploy may legitimately run for the docker build
	// timeout (30 min) plus compose up (5 min) plus git and image cleanup, and
	// stealing a live deploy's lock reintroduces exactly the corruption this
	// package prevents.
	StaleAfter = 90 * time.Minute

	lockSuffix = ".deploy.lock"
)

var (
	// held tracks locks this process owns, so a SIGINT/SIGTERM handler can
	// release them. Without that, Ctrl-C at any of the deploy wizard's several
	// interactive prompts left a lock behind with a FRESH mtime — blocking
	// every further deploy AND `remove` of that app until the operator deleted
	// the file by hand or waited out StaleAfter.
	heldMu     sync.Mutex
	held       = map[string]string{} // lock path -> token
	signalOnce sync.Once
)

// Acquire takes the lock for appName. The returned release is idempotent and
// only removes the lock while it still carries this acquisition's token.
//
// It never blocks: contention returns an error wrapping ErrHeld immediately.
// Waiting would read as a hang at an interactive prompt, and the one caller
// that genuinely should wait retries around ErrHeld instead.
//
// The core O_EXCL/token-check/stale-recovery logic is delegated to the shared
// lockfile primitive. This package layers on top: process-wide held-lock
// tracking (for SIGINT cleanup) and the StaleAfter window sized for deploy
// operations (90 min — a deploy may run for the docker build timeout plus
// compose up plus git/image cleanup).
func Acquire(appName string) (release func(), err error) {
	// Locks live in the STATE directory, not AppsDir.
	//
	// AppsDir is /opt/simpledeploy/apps, which only root can create. Putting
	// locks there meant taking a lock could fail with
	// "mkdir /opt/simpledeploy: permission denied" — and because the lock is
	// taken before anything else, that error replaced the real one: a
	// non-root `simpledeploy stop <typo>` reported a permission problem
	// instead of "app not found". The state directory is the right home: every
	// command already depends on it being writable (state.json lives there),
	// it is created on demand, and the service container bind-mounts it at the
	// same host path with SIMPLEDEPLOY_STATE_DIR pointing at it — so a
	// containerised webhook deploy and a host CLI still contend on the same
	// file. Scoping the lock to the state directory also matches the scope of
	// the state itself: two users with different state files are separate
	// installs with separate apps, not racers.
	dir := config.HomeDataDir()
	lockPath := filepath.Join(dir, "."+appName+lockSuffix)

	lockRelease, token, err := lockfile.Acquire(lockPath, lockfile.Options{
		StaleAfter: StaleAfter,
		Retry:      lockfile.FailFast,
	})
	if err != nil {
		if errors.Is(err, lockfile.ErrHeld) {
			return nil, fmt.Errorf("%w: %s (lock held %s; %s).\n"+
				"Wait for it to finish, or check `docker ps` / `simpledeploy logs %s` first — "+
				"deleting %s while an operation really is running can corrupt the deployment",
				ErrHeld, appName, lockAge(lockPath), describeHolder(lockPath), appName, lockPath)
		}
		return nil, err
	}

	// Track this acquisition for the SIGINT/SIGTERM signal handler.
	heldMu.Lock()
	held[lockPath] = token
	heldMu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			// Remove from the process-wide registry BEFORE releasing the file
			// lock. If ReleaseAll runs concurrently, it must not see a stale
			// entry pointing at a file we already removed — the token check
			// in ReleaseAll would pass (the file is gone, ReadFile fails, it
			// skips) but the delete-after-release order is still a race window
			// where a second ReleaseAll could observe the entry and act on it.
			heldMu.Lock()
			delete(held, lockPath)
			heldMu.Unlock()
			lockRelease()
		})
	}, nil
}

// InstallSignalCleanup arranges for locks this process holds to be released on
// SIGINT/SIGTERM, then calls exit.
//
// Go's default signal behaviour terminates without running deferred functions,
// and the deploy wizard blocks on human input at roughly a dozen prompts while
// holding a lock. Ctrl-C there used to leave that lock behind with a fresh
// mtime, so the operator could neither retry the deploy nor `remove` the app
// for the next 90 minutes.
//
// exit is a parameter so tests can observe the path without terminating the
// test binary. Safe to call more than once; only the first call installs.
func InstallSignalCleanup(exit func(code int)) {
	signalOnce.Do(func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-ch
			ReleaseAll()
			// 130 is the conventional shell status for SIGINT.
			exit(130)
		}()
	})
}

// ReleaseAll releases every lock this process currently holds. Exported for the
// signal handler and for tests.
func ReleaseAll() {
	heldMu.Lock()
	snapshot := make(map[string]string, len(held))
	for path, token := range held {
		snapshot[path] = token
	}
	heldMu.Unlock()

	for path, token := range snapshot {
		// Token-checked release: only remove the lock if it still carries our
		// token. If someone else now owns the path (ours was recovered as
		// stale), removing it would tear down a live holder's lock.
		data, err := os.ReadFile(path)
		if err != nil || string(data) != token {
			continue
		}
		os.Remove(path)
	}
}

// lockAge reports how long the lock file at lockPath has been held, based on
// its mtime — the same source of truth the lockfile primitive's stale
// recovery uses, so the age shown to the operator matches what "stale" means
// for this lock. Returns "unknown" if the file cannot be stat-ed (e.g. it
// vanished between the failed acquire and this message).
func lockAge(lockPath string) string {
	info, err := os.Stat(lockPath)
	if err != nil {
		return "unknown"
	}
	return time.Since(info.ModTime()).Round(time.Second).String()
}

// describeHolder renders what is known about the current holder.
//
// Deliberately does NOT present the recorded pid as something to look up: a
// webhook-triggered deploy runs inside the qd-service container, so the pid
// belongs to that container's namespace and is often 1. An operator who checked
// `ps 1` on the host would see systemd, conclude the lock was a crash leftover,
// delete it, and re-enable the concurrent-deploy corruption this package exists
// to prevent.
func describeHolder(lockPath string) string {
	data, err := os.ReadFile(lockPath)
	if err != nil || len(data) == 0 {
		return "holder unknown"
	}
	return "holder may be a webhook deploy in the qd-service container"
}
