<!-- doc-class: historical -->
# An anchored comment line is a frozen ADDRESS — reflow the prose, never grow the block above it

Task 01a1416f (lands the lesson the doccheck fix at `4ec787acc` paid for).

## The anchor

`devdoc/mergeset-published-anchor-union.md` anchors the TEXT of one comment line
inside the doccheck verb's own source:

    cmd/agent/cli_doccheck.go:161
    // modes, what each writes, and the exit convention a caller gates on.

Three independent things hold that address:

1. **The manifest.** `devdoc/DOC-INDEX.tsv` carries the row for
   `devdoc/mergeset-published-anchor-union.md` with an `anchor` evidence cell —
   `anchor cmd/agent/cli_doccheck.go at // modes, what each writes, and the exit
   convention a caller gates on.` The cell records the path and the text; the
   LINE NUMBER lives in the citing document, and the anchor leg of
   `gopi tool doccheck` resolves the pair (does that text still sit at that
   line?).
2. **The verb itself.** `cmd/agent/doccheck_usage_anchor_pin_test.go` pins
   `docUsageAnchorLine = 161` and `docUsageAnchorText` to the doc's own copy of
   the line. It reads the number **from the constant, never from the file** — a
   pin that re-read the number would agree with any drift.
3. **The anchor-number census.** `go run
   internal/docclass/testdata/anchor-number-census.go . <baseline>` freezes the
   DRIFT population of every anchor row into
   `internal/docclass/testdata/anchor-number-baseline.tsv`; a drift absent from
   that table is `NOT PERMITTED`.

So the comment line is not decoration. It is an address three surfaces resolve,
and the file it lives in is the same file whose help text is edited every time
the verb grows a leg.

## Why a const above that line breaks it

The pressure is structural, not careless: Go source puts package-level `const`
blocks at the top, and a new leg's constants naturally land between the previous
`const` block and `var docCheckUsage`. That is **above** line 161. Every inserted
line pushes the anchored comment down by one, and the three surfaces above all
turn red — including the verb reporting **itself** as number-drifted, because the
corpus's anchor leg measures this anchor as if it were any other doc's citation.

Go does not require a constant to precede its user. That is the whole opening:
**the const can go below the anchor**, or the prose above it can be reflowed to
occupy the lines it already occupied. Nothing forces the block to grow.

## The three measured failures

Measured on the branch's regression commit `7442e4f55` (*"fix(doccheck): a red
names its cause — ONE verdict line for every leg, and an arm-level guard on it"*),
which added ten lines of const+comment above the anchor. Recorded three times
before the repair:

| # | when | artifact | what it says |
|---|---|---|---|
| 1 | 04:37:53 | `$SESSION/gates.log` | `--- FAIL: TestDoccheckUsageAnchorLineIsWhereTheCorpusSaysItIs` — `doccheck_usage_anchor_pin_test.go:81: cmd/agent/cli_doccheck.go:161 no longer carries the corpus's recorded text.` / `want "// modes, what each writes, and the exit convention a caller gates on."` / `got  "\tdocCheckDriftUnpermitted = \"doccheck:   NOT PERMITTED (drifts, absent from the baseline): %s\""` |
| 2 | 04:44 | none of its own | the same pin red, recorded in that run's report (`$SESSION/task-report.md`, "Measured three times before the fix"). Honest note: this observation carried **no separate artifact** — the two that do are #1 and #3. |
| 3 | job-4, exited 1 (recorded 04:49) | `$SESSION/budget.log` | the branch-scoped budget ends `stamp: 2026-10-16T04:35:44+08:00 host=peerbox pkg=./cmd/agent budget=25m elapsed=1002s … exit=1 outcome=fail`, and its failure section names the same test with a **third** piece of evidence — line 161 carrying a mid-edit comment: `got  "\t// could return a failing class that nothing ever printed — a non-zero exit"`. 22 failing tests in that package run, this one among them. |

Failure #3 is the one that matters most: it shows the anchor failing **inside the
full package budget**, i.e. a real gate on the merge path, not just a targeted
test run. Its `got` values also show the line drifting through TWO different
intermediate edits — the const block first, then in-progress prose — which is the
signature of editing above a frozen address.

The exit-1 claim has its own history worth keeping: an earlier message of that
run attributed `0` to job-4. The log line above is the correction — `exit=1
outcome=fail`. A green claim not read off the artifact is not a claim.

## The remedy actually applied

Commit `4ec787acc` — *"fix(doccheck): keep the anchored help line at 161 — the
verdict-line prose is reflowed, never grown above it"*:

- **Reflow, do not relocate.** The prose block above the anchor was compressed so
  it occupies the lines it occupied before (16 lines → 6), leaving line 161
  exactly where the corpus froze it.
- **No prose was deleted.** It was compressed. Every sentence survives; the
  paragraph breaks and the redundant restatement did not.
- **The new constants moved BELOW the anchored line** where the ordering was what
  cost the lines. (Go allows a constant to follow its user.)
- **The anchor was not moved and the docs were not re-pointed.** Updating the doc
  and the pin "in the same commit" is the pin's own fallback wording, and it is a
  worse trade: it re-keys a landed record and makes every other citation of that
  address stale for no gain.
- **The baseline table was not regenerated.** `anchor-number-baseline.tsv` is a
  ratchet-down-only freeze of DRIFT. Permitting a drift this run CREATED would
  convert a fixable edit into a permanent allowance — the table would then carry
  a permit whose reason ("inherited defect") is false, and
  `AnchorNumberAssertReasonIsTrue` is built to catch exactly that.

**The repair was itself red-proved** (`$SESSION/redproof-anchor.log`,
`MUT_RC=1`): inserting ONE probe comment line above 161 makes the pin fail, naming
line 161 and the text it now carries —
`got "// MUTATION-PROBE: one inserted line shifts the anchored line by one"`.
Restoring the file gives green again (`$SESSION/redproof-anchor-green.log`).

## Rule: the anchored line is an address, and the block above it MUST NOT grow

A const, a comment, a blank line — anything inserted **above**
`cmd/agent/cli_doccheck.go:161` moves the anchor. This is a standing rule of this
tree, not a note about one run, and each clause carries its own enforcement:

1. **MUST NOT insert above the anchor.** New constants go below `docCheckUsage`;
   new help prose goes inside the `docCheckUsage` string, which is *below* the
   anchor. A constant is not required to precede its user, so nothing is lost.
2. **MUST NOT grow the block above the anchor.** If a fact has to be stated
   above the line, it replaces lines rather than adding them.
3. **MUST NOT delete prose to make room.** Deleting a standing explanation to
   protect a line number trades a real claim for an address. Reflow instead.
4. **MUST NOT move the anchor.** The address is cited by other docs, by the
   manifest and by the pin; moving it is a silent falsification of all three.
5. **MUST NOT regenerate the anchor-number baseline to absorb the drift.** A
   permit for drift this run created is a self-issued exemption, and the census's
   provenance probe refuses the reason such a row would have to carry.

The pin (clause 4's own guard) fails the build; the anchor leg of `gopi tool
doccheck` is the corpus's criterion; the census is the freeze. The policy is one
line to state and three surfaces deep.

The test of the discipline is the same three commands every time.

## Check commands

```bash
# 1. the address — the anchored line is where the corpus froze it (expect 161)
grep -n "// modes, what each writes" cmd/agent/cli_doccheck.go

# 2. the pin — the in-package guard, legal to run alone
go test ./cmd/agent/ -run 'TestDoccheckUsageAnchorLine' -count=1

# 3. the corpus — the anchor leg resolving the manifest against this tree
gopi tool doccheck
#    GREEN means: "0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv" AND no
#    "NOT PERMITTED"/"STALE PERMIT" AND "the devdoc corpus is consistent".
#    On a tree where other rows have gone stale this verb exits 1 with
#    "verdict: drift" for a stored-cell population that has nothing to do with
#    the anchor — read the gap line and the anchor section, not the exit status
#    alone (measured 2026-10-16: 0 gaps, 8 unrelated stored-cell rows, RC=1).

# 4. the freeze — NO drift may be absent from the table (read-only verdict)
go run internal/docclass/testdata/anchor-number-census.go . \
    internal/docclass/testdata/anchor-number-baseline.tsv --check
```

If #1 prints anything other than 161, do not look at the table and do not look at
the docs: find the edit that grew the block above the anchor and reflow it. The
repair has been known to take one deletion of a redundant line.
