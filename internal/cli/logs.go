package cli

import (
	"context"
	"fmt"

	cfgpkg "github.com/ersinkoc/SimpleDeploy/internal/config"
)

func RunLogs(args []string) error {
	appName, err := appNameFromArgs(args)
	if err != nil {
		return err
	}

	if _, err := stateGetApp(appName); err != nil {
		return err
	}

	appDir := cfgpkg.AppDir(appName)

	if err := dockerComposeLogs(context.Background(), appDir, appName, true); err != nil {
		return fmt.Errorf("failed to get logs: %w", err)
	}

	return nil
}
