package state

import (
	"strings"
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

func TestValidateBaseDomain_Valid(t *testing.T) {
	domains := []string{
		"example.com",
		"apps.example.com",
		"deeply.nested.example.co.uk",
		"a.b",
		"my-apps.example.com",
		"123.example.com",
	}
	for _, d := range domains {
		t.Run(d, func(t *testing.T) {
			if err := ValidateBaseDomain(d); err != nil {
				t.Errorf("ValidateBaseDomain(%q) should succeed, got: %v", d, err)
			}
		})
	}
}

func TestValidateBaseDomain_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"single label", "localhost"},
		{"uppercase", "Example.com"},
		{"trailing dot label", "example.com."},
		{"leading hyphen", "-example.com"},
		{"trailing hyphen", "example-.com"},
		{"double dot", "example..com"},
		{"underscore", "my_app.example.com"},
		// Injection vectors that must be rejected because the value is
		// interpolated unescaped into Traefik label rules and Caddy site
		// blocks.
		{"backtick", "evil.com`"},
		{"backtick mid", "ev`il.com"},
		{"single quote", "ev'il.com"},
		{"double quote", "ev\"il.com"},
		{"paren", "ev(il.com"},
		{"semicolon", "evil.com;"},
		{"space", "ev il.com"},
		{"newline", "evil.com\nbad"},
		{"too long", strings.Repeat("a", 254) + ".com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateBaseDomain(tt.input); err == nil {
				t.Errorf("ValidateBaseDomain(%q) should fail", tt.input)
			}
		})
	}
}
