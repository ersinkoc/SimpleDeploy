package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ersinkoc/SimpleDeploy/internal/wizard"
)

// sortedAppNames returns the app names in a state map in a stable order.
// Go randomises map iteration, so `list` and `status` reshuffled their output
// on every invocation — which makes the tool feel broken and makes diffing two
// runs impossible.
func sortedAppNames[T any](apps map[string]T) []string {
	names := make([]string, 0, len(apps))
	for name := range apps {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func RunList() error {
	s, err := stateLoad()
	if err != nil {
		return err
	}

	if len(s.Apps) == 0 {
		wizard.Info("No applications deployed yet.")
		wizard.Info("Run 'simpledeploy deploy' to deploy your first application.")
		return nil
	}

	wizard.Header("Applications")
	fmt.Println()

	for _, name := range sortedAppNames(s.Apps) {
		app := s.Apps[name]
		statusIcon := "●"
		switch app.Status {
		case "running":
			fmt.Printf("  %s \033[32m%s\033[0m\n", statusIcon, name)
		case "stopped":
			fmt.Printf("  %s \033[31m%s\033[0m\n", statusIcon, name)
		default:
			fmt.Printf("  %s \033[33m%s\033[0m\n", statusIcon, name)
		}
		fmt.Printf("    Domain:   %s\n", app.Domain)
		fmt.Printf("    Type:     %s\n", app.Type)
		fmt.Printf("    Image:    %s\n", app.CurrentImage)
		fmt.Printf("    Port:     %d\n", app.Port)
		if len(app.Databases) > 0 {
			fmt.Printf("    DB:       %s\n", strings.Join(app.Databases, ", "))
		}
		fmt.Printf("    Webhook:  %v\n", app.WebhookEnabled)
		fmt.Printf("    Deploys:  %d\n", app.DeployCount)
		fmt.Printf("    Last:     %s\n", app.LastDeploy)
		fmt.Println()
	}

	return nil
}
