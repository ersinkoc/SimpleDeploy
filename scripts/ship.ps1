# Verify, commit, push — and optionally tag a release.  (PowerShell)
#
#   .\scripts\ship.ps1              verify + commit + push
#   .\scripts\ship.ps1 v0.1.0       ... and tag the release
#
# Refuses to commit anything if vet or the tests fail, and prints the failing
# tests verbatim. The POSIX equivalent is scripts/ship.sh.

param([string]$Tag = "")

$ErrorActionPreference = "Continue"

function Step($msg) { Write-Host "`n==> $msg" -ForegroundColor Cyan }
function Ok($msg)   { Write-Host $msg -ForegroundColor Green }
function Bad($msg)  { Write-Host $msg -ForegroundColor Red }

Set-Location (Join-Path $PSScriptRoot "..")

Step "go vet ./..."
go vet ./...
if ($LASTEXITCODE -ne 0) {
    Bad "vet failed - nothing committed."
    exit 1
}
Ok "vet ok"

Step "go test -p=1 -count=1 ./..."
# Keep the full log so failures can be shown in full rather than summarised.
go test -p=1 -count=1 ./... 2>&1 | Tee-Object -FilePath ".test-output.log" | Out-Null
if ($LASTEXITCODE -ne 0) {
    Bad "TESTS FAILED - nothing committed. Failing tests:"
    Write-Host ""
    Select-String -Path ".test-output.log" -Pattern "--- FAIL|^FAIL|panic:|_test\.go:\d+" |
        Select-Object -First 60 |
        ForEach-Object { Write-Host $_.Line }
    Write-Host ""
    Write-Host "Full log: .test-output.log"
    exit 1
}
Remove-Item ".test-output.log" -ErrorAction SilentlyContinue
Ok "tests ok"

Step "git commit"
git add -A
git diff --cached --quiet
if ($LASTEXITCODE -eq 0) {
    Write-Host "Nothing staged; working tree already matches HEAD."
} else {
    git status --short

    $msg = @'
test: separate integration tests from the unit suite; fix Traefik dir and rule

CI has never passed in this repository. The first CI run, months ago and long
before the current work, already failed at the Test step while vet and build
passed. The cause is structural: ~40 tests drive a real Docker daemon - pulling
images, building, starting containers - and the proxy ones need exclusive use
of ports 80/443, which an unprivileged runner cannot bind at all. They were
gated only on "is Docker installed", so on a runner they all ran. The reported
"13/13 packages passing" came from local Windows runs, where every one of them
silently skipped.

Integration tests are now opt-in via SIMPLEDEPLOY_INTEGRATION=1 and run from a
separate workflow (manual dispatch plus weekly). The push pipeline runs the
unit suite, which is the part that can actually be green. A permanently red
pipeline is worse than none: it trains everyone to ignore it.

Also fixed:

  - SetupTraefik mounted ./dynamic but created that directory only afterwards,
    so Docker created the bind source itself as root. That prevented
    SetupWebhookRoute from writing the route when SimpleDeploy runs as a
    non-root user, silently disabling push-to-deploy.
  - generateServiceCompose emitted a literal \x60 instead of a backtick. The
    compose template is a raw string literal, where \x60 is not an escape
    sequence, so the Traefik router rule read Host(\x60domain\x60) and could
    never be parsed. Built with an interpreted string now.
  - Make the rate-limiter window test insensitive to CI scheduler jitter.
  - GeneratePassword computed its rejection-sampling threshold with a narrowing
    byte() conversion. Correct for the current charset, but it wraps silently
    for a one-character set and the compiler cannot check it - this is the
    gosec G115 finding that has kept the Security Scan workflow red since it
    was added. Compared as an int now. Both generators also reject
    non-positive lengths instead of panicking in make([]byte, negative).
  - Webhook server sets ReadHeaderTimeout; ReadTimeout alone does not bound a
    slowloris attack, and that listener faces the internet.
'@

    Set-Content -Path ".commit-msg.tmp" -Value $msg -Encoding UTF8
    git commit -F .commit-msg.tmp
    if ($LASTEXITCODE -ne 0) { Bad "commit failed"; exit 1 }
    Remove-Item ".commit-msg.tmp" -ErrorAction SilentlyContinue
    Ok "committed"
}

Step "git push origin main"
git push origin main
if ($LASTEXITCODE -ne 0) { Bad "push failed"; exit 1 }
Ok "pushed"

if ($Tag -ne "") {
    Step "tag $Tag"
    git tag -a $Tag -m $Tag
    git push origin $Tag
    if ($LASTEXITCODE -ne 0) { Bad "tag push failed"; exit 1 }
    Ok "tagged $Tag - the release workflow gates on vet/test/race before publishing"
}

Write-Host ""
Ok "Done."
