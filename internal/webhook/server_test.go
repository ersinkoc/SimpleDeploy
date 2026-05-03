package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

	deployCalled := false
	srv := NewServer(9000, "secret")
	srv.SetDeployHandler(func(ctx context.Context, appName string) error {
		deployCalled = true
		if appName != "myapp" {
			t.Errorf("Deploy called with %q, want 'myapp'", appName)
		}
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

	time.Sleep(100 * time.Millisecond)
	if !deployCalled {
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

func TestHandleWebhook_EmptyBranch(t *testing.T) {
	webhookInitState(t)
	webhookSaveApp(t, "myapp", "main")

	deployTriggered := false
	srv := NewServer(9000, "secret")
	srv.SetDeployHandler(func(ctx context.Context, appName string) error {
		deployTriggered = true
		return nil
	})

	// Tag push — branch will be empty string
	body := `{"ref":"refs/tags/v1.0.0"}`
	sig := webhookSignBody([]byte(body), "secret")
	req := httptest.NewRequest(http.MethodPost, "/_qd/webhook/myapp", strings.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", sig)
	req.Header.Set("X-GitHub-Event", "push")
	rec := httptest.NewRecorder()
	srv.handleWebhook(rec, req)

	// Empty branch should not mismatch (branch == "" check is skipped)
	if rec.Code != http.StatusOK {
		t.Errorf("Empty branch should succeed, got %d", rec.Code)
	}

	time.Sleep(100 * time.Millisecond)
	if !deployTriggered {
		t.Error("Deploy handler should have been called for tag push with empty branch")
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
