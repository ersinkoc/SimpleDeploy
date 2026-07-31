package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/ersinkoc/SimpleDeploy/internal/docker"
	"github.com/ersinkoc/SimpleDeploy/internal/state"
	"github.com/ersinkoc/SimpleDeploy/internal/wizard"
)

// statusJSON is the machine-readable representation of `status --json`.
type statusJSON struct {
	Proxy          string          `json:"proxy"`
	BaseDomain     string          `json:"base_domain"`
	AcmeEmail      string          `json:"acme_email"`
	Webhook        int             `json:"webhook_port"`
	ProxyContainer string          `json:"proxy_container"`
	ProxyStatus    string          `json:"proxy_status"`
	Apps           []statusAppJSON `json:"apps"`
}

type statusAppJSON struct {
	Name   string `json:"name"`
	Domain string `json:"domain"`
	Status string `json:"container_status"`
}

func RunStatus(args []string) error {
	jsonOutput := false
	for _, arg := range args {
		switch arg {
		case "--json":
			jsonOutput = true
		default:
			return fmt.Errorf("unknown option %q. Usage: simpledeploy status [--json]", arg)
		}
	}

	cfg, err := state.GetConfig()
	if err != nil {
		return err
	}

	s, err := stateLoad()
	if err != nil {
		return err
	}

	proxyContainer := "qd-traefik"
	if cfg.Proxy == "caddy" {
		proxyContainer = "qd-caddy"
	}
	proxyStatus, err := docker.ContainerStatus(context.Background(), proxyContainer)
	if err != nil {
		proxyStatus = "unknown"
	}

	if jsonOutput {
		out := statusJSON{
			Proxy:          cfg.Proxy,
			BaseDomain:     cfg.BaseDomain,
			AcmeEmail:      cfg.AcmeEmail,
			Webhook:        cfg.WebhookPort,
			ProxyContainer: proxyContainer,
			ProxyStatus:    proxyStatus,
			Apps:           make([]statusAppJSON, 0, len(s.Apps)),
		}
		for _, name := range sortedAppNames(s.Apps) {
			app := s.Apps[name]
			containerName := docker.ContainerName(name)
			status, err := docker.ContainerStatus(context.Background(), containerName)
			if err != nil {
				status = "unknown"
			}
			out.Apps = append(out.Apps, statusAppJSON{
				Name:   name,
				Domain: app.Domain,
				Status: status,
			})
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	wizard.Header("SimpleDeploy Status")
	fmt.Println()

	fmt.Printf("  Proxy:      %s\n", cfg.Proxy)
	fmt.Printf("  Base Domain: %s\n", cfg.BaseDomain)
	fmt.Printf("  ACME Email:  %s\n", cfg.AcmeEmail)
	fmt.Printf("  Webhook:     :%d\n", cfg.WebhookPort)
	fmt.Println()

	fmt.Printf("  Proxy:       %s (%s)\n", proxyContainer, coloredStatus(proxyStatus))
	fmt.Println()

	if len(s.Apps) == 0 {
		wizard.Info("No applications deployed.")
		return nil
	}

	fmt.Printf("  Applications (%d):\n", len(s.Apps))
	for _, name := range sortedAppNames(s.Apps) {
		app := s.Apps[name]
		containerName := docker.ContainerName(name)
		status, err := docker.ContainerStatus(context.Background(), containerName)
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
