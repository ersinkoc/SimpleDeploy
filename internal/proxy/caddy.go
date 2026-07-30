package proxy

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ersinkoc/SimpleDeploy/internal/docker"
	"github.com/ersinkoc/SimpleDeploy/internal/state"
	"github.com/ersinkoc/SimpleDeploy/internal/wizard"
)

// proxyExecTimeout bounds the time we spend waiting for `docker compose
// down` / `docker exec ... caddy reload` / `docker compose restart` style
// commands. A wedged Docker daemon would otherwise hang the operator's
// CLI session indefinitely; 30 s matches the timeouts used in
// internal/docker/runner.go for read-mostly inspect/list operations.
const proxyExecTimeout = 30 * time.Second

// proxySetupTimeout is a more generous bound for the one-shot `docker
// compose up -d` invocation during SetupCaddy / SetupTraefik. The first
// run pulls the proxy image (caddy:2-alpine ~30 MB, traefik:v3 ~100 MB)
// over the network, which on a slow link can legitimately take a minute
// or two. 5 min still bounds a truly wedged daemon while not aborting a
// healthy first install on a slow connection.
const proxySetupTimeout = 5 * time.Minute

type commandRunner interface {
	SetDir(string)
	SetStdout(io.Writer)
	SetStderr(io.Writer)
	Run() error
}

type execWrapper struct {
	*exec.Cmd
}

func (e *execWrapper) SetDir(dir string)     { e.Dir = dir }
func (e *execWrapper) SetStdout(w io.Writer) { e.Stdout = w }
func (e *execWrapper) SetStderr(w io.Writer) { e.Stderr = w }

var (
	osMkdirAll          = os.MkdirAll
	osWriteFile         = os.WriteFile
	dockerCreateNetwork = docker.CreateNetwork
	execCommand         = func(ctx context.Context, name string, arg ...string) commandRunner {
		return &execWrapper{exec.CommandContext(ctx, name, arg...)}
	}
)

// writeCaddyfile overwrites the Caddyfile IN PLACE, deliberately preserving
// the file's inode.
//
// It must not write-to-temp-then-rename, which is what it used to do. The
// generated Caddy compose bind-mounts the Caddyfile as a SINGLE FILE
// (./Caddyfile:/etc/caddy/Caddyfile). Docker resolves such a mount to an inode
// once, at container creation. A rename replaces the directory entry with a new
// inode, so the running container stays bound to the old one — which no longer
// has a name — and never sees another byte we write.
//
// The effect was that Caddy mode did not route anything at all. `init` wrote the
// global block, started the container, and then the very first atomic write
// (the /_qd webhook route) orphaned the mount. Every AddCaddyApp after that was
// invisible: the host Caddyfile grew correct site blocks while the container
// kept reading the original 3-line stub, so `caddy reload` reported
// "config is unchanged" and succeeded, Caddy had no site blocks and therefore
// did not even listen on :80/:443, and `deploy` still printed
// "https://<app> is ready!". Confirmed by inode comparison: host 20 lines,
// container 3 lines, different inodes.
//
// Losing rename atomicity is acceptable here, and much cheaper than the bug it
// caused: nothing reads this file except `caddy reload`, which we invoke
// ourselves after the write returns, and Caddy validates the config while
// adapting it — a truncated or malformed Caddyfile makes the reload fail and
// leaves the previously loaded config running, which the callers already
// surface as a warning.
//
// Sync before returning so the bytes are on disk before we ask Caddy to
// re-read them.
func writeCaddyfile(path string, data []byte, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// sortedHeaderKeys returns a header map's keys in lexical order so Caddyfile
// emission is a pure function of its input. Mirrors compose.sortedKeys.
func sortedHeaderKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// filterCaddyDomain returns the input Caddyfile content with the block for
// `domain` removed. Used by both RemoveCaddyApp (to delete a block) and
// AddCaddyApp (to dedupe — calling AddCaddyApp twice for the same domain
// must not produce two ambiguous routing blocks).
func filterCaddyDomain(content, domain string) string {
	lines := strings.Split(content, "\n")
	result := make([]string, 0, len(lines))
	skip := false
	depth := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !skip && (trimmed == domain+" {" || trimmed == domain+"{") {
			skip = true
			depth = 1
			continue
		}
		if skip {
			depth += strings.Count(line, "{") - strings.Count(line, "}")
			if depth <= 0 {
				skip = false
			}
			continue
		}
		result = append(result, line)
	}
	return strings.Join(result, "\n")
}

func SetupCaddy(ctx context.Context, acmeEmail string) error {
	// Defense-in-depth: init.go validates this at the input layer, but the
	// email is interpolated raw into the Caddyfile global block below, so
	// re-check here. A newline-bearing value would let an attacker append
	// directives.
	if err := state.ValidateEmail(acmeEmail); err != nil {
		return fmt.Errorf("invalid acme email: %w", err)
	}

	wizard.Info("Setting up Caddy reverse proxy...")

	if err := osMkdirAll(getProxyDir(), 0755); err != nil {
		return fmt.Errorf("failed to create proxy directory: %w", err)
	}

	composeContent := generateCaddyCompose()
	composePath := filepath.Join(getProxyDir(), "docker-compose.yml")
	if err := osWriteFile(composePath, []byte(composeContent), 0644); err != nil {
		return fmt.Errorf("failed to write Caddy compose: %w", err)
	}

	caddyfile := fmt.Sprintf("{\n    email %s\n}\n", acmeEmail)
	caddyfilePath := filepath.Join(getProxyDir(), "Caddyfile")
	if err := osWriteFile(caddyfilePath, []byte(caddyfile), 0644); err != nil {
		return fmt.Errorf("failed to write Caddyfile: %w", err)
	}

	if err := dockerCreateNetwork(ctx, "simpledeploy"); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), proxySetupTimeout)
	defer cancel()
	cmd := execCommand(ctx, "docker", "compose", "up", "-d")
	cmd.SetDir(getProxyDir())
	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to start Caddy: %w", err)
	}

	wizard.Success("Caddy reverse proxy started")
	return nil
}

var safeDomainRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*\.[a-zA-Z]{2,}$`)

// escapeCaddyValue escapes a value for safe use in a Caddyfile.
// It prevents Caddyfile injection attacks by escaping special characters.
func escapeCaddyValue(s string) string {
	// Escape backslashes first
	s = strings.ReplaceAll(s, `\`, `\\`)
	// Escape double quotes
	s = strings.ReplaceAll(s, `"`, `\"`)
	// Escape newlines
	s = strings.ReplaceAll(s, "\n", `\n`)
	// Escape carriage returns
	s = strings.ReplaceAll(s, "\r", `\r`)
	return s
}

func AddCaddyApp(appName, domain string, port int, headers map[string]string) error {
	// Defense-in-depth: deploy.go validates these at the input layer, but the
	// values flow through state.json into Caddyfile emission below where they
	// are interpolated unescaped. A tampered or pre-validator state file
	// could otherwise inject Caddyfile directives.
	if err := state.ValidateAppName(appName); err != nil {
		return fmt.Errorf("invalid app name: %w", err)
	}
	if !safeDomainRe.MatchString(domain) {
		return fmt.Errorf("invalid domain: %q", domain)
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("invalid port %d (must be 1-65535)", port)
	}

	caddyfilePath := filepath.Join(getProxyDir(), "Caddyfile")
	data, err := os.ReadFile(caddyfilePath)
	if err != nil {
		return fmt.Errorf("failed to read Caddyfile: %w", err)
	}

	// Validate every header before touching the file so a partially-valid
	// header map cannot leave the Caddyfile half-rewritten on the disk.
	for key, val := range headers {
		// Header NAMES are interpolated raw into the Caddyfile; a key with
		// `\n}` or whitespace would let an attacker break out of the block
		// and inject directives. Validate here in addition to escapeCaddyValue
		// (which only protects the value side).
		if err := state.ValidateHeaderName(key); err != nil {
			return fmt.Errorf("invalid header name in app %q: %w", appName, err)
		}
		// Values must be validated too, not just escaped: escapeCaddyValue
		// keeps `{` and `}` intact, but `{...}` is Caddy placeholder syntax
		// (expanded even inside quotes) and filterCaddyDomain counts braces to
		// find a block's end — an unbalanced brace in a value makes the next
		// rewrite swallow the rest of the Caddyfile. Same check the Traefik
		// path applies in compose.Generate.
		if err := state.ValidateHeaderValue(val); err != nil {
			return fmt.Errorf("invalid header value for %q in app %q: %w", key, appName, err)
		}
	}

	// Dedup: if AddCaddyApp is called twice for the same domain (e.g. on a
	// redeploy that changes port or headers) the previous block must be
	// stripped first. Otherwise Caddy would parse two routing blocks for the
	// same hostname and pick whichever came first, silently masking the
	// updated config.
	existing := filterCaddyDomain(string(data), domain)

	var b strings.Builder
	b.WriteString(existing)
	if !strings.HasSuffix(existing, "\n") {
		b.WriteString("\n")
	}
	b.WriteString(fmt.Sprintf("\n%s {\n", domain))
	b.WriteString(fmt.Sprintf("    reverse_proxy qd-%s:%d\n", appName, port))
	// Sorted, for the same reason compose/generator.go sorts every map it
	// walks: Go randomises map iteration order per run, so an unsorted walk
	// emitted the header directives in a different order on every deploy. The
	// Caddyfile is rewritten and reloaded on each one, which turned a no-op
	// redeploy into a config change — unreviewable diffs, and a proxy reload
	// that cannot be distinguished from a real one.
	for _, key := range sortedHeaderKeys(headers) {
		// Escape the value to prevent Caddyfile injection
		escapedVal := escapeCaddyValue(headers[key])
		b.WriteString(fmt.Sprintf("    header %s \"%s\"\n", key, escapedVal))
	}
	b.WriteString("}\n")

	return writeCaddyfile(caddyfilePath, []byte(b.String()), 0644)
}

func RemoveCaddyApp(domain string) error {
	caddyfilePath := filepath.Join(getProxyDir(), "Caddyfile")
	data, err := os.ReadFile(caddyfilePath)
	if err != nil {
		return err
	}

	filtered := filterCaddyDomain(string(data), domain)
	return writeCaddyfile(caddyfilePath, []byte(filtered), 0644)
}

func ReloadCaddy() error {
	ctx, cancel := context.WithTimeout(context.Background(), proxyExecTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "exec", "qd-caddy", "caddy", "reload", "--config", "/etc/caddy/Caddyfile")
	return cmd.Run()
}

func StopCaddy() error {
	ctx, cancel := context.WithTimeout(context.Background(), proxyExecTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "compose", "down")
	cmd.Dir = getProxyDir()
	return cmd.Run()
}

func generateCaddyCompose() string {
	// extra_hosts maps host.docker.internal to the Docker bridge gateway so
	// the containerised proxy can reach the webhook server, which runs as a
	// plain process on the host rather than as a container.
	return `# Auto-generated by SimpleDeploy — DO NOT EDIT
# Reverse Proxy: Caddy

networks:
  simpledeploy:
    name: simpledeploy
    external: true

services:
  caddy:
    image: caddy:2-alpine
    container_name: qd-caddy
    restart: unless-stopped
    ports:
      - "80:80"
      - "443:443"
      - "443:443/udp"
    extra_hosts:
      - "host.docker.internal:host-gateway"
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile
      - qd-caddy-data:/data
      - qd-caddy-config:/config
    networks:
      - simpledeploy

volumes:
  qd-caddy-data:
  qd-caddy-config:
`
}

// setupCaddyWebhookRoute adds (or replaces) the Caddyfile block that publishes
// the host-side webhook server at https://<baseDomain>/_qd/*.
//
// It reuses filterCaddyDomain for dedupe, so re-running `init` rewrites the
// block rather than stacking a second ambiguous one for the same hostname.
func setupCaddyWebhookRoute(baseDomain string, webhookPort int) error {
	caddyfilePath := filepath.Join(getProxyDir(), "Caddyfile")
	data, err := os.ReadFile(caddyfilePath)
	if err != nil {
		return fmt.Errorf("failed to read Caddyfile: %w", err)
	}

	existing := filterCaddyDomain(string(data), baseDomain)

	var b strings.Builder
	b.WriteString(existing)
	if !strings.HasSuffix(existing, "\n") {
		b.WriteString("\n")
	}
	b.WriteString(fmt.Sprintf("\n%s {\n", baseDomain))
	b.WriteString("    handle /_qd/* {\n")
	b.WriteString(fmt.Sprintf("        reverse_proxy host.docker.internal:%d\n", webhookPort))
	b.WriteString("    }\n")
	// Anything else on the bare base domain is not ours to serve. Answering
	// 404 explicitly is clearer than Caddy's default behaviour of falling
	// through to whichever block happens to match.
	b.WriteString("    handle {\n        respond \"Not Found\" 404\n    }\n")
	b.WriteString("}\n")

	return writeCaddyfile(caddyfilePath, []byte(b.String()), 0644)
}
