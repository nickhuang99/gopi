<!-- doc-class: normative -->
# the replay control OSD speaks in actions, not in coordinates

**Date**: 2026-10-11 · **Branch**: `task/01a12d00e4b0787db7fcce75a8ed966c` ·
**Files**: `cmd/agent/chat_replay_control.go`, `cmd/agent/chat_replay.go`,
`cmd/agent/chat_replay_control_test.go`, `cmd/agent/chat_replay_readonly_test.go`,
`cmd/agent/chat_replay_watcher_repro_test.go`

## The report

The operator ran `gopi chat --replay <id>` and photographed the info bar:

```
[gopi replay] jump back: cursor at beat 107 of 163 (was 112) — ...
```

and stated the rule that this document exists to record:

> 要么告诉用户能干什么，要么什么都不显示。
> (Either tell the user what they can DO, or show nothing at all.)

`beat`, `cursor` and `(was N)` name the replay's own machinery — positions in a
recording. The watcher has no model of a beat and no key that addresses one, so
the line was reporting a coordinate nobody could act on.

## Why this is the SECOND fix, and why the first one failed

`chatControlMovedLine` was already rewritten on 2026-10-11 (commit `11eb58815`),
from a promise the driver did not keep ("space resumes typing to the cursor") to a
statement of the limit. The operator rejected that version too, and the reason is
worth keeping: **the correction was more coordinates**. The line went from one
position to three ("cursor at beat 107 of 163 (was 112) … the run is still held at
beat 100"). The file's own `chatControlStepNameFwd` comment had already diagnosed
the class ("position words are read as statements about the RECORDING") and the
fix went the other way.

So the direction here is the opposite one, and it is the whole of the change:
**the OSD says what the key DID, and what the watcher can do NEXT.**

## What changed, line by line

| constant | before | after |
|---|---|---|
| `chatControlKeysLine` | `… ↑ ↓ jump 5 beats …` | `… ↑↓ skip (the run never goes back) …` |
| `chatControlPausedLine` | `PAUSED at beat N of M…` | `PAUSED — space resumes, ← → step back/forward (the run never goes back), q/Esc leaves the replay` |
| `chatControlResumedLine` | `resumed at beat N of M` | `resumed — the recording is being typed again; space pauses it, q/Esc leaves the replay` |
| `chatControlMovedLine` | `cursor at beat N of M (was X) … still held at beat H` | `step back ← (already paused) — done — paused; space resumes from where the run already is (…), q/Esc leaves the replay` |
| `chatControlHeldLine` | `held at beat N of M: the watcher's cursor is at beat C …` | `the run is held here — step forward → or skip forward ↓ moves it on, or space lets it continue (…)` |
| `chatControlQuitLine` | `the watcher left the replay after beat N — …` | `leaving the replay — the replay is ENDING, the remaining recorded turn(s) will not be typed, …` |
| `chatControlAlreadyLeftLine` | `already leaving the replay from beat N of M — …` | `already leaving the replay — the closeout is in progress (…)` |
| `chatReplayLeftNotice` | `the replay ended at the watcher's request after beat N of M — …` | `the replay ended at the watcher's request — …` |
| `chatControlStartLine` | `replaying N recorded turn(s) at <pace> — this is a playback` | **DELETED** — no caller had used it since the controls landed; the sentence is kept in the comment |
| `chatReplayControl.state()` | `"%d/%d (cursor %d)"` for a report line | **DELETED** — no caller anywhere in the tree (the only `.state()` left belongs to an unrelated `lineedit` interface), and it was the last position-reporter in the file |

**Without a coordinate anywhere, no key that still works.** The bindings, the
gate and the driver are untouched: `chatControlJumpBeats` is still what the ↑/↓
arms are spelled with, and every key text on every line is still composed from
`chatControlPauseRune` / `chatControlStepForwardGlyph` / … (Rule 13) — the glyphs
in the table above are rendered from those constants by the test, not typed.

## The hard part: keeping a REFUSED move distinguishable from one that HAPPENED

`chatControlAtEdge` exists because a clamped step that reads like a step is a
silent lie (Rule 15). It made that point with a coordinate
(`— already at beat 7 of 7`); the replacement has to make it without one:

```
step forward → (already paused) — done — paused; space resumes from where the run already is (…)
step forward → (already paused) — no move happened: that is the edge of the replay — paused; space resumes (…)
```

`chatControlMoveApplied` and `chatControlAtEdge` are opposite sentences of the
same shape, and the refused line carries one while the travelled line carries the
other. Two assertions pin it: one reads the two sentences back off the constants
and requires each line to carry exactly one of them (not both, not neither), and
the real-pty runs below produce both.

**A second signal was tried and REMOVED, and the removal is the interesting
part.** The held line was given two sentences — "held, step forward to release"
and "held at the edge, only the resume answers" — selected by an
`edgeMove atomic.Bool` the move set. It does not survive contact with the driver:
a stopping point walked to the TOP of the recording is at the run's own end, where
`beatAllowed` **allows** the beat about to be typed (`cursorOf() >= typed+1`), so
the run is not held there at all — the second sentence describes a state no run
reaches. The flag, its atomic and the branch were deleted; the refusal is said
where it happens, on the move's own line. Measured, not reasoned: two real pty runs
produce the refusal line and neither produces the held-at-edge one.

## Evidence: five real pty runs

The harness is the one this repo already established for this surface
(`ptydrive.py`: `pty.openpty()` → `Popen([bin, "chat", "--replay", id], stdin=
stdout=stderr=slave)` → write named key bytes → capture every byte the surface
wrote; raw transcripts under
`~/.gopi/sessions/01a12d00-e4b0-787d-b7fc-ce75a8ed966c/artifacts/`). Keys arrive
from the TERMINAL arm, which is the arm the control layer's origin test must tell
apart from the recording's injected keys.

```
$ python3 ptydrive.py ./bin/gopi 603999b6-… artifacts/final-keys \
    "WAITFOR:[gopi replay] playback:,WAIT:0.6,KEY:SPACE,WAIT:0.5,KEY:LEFT,KEY:RIGHT,KEY:RIGHT,KEY:LEFT,KEY:UP,KEY:DOWN,KEY:SPACE,KEY:SPACE,KEY:Q,KEY:Q"
wrote artifacts/final-keys.pty.raw (23450 bytes) exit 0
```

Every `[gopi replay]` control line from that transcript, verbatim:

```
[gopi replay] playback: space pause/resume · ← → step back/forward · ↑↓ skip (the run never goes back) · ?/h help · q/Esc quit the replay
[gopi replay] PAUSED — space resumes, ← → step back/forward (the run never goes back), q/Esc leaves the replay
[gopi replay] step back ← (already paused) — done — paused; space resumes from where the run already is (a step moves the replay's stopping point, and the run never goes back), q/Esc leaves the replay
[gopi replay] step forward → (already paused) — done — paused; space resumes from where the run already is (a step moves the replay's stopping point, and the run never goes back), q/Esc leaves the replay
[gopi replay] step forward → (already paused) — no move happened: that is the edge of the replay — paused; space resumes from where the run already is (…), q/Esc leaves the replay
[gopi replay] skip back ↑ (already paused) — no move happened: that is the edge of the replay — paused; space resumes from where the run already is (…), q/Esc leaves the replay
[gopi replay] skip forward ↓ (already paused) — done — paused; space resumes from where the run already is (…), q/Esc leaves the replay
[gopi replay] resumed — the recording is being typed again; space pauses it, q/Esc leaves the replay
```

`resumed` and the quit line come from `run3-full` / `run5-keys` in the same
directory; `q` twice is exercised in `run6-edge-q`.

## What was NOT done

- **No rewind.** The operator accepted this limit, and nothing here implements
  one: the run goes forward from where it stands. Every line that offers the
  resume still says so, once.
- **No switch, no second explanation.** There is no `--verbose`, and the clauses
  are not doubled (the rejected first fix).
- **The binding table did not change.** Pause / step / skip / help / quit are the
  same keys with the same effects.
- **`state()` did NOT get a reprieve.** It rendered `"%d/%d (cursor %d)"` and had
  **no caller anywhere in the tree** (searched: the only remaining `.state()` is an
  unrelated `lineedit` interface). It was the last position-reporter in the file, and
  an uncalled helper kept "for diagnostics" is a line waiting to be wired back up.
