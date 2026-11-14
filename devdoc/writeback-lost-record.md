<!-- doc-class: normative -->
# A refused terminal writeback gets a record that outlives the process

**The defect.** `writebackOutcome` returns the LOCAL ledger's verdict on a row's
terminal write (`cmd/agent/cli_tasks.go:4278`). The success arm has read that
value since epic `worktree-dirty-residue` — `taskRunCloseoutLanded` turns a
non-landing close-out into an escalation line and a non-zero exit. The **five
failure arms did not**: each called

```go
writebackOutcome(target, tools.TaskStatusFailed, …, opts.reportTo, opts.repo)
```

as an expression statement, so a refusal — an illegal transition, a busy or
read-only ledger, a process killed mid-write — was discarded, and the arms whose
own gates DO record a refusal (Rule 30's dirty/residue arms) guard only the
DELIVERY claims (`completed`/`verified`).

**What that looked like on this host's ledger (2026-10-27).** Four rows on one
peer whose holder pid was long dead: three labelled `[died …]`, one carrying no
claim stamp at all. Every one of them was still `in_progress`, and nothing on
the row — no trail event, no verdict line — accounted for the terminal write
that never landed. The wrapper's own stderr line was the only account, and it
belonged to a process that had already exited.

**The fix, in three pieces.**

1. `internal/tools/task_writeback_lost.go` — `TaskWritebackLost(taskID, status,
   wbErr)` records the loss in the two places that survive the dying process:
   the row's own `task_events` trail under `WritebackLostEvent`
   (`"writeback-lost"`), and the notify pipeline under the same name. One
   constant, two consumers (Rule 1b/13): the entry in `taskNotifyAlarmEvents`
   and the entry in `verdictTrailEventNames` both reference it, so a rename
   cannot make the alarm and the `task show` render disagree. When even the
   trail write fails, the sentence SAYS so and carries the reason — a reporter
   that hides its own failure is the defect it exists to catch.

2. `cmd/agent/task_run_writeback_lost.go` — `writebackFailure(...)` is the ONE
   place a failure-branch writeback is allowed to happen. It calls
   `writebackOutcome`, reads the error, and on a refusal routes it through
   `TaskWritebackLost` and prints the resulting sentence on **both** streams:
   the run log (`execRunFacts`, the copy that survives the session directory)
   and the spawning stderr (the copy a detached worker's reader can see before
   they know the log's path). It is deliberately VOID: on these arms the run is
   already failing and about to return, so there is no second decision — the
   value that must not be dropped is the RECORD.

3. The five arms now call `writebackFailure`. The success arm still calls
   `writebackOutcome` directly and keeps its `wbErr` — that value drives the
   landed check, and converting it to the void seam would delete a decision.

**What this must never become.** A disposer. The row stays open and
`in_progress` with its dead holder: that is the operator's ruling
(`devdoc/zombie-sweep-removed.md`, 2026-10-26). The sweep that used to rewrite
such rows is GONE and must not be resurrected under a new name. Reporting is the
whole deliverable; rewriting the status to make the report unnecessary would be
that sweep wearing this file's name. Both the seam's header and the new tests
assert the row does not move.

**The gate.** `cmd/agent/task_run_writeback_lost_gate_test.go` parses the
production file and fails on any bare `writebackOutcome(...)` EXPRESSION
STATEMENT (naming the offending line), with a floor on `writebackFailure` call
sites so an edit that deletes the arms cannot read green, and a contrast
assertion that the success arm is still an ASSIGNMENT. A source-level property
is the only honest instrument here: a discarded error is invisible at runtime
precisely because nobody looks at it.

**Verification measured on this rev.**

- `go build ./...` — clean.
- `go test ./internal/tools/ -run TestTaskWritebackLost` — 3 tests, PASS.
- `go test ./cmd/agent/ -run 'TestNoBareWritebackOutcomeCallSurvives|TestWritebackFailureSeamIsActuallyUsed|TestWritebackSuccessArmStillReadsItsOwnVerdict|TestWritebackFailure'`
  — 6 tests, PASS.
- Red proofs (mutate → red → restore → green), each restore verified by content
  hash against a snapshot taken before the mutation:
  - drop the `verdictTrailEventNames` entry and the `taskNotifyAlarmEvents`
    entry → `TestTaskWritebackLostJoinsBothRegistries` names both registries;
  - revert ONE arm to a bare `writebackOutcome(...)` statement →
    `TestNoBareWritebackOutcomeCallSurvives` prints `cli_tasks.go:2951`;
  - make the seam discard the refusal (the pre-fix shape) →
    `TestWritebackFailurePutsARefusedWriteOnBothStreams` reports the run log
    carrying no verdict;
  - print the sentence unconditionally on both faces → the dedup test counts 2
    on the operator's stream.

**Live end-to-end, on a throwaway profile** (`gopi tool sandbox create` under
`/dev/shm/wbl-tmp`, `GOPI_HOME`+`GOPI_MEMORY_DB` pointing inside it, a binary
built from this tree). To make the ledger REFUSE a terminal write without
faking the seam, the sandbox's own ledger carried a `BEFORE UPDATE OF status ON
tasks` trigger that aborts on `failed`/`completed`:

```sql
CREATE TRIGGER e2e_refuse_terminal_write BEFORE UPDATE OF status ON tasks
WHEN NEW.status IN ('failed','completed')
BEGIN SELECT RAISE(ABORT,'e2e fixture: the terminal write is refused by the ledger'); END;
```

Then a real worker run on a real row. What the three surfaces said afterwards:

- **row** — `status: in_progress` (it stayed OPEN; the report did not dispose
  it), and `task show` carries
  `verdict.event: writeback-lost` with the ledger's own words
  `constraint failed: e2e fixture: the terminal write is refused by the ledger (1811)`;
- **notify** — `~/.gopi/notify/task-events.jsonl` carries the alarm with
  `"event":"writeback-lost"` and the identical sentence;
- **stderr** — the worker stream carries the same line (`proof.out:11`).

A SECOND measured defect was found by that live run and is fixed here: before
the spawn seam runs, `execRunFacts` IS `os.Stderr`, so printing both faces
unconditionally put the sentence on the operator's stream **twice** (observed
verbatim in the live proof). `writebackFailureSameSink` now skips the second
write when the two destinations are the same file. The first version of the
dedup test PASSED against the un-deduped seam, because it set the latch before
`captureStderrStream` swapped `os.Stderr` — the test now establishes the
same-sink state INSIDE the capture, and the red proof above is what proves it
measures something.

**One thing this change does NOT do**, stated so nobody reads it as covered: it
does not retry the refused write and does not reconcile the row. A row whose
terminal write was refused is still a row an operator (or the next pass) must
look at — it is now a row that SAYS SO.

