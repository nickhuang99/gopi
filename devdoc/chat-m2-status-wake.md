<!-- doc-class: historical -->
# Task Report: `gopi chat` M2 status row — one terminal writer (publish + wake)

Branch `feat/chat-m2-status-wake`, worktree `~/workspace/gopi-chat-m2-status`,
off master `69975030`. Supersedes the status-refresh half of
`devdoc/chat-m2-streaming.md` (that branch is NOT extended — Rule 23).

## The defect (root cause, not a symptom)

Two independent goroutines wrote absolute-cursor byte streams to ONE terminal fd:

1. **The REPL goroutine** — the turn loop refreshed the status row directly:
   `cli_chat.go` set `status.live`/`status.iter`/`status.tokens` and called
   `input.StatusTick()` → `Editor.StatusTick` → `AnswerRenderer.drawStatus`,
   which emitted its own cursor placement + erase + text **from that goroutine**.
2. **The editor's read loop / feeder** — the same fd was being repainted for the
   editing line, the completion menu and the answer band.

The bytes are not *corrupted* (`write(2)` is atomic enough for these sizes) —
the **order is**: the caller's cursor placement lands between the read loop's
placement and the text that placement was written for. That is why the previous
`-race` runs could not see it: it is an interleaving defect, not a data race.
The correct instrument is an assertion about **who writes and how much**, and
that is what the new guard tests are.

## The fix — publish, and exactly one writer

`StatusTick` (draw from the caller) is gone. A refresh is now a **publish**: a
sticky dirty flag plus a wake, never terminal I/O. The read loop — the only
goroutine that ever writes to the fd — draws it.

### `internal/lineedit`

- **`notify.go` (new)** — the wake pipe. A `wakePipe` is a non-blocking
  (`unix.SetNonblock`) 1-byte pipe: `Notify()` writes one byte (`wakePipeBytes`,
  a full pipe already carries the same information, so the write is dropped
  rather than blocked), the read loop's `poll(2)` wakes on the read end, and a
  drain-until-empty (`EAGAIN` terminates) leaves it clean. The fds are captured
  once at creation because `(*os.File).Fd()` **forces the descriptor back into
  blocking mode** — re-deriving the fd in the drain would silently undo the flag
  it depends on. `Notifier` is the seam; `noNotify{}` is the no-op sink.
- **`raw.go`** — `EventWake` (a new `EventKind`) and `Terminal.WakeNotifier()`;
  the wake-pipe read fd is polled alongside stdin and the WINCH pipe.
- **`answer.go`** — `AnswerRenderer.PublishStatus()` sets `statusDirty`
  (`atomic.Bool`) and calls the notifier; **no I/O**. `Tick()` (now private to the
  read loop) consumes the flag and calls `drawStatus`, which dedupes against
  `drawnStatus statusKey{row, cols, text}` — an unchanged status costs **zero
  writes**. The dirty flag is *sticky until drawn*, so a publish can be seen
  late but never lost.
- **`editor.go`** — `PublishStatus`, `statusPending`/`menuUnread` (both *peek*:
  the repaint consumes the flag, so consuming in both places would drop the
  draw), `SetNotification`, `NotifyEnabled`, `Source`, `SetStatusLayout`.
  `Run`'s `EventWake` branch repaints when a publish is pending.
- **`menu.go`** — the same seam for the completion worker (see "second defect"
  below).

### `cmd/agent` (M2 wiring)

- **`cli_chat.go`** — all three `StatusTick` sites became `publishChatStatus`,
  the one status-update path (`s.update(...)` is atomic inside the source, then
  `input.PublishStatus()`). The **in-flight** publish deliberately happens
  *after* `ReadLine` returns — i.e. after the editor's feeder parked on the next
  read — so the wake always finds a **blocked** reader rather than racing one
  into the park.
- **`chat_stream.go`** — `chatStatusSource` is mutex-guarded (`update` /
  `setLive` / `status` all take it), and `status()` composes the line **inside**
  the lock, so no read ever sees half an update (no torn field). `modelShort`
  and `iterLabel` became pure functions over the snapshot.
- **`lineedit_wire.go`** — `StatusTick` → `PublishStatus`; `SetStatus` installs
  the wake seam supplied by the **source** when the source is a
  `lineedit.Notifier` (the terminal is; a scripted test source is not); the
  REPL announces the degradation on stderr when no wake channel exists — the
  status row then refreshes on the next keystroke (Rule 15, no silent failure).

## Second defect found and fixed inside this increment

The **completion worker** had the same shape: it published candidates on its own
goroutine, but the read loop only discovered them via a **bounded poll**
(`completionPollMS`). Under `-race` the loop could park with the candidates
still unread — reproducibly `TestChatCompletion_MenuOpensBelowTheLine` /
`ArrowMovesTheSelectionOnScreen` timeouts (on HEAD *and* on the pre-existing
branch). Fixed by the same seam: `completionState` gained `notify` +
`unread atomic.Bool` (set by the worker at publish, cleared by
`Editor.repaint` via `noteDrawn`), the worker `Notify()`s after publishing, and
`Run`'s `EventWake` branch repaints on `statusPending() || menuUnread()`.
Result: 60/60 `-race -count=15` runs green (4 failing runs before).

## Regression guards (`internal/lineedit/status_publish_test.go`, new)

Written against **who writes and how much**, never "a write happened":

- `TestStatusPublishWritesNothingFromThePublisher` — the defect's direct guard.
  The read loop is stopped first, so the only goroutine that *could* write is
  the publisher: 50 publishes must produce **0** recorded chunks. Red-proofed
  against the old design.
- `TestStatusPublishIsDrawnByTheReadLoop` — the other half: the loop *does* draw
  what a publisher raised, with no keystroke. Without it the fix would be
  "nothing writes" and the row would go stale during a turn.
- `TestStatusPublishDoesNotRepaintUnchangedValues` — bounds: a storm of 50
  publishes of an *unchanged* value plus 1 change = **exactly 1** draw (the turn
  loop publishes at every turn boundary; an unbounded repaint would write on
  every one of them).
- `TestStatusPublishSurvivesAConcurrentRepaint` — the two-goroutine shape: every
  recorded chunk must *begin* a sequence (a cursor placement, an erase-to-EOL, a
  scroll newline) — a draw split across two chunks, or spliced into a keystroke
  repaint, shows up as a fragment.
- Drain proof in every wait: a final **changed** publish seen on the row means
  every earlier one was consumed (flags are raised in order and sticky).

## Verification

- Build: `CGO_ENABLED=0 go build ./...` — **OK**; `go vet ./internal/lineedit/ ./cmd/agent/` — **OK**.
- `go test -race -count=1 ./internal/lineedit/` — **ok** (0 data-race warnings).
  On master the same command reports **5 pre-existing races**
  (`TestA5B_AnswerWidthFollowsAResize` + 4 `TestChatCompletion_*`) — a *harness*
  race (the test read region state before `Run()` returned and closed the editor
  before stopping the source), fixed here by waiting for `Run()` to return and
  stopping the source before `editor.Close()`.
- A5B suite (12/12) + `TestChatCompletion_*` (6/6) — **PASS**.
- The four new guards (`-run TestStatusPublish`) — **4/4 PASS**.
- `go test -race -count=10 -run 'Chat|Lineedit|Status' ./cmd/agent/` — **ok
  167.7s** (10 repetitions, zero race reports) — the REPL's own wiring seam.
- `GOPI_BIN=bin/gopi go test -count=1 -run TestChat ./testsuite/` — **ok 10.2s**
  (the e2e chat suite, unchanged file).
- `-race -count=15` over the menu/status tests — **60/60 green** (4 failing runs before).
- Protected files: `testsuite/chat_test.go`, `go.mod`, `go.sum` — **byte-identical**
  to master (`git hash-object` vs `git rev-parse master:<file>`).
- `gofmt`: the two files introduced/changed by this increment are clean; the four
  remaining gofmt-unclean `cmd/agent` files are pre-existing on master.

## Files

| file | change |
|---|---|
| `internal/lineedit/notify.go` | new — wake pipe, `Notifier` seam |
| `internal/lineedit/raw.go` | `EventWake`, `Terminal.WakeNotifier()` |
| `internal/lineedit/answer.go` | `PublishStatus` (flag + wake, no I/O), draw-once dedupe |
| `internal/lineedit/editor.go` | `PublishStatus`/`statusPending`/`SetNotification`/`NotifyEnabled`/`Source` |
| `internal/lineedit/menu.go` | completion worker publishes + wakes (same seam) |
| `internal/lineedit/status_publish_test.go` | new — the four single-writer guards |
| `cmd/agent/cli_chat.go` | three tick sites → `publishChatStatus`; in-flight publish after `ReadLine` |
| `cmd/agent/chat_stream.go` | mutex-guarded `chatStatusSource`, composed in-lock |
| `cmd/agent/lineedit_wire.go` | `PublishStatus`, `StatusNotifyEnabled`, seam from the source |

## Not Done (honest)

- **No end-to-end proof on a real TTY (live-terminal session capture).** The
  single-writer contract is pinned at the `internal/lineedit` seam
  (recorded-chunk assertions), exercised by the REPL's own wiring tests
  (`-race -count=10`) and by the e2e `testsuite` chat suite (which drives a real
  PTY); an interactive session-scoped screen capture was not run.
- **`testsuite/chat_test.go` was not extended** — it is a protected file for this
  task (byte-identical to master required), so the e2e half of the evidence is
  the package-level suites above.
- The `cmd/agent` `chat_stream.go` degradation warning is announced at startup
  only; if a later input mode switches away from the wake-capable source the
  announcement is not repeated.

## Commit

Tip of branch `feat/chat-m2-status-wake` (worktree `~/workspace/gopi-chat-m2-status`),
off master `69975030` — the commit whose subject is
`fix(chat): one terminal writer — status refresh is a publish, not a draw`.
The commit before it, `ff9927a0`, is the PA salvage of the earlier, interrupted
run. Recover the deliverable with:
`git -C ~/work/GoPi show feat/chat-m2-status-wake:devdoc/chat-m2-status-wake.md`.
