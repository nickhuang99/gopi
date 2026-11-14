<!-- doc-class: historical -->
# Devdoc: a permit's reason is a CLAIM — and a claim must be decidable

Follow-up to `devdoc/anchor-number-drift-detection.md` (that devdoc built the
detector; this one repairs the provenance field of the allow-list it produces).
Task `01a12e59`, branch `task/01a12e59d830718f996cd6a34e597baf`.

## The defect, in one sentence

`AnchorNumberBaselineReason` was a **pure function of the finding's CLASS** —
`number-drifted` → `"pre-existing at the 0b3c1de29 freeze; …"`,
`recorded-line-blank` → its sibling — so **every** permit asserted that its drift
predated the freeze, whether or not the freeze's tree ever carried it.

The table is a **ratchet-down allow-list of PRE-EXISTING defects** (the gate fails
only on drift NOT listed). So the constant did not just print a wrong sentence:
it **promoted** a defect the freeze had never contained into an inherited one,
and the promotion was invisible — nothing anywhere asked the tree.

## The measurement

Two carriers, both measured with `git show` only (no checkout — Rule 29):

```
git show 93e402d36^:internal/docclass/testdata/anchor-number-baseline.tsv | grep archive-mint-outside-writer
# devdoc/archive-mint-outside-writer.md  number-drifted  cmd/agent/cli_tasks.go:3655  "…"  pre-existing at the 0b3c1de29 freeze; …

git show 0b3c1de29:cmd/agent/cli_tasks.go | grep -c -F "<that row's own recorded text>"   # 0
git cat-file -e 0b3c1de29:devdoc/archive-mint-outside-writer.md                            # NO — the doc does not exist there
git show 0b3c1de29:devdoc/DOC-INDEX.tsv | grep -c archive-mint-outside-writer              # 0
```

Probing the shipped table against the freeze's tree found a **second** one
(`devdoc/cov-agent-cr.md`, `devdoc/nightshift-20260911.md`) — a class, not a typo.
Both now carry `first-frozen at 0b3c1de29`. Full transcript:
`~/.gopi/sessions/01a12e59-d830-718f-996c-d6a34e597baf/evidence/provenance-repro.txt`.

**The epic's own attribution was refuted by the same method, and that matters
more than the repair.** It said commit `c6e93569e` (a cherry-pick expected to
delete ~12 duplicate lines in `cmd/agent/cli_tasks.go`) moved the recorded line
and produced the drift. Measured: the drift is **not** a deletion artifact — the
recorded text sat above 3655 in `c6e93569e^`'s file too (a *widening* edit), and
at `0b3c1de29` that file did not carry the text at all. The repair is correct
regardless; the premise it was commissioned under is not. Reporting the
refutation is part of the deliverable (Rule 26 — an incomplete fix must be narrow).

## What was built

| artifact | what it answers |
|---|---|
| `internal/docclass/anchor_number_provenance.go` | the ONE judge: does this row's reason **describe this row**, at the named commit? |
| `AnchorNumberProvenanceProbe(root, commit, keys)` | materialises commit C's manifest + anchored files **read-only out of the object store** and runs the **same detector** over them → `map[key]bool` |
| `…BaselineReason` (producer) | marks each permit from the probe's verdict, never from its class |
| `…BaselineProvenanceCheck` + the census's `--check` arm | the table's own claims are re-decided, not trusted |
| `cmd/agent/cli_doccheck.go` | the human surface: `CLAIM NOT TRUE (regenerate the table)` / `REASON UNREADABLE (no provenance claim — regenerate the table)` |

**Two marks, one vocabulary** (Rule 1 — a closed set, stored, therefore a wire
format):

- `pre-existing at the <C> freeze` — the probe found this key drifting at C.
- `first-frozen at <C>` — it did not; this table revision is where the key first
  becomes visible to the gate.

A reason carrying **neither** is an error: an unreviewable permit is exactly the
silence Rule 15 forbids.

## The decisive design decision: one detector, two trees

The temptation was to write a *second* predicate for the historical question
("was it drifting at C?") — and that is why the first version had no such
predicate at all: a second spelling of the format is a second question wearing
the first one's clothes (the sibling devdoc's own words).

So the probe **checks out nothing** and **re-implements nothing**: it feeds C's
`DOC-INDEX.tsv` and C's files through `AnchorNumberFindings` — the same
`ParseEvidence` / `anchoredTextLine` / `anchorTextKey` grammar, the same
classification — and answers "is this key in C's finding set?". Producer and gate
call the same judge (`AnchorNumberAssertReasonIsTrue`), so the table the generator
writes and the table the gate accepts cannot disagree about what a claim means.

**The scratch materialisation is bounded and off `/`** (Rule 22): one temp root
under the scratch tree, named `AnchorNumberProvenanceScratchPrefix`, removed when
the probe returns; C's blobs are written by `git show`, never by a worktree add.

## No gate was loosened

Read floor, permit-ratio floor, Key identity, stale-permit-is-an-ERROR and
ratchet-down are untouched. The three new arms can only **redden** a table:
a permit whose claim is false, a reason with no claim, and a reason whose named
commit cannot be read. Red proof was run for real — restoring the old template
makes `--check` exit 1 and the arms' tests FAIL (see the report's `## Evidence`).

## Not taken, deliberately

- **Repairing `prose-path-line-baseline.tsv`'s sibling reasons.** It is a different
  table with a different generator and a different key; folding it in would be a
  second change in the same diff. Named in the report's `## Not Done`.
- **Re-pointing the 153 drifted numbers.** Unchanged from the sibling devdoc's
  ruling: making the population visible is this line's job; repairing it edits 153
  docs' prose and is the owner's policy call.
- **Auto-regenerating on `--check`.** A check that fixes what it finds cannot
  report a ratchet-up; `--check` stays read-only (Rule 36's own row).
