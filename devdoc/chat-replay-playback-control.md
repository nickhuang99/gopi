<!-- doc-class: normative -->
# Chat replay playback control — pause, step, jump, quit

**Status: normative.** The behaviour described here is on the branch
`task/01a105dbcd077a99beb96f61e365f64f`; the keys, the constants and the seams it
names resolve there.

## The want, in the operator's own words

> "我无法暂停，或者快进快退，或者退出…否则就失去了回放的目的"

A `gopi chat --replay` run typed the recording at its own pace and the watcher
could only watch. That is not a replay; it is a performance. This change gives the
watcher the four verbs a replay needs — **pause/resume, step, jump, quit** — plus a
help key, and an on-screen line that says which state the run is in.

## The design decision that everything else follows from

**The control keys arrive at the editor's own key seam — never a second reader on
the terminal.**

A second reader was the obvious first idea and it is wrong. The read loop already
owns the terminal: it is inside the reserved layout, it is the layout's only
writer, and a second reader on the same pty would fight it for bytes — the two
would interleave, a byte a human typed could be eaten by whichever read came first,
and the recording's own typing would sometimes be read by the wrong party. So the
control layer installs itself at the editor's key seam and claims the keys it
defines, before the editor applies them.

That decision produced three requirements the implementation would otherwise have
had to invent a mechanism for:

### 1. An operator's key must be distinguishable from the driver's — a FACT, not an inference

The recording contains spaces. It contains `q`, `?`, arrows, and (measured, in the
very session this was built from) a recorded `/quit`. A control layer that matched
on the key code alone would pause the run every time a recorded prompt contained a
space, and would eat the run's own exit line.

The distinction is made where it is **known**: the injector says which of its two
arms served the read it just returned (`Injector.ReadOrigin`), the editor asks its
source at the moment it decodes (`Editor.sourceOrigin`), and every decoded key
carries the answer in `Key.Origin` (`internal/lineedit/keysource.go`). Nothing
downstream guesses from timing or content.

**The zero value is "typed"** (`ReadOriginTyped`), deliberately: every existing
caller and every hand-built `Key` literal means "a human typed this", so the
zero value carries the historic meaning and cannot be mistaken for a missing one.
`ReadOriginInjected` is the injector's queue arm, and it is stamped in the one
decode path, so keystroke, short-paste envelope and `applyData` reads all get it.

### 2. An operator's key must not be charged against the injection budget

The injection budget (`chat_replay_budget.go`) exists to bound the keystrokes the
**model** and the **script** deliver. The watcher's keys arrive from the terminal
arm and are never injected, so they spend nothing the bound was protecting — and
the run's recorded `/quit` must still be typed afterwards. That is an acceptance
case below, measured, not asserted.

### 3. Pause freezes the DRIVER and nothing else

The driver parks before its next keystroke, so not one byte is typed while paused
and the picture stops changing. What does **not** park is the read loop: the
watcher's next key has to be read, or `q` while paused would be queued behind the
pause the watcher is pressing it to escape. So the surface stays live, nobody's
keystroke is swallowed, and the terminal is never left in a special mode.

## The seam's contract (`internal/lineedit/keysource.go`)

`KeyGate` is **installable, not implemented**: the editor carries a `nil`
interface by default and then calls nothing, so a surface with no owner behaves
byte for byte as it did before. Three methods, one question each:

| method | question | meaning |
|---|---|---|
| `GateKey(k) bool` | is this key yours? | `true` = consumed; the editor must not apply it. `false` = the owner does not want it. |
| `KeyUnclaimed(k)` | — | reported when the editor would ignore the key anyway (an Alt-modified rune, `KeyNone`), after `GateKey` declined. |
| `KeyRelease(k)` | — | reported when the editor **is** about to act on the key. |

**The triple is total and disjoint for every key of every read**, in arrival
order, and the release is announced in ONE place — above both the rune fast path
and the handler switch — which is what makes that property true rather than
described. `TestKeyGateIsANoOpWhenNoneIsInstalled` and the keysource tests pin it.

## The controls

| key | action |
|---|---|
| `space` | pause / resume |
| `←` / `→` | move the **cursor** one beat back / forward (a step moves the cursor only — the run never goes back: see `devdoc/chat-replay-step-meaning.md`) |
| `↑` / `↓` | jump `chatControlJumpBeats` (5) beats back / forward (same limit) |
| `?` / `h` | reprint the key line |
| `q` / `Esc` | leave the replay |

Their OSD lines go through the run's ONE notice channel
(`chat_notice_sink.go`), which is the destination that already knows whether the
run is its own film's producer: a plain replay prints them on the operator's
terminal, and a self-filming run keeps them **off the photographed screen** — in
the session's own `console.log`. That is why the key line is announced by the
driver (`run()`) and not by the constructor: the constructor runs *before*
`routeChatNoticesToConsoleLog`, so announcing there would print the keys onto the
terminal a film is photographing. (The first filmed acceptance run caught exactly
that; see the evidence below.)

## TWO POSITIONS — the model, written down because conflating them is the bug

The control layer holds two numbers, and they are different things:

- **`cursor`** — WHERE THE WATCHER WANTS THE RUN. Set by `←/→/↑/↓`, consulted by
  the driver before typing a beat. It **starts at the end of the recording**, so a
  replay nobody touches plays the whole thing, exactly as before.
- **`head`** — WHERE THE RUN ACTUALLY IS: the last beat the driver finished.
  Only the driver moves it, and the state lines report **it**, because a pause
  announcing the cursor would claim the run had reached a beat it had not.

They differ in the two states the watcher creates on purpose: **HELD** (cursor
behind head — the run stopped, and says so) and **BEHIND** (cursor ahead — the run
is still typing its way there).

**A hold happens at a BEAT boundary, never mid-line.** Cutting a turn in half
because the watcher stepped back into the middle of it would leave a half-typed
line on the surface — a line the recording never contained, which is the one thing
a replay may not show. A **pause**, by contrast, is checked inside the chunk loop
as well, so pausing on the third unit of a forty-unit line stops the line there:
that is what "pause" has to mean for a run being typed at you.

A **move lifts a pause** (`space` then `→` resumes). The arrows answer "put the run
HERE", and a layer that acknowledged an instruction it refused to obey would be
lying. A move that cannot travel — a jump forward while already at the end — says
`already at beat N of M` and reports the distance **actually travelled** (`0`),
because a jump that reported its nominal step while sitting still is the silent
no-op Rule 15 names.

## Files

| file | what it carries |
|---|---|
| `internal/lineedit/keysource.go` | NEW — `ReadOrigin`, `ReadOriginSource`, the `KeyGate` contract, `editorIgnoresKey`. |
| `internal/lineedit/editor.go` | `SetKeyGate`, `dispatchKeyGate`, `keyGateRelease`, `sourceOrigin`, `stampKeyOrigins`; the gate dispatch + release in the read's key loop. |
| `internal/lineedit/keys.go` | `Key.Origin`. |
| `internal/lineedit/inject.go` | `lastOrigin`, `noteOrigin` in both arms of `ReadEvent`, `ReadOrigin()`. |
| `cmd/agent/chat_replay_control.go` | NEW — the key map, the two positions, the state machine, the OSD lines. |
| `cmd/agent/chat_replay.go` | the `control` field, `beatAllowed`/`waitForCursor`/`advance`, the hold in the run loop, the layer built in `startChatReplay`, the gate installed + the keys announced in `run()`. |
| `cmd/agent/chat_replay_control_test.go` | NEW — the claims a unit test can decide (see below). |

## What the unit tests decide, and what they cannot

`cmd/agent/chat_replay_control_test.go` decides **which keys the layer claims**
(and which it refuses), **what each key does to the state the driver obeys**, and
**that a held beat stays held until the cursor moves**. Its central case offers the
same glyphs twice — once from the terminal arm, once from the injector's — and
requires the layer to claim exactly the first set; an implementation that switched
on the key code alone passes every other case in the file and fails that one.

What no unit test here decides is that a real terminal's space bar reaches this
layer: that is the read loop's behaviour, and it is exercised through the package
that owns the read loop (`internal/lineedit/keysource_test.go` drives the same
seam through a real editor and a real injector). The acceptance runs below are the
end-to-end half, on a real pty.

## Not Done

- **`--film` frames were not re-verified by pixel comparison against a pre-change
  baseline.** The exclusion was verified by two other means: the film's own
  `beats.tsv` (113 rows) carries **zero** OSD rows, and a vision read of the frames
  finds no playback text; the line is present in the run's `console.log`. A
  byte-for-byte frame diff against the pre-change binary was attempted and is not
  a usable signal — the frames differ in `at_ms` and in the amount of text already
  typed, because the two runs are two different real runs, not two renders of one
  state.
- **The jump lands on a beat but does not seek backwards through already-served
  model turns.** A step/jump back moves the cursor and stops the run there; the
  replay does not re-serve model requests it has already served. Re-serving is a
  larger change to the replay driver's request accounting and is deliberately out
  of this scope.
