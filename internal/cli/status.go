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

	s, err := state.Load()
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
	proxyStatus, _ := docker.ContainerStatus(proxyContainer)
	fmt.Printf("  Proxy:       %s (%s)\n", proxyContainer, coloredStatus(proxyStatus))
	fmt.Println()

	// Apps
	if len(s.Apps) == 0 {
		wizard.Info("No applications deployed.")
		return nil
	}

	fmt.Printf("  Applications (%d):\n", len(s.Apps))
	for name, app := range s.Apps {
		containerName := fmt.Sprintf("qd-%s", name)
		status, _ := docker.ContainerStatus(containerName)
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
	default:
		return wizard.Yellow(status)
	}
}
