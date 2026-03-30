package cli

import (
	"strings"
	"testing"

	"github.com/ersinkoc/SimpleDeploy/internal/state"
)

func TestAppNameFromArgs(t *testing.T) {
	name, err := appNameFromArgs([]string{"myapp"})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if name != "myapp" {
		t.Errorf("Got %q, want 'myapp'", name)
	}
}

func TestAppNameFromArgs_Empty(t *testing.T) {
	_, err := appNameFromArgs([]string{})
	if err == nil {
		t.Error("Should return error for empty args")
	}
}

func TestAppNameFromArgs_InvalidNames(t *testing.T) {
	invalid := []string{"../etc", "MYAPP", "my app", "my_app", "a", "app.", "-app", "app-"}
	for _, name := range invalid {
		_, err := appNameFromArgs([]string{name})
		if err == nil {
			t.Errorf("appNameFromArgs(%q) should reject invalid name", name)
		}
	}
}

func TestAppNameFromArgs_ValidNames(t *testing.T) {
	valid := []string{"myapp", "my-app", "app123", "my-app-123", "ab"}
	for _, name := range valid {
		_, err := appNameFromArgs([]string{name})
		if err != nil {
			t.Errorf("appNameFromArgs(%q) should accept valid name: %v", name, err)
		}
	}
}

func TestSanitizeDefaultName(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"MyApp", "myapp"},
		{"my-app", "my-app"},
		{"my_app", "my-app"},
		{"My App", "my-app"},
		{"my.app", "my-app"},
		{"../etc", "etc"},
		{"UPPER_CASE", "upper-case"},
		{"a", "a"},
		{"", "app"},
	}
	for _, tt := range tests {
		got := sanitizeDefaultName(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeDefaultName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSanitizeDefaultName_TooLong(t *testing.T) {
	long := strings.Repeat("a", 100)
	got := sanitizeDefaultName(long)
	if len(got) > 63 {
		t.Errorf("Result too long: %d chars", len(got))
	}
}

func TestSanitizeDefaultName_TrimHyphens(t *testing.T) {
	got := sanitizeDefaultName("-hello-")
	if strings.HasPrefix(got, "-") || strings.HasSuffix(got, "-") {
		t.Errorf("Should trim leading/trailing hyphens, got %q", got)
	}
}

func TestHomeDir(t *testing.T) {
	home := homeDir()
	if home == "" {
		t.Error("homeDir should not be empty")
	}
}

func TestRoute_Version(t *testing.T) {
	if err := Route([]string{"version"}); err != nil {
		t.Errorf("version command failed: %v", err)
	}
}

func TestRoute_Help(t *testing.T) {
	if err := Route([]string{"help"}); err != nil {
		t.Errorf("help command failed: %v", err)
	}
}

func TestRoute_Flags(t *testing.T) {
	tests := []string{"-v", "--version", "-h", "--help"}
	for _, flag := range tests {
		if err := Route([]string{flag}); err != nil {
			t.Errorf("Route(%q) failed: %v", flag, err)
		}
	}
}

func TestRoute_Empty(t *testing.T) {
	if err := Route([]string{}); err != nil {
		t.Errorf("Empty args should not error: %v", err)
	}
}

func TestRoute_Unknown(t *testing.T) {
	err := Route([]string{"nonexistent"})
	if err == nil {
		t.Error("Unknown command should return error")
	}
}

func TestRoute_Aliases(t *testing.T) {
	_ = Route([]string{"ls"})
	_ = Route([]string{"rm", "test-app"})
}

func TestRoute_Aliases_InvalidName(t *testing.T) {
	err := Route([]string{"rm", "../etc"})
	if err == nil {
		t.Error("Should reject path traversal in app name")
	}
}

func TestPrintUsage(t *testing.T) {
	PrintUsage()
}

func TestRoute_StatusRequiresInit(t *testing.T) {
	err := Route([]string{"status"})
	if err == nil {
		t.Error("status should fail without init")
	}
}

func TestRoute_ListEmpty(t *testing.T) {
	err := Route([]string{"list"})
	if err != nil {
		t.Errorf("list should work without init: %v", err)
	}
}

func TestRoute_RestartWithoutArgs(t *testing.T) {
	err := Route([]string{"restart"})
	if err == nil {
		t.Error("restart without app name should return error")
	}
	if !strings.Contains(err.Error(), "application name") && !strings.Contains(err.Error(), "app-name") {
		t.Errorf("Error should mention application name required, got: %v", err)
	}
}

func TestRoute_StopWithoutArgs(t *testing.T) {
	err := Route([]string{"stop"})
	if err == nil {
		t.Error("stop without app name should return error")
	}
	if !strings.Contains(err.Error(), "application name") && !strings.Contains(err.Error(), "app-name") {
		t.Errorf("Error should mention application name required, got: %v", err)
	}
}

func TestRoute_ExecWithoutArgs(t *testing.T) {
	err := Route([]string{"exec"})
	if err == nil {
		t.Error("exec without args should return error")
	}
}

func TestRoute_ExecWithOnlyApp(t *testing.T) {
	err := Route([]string{"exec", "myapp"})
	if err == nil {
		t.Error("exec with only app name should return error")
	}
	if !strings.Contains(err.Error(), "usage") {
		t.Errorf("Error should mention usage, got: %v", err)
	}
}

func TestRoute_RestartNonExistent(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)

	err := Route([]string{"restart", "nonexistent"})
	if err == nil {
		t.Error("restart of non-existent app should return error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("Error should mention 'not found', got: %v", err)
	}
}

func TestRoute_StopNonExistent(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)

	err := Route([]string{"stop", "nonexistent"})
	if err == nil {
		t.Error("stop of non-existent app should return error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("Error should mention 'not found', got: %v", err)
	}
}

func TestRoute_ExecNonExistent(t *testing.T) {
	dir := t.TempDir()
	state.InitState(dir)

	err := Route([]string{"exec", "nonexistent", "ls"})
	if err == nil {
		t.Error("exec on non-existent app should return error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("Error should mention 'not found', got: %v", err)
	}
}

func TestRoute_RestartInvalidName(t *testing.T) {
	err := Route([]string{"restart", "../etc"})
	if err == nil {
		t.Error("Should reject path traversal in app name")
	}
}

func TestRoute_StopInvalidName(t *testing.T) {
	err := Route([]string{"stop", "../etc"})
	if err == nil {
		t.Error("Should reject path traversal in app name")
	}
}

func TestRoute_ExecInvalidName(t *testing.T) {
	err := Route([]string{"exec", "../etc", "ls"})
	if err == nil {
		t.Error("Should reject path traversal in app name")
	}
}
