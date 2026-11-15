<!-- doc-class: normative -->
# Task Report: `gopi chat` M2 — A5 (scrollback-safe streaming + one-line status) and A7 (completion menu)

Branch: `feat/chat-m2-streaming` (round-6 fix `f5f16b6d`, round-7 harness fix `71f0f853`) (worktree `~/workspace/gopi-chat-m2`), off master `c6abf8b9`,
merged with master `6a105a48` and rebased-onto state through `c5010c2a`.
Builds on the landed M1 editor (`a2dc2c5e`, `internal/lineedit`).

Status: **DOABLE, delivered.** A5 + A7 + the round-4 real-terminal freeze fix are all on the
branch. Read "Round 4" and "Round 7" before trusting any earlier verification claim in this
file — round 7 found that the round-6 EVIDENCE was produced by a non-idempotent harness and
re-took it, and that the layout state was read from two goroutines.

**Round-7 headline (one paragraph).** The round-6 layout was correct and its assertions were
green; the screen it quoted was not the screen the assertions read (a stateful cell grid fed
its whole write history once per replay). That is fixed (`71f0f853`) and the real-emulator A/B
re-measured on the current binaries (PROBE-LINE-01 present: 1 fixed vs 0 pre-fix). Re-running
the suite under `-race` then found the region's published layout being read from the REPL's
goroutine — a real race, reachable because of round 6's accessors — and that is fixed too
(`d67da00b`, `go test -race ./internal/lineedit/` was RED before it and is green after). One
race remains, is pre-existing on master, and is DISPATCHED rather than half-fixed
(`01a0aa9a-84de-7555-b14a-c1f087d82515`): the composer and the status row still write to one
terminal from two goroutines.

## Completed

### A5 — scrollback-safe streaming + one-line status

- **`internal/lineedit/region.go`** — the reserved top-region compositor, built on a
  **scroll region (DECSTBM)** so the terminal itself guarantees the invariant: the chrome
  (editing row, menu, status row) is OUTSIDE the region, so committing answer lines cannot
  move it. The band is at the screen's FIRST row — the scrollback condition (round 6).
  `computePlan` is a **pure** function (band → editing row → menu → status row), so the
  layout arithmetic is testable without a terminal and exists in one place.
- **`internal/lineedit/answer.go`** — `AnswerRenderer`, the codex `live_wrap.rs
  drain_commit_ready` policy: **a finalized line leaves the live region exactly once and is
  never repainted again**. Only the in-flight line stays live. Committing = write the row +
  ONE region scroll, so the repaint count is linear in the answer's lines, not in the number
  of deltas. Plus the one-line status (`Status`), composed and truncated inside the package.

  > **CORRECTION (round 5 — see that section).** Earlier revisions of this report claimed
  > the line was "committed to the scrollback". With the band's scroll-region top margin
  > below the screen's first row, the terminal **discarded** every line scrolled off that
  > top, so the invariant this code actually provided was the weaker, honest one quoted
  > above — *leaves the live region exactly once, never repainted* — which is the
  > no-repaint-storm half of A5.
  >
  > **RESOLVED (round 6).** The layout was inverted — the band is now anchored at the
  > screen's FIRST row (`editorTopRow = 0`), the chrome (editing line → menu → status) sits
  > BELOW it — so every finalized line is pushed off the screen's top row by construction and
  > IS saved into scrollback. Measured on real xterm 372: oldest reachable line went from
  > `PROBE-LINE-14` (13 of 30 destroyed) to `PROBE-LINE-01`. Both halves of A5 are delivered;
  > `Screen` now models the emulator's rule, and `TestA5B_InteriorRegionTopDiscardsLines` is
  > the negative control that would go red if the top margin moved off row 1 again.
  >
  > **EVIDENCE RE-TAKEN (round 7).** The scrollback claim is now backed by an idempotent
  > harness AND a real-emulator A/B re-measured on the current binaries (see the round-7
  > sections): the round-6 quote came from a harness that re-fed its whole write history into
  > a stateful grid, so the picture it printed was not the screen its assertions read. The
  > measurement itself (PROBE-LINE-01 reachable, `PROBE-LINE-01` present: 1 vs 0 pre-fix)
  > holds on the re-run.
- **`cmd/agent/chat_stream.go`** — the turn-level wiring: `chatTurnRenderer` picks
  the reserved path when the layout is installed and the **historic buffered renderer
  byte-for-byte** everywhere else; `chatStatusSource` renders model / iteration / tokens
  from counters the REPL already keeps (no new state).
- **`cmd/agent/cli_chat.go`** — the turn loop feeds deltas through `chatTurnRenderer` and
  ticks the status before/after each turn. Single-session consolidation untouched: one
  agent, one session id, one JSONL, one `session_end`, history still captured from
  `ag.Ctx.Messages`.

### A7 — completion menu below the line

- **`internal/lineedit/menu.go`** — the menu (drawn BELOW the editing line, outside the
  scroll region), the `Menu` value, and `completionState`: candidates are computed **on a
  goroutine** off the read/redraw path (hermes `ThreadedCompleter` rationale). The editor
  never waits: it reads the last published snapshot and re-asks after a buffer change.
- **`Completer` is the stub seam** — `func(line string, cursor, limit int) []Candidate`;
  tests drive a deterministic stub, production installs `NewSlashCompleter()`.
- **`cmd/agent/lineedit_wire.go`** — `NewSlashCompleter`: slash commands from the SAME
  registry the REPL dispatches through (`slashCommandNames()`), plus the session commands,
  plus `@`-prefixed file candidates from a bounded directory scan.
- Typing `/` opens it, Tab completes onto the line, Up/Down move the selection, Esc closes
  (and ERASES the rows it painted).

### Verification (regression tests, per increment)

- **A5** (`internal/lineedit/answer_test.go`, 14 tests): rendered vt100 **cell-grid**
  assertions — the band's contents, the live row carrying only the in-flight answer, the
  scroll region matching the band, the chrome immobile under many answer lines, a bounded
  write count for a fixed delta stream, split-rune hold-back, and the resize path.
- **A5 wiring** (`cmd/agent/chat_stream_test.go`): the same claims through the REPL's own
  seam (`newChatTurnRenderer` over `chatInput.Answer()`), including the cooked-path byte
  stream and `band.Committed() == lines` (no line committed twice).
- **A7** (`internal/lineedit/menu_test.go` + `-run TestChatCompletion`
  `cmd/agent/chat_completion_test.go`): menu contents, Tab completion, Esc close, selection
  movement — all read off the cell grid through a stub completer.

### Round 2 — three defects found by hunting a 1-in-12 flake (commit `d29f00a1`)

The A7 bounded-repaint test failed once every ~12 runs, always by exactly 2 writes. Chasing it to the root cause
(not to a wider budget) found three real production defects. Each ships with a regression test that FAILS on the
pre-fix code.

13. **A completion publish could be lost entirely (the editor never drew the menu).** The editor's read wait is
    `poll(-1)`; the completion worker publishes from its own goroutine. If the publish landed just after the reader
    had decided to park, nothing woke it and the menu only appeared on the NEXT keystroke — typing `/` showed
    nothing. This is why the earlier design had a fixed poll budget, and why that budget could not work (see 14).
    Fix, in two halves:
    - **A real interrupt**: `internal/lineedit/event.go` (new) adds `wakePipe`, a one-way notification pipe;
      `raw.go`'s `EnableWakeNotification` installs its read end in the same `poll(2)` set as stdin and the
      SIGWINCH pipe, and `EventWake` is a new wake reason. `cmd/agent/lineedit_wire.go` wires it into the chat
      editor; a pipe that cannot be built **announces the degradation** (Rule 15: "completion arrives on the next
      keystroke only").
    - **A publish generation that cannot be missed**: the completion state counts publishes (`rev`); the editor
      records the generation its last repaint drew (`menuRev`). "A published menu is undrawn" is then a counter
      comparison — seen or not-yet-seen, never lost. The fixed give-up budget is GONE; the wake cadence is
      re-armed (fast `completionPollMS` for `completionFastPollWakes` checks, then `completionBlockedMS`) and
      cannot spin, because the read really parks.
    Repro: `TestChatCompletion_SlowCompleterStillGetsItsMenu` (3/3 FAIL on `0ecda146`, PASS after).

14. **An idle bounded wait repainted the line unconditionally** (the extra 2 writes). The read loop's
    `EventNone` fallthrough — a wait that expired with no input and nothing to classify — called `repaint()`
    regardless. On a source **without** a wake pipe, that bounded cadence IS the fallback, so every poll bought a
    full redraw: a repaint per poll, invisible to the user and a storm by count. Fix: the only thing such a wait
    can still discover is a resize whose notification was coalesced away, so it redraws when the geometry really
    moved (`geometryChanged` remembers the size the last redraw saw).
    Repro: `TestChatCompletion_IdleWaitsDoNotRepaint` — **differential**, so no budget can hide it: the same
    script with three idle waits must cost exactly the same number of writes as without them (measured: 13 vs 7
    before the fix, equal after).

15. **`menuDirty` outlived the redraw it asked for.** The flag was cleared at some call sites but not others, so
    after Escape the editor kept a standing permission to perform one redundant full redraw per wake. Fix:
    `repaint()` is the ONE place that consumes the flag (clear it where the paint happens, not per call site).
    Repro: `TestChatCompletion_ClosedMenuIsNotRepaintedByAWake` — a wake carrying no news must write ZERO bytes
    (measured: 2 writes before the fix). The assertion needs a write-independent "the read loop came round"
    signal, which is why `scriptedSource` now exposes `timeoutCount()`.

**Test-harness defects fixed in the same pass** (they were masking/creating the flake): `recordingWriter`
appended from the editor goroutine while the test read it (a data race, and the stale read is exactly the redraw
a poll was waiting for) — now mutex-protected; `scriptedSource`'s bounded wait is a real park
(`scriptedWaitBudget`) so the completion worker gets its turn; the notification pipe is watched by ONE
long-lived goroutine (never one per read); `a7Harness.teardown` stops `Run` BEFORE `Close` (Run's goroutine
reads editor state every iteration); the A5 resize test waits for `Run` to end instead of polling live region
state.

**Evidence**: `go test ./internal/lineedit/ -count=40 -race` CLEAN — the same command had 27 failures across 12
runs before; `-count=15 -race` CLEAN; a 400-run write-count histogram for the bounded-repaint sequence is
46/46 **deterministic** (was `map[46:298 48:2]`); `go vet` + `gofmt` clean; `TestStyleGate` counts identical to
master (after adding the two required annotations: the deliberate `//stylegate:allow-sleep` on the
slow-completer hold, whose whole point is real elapsed time, and a `//stylegate:ignore` on the teardown close).

### Round 3 — a real branch-caused gate violation, caught by master-vs-branch comparison

The full-suite comparison initially reported "every branch failure also fails on master". That was **wrong**, and
the correction is the point of this section.

16. **`internal/lineedit` created scratch on the boot device.** The new package uses `t.TempDir()`
    (`history_test.go`) but shipped **no** `TestMain` routing its scratch root through `internal/testutil`, so its
    scratch data landed on the ambient temp root — `/tmp` on this host, the boot partition (Rule 22). Two gates
    caught it and both are **green on master**: `internal/testutil.TestEveryTempUsingSuiteIsPinned`
    (`scripts_gate_test.go:248`) and `TestPinCoverageTable` (`pin_coverage_test.go:92`, "29 scratch-using /
    28 pinned"). Fix: `internal/lineedit/testmain_scratchroot_test.go` calling
    `testutil.RunSuiteMain(m, "lineedit")` — the shape every other scratch-using package already has.
    Repro: `go test ./internal/testutil/ -run 'TestEveryTempUsingSuiteIsPinned|TestPinCoverageTable'` — FAIL
    before, PASS after.

    Why the first comparison missed it: the master-vs-branch pass compared only the **`--- FAIL` names** of the
    two full-suite logs, and master's run had gone red in `internal/testutil` on a *different* test, so
    `internal/testutil` was present in both lists. A per-**test** diff is the only honest form; a per-package
    diff hides exactly this class of defect.

**Stale baseline, not a defect (recorded so nobody re-chases it).** Three further failures in the first
comparison (`TestCLIContractTaskServeAliasPrintsOneLinePointer`, `TestTasksCLIListHeaderCountMatchesRows`, and
the `[gopi] tmpdir: TMPDIR is already set …` banner noise in their output) were **not** real: this branch's base
was `c6abf8b9`, and master had since landed `176d8105` — *"make the temp workspace an ordinary parameter, and
stop announcing it"* — which removes exactly that banner. The branch was simply 6 commits behind master. After
`git merge master` (no conflicts, `e415b616`) the banner is gone and both tests pass. **The lesson: when a
branch's base is not master's tip, compare against the MERGED tree, or the comparison measures master's own
progress, not the branch.**

### Round 4 — the real-terminal freeze: a self-pipe drained on a BLOCKING descriptor

**This round exists because the previous report was wrong about its own evidence.** The committed
report claimed `testsuite TestChat 5/5 PASS`, but the evidence file it pointed at
(`.evid/round2-testsuite.log`) shows **both** PTY tests `--- FAIL` with 60s timeouts. Verified
against the M1 base (`~/workspace/gopi-chat-m1` @ `a2dc2c5e`, binary rebuilt): both PTY tests
**PASS there in 6.35s / 2.55s** — so this was a real A5/A7 regression on the real-terminal path
that every scripted-source test was structurally blind to.

17. **The editor parked in `pipe_read` on its first notification.** `drainPipe` (`raw.go`) loops
    `unix.Read` until the descriptor reports *nothing left* — and an **empty pipe on a BLOCKING
    descriptor cannot report emptiness**: the read that would prove it empty parks instead, so the
    loop never returns and the reader is stuck forever. The child never read another keystroke
    while its PTY filled up. Three independent confirmations: (a) `/proc/<pid>/wchan` = `pipe_read`
    on the live frozen children; (b) `/proc/<pid>/fdinfo/*` `flags=02000000` — no `O_NONBLOCK` on
    either notification read end; (c) a throwaway probe showing `read#0 n=1`, then a 2s block on a
    plain `os.Pipe`.

    **Both** self-pipes have this shape, so the fix also closes a **latent M1 bug**:
    `drainWinchPipe` is the same call with the same loop, i.e. *any* real `SIGWINCH` during a
    session froze the editor identically. It was never observed because no test fires a real
    resize — which is exactly why it survived M1.

    **The second, deeper trap.** The first fix attempt — `unix.SetNonblock(fd, true)` at pipe
    creation — **did not work**: `F_GETFL` came back `0` immediately afterwards, because Go's
    `(*os.File).Fd()` forces the descriptor back into **BLOCKING** mode, so every later
    `unix.Read(int(f.Fd()), …)` silently undid the flag. Fix: capture the raw descriptors **once**
    at creation and use `int` everywhere — `setNotificationPipe(r, w)` returns `(readFD, writeFD)`;
    `Terminal` gained `winchPipeReadFD` / `winchPipeWriteFD` / `wakeReadFD`; `wakePipe` carries
    `readFD` / `writeFD`; `drainPipe` takes an `int`; the SIGWINCH goroutine and `signal()` write
    through `unix.Write` to the raw fd; the poll set uses the stored descriptors.

    Regression tests (`internal/lineedit/notify_drain_test.go`, all three red before the fix):
    `TestDrainPipeNeverBlocks` (empty pipe / one notification / a burst — the empty case is the one
    that parks), `TestWakePipeIsNonBlockingOnBothEnds` (asserts `O_NONBLOCK` via `F_GETFL` on the
    kernel flags of *both raw descriptors*, not on a copied constant),
    `TestWakeSignalNeverBlocksWhenThePipeIsFull` (the write end must return `EAGAIN`, never park).

    End-to-end proof: after `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`,
    `go test ./testsuite/ -run TestChatPTY` → **ok 10.0s** (was 60s+60s timeouts).

18. **The `Close()` reasons sat one line above the statements.** The same fix introduced two
    genuinely reviewed discards (`_ = r.Close()` / `_ = w.Close()` in `newWakePipe`'s failure
    path) but wrote the reason on the line **above**, while the `ignored-error` rule reads the
    statement's own line — 2 production hits against a ceiling of 0, i.e. a **new branch
    violation** caught by the master-vs-branch comparison (master: 1 production hit; the site
    lists are now byte-identical). Fix: the reason moved onto each statement.

**Lesson (the one worth carrying forward).** The PTY suite was the *only* layer that could catch
this: a unit test asserting "`notify()` buffers and the poller sees it" was green the whole time,
because the scripted source never touches the real notification pipe. And a claimed test result
must never contradict the captured log sitting next to it.

## Defects found and fixed inside this increment (root cause, not parameter)

Every one of these was reachable in production and none was visible to a
"the callback fired" test. Each has a regression test that fails without the fix.

1. **The live row was overwritten by its own in-flight bursts.** `writeLive` placed the
   cursor at **column 0** on every cadence flush, so a line arriving as the normal
   several-burst stream (`"alpha "`, `"bravo "`) rendered as `"charlie avo"`. Root cause:
   no tracking of the live row's current column. Fix: `liveCol` appends each burst where
   the previous one stopped. Repro: `TestA5B_InFlightBurstsAppendToTheLiveRow` (verified to
   fail with the old `place(..., 0)`).
2. **A finalized line was painted twice.** `drain` wrote the finalized body to the live row
   AND queued the same text for commit, so every line appeared twice on screen (and the
   "exactly once" invariant was false). Root cause: two overlapping paths for the same text.
   Fix: `drain` splits at the last line break — the finalized part goes through
   `commitPrefix` only, the remainder stays live.
3. **A line wider than the terminal was silently truncated.** The in-flight path cut at the
   row's end and dropped the tail; only `commitPrefix` wrapped. Fix: a burst that fills the
   live row is **finalized** (one scroll) and writing continues on the fresh row — the same
   policy as a line break. Repro: `TestA5B_WideInFlightLineIsFinalizedNotDropped`.
4. **`commitScroll` scrolled whatever row the cursor happened to be on.** After a status
   repaint the cursor sits BELOW the scroll region, so a bare newline scrolled the whole
   screen and wiped the editing row and the menu (measured: `"you> typed by the user"` →
   `""` mid-answer). Fix: place the cursor absolutely on the live row before scrolling.
   Repro: the chrome half of `TestChatStream_ReservedLayoutCommitsEachLineOnce`.
5. **The answer renderer never learned the terminal width** (`SetCols` had no production
   caller), so it composed a status line and wrapped rows at the column-80 default — on a
   40-column terminal the status wrapped onto a second row. Fix: `EnableRegion` and the
   WINCH handler publish `region.Cols()` — one source of truth for the geometry.
   Repro: `TestA5B_AnswerWidthFollowsAResize` (drives a real `EventWinch`, verified red with
   the propagation removed).
6. **The status line was cut mid-field** (`"… · 12.3k to"`), which reads as corrupted data.
   Fix: drop trailing fields WHOLE, never truncate a field. Repro:
   `TestA5B_NarrowStatusDropsWholeFields` (asserts no fragment survives).
7. **`setCompleter` self-deadlocked.** It held the mutex and called `cancel()`, which locks
   the same mutex — Go's `sync.Mutex` is not reentrant, so the process **hung** (measured:
   `goroutine 19 [sync.Mutex.Lock, 5 minutes]` and a 300 s test timeout). This is the seam
   every test uses, so it froze the whole `-run TestChatCompletion` run. Fix: split every
   entry point into a `*Locked` body (`cancelLocked`, `publishLocked`) and launch the worker
   outside the lock.
8. **A completer answer could reopen a menu the user had Escaped**, because the worker
   publishes whatever it computed for the line it was asked about. Fix: `cancel()` bumps the
   request generation, so an in-flight result is discarded as stale.
9. **Closing the menu left its rows painted** (the rows are placed absolutely; nothing
   erased them). Fix: track the painted rows and erase them on close.
10. **Menu state changes (selection move, close) never repainted** — the read loop's gate was
    the buffer's `Changed()`, and `Changed()` CLEARS the flag, so a menu-only change was
    invisible. Fix: a `menuDirty` flag.
11. **Dead code that Rule 13 forbids**: `RuneSafeCut` / `trailingPartialEscape` /
    `answerMaxPrefixBytes` were defined and referenced by nothing. Fix: `RuneSafeCut` is now
    a real hold-back (a split multi-byte rune is never written; `answerMaxPrefixBytes` caps
    it) and the unused `trailingPartialEscape` was deleted, its constant with it.
12. Rule 18 violation (an `else` after a terminating `if`) introduced in the ESC path — fixed
    by de-indenting to a guard clause.

### Honest bound of the A5 design (stated, not hidden)

The answer band is the region between the chrome and the status row, so an answer longer
than the band **scrolls the band and its oldest visible rows leave the view**. The
terminal's scrollback is not grown by the answer, because the live row deliberately sits
outside the scroll region (that is what stops a line advance from scrolling the live row
into the band). What the increment buys is what it promised: no repaint of an ever-taller
answer, no fight with the terminal's auto-scroll, and a reserved status row. Turning the
band into a true scrollback-growing log is a further increment, not a claim of this one.

**Round 5 measured WHY the scrollback is not grown, and it is not the reason this section
originally implied.** See "Round 5" below: it is the region's TOP MARGIN, not the live
row, and the loss is not a "bound" of the design — it is a defect against A5's own
acceptance ("finalized lines land in scrollback exactly once").

### Round 5 — a real emulator disproves "committed to scrollback": the region's TOP MARGIN

**How this was found.** Section "Honest bound" sat next to a Completed bullet claiming the
opposite ("committed to the scrollback exactly once"). Both cannot be true, so the claim was
tested against a REAL terminal instead of the branch's own `Screen` model. Xvfb `:99` +
xterm 372 + `xdotool` (pointer-over-window wheel; xterm ignores synthetic keys unless
`allowSendEvents` is set) + a drag-select read back through the PRIMARY selection with
`xclip` — i.e. the emulator's own buffer read as exact text, not a screenshot guess (at small
sizes vision reported 17 vs 18 lines for the same screen, so pixel reads alone were not
trusted for counting).

**The rule, measured.** A scroll region only feeds the terminal's scrollback when its TOP
MARGIN is the screen's **first row**. 60 numbered lines written with an in-region LF, then
wheel to the top and read the oldest line still present:

| geometry (20-row screen) | oldest line still readable | verdict |
|---|---|---|
| region `[1;19]` — top == screen row 1, reserved row BELOW it | `LINE-001` | overflow **saved** |
| region `[2;19]` — top below row 1, chrome ABOVE it | `LINE-044` | lines 1–43 **destroyed** |
| region `[2;20]` — top below row 1 | `LINE-043` | lines 1–42 **destroyed** |
| no region (plain flow) | `LINE-001` | overflow saved |

Raw reads: `.evid/o2-{A,C,D}.txt`, photos `.evid/big-A.png` (topmost `BA-LINE-044`),
`.evid/big-D.png` (topmost `BD-LINE-001`), `.evid/bg2-C.png` (region top = row 1 →
`ZC-LINE-001`), `.evid/bg2-A2.png` (region `[2;20]` → topmost `ZA2-LINE-043`). Scripts:
`.evid/oracle2.sh`, `.evid/big2.sh`.

**The real code, not a synthetic shape.** `cmd/scrollprobe/main.go` (a throwaway probe added
for this measurement) drives the ACTUAL A5 stack — `lineedit.NewChatEditor` +
`EnableRegion()` + `AnswerRenderer` — with 30 `PROBE-LINE-nn …\n` deltas in a real xterm
(`.evid/realprobe.sh`). Scrolled to the top, the oldest readable line is **`PROBE-LINE-14`**:
**13 of the 30 answer lines were destroyed.** Raw readback: `.evid/real-sel.txt`.

**Why A5's own tests could not see it.** `internal/lineedit/answer_test.go` replays the
written bytes into the branch's own `Screen`, and `Screen.scrollRows` models a region scroll
as "shift the band up, blank the last row" with no scrollback at all — so the model and the
code agree with each other and both disagree with the emulator. This is the Rule 10.2 trap in
its exact form: a cell grid is only truth if it models the terminal's rule, and the rule it
needed here is "only rows pushed off the SCREEN's first row enter scrollback".

**Consequence, stated plainly.** The band's region top sits below the editing row
(`computePlan`: `MenuTop = 1`, `bandTop = 1 + chrome`), so every finalized line that scrolls
past the band is discarded. The layout is the INVERSE of the codex geometry this increment
cites (`insert_history.rs` sets `SetScrollRegion(1..area.top())` — region top == screen row
1, composer viewport BELOW it). What the branch does deliver, and what its tests do prove,
is the no-repaint-storm half of A5: a finalized line leaves the live region exactly once and
is never repainted, with a reserved one-line status. The scrollback half is **not delivered**.

**Dispatched as task `01a0aa61-9e26-7031-8b8e-69f3cc2bb168`** ("Fix chat M2 A5: band
scroll-region top must be screen row 1, else finalized lines are destroyed instead of
reaching scrollback"), carrying this evidence, the reproduction harness
(`.evid/realprobe.sh`, `.evid/oracle2.sh`), the required fix (invert the reserved layout:
answer band at the top with region top = screen row 1, chrome below it) and the required test
shape (model the emulator's rule in `Screen` — record rows pushed off the screen's top into a
scrollback buffer — then assert line 1 present, each finalized line exactly once, and the
NEGATIVE case: region top > 1 ⇒ nothing reaches scrollback).

### Round 6 — the fix: invert the layout so the band IS the top of the screen

The round-5 finding had one root cause and one fix. **Root cause**: `computePlan` placed the
editing line at row 0 and the band below it, so the band's region top was `1 + chrome` — an
*interior* margin. A terminal saves a line to scrollback only when that line is pushed off
the **screen's** first row; lines scrolled off an interior region margin are destroyed. That
is a property of the emulator, not a tuning value — no parameter could fix it.

**Fix**: invert the reserved layout. The band occupies the screen's TOP rows, its region top
IS screen row 1, and the chrome sits BELOW it, in this order:

```
row 0 .. BandBottom        the answer band (the scroll region; its LAST row = live row)
next editorRows            the editing line
next menuRows              the completion menu (A7)
row R-1                    the one-line status
```

`editorTopRow` is now a named constant with the invariant written down, because it is the
layout's central property rather than an incidental zero. What follows from the inversion:

- The region top never moves. Only the band's **height** varies (the menu changes the band's
  bottom, one CSI sequence), so a finalized line is always pushed off row 1 — saved — and
  never off an interior margin.
- `Layout` is exported (`EditorTop`, `EditorRows`, `MenuTop`, `MenuRows`, `BandTop`,
  `BandBottom`, `StatusRow`) so callers place things without assuming the editing line is the
  first row; `region.placeInto`/`cursorTo` keep every placement absolute.
- `render.go` places at `reg.EditorTop()` and remembers `placedRow`; `menu.go` places from
  `Plan().MenuTop` and only erases the old rows when the layout has not already done so.
  Without that, the menu's repaint would erase rows the layout just reused.

**The test that was missing, and why.** `Screen` now models the emulator's actual rule: rows
pushed off the screen's first row are appended to a scrollback buffer, rows pushed off an
interior region margin are discarded. That is the model the branch lacked — round 5's `Screen`
shifted the band and blanked the last row with no scrollback at all, so model and code agreed
with each other and both disagreed with xterm. The three round-6 tests:

- `TestA5B_CommittedLineSurvivesInScrollback` — 24 answer lines: every `PROBE-LINE-nn` is
  reachable **exactly once** across scrollback ∪ band, the live row carries only the in-flight
  line, and the chrome is intact. This fails on the pre-fix layout (line 1 destroyed).
- `TestA5B_InteriorRegionTopDiscardsLines` — the **negative control**: a region whose top is
  row 2 loses lines. Without this, the positive test could pass for the wrong reason (a model
  that keeps everything regardless of position).
- `TestA5B_RepaintCountIsBoundedByTheAnswerShape` — the repaint count stays linear in answer
  LINES, not in deltas.

**Verified on the real emulator, not only in the model** (Xvfb :99, xterm 372, wheel-to-top,
drag-select readback via PRIMARY): the same probe against the pre-fix revision reports
`PROBE-LINE-01 present: 0` / topmost `PROBE-LINE-14`; against the fixed revision
`PROBE-LINE-01 present: 1` / topmost `PROBE-LINE-01`. A/B, one source, two builds.

### Round 7 — the harness was corrupting the very screen it quoted

Round 6's fix was right, but its quoted evidence was not: the `Raw rendered screen` block
showed the answer **twice**, and it still showed the PRE-inversion layout (editing row 0,
band below). Chasing that printout found a defect in the TEST HARNESS, not in the layout.

**Root cause.** `recordingWriter` keeps every write the editor emits, and the harness's
`replay()` fed **the entire history** into a `lineedit.Screen` that is a **stateful cell
grid**. The recorded stream moves the cursor **relatively** (never an absolute home), so
feeding the same bytes a second time does not reproduce the screen — it REPAINTS it: the
answer lands again below itself. And `replay()` was called far more than once per test, as a
SIDE EFFECT of other helpers (`waitFor` replays on every poll; `layout()` replays before
reading the plan). The consequence is the one that matters for a report: the assertions at
`chat_stream_test.go:196-203` ran against a **correctly** painted screen (the band showed each
line exactly once, so they passed), and then the test's final `t.Logf` re-replayed and quoted
a **doubled** screen as its evidence. A1/A7's `completionHarness` had the identical `replay()`
(poll-driven, re-feeding the whole history), so the same corruption was one `Contents()` call
away from being quoted there too.

**Fix.** The fed-count lives on `recordingWriter` (`ReplayInto`), so `replay()` is
**idempotent**: it feeds exactly the writes the screen has not seen. Both harnesses delegate
to it. The screen a test dumps is now the screen its assertions ran on — the printout above
is that screen.

**The same defect class in `internal/lineedit`, caught by the same question.** Repeating the
check there found the identical full-replay in `answer_test.go`'s `a5Harness`: on a 24-line
answer a second full replay left the SCREEN text identical but the **scrollback at 48 entries
instead of 24**, with `PROBE-LINE-01` present twice. That is the exact quantity A5 asserts on
("exactly once across scrollback ∪ band"), so the harness could have agreed with a duplicated
paint. No current test replayed a scrollback-bearing screen twice (the three tests that replay
twice assert only chrome/status convergence, which does converge), so this was LATENT rather
than a live false pass — stated plainly rather than dressed up as a finding. It is fixed the
same way (`chunks[h.fed:]`, plus `resizeScreen` which resets the counter because a new grid
must be fed the whole history), and `menu_test.go`'s `a7Harness` was already incremental for
its own reason (poll cost), so the three harnesses now share one rule. The removed
`replayDroppingLast` helper had no callers and would have been wrong under incremental
semantics (its "drop the last write" meant "drop a write already fed").

**The regression guard** (Rule 10.5): `TestRecordingWriterReplayIsIdempotent` writes a
relative-motion stream, replays it once, then replays it three more times and fails if the
counter reports a re-feed or the screen moves. **Red-proof**: against the pre-fix naive
`ReplayInto` (re-feeding every recorded write) it FAILS — `replay 2 fed 1 already-seen
writes, want 0 (the counter is the guard)`.

**The defect reproduced on the very test whose evidence was wrong.** Restoring the naive
`ReplayInto` and re-running `TestChatStream_ReservedLayoutCommitsEachLineOnce` gives the
original paradox in one output: the test **PASSES** — every assertion still holds, because
the poll-driven replays landed a correct screen at the moment each one read it — and the
screen it quotes is wrong:

```
--- PASS: TestChatStream_ReservedLayoutCommitsEachLineOnce (0.00s)
    rendered screen after one 4-line answer (band [0,9], live row 9, ...):
        fourth answer line
        first answer line     <- the answer, painted a SECOND time
        second answer line
        third answer line
        fourth answer line
```

That is the whole failure mode in one screen: a green test and a fabricated-looking
artifact, from the same run. It is also why the defect is a HARNESS defect and not a
cosmetic one — nothing in the suite could have caught the wrong printout, because the
assertions that would have caught it were reading a different screen.

**Why this was worth a round of its own.** The layout was correct and the tests were green;
what was wrong was EVIDENCE — the human-readable artifact this report hands to a reviewer. A
green assertion plus a fabricated-looking printout is exactly the failure mode where a
reader concludes the feature is broken (or, worse, trusts a screen that never existed). The
increment's own habit — quote the raw terminal picture rather than a summary of assertions —
is what exposed it. The quoted header was also corrected while there: it labelled
`bottom+1` as the status row (that row is the EDITING row; the status row is `StatusRow`),
so the picture's own annotation now comes from the layout
(`band [0,9], live row 9, editing row 10, status row 11`) rather than from an assumption
about which row follows the band.

## Not Done (honest)

- **The two-goroutine terminal WRITE is not fixed — it is dispatched.**
  `go test -race ./cmd/agent/ -run 'TestChat|TestLineedit'` still reports races between the
  editor's feeder goroutine (composer/menu writes inside `Editor.Run`) and the REPL loop's
  `StatusTick` (status row writes). Round 7 fixed the LAYOUT-state half (`d67da00b`: the
  region's published plan is mutex-guarded, so `go test -race ./internal/lineedit/` is green
  again); the byte-stream half is a different defect — two writers to one terminal interleave
  between a cursor placement and its text, which no lock on the layout can prevent. The same
  race core reproduces on **master `69975030`**, so it is pre-existing M2 wiring, not a
  regression of this branch. Filed as task
  **`01a0aa9a-84de-7555-b14a-c1f087d82515`** with the measured evidence and the fix direction
  (publish the status change + wake the editor, so terminal writes have exactly one owner —
  the `wakePipe`/`EventWake` mechanism the completion path already uses).
- **No PTY-level test of the A5/A7 SCREEN in `testsuite/`.** The existing
  `testsuite/chat_pty_test.go` (M1, unchanged and green) proves the editor binds to a real
  terminal, renders, and submits. The new screen assertions run against the same code paths
  through `NewScreen` cell-grid replay (deterministic, no PTY timing). A PTY variant that
  drives a real resize during an answer is NOT covered.
- **`testsuite/chat_pty_test.go` gained ONE test in round 4**
  (`TestChatPTY_ResizeDoesNotFreezeTheEditor`, a real `TIOCSWINSZ`-driven SIGWINCH), so the freeze
  class is now covered end-to-end. What is still **not** covered on a real PTY: the rendered A5
  region geometry during a resize (the cell-grid assertions for that are `NewScreen` replays, not
  PTY byte streams) and a resize arriving MID-ANSWER rather than while idle.
- **File completion is a single bounded directory scan** (`completionFileScanMax` entries,
  no recursion). A deep path completes one level at a time; that is the documented scope.
- **The band does not preserve answer text that scrolls out of view** (see the honest bound
  above). A scrollback-growing live log is out of scope for A5.
- **A5's scrollback half: DELIVERED in round 6 (was the round-5 finding).** Round 5 measured
  on a real xterm that the band's scroll-region top margin sat below the screen's first row,
  and a terminal discards every line scrolled off a region whose top is not screen row 1 —
  oldest readable line after a 30-line answer was `PROBE-LINE-14`, 13 lines lost. Round 6
  inverted the layout (band anchored at the screen's first row, chrome BELOW it) and re-ran
  the same probe: **`PROBE-LINE-01` is now readable**, oldest reachable line = `PROBE-LINE-01`
  (raw readback `.evid/newlayout-sel.txt` vs the pre-fix `.evid/oldlayout-sel.txt`). The A5
  acceptance wording "finalized lines land in scrollback exactly once" is **met**.
- **`internal/standards/baseline.go` and `internal/stylegate` ceilings were not touched.**
  Measured master-vs-branch equality is quoted below; the pre-existing failures
  (`errorf-verb 9 > ceiling 8`, `ignored-error 1 production`, the test-scope ceilings) are
  IDENTICAL on master and are therefore not regressions from this branch.

## Verification

Round 6 (the fix for the round-5 finding) on `feat/chat-m2-streaming`:

- Build: `CGO_ENABLED=0 go build ./...` → **OK** (exit 0).
- `go test -count=1 ./internal/lineedit/` → **ok, 2.1s** (whole package).
- `go test -count=1 -run TestA5B -v ./internal/lineedit/` → **17/17 PASS**, including the three
  round-6 additions: `TestA5B_CommittedLineSurvivesInScrollback` (PROBE-LINE-01..24 each
  reachable EXACTLY once across scrollback+band, live row carries only the in-flight line,
  chrome survives), `TestA5B_InteriorRegionTopDiscardsLines` (the negative control — a region
  whose top is row 2 discards, so the test would go red on the pre-fix layout), and
  `TestA5B_RepaintCountIsBoundedByTheAnswerShape`.
- **Real-emulator A/B, same probe binary source, only the production layout differs**
  (Xvfb :99 + xterm 372 + `xdotool` wheel-to-top + drag-select readback via PRIMARY):

```
pre-fix (a06d0bf9, band BELOW the editing row)   PROBE-LINE-01 present: 0   topmost: PROBE-LINE-14
fixed   (round 6, band AT screen row 1)          PROBE-LINE-01 present: 1   topmost: PROBE-LINE-01
```

  Pre-fix readback `.evid/oldlayout-sel.txt` (17 distinct lines, oldest 14 — 13 lost);
  fixed readback `.evid/newlayout-sel.txt` (oldest is line 01). The probe is the production
  stack, not a re-enactment: `lineedit.NewChatEditor` + `EnableRegion()` + `AnswerRenderer`
  (`.evid/scrollprobe_src/main.go`, rebuilt with `CGO_ENABLED=0 go build -o .evid/scrollprobe
  ./.evid/scrollprobe_src`). The pre-fix arm is a throwaway detached worktree
  (`~/workspace/gopi-a5-old` at `a06d0bf9`) carrying the same probe with only the missing
  `Layout()` accessor removed — one source, two builds.
- **Round 7 (the harness-evidence defect)**: `go test ./cmd/agent/ -run
  'TestRecordingWriterReplayIsIdempotent' -count=1 -v` → **PASS**; **red-proof** against the
  pre-fix naive `ReplayInto` (re-feeding every recorded write) → **FAIL** with
  `replay 2 fed 1 already-seen writes, want 0 (the counter is the guard)`. The A5 wiring test
  now prints the same single-painted screen its assertions ran on (quoted above), and
  `go test ./cmd/agent/ -run 'TestChatStream|TestChatCompletion' -count=1` → **ok**.
- **Round 7 (the A/B re-run on the current binaries).** The round-6 A/B was re-measured with
  BOTH arms re-typed after the harness change, so the numbers below are not inherited from the
  earlier round. Same probe source (`.evid/scrollprobe_src/main.go`, committed), one build
  against this tip's `internal/lineedit` and one against the pre-fix worktree's
  (`~/workspace/gopi-a5-old` at `a06d0bf9`, whose only source difference is the missing
  `Layout()` accessor — the probe's plan printout is what that accessor adds):

```
fixed    (this tip,  band at screen row 1)   .evid/r7b-sel.txt   190 bytes, PROBE-LINE-01 present: 1, topmost PROBE-LINE-01
pre-fix  (a06d0bf9, band interior)           .evid/r7old-sel.txt 306 bytes, PROBE-LINE-01 present: 0, topmost PROBE-LINE-14
```

  The fixed readback's own header (`layout editorTop=18 band=[0,11] status=19`, 20 rows) shows
  the band's top IS screen row 0 — the scrollback condition. `git diff f5f16b6d..HEAD -- '*.go'
  ':!*_test.go'` → **empty**, so the production layout the A/B measures is this tip's layout.
  Scripts: `.evid/realprobe.sh` / `.evid/realprobe-old.sh` (the throwaway raw-region control
  `.evid/realprobe-prefix.sh` is kept for the shape table; it is NOT the production layout).
- **Rule 22 sweep of the evidence scripts.** Every `.evid/*.sh` that generated a throwaway
  script under `/tmp` now takes it from the shared scratch tree
  (`${GOPI_TEST_SCRATCH_HOME:-$HOME/workspace/gopi-test-scratch}`), asserted off the `/`
  partition (`/` and `/tmp` are both device `66307` on this host; the workspace is `66309`).
  `bash -n` clean on all of them; the two `demo-*.sh` that are MASTER's tracked files were
  left alone rather than edited on this branch.
- **Round 7 (the layout-state race), commit `d67da00b`.** Re-running the A5 assertions under
  `-race` turned up a real data race on the region's published layout — `active`/`cols`/`plan`
  is written by the editor's read loop and read by the REPL's OWN goroutine
  (`StatusTick` → `StatusRow`/`LiveRow`, cmd/agent/cli_chat.go holds the editor in a feeder
  goroutine while the loop ticks the status). Round 6 is what made it reachable, so
  `go test -race ./internal/lineedit/` went RED on this branch — measured green at
  `a06d0bf9`, the pre-round-6 revision. The same race also fires on master through
  `cmd/agent`, so the defect predates the branch; this branch lands the fix rather than only
  its own share of it.

```
before (this branch, round 6):  go test -race ./internal/lineedit/  → FAIL, WARNING: DATA RACE
                                (region.layout write vs region.EditorTop read)
after  (d67da00b):              go test -race ./internal/lineedit/  → ok  (18/18 TestA5B_* PASS)
A/B: go test ./cmd/agent/ -race -run TestChatStream_ReservedLayoutCommitsEachLineOnce
     master 69975030: the same race core (chatInput.StatusTick / ReadLine / Write paths)
     branch d67da00b: the same shared core — this branch adds NO new race site
```

  The fix is a mutex over the published layout plus a `state()` reader that takes it ONCE
  (a caller cannot pair the editing row of one layout with the status row of another — on a
  20-row screen a menu opening moves the editing row and the band bottom together, so halves
  from two layouts would misplace a row, not merely tear). Guard
  `TestA5B_LayoutStateIsReadableFromAnotherGoroutine` exercises every accessor from a second
  goroutine against a republishing writer; **red-proof**: with the lock neutralized the race
  detector fires on the first run (`WARNING: DATA RACE` + `--- FAIL`).
- **What this round did NOT fix (dispatchable, honestly).** The races that remain in
  `cmd/agent` are a different defect with the same shape: the harness's `recordingWriter` and
  `scriptSource` are touched from the test goroutine and the editor goroutine without a lock,
  and — more importantly — **in production the composer and the status row write to the same
  terminal from two goroutines** (the feeder goroutine inside `Editor.Run`, the REPL loop's
  `StatusTick`). Byte-level interleaving of two `write(2)` streams to one fd is not caught by a
  lock on the layout; it needs the status refresh to be routed THROUGH the editor (the
  notification pipe already exists for exactly this: `Terminal.Notify()` /
  `SetCompletionNotify`). That is a design change to the M2 wiring, not a one-line guard, and
  it is left for its own task.
- **Round 7 (the same class in `internal/lineedit`)**: `go test -count=1 ./internal/lineedit/`
  → **ok** (whole package, including all 17 `TestA5B_*` and the `TestA5B_*`/`TestChatCompletion_*`
  A7 set). Measured before the fix: a second full replay of a 24-line answer left the screen
  text identical but the scrollback at **48** entries (`PROBE-LINE-01` twice); after the fix a
  repeated `replay()` feeds 0 further writes and the scrollback is untouched. Latent, not a
  live false pass — no current test replayed a scrollback-bearing screen twice.
- Round-4 state re-verified unchanged (the sections below are kept as the record): `go test
  ./testsuite/ -run TestChatPTY -count=1 -v` → **3/3 PASS**; PTY resize red-proof against the
  pre-fix binary; `go test ./internal/lineedit/ -race -count=5` → ok; per-TEST master-vs-branch
  gate diff with zero branch-only failures; `testsuite/chat_test.go`, `go.mod`, `go.sum`
  byte-identical to master.

Round 4 (the previous report state) on `feat/chat-m2-streaming` after `87a62936`:

- Build: `go build ./...` → **OK**; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK, and the
  binary's mtime (`1789562577`) is **newer than every source file** (`1789562532`), so the e2e tests
  below exercise this code.
- `go test ./testsuite/ -run TestChatPTY -count=1 -v` → **3/3 PASS** in 20.4s:
  `TestChatPTY_ResizeDoesNotFreezeTheEditor` (7.91s, NEW), `TestChatPTY_EditorRendersAndSubmits`
  (6.36s), `TestChatPTY_PasteBurstIsOneTurn` (2.55s) — the latter two were 64.90s / 61.01s
  **timeouts** before the fix. Log: `.evid/round2-testsuite.log` (before), `.evid/fulltest-final.log`.
- **Red-proof of the new resize test**: with `GOPI_BIN` pointed at the pre-fix binary
  (`~/workspace/gopi-prefix/bin/gopi`, built from `d29f00a1`) it **FAILS** —
  `timed out waiting for "answer after resize."` after 66.39s (the reader parked in `pipe_read`).
  Against the fixed binary it PASSES in 7.91s. Same test, same harness, only the binary differs.
- `go test ./internal/lineedit/ -race -count=5` → **ok** (6.4s, no race).
- `go test ./cmd/agent/ -run 'TestChat|TestLineedit' -count=2` → **ok** (3.0s).
- `go test ./internal/lineedit/ -run 'TestDrainPipeNeverBlocks|TestWakePipeIsNonBlockingOnBothEnds|TestWakeSignalNeverBlocksWhenThePipeIsFull' -v`
  → **3/3 PASS**; all three FAIL against the pre-fix code (the drain hangs on the empty-pipe case).
- Increment test inventory, re-run on the final commit (`.evid/a5a7-list.txt`):
  **A5 = 14** `TestA5B_*` PASS (region install, deltas never repaint committed text, live row carries
  only the in-flight answer, scroll region never moves the chrome, chrome immobile under a long
  answer, answer longer than the band keeps the newest rows, status shows model/iter/tokens, narrow
  status drops whole fields, too-small terminal refuses, region clip keeps the editing line, split
  rune held back, in-flight bursts append, wide in-flight line finalized not dropped, width follows a
  resize); **A7 = 9** `TestChatCompletion_*` PASS (menu opens below the line, arrow moves the selection
  ON SCREEN, Tab completes, Esc closes, slow completer never blocks the line, menu repaints bounded,
  idle waits do not repaint, a closed menu is not repainted by a wake, a slow completer still gets its
  menu); plus `cmd/agent` `TestChatStream` **3/3** and `TestChatCompletion` **2/2** PASS.
- Gates, **master (`6a105a48`) vs branch**, both run as
  `go test ./internal/standards/ ./internal/stylegate/ -count=1 -v`:

```
standards (per-TEST verdicts identical):  ✓ all 8 red-proof gates PASS on both sides
  Rule 16  hits=9  baselined=8 escaped=1 NEW=0
  Rule 17  hits=114 baselined=91 escaped=23 NEW=0
  Rule 18  hits=3  baselined=3 escaped=0 NEW=0
  Rule 19/20/13  hits=1 each, escaped, NEW=0        Rule 21 / 13-14/scratch-env  hits=0 NEW=0

stylegate (SITE LISTS byte-identical after normalisation):
  test-sleep   2   errorf-verb 9 (ceiling 8)   ignored-error 1 production / 201 test
  test-setenv  1   test-skip  1                 unchecked-close 1 test
  → 19 identity lines / 0 differing  (first round: 2 production ignored-error on the branch vs 1 on master — fixed in c5010c2a)
```

  The ONLY failing gate test on either side is `TestGateCwdReleaseFindsTheRealTree`, with the same
  message and the same counts on master and on the branch (a pre-existing host-state gate).
- **Round-7 final gate on the committed tip** (`.evid/r7-final-gate.log`):
  `go test -count=1 ./internal/lineedit/ ./cmd/agent/ ./testsuite/ ./internal/standards/
  ./internal/stylegate/` → `internal/lineedit` **ok**, `testsuite` **ok**; every `cmd/agent` /
  `standards` / `stylegate` failure is present on master too — `comm -13 <master test names>
  <branch test names>` is **empty** (zero branch-only failures), and **no chat test fails
  anywhere in the run**.
- Invariants: `git diff master --stat -- testsuite/chat_test.go` → **empty** (the load-bearing cooked
  path the piped-stdin suite drives is untouched; the PTY addition is in `testsuite/chat_pty_test.go`);
  `git diff master --stat -- go.mod go.sum` → **empty** (zero new dependencies — the PTY resize uses
  `golang.org/x/sys/unix`, already a DIRECT dependency).
- Full suite, **per-TEST diff master vs branch**: 31 packages `ok` on the branch;
  `comm -13 <master fails> <branch fails>` → the 21 branch failures are **all** present on master too
  (daemon cgroup scope, tmpdir/scratch-root config, X11 oracle, worker-ownership pointer, the
  pre-existing `TestGateCwdReleaseFindsTheRealTree` and `TestStyleGate` ceilings). **Zero
  branch-only failures.** Logs: `.evid/fulltest-final.log`, `.evid/fulltest-master.log`.
- Earlier rounds (unchanged, re-verified): `go vet` + `gofmt -l` clean on the touched files.

### Raw rendered screen (A5, quoted from the test's own log)

40×12 terminal, one answer of four finalized lines, `go test ./cmd/agent/ -run
TestChatStream_ReservedLayoutCommitsEachLineOnce -v`. The test's own header line is
`band [0,9], live row 9, editing row 10, status row 11` (12 screen rows) — the band's
top is screen row 0, which is the scrollback condition:

```
<band row 0>          blank
<band rows 1-4>       blank
first answer line     <- band row 5
second answer line
third answer line
fourth answer line    <- band row 8
<band row 9>          blank (the live row, empty because the answer is finished)
you> typed by the user   <- the editing row (band bottom + 1)
glm-5.3-flash · iter 1  ready   <- the status row (the screen's last row)
```

The band is the scroll region [0,9] and the status row is 11; the editing row (10)
keeps the user's text BELOW the band, which is the inversion round 6 made: the
region's top is the screen's first row, so every finalized line is pushed off row
0 and SAVED rather than discarded off an interior margin. The four lines are
committed ONCE into band rows 5-8 and never repainted; the live row (9) is empty
because the answer is finished. The status row shows model and iteration plus the
live hint; the token field ("· 12.3k tokens") does not fit 40 columns together
with the live hint, and the narrow-terminal policy drops it WHOLE rather than
cutting it mid-field (that behaviour is asserted by
`TestA5B_NarrowStatusDropsWholeFields`; the full four-field row is asserted by
`TestA5B_StatusLineShowsModelIterTokens` on a wider terminal).

## Deliverable

Branch **`feat/chat-m2-streaming`** in the shared repo (`~/work/GoPi`); the worktree is
`~/workspace/gopi-chat-m2`. Recovery: `git log --oneline feat/chat-m2-streaming`,
`git show feat/chat-m2-streaming:devdoc/chat-m2-streaming.md`.

Round-7 commits on that branch (the worktree shares the main repo's refs, so they are already
in `~/work/GoPi` — no push needed):

```
22106d96  docs(chat): M2 round 7 — harness evidence (fixed), layout race (fixed), terminal-write race (dispatched)
d67da00b  fix(lineedit): guard the published layout — it is read from the REPL's goroutine
71f0f853  test(chat): make harness replay idempotent — assertions and quoted evidence now read one screen
```

Dispatched follow-up (not part of this branch): task `01a0aa9a-84de-7555-b14a-c1f087d82515` —
route the status-row refresh through the editor so terminal writes have one owner.
(`gopi task show 01a0aa9a-84de-7555-b14a-c1f087d82515`)
