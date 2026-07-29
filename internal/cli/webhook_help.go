package cli

import (
	"fmt"

	"github.com/ersinkoc/SimpleDeploy/internal/state"
	"github.com/ersinkoc/SimpleDeploy/internal/wizard"
)

// WebhookURL returns the public endpoint a repository should POST push events
// to for the given app. It mirrors the route published by
// proxy.SetupWebhookRoute during `init` — keep the two in sync.
func WebhookURL(baseDomain, appName string) string {
	return fmt.Sprintf("https://%s/_qd/webhook/%s", baseDomain, appName)
}

// printWebhookHelp prints everything needed to finish wiring push-to-deploy,
// including the piece that used to be missing entirely: the listener has to be
// running. `init` publishes the proxy route, but the route points at a host
// process that only exists while `simpledeploy webhook start` (or the
// containerised service) is up. An operator who followed the old instructions
// got a URL that returned 502 with no explanation of why.
func printWebhookHelp(cfg *state.GlobalConfig, appName, branch string) {
	fmt.Println()
	wizard.Header("Push-to-deploy setup")
	fmt.Println()
	fmt.Printf("  1. Add a webhook to your repository:\n")
	fmt.Printf("       Payload URL:  %s\n", WebhookURL(cfg.BaseDomain, appName))
	fmt.Printf("       Content type: application/json\n")
	fmt.Printf("       Secret:       %s\n", cfg.WebhookSecret)
	fmt.Printf("       Events:       push only (branch: %s)\n", branch)
	fmt.Println()
	fmt.Printf("  2. Make sure the listener is running on this server:\n")
	fmt.Printf("       simpledeploy webhook start\n")
	fmt.Printf("     Or run it as a managed container:\n")
	fmt.Printf("       simpledeploy service install && simpledeploy service start\n")
	fmt.Println()
	fmt.Printf("  Supported providers: GitHub and Gitea (HMAC-SHA256), GitLab (token).\n")
}
