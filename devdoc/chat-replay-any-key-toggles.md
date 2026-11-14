<!-- doc-class: historical -->
# `gopi chat --replay`: any key toggles, Esc leaves, and the surface prints no help

**Date:** 2026-10-21 · branch `master` in worktree `01a15b1d` (base `bf119eb`).

## What changed and why

The playback surface of `gopi chat --replay` printed instructions. Three lines
carried them:

| line | before | after |
|---|---|---|
| `chatControlPausedLine` | `[gopi replay] PAUSED — space resumes, q/Esc leaves the replay` | `[gopi replay] PAUSED` |
| `chatControlResumedLine` | `[gopi replay] resumed — space pauses, q/Esc leaves the replay` | `[gopi replay] RESUMED` |
| `chatControlQuitLine` | `[gopi replay] leaving the replay — the replay is ENDING, … (the model is live from the next turn on)` | `[gopi replay] LEAVING` |
| `chatControlAlreadyLeftLine` | `[gopi replay] already leaving the replay — the closeout is in progress (this line is the ONLY key this layer still answers: …)` | `[gopi replay] LEAVING` |
| `chatReplayLeftNotice` (the driver's) | `[gopi replay] the replay ended at the watcher's request — the remaining recorded turn(s) …` | `= chatControlQuitLine` |

The information bar sits inside the picture the watcher is watching, so every word
of instruction is a word of the recording they do not get to see. The operator's
rule for this column is "要么告诉用户能干什么，要么什么都不显示", and the way to satisfy
both halves is a control scheme simple enough that saying nothing loses nothing.

## The scheme

```
ANY key   -> toggle pause/resume
Esc       -> leave  (and it is the ONLY key that leaves)
```

`controlAction` (`cmd/agent/chat_replay_control.go`) is now two lines of
decision: `KeyEscape` → `leave`, everything else → `toggle`. It returns `nil` for
no key.

## Why "any key" must be CLAIMED, not merely "not bound"

The key seam is `lineedit.KeyGate`, and a key the gate **declines** falls through
to the **editor** — which is the performance surface, the line the recording is
being typed into. Measured 2026-10-20 (previous pass of this same epic): `↑`
reached the editor's history recall and REPLACED the performed line with a
previous prompt — 765 bytes of redraw — and `Editor.InputLocked` did not stop it,
because `lineedit.MutatesLine` classifies `↑` as motion.

So the requirement is per key: **claimed AND acted on**. Claimed-but-inert (the
previous pass's fix for the arrows) satisfied the fall-through requirement and
violated the new one; the any-key rule satisfies both with one rule instead of a
special case.

The **origin test stays first**: `k.Origin != lineedit.ReadOriginTyped` → not
claimed. Recorded typing is never a control — otherwise a replay pauses itself
every time a recorded prompt contains a space.

## Termination and the editor hold

- `leaveAgain` (post-`closing`) now answers **Esc only** — while the replay runs
  every key toggles, and once it is ending there is no pause left to toggle.
- `markClosing` still calls `releaseLine`, so the hold is given back at the same
  instant the mode flips: a hold outliving its owner drops the operator's first
  keystrokes at the REPL prompt.
- `chatReplayLeftNotice` is now the same word as the key layer's, because the two
  moments are ONE transition (and two different words for it would be two states
  the watcher has to reconcile).

## Tests: assert ABSENCE, not a corrected sentence

`cmd/agent/chat_replay_control_test.go`:

- `chatControlRetiredWords` now carries both generations of removed prose — the
  step/skip words **and** the key-naming help words (`space resumes`,
  `space pauses`, `q/Esc`, `press `, `leaves the replay`, `key`, `keys`, `help`).
  A new helper `chatControlHelpViolation` is the ONE reading of that list, shared
  by the unit cases and the pty cases beside them (Rule 13).
- `TestChatReplayControlAnyKeyToggles` (new) drives eleven unrelated keys (letter,
  digit, punctuation, four arrows, Enter, Backspace, Ctrl+C) through ONE layer and
  asserts the ALTERNATION of the pause state, plus "none of them is a quit".
- `TestChatReplayControlClaimsOnlyATypedKey` gained the typed half: thirteen keys,
  each on a fresh layer, each claimed, each writing exactly one state word, each
  doing the one thing it can do (toggle, or leave for Esc).
- `TestChatReplayControlPrintsNoHelpText` (renamed from `…PrintsNoKeyHelp`)
  asserts four things: no opening line; `?` is an ordinary toggle that names
  nothing; the retired words appear in no constant and in no line a key produces;
  and each state line is a **bare state word** (≤24 bytes, no em dash/semicolon/
  comma, no space, upper case).

`cmd/agent/chat_replay_readonly_test.go` / `chat_replay_watcher_repro_test.go`:
the pause is now taken with `a` (not the space the deleted binding table named —
a case that pressed the bound key would pass against the binding this change
removed), and the arrow cases assert the alternation instead of silence, with the
editor line still byte-identical.

`waitForDrops` → `waitForClaimed`: the editor's hold count stopped being the right
instrument the moment every typed key became a control, because a key the gate
claims never reaches the hold. The property asserted (a watcher's key does not
edit the performance surface) is unchanged; the tally that proves the key landed
on the control path is the layer's own.

## Verification

`go build ./...` exit 0; `go test ./cmd/agent/ -run 'ChatReplay|ChatControl|Repro'`
→ `ok 24.778s`; `go test ./internal/replayplay/` → `ok 0.301s`.

Real pty (harness `ptydrive2.py`, session `004c3578-…`, per-key byte windows):
nine key presses across two runs each produced the expected state word, and the
editor's own row was **byte-identical before and after every one of them**; the
whole transcript carried **zero** occurrences of any help word. The base binary
(`bf119eb`) is the red proof: `a`/`↑` produce no state at all there, and
`space`/`space`/`Esc` print the three help sentences this change deletes.

Evidence files: `~/.gopi/sessions/01a15b1d-1fc0-7512-85ad-2b279b888651/`
(`evidence.md`, `runA/runB.*`, `runBASE/runBASE2.*`, `ptydrive2.py`).
