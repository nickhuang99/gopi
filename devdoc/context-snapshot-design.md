<!-- doc-class: historical -->
# Context Snapshot & Clone/Resume — Study + Design Roadmap

> **Type:** research + design (no implementation in this change)
> **Date:** 2026-08-05
> **Scope:** GoPi (~/work/GoPi) — DeepSeek-backed ReAct agent
> **Goal:** enable `gopi` to snapshot its exact LLM context at any point and
> clone/resume a new instance with byte-identical context, so the new instance
> (a) hits DeepSeek's automatic prefix cache (near-free TTL window), and
> (b) can run sub-tasks with the parent's exact context without polluting the
> parent's context (divide & conquer, avoid context explosion).

---

## 1. Executive Summary

**Q1 — Does GoPi modify the raw HTTP content in the raw log?**
Partially. The **request bodies are byte-identical** to what was sent on the
wire (the exact same `[]byte` buffer). The **SSE chunk payloads are modified**:
the `data:` envelope is stripped, whitespace is trimmed, event framing is
removed, and `[DONE]`/comments are dropped. This is exactly the "data tag
modified for easy reading" suspicion from the task — **but it does not matter
for cache-hit**, because DeepSeek's automatic context cache keys on the
*request* prefix (the prompt), not on responses. The request half of the raw
log is pristine, which is precisely what a clone needs.

**Q2 — Is a context-snapshot feature feasible?**
Yes — and the data already exists. Every `→ REQUEST` line in
`~/.gopi/raw/gopi-raw-*.log` is a **complete, byte-exact context checkpoint**:
system prompt + GOPI.md + full message history + tool definitions +
`stream`/`reasoning_effort`/`thinking` parameters, exactly as serialized and
sent to DeepSeek. What is missing is (a) an index to locate checkpoints,
(b) an extractor to materialize them into standalone snapshot files, (c) a
clone/resume mode that bootstraps an agent from a snapshot instead of from
scratch, and (d) a detached sub-task mode for divide-and-conquer.

**Recommendation:** raw log = source of truth for clone (byte-exact).
Session JSONL = human-readable fallback only (NOT byte-safe for clone, §5).

---

## 2. How the Raw Log Works Today (verified against source + real logs)

### 2.1 Writers

| Marker | Where | Payload source | Modified? |
|---|---|---|---|
| `→ REQUEST\n` | `client.go` `Chat()` / `ChatStream()` | `reqBody` — the exact bytes given to `bytes.NewReader(reqBody)` for the HTTP POST | **NO — byte-identical to wire** |
| `← RESPONSE\n` | `client.go` `Chat()` (non-stream fallback) | `io.ReadAll(resp.Body)` | **NO — byte-identical to wire** |
| `← CHUNK\n` | `client.go` `emitSSEEvent()` | extracted `data:` payload | **YES — SSE envelope stripped** |

All writes go through `RawLogger.LogFunc()` which appends `direction + data + "\n"`
and `fsync`s after every write (thread-safe, crash-safe).

### 2.2 The chunk modification, exactly

In `internal/deepseek/client.go`, `emitSSEEvent()`:

```go
for _, eventLine := range bytes.Split(event, []byte("\n")) {
    if bytes.HasPrefix(eventLine, []byte(":")) { continue }          // comments dropped
    if bytes.HasPrefix(eventLine, []byte("data:")) {
        d := bytes.TrimPrefix(eventLine, []byte("data:"))
        d = bytes.TrimSpace(d)                                        // ← trim
        if string(d) == "[DONE]" { return }                          // ← sentinel dropped
        dataPayload = append(dataPayload, d...)                      // ← joined w/o separator
    }
}
...
if c.RawLog != nil { c.RawLog("← CHUNK\n", dataPayload) }
```

So for a wire event:

```
data: {"id":"...","choices":[...]}

```

the log records only:

```
← CHUNK
{"id":"...","choices":[...]}
```

Differences vs. the original HTTP stream:
1. `data: ` prefix removed (readability — the suspected modification).
2. `bytes.TrimSpace` applied (removes the single protocol space after `data:`).
3. `\n\n` event boundaries and `\n` separators between multiple `data:` lines removed.
4. `: ` comments and `data: [DONE]` never logged.
5. Empty payloads never logged.
6. Direction markers (`→ REQUEST\n`, `← CHUNK\n`, `← RESPONSE\n`) are GoPi-added, not part of HTTP.

**Practical effect on payload bytes:** DeepSeek emits single-line
`data: {json}` events, so the extracted JSON payload is byte-identical to the
JSON on the wire; only the SSE envelope is lost. The log is therefore **not a
byte-for-byte HTTP transcript**, but it IS a byte-exact record of every
request body and every chunk's JSON payload.

### 2.3 Verified against real logs (`~/.gopi/raw/`)

Line-level scan of six real logs (`gopi-raw-2026080*.log`):

```
gopi-raw-20260803-184426.log  (32 requests,  12738 chunks)
gopi-raw-20260803-190611.log  (24 requests,  15977 chunks)
gopi-raw-20260803-192027.log  (13 requests,  13636 chunks)
gopi-raw-20260803-193307.log  (21 requests,   6838 chunks)
gopi-raw-20260803-195322.log  (46 requests,  25597 chunks)
gopi-raw-20260803-201310.log  (59 requests,  21274 chunks)
```

- Every marker is followed by exactly **one** payload line (compact
  `json.Marshal` output is single-line; JSON string escapes keep it that way).
- Marker/payload pairing is exact: #body-lines == #requests + #chunks + #responses.
- In `gopi-raw-20260803-235633.log`: 46 request bodies, **46 unique SHA-256**
  (no duplicate bodies; retries either did not occur or the same body was sent
  — dedupe handling is still designed in §6.4 for safety).
- Request bodies contain `model`, `messages` (system + GOPI.md + history),
  `tools` (4 defs), `stream:true`, `reasoning_effort`, `thinking` — i.e. the
  **full context**.

### 2.4 Why the request side is what matters for cache-hit

DeepSeek's context cache is an **automatic prefix cache**: a subsequent request
whose prompt (messages array) starts with previously-seen bytes/tokens gets a
cache hit (`usage.prompt_cache_hit_tokens`). It is keyed on the request, never
on responses. Therefore:

- The `← CHUNK` modifications are **irrelevant** to cache-hit.
- The `→ REQUEST` integrity is **critical** — and it is already pristine.
- A clone that re-sends the exact parent request bytes (with only a new message
  appended at the end of `messages`) reproduces the exact cached prefix →
  `prompt_cache_hit_tokens ≈ parent's last request size`, at cache-hit pricing.

---

## 3. Data Sources Compared for Context Reconstruction

### 3.1 Raw log (`~/.gopi/raw/gopi-raw-*.log`) — ✅ SOURCE OF TRUTH

| What a checkpoint needs | In raw log? |
|---|---|
| System prompt (kernel) | ✅ inside each `→ REQUEST` body (`messages[0]`) |
| GOPI.md project context | ✅ `messages[1]` (exact bytes at request time) |
| Full conversation (user/assistant/tool) | ✅ `messages[2:]` |
| Tool definitions | ✅ `tools` field |
| Thinking / reasoning_effort / stream | ✅ top-level fields |
| Model | ✅ `model` field |
| Token usage / cache stats at that point | ✅ next `request` event in session log; `usage` in last stream chunk |

**Byte-exactness:** guaranteed — the log line is the literal wire buffer.

### 3.2 Session JSONL (`~/.gopi/sessions/<id>.jsonl`) — ⚠️ NOT byte-safe

- Stores `user` / `assistant` (text, thinking, tool_use) / `tool_result` /
  `request` (with `cache_hit`/`cache_miss` per iteration) / `session_end`.
- Missing: GOPI.md content (only `workdir`), tool definitions, `thinking` and
  `reasoning_effort` params, the exact system prompt.
- Re-serialization is lossy: `tool_use.input` is re-marshalled from
  `map[string]interface{}` (Go sorts map keys), which is **not guaranteed
  byte-identical** to the model's original `arguments` string — a byte change
  anywhere in the prompt breaks the cached prefix (cache-hit is all-or-nothing
  for the prefix; any divergence resets it).
- **Use for:** human-readable history export, debugging, cross-referencing
  iterations with token usage. **Do NOT use for clone.**

### 3.3 In-memory `Context` (`internal/agent/context.go`)

- Has everything structurally (Messages, SystemPrompt, ProjectContext) but is
  gone when the process exits. A checkpoint file is the persistence layer for
  it. Note `GetMessages()` injects SystemPrompt/ProjectContext on the wire —
  a snapshot must capture **post-injection** bytes (which the raw log does).

---

## 4. Proposed Design

### 4.1 Concepts

```
checkpoint  = one → REQUEST body (exact wire bytes) + manifest metadata
snapshot    = materialized checkpoint: request.json + snapshot.json manifest
registry    = per-raw-log index file mapping checkpoint# → byte offset, sha256, stats
clone       = new agent bootstrapped from a snapshot's messages (verbatim),
              then continuing the ReAct loop with a new user message appended
detached    = clone variant that returns only its final result to the parent
              (file/stdout), never touching the parent's context
```

### 4.2 Checkpoint extraction (offline, no network)

New package `internal/checkpoint` (pattern: small, focused, like `internal/tools`):

```go
// Parse raw log into ordered entries. Marker-driven, line-based.
// Each entry: Direction + exact payload bytes + byte offset + line number.
type Entry struct {
    Direction string // "→ REQUEST", "← CHUNK", "← RESPONSE"
    Payload   []byte // exact bytes as logged (request = byte-exact wire body)
    Offset    int64  // byte offset of payload in the raw log
    Line      int64
}
func Parse(r io.Reader) ([]Entry, error)

// Build the checkpoint registry from a raw log. A checkpoint exists for every
// "→ REQUEST" entry; retries with identical sha256 are deduped (keep last).
type Checkpoint struct {
    Index         int    // 0-based, matches iteration order
    RawLog        string // source file (stem)
    SHA256        string // hex of payload
    Model         string
    MessageCount  int
    EstimatedTokens int
    PromptTokens  int    // from following usage, when available
    CacheHit      int
    CacheMiss     int
    Timestamp     string // from raw log mtime approx; precise ts from session log
}
func Index(entries []Entry) ([]Checkpoint, error)
```

**Parse rules (verified against real logs):**
1. Line `→ REQUEST\n` → next line is the full request body (single-line JSON).
2. Line `← CHUNK\n` / `← RESPONSE\n` → next line is the payload.
3. Any other line = corruption → fail loudly (log was written under mutex +
   fsync, so this should never happen; failing is better than silent truncation).
4. `[DONE]`/comments are absent by construction (see §2.2).

**Registry storage:** `~/.gopi/checkpoints/<rawlog-stem>.json`
- one JSON object per checkpoint, or a JSON array; regenerate-able from the raw
  log any time (idempotent), so it is a cache, never the source of truth.

### 4.3 Snapshot materialization

```
gopi checkpoint export --raw ~/.gopi/raw/gopi-raw-20260803-195322.log \
    --at 23 --out /tmp/ctx-23.json
```

`ctx-23.json` = the exact `→ REQUEST` payload (raw bytes). Optional
`--manifest` sidecar (`ctx-23.manifest.json`) with the Checkpoint metadata.
Export is just `Index() + offset lookup + copy bytes` — trivially testable
without a network.

### 4.4 Clone mode (agent bootstrap from snapshot)

New agent construction path (do NOT reuse `NewAgent`'s context assembly):

```go
// internal/agent/bootstrap.go (new)
// Build an Agent whose Context is pre-seeded from a snapshot request body.
// CRITICAL: messages are taken VERBATIM from the snapshot. The clone must NOT
// re-read GOPI.md from cwd and must NOT rebuild the system prompt — any
// byte difference from the parent's wire bytes kills the cached prefix.
func AgentFromSnapshot(snap *Snapshot, newPrompt string, cfg CloneConfig) (*Agent, error)
```

Clone semantics:
1. Parse snapshot request JSON → `deepseek.ChatRequest` (or keep raw bytes
   and only splice the new message in — see §4.6).
2. Validate: snapshot model == resolved model (`ResolveModel`); warn and
   proceed otherwise (cache-hit will be partial/none).
3. Preserve `tools`, `thinking`, `reasoning_effort`, `stream` from snapshot.
4. Append the new user message to `messages` (the cached prefix = everything
   before it, unchanged).
5. Run the existing ReAct loop (`Agent.Run` variant that skips the
   `[EPIC GOAL]` framing when cloning — the clone's first message is the
   sub-task prompt verbatim; framing would change bytes only *after* the
   cached prefix, so it is harmless for cache-hit, but verbatim is cleaner).

**Cache-hit verification (built into clone):** after the first API response,
log the delta `prompt_cache_hit_tokens` (already tracked via `CostTracker` and
session `request` events). Expected: `hit ≈ parent's last prompt_tokens`,
`miss ≈ new message tokens + small overhead`. Emit a one-line verdict to
stderr: `[gopi] clone cache-hit: 98765/98901 tokens (99.9%)`.

### 4.5 Resume semantics

- **Resume = clone with continuation prompt.** Replaying the last request
  verbatim would just re-ask the same question; instead `gopi clone --from
  ctx-N.json --prompt "[system] Continue from where you left off..."` appends
  a continuation message. Context is identical; behavior is a natural
  continuation.
- Checkpoints before the final one allow **rollback-style resume** (e.g. the
  agent went down a wrong path after iteration 7 — resume from 7 with a
  corrective instruction).

### 4.6 Byte-splicing vs. re-marshal (important detail)

Two ways to build the clone's first request:

- **A. Re-marshal** (`json.Marshal` of a `ChatRequest` built from the snapshot):
  deterministic (Go marshals struct fields in order, sorts map keys) and equal
  to the parent's bytes *if* the snapshot was produced by the same struct
  versions. Safe today, but fragile across struct changes.
- **B. Byte splice** (recommended): parse only the `messages` array boundaries
  from the raw snapshot bytes, append the new message's JSON bytes, re-emit
  the request verbatim with the splice. Zero risk of incidental re-serialization
  drift; the prefix is the parent's literal bytes.

Design choice: **B as primary** (byte-preserving), with A as a fallback used
only when the snapshot lacks valid JSON structure. A unit test must assert
`replay(snapshot) == snapshot` byte-for-byte (the "no-op clone" invariant).

### 4.7 Detached sub-task mode (divide & conquer)

```
gopi clone --from ctx-23.json --detached --out /tmp/subtask-result.txt \
    --prompt "<sub-task spec>"
```

- Runs a full agent session with the cloned context.
- On completion, writes **only** the final answer to `--out` (and the normal
  session/raw logs for the sub-task session itself).
- Parent never receives the sub-task's intermediate history; the parent's
  context is not polluted.
- Parent integrates the result as a normal tool result. **No new top-level
  tool** (Rule 3): the parent just runs `bash: gopi clone --detached ...`
  and `read`s the result file — exactly how GoPi already shells out.
- Guardrails: `--timeout` (GOPI_TIMEOUT_SEC already exists), result size cap
  (mirror the 100KB bash output cap), and a `--max-iterations` cap for
  sub-tasks so a runaway sub-task cannot burn unbounded budget.

### 4.8 CLI surface (follows existing main.go flag style)

`cmd/agent/main.go` currently does manual `os.Args` scanning with `--settings`.
Extend it with the same manual style (no new flag framework):

```
gopi checkpoint list   <rawlog>                 # iterations, sha, tokens
gopi checkpoint export <rawlog> --at N [--out F]
gopi clone --from <snapshot.json|rawlog@N> [--prompt "..."] [--detached] [--out F]
gopi clone --resume --from <snapshot.json>      # = clone with continuation prompt
```

New env (same single-source-of-truth pattern as GOPI_TIMEOUT_SEC):
`GOPI_CHECKPOINT_DIR` (default `~/.gopi/checkpoints/`).

### 4.9 Snapshot file layout (proposal)

```
~/.gopi/checkpoints/
├── <rawlog-stem>.idx.json        # registry (regenerable)
└── snapshots/
    ├── <rawlog-stem>-iter-023.request.json   # byte-exact request
    └── <rawlog-stem>-iter-023.manifest.json  # metadata (0600)
```

Permissions: `0600` for all snapshot files (contain full conversations).

---

## 5. Session-Log Fallback (non-goal for v1)

A `gopi export --session <id>.jsonl` command that renders a readable transcript
is nice-to-have, but the design explicitly **forbids cloning from the session
log**: missing GOPI.md/tools/thinking params + re-serialized tool args make
byte-exact replay impossible. If ever needed, reconstruct `messages` from
`user`/`assistant`/`tool_result` events and re-marshal — and expect cache
misses (document as "best-effort resume").

---

## 6. Edge Cases & Risks

| # | Risk | Mitigation |
|---|---|---|
| 1 | GOPI.md changes between parent and clone | Clone uses snapshot messages verbatim; never re-reads GOPI.md. Snapshot embeds the exact system messages. |
| 2 | Retry duplicates in raw log | Registry dedupes by SHA-256 (keep last); observed 0 dupes in real logs but cheap insurance. |
| 3 | `TrimToFit` trimmed history | Correct semantics: snapshot = what the API actually saw, not the full chat. Document that resumed context may be trimmed. |
| 4 | `finish_reason=length` re-issue user messages | Captured naturally in the request body; resume inherits the corrected context. |
| 5 | Model/settings mismatch at clone time | Validate snapshot model vs resolved model; warn on mismatch (partial cache-hit). |
| 6 | Struct drift breaking re-marshal | Byte-splice (§4.6) + no-op clone invariant test. |
| 7 | Snapshot corrupted/truncated | SHA-256 in manifest; parser fails loudly on malformed marker structure. |
| 8 | Sub-task runaway | `--max-iterations` + existing GOPI_TIMEOUT_SEC + result size cap. |
| 9 | Privacy | 0600 files; snapshots contain full conversation history; add `gopi checkpoint rm`. |
| 10 | Cache TTL expiry | Cache is TTL-bounded; clone is most valuable within the TTL window. Report hit ratio per clone so the user sees when it degrades. |
| 11 | Response-side byte fidelity (future "wire transcript" need) | Separate low-priority change: log SSE envelope as-is (`data:` + `\n\n`) under a new marker; does not affect cache-hit design. |

---

## 7. Roadmap

### Phase 1 (P0) — Checkpoint extraction & registry *(offline, no network)*
**Files:** `internal/checkpoint/parse.go`, `index.go`, `*_test.go`;
CLI wiring in `cmd/agent/main.go`.
- [ ] `checkpoint.Parse` (marker-driven line parser, §4.2) with synthesized-log
      tests (`t.TempDir()` + fixture files — pattern: `rawlog_test.go`).
- [ ] `checkpoint.Index` — registry with dedupe; golden-file tests on a
      captured real log snippet.
- [ ] `gopi checkpoint list/export` commands.
- [ ] No-op clone invariant test: `replay(export(snapshot)) == snapshot` bytes.
- **Accept:** `go test ./...` green; export of a real raw log produces a valid
  request JSON identical to the logged body; manifest sha256 matches.

### Phase 2 (P0) — Clone & cache-hit verification *(needs API)*
**Files:** `internal/agent/bootstrap.go`, `internal/agent/clone_test.go`,
`cmd/agent/main.go`.
- [ ] `AgentFromSnapshot` (§4.4) with byte-splice request builder (§4.6).
- [ ] `gopi clone --from ... --prompt ...`; first-response cache-hit report.
- [ ] Integration test with `httptest` SSE server (pattern:
      `internal/deepseek/sse_integration_test.go`) asserting the replayed
      request bytes equal the snapshot bytes + appended message.
- **Accept:** clone of a real session reports
  `hit ≈ parent prompt_tokens, miss ≈ new message tokens`; session log of the
  clone shows `cache_hit` on iteration 0.

### Phase 3 (P1) — Checkpoint management
- [ ] Registry lifecycle: `list` by session/date, `rm`, GC of old snapshots.
- [ ] Cross-reference session JSONL ↔ raw log (iteration alignment via
      `request` events vs REQUEST order; timestamps).
- [ ] Auto-checkpoint on SIGINT/SIGTERM (grab latest request before exit) so
      interrupted runs are resumable without a full re-run.

### Phase 4 (P1) — Detached sub-task mode (divide & conquer)
- [ ] `--detached --out F --max-iterations N`; result-size cap.
- [ ] Parent-side usage pattern documented (bash + read; no new tool).
- [ ] Demo script: parent clones context, forks 3 sub-tasks with identical
      context + different prompts, merges results — parent context grows by
      only the 3 result strings.

### Phase 5 (P2) — Analytics & ergonomics
- [ ] `gopi checkpoint stats` — per-session cache-hit ratio over time
      (uses `request` events: `cache_hit/(cache_hit+cache_miss)`).
- [ ] Snapshot diff tool (show byte-delta between checkpoint N and N+1 —
      i.e., what the conversation gained).
- [ ] `export --session` readable transcript (session-log path, explicitly
      marked "not byte-safe for clone").

---

## 8. Verification Plan (per phase)

- **Unit:** parser/index/splice tests with fixture logs and `t.TempDir()`
  (existing rawlog test style); fuzz the parser against truncated logs.
- **Integration:** httptest SSE server asserting exact request bytes;
  replay-then-append byte equality.
- **Live (manual, one-time):** clone a real recent session, confirm
  `prompt_cache_hit_tokens` on the clone's first request ≈ parent's last
  `prompt_tokens` (session log `request` events make this measurable).
- **Regression:** full `go test ./...` before each commit; no changes to
  `ticker/collect/orphan`-style hot paths — specifically, the deepseek client
  and ReAct loop remain untouched in Phase 1 (extraction is read-only).

## 9. Key File Map (for implementers)

| Path | Role |
|---|---|
| `internal/agent/rawlog.go` | RawLogger — writes `direction + payload + "\n"`, fsync per write |
| `internal/deepseek/client.go` | where RawLog callbacks fire (`Chat`, `ChatStream`, `emitSSEEvent`) |
| `internal/agent/context.go` | Context.Messages / GetMessages / TrimToFit — what a snapshot represents on the wire |
| `internal/agent/loop.go` | Agent.Run ReAct loop; `NewAgent` env handling to mirror for CloneConfig |
| `internal/agent/session.go` | SessionLogger JSONL events (`request` carries cache_hit/miss per iteration) |
| `internal/deepseek/types.go` | ChatRequest/Message/ToolDef — wire format the snapshot contains |
| `internal/deepseek/config.go` | ResolveModel / SettingsPath — clone model resolution |
| `cmd/agent/main.go` | CLI — extend with `checkpoint`/`clone` flag family (manual os.Args style) |
| `internal/agent/rawlog_test.go` | test style: `t.TempDir()` + `GOPI_RAW_LOG_DIR` |
| `internal/deepseek/sse_integration_test.go` | httptest SSE integration pattern |
