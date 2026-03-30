package cli

import (
	"fmt"

	"github.com/ersinkoc/SimpleDeploy/internal/docker"
	"github.com/ersinkoc/SimpleDeploy/internal/state"
)

func RunExec(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: simpledeploy exec <app> <command> [args...]")
	}

	appName, err := appNameFromArgs(args[:1])
	if err != nil {
		return err
	}

	if _, err := state.GetApp(appName); err != nil {
		return fmt.Errorf("app %q not found", appName)
	}

	containerName := "qd-" + appName
	return docker.ExecContainer(containerName, args[1:]...)
}
