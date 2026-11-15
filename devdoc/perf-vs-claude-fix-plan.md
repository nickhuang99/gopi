<!-- doc-class: roadmap -->
# Performance Gap vs Claude — Root-Cause Analysis & Fix Plan

**Date:** 2026-08-22
**Status:** analysis + prioritized fix suggestions (no code changed — evaluation only)
**Question:** GoPi is 2-3x slower than Claude (Code CLI) on the same task / same
model / same endpoint. Where does the time go, and what fixes close the gap?

---

## 1. TL;DR

The gap is **not** model speed, not the API, not Go, and (since the
thinking-effort sweep) not the reasoning level. It is **harness-side, per-round
overhead**:

| Per-round cost | gopi (current defaults) | claude (Code CLI) |
|---|---|---|
| Median round time | **12.0s** (thinking=enabled, effort=minimal) | **3.9s** |
| Round-trips (W3-scale task) | 15 | ~10-15 |
| Thinking tokens/round (med) | 75 | ~43-266 |

Same backend (`deepseek-v4-flash`, same key/endpoint). 12.0/3.9 = **3.1x per
round** — that is the "two or three times slower" the user observes, and it
compounds over 15-45 rounds.

The dominant cost inside each gopi round is **LLM generation time** (streamed
output tokens), followed by **prefill of a growing context** on longer tasks.
Tool execution is µs-ms; the HTTP layer is already tuned (HTTP/2, keep-alive,
TCP_NODELAY, pooled transport — `internal/deepseek/client.go`).

Fix levers, ranked by (impact × effort):

1. **Bound the context aggressively** (biggest lever on long tasks) — lower the
   trim threshold from 90% → ~60%, compact earlier, and auto-trim old tool
   results. A 290K-token prefill alone measures ~16s (`client.go:24` comment);
   gopi currently lets history grow to **810K tokens** before trimming.
2. **Drop the mandatory full task-list JSON from every round** — emit only on
   change. Saves 150-500 output tokens/round of pure bookkeeping.
3. **Trust a single end_turn when the task list is all-completed** — removes
   the guaranteed +1 confirmation round per task (`maxConsecutiveEndTurnRounds`).
4. **Parallelize the read subset inside mixed batches** — today a
   `[read, read, bash]` round runs fully serially even though reads are
   side-effect free (the fix shape is already vetted in
   `devdoc/bug026-parallel-tools-evaluation.md`).
5. **Batch log flushing** — the raw log `fsync`s **every SSE chunk** and the
   session log fsyncs every event; per round that's dozens-hundreds of fsyncs.
6. **Cache the code_search staleness walk** — every `code_search` call
   re-walks the whole tree to find the newest source mtime.
7. (Future) adaptive reasoning effort; (future) subagent-style parallel
   exploration to keep the main context small.

Detailed analysis and concrete patches below.

---

## 2. Evidence: the gap is per-round, not round-count or model

Three measured head-to-heads exist in the repo (same `deepseek-v4-flash`, same
endpoint, comparable tasks):

| Run (devdoc) | gopi | claude | Gap driver |
|---|---|---|---|
| Task G, effort=max (`gopi-efficiency.md`) | 39-46 rounds, 1850-2123s | 66 turns, 258s (3.9s/turn) | **per-round latency 46-48s vs 3.9s** — the prompt fix moved nothing |
| W3 effort sweep (`thinking-effort-sweep-20260821.md`) | 15 rounds, 216s at enabled+minimal | — | effort=medium/max are 2x+ slower; minimal beats disabled on wall time |
| Race 2026-08-06 (`bash-efficiency.md`) | 61 rounds, 27 min | 82 rounds, 7 min | per-round info density + **context growth: 2.4K → 173K tokens, 27s/round late** |

Conclusions already documented: "the entire gap is LLM generation time"; "the
residual gap is structural, not thinking-related: the larger system prompt, the
mandatory [TASK_LIST_BEGIN] JSON block every round, and the bug022 double
end-turn confirmation." This doc turns that structural list into concrete,
code-grounded fixes.

Current defaults (post-sweep) are correct and should stay: thinking=enabled,
reasoning_effort=minimal, max_tokens=16384, cache_control on system+tools. The
remaining 3x is the structural layer below.

---

## 3. Root causes, ranked, with concrete fixes

### R1 — Context grows to 810K tokens before anything is trimmed  *(biggest lever on long tasks)*

**Where:** `internal/agent/context.go` — `DefaultContextWindow = 900000`,
`TrimThresholdRatio = 0.9`; `internal/agent/compact.go` —
`CompactionTargetRatio = 0.5`; trim is called per-round in
`loop.go:833` (`trimContext`).

**Mechanism:** history (including every tool result, kept verbatim) is only
trimmed/compacted when estimated tokens exceed **810K**. On a 30-45 round task
with 8-16KB bash outputs, context easily reaches 150-400K tokens by mid-task.
Every round then re-sends that history: cache-read of a 300K prefix + prefill of
the new tail. The measured prefill for ~290K tokens is **~16s** (`client.go:24`
comment), and the 2026-08-06 race hit 27s/round late. Claude Code keeps context
bounded by aggressive compaction, so its rounds never degrade.

**Fixes (cheap → expensive):**

1. **Lower `TrimThresholdRatio` to ~0.6** and `CompactionTargetRatio` to ~0.35.
   Compaction is LLM-assisted and already idempotent; more frequent, smaller
   compactions keep the cache prefix stable (DeepSeek's auto-cache is
   content-addressed — the summary only invalidates the tail it replaces).
   *~5 lines. Expected: late-task rounds stay ~5-10s instead of 15-30s.*
2. **Auto-trim old tool results.** Tool results are the growth driver (a 16KB
   bash output = ~4K tokens). Keep the last N (e.g. 8) tool results verbatim;
   collapse older ones into a one-line `[tool result summarized: bash — exit=0,
   16KB output]` stub. The model rarely re-reads old results, and when it does
   it can re-run the command. *~40 lines in context.go + a small
   `summarizeOldResults` pass inside `trimContext`.*
3. **Reduce `softOutputSize` from 16KB → 8KB** (`internal/tools/bash.go:24`).
   Halves the per-call context growth at the source; the soft-cap hint already
   tells the model to narrow commands. *1 line.*
4. When compaction is enabled, prefer it over plain trimming earlier than the
   current 90% trip point (currently `trimContext` only tries compaction when
   the 90% threshold already tripped).

### R2 — Mandatory full task-list JSON emitted every round

**Where:** `internal/agent/loop.go:128` — `taskManagementRules`:
"Every response MUST end with [TASK_LIST_BEGIN]...[TASK_LIST_END] containing
your full task list as JSON", with `task_id`, `summary`, `create_at`,
`status`.

**Mechanism:** every round — including pure tool rounds — the model re-emits
the *entire* list (it grows as tasks are added) plus invents `create_at`
timestamps, and spends thinking on maintaining it. Measured per-entry cost
~90-140 chars → 6-task list ≈ 150-300 output tokens/round of pure
bookkeeping. Generation at ~60 tok/s makes that 2-5s of the 12s round. The
harness **already tolerates omitted lists** (`ParseTaskList` returns nil,nil;
the loop treats nil as legal — see the comment at `loop.go:40-41`: "DeepSeek's
learned token-saving behavior of omitting the task list"), so the constraint
is prompt-only and can be relaxed without any harness change.

**Fixes:**

1. Reword rule 2 to: *"Emit the task list ONLY when it changed (new task /
   status change), at most every 3 rounds. Omit it on ordinary tool rounds."*
   The `todo` tool already gives the model a zero-composition way to see the
   tracked state (A3), so the JSON echo is redundant observability.
2. Drop the `create_at` requirement (or make it optional) — the model has to
   fabricate/remember unix timestamps; nothing consumes them.
3. Keep the "never delete task_ids" rule (it protects progress tracking) but
   note that a *single* omitted round must not be treated as deletion — the
   harness already keeps the last tracked state (`todoState.update` never wipes
   on nil).

*Prompt-only change. Expected: −150-300 output tokens/round → −1-3s/round and
less thinking spent on list bookkeeping.*

### R3 — Double end-turn confirmation adds a guaranteed wasted round per task

**Where:** `internal/agent/loop.go:69` (`maxConsecutiveEndTurnRounds = 2`) and
the exit block at `loop.go:958-976`: first no-tool round stores the answer,
injects `endTurnContinuationMessage`, and only the *second* consecutive no-tool
round exits.

**Mechanism:** every task pays +1 full round (~12s) for the "confirm you're
done" exchange, plus a context-growing injected message. The bug022 rationale
(DeepSeek ends mid-task) applies when tasks are still *progressing* — but the
normal completion path (all tasks completed, substantive final answer present)
does not need it.

**Fix:** exit on the **first** end_turn when the round's parsed task list is
all-completed **and** `content` is substantive; keep the confirmation nudge
only while any task is progressing (or no task list was ever established):

```go
if len(toolCalls) == 0 {
    consecutiveEndTurn++
    if tasks, _ := ParseTaskList(content); tasks != nil && AllCompleted(tasks) {
        return content, nil // normal completion: trust one end_turn (bug022 only guards mid-task exits)
    }
    if consecutiveEndTurn == 1 { pendingFinalContent = content }
    ... // existing nudge path for progressing tasks
}
```

*~10 lines. Expected: −1 round per task (−12s), and smaller final context.*

### R4 — Mixed tool batches execute fully serially even though reads are safe

**Where:** `internal/agent/loop.go:1335-1370` — `executeToolBatch` only takes
the parallel path when **every** call in the round is whitelisted
(`parallelSafeTools` = read/code_search/keyword_search/todo); a round with any
bash/write/edit falls back to the fully serial loop.

**Mechanism:** rounds like `[read, read, bash]` are common; the two reads (each
up to 30s worst case) run one-after-another instead of concurrently.

**Fix:** run the **read-only subset** concurrently inside mixed batches, then
execute side-effect tools serially, and append results in tool-call order. The
exact race-free patch shape (pre-sized indexed slice, no shared maps, single
ordered append pass) was already vetted live in
`devdoc/bug026-parallel-tools-evaluation.md` (Section 4) — including the
`go test -race` verification that catches the naive map-write variant.
*~30 lines. Expected: mixed rounds drop from `sum(reads) + rest` to
`max(reads) + rest` wall time.*

### R5 — Logging fsyncs every event (raw log fsyncs every SSE chunk)

**Where:** `internal/agent/session.go:98` (`sl.file.Sync()` after every log
event) and `internal/agent/rawlog.go:59-91` (every raw-log write — including
**each streamed SSE chunk** via `LogFunc` — calls `Sync()`).

**Mechanism:** a round with 2 tool calls emits ~1 (user) + 1 (assistant) + 1
(thinking) + 2 (tool_use) + 2 (tool_result) + 1 (request) session events, plus
request-body and every SSE chunk in the raw log. A 12s round streaming in
small deltas can mean 100-600 `fsync` syscalls. On SSD each is ~0.1-1ms, so
this is typically 0.05-0.5s/round — small, but pure waste and much worse on
HDD/network filesystems.

**Fix:** buffer with a mutex-protected writer and `Sync` only on round
boundaries and at close (crash-safety argument mostly applies to the session
log, and per-round granularity preserves it). Optionally default raw log to
off (it exists for debugging; `GOPI_RAW_LOG=OFF` already documented).
*~20 lines. Expected: −0.1-0.5s/round.*

### R6 — code_search re-walks the whole tree on every call

**Where:** `internal/tools/symbol.go:252-303` — `ensureTags` calls
`newestSourceFile`, a full `filepath.Walk` over the repo (skipping vendor/.git),
on **every** code_search call, even when the tags file is fresh.

**Mechanism:** the model is told to call `code_search` FIRST for every symbol
lookup (`toolRoutingRules`), so it's one of the most frequent tools. A walk of
a 10-50K-file tree is 50-500ms per call — added to the round every time.

**Fix:** cache the staleness verdict per root with a short TTL (e.g. 5-10s), or
stamp the newest source mtime into the tags file header when generating and
compare against `os.Stat` of the tags file + a cheap top-dir scan instead of a
full walk. *~15 lines.*

### R7 — Reasoning effort is static; make it adaptive (future)

**Where:** `internal/agent/loop.go:600-658` (NewAgent reads
`GOPI_REASONING_EFFORT`, default `minimal`).

The sweep proved minimal is the right default, but effort is fixed for the whole
run. Simple version: let the harness escalate to `low` after N consecutive
rounds with no task progress (the stalled-progress signal already exists in
`heartbeat.go` — currently **dead code**, never wired into `Run()`, worth
either wiring up or removing). Full version: dsh-style per-phase effort
(routine edit rounds at minimal, architecture rounds at low/medium).
*Medium effort; do after R1-R4, which are pure wins.*

### R8 — Subagent-style parallel exploration (future)

Claude Code keeps the main context small by delegating exploration to
subagents; gopi has one flat context, so every read/search result lands in the
main history (which compounds R1). A minimal version: a `explore(query)` tool
that runs a bounded read-only exploration (rg + a few reads) and returns a
compressed digest instead of raw bytes, so exploration context stays small.
*Large; the natural follow-up after R1-R2 prove the context-bound thesis.*

---

## 4. Expected impact (rough, model-dependent)

| Fix | Cost | Expected effect |
|---|---|---|
| R1 context bound (threshold + tool-result trim + 8KB soft cap) | small | late-task rounds 15-30s → 5-10s; on 30+ round tasks this is the single biggest wall-time win |
| R2 task list on change only | tiny (prompt) | −1-3s/round, less thinking overhead |
| R3 trust completed end_turn | tiny (~10 lines) | −1 round/task (~12s) |
| R4 parallel reads in mixed batches | small | −(N−1)×read latency on read-heavy mixed rounds |
| R5 batched log flushing | small | −0.1-0.5s/round |
| R6 search staleness cache | small | −50-500ms per code_search call |

Combined, these attack exactly the three structural items `gopi-efficiency.md`
and the sweep identified (task-list block, double end-turn, context growth) —
the remaining gap after the effort fix. Realistic target: bring the 3.1x
per-round gap toward ~1.5x on typical tasks and eliminate the late-task
blowup that makes long tasks 5-8x.

## 5. Measure first — reproducible head-to-head

The repo already has a valid methodology (`gopi-efficiency.md` §"Benchmark
integrity note"): same task text, three isolated clones (before / after /
claude), same model+endpoint, **pin the main repo HEAD so the Rule-6 clone
checks out the intended state**, and extract metrics from the session JSONL
(`~/.gopi/sessions/*.jsonl`: `request` events give per-round tokens; wall time
from `session_end`). Run the before/after at the **current defaults**
(enabled+minimal), not the old max-effort configuration, so the baseline
matches what the user actually experiences today. Suggested order:
R1 → R2+R3 (one round) → measure; R4+R5+R6 → measure; only then decide on
R7/R8.

## 6. Not the bottleneck (verified in code — do not chase)

- **HTTP/transport** — already tuned (HTTP/2, pool, TCP_NODELAY, 120s header
  timeout; `client.go`). `ResponseHeaderTimeout` is deliberately generous so a
  slow prefill doesn't abort+retry a whole request.
- **Streaming** — `chatOnceAttempt` streams; text/thinking display to stderr is
  unbuffered-by-design and does not gate the round.
- **Reasoning level** — minimal is right; medium/max multiply latency with zero
  quality gain (sweep).
- **max_tokens 16384** — converged (A4); 384K would allow pathological rounds.
- **System prompt / tool defs** — 4.5K + 5.3K chars (~2.4K tokens), cache-marked
  (A5), read once and cached; small relative to history growth (R1).
- **Heartbeat** — currently dead code in `Run()` (state machine exists, never
  wired); it adds no per-round cost today.

## 7. Files touched per fix

| Fix | Files |
|---|---|
| R1 | `internal/agent/context.go`, `internal/agent/compact.go`, `internal/tools/bash.go` |
| R2 | `internal/agent/loop.go` (prompt const only) |
| R3 | `internal/agent/loop.go` (exit block) |
| R4 | `internal/agent/loop.go` (+`internal/agent/loop_test.go`) |
| R5 | `internal/agent/session.go`, `internal/agent/rawlog.go` |
| R6 | `internal/tools/symbol.go` |
| R7/R8 | design first (devdoc) |
