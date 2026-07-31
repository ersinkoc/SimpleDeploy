#!/bin/sh
# Full evidence run for SimpleDeploy.
#
# scripts/verify.sh is the fast pre-push gate. THIS script is the slow one: it
# runs every gate CI runs, on both the local platform and a Linux container
# matching CI, plus the Docker-backed suites that prove behaviour rather than
# text. It exists because a fully green local run has twice hidden a defect that
# CI would have caught:
#
#   - a lock placed under /opt/simpledeploy, which only root can create — the
#     path resolves somewhere writable on Windows, so it passed locally
#   - a leaked goroutine in a test that raced the package-level test doubles,
#     which the race detector only ever tripped on Linux
#
# Reports every failure rather than stopping at the first.
#
# Usage:  sh scripts/prove.sh
#
# Requires: Docker (for the Linux and integration stages). Stages whose
# prerequisites are missing are reported as SKIP, never silently passed.

set -u

FAILED=0
SKIPPED=0
CI_GO_VERSION=1.23
REPO=$(pwd)

step()  { printf '\n\033[36m==> %s\033[0m\n' "$1"; }
pass()  { printf '\033[32m    PASS\033[0m %s\n' "$1"; }
skip()  { printf '\033[33m    SKIP\033[0m %s (%s)\n' "$1" "$2"; SKIPPED=$((SKIPPED + 1)); }
fail()  { printf '\033[31m    FAIL\033[0m %s\n' "$1"; FAILED=$((FAILED + 1)); }

check() {
    name="$1"
    shift
    if "$@" >/tmp/prove.$$ 2>&1; then
        pass "$name"
    else
        fail "$name"
        sed 's/^/        /' /tmp/prove.$$ | tail -30
    fi
    rm -f /tmp/prove.$$
}

have_docker() { command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; }

# Git Bash on Windows rewrites arguments that look like absolute POSIX paths, so
# `docker run -w /src` arrives as `-w C:/Program Files/Git/src` and the daemon
# rejects it. These variables disable that rewriting; they are ignored on Linux
# and macOS.
export MSYS_NO_PATHCONV=1
export MSYS2_ARG_CONV_EXCL='*'

step "Toolchain"
go version || { echo "go is not installed"; exit 1; }

step "Static checks"
UNFORMATTED=$(gofmt -l . 2>/dev/null | grep -v '^$' || true)
if [ -n "$UNFORMATTED" ]; then
    fail "gofmt"
    printf '%s\n' "$UNFORMATTED" | sed 's/^/        /'
else
    pass "gofmt"
fi
check "go vet ./..." go vet ./...
check "build binary" env CGO_ENABLED=0 go build -ldflags "-s -w" -o simpledeploy .

# The `go` directive in go.mod gates LANGUAGE features but not standard-library
# APIs, so a function added after CI's Go version compiles here and breaks there.
step "CI toolchain (Go $CI_GO_VERSION)"
check "build with Go $CI_GO_VERSION" env GOTOOLCHAIN=go${CI_GO_VERSION}.0 go build ./...
check "vet with Go $CI_GO_VERSION" env GOTOOLCHAIN=go${CI_GO_VERSION}.0 go vet ./...

step "Unit suite (this platform)"
check "go test ./..." go test -p=1 -count=1 ./...
check "go test -race ./..." go test -race -p=1 -count=1 ./...

step "Unit suite (Linux, non-root, Go $CI_GO_VERSION)"
if have_docker; then
    # -u 1000:1000 matters: as root, a read-only-directory test silently passes
    # and a root-only path looks writable. CGO_ENABLED=1 matters too — without
    # cgo the race detector refuses to run and only prints a notice.
    check "linux go test ./..." docker run --rm -u 1000:1000 \
        -e HOME=/tmp -e GOCACHE=/tmp/gocache -e GOPATH=/tmp/gopath -e CGO_ENABLED=0 \
        -v "$REPO:/src" -w /src "golang:$CI_GO_VERSION" \
        sh -c "go vet ./... && go test -p=1 -count=1 ./..."
    check "linux go test -race ./..." docker run --rm -u 1000:1000 \
        -e HOME=/tmp -e GOCACHE=/tmp/gocache -e GOPATH=/tmp/gopath -e CGO_ENABLED=1 \
        -v "$REPO:/src" -w /src "golang:$CI_GO_VERSION" \
        sh -c "go test -race -p=1 -count=1 ./..."
else
    skip "Linux unit + race suite" "Docker unavailable"
fi

step "Security scan (workflow's exact arguments)"
if command -v go >/dev/null 2>&1; then
    check "gosec" go run github.com/securego/gosec/v2/cmd/gosec@v2.22.0 \
        -severity high -confidence medium -exclude=G204,G304 -quiet ./...
else
    skip "gosec" "go unavailable"
fi

# These are the suites that prove behaviour instead of text: what a container
# actually receives, whether a crash loop is really detected, whether a real
# Caddy accepts and follows the config we generate, and the whole
# deploy → push → redeploy → rollback chain.
step "Behaviour suites against a live Docker daemon"
if have_docker; then
    check "integration suite" env SIMPLEDEPLOY_INTEGRATION=1 go test -p=1 -count=1 -timeout 30m ./...
else
    skip "integration suite" "Docker unavailable"
fi

step "Binary smoke test"
check "version" ./simpledeploy version
check "help" ./simpledeploy help
if ./simpledeploy definitely-not-a-command >/dev/null 2>&1; then
    fail "unknown command should exit non-zero"
else
    pass "unknown command exits non-zero"
fi
# Regression guard: with stdin closed the wizard prompts used to spin forever.
if command -v timeout >/dev/null 2>&1; then
    timeout 15 ./simpledeploy deploy </dev/null >/dev/null 2>&1
    if [ "$?" -eq 124 ]; then
        fail "deploy hung on closed stdin (wizard EOF regression)"
    else
        pass "deploy terminates on closed stdin"
    fi
else
    skip "closed-stdin guard" "timeout(1) unavailable"
fi

printf '\n'
if [ "$FAILED" -eq 0 ] && [ "$SKIPPED" -eq 0 ]; then
    printf '\033[32mEvery gate passed.\033[0m\n'
    exit 0
fi
if [ "$FAILED" -eq 0 ]; then
    printf '\033[33mAll executed gates passed, but %d were skipped — the run is incomplete.\033[0m\n' "$SKIPPED"
    exit 0
fi
printf '\033[31m%d gate(s) failed.\033[0m\n' "$FAILED"
exit 1
