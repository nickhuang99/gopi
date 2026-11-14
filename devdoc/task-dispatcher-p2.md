<!-- doc-class: historical -->
# devdoc — task dispatcher P2 (feat/task-dispatcher)

## What shipped

The tasks ledger was upgraded from a passive audit layer to a **work queue**:
LLM sessions self-enqueue (bug found / task too large → split), and a gopi
instance (`task next` / `task run` / `task serve`) claims and executes. This
is the divide-and-conquer experiment's data plane (ADDENDUM #8: observe LLM
behavior first, constrain later).

Naming follows nick's verdict: `task_evaluate` / `task_submit` tools, `gopi
task next|run|submit|serve` CLI.

## Schema (internal/tools/tasks.go)

**Status set** — `pending, claimed, in_progress, completed, failed,
cancelled` (P2.5's verify/reopened/blocked deliberately deferred; the
`taskStatusSet` slice + `taskSchemaStatusCHECK()` are the single source the
CREATE literal and the rebuild migration both derive from).

**New columns** — `parent_task_id, tags, payload, source, claimed_by,
claimed_at` via the idempotent `ALTER TABLE … ADD COLUMN` pattern
(`tasksP2Columns`; "duplicate column name" swallowed). `model` already
existed (P0).

**CHECK migration** — SQLite cannot ALTER a CHECK, so a legacy four-status
table is detected by probing `sqlite_master.sql` (`tasksNeedsRebuild`) and
rebuilt rename→create→copy→drop inside ONE transaction
(`tasksRebuildCheckConstraint`). The copy SELECT probes
`pragma_table_info('tasks_old')` and backfills `model`/P2 columns with
defaults when the old table predates them (pre-model ledgers migrate too —
TestEnsureTasksSchemaMigratesLegacyFourStatusDB covers the full path).
Legacy rows survive verbatim; nothing is rewritten or deleted (append-only).

**FTS scope** — `tasksFTSColumns` (task_id, session_id, summary,
parent_task_id, tags, payload, source) indexes all CONTENT columns; the CAS
columns (claimed_by/claimed_at) are deliberately unindexed — they churn on
every pickup and would rewrite each row's FTS entry per claim. Index-shape
migration (`migrateTasksFTS`): drop + recreate + `VALUES('rebuild')`
repopulation for indexes missing any column, triggers recreated last. The
ai/au/ad trigger bodies are generated from the same column list.

## State machine

```
pending → claimed → in_progress → completed | failed
claimed → pending (release)     in_progress → pending
failed  → pending (retry)       completed → in_progress (rework)
any non-terminal → cancelled    cancelled → pending (resurrect)
```

`taskAllowedTransitions` is the explicit table (the ~/work/tasks
validTransitions pattern); `TaskUpdate` loads the current row and refuses
illegal moves BEFORE mutating (error names the allowed set). Same-status
updates are idempotent no-ops.

## Atomic claim

`TaskNext(claimedBy, excludeSession)`: select oldest eligible pending →
conditional `UPDATE … WHERE task_id=? AND status='pending'` →
`RowsAffected()==1` wins. Losers loop (≤10 attempts) onto the next row or
observe an empty queue. SQLite WAL + busy_timeout + `MaxOpenConns(1)`
serialize writers; the conditional UPDATE is the correctness gate, not
scheduling. `--as` overrides the holder (default `hostname:pid`).

**Anti-self-reference (ADDENDUM #5)**: `task_submit` stamps
`source='llm'` + the submitting session's id; `next`/`run`/`serve` accept
`--exclude-session <GOPI_SESSION_ID>` and skip those rows. A serve loop
running inside a session therefore never picks up its own submissions; a
second instance or the human does.

## LLM tools (loop.go)

- `task_evaluate(request)` — the routing GATE. The evaluation is the LLM's
  own judgment (criteria live in the tool description: locally doable in a
  few tool rounds → do it NOW; multi-stage/out-of-scope/bug-that-would-
  derail → submit). The handler returns the decision framing + the request
  text; it writes nothing.
- `task_submit(summary, payload?, tags?, parent_task_id?)` — the enqueue
  EXIT. `payload` is the execution briefing (self-contained: context, goal,
  constraints, acceptance); `tags=['bug']` is the bug-flow entry point;
  `parent_task_id` must reference an existing task (dangling = error).
  Descriptions carry the value contrast + routing triggers (8/28 lesson:
  feature-only descriptions don't get adopted) and both descriptions
  cross-reference each other plus `gopi task run/serve` so the loop is
  discoverable.

## CLI (cmd/agent/cli_tasks.go)

- `task submit "<summary>" --payload --tags --parent-task-id --source
  --session-id` — human/software enqueue (source defaults to `human`).
- `task next --as --exclude-session` — atomic claim, prints id + payload.
- `task run [<id>|next] --as --exclude-session [-- <args>]` — claim →
  in_progress → spawn the SAME executable (`os.Executable()`) with the
  payload on stdin as a normal gopi run → writeback completed/failed by
  child exit code + elapsed appended to summary. No second execution engine
  (ADDENDUM #2): the child gets session registration, GOPI.md, all tools.
- `task serve --every 5m --once` — serial daemon loop over run; `--once`
  exits after one pass (E2E/unit seam).

## TaskSubmit adoption metrics (divide-and-conquer data plane)

The experiment needs measurable adoption signals. Available today:

- **Countable ledger truth**: `gopi task list --status pending` /
  `source LIKE 'llm'` — submissions per session (`session_id` column is
  stamped), per tag (`tags`), bug-flow volume (`tags LIKE '%bug%'`).
- **Split-tree shape**: `parent_task_id` chains → depth/breadth per
  experiment run (SQL recursion over the column; no depth cap shipped by
  design — ADDENDUM #4: observe first).
- **Loop closure**: rows transitioning llm-submitted → claimed by a
  `hostname:pid` different from the submitter (claimed_by ≠ submitter
  session) = real dispatch; `completed` vs `failed` writeback ratios.
- **Session-log correlation**: every session JSONL
  (~/.gopi/sessions/<id>.jsonl) carries `session_start` with model +
  briefing; a run that EVALUATED a request shows the `task_evaluate`
  tool_use in its JSONL (grep-able), whether it routed to do-direct or
  submit. Adoption rate = sessions with task_submit tool_use ÷ sessions
  whose briefing matched the trigger criteria.
- **What's NOT measurable yet**: per-task wall-time attribution to the
  parent's remaining work (needs the chat/web poller, P3), and outcome
  quality (needs P2.5 verify). Both are the next phases' instrumentation.

## Tests

`internal/tools/task_dispatch_test.go` — lifecycle (submit→claim→start→
writeback), CAS race (`-race`, 8 tasks × 4 goroutines, exactly-once),
illegal-transition refusals, FTS recall over new columns (ASCII word +
CJK fragment via trigram), legacy four-status DB migration (verbatim rows,
pending insertable, FTS widened, legacy row still searchable).
`internal/agent/task_tools_test.go` — defs registered with trigger words,
validateToolArgs, evaluate is pure routing, submit stamps pending/llm/
session + anti-self-reference exclusion.

## E2E evidence (fresh DB /tmp/p2e2e, real child gopi run)

```
$ gopi task submit "create hello.go in /tmp/p2e2e/work that prints hello dispatcher" \
    --payload "In /tmp/p2e2e/work create hello.go printing exactly: hello dispatcher. …" \
    --tags demo --source human --task-id t-e2e-hello
Task queued: t-e2e-hello (status=pending, source=human, tags=demo)

$ gopi task list --status pending
1 task(s) with status=pending:
#1  t-e2e-hello  [pending]  create hello.go …  (model (unknown), source human, created …)

$ gopi task run
claimed: t-e2e-hello
running gopi with task t-e2e-hello as briefing…
… child gopi session runs with the payload as briefing …

$ cat /tmp/p2e2e/work/hello.go
package main
import "fmt"
func main() { fmt.Println("hello dispatcher") }

$ gopi task show t-e2e-hello
status:         completed
claimed_by:     peerbox:4064111 (at 2026-08-29 14:26:41)
payload:        In /tmp/p2e2e/work create hello.go …
ended_at:       2026-08-29 14:27:52

# two-process concurrent claim (exactly one winner):
proc-A: claimed: t-race-proc / proc-B: no pending task
```

Bug found+fixed during E2E: serve/run leaked dispatcher flags (--once,
--as) into the child gopi argv → "unknown flag --once"; the sub-verb's
args now stay in this process (the child gets only the payload on stdin).

## Not done (per epic, later phases)

verify/reopened/blocked + bug-flow closure (P2.5); per-task modelname
routing (column already there); chat/web embedded poll (P3 — serve is the
standalone path).
