package compose

import (
	"strings"
	"testing"
)

func fixtureData() *ComposeData {
	return &ComposeData{
		AppName:   "detapp",
		Image:     "detapp:v1",
		Port:      3000,
		Domain:    "detapp.example.com",
		ProxyType: "traefik",
		Environment: map[string]string{
			"ZULU": "1", "ALPHA": "2", "MIKE": "3", "BRAVO": "4", "YANKEE": "5",
		},
		Headers: map[string]string{
			"X-Frame-Options": "DENY", "X-Custom": "v", "Referrer-Policy": "no-referrer",
		},
		Databases: []DBService{{
			Type:       "mysql",
			Image:      "mysql:8",
			Volume:     "/var/lib/mysql",
			VolumeName: "qd-detapp-mysql-data",
			Env: map[string]string{
				"MYSQL_ROOT_PASSWORD": "s3cret", "MYSQL_DATABASE": "detapp", "MYSQL_USER": "app",
			},
		}},
		Repo:        "https://github.com/test/detapp.git",
		Branch:      "main",
		GeneratedAt: "2024-01-15T10:30:00Z",
	}
}

// TestGenerate_IsDeterministic guards against Go's randomised map iteration
// leaking into the output. Before sorting was introduced, every call produced
// a different ordering of environment variables, Traefik header middlewares,
// and database env — so an unchanged app rewrote its docker-compose.yml on
// every redeploy, producing noisy diffs and making the file impossible to
// compare across runs.
func TestGenerate_IsDeterministic(t *testing.T) {
	first, err := Generate(fixtureData())
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	for i := 0; i < 50; i++ {
		got, err := Generate(fixtureData())
		if err != nil {
			t.Fatalf("Generate failed on iteration %d: %v", i, err)
		}
		if got != first {
			t.Fatalf("output differs between runs (iteration %d)", i)
		}
	}
}

func TestGenerate_SortsEnvironment(t *testing.T) {
	out, err := Generate(fixtureData())
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	// "KEY:" — env vars are emitted as YAML map entries, not "KEY=" list items
	// (see TestGenerate_EnvUsesYAMLMapForm in generator_test.go).
	order := []string{"ALPHA:", "BRAVO:", "MIKE:", "YANKEE:", "ZULU:"}
	prev := -1
	for _, key := range order {
		idx := strings.Index(out, key)
		if idx == -1 {
			t.Fatalf("missing env key %q in output", key)
		}
		if idx < prev {
			t.Errorf("env keys are not in sorted order (%q out of place)", key)
		}
		prev = idx
	}
}

// TestGenerate_DoesNotPublishHostPorts is the regression test for a proxy
// bypass: the generator used to emit `ports: - "3000"`, which publishes the
// container port on a random host port bound to 0.0.0.0. Every deployed app
// was therefore reachable over plain HTTP on the public IP, skipping the
// reverse proxy, its TLS termination, and the security headers below.
func TestGenerate_DoesNotPublishHostPorts(t *testing.T) {
	out, err := Generate(fixtureData())
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "ports:" {
			t.Error("app service must not publish host ports; use expose so it is reachable only via the proxy")
		}
	}
	if !strings.Contains(out, "expose:") {
		t.Error("app service should declare expose for the application port")
	}
	if !strings.Contains(out, `      - "3000"`) {
		t.Error("exposed port value missing")
	}
}
