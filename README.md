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

Download the archive for your platform from GitHub Releases, extract `ai-usage`, and place it on your `PATH`.

To build from source:

```bash
go install github.com/gustmrg/ai-usage/cmd/ai-usage@latest
```

Or from a checkout:

```bash
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
