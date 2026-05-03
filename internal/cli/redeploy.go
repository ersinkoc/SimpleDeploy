package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	cfgpkg "github.com/ersinkoc/SimpleDeploy/internal/config"
	"github.com/ersinkoc/SimpleDeploy/internal/state"
	"github.com/ersinkoc/SimpleDeploy/internal/wizard"
)

// RunRedeploy is the CLI-facing entry point. It wraps RunRedeployContext
// with a never-cancelling context.Background() so existing callers
// (root.go's command dispatch, helpers_test.go) keep working unchanged.
func RunRedeploy(args []string) error {
	return RunRedeployContext(context.Background(), args)
}

// RunRedeployContext is the ctx-aware variant used by the webhook server,
// which arms a per-deploy timeout (deployTimeout, default 30 min) and
// shuts the rate-limiter down on shutdown. ctx is checked at coarse
// boundaries between major steps; if cancelled, the function returns
// without continuing. The long-running subprocess steps themselves
// (gitPull, dockerBuildImage, dockerComposeUp) currently use their own
// internal timeouts and do NOT honor caller ctx — fully cancellable
// deploys would require threading ctx into git.Pull, docker.BuildImage,
// docker.ComposeUp, which is intentionally deferred.
func RunRedeployContext(ctx context.Context, args []string) error {
	appName, err := appNameFromArgs(args)
	if err != nil {
		return err
	}

	app, err := state.GetApp(appName)
	if err != nil {
		return err
	}

	cfg, err := state.GetConfig()
	if err != nil {
		return err
	}

	wizard.Info(fmt.Sprintf("Redeploying %s...", appName))

	appDir := cfgpkg.AppDir(appName)
	sourceDir := filepath.Join(appDir, "source")

	// Decrypt git token
	gitToken := app.GitToken
	if gitToken != "" {
		decrypted, err := stateDecrypt(gitToken)
		if err != nil {
			wizard.Warn("Failed to decrypt git token: " + err.Error())
			gitToken = ""
		} else {
			gitToken = decrypted
		}
	}

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("redeploy cancelled before git pull: %w", err)
	}

	// Pull latest
	wizard.Info("Pulling latest changes...")
	if err := gitPull(ctx, sourceDir, app.Branch, gitToken); err != nil {
		return fmt.Errorf("git pull failed: %w", err)
	}
	wizard.Success("Repository updated")

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("redeploy cancelled before build: %w", err)
	}

	// Build new image
	wizard.Info("Building new image...")
	imageTag, err := dockerBuildImage(ctx, sourceDir, appName)
	if err != nil {
		return fmt.Errorf("build failed: %w", err)
	}
	wizard.Success("Image built: " + imageTag)

	// Update the image tag in the existing compose file
	composePath := filepath.Join(appDir, "docker-compose.yml")
	composeData, err := osReadFile(composePath)
	if err != nil {
		return fmt.Errorf("failed to read compose file: %w", err)
	}

	// Replace only the app service's image line (first occurrence under the app name)
	newCompose := replaceAppImage(string(composeData), appName, imageTag)
	if err := osWriteFile(composePath, []byte(newCompose), 0644); err != nil {
		return fmt.Errorf("failed to update compose: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("redeploy cancelled before compose up: %w", err)
	}

	// Restart
	wizard.Info("Restarting containers...")
	if err := dockerComposeUp(ctx, appDir); err != nil {
		return fmt.Errorf("failed to restart: %w", err)
	}
	wizard.Success("Containers restarted")

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("redeploy cancelled before caddy reload: %w", err)
	}

	// Reload Caddy if applicable
	if cfg.Proxy == "caddy" {
		if err := proxyReloadCaddy(); err != nil {
			wizard.Warn("Failed to reload Caddy: " + err.Error())
		}
	}

	// Cleanup old images (keep last 3) in the background so a slow docker
	// listing doesn't delay redeploy completion. The goroutine is wrapped
	// in a recover so a panic in image listing doesn't crash the CLI.
	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintf(os.Stderr, "warning: image cleanup panicked: %v\n", r)
			}
		}()
		dockerCleanupOldImages(ctx, appName, 3)
	}()

	// Update state
	app.CurrentImage = imageTag
	app.Status = "running"
	app.LastDeploy = time.Now().UTC().Format(time.RFC3339)
	app.DeployCount++
	if err := stateSaveApp(app); err != nil {
		return err
	}

	logDeploy(appDir, appName, imageTag)

	wizard.Success(fmt.Sprintf("%s redeployed successfully!", appName))
	return nil
}

// replaceAppImage replaces only the app service's image line in compose content.
// It targets the line under the app name service block, not database image lines.
//
// The match is robust against arbitrary indentation widths (2, 4, or tab) and
// trailing whitespace on the service header line. The previous version compared
// against a hard-coded "  appname:" string, which silently failed if the
// generator's indentation ever changed.
func replaceAppImage(content, appName, newImage string) string {
	lines := strings.Split(content, "\n")
	header := appName + ":"
	inAppService := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Detect start of app service block: a line that, when trimmed,
		// equals "<appName>:" AND was indented (so it's a service entry,
		// not a top-level key like "services:").
		if !inAppService {
			if trimmed == header && len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
				inAppService = true
			}
			continue
		}

		// Inside the app service: replace the first "image:" line we see.
		if strings.HasPrefix(trimmed, "image:") {
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			lines[i] = indent + "image: " + newImage
			break
		}

		// If we hit another top-level key (zero indent, non-empty), we've
		// left the service block without finding an image line. Stop.
		if trimmed != "" && line[0] != ' ' && line[0] != '\t' {
			break
		}
	}

	return strings.Join(lines, "\n")
}
