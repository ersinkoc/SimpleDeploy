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

// ValidateSubdomain checks that a subdomain is a single safe DNS label.
// The subdomain is concatenated with the base domain and used as a per-app
// hostname; like the base domain it ends up inside Traefik Host(`...`) rules
// and Caddy site blocks, so it must be free of metacharacters that could
// inject additional routing config.
func ValidateSubdomain(sub string) error {
	if sub == "" {
		return fmt.Errorf("subdomain cannot be empty")
	}
	if len(sub) > 63 {
		return fmt.Errorf("subdomain must be at most 63 characters")
	}
	if strings.ContainsAny(sub, " \t\r\n`'\"()[]{}<>,;|&$\\.") {
		return fmt.Errorf("subdomain contains invalid characters")
	}
	if !dnsLabelRegex.MatchString(sub) {
		return fmt.Errorf("invalid subdomain %q: must be 1-63 chars, lowercase letters/digits/hyphens, starting and ending with alphanumeric", sub)
	}
	return nil
}

// ValidateAppDomain validates a fully-qualified per-app hostname (subdomain +
// base domain). Defense-in-depth check used at compose-generation time: a
// state file written before the validators existed, or one tampered with on
// disk, could still slip an unsafe value past the input layer.
func ValidateAppDomain(domain string) error {
	if domain == "" {
		return fmt.Errorf("app domain cannot be empty")
	}
	return ValidateBaseDomain(domain)
}

// emailRegex is intentionally narrow: most @ left-hand-side punctuation is
// rejected because the email value lands in YAML and Caddyfile contexts. We
// accept the common subset (alphanumerics, dot, underscore, hyphen, plus)
// rather than the full RFC 5322 grammar.
var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)

// ValidateEmail checks that an address is safe to embed in Caddyfile global
// blocks ("email %s") and Traefik compose YAML
// ("--certificatesresolvers.letsencrypt.acme.email=%s"). A newline or
// backtick in the raw value would let an attacker append directives, so we
// reject any whitespace/metacharacters explicitly in addition to the regex.
func ValidateEmail(email string) error {
	if email == "" {
		return fmt.Errorf("email cannot be empty")
	}
	if len(email) > 254 {
		return fmt.Errorf("email must be at most 254 characters")
	}
	if strings.ContainsAny(email, " \t\r\n`'\"()[]{}<>,;|&$\\") {
		return fmt.Errorf("email contains invalid characters")
	}
	if !emailRegex.MatchString(email) {
		return fmt.Errorf("invalid email address %q", email)
	}
	return nil
}
