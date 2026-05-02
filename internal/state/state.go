package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
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
	statePath string
	mu        sync.Mutex
)

// lockStateFile acquires an advisory lock on the state file.
// This prevents concurrent modifications from different processes.
func lockStateFile() (unlock func(), err error) {
	lockPath := getStatePath() + ".lock"

	for retries := 0; retries < 100; retries++ {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			f.WriteString(fmt.Sprintf("%d\n", os.Getpid()))
			f.Close()
			return func() {
				os.Remove(lockPath)
			}, nil
		}

		// Lock file exists, check if stale based on file age
		info, statErr := os.Stat(lockPath)
		if statErr == nil && time.Since(info.ModTime()) > 30*time.Second {
			os.Remove(lockPath)
			continue
		}

		time.Sleep(10 * time.Millisecond)
	}

	return nil, fmt.Errorf("could not acquire state lock after 100 retries")
}

func InitState(baseDir string) {
	if baseDir == "" {
		home, _ := osUserHomeDir()
		baseDir = filepath.Join(home, ".simpledeploy")
	}
	statePath = filepath.Join(baseDir, "state.json")
}

func getStatePath() string {
	if statePath != "" {
		return statePath
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
