package state

import (
	"testing"
)

func TestValidateAppName_Valid(t *testing.T) {
	names := []string{
		"myapp",
		"my-app",
		"app123",
		"my-app-123",
		"a1",
		"ab",
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			if err := ValidateAppName(name); err != nil {
				t.Errorf("ValidateAppName(%q) should succeed, got: %v", name, err)
			}
		})
	}
}

func TestValidateAppName_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"single char", "a"},
		{"uppercase", "MyApp"},
		{"spaces", "my app"},
		{"path traversal", "../etc"},
		{"backslash", "my\\app"},
		{"dot", "my.app"},
		{"underscore", "my_app"},
		{"too long", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{"special chars", "my$app!"},
		{"starts with hyphen", "-app"},
		{"ends with hyphen", "app-"},
		{"double dot", "my..app"},
		{"slash", "my/app"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateAppName(tt.input); err == nil {
				t.Errorf("ValidateAppName(%q) should fail", tt.input)
			}
		})
	}
}
