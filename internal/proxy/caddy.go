package proxy

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ersinkoc/SimpleDeploy/internal/docker"
	"github.com/ersinkoc/SimpleDeploy/internal/state"
	"github.com/ersinkoc/SimpleDeploy/internal/wizard"
)

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

	var b strings.Builder
	b.WriteString(string(data))
	b.WriteString(fmt.Sprintf("\n%s {\n", domain))
	b.WriteString(fmt.Sprintf("    reverse_proxy qd-%s:%d\n", appName, port))
	for key, val := range headers {
		// Header NAMES are interpolated raw into the Caddyfile; a key with
		// `\n}` or whitespace would let an attacker break out of the block
		// and inject directives. Validate here in addition to escapeCaddyValue
		// (which only protects the value side).
		if err := state.ValidateHeaderName(key); err != nil {
			return fmt.Errorf("invalid header name in app %q: %w", appName, err)
		}
		// Escape the value to prevent Caddyfile injection
		escapedVal := escapeCaddyValue(val)
		b.WriteString(fmt.Sprintf("    header %s \"%s\"\n", key, escapedVal))
	}
	b.WriteString("}\n")

	return os.WriteFile(caddyfilePath, []byte(b.String()), 0644)
}

func RemoveCaddyApp(domain string) error {
	caddyfilePath := filepath.Join(getProxyDir(), "Caddyfile")
	data, err := os.ReadFile(caddyfilePath)
	if err != nil {
		return err
	}

	lines := strings.Split(string(data), "\n")
	var result []string
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

	return os.WriteFile(caddyfilePath, []byte(strings.Join(result, "\n")), 0644)
}

func ReloadCaddy() error {
	cmd := exec.Command("docker", "exec", "qd-caddy", "caddy", "reload", "--config", "/etc/caddy/Caddyfile")
	return cmd.Run()
}

func StopCaddy() error {
	cmd := exec.Command("docker", "compose", "down")
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
