<!-- doc-class: normative -->
# GoPi Optimization Report: Comparison with DeepSeek Pi Integration

**Date:** 2025-07-17  
**Sources analyzed:**
- DeepSeek Pi integration doc: https://api-docs.deepseek.com/zh-cn/quick_start/agent_integrations/pi_mono
- pi-mono source: `/home/user/work/pi-mono/packages/agent/src/agent-loop.ts` (792 lines)
- pi-mono AI layer: `/home/user/work/pi-mono/packages/ai/` (retry, context overflow, token tests)
- GoPi source: `/home/user/work/GoPi/` (all files)

---

## 1. What Pi Does That GoPi Should Add

### 1.1 Cost Tracking (from models.json)
Pi embeds per-model cost data in `models.json`:
```json
"cost": { "input": 1.74, "output": 3.48, "cacheRead": 0.145, "cacheWrite": 0 }
```
Pi aggregates usage × cost to show session spend. GoPi receives `Usage` (prompt_tokens, completion_tokens) in every response but never aggregates or reports it.

**Impact:** Users have no visibility into API spend. A long agent session can silently cost $10+.

### 1.2 Retry with Exponential Backoff
pi-mono has a robust retry layer:
- Disables the OpenAI SDK's built-in retries (`maxRetries: 0`)
- Implements its own provider-level retry loop with `maxRetries` (default 2) and `maxRetryDelayMs`
- Handles HTTP 429 (rate limit) by reading `retry-after-ms` header
- Handles HTTP 5xx (server errors) with backoff
- Tested with fake timers in `openai-completions-retry.test.ts`

GoPi has **zero retry logic**. A single 429 or 503 kills the entire agent run.

### 1.3 Context Overflow Detection
pi-mono has:
- `isContextOverflow()` utility in `packages/ai/src/utils/overflow.ts`
- Full e2e tests (`context-overflow.test.ts`) across Anthropic, OpenAI, and other providers
- Detects when input exceeds `contextWindow` and returns `stopReason: "error"` with a descriptive message
- Model definitions include `contextWindow` and `maxTokens` (DeepSeek V4: 1M context, 384K max output)

GoPi has no context window awareness at all. If the conversation grows past ~128K (or whatever the actual limit is), the API will fail cryptically.

### 1.4 SSE Streaming in the Agent Loop
pi-mono's `agentLoop` / `streamAssistantResponse` streams LLM output token-by-token, emitting events as they arrive. This gives users real-time feedback.

GoPi's `ChatStream()` is **implemented** in the client (with proper SSE parsing, buffer sizing, `[DONE]` handling) but the agent loop (`internal/agent/loop.go`) only calls `client.Chat()` — the non-streaming path. The stream path is dead code.

### 1.5 Reasoning/Thinking Format Compatibility
The DeepSeek models.json reveals model-specific compat fields:
```json
"compat": {
  "requiresReasoningContentOnAssistantMessages": true,
  "thinkingFormat": "deepseek",
  "reasoningEffortMap": { "minimal": "high", ..., "xhigh": "max" }
}
```
These control how reasoning tokens (DeepSeek-R1/V4's thinking blocks) are formatted in API requests. Without these:
- Reasoning content may be stripped or malformed on assistant messages
- `reasoning_effort` parameters won't map correctly

GoPi sends no reasoning-related parameters at all.

### 1.6 Truncated Tool Call Handling
pi-mono detects when a response hits `finish_reason: "length"` (output token limit) and:
- Marks all tool calls in that message as failed with a clear error
- Tells the model: *"Tool call was not executed: the response hit the output token limit, so its arguments may be truncated. Re-issue the tool call with complete arguments."*

GoPi ignores `finish_reason` entirely.

### 1.7 Structured Agent Events
pi-mono emits typed events: `agent_start`, `turn_start`, `message_start`, `message_end`, `tool_call_start`, `tool_call_end`, etc. This enables UIs, logging, and progress tracking.

GoPi's `Run()` returns a single `(string, error)`. There is no streaming feedback during execution.

### 1.8 Tool Execution Safety
pi-mono:
- Validates tool arguments before execution (`validateToolArguments`)
- Has `before`/`after` hooks per tool
- Wraps execution in try/catch with proper error tool results (`isError: true`)
- Handles abort signals mid-execution

GoPi does naive type assertions (`args["path"].(string)`) with zero values on failure, and no pre/post hooks.

### 1.9 Model Selection / Provider Abstraction
Pi supports 15+ providers via `models.json`. The model catalog is typed and validated. GoPi hardcodes `"deepseek-v4-pro"` and `"https://api.deepseek.com"`.

### 1.10 System Prompt Configuration
GoPi's `Context.SystemPrompt` field exists but is never populated from config — `cmd/agent/main.go` never sets it. Without a system prompt, the model has no instructions about being a coding agent.

---

## 2. What GoPi Does Better Than Pi

| GoPi Advantage | Pi Disadvantage |
|---|---|
| **Static binary** (`CGO_ENABLED=0`) → single file, no runtime | Requires Node.js + npm + 100MB+ of node_modules |
| **Zero dependencies** (only Go stdlib) | Dependency on `openai` npm, `@mariozechner/pi-*` packages |
| **~400 lines of Go** total | Thousands of TypeScript lines across many packages |
| **Instant cold start** | Node.js startup + module resolution overhead |
| **Single-file deployment** | Complex install (`npm install -g`) or shell script |
| **Lower memory** (no V8 heap) | Node.js baseline ~30-50MB |
| **Simple mental model** — one loop, one client | Many abstractions: AgentContext, convertToLlm, StreamFn, EventStream, etc. |
| **SSE implementation is clean and correct** | Spread across multiple files |
| **Direct bash tool** (practical for coding) | Same idea, but GoPi's is simpler |

---

## 3. Priority-Ranked Optimization Recommendations

### 🔴 P0 — Critical (Must Add)

#### P0.1: Retry Logic with Backoff
**Effort:** Small (~40 lines)  
**What:** Add retry to `deepseek.Client.Chat()` and `ChatStream()`. On HTTP 429 or 5xx, retry up to 3 times with exponential backoff (1s, 2s, 4s). Read `retry-after-ms` header when present.

**Why:** A single transient API error kills the entire agent run today. This is the #1 reliability gap.

**Implementation:** Wrap the HTTP call in a loop inside `Chat()` / `ChatStream()`. Extract retry logic into a shared helper.

---

#### P0.2: Streaming Agent Loop
**Effort:** Medium (~80 lines)  
**What:** Use the already-implemented `ChatStream()` in the agent loop instead of `Chat()`. Accumulate delta content/tool calls across chunks. Emit real-time output to stderr.

**Why:** Users wait blind for 30-60 seconds with no feedback. Streaming gives UX parity with Pi and lets users see thinking progress.

**Implementation:** Replace `client.Chat()` call in `internal/agent/loop.go` with `client.ChatStream()`. Accumulate deltas into the final Message. Print delta content to stderr as it arrives.

---

### 🟡 P1 — High Value

#### P1.1: Token Usage Tracking & Cost Reporting
**Effort:** Small (~50 lines)  
**What:** Sum `Usage.PromptTokens` and `Usage.CompletionTokens` across all LLM calls in a session. At the end of `Run()`, log total tokens and estimated cost using the hardcoded DeepSeek V4 pricing ($1.74/$3.48 per million tokens).

**Why:** Users need cost visibility. A long session can cost $5-20 without the user knowing.

**Implementation:** Add `TotalUsage Usage` field to `Agent`. Accumulate after each API call. Print in `Run()` before returning.

---

#### P1.2: Context Window Awareness
**Effort:** Small (~40 lines)  
**What:** Before each API call, estimate token count of the message history (rough heuristic: chars/4). If it exceeds a configurable limit (default: 900,000 to leave room for the 1M window), trim the oldest messages (keeping system prompt + last N exchanges).

**Why:** Prevents cryptic API failures when conversations grow long. DeepSeek V4 has a 1M context window, but conversations with large file reads can fill it.

**Implementation:** In `Context.GetMessages()`, add a `MaxTokens` field. Count tokens with a rough estimator. Trim from the front if needed, logging a warning.

---

#### P1.3: System Prompt Injection
**Effort:** Trivial (~5 lines)  
**What:** Set a default system prompt in `cmd/agent/main.go` or via `GOPI_SYSTEM_PROMPT` env var. Something like: *"You are GoPi, a coding agent. You have tools (read, write, edit, bash) to help with software development. Think step by step. Use tools when needed. Be concise."*

**Why:** Without a system prompt, the model doesn't know it's a coding agent with tool access. Quality of initial tool use is lower.

**Implementation:** In `cmd/agent/main.go`, read `GOPI_SYSTEM_PROMPT` env var, fall back to a sensible default, set `ag.Ctx.SystemPrompt`.

---

### 🟢 P2 — Nice to Have

#### P2.1: Reasoning Effort Configuration
**Effort:** Small (~20 lines)  
**What:** Add a `reasoning_effort` parameter to `ChatRequest` (supported by DeepSeek V4: "minimal", "low", "medium", "high", "xhigh"). Map to DeepSeek's internal values ("high" → "max" for xhigh). Expose via `GOPI_REASONING_EFFORT` env var.

**Why:** Lets users trade reasoning depth for speed/cost. DeepSeek V4 Pro's reasoning mode is powerful but expensive.

---

#### P2.2: Finish Reason Handling
**Effort:** Small (~15 lines)  
**What:** After each API call, check `choice.FinishReason`. If `"length"`, warn the user that output was truncated. If `"content_filter"`, report the error.

**Why:** Silent truncation leads to broken tool calls and confusing behavior.

---

#### P2.3: Tool Argument Validation
**Effort:** Small (~30 lines)  
**What:** Before executing each tool call, validate required fields are present and non-empty. Return clear error messages for missing arguments instead of zero-value defaults.

**Why:** Better error messages help the model self-correct. Today, a missing `path` silently becomes `""` and produces a confusing error.

---

### 🔵 P3 — Future

#### P3.1: models.json Provider Configuration
**Effort:** Large (~200 lines)  
**What:** Support a `~/.gopi/models.json` config file similar to Pi's format. Parse providers, models, costs, and context windows. Allow switching models with `--model` flag.

**Why:** Enables multi-model support, model switching, and cleaner configuration. But adds significant complexity to the "zero deps, single binary" philosophy.

---

#### P3.2: Conversation History Persistence
**Effort:** Medium (~100 lines)  
**What:** Save/load conversation history to/from a JSON file. Enable resuming interrupted sessions.

**Why:** Useful for long multi-turn coding sessions. Pi supports this via its tree-structured conversations.

---

#### P3.3: Tool Execution Hooks (before/after)
**Effort:** Medium (~60 lines)  
**What:** Add optional `Before`/`After` hook functions to each tool. Enable confirmation prompts for destructive operations (write, edit, bash).

**Why:** Safety — prevents the model from accidentally deleting files or running dangerous commands. Pi supports this.

---

## 4. Summary Table

| # | Feature | Priority | Effort | Lines |
|---|---------|----------|--------|-------|
| P0.1 | Retry with backoff | Critical | Small | ~40 |
| P0.2 | Streaming agent loop | Critical | Medium | ~80 |
| P1.1 | Token/cost tracking | High | Small | ~50 |
| P1.2 | Context window awareness | High | Small | ~40 |
| P1.3 | System prompt injection | High | Trivial | ~5 |
| P2.1 | Reasoning effort config | Nice | Small | ~20 |
| P2.2 | Finish reason handling | Nice | Small | ~15 |
| P2.3 | Tool argument validation | Nice | Small | ~30 |
| P3.1 | models.json config | Future | Large | ~200 |
| P3.2 | History persistence | Future | Medium | ~100 |
| P3.3 | Tool hooks (before/after) | Future | Medium | ~60 |

**Total estimated effort for P0+P1: ~215 lines across 5 changes.** This gets GoPi to ~85% feature parity with Pi's core agent loop while maintaining its key advantage (static binary, zero deps, simple).

---

## 5. Key Insight

The DeepSeek doc page itself is a simple integration guide — it doesn't describe advanced features. The real learning comes from studying pi-mono's source code (792-line agent-loop.ts + AI compat layer). The most impactful finding is that **GoPi already has streaming infrastructure (SSE parser, ChatStream, StreamChunk types) — it's just not wired into the agent loop**. Enabling it gives immediate UX improvement with minimal code change.
