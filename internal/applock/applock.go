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
func Acquire(appName string) (release func(), err error) {
	dir := config.AppsDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create %s: %w", dir, err)
	}
	lockPath := filepath.Join(dir, "."+appName+lockSuffix)

	// pid + nanosecond stamp identifies this acquisition for the token-checked
	// release below.
	token := fmt.Sprintf("%d %d\n", os.Getpid(), time.Now().UnixNano())

	for attempt := 0; attempt < 2; attempt++ {
		f, createErr := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if createErr == nil {
			// Write errors are fatal to the acquisition, not ignorable. An
			// empty or short-written lock file still EXISTS, so it blocks
			// everyone else, but its content no longer matches our token —
			// meaning our own release would decline to remove it and the app
			// would stay locked for StaleAfter. Fail loudly and clean up
			// instead.
			if _, werr := f.WriteString(token); werr != nil {
				f.Close()
				os.Remove(lockPath)
				return nil, fmt.Errorf("failed to write deploy lock %s: %w", lockPath, werr)
			}
			if serr := f.Sync(); serr != nil {
				f.Close()
				os.Remove(lockPath)
				return nil, fmt.Errorf("failed to flush deploy lock %s: %w", lockPath, serr)
			}
			if cerr := f.Close(); cerr != nil {
				os.Remove(lockPath)
				return nil, fmt.Errorf("failed to close deploy lock %s: %w", lockPath, cerr)
			}

			heldMu.Lock()
			held[lockPath] = token
			heldMu.Unlock()

			var once sync.Once
			return func() { once.Do(func() { releaseLock(lockPath, token) }) }, nil
		}
		if !os.IsExist(createErr) {
			return nil, fmt.Errorf("failed to create deploy lock %s: %w", lockPath, createErr)
		}

		info, statErr := os.Stat(lockPath)
		if statErr != nil {
			// Vanished between the failed create and the stat — the holder
			// released it. Retry.
			continue
		}
		age := time.Since(info.ModTime())
		if age <= StaleAfter {
			return nil, fmt.Errorf("%w: %s (lock held %s, %s).\n"+
				"Wait for it to finish, or check `docker ps` / `simpledeploy logs %s` first — "+
				"deleting %s while an operation really is running can corrupt the deployment",
				ErrHeld, appName, age.Round(time.Second), describeHolder(lockPath), appName, lockPath)
		}
		// Steal it only if the mtime is unchanged across a re-stat: another
		// recoverer may have replaced it with a fresh lock in between, and
		// removing that would let two writers run at once.
		if info2, statErr2 := os.Stat(lockPath); statErr2 == nil && info2.ModTime().Equal(info.ModTime()) {
			os.Remove(lockPath)
		}
	}

	return nil, fmt.Errorf("could not acquire the deploy lock for %q (%s)", appName, lockPath)
}

// releaseLock removes the lock only while it still carries our token, and stops
// tracking it either way.
func releaseLock(lockPath, token string) {
	heldMu.Lock()
	delete(held, lockPath)
	heldMu.Unlock()

	data, err := os.ReadFile(lockPath)
	if err != nil || string(data) != token {
		// Someone else owns this path now (ours was recovered as stale).
		// Removing it would tear down a live holder's lock.
		return
	}
	os.Remove(lockPath)
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
		releaseLock(path, token)
	}
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
