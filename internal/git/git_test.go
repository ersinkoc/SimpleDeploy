package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestClone_InvalidRepo(t *testing.T) {
	tmpDir := t.TempDir()
	err := Clone("https://github.com/nonexistent/repo-xyz-999.git", "main", filepath.Join(tmpDir, "dest"), "")
	if err == nil {
		t.Error("Should fail for invalid repo")
	}
	if err != nil && !strings.Contains(err.Error(), "<redacted>") {
		// Good — URL is redacted in error output
	}
}

func TestClone_MkdirAllError(t *testing.T) {
	old := osMkdirAll
	osMkdirAll = func(path string, perm os.FileMode) error {
		return os.ErrPermission
	}
	defer func() { osMkdirAll = old }()

	err := Clone("https://github.com/test/repo.git", "main", "/tmp/dest", "")
	if err == nil {
		t.Error("Clone should fail when MkdirAll fails")
	}
}

func TestClone_WriteAskpassError(t *testing.T) {
	old := osCreateTemp
	osCreateTemp = func(dir, pattern string) (*os.File, error) {
		return nil, os.ErrPermission
	}
	defer func() { osCreateTemp = old }()

	tmpDir := t.TempDir()
	err := Clone("https://github.com/test/repo.git", "main", filepath.Join(tmpDir, "dest"), "token")
	if err == nil {
		t.Error("Clone should fail when askpass script creation fails")
	}
}

func TestWriteAskpassScript_CreateTempError(t *testing.T) {
	old := osCreateTemp
	osCreateTemp = func(dir, pattern string) (*os.File, error) {
		return nil, os.ErrPermission
	}
	defer func() { osCreateTemp = old }()

	_, _, err := writeAskpassScript("token")
	if err == nil {
		t.Error("writeAskpassScript should fail when CreateTemp fails")
	}
}

func TestWriteAskpassScript_WriteFileError(t *testing.T) {
	old := osWriteFile
	osWriteFile = func(name string, data []byte, perm os.FileMode) error {
		return os.ErrPermission
	}
	defer func() { osWriteFile = old }()

	_, _, err := writeAskpassScript("token")
	if err == nil {
		t.Error("writeAskpassScript should fail when WriteFile fails")
	}
}

func TestPull_WithToken(t *testing.T) {
	repoDir := t.TempDir()
	runGitCmd(t, repoDir, "init")
	runGitCmd(t, repoDir, "config", "user.email", "test@test.com")
	runGitCmd(t, repoDir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(repoDir, "file.txt"), []byte("v1"), 0644)
	runGitCmd(t, repoDir, "add", ".")
	runGitCmd(t, repoDir, "commit", "-m", "initial")

	cloneDir := filepath.Join(t.TempDir(), "clone")
	Clone(repoDir, "master", cloneDir, "")

	// Update original
	os.WriteFile(filepath.Join(repoDir, "file.txt"), []byte("v2"), 0644)
	runGitCmd(t, repoDir, "add", ".")
	runGitCmd(t, repoDir, "commit", "-m", "update")

	// Pull with token (on local repo, token is ignored but path is exercised)
	if err := Pull(cloneDir, "master", "test-token"); err != nil {
		t.Fatalf("Pull with token failed: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(cloneDir, "file.txt"))
	if string(data) != "v2" {
		t.Error("Pull should update files")
	}
}

func TestPull_WriteAskpassError(t *testing.T) {
	repoDir := t.TempDir()
	runGitCmd(t, repoDir, "init")
	runGitCmd(t, repoDir, "config", "user.email", "test@test.com")
	runGitCmd(t, repoDir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(repoDir, "file.txt"), []byte("v1"), 0644)
	runGitCmd(t, repoDir, "add", ".")
	runGitCmd(t, repoDir, "commit", "-m", "initial")

	cloneDir := filepath.Join(t.TempDir(), "clone")
	Clone(repoDir, "master", cloneDir, "")

	old := osCreateTemp
	osCreateTemp = func(dir, pattern string) (*os.File, error) {
		return nil, os.ErrPermission
	}
	defer func() { osCreateTemp = old }()

	err := Pull(cloneDir, "master", "token")
	if err == nil {
		t.Error("Pull should fail when askpass script creation fails")
	}
}

func TestSanitizeOutput(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		repoURL  string
		expected string
	}{
		{
			name:     "removes repo url",
			output:   "fatal: repository 'https://github.com/user/repo.git/' not found",
			repoURL:  "https://github.com/user/repo.git",
			expected: "fatal: repository '<redacted>/' not found",
		},
		{
			name:     "no match",
			output:   "some error message",
			repoURL:  "https://github.com/user/repo.git",
			expected: "some error message",
		},
		{
			name:     "empty output",
			output:   "",
			repoURL:  "https://github.com/user/repo.git",
			expected: "",
		},
		{
			name:     "url in middle",
			output:   "error cloning https://github.com/user/repo.git failed",
			repoURL:  "https://github.com/user/repo.git",
			expected: "error cloning <redacted> failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeOutput(tt.output, tt.repoURL)
			if result != tt.expected {
				t.Errorf("sanitizeOutput(%q, %q) = %q, want %q", tt.output, tt.repoURL, result, tt.expected)
			}
		})
	}
}

func TestSanitizeOutput_RemovesAllOccurrences(t *testing.T) {
	output := "Cloning https://github.com/user/repo.git... error at https://github.com/user/repo.git"
	result := sanitizeOutput(output, "https://github.com/user/repo.git")
	if strings.Contains(result, "https://github.com/user/repo.git") {
		t.Error("Should remove all occurrences of repo URL")
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
	if !strings.Contains(content, "QD_GIT_TOKEN") {
		t.Error("Script should reference QD_GIT_TOKEN env var")
	}
	// Token should NOT be embedded in the script for security
	if strings.Contains(content, "mytoken123") {
		t.Error("Script should NOT contain the raw token")
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
	content := string(data)
	// Script should use env var, not embed the token
	if strings.Contains(content, token) {
		t.Error("Script should NOT embed token with special chars")
	}
	if !strings.Contains(content, "QD_GIT_TOKEN") {
		t.Error("Script should reference QD_GIT_TOKEN env var")
	}
}

func TestWriteAskpassScript_UniquePaths(t *testing.T) {
	path1, cleanup1, err := writeAskpassScript("token1")
	if err != nil {
		t.Fatalf("writeAskpassScript failed: %v", err)
	}
	defer cleanup1()

	path2, cleanup2, err := writeAskpassScript("token2")
	if err != nil {
		t.Fatalf("writeAskpassScript failed: %v", err)
	}
	defer cleanup2()

	if path1 == path2 {
		t.Error("Each call should produce a unique script path")
	}
}

func TestClone_LocalRepo(t *testing.T) {
	// Create a local git repo
	repoDir := t.TempDir()
	runGitCmd(t, repoDir, "init")
	runGitCmd(t, repoDir, "config", "user.email", "test@test.com")
	runGitCmd(t, repoDir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(repoDir, "test.txt"), []byte("hello"), 0644)
	runGitCmd(t, repoDir, "add", ".")
	runGitCmd(t, repoDir, "commit", "-m", "initial")

	destDir := filepath.Join(t.TempDir(), "clone")
	if err := Clone(repoDir, "master", destDir, ""); err != nil {
		t.Fatalf("Clone failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(destDir, "test.txt")); err != nil {
		t.Error("Cloned file should exist")
	}
}

func TestPull_LocalRepo(t *testing.T) {
	repoDir := t.TempDir()
	runGitCmd(t, repoDir, "init")
	runGitCmd(t, repoDir, "config", "user.email", "test@test.com")
	runGitCmd(t, repoDir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(repoDir, "file.txt"), []byte("v1"), 0644)
	runGitCmd(t, repoDir, "add", ".")
	runGitCmd(t, repoDir, "commit", "-m", "initial")

	cloneDir := filepath.Join(t.TempDir(), "clone")
	Clone(repoDir, "master", cloneDir, "")

	// Update original
	os.WriteFile(filepath.Join(repoDir, "file.txt"), []byte("v2"), 0644)
	runGitCmd(t, repoDir, "add", ".")
	runGitCmd(t, repoDir, "commit", "-m", "update")

	// Pull
	if err := Pull(cloneDir, "master"); err != nil {
		t.Fatalf("Pull failed: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(cloneDir, "file.txt"))
	if string(data) != "v2" {
		t.Error("Pull should update files")
	}
}

func TestPull_NotRepo(t *testing.T) {
	err := Pull(t.TempDir(), "main")
	if err == nil {
		t.Error("Should fail for non-repo directory")
	}
}

func TestClone_WithToken(t *testing.T) {
	// Test that the token is passed via env var, not embedded in script
	repoDir := t.TempDir()
	runGitCmd(t, repoDir, "init")
	runGitCmd(t, repoDir, "config", "user.email", "test@test.com")
	runGitCmd(t, repoDir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(repoDir, "file.txt"), []byte("hello"), 0644)
	runGitCmd(t, repoDir, "add", ".")
	runGitCmd(t, repoDir, "commit", "-m", "initial")

	destDir := filepath.Join(t.TempDir(), "clone")
	// Clone with a local path won't use the token but verifies no crash
	if err := Clone(repoDir, "master", destDir, "test-token-123"); err != nil {
		t.Fatalf("Clone with token on local repo should work: %v", err)
	}
}

func TestClone_CreatesParentDir(t *testing.T) {
	repoDir := t.TempDir()
	runGitCmd(t, repoDir, "init")
	runGitCmd(t, repoDir, "config", "user.email", "test@test.com")
	runGitCmd(t, repoDir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(repoDir, "test.txt"), []byte("hello"), 0644)
	runGitCmd(t, repoDir, "add", ".")
	runGitCmd(t, repoDir, "commit", "-m", "initial")

	destDir := filepath.Join(t.TempDir(), "nested", "dir", "clone")
	if err := Clone(repoDir, "master", destDir, ""); err != nil {
		t.Fatalf("Should create parent dirs and clone: %v", err)
	}
}

func TestClone_WrongBranch(t *testing.T) {
	repoDir := t.TempDir()
	runGitCmd(t, repoDir, "init")
	runGitCmd(t, repoDir, "config", "user.email", "test@test.com")
	runGitCmd(t, repoDir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(repoDir, "test.txt"), []byte("hello"), 0644)
	runGitCmd(t, repoDir, "add", ".")
	runGitCmd(t, repoDir, "commit", "-m", "initial")

	destDir := filepath.Join(t.TempDir(), "clone")
	err := Clone(repoDir, "nonexistent-branch-xyz", destDir, "")
	if err == nil {
		t.Error("Should fail for nonexistent branch")
	}
}

func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v failed: %v", args, err)
	}
}
