<!-- doc-class: normative -->
# internal/lineedit — the East Asian Width AMBIGUOUS (A) gap

**Task:** `01a10ed1-7137-73c7-9b7d-c4b3c6f50d88` · branch `task/01a10ed1713773c79b7dc4b3c6f50d88`
**Class:** bug — the cursor sat one row above the text after pasting a box-drawing table.

---

## Completed

### 1. The class that was measured nowhere

`internal/lineedit/render.go`'s `wideRanges` carried the East Asian Width **W/F**
set and nothing else (the comment said so: *"property W and F"*), so every rune
of the **AMBIGUOUS (A)** class was measured **one cell in every environment**.
A is the class whose width *depends on the environment* — box-drawing
`U+2500–257F` in full, `▌` (gopi's own `TurnMarker`), `—`, `→`, the enclosed
numbers, Greek and Cyrillic — and in a CJK terminal those really are two cells.

Where the drift was paid:

| surface | file | what it uses |
|---|---|---|
| the editor's layout (wrap + cursor row) | `internal/lineedit/render.go` `runeCells`/`displayWidth`/`layoutRows` | `isWideRune` |
| the editor's **cell grid** (the screen the dump reads) | `internal/lineedit/screen.go` `put`, `screenattr.go` `CellsOf`/`isContinuation` | `isWideRune`, `runeCells` |
| the answer band's gutter/cut/wrap math | `internal/lineedit/answer.go` (27 call sites) | `displayWidth`, `runeCells` |
| the help-wrap budget | `internal/config/width.go` `IsWideRune` | its own table |
| the image renderer's grid | `internal/text2img/render.go` `isWideRuneForGrid` | its own table |

One A rune short per symbol → the wrap count came out low → `layoutRows`
reported fewer rows than the terminal made → the cursor was placed one row
**above** the text's real end, growing with the length of the paste. That is the
reported symptom, and it is exactly what the red run below shows.

### 2. Where the fix lives: `internal/config/ambiguous_width.go` (new, 173 ranges)

The A table had to live in the **one package all three measurers can import**
(Rule 14). `internal/lineedit` cannot be imported by `internal/config`
(`internal/lineedit` imports `internal/config`, never the reverse) and
`internal/text2img` imports neither, so a table in any one of them would have
been **mirrored** — the drift Rules 13/14 forbid. `internal/config` is the leaf:
it imports no gopi package, and every measurer already imports it.

* **The table is generated, not hand-written** — 173 sorted, disjoint ranges
  from CPython's `unicodedata` (Unicode 14.0.0); the exact generator command is
  in the file header (Rule 38: a figure names its generator). Three families are
  deliberately excluded: the CJK-machinery blocks the W/F table already carries,
  the private-use and plane-15/16 areas, and the variation selectors
  (`ambiguousRangeCount = 173` is the pinned shape; the test fails on an edit
  that breaks the ordering or the count).
* **Rule 35 — no new dependency**: the table is DATA in the source, produced once
  offline. No EAW generator runs at build time, no external table is fetched.
* `IsAmbiguousRune` (binary search) — the class question, environment-free.
* `IsAmbiguousWide` — the class question **and** the environment's answer; what
  `internal/lineedit`'s `isWideRune` now delegates to (one line: the W/F loop,
  then `config.IsAmbiguousWide(r)`).

### 3. The environment decision — the epic's key question, answered by measurement

The epic's hypothesis was *"only the locale can explain the operator's field"*,
and asked for the terminal's **real** rendering width first. Measured on this
host (xterm on `localhost:15.0`, both readings taken):

| terminal | locale | `─` U+2500 |
|---|---|---|
| xterm, default `fixed` face | `LANG=C`, `LC_ALL=zh_CN.UTF-8`, `ja`, CJK bitmap fonts | **1 cell** (DSR answer `1;2R` = column 2 after one glyph) |
| xterm, `-fa "Noto Sans Mono CJK SC" -fs 12` | `LANG=C`, `LC_ALL=zh_CN.UTF-8` | **2 cells** (DSR `1;3R` = column 3) |

Two consequences, and they are why the answer is a **knob and not a
measurement**:

1. **`LC_ALL=zh_CN.UTF-8` is not sufficient.** The A width is a property of the
   **font**, and xterm's default face renders A one cell wide *under a CJK
   locale*. An in-process measurement would therefore be wrong on the operator's
   own terminal — and the operator's live chat processes carry
   `LANG=en_US.UTF-8` (`/proc/<pid>/environ`, measured), so a locale-only rule
   would not have changed their screen either.
2. **A locale-only rule is still the right DEFAULT channel**, because it is the
   variable the CJK conventions are already keyed on (`less(1)` treats A as two
   cells iff `LC_CTYPE` is East Asian), the operator can see and set it, and a
   wrong answer is one export away from being fixed — none of which is true of a
   number gopi inferred from a font it cannot name.

**Precedence** (one reader, `config.ResolveAmbiguousWidthMode`, Rule 2):
`GOPI_AMBIGUOUS_WIDTH=wide|narrow` > the locale (`LC_ALL` → `LC_CTYPE` → `LANG`;
`zh`/`ja`/`ko` = wide) > `narrow` (the historical behaviour). The three channels
meet in the env var: the settings key and the `--ambiguous-width` flag (both the
top-level hoist and the verb spelling) are **published** into it
(`ApplyEnvAmbiguousWidth` / `config.ApplyEnv` / `chatParseAmbiguousWidthArg`), so
nothing downstream reads a second field. An unusable value is **reported once**
on stderr and the next channel decides (Rule 15) — never silently read as unset.

### 4. The known deviations, stated

* **Western text in a CJK environment pays the same reading.** With the reading
  on `wide`, `—`, `→`, `①` and the Greek/Cyrillic letters are two cells as well.
  That is the CJK convention's own answer (UAX #11 says so), but it means a
  terminal whose font is narrow for the A class while its locale is CJK will
  over-count — the exact mirror of the bug this fixes, and the reason the knob
  exists instead of a table edit. **In a terminal whose A runes really are
  narrow, set the reading to `narrow` (or leave the locale Western).**
* **gopi's own TEXT is now consistent with the reading, but the source code's
  literals are not.** `lineedit.TurnMarker = "▌"` is A. Under a `wide` reading
  the marker is two cells, which — measured — is the font's own answer, but the
  parser's marker-column literal would also mean two source columns. The test
  suite pins the narrow geometry in several places (the fixtures are ASCII), so
  `GOPI_AMBIGUOUS_WIDTH=wide` **fails 75 of `./internal/lineedit`'s assertions**
  in fixtures that hard-code a narrow frame. Running the whole editor under
  `wide` is therefore a **second work item**, not something this change delivers
  — see `## Not Done`. The reading is exercised correctly on the surfaces that
  are not fixture-ASCII-pinned (the A fixtures, the grid, the live chat).
* **`internal/config.IsWideRune` and `internal/text2img.isWideRuneForGrid` are
  untouched** (named in `render.go`'s table comment, deliberately not unified
  here). The help text stays A-narrow, and `text2img` **stays A-narrow by
  design**: its background is white, so an A rune a font paints two cells wide is
  a cosmetic mismatch inside an image, not a cursor in the wrong place.

---

## Not Done (honest)

1. **`GOPI_AMBIGUOUS_WIDTH=wide` is not a green configuration for the whole
   lineedit suite.** 75 assertions fail there, all in fixtures whose expected
   frame geometry is printed from ASCII/box-drawing literals the tests build by
   hand (`answer_cut_marker_test.go`, `answer_test.go`'s band rows,
   `emphasis_span_grid_test.go`'s `CellsOf` expectations). The **production**
   path is coherent (the grid, the layout and the measurer all read the same
   decision) — what is narrow-booked is the fixtures' own arithmetic. Making the
   editor green under `wide` means threading an explicit mode through the
   fixtures, which is its own task.
2. **The locale→width cross-product is not automatable here.** The
   `Term::ReadKey` path (`perl -MTerm::ReadKey -e 'ReadKey 0'`) that would let a
   pty do a DSR round-trip is not available on this host, and `script(1)`'s pty
   reports `0x0` (no winsize), so `gopi chat` under `script` takes the no-layout
   path — its dump is an unpainted fallback grid (measured: `grid=80x24
   frames=0`). The measurement table above was taken in a real xterm instead, one
   glyph at a time, which is a smaller sample than the epic's "paste the table"
   scenario — see `## Verification` for exactly what was and was not run.
3. **The interactive paste could not be injected into the live chat.** xdotool
   cannot map `U+2500` to a keysym on this display ("Invalid multi-byte
   sequence"), and writing the UTF-8 bytes to the pts **slave** reaches the
   emulator rather than gopi's input (framebuffer verified: 37 → 38 frames, no
   buffer change). The live run is therefore reported as **started and observed
   at its editor geometry**, not as a pasted-table capture.

---

## Verification

**Build**

```
$ go build ./...
(exit 0, no output)
```

**Tests**

```
$ go test ./internal/config/ -run 'Ambiguous' -count=1
ok  	github.com/nick/gopi/internal/config	5.935s

$ GOPI_AMBIGUOUS_WIDTH=wide go test ./internal/lineedit/ -run 'Ambiguous' -count=1
ok  	github.com/nick/gopi/internal/lineedit	1.648s

$ go test ./internal/lineedit/ ./internal/config/ -count=1          # default (narrow)
ok  	github.com/nick/gopi/internal/lineedit	49.839s
ok  	github.com/nick/gopi/internal/config	4.396s

$ go test ./internal/standards/ -run 'ConfigDoc' -count=1
ok  	github.com/nick/gopi/internal/standards	7.271s

$ make test-budget PKGS='./internal/config'
stamp: 2026-10-06T10:31:20+08:00 host=peerbox pkg=./internal/config budget=25m elapsed=28s exit=0 outcome=pass
```

**Red before green (Rule 10.4).** The fix in the production path was reverted to
the old behaviour (`isWideRune` returning the W/F table only) and the new
assertions were re-run:

```
$ GOPI_AMBIGUOUS_WIDTH=wide go test ./internal/lineedit/ -run 'Ambiguous' -count=1
--- FAIL: TestAmbiguousRunesAreMeasuredByTheEnvironment
    --- FAIL: TestAmbiguousRunesAreMeasuredByTheEnvironment/wide_environment
        ambiguous_width_test.go:77: runeCells('─' 0x2500) = 1 cells with GOPI_AMBIGUOUS_WIDTH=wide, want 2
        ambiguous_width_test.go:77: runeCells('│' 0x2502) = 1 cells with GOPI_AMBIGUOUS_WIDTH=wide, want 2
        ambiguous_width_test.go:77: runeCells('═' 0x2550) = 1 cells with GOPI_AMBIGUOUS_WIDTH=wide, want 2
        ambiguous_width_test.go:77: runeCells('━' 0x2501) = 1 cells with GOPI_AMBIGUOUS_WIDTH=wide, want 2
        ambiguous_width_test.go:77: runeCells('▌' 0x258c) = 1 cells with GOPI_AMBIGUOUS_WIDTH=wide, want 2
        ambiguous_width_test.go:77: runeCells('—' 0x2014) = 1 cells with GOPI_AMBIGUOUS_WIDTH=wide, want 2
        ambiguous_width_test.go:77: runeCells('→' 0x2192) = 1 cells with GOPI_AMBIGUOUS_WIDTH=wide, want 2
        ambiguous_width_test.go:77: runeCells('①' 0x2460) = 1 cells with GOPI_AMBIGUOUS_WIDTH=wide, want 2
        ambiguous_width_test.go:85: displayWidth("│ 第一行 ─ 51G │") = 16 with GOPI_AMBIGUOUS_WIDTH=wide, want 19
--- FAIL: TestAmbiguousPasteWrapsTheRowsTheTerminalWouldShow
    ambiguous_width_test.go:159: the CJK reading laid out 2 rows and the narrow reading 2 — 40 ambiguous runes
        are 40 extra cells there, so the wrap counts must differ (the defect was exactly that they did not)
--- FAIL: TestAmbiguousGridEqualsTheTerminalsOwnCount
    ambiguous_width_test.go:192: the cell grid gives "│ ─ │" 5 columns with GOPI_AMBIGUOUS_WIDTH=wide, want 8
FAIL
```

The layout failure is the reported symptom in one line: the A runes contributed
**no width at all**, so the row count was identical in both environments.

**Ran**

```
$ xterm -geometry 80x25 -T eawprobe2 -e ./bin/gopi-eaw chat --ambiguous-width wide --dump-screen=/tmp/eaw-x2.dump
$ head -3 /tmp/eaw-x2.dump
#gopi-chat-dump pid=621435 target=self:chat tools=off terminal=0x0 model=0x0 grid=80x25
  geometry=layout mode=ansi frames=31 dumps=4
#gopi-screen-dump v=1 cols=80 rows=25
cursor row=24 col=80
```

The new verb flag **runs** (before the wiring it was
`gopi chat: unknown flag --ambiguous-width`, exit 2 — the run that proved the
flag had to be registered on the verb's own closed vocabulary, not only in the
config catalog). The live run reaches the reserved layout (`geometry=layout`,
`frames=31`) — the path that was a 0×0 fallback under `script(1)`.

The A-class width measurement (xterm `localhost:15.0`, one glyph + a cursor
position request, the terminal's own answer):

```
-fn default "fixed":      LV19 = column 2   LV18 = column 2   (─ is ONE cell)
-fa "Noto Sans Mono CJK SC" -fs 12:  LV19 = column 3   LV18 = column 3   (─ is TWO cells)
```

(LV19 = the tagged variable built only from `─`; LV18 = the same, built at
start-up, both non-empty — that is the evidence the glyph reached the terminal.)

---

## Evidence (self-filed)

* **Red → green transition**: the run above — same invocation, 3 failing
  functions / 13 assertions with the old production path, `ok` with the new one.
* **Table shape**: `go test ./internal/config/ -run Ambiguous` asserts 173
  ranges, sorted, disjoint, coalesced, and that box-drawing/`▌`/`—`/`→`/`①` are
  members while `中`/`Ａ`/`あ`/U+E000/U+FE0F are not.
* **Config gates**: `go test ./internal/standards/ -run ConfigDoc` green after the
  new CONFIG.md row (`§2c` rows 61 → 62, the gate's own audit constant bumped
  with its reason) and after `CONFIG.md` gained the reader's line references.
* **Live surface**: the xterm run above; the flag's refusal before wiring is
  quoted verbatim.
* **Not observed**: an interactive paste into `gopi chat` (injection blocked —
  see `## Not Done` #3), and the full suite under `wide` (75 narrow-booked
  fixtures — see `## Not Done` #1).
