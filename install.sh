#!/bin/sh
set -e

REPO="codingconcepts/next-slide-please"
INSTALL_DIR="/usr/local/bin"

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
  arm64)   ARCH="arm64" ;;
  *)       echo "Unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

case "$OS" in
  linux|darwin) ;;
  mingw*|msys*|cygwin*) OS="windows" ;;
  *) echo "Unsupported OS: $OS" >&2; exit 1 ;;
esac

if [ -n "$1" ]; then
  VERSION="$1"
  case "$VERSION" in
    v[0-9]*) ;;
    *) echo "Invalid version format: $VERSION (expected vX.Y.Z)" >&2; exit 1 ;;
  esac
else
  VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | cut -d'"' -f4)
fi

if [ -z "$VERSION" ]; then
  echo "Failed to determine latest version" >&2
  exit 1
fi

BINARY="nsp-${OS}-${ARCH}"
if [ "$OS" = "windows" ]; then
  BINARY="${BINARY}.exe"
fi

URL="https://github.com/${REPO}/releases/download/${VERSION}/${BINARY}"

TMPFILE=$(mktemp)
trap 'rm -f "$TMPFILE"' EXIT

echo "Downloading nsp ${VERSION} (${OS}/${ARCH})..."
curl -fsSL -o "$TMPFILE" "$URL"
chmod +x "$TMPFILE"

if [ -w "$INSTALL_DIR" ]; then
  mv "$TMPFILE" "${INSTALL_DIR}/nsp"
else
  echo "Installing to ${INSTALL_DIR} (requires sudo)..."
  sudo mv "$TMPFILE" "${INSTALL_DIR}/nsp"
fi

echo "nsp ${VERSION} installed to ${INSTALL_DIR}/nsp"

# Speaking works everywhere; presenting needs CoreGraphics, so say so now
# rather than leaving someone to discover it mid-talk.
if [ "$OS" != "darwin" ]; then
  echo
  echo "Note: 'nsp speak' works on ${OS}, but 'nsp present' is macOS only for now."
  echo "      Use 'nsp present --dry-run' to try everything except the keypress."
fi
