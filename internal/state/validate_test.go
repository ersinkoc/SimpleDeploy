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

func TestValidateSubdomain_Valid(t *testing.T) {
	subs := []string{"app", "my-app", "app123", "a", "a1", strings.Repeat("a", 63)}
	for _, s := range subs {
		t.Run(s, func(t *testing.T) {
			if err := ValidateSubdomain(s); err != nil {
				t.Errorf("ValidateSubdomain(%q) should succeed, got: %v", s, err)
			}
		})
	}
}

func TestValidateSubdomain_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"too long", strings.Repeat("a", 64)},
		{"uppercase", "MyApp"},
		{"dot", "my.app"},
		{"leading hyphen", "-app"},
		{"trailing hyphen", "app-"},
		{"underscore", "my_app"},
		{"backtick", "ev`il"},
		{"single quote", "ev'il"},
		{"paren", "ev(il"},
		{"semicolon", "evil;"},
		{"space", "ev il"},
		{"newline", "evil\nbad"},
		{"backslash", "ev\\il"},
		{"slash", "ev/il"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateSubdomain(tt.input); err == nil {
				t.Errorf("ValidateSubdomain(%q) should fail", tt.input)
			}
		})
	}
}

func TestValidateAppDomain_Valid(t *testing.T) {
	domains := []string{"app.example.com", "my-app.apps.example.com", "a.b"}
	for _, d := range domains {
		t.Run(d, func(t *testing.T) {
			if err := ValidateAppDomain(d); err != nil {
				t.Errorf("ValidateAppDomain(%q) should succeed, got: %v", d, err)
			}
		})
	}
}

func TestValidateAppDomain_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"single label", "localhost"},
		{"backtick", "ev`il.com"},
		{"paren", "ev(il.com"},
		{"newline", "evil.com\nbad"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateAppDomain(tt.input); err == nil {
				t.Errorf("ValidateAppDomain(%q) should fail", tt.input)
			}
		})
	}
}

func TestValidateEmail_Valid(t *testing.T) {
	emails := []string{
		"user@example.com",
		"user.name@example.com",
		"user+tag@example.com",
		"user_name@sub.example.co.uk",
		"a@b.co",
	}
	for _, e := range emails {
		t.Run(e, func(t *testing.T) {
			if err := ValidateEmail(e); err != nil {
				t.Errorf("ValidateEmail(%q) should succeed, got: %v", e, err)
			}
		})
	}
}

func TestValidateEmail_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"no @", "userexample.com"},
		{"no domain", "user@"},
		{"no local", "@example.com"},
		{"single label domain", "user@localhost"},
		{"uppercase domain", "user@EXAMPLE.com"},
		// Injection vectors that must be rejected because the email is
		// interpolated raw into Caddyfile and Traefik compose YAML.
		{"backtick", "user@example.com`"},
		{"newline", "user@example.com\n}\nbad{"},
		{"space", "user @example.com"},
		{"semicolon", "user@example.com;"},
		{"paren", "user@ex(ample.com"},
		{"single quote", "user'@example.com"},
		{"backslash", "user\\@example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateEmail(tt.input); err == nil {
				t.Errorf("ValidateEmail(%q) should fail", tt.input)
			}
		})
	}
}
