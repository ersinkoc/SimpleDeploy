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

// tokenEnv builds the environment used to feed a credential to git without
// ever putting it on the command line (where it would be visible in `ps`).
func tokenEnv(askpass, token string) []string {
	return append(os.Environ(),
		"GIT_ASKPASS="+askpass,
		"GIT_TERMINAL_PROMPT=0",
		"QD_GIT_TOKEN="+token,
	)
}

func Clone(ctx context.Context, repoURL, branch, destDir, token string) error {
	if err := osMkdirAll(filepath.Dir(destDir), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// git clone refuses to write into an existing non-empty directory. A
	// leftover tree from an interrupted deploy would therefore make every
	// subsequent attempt fail with "destination path already exists" and no
	// way forward short of manual cleanup. Clone's contract is "produce a
	// fresh checkout here", so clear the path first.
	if _, err := os.Stat(destDir); err == nil {
		if err := os.RemoveAll(destDir); err != nil {
			return fmt.Errorf("failed to clear existing source directory: %w", err)
		}
	}

	cmd := exec.CommandContext(ctx, "git", "clone", "--branch", branch, "--single-branch", "--depth", "1", repoURL, destDir)
	if token != "" {
		// Use GIT_ASKPASS to avoid token in process args
		askpass, cleanup, err := writeAskpassScript(token)
		if err != nil {
			return fmt.Errorf("failed to create askpass script: %w", err)
		}
		defer cleanup()
		cmd.Env = tokenEnv(askpass, token)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Sanitize output — never leak the URL (which may carry embedded
		// credentials) or the token itself.
		safeOutput := redactSecrets(sanitizeOutput(string(output), repoURL), token)
		return fmt.Errorf("git clone failed: %s: %w", safeOutput, err)
	}
	return nil
}

// Pull brings an existing checkout up to date with the remote branch.
//
// It fetches and hard-resets rather than running `git pull`. Two reasons:
//
//   - Clone uses --depth 1, and `git pull` against a shallow clone fails or
//     silently un-shallows depending on the git version.
//   - `git pull` is a merge. Any local divergence — a file the build process
//     modified in place, a conflicting force-push upstream — leaves the
//     working tree wedged, and since SimpleDeploy never resolves conflicts
//     that app could never deploy again. The source directory is a disposable
//     mirror of the remote, so "make it identical to origin/<branch>" is both
//     the safe operation and the one the operator expects.
//
// Untracked files survive the reset, which matters: a generated Dockerfile and
// .dockerignore live in this directory and must not be discarded.
func Pull(ctx context.Context, repoDir, branch string, token ...string) error {
	authToken := ""
	if len(token) > 0 {
		authToken = token[0]
	}

	var env []string
	if authToken != "" {
		askpass, cleanup, err := writeAskpassScript(authToken)
		if err != nil {
			return fmt.Errorf("failed to create askpass script: %w", err)
		}
		defer cleanup()
		env = tokenEnv(askpass, authToken)
	}

	run := func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repoDir}, args...)...)
		if env != nil {
			cmd.Env = env
		}
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	// No --depth here even though Clone is shallow: git refuses or ignores
	// shallow fetches over the local (non-file://) transport depending on
	// version, and a plain fetch into an already-shallow repository keeps it
	// shallow anyway. Correctness over a marginal transfer saving.
	if output, err := run("fetch", "origin", branch); err != nil {
		return fmt.Errorf("git fetch failed: %s: %w", redactSecrets(output, authToken), err)
	}
	if output, err := run("reset", "--hard", "FETCH_HEAD"); err != nil {
		return fmt.Errorf("git reset failed: %s: %w", redactSecrets(output, authToken), err)
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

// sanitizeOutput replaces the repository URL — which may carry embedded
// credentials — with a placeholder.
//
// The empty-repoURL guard is not cosmetic: strings.ReplaceAll with an empty
// needle inserts the replacement between every character of the input, so the
// previous unguarded call in Pull turned every error message into
// "<redacted>f<redacted>a<redacted>t<redacted>a<redacted>l..." — unreadable,
// and it hid the actual git error from the operator.
func sanitizeOutput(output, repoURL string) string {
	if repoURL == "" {
		return output
	}
	return strings.ReplaceAll(output, repoURL, "<redacted>")
}

// redactSecrets removes literal secret values from text bound for an error
// message or a log. git echoes the remote URL in several of its own messages,
// and with a credential helper in play that URL can include the token, so
// redacting the URL alone was not enough.
func redactSecrets(output string, secrets ...string) string {
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		output = strings.ReplaceAll(output, secret, "<redacted>")
	}
	return output
}
