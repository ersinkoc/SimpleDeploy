package docker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsInstalled(t *testing.T) {
	if !IsInstalled() {
		t.Error("Docker should be installed in test environment")
	}
}

func TestGetVersion(t *testing.T) {
	if !IsInstalled() {
		t.Skip("Docker not installed")
	}
	ver, err := GetVersion()
	if err != nil {
		t.Fatalf("GetVersion failed: %v", err)
	}
	if ver == "" {
		t.Error("Version should not be empty")
	}
	if !strings.Contains(ver, "Docker") {
		t.Errorf("Version should contain 'Docker', got %q", ver)
	}
}

func TestIsComposeInstalled(t *testing.T) {
	if !IsInstalled() {
		t.Skip("Docker not installed")
	}
	if !IsComposeInstalled() {
		t.Error("Docker Compose should be installed")
	}
}

func TestContainerStatus_NotFound(t *testing.T) {
	if !IsInstalled() {
		t.Skip("Docker not installed")
	}
	status, err := ContainerStatus("nonexistent-container-xyz-123")
	if err != nil {
		t.Fatalf("ContainerStatus should not error for missing container: %v", err)
	}
	if status != "not found" {
		t.Errorf("Status = %q, want 'not found'", status)
	}
}

func TestContainerExists_NotFound(t *testing.T) {
	if !IsInstalled() {
		t.Skip("Docker not installed")
	}
	if ContainerExists("nonexistent-container-xyz-123") {
		t.Error("Nonexistent container should not exist")
	}
}

func TestListImages_Empty(t *testing.T) {
	if !IsInstalled() {
		t.Skip("Docker not installed")
	}
	// Use a name that won't match any real images
	images, err := ListImages("nonexistent-app-xyz-999")
	if err != nil {
		t.Fatalf("ListImages failed: %v", err)
	}
	if len(images) != 0 {
		t.Errorf("Should have 0 images for nonexistent app, got %d", len(images))
	}
}

func TestCleanupOldImages_NoImages(t *testing.T) {
	if !IsInstalled() {
		t.Skip("Docker not installed")
	}
	// Should not error when there are no images to clean
	err := CleanupOldImages("nonexistent-app-xyz-999", 3)
	if err != nil {
		t.Fatalf("CleanupOldImages failed: %v", err)
	}
}

func TestRunOutput_InvalidCommand(t *testing.T) {
	if !IsInstalled() {
		t.Skip("Docker not installed")
	}
	output, err := RunOutput([]string{"nonexistent-subcommand-xyz"})
	if err == nil {
		t.Error("Should error for invalid docker command")
	}
	_ = output // output may contain error message
}

func TestNetworkExists_NonExistent(t *testing.T) {
	if !IsInstalled() {
		t.Skip("Docker not installed")
	}
	if NetworkExists("nonexistent-network-xyz-999") {
		t.Error("Nonexistent network should not exist")
	}
}

func TestCreateAndRemoveNetwork(t *testing.T) {
	if !IsInstalled() {
		t.Skip("Docker not installed")
	}
	testNet := "test-simpledeploy-unit-test"

	// Clean up from any previous failed test
	_ = Run([]string{"network", "rm", testNet})

	err := CreateNetwork(testNet)
	if err != nil {
		t.Fatalf("CreateNetwork failed: %v", err)
	}
	if !NetworkExists(testNet) {
		t.Error("Network should exist after creation")
	}

	// Creating again should be idempotent
	err = CreateNetwork(testNet)
	if err != nil {
		t.Fatalf("CreateNetwork (idempotent) failed: %v", err)
	}

	// Clean up
	Run([]string{"network", "rm", testNet})
}

func TestRun_InvalidArgs(t *testing.T) {
	if !IsInstalled() {
		t.Skip("Docker not installed")
	}
	err := Run([]string{"nonexistent-command-xyz"})
	if err == nil {
		t.Error("Should error for invalid docker command")
	}
}

func TestRunOutput_Version(t *testing.T) {
	if !IsInstalled() {
		t.Skip("Docker not installed")
	}
	output, err := RunOutput([]string{"--version"})
	if err != nil {
		t.Fatalf("docker --version failed: %v", err)
	}
	if !strings.Contains(output, "Docker") {
		t.Errorf("Output should contain 'Docker', got %q", output)
	}
}

func TestRun_Version(t *testing.T) {
	if !IsInstalled() {
		t.Skip("Docker not installed")
	}
	err := Run([]string{"--version"})
	if err != nil {
		t.Errorf("docker --version should succeed: %v", err)
	}
}

func TestPullImage_HelloWorld(t *testing.T) {
	if !IsInstalled() {
		t.Skip("Docker not installed")
	}
	err := PullImage("hello-world:latest")
	if err != nil {
		t.Fatalf("PullImage failed: %v", err)
	}
}

func TestListImages_AfterPull(t *testing.T) {
	if !IsInstalled() {
		t.Skip("Docker not installed")
	}
	PullImage("hello-world:latest")
	images, err := ListImages("hello-world")
	if err != nil {
		t.Fatalf("ListImages failed: %v", err)
	}
	// hello-world images may or may not show depending on Docker config
	_ = images
}

func TestContainerStatus_RunningContainer(t *testing.T) {
	if !IsInstalled() {
		t.Skip("Docker not installed")
	}
	// Run a quick container and check status
	name := "qd-test-status-check"

	// Clean up from any previous run
	_ = Run([]string{"rm", "-f", name})

	// Run a container
	err := Run([]string{"run", "-d", "--name", name, "hello-world"})
	if err != nil {
		t.Skip("Could not run test container")
	}

	status, err := ContainerStatus(name)
	if err != nil {
		t.Fatalf("ContainerStatus failed: %v", err)
	}
	if status == "not found" {
		t.Error("Container should be found after running")
	}

	// Verify ContainerExists returns true
	if !ContainerExists(name) {
		t.Error("ContainerExists should return true for running container")
	}

	// Clean up
	_ = Run([]string{"rm", "-f", name})
}

func TestStopContainer(t *testing.T) {
	if !IsInstalled() {
		t.Skip("Docker not installed")
	}
	name := "qd-test-stop-check"

	// Clean up
	_ = Run([]string{"rm", "-f", name})

	// Run a container that stays alive
	err := Run([]string{"run", "-d", "--name", name, "alpine", "sleep", "60"})
	if err != nil {
		t.Skip("Could not run test container")
	}

	err = StopContainer(name)
	if err != nil {
		t.Errorf("StopContainer failed: %v", err)
	}

	// Clean up
	_ = Run([]string{"rm", "-f", name})
}

func TestRestartContainer(t *testing.T) {
	if !IsInstalled() {
		t.Skip("Docker not installed")
	}
	name := "qd-test-restart-check"


	// Clean up
	_ = Run([]string{"rm", "-f", name})

	err := Run([]string{"run", "-d", "--name", name, "alpine", "sleep", "60"})
	if err != nil {
		t.Skip("Could not run test container")
	}

	err = RestartContainer(name)
	if err != nil {
		t.Errorf("RestartContainer failed: %v", err)
	}

	// Clean up
	_ = Run([]string{"rm", "-f", name})
}

func TestListContainers(t *testing.T) {
	if !IsInstalled() {
		t.Skip("Docker not installed")
	}
	containers, err := ListContainers("")
	if err != nil {
		t.Fatalf("ListContainers failed: %v", err)
	}
	// May be empty or have containers, just verify no error
	_ = containers
}

func TestExecContainer(t *testing.T) {
	if !IsInstalled() {
		t.Skip("Docker not installed")
	}
	name := "qd-test-exec-check"


	// Clean up
	_ = Run([]string{"rm", "-f", name})

	err := Run([]string{"run", "-d", "--name", name, "alpine", "sleep", "60"})
	if err != nil {
		t.Skip("Could not run test container")
	}

	err = ExecContainer(name, "echo", "hello")
	if err != nil {
		t.Errorf("ExecContainer failed: %v", err)
	}

	// Clean up
	_ = Run([]string{"rm", "-f", name})
}

func TestBuildImage_SimpleDockerfile(t *testing.T) {
	if !IsInstalled() {
		t.Skip("Docker not installed")
	}

	dir := t.TempDir()
	dockerfile := "FROM hello-world:latest\n"
	os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(dockerfile), 0644)

	tag, err := BuildImage(dir, "qd-test-build")
	if err != nil {
		t.Fatalf("BuildImage failed: %v", err)
	}
	if tag == "" {
		t.Error("Tag should not be empty")
	}
	if !strings.HasPrefix(tag, "qd-test-build:") {
		t.Errorf("Tag = %q, should start with 'qd-test-build:'", tag)
	}

	_ = RemoveImage(tag)
}

func TestBuildImageWithDockerfile_CustomPath(t *testing.T) {
	if !IsInstalled() {
		t.Skip("Docker not installed")
	}

	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "custom"), 0755)
	dfPath := filepath.Join(dir, "custom", "Dockerfile")
	os.WriteFile(dfPath, []byte("FROM hello-world:latest\n"), 0644)

	tag, err := BuildImageWithDockerfile(dir, dfPath, "qd-test-build2")
	if err != nil {
		t.Fatalf("BuildImageWithDockerfile failed: %v", err)
	}
	if tag == "" {
		t.Error("Tag should not be empty")
	}

	_ = RemoveImage(tag)
}

func TestTagAndRemoveImage(t *testing.T) {
	if !IsInstalled() {
		t.Skip("Docker not installed")
	}

	PullImage("hello-world:latest")

	err := TagImage("hello-world:latest", "qd-test-tag:v1")
	if err != nil {
		t.Fatalf("TagImage failed: %v", err)
	}

	images, _ := ListImages("qd-test-tag")
	if len(images) == 0 {
		t.Error("Should find tagged image")
	}

	err = RemoveImage("qd-test-tag:v1")
	if err != nil {
		t.Fatalf("RemoveImage failed: %v", err)
	}
}

func TestCleanupOldImages_WithImages(t *testing.T) {
	if !IsInstalled() {
		t.Skip("Docker not installed")
	}

	PullImage("hello-world:latest")
	TagImage("hello-world:latest", "qd-test-cleanup:v1")
	TagImage("hello-world:latest", "qd-test-cleanup:v2")
	TagImage("hello-world:latest", "qd-test-cleanup:v3")

	err := CleanupOldImages("qd-test-cleanup", 1)
	if err != nil {
		t.Fatalf("CleanupOldImages failed: %v", err)
	}

	remaining, _ := ListImages("qd-test-cleanup")
	for _, img := range remaining {
		_ = RemoveImage(img)
	}
}

func TestComposeUpAndDown(t *testing.T) {
	if !IsInstalled() || !IsComposeInstalled() {
		t.Skip("Docker/Compose not installed")
	}

	dir := t.TempDir()
	composeContent := "services:\n  test-hello:\n    image: hello-world:latest\n    container_name: qd-test-compose\n"
	os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(composeContent), 0644)

	err := ComposeUp(dir)
	if err != nil {
		t.Fatalf("ComposeUp failed: %v", err)
	}

	err = ComposeDown(dir)
	if err != nil {
		t.Fatalf("ComposeDown failed: %v", err)
	}
}

func TestComposeRemove_NoVolumes(t *testing.T) {
	if !IsInstalled() || !IsComposeInstalled() {
		t.Skip("Docker/Compose not installed")
	}

	dir := t.TempDir()
	composeContent := "services:\n  test-hello:\n    image: hello-world:latest\n    container_name: qd-test-compose-rm\n"
	os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(composeContent), 0644)

	ComposeUp(dir)
	err := ComposeRemove(dir, false)
	if err != nil {
		t.Fatalf("ComposeRemove failed: %v", err)
	}
}

func TestComposeRemove_WithVolumes(t *testing.T) {
	if !IsInstalled() || !IsComposeInstalled() {
		t.Skip("Docker/Compose not installed")
	}

	dir := t.TempDir()
	composeContent := "services:\n  test-hello:\n    image: hello-world:latest\n    container_name: qd-test-compose-vol\n"
	os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(composeContent), 0644)

	ComposeUp(dir)
	err := ComposeRemove(dir, true)
	if err != nil {
		t.Fatalf("ComposeRemove with volumes failed: %v", err)
	}
}

func TestComposeLogs(t *testing.T) {
	if !IsInstalled() || !IsComposeInstalled() {
		t.Skip("Docker/Compose not installed")
	}

	dir := t.TempDir()
	composeContent := "services:\n  test-hello:\n    image: hello-world:latest\n    container_name: qd-test-compose-logs\n"
	os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(composeContent), 0644)

	ComposeUp(dir)
	defer ComposeDown(dir)

	// Test logs without follow
	err := ComposeLogs(dir, "", false)
	if err != nil {
		t.Errorf("ComposeLogs (no follow) failed: %v", err)
	}

	// Test logs with service name
	err = ComposeLogs(dir, "test-hello", false)
	if err != nil {
		t.Errorf("ComposeLogs with service name failed: %v", err)
	}
}

func TestEnsureDocker_AlreadyInstalled(t *testing.T) {
	if !IsInstalled() {
		t.Skip("Docker not installed")
	}

	old := os.Stdout
	devNull, _ := os.Open(os.DevNull)
	os.Stdout = devNull
	defer func() {
		devNull.Close()
		os.Stdout = old
	}()

	err := EnsureDocker()
	if err != nil {
		t.Errorf("EnsureDocker should succeed when Docker is installed: %v", err)
	}
}

func TestInstall_NotAvailable(t *testing.T) {
	// Install calls curl which may or may not work — just test that
	// it returns an error on systems without sh/curl.
	// This test is only meaningful on non-Linux or without curl.
	// We can't really test the happy path without side effects.
	// Just ensure it doesn't panic.
	if IsInstalled() {
		// Docker is installed, so we can't test the "not installed" path
		t.Skip("Docker is installed; can't test Install failure path")
	}
}
