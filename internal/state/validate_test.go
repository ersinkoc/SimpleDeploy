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

func TestValidateRepoURL_Valid(t *testing.T) {
	urls := []string{
		"https://github.com/user/repo",
		"https://github.com/user/repo.git",
		"http://gitea.local:3000/u/r.git",
		"https://token@github.com/user/repo.git",
		"https://user:token@gitlab.example.com/group/sub/repo.git",
		"git://git.kernel.org/pub/scm/linux/kernel/git/torvalds/linux.git",
		"git@github.com:user/repo.git",
		"git@gitlab.example.com:group/sub/repo",
	}
	for _, u := range urls {
		t.Run(u, func(t *testing.T) {
			if err := ValidateRepoURL(u); err != nil {
				t.Errorf("ValidateRepoURL(%q) should succeed, got: %v", u, err)
			}
		})
	}
}

func TestValidateRepoURL_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"local path", "/etc/passwd"},
		{"file scheme", "file:///etc/passwd"},
		{"ssh scheme", "ssh://git@github.com/user/repo.git"},
		{"backtick", "https://github.com/u/r`bad"},
		{"semicolon", "https://github.com/u/r;rm -rf /"},
		{"space", "https://github.com/u/ repo"},
		{"newline", "https://github.com/u/r\nbad"},
		{"single quote", "https://github.com/u/r'bad"},
		{"unsupported scheme", "ftp://github.com/u/r"},
		{"raw host", "github.com/user/repo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateRepoURL(tt.input); err == nil {
				t.Errorf("ValidateRepoURL(%q) should fail", tt.input)
			}
		})
	}
}

func TestValidateBranch_Valid(t *testing.T) {
	branches := []string{
		"main",
		"master",
		"develop",
		"feature/login",
		"release-1.2.3",
		"hotfix/2026.05",
		"v1.0.0",
	}
	for _, b := range branches {
		t.Run(b, func(t *testing.T) {
			if err := ValidateBranch(b); err != nil {
				t.Errorf("ValidateBranch(%q) should succeed, got: %v", b, err)
			}
		})
	}
}

func TestValidateBranch_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"double dot", "feature..branch"},
		{"trailing .lock", "feature.lock"},
		{"trailing slash", "feature/"},
		{"leading slash", "/feature"},
		{"backtick", "feat`bad"},
		{"semicolon", "feat;bad"},
		{"newline", "feat\nbad"},
		{"colon", "feat:bad"},
		{"caret", "feat^bad"},
		{"tilde", "feat~bad"},
		{"question mark", "feat?bad"},
		{"asterisk", "feat*bad"},
		{"left bracket", "feat[bad"},
		{"leading dot", ".badstart"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateBranch(tt.input); err == nil {
				t.Errorf("ValidateBranch(%q) should fail", tt.input)
			}
		})
	}
}

func TestValidateImageTag_Valid(t *testing.T) {
	tags := []string{
		"qd-myapp:20260502-153045",
		"alpine",
		"alpine:3.19",
		"registry.example.com/team/app:1.2.3",
		"library/redis:7.2-alpine",
		"my-app",
	}
	for _, ttag := range tags {
		t.Run(ttag, func(t *testing.T) {
			if err := ValidateImageTag(ttag); err != nil {
				t.Errorf("ValidateImageTag(%q) should succeed, got: %v", ttag, err)
			}
		})
	}
}

func TestValidateImageTag_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"uppercase name", "MyApp:1.0"},
		{"backtick", "app:tag`"},
		{"space", "app :tag"},
		{"newline", "app:tag\nbad"},
		{"semicolon", "app:tag;rm"},
		{"trailing dash", "app-:tag"},
		{"sha256 digest", "app@sha256:deadbeef"},
		{"empty tag", "app:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateImageTag(tt.input); err == nil {
				t.Errorf("ValidateImageTag(%q) should fail", tt.input)
			}
		})
	}
}

func TestValidateEnvKey_Valid(t *testing.T) {
	keys := []string{"FOO", "FOO_BAR", "_PRIVATE", "foo123", "X"}
	for _, k := range keys {
		t.Run(k, func(t *testing.T) {
			if err := ValidateEnvKey(k); err != nil {
				t.Errorf("ValidateEnvKey(%q) should succeed, got: %v", k, err)
			}
		})
	}
}

func TestValidateEnvKey_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"leading digit", "1FOO"},
		{"hyphen", "FOO-BAR"},
		{"dot", "FOO.BAR"},
		{"space", "FOO BAR"},
		{"newline", "FOO\nBAR"},
		{"equals", "FOO=BAR"},
		{"backtick", "FOO`BAR"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateEnvKey(tt.input); err == nil {
				t.Errorf("ValidateEnvKey(%q) should fail", tt.input)
			}
		})
	}
}
