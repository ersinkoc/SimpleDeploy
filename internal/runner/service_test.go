package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupTestServiceDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	ServiceDir = dir
	return dir
}

func TestGenerateServiceCompose(t *testing.T) {
	content := generateServiceCompose("deploy.example.com", 9000)
	if !strings.Contains(content, "simpledeploy:latest") {
		t.Error("Should contain image name")
	}
	if !strings.Contains(content, "qd-service") {
		t.Error("Should contain container name")
	}
	if !strings.Contains(content, "deploy.example.com") {
		t.Error("Should contain base domain")
	}
	if !strings.Contains(content, "9000") {
		t.Error("Should contain webhook port")
	}
	if !strings.Contains(content, "Auto-generated") {
		t.Error("Should contain auto-generated header")
	}
	if !strings.Contains(content, "traefik.enable=true") {
		t.Error("Should contain traefik labels")
	}
}

func TestGenerateServiceCompose_CustomPort(t *testing.T) {
	content := generateServiceCompose("myapps.example.com", 8080)
	if !strings.Contains(content, "myapps.example.com") {
		t.Error("Should contain custom domain")
	}
	if !strings.Contains(content, "8080") {
		t.Error("Should contain custom port")
	}
}

func TestInstallService_WritesCompose(t *testing.T) {
	dir := setupTestServiceDir(t)

	err := InstallService("test.example.com", 9000)
	if err != nil {
		t.Fatalf("InstallService failed: %v", err)
	}

	composePath := filepath.Join(dir, "docker-compose.yml")
	if _, err := os.Stat(composePath); os.IsNotExist(err) {
		t.Fatal("docker-compose.yml should exist")
	}

	data, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("Failed to read compose: %v", err)
	}

	if !strings.Contains(string(data), "test.example.com") {
		t.Error("Compose should contain domain")
	}
}

func TestInstallService_CreatesDir(t *testing.T) {
	dir := t.TempDir()
	nestedDir := filepath.Join(dir, "nested", "service")
	ServiceDir = nestedDir

	err := InstallService("test.com", 9000)
	if err != nil {
		t.Fatalf("InstallService failed with nested dir: %v", err)
	}

	if _, err := os.Stat(nestedDir); os.IsNotExist(err) {
		t.Error("Should create nested directory")
	}
}

func TestStartService_NoCompose(t *testing.T) {
	setupTestServiceDir(t)
	// No docker-compose.yml → should fail

	old := os.Stdout
	devNull, _ := os.Open(os.DevNull)
	os.Stdout = devNull
	defer func() {
		devNull.Close()
		os.Stdout = old
	}()

	err := StartService()
	if err == nil {
		t.Error("Should fail without compose file")
	}
}

func TestStopService_NoCompose(t *testing.T) {
	setupTestServiceDir(t)

	old := os.Stdout
	devNull, _ := os.Open(os.DevNull)
	os.Stdout = devNull
	defer func() {
		devNull.Close()
		os.Stdout = old
	}()

	err := StopService()
	if err == nil {
		t.Error("Should fail without compose file")
	}
}
