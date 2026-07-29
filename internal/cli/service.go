package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/ersinkoc/SimpleDeploy/internal/runner"
	"github.com/ersinkoc/SimpleDeploy/internal/state"
	"github.com/ersinkoc/SimpleDeploy/internal/wizard"
)

func RunService(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: simpledeploy service <install|start|stop>")
	}

	action := strings.ToLower(args[0])
	switch action {
	case "install":
		return runServiceInstall()
	case "start":
		return runServiceStart()
	case "stop":
		return runServiceStop()
	default:
		return fmt.Errorf("unknown service action: %s (install|start|stop)", action)
	}
}

func runServiceInstall() error {
	cfg, err := state.GetConfig()
	if err != nil {
		return err
	}
	return runner.InstallService(cfg.BaseDomain, cfg.WebhookPort)
}

func runServiceStart() error {
	wizard.Info("Starting SimpleDeploy service...")
	return runner.StartService()
}

func runServiceStop() error {
	wizard.Info("Stopping SimpleDeploy service...")
	return runner.StopService()
}

func RunWebhook(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: simpledeploy webhook start [--port PORT]")
	}

	if strings.ToLower(args[0]) != "start" {
		return fmt.Errorf("usage: simpledeploy webhook start [--port PORT]")
	}

	cfg, err := state.GetConfig()
	if err != nil {
		return err
	}

	// Fail fast rather than starting a listener that will reject every push.
	// The server refuses unauthenticated deploys per-request as well, but an
	// operator is far more likely to notice this at startup than to discover
	// it from a 401 in their provider's webhook delivery log.
	if cfg.WebhookSecret == "" {
		return fmt.Errorf("no webhook secret configured; re-run 'simpledeploy init' to set one")
	}

	port := cfg.WebhookPort
	for i := 1; i < len(args); i++ {
		if args[i] == "--port" && i+1 < len(args) {
			p, err := strconv.Atoi(args[i+1])
			if err != nil {
				return fmt.Errorf("invalid --port value %q: %w", args[i+1], err)
			}
			if p < 1 || p > 65535 {
				return fmt.Errorf("invalid --port value %d (must be 1-65535)", p)
			}
			port = p
		}
	}

	srv := webhookNewServer(port, cfg.WebhookSecret)
	srv.SetDeployHandler(func(ctx context.Context, appName string) error {
		return RunRedeployContext(ctx, []string{appName})
	})

	wizard.Info(fmt.Sprintf("Starting webhook server on :%d", port))
	return srv.Start()
}
