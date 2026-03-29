package cli

import (
	"fmt"
	"strconv"

	"github.com/ersinkoc/SimpleDeploy/internal/docker"
	"github.com/ersinkoc/SimpleDeploy/internal/proxy"
	"github.com/ersinkoc/SimpleDeploy/internal/state"
	"github.com/ersinkoc/SimpleDeploy/internal/wizard"
)

func RunInit() error {
	wizard.Header("SimpleDeploy Setup")

	// 1. Docker check
	wizard.Info("Checking Docker installation...")
	if err := docker.EnsureDocker(); err != nil {
		return err
	}

	// Check if already initialized
	if state.IsInitialized() {
		if !wizard.Confirm("SimpleDeploy is already initialized. Reconfigure?", false) {
			return nil
		}
	}

	// 2. Reverse proxy selection
	fmt.Println()
	proxyChoice := wizard.Choose("Select reverse proxy:", []string{
		"Traefik (recommended, auto-discovery)",
		"Caddy (simple, auto-SSL)",
	}, 1)

	var proxyType string
	switch proxyChoice {
	case 1:
		proxyType = "traefik"
	case 2:
		proxyType = "caddy"
	}

	// 3. Domain
	fmt.Println()
	baseDomain := wizard.AskRequired("Base domain (e.g.: apps.example.com)")

	if !wizard.Confirm(
		fmt.Sprintf("Wildcard DNS configured? (*.%s → this server)", baseDomain), true) {
		wizard.Warn("Without wildcard DNS, you'll need to manually configure DNS for each app")
	}

	// 4. SSL
	fmt.Println()
	acmeEmail := wizard.AskRequired("Let's Encrypt email address")

	// 5. Webhook secret
	fmt.Println()
	var webhookSecret string
	if wizard.Confirm("Auto-generate webhook secret?", true) {
		secret, err := state.GenerateSecret("whk_", 32)
		if err != nil {
			return fmt.Errorf("failed to generate secret: %w", err)
		}
		webhookSecret = secret
		wizard.Success("Webhook secret: " + webhookSecret)
		wizard.Info("Store this secret securely — it won't be shown again")
	} else {
		webhookSecret = wizard.AskRequired("Enter webhook secret")
	}

	webhookPort := 9000
	portStr := wizard.Ask("Webhook port", "9000")
	if portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			webhookPort = p
		} else {
			wizard.Warn("Invalid port number, using default 9000")
		}
	}

	// Save config
	cfg := &state.GlobalConfig{
		BaseDomain:    baseDomain,
		Proxy:         proxyType,
		AcmeEmail:     acmeEmail,
		WebhookPort:   webhookPort,
		WebhookSecret: webhookSecret,
	}

	if err := state.SaveConfig(cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	// 6. Setup reverse proxy
	fmt.Println()
	if proxyType == "traefik" {
		if err := proxy.SetupTraefik(acmeEmail); err != nil {
			return fmt.Errorf("failed to setup Traefik: %w", err)
		}
	} else {
		if err := proxy.SetupCaddy(acmeEmail); err != nil {
			return fmt.Errorf("failed to setup Caddy: %w", err)
		}
	}

	// Summary
	fmt.Println()
	wizard.Success("SimpleDeploy initialized successfully!")
	fmt.Printf("  Config:  %s\n", getStateDir())
	fmt.Printf("  Proxy:   %s\n", proxyType)
	fmt.Printf("  Domain:  %s\n", baseDomain)
	fmt.Printf("  Webhook: :%d\n", webhookPort)
	fmt.Println()
	fmt.Println("Run 'simpledeploy deploy' to deploy your first application.")

	return nil
}

func getStateDir() string {
	home := homeDir()
	return home + "/.simpledeploy"
}
