<!-- doc-class: normative -->
# `<path>:<line>` citations in `tasks/*.md` — the second reader, and why a register beats a shape rule

**Task**: `01a12de6-c052-7346-9efc-bbd885387489`
**Full run record (evidence, red/green transcripts):** `tasks/01a12de6-c052-7346-9efc-bbd885387489-report.md`
**Sits beside** `prose_anchor.go` (bare `line N` + quote), `anchor_number.go` (DOC-INDEX
anchors) and `internal/standards/census_evidence_gate.go` (Rule 38). Four carriers,
four readers; each reader's judgement surface is its own.

This stub exists so the NEXT agent who opens `internal/docclass/prose_path_line.go`
finds, in the tracked corpus, what the reader does, what it deliberately does NOT
cover, which tests pin the boundary, and which decisions were argued rather than
assumed. Only the standing facts are here; the run's narrative lives in the task
report.

## The shape, and why it needed its own reader

A `tasks/*.md` report writes two kinds of line-number citation:

| form | example | mentions (3a4fe7d4b) | resolvable by |
|---|---|---|---|
| A — bare mention + quote | `(that file, line 331: *"A whole-package run of …"*)` | 201, of which the pairing rule adjudicates 10 | a quoted span (`prose_anchor.go`) |
| B — explicit `path:line` | `` `internal/agent/chat_ops.go:1854` `` | 2158, of which 949 name an existing file | the PATH itself (`prose_path_line.go`) |

B is the dominant form and its referent needs no quotation, so a second reader
was built rather than widening A's pairing rule: A's identity lives in a quote,
B's in a path, and their verdict vocabularies are disjoint by design
(`TestProsePathLineIsASeparateReaderFromTheProseAnchorRung`).

## The one red arm, and the two silences

```bash
go test ./internal/docclass/ -run TestProsePathLine -count=1        # the gate
go run internal/docclass/testdata/prose-path-line-census.go . \
    internal/docclass/testdata/prose-path-line-baseline.tsv --check # Rule 38 re-derivation
```

Only a citation whose path **exists** and whose number is **past the file's end**
is a finding. The other two answers are recorded and never red:

* **path-unresolved** — 765 of the 1484 mentions read. A branch-qualified path, a
  foreign absolute path, a plan, or a path that simply is not here. A machine
  cannot tell "wrong path" from "a path that was never meant to be a citation",
  so it stays silent (the epic's constraint ②).
* **in-range** — the number is inside the file. That is not a claim about *which*
  line, so it is not a verdict; resolving content is `internal/agent`'s docanchor
  rung, which resolves a symbol.

## Coverage boundary, measured — not hidden

Fenced code blocks are **not read**: a fenced region holds recorded tool output (a
compiler diagnostic, a gate transcript), which the report *quotes* rather than
*states*, and which often names a line that never existed in any file. Measured
over the frozen corpus at 3a4fe7d4b, by region: fenced 688 mentions — **15 of the
census's 24 past-EOF sites, 62%** — prose 964 (9 sites), indented 93 (0), table
408 (0), blockquote 5 (0). So the reader sees the prose-borne citations and reports
the exclusion as a boundary; the frozen table therefore carries 13 rows against the
census's 24 sites, and the ratio is a measurement rather than a repair
(`TestProsePathLineReadsProseNotFencedTranscripts`).

The remaining deltas between 24 and 13 are measured too, not argued:

* **718 of the census's mentions live in the reporting run's own report**
  (`tasks/01a12dc1-report.md`), which is not part of the frozen corpus — a report
  cannot be its own input.
* **region boundaries shift between instruments.** The census classified per LINE
  (a line inside a fence is `fence`); this reader toggles one state per document,
  which is the same rule read differently at a nested or unclosed fence. Counts per
  region are measurements each instrument prints, not a shared table.

## Why a REGISTER, not a shape rule (the epic's constraint ③, decided)

The corpus contains two impossible numbers written **on purpose** to demonstrate a
gate's red arm (`internal/tools/tasks.go:999999`, `internal/tools/guest_row_guard.go:9999`).
They must not be reported as drift — the evidence that a red proof exists would be
deleted by whoever "repairs" them — and they must not buy a permanent exemption
for anything else.

A **shape rule** ("an impossible number") is not available: every genuine drift is
also an impossible number until someone reads the file, so the rule would swallow
the whole red arm. **A register is**: one line of Go per demonstration
(`prosePathLineDemos`), reviewable in a diff, checked against the corpus every run
(`TestProsePathLineDemoRegisterStillFires` — an entry that stops firing is an
error), and consulted by (ref, number, citing doc) so an identical shape in any
other report is an ordinary drift
(`TestProsePathLineDemoAndDriftAreDistinguished`).

The two kinds get **opposite** ratchet rules, and that asymmetry is the reason the
table carries a `kind` column: a `real-drift` permit whose citation stops firing is
**STALE** and must be deleted (Rule 39, ratchet down only), while a
`demo-placeholder` permit whose citation stops firing is the legitimate end of a
demonstration and is **not** a red
(`TestProsePathLineDemoPermitMayDisappearAndRealDriftPermitMayNot`). Without the
distinction the gate would hold veto power over the removal of the very evidence it
was built from.

## Line counting: one definition, one line stricter than the census

`prosePathLineCount` counts the lines a citation can **address**, the way a reader
addresses one: `sed -n 'Np'` prints nothing when N is past it, and `wc -l` agrees;
an unterminated last line still counts. This is one line **stricter** than the
census's `bytes.Count('\n') + 1`, which counts the empty tail after a trailing
newline as an addressable line — under which a citation to that tail reads
`in-range` while the file has nothing there. The delta moves verdicts only in the
safe direction (census `in-range` → reader `past-eof`, never the reverse).

## The unit the report counts, and the freeze assertion's property

Two arms of this rung measured the wrong thing. Both were found by running the
gate, not by reading it, and both are recorded here because the next reader of
either arm would otherwise "fix" it back.

### One citation is one claim, however many times it is written

`Key()` is deliberately **not** a function of `RowLine`: a report that writes
`internal/tools/merge_contract_gate.go:230` in two sections has made **one**
claim, twice. `Permitted` was always counted per **key**; the past-EOF
population (`Incoherent`, and therefore `Unpermitted`) was appended per
**mention**. The two units agree except when a key occurs more than once — which
the real corpus does often: measured 2026-10-12 at base `45b54d81f`, **18
past-EOF mentions naming 16 distinct citations** (one key written 3×, one 2×).

The disagreement was invisible until the real-tree red proof
(`TestProsePathLineRealTreePermitMovedPastEOFIsRed`) ran: it re-points **every**
finding sharing the victim's key — which is the right thing to do, since the key
is the citation — so on a key written twice it reported the unpermitted
population growing by **2** where the table had gained one row, and the test's
own `+1` assertion failed. `computeProsePathLineDrift` now keeps **one entry per
key, first occurrence wins**, so the list and the permit map cannot part company.

`TestProsePathLineDuplicateMentionIsOneClaim` pins this on a fixture (two
mentions of one `path:line` in one report), because a unit mismatch that is
visible only when a corpus happens to repeat a citation is a coincidence away
from being read as a correct red — and the next reader would find it by
debugging a live red instead of by reading a test.

### The freeze assertion measures the still-firing permit, not the row's existence

`TestProsePathLineBaselineFreezeCommitHolds` asserted the shipped table is a
plain **superset** of the rows at `ProsePathLineFreezeCommit`. That reads as
"the table ratchets down only", but it is equivalent to *no permit row may ever
be removed* — and the **same gate's stale arm orders exactly that removal**
("never leave a permission that covers nothing"). Two arms demanded opposite
things, so the rung shipped an unsatisfiable assertion.

The instance is measured, not hypothetical: `internal/scratch/holder.go:489` was
a **genuine** past-EOF citation at the freeze commit `00b26bd145` (the file was
486 lines there) and stopped being one when `e32a74237` grew it to **685**. The
permit therefore had to be deleted — and the generator's write arm did delete it,
leaving a row absent that the superset claim forbade removing.

`ProsePathLineFreezeCommit` is **honest and stays**: it is the population's
provenance (Rule 38), and the rows that still fire are still present. What the arm
now asserts is the property that matters, and it is strictly **stronger** than
"the row is still listed":

1. a permit present at the freeze whose citation **still points past EOF** must
   still be in the shipped table — its removal is a permission quietly withdrawn
   to turn the gate green; and
2. it must still carry the **same kind** — a real drift re-labelled a
   demonstration escapes the stale arm entirely, since a demonstration's permit
   may be withdrawn freely. A permit kept but re-labelled is the same withdrawal
   in the row it used to occupy.

A permit whose citation **stopped** firing is the one legitimate exit, and the
stale arm is what requires it.
