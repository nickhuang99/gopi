<!-- doc-class: normative -->

> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.

# A docclass freeze table is green on ONE tree — the sibling-merge race, re-measured after 974a3f456

**Task** `01a12fb8-9fea-7b40-889a-d1bc554156eb` · **host** `hosta` ·
**base** `8922d775a` · **branch** `task/01a12fb89fea7b40889ad1bc554156eb`

Rule 41 row, filed from HP's ledger (discovery run `01a12f8d`). This document is the
MECHANISM, the DECISION, and the recipe a reader needs when they meet the symptom.

## The mechanism (measured, not inferred)

A docclass freeze table (`internal/docclass/testdata/anchor-number-baseline.tsv`) is a
census of **one tree**. Two things produce a citation: `devdoc/DOC-INDEX.tsv`'s anchor
cell (written by `tools/docclass/classify.py`) and the freeze table (written by
`internal/docclass/testdata/anchor-number-census.go`). **Neither is a precondition of the
other, and both are regenerated on whatever branch is at hand** — so two branches can each
run the corpus's OWN generator and neither can see the other's in-flight edit.

The measured instance (`git merge-base --is-ancestor` BOTH directions = exit 1, i.e. the
two commits are SIBLINGS):

```
22c3ea6f2  2026-10-12 16:40:22  re-points devdoc/rule17-gate-report.md's manifest cell
                                :43 → :107 (generator, not hand-edit; TEXT unchanged)
7a64b9ad0  2026-10-12 16:51:24  regenerates the freeze table on a tree where :43 still
                                drifted, freezing that row as a permit
dbcf618da  2026-10-12 17:28:30  ratchets the now-stale permit back down
```

Reproduced here with the real generator, on a scratch clone
(`/home/user/workspace/gopi-anchor-probe`, `--no-hardlinks`, off `/` per Rule 22) whose only
edit is `git checkout 22c3ea6f2^ -- devdoc/DOC-INDEX.tsv` — i.e. the tree sibling B's census
ran on:

```
$ go run internal/docclass/testdata/anchor-number-census.go . <tmp>   # sibling B's act
anchor-number-census: 237 anchor row(s) scanned, 157 incoherent (4 new permit(s), …) → wrote <tmp>
  <tmp>:134: devdoc/rule17-gate-report.md  number-drifted  internal/tools/testmain_test.go:43  …

$ git checkout HEAD -- devdoc/DOC-INDEX.tsv                          # sibling A's re-point lands
$ go run internal/docclass/testdata/anchor-number-census.go . <tmp> --check
anchor-number-census: scanned 238 anchor row(s), 156 incoherent; table carries 157 row(s)
  STALE PERMIT (in the table, no longer drifts): devdoc/rule17-gate-report.md …
  → exit 1
```

**So: a branch that regenerates a freeze table can hand master an exit-1 tree through a
merge that is itself conflict-free.** No generator wrote anything wrong in either commit.
`STALE PERMIT` is the deterministic output of the merge, not a defect in either half.

## Is the race still live after `974a3f456`? — yes, but its DAMAGE was halved

`974a3f456` ("key an anchor permit on the claim, not the line number") replaced the permit
identity with the claim:

```go
func (f AnchorFinding) Key() string {                    // internal/docclass/anchor_number.go:124
	return strings.Join([]string{f.Doc, f.Path, f.Text}, AnchorNumberBaselineFieldSep)
}
// pre-fix, at 974a3f456^:
//	return strings.Join([]string{f.Doc, f.Verdict, AnchorNumberAnchorCell(f), f.Text}, …)
```

Re-measured at this branch's HEAD, both halves of the answer:

- **The bogus half is gone.** `22c3ea6f2`'s re-point changed the recorded NUMBER only. The
  claim (`doc | path | text`) is byte-identical across the re-point, so the key is
  identical and **the permit is not orphaned by the re-point itself**. Pinned by
  `TestAnchorNumberSiblingLineRePointKeepsThePermit`, whose red arm
  (`TestAnchorNumber…` at the pre-fix key) is quoted in the task report.
- **The stale-permit half remains, and it is CORRECT.** The sibling's re-point **repaired**
  the drift, so the permit frozen for it is a permission nobody needs any more. Ratcheting
  it down is exactly what the table's contract already says (`Ratchet DOWN only: a row whose
  site no longer drifts is an ERROR`). A lone branch doing this repair on its own tree would
  get the very same verdict from the very same generator — **the merge does not create a
  defect here, it makes an existing repair visible.**
- **What still orphans a permit is a re-point of the CLAIM** (a different anchored file, or a
  different recorded TEXT). That case reports BOTH arms — the old permit stale AND the new
  claim unpermitted — pinned by
  `TestAnchorNumberSiblingClaimRePointStillOrphansBothArms`. It is loud and attributed.

## The decision: ACCEPT and self-heal — do NOT block a re-point on its freeze consumers

Worst case a sibling merge can now produce is **an over-permissive row** (a `STALE PERMIT`);
it can **no longer** produce an unpermitted drift-vs-permit disagreement for an unchanged
claim. Accept, for four measured reasons:

1. **The failure is loud and attributed.** `census --check` prints `STALE PERMIT …
   <doc> <path> <text>` by name and exits 1, under a header that names the generator and
   the freeze commit (Rule 38). Nothing is silent.
2. **It is already a suite gate.** `TestAnchorNumberGateOnTheRealTree`
   (`internal/docclass/anchor_number_gate_test.go`) treats a stale permit as an ERROR, so
   the same verdict is a red test wherever the suite runs — the enforcement is machinery
   that already exists (Rule 35: no new dependency).
3. **One generator run repairs it.** `go run internal/docclass/testdata/anchor-number-census.go . internal/docclass/testdata/anchor-number-baseline.tsv`
   is the whole repair — never a hand-edit. The measured diff is one deleted row.
4. **A block cannot be built where the race happens.** A gate that refuses "a commit that
   moves a published anchor source" would have to know whether some freeze consumer, on a
   branch it cannot see, still needs the old cell. On one branch that knowledge does not
   exist; cross-branch it would need a new oracle over branch heads — a new dependency
   (Rule 35) that guesses. Worse, it would **block the repair itself**: re-pointing a drifted
   anchor through the corpus's own generator is the corpus's prescribed fix, and a re-point
   is what a freeze consumer must eventually follow.

The cost accepted, stated honestly: a waiter (`stale permit`) instead of a silence, and one
generator run on the branch that meets it — measured cost of the whole instance: one commit
(`dbcf618da`), one deleted row, zero claim changes.

**Measured maintenance frequency, which is the other half of the cost/benefit.**
`git log --oneline 8922d775a -- internal/docclass/testdata/anchor-number-baseline.tsv | wc -l`
= **16**, and the recent entries are almost entirely this class of repair on this one table
(`dbcf618da` ratchet-down; `a70f2c80a`, `d4542073a`, `bc10e8d78`, `3ecc16d5d` regenerations
after a sibling's own edit moved anchored lines). So the accepted cost is not hypothetical and
not rare — it is a routine, single-command repair that the branch that meets it performs, and
it is precisely the price of the decoupling that makes two branches able to cite the same
manifest without a cross-branch precondition. The sibling tables are cheaper still
(`prose-path-line-baseline.tsv` 5 commits, `prose-anchor-baseline.tsv` 1), and `DOC-INDEX.tsv`
carries 2081 — the manifest is touched constantly, which is exactly why keying a permit on a
manifest cell's arithmetic was the wrong identity (fixed by `974a3f456`).

## Operational recipe — sibling-transient stale permit vs real drift

```
# 1. WHAT does the check say? (names the key, exits 1)
go run internal/docclass/testdata/anchor-number-census.go . \
    internal/docclass/testdata/anchor-number-baseline.tsv --check
#    "STALE PERMIT …"            → a permission nobody needs (below)
#    "NOT PERMITTED …"           → real unpermitted drift (see 4)

# 2. Is it a sibling? BOTH directions must be false (= siblings; Rule: `merge-base --is-ancestor`)
git merge-base --is-ancestor <re-point-commit> <freeze-commit>; echo $?   # 1 = not an ancestor
git merge-base --is-ancestor <freeze-commit> <re-point-commit>; echo $?   # 1 = not an ancestor
git log -1 --format='%h %ci %s' <both>

# 3. REPAIR — through the generator, never by hand (it will delete the row)
go run internal/docclass/testdata/anchor-number-census.go . \
    internal/docclass/testdata/anchor-number-baseline.tsv
git diff --stat   # expected: the stale row's line removed

# 4. A STALE PERMIT and a NOT PERMITTED row for the SAME doc|path, different texts =
#    the claim itself moved → the re-point must be frozen/unfrozen on purpose, not by an
#    arithmetic guess. Read the doc's citation; do not permit a claim you cannot explain.
```

`gopi tool doccheck` is the same verdict on the human surface; `-h` is not a run.

## What this document does NOT claim

- The re-point in step 3 is not re-tested against the OTHER freeze tables
  (`prose-path-line-baseline.tsv`, `prose-anchor-baseline.tsv`): same shape, same two
  generators, not measured here.
- Nothing in the merge/worktree/task-ledger machinery was touched (Rule 28: this
  diagnosis+decision belongs to the corpus layer; a repair in that machinery belongs on
  peerbox's ledger).
