package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultBaseDir(t *testing.T) {
	if BaseDir != DefaultBaseDir {
		t.Errorf("BaseDir = %q, want %q", BaseDir, DefaultBaseDir)
	}
}

func TestInitWithEnv(t *testing.T) {
	os.Setenv("SIMPLEDEPLOY_DIR", "/tmp/custom")
	defer os.Unsetenv("SIMPLEDEPLOY_DIR")

	Init()
	if BaseDir != "/tmp/custom" {
		t.Errorf("BaseDir = %q, want '/tmp/custom'", BaseDir)
	}

	// Reset
	BaseDir = DefaultBaseDir
}

func TestInitWithoutEnv(t *testing.T) {
	os.Unsetenv("SIMPLEDEPLOY_DIR")
	Init()
	if BaseDir != DefaultBaseDir {
		t.Errorf("BaseDir = %q, want %q", BaseDir, DefaultBaseDir)
	}
}

func TestProxyDir(t *testing.T) {
	expected := filepath.Join(BaseDir, "proxy")
	if ProxyDir() != expected {
		t.Errorf("ProxyDir() = %q, want %q", ProxyDir(), expected)
	}
}

func TestAppsDir(t *testing.T) {
	expected := filepath.Join(BaseDir, "apps")
	if AppsDir() != expected {
		t.Errorf("AppsDir() = %q, want %q", AppsDir(), expected)
	}
}

func TestAppDir(t *testing.T) {
	expected := filepath.Join(BaseDir, "apps", "myapp")
	if AppDir("myapp") != expected {
		t.Errorf("AppDir('myapp') = %q, want %q", AppDir("myapp"), expected)
	}
}

func TestServiceDir(t *testing.T) {
	expected := filepath.Join(BaseDir, "service")
	if ServiceDir() != expected {
		t.Errorf("ServiceDir() = %q, want %q", ServiceDir(), expected)
	}
}

func TestConfigPath(t *testing.T) {
	expected := filepath.Join(BaseDir, "config.json")
	if ConfigPath() != expected {
		t.Errorf("ConfigPath() = %q, want %q", ConfigPath(), expected)
	}
}

func TestHomeDataDir(t *testing.T) {
	dir := HomeDataDir()
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".simpledeploy")
	if dir != expected {
		t.Errorf("HomeDataDir() = %q, want %q", dir, expected)
	}
}

func TestHomeDataDir_ErrorFallback(t *testing.T) {
	old := osUserHomeDir
	osUserHomeDir = func() (string, error) {
		return "", os.ErrNotExist
	}
	defer func() { osUserHomeDir = old }()

	dir := HomeDataDir()
	expected := filepath.Join("/root", ".simpledeploy")
	if dir != expected {
		t.Errorf("HomeDataDir() = %q, want %q", dir, expected)
	}
}

func TestStatePath(t *testing.T) {
	sp := StatePath()
	expected := filepath.Join(HomeDataDir(), "state.json")
	if sp != expected {
		t.Errorf("StatePath() = %q, want %q", sp, expected)
	}
}
