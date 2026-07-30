package proxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/ersinkoc/SimpleDeploy/internal/docker"
)

type mockCommandRunner struct {
	runErr error
}

func (m *mockCommandRunner) SetDir(string)       {}
func (m *mockCommandRunner) SetStdout(io.Writer) {}
func (m *mockCommandRunner) SetStderr(io.Writer) {}
func (m *mockCommandRunner) Run() error          { return m.runErr }

func TestMain(m *testing.M) {
	code := m.Run()
	// Clean up any containers left behind by tests
	if docker.IsInstalled() {
		_ = docker.Run([]string{"rm", "-f", "qd-caddy", "qd-traefik"})
	}
	os.Exit(code)
}

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

// AddCaddyApp walks a map to emit its `header` directives. Go randomises map
// iteration order per run, so an unsorted walk produced a different Caddyfile
// on every deploy for an unchanged app — the same defect compose/generator.go
// fixes with sortedKeys. Enough headers here that a random order would differ
// from the sorted one with overwhelming probability.
func TestAddCaddyApp_HeaderOrderIsDeterministic(t *testing.T) {
	headers := map[string]string{
		"X-Frame-Options":        "SAMEORIGIN",
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
		"X-XSS-Protection":       "1; mode=block",
		"Permissions-Policy":     "geolocation=()",
		"X-Alpha":                "a",
		"X-Beta":                 "b",
	}

	render := func() string {
		t.Helper()
		dir := setupTestProxyDir(t)
		caddyfilePath := filepath.Join(dir, "Caddyfile")
		if err := os.WriteFile(caddyfilePath, []byte("{\n    email test@example.com\n}\n"), 0644); err != nil {
			t.Fatalf("seed Caddyfile: %v", err)
		}
		if err := AddCaddyApp("myapp", "myapp.example.com", 3000, headers); err != nil {
			t.Fatalf("AddCaddyApp failed: %v", err)
		}
		data, err := os.ReadFile(caddyfilePath)
		if err != nil {
			t.Fatalf("read Caddyfile: %v", err)
		}
		return string(data)
	}

	want := render()
	for i := 0; i < 20; i++ {
		if got := render(); got != want {
			t.Fatalf("Caddyfile is not deterministic across runs.\nrun 0:\n%s\nrun %d:\n%s", want, i+1, got)
		}
	}

	// And the order is actually sorted, not merely stable-by-accident.
	var seen []string
	for _, line := range strings.Split(want, "\n") {
		if fields := strings.Fields(line); len(fields) >= 2 && fields[0] == "header" {
			seen = append(seen, fields[1])
		}
	}
	if len(seen) != len(headers) {
		t.Fatalf("emitted %d header directives, want %d", len(seen), len(headers))
	}
	if !sort.StringsAreSorted(seen) {
		t.Errorf("header directives are not in sorted order: %v", seen)
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
	requireCompose(t)

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
	docker.CreateNetwork(context.Background(), "simpledeploy")

	err := SetupCaddy(context.Background(), "test@test.com")
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
	if !docker.ContainerExists(context.Background(), "qd-caddy") {
		t.Error("qd-caddy container should exist after SetupCaddy")
	}

	// Test StopCaddy
	err = StopCaddy()
	if err != nil {
		t.Errorf("StopCaddy failed: %v", err)
	}
}

func TestSetupTraefik_WithDocker(t *testing.T) {
	requireCompose(t)

	if err := checkPortAvailable(80); err != nil {
		t.Skipf("Port 80 not available: %v", err)
	}
	if err := checkPortAvailable(443); err != nil {
		t.Skipf("Port 443 not available: %v", err)
	}

	setupTestProxyDir(t)

	// Create network
	docker.CreateNetwork(context.Background(), "simpledeploy")

	err := SetupTraefik(context.Background(), "admin@test.com")
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
	if !docker.ContainerExists(context.Background(), "qd-traefik") {
		t.Error("qd-traefik container should exist after SetupTraefik")
	}

	// Test StopTraefik
	err = StopTraefik()
	if err != nil {
		t.Errorf("StopTraefik failed: %v", err)
	}
}

func TestReloadCaddy_NoContainer(t *testing.T) {
	requireDocker(t)
	// Ensure no stale qd-caddy container from previous tests
	_ = docker.Run([]string{"rm", "-f", "qd-caddy"})
	err := ReloadCaddy()
	if err == nil {
		t.Error("ReloadCaddy should fail without running container")
	}
}

func TestStopCaddy_NoCompose(t *testing.T) {
	requireDocker(t)
	setupTestProxyDir(t)
	err := StopCaddy()
	if err == nil {
		t.Error("StopCaddy should fail without compose file")
	}
}

func TestStopTraefik_NoCompose(t *testing.T) {
	requireDocker(t)
	setupTestProxyDir(t)
	err := StopTraefik()
	if err == nil {
		t.Error("StopTraefik should fail without compose file")
	}
}

func TestRestartTraefik_NoCompose(t *testing.T) {
	requireDocker(t)
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
		"X-Frame-Options": "DENY",
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

// TestSetupCaddy_WritesFiles tests that SetupCaddy creates the expected files
// (compose + Caddyfile) without needing port 80/443 (it'll fail at docker compose up).
func TestSetupCaddy_WritesFiles(t *testing.T) {
	requireDocker(t)

	dir := setupTestProxyDir(t)

	// Clean up any stale containers from previous test runs
	_ = docker.Run([]string{"rm", "-f", "qd-caddy"})

	docker.CreateNetwork(context.Background(), "simpledeploy")

	err := SetupCaddy(context.Background(), "setup-test@example.com")
	// May fail at compose up if ports are in use, but files should still be written
	_ = err

	// Check compose file was created
	composePath := filepath.Join(dir, "docker-compose.yml")
	data, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("docker-compose.yml should be written: %v", err)
	}
	if !strings.Contains(string(data), "caddy:2-alpine") {
		t.Error("Compose should contain caddy image")
	}

	// Check Caddyfile was created
	caddyPath := filepath.Join(dir, "Caddyfile")
	caddyData, err := os.ReadFile(caddyPath)
	if err != nil {
		t.Fatalf("Caddyfile should be written: %v", err)
	}
	if !strings.Contains(string(caddyData), "setup-test@example.com") {
		t.Error("Caddyfile should contain ACME email")
	}

	// Cleanup if it actually started
	if docker.ContainerExists(context.Background(), "qd-caddy") {
		StopCaddy()
	}
}

// TestSetupTraefik_WritesFiles tests that SetupTraefik creates the expected files.
func TestSetupTraefik_WritesFiles(t *testing.T) {
	requireDocker(t)

	dir := setupTestProxyDir(t)

	// Clean up any stale containers from previous test runs
	_ = docker.Run([]string{"rm", "-f", "qd-traefik", "qd-caddy"})

	docker.CreateNetwork(context.Background(), "simpledeploy")

	err := SetupTraefik(context.Background(), "traefik-setup@example.com")
	_ = err

	// Check compose file
	composePath := filepath.Join(dir, "docker-compose.yml")
	data, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("docker-compose.yml should be written: %v", err)
	}
	if !strings.Contains(string(data), "traefik:v3") {
		t.Error("Compose should contain traefik image")
	}
	if !strings.Contains(string(data), "traefik-setup@example.com") {
		t.Error("Compose should contain ACME email")
	}

	// Check .env file
	envPath := filepath.Join(dir, ".env")
	envData, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf(".env should be written: %v", err)
	}
	if !strings.Contains(string(envData), "ACME_EMAIL=traefik-setup@example.com") {
		t.Error(".env should contain ACME email")
	}

	// Cleanup if it actually started
	if docker.ContainerExists(context.Background(), "qd-traefik") {
		StopTraefik()
	}
}

func TestSetupCaddy_ReadOnlyDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions not supported on Windows")
	}
	requireDocker(t)

	tmpDir := t.TempDir()
	readOnlyDir := filepath.Join(tmpDir, "readonly")
	os.MkdirAll(readOnlyDir, 0555)
	os.Chmod(readOnlyDir, 0555)
	defer os.Chmod(readOnlyDir, 0755)

	ProxyDir = filepath.Join(readOnlyDir, "nested", "proxy")
	defer func() { ProxyDir = "" }()

	err := SetupCaddy(context.Background(), "test@test.com")
	if err == nil {
		t.Error("Should fail with read-only proxy dir")
	}
}

func TestSetupTraefik_ReadOnlyDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions not supported on Windows")
	}
	requireDocker(t)

	tmpDir := t.TempDir()
	readOnlyDir := filepath.Join(tmpDir, "readonly")
	os.MkdirAll(readOnlyDir, 0555)
	os.Chmod(readOnlyDir, 0555)
	defer os.Chmod(readOnlyDir, 0755)

	ProxyDir = filepath.Join(readOnlyDir, "nested", "proxy")
	defer func() { ProxyDir = "" }()

	err := SetupTraefik(context.Background(), "test@test.com")
	if err == nil {
		t.Error("Should fail with read-only proxy dir")
	}
}

func TestGetProxyDir_Fallback(t *testing.T) {
	old := ProxyDir
	ProxyDir = ""
	defer func() { ProxyDir = old }()

	dir := getProxyDir()
	if dir == "" {
		t.Error("getProxyDir fallback should not be empty")
	}
}

func TestSetupCaddy_MkdirAllError(t *testing.T) {
	old := osMkdirAll
	osMkdirAll = func(path string, perm os.FileMode) error {
		return os.ErrPermission
	}
	defer func() { osMkdirAll = old }()

	err := SetupCaddy(context.Background(), "test@test.com")
	if err == nil {
		t.Error("SetupCaddy should fail when MkdirAll fails")
	}
}

func TestSetupCaddy_WriteComposeError(t *testing.T) {
	setupTestProxyDir(t)

	old := osWriteFile
	callCount := 0
	osWriteFile = func(name string, data []byte, perm os.FileMode) error {
		callCount++
		if callCount == 1 {
			return os.ErrPermission
		}
		return os.WriteFile(name, data, perm)
	}
	defer func() { osWriteFile = old }()

	err := SetupCaddy(context.Background(), "test@test.com")
	if err == nil {
		t.Error("SetupCaddy should fail when WriteFile for compose fails")
	}
}

func TestSetupCaddy_WriteCaddyfileError(t *testing.T) {
	setupTestProxyDir(t)

	old := osWriteFile
	callCount := 0
	osWriteFile = func(name string, data []byte, perm os.FileMode) error {
		callCount++
		if callCount == 2 {
			return os.ErrPermission
		}
		return os.WriteFile(name, data, perm)
	}
	defer func() { osWriteFile = old }()

	err := SetupCaddy(context.Background(), "test@test.com")
	if err == nil {
		t.Error("SetupCaddy should fail when WriteFile for Caddyfile fails")
	}
}

func TestSetupCaddy_CreateNetworkError(t *testing.T) {
	setupTestProxyDir(t)

	old := dockerCreateNetwork
	dockerCreateNetwork = func(ctx context.Context, name string) error {
		return fmt.Errorf("network error")
	}
	defer func() { dockerCreateNetwork = old }()

	err := SetupCaddy(context.Background(), "test@test.com")
	if err == nil {
		t.Error("SetupCaddy should fail when CreateNetwork fails")
	}
}

func TestSetupTraefik_MkdirAllError(t *testing.T) {
	old := osMkdirAll
	osMkdirAll = func(path string, perm os.FileMode) error {
		return os.ErrPermission
	}
	defer func() { osMkdirAll = old }()

	err := SetupTraefik(context.Background(), "test@test.com")
	if err == nil {
		t.Error("SetupTraefik should fail when MkdirAll fails")
	}
}

func TestSetupTraefik_WriteComposeError(t *testing.T) {
	setupTestProxyDir(t)

	old := osWriteFile
	callCount := 0
	osWriteFile = func(name string, data []byte, perm os.FileMode) error {
		callCount++
		if callCount == 1 {
			return os.ErrPermission
		}
		return os.WriteFile(name, data, perm)
	}
	defer func() { osWriteFile = old }()

	err := SetupTraefik(context.Background(), "test@test.com")
	if err == nil {
		t.Error("SetupTraefik should fail when WriteFile for compose fails")
	}
}

func TestSetupTraefik_WriteEnvError(t *testing.T) {
	setupTestProxyDir(t)

	old := osWriteFile
	callCount := 0
	osWriteFile = func(name string, data []byte, perm os.FileMode) error {
		callCount++
		if callCount == 2 {
			return os.ErrPermission
		}
		return os.WriteFile(name, data, perm)
	}
	defer func() { osWriteFile = old }()

	err := SetupTraefik(context.Background(), "test@test.com")
	if err == nil {
		t.Error("SetupTraefik should fail when WriteFile for .env fails")
	}
}

// TestSetupTraefik_EnvFileMode0600 confirms the proxy's ACME .env is written
// with owner-only perms. The file currently only contains ACME_EMAIL, but env
// files are an acknowledged secrets-carrying convention so we keep the mode
// tight to avoid future leakage if more values are added. Captured via the
// osWriteFile indirection so it works on Windows too.
func TestSetupTraefik_EnvFileMode0600(t *testing.T) {
	setupTestProxyDir(t)

	old := osWriteFile
	captured := map[string]os.FileMode{}
	osWriteFile = func(name string, data []byte, perm os.FileMode) error {
		captured[filepath.Base(name)] = perm
		return os.WriteFile(name, data, perm)
	}
	defer func() { osWriteFile = old }()

	// SetupTraefik will fail at the docker-network step in unit tests without
	// Docker installed; we only care about the perm passed to the .env write,
	// which happens before that.
	_ = SetupTraefik(context.Background(), "test@test.com")

	if perm, ok := captured[".env"]; !ok {
		t.Fatal("SetupTraefik did not write .env")
	} else if perm != 0600 {
		t.Errorf("proxy .env mode = %#o, want 0600", perm)
	}
}

func TestSetupTraefik_CreateNetworkError(t *testing.T) {
	setupTestProxyDir(t)

	old := dockerCreateNetwork
	dockerCreateNetwork = func(ctx context.Context, name string) error {
		return fmt.Errorf("network error")
	}
	defer func() { dockerCreateNetwork = old }()

	err := SetupTraefik(context.Background(), "test@test.com")
	if err == nil {
		t.Error("SetupTraefik should fail when CreateNetwork fails")
	}
}

func TestSetupCaddy_DockerComposeUpError(t *testing.T) {
	setupTestProxyDir(t)

	old := execCommand
	execCommand = func(ctx context.Context, name string, arg ...string) commandRunner {
		return &mockCommandRunner{runErr: fmt.Errorf("compose up failed")}
	}
	defer func() { execCommand = old }()

	err := SetupCaddy(context.Background(), "test@test.com")
	if err == nil {
		t.Error("SetupCaddy should fail when docker compose up fails")
	}
}

func TestSetupTraefik_DockerComposeUpError(t *testing.T) {
	setupTestProxyDir(t)

	old := execCommand
	execCommand = func(ctx context.Context, name string, arg ...string) commandRunner {
		return &mockCommandRunner{runErr: fmt.Errorf("compose up failed")}
	}
	defer func() { execCommand = old }()

	err := SetupTraefik(context.Background(), "test@test.com")
	if err == nil {
		t.Error("SetupTraefik should fail when docker compose up fails")
	}
}

func TestAddCaddyApp_InvalidDomain(t *testing.T) {
	dir := setupTestProxyDir(t)
	caddyfilePath := filepath.Join(dir, "Caddyfile")
	os.WriteFile(caddyfilePath, []byte("{\n    email test@test.com\n}\n"), 0644)

	tests := []struct {
		name   string
		domain string
	}{
		{"braces", "evil{.example.com"},
		{"semicolon", "evil;example.com"},
		{"newline", "evil\n.example.com"},
		{"empty", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := AddCaddyApp("test", tc.domain, 3000, nil)
			if err == nil {
				t.Errorf("AddCaddyApp should reject domain %q", tc.domain)
			}
		})
	}
}

// TestEscapeCaddyValue_Security tests that escapeCaddyValue properly escapes
// special characters to prevent Caddyfile injection attacks.
func TestEscapeCaddyValue_Security(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    `normal value`,
			expected: `normal value`,
		},
		{
			input:    `value with "quotes"`,
			expected: `value with \"quotes\"`,
		},
		{
			input:    `value with \ backslash`,
			expected: `value with \\ backslash`,
		},
		{
			input:    "value with\nnewline",
			expected: `value with\nnewline`,
		},
		{
			input:    `value with "} malicious {`,
			expected: `value with \"} malicious {`,
		},
	}

	for _, tc := range tests {
		result := escapeCaddyValue(tc.input)
		if result != tc.expected {
			t.Errorf("escapeCaddyValue(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

// TestAddCaddyApp_HeaderInjection tests that AddCaddyApp rejects header
// values that could be used for Caddyfile injection. Escaping alone is not
// enough: `{`/`}` survive escapeCaddyValue, `{...}` is Caddy placeholder
// syntax even inside quotes, and an unbalanced brace corrupts
// filterCaddyDomain's block-end counting — so such values are refused
// outright (mirroring state.ValidateHeaderValue on the Traefik path), and
// the Caddyfile must be left untouched.
func TestAddCaddyApp_HeaderInjection(t *testing.T) {
	setupTestProxyDir(t)

	// Create initial Caddyfile
	caddyfilePath := filepath.Join(getProxyDir(), "Caddyfile")
	initial := "# Initial Caddyfile\n"
	os.WriteFile(caddyfilePath, []byte(initial), 0644)

	maliciousValues := []string{
		`value"} malicious_directive "another`, // quote + unbalanced brace
		"{http.request.header.Cookie}",         // Caddy placeholder
		"multi\nline",                          // newline
		`back\slash`,                           // backslash
	}

	for _, v := range maliciousValues {
		err := AddCaddyApp("testapp", "test.example.com", 3000,
			map[string]string{"X-Custom": v})
		if err == nil {
			t.Errorf("AddCaddyApp should reject header value %q", v)
		}
	}

	// The file must not have been touched by any of the rejected calls.
	data, err := os.ReadFile(caddyfilePath)
	if err != nil {
		t.Fatalf("Failed to read Caddyfile: %v", err)
	}
	if string(data) != initial {
		t.Errorf("Caddyfile was modified despite rejected headers:\n%s", data)
	}
}

// TestAddCaddyApp_InvalidAppName ensures AddCaddyApp's defense-in-depth
// validation rejects app names that would otherwise be interpolated raw
// into the Caddyfile's `reverse_proxy qd-%s:%d` line.
func TestAddCaddyApp_InvalidAppName(t *testing.T) {
	dir := setupTestProxyDir(t)
	caddyfilePath := filepath.Join(dir, "Caddyfile")
	os.WriteFile(caddyfilePath, []byte("{\n    email test@test.com\n}\n"), 0644)

	tests := []struct {
		name    string
		appName string
	}{
		{"empty", ""},
		{"newline injects directive", "app\nmalicious"},
		{"caddy block break", "app}"},
		{"slash", "app/foo"},
		{"colon", "app:bar"},
		{"backtick", "app`whoami`"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := AddCaddyApp(tc.appName, "valid.example.com", 3000, nil)
			if err == nil {
				t.Errorf("AddCaddyApp should reject app name %q", tc.appName)
			}
		})
	}
}

// TestAddCaddyApp_InvalidPort verifies that out-of-range ports are rejected
// before the reverse_proxy directive is emitted. A negative or > 65535 port
// would produce a syntactically invalid Caddyfile.
func TestAddCaddyApp_InvalidPort(t *testing.T) {
	dir := setupTestProxyDir(t)
	caddyfilePath := filepath.Join(dir, "Caddyfile")
	os.WriteFile(caddyfilePath, []byte("{\n    email test@test.com\n}\n"), 0644)

	tests := []struct {
		name string
		port int
	}{
		{"zero", 0},
		{"negative", -1},
		{"too large", 65536},
		{"way too large", 1 << 30},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := AddCaddyApp("valid", "valid.example.com", tc.port, nil)
			if err == nil {
				t.Errorf("AddCaddyApp should reject port %d", tc.port)
			}
		})
	}
}

// TestAddCaddyApp_InvalidHeaderName covers the gap previously left by
// escapeCaddyValue (which only protected the value half). A header name
// containing `\n}` would let an attacker break out of the app block and
// inject Caddyfile directives.
func TestAddCaddyApp_InvalidHeaderName(t *testing.T) {
	dir := setupTestProxyDir(t)
	caddyfilePath := filepath.Join(dir, "Caddyfile")
	os.WriteFile(caddyfilePath, []byte("{\n    email test@test.com\n}\n"), 0644)

	tests := []struct {
		name      string
		headerKey string
	}{
		{"newline injects directive", "X-Foo\nrespond \"pwned\""},
		{"caddy block break", "X-Foo}"},
		{"space", "X Frame Options"},
		{"empty", ""},
		{"semicolon", "X-Foo;"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := AddCaddyApp("valid", "valid.example.com", 3000,
				map[string]string{tc.headerKey: "ok"})
			if err == nil {
				t.Errorf("AddCaddyApp should reject header name %q", tc.headerKey)
			}
		})
	}
}

// TestAddCaddyApp_DedupSameDomain verifies that calling AddCaddyApp twice for
// the same domain does NOT produce two routing blocks. With the previous
// implementation a redeploy that changed port or headers would leave the old
// block in place; Caddy would then parse two blocks for the same hostname and
// pick the one that came first, masking the updated config.
func TestAddCaddyApp_DedupSameDomain(t *testing.T) {
	dir := setupTestProxyDir(t)
	caddyfilePath := filepath.Join(dir, "Caddyfile")
	os.WriteFile(caddyfilePath, []byte("{\n    email test@test.com\n}\n"), 0644)

	if err := AddCaddyApp("app1", "app1.example.com", 3000, nil); err != nil {
		t.Fatalf("first AddCaddyApp failed: %v", err)
	}
	if err := AddCaddyApp("app1", "app1.example.com", 8080, nil); err != nil {
		t.Fatalf("second AddCaddyApp failed: %v", err)
	}

	data, _ := os.ReadFile(caddyfilePath)
	content := string(data)

	if strings.Count(content, "app1.example.com {") != 1 {
		t.Errorf("expected exactly one app1.example.com block, got %d. Caddyfile:\n%s",
			strings.Count(content, "app1.example.com {"), content)
	}
	if strings.Contains(content, "qd-app1:3000") {
		t.Errorf("old port 3000 should have been removed. Caddyfile:\n%s", content)
	}
	if !strings.Contains(content, "qd-app1:8080") {
		t.Errorf("new port 8080 should be present. Caddyfile:\n%s", content)
	}
}

// TestAddCaddyApp_DedupPreservesOtherDomains ensures that re-adding domain A
// does not disturb the block for domain B.
func TestAddCaddyApp_DedupPreservesOtherDomains(t *testing.T) {
	dir := setupTestProxyDir(t)
	caddyfilePath := filepath.Join(dir, "Caddyfile")
	os.WriteFile(caddyfilePath, []byte("{\n    email test@test.com\n}\n"), 0644)

	if err := AddCaddyApp("app1", "app1.example.com", 3000, nil); err != nil {
		t.Fatalf("AddCaddyApp app1 failed: %v", err)
	}
	if err := AddCaddyApp("app2", "app2.example.com", 4000, nil); err != nil {
		t.Fatalf("AddCaddyApp app2 failed: %v", err)
	}
	// Re-add app1 with a new port
	if err := AddCaddyApp("app1", "app1.example.com", 5000, nil); err != nil {
		t.Fatalf("AddCaddyApp app1 (redeploy) failed: %v", err)
	}

	data, _ := os.ReadFile(caddyfilePath)
	content := string(data)

	if strings.Count(content, "app1.example.com {") != 1 {
		t.Errorf("app1 should appear exactly once after re-add. Caddyfile:\n%s", content)
	}
	if strings.Count(content, "app2.example.com {") != 1 {
		t.Errorf("app2 should still appear exactly once. Caddyfile:\n%s", content)
	}
	if !strings.Contains(content, "qd-app1:5000") {
		t.Errorf("app1 redeploy port 5000 should be present. Caddyfile:\n%s", content)
	}
	if !strings.Contains(content, "qd-app2:4000") {
		t.Errorf("app2 port 4000 should be preserved. Caddyfile:\n%s", content)
	}
}

// TestWriteCaddyfile_BasicWrite covers the happy path: data lands in the
// destination file and no .tmp file is left behind.
func TestWriteCaddyfile_BasicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	if err := writeCaddyfile(path, []byte("hello"), 0644); err != nil {
		t.Fatalf("writeCaddyfile failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile after writeCaddyfile failed: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("got %q, want \"hello\"", string(data))
	}

	// No sibling temp file: the write is in place, so none is ever created.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("no .tmp sibling should exist, got err=%v", err)
	}
}

// TestWriteCaddyfile_PreservesInode is the regression test for Caddy mode not
// routing anything at all.
//
// The generated Caddy compose bind-mounts the Caddyfile as a single FILE
// (./Caddyfile:/etc/caddy/Caddyfile). Docker resolves that to an inode once, at
// container creation. The previous write-to-temp-then-rename implementation
// swapped in a new inode on every write, so the running Caddy stayed bound to
// the original — orphaned — inode and never saw a single update. `init` wrote
// the global block, started Caddy, then the first atomic write (the /_qd
// webhook route) broke the mount; every AddCaddyApp afterwards was invisible.
// Caddy ended up with no site blocks, did not listen on :80/:443 at all, and
// `caddy reload` cheerfully reported "config is unchanged".
//
// Skipped on Windows: os.SameFile is the portable identity check, but Windows
// does not expose stable inode semantics for this scenario.
func TestWriteCaddyfile_PreservesInode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("inode identity is not meaningful on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "Caddyfile")

	if err := writeCaddyfile(path, []byte("{\n    email a@b.com\n}\n"), 0644); err != nil {
		t.Fatalf("initial writeCaddyfile failed: %v", err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	// Two more writes — a bind-mounted container must see both.
	for i, content := range []string{"first update\n", "second update\n"} {
		if err := writeCaddyfile(path, []byte(content), 0644); err != nil {
			t.Fatalf("writeCaddyfile #%d failed: %v", i, err)
		}
		after, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat #%d failed: %v", i, err)
		}
		if !os.SameFile(before, after) {
			t.Fatalf("write #%d replaced the file's inode — a single-file bind mount would go stale and Caddy would never see this config", i)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile #%d failed: %v", i, err)
		}
		if string(got) != content {
			t.Errorf("write #%d: got %q, want %q", i, got, content)
		}
	}
}

// TestAddCaddyApp_PreservesInode exercises the same property through the real
// call path an operator hits: init writes the stub, then every deploy appends a
// site block. All of them must land on the same inode.
func TestAddCaddyApp_PreservesInode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("inode identity is not meaningful on Windows")
	}
	dir := t.TempDir()
	oldProxyDir := ProxyDir
	ProxyDir = dir
	defer func() { ProxyDir = oldProxyDir }()

	caddyfile := filepath.Join(dir, "Caddyfile")
	if err := os.WriteFile(caddyfile, []byte("{\n    email a@b.com\n}\n"), 0644); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	// This is the inode the container binds at `docker compose up -d`.
	mounted, err := os.Stat(caddyfile)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	if err := setupCaddyWebhookRoute("apps.example.com", 9000); err != nil {
		t.Fatalf("setupCaddyWebhookRoute failed: %v", err)
	}
	if err := AddCaddyApp("one", "one.apps.example.com", 3000, map[string]string{"X-Frame-Options": "SAMEORIGIN"}); err != nil {
		t.Fatalf("AddCaddyApp failed: %v", err)
	}
	if err := AddCaddyApp("two", "two.apps.example.com", 8080, nil); err != nil {
		t.Fatalf("AddCaddyApp failed: %v", err)
	}
	if err := RemoveCaddyApp("one.apps.example.com"); err != nil {
		t.Fatalf("RemoveCaddyApp failed: %v", err)
	}

	final, err := os.Stat(caddyfile)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if !os.SameFile(mounted, final) {
		t.Fatal("Caddyfile inode changed across the init/deploy/remove sequence — the container's bind mount would be stale and no app would be routed")
	}

	data, err := os.ReadFile(caddyfile)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "two.apps.example.com {") {
		t.Errorf("remaining app block missing from Caddyfile:\n%s", content)
	}
	if strings.Contains(content, "one.apps.example.com {") {
		t.Errorf("removed app block still present:\n%s", content)
	}
	if !strings.Contains(content, "reverse_proxy host.docker.internal:9000") {
		t.Errorf("webhook route missing from Caddyfile:\n%s", content)
	}
}

// TestWriteCaddyfile_OverwritesExisting verifies that a second call replaces
// the existing file contents (this is the redeploy / Caddyfile-edit scenario).
func TestWriteCaddyfile_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatalf("setup WriteFile failed: %v", err)
	}
	if err := writeCaddyfile(path, []byte("new"), 0644); err != nil {
		t.Fatalf("writeCaddyfile failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(data) != "new" {
		t.Errorf("got %q, want \"new\"", string(data))
	}
}

// TestFilterCaddyDomain_RemovesBlock covers the basic line-filtering logic,
// independent of file I/O. AddCaddyApp's dedup and RemoveCaddyApp both rely
// on this helper, so a single unit test pins the behavior.
func TestFilterCaddyDomain_RemovesBlock(t *testing.T) {
	input := "{\n    email a@b.c\n}\n\nfoo.example.com {\n    reverse_proxy qd-foo:3000\n}\n\nbar.example.com {\n    reverse_proxy qd-bar:4000\n}\n"
	out := filterCaddyDomain(input, "foo.example.com")
	if strings.Contains(out, "foo.example.com") {
		t.Errorf("foo block should be removed:\n%s", out)
	}
	if !strings.Contains(out, "bar.example.com") {
		t.Errorf("bar block should remain:\n%s", out)
	}
}

// TestFilterCaddyDomain_NoMatch should be a no-op when the domain is absent.
func TestFilterCaddyDomain_NoMatch(t *testing.T) {
	input := "{\n    email a@b.c\n}\n\nfoo.example.com {\n    reverse_proxy qd-foo:3000\n}\n"
	out := filterCaddyDomain(input, "nope.example.com")
	if !strings.Contains(out, "foo.example.com") {
		t.Errorf("foo block should be preserved when filtering an unrelated domain:\n%s", out)
	}
}
