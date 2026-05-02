package proxy

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	execCommand         = func(name string, arg ...string) commandRunner { return &execWrapper{exec.Command(name, arg...)} }
)

// atomicWriteFile writes data to a sibling temp file then renames it over
// the destination. On both POSIX and Windows os.Rename is atomic when
// source and destination are on the same filesystem, so a partial /
// interrupted write can never leave a half-written Caddyfile in place
// (which would either crash Caddy on reload or, worse, accept a malformed
// routing config). The temp file uses the same parent directory to keep
// the rename intra-fs.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, perm); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		// Best-effort cleanup — leaving the .tmp behind is safer than
		// returning the rename error and pretending nothing happened.
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
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

func SetupCaddy(acmeEmail string) error {
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

	if err := dockerCreateNetwork("simpledeploy"); err != nil {
		return err
	}

	cmd := execCommand("docker", "compose", "up", "-d")
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
	for key := range headers {
		// Header NAMES are interpolated raw into the Caddyfile; a key with
		// `\n}` or whitespace would let an attacker break out of the block
		// and inject directives. Validate here in addition to escapeCaddyValue
		// (which only protects the value side).
		if err := state.ValidateHeaderName(key); err != nil {
			return fmt.Errorf("invalid header name in app %q: %w", appName, err)
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
	for key, val := range headers {
		// Escape the value to prevent Caddyfile injection
		escapedVal := escapeCaddyValue(val)
		b.WriteString(fmt.Sprintf("    header %s \"%s\"\n", key, escapedVal))
	}
	b.WriteString("}\n")

	return atomicWriteFile(caddyfilePath, []byte(b.String()), 0644)
}

func RemoveCaddyApp(domain string) error {
	caddyfilePath := filepath.Join(getProxyDir(), "Caddyfile")
	data, err := os.ReadFile(caddyfilePath)
	if err != nil {
		return err
	}

	filtered := filterCaddyDomain(string(data), domain)
	return atomicWriteFile(caddyfilePath, []byte(filtered), 0644)
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
