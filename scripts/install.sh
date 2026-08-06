#!/bin/sh
# rf-clipboard client installer: curl -fsSL https://raw.githubusercontent.com/RunFridge/rf-clipboard/main/scripts/install.sh | sh
# Override install locations with BINDIR / MANDIR.
set -eu

REPO="RunFridge/rf-clipboard"

# Termux exports PREFIX for its own filesystem layout, so we must not use
# PREFIX as our override name; its PATH expects binaries in $PREFIX/bin.
if [ -n "${TERMUX_VERSION:-}" ]; then
  BINDIR="${BINDIR:-$PREFIX/bin}"
  MANDIR="${MANDIR:-$PREFIX/share/man/man1}"
else
  BINDIR="${BINDIR:-$HOME/.local/bin}"
  MANDIR="${MANDIR:-${XDG_DATA_HOME:-$HOME/.local/share}/man/man1}"
fi

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
# Termux reports Linux but needs the Bionic-targeted android binary
[ -n "${TERMUX_VERSION:-}" ] && os=android
case "$os" in
  linux | darwin | android) ;;
  *) echo "unsupported OS: $os" >&2; exit 1 ;;
esac
case "$arch" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) echo "unsupported architecture: $arch" >&2; exit 1 ;;
esac

url="https://github.com/$REPO/releases/latest/download/rf-clip_${os}_${arch}"
mkdir -p "$BINDIR"
echo "downloading $url"
curl -fsSL "$url" -o "$BINDIR/rf-clip"
chmod +x "$BINDIR/rf-clip"
ln -sf "$BINDIR/rf-clip" "$BINDIR/rf-copy"
ln -sf "$BINDIR/rf-clip" "$BINDIR/rf-paste"

# man pages — ~/.local/share/man is on the manpath whenever ~/.local/bin is on PATH
mkdir -p "$MANDIR"
for page in rf-clip.1 rf-copy.1 rf-paste.1; do
  curl -fsSL "https://raw.githubusercontent.com/$REPO/main/docs/man/$page" -o "$MANDIR/$page"
done

echo "installed rf-clip (with rf-copy and rf-paste symlinks) to $BINDIR"
case ":$PATH:" in
  *":$BINDIR:"*) ;;
  *) echo "note: $BINDIR is not on your PATH" >&2 ;;
esac
echo "next: rf-clip init"
