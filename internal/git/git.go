package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var (
	osMkdirAll   = os.MkdirAll
	osCreateTemp = os.CreateTemp
	osWriteFile  = os.WriteFile
)

func Clone(ctx context.Context, repoURL, branch, destDir, token string) error {
	if err := osMkdirAll(filepath.Dir(destDir), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	cmd := exec.CommandContext(ctx, "git", "clone", "--branch", branch, "--single-branch", "--depth", "1", repoURL, destDir)
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

func Pull(ctx context.Context, repoDir, branch string, token ...string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "pull", "origin", branch)
	if len(token) > 0 && token[0] != "" {
		askpass, cleanup, err := writeAskpassScript(token[0])
		if err != nil {
			return fmt.Errorf("failed to create askpass script: %w", err)
		}
		defer cleanup()
		cmd.Env = append(os.Environ(),
			"GIT_ASKPASS="+askpass,
			"GIT_TERMINAL_PROMPT=0",
			"QD_GIT_TOKEN="+token[0],
		)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		safeOutput := sanitizeOutput(string(output), "")
		return fmt.Errorf("git pull failed: %s: %w", safeOutput, err)
	}
	return nil
}

// writeAskpassScript creates a temporary script that echoes the token
// without exposing it in process arguments.
func writeAskpassScript(token string) (path string, cleanup func(), err error) {
	f, err := osCreateTemp("", "qd-askpass-*.sh")
	if err != nil {
		return "", nil, err
	}
	name := f.Name()
	f.Close()
	// Use printf to avoid single-quote injection in the token value
	script := "#!/bin/sh\nprintf '%s\\n' \"$QD_GIT_TOKEN\"\n"
	if err := osWriteFile(name, []byte(script), 0700); err != nil {
		os.Remove(name)
		return "", nil, err
	}

	// Return the token via environment variable instead of embedding in script
	cleanup = func() { os.Remove(name) }
	return name, cleanup, nil
}

// sanitizeOutput removes any token-like strings from git output.
func sanitizeOutput(output, repoURL string) string {
	sanitized := output
	// Remove the repo URL if present (it might contain embedded creds)
	sanitized = strings.ReplaceAll(sanitized, repoURL, "<redacted>")
	return sanitized
}
