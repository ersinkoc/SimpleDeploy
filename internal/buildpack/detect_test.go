package buildpack

import (
	"os"
	"path/filepath"
	"testing"
)

func createTempRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0755)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestDetect_Dockerfile(t *testing.T) {
	dir := createTempRepo(t, map[string]string{"Dockerfile": "FROM alpine"})
	result := Detect(dir)
	if result.Name != TypeDocker {
		t.Errorf("Name = %q, want %q", result.Name, TypeDocker)
	}
	if !result.Detected {
		t.Error("Should be detected")
	}
}

func TestDetect_Node(t *testing.T) {
	dir := createTempRepo(t, map[string]string{"package.json": `{"name":"test"}`})
	result := Detect(dir)
	if result.Name != TypeNode {
		t.Errorf("Name = %q, want %q", result.Name, TypeNode)
	}
	if result.Port != 3000 {
		t.Errorf("Port = %d, want 3000", result.Port)
	}
}

func TestDetect_Go(t *testing.T) {
	dir := createTempRepo(t, map[string]string{"go.mod": "module test\n"})
	result := Detect(dir)
	if result.Name != TypeGo {
		t.Errorf("Name = %q, want %q", result.Name, TypeGo)
	}
	if result.Port != 8080 {
		t.Errorf("Port = %d, want 8080", result.Port)
	}
}

func TestDetect_Python(t *testing.T) {
	dir := createTempRepo(t, map[string]string{"requirements.txt": "flask\n"})
	result := Detect(dir)
	if result.Name != TypePython {
		t.Errorf("Name = %q, want %q", result.Name, TypePython)
	}
	if result.Port != 8000 {
		t.Errorf("Port = %d, want 8000", result.Port)
	}
}

func TestDetect_PHP(t *testing.T) {
	dir := createTempRepo(t, map[string]string{"composer.json": `{}`})
	result := Detect(dir)
	if result.Name != TypePHP {
		t.Errorf("Name = %q, want %q", result.Name, TypePHP)
	}
}

func TestDetect_PHPFiles(t *testing.T) {
	dir := createTempRepo(t, map[string]string{"index.php": "<?php echo 'hello';"})
	result := Detect(dir)
	if result.Name != TypePHP {
		t.Errorf("Name = %q, want %q", result.Name, TypePHP)
	}
}

func TestDetect_Ruby(t *testing.T) {
	dir := createTempRepo(t, map[string]string{"Gemfile": "source 'https://rubygems.org'"})
	result := Detect(dir)
	if result.Name != TypeRuby {
		t.Errorf("Name = %q, want %q", result.Name, TypeRuby)
	}
}

func TestDetect_Static(t *testing.T) {
	dir := createTempRepo(t, map[string]string{"index.html": "<html></html>"})
	result := Detect(dir)
	if result.Name != TypeStatic {
		t.Errorf("Name = %q, want %q", result.Name, TypeStatic)
	}
}

func TestDetect_Empty(t *testing.T) {
	dir := createTempRepo(t, map[string]string{})
	result := Detect(dir)
	if result.Detected {
		t.Error("Empty dir should not be detected")
	}
}

func TestDetect_DockerfileTakesPrecedence(t *testing.T) {
	dir := createTempRepo(t, map[string]string{
		"Dockerfile":   "FROM alpine",
		"package.json": `{"name":"test"}`,
	})
	result := Detect(dir)
	if result.Name != TypeDocker {
		t.Errorf("Dockerfile should take precedence, got %q", result.Name)
	}
}

func TestGetDockerfileTemplate(t *testing.T) {
	tests := []string{TypeNode, TypeGo, TypePHP, TypePython, TypeRuby, TypeStatic}
	for _, appType := range tests {
		tmpl := GetDockerfileTemplate(appType)
		if tmpl == "" {
			t.Errorf("GetDockerfileTemplate(%q) returned empty", appType)
		}
	}
}

func TestGetDockerfileTemplate_Unknown(t *testing.T) {
	tmpl := GetDockerfileTemplate("unknown")
	if tmpl != "" {
		t.Error("Unknown type should return empty template")
	}
}

func TestWriteDockerfile(t *testing.T) {
	dir := t.TempDir()
	for _, appType := range []string{TypeNode, TypeGo, TypePHP, TypePython, TypeRuby, TypeStatic} {
		t.Run(appType, func(t *testing.T) {
			subdir := filepath.Join(dir, appType)
			os.MkdirAll(subdir, 0755)
			if err := WriteDockerfile(subdir, appType); err != nil {
				t.Fatalf("WriteDockerfile(%q) failed: %v", appType, err)
			}
			data, err := os.ReadFile(filepath.Join(subdir, "Dockerfile"))
			if err != nil {
				t.Fatalf("ReadFile failed: %v", err)
			}
			if len(data) == 0 {
				t.Error("Dockerfile should not be empty")
			}
			if len(data) < 5 || string(data)[:5] != "FROM " {
				t.Error("Dockerfile should start with FROM")
			}
		})
	}
}

func TestWriteDockerfile_Unsupported(t *testing.T) {
	dir := t.TempDir()
	err := WriteDockerfile(dir, "unknown")
	if err == nil {
		t.Error("Should fail for unknown type")
	}
}

func TestDetectNodePort_NextProject(t *testing.T) {
	dir := createTempRepo(t, map[string]string{
		"package.json": `{"dependencies":{"next":"14.0.0"}}`,
	})
	port := detectNodePort(dir)
	if port != 3000 {
		t.Errorf("Next.js port = %d, want 3000", port)
	}
}

func TestDetectNodePort_NuxtProject(t *testing.T) {
	dir := createTempRepo(t, map[string]string{
		"package.json": `{"dependencies":{"nuxt":"3.0.0"}}`,
	})
	port := detectNodePort(dir)
	if port != 3000 {
		t.Errorf("Nuxt port = %d, want 3000", port)
	}
}

func TestDetectNodePort_PortReference(t *testing.T) {
	dir := createTempRepo(t, map[string]string{
		"package.json": `{"port":8080}`,
	})
	port := detectNodePort(dir)
	if port != 3000 {
		t.Errorf("Port reference = %d, want 3000", port)
	}
}

func TestDetectNodePort_Default(t *testing.T) {
	dir := createTempRepo(t, map[string]string{
		"package.json": `{"name":"basic-app","version":"1.0.0"}`,
	})
	port := detectNodePort(dir)
	if port != 3000 {
		t.Errorf("Default port = %d, want 3000", port)
	}
}

func TestDetectNodePort_NoPackageJson(t *testing.T) {
	dir := t.TempDir()
	port := detectNodePort(dir)
	if port != 3000 {
		t.Errorf("Missing package.json port = %d, want 3000", port)
	}
}

func TestDetect_PythonPyproject(t *testing.T) {
	dir := createTempRepo(t, map[string]string{"pyproject.toml": "[project]\nname='test'\n"})
	result := Detect(dir)
	if result.Name != TypePython {
		t.Errorf("Name = %q, want %q", result.Name, TypePython)
	}
	if result.Port != 8000 {
		t.Errorf("Port = %d, want 8000", result.Port)
	}
}

func TestHasFiles_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	if hasFiles(dir, ".xyz") {
		t.Error("Empty dir should have no .xyz files")
	}
}

func TestHasFiles_NonexistentDir(t *testing.T) {
	if hasFiles("/nonexistent/path/that/does/not/exist", ".html") {
		t.Error("Nonexistent dir should return false")
	}
}

func TestDetect_DockerfileDefaultPort(t *testing.T) {
	dir := createTempRepo(t, map[string]string{"Dockerfile": "FROM alpine\nEXPOSE 5000"})
	result := Detect(dir)
	if result.Port != 3000 {
		t.Errorf("Dockerfile default port = %d, want 3000", result.Port)
	}
}

func TestDetect_RubyDefaultPort(t *testing.T) {
	dir := createTempRepo(t, map[string]string{"Gemfile": "source 'https://rubygems.org'"})
	result := Detect(dir)
	if result.Port != 3000 {
		t.Errorf("Ruby default port = %d, want 3000", result.Port)
	}
}

func TestDetect_PHPDefaultPort(t *testing.T) {
	dir := createTempRepo(t, map[string]string{"composer.json": `{}`})
	result := Detect(dir)
	if result.Port != 80 {
		t.Errorf("PHP default port = %d, want 80", result.Port)
	}
}

func TestDetect_StaticDefaultPort(t *testing.T) {
	dir := createTempRepo(t, map[string]string{"index.html": "<html></html>"})
	result := Detect(dir)
	if result.Port != 80 {
		t.Errorf("Static default port = %d, want 80", result.Port)
	}
}

func TestDetect_DetectsDockerfileBeforeNode(t *testing.T) {
	dir := createTempRepo(t, map[string]string{
		"Dockerfile":   "FROM node:22",
		"package.json": `{"name":"test"}`,
		"go.mod":       "module test",
	})
	result := Detect(dir)
	if result.Name != TypeDocker {
		t.Errorf("Should detect Dockerfile first, got %q", result.Name)
	}
}

func TestDetect_DetectsNodeBeforeGo(t *testing.T) {
	dir := createTempRepo(t, map[string]string{
		"package.json":  `{"name":"test"}`,
		"requirements.txt": "flask",
	})
	result := Detect(dir)
	if result.Name != TypeNode {
		t.Errorf("Should detect Node before Python, got %q", result.Name)
	}
}

func TestConstTypes(t *testing.T) {
	if TypeNode != "node" {
		t.Errorf("TypeNode = %q", TypeNode)
	}
	if TypeGo != "go" {
		t.Errorf("TypeGo = %q", TypeGo)
	}
	if TypePHP != "php" {
		t.Errorf("TypePHP = %q", TypePHP)
	}
	if TypePython != "python" {
		t.Errorf("TypePython = %q", TypePython)
	}
	if TypeRuby != "ruby" {
		t.Errorf("TypeRuby = %q", TypeRuby)
	}
	if TypeStatic != "static" {
		t.Errorf("TypeStatic = %q", TypeStatic)
	}
	if TypeDocker != "dockerfile" {
		t.Errorf("TypeDocker = %q", TypeDocker)
	}
}
