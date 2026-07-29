package proxy

import (
	"fmt"

	"github.com/ersinkoc/SimpleDeploy/internal/state"
)

// SetupWebhookRoute publishes the SimpleDeploy webhook server at
// https://<baseDomain>/_qd/* through whichever reverse proxy is configured.
//
// Why this exists: the deploy wizard has always told operators to point their
// repository's webhook at https://<baseDomain>/_qd/webhook/<app>, but nothing
// ever routed that path. Traefik's docker provider can only discover
// containers, and the webhook server is a host process; the Caddyfile got a
// block per app and never one for the base domain. The result was an
// advertised URL that returned 404 forever, so "push to deploy" — the feature
// the tool leads with — did not work out of the box under either proxy.
//
// Both implementations target host.docker.internal, which the generated proxy
// compose files map to the Docker bridge gateway via extra_hosts. The caller
// is responsible for reloading/restarting the proxy afterwards.
func SetupWebhookRoute(proxyType, baseDomain string, webhookPort int) error {
	// Defense-in-depth: baseDomain is interpolated into a Traefik Host(`...`)
	// rule and a Caddyfile site address, both unescaped.
	if err := state.ValidateBaseDomain(baseDomain); err != nil {
		return fmt.Errorf("invalid base domain: %w", err)
	}
	if webhookPort < 1 || webhookPort > 65535 {
		return fmt.Errorf("invalid webhook port %d (must be 1-65535)", webhookPort)
	}

	switch proxyType {
	case "traefik":
		return setupTraefikWebhookRoute(baseDomain, webhookPort)
	case "caddy":
		return setupCaddyWebhookRoute(baseDomain, webhookPort)
	default:
		return fmt.Errorf("unknown proxy type %q", proxyType)
	}
}
