package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	cfgpkg "github.com/ersinkoc/SimpleDeploy/internal/config"
	"github.com/ersinkoc/SimpleDeploy/internal/docker"
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

	// Existence first, lock second: an unknown app name must answer "not found"
	// rather than whatever the lock has to say. The record read here is only
	// used for values that do not change under us (repo, branch, domain); the
	// deploy's own result is written back through UpdateApp, which re-reads
	// under the state lock and fails if the app was removed meanwhile.
	app, err := state.GetApp(appName)
	if err != nil {
		return err
	}

	cfg, err := state.GetConfig()
	if err != nil {
		return err
	}

	// Serialize deploys of this app ACROSS PROCESSES before touching anything.
	// The webhook server's in-memory lock does not cover a CLI redeploy run by
	// hand, and the two racing corrupted each other: parallel `git pull` of the
	// same source tree, a rollback that reverted the peer's successful deploy,
	// and an image prune that deleted the image the peer had just built.
	releaseLock, err := acquireDeployLock(appName)
	if err != nil {
		return err
	}
	defer releaseLock()

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
	previousCompose := string(composeData)
	newCompose := replaceAppImage(previousCompose, appName, imageTag)
	// 0600, matching compose.WriteCompose: the file embeds database root
	// passwords in the db services' environment blocks. os.WriteFile only
	// applies perm when it creates the file, so on an existing deployment this
	// is a no-op — but if the compose file was ever recreated (restored from a
	// backup, or a future code path), 0644 would have silently published those
	// credentials to every user on the host.
	if err := osWriteFile(composePath, []byte(newCompose), 0600); err != nil {
		return fmt.Errorf("failed to update compose: %w", err)
	}

	// rollbackCompose restores the previous image reference. Called on any
	// failure after the compose file has been rewritten, so a failed redeploy
	// leaves the app pointing at the image that was last known to work rather
	// than at one that will not start.
	rollbackCompose := func() {
		if err := osWriteFile(composePath, []byte(previousCompose), 0600); err != nil {
			wizard.Warn("Failed to restore previous compose file: " + err.Error())
			return
		}
		if err := dockerComposeUp(context.Background(), appDir); err != nil {
			wizard.Warn("Rollback failed to restart previous version: " + err.Error())
			return
		}
		wizard.Info("Rolled back to previous image " + app.CurrentImage)
	}

	if err := ctx.Err(); err != nil {
		rollbackCompose()
		return fmt.Errorf("redeploy cancelled before compose up: %w", err)
	}

	// Restart
	wizard.Info("Restarting containers...")
	if err := dockerComposeUp(ctx, appDir); err != nil {
		rollbackCompose()
		return fmt.Errorf("failed to restart: %w", err)
	}

	// Verify the new container actually came up. Previously the status was set
	// to "running" unconditionally, so a redeploy that shipped a crashing image
	// reported success and `simpledeploy list` kept claiming the app was
	// healthy — worst of all on the webhook path, which runs unattended.
	//
	// Only a definitive failure triggers rollback. If the state cannot be read
	// at all (Docker unreachable) or the container is simply absent, we warn
	// and record "error" instead: rolling back on an inconclusive reading
	// risks tearing down a deployment that is actually fine.
	containerName := docker.ContainerName(appName)
	status := waitForContainer(context.Background(), containerName, containerStartTimeout)
	confirmedRunning := status == "running"
	switch {
	case confirmedRunning:
		wizard.Success("Containers restarted")
	case isFailedContainerState(status):
		rollbackCompose()
		return fmt.Errorf("new container %s is %q after deploy; rolled back to %s — check 'simpledeploy logs %s'",
			containerName, status, app.CurrentImage, appName)
	default:
		wizard.Warn(fmt.Sprintf("Could not confirm %s is running (state: %q). Check 'simpledeploy logs %s'.",
			containerName, status, appName))
	}

	// Re-assert the app's Caddy route, then reload. Not fatal — the container
	// is already up, and a proxy problem must not undo a successful build.
	//
	// AddCaddyApp is called here, not just ReloadCaddy, because the route can
	// legitimately be missing at this point: deploy only WARNS when AddCaddyApp
	// fails, so an app can be recorded as running with no Caddyfile block at
	// all, and the Caddyfile is a plain text file an operator may have edited or
	// restored. Reloading alone would then re-read a config that still does not
	// route the app, leaving it unreachable with no error anywhere. AddCaddyApp
	// strips an existing block for the same domain before appending, so calling
	// it on every redeploy is idempotent.
	if cfg.Proxy == "caddy" {
		if err := proxyAddCaddyApp(app.Name, app.Domain, app.Port, app.Headers); err != nil {
			wizard.Warn("Failed to update Caddyfile: " + err.Error())
		}
		if err := proxyReloadCaddy(); err != nil {
			wizard.Warn("Failed to reload Caddy: " + err.Error())
		}
	}

	// Update state. Derived from this run's observation, not from the stored
	// value — an app previously marked "error" must be able to recover to
	// "running", and one we could not verify must not be claimed healthy.
	//
	// UpdateApp, not SaveApp: `app` was read at the top of this function and a
	// deploy spends minutes in git pull + docker build, so writing the whole
	// stale record back would clobber anything that changed meanwhile. Two
	// real failures came from that — a `simpledeploy remove` issued during a
	// webhook redeploy was undone when this save re-inserted the deleted app,
	// and a concurrent `stop` had its status silently overwritten. UpdateApp
	// re-reads the record under the lock and errors if the app is gone, so a
	// removed app stays removed.
	newStatus := "error"
	if confirmedRunning {
		newStatus = "running"
	}
	lastDeploy := time.Now().UTC().Format(time.RFC3339)
	if err := stateUpdateApp(appName, func(cur *state.AppConfig) {
		cur.CurrentImage = imageTag
		cur.Status = newStatus
		cur.LastDeploy = lastDeploy
		cur.DeployCount++
	}); err != nil {
		return fmt.Errorf("failed to record deploy in state: %w", err)
	}

	logDeploy(appDir, appName, imageTag)

	// Prune old images LAST and synchronously. This used to run in a
	// fire-and-forget goroutine, which in a CLI invocation almost never got
	// scheduled before the process exited — so images accumulated forever and
	// eventually filled the disk. It is cheap (one `docker images` plus a few
	// `docker rmi`) and failures here are cosmetic.
	pruneImages(appName, keepImages)

	wizard.Success(fmt.Sprintf("%s redeployed successfully!", appName))
	return nil
}

// keepImages is how many previously-built images per app survive a prune.
// Enough to roll back by hand a couple of times without unbounded growth.
const keepImages = 3

// pruneImages removes an app's older images, never failing the caller.
// Disk hygiene must not be able to fail a deploy that has already succeeded,
// so both errors and panics from the docker layer are downgraded to warnings.
func pruneImages(appName string, keep int) {
	defer func() {
		if r := recover(); r != nil {
			wizard.Warn(fmt.Sprintf("Image cleanup for %s panicked: %v", appName, r))
		}
	}()
	if err := dockerCleanupOldImages(context.Background(), appName, keep); err != nil {
		wizard.Warn("Failed to prune old images: " + err.Error())
	}
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
