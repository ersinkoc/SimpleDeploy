# Changelog

All notable changes to SimpleDeploy will be documented in this file.

## [Unreleased]

Second audit pass, from a file-by-file review of every production source file
plus four independent adversarial reviews (concurrency, injection surface, CLI
flow logic, webhook protocol correctness against provider documentation) — then
a third pass reviewing the fixes themselves, which found and corrected several
regressions they had introduced.

### Test coverage for the gaps that produced these bugs

Every gap CLAUDE.md named as the source of shipped bugs now has a test, each
covering something a unit test provably cannot. The Docker-backed ones are
opt-in (`SIMPLEDEPLOY_INTEGRATION=1`):

- **What a container's environment actually receives** — the only way to verify
  the `$`-escaping contract, since Compose interpolates after the YAML parse and
  a text assertion cannot see it.
- **Crash-loop detection against real Docker restart timing** — a stub cannot
  produce the restarting/running flicker that made a single status read report a
  crash-looping deploy as a success.
- **The single-file bind-mount contract** — `writeCaddyfile` and an atomic
  writer emit byte-identical files, so only a real mount tells them apart.
- **The fresh-install path** — every other state test points `InitState` at a
  directory that already exists, which is why "every first-ever init dies with
  could not acquire state lock" shipped.
- **The full chain, end to end** — a real deploy (real build, real container
  answering HTTP), a real signed push over real HTTP to a real webhook server, a
  redeploy that replaces the running version, then a push of a genuinely
  crashing build proving the **rollback fires**: the previous version serves
  again, `CurrentImage` still names the last working image, and the rolled-back
  attempt does not count as a deploy.

### Regressions found by reviewing the fixes (all fixed here)

- **Ctrl-C at a wizard prompt locked an app out for 90 minutes.** The new
  per-app lock is held across the deploy wizard's interactive prompts, and Go's
  default signal handling skips deferred functions — so interrupting left a lock
  behind with a fresh timestamp, blocking both a retry and `remove`. Locks are
  now released on SIGINT/SIGTERM.
- **A webhook push was answered `200 Deploy triggered` and then dropped** when a
  hand-run `redeploy`/`remove` held the app lock: the deploy attempt failed
  immediately and nothing retried, which is precisely the bug the in-process
  queue was added to prevent. Lock contention is now retried within the deploy
  budget.
- **`stop` was still silently undone by a concurrent redeploy.** `UpdateApp`
  protected the other fields but not Status, and neither `stop` nor `restart`
  took the lock — so a redeploy restarted the container and overwrote the status.
  Both commands now take the lock.
- **A failed lock write made release a no-op**, leaving the app locked for 90
  minutes; write/close errors now abort the acquisition. Same fix in the state
  lock.
- **The contention message invited deleting a live lock.** It reported a pid that
  is namespace-local when the holder is the containerised service (typically 1),
  so `ps 1` on the host looked like a crash leftover.
- **An aborted `init` reconfigure could leave the server with no proxy running.**
  The old proxy was stopped before the domain/email validators, which abort with
  no retry loop — a typo took every app offline with no hint why. The stop now
  happens after the new config is saved.
- **A crash-looping first deploy never printed the webhook URL or secret.** The
  new "not running correctly" branch returned before `printWebhookHelp`, which is
  the only place either value is ever shown.
- **Branch deletions triggered deploys.** GitHub delivers them as push events
  with a normal `refs/heads` ref plus `deleted: true`; the new non-branch gate
  did not catch them, so deleting the deployed branch kicked off a redeploy that
  then failed at `git fetch`. All-zero `after` (GitLab/Gitea) is handled too.
- **An unparseable payload deployed unconditionally.** Failing to read the body
  left the ref empty, which skipped every gate; it is now refused with 400.
- The webhook body cap was raised from 10 MB to GitHub's own 25 MB delivery
  limit — a lower cap only turned "never deploys" from a misleading 401 into an
  honest 413. The generated service compose also sets `stop_grace_period`, without
  which Docker SIGKILLed the container 10 s into a graceful shutdown that is
  meant to drain in-flight deploys.

### Push-to-deploy correctness

- **Gitea signatures were never accepted.** `X-Gitea-Signature` carries a bare
  hex HMAC-SHA256 digest, but verification required GitHub's `sha256=` prefix.
  Deploys only worked because modern Gitea also sends the GitHub-compatible
  header. The bare form is now accepted (the prefixed one still tolerated).
- **The branch filter was silently dead in two cases.** GitHub's
  `application/x-www-form-urlencoded` delivery mode sends `payload=<urlencoded
  JSON>`, which the ref parser could not read — an empty ref skipped the branch
  check, so pushes to *every* branch redeployed. Tag pushes arrive as `push`
  events with `refs/tags/...` and hit the same hole. Both are now handled: form
  bodies are parsed, and a non-branch ref is acknowledged without deploying.
- **`WebhookEnabled` was never enforced.** An app deployed with push-to-deploy
  declined still auto-deployed on every push; `list` displayed `Webhook: false`
  the whole time. Such pushes now get 403.
- **Pushes arriving mid-deploy were dropped but answered `200 Deploy
  triggered`.** The provider's delivery log showed success for a commit that was
  never deployed. One follow-up run is now queued (response `202`).
- **Oversized payloads were reported as bad signatures.** The 10 MB read cap
  truncated silently, so the HMAC was computed over partial bytes. Now `413`.

### Concurrency and state integrity

- **A removed app could be resurrected.** `remove` during a webhook redeploy was
  undone when the redeploy's final save re-inserted its minutes-old copy of the
  record; a concurrent `stop` likewise had its status overwritten. Commands that
  own only part of a record now use the new `state.UpdateApp`, which re-reads
  under the lock and fails if the app is gone.
- **The state lock could be held by two processes at once.** Unlocking removed
  the lock file unconditionally, so after two waiters recovered the same stale
  lock the loser tore down the winner's fresh lock. Unlock is now token-checked
  and stale recovery re-stats before removing.
- **Graceful shutdown could kill a deploy it should have waited for.**
  `ListenAndServe` returns as soon as `Shutdown` is called, so the in-flight
  deploy wait could run before a handler had registered its deploy.
- **Two builds in the same second collided on one image tag**, making the
  deployed image nondeterministic. Tags now carry milliseconds.
- **Deploys of one app are now serialized across processes.** A hand-run
  `redeploy` during a webhook-triggered deploy of the same app ran fully
  concurrently with it: both `git pull`ed the same source tree, the loser's
  rollback could revert the winner's *successful* deploy, and the winner's image
  prune could delete the image the loser had just built. `deploy`, `redeploy`
  and `remove` now hold a per-app lock and fail fast, naming the holder, instead
  of interleaving. This also closes the deploy-name race in which two wizard
  sessions picking the same name both passed the "already exists" check and the
  loser's cleanup deleted the winner's cloned source mid-build.
- **The webhook flood guard was tight enough to throttle real traffic.** Behind
  the proxy every delivery shares one bucket key, so the old 60/min was a global
  ceiling a busy multi-app server could trip with its own pushes — after which
  genuine deliveries got 429 and push-to-deploy silently stopped. Raised to
  600/min; see CLAUDE.md for why a stricter failure-only bucket does not help.

### Generated-config correctness

- **`$` in any value broke or silently changed the deploy.** Compose
  interpolates `$VAR`/`${VAR}` after the YAML parse: a bcrypt hash aborted
  `docker compose up` *after* the image build, and `${...}` could expand a
  generated database password (Compose auto-loads the app's own `.env`). All
  emitted values now escape `$`.
- **Caddy accepted domains no other component would.** `AddCaddyApp` used a
  looser pattern than the state validators; it now applies
  `state.ValidateAppDomain`, and validates header *values* as the Traefik path
  already did.
- **`remove` on an app record with no domain deleted the Caddyfile's global
  block** (the ACME email), because the block matcher matched a bare `{`.
- **git argv values are validated at the sink.** Redeploy passed `app.Branch`
  from state.json into `git fetch` with nothing in between; a branch beginning
  with `-` was parsed by git as an option.

### CLI behaviour

- `init` no longer silently regenerates the webhook secret on a reconfigure
  (which broke every already-configured repository webhook), and switching proxy
  type now stops the old proxy first and warns that existing apps are not
  re-routed — previously it overwrote the old proxy's compose file while that
  container still held :80/:443, leaving state describing a proxy that was not
  running.
- A failed deploy no longer deletes the app directory out from under *running*
  containers; it tears them down and removes the proxy route first.
- `deploy` no longer prints "https://… is ready!" for a container it just
  reported as crash-looping, and `restart` verifies the container stayed up
  instead of recording `running` unconditionally.
- An invalid answer at a multi-select prompt is reported instead of silently
  meaning "none" — a typo at the database prompt used to deploy an app with no
  database and no `DATABASE_URL`.

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

### Static analysis

- **Fixed the gosec finding that had kept the Security Scan workflow red since
  it was added.** `GeneratePassword` computed its rejection-sampling threshold
  as `byte(256 - 256%len(charset))` — a narrowing conversion that happens to be
  correct for the current 62-character set but silently wraps for any charset
  of length 1, and which the compiler cannot check. The threshold is now
  compared as an `int`.
- `GenerateSecret` and `GeneratePassword` reject non-positive lengths instead of
  panicking in `make([]byte, negative)`.
- The webhook server now sets `ReadHeaderTimeout` (and `IdleTimeout`). Without
  it, `ReadTimeout` alone does not bound a slowloris attack, because its clock
  only starts once the request line has been read — and this listener is
  reachable from the internet.

### Testing

- **Split integration tests from the unit suite.** Roughly forty tests drive a
  real Docker daemon — pulling images, building, starting containers — and the
  proxy ones additionally need exclusive use of ports 80/443, which an
  unprivileged runner cannot bind at all. They were gated only on "is Docker
  installed", so on a CI runner they all ran, and `go test ./...` had never
  passed there: the very first CI run, months before this release, already
  failed at the Test step while vet and build passed. The reported "13/13
  packages passing" reflected local Windows runs, where every one of these
  tests silently skipped.

  They are now opt-in via `SIMPLEDEPLOY_INTEGRATION=1` and run from a separate
  "Integration Tests" workflow (manual dispatch plus weekly). The push pipeline
  runs the unit suite, which is the part that can actually be green.

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
