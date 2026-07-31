// Package lockfile provides a cross-process advisory lock primitive using
// O_EXCL file creation with token-checked release and stale-lock recovery.
//
// It is the shared implementation behind internal/state (state.json lock)
// and internal/applock (per-app deploy lock). Those two independently
// implemented the same subtle logic — O_EXCL create, token write,
// token-checked release, stat → re-stat-same-mtime → remove stale — and
// the subtle parts (exactly the parts that had bugs) were duplicated.
//
// Callers configure the staleness window and retry policy via Options; the
// lockfile package handles the rest.
package lockfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// ErrHeld reports that another process holds the lock. Callers that can
// usefully wait (the unattended webhook path) test for it with errors.Is
// and retry; interactive callers surface it to the operator.
var ErrHeld = errors.New("another process holds the lock")

// ErrTimeout reports that the retry budget was exhausted before the lock
// became available.
var ErrTimeout = errors.New("timed out waiting for lock")

var tokenSeq atomic.Uint64

// RetryPolicy controls how Acquire retries on contention.
type RetryPolicy int

const (
	// FailFast returns immediately on contention (no retries). Used by
	// applock, where waiting would read as a hang at an interactive prompt.
	FailFast RetryPolicy = iota
	// Spin retries with a short sleep until the budget expires. Used by
	// state, where contending writers are brief (milliseconds) and failing
	// fast would corrupt every concurrent state mutation.
	Spin
)

// Options configures a lockfile's behaviour.
type Options struct {
	// StaleAfter is how old a lock file must be before it is assumed to
	// belong to a crashed process and recovered. Setting this too low
	// steals a live holder's lock; too high and a crash blocks everyone.
	StaleAfter time.Duration

	// Retry is FailFast (return ErrHeld immediately) or Spin (retry until
	// the budget expires).
	Retry RetryPolicy

	// SpinInterval is the sleep between spin retries. Defaults to 10ms
	// when Retry is Spin and the value is zero.
	SpinInterval time.Duration

	// MaxRetries bounds the spin retry count. Zero means 100 (matching
	// the original state.lockStateFile loop).
	MaxRetries int
}

// Acquire creates an advisory lock file at path using O_EXCL, writes a
// unique token into it, and returns an idempotent release function along
// with the token (for callers that need to track ownership independently).
//
// The release function removes the lock only while it still carries this
// acquisition's token, so it cannot tear down a lock that a different
// process now owns (e.g. after our lock was recovered as stale).
//
// On contention (EEXIST): if Retry is FailFast, returns an error wrapping
// ErrHeld. If Retry is Spin, sleeps and retries until MaxRetries or
// StaleAfter triggers recovery.
//
// Stale recovery uses a stat → re-stat-same-mtime → remove pattern to
// avoid a TOCTOU window where two recoverers race to replace a stale lock:
// the second stat confirms the mtime is unchanged before removing, and the
// O_EXCL create that follows still serializes whoever wins.
func Acquire(path string, opts Options) (release func(), token string, err error) {
	if opts.SpinInterval == 0 {
		opts.SpinInterval = 10 * time.Millisecond
	}
	if opts.MaxRetries == 0 {
		opts.MaxRetries = 100
	}

	if err := os.MkdirAll(dir(path), 0700); err != nil {
		return nil, "", fmt.Errorf("failed to create directory for %s: %w", path, err)
	}

	token = fmt.Sprintf("%d %d %d\n", os.Getpid(), time.Now().UnixNano(), tokenSeq.Add(1))

	maxAttempts := 2 // FailFast: 1 initial + 1 retry after stale recovery
	if opts.Retry == Spin {
		maxAttempts = opts.MaxRetries
	}

	for attempt := 0; attempt < maxAttempts; attempt++ {
		f, createErr := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if createErr == nil {
			if _, werr := f.WriteString(token); werr != nil {
				f.Close()
				os.Remove(path)
				return nil, "", fmt.Errorf("failed to write lock %s: %w", path, werr)
			}
			if serr := f.Sync(); serr != nil {
				f.Close()
				os.Remove(path)
				return nil, "", fmt.Errorf("failed to sync lock %s: %w", path, serr)
			}
			if cerr := f.Close(); cerr != nil {
				os.Remove(path)
				return nil, "", fmt.Errorf("failed to close lock %s: %w", path, cerr)
			}

			var once sync.Once
			return func() {
				once.Do(func() { releaseLock(path, token) })
			}, token, nil
		}

		if !os.IsExist(createErr) {
			return nil, "", fmt.Errorf("failed to create lock %s: %w", path, createErr)
		}

		info, statErr := os.Stat(path)
		if statErr != nil {
			// Vanished between the failed create and the stat — the holder
			// released it. Retry.
			continue
		}
		age := time.Since(info.ModTime())
		if age <= opts.StaleAfter {
			if opts.Retry == FailFast {
				return nil, "", fmt.Errorf("%w: lock held %s at %s", ErrHeld, age.Round(time.Second), path)
			}
			time.Sleep(opts.SpinInterval)
			continue
		}

		// Stale: steal it only if the mtime is unchanged across a re-stat.
		if info2, statErr2 := os.Stat(path); statErr2 == nil && info2.ModTime().Equal(info.ModTime()) {
			os.Remove(path)
		}
	}

	if opts.Retry == FailFast {
		return nil, "", fmt.Errorf("%w: %s", ErrHeld, path)
	}
	return nil, "", fmt.Errorf("%w: %s after %d retries", ErrTimeout, path, maxAttempts)
}

// releaseLock removes the lock only while it still carries the given token.
func releaseLock(path, token string) {
	data, err := os.ReadFile(path)
	if err != nil || string(data) != token {
		// Someone else owns this path now (ours was recovered as stale).
		return
	}
	os.Remove(path)
}

// dir returns the directory component of path.
func dir(path string) string {
	return filepath.Dir(path)
}
