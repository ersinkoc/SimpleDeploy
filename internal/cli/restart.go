package cli

import (
	"context"
	"fmt"

	"github.com/ersinkoc/SimpleDeploy/internal/docker"
	"github.com/ersinkoc/SimpleDeploy/internal/state"
	"github.com/ersinkoc/SimpleDeploy/internal/wizard"
)

func RunRestart(args []string) error {
	appName, err := appNameFromArgs(args)
	if err != nil {
		return err
	}

	// Same reasoning as stop: a concurrent redeploy would recreate the
	// container and overwrite the status this command records.
	releaseLock, err := acquireDeployLock(appName)
	if err != nil {
		return err
	}
	defer releaseLock()

	if _, err := stateGetApp(appName); err != nil {
		return fmt.Errorf("app %q not found", appName)
	}

	containerName := docker.ContainerName(appName)
	wizard.Info(fmt.Sprintf("Restarting %s...", appName))
	if err := dockerRestartContainer(context.Background(), containerName); err != nil {
		return fmt.Errorf("failed to restart %s: %w", appName, err)
	}

	// `docker restart` returning 0 only means the daemon started the
	// container, not that it stayed up — an image that crashes on boot is put
	// straight back into the restart loop. Confirm it the same way deploy does
	// before recording "running", so `list` does not claim a crash-looping app
	// is healthy.
	status := waitForContainer(context.Background(), containerName, containerStartTimeout)
	if status == "running" {
		wizard.Success(fmt.Sprintf("App %s restarted", appName))
	} else {
		wizard.Warn(fmt.Sprintf("%s is %q after restart. Check 'simpledeploy logs %s'.", containerName, status, appName))
		status = "error"
	}

	if err := stateUpdateApp(appName, func(cur *state.AppConfig) {
		cur.Status = status
	}); err != nil {
		wizard.Warn("Failed to update app status: " + err.Error())
	}
	return nil
}
