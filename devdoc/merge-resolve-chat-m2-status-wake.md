<!-- doc-class: normative -->
# Merge resolution: feat/chat-m2-status-wake → master

Park tag: `archive/feat/chat-m2-status-wake` → branch tip `d8596127` (*"fix(chat):
one terminal writer — status refresh is a publish, not a draw"*).
Resolution commit: two-parent merge, `master` FIRST (`9684e479`), second parent
`d8596127`.

## Why the branch was parked, and what the shape of the fix is

`d8596127` fixes a **two-writer defect on one terminal fd**: the status row was
being refreshed by the CALLER's own goroutine (`StatusTick` → `AnswerRenderer.drawStatus`,
an absolute cursor placement + text) while the editor's read loop was mid-repaint
on the same fd. The bytes were never corrupted — the ORDER was, so no lock could
see it. The fix makes the status a **PUBLISH**: the caller raises a flag and wakes
the read loop (`internal/lineedit/notify.go` — `Notifier`, `wakePipe`, `wakeSink`,
`noNotify`/`funcNotifier`), and exactly one goroutine (the read loop) writes.

master had meanwhile grown a *different* publish/redraw bookkeeping spiral
(`menuDirty` / `menuUndrawn` / `menuRev` / `clampedMenu` / `paintMenu` /
`menuUnread`, A5 top-band layout, `Repaints()`), so both sides touched the same
files with **different mechanisms for the same concern**.

**Resolution rule applied to every hunk: UNION — keep both mechanisms, dedupe the
overlapping implementation, never a blind side-pick.** Where the two sides held
the same concept twice (two wake pipes, two gear fields, two notify seams) the
branch's single-writer seam won and master's bookkeeping was kept on top of it.

## The conflict inventory and the decision taken per file

Base `69975030`, branch tip `d8596127`, master tip `9684e479`.

| file | conflict | decision |
|---|---|---|
| `internal/lineedit/editor.go` | struct fields + the whole read-loop body | master's `menuDirty/menuUndrawn/menuRev/clampedMenu/paintMenu/menuUnread` bookkeeping kept; branch's `PublishStatus`/`NotifyEnabled`/`WakeNotifier` seam adopted as the single wake path; `SetCompletionNotify` rewritten to REUSE an already-installed real `Notifier` instead of overwriting the seam |
| `internal/lineedit/raw.go` | two `wakePipe` implementations | branch's `wakePipe`/`wakeSink`/`winchSig` shape kept; master's duplicate `setNotificationPipe` REMOVED (one definition, in `notify.go`); `wakeR`/`wakeReadFD`/`wakeNotify` gone, `Terminal` carries `wake` + `wakeSink` only |
| `internal/lineedit/event.go` | whole file | **deleted** — it declared a second `wakePipe`/`newWakePipe`/`signal`/`close`, i.e. the branch's `notify.go` supersedes it. This is the one deliberate deletion vs master |
| `internal/lineedit/menu.go` | a duplicate `notify func()` field next to `Notifier` | the duplicate field removed; `setNotifier` wraps a plain func in `funcNotifier`; `notifyLocked` calls `c.notify.Notify()` |
| `internal/lineedit/answer.go` | status draw path | master's `drawStatus` call sites kept, the draw now invoked BY the read loop on a publish (never by the publisher) |
| `internal/lineedit/scripted_test.go` | `notifyCh` (master) vs `notify` (branch) | **both kept**: `notifyCh` is the real pipe's channel (set by `setNotifyPipe`, watched by one long-lived goroutine), `notify` is the `Notifier` seam stand-in. They are different things and the tests use both |
| `cmd/agent/lineedit_wire_test.go` | `recordingWriter` | master's `fed` counter + `ReplayInto` REPLAY UNIFICATION kept; branch's `mu`/`snapshot()`/`count()` adopted. `ReplayInto` now runs under `mu` |
| `internal/lineedit/{answer,menu}_test.go`, `cmd/agent/{chat_stream,chat_completion}_test.go` | replay strategy | master's INCREMENTAL replay (`fed` counter) kept; the branch's "rebuild a fresh screen" alternative was DROPPED — the `fed` counter answers the branch's own objection (a read loop writing concurrently cannot append an already-seen write twice) and avoids the quadratic re-feed that made the polls consume their own deadline under `-race`. Screen geometry (`screenCols`/`screenRows`) kept where a rebuild is genuinely needed (`replayDroppingLast`, the resize tests) |
| `cmd/agent/{chat_stream.go,cli_chat.go}` | publish wiring | branch's publish call sites kept; master's call sites preserved |

**Bug found and fixed in the branch's own new test**
(`internal/lineedit/status_publish_test.go`): `TestStatusPublishSurvivesAConcurrentRepaint`
asserted the storm's keystrokes on screen **row 0**. In the A5 layout the answer
band is anchored at row 0 (the scrollback condition), so row 0 is the BAND, not
the editing line — the assertion failed on the correctly-merged tree
(`the editing row = "", want the storm's keystrokes on it`). It now reads the
editing row from `ed.Layout().EditorTop`, which is what every sibling test does.

## Verification (all commands in `/home/user/workspace/gopi-cm2sw-resolve`)

```
CGO_ENABLED=0 go build ./...                            → BUILD_OK
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/         → BIN_OK
go vet ./internal/lineedit/ ./cmd/agent/                → VET_OK
go test -race -count=1 ./internal/lineedit/             → ok 5.797s
go test ./... -count=1                                  → see below
git merge-base --is-ancestor master HEAD                 → FF_OK    (fast-forwardable)
git merge-base --is-ancestor d8596127 HEAD               → TIP_COVERED
git diff --diff-filter=D --name-only master HEAD         → internal/lineedit/event.go (only; intentional dedupe)
grep -rn '^<<<<<<<' --include=*.go .                     → no hits
```

Master-side loss check: every path master changed since the base is either
byte-identical in the result or one of the files listed above (both-touched, the
resolution read). Two concurrent master commits
(`devdoc/finish-cleanup-single-node-wording-merge-2.md`,
`briefings/brief-merger-status-defects.md`) had to be re-merged in because master
moved twice while this resolution was being built.

## Not Done (honest)

- The park tag `archive/feat/chat-m2-status-wake` is NOT deleted here. `master`
  moved twice during the resolution, so the tag is only dropped once the merge
  commit is actually on master (the briefing's own condition: *when the branch has
  landed*). The command is `git tag -d archive/feat/chat-m2-status-wake`.
- The branch ref `feat/chat-m2-status-wake` is likewise left for the merger's
  ancestry prune (`git branch -d`), which is the repo's normal path.
