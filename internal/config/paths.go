package config

import (
	"os"
	"path/filepath"
)

const (
	DefaultBaseDir = "/opt/simpledeploy"
	DefaultDataDir = ".simpledeploy" // relative to $HOME
)

// BaseDir is the root directory for all SimpleDeploy data on the server.
var BaseDir = DefaultBaseDir

// Init sets the base directory from env or default.
func Init() {
	if dir := os.Getenv("SIMPLEDEPLOY_DIR"); dir != "" {
		BaseDir = dir
	}
}

func ProxyDir() string          { return filepath.Join(BaseDir, "proxy") }
func AppsDir() string           { return filepath.Join(BaseDir, "apps") }
func AppDir(name string) string { return filepath.Join(BaseDir, "apps", name) }
func ServiceDir() string        { return filepath.Join(BaseDir, "service") }
func ConfigPath() string        { return filepath.Join(BaseDir, "config.json") }

// HomeDataDir returns ~/.simpledeploy for local state.
func HomeDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/root"
	}
	return filepath.Join(home, DefaultDataDir)
}

func StatePath() string { return filepath.Join(HomeDataDir(), "state.json") }
