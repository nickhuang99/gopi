<!-- doc-class: historical -->
# The dump artifact froze because a throttle window was never closed

**Task:** `01a0fd34-807c-79b0-909d-29eab89dcbc1`
**Date measured:** 2026-10-02
**Files:** `internal/uiclient/dump.go`, `internal/uiclient/dump_tailflush_test.go`,
`testsuite/chat_info_pty_test.go`
**Symptom (as filed):** on a real pty, after a REPL-native slash line (`/info`),
the `gopi chat --dump-screen` artifact stayed byte-identical from before the line
to 10 s after it, while the same run's pty byte stream carried the whole
`session info:` block cell for cell.

## Root cause — measured, not inferred

**The throttle window had no closure of its own.** `internal/uiclient`.
`ScreenDumper.Write` refreshed the artifact only when a *write* arrived and at
least `dumpMinInterval` had passed:

```go
due := d.now().Sub(d.last) >= dumpMinInterval
if !due { return len(p), nil }   // held — and only a FUTURE write could close it
```

That makes the artifact's freshness depend on the render stream producing another
byte. For a **REPL-native** line there is no such byte: `/info` (like `/help`,
`/queue`, a refusal) is answered by the REPL's own switch, in process, and the REPL
then parks in `ReadLine` in raw mode. The last write of the run is the burst that
painted the block — and every byte of it lands inside the window the immediately
preceding paint had just opened, so every one of them is `due=false`.

### The measurement that named it

A temporary env-gated trace inside the sink itself (`GOPI_DUMP_TRACE=<path>`,
logging `hasBlock=on|off`, `frames`, `dumps` and `due` at `Write`/`flush`/install),
driven by a python pty harness:

```
due        frames=89   dumps=6   flag=true     ← the window OPENS here (block not yet painted)
flush      frames=89   dumps=6   hasBlock=off  ← installs the PREVIOUS screen
installed  frames=89   dumps=7   hasBlock=off
due        frames=90   dumps=7   flag=false    ← the block's first bytes…
due        frames=139  dumps=7   flag=false    ← …and its last, all held
(due       frames=140  dumps=7   flag=true)    ← ONLY after a keystroke, 3 s later
```

Then, with **no keystroke at all**, the artifact was sampled 50 × 200 ms:
`frames=89 dumps=7`, byte-identical across all 50 samples, grid rows 036/037 still
carrying the pre-block lines, while the pty stream carried all 14 `session info:`
rows. ONE later keystroke closed the window and installed the block
(`frames=140 dumps=8`).

**That last observation is the discriminator**, and it is why this is not "a lost
write": the bytes were in the grid the whole time (`Snapshot()` had them, and the
trace's `due` lines show the feed running). The artifact was one **window closure**
short of correct.

### What made this hard to see from the outside

`Settle()` already existed for exactly this class of defect, and its own comment
argues against a timer. That argument is correct **for the turn boundary** — where
the producer *knows* the screen is final and a timer would have to guess, and where
an always-armed timer would write files on an idle surface forever. A REPL-native
slash line has no such caller, so `Settle` never runs for it. The fix therefore
does not contradict `Settle`; it closes the window the *throttle* left open, which
is the sink's own private bookkeeping and which no producer can know about.

## Fix

`internal/uiclient/dump.go`:

- **`armTailFlush()`** — called after a `Write` that actually flushed (i.e. one
  that opened a window). It arms a **one-shot** `time.AfterFunc(dumpMinInterval)`,
  guarded by three conditions: there is a file to write (`path != ""`), no closure
  is already armed (**one per window**), and the sink is not frozen.
- **`tailFlush()`** — clears the armed slot, then runs the *same* `flush(false)`
  the render path runs, so the unchanged-body suppression still applies: a window
  whose frames changed nothing writes nothing. Its error rides the record's own
  `error=` field (Rule 15 — the timer goroutine has no caller to return to).
- **`Close()`** stops and clears any pending closure, so a frozen sink cannot be
  re-opened by a stray timer.

The three guards are what keep this from becoming a writer that runs by itself:
it is armed **only by a real paint** (an idle surface never arms it), it is
one-shot, and it fires at most once per window — the sink's own staleness bound,
which the record's `recorded=` field already claimed.

## Verification

Measured on a real pty, artifact channel only:

| run | window after `/info` | artifact carries the block |
|---|---|---|
| pre-fix | 30 s quiet, no key | **0 / 151 samples** |
| post-fix | 6 s quiet, no key | **30 / 30 samples**, first at t+0.2 s |

Unit guards (`internal/uiclient/dump_tailflush_test.go`), all three **red with the
fix disabled** and green with it:

- `TestScreenDumperInstallsTheFrameAWindowEndedOn` — the burst's tail reaches the
  artifact with no further write, no keystroke and no `Settle`.
- `TestScreenDumperTailFlushDoesNotOutliveClose` — a frozen sink stays frozen.
- `TestScreenDumperArmsOneWindowClosurePerWindow` — one closure per window, the
  slot clears when it fires, and a new due paint arms a new one.
- `TestScreenDumperHasNoTimerWhenThereIsNoFile` — a memory-only sink arms nothing.

End-to-end, `testsuite/chat_info_pty_test.go`:
`TestChatInfo_OnRealPTY_TheDumpArtifactCarriesTheBlock` drives the real binary on a
real pty, types `/info`, and reads the **artifact** back through
`uiclient.ReadDump` (the product's own reader, so a pass also states the file is a
well-formed dump rather than a file containing the right bytes). It asserts the
artifact's session row **equals the stream's** — agreement between the two channels
of one run — and that the copy section (the block's last bytes) landed, which is
what proves the whole block arrived rather than a prefix.

## The harness lesson worth keeping

The sibling case in that file asserts on the **pty stream** and carries a header
explaining why — because the artifact was broken. That header is now updated to
name the artifact's lag as fixed, and the stream case stays as-is: it remains the
right instrument for "what the operator read", while the new case is the instrument
for "what an agent observing the run can read". **Two channels, two cases** — and
the frozen artifact is exactly what you get when only one of them is ever asserted.
