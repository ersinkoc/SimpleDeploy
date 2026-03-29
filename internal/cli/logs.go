package cli

import (
	cfgpkg "github.com/ersinkoc/SimpleDeploy/internal/config"
	"github.com/ersinkoc/SimpleDeploy/internal/docker"
	"github.com/ersinkoc/SimpleDeploy/internal/state"
	"github.com/ersinkoc/SimpleDeploy/internal/wizard"
)

func RunLogs(args []string) error {
	appName, err := appNameFromArgs(args)
	if err != nil {
		return err
	}

	if _, err := state.GetApp(appName); err != nil {
		return err
	}

	appDir := cfgpkg.AppDir(appName)

	if err := docker.ComposeLogs(appDir, appName, true); err != nil {
		wizard.Warn("Failed to get logs: " + err.Error())
	}

	return nil
}
