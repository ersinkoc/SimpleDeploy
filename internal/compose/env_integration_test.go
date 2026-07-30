package compose

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// These tests close a gap CLAUDE.md calls out explicitly: nothing in the suite
// asserted what a container's environment actually RECEIVES — only on the YAML
// text SimpleDeploy emits. That gap is what let the list-form `environment:`
// bug ship (quotes arrived as literal characters inside the value, so no
// database driver could parse DATABASE_URL), and it is the only way to verify
// the `$`-escaping contract, since Compose's own interpolation happens after
// the YAML parse and is therefore invisible to a text assertion.
//
// Opt-in, like the other integration tests — they need a Docker daemon and the
// compose plugin:
//
//	SIMPLEDEPLOY_INTEGRATION=1 go test -p=1 -count=1 ./internal/compose/
const integrationEnv = "SIMPLEDEPLOY_INTEGRATION"

// probeImage is a tiny image that is already pulled by the buildpack
// integration tests, so this adds no new registry traffic in a full run.
const probeImage = "alpine:3.21"

func requireCompose(t *testing.T) {
	t.Helper()
	if os.Getenv(integrationEnv) != "1" {
		t.Skipf("integration test; set %s=1 to run", integrationEnv)
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker not installed")
	}
	if err := exec.Command("docker", "compose", "version").Run(); err != nil {
		t.Skip("Docker Compose plugin not installed")
	}
}

// TestContainerReceivesExactEnvironment runs a container from a generated
// compose file and reads back the environment the process actually got.
func TestContainerReceivesExactEnvironment(t *testing.T) {
	requireCompose(t)

	// The generated compose declares the shared network as external, so it has
	// to exist. Ignore the error: it is already there in a full integration run.
	_ = exec.Command("docker", "network", "create", "simpledeploy").Run()

	// Every value here is one that has broken, or could break, on the way to
	// the container.
	want := map[string]string{
		// A bcrypt hash: the headline casualty of Compose's `$VAR`
		// interpolation. Unescaped, `docker compose up` aborts outright with
		// "invalid interpolation format" — after the image has been built.
		"BCRYPT_HASH": `$2b$12$abcdefghijklmnopqrstuv`,
		// A `${...}` reference must arrive literally, not expanded. Compose
		// auto-loads the project directory's .env for interpolation, and that
		// is the file holding the generated database connection string.
		"PLACEHOLDER": "${DATABASE_URL}",
		// A single trailing `$` is its own edge case for the doubling rule.
		"TRAILING_DOLLAR": "cost: 5$",
		// The value shape that shipped broken once: quotes must be YAML syntax,
		// not literal characters in the value.
		"DATABASE_URL": "postgresql://postgres:pw@qd-envprobe-postgresql:5432/envprobe",
		"WITH_QUOTES":  `say "hi"`,
		"WITH_SPACES":  "a b c",
		"WITH_COLON":   "key: value",
		"WITH_HASH":    "not # a comment",
		"NODE_ENV":     "production",
	}

	dir := t.TempDir()
	data := &ComposeData{
		AppName:     "envprobe",
		Image:       probeImage,
		Port:        3000,
		Domain:      "envprobe.example.com",
		ProxyType:   "caddy",
		Environment: want,
	}
	yaml, err := Generate(data)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if err := WriteCompose(dir, yaml); err != nil {
		t.Fatalf("WriteCompose: %v", err)
	}

	// `compose run` uses the service's environment block and lets us override
	// the command, so the probe image does not need to be long-running.
	cmd := exec.Command("docker", "compose", "run", "--rm", "--no-deps", "envprobe", "env")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker compose run failed: %v\n%s\ngenerated compose:\n%s", err, out, yaml)
	}

	got := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		if k, v, ok := strings.Cut(strings.TrimRight(line, "\r"), "="); ok {
			got[k] = v
		}
	}

	for key, wantVal := range want {
		gotVal, ok := got[key]
		if !ok {
			t.Errorf("%s never reached the container", key)
			continue
		}
		if gotVal != wantVal {
			t.Errorf("container received %s=%q, want %q", key, gotVal, wantVal)
		}
	}
}

// TestContainerReceivesDatabasePassword covers the same contract for a database
// service's environment, which carries the generated root password — a value
// that must arrive byte-exact or the database rejects every connection the app
// makes with the connection string SimpleDeploy generated from it.
func TestContainerReceivesDatabasePassword(t *testing.T) {
	requireCompose(t)
	_ = exec.Command("docker", "network", "create", "simpledeploy").Run()

	// GeneratePassword only emits alphanumerics today, but the compose layer
	// must not depend on that: an operator-restored state file or a future
	// charset change would otherwise produce a container that silently gets a
	// different password than the connection string embeds.
	const password = `pa$$w0rd"with:specials`

	dir := t.TempDir()
	data := &ComposeData{
		AppName:   "dbprobe",
		Image:     probeImage,
		Port:      3000,
		Domain:    "dbprobe.example.com",
		ProxyType: "caddy",
		Databases: []DBService{{
			Type:       "postgresql",
			Image:      probeImage,
			Volume:     "/var/lib/postgresql/data",
			VolumeName: "qd-dbprobe-postgresql-data",
			Env:        map[string]string{"POSTGRES_PASSWORD": password},
		}},
	}
	yaml, err := Generate(data)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if err := WriteCompose(dir, yaml); err != nil {
		t.Fatalf("WriteCompose: %v", err)
	}
	t.Cleanup(func() {
		down := exec.Command("docker", "compose", "down", "-v")
		down.Dir = dir
		_ = down.Run()
	})

	cmd := exec.Command("docker", "compose", "run", "--rm", "--no-deps", "qd-dbprobe-postgresql",
		"sh", "-c", "printf '%s' \"$POSTGRES_PASSWORD\"")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("docker compose run failed: %v\ngenerated compose:\n%s", err, yaml)
	}

	if got := strings.TrimRight(string(out), "\r\n"); got != password {
		t.Errorf("database container received password %q, want %q\ngenerated compose:\n%s", got, password, yaml)
	}
}

// TestGeneratedComposeIsValidToCompose is the cheap smoke test the other two
// depend on: `docker compose config` parses and validates the file, including
// its interpolation pass. An escaping bug that makes the file unparseable fails
// here with the real error message rather than at deploy time.
func TestGeneratedComposeIsValidToCompose(t *testing.T) {
	requireCompose(t)

	dir := t.TempDir()
	data := &ComposeData{
		AppName:   "cfgprobe",
		Image:     probeImage,
		Port:      8080,
		Domain:    "cfgprobe.example.com",
		ProxyType: "traefik",
		Environment: map[string]string{
			"BCRYPT_HASH": `$2b$12$abcdefghijklmnopqrstuv`,
			"PLACEHOLDER": "${DATABASE_URL}",
		},
		Headers: map[string]string{
			"X-Frame-Options": "SAMEORIGIN",
			"X-Price":         "5 USD",
		},
		Databases: []DBService{{
			Type:       "mysql",
			Image:      probeImage,
			Volume:     "/var/lib/mysql",
			VolumeName: "qd-cfgprobe-mysql-data",
			Env:        map[string]string{"MYSQL_ROOT_PASSWORD": `pa$$word`},
			HealthCheck: &HealthCheckData{
				Test:     []string{"CMD", "true"},
				Interval: "10s",
				Timeout:  "5s",
				Retries:  5,
			},
		}},
	}
	yaml, err := Generate(data)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if err := WriteCompose(dir, yaml); err != nil {
		t.Fatalf("WriteCompose: %v", err)
	}

	cmd := exec.Command("docker", "compose", "config")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated compose is not valid:\n%s\ngenerated:\n%s", out, yaml)
	}

	// `compose config` normalises the file but deliberately KEEPS `$$` doubled,
	// so its output can be fed back in without changing meaning — it is not a
	// view of the values the container will see. (Confirmed against compose
	// v5.3.1: `BCRYPT_HASH: $$2b$$12$$…`.) What matters here is only that the
	// file parses and its interpolation pass raises no error; the values the
	// process actually receives are asserted by
	// TestContainerReceivesExactEnvironment, which reads them out of a running
	// container.
	normalised := string(out)
	if !strings.Contains(normalised, `$$2b$$12$$abcdefghijklmnopqrstuv`) {
		t.Errorf("escaped bcrypt hash is not preserved round-trip:\n%s", normalised)
	}
	if !strings.Contains(normalised, `$${DATABASE_URL}`) {
		t.Errorf("placeholder lost its escaping, so Compose would expand it:\n%s", normalised)
	}
	// The interpolation pass must not have consumed anything: a single `$`
	// surviving here means a value would be expanded at deploy time.
	if strings.Contains(normalised, "BCRYPT_HASH: $2b") {
		t.Errorf("bcrypt hash reached Compose under-escaped:\n%s", normalised)
	}
}
