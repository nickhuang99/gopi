<!-- doc-class: historical -->
# devdoc: `gopi ui_state` — one call to read a live `gopi ui` screen state

**Task:** 01a0d3373e7379ecbdcb1a60ca1bbe9d
**Branch:** `task/01a0d3373e7379ecbdcb1a60ca1bbe9d`
**Builds on:** the self-observation dump (`internal/lineedit/screendump.go` +
`internal/uiclient/dump.go` + `gopi ui --dump-screen`), delivered on
`task/01a0d2ee6d1b781faad3803b60a4e302`.

---

## 1. The problem this closes

The dump gave a surface the ability to say what it painted. It did not give an
agent the ability to **ask**. Between "a file exists somewhere" and "here is the
current state of the `gopi ui` I mean" sat four questions nobody answered:

| question | the naive answer gets it wrong because |
|---|---|
| *where* is the dump? | the path is resolved at start time (flag > env > session dir > scratch class), so the layout is knowledge the reader must already have — and every caller re-deriving it is the drift Rule 13 exists to stop |
| *whose* screen is it? | the record names a pid, and a pid is recycled within minutes; without a second, verifiable fact the reader attributes a screen to a process that may never have painted it |
| *which* one, when several run? | the operator's `gopi ui --tools` and the agent's own surface are two processes with two dumps — picking the first found is a silent substitution |
| is it even *current*? | the sink is throttled (100 ms) and rewrites only on change, so the mtime proves nothing; a file whose writer is gone is a snapshot, and reporting it as the current screen is a false claim about the present |

`gopi ui_state` answers all four in one call, and — when it cannot — it
**refuses** with the information needed to resolve the refusal.

## 2. Shape: a CLI subcommand, not a tool

Rule 3's list of 17 agent tools is untouched. The verb is `ui_state` — the
TOOL-shaped underscore spelling (the `screen_capture` / `vision_analyze`
convention, devdoc/cli-naming-convention.md), so an agent reaches it through the
`bash` tool it already uses, and the word is byte-identical to the tool-shaped
surface a session log would name. `cli_completion_exceptions_test.go` carries it
as a *deliberate, reasoned* exception to the dash-casing convention, next to the
three tool-word commands.

## 3. The identity check — why (pid, start ticks) and not a path

`internal/uiclient/dumpread.go` reads the dump's provenance record and then
re-derives the same fact from the kernel:

```
record pid=4711 started=<RFC3339>     (what the surface wrote)
/proc/4711/stat  field 22             (what the kernel says, in ticks since boot)
  -> boot = now - /proc/uptime; start = boot + ticks/100
```

The two instants are derived independently — the writer records its own start
instant, the reader derives it again — so they agree only if it really is the
same incarnation. Consequences, each deliberate:

- a **gone** pid → `exited` (the dump is the last frame, and the verb prints a
  fixed sentence saying so);
- a **live but different** incarnation → `foreign` ("the pid has been recycled"):
  the reader does not touch that process and does not call its screen current;
- an **unreadable** /proc → `foreign`, never `live`: "I could not tell" must
  never authorise the strong claim (the same conservative one-way rule the
  sandbox reclaimer applies to a delete).

**`DumpStartSkew` (2 s) is not slop.** The instant is computed twice from
different observations, and `/proc/uptime` itself resolves to 10 ms, so an
exact-equality check would make live dumps read `foreign` — a check that always
fails answers nothing. A recycled pid is minutes to hours away, so the window
still separates the two cases it exists to separate. The writer changed too:
`uiArmDump` now stamps the identity with the kernel start instant
(`uiclient.ProcessStartTime`) instead of the wall clock, and a failure to read it
is reported rather than papered over with a fabricated stamp.

## 4. Ambiguity is a refusal, and the refusal is useful

`SelectLive`: one live candidate → it; several → `*AmbiguityError` listing **pid,
target, started and path per candidate**; none live but exactly one dump → that
dump *with its exited verdict* (there is still an answer to "what did it paint");
several dead dumps → the same refusal (several screens are still several
screens); nothing → `ErrNoCandidates`.

The exit codes make the three outcomes distinguishable from a shell:
`0` read, `1` no dump / bad flag, `2` ambiguous. `--pid` is the resolving route,
and a `--pid` that matches nothing **never** falls back to the one dump the scan
found — a silent substitution is exactly how a caller ends up asserting on a
screen it never asked about.

## 5. What every output carries

- **The grid notice, first**: `ScreenGridNotice` — "an interpreted cell grid (not
  bytes, not pixels) …". It is the first sentence of the description in both
  renderings, because every wrong conclusion about this artifact starts from
  reading it as pixels (a glyph question) or as bytes (a sequence-order question).
- **All 8 blind spots**, in text (`blind_spots_begin … blind_spots_end`, part of
  the dump's own rendering) and in JSON (`blind_spots`, emitted from the
  representation itself via a throwaway 1×1 grid — not from a list typed into the
  CLI, so the list cannot drift from the grid it describes). They are present on a
  REFUSAL too, when no dump was read at all.
- **The exited diagnosis** as a fixed sentence (`uiclient.ExitedNotice`), so the
  claim is made the same way every time.
- **The unpainted diagnosis** (`uiclient.UnpaintedNotice`) whenever the sink's own
  frame counter is zero — §6. It is emitted *before* the grid, so a reader that
  quotes only the head of the output keeps the qualification attached to the thing
  it qualifies; `--json` carries both the sentence (`unpainted_notice`) and the
  counter itself (`frames`), so a machine caller detects the condition without
  parsing English.

## 6. The empty grid, and why it needed its own diagnosis

`frames=0` is not a corner case, and it cost this delivery a false reading
before it was handled. MEASURED: the first E2E run of this feature read a dump
whose provenance line said `frames=0 dumps=0` and whose every grid row was
blank, and the verb — correctly, as written — rendered it as an ordinary
screen. Nothing in the report would have distinguished it from "the surface is
showing an empty grid", which is a claim about the product. The re-run of that
same harness with its own deadlock fixed reads the product's `frames=4 dumps=1`
(§9) — i.e. the defect was in the harness's read window, and the diagnosis
below is what let the two states be told apart instead of both being reported
as "a screen".

Three ways to reach that artifact, and they are not the same fact:

1. the surface has not painted yet (the constructor installs an empty state at
   start-up, before the TUI draws);
2. the surface's panel was **erased as it quit** — bubbletea treats Ctrl+C as an
   abort and clears its panel on the way out, so the `frames=0` file the
   aborting run leaves is a *former* screen;
3. every write to the file failed (the sink records that error on its own
   line; the reader now carries it as `sink_error`).

The reader therefore does not let the grid speak for itself: it surfaces the
sink's own counter (`Candidate.Frames` / `Candidate.Unpainted()`), carries
`UnpaintedNotice` on the output, and `--json` exposes `frames`/`dumps`/
`sink_error` per candidate. A caller asserting layout must treat an unpainted
dump as **no observation** — which is exactly what the E2E harness now checks
before it claims anything about layout (§8, the live-vs-after-exit comparison).

This is the same distinction `ErrNoCandidates` already makes for "nothing to
observe": **"the screen is blank" and "nobody painted" are different facts**, and
only one of them is about the product.

## 7. Reading the twin, not the text

A dump exists in two renderings: the text file (what a human reads, what a
report quotes) and the `.json` twin (the same observation, structure intact).
`ReadDump` takes the provenance record from the text dump and the *screen* from
the twin. The alternative — parsing the screen back out of the text rendering —
would be a second parser kept in step with a renderer by hand, which is precisely
what the twin exists to avoid. A pair that is missing, or that names two
different pids, is reported `unreadable` rather than half-read.

## 8. Tests (all zero-pixel, zero-vision)

| file | what it pins |
|---|---|
| `internal/uiclient/dumpread_test.go` | live vs exited, recycled-pid `foreign`, unreadable-is-not-an-empty-screen, **unpainted-is-not-an-empty-screen** (the `frames=0` regression guard, and its negative: a painted dump must NOT be flagged), ambiguity refusal listing both, single-candidate non-ambiguity, `--pid` miss without substitution, both scan roots, the `.json` twin not seen double, field-named record parsing |
| `cmd/agent/cli_uistate_test.go` | the flag grammar, the refusal-then-answer lifecycle through `runUIStateSubcommand`, the grid notice + 8 blind spots in both renderings, text-vs-JSON consistency of the same observation, `--list` never refusing, a missed `--pid` never substituting, the unpainted notice in BOTH renderings and its ABSENCE on a painted surface |

Fixtures are built by the product's own sink (`NewScreenDumper`), never by a
hand-typed copy of the format: a fixture that spelled the record line itself
would keep passing after the format changed.

Two of the suite's own gates fired at me and were obeyed, not baselined:

- `sessions_dir_name_test.go` refused a `"sessions"` literal in the new reader →
  the sessions root now comes from `config.SessionsDirName`.
- `screen_assertion_gate_test.go` flagged `dump_test.go`'s `Writes()` waits as NEW
  violations **because my own test used the same file's helper** — the flagged
  lines are pre-existing (verified against master `2ed9800a`, where the gate is
  green), so nothing was baselined; the ratchet was left untouched.

## 9. The painted-grid evidence (the re-run)

The first harnessed E2E run kept a `frames=0` dump (§6). The harness's own
deadlock was the cause, and it is fixed — one harmless key at 20 s makes
bubbletea paint (its first frame fires on an EVENT), and the quit key is `q`,
never Ctrl+C. The re-run, on the SAME script, reads `frames=4 dumps=1` and its
five layout assertions pass (`devdoc/evidence/ui-state/ui_state_e2e.out`):

| # | assertion | the pixel path cannot say it because |
|---|---|---|
| A1 | header at cell row 0, cols 0..43, byte-exact (`gopi ui → http://127.0.0.1:<port>  (tools off)`) | a pixel row is a scanline of glyph pixels; it has no column index and no code points |
| A2 | input box at row 28 and status line at row 29 of a 30-row grid (i.e. `rows-2` / `rows-1`), transcript short | pixels show ink near the bottom, not WHICH grid row |
| A3 | every grid row is exactly 100 CELLS | cell count ≠ pixel width: the same 100 cells span different pixel widths per glyph |
| A4 | `cursor row=29 col=0`, `scroll_region top=0 bottom=29`, `cols=100 rows=30` as FIELDS | a pixel path can find a caret by colour and has no scroll-region concept |
| A5 | all 8 blind spots present in the tool's own output | a pixel carries no statement of what it is blind to |

A3 is not vacuous: the run's own rows carry the CJK arrow `→` and the middle
dot `·`, so a byte count and a cell count genuinely diverge on those rows.

**The ambiguity refusal, on two REAL live surfaces**
(`devdoc/evidence/ui-state/ambiguity_probe.sh`, output `ambiguity.out`): two
`gopi ui` processes in one profile, each with its own dump → `ui_state` exits
**2**, names both pids, targets and dumps, and `--list` still answers (exit 0).
That is the difference between a usable refusal and "ambiguous".

---

## 10. What this does NOT do (honest)

1. **Two dumps from the same pid are not distinguishable by `--pid` alone** —
   the identity is (pid, started, target), and `--pid` matches the pid. Two
   surfaces of one process (the same engine serving two targets) both carry that
   pid, so `--pid` returns the first in scan order and `SelectLive` still refuses
   without it. Naming the target on the command line is the obvious next field
   (`--target`), and it is not in this delivery.
2. **Discovery does not know about a dump written to an explicit path outside the
   two roots** (`--dump-screen=/somewhere/else`). Such a dump is only reachable by
   reading it directly; the verb prints the roots it scanned when it finds nothing.
3. **The identity is Linux-specific** (`/proc/<pid>/stat`, `/proc/uptime`), like
   the rest of this project's process machinery; there is no portable fallback and
   none is attempted.
4. **A clean quit wipes the panel.** bubbletea erases its own panel on exit, so a
   dump read after a *clean* exit can legitimately show an empty frame; the verb
   diagnoses the *process* state ("the target has exited cleanly") and does not
   claim the grid content is meaningful in that case. Reading DURING the run is
   what the E2E evidence does. **An ABORT (Ctrl+C) reaches the same place earlier**:
   the panel is erased before the sink's last write, leaving `frames=0` — §6's
   diagnosis, and the reason the harness waits on the artifact's frame counter
   rather than on a fixed sleep.
5. **Discovery matches a dump to a *process*, not to a *screen the agent asked
   about*.** The verb answers "give me the state of the `gopi ui` running now",
   and with exactly one live surface that is well-defined; it does not carry a
   session id or a conversation identity, so an agent driving two surfaces must
   resolve them by `--pid` from `--list`.
