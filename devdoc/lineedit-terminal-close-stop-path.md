<!-- doc-class: normative -->
# Terminal.Close Is a Stop Path — ending a parked read and joining the SIGWINCH watcher

Task: `01a0faf5-5530-7798-bd98-244a24c4c20d` (branch `task/01a0faf555307798bd98244a24c4c20d`).

## The symptom

`cmd/agent`'s chat-inject pty case **PASSED while the package FAILED**:

```
$ CGO_ENABLED=0 go test ./cmd/agent/ -run TestChatInject_EscapeByNameReachesTheTurnSeam -count=1
PASS
suite goroutine sweep: parked repository goroutines: 2
suite goroutine sweep: goroutine 37 [runnable]:
  github.com/the operator/gopi/internal/lineedit.(*Terminal).startWinch.func1()
        /…/internal/lineedit/raw.go:362
  created by github.com/the operator/gopi/internal/lineedit.(*Terminal).startWinch (raw.go:360)
suite goroutine sweep: goroutine 38 [runnable]:
  github.com/the operator/gopi/internal/lineedit.(*Terminal).ReadEvent(...)
        /…/internal/lineedit/raw.go:479
  github.com/the operator/gopi/internal/lineedit.(*Injector).ReadEvent(...)  /…/internal/lineedit/inject.go:656
  github.com/the operator/gopi/internal/lineedit.(*Editor).Run(…)             /…/internal/lineedit/editor.go:1361
  github.com/the operator/gopi/cmd/agent.newInjectPtyGrid.func1()
        /…/cmd/agent/chat_inject_pty_test.go:94
FAIL	github.com/the operator/gopi/cmd/agent	6.083s
```

It was **intermittent**: 6 consecutive `-count=5` batches each carried one red bit.

## Root cause (measured, not inferred)

Instrumenting `ReadEvent`'s poll loop and the fixture teardown produced the
mechanism in one run (`poLLDBG`/`GRIDDBG`, evidence under the session dir):

```
POLLDBG n=1 in=0 winch=32 wakeIdx=2 fds=[{7 1 0} {8 1 32} {10 1 32}] wake=true closed=true
GRIDDBG enter → closed-editor → timeout (3s) → done
GRIDDBG loopexit err=<nil> eof=true          ← only after pair.Master.Close()
```

Two independent defects, both in `internal/lineedit`:

1. **Closing a descriptor is not a stop path.** `Terminal.Close()` closed the
   winch pipe and the wake pipe underneath a **parked** `poll(2)`. poll returns
   `POLLNVAL` (=32, visible above as `winch=32`) — which is **not**
   `POLLIN`/`POLLHUP`, and the loop inspected only those two — so it re-polled
   the dead descriptors **for ever**. The read loop left `Run` only when the
   pty **master** was closed, i.e. on the scheduling luck of the teardown that
   followed.
2. **The watcher was asked to stop, never joined.** `stopWinchWatch()` did
   `signal.Stop` + `close(stopSig)` and returned; the watcher goroutine was left
   runnable, which is exactly what a close-out sweep samples.

Both together are why the leak was intermittent: whichever of the two the sweep
caught, and whether the master's close landed first, decided the count.

## The fix

`internal/lineedit/raw.go` (production — the leak was production code, not the
fixture):

- **`Terminal.stopped`** (`atomic.Bool`). `Close()` raises it **before** anything
  is torn down, and **wakes the parked read through the seam it already polls**
  (the wake pipe, or the resize pipe when there is none). A stop stated before
  the teardown is observed at once instead of at the next keystroke.
- **`ReadEvent` observes it three times**: at entry (a read that *starts* after
  `Close`), after every poll (a read that is *already* parked), and via
  **`closedPollFd`** — `POLLNVAL` on any polled descriptor is treated as "the
  terminal is gone", so a reader that somehow misses the flag still ends.
- **`stopWinchWatch` JOINS the watcher** (`winchDone`), so `Close()` returns only
  once the stop it asked for has really happened.
- **`Terminal.Stopped()`** exposes the stop so a caller can ASK rather than assume.
- The stop is reported as the **`EOF`** event (what a pty hang-up already
  produces), never as a read error — a stop is not a broken descriptor.

`cmd/agent/chat_inject_pty_test.go` (fixture): the teardown now closes the
terminal **before** joining the read loop (the terminal *is* what stops it; the
old order made the wait expire on its full bound on every run, and the exit it
observed was the descriptor's), and a loop that fails to return is **reported**
(`t.Errorf`) instead of being waited out silently (Rule 15).

### Attribution, measured

| tree | `parked repository goroutines` | test result |
|---|---|---|
| pre-fix (master) | **2** (intermittent) | PASS, package FAIL |
| production fix only, old fixture | **0** | PASS (but each case burns the 3s bound) |
| fixture fix only, pre-fix production | 0 | **FAIL** — "the editor's read loop did not return within 3s" |
| both (final) | **0** | PASS in 0.28s (was 3.28s) |

So the **production stop path is what removes the parked goroutines**; the
fixture change converts the old silent wait into a loud failure.

## Regression test (Rule 10.5)

`internal/lineedit/terminal_close_test.go`, three cases, each naming one half:

- `TestTerminalCloseEndsAParkedRead` — a reader **looping** on `ReadEvent` (the
  editor's shape) reaches the terminal-is-gone result, not merely "some event".
- `TestTerminalCloseJoinsTheWinchWatcher` — the watcher's exit signal is closed
  when `Close()` returns.
- `TestTerminalCloseEndsAReadThatStartsAfterwards` — the entry arm.

**Red-proved**: with the stop path disabled (`GOPI_LINEEDIT_REVERT=1`, a
throwaway switch used only for the proof and removed from the tree before the
commit), cases 1 and 3 fail by name; case 2's guard (`winchDone != nil` plus the
join) is a source-shape invariant and is *not* independently revertible — said
here rather than implied.

## Completed

- Production stop path in `internal/lineedit/raw.go` (`stopped`, `closeEvent`,
  `closedPollFd`, the join in `stopWinchWatch`, `Terminal.Stopped`).
- Fixture teardown order fixed and made loud on failure
  (`cmd/agent/chat_inject_pty_test.go`).
- Regression test `internal/lineedit/terminal_close_test.go` (3 cases, red-proved).
- The sweep guard is **untouched**: `cmd/agent`'s close-out sweep still counts
  every repository goroutine and still fails the package.

## Not Done

- **`TestTerminalCloseJoinsTheWinchWatcher` has no independent red-proof** (see
  above). It is a guarded invariant, not a demonstrated regression.
- Other `cmd/agent` pty fixtures (`chat_surface_key_e2e_test.go`,
  `chat_surface_bound_e2e_test.go`, `chat_turn_dump_*`, …) have the same
  join-then-close teardown ORDER this task fixed in the inject grid. They are
  green today — the production fix ends their read loops too — but they still pay
  a bounded wait they no longer need. **Not fixed here** (out of the epic's
  scope, and no failure observed): a follow-up task should apply the same order,
  or the fixture helper should own it.
- The `POLLNVAL` arm is not exercised by a test of its own; it is covered
  indirectly (it is the arm the pre-fix spin lacked).

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK
- Tests: `CGO_ENABLED=0 go test ./internal/lineedit/ -run TestTerminalClose -count=1`
  → 3/3 PASS; `-race` (`CGO_ENABLED=1 go test -race ./internal/lineedit/ -run TestTerminalClose -count=3`)
  → ok, 0 `WARNING: DATA RACE`; budget run over the touched packages
  (`make test-budget PKGS='./internal/lineedit ./cmd/agent'`) recorded in
  `budget-final.log` under this run's session dir.
- Ran: `CGO_ENABLED=0 go test ./cmd/agent/ -run TestChatInject -count=1 -v`
- Result:

  ```
  --- PASS: TestChatInject_ProductionConstructorExposesTheSeam (0.34s)
  --- PASS: TestChatInject_KeysReachTheEditorOnARealPty (0.49s)
  --- PASS: TestChatInject_EscapeByNameReachesTheTurnSeam (0.28s)
  suite goroutine sweep: parked repository goroutines: 0
  ok  	github.com/the operator/gopi/cmd/agent	7.353s
  ```

  and `-run 'TestChatInject|TestPrefetchGoroutineSettleGap' -count=5` → `ok`,
  `parked repository goroutines: 0` (11/11 cases PASS, no red bits).
- Ran: the same source tree under `gopi chat` for real (Rule 31) —
  `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` then
  `CGO_ENABLED=0 go test ./testsuite/ -run 'TestChatPTY_EditorRendersAndSubmits|TestChatPTY_ResizeDoesNotFreezeTheEditor|TestChatPTY_FailedTurnKeepsThePrompt' -count=1`
- Result: 3/3 PASS (11.5s / 13.1s / 16.2s) — real pty, real binary, real `/quit`.
- Budget run: `make test-budget PKGS='./internal/lineedit ./cmd/agent'` →
  **exit 1, and NOT the budget** (`the budget did NOT trip`; elapsed 1008s of
  25m/package). `./internal/lineedit` alone: `ok 64.843s`. The `cmd/agent`
  failures were diagnosed from the run's own messages and are **environmental**,
  not this change:
  - `is OLDER than …/internal/lineedit/raw.go — this test drives the BUILT binary`
    (this run had built `bin/gopi` before the last source edit; the binary gate
    said so in its own words) — the pty/binary-driven e2e cases;
  - `TestChatSurfaceBound_TerminatesAndIsRenewedOnlyByTheOperator` — the run's own
    line: `41 model round(s) served so far`, i.e. the mock's round budget was
    spent, under `load_start=18.92` on 4 cores (a model-latency-sensitive case);
  - the rest are the same binary-driving family (`TestRawPromptRunReleasesItsWorktree`,
    `TestWorker*`, `TestTaskRun*`).
  **Re-measured after rebuilding the binary**: the failed set re-run green
  (`TestPrefetchGoroutineSettleGapDetectsALiveLeak`, `TestRawPromptRunReleasesItsWorktree`,
  `TestChatSurfaceBound_TerminatesAndIsRenewedOnlyByTheOperator` → 3/3 PASS,
  `failures-recheck.log`).
- Commit: `e6e1791d`, report commit `b1320ffa`

## Evidence (self-filed)

- Pre-fix sweep line, observed verbatim: `suite goroutine sweep: parked repository goroutines: 2` with `startWinch.func1 (raw.go:362)` and `Terminal.ReadEvent (raw.go:479)`.
- The spin, observed with a temporary poll trace (`pollDbg`): `POLLDBG n=1 in=0 winch=32 wakeIdx=2 fds=[{7 1 0} {8 1 32} {10 1 32}] wake=true closed=true` — POLLNVAL on the descriptors `Close` had just closed, round after round, while `GRIDDBG loopexit` printed only after `pair.Master.Close()`.
- Sweep red-proof, observed: a temporary test leaking one goroutine gave `suite goroutine sweep: parked repository goroutines: 1` naming `zz_sweep_redproof_temp_test.go:8` and `FAIL github.com/the operator/gopi/cmd/agent` (file deleted afterwards).
- `gofmt -l` on the three touched files → clean (`internal/lineedit/paste_envelope_test.go` is unformatted on master, untouched by this task).
