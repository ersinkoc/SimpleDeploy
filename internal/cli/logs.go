package cli

import (
	"context"
	"fmt"

	cfgpkg "github.com/ersinkoc/SimpleDeploy/internal/config"
)

// RunLogs shows an application's container logs.
//
// Defaults to a one-shot dump rather than following. The previous behaviour
// hard-coded follow mode, so `simpledeploy logs myapp` never returned — it
// blocked until interrupted, which makes it unusable from a script, from CI,
// or from anything reading the output programmatically. Following is still
// available, now explicitly, via -f/--follow.
func RunLogs(args []string) error {
	appName, err := appNameFromArgs(args)
	if err != nil {
		return err
	}

	if _, err := stateGetApp(appName); err != nil {
		return err
	}

	follow := false
	for _, arg := range args[1:] {
		switch arg {
		case "-f", "--follow":
			follow = true
		default:
			return fmt.Errorf("unknown option %q. Usage: simpledeploy logs <app> [-f]", arg)
		}
	}

	appDir := cfgpkg.AppDir(appName)

	if err := dockerComposeLogs(context.Background(), appDir, appName, follow); err != nil {
		return fmt.Errorf("failed to get logs: %w", err)
	}

	return nil
}
