package proxy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// This closes the third gap CLAUDE.md calls out: nothing in the unit suite
// mounts a file into a container, so the inode/bind-mount contract the Caddy
// path depends on was completely unrepresented.
//
// The contract: the proxy compose bind-mounts the Caddyfile as a SINGLE FILE,
// and Docker resolves such a mount to an inode once, at container creation.
// Writing via temp-file-then-rename replaces the directory entry with a NEW
// inode, so the running container stays bound to the old one — which no longer
// has a name — and never sees another byte. That is exactly what happened:
// `init` wrote the global block, started Caddy, and the first atomic write
// orphaned the mount, after which every AddCaddyApp was invisible. The host
// file grew correct site blocks while the container kept reading the original
// stub, `caddy reload` reported "config is unchanged" and succeeded, and
// `deploy` still printed "is ready!" for an app that was never routed.
//
// A unit test cannot catch this — writeCaddyfile and an atomic writer produce
// byte-identical files. Only a real mount can tell them apart.

// readThroughMount starts a container with hostFile bind-mounted at a fixed
// path, applies mutate to the host file, and returns what the container reads
// afterwards.
func readThroughMount(t *testing.T, hostFile string, mutate func()) string {
	t.Helper()

	name := "qd-mountprobe"
	_ = exec.Command("docker", "rm", "-f", name).Run()
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() })

	// Long-lived container so the mount is established before the write, which
	// is the whole point: Docker binds the inode at creation time.
	out, err := exec.Command("docker", "run", "-d", "--name", name,
		"-v", hostFile+":/probe/file.txt",
		"alpine:3.21", "sleep", "120").CombinedOutput()
	if err != nil {
		t.Fatalf("docker run: %v\n%s", err, out)
	}

	mutate()

	got, err := exec.Command("docker", "exec", name, "cat", "/probe/file.txt").CombinedOutput()
	if err != nil {
		t.Fatalf("docker exec: %v\n%s", err, got)
	}
	return string(got)
}

// TestWriteCaddyfile_SurvivesBindMount pins that writeCaddyfile's in-place
// write reaches a container that already has the file mounted.
func TestWriteCaddyfile_SurvivesBindMount(t *testing.T) {
	requireDocker(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "Caddyfile")
	if err := os.WriteFile(path, []byte("original\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	const updated = "updated by writeCaddyfile\n"
	got := readThroughMount(t, path, func() {
		if err := writeCaddyfile(path, []byte(updated), 0644); err != nil {
			t.Fatalf("writeCaddyfile: %v", err)
		}
	})

	if !strings.Contains(got, "updated by writeCaddyfile") {
		t.Errorf("the container still reads %q after writeCaddyfile — the bind mount was orphaned, "+
			"which is what temp+rename used to do and why Caddy routed nothing", got)
	}
}

// TestAtomicWriteOrphansBindMount demonstrates the failure mode the comment on
// writeCaddyfile describes, so the reason for its unusual in-place write is
// verifiable rather than folklore. If this ever stops reproducing (a Docker
// change), writeCaddyfile's constraint deserves re-examination.
func TestAtomicWriteOrphansBindMount(t *testing.T) {
	requireDocker(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "Caddyfile")
	if err := os.WriteFile(path, []byte("original\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got := readThroughMount(t, path, func() {
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, []byte("written atomically\n"), 0644); err != nil {
			t.Fatalf("write temp: %v", err)
		}
		if err := os.Rename(tmp, path); err != nil {
			t.Fatalf("rename: %v", err)
		}
	})

	if strings.Contains(got, "written atomically") {
		t.Skip("this Docker version follows the rename through a single-file bind mount; " +
			"writeCaddyfile's in-place write is then merely harmless rather than required")
	}
	if !strings.Contains(got, "original") {
		t.Errorf("container read %q, expected the pre-rename content — the test no longer "+
			"demonstrates the orphaned-mount failure it exists to document", got)
	}
}

// TestAddCaddyApp_VisibleThroughBindMount is the end-to-end version: the
// operation a deploy actually performs must be visible to a container that
// mounted the file beforehand.
func TestAddCaddyApp_VisibleThroughBindMount(t *testing.T) {
	requireDocker(t)

	dir := setupTestProxyDir(t)
	path := filepath.Join(dir, "Caddyfile")
	if err := os.WriteFile(path, []byte("{\n    email test@example.com\n}\n"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got := readThroughMount(t, path, func() {
		if err := AddCaddyApp("mountapp", "mountapp.example.com", 3000, nil); err != nil {
			t.Fatalf("AddCaddyApp: %v", err)
		}
	})

	if !strings.Contains(got, "mountapp.example.com") {
		t.Errorf("a container that mounted the Caddyfile before the deploy does not see the "+
			"new site block; it reads:\n%s", got)
	}
	if !strings.Contains(got, "reverse_proxy qd-mountapp:3000") {
		t.Errorf("the route itself is missing from what the container reads:\n%s", got)
	}
}
