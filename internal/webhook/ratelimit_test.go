package webhook

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestRateLimiter_WindowIsNotExtendedByTraffic is the regression test for a
// limiter that could permanently lock out an active client.
//
// The old implementation compared "time since lastSeen" against the window and
// refreshed lastSeen on every request, so the window only elapsed after a full
// period of total silence. A caller sending steady traffic used up its budget
// and was then blocked forever. Here the window is short and requests keep
// arriving across it — the client must be allowed again once the window rolls
// over, not blocked indefinitely.
func TestRateLimiter_WindowIsNotExtendedByTraffic(t *testing.T) {
	// The window is deliberately generous relative to the polling interval
	// below. A tight window makes the test sensitive to scheduler jitter on a
	// loaded CI runner (especially under -race), and a rate limiter that fails
	// intermittently in CI is worse than no test at all. Total runtime is
	// bounded at roughly window + probeBudget.
	const window = 200 * time.Millisecond
	const probeEvery = 25 * time.Millisecond

	rl := newRateLimiter(2, window)
	defer rl.stop()

	const ip = "203.0.113.9"

	if !rl.allow(ip) || !rl.allow(ip) {
		t.Fatal("first two requests should be allowed")
	}
	if rl.allow(ip) {
		t.Fatal("third request within the window should be limited")
	}

	// Send continuously for longer than one full window. Every call refreshes
	// lastSeen, which is exactly what the old implementation compared against —
	// so under the bug the window can never elapse and the client stays blocked
	// for as long as it keeps talking.
	//
	// The probe runs for 3x the window so at least two rollovers are crossed
	// regardless of how the sleeps land, and it records whether the limiter
	// ever let the client back in.
	allowedAgain := false
	deadline := time.Now().Add(3 * window)
	for time.Now().Before(deadline) {
		if rl.allow(ip) {
			allowedAgain = true
			break
		}
		time.Sleep(probeEvery)
	}

	if !allowedAgain {
		t.Errorf("a client sending steady traffic was never allowed again after %v "+
			"(window %v) — the counting window is being extended by traffic", 3*window, window)
	}
}

func TestRateLimiter_PerIPIsolation(t *testing.T) {
	rl := newRateLimiter(1, time.Minute)
	defer rl.stop()

	if !rl.allow("198.51.100.1") {
		t.Fatal("first IP should be allowed")
	}
	if rl.allow("198.51.100.1") {
		t.Fatal("first IP should now be limited")
	}
	if !rl.allow("198.51.100.2") {
		t.Error("a different IP must have its own budget")
	}
}

// TestHandleWebhook_RefusesPushWithoutSecret covers the case where an operator
// has no webhook secret configured. Every signature-verification branch is
// skipped in that state, so before this guard any anonymous POST could make
// the server pull and execute repository code.
func TestHandleWebhook_RefusesPushWithoutSecret(t *testing.T) {
	webhookInitState(t)
	srv := NewServer(9000, "")
	deployed := false
	srv.SetDeployHandler(func(ctx context.Context, appName string) error {
		deployed = true
		return nil
	})

	req := httptest.NewRequest(http.MethodPost, "/_qd/webhook/myapp",
		strings.NewReader(`{"ref":"refs/heads/main"}`))
	req.Header.Set("X-GitHub-Event", "push")
	rec := httptest.NewRecorder()

	srv.handleWebhook(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated push = %d, want 401", rec.Code)
	}
	if deployed {
		t.Error("deploy handler must not run for an unauthenticated push")
	}
}
