# ai-usage

`ai-usage` is a portable CLI and tabbed terminal dashboard for AI subscription usage. It supports Codex/ChatGPT, Kimi Code, and OpenCode Go without a daemon, hosted account, or native UI toolchain.

![Status: early development](https://img.shields.io/badge/status-early%20development-blue)

## Features

- Tabbed TUI with independent Codex, Kimi, and OpenCode Go views.
- Setup tabs remain visible when a provider is not configured.
- Human-readable and versioned JSON CLI output.
- Depleting quota bars labeled with the percentage left.
- Reuses the credentials created by `codex login`.
- Reuses the subscription OAuth credentials created by `kimi login`.
- Tracks OpenCode Go through its server-authoritative opencode.ai console data.
- Five-minute request cache with last-known-good fallback.
- Concurrent, non-blocking provider refreshes in the TUI.
- One statically linked Go binary for macOS, Linux, and Windows.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/gustmrg/ai-usage/main/install.sh | sh
```

Other options — manual download, Windows, `go install`, building from source — are in [docs/installation.md](docs/installation.md).

## Quick start

Authenticate at least one provider (details in [docs/authentication.md](docs/authentication.md)):

```bash
codex login    # Codex/ChatGPT
kimi login     # Kimi Code
               # Set OPENCODE_AUTH_COOKIE for OpenCode Go
```

Then open the dashboard:

```bash
ai-usage
```

## Documentation

- [Installation](docs/installation.md) — installer script, manual download, Windows, Go, source builds
- [Authentication](docs/authentication.md) — Codex, Kimi, and OpenCode Go credential setup
- [Usage](docs/usage.md) — CLI commands, TUI keys, JSON contract, caching
- [Development](docs/development.md) — tests, endpoint stability, project scope

## License

MIT
