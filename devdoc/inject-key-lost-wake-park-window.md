<!-- doc-class: historical -->
# The injected key's lost wake edge — an EDGE/LEVEL asymmetry in the read loop's park window

**Task**: `01a0f876-d6a4-7af2-b572-751cf728e1f6`
**Subject**: `cmd/agent`'s `TestChatInject_EscapeByNameReachesTheTurnSeam`
(`chat_inject_pty_test.go:288`) failing at `:338` with *"the injected key NAME
\"escape\" never reached the turn seam within 3s — the tool's own spelling is
broken while the raw byte works"*.

## The verdict, in one line

**Arm (a)**: the wake was an **EDGE** on a one-byte pipe that the terminal
**drains**, and the injected-key path had **no LEVEL** to re-derive at the park
point — so an edge that landed in the window was spent on a loop that then
committed itself to `poll(-1)` with the key still queued. The completion/publish
path had survived the same window for years precisely because it *is* a level.

## The measurement that decided it

The red is real and it is a race (**1 FAIL in 25**, `-count=25`, ambient load
`7.08 9.56 9.08` on 4 cores, **no synthetic load — Rule 27**): the red takes
**6.36s** while every green takes 3.27–3.29s. That duration alone says the case
sat on its own 3s bound and was then finished *from outside* — i.e. the key was
never delivered, and something else ended the wait.

The mechanism came from the read loop's own trace on a pty (`TraceAdd` at the
wait decision, both event returns, `Injector.ReadEvent`, and around `unix.Poll`),
first red in a `-count=100` run. Verbatim, `kind=3` = `EventWake`:

```
   0.000 === escape injection begins ===
   0.000 inj: wake("escape") pending=1
   0.000 term: poll ret n=1 err=<nil> wake=0x1 in=0x0
   0.000 term: poll ret n=1 err=<nil> wake=0x1 in=0x0
   0.000 loop got kind=3 bytes=0 err=<nil>
   0.000 loop wait=-1 bareEsc=false compl=false src=*lineedit.Injector
   0.000 inj: ReadEvent(-1) -> inner
   0.000 term: poll timeout=-1 wakeFd=true
   0.000 loop got kind=3 bytes=0 err=<nil>
   0.000 loop wait=-1 bareEsc=false compl=false src=*lineedit.Injector
   0.000 inj: ReadEvent(-1) -> inner
   0.000 term: poll timeout=-1 wakeFd=true
   0.344 inj: awaitDrain("escape") read=false pending=1
```

Step by step: (1) `Injector.wake` writes **one byte** into the terminal's wake
pipe; (2) the parked `poll` returns it and `Terminal.ReadEvent` **drains** the
pipe before reporting `EventWake`; (3) the loop's `EventWake` arm finds nothing
new to draw (all four draw flags false — the draft was already painted) and
continues, so **the edge is now spent**; (4) the next iteration decides
`wait=-1` — unbounded — because the injected queue is not part of that decision
(`bareEsc` is false: the ESC has not been *read*, it is still queued); (5)
`awaitDrain` gives up with `read=false pending=1`, and the key is still queued
1.6s later when the 3s bound expires.

## Why the sibling arm (b) is refuted

(b) would mean the key *arrives late*. The trace says it arrives **the instant
anything asks** — the injector holds it paced for 240ms
(`PasteEnterSuppressWindow × injectKeyFactor`), orders of magnitude inside any
bound a test would carry. It never arrives because after the spent edge
**nothing asks again and nothing can**: the wait it sits behind is `poll(-1)`,
which by construction has no terminator. A larger `injectEscapeClaimWait` would
only postpone the failure; it cannot end a wait that no signal can end. The
host's load is the **trigger** (a wake must land in the window), never the cause
— the window exists on any host, and the guard below reaches it deterministically
with no load at all.

## The fix

`internal/lineedit/editor.go`, at the park point, beside the existing
`runParkGateHook()` — 18 lines, no new mechanism:

```go
if retryMS, bounded := runInjectParkHook(); !bounded && timeout == pollTimeoutBlock && e.inputPending() {
    // A read unit is queued while the loop was about to block for ever:
    // bound this wait so the read collects the key instead of waiting
    // for a notification that has already been spent.
    timeout = injectParkRetryMS
} else if bounded && timeout == pollTimeoutBlock {
    timeout = retryMS
}
```

`e.inputPending()` is the pre-existing seam that already answers "is input
waiting" (`inputBacklog`); the injector forwards it (`Injector.InputPending`) and
answers **true for a queued-but-still-paced key**, which is correct: the read the
loop is about to make carries the pacing gap itself (`Injector.parkUntilDue`), so
bounding the wait is all the injector needs in order to deliver. Production never
installs the hook, so the default waits — including the unbounded block — are
untouched.

## The guard (`internal/lineedit/injectpark_test.go`)

The window is microseconds wide, so the guard **constructs** the interleaving
instead of sampling it — the same discipline `parkgate.go`'s publish-path hook was
built with:

- **`parkWindowSource`**: answers `EventNone` to a bounded wait and **parks** on
  an unbounded one (the shape `poll(-1)` has: a signal is the only terminator);
  its wake seam **swallows the first `Notify`** — a drained one-byte pipe, which
  is literally what `Terminal.ReadEvent` does to the terminal's.
- **The park hook** (`setInjectParkHook`) lands the injection *in* the window:
  after the wait has been decided, before the source is asked.

```
Ran:    go test ./internal/lineedit/ -run TestInjector_QueuedKeyIsReadWhenItsWakeIsLostInTheParkWindow -count=5 -v
Result: 5/5 PASS (2.34–2.35s)                             → $S/guard3-green.log

Ran:    (only the 18-line level removed from editor.go)
        go test ./internal/lineedit/ -run TestInjector_QueuedKeyIsReadWhenItsWakeIsLostInTheParkWindow -count=2 -v
Result: 2/2 FAIL (4.00s)                                  → $S/guard3-red.log
        injectpark_test.go:173: the injected key NAME "escape" never reached the
        turn seam within 2s — its wake was lost in the read loop's park window
        and no LEVEL brought the loop back for it
```

## The wrong guard, and how it was caught (worth reusing)

The **first** version of this guard used a source with **no wake seam at all**
(`SourceNotifier` → nil, so `Injector.wake()` is the documented no-op). It was
measured **PASSING with the fix reverted** (2/2 — `$S/guard2-red-local.log`). The
instrumented trace showed why: the loop's **opening** bounded wait
(`escWaitMS = 30`, flushed by `FlushEscape`) was enough to read the key, so the
case never reached the wait under test — it asserted a mechanism it had not
reached. That is the same defect class this task is about (a green that measures
nothing), so it is recorded here rather than deleted.

Two lessons, both measured on this branch:

1. **A guard must fail without the fix — verify it, do not assume it.** The
   red→green transition is the only evidence the guard bites.
2. **A teardown must not release the thing under test.** That first source's
   teardown closed a channel *before* the verdict, and an EOF from under the
   parked read unblocked it and handed the key to the loop — a second independent
   false green in the same file.

## Why the pty case keeps its 3s bound

The case is unchanged: it remains the end-to-end rung (a NAME read from
`lineedit.InjectableKeyNames()`, the production constructor, a real pty, the real
turn seam). Loosening its bound was never on the table — the trace shows the
bound is not what fails; the wait behind it has no terminator. The guard covers
the window deterministically; the pty case covers the wiring, and both are green.

## Measurement provenance

`$S = /home/user/.gopi/sessions/01a0f876-d6a4-7af2-b572-751cf728e1f6`

| artifact | what it shows |
|---|---|
| `repro25.log` | 24 PASS + 1 FAIL; red 6.36s vs green 3.27–3.29s |
| `load-at-repro.txt` | `7.08 9.56 9.08`, 4 cores — the load the red was measured under |
| `trace100.log` | the raw pty trace quoted above |
| `guard3-green.log` / `guard3-red.log` | the shipped guard's red→green |
| `guard2-red-local.log` | the false green (fix reverted, guard passing) — the honesty record |
| `base-count2.log` | the neighbour group's `-count=2` parked-goroutine red **without this branch's files**: pre-existing |
