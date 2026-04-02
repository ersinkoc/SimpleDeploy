package cli

import (
	"fmt"
	"strings"

	"github.com/ersinkoc/SimpleDeploy/internal/docker"
	"github.com/ersinkoc/SimpleDeploy/internal/state"
	"github.com/ersinkoc/SimpleDeploy/internal/wizard"
)

func RunStatus() error {
	cfg, err := state.GetConfig()
	if err != nil {
		return err
	}

	s, err := stateLoad()
	if err != nil {
		return err
	}

	wizard.Header("SimpleDeploy Status")
	fmt.Println()

	fmt.Printf("  Proxy:      %s\n", cfg.Proxy)
	fmt.Printf("  Base Domain: %s\n", cfg.BaseDomain)
	fmt.Printf("  ACME Email:  %s\n", cfg.AcmeEmail)
	fmt.Printf("  Webhook:     :%d\n", cfg.WebhookPort)
	fmt.Println()

	// Proxy status
	proxyContainer := "qd-traefik"
	if cfg.Proxy == "caddy" {
		proxyContainer = "qd-caddy"
	}
	proxyStatus, err := docker.ContainerStatus(proxyContainer)
	if err != nil {
		wizard.Warn(fmt.Sprintf("Failed to get proxy status: %v", err))
		proxyStatus = "unknown"
	}
	fmt.Printf("  Proxy:       %s (%s)\n", proxyContainer, coloredStatus(proxyStatus))
	fmt.Println()

	// Apps
	if len(s.Apps) == 0 {
		wizard.Info("No applications deployed.")
		return nil
	}

	fmt.Printf("  Applications (%d):\n", len(s.Apps))
	for name, app := range s.Apps {
		containerName := docker.ContainerName(name)
		status, err := docker.ContainerStatus(containerName)
		if err != nil {
			wizard.Warn(fmt.Sprintf("Failed to get status for %s: %v", name, err))
			status = "unknown"
		}
		fmt.Printf("    %-20s %s  %s\n", name, coloredStatus(status), app.Domain)
	}

	return nil
}

func coloredStatus(status string) string {
	switch strings.ToLower(status) {
	case "running":
		return wizard.Green("running")
	case "stopped", "exited":
		return wizard.Red("stopped")
	case "not found":
		return wizard.Yellow("not found")
	case "unknown":
		return wizard.Yellow("unknown")
	default:
		return wizard.Yellow(status)
	}
}
