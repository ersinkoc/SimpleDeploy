# SimpleDeploy — System Review & Refactoring Report

**Date:** 2026-07-31
**Scope:** every production source file (`main.go` + 12 packages under `internal/`, ~6,450 lines), the test suite (~15,400 lines), the five GitHub workflows, the Makefile, and the shipping scripts.
**Method:** file-by-file manual reading, seven independent adversarial review passes (concurrency, injection surface, CLI flow logic, webhook protocol correctness against provider documentation, plus three passes reviewing the fixes themselves), and behaviour verification against a live Docker daemon on both Windows and Linux (non-root, CI's Go 1.23, cgo-enabled race detector).

This report describes the system as it stands **after** the audit fixes of the last eight commits (`a39c528`..`88f3add`). Bugs found during the audit are already fixed and are listed here only where they motivate a structural recommendation. Everything below is either a refactoring opportunity, a deliberate limitation worth re-examining, or a strength that any refactor must preserve.

---

## 1. Executive Summary

SimpleDeploy is a zero-dependency, single-binary Go PaaS CLI in unusually good health for its size: 89.6% statement coverage, deterministic config generation, a layered validator architecture with defense-in-depth at every sink, and an opt-in integration tier that now proves behaviour (what a container *receives*, whether a crash loop is *really* detected, whether a real Caddy *accepts and follows* the generated config, the full deploy → push → redeploy → rollback chain) rather than emitted text.

The largest remaining risks are **structural**, not point bugs:

1. **Global mutable state as the dependency-injection mechanism** (48 package-level function variables in `internal/cli/deps.go` alone, plus globals in `state`, `wizard`, `proxy`, `config`). It works, but it already produced one data race in the test suite and makes every test responsible for save/restore discipline. *(§4.1)*
2. **`RunDeploy` is an 804-line file whose main function interleaves prompting, validation, provisioning, building, and verification** in one pass. It is correct today because it was audited line-by-line; it will not stay correct under casual modification. *(§4.2)*
3. **The Caddyfile is edited incrementally with a hand-rolled brace-counting parser** instead of being regenerated from state. Three separate shipped bugs trace to this choice. *(§4.3)*
4. **Redeploy patches one line of the compose file instead of regenerating it**, because operator environment variables live only in the generated artifacts, not in state. This makes several categories of state change silently non-propagating. *(§4.4)*

None of these require urgent action — the current code is verified working — but each is the kind of debt that converts the next feature request into a regression. A prioritized roadmap is in §6.

---

## 2. Architecture Overview

```
main.go ──► internal/cli (Route → per-command Run* handlers)
              │
              ├─► internal/wizard      interactive prompts, ANSI output
              ├─► internal/state       state.json (per-field AES-256-GCM), validators, file lock
              ├─► internal/applock     cross-process per-app deploy lock
              ├─► internal/git         clone/pull via git argv, GIT_ASKPASS token handling
              ├─► internal/buildpack   app-type detection + embedded Dockerfile templates
              ├─► internal/docker      docker/compose argv wrappers, image lifecycle
              ├─► internal/compose     docker-compose.yml generator (text emission)
              ├─► internal/proxy       Traefik compose + Caddyfile emission, webhook route
              ├─► internal/db          database service definitions + credential generation
              ├─► internal/webhook     HTTP push-to-deploy server (GitHub/GitLab/Gitea)
              ├─► internal/runner      self-containerisation as a compose service
              └─► internal/config      path resolution (BaseDir, HomeDataDir)
```

Key data flows:

- **Deploy:** wizard input → validators → git clone → buildpack Dockerfile → `docker build` → compose generation → `compose up` → container-stability verification → state save. The whole span holds a cross-process per-app lock.
- **Push-to-deploy:** provider POST → HMAC verification → ref/branch/deletion gates → per-app queue (in-process) → `RunRedeployContext` → per-app file lock (cross-process, retried on contention) → pull/build/patch-compose/up → verify or roll back → `state.UpdateApp`.
- **State:** plain JSON at `~/.simpledeploy/state.json` (mode 0600); only `git_token` and `db_credentials` values are encrypted (machine-id-derived key). Writes are temp+rename under an in-process mutex plus a token-checked cross-process lock file.

Two deployment modes share every artifact: the CLI as a host process, and the same binary inside the `qd-service` container (state dir and BaseDir bind-mounted at identical host paths, `/etc/machine-id` mounted so ciphertext stays decryptable).

---

## 3. Strengths — Preserve These in Any Refactor

These are load-bearing properties, most of them earned through shipped bugs (each is documented with its history in `CLAUDE.md`). A refactor that "cleans up" any of them reintroduces a known failure.

| Property | Where | Why it exists |
|---|---|---|
| Deterministic generation: every map walk sorted | `compose.sortedKeys`, `proxy.sortedHeaderKeys` | unsorted walks rewrote configs and reloaded the proxy on every no-op redeploy |
| `expose`, never `ports`, for app containers | `compose/generator.go` | a published port bypasses proxy TLS/headers on the public IP |
| YAML **map form** for `environment:` | `compose/generator.go` | list form delivered literal quote characters inside values |
| `$` doubled in every emitted compose value | `escapeComposeInterpolation` | Compose interpolates after the YAML parse; bcrypt hashes crashed deploys, `${…}` could expand secrets |
| Caddyfile written **in place**, never temp+rename | `proxy.writeCaddyfile` | single-file bind mounts pin an inode; rename orphans the running container's view |
| Container "up" = stable `running` + unmoved restart counter | `cli.waitForContainer` | `restart: unless-stopped` makes crash loops flicker through `running` |
| UTC + millisecond, lexically sortable image tags | `docker.BuildImage` | prune trusts reverse string sort as recency; same-second builds collided |
| Validators at input **and** at every sink | `state/validate.go` + call sites | state.json is plain JSON on disk; tampered/legacy files must not become injection vectors |
| Token-checked lock release + re-stat stale recovery | `state.lockStateFile`, `applock` | unconditional removal deleted other processes' live locks |
| Partial-field writes via `state.UpdateApp` | `state`, used by redeploy/stop/restart | whole-record `SaveApp` resurrections and lost concurrent updates |
| Synchronous cleanup at command end | `pruneImages` call sites | end-of-command goroutines never ran before process exit |
| Fail-closed webhook gates (unparseable payload → 400, deletion → ignored, tag → ignored, disabled app → 403) | `webhook/server.go` | each was a fail-open hole that deployed when it should not have |
| The zero-dependency constraint (`go.mod` has no requires) | everywhere | single-binary distribution story; also removes supply-chain surface |

The comment discipline itself is an asset: nearly every non-obvious decision carries its failure history inline. Refactors should move these comments with the code, not drop them.

---

## 4. Findings and Refactoring Opportunities

Ordered by architectural weight, not severity. Items marked **[verified]** were confirmed by executing code; items marked **[observed]** come from reading.

### 4.1 Dependency injection via package-level mutable variables

**[verified — it has already caused a data race]**

`internal/cli/deps.go` holds ~48 package-level `var` function pointers (`gitClone`, `dockerBuildImage`, `stateSaveApp`, `proxyReloadCaddy`, …). The same pattern repeats in `state` (`osOpenFile`, `jsonMarshalIndent`, …), `docker` (`newDockerCmdContext`, all timeouts), `proxy`, `git`, `db`, and `wizard` (a global `bufio.Scanner` with `SetScannerForTesting`).

Consequences observed this audit:

- A test that leaked a goroutine (`go Route(...)`) raced other tests' swap/restore of these variables. The race detector only tripped on Linux; the race workflow could have gone red at any time. (Fixed by making the call synchronous — but the *mechanism* that made it dangerous is unchanged.)
- Every test must hand-write `old := X; X = mock; defer func(){ X = old }()`; there are hundreds of these blocks, and a single forgotten restore poisons subsequent tests in the same package (tests run with `-p=1` for exactly this reason).
- `-p=1 -count=1` is mandatory suite-wide, serializing what could be parallel.

**Recommendation.** Introduce an explicit dependency container per package boundary, e.g.:

```go
// internal/cli
type App struct {
    Git    GitClient      // Clone, Pull
    Docker DockerClient   // BuildImage, ComposeUp, ContainerStatus, ...
    Proxy  ProxyClient
    State  StateStore
    Prompt Prompter       // wizard
    Lock   func(app string) (release func(), err error)
}
func (a *App) RunDeploy() error { ... }
```

`Route` constructs one production `App`; tests construct theirs with fakes and get parallelizable, race-free isolation for free. Migration can be incremental (one command at a time; the package vars can delegate to a default `App` during transition). This is the single highest-leverage refactor in the codebase — it removes the mechanism behind an entire class of test-infrastructure races and unlocks dropping `-p=1`.

**Effort:** large (touches every command and most tests) but mechanical. **Risk:** low if done command-by-command with the suite green between steps.

### 4.2 `RunDeploy` interleaves interaction, validation, and execution (804-line file)

**[observed]**

`RunDeploy` runs the full wizard *and* the full deployment in one function: ~12 interactive prompts, validation, DB credential provisioning and encryption, the deferred multi-mode cleanup (`deployed` / `containersStarted` flags), image build, compose generation, container verification, proxy wiring, and state save. It is currently correct — every path was audited — but:

- The deferred cleanup's correctness depends on variable initialization order across 300 lines (`cfg` before the defer, `app.Domain` before `containersStarted`); the audit found one real bug here (post-startup failure deleting the directory under running containers) and reviewers had to *prove* the remaining guards sufficient rather than see it.
- The per-app lock is held across all interactive prompts, which forced signal-handler cleanup (`applock.InstallSignalCleanup`) to exist at all.
- A non-interactive deploy (flags, config file, CI usage) — the most commonly requested evolution for a tool like this — cannot be added without untangling this first.

**Recommendation.** Split into two phases with a plain data struct between them:

```go
type deployPlan struct {
    App      *state.AppConfig
    EnvVars  map[string]string
    Imported []byte
    DBs      []string
    // everything the wizard decides, nothing it does
}
func collectDeployPlan(p Prompter, cfg *state.GlobalConfig) (*deployPlan, error) // no side effects, no lock
func executeDeploy(deps *App, plan *deployPlan) error                            // lock, clone, build, up, verify, save
```

Benefits: the lock is only held during `executeDeploy` (shrinking the Ctrl-C window the signal handler exists for); the cleanup logic lives next to the resources it manages; `executeDeploy` becomes directly testable without wizard scripting; and a future `deploy --from-file plan.yml` is a parser away.

**Effort:** medium. **Risk:** medium — the cleanup-path semantics must be preserved exactly (they are pinned by tests, which helps).

### 4.3 Caddyfile management: incremental text edits with a brace-counting parser

**[verified — three shipped bugs trace here]**

`AddCaddyApp` / `RemoveCaddyApp` / `setupCaddyWebhookRoute` read the current Caddyfile, strip a block via `filterCaddyDomain` (which finds a block's end by counting `{` vs `}` per line), and append a regenerated block. This design forced a chain of defensive rules: header values must not contain braces (an unbalanced one makes the *next* rewrite swallow the rest of the file), an empty domain used to match the global block and delete the ACME email, and dedupe/ordering had to be handled by hand.

The root cause is treating the Caddyfile as the source of truth to be *edited*, when the actual source of truth — `state.json` — is already available and complete.

**Recommendation.** Regenerate the entire Caddyfile from state on every change:

```go
func RenderCaddyfile(cfg *state.GlobalConfig, apps []*state.AppConfig) string
// global block + webhook route + one block per app, sorted by domain
```

`AddCaddyApp`/`RemoveCaddyApp` become "update state, render, write in place, reload". This deletes `filterCaddyDomain` and its entire bug class; determinism comes from sorting once; the redeploy path's "re-assert the route" special case disappears because every write is a full assert. The in-place `writeCaddyfile` (bind-mount contract) and the brace/placeholder *validation* on header values must both stay — `{…}` is still Caddy placeholder syntax at serve time even if the parser fragility goes away.

One design decision to make consciously: full regeneration means a Caddyfile hand-edited by an operator gets overwritten. Today's incremental editing *partially* preserves manual edits (outside managed blocks), which is undocumented and fragile; regeneration makes the "generated file — do not edit" contract honest. Document it in the header comment the way the compose generator already does.

**Effort:** small-medium. **Risk:** low; the proxy package's integration tests (bind-mount visibility, real-Caddy validation, routing chain) already pin the observable behaviour.

### 4.4 Redeploy patches the compose file instead of regenerating it

**[observed; the underlying gap is data, not code]**

`RunRedeployContext` rewrites only the app service's `image:` line (`replaceAppImage`) rather than calling `compose.Generate`. The reason is structural: **operator-supplied environment variables are not persisted in state** — they exist only in the generated `docker-compose.yml` and `.env`. Regenerating from state would silently drop them.

Consequences:

- Changes to state that *should* propagate never do: edited headers, a changed port, a database added later — none reach the compose file until the app is removed and re-deployed. `init`'s proxy-switch warning ("apps must be removed and deployed again") is a symptom of the same gap.
- `replaceAppImage` needed its own careful parser (indent tracking, sibling-service detection) — reviewers confirmed it correct for generated files but had to prove a hardening note for hand-edited ones.

**Recommendation (two steps).**

1. **Persist the operator environment in state.** Either as an encrypted blob field on `AppConfig` (consistent with `git_token`/`db_credentials`: values are secrets more often than not), or by declaring the app's `.env` file the single source of operator env (compose already loads it; generated variables would move there too, ordered after imported ones as `renderEnvFile` already does) and dropping the `environment:` block for the app service entirely. The second option is simpler and removes the `$`-escaping surface for app env at the same time — but changes precedence semantics (`environment:` currently overrides `env_file`), so it needs the env-integration test extended first.
2. **Then make redeploy call `compose.Generate`** and delete `replaceAppImage`. Every future field added to `ComposeData` propagates automatically.

**Effort:** medium (includes a small state migration for existing installs). **Risk:** medium — precedence semantics are load-bearing and pinned in CLAUDE.md; do step 1's test work first.

### 4.5 Two lock implementations share their hardest logic

**[observed]**

`state.lockStateFile` and `applock.Acquire` independently implement: O_EXCL create, token write with error checking, token-checked release, stat → re-stat-same-mtime → remove stale recovery. They differ only in path, staleness window (30 s vs 90 min), retry style (spin vs fail-fast), and the process-wide `held` registry (applock only). The subtle parts — exactly the parts that had bugs — are duplicated.

**Recommendation.** Extract a shared primitive:

```go
// internal/lockfile
type Options struct{ StaleAfter time.Duration; Retry RetryPolicy }
func Acquire(path string, o Options) (release func(), err error)
```

with `state` and `applock` as thin adapters (applock keeps its registry + signal hook). One place to reason about the TOCTOU windows, one set of tests.

**Effort:** small. **Risk:** low.

### 4.6 Configuration resolution is split across three mechanisms

**[verified — it produced a real trap this audit]**

Where things live is answered three different ways:

- `config.BaseDir` — a package-level **var**, mutated directly by tests, initialized from `SIMPLEDEPLOY_DIR` once in `config.Init()`;
- `config.HomeDataDir()` — reads `SIMPLEDEPLOY_STATE_DIR` **on every call**;
- `state.InitState(dir)` — a third, independent override used by tests, disconnected from `HomeDataDir()`.

The trap: tests isolated state via `state.InitState(t.TempDir())` while the new lock used `config.HomeDataDir()` — so tests wrote lock files into the developer's real `~/.simpledeploy` until `TestMain` grew a fourth mechanism (env override for the whole package). Every future path-dependent feature will hit the same fork.

**Recommendation.** One resolution point:

```go
type Paths struct{ Base, StateDir string }
func Resolve() Paths            // env + defaults, called once in main
```

passed down explicitly (or held by the §4.1 `App` struct). `state.InitState` becomes derived from it rather than parallel to it. Tests override one thing.

**Effort:** small-medium. **Risk:** low, but touches many call sites; do it together with §4.1.

### 4.7 Webhook provider handling is header-spelunking; make it a table

**[observed]**

`handleWebhook` distinguishes GitHub/GitLab/Gitea by probing header names inline, normalizes event names with `strings.ReplaceAll(lower, " hook", "")`, and delegates to three verifier functions with different shapes (`VerifyGitLabToken` takes `*http.Request`; the other two take body+sig). The audit showed how easy it is for provider-specific facts to hide here (bare-hex Gitea signatures; form-encoded GitHub payloads; `deleted: true`).

**Recommendation.**

```go
type provider struct {
    name      string
    detect    func(h http.Header) bool
    verify    func(h http.Header, body []byte, secret string) bool
    isPush    func(h http.Header) bool
}
var providers = []provider{github, gitlab, gitea}
```

Each provider's quirks (and the doc-verified facts the audit collected — they are currently spread across comments) live in one struct with its own focused tests. The handler shrinks to gate logic. This also makes adding Bitbucket/Forgejo a data change rather than another header-probe branch.

**Effort:** small. **Risk:** low; the webhook test suite is extensive and pins current behaviour.

### 4.8 Deploy serialization is two stacked mechanisms

**[observed]**

A webhook-triggered deploy is serialized twice: the in-process `deployLocks`/`deployPending` queue in `webhook.Server`, and the cross-process `applock` file (with `ErrHeld` retry inside `runDeploy`). Both are needed *today* — the in-memory layer provides queue-one-follow-up semantics the file lock doesn't — but the composition is subtle: reviewers had to prove the retry loop, the pending flag, and the file lock don't livelock, and the `<-done` wait on a ctx-ignoring handler can still park a goroutine for the process lifetime (documented limitation).

**Recommendation (lower priority).** Fold queueing into `applock`: `AcquireOrQueue(app) (release, queued, err)` with the pending marker as a sibling file. One mechanism, cross-process queue semantics for free (a CLI redeploy could then also honor a queued webhook push), and the server loses its two maps. Prerequisite: §4.5.

**Effort:** medium. **Risk:** medium — this is the most concurrency-sensitive code in the repo; only do it with the race suite on Linux in the loop.

### 4.9 `db.DatabaseConfig.HealthCheck` is an untyped map

**[observed]**

`HealthCheck map[string]interface{}` is populated from literals in `provisioner.go` and consumed in `cli.buildComposeData` through four type-assertion blocks with silent fallbacks. `compose.HealthCheckData` — the typed struct — already exists one layer down.

**Recommendation.** Use `*compose.HealthCheckData` (or a local typed struct) in `databaseDefs` directly; delete the assertion block. Also closes the reviewer note that `Interval`/`Timeout` are interpolated `%s`-raw: with a typed struct, add a `time.ParseDuration` validation in one place.

**Effort:** trivial. **Risk:** none.

### 4.10 Stale comments contradict the code on context cancellation

**[verified against the code]**

Comments in `redeploy.go` ("the long-running subprocess steps … do NOT honor caller ctx") and `webhook/server.go`'s `runDeploy` doc repeat that `gitPull` / `dockerBuildImage` / `dockerComposeUp` ignore the caller's context. They don't: all three take `ctx` and wrap it via `context.WithTimeout(ctx, …)` + `exec.CommandContext`, so caller cancellation kills the subprocess. What *is* true is narrower: `RunDeploy` (initial deploy) deliberately uses `context.Background()`, and a custom `SetDeployHandler` ignoring ctx would still park `<-done`.

Misleading comments about cancellation are dangerous precisely because someone will "fix" the already-working thing. Rewrite them to state the actual contract, and add a small test that cancels mid-`gitPull` and observes subprocess termination (the cancellation path currently has only a boundary-check test).

**Effort:** trivial. **Risk:** none.

### 4.11 Text emission for YAML/Caddyfile — keep it, but centralize the escaping

**[observed]**

Both generators build output with `strings.Builder`. Given the zero-dependency constraint, this is the right call (`encoding/yaml` doesn't exist in the stdlib, and importing `gopkg.in/yaml.v3` only for marshaling would trade a well-tested emitter for a dependency). But the escaping knowledge is scattered: `yamlQuote` + `escapeComposeInterpolation` in `compose`, `escapeCaddyValue` in `proxy`, and the *decision of which combination applies where* lives at each call site. The audit found one sink (`Traefik label header names`) where an escape had to be added retroactively.

**Recommendation.** A tiny internal emit helper per format — `composeString(v)`, `composeLabelValue(v)`, `caddyQuoted(v)` — so a call site can't pick half the pipeline. Pair each with the emitter-level tests that already exist (`TestGenerate_Escapes*`).

**Effort:** small. **Risk:** none.

### 4.12 CLI surface: ad-hoc flag parsing, stdout-only messaging, coarse exit codes

**[observed]**

- Flags are parsed by hand (`logs -f`, `webhook start --port` via index loops). Fine at this scale; will not survive two more flags. The stdlib `flag` package with per-command FlagSets keeps the zero-dep constraint.
- All wizard output — including `Warn` and `Fail` — goes to **stdout**. Scripts consuming `list`/`status` output get warnings interleaved; errors belong on stderr.
- Exit codes are binary (0/1). A deploy that succeeded-with-warnings (proxy route failed; container unverified) exits 0 with the nuance only in prose. Consider a distinct code for "deployed but unverified" since unattended callers exist (CI wrapping the CLI).
- `list`/`status` have no machine-readable mode; a `--json` flag on those two commands is cheap and prevents the scraping that otherwise calcifies the human format into an API.

**Effort:** small each. **Risk:** low; changing stream/exit-code behaviour is user-visible — note it in the changelog.

### 4.13 Test suite organization and cost

**[observed]**

- `internal/cli/helpers_test.go` (≈2,400 lines) and `mocks_test.go` (≈1,800 lines) mix fixtures, fakes, and tests for a dozen commands. Splitting per-command (`deploy_test.go`, `redeploy_test.go`, …) with a shared `testfixtures_test.go` would cut review time materially. Pure mechanics.
- The `-p=1` requirement is a tax paid on every run (~50 s for `cli` alone) and is a direct consequence of §4.1's globals; it can be lifted only after that refactor.
- Wall-clock note: the wizard-driven tests script stdin byte-by-byte; after §4.2, most of them can target `executeDeploy` with a plan struct and drop the scripting.
- `scripts/prove.sh` (the full evidence run: both platforms, race with cgo, gosec with the workflow's exact args, integration suite) exists but is not wired into CI. A weekly `prove` workflow — or folding the Linux-non-root leg into the existing integration workflow — would close the "green locally, red on CI" class permanently. **This is the cheapest high-value change in this report.**

### 4.14 Minor observations (grab-bag)

- `internal/docker/exec.go` defines `pullTimeout` which is unused (flagged by the IDE; `go vet` doesn't catch it). Delete or use.
- `internal/cli/deps.go`'s `discardWriter`/`failWriter` are test doubles living in a production file; move to a `_test.go`.
- `describeLockHolder` in the old `deploylock.go` era was replaced by `describeHolder` in `applock`; grep confirms no dead twin remains — but `applock.deployLockStale` naming drifted to `StaleAfter` while CLAUDE.md still references the 90-minute figure in prose only; keep the constant referenced from the doc comment.
- `wizard.MultiChoose` reports invalid entries now but still proceeds as "none" on a lone invalid single choice rather than re-prompting like `Choose` does. Harmonize when touching the wizard next.
- The Traefik path trusts container labels for routing (good — dynamic), while Caddy needs explicit blocks; after §4.3, consider extracting a `ProxyDriver` interface (`EnsureApp`, `RemoveApp`, `EnsureWebhookRoute`, `Reload`) so `cli` stops branching on `cfg.Proxy == "caddy"` at four call sites.
- `install.sh` verifies SHA-256 but not a signature; if the project ever grows a threat model beyond "trust GitHub releases", sigstore/cosign is the standard next step. Out of scope for now.

---

## 5. Known Limitations (deliberate — documented, not defects)

Carried from `CLAUDE.md`; listed so a refactor doesn't "fix" them accidentally or forget why they stand:

1. **The webhook listener binds all interfaces** and `service install` publishes the port — one proxy route serves both deployment modes. Mitigation is firewalling; the deploy trigger is HMAC-gated. A future refactor could bind to the bridge interface only when running containerised.
2. **The rate limiter cannot be a tight budget** behind the proxy (single shared bucket key, charged before verification). It is a flood guard (600/min); authorization is the HMAC. A stricter failure-only bucket provably starves valid deliveries and was rejected with a pinning test.
3. **`runDeploy`'s `<-done` can park forever** if a custom deploy handler ignores ctx. The shipped handler honors ctx at boundaries; the orphan case is additionally bounded in effect by the app file lock.
4. **TLS/ACME is untestable locally** — certificates need public DNS. The routing integration test proves everything up to the TLS redirect and says so explicitly.
5. **Windows is a development platform, not a deployment target** — `/opt/simpledeploy` semantics, signal handling, and the bind-mount contract are Linux-real. The suite runs on Windows for developer convenience; CI-parity checks run in a Linux container (`make prove`).

---

## 6. Prioritized Roadmap

| # | Item | Section | Effort | Risk | Payoff |
|---|---|---|---|---|---|
| P0 | Wire `scripts/prove.sh` (or its Linux-non-root leg) into CI | 4.13 | XS | none | permanently closes the "green locally, red on CI" class |
| P0 | Fix stale cancellation comments + add a real cancel test | 4.10 | XS | none | prevents a future "fix" of working code |
| P0 | Typed `HealthCheck`; delete assertion block; validate durations | 4.9 | XS | none | removes silent-fallback casts |
| P1 | Shared `lockfile` primitive under `state` + `applock` | 4.5 | S | low | one home for the hardest concurrency logic |
| P1 | Full-Caddyfile regeneration from state | 4.3 | S-M | low | deletes the brace-parser bug class |
| P1 | Unify path/config resolution | 4.6 | S-M | low | removes the three-mechanism trap |
| P1 | Webhook provider table | 4.7 | S | low | quirks become data with per-provider tests |
| P2 | Dependency container replacing `deps.go` globals (incremental) | 4.1 | L | low* | race-free tests, parallel suite, honest wiring |
| P2 | Split `RunDeploy` into plan/execute | 4.2 | M | med | shrinks lock window; enables non-interactive deploy |
| P2 | Persist operator env in state → regenerate compose on redeploy | 4.4 | M | med | makes state the real source of truth |
| P3 | Fold webhook queue into the file lock | 4.8 | M | med | one serialization mechanism, cross-process queueing |
| P3 | CLI polish: stdlib flags, stderr, `--json`, exit codes | 4.12 | S | low | scriptability |
| P3 | Test-file reorganization per command | 4.13 | S | none | reviewability |

\* low per-step when done command-by-command with the suite green between steps; the aggregate is large only in volume.

Sequencing note: P2's dependency container is the enabler for most of the rest to get *cheaper* (tests stop fighting globals), so if a sustained refactoring effort is planned, start there and let P1 items ride along per-package. If only opportunistic time is available, the P0/P1 rows are each independently landable in an afternoon with the existing suite as a net.

---

## 7. Invariant Checklist for Any Refactor

Before merging any structural change, re-verify (all are pinned by tests; this is the human-facing summary):

- [ ] `make prove` green: both platforms, non-root Linux, race with cgo, gosec, full integration suite.
- [ ] Generated compose: map-form env, `$$` escaping, `expose` not `ports`, sorted walks, 0600 modes.
- [ ] Caddyfile: in-place writes only; header name/value validation intact; bind-mount visibility test green.
- [ ] Deploy verification: stability window + restart counter; rollback path exercised by the E2E test.
- [ ] Webhook gates in order: method → rate → app name → body cap (413) → signature → event → parseability (400) → non-branch ref → deletion → branch match → `WebhookEnabled` → queue.
- [ ] Locks: existence checked before lock; token-checked release; state-dir placement; signal cleanup.
- [ ] State: `UpdateApp` for partial writes; temp+rename persistence; fresh-install path (`TestFreshInstall_*`).
- [ ] Zero dependencies in `go.mod`; version stamp path (`-X …cli.version`) unchanged.

---

*Report generated from the audit sessions culminating in commit `88f3add`. The bug-history context referenced throughout lives in `CLAUDE.md`; the release-facing summary of the same period lives under `[Unreleased]` in `CHANGELOG.md`.*
