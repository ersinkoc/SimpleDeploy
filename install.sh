#!/bin/sh
# SimpleDeploy install script
# Downloads the latest release binary from GitHub for the detected OS/arch.

set -e

REPO="ersinkoc/SimpleDeploy"
API_URL="https://api.github.com/repos/${REPO}/releases/latest"

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
        echo "Unsupported OS: $os"
        exit 1
        ;;
esac

# Detect architecture
arch=$(uname -m)
case "$arch" in
    x86_64|amd64)  ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *)
        echo "Unsupported architecture: $arch"
        exit 1
        ;;
esac

BINARY="simpledeploy-${OS}-${ARCH}"
DOWNLOAD_URL="https://github.com/${REPO}/releases/latest/download/${BINARY}"

echo "Detected: ${OS}/${ARCH}"
echo "Downloading ${BINARY}..."

if command -v curl >/dev/null 2>&1; then
    curl -fsSL -o simpledeploy "$DOWNLOAD_URL"
elif command -v wget >/dev/null 2>&1; then
    wget -q -O simpledeploy "$DOWNLOAD_URL"
else
    echo "curl or wget is required"
    exit 1
fi

chmod +x simpledeploy
echo "Downloaded successfully. Install with:"
echo "  sudo mv simpledeploy /usr/local/bin/"
