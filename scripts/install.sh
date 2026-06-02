#!/usr/bin/env sh
set -eu

REPO="${REPO:-cosmtrek/jeju}"
VERSION="${VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "error: $1 is required" >&2
    exit 1
  fi
}

need curl
need tar

resolve_latest_tag() {
  tag="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n 1 || true)"
  if [ -n "$tag" ]; then
    printf '%s\n' "$tag"
    return
  fi

  tag="$(curl -fsSL "https://github.com/$REPO/releases.atom" 2>/dev/null | sed -n "s#.*href=\"https://github.com/$REPO/releases/tag/\\([^\"]*\\)\".*#\\1#p" | head -n 1 || true)"
  if [ -n "$tag" ]; then
    printf '%s\n' "$tag"
    return
  fi

  latest_url="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest" 2>/dev/null || true)"
  printf '%s\n' "$latest_url" | sed -n 's#.*/releases/tag/\([^/?#]*\).*#\1#p'
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
    return
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
    return
  fi
  echo "error: sha256sum or shasum is required" >&2
  exit 1
}

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"

case "$os" in
  darwin|linux) ;;
  *)
    echo "error: unsupported OS: $os" >&2
    exit 1
    ;;
esac

case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *)
    echo "error: unsupported architecture: $arch" >&2
    exit 1
    ;;
esac

if [ "$VERSION" = "latest" ]; then
  tag="$(resolve_latest_tag)"
else
  tag="$VERSION"
fi

if [ -z "$tag" ]; then
  echo "error: could not resolve release version" >&2
  exit 1
fi

archive="jeju_${tag#v}_${os}_${arch}.tar.gz"
base_url="https://github.com/$REPO/releases/download/$tag"
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

curl -fsSL "$base_url/$archive" -o "$tmpdir/$archive"
curl -fsSL "$base_url/checksums.txt" -o "$tmpdir/checksums.txt"

(
  cd "$tmpdir"
  expected="$(awk -v file="$archive" '{ name=$2; sub(/^\*/, "", name); if (name == file) { print $1; found=1 } } END { if (!found) exit 1 }' checksums.txt)"
  actual="$(sha256_file "$archive")"
  if [ "$expected" != "$actual" ]; then
    echo "error: checksum mismatch for $archive" >&2
    exit 1
  fi
  tar -xzf "$archive"
)

mkdir -p "$INSTALL_DIR"
mv "$tmpdir/jeju" "$INSTALL_DIR/jeju"
chmod +x "$INSTALL_DIR/jeju"

echo "installed jeju $tag to $INSTALL_DIR/jeju"
echo "run: jeju version"
