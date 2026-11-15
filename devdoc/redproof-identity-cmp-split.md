<!-- doc-class: normative -->
# The Rule 5 identity arm's `cmp` is a second, stricter predicate

## What was wrong

`internal/standards` arm (c) (`redproofIdentityFind`) polices "a content-identity
comparison anchored to NO revision". Its command vocabulary was ONE regex —
the digest commands plus the byte comparator, all behind the same boundary: *the
token follows a space, a quote, or a shell separator*. That boundary is a fact
about shell TEXT. It is not a fact about Go, and the byte comparator is a Go
identifier as well as a program: the standard library package, a local variable,
a sort callback, a field name ending in it.

Measured 2026-10-26 (task `01a1750d`): a 17-line Go file that compares NO
content — it takes a comparator's result into a local and tests that local
against zero — turned `TestGateRedProofRestoreIdentityRealTreeIsGreen` RED,
naming the line that carries the identifier, with the arm's full "no revision
anchor" message. A gate whose false positive is "you imported the standard
library" gets routed around, and a routed-around gate guards nothing.

## The discriminator

The fix does not lengthen a spelling list; it states the FACT the false positive
gets wrong: **a byte comparison is a COMMAND LINE, and a command line names its
file arguments on its own line.** The token must satisfy three conditions
(`redproofIdentityCmpShape`):

1. **Command-line position** — preceded only by shell lead-in (line start, the
   class `redproofIdentityCmpLeadIn`). An identifier character, a digit, `.`,
   `-` or `_` before it disqualifies. That single requirement removes every Go
   spelling in one stroke, because Go always puts one of those characters
   between a name and its use.
2. **Not a field/call qualifier** — not immediately followed by `.`.
3. **It names its arguments on the same line** (conditions in
   `redproofIdentityCmpArg`): the FIRST non-flag operand must be file-shaped,
   and at least `redproofIdentityCmpMinPaths` (2) operands must be file-shaped
   or short bare placeholders.

Condition 3 is what keeps a sentence that merely mentions the program out, and
its own red proof (the arm's existing prose negative) is what calibrated the
width: a placeholder is letters/digits only and at most
`redproofIdentityCmpPlaceholderMaxLen` (2) characters, so an operator, a
punctuated word, or an English word never qualifies.

## The second half: a false NEGATIVE the same change closes

While calibrating, the pre-fix predicate turned out to be silent on a bare byte
comparison with no operator on the line. That was by construction: the arm
required an equality operator on the SAME line, which is right for the DIGEST
commands (they PRINT a value) and wrong for the byte comparator (its exit
STATUS is the verdict). `redproofIdentityLineCompares` now expresses that
distinction as the fact it is, and four fixtures in the regression table go
green-before / red-after to pin it. A fix that only NARROWED would have been a
false positive traded for a false negative.

## How the fix is proven without mutating anything

The pre-split regex survives as `redproofIdentityCmdRePreSplit`, reached only
through `redproofIdentityScanStrict == false`, which no shipped path does. Both
predicates therefore run over the SAME fixtures in the SAME process at the SAME
revision, and every row of the table declares BOTH expectations:

- red-before / green-after — the identifier false positives (the fix's point);
- green-before / red-after — the real byte comparisons the operator requirement
  missed (the fix's other direction);
- red-both — the digest commands, which this change deliberately does not touch;
- green-both — revision-anchored spellings, and two Go shapes that were already
  silent (regression guards against the next loosening).

The census at the bottom of the test rejects a table that has no fixture in
either direction, so it cannot be satisfied by a predicate that moved one way
only. And because nothing is mutated, Rule 5's restore criterion has nothing to
measure — the timeliness trap is designed out rather than satisfied.

## Boundaries worth keeping

- **This scanner stays a TEXT walk.** It reads markdown as well as Go, so a
  Go-AST implementation would cover half its corpus. The three conditions above
  are syntax facts a text scan can decide, which is what keeps the fix surgical
  rather than a second gate.
- **Do not widen condition 1 to "anything non-alphanumeric".** `.`, `-`, `_` and
  digits are excluded on purpose; their absence is the whole fix.
- **A devdoc must not spell the fragile shape in a gated position.** The gate
  scans `internal/` and `cmd/`, not `devdoc/`, but a fixture or a report copied
  into those trees is scanned — which is why the arm's own fixtures are spliced
  from named constants instead of typed.
- **Do not add a `cmp`-specific escape instead of a predicate** (memory #4285:
  widening a substring always loses the next round). The discriminator has to be
  the fact, and the default has to stay on the gated side.
