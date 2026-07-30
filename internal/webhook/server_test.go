package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ersinkoc/SimpleDeploy/internal/applock"
	"github.com/ersinkoc/SimpleDeploy/internal/state"
)

func webhookInitState(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	state.InitState(dir)
}

func webhookSaveApp(t *testing.T, name, branch string) {
	t.Helper()
	app := state.NewAppConfig()
	app.Name = name
	app.Branch = branch
	app.Domain = fmt.Sprintf("%s.example.com", name)
	app.Port = 3000
	app.Type = "node"
	app.Status = "running"
	// Push-to-deploy on: these fixtures exist to exercise the deploy path, and
	// the server now honours the per-app opt-out (a disabled app answers 403
	// and never deploys — see TestHandleWebhook_WebhookDisabled).
	app.WebhookEnabled = true
	if err := state.SaveApp(app); err != nil {
		t.Fatalf("Failed to save test app: %v", err)
	}
}

func webhookSignBody(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestHandleWebhook_GetMethod(t *testing.T) {
	srv := NewServer(9000, "secret")
	req := httptest.NewRequest(http.MethodGet, "/_qd/webhook/myapp", nil)
	rec := httptest.NewRecorder()
	srv.handleWebhook(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET should return 405, got %d", rec.Code)
	}
}

func TestHandleWebhook_EmptyAppName(t *testing.T) {
	srv := NewServer(9000, "secret")
	req := httptest.NewRequest(http.MethodPost, "/_qd/webhook/", strings.NewReader(""))
	rec := httptest.NewRecorder()
	srv.handleWebhook(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("Empty app name should return 400, got %d", rec.Code)
	}
}

func TestHandleWebhook_InvalidSignature(t *testing.T) {
	srv := NewServer(9000, "secret")
	body := `{"ref":"refs/heads/main"}`
	req := httptest.NewRequest(http.MethodPost, "/_qd/webhook/myapp", strings.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", "sha256=invalid")
	req.Header.Set("X-GitHub-Event", "push")
	rec := httptest.NewRecorder()
	srv.handleWebhook(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Invalid signature should return 401, got %d", rec.Code)
	}
}

func TestHandleWebhook_NoSecret(t *testing.T) {
	webhookInitState(t)
	srv := NewServer(9000, "")
	body := `{"ref":"refs/heads/main"}`
	req := httptest.NewRequest(http.MethodPost, "/_qd/webhook/myapp", strings.NewReader(body))
	req.Header.Set("X-GitHub-Event", "ping")
	rec := httptest.NewRecorder()
	srv.handleWebhook(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("Non-push with no secret should return 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ignored") {
		t.Errorf("Body should contain 'ignored', got %q", rec.Body.String())
	}
}

func TestHandleWebhook_NonPushEvent(t *testing.T) {
	webhookInitState(t)
	srv := NewServer(9000, "secret")
	body := `{"ref":"refs/heads/main"}`
	sig := webhookSignBody([]byte(body), "secret")
	req := httptest.NewRequest(http.MethodPost, "/_qd/webhook/myapp", strings.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", sig)
	req.Header.Set("X-GitHub-Event", "ping")
	rec := httptest.NewRecorder()
	srv.handleWebhook(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("Non-push should return 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ignored") {
		t.Errorf("Body should contain 'ignored', got %q", rec.Body.String())
	}
}

func TestHandleWebhook_AppNotFound(t *testing.T) {
	webhookInitState(t)
	srv := NewServer(9000, "secret")
	body := `{"ref":"refs/heads/main"}`
	sig := webhookSignBody([]byte(body), "secret")
	req := httptest.NewRequest(http.MethodPost, "/_qd/webhook/nonexistent", strings.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", sig)
	req.Header.Set("X-GitHub-Event", "push")
	rec := httptest.NewRecorder()
	srv.handleWebhook(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("Nonexistent app should return 404, got %d", rec.Code)
	}
}

func TestHandleWebhook_WrongBranch(t *testing.T) {
	webhookInitState(t)
	webhookSaveApp(t, "myapp", "main")
	srv := NewServer(9000, "secret")
	body := `{"ref":"refs/heads/develop"}`
	sig := webhookSignBody([]byte(body), "secret")
	req := httptest.NewRequest(http.MethodPost, "/_qd/webhook/myapp", strings.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", sig)
	req.Header.Set("X-GitHub-Event", "push")
	rec := httptest.NewRecorder()
	srv.handleWebhook(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("Wrong branch should return 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Branch ignored") {
		t.Errorf("Body should mention 'Branch ignored', got %q", rec.Body.String())
	}
}

func TestHandleWebhook_Success(t *testing.T) {
	webhookInitState(t)
	webhookSaveApp(t, "myapp", "main")

	// The handler runs on a goroutine the request handler spawns, so the call
	// has to be reported over a channel. A plain bool written there and read
	// here is a data race even with a sleep in between — the race detector
	// fails the test, and without it the read can see a stale value.
	deployCalled := make(chan string, 1)
	srv := NewServer(9000, "secret")
	srv.SetDeployHandler(func(ctx context.Context, appName string) error {
		deployCalled <- appName
		return nil
	})

	body := `{"ref":"refs/heads/main"}`
	sig := webhookSignBody([]byte(body), "secret")
	req := httptest.NewRequest(http.MethodPost, "/_qd/webhook/myapp", strings.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", sig)
	req.Header.Set("X-GitHub-Event", "push")
	rec := httptest.NewRecorder()
	srv.handleWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Success should return 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Deploy triggered") {
		t.Errorf("Body should contain 'Deploy triggered', got %q", rec.Body.String())
	}

	select {
	case got := <-deployCalled:
		if got != "myapp" {
			t.Errorf("Deploy called with %q, want 'myapp'", got)
		}
	case <-time.After(2 * time.Second):
		t.Error("Deploy handler should have been called")
	}
}

func TestHandleWebhook_NoDeployHandler(t *testing.T) {
	webhookInitState(t)
	webhookSaveApp(t, "myapp", "main")
	srv := NewServer(9000, "secret")

	body := `{"ref":"refs/heads/main"}`
	sig := webhookSignBody([]byte(body), "secret")
	req := httptest.NewRequest(http.MethodPost, "/_qd/webhook/myapp", strings.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", sig)
	req.Header.Set("X-GitHub-Event", "push")
	rec := httptest.NewRecorder()
	srv.handleWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Should return 200 even without deploy handler, got %d", rec.Code)
	}
}

func TestHandleWebhook_DeployError(t *testing.T) {
	webhookInitState(t)
	webhookSaveApp(t, "myapp", "main")

	srv := NewServer(9000, "secret")
	srv.SetDeployHandler(func(ctx context.Context, appName string) error {
		return fmt.Errorf("deploy failed")
	})

	body := `{"ref":"refs/heads/main"}`
	sig := webhookSignBody([]byte(body), "secret")
	req := httptest.NewRequest(http.MethodPost, "/_qd/webhook/myapp", strings.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", sig)
	req.Header.Set("X-GitHub-Event", "push")
	rec := httptest.NewRecorder()
	srv.handleWebhook(rec, req)

	// Response is still 200 (deploy runs async)
	if rec.Code != http.StatusOK {
		t.Errorf("Should return 200, got %d", rec.Code)
	}
}

// TestHandleWebhook_TagPushIgnored pins that a TAG push does not deploy.
//
// GitHub delivers tag pushes as `X-GitHub-Event: push` with
// `ref: refs/tags/...`, and its webhook UI cannot filter them out. Because a
// tag ref yields no branch name, the branch check used to be skipped entirely
// and every tag push redeployed the app despite the operator having configured
// a specific branch.
func TestHandleWebhook_TagPushIgnored(t *testing.T) {
	webhookInitState(t)
	webhookSaveApp(t, "myapp", "main")

	deployTriggered := make(chan struct{}, 1)
	srv := NewServer(9000, "secret")
	srv.SetDeployHandler(func(ctx context.Context, appName string) error {
		deployTriggered <- struct{}{}
		return nil
	})

	body := `{"ref":"refs/tags/v1.0.0"}`
	sig := webhookSignBody([]byte(body), "secret")
	req := httptest.NewRequest(http.MethodPost, "/_qd/webhook/myapp", strings.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", sig)
	req.Header.Set("X-GitHub-Event", "push")
	rec := httptest.NewRecorder()
	srv.handleWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Tag push should be acknowledged with 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "not a branch") {
		t.Errorf("Body should say the ref was ignored, got %q", rec.Body.String())
	}

	select {
	case <-deployTriggered:
		t.Error("A tag push must not trigger a deploy")
	case <-time.After(250 * time.Millisecond):
	}
}

// TestHandleWebhook_FormEncodedPayload covers GitHub's
// application/x-www-form-urlencoded delivery mode, where the body is
// payload=<urlencoded JSON>. The ref used to be unparseable in that mode,
// which left branch empty and skipped the branch filter — so a repo using this
// content type redeployed on pushes to EVERY branch.
func TestHandleWebhook_FormEncodedPayload(t *testing.T) {
	webhookInitState(t)
	webhookSaveApp(t, "myapp", "main")

	deployTriggered := make(chan struct{}, 1)
	srv := NewServer(9000, "secret")
	srv.SetDeployHandler(func(ctx context.Context, appName string) error {
		deployTriggered <- struct{}{}
		return nil
	})

	// Push to a branch the app is NOT configured for: must be ignored.
	body := "payload=" + url.QueryEscape(`{"ref":"refs/heads/dev"}`)
	req := httptest.NewRequest(http.MethodPost, "/_qd/webhook/myapp", strings.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", webhookSignBody([]byte(body), "secret"))
	req.Header.Set("X-GitHub-Event", "push")
	rec := httptest.NewRecorder()
	srv.handleWebhook(rec, req)

	if !strings.Contains(rec.Body.String(), "Branch ignored") {
		t.Errorf("form-encoded push to 'dev' should be branch-filtered, got %q", rec.Body.String())
	}
	select {
	case <-deployTriggered:
		t.Error("form-encoded push to a non-configured branch must not deploy")
	case <-time.After(250 * time.Millisecond):
	}

	// The configured branch, same encoding: must deploy.
	body = "payload=" + url.QueryEscape(`{"ref":"refs/heads/main"}`)
	req = httptest.NewRequest(http.MethodPost, "/_qd/webhook/myapp", strings.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", webhookSignBody([]byte(body), "secret"))
	req.Header.Set("X-GitHub-Event", "push")
	rec = httptest.NewRecorder()
	srv.handleWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("form-encoded push to the configured branch should return 200, got %d", rec.Code)
	}
	select {
	case <-deployTriggered:
	case <-time.After(2 * time.Second):
		t.Error("form-encoded push to the configured branch should deploy")
	}
	srv.deployWg.Wait()
}

// TestHandleWebhook_WebhookDisabled pins the per-app opt-out. The flag was
// collected by the deploy wizard and displayed by `list`, but never consulted
// here — so an app deployed with push-to-deploy declined still auto-deployed.
func TestHandleWebhook_WebhookDisabled(t *testing.T) {
	webhookInitState(t)
	app := state.NewAppConfig()
	app.Name = "optout"
	app.Branch = "main"
	app.Domain = "optout.example.com"
	app.Port = 3000
	app.WebhookEnabled = false
	if err := state.SaveApp(app); err != nil {
		t.Fatalf("save app: %v", err)
	}

	deployTriggered := make(chan struct{}, 1)
	srv := NewServer(9000, "secret")
	srv.SetDeployHandler(func(ctx context.Context, appName string) error {
		deployTriggered <- struct{}{}
		return nil
	})

	body := `{"ref":"refs/heads/main"}`
	req := httptest.NewRequest(http.MethodPost, "/_qd/webhook/optout", strings.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", webhookSignBody([]byte(body), "secret"))
	req.Header.Set("X-GitHub-Event", "push")
	rec := httptest.NewRecorder()
	srv.handleWebhook(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected 403 for an app with push-to-deploy disabled, got %d", rec.Code)
	}
	select {
	case <-deployTriggered:
		t.Error("An app with WebhookEnabled=false must not be deployed")
	case <-time.After(250 * time.Millisecond):
	}
}

// TestHandleWebhook_OversizedPayload pins that an over-cap body is answered
// 413 rather than being silently truncated and then reported as a signature
// failure — which sent operators hunting for a secret mismatch that did not
// exist.
func TestHandleWebhook_OversizedPayload(t *testing.T) {
	webhookInitState(t)
	webhookSaveApp(t, "myapp", "main")

	srv := NewServer(9000, "secret")
	body := strings.Repeat("x", maxWebhookBody+1024)
	req := httptest.NewRequest(http.MethodPost, "/_qd/webhook/myapp", strings.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", webhookSignBody([]byte(body), "secret"))
	req.Header.Set("X-GitHub-Event", "push")
	rec := httptest.NewRecorder()
	srv.handleWebhook(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("Expected 413 for an oversized payload, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestClientIP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		want       string
	}{
		{"empty", "", ""},
		{"ipv4 with port", "192.168.1.1:54321", "192.168.1.1"},
		{"ipv6 bracketed with port", "[::1]:8080", "::1"},
		{"ipv6 full bracketed with port", "[2001:db8::1]:443", "2001:db8::1"},
		{"ipv6 bracketed no port", "[::1]", "::1"},
		{"ipv4 only no port", "10.0.0.5", "10.0.0.5"},
		{"ipv6 bare no port no brackets", "fe80::1", "fe80::1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clientIP(tt.remoteAddr); got != tt.want {
				t.Errorf("clientIP(%q) = %q, want %q", tt.remoteAddr, got, tt.want)
			}
		})
	}
}

// TestClientIP_BracketStripConsistency confirms that the bracketed and
// non-bracketed forms of the same IPv6 address normalise to the same
// rate-limiter key — the prior LastIndex-based approach left the brackets
// in for the "[host]" no-port case while stripping them from the
// "[host]:port" case, splitting the bucket.
func TestClientIP_BracketStripConsistency(t *testing.T) {
	withPort := clientIP("[::1]:8080")
	noPort := clientIP("[::1]")
	if withPort != noPort {
		t.Errorf("bracketed v6 with/without port should match: %q vs %q", withPort, noPort)
	}
}

// TestRateLimit_BudgetSurvivesRealisticPushVolume pins that the flood guard is
// not tight enough to throttle the traffic the server exists to serve.
//
// Behind the reverse proxy every delivery arrives from the same bridge-gateway
// address, so the per-IP bucket is effectively ONE GLOBAL bucket, and it is
// necessarily charged before signature verification (verification needs the
// body). At the previous 60/min that global ceiling was low enough for a busy
// multi-app server's own pushes to trip it, and once tripped, genuine
// deliveries got 429 and push-to-deploy silently stopped.
//
// Note the limitation this test does NOT claim to fix: with one shared bucket
// key, sustained anonymous traffic can still exhaust the budget. Deploy
// authorization is the HMAC gate, and the port should be firewalled to the
// proxy (documented in CLAUDE.md). Charging failures to a stricter second
// bucket does not help — blocking that bucket blocks the valid deliveries with
// it.
func TestRateLimit_BudgetSurvivesRealisticPushVolume(t *testing.T) {
	webhookInitState(t)
	webhookSaveApp(t, "myapp", "main")

	srv := NewServer(9000, "secret")
	defer srv.limiter.stop()
	deployed := make(chan struct{}, 1)
	srv.SetDeployHandler(func(ctx context.Context, appName string) error {
		deployed <- struct{}{}
		return nil
	})

	body := `{"ref":"refs/heads/main"}`

	// Well past the old 60/min ceiling.
	for i := 0; i < 200; i++ {
		req := httptest.NewRequest(http.MethodPost, "/_qd/webhook/myapp", strings.NewReader(body))
		req.Header.Set("X-Hub-Signature-256", "sha256=deadbeef")
		req.Header.Set("X-GitHub-Event", "push")
		rec := httptest.NewRecorder()
		srv.handleWebhook(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("hit the rate limit after only %d requests; the flood guard is too tight for a busy server", i+1)
		}
	}

	// A valid delivery still works after that volume.
	req := httptest.NewRequest(http.MethodPost, "/_qd/webhook/myapp", strings.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", webhookSignBody([]byte(body), "secret"))
	req.Header.Set("X-GitHub-Event", "push")
	rec := httptest.NewRecorder()
	srv.handleWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("a valid delivery should still be accepted, got %d: %s", rec.Code, rec.Body.String())
	}
	select {
	case <-deployed:
	case <-time.After(2 * time.Second):
		t.Error("the valid delivery should have triggered a deploy")
	}
	srv.deployWg.Wait()
}

// TestHandleWebhook_RefDeletionIgnored pins that deleting a branch does not
// trigger a deploy. GitHub delivers a deletion as a push event with a normal
// refs/heads ref plus `deleted: true`, so the branch filter matched it happily
// and the redeploy then failed at `git fetch` on a branch that no longer exists.
func TestHandleWebhook_RefDeletionIgnored(t *testing.T) {
	webhookInitState(t)
	webhookSaveApp(t, "myapp", "main")

	deployed := make(chan struct{}, 1)
	srv := NewServer(9000, "secret")
	defer srv.limiter.stop()
	srv.SetDeployHandler(func(ctx context.Context, appName string) error {
		deployed <- struct{}{}
		return nil
	})

	for _, body := range []string{
		// GitHub's explicit flag.
		`{"ref":"refs/heads/main","deleted":true}`,
		// GitLab/Gitea signal the same thing with an all-zero `after`.
		`{"ref":"refs/heads/main","after":"0000000000000000000000000000000000000000"}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/_qd/webhook/myapp", strings.NewReader(body))
		req.Header.Set("X-Hub-Signature-256", webhookSignBody([]byte(body), "secret"))
		req.Header.Set("X-GitHub-Event", "push")
		rec := httptest.NewRecorder()
		srv.handleWebhook(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("a deletion should be acknowledged with 200, got %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "deletion ignored") {
			t.Errorf("body should say the deletion was ignored, got %q", rec.Body.String())
		}
	}

	select {
	case <-deployed:
		t.Error("a ref deletion must not trigger a deploy")
	case <-time.After(250 * time.Millisecond):
	}
}

// TestHandleWebhook_UnparseablePayloadRefused pins that the branch filter fails
// CLOSED. Every gate depends on the ref, so treating "could not parse" as "no
// branch restriction" meant an unreadable body deployed the configured branch
// unconditionally.
func TestHandleWebhook_UnparseablePayloadRefused(t *testing.T) {
	webhookInitState(t)
	webhookSaveApp(t, "myapp", "main")

	deployed := make(chan struct{}, 1)
	srv := NewServer(9000, "secret")
	defer srv.limiter.stop()
	srv.SetDeployHandler(func(ctx context.Context, appName string) error {
		deployed <- struct{}{}
		return nil
	})

	body := `this is not a payload`
	req := httptest.NewRequest(http.MethodPost, "/_qd/webhook/myapp", strings.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", webhookSignBody([]byte(body), "secret"))
	req.Header.Set("X-GitHub-Event", "push")
	rec := httptest.NewRecorder()
	srv.handleWebhook(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("an unparseable payload should be refused with 400, got %d: %s", rec.Code, rec.Body.String())
	}
	select {
	case <-deployed:
		t.Error("an unparseable payload must not deploy")
	case <-time.After(250 * time.Millisecond):
	}
}

// TestRunDeploy_RetriesWhileAppLockHeld pins that contention on the
// cross-process app lock is retried rather than logged and dropped.
//
// The webhook answers "200 Deploy triggered" before the deploy runs. When a
// hand-run redeploy (or a `remove` sitting at its confirmation prompt) held the
// app lock, the deploy attempt failed immediately, nothing retried, and the
// provider's delivery log showed success for a commit that was never deployed —
// exactly the bug the in-process queue was added to fix.
func TestRunDeploy_RetriesWhileAppLockHeld(t *testing.T) {
	oldInterval := lockRetryInterval
	lockRetryInterval = 10 * time.Millisecond
	defer func() { lockRetryInterval = oldInterval }()

	srv := NewServer(9000, "secret")
	defer srv.limiter.stop()

	var mu sync.Mutex
	attempts := 0
	srv.SetDeployHandler(func(ctx context.Context, appName string) error {
		mu.Lock()
		attempts++
		n := attempts
		mu.Unlock()
		if n < 3 {
			// What RunRedeployContext returns while another process holds it.
			return fmt.Errorf("wrapped: %w", applock.ErrHeld)
		}
		return nil
	})

	srv.runDeploy("myapp")

	mu.Lock()
	got := attempts
	mu.Unlock()
	if got != 3 {
		t.Errorf("deploy attempts = %d, want 3 (two lock refusals then success)", got)
	}
}

// TestRunDeploy_GivesUpOnLockWhenBudgetExpires pins that the retry loop is
// bounded by deployTimeout rather than spinning forever.
func TestRunDeploy_GivesUpOnLockWhenBudgetExpires(t *testing.T) {
	oldInterval, oldTimeout := lockRetryInterval, deployTimeout
	lockRetryInterval = 5 * time.Millisecond
	deployTimeout = 40 * time.Millisecond
	defer func() { lockRetryInterval, deployTimeout = oldInterval, oldTimeout }()

	srv := NewServer(9000, "secret")
	defer srv.limiter.stop()
	srv.SetDeployHandler(func(ctx context.Context, appName string) error {
		return fmt.Errorf("wrapped: %w", applock.ErrHeld)
	})

	done := make(chan struct{})
	go func() { srv.runDeploy("myapp"); close(done) }()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runDeploy should give up when the deploy budget expires, not spin forever")
	}
}
