<!-- doc-class: historical -->
# Task Report: task-process-identity — zombie `in_progress` tasks become verifiable and resumable

Branch: `feat/task-process-identity` (worktree `~/workspace/gopi-task-identity`, off master `c817d680`)
Date: 2026-09-13

## The problem (what a zombie row actually costs)

`gopi task list --status in_progress` on 2026-09-13 showed five rows the ledger
called `in_progress`. Four of them were dead: workers killed by SIGKILL/Ctrl-C
whose rows were never closed. The status column cannot tell them from a live
run — it records "this row was never closed", nothing about the machine. The
consequences: (a) recovering a killed run meant hand-hunting a session id
before `gopi resume`, and (b) nothing prevented a second run of the same
context, because the only liveness evidence was a **pid** — and the OS recycles
pids, so a dead runner's number can be owned by an unrelated process that every
naive check reads as "still running".

## What was built

### 1. The process identity triple — recorded, not inferred

Three new columns on `tasks`: `proc_pid`, `proc_start_ticks`, `proc_cmd` (one
name per column: `tasksProcPidColumnName` / `tasksProcStartColumnName` /
`tasksProcCmdColumnName`, `internal/tools/tasks.go`).

They are written **atomically with the transition** that names a holder —
`taskUpdateStamped` (claimed→in_progress) and `TaskStampSelfClaim` — so a row
can never carry a holder without the facts that make the holder checkable.
They are **cleared** on every terminal transition (a stale identity left behind
would let a later probe compare a fresh pid against an old record).

Why three factors instead of a pid:

| factor | what it defeats |
|---|---|
| pid | nothing on its own — the OS reuses numbers |
| **start ticks** (`/proc/<pid>/stat` field 22) | **pid recycling**: unique per process *incarnation* |
| command line | a same-pid non-runner; also the record's human-readable face |

The ALTER migration reuses the existing idempotent `tasksP2Columns` pattern
(`tasksProcIdentityAlters()`), and the legacy-table rebuild copy
(`colExpr`) carries the same three columns — schema, ALTER list, rebuild copy
and row projection all reference the same constants (Rules 1/13/14).

### 2. One verdict function, one ladder (`internal/tools/taskproc.go`)

`taskIdentityVerdict(pid, recorded)` is the **single** implementation of "is
this pid MY process?"; every consumer calls it, so no two can disagree:

```
pid gone                     → dead      (definite: the run ended)
start ticks differ           → recycled  (definite: the number was REUSED)
cmdline differs              → recycled  (definite: a different program)
start + cmdline agree        → running   (definite)
no identity on the row       → the pre-existing name-guarded pid check
                               (legacy rows: no verdict stronger, none weaker)
unreadable procfs / race     → unknown   (NEVER a fabricated death — Rule 15)
```

Degradation is deliberate: on a host without procfs (non-Linux) nothing is
read and the verdict is `unknown`, never `dead`.

### 3. `gopi task status <task-id> [--json]` — the definite answer

Prints the ledger status, the verdict, and **every raw fact behind it**
(recorded vs live start time and command line, holder, freshness), so the
decision is auditable without reading `/proc` by hand. The exit code carries
the verdict (`0` resumable / `1` running / `3` unverifiable) so scripts need no
parsing.

### 4. `gopi resume <task-id>` — the zombie-recovery path

Previously the resume target had to be a raw-log path or a session id. Now a
**task id** is a first-class target: the row's own session checkpoint is
resolved from `tasks.session_id`, and the row is auto-linked (no `--task`
repetition) — the session-id hunting is gone.

**The gate is the machine, not the status string.** `decideResumeByTaskID`
refuses when the verdict is `running` (double-run) or `foreign`/`unknown`
(cannot prove the run is over here), and proceeds for
`dead`/`recycled`/`no-holder` — i.e. it reclaims zombies.

### 5. Ownership handoff on resume (a bug this task found and fixed)

The first end-to-end run exposed a real hole: after a successful resume the row
still named the **original, dead** pid, so a second concurrent
`gopi resume <task-id>` saw `dead` and would have started a second run of the
same context. Fixed by stamping the resuming process as the row's holder
(`TaskStampSelfClaim`) after the gate passes, with its own identity — so while
that run lives its successor's gate sees a verified-live holder and refuses,
and if it dies the row flips back to `dead` and is resumable again. Verification
below is the transcript of exactly that sequence.

## Verification

Build: `go build ./...` → **OK**. `CGO_ENABLED=0 go build -o /dev/shm/gopi-ident-bin ./cmd/agent/` → OK
(mandatory: the cmd/agent e2e suite refuses a stale `bin/gopi`).

Tests — `go test ./internal/tools/ ./internal/standards/ ./cmd/agent/ -count=1` → all **ok**
(new: `internal/tools/taskproc_test.go`, `cmd/agent/cli_resume_gate_test.go`).

Pre-existing failures elsewhere in `go test ./...` (`internal/stylegate`,
`internal/testutil`) were confirmed **identical on pristine master** (`git stash`
→ same failures → `git stash pop`), i.e. not caused by this change.

### Real end-to-end transcript (built binary, sandboxed ledger)

Zombie recovery — live → SIGNAL → dead → resume → lineage → refusal:

```
$ gopi task status t-e2e-hand          # a real worker running a 300s sleep
verdict:     running
evidence:    pid 1882668 is alive and matches the recorded identity
             (start=2026-09-13T07:53:38+08:00, cmdline identical)
resumable:   no                        # exit=1 → do not resume

$ kill -9 -<pgid of 1882668>           # SIGKILL the worker

$ gopi task status t-e2e-hand
verdict:     dead
evidence:    pid 1882668 is gone — the run ended (killed/crashed) and the row
             was never closed
resumable:   yes                       # exit=0 → reclaimable

$ gopi resume t-e2e-hand <continuation>        # resumed BY TASK ID, no session hunting
                                               # …runs through to completion

$ gopi task status t-e2e-hand                  # while the resumed run A was live
verdict:     running
evidence:    pid 1882804 is alive and matches the recorded identity
             (start=2026-09-13T07:54:22+08:00, cmdline identical)
holder:      peerbox:1882804 (claim stamp is fresh for this run)

$ gopi resume t-e2e-hand                       # resume B while A runs
gopi resume: refusing — task t-e2e-hand has a verified-LIVE holder process;
             resuming would run it twice.
  Check with: gopi task status t-e2e-hand

$ gopi task show t-e2e-hand | grep -E "tags:|claimed_by:"
tags:        resumed:aa68ad07-08ff-43f0-95a9-e3d2de942268
claimed_by:  peerbox:1882804 (at 2026-09-13 07:54:24)
```

Also verified: a `pending` row → `verdict: n/a`, and a `completed` row rendered
by `task show` → `process: (none) (row predates the process-identity columns…)`.

The false-live regression is pinned by test rather than by waiting for a real
pid wrap: `TestTaskStatusRecycledWhenStartTimeDiffers` drives the ladder with
the live pid and a start time off by one tick — the exact shape of a reused
number — and asserts `recycled`, then asserts the agreeing identity still reads
`running` (the check is a comparison, not a blanket refusal).

## Files

- `internal/tools/taskproc.go` (new) — identity capture/verify, verdicts, `task status` report + JSON
- `internal/tools/taskproc_test.go` (new) — 12 tests pinning capture, verdict ladder, leak guard, degradation
- `cmd/agent/cli_resume_gate_test.go` (new) — the resume gate's six outcomes
- `internal/tools/tasks.go` — schema + ALTER + rebuild copy + projection, `scanTaskInto` (one scan for both readers), identity write/clear, `task show` line, `FreshnessStale`
- `cmd/agent/cli_tasks.go` — `task status` verb, `--json`, flag whitelist, usage
- `cmd/agent/cli_resume.go` — gate, task-id target, ownership handoff, usage
- `cmd/agent/cli_completion.go` — `status` in the tab set

## Not Done (honest list)

- **No `task_proc_*` fields travel through the memory-sync path** — the sync
  projection is executor-owned (`claimed_by`/`claimed_at` are deliberately
  excluded there too), so a row synced from another host arrives without an
  identity and its verdict degrades to `unknown`/legacy. Deliberate, but it
  means cross-host rows are not yet verifiable from the receiver.
- **`procClockTicksPerSec = 100` is a documented assumption** (no CGO
  `sysconf`); it affects only the *display* of a start time, never a verdict —
  comparisons use raw tick counts.
- The 352 `%v`-wrap baselines and the other pre-existing `go test ./...`
  failures are untouched (out of scope).
- Resume B's refusal was verified by transcript, not by a unit test (the
  background-process orchestration is not deterministic enough for CI).

## Deliverable location (Rule 6d)

- **Branch:** `feat/task-process-identity` (in the main repo's refs — worktree-shared, no push needed)
- **Recover:** `git show feat/task-process-identity:internal/tools/taskproc.go` /
  `git show feat/task-process-identity:devdoc/task-process-identity.md`
- **Commit:** see `git log --oneline -1` on that branch.
