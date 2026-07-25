# Development

## Checks

```bash
go test ./...
go vet ./...
go build ./cmd/ai-usage
```

## Endpoint Stability

All current usage integrations rely on endpoints used by provider tooling but not documented as public APIs:

- Codex: `https://chatgpt.com/backend-api/wham/usage`
- Kimi: `https://api.kimi.com/coding/v1/usages`
- OpenCode Go: `https://opencode.ai/_server` and `https://opencode.ai/workspace/<id>/go`

The clients validate response shapes, cap response bodies, and report schema drift instead of converting unknown responses into zero usage. Fixture tests cover normal, weekly-only, reordered, aliased, malformed, and large-counter responses.

The OpenCode Go console server-function id is a build hash that can rotate on console deploys. If it changes, the provider reports schema drift instead of displaying estimated usage.

## Scope

The project intentionally excludes usage history and a macOS menu-bar frontend for now. The future menu-bar client should consume `status --json` and leave provider authentication in this binary.
