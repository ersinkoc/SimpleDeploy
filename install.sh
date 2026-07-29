#!/bin/sh
# SimpleDeploy install script
# Downloads the latest release binary from GitHub for the detected OS/arch and
# verifies its SHA-256 checksum against the published SHA256SUMS.

set -eu

REPO="ersinkoc/SimpleDeploy"
BASE_URL="https://github.com/${REPO}/releases/latest/download"

fail() {
    echo "error: $*" >&2
    exit 1
}

# Detect OS
os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
    linux)   OS="linux" ;;
    darwin)  OS="darwin" ;;
    mingw*|msys*|cygwin*)
        echo "Windows detected. Please download the .exe from:"
        echo "  https://github.com/${REPO}/releases/latest"
        exit 0
        ;;
    *)
        fail "unsupported OS: $os"
        ;;
esac

# Detect architecture
arch=$(uname -m)
case "$arch" in
    x86_64|amd64)  ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *)
        fail "unsupported architecture: $arch"
        ;;
esac

BINARY="simpledeploy-${OS}-${ARCH}"

# Pick a downloader once and reuse it. -f makes curl fail on 404 instead of
# writing an HTML error page to the output file and pretending it succeeded.
if command -v curl >/dev/null 2>&1; then
    fetch() { curl -fsSL -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
    fetch() { wget -q -O "$2" "$1"; }
else
    fail "curl or wget is required"
fi

# Stage in a temp directory so a failed or unverified download never leaves a
# partial binary named `simpledeploy` sitting in the current directory.
TMPDIR_INSTALL=$(mktemp -d)
cleanup() { rm -rf "$TMPDIR_INSTALL"; }
trap cleanup EXIT INT TERM

echo "Detected: ${OS}/${ARCH}"
echo "Downloading ${BINARY}..."
fetch "${BASE_URL}/${BINARY}" "${TMPDIR_INSTALL}/${BINARY}" \
    || fail "download failed — check that a release exists at https://github.com/${REPO}/releases/latest"

# Verify against the published checksums. Releases from v0.1.0 onward include
# SHA256SUMS; if it is absent (older release) we say so rather than silently
# skipping the check.
if fetch "${BASE_URL}/SHA256SUMS" "${TMPDIR_INSTALL}/SHA256SUMS" 2>/dev/null; then
    if command -v sha256sum >/dev/null 2>&1; then
        HASHER="sha256sum"
        ACTUAL=$(sha256sum "${TMPDIR_INSTALL}/${BINARY}" | awk '{print $1}')
    elif command -v shasum >/dev/null 2>&1; then
        HASHER="shasum -a 256"
        ACTUAL=$(shasum -a 256 "${TMPDIR_INSTALL}/${BINARY}" | awk '{print $1}')
    else
        HASHER=""
    fi

    if [ -n "$HASHER" ]; then
        EXPECTED=$(awk -v f="$BINARY" '$2 == f || $2 == "*"f {print $1}' "${TMPDIR_INSTALL}/SHA256SUMS")
        [ -n "$EXPECTED" ] || fail "no checksum listed for ${BINARY} in SHA256SUMS"
        if [ "$ACTUAL" != "$EXPECTED" ]; then
            fail "checksum mismatch for ${BINARY}
  expected: ${EXPECTED}
  actual:   ${ACTUAL}
The download may be corrupt or tampered with. Not installing."
        fi
        echo "Checksum verified."
    else
        echo "warning: no sha256sum/shasum available; skipping checksum verification" >&2
    fi
else
    echo "warning: SHA256SUMS not published for this release; skipping verification" >&2
fi

mv "${TMPDIR_INSTALL}/${BINARY}" ./simpledeploy
chmod +x ./simpledeploy

echo
echo "Downloaded to ./simpledeploy"
echo "Install it with:"
echo "  sudo mv simpledeploy /usr/local/bin/"
echo
echo "Then run:"
echo "  simpledeploy init"
