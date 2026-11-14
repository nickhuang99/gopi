<!-- doc-class: historical -->
# The completion menu's lost wake: a notification a parked read never sees

**Task:** fix `TestChatCompletion_EscapeClosesTheMenu` (internal/lineedit/menu_test.go:518),
which flakes ~1 in 30-60 under `-race` on master `2fdc65e4` with
`drawn=1 wakes=0 lastTimeouts=[-1 -1 -1]`.
**Branch:** `fix/chat-completion-sticky-wake` (fresh off master, Rule 23).
**Report:** `/home/user/.gopi/sessions/01a0c1a2-4306-7929-a8f8-64caed064f7d/task-report.md`.

## What the DIAG actually meant

The flake's own diagnostic names two different things, and separating them is the
whole analysis:

| field | value | what it says |
|---|---|---|
| `seq=2 fin=2 rev=2` | the publish happened | the worker computed and installed the menu |
| `drawn=1` | the screen never got it | the read loop drew the PREVIOUS generation |
| `wakes=0` | no bounded wake was spent | the loop was in an UNBOUNDED read (`-1`) |
| `lastTimeouts=[-1 -1 -1]` | three unbounded reads | nothing was outstanding when each was decided |

`wakes=0` + all `-1` is the load-bearing pair: the loop was parked in a BLOCKING
read at the moment the publish landed, and it never came back to look.

## The window, measured (not guessed)

Master's mechanism is *supposed* to close this window with a generation counter
(`rev` vs the editor's `menuRev`), and everything else about it is correct — a
generation "cannot be missed, only seen". Instrumenting the loop directly (a
throwaway probe worktree, `GOPI_LINEEDIT_PROBE=1`) showed the two halves of the
window are NOT equivalent:

```
publishLocked rev=2                        ← the worker installs the menu (lock held)
worker: published rev=2 seq=2 fin=2        ← …and stores the unread flag AFTER
worker: notify sent                        ← …and raises the notification AFTER that
ANOMALY parked-unbounded while completion pending (rev=2 menuRev=1 reqPend=false)
loop: PARK-UNBOUNDED rev=2 menuRev=1 undrawn=true reqPend=false unread=true
src: parking (timeoutMS=-1)
```

`menu.go`'s worker does three separate things outside one atomic step:
`publishLocked` (rev++, notify), then `unread.Store(true)`, then `notify.Notify()`.
A read loop that checks in between can therefore see `rev != menuRev` while
`unread` is still false — and the repaint that acknowledges the generation is
itself split: `clampedMenu()` READS `rev` (mid-repaint, via
`publishMenuHeight`), and `paintMenu()` DRAWS later in the same call.

## The root cause: the notification was never a wake

Reducing the flake to a deterministic guard exposed the real defect, and it is
not in the editor at all. `scriptedSource` stands in for a TERMINAL, and a
terminal owns the notification PIPE (`setNotifyPipe` is called from
`newA7Harness`); but `Notify()` ignored the pipe it owns and sent on an
in-process channel instead:

```go
// master — the wake that never reaches a parked reader
func (s *scriptedSource) Notify() {
    notify := s.notify            // cap-1 channel, selected on only when NOT holding the lock path
    select { case notify <- struct{}{}: default: }
}
```

The reader watches the PIPE (`notifyCh`) via a watcher goroutine; the inner
`park` select watches `wake` / `stop` / `notifyCh`. So the publish's wake was
delivered to a channel **no parked reader selects on** — precisely the DIAG's
`wakes=0`. The library half (`Terminal` + `wakePipe`) was always correct: it
writes a byte into the pipe, and `ReadEvent` polls that pipe.

## The fix (two halves, both required)

**1. The test seam delivers its notification through the pipe it owns**
(`internal/lineedit/scripted_test.go`). `Notify()` now writes the byte into
`notifyPipeW` when a pipe is installed, exactly as production's `wakePipe` does,
falling back to the in-process channel only for a source with no pipe.

**2. The completion publish becomes a LEVEL, not a notification**
(`internal/lineedit/editor.go`):

- `menuNeedsDraw()` — ONE predicate for "a published candidate list the screen has
  not seen", the OR of the three ways it can be outstanding
  (`menuUnread() || menuDirty || menuUndrawn()`). Every read outcome consults it,
  including the IDLE arm, which previously asked only about the status row.
- `clampedMenu()` raises `menuDirty` when it observes a generation it has not
  acknowledged, instead of acknowledging it silently (`e.menuRev = rev` alone is
  a read, not a draw — that is how the window was lost).
- `repaint()`'s tail keeps the level RAISED when the publish it just
  acknowledged was not drawn after all (`if e.menuUndrawn() { e.menuDirty = true }`),
  so the request survives and the next read outcome repaints.
- `parkgate.go` — the *park window* hook (`runParkGateHook()`, one nil check in
  the loop, nil in production) that lets the guard construct the interleaving
  instead of sampling it.

**Why a level and not a retry:** the level is idempotent and costs nothing —
consulting it twice cannot miss a draw and cannot buy one that is not owed, so
the "no repaint per poll" budget (`menuDirty` false on an idle wait with no
publish) still holds.

## A/B matrix — which half does what

The guard `TestChatCompletion_PublishInTheParkWindowIsStillDrawn` runs the same
window against four tree states (3 runs each, `-count=1`):

| state | result |
|---|---|
| [A] editor fix only (seam reverted) | FAIL FAIL FAIL |
| [B] seam fix only (editor fix reverted) | FAIL FAIL FAIL |
| [C] both | **PASS PASS PASS** |
| [D] neither (master-equivalent) | FAIL FAIL FAIL |

Neither half is decorative: without the seam the notification never reaches a
parked read; without the level the publish still does not survive the park.

## Red-proof (recorded)

Reverting ONLY `scripted_test.go` while keeping the guard gives 3/3 FAIL:

```
menu_publish_window_test.go:132: timed out waiting for the candidates published
in the park window to reach the screen
[DIAG seq=4 fin=4 rev=4 drawn=3 wakes=0 open=true ncand=2 lastTimeouts=[-1 15 -1 -1 -1 -1]]
```

The DIAG in the red-proof is the flake's own — a published, open menu
(`ncand=2`), `drawn=3`, `wakes=0` — reduced to zero timing luck.

## The guards

`internal/lineedit/menu_publish_window_test.go`:

- `TestChatCompletion_ParkGateHookIsWired` — the guard's own precondition; RED if
  the hook's call site is removed, so the guard below cannot pass vacuously.
- `TestChatCompletion_PublishInTheParkWindowIsStillDrawn` — THE red-proofed guard:
  the loop is held in its park window (the wait has been decided, the read has not
  begun), a publish carrying a NEW candidate list is raised through the editor's own
  completion seam, and the assertion is the RENDERED SCREEN. Nothing is pushed, no
  second publish is raised: the only thing that can end that read and paint the
  candidates is the notification the parked loop must see.
- `TestChatCompletion_PublishWhileParkedIsDrawn` — the same level from the other
  end (publish from the test goroutine while the loop is parked on an idle wait).

The guard asserts the LEVEL itself (`h.completionLevelUp()`), never the editor's
own predicate, so it states behaviour and can run against a tree without the fix
— which is what makes the red-proof possible.

## Two findings that did NOT pan out (recorded so the next reader does not chase them)

1. **The briefing's park window (publish between `drainNotify()` and the park) is
   SURVIVABLE.** `scriptedSource.ReadEvent` re-checks `drainNotify()` inside its
   loop, and the inner park select includes `notifyCh`, so a publish landing there
   is caught. Measured: that race is rescued (DIAG `wakes=1`, menu drawn twice).
2. **The idle-arm term is not independently reachable in the guard.** Removing
   `menuNeedsDraw()` from the idle arm does not change the guard's verdict,
   because a pending level forces the BOUNDED cadence *before* the read, so the
   idle arm is never the outcome that carries a publish. The term is kept as
   defence in depth (it is correct, it costs nothing — the level is false on an
   idle wait with no news), but it is NOT the flake's root cause and no guard
   red-proofs it. A candidate guard for it was written and withdrawn because its
   premise (reaching the idle arm with the level up) is what the cadence prevents.

## Baseline note (honest)

The briefing's rate did not reproduce in an isolated single-test loop on this
host: `for i in $(seq 1 60); … go test -race -count=1 -run
TestChatCompletion_EscapeClosesTheMenu` gave **60/60 ok**. The flake reproduces
readily with `-count=25` in ONE process (iteration 9 and 12 of two runs), which
is how every trace above was captured. The pre-existing `-race` harness race at
`scripted_test.go:141` (`initLocked` without the mutex) is a separate defect that
`-count=25` surfaces; it is NOT this flake and was left alone.

## Files

- `internal/lineedit/editor.go` — `menuNeedsDraw()`, level-aware arms, the
  `clampedMenu` / `repaint`-tail level maintenance, the park-window hook call.
- `internal/lineedit/scripted_test.go` — `notifyPipeW` + `Notify()` through the pipe.
- `internal/lineedit/parkgate.go` — the park-window seam.
- `internal/lineedit/menu_publish_window_test.go` — the guards.
