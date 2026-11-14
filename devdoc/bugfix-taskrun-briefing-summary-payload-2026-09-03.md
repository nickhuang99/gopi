<!-- doc-class: normative -->
# Task Report: task run briefing composes summary + payload

Date: 2026-09-03 · Branch: `feat/briefing-summary-payload`

## Problem

`taskRunBriefing` (cmd/agent/cli_tasks.go) treated the **payload column as
the single briefing source**: when a payload was non-empty the summary was
completely suppressed, and only an EMPTY payload fell back to the summary.
With the normal submit convention (`gopi task submit "<summary>" --payload
<briefing>` — both fields set), the child gopi received ONLY the payload via
stdin. A submitter who put the essential one-line intent in the summary and
omitted it from the payload (to avoid repetition — the operator, 2026-09-02) had
that intent silently dropped from the child's execution context, so the
child executed on an incomplete briefing.

## Fix

`taskRunBriefing` now composes BOTH fields into one briefing in a stable,
section-marked format:

```
[Task summary]
<summary>          (when non-empty)
[Briefing payload]
<payload>          (when non-empty)
```

Order: summary first (the intent line), then payload (the detail). The two
explicit headers (`[Task summary]` / `[Briefing payload]` — named constants,
Rule 1) stop the child LLM confusing the one-line intent with the detailed
request inside its `[EPIC GOAL]` first user message.

Fallback/refusal semantics preserved:
- summary + payload → both, summary first.
- payload only (empty summary) → `[Briefing payload]` section only —
  unchanged from the old passthrough.
- summary only (empty payload, the `--payload`-absent submit contract) →
  `[Task summary]` section only — the old fallback, preserved.
- neither → refuse (unchanged e3ca35ca defense — no bare task_id/codename
  dispatch).

### Single point of composition

New `tools.TaskBriefing(taskID) (summary, payload string, err error)`
(internal/tools/tasks.go) returns BOTH columns in ONE row read (one DB open
per dispatch — two separate accessors would race the ledger and double the
opens). Every dispatch path funnels through `runTaskRunOnce` → the ONE
`taskRunBriefing` call site, so the fix lands everywhere:

- `gopi task run <id>` (runTaskRun → runTaskRunOnce)
- serve daemon loop (runTaskServe → runTaskRunOnce)
- worker round (cli_tasks_worker.go workerRound → runTaskRun)
- claimed-row crash recovery resume (TaskResumeClaimed → runTaskRunOnce →
  taskRunBriefing)
- the resume/--task linkage is a checkpoint-continuation, not a briefing
  path — unaffected.

## Completed

- `taskRunBriefing` composes summary + payload with section headers.
- `tools.TaskBriefing` single-row-read accessor (summary+payload pair).
- Regression unit tests (cmd/agent/cli_tasks_test.go):
  - `TestTaskRunBriefingComposesSummaryAndPayload` — distinct summary AND
    payload → both present, summary section ordered before payload.
  - `TestTaskRunBriefingFallbacks` — payload-only and summary-only shapes.
  - `TestTaskRunBriefingRefusesEmpty` — neither → refuse.
- E2E acceptance test `TestTaskRunChildReceivesSummaryAndPayloadE2E` — real
  `gopi task run <id>` subprocess against the mock LLM; the child's session
  JSONL first user message must contain BOTH the distinct summary text and
  the payload text (machine-checkable acceptance #1).
- This devdoc.

## Not Done

- `gopi task run --rework` — no such flag exists today (rework is a
  reopen/re-submit construct carrying the reason into a NEW row's payload,
  which then flows through the same composed briefing). Nothing to change.
- No GOPI.md change needed (no new magic numbers; constants satisfy Rule 1).

## Verification

- Build: `go build ./...` OK (worktree ~/workspace/gopi-briefing-summary-payload)
- Tests:
  - `go test ./cmd/agent/ -run 'TestTaskRunBriefing'` — 3/3 PASS
  - `go test ./cmd/agent/ -run 'TestTaskRunChildReceivesSummaryAndPayloadE2E'`
    — PASS (E2E: child session carries both fields)
  - `go test ./cmd/agent/ -run 'TestTaskRunBriefing|TestTaskRunChildReceivesSummaryAndPayloadE2E|TestTaskRunTwoRowLinkageE2E|TestRunServeClaimLocalOnlyWithNoPeers|TestTaskRunDoubleExec'`
    — PASS (briefing + adjacent run-path tests)
  - `go test ./internal/tools/ -run 'TestTaskShowBriefing|TestTaskSubmitModelFlag|TestTaskUpdateLifecycle'`
    — PASS (internal/tools FULL suite hangs pre-existing on master — sql
    connectionOpener, memory #1172; targeted run only)
- Commit: `3993274` on `feat/briefing-summary-payload`.

### Pre-existing flake disclosure (blocks full-suite green)

`go test ./cmd/agent/` (full) is NOT deterministic on this branch for a
PRE-EXISTING reason unrelated to this fix: `TestTaskRunSurvivesClosedStdoutPipe`
(t-1788305178 broken-pipe domain) is flaky ~40-60% STANDALONE — the child
gopi dies SIGPIPE ("signal: broken pipe", exit -1) despite the wrapper's
signal.Ignore. Proven pre-existing: same failure on clean HEAD with these
changes stashed (git stash → run → pop, 6/6 fail standalone). The
singlenode serve test also flaked once in a full run (passes 3× isolated
with and without these changes). Filed as bug task t-1788387314 with a
full repro + root-cause hypothesis; the fix belongs to the broken-pipe
domain (cmd/agent/main.go + cli_tasks.go guard ordering), NOT this
briefing change.

## Test-fix note (self-inflicted bug caught by the suite)

The first draft of `TestTaskRunBriefingFallbacks` seeded its "payload-only"
row via `TaskSubmit` — whose summary column is NOT NULL — so the row carried
a non-empty summary and the test wrongly expected NO summary section. Fix:
blank the summary column with raw SQL (`UPDATE tasks SET summary=''`) after
insert, exactly as `TestTaskRunBriefingRefusesEmpty` already did for the
both-empty shape. The suite caught the wrong premise (2s run, real failure).
