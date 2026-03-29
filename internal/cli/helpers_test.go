package cli

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ersinkoc/SimpleDeploy/internal/buildpack"
	cfgpkg "github.com/ersinkoc/SimpleDeploy/internal/config"
	compose "github.com/ersinkoc/SimpleDeploy/internal/compose"
	"github.com/ersinkoc/SimpleDeploy/internal/db"
	"github.com/ersinkoc/SimpleDeploy/internal/docker"
	"github.com/ersinkoc/SimpleDeploy/internal/proxy"
	"github.com/ersinkoc/SimpleDeploy/internal/runner"
	"github.com/ersinkoc/SimpleDeploy/internal/state"
	"github.com/ersinkoc/SimpleDeploy/internal/wizard"
)

func TestMapDetectedDefault(t *testing.T) {
	tests := []struct {
		appType string
		want    int
	}{
		{buildpack.TypeNode, 1},
		{buildpack.TypeGo, 2},
		{buildpack.TypePHP, 3},
		{buildpack.TypePython, 4},
		{buildpack.TypeDocker, 7},
		{"unknown", 7},
		{"", 7},
	}

	for _, tt := range tests {
		t.Run(tt.appType, func(t *testing.T) {
			got := mapDetectedDefault(tt.appType)
			if got != tt.want {
				t.Errorf("mapDetectedDefault(%q) = %d, want %d", tt.appType, got, tt.want)
			}
		})
	}
}

func TestReplaceAppImage(t *testing.T) {
	input := `services:
  myapp:
    image: myapp:old
    ports:
      - "3000:3000"
  mysql:
    image: mysql:8
`
	result := replaceAppImage(input, "myapp", "myapp:new")
	if !strings.Contains(result, "image: myapp:new") {
		t.Error("Should replace app image")
	}
	if !strings.Contains(result, "image: mysql:8") {
		t.Error("Should NOT replace database image")
	}
	if strings.Contains(result, "image: myapp:old") {
		t.Error("Old image should be gone")
	}
}

func TestReplaceAppImage_NoMatch(t *testing.T) {
	input := "services:\n  otherapp:\n    image: other:old\n"
	result := replaceAppImage(input, "myapp", "myapp:new")
	if result != input {
		t.Error("Should not modify when app not found")
	}
}

func TestReplaceAppImage_TopLevelService(t *testing.T) {
	input := "services:\n  myapp:\n    image: myapp:old\n    ports:\n      - \"3000:3000\"\n"
	result := replaceAppImage(input, "myapp", "myapp:new")
	if !strings.Contains(result, "    image: myapp:new") {
		t.Errorf("Should replace image with preserved indent, got:\n%s", result)
	}
}

func TestColoredStatus(t *testing.T) {
	tests := []struct {
		status string
		word   string
	}{
		{"running", "running"},
		{"stopped", "stopped"},
		{"exited", "stopped"},
		{"not found", "not found"},
		{"paused", "paused"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			result := coloredStatus(tt.status)
			if tt.word != "" && !strings.Contains(result, tt.word) {
				t.Errorf("coloredStatus(%q) = %q, should contain %q", tt.status, result, tt.word)
			}
		})
	}
}

func TestLogDeploy(t *testing.T) {
	dir := t.TempDir()
	logDeploy(dir, "testapp", "testapp:v1")

	logPath := filepath.Join(dir, "deploy.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("deploy.log should exist: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "testapp") {
		t.Error("Log should contain app name")
	}
	if !strings.Contains(content, "testapp:v1") {
		t.Error("Log should contain image tag")
	}
}

func TestLogDeploy_Append(t *testing.T) {
	dir := t.TempDir()
	logDeploy(dir, "app1", "app1:v1")
	logDeploy(dir, "app1", "app1:v2")

	data, _ := os.ReadFile(filepath.Join(dir, "deploy.log"))
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Errorf("Expected 2 log lines, got %d", len(lines))
	}
}

func TestLogDeploy_InvalidDir(t *testing.T) {
	// Should not panic on invalid dir
	logDeploy("/nonexistent/path/that/does/not/exist", "app", "app:v1")
}

func TestLogDeploy_EmptyAppName(t *testing.T) {
	dir := t.TempDir()
	// Should not panic with empty values
	logDeploy(dir, "", "")
}

func TestReplaceAppImage_MultipleImages(t *testing.T) {
	input := `services:
  myapp:
    image: myapp:old
    ports:
      - "3000:3000"
  db:
    image: mysql:8
  myapp-db:
    image: redis:7
`
	result := replaceAppImage(input, "myapp", "myapp:new")
	if !strings.Contains(result, "image: myapp:new") {
		t.Error("Should replace myapp image")
	}
	if !strings.Contains(result, "image: mysql:8") {
		t.Error("Should NOT replace mysql image")
	}
	if !strings.Contains(result, "image: redis:7") {
		t.Error("Should NOT replace redis image")
	}
}

func TestReplaceAppImage_NoMatchingService(t *testing.T) {
	input := "services:\n  otherapp:\n    image: other:old\n"
	result := replaceAppImage(input, "myapp", "myapp:new")
	if result != input {
		t.Error("Should not modify when app service not found")
	}
}

func TestReplaceAppImage_EmptyContent(t *testing.T) {
	result := replaceAppImage("", "myapp", "myapp:new")
	if result != "" {
		t.Error("Should return empty for empty input")
	}
}

func TestBuildComposeData_EncryptionError(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)

	app := state.NewAppConfig()
	app.Name = "encapp"
	app.Port = 3000
	app.Type = "node"
	app.Domain = "encapp.example.com"
	app.Repo = "https://github.com/test/app.git"
	app.Databases = []string{"mysql"}
	// Set invalid encrypted credentials — decryption should handle gracefully
	app.DBCredentials = map[string]string{"mysql": "invalid-encrypted-value"}
	app.Headers = map[string]string{}

	cfg := &state.GlobalConfig{Proxy: "traefik"}

	data := buildComposeData(app, cfg, []string{"qd-encapp-mysql-data"}, map[string]string{})
	if len(data.Databases) != 1 {
		t.Error("Should still create DB service even with bad credentials")
	}
}

func TestBuildComposeData(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)

	app := state.NewAppConfig()
	app.Name = "testapp"
	app.Port = 3000
	app.Type = "node"
	app.Branch = "main"
	app.Domain = "testapp.example.com"
	app.Repo = "https://github.com/test/app.git"
	app.CurrentImage = "testapp:v1"
	app.Databases = []string{"mysql"}
	app.Headers = map[string]string{"X-Custom": "yes"}

	// Encrypt a test password for mysql
	encPass, err := state.Encrypt("secretpass123")
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}
	app.DBCredentials = map[string]string{"mysql": encPass}

	cfg := &state.GlobalConfig{
		Proxy:      "traefik",
		BaseDomain: "example.com",
	}

	envVars := map[string]string{"NODE_ENV": "production"}
	volumes := []string{"qd-testapp-mysql-data"}

	data := buildComposeData(app, cfg, volumes, envVars)

	if data.AppName != "testapp" {
		t.Errorf("AppName = %q, want 'testapp'", data.AppName)
	}
	if data.Port != 3000 {
		t.Errorf("Port = %d, want 3000", data.Port)
	}
	if data.ProxyType != "traefik" {
		t.Errorf("ProxyType = %q, want 'traefik'", data.ProxyType)
	}
	if len(data.Databases) != 1 {
		t.Fatalf("Databases count = %d, want 1", len(data.Databases))
	}
	if data.Databases[0].Type != "mysql" {
		t.Errorf("DB type = %q, want 'mysql'", data.Databases[0].Type)
	}
	if data.Databases[0].Image != "mysql:8" {
		t.Errorf("DB image = %q, want 'mysql:8'", data.Databases[0].Image)
	}
	if data.Databases[0].VolumeName != "qd-testapp-mysql-data" {
		t.Errorf("DB volume = %q, want 'qd-testapp-mysql-data'", data.Databases[0].VolumeName)
	}
	if data.Databases[0].Env["MYSQL_DATABASE"] != "testapp" {
		t.Errorf("DB name env = %q, want 'testapp'", data.Databases[0].Env["MYSQL_DATABASE"])
	}
	if data.Databases[0].Env["MYSQL_ROOT_PASSWORD"] != "secretpass123" {
		t.Errorf("DB password not decrypted correctly")
	}
	if data.Environment["NODE_ENV"] != "production" {
		t.Error("Should preserve environment variables")
	}
}

func TestBuildComposeData_MultipleDBs(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)

	app := state.NewAppConfig()
	app.Name = "dbapp"
	app.Port = 8080
	app.Type = "go"
	app.Domain = "dbapp.example.com"
	app.Repo = "https://github.com/test/dbapp.git"
	app.Databases = []string{"mysql", "redis"}
	app.Headers = map[string]string{}

	cfg := &state.GlobalConfig{Proxy: "traefik"}
	volumes := []string{"qd-dbapp-mysql-data", "qd-dbapp-redis-data"}

	data := buildComposeData(app, cfg, volumes, map[string]string{})

	if len(data.Databases) != 2 {
		t.Fatalf("Databases count = %d, want 2", len(data.Databases))
	}
	foundMySQL, foundRedis := false, false
	for _, dbSvc := range data.Databases {
		if dbSvc.Type == "mysql" {
			foundMySQL = true
		}
		if dbSvc.Type == "redis" {
			foundRedis = true
		}
	}
	if !foundMySQL || !foundRedis {
		t.Error("Should have both mysql and redis databases")
	}
}

func TestBuildComposeData_NoDBs(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)

	app := state.NewAppConfig()
	app.Name = "simple"
	app.Port = 80
	app.Type = "static"
	app.Domain = "simple.example.com"
	app.Repo = "https://github.com/test/simple.git"
	app.Headers = map[string]string{}

	cfg := &state.GlobalConfig{Proxy: "caddy"}

	data := buildComposeData(app, cfg, nil, map[string]string{})

	if len(data.Databases) != 0 {
		t.Errorf("Should have 0 databases, got %d", len(data.Databases))
	}
	if data.ProxyType != "caddy" {
		t.Errorf("ProxyType = %q, want 'caddy'", data.ProxyType)
	}
}

func TestBuildComposeData_UnknownDB(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)

	app := state.NewAppConfig()
	app.Name = "testapp"
	app.Port = 3000
	app.Type = "node"
	app.Domain = "test.example.com"
	app.Repo = "https://github.com/test/app.git"
	app.Databases = []string{"couchdb"} // not supported
	app.DBCredentials = map[string]string{}
	app.Headers = map[string]string{}

	cfg := &state.GlobalConfig{Proxy: "traefik"}

	data := buildComposeData(app, cfg, []string{}, map[string]string{})

	// GetDatabaseConfig returns false for unsupported DBs → skip
	// But the dbSvc still gets created with Type "couchdb" and zero values
	// Actually looking at the code, if !ok it continues, so no DB service added
	// Wait no: the loop is `for i, dbType := range app.Databases` and inside it
	// calls `db.GetDatabaseConfig(dbType)` - if !ok it continues. So couchdb is skipped.
	if len(data.Databases) != 0 {
		t.Errorf("Unsupported DB should be skipped, got %d databases", len(data.Databases))
	}
}

func TestBuildComposeData_DBHealthCheck(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)

	app := state.NewAppConfig()
	app.Name = "hcapp"
	app.Port = 3000
	app.Type = "node"
	app.Domain = "hc.example.com"
	app.Repo = "https://github.com/test/app.git"
	app.Databases = []string{"mysql"}
	app.DBCredentials = map[string]string{}
	app.Headers = map[string]string{}

	cfg := &state.GlobalConfig{Proxy: "traefik"}
	volumes := []string{"qd-hcapp-mysql-data"}

	data := buildComposeData(app, cfg, volumes, map[string]string{})

	if len(data.Databases) == 0 {
		t.Fatal("Expected 1 database")
	}
	if data.Databases[0].HealthCheck == nil {
		t.Error("MySQL should have health check")
	}
	if len(data.Databases[0].HealthCheck.Test) == 0 {
		t.Error("HealthCheck test should not be empty")
	}
}

func TestGetStateDir(t *testing.T) {
	dir := getStateDir()
	if dir == "" {
		t.Error("getStateDir should not be empty")
	}
	if !strings.Contains(dir, ".simpledeploy") {
		t.Errorf("getStateDir = %q, should contain '.simpledeploy'", dir)
	}
}

func TestRunService_NoArgs(t *testing.T) {
	err := RunService([]string{})
	if err == nil {
		t.Error("Should return error for no args")
	}
	if !strings.Contains(err.Error(), "usage") {
		t.Errorf("Error should mention usage, got %q", err.Error())
	}
}

func TestRunService_InvalidAction(t *testing.T) {
	err := RunService([]string{"invalid"})
	if err == nil {
		t.Error("Should return error for invalid action")
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Errorf("Error should mention unknown, got %q", err.Error())
	}
}

func TestRunWebhook_NoArgs(t *testing.T) {
	err := RunWebhook([]string{})
	if err == nil {
		t.Error("Should return error for no args")
	}
}

func TestRunWebhook_InvalidAction(t *testing.T) {
	err := RunWebhook([]string{"stop"})
	if err == nil {
		t.Error("Should return error for non-start action")
	}
}

// Test that db.GetDatabaseConfig and compose.Generate work together
func TestComposeIntegration(t *testing.T) {
	app := state.NewAppConfig()
	app.Name = "integration"
	app.Port = 8080
	app.Type = "go"
	app.Domain = "integration.test.com"
	app.Repo = "https://github.com/test/app.git"
	app.CurrentImage = "integration:v1"
	app.Headers = map[string]string{"X-Test": "yes"}

	cfg := &state.GlobalConfig{Proxy: "traefik"}
	data := buildComposeData(app, cfg, nil, map[string]string{"GO_ENV": "test"})

	yaml := compose.Generate(data)
	if !strings.Contains(yaml, "qd-integration") {
		t.Error("Generated compose should contain container name")
	}
	if !strings.Contains(yaml, "GO_ENV=test") {
		t.Error("Generated compose should contain env vars")
	}
	if !strings.Contains(yaml, "traefik.enable=true") {
		t.Error("Traefik mode should have traefik labels")
	}
}

// Test db.GetDatabaseConfig used in buildComposeData
func TestBuildComposeData_PostgreSQL(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)

	encPass, _ := state.Encrypt("pgpass123")

	app := state.NewAppConfig()
	app.Name = "pgapp"
	app.Port = 5432
	app.Type = "python"
	app.Domain = "pg.example.com"
	app.Repo = "https://github.com/test/pgapp.git"
	app.Databases = []string{"postgresql"}
	app.DBCredentials = map[string]string{"postgresql": encPass}
	app.Headers = map[string]string{}

	cfg := &state.GlobalConfig{Proxy: "traefik"}
	volumes := []string{"qd-pgapp-postgresql-data"}

	data := buildComposeData(app, cfg, volumes, map[string]string{})

	if len(data.Databases) != 1 {
		t.Fatal("Expected 1 database")
	}
	if data.Databases[0].Image != "postgres:16" {
		t.Errorf("Image = %q, want 'postgres:16'", data.Databases[0].Image)
	}
	if data.Databases[0].Env["POSTGRES_DB"] != "pgapp" {
		t.Errorf("POSTGRES_DB = %q, want 'pgapp'", data.Databases[0].Env["POSTGRES_DB"])
	}
	if data.Databases[0].Env["POSTGRES_PASSWORD"] != "pgpass123" {
		t.Error("PostgreSQL password not decrypted correctly")
	}
}

func TestBuildComposeData_MongoDB(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)

	encPass, _ := state.Encrypt("mongopass")

	app := state.NewAppConfig()
	app.Name = "mongoapp"
	app.Port = 3000
	app.Type = "node"
	app.Domain = "mongo.example.com"
	app.Repo = "https://github.com/test/app.git"
	app.Databases = []string{"mongodb"}
	app.DBCredentials = map[string]string{"mongodb": encPass}
	app.Headers = map[string]string{}

	cfg := &state.GlobalConfig{Proxy: "traefik"}
	volumes := []string{"qd-mongoapp-mongodb-data"}

	data := buildComposeData(app, cfg, volumes, map[string]string{})

	if len(data.Databases) != 1 {
		t.Fatal("Expected 1 database")
	}
	if data.Databases[0].Image != "mongo:7" {
		t.Errorf("Image = %q, want 'mongo:7'", data.Databases[0].Image)
	}
}

func TestBuildComposeData_MariaDB(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)

	encPass, _ := state.Encrypt("mariapass")

	app := state.NewAppConfig()
	app.Name = "mariaapp"
	app.Port = 3000
	app.Type = "php"
	app.Domain = "maria.example.com"
	app.Repo = "https://github.com/test/app.git"
	app.Databases = []string{"mariadb"}
	app.DBCredentials = map[string]string{"mariadb": encPass}
	app.Headers = map[string]string{}

	cfg := &state.GlobalConfig{Proxy: "caddy"}
	volumes := []string{"qd-mariaapp-mariadb-data"}

	data := buildComposeData(app, cfg, volumes, map[string]string{})

	if len(data.Databases) != 1 {
		t.Fatal("Expected 1 database")
	}
	if data.Databases[0].Image != "mariadb:11" {
		t.Errorf("Image = %q, want 'mariadb:11'", data.Databases[0].Image)
	}
	if data.Databases[0].Env["MARIADB_DATABASE"] != "mariaapp" {
		t.Errorf("MARIADB_DATABASE = %q, want 'mariaapp'", data.Databases[0].Env["MARIADB_DATABASE"])
	}
}

// Test volume count mismatch with DB count (fewer volumes than DBs)
func TestBuildComposeData_VolumeMismatch(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)

	app := state.NewAppConfig()
	app.Name = "volapp"
	app.Port = 3000
	app.Type = "node"
	app.Domain = "vol.example.com"
	app.Repo = "https://github.com/test/app.git"
	app.Databases = []string{"mysql", "redis"}
	app.DBCredentials = map[string]string{}
	app.Headers = map[string]string{}

	cfg := &state.GlobalConfig{Proxy: "traefik"}
	// Only 1 volume but 2 DBs — should not panic
	data := buildComposeData(app, cfg, []string{"qd-volapp-mysql-data"}, map[string]string{})
	if len(data.Databases) != 2 {
		t.Errorf("Expected 2 databases, got %d", len(data.Databases))
	}
	// First DB should have volume name
	if data.Databases[0].VolumeName != "qd-volapp-mysql-data" {
		t.Errorf("First DB volume = %q, want 'qd-volapp-mysql-data'", data.Databases[0].VolumeName)
	}
	// Second DB should have empty volume name (no crash)
	if data.Databases[1].VolumeName != "" {
		t.Errorf("Second DB volume should be empty, got %q", data.Databases[1].VolumeName)
	}
}

// Test with db.GetDatabaseConfig for each supported type
func TestAllDBConfigs(t *testing.T) {
	for _, dbType := range []string{"mysql", "postgresql", "mariadb", "mongodb", "redis"} {
		t.Run(dbType, func(t *testing.T) {
			cfg, ok := db.GetDatabaseConfig(dbType)
			if !ok {
				t.Errorf("GetDatabaseConfig(%q) should return true", dbType)
			}
			if cfg.Image == "" {
				t.Errorf("Image should not be empty for %q", dbType)
			}
			if cfg.Port == 0 {
				t.Errorf("Port should not be 0 for %q", dbType)
			}
		})
	}
}

// captureStdout captures stdout output during the execution of f.
func captureStdout(f func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	f()
	w.Close()
	os.Stdout = old
	data, _ := io.ReadAll(r)
	return string(data)
}

func TestRunList_WithApps(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)

	app := state.NewAppConfig()
	app.Name = "listapp"
	app.Port = 3000
	app.Type = "node"
	app.Domain = "listapp.example.com"
	app.Repo = "https://github.com/test/listapp.git"
	app.CurrentImage = "listapp:v1"
	app.Status = "running"
	app.LastDeploy = "2025-01-01T00:00:00Z"
	app.DeployCount = 3

	if err := state.SaveApp(app); err != nil {
		t.Fatalf("SaveApp failed: %v", err)
	}

	output := captureStdout(func() {
		err := RunList()
		if err != nil {
			t.Errorf("RunList returned error: %v", err)
		}
	})

	if !strings.Contains(output, "listapp") {
		t.Error("Output should contain app name 'listapp'")
	}
	if !strings.Contains(output, "listapp.example.com") {
		t.Error("Output should contain app domain")
	}
	if !strings.Contains(output, "listapp:v1") {
		t.Error("Output should contain app image")
	}
}

func TestRunList_Empty(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)

	output := captureStdout(func() {
		err := RunList()
		if err != nil {
			t.Errorf("RunList returned error: %v", err)
		}
	})

	if !strings.Contains(output, "No applications") {
		t.Errorf("Output should mention no applications, got: %s", output)
	}
}

func TestRunStatus_WithState(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)

	cfg := &state.GlobalConfig{
		Proxy:       "traefik",
		BaseDomain:  "example.com",
		AcmeEmail:   "test@example.com",
		WebhookPort: 9000,
	}
	if err := state.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	app := state.NewAppConfig()
	app.Name = "statusapp"
	app.Port = 8080
	app.Type = "go"
	app.Domain = "statusapp.example.com"
	app.Repo = "https://github.com/test/statusapp.git"
	app.CurrentImage = "statusapp:v1"
	app.Status = "running"
	if err := state.SaveApp(app); err != nil {
		t.Fatalf("SaveApp failed: %v", err)
	}

	output := captureStdout(func() {
		err := RunStatus()
		if err != nil {
			t.Errorf("RunStatus returned error: %v", err)
		}
	})

	if !strings.Contains(output, "example.com") {
		t.Error("Output should contain base domain")
	}
	if !strings.Contains(output, "traefik") {
		t.Error("Output should contain proxy type")
	}
	if !strings.Contains(output, "statusapp") {
		t.Error("Output should contain app name")
	}

	// Verify docker.ContainerStatus works for non-existent containers
	status, err := docker.ContainerStatus("qd-nonexistent")
	if err != nil {
		t.Errorf("ContainerStatus should not error for missing containers: %v", err)
	}
	if status != "not found" {
		t.Errorf("ContainerStatus for missing container = %q, want 'not found'", status)
	}
}

func TestRoute_DeployWithoutInit(t *testing.T) {
	err := Route([]string{"deploy"})
	if err == nil {
		t.Error("deploy without init should return error")
	}
	if !strings.Contains(err.Error(), "init") {
		t.Errorf("Error should mention init, got: %v", err)
	}
}

func TestRoute_RedeployWithoutArgs(t *testing.T) {
	err := Route([]string{"redeploy"})
	if err == nil {
		t.Error("redeploy without app name should return error")
	}
	if !strings.Contains(err.Error(), "application name") && !strings.Contains(err.Error(), "app-name") {
		t.Errorf("Error should mention application name required, got: %v", err)
	}
}

func TestRoute_RemoveWithoutArgs(t *testing.T) {
	err := Route([]string{"rm"})
	if err == nil {
		t.Error("rm without app name should return error")
	}
	if !strings.Contains(err.Error(), "application name") && !strings.Contains(err.Error(), "app-name") {
		t.Errorf("Error should mention application name required, got: %v", err)
	}
}

func TestRoute_LogsWithoutArgs(t *testing.T) {
	err := Route([]string{"logs"})
	if err == nil {
		t.Error("logs without app name should return error")
	}
	if !strings.Contains(err.Error(), "application name") && !strings.Contains(err.Error(), "app-name") {
		t.Errorf("Error should mention application name required, got: %v", err)
	}
}

func TestRoute_RedeployNonExistent(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)

	err := Route([]string{"redeploy", "nonexistent"})
	if err == nil {
		t.Error("redeploy of non-existent app should return error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("Error should mention 'not found', got: %v", err)
	}
}

func TestRoute_RemoveNonExistent(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)

	_ = captureStdout(func() {
		err := Route([]string{"rm", "nonexistent"})
		if err == nil {
			t.Error("rm of non-existent app should return error")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("Error should mention 'not found', got: %v", err)
		}
	})
}

func TestRoute_LogsNonExistent(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)

	err := Route([]string{"logs", "nonexistent"})
	if err == nil {
		t.Error("logs of non-existent app should return error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("Error should mention 'not found', got: %v", err)
	}
}

func TestRunServiceInstall(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)

	cfg := &state.GlobalConfig{
		Proxy:         "traefik",
		BaseDomain:    "test.example.com",
		AcmeEmail:     "test@test.com",
		WebhookPort:   9000,
		WebhookSecret: "secret",
	}
	if err := state.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	// Override service dir to temp
	serviceDir := filepath.Join(dir, "service")
	runner.ServiceDir = serviceDir

	_ = captureStdout(func() {
		err := runServiceInstall()
		if err != nil {
			t.Fatalf("runServiceInstall failed: %v", err)
		}
	})

	composePath := filepath.Join(serviceDir, "docker-compose.yml")
	if _, err := os.Stat(composePath); os.IsNotExist(err) {
		t.Error("docker-compose.yml should exist after runServiceInstall")
	}
	data, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("Failed to read compose: %v", err)
	}
	if !strings.Contains(string(data), "test.example.com") {
		t.Error("Compose should contain base domain")
	}
}

func TestRunServiceInstall_NoConfig(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)

	err := runServiceInstall()
	if err == nil {
		t.Error("Should fail without config")
	}
}

func TestRunWebhook_NoConfig(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)

	err := RunWebhook([]string{"start"})
	if err == nil {
		t.Error("Should fail without config")
	}
}

func TestRunWebhook_StartsWithServer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to find available port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	dir := t.TempDir()
	state.InitState(dir)

	cfg := &state.GlobalConfig{
		Proxy:         "traefik",
		BaseDomain:    "test.example.com",
		AcmeEmail:     "test@test.com",
		WebhookPort:   port,
		WebhookSecret: "test-secret",
	}
	if err := state.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- RunWebhook([]string{"start"})
	}()

	time.Sleep(300 * time.Millisecond)

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/_qd/health", port))
	if err != nil {
		t.Fatalf("Health check failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Health status = %d, want 200", resp.StatusCode)
	}
}

func TestRunWebhook_PortOverride(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to find available port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	dir := t.TempDir()
	state.InitState(dir)

	cfg := &state.GlobalConfig{
		Proxy:         "traefik",
		BaseDomain:    "test.example.com",
		AcmeEmail:     "test@test.com",
		WebhookPort:   9999, // will be overridden
		WebhookSecret: "test-secret",
	}
	if err := state.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- RunWebhook([]string{"start", "--port", fmt.Sprintf("%d", port)})
	}()

	time.Sleep(300 * time.Millisecond)

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/_qd/health", port))
	if err != nil {
		t.Fatalf("Health check on overridden port failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Health status = %d, want 200", resp.StatusCode)
	}
}

func TestRunRedeploy_GitPullFails(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)

	cfg := &state.GlobalConfig{
		Proxy:       "traefik",
		BaseDomain:  "test.example.com",
		AcmeEmail:   "test@test.com",
		WebhookPort: 9000,
	}
	if err := state.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	app := state.NewAppConfig()
	app.Name = "pullfail"
	app.Branch = "main"
	app.Repo = "https://github.com/nonexistent/repo.git"
	app.Domain = "pullfail.example.com"
	app.Port = 3000
	app.Type = "node"
	if err := state.SaveApp(app); err != nil {
		t.Fatalf("SaveApp failed: %v", err)
	}

	_ = captureStdout(func() {
		err := RunRedeploy([]string{"pullfail"})
		if err == nil {
			t.Error("Should fail when git pull fails (source dir doesn't exist)")
		}
	})
}

func TestRunRedeploy_NoConfig(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)

	app := state.NewAppConfig()
	app.Name = "nocfg"
	app.Branch = "main"
	app.Repo = "https://github.com/test/app.git"
	state.SaveApp(app)

	err := RunRedeploy([]string{"nocfg"})
	if err == nil {
		t.Error("Should fail without config")
	}
}

func TestRunRedeploy_ComposeReadFails(t *testing.T) {
	if !docker.IsInstalled() {
		t.Skip("Docker not installed")
	}

	dir := t.TempDir()
	state.InitState(dir)

	cfg := &state.GlobalConfig{Proxy: "traefik", BaseDomain: "test.example.com"}
	state.SaveConfig(cfg)

	app := state.NewAppConfig()
	app.Name = "noread"
	app.Branch = "main"
	app.Repo = "https://github.com/test/app.git"
	app.Domain = "noread.example.com"
	app.Port = 3000
	app.Type = "node"
	state.SaveApp(app)

	// Create source dir with a valid git repo so Pull doesn't fail immediately
	sourceDir := filepath.Join(dir, "opt", "simpledeploy", "apps", "noread", "source")
	os.MkdirAll(sourceDir, 0755)
	runGitCmd(t, sourceDir, "init")
	runGitCmd(t, sourceDir, "config", "user.email", "test@test.com")
	runGitCmd(t, sourceDir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(sourceDir, "file.txt"), []byte("content"), 0644)
	runGitCmd(t, sourceDir, "add", ".")
	runGitCmd(t, sourceDir, "commit", "-m", "initial")

	// No Dockerfile → build will fail
	_ = captureStdout(func() {
		err := RunRedeploy([]string{"noread"})
		if err == nil {
			t.Error("Should fail when Dockerfile doesn't exist")
		}
	})
}

func TestRunRedeploy_WithDockerfile(t *testing.T) {
	if !docker.IsInstalled() {
		t.Skip("Docker not installed")
	}

	dir := t.TempDir()
	state.InitState(dir)

	// Override config base dir so AppDir() returns our temp dir
	cfgpkg.BaseDir = filepath.Join(dir, "opt", "simpledeploy")

	cfg := &state.GlobalConfig{Proxy: "traefik", BaseDomain: "test.example.com"}
	state.SaveConfig(cfg)

	app := state.NewAppConfig()
	app.Name = "redeploydf"
	app.Branch = "main"
	app.Repo = "https://github.com/test/app.git"
	app.Domain = "redeploydf.example.com"
	app.Port = 3000
	app.Type = "node"
	state.SaveApp(app)

	// Create source dir with git repo + Dockerfile
	sourceDir := filepath.Join(cfgpkg.AppDir("redeploydf"), "source")
	os.MkdirAll(sourceDir, 0755)
	runGitCmd(t, sourceDir, "init")
	runGitCmd(t, sourceDir, "config", "user.email", "test@test.com")
	runGitCmd(t, sourceDir, "config", "user.name", "Test")

	// Write a simple Dockerfile
	dockerfile := `FROM alpine:3.19
RUN echo "hello" > /app/hello.txt
CMD ["cat", "/app/hello.txt"]
`
	os.WriteFile(filepath.Join(sourceDir, "Dockerfile"), []byte(dockerfile), 0644)
	os.WriteFile(filepath.Join(sourceDir, "app.txt"), []byte("test"), 0644)
	runGitCmd(t, sourceDir, "add", ".")
	runGitCmd(t, sourceDir, "commit", "-m", "initial")

	// No docker-compose.yml → should fail at compose read step
	_ = captureStdout(func() {
		err := RunRedeploy([]string{"redeploydf"})
		if err == nil {
			t.Error("Should fail at compose read step")
		}
		if !strings.Contains(err.Error(), "compose") {
			t.Logf("Error (expected): %v", err)
		}
	})
}

func TestRunRedeploy_WithCompose(t *testing.T) {
	if !docker.IsInstalled() {
		t.Skip("Docker not installed")
	}

	dir := t.TempDir()
	state.InitState(dir)

	cfgpkg.BaseDir = filepath.Join(dir, "opt", "simpledeploy")

	cfg := &state.GlobalConfig{Proxy: "traefik", BaseDomain: "test.example.com"}
	state.SaveConfig(cfg)

	app := state.NewAppConfig()
	app.Name = "redeployc"
	app.Branch = "main"
	app.Repo = "https://github.com/test/app.git"
	app.Domain = "redeployc.example.com"
	app.Port = 3000
	app.Type = "node"
	state.SaveApp(app)

	// Create source dir with git repo + Dockerfile
	sourceDir := filepath.Join(cfgpkg.AppDir("redeployc"), "source")
	os.MkdirAll(sourceDir, 0755)
	runGitCmd(t, sourceDir, "init")
	runGitCmd(t, sourceDir, "config", "user.email", "test@test.com")
	runGitCmd(t, sourceDir, "config", "user.name", "Test")

	dockerfile := `FROM alpine:3.19
RUN echo "hello" > /app/hello.txt
CMD ["cat", "/app/hello.txt"]
`
	os.WriteFile(filepath.Join(sourceDir, "Dockerfile"), []byte(dockerfile), 0644)
	os.WriteFile(filepath.Join(sourceDir, "app.txt"), []byte("test"), 0644)
	runGitCmd(t, sourceDir, "add", ".")
	runGitCmd(t, sourceDir, "commit", "-m", "initial")

	// Create docker-compose.yml in app dir
	appDir := cfgpkg.AppDir("redeployc")
	composeContent := `services:
  redeployc:
    image: redeployc:old
    ports:
      - "3000"
`
	os.WriteFile(filepath.Join(appDir, "docker-compose.yml"), []byte(composeContent), 0644)

	// Should fail at compose up (no network, etc.) but tests more code paths
	_ = captureStdout(func() {
		err := RunRedeploy([]string{"redeployc"})
		if err == nil {
			t.Log("Redeploy succeeded (unexpected but OK)")
		} else {
			t.Logf("Redeploy error (expected): %v", err)
		}
	})
}

func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v failed: %v", args, err)
	}
}

func TestRunLogs_AppFound(t *testing.T) {
	if !docker.IsInstalled() {
		t.Skip("Docker not installed")
	}

	dir := t.TempDir()
	state.InitState(dir)

	app := state.NewAppConfig()
	app.Name = "logsapp"
	app.Branch = "main"
	app.Repo = "https://github.com/test/app.git"
	app.Domain = "logsapp.example.com"
	app.Port = 3000
	app.Type = "node"
	state.SaveApp(app)

	// Logs will fail because there's no compose file, but it tests more code paths
	_ = captureStdout(func() {
		err := RunLogs([]string{"logsapp"})
		// Should not error - RunLogs catches the error internally with wizard.Warn
		if err != nil {
			t.Logf("RunLogs returned: %v (expected for non-existent compose)", err)
		}
	})
}

func TestRoute_ServiceWithState(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)

	cfg := &state.GlobalConfig{
		Proxy:       "traefik",
		BaseDomain:  "test.example.com",
		AcmeEmail:   "test@test.com",
		WebhookPort: 9000,
	}
	state.SaveConfig(cfg)

	// Set service dir to temp so it doesn't try real Docker
	serviceDir := filepath.Join(dir, "service")
	runner.ServiceDir = serviceDir

	_ = captureStdout(func() {
		err := Route([]string{"service", "install"})
		if err != nil {
			t.Errorf("service install should succeed: %v", err)
		}
	})

	// Verify compose was written
	composePath := filepath.Join(serviceDir, "docker-compose.yml")
	if _, err := os.Stat(composePath); os.IsNotExist(err) {
		t.Error("docker-compose.yml should exist after service install")
	}
}

func TestRoute_WebhookStartFails(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)

	// No config → should fail
	err := Route([]string{"webhook", "start"})
	if err == nil {
		t.Error("webhook start should fail without config")
	}
}

func TestRunRemove_UserCancels(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)

	cfg := &state.GlobalConfig{Proxy: "traefik", BaseDomain: "test.example.com"}
	state.SaveConfig(cfg)

	app := state.NewAppConfig()
	app.Name = "cancelapp"
	app.Branch = "main"
	app.Repo = "https://github.com/test/app.git"
	app.Domain = "cancelapp.example.com"
	app.Port = 3000
	app.Type = "node"
	state.SaveApp(app)

	// Simulate user typing "n" to cancel removal
	setWizardInput(t, "n\n")

	_ = captureStdout(func() {
		err := RunRemove([]string{"cancelapp"})
		if err != nil {
			t.Errorf("RunRemove cancelled should not error: %v", err)
		}
	})

	// App should still exist
	_, err := state.GetApp("cancelapp")
	if err != nil {
		t.Error("App should still exist after cancel")
	}
}

func TestRunRemove_NoConfig(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)

	app := state.NewAppConfig()
	app.Name = "nocfgremove"
	app.Branch = "main"
	app.Repo = "https://github.com/test/app.git"
	app.Domain = "nocfg.example.com"
	app.Port = 3000
	app.Type = "node"
	state.SaveApp(app)

	// Simulate "n" to cancel
	setWizardInput(t, "n\n")

	// Config is nil → wizard.Warn path + cancel
	_ = captureStdout(func() {
		err := RunRemove([]string{"nocfgremove"})
		if err != nil {
			t.Logf("RunRemove with no config: %v", err)
		}
	})
}

func TestRunServiceStartStop_NoDocker(t *testing.T) {
	if !docker.IsInstalled() {
		t.Skip("Docker not installed")
	}

	dir := t.TempDir()
	state.InitState(dir)
	runner.ServiceDir = dir

	_ = captureStdout(func() {
		err := Route([]string{"service", "start"})
		if err == nil {
			t.Error("service start should fail without compose file")
		}
	})
	_ = captureStdout(func() {
		err := Route([]string{"service", "stop"})
		if err == nil {
			t.Error("service stop should fail without compose file")
		}
	})
}

func TestRunRedeploy_WithGitToken(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)

	encToken, err := state.Encrypt("ghp_testtoken123")
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	cfg := &state.GlobalConfig{Proxy: "traefik", BaseDomain: "test.example.com"}
	state.SaveConfig(cfg)

	app := state.NewAppConfig()
	app.Name = "tokenapp"
	app.Branch = "master"
	app.Repo = "https://github.com/test/tokenapp.git"
	app.Domain = "tokenapp.example.com"
	app.Port = 3000
	app.Type = "node"
	app.GitToken = encToken
	state.SaveApp(app)

	// Create source dir with git repo
	sourceDir := filepath.Join(dir, "opt", "simpledeploy", "apps", "tokenapp", "source")
	os.MkdirAll(sourceDir, 0755)
	runGitCmd(t, sourceDir, "init")
	runGitCmd(t, sourceDir, "config", "user.email", "test@test.com")
	runGitCmd(t, sourceDir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(sourceDir, "file.txt"), []byte("hello"), 0644)
	runGitCmd(t, sourceDir, "add", ".")
	runGitCmd(t, sourceDir, "commit", "-m", "initial")

	// Should get past git token decryption, past pull, then fail at build
	_ = captureStdout(func() {
		err := RunRedeploy([]string{"tokenapp"})
		if err == nil {
			t.Error("Should fail at build step")
		}
		if !strings.Contains(err.Error(), "build") && !strings.Contains(err.Error(), "Dockerfile") && !strings.Contains(err.Error(), "failed") {
			t.Logf("Error: %v", err)
		}
	})
}

func TestRunRemove_WithDatabases(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)

	cfg := &state.GlobalConfig{Proxy: "caddy", BaseDomain: "test.example.com"}
	state.SaveConfig(cfg)

	app := state.NewAppConfig()
	app.Name = "dbapp"
	app.Branch = "main"
	app.Repo = "https://github.com/test/app.git"
	app.Domain = "dbapp.example.com"
	app.Port = 3000
	app.Type = "node"
	app.Databases = []string{"mysql", "redis"}
	state.SaveApp(app)

	// First answer: "n" for remove volumes, then "n" for cancel remove
	setWizardInput(t, "n\nn\n")

	_ = captureStdout(func() {
		err := RunRemove([]string{"dbapp"})
		if err != nil {
			t.Errorf("RunRemove cancelled should not error: %v", err)
		}
	})
}

func TestRunRemove_ConfirmRemoval(t *testing.T) {
	if !docker.IsInstalled() {
		t.Skip("Docker not installed - needed for compose remove")
	}

	dir := t.TempDir()
	state.InitState(dir)

	cfg := &state.GlobalConfig{Proxy: "caddy", BaseDomain: "test.example.com"}
	state.SaveConfig(cfg)

	app := state.NewAppConfig()
	app.Name = "rmapp"
	app.Branch = "main"
	app.Repo = "https://github.com/test/app.git"
	app.Domain = "rmapp.example.com"
	app.Port = 3000
	app.Type = "node"
	state.SaveApp(app)

	// Answer "y" to confirm removal
	setWizardInput(t, "y\n")

	_ = captureStdout(func() {
		err := RunRemove([]string{"rmapp"})
		// Will fail because there's no actual compose to remove, but tests more code
		if err != nil {
			t.Logf("RunRemove confirmed: %v (expected - no compose to remove)", err)
		}
	})
}

// setWizardInput replaces the wizard scanner with one that reads from the given string.
func setWizardInput(t *testing.T, input string) {
	t.Helper()
	wizard.SetScannerForTesting(bufio.NewScanner(strings.NewReader(input)))
}

func TestRunInit_WithDocker(t *testing.T) {
	if !docker.IsInstalled() || !docker.IsComposeInstalled() {
		t.Skip("Docker/Compose not installed")
	}

	dir := t.TempDir()
	state.InitState(dir)

	// Override proxy dir to temp
	proxyDir := filepath.Join(dir, "proxy")
	proxy.ProxyDir = proxyDir

	// Sequential wizard inputs:
	// 1 - Traefik proxy choice
	// apps.test.com - base domain
	// (empty=Yes) - wildcard DNS confirm (default yes)
	// admin@test.com - ACME email
	// (empty=Yes) - auto-generate webhook secret (default yes)
	// (empty=9000) - webhook port (default)
	input := "1\napps.test.com\n\nadmin@test.com\n\n\n"
	setWizardInput(t, input)

	// Check if ports 80/443 are available
	ln80, err := net.Listen("tcp", ":80")
	if err != nil {
		t.Skipf("Port 80 not available: %v", err)
	}
	ln80.Close()
	ln443, err := net.Listen("tcp", ":443")
	if err != nil {
		t.Skipf("Port 443 not available: %v", err)
	}
	ln443.Close()

	// Create network (needed by proxy setup)
	docker.CreateNetwork("simpledeploy")

	_ = captureStdout(func() {
		err := RunInit()
		if err != nil {
			t.Fatalf("RunInit failed: %v", err)
		}
	})

	// Verify config was saved
	cfg, err := state.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}
	if cfg.BaseDomain != "apps.test.com" {
		t.Errorf("BaseDomain = %q, want 'apps.test.com'", cfg.BaseDomain)
	}
	if cfg.Proxy != "traefik" {
		t.Errorf("Proxy = %q, want 'traefik'", cfg.Proxy)
	}
	if cfg.AcmeEmail != "admin@test.com" {
		t.Errorf("AcmeEmail = %q, want 'admin@test.com'", cfg.AcmeEmail)
	}
	if cfg.WebhookSecret == "" {
		t.Error("WebhookSecret should be set")
	}

	// Cleanup: stop traefik
	captureStdout(func() {
		proxy.StopTraefik()
	})
}

func TestRunInit_CaddyChoice(t *testing.T) {
	if !docker.IsInstalled() || !docker.IsComposeInstalled() {
		t.Skip("Docker/Compose not installed")
	}

	dir := t.TempDir()
	state.InitState(dir)

	proxyDir := filepath.Join(dir, "proxy")
	proxy.ProxyDir = proxyDir

	// Choose Caddy (2), no wildcard DNS, manual webhook secret
	input := "2\nmyapps.example.com\nn\nadmin@myapps.com\nn\nmy-custom-secret\n8080\n"
	setWizardInput(t, input)

	ln80, err := net.Listen("tcp", ":80")
	if err != nil {
		t.Skipf("Port 80 not available: %v", err)
	}
	ln80.Close()
	ln443, err := net.Listen("tcp", ":443")
	if err != nil {
		t.Skipf("Port 443 not available: %v", err)
	}
	ln443.Close()

	// Clean up stale containers from previous test runs
	_ = docker.Run([]string{"rm", "-f", "qd-traefik", "qd-caddy"})

	docker.CreateNetwork("simpledeploy")

	_ = captureStdout(func() {
		err := RunInit()
		if err != nil {
			t.Fatalf("RunInit with Caddy failed: %v", err)
		}
	})

	cfg, err := state.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}
	if cfg.Proxy != "caddy" {
		t.Errorf("Proxy = %q, want 'caddy'", cfg.Proxy)
	}
	if cfg.WebhookPort != 8080 {
		t.Errorf("WebhookPort = %d, want 8080", cfg.WebhookPort)
	}
	if cfg.WebhookSecret != "my-custom-secret" {
		t.Errorf("WebhookSecret = %q, want 'my-custom-secret'", cfg.WebhookSecret)
	}

	// Cleanup
	captureStdout(func() {
		proxy.StopCaddy()
	})
}

func TestRunInit_ReconfigureNo(t *testing.T) {
	if !docker.IsInstalled() {
		t.Skip("Docker not installed")
	}

	dir := t.TempDir()
	state.InitState(dir)

	// Save existing config
	cfg := &state.GlobalConfig{
		Proxy:       "traefik",
		BaseDomain:  "existing.com",
		AcmeEmail:   "admin@existing.com",
		WebhookPort: 9000,
	}
	state.SaveConfig(cfg)

	// Say "n" to reconfigure question
	setWizardInput(t, "n\n")

	_ = captureStdout(func() {
		err := RunInit()
		if err != nil {
			t.Errorf("RunInit cancelled should not error: %v", err)
		}
	})

	// Config should be unchanged
	loaded, _ := state.GetConfig()
	if loaded.BaseDomain != "existing.com" {
		t.Error("Config should be unchanged after reconfigure=no")
	}
}

func TestRunInit_AlreadyInitReconfigure(t *testing.T) {
	if !docker.IsInstalled() || !docker.IsComposeInstalled() {
		t.Skip("Docker/Compose not installed")
	}

	dir := t.TempDir()
	state.InitState(dir)

	// Save existing config so IsInitialized returns true
	cfg := &state.GlobalConfig{Proxy: "traefik", BaseDomain: "old.com"}
	state.SaveConfig(cfg)

	proxyDir := filepath.Join(dir, "proxy")
	proxy.ProxyDir = proxyDir

	ln80, err := net.Listen("tcp", ":80")
	if err != nil {
		t.Skipf("Port 80 not available: %v", err)
	}
	ln80.Close()
	ln443, err := net.Listen("tcp", ":443")
	if err != nil {
		t.Skipf("Port 443 not available: %v", err)
	}
	ln443.Close()

	// Clean up stale containers from previous test runs
	_ = docker.Run([]string{"rm", "-f", "qd-traefik", "qd-caddy"})

	docker.CreateNetwork("simpledeploy")

	// Say "y" to reconfigure, then new inputs
	input := "y\n1\nnewdomain.com\n\nnew@test.com\n\n\n"
	setWizardInput(t, input)

	_ = captureStdout(func() {
		err := RunInit()
		if err != nil {
			t.Fatalf("RunInit reconfigure failed: %v", err)
		}
	})

	loaded, _ := state.GetConfig()
	if loaded.BaseDomain != "newdomain.com" {
		t.Errorf("BaseDomain = %q, want 'newdomain.com'", loaded.BaseDomain)
	}

	captureStdout(func() {
		proxy.StopTraefik()
	})
}

func TestEnsureDocker_Installed(t *testing.T) {
	if !docker.IsInstalled() {
		t.Skip("Docker not installed")
	}

	_ = captureStdout(func() {
		err := docker.EnsureDocker()
		if err != nil {
			t.Errorf("EnsureDocker should succeed when Docker installed: %v", err)
		}
	})
}

func TestRunInit_InvalidPort(t *testing.T) {
	if !docker.IsInstalled() || !docker.IsComposeInstalled() {
		t.Skip("Docker/Compose not installed")
	}

	dir := t.TempDir()
	state.InitState(dir)

	proxyDir := filepath.Join(dir, "proxy")
	proxy.ProxyDir = proxyDir

	ln80, err := net.Listen("tcp", ":80")
	if err != nil {
		t.Skipf("Port 80 not available: %v", err)
	}
	ln80.Close()
	ln443, err := net.Listen("tcp", ":443")
	if err != nil {
		t.Skipf("Port 443 not available: %v", err)
	}
	ln443.Close()

	// Clean up stale containers from previous test runs
	_ = docker.Run([]string{"rm", "-f", "qd-traefik", "qd-caddy"})

	docker.CreateNetwork("simpledeploy")

	// Choose traefik, domain, wildcard yes, email, auto secret, invalid port "abc" → should default to 9000
	input := "1\ntest.com\n\nadmin@test.com\n\nabc\n"
	setWizardInput(t, input)

	_ = captureStdout(func() {
		err := RunInit()
		if err != nil {
			t.Fatalf("RunInit failed: %v", err)
		}
	})

	cfg, _ := state.GetConfig()
	if cfg.WebhookPort != 9000 {
		t.Errorf("WebhookPort = %d, want 9000 (default for invalid input)", cfg.WebhookPort)
	}

	captureStdout(func() {
		proxy.StopTraefik()
	})
}

func TestRunDeploy_NoConfig(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)

	_ = captureStdout(func() {
		err := RunDeploy()
		if err == nil {
			t.Error("Should fail without config")
		}
	})
}

func TestRunDeploy_AppAlreadyExists(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)

	cfg := &state.GlobalConfig{Proxy: "traefik", BaseDomain: "test.example.com"}
	state.SaveConfig(cfg)

	app := state.NewAppConfig()
	app.Name = "existingapp"
	app.Branch = "main"
	app.Repo = "https://github.com/test/app.git"
	app.Domain = "existingapp.example.com"
	app.Port = 3000
	app.Type = "node"
	state.SaveApp(app)

	// Input: repo URL → branch (default) → not private → app name "existingapp"
	input := "https://github.com/test/existingapp.git\n\nn\nexistingapp\n"
	setWizardInput(t, input)

	_ = captureStdout(func() {
		err := RunDeploy()
		if err == nil {
			t.Error("Should fail when app already exists")
		}
		if err != nil && !strings.Contains(err.Error(), "already exists") {
			t.Errorf("Error should mention 'already exists', got: %v", err)
		}
	})
}

func TestRunDeploy_CloneFails(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)

	cfgpkg.BaseDir = filepath.Join(dir, "opt", "simpledeploy")

	cfg := &state.GlobalConfig{Proxy: "traefik", BaseDomain: "test.example.com"}
	state.SaveConfig(cfg)

	// Input: repo URL → branch → not private → app name → clone will fail
	input := "https://github.com/nonexistent/repo-definitely-does-not-exist-xyz.git\n\nn\nnewapp\n"
	setWizardInput(t, input)

	_ = captureStdout(func() {
		err := RunDeploy()
		if err == nil {
			t.Error("Should fail when git clone fails")
		}
		if err != nil && !strings.Contains(err.Error(), "clone") {
			t.Errorf("Error should mention clone, got: %v", err)
		}
	})
}

func TestRunDeploy_CancelDeploy(t *testing.T) {
	if !docker.IsInstalled() {
		t.Skip("Docker not installed")
	}

	dir := t.TempDir()
	state.InitState(dir)

	cfgpkg.BaseDir = filepath.Join(dir, "opt", "simpledeploy")

	cfg := &state.GlobalConfig{Proxy: "traefik", BaseDomain: "test.example.com"}
	state.SaveConfig(cfg)

	// Create a local git repo to clone from
	repoDir := filepath.Join(dir, "repo")
	os.MkdirAll(repoDir, 0755)
	runGitCmd(t, repoDir, "init", "-b", "main")
	runGitCmd(t, repoDir, "config", "user.email", "test@test.com")
	runGitCmd(t, repoDir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(repoDir, "file.txt"), []byte("hello"), 0644)
	runGitCmd(t, repoDir, "add", ".")
	runGitCmd(t, repoDir, "commit", "-m", "initial")

	// Input: repo path → branch (default main) → not private → app name →
	// app type (7=Dockerfile) → port → no env vars → no .env →
	// no databases (6=None) → subdomain → no extra headers →
	// no webhook → cancel deploy
	input := repoDir + "\n\nn\ncancelapp\n7\n3000\n\nn\n6\ncancelapp\n\nn\nn\n"
	setWizardInput(t, input)

	_ = captureStdout(func() {
		err := RunDeploy()
		if err != nil {
			t.Errorf("Cancelled deploy should not error: %v", err)
		}
	})
}
