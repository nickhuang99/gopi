<!-- doc-class: normative -->
# The small-height chat flake: the draw overtook queued input

Task: `bug(master): TestChatEditingLine_SurvivesALargeToolOutputAtSmallHeights
flakes ~1 in 3 on pure master — small-height chat layout is off by a row`.
Branch: `fix/chat-small-height-wait-from-master` (worktree
`/home/user/workspace/gopi-chat-height-fix`, based on master `2fdc65e4`).

## Completed

### The briefing's hypothesis was wrong, and the measurement said so

The briefing hypothesised a layout off-by-one ("the editor row is laid out one
row short / the typed text is truncated by one character"). It is not: **the
layout arithmetic is self-consistent at every swept height**. In the failing runs
every numeric layout assertion in the same test passed (`EditorTop+EditorRows ==
StatusRow`, `StatusRow == rows-1`, `BandBottom < EditorTop`, band floor), and the
`t.Logf` dump the failure prints shows a consistent layout
(`height=4 layout={EditorTop:2 EditorRows:1 … StatusRow:3}`).

The rendered screen really did read `you> hel` where the test wanted
`you> hello` — but a probe (throwaway, deleted before the commit) showed the
editor's *buffer* was `"hel"` at that instant too: **buffer and screen agreed**,
and 2.2ms later the same screen read `you> hello`. Nothing was mis-laid; nothing
was truncated. The test read the screen **before the read loop had finished
consuming the input it had been given**.

Three failure shapes were observed, which is what a *timing* defect looks like
and a layout defect does not:

| shape | evidence |
|---|---|
| editing row short by N chars | `editing row 2 = "you> hel"`, `editing row 4 = "you> hell"`, `rows=4: … = "you> he"` |
| status row not yet drawn | `status row = "… · iter 0  ready", want the counters` |
| wait primitive gave up | `timed out waiting for 3 status draws (got 2)` |

Master baseline, measured in a detached worktree of pure master (`2fdc65e4`,
zero local changes):

```
fails=8/20   (the briefing measured 2/6 on f90db8d6 — same defect, load-dependent rate)
```

### Root cause

Two halves, one mechanism — **a drawn frame is not evidence that queued input
has been read**:

1. **The waiting.** The harness's wait primitive `waitDraws` waited for
   `AnswerRenderer.StatusPublishes()`, a counter that `Tick()` increments for
   *whatever draw the read loop services* — it is not a statement about the
   keystrokes sitting unread in the harness source. The test's own comment
   defended the choice ("waiting on the screen would replay the write log
   twice"), but `ReplayInto` is idempotent (it feeds only writes it has not fed),
   so the stated reason for avoiding a screen wait was false. The wait was
   therefore satisfied by an *earlier* draw, or by one taken while the loop had
   not read the keys — and the assertion ran against a half-produced screen.

2. **The loop really can draw ahead of queued input.** The harness source serves
   a keystroke and a publish wake from ONE channel (exactly as `Terminal` polls
   stdin and the wake pipe together), so a single `EventWake` can report both.
   `Terminal.ReadEvent` tested the **wake-pipe branch before stdin**, so it
   returned `EventWake` with a key already buffered; the loop's `EventWake` arm
   then repainted (drawing the publish) *without reading that key*. The editor's
   repaint gate was `statusPending || menuUnread || menuDirty || menuUndrawn`
   with no question asked about input.

### The fix

- **`internal/lineedit/raw.go`** — `ReadEvent` reports **input before the wake**
  when one poll sees both. The wake pipe is level-triggered and is *not* drained
  on that path, so the publish's byte keeps `POLLIN` set and is reported as an
  `EventWake` on the next round: **late, never lost**. `Terminal` also gains
  `InputPending()` (a zero-timeout `poll(2)` on stdin's fd, captured once at
  `Open` for the same reason the pipe descriptors are — `(*os.File).Fd()`
  undoes non-blocking mode).
- **`internal/lineedit/editor.go`** — the read loop consults an **optional**
  source seam (`inputBacklog.InputPending`) before it draws a wake's pending
  work: input already waiting is read FIRST, leaving the publish's sticky flag
  raised. A source that does not implement the seam keeps the historic order.
- **`internal/lineedit/answer.go`** — `Begin`/`Finish` **publish** the status
  instead of drawing it. Both run on the turn's own goroutine while the read loop
  draws, which is precisely the two-writer pattern `Tick`'s own doc comment
  forbids ("never by a caller from its own goroutine"); it surfaced under `-race`
  as `Begin → drawStatus` vs `Tick → drawStatus` sharing `drawnStatus` and the
  status row. The `writes` counter became atomic for the same reason.
- **`internal/lineedit/menu.go`** — `completionState.setNotification` publishes
  `notify` **under the lock**, like the func-shaped `setNotifier` beside it; the
  bare assignment raced `notifyLocked` from the read loop once `chatInput.SetStatus`
  re-installed the notifier after the editor started (measured under `-race`).
- **Tests** — `waitDraws` is **retired** (deleted, with a note saying why): every
  one of its sites now waits on its assertion's own subject. The harness source
  gained `InputPending`/`consumed`, `testClock` became mutex-protected (it is
  advanced by the read loop and read by the test goroutine), and both harnesses
  wait for their read loop to return before `Close` (otherwise `region.Disable`
  races the loop's `repaint`).

### Regression guard (Rule 10.5)

`TestChatPublishDoesNotOvertakeQueuedInput` (cmd/agent/lineedit_height_test.go).
It is **deterministic, not a sampled race**: the read loop is parked on a gate
while a publish is raised and the keys are queued behind it, the source then
reports the *ambiguous* single wake (publish ahead of queued input), and the
guard reads the ORDER off the write stream — the index of the chunk that drew the
status vs the write count at the instant the loop first read input.

**Red-proof (measured):** with the ordering gate disabled (one-line `inputPending
→ false`), the guard fails **3/3**:

```
lineedit_height_test.go:485: rows=4: the status draw overtook queued input — the
2-th recorded chunk drew the status, and the read loop only read its first
keystroke after 3 chunk(s) had been written
```

with the gate enabled it passes. That is a real red-then-green transition, not a
guard that merely agrees with the code.

## Not Done

1. **The answer's BYTES are still emitted from two goroutines.** Making `Begin`/
   `Finish` publish removed the *drawn-state* race, and making `writes` atomic
   removed the counter race, but the turn goroutine's `Delta` path
   (`writeLive`/`commitPrefix` → `write`) and the read loop's `drawStatus` still
   emit **byte sequences** to one fd without frame coordination, so a cursor
   placement and the text it was for can interleave. This is the same two-writer
   family the `PublishStatus` path removes for the status *source*, and closing
   it needs the answer's writes run through the read loop — its own task, not a
   line in this one. It is recorded honestly on `Writes()`'s doc comment, where
   the next reader will find it. (Note: it is not what made the assertion flake —
   that was the waiting — but it becomes reachable now that the test survives to
   the end of the stream.)
2. **The whole `cmd/agent` suite was not run to completion.** It is also a
   documented hazard: that package's worker/merge E2E tests reap worktrees of the
   repo they stand in, and running it from a task worktree deletes that worktree
   mid-suite (recorded in the sibling branch's `devdoc/chat-quit-race.md`). The
   branch-scoped smoke that WAS run —
   `make test-budget PKGS='./internal/lineedit ./cmd/agent'` — reports
   `internal/lineedit` **pass** (17.3s, zero failures) and `cmd/agent` FAIL with
   exactly five named tests, all of them **pre-existing on master and unrelated
   to this change** (re-measured here on a detached worktree of pure master
   `2fdc65e4`, same five, same 0.00s each):
   `TestCompletionCandidateSetsCoverTopLevel`,
   `TestTaskListReadPathE2E`,
   `TestSweepZombiesCLI_DryRunTouchesNoNotifySink`,
   `TestSweepZombiesCLI_ReportNotifiesOperator`,
   `TestTopLevelHelpCoversEveryDispatchedSubcommand`.
   The budget did **not** trip (`budget=25m elapsed=498s outcome=fail`); the
   FAIL is the five assertions, not a timeout.
3. **Two sibling flakes were repaired in passing, not investigated as tasks** —
   `TestStatusPublishSurvivesAConcurrentRepaint` (flaky on master too: 1 of 3
   full-package runs; MEASURED AGAIN here as master's own failure in an 8-run
   `-race` loop of `./internal/lineedit/` — see Evidence — and now waits on its
   assertion's subject like the rest; 0 failures in the branch's 8-run loop).
4. **A third sibling flaked ONCE inside the branch's 8-run `-race` package loop
   and is NOT caused by this change**: `TestChatCompletion_EscapeClosesTheMenu`
   (menu_test.go:521, 2.00s timeout, `DIAG seq=2 fin=2 rev=2 drawn=1 wakes=0
   open=true ncand=3 lastTimeouts=[-1 -1 -1]` — the menu WAS published and open,
   the read loop never drew that generation, and it was parked in a BLOCKING read).
   Evidence that it is not mine:
   - **30 targeted `-race` runs of that one test on this branch: 0 failures**
     (`targeted-branch.txt` → `BRANCH ok=30 bad=0`);
   - **the SAME targeted 30-run loop on pure master `2fdc65e4` failed once**
     (`targeted-master.txt` → `MASTER ok=29 bad=1`, run 9) — so the test is
     independently flaky on the baseline, and the branch's single package-loop
     failure is the same pre-existing flake, not a regression;
   - the harness that test uses (`scriptedSource`, internal/lineedit/scripted_test.go)
     does **not** implement `InputPending`, so this branch's ordering gate is
     inert for it (`inputPending()` returns false, no `continue`), and its
     `ReadEvent` is a different type from the one this change reordered.
   It shares the FAMILY (a publish the loop did not draw) but not the mechanism
   this change fixes, and it reproduces about 1 run in 30–60 — filed as its own
   bug task **`01a0c1a2-4306-7929-a8f8-64caed064f7d`** with the DIAG reading and
   a red-proof acceptance.

## Verification

- Build: `go build ./...` → OK; `go vet ./internal/lineedit/ ./cmd/agent/` → OK;
  `gofmt -l internal/lineedit/ cmd/agent/` → only the three files that are
  already unformatted on master and untouched by this branch
  (`cli_config.go`, `serve_lock_replacement_test.go`,
  `worker_stop_forward_regression_test.go`).
- Tests (acceptance, 20 runs, exact transcript):
  `for i in $(seq 1 20); do go test -count=1 -run
  TestChatEditingLine_SurvivesALargeToolOutputAtSmallHeights ./cmd/agent/ ||
  echo FAIL $i; done` → **20 × `ok`, zero FAIL** — the transcript is inlined
  rather than left at a path that will not outlive the run:

  ```
  run 1: ok   … run 20: ok      (20 lines, no FAIL line)
  ```

  Master, same command shape: **8 FAIL in 20**.
- Tests (whole Chat set, 12 runs): `go test -count=1 -run 'TestChat'
  ./cmd/agent/` → **0 FAIL / 12** (master: 2 FAIL / 10 on the
  `TestChatEditingLine|TestChatStatus` subset).
- Tests (branch-scoped smoke, the merge path's command):
  `make test-budget PKGS='./internal/lineedit ./cmd/agent'` →
  `internal/lineedit` **pass** (17.299s, no failing test); `cmd/agent` FAIL with
  exactly the five pre-existing tests listed in "Not Done" §2, each 0.00s, all
  reproduced on a detached worktree of pure master `2fdc65e4`. The budget did not
  trip: `budget=25m elapsed=498s outcome=fail`.
- Tests (`-race` repeated): `go test -race -count=1 ./internal/lineedit/` →
  **ok × 6** (81.4s / 78.3s / 74.9s / 91.2s / 89.5s / 76.8s), zero FAIL lines and
  zero `WARNING: DATA RACE`.
- Tests (`-race`, Chat set on `cmd/agent`):
  `go test -race -count=1 -run 'TestChat' ./cmd/agent/` → **0 FAIL / 3**, and the
  target test `-race` → ok (master: 4 races, 3 of them the production
  `Begin`→`drawStatus` pair).
- Regression guard: green 20/20 (`-run TestChatPublishDoesNotOvertakeQueuedInput`),
  and red 3/3 with the gate disabled.
- Commit: `65d7e278` fix; `f2a125fc` devdoc; `527bb2a2` devdoc transcript inline.

## Evidence (self-filed)

- Acceptance loop, 20 runs, zero FAIL — command and per-run output:
  `for i in $(seq 1 20); do … || echo FAIL $i; done` →
  `run 1: ok … run 20: ok`, `FAIL` count 0.
- Master baseline — `-count=1` in a detached worktree at pure master, output
  `FAIL 6 / FAIL 8 / …` → `fails=8/20`, with the shapes
  `editing row 2 = "you> hel"` and `status row = "… iter 0  ready"`.
- `-race` before: `grep -c 'WARNING: DATA RACE'` = **4**, of which three are
  `AnswerRenderer.drawStatus` ← `Tick` vs ← `Begin` (production).
- `-race` after: `go test -race -count=1 -run 'TestChat' ./cmd/agent/` → 0 races,
  and `go test -race -count=1 ./internal/lineedit/` → ok on **6 of 6** repeat runs
  (the longest-captured output is
  `/home/user/workspace/gopi-test-scratch/queue/queue-runtime-job-3198258/gopi-job-3578295801.out`).
- Branch-scoped smoke: `make test-budget PKGS='./internal/lineedit ./cmd/agent'`
  → `pkg=./internal/lineedit ./cmd/agent budget=25m elapsed=498s outcome=fail`,
  the fail being the five `cmd/agent` tests above, all 0.00s, all reproduced on
  pure master `2fdc65e4` in a detached worktree; `internal/lineedit` pass
  (`Elapsed:17.299`, package `Action:pass`).
- `-race` package loop, BRANCH (8 runs of `./internal/lineedit/`, full output at
  `<session-dir>/race-lineedit-loop.txt`): **7 ok, 1 FAIL**; the one FAIL is
  `TestChatCompletion_EscapeClosesTheMenu` (2.00s timeout, DIAG in "Not Done" §4).
  That test then passed **30 of 30** in a targeted `-race` loop on the branch
  (`<session-dir>/targeted-branch.txt`: `BRANCH ok=30 bad=0`) — and the SAME
  targeted 30-run loop on pure master failed once
  (`<session-dir>/targeted-master.txt`: `MASTER ok=29 bad=1`, run 9), which is
  what establishes the flake as pre-existing rather than a regression.
- `-race` package loop, MASTER (8 runs, detached worktree at `2fdc65e4`, full
  output at `<session-dir>/race-lineedit-master-baseline.txt`): **7 ok, 1 FAIL**,
  the one FAIL being `TestStatusPublishSurvivesAConcurrentRepaint` — i.e. master
  has its own failure of that loop, and it is a different test from the branch's
  single failure. `TestChatCompletion_EscapeClosesTheMenu` did not appear in
  master's loop (8 runs is not enough to bound a rare flake; stated as observed,
  not as absence).
- Red-proof: with `inputPending() { return false }`, the guard printed
  `lineedit_height_test.go:485: … the status draw overtook queued input` on 3 of
  3 runs; restored, 3 of 3 passed.
- The probe that established "buffer and screen agree" is deleted; its reading
  (`served=3 (of 5) scripted keystrokes, buffer "hel", screen "you> hel"`, then
  `"you> hello"` 2.2ms later) is quoted here as the measurement it was.
