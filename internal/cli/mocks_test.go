package cli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cfgpkg "github.com/ersinkoc/SimpleDeploy/internal/config"
	"github.com/ersinkoc/SimpleDeploy/internal/state"
)

// ------------------------------------------------------------------
// Helpers
// ------------------------------------------------------------------

func setupDeployTest(t *testing.T, files map[string]string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	state.InitState(dir)
	cfgpkg.BaseDir = filepath.Join(dir, "opt", "simpledeploy")
	cfg := &state.GlobalConfig{Proxy: "traefik", BaseDomain: "test.example.com"}
	state.SaveConfig(cfg)
	repoDir := filepath.Join(dir, "repo")
	os.MkdirAll(repoDir, 0755)
	for name, content := range files {
		os.WriteFile(filepath.Join(repoDir, name), []byte(content), 0644)
	}
	return repoDir, ""
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = dir
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		t.Fatalf("git init failed: %v", err)
	}
	exec.Command("git", "config", "user.email", "test@test.com").Run()
	exec.Command("git", "config", "user.name", "Test").Run()
	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hello"), 0644)
	cmd = exec.Command("git", "add", ".")
	cmd.Dir = dir
	cmd.Run()
	cmd = exec.Command("git", "commit", "-m", "initial")
	cmd.Dir = dir
	cmd.Run()
}

func deployInputBasic(repoDir, appType, appName, startDeploy string) string {
	inputs := []string{repoDir, "", "n", appName, appType, "3000", "", "n", "6", appName, "", "n", startDeploy}
	return strings.Join(inputs, "\n") + "\n"
}

func mockDeploySuccess() func() {
	oldBuild := dockerBuildImage
	oldComposeWrite := composeWriteCompose
	oldComposeUp := dockerComposeUp
	oldStatus := dockerContainerStatus
	oldBuildpack := buildpackWriteDockerfile

	dockerBuildImage = func(dir, name string) (string, error) { return "test:v1", nil }
	composeWriteCompose = func(dir, content string) error { return nil }
	dockerComposeUp = func(dir string) error { return nil }
	dockerContainerStatus = func(name string) (string, error) { return "running", nil }
	buildpackWriteDockerfile = func(dir, appType string) error { return nil }

	return func() {
		dockerBuildImage = oldBuild
		composeWriteCompose = oldComposeWrite
		dockerComposeUp = oldComposeUp
		dockerContainerStatus = oldStatus
		buildpackWriteDockerfile = oldBuildpack
	}
}

// ------------------------------------------------------------------
// Home / Route / Misc
// ------------------------------------------------------------------

func TestHomeDir_Error(t *testing.T) {
	old := osUserHomeDir
	osUserHomeDir = func() (string, error) { return "", errors.New("no home") }
	defer func() { osUserHomeDir = old }()
	if homeDir() != "/root" {
		t.Error("Expected /root fallback")
	}
}

func TestRoute_Status(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)
	state.SaveConfig(&state.GlobalConfig{Proxy: "traefik", BaseDomain: "test.example.com"})
	if err := Route([]string{"status"}); err != nil {
		t.Errorf("status should not error: %v", err)
	}
}

func TestRoute_Init(t *testing.T) {
	old := dockerEnsureDocker
	dockerEnsureDocker = func() error { return errors.New("docker not installed") }
	defer func() { dockerEnsureDocker = old }()
	if err := Route([]string{"init"}); err == nil {
		t.Error("Expected error")
	}
}

func TestRoute_Deploy(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)
	state.SaveConfig(&state.GlobalConfig{Proxy: "traefik", BaseDomain: "test.example.com"})
	setWizardInput(t, "n\n")
	_ = Route([]string{"deploy"})
}

func TestRoute_WebhookInvalidPort(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)
	state.SaveConfig(&state.GlobalConfig{Proxy: "traefik", BaseDomain: "test.example.com", WebhookPort: 9000, WebhookSecret: "s"})
	go Route([]string{"webhook", "start", "--port", "invalid"})
}

func TestReplaceAppImage_NoImageLine(t *testing.T) {
	input := "services:\n  myapp:\n    ports:\n      - \"3000:3000\"\n"
	if replaceAppImage(input, "myapp", "myapp:new") != input {
		t.Error("Should not modify when no image line")
	}
}

func TestReplaceAppImage_BreakOnOtherService(t *testing.T) {
	input := "  myapp:\n    ports:\n      - \"3000:3000\"\nother:\n  image: other:old\n"
	result := replaceAppImage(input, "myapp", "myapp:new")
	if strings.Contains(result, "myapp:new") {
		t.Error("Should not replace when no image under app service")
	}
	if !strings.Contains(result, "other:old") {
		t.Error("Should keep other service image")
	}
}

func TestLogDeploy_WriteError(t *testing.T) {
	old := osOpenFileFunc
	osOpenFileFunc = func(name string, flag int, perm os.FileMode) (fileWriter, error) { return &failWriter{}, nil }
	defer func() { osOpenFileFunc = old }()
	logDeploy("/tmp", "app", "app:v1")
}

func TestLogDeploy_DiscardWriter(t *testing.T) {
	old := osOpenFileFunc
	osOpenFileFunc = func(name string, flag int, perm os.FileMode) (fileWriter, error) { return &discardWriter{}, nil }
	defer func() { osOpenFileFunc = old }()
	logDeploy("/tmp", "app", "app:v1")
}

// ------------------------------------------------------------------
// Init
// ------------------------------------------------------------------

func TestRunInit_GenerateSecretError(t *testing.T) {
	old := stateGenerateSecret
	stateGenerateSecret = func(prefix string, length int) (string, error) { return "", errors.New("fail") }
	defer func() { stateGenerateSecret = old }()
	dir := t.TempDir()
	state.InitState(dir)
	setWizardInput(t, "1\ntest.com\n\nadmin@test.com\n\n\n")
	_ = captureStdout(func() {
		if err := RunInit(); err == nil {
			t.Error("Expected error")
		}
	})
}

func TestRunInit_SaveConfigError(t *testing.T) {
	old := stateSaveConfig
	stateSaveConfig = func(cfg *state.GlobalConfig) error { return errors.New("fail") }
	defer func() { stateSaveConfig = old }()
	dir := t.TempDir()
	state.InitState(dir)
	setWizardInput(t, "1\ntest.com\n\nadmin@test.com\n\n\n")
	_ = captureStdout(func() {
		if err := RunInit(); err == nil {
			t.Error("Expected error")
		}
	})
}

func TestRunInit_SetupTraefikError(t *testing.T) {
	old := proxySetupTraefik
	proxySetupTraefik = func(email string) error { return errors.New("fail") }
	defer func() { proxySetupTraefik = old }()
	dir := t.TempDir()
	state.InitState(dir)
	setWizardInput(t, "1\ntest.com\n\nadmin@test.com\n\n\n")
	_ = captureStdout(func() {
		if err := RunInit(); err == nil {
			t.Error("Expected error")
		}
	})
}

func TestRunInit_SetupCaddyError(t *testing.T) {
	old := proxySetupCaddy
	proxySetupCaddy = func(email string) error { return errors.New("fail") }
	defer func() { proxySetupCaddy = old }()
	dir := t.TempDir()
	state.InitState(dir)
	setWizardInput(t, "2\ntest.com\n\nadmin@test.com\n\n\n")
	_ = captureStdout(func() {
		if err := RunInit(); err == nil {
			t.Error("Expected error")
		}
	})
}

// ------------------------------------------------------------------
// Restart / Stop / Exec / Logs / Status
// ------------------------------------------------------------------

func TestRunRestart_Success(t *testing.T) {
	old := dockerRestartContainer
	dockerRestartContainer = func(name string) error { return nil }
	defer func() { dockerRestartContainer = old }()
	dir := t.TempDir()
	state.InitState(dir)
	app := state.NewAppConfig()
	app.Name = "restarttest"
	state.SaveApp(app)
	if err := RunRestart([]string{"restarttest"}); err != nil {
		t.Fatalf("Expected no error: %v", err)
	}
	app, _ = state.GetApp("restarttest")
	if app.Status != "running" {
		t.Errorf("Status = %q, want running", app.Status)
	}
}

func TestRunRestart_DockerError(t *testing.T) {
	old := dockerRestartContainer
	dockerRestartContainer = func(name string) error { return errors.New("fail") }
	defer func() { dockerRestartContainer = old }()
	dir := t.TempDir()
	state.InitState(dir)
	app := state.NewAppConfig()
	app.Name = "rdockererr"
	state.SaveApp(app)
	if err := RunRestart([]string{"rdockererr"}); err == nil {
		t.Error("Expected error")
	}
}

func TestRunRestart_StateGetError(t *testing.T) {
	oldRestart := dockerRestartContainer
	dockerRestartContainer = func(name string) error { return nil }
	defer func() { dockerRestartContainer = oldRestart }()
	oldGet := stateGetApp
	calls := 0
	stateGetApp = func(name string) (*state.AppConfig, error) {
		calls++
		if calls == 1 {
			return &state.AppConfig{Name: name}, nil
		}
		return nil, errors.New("fail")
	}
	defer func() { stateGetApp = oldGet }()
	dir := t.TempDir()
	state.InitState(dir)
	if err := RunRestart([]string{"rstategeterr"}); err != nil {
		t.Fatalf("Expected no error: %v", err)
	}
}

func TestRunRestart_StateSaveError(t *testing.T) {
	oldRestart := dockerRestartContainer
	dockerRestartContainer = func(name string) error { return nil }
	defer func() { dockerRestartContainer = oldRestart }()
	oldSave := stateSaveApp
	stateSaveApp = func(app *state.AppConfig) error { return errors.New("fail") }
	defer func() { stateSaveApp = oldSave }()
	dir := t.TempDir()
	state.InitState(dir)
	app := state.NewAppConfig()
	app.Name = "rsaveerr"
	state.SaveApp(app)
	_ = captureStdout(func() {
		if err := RunRestart([]string{"rsaveerr"}); err != nil {
			t.Fatalf("Expected no error: %v", err)
		}
	})
}

func TestRunStop_Success(t *testing.T) {
	old := dockerStopContainer
	dockerStopContainer = func(name string) error { return nil }
	defer func() { dockerStopContainer = old }()
	dir := t.TempDir()
	state.InitState(dir)
	app := state.NewAppConfig()
	app.Name = "stoptest"
	state.SaveApp(app)
	if err := RunStop([]string{"stoptest"}); err != nil {
		t.Fatalf("Expected no error: %v", err)
	}
	app, _ = state.GetApp("stoptest")
	if app.Status != "stopped" {
		t.Errorf("Status = %q, want stopped", app.Status)
	}
}

func TestRunStop_DockerError(t *testing.T) {
	old := dockerStopContainer
	dockerStopContainer = func(name string) error { return errors.New("fail") }
	defer func() { dockerStopContainer = old }()
	dir := t.TempDir()
	state.InitState(dir)
	app := state.NewAppConfig()
	app.Name = "sdockererr"
	state.SaveApp(app)
	if err := RunStop([]string{"sdockererr"}); err == nil {
		t.Error("Expected error")
	}
}

func TestRunStop_StateGetError(t *testing.T) {
	oldStop := dockerStopContainer
	dockerStopContainer = func(name string) error { return nil }
	defer func() { dockerStopContainer = oldStop }()
	oldGet := stateGetApp
	calls := 0
	stateGetApp = func(name string) (*state.AppConfig, error) {
		calls++
		if calls == 1 {
			return &state.AppConfig{Name: name}, nil
		}
		return nil, errors.New("fail")
	}
	defer func() { stateGetApp = oldGet }()
	dir := t.TempDir()
	state.InitState(dir)
	if err := RunStop([]string{"sstategeterr"}); err != nil {
		t.Fatalf("Expected no error: %v", err)
	}
}

func TestRunStop_StateSaveError(t *testing.T) {
	oldStop := dockerStopContainer
	dockerStopContainer = func(name string) error { return nil }
	defer func() { dockerStopContainer = oldStop }()
	oldSave := stateSaveApp
	stateSaveApp = func(app *state.AppConfig) error { return errors.New("fail") }
	defer func() { stateSaveApp = oldSave }()
	dir := t.TempDir()
	state.InitState(dir)
	app := state.NewAppConfig()
	app.Name = "ssaveerr"
	state.SaveApp(app)
	_ = captureStdout(func() {
		if err := RunStop([]string{"ssaveerr"}); err != nil {
			t.Fatalf("Expected no error: %v", err)
		}
	})
}

func TestRunExec_TooFewArgs(t *testing.T) {
	if err := RunExec([]string{"myapp"}); err == nil {
		t.Error("Expected error")
	}
}

func TestRunExec_Success(t *testing.T) {
	old := dockerExecContainer
	dockerExecContainer = func(name string, args ...string) error { return nil }
	defer func() { dockerExecContainer = old }()
	dir := t.TempDir()
	state.InitState(dir)
	app := state.NewAppConfig()
	app.Name = "exectest"
	state.SaveApp(app)
	if err := RunExec([]string{"exectest", "echo", "hi"}); err != nil {
		t.Fatalf("Expected no error: %v", err)
	}
}

func TestRunLogs_Success(t *testing.T) {
	old := dockerComposeLogs
	dockerComposeLogs = func(dir, svc string, follow bool) error { return nil }
	defer func() { dockerComposeLogs = old }()
	dir := t.TempDir()
	state.InitState(dir)
	app := state.NewAppConfig()
	app.Name = "logstest"
	state.SaveApp(app)
	if err := RunLogs([]string{"logstest"}); err != nil {
		t.Fatalf("Expected no error: %v", err)
	}
}

func TestRunStatus_WithCaddy(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)
	state.SaveConfig(&state.GlobalConfig{Proxy: "caddy", BaseDomain: "example.com", AcmeEmail: "test@example.com", WebhookPort: 9000})
	output := captureStdout(func() {
		if err := RunStatus(); err != nil {
			t.Errorf("RunStatus failed: %v", err)
		}
	})
	if !strings.Contains(output, "caddy") {
		t.Error("Should mention caddy")
	}
}

func TestRunStatus_LoadError(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)
	state.SaveConfig(&state.GlobalConfig{Proxy: "traefik", BaseDomain: "example.com", AcmeEmail: "test@example.com", WebhookPort: 9000})
	old := stateLoad
	stateLoad = func() (*state.State, error) { return nil, errors.New("fail") }
	defer func() { stateLoad = old }()
	if err := RunStatus(); err == nil {
		t.Error("Expected error")
	}
}

// ------------------------------------------------------------------
// List
// ------------------------------------------------------------------

func TestRunList_LoadError(t *testing.T) {
	old := stateLoad
	stateLoad = func() (*state.State, error) { return nil, errors.New("fail") }
	defer func() { stateLoad = old }()
	if err := RunList(); err == nil {
		t.Error("Expected error")
	}
}

func TestRunList_StoppedStatus(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)
	app := state.NewAppConfig()
	app.Name = "sapp"
	app.Status = "stopped"
	state.SaveApp(app)
	_ = captureStdout(func() { RunList() })
}

func TestRunList_DefaultStatus(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)
	app := state.NewAppConfig()
	app.Name = "dapp"
	app.Status = "unknown"
	state.SaveApp(app)
	_ = captureStdout(func() { RunList() })
}

func TestRunList_WithDBs(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)
	app := state.NewAppConfig()
	app.Name = "dbapp"
	app.Status = "running"
	app.Databases = []string{"mysql"}
	state.SaveApp(app)
	_ = captureStdout(func() { RunList() })
}

// ------------------------------------------------------------------
// Remove
// ------------------------------------------------------------------

func TestRunRemove_Success(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)
	state.SaveConfig(&state.GlobalConfig{Proxy: "traefik", BaseDomain: "test.example.com"})
	app := state.NewAppConfig()
	app.Name = "rmsuccess"
	app.Domain = "rmsuccess.example.com"
	state.SaveApp(app)
	oldRemove := dockerComposeRemove
	dockerComposeRemove = func(appDir string, vols bool) error { return nil }
	defer func() { dockerComposeRemove = oldRemove }()
	setWizardInput(t, "y\n")
	_ = captureStdout(func() {
		if err := RunRemove([]string{"rmsuccess"}); err != nil {
			t.Fatalf("Expected no error: %v", err)
		}
	})
}

func TestRunRemove_StateRemoveError(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)
	state.SaveConfig(&state.GlobalConfig{Proxy: "traefik", BaseDomain: "test.example.com"})
	app := state.NewAppConfig()
	app.Name = "rmstateerr"
	app.Domain = "rmstateerr.example.com"
	state.SaveApp(app)
	oldRemove := dockerComposeRemove
	dockerComposeRemove = func(appDir string, vols bool) error { return nil }
	defer func() { dockerComposeRemove = oldRemove }()
	oldStateRemove := stateRemoveApp
	stateRemoveApp = func(name string) error { return errors.New("fail") }
	defer func() { stateRemoveApp = oldStateRemove }()
	setWizardInput(t, "y\n")
	_ = captureStdout(func() {
		if err := RunRemove([]string{"rmstateerr"}); err == nil {
			t.Error("Expected error")
		}
	})
}

func TestRunRemove_DirRemoveError(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)
	state.SaveConfig(&state.GlobalConfig{Proxy: "traefik", BaseDomain: "test.example.com"})
	app := state.NewAppConfig()
	app.Name = "rmdirerr"
	app.Domain = "rmdirerr.example.com"
	state.SaveApp(app)
	oldRemove := dockerComposeRemove
	dockerComposeRemove = func(appDir string, vols bool) error { return nil }
	defer func() { dockerComposeRemove = oldRemove }()
	oldOsRemove := osRemoveAll
	osRemoveAll = func(path string) error { return errors.New("fail") }
	defer func() { osRemoveAll = oldOsRemove }()
	setWizardInput(t, "y\n")
	_ = captureStdout(func() {
		if err := RunRemove([]string{"rmdirerr"}); err != nil {
			t.Fatalf("Expected no error: %v", err)
		}
	})
}

func TestRunRemove_Caddy(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)
	state.SaveConfig(&state.GlobalConfig{Proxy: "caddy", BaseDomain: "test.example.com"})
	app := state.NewAppConfig()
	app.Name = "rmcaddy"
	app.Domain = "rmcaddy.example.com"
	state.SaveApp(app)
	oldRemove := dockerComposeRemove
	dockerComposeRemove = func(appDir string, vols bool) error { return nil }
	defer func() { dockerComposeRemove = oldRemove }()
	setWizardInput(t, "y\n")
	_ = captureStdout(func() {
		if err := RunRemove([]string{"rmcaddy"}); err != nil {
			t.Fatalf("Expected no error: %v", err)
		}
	})
}

func TestRunRemove_ImageCleanupPanic(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)
	state.SaveConfig(&state.GlobalConfig{Proxy: "traefik", BaseDomain: "test.example.com"})
	app := state.NewAppConfig()
	app.Name = "panicrm"
	app.Domain = "panicrm.example.com"
	state.SaveApp(app)
	oldRemove := dockerComposeRemove
	dockerComposeRemove = func(appDir string, vols bool) error { return nil }
	defer func() { dockerComposeRemove = oldRemove }()
	oldCleanup := dockerCleanupOldImages
	dockerCleanupOldImages = func(appName string, keep int) error { panic("boom") }
	defer func() { dockerCleanupOldImages = oldCleanup }()
	setWizardInput(t, "y\n")
	_ = captureStdout(func() {
		if err := RunRemove([]string{"panicrm"}); err != nil {
			t.Fatalf("Expected no error: %v", err)
		}
	})
	time.Sleep(100 * time.Millisecond)
}

// ------------------------------------------------------------------
// Deploy
// ------------------------------------------------------------------

func TestRunDeploy_MkdirAllError(t *testing.T) {
	repoDir, _ := setupDeployTest(t, map[string]string{"Dockerfile": "FROM alpine\nCMD [\"sleep\", \"60\"]\n"})
	initGitRepo(t, repoDir)
	old := osMkdirAll
	osMkdirAll = func(path string, perm os.FileMode) error { return errors.New("fail") }
	defer func() { osMkdirAll = old }()
	setWizardInput(t, deployInputBasic(repoDir, "7", "mkdirerr", "n"))
	_ = captureStdout(func() {
		if err := RunDeploy(); err == nil {
			t.Error("Expected error")
		}
	})
}

func TestRunDeploy_BuildFailure(t *testing.T) {
	repoDir, _ := setupDeployTest(t, map[string]string{"Dockerfile": "FROM alpine\nCMD [\"sleep\", \"60\"]\n"})
	initGitRepo(t, repoDir)
	oldBuild := dockerBuildImage
	dockerBuildImage = func(dir, name string) (string, error) { return "", errors.New("build failed") }
	defer func() { dockerBuildImage = oldBuild }()
	oldRemoveAll := osRemoveAll
	removed := false
	osRemoveAll = func(path string) error { removed = true; return nil }
	defer func() { osRemoveAll = oldRemoveAll }()
	setWizardInput(t, deployInputBasic(repoDir, "7", "buildfail", "y"))
	_ = captureStdout(func() {
		if err := RunDeploy(); err == nil {
			t.Error("Expected error")
		}
	})
	if !removed {
		t.Error("Should clean up on build failure")
	}
}

func TestRunDeploy_WriteEnvError(t *testing.T) {
	repoDir, _ := setupDeployTest(t, map[string]string{"Dockerfile": "FROM alpine\nCMD [\"sleep\", \"60\"]\n"})
	initGitRepo(t, repoDir)
	oldWrite := osWriteFile
	calls := 0
	osWriteFile = func(name string, data []byte, perm os.FileMode) error {
		calls++
		if calls == 1 {
			return errors.New("fail")
		}
		return nil
	}
	defer func() { osWriteFile = oldWrite }()
	setWizardInput(t, deployInputBasic(repoDir, "7", "envfail", "y"))
	_ = captureStdout(func() {
		if err := RunDeploy(); err == nil {
			t.Error("Expected error")
		}
	})
}

func TestRunDeploy_ComposeWriteError(t *testing.T) {
	repoDir, _ := setupDeployTest(t, map[string]string{"Dockerfile": "FROM alpine\nCMD [\"sleep\", \"60\"]\n"})
	initGitRepo(t, repoDir)
	oldBuild := dockerBuildImage
	dockerBuildImage = func(dir, name string) (string, error) { return "test:v1", nil }
	defer func() { dockerBuildImage = oldBuild }()
	oldComposeWrite := composeWriteCompose
	composeWriteCompose = func(dir, content string) error { return errors.New("fail") }
	defer func() { composeWriteCompose = oldComposeWrite }()
	setWizardInput(t, deployInputBasic(repoDir, "7", "composewriteerr", "y"))
	_ = captureStdout(func() {
		if err := RunDeploy(); err == nil {
			t.Error("Expected error")
		}
	})
}

func TestRunDeploy_ComposeUpError(t *testing.T) {
	repoDir, _ := setupDeployTest(t, map[string]string{"Dockerfile": "FROM alpine\nCMD [\"sleep\", \"60\"]\n"})
	initGitRepo(t, repoDir)
	oldBuild := dockerBuildImage
	dockerBuildImage = func(dir, name string) (string, error) { return "test:v1", nil }
	defer func() { dockerBuildImage = oldBuild }()
	oldComposeWrite := composeWriteCompose
	composeWriteCompose = func(dir, content string) error { return nil }
	defer func() { composeWriteCompose = oldComposeWrite }()
	oldComposeUp := dockerComposeUp
	dockerComposeUp = func(dir string) error { return errors.New("up fail") }
	defer func() { dockerComposeUp = oldComposeUp }()
	oldComposeDown := dockerComposeDown
	dockerComposeDown = func(dir string) error { return nil }
	defer func() { dockerComposeDown = oldComposeDown }()
	setWizardInput(t, deployInputBasic(repoDir, "7", "composeuperr", "y"))
	_ = captureStdout(func() {
		if err := RunDeploy(); err == nil {
			t.Error("Expected error")
		}
	})
}

func TestRunDeploy_ComposeUpRollbackFail(t *testing.T) {
	repoDir, _ := setupDeployTest(t, map[string]string{"Dockerfile": "FROM alpine\nCMD [\"sleep\", \"60\"]\n"})
	initGitRepo(t, repoDir)
	oldBuild := dockerBuildImage
	dockerBuildImage = func(dir, name string) (string, error) { return "test:v1", nil }
	defer func() { dockerBuildImage = oldBuild }()
	oldComposeWrite := composeWriteCompose
	composeWriteCompose = func(dir, content string) error { return nil }
	defer func() { composeWriteCompose = oldComposeWrite }()
	oldComposeUp := dockerComposeUp
	dockerComposeUp = func(dir string) error { return errors.New("up fail") }
	defer func() { dockerComposeUp = oldComposeUp }()
	oldComposeDown := dockerComposeDown
	dockerComposeDown = func(dir string) error { return errors.New("down fail") }
	defer func() { dockerComposeDown = oldComposeDown }()
	setWizardInput(t, deployInputBasic(repoDir, "7", "rollbackfail", "y"))
	_ = captureStdout(func() {
		if err := RunDeploy(); err == nil {
			t.Error("Expected error")
		}
	})
}

func TestRunDeploy_ContainerNotRunning(t *testing.T) {
	repoDir, _ := setupDeployTest(t, map[string]string{"Dockerfile": "FROM alpine\nCMD [\"sleep\", \"60\"]\n"})
	initGitRepo(t, repoDir)
	oldBuild := dockerBuildImage
	dockerBuildImage = func(dir, name string) (string, error) { return "test:v1", nil }
	defer func() { dockerBuildImage = oldBuild }()
	oldComposeWrite := composeWriteCompose
	composeWriteCompose = func(dir, content string) error { return nil }
	defer func() { composeWriteCompose = oldComposeWrite }()
	oldComposeUp := dockerComposeUp
	dockerComposeUp = func(dir string) error { return nil }
	defer func() { dockerComposeUp = oldComposeUp }()
	oldStatus := dockerContainerStatus
	dockerContainerStatus = func(name string) (string, error) { return "exited", nil }
	defer func() { dockerContainerStatus = oldStatus }()
	setWizardInput(t, deployInputBasic(repoDir, "7", "notrunning", "y"))
	_ = captureStdout(func() {
		if err := RunDeploy(); err != nil {
			t.Fatalf("Expected no error: %v", err)
		}
	})
	app, _ := state.GetApp("notrunning")
	if app == nil || app.Status != "error" {
		t.Errorf("Status = %v, want error", app)
	}
}

func TestRunDeploy_DockerfileExists(t *testing.T) {
	repoDir, _ := setupDeployTest(t, map[string]string{"Dockerfile": "FROM alpine\nCMD [\"sleep\", \"60\"]\n"})
	initGitRepo(t, repoDir)
	defer mockDeploySuccess()()
	setWizardInput(t, deployInputBasic(repoDir, "1", "dfile", "y"))
	_ = captureStdout(func() {
		if err := RunDeploy(); err != nil {
			t.Fatalf("Expected no error: %v", err)
		}
	})
}

func TestRunDeploy_SaveAppError(t *testing.T) {
	repoDir, _ := setupDeployTest(t, map[string]string{"Dockerfile": "FROM alpine\nCMD [\"sleep\", \"60\"]\n"})
	initGitRepo(t, repoDir)
	defer mockDeploySuccess()()
	oldSave := stateSaveApp
	stateSaveApp = func(app *state.AppConfig) error { return errors.New("fail") }
	defer func() { stateSaveApp = oldSave }()
	setWizardInput(t, deployInputBasic(repoDir, "7", "saveerr", "y"))
	_ = captureStdout(func() {
		if err := RunDeploy(); err == nil {
			t.Error("Expected error")
		}
	})
}

func TestRunDeploy_CaddyProxy(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)
	cfgpkg.BaseDir = filepath.Join(dir, "opt", "simpledeploy")
	state.SaveConfig(&state.GlobalConfig{Proxy: "caddy", BaseDomain: "test.example.com"})
	repoDir := filepath.Join(dir, "repo")
	os.MkdirAll(repoDir, 0755)
	initGitRepo(t, repoDir)
	os.WriteFile(filepath.Join(repoDir, "Dockerfile"), []byte("FROM alpine\nCMD [\"sleep\", \"60\"]\n"), 0644)
	exec.Command("git", "add", ".").Dir = repoDir
	exec.Command("git", "commit", "-m", "df").Dir = repoDir
	defer mockDeploySuccess()()
	oldAdd := proxyAddCaddyApp
	addCalled := false
	proxyAddCaddyApp = func(name, domain string, port int, headers map[string]string) error { addCalled = true; return nil }
	defer func() { proxyAddCaddyApp = oldAdd }()
	oldReload := proxyReloadCaddy
	proxyReloadCaddy = func() error { return nil }
	defer func() { proxyReloadCaddy = oldReload }()
	setWizardInput(t, deployInputBasic(repoDir, "7", "caddyok", "y"))
	_ = captureStdout(func() {
		if err := RunDeploy(); err != nil {
			t.Fatalf("Expected no error: %v", err)
		}
	})
	if !addCalled {
		t.Error("AddCaddyApp should be called")
	}
}

func TestRunDeploy_CaddyAddError(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)
	cfgpkg.BaseDir = filepath.Join(dir, "opt", "simpledeploy")
	state.SaveConfig(&state.GlobalConfig{Proxy: "caddy", BaseDomain: "test.example.com"})
	repoDir := filepath.Join(dir, "repo")
	os.MkdirAll(repoDir, 0755)
	initGitRepo(t, repoDir)
	os.WriteFile(filepath.Join(repoDir, "Dockerfile"), []byte("FROM alpine\n"), 0644)
	exec.Command("git", "add", ".").Dir = repoDir
	exec.Command("git", "commit", "-m", "df").Dir = repoDir
	defer mockDeploySuccess()()
	oldAdd := proxyAddCaddyApp
	proxyAddCaddyApp = func(name, domain string, port int, headers map[string]string) error { return errors.New("fail") }
	defer func() { proxyAddCaddyApp = oldAdd }()
	oldReload := proxyReloadCaddy
	proxyReloadCaddy = func() error { return nil }
	defer func() { proxyReloadCaddy = oldReload }()
	setWizardInput(t, deployInputBasic(repoDir, "7", "caddyadderr", "y"))
	_ = captureStdout(func() {
		if err := RunDeploy(); err != nil {
			t.Fatalf("Expected no error: %v", err)
		}
	})
}

func TestRunDeploy_CaddyReloadError(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)
	cfgpkg.BaseDir = filepath.Join(dir, "opt", "simpledeploy")
	state.SaveConfig(&state.GlobalConfig{Proxy: "caddy", BaseDomain: "test.example.com"})
	repoDir := filepath.Join(dir, "repo")
	os.MkdirAll(repoDir, 0755)
	initGitRepo(t, repoDir)
	os.WriteFile(filepath.Join(repoDir, "Dockerfile"), []byte("FROM alpine\n"), 0644)
	exec.Command("git", "add", ".").Dir = repoDir
	exec.Command("git", "commit", "-m", "df").Dir = repoDir
	defer mockDeploySuccess()()
	oldAdd := proxyAddCaddyApp
	proxyAddCaddyApp = func(name, domain string, port int, headers map[string]string) error { return nil }
	defer func() { proxyAddCaddyApp = oldAdd }()
	oldReload := proxyReloadCaddy
	proxyReloadCaddy = func() error { return errors.New("fail") }
	defer func() { proxyReloadCaddy = oldReload }()
	setWizardInput(t, deployInputBasic(repoDir, "7", "caddyreloaderr", "y"))
	_ = captureStdout(func() {
		if err := RunDeploy(); err != nil {
			t.Fatalf("Expected no error: %v", err)
		}
	})
}

func TestRunDeploy_NodeApp(t *testing.T) {
	repoDir, _ := setupDeployTest(t, map[string]string{"package.json": "{}\n"})
	initGitRepo(t, repoDir)
	defer mockDeploySuccess()()
	setWizardInput(t, deployInputBasic(repoDir, "1", "nodeapp", "y"))
	_ = captureStdout(func() { RunDeploy() })
	app, _ := state.GetApp("nodeapp")
	if app == nil || app.Type != "node" {
		t.Errorf("Type = %v, want node", app)
	}
}

func TestRunDeploy_GoApp(t *testing.T) {
	repoDir, _ := setupDeployTest(t, map[string]string{"go.mod": "module test\n"})
	initGitRepo(t, repoDir)
	defer mockDeploySuccess()()
	setWizardInput(t, deployInputBasic(repoDir, "2", "goapp", "y"))
	_ = captureStdout(func() { RunDeploy() })
	app, _ := state.GetApp("goapp")
	if app == nil || app.Type != "go" {
		t.Errorf("Type = %v, want go", app)
	}
}

func TestRunDeploy_PHPApp(t *testing.T) {
	repoDir, _ := setupDeployTest(t, map[string]string{"composer.json": "{}\n"})
	initGitRepo(t, repoDir)
	defer mockDeploySuccess()()
	setWizardInput(t, deployInputBasic(repoDir, "3", "phpapp", "y"))
	_ = captureStdout(func() { RunDeploy() })
	app, _ := state.GetApp("phpapp")
	if app == nil || app.Type != "php" {
		t.Errorf("Type = %v, want php", app)
	}
}

func TestRunDeploy_PythonApp(t *testing.T) {
	repoDir, _ := setupDeployTest(t, map[string]string{"requirements.txt": "flask\n"})
	initGitRepo(t, repoDir)
	defer mockDeploySuccess()()
	setWizardInput(t, deployInputBasic(repoDir, "4", "pyapp", "y"))
	_ = captureStdout(func() { RunDeploy() })
	app, _ := state.GetApp("pyapp")
	if app == nil || app.Type != "python" {
		t.Errorf("Type = %v, want python", app)
	}
}

func TestRunDeploy_RubyApp(t *testing.T) {
	repoDir, _ := setupDeployTest(t, map[string]string{"Gemfile": "source 'https://rubygems.org'\n"})
	initGitRepo(t, repoDir)
	defer mockDeploySuccess()()
	setWizardInput(t, deployInputBasic(repoDir, "5", "rubyapp", "y"))
	_ = captureStdout(func() { RunDeploy() })
	app, _ := state.GetApp("rubyapp")
	if app == nil || app.Type != "ruby" {
		t.Errorf("Type = %v, want ruby", app)
	}
}

func TestRunDeploy_StaticApp(t *testing.T) {
	repoDir, _ := setupDeployTest(t, map[string]string{"index.html": "<html></html>\n"})
	initGitRepo(t, repoDir)
	defer mockDeploySuccess()()
	setWizardInput(t, deployInputBasic(repoDir, "6", "staticapp", "y"))
	_ = captureStdout(func() { RunDeploy() })
	app, _ := state.GetApp("staticapp")
	if app == nil || app.Type != "static" {
		t.Errorf("Type = %v, want static", app)
	}
}

func TestRunDeploy_WriteDockerfileError(t *testing.T) {
	repoDir, _ := setupDeployTest(t, map[string]string{"package.json": "{}\n"})
	initGitRepo(t, repoDir)
	old := buildpackWriteDockerfile
	buildpackWriteDockerfile = func(dir, appType string) error { return errors.New("fail") }
	defer func() { buildpackWriteDockerfile = old }()
	setWizardInput(t, deployInputBasic(repoDir, "1", "dockergen", "n"))
	_ = captureStdout(func() {
		if err := RunDeploy(); err == nil {
			t.Error("Expected error")
		}
	})
}

func TestRunDeploy_PortInvalid(t *testing.T) {
	repoDir, _ := setupDeployTest(t, map[string]string{"Dockerfile": "FROM alpine\n"})
	initGitRepo(t, repoDir)
	defer mockDeploySuccess()()
	input := repoDir + "\n\nn\nportinv\n7\nabc\n\nn\n6\nportinv\n\nn\ny\n"
	setWizardInput(t, input)
	_ = captureStdout(func() { RunDeploy() })
	app, _ := state.GetApp("portinv")
	if app == nil || app.Port != 3000 {
		t.Errorf("Port = %v, want 3000", app)
	}
}

func TestRunDeploy_PortOutOfRange(t *testing.T) {
	repoDir, _ := setupDeployTest(t, map[string]string{"Dockerfile": "FROM alpine\n"})
	initGitRepo(t, repoDir)
	defer mockDeploySuccess()()
	input := repoDir + "\n\nn\nportoor\n7\n99999\n\nn\n6\nportoor\n\nn\ny\n"
	setWizardInput(t, input)
	_ = captureStdout(func() { RunDeploy() })
	app, _ := state.GetApp("portoor")
	if app == nil || app.Port != 3000 {
		t.Errorf("Port = %v, want 3000", app)
	}
}

func TestRunDeploy_PrivateRepo(t *testing.T) {
	repoDir, _ := setupDeployTest(t, map[string]string{"Dockerfile": "FROM alpine\n"})
	initGitRepo(t, repoDir)
	defer mockDeploySuccess()()
	oldEnc := stateEncrypt
	stateEncrypt = func(data string) (string, error) { return "enc-" + data, nil }
	defer func() { stateEncrypt = oldEnc }()
	oldDec := stateDecrypt
	stateDecrypt = func(data string) (string, error) { return strings.TrimPrefix(data, "enc-"), nil }
	defer func() { stateDecrypt = oldDec }()
	input := repoDir + "\n\ny\nmytoken\nprivateapp\n7\n3000\n\nn\n6\nprivateapp\n\nn\ny\n"
	setWizardInput(t, input)
	_ = captureStdout(func() { RunDeploy() })
	app, _ := state.GetApp("privateapp")
	if app == nil || app.Name != "privateapp" {
		t.Errorf("Name = %v, want privateapp", app)
	}
}

func TestRunDeploy_EncryptError(t *testing.T) {
	repoDir, _ := setupDeployTest(t, map[string]string{"Dockerfile": "FROM alpine\n"})
	initGitRepo(t, repoDir)
	old := stateEncrypt
	stateEncrypt = func(data string) (string, error) { return "", errors.New("fail") }
	defer func() { stateEncrypt = old }()
	input := repoDir + "\n\ny\ntok\nencerr\n7\n3000\n\nn\n6\nencerr\n\nn\nn\n"
	setWizardInput(t, input)
	_ = captureStdout(func() {
		if err := RunDeploy(); err != nil {
			t.Fatalf("Expected no error: %v", err)
		}
	})
}

func TestRunDeploy_DecryptError(t *testing.T) {
	repoDir, _ := setupDeployTest(t, map[string]string{"Dockerfile": "FROM alpine\n"})
	initGitRepo(t, repoDir)
	oldEnc := stateEncrypt
	stateEncrypt = func(data string) (string, error) { return "enc-" + data, nil }
	defer func() { stateEncrypt = oldEnc }()
	oldDec := stateDecrypt
	stateDecrypt = func(data string) (string, error) { return "", errors.New("fail") }
	defer func() { stateDecrypt = oldDec }()
	input := repoDir + "\n\ny\ntok\ndecerr\n7\n3000\n\nn\n6\ndecerr\n\nn\nn\n"
	setWizardInput(t, input)
	_ = captureStdout(func() {
		if err := RunDeploy(); err != nil {
			t.Fatalf("Expected no error: %v", err)
		}
	})
}

func TestRunDeploy_WriteEnvCustomPathError(t *testing.T) {
	repoDir, _ := setupDeployTest(t, map[string]string{"Dockerfile": "FROM alpine\n"})
	initGitRepo(t, repoDir)
	customEnv := filepath.Join(repoDir, "custom.env")
	os.WriteFile(customEnv, []byte("KEY=VAL\n"), 0644)
	oldWrite := osWriteFile
	osWriteFile = func(name string, data []byte, perm os.FileMode) error {
		return errors.New("fail")
	}
	defer func() { osWriteFile = oldWrite }()
	input := repoDir + "\n\nn\nenvwriteerr\n7\n3000\n\ny\n" + customEnv + "\n6\nenvwriteerr\n\nn\nn\n"
	setWizardInput(t, input)
	_ = captureStdout(func() {
		if err := RunDeploy(); err != nil {
			t.Fatalf("Expected no error: %v", err)
		}
	})
}

func TestRunDeploy_MalformedEnvVar(t *testing.T) {
	repoDir, _ := setupDeployTest(t, map[string]string{"Dockerfile": "FROM alpine\n"})
	initGitRepo(t, repoDir)
	defer mockDeploySuccess()()
	input := repoDir + "\n\nn\nmalform\n7\n3000\nbadvar\nKEY=VALUE\n\nn\n6\nmalform\n\nn\ny\n"
	setWizardInput(t, input)
	_ = captureStdout(func() { RunDeploy() })
}

func TestRunDeploy_DBProvisionError(t *testing.T) {
	repoDir, _ := setupDeployTest(t, map[string]string{"Dockerfile": "FROM alpine\n"})
	initGitRepo(t, repoDir)
	old := dbProvisionDatabases
	dbProvisionDatabases = func(appName string, selectedDBs []string) (map[string]string, []string, map[string]string, error) {
		return nil, nil, nil, errors.New("fail")
	}
	defer func() { dbProvisionDatabases = old }()
	input := repoDir + "\n\nn\ndbfail\n7\n3000\n\nn\n1\ndbfail\n\nn\nn\n"
	setWizardInput(t, input)
	_ = captureStdout(func() {
		if err := RunDeploy(); err == nil {
			t.Error("Expected error")
		}
	})
}

func TestRunDeploy_DBWithEnvVars(t *testing.T) {
	repoDir, _ := setupDeployTest(t, map[string]string{"Dockerfile": "FROM alpine\n"})
	initGitRepo(t, repoDir)
	defer mockDeploySuccess()()
	input := repoDir + "\n\nn\ndbenv\n7\n3000\n\nn\n1\ndbenv\n\nn\ny\n"
	setWizardInput(t, input)
	_ = captureStdout(func() { RunDeploy() })
	app, _ := state.GetApp("dbenv")
	if app == nil || len(app.Databases) == 0 {
		t.Error("Expected db saved")
	}
}

func TestRunDeploy_DBCredEncryptError(t *testing.T) {
	repoDir, _ := setupDeployTest(t, map[string]string{"Dockerfile": "FROM alpine\n"})
	initGitRepo(t, repoDir)
	defer mockDeploySuccess()()
	old := stateEncrypt
	stateEncrypt = func(data string) (string, error) { return "", errors.New("fail") }
	defer func() { stateEncrypt = old }()
	input := repoDir + "\n\nn\ndbencerr\n7\n3000\n\nn\n1\ndbencerr\n\nn\ny\n"
	setWizardInput(t, input)
	_ = captureStdout(func() { RunDeploy() })
}

func TestRunDeploy_MalformedHeader(t *testing.T) {
	repoDir, _ := setupDeployTest(t, map[string]string{"Dockerfile": "FROM alpine\n"})
	initGitRepo(t, repoDir)
	defer mockDeploySuccess()()
	input := repoDir + "\n\nn\nhdrbad\n7\n3000\n\nn\n6\nhdrbad\nbadheader\nX-Custom: value\n\nn\ny\n"
	setWizardInput(t, input)
	_ = captureStdout(func() { RunDeploy() })
}

func TestRunDeploy_WithDatabaseSummary(t *testing.T) {
	repoDir, _ := setupDeployTest(t, map[string]string{"Dockerfile": "FROM alpine\n"})
	initGitRepo(t, repoDir)
	defer mockDeploySuccess()()
	input := repoDir + "\n\nn\ndbsum\n7\n3000\n\nn\n1\ndbsum\n\nn\ny\n"
	setWizardInput(t, input)
	_ = captureStdout(func() { RunDeploy() })
}

func TestRunDeploy_CaddyPostDeployErrors(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)
	cfgpkg.BaseDir = filepath.Join(dir, "opt", "simpledeploy")
	state.SaveConfig(&state.GlobalConfig{Proxy: "caddy", BaseDomain: "test.example.com"})
	repoDir := filepath.Join(dir, "repo")
	os.MkdirAll(repoDir, 0755)
	initGitRepo(t, repoDir)
	os.WriteFile(filepath.Join(repoDir, "Dockerfile"), []byte("FROM alpine\n"), 0644)
	exec.Command("git", "add", ".").Dir = repoDir
	exec.Command("git", "commit", "-m", "df").Dir = repoDir
	defer mockDeploySuccess()()
	oldAdd := proxyAddCaddyApp
	proxyAddCaddyApp = func(name, domain string, port int, headers map[string]string) error { return errors.New("add fail") }
	defer func() { proxyAddCaddyApp = oldAdd }()
	oldReload := proxyReloadCaddy
	proxyReloadCaddy = func() error { return errors.New("reload fail") }
	defer func() { proxyReloadCaddy = oldReload }()
	input := repoDir + "\n\nn\ncaddyerr\n7\n3000\n\nn\n6\ncaddyerr\n\nn\ny\n"
	setWizardInput(t, input)
	_ = captureStdout(func() { RunDeploy() })
}

func TestRunDeploy_EnvFile(t *testing.T) {
	repoDir, _ := setupDeployTest(t, map[string]string{"Dockerfile": "FROM alpine\n"})
	initGitRepo(t, repoDir)
	customEnv := filepath.Join(repoDir, "custom.env")
	os.WriteFile(customEnv, []byte("KEY=VALUE\n"), 0644)
	input := repoDir + "\n\nn\nenvapp\n7\n3000\n\ny\n" + customEnv + "\n6\nenvapp\n\nn\nn\n"
	setWizardInput(t, input)
	_ = captureStdout(func() {
		if err := RunDeploy(); err != nil {
			t.Fatalf("Expected no error: %v", err)
		}
	})
}

func TestRunDeploy_EnvFileReadError(t *testing.T) {
	repoDir, _ := setupDeployTest(t, map[string]string{"Dockerfile": "FROM alpine\n"})
	initGitRepo(t, repoDir)
	input := repoDir + "\n\nn\nenvapp\n7\n3000\n\ny\n/nonexistent/env\n6\nenvapp\n\nn\nn\n"
	setWizardInput(t, input)
	_ = captureStdout(func() {
		if err := RunDeploy(); err != nil {
			t.Fatalf("Expected no error: %v", err)
		}
	})
}

func TestRunDeploy_WebhookEnabled(t *testing.T) {
	repoDir, _ := setupDeployTest(t, map[string]string{"Dockerfile": "FROM alpine\n"})
	initGitRepo(t, repoDir)
	defer mockDeploySuccess()()
	input := repoDir + "\n\nn\nhookapp\n7\n3000\n\nn\n6\nhookapp\n\ny\ny\n"
	setWizardInput(t, input)
	_ = captureStdout(func() { RunDeploy() })
	app, _ := state.GetApp("hookapp")
	if app == nil || !app.WebhookEnabled {
		t.Error("Webhook should be enabled")
	}
}

func TestRunDeploy_Success(t *testing.T) {
	repoDir, _ := setupDeployTest(t, map[string]string{"Dockerfile": "FROM alpine\nCMD [\"sleep\", \"60\"]\n"})
	initGitRepo(t, repoDir)
	defer mockDeploySuccess()()
	setWizardInput(t, deployInputBasic(repoDir, "7", "fullapp", "y"))
	_ = captureStdout(func() {
		if err := RunDeploy(); err != nil {
			t.Fatalf("Expected no error: %v", err)
		}
	})
	app, _ := state.GetApp("fullapp")
	if app.Status != "running" {
		t.Errorf("Status = %q, want running", app.Status)
	}
	if app.CurrentImage != "test:v1" {
		t.Errorf("Image = %q, want test:v1", app.CurrentImage)
	}
}

// ------------------------------------------------------------------
// Redeploy
// ------------------------------------------------------------------

func TestRunRedeploy_Success(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)
	cfgpkg.BaseDir = filepath.Join(dir, "opt", "simpledeploy")
	state.SaveConfig(&state.GlobalConfig{Proxy: "traefik", BaseDomain: "test.example.com"})
	app := state.NewAppConfig()
	app.Name = "redeployok"
	app.Branch = "main"
	app.Repo = "https://github.com/test/app.git"
	app.Domain = "redeployok.example.com"
	app.Port = 3000
	app.Type = "node"
	state.SaveApp(app)

	sourceDir := filepath.Join(cfgpkg.AppDir("redeployok"), "source")
	os.MkdirAll(sourceDir, 0755)
	exec.Command("git", "init", "-b", "main").Dir = sourceDir
	exec.Command("git", "config", "user.email", "test@test.com").Dir = sourceDir
	exec.Command("git", "config", "user.name", "Test").Dir = sourceDir
	os.WriteFile(filepath.Join(sourceDir, "file.txt"), []byte("v1"), 0644)
	exec.Command("git", "add", ".").Dir = sourceDir
	exec.Command("git", "commit", "-m", "initial").Dir = sourceDir

	appDir := cfgpkg.AppDir("redeployok")
	os.WriteFile(filepath.Join(appDir, "docker-compose.yml"), []byte("services:\n  redeployok:\n    image: old\n"), 0644)

	oldPull := gitPull
	gitPull = func(dir, branch string, token ...string) error { return nil }
	defer func() { gitPull = oldPull }()
	oldBuild := dockerBuildImage
	dockerBuildImage = func(dir, name string) (string, error) { return "redeployok:v2", nil }
	defer func() { dockerBuildImage = oldBuild }()
	oldComposeUp := dockerComposeUp
	dockerComposeUp = func(dir string) error { return nil }
	defer func() { dockerComposeUp = oldComposeUp }()

	_ = captureStdout(func() {
		if err := RunRedeploy([]string{"redeployok"}); err != nil {
			t.Fatalf("Expected no error: %v", err)
		}
	})
}

func TestRunRedeploy_GitTokenDecryptFail(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)
	cfgpkg.BaseDir = filepath.Join(dir, "opt", "simpledeploy")
	state.SaveConfig(&state.GlobalConfig{Proxy: "traefik", BaseDomain: "test.example.com"})
	app := state.NewAppConfig()
	app.Name = "tokendeploy"
	app.Branch = "main"
	app.Repo = "https://github.com/test/app.git"
	app.Domain = "tokendeploy.example.com"
	app.Port = 3000
	app.Type = "node"
	app.GitToken = "invalid-encrypted-token"
	state.SaveApp(app)

	sourceDir := filepath.Join(cfgpkg.AppDir("tokendeploy"), "source")
	os.MkdirAll(sourceDir, 0755)
	exec.Command("git", "init", "-b", "main").Dir = sourceDir
	exec.Command("git", "config", "user.email", "test@test.com").Dir = sourceDir
	exec.Command("git", "config", "user.name", "Test").Dir = sourceDir
	os.WriteFile(filepath.Join(sourceDir, "file.txt"), []byte("v1"), 0644)
	exec.Command("git", "add", ".").Dir = sourceDir
	exec.Command("git", "commit", "-m", "initial").Dir = sourceDir

	appDir := cfgpkg.AppDir("tokendeploy")
	os.WriteFile(filepath.Join(appDir, "docker-compose.yml"), []byte("services:\n  tokendeploy:\n    image: old\n"), 0644)

	oldPull := gitPull
	gitPull = func(dir, branch string, token ...string) error { return nil }
	defer func() { gitPull = oldPull }()
	oldBuild := dockerBuildImage
	dockerBuildImage = func(dir, name string) (string, error) { return "tokendeploy:v2", nil }
	defer func() { dockerBuildImage = oldBuild }()
	oldComposeUp := dockerComposeUp
	dockerComposeUp = func(dir string) error { return nil }
	defer func() { dockerComposeUp = oldComposeUp }()

	_ = captureStdout(func() {
		if err := RunRedeploy([]string{"tokendeploy"}); err != nil {
			t.Fatalf("Expected no error: %v", err)
		}
	})
}

func TestRunRedeploy_BuildFail(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)
	cfgpkg.BaseDir = filepath.Join(dir, "opt", "simpledeploy")
	state.SaveConfig(&state.GlobalConfig{Proxy: "traefik", BaseDomain: "test.example.com"})
	app := state.NewAppConfig()
	app.Name = "buildfail"
	app.Branch = "main"
	app.Repo = "https://github.com/test/app.git"
	app.Domain = "buildfail.example.com"
	app.Port = 3000
	app.Type = "node"
	state.SaveApp(app)

	sourceDir := filepath.Join(cfgpkg.AppDir("buildfail"), "source")
	os.MkdirAll(sourceDir, 0755)
	exec.Command("git", "init", "-b", "main").Dir = sourceDir
	exec.Command("git", "config", "user.email", "test@test.com").Dir = sourceDir
	exec.Command("git", "config", "user.name", "Test").Dir = sourceDir
	os.WriteFile(filepath.Join(sourceDir, "file.txt"), []byte("v1"), 0644)
	exec.Command("git", "add", ".").Dir = sourceDir
	exec.Command("git", "commit", "-m", "initial").Dir = sourceDir

	appDir := cfgpkg.AppDir("buildfail")
	os.WriteFile(filepath.Join(appDir, "docker-compose.yml"), []byte("services:\n  buildfail:\n    image: old\n"), 0644)

	oldPull := gitPull
	gitPull = func(dir, branch string, token ...string) error { return nil }
	defer func() { gitPull = oldPull }()
	oldBuild := dockerBuildImage
	dockerBuildImage = func(dir, name string) (string, error) { return "", errors.New("build fail") }
	defer func() { dockerBuildImage = oldBuild }()

	_ = captureStdout(func() {
		if err := RunRedeploy([]string{"buildfail"}); err == nil {
			t.Error("Expected error")
		}
	})
}

func TestRunRedeploy_ComposeWriteFail(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)
	cfgpkg.BaseDir = filepath.Join(dir, "opt", "simpledeploy")
	state.SaveConfig(&state.GlobalConfig{Proxy: "traefik", BaseDomain: "test.example.com"})
	app := state.NewAppConfig()
	app.Name = "composewritefail"
	app.Branch = "main"
	app.Repo = "https://github.com/test/app.git"
	app.Domain = "composewritefail.example.com"
	app.Port = 3000
	app.Type = "node"
	state.SaveApp(app)

	sourceDir := filepath.Join(cfgpkg.AppDir("composewritefail"), "source")
	os.MkdirAll(sourceDir, 0755)
	exec.Command("git", "init", "-b", "main").Dir = sourceDir
	exec.Command("git", "config", "user.email", "test@test.com").Dir = sourceDir
	exec.Command("git", "config", "user.name", "Test").Dir = sourceDir
	os.WriteFile(filepath.Join(sourceDir, "file.txt"), []byte("v1"), 0644)
	exec.Command("git", "add", ".").Dir = sourceDir
	exec.Command("git", "commit", "-m", "initial").Dir = sourceDir

	appDir := cfgpkg.AppDir("composewritefail")
	os.WriteFile(filepath.Join(appDir, "docker-compose.yml"), []byte("services:\n  composewritefail:\n    image: old\n"), 0644)

	oldPull := gitPull
	gitPull = func(dir, branch string, token ...string) error { return nil }
	defer func() { gitPull = oldPull }()
	oldBuild := dockerBuildImage
	dockerBuildImage = func(dir, name string) (string, error) { return "cf:v2", nil }
	defer func() { dockerBuildImage = oldBuild }()
	oldWrite := osWriteFile
	osWriteFile = func(name string, data []byte, perm os.FileMode) error { return errors.New("fail") }
	defer func() { osWriteFile = oldWrite }()

	_ = captureStdout(func() {
		if err := RunRedeploy([]string{"composewritefail"}); err == nil {
			t.Error("Expected error")
		}
	})
}

func TestRunRedeploy_ComposeUpFail(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)
	cfgpkg.BaseDir = filepath.Join(dir, "opt", "simpledeploy")
	state.SaveConfig(&state.GlobalConfig{Proxy: "traefik", BaseDomain: "test.example.com"})
	app := state.NewAppConfig()
	app.Name = "composeupfail"
	app.Branch = "main"
	app.Repo = "https://github.com/test/app.git"
	app.Domain = "composeupfail.example.com"
	app.Port = 3000
	app.Type = "node"
	state.SaveApp(app)

	sourceDir := filepath.Join(cfgpkg.AppDir("composeupfail"), "source")
	os.MkdirAll(sourceDir, 0755)
	exec.Command("git", "init", "-b", "main").Dir = sourceDir
	exec.Command("git", "config", "user.email", "test@test.com").Dir = sourceDir
	exec.Command("git", "config", "user.name", "Test").Dir = sourceDir
	os.WriteFile(filepath.Join(sourceDir, "file.txt"), []byte("v1"), 0644)
	exec.Command("git", "add", ".").Dir = sourceDir
	exec.Command("git", "commit", "-m", "initial").Dir = sourceDir

	appDir := cfgpkg.AppDir("composeupfail")
	os.WriteFile(filepath.Join(appDir, "docker-compose.yml"), []byte("services:\n  composeupfail:\n    image: old\n"), 0644)

	oldPull := gitPull
	gitPull = func(dir, branch string, token ...string) error { return nil }
	defer func() { gitPull = oldPull }()
	oldBuild := dockerBuildImage
	dockerBuildImage = func(dir, name string) (string, error) { return "cuf:v2", nil }
	defer func() { dockerBuildImage = oldBuild }()
	oldComposeUp := dockerComposeUp
	dockerComposeUp = func(dir string) error { return errors.New("up fail") }
	defer func() { dockerComposeUp = oldComposeUp }()

	_ = captureStdout(func() {
		if err := RunRedeploy([]string{"composeupfail"}); err == nil {
			t.Error("Expected error")
		}
	})
}

func TestRunRedeploy_ReadComposeFail(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)
	cfgpkg.BaseDir = filepath.Join(dir, "opt", "simpledeploy")
	state.SaveConfig(&state.GlobalConfig{Proxy: "traefik", BaseDomain: "test.example.com"})
	app := state.NewAppConfig()
	app.Name = "noread"
	app.Branch = "main"
	app.Repo = "https://github.com/test/app.git"
	app.Domain = "noread.example.com"
	app.Port = 3000
	app.Type = "node"
	state.SaveApp(app)

	sourceDir := filepath.Join(cfgpkg.AppDir("noread"), "source")
	os.MkdirAll(sourceDir, 0755)
	exec.Command("git", "init", "-b", "main").Dir = sourceDir
	exec.Command("git", "config", "user.email", "test@test.com").Dir = sourceDir
	exec.Command("git", "config", "user.name", "Test").Dir = sourceDir
	os.WriteFile(filepath.Join(sourceDir, "file.txt"), []byte("v1"), 0644)
	exec.Command("git", "add", ".").Dir = sourceDir
	exec.Command("git", "commit", "-m", "initial").Dir = sourceDir

	oldPull := gitPull
	gitPull = func(dir, branch string, token ...string) error { return nil }
	defer func() { gitPull = oldPull }()
	oldBuild := dockerBuildImage
	dockerBuildImage = func(dir, name string) (string, error) { return "nr:v2", nil }
	defer func() { dockerBuildImage = oldBuild }()

	_ = captureStdout(func() {
		if err := RunRedeploy([]string{"noread"}); err == nil {
			t.Error("Expected error")
		}
	})
}

func TestRunRedeploy_ImageCleanupPanic(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)
	cfgpkg.BaseDir = filepath.Join(dir, "opt", "simpledeploy")
	state.SaveConfig(&state.GlobalConfig{Proxy: "traefik", BaseDomain: "test.example.com"})
	app := state.NewAppConfig()
	app.Name = "panicre"
	app.Branch = "main"
	app.Repo = "https://github.com/test/app.git"
	app.Domain = "panicre.example.com"
	app.Port = 3000
	app.Type = "node"
	state.SaveApp(app)

	sourceDir := filepath.Join(cfgpkg.AppDir("panicre"), "source")
	os.MkdirAll(sourceDir, 0755)
	exec.Command("git", "init", "-b", "main").Dir = sourceDir
	exec.Command("git", "config", "user.email", "test@test.com").Dir = sourceDir
	exec.Command("git", "config", "user.name", "Test").Dir = sourceDir
	os.WriteFile(filepath.Join(sourceDir, "file.txt"), []byte("v1"), 0644)
	exec.Command("git", "add", ".").Dir = sourceDir
	exec.Command("git", "commit", "-m", "initial").Dir = sourceDir

	appDir := cfgpkg.AppDir("panicre")
	os.WriteFile(filepath.Join(appDir, "docker-compose.yml"), []byte("services:\n  panicre:\n    image: old\n"), 0644)

	oldPull := gitPull
	gitPull = func(dir, branch string, token ...string) error { return nil }
	defer func() { gitPull = oldPull }()
	oldBuild := dockerBuildImage
	dockerBuildImage = func(dir, name string) (string, error) { return "pr:v2", nil }
	defer func() { dockerBuildImage = oldBuild }()
	oldComposeUp := dockerComposeUp
	dockerComposeUp = func(dir string) error { return nil }
	defer func() { dockerComposeUp = oldComposeUp }()
	oldCleanup := dockerCleanupOldImages
	dockerCleanupOldImages = func(appName string, keep int) error { panic("boom") }
	defer func() { dockerCleanupOldImages = oldCleanup }()

	_ = captureStdout(func() {
		if err := RunRedeploy([]string{"panicre"}); err != nil {
			t.Fatalf("Expected no error: %v", err)
		}
	})
	time.Sleep(100 * time.Millisecond)
}

func TestRunRedeploy_CaddyReloadError(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)
	cfgpkg.BaseDir = filepath.Join(dir, "opt", "simpledeploy")
	state.SaveConfig(&state.GlobalConfig{Proxy: "caddy", BaseDomain: "test.example.com"})
	app := state.NewAppConfig()
	app.Name = "caddyreloaderr"
	app.Branch = "main"
	app.Repo = "https://github.com/test/app.git"
	app.Domain = "caddyreloaderr.example.com"
	app.Port = 3000
	app.Type = "node"
	state.SaveApp(app)

	sourceDir := filepath.Join(cfgpkg.AppDir("caddyreloaderr"), "source")
	os.MkdirAll(sourceDir, 0755)
	exec.Command("git", "init", "-b", "main").Dir = sourceDir
	exec.Command("git", "config", "user.email", "test@test.com").Dir = sourceDir
	exec.Command("git", "config", "user.name", "Test").Dir = sourceDir
	os.WriteFile(filepath.Join(sourceDir, "file.txt"), []byte("v1"), 0644)
	exec.Command("git", "add", ".").Dir = sourceDir
	exec.Command("git", "commit", "-m", "initial").Dir = sourceDir

	appDir := cfgpkg.AppDir("caddyreloaderr")
	os.WriteFile(filepath.Join(appDir, "docker-compose.yml"), []byte("services:\n  caddyreloaderr:\n    image: old\n"), 0644)

	oldPull := gitPull
	gitPull = func(dir, branch string, token ...string) error { return nil }
	defer func() { gitPull = oldPull }()
	oldBuild := dockerBuildImage
	dockerBuildImage = func(dir, name string) (string, error) { return "cre:v2", nil }
	defer func() { dockerBuildImage = oldBuild }()
	oldComposeUp := dockerComposeUp
	dockerComposeUp = func(dir string) error { return nil }
	defer func() { dockerComposeUp = oldComposeUp }()
	oldReload := proxyReloadCaddy
	proxyReloadCaddy = func() error { return errors.New("reload fail") }
	defer func() { proxyReloadCaddy = oldReload }()

	_ = captureStdout(func() {
		if err := RunRedeploy([]string{"caddyreloaderr"}); err != nil {
			t.Fatalf("Expected no error: %v", err)
		}
	})
}

func TestRunRedeploy_SaveAppError(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)
	cfgpkg.BaseDir = filepath.Join(dir, "opt", "simpledeploy")
	state.SaveConfig(&state.GlobalConfig{Proxy: "traefik", BaseDomain: "test.example.com"})
	app := state.NewAppConfig()
	app.Name = "savefail"
	app.Branch = "main"
	app.Repo = "https://github.com/test/app.git"
	app.Domain = "savefail.example.com"
	app.Port = 3000
	app.Type = "node"
	state.SaveApp(app)

	sourceDir := filepath.Join(cfgpkg.AppDir("savefail"), "source")
	os.MkdirAll(sourceDir, 0755)
	exec.Command("git", "init", "-b", "main").Dir = sourceDir
	exec.Command("git", "config", "user.email", "test@test.com").Dir = sourceDir
	exec.Command("git", "config", "user.name", "Test").Dir = sourceDir
	os.WriteFile(filepath.Join(sourceDir, "file.txt"), []byte("v1"), 0644)
	exec.Command("git", "add", ".").Dir = sourceDir
	exec.Command("git", "commit", "-m", "initial").Dir = sourceDir

	appDir := cfgpkg.AppDir("savefail")
	os.WriteFile(filepath.Join(appDir, "docker-compose.yml"), []byte("services:\n  savefail:\n    image: old\n"), 0644)

	oldPull := gitPull
	gitPull = func(dir, branch string, token ...string) error { return nil }
	defer func() { gitPull = oldPull }()
	oldBuild := dockerBuildImage
	dockerBuildImage = func(dir, name string) (string, error) { return "sf:v2", nil }
	defer func() { dockerBuildImage = oldBuild }()
	oldComposeUp := dockerComposeUp
	dockerComposeUp = func(dir string) error { return nil }
	defer func() { dockerComposeUp = oldComposeUp }()
	oldSave := stateSaveApp
	stateSaveApp = func(app *state.AppConfig) error { return errors.New("save fail") }
	defer func() { stateSaveApp = oldSave }()

	_ = captureStdout(func() {
		if err := RunRedeploy([]string{"savefail"}); err == nil {
			t.Error("Expected error")
		}
	})
}

// ------------------------------------------------------------------
// Webhook
// ------------------------------------------------------------------

type mockWebhookServer struct {
	handler func(appName string) error
}

func (m *mockWebhookServer) SetDeployHandler(h func(appName string) error) {
	m.handler = h
}
func (m *mockWebhookServer) Start() error {
	if m.handler != nil {
		_ = m.handler("webhookapp")
	}
	return nil
}

func TestRunWebhook_Callback(t *testing.T) {
	old := webhookNewServer
	webhookNewServer = func(port int, secret string) webhookServer {
		return &mockWebhookServer{}
	}
	defer func() { webhookNewServer = old }()

	dir := t.TempDir()
	state.InitState(dir)
	state.SaveConfig(&state.GlobalConfig{WebhookPort: 9000, WebhookSecret: "secret"})

	app := state.NewAppConfig()
	app.Name = "webhookapp"
	app.Branch = "main"
	app.Repo = "https://github.com/test/app.git"
	state.SaveApp(app)

	_ = captureStdout(func() {
		if err := RunWebhook([]string{"start"}); err != nil {
			t.Fatalf("Expected no error: %v", err)
		}
	})
}
