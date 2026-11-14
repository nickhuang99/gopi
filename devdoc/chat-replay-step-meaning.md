<!-- doc-class: historical -->
# `--replay` stepping: what the key does, and what the OSD was promising

**Tasks** `01a12a67-777d-7426-ab4a-ce7b1b520f78` (the wording) ·
`01a12a8a-99c2-74a5-b408-4fe44dd41664` (this one: **decide whether the step should
really rewind**)
**Dates** 2026-10-11 · **Surface** `gopi chat --replay <session-id>`

## The question, and the answer in one line

The operator asked (2026-10-11, verbatim): *"if step back or forward is really
difficult to implement, agent should issue a report to explain."* The report is
this document, and its finding is:

**A step CANNOT rewind, and the reason is the terminal rather than the effort.**
The beats the watcher stepped back over have already been pushed OUT of the band
and into the terminal's scrollback, where no escape sequence a program may send
can take them back — so any "un-type" would either leave those rows standing
(proving the surface lied about where the replay is) or be a full-screen wipe
that is a *mode change*, not a step. The beats themselves are also not
replayable: the model script is served by a monotonic request counter.

The OSD wording therefore is not a deferral — **it is this surface's terminal
state**, and it says exactly that: *a step moves the cursor, and the run never
goes back*.

## What the operator reported first, and what was measured then

`gopi chat --replay <session>` paused on `space`, and `←`/`→` "did nothing". The
attached capture reads:

```
you> [system] [t+511s[gopi replay] step back: cursor at beat 74 of 75 — the pause
HOLDS the run there — the run ythe pause HOLDS the run there — the run has typed
7; space resumes typing to the cursor, q/Esc leaves the repla
```

Harness `ptydrive.py` drives the run on a **real pty** at 117×26, pressing keys by
name (`SPACE`, `LEFT`, `RIGHT`, `UP`, `Q`) and recording **every byte the surface
wrote**; reading is a separate pass over the raw bytes, so the harness cannot
report its own expectation back as evidence.

**1. The arrows DO work, and always did on this terminal.** Four arrows in one run
(pre-fix binary) each moved the cursor exactly one beat. Transcript
`ptyrun/PREV-C.pty.raw`.

**2. The garbled capture is the reporter's own paste, not the surface.** The
`[gopi replay]` lines of two consecutive moves sit at *different columns of the
same row* (measured: `\r\x1b[42C\x1b[25;43H` then `\r\x1b[37C\x1b[25;38H`), so
copying the row concatenates them — "the run ythe pause HOLDS" is `…the run y|ou>`
+ `the pause HOLDS…` overlapped. Nothing on the screen is broken; the paste is.

**3. The real defect was a PROMISE THE MECHANISM DOES NOT KEEP.** The line ended
`the run has typed 7; space resumes typing to the cursor`. A cursor behind the run
**does not rewind it**: the driver types forward from where it stands
(`beatAllowed`/`waitForCursor`, `chat_replay.go`). Third report of this key.
The fix: the words now state the limit, at one place per line, composed from the
same constants the bindings use (Rule 1b/13) — `chatControlMovedLine`,
`chatControlKeysLine`, `chatControlPausedLine`, `chatControlCursorClause`, the
step names carrying their glyphs, `chatControlAtEdge`, the jump words.

## Does a step really rewind? — the measurement this task exists for

Runs D1–D3 used the **pre-fix** binary, D4 the post-fix one; all four are live
`gopi chat --replay` runs on a real pty of the recording
`5245eeaf-b17a-407d-93e6-4c5bdcfefbc4` (8 turns, 10 recorded tool results,
recorded at 62×26). Transcripts: `ptyrun/{D1,D2,D3,D4}.pty.raw`, schedules in
`*.plan.json`.

### (1) What a step-back actually emits: its own OSD line, and nothing else

`ptyrun/D3.pty.raw`, four `←` presses at byte offsets 18184 / 18350 / 18516 /
18682 — **each record is exactly 166 bytes**:

```
ESC[25;1H  ESC[2K  ESC[1m ESC[38;2;255;248;220m you> ESC[0m \r ESC[55C
ESC[25;56H [gopi replay] step back: cursor at beat 7 of 8 — the pause HOLDS …
```

Across the whole record: **0 newline bytes, 0 placements inside the band**
(the only two absolute placements are row 25 — the editing line; the band is rows
1..24 per the run's own `ESC[1;24r`), **0 ED**. A step does not touch a single
band cell: it rewrites the OSD text on the editing row. That is *moving a cursor*,
exactly as the line now says.

### (2) Nothing is taken back, and nothing is hidden

Feeding those same transcripts to the repository's **own** terminal interpreter
(`lineedit.Screen` — the interpreter the chat dump itself uses, `lineedit/screen.go`)
at every step-back boundary and reading the band and the scrollback:

```
D2  END        byte= 24292 scrollback=187 | turn 1=scrollback turn 2=scrollback turn 3=scrollback
D2  step-back  byte= 18130 scrollback=117 | turn 1=scrollback turn 2=band       turn 3=band
D2  step-back  byte= 18296 scrollback=120 | turn 1=scrollback turn 2=scrollback turn 3=band
D2  step-back  byte= 18628 scrollback=127 | turn 1=scrollback turn 2=scrollback turn 3=band
D2  step-back  byte= 18960 scrollback=134 | turn 1=scrollback turn 2=scrollback turn 3=scrollback
D3  step-back  byte= 18184 scrollback=118 | turn 1=scrollback turn 2=band       turn 3=band
```

Both surfaces only ever **grow**: the scrollback goes 117 → 134 lines across six
`←` presses. The rows that leave the band leave it because the RECORDING's own
lines push them with the LF at the band's live row (`region.commitScroll`), which
is how a committed row is saved — the rule the whole layout exists for (an
interior top margin discards what it scrolls off). A rewind would have to run that
direction backwards. It cannot: those lines are in the terminal's scrollback.

### (3) The half-erase, on the minimal sample — the failure named concretely

`ptyrun/D2.pty.raw` byte 21336 is the `←` that lands a turn behind the run while
it is held. Its record (213 bytes) again places only rows 25, and reads:

```
ESC[25;1H ESC[2K ESC[1m … you> ESC[0m \r ESC[5C ESC[25;6H
[gopi replay] step back: cursor at beat 1 of 8 — the step PAUSED the run — the run has typed 4; …
```

At that moment the band holds `you> [gopi replay] held at beat 5 of 8: …` on row
13 plus the recording's turn-4 block below it, and only **5 of the 10** recorded
rows then on screen are already in the scrollback; the four **turn headers** the
cursor has gone behind are **band-only** (`──── turn 2 · …`, `──── turn 3 · …`
verified: `already in scrollback: false`).

Now perform the erase the layout's own reset uses — `region.Reset()` = reset the
region, home the cursor, ED(2) (`ESC[1;1H ESC[2J`):

```
band AFTER the half-erase:        blank=true   — every row the recording drew is gone from the SCREEN
scrollback AFTER the half-erase:  117 lines, unchanged
```

**The two halves of the screen end up disagreeing, and no driver-side sequence can
reconcile them.** That is the concrete intermediate state this task was asked to
name:

- **ED(2) leaves the scrollback holding rows the replay claims not to have
  reached** — the recording's own rows, in the operator's terminal, un-erasable by
  any program. A step that wiped the band and then re-typed from the cursor would
  leave the earlier turns standing one keystroke up: *"a replay shows a line the
  recording never contained"* has a second, worse form — a replay whose screen
  says "beat 1 of 8" while the operator can scroll up and read turns 1–3. The
  driver's own comment (`chat_replay.go`, above `beatAllowed`) refuses exactly
  this shape.
- **The interior erasure the band would honour is the one the layout may not
  issue.** `region.go` anchors the band's top at the screen's FIRST row on
  purpose; an erasure from an interior row is what discards committed lines
  (measured on real xterm: region `[1;19]` kept LINE-001 after 60 lines, region
  `[2;19]` lost lines 1..43). So (i) "the band's own rows are inside the region"
  and (ii) "those rows can be cleared without the discard" cannot both hold — and
  (i) is why the top margin never moves.
- **What is left is a full repaint**, which is what `region.Reset()` does: reset,
  home, ED(2), then draw the recording again from its first line. That is a
  *mode change of the whole chat*, not a step — every non-recorded row (the
  startup notices, the session banner) goes with it, and the "step" would have to
  redraw the run to the cursor to be honest about where it is.

### (4) The beats are not replayable either — the script is a counter

Even a driver that could re-draw could not re-**ask**. `replaydrive.Stub.handle`
takes the next response by an integer it increments per request
(`s.reqs++`, `idx := s.reqs - 1`; `Bodies()` appends for the same reason), the
executor consumes `served` in arrival order, and the replay's own turn-over
predicate is that counter (`RequestsThroughTurn`). Re-typing turn *k* would make
the surface issue request *k* again and be answered with response *k+1* — a
replay that silently shows the wrong turn's answer, which is worse than no
rewind. And a beat typed twice is a beat the protocol would record twice
(`chat_replay_beats.go` numbers beats monotonically).

### (5) The post-fix run confirms the surface says so

`ptyrun/D4.pty.raw` (post-fix binary, same plan as D3): the help line reads
`← → move the cursor (the run never goes back)`, the pause line the same, each
step names itself with its glyph and `(was %d)`, and the move line ends
`the run is still held at beat 3; space resumes the typing that is left (a step
moves the cursor, and the run never goes back)`. The held line is
`held at beat 5 of 8: the watcher's cursor is at beat 4 — →/↓ move it ahead, space
resumes the typing that is left`. No line offers a rewind.

## The change (what shipped, and where)

One place per sentence, every key composed from the same constants the bindings
use (Rule 1b/13) — `cmd/agent/chat_replay_control.go`:

- `chatControlMovedLine`: names the cursor, **the beat it came from**, whether the
  move took or maintained the pause, the run's own held beat, and states the
  limit outright.
- `chatControlKeysLine` / `chatControlPausedLine`: `move the cursor (the run
  never goes back)` instead of advertising a step in the recording.
- `chatControlStepNameFwd/Bk`: the step's NAME carries its `→`/`←` glyph.
- `chatControlCursorClause`: `; cursor at beat %d, behind the run`.
- `chatControlAtEdge` lost a `; ` that read as a new sentence; the jump words
  became `jump back`/`jump forward`.

**No binding changed. No key was added. No driver behaviour changed:** the pause
still holds, the cursor still moves, the run still never goes back.

## Tests

Three cases asserted the old sentences and now assert the corrected ones — each on
the key itself, each stating the limit (`chat_replay_readonly_test.go`,
`chat_replay_watcher_repro_test.go`, `chat_replay_control_test.go`).

## Not done (honest)

- **The beats a step goes back over are still not replayed** — and this document
  is the finding that they cannot be, with the terminal's own scrollback as the
  reason. This is not a deferral: **the current OSD wording is the surface's
  terminal state**, and no follow-up task is created for a rewind. A future
  task that wants one must first answer §(3): how a band that has committed its
  rows into the terminal's scrollback is un-drawn without (a) leaving those rows
  standing or (b) wiping the whole chat — both measured above.
- **The split-sequence keyboard was not reproduced.** `\x1b[D` arrived whole on
  this host, so the "arrow became a lone Escape → leave" path is described from
  the decoder's contract, not from a measurement.
- `gopi session replay`'s own keys are untouched (that verb binds `←`/`→` to
  `StepForward` in its own loop, a different surface).

## Evidence (self-filed)

- Pre-fix arms: `ptyrun/D1.pty.raw`, `D2.pty.raw`, `D3.pty.raw` (live runs,
  pre-fix binary `bin-gopi`); earlier wording arm `ptyrun/PREV-C.pty.raw`.
- Post-fix arm: `ptyrun/D4.pty.raw` (binary `bin-gopi-postfix`, built from this
  branch).
- Per-step byte records: 166 bytes each, 0 LF / 0 band placement / 0 ED — offsets
  18184, 18350, 18516, 18682 in `D3.pty.raw`.
- Band/scrollback readings and the half-erase: produced by feeding those
  transcripts to `lineedit.Screen` (62×26) — the same interpreter the chat dump
  uses; the readings are quoted verbatim above.
- All artifacts live under
  `/home/user/.gopi/sessions/01a12a8a-99c2-74a5-b408-4fe44dd41664/ptyrun/`
  (Rule 37: evidence in the session directory, never the worktree; task report in
  this session's `task-report.md`).
