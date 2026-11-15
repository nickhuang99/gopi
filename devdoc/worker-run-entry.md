<!-- doc-class: normative -->
# Worker-run-entry: `gopi worker` is the ONE command that runs a task row

**Status: landed on `feat/worker-run-entry` (epic worker-run-entry, 2026-09-16).**

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md**
>
> The `gopi worker …` forms quoted on this page (the loop, `--once`, `--id`,
> `--daemon`) and the retired `gopi task run` shape it documents are **the
> operator's** commands, run in his own terminal. hermes / PA / any agent must
> NEVER run them — submit the work (`gopi task submit`) and let the queue hand
> it off. A refused `gopi worker` (exit 3) is the contract working.


## The problem the epic named

Three entry points ran tasks and each one behaved differently, even though they
converged on ONE function (`runTaskRunOnce`):

| entry | selection | loop | output |
|---|---|---|---|
| `gopi task run <id>` | explicit row + singleton guard | one row, then exit | child output NOT echoed |
| `gopi task run next` | FIFO claim | one row, then exit | child output NOT echoed |
| `gopi worker [--once]` | worker CAS claim | drains the queue | worker progress lines |
| `gopi task serve` | tick claim | resident | — |

The operator's report: *"I was confused about which one to use today"* —
`task run` in the foreground stalls an agent, the worker shows progress the
manual verb does not, and the answer to "which do I type" depends on knowledge
that was nowhere on the help page. Same function, three names, three doc sets,
three user expectations: the boundary is where the bugs and the confusion live.

## The decision (operator's option C, from the epic briefing)

**Keep `worker`, retire `task run`.** The worker is already the only process
that *claims* rows; giving it the explicit-selection face makes it the only
process that *runs* them too. `gopi raw-prompt` stays for flexibility.

## What the rework does

1. **`gopi worker --id <task-id>` — the explicit face.** Runs EXACTLY that row,
   in the foreground, and the process exit code carries the row's outcome
   (0 completed / 1 failed). The named-run flags it accepts —
   `--takeover`, `--as <holder>`, `--db <path>`, `--report-to <alias>`,
   `--repo <target>` — are precisely the flags the retired verb carried, and
   they fill the same `tasksOptions` fields, so the run path is literally the
   same struct, not a parallel one.

2. **One round body, shared.** `workerRound(explicit, opts)` is the ONE round:
   with `explicit == ""` it claims the next eligible row (the loop's arm); with
   a row it skips the claim scan and runs that row. Both faces therefore inherit
   the worker's provenance tag, output attribution, singleton guard, graceful
   stop, lifecycle ownership and own-worktree release *by construction* — the
   "no more divergent handling" contract.

3. **`gopi task run` is RETIRED, not deleted.** The spelling still DISPATCHES
   (`internal/taskverbs.RemovedVerbs`) and answers with a pointed removal
   message — the replacement spelled out — plus exit 1. Rationale (Rule 15):
   the shape is recorded verbatim in `run_cmd` stamps, dispatch logs and months
   of transcripts; a script that types it must hear *where the function went*,
   not a generic usage dump. It lives in no advertised group, so `gopi task -h`
   and `gopi task <TAB>` teach management only.

4. **The dispatch path emits the new argv.**
   `dispatchRemoteCommand` and the recorded `run_cmd` now build
   `gopi worker --id <task-id> --report-to <alias> [--repo X]` from
   `internal/runcmd` constants (the remote runs ONE named row through the one
   runner — exactly what the handoff needs, since leg ① pushed the row into the
   node's ledger).

5. **Contradictory argv is refused with a sentence, not "unexpected argument".**
   `--once` + `--id`, `--daemon` + `--id`, and a named-run flag without `--id`
   are each refused (exit 2) with a message that names the conflict — every word
   is known, so the stale-binary hint would be wrong.

6. **One diagnostic name.** The run path used to print `gopi task run:` on 27
   sites; they now read `taskProgName` (`gopi worker`), the name the reader is
   actually standing at.

## Files

- `cmd/agent/main.go` — worker dispatch: `--id` branch, `runOptions()`, the
  refusal wiring, the rewritten usage head.
- `cmd/agent/cli_worker_daemon.go` — `scanWorkerArgs`/`workerArgs` (the explicit
  face's fields + `refuse`), `workerIdFlag`.
- `cmd/agent/cli_tasks_worker.go` — `runTaskWorker(once, explicit, opts)`,
  `runTaskWorkerExplicit`, `workerRound(explicit, opts)`.
- `cmd/agent/cli_tasks.go` — the retired verb's dispatch case and removal
  message, `taskProgName`, the run path's diagnostics.
- `cmd/agent/dispatch.go` — the remote argv.
- `internal/taskverbs/verbs.go` — `RemovedVerbs`, `DispatchVerbs` union.
- `internal/runcmd/runcmd.go` — `FlagID`; `VerbRun` documented as retired.
- `internal/config/help.go` — the top-level index line.
- `internal/workerownership/workerownership.go` — the retired needle's doc.
- `devdoc/worker-ownership.md` — the canonical table's "Running one task" row.
- `cmd/agent/worker_run_entry_test.go` — the new seam test (see below).

## Verification

- **Build**: `CGO_ENABLED=0 go build ./...` → OK; `bin/gopi` rebuilt.
- **Seam test** (`cmd/agent/worker_run_entry_test.go`): the retired spelling
  explains itself for every historical argv shape (bare, with row, `next`,
  `--report-to/--repo`, `--takeover`), `-h` answers exit 0, and the ledger page
  advertises no runner.
- **Measured against the built binary** (sandbox profile):
  - `gopi worker --id t-bogus` → the agent-session refusal (exit 3), i.e. the
    explicit face reaches the SAME guard as the loop;
  - `gopi worker --once --id t-1` → `--once and --id t-1 ask for different work…`
    (exit 2);
  - `gopi worker --daemon --id t-1` → `--daemon with --id t-1 is not a thing…`
    (exit 2);
  - `gopi worker --takeover` (without `--id`) → unexpected-argument refusal
    naming the accepted set (exit 2);
  - `gopi task run` → the removal sentence (exit 1);
  - `gopi worker --id <a real row>` → ran the row and exited 1 with the FAILED
    line (the row's own outcome).
- **Suites**: `go test ./internal/taskverbs/... ./internal/config/...
  ./internal/runcmd/...` → ok. `./internal/standards/` carries ONE failure,
  `TestGateCwdReleaseFindsTheRealTree` (floor 5 vs 4 producers) — reproduced
  **identically on master**, i.e. pre-existing and not caused by this branch.

## Not done (honest)

- **`worker --once` is kept.** The briefing flagged it as a suspect duplicate of
  plain `worker`. It is not removed here: `--once` is the drain-then-exit face
  scripts and rigs already use, and removing it is a second, separable
  restructure with its own blast radius. It is documented as the same loop,
  bounded to one pass.
- **`gopi raw-prompt`** is untouched (deliberately: the epic's option C keeps it
  as the flexible face; auto-creating a tracking row for it is a separate task).
- **Historical devdocs/briefings** that teach `gopi task run` were NOT rewritten.
  They are records of what was true when written; the corpus is what the
  worker-ownership gate audits, and the retired spelling stays a needle so those
  documents keep carrying the pointer.
