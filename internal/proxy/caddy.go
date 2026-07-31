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

// filterCaddyDomain is kept as a thin wrapper around the structured parser for
// backward compatibility with tests that exercise block removal in isolation.
// Production code uses parseCaddyfile + removeBlock + renderCaddyBlocks directly.
func filterCaddyDomain(content, domain string) string {
	blocks := parseCaddyfile(content)
	blocks = removeBlock(blocks, domain)
	return renderCaddyBlocks(blocks)
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

// safeDomainRe is kept only as a fast structural pre-check; the authoritative
// check is state.ValidateAppDomain, which callers below use. On its own this
// pattern is weaker than the state validators — it admits uppercase, `_`,
// empty/consecutive labels ("a..b.com"), labels ending in `-`, and has no
// length cap — so relying on it alone left the Caddy path accepting domains
// every other sink rejects.
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
	// Same validator the compose/Traefik path uses, so a domain is either
	// acceptable everywhere or nowhere. safeDomainRe alone let through values
	// ValidateAppDomain rejects (see its comment), which is how a Caddy-mode
	// install could hold a site address that no other component would accept.
	if err := state.ValidateAppDomain(domain); err != nil {
		return fmt.Errorf("invalid domain %q: %w", domain, err)
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
		// (expanded even inside quotes). Same check the Traefik path applies
		// in compose.Generate.
		if err := state.ValidateHeaderValue(val); err != nil {
			return fmt.Errorf("invalid header value for %q in app %q: %w", key, appName, err)
		}
	}

	// Parse the existing Caddyfile into structured blocks, then upsert the
	// app's block. This replaces the old filterCaddyDomain brace-counting
	// approach: dedup is inherent (upsertBlock replaces by address), and the
	// global block is never at risk from an empty domain.
	blocks := parseCaddyfile(string(data))

	// Build the body lines for this app's site block.
	var body []string
	body = append(body, fmt.Sprintf("    reverse_proxy qd-%s:%d", appName, port))
	// Sorted, for the same reason compose/generator.go sorts every map it
	// walks: Go randomises map iteration order per run, so an unsorted walk
	// emitted the header directives in a different order on every deploy.
	for _, key := range sortedHeaderKeys(headers) {
		escapedVal := escapeCaddyValue(headers[key])
		body = append(body, fmt.Sprintf("    header %s \"%s\"", key, escapedVal))
	}

	blocks = upsertBlock(blocks, domain, body)
	rendered := renderCaddyBlocks(blocks)

	return writeCaddyfile(caddyfilePath, []byte(rendered), 0644)
}

func RemoveCaddyApp(domain string) error {
	// An empty domain is not a harmless no-op: with the old filterCaddyDomain
	// it matched the global block's bare `{`. The structured parser makes this
	// impossible (removeBlock matches on address, which is "" only for the
	// global block, and we skip global blocks), but validation still runs to
	// fail fast on a clearly invalid value from a tampered state file.
	if err := state.ValidateAppDomain(domain); err != nil {
		return fmt.Errorf("refusing to remove Caddy block for invalid domain %q: %w", domain, err)
	}

	caddyfilePath := filepath.Join(getProxyDir(), "Caddyfile")
	data, err := os.ReadFile(caddyfilePath)
	if err != nil {
		return err
	}

	blocks := parseCaddyfile(string(data))
	blocks = removeBlock(blocks, domain)
	rendered := renderCaddyBlocks(blocks)

	return writeCaddyfile(caddyfilePath, []byte(rendered), 0644)
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
// Uses the structured block parser: the route block is upserted by address,
// so re-running `init` replaces the block rather than stacking a second
// ambiguous one for the same hostname.
func setupCaddyWebhookRoute(baseDomain string, webhookPort int) error {
	caddyfilePath := filepath.Join(getProxyDir(), "Caddyfile")
	data, err := os.ReadFile(caddyfilePath)
	if err != nil {
		return fmt.Errorf("failed to read Caddyfile: %w", err)
	}

	blocks := parseCaddyfile(string(data))

	body := []string{
		"    handle /_qd/* {",
		fmt.Sprintf("        reverse_proxy host.docker.internal:%d", webhookPort),
		"    }",
		// Anything else on the bare base domain is not ours to serve. Answering
		// 404 explicitly is clearer than Caddy's default behaviour of falling
		// through to whichever block happens to match.
		"    handle {",
		"        respond \"Not Found\" 404",
		"    }",
	}

	blocks = upsertBlock(blocks, baseDomain, body)
	rendered := renderCaddyBlocks(blocks)

	return writeCaddyfile(caddyfilePath, []byte(rendered), 0644)
}
