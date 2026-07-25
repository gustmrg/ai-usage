# Installation

Prebuilt binaries are published on [GitHub Releases](https://github.com/gustmrg/ai-usage/releases) for macOS, Linux, and Windows.

## Installer Script (macOS/Linux)

The installer detects your operating system and architecture, downloads the latest release, verifies its SHA-256 checksum, and installs to `~/.local/bin` without `sudo`:

```bash
curl -fsSL https://raw.githubusercontent.com/gustmrg/ai-usage/main/install.sh | sh
```

To inspect the script before running it:

```bash
curl -fsSLO https://raw.githubusercontent.com/gustmrg/ai-usage/main/install.sh
less install.sh
sh install.sh
```

Install a specific version or choose another destination:

```bash
AI_USAGE_VERSION=v0.1.0 sh install.sh
AI_USAGE_INSTALL_DIR="$HOME/bin" sh install.sh

# Options are also supported:
sh install.sh --version v0.1.0 --install-dir "$HOME/bin"
```

If the destination is not on `PATH`, the installer prints the exact `export PATH=...` line to add to your shell profile.

## Manual Download

Choose the archive for your machine:

| System | Release asset |
|---|---|
| Apple Silicon Mac | `ai-usage_<version>_darwin_arm64.tar.gz` |
| Intel Mac | `ai-usage_<version>_darwin_amd64.tar.gz` |
| Linux x86-64 | `ai-usage_<version>_linux_amd64.tar.gz` |
| Linux ARM64 | `ai-usage_<version>_linux_arm64.tar.gz` |
| Windows x86-64 | `ai-usage_<version>_windows_amd64.zip` |

On macOS or Linux, download the archive and `checksums.txt` from the same release. Verify and install it with:

```bash
# Replace ASSET with the downloaded archive name.
ASSET=ai-usage_0.1.0_darwin_arm64.tar.gz
grep " $ASSET$" checksums.txt | shasum -a 256 -c -  # macOS
grep " $ASSET$" checksums.txt | sha256sum -c -       # Linux
tar -xzf "$ASSET"
mkdir -p "$HOME/.local/bin"
install -m 0755 ai-usage "$HOME/.local/bin/ai-usage"
```

Ensure the destination is on `PATH`:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

Add that line to `~/.zshrc`, `~/.bashrc`, or the profile used by your shell to make it permanent.

## Windows

Download `ai-usage_<version>_windows_amd64.zip` and `checksums.txt` from [GitHub Releases](https://github.com/gustmrg/ai-usage/releases). In PowerShell:

```powershell
$Archive = "ai-usage_0.1.0_windows_amd64.zip"
Get-FileHash $Archive -Algorithm SHA256
# Compare the displayed hash with the entry in checksums.txt.

Expand-Archive $Archive -DestinationPath "$HOME\bin" -Force
& "$HOME\bin\ai-usage.exe" version
```

Add `$HOME\bin` to your user `PATH` if it is not already present.

## Install with Go

With Go 1.26 or newer:

```bash
go install github.com/gustmrg/ai-usage/cmd/ai-usage@latest
```

## Build from Source

```bash
git clone https://github.com/gustmrg/ai-usage.git
cd ai-usage
go build -o ai-usage ./cmd/ai-usage
```
