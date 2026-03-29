package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	cfgpkg "github.com/ersinkoc/SimpleDeploy/internal/config"
	"github.com/ersinkoc/SimpleDeploy/internal/docker"
	"github.com/ersinkoc/SimpleDeploy/internal/git"
	"github.com/ersinkoc/SimpleDeploy/internal/proxy"
	"github.com/ersinkoc/SimpleDeploy/internal/state"
	"github.com/ersinkoc/SimpleDeploy/internal/wizard"
)

func RunRedeploy(args []string) error {
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
		decrypted, err := state.Decrypt(gitToken)
		if err == nil {
			gitToken = decrypted
		}
	}

	// Pull latest
	wizard.Info("Pulling latest changes...")
	if err := git.Pull(sourceDir, app.Branch); err != nil {
		return fmt.Errorf("git pull failed: %w", err)
	}
	wizard.Success("Repository updated")

	// Build new image
	wizard.Info("Building new image...")
	imageTag, err := docker.BuildImage(sourceDir, appName)
	if err != nil {
		return fmt.Errorf("build failed: %w", err)
	}
	wizard.Success("Image built: " + imageTag)

	// Update the image tag in the existing compose file
	composePath := filepath.Join(appDir, "docker-compose.yml")
	composeData, err := os.ReadFile(composePath)
	if err != nil {
		return fmt.Errorf("failed to read compose file: %w", err)
	}

	// Replace only the app service's image line (first occurrence under the app name)
	newCompose := replaceAppImage(string(composeData), appName, imageTag)
	if err := os.WriteFile(composePath, []byte(newCompose), 0644); err != nil {
		return fmt.Errorf("failed to update compose: %w", err)
	}

	// Restart
	wizard.Info("Restarting containers...")
	if err := docker.ComposeUp(appDir); err != nil {
		return fmt.Errorf("failed to restart: %w", err)
	}
	wizard.Success("Containers restarted")

	// Reload Caddy if applicable
	if cfg.Proxy == "caddy" {
		if err := proxy.ReloadCaddy(); err != nil {
			wizard.Warn("Failed to reload Caddy: " + err.Error())
		}
	}

	// Cleanup old images (keep last 3)
	go docker.CleanupOldImages(appName, 3)

	// Update state
	app.CurrentImage = imageTag
	app.Status = "running"
	app.LastDeploy = time.Now().UTC().Format(time.RFC3339)
	app.DeployCount++
	if err := state.SaveApp(app); err != nil {
		return err
	}

	logDeploy(appDir, appName, imageTag)

	wizard.Success(fmt.Sprintf("%s redeployed successfully!", appName))
	return nil
}

// replaceAppImage replaces only the app service's image line in compose content.
// It targets the line under the app name service block, not database image lines.
func replaceAppImage(content, appName, newImage string) string {
	lines := strings.Split(content, "\n")
	inAppService := false
	appServicePrefix := "  " + appName + ":"

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Detect start of app service block
		if trimmed == appServicePrefix || line == appServicePrefix {
			inAppService = true
			continue
		}

		// If inside app service, replace the first "image:" line
		if inAppService {
			if strings.HasPrefix(trimmed, "image:") {
				// Preserve indentation
				indent := line[:len(line)-len(trimmed)]
				lines[i] = indent + "image: " + newImage
				break // Only replace the first match in the app service
			}
			// If we hit another top-level key, we've left the app service
			if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") && trimmed != "" {
				break
			}
		}
	}

	return strings.Join(lines, "\n")
}
