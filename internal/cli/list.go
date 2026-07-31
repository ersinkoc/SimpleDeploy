package cli

import (
	"encoding/json"
	"fmt"
	"os"
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

// listAppJSON is the machine-readable representation of one app in `list --json`.
// It mirrors the fields shown in the human format, with stable JSON keys.
type listAppJSON struct {
	Name      string   `json:"name"`
	Domain    string   `json:"domain"`
	Type      string   `json:"type"`
	Image     string   `json:"image"`
	Port      int      `json:"port"`
	Databases []string `json:"databases,omitempty"`
	Webhook   bool     `json:"webhook"`
	Deploys   int      `json:"deploys"`
	Last      string   `json:"last_deploy"`
	Status    string   `json:"status"`
}

func RunList(args []string) error {
	jsonOutput := false
	for _, arg := range args {
		switch arg {
		case "--json":
			jsonOutput = true
		default:
			return fmt.Errorf("unknown option %q. Usage: simpledeploy list [--json]", arg)
		}
	}

	s, err := stateLoad()
	if err != nil {
		return err
	}

	if jsonOutput {
		apps := make([]listAppJSON, 0, len(s.Apps))
		for _, name := range sortedAppNames(s.Apps) {
			app := s.Apps[name]
			apps = append(apps, listAppJSON{
				Name:      app.Name,
				Domain:    app.Domain,
				Type:      app.Type,
				Image:     app.CurrentImage,
				Port:      app.Port,
				Databases: app.Databases,
				Webhook:   app.WebhookEnabled,
				Deploys:   app.DeployCount,
				Last:      app.LastDeploy,
				Status:    app.Status,
			})
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(apps)
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
