#!/bin/sh
# Installs a22r from GitHub Releases without needing Go.
#
#   curl -fsSL https://raw.githubusercontent.com/omerkaratas/azure-devops-batch-operator/main/install.sh | sh
#   curl -fsSL https://raw.githubusercontent.com/omerkaratas/azure-devops-batch-operator/main/install.sh | sh -s -- v1.2.3
#
# The version defaults to the latest release. Overrides: VERSION=v1.2.3, INSTALL_DIR=/path, A22R_REPO=owner/repo.
set -eu

REPO="${A22R_REPO:-theomerkaratas/azure-devops-batch-operator}"
BIN="a22r"
VERSION="${1:-${VERSION:-latest}}"

fail() { echo "error: $*" >&2; exit 1; }

case "$(uname -s)" in
  Linux)  os=linux ;;
  Darwin) os=darwin ;;
  *) fail "unsupported OS '$(uname -s)'; on Windows download the .zip from https://github.com/$REPO/releases" ;;
esac
case "$(uname -m)" in
  x86_64|amd64)  arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) fail "unsupported architecture '$(uname -m)'" ;;
esac

if command -v curl >/dev/null 2>&1; then
  download() { curl -fsSL -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
  download() { wget -q -O "$2" "$1"; }
else
  fail "curl or wget is required"
fi

asset="${BIN}_${os}_${arch}.tar.gz"
if [ "$VERSION" = "latest" ]; then
  base="https://github.com/$REPO/releases/latest/download"
else
  case "$VERSION" in v*) ;; *) VERSION="v$VERSION" ;; esac
  base="https://github.com/$REPO/releases/download/$VERSION"
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Downloading $asset ($VERSION)..."
download "$base/$asset" "$tmp/$asset" || fail "download failed: $base/$asset (does version '$VERSION' exist?)"
download "$base/checksums.txt" "$tmp/checksums.txt" || fail "could not download checksums.txt"

expected="$(grep " $asset\$" "$tmp/checksums.txt" | cut -d ' ' -f 1)"
[ -n "$expected" ] || fail "no checksum listed for $asset"
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$tmp/$asset" | cut -d ' ' -f 1)"
else
  actual="$(shasum -a 256 "$tmp/$asset" | cut -d ' ' -f 1)"
fi
[ "$expected" = "$actual" ] || fail "checksum mismatch for $asset"

tar -xzf "$tmp/$asset" -C "$tmp" "$BIN"
if tar -tzf "$tmp/$asset" | grep -q '^config.example.yml$'; then
  tar -xzf "$tmp/$asset" -C "$tmp" config.example.yml
else
  # Compatibility with releases created before the config template was bundled.
  printf '%s\n' \
    'deployment: cloud' \
    'azure_devops_url: "https://dev.azure.com/your-org"' \
    'default_token: read' \
    'tokens:' \
    '  read: ""' \
    '  read_write: ""' \
    '  manage: ""' > "$tmp/config.example.yml"
fi
if tar -tzf "$tmp/$asset" | grep -q '^config.onprem.example.yml$'; then
  tar -xzf "$tmp/$asset" -C "$tmp" config.onprem.example.yml
fi

if [ -n "${INSTALL_DIR:-}" ]; then
  dir="$INSTALL_DIR"
elif [ -w /usr/local/bin ]; then
  dir=/usr/local/bin
else
  dir="$HOME/.local/bin"
fi
mkdir -p "$dir"
install -m 0755 "$tmp/$BIN" "$dir/$BIN"

case "$os" in
  darwin|linux) config_dir="${XDG_CONFIG_HOME:-$HOME/.config}/a22r" ;;
esac
config_file="$config_dir/config.yml"
mkdir -p "$config_dir"
if [ ! -e "$config_file" ]; then
  install -m 0600 "$tmp/config.example.yml" "$config_file"
  echo "Created configuration template at $config_file"
else
  echo "Kept existing configuration at $config_file"
fi
[ -f "$tmp/config.example.yml" ] && cp -f "$tmp/config.example.yml" "$config_dir/config.example.yml"
[ -f "$tmp/config.onprem.example.yml" ] && cp -f "$tmp/config.onprem.example.yml" "$config_dir/config.onprem.example.yml"

echo "Installed $("$dir/$BIN" version) to $dir/$BIN"
case ":$PATH:" in
  *":$dir:"*) ;;
  *) echo "Note: $dir is not on your PATH; add it to run '$BIN' directly." ;;
esac
