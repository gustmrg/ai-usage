# Authentication

Each provider reuses credentials you already have; there is no ai-usage account. After configuring any provider, verify the setup without making provider requests:

```bash
ai-usage doctor
```

## Codex

Run the official Codex login flow once:

```bash
codex login
```

`ai-usage` reads `~/.codex/auth.json`. Expired OAuth access tokens are refreshed under an inter-process lock, and rotated credentials are written back atomically while preserving fields owned by Codex.

## Kimi

Kimi Code subscriptions use OAuth rather than an API key. Run the official login flow once:

```bash
kimi login
```

`ai-usage` reads `$KIMI_CODE_HOME/credentials/kimi-code.json`, defaulting to `~/.kimi-code/credentials/kimi-code.json`. It refreshes expired tokens using Kimi's cross-process lock directory so it coordinates with the official CLI.

`KIMI_CODE_BASE_URL`, `KIMI_CODE_OAUTH_HOST`, and `KIMI_OAUTH_HOST` are honored, including Kimi's endpoint-scoped credential filenames.

`KIMI_API_KEY` remains available as a secondary fallback for a key that is explicitly valid against the managed Kimi Code endpoint. Moonshot Open Platform keys normally target `api.moonshot.ai` or `api.moonshot.cn` and are not Kimi Code subscription credentials.

## OpenCode Go

OpenCode has no public usage API, so `ai-usage` supports two sources, tried in order:

1. **Console RPC (primary).** Authenticated with your opencode.ai web session cookie, the client resolves your workspace through the console's server functions and reads the Go subscription page — the same approach CodexBar uses.
2. **Local database (fallback).** When no cookie is set, or the RPC path fails (expired cookie, rotated server ids, offline), usage is summed from the per-message costs in opencode's local SQLite database. This sees only usage made through opencode on this machine; the monthly window uses the UTC calendar month because the subscription anchor date is not knowable locally.

If you only use opencode on this machine, the fallback alone is enough — there is nothing to configure. For server-authoritative, cross-device numbers, configure the cookie below.

The Go plan limits ($12 per rolling 5 hours, $30 per week, $60 per month) come from the public OpenCode Go documentation.

### Getting the session cookie

1. Sign in to [opencode.ai](https://opencode.ai) in your browser.
2. Open DevTools → Network, then reload the page.
3. Select any request to `opencode.ai` and copy the full `Cookie` request header value.

### Configuration

Set the variables with `export` in your shell:

```bash
export OPENCODE_SESSION_COOKIE="paste-the-full-cookie-header-here"
export OPENCODE_WORKSPACE_ID="wrk_..."   # optional, skips the workspace lookup
```

`OPENCODE_WORKSPACE_ID` accepts a raw `wrk_…` id or a full workspace URL such as `https://opencode.ai/workspace/wrk_.../go`. It is only needed when the automatic workspace lookup picks the wrong workspace.

To make the variables permanent, add the same `export` lines to `~/.zshrc`, `~/.bashrc`, or the profile used by your shell, then start a new shell (or `source` the file) before running `ai-usage`.

The cookie expires with your browser session. When it does, `ai-usage` silently falls back to the local database; replace the cookie to restore server-authoritative numbers.

### Database location overrides

The local database fallback looks for `opencode.db` in `$OPENCODE_DATA_DIR`, then `$XDG_DATA_HOME/opencode`, then `~/.local/share/opencode`:

```bash
export OPENCODE_DATA_DIR="$HOME/.local/share/opencode"   # optional override
```
