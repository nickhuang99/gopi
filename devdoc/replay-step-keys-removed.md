<!-- doc-class: normative -->
# The step keys are REMOVED, not re-explained

**Task** `01a15997-3a10-77be-8280-0c9c9f9eac9c` · 2026-10-20 · branch `task/01a15997-3a10-77be-8280-0c9c9f9eac9c`

## The defect, in one paragraph

`gopi chat --replay` bound ←/→/↑/↓ to "step back / step forward / skip". The
driver types a recording **forward** from where it stands and cannot un-type a
beat, so those keys moved a cursor that is not on the screen and changed nothing
the watcher could see. What they *did* produce was an OSD line per press: a
paragraph explaining what the key had not done, which replaced the information
bar. The operator reported this three times; each previous repair made the
sentence **longer and more specific** ("a step moves the replay's stopping point,
and the run never goes back"), which makes the illusion stronger, not weaker.

## The rule this task applies

> 要么告诉用户能干什么，要么什么都不显示。

A key whose only effect is a line explaining that it does nothing is a key that
must not be bound. So the fix is removal, and the removal is the deliverable:

| surface | before | after |
|---|---|---|
| `chat --replay` keys | space, ←/→, ↑/↓, `?`/`h`, `q`/Esc | **space, `q`/Esc** (arrows swallowed, inert) |
| `chat --replay` opening line | key list before the first beat | **nothing** |
| pause / resume line | a sentence with a coordinate and a clause | `PAUSED — space resumes, q/Esc leaves the replay` |
| `session replay` (`internal/replayplay`) | space, `n`/→, `p`/←, ↑/↓, Home/End, +/−, `?`, `q` | **untouched — it really can step** (see the range correction below) |
| the test that froze the defect | *required* the strings "step back/forward", "the run never goes back" | **asserts them absent** |

## Range correction: `internal/replayplay` is NOT the same surface (task `01a15998`)

The briefing for this task listed `internal/replayplay/help.go`'s `p or ←` row and
`loop.go`'s `endOfMaterialNotice` among the things to delete. **That instruction was
withdrawn**, and the operator's reason is the criterion to keep:

> "不，这个功能是完全实现了，而且实现的很好，不要改！session replay 是支持步进步退，很完美。"

`gopi session replay` **really does step** — its driver rewinds a round — so
`p`/`←` there is a binding on a capability that exists, and the end-of-recording
notice tells the truth. `gopi chat --replay`'s arrows moved a cursor that moved
nothing. Same phrase on the OSD, **opposite truth value behind it**.

So the first cut of this branch did touch `internal/replayplay/{help.go,loop.go,
loop_test.go}` and `cmd/agent/cli_session_replay_test.go`; commit `9f7ea7758` restored
all four to their base (`224cbc6`) content, byte for byte:

```
$ git diff --stat 224cbc6 HEAD -- internal/replayplay/ cmd/agent/cli_session_replay_test.go
(empty)
```

**The principle, for the next reader:** "the same word" is not "the same thing".
The test of whether a key may be deleted is whether the DRIVER behind it does the
thing — never whether the wording looks alike.

## What was deleted (and why each one had to go)

- `chatControlStepFwd/Back`, `chatControlJumpFwd/Back`, `chatControlSkipNameFwd/Bk`,
  `chatControlStepNameFwd/Bk`, `chatControlJumpBeats`, the four glyphs,
  `chatControlMovedLine`, `chatControlHeldLine`, `chatControlAtEdge`,
  `chatControlMoveApplied`, `chatControlHeldClause`, `chatControlPausedByStepC`,
  `chatControlKeysLine`, `chatControlHelpRune/Letter`, `announceStart`.
  Every one described a step or a skip.
- `chatReplayControl.move`, `clampLocked`, `cursorOf`, `setHead`, `edge`; the
  fields `cursor`, `head`, `total`, `edgeNow`; `chatReplay.{beatAllowed,
  sayHeld, waitForCursor, advance, heldBeats, heldAt}`. With no key able to move
  the cursor, the cursor could only ever be "at the end" — a constant dressed as a
  question, asked before every keystroke.
- `newChatReplayControl`'s `turns` parameter: it seeded and clamped the cursor.
  Kept "so callers do not change" it would read as meaningful while nothing read
  it. The callers changed.

**Not deleted, after the range correction:** `replayplay.KeyStepForward/KeyStepBack`,
`Player.step`, the `KeyMapRows` step rows and `endOfMaterialNotice` are all still
there, unmodified — that surface's step is real (see below).

## What was KEPT

space (pause/resume), `q`/Esc (leave), the OSD notice prefix, the `closing` /
`quit` distinction, the editor's input hold, `Home`/`End` (a jump names a round
that exists, so it needs no saturation notice), the `+`/`−` speed keys, `?` in
`session replay` (that surface still has a list to reprint).

## The correction the pty evidence forced (this is the interesting part)

The first cut made the arrows **unbound** — `controlAction` returned nil, the gate
declined them, they fell through to the editor "which is held read-only anyway".
A real-pty run with the section paused disproved that reasoning:

```
P0-no-keys         bytes=0
P1-four-arrows     bytes=765   visible='you> [EPIC GOAL][Task summary]chat /voice …'
P2-no-keys-again   bytes=0
```

`↑` reached the **editor's history recall** and REPLACED the line the recording
was being typed into. The input hold did not stop it because the hold refuses keys
that *mutate* the line and `lineedit.MutatesLine` classifies ↑ as **motion** —
correct for an editor in a menu, wrong for a line nobody may edit. Fixed at the
layer that owns the surface: the four arrows are **claimed and inert**
(`return func() {}`) rather than declined. Re-measured on the same pty:

```
P0-no-keys         bytes=0
P1-four-arrows     bytes=0
P2-no-keys-again   bytes=0
```

This is Rule 31 in its plainest form: the arm that "obviously" could not matter —
the fall-through — was the only arm with a visible effect, and only executing it
found that out.

## Evidence

Run directory: `~/.gopi/sessions/01a15997-3a10-77be-8280-0c9c9f9eac9c/evidence/`
(scripts + raw pty streams; the scripts are the recipe, the `.txt` files are what
the terminal actually received).

- `pty_session_replay_step.py` → `pty_session_replay_step.txt` — **the regression
  evidence for the range correction**, on the real pty with the real binary:
  `gopi session replay <id>` opened at round 1, and while paused `n` moved 3→4,
  `→` 4→5, `p` 5→4, `←` 4→3, `exit_status=0`, no failed expectation. That surface
  really steps, and this branch did not take it away.

- `pty_keys_AFTER.txt` — the whole acceptance run, per-segment byte counts:
  `B_arrows-while-paused 0`, `space-PAUSE 1398`, `C_space-RESUME 11748`,
  `D_escape-QUIT 2628`, `exit_status=0`; the notice lines are exactly
  `turn 1/5`, `PAUSED — …`, `resumed — …`, `leaving the replay …`; and every
  retired word (`step back`, `step forward`, `skip`, `never goes back`,
  `step moves`, `pause/resume`) is **absent** from the whole stream.
- `pty_pause_control_AFTER.txt` — the three equal windows above.
- `pty_pause_control.txt` — the same three windows measured **before** the
  correction, which is the red proof for the inert-claim arm.

## Gates

- `go build ./...` — clean.
- `make test-budget PKGS='./cmd/agent ./internal/replayplay'` — see the task report.
- `go test ./internal/standards/` — see the task report.
- The inverted assertions: `TestChatReplayControlPrintsNoKeyHelp`,
  `TestChatReplayControlArrowsAreUnbound` (claims, writes nothing, leaves the
  pause standing), `TestReproArrowsDoNothingAtAll`,
  `TestChatReplayArrowsLeaveTheSurfaceSilent`.
- `internal/replayplay`'s own tests are unchanged and green — that is the
  regression evidence for the range correction: `go test ./internal/replayplay/`
  → `ok github.com/the operator/gopi/internal/replayplay 1.214s`.
