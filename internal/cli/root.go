package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/ersinkoc/SimpleDeploy/internal/state"
)

const version = "0.0.3"

func PrintUsage() {
	fmt.Printf("SimpleDeploy v%s — Single-Binary PaaS CLI\n", version)
	fmt.Println("Author: Ersin KOC — https://x.com/ersinkoc")
	fmt.Println()
	fmt.Println("Usage: simpledeploy <command> [arguments]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  init                First-time setup (interactive wizard)")
	fmt.Println("  deploy              Deploy a new application (interactive)")
	fmt.Println("  list                List deployed applications")
	fmt.Println("  redeploy <app>      Redeploy an application")
	fmt.Println("  remove <app>        Remove an application")
	fmt.Println("  restart <app>       Restart an application")
	fmt.Println("  stop <app>          Stop an application")
	fmt.Println("  exec <app> <cmd>    Execute command in app container")
	fmt.Println("  logs <app>          Show application logs")
	fmt.Println("  status              Show SimpleDeploy status")
	fmt.Println("  service <action>    Manage SimpleDeploy service (install|start|stop)")
	fmt.Println("  webhook start       Start webhook server")
	fmt.Println("  version             Show version")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  simpledeploy init")
	fmt.Println("  simpledeploy deploy")
	fmt.Println("  simpledeploy redeploy myapp")
	fmt.Println("  simpledeploy logs myapp")
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
		return RunList()
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
		return RunStatus()
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
	home, err := os.UserHomeDir()
	if err != nil {
		return "/root"
	}
	return home
}
