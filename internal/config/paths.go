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

var osUserHomeDir = os.UserHomeDir

// StateDirEnv overrides where state.json lives, independently of BaseDir.
//
// This exists for containerised operation. Inside the service container $HOME
// is /root, so HomeDataDir() resolved to /root/.simpledeploy — a path the
// generated service compose never mounted. The container therefore started
// with an empty state file, found no global config, and exited immediately;
// `service install` could not work at all. The compose file now bind-mounts
// the host's state directory and points this variable at it.
const StateDirEnv = "SIMPLEDEPLOY_STATE_DIR"

// HomeDataDir returns the directory holding state.json — $SIMPLEDEPLOY_STATE_DIR
// when set, otherwise ~/.simpledeploy.
func HomeDataDir() string {
	if dir := os.Getenv(StateDirEnv); dir != "" {
		return dir
	}
	home, err := osUserHomeDir()
	if err != nil {
		home = "/root"
	}
	return filepath.Join(home, DefaultDataDir)
}

func StatePath() string { return filepath.Join(HomeDataDir(), "state.json") }
