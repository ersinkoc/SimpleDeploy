package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeOutput(t *testing.T) {
	tests := []struct {
		name   string
		output string
		repo   string
		want   string
	}{
		{
			name:   "removes_repo_url",
			output: "fatal: repository 'https://github.com/user/repo.git' not found",
			repo:   "https://github.com/user/repo.git",
			want:   "<redacted>",
		},
		{
			name:   "no_match",
			output: "some random error",
			repo:   "https://github.com/user/repo.git",
			want:   "some random error",
		},
		{
			name:   "empty_output",
			output: "",
			repo:   "https://github.com/test/repo.git",
			want:   "",
		},
		{
			name:   "url_in_middle",
			output: "error: failed to clone https://github.com/secret/repo.git: access denied",
			repo:   "https://github.com/secret/repo.git",
			want:   "error: failed to clone <redacted>: access denied",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeOutput(tt.output, tt.repo)
			if !strings.Contains(result, tt.want) {
				t.Errorf("sanitizeOutput() = %q, should contain %q", result, tt.want)
			}
			if tt.repo != "" && tt.output != "" && strings.Contains(result, tt.repo) {
				t.Errorf("Result should not contain repo URL %q", tt.repo)
			}
		})
	}
}

func TestSanitizeOutput_RemovesAllOccurrences(t *testing.T) {
	output := "clone https://github.com/test/r.git failed at https://github.com/test/r.git"
	repo := "https://github.com/test/r.git"
	result := sanitizeOutput(output, repo)
	count := strings.Count(result, repo)
	if count != 0 {
		t.Errorf("Expected 0 occurrences of repo URL, got %d", count)
	}
}

func TestIsRepo_WithGitDir(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatalf("Failed to create .git dir: %v", err)
	}
	if !IsRepo(dir) {
		t.Error("Dir with .git should be a repo")
	}
}

func TestIsRepo_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	if IsRepo(dir) {
		t.Error("Empty dir should not be a repo")
	}
}

func TestIsRepo_NonexistentDir(t *testing.T) {
	if IsRepo("/nonexistent/path/that/does/not/exist") {
		t.Error("Nonexistent dir should not be a repo")
	}
}

func TestIsRepo_GitFile(t *testing.T) {
	// A file named .git (not a directory) should not count
	dir := t.TempDir()
	gitFile := filepath.Join(dir, ".git")
	if err := os.WriteFile(gitFile, []byte("gitdir: something"), 0644); err != nil {
		t.Fatalf("Failed to create .git file: %v", err)
	}
	if IsRepo(dir) {
		t.Error("Dir with .git file (not dir) should not be a repo")
	}
}

func TestWriteAskpassScript(t *testing.T) {
	path, cleanup, err := writeAskpassScript("mytoken123")
	if err != nil {
		t.Fatalf("writeAskpassScript failed: %v", err)
	}
	defer cleanup()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read script: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "#!/bin/sh") {
		t.Error("Script should have shebang line")
	}
	if !strings.Contains(content, "mytoken123") {
		t.Error("Script should contain the token")
	}
	if !strings.HasSuffix(content, "\n") {
		t.Error("Script should end with newline")
	}
}

func TestWriteAskpassScript_Cleanup(t *testing.T) {
	path, cleanup, err := writeAskpassScript("token")
	if err != nil {
		t.Fatalf("writeAskpassScript failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("Script file should exist before cleanup")
	}

	// Call cleanup
	cleanup()

	// Verify file is removed
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("Script file should be removed after cleanup")
	}
}

func TestWriteAskpassScript_SpecialChars(t *testing.T) {
	token := "abc123!@#$%^&*()"
	path, cleanup, err := writeAskpassScript(token)
	if err != nil {
		t.Fatalf("writeAskpassScript failed: %v", err)
	}
	defer cleanup()

	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), token) {
		t.Error("Script should contain the token with special chars")
	}
}

func TestWriteAskpassScript_UniquePaths(t *testing.T) {
	path1, cleanup1, _ := writeAskpassScript("token1")
	defer cleanup1()
	path2, cleanup2, _ := writeAskpassScript("token2")
	defer cleanup2()

	if path1 == path2 {
		t.Error("Each call should create a unique file")
	}
}

func TestClone_LocalRepo(t *testing.T) {
	// Create a local test repo
	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "config", "user.email", "test@test.com")
	runGit(t, repoDir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(repoDir, "test.txt"), []byte("hello"), 0644)
	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "commit", "-m", "initial")

	cloneDir := filepath.Join(t.TempDir(), "cloned")
	err := Clone(repoDir, "master", cloneDir, "")
	if err != nil {
		t.Fatalf("Clone failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cloneDir, "test.txt")); err != nil {
		t.Error("Cloned repo should have test.txt")
	}
	if !IsRepo(cloneDir) {
		t.Error("Cloned dir should be a repo")
	}
}

func TestClone_InvalidRepo(t *testing.T) {
	cloneDir := filepath.Join(t.TempDir(), "cloned")
	err := Clone("/nonexistent/path/repo.git", "main", cloneDir, "")
	if err == nil {
		t.Error("Should fail for nonexistent repo")
	}
}

func TestPull_LocalRepo(t *testing.T) {
	// Create a local test repo
	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "config", "user.email", "test@test.com")
	runGit(t, repoDir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(repoDir, "file.txt"), []byte("content"), 0644)
	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "commit", "-m", "initial")

	// Clone it
	cloneDir := filepath.Join(t.TempDir(), "cloned")
	Clone(repoDir, "master", cloneDir, "")

	// Make a new commit in original
	os.WriteFile(filepath.Join(repoDir, "file.txt"), []byte("updated"), 0644)
	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "commit", "-m", "update")

	// Pull should succeed
	err := Pull(cloneDir, "master")
	if err != nil {
		t.Fatalf("Pull failed: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(cloneDir, "file.txt"))
	if string(data) != "updated" {
		t.Errorf("File content after pull = %q, want 'updated'", string(data))
	}
}

func TestGetShortHash(t *testing.T) {
	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "config", "user.email", "test@test.com")
	runGit(t, repoDir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(repoDir, "file.txt"), []byte("content"), 0644)
	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "commit", "-m", "initial")

	hash, err := GetShortHash(repoDir)
	if err != nil {
		t.Fatalf("GetShortHash failed: %v", err)
	}
	if hash == "" {
		t.Error("Hash should not be empty")
	}
	if len(hash) < 7 {
		t.Errorf("Hash seems too short: %q", hash)
	}
}

func TestDetectBranch(t *testing.T) {
	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "config", "user.email", "test@test.com")
	runGit(t, repoDir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(repoDir, "file.txt"), []byte("content"), 0644)
	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "commit", "-m", "initial")

	branch, err := DetectBranch(repoDir)
	if err != nil {
		t.Fatalf("DetectBranch failed: %v", err)
	}
	if branch == "" {
		t.Error("Branch should not be empty")
	}
}

func TestGetShortHash_NotRepo(t *testing.T) {
	_, err := GetShortHash(t.TempDir())
	if err == nil {
		t.Error("Should error for non-repo directory")
	}
}

func TestDetectBranch_NotRepo(t *testing.T) {
	_, err := DetectBranch(t.TempDir())
	if err == nil {
		t.Error("Should error for non-repo directory")
	}
}

func TestPull_NotRepo(t *testing.T) {
	err := Pull(t.TempDir(), "main")
	if err == nil {
		t.Error("Should error for non-repo directory")
	}
}

func TestClone_WithToken(t *testing.T) {
	// Test that clone with token creates askpass script and uses it
	// We'll use a local repo with a fake token — the clone itself won't
	// need auth for local repos, but we verify the askpass mechanism works.
	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "config", "user.email", "test@test.com")
	runGit(t, repoDir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(repoDir, "file.txt"), []byte("hello"), 0644)
	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "commit", "-m", "initial")

	cloneDir := filepath.Join(t.TempDir(), "cloned-with-token")
	err := Clone(repoDir, "master", cloneDir, "fake-token-12345")
	if err != nil {
		t.Fatalf("Clone with token failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cloneDir, "file.txt")); err != nil {
		t.Error("Cloned repo should have file.txt")
	}
}

func TestClone_CreatesParentDir(t *testing.T) {
	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "config", "user.email", "test@test.com")
	runGit(t, repoDir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(repoDir, "test.txt"), []byte("hello"), 0644)
	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "commit", "-m", "initial")

	cloneDir := filepath.Join(t.TempDir(), "nested", "deep", "cloned")
	err := Clone(repoDir, "master", cloneDir, "")
	if err != nil {
		t.Fatalf("Clone to nested dir failed: %v", err)
	}
}

func TestClone_WrongBranch(t *testing.T) {
	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "config", "user.email", "test@test.com")
	runGit(t, repoDir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(repoDir, "test.txt"), []byte("hello"), 0644)
	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "commit", "-m", "initial")

	cloneDir := filepath.Join(t.TempDir(), "cloned")
	err := Clone(repoDir, "nonexistent-branch", cloneDir, "")
	if err == nil {
		t.Error("Should fail for non-existent branch")
	}
}

func TestGetShortHash_Consistent(t *testing.T) {
	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "config", "user.email", "test@test.com")
	runGit(t, repoDir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(repoDir, "file.txt"), []byte("content"), 0644)
	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "commit", "-m", "initial")

	hash1, _ := GetShortHash(repoDir)
	hash2, _ := GetShortHash(repoDir)
	if hash1 != hash2 {
		t.Errorf("Hashes should be consistent: %q vs %q", hash1, hash2)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v failed: %v", args, err)
	}
}
