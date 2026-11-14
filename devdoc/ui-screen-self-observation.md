<!-- doc-class: historical -->
# Task Report: `gopi ui` self-observation — the surface exports its own screen as text

**Task:** 01a0d2ee6d1b781faad3803b60a4e302
**Branch:** `task/01a0d2ee6d1b781faad3803b60a4e302`
**Verdict on the epic:** DELIVERED. The TUI can be read by another process as
**plain text** — geometry, cursor, cell grid, per-cell attributes, scrollback —
with no pixels, no vision model and no cross-process screen access. Two real
defects in the underlying terminal-grid representation were found by the E2E run
and fixed; both are a reason the capability is worth having, not incidental.

---

## The capability, in one paragraph

`gopi ui --dump-screen[=PATH]` arms a sink beside the surface's render stream.
The sink interprets exactly the bytes bubbletea paints into a `lineedit.Screen`
cell grid and writes that grid to a text file (plus a `.json` twin) an agent
reads directly. Without a PATH the file lands beside the run's own session
artifacts (a scratch directory, off `/`, when the run has none) and the resolved
path is printed **before** the TUI takes the terminal. Default off: a run with no
flag behaves exactly as before.

---

## Completed

### 1. `internal/lineedit/screendump.go` — the structured snapshot API
`Screen.Dump() ScreenDump`: copies (never aliases) the grid; exports cols/rows,
cursor row+col, scroll region, the **cell-exact** grid (one rune per cell, wide
runes materialising their continuation cell), per-row attribute runs, scrollback,
and an explicit **blind-spot list** (8 entries, each with its explanation).
`Text()` is the readable rendering, `JSON()` the machine-checkable one.

### 2. `internal/uiclient/dump.go` — the live dump sink
`ScreenDumper` is an `io.Writer` placed **beside** `ScreenWriter` on the stream
bubbletea paints, so the dump describes the surface **as rendered**, not a model
re-derived from the view function. Design decisions, each with a reason:

- **Throttled**: a write refreshes the snapshot at most once per
  `dumpMinInterval` (100 ms), and rewrites the file only when the rendering
  **changed** (compared on the dump body, never on the record — see the defects
  below). An idle surface repainting one frame costs a string comparison.
- **`Snapshot()` reads the grid, not the last written file.** The throttle bounds
  what the *render path* pays; a holder of the sink asks for the truth now.
- **`ansi` / `plain` modes**, selectable by flag or env; `plain` treats bytes as
  literal text with LF/CR as the only controls.
- **Atomic install** (temp file + `os.Rename`) so a reader never sees a
  half-written dump; every failure path removes its temp file.
- **Provenance above the body**: pid, target, tools switch, terminal vs model
  geometry, grid, mode, frame/dump counters, timestamps, and any write **error**
  (an observer that cannot write must not read as healthy — Rule 15).

### 3. `cmd/agent/cli_ui.go` — flags, env, resolution, reporting
`--dump-screen[=PATH]`, `--dump-mode ansi|plain`, env `GOPI_UI_DUMP` /
`GOPI_UI_DUMP_MODE`; path resolution flag > env > session dir > scratch
(measured off the root filesystem, Rule 22); the resolved path printed before the
TUI starts; a one-line `#gopi-ui-dump-report …` printed after it exits. A
separated path (`--dump-screen /a/path`) is refused **by name** with the fix in
the message.

### 4. Two REAL defects in the grid, found by the E2E run and fixed

Both were found by comparing the dump against the bytes the surface actually
wrote (strace + the pty transcript) — not by reading the code.

**(a) An OSC string sequence was rendered as TEXT.** `consumeEscape` treated
`ESC ]` as "ESC + one byte" and then painted the payload: `ESC ] 11 ; ? ESC \`
(a bubbletea background-colour query, emitted before the first frame) left
`"1;?"` in cells 0–2 of row 0. Measured: the dump's header row read
`" opi ui → …"` — a stray `"1;?"` over the first four cells of a frame that was
correct on the wire. Fix: an OSC arm consuming to BEL or ST (`oscLen`).

**(b) `CSI J` was implemented only for parameter 2.** The directional forms a
TUI actually uses were **no-ops**, so cells the terminal had already wiped
survived in the grid. Bubbletea emits `CSI 29A CSI J` on every frame whose
content grew shorter; with mode 0 ignored, the single space it writes for an
empty view stayed on the header row. Fix: `eraseDisplay` implements ED 0/1/2,
and EL's parameters are now named constants. (Also found: a standalone `ESC \`
from outside an OSC consumed the next byte — left as-is, noted below.)

### 5. Tests (all green)
- `internal/lineedit/screendump_test.go` — grid column-exactness (ASCII + wide
  runes), attribute runs covering the full row, geometry/cursor/scroll region,
  scrollback, snapshot immutability, text rendering, JSON round trip, plus
  **three regression tests** for defects (a)/(b) and the OSC chunk rule.
- `internal/uiclient/dump_test.go` — stream interpretation, plain mode, the
  throttle (snapshot freshness inside the window, no rewrite on an unchanged
  render), identity + JSON twin, atomic install with no litter, snapshot-only
  mode, unknown mode refused.
- `cmd/agent/cli_ui_test.go` — the flag table, the env switch's truthy-vs-path
  rule, path resolution (incl. the off-`/` measurement), the tee wiring, and the
  end-of-run record format.

### 6. Documentation
`CONFIG.md` §2c carries both env knobs with the source line that reads them
(`config_doc_gate_test.go` green). `--help` documents both flags, the env
variables and the *why*.

---

## Verification

- **Build:** `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK;
  `go build ./...` → OK.
- **Tests:** `go test ./internal/lineedit/` → `ok 9.679s`;
  `go test ./internal/uiclient/` → `ok 0.243s`;
  `go test ./internal/standards/ -run TestConfigDoc` → `ok 1.354s`;
  `go test ./cmd/agent/ -run 'TestUI|TestWeb' -v` → **20/20 PASS** (all 15
  `TestUI*`, my 5 new ones among them: `TestUIParseFlagsScreenDump`,
  `TestUIDumpFollowsTheEnvironmentSwitch`, `TestUIResolveDumpPath`,
  `TestUIArmDumpWiresTheRenderStream`, `TestUIReportDumpRecord`).
  The whole `cmd/agent` package is a ~10-minute suite (its own `-test.timeout`),
  so it is not quoted as a gate here; the branch-scoped run above is the gate for
  what this diff touched, and its own integration tests are unaffected by it.
  Every new test was run red-then-green: the OSC and ED tests were written
  **after** their defects were measured in the E2E dump, and each failed against
  the unfixed parser before it passed.
- **Ran (Rule 31 — the real surface, verbatim):**

```
GOPI_UI_SANDBOX=<sandbox from `gopi sandbox create`> \
  devdoc/evidence/ui-screen-dump/capture.sh <repo>/bin/gopi

# which drives, under a pty at 100x30:
$BIN ui --addr 127.0.0.1:0 --dump-screen=<evidence>/ui-screen.dump
```

- **Result:** the surface painted and the dump was captured **live**:
  `painted after 2s (frames=5)`, `ui-screen.dump.live` 5650 bytes,
  `ui-screen.dump.live.json` 9340 bytes, transcript 1536 bytes. Verbatim from
  the live dump (the typed `q` is visible in the input row — the dump is of a
  session that ran, not a mock):

```
#gopi-ui-dump pid=41650 target=http://127.0.0.1:43687 tools=off terminal=100x30 model=100x30 grid=100x30 mode=ansi frames=5 dumps=2 recorded=2026-09-24T10:53:59Z started=2026-09-24T10:51:59Z
#gopi-screen-dump v=1 cols=100 rows=30
cursor row=29 col=0
scroll_region top=0 bottom=29
attrs recorded=true
000|gopi ui → http://127.0.0.1:43687  (tools off)
001|  · gopi ui — type a message and press Enter. Ctrl+X interrupts a turn, Ctrl+C quits.
002|
028|you> q▏
029|
```

**Layout assertions from that text (4, not "looks right"):**
1. The header row **starts at cell 0** with `gopi ui → <target>` and carries no
   survivor — the defect (a)/(b) regression, asserted on a real frame
   (`^\d{3}\|gopi ui` at line index 000).
2. The cursor is at **row 29, col 0** while the input row is **28** — i.e. the
   input line sits one row above the screen's bottom, with the status area last;
   a wrong geometry assumption is visible as an off-by-one here.
3. The grid is **column-exact**: the grid row for index 000 is exactly `cols`
   = **100 cells** — measured as 100 runes, with the wide `→` carrying its own
   continuation cell — so a rune index IS a cell column. (The `000|` prefix is
   the dump's own frame, not a cell: `4 + cols` for the line, never confused
   with the grid's width.)
4. The attribute section reports `recorded=true` and a full-width run per row —
   so a layout assertion can include style, not only text.

**Cost, measured on this host (`gopi capture`, no load generated — Rule 27):**

| observation route | bytes | what it measures |
|---|---|---|
| `capture.sh` live dump (text) | **5 650** | the surface's own interpreted cells |
| its `.json` twin | 9 340 | the same, machine-readable |
| `gopi capture` whole screen (1920×1080) | **310 249** | every window on the display |
| `gopi capture --window` (1364×740) | **188 204** | the active window's pixels |

The dump is **~33× smaller than the active-window grab** and **~55× smaller than
the whole screen**, and unlike either it reads *this* surface rather than
whatever else is on the display.

- **Commit:** `ce0bb4eb` (feat) + `b02d2196` (evidence correction) on
  `task/01a0d2ee6d1b781faad3803b60a4e302`.

## Evidence (self-filed)

- `go build ./...` → exit 0, no output.
- `go test ./internal/lineedit/` → `ok github.com/nick/gopi/internal/lineedit
  9.679s` (interval `command timed out` risk: run under `timeout 120`).
- Red-then-green, defect (a): with `oscLen` absent, the dump row 0 read
  `" opi ui → …"` (pty run, `ui-screen-fixed.dump`, since removed as superseded);
  with it, `000|gopi ui → …`.
- Red-then-green, defect (b): `TestScreenEraseDisplayModes` failed
  `row 0 = "  A", want ""` and `TestScreenEraseDisplayMatchesTheRenderedFrame`
  failed `row 0 = "grownui one", want "grown"` against the unfixed parser; both
  pass now.
- `TestScreenDumperInterpretsTheRenderStream` was itself red first — it caught
  **my** bug: `Snapshot()` returned the throttled copy, so a snapshot inside the
  window reported the previous frame. Fixed by reading the grid.
- Cache hit noted: the first `go test ./internal/lineedit/ ./internal/uiclient/
  ./cmd/agent/` invocation printed `(cached)` for the first two packages, so the
  package suites were re-run individually to get real output.
- `gopi sandbox create --label e2e-dump --task-id 01a0d2ee` →
  `/home/user/workspace/gopi-sandbox/sandbox-e2e-dump-ecf8ca03`
  (`created=true`, `source=from the built-in default`).

---

## Not Done (honest)

1. **What a dump taken after the surface ENDS contains depends on HOW it
   ended** — measured both ways, and I had this wrong before I measured it:

   - **A clean quit** (the TUI's own key, or a signal its handler catches) lets
     bubbletea wipe its own panel on exit (`CSI J`, and now that the grid honours
     it the wipe is real), so the file's last write is the **wiped** screen:
     empty. An observer must read the dump **while the surface runs** — which is
     what `capture.sh` does.
   - **An abrupt kill** (SIGTERM/SIGKILL, e.g. `timeout` — rc=124) leaves the
     **last painted frame** in the file instead: bubbletea never ran its exit
     sequence, so no wipe was written. `ui-screen.dump.abrupt-kill` is that,
     captured deliberately; `ui-screen.dump.before-first-frame` is the same
     run's *constructor* snapshot (frames=0, taken before the surface painted —
     what a reader finds if it polls too early), kept as the third state a
     reader can meet.

   The three files are the three states, each labelled with what produced it. A
   future "keep the last painted frame regardless of exit" cache is a *separate*
   decision (it means displaying a frame the terminal no longer shows) and is
   not silently smuggled in.
2. **The parser is stateless across chunk boundaries.** An unterminated OSC tail
   is dropped rather than carried; a standalone `ESC \` split across two writes
   consumes one following byte. Pre-existing, narrowed to a bounded window (≤ the
   escape buffer), and documented by `TestScreenOSCSplitAcrossChunks`. Not a
   blind spot of the *dump* — the dump cannot report a byte the parser discarded
   — so it is listed here rather than in `BlindSpots`.
3. **TUI tests are record-level, not program-level.** A test that starts
   bubbletea blocks on its input; the live path is exercised end to end by
   `capture.sh` instead of by `go test`. The suite therefore asserts the record
   format and the wiring, not the live program.
4. **`--dump-screen` is Boolean**, so a separated path is refused (joined form,
   or the bare flag). Deliberate, matching `--tools`, and the refusal names the
   fix.

---

## The blind-spot list (what a text dump cannot assert)

Carried in every dump, so a reader knows the limit of its own evidence:
`font`, `ime_composition`, `raw_bytes`, `graphics_protocols`, `private_modes`,
`blank_vs_unpainted`, `interior_scroll_loss`, `frame_timing` — each with an
explanation (see `blind_spots_begin` in the live dump).

**Verdict on text-dump-only layout assertion: sufficient for this surface's
layout, and NOT sufficient for its appearance.** Everything `gopi ui` decides
itself — row assignment, column offsets, padding, wrapping, cursor placement,
per-cell reversal/bold, the scroll region — is in the grid and assertable as
text, which is why the four assertions above need no pixels. What is *not* there:
whether the glyphs render at the sizes the terminal claims, whether an IME put
its preedit somewhere, and what a graphics protocol drew. Those are the terminal
emulator's behaviour, not the surface's, and a vision model looking at pixels
would report them no more reliably than the blind-spot list states them.
