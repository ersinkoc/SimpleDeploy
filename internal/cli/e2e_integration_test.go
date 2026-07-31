package cli

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cfgpkg "github.com/ersinkoc/SimpleDeploy/internal/config"
	"github.com/ersinkoc/SimpleDeploy/internal/docker"
	"github.com/ersinkoc/SimpleDeploy/internal/state"
	"github.com/ersinkoc/SimpleDeploy/internal/webhook"
)

// TestEndToEnd_DeployPushRedeployRollback drives the whole chain CLAUDE.md says
// the unit suite cannot see: a real `deploy` (real docker build, real compose
// up, real container), then a real HTTP push to a real webhook server, then a
// redeploy of a genuinely crashing image to prove the rollback actually fires.
//
// This is the shape of run that caught most of the bugs recorded in CLAUDE.md.
// Several of them — a crash-looping container reported as a successful deploy,
// a rollback that never fired, an app recorded `running` while serving 502 —
// were each invisible to every unit test in this repository.
//
// One substitution: git. state.ValidateRepoURL deliberately refuses local paths
// at the input layer (deploying from a host path would let a caller copy
// arbitrary host state into an app), so a local directory cannot be used as a
// remote without standing up a git server. gitClone/gitPull are therefore
// replaced with a copy from a fixture directory, and mutating that directory is
// how this test simulates "someone pushed a new commit". EVERYTHING else runs
// for real.
//
//	SIMPLEDEPLOY_INTEGRATION=1 go test -p=1 -count=1 -run TestEndToEnd ./internal/cli/
func TestEndToEnd_DeployPushRedeployRollback(t *testing.T) {
	requireCompose(t)

	const appName = "e2eprobe"
	const secret = "whk_e2e_secret"

	// --- environment -------------------------------------------------------
	root := t.TempDir()
	oldBase := cfgpkg.BaseDir
	t.Cleanup(func() { cfgpkg.BaseDir = oldBase })
	cfgpkg.BaseDir = filepath.Join(root, "opt", "simpledeploy")
	state.InitState(filepath.Join(root, "state"))

	if err := state.SaveConfig(&state.GlobalConfig{
		BaseDomain: "e2e.example.com",
		// Traefik discovers containers by label, so no proxy container has to
		// run for this test; the Caddy path is covered by the proxy package's
		// own bind-mount integration tests.
		Proxy:         "traefik",
		AcmeEmail:     "admin@example.com",
		WebhookPort:   0,
		WebhookSecret: secret,
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	// The app containers attach to this network.
	_ = exec.Command("docker", "network", "create", "simpledeploy").Run()

	// Real stability window: this test is about behaviour under real Docker
	// timing, not the 10 ms TestMain installs for speed.
	oldStable := containerStableFor
	containerStableFor = 2 * time.Second
	t.Cleanup(func() { containerStableFor = oldStable })

	// --- the "repository" --------------------------------------------------
	repoDir := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	writeServingApp(t, repoDir, "APP_V1")
	installCopyingGit(t, repoDir)

	appDir := cfgpkg.AppDir(appName)
	containerName := docker.ContainerName(appName)
	t.Cleanup(func() {
		down := exec.Command("docker", "compose", "down", "-v")
		down.Dir = appDir
		_ = down.Run()
		_ = exec.Command("docker", "rm", "-f", containerName).Run()
		// Remove every image this test built.
		out, _ := exec.Command("docker", "images", "--format", "{{.Repository}}:{{.Tag}}", appName).Output()
		for _, tag := range strings.Fields(string(out)) {
			_ = exec.Command("docker", "rmi", "-f", tag).Run()
		}
	})

	// --- 1. deploy ---------------------------------------------------------
	// repo, branch(default), private?n, name, type 7 (existing Dockerfile),
	// port, env vars(end), import .env?n, databases(None), subdomain,
	// extra headers(end), webhook?y, start?y
	input := strings.Join([]string{
		"https://example.com/e2e/probe.git", "", "n", appName, "7", "3000",
		"", "n", "6", appName, "", "y", "y",
	}, "\n") + "\n"
	setWizardInput(t, input)

	out := captureStdout(func() {
		if err := RunDeploy(); err != nil {
			t.Errorf("RunDeploy: %v", err)
		}
	})
	if t.Failed() {
		t.Fatalf("deploy output:\n%s", out)
	}

	app, err := state.GetApp(appName)
	if err != nil {
		t.Fatalf("app missing from state after deploy: %v\noutput:\n%s", err, out)
	}
	if app.Status != "running" {
		t.Fatalf("Status = %q, want running\noutput:\n%s", app.Status, out)
	}
	v1Image := app.CurrentImage
	if v1Image == "" {
		t.Fatal("CurrentImage not recorded")
	}
	// The webhook URL and secret must have been printed — this is the only
	// place either is ever shown.
	if !strings.Contains(out, "/_qd/webhook/"+appName) || !strings.Contains(out, secret) {
		t.Errorf("deploy did not print the push-to-deploy setup details:\n%s", out)
	}
	assertServes(t, containerName, "APP_V1")

	// --- 2. push -> redeploy, over real HTTP -------------------------------
	srvURL, waitWebhookDeploys := startWebhookServer(t, secret)

	writeServingApp(t, repoDir, "APP_V2") // "someone pushed"
	postPush(t, srvURL, appName, secret, `{"ref":"refs/heads/main"}`, http.StatusOK)

	waitFor(t, 240*time.Second, "redeploy to finish", func() bool {
		cur, err := state.GetApp(appName)
		return err == nil && cur.DeployCount == 2
	})

	app, err = state.GetApp(appName)
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if app.CurrentImage == v1Image {
		t.Error("CurrentImage did not change after the push-triggered redeploy")
	}
	if app.Status != "running" {
		t.Errorf("Status = %q after redeploy, want running", app.Status)
	}
	v2Image := app.CurrentImage
	assertServes(t, containerName, "APP_V2")

	// --- 3. push a broken build -> rollback --------------------------------
	// The container starts and exits immediately, which `restart:
	// unless-stopped` turns into a crash loop — the case that used to be
	// reported as a successful deploy while the site served 502.
	writeCrashingApp(t, repoDir)
	postPush(t, srvURL, appName, secret, `{"ref":"refs/heads/main"}`, http.StatusOK)

	waitFor(t, 240*time.Second, "rollback to restore the previous deployment", func() bool {
		return servesMarker(containerName, "APP_V2")
	})

	waitWebhookDeploys()

	app, err = state.GetApp(appName)
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if app.CurrentImage != v2Image {
		t.Errorf("CurrentImage = %q after a failed deploy, want the last working image %q — "+
			"state must not claim an image that was rolled back", app.CurrentImage, v2Image)
	}
	if app.DeployCount != 2 {
		t.Errorf("DeployCount = %d, want 2 — a rolled-back deploy must not count as one", app.DeployCount)
	}
	assertServes(t, containerName, "APP_V2")
}

// writeServingApp writes a Dockerfile for a container that answers HTTP on port
// 3000 with marker and stays up.
//
// It serves via a `nc` loop rather than a web server: alpine's base busybox has
// no httpd applet (that lives in busybox-extras), and installing a package
// would make every build in this test hit the network. `nc` and `wget` are both
// in the base image, so nothing extra is pulled.
func writeServingApp(t *testing.T, dir, marker string) {
	t.Helper()
	// Every newline below is a printf ESCAPE (backslash-n in the Dockerfile),
	// not a real newline — a real one would split the CMD line and make the
	// Dockerfile unparseable. Content-Length counts the marker plus its
	// trailing newline.
	response := fmt.Sprintf("HTTP/1.1 200 OK\\r\\nContent-Length: %d\\r\\nConnection: close\\r\\n\\r\\n%s\\n", len(marker)+1, marker)
	dockerfile := "FROM alpine:3.21\n" +
		"EXPOSE 3000\n" +
		"CMD [\"sh\", \"-c\", \"while true; do printf '" + response + "' | nc -l -p 3000; done\"]\n"
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(dockerfile), 0644); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}
}

// writeCrashingApp writes a Dockerfile whose container exits immediately.
func writeCrashingApp(t *testing.T, dir string) {
	t.Helper()
	dockerfile := "FROM alpine:3.21\n" +
		"EXPOSE 3000\n" +
		"CMD [\"sh\", \"-c\", \"echo boom >&2; exit 1\"]\n"
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(dockerfile), 0644); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}
}

// installCopyingGit replaces clone/pull with a copy from src. See the note on
// the test for why a real local remote is not usable.
func installCopyingGit(t *testing.T, src string) {
	t.Helper()
	origClone, origPull := gitClone, gitPull
	t.Cleanup(func() { gitClone, gitPull = origClone, origPull })

	copyTree := func(dest string) error {
		if err := os.MkdirAll(dest, 0755); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			data, err := os.ReadFile(filepath.Join(src, e.Name()))
			if err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(dest, e.Name()), data, 0644); err != nil {
				return err
			}
		}
		return nil
	}

	gitClone = func(ctx context.Context, repo, branch, dest, token string) error {
		return copyTree(dest)
	}
	gitPull = func(ctx context.Context, dir, branch string, token ...string) error {
		return copyTree(dir)
	}
}

// startWebhookServer runs a real webhook server wired to the real redeploy
// handler, and returns its base URL.
func startWebhookServer(t *testing.T, secret string) (string, func()) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pick port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	srv := webhook.NewServer(port, secret)
	srv.SetDeployHandler(func(ctx context.Context, appName string) error {
		return RunRedeployContext(ctx, []string{appName})
	})
	go func() { _ = srv.Start() }()

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitFor(t, 15*time.Second, "webhook server to accept connections", func() bool {
		resp, err := http.Get(base + "/_qd/health")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})
	return base, srv.WaitDeploys
}

// postPush delivers a signed GitHub-style push over real HTTP.
func postPush(t *testing.T, base, appName, secret, body string, wantStatus int) {
	t.Helper()

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequest(http.MethodPost, base+"/_qd/webhook/"+appName, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", sig)
	req.Header.Set("X-GitHub-Event", "push")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST push: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		t.Fatalf("push returned %d, want %d", resp.StatusCode, wantStatus)
	}
}

// servesMarker reports whether the container serves marker on its own port.
func servesMarker(containerName, marker string) bool {
	out, err := exec.Command("docker", "exec", containerName,
		"wget", "-q", "-O", "-", "http://127.0.0.1:3000/").CombinedOutput()
	return err == nil && strings.Contains(string(out), marker)
}

func assertServes(t *testing.T, containerName, marker string) {
	t.Helper()
	waitFor(t, 60*time.Second, "container to serve "+marker, func() bool {
		return servesMarker(containerName, marker)
	})
}

// waitFor polls cond until it holds or the budget expires.
func waitFor(t *testing.T, budget time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("timed out after %v waiting for %s", budget, what)
}
