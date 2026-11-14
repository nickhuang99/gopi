<!-- doc-class: historical -->
# Merger "losing track" — the two defects behind a wedged merge daemon (2026-09-16)

Operator report: `gopi merger --status` showed the child state as **unknown** for
a branch that `git log` proved was merged hours earlier, and the merger stopped
advancing to the next branch. The suspicion ("it is stuck somewhere") was
correct, and there were **two independent defects stacked**: one wedged the
process, the other made the status surface blind to it.

## Defect 1 — a one-value channel with two readers (the wedge)

`cmd/agent/merger_verify_child.go` (verify path) and `cmd/agent/merge_resolve_child.go`
(resolve path) both waited on a spawned child like this:

```go
done := make(chan error, 1)                 // ONE value will ever be sent
go func() { done <- child.Wait() }()        // …by ONE Wait()
select {
case werr := <-done:                        // healthy path
case <-time.After(cap):
    gone := make(chan struct{})
    go func() { <-done; close(gone) }()     // READER #1 (parked immediately)
    outcome := stopChildGracefully(..., waitChildExitGone(gone))
    <-done                                  // READER #2 — blocks FOR EVER if #1 won
}
```

The cap branch needs **two** independent waiters on the same exit: the grace
probe handed to `stopChildGracefully`, and the caller's own read after the stop.
The probe is already parked on the channel, so it takes the single value first
and the caller's `<-done` can never return. The channel is never closed, so
nothing rescues it: the merger froze **mid-pass with `serve.lock` held**, its
state file stopped advancing, and every later pass was blocked behind it.

Not a timing nicety: `waitChildExitGone` is bounded (5s grace), so
`stopChildGracefully` always returns — the unbounded wait was the caller's read.

Measured on this machine twice in one day (06:03 and 14:31). The 14:31 instance:
pid 3473055, `wchan=futex_wait_queue_me`, no child processes left, 6 open fds
(the lock only), state file frozen at 14:31 while the log's last line was the
signal-provenance intent line for a child it had already SIGTERMed — i.e. it was
parked on the line *immediately after* the graceful stop.

### Fix

`childExit` (in `cmd/agent/child_stop.go`) is an exit signal that **cannot be
read wrongly**: `sync.Once` + a channel that is **closed** when the child is
reaped, with the wait status published before the close. A close wakes *every*
waiter, repeatedly, so the grace probe and the caller can both wait and neither
can steal the other's signal. The two call sites now share one bounded wait —
`awaitChildExit(exit, proc, kind, reason, cap)` — so they cannot drift into two
shapes of the same wait again.

Invariants kept (the briefing's boundaries): the default cap is untouched, the
verdict policy is untouched, and the graceful ladder (intent → SIGTERM group →
bounded grace → SIGKILL) is untouched.

### Red proofs (shown, not asserted)

1. **Pre-fix shape blocks** — the old code path reproduced verbatim in a
   throwaway test: reader #1 parks, reader #2 never gets the single value.
   `--- FAIL: TestRedProofOldShapeBlocks … PRE-FIX SHAPE BLOCKED: the second
   reader never got the single channel value — the merger wedge, reproduced`
   (7.07s). The fix is scheduling-independent, and the permanent guard
   `TestAwaitChildExitReturnsAfterTheCapAndStopsTheChild` asserts the timeout
   branch *returns*, reports the stop, and leaves no live child behind.
2. **Shape gate** — `TestNoDoneChannelDoubleReaderShape` scans every production
   `.go` file of `cmd/agent` and fails on the forwarding-reader literal, so the
   shape cannot be retyped (Rule 13), with a read-floor so a broken directory
   read cannot pass vacuously.

## Defect 2 — a durable state file that the next binary cannot read

The merger's state file outlives the process that wrote it, and Go's
`encoding/json` fails the **whole document** when one field's shape mismatches.
`children` changed from `[]string` (rendered labels) + a parallel `child_pids`
to `[]MergerChild` (kind/label/pid/group/started) in `9032e777`. A daemon
started before that commit kept writing the old shape, so the *only* record of
what was in flight became unreadable and `--status` reported
`in flight: unknown — unparsable merger state file (pid 3473055)`.

The irony is exact: the frozen file **named everything** —
`branch fix/merge-rebuild-loop`, `stage verify`,
`verify-child 01a0a7ad-… (pid 39029)` — while the reader refused to look.

### Fix

`internal/tools/merger_interrupt_legacy.go`: a compatibility decoder
(`MergerInterrupt.UnmarshalJSON`) that decodes the **current** shape first
(never shadowing the ordinary path) and, only on failure, maps the legacy
`children`/`child_pids` pair into `[]MergerChild`. The legacy label's own text
is parsed for kind/work/pid (`"<kind> <work> (pid N)"`, the format the older
writer emitted); a label that does not match that format keeps its text and
reports pid 0 — "pid unknown" is a legal answer, an invented pid is not. A
document matching **neither** shape is still reported unparsable (Rule 15:
repairing readability must not make the reader credulous).

### Red proof

Decoder removed, tests kept: `the frozen legacy state file must decode: json:
cannot unmarshal string into Go struct field MergerInterrupt.interrupt.children
of type tools.MergerChild`, and `a readable legacy record must not be excused
with a note, got "unparsable merger state file (pid 656702): …"` — the operator's
exact line. Restored → green.

### Live end-to-end proof (real data, real binaries)

The wedged daemon's actual state file (`merger-state-3473055`) copied into a
sandbox profile, under a **live** pid so the reader counts it:

```
# UNFIXED (master binary):
in flight:   unknown — unparsable merger state file (pid 638001): {"pid":3473055,…}

# FIXED (this branch):
in flight:   pass started 2026-09-16T14:24:12+08:00 (elapsed 2h5m30s), repo
             /home/user/work/GoPi, branch fix/merge-rebuild-loop, stage verify
  children:  none running — no work is in flight; the record still names 1 ended child process(es)
  ended:     1 named in the last record but NO LONGER RUNNING (their processes are gone)
    - verify-child 01a0a7ad-2037-7d96-827b-7a703b319cad (pid 39029, process GONE)
```

The operator's "unknown" is now the actual answer: which branch, which stage,
which child, and that the child is gone.

## Recovery for an already-wedged merger (there is no automatic escape)

A wedged merger cannot be stopped gracefully, and this is not a bug to fix — it
is a property of the wedge, so an operator needs the procedure:

1. **Diagnose.** `gopi merger --status` names the holder pid and the frozen
   pass. Then confirm the wedge (all three, not one):
   - `cat /proc/<pid>/wchan` → `futex_wait_queue_me` (blocked on a futex, not IO);
   - `cat /proc/<pid>/task/*/children` → empty (it is waiting on a child that
     no longer exists);
   - the state file's mtime is far older than the pass it describes, while the
     pass is recorded as in flight.
2. **Escape with SIGKILL.** `kill -KILL <pid>`. SIGTERM is *not* an escape on
   this process: the merger's handler only sets a flag that is read at a pass
   boundary, and a wedged process never reaches one. Do not expect
   `gopi merger --stop` to help either — it is a bounded *wait* (reports "still
   running" when the bound expires), which is the correct design for a healthy
   daemon and useless for a wedged one.
3. **Do not delete `serve.lock`.** The flock is released by the kernel when the
   holder dies, so nothing needs cleaning. Unlinking the lock *while* a holder
   lives is the 2026-09-12 orphan-lock incident (two daemons sweeping the same
   branches). Leave the file; it is a name, the flock is the guard.
4. **Restart and verify.** Start the daemon and confirm the next pass appears in
   the status output with a fresh `pass started`, and that the pending queue
   drains.

After the fix has been built and the daemon restarted from the new binary, step
2 should no longer be needed: the cap branch always returns.

## Not done (honest)

- **The wedge cannot be reproduced live on the real daemon** without deliberately
  wedging it (which would require reverting the fix in the running binary). The
  reproduction is the schedule-independent unit-level red proof above (the old
  shape driven verbatim and observed to block).
- **"Is the pass progressing?" is still not shown.** The status surface renders
  the pass start and the children, but not the state file's last-advance age, so
  an operator still has to compare the mtime by hand (step 1 above). Dispatched
  as a follow-up task rather than half-implemented here: a threshold-based
  "stalled" verdict would false-alarm on a legitimately long verify child (the
  cap is 45m and the state file does not advance while the child runs), so it
  needs a design that reports the age without inventing a verdict.
- The resolve path's identical pattern was fixed in the same commit (the
  briefing told me to say so): `merge_resolve_child.go` carried the same
  double-reader shape against the same single-value channel.
