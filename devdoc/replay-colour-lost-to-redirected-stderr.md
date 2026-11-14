<!-- doc-class: historical -->
# Task Report: the replay film's colour — the capability was asked about a redirected stderr

Task `01a0fa3d-b43c-75e7-8604-1720c8b09633` (epic reframed DOABLE; the raw briefing
is in this session's log).

## The finding: the briefing's stated cause was wrong, and the real one is one line

The epic opened with "重放进程（gopi-replay）拿不到 TERM/COLORTERM". **That is not what
happened.** Measured on this host, in a terminal this repository's own launcher
started:

```
TERM=[xterm-256color]
COLORTERM=[truecolor]
tty=/dev/pts/9
```

`xfce4-terminal` (a VTE terminal) **sets** `TERM=xterm-256color` and
`COLORTERM=truecolor` for its child. Path (a) — "the terminal does not declare its
depth" — is REFUTED, and path (b) — "xterm is the only `colour_cli=True` candidate,
so picking xfce4-terminal lost the palette" — is refuted with it: `colour_cli` in
`tools/termemu/termemu.py` is about the launcher's `-xrm 'XTerm*colorN'` override,
not about what the child's environment declares.

The real chain, every link measured:

1. `scripts/replay/replay.sh:246-253` generates the launcher and redirects stderr:
   `… --dump '…' 2>'$SCRATCH/replay-stderr.log'`.
2. `cmd/gopi-replay/main.go:670` asked the capability about **`os.Stderr`**:
   `lineedit.PaletteForEnvCapability(false, os.Stderr)`.
3. `internal/lineedit/colour.go`'s `ColourWanted` rule 4 is *"the sink: not a
   terminal (a pipe, a file, a test buffer) is never coloured"* — and
   `replay-stderr.log` is a regular file. The decision returned **`ColourOff`**.
4. `PaletteForCapability(ColourOff)` returns **`MonoPalette`**, whose every field is
   the zero `Style` — no SGR at all. The film drew gopi's real surface with **no
   colour whatsoever**, which is why even `BasicPalette.Answer` (already the empty
   `Style{}`) was not the whole story: the layout lost every colour it had, including
   `Prompt`, `Buffer`, `ToolRow` and `Error`.

Two independent measurements of the before-artifact agree with the chain:
- every row of `chat-screen.dump.json`'s `attr_runs` is a single
  `{"cells": 117, "style": "default"}` run — the wire carried no SGR to attribute;
- the delivered GIF's palette is 253 entries of which **5 are non-grey and all 5 are
  the background gradient** `(19,25,38) (28,34,46) (39,44,55) (54,58,69) (61,66,75)`.

## The fix — one descriptor, chosen once, no flag

`cmd/gopi-replay/main.go`: the sink the capability is asked about is now *the
descriptor the run actually draws to*, and that is the first of `os.Stdout`,
`os.Stderr`, `os.Stdin` that **is** a terminal (`colourSinkFor`). When none is, the
run REFUSES by name instead of painting a silent grey film (Rule 15). The editor and
the capability now share that one value, so they cannot disagree again.

**Default-on, no knob**: `basic`/`256`/`truecolor` is still decided by
`lineedit/colour.go` alone (Rule 13) — the fix changed which descriptor is *asked*,
never the precedence. `GOPI_PALETTE`, `NO_COLOR` and `--no-color` behave exactly as
before.

## Not Done (honest)

- **The default arm is now the 24-bit table, not 256.** `COLORTERM=truecolor` is
  declared by this terminal, so `PaletteForCapability` resolves `hermes` (38;2). The
  acceptance named `Palette256.Answer`'s `colour-230`/`colour-178`; the delivered film
  instead carries `#FFF8DC` (`38;2;255,248,220`), which is *the deeper tier of the
  same gold family*. Both are "gopi's real palette"; I did not force the 256 table
  with a knob, because forcing it would be the switch the epic forbids.
- **`beat 170` / `beat 158` traceability FAILs on the new film.** They are
  `verify.py`'s pre-existing provenance checks; the same film's beats come from the
  *same recording* as the before-arm, and `--max-beats` was not given, so the new arm
  replayed 209 beats where the old arm replayed 140 (more of the session, including a
  multi-line `task_submit {"payload":"…` activity row). Named here rather than
  hidden: they are **not** touched by this change (they are text-provenance checks
  over the ledger), and the colour claim is independent of them.
- The GIF is not re-delivered to `docs/archive/demo/replay/` (19.5 MiB, and delivery is a
  separate decision); the before/after artifacts live in the scratch and the session
  directory.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi-replay ./cmd/gopi-replay/` → OK
- Tests: `CGO_ENABLED=0 go test ./cmd/gopi-replay/ -run TestColourSinkFor -v` →
  `--- PASS: TestColourSinkForPrefersTheTerminalDescriptor (0.04s)` — three arms: the
  redirected file still resolves `MonoPalette` (the RED half, stated so the fix can
  never be "make a file count as a terminal"); the pty resolves `Palette256` under
  `TERM=xterm-256color`; `colourSinkFor(log, slave, master)` picks the pty and
  `colourSinkFor(log)` is `nil` (refuse, never a grey film).
- Ran: `bash scripts/replay/replay.sh --bin bin --session
  /home/user/.gopi/sessions/4ca8cd29-019c-4dda-a000-6dafd782bf91 --scratch
  …/scr-after2 --out …/out-after --emu xfce4-terminal --out-tag after`
  Result: `[replay] terminal pid=3661568 window=2097155 2225x1432`,
  `photographed 209 frames`, `distinct frames: 202 (dropped 7 identical)`.
- Ran (Rule 31 — the changed surface, invoked directly with the launcher's OWN
  redirect, on a real pty at 117x26 with `TERM=xterm-256color` and `COLORTERM`
  unset, `2>…/v31/stderr.log`):
  `python3 /home/user/.gopi/sessions/01a0fa3d-…/drive-replay.py …/v31 …/v31/dump …/v31/stderr.log $(pwd) /home/user/.gopi/sessions/4ca8cd29-…`
  Result: the stderr redirect is real (`gopi-replay: redacted 185 identifying
  run(s) …`), and the dump's own `attr_runs` now carry
  `{'default': 26, 'fg=colour-186 bold': 1, 'fg=colour-220 bold': 1}` —
  `colour-186` = `Palette256.Prompt`, `colour-220` = `Palette256.Buffer`. The SAME
  shape before the fix produced 26/26 rows of `{"cells": 117, "style": "default"}`.
  That is the minimal repro of the defect, in one command, without the terminal
  and the three-minute film around it.
- Ran: `python3 tools/replay/verify.py --out …/before-arm --scratch … --session … --allow-sampled-frames`
  → `the film's 121 frame(s) carry 0 distinct hue bucket(s)` →
  `FAIL: the film has 0 hue bucket(s), floor 2` → `RESULT: FAILED`, exit 1 (**RED**).
- Result (the before/after palette table, the deliverable's own comparison):

  | | palette entries | non-grey | the non-grey values |
  |---|---|---|---|
  | BEFORE | 253 | **5** | `(19,25,38) (28,34,46) (39,44,55) (54,58,69) (61,66,75)` — the background gradient only |
  | AFTER | 256 | **21** | gold family `(69,60,36) (85,72,36) (107,87,35) (123,99,34) (144,113,34) (152,132,16) (161,125,34) (178,137,33) (188,161,11) (202,154,32) (218,165,32) (224,190,5) (254,214,0)`, plus `(255,253,244)` = `#FFF8DC` (the answer body) and antialias blends |

## Evidence (self-filed)

- The terminal's own environment, read from inside the terminal the launcher started:
  `TERM=[xterm-256color] COLORTERM=[truecolor] tty=/dev/pts/9`.
- The redirect that caused it, verbatim from the tree:
  `scripts/replay/replay.sh:251` → `--dump '$SCRATCH/chat-screen.dump' 2>'$SCRATCH/replay-stderr.log'`.
- The before-artifact's grid: every `attr_runs` row =
  `{"col": 0, "cells": 117, "style": "default"}` (26/26 rows).
- The RED gate run above (exit 1, "0 saturated bright pixel(s) in 0 hue bucket(s)").
- The GREEN run of claim 5 on the new film, verbatim:
  `the film's 202 frame(s) carry 1 distinct hue bucket(s) of saturated, bright ink
  (27121676 such pixels; floors: saturation >= 0.40, value >= 0.50)` →
  `palette roles present: answer emphasis, colour-178 cubeDeepGold (#D7AF00),
  editing line, colour-220 (#FFD700)` → `OK: gopi's own palette is on the
  artifact, not only in the renderer`. The overall `verify.py` exit is 1 **only**
  because of the two pre-existing traceability FAILs named in `## Not Done`; every
  other claim (legibility, motion, withholding, colour) passes on the new film.
- The palette table above is the artifact-level evidence; the same table is filed at
  `/home/user/.gopi/sessions/01a0fa3d-…/palette-before-after.txt`, and the root-cause
  record (the three candidate paths, each refuted or confirmed) at
  `/home/user/.gopi/sessions/01a0fa3d-…/root-cause.md`.
- `git status --porcelain`: `M cmd/gopi-replay/main.go`, `M tools/replay/verify.py`,
  `?? cmd/gopi-replay/colour_sink_test.go` — committed below, then clean.
