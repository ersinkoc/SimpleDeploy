package proxy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These tests prove the routing chain SimpleDeploy generates is one a real
// proxy accepts and can actually follow. Everything else in the suite asserts
// on the TEXT we emit, which cannot tell a valid Caddyfile from an invalid one,
// nor a reachable upstream from a typo'd container name.
//
// TLS is deliberately out of scope: certificates need real DNS and a public
// ACME challenge, which no local run can provide. What is proven here is
// everything up to that point — the config parses, the site block matches the
// app's hostname, and the upstream address we emit resolves and serves.
//
//	SIMPLEDEPLOY_INTEGRATION=1 go test -p=1 -count=1 -run TestRouting ./internal/proxy/

const (
	routeApp       = "routeprobe"
	routeContainer = "qd-routeprobe"
	routeCaddy     = "qd-routetest"
	routeDomain    = "routeprobe.example.com"
	routeMarker    = "ROUTED_OK"
)

func dockerRun(t *testing.T, args ...string) (string, error) {
	t.Helper()
	out, err := exec.Command("docker", args...).CombinedOutput()
	return string(out), err
}

// startAppContainer runs a stand-in for a deployed app: it answers HTTP on 3000
// with routeMarker and joins the shared network under the exact container name
// AddCaddyApp points its reverse_proxy at.
func startAppContainer(t *testing.T) {
	t.Helper()
	_, _ = dockerRun(t, "rm", "-f", routeContainer)
	t.Cleanup(func() { _, _ = dockerRun(t, "rm", "-f", routeContainer) })

	body := routeMarker + "\n"
	script := "while true; do printf 'HTTP/1.1 200 OK\\r\\nContent-Length: " +
		itoaLen(body) + "\\r\\nConnection: close\\r\\n\\r\\n" + routeMarker + "\\n' | nc -l -p 3000; done"

	if out, err := dockerRun(t, "run", "-d", "--name", routeContainer,
		"--network", "simpledeploy", "alpine:3.21", "sh", "-c", script); err != nil {
		t.Fatalf("start app container: %v\n%s", err, out)
	}
}

func itoaLen(s string) string {
	n := len(s)
	digits := ""
	if n == 0 {
		return "0"
	}
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

// TestRoutingChain_CaddyAcceptsAndFollowsGeneratedConfig is the end-to-end
// proof: a real Caddy validates the file AddCaddyApp produced, then a running
// Caddy matches the app's hostname and can reach the upstream we told it about.
func TestRoutingChain_CaddyAcceptsAndFollowsGeneratedConfig(t *testing.T) {
	requireDocker(t)
	_, _ = dockerRun(t, "network", "create", "simpledeploy")

	dir := setupTestProxyDir(t)
	caddyfile := filepath.Join(dir, "Caddyfile")
	if err := os.WriteFile(caddyfile, []byte("{\n    email admin@example.com\n}\n"), 0644); err != nil {
		t.Fatalf("seed Caddyfile: %v", err)
	}

	// The same call a deploy makes, headers and all.
	headers := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "SAMEORIGIN",
	}
	if err := AddCaddyApp(routeApp, routeDomain, 3000, headers); err != nil {
		t.Fatalf("AddCaddyApp: %v", err)
	}
	// And the webhook route init publishes, so the file under test is the shape
	// a real install actually runs.
	if err := setupCaddyWebhookRoute("example.com", 9000); err != nil {
		t.Fatalf("setupCaddyWebhookRoute: %v", err)
	}

	// 1. Caddy itself must accept the file. A unit test comparing strings
	//    cannot distinguish a valid Caddyfile from one Caddy rejects.
	out, err := dockerRun(t, "run", "--rm", "-v", caddyfile+":/etc/caddy/Caddyfile:ro",
		"caddy:2-alpine", "caddy", "validate", "--adapter", "caddyfile", "--config", "/etc/caddy/Caddyfile")
	if err != nil {
		content, _ := os.ReadFile(caddyfile)
		t.Fatalf("real Caddy rejects the generated config: %v\n%s\ngenerated:\n%s", err, out, content)
	}

	// 2. Bring up the app and a real Caddy on the shared network. No host ports
	//    are published, so this cannot collide with anything on :80/:443.
	startAppContainer(t)
	_, _ = dockerRun(t, "rm", "-f", routeCaddy)
	t.Cleanup(func() { _, _ = dockerRun(t, "rm", "-f", routeCaddy) })
	if out, err := dockerRun(t, "run", "-d", "--name", routeCaddy, "--network", "simpledeploy",
		"-v", caddyfile+":/etc/caddy/Caddyfile:ro", "caddy:2-alpine"); err != nil {
		t.Fatalf("start caddy: %v\n%s", err, out)
	}

	// 3. The upstream address the Caddyfile names must resolve and serve. This
	//    is exactly the request `reverse_proxy qd-routeprobe:3000` performs, run
	//    from inside the proxy container.
	waitForOutput(t, 60*time.Second, "the app to answer from inside the proxy container", func() (string, bool) {
		out, err := dockerRun(t, "exec", routeCaddy, "wget", "-q", "-O", "-", "http://"+routeContainer+":3000/")
		return out, err == nil && strings.Contains(out, routeMarker)
	})

	// 4. Caddy must MATCH the app's hostname. Requested over plain HTTP, an
	//    automatic-HTTPS site answers a redirect — which only happens if the
	//    site block for this domain was parsed and matched. (Following it to a
	//    200 would need a real certificate, i.e. real DNS and ACME.)
	waitForOutput(t, 60*time.Second, "Caddy to match the app's hostname", func() (string, bool) {
		out, _ := dockerRun(t, "exec", routeCaddy, "wget", "-S", "--spider",
			"--header", "Host: "+routeDomain, "http://127.0.0.1/")
		return out, strings.Contains(out, "308") || strings.Contains(out, "301") ||
			strings.Contains(out, "Location: https://"+routeDomain)
	})

	// 5. A hostname we never configured must NOT be routed to the app.
	unknown, _ := dockerRun(t, "exec", routeCaddy, "wget", "-S", "--spider",
		"--header", "Host: not-ours.example.com", "http://127.0.0.1/")
	if strings.Contains(unknown, routeMarker) {
		t.Errorf("Caddy served the app for a hostname it was never configured for:\n%s", unknown)
	}
}

// TestRoutingChain_TraefikComposeIsValid proves the other proxy's generated
// artifacts are well-formed. Traefik has no offline validate command, so this
// checks what can be checked: the compose file parses and carries the pieces
// the routing depends on (the file provider that publishes the webhook route,
// and the host-gateway mapping the route points at).
func TestRoutingChain_TraefikComposeIsValid(t *testing.T) {
	requireCompose(t)

	dir := setupTestProxyDir(t)
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"),
		[]byte(generateTraefikCompose("admin@example.com")), 0644); err != nil {
		t.Fatalf("write compose: %v", err)
	}
	if err := setupTraefikWebhookRoute("example.com", 9000); err != nil {
		t.Fatalf("setupTraefikWebhookRoute: %v", err)
	}

	cmd := exec.Command("docker", "compose", "config")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated Traefik compose is not valid:\n%s", out)
	}
	normalised := string(out)
	for _, want := range []string{
		"--providers.file.directory=/dynamic",
		// Compose rewrites extra_hosts entries from "host:addr" to "host=addr"
		// in its normalised output, so assert the form it actually prints
		// rather than the form the generator writes.
		"host.docker.internal=host-gateway",
		"traefik:v3",
	} {
		if !strings.Contains(normalised, want) {
			t.Errorf("generated Traefik compose is missing %q:\n%s", want, normalised)
		}
	}

	// The dynamic route file must be YAML Traefik's file provider can read.
	routeFile := filepath.Join(dir, "dynamic", "webhook.yml")
	data, err := os.ReadFile(routeFile)
	if err != nil {
		t.Fatalf("read webhook route: %v", err)
	}
	for _, want := range []string{
		"rule: \"Host(`example.com`) && PathPrefix(`/_qd`)\"",
		"url: \"http://host.docker.internal:9000\"",
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("webhook route missing %q:\n%s", want, data)
		}
	}
}

// waitForOutput polls until cond holds, reporting the last output on failure.
func waitForOutput(t *testing.T, budget time.Duration, what string, cond func() (string, bool)) {
	t.Helper()
	deadline := time.Now().Add(budget)
	var last string
	for time.Now().Before(deadline) {
		out, ok := cond()
		if ok {
			return
		}
		last = out
		time.Sleep(time.Second)
	}
	t.Fatalf("timed out after %v waiting for %s; last output:\n%s", budget, what, last)
}
