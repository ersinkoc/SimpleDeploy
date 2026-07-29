package docker

import (
	"context"
	"os"
	"testing"
)

// IntegrationEnv opts a test run in to the tests that drive a real Docker
// daemon — pulling images, building, and starting containers.
//
// These are skipped by default because they are not unit tests: they need a
// reachable Docker daemon, network access to a registry (subject to Docker Hub
// anonymous pull limits), and in some cases exclusive use of ports 80/443.
// `go test ./...` on a CI runner therefore could not pass, and in this
// repository it never did — the very first CI run, months before this gate was
// added, already failed at the Test step while vet and build passed.
//
// A permanently red pipeline is worse than no pipeline: it trains everyone to
// ignore it, and it hides the failures that do matter. Splitting the two tiers
// keeps the unit suite meaningful on every push while the integration suite
// stays runnable on demand:
//
//	SIMPLEDEPLOY_INTEGRATION=1 go test -p=1 -count=1 ./...
//
// or via the "Integration Tests" workflow (manual dispatch).
// Declared per package rather than in one shared place: identifiers defined in
// a _test.go file are not visible to other packages, and this is a test-only
// concern that does not belong in the production API.
const integrationEnv = "SIMPLEDEPLOY_INTEGRATION"

// integrationEnabled reports whether integration tests were opted in to.
func integrationEnabled() bool { return os.Getenv(integrationEnv) == "1" }

// requireDocker skips unless integration tests are enabled AND Docker is
// actually present.
func requireDocker(t *testing.T) {
	t.Helper()
	if !integrationEnabled() {
		t.Skipf("integration test; set %s=1 to run", integrationEnv)
	}
	if !IsInstalled() {
		t.Skip("Docker not installed")
	}
}

// requireCompose additionally requires the Compose plugin.
func requireCompose(t *testing.T) {
	t.Helper()
	requireDocker(t)
	if !IsComposeInstalled(context.Background()) {
		t.Skip("Docker Compose plugin not installed")
	}
}
