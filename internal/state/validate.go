package state

import (
	"fmt"
	"regexp"
)

// AppNameRegex matches valid application names.
// Exported for use by other packages.
var AppNameRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}[a-z0-9]$`)

// ValidateAppName checks that an application name is safe to use in
// file paths, Docker container names, DNS labels, and git URLs.
func ValidateAppName(name string) error {
	if name == "" {
		return fmt.Errorf("application name cannot be empty")
	}
	if len(name) < 2 {
		return fmt.Errorf("application name must be at least 2 characters")
	}
	if len(name) > 64 {
		return fmt.Errorf("application name must be at most 64 characters")
	}
	if !AppNameRegex.MatchString(name) {
		return fmt.Errorf("application name must contain only lowercase letters, digits, and hyphens (e.g. 'my-app-123')")
	}
	return nil
}
