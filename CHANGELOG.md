# Changelog

All notable changes to SimpleDeploy will be documented in this file.

## [0.1.0] - 2026-07-29

Audit release. Every item below is a defect that was present in 0.0.8 and is
fixed here — several of them meant a headline feature did not work at all.

### Features that did not work

- **Push-to-deploy was never reachable.** The deploy wizard advertised
  `https://<domain>/_qd/webhook/<app>`, but nothing routed that path: Traefik's
  docker provider only discovers containers and the webhook server is a host
  process, while the Caddyfile only ever got a per-app block. `init` now writes
  a real route for both proxies (Traefik file provider, Caddy `handle /_qd/*`)
  and the generated proxy compose maps `host.docker.internal`.
- **MongoDB containers could not start.** `MONGO_INITDB_ROOT_USERNAME` was never
  emitted, so the mongo entrypoint aborted on boot. The connection string also
  lacked `authSource=admin`, so authentication would have failed regardless.
- **The Node.js Dockerfile template failed on any project with a build step**
  (`npm ci --production` followed by `npm run build`). Python broke on
  `pyproject.toml`-only projects, Ruby required a committed `Gemfile.lock`, and
  Go pinned a base image tag that does not exist.
- **Importing an existing `.env` was impossible and lossy.** The path check
  required the file to live inside the app directory SimpleDeploy had just
  created; if it had passed, the imported content was truncated by the
  subsequent write of generated variables.
- **Containerised service mode could not run.** The compose file mounted
  `/opt/simpledeploy` while state lives in `~/.simpledeploy`, and the container's
  machine ID differs from the host's, so all secret decryption failed. State and
  machine-id are now mounted, with `SIMPLEDEPLOY_STATE_DIR` to locate them.

### Security

- **App containers no longer publish host ports.** `ports: - "3000"` bound each
  app to a random port on `0.0.0.0`, reachable over plain HTTP on the public IP,
  bypassing the proxy's TLS and security headers. Now `expose`.
- **The webhook server refuses to deploy without a configured secret.** With an
  empty secret every signature-verification branch was skipped, so any anonymous
  POST could trigger a pull-and-run of repository code. Rejected per-request and
  at startup.
- **Rate limiter no longer locks out active clients permanently.** It compared
  against `lastSeen` while refreshing `lastSeen` on every request, so the window
  never elapsed for a steady caller. Now a true fixed window.
- Git tokens are redacted from error output, not just the repository URL.
- Generated `.dockerignore` keeps `.git` out of built images.
- `redeploy` writes `docker-compose.yml` with mode 0600, matching initial deploy
  (the file embeds database root passwords).

### Reliability

- **Redeploy verifies the new container and rolls back.** Status was previously
  set to `running` unconditionally, so a crashing image reported success.
- **Image pruning actually runs.** It was in fire-and-forget goroutines launched
  microseconds before process exit, so images accumulated until the disk filled.
- **`AskRequired` and `Choose` no longer hang on EOF.** With stdin closed
  (piped input, CI) they looped forever printing a validation error.
- **`git clone` recovers from a leftover source tree**, and `git pull` is now
  fetch + hard reset — the previous merge wedged permanently on local divergence
  and misbehaved against shallow clones.
- `sanitizeOutput` with an empty repo URL no longer interleaves `<redacted>`
  between every character of the git error message.
- The Docker Compose plugin is checked on hosts that already have the engine,
  not only after a fresh install.
- Compose generation is deterministic (sorted maps); `list` and `status` output
  in stable order.
- Failed or cancelled deploys clean up the app directory instead of leaving
  orphaned database credentials on disk.
- Extra headers are validated before the image is built, not after.
- Container readiness is polled instead of checked once after a fixed 2 s sleep.
- `logs` defaults to a bounded one-shot dump; follow is opt-in via `-f`.
- Old image pruning sorts by tag so it can no longer delete the running image.

### Release engineering

- The release workflow now runs `go vet`, the test suite, and the race detector
  **before** anything is built. Previously a tag pushed on a broken commit
  published broken binaries straight to everyone running `install.sh`.
- Releases publish `SHA256SUMS`, and `install.sh` verifies the downloaded binary
  against it, staging in a temp directory so a failed or unverified download
  never leaves a partial `simpledeploy` behind.
- Release notes are generated from this file, and the workflow refuses to
  publish a tag with no matching changelog section.
- The built binary's reported version is asserted against the tag, catching a
  silently-wrong `-ldflags -X` path.

### Other

- Version is stamped via `-ldflags` from the Makefile and the release workflow,
  so `simpledeploy version` cannot drift from the tag.
- `make verify` runs everything CI does plus a binary smoke test, including a
  guard that `deploy` terminates on closed stdin.
- `templates/` is dead code — it is not embedded and lacks the validation the
  real generators perform. Delete it.

## [0.0.8] - 2026-05-03

### Security
- Fail-closed credential encryption: any AES-256-GCM error aborts `RunDeploy` instead of falling back to plaintext
- File mode tightening: proxy `.env` files written at 0600, configs at 0644/0755
- IPv6 rate-limit key fix: `X-Forwarded-For` with IPv6 no longer produces malformed limiter keys
- Database compose field validation: rejects empty engine, name, or version before generating YAML
- Header-name validation in proxy paths: rejects empty names and names containing colons
- Duplicate prevention: `RunDeploy` skips redundant builds when image hash is unchanged; Caddy/Traefik proxy configs deduplicate on redeploy

### Reliability
- Context propagation end-to-end: webhook timeout signals now propagate into `git pull`, `docker build`, and `docker compose up` subprocesses
- Proxy setup bounded by `proxySetupTimeout` (5 min) and `proxyExecTimeout` (30 s) to prevent wedged-Docker hangs
- Atomic Caddyfile writes via temp-file + rename to prevent partial config corruption

### Code Quality
- Comprehensive mock signature updates across all packages for ctx-aware testing

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
