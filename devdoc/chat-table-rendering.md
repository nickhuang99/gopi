<!-- doc-class: historical -->
# Markdown Tables in the Answer Band

**Task:** `01a0ddb4-4b9c-77ee-b4f3-ba9de3c4f725` · **Commit:** `5eaf5614`
**Files:** `internal/lineedit/table.go` (new), `internal/lineedit/answer.go`,
`internal/lineedit/emphasis.go`, `internal/lineedit/table_grid_test.go` (new)

## Why this exists

`internal/lineedit` had **no table code at all**. A markdown table reached the
answer band as ordinary prose and was wrapped at the band's cell cut: a row wider
than the band was chopped mid-token and its tail landed as an orphan run at the
block's column. A table is the one markdown construct whose meaning *is* the
column alignment the cut destroys.

A second, separate defect: one table row lost its **head** while its neighbour
stayed whole. An earlier unmerged branch
(`fix/chat-table-row-head-and-double-marker` @ `3676d3bb`) had diagnosed part of
it; those three fixes were ported here, and the *measured* root cause of the head
loss turned out to be a fourth thing entirely (below).

## The policy (one statement, in `table.go`)

1. A table is a **block**: a pipe row, a delimiter row under it with the same cell
   count, then the records. The delimiter row is **syntax**, never content — it is
   the marker saying "the row above is this table's header", and painting
   `|---|---|` shows the reader the format instead of the content.
2. **GRID when the grid fits** the band's *content* width (terminal minus the
   turn's gutter).
3. **Otherwise TRANSPOSED `field: value` blocks** — one row per field, labelled by
   the header's own cell for that column. A grid that cannot fit is not a grid;
   the form that survives any width is the one whose labels are short.
4. The band's own wrap still applies to the transposed form; the turn marker is
   drawn once per row.

**The operator's own table transposes at every rung.** Measured: its grid needs
~123 cells; a 117-column band leaves 115 of content. So it is *not* a grid case at
117, and the honest degradation is the transposed one. (My first test asserted a
grid at 117 — the measurement corrected the test, not the policy.)

## The measured root cause of the head loss

Every earlier attempt reasoned about the **live row's width**: `fitCommitRow`
reconciles a head wider than the row it was painted on by *cutting* the row to
what the head leaves. That mechanism is real (`emphasis.go` keeps it for the case
it exists for) but it was **not** what the operator saw.

The actual mechanism, found by instrumenting the commit path:

- A burst boundary falling inside a table row leaves that row's first half parked
  as `compPrefix` (`| ` for a 64-byte stream).
- The continuation then reaches `computeRows` as a **whole line without its head**
  — `Non-interactive faces | …` — which parses as *no* table row at all, so it was
  committed as prose.
- Where exactly the boundary fell decided how many characters were missing: that
  is why the damage looked arbitrary and why width-reasoning never found it.

Fixes: `routeBody` rejoins a parked pipe-row fragment **before** splitting the
body; a piped *tail* is parked as the next line's head (never painted live); a
piped complete line is routed whole to the block park, joined to its parked head.

Also measured: painting the park must clear the live row's stale partial paint
(`liveText`), or that stale text becomes the head of the next committed line and
trims it.

## The three ported fixes

| site | change | measured effect |
|---|---|---|
| `writeGutter` | drop `r.liveText += text` | the gutter was replayed as body cells → the turn marker drew **twice** on one row |
| `fitCommitRow` | `return row.withHead(head), true` | the caller re-opens the committed row at column 0, so a row committed without its head *in its text* loses it |
| `finalizeLiveRow` | `r.compPrefix = ""` | a parked fragment made the next row own another row's text, and that row was trimmed for a head it did not show |

## Isolation (transcript byte-exact)

The whole change lives in the **rendering** layer. `git diff --name-only` is
`internal/lineedit/answer.go` + `internal/lineedit/emphasis.go`; the new files are
`table.go` and `table_grid_test.go`. No storage, session-record or serializer path
is touched — nothing to prove about the ledger by construction. (The remaining
acceptance check, `gopi log <id> --response --text` before/after, is listed under
*Not Done* below.)

## Tests

`internal/lineedit/table_grid_test.go` drives a **real editor over a real region**
(the in-package `newA5HarnessSized`, reused — no second pty driver) at
117/80/60/40/24 columns and asserts the **painted grid**:

- the delimiter row is never painted;
- the grid form when it fits (200-col case);
- the policy at every rung, derived from the table's own `gridWidth()`;
- the transposed form: every field label present, **no cell ever cut**;
- no row loses its head and no row carries the marker twice — at every width ×
  every burst chunk (1, 3, 7, 64, whole-burst), because the carry is a function of
  both.

Assertions compare `noSpace`-normalised text: the band's cell-precise wrap breaks
a word at the column and re-indents it, and a wrap is not a loss. A whole-burst
run cannot see the carry; a chunked one can.
