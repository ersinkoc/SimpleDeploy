package cli

import (
	"context"
	"fmt"

	cfgpkg "github.com/ersinkoc/SimpleDeploy/internal/config"
	"github.com/ersinkoc/SimpleDeploy/internal/wizard"
)

func RunRemove(args []string) error {
	appName, err := appNameFromArgs(args)
	if err != nil {
		return err
	}

	// Existence first, lock second — see stop.go.
	app, err := stateGetApp(appName)
	if err != nil {
		return err
	}

	// Take the same lock a deploy holds: tearing containers down and deleting
	// the app directory while a deploy of that app is mid-build leaves the
	// deploy writing into a directory that is being removed under it.
	releaseLock, err := acquireDeployLock(appName)
	if err != nil {
		return err
	}
	defer releaseLock()

	cfg, err := stateGetConfig()
	if err != nil {
		wizard.Warn("Could not load config: " + err.Error())
	}

	fmt.Printf("Application: %s\n", wizard.Bold(app.Name))
	fmt.Printf("  Domain: %s\n", app.Domain)
	fmt.Printf("  Image:  %s\n", app.CurrentImage)
	if len(app.Databases) > 0 {
		fmt.Printf("  DBs:    %v\n", app.Databases)
	}
	fmt.Println()

	removeVolumes := false
	if len(app.Databases) > 0 {
		removeVolumes = wizard.Confirm("Remove database volumes? (WARNING: all data will be lost)", false)
	}

	if !wizard.Confirm(fmt.Sprintf("Remove %s?", app.Name), false) {
		wizard.Info("Cancelled.")
		return nil
	}

	appDir := cfgpkg.AppDir(appName)

	// Stop and remove containers
	wizard.Info("Stopping containers...")
	if err := dockerComposeRemove(context.Background(), appDir, removeVolumes); err != nil {
		wizard.Warn("Failed to remove containers: " + err.Error())
	}

	// Proxy cleanup
	if cfg != nil && cfg.Proxy == "caddy" {
		wizard.Info("Removing from Caddyfile...")
		if err := proxyRemoveCaddyApp(app.Domain); err != nil {
			wizard.Warn("Failed to remove from Caddyfile: " + err.Error())
		}
		if err := proxyReloadCaddy(); err != nil {
			wizard.Warn("Failed to reload Caddy: " + err.Error())
		}
	}

	// Remove app directory
	wizard.Info("Removing application files...")
	if err := osRemoveAll(appDir); err != nil {
		wizard.Warn("Failed to remove app directory: " + err.Error())
	}

	// Remove from state
	if err := stateRemoveApp(appName); err != nil {
		return fmt.Errorf("failed to remove app from state: %w", err)
	}

	// Remove this app's images. Done synchronously: the previous background
	// goroutine was launched microseconds before the CLI process exited, so it
	// effectively never ran and every removed app left its images behind.
	// pruneImages downgrades errors and panics to warnings — the app is
	// already gone from state, so leftover images must not fail the command.
	pruneImages(appName, 0)

	wizard.Success(fmt.Sprintf("Application '%s' removed", appName))
	return nil
}
