package cli

import (
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

	port := cfg.WebhookPort
	for i := 1; i < len(args); i++ {
		if args[i] == "--port" && i+1 < len(args) {
			p, err := strconv.Atoi(args[i+1])
			if err == nil {
				port = p
			}
		}
	}

	srv := webhookNewServer(port, cfg.WebhookSecret)
	srv.SetDeployHandler(func(appName string) error {
		return RunRedeploy([]string{appName})
	})

	wizard.Info(fmt.Sprintf("Starting webhook server on :%d", port))
	return srv.Start()
}
