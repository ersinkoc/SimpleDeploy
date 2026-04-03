# Changelog

All notable changes to SimpleDeploy will be documented in this file.

## [0.0.7] - 2026-04-03

### Security
- YAML injection prevention: repo URL and branch now properly quoted in compose labels
- ACME email validation with regex in Traefik setup
- Environment variable key validation (must match `[A-Za-z_][A-Za-z0-9_]*`)
- IP extraction panic safety in webhook server
- Deep copy returned from `GetApp` to prevent shared mutable state race conditions

### Reliability
- MongoDB connection string fixed (missing database name in template)
- Webhook deploy goroutine leak fixed (timeout now waits for inner goroutine)
- Lock timeout increased 5s → 30s to prevent false stale detection on slow I/O
- Caddy block removal now tracks brace depth to correctly handle nested blocks

### Bug Fixes
- Node.js Dockerfile now properly fails on build errors (removed `|| true`)
- `GenerateSecret` now produces correct entropy (was producing half)
- `yamlQuote` now escapes dangerous chars instead of rejecting them
- Restart/Stop commands no longer load state twice

### Performance
- Restart/Stop: single state load instead of double
- `.env` file now deterministically sorted for reproducible deployments

### Code Quality
- Dead code removal in `detectNodePort`

## [0.0.6] - 2026-04-02

### Security
- Path traversal protection in `.env` file handling
- YAML injection prevention in compose generation (`${`, `#`, special chars blocked)
- Caddyfile header value escaping
- Git token sanitization in error output

### Reliability
- State file locking with stale-lock detection (cross-platform)
- Deploy lock race condition fix (context-based timeout replacing `time.AfterFunc`)
- Goroutine leak fix in rate limiter cleanup (ticker + stop channel)
- Proper error propagation in `ContainerStatus`
- Graceful token decrypt failure in redeploy (warn + continue instead of hard-fail)

### Code Quality
- Dead code removal: `BuildImageWithDockerfile`, `TagImage`, `PullImage`, `ContainerExists` wrapper, `GetShortHash`, `DetectBranch`, `IsRepo`, `ParseGitHubEvent`
- Dead struct fields removed: `Container`, `Port`, `ConnEnvKey` from `DatabaseConfig`
- Container name helper consolidation (`docker.ContainerName`)
- Regex pattern consolidation (`state.AppNameRegex`)
- Go version fixed (1.26.1 → 1.23.0)

### CI/CD
- Race detector workflow (`.github/workflows/race.yml`)
- Security scanner workflow (`.github/workflows/security.yml`)

## [0.0.5] - 2026-03-30

### Changed
- Bump version to 0.0.5
- Remove dead code from codebase

## [0.0.4] - 2026-03-28

### Changed
- Bump version to 0.0.4
- Add dependency injection for testing across all packages

## [0.0.3] - 2026-03-25

### Fixed
- Sanitize git pull error output to prevent token leakage
- Use `getProxyDir()`/`getServiceDir()` consistently
