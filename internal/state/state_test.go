package state

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func tempStateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	InitState(dir)
	return dir
}

func TestLoadEmpty(t *testing.T) {
	tempStateDir(t)
	s, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if s.Version != 1 {
		t.Errorf("Version = %d, want 1", s.Version)
	}
	if len(s.Apps) != 0 {
		t.Errorf("Apps should be empty, got %d", len(s.Apps))
	}
}

func TestSaveAndLoad(t *testing.T) {
	tempStateDir(t)
	s := &State{
		Version: 1,
		Apps:    make(map[string]*AppConfig),
		Config: &GlobalConfig{
			BaseDomain:    "test.example.com",
			Proxy:         "traefik",
			AcmeEmail:     "test@test.com",
			WebhookPort:   9000,
			WebhookSecret: "secret123",
		},
	}
	s.Apps["myapp"] = &AppConfig{
		Name:   "myapp",
		Repo:   "https://github.com/test/myapp.git",
		Branch: "main",
		Domain: "myapp.test.example.com",
		Port:   3000,
		Type:   "node",
		Status: "running",
	}

	if err := Save(s); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.Config.BaseDomain != "test.example.com" {
		t.Errorf("BaseDomain = %q, want 'test.example.com'", loaded.Config.BaseDomain)
	}
	if loaded.Apps["myapp"].Port != 3000 {
		t.Errorf("App Port = %d, want 3000", loaded.Apps["myapp"].Port)
	}
}

func TestSaveApp(t *testing.T) {
	tempStateDir(t)

	app := NewAppConfig()
	app.Name = "testapp"
	app.Repo = "https://github.com/test/app.git"
	app.Branch = "main"
	app.Port = 8080
	app.Type = "go"

	if err := SaveApp(app); err != nil {
		t.Fatalf("SaveApp failed: %v", err)
	}

	loaded, err := GetApp("testapp")
	if err != nil {
		t.Fatalf("GetApp failed: %v", err)
	}
	if loaded.Name != "testapp" {
		t.Errorf("Name = %q, want 'testapp'", loaded.Name)
	}
	if loaded.Port != 8080 {
		t.Errorf("Port = %d, want 8080", loaded.Port)
	}
}

func TestGetAppNotFound(t *testing.T) {
	tempStateDir(t)
	_, err := GetApp("nonexistent")
	if err == nil {
		t.Error("Should return error for nonexistent app")
	}
}

func TestRemoveApp(t *testing.T) {
	tempStateDir(t)
	app := NewAppConfig()
	app.Name = "to-remove"
	SaveApp(app)

	if err := RemoveApp("to-remove"); err != nil {
		t.Fatalf("RemoveApp failed: %v", err)
	}

	_, err := GetApp("to-remove")
	if err == nil {
		t.Error("App should be removed")
	}
}

func TestSaveConfig(t *testing.T) {
	tempStateDir(t)
	cfg := &GlobalConfig{
		BaseDomain:    "example.com",
		Proxy:         "caddy",
		AcmeEmail:     "admin@example.com",
		WebhookPort:   8080,
		WebhookSecret: "secret",
	}
	if err := SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	loaded, err := GetConfig()
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}
	if loaded.Proxy != "caddy" {
		t.Errorf("Proxy = %q, want 'caddy'", loaded.Proxy)
	}
}

func TestGetConfigNotInitialized(t *testing.T) {
	tempStateDir(t)
	_, err := GetConfig()
	if err == nil {
		t.Error("Should return error when not initialized")
	}
}

func TestIsInitialized(t *testing.T) {
	dir := tempStateDir(t)
	if IsInitialized() {
		// statePath points to temp dir, file doesn't exist yet
		path := filepath.Join(dir, "state.json")
		if _, err := os.Stat(path); err == nil {
			t.Log("State file exists from previous test")
		}
	}

	// Create the state file
	s := &State{Version: 1, Apps: make(map[string]*AppConfig)}
	Save(s)

	if !IsInitialized() {
		t.Error("IsInitialized should return true after save")
	}
}

func TestNewAppConfig(t *testing.T) {
	app := NewAppConfig()
	if app.Status != "pending" {
		t.Errorf("Status = %q, want 'pending'", app.Status)
	}
	if app.Headers == nil {
		t.Error("Headers should be initialized")
	}
	if app.Databases == nil {
		t.Error("Databases should be initialized")
	}
	if app.DBCredentials == nil {
		t.Error("DBCredentials should be initialized")
	}
	if app.CreatedAt == "" {
		t.Error("CreatedAt should be set")
	}
}

func TestInitState_EmptyBaseDir(t *testing.T) {
	InitState("")
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".simpledeploy", "state.json")
	if statePath != expected {
		t.Errorf("statePath = %q, want %q", statePath, expected)
	}
}

func TestInitState_CustomDir(t *testing.T) {
	customDir := t.TempDir()
	InitState(customDir)
	expected := filepath.Join(customDir, "state.json")
	if statePath != expected {
		t.Errorf("statePath = %q, want %q", statePath, expected)
	}
}

func TestLoad_CorruptedJSON(t *testing.T) {
	dir := tempStateDir(t)
	os.WriteFile(filepath.Join(dir, "state.json"), []byte("not valid json{{{"), 0600)
	_, err := Load()
	if err == nil {
		t.Error("Should fail on corrupted JSON")
	}
}

func TestSave_NilApps(t *testing.T) {
	tempStateDir(t)
	s := &State{Version: 1}
	if err := Save(s); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.Apps == nil {
		t.Error("Apps should be initialized to empty map, not nil")
	}
}

func TestSaveApp_Overwrite(t *testing.T) {
	tempStateDir(t)
	app := NewAppConfig()
	app.Name = "overwrite"
	app.Port = 3000
	SaveApp(app)

	app.Port = 8080
	SaveApp(app)

	loaded, err := GetApp("overwrite")
	if err != nil {
		t.Fatalf("GetApp failed: %v", err)
	}
	if loaded.Port != 8080 {
		t.Errorf("Port = %d, want 8080 after overwrite", loaded.Port)
	}
}

func TestSaveConfig_Overwrite(t *testing.T) {
	tempStateDir(t)
	cfg := &GlobalConfig{Proxy: "traefik", BaseDomain: "a.com", AcmeEmail: "a@a.com", WebhookPort: 9000, WebhookSecret: "s"}
	SaveConfig(cfg)

	cfg.Proxy = "caddy"
	cfg.BaseDomain = "b.com"
	SaveConfig(cfg)

	loaded, err := GetConfig()
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}
	if loaded.Proxy != "caddy" || loaded.BaseDomain != "b.com" {
		t.Errorf("Config not overwritten: proxy=%q domain=%q", loaded.Proxy, loaded.BaseDomain)
	}
}

func TestSaveAndLoad_MultipleApps(t *testing.T) {
	tempStateDir(t)
	for _, name := range []string{"app1", "app2", "app3"} {
		app := NewAppConfig()
		app.Name = name
		app.Port = 3000
		app.Domain = name + ".example.com"
		SaveApp(app)
	}

	s, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(s.Apps) != 3 {
		t.Errorf("Apps count = %d, want 3", len(s.Apps))
	}
}

func TestRemoveApp_NonExistent(t *testing.T) {
	tempStateDir(t)
	// Should not error when removing non-existent app
	err := RemoveApp("ghost")
	if err != nil {
		t.Errorf("RemoveApp for non-existent app should not error: %v", err)
	}
}

func TestIsInitialized_Not(t *testing.T) {
	dir := t.TempDir()
	InitState(dir)
	if IsInitialized() {
		t.Error("Should not be initialized without state file")
	}
}

func TestFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions not supported on Windows")
	}
	dir := tempStateDir(t)
	s := &State{Version: 1, Apps: make(map[string]*AppConfig)}
	Save(s)

	path := filepath.Join(dir, "state.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	perm := info.Mode().Perm()
	if perm&0077 != 0 {
		t.Errorf("State file has too open permissions: %o, should be 0600", perm)
	}
}

func TestGetStatePath_Fallback(t *testing.T) {
	// Reset statePath to test fallback
	statePath = ""
	path := getStatePath()
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".simpledeploy", "state.json")
	if path != expected {
		t.Errorf("getStatePath() = %q, want %q", path, expected)
	}
}

func TestSave_ReadOnlyDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions not supported on Windows")
	}
	dir := t.TempDir()
	readOnlyDir := filepath.Join(dir, "readonly")
	os.MkdirAll(readOnlyDir, 0555)
	os.Chmod(readOnlyDir, 0555)
	defer os.Chmod(readOnlyDir, 0755) // restore for cleanup

	InitState(filepath.Join(readOnlyDir, "nested"))
	s := &State{Version: 1, Apps: make(map[string]*AppConfig)}
	err := Save(s)
	if err == nil {
		t.Error("Save should fail with read-only directory")
	}
}
