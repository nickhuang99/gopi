<!-- doc-class: historical -->
# Task Report: gopi proxy — Anthropic↔OpenAI protocol proxy (GLM-5.3-Flash)

## Summary

Added the `gopi proxy` subcommand: a local HTTP server that accepts the
Anthropic Messages protocol (`/v1/messages` + Anthropic SSE, both stream and
non-stream) and translates it to the OpenAI Chat Completions protocol before
forwarding to an OpenAI-only upstream (GLM-5.3-Flash at
`open.bigmodel.cn/api/paas/v4`). Responses are translated back to the Anthropic
SSE wire format so unmodified Anthropic clients — including gopi's own
DeepSeek-style client — can talk to GLM with zero code changes.

Zero new external dependencies (net/http + encoding/json only).

## Files

```
cmd/agent/cli_proxy.go         # gopi proxy subcommand: flag parsing, CLI wiring
cmd/agent/main.go              # dispatch chain += isProxySubcommand()
internal/proxy/types.go        # Anthropic + OpenAI wire types
internal/proxy/request.go      # Anthropic→OpenAI request conversion
internal/proxy/response.go     # OpenAI non-stream → Anthropic message
internal/proxy/stream.go       # OpenAI SSE chunk → Anthropic SSE events (stream translator)
internal/proxy/server.go       # HTTP server: handlers, forwarding, error path
internal/proxy/request_test.go # request conversion unit tests
internal/proxy/stream_test.go  # stream translation unit tests (thinking/tool delta)
internal/proxy/response_test.go# non-stream response unit tests
internal/proxy/server_test.go  # E2E roundtrip tests via httptest mock upstream
```

## Completed

### 1. Request conversion (Anthropic → OpenAI)
- `system` (string or block array) → leading `role=system` message.
- message `content` string → `content` string; content array → OpenAI parts.
- `role=user` text+image blocks → OpenAI `content` array (`image_url` incl.
  base64→data URL).
- `role=assistant` text + `tool_use` + thinking → assistant message with
  `tool_calls` (thinking dropped — GLM reasons itself); tool arguments pass
  through as the JSON string.
- `tool_result` blocks → separate `role=tool` messages with the matching
  `tool_call_id` (preserved from the assistant tool_use id).
- `tools` → OpenAI `type:"function"` + `function.parameters` (input_schema is
  already JSON-Schema).
- `tool_choice`: auto/any→`auto`, none→`none`, tool→`{"type":"function","function":{name}}`.
- `max_tokens`, `temperature`, `top_p`, `stop_sequences` mapped; `top_k`
  dropped (OpenAI has no direct equivalent).
- `thinking.type=="disabled"` mapped to `{"thinking":{"type":"disabled"}}`;
  enabled/absent left to GLM's default reasoning.
- Auth: client `x-api-key`/`Authorization` replaced with `Authorization: Bearer <upstream-key>`.

### 2. Stream response conversion (OpenAI → Anthropic SSE)
Emits the correct Anthropic event ordering:
`message_start` → (`content_block_start` / `content_block_delta` /
`content_block_stop`)* → `message_delta`(stop_reason+usage) → `message_stop`.
- `delta.content` → text block, `text_delta` deltas.
- `delta.reasoning_content` → **thinking block** (`thinking_delta`) so GLM's
  CoT is surfaced as Anthropic thinking, transparent to the upper layer.
- `delta.tool_calls` → **tool_use blocks**. Incremental deltas are accumulated
  correctly: the first fragment on an index opens the block with id/name;
  subsequent `arguments` fragments concatenate in order and stream as
  `input_json_delta`. Multiple tool indexes each get their own block.
- `finish_reason` → Anthropic `stop_reason` (`stop`→`end_turn`,
  `tool_calls`→`tool_use`, `length`→`max_tokens`).
- `usage` → `message_delta.usage`.

### 3. Non-stream response conversion
`choices[0].message` → Anthropic message with `content` block array
(reasoning_content→thinking, content→text, tool_calls→tool_use), `stop_reason`,
`usage`. id preserved from the upstream response.

### 4. Tool-call loop
- `OpenAI tool_calls.id` ↔ Anthropic `tool_use.id` preserved exactly — the
  client's `tool_result` references the same id.
- Client `tool_result` → OpenAI `role=tool` message with the corresponding
  `tool_call_id`.
- Multiple concurrent tool calls in one assistant turn map correctly.

### 5. Errors
- Upstream non-2xx → Anthropic error envelope
  `{"type":"error","error":{"type":"api_error","message":...}}` with the
  upstream HTTP status passthrough.

### 6. Logging
- `--verbose`: method/path/upstream/stream/max_tokens + per-request timing.
- Default: only startup line + errors.

### 7. Endpoints
- `POST /v1/messages` (standard Anthropic) and `POST /anthropic/messages`
  (gopi's own DeepSeek-style client path) both handled.

## Verification

### Unit + E2E (internal/proxy) — 13 tests all pass
- request conversion: system/messages, image blocks, assistant tool_use,
  tool_result, tools+tool_choice(+variants), temp/top_p/stop, thinking-disabled.
- stream: text ordering + delta concatenation, thinking block ordering,
  tool_calls incremental accumulation (+multiple calls), finish_reason mapping,
  usage passthrough/nil.
- non-stream: text+thinking, tool_calls, empty content.
- server E2E (httptest mock upstream): stream roundtrip, non-stream roundtrip,
  tool roundtrip id preservation, upstream error passthrough (429),
  tool_result→tool message, /anthropic/messages alias.

### Manual E2E — mock upstream + curl
`./bin/gopi proxy --upstream openai --openai-base-url http://127.0.0.1:9999 ...`
- Stream curl → correct Anthropic SSE sequence: message_start → thinking block
  → text block → message_delta(end_turn) → message_stop.
- Non-stream curl → `{"type":"message","content":[thinking,text],...}`.
- Verbose logs confirm request/response/timing.

### Real GLM E2E (the key requirement)
```
./bin/gopi proxy --upstream openai \
  --openai-base-url https://open.bigmodel.cn/api/paas/v4 \
  --openai-model glm-5.3-flash --openai-key '0922...' \
  --listen 127.0.0.1:8090 --verbose
```
- **Stream (max_tokens=8192):** curl Anthropic `/v1/messages` → full well-formed
  Anthropic SSE: 72 `content_block_delta` (thinking + text), message_delta
  `{stop_reason:"end_turn", usage:{input_tokens:35, output_tokens:76}}`,
  message_stop. ✓
- **Tool-call (max_tokens=8192):** same + tools → thinking block, then
  tool_use block `{id:"call_8cb69...", name:"get_weather"}` + `input_json_delta
  {"city":"Beijing"}`, `message_delta stop_reason:"tool_use"`, message_stop. ✓
- **Non-stream:** → `{thinking, text:"Hello."}` + usage. ✓
- **gopi-through-proxy (ultimate test):** settings base_url=`http://127.0.0.1:8090`
  and running `gopi "Reply with exactly: PROXY_WORKS_42"` → gopi's own Anthropic
  client (POST `/anthropic/messages` + SSE) went through the proxy to GLM and
  returned `PROXY_WORKS_42`; full gopi loop (memory, tools, completion) ran. ✓

### Build / Vet / Test
- `go build ./...`: OK.
- `go vet ./internal/proxy/ ./cmd/agent/`: clean (no warnings).
- `go test ./internal/{proxy,agent,deepseek,config,gopilog,checkpoint,text2img}
  ./cmd/{agent,text2img-verify}`: all PASS.
- ⚠ `internal/tools`: pre-existing failure (database/sql test times out) —
  fails identically on base master 76192cc; NOT caused by this change
  (verified via a git worktree at the base commit).

## Not Done
- `--upstream anthropic` mode is wired for flag parsing but this is a pass-through
  to the same OpenAI translation against a different base/key/model; a true
  Anthropic-native passthrough is out of scope (GLM is OpenAI-only, the stated
  goal).
- `top_k` is dropped (OpenAI has no analogue). `reasoning_effort` mapping for
  thinking budgets is not wired (GLM defaults its own reasoning depth).

## Commits
```
4661c41 feat(proxy): anthropic-openai protocol proxy (types, request/response conversion, stream translator, server, gopi proxy CLI)
dec7990 test(proxy): unit + E2E roundtrip tests — request conversion, stream thinking/tool delta accumulation, non-stream, tool roundtrip, error passthrough
1a860b0 fix(proxy): non-stream id preserves upstream id; single [proxy] log prefix
65ddc94 style(proxy): gofmt
87b9f21 feat(proxy): accept /anthropic/messages alias so gopi's own Anthropic client can point base_url at the proxy
```

## README usage

```bash
# Build
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/

# Run the proxy against GLM (OpenAI-only upstream)
./bin/gopi proxy \
  --upstream openai \
  --openai-base-url https://open.bigmodel.cn/api/paas/v4 \
  --openai-model glm-5.3-flash \
  --openai-key '<GLM_KEY>' \
  --listen 127.0.0.1:8090 \
  --verbose

# Point any Anthropic client's base_url at 127.0.0.1:8090
curl -N -X POST http://127.0.0.1:8090/v1/messages \
  -H 'Content-Type: application/json' -H 'x-api-key: any' \
  -d '{"model":"claude-3","max_tokens":8192,"stream":true,
       "messages":[{"role":"user","content":"hi"}]}'

# gopi itself: set settings base_url to http://127.0.0.1:8090
```
