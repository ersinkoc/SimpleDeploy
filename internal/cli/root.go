package cli

import (
	"fmt"
	"os"
	"strings"
)

const (
	version = "0.0.1"
	banner  = `
  ____ _           __ _ _
 / ___| | ___  ___/ _(_) | ___
| (___| |/ _ \/ __| |_| | |/ _ \
 \___ \ |  __/ (__|  _| | |  __/
 |____/_|\___|\___|_| |_|_|\___|
`
)

func PrintUsage() {
	fmt.Print(banner)
	fmt.Println()
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
	return args[0], nil
}

func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/root"
	}
	return home
}
