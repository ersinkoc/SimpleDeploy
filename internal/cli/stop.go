package cli

import (
	"fmt"

	"github.com/ersinkoc/SimpleDeploy/internal/docker"
	"github.com/ersinkoc/SimpleDeploy/internal/state"
	"github.com/ersinkoc/SimpleDeploy/internal/wizard"
)

func RunStop(args []string) error {
	appName, err := appNameFromArgs(args)
	if err != nil {
		return err
	}

	if _, err := state.GetApp(appName); err != nil {
		return fmt.Errorf("app %q not found", appName)
	}

	containerName := "qd-" + appName
	wizard.Info(fmt.Sprintf("Stopping %s...", appName))
	if err := docker.StopContainer(containerName); err != nil {
		return fmt.Errorf("failed to stop %s: %w", appName, err)
	}
	wizard.Success(fmt.Sprintf("App %s stopped", appName))

	app, err := state.GetApp(appName)
	if err != nil {
		return nil // container stopped, state update is best-effort
	}
	app.Status = "stopped"
	if err := state.SaveApp(app); err != nil {
		wizard.Warn("Failed to update app status: " + err.Error())
	}
	return nil
}
