#!/bin/sh
set -e

REPO="abbayosua/skynet"

usage() {
	cat <<EOF
Usage: install.sh [--version <tag>] [--dir <path>]

Install skynet from GitHub releases.

Options:
  --version <tag>  Install a specific version (default: latest)
  --dir <path>     Install directory (default: /usr/local/bin)
  --help           Show this help
EOF
	exit 0
}

VERSION="latest"
INSTALL_DIR="/usr/local/bin"

while [ $# -gt 0 ]; do
	case "$1" in
	--version)
		VERSION="$2"
		shift 2
		;;
	--dir)
		INSTALL_DIR="$2"
		shift 2
		;;
	--help)
		usage
		;;
	*)
		echo "Unknown option: $1"
		usage
		;;
	esac
done

# Detect OS and arch
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
x86_64 | amd64)
	ARCH="amd64"
	;;
aarch64 | arm64)
	ARCH="arm64"
	;;
i386 | i686)
	ARCH="386"
	;;
*)
	echo "Unsupported architecture: $ARCH"
	exit 1
	;;
esac

case "$OS" in
linux) ;;
darwin) ;;
freebsd) OS="freebsd" ;;
openbsd) OS="openbsd" ;;
netbsd) OS="netbsd" ;;
*)
	echo "Unsupported OS: $OS"
	exit 1
	;;
esac

# Resolve latest version
if [ "$VERSION" = "latest" ]; then
	echo "Fetching latest release..."
	VERSION=$(curl -sSfL "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name"' | cut -d'"' -f4)
	if [ -z "$VERSION" ]; then
		echo "Failed to fetch latest version"
		exit 1
	fi
fi

# Construct filenames (matching release.yml naming)
ARCHIVE="skynet_${VERSION}_${OS}_${ARCH}.tar.gz"
DOWNLOAD_URL="https://github.com/$REPO/releases/download/$VERSION/$ARCHIVE"

echo "Downloading skynet $VERSION for ${OS}/${ARCH}..."
echo "  $DOWNLOAD_URL"

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

curl -sSfL "$DOWNLOAD_URL" -o "$TMPDIR/$ARCHIVE"

echo "Extracting..."
tar -xzf "$TMPDIR/$ARCHIVE" -C "$TMPDIR"

# Find the binary (may be in a subdirectory)
BIN=$(find "$TMPDIR" -name "skynet" -type f 2>/dev/null | head -1)
if [ -z "$BIN" ]; then
	echo "Binary not found in archive"
	exit 1
fi

echo "Installing to $INSTALL_DIR/skynet..."
if [ "$(id -u)" -ne 0 ]; then
	echo "Requesting sudo to install..."
	sudo cp "$BIN" "$INSTALL_DIR/skynet"
	sudo chmod 755 "$INSTALL_DIR/skynet"
else
	cp "$BIN" "$INSTALL_DIR/skynet"
	chmod 755 "$INSTALL_DIR/skynet"
fi

echo ""
echo "Installed skynet $VERSION!"
echo "Run 'skynet --help' to get started."
