<!-- doc-class: historical -->
# Task Report: `gopi resume <session-id>` no longer latches a ledger row that cannot exist

Epic `resume-session-target-latch` (verdict DOABLE). Run at
`95693654` on branch `task/01a0edfaa30a7ef9a8d6ad396fe5d67d`.

## Completed

**The defect, and the one seam that caused it.**

`resolveResumeTarget` (`cmd/agent/cli_resume.go`) has two task-mode branches:

1. the **row view** — `resumeTaskRawLog` → `tools.TaskResumeTarget` → `TaskProcessStatusFor`,
   which returns only for a row the LEDGER holds (`taskproc.go:888`, error on unknown id);
2. the **run-directory** branch — `resumeRunLog` → `rundir.DirFor(target)` +
   `rundir.WithCheckpoint`/`RunDirRawLog`, a pure FILESYSTEM read addressed by the target string.

Both set `resumeTargetWasTaskID = true`, and `runResume` then did
`taskID = args[0]` from that flag alone. A **session uuid is the same address on branch 2**:
`internal/agent/session.go` `sessionDirForID` names a bare session's run directory
`<gopi dir>/sessions/<session-uuid>/` and the run path writes `<uuid>.raw.log` beside
`session.jsonl`, so `gopi resume <session-id>` resolves a real checkpoint and adopts
the session uuid as a ledger row. `Agent.AdoptLedgerRow` sets `autoTaskRowID` + `autoTaskRegistered`
and reports success; the start hook's `latchWrapperRow()` then returns `true`
(`taskregister.go:848`) and the run mints **no** auto mirror; `TaskTagResumed` and
`TaskUpdateReason` both fail against a row that cannot exist. Net result: three `[gopi]`
stderr lines and **no ledger record at all** for the resumed run.

**The fix (Rules 3/15 — same seam, no new mechanism, nothing silent).**

- `cmd/agent/cli_resume.go`
  - new `resumeTargetRowID` — set **only** on the row-view branch (to `st.TaskID`, the row's
    OWN id, never the operator's spelling). The run-directory branch leaves it empty.
  - `runResume` builds `taskID` from `resumeTargetRowID`, not from `resumeTargetWasTaskID`;
    a run-directory resolution prints one honest note (`resumeNoRowNoteFmt` via
    `reportResumeNoRow`) and runs **session-only**.
  - `adoptResumeLedgerRow` now GATES: a row that does not resolve through the same ledger
    row view (`tools.TaskStatusOf`, seam `resumeRowExists`) is **reported and not adopted**.
  - `resumeUsage`'s `<session-id>` paragraph documents the behaviour and the `--task <id>`
    remedy.
- `internal/agent/taskregister.go`: added the read-only accessor `Agent.AdoptedRowID()`
  (no new field — the docanchor `loop.go` line-anchor constraint is respected; `loop.go`
  was not touched).

`--task <id>` (explicit) is unchanged: naming a row is the caller's assertion, and a row
that is MISSING there is now reported instead of latching onto nothing.

## Not Done

- **A session-only resume still writes no ledger row.** This is deliberate and now
  REPORTED, not fixed here: the start hook is documented non-resume-only
  (`if !resume { maybeRegisterSessionTask(userInput) }`, `loop.go`) and adoption was the only
  thing that ever set the end hook's gate on a resume, so there is no row to write a terminal
  status onto. Doing it would mean either a second registration mechanism (Rule 3) or an
  adoption that fabricates a row for a target that named none — both worse than the honest
  note, and neither is what the acceptance asked for ("the absence of a row is reported
  honestly as a decision"). The note names the remedy (`--task <id>`) and `resume -h`
  documents it. **Dispatchable gap** if the operator wants a row for every resume.
- Adoption for an explicit `--task <id>` that names no row now reports; it does not refuse.

## Verification

- **Build:** `go build ./...` → OK; `go vet ./cmd/agent ./internal/agent` → OK;
  `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK.
- **Tests:** `GOPI_BIN=$PWD/bin/gopi go test ./cmd/agent -run 'TestResume' -count=1` → **ok**
  (includes the new `TestResumeSessionIDTargetAdoptsNoRow`, `TestResumeTaskIDTargetStillAdopts`,
  `TestResumeRowGateReportsANonResolvingAdopt`);
  `go test ./internal/agent -run 'TestAutoTaskRegister|TestAgent_Resume' -count=1` → ok;
  `go test ./cmd/agent -run 'TestGopiHelpSurface|TestUsage|TestSessionHelp'` → ok.
- **Ran (the surface changed, verbatim, in a sandbox profile — Rule 31):**

  ```
  SB=$(gopi sandbox create --label resume-latch | tail -1)      # /home/user/workspace/gopi-sandbox/sandbox-resume-latch-b2b02b73
  GOPI_HOME=$SB ANTHROPIC_BASE_URL=http://127.0.0.1:18123 … \
      echo 'reply ok' | bin/gopi                                  # one real session, mock LLM, exit 0
  GOPI_HOME=$SB … bin/gopi resume da892a10-ab50-4c21-8295-b1291f31cd00 cont.txt   # before: bin/gopi-before
  GOPI_HOME=$SB … bin/gopi resume 01a0ee15-6e4f-706d-878a-0bdaf0a03add cont.txt   # after: bin/gopi
  ```

- **Result — BEFORE (`bin/gopi-before`, built at the parent commit), exit 0:**

  ```
  [gopi] task ledger: adopted row da892a10-ab50-4c21-8295-b1291f31cd00 for this run (…)
  [gopi] resume: warning: task linkage failed (da892a10-… ← 17c52c1c-…): task: no task with task_id="da892a10-…"
  [gopi] task ledger: close failed (continuing): task: no task with task_id="da892a10-…"
  ```
  leaked-line counts: `adopted row`=1, `task linkage failed`=1, `close failed`=1;
  ledger after the run: the fixture row only — **no record of the resumed run**.

- **Result — AFTER (`bin/gopi`, same target, exit 0):** all three counts **0**, and one
  honest line instead:
  `[gopi] resume: da892a10-…: this target names no ledger row (resolved from its run directory, not from the task-id view) — continuing session-only: nothing is adopted, linked or closed (a resume CONTINUES work; it does not dispatch a row). Pass --task <id> to link the resume to a row.`

- **Result — task-id target, live (non-regression):** `bin/gopi resume 01a0ee15-…` on an
  `in_progress` row with a run directory → exit 0, `adopted row 01a0ee15-…`,
  `tagged resumed:9fb4d55f-…`, `task ledger: completed 01a0ee15-…`; the row went
  `in_progress → completed`, `ended_at` stamped.

- **Red proof (each half watched FAIL, then restored green):**
  - reverting only the resolution half (`resumeTargetRowID = target` on the run-directory
    branch, note removed) → `TestResumeSessionIDTargetAdoptsNoRow` FAILS
    (`stderr carries "task linkage failed"`); restored → ok.
  - reverting only the adopt gate (`ag.AdoptLedgerRow(strings.TrimSpace(taskID))`) →
    `TestResumeRowGateReportsANonResolvingAdopt` FAILS
    (`a non-resolving row must NOT be adopted, got "t-resume-latch-row"`); restored → ok.

## Evidence (self-filed)

- `grep -c` over the before/after stderr files, session-id target:
  `task ledger: adopted row` 1→0, `task linkage failed` 1→0, `task ledger: close failed` 1→0.
- `sqlite3 -readonly "$SB/.gopi/memory.db" "select task_id,status from tasks;"` after the
  session-only resume → `01a0ee0b-987d-74d1-9280-cc96b1239b38|completed` and nothing else —
  the honesty of the new note is measured, not asserted ("nothing is adopted, linked or
  closed" carries no claim of a row).
- Task-id live case: `select task_id,status,ended_at … where task_id='01a0ee15-6e4f-706d-878a-0bdaf0a03add'`
  → `in_progress|NULL` before, `completed|1790700782.0` after.
- Fixture row readability is asserted IN the test before the run
  (`tools.TaskStatusOf` on the sandbox ledger), so a double-pinned-nowhere fixture fails
  loudly instead of passing vacuously — that mistake cost two iterations here (the row was
  written into the suite's ledger while the child read the sandbox's).
- Sandbox/scratch: the live rig ran in `gopi sandbox create` output under
  `/home/user/workspace/gopi-sandbox/…`; the test suite pins its scratch to `/dev/shm`
  (device 28, vs `/` 2051) — Rule 22.

## Commit

`29caf15c fix(resume): adopt a ledger row only through the row (task-id) view`
