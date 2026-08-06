#!/bin/sh
# rf-clipboard client installer: curl -fsSL https://raw.githubusercontent.com/RunFridge/rf-clipboard/main/scripts/install.sh | sh
set -eu

REPO="RunFridge/rf-clipboard"
PREFIX="${PREFIX:-$HOME/.local/bin}"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$os" in
  linux | darwin) ;;
  *) echo "unsupported OS: $os" >&2; exit 1 ;;
esac
case "$arch" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) echo "unsupported architecture: $arch" >&2; exit 1 ;;
esac

url="https://github.com/$REPO/releases/latest/download/rf-clip_${os}_${arch}"
mkdir -p "$PREFIX"
echo "downloading $url"
curl -fsSL "$url" -o "$PREFIX/rf-clip"
chmod +x "$PREFIX/rf-clip"
ln -sf "$PREFIX/rf-clip" "$PREFIX/rf-copy"
ln -sf "$PREFIX/rf-clip" "$PREFIX/rf-paste"

# man pages — ~/.local/share/man is on the manpath whenever ~/.local/bin is on PATH
MANDIR="${MANDIR:-${XDG_DATA_HOME:-$HOME/.local/share}/man/man1}"
mkdir -p "$MANDIR"
for page in rf-clip.1 rf-copy.1 rf-paste.1; do
  curl -fsSL "https://raw.githubusercontent.com/$REPO/main/docs/man/$page" -o "$MANDIR/$page"
done

echo "installed rf-clip (with rf-copy and rf-paste symlinks) to $PREFIX"
case ":$PATH:" in
  *":$PREFIX:"*) ;;
  *) echo "note: $PREFIX is not on your PATH" >&2 ;;
esac
echo "next: rf-clip init"
