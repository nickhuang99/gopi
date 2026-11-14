<!-- doc-class: historical -->
# The SGR-aware self-observation instrument (`devdoc/evidence/sgr-grid/`)

Task `01a0b974` (epic: *self-observation instrument, SGR-aware*).

## What this closes

The previous chat-UI task captured the screen two ways and found the gap: the
byte-exact instrument **dropped every SGR sequence** — "SGR changes colour, not
cells". Its own doc said so, and every colour/dim/bold claim in a capture was
therefore a screenshot impression, not a fact. A grid that discards SGR renders
a dimmed reasoning line, a coloured tool row and a plain paragraph *identically*,
so the instrument was blind to exactly the property those claims are about.

Now: **one committed command** produces both instruments over a scripted
fake-model run, and the grid half carries per-cell attributes.

```bash
devdoc/evidence/sgr-grid/capture.sh <label> [repo-root]
# e.g. the committed master baseline:
devdoc/evidence/sgr-grid/capture.sh before ~/workspace/gopi-sgr-master-baseline
```

**`repo-root` must be a checkout whose files are readable at that path** — the
default (this repo) always is. A linked worktree normally lists its *main*
repo's path too, but when its mount lives under a per-session mount namespace
(`/proc/<pid>/root/home/user/work/GoPi`) a sibling session cannot see that path,
so pass the worktree's own directory instead.

## The two instruments

| | instrument | proves |
|---|---|---|
| 1 | `termgrid.py` — real PTY, VT100 cell grid, **now with per-cell SGR** | exact columns *and* exact attributes: "columns 4-6 of row 12 are `fg=1`" is a fact |
| 2 | `screen.sh` — Xvfb + xterm + xdotool + `import` → PNG | what the terminal actually painted (fallback fonts, real colour, wide chars); read back with `vision_analyze` |

Both run the **same** script, the same mock and the same geometry, so the two
can be read against each other — which is what the baseline below does.

### Reading the attribute dump

`<label>-<shot>.grid.attr.txt` is the screen plus one attribute-run line per row:

```
26|RED-ON-DEFAULT and BOLD-DIM from the model.
  ~ 0-13 fg=1 | 14-18 default | 19-26 bold dim | 27-99 default
```

Every column of the row appears in exactly one run (`start-end` inclusive), so
"column 45 is red" is greppable; an untouched row reads as a single `default`
run; truecolour prints as `fg=rgb:<0xRRGGBB>` (SGR `38;2`/`48;2`).

The Go side (`internal/lineedit/attr.go`, `AttributedContents`) writes the same
shape for the unit tests, so one contract covers the emulator in the tests and
the emulator in the harness.

## The two-colour probe (the acceptance evidence)

The scripted model (`fakellm.py`, scenario `styled`) emits, inside its answer
text, one line of raw SGR:

```
\x1b[31mRED-ON-DEFAULT\x1b[0m and \x1b[1;2mBOLD-DIM\x1b[0m
```

Red vs default is therefore **distinguishable in the dump** — the property the
task asked to demonstrate.

`termgrid.py --selftest` is the same probe without the app, so the instrument is
proved even if a capture run goes wrong; `capture.sh` runs it **before every
capture** and refuses to capture with an attribute-blind emulator.

Red-proof (measured, both directions):

| emulator | `termgrid.py --selftest` |
|---|---|
| SGR recorded (this branch) | `0-13 fg=1`, `14-18 default`, `19-26 bold dim` → **exit 0**, `selftest OK` |
| SGR dropped, i.e. the OLD `sgr()` body (`return` first line) | `0-53 default` → **exit 1**, "no fg=1 (red) run … SGR is being dropped" |
| Go: `internal/lineedit/screen_attr_test.go` | `TestScreenRecordsPerCellAttributes` asserts `CellAttr(0,0) == {Fg: ColorRed}` and that it **does not Equal** the default cell next to it |

## Committed baseline (`before-*`)

Captured from a **clean master worktree** at `4aea3c76` (worktree
`~/workspace/gopi-sgr-master-baseline`, `git worktree add --detach … master`),
so it is the current-master "before" the epic's colour work lands.

- grid: `before-ui-{1-boot,2-answer-start,3-answer-done,4-quit}.{grid,grid.attr,raw}.txt`
- pixels: `before-screen-{1-boot,2-answer-start,3-answer-done,4-quit}.png` (1400×900)
- the run's own log: `capture-before.log` (its shot-hash table is the record below)

Each shot's name is the state the instrument OBSERVED before taking it (see
"shots are named for observed states"): `1-boot` = the banner is up (`waitfor
gopi chat`), `2-answer-start` = the answer's first line is on screen, `3-answer-done`
= its last line (`Done.`) is, `4-quit` = the app process exited.

The four independent readings of the *same* probe line agree — and none of them
is OCR:

| reading | evidence |
|---|---|
| grid dump, row 26 | `~ 0-13 fg=1 \| 14-18 default \| 19-26 bold dim \| 27-99 default` (`before-ui-3-answer-done.grid.attr.txt`, the attr line under screen row 26) |
| pixels (`PIL`, a xerox segmentation — **not** a glyph count) | `before-screen-3-answer-done.png`: 331 red-ish pixels (`r>150,g<90,b<90`), all within rows y=500-511, x from 4 to 128, and exactly one 12-px-tall row band — one text line's worth of red glyph ink, ×27 red cells. Zero red pixels in the other three shots. The x-extent is *consistent with* "14 cells" and does not by itself measure it: a pixel probe tells you a line is red, the grid tells you exactly which cells are |
| the RAW byte trace (grid-free truth) | `grep -ao $'\x1b\[[0-9;]*m' before-ui-3-answer-done.raw.txt` → exactly 4 SGR sequences: `[31m`, `[0m` ×2, `[1;2m` |
| vision read-back | "the line `RED-ON-DEFAULT and BOLD-DIM from the model.` is rendered in red, and within it the token `BOLD-DIM` appears in a bold/dim style" |

`2-answer-start` is the mid-stream cut: the answer's first paragraph is on
screen and the list has not arrived (`A short list:` with the cursor after it),
which is what its name claims and what the vision read-back of that PNG
independently reports.

**The instrument's headline observation about current master**: the app itself
emits **no SGR at all**. Per-shot SGR count in the raw byte streams:

| shot | SGR sequences in the byte stream |
|---|---|
| `before-ui-1-boot` | 0 |
| `before-ui-2-answer-start` | 0 |
| `before-ui-3-answer-done` | 4 — all four are the model's probe line, which arrives in the final delta |
| `before-ui-4-quit` | 4 — same line, still on the frozen screen |

So a 100×30 chat screen is entirely `default` cells, and the only coloured thing
on it is text the *model* wrote. That is the "before" the colour work is
measured against.

**A defect the instrument found on master (reported, not captured)**: on `/help
clear` the help panel and the live status row are painted onto the **same
absolute row** — the panel's prose and the row's text interleave
character-by-character in the dump, so no part of the panel is reliably readable
and no observation can back a "help panel is drawn" caption.

Measured on a clean master binary (`4aea3c76`), stable across two shots 3.4 s
apart (rows 26-29, 100×30):

```
26|opped and the display is wiped. "/clear" is the same reset (the chat
27|                                                                    REPL's spelling). Local and imme
28|diate — never a model turn.
29|deepseek-flash ·Clear the conversation and start fresh: the transcript the next turn seedsfrom is dr
```

Row 29 is the reserved status row: the help panel's closing lines and the status
row's text are both writing it. (`from is dr` — the panel's `…seeds from is
dropp…` and the row's `…ready` have already been overwritten by each other.) Row
27's text starting at column 68 is the same collision on the row above.

That is why the shot set has **no `-help` shot**: rather than commit a caption
the harness cannot verify, the collision is reported here as a finding for the
chat-UI work.


### The baseline is REPRODUCIBLE (measured 2026-09-19)

The claim "this instrument is deterministic and the baseline is real" is itself
only credible with an observation, so the committed set was re-captured three
times from a clean master worktree (`4aea3c76`) with the same script, mock and
geometry, and compared shot by shot (`determinism-3run.txt` — the raw table).

**6 of 8 artifacts are byte-identical across the committed baseline and all
three fresh runs** (4 grid shots, 4 PNG shots, 3 varying lines). A **fourth**
run on the committed HEAD (a fresh throwaway work dir, built from scratch,
`capture-committed-head-verify.log`) reproduced the whole baseline **8/8**: the
two sensitive rows landed on the committed variant that time, so nothing here is
a systematic drift — they are the two rows where the subject itself is
non-deterministic.

```
grid 1-boot         434dd8d449bc41fd 434dd8d449bc41fd 434dd8d449bc41fd 434dd8d449bc41fd
grid 2-answer-start 70c3ef8153259e0f 70c3ef8153259e0f 70c3ef8153259e0f 70c3ef8153259e0f
grid 3-answer-done  b02c4da55d1f203d b02c4da55d1f203d b02c4da55d1f203d b02c4da55d1f203d
grid 4-quit         8623331b20ac4146 8623331b20ac4146 bda474ac48d5b423 8623331b20ac4146
png  1-boot         07bc132b4c97833d 07bc132b4c97833d 07bc132b4c97833d 07bc132b4c97833d
png  2-answer-start c9f8634cdc774157 c9f8634cdc774157 bf3e81be23ff263c c9f8634cdc774157
png  3-answer-done  e905fc539cf0951d e905fc539cf0951d e905fc539cf0951d e905fc539cf0951d
png  4-quit         a67a0fbffcab568e a67a0fbffcab568e a67a0fbffcab568e a67a0fbffcab568e
```

**The two that vary are SUBJECT-side, not instrument-side**, and both are worth
knowing before reading an `after-*` diff:

* `grid 4-quit` — the app's shutdown writes `bye.` and the repainted prompt
  **concurrently**, so their order differs run to run. Measured in the raw byte
  stream: one run ends `…bye.` / `2Kyou>` and another `…2Kyou>` / `5Cbye.` — two
  interleavings of the same two writes, both faithful. (This is the "one writer
  per row" family of defect; here it costs a shot's byte-stability, not its
  caption.)
* `png 2-answer-start` — the shot is a **cut of the answer while it streams**, so
  which delta has landed at capture time varies (the two variants differ over
  18 876 px in the answer band, rows 424-511 — one more line of text, not a
  repaint).

Neither variant equals another shot's caption, so the distinctness gate stays
green: it is a lie-detector, not a determinism requirement. A byte-diff of a
later `after-*` capture must therefore be read on `1-boot`/`3-answer-done`
(rock-stable) and with these two rows in mind. Both variants are committed as
evidence (`repro-variant-quit-order.{grid,raw}.txt`, `repro-variant-answer-cut.png`),
so the claim above can be checked rather than believed.

## Requirements the script encodes (both measured — do not "simplify" them away)

The two the task briefing named are §1 (env scrub) and §2 (`xterm -u8`), with
the paste-burst rule at §5. §2b/§2c/§3/§4 are the ones measurement added, and
each one is a way the instrument previously reported a state it had not observed.

### 1. The environment MUST be scrubbed

A capture that inherited `DEEPSEEK_API_KEY`/`ANTHROPIC_BASE_URL` talked to the
**real API** (measured 2026-09-19: the "grid" run reasoned about memory rules
and fired `bash find /` at the root filesystem). The scrub list is one named
array in `capture.sh`:

* `-u` every key and base URL (`DEEPSEEK_*`, `ANTHROPIC_*`), so the local
  scripted server **is** the model;
* `GOPI_HOME` → a **throwaway** directory under the work dir, so a capture never
  reads or writes the operator's real `~/.gopi` (memory, sessions, ledger);
* `-u GOPI_TASK_REPORT_PATH/-OWNER/-EXECUTOR_SESSION_ID` — a capture is not a
  task run;
* `GOPI_HUMAN_TERMINAL=1`, `LANG=C.utf8`, `LC_ALL=C.utf8`,
  `TERM=xterm-256color`.

Throwaway trees live under `$HOME/workspace/_tmp/…`, never `/tmp` (Rule 22).

### 2. Pitfall: `LANG=C` gives mojibake → `xterm -u8`

The UI draws `·` and `⚙`; under a C locale those bytes come out as garbage in
the PNG, which reads as a UI defect and is not one. The script sets
`LANG/LC_ALL=C.utf8` **and** passes `xterm -u8` — either alone was not enough.

### 2b. Pitfall: the mock's own stream shape hides the state you want to shoot

The app's status row goes live only when the model's FIRST delta arrives, so a
mock that pushes its whole answer in two back-to-back SSE deltas leaves the
in-flight state existing for **microseconds**. Measured 2026-09-19: shots taken
at t+1 s/t+3 s/t+5 s after submit were all still `iter 0 · tools  ready` — the
state a "toolrunning" caption claims was never photographable, and the caption
was a lie no matter how long the harness waited.

`fakellm.py`'s `text_stream` therefore **drips** the answer one line per delta
with a gap (`FAKELLM_LINE_GAP`, default 0.4 s — set via `LINE_GAP`) and marks
`streaming` the instant the first delta is written. That is what a real
streamed answer does, and it is what makes "the answer has started arriving" an
observable fact rather than a race the instrument always loses.

The lesson generalises: when a state cannot be captured, check whether the
**subject** ever holds that state for the harness's measurement window before
blaming the harness's timing.

### 2c. Pitfall: shots are named for OBSERVED states, and the names must be true

A shot's filename is a claim ("`-2-toolrunning`"), and the claim is what every
later reader trusts. The committed baseline's shots are named for the fact their
own `waitfor` established:

| shot | the state the instrument waited for |
|---|---|
| `-1-boot` | the banner line `gopi chat` is on screen (grid) / the window is mapped (X11) |
| `-2-answer-start` | the answer's first line is on screen (grid) / the stub delivered its FIRST answer delta, `wait_mark 2 streaming` (X11) |
| `-3-answer-done` | the answer's last line (`Done.`) is on screen (grid) / the 2nd call CLOSED, `wait_mark 2 done` (X11) |
| `-4-quit` | the app process exited (grid `exit` op) / the xterm window is gone from the root window (X11) |

There is **no `-help` shot** in the committed set: the help panel is measured
interleaved with the live status row on the same rows (see "a defect the
instrument found on master" above), so no observation can back that caption.

The status row's own wording was tried as an anchor and **measured unusable**:
it read `ready` / `iter 0` in shots taken while the answer was already painted,
because the row's live state lags the answer text. An anchor that is weaker than
the claim it backs is not an anchor.

### 3. Pitfall: the X11 input path must not depend on INPUT FOCUS

Measured 2026-09-19, and it produced a capture in which **every keystroke went
nowhere**: this runs on a bare Xvfb with **no window manager**, so
`xdotool windowactivate` ALWAYS fails ("Your windowmanager claims not to support
_NET_ACTIVE_WINDOW") and the input focus is whatever it happens to be.
`xdotool type --window W` sends an `XSendEvent`, which xterm accepts **only
while its window actually holds the input focus**; when that focus did not stick,
nothing was typed, the stub was never called, and all four PNGs came out
byte-identical. `capture.sh`'s distinctness gate failed that run (correctly), but
the instrument had silently tested *nothing* — a whole-branch acceptance could
have been read off four copies of the same boot screen.

A/B on a bare Xvfb, a fresh display per case, every case sending its Return:

| focus | mechanism | landed? |
|---|---|---|
| none | `xdotool type --window W` (XSendEvent) | **nothing** |
| none | `xdotool type` (XTEST, no `--window`) | **landed** |
| held | both | landed |

XTEST delivers to the window **under the pointer** when the focus is
`PointerRoot` (what a WM-less X server uses), so `screen.sh` now moves the
pointer inside the window and routes **every** keystroke and Return through XTEST
(`windowfocus` is still attempted, but only as an optimisation). Focus is no
longer a dependency.

Symptom → cause, so the misattribution does not repeat: the failure surfaced as
`screen.sh: timed out waiting for request 2 state=streaming (mark='none')` 60 s
later — which names the *turn*, not the *typing*. `screen.sh` now asserts
`wait_mark 2 arrived` **immediately after typing the prompt** and exits with "the
typed prompt never reached the model … the input path did not land".

### 4. Pitfall: a fixed-sleep shot anchor photographs the WRONG state

The X11 instrument's shot captions are state claims ("the tool is running", "the
answer"), and its first version anchored them on `sleep 4` / `sleep 5` guesses.
A sleep anchors on the **clock**; the caption claims a **state** the clock never
promised. Measured 2026-09-19 on the first baseline: `xdotool type --delay 150`
needs ~7 s for a 47-char prompt, the stub held its 2nd call open for only 3 s,
so by the time shot 2 was due the hold had expired — `-2-toolrunning` and
`-3-answer` came out **byte-identical** (`sha256 1c242800c7ff98ff…`), one of them
mislabeled, and nothing in the harness noticed.

The fix is the instrument's own rule (anchor on an observed fact, never a guess):

* the stub writes its request index and state to `$FAKELLM_MARK`
  (`2 arrived` / `2 done`) — an observation, not a clock;
* `screen.sh`'s `wait_mark <n> <state>` blocks on that file before each shot;
* the stub's hold is a named const with an env override (`FAKELLM_HOLD_SEC`,
  default 3.0 s, `HOLD_SEC` in `capture.sh` = 14 s) — long enough for the typing
  to finish inside the in-flight window;
* a PNG is a **snapshot**: the wait happens once, no retry loop (a retry would
  photograph the state that replaced the one wanted);
* `capture.sh` now hashes all four shots on **both** instruments and **fails the
  run** if any two are identical — different captions cannot be the same pixels.
  The hashes are logged either way, so a reviewer checks the record rather than
  trusting it. (RED-proof: the same comparison run against the committed v1
  baseline fails on its real `-2-toolrunning`/`-3-answer` pair, both
  `sha256 1c242800c7ff98ff…`.)

### 5. Pitfall: a whole-line write arrives as a PASTE BURST

Writing a line in one go makes the editor's paste-suppression window eat the
submit CR as pasted content: the line never runs and the harness silently tests
nothing (measured 2026-09-19 — a slash-command probe read as "the output never
printed"). Both instruments type **character by character** and send Enter as a
separate event after a pause:

* grid: `termgrid.py`'s `type` op writes one char per `BURST_GAP`, `submit`
  waits out the window first;
* X11: `xdotool type --delay 150` + `sleep 1.2` + a separate `Return`.

## What this instrument is NOT

* Not a full VT emulator: CUP/HVP/CUU/CUD/CUF/CUB/EL/ED/SGR/DECSTBM + CR/LF/BS/
  TAB and the alternate screen, nothing else. It is the subset the chat UI emits.
* Not pixel truth: use the PNG (and `vision_analyze`) when the question is what
  a real terminal painted. Use the grid when the question is *exactly where* and
  *exactly which attributes*.
* No device load anywhere (Rule 27): Xvfb is a virtual framebuffer, the model is
  a local HTTP stub, and the only scripted delay is the stub's one deliberate
  hold (14 s) so a capture can land mid-turn. Shot timing is NOT sleeping: every
  shot waits on an observed fact (a `waitfor` on the emulated screen, or a mark
  the stub itself wrote) — see pitfall 3.

## Not done (honest)

* The `after-*` half of the baseline: this task only captures and commits the
  **current-master** baseline. `capture.sh after <your-checkout>` produces the
  comparison pair for whichever branch lands colour work.
* `fakellm.py` is committed as a **copy** under the evidence dir (the original
  lives in `~/workspace/gopi-chat-ui-debug-evidence/evidence/`), so this
  directory is self-contained. The copy gained the `styled` scenario; the two
  will drift if edited separately — the evidence-dir copy is the one this
  instrument uses.
* The emulator does not model reverse video (SGR 7) or a real 256-colour
  palette table; both are recorded/ignored faithfully enough for the UI's own
  output, and a future colour layer that uses them must extend `Attr` (one file,
  one place) rather than work around the dump.
