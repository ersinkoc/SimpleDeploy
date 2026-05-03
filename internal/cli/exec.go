package cli

import (
	"context"
	"fmt"

	"github.com/ersinkoc/SimpleDeploy/internal/docker"
)

func RunExec(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: simpledeploy exec <app> <command> [args...]")
	}

	appName, err := appNameFromArgs(args[:1])
	if err != nil {
		return err
	}

	if _, err := stateGetApp(appName); err != nil {
		return fmt.Errorf("app %q not found", appName)
	}

	containerName := docker.ContainerName(appName)
	return dockerExecContainer(context.Background(), containerName, args[1:]...)
}
