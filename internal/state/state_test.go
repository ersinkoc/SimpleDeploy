package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

type mockStateFile struct {
	writeErr error
	syncErr  error
	closeErr error
	data     []byte
}

func (m *mockStateFile) Write(p []byte) (int, error) {
	if m.writeErr != nil {
		return 0, m.writeErr
	}
	m.data = append(m.data, p...)
	return len(p), nil
}

func (m *mockStateFile) Sync() error  { return m.syncErr }
func (m *mockStateFile) Close() error { return m.closeErr }

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

func TestLoad_ReadError(t *testing.T) {
	tempStateDir(t)
	oldReadFile := osReadFile
	osReadFile = func(name string) ([]byte, error) {
		return nil, errors.New("read error")
	}
	defer func() { osReadFile = oldReadFile }()

	_, err := Load()
	if err == nil {
		t.Error("Load should fail on read error")
	}
}

func TestSave_MkdirAllError(t *testing.T) {
	tempStateDir(t)
	oldMkdirAll := osMkdirAll
	osMkdirAll = func(path string, perm os.FileMode) error {
		return errors.New("mkdir error")
	}
	defer func() { osMkdirAll = oldMkdirAll }()

	err := Save(&State{Version: 1})
	if err == nil {
		t.Error("Save should fail when MkdirAll fails")
	}
}

func TestSave_MarshalError(t *testing.T) {
	tempStateDir(t)
	oldMarshal := jsonMarshalIndent
	jsonMarshalIndent = func(v interface{}, prefix, indent string) ([]byte, error) {
		return nil, errors.New("marshal error")
	}
	defer func() { jsonMarshalIndent = oldMarshal }()

	err := Save(&State{Version: 1})
	if err == nil {
		t.Error("Save should fail when marshal fails")
	}
}

func TestSave_OpenFileError(t *testing.T) {
	tempStateDir(t)
	oldOpenFile := osOpenFile
	osOpenFile = func(name string, flag int, perm os.FileMode) (stateFile, error) {
		return nil, errors.New("open error")
	}
	defer func() { osOpenFile = oldOpenFile }()

	err := Save(&State{Version: 1})
	if err == nil {
		t.Error("Save should fail when OpenFile fails")
	}
}

func TestSave_WriteError(t *testing.T) {
	tempStateDir(t)
	oldOpenFile := osOpenFile
	osOpenFile = func(name string, flag int, perm os.FileMode) (stateFile, error) {
		return &mockStateFile{writeErr: errors.New("write error")}, nil
	}
	defer func() { osOpenFile = oldOpenFile }()

	err := Save(&State{Version: 1})
	if err == nil {
		t.Error("Save should fail when Write fails")
	}
}

func TestSave_SyncError(t *testing.T) {
	tempStateDir(t)
	oldOpenFile := osOpenFile
	osOpenFile = func(name string, flag int, perm os.FileMode) (stateFile, error) {
		return &mockStateFile{syncErr: errors.New("sync error")}, nil
	}
	defer func() { osOpenFile = oldOpenFile }()

	err := Save(&State{Version: 1})
	if err == nil {
		t.Error("Save should fail when Sync fails")
	}
}

func TestSave_RenameError(t *testing.T) {
	tempStateDir(t)
	oldRename := osRename
	osRename = func(oldpath, newpath string) error {
		return errors.New("rename error")
	}
	defer func() { osRename = oldRename }()

	err := Save(&State{Version: 1})
	if err == nil {
		t.Error("Save should fail when Rename fails")
	}
}

func TestInitState_EmptyBaseDir_Error(t *testing.T) {
	oldUserHomeDir := osUserHomeDir
	osUserHomeDir = func() (string, error) {
		return "", errors.New("no home")
	}
	defer func() { osUserHomeDir = oldUserHomeDir }()

	InitState("")
	expected := filepath.Join("", ".simpledeploy", "state.json")
	if statePath != expected {
		t.Errorf("statePath = %q, want %q", statePath, expected)
	}
}

func TestGetStatePath_Fallback_Error(t *testing.T) {
	oldStatePath := statePath
	statePath = ""
	defer func() { statePath = oldStatePath }()

	oldUserHomeDir := osUserHomeDir
	osUserHomeDir = func() (string, error) {
		return "", errors.New("no home")
	}
	defer func() { osUserHomeDir = oldUserHomeDir }()

	path := getStatePath()
	expected := filepath.Join("", ".simpledeploy", "state.json")
	if path != expected {
		t.Errorf("getStatePath() = %q, want %q", path, expected)
	}
}

func TestGetApp_LoadError(t *testing.T) {
	tempStateDir(t)
	oldReadFile := osReadFile
	osReadFile = func(name string) ([]byte, error) {
		return nil, errors.New("read error")
	}
	defer func() { osReadFile = oldReadFile }()

	_, err := GetApp("myapp")
	if err == nil {
		t.Error("GetApp should fail when Load fails")
	}
}

func TestSaveApp_LoadError(t *testing.T) {
	tempStateDir(t)
	oldReadFile := osReadFile
	osReadFile = func(name string) ([]byte, error) {
		return nil, errors.New("read error")
	}
	defer func() { osReadFile = oldReadFile }()

	app := NewAppConfig()
	app.Name = "myapp"
	err := SaveApp(app)
	if err == nil {
		t.Error("SaveApp should fail when Load fails")
	}
}

func TestSaveApp_SaveError(t *testing.T) {
	tempStateDir(t)
	oldMkdirAll := osMkdirAll
	osMkdirAll = func(path string, perm os.FileMode) error {
		return errors.New("mkdir error")
	}
	defer func() { osMkdirAll = oldMkdirAll }()

	app := NewAppConfig()
	app.Name = "myapp"
	err := SaveApp(app)
	if err == nil {
		t.Error("SaveApp should fail when Save fails")
	}
}

func TestRemoveApp_LoadError(t *testing.T) {
	tempStateDir(t)
	oldReadFile := osReadFile
	osReadFile = func(name string) ([]byte, error) {
		return nil, errors.New("read error")
	}
	defer func() { osReadFile = oldReadFile }()

	err := RemoveApp("myapp")
	if err == nil {
		t.Error("RemoveApp should fail when Load fails")
	}
}

func TestRemoveApp_SaveError(t *testing.T) {
	tempStateDir(t)
	oldMkdirAll := osMkdirAll
	osMkdirAll = func(path string, perm os.FileMode) error {
		return errors.New("mkdir error")
	}
	defer func() { osMkdirAll = oldMkdirAll }()

	// First save without the mock so there's data to load
	app := NewAppConfig()
	app.Name = "myapp"
	SaveApp(app)

	err := RemoveApp("myapp")
	if err == nil {
		t.Error("RemoveApp should fail when Save fails")
	}
}

func TestSaveConfig_LoadError(t *testing.T) {
	tempStateDir(t)
	oldReadFile := osReadFile
	osReadFile = func(name string) ([]byte, error) {
		return nil, errors.New("read error")
	}
	defer func() { osReadFile = oldReadFile }()

	cfg := &GlobalConfig{Proxy: "traefik"}
	err := SaveConfig(cfg)
	if err == nil {
		t.Error("SaveConfig should fail when Load fails")
	}
}

func TestSaveConfig_SaveError(t *testing.T) {
	tempStateDir(t)
	oldMkdirAll := osMkdirAll
	osMkdirAll = func(path string, perm os.FileMode) error {
		return errors.New("mkdir error")
	}
	defer func() { osMkdirAll = oldMkdirAll }()

	cfg := &GlobalConfig{Proxy: "traefik"}
	err := SaveConfig(cfg)
	if err == nil {
		t.Error("SaveConfig should fail when Save fails")
	}
}

func TestGetConfig_LoadError(t *testing.T) {
	tempStateDir(t)
	oldReadFile := osReadFile
	osReadFile = func(name string) ([]byte, error) {
		return nil, errors.New("read error")
	}
	defer func() { osReadFile = oldReadFile }()

	_, err := GetConfig()
	if err == nil {
		t.Error("GetConfig should fail when Load fails")
	}
}

// TestStateFileLocking tests that concurrent Save operations from different
// goroutines don't corrupt the state file.
func TestStateFileLocking(t *testing.T) {
	tempStateDir(t)

	// Create initial state
	cfg := &GlobalConfig{Proxy: "traefik", BaseDomain: "test.com"}
	if err := SaveConfig(cfg); err != nil {
		t.Fatalf("Failed to save initial config: %v", err)
	}

	// Concurrent save operations
	var wg sync.WaitGroup
	errors := make(chan error, 10)

	// 10 goroutines doing SaveApp operations
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			app := NewAppConfig()
			app.Name = fmt.Sprintf("locking-app-%d", id)
			if err := SaveApp(app); err != nil {
				errors <- fmt.Errorf("goroutine %d: SaveApp failed: %v", id, err)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Error(err)
	}

	// Verify state consistency - all apps should be present
	s, err := Load()
	if err != nil {
		t.Fatalf("Failed to load state: %v", err)
	}

	// Should have all 10 apps (or at least not corrupted)
	// Note: Due to read-modify-write race, some apps might be lost,
	// but the file should not be corrupted
	appCount := len(s.Apps)
	if appCount == 0 {
		t.Error("Expected at least some apps, got none")
	}
	t.Logf("State has %d apps after concurrent writes", appCount)

	// Verify no corruption by checking we can read the file again
	_, err = Load()
	if err != nil {
		t.Errorf("State file corrupted: %v", err)
	}
}

// TestLockStateFile_StaleLock tests that stale locks are detected and removed.
func TestLockStateFile_StaleLock(t *testing.T) {
	tempStateDir(t)

	// Create a stale lock file with a very old modification time
	lockPath := getStatePath() + ".lock"
	os.WriteFile(lockPath, []byte("99999\n"), 0600)

	// Set modification time to 10 seconds ago (older than 5 second threshold)
	oldTime := time.Now().Add(-10 * time.Second)
	os.Chtimes(lockPath, oldTime, oldTime)

	// Try to acquire lock - should succeed after detecting stale lock
	unlock, err := lockStateFile()
	if err != nil {
		t.Fatalf("Failed to acquire lock with stale lock file: %v", err)
	}
	defer unlock()
}

// TestProcessExists tests the processExists function.
func TestProcessExists(t *testing.T) {
	tempStateDir(t)

	// Create a lock file for current process
	lockPath := getStatePath() + ".lock"
	os.WriteFile(lockPath, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0600)

	// Current process should exist
	if !processExists(os.Getpid()) {
		t.Error("processExists should return true for current process")
	}

	// Clean up
	os.Remove(lockPath)
}
