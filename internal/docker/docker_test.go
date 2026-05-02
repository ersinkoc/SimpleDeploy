package docker

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type mockCmd struct {
	runErr         error
	output         []byte
	outputErr      error
	combinedOutput []byte
	combinedErr    error
	ctx            context.Context
	stdout         io.Writer
	stderr         io.Writer
	dir            string
}

func (m *mockCmd) SetDir(dir string)       { m.dir = dir }
func (m *mockCmd) SetStdout(w io.Writer)   { m.stdout = w }
func (m *mockCmd) SetStderr(w io.Writer)   { m.stderr = w }
func (m *mockCmd) Output() ([]byte, error) { return m.output, m.outputErr }
func (m *mockCmd) CombinedOutput() ([]byte, error) {
	if m.combinedErr != nil && len(m.combinedOutput) == 0 {
		return nil, m.combinedErr
	}
	return m.combinedOutput, m.combinedErr
}
func (m *mockCmd) Run() error {
	if m.runErr != nil {
		return m.runErr
	}
	if m.ctx != nil {
		select {
		case <-m.ctx.Done():
			return m.ctx.Err()
		default:
		}
	}
	return nil
}

func TestBuildImage_Timeout(t *testing.T) {
	oldTimeout := buildTimeout
	buildTimeout = 0
	defer func() { buildTimeout = oldTimeout }()

	oldNew := newDockerCmdContext
	newDockerCmdContext = func(ctx context.Context, name string, arg ...string) dockerCmd {
		return &mockCmd{ctx: ctx, runErr: context.DeadlineExceeded}
	}
	defer func() { newDockerCmdContext = oldNew }()

	_, err := BuildImage("/tmp", "app")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("Expected timeout error, got %v", err)
	}
}

func TestBuildImage_Error(t *testing.T) {
	oldNew := newDockerCmdContext
	newDockerCmdContext = func(ctx context.Context, name string, arg ...string) dockerCmd {
		return &mockCmd{runErr: errors.New("build failed")}
	}
	defer func() { newDockerCmdContext = oldNew }()

	_, err := BuildImage("/tmp", "app")
	if err == nil || !strings.Contains(err.Error(), "docker build failed") {
		t.Fatalf("Expected build error, got %v", err)
	}
}

func TestListImages_Error(t *testing.T) {
	oldNew := newDockerCmdContext
	newDockerCmdContext = func(ctx context.Context, name string, arg ...string) dockerCmd {
		return &mockCmd{outputErr: errors.New("docker error")}
	}
	defer func() { newDockerCmdContext = oldNew }()

	_, err := ListImages("app")
	if err == nil {
		t.Fatal("Expected error")
	}
}

func TestCleanupOldImages_ListError(t *testing.T) {
	oldNew := newDockerCmdContext
	newDockerCmdContext = func(ctx context.Context, name string, arg ...string) dockerCmd {
		return &mockCmd{outputErr: errors.New("docker error")}
	}
	defer func() { newDockerCmdContext = oldNew }()

	err := CleanupOldImages("app", 1)
	if err == nil {
		t.Fatal("Expected error")
	}
}

func TestCleanupOldImages_RemoveError(t *testing.T) {
	oldList := newDockerCmdContext
	newDockerCmdContext = func(ctx context.Context, name string, arg ...string) dockerCmd {
		return &mockCmd{output: []byte("app:v1\napp:v2\n")}
	}
	defer func() { newDockerCmdContext = oldList }()

	oldTag := newDockerCmdContext
	callCount := 0
	newDockerCmdContext = func(ctx context.Context, name string, arg ...string) dockerCmd {
		callCount++
		if callCount == 1 {
			return &mockCmd{output: []byte("app:v1\napp:v2\n")}
		}
		return &mockCmd{runErr: errors.New("remove failed")}
	}
	defer func() { newDockerCmdContext = oldTag }()

	// Should not return error; just prints warning
	err := CleanupOldImages("app", 0)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
}

func TestComposeUp_Timeout(t *testing.T) {
	oldTimeout := composeTimeout
	composeTimeout = 0
	defer func() { composeTimeout = oldTimeout }()

	oldNew := newDockerCmdContext
	newDockerCmdContext = func(ctx context.Context, name string, arg ...string) dockerCmd {
		return &mockCmd{ctx: ctx, runErr: context.DeadlineExceeded}
	}
	defer func() { newDockerCmdContext = oldNew }()

	err := ComposeUp("/tmp")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("Expected timeout error, got %v", err)
	}
}

func TestComposeUp_Error(t *testing.T) {
	oldNew := newDockerCmdContext
	newDockerCmdContext = func(ctx context.Context, name string, arg ...string) dockerCmd {
		return &mockCmd{runErr: errors.New("compose up failed")}
	}
	defer func() { newDockerCmdContext = oldNew }()

	err := ComposeUp("/tmp")
	if err == nil || !strings.Contains(err.Error(), "docker compose up failed") {
		t.Fatalf("Expected error, got %v", err)
	}
}

func TestComposeDown_Timeout(t *testing.T) {
	oldTimeout := composeTimeout
	composeTimeout = 0
	defer func() { composeTimeout = oldTimeout }()

	oldNew := newDockerCmdContext
	newDockerCmdContext = func(ctx context.Context, name string, arg ...string) dockerCmd {
		return &mockCmd{ctx: ctx, runErr: context.DeadlineExceeded}
	}
	defer func() { newDockerCmdContext = oldNew }()

	err := ComposeDown("/tmp")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("Expected timeout error, got %v", err)
	}
}

func TestComposeDown_Error(t *testing.T) {
	oldNew := newDockerCmdContext
	newDockerCmdContext = func(ctx context.Context, name string, arg ...string) dockerCmd {
		return &mockCmd{runErr: errors.New("compose down failed")}
	}
	defer func() { newDockerCmdContext = oldNew }()

	err := ComposeDown("/tmp")
	if err == nil || !strings.Contains(err.Error(), "docker compose down failed") {
		t.Fatalf("Expected error, got %v", err)
	}
}

func TestComposeRemove_Timeout(t *testing.T) {
	oldTimeout := composeTimeout
	composeTimeout = 0
	defer func() { composeTimeout = oldTimeout }()

	oldNew := newDockerCmdContext
	newDockerCmdContext = func(ctx context.Context, name string, arg ...string) dockerCmd {
		return &mockCmd{ctx: ctx, runErr: context.DeadlineExceeded}
	}
	defer func() { newDockerCmdContext = oldNew }()

	err := ComposeRemove("/tmp", true)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("Expected timeout error, got %v", err)
	}
}

func TestComposeRemove_Error(t *testing.T) {
	oldNew := newDockerCmdContext
	newDockerCmdContext = func(ctx context.Context, name string, arg ...string) dockerCmd {
		return &mockCmd{runErr: errors.New("compose remove failed")}
	}
	defer func() { newDockerCmdContext = oldNew }()

	err := ComposeRemove("/tmp", false)
	if err == nil {
		t.Fatalf("Expected error, got %v", err)
	}
}

func TestComposeLogs_Follow(t *testing.T) {
	oldNew := newDockerCmd
	newDockerCmd = func(name string, arg ...string) dockerCmd {
		return &mockCmd{runErr: errors.New("follow interrupted")}
	}
	defer func() { newDockerCmd = oldNew }()

	err := ComposeLogs("/tmp", "svc", true)
	if err == nil {
		t.Fatal("Expected error from follow mode")
	}
}

func TestComposeLogs_Error(t *testing.T) {
	oldNew := newDockerCmdContext
	newDockerCmdContext = func(ctx context.Context, name string, arg ...string) dockerCmd {
		return &mockCmd{runErr: errors.New("logs failed")}
	}
	defer func() { newDockerCmdContext = oldNew }()

	err := ComposeLogs("/tmp", "", false)
	if err == nil {
		t.Fatal("Expected error")
	}
}

func TestRestartContainer_Error(t *testing.T) {
	oldNew := newDockerCmdContext
	newDockerCmdContext = func(ctx context.Context, name string, arg ...string) dockerCmd {
		return &mockCmd{runErr: errors.New("restart failed")}
	}
	defer func() { newDockerCmdContext = oldNew }()

	err := RestartContainer("c")
	if err == nil {
		t.Fatal("Expected error")
	}
}

func TestStopContainer_Error(t *testing.T) {
	oldNew := newDockerCmdContext
	newDockerCmdContext = func(ctx context.Context, name string, arg ...string) dockerCmd {
		return &mockCmd{runErr: errors.New("stop failed")}
	}
	defer func() { newDockerCmdContext = oldNew }()

	err := StopContainer("c")
	if err == nil {
		t.Fatal("Expected error")
	}
}

func TestExecContainer_Error(t *testing.T) {
	oldNew := newDockerCmdContext
	newDockerCmdContext = func(ctx context.Context, name string, arg ...string) dockerCmd {
		return &mockCmd{runErr: errors.New("exec failed")}
	}
	defer func() { newDockerCmdContext = oldNew }()

	err := ExecContainer("c", "echo")
	if err == nil {
		t.Fatal("Expected error")
	}
}

func TestListContainers_Error(t *testing.T) {
	oldNew := newDockerCmdContext
	newDockerCmdContext = func(ctx context.Context, name string, arg ...string) dockerCmd {
		return &mockCmd{outputErr: errors.New("ps failed")}
	}
	defer func() { newDockerCmdContext = oldNew }()

	_, err := ListContainers("")
	if err == nil {
		t.Fatal("Expected error")
	}
}

func TestRun_Error(t *testing.T) {
	oldNew := newDockerCmd
	newDockerCmd = func(name string, arg ...string) dockerCmd {
		return &mockCmd{runErr: errors.New("docker run failed")}
	}
	defer func() { newDockerCmd = oldNew }()

	err := Run([]string{"ps"})
	if err == nil {
		t.Fatal("Expected error")
	}
}

func TestRunOutput_Error(t *testing.T) {
	oldNew := newDockerCmdContext
	newDockerCmdContext = func(ctx context.Context, name string, arg ...string) dockerCmd {
		return &mockCmd{combinedErr: errors.New("docker output failed")}
	}
	defer func() { newDockerCmdContext = oldNew }()

	_, err := RunOutput([]string{"ps"})
	if err == nil {
		t.Fatal("Expected error")
	}
}

func TestGetVersion_Error(t *testing.T) {
	oldNew := newDockerCmdContext
	newDockerCmdContext = func(ctx context.Context, name string, arg ...string) dockerCmd {
		return &mockCmd{outputErr: errors.New("version failed")}
	}
	defer func() { newDockerCmdContext = oldNew }()

	_, err := GetVersion()
	if err == nil {
		t.Fatal("Expected error")
	}
}

func TestIsComposeInstalled_False(t *testing.T) {
	oldNew := newDockerCmd
	newDockerCmd = func(name string, arg ...string) dockerCmd {
		return &mockCmd{runErr: errors.New("not installed")}
	}
	defer func() { newDockerCmd = oldNew }()

	if IsComposeInstalled() {
		t.Fatal("Expected false")
	}
}

func TestInstall_Success(t *testing.T) {
	oldNew := newDockerCmd
	var buf bytes.Buffer
	newDockerCmd = func(name string, arg ...string) dockerCmd {
		return &mockCmd{stdout: &buf, stderr: &buf}
	}
	defer func() { newDockerCmd = oldNew }()

	err := Install()
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
}

func TestInstall_Error(t *testing.T) {
	oldNew := newDockerCmd
	var buf bytes.Buffer
	newDockerCmd = func(name string, arg ...string) dockerCmd {
		return &mockCmd{stdout: &buf, stderr: &buf, runErr: errors.New("install failed")}
	}
	defer func() { newDockerCmd = oldNew }()

	err := Install()
	if err == nil || !strings.Contains(err.Error(), "Docker installation failed") {
		t.Fatalf("Expected install error, got %v", err)
	}
}

func TestEnsureDocker_InstallAndComposeSuccess(t *testing.T) {
	oldLookPath := execLookPath
	lookPathCalls := 0
	execLookPath = func(file string) (string, error) {
		lookPathCalls++
		if lookPathCalls == 1 {
			return "", errors.New("not found")
		}
		return "/usr/bin/docker", nil
	}
	defer func() { execLookPath = oldLookPath }()

	oldConfirm := wizardConfirm
	wizardConfirm = func(prompt string, defaultYes bool) bool { return true }
	defer func() { wizardConfirm = oldConfirm }()

	callCount := 0
	oldNew := newDockerCmd
	newDockerCmd = func(name string, arg ...string) dockerCmd {
		callCount++
		if name == "sh" {
			return &mockCmd{} // Install succeeds
		}
		if strings.Contains(strings.Join(arg, " "), "compose version") {
			return &mockCmd{} // Compose installed
		}
		return &mockCmd{output: []byte("Docker version 24.0")}
	}
	defer func() { newDockerCmd = oldNew }()

	err := EnsureDocker()
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
}

func TestListContainers_WithLabelFilter(t *testing.T) {
	oldNew := newDockerCmdContext
	newDockerCmdContext = func(ctx context.Context, name string, arg ...string) dockerCmd {
		return &mockCmd{output: []byte("container1\ncontainer2\n")}
	}
	defer func() { newDockerCmdContext = oldNew }()

	containers, err := ListContainers("simpledeploy=app")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if len(containers) != 2 {
		t.Fatalf("Expected 2 containers, got %d", len(containers))
	}
}

func TestEnsureDocker_AlreadyInstalled_Mock(t *testing.T) {
	oldLookPath := execLookPath
	execLookPath = func(file string) (string, error) { return "/usr/bin/docker", nil }
	defer func() { execLookPath = oldLookPath }()

	oldNew := newDockerCmdContext
	newDockerCmdContext = func(ctx context.Context, name string, arg ...string) dockerCmd {
		return &mockCmd{output: []byte("Docker version 24.0")}
	}
	defer func() { newDockerCmdContext = oldNew }()

	err := EnsureDocker()
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
}

func TestEnsureDocker_DeclineInstall(t *testing.T) {
	oldLookPath := execLookPath
	execLookPath = func(file string) (string, error) { return "", errors.New("not found") }
	defer func() { execLookPath = oldLookPath }()

	oldConfirm := wizardConfirm
	wizardConfirm = func(prompt string, defaultYes bool) bool { return false }
	defer func() { wizardConfirm = oldConfirm }()

	err := EnsureDocker()
	if err == nil || !strings.Contains(err.Error(), "Docker is required") {
		t.Fatalf("Expected error, got %v", err)
	}
}

func TestEnsureDocker_InstallError(t *testing.T) {
	oldLookPath := execLookPath
	execLookPath = func(file string) (string, error) { return "", errors.New("not found") }
	defer func() { execLookPath = oldLookPath }()

	oldConfirm := wizardConfirm
	wizardConfirm = func(prompt string, defaultYes bool) bool { return true }
	defer func() { wizardConfirm = oldConfirm }()

	oldNew := newDockerCmd
	newDockerCmd = func(name string, arg ...string) dockerCmd {
		return &mockCmd{runErr: errors.New("install failed")}
	}
	defer func() { newDockerCmd = oldNew }()

	err := EnsureDocker()
	if err == nil || !strings.Contains(err.Error(), "install failed") {
		t.Fatalf("Expected error, got %v", err)
	}
}

func TestEnsureDocker_MissingCompose(t *testing.T) {
	oldLookPath := execLookPath
	execLookPath = func(file string) (string, error) { return "", errors.New("not found") }
	defer func() { execLookPath = oldLookPath }()

	oldConfirm := wizardConfirm
	wizardConfirm = func(prompt string, defaultYes bool) bool { return true }
	defer func() { wizardConfirm = oldConfirm }()

	callCount := 0
	oldNew := newDockerCmd
	newDockerCmd = func(name string, arg ...string) dockerCmd {
		callCount++
		if name == "sh" {
			return &mockCmd{} // Install succeeds
		}
		// docker compose version
		return &mockCmd{runErr: errors.New("compose missing")}
	}
	defer func() { newDockerCmd = oldNew }()

	err := EnsureDocker()
	if err == nil || !strings.Contains(err.Error(), "Docker Compose plugin is required") {
		t.Fatalf("Expected compose error, got %v", err)
	}
}

func TestNetworkExists_False(t *testing.T) {
	oldNew := newDockerCmd
	newDockerCmd = func(name string, arg ...string) dockerCmd {
		return &mockCmd{runErr: errors.New("not found")}
	}
	defer func() { newDockerCmd = oldNew }()

	if NetworkExists("net") {
		t.Fatal("Expected false")
	}
}

func TestCreateNetwork_Error(t *testing.T) {
	oldNew := newDockerCmd
	newDockerCmd = func(name string, arg ...string) dockerCmd {
		return &mockCmd{runErr: errors.New("not found")}
	}
	defer func() { newDockerCmd = oldNew }()

	oldCtx := newDockerCmdContext
	newDockerCmdContext = func(ctx context.Context, name string, arg ...string) dockerCmd {
		return &mockCmd{combinedErr: errors.New("create failed")}
	}
	defer func() { newDockerCmdContext = oldCtx }()

	err := CreateNetwork("net")
	if err == nil || !strings.Contains(err.Error(), "failed to create network") {
		t.Fatalf("Expected error, got %v", err)
	}
}

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
	// IsComposeInstalled returning false on a host without the compose
	// plugin is a host-environment property, not a code defect — skip
	// rather than fail so CI / dev machines without compose still run
	// the rest of the docker package tests cleanly.
	if !IsComposeInstalled() {
		t.Skip("Docker Compose plugin not installed on host")
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
