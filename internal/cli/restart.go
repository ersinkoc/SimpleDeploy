package cli

import (
	"fmt"

	"github.com/ersinkoc/SimpleDeploy/internal/docker"
	"github.com/ersinkoc/SimpleDeploy/internal/wizard"
)

func RunRestart(args []string) error {
	appName, err := appNameFromArgs(args)
	if err != nil {
		return err
	}

	app, err := stateGetApp(appName)
	if err != nil {
		return fmt.Errorf("app %q not found", appName)
	}

	containerName := docker.ContainerName(appName)
	wizard.Info(fmt.Sprintf("Restarting %s...", appName))
	if err := dockerRestartContainer(containerName); err != nil {
		return fmt.Errorf("failed to restart %s: %w", appName, err)
	}
	wizard.Success(fmt.Sprintf("App %s restarted", appName))

	app.Status = "running"
	if err := stateSaveApp(app); err != nil {
		wizard.Warn("Failed to update app status: " + err.Error())
	}
	return nil
}
