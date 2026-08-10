#!/bin/sh

set -eu

repository="${MIMIR_GITHUB_REPOSITORY:-cloudboy-jh/mimir}"
releases_url="${MIMIR_RELEASES_URL:-https://github.com/$repository/releases}"
version="${MIMIR_VERSION:-}"
install_dir="${MIMIR_INSTALL_DIR:-}"

usage() {
  printf '%s\n' 'usage: install.sh [--version VERSION] [--bin-dir DIR]' >&2
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --version)
      [ "$#" -ge 2 ] || { usage; exit 2; }
      version=$2
      shift 2
      ;;
    --version=*)
      version=${1#--version=}
      shift
      ;;
    --bin-dir)
      [ "$#" -ge 2 ] || { usage; exit 2; }
      install_dir=$2
      shift 2
      ;;
    --bin-dir=*)
      install_dir=${1#--bin-dir=}
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      usage
      exit 2
      ;;
  esac
done

require_command() {
  command -v "$1" >/dev/null 2>&1 || {
    printf 'mimir install: required command not found: %s\n' "$1" >&2
    exit 1
  }
}

require_command curl
require_command tar
require_command awk
require_command mktemp
require_command tr
require_command uname

case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *)
    printf 'mimir install: unsupported operating system: %s\n' "$(uname -s)" >&2
    exit 1
    ;;
esac

case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *)
    printf 'mimir install: unsupported architecture: %s\n' "$(uname -m)" >&2
    exit 1
    ;;
esac

if [ -z "$version" ]; then
  latest_url=$(curl -fsSL -o /dev/null -w '%{url_effective}' "$releases_url/latest") || {
    printf '%s\n' 'mimir install: could not resolve the latest stable release' >&2
    exit 1
  }
  tag=${latest_url%%\?*}
  tag=${tag%/}
  tag=${tag##*/}
else
  tag=$version
fi

case "$tag" in
  v*) asset_version=${tag#v} ;;
  *) asset_version=$tag; tag="v$tag" ;;
esac
[ -n "$asset_version" ] || {
  printf 'mimir install: invalid release version: %s\n' "$tag" >&2
  exit 1
}

archive="mimir_${asset_version}_${os}_${arch}.tar.gz"
download_base="$releases_url/download/$tag"
umask 077
temp_root=$(mktemp -d "${TMPDIR:-/tmp}/mimir-install.XXXXXX") || {
  printf '%s\n' 'mimir install: could not create a private temporary directory' >&2
  exit 1
}
archive_path="$temp_root/$archive"
checksums_path="$temp_root/checksums.txt"
extract_root="$temp_root/extract"

mkdir -p "$extract_root"
cleanup() {
  rm -rf "$temp_root"
}
trap cleanup EXIT HUP INT TERM

curl -fsSL "$download_base/$archive" -o "$archive_path" || {
  printf 'mimir install: release asset not found: %s\n' "$archive" >&2
  exit 1
}
curl -fsSL "$download_base/checksums.txt" -o "$checksums_path" || {
  printf '%s\n' 'mimir install: checksums.txt was not found for the release' >&2
  exit 1
}

expected=$(awk -v name="$archive" '$2 == name || $2 == "*" name { print $1; exit }' "$checksums_path" | tr 'A-F' 'a-f')
[ -n "$expected" ] || {
  printf 'mimir install: checksum entry not found for %s\n' "$archive" >&2
  exit 1
}

if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$archive_path" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
  actual=$(shasum -a 256 "$archive_path" | awk '{print $1}')
else
  printf '%s\n' 'mimir install: sha256sum or shasum is required' >&2
  exit 1
fi

if [ "$actual" != "$expected" ]; then
  printf 'mimir install: checksum mismatch for %s\n' "$archive" >&2
  exit 1
fi

tar -xzf "$archive_path" -C "$extract_root" || {
  printf 'mimir install: could not extract %s\n' "$archive" >&2
  exit 1
}
binary="$extract_root/mimir_${asset_version}_${os}_${arch}/mimir"
[ -f "$binary" ] || {
  printf '%s\n' 'mimir install: archive did not contain the expected Mimir binary' >&2
  exit 1
}
chmod 755 "$binary"

if [ -z "$install_dir" ]; then
  [ -n "${HOME:-}" ] || {
    printf '%s\n' 'mimir install: HOME is not set; pass --bin-dir' >&2
    exit 1
  }
  existing=$(command -v mimir 2>/dev/null || true)
  case "$existing" in
    /*) install_dir=${existing%/*} ;;
  esac
fi

if [ -z "$install_dir" ]; then
  old_ifs=$IFS
  IFS=:
  for candidate in ${PATH:-}; do
    case "$candidate" in
      "$HOME"|"$HOME"/*)
        if [ -d "$candidate" ] && [ -w "$candidate" ]; then
          install_dir=$candidate
          break
        fi
        ;;
    esac
  done
  IFS=$old_ifs
fi

if [ -z "$install_dir" ]; then
  install_dir="$HOME/.local/bin"
fi

case "$install_dir" in
  /*) ;;
  *) install_dir="$(pwd)/$install_dir" ;;
esac

MIMIR_INSTALL_SOURCE=release "$binary" install --bin-dir "$install_dir"

case ":${PATH:-}:" in
  *:"$install_dir":*) ;;
  *)
    printf '\nMimir was installed outside PATH. Run:\n  export PATH="%s:$PATH"\n' "$install_dir"
    ;;
esac
