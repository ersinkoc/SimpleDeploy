package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/ersinkoc/SimpleDeploy/internal/docker"
)

// These tests close the second gap CLAUDE.md calls out: the crash-loop
// detection that guards every deploy and drives redeploy's rollback was only
// ever exercised against a scripted stub. The bug it exists for — a
// crash-looping container reported as a successful deploy — was invisible to
// the unit suite precisely because a stub cannot reproduce the timing that
// `restart: unless-stopped` produces on a real daemon: the container flickers
// between "restarting" and "running" and NEVER settles in "exited"/"dead", so a
// single status read sees "running" and calls it a success.
//
// Opt-in like the rest:
//
//	SIMPLEDEPLOY_INTEGRATION=1 go test -p=1 -count=1 ./internal/cli/

// runCompose runs a compose subcommand in dir, returning combined output.
func runCompose(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("docker", append([]string{"compose"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// writeCrashLoopCompose writes a compose file whose service exits immediately
// and is restarted forever — the exact shape SimpleDeploy generates for an app
// whose image crashes on boot.
func writeCrashLoopCompose(t *testing.T, dir, containerName string, exitCode int) {
	t.Helper()
	yaml := "services:\n" +
		"  probe:\n" +
		"    image: alpine:3.21\n" +
		"    container_name: " + containerName + "\n" +
		"    restart: unless-stopped\n" +
		"    command: [\"sh\", \"-c\", \"exit " + strconv.Itoa(exitCode) + "\"]\n"
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(yaml), 0600); err != nil {
		t.Fatalf("write compose: %v", err)
	}
}

// TestWaitForContainer_DetectsRealCrashLoop starts a genuinely crash-looping
// container and asserts waitForContainer reports it rather than believing the
// "running" it will briefly observe.
func TestWaitForContainer_DetectsRealCrashLoop(t *testing.T) {
	requireCompose(t)

	dir := t.TempDir()
	const name = "qd-crashprobe"
	writeCrashLoopCompose(t, dir, name, 1)

	_, _ = runCompose(t, dir, "down", "-v")
	if out, err := runCompose(t, dir, "up", "-d"); err != nil {
		t.Fatalf("compose up: %v\n%s", err, out)
	}
	t.Cleanup(func() { _, _ = runCompose(t, dir, "down", "-v") })

	// The real stability window, not the shortened one TestMain installs: this
	// test is about behaviour under real Docker timing.
	old := containerStableFor
	containerStableFor = 5 * time.Second
	defer func() { containerStableFor = old }()

	status := waitForContainer(context.Background(), name, 30*time.Second)
	if status != statusCrashLooping {
		t.Errorf("waitForContainer = %q, want %q — a container restarting forever must not be reported as up", status, statusCrashLooping)
	}
	if !isFailedContainerState(status) {
		t.Errorf("%q must be a failed state, or redeploy will not roll back", status)
	}
}

// TestWaitForContainer_AcceptsRealHealthyContainer is the other half: a
// container that genuinely stays up must be reported as running, so the
// crash-loop check cannot be satisfied by simply failing everything.
func TestWaitForContainer_AcceptsRealHealthyContainer(t *testing.T) {
	requireCompose(t)

	dir := t.TempDir()
	const name = "qd-healthyprobe"
	yaml := "services:\n" +
		"  probe:\n" +
		"    image: alpine:3.21\n" +
		"    container_name: " + name + "\n" +
		"    restart: unless-stopped\n" +
		"    command: [\"sleep\", \"120\"]\n"
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(yaml), 0600); err != nil {
		t.Fatalf("write compose: %v", err)
	}

	_, _ = runCompose(t, dir, "down", "-v")
	if out, err := runCompose(t, dir, "up", "-d"); err != nil {
		t.Fatalf("compose up: %v\n%s", err, out)
	}
	t.Cleanup(func() { _, _ = runCompose(t, dir, "down", "-v") })

	old := containerStableFor
	containerStableFor = 3 * time.Second
	defer func() { containerStableFor = old }()

	if status := waitForContainer(context.Background(), name, 30*time.Second); status != "running" {
		t.Errorf("waitForContainer = %q, want \"running\" for a container that stays up", status)
	}
}

// TestContainerRestartCount_TracksRealRestarts pins the signal the crash-loop
// detection is built on. It is the only thing that persists between the
// flickers of a restarting container, so if Docker ever stopped reporting it
// the detection would silently degrade to the status-only behaviour that
// shipped the bug.
func TestContainerRestartCount_TracksRealRestarts(t *testing.T) {
	requireCompose(t)

	dir := t.TempDir()
	const name = "qd-restartprobe"
	writeCrashLoopCompose(t, dir, name, 2)

	_, _ = runCompose(t, dir, "down", "-v")
	if out, err := runCompose(t, dir, "up", "-d"); err != nil {
		t.Fatalf("compose up: %v\n%s", err, out)
	}
	t.Cleanup(func() { _, _ = runCompose(t, dir, "down", "-v") })

	ctx := context.Background()
	first, err := docker.ContainerRestartCount(ctx, name)
	if err != nil {
		t.Fatalf("ContainerRestartCount: %v", err)
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		later, err := docker.ContainerRestartCount(ctx, name)
		if err != nil {
			t.Fatalf("ContainerRestartCount: %v", err)
		}
		if later > first {
			return // the counter moves, which is all the detection needs
		}
		time.Sleep(time.Second)
	}
	t.Errorf("restart count never moved past %d for a crash-looping container; crash-loop detection depends on it", first)
}
