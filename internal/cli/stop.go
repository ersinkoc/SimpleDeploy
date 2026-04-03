package cli

import (
	"fmt"

	"github.com/ersinkoc/SimpleDeploy/internal/docker"
	"github.com/ersinkoc/SimpleDeploy/internal/wizard"
)

func RunStop(args []string) error {
	appName, err := appNameFromArgs(args)
	if err != nil {
		return err
	}

	app, err := stateGetApp(appName)
	if err != nil {
		return fmt.Errorf("app %q not found", appName)
	}

	containerName := docker.ContainerName(appName)
	wizard.Info(fmt.Sprintf("Stopping %s...", appName))
	if err := dockerStopContainer(containerName); err != nil {
		return fmt.Errorf("failed to stop %s: %w", appName, err)
	}
	wizard.Success(fmt.Sprintf("App %s stopped", appName))

	app.Status = "stopped"
	if err := stateSaveApp(app); err != nil {
		wizard.Warn("Failed to update app status: " + err.Error())
	}
	return nil
}
