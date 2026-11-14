<!-- doc-class: historical -->
# The cmd/agent "load-only" reds were a leaked process-wide stop flag, not load

**Date**: 2026-10-17 · **Task**: `01a148bc-1b85-7b20-b24f-e3da69ee0440` ·
**Tree**: `task/01a148bc1b857b20b24fe3da69ee0440` (cut from `7ecfa12da`)

A full-package `cmd/agent` budget run reported eight red entries while the same
test names were green under a scoped `-run` on the same blobs. The reading on
offer was "package-load-only failure". It is wrong, and this document records
the measurement that refutes it, the mechanism, and the recipe for telling the
two shapes apart next time.

## 1. The names

Under `make test-budget PKGS='./cmd/agent/'`:

```
TestWorkerLoopLeavesNoWatcherPollBehind
TestRunWorkerClaimLocalOnlyWithNoPeers
TestWorkerTakeOverFollowsThePullSwitchOnly            (+ both "pull on" sub-tests)
TestWorkerTakeOverGatesRefuseWithoutDialling/task_sync_off_still_allows_the_transfer
TestWorkerTakeOverWalksToTheNextPeer
```

All five failure texts are the same family — *a value that should have been
produced was never produced*:

```
task_pull_switch_test.go:57:  workerTakeOverAttempt = "", want "t-taken-over" (task_pull=1 task_sync=0)
task_takeover_trigger_test.go:100: with task_pull on and task_sync off, the round took over "", want the peer's row
task_takeover_trigger_test.go:140: takeover returned "", want the id the second peer handed over
singlenode_serve_test.go:132: task-sync-off claim gate did not fire on empty topology
cli_worker_control_watcher_test.go:158: the running loop has no file-watcher poll on the stack (0, was 0 before)
```

## 2. The refutation: it is not load, it is SELECTION SIZE

The failure reproduces in **three-test** selections, on an idle host (load
0.8–2.7 on 8 cores), with no full-package run involved:

```bash
$ go test ./cmd/agent/ -run 'TestWorkerAckIsWrittenOnConsumeAndAnswersOnlyItsOwnRequest|TestWorkerTakeOverWalksToTheNextPeer|TestWorkerTakeOverFollowsThePullSwitchOnly'
--- FAIL: TestWorkerTakeOverFollowsThePullSwitchOnly
    --- FAIL: .../pull_on,_row_exchange_off   workerTakeOverAttempt = "", want "t-taken-over"
    --- FAIL: .../pull_on,_row_exchange_on    workerTakeOverAttempt = "", want "t-taken-over"
--- FAIL: TestWorkerTakeOverWalksToTheNextPeer
    takeover returned "", want the id the second peer handed over
```

Bisected to ONE polluter, one line of selection:

```bash
$ go test ./cmd/agent/ -run '^TestWorkerTakeOverWalksToTheNextPeer$'                       # ok 3.9s
$ go test ./cmd/agent/ -run '^(TestWorkerAckIsWritten…|TestWorkerTakeOverWalksToTheNextPeer)$'  # FAIL
$ go test ./cmd/agent/ -run '^(TestWorkerLoopLeavesNoWatcherPollBehind|TestRunWorkerClaimLocalOnlyWithNoPeers)$'  # ok 11.6s
$ go test ./cmd/agent/ -run '^(TestWorkerAckIsWritten…|TestWorkerLoopLeavesNoWatcherPollBehind|TestRunWorkerClaimLocalOnlyWithNoPeers)$'  # FAIL both
```

So the variable is **which other tests the selection includes**, never the
machine's load. A full-package run includes everything, which is why it was
100% red; a three-name `-run` includes the polluter only sometimes, which is why
the isolation re-run "looked flaky".

## 3. The mechanism

`workerStopState` is a deliberate **module-level** record
(`cli_tasks_worker_stop.go:131`: "the one flag every exit path reads"). Every
in-process worker test drives it, so every such test must hand it back.

`TestWorkerAckIsWrittenOnConsumeAndAnswersOnlyItsOwnRequest`
(`cli_worker_ack_test.go`) called `resetWorkerStopState()` on ENTRY and then ran
the PRODUCTION consume path:

```go
ctx.consumeDrainRequests()   // a real durable request file → workerRequestStop(reason, false)
```

and never restored. From that instant `workerStopRequested()` is `true` for the
rest of the test binary, and every later in-process round reads a **pending
graceful stop**:

| surface | stop-check | observed symptom |
|---|---|---|
| `runTaskWorker(true, …)` (`--once`) | `cli_tasks_worker.go:970/1002` | returns before the claim round → no `[claim-lock]` line, no watcher installed |
| `workerTakeOverAttempt(false)` | `cli_tasks_worker.go:1234` | returns `""` before it dials → `took over ""` |

That is exactly the five red names, with no reference to load.

The same entry-reset-without-restore shape was found in **eleven** other tests
across six files (`worker_drain_unit_test.go` ×6, `worker_termination_provenance_test.go` ×2,
`worker_stop_forward_regression_test.go` ×3, `worker_control_nudge_loop_test.go`,
`worker_foreground_file_stop_test.go`, `worker_idle_drained_stop_test.go`,
`worker_file_watcher_lifecycle_test.go`). Only one of them has to run before the
victims; in a full-package run, one always does.

## 4. The fix

1. **Every** test that engages the record hands it back:
   `t.Cleanup(resetWorkerStopState)` next to its `resetWorkerStopState()`.
   (Reset-on-entry is still required — a restoring test that never resets still
   inherits a sibling's leftovers.)
2. A **census guard**, `TestWorkerStopStateIsHandedBackByEveryTest`
   (`worker_stop_state_leak_gate_test.go`): parses every `*_test.go` in
   `cmd/agent`, and for every top-level `Test*` that calls one of
   `resetWorkerStopState | workerRequestStop | workerRequestStopFile |
   workerMarkDrainRequested` requires a `t.Cleanup(resetWorkerStopState)` or
   `defer resetWorkerStopState()`. It names the offending file+test, so the next
   leak is diagnosed in milliseconds instead of in a 600s run.
   Two RED proofs ship with it: a fixture reproducing the original shape must be
   named, and a `defer` restore must be accepted (a census that reds a correct
   test is not a guard).

Why a source census instead of a runtime assertion: the property is "this test
restores after ITSELF", and only the source carries the attribution. A
`TestMain` check can only report that the record is dirty at close-out (the
victim, not the culprit), and `-shuffle` searches report whichever pair happens
to collide.

## 5. Recipe: is a package-only red load, or selection?

Do these in order; each is cheap and each can end the investigation:

1. **Read the FAIL duration.** `0.00s`/`0.03s` rules out load entirely — there
   was no time for load to matter. (`--once` victims here failed in 0.03–2.0s.)
2. **Re-run the reds as a THREE-name `-run` selection.** Still red on an idle
   host ⇒ not load. (If you only re-run them ALONE you learn nothing: a lone
   victim is always green.)
3. **Bisect the selection, not the machine.** Add one helper/sibling test at a
   time until the red appears; the test that makes it appear is the polluter.
   This run took two rounds and ~40s.
4. **Grep the polluter's body for writes to package-level state** that the
   victim reads (`workerStopState`, `workerTakeOverCall`, `workerWriteback`,
   `workerCtx`, `scratchGCLastTick`, …). The victim's failure text then tells
   you WHICH check returned early — here `workerStopRequested()`.
5. Only if the red survives steps 1–4 should load be hypothesised — and then it
   is proven with the control-tree differential (memory #3062), not asserted.

Note the general shape: **package-level mutable state + entry-only reset** turns
"which tests ran before me" into a failure that looks exactly like flakiness or
load. The census guard is the cheap way to keep that class out, and it costs one
`go/parser` pass.
