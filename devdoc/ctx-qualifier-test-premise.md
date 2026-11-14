<!-- doc-class: historical -->
# The ctx-qualifier pty case: a test that spelled the product's rule, and the ladder that moved under it

Status: fixed. Test file: `testsuite/chat_pty_ctx_qualifier_test.go` (case
`TestChatPTY_ContextDenominatorIsQualifiedOnScreen`).

## The symptom

The case failed at an IDENTICAL deadline on every run — `--- FAIL … (71.69s)`,
`timed out waiting for "92.1k/200.0k≡"` — which is the signature of a fixed wait,
not of slowness. Reproduced at the tip (`d1209fe0`) and, from a `git archive`
extraction outside any worktree, at the same commit unmodified: **71.79s, same
symptom**. Pre-existing, not a merge regression.

## Root cause (5-why)

1. The wait never saw `"92.1k/200.0k≡"`.
2. It never saw it because the row paints **`ctx 92k/1M≡`** — read off the pty in
   the failing run itself.
3. The row paints that because `statusCtxPair` renders both sides through the
   row's ONE count ladder (`formatCountBounded`), which states at most two whole
   digits (`statusCountWholeDigitsWidest = 2`): 92100 → `92k`, and 200000 needs
   three digits, so it climbs a rung → `1M`.
4. The case's expectation was written when the pair came from the retired k-only
   formatter (`"%.1fk"`, deleted in `81c29f95` "one count rule for the whole row —
   the context pair joins the ladder", `devdoc/status-row-one-count-rule.md`). So
   the case asserts **the retired formatter**, not the row.
5. Why it was never noticed: the string was a second copy of a product rule with
   nothing keeping the copies in step — and it was impossible for the old spelling
   to survive a ladder that rounds to a whole part: `"%.1fk"` is only equal to the
   bounded k form for values ≤ 999.

The defect the case exists for (bug F: the denominator must state its identity —
the qualifier rides the denominator; the retired heavy marker must not come back)
**never regressed**: the `≡` is still the denominator's next cell, and `!` is
nowhere.

## The repair — the expectation is DERIVED, never re-typed

`ptyStatusLadder` reads the row's ladder out of its own source at run time
(`../cmd/agent/chat_status_cache_bound.go` for the rung table, `../cmd/agent/chat_stream.go`
for the scale constant and digit policy — two files, because the rung table and the
scale it is built from are two halves of one rule). `ptyCtxPairRendered` then composes
the expected cells from the ladder's own semantics: rung selection by magnitude, one
rounding to the whole part the rung may state, a climb when that rounding runs past
the digits, `statusCountSmallestMantissa` when it runs away. The deployed window comes
from its ONE definition, `agent.DefaultContextWindow` — read, never typed (Rule 14).

The parser reads the DECLARATION (`(?m)^[ \t]*(?:const[ \t]+)?statusCountCompactKilo[ \t]*=[ \t]*([0-9]+)[ \t]*$`),
anchored at a line start and closed at that line's end, because the constant's own doc
comment quotes the same text mid-sentence. A ladder it cannot read, or cannot read as
plausible (not a power-of-ten base, not `whole + 3 named` rungs, rungs not climbing),
is a FAILURE, never a quiet empty expectation — an empty `want` would be contained in
every screen, i.e. a green asserting nothing.

Guards (both run in milliseconds, no pty):

- `TestChatPTYCtxQualifierExpectedCellsFollowTheRowsLadder` pins the ladder's
  boundaries: 92100 → `92k`, `DefaultContextWindow` → `1M`, 999 → `999`, 42100 → `42k`;
  the composed pair; the qualifier riding it; no fraction separator (the retired
  formatter's forced tail); no side stating more whole digits than a rung may print.
- `TestChatPTYCtxQualifierExpectationRefusesTheRetiredSpelling` is the RED PROOF:
  the retired formatter's own outputs for these two counts (`"92.1k"`, `"200.0k"`,
  spelled as the literals it produced — its code is deleted) must be REFUSED by the
  same predicate the live sides pass, and the refusal must name the unit system.

## Carrier: the assertion is on the stream, and why a grid is NOT a wait

The case now asserts on the **pty byte stream** (`chat.waitFor`), while also arming the
surface's own dump (`chat --dump-screen=…`) and reading it back through
`internal/uiclient.ReadDump` for the failure note and for a green run's log line.

A first attempt asserted on the dump's cell grid and waited on it — **and it hung for
the full 71s.** The reason is a property of the sink, stated in its own source
(`cmd/agent/chat_dump.go`): it installs a state only when the screen CHANGES, and it is
throttled. A change that lands inside the throttle window is dropped, and because the
screen then stands still, nothing installs it: measured, the dump's last frame stayed
on `responding … no metrics` while the terminal had already received the qualified row.
**A grid that can be one change behind is a fine artifact and an unfit wait.**

The stream's own shape matters too, and is now documented in the case: a redraw writes
only the cells that CHANGED, so the row arrives in fragments (in the failing run the
input line was repainted one column per frame). Assertions are therefore stated over
the ACCUMULATED stream, one substring per cell-run — never over a single read, where a
substring spanning two repaints would be a fiction. The old `strings.Contains(out, label+pair)`
form was exactly that fiction: it demanded two adjacent cells arrive in one contiguous
run, which the layout contract does not promise.

## Verification (this branch)

```
go test ./testsuite/ -run 'TestChatPTYCtxQualifier|TestChatPTY_ContextDenominatorIsQualifiedOnScreen' -count=1 -v -timeout 300s
--- PASS: TestChatPTY_ContextDenominatorIsQualifiedOnScreen (13.15s)
--- PASS: TestChatPTYCtxQualifierExpectedCellsFollowTheRowsLadder (0.05s)
--- PASS: TestChatPTYCtxQualifierExpectationRefusesTheRetiredSpelling (0.01s)
```

Baseline (unmodified `d1209fe0`, `git archive` extraction outside the worktree):
`--- FAIL: TestChatPTY_ContextDenominatorIsQualifiedOnScreen (71.79s)`.
