package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// errStatusUnavailable stands in for "the Docker daemon could not be reached",
// which ContainerStatus surfaces as a non-nil error (as opposed to the
// "not found" string it returns for a container that simply does not exist).
var errStatusUnavailable = errors.New("docker daemon unreachable")

// TestRenderEnvFile_GeneratedWins locks in the precedence rule: dotenv parsers
// apply assignments top-to-bottom, so the generated block must come last and
// override anything with the same key in an imported file. Otherwise a stale
// DATABASE_URL carried over from an old .env would shadow the connection
// string for the database SimpleDeploy just provisioned.
func TestRenderEnvFile_GeneratedWins(t *testing.T) {
	imported := []byte("DATABASE_URL=postgres://old/db\nAPP_NAME=legacy\n")
	generated := map[string]string{"DATABASE_URL": "postgres://new/db"}

	out := string(renderEnvFile(imported, generated))

	oldIdx := strings.Index(out, "postgres://old/db")
	newIdx := strings.Index(out, "postgres://new/db")
	if oldIdx == -1 {
		t.Fatal("imported content was dropped")
	}
	if newIdx == -1 {
		t.Fatal("generated content was dropped")
	}
	if newIdx < oldIdx {
		t.Error("generated value must appear after the imported one so it wins")
	}
	if !strings.Contains(out, "APP_NAME=legacy") {
		t.Error("unrelated imported keys must be preserved")
	}
}

// TestRenderEnvFile_PreservesImportOnly guards the regression this fix was
// written for: the imported file used to be written to disk and then silently
// truncated by the write of generated variables.
func TestRenderEnvFile_PreservesImportOnly(t *testing.T) {
	out := string(renderEnvFile([]byte("A=1\nB=2"), nil))
	for _, want := range []string{"A=1", "B=2"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q; got:\n%s", want, out)
		}
	}
}

// TestRenderEnvFile_Deterministic ensures repeated renders of identical input
// are byte-identical, so an unchanged app does not produce a churning .env.
func TestRenderEnvFile_Deterministic(t *testing.T) {
	gen := map[string]string{"Z": "1", "A": "2", "M": "3"}
	first := string(renderEnvFile(nil, gen))
	for i := 0; i < 20; i++ {
		if got := string(renderEnvFile(nil, gen)); got != first {
			t.Fatalf("render is not deterministic:\n%q\nvs\n%q", first, got)
		}
	}
	if !strings.HasPrefix(first, "A=2\n") {
		t.Errorf("generated keys should be sorted, got:\n%s", first)
	}
}

func TestRenderEnvFile_Empty(t *testing.T) {
	if got := renderEnvFile(nil, nil); len(got) != 0 {
		t.Errorf("expected empty output, got %q", got)
	}
}

// TestValidateEnvSourcePath_AcceptsPathOutsideAppDir is the regression test for
// the import feature being unusable: the old check required the file to live
// inside the app directory SimpleDeploy had just created, so no real operator
// path could ever pass.
func TestValidateEnvSourcePath_AcceptsPathOutsideAppDir(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "my.env")
	if err := os.WriteFile(src, []byte("K=V\n"), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	resolved, err := validateEnvSourcePath(src, filepath.Join("/opt/simpledeploy/apps/x", ".env"))
	if err != nil {
		t.Fatalf("path outside the app dir must be accepted, got: %v", err)
	}
	if resolved == "" {
		t.Error("expected a resolved absolute path")
	}
}

func TestValidateEnvSourcePath_Rejects(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "same.env")
	if err := os.WriteFile(existing, []byte("K=V\n"), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	tests := []struct {
		name string
		path string
		dest string
	}{
		{"empty", "", filepath.Join(dir, "dest.env")},
		{"whitespace only", "   ", filepath.Join(dir, "dest.env")},
		{"missing file", filepath.Join(dir, "nope.env"), filepath.Join(dir, "dest.env")},
		{"directory", dir, filepath.Join(dir, "dest.env")},
		{"same as destination", existing, existing},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := validateEnvSourcePath(tt.path, tt.dest); err == nil {
				t.Errorf("validateEnvSourcePath(%q) should have failed", tt.path)
			}
		})
	}
}

// TestIsFailedContainerState pins which Docker states are treated as a
// definitive deploy failure. Only these trigger an automatic rollback in
// redeploy; transient states must keep polling and unreadable ones must not
// tear down a deployment that may be healthy.
func TestIsFailedContainerState(t *testing.T) {
	failed := []string{"exited", "dead"}
	notFailed := []string{"running", "created", "restarting", "paused", "removing", "not found", "unknown", ""}

	for _, s := range failed {
		if !isFailedContainerState(s) {
			t.Errorf("%q should be treated as a failed state", s)
		}
	}
	for _, s := range notFailed {
		if isFailedContainerState(s) {
			t.Errorf("%q must NOT trigger an automatic rollback", s)
		}
	}
}

// TestWaitForContainer_TerminatesOnUnreadableStatus guards against the CLI
// blocking for the full start timeout when Docker cannot be reached at all.
func TestWaitForContainer_TerminatesOnUnreadableStatus(t *testing.T) {
	old := dockerContainerStatus
	dockerContainerStatus = func(ctx context.Context, name string) (string, error) {
		return "", errStatusUnavailable
	}
	defer func() { dockerContainerStatus = old }()

	if got := waitForContainer(context.Background(), "qd-x", time.Minute); got != "unknown" {
		t.Errorf("waitForContainer = %q, want \"unknown\"", got)
	}
}

func TestWaitForContainer_ReturnsRunning(t *testing.T) {
	old := dockerContainerStatus
	dockerContainerStatus = func(ctx context.Context, name string) (string, error) {
		return "running", nil
	}
	defer func() { dockerContainerStatus = old }()

	if got := waitForContainer(context.Background(), "qd-x", time.Minute); got != "running" {
		t.Errorf("waitForContainer = %q, want \"running\"", got)
	}
}

func TestWaitForContainer_ReturnsTerminalFailureImmediately(t *testing.T) {
	old := dockerContainerStatus
	calls := 0
	dockerContainerStatus = func(ctx context.Context, name string) (string, error) {
		calls++
		return "exited", nil
	}
	defer func() { dockerContainerStatus = old }()

	if got := waitForContainer(context.Background(), "qd-x", time.Minute); got != "exited" {
		t.Errorf("waitForContainer = %q, want \"exited\"", got)
	}
	if calls != 1 {
		t.Errorf("a terminal state should be reported after one check, got %d calls", calls)
	}
}
