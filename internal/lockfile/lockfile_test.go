package lockfile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquire_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lock")

	release, token, err := Acquire(path, Options{
		StaleAfter: 30 * time.Second,
		Retry:      FailFast,
	})
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	if token == "" {
		t.Error("token should not be empty on success")
	}
	defer release()

	// File must exist with the token as content.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(data) != token {
		t.Errorf("file content = %q, want token %q", string(data), token)
	}
}

func TestAcquire_FailFast_ErrHeld(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lock")

	release1, _, err := Acquire(path, Options{
		StaleAfter: 30 * time.Second,
		Retry:      FailFast,
	})
	if err != nil {
		t.Fatalf("first Acquire failed: %v", err)
	}
	defer release1()

	// Second acquire must fail immediately with ErrHeld.
	_, _, err2 := Acquire(path, Options{
		StaleAfter: 30 * time.Second,
		Retry:      FailFast,
	})
	if !errors.Is(err2, ErrHeld) {
		t.Errorf("second Acquire error = %v, want ErrHeld", err2)
	}
}

func TestAcquire_ReleaseRemovesLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lock")

	release, _, err := Acquire(path, Options{
		StaleAfter: 30 * time.Second,
		Retry:      FailFast,
	})
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("lock file should exist before release")
	}

	release()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("lock file should be removed after release")
	}
}

func TestAcquire_ReleaseIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lock")

	release, _, err := Acquire(path, Options{
		StaleAfter: 30 * time.Second,
		Retry:      FailFast,
	})
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	release()
	release() // must not panic

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("lock file should be removed after double release")
	}
}

func TestAcquire_AfterRelease_CanReacquire(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lock")

	opts := Options{StaleAfter: 30 * time.Second, Retry: FailFast}

	release, _, err := Acquire(path, opts)
	if err != nil {
		t.Fatalf("first Acquire failed: %v", err)
	}
	release()

	release2, _, err := Acquire(path, opts)
	if err != nil {
		t.Fatalf("second Acquire after release failed: %v", err)
	}
	release2()
}

func TestAcquire_StaleRecovery(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stale.lock")

	// Seed a stale lock file.
	if err := os.WriteFile(path, []byte("99999 1\n"), 0600); err != nil {
		t.Fatalf("seed lock: %v", err)
	}
	old := time.Now().Add(-2 * time.Minute)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	release, _, err := Acquire(path, Options{
		StaleAfter: 30 * time.Second,
		Retry:      FailFast,
	})
	if err != nil {
		t.Fatalf("Acquire should recover stale lock: %v", err)
	}
	release()
}

func TestAcquire_DoesNotStealFreshLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fresh.lock")

	// Seed a lock that is NOT stale.
	if err := os.WriteFile(path, []byte("12345 67890\n"), 0600); err != nil {
		t.Fatalf("seed lock: %v", err)
	}

	_, _, err := Acquire(path, Options{
		StaleAfter: 30 * time.Second,
		Retry:      FailFast,
	})
	if !errors.Is(err, ErrHeld) {
		t.Errorf("Acquire on a fresh lock should return ErrHeld, got %v", err)
	}

	// The existing lock must survive — not removed by the failed acquire.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("lock file disappeared: %v", err)
	}
	if string(data) != "12345 67890\n" {
		t.Errorf("lock content was modified: %q", string(data))
	}
}

func TestAcquire_ReleaseLeavesForeignLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lock")

	release, _, err := Acquire(path, Options{
		StaleAfter: 30 * time.Second,
		Retry:      FailFast,
	})
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	// Simulate another process taking over this lock path.
	foreign := []byte("4242 1700000000\n")
	if err := os.WriteFile(path, foreign, 0600); err != nil {
		t.Fatalf("overwrite lock: %v", err)
	}

	release()

	// Our token no longer matches, so the foreign lock must survive.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("foreign lock was removed: %v", err)
	}
	if string(data) != string(foreign) {
		t.Errorf("lock content = %q, want foreign %q", string(data), foreign)
	}
}

func TestAcquire_CreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "subdir", "nested", "test.lock")

	release, _, err := Acquire(nested, Options{
		StaleAfter: 30 * time.Second,
		Retry:      FailFast,
	})
	if err != nil {
		t.Fatalf("Acquire should create parent dirs: %v", err)
	}
	defer release()

	if _, err := os.Stat(nested); err != nil {
		t.Errorf("lock file should exist: %v", err)
	}
}

func TestAcquire_Spin_AcquiresAfterRelease(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spin.lock")

	opts := Options{
		StaleAfter:   30 * time.Second,
		Retry:        Spin,
		SpinInterval: 5 * time.Millisecond,
		MaxRetries:   100,
	}

	release1, _, err := Acquire(path, opts)
	if err != nil {
		t.Fatalf("first Acquire failed: %v", err)
	}

	// Release shortly after starting a second acquire attempt.
	go func() {
		time.Sleep(20 * time.Millisecond)
		release1()
	}()

	release2, _, err := Acquire(path, opts)
	if err != nil {
		t.Fatalf("second Acquire should succeed after release: %v", err)
	}
	release2()
}

func TestAcquire_Spin_TimeoutAfterMaxRetries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "held.lock")

	// Hold the lock permanently.
	release, _, err := Acquire(path, Options{
		StaleAfter: 30 * time.Second,
		Retry:      FailFast,
	})
	if err != nil {
		t.Fatalf("seed Acquire failed: %v", err)
	}
	defer release()

	// Try to acquire with a tiny retry budget — must time out.
	_, _, err = Acquire(path, Options{
		StaleAfter:   30 * time.Second,
		Retry:        Spin,
		SpinInterval: time.Millisecond,
		MaxRetries:   3,
	})
	if err == nil {
		t.Fatal("Acquire should fail when retry budget is exhausted")
	}
	if !errors.Is(err, ErrTimeout) {
		t.Errorf("error = %v, want ErrTimeout", err)
	}
}

func TestAcquire_TokenIsUnique(t *testing.T) {
	dir := t.TempDir()

	tokens := make(map[string]bool)
	for i := 0; i < 10; i++ {
		path := filepath.Join(dir, "lock"+string(rune('a'+i))+".lock")
		release, token, err := Acquire(path, Options{
			StaleAfter: 30 * time.Second,
			Retry:      FailFast,
		})
		if err != nil {
			t.Fatalf("Acquire %d failed: %v", i, err)
		}
		defer release()
		if tokens[token] {
			t.Errorf("token %q is not unique across acquisitions", token)
		}
		tokens[token] = true
	}
}
