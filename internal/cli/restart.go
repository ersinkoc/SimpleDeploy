package cli

import (
	"fmt"

	"github.com/ersinkoc/SimpleDeploy/internal/wizard"
)

func RunRestart(args []string) error {
	appName, err := appNameFromArgs(args)
	if err != nil {
		return err
	}

	if _, err := stateGetApp(appName); err != nil {
		return fmt.Errorf("app %q not found", appName)
	}

	containerName := "qd-" + appName
	wizard.Info(fmt.Sprintf("Restarting %s...", appName))
	if err := dockerRestartContainer(containerName); err != nil {
		return fmt.Errorf("failed to restart %s: %w", appName, err)
	}
	wizard.Success(fmt.Sprintf("App %s restarted", appName))

	app, err := stateGetApp(appName)
	if err != nil {
		return nil // container restarted, state update is best-effort
	}
	app.Status = "running"
	if err := stateSaveApp(app); err != nil {
		wizard.Warn("Failed to update app status: " + err.Error())
	}
	return nil
}
