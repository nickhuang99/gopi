<!-- doc-class: normative -->
# Film terminal selection — which emulator a film runs inside

**Status**: implemented, measured, on `fix/chat-film-terminal-selection`.
**Commit scope**: `internal/shot/terminalregistry.go` (new), `internal/config/film_terminal.go`
(new), `cmd/agent/chat_film.go`, `cmd/agent/cli_chat.go`, `internal/config/{config,settings,flags}.go`,
`CONFIG.md`, `devdoc/DOC-INDEX.tsv`.

## The problem

`internal/shot/terminalterm.go` carried `FilmTerminalBinary = "xterm"` and one hard-coded
flag protocol (`-geometry WxC`, `-e ARGV`, `-T TITLE`, `-l -lf FILE`). The terminal a film
is photographed in was therefore **not selectable at all**, and the operator's desktop is
not xterm's: on peer1 it is `xfce4-terminal`, so a film showed a terminal he never sees.

The instruction that produced this task was explicit about the shape of the fix: *"keep
xterm as the default without evidence-driven change"*, and the tempting smaller fix —
`strings.Replace("xterm", "xfce4-terminal")` — is wrong, because the two terminals do not
share a launch protocol.

## What was measured (2026-10-03, private Xvfb, `peer1`)

Every fact below is an observation from this run, not a reading of a manual.

| terminal | geometry request | reported by the child (`stty size`) | window(s) mapped under one pid |
|---|---|---|---|
| `xterm` | `-geometry 117x26` (two argv slots) | 26 117 | one real window |
| `xfce4-terminal` | `--geometry=117x26` (one slot, `=`) | `24 80`, then `26 117` for 117x26 | **three**: the real window (1186x521), a 10x10 `Xfce Terminal` placeholder, a 1x1 auxiliary |
| `gnome-terminal` | `--geometry=80x24` | — (never measured; see below) | one 10x10 `gnome-terminal` placeholder |

Three consequences, each of which the implementation had to answer:

1. **The exec protocol differs.** xterm runs the command after `-e`; xfce4-terminal runs it
   after `-x` (argv, no shell, no `--` needed — verified with `echo`). Writing `-e gopi chat …`
   into xfce4-terminal is not a synonym, and `xfce4-terminal -e "sh -c …"` fails outright
   (`Unknown option "-c"`), so a one-string command spelling is not portable either.
2. **The geometry unit happens to agree, and that is a MEASUREMENT, not an assumption.**
   Both take cells; only the argv join differs. This is what made xfce4-terminal a drop-in
   for the existing pipeline (the film's layout is drawn for `--cols`/`--rows` and the
   terminal must be that size).
3. **`gnome-terminal` cannot do it here.** Its binary does not draw the window: it asks the
   D-Bus-activated service `org.gnome.Terminal` to draw it, and that service uses the display
   **it** inherits, not the private display a film run owns. Two measured outcomes on a
   private Xvfb:
   - no session bus reachable → exit 1, `Error constructing proxy for org.gnome.Terminal: … Timeout was reached`, **no window mapped**;
   - a session bus present → only its own 10x10 placeholder window, never a real one.
   Neither is a film of this run's surface. It is therefore a registry row **with a refusal**,
   not a supported terminal.

## The design

**A terminal is a REGISTRY ROW, not a constant** (`internal/shot/terminalregistry.go`).
One `FilmTerminalSpec` per terminal carries its binary, geometry flag/join, exec protocol,
title flag, session-log flags, its window-selection rule and — when it has one — its refusal.
`StartFilmTerminalSpec` executes a row and never branches on a terminal's name again, so
adding a terminal is a row. `FilmTerminalBinary` survives as a deprecated alias of
`FilmTerminalDefault` so no caller re-types `"xterm"`.

**The knob has four channels and ONE reader.** `shot.ResolveFilmTerminal` implements
`--film-terminal NAME` > `GOPI_FILM_TERMINAL` > settings `film_terminal` > the built-in
default, returning which channel decided. The knob's three names live in exactly one package
(`internal/config/film_terminal.go`) because `internal/shot` imports `internal/config` and not
the reverse — the alternative was two spellings of `GOPI_FILM_TERMINAL`.

Both CLI spellings reach that one reader through the mechanism `--chat-ideal-geometry` already
uses: `config.BootstrapEnvFromArgs` hoists the **pre-verb** form and `PublishFilmTerminalFlag`
writes the env name; the chat verb's own parser publishes the **post-verb** form through the
same writer. One spelling, one writer.

**Window selection is three clauses, and the third one is new**
(`windowForSpec`, rule `pid+title+measured-rect`): the pid (identity, `_NET_WM_PID` read against
the kernel's process table), the **title** (xfce4-terminal maps three top-levels under one pid —
pid alone photographs the placeholder), and a **real rectangle** (`filmTerminalMinWindowPixels`,
because the placeholder is 10x10 and an unmeasured window has no area).

**A refusal is a first-class outcome.** An unknown name names itself, the reason (exact names,
no PATH lookup, no substring match) and the usable list; a *known but unusable* terminal names
its own reason. Nothing falls back to the default.

## The refinement the first run forced

The first version made a missing **session log** a hard refusal — xfce4-terminal has no `-l/-lf`
counterpart. That was wrong in the most expensive way: the operator's own terminal is exactly
the one without it, so the feature would have been unusable on the machine that asked for it.

The rule that replaced it: the log is **evidence**, the frames are the **deliverable**, so a
terminal without the channel still films, and the missing log is **announced**
(`SessionLogAbsentNotice`) and `evidenceHint()` stops every report from naming a `producer.log`
that this run never wrote. Leaving an empty file unexplained would have read as *"the producer
printed nothing"* — the wrong conclusion to leave behind.

## Not done / honest gaps

- **The `12–13 lost frames with VTE` claim from 2026-10-02 was NOT reproduced in this run.**
  The xfce4-terminal film delivered **162 frames from 162 drawn beats** (session
  `aa0d772f-fc2e-4ac5-91af-b03d48a21e2d`), i.e. **zero loss** under the Go driver, which
  contradicts the earlier shell-harness measurement. `#7487` (in memory) records the original
  claim; this run could not confirm it, and the difference is plausibly the driver (settled-grab +
  per-beat ledger + acknowledgement) versus the shell harness. It is reported as **unresolved**,
  not as refuted: one session id at this revision is not a falsification. One frame of the 162
  never reached the settled criterion (`settled=no`) and is delivered **marked as such** — the
  driver's own distinction between "a still surface" and "a lost repaint".
- **`gnome-terminal` has no working path**, only a refusal. Preparing a session bus + a window
  manager for its service is an operator prerequisite (Rule 35 — this driver starts no daemon
  and adds no dependency). If a future task wants it, that is the shape: bus + WM + then confirm
  the service draws on the private display.
- **No `konsole`/`rxvt`/`terminator` rows.** Each is measured work (protocol + window rule), and
  an unmeasured row would be a claim nobody checked.
- The `xterm` **default was unchanged at the time of this document** (2026-10-03): every
  measurement this pipeline's arithmetic rests on (the settled grab, the frame-per-beat ledger,
  the legibility floor) was taken with it. **Superseded 2026-10-04**: the operator ruled that a
  CJK surface is never filmed in xterm (its default font is the X11 core bitmap `fixed` 6x13,
  ISO8859-1 only, so Chinese films as boxes), so `FilmTerminalDefault` is now `xfce4-terminal`.
  The *measurements* recorded here still stand as the xterm row's baseline — the default it was
  taken under does not. See `devdoc/film-cjk-default-terminal.md`.

## Verification recipe (reusable)

```bash
# a session with at least one assistant turn is required — a replay of a session that
# carries no model response has nothing to film, and says so.
SID=<session-with-an-assistant-turn>
./bin/gopi chat --replay "$SID" --film /some/dir --out /some/dir/x.gif --film-terminal xfce4-terminal
# → "[gopi film] terminal xfce4-terminal (from --film-terminal / GOPI_FILM_TERMINAL) at 117x26 cells"
# → "[gopi film] terminal window <id> ("gopi film <dir>") area 1186x521+0+0"
# → frames.tsv rows == drawn beats
```

The window area is the proof the **right** window was photographed: 1186x521 is the 117x26
cell grid, where the placeholder would have been 10x10.
