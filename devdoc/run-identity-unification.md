<!-- doc-class: historical -->
# Run identity unification — the run id names the directory (2026-09-20)

Epic: **unify run identity in GoPi**. Every run's session directory is named by
its run id (a ledger row → `sessions/<task_id>/`, a chat → `sessions/<session_id>/`
— the same role), and the ledger's `tasks.session_id` column stops being the
pointer readers resolve a run through.

## The defect, stated precisely

`tasks.session_id` is a second, weaker pointer at a run that already has an
address:

| | run directory (`sessions/<id>/`) | `tasks.session_id` column |
|---|---|---|
| cardinality | **1:N** — a task with several serial runs (killed + resume + retry) has one directory holding all of them | **1:1** — one value per row |
| what it names | the run | the session that *submitted or executed* the row (one worker session executes MANY rows) |
| role | the run's address | ledger field: anti-self-reference key, lineage record |

Consequences, each measured on the operator's live data before the change (see
`## Measurements`):

1. **The resume path resolved through the column.** `TaskResumeTarget(taskID)`
   read `st.SessionID` and resolved *that session's* raw log — often the run that
   did not execute the row, and never the second run of a chain.
2. **It cost a whole-tree search** (`taskFindSessionJSONL` over `sessions/`,
   ~70k directories on this host) for a path the row's own task id names.
3. **It rendered as a bare uuid** — `session_id: 3f9c…` — which names no
   directory, no file and no lineage. The operator's word for it: *"completely
   meaningless"*.

## What changed

New leaf package **`internal/rundir`** (stdlib + `config`/`sessionlayout`/
`sessionevent` only — no consumer can create a cycle):

```go
rundir.Root() / RootAt(gopiDir) / Dir(gopiDir, runID) / DirFor(runID)  // the ONE run→directory mapping
rundir.Records(gopiDir, runID) / RecordsDir(dir) []Record             // the runs in one run directory
rundir.Latest(recs) / WithCheckpoint(recs) / ExistingRawLog(rec)      // newest, newest-resumable, the checkpoint
```

`rundir.Record` carries the run's own facts, read off its own log — `RunID`,
`SessionID`, `RawLogPath`, `Model`, `Started/HasStart`, `HasEnd/ElapsedSec/
Turns/Tokens/ExitCode`, `LastWrite`. A **bounded** read gets them: the first line
(`session_start`, 1 MiB window) and a 1 MiB tail. An unfinished run reports its
progress (`HasEnd=false` plus the last `request` event's iteration), never a
fabricated terminal state. Side files (`signal-provenance.jsonl`) are classified
by `internal/sessionlayout`, so a run is never double-counted.

**`internal/tools/taskrunlog.go`** (new) — the row-keyed resolution:

```go
taskRunDir(taskID)              // sessions/<task_id> — the address a row always names
taskRuns(taskID)                // the runs in it, newest last
taskRunResumeRawLog(taskID)     // the checkpoint a `gopi resume <task-id>` continues
taskRunRecordedRawLog(taskID)   // the recorded path even when the file is gone
taskRunLines(taskID)            // the `task show` run block
TaskCarryForwardSessionIDs(id)  // resume's cross-run conclusion carry-forward keys
```

**`internal/tools/taskproc.go`** — `TaskResumeTarget` resolves the row's run
directory first; the session-keyed chain is the fallback (rows whose directory
holds no run log). `TaskProcessStatusReport` gained `run_dir:` and labels the
value it prints as the ledger's own column rather than a run name.

**`internal/tools/tasks.go`** — `TaskShow` renders the run block, and the
`session_id` line keeps the ledger column's own rendering (present whether or
not the column is filled):

```
run_dir:        /home/user/.gopi/sessions/t-1788773479
run_log:        …/6ddc2873-….jsonl
runs:           2 run(s) — 2 completed, 0 with a raw-log checkpoint
run:            265ba4e9-…  2026-09-09 04:33:30  completed exit=0  68 turns  3531977 tokens  722s
run:            6ddc2873-…  2026-09-09 04:37:48  completed exit=0  2 turns  40456 tokens  3s
```

`session_log`/`raw_log` resolve from the row's own directory first (the
session-keyed lookup stays as the fallback), and the `model` fallback reads the
run's `session_start` from that directory.

**`cmd/agent/cli_resume.go`** — the carry-forward now walks the row's runs
newest-first (`CarryForwardHasConclusions`), the column last: for a multi-run row
the column routinely names a session that wrote no conclusions, so the previous
code silently dropped the ones a recovery exists to re-inject.

**`internal/agent/taskregister.go`** — the misleading labels the briefing called
out: the "two different entities" comment now states the one role (task id = the
run's address and directory name; session id = the ledger's column), and the two
`gopi resume <session-id>` doc strings read `gopi resume <task-id>` — which is
what `cli_resume.go` actually accepts and what the hint's `%s` already contains.

## Measurements (live host, 2026-09-20)

The demo test `TestRunIdentityDemoRealMultiRunRow` (opt-in:
`GOPI_TASK_RUNID_DEMO_REAL=1 GOPI_TASK_RUNID_DEMO_TASK_ID=<row>`; skipped in CI —
it reads the operator's real ledger) on the real multi-run row:

| step | result |
|---|---|
| old chain `taskRawLogPathFromSession("265ba4e9-…")` | `…/t-1788773479/265ba4e9-….raw.log` — **1m30s** cold, 3.27–5.92s warm |
| new path `taskRuns` + `taskRunResumeRawLog` | 2 runs found — **4.3 ms** |
| resume entry `TaskResumeTarget(t-1788773479)` | `…/6ddc2873-….raw.log` — **14.3 ms** |

**Same answer, ~230× faster warm and ~3800× faster cold.** The correctness half
is the point, though: the run the row must resume is the SECOND one
(`6ddc2873-…`, 2 turns, started 04:37), and the column names the FIRST
(`265ba4e9-…`, 68 turns, 04:33). Only the directory enumerates both.

Row shape measured (also true of `01a0b47e-…`, whose column names `69fd4bba-…`,
a session present in NO directory of that row): the column is not merely a weak
key, it is frequently the wrong run.

## The RED proof

`TestRunDirResolution_OldChainCannotReachTheRun` reproduces the legacy chain
verbatim against the same fixture and asserts it cannot answer, while the new
path does:

```
RED PROOF — same row, same fixture, two resolutions:
  OLD (session_id column -> taskRawLogPathFromSession) = ""
  NEW (row id -> sessions/<task_id>/*.jsonl)            = "…/t-plural-run/bbbb2222-….raw.log"
```

The fixture is the real host shape with ids rewritten: `aaaa1111…` (the column's
value — short, unfinished, checkpoint gone) and `bbbb2222…` (the run that must be
continued — in the directory and in **no** column). Fixture root is
`$GOPI_HOME/.gopi/sessions/<task_id>/` via `t.Setenv(config.GopiDirEnv, …)` —
hermetic, no operator data.

## The operator's chat-resume question, answered with measurements

> "why can't a chat resume from its previous run — the raw log is there, why not
> send it as the request and continue?"

**The resolution is not what blocks a chat.** A bare chat directory is named by
its session id, and `resolveResumeLogPath` already resolves it
(`gopilog.FindSession` → `SessionRawLogPath` → `SiblingRawLog`). Measured on this
host: **211 of the 238** identity-matched bare raw logs have a `session.jsonl`
beside them, so the ordinary chat resolves.

**What a chat lacks is the ledger ROW.** Everything the resume path does beyond
loading the checkpoint keys on a `task_id`: the `--task` linkage tag, the
cross-run `write_task_area` carry-forward, and the process-identity (claim/
holder) gate. A bare prompt has no row, so it cannot be resumed *as a row* — a
product gap, not a resolution defect, and not something this change can close
without inventing a row for every prompt.

**What a chat CAN do today**: `gopi resume <session-id>` replays its checkpoint,
and `gopi resume <raw-log-path>` takes the raw log directly. The remaining **27**
bare raw logs have no jsonl beside them — that absence is reported, never
guessed at.

## Boundaries held (the briefing's explicit constraints)

- `tasks.session_id` is **not** renamed, dropped or migrated — the append-only
  ledger, its sync replicas and `parent_task_id` are untouched. Nothing in this
  change writes to the column; the resolution path no longer *reads* it. It is
  still read in exactly two places, and both should: the carry-forward's last
  resort, and `TaskSessionID`'s own API (used by the ledger's lineage display).
- **No chats and no chat consumers were touched** (the change is on the tools
  side plus the resume CLI, and chats keep their existing resolution path).
- The `sessions/` **on-disk layout is untouched** — no file moved, renamed or
  pruned; the change only *reads* it, and `internal/sessionlayout` remains the
  single authority on its shapes.
- The one-concurrent-run singleton rule and the process-identity gate are
  unchanged.
- `internal/tools/taskstate.go` was deliberately **not** touched: it resolves a
  status view through `taskFindSessionJSONLForRow` (already addressed, not a
  whole-tree search), and rewriting it would churn the session-resolution cost
  tests that pin that behaviour for no gain to this epic.

## Verification

- `go build ./...` — OK.
- `go test ./internal/tools/` (with the new tests) — OK.
- `go test ./internal/rundir/` — OK.
- `go test ./cmd/agent/ -run TestResume` — OK (33.4s).
- `go test ./internal/standards/` — the six AST gates — OK.
- `gofmt -l` clean for every file this change touches (pre-existing
  gofmt-uncleanliness in `task_staleness_guard_test.go`,
  `taskledger_cost_test.go`, `serve_lock_replacement_test.go`,
  `worker_stop_forward_regression_test.go` is NOT this change's — `make fmt` is
  forbidden here).
