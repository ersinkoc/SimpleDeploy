package proxy

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ersinkoc/SimpleDeploy/internal/docker"
)

func checkPortAvailable(port int) error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}
	ln.Close()
	return nil
}

func setupTestProxyDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	ProxyDir = dir
	return dir
}

func TestAddCaddyApp(t *testing.T) {
	dir := setupTestProxyDir(t)
	caddyfilePath := filepath.Join(dir, "Caddyfile")

	// Create initial Caddyfile
	os.WriteFile(caddyfilePath, []byte("{\n    email test@example.com\n}\n"), 0644)

	headers := map[string]string{
		"X-Custom": "yes",
	}
	err := AddCaddyApp("myapp", "myapp.example.com", 3000, headers)
	if err != nil {
		t.Fatalf("AddCaddyApp failed: %v", err)
	}

	data, err := os.ReadFile(caddyfilePath)
	if err != nil {
		t.Fatalf("Failed to read Caddyfile: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "myapp.example.com") {
		t.Error("Caddyfile should contain domain")
	}
	if !strings.Contains(content, "reverse_proxy qd-myapp:3000") {
		t.Error("Caddyfile should contain reverse_proxy directive")
	}
	if !strings.Contains(content, "X-Custom") {
		t.Error("Caddyfile should contain custom header")
	}
}

func TestAddCaddyApp_NoHeaders(t *testing.T) {
	dir := setupTestProxyDir(t)
	caddyfilePath := filepath.Join(dir, "Caddyfile")
	os.WriteFile(caddyfilePath, []byte("{\n    email test@test.com\n}\n"), 0644)

	err := AddCaddyApp("simple", "simple.example.com", 8080, nil)
	if err != nil {
		t.Fatalf("AddCaddyApp failed: %v", err)
	}

	data, _ := os.ReadFile(caddyfilePath)
	if !strings.Contains(string(data), "reverse_proxy qd-simple:8080") {
		t.Error("Should contain reverse_proxy without headers")
	}
}

func TestAddCaddyApp_MultipleApps(t *testing.T) {
	dir := setupTestProxyDir(t)
	caddyfilePath := filepath.Join(dir, "Caddyfile")
	os.WriteFile(caddyfilePath, []byte("{\n    email test@test.com\n}\n"), 0644)

	AddCaddyApp("app1", "app1.example.com", 3000, nil)
	AddCaddyApp("app2", "app2.example.com", 8080, map[string]string{"X-Auth": "token"})

	data, _ := os.ReadFile(caddyfilePath)
	content := string(data)
	if !strings.Contains(content, "app1.example.com") {
		t.Error("Should contain app1 domain")
	}
	if !strings.Contains(content, "app2.example.com") {
		t.Error("Should contain app2 domain")
	}
}

func TestRemoveCaddyApp(t *testing.T) {
	dir := setupTestProxyDir(t)
	caddyfilePath := filepath.Join(dir, "Caddyfile")

	initial := "{\n    email test@test.com\n}\n\napp1.example.com {\n    reverse_proxy qd-app1:3000\n}\n\napp2.example.com {\n    reverse_proxy qd-app2:8080\n}\n"
	os.WriteFile(caddyfilePath, []byte(initial), 0644)

	err := RemoveCaddyApp("app1.example.com")
	if err != nil {
		t.Fatalf("RemoveCaddyApp failed: %v", err)
	}

	data, _ := os.ReadFile(caddyfilePath)
	content := string(data)
	if strings.Contains(content, "app1.example.com") {
		t.Error("app1 should be removed")
	}
	if !strings.Contains(content, "app2.example.com") {
		t.Error("app2 should still be present")
	}
}

func TestRemoveCaddyApp_AllApps(t *testing.T) {
	dir := setupTestProxyDir(t)
	caddyfilePath := filepath.Join(dir, "Caddyfile")

	initial := "app1.example.com {\n    reverse_proxy qd-app1:3000\n}\n"
	os.WriteFile(caddyfilePath, []byte(initial), 0644)

	RemoveCaddyApp("app1.example.com")

	data, _ := os.ReadFile(caddyfilePath)
	if strings.Contains(string(data), "app1") {
		t.Error("app1 should be fully removed")
	}
}

func TestRemoveCaddyApp_NonExistent(t *testing.T) {
	dir := setupTestProxyDir(t)
	caddyfilePath := filepath.Join(dir, "Caddyfile")
	initial := "app1.example.com {\n    reverse_proxy qd-app1:3000\n}\n"
	os.WriteFile(caddyfilePath, []byte(initial), 0644)

	// Removing non-existent domain should not error, just no-op
	err := RemoveCaddyApp("nonexistent.example.com")
	if err != nil {
		t.Errorf("RemoveCaddyApp for non-existent domain should not error: %v", err)
	}

	data, _ := os.ReadFile(caddyfilePath)
	if !strings.Contains(string(data), "app1.example.com") {
		t.Error("Existing app should still be present")
	}
}

func TestAddCaddyApp_NoCaddyfile(t *testing.T) {
	_ = setupTestProxyDir(t)
	// Don't create Caddyfile
	err := AddCaddyApp("myapp", "myapp.example.com", 3000, nil)
	if err == nil {
		t.Error("Should return error when Caddyfile doesn't exist")
	}
}

func TestRemoveCaddyApp_NoCaddyfile(t *testing.T) {
	_ = setupTestProxyDir(t)
	err := RemoveCaddyApp("myapp.example.com")
	if err == nil {
		t.Error("Should return error when Caddyfile doesn't exist")
	}
}

func TestGenerateCaddyCompose(t *testing.T) {
	content := generateCaddyCompose()
	if !strings.Contains(content, "caddy:2-alpine") {
		t.Error("Should contain caddy image")
	}
	if !strings.Contains(content, "qd-caddy") {
		t.Error("Should contain container name")
	}
	if !strings.Contains(content, "simpledeploy") {
		t.Error("Should contain network name")
	}
	if !strings.Contains(content, "Auto-generated") {
		t.Error("Should contain auto-generated header")
	}
}

func TestGenerateTraefikCompose(t *testing.T) {
	content := generateTraefikCompose("admin@example.com")
	if !strings.Contains(content, "traefik:v3") {
		t.Error("Should contain traefik image")
	}
	if !strings.Contains(content, "qd-traefik") {
		t.Error("Should contain container name")
	}
	if !strings.Contains(content, "admin@example.com") {
		t.Error("Should contain ACME email")
	}
	if !strings.Contains(content, "simpledeploy") {
		t.Error("Should contain network name")
	}
	if !strings.Contains(content, "letsencrypt") {
		t.Error("Should contain letsencrypt config")
	}
	if !strings.Contains(content, "Auto-generated") {
		t.Error("Should contain auto-generated header")
	}
}

func TestSetupCaddy_WithDocker(t *testing.T) {
	if !docker.IsInstalled() || !docker.IsComposeInstalled() {
		t.Skip("Docker/Compose not installed")
	}

	// Check if ports 80/443 are available
	if err := checkPortAvailable(80); err != nil {
		t.Skipf("Port 80 not available: %v", err)
	}
	if err := checkPortAvailable(443); err != nil {
		t.Skipf("Port 443 not available: %v", err)
	}

	dir := setupTestProxyDir(t)
	_ = dir

	// Create the simpledeploy network (required by caddy compose)
	docker.CreateNetwork("simpledeploy")

	err := SetupCaddy("test@test.com")
	if err != nil {
		t.Fatalf("SetupCaddy failed: %v", err)
	}

	// Verify compose file was written
	composePath := filepath.Join(ProxyDir, "docker-compose.yml")
	if _, err := os.Stat(composePath); os.IsNotExist(err) {
		t.Error("docker-compose.yml should exist after SetupCaddy")
	}

	// Verify Caddyfile was written
	caddyfilePath := filepath.Join(ProxyDir, "Caddyfile")
	data, err := os.ReadFile(caddyfilePath)
	if err != nil {
		t.Fatalf("Caddyfile should exist after SetupCaddy: %v", err)
	}
	if !strings.Contains(string(data), "test@test.com") {
		t.Errorf("Caddyfile should contain email, got: %s", string(data))
	}

	// Verify container is running
	if !docker.ContainerExists("qd-caddy") {
		t.Error("qd-caddy container should exist after SetupCaddy")
	}

	// Test StopCaddy
	err = StopCaddy()
	if err != nil {
		t.Errorf("StopCaddy failed: %v", err)
	}
}

func TestSetupTraefik_WithDocker(t *testing.T) {
	if !docker.IsInstalled() || !docker.IsComposeInstalled() {
		t.Skip("Docker/Compose not installed")
	}

	if err := checkPortAvailable(80); err != nil {
		t.Skipf("Port 80 not available: %v", err)
	}
	if err := checkPortAvailable(443); err != nil {
		t.Skipf("Port 443 not available: %v", err)
	}

	setupTestProxyDir(t)

	// Create network
	docker.CreateNetwork("simpledeploy")

	err := SetupTraefik("admin@test.com")
	if err != nil {
		t.Fatalf("SetupTraefik failed: %v", err)
	}

	// Verify compose file
	composePath := filepath.Join(ProxyDir, "docker-compose.yml")
	if _, err := os.Stat(composePath); os.IsNotExist(err) {
		t.Error("docker-compose.yml should exist after SetupTraefik")
	}

	// Verify .env file
	envPath := filepath.Join(ProxyDir, ".env")
	envData, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf(".env should exist: %v", err)
	}
	if !strings.Contains(string(envData), "admin@test.com") {
		t.Errorf(".env should contain email, got: %s", string(envData))
	}

	// Verify container
	if !docker.ContainerExists("qd-traefik") {
		t.Error("qd-traefik container should exist after SetupTraefik")
	}

	// Test StopTraefik
	err = StopTraefik()
	if err != nil {
		t.Errorf("StopTraefik failed: %v", err)
	}
}

func TestReloadCaddy_NoContainer(t *testing.T) {
	if !docker.IsInstalled() {
		t.Skip("Docker not installed")
	}
	err := ReloadCaddy()
	if err == nil {
		t.Error("ReloadCaddy should fail without running container")
	}
}

func TestStopCaddy_NoCompose(t *testing.T) {
	if !docker.IsInstalled() {
		t.Skip("Docker not installed")
	}
	setupTestProxyDir(t)
	err := StopCaddy()
	if err == nil {
		t.Error("StopCaddy should fail without compose file")
	}
}

func TestStopTraefik_NoCompose(t *testing.T) {
	if !docker.IsInstalled() {
		t.Skip("Docker not installed")
	}
	setupTestProxyDir(t)
	err := StopTraefik()
	if err == nil {
		t.Error("StopTraefik should fail without compose file")
	}
}

func TestRestartTraefik_NoCompose(t *testing.T) {
	if !docker.IsInstalled() {
		t.Skip("Docker not installed")
	}
	setupTestProxyDir(t)
	err := RestartTraefik()
	if err == nil {
		t.Error("RestartTraefik should fail without compose file")
	}
}

func TestAddCaddyApp_WithHeaders(t *testing.T) {
	dir := setupTestProxyDir(t)
	caddyfilePath := filepath.Join(dir, "Caddyfile")
	os.WriteFile(caddyfilePath, []byte("{\n    email test@test.com\n}\n"), 0644)

	headers := map[string]string{
		"X-Frame-Options":  "DENY",
		"X-Custom-Header": "value123",
	}
	err := AddCaddyApp("hdrapp", "hdrapp.example.com", 3000, headers)
	if err != nil {
		t.Fatalf("AddCaddyApp with headers failed: %v", err)
	}

	data, _ := os.ReadFile(caddyfilePath)
	content := string(data)
	if !strings.Contains(content, "X-Frame-Options") {
		t.Error("Should contain X-Frame-Options header")
	}
	if !strings.Contains(content, "X-Custom-Header") {
		t.Error("Should contain custom header")
	}
	if !strings.Contains(content, "\"DENY\"") {
		t.Error("Should contain header value DENY")
	}
}

func TestAddCaddyApp_AppendsToExisting(t *testing.T) {
	dir := setupTestProxyDir(t)
	caddyfilePath := filepath.Join(dir, "Caddyfile")

	// Create initial with existing block
	initial := "{\n    email test@test.com\n}\n\napp1.example.com {\n    reverse_proxy qd-app1:3000\n}\n"
	os.WriteFile(caddyfilePath, []byte(initial), 0644)

	err := AddCaddyApp("app2", "app2.example.com", 8080, nil)
	if err != nil {
		t.Fatalf("AddCaddyApp failed: %v", err)
	}

	data, _ := os.ReadFile(caddyfilePath)
	content := string(data)
	if !strings.Contains(content, "app1.example.com") {
		t.Error("Should preserve existing app1 block")
	}
	if !strings.Contains(content, "app2.example.com") {
		t.Error("Should add app2 block")
	}
	if !strings.Contains(content, "reverse_proxy qd-app2:8080") {
		t.Error("Should contain app2 reverse proxy")
	}
}

func TestRemoveCaddyApp_OnlyRemovesTarget(t *testing.T) {
	dir := setupTestProxyDir(t)
	caddyfilePath := filepath.Join(dir, "Caddyfile")

	initial := "{\n    email test@test.com\n}\n\napp1.example.com {\n    reverse_proxy qd-app1:3000\n}\n\napp2.example.com {\n    reverse_proxy qd-app2:8080\n}\n\napp3.example.com {\n    reverse_proxy qd-app3:9000\n}\n"
	os.WriteFile(caddyfilePath, []byte(initial), 0644)

	err := RemoveCaddyApp("app2.example.com")
	if err != nil {
		t.Fatalf("RemoveCaddyApp failed: %v", err)
	}

	data, _ := os.ReadFile(caddyfilePath)
	content := string(data)
	if strings.Contains(content, "app2.example.com") {
		t.Error("app2 should be removed")
	}
	if !strings.Contains(content, "app1.example.com") {
		t.Error("app1 should remain")
	}
	if !strings.Contains(content, "app3.example.com") {
		t.Error("app3 should remain")
	}
}
