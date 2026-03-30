package cli

import (
	"fmt"
	"os"

	cfgpkg "github.com/ersinkoc/SimpleDeploy/internal/config"
	"github.com/ersinkoc/SimpleDeploy/internal/docker"
	"github.com/ersinkoc/SimpleDeploy/internal/proxy"
	"github.com/ersinkoc/SimpleDeploy/internal/state"
	"github.com/ersinkoc/SimpleDeploy/internal/wizard"
)

func RunRemove(args []string) error {
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
	if err := docker.ComposeRemove(appDir, removeVolumes); err != nil {
		wizard.Warn("Failed to remove containers: " + err.Error())
	}

	// Proxy cleanup
	if cfg != nil && cfg.Proxy == "caddy" {
		wizard.Info("Removing from Caddyfile...")
		if err := proxy.RemoveCaddyApp(app.Domain); err != nil {
			wizard.Warn("Failed to remove from Caddyfile: " + err.Error())
		}
		if err := proxy.ReloadCaddy(); err != nil {
			wizard.Warn("Failed to reload Caddy: " + err.Error())
		}
	}

	// Remove app directory
	wizard.Info("Removing application files...")
	if err := os.RemoveAll(appDir); err != nil {
		wizard.Warn("Failed to remove app directory: " + err.Error())
	}

	// Remove from state
	if err := state.RemoveApp(appName); err != nil {
		return fmt.Errorf("failed to remove app from state: %w", err)
	}

	// Cleanup images
	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintf(os.Stderr, "warning: image cleanup panicked: %v\n", r)
			}
		}()
		docker.CleanupOldImages(appName, 0)
	}()

	wizard.Success(fmt.Sprintf("Application '%s' removed", appName))
	return nil
}
