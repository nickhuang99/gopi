<!-- doc-class: historical -->
# Task Report: Switch GoPi to DeepSeek Claude API Interface

## Completed

### Phase 1 — Tag & Protect Current Code
- `git tag openai-sdk-legacy` + `git branch openai-sdk-backup` at HEAD `9e46d1d`
  in `/home/user/work/GoPi`.
- Push skipped: no `origin` remote is configured on this machine (documented).

### Phase 2 — Claude API Client (`internal/deepseek/claude_client.go`, ~700 lines)
- `ClaudeChat` / `ClaudeChatStream`: POST `{base}/anthropic/messages` with
  Anthropic content blocks, `anthropic-version: 2023-06-01` header, retry
  support via the shared `retryDo`.
- SSE parser for Claude events: `message_start`, `content_block_start`
  (thinking/text/tool_use), `content_block_delta` (text_delta, thinking_delta,
  input_json_delta, signature_delta), `content_block_stop`, `message_delta`
  (stop_reason + usage), `message_stop`, `ping`, `error`. Raw log preserves
  `event:` + `data:` prefixes → complete replayable transcript.
- `BuildClaudeRequest`: internal OpenAI-style history → Claude wire format.
- `MapClaudeStopReason`: `end_turn→stop`, `tool_use→tool_calls`,
  `max_tokens→length`, `refusal→content_filter`.
- Config: `ANTHROPIC_AUTH_TOKEN` > `DEEPSEEK_API_KEY` > settings (key);
  `ANTHROPIC_BASE_URL` > `DEEPSEEK_BASE_URL` > settings (base URL); base URL
  accepted with or without the `/anthropic` prefix; `ANTHROPIC_MODEL` takes
  precedence in `ResolveModel`.
- Refactor: `scanSSEEvents` shared frame scanner + `newHTTPClient()` shared
  transport (OpenAI path behavior unchanged — full SSE test suite still green).

### Phase 3 — Loop Adaptation (`internal/agent/loop.go`)
- Exit logic (bug022): **never** exits on `toolCalls == 0`. Tracks
  `consecutiveEndTurn`; exit only after `maxConsecutiveEndTurnRounds = 2`
  consecutive end_turn rounds with no tool calls. Between rounds a
  continuation user message is injected; the substantive first end_turn answer
  is returned (not the terse confirmation round).
- Truncated (`max_tokens`) tool calls are **dropped** from history — the
  Claude API rejects `tool_use` blocks without immediately-following
  `tool_result` blocks (verified live; the OpenAI path used to pass them).
- Thinking round-trip: thinking blocks + signatures stored in context
  (`Message.ThinkingSignature`, `AddAssistantMessageWithThinking`) and
  replayed — the API 400s on assistant history that drops thinking blocks.
- `chatOnce` rewritten for Claude streaming (block-index accumulation, live
  text/thinking display, usage from `message_delta` only — `message_start`
  carries input-only counts and would double-count), with non-streaming
  `ClaudeChat` fallback.
- `ParseTaskList` exit-check kept as observability-only (log warning).

### Phase 4 — System Prompt
- `"You are GoPi, a coding agent. Tools: read, write, edit, bash. Use tool_use content blocks."`

### Tests
- New: `claude_client_test.go` — conversion rules (system blocks, thinking
  replay, tool-result coalescing, user merging, empty drops, thinking param),
  stop-reason mapping, response helpers, SSE transcript parsing (captured from
  the live API), HTTP integration (stream + non-stream), URL resolution.
- Rewritten: 4 agent `Run()` tests use Claude SSE mocks; new coverage for the
  3-call tool→end_turn→confirmation flow and the continuation message.
- Legacy `runNonStreaming` + OpenAI client tests untouched (still green).

### Verification (live, real API key)
- `go build ./...`: OK · `go vet ./...`: OK · `go test ./...`: ALL PASS
- E2E: "Read secret.txt..." → 3 iterations (tool_use → end_turn → confirmation),
  correct answer, cache hits growing (0 → 1152 → 1280), session log with
  thinking/tool_use/tool_result blocks, raw log with event: prefixes.
- ANTHROPIC_BASE_URL=https://api.deepseek.com/anthropic + ANTHROPIC_MODEL
  verified working; `[thinking]` live display verified.

### Commits (branch feat/claude-api)
- `f10ab29` feat(deepseek): Claude Messages API client
- `f7122cf` refactor(deepseek): shared SSE scanner + transport; ThinkingSignature
- `f9dbaae` feat(agent): Claude API loop — consecutiveEndTurn exit logic (bug022)
- `d28795d` test(agent): rewrite Run() mocks for Claude SSE format
- `77a7989` docs(cli): usage text lists ANTHROPIC_* env vars

## Not Done
- No git push (no `origin` remote); branch must be pushed/merged separately.
- `GOPI_REASONING_EFFORT` (OpenAI-only param) is no longer sent on the Claude
  path; DeepSeek V4 thinking depth is controlled by the thinking budget
  (2048) instead. Env var is accepted but inert.
- `runNonStreaming` still uses the legacy OpenAI client (kept as reference).
- Heartbeat task-list guardrails untouched (out of scope).
- Retry error bodies still not surfaced in error messages (raw log has them).

## Verification
- Build: `go build ./...` OK
- Tests: `go test ./...` — agent/deepseek/tools all PASS
- Commit: 77a7989 (HEAD of feat/claude-api, 5 commits)
- Live E2E: PASS (3 iterations, correct result, cache + session/raw logs intact)
