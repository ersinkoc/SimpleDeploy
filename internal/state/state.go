package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
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

func InitState(baseDir string) {
	if baseDir == "" {
		home, _ := os.UserHomeDir()
		baseDir = filepath.Join(home, ".simpledeploy")
	}
	statePath = filepath.Join(baseDir, "state.json")
}

func getStatePath() string {
	if statePath != "" {
		return statePath
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".simpledeploy", "state.json")
}

func Load() (*State, error) {
	mu.Lock()
	defer mu.Unlock()

	path := getStatePath()
	data, err := os.ReadFile(path)
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

func Save(s *State) error {
	mu.Lock()
	defer mu.Unlock()

	path := getStatePath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	// Write with restricted permissions (contains encrypted secrets)
	return os.WriteFile(path, data, 0600)
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
	return app, nil
}

func SaveApp(app *AppConfig) error {
	s, err := Load()
	if err != nil {
		return err
	}
	s.Apps[app.Name] = app
	return Save(s)
}

func RemoveApp(name string) error {
	s, err := Load()
	if err != nil {
		return err
	}
	delete(s.Apps, name)
	return Save(s)
}

func SaveConfig(cfg *GlobalConfig) error {
	s, err := Load()
	if err != nil {
		return err
	}
	s.Config = cfg
	return Save(s)
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
