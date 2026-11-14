<!-- doc-class: normative -->
# worker-positional-taskid: the row is an ARGUMENT, not a flag

**Status: implemented on `feat/worker-positional-taskid` (epic
worker-positional-taskid, 2026-09-18).**

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md**
>
> The `gopi worker …` forms on this page (the loop, `--once`, `<task-id>`,
> `--daemon`) are **the operator's** commands. hermes / PA / any agent must
> NEVER run them — submit the work (`gopi task submit`) and let the queue hand
> it off. A refused `gopi worker` (exit 3) is the contract working.

## The complaint

`gopi worker --id TASK_ID` selected one row with a **flag**, for a value that
cannot be anything else. The verb takes exactly one row, a task id is
unmistakable, and the flag bought nothing but a second way to get the
invocation wrong. The operator's framing: *"worker parameter `--id` is
unnecessary, it should just use taskid as param, i.e. `gopi worker taskid`;
there should be no ambiguity for parsing. Don't forget to update the help
msg."*

## What changed

    gopi worker --id t-abc            →   gopi worker t-abc

| surface | before | after |
|---|---|---|
| the row | `--id TASK_ID` (value flag) | **positional** `<task-id>` |
| the run-shaped options | `--takeover`, `--as`, `--db`, `--report-to`, `--repo` (unchanged spellings, now documented "with a task id") | idem |
| the retired spelling | — | **REFUSED**, naming the replacement |
| help | `--id TASK_ID` in the synopsis, the options list and the Notes | `<task-id>` as an argument (synopsis + options), Notes say "naming a row" |
| recorded `run_cmd` | `gopi worker --id <id> [--report-to …]` | `gopi worker <id> [--report-to …]` |
| dispatch handoff (ssh) | `nohup gopi worker --id <id> --report-to <alias> …` | `nohup gopi worker <id> --report-to <alias> …` |

## The decisions behind it (and why)

1. **ONE scanner, not two** — the CLI face (`isWorkerSubcommand`) and the
   daemon's pre-detach validation (`workerDaemonSpec.prep`) share
   `scanWorkerArgs`. The positional lives there, so both faces agree by
   construction (Rule 2/14); the daemon's prep now renders its refusal with the
   **same** `workerUnexpectedArgMessage` the foreground face prints, instead of
   a private `(accepted: …)` spelling.
2. **The retired flag is RECOGNIZED, then refused** (Rule 15). A command
   recorded while `--id` was live — a `run_cmd` on a row, a dispatch log, an
   operator's handoff script — must hear *what replaced it*, not "unknown
   option, your binary is stale" (that hint would send the operator to install
   the version they already run). `workerUnexpectedArgMessage` therefore
   special-cases the retired spelling (`workerIdRetiredSpelling`) with its own
   remedy line.
3. **`runcmd.FlagID` KEEPS its value and its home** — exactly the `VerbRun`
   precedent (Rule 14): old recorded commands must stay *readable*, and the
   spelling is needed by two packages (cmd/agent refuses it, the ledger's
   rendering documents it). It is now documented as RETIRED, its only reader is
   the refusal, and **nothing renders it** any more.
4. **A second positional is refused.** The positional grammar could fold
   `t-1 t-2` silently into "the row is `t-1`" (dropping the rest — precisely the
   silent degradation the unknown-word refusal exists for), so the second word
   is yielded as the offending word.
5. **The contradictions kept their teeth** (`--once` + a row, `--daemon` + a
   row, a run-shaped flag with no row): the *words* changed, the refusal
   sentences were re-cut to name the row instead of the flag.

## What deliberately did NOT change

- `--once`, `--daemon`, the daemon seam flag, and the control verbs
  (`--status`/`--stop`/`--restart`/`--start`, routed BEFORE the scan) — the
  positional joins the vocabulary, it does not replace it.
- The gate/spawn seam, the exit codes (`0` completed / `1` failed / `2` usage),
  and the run-shaped options' injection guards
  (`peerFlagError` / `repoFlagError`).

## Verification (run for this change)

- `go build ./...` — OK; `go build -o bin/gopi ./cmd/agent/` — OK.
- `go vet ./...` — clean.
- Real binary:

      $ ./bin/gopi worker --id t-abc
      gopi worker: unexpected argument "--id" — refusing to start the worker loop
        accepted: -h/--help | --once | <task-id> [--takeover] [--as <holder>] …
        (the row is an ARGUMENT, not a flag: `gopi worker <task-id>` runs that one row)
        the row is POSITIONAL now: `gopi worker <task-id>` (the --id flag was retired)

      $ ./bin/gopi worker --id=t-abc     # same refusal, whole token named
      $ ./bin/gopi worker t-1 t-2        # second positional refused, named "t-2"
      $ ./bin/gopi worker --stray        # unknown word → the STALE hint (unrelated case)
      $ ./bin/gopi worker -h             # synopsis + options show `<task-id>`, no `--id`

- Go tests: `go test -count=1 ./cmd/agent/ ./internal/runcmd/
  ./internal/taskverbs/ ./internal/config/ ./internal/tools/` → EXIT=0, all
  green (cmd/agent 335.7s, tools 535.0s). Full lines in the task report.

## Blast radius (files)

- `cmd/agent/cli_worker_daemon.go` — scanner arm, `workerIdRetiredSpelling`,
  `validate()` sentences, `workerRunFlags` (row removed), daemon prep reuses
  the single refusal renderer.
- `cmd/agent/main.go` — `workerUnexpectedArgMessage` (retired-spelling remedy),
  `workerUsageHead` (synopsis + options + Notes).
- `cmd/agent/dispatch.go`, `cmd/agent/cli_tasks.go` — recorded renders drop the
  flag (`dispatchRemoteCommand`, the run_cmd stamp, `runMethodFor`).
- `internal/runcmd/runcmd.go` — `FlagID` documented RETIRED (value kept).
- `internal/config/help.go`, `cmd/agent/cli_tasks.go` (family page),
  `internal/taskverbs/verbs.go` (doc) — help/prose.
- `devdoc/worker-ownership.md`, `devdoc/daemon-machinery-removal.md` — prose.
- tests: `worker_run_entry_test.go` (contradictions + the retired spelling
  covered), `worker_daemon_flag_e2e_test.go` (scanner: row in, retired flag
  out), `cli_tasks_worker_unknown_arg_test.go` (second positional), the e2e
  rigs that drove `worker --id <row>` (recycle / singleton / waitpid / report
  seam / double-exec / dispatch / peer-alias / spawn-guard), and the run_cmd
  literals in `cli_tasks_test.go`.
