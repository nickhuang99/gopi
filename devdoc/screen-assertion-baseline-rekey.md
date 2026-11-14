<!-- doc-class: historical -->
# The screen-assertion baseline's two line-keyed permits were RE-KEYED, not widened (2026-09-27)

Task row `01a0e34a-5302-71f3-9703-cfeaafae7267`, branch
`task/01a0e34a530271f39703cfeaafae7267`, worktree
`/home/user/workspace/gopi-01a0e34a530271f39703cfeaafae7267` (== master tip at the
time of the run). Base of measurement: the worktree's own `HEAD`.

## The red, and the 2+2 shape that names its cause

`go test ./internal/standards/ -count=1` was RED, and every failing line came from
ONE gate, `plugin/screen-assertion-sync`
(`internal/standards/screen_assertion_gate_test.go`):

```
--- FAIL: TestScreenAssertionDoesNotSyncOnProducerCounters (2.55s)
    screen_assertion_gate_test.go:173: NEW violation (checklist 4.3 (assert the
        subject, not a proxy)) at internal/lineedit/menu_test.go:624
    screen_assertion_gate_test.go:173: NEW violation … at internal/lineedit/menu_test.go:642
    screen_assertion_gate_test.go:173: STALE baseline entry internal/lineedit/menu_test.go:607 …
    screen_assertion_gate_test.go:173: STALE baseline entry internal/lineedit/menu_test.go:625 …
FAIL	github.com/nick/gopi/internal/standards	34.824s
```

Two NEW hits and two STALE permits for the same two call sites is the gate's own
diagnostic for one thing: the sites MOVED. `screenAssertionBaseline` is keyed by
`file:line` on purpose (a permit that moved is re-reported — `internal/standards/
gate.go`, "Baselines are exact `file:line` entries … so a violating line that MOVES
is reported again — the allow-list cannot rot into a blanket file exemption"), so a
refactor that inserts lines above a permitted site makes its permit stale AND its
site new — the same site, twice.

## What the evidence says the direction is (not both, and not a widening)

Three candidate directions were on the table; the tree decides which one is true.

**1. Where the numbers moved, measured per commit** (this is what rules the
"convert to escape" direction OUT for the re-key question):

```
$ for c in 6620b59c ff7d6e68^ ff7d6e68 a02e53d8^ a02e53d8 HEAD; do
    echo "--- $c"; git show $c:internal/lineedit/menu_test.go | grep -n "Repaints()"; done
--- 6620b59c   607: beforeRepaints := h.ed.Repaints()      625: if got := h.ed.Repaints() - beforeRepaints; …
--- ff7d6e68^  607: …                                      625: …
--- ff7d6e68   607: …                                      625: …
--- a02e53d8^  607: …                                      625: …
--- a02e53d8   624: …                                      642: …
--- HEAD       624: …                                      642: …
```

EXACTLY ONE commit is accountable — `a02e53d8` ("feat(lineedit): a one-candidate
menu states why Up/Down cannot move"), which inserted 17 lines above the budget
window. `6620b59c` is the gate's own landing commit (Sep 21), so 607/625 there is
the site the permits were measured from, and `git diff 6620b59c HEAD --` the window
region shows inserts above it and no edit to the window's own lines. 607→624 and
625→642 are therefore the SAME two call sites, one insert apart — the exact case
the allow-list's own prose anticipates and the direction a re-key addresses.

**2. Are the two sites still the same KIND of thing?** Read in place, they are the
repaint-budget pair of `TestChatCompletion_MenuRepaintsStayBounded`: the window
OPENER, `beforeRepaints := h.ed.Repaints()`, read after `waitForSettledInput` has
consumed every keystroke (the comment above it states the window must not open on a
screen condition, which is the retired wait's defect, inverted), and the BOUND,
`h.ed.Repaints() - beforeRepaints > a7RepaintsPerKey*keys+a7RepaintsOverhead`, a
count of redraw DECISIONS compared against a computed budget. A budget test MEASURES
its package's counter; it never waits on a producer to conclude a screen is
finished. Nothing about the two lines changed in `a02e53d8` — only their offset.

**3. Why not simply delete the permits and rely on the escapes?** The gate offers
that exit (`//screen-ok: <what it measures>`), and the five `internal/uiclient/
dump_test.go` sites use it instead of a permit (devdoc/cmd-agent-master-reds-
20260925.md §1). But here NEITHER direction alone says enough. A permit records
*measured and reasoned, line-keyed*; an escape records *correct, and here is why, on
the line itself*. The two lines can carry BOTH, and that is the strongest residue:
the number keeps the allow-list honest (the gate can still report a STALE permit if
the site is deleted) while the one-line reason travels with the code for the reader
who never opens this package. So the direction chosen is **re-point with the reason
preserved, AND state the measurement at the site** — and the permits stay line-keyed
rather than being deleted.

## What was changed, precisely (baseline 5 → 5 — SHRINK applied where the shift allowed)

- `internal/standards/screen_assertion_gate_test.go`: the two permits re-keyed
  `607 → 624` and `625 → 642`, each comment preserved and extended to name the site
  and its inline escape. The baseline's LENGTH is unchanged at 5 (it did not grow);
  the two slots are the same two reviewed sites, shifted. A NEW
  `screenAssertionBaselineProvenance` map records, per re-key, the old key, the
  accountable commit and the proof command — so a future reader can tell a
  re-keyed permit from a silent permission without re-deriving it. It is
  documentation: `enforce()` ignores it, it holds no permit.
- `internal/lineedit/menu_test.go`: each of the two sites now states what it
  measures in the gate's own escape vocabulary:
  - `:624` — "measures the repaint budget the test's own keys earned — a count read
    after the settled window, never a wait on a screen";
  - `:642` — "measures that redraw DECISIONS stay under the per-key budget (a storm
    bound), not that a draw finished".
  Both reasons name the subject (a count vs. a screen), which is the distinction the
  gate exists to force a reader to make.

## Verification — the gate bites, and the exemption is load-bearing

```
# before (master tip / this worktree's base)
$ go test ./internal/standards/ -count=1
--- FAIL: TestScreenAssertionDoesNotSyncOnProducerCounters (2.55s)
    2 NEW (menu_test.go:624, :642) + 2 STALE (menu_test.go:607, :625)
FAIL	github.com/nick/gopi/internal/standards	34.824s

# after
$ go test ./internal/standards/ -run TestScreenAssertion -count=1 -v
--- PASS: TestScreenAssertionDoesNotSyncOnProducerCounters (2.97s)
--- PASS: TestScreenAssertionCountersRedProof (0.00s)
--- PASS: TestScreenAssertionCounterNameIsExact (0.00s)
$ go test ./internal/standards/ -count=1
ok  	github.com/nick/gopi/internal/standards	33.785s
$ go test ./internal/standards/ -run TestScreenAssertionCountersRedProof -count=1
ok  	github.com/nick/gopi/internal/standards	…

# the exemption is load-bearing, not decorative: strip BOTH permissions (the two
# permits AND the two inline escapes) and the gate reports exactly those two sites
# again — i.e. the gate is still biting on the call shape, and it is the two
# reviewed permissions that make it green (session evidence dir,
# evidence/redproof-both-excuses-removed.txt):
$ go test ./internal/standards/ -run TestScreenAssertionDoesNotSync -count=1
2 × "NEW violation … internal/lineedit/menu_test.go:624 / :642"

# the sites' own package, and the neighbours the escapes touch
$ go test ./internal/lineedit/ -count=1   →  ok  	…	18.291s
$ go build ./...                          →  build OK, no output
```

The gate was NOT widened: `screenAssertionMinTestCallSites`, `prodRoots`,
`screenAssertionFind`, `screenAssertionCounterName` and
`TestScreenAssertionCountersRedProof` are byte-identical to base, and the baseline's
entry count is unchanged. Nothing was silenced: the two sites are permitted AND
justified at the site, and the permit's provenance is recorded with the command
that proves it.

## The trap this run walked into, recorded so the next reader does not

The FIRST attempt keyed the second permit to `:650` — the line number read off the
ORIGINAL (pre-escape) file, before the first edit added 8 characters to line 624.
The gate answered `STALE baseline entry internal/lineedit/menu_test.go:650`, which is
the gate doing its job: a line-keyed permit must be keyed to the line the counter
call is on **after** every edit in the same change, and a `grep -n` after the last
edit is the only reading that is true. Cheap to hit, cheap to fix, worth one
sentence here because the same 2+2 pattern (NEW+STALE for one site) will appear
again the next time a permitted line is edited.
