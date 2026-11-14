<!-- doc-class: historical -->
# Task Report: the resume path writes the ledger row's terminal status

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Epic** (as reframed, 2026-09-22): `gopi resume <task_id>` runs to completion —
exit 0, work committed, worktree reclaimed — but the row was NEVER re-closed, so
it stayed `failed` with `ended_at` frozen at the FIRST run's death forever. Wire
the resume path's terminal outcome to its row through the SAME close-out
implementation, carrying the real end reason, without a second status writer and
without faking the wrapper identity.

**Branch**: `fix/resume-terminal-status` (worktree
`/home/user/workspace/gopi-resume-status`).

## Root cause

The only in-process writer of a row's terminal status is the agent **end hook**
(`FinishSessionTask` → `finishSessionTaskRecorded`, `internal/agent/taskregister.go`). That writer acts
on whatever row the **start hook** latched into `a.autoTaskRowID`, and the start
hook had exactly two sources: `GOPI_TASK_WRAPPER_ID` (a `gopi worker`/`task run`
*child*, exported by the wrapper pre-spawn) or its own auto row. `gopi resume`
was neither: its `--task` linkage was only a **ledger tag** (`resume: task <id>
tagged resumed:<session>`), so the end hook ran with `autoTaskRowID == ""` and
`finishSessionTaskRecorded` returned early on `!a.autoTaskRegistered`. The
cleanup half was already fixed (the earlier resume-finish-cleanup epic) — which
is exactly why the residue was so quiet: everything the operator could *see* had
finished, while the row kept the dead run's verdict.

## Completed

- `internal/agent/taskregister.go`
  - `(*Agent).AdoptLedgerRow(taskID)` — the resume path's half of the start
    hook, and it sets **both halves of that hook's latch**: `autoTaskRowID`
    (which row) and `autoTaskRegistered` (there is a row to close). It MUST set
    the latch, because `maybeRegisterSessionTask` is documented "called once per
    **non-resume** run" — on the resume path nothing else ever sets it, and an
    adoption that only named the row left the end hook returning early on
    `!a.autoTaskRegistered` and the resume wrote **nothing at all** (measured;
    the row stayed exactly as the dead run left it). Setting it before the
    engine is also what keeps the liveness heartbeat on the continued row.
  - `(*Agent).latchWrapperRow()` — the two "this run attaches to an EXISTING
    row" gates (wrapper child, adopted row) in ONE place, returning whether the
    run attaches. **This is a bug fix inside the fix**: the first draft added a
    second gate *below* `maybeRegisterSessionTask`'s `a.SessionLog == nil` early
    return; a resume run has no session log at start-hook time, so the gate never
    ran and the whole change was a silent no-op (caught by running the fix, not
    by reading it).
  - **The end hook is UNTOUCHED.** A first draft ALSO latched at the end hook
    (for runs that skip the start hook) and that was a **measured regression**:
    the end hook CLEARS `autoTaskRegistered` by design ("one row per session —
    never updated twice") and legitimately runs twice
    (`TestAutoTaskRegisterOnAndEndHook`), so a gate that re-armed it on an
    existing `autoTaskRowID` re-armed it on a **spent AUTO row** and the second
    end hook mutated a row it must not touch —
    `second end hook mutated the row: … status: completed …`. With the latch set
    by the adopter instead, the auto path is bit-for-bit what it was.
- **No new field anywhere.** `AdoptLedgerRow` reuses the existing
  `a.autoTaskRowID` — that variable IS "the row this run closes out" (the start
  hook sets it for a wrapper child, the end hook writes it), so a second one
  would be the same fact twice (Rule 14). Reusing it also kept the change inside
  `taskregister.go` + `cli_resume.go`: a new `internal/agent/loop.go` field shifted every line
  after `type Agent struct` by +17, which broke the docanchor gate for **six**
  normative docs that anchor `internal/agent/loop.go` by line number
  (`hippocampus.md`, `agent-five-organs-ontology.md`, `docanchor-gate-bare-lineno.md`,
  `hippocampus-spec-report.md`, 2 DOC-INDEX rows). Those anchors were repaired
  once, then the field was dropped and the repairs reverted — fixing a resume bug
  by re-keying six unrelated documents is churn nobody asked for.
  It is deliberately NOT an environment variable and NOT
  `GOPI_TASK_WRAPPER_ID`: that variable means "this process is a task run's
  CHILD" and the worktree/console/main dispatchers consume that meaning; a
  resume that exported it would lie. A resume spawns no child, so there is
  nothing to inherit a value from anyway (Rule 14 — no `os.Setenv` for a value
  with one in-process reader).
- `cmd/agent/cli_resume.go`
  - `adoptResumeLedgerRow(ag, taskID)` — a named seam (so the wiring is
    assertable), called on the resume path **before the engine starts**, which
    is what makes this the *same* implementation as every other run rather than
    a resume-only writer.
- `cmd/agent/cli_resume_status_e2e_test.go` (new) — the acceptance, driven
  through the real binary and the real resume entry point: 4 tests
  (completion, failure, status-without-force-cleanup, refusal-leaves-the-row).

No second status writer, no `taskUpdate` in `cli_resume.go`, no
`taskAllowedTransitions` change, no weakened finish-cleanup refusal, no change
to sibling ticket `01a0c3db`'s surface.

## Not Done (honest)

- **The failing-resume case writes "task failed" as its reason.** The epic's
  constraint was "carry the same `internal/runend` reason rather than
  re-deriving from exitCode alone". That holds for the *decision*
  (`runend`/`finishSessionTaskRecorded` choose `failed`, so a
  truncated resume cannot close `completed`), and the run's own end reason is
  what the end hook computes. What the row's `reason` column shows in my rig is
  the generic `task failed` / `task finished`, because the close-out stamps the
  column from its own status vocabulary — refining that column to carry the
  truncation/error detail verbatim is a **separate, dispatchable** change on the
  close-out's reason vocabulary, not on this linkage. Named here rather than
  overstated.
- **`ended_at` for a successful resume is the end-hook's own stamp** (the moment
  the resumed run ended), which is what the epic asked for ("`ended_at` frozen"
  ⇒ it must move). It is not the commit time of the work.
- **The `resumeRunLog` filesystem fallback I first wrote was dropped.** It keyed
  the run calendar directly (`rundir.DirFor`) in case the ledger could not
  answer the target. Investigation showed `resumeTaskRawLog` already resolves a
  target that is a session UUID with no ledger read, and for a task-id target the
  ledger is open anyway (the operator's environment) — so the fallback accepted a
  regression risk (`Latest` without `WithCheckpoint` can pick a checkpoint's
  sibling raw log) for no reachable benefit. Dropping it kept the diff inside the
  reported defect.
- **Not covered here: a resume killed mid-flight leaves the row as it was**
  (rig case 5) — proven live, but the "no `session_end` in the log" variant that
  I wrote as a test first turned out to assert nothing useful (the end hook
  always writes for the run that just ended, because the end hook *is* what wrote
  that event), so it was replaced by the failed-run test. The killed-resume case
  has live evidence and no automated test; a test that kills a real resume
  mid-round is a heavier harness than this deliverable warranted.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → clean; `go vet ./internal/agent ./cmd/agent` → clean;
  `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK.
- Tests: `go test ./cmd/agent -run TestResume` → `ok github.com/nick/gopi/cmd/agent 17.795s`
  (this includes the pre-existing `TestResumeKeepsADirtyWorktree` and
  `TestResumeHelpDocumentsTheOrdering`, plus the new
  `TestResumeWritesTerminalStatus`, `TestResumeFailedRunLandsFailed`,
  `TestResumeWritesStatusWithoutForcingCleanup`,
  `TestResumeRefusedLeavesTheRowAlone`).
- Tests (branch scope): `go test ./internal/agent ./internal/standards ./internal/runend ./cmd/agent`
  — see the job output quoted in the session (the whole-tree suite is the
  off-peak heavy path, Rule 27).

### RED before the fix (the same test, the call site removed)

```
$ # comment out adoptResumeLedgerRow(ag, taskID) in cmd/agent/cli_resume.go, rebuild bin/gopi
$ GOPI_BIN=$PWD/bin/gopi go test ./cmd/agent -run 'TestResumeWritesTerminalStatus|TestResumeFailedRunLandsFailed'
--- FAIL: TestResumeWritesTerminalStatus (1.13s)
    cli_resume_status_e2e_test.go:186: a successful resume must close its row completed, got "failed"
        (the row still carries the dead run's verdict)
--- FAIL: TestResumeFailedRunLandsFailed (1.23s)
    cli_resume_status_e2e_test.go:233: a FAILED resume must land the row failed (never completed), got "in_progress"
FAIL
```

The first failure is the operator's report exactly: a resume that exited 0,
committed its work and reclaimed its worktree, with the row still `failed`.

### Ran: the real surface, five cases (Rule 31)

Rig: `/dev/shm/gopi-r31` (device 28 — off the `/` partition, Rule 22); a
scriptable SSE mock LLM (zero real API calls); one sandbox per case (its own
`GOPI_HOME`, ledger and sessions tree). Each row is stood up in the shape the
report describes by starting its own run and SIGKILLing the whole process group
mid-LLM-round-trip (`in_progress`, claim naming a corpse, a resumable
checkpoint), then running the real binary's resume.

**(1) `gopi resume <task-id>` on a successful resume — the headline case**

```
--- (s1) ROW BEFORE RESUME ---
task_id   status       ended_at  claimed_by     reason
t-r31-s1  in_progress            peerbox:2944855  work started
claim holder pid alive?  NO (pid 2944855 is dead — a resume is the only closer left)

$ gopi resume t-r31-s1                       # exit=0
gopi resume: task t-r31-s1 tagged resumed:f92ef6f6-8725-4537-b61c-8c24fb977e89
task ledger: adopted row t-r31-s1 for this run (continues an existing row; no auto mirror)
task ledger: completed t-r31-s1 (auto)

--- (s1) ROW AFTER RESUME ---
task_id   status     ended_at             claimed_by     reason
t-r31-s1  completed  2026-09-21 21:18:58  peerbox:2952372  task finished
```

**(2) a resume whose run FAILS — the row must land `failed`, never `completed`**

```
$ echo fail > mock-mode && gopi resume t-r31-s2b      # exit=1
Error: LLM call failed (stream: deepseek: API error (status 400): ...)

--- (s2) ROW AFTER FAILED RESUME ---
task_id    status  ended_at             claimed_by     reason
t-r31-s2b  failed  2026-09-21 21:19:59  peerbox:3003691  task failed
```

**(3) a refused resume — the row must be untouched**

```
--- (s3) ROW BEFORE ---   t-r31-s3  pending
$ gopi resume t-r31-s3                                # exit=1
gopi resume: cannot resolve session id "t-r31-s3": .../sessions: no such file or directory
--- (s3) ROW AFTER ---    t-r31-s3  pending            # identical: no write, no optimistic claim
```

**(4) the explicit form — `gopi resume <raw-log> --task <id>` reaches the SAME close-out**

```
$ gopi resume /dev/shm/gopi-r31/s4/home/.gopi/sessions/t-r31-s4/7621ffb7-….raw.log --task t-r31-s4
  → exit=0
task ledger: adopted row t-r31-s4 for this run (continues an existing row; no auto mirror)
task ledger: completed t-r31-s4 (auto)

--- (s4) ROW AFTER ---    t-r31-s4  completed  2026-09-21 21:20:18  task finished
```

**(5) a resume KILLED mid-flight — no optimistic write**

```
$ gopi resume t-r31-s5          # mock holds the LLM round forever
resume pid=3031792 alive=1 (blocked in a held LLM round)
$ kill -9 -3031792              # the resume dies mid-flight, like any other run
--- (s5) ROW AFTER A KILLED RESUME ---
task_id   status       ended_at  claimed_by     reason
t-r31-s5  in_progress            peerbox:3022771  work started   # unchanged — no `completed` appeared
```

**Final-code re-run of all five cases** (after the two post-green iterations, on
the committed binary; the rig allocates a fresh sandbox per case, so case ids
differ from the first run):

| case | before | invocation | exit | after |
|---|---|---|---|---|
| 1 success | `t-r31-v7 in_progress` (holder dead) | `gopi resume t-r31-v7` | 0 | `completed  2026-09-21 22:08:27  task finished` |
| 2 failure | `t-r31-v8 in_progress` (holder dead) | `gopi resume t-r31-v8` | 1 | `failed  2026-09-21 22:08:54  task failed` |
| 3 refused | `t-r31-w2 pending` | `gopi resume t-r31-w2` | 1 | `pending` — byte-identical, no write |
| 4 explicit | `t-r31-v9 in_progress` (holder dead) | `gopi resume <raw.log> --task t-r31-v9` | 0 | `completed  2026-09-21 22:09:05  task finished` |
| 5 killed resume | `t-r31-w3 in_progress` (holder dead) | `gopi resume t-r31-w3` then SIGKILL its group mid-round-trip | killed | `in_progress  peerbox:1228851  work started` — unchanged (no optimistic write) |

Case 1's stderr, verbatim (the whole feature in two lines):
`task ledger: adopted row t-r31-v7 for this run (a resume CONTINUES an existing row: …)`
then `task ledger: completed t-r31-v7 (auto)`.

Result: all five behave as specified — the fix closes the row with THIS run's
real outcome, refuses to fabricate one, and leaves a refused or killed resume's
row exactly as it was.

## Evidence (self-filed)

- `go test ./cmd/agent -run TestResume` → `ok github.com/nick/gopi/cmd/agent 17.795s`.
- Red proof above: 2 FAILs with the call site removed, on a rebuilt `bin/gopi`
  (the resume-finish-cleanup epic's first red attempt was fooled by a stale
  binary — rebuild after every edit).
- Rig evidence above: 5 cases, verbatim sqlite rows before/after, in
  `/dev/shm/gopi-r31/logs/*.resume.err` (`task ledger: adopted row …` then
  `task ledger: completed … (auto)`).
### Re-verification after the regression (the evidence below is the FINAL code's)

Two iterations happened after the first green run, both forced by re-running the
suites rather than trusting the unit-level pass:

1. **Dropped the new `Agent` field** (`ledgerRowID` → the existing
   `autoTaskRowID`). The field was not wrong, but a new field in `internal/agent/loop.go`
   shifted every line after `type Agent struct` by +17 and broke the docanchor
   gate for **six** normative docs that anchor `internal/agent/loop.go` by line
   number. Note the gate is red on **master** too (both master and branch fail
   `TestDocAnchorsResolveInNormativeDocs`; the listing diffs only in pre-existing
   master drift), so it could not be used as a green/red signal — the
   comparison against master is the measurement.
2. **Removed the end-hook latch** — the measured regression above, found by the
   full-package run, not by the resume tests.

Final verification on the frozen code: `go test ./cmd/agent -run TestResume`
→ `ok … 80.649s`, **27 tests PASS** (23 pre-existing + the 4 new);
`go test ./internal/agent -run TestAutoTaskRegister` → `ok … 20.205s` (the 7
auto-registration tests, including the one the end-hook latch broke). Rig
re-runs on the final binary: case 1 `completed`.
 The gate comparison vs master: `go test ./internal/agent -run
TestDocAnchorsResolveInNormativeDocs` differs from master only in rows that are
master's own drift already (a landed record's past-EOF line counts and the
landed-record row numbering in `devdoc/DOC-INDEX.tsv`) — no anchor this change
touched. Note this gate is RED on master too (231 listing rows), so it cannot be
used as a green/red signal in isolation; the comparison is the measurement.

- `git status --porcelain` in the worktree
  (`/home/user/workspace/gopi-resume-status`, branch `fix/resume-terminal-status`
  at `a7bb7f16`): prints nothing — observed after the commit, worktree clean
  (Rule 30).
