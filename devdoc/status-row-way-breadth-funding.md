<!-- doc-class: normative -->
# Restoring the status row's way-marker breadth — the funding does not exist

Task `01a0faa7`, item 4/4 of epic `01a0f8c9`. The item asked:

> Restore the tool column's way-marker breadth to 4 without moving the pin, with
> the rate's 10 cells paid by a different named term that nothing draws.

**The item cannot be satisfied as written, and this document is the measurement
that says so.** Every named term in the geometry is fully drawn; raising the
breadth by 4 cells is refused by the compile-time guard, by name.

## The geometry at this pin

```
model 14 + iter 7 + tool 19 + metricsForm 33 + 3 seps × 4 + gap 2 + live 13 = 100
statusPinCols == statusColumnsAtCols == statusLayoutBudgetCols == 100, slack 0
statusToolCol = statusToolColOwn 14 + statusModeColRetired 5 + statusToolColWayBreadth 0
```

## The air audit — the test that decides it

`cmd/agent/chat_status_way_breadth_funding_test.go` (new, committed) drives each
field's **production composer** over the bounded-count ladder's rung boundaries
(999/1000, 99999/100000, 999999/1000000) and the magnitudes this row is actually
read at, and compares the widest form it can produce against the term's
reservation:

```
air audit: ctx 12/12, cache 19/19, group 33/33, tool 19/19, live 13/13
```

**Five of five terms are exactly drawn.** No term holds cells nothing draws, so
no term is a donor — which is the whole of the funding question.

The acceptance named three candidates; each is measured, not reasoned:

| candidate | measurement | verdict |
|---|---|---|
| the cache pair's per-side pin | `statusCacheCol = 2×4 + 0 = 8`; the pair's widest drawn form is 8 (`↑999↓999`) | fully spent — no air |
| the way marker's own reservation | not drawn by **any** code path (below) | real candidate, but see below |
| the gap before the live block | `lineedit.StatusLineGapCells` — the acceptance's suspicion is **correct**; the row references lineedit's term, and it is drawn | not a donor |

Two further terms were *short*, not spare, and one of them is now drawn exactly:
`statusCountSideTextWidest` is 3 with the side's widest drawn form 3 (exactly
drawn), and `statusIterCol` is 7 because `iterLabel` now composes the word plus
three digits with no space between them — task `01a10dd0` removed that space
("iter 999" measured 8 cells against the 7 reserved, so the first three-digit count
the row met was cut), and the term is drawn exactly: word 4 + digits 3 = 7.

## The red proof — the machine refuses the restoration

```
$ sed -i 's/statusToolColWayBreadth = 0/statusToolColWayBreadth = 4/' cmd/agent/chat_stream.go
$ CGO_ENABLED=0 go build -o /dev/null ./cmd/agent/
cmd/agent/chat_stream.go:952:16: constant -4 overflows uint
```

The compile-time guard (`const _ = uint(statusLayoutBudgetCols - statusColumnsAtCols)`)
refuses **by name**. Raising the breadth puts the drawn pin at 104 against a
100-cell budget, and there is no term anywhere in the geometry to pay the 4 cells
from. The tree was restored after the proof.

## The finding underneath: the marker is not drawn at any breadth

`statusToolColumnField` (cmd/agent/chat_stream.go:3157) composes the tool cell as

```go
head := toolGlyph + " " + toolName + " "
room := statusToolCol - truncateWidth(head)
return truncateRunes(head+truncateRunes(toolSummary, room-1), statusToolCol)
```

— **there is no branch that draws a way separator.** The constant's own comment
describes "the two-segment way marker the row learned in task `01a0e3db`
(`⚙ bash · retrieval: …`)", a form this composer never renders: the field is the
*same string* at `way=0`, `way=1` and `way=4`. The 4 cells the constant reserves
are therefore spent by the **digest** (the pin-row test pins the composed row, and
its comment says exactly this: the digest is "kept by the pin-row render test").

So restoring the breadth cannot bring the marker back. **The marker has to be
drawn before its width can be worth four cells** — and drawing it costs the digest
the same 4 cells it holds today, which is a content trade the epic has not decided.

## The pinned row, verbatim

At `statusColumnsAtCols = 100`, the fixture `TestStatusRowAtThePinIsExactlyTheBudget`
uses (deepseek-flash, iter 12, ctx 42100/64000, cache read 12k, a tool digest, a
1h02m clock):

```
deepseek-flash ·  iter 12 ·  ⚙ bash retrieval: … ·  ctx 42k/64k≡ ·↑12k↓0 hit 100.0%    1h02m · ready
```

measured 100 cells (`lineedit.DisplayWidth`) — the two-segment way marker absent.

## What a successor needs

The item is not *impossible* — it is **blocked on a decision, not on cells**:

1. **Decide the content trade.** 4 cells for the marker must come from the digest
   (there is no air). The operator's bug #5 note says the marker is "the shape that
   made the operator's row readable" — so the trade is probably right, but it is a
   content decision, and the epic forbids it being made silently.
2. **Draw the marker.** `statusToolColumnField` learns the two-segment form
   (`glyph + " " + name + " · " + digest`) and `statusToolColWayBreadth` returns to
   4 with the composite's width verified against the column.
3. **Keep the pin.** `statusPinCols` stays 100, `statusPinSlackCells` 0, the guard
   compiles — unchanged, because the 4 cells move *within* `statusToolCol`.

`chat_status_way_breadth_funding_test.go` is the guard for all three: it fails
today if a reservation stops matching what its composer draws, so the next
re-partition cannot fund itself from a term that regrows.
