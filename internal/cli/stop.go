package cli

import (
	"context"
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

	// Existence first, lock second: a typo'd app name must answer "not found"
	// rather than whatever the lock has to say about a directory it would have
	// to create.
	if _, err := stateGetApp(appName); err != nil {
		return fmt.Errorf("app %q not found", appName)
	}

	// Take the deploy lock. UpdateApp alone was not enough: a redeploy running
	// concurrently would `docker compose up` the container back up — defeating
	// the stop outright — and then write Status:"running" from its own
	// observation, so the operator's stop vanished with no error anywhere.
	// Refusing while a deploy is in flight is the honest outcome.
	releaseLock, err := acquireDeployLock(appName)
	if err != nil {
		return err
	}
	defer releaseLock()

	containerName := docker.ContainerName(appName)
	wizard.Info(fmt.Sprintf("Stopping %s...", appName))
	if err := dockerStopContainer(context.Background(), containerName); err != nil {
		return fmt.Errorf("failed to stop %s: %w", appName, err)
	}
	wizard.Success(fmt.Sprintf("App %s stopped", appName))

	// UpdateApp rather than writing back the record read above: this command
	// only owns the Status field, and a concurrent redeploy must not have its
	// CurrentImage/DeployCount reverted by our stale copy.
	if err := stateUpdateApp(appName, func(cur *state.AppConfig) {
		cur.Status = "stopped"
	}); err != nil {
		wizard.Warn("Failed to update app status: " + err.Error())
	}
	return nil
}
