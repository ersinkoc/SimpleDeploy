package cli

import (
	"context"
	"fmt"
	"strconv"

	"github.com/ersinkoc/SimpleDeploy/internal/state"
	"github.com/ersinkoc/SimpleDeploy/internal/wizard"
)

func RunInit() error {
	wizard.Header("SimpleDeploy Setup")

	// 1. Docker check
	wizard.Info("Checking Docker installation...")
	if err := dockerEnsureDocker(context.Background()); err != nil {
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
	if err := state.ValidateBaseDomain(baseDomain); err != nil {
		return fmt.Errorf("invalid base domain: %w", err)
	}

	if !wizard.Confirm(
		fmt.Sprintf("Wildcard DNS configured? (*.%s → this server)", baseDomain), true) {
		wizard.Warn("Without wildcard DNS, you'll need to manually configure DNS for each app")
	}

	// 4. SSL
	fmt.Println()
	acmeEmail := wizard.AskRequired("Let's Encrypt email address")
	if err := state.ValidateEmail(acmeEmail); err != nil {
		return fmt.Errorf("invalid email: %w", err)
	}

	// 5. Webhook secret
	fmt.Println()
	var webhookSecret string
	if wizard.Confirm("Auto-generate webhook secret?", true) {
		secret, err := stateGenerateSecret("whk_", 32)
		if err != nil {
			return fmt.Errorf("failed to generate secret: %w", err)
		}
		webhookSecret = secret
		wizard.Success("Webhook secret: " + webhookSecret)
		wizard.Info("Paste this into your repository's webhook settings.")
		wizard.Info("You can see it again after a deploy, or in " + getStateDir() + "/state.json")
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

	if err := stateSaveConfig(cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	// 6. Setup reverse proxy
	fmt.Println()
	if proxyType == "traefik" {
		if err := proxySetupTraefik(context.Background(), acmeEmail); err != nil {
			return fmt.Errorf("failed to setup Traefik: %w", err)
		}
	} else {
		if err := proxySetupCaddy(context.Background(), acmeEmail); err != nil {
			return fmt.Errorf("failed to setup Caddy: %w", err)
		}
	}

	// 7. Publish the webhook endpoint through the proxy. Without this the
	// https://<domain>/_qd/webhook/<app> URL the deploy wizard hands out has
	// nothing serving it. Non-fatal: the rest of SimpleDeploy works fine
	// without push-to-deploy, so warn rather than abort a successful install.
	if err := proxySetupWebhookRoute(proxyType, baseDomain, webhookPort); err != nil {
		wizard.Warn("Failed to publish webhook route: " + err.Error())
		wizard.Warn("Push-to-deploy will not work until this is resolved.")
	} else {
		if proxyType == "caddy" {
			if err := proxyReloadCaddy(); err != nil {
				wizard.Warn("Failed to reload Caddy: " + err.Error())
			}
		} else {
			// Traefik's file provider watches /dynamic, so the route is picked
			// up without a restart — but the mount and the provider flag are
			// new if this is a re-init over an older install, and those only
			// take effect on recreate.
			if err := proxyRestartTraefik(); err != nil {
				wizard.Warn("Failed to restart Traefik: " + err.Error())
			}
		}
		wizard.Success(fmt.Sprintf("Webhook endpoint published at https://%s/_qd/", baseDomain))
		wizard.Info(fmt.Sprintf("Point an A record for %s at this server (in addition to the wildcard).", baseDomain))
	}

	// Summary
	fmt.Println()
	wizard.Success("SimpleDeploy initialized successfully!")
	fmt.Printf("  Config:  %s\n", getStateDir())
	fmt.Printf("  Proxy:   %s\n", proxyType)
	fmt.Printf("  Domain:  %s\n", baseDomain)
	fmt.Printf("  Webhook: :%d\n", webhookPort)
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. simpledeploy deploy         — deploy your first application")
	fmt.Println("  2. simpledeploy webhook start  — run the push-to-deploy listener")
	fmt.Println("     (or 'simpledeploy service install' to run it as a container)")

	return nil
}

func getStateDir() string {
	home := homeDir()
	return home + "/.simpledeploy"
}
