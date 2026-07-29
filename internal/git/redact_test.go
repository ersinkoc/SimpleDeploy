package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return strings.TrimSpace(string(data))
}

func fileExists(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

// TestSanitizeOutput_EmptyRepoURL is the regression test for garbled error
// messages. strings.ReplaceAll with an empty needle inserts the replacement
// between every character of the input, so Pull's old unguarded
// sanitizeOutput(output, "") turned "fatal: ..." into
// "<redacted>f<redacted>a<redacted>t<redacted>a<redacted>l..." — which hid the
// real git error from the operator.
func TestSanitizeOutput_EmptyRepoURL(t *testing.T) {
	const output = "fatal: couldn't find remote ref main"
	if got := sanitizeOutput(output, ""); got != output {
		t.Errorf("sanitizeOutput with empty repoURL mangled the message:\n got: %q\nwant: %q", got, output)
	}
}

func TestRedactSecrets(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		secrets []string
		want    string
	}{
		{
			name:    "redacts token",
			input:   "remote: https://ghp_abc123@github.com/u/r.git rejected",
			secrets: []string{"ghp_abc123"},
			want:    "remote: https://<redacted>@github.com/u/r.git rejected",
		},
		{
			name:    "redacts every occurrence",
			input:   "tok tok tok",
			secrets: []string{"tok"},
			want:    "<redacted> <redacted> <redacted>",
		},
		{
			name:    "empty secrets are skipped",
			input:   "unchanged message",
			secrets: []string{"", ""},
			want:    "unchanged message",
		},
		{
			name:    "no secrets at all",
			input:   "unchanged message",
			secrets: nil,
			want:    "unchanged message",
		},
		{
			name:    "multiple secrets",
			input:   "a=one b=two",
			secrets: []string{"one", "two"},
			want:    "a=<redacted> b=<redacted>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := redactSecrets(tt.input, tt.secrets...); got != tt.want {
				t.Errorf("redactSecrets() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestClone_OverwritesExistingDestination covers a deploy that could never
// recover on its own: git clone refuses a non-empty destination, so a source
// tree left behind by an interrupted deploy made every retry fail with
// "destination path already exists" until someone deleted it by hand.
func TestClone_OverwritesExistingDestination(t *testing.T) {
	repoDir := t.TempDir()
	// -b master explicitly: git's default initial branch depends on the host's
	// init.defaultBranch setting, and Clone below asks for a specific branch by
	// name. Pinning it here keeps the test independent of that config.
	runGitCmd(t, repoDir, "init", "-b", "master")
	runGitCmd(t, repoDir, "config", "user.email", "test@test.com")
	runGitCmd(t, repoDir, "config", "user.name", "Test")
	runGitCmd(t, repoDir, "config", "commit.gpgsign", "false")
	writeFile(t, repoDir, "test.txt", "hello")
	runGitCmd(t, repoDir, "add", ".")
	runGitCmd(t, repoDir, "commit", "-m", "initial")

	destDir := t.TempDir() // already exists...
	writeFile(t, destDir, "leftover.txt", "stale")

	if err := Clone(context.Background(), repoDir, "master", destDir, ""); err != nil {
		t.Fatalf("Clone into an existing directory should succeed: %v", err)
	}

	if !fileExists(destDir, "test.txt") {
		t.Error("cloned file should be present")
	}
	if fileExists(destDir, "leftover.txt") {
		t.Error("stale contents should have been cleared before cloning")
	}
}

// TestPull_RecoversFromLocalDivergence covers the second unrecoverable state:
// `git pull` merges, so a locally modified tracked file left the working tree
// wedged and the app could never deploy again. Pull now hard-resets to the
// fetched tip.
func TestPull_RecoversFromLocalDivergence(t *testing.T) {
	repoDir := t.TempDir()
	// -b master explicitly: git's default initial branch depends on the host's
	// init.defaultBranch setting, and Clone below asks for a specific branch by
	// name. Pinning it here keeps the test independent of that config.
	runGitCmd(t, repoDir, "init", "-b", "master")
	runGitCmd(t, repoDir, "config", "user.email", "test@test.com")
	runGitCmd(t, repoDir, "config", "user.name", "Test")
	runGitCmd(t, repoDir, "config", "commit.gpgsign", "false")
	writeFile(t, repoDir, "file.txt", "v1")
	runGitCmd(t, repoDir, "add", ".")
	runGitCmd(t, repoDir, "commit", "-m", "initial")

	cloneDir := t.TempDir()
	if err := Clone(context.Background(), repoDir, "master", cloneDir, ""); err != nil {
		t.Fatalf("Clone failed: %v", err)
	}

	// Diverge locally: modify a tracked file in the checkout.
	writeFile(t, cloneDir, "file.txt", "locally modified")
	// And move the remote forward.
	writeFile(t, repoDir, "file.txt", "v2")
	runGitCmd(t, repoDir, "add", ".")
	runGitCmd(t, repoDir, "commit", "-m", "update")

	if err := Pull(context.Background(), cloneDir, "master"); err != nil {
		t.Fatalf("Pull should recover from local divergence, got: %v", err)
	}
	if got := readFile(t, cloneDir, "file.txt"); got != "v2" {
		t.Errorf("file.txt = %q, want %q (remote state should win)", got, "v2")
	}
}

// TestPull_KeepsUntrackedFiles matters because SimpleDeploy writes a generated
// Dockerfile and .dockerignore into the source tree. A reset that discarded
// them would break every subsequent build for repos without their own.
func TestPull_KeepsUntrackedFiles(t *testing.T) {
	repoDir := t.TempDir()
	// -b master explicitly: git's default initial branch depends on the host's
	// init.defaultBranch setting, and Clone below asks for a specific branch by
	// name. Pinning it here keeps the test independent of that config.
	runGitCmd(t, repoDir, "init", "-b", "master")
	runGitCmd(t, repoDir, "config", "user.email", "test@test.com")
	runGitCmd(t, repoDir, "config", "user.name", "Test")
	runGitCmd(t, repoDir, "config", "commit.gpgsign", "false")
	writeFile(t, repoDir, "file.txt", "v1")
	runGitCmd(t, repoDir, "add", ".")
	runGitCmd(t, repoDir, "commit", "-m", "initial")

	cloneDir := t.TempDir()
	if err := Clone(context.Background(), repoDir, "master", cloneDir, ""); err != nil {
		t.Fatalf("Clone failed: %v", err)
	}
	writeFile(t, cloneDir, "Dockerfile", "FROM alpine\n")

	writeFile(t, repoDir, "file.txt", "v2")
	runGitCmd(t, repoDir, "add", ".")
	runGitCmd(t, repoDir, "commit", "-m", "update")

	if err := Pull(context.Background(), cloneDir, "master"); err != nil {
		t.Fatalf("Pull failed: %v", err)
	}
	if !fileExists(cloneDir, "Dockerfile") {
		t.Error("generated Dockerfile (untracked) must survive the reset")
	}
	if got := strings.TrimSpace(readFile(t, cloneDir, "file.txt")); got != "v2" {
		t.Errorf("tracked file should be updated, got %q", got)
	}
}
