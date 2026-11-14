<!-- doc-class: historical -->
# The evidence hold-out table ran to its end — the stale rows deleted, and why THIS branch's rerun kept master's copy

> **historical**: this is one run's record (task `01a12d60-cd18-74b1-8447-963e8b9e1c29`,
> 2026-10-12, host `hostb-Laptop`). The figures below are **a snapshot at named commits**,
> not a claim about master. The standing claim the underlying work left behind is the code
> and its tests (`internal/standards/doc_evidence_gate.go`), which are normative by nature.

## What was asked

Branch `task/01a12d06c02f71008b77c0580690cdf1` (tip `bc8e2ea8c279c86a6682c50853cb6bbfde05f79b`,
one commit: *"the evidence hold-out ratchet runs to its end — delete the two stand-ins the
merge union outlived"*) could not be merged: `gopi merge` hit content conflicts in
`internal/standards/doc_evidence_gate.go` and `doc_evidence_gate_test.go` and archived the
branch before deleting any ref. The task: re-express that intent on the CURRENT master tip,
reconciling the two files **substantively** — including recognising, with evidence, that the
same intent may already be landed, in which case "resolving" means accepting master's content
rather than re-applying a stale hunk.

## The measurement that decided it (evidence, not inference)

The branch's own commit is still readable in the peer's object store (`peerbox`,
`/home/user/work/GoPi`): `git rev-parse bc8e2ea8^{commit}` resolves. Its diff touches four
files — the two conflicted ones, a new devdoc, and one `DOC-INDEX.tsv` row.

The two conflicted files, as blobs:

| revision | `doc_evidence_gate.go` | `doc_evidence_gate_test.go` |
|---|---|---|
| this branch, before the resolution | `5ef02751a444b216bb06f5829f967abcdde9243d` | `b0d518609d2ce3786361e81374c2601917190d0f` |
| peer master `118bb3c47` | `5ef02751a444b216bb06f5829f967abcdde9243d` | `b0d518609d2ce3786361e81374c2601917190d0f` |
| `git diff 118bb3c47 HEAD -- <both files>` | **empty** | **empty** |

**The branch's tree and master's tree already carried byte-identical copies of both files** —
the same blob hash, which is the only spelling of "the same content" git has. There was no
hunk left to re-apply: the conflict was between two sides that had converged on the same
bytes, so *the resolution is master's own content, unchanged*, and applying the branch's
older diff would have been the stale-hunk error the task warned against.

## Why the branch's intent is landed, and how it got there first

The branch's intent was to empty `docEvidenceHeldOut` (two entries whose drift no longer
fired) and to keep the ratchet exercisable with the shipped table empty. Measured on the
integration trunk:

```
$ git show 225aa45ef --format="" --stat | head -3
225aa45ef fix(standards): ratchet the evidence-freshness gate's hold-out table to empty
 devdoc/doc-evidence-freshness-gate.md        |  28 ++++
 internal/standards/doc_evidence_gate.go      |  30 +++-
 internal/standards/doc_evidence_gate_test.go |  15 ++

$ git show 8c5f4812e:internal/standards/doc_evidence_gate.go | grep -n 'docEvidenceHeldOut = '
```

`225aa45ef` (a sibling of the gate's own introducing commit) emptied the table and recorded
the two deleted entries' provenance in the doc comment above it — the same intent, landed
independently, and **with the record the conflict could otherwise have destroyed**. `d8b77253f`
then added the birth-time guard, which subsumes the test half: with a guard that refuses an
entry added since the base whose drift does not fire, the fixture-driven ratchet case the
branch proposed is no longer the only way to exercise the contract at an empty table.

Master's copy records, **in the file itself** (lines 61–75 of `doc_evidence_gate.go`), the
task (`01a12d1b-7599-7aa8-8585-6a2d20ef7fbe`), the two deleted paths, the upstream commits
(`8a0d75d9b`, merged by `82777a9f`), and why the entries were born stale. Nothing was silently
dropped: the deleted entries' identities and the reason they died are in the landed file, and
this document carries the branch-side record that the merge could not.

## The stale-hunk hazard, measured rather than assumed

Accepting master's content is only "the resolution" if master's content is the *superset*.
Applying the branch's own diff instead would NOT have been harmless: its hunks rewrite the
same doc comment into a shorter form and change the stale predicate's signature to
`docEvidenceStaleHoldOuts(held, rep)`. That signature is **gone from master's copy**, which
reaches the same goal by extracting a pure predicate instead:

```go
func docEvidenceStaleHoldOuts(rep docEvidenceReport) []string   // master: 1 argument
func docEvidenceStaleIn(released []string, fired map[string]bool) []string  // the fixture seam
```

So the branch's hunk is not merely redundant — it is obsolete, and re-applying it would have
reverted master's refactor. Measured consequence of the *rejected* path: the branch, before the
resolution, was **224 commits behind** the trunk with a non-empty `git status`-level deviation
across `internal/standards/`; after resolving to master's content, `git diff 118bb3c47 HEAD --
internal/standards/` is **empty**.

## What this branch actually delivers

1. The two conflicted files reconciled **substantively**: master's landed content, accepted
   with the evidence above rather than re-derived; neither side dropped, and the resolution is
   recorded here and in the report rather than performed silently.
2. The branch-side record the merge destroyed: this document, classified `historical`.
3. The archived branch's own diff, `independent` of the peer's live store, captured under this
   run's session directory (`artifacts/branch-diff-2files.diff`) before the object could be
   collected.

## Honest limits

- **The peer-side archive had already failed to keep the commit reachable.** `git for-each-ref
  --contains bc8e2ea8` on the peer returns **nothing** — no `refs/tags/archive/...`, no
  `refs/gopi/archive-reason/...`, and `git fsck --dangling` does not report it either. The
  commit survives unattached and is pruned by the next `git gc`. This run read it in time and
  filed the defect as its own task (`01a12f48-7ca7-74bf-8f63-2a8fd3f8dd22`); it is *not* fixed
  here (Rule 28: archiving is the system layer).
- **The scoped smoke is not fully green on this tree, and the reds are master's, not this change's.**
  `make test-budget PKGS='./internal/standards/...'` reports 3 failing tests
  (`TestStandardGates`, its `TestGate3ElseAfterReturnRedProof` subtest, and
  `TestStandardGateDataReport`) against 184 passes. They are **provably pre-existing on master**:
  the entire `internal/standards/` tree on this branch is byte-identical to peer master
  (`git diff 118bb3c47 HEAD -- internal/standards/` is empty), as is the file the Rule-18
  violation sits in (`internal/scratch/sandbox_gc.go`, blob `f2ed8aa6` on both). This change
  cannot have introduced what it does not touch.
  **Measured precisely on the trunk, this is a MASTER defect and it was filed as one** (Rule 41,
  evidence before prose): the gate's own output is `Rule 18 (no else after terminating if):
  hits=3 baselined=0 escaped=2 NEW=1`, naming
  `internal/scratch/sandbox_gc.go:247` — a real-tree violation with no baseline, which is also
  why the red-proof subtest fails. `TestStandardGateDataReport` additionally fails on pre-existing
  *escapes* (Rules 19/20/13), and those report `NEW=0`. Filed as row
  `01a12f51-69d6-70a3-9496-7c9a7e7cf11c` (`--tags bug`), with the raw output and the blob-parity
  proof in the run's session directory. Not fixed here (Rule 28: the standards gate is the system
  layer) — the consequence, worth naming for whoever reads this next, is that **every** run that
  scopes its smoke to `./internal/standards/...` inherits this red and must write an honest
  baseline-vs-delta paragraph for it.
- **The manifest row is the write hook's, not this run's hand.** `write` classified
  `devdoc/doc-evidence-holdout-ratchet-end.md` the moment the bytes landed (Rule 36), inserting
  its row at `DOC-INDEX.tsv:632` with the shape the corpus's writer produces for a `historical`
  doc. This run had ALSO added a row by hand (written carrying the branch's row verbatim, as
  stale-row insurance); the gate then refused the duplicate
  (`TestDocManifestCoversEveryDevdocFile`: "duplicate row … 2354 rows cover 2353 files"), the
  hand-added row was deleted, and the docclass gates went green on the hook's row alone. The
  hand row was **not** the deliverable and its removal cost nothing — recorded here because a
  report that said "row added by hand" would describe a state the tree never shipped.
- Rows surviving on this tree: 1 for this doc (`doc-evidence-holdout-ratchet-end.md`, class
  `historical`); measured `grep -c` → 1, and
  `go test -count=1 ./internal/standards/ -run 'DocManifest|DocClass|DocEvidence|DocIndex'` → `ok`.
