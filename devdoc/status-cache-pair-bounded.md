<!-- doc-class: historical -->
# The status row's cache pair: a bounded form and a reservation derived from it

Task `01a0e2e9` (epic: the cache pair's unbounded width). Branch
`feat/status-cache-pair-bounded`.

## The defect, as measured

The operator's row, on a session with ~1.66M cached tokens:

```
deepseek-flash ·  iter 9  ·  tools ·  ⚙ keyword_search ·  ctx 42.1k/200.0k ·↑1662k↓…  4m00s · ready
```

The cache read-out is a PAIR — `↑` the tokens the prompt cache SERVED (the cheap
path) and `↓` the tokens WRITTEN into it (the miss that creates the entry, billed at
the miss rate, ~30× on the current card). On this row the ↓ side is gone: the string
ends `↑1662k↓…`. The surviving half reads as the whole story, and the half that was
lost is the expensive one.

Two independent faults produce it, and neither alone is sufficient:

1. **The form was unbounded.** `statusCacheField` composed the pair with
   `formatCountCompact`, whose output width is a function of its INPUT's magnitude
   (`1234 → "1k"` is 2 cells; `1662000 → "1662k"` is 5; the int range spans ten
   magnitudes). A reservation cannot cover such a form: covering ten orders of
   magnitude costs cells the 100-column budget does not have.
2. **The reservation was a sample, not a bound.** `statusCacheCol = 7` was measured
   from ONE string — `"↑12k↓4k"`, the pair at a four-digit count. The group's own
   arithmetic was then consistent with that 7, so nothing widened: the layout's own
   tail cut took the 5 cells that were missing, and the tail is where the ↓ arrow and
   the write number sit.

## The fix

**The form is bounded by construction** (`formatCountBounded`,
`chat_status_cache_bound.go`). A magnitude selects a rung of a fixed unit ladder
(`""`, `k`, `M`, `G`); the whole part is capped per rung and the value ROUNDS to the
whole unit — and when the rounding carries the value into the next magnitude the rung
advances, so the printed unit always states the magnitude the value actually has.
Every rendering is at most three TEXT cells (digits plus, on a unit rung, its unit):

| count | rendered | text cells |
|---|---|---|
| 999 | `999` | 3 |
| 1234 | `1k` | 2 |
| 12345 | `12k` | 3 |
| 1662000 | `2M` | 2 |
| 999999999 | `1G` | 2 |
| 2147483647 | `2G` | 2 |
| 999999999999 | `10G` | 3 |
| MaxInt64 | `922G` | 4 → ROUNDED at the top rung |

**The reservation is derived from that bound.** One side is its text plus the
direction arrow: `statusCacheCountSideCellsWidest = statusCountSideTextWidest (3) +
statusCacheCountGlyphCells (1)` = **4**. The sub-cell is then
`statusCacheCol = 2 * side + statusCachePairSepCells (0)` = **8** — two cells wider
than the 7 the pre-fix compact string reserved.

**The precision is ZERO decimals, and that is the budget's own arithmetic, not a
taste.** A decimal costs the separator plus the digit on top of the whole part —
FOUR cells in the row, because the side appears twice beside its arrow — and the pair
must fit the sub-cell the budget has. The tenth is what the reader gives up; both
sides WHOLE is what he keeps. An earlier iteration kept one decimal and printed
`↑1.7M↓…` — which is the defect again, with the miss-billed half traded for a
decimal place.

**Two corrections along the way, both of which were cells reserved over nothing:**

- `statusCachePairSepCells` is **0**, not 1: each side carries its own direction
  glyph, so the second side's `↓` IS the boundary between them. Counting a separator
  as well reserved a cell no character of the composed string occupied — the
  retired-column defect in miniature.
- The stale prose arithmetic (which claimed `group = 26`, `pin = 99`, `slack = 1`)
  is updated to the derived truth: `group = ctx 17 + 2 seams ×1 + cache 8 = 27`,
  `pin = 100`, `slack = 0`.

The pin now spends the budget exactly (100 = 100). That is an EQUALITY, not a
shortage: every cell is drawn, and the equality was reached by fixing a
double-counted reservation rather than by spending air — the opposite of the
retired-cache-column defect.

**No other column gave cells up.** An earlier iteration took a cell from
`statusIterCol` (7→6) and from `statusLiveCol` (13→12); both are WRONG and both were
caught by the existing suites (`TestStatusRowRendersAtThePin` requires `iter 12`, 7
cells; `TestStatusLadder_LiveBlockIsReserved` requires the hours-deep label
`1h02m · ready`, 12 cells plus structure). The final change touches neither.

## The regression guard

`chat_status_cache_bound_test.go` (Rule 10.5 — every fix ships a minimal repro):

- `TestStatusCacheFieldWidthIsBounded` — walks every rung, its boundaries, the
  operator's session and the int extremes; asserts each side fits
  `statusCacheCountSideCellsWidest` and carries no ellipsis. The check is against the
  RESERVATION, so the test and the geometry cannot be updated apart.
- `TestStatusCacheReservationCoversItsFormatter` — the arithmetic tie: the sub-cell
  is at least the pair the formatter composes, the separator is 0 for the stated
  reason, and the group's reservation carries its sub-cells. This is the assertion
  that would have caught the epic's defect at review time (nothing compared the two
  numbers).
- `TestStatusCachePairSurvivesItsOwnMagnitude` — the epic stated operator-facing:
  the composed ROW, through the production composer at the pin, carries BOTH numbers
  whole, with distinct read/write values so a composition bug that drops one side
  cannot hide. Rule 31: the surface is executed, not re-composed in the test.

## Honest limits

- The `G` rung is the ladder's top. A count past the widest whole part a rung may
  print is ROUNDED to those digits in `G` (`922G` for MaxInt64), which still states
  the magnitude — never clipped, which would print a number the counter never
  reported. A reader who needs the exact count beyond ~10^12 has the context
  read-out; the row's job is the order of magnitude and both sides whole.
- At zero decimals a value just under a rung boundary can round ACROSS it (`999999`
  → `1M`), which is correct and is why the rung is re-resolved after the carry.
- `formatCountCompact` (the unbounded predecessor) is now **unused by the row** but
  kept: it defines the cell threshold in the constants' notes and the arithmetic test
  measures it. Its doc comment says plainly that it must not go back on the row.
- The compile-time budget guard changed shape: it was `uint(budget - cols)` (which
  refuses EQUALITY), now `uint(budget - cols + shift)` with a named zero slack — so
  `pin == budget` builds while an overshoot does not.
