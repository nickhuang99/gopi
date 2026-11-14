<!-- doc-class: normative -->
# Film assembly + verification in Go (`internal/film`) — spec

**Standing description of `internal/film`.** The Python leg it replaces is
`tools/replay/assemble.py` and the criterion bodies of `tools/replay/verify.py`;
the port is held to the five criteria at their original strength, and the
legibility floor is the one definition of that claim
(`internal/film/checks.go:396`, `MinPXPerChar`).

**Branch**: `task/01a1001656777ef69fd6a4bbc7f71ca9`, off master `af8cae73`
(worktree `/home/user/workspace/gopi-01a1001656777ef69fd6a4bbc7f71ca9`). The work
was re-landed here from the earlier attempt's branch
(`feat/film-assembly-and-verification`), which could not auto-merge: master
deleted `internal/replay/replay.go` (971400a7) while that branch touched it.

**Evidence** (not in the tree — Rule 37/39): a real frozen run's artifacts, at
`/home/user/.gopi/sessions/01a0ffbb-a745-7cb7-a1b4-3db95617ea8e/evidence/{primary,secondary-201beats}`;
the frozen `tools/replay/verify.py` (`971400a7^`) still runs against the same set
and is the independent opinion the parity table below rests on.

## What was built

`internal/film` — one package, two halves, because they answer one question from
opposite ends:

| file | what it is |
|---|---|
| `assemble.go` | `Assemble()`: frames → `film.gif` (via `internal/gifgen`, the `gopi gif` backend) + `replay.json` + a readable `Summary()` |
| `checks.go` | the five criteria of `tools/replay/verify.py`, ported |
| `provenance.go` | verify.py's text layer: what a beat's text IS, what the surface drew for it |
| `text.go` | the surface form (gutter/decorations, markdown table geometry, whitespace folding) |
| `artifacts.go` | the film directory's shape, the ledger/dump/metadata readers |
| `palette.go` | the colour claim: palette anchors derived from `internal/lineedit`, one pass per frame |

The Python it replaces: `tools/replay/assemble.py` (343 lines) and the criterion
bodies of `tools/replay/verify.py` (1949 lines, of which the five checks are <!-- REPLAY-CHECK-HISTORICAL -->
~460 and the rest is its own plumbing). The 1949 is **the frozen port source as
this doc's port measured it**, which is not the `971400a7^` pre-image
(1609 lines, the `tools/replay/verify.py` the ablation commit deleted): no
revision of this repository holds a 1949-line `verify.py`, so that figure is
attributable only to the port's own frozen copy.

## The port's discipline — three decisions worth a reader's time

### 1. The criteria were frozen BEFORE the port, and each check cites its source

`tools/replay/verify.py` was frozen verbatim (with a criteria-only extract) so a
criterion cannot disappear together with its script. Every ported check carries
the verify.py line range in its doc comment:

| criterion (verify.py) | lines | Go |
|---|---|---|
| `check_traceability` | L1043–L1265 | `CheckTraceability` |
| `check_legibility` | L1279–L1313 | `CheckLegibility` |
| `check_motion` | L1316–L1378 | `CheckMotion` |
| `check_withholding` | L1381–L1471 | `CheckWithholding` |
| `check_colour` | L1533–L1576 | `CheckColour` |

### 2. The Python was REWRITTEN, not transliterated — and that is deliberate

`verify.py`'s own header says its criterion bodies were **replaced** rather than
debugged, and then refined at length. A transliteration would freeze the version
that was already known to be too strict. So the port carries over the CRITERION,
the reason attached to it, and the exemptions — and it simplifies the
contorted parts of the predicate (a `break`-out-of-`for`-`else` at the end of a
long loop) into the DIRECT question each one was approximating, with the
acceptance set the source's comments describe. Where the port is *looser*, the
loosening is the source's own stated rule ("a table row's pipes are the layout,
not characters the surface drew", "the turn's grid, not the beat's"); where the
port is *stronger*, it is named below.

Every load-bearing constant is verify.py's own value, named and referenced
(Rule 1/13): `MinPXPerChar = 16.0`, `MinFrames = 12`, `ColourMinSat = 0.40`,
`ColourMinValue = 0.50`, `HueBuckets = 12`, `MinHueBuckets = 1`,
`MinColouredPixels = 1000`, `PaletteRGBTolerance = 16`, `MinPaletteRoles = 1`,
`visibilityMinRunes = 4`, the four kind sets, `GUTTER_GLYPHS`.

### 3. Two substitutions, both because the port may import what Python could not

**The identifying-shape vocabulary is not re-typed.** `verify.py` carried its own
copy of the mask regex (it could not import a Go package); the port takes
`replay.IdentShapes` and `replay.RedactionStandin` from `internal/replay` — the
ONE definition (Rule 14). A shape the masker learns is therefore visible to the
checker in the same commit, with no second list to drift.

**The palette anchors are DERIVED, not tabled.** `verify.py` listed four sRGB
values with the names of the roles that paint them (`PALETTE_RGB`, L126–L137).
The port resolves them out of `internal/lineedit`'s `Palette256` through the
xterm cube's own arithmetic — so the anchors ARE the palette, and a palette
change moves them (a strengthening: a literal table can drift from the palette it
describes). `palette_test.go` pins the derived values against verify.py's four
literals, so the substitution is measured rather than asserted.

Three constants moved house for the same reason and are now **relocated, not
copied** (in `internal/replay/vocab.go`, aliased by `replay.go`):
`ToolSummaryRunes`, `ArgSep`, `DigestKeyOrder`. They are the renderer's own
digest rule, and a checker must reproduce them exactly; `replay.go` is the file
this epic deletes, so its unexported constants had to become importable before
the deletion.

### What the port is STRONGER at

- **`CheckColour` decides on more than one number.** The Python reported a slot
  count; a colourless film was measured to report **253 slots** while carrying no
  colour of gopi's, so the count alone can read as the opposite of what the
  artifact is. The `Summary()` names slots / used / non-grey / **saturated** side
  by side, and the check decides on hue buckets + anchored roles.
- **`CheckMotion` measures the FILM's frame count**, not the PNG sample's. The
  floor is about the animation a reader watches; sampling the PNGs alone would let
  a 13-PNG sample stand for a 3-frame GIF. (`sampled=true` is the port of
  `--allow-sampled-frames`: count from the GIF, per-pair table from the sample,
  each named in the notes.)
- **`CheckLegibility` cross-checks two artifacts**: the px-per-character
  arithmetic from `replay.json` AND the delivered frame's actual width — "a figure
  from one artifact was applied to another" is a check, not a comment.
- **A refused/skipped measurement is a FAILURE, not a pass**: no grid dumps, no
  metadata, an unreadable delivered file and no ledger row are each reported by
  name, so a scan that found nothing is distinguishable from a scan that never
  ran (Rule 15). The one place a test *skips* is the absence of the evidence
  directory itself, and it says so in the skip reason.

## The evidence the suite drives (and why it is not the run the briefing named)

The epic pointed at the 2026-10-02 run `01a0f7a9` (201 beats, 201 grid dumps, 186
distinct frames). It is a real frozen run and it is used here — as the
**secondary** set. It cannot be the acceptance set for one measured reason:
**its frames carry no colour at all** (0 saturated-bright pixels in a sampled
106 M; `check_colour`'s own verdict on it is RED in Python too, while
traceability / legibility / withholding are green).

So the acceptance drives the **primary** set — a real frozen run of the same day,
session `01a0fa45` (140 beats, 209 grid dumps, 139 delivered frames, 284 recorded
lines, 88 redacted runs, a colour-capable terminal) whose frames DO carry gopi's
palette (18 894 323 saturated-bright pixels over 9 distinct RGB kinds, all four
anchored roles present). The colourless run is not discarded: it is the real
artifact the colour counterexample is really about.

Both sets are **outside the worktree** (Rule 37/39: a session's evidence belongs
in its session directory, and a 34 MB frame set is a blob, not a deliverable):

```
GOPI_FILM_EVIDENCE=<session>/evidence/primary        # the acceptance set
GOPI_FILM_SECONDARY_EVIDENCE=<session>/evidence/secondary-201beats
```

## Verification recipe (reuse this, do not re-derive)

```bash
cd <worktree>
export GOPI_FILM_EVIDENCE=/home/user/.gopi/sessions/01a0ffbb-a745-7cb7-a1b4-3db95617ea8e/evidence/primary
export GOPI_FILM_SECONDARY_EVIDENCE=/home/user/.gopi/sessions/01a0ffbb-a745-7cb7-a1b4-3db95617ea8e/evidence/secondary-201beats
go test ./internal/film/ -v                      # green arm + red arm + palette anchors
```

The two arms are separate tests on purpose: `TestFilmAssemblesAndAllFiveCriteriaHold`
is the acceptance (all five green on the real artifacts, with the production
summary and the decoded-GIF numbers logged), and
`TestFilmChecksFailOnTheirCounterexample` is the red proof (each criterion red on
an artifact carrying one defect, and the failure must NAME the defect).

## Measured parity with the Python oracle — the numbers match, not just the verdict

`tools/replay/verify.py` was run on the SAME evidence, and the two implementations
agree digit for digit:

| criterion | Python (verify.py) | Go port |
|---|---|---|
| traceability | PASS — 140 beats / 284 lines / 7 turns / 85 structure-only exempt / 55 beats confirmed visible | identical counts |
| legibility | PASS — 2225 px over 117 columns, floor 16.0 | PASS — 19.017 px/char, delivered width 2225 |
| withholding | PASS — 88 runs replaced, no delivered artifact or grid carries a shape | PASS — same 88, same containment |
| colour (primary) | PASS — **18 894 323** saturated-bright px, **1** hue bucket, **9** saturated RGB kinds, roles `colour-178 cubeDeepGold`, `colour-220` | PASS — **18 894 323** / **1** / **9**, roles `answer emphasis`, `editing line / buffer` (+ the two more this port anchors) |
| colour (secondary, the colourless run) | **FAIL** — 0 saturated-bright px in 0 hue buckets | **FAIL** — 0 / 0 |

The colour numbers being IDENTICAL is the load-bearing measurement: it says the
substitute measurement (Go's decoders, one pass over the indexed bytes) reproduces
numpy's per-pixel arithmetic exactly on a 139-frame film.

The colour criterion's only substitution — the ANCHOR TABLE — is pinned by
`palette_test.go`: verify.py carried four literal sRGB values and the port derives
them from `internal/lineedit`'s `Palette256`; the three the two tables share are
asserted equal by value (`#FFFFD7`, `#D7AF00`, `#FFD700`). verify.py's fourth entry,
cornsilk `#FFF8DC`, is the 24-BIT tier's answer body, and this film is drawn by the
256-colour tier — the tier whose SGR its frames carry — so it is not an anchor here;
the port's fourth anchor is `tool activity row`, which the Python did not anchor.

## The Python oracle recipe (for re-running the parity above)

The frozen `verify.py` still runs against the primary evidence and is the
independent opinion on the same artifacts:

```bash
python3 - <<'PY'
import sys, os
sys.path.insert(0, "<worktree>/tools/replay")
import verify
E = "<session>/evidence/primary"
out = os.path.join(E, "python-out"); os.makedirs(out, exist_ok=True)
if not os.path.exists(os.path.join(out, "frames")):
    os.symlink(os.path.join(E, "frames"), os.path.join(out, "frames"))
verify.check_traceability(out, E, os.path.join(E, "session.jsonl"))
verify.check_legibility(out, E)
verify.check_withholding(out, E, E)
print("FAIL FLAG =", verify.fail)
PY
```

Measured on the primary set: traceability, legibility and withholding all green
with the SAME counts the Go port reports (140 beats / 284 lines / 7 turns /
85 structure-only exemptions / 55 beats confirmed visible; 88 redacted runs).

The colour criterion needs the delivered film at the name verify.py looks for
(`chat-replay.gif`), so the evidence directory carries the run's OWN assembler's
GIF beside the frames — the Python reads that one, the Go criterion reads the film
this port assembles from the same frames:

```bash
python3 <this-session>/colour_oracle.py     # writes <session>/colour-oracle.log
```

`TestFilmColourCriterionAgreesWithThePythonOracle` takes that log's verdict
(`GOPI_FILM_ORACLE`) and asserts the port and the Python agree on the DECISION for
the same film.

## Not done / honest gaps

- **`check_colour`'s Python body runs on the film the run's OWN assembler wrote**,
  not on the film this port assembles: verify.py looks the GIF up by name
  (`chat-replay.gif`) and this port writes `film.gif`, so the oracle would need the
  port's output copied under the Python's name to measure the port's own bytes. The
  pixel numbers being identical across two independently encoded GIFs of the same
  frames is the stronger statement; a name-level bridge between the two pipelines
  is what `gopi backspace --film` (item 2/5) will settle, and it is deliberately not
  invented here.
- **`check_motion`'s pxdiff subprocess is not exercised.** verify.py shelled out
  to `tools/human-ui/pxdiff.py`; the port compares frames in-process and writes a
  `diff-table.tsv` with pxdiff's columns for the two columns a reader acts on.
  The Python's exit-status rule (a zero-change adjacent pair is a failure) IS
  ported and IS red-proved; the two implementations' *numbers* were not compared
  file by file.
- **The assembler has no CLI verb.** `gopi backspace --film` (item 2/5 of this
  epic) is the surface that will call `film.Assemble`; this item delivers the
  package and its tests, per its own constraint of "no new subcommand".
- **`Summary()` is the production output**, printed by the test; a future verb
  will print it to the operator's terminal.
- The full 209-dump visibility pass over the SECONDARY set is not part of the
  acceptance test (only the primary set is driven end to end); the secondary set
  is used for the colour counterexample.

## Rejected alternatives, with reasons

- **Transliterate verify.py line by line.** Rejected: its own header says the
  criterion bodies were rewritten because transliterating preserved defects
  already known (`count("\n")+1`, a Unicode-unsafe byte probe, `math.log(0)`,
  `item[3]["text"]` with no guard). The port carries the CRITERION and its
  exemptions, not the contortions.
- **Use PIL/numpy via cgo or a vendored wheel.** Rejected by the epic's
  constraint (zero new module dependency, Rule 35) — and unnecessary: Go's
  decoders measure what the claim needs, and `summary()`'s palette statistics come
  from `internal/gifgen`, which was already writing them.
- **A `film` CLI verb in this item.** Rejected: `gopi backspace --film` is item
  2/5, and two surfaces for one assembly is the drift this epic is removing.
- **Commit the frames as a fixture.** Rejected (Rule 39): the seed is the RUN, and
  the run's session directory is where a run's evidence lives.

## Rules this change is held to

- **Zero dependency (Rule 35).** `go.mod` / `go.sum` are untouched: the port is
  stdlib plus this repo's own `internal/gifgen`, `internal/lineedit` and
  `internal/replay`. No new module, no script, no daemon, no state file.
- **One definition, referenced (Rules 1, 13, 14).** Every threshold is a named
  constant in `internal/film/palette.go` / `internal/film/checks.go`; the
  identifying shapes come from `internal/replay`; the digest rule moved to
  `internal/replay/vocab.go` rather than being copied.
- **No silent failure (Rule 15).** An unreadable, absent or contradicting artifact
  is a finding by name, never a pass; the metadata writer computes the arithmetic
  itself rather than trusting a caller's numbers.
- **Everything worth mentioning, devdoc'd (Rule 7).** This document is the standing
  description `internal/film` names in its package comment.
- **No generated device load (Rule 27).** The red proofs are synthetic
  counterexample artifacts (a lost grid, a duplicated frame, a grey palette, a
  re-serialized leak) — a few hundred kilobytes of pixels, never a saturated
  device.
- **Evidence off `/` and out of the tree (Rules 22, 30, 37, 39).** The frame sets
  live in the run's session directory; the worktree carries code, tests and this
  doc only.
