<!-- doc-class: historical -->
# serve-lock holder handshake: a 5.00s budget against a 5.25–6.25s fixture

## What was broken

`TestAcquireServeLock_RefusesAfterUnlinkUnderneath` (and, through the shared
helper, `TestMergerWorkLock_MergeFacesRefuseWhileAHolderRuns`) spawns a REAL
child — this test binary re-exec'd as `TestServeLockHolderHelperProcess` — which
takes the flock and then writes a readiness marker. The parent polled for that
marker with a fixed window:

```go
serveLockHolderWaitTicks = 100
serveLockHolderWaitTick  = 50 * time.Millisecond   // 100 × 50ms = 5.00s
```

and then failed with `helper never signalled readiness at <path>`.

**Measured on this host (2026-10-12, load ~4–8/8 cores): the child needs
5.25–6.25s to reach readiness on an IDLE machine.** The window was not too
small for a loaded host — it sat *below its own fixture's floor*, so the test
raced its own startup and lost roughly one run in five:

```
--- PASS: TestAcquireServeLock_RefusesAfterUnlinkUnderneath (4.90s)
    cli_serve_test.go:105: helper never signalled readiness at …/holder.ready
--- FAIL: TestAcquireServeLock_RefusesAfterUnlinkUnderneath (5.03s)   ← 1 of 5
```

## Root cause (5-why)

1. **Why did the parent time out?** The readiness marker did not exist when the
   window closed.
2. **Why was it late?** The child was still in the test binary's own **suite
   startup**, not in the helper body. Measured: `agent.test -test.run='^$'`
   (no test runs at all) takes **5.02s** — `testutil.PinSuiteForTestMain` (the
   scratch root, the notify pin, the GOPI_HOME/ledger pins) plus
   `ArmAndSweepForSuite` and the close-out sweeps. The helper re-execs the same
   binary, so it pays that before `os.OpenFile`+`flock`.
3. **Why does an *idle* host pay 5+ seconds?** The suite prints its own answer in
   every run: the scratch root `/home/user/workspace/gopi-test-scratch` is
   ext4/block-backed — *"a fresh ledger built under it pays this device's journal
   commits"* — and `/dev/shm` (774 MiB, floor 1.0 GiB) has no room to offer a
   tmpfs. This suite is therefore slow *by configuration*, and any load
   multiplies it.
4. **Why was 5.00s chosen?** It is a round number typed into a constant whose own
   comment says *"a slow CI must not turn a working test red"* — the constant
   asserts a property the measurement contradicts.
5. **Why did the failure not say any of this?** Because the parent reported only
   its own budget. The child's exit status and stderr went to the suite's stream
   where nobody correlated them, so **a child that died taking the flock was
   indistinguishable from a child that was merely slow** — the fatal half, since
   a broken fixture would have been "fixed" by widening the window.

## The fix

`cmd/agent/cli_serve_test.go`:

- **`serveLockHolderWaitBudget(deadline, elapsed, measuredStartup)`** — the
  adaptive budget as a **pure function**: a floor (`serveLockHolderMinWait`,
  30s) raised to `serveLockHolderSampleMultiple ×` a startup sample **measured
  by an earlier handshake in the same test**, and always capped by what is left
  of the run's own `-test.timeout` minus `serveLockHolderDeadlineReserve`. The
  window now tracks the machine and the run's configuration instead of a typed
  constant, and it can never grow into the test's own deadline (this handshake
  fails naming the child; `-test.timeout` never panics over the whole binary).
  This deliberately does **not** free the budget from wall-clock: it frees it
  from a *fixed* wall-clock, which is the root cause. Waiting for a real child
  to start is not a sleep to be evented away (Rule 27: no load is generated
  anywhere in this change).
- **The child's death is an EVENT, not a timeout.** One `cmd.Wait()` goroutine
  per handshake delivers the exit status into a channel the poll loop selects
  on, so a child that dies is reported **immediately**, with **its own stderr**
  (captured, bounded, drained) and **its own exit status**.
- **The timeout verdict reports the child's state** ("still RUNNING" vs "EXITED
  as the budget ran out") and says which of the two it is, so "slow" and
  "crashed" can never again be the same string.
- The helper gained the regression body: `GOPI_TEST_SERVE_LOCK_FAIL_BEFORE_READY`
  makes it take the flock and then exit `66` with its own stderr line BEFORE
  signalling readiness.
- `merger_work_lock_e2e_test.go`'s `mwlHolder` now routes through the same
  handshake (it re-execs the same helper), so that fixture gets the same
  diagnosis and the same derived window; its own 20s constant is now unused.

### Two defects found in my own first cut (both fixed, both instructive)

1. **A recording sink must not return.** `*testing.T.Fatalf` ends the
   goroutine; my `recordedFailure` only set a field, so `waitReady` kept
   polling and the **timeout Fatalf overwrote the correct exit message 30s
   later** — the assertion read a slowness verdict for a child that had died in
   milliseconds. The exit arm now `return`s explicitly after `Fatalf`, with the
   reason written down.
2. **`holderStderr` needed a constructor.** A zero-valued struct has a nil
   `done` channel and `capture` closes it → `panic: close of nil channel`
   (Rule 15: uninitialised state must not be reachable). `newHolderStderr()` is
   now the one constructor.

## Not Done (honest)

- `-race` was not run on this branch (the suite is heavy; the change adds one
  goroutine per handshake, and `killAndReap` joins it with a bounded grace).
- The ext4 scratch root / `/dev/shm` floor remains the underlying reason this
  suite is slow here. This change makes the handshake stop caring; it does not
  make the suite fast. Pointing `GOPI_TEST_SCRATCH_HOME` at roomy tmpfs is an
  operator decision, not a code change.
- The budget floor (30s) is a constant, as Rule 1 requires. It is a FLOOR under
  a derived window, not the window itself — 30s is ~5× the measured 5.25–6.25s
  fixture cost, and the cap keeps it inside the deadline.

## Verification

- Build: `go build ./...` → OK; `go vet ./cmd/agent/` → clean.
- Tests: `agent.test -test.run 'TestAcquireServeLock|TestServeLockHolder|TestHolderStderr' -test.count=3 -test.v` → **PASS 3/3**
  (pre-fix, the same subject FAILed 1/5 on this host:
  `agent.test -test.run 'TestAcquireServeLock' -test.count=5`).
- Ran: the two new subjects and the merger family, verbatim invocations and
  raw output in the run's session directory (`~/.gopi/sessions/01a12e9c-…/`):
  `evidence-prefix-serve-lock-count5.txt` (pre-fix FAIL),
  `evidence-startup-times.txt` (the 5.02s `-test.run='^$'` measurement),
  `evidence-newtests.txt`, `evidence-redproof-mutation.txt` (red proof),
  `evidence-acceptance-serve-lock-count3.txt`, `evidence-merger-family.txt`,
  `probe_startup.py` (the 5.25–6.25s startup samples).
- Result: the measured startup is logged by the test itself
  (`holder reached readiness in 4.823s / 7.482s / 5.123s (handshake budget 30s)`)
  — note the 7.48s sample, which the old 5.00s window could not survive; and the
  red proof (death event suppressed) fails as designed:
  `a child that died at once must be reported at once, took 30.037s`.

## Assertions unchanged

`git diff master -- cmd/agent/cli_serve_test.go` adds and removes **no line**
carrying `UNLINKED copy`, `already running` or `OrphanLockHolder`; the only edit
inside those blocks is the fixture handle's rename (`holder.Process.Pid` →
`holder.pid`, the same pid). The three assertions stand verbatim.
