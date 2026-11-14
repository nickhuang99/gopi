<!-- doc-class: historical -->
# GLM Direct-Connect Compatibility Fix

Branch: `feat/glm-direct-compat` (one worktree, ~/workspace/gopi-glm-fix)

## Symptom (before)

`gopi --settings ~/.gopi/.glm_settings.json` (base_url=https://open.bigmodel.cn/api/anthropic,
model=glm-5.3-flash, thinking:disabled) against the GLM Anthropic endpoint:

- reported **0 tokens / 0 tool_calls / ~0.6s / 2 iterations**, exit "OK"
- some raw logs showed **0 `← CHUNK`** lines at all
- the request line correctly carried `"model":"glm-5.3-flash"` but produced nothing

Raw log `gopi-raw-*221150.log` (repro): `REQ=2 CHUNK=0` — both requests logged but zero
response events.

## Root Cause (5-why)

**Symptom** → loop exits after 2 iterations with empty results.
**Why-1** → an empty "end_turn" (empty content, no tool calls) increments
`consecutiveEndTurn` to `maxConsecutiveEndTurnRounds=2`.
**Why-2** → the round streamed zero usable events (no content block, no `message_delta`,
empty `stop_reason` → mapped to "stop").
**Why-3** → the HTTP response carried **no SSE events** (0 `← CHUNK`); `parseClaudeSSE`
saw immediate EOF and silently closed the channel.
**Why-4 (transport)** → gopi hit the **wrong URL path**.

### Bug A — wrong API path (the primary cause)

`claudeMessagesURL()` appended `/messages` to any base ending in `/anthropic`:

```
DeepSeek base https://api.deepseek.com/anthropic   → .../anthropic/messages   ✓ (DeepSeek path)
GLM      base https://open.bigmodel.cn/api/anthropic → .../api/anthropic/messages ✗
```

GLM expects the **standard Anthropic wire path `/v1/messages`** (the CLAUDE guide sets
`ANTHROPIC_BASE_URL=.../api/anthropic` and Claude Code appends `/v1/messages`). Against
`/anthropic/messages` GLM returns **HTTP 200 with body `{"code":500,"msg":"404 NOT_FOUND"}`
— a single-line JSON with no `data:` prefix**. Because the status is 200, `retryDo` treats
it as success; `scanSSEEvents` flushes the one line at EOF as a single "event" that has no
`data:` line, so `emitClaudeEvent` drops it → **0 CHUNK** → the 2-iteration empty run.
Verified live with `curl`: `/anthropic/messages` → 200 + 404-body; `/v1/messages` → real SSE.

### Bug B — `thinking:disabled` is rejected by GLM

Even on the correct path, GLM **always thinks** and **rejects `thinking:{type:"disabled"}`**
with **HTTP 400 error 1210** ("该模型始终思考，不支持关闭思考；请使用 low、high 或 max").
Verified live with `curl` on `/v1/messages`.

## Fixes

### Fix 1 — always append `/v1/messages` to an `/anthropic` base (`internal/deepseek/claude_client.go`)

`claudeMessagesURL()` now returns `{base}/v1/messages` when `base` ends in `/anthropic`.
DeepSeek **accepts both** `/anthropic/messages` and `/anthropic/v1/messages` (both HTTP 200,
verified live), so this is a **universal** fix: correct standard path for GLM, harmless for
DeepSeek. `TestClaudeMessagesURL` updated to assert `/v1/messages` + a GLM case.

### Fix 2 — omit `thinking` for GLM models when disabled (`internal/deepseek/claude_client.go`)

New `isGLMModel()` (prefix `glm-`/`glm_`/contains `/glm-`). In `BuildClaudeRequest`, when a
GLM model is requested **with thinking disabled**, the `thinking` field is **omitted**
(`req.Thinking = nil`) — GLM then thinks natively and its budget is ignored by the harness
streaming loop (which already handles thinking + signature replay). DeepSeek models keep
`thinking:{type:"disabled"}` untouched (verified regression: DeepSeek still sends the field
and gets 0 thinking_delta). New test `TestBuildClaudeRequest_GLMDisabledThinkingOmitsField`
pins all three cases (GLM disabled → omitted; GLM enabled → kept; DeepSeek disabled → kept).

## Verification (after)

| run | settings | result |
|-----|----------|--------|
| GLM simple task `Reply with exactly: GLM_OK` | `.glm_settings.json`, env cleared | **38.8s, 4 iters, prompt=27087, completion=452** (was 0.6s / 0 tok) |
| GLM tool loop `read GOPI.md first 20 lines, summarize, git status` | `.glm_settings.json`, env cleared | **147.8s, 12 iters, prompt=122305, completion=3040, 12 tool_calls; thinking=4020 chars (9/12 rounds)** — GLM always-on thinking handled |
| DeepSeek regression same tool loop | default env (deepseek) | **19.2s, 7 iters, prompt=58877, completion=1612** — thinking field disabled present, 0 thinking_delta |

- Request line now goes to model=glm-5.3-flash, `thinking` field omitted for GLM,
  proper SSE streaming (verifiable in raw log `gopi-raw-*221825.log`, `221914.log`).
- `go build ./...` OK; `go test ./internal/deepseek/` and `./internal/agent/` pass.
- GLM's intermittent 0-CHUNK (which read as endpoint instability) was caused by Bug A, not
  by the GLM network: with the correct path, GLM streams reliably. No retry hack needed.

## Not Done / Honest Notes

- No mid-stream empty-body **retry** was added: the URL fix resolved the reproduced failure
  at its source (GLM now returns real SSE). If a future provider genuinely drops a
  mid-stream connection, `retryDo` covers the request phase but an empty-body 200 is still
  invisible to it — a downstream empty-stream retry would be the extension point
  (`chatOnceAttempt`, after the `for ev := range events` loop). Recorded for future work,
  deliberately not speculative-added.
- `.glm_settings.json` still says `thinking:disabled`, but GLM ignores it (always thinks).
  The setting is harmless now; a user who wants lighter thinking from GLM would configure a
  GLM provider accordingly (GLM has no off switch).
