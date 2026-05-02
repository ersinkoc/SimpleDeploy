package state

import (
	"fmt"
	"regexp"
	"strings"
)

// AppNameRegex matches valid application names.
// Exported for use by other packages.
var AppNameRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}[a-z0-9]$`)

// dnsLabelRegex matches a single DNS label per RFC 1123: 1-63 chars, lowercase
// letters/digits/hyphens, must start and end with alphanumeric.
var dnsLabelRegex = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// ValidateAppName checks that an application name is safe to use in
// file paths, Docker container names, DNS labels, and git URLs.
func ValidateAppName(name string) error {
	if name == "" {
		return fmt.Errorf("application name cannot be empty")
	}
	if len(name) < 2 {
		return fmt.Errorf("application name must be at least 2 characters")
	}
	if len(name) > 64 {
		return fmt.Errorf("application name must be at most 64 characters")
	}
	if !AppNameRegex.MatchString(name) {
		return fmt.Errorf("application name must contain only lowercase letters, digits, and hyphens (e.g. 'my-app-123')")
	}
	return nil
}

// ValidateBaseDomain checks that a domain is a syntactically valid hostname.
// This is critical because the base domain is interpolated unescaped into
// Traefik label rules (Host(`...`)) and Caddy site blocks; a malicious value
// containing backticks, parentheses, or whitespace could inject extra
// routing rules. Accepts only lowercase RFC 1123 hostnames with at least
// two labels (e.g. "apps.example.com").
func ValidateBaseDomain(domain string) error {
	if domain == "" {
		return fmt.Errorf("base domain cannot be empty")
	}
	if len(domain) > 253 {
		return fmt.Errorf("base domain must be at most 253 characters")
	}
	if strings.ContainsAny(domain, " \t\r\n`'\"()[]{}<>,;|&$\\") {
		return fmt.Errorf("base domain contains invalid characters")
	}
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return fmt.Errorf("base domain must have at least two labels (e.g. 'apps.example.com')")
	}
	for _, label := range labels {
		if !dnsLabelRegex.MatchString(label) {
			return fmt.Errorf("invalid DNS label %q in base domain: each label must be 1-63 chars, lowercase letters/digits/hyphens, starting and ending with alphanumeric", label)
		}
	}
	return nil
}

// ValidateSubdomain checks that a subdomain is a single safe DNS label.
// The subdomain is concatenated with the base domain and used as a per-app
// hostname; like the base domain it ends up inside Traefik Host(`...`) rules
// and Caddy site blocks, so it must be free of metacharacters that could
// inject additional routing config.
func ValidateSubdomain(sub string) error {
	if sub == "" {
		return fmt.Errorf("subdomain cannot be empty")
	}
	if len(sub) > 63 {
		return fmt.Errorf("subdomain must be at most 63 characters")
	}
	if strings.ContainsAny(sub, " \t\r\n`'\"()[]{}<>,;|&$\\.") {
		return fmt.Errorf("subdomain contains invalid characters")
	}
	if !dnsLabelRegex.MatchString(sub) {
		return fmt.Errorf("invalid subdomain %q: must be 1-63 chars, lowercase letters/digits/hyphens, starting and ending with alphanumeric", sub)
	}
	return nil
}

// ValidateAppDomain validates a fully-qualified per-app hostname (subdomain +
// base domain). Defense-in-depth check used at compose-generation time: a
// state file written before the validators existed, or one tampered with on
// disk, could still slip an unsafe value past the input layer.
func ValidateAppDomain(domain string) error {
	if domain == "" {
		return fmt.Errorf("app domain cannot be empty")
	}
	return ValidateBaseDomain(domain)
}

// emailRegex is intentionally narrow: most @ left-hand-side punctuation is
// rejected because the email value lands in YAML and Caddyfile contexts. We
// accept the common subset (alphanumerics, dot, underscore, hyphen, plus)
// rather than the full RFC 5322 grammar.
var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)

// ValidateEmail checks that an address is safe to embed in Caddyfile global
// blocks ("email %s") and Traefik compose YAML
// ("--certificatesresolvers.letsencrypt.acme.email=%s"). A newline or
// backtick in the raw value would let an attacker append directives, so we
// reject any whitespace/metacharacters explicitly in addition to the regex.
func ValidateEmail(email string) error {
	if email == "" {
		return fmt.Errorf("email cannot be empty")
	}
	if len(email) > 254 {
		return fmt.Errorf("email must be at most 254 characters")
	}
	if strings.ContainsAny(email, " \t\r\n`'\"()[]{}<>,;|&$\\") {
		return fmt.Errorf("email contains invalid characters")
	}
	if !emailRegex.MatchString(email) {
		return fmt.Errorf("invalid email address %q", email)
	}
	return nil
}

// repoURLRegex matches the three URL forms `git clone` actually consumes in
// SimpleDeploy: https://, git://, and the scp-style git@host:path. Local
// file paths are intentionally rejected — the deploy flow always pulls from
// a remote, and accepting `file:///etc/passwd` or `/etc/shadow` would let a
// caller make the daemon copy arbitrary host state into an app source dir.
var repoURLRegex = regexp.MustCompile(
	`^(?:` +
		// https://[user[:token]@]host[:port]/path(.git)
		`https?://[A-Za-z0-9._:%@\-]+(?:/[A-Za-z0-9._\-/~]+)+(?:\.git)?` +
		`|` +
		// git://host[:port]/path(.git)
		`git://[A-Za-z0-9._\-]+(?::[0-9]+)?(?:/[A-Za-z0-9._\-/~]+)+(?:\.git)?` +
		`|` +
		// user@host:path(.git) — scp-style
		`[A-Za-z0-9_\-]+@[A-Za-z0-9._\-]+:[A-Za-z0-9._\-/~]+(?:\.git)?` +
		`)$`,
)

// ValidateRepoURL guards the URL passed to git clone/pull. It is also stored
// on disk and emitted into a "simpledeploy.repo" compose label, so a value
// containing newlines, backticks, or shell metacharacters could leak into
// either context. We require one of the canonical forms (https://, git://,
// or scp-style git@host:path) and reject anything else, including local
// paths and ssh:// URLs whose user/host parsing varies between git versions.
func ValidateRepoURL(repo string) error {
	if repo == "" {
		return fmt.Errorf("repository URL cannot be empty")
	}
	if len(repo) > 1024 {
		return fmt.Errorf("repository URL too long (max 1024)")
	}
	if strings.ContainsAny(repo, " \t\r\n`'\"<>;|&$\\") {
		return fmt.Errorf("repository URL contains invalid characters")
	}
	if !repoURLRegex.MatchString(repo) {
		return fmt.Errorf("repository URL must be https://, git://, or git@host:path form")
	}
	return nil
}

// branchRegex follows git's own ref format rules in spirit: alphanumerics
// plus `-` `_` `/` `.`. It is deliberately tighter than git's full grammar
// (which allows `+`, `=`, etc.) because the branch name lands in compose
// labels and is also passed as an argv to git — keeping the alphabet small
// avoids having to reason about every git version's parser.
var branchRegex = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._\-/]*$`)

// ValidateBranch checks the branch name passed to git clone/pull. The name
// is also written into a "simpledeploy.branch" compose label (yaml-quoted)
// and shown to the user; we reject `..` and trailing `.lock` per git's own
// reference-format rules so future git operations don't surprise the user.
func ValidateBranch(branch string) error {
	if branch == "" {
		return fmt.Errorf("branch cannot be empty")
	}
	if len(branch) > 255 {
		return fmt.Errorf("branch name too long (max 255)")
	}
	if strings.ContainsAny(branch, " \t\r\n`'\"<>;|&$\\:?*[~^") {
		return fmt.Errorf("branch contains invalid characters")
	}
	if strings.Contains(branch, "..") {
		return fmt.Errorf("branch must not contain '..'")
	}
	if strings.HasSuffix(branch, ".lock") {
		return fmt.Errorf("branch must not end with '.lock'")
	}
	if strings.HasSuffix(branch, "/") || strings.HasPrefix(branch, "/") {
		return fmt.Errorf("branch must not start or end with '/'")
	}
	if !branchRegex.MatchString(branch) {
		return fmt.Errorf("invalid branch name %q", branch)
	}
	return nil
}

// imageTagRegex matches the Docker reference grammar we actually emit:
// "<name>[:<tag>]" where name is alphanumerics + `-` `_` `.` `/` (registry
// path) and tag is alphanumerics + `-` `_` `.`. Digests (@sha256:...) are
// not currently produced by SimpleDeploy and so deliberately rejected; if
// support is added, broaden this regex first.
var imageTagRegex = regexp.MustCompile(`^[a-z0-9]([a-z0-9._\-/]*[a-z0-9])?(:[A-Za-z0-9._\-]+)?$`)

// ValidateImageTag is a defense-in-depth check on values interpolated into
// "image: %s" compose lines. The build pipeline currently produces names
// like "qd-myapp:20260502-153045"; this validator codifies that contract so
// a corrupted state file or a future buildpack bug can't put YAML-breaking
// characters into the compose output.
func ValidateImageTag(tag string) error {
	if tag == "" {
		return fmt.Errorf("image tag cannot be empty")
	}
	if len(tag) > 255 {
		return fmt.Errorf("image tag too long (max 255)")
	}
	if strings.ContainsAny(tag, " \t\r\n`'\"<>;|&$\\") {
		return fmt.Errorf("image tag contains invalid characters")
	}
	if !imageTagRegex.MatchString(tag) {
		return fmt.Errorf("invalid image tag %q (expected name[:tag] with [a-z0-9._/-])", tag)
	}
	return nil
}

// envKeyRegex matches a POSIX-ish environment variable name. Identical to
// the regex compose/generator.go uses internally; centralizing it here lets
// the deploy wizard reject bad keys at input time instead of failing a
// later compose-generation step.
var envKeyRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ValidateEnvKey rejects environment variable names that would be illegal
// inside a YAML environment block ("KEY=value") or a shell `export`. The
// value half is left unchecked — yamlQuote handles escaping at the consumer.
func ValidateEnvKey(key string) error {
	if key == "" {
		return fmt.Errorf("env key cannot be empty")
	}
	if len(key) > 255 {
		return fmt.Errorf("env key too long (max 255)")
	}
	if !envKeyRegex.MatchString(key) {
		return fmt.Errorf("invalid env key %q (must match [A-Za-z_][A-Za-z0-9_]*)", key)
	}
	return nil
}

// dbTypeRegex codifies the format of a database type identifier as it lands
// in compose YAML — service names ("qd-app-{type}"), container names, and
// the depends_on key. We deliberately do NOT enumerate the supported set
// here (mysql/postgresql/...) because that lookup belongs to the db
// provisioner; this validator's job is only to ensure the string is safe to
// interpolate. Lowercase ASCII only — the provisioner's static map keys are
// all lowercase.
var dbTypeRegex = regexp.MustCompile(`^[a-z][a-z0-9]{0,31}$`)

// ValidateDBType is a defense-in-depth check on values interpolated into
// compose service names ("qd-%s-%s") and the depends_on map. The actual
// supported-database lookup happens in db.GetDatabaseConfig — this only
// guards the string form. A state file naming an unknown type with
// metacharacters would otherwise reach compose.Generate before failing.
func ValidateDBType(t string) error {
	if t == "" {
		return fmt.Errorf("database type cannot be empty")
	}
	if len(t) > 32 {
		return fmt.Errorf("database type too long (max 32)")
	}
	if !dbTypeRegex.MatchString(t) {
		return fmt.Errorf("invalid database type %q (must match [a-z][a-z0-9]*)", t)
	}
	return nil
}

// containerPathRegex matches an absolute Linux path with the conservative
// alphabet we actually emit in databaseDefs ("/var/lib/mysql", "/data/db",
// "/var/lib/postgresql/data"). Spaces, glob metachars, quotes, and shell
// operators are rejected so the value is safe to interpolate into
// "volumes: - name:%s" without further escaping.
var containerPathRegex = regexp.MustCompile(`^/[A-Za-z0-9_./\-]*$`)

// ValidateContainerPath checks a container-side mount path. The values
// originate from a hardcoded map in db/provisioner.go, so a violation here
// means the state file or the map was tampered with — fail loudly rather
// than silently emit a malformed compose file. Rejects `..` segments and
// any of the metacharacters that could break out of a YAML scalar.
func ValidateContainerPath(p string) error {
	if p == "" {
		return fmt.Errorf("container path cannot be empty")
	}
	if len(p) > 4096 {
		return fmt.Errorf("container path too long (max 4096)")
	}
	if !strings.HasPrefix(p, "/") {
		return fmt.Errorf("container path must be absolute (start with '/')")
	}
	if !containerPathRegex.MatchString(p) {
		return fmt.Errorf("container path contains invalid characters")
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return fmt.Errorf("container path must not contain '..' segments")
		}
	}
	return nil
}

// volumeNameRegex follows Docker's documented volume-name grammar:
// "[a-zA-Z0-9][a-zA-Z0-9_.-]+". We require a non-empty character after the
// leading alphanumeric (the {1,254} below) so single-character names — which
// Docker actually accepts as well — are still allowed.
var volumeNameRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.\-]{0,254}$`)

// ValidateVolumeName guards the named-volume identifier emitted into
// "volumes: - %s:..." and the top-level volumes block. The provisioner
// builds it as "qd-{appName}-{dbType}-data" from values both validated
// upstream, so this is defense-in-depth: a state file with a hand-edited
// VolumeName must still be safe to interpolate.
func ValidateVolumeName(name string) error {
	if name == "" {
		return fmt.Errorf("volume name cannot be empty")
	}
	if len(name) > 255 {
		return fmt.Errorf("volume name too long (max 255)")
	}
	if !volumeNameRegex.MatchString(name) {
		return fmt.Errorf("invalid volume name %q (must match [a-zA-Z0-9][a-zA-Z0-9_.-]*)", name)
	}
	return nil
}
