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

OpenCode has no public usage API. Authenticated with your opencode.ai web session cookie, `ai-usage` resolves your workspace through the console's server functions and reads the server-authoritative Go subscription page — the same approach CodexBar uses.

The provider is unavailable without a cookie. An expired cookie, console schema change, or network failure is reported as an error rather than replaced with a local usage estimate.

The Go plan limits ($12 per rolling 5 hours, $30 per week, $60 per month) come from the public OpenCode Go documentation.

### Getting the session cookie

1. Sign in to [opencode.ai](https://opencode.ai) in your browser.
2. Open DevTools → Network, then reload the page.
3. Open Application/Storage → Cookies → `https://opencode.ai`.
4. Copy only the value of the cookie named `auth`.

### Configuration

Set the variables with `export` in your shell:

```bash
export OPENCODE_AUTH_COOKIE="paste-the-auth-cookie-value-here"
export OPENCODE_WORKSPACE_ID="wrk_..."   # optional, skips the workspace lookup
```

`OPENCODE_WORKSPACE_ID` accepts a raw `wrk_…` id or a full workspace URL such as `https://opencode.ai/workspace/wrk_.../go`. It is only needed when the automatic workspace lookup picks the wrong workspace.

Avoid putting the cookie directly in shell history. For a temporary session, prompt for it without echoing:

```zsh
read -rs "OPENCODE_AUTH_COOKIE?OpenCode auth cookie: "
export OPENCODE_AUTH_COOKIE
echo
```

Treat the cookie like a password. For persistent use, inject `OPENCODE_AUTH_COOKIE` from your operating-system keychain or password manager when launching `ai-usage` rather than committing it to a dotfile. The application reads the cookie from the environment and does not persist it.

The cookie expires with the browser session. When it does, `ai-usage` reports that the session is invalid or expired; copy a fresh cookie and restart the application.

`OPENCODE_SESSION_COOKIE` remains supported for compatibility. It accepts either the raw `auth` value, `auth=<value>`, or a full Cookie request header containing `auth`.
