# ai-usage

`ai-usage` is a portable CLI and tabbed terminal dashboard for AI subscription usage. The initial release supports Codex/ChatGPT and Kimi Code without a daemon, hosted account, or native UI toolchain.

![Status: early development](https://img.shields.io/badge/status-early%20development-blue)

## Features

- Tabbed TUI with independent Codex and Kimi views.
- Setup tabs remain visible when a provider is not configured.
- Human-readable and versioned JSON CLI output.
- Depleting quota bars labeled with the percentage left.
- Reuses the credentials created by `codex login`.
- Reuses the subscription OAuth credentials created by `kimi login`.
- Five-minute request cache with last-known-good fallback.
- Concurrent, non-blocking provider refreshes in the TUI.
- One statically linked Go binary for macOS, Linux, and Windows.

## Install

Prebuilt binaries are published on [GitHub Releases](https://github.com/gustmrg/ai-usage/releases) for macOS, Linux, and Windows.

### Installer Script (macOS/Linux)

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

### Manual Download

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

### Windows

Download `ai-usage_<version>_windows_amd64.zip` and `checksums.txt` from [GitHub Releases](https://github.com/gustmrg/ai-usage/releases). In PowerShell:

```powershell
$Archive = "ai-usage_0.1.0_windows_amd64.zip"
Get-FileHash $Archive -Algorithm SHA256
# Compare the displayed hash with the entry in checksums.txt.

Expand-Archive $Archive -DestinationPath "$HOME\bin" -Force
& "$HOME\bin\ai-usage.exe" version
```

Add `$HOME\bin` to your user `PATH` if it is not already present.

### Install with Go

With Go 1.26 or newer:


```bash
go install github.com/gustmrg/ai-usage/cmd/ai-usage@latest
```

### Build from Source

```bash
git clone https://github.com/gustmrg/ai-usage.git
cd ai-usage
go build -o ai-usage ./cmd/ai-usage
```

## Authentication

### Codex

Run the official Codex login flow once:

```bash
codex login
```

`ai-usage` reads `~/.codex/auth.json`. Expired OAuth access tokens are refreshed under an inter-process lock, and rotated credentials are written back atomically while preserving fields owned by Codex.

### Kimi

Kimi Code subscriptions use OAuth rather than an API key. Run the official login flow once:

```bash
kimi login
```

`ai-usage` reads `$KIMI_CODE_HOME/credentials/kimi-code.json`, defaulting to `~/.kimi-code/credentials/kimi-code.json`. It refreshes expired tokens using Kimi's cross-process lock directory so it coordinates with the official CLI.

`KIMI_CODE_BASE_URL`, `KIMI_CODE_OAUTH_HOST`, and `KIMI_OAUTH_HOST` are honored, including Kimi's endpoint-scoped credential filenames.

`KIMI_API_KEY` remains available as a secondary fallback for a key that is explicitly valid against the managed Kimi Code endpoint. Moonshot Open Platform keys normally target `api.moonshot.ai` or `api.moonshot.cn` and are not Kimi Code subscription credentials.

Check setup without making provider requests:

```bash
ai-usage doctor
```

## Usage

Open the tabbed dashboard:

```bash
ai-usage
# or
ai-usage tui
```

Print current usage:

```bash
ai-usage status
ai-usage status --provider codex
ai-usage status --provider kimi
ai-usage status --json
```

Request fresh data instead of accepting a five-minute cache hit:

```bash
ai-usage refresh
ai-usage refresh --provider codex --json
```

TUI keys:

| Key | Action |
|---|---|
| `Tab`, `l`, `Right` | Next provider |
| `Shift+Tab`, `h`, `Left` | Previous provider |
| `1`, `2` | Select provider |
| `r` | Refresh selected provider |
| `R` | Refresh every provider |
| `?` | Toggle help |
| `q`, `Esc`, `Ctrl+C` | Quit |

## JSON Contract

`ai-usage status --json` emits a provider-neutral document intended for scripts and future desktop clients:

```json
{
  "schemaVersion": 1,
  "generatedAt": "2026-07-24T12:00:00Z",
  "providers": [
    {
      "schemaVersion": 1,
      "provider": "codex",
      "account": "default",
      "plan": "ChatGPT Plus",
      "collectedAt": "2026-07-24T11:59:40Z",
      "stale": false,
      "windows": [
        {
          "kind": "weekly",
          "label": "Weekly",
          "usedPercent": 51,
          "durationSeconds": 604800,
          "resetsAt": "2026-07-28T18:24:25Z"
        }
      ]
    }
  ],
  "errors": []
}
```

New fields may be added without changing `schemaVersion`. Existing field meanings will not change without a schema version increment.

## Cache

Successful normalized snapshots are stored under the platform user cache directory:

```text
macOS:   ~/Library/Caches/ai-usage/<provider>/<account-fingerprint>.json
Linux:   ~/.cache/ai-usage/<provider>/<account-fingerprint>.json
Windows: %LocalAppData%/ai-usage/<provider>/<account-fingerprint>.json
```

The fingerprint is a one-way hash of the active account or API credential, so changing accounts can never return another account's cached usage. Normal reads reuse snapshots for five minutes. If a refresh fails, a successful snapshot up to 24 hours old can be shown as stale. Failed or malformed responses never replace the last-known-good snapshot.

## Endpoint Stability

Both current usage integrations rely on endpoints used by provider tooling but not documented as public APIs:

- Codex: `https://chatgpt.com/backend-api/wham/usage`
- Kimi: `https://api.kimi.com/coding/v1/usages`

The clients validate response shapes, cap response bodies, and report schema drift instead of converting unknown responses into zero usage. Fixture tests cover normal, weekly-only, reordered, aliased, malformed, and large-counter responses.

## Development

```bash
go test ./...
go vet ./...
go build ./cmd/ai-usage
```

The project intentionally excludes usage history, OpenCode Go, and a macOS menu-bar frontend for now. The future menu-bar client should consume `status --json` and leave provider authentication in this binary.

## License

MIT
