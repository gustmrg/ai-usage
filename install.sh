#!/bin/sh

set -eu

repo="gustmrg/ai-usage"
version="${AI_USAGE_VERSION:-}"
install_dir="${AI_USAGE_INSTALL_DIR:-${HOME}/.local/bin}"

usage() {
    cat <<'EOF'
Install ai-usage from GitHub Releases.

Usage: install.sh [options]

Options:
  --version VERSION    Install a specific version, such as v0.1.0
  --install-dir PATH   Install into PATH (default: ~/.local/bin)
  -h, --help           Show this help

Environment variables:
  AI_USAGE_VERSION
  AI_USAGE_INSTALL_DIR
EOF
}

die() {
    printf 'ai-usage installer: %s\n' "$*" >&2
    exit 1
}

while [ "$#" -gt 0 ]; do
    case "$1" in
        --version)
            [ "$#" -ge 2 ] || die "--version requires a value"
            version=$2
            shift 2
            ;;
        --install-dir)
            [ "$#" -ge 2 ] || die "--install-dir requires a value"
            install_dir=$2
            shift 2
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            die "unknown option: $1"
            ;;
    esac
done

case "$(uname -s)" in
    Darwin) os=darwin ;;
    Linux) os=linux ;;
    *) die "unsupported operating system: $(uname -s)" ;;
esac

case "$(uname -m)" in
    x86_64|amd64) arch=amd64 ;;
    arm64|aarch64) arch=arm64 ;;
    *) die "unsupported architecture: $(uname -m)" ;;
esac

if command -v curl >/dev/null 2>&1; then
    download_file() {
        curl --fail --silent --show-error --location --output "$2" "$1"
    }
    if [ -z "$version" ]; then
        latest_url=$(curl --fail --silent --show-error --location \
            --output /dev/null --write-out '%{url_effective}' \
            "https://github.com/${repo}/releases/latest")
        version=${latest_url##*/}
    fi
elif command -v wget >/dev/null 2>&1; then
    download_file() {
        wget --quiet --output-document="$2" "$1"
    }
    if [ -z "$version" ]; then
        version=$(wget --quiet --output-document=- \
            "https://api.github.com/repos/${repo}/releases/latest" |
            sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' |
            sed -n '1p')
    fi
else
    die "curl or wget is required"
fi

[ -n "$version" ] || die "could not resolve the latest release"
case "$version" in
    v*) tag=$version; release_version=${version#v} ;;
    *) tag="v${version}"; release_version=$version ;;
esac

asset="ai-usage_${release_version}_${os}_${arch}.tar.gz"
base_url="https://github.com/${repo}/releases/download/${tag}"

tmp_dir=$(mktemp -d 2>/dev/null || mktemp -d -t ai-usage)
trap 'rm -rf "$tmp_dir"' EXIT HUP INT TERM

printf 'Downloading ai-usage %s for %s/%s...\n' "$tag" "$os" "$arch"
download_file "${base_url}/${asset}" "${tmp_dir}/${asset}"
download_file "${base_url}/checksums.txt" "${tmp_dir}/checksums.txt"

expected=$(awk -v name="$asset" '$2 == name { print $1; exit }' "${tmp_dir}/checksums.txt")
[ -n "$expected" ] || die "${asset} is missing from checksums.txt"

if command -v sha256sum >/dev/null 2>&1; then
    actual=$(sha256sum "${tmp_dir}/${asset}" | awk '{ print $1 }')
elif command -v shasum >/dev/null 2>&1; then
    actual=$(shasum -a 256 "${tmp_dir}/${asset}" | awk '{ print $1 }')
elif command -v openssl >/dev/null 2>&1; then
    actual=$(openssl dgst -sha256 "${tmp_dir}/${asset}" | awk '{ print $NF }')
else
    die "sha256sum, shasum, or openssl is required to verify the download"
fi

[ "$actual" = "$expected" ] || die "checksum verification failed for ${asset}"
printf 'Checksum verified.\n'

tar -xzf "${tmp_dir}/${asset}" -C "$tmp_dir" ai-usage
mkdir -p "$install_dir"
if command -v install >/dev/null 2>&1; then
    install -m 0755 "${tmp_dir}/ai-usage" "${install_dir}/ai-usage"
else
    cp "${tmp_dir}/ai-usage" "${install_dir}/ai-usage"
    chmod 0755 "${install_dir}/ai-usage"
fi

printf 'Installed %s\n' "$("${install_dir}/ai-usage" version)"
printf 'Binary: %s\n' "${install_dir}/ai-usage"

case ":${PATH}:" in
    *":${install_dir}:"*) ;;
    *)
        printf '\n%s is not currently on PATH. Add this to your shell profile:\n\n' "$install_dir"
        printf '  export PATH="%s:$PATH"\n' "$install_dir"
        ;;
esac
