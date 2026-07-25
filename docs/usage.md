# Usage

## Interactive dashboard

```bash
ai-usage
# or
ai-usage tui
```

| Key | Action |
|---|---|
| `Tab`, `l`, `Right` | Next provider |
| `Shift+Tab`, `h`, `Left` | Previous provider |
| `1`, `2` | Select provider |
| `r` | Refresh selected provider |
| `R` | Refresh every provider |
| `?` | Toggle help |
| `q`, `Esc`, `Ctrl+C` | Quit |

## CLI

Print current usage:

```bash
ai-usage status
ai-usage status --provider codex
ai-usage status --provider kimi
ai-usage status --provider opencodego
ai-usage status --json
```

Request fresh data instead of accepting a five-minute cache hit:

```bash
ai-usage refresh
ai-usage refresh --provider codex --json
```

Check provider credentials and local paths without making requests:

```bash
ai-usage doctor
```

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

The fingerprint is a one-way hash of the active account or API credential, so changing accounts can never return another account's cached usage. Normal reads reuse snapshots for five minutes. If a refresh fails, most providers can show a successful snapshot up to 24 hours old as stale. OpenCode Go reports the console error without stale usage because its quota must remain server-authoritative. Failed or malformed responses never replace the last-known-good snapshot.
