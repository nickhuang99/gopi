<!-- doc-class: normative -->
# Resume's blind spot: a ground in use that the ledger cannot see

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Status: implemented 2026-10-07 · row `01a11561` · branch `task/01a11561a9657ed0afb520e5537ba991`

## The report, and what it turned out to be

The operator's shape: `gopi session resume <task-id>` was running, a live gopi process
was writing files in the task's worktree, and the ledger showed `holder: (none)` +
`resumable: yes`. Was that deliberate, or an oversight?

It is **one deliberate mechanism with an oversight on top of it**, and the two halves
have to be separated before either can be judged.

### Q1 — the entry point

`gopi session resume <task-id>` → `cmd/agent/cli_resume.go:468 runResume` → the
process-identity gate at `cmd/agent/cli_resume.go:533` (`decideResumeByTaskID`, defined
at `:136`) → the ownership handoff stamp at `cmd/agent/cli_resume.go:571`
(`tools.TaskStampSelfClaim`).

### Q2 — why no claim write lands

`TaskStampSelfClaim` delegates to the same writer as the ordinary holder stamp, and that
writer is **pinned to one status** (`internal/tools/tasks.go:7710-7712`):

```sql
UPDATE tasks SET claimed_by = ?, claimed_at = ?, in_progress_at = ?, updated_at = ?,
                 proc_pid = ?, proc_start_ticks = ?, proc_cmd = ?
 WHERE task_id = ? AND status = ?        -- taskStatusInProgress
```

Resume's ordinary targets are exactly the rows this pin excludes: a `failed` row, or a
row whose run already closed. On such a row the UPDATE **matches zero rows** — and it
**does not error**: `ExecContext` returns success for a zero-row match, so the handoff
silently no-ops. The row therefore keeps `claimed_by = ""` and all three identity
columns empty, which is literally what `gopi task status` renders as `holder: (none)`,
`recorded: (none) (row predates the identity columns)` and `verdict: n/a`.

### Q3 — deliberate, or oversight?

**Both, at different layers, and the boundary is sharp.**

- **Deliberate**: the status pin itself. Resurrection of a closed row's claim stamp is
  not something a resume should be able to do — the pin is what stops a resume from
  rewriting the ownership record of a row the ledger has already closed.
- **Oversight**: the *consequence*. Because the verdict ladder is deliberately
  status-blind (`internal/tools/taskproc.go`, the `!taskRunRowIsLive(t.Status)` arm), a
  terminal row answers `resumable: yes` **unconditionally** — the ledger is asked a
  question it structurally cannot answer, and nothing asks the second question ("is
  anyone actually in the directory?"). This was **already written down** before this
  task, in `devdoc/resume-on-failed.md:75`: *"the gate is deliberately status-blind
  (its own usage text: 'GATED on the row's process identity, never on its status
  string'), which is correct for its purpose (double-run prevention) — but it means a
  terminal row is always resumable, and nothing downstream records what the resume
  did."* The same file measured the sibling half of the same blind spot: no
  `task_events` row was written by a resume at all.

So: the no-op stamp is by design; **"a terminal row always reads resumable, whatever is
standing in its directory" is the defect**, and it is a defect of *omission* — one
question was never asked, never a mechanism that was chosen wrongly.

### Q4 — which columns feed `status`'s holder cell

`claimed_by`, `claimed_at`, `in_progress_at` (the constant is
`tasksInProgressAtColumnName`, `internal/tools/tasks.go:268`) plus the identity triple
`proc_pid` / `proc_start_ticks` / `proc_cmd` (`:294-296`). Nothing in that set is
written by anything except `TaskStampHolder`/`TaskStampSelfClaim`, and both carry the
same `status = 'in_progress'` pin — which is why the cell is empty on every terminal row
by construction, not by accident of this incident.

## The removal arms were NEVER the problem (measured, contradicting the briefing)

This task's briefing assumed the sweep/reclaim path would **wrongly delete** such a
worktree. **That belief is not supported by measurement**, and it is recorded here as
contradicted rather than inherited.

Run on this host, 2026-10-07, with three rows whose grounds each held a live gopi run
(`01a11548…`, `01a11561…`, `01a114cb…`):

```
$ gopi task sweep-worktrees --dry-run
  keep  [self-checkout] age=0h /home/user/workspace/gopi-01a11561a9657ed0afb520e5537ba991
  keep  [run-ground-anchor] /home/user/workspace/gopi-01a11561a9657ed0afb520e5537ba991
        kept: the launch context that took this ground (pid 827522) is still alive
  215 collectable, 31 keep   (totals unchanged before and after this change)
```

The worktree sweep asks `scratch.HeldByProcesses(reg.Path, holders)` over
`scratch.LiveHolders()` on **two** separate rungs (cwd/fd/exe/maps), and the
ledger-keyed reclaim arm asks `finishCleanupWorktreeInUse` at its guard ③
(`internal/tools/task_reclaim.go:639`). A live process standing in a ground therefore
kept it on **both** paths already. The gap was in the **status/resume reading alone**.

That distinction mattered for the fix: the correct move was to make the STATUS surfaces
read the evidence the removal arms already trust — **not** to add a second liveness
probe. A second probe is how a live worktree gets deleted (memory #2632).

## The fix — ONE criterion, consumed everywhere

`internal/tools/task_ground_in_use.go`:

- `TaskGroundInUse(dir) (holder, reason)` — the live holder standing in a ground, or the
  explicit absence. It is a **thin face over the two calls every removal arm already
  pairs**: `finishCleanupWorktreeInUse` (the shared cwd scan) plus
  `finishCleanupOrphanCwdInheritorReason` (the orphan reading, which may only ever
  *relax* a refusal). Its output strings are the same evidence a refused sweep prints —
  one reading, one sentence (Rule 14).
- `taskGroundInUseForRow(deliverable)` — the same question for one ledger row, asked of
  the row's own `deliverable.worktree`.
- `TaskStampDeliverableGroundForTest` — the exported seam onto the executor's own
  backfill writer, so a test can build the terminal-row shape without a second writer
  (precedent: `MergeVerifySetDeliverableBranchForTest`).

Consumers, all of them existing surfaces (no new verb — Rule 11; no new artifact —
Rule 35):

| surface | change |
|---|---|
| `gopi task status` | new `ground:` / `ground_now:` / `ground_why:` lines + `ground_in_use` fields in `--json`; `resumable` may only become **more** conservative |
| `runGroundOccupied` (run-ground anchor) | now a boolean face of the same criterion |
| reclaim guard ③ | now calls the same criterion (was the same two calls, inlined) |

**Two consumers were built and then WITHDRAWN — the resume gate and the retry gate do
not ship, and the paragraphs below are kept as the record of why.** I had added a
`resumeGateRefuseGround` verdict to `gopi session resume` and a ground arm to
`TaskRunChildResumable`; both are reverted (`cmd/agent/cli_resume.go`,
`internal/tools/task_run_retry.go` back at their base, their tests removed) after a
full-suite run caught a real regression: `gopi worker --resume --branch` spawns a
process whose **cwd is the worktree** and then resumes that same row — the resumer is
itself the live process standing in the ground, so the gate refused every worker resume
(`TestWorkerResumeEmitsTheCloseoutReportOnce`, code=1). **A removal arm asks "may I
DESTROY this directory?" (any live process = no, my own tree included); an admission
gate asks "may a SECOND run start here?" — a different question, already answered by
the process-identity ladder (pid + start ticks + cmd).** Reusing the removal criterion
as an admission veto is over-broad; the ground axis therefore ships on the surfaces
that read and remove a ground, never on the one that admits a run into it.

**The verdict labels are untouched.** `Verdict` still reports the ledger's own answer
about the row's *holder*; the ground axis is carried in its own fields and its own line.
That separation is deliberate: the enum is read by the retry ladder, the resume gate and
the CLI exit code, and "the row's holder is alive" is a genuinely different statement
from "somebody is standing in the row's directory". The status verb's machine contract
gained a code to match — **0 resumable / 1 running / 2 ground-in-use / 3 unverifiable** —
because those two refusals send an operator to different places.

## Verification

Red-then-green, with a **real** process (never a forged pid — a synthetic pid would
prove nothing about /proc, which is the evidence channel, Rule 24):

```
$ go test ./internal/tools/  -run TestGroundInUse -count=1     # criterion disabled (red proof)
--- FAIL: TestGroundInUseTerminalRowIsNotResumable
    ground_in_use = false while a live process stands in … — a caller reading
    "resumable" would admit a second run into a working directory

$ go test ./internal/tools/  -run TestGroundInUse -count=1     # green
ok  github.com/the operator/gopi/internal/tools   …
$ go test ./cmd/agent/ -run 'TestResumeGate|TestStatusExitCodes'
ok  github.com/the operator/gopi/cmd/agent        1.291s
```

(The red run above ALSO failed a `TestRetryGateRefusesAnOccupiedGround` — that test
belonged to the retry gate this change then withdrew, and it is gone with it: it was
the first signal that the removal criterion does not belong on an admission gate.)

**A red proof that lied, and its repair.** The first red run came back GREEN. The cause
was in the *test*, not the fix: the fixture used `t.Skipf` when /proc could not yet
confirm the stand-in holder, so with the criterion disabled the assertion never ran —
the suite passed for the wrong reason. The fixture now FAILS on an unobservable holder
and polls a bounded window for visibility; the second red run produced the two failures
above. This is recorded because a red proof that cannot go red is worse than none.

Field run (Rule 31), in a sandbox (`gopi tool sandbox create --label groundstatus`, the
real verb — plain `gopi sandbox` is retired), against a real seeded terminal row (a
`failed` row that walked `pending → in_progress → failed` through the production
writer):

```
$ GOPI_HOME=$SB gopi task status $ROW                          # nobody in the ground
status:      failed
verdict:     n/a
holder:      (none)
recorded:    (none) (row predates the identity columns)
ground:      /home/user/workspace/gopi-sandbox/sandbox-groundstatus-43aa024e/ground
ground_now:  no live process stands in it                          EXIT=0

$ (cd $GROUND && exec sleep 240) &                              # a REAL holder
$ GOPI_HOME=$SB gopi task status $ROW
verdict:     n/a                     <- the ledger's answer is UNCHANGED
resumable:   no                      <- the ground axis decides
ground_now:  IN USE by live process pid 963978 (sleep) argv="sleep 240" ppid=963356
             — cwd …/sandbox-groundstatus-43aa024e/ground
ground_why:  worktree in use by live process pid 963978 (sleep) argv="sleep 240"
             ppid=963356: cwd …                                 EXIT=2

$ GOPI_HOME=$SB gopi session resume $ROW                        # bare row id
gopi session resume: refusing — task $ROW's ledger entry is free (no live holder),
but the ground it RECORDS is in use right now, so a resume would put a second run
into the same directory.                                            EXIT=1
                                                                 # ^ WITHDRAWN wire:
                                                                 #   this refusal does
                                                                 #   NOT ship — see the
                                                                 #   withdrawal note above

$ GOPI_HOME=$SB gopi task status $ROW --json
{verdict: "n/a", resumable: false, ground_in_use: true,
 ground_holder: "pid 986283 (sleep) argv=\"sleep 240\" ppid=986281"}

$ kill $HOLDER                                                  # counter-case
$ GOPI_HOME=$SB gopi task status $ROW
ground_now:  no live process stands in it ; resumable: yes           EXIT=0
$ GOPI_HOME=$SB gopi task status $ROW --json
{verdict: "n/a", resumable: true, ground_in_use: false}     <- the false TRAVELS
$ GOPI_HOME=$SB gopi session resume $ROW
[gopi] resume: working in deliverable.worktree …   <- NOT refused on the ground axis
```

TWO MEASURED CORRECTIONS this field run forced, both recorded because the doc would
otherwise carry a transcript nobody can reproduce:

1. **`gopi session resume --task <id>` resolves nothing by itself.** The gate's
   `taskID` comes from either the positional target's ROW VIEW (`TaskResumeTarget`) or
   the `--task` flag, and `--task` is parsed from `args[1:]` *after* `args[0]` is
   resolved as a log path — so `resume --task 01a11561-…` died in
   `resolveResumeTarget` with *"resume target "--task" is neither a raw log path nor
   an existing session id"* (`cmd/agent/cli_resume.go:468`). The shape that reaches
   the gate is the **positional row id** `gopi session resume <task-id>`, whose gate
   needs the row VIEW to answer — i.e. the row's run directory must hold a resolvable
   checkpoint (`sessions/<task-id>/<task-id>.jsonl` naming a raw log, or the
   co-located file). Measured both ways on the fixture: with only a bare `.raw.log`
   the target resolved through the RUN-DIRECTORY branch and the gate was **skipped
   entirely** (session-only: *"this target names no ledger row … Pass --task <id> to
   link the resume to a row"*); once the `session_start` record named the checkpoint,
   the refusal above fired.
2. **`ground_in_use: false` was missing from the JSON.** The field carried
   `omitempty`, so the FREE case shipped `resumable: true` with no
   `ground_in_use` key at all — the answer a machine consumer asked for, erased by
   the zero value (Rule 15a). Fixed and pinned by
   `TestGroundInUseWireKeepsTheFalse`.

## Field recipe — how to decide this on a live row

```bash
gopi task status <task-id>            # exit 0 resumable · 1 already running ·
                                      # 2 the recorded ground is in use · 3 unverifiable
gopi task status <task-id> --json | jq '{verdict,resumable,ground_worktree,ground_in_use,ground_holder}'
```

`ground_in_use: true` with `verdict: n/a` is the shape this document is about: the row
is closed, and somebody is still in its directory. Do not resume; let the standing run
finish, or clear it once it is genuinely gone (re-check — the answer flips the moment
the process exits, which is exactly what the counter-case above shows).

## Damage list, honestly

- **Nothing was ever destroyed.** Both removal arms already refused; no worktree was
  collected while in use. The damage was **misinformation**: an operator reading
  `resumable: yes` on such a row had a documented green light to start a second run
  into a working directory.
- **Not fixed here**: `devdoc/resume-on-failed.md`'s two other findings — a resume writes
  no `task_events` row, and `TaskResumeTarget` silently falls back to the row's own
  session — are untouched by this change. Both remain live and are named in the task
  report's `## Not Done`.
- **Coverage boundary**: the criterion reads the row's `deliverable.worktree` only. A
  live run standing in a directory that **no** row records is invisible to the status
  surface (it is not invisible to the sweep, which scans every registration).
