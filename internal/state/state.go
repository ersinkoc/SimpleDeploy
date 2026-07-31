package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ersinkoc/SimpleDeploy/internal/lockfile"
)

type stateFile interface {
	Write([]byte) (int, error)
	Sync() error
	Close() error
}

var (
	osUserHomeDir     = os.UserHomeDir
	osMkdirAll        = os.MkdirAll
	osOpenFile        = func(name string, flag int, perm os.FileMode) (stateFile, error) { return os.OpenFile(name, flag, perm) }
	osRename          = os.Rename
	osRemove          = os.Remove
	osReadFile        = os.ReadFile
	jsonMarshalIndent = json.MarshalIndent
)

type AppConfig struct {
	Name           string            `json:"name"`
	Repo           string            `json:"repo"`
	Branch         string            `json:"branch"`
	GitToken       string            `json:"git_token,omitempty"`
	Domain         string            `json:"domain"`
	Port           int               `json:"port"`
	Type           string            `json:"type"`
	CurrentImage   string            `json:"current_image"`
	Databases      []string          `json:"databases"`
	DBCredentials  map[string]string `json:"db_credentials,omitempty"`
	WebhookEnabled bool              `json:"webhook_enabled"`
	Headers        map[string]string `json:"headers"`
	CreatedAt      string            `json:"created_at"`
	LastDeploy     string            `json:"last_deploy"`
	DeployCount    int               `json:"deploy_count"`
	Status         string            `json:"status"`
}

type GlobalConfig struct {
	BaseDomain    string `json:"base_domain"`
	Proxy         string `json:"proxy"`
	AcmeEmail     string `json:"acme_email"`
	WebhookPort   int    `json:"webhook_port"`
	WebhookSecret string `json:"webhook_secret"`
}

type State struct {
	Version int                   `json:"version"`
	Apps    map[string]*AppConfig `json:"apps"`
	Config  *GlobalConfig         `json:"config,omitempty"`
}

var (
	mu sync.Mutex
)

// lockStateFile acquires an advisory lock on the state file using the shared
// lockfile primitive. This prevents concurrent modifications from different
// processes.
//
// The lock uses Spin retry (brief waits, up to 100 attempts) because contending
// state writers are short-lived (milliseconds) and failing fast would corrupt
// every concurrent state mutation. Stale locks older than 30 seconds are
// recovered.
func lockStateFile() (unlock func(), err error) {
	lockPath := getStatePath() + ".lock"
	release, _, err := lockfile.Acquire(lockPath, lockfile.Options{
		StaleAfter:   30 * time.Second,
		Retry:        lockfile.Spin,
		SpinInterval: 10 * time.Millisecond,
		MaxRetries:   100,
	})
	return release, err
}

// InitState sets the directory holding state.json. It works by setting the
// SIMPLEDEPLOY_STATE_DIR environment variable, which config.HomeDataDir()
// reads — so state and applock (which also call HomeDataDir) always agree on
// where state lives.
//
// This replaced a parallel package-level statePath var that was disconnected
// from config.HomeDataDir(). The divergence caused a real trap: tests called
// InitState(t.TempDir()) to isolate state.json, but applock.Acquire called
// config.HomeDataDir() (which read the env var, not statePath) — so per-app
// lock files landed in the developer's real ~/.simpledeploy until TestMain
// grew a second mechanism to set the env var. Now there is one knob.
func InitState(baseDir string) {
	if baseDir == "" {
		home, _ := osUserHomeDir()
		baseDir = filepath.Join(home, ".simpledeploy")
	}
	os.Setenv(stateDirEnv, baseDir)
}

// stateDirEnv mirrors config.StateDirEnv. Declared locally to avoid importing
// config (which would create a cycle: config has no deps on state, but keeping
// the const local makes the dependency direction explicit).
const stateDirEnv = "SIMPLEDEPLOY_STATE_DIR"

func getStatePath() string {
	if dir := os.Getenv(stateDirEnv); dir != "" {
		return filepath.Join(dir, "state.json")
	}
	home, _ := osUserHomeDir()
	return filepath.Join(home, ".simpledeploy", "state.json")
}

// loadStateLocked reads state from disk. Caller MUST hold mu.
// This is the underlying read primitive used by Load and the
// load-modify-save mutators (SaveApp/RemoveApp/SaveConfig) so a single
// critical section can span the full transaction.
func loadStateLocked() (*State, error) {
	path := getStatePath()
	data, err := osReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{
				Version: 1,
				Apps:    make(map[string]*AppConfig),
			}, nil
		}
		return nil, fmt.Errorf("failed to read state: %w", err)
	}

	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("failed to parse state: %w", err)
	}
	if s.Apps == nil {
		s.Apps = make(map[string]*AppConfig)
	}
	return &s, nil
}

// saveStateLocked writes state to disk atomically. Caller MUST hold mu
// AND the cross-process file lock.
func saveStateLocked(s *State) error {
	path := getStatePath()
	dir := filepath.Dir(path)
	if err := osMkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	data, err := jsonMarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	tmpPath := path + ".tmp"
	tmpFile, err := osOpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to create temp state file: %w", err)
	}
	defer func() {
		if tmpFile != nil {
			tmpFile.Close()
		}
	}()

	if _, err := tmpFile.Write(data); err != nil {
		osRemove(tmpPath)
		return fmt.Errorf("failed to write state: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		osRemove(tmpPath)
		return fmt.Errorf("failed to sync state file: %w", err)
	}
	tmpFile.Close()
	tmpFile = nil

	if err := osRename(tmpPath, path); err != nil {
		osRemove(tmpPath)
		return fmt.Errorf("failed to rename state file: %w", err)
	}
	return nil
}

func Load() (*State, error) {
	mu.Lock()
	defer mu.Unlock()
	return loadStateLocked()
}

func Save(s *State) error {
	mu.Lock()
	defer mu.Unlock()

	unlock, err := lockStateFile()
	if err != nil {
		return fmt.Errorf("failed to lock state: %w", err)
	}
	defer unlock()

	return saveStateLocked(s)
}

func GetApp(name string) (*AppConfig, error) {
	s, err := Load()
	if err != nil {
		return nil, err
	}
	app, ok := s.Apps[name]
	if !ok {
		return nil, fmt.Errorf("application '%s' not found", name)
	}
	cp := *app
	if cp.Headers != nil {
		cp.Headers = make(map[string]string, len(app.Headers))
		for k, v := range app.Headers {
			cp.Headers[k] = v
		}
	}
	if cp.DBCredentials != nil {
		cp.DBCredentials = make(map[string]string, len(app.DBCredentials))
		for k, v := range app.DBCredentials {
			cp.DBCredentials[k] = v
		}
	}
	if cp.Databases != nil {
		cp.Databases = make([]string, len(app.Databases))
		copy(cp.Databases, app.Databases)
	}
	return &cp, nil
}

func SaveApp(app *AppConfig) error {
	mu.Lock()
	defer mu.Unlock()

	unlock, err := lockStateFile()
	if err != nil {
		return fmt.Errorf("failed to lock state: %w", err)
	}
	defer unlock()

	s, err := loadStateLocked()
	if err != nil {
		return err
	}
	s.Apps[app.Name] = app
	return saveStateLocked(s)
}

func RemoveApp(name string) error {
	mu.Lock()
	defer mu.Unlock()

	unlock, err := lockStateFile()
	if err != nil {
		return fmt.Errorf("failed to lock state: %w", err)
	}
	defer unlock()

	s, err := loadStateLocked()
	if err != nil {
		return err
	}
	delete(s.Apps, name)
	return saveStateLocked(s)
}

// UpdateApp atomically applies mutate to the CURRENT stored record for name,
// under both the in-process mutex and the cross-process file lock.
//
// This exists because the load-mutate-SaveApp pattern spans minutes for a
// deploy (git pull + docker build happen between the load and the save), and
// SaveApp wholesale-replaces the record with that stale copy. Two concrete
// failures followed: a `simpledeploy remove` issued during a webhook redeploy
// was undone when the redeploy's final SaveApp re-inserted the deleted app
// ("resurrection"), and a `stop` issued in the same window had its
// Status:"stopped" silently overwritten. UpdateApp re-reads the record inside
// the critical section, refuses to touch an app that no longer exists, and
// lets the caller set only the fields it actually owns.
func UpdateApp(name string, mutate func(*AppConfig)) error {
	mu.Lock()
	defer mu.Unlock()

	unlock, err := lockStateFile()
	if err != nil {
		return fmt.Errorf("failed to lock state: %w", err)
	}
	defer unlock()

	s, err := loadStateLocked()
	if err != nil {
		return err
	}
	app, ok := s.Apps[name]
	if !ok {
		return fmt.Errorf("application '%s' not found", name)
	}
	mutate(app)
	return saveStateLocked(s)
}

func SaveConfig(cfg *GlobalConfig) error {
	mu.Lock()
	defer mu.Unlock()

	unlock, err := lockStateFile()
	if err != nil {
		return fmt.Errorf("failed to lock state: %w", err)
	}
	defer unlock()

	s, err := loadStateLocked()
	if err != nil {
		return err
	}
	s.Config = cfg
	return saveStateLocked(s)
}

func GetConfig() (*GlobalConfig, error) {
	s, err := Load()
	if err != nil {
		return nil, err
	}
	if s.Config == nil {
		return nil, fmt.Errorf("SimpleDeploy not initialized. Run 'simpledeploy init' first")
	}
	return s.Config, nil
}

func IsInitialized() bool {
	_, err := os.Stat(getStatePath())
	return err == nil
}

func NewAppConfig() *AppConfig {
	return &AppConfig{
		Headers:       make(map[string]string),
		Databases:     []string{},
		DBCredentials: make(map[string]string),
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		Status:        "pending",
	}
}
