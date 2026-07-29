package proxy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The webhook endpoint the deploy wizard advertises —
// https://<base-domain>/_qd/webhook/<app> — had nothing serving it under
// either proxy: Traefik's docker provider can only discover containers and the
// webhook server is a host process, while the Caddyfile only ever received a
// block per app. These tests pin the routes that fix that.

func TestSetupWebhookRoute_Traefik(t *testing.T) {
	dir := setupTestProxyDir(t)

	if err := SetupWebhookRoute("traefik", "apps.example.com", 9000); err != nil {
		t.Fatalf("SetupWebhookRoute failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "dynamic", "webhook.yml"))
	if err != nil {
		t.Fatalf("dynamic route file should exist: %v", err)
	}
	content := string(data)

	for _, want := range []string{
		"Host(`apps.example.com`) && PathPrefix(`/_qd`)",
		"http://host.docker.internal:9000",
		"certResolver: letsencrypt",
		"websecure",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("route file missing %q, got:\n%s", want, content)
		}
	}
}

// The file provider must actually be enabled and the directory mounted, or the
// route written above is never read.
func TestTraefikCompose_EnablesFileProvider(t *testing.T) {
	content := generateTraefikCompose("admin@example.com")

	for _, want := range []string{
		"--providers.file.directory=/dynamic",
		"./dynamic:/dynamic:ro",
		"host.docker.internal:host-gateway",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("traefik compose missing %q", want)
		}
	}
}

func TestSetupWebhookRoute_Caddy(t *testing.T) {
	dir := setupTestProxyDir(t)
	caddyfile := filepath.Join(dir, "Caddyfile")
	if err := os.WriteFile(caddyfile, []byte("{\n    email admin@example.com\n}\n"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := SetupWebhookRoute("caddy", "apps.example.com", 9000); err != nil {
		t.Fatalf("SetupWebhookRoute failed: %v", err)
	}

	data, err := os.ReadFile(caddyfile)
	if err != nil {
		t.Fatalf("read Caddyfile: %v", err)
	}
	content := string(data)

	for _, want := range []string{
		"apps.example.com {",
		"handle /_qd/*",
		"reverse_proxy host.docker.internal:9000",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("Caddyfile missing %q, got:\n%s", want, content)
		}
	}
	if !strings.Contains(content, "email admin@example.com") {
		t.Error("existing global block should be preserved")
	}
}

// Re-running init must not stack duplicate blocks for the same hostname —
// Caddy would parse both and silently use whichever came first.
func TestSetupWebhookRoute_CaddyIsIdempotent(t *testing.T) {
	dir := setupTestProxyDir(t)
	caddyfile := filepath.Join(dir, "Caddyfile")
	if err := os.WriteFile(caddyfile, []byte("{\n    email admin@example.com\n}\n"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	for i := 0; i < 3; i++ {
		if err := SetupWebhookRoute("caddy", "apps.example.com", 9000); err != nil {
			t.Fatalf("SetupWebhookRoute call %d failed: %v", i, err)
		}
	}

	data, _ := os.ReadFile(caddyfile)
	if n := strings.Count(string(data), "apps.example.com {"); n != 1 {
		t.Errorf("found %d blocks for the base domain, want exactly 1:\n%s", n, string(data))
	}
}

func TestSetupWebhookRoute_CaddyReflectsNewPort(t *testing.T) {
	dir := setupTestProxyDir(t)
	caddyfile := filepath.Join(dir, "Caddyfile")
	if err := os.WriteFile(caddyfile, []byte("{\n}\n"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := SetupWebhookRoute("caddy", "apps.example.com", 9000); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := SetupWebhookRoute("caddy", "apps.example.com", 9100); err != nil {
		t.Fatalf("second call: %v", err)
	}

	content, _ := os.ReadFile(caddyfile)
	if strings.Contains(string(content), "host.docker.internal:9000") {
		t.Error("stale port should have been replaced")
	}
	if !strings.Contains(string(content), "host.docker.internal:9100") {
		t.Error("updated port missing")
	}
}

func TestCaddyCompose_HasHostGateway(t *testing.T) {
	if !strings.Contains(generateCaddyCompose(), "host.docker.internal:host-gateway") {
		t.Error("caddy compose must map host.docker.internal or it cannot reach the webhook server")
	}
}

func TestSetupWebhookRoute_Rejects(t *testing.T) {
	setupTestProxyDir(t)

	tests := []struct {
		name       string
		proxyType  string
		baseDomain string
		port       int
	}{
		{"unknown proxy", "nginx", "apps.example.com", 9000},
		{"empty domain", "traefik", "", 9000},
		{"domain with backtick", "traefik", "apps.example.com`)||Host(`evil.com", 9000},
		{"single label domain", "traefik", "localhost", 9000},
		{"port too low", "traefik", "apps.example.com", 0},
		{"port too high", "traefik", "apps.example.com", 70000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := SetupWebhookRoute(tt.proxyType, tt.baseDomain, tt.port); err == nil {
				t.Error("expected an error")
			}
		})
	}
}
