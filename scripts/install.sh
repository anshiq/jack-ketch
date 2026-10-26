#!/usr/bin/env bash
set -euo pipefail

REPO="anshiq/jack-ketch"
BINARIES=(jack-ketch jackketchd jack-ketch-shim)
VERSION="${VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"

usage() {
  cat <<'EOF'
Install jack-ketch from GitHub releases.

Usage: install.sh [options]

Options:
  -v, --version <version>  Release to install (default: latest)
  -d, --dir <path>         Install directory (default: ~/.local/bin)
  -h, --help               Show this help

Environment: VERSION, INSTALL_DIR
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    -v|--version)
      [ "$#" -ge 2 ] || { echo "error: $1 needs a value" >&2; exit 2; }
      VERSION="$2"
      shift 2
      ;;
    -d|--dir)
      [ "$#" -ge 2 ] || { echo "error: $1 needs a value" >&2; exit 2; }
      INSTALL_DIR="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "error: unknown option: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

command -v curl >/dev/null 2>&1 || { echo "error: curl is required" >&2; exit 1; }
command -v tar >/dev/null 2>&1 || { echo "error: tar is required" >&2; exit 1; }

os="$(uname -s)"
case "$os" in
  Linux) os="linux" ;;
  Darwin) os="darwin" ;;
  *) echo "error: unsupported OS: $os" >&2; exit 1 ;;
esac

arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) echo "error: unsupported architecture: $arch" >&2; exit 1 ;;
esac

if [ "$VERSION" = "latest" ]; then
  VERSION="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest")"
  VERSION="${VERSION##*/}"
fi
VERSION="${VERSION#v}"
[ -n "$VERSION" ] || { echo "error: could not resolve the latest release" >&2; exit 1; }

asset="jack-ketch_${VERSION}_${os}_${arch}.tar.gz"
base="https://github.com/${REPO}/releases/download/v${VERSION}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Downloading ${asset}..."
curl -fsSL "${base}/${asset}" -o "${tmp}/${asset}"
curl -fsSL "${base}/checksums.txt" -o "${tmp}/checksums.txt"

expected="$(awk -v name="$asset" '$2 == name { print $1; exit }' "$tmp/checksums.txt")"
[ -n "$expected" ] || { echo "error: checksum for ${asset} not found" >&2; exit 1; }

if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$tmp/${asset}" | awk '{print $1}')"
else
  actual="$(shasum -a 256 "$tmp/${asset}" | awk '{print $1}')"
fi
[ "$actual" = "$expected" ] || { echo "error: checksum mismatch for ${asset}" >&2; exit 1; }

tar -xzf "${tmp}/${asset}" -C "$tmp"

if ! mkdir -p "$INSTALL_DIR" 2>/dev/null; then
  command -v sudo >/dev/null 2>&1 || {
    echo "error: cannot create ${INSTALL_DIR}; set INSTALL_DIR to a writable directory" >&2
    exit 1
  }
  sudo mkdir -p "$INSTALL_DIR"
fi

if [ -w "$INSTALL_DIR" ]; then
  run=(install -m 0755)
elif command -v sudo >/dev/null 2>&1; then
  run=(sudo install -m 0755)
else
  echo "error: ${INSTALL_DIR} is not writable; set INSTALL_DIR to a writable directory" >&2
  exit 1
fi

for bin in "${BINARIES[@]}"; do
  "${run[@]}" "${tmp}/${bin}" "${INSTALL_DIR}/${bin}"
done

echo "Installed jack-ketch ${VERSION} to ${INSTALL_DIR}"
case ":${PATH}:" in
  *":${INSTALL_DIR}:"*) ;;
  *)
    echo "Add ${INSTALL_DIR} to your PATH:"
    echo "  export PATH=\"${INSTALL_DIR}:\$PATH\""
    ;;
esac
"${INSTALL_DIR}/jack-ketch" --version
