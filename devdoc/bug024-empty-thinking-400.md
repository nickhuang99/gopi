<!-- doc-class: normative -->
# Bug 024: Empty thinking-block replay → HTTP 400 "missing field `thinking`"

## Symptom

Long agent runs (20+ iterations) died with:

```
Error: LLM call failed (stream: deepseek: API error (status 400), fallback: deepseek: API error (status 400))
```

Reproduced live with the fib e2e task (fails at ~iteration 6-8, ~60-120s in).
The 400 hit the request *following* a response that contained an **empty
thinking block** (`{"type":"thinking","thinking":"","signature":"..."}`,
sometimes with a bogus message-id signature).

## Root cause

Two compounding facts:

1. DeepSeek's Anthropic-compatible endpoint sometimes streams an **empty
   thinking block**: `content_block_start {type: thinking, thinking: "", signature: ""}`,
   then only a `signature_delta` (often carrying the *message id* as the
   signature) and **no `thinking_delta` at all**.
2. GoPi stored that block in history and replayed it. `ClaudeContentBlock`
   uses `json:"thinking,omitempty"` / `json:"signature,omitempty"`, so
   `json.Marshal` **drops the empty `thinking` value** → the wire block was
   `{"type":"thinking","signature":"<msgid>"}` (or bare `{"type":"thinking"}`).
3. DeepSeek's deserializer requires the `thinking` field on thinking-type
   blocks (strict serde):

   ```
   Failed to deserialize the JSON body into the target type:
   messages[9].content: missing field `thinking` at line 1 column 11197
   ```

   → HTTP 400 on both the streaming and non-streaming calls → run dies.

Verified against the live API with minimal requests:

| wire block | result |
|---|---|
| `{"type":"thinking","signature":"abc"}` (thinking omitted) | **400 missing field `thinking`** |
| `{"type":"thinking","thinking":"","signature":""}` | 200 |
| `{"type":"thinking","thinking":"text"}` (signature omitted) | 200 |
| text block with extra `thinking`/`signature` keys | 200 |

So dropping the *empty* thinking block from history is safe (200) and is the
fix; the API only requires the fields when a thinking block is present.

## Fix

`BuildClaudeRequest` (internal/deepseek/claude_client.go): replay a thinking
block **only when it carries actual thinking text**:

```go
if m.ReasoningContent != "" {
    blocks = append(blocks, ClaudeContentBlock{Type: "thinking", Thinking: m.ReasoningContent, Signature: m.ThinkingSignature})
}
```

Previously the condition was `m.ReasoningContent != "" || m.ThinkingSignature != ""`,
which emitted empty-thinking blocks whenever a (bogus) signature existed.

## Verification

- Regression test `TestBuildClaudeRequest_EmptyThinkingDropped` (non-empty
  thinking + empty signature still replayed; empty thinking dropped).
- `go test ./internal/deepseek/ ./internal/agent/` — all Claude API tests pass
  (pre-existing failures: live legacy OpenAI-style Chat tests 404 after the
  anthropic switch; config tests are env-dependent).
- Live e2e: the exact fib task that reproduced the 400 in 57.6s now completes
  (`exit=0`, 9 iterations, sum=986). The run's raw log shows an empty thinking
  block streamed at req[4] and `empty_thinking_replayed=0` — the run survived.

## Related

- `internal/deepseek/retry.go` (uncommitted): error messages now include the
  API response body (first 500 chars) — this is what made the 400 diagnosable.
  Without it, retryDo discarded the body and only "status 400" was visible.
- The two other hard rules for this endpoint are documented in
  claude_client.go's header (thinking replay with signature, tool_use →
  tool_result adjacency).
