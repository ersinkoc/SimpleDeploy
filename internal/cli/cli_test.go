package cli

import (
	"testing"
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
	// 'ls' alias for list — will fail without init but should route correctly
	_ = Route([]string{"ls"})
	_ = Route([]string{"rm", "test"})
}

func TestPrintUsage(t *testing.T) {
	// Should not panic
	PrintUsage()
}

func TestRoute_StatusRequiresInit(t *testing.T) {
	// status requires init — will return error
	err := Route([]string{"status"})
	if err == nil {
		t.Error("status should fail without init")
	}
}

func TestRoute_ListEmpty(t *testing.T) {
	// list now works without init — just shows empty
	err := Route([]string{"list"})
	if err != nil {
		t.Errorf("list should work without init: %v", err)
	}
}
