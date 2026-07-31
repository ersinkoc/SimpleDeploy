package cli

import (
	"fmt"
	"strings"

	"github.com/ersinkoc/SimpleDeploy/internal/state"
)

// version is overridable at link time so the binary, the Makefile, and the
// release workflow cannot drift apart:
//
//	go build -ldflags "-X github.com/ersinkoc/SimpleDeploy/internal/cli.version=1.2.3"
//
// The literal below is the fallback for a plain `go build`.
var version = "0.1.0"

// Version returns the running binary's version string.
func Version() string { return version }

func PrintUsage() {
	fmt.Printf("SimpleDeploy v%s — Single-Binary PaaS CLI\n", version)
	fmt.Println("Author: Ersin KOC — https://x.com/ersinkoc")
	fmt.Println()
	fmt.Println("Usage: simpledeploy <command> [arguments]")
	fmt.Println()
	fmt.Println("Setup:")
	fmt.Println("  init                First-time setup (interactive wizard)")
	fmt.Println("  status [--json]     Show proxy and application status (--json for machine-readable output)")
	fmt.Println()
	fmt.Println("Applications:")
	fmt.Println("  deploy              Deploy a new application (interactive)")
	fmt.Println("  list [--json]       List deployed applications (--json for machine-readable output)")
	fmt.Println("  redeploy <app>      Rebuild and restart an application")
	fmt.Println("  restart <app>       Restart an application's container")
	fmt.Println("  stop <app>          Stop an application")
	fmt.Println("  remove <app>        Remove an application and its files")
	fmt.Println("  logs <app> [-f]     Show application logs (-f to follow)")
	fmt.Println("  exec <app> <cmd>    Run a command inside the app container")
	fmt.Println()
	fmt.Println("Push-to-deploy:")
	fmt.Println("  webhook start       Run the webhook listener in the foreground")
	fmt.Println("  service install     Generate compose to run the listener as a container")
	fmt.Println("  service start|stop  Start or stop that container")
	fmt.Println()
	fmt.Println("Other:")
	fmt.Println("  version             Show version")
	fmt.Println("  help                Show this message")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  simpledeploy init")
	fmt.Println("  simpledeploy deploy")
	fmt.Println("  simpledeploy redeploy myapp")
	fmt.Println("  simpledeploy logs myapp -f")
}

func Route(args []string) error {
	if len(args) == 0 {
		PrintUsage()
		return nil
	}

	command := args[0]
	cmdArgs := args[1:]

	switch strings.ToLower(command) {
	case "init":
		return RunInit()
	case "deploy":
		return RunDeploy()
	case "list", "ls":
		return RunList(cmdArgs)
	case "redeploy":
		return RunRedeploy(cmdArgs)
	case "remove", "rm":
		return RunRemove(cmdArgs)
	case "restart":
		return RunRestart(cmdArgs)
	case "stop":
		return RunStop(cmdArgs)
	case "exec":
		return RunExec(cmdArgs)
	case "logs":
		return RunLogs(cmdArgs)
	case "status":
		return RunStatus(cmdArgs)
	case "service":
		return RunService(cmdArgs)
	case "webhook":
		return RunWebhook(cmdArgs)
	case "version", "-v", "--version":
		fmt.Printf("SimpleDeploy v%s\n", version)
		return nil
	case "help", "-h", "--help":
		PrintUsage()
		return nil
	default:
		return fmt.Errorf("unknown command: %s. Run 'simpledeploy help' for usage", command)
	}
}

func appNameFromArgs(args []string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("application name required. Usage: simpledeploy <command> <app-name>")
	}
	name := args[0]
	if err := state.ValidateAppName(name); err != nil {
		return "", err
	}
	return name, nil
}

func homeDir() string {
	home, err := osUserHomeDir()
	if err != nil {
		return "/root"
	}
	return home
}
