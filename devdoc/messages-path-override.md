<!-- doc-class: normative -->
# messages_path Override for Custom Anthropic Endpoints

**Branch:** `feat/messages-path-override`
**Status:** merged-ready, verified 2026-08-28
**Base:** master @ c29ce89

## Why

GoPi's `claudeMessagesURL` (internal/deepseek/claude_client.go) only knew two
ways to derive the Claude Messages endpoint from `base_url`:

1. base ends with `/anthropic` → append `/v1/messages`
   (DeepSeek `https://api.deepseek.com/anthropic`, GLM
   `https://open.bigmodel.cn/api/anthropic`)
2. otherwise → append `/anthropic/messages`

Tencent's hy4-preview Anthropic-compatible endpoint breaks both derivations:
the only valid path is `https://tokenhub.tencentmaas.com/v1/messages`
(verified live 2026-08-28: `Bearer` + `anthropic-version` headers pass, model
`hy4-preview` returned; both `/anthropic/v1/messages` and
`/v1/anthropic/messages` 404).

## Change

New settings.json field `messages_path` — a full request path appended
verbatim to `base_url`:

```json
{
  "api_key": "sk-...",
  "base_url": "https://tokenhub.tencentmaas.com",
  "model": "hy4-preview",
  "messages_path": "/v1/messages"
}
```

### Files

- **internal/deepseek/config.go** — `Config.MessagesPath string
  \`json:"messages_path"\``
- **internal/deepseek/client.go** — `Client.MessagesPath string` field
- **internal/deepseek/claude_client.go**
  - `claudeMessagesURL()`: `if c.MessagesPath != "" { return base +
    c.MessagesPath }` **before** the legacy suffix logic (override wins)
  - `NewClaudeClient()`: wires `cfg.MessagesPath` via
    `claudeMessagesPathFromConfig(cfg)` (nil-safe)
  - Settings-only override — no env var added (matches the task scope)

### Precedence

```
MessagesPath != ""  →  {base_trimmed}{messages_path}
else base ends /anthropic  →  {base}/v1/messages
else  →  {base}/anthropic/messages
```

`base` is always `TrimRight(BaseURL, "/")`, so trailing-slash base + leading
slash path compose cleanly (`https://tokenhub.tencentmaas.com/` +
`/v1/messages` → no double slash).

### Compatibility

Unset `messages_path` (zero value `""`) takes the exact legacy branch — DeepSeek
and GLM behavior byte-identical. The vision client is untouched (its own
`ResolveVisionBaseURL` path; `MessagesPath` empty there keeps existing
behavior).

## Tests

- `TestClaudeMessagesURL` (claude_client_test.go) — extended table with
  `messagesPath` column:
  - regression: all 5 pre-existing cases unchanged (DeepSeek bare,
    DeepSeek/GLM `/anthropic`-suffixed, trailing slash, non-anthropic base)
  - new: `{base}/v1/messages` override (tencent + trailing-slash variant),
    override winning over an `/anthropic`-suffixed base, custom path
- `TestLoadConfig_MessagesPath` (config_test.go) — settings.json JSON unmarshal
  of `messages_path`; nil/missing-file regression

## Verification

- `go build ./...` — clean
- `go test ./...` — 9/9 packages PASS (incl. internal/deepseek, uncached
  `-count=1` behavior verified via full run)
- `gofmt -l internal/deepseek` — clean (no touched file listed)

## Live reference

Tencent hy4-preview endpoint (from task brief, verified live):
`https://tokenhub.tencentmaas.com/v1/messages` — Bearer auth +
`anthropic-version` header, returns `model=hy4-preview`.
