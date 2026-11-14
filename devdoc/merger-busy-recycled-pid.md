<!-- doc-class: historical -->
> **CORRECTION-ANCHOR (2026-09-19).** The `--stop` mention below is a report of that
> run; the whole `--stop` / `--status` display surface was deleted with the resident
> merger (`f9ff1a89`, `55c4d6f3`). Every spelling of `gopi merger` now refuses
> (`mergerRetiredCause`, exit 1); the one-tick merge is the bare `gopi merge`.

# Task Report: the merger display no longer claims an IN-FLIGHT pass for a pid the kernel recycled

Branch: `fix/merger-busy-liveness` (worktree `~/workspace/gopi-busy-liveness`)
Epic: merger `--status` / `--stop` could render `", IN-FLIGHT pass (started …)"` for a
merger that is NOT running, because `MergerState.Busy()` is a pure timestamp
comparison over a state record whose pid the kernel had RECYCLED to a live
unrelated process (so the reader's `PidAlive` filter passed it).

## Completed

- **Reproduced the defect (RED, recorded).** `cmd/agent/merger_busy_recycle_red_test.go`
  plants the exact shape: a record for pid P (in-flight pass, one hour old) whose
  live process at P is a REAL holder of the private profile's singleton flock —
  i.e. "a live process stands at the dead writer's number". Ran against the
  pre-fix tree (`9a3b9f35^`, with the fixture pointed at the old API), the status
  face failed with the defect printed verbatim:

  ```
  --- FAIL: TestMergerStatusRendersInFlightForARecycledPid
      merger --status must state that no pass is in flight (Rule 15: no silent omission), got:
          in flight:   pass started 2026-09-16T14:33:27+08:00 (elapsed 1h0m0s)
  ```

  and `gopi merger --stop` printed
  `current state — pid 352938, …, IN-FLIGHT pass (started …)` for the same
  record. Both faces are covered by the test (one arm each); the status output
  was also observed in the same IN-FLIGHT shape with a record whose live pid did
  NOT hold the flock (the display consumes the state file, not the lock).


- **Fix: the stamp is attributable to the process that wrote it.**
  - `MergerState.ProcStart` (`proc_start_ticks`) — the writer's own process start
    time (`/proc/<pid>/stat` field 22), filled by `MergerWriteState` when the pid
    IS the calling process (`NewMergerStateWriter` measures it once), so a writer
    can never stamp a record with a stranger's incarnation.
  - `MergerStateFile.WriterLive()` — the ONE reader-side identity check, in the
    struct that already owns the pid/raw record: pid alive AND the live
    incarnation equals the recorded one. Read through `internal/procname`
    (`StartTicks`), the same starttime technique the tasks-ledger claim lock uses
    (`proc_pid` + `proc_start_ticks`, `internal/tools/taskproc.go`).
  - **`Busy()` stays a pure timestamp comparison** over writer-filled fields: it
    is now `WriterLive() AND State.Busy()` at the FILE layer — exactly the
    layering the previous epic established (record verdict vs process
    measurement), with the measurement strengthened from "something alive" to
    "the writer".
  - Degradation is explicit and fail-open (Rule 15): no pid / dead pid → not
    busy; a record with NO stamp (an older binary's file) → the bare liveness
    answer, i.e. exactly the pre-existing behaviour, so files already on disk are
    unaffected; an unreadable live incarnation → not attributable.
  - `Report()` / `Render()` and the status in-flight block follow the same
    verdict without deleting the record: a post-mortem rendering states the pass
    INSTANT (`last pass started … — NOT in flight (the record's writer is not
    running)`) instead of claiming `IN-FLIGHT pass`, and the recycled case is
    named (`… the pid was RECYCLED to a live unrelated process …`). The status
    label follows the verdict (`last record:` vs `in flight:`), and the
    "unknown" note is printed when a live writer exists but no record could be
    attributed to it.
  - No new authority for "is a merger running": liveness is still `PidAlive`
    (+ the singleton flock, which `--status` still prints as the authority). The
    flock-owned paths (`MergerStopTargets`, `mergerTargetsDescribe`) are
    untouched, and the stale-record line they already print
    (`… names pid N, which does not hold the singleton lock …`) is what a
    recycled record now reads there.
  - No new bare literals: `MergerRecycledWord` (`RECYCLED`),
    `MergerNotInFlightWord` (`NOT in flight`) and the two shared format/suffix
    consts are the single spelling of each fact, referenced by the test
    (`Rule 13`).

## Scope note (what this fix does NOT do)

**It closes the DISPLAY false positive, not the pid's reusability.** After this
change `--status` / the stop report no longer calls a stopped merger's last tick
"in flight", and the stamp makes the record attributable. But a record naming a
RECYCLED pid can still be *listed* as a live state file
(`MergerLiveStateFiles` keeps the bare-liveness semantics on purpose — the
flock-owned stop path's stale/foreign message and several existing tests are
written against it), so a future display that consumes that list and renders a
verdict WITHOUT going through `WriterLive` can reintroduce the misdirection. The
verdict/report faces added here are the ones the epic named; the list is the
residual, and it is named here rather than quietly absorbed.

## Not Done

- **Not covered: the flock-owned stop path naming the recycle.** When the
  recycled pid is NOT a lock holder, `gopi merger --stop` already refuses it as a
  "stale or foreign record and is NOT a target" — the same defect at a second
  face, but the dedicated wording (`RECYCLED`) is not added there. That path
  lives in `cmd/agent/cli_merger_stop.go` (`mergerPrintStaleRecord` via
  `MergerLiveStateFiles`) and is deliberately left alone: adding the attribution
  filter to `MergerLiveStateFiles` would change which records the stop path lists
  and messages, and the epic's acceptance is about the display that claimed work
  in flight.
- **Not covered: `MergerStateWriterLive` / `MergerFreshestLiveState`
  attribution.** They still answer "a live process is at this pid"; every
  display built on them goes through `WriterLive` before rendering a verdict, so
  the residual is a mislabelled registry, not a false verdict.
- **Not a kernel-verified recycle.** A test cannot make the kernel recycle a
  number on command; the fixture presents the two facts a real recycle presents
  (record claims a pass; the live process at that pid is not the writer). The
  lookup itself (`procname.StartTicks` vs a recorded tick count) is the same one
  the ledger identity tests exercise against real processes
  (`internal/testutil/procname_triple_redproof_test.go`).

## Verification

- Build: `go build ./...` → OK; `go vet ./internal/tools/ ./cmd/agent/` → OK.
- Repro/regression (both display faces, red before the fix, green after):
  `go test -count=1 -run 'TestMergerStatusRendersInFlightForARecycledPid|TestMergerStopReportRendersInFlightForARecycledPid' ./cmd/agent/`
  → `--- PASS: TestMergerStatusRendersInFlightForARecycledPid` +
  `--- PASS: TestMergerStopReportRendersInFlightForARecycledPid` (2.6s).
- `go test -count=1 ./internal/tools/` → `ok github.com/nick/gopi/internal/tools 316.601s`
  (whole package green).
- `go test -count=1 -run 'Merger' ./cmd/agent/` → one FAIL:
  `TestMergerStop_StateFileNamingAForeignPidStopsTheDaemon`. **Pre-existing and
  reproduced at the pristine baseline**: the same test fails identically on
  unmodified master `0dac2ae7` (the test's state-file naming vs the lock-holder
  assertion; it never asserts the flock message this change touches). It is
  named here, not absorbed.
- Status/stop contract tests: `TestStatusAloneStaysReadOnly` and
  `TestActingActionAloneStillActs` → PASS (3 subtests each, incl. the merger and
  the task-merger alias).
- `gofmt`: the three touched/added files are clean. Four files in the tree were
  already unformatted at the pristine baseline (`internal/tools/task_deliverable_worktree.go`,
  `cmd/agent/cli_tasks_advert_gate_test.go`, `cmd/agent/cli_worker_control.go`,
  `cmd/agent/daemon_scope_unit_test.go`, `cmd/agent/usage_test.go` — verified with
  `git show HEAD:<file> | gofmt -l /dev/stdin`); they are pre-existing and were
  left alone.
- Commit: `9a3b9f35 fix(merger): attribute a state record to the process that wrote it`
