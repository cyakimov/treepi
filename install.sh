#!/bin/sh
# treepi installer. Downloads the latest release for your OS/arch, verifies its
# SHA-256 checksum, and installs `treepi` plus a `tp` symlink into a bin dir.
#
#   curl -fsSL https://raw.githubusercontent.com/cyakimov/treepi/main/install.sh | sh
#
# Override the destination with TREEPI_INSTALL_DIR (default /usr/local/bin).
set -eu

REPO="cyakimov/treepi"
INSTALL_DIR="${TREEPI_INSTALL_DIR:-/usr/local/bin}"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) echo "treepi: unsupported architecture: $arch" >&2; exit 1 ;;
esac
case "$os" in
	linux | darwin) ;;
	*) echo "treepi: unsupported OS: $os (use the Windows zip, Scoop, or 'go install')" >&2; exit 1 ;;
esac

tag=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" |
	grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name" *: *"([^"]+)".*/\1/')
if [ -z "$tag" ]; then
	echo "treepi: could not resolve the latest release" >&2
	exit 1
fi
version=${tag#v}
archive="treepi_${version}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/$tag"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "treepi: downloading $archive ..."
curl -fsSL "$base/$archive" -o "$tmp/$archive"
curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt"

echo "treepi: verifying checksum ..."
sumline=$(grep " $archive\$" "$tmp/checksums.txt" || true)
if [ -z "$sumline" ]; then
	echo "treepi: $archive not found in checksums.txt" >&2
	exit 1
fi
if command -v sha256sum >/dev/null 2>&1; then
	(cd "$tmp" && echo "$sumline" | sha256sum -c -) || { echo "treepi: checksum verification FAILED" >&2; exit 1; }
elif command -v shasum >/dev/null 2>&1; then
	(cd "$tmp" && echo "$sumline" | shasum -a 256 -c -) || { echo "treepi: checksum verification FAILED" >&2; exit 1; }
else
	echo "treepi: no sha256 tool found; refusing to install unverified" >&2
	exit 1
fi

tar -xzf "$tmp/$archive" -C "$tmp"
mkdir -p "$INSTALL_DIR"
install -m 0755 "$tmp/treepi" "$INSTALL_DIR/treepi"
ln -sf treepi "$INSTALL_DIR/tp"

echo "treepi: installed $tag to $INSTALL_DIR/treepi (and the tp alias)"
echo "For \`tp cd <task>\`, add to your shell rc:  eval \"\$(treepi shell-init)\""
