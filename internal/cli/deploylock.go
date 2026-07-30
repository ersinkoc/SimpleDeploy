package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	cfgpkg "github.com/ersinkoc/SimpleDeploy/internal/config"
)

// Per-app deploy lock, held across a whole deploy or redeploy.
//
// Nothing used to serialize deploys BETWEEN PROCESSES. The webhook server's
// deployLocks map only covers deploys it starts itself, so an operator running
// `simpledeploy redeploy X` by hand while a webhook-triggered deploy of X was
// in flight ran fully concurrently with it. Observed consequences:
//
//   - Both processes `git pull` the same apps/X/source tree; git's own
//     index.lock makes one of them fail nondeterministically.
//   - Redeploy snapshots the compose file before rewriting the image line, and
//     restores that snapshot on failure. With a peer in the middle, the loser's
//     rollback reverted the winner's SUCCESSFUL deploy — re-`compose up`-ing the
//     old image while state.json claimed the new one was running.
//   - The winner's image prune (`docker rmi` of all but the newest few) could
//     delete the image the peer had just built, so the peer's `compose up`
//     failed on a missing image and triggered exactly the rollback above.
//
// It also closes the check-then-act in `deploy`: two interactive sessions
// choosing the same app name both passed the "already exists" check (neither
// had saved state yet), and the loser's abort cleanup deleted the winner's
// cloned source and .env mid-build.
//
// Design notes:
//
//   - The lock file lives in AppsDir, NOT inside the app directory: deploy's
//     abort path removes the app directory wholesale, which would drop the lock
//     out from under a live holder.
//   - Contention fails fast with the holder's identity rather than waiting. A
//     deploy legitimately takes minutes, so queuing behind one at the CLI would
//     look like a hang; the webhook path has its own queueing (deployPending)
//     and reports the failure in its log.
//   - Release is token-checked, the same lesson as lockStateFile: removing by
//     path alone can delete a lock a different live process created after ours
//     was recovered as stale.
const (
	// deployLockStale is when a lock is assumed to belong to a crashed process.
	// Generous on purpose: a single deploy may legitimately run for the docker
	// build timeout (30 min) plus compose up (5 min) plus git and image
	// cleanup, and stealing a live deploy's lock is worse than making an
	// operator wait or clear it by hand.
	deployLockStale  = 90 * time.Minute
	deployLockSuffix = ".deploy.lock"
)

// acquireDeployLock takes the per-app deploy lock. The returned release func is
// safe to call exactly once, and is a no-op if another holder has taken over.
func acquireDeployLock(appName string) (release func(), err error) {
	dir := cfgpkg.AppsDir()
	if err := osMkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create %s: %w", dir, err)
	}
	lockPath := filepath.Join(dir, "."+appName+deployLockSuffix)

	// pid + nanosecond start stamp: identifies this acquisition for the
	// token-checked release, and gives a human something to look up.
	token := fmt.Sprintf("%d %d\n", os.Getpid(), time.Now().UnixNano())

	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			f.WriteString(token)
			f.Close()
			return func() { releaseDeployLock(lockPath, token) }, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("failed to create deploy lock %s: %w", lockPath, err)
		}

		// Held. Steal it only if it is old enough to be a crash leftover, and
		// require the mtime to be unchanged across the re-stat so we cannot
		// remove a fresh lock another recoverer just created.
		info, statErr := os.Stat(lockPath)
		if statErr != nil {
			// Vanished between the failed create and the stat — the holder
			// released it. Retry once.
			continue
		}
		age := time.Since(info.ModTime())
		if age <= deployLockStale {
			return nil, fmt.Errorf(
				"a deploy for %q is already in progress (%s, started %s ago).\n"+
					"Wait for it to finish. If you are certain no deploy is running, remove %s",
				appName, describeLockHolder(lockPath), age.Round(time.Second), lockPath)
		}
		if info2, statErr2 := os.Stat(lockPath); statErr2 == nil && info2.ModTime().Equal(info.ModTime()) {
			os.Remove(lockPath)
		}
	}

	return nil, fmt.Errorf("could not acquire the deploy lock for %q (%s)", appName, lockPath)
}

// releaseDeployLock removes the lock only while it still carries our token.
func releaseDeployLock(lockPath, token string) {
	data, err := os.ReadFile(lockPath)
	if err != nil || string(data) != token {
		return
	}
	os.Remove(lockPath)
}

// describeLockHolder renders the lock file's contents for an error message.
// Best-effort: an unreadable or unexpected file yields a generic description
// rather than failing the caller that is already reporting a problem.
func describeLockHolder(lockPath string) string {
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return "holder unknown"
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return "holder unknown"
	}
	if pid, convErr := strconv.Atoi(fields[0]); convErr == nil {
		return fmt.Sprintf("pid %d", pid)
	}
	return "holder unknown"
}
