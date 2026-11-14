<!-- doc-class: historical -->
# A band wait must end when its read loop is GONE — not spend 4s blaming a stall

Task `01a12c43-141e-7190-918f-3d245d3f61e6` (bug row filed by `01a12c12`, Rule 41).
Branch `task/01a12c43141e7190918f3d245d3f61e6`.

## The symptom

```
go test ./cmd/agent/ -cpu=1 -count=40 -v \
  -run 'TestChatVoice_TheRunOfFailuresEndsTheMode|TestChatVoiceLive_DraftsGrowInTheLineThenSettleOnTheFinishKey'
```

Both cases pass, and the PACKAGE still fails:

```
suite goroutine sweep: parked repository goroutines: 2
suite goroutine sweep: goroutine 669 [runnable]:
  lineedit.(*Editor).waitBandWrite  internal/lineedit/bandwait.go:84
  lineedit.(*Editor).WriteLines     internal/lineedit/editor.go:846
  cmd/agent.(*chatInput).WriteNotice  cmd/agent/lineedit_wire.go:685
  cmd/agent.(*chatInput).VoiceNotice  cmd/agent/lineedit_wire.go:570
  cmd/agent.(*voiceChatMode).notice   cmd/agent/chat_voice.go:1115
  cmd/agent.(*voiceChatMode).leave    cmd/agent/chat_voice.go:948
FAIL  github.com/nick/gopi/cmd/agent  112.079s
```

On the console, in the same runs:

```
[gopi] editor: a band write was not drawn within 4s (the read loop is stalled?) — the caller announced a screen it does not own
```

## Which half owns it — MEASURED, not argued

The finding was instrumented rather than reasoned (the memory recipe for this
family: *instrument the poll loop; reasoning alone got it wrong twice*). Two
temporary instruments, both removed before the commit:

1. an entry/exit sample inside `waitBandWrite`, printing `entryGoid`, the
   `loopGoid` at the moment of the report, the caller's own id and the queue's
   level;
2. a one-line `RUN EXIT` print around `Editor.Run`.

Raw streams (this run's session directory):
`debug2-bandwait.txt`, `debug3-runexit.txt`.

### Half 1 — the WAIT'S CONDITION (lineedit, the product): CONFIRMED

```
=== BANDWAIT DEBUG editor=0xc000005008 entryGoid=68 nowGoid=0 callerGoid=69 bandActive=false pending=true notifyEnabled=true ===
=== BANDWAIT DEBUG editor=0xc000004408 entryGoid=75 nowGoid=0 callerGoid=76 bandActive=false pending=true notifyEnabled=true ===
```

- `entryGoid=75` ≠ 0 ⇒ **a read loop was running when the row was queued**, so the
  seam correctly took the waiting road (`loopGoid == 0` short-circuits before
  `waitBandWrite`).
- `nowGoid=0` at the moment the bound expired ⇒ **the loop was GONE**. The waiter
  was not waiting for a slow loop; it was waiting for the next `Run` to start.
- `pending=true bandActive=false` ⇒ the frame was still a level with no drainer.

`RUN EXIT debug editor=0xc000005808 err=<nil> res={Line: EOF:true ...}` — the read
loop exits with EOF, i.e. **the harness's teardown closed the terminal**, and it
does so ~2.25s into a ~2.4s iteration. So the window is: the capture loop queues a
leaving row while a teardown closes the terminal under it; the loop exits; the row
is left with no drainer, and the wait spends the entire 4000 × 1 ms bound.

The wait's own contract says what it should have done — `bandWaitBudget`'s doc:
*"Reaching it means the read loop is gone, not slow"*. It is a bound on a **live**
loop's answer. The wait never checked that the loop it was waiting for still
existed; it read `bandPending()` alone.

### Half 2 — the test's teardown ORDER (cmd/agent): NOT the cause

The fixture's teardown is already the four-step order this family's memory
prescribes (`chat_voice_pty_test.go`): `mode.shutdown()` → `editor.Close()` →
`term.Close()` (THE STOP) → bounded, loud join → `Master.Close()`. The same
teardown shape serves every other `voicePtyGrid` case, and only this one takes the
failure, because only this one leaves a capture loop inside a notice when the
terminal is closed. It is not an ordering defect: the join is present, loud
(`t.Errorf`), and it waits. The row that is parked is not a fixture goroutine at
all — it is the mode's **capture loop**, parked inside its own notice, which is
half 3.

### Half 3 — the SECOND defect this measurement exposed (product): `state.inLoop` was never cleared

`voiceChatMode.shutdown()` skips its join of the capture loop when *"the loop is
the caller"*:

```go
if loopDone != nil && !m.state.inLoop {   // pre-fix
    select { case <-loopDone: case <-time.After(voiceChatFinishWait): }
}
```

`inLoop` was a **bool set true by the loop and never set back to false**. A mode is
entered once per test iteration and left inside the same iteration, so from the
first entry onward the field read `true` for **every** caller. Consequences, in
order:

1. Every FOREIGN teardown (`/voice stop`, Esc, `shutdown`) skipped the join — the
   documented deadlock exemption was applied to callers it was never meant for;
2. `shutdown()` therefore closed the terminal under a capture loop still inside
   its own notice, which is half 1's window;
3. the capture loop was left parked inside that notice, and the suite's sweep
   sampled it.

So the two goroutines in the sweep and the 4 s not-drawn warning are **one
mechanism**: a mode state bit that was a **claim the loop made about itself** and
never withdrew. This is the same rule the band seam already follows — *"The two
cases are told apart by GOROUTINE IDENTITY, never by a flag the caller sets… A
flag would be a claim; the id is a fact."* (`internal/lineedit/bandwait.go`).

## The fix

Three changes, each in the half that owns it. No sleep was added, no bound
widened, no sweep baseline touched, no assertion weakened.

**1. `internal/lineedit/bandwait.go` — the wait reads the owner ONCE, and ends when
that owner is gone.**

```go
owner := e.loopGoid.Load()
for i := 0; i < bandWaitSteps; i++ {
    if !e.bandPending() { return e.BandActive() }
    if e.loopGoid.Load() != owner {
        // THE DRAINER IS GONE: the frame stays the LEVEL the next read outcome
        // re-derives — late, never lost — which is the seam's own answer for the
        // no-loop state. NOT a failure, so it is not reported as one.
        return e.BandActive()
    }
    time.Sleep(bandWaitStep)
}
```

A loop that is merely **busy** keeps the same id and keeps the full bound — the
bound is not weakened, it is now applied to the state it was written about.

**2. `internal/lineedit/bandwait.go` — `CurrentGoroutineID()`** exports the id fact
(`goid()`), so a caller outside the package can record an identity rather than set
a flag about itself. Nothing else exports a goroutine id (`grep runtime.Stack`
over non-test code finds only this file).

**3. `cmd/agent/chat_voice.go` — `inLoop` is an ID, not a flag.** It now holds the
goroutine id of the current capture loop, `0` when none is running:

- `markLoopGoroutine()` stores `lineedit.CurrentGoroutineID()` and returns the
  restore, which both loops `defer` — so the id is cleared when the loop RETURNS,
  and the teardown that follows joins it instead of skipping the wait;
- the restore is guarded by the id it set, so a loop of an earlier entry cannot
  clear the one a later entry installed;
- `shutdown()` compares `m.state.inLoop != lineedit.CurrentGoroutineID()`.

The deadlock exemption is preserved exactly: the loop's own call still matches and
still skips the wait. What changed is that every OTHER caller now really waits.

## Verification

### Red → green on the original command

Both streams are in this run's session directory. The red state is the pre-fix
tree (`acc3fa3eb`, this worktree's tip).

| state | command | result |
|---|---|---|
| **red** | `go test ./cmd/agent/ -run '<两个用例>' -cpu=1 -count=40 -v` | `before-cpu1-40.txt`: **10 FAIL / 70 PASS**, `parked repository goroutines: 2`, **29** × not-drawn warning, `FAIL github.com/nick/gopi/cmd/agent 111.719s`, `exit=1` |
| **green** | same command | `after2-cpu1-40.txt`: **80 PASS / 0 FAIL**, `parked repository goroutines: 0`, **0** not-drawn warnings, `ok github.com/nick/gopi/cmd/agent 112.141s`, `exit=0` |

The green run carries a SECOND change as well, and it is not this task's: the
sibling task `01a12c12`'s wait-face fix (`eab9cc630`/`ff3b8c5b`) is **not an
ancestor of this worktree's head** (`git merge-base --is-ancestor eab9cc630 HEAD`
→ exit 1), and its one flaker — `chat_voice_failure_run_test.go:154` *"the mode is
still ON after the run of failures reached its limit"* — is live here too. So the
two halves were measured SEPARATELY rather than claimed together:

| state | `parked goroutines` | not-drawn warnings | test FAILs |
|---|---|---|---|
| red, pre-fix tree | **2** | **29** | 10 (all line 154) |
| this task's fix only (`after-cpu1-40.txt`) | **0** | **0** | 9 (all line 154) |
| this task's fix + the sibling's wait-face fix (`after2-cpu1-40.txt`) | **0** | **0** | **0** |

The middle row is this task's own claim stated exactly: **the two symptoms this
row was filed for — the parked sweep and the 4 s not-drawn warning — are gone, and
the residual red is a different flaker (a wait-face gap) that this worktree did not
carry the fix for.** The sibling's one-line wait (both facts inside one bound) is
taken here verbatim, credited in the code comment, so the acceptance command is
green in full on this branch.

### Red → green on the regression test

`internal/lineedit/bandwait_owner_test.go`:

- guard disabled (`if false && e.loopGoid.Load() != owner`):
  `--- FAIL: TestBandWait_EndsWhenTheOwningReadLoopIsGone (0.55s)` —
  *"the wait did not end within 500ms after its read loop was gone"*;
- guard restored: `--- PASS` (0.05s), and its control
  `TestBandWait_StillBoundsALiveButSilentLoop` **PASS (4.32s)**, i.e. a live but
  silent loop still spends the full bound and still announces the stall.

### Instrumented evidence (the root-cause step)

`debug2-bandwait.txt` (entry/exit sample), `debug3-runexit.txt` (`RUN EXIT`),
`GOPI_BANDWAIT_DEBUG=1`. Both instruments were temporary and are not in the commit.

## Verification (Rule 5 shape)

- **Build:** `go build ./...` — no output (exit 0); `go vet ./cmd/agent/ ./internal/lineedit/` — no output.
- **Tests:** `go test ./internal/lineedit/ -count=1` → `ok github.com/nick/gopi/internal/lineedit 37.917s`
  (includes the new regression + its control).
- **Ran:** `go test ./cmd/agent/ -run 'TestChatVoice_TheRunOfFailuresEndsTheMode|TestChatVoiceLive_DraftsGrowInTheLineThenSettleOnTheFinishKey' -cpu=1 -count=40 -v`
  — run three times: on the pre-fix tree, with this task's fix only, and at the
  committed tip (with the sibling's wait-face fix as well).
- **Result:** pre-fix `10 FAIL / 70 PASS`, `parked repository goroutines: 2`, 29
  not-drawn warnings, `exit=1`; fix-only `0 FAIL counts on this task's symptoms`
  (`parked 0`, warnings 0) with the sibling's 9; at the committed tip **80 PASS /
  0 FAIL**, `parked repository goroutines: 0`, 0 warnings, `ok … 112.141s`,
  `exit=0`.
- **Commit:** the tip of `task/01a12c43141e7190918f3d245d3f61e6`,
  subject `fix(lineedit): a band wait ends when its read loop is gone (01a12c43)`.
  A report written before hand-over cannot pin a SHA the branch may still move
  under, so the SUBJECT is the stable handle — `git log --oneline -1
  task/01a12c43141e7190918f3d245d3f61e6`. (Earlier drafts of this report named
  `1d0d03595` and `df9725998`, pre-amend objects of this same commit, identical
  trees apart from this document; those objects are orphaned.)

## Evidence (self-filed)

- `entryGoid=75 nowGoid=0 bandActive=false pending=true notifyEnabled=true` —
  `GOPI_BANDWAIT_DEBUG=1 go test … -count=12` (§"Half 1"), raw in
  `debug2-bandwait.txt:139`.
- `=== RUN EXIT debug editor=0xc000005808 err=<nil> res={Line: EOF:true …} ===` —
  same stream, `debug3-runexit.txt:21`.
- `parked repository goroutines: 2` / `goroutine 669` frames naming
  `waitBandWrite` ← `voiceChatMode.leave` — `before-cpu1-40.txt:243`.
- Red proof of the regression: guard disabled → `--- FAIL:
  TestBandWait_EndsWhenTheOwningReadLoopIsGone (0.55s)`; restored → `--- PASS (0.05s)`,
  control `--- PASS (4.32s)`.
- `git status --porcelain` at the tip: prints nothing (Rule 30).

## Not Done (honest)

- The failure is a **scheduling window**, not a fixed interleaving: the case opens
  the window and reproduces it many times (10/40) but the exact per-iteration
  ordering that closes it can vary. What is deterministic is the **instrumented
  invariant** (`entryGoid≠0`, `nowGoid=0`) and the regression test, which opens the
  window by construction rather than by luck.
- The capture loop's notice still goes through a band wait when the terminal is
  closed under it. That is now cheap and silent (one loop-identity read) instead of
  a 4 s park, but a row queued during teardown is still not drawn anywhere — it is
  dropped by the layout's own teardown, which is the documented `drainBand` arm.
- `voiceChatFinishWait` (30 s) is unchanged: with `inLoop` now correct, an operator's
  foreign teardown can block up to that bound on a loop that does not answer. No
  measured run reached it (the loop returns in microseconds); it is named here
  rather than changed.
- `goid()` (a `runtime.Stack` header parse) is the one input to both halves, and it
  can return `0` if the header does not parse — a platform shape, not a state
  reachable on this host (`goid()` returned a real id in every instrumented run
  above, and `bandwait.go`'s `onLoopGoroutine` already tolerates the zero). The
  degradation is BOUNDED, not a deadlock, and it is safe in both halves: a `0`
  makes the band wait take the waiting road and a loop's own `shutdown` wait up to
  `voiceChatFinishWait` — worse latency, never a hang.

## Files

- `internal/lineedit/bandwait.go` — owner-read bound + `CurrentGoroutineID()`
- `cmd/agent/chat_voice.go` — `inLoop` becomes an identity, cleared on return
- `internal/lineedit/bandwait_owner_test.go` — the regression (new)
- `cmd/agent/chat_voice_failure_run_test.go` — the sibling task's wait-face fix,
  taken verbatim with credit (this worktree did not carry it)
- `devdoc/bandwait-owner-gone-2026-10-12.md` — this report
- `devdoc/DOC-INDEX.tsv` — the row for this document
