package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ersinkoc/SimpleDeploy/internal/state"
)

func TestVerifyGitHubSignature_Valid(t *testing.T) {
	body := []byte(`{"ref":"refs/heads/main"}`)
	secret := "mysecret"

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !VerifyGitHubSignature(body, sig, secret) {
		t.Error("Should verify valid signature")
	}
}

func TestVerifyGitHubSignature_Invalid(t *testing.T) {
	body := []byte(`{"ref":"refs/heads/main"}`)
	if VerifyGitHubSignature(body, "sha256=invalid", "secret") {
		t.Error("Should reject invalid signature")
	}
}

func TestVerifyGitHubSignature_Empty(t *testing.T) {
	if VerifyGitHubSignature([]byte("test"), "", "secret") {
		t.Error("Should reject empty signature")
	}
}

func TestVerifyGitHubSignature_WrongPrefix(t *testing.T) {
	if VerifyGitHubSignature([]byte("test"), "md5=abc", "secret") {
		t.Error("Should reject non-sha256 prefix")
	}
}

func TestVerifyGitHubSignature_WrongSecret(t *testing.T) {
	body := []byte("test")
	mac := hmac.New(sha256.New, []byte("right_secret"))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if VerifyGitHubSignature(body, sig, "wrong_secret") {
		t.Error("Should reject wrong secret")
	}
}

func TestExtractRefFromPayload(t *testing.T) {
	tests := []struct {
		body string
		want string
	}{
		{`{"ref":"refs/heads/main"}`, "refs/heads/main"},
		{`{"ref":"refs/heads/feature/branch"}`, "refs/heads/feature/branch"},
		{`{"no_ref":true}`, ""},
		{`{}`, ""},
		{``, ""},
		{`{"ref":"refs/tags/v1.0"}`, "refs/tags/v1.0"},
		{`invalid json`, ""},
		{`{"ref":null}`, ""},
		{`{"ref":""}`, ""},
		{`{"ref":"refs/heads/fix/bug-123"}`, "refs/heads/fix/bug-123"},
	}

	for _, tt := range tests {
		got := extractRefFromPayload(tt.body)
		if got != tt.want {
			t.Errorf("extractRefFromPayload(%q) = %q, want %q", tt.body, got, tt.want)
		}
	}
}

func TestVerifyGitLabToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Header.Set("X-Gitlab-Token", "my-token")

	if !VerifyGitLabToken(req, "my-token") {
		t.Error("Should verify valid GitLab token")
	}
	if VerifyGitLabToken(req, "wrong-token") {
		t.Error("Should reject wrong GitLab token")
	}
}

func TestVerifyGitLabToken_Missing(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	if VerifyGitLabToken(req, "any") {
		t.Error("Should reject missing token")
	}
}

func TestVerifyGiteaSignature(t *testing.T) {
	body := []byte("test")
	mac := hmac.New(sha256.New, []byte("secret"))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !VerifyGiteaSignature(body, sig, "secret") {
		t.Error("Gitea signature verification should work same as GitHub")
	}
}

func TestNewServer(t *testing.T) {
	srv := NewServer(9000, "secret")
	if srv.Port != 9000 {
		t.Errorf("Port = %d, want 9000", srv.Port)
	}
	if srv.Secret != "secret" {
		t.Error("Secret mismatch")
	}
}

func TestServerSetDeployHandler(t *testing.T) {
	srv := NewServer(9000, "secret")
	called := false
	srv.SetDeployHandler(func(appName string) error {
		called = true
		return nil
	})
	if srv.deploy == nil {
		t.Error("Deploy handler should be set")
	}
	srv.deploy("test")
	if !called {
		t.Error("Deploy handler should have been called")
	}
}

func TestServerHealthEndpoint(t *testing.T) {
	srv := NewServer(0, "secret")

	req := httptest.NewRequest(http.MethodGet, "/_qd/health", nil)
	rec := httptest.NewRecorder()

	// Manually test the handler by calling it
	srv.handleHealth(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Health endpoint returned %d", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Errorf("Health body = %q, want 'ok'", rec.Body.String())
	}
}

func TestServer_StartAndHealthCheck(t *testing.T) {
	// Find an available port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to find available port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	srv := NewServer(port, "test-secret")

	done := make(chan error, 1)
	go func() {
		done <- srv.Start()
	}()

	// Give server time to start
	time.Sleep(200 * time.Millisecond)

	// Make health check request
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/_qd/health", port))
	if err != nil {
		t.Fatalf("Health check failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Health status = %d, want 200", resp.StatusCode)
	}
}

func TestRateLimiter_Allow(t *testing.T) {
	rl := newRateLimiter(2, time.Minute)
	ip := "127.0.0.1"

	if !rl.allow(ip) {
		t.Error("First request should be allowed")
	}
	if !rl.allow(ip) {
		t.Error("Second request should be allowed")
	}
	if rl.allow(ip) {
		t.Error("Third request should be rate limited")
	}
}

func TestRateLimiter_Cleanup(t *testing.T) {
	oldInterval := cleanupInterval
	cleanupInterval = 50 * time.Millisecond
	defer func() { cleanupInterval = oldInterval }()

	rl := newRateLimiter(2, 100*time.Millisecond)
	ip := "127.0.0.1"
	rl.allow(ip)

	time.Sleep(250 * time.Millisecond)

	rl.mu.Lock()
	_, exists := rl.visitors[ip]
	rl.mu.Unlock()

	if exists {
		t.Error("Stale visitor should be cleaned up")
	}
}

func TestServer_ListenAndServeError(t *testing.T) {
	old := httpListenAndServe
	httpListenAndServe = func(srv *http.Server) error {
		return fmt.Errorf("bind error")
	}
	defer func() { httpListenAndServe = old }()

	srv := NewServer(9999, "secret")
	err := srv.Start()
	if err == nil {
		t.Error("Start should fail when ListenAndServe fails")
	}
}

func TestHandleWebhook_RateLimitExceeded(t *testing.T) {
	webhookInitState(t)
	webhookSaveApp(t, "myapp", "main")
	srv := NewServer(9000, "secret")

	body := `{"ref":"refs/heads/main"}`
	reqBase := httptest.NewRequest(http.MethodPost, "/_qd/webhook/myapp", strings.NewReader(body))
	reqBase.Header.Set("X-GitHub-Event", "push")

	// Exhaust rate limit for a specific IP
	rec := httptest.NewRecorder()
	for i := 0; i < 65; i++ {
		req := reqBase.Clone(reqBase.Context())
		// Reset recorder
		rec = httptest.NewRecorder()
		srv.handleWebhook(rec, req)
	}

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("Expected 429 after rate limit, got %d", rec.Code)
	}
}

func TestHandleWebhook_GitLabValidWithApp(t *testing.T) {
	webhookInitState(t)
	webhookSaveApp(t, "gitlabapp", "main")

	srv := NewServer(0, "my-token")
	body := `{"ref":"refs/heads/main"}`
	req := httptest.NewRequest(http.MethodPost, "/_qd/webhook/gitlabapp", strings.NewReader(body))
	req.Header.Set("X-Gitlab-Token", "my-token")
	req.Header.Set("X-Gitlab-Event", "Push Hook")
	rec := httptest.NewRecorder()
	srv.handleWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 for valid GitLab push, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleWebhook_DeployAlreadyInProgress(t *testing.T) {
	webhookInitState(t)
	webhookSaveApp(t, "myapp", "main")

	srv := NewServer(9000, "secret")

	deployStarted := make(chan struct{})
	srv.SetDeployHandler(func(appName string) error {
		close(deployStarted)
		time.Sleep(500 * time.Millisecond)
		return nil
	})

	body := `{"ref":"refs/heads/main"}`
	sig := webhookSignBody([]byte(body), "secret")

	// First webhook
	req1 := httptest.NewRequest(http.MethodPost, "/_qd/webhook/myapp", strings.NewReader(body))
	req1.Header.Set("X-Hub-Signature-256", sig)
	req1.Header.Set("X-GitHub-Event", "push")
	rec1 := httptest.NewRecorder()
	srv.handleWebhook(rec1, req1)

	// Wait for deploy to start
	<-deployStarted

	// Second webhook while first is still running
	req2 := httptest.NewRequest(http.MethodPost, "/_qd/webhook/myapp", strings.NewReader(body))
	req2.Header.Set("X-Hub-Signature-256", sig)
	req2.Header.Set("X-GitHub-Event", "push")
	rec2 := httptest.NewRecorder()
	srv.handleWebhook(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Errorf("Expected 200 for second webhook, got %d", rec2.Code)
	}

	// Wait for first deploy to finish
	srv.deployWg.Wait()
}

func TestHandleWebhook_DeployTimeout(t *testing.T) {
	oldTimeout := deployTimeout
	deployTimeout = 50 * time.Millisecond
	defer func() { deployTimeout = oldTimeout }()

	webhookInitState(t)
	webhookSaveApp(t, "myapp", "main")

	srv := NewServer(9000, "secret")
	srv.SetDeployHandler(func(appName string) error {
		time.Sleep(200 * time.Millisecond)
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
		t.Errorf("Expected 200, got %d", rec.Code)
	}

	// Wait for deploy to finish (and timeout callback may have fired)
	srv.deployWg.Wait()
}

type errorReader struct{}

func (e *errorReader) Read(p []byte) (int, error) {
	return 0, fmt.Errorf("read error")
}

func TestHandleWebhook_BodyReadError(t *testing.T) {
	webhookInitState(t)
	webhookSaveApp(t, "myapp", "main")

	srv := NewServer(9000, "secret")
	req := httptest.NewRequest(http.MethodPost, "/_qd/webhook/myapp", &errorReader{})
	req.Header.Set("X-GitHub-Event", "push")
	rec := httptest.NewRecorder()
	srv.handleWebhook(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected 500 for body read error, got %d", rec.Code)
	}
}

func TestServer_ShutdownPath(t *testing.T) {
	old := httpListenAndServe
	httpListenAndServe = func(srv *http.Server) error {
		return http.ErrServerClosed
	}
	defer func() { httpListenAndServe = old }()

	srv := NewServer(9999, "secret")
	err := srv.Start()
	if err != nil {
		t.Errorf("Start should return nil after graceful shutdown, got %v", err)
	}
}

func TestIsValidAppName(t *testing.T) {
	valid := []string{"myapp", "my-app-123", "ab", "app123app"}
	for _, name := range valid {
		if !isValidAppName(name) {
			t.Errorf("isValidAppName(%q) should be true", name)
		}
	}

	invalid := []string{"", "a", "MyApp", "my app", "../etc", "my_app", "-app", "app-"}
	for _, name := range invalid {
		if isValidAppName(name) {
			t.Errorf("isValidAppName(%q) should be false", name)
		}
	}
}

func TestWebhook_InvalidAppName(t *testing.T) {
	srv := NewServer(0, "secret")
	req := httptest.NewRequest(http.MethodPost, "/_qd/webhook/../etc", strings.NewReader(`{}`))
	req.Header.Set("X-GitHub-Event", "push")
	rec := httptest.NewRecorder()
	srv.handleWebhook(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid app name, got %d", rec.Code)
	}
}

func TestWebhook_EmptyAppName(t *testing.T) {
	srv := NewServer(0, "secret")
	req := httptest.NewRequest(http.MethodPost, "/_qd/webhook/", strings.NewReader(`{}`))
	req.Header.Set("X-GitHub-Event", "push")
	rec := httptest.NewRecorder()
	srv.handleWebhook(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for empty app name, got %d", rec.Code)
	}
}

func TestWebhook_WrongMethod(t *testing.T) {
	srv := NewServer(0, "secret")
	req := httptest.NewRequest(http.MethodGet, "/_qd/webhook/myapp", nil)
	rec := httptest.NewRecorder()
	srv.handleWebhook(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405 for GET, got %d", rec.Code)
	}
}

func TestWebhook_GitLabValid(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)

	srv := NewServer(0, "my-token")
	body := `{"ref":"refs/heads/main"}`
	req := httptest.NewRequest(http.MethodPost, "/_qd/webhook/gitlabapp", strings.NewReader(body))
	req.Header.Set("X-Gitlab-Token", "my-token")
	req.Header.Set("X-Gitlab-Event", "Push Hook")
	rec := httptest.NewRecorder()
	srv.handleWebhook(rec, req)

	if rec.Code != http.StatusNotFound {
		// 404 because app doesn't exist, but auth passed
		t.Errorf("Expected 404 (app not found, auth passed), got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestWebhook_GitLabInvalid(t *testing.T) {
	srv := NewServer(0, "correct-token")
	body := `{"ref":"refs/heads/main"}`
	req := httptest.NewRequest(http.MethodPost, "/_qd/webhook/gitlabapp", strings.NewReader(body))
	req.Header.Set("X-Gitlab-Token", "wrong-token")
	req.Header.Set("X-Gitlab-Event", "Push Hook")
	rec := httptest.NewRecorder()
	srv.handleWebhook(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 for invalid GitLab token, got %d", rec.Code)
	}
}

func TestWebhook_GiteaValid(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)

	srv := NewServer(0, "gitea-secret")
	body := []byte(`{"ref":"refs/heads/main"}`)
	mac := hmac.New(sha256.New, []byte("gitea-secret"))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest(http.MethodPost, "/_qd/webhook/giteaapp", strings.NewReader(string(body)))
	req.Header.Set("X-Gitea-Signature", sig)
	req.Header.Set("X-Gitea-Event", "push")
	rec := httptest.NewRecorder()
	srv.handleWebhook(rec, req)

	if rec.Code != http.StatusNotFound {
		// 404 because app doesn't exist, but auth passed
		t.Errorf("Expected 404 (app not found, auth passed), got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestWebhook_GiteaInvalid(t *testing.T) {
	srv := NewServer(0, "gitea-secret")
	body := `{"ref":"refs/heads/main"}`
	req := httptest.NewRequest(http.MethodPost, "/_qd/webhook/giteaapp", strings.NewReader(body))
	req.Header.Set("X-Gitea-Signature", "sha256=invalidsignature")
	req.Header.Set("X-Gitea-Event", "push")
	rec := httptest.NewRecorder()
	srv.handleWebhook(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 for invalid Gitea signature, got %d", rec.Code)
	}
}

func TestWebhook_NoProviderHeaders(t *testing.T) {
	srv := NewServer(0, "secret")
	body := `{"ref":"refs/heads/main"}`
	req := httptest.NewRequest(http.MethodPost, "/_qd/webhook/myapp", strings.NewReader(body))
	// No provider-specific headers at all
	rec := httptest.NewRecorder()
	srv.handleWebhook(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 for no provider headers, got %d", rec.Code)
	}
}
