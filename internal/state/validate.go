package state

import (
	"fmt"
	"regexp"
	"strings"
)

// AppNameRegex matches valid application names.
// Exported for use by other packages.
var AppNameRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}[a-z0-9]$`)

// dnsLabelRegex matches a single DNS label per RFC 1123: 1-63 chars, lowercase
// letters/digits/hyphens, must start and end with alphanumeric.
var dnsLabelRegex = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

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

// ValidateBaseDomain checks that a domain is a syntactically valid hostname.
// This is critical because the base domain is interpolated unescaped into
// Traefik label rules (Host(`...`)) and Caddy site blocks; a malicious value
// containing backticks, parentheses, or whitespace could inject extra
// routing rules. Accepts only lowercase RFC 1123 hostnames with at least
// two labels (e.g. "apps.example.com").
func ValidateBaseDomain(domain string) error {
	if domain == "" {
		return fmt.Errorf("base domain cannot be empty")
	}
	if len(domain) > 253 {
		return fmt.Errorf("base domain must be at most 253 characters")
	}
	if strings.ContainsAny(domain, " \t\r\n`'\"()[]{}<>,;|&$\\") {
		return fmt.Errorf("base domain contains invalid characters")
	}
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return fmt.Errorf("base domain must have at least two labels (e.g. 'apps.example.com')")
	}
	for _, label := range labels {
		if !dnsLabelRegex.MatchString(label) {
			return fmt.Errorf("invalid DNS label %q in base domain: each label must be 1-63 chars, lowercase letters/digits/hyphens, starting and ending with alphanumeric", label)
		}
	}
	return nil
}
