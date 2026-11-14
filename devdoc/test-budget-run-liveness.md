<!-- doc-class: historical -->
# The test-budget entry point must show it is ALIVE while it works

**Measured 2026-10-14, task `01a1387526ff7699820066d5b2cb5186` (peerbox).**

## The symptom

`make test-budget PKGS='./internal/tools ./cmd/agent'` — the branch-scoped suite
entry point GOPI.md prescribes — produced **no observable output**:

- a foreground `bash` call was killed by its own 2-minute no-output net;
- a background job left `stdout`/`stderr` at **0 bytes** for 7+ minutes.

The run was healthy the whole time. Its per-run stream
`…/queue/test-budget/run-<epoch>-<pid>.json` was written from the first byte.
So the defect was never "the run is stuck" — it was **"the run's observable
surface is silent"**, which is indistinguishable from a hang to every caller and
to every supervisory net.

## The two causes, and why fixing one is not enough

**1. The header waited on the run's first slow step.** `=== gopi test budget
run ===` was printed at script line ~1171, but the tested-binary build
(`CGO_ENABLED=0 go build -o … ./cmd/agent/`, tens of seconds on a cold cache) ran
at ~484. The run's first words therefore arrived after a Go build.

**2. `go test` had no liveness channel.** Line 1088:

```sh
go test -count=1 -timeout "$test_budget" -json "${args[@]}" >"$raw_json" 2>&1
```

…and the human summary is rendered from `$raw_json` only **after** that returns.
For the whole suite, nothing wrote to the caller's stdout. This is the load-
bearing one: fixing (1) alone buys a single early line and then the same silence.

## The fix

- `report_run_header()` prints the identity block **before**
  `build_tested_binary()`, so the header precedes the first slow step. Both are
  now functions; the build's behaviour is unchanged (same `mkdir -p`, same
  `-ldflags -X …Revision` stamp, same `GOPI_TEST_BIN` export, same three
  `NOTE:` lines on failure).
- `heartbeat_while_running PID` prints, while `go test` runs:

  ```
  heartbeat t+16s: go test still running (2516 bytes, last test started: TestBaselineWorktreesAreAssertedClean)
  ```

- `stream_progress_of PATH` reads a **4 KiB tail window** (a named constant) and
  reports the stream's byte count plus the most recent `Action=="run"` test name.
  Bounded on purpose: the stream reaches hundreds of megabytes and the observer
  must add no meaningful device work to the run it observes.

## The two properties the fix was FORBIDDEN to trade away

1. **The stream stays a pure `go test -json` stream.** The redirect is untouched
   — the heartbeat writes to the *script's* stdout, never into `$raw_json`. It is
   deliberately a sibling of the redirect, **not a `tee` into it**: every other
   reader (`summarise_json`, the scope check, the failure report,
   `--scope-check`) parses the file unchanged.
2. **The verdict stays `go test`'s own.** The child is backgrounded only so the
   parent has a loop to run in; `wait "$go_test_pid" || status=$?` collects the
   child's status, so the exit-code contract (0/1/2/3/4/5/6) is unchanged.

## The observer must not lengthen what it observes

The heartbeat sleeps in **1 s slices** and re-checks `kill -0` in each one, so
the loop notices the suite's exit within a slice instead of sleeping out a whole
10 s interval of dead time after `go test` returned. The loop is bounded twice —
by liveness and by `heartbeat_max_rounds` (Rule 27: a bound, not a timeout).

## Two measured traps, recorded so they are not re-derived

- **The progress reader matched nothing at first.** It grepped for the adjacent
  pair `"Action":"run"` … `"Test"`, but `go test -json` puts `Package` BETWEEN
  them: `{"Action":"run","Package":"…","Test":"…"}`. The symptom was every
  heartbeat printing `last test started: <none yet>` while the byte count climbed.
  Fix: select the event line on `"Action":"run"`, then extract `"Test"` in a
  second stage.
- **A gate keyed on the bare word `heartbeat` is satisfied by the header.** The
  header itself names the heartbeat's cadence, so a substring test on
  `heartbeat` passes on a run that never emits one — measured: the first version
  of the gate stayed GREEN against a mutant with the heartbeat loop deleted. The
  gate keys on the heartbeat's own sentence (`go test still running`) instead.

## The check

`internal/testutil/testbudget_observable_gate_test.go`:

- `TestBudgetRunIsObservableWhileItWorks` runs the **real** entry point against a
  stub `go` that emits structured events and then keeps working, and reads its
  stdout **through a pipe while the child is alive**. The stub touches a marker
  as its last act, so "the heartbeat arrived before the suite finished" is
  measured, not inferred. The same arm asserts the stream carries the stub's
  events unchanged and no heartbeat text, and that the exit status is unchanged.
- `TestBudgetObservablePredicateIsFalsifiable` drives the gate's decision
  function with the pre-fix shapes (output only at the end; header and no
  heartbeat; nothing at all) and requires each to be reported.

```sh
go test ./internal/testutil/ -run 'TestBudgetRunIsObservable|TestBudgetObservable'
```
