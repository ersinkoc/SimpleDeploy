#!/bin/sh
# Full local verification for SimpleDeploy.
#
# Runs the same checks CI does (vet, build, test, race) plus a smoke test of
# the built binary, and reports every failure rather than stopping at the first
# one — so a single run tells you everything that is wrong.
#
# Usage:  sh scripts/verify.sh

set -u

FAILED=0

step() {
    printf '\n\033[36m==> %s\033[0m\n' "$1"
}

check() {
    name="$1"
    shift
    if "$@"; then
        printf '\033[32m    PASS\033[0m %s\n' "$name"
    else
        printf '\033[31m    FAIL\033[0m %s\n' "$name"
        FAILED=$((FAILED + 1))
    fi
}

step "Go toolchain"
go version || { echo "go is not installed"; exit 1; }

step "Formatting"
# Advisory only. gofmt differences are cosmetic — they do not affect vet,
# build, or tests — so they are reported without failing the run. Fix with
# `make fmt`.
UNFORMATTED=$(gofmt -l . 2>/dev/null | grep -v '^$' || true)
if [ -n "$UNFORMATTED" ]; then
    printf '\033[33m    NOTE\033[0m files not gofmt-clean (cosmetic, run `make fmt`):\n%s\n' "$UNFORMATTED"
else
    printf '\033[32m    PASS\033[0m all files formatted\n'
fi

step "Vet"
check "go vet ./..." go vet ./...

step "Build"
check "compile all packages" go build ./...
check "build binary" env CGO_ENABLED=0 go build -ldflags "-s -w" -o simpledeploy .

step "Tests"
check "go test ./..." go test -p=1 -count=1 ./...

step "Race detector"
check "go test -race ./..." go test -race -p=1 -count=1 ./...

step "Binary smoke test"
# The CLI must respond to these without a config file present.
check "version" ./simpledeploy version
check "help" ./simpledeploy help
# An unknown command must exit non-zero.
if ./simpledeploy definitely-not-a-command >/dev/null 2>&1; then
    printf '\033[31m    FAIL\033[0m unknown command should exit non-zero\n'
    FAILED=$((FAILED + 1))
else
    printf '\033[32m    PASS\033[0m unknown command exits non-zero\n'
fi
# Commands needing init must fail cleanly, not panic.
if ./simpledeploy status >/dev/null 2>&1; then
    printf '\033[33m    NOTE\033[0m status succeeded (this machine is already initialized)\n'
else
    printf '\033[32m    PASS\033[0m status fails cleanly without init\n'
fi

step "Non-interactive safety"
# Regression guard: with stdin closed the wizard prompts used to spin forever.
# A hang here means the EOF handling in internal/wizard regressed.
if command -v timeout >/dev/null 2>&1; then
    timeout 15 ./simpledeploy deploy </dev/null >/dev/null 2>&1
    rc=$?
    if [ "$rc" -eq 124 ]; then
        printf '\033[31m    FAIL\033[0m deploy hung on closed stdin (wizard EOF regression)\n'
        FAILED=$((FAILED + 1))
    else
        printf '\033[32m    PASS\033[0m deploy terminates on closed stdin\n'
    fi
else
    printf '\033[33m    SKIP\033[0m timeout(1) not available\n'
fi

printf '\n'
if [ "$FAILED" -eq 0 ]; then
    printf '\033[32mAll checks passed.\033[0m\n'
    exit 0
fi
printf '\033[31m%d check(s) failed.\033[0m\n' "$FAILED"
exit 1
