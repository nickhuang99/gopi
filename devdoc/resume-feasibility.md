<!-- doc-class: normative -->
# Feasibility Study: Resume Functionality for GoPi

> **Type:** feasibility study (research only — no implementation in this change)
> **Date:** 2026-08-26
> **Scope:** GoPi (~/work/GoPi) — DeepSeek/Anthropic-compatible ReAct agent
> **Epic:** "almost all agent harness has a resume functionality just in case
> previous operation is interrupted. gopi currently lacks this important
> feature. read source and see if this can be easily added, if not, report a
> feasibility study report."
> **Method:** source-grounded — every claim below was verified against the
> current code (`master@ccdca44`) and real logs under `~/.gopi/`.

---

## 1. Verdict (TL;DR)

**Resume is NOT a trivially-easy add (not a flag-and-go change), but it IS
highly feasible and well-scoped.** Two implementation tiers exist:

| Tier | Source of truth | Effort | Prefix-cache hit | Verdict |
|---|---|---|---|---|
| **A — minimal resume** | session JSONL | ~1 day | ❌ (re-marshaled request ≠ original bytes) | Blocked for thinking-enabled sessions (data gap, §5) — "best-effort" only |
| **B — byte-exact resume** | raw log `→ REQUEST` | 2–4 days | ✅ (byte-splice, §6.4) | **Recommended** — matches the existing design doc `devdoc/context-snapshot-design.md` (research-only, written 2026-08-05, still unimplemented) |

**Key finding:** the data for resume **already exists on disk today**. Every
`→ REQUEST` line in `~/.gopi/raw/gopi-raw-*.log` is a complete, byte-exact
context checkpoint (system prompt + GOPI.md + full history + tool defs +
thinking/effort params). What is missing is *extraction + bootstrap*, not
*capture* — no new logging infrastructure is required. The bottleneck is a
real one though: the **session JSONL path cannot faithfully rebuild thinking
blocks** (signature not logged, §5.1), which forces the recommended design
onto the raw-log path.

---

## 2. Current State (verified in source)

### 2.1 Resume does not exist today

- `cmd/agent/main.go` (160 lines): subcommands are only `memory`, `web_search`,
  `log`. No `--resume`, no `checkpoint`, no `clone`. The main flow is
  `newAgent() → ag.Run(input)`, one-shot, then exit.
- `internal/`: `agent`, `config`, `deepseek`, `gopilog`, `tools` — **no
  `internal/checkpoint`** package (the design doc's proposal was never built).
- `internal/config/` catalog: no resume/checkpoint/snapshot knobs.
- The gap was already known: `devdoc/gopi_design_concern.txt` §5.4
  `[MEDIUM] No Conversation Persistence` — "Session logs are write-only JSONL
  files. There is no way to resume an interrupted session."

### 2.2 What a resume needs, and where it already lives

| Resume ingredient | Source today | Byte-exact? |
|---|---|---|
| System prompt (kernel) | `assembleSystemPrompt()` (loop.go:322) — deterministic; rebuildable | n/a (deterministic) |
| GOPI.md project context | re-read from cwd at `NewAgent` (loop.go:1053); **also embedded in every raw-log request** | ✅ in raw log |
| Full conversation (user/assistant/tool) | session JSONL events (§3.1) **and** raw-log `→ REQUEST` `messages` | ✅ only in raw log |
| Tool definitions | rebuilt by the loop each round (`claudeToolDefs`) | n/a |
| Thinking / reasoning_effort / stream params | **only** in raw-log request body | ✅ only in raw log |
| Token usage / cache stats per iteration | session `request` events (session.go `LogRequest`) | n/a |

### 2.3 The clean seam

`internal/agent/loop.go` builds **every** API request from the same place:

```go
req := deepseek.BuildClaudeRequest(
    a.Model,
    a.Ctx.GetMessages(),   // ← context.go:233 — the single injection point
    a.SystemPrompt,
    a.Ctx.ProjectContext,
    claudeToolDefs, ...)
```

`Context.GetMessages()` (context.go:233) prepends `SystemPrompt` + `ProjectContext`
to `Ctx.Messages` on the wire. Therefore **pre-seeding
`agent.Ctx.Messages`/`SystemPrompt`/`ProjectContext` before the loop starts
gives full control of resumed context** — the loop needs zero changes for
resume. The only loop changes needed are in `Run()`:

- `Run()` wraps the input in `[EPIC GOAL] ... [TASK_LIST_BEGIN]` framing
  (loop.go:1210) — a resume must append the continuation message **verbatim**,
  no framing.
- `Run()` fires `maybeInjectTaskStartSearch()` (loop.go:1223) — a resumed
  session must not re-inject the one-shot task-start checkpoint.
- A resume run should also skip the memory-search enforcement and start with a
  fresh session/raw log (new IDs are already generated per `NewAgent` — fine).

A `Resume(userInput string, history []deepseek.Message)` method that seeds
`Ctx` and then shares the existing loop body is the natural shape (~40 lines of
new code in loop.go, plus a session-JSONL or raw-log reader).

### 2.4 Raw log format today (updated vs. the 2026-08-05 design doc)

Verified in `internal/deepseek/claude_client.go` (the active Claude-compatible
path):

- `→ REQUEST\n` + `reqBody` — the **exact bytes** handed to the HTTP POST
  (claude_client.go:686). This is the full `ClaudeRequest`:
  `model`, `messages` (system + GOPI.md + history), `tools`, `stream`,
  `thinking{type,budget_tokens}`, `reasoning_effort`. **Byte-exact checkpoint.**
- `← CHUNK\n` + raw SSE event — **prefixes now preserved** (`event:`/`data:`
  lines kept, claude_client.go:744, comment: "prefixes preserved so the
  transcript is complete and replayable"). This is *better* than the old
  `client.go` `emitSSEEvent` (which stripped the envelope) and makes the chunk
  side multi-line — the design doc's single-line parser rule (§4.2) needs one
  tweak: chunk payloads may span multiple lines, so parsing must be
  marker-driven per entry, not "one payload line per marker".
- `← RESPONSE\n` + body for non-stream fallback (claude_client.go:637).
- Writes go through `RawLogger.LogFunc()` — buffered, fsync'd at round
  boundaries (rawlog.go, R5), crash-safe.

### 2.5 Real-log confirmation

A live raw log (this session) shows the last `→ REQUEST` ending with
`..."stream":true,"thinking":{"type":"enabled","budget_tokens":4096},
"reasoning_effort":"high"}` — the full context including wire params is there.

---

## 3. Tier A — Minimal resume from session JSONL (~1 day)

### 3.1 How it would work

1. `gopi --resume <session-id>` (or `--last`) → read
   `~/.gopi/sessions/<id>.jsonl`.
2. Rebuild `[]deepseek.Message` from events:
   - `user` → `{Role:"user", Content}`
   - `assistant` text → `{Role:"assistant", Content}`
   - `assistant` tool_use → `{Role:"assistant", ToolCalls:[{ID, Name,
     Arguments}]}` — note `LogAssistantToolUse` stores `"input": tc.Arguments`
     **as a string**, so arguments survive verbatim (session.go:187).
   - `tool_result` → `{Role:"user", ToolCallID, Content}`
   - `request`/`session_start`/`session_end` → metadata only, skipped.
3. Seed `Ctx.Messages` with the rebuilt history; system prompt + GOPI.md are
   rebuilt fresh by `NewAgent` (deterministic — correct iff the prompt,
   GOPI.md, model and tool set are unchanged since the interrupted run).
4. Call the shared loop via `Resume()`.

### 3.2 Why it is NOT the recommended path (verified blockers)

1. **Thinking-signature data gap (hard blocker on the Claude path).** The
   Anthropic-compatible endpoint **rejects assistant history that drops
   thinking blocks** (comment at context.go:120 "verified live"; echoed at
   claude_client.go:45). Replaying history requires each thinking block's
   `ThinkingSignature` — which the session JSONL **never logs**
   (`LogAssistantThinking` stores only the text, session.go:174). The signature
   exists only in the raw-log request/response bytes. So a session-JSONL resume
   of any thinking-enabled run is either rejected by the API or must strip
   thinking (unverified behavior — likely also rejected). **Tier A is only
   safe for thinking-disabled sessions.**
2. **No prefix-cache hit.** Rebuilt messages are re-marshaled
   (`ToolCall.Arguments` is a `map` → `json.Marshal` sorts keys, types.go:56),
   so the resumed request bytes differ from the interrupted session's — the
   DeepSeek automatic prefix cache (keyed on request bytes) misses, costing a
   full prefill (~16s for a 290K-token context, measured per context.go docs).
3. **Implicit assumptions** — model/tool-set/GOPI.md unchanged; silently wrong
   context if they drifted.

**Use Tier A only as a fallback / export path**, and document it as
"best-effort resume" exactly as the design doc §5 does.

---

## 4. Tier B — Byte-exact resume from raw log (recommended, 2–4 days)

This is the design doc's Phase 1–2 (`devdoc/context-snapshot-design.md`),
which was fully researched on 2026-08-05 against the then-current source. My
re-verification confirms it is still valid, with two small updates:

1. **Parser update (§2.4):** `← CHUNK` payloads are now multi-line
   (SSE framing preserved) — the parser must treat each marker as an entry
   boundary and take everything until the next marker, rather than "one line".
   `→ REQUEST` payloads remain single-line JSON.
2. **Claude-path specifics:** the resumed request must replay the
   **ClaudeRequest** shape (`BuildClaudeRequest`, claude_client.go:376:
   `system` array + `messages` + `tools` + `thinking` + `reasoning_effort`),
   and assistant history must carry `thinking_signature` — which the raw log
   provides for free since it is a byte-exact replay.

### 4.1 Components (from the design doc, costed)

| Component | What | Est. effort |
|---|---|---|
| `internal/checkpoint/parse.go` | marker-driven raw-log parser → `[]Entry{Direction, Payload, Offset}` (read-only, offline, unit-testable with fixtures) | 0.5 day |
| `internal/checkpoint/index.go` | checkpoint registry per raw log (sha256 dedupe, model, msg count, token stats) | 0.5 day |
| `gopi checkpoint list/export` | CLI to materialize any `→ REQUEST` into a snapshot file | 0.5 day |
| `internal/agent/resume.go` (`Resume()`) | seed `Ctx` from snapshot messages **verbatim**; append continuation message via byte-splice (§6.4 of the design doc: parse `messages` array bounds, append new message bytes — zero re-serialization drift); skip `[EPIC GOAL]` framing + taskStartSearch | 1 day |
| Cache-hit verification | first response logs `prompt_cache_hit_tokens ≈ parent's last prompt_tokens` (already tracked via `LogRequest`) | 0.5 day |
| Tests + docs | no-op clone invariant (`replay(snapshot) == snapshot` bytes), httptest SSE integration, report | 0.5–1 day |

**Total: ~3–4 days** for a production-quality Tier B with tests. A
thinking-disabled-only v1 (skip the signature complexity) trims ~1 day.

### 4.2 Why Tier B is the right call

- **Resume = clone with a continuation prompt** (design doc §4.5): replay the
  last request bytes, append `[system] Continue from where you left off...`
  — the model resumes with *identical* context, and the request hits the
  prefix cache (near-free within the TTL window).
- Rollback-style resume: any checkpoint N < last is a valid resume point.
- Bonus (free from the same machinery): detached sub-task mode
  (divide-and-conquer, design doc §4.7) — parent runs
  `bash: gopi clone --from ... --detached --out F`, reads the result. **No new
  top-level tool** (GoPi Rule 3).

---

## 5. Risks & blockers (ranked)

| # | Risk | Evidence | Mitigation |
|---|---|---|---|
| 1 | **Thinking-signature gap** — session JSONL cannot rebuild Claude-path assistant history | context.go:120 comment "endpoint rejects history that drops thinking blocks (verified live)"; `LogAssistantThinking` stores no signature | Tier B (raw log replay carries signatures). Tier A restricted to thinking-disabled sessions. |
| 2 | No prefix-cache hit on rebuilt requests (cost + latency) | `ToolCall.MarshalJSON` re-marshals `map` args (types.go:56) | Byte-splice (design doc §4.6) + no-op clone invariant test |
| 3 | `Run()` framing / taskStartSearch would corrupt resumed context | loop.go:1210, 1133 | `Resume()` variant that shares the loop body but skips both |
| 4 | Chunk parser multi-line (design doc assumed one line) | claude_client.go:744 preserves SSE framing | Marker-driven entry parsing (§4.1) |
| 5 | Model/settings drift between sessions | — | Validate snapshot model vs `ResolveModel`; warn (partial cache-hit) |
| 6 | Session logged a SIGKILL-truncated tail | SessionLogger buffers ≤64KB (R5); raw log fsync'd per round | Resume from the **last raw-log request** (always on disk) — never from the session tail |
| 7 | Trimmed/compacted history | `TrimToFit` drops oldest messages | Correct semantics: snapshot = what the API actually saw (design doc §6 risk 3) |

---

## 6. Recommendation

1. **Do not attempt Tier A as the primary feature** — the thinking-signature
   gap makes it wrong for the default (thinking-enabled) configuration.
2. **Implement Tier B, Phase 1 first** (parser + index + `checkpoint
   list/export`): pure read-only code, fully unit-testable offline, zero risk
   to the hot paths (the deepseek client and ReAct loop stay untouched — this
   is the design doc's own Phase-1 acceptance criterion).
3. **Phase 2** = `Resume()` + cache-hit verification against the live API.
4. Ship the existing `devdoc/context-snapshot-design.md` roadmap unchanged;
   this study only updates §2.2 (chunk framing) and re-confirms feasibility
   against `master@ccdca44`.

**Bottom line for the Epic:** resume *cannot* be "easily added" as a small
flag today — the clean seam exists (`Context.GetMessages()` + a `Resume()`
variant), but a correct implementation must go through the raw-log byte-exact
path (~3–4 days, no capture changes needed, design already written). That is a
normal, well-scoped feature task, not a research problem.

---

## 7. Verification plan (for the future implementation)

- **Unit:** raw-log parser + registry with `t.TempDir()` fixtures (pattern:
  `internal/agent/rawlog_test.go`); no-op clone invariant
  `replay(export(snapshot)) == snapshot` bytes; byte-splice appends exactly
  one message.
- **Integration:** httptest SSE server asserting the resumed request bytes =
  snapshot bytes + appended continuation message (pattern:
  `internal/deepseek/sse_integration_test.go`).
- **Live (one-time):** resume a real interrupted session; confirm session
  `request` event 0 reports `cache_hit ≈ parent's last prompt_tokens`.
- **Regression:** `go build` + `go test ./...` green; Phase 1 touches no hot
  path.

## 8. Key file map (verified paths)

| Path | Role in resume |
|---|---|
| `internal/agent/rawlog.go` | RawLogger — marker + payload writes, fsync'd per round |
| `internal/deepseek/claude_client.go` | `→ REQUEST`/`← CHUNK` callbacks (686/744); `BuildClaudeRequest` (376); `ClaudeRequest` shape (233) |
| `internal/agent/context.go` | `Context.Messages/SystemPrompt/ProjectContext`; `GetMessages()` (233) — the injection seam |
| `internal/agent/loop.go` | `NewAgent` (900) config assembly; `Run` (1115) framing + taskStartSearch to skip; request build (1267) |
| `internal/agent/session.go` | SessionLogger events (user/assistant/tool_result/request) — Tier-A source, Tier-B metadata |
| `internal/deepseek/types.go` | `Message` (11) / `ToolCall` (34) — reconstruction target types |
| `cmd/agent/main.go` | CLI — add `checkpoint`/`--resume` flag family (manual os.Args style) |
| `devdoc/context-snapshot-design.md` | Existing full design + roadmap (supersedes this study's component detail) |
