<!-- doc-class: historical -->
# A SIGINT at the chat prompt recorded nothing — CaptureSignals owned it

**Task:** `01a163d4-b44a-7ae4-a767-9ffad2b6b82f` · **Measured:** 2026-10-22 ·
**Branch:** `master`, base `ef5ed68`

## The report that started it

Session `b9936006-5e63-4668-8be1-e08501bcbe21` stopped writing at 18:32:59. Its
`session.jsonl` ends on a `request` record — 1803 lines, **zero `session_end`** —
and its `session.dump.json` (written later, at 19:24, by a sweep) categorised it:

```json
{"category": "orphan-no-session-end",
 "note": "process died without session_end (kill/panic/power loss); ..."}
```

No `signal-provenance.jsonl` existed, and `console.log` carries none of the
close-out sentences (`finalizing`, `bye.`) — grep count 0. So the signal, the
exit code and the last state were all unknowable after the fact.

## What the previous fix had already covered, and what it had not

A fix from the day before (session `ab9e0023`) added `CloseSignal`,
`deepseek.CategorySignal` and `settleProvenanceRecord` to the chat close-out.
That half is real and still works. Measured on this branch's binary, on a real
pty, one signal each at a booted prompt:

| signal | exit | session_end | provenance | dump category |
|---|---|---|---|---|
| `SIGTERM` | 1 | 1, names SIGTERM | `SIGTERM` | `signal` |
| `SIGHUP` | 1 | 1, names SIGHUP | `SIGHUP` | `signal` |
| **`SIGINT`** | **-2** | **0** | **ABSENT** | **NO DUMP** |

So the epic's premise ("a signal kill leaves no record") was too broad, and the
briefing said so honestly. **SIGINT was the one signal that could not reach the
close-out at all.** That is what this change fixes.

## Root cause, and why SIGINT alone

A raw-mode terminal clears `ISIG`, so the Ctrl+C **key** arrives as the `0x03`
byte and never becomes a signal; a **real** SIGINT (`kill -INT`, a process-group
delivery, an ssh teardown) takes the signal path instead. That path is
`internal/lineedit`'s `Terminal.CaptureSignals(os.Interrupt)` — and its handler
is the **fatal** one:

```go
if fn := t.sigTarget.Load(); fn != nil && (*fn)() {
    return                       // the target CLAIMED it — the process lives
}
_ = t.Close()
signal.Reset(sigs...)
_ = syscall.Kill(syscall.Getpid(), s)   // re-raise: die on the default disposition
```

The target is installed by `chatInput.SetInterrupt` (`cmd/agent/lineedit_wire.go`),
and before this change it answered:

```go
if !active() { return false }    // no turn in flight → "not mine, let it die"
```

`active()` is `turnRunning(sess)`. **At the prompt there is never a turn in
flight**, so SIGINT at the prompt always answered `false` → restore attributes,
`signal.Reset`, re-raise → exit `-2` in **3 ms**, three milliseconds ahead of the
REPL's own close-out. SIGTERM and SIGHUP never enter `CaptureSignals` (it is
registered for `os.Interrupt` only), which is exactly why they worked and this
one did not.

The one case that did work was a turn in flight — `active()` true, the turn gets
cancelled, the session stays open. That shape is pinned by
`testsuite/chat_pty_interrupt_test.go` and is unchanged.

## The fix — two halves, one idea

The seam now claims a terminating signal **unconditionally**, and the two halves
of the fix are the two windows where it can be asked.

**1. At the prompt (`SetInterrupt`).** `!active()` no longer means "let it die".
It means "end this session", which is the same ending the Ctrl+C *key* gets one
layer up (editor → `KeyInterrupt` outside a turn → `Run` returns `Interrupted` →
`leaveChat` → `finishChat`). Claiming does **not** create a second close-out
path: `cli_chat.go` registers its own `signal.Notify(sigCh,
chatGracefulSignals()...)` watcher *before* the input layer is built, Go relays
one delivery to **every** registered channel, and that watcher closes
`interruptCh` — which wakes the REPL's prompt select and breaks the loop to the
same `finishChat` as `/quit`. `return true` only stops lineedit from killing the
process before that unwind can run. The terminal is restored by the closeout's
own `input.CloseOut()`, the same restore the fatal path was doing by hand.

**2. The start-up window (`newChatInputWithPalette`).** `CaptureSignals` is
installed at line ~1116, but `SetInterrupt` is called by the REPL hundreds of
lines later — after the banner, the geometry probe, the dump arming and the
history load. In between, `sigTarget` is `nil`, and a nil target means the fatal
path. **This was found by the new regression test, not by reading**: the test
fires SIGINT as soon as the banner is up, which lands inside that window, and it
failed against the first version of this fix with `exit = -1`. The provisional
handler (`func() bool { return true }`) is installed immediately after
`CaptureSignals` and is replaced by `SetInterrupt`'s turn-aware one once the REPL
can answer the question. Claiming provisionally is safe for the same reason it is
safe at the prompt: the REPL's watcher is already registered by then.

## Why this satisfies the epic's constraints

- **Does not block the exit.** Measured `kill → reap`: **64–65 ms**, against a
  5 s ceiling asserted by the new test. The close-out writes one JSONL line, one
  dump and settles the recorder's record; none of that can hang the unwind
  because the exit path is the one `/quit` already uses.
- **No new dependency, no new signal framework (Rule 35).** No goroutine, no
  file, no script, no new `signal.Notify`. The `lineedit` seam, the REPL's
  watcher and `signalrec` all already existed; this change changes **what one
  existing seam answers**, in the one file that already owned the answer.
- **Ctrl+C at the prompt keeps its behaviour.** The Ctrl+C **key** path is
  untouched (it is a byte, not a signal). Measured after the fix: the `0x03`
  byte at the prompt still exits **0** with one `session_end` whose `exit_code`
  is 0 — the historic clean ending.
- **`/quit` unchanged.** Measured: exit 0, one `session_end`, `bye.` on screen.

## Red → green, the same harness and the same command

The harness is `testsuite/chat_pty_sigint_prompt_test.go`, and it is run twice
with **only the binary pin differing** (`GOPI_BIN`):

```
# pre-fix binary (lineedit_wire.go restored from HEAD, built to /tmp/gopi-prefix)
GOPI_BIN=/tmp/gopi-prefix go test ./testsuite/ -run TestChatPTY_SIGINTAtPrompt_RecordsSessionEnd -count=1 -v
  → chat exit = -1 after SIGINT, want a POSITIVE status: a negative code means the
    process died on the default disposition, so no close-out ran (this is the
    session-b9936006 shape)
  → FAIL

# post-fix binary
GOPI_TEST_BIN=$PWD/bin/gopi go test ./testsuite/ -run TestChatPTY_SIGINTAtPrompt_RecordsSessionEnd -count=1 -v
  → measured: exit=1, kill→gone=54.704761ms
  → PASS
```

The restore of `lineedit_wire.go` was done by `git show HEAD:<path> > <path>`, and
the fix put back from a copy taken before — the tree's final content is the fixed
one (verified by `git diff` in the task report).

## What this does NOT fix, stated honestly

`SIGKILL` cannot be caught by any process, so a `kill -9` still leaves no
`session_end`. The existing reconciliation covers that shape
(`orphan-no-session-end` dump + `sweep-zombies`), and it is not in this epic's
scope. Nothing else about chat's exit paths was changed.
