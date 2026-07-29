package cli

import (
	"context"
	"os"
	"testing"

	"github.com/ersinkoc/SimpleDeploy/internal/docker"
)

// See internal/docker/integration_test.go for why these tests are opt-in.
// The constant is repeated per package because identifiers declared in a
// _test.go file are not visible to other packages.
//
//	SIMPLEDEPLOY_INTEGRATION=1 go test -p=1 -count=1 ./...
const integrationEnv = "SIMPLEDEPLOY_INTEGRATION"

func requireDocker(t *testing.T) {
	t.Helper()
	if os.Getenv(integrationEnv) != "1" {
		t.Skipf("integration test; set %s=1 to run", integrationEnv)
	}
	if !docker.IsInstalled() {
		t.Skip("Docker not installed")
	}
}

func requireCompose(t *testing.T) {
	t.Helper()
	requireDocker(t)
	if !docker.IsComposeInstalled(context.Background()) {
		t.Skip("Docker Compose plugin not installed")
	}
}
