<!-- doc-class: historical -->
# Task Report: `gopi task show` briefing display + codename-briefing root cause (e3ca35ca incident)

**Epic:** user report — `gopi task show` gives no idea what a task is about; session
`e3ca35ca-d664-4458-ac73-ead337cade07` ran on a bare code-name briefing (`pa-q-p3-wf-contract`)
and needed a `memory_search` round-trip just to learn what the task was.
**Branch:** `feat/task-show-briefing` (worktree `~/workspace/gopi-task-show-briefing`)
**Date:** 2026-08-30

---

## Investigation answers (the user's questions)

**Q: Why is the briefing a code name? Is this a mistake or routine?**

Root-caused chain, four independent findings:

1. **Who registered the row:** the harness auto-registration start hook
   (`internal/agent/taskregister.go`) — every gopi run inserts a ledger row keyed by its
   session id with `summary = "[auto] " + normalizeTaskSummary(briefing)`. So
   `e3ca35ca` is a SELF-row: the run registered itself.
2. **Why the code name:** the run's entire user prompt (first line of its session JSONL)
   was `[EPIC GOAL]\npa-q-p3-wf-contract\n\nBased on this Epic, …` — the codename was the
   *whole* briefing it received. Auto-registration just mirrored it into the summary.
3. **Who submitted it:** NOT `task_submit` (those stamp `source=llm` + submitter session;
   e3ca35ca is `source=auto`, `session_id` = itself). The `pa-q-*` prefix decodes as
   **PA-QUEUE**: rows `pa-q-cny-screenshot-shot` etc. carry `[PA-QUEUE][P3]` summaries with
   rich one-line Chinese descriptions, all stamped 2026-08-29 13:51–13:57 — a bulk insert by
   nick's PA (hermes) writing directly into gopi's ledger while batching its queue. The PA
   then dispatched one of them via `gopi task run` with **no payload**.
4. **Why the codename reached the child gopi:** `cmd/agent/cli_tasks.go` runTaskRun had
   `if strings.TrimSpace(payload) == "" { payload = target }` — the empty-payload fallback
   fed the **bare task_id as the briefing**. This is the actual bug. A codename carries zero
   task semantics; the child burned 37 iterations / 1.29M tokens reverse-engineering what
   "pa-q-p3-wf-contract" meant (it did find devdoc/tasklist-audit-compare-report.md §9 and
   delivered master 2d44ad1, but only via costly search, not via a briefing).

**Verdict: two-sided mistake, now defended on both sides.** The PA submitted summary-only
rows (its own summaries for `pa-q-*` are actually descriptive — the dispatcher just never
passed the description through as payload), and gopi's `task run` silently dispatched the
id when the payload was empty. Routine-ized gopi-side defense below; the PA-side fix
(feed the queue item's description as payload) belongs to `~/work/tasks`.

## Completed

- `internal/tools/tasks.go` — TaskShow now renders a **briefing block, always**:
  - payload non-empty → `briefing:\n<payload verbatim>` (payload IS the briefing);
  - payload empty + session JSONL exists → `briefing: (from session JSONL)` + the first
    plain-text user message with `[EPIC GOAL]` framing and the appended task-list
    instruction paragraph stripped (`stripEpicFraming`), capped at
    `taskBriefingMaxChars=800` runes (`capTaskBriefing`, `…[truncated — full text: session JSONL]`);
  - nothing recoverable → explicit `briefing: (none — summary is the briefing)`
    (no more silent field disappearance — cf. memory #1107 verbatim-first rule).
- `internal/tools/tasks.go` — new `submitted_by:` provenance line
  (`taskSubmittedBy`): llm → `LLM session <id> (task_submit)`, human → CLI, auto+task_id==session_id
  → `harness auto-registration (row IS the run: task_id = session id)`, other auto →
  `harness/external tooling (source=auto, no session recorded)`. Directly answers
  "why is this task submitted? by who?" for every future row.
- `cmd/agent/cli_tasks.go` — **`task run` refuses to dispatch an empty-payload row**
  (`taskRunBriefing`): error `task <id> has no payload briefing — refusing to dispatch…`
  instead of feeding the codename. The e3ca35ca failure mode is now impossible; the row
  keeps its descriptive summary as the human-readable record.
- Tests (regression guards): `TestTaskShowBriefingFromPayload`,
  `TestTaskShowBriefingFromSession` (reconstructs the e3ca35ca fixture exactly:
  codename summary + `[EPIC GOAL]…` first message → briefing recovered, framing stripped),
  `TestTaskShowBriefingTruncated`, `TestTaskShowBriefingMissingSession` (pending `pa-q-*`
  external row), `TestSubmittedByProvenance` (4 provenance cases), plus
  `TestTaskRunBriefingRefusesCodename` (cmd/agent) proving refusal + payload passthrough.
  Fixture JSONL lines are built with `json.Marshal` (hand-rolled JSON broke on newlines).
- Devdoc: this file.

## Not Done

- **PA-side payload propagation** (`~/work/tasks` dispatcher passing the queue item's
  description as gopi payload) — separate codebase, owner: nick/PA. The gopi-side refusal
  will make the next summary-only dispatch loud instead of silent, which is the point.
- Existing rows keep `payload=''`; no backfill (append-only ledger; the session-JSONL
  fallback recovers history at show time — zero backfill needed).
- Not merged to master (Rule 9: merge is PA's job after review; safe-merge gate applies).

## Verification

- Build: `go build ./...` OK; `gofmt` clean for all touched hunks (repo-wide gofmt
  pre-existing quote-normalization diffs in untouched comments are NOT from this change).
- Tests: `go test ./internal/tools -run 'TestTaskShow|TestSubmittedBy' -count=1` → ok
  (incl. 6 new tests); `go test ./cmd/agent -run 'TestTaskRunBriefing' -count=1` → ok;
  full `go test ./...` run recorded in the commit message.
- Real-data acceptance (the actual reported task):
  `/tmp/gopi-show task show e3ca35ca-d664-4458-ac73-ead337cade07` now prints
  `submitted_by: harness auto-registration (row IS the run: task_id = session id)` and
  `briefing: (from session JSONL)` → `pa-q-p3-wf-contract` — the user can finally see what
  the task was without a memory_search detour.
