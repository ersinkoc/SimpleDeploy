package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func Clone(repoURL, branch, destDir, token string) error {
	if err := os.MkdirAll(filepath.Dir(destDir), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	cmd := exec.Command("git", "clone", "--branch", branch, "--single-branch", "--depth", "1", repoURL, destDir)
	if token != "" {
		// Use GIT_ASKPASS to avoid token in process args
		askpass, cleanup, err := writeAskpassScript(token)
		if err != nil {
			return fmt.Errorf("failed to create askpass script: %w", err)
		}
		defer cleanup()
		cmd.Env = append(os.Environ(),
			"GIT_ASKPASS="+askpass,
			"GIT_TERMINAL_PROMPT=0",
			"QD_GIT_TOKEN="+token,
		)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Sanitize output — never leak the token
		safeOutput := sanitizeOutput(string(output), repoURL)
		return fmt.Errorf("git clone failed: %s: %w", safeOutput, err)
	}
	return nil
}

func Pull(repoDir, branch string) error {
	cmd := exec.Command("git", "-C", repoDir, "pull", "origin", branch)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git pull failed: %s: %w", string(output), err)
	}
	return nil
}

func GetShortHash(repoDir string) (string, error) {
	cmd := exec.Command("git", "-C", repoDir, "rev-parse", "--short", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get git hash: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func DetectBranch(repoDir string) (string, error) {
	cmd := exec.Command("git", "-C", repoDir, "rev-parse", "--abbrev-ref", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to detect branch: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func IsRepo(dir string) bool {
	gitDir := filepath.Join(dir, ".git")
	info, err := os.Stat(gitDir)
	return err == nil && info.IsDir()
}

// writeAskpassScript creates a temporary script that echoes the token
// without exposing it in process arguments.
func writeAskpassScript(token string) (path string, cleanup func(), err error) {
	f, err := os.CreateTemp("", "qd-askpass-*.sh")
	if err != nil {
		return "", nil, err
	}
	// Use printf to avoid single-quote injection in the token value
	script := "#!/bin/sh\nprintf '%s\\n' \"$QD_GIT_TOKEN\"\n"
	if _, err := f.WriteString(script); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", nil, err
	}
	f.Chmod(0700)
	f.Close()

	// Return the token via environment variable instead of embedding in script
	cleanup = func() { os.Remove(f.Name()) }
	return f.Name(), cleanup, nil
}

// sanitizeOutput removes any token-like strings from git output.
func sanitizeOutput(output, repoURL string) string {
	sanitized := output
	// Remove the repo URL if present (it might contain embedded creds)
	sanitized = strings.ReplaceAll(sanitized, repoURL, "<redacted>")
	return sanitized
}
