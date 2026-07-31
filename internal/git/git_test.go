package git

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestClone_InvalidRepo(t *testing.T) {
	tmpDir := t.TempDir()
	err := Clone(context.Background(), "https://github.com/nonexistent/repo-xyz-999.git", "main", filepath.Join(tmpDir, "dest"), "")
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

	err := Clone(context.Background(), "https://github.com/test/repo.git", "main", "/tmp/dest", "")
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
	err := Clone(context.Background(), "https://github.com/test/repo.git", "main", filepath.Join(tmpDir, "dest"), "token")
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
	Clone(context.Background(), repoDir, "master", cloneDir, "")

	// Update original
	os.WriteFile(filepath.Join(repoDir, "file.txt"), []byte("v2"), 0644)
	runGitCmd(t, repoDir, "add", ".")
	runGitCmd(t, repoDir, "commit", "-m", "update")

	// Pull with token (on local repo, token is ignored but path is exercised)
	if err := Pull(context.Background(), cloneDir, "master", "test-token"); err != nil {
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
	Clone(context.Background(), repoDir, "master", cloneDir, "")

	old := osCreateTemp
	osCreateTemp = func(dir, pattern string) (*os.File, error) {
		return nil, os.ErrPermission
	}
	defer func() { osCreateTemp = old }()

	err := Pull(context.Background(), cloneDir, "master", "token")
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
	if err := Clone(context.Background(), repoDir, "master", destDir, ""); err != nil {
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
	Clone(context.Background(), repoDir, "master", cloneDir, "")

	// Update original
	os.WriteFile(filepath.Join(repoDir, "file.txt"), []byte("v2"), 0644)
	runGitCmd(t, repoDir, "add", ".")
	runGitCmd(t, repoDir, "commit", "-m", "update")

	// Pull
	if err := Pull(context.Background(), cloneDir, "master"); err != nil {
		t.Fatalf("Pull failed: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(cloneDir, "file.txt"))
	if string(data) != "v2" {
		t.Error("Pull should update files")
	}
}

func TestPull_NotRepo(t *testing.T) {
	err := Pull(context.Background(), t.TempDir(), "main")
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
	if err := Clone(context.Background(), repoDir, "master", destDir, "test-token-123"); err != nil {
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
	if err := Clone(context.Background(), repoDir, "master", destDir, ""); err != nil {
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
	err := Clone(context.Background(), repoDir, "nonexistent-branch-xyz", destDir, "")
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

// TestWriteAskpassScript_IsExecutable is a regression test for private-repo
// deploys being broken outright.
//
// writeAskpassScript passed 0700 to os.WriteFile, but os.WriteFile only honours
// its perm argument when it CREATES the file — os.CreateTemp had already made
// it at 0600. Git therefore could not exec the helper:
//
//	fatal: cannot exec '/tmp/qd-askpass-...': Permission denied
//	fatal: could not read Username for 'https://github.com': terminal prompts disabled
//
// which failed every clone and pull of a private repository, including
// unattended webhook redeploys. Verified against real git on Linux: the same
// script at 0700 authenticates normally.
//
// Skipped on Windows, where Go's FileMode does not reflect ACLs and there is no
// execute bit.
func TestWriteAskpassScript_IsExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no POSIX execute bit on Windows")
	}
	path, cleanup, err := writeAskpassScript("tok")
	if err != nil {
		t.Fatalf("writeAskpassScript failed: %v", err)
	}
	defer cleanup()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if perm := info.Mode().Perm(); perm&0100 == 0 {
		t.Errorf("askpass script mode = %#o, want owner-executable — git cannot exec it otherwise", perm)
	}
	// Owner-only: the script itself carries no secret (the token arrives via
	// QD_GIT_TOKEN in the environment), but keep it off-limits to other users.
	if perm := info.Mode().Perm(); perm&0077 != 0 {
		t.Errorf("askpass script mode = %#o, want owner-only", perm)
	}
}

// TestWriteAskpassScript_ChmodError covers the failure branch of the chmod
// added alongside the fix above: the temp file must not be left behind.
func TestWriteAskpassScript_ChmodError(t *testing.T) {
	var chmodded string
	origChmod := osChmod
	osChmod = func(name string, mode os.FileMode) error {
		chmodded = name
		return errors.New("chmod boom")
	}
	defer func() { osChmod = origChmod }()

	_, _, err := writeAskpassScript("token")
	if err == nil {
		t.Fatal("writeAskpassScript should fail when chmod fails")
	}
	if chmodded == "" {
		t.Fatal("chmod was never attempted")
	}
	if _, statErr := os.Stat(chmodded); !os.IsNotExist(statErr) {
		t.Errorf("temp script %s should have been removed after chmod failure", chmodded)
	}
}

// TestPull_CancelledByContext verifies that cancelling the caller's context
// kills the in-flight git subprocess rather than waiting for it to finish.
//
// This test exists because the comment in redeploy.go used to claim that
// git.Pull, docker.BuildImage, and docker.ComposeUp "do NOT honor caller ctx"
// — which was wrong: all three use exec.CommandContext(ctx, ...). The
// misleading comment risked someone "fixing" the already-working cancellation
// path. This test proves the behaviour so a future reader does not have to
// take the comment on faith.
//
// The test sets up a real local repo whose fetch target is a local
// black-hole HTTP server (accepts the connection but holds the response),
// giving git enough work to still be running when we cancel. It then
// verifies that Pull returns with an error rather than hanging — proving
// the subprocess was killed by ctx.Done, not by completing normally.
// CommandContext kills only the direct child (git); the surviving HTTP
// helper (git-remote-https) keeps Pull's output pipes open until the
// server's bounded hold (5 s, below) ends the exchange, so this test
// takes a few seconds on every platform instead of returning instantly.
func TestPull_CancelledByContext(t *testing.T) {
	// Set up a clone of a local repo so we have a valid working tree.
	repoDir := t.TempDir()
	runGitCmd(t, repoDir, "init")
	runGitCmd(t, repoDir, "config", "user.email", "test@test.com")
	runGitCmd(t, repoDir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(repoDir, "file.txt"), []byte("v1"), 0644)
	runGitCmd(t, repoDir, "add", ".")
	runGitCmd(t, repoDir, "commit", "-m", "initial")

	cloneDir := filepath.Join(t.TempDir(), "clone")
	if err := Clone(context.Background(), repoDir, "master", cloneDir, ""); err != nil {
		t.Fatalf("Clone failed: %v", err)
	}

	// Point origin at a local black-hole HTTP server: the TCP handshake
	// completes (so git is reliably mid-request) but the server holds the
	// response, so fetch blocks until we cancel. A local listener is
	// deterministic on every platform — a blackholed IP like 192.0.2.1
	// fails fast with ENETUNREACH on hosts without a default route, which
	// would make the strict errors.Is(err, context.Canceled) assertion
	// flaky.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to open black-hole listener: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	// connected is closed the first time the listener accepts a connection.
	// Accept only returns after the TCP handshake completes, which happens
	// once git's HTTP child (git-remote-https) has spawned and started its
	// request — so the channel is a deterministic "the subprocess is
	// mid-request" signal, unlike a fixed sleep, which races subprocess
	// startup on loaded CI runners.
	connected := make(chan struct{})
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed during cleanup
			}
			select {
			case <-connected:
			default:
				close(connected)
			}
			// Hold the connection open and never respond: read and
			// discard so the socket stays alive until git is killed. The
			// hold is bounded — after 5 s the server answers 404 and
			// closes. CommandContext kills only the direct child (git),
			// so on every platform the surviving git-remote-https child
			// keeps Pull's Wait blocked until the HTTP exchange ends; an
			// established localhost connection would otherwise never
			// terminate and the test would die to go test's timeout
			// instead of asserting.
			go func(c net.Conn) {
				defer c.Close()
				_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
				_, _ = io.Copy(io.Discard, c)
				_, _ = c.Write([]byte("HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
			}(conn)
		}
	}()
	runGitCmd(t, cloneDir, "remote", "set-url", "origin",
		fmt.Sprintf("http://%s/delayed.git", ln.Addr().String()))

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel once git is demonstrably mid-request (first connection
	// accepted). The 10s fallback only fires if git never connects at all,
	// so the test cannot hang forever even then.
	go func() {
		select {
		case <-connected:
		case <-time.After(10 * time.Second):
		}
		cancel()
	}()

	err = Pull(ctx, cloneDir, "master")

	if err == nil {
		t.Fatal("Pull should fail when context is cancelled")
	}

	// Prove the subprocess was killed by ctx rather than completing on its
	// own. Note that errors.Is(err, context.Canceled) does NOT hold here:
	// os/exec documents that when a killed command exits with a non-success
	// status, Wait returns the command's usual exit status and drops the
	// context error. The reliable, threshold-free discriminator is that the
	// error chain contains an *exec.ExitError from the git subprocess, and
	// on Unix its process state shows the process was killed, not exited
	// normally.
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("Pull returned %v; expected an *exec.ExitError from the git subprocess", err)
	}
	if runtime.GOOS != "windows" && ee.ProcessState.Exited() {
		// On Unix, CommandContext kills the direct child (git) with SIGKILL,
		// so Exited()==false proves the subprocess was killed by ctx
		// cancellation; a normal exit would mean the fetch completed on its
		// own. On Windows the kill is TerminateProcess, whose exit status is
		// indistinguishable from a natural failure (and whose surviving
		// git-remote-https child reports the fetch error), so this check is
		// only meaningful where a signal kill is observable.
		t.Errorf("git subprocess exited normally (%v); expected it to be killed by ctx cancellation", ee.ProcessState)
	}
}
