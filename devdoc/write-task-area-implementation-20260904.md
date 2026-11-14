<!-- doc-class: historical -->
# write_task_area — Data-Backed In-Session Subtask Commit + Working-Memory Hint

Date: 2026-09-04 · Branch: `feat/write-task-area`

## What & Why

The memory-aware-agent POC (devdoc/memory-aware-agent-poc-20260904.md) showed a
submit tool decisively beats free-text on multi-item chains (9/9 vs 0/3 on a
5-item chain): a *submit* forces one-item-at-a-time execution, because the
item's completion is a **recorded, parseable fact**, not prose the model may
or may not conclude. This ticket ships the data-backed `write_task_area`
**in-session** tool (the analog of the task-ledger writeback at task level,
but scoped to ONE gopi run) plus a soft working-memory-layout hint.

## Completed

### 1. Tool: `write_task_area` (session-internal subtask commit)
- **Definition** in `internal/agent/loop.go` `claudeToolDefs` (new tool #21-23
  — header text updated 20→23).
- **Schema**: `task_id` (string), `status` (`"completed"` only),
  `conclusion` (REQUIRED — compact final result/decision/key facts),
  `compressed_analysis` (optional — omit = discard analysis).
- **Semantics** (from the POC):
  - ONE item per call — never batch several items' completions into one call,
    never free-text "all done" prose (enforced by conclusion-required).
  - A successful commit = the item's working frame is COMMITTED: the framework
    records the conclusion (append-only, session-scoped) and may
    drop/trim the completed working region next round.
  - Do NOT re-derive a committed item's analysis — refer to its recorded
    conclusion (carried forward as authoritative context).
- **Distinct from** the cross-session task ledger (`task_submit` /
  `task_evaluate` / worker claim-run-writeback). `write_task_area` never
  touches the tasks table.

### 2. Persistence — `internal/tools/writetaskarea.go`
- Append-only `session_conclusions` table in the shared gopi memory DB
  (same DB + heal path as the tasks ledger; created idempotently via
  `ensureSessionConclusionsSchema` — never mutates a foreign DB).
- Columns: session_id / task_id / status(completed) / conclusion / analysis /
  created_at. Scoped by session_id; reads in creation order.
- `WriteTaskArea(opts)` validates (session/task non-empty, status=completed,
  conclusion non-empty) and inserts. `SessionConclusions(sessionID)` returns
  the machine-trusted completed list; absent table/DB reads as empty (a
  read-only carry-forward must never fail a run).

### 3. Framework signals (Agent loop) — `internal/agent/loop.go`
- After a successful commit: `taskAreaActive` + `taskAreaDirty` latched.
- Hook after each tool round (adjacent to the dead-end nudge):
  `injectTaskAreaConclusions()` then `trimCommittedTaskArea()`.
- **Carry-forward** (POC H3): inject the full recorded-conclusion set as ONE
  authoritative user message (`[WRITE_TASK_AREA — COMMITTED CONCLUSIONS
  (authoritative)]` marker + one `- task_id: conclusion` line each) — placed
  BEFORE any trim, so a referenced conclusion can never be implied-but-missing
  (H4 safety red line: missing-but-referenced → fabricated facts).
- **Committed-region trim**: drop messages in `[1, commitRoundIdx)` — the
  item's analysis region (and all earlier committed items) — keeping the epic
  briefing (index 0) and the commit round + live tail untouched. Orphan-safe
  by construction (kept head is an assistant message carrying the commit).
- Validation case in `validateToolArgs`; dispatch case in
  `executeToolDefault` (NOT parallel-safe — serial, like other state-mutating
  tools).

### 4. Memlayout soft hint (system prompt)
- New `[WORKING MEMORY LAYOUT — soft]` section (Order 62): read-only anchors
  (SYSTEM + the goal briefing) vs working area (analysis/tool rounds); analysis
  ≠ conclusion; keep analysis COMPACT; item converges → write_task_area a
  CONCLUSION; committed — do NOT re-derive; keep memory bounded.
  NOTE: must not literally contain "[EPIC GOAL]" — ops turns send the full
  system prompt and TestChatOps_FullToolsetAndPrompt bans task scaffolding
  from ops bodies (reworded to "the goal briefing").
- POC V2: the soft hint was a free win (−30% length on simple tasks,
  90→100 long-chain completion). Kept as soft/no-per-round-offsets guidance
  (per-token offsets were rejected as fake precision in the POC).

## Not Done (honest)
- Full pending/working-area compaction *replacement* of the compaction engine:
  the trim is a targeted region drop on commit; the generic context
  compaction (TakeOldest/summarize) still runs when near the token limit.
  Deferred — the committed-region trim + carry-forward covers the POC's
  "bounded working memory" for typical multi-item tasks.
- Cross-RUN (process-boundary) carry-forward helper `taskAreaCarryForward` is
  implemented as a standalone function but NOT yet wired into a resume/reload
  path — resume re-injection is a follow-up (in-loop carry-forward is the
  shipped path; the acceptance run covers in-run cross-round).
- No per-session visibility tool (`gopi task show` analog for conclusions) —
  DB rows are inspectable directly.

## Verification
- Build: `go build ./...` OK.
- Unit tests (new): `internal/tools/writetaskarea_test.go` — validation,
  append-only/scoping, missing-DB-empty read. `internal/agent/
  writetaskarea_test.go` — trim keeps epic+commit, drops analysis, never
  orphans tool results, keeps live tail; carry-forward injects recorded facts.
- Existing prompt tests pass (density gate honored: baseline 4498 → now 4491
  chars after compression of routing/efficiency/job/engineering text that had
  drifted — no marker strings lost; counter-example + routing audit tests
  green).
- Real-task acceptance (3× multi-item chains calling the tool once per item,
  in order, conclusions persisted + carried): **3/3 PASS** — every run
  committed 5/5 write_task_area conclusions (one per item, task_id 1..5 in
  order) and produced the correct chain
  `France → Paris → Seine → English Channel → Atlantic Ocean`.
  Sessions: e4cd1aed / 3757c2c6 / fc426fed (5 rows each in
  session_conclusions, session-scoped). Carry-forward block
  `[WRITE_TASK_AREA — COMMITTED CONCLUSIONS (authoritative)]` injected after
  each commit round (8 occurrences in run1's request payloads); committed
  analysis regions trimmed (5 trim events logged per run).
- Full suite: `go test ./internal/agent/ ./internal/tools/ -count=1`.

## Deviations / notes
- The system-prompt density gate (4500 chars) had 2 chars of headroom at
  baseline; fitting the new section required compressing existing verbose
  blocks (memory_feedback d-line, engineering rule 2 parenthetical, job rules
  1-2, code_search routing line). Every compression preserved the exact marker
  strings the prompt tests assert.
- `taskAreaCarryForward` + `injectTaskAreaConclusions` share one render so
  in-loop and (future) cross-run carry-forward agree.
- Session ID source: `sessionIDForTasks()` (SessionLog.SessionID, with
  autoTaskSessionID test seam) — same provenance stamp the task ledger uses.

## Files
- internal/agent/loop.go (tool def, routing/hint text, dispatch, Agent fields,
  hook, inject + trim methods)
- internal/tools/writetaskarea.go (persistence layer)
- internal/tools/writetaskarea_test.go, internal/agent/writetaskarea_test.go
