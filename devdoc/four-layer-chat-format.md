<!-- doc-class: historical -->
# The Four-Layer Chat Format

> class: normative · evidence: internal/lineedit/layers.go, layers_test.go, layers_appendonly_test.go, testsuite/chat_layer_pty_test.go

A chat turn's transcript is four layers — **request** (the operator's own line),
**thinking** (the model's reasoning), **response** (the answer body) and
**code-tool** (a fenced/diff line, and a committed tool-activity row) — and each
one is visually distinguishable: a two-cell GUTTER at column 0 plus a named
palette Style. This document is the design record of the table that says so;
`internal/lineedit/layers.go` is the table.

## The table, and why it is a table

The four layers already existed, as four independent behaviours scattered over
the render path: the reasoning took a gutter and the status style, the answer
took the turn's marker, a tool row was a note whose GLYPH its caller wrote, and
the operator's own line was not drawn at all (the prompt is the editor's). So
"what does the transcript look like" could only be answered by reading four call
sites, and a change to one of them was invisible to the other three.

`layers.go` collapses that into ONE row per layer: the marker its rows open with
(`layerMarker`) and the palette field they draw in (`layerStyleFor`). The
renderer and the tests both ask the table, so the drawn format is a value a
reader can consult (`LayerMarkerForTest`) rather than a behaviour to infer.

| layer | gutter | style | stream |
|---|---|---|---|
| request | `▸ ` | `pal.Prompt` | the turn's own prompt, drawn by `BeginTurn` |
| thinking | `· ` (`ThinkingGutter`) | `pal.Thinking` | the reasoning stream |
| response | `▌ ` (`TurnMarker`), `' '` continuation | the body line's own style | the answer body |
| code-tool | code: the body's marker / tool: `⚙ ` (`ToolGutter`) | code: `pal.Code` / tool: `pal.ToolCall` | the answer body, or a committed `Note` row |

Two decisions in that table are worth their own paragraph, because each was a
choice between two defensible shapes.

**The gutter follows the STREAM; the style follows the KIND.** A fenced line is
part of the answer body, so it opens at the answer's marker column — while it is
drawn in the palette's CODE field, which the palette keeps deliberately distinct
from the tool field. Flattening both halves of the family into one field would
have made a code block and a tool call one visual thing, which is precisely the
distinction the family exists to draw.

**A tool row carries its own glyph; the renderer draws none for it.** The row's
TEXT already opens with `⚙ ` (cmd/agent's `termToolLine` — the same line the
`/cli` status row shows), so `ToolGutter` is the layer's marker as a VALUE and
`cmd/agent/term.go` now spells its prefix as `lineedit.ToolGlyph + " "` — one
definition of the glyph, referenced by both. The renderer drawing a gutter too
would print the glyph twice, which reads as two calls; the test
`TestLayerTable_ToolGutterIsTheRowsOwnMarker` pins exactly that failure.

## Draw order: the request row is drawn by `BeginTurn`

The layers could not simply be appended in arrival order. A tool call is
announced **before** the answer's first fragment (the agent reports the call,
runs the tool, then streams the text — measured on the pty evidence, where the
`⚙ call bash …` row lands under the header with no answer row yet), so the
row that physically arrives first is the code-tool layer's. A transcript showing
`⚙ bash …` above `▸ what is in …` would be a lie about what happened: the call is
the answer to the request, not its preface.

Three shapes were considered.

1. **Repair at the first answer row** — hold the call, paint the request when the
   answer's first line arrives. Rejected: it needs a reservation, a cap and a
   repair path, and the repair inserts a row above rows already painted.
2. **Hold the call back until the request is drawn** — rejected outright: it
   defers a tool call for as long as the model takes to answer, which is a
   present-tense transcript going silent about the thing it is doing right now.
3. **Draw the request immediately after the header** — chosen. `BeginTurn` is
   the one place that knows the turn has begun, and the request is the turn's
   first body row; the append order then IS the causal order and no reordering
   machinery exists at all. A unit test pins the ordering
   (`TestLayerTable_FourLayersAppearOnTheGridAsValues`: the request row is the
   first body row, everything else below), and the pty evidence confirms it on a
   real run.

The request row is capped at `requestRowMaxRows` (3) and CUT WITH THE PRODUCT'S
OWN MARKER (`cutField`'s ellipsis) rather than silently truncated — the rows the
cap drops are rejoined into the last kept row before that row is cut, because a
retained row is already within the width by construction and cutting it alone
would never show the marker. This was a real defect during development: the first
draft cut the retained row and produced three unmarked rows.

**Every one of these rows takes the ordinary append path** (`writeRow` →
`finalizeLiveRow` → the band's one scroll). No cursor is placed onto a committed
row, no scroll region is opened, no block is collapsed; the table chooses a
gutter and a style and nothing else.

## Appendix: what "append-only" is measured as

`layers_appendonly_test.go` renders TWO four-layer turns and compares the first
turn's grid rows — text **and** per-cell attrs — before and after the second turn
paints: every row above turn 2's header keeps both, so a row quietly re-drawn to
make room fails. It then checks the accounting: the rows the turn commits equal
the rows it shows (blank separators included), so a layer that cost a hidden row
would show up as a delta larger than the turn's own visible height.

The attrs half is not decoration: a string comparison cannot see a row that kept
its characters while its styles were rewritten under it, which is the shape a
"stale re-paint" takes.
