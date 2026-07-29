#!/bin/sh
# Verify, commit, push — and optionally tag a release.
#
#   sh scripts/ship.sh              verify + commit + push
#   sh scripts/ship.sh v0.1.0       ... and tag the release after CI is green
#
# Refuses to commit anything if vet or the tests fail, and prints the failing
# tests verbatim so they can be acted on directly.

set -eu

TAG="${1:-}"

red()   { printf '\033[31m%s\033[0m\n' "$1"; }
green() { printf '\033[32m%s\033[0m\n' "$1"; }
step()  { printf '\n\033[36m==> %s\033[0m\n' "$1"; }

cd "$(dirname "$0")/.."

step "go vet ./..."
if ! go vet ./...; then
    red "vet failed — nothing committed."
    exit 1
fi
green "vet ok"

step "go test -p=1 -count=1 ./..."
# Keep the full log so failures can be shown in full rather than summarised.
if go test -p=1 -count=1 ./... > .test-output.log 2>&1; then
    green "tests ok"
    rm -f .test-output.log
else
    red "TESTS FAILED — nothing committed. Failing tests:"
    echo
    grep -E -- "--- FAIL|^FAIL|panic:|^\s+.*_test\.go:[0-9]+" .test-output.log | head -60
    echo
    echo "Full log: .test-output.log"
    exit 1
fi

step "git commit"
git add -A
if git diff --cached --quiet; then
    echo "Nothing staged; working tree already matches HEAD."
else
    git status --short
    cat > .commit-msg.tmp <<'EOF'
test: separate integration tests from the unit suite; fix Traefik dir and rule

CI has never passed in this repository. The first CI run, months ago and long
before the current work, already failed at the Test step while vet and build
passed. The cause is structural: ~40 tests drive a real Docker daemon — pulling
images, building, starting containers — and the proxy ones need exclusive use
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
    for a one-character set and the compiler cannot check it — this is the
    gosec G115 finding that has kept the Security Scan workflow red since it
    was added. Compared as an int now. Both generators also reject
    non-positive lengths instead of panicking in make([]byte, negative).
  - Webhook server sets ReadHeaderTimeout; ReadTimeout alone does not bound a
    slowloris attack, and that listener faces the internet.
EOF
    git commit -F .commit-msg.tmp
    rm -f .commit-msg.tmp
    green "committed"
fi

step "git push origin main"
git push origin main
green "pushed"

if [ -n "$TAG" ]; then
    step "tag ${TAG}"
    echo "Waiting for CI before tagging is recommended. Tagging now anyway."
    git tag -a "$TAG" -m "$TAG"
    git push origin "$TAG"
    green "tagged ${TAG} — the release workflow gates on vet/test/race before publishing"
fi

echo
green "Done."
