<!-- doc-class: historical -->
# Task Report: `gopi worker --status` over-reported workers (2026-09-13)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->


**Epic:** *why are there so many worker processes?* — a `gopi worker --status`
run returned what looked like a huge list of worker processes.

## Verdict (measured, not inferred)

**`--status` was over-reporting. Most of what the operator saw was NOT a worker.**

Measured on the incident host at 22:43–23:0x (attachment
`tmp/status_worker.txt` + independent `ps -eo pid,ppid,comm,args` and
`/proc/*/cmdline` ground truth):

| What `--status` showed | What it really was |
|---|---|
| `gopi worker pid 936911`, `pid 936941` | **Another agent's leaked test-fixture workers** (`gopi-peak-diffctl/bin/gopi worker --worker-log-root /home/user/scratch/peakgaps-run3/…/TestGlmNightWatchOperatorPathStartsWorkersUnattended…`). Both **gone** by 22:43 — a snapshot of a transient leak, not a resident worker. |
| `gopi worker pid 2140436`, `pid 2140461` | **Real** resident workers (argv `gopi-worker worker`, detached in `session-18200.scope`, both started 12:10 — i.e. running since before the incident). Their binary is `bin/gopi (deleted)`, i.e. a **stale build**. |
| `66 stale state file(s): …worker-state-<pid>…` (4KB on one line) | **Dead-pid residue** in `~/.gopi/worker/` — one file per worker that ever died without cleanup (SIGKILL, leaked tests). By the end of the session the pile had grown to **70**. Not workers — but rendering them as a comma-joined pid dump made the line read like a worker list. |

### The real defect in the enumeration (found by repro, not by argument)

A state file `worker-state-<pid>` was trusted on **NAME + liveness alone** — no
process-shape check. Two consequences:

1. **A recycled pid becomes a fabricated worker.** `pid_max` on this host is
   `4194304`, so the pid counter wraps; a residue file eventually names a live
   *unrelated* process. Measured: residue file `worker-state-899174` names a live
   `agent.test`. Repro (before the fix): writing
   `{"pid":<live sleep pid>}` to `~/.gopi/worker/worker-state-<pid>` and running
   the old `--status` printed
   `gopi worker pid <N>: idle` — a worker that does not exist.
2. **The report was non-deterministic.** The same residue file appeared as
   "stale" in one run and as a live worker in the next, purely depending on
   whether its pid happened to be alive at that instant.

## Completed

- `cmd/agent/cli_worker_status.go`
  - **Attestation**: a state-file record is rendered as a worker ONLY when the
    live process behind its pid has `comm == gopi` (`processComm` /
    `ResidueReasonNotGopi`). Dead pid → residue; recycled pid → residue;
    unreadable `/proc` → residue; unreadable/unparsable body → residue. Nothing
    is dropped (Rule 15).
  - **One bounded residue line** (`workerStatusResidueLine`): exact count first,
    at most `workerStatusResidueSample` (=6) entries named with their reason,
    then `… and N more`. 4KB of pid noise → one readable line. Every reason is a
    named constant (Rule 1b): dead / recycled / unattested / unreadable /
    unparsable.
    **Superseded 2026-09-14** (`devdoc/worker-status-output.md`, commit on
    `fix/worker-status-residue-output`): ONE comma-joined line was still the wrong
    shape — 929 runes of pids read like a worker LIST. The residue is now a
    labelled BLOCK (header + what the files are + ≤6 rows + omitted count +
    the `rm -f` remedy), the empty answer explains itself ("why / lock / start
    one"), `--status-verbose` lists every path, every row is capped at
    `workerStatusResidueMaxRowRunes`, and the dead `workerControlStatusCommand`
    (the unmaintained second `--status` face that still printed "N stale state
    file(s)") was deleted.
  - **Self-healing prune** (`pruneWorkerStateResidue` + `workerStateResiduePrune`):
    dead-pid state files are removed when a worker **starts** — never by
    `--status`, which stays side-effect-free.
- `cmd/agent/cli_tasks_worker.go`: every worker face (foreground `--once`
  included — that is exactly what leaked the pile) calls
  `pruneWorkerStateResidue()` at loop start.
- Tests
  - `cmd/agent/cli_worker_status_test.go`: the recycled-pid regression guard
    (a live non-gopi pid is residue, never a worker), the bounded-residue-line
    test, the prune test (removes only dead pids), the dead-pid residue test,
    plus a real fixture (`gopiNamedChild`) that spawns a live process literally
    named `gopi` so the *positive* path (a state file that attests → a worker
    line) stays covered rather than skipped.
  - `cmd/agent/worker_status_residue_e2e_test.go` (new, CLI level): drives the
    **shipped binary** against a private `GOPI_HOME` seeded with a dead-pid and
    a recycled-pid record and asserts (a) no `gopi worker pid N` line exists for
    either, (b) the exact count is printed and the sample list is bounded, (c)
    `--status` created and removed nothing; plus the clean-profile case.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → **OK**
- Tests: `go test ./cmd/agent/ ./internal/standards/ ./internal/tools/ ./internal/procname/ -count=1` → see `## Verification output` below (job-2).
- **Real CLI on the live host** (fixed `bin/gopi`, same profile that produced the 4KB attachment):

```
$ ./bin/gopi worker --status
gopi worker pid 2140436: idle — gopi-worker worker (no state file — identified by argv; …)
gopi worker pid 2140461: idle — gopi-worker worker (no state file — identified by argv; …)
gopi worker: 70 state file record(s) that are NOT workers (dead pid, or the pid now belongs to another process): /home/user/.gopi/worker/worker-state-111220 (pid 111220 is gone — SIGKILL residue, no cleanup ran), …, … and 64 more (a worker clears them when it next starts)
```

  Two real workers, one bounded honest line — against the old output's 4 worker
  lines (2 of them fake) + 66 comma-joined paths. Independent check:
  `ls ~/.gopi/worker | grep -c worker-state-` = 70 **before and after** the
  status run → the status face removed nothing.
- **Prune, proven end-to-end** (private `GOPI_HOME`, `gopi worker --once`):

```
before: 3 state files (2 dead pids, 1 live pid)
gopi worker: removed 2 stale worker state file(s) left by dead or recycled pids
after: 1 → worker-state-1038891     # the LIVE pid's file survived
```

## Not Done

- **Did not remove the two stale resident workers** (2140436/2140461, running a
  deleted `bin/gopi` since 12:10). Restarting them is the operator's call
  (`gopi worker --restart`); killing a resident worker mid-round from a task
  session is out of scope here.
- **Did not touch the residue files already on the host** (`--status` stays
  read-only by design). They will be swept by the next worker start; `~/.gopi/worker/`
  still holds the pile until then. Deliberate: the fix must not make a status
  query a mutating act.
- **Did not chase the leaked test-fixture workers further** (the
  `TestGlmNightWatchOperatorPathStartsWorkersUnattended` fixture spawns real
  `gopi worker --worker-log-root …` children; those two pids were already gone).
  Whether that test reliably reaps its children is a separate investigation —
  the residue pile it feeds is now self-healing.
- `internal/tools.LiveStateFiles` (used by the **daemon** status face, not by
  this enumeration) still trusts name + liveness with no comm attestation. Left
  alone on purpose: it drives the daemon singleton/stop path, where changing the
  predicate has stop-semantics consequences; flagged here as the next candidate.

## Files

- `cmd/agent/cli_worker_status.go` (attestation, bounded residue, prune helpers)
- `cmd/agent/cli_tasks_worker.go` (call the prune at worker start)
- `cmd/agent/cli_worker_status_test.go` (unit/fixture tests)
- `cmd/agent/worker_status_residue_e2e_test.go` (new CLI-level tests)

## Verification output

```
$ go build ./...                                   # OK
$ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/  # OK (BUILD_OK)
$ go test ./cmd/agent/ ./internal/standards/ ./internal/tools/ ./internal/procname/ -count=1
ok  	github.com/nick/gopi/cmd/agent	471.655s
ok  	github.com/nick/gopi/internal/standards	13.174s
ok  	github.com/nick/gopi/internal/tools	145.465s
?   	github.com/nick/gopi/internal/procname	[no test files]
EXIT=0
```

