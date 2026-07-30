package cli

import (
	"os"
	"testing"
	"time"
)

// TestMain shortens the container stability window for the whole package.
//
// waitForContainer requires a container to hold "running" for
// containerStableFor before believing it — 5 s in production, which is nothing
// next to an image build but is dead weight multiplied across every
// Docker-backed test in this package (it took the suite from ~12 s to ~148 s).
//
// Tests that actually exercise the window set their own value; everything else
// only needs it to be non-zero, because zero would short-circuit the very check
// that catches a crash-looping deploy.
func TestMain(m *testing.M) {
	containerStableFor = 10 * time.Millisecond
	os.Exit(m.Run())
}
