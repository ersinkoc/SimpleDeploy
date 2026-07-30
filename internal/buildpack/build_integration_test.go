package buildpack

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These tests build every generated Dockerfile with a real Docker daemon and
// run the resulting image, because nothing else in the suite does.
//
// Until this existed, the six templates in detect.go were only ever compared as
// strings. A template that does not build is not a cosmetic problem — it fails
// every deploy of that stack, at `docker build`, after the repo has already been
// cloned. Two of the fixed bugs recorded in detect.go's comments (npm's
// `--production` before `npm run build`, and Python's unconditional
// `COPY requirements.txt`) were exactly this, and both would have been caught
// here on the first run.
//
// Opt-in for the same reason as the other integration tests: an unprivileged CI
// runner cannot talk to a Docker daemon, and these pull ~1.5 GB of base images.
//
//	SIMPLEDEPLOY_INTEGRATION=1 go test -p=1 -count=1 ./internal/buildpack/
const integrationEnv = "SIMPLEDEPLOY_INTEGRATION"

func requireDocker(t *testing.T) {
	t.Helper()
	if os.Getenv(integrationEnv) != "1" {
		t.Skipf("integration test; set %s=1 to run", integrationEnv)
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker not installed")
	}
}

// buildFixture is a minimal but genuine project of one stack: enough for the
// template's dependency install and entrypoint detection to do real work.
type buildFixture struct {
	appType string
	// port the template's EXPOSE / CMD listens on, per the table in README.
	port  int
	files map[string]string
	// want is a substring the running container must serve on /.
	want string
}

func fixtures() []buildFixture {
	return []buildFixture{
		{
			appType: TypeStatic,
			port:    80,
			want:    "static-ok",
			files:   map[string]string{"index.html": "<html><body>static-ok</body></html>"},
		},
		{
			appType: TypeNode,
			port:    3000,
			want:    "node-ok",
			files: map[string]string{
				// A "build" script is present on purpose: the template must run
				// it, which needs devDependencies installed first.
				"package.json": `{"name":"n","version":"1.0.0","scripts":{"build":"echo built > built.txt","start":"node server.js"}}`,
				"server.js":    "const h=require('http');h.createServer((q,s)=>s.end('node-ok\\n')).listen(3000,'0.0.0.0');",
			},
		},
		{
			appType: TypeGo,
			port:    8080,
			want:    "go-ok",
			// No go.sum — the template's `go.su[m]` wildcard must tolerate it.
			files: map[string]string{
				"go.mod":  "module probe\n\ngo 1.23\n",
				"main.go": "package main\n\nimport (\n\t\"fmt\"\n\t\"net/http\"\n)\n\nfunc main() {\n\thttp.HandleFunc(\"/\", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, \"go-ok\") })\n\thttp.ListenAndServe(\":8080\", nil)\n}\n",
			},
		},
		{
			appType: TypePython,
			port:    8000,
			want:    "python-ok",
			files: map[string]string{
				"requirements.txt": "",
				"app.py":           "import http.server, socketserver\n\nclass H(http.server.BaseHTTPRequestHandler):\n    def do_GET(s):\n        s.send_response(200)\n        s.end_headers()\n        s.wfile.write(b'python-ok\\n')\n\nsocketserver.TCPServer(('', 8000), H).serve_forever()\n",
			},
		},
		{
			appType: TypePHP,
			port:    80,
			want:    "php-ok",
			// No composer.json: a bare *.php project must still build.
			files: map[string]string{"index.php": "<?php echo \"php-ok\\n\";"},
		},
		{
			appType: TypeRuby,
			port:    3000,
			want:    "ruby-ok",
			// No Gemfile.lock — the template's `Gemfile.loc[k]` wildcard and the
			// absence of `--deployment` must tolerate it.
			files: map[string]string{
				"Gemfile":   "source 'https://rubygems.org'\ngem 'rackup'\ngem 'webrick'\n",
				"config.ru": "run ->(env){ [200, {'content-type'=>'text/plain'}, ['ruby-ok']] }\n",
			},
		},
	}
}

func TestGeneratedDockerfilesBuildAndServe(t *testing.T) {
	requireDocker(t)

	for _, f := range fixtures() {
		t.Run(f.appType, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range f.files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
					t.Fatalf("writing fixture %s: %v", name, err)
				}
			}
			if err := WriteDockerfile(dir, f.appType); err != nil {
				t.Fatalf("WriteDockerfile: %v", err)
			}
			if _, err := os.Stat(filepath.Join(dir, ".dockerignore")); err != nil {
				t.Errorf("WriteDockerfile should also emit .dockerignore: %v", err)
			}

			image := fmt.Sprintf("simpledeploy-bptest-%s:test", f.appType)
			if out, err := run("docker", "build", "-q", "-t", image, dir); err != nil {
				t.Fatalf("generated Dockerfile for %s does not build — every deploy of this stack would fail at build:\n%s", f.appType, out)
			}
			t.Cleanup(func() { _, _ = run("docker", "rmi", "-f", image) })

			name := "simpledeploy-bptest-" + f.appType
			_, _ = run("docker", "rm", "-f", name)
			if out, err := run("docker", "run", "-d", "--name", name, "-P", image); err != nil {
				t.Fatalf("docker run: %s", out)
			}
			t.Cleanup(func() { _, _ = run("docker", "rm", "-f", name) })

			hostPort, err := publishedPort(name, f.port)
			if err != nil {
				t.Fatalf("the image must EXPOSE %d (the documented default port for %s): %v", f.port, f.appType, err)
			}

			body, err := getWithin(fmt.Sprintf("http://127.0.0.1:%s/", hostPort), f.want, 60*time.Second)
			if err != nil {
				logs, _ := run("docker", "logs", "--tail", "20", name)
				t.Fatalf("%s image never served %q on port %d: %v\ncontainer logs:\n%s", f.appType, f.want, f.port, err, logs)
			}
			if !strings.Contains(body, f.want) {
				t.Errorf("served %q, want it to contain %q", body, f.want)
			}
		})
	}
}

// TestNodeTemplateRunsBuildScript pins the specific regression called out in
// detect.go: the template used to run `npm ci --production` and then
// `npm run build`, which failed on essentially every project that had a build
// step, because the tooling lives in devDependencies.
func TestNodeTemplateRunsBuildScript(t *testing.T) {
	requireDocker(t)

	dir := t.TempDir()
	files := map[string]string{
		"package.json": `{"name":"n","version":"1.0.0","scripts":{"build":"echo built > built.txt","start":"node server.js"}}`,
		"server.js":    "setInterval(()=>{},1000);",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := WriteDockerfile(dir, TypeNode); err != nil {
		t.Fatal(err)
	}

	image := "simpledeploy-bptest-nodebuild:test"
	if out, err := run("docker", "build", "-q", "-t", image, dir); err != nil {
		t.Fatalf("build: %s", out)
	}
	t.Cleanup(func() { _, _ = run("docker", "rmi", "-f", image) })

	out, err := run("docker", "run", "--rm", image, "sh", "-c", "ls built.txt")
	if err != nil {
		t.Fatalf("the template did not run the project's build script: %s", out)
	}
}

// TestPythonTemplateHandlesPyprojectOnly pins the other regression from
// detect.go: the template used to COPY requirements.txt unconditionally, so a
// pyproject.toml-only project failed at COPY before installing anything.
func TestPythonTemplateHandlesPyprojectOnly(t *testing.T) {
	requireDocker(t)

	dir := t.TempDir()
	files := map[string]string{
		"pyproject.toml": "[project]\nname = \"probe\"\nversion = \"0.1.0\"\n\n[build-system]\nrequires = [\"setuptools\"]\nbuild-backend = \"setuptools.build_meta\"\n",
		"app.py":         "print('pyproject-ok')\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := WriteDockerfile(dir, TypePython); err != nil {
		t.Fatal(err)
	}

	image := "simpledeploy-bptest-pyproject:test"
	if out, err := run("docker", "build", "-q", "-t", image, dir); err != nil {
		t.Fatalf("a pyproject.toml-only project must build: %s", out)
	}
	t.Cleanup(func() { _, _ = run("docker", "rmi", "-f", image) })

	out, err := run("docker", "run", "--rm", image)
	if err != nil || !strings.Contains(out, "pyproject-ok") {
		t.Errorf("ran %q (err=%v), want it to contain \"pyproject-ok\"", out, err)
	}
}

func run(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return string(out), err
}

// publishedPort returns the host port Docker mapped for the container's
// containerPort, which only exists if the image actually EXPOSEs it.
func publishedPort(container string, containerPort int) (string, error) {
	out, err := run("docker", "port", container, fmt.Sprintf("%d/tcp", containerPort))
	if err != nil {
		return "", fmt.Errorf("docker port: %s", out)
	}
	line := strings.TrimSpace(strings.SplitN(strings.TrimSpace(out), "\n", 2)[0])
	idx := strings.LastIndex(line, ":")
	if idx < 0 {
		return "", fmt.Errorf("unexpected `docker port` output %q", out)
	}
	return line[idx+1:], nil
}

// getWithin polls url until the response contains want or the budget expires.
// Uses curl rather than net/http so a container that is still starting produces
// a retry rather than a spurious failure.
func getWithin(url, want string, budget time.Duration) (string, error) {
	deadline := time.Now().Add(budget)
	var last string
	for time.Now().Before(deadline) {
		out, err := run("curl", "-s", "-m", "3", url)
		if err == nil && strings.Contains(out, want) {
			return out, nil
		}
		last = out
		time.Sleep(time.Second)
	}
	return last, fmt.Errorf("timed out after %v; last response %q", budget, last)
}
