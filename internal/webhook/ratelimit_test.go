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
	rl := newRateLimiter(2, 80*time.Millisecond)
	defer rl.stop()

	const ip = "203.0.113.9"

	if !rl.allow(ip) || !rl.allow(ip) {
		t.Fatal("first two requests should be allowed")
	}
	if rl.allow(ip) {
		t.Fatal("third request within the window should be limited")
	}

	// Keep sending during the window so lastSeen is continuously refreshed.
	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		rl.allow(ip)
		time.Sleep(10 * time.Millisecond)
	}

	// The window has now rolled over at least once. A correct fixed-window
	// limiter lets the client back in; the buggy one never would.
	allowedAgain := false
	for i := 0; i < 5; i++ {
		if rl.allow(ip) {
			allowedAgain = true
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if !allowedAgain {
		t.Error("client with steady traffic was locked out permanently")
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
