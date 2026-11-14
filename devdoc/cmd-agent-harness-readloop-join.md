<!-- doc-class: historical -->
# cmd/agent's chat test harnesses: the read loop and its join are ONE contract

Status: normative — the standing claim is that **every harness in this family
joins the editor's read loop before it closes the editor**, and that a new arm
cannot silently skip the join (the helper refuses a nil retirement channel).

## The defect, structurally

Four of the five chat test harnesses in `cmd/agent` start the editor's read loop
with `go in.ReadLine()` and split the loop from its teardown:

| harness | file | retirement channel | cleanup before this fix |
|---|---|---|---|
| `newStreamHarnessDumped` | `chat_stream_test.go` | `done: make(chan struct{})` | `stopSource` → wait `done` → `Close` ✅ |
| `panelHarness` | `chat_panel_rows_test.go` | **nil** | `stopSource(); Close()` ❌ |
| `flowHarness` | `chat_flow_rows_test.go` | **nil** | `stopSource(); Close()` ❌ |
| `heightHarnessGated` | `lineedit_height_test.go` | `done: make(chan struct{})` | `stopSource` → wait `done` → `Close` ✅ (silent on timeout) |
| `signalNoticeHarness` | `chat_signal_notice_test.go` | `done: make(chan struct{})` | `stopSource` → wait `done` → `Close` ✅ (silent on timeout) |
| `completionHarness` | `chat_completion_test.go` | result channel `done`, **not a join** | `stopSource(); Close()` ❌ |

The last row is the one the FIRST pass of this work missed, and it is worth
stating why, because the mistake is the general one: `completionHarness` DOES
carry a channel named `done`, so a reader who greps for the pre-fix shape
(`stopSource(); Close()` with no wait) finds it, but a reader who greps for the
*name* `done` concludes it is fine. Its `done` is `chan chatLine` — the loop's
**RESULT**, buffered, and a result is not a retirement: a test may drain the line
while the goroutine is still inside the editor's exit write, and the pre-fix
cleanup closed the editor against a live loop. The generation is now separate
(`loopDone`, closed by `defer` in the loop), which is what makes the join
expressible for it too.

**The nil channel is the whole mechanism.** `done` was left unset in the harness
literal, so the loop body had no `defer close(h.done)` and the cleanup had
nothing to wait on: a nil channel never closes, which means **the join was not
merely omitted — it was inexpressible**. The goroutine could not retire at all.
It stayed parked past the end of its test, and `TestMain`'s close-out
(`reportSuiteGoroutinesAtCloseOut`, `suite_stall_watchdog_test.go`) sampled a
goroutine whose frames are a repository frame, returned the count, and `TestMain`
turned non-zero into a failed run. The reported sighting is the read loop on its
way through the editing line's exit write (`Renderer.Finish`); that is where the
corpse was sampled.

So the observable failure is the worst shape available: **every assertion in the
test body passes and the RUN is FAILED, intermittently**, because it depends on
where the sampling caught a goroutine that could never leave. Measured rate: 4 of
12 runs on the arm that carries it (below).

There is a second, narrower trap on the same arms:
`done: make(chan struct{})` assigned on a `streamHarness{}` literal **after** the
loop was started would `close(nil)` (panic). None did, but the ordering is a trap;
the helper closes it.

## The fix: one helper, so a new arm cannot get half of it

`chat_stream_test.go`:

```go
func (h *streamHarness) startReadLoop(t *testing.T, closeInput bool)
func (h *streamHarness) stopReadLoopOrFail(t *testing.T)
```

- `startReadLoop` **refuses a nil `h.done`** (`t.Fatalf`, naming the field): the
  channel is what makes the join expressible, so the next arm that forgets it is
  told, by name, instead of starting a loop nobody can wait for.
- It registers the cleanup in the measured order — stop the source, **wait for
  the loop to return**, then (optionally) close the editor. The order matters:
  `Close` mutates the region (`Disable`) while the loop reads it from repaint
  (`-race`: `region.Disable` vs `region.eraseMenuPaint`).
- `closeInput bool` is the caller's one real choice — a test that closes the
  harness itself must not have production `Close` run twice
  (`chat_dump_test.go` drives the dump sink through it).
- The cleanup reports a failed join with `t.Errorf`, never `t.Fatalf` (a
  `Fatalf` in `t.Cleanup` panics — the body is over) and never silently: a
  swallowed join is exactly the defect being closed.

All five harnesses now run through it. The sixth (`completionHarness`) reaches the
same join through `joinReadLoopOrFail`, because its first `ReadLine` result IS its
assertion channel and so it cannot take a `streamHarness`'s shape.

## Why the join cannot be folded INTO `panelHarness` (the briefing's question)

It can be, and it now is — but only because the channel was added first, and that
order is the whole finding. The pre-fix `panelHarness` could not join **at all**:
it built its harness literal without `done` (`nil`), started `go in.ReadLine()` as
a bare goroutine with no `defer close`, and returned. A join in the cleanup has
nothing to wait on — `<-h.done` on a nil channel blocks forever, and the loop
could not be asked to signal because its body knew of no channel. So the fix is
not "add a wait to the cleanup"; it is "give the loop a retirement edge and then
wait on it", which is what `startReadLoop` does in one place and refuses to do
half of (a nil `done` is `t.Fatalf`, by name).

The same rule decides the `completionHarness` shape: there the loop does not
retire from a source EOF (`holdAtEnd` parks it), so it needs its own channel
(`loopDone`) distinct from its result, or the join would wait on the wrong fact.

## The guards (Rule 10.5) — three, because a guard on the arms is not a guard on the family

1. `TestChatHarnessReadLoopsRetireOnTeardown` — per arm, `done != nil` AND
   `stopReadLoop()` returns true inside `completionTimeout`. Enumerates the
   `streamHarness` arms (`panel`, `flow`, `height`, `signal`) through
   `streamHarnessArms()`.
2. `TestChatHarnessReaderArmsRetireOnTeardown` — the same contract for the arms
   whose first `ReadLine` RESULT is the artifact (`completion`, via
   `chatReaderHarnessArms()`), asserting the join itself.
3. `TestChatHarnessReadLoopsAreTheOnlyReaders` — **the completeness guard**, and
   the answer to "and how does a new arm not repeat this?". 1 and 2 can only
   enumerate harnesses a human remembered, which is exactly how `panelHarness`
   and `flowHarness` drifted and exactly how this session found the sixth. So
   this one reads every `*_test.go` in the package, strips comments, and fails any
   file that starts a `go func()` owning an `in.ReadLine()` without reaching the
   family's teardown (`startReadLoop` / `joinReadLoop` / `stopReadLoopOrFail`).

   Two measured details, both of which changed the guard: comments must be
   stripped (this family discusses its own defect in prose —
   `lineedit_wire_test.go`'s comment naming `go in.ReadLine()` was reported as a
   loop it does not have), and the glob is **package-relative** (`go test` sets
   the working directory to the package; a repo-relative glob matched nothing and
   the guard's own non-empty check caught that rather than passing vacuously).

   Honest scope: it is a COVERAGE check, not a proof that a given loop is joined
   — the join and its loop are ~150 lines apart, so it is whole-file by
   construction, and a file could mention the name and still start an unjoined
   loop. Guards 1 and 2 close that gap for the enumerated arms (they build the arm
   and assert the loop RETIRES); guard 3 exists for the arm nobody enumerated yet.

**Red proof (structural, deterministic).** The original guard was transplanted
onto an unmodified-`HEAD` worktree, where the panel and flow arms fail immediately:

```
--- FAIL: .../panel: the panel harness left its loop-retirement channel nil, so joining the read loop is not even expressible
--- FAIL: .../flow:  the flow harness left its loop-retirement channel nil, so joining the read loop is not even expressible
```

For the two additions the red proof is a live revert, both recorded in
`evidence/red-proof-completion-and-coverage.txt`:

```
# join removed from completionHarness (the pre-fix shape)
--- FAIL: TestChatHarnessReaderArmsRetireOnTeardown/completion
    the completion harness's read loop did not retire within 5s of the source stopping
# a NEW file in the pre-fix panelHarness shape
--- FAIL: TestChatHarnessReadLoopsAreTheOnlyReaders
    zz_redproof_new_arm_test.go starts a background `in.ReadLine()` but never reaches the family's teardown
# the same guard after deleting that file
ok  	github.com/nick/gopi/cmd/agent	1.305s
```

That is the pre-fix code's own structural state — no timing, no load, no sampling
luck. The probabilistic red proof is the campaign below.

## Where the flake lives, and the measured red→green

The acceptance's arm — `TestChatInfo*` — **is not on `master` or on this
branch**. It lives on `task/01a0fd34807c79b0909d29eab89dcbc1`
(`cmd/agent/chat_info_test.go`), and it calls **`panelHarness(t)`**
(`chat_info_test.go:412`; the file's own comment: "The harness is the /help
panel's own"). `-run TestChatInfo` on this tree selects zero tests
(`ok … [no tests to run]`). So the acceptance's arm and the defective harness sit
on two branches, and the arm is a panelHarness arm.

**RED (measured).** That branch, unmodified, in its own detached worktree
(`/home/user/workspace/gopi-01a0fd34-infoharness`), the arm run 12 times:

```
CHATINFO_PREFIX_TOTAL_FAIL=4 of 12
--- run 1 rc=1 ---   suite goroutine sweep: parked repository goroutines: 1
--- run 4 rc=1 ---   suite goroutine sweep: parked repository goroutines: 1
--- run 6 rc=1 ---   suite goroutine sweep: parked repository goroutines: 1
--- run 10 rc=1 ---  suite goroutine sweep: parked repository goroutines: 1
```

Every failing run has the epic's exact shape: **the test body green, the run
FAILED, and the sweep naming exactly one parked repository goroutine.**

**GREEN (measured).** The same arm on the same tree with only this branch's two
harness files applied:

```
CHATINFO_FIXED_TOTAL_FAIL=0 of 12
```

An earlier reading that this flake "does not reproduce" was a **false negative
from measuring on the wrong tree**: on this branch the arm does not exist, and a
12-run campaign of the sibling arms (`TestChatHelpPanel`, `TestChatNotice`,
`TestChatFlow_AgentOutputTakesWholeBandRows`) on the unmodified pre-fix tree also
came back 0 FAIL in 12 — the residue is short-lived enough that a 12-run campaign
of those arms is not a reliable instrument, while the 33 % rate above is.

## The residual, stated honestly

The loop could never retire, so it was parked in the source's read, and the
reported sighting was that read loop at some later instant. `panelHarness` runs at
the very start of the package (its file sorts early), which is consistent with a
lock-step park, with the sweep's sampling catching a loop on its way through the
editor's exit write (`Renderer.Finish`) on some runs and the parked read on
others. The measurement here establishes the *rate* (4/12) and the *mechanism*
(the join was inexpressible); it does not pin which frame each of those four
samples carried, and this document does not claim it does.

## Not done

- No skip, no baseline relaxation, no change to `internal/lineedit`: the sweep's
  predicate and `suiteSweepLeakLimit` are untouched.
- The completeness guard is whole-file and mention-based (see its own scope note
  above). It would NOT catch a file that reaches the family's teardown for one
  loop and hand-rolls a second unjoined loop; nothing in this package does that
  today, and the enumerated-arm guards would not catch it either.
- The `signal` and `height` arms were brought under the retirement guard during
  this pass but their own pre-fix cleanups already had the sequence (their defect
  was a SILENT timed-out wait, now reported) — so their red proof is the shared
  transplant, not an arm-specific revert.
- The whole-package run (`go test ./cmd/agent`) does not complete: on the
  unmodified pre-fix tree the binary hits its own 10-minute budget and dies with
  `panic: test timed out after 10m0s` — a panic that PREEMPTS the close-out
  sweep, which is why a whole-package run is not the instrument that can see this
  defect at all. Two pre-existing, unrelated FAILs precede it
  (`TestTaskShowRendersTheSameDigestForAllThreeAddresses`,
  `TestSyncBarePeersTestCoversEveryChangedFile`). The branch-scoped arms plus the
  close-out sweep they trigger are the evidence.
