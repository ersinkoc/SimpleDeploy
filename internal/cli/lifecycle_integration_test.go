package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	cfgpkg "github.com/ersinkoc/SimpleDeploy/internal/config"
	"github.com/ersinkoc/SimpleDeploy/internal/docker"
	"github.com/ersinkoc/SimpleDeploy/internal/state"
)

// TestLifecycle_EveryCommandAgainstRealDocker walks a deployment through every
// command SimpleDeploy exposes for an app — deploy, list, status, logs, exec,
// stop, restart, redeploy, remove — against a real daemon, asserting the
// OBSERVABLE effect of each one rather than that it returned nil.
//
// The unit suite drives these commands through mocks, so it can only prove that
// the code calls what it means to call. This proves the calls do what the code
// believes: the container really stops, really comes back, state really matches
// the daemon, and `remove` really leaves nothing behind.
//
//	SIMPLEDEPLOY_INTEGRATION=1 go test -p=1 -count=1 -run TestLifecycle ./internal/cli/
func TestLifecycle_EveryCommandAgainstRealDocker(t *testing.T) {
	requireCompose(t)

	const appName = "lifeprobe"
	root := t.TempDir()
	oldBase := cfgpkg.BaseDir
	t.Cleanup(func() { cfgpkg.BaseDir = oldBase })
	cfgpkg.BaseDir = filepath.Join(root, "opt", "simpledeploy")
	state.InitState(filepath.Join(root, "state"))

	if err := state.SaveConfig(&state.GlobalConfig{
		BaseDomain: "life.example.com", Proxy: "traefik",
		AcmeEmail: "admin@example.com", WebhookPort: 9000, WebhookSecret: "whk_life",
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	_ = exec.Command("docker", "network", "create", "simpledeploy").Run()

	oldStable := containerStableFor
	containerStableFor = 2 * time.Second
	t.Cleanup(func() { containerStableFor = oldStable })

	repoDir := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	writeServingApp(t, repoDir, "LIFE_V1")
	installCopyingGit(t, repoDir)

	appDir := cfgpkg.AppDir(appName)
	containerName := docker.ContainerName(appName)
	t.Cleanup(func() {
		down := exec.Command("docker", "compose", "down", "-v")
		down.Dir = appDir
		_ = down.Run()
		_ = exec.Command("docker", "rm", "-f", containerName).Run()
		out, _ := exec.Command("docker", "images", "--format", "{{.Repository}}:{{.Tag}}", appName).Output()
		for _, tag := range strings.Fields(string(out)) {
			_ = exec.Command("docker", "rmi", "-f", tag).Run()
		}
	})

	ctx := context.Background()

	// ---- deploy -----------------------------------------------------------
	setWizardInput(t, strings.Join([]string{
		"https://example.com/life/probe.git", "", "n", appName, "7", "3000",
		"", "n", "6", appName, "", "n", "y",
	}, "\n")+"\n")
	deployOut := captureStdout(func() {
		if err := RunDeploy(); err != nil {
			t.Errorf("RunDeploy: %v", err)
		}
	})
	if t.Failed() {
		t.Fatalf("deploy output:\n%s", deployOut)
	}

	app, err := state.GetApp(appName)
	if err != nil {
		t.Fatalf("app missing from state: %v", err)
	}
	if app.Status != "running" {
		t.Fatalf("Status = %q, want running", app.Status)
	}
	firstImage := app.CurrentImage
	assertServes(t, containerName, "LIFE_V1")

	// The files a deploy is responsible for, with the modes they are
	// documented to have: both embed secrets (the .env holds generated database
	// credentials, the compose file repeats them in db environment blocks).
	for _, name := range []string{"docker-compose.yml", ".env"} {
		info, err := os.Stat(filepath.Join(appDir, name))
		if err != nil {
			t.Errorf("deploy should have written %s: %v", name, err)
			continue
		}
		if runtime.GOOS != "windows" {
			if perm := info.Mode().Perm(); perm != 0600 {
				t.Errorf("%s mode = %o, want 0600", name, perm)
			}
		}
	}

	// The container must NOT publish a host port: a published port bypasses the
	// proxy's TLS and security headers and exposes the app on the public IP.
	ports, err := exec.Command("docker", "inspect", "-f", "{{json .NetworkSettings.Ports}}", containerName).Output()
	if err != nil {
		t.Fatalf("docker inspect ports: %v", err)
	}
	if strings.Contains(string(ports), "HostPort") {
		t.Errorf("container publishes a host port, which bypasses the proxy: %s", ports)
	}

	// ---- list / status ----------------------------------------------------
	listOut := captureStdout(func() {
		if err := RunList(nil); err != nil {
			t.Errorf("RunList: %v", err)
		}
	})
	if !strings.Contains(listOut, appName) || !strings.Contains(listOut, app.Domain) {
		t.Errorf("list output should describe the app:\n%s", listOut)
	}

	statusOut := captureStdout(func() {
		if err := RunStatus(nil); err != nil {
			t.Errorf("RunStatus: %v", err)
		}
	})
	// status reads the live daemon, so it must agree with it.
	if !strings.Contains(statusOut, appName) || !strings.Contains(statusOut, "running") {
		t.Errorf("status should report the app as running:\n%s", statusOut)
	}

	// ---- logs / exec ------------------------------------------------------
	// logs must be one-shot: following would block forever here, which is the
	// regression that made `simpledeploy logs` unusable from a script.
	logsDone := make(chan error, 1)
	go func() { logsDone <- RunLogs([]string{appName}) }()
	select {
	case err := <-logsDone:
		if err != nil {
			t.Errorf("RunLogs: %v", err)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("RunLogs did not return; it must not follow unless -f is given")
	}

	if err := RunExec([]string{appName, "sh", "-c", "exit 0"}); err != nil {
		t.Errorf("RunExec: %v", err)
	}
	// A failing command inside the container must surface as an error.
	if err := RunExec([]string{appName, "sh", "-c", "exit 3"}); err == nil {
		t.Error("RunExec should report a non-zero exit from the container")
	}

	// ---- stop -------------------------------------------------------------
	captureStdout(func() {
		if err := RunStop([]string{appName}); err != nil {
			t.Errorf("RunStop: %v", err)
		}
	})
	waitFor(t, 60*time.Second, "container to stop", func() bool {
		s, _ := docker.ContainerStatus(ctx, containerName)
		return s == "exited"
	})
	if app, _ = state.GetApp(appName); app.Status != "stopped" {
		t.Errorf("Status = %q after stop, want stopped", app.Status)
	}

	// ---- restart ----------------------------------------------------------
	captureStdout(func() {
		if err := RunRestart([]string{appName}); err != nil {
			t.Errorf("RunRestart: %v", err)
		}
	})
	if app, _ = state.GetApp(appName); app.Status != "running" {
		t.Errorf("Status = %q after restart, want running", app.Status)
	}
	assertServes(t, containerName, "LIFE_V1")

	// ---- redeploy ---------------------------------------------------------
	writeServingApp(t, repoDir, "LIFE_V2")
	captureStdout(func() {
		if err := RunRedeploy([]string{appName}); err != nil {
			t.Errorf("RunRedeploy: %v", err)
		}
	})
	app, err = state.GetApp(appName)
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if app.CurrentImage == firstImage {
		t.Error("redeploy did not produce a new image")
	}
	if app.DeployCount != 2 {
		t.Errorf("DeployCount = %d, want 2", app.DeployCount)
	}
	assertServes(t, containerName, "LIFE_V2")

	// ---- remove -----------------------------------------------------------
	setWizardInput(t, "y\n")
	captureStdout(func() {
		if err := RunRemove([]string{appName}); err != nil {
			t.Errorf("RunRemove: %v", err)
		}
	})

	if _, err := state.GetApp(appName); err == nil {
		t.Error("app still present in state after remove")
	}
	if _, err := os.Stat(appDir); !os.IsNotExist(err) {
		t.Errorf("app directory should be gone after remove, stat err = %v", err)
	}
	waitFor(t, 60*time.Second, "container to disappear", func() bool {
		s, _ := docker.ContainerStatus(ctx, containerName)
		return s == "not found"
	})
	// remove prunes every image for the app (pruneImages(name, 0)).
	imgs, _ := exec.Command("docker", "images", "--format", "{{.Repository}}:{{.Tag}}", appName).Output()
	if left := strings.Fields(string(imgs)); len(left) != 0 {
		t.Errorf("remove left images behind: %v", left)
	}
	// And the per-app lock file must not survive any of the above.
	lock := filepath.Join(cfgpkg.HomeDataDir(), "."+appName+".deploy.lock")
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Errorf("a lock file survived the lifecycle: %s", lock)
	}
}
