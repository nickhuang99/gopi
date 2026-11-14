<!-- doc-class: historical -->
# The ⓪c union gate's materialisation primitive: `merge-tree --write-tree`, not `read-tree -m`

**Class: historical** — this is the record of one run's measurement and its fix
(task `01a13175`), not a standing claim about master's current shape. The standing
claim (the gate's own contract) lives in the file's doc comment and in the
gate's tests.

## The host this rework was measured on — and what that does NOT change

`git merge-tree --write-tree` is **git ≥ 2.38**, and the host this rework ran on
(peerbox, 2026-10-13) has **git 2.34.1**:

```
$ git --version
git version 2.34.1
$ git merge-tree --write-tree feat/split-hunk master
fatal: unknown rev --write-tree          # exit 128
```

So on THIS host the new primitive cannot execute, and the gate ABSTAINS with
`mergeUnionGateOldGitText`. That is a **host capability**, not a defect of the
fix — and the fix's value is host-independent, because the DEFECT reproduces on
this very host and is untouched by it: the old builder still classifies a clean
split-hunk merge as a conflict here (measured, same script):

```
$ GIT_INDEX_FILE=$SB/idx git read-tree -m $BASE feat/split-hunk master   # exit 0
$ GIT_INDEX_FILE=$SB/idx git write-tree
split.txt: unmerged (…)   ×3
fatal: git-write-tree: error building trees                              # exit 128
$ GIT_INDEX_FILE=$SB/idx git ls-files -u      # ← what the old gate read as "conflict"
100644 7a28df3c… 1	split.txt
100644 1e87327f… 2	split.txt
100644 66aae181… 3	split.txt
```

Two consequences, both deliberate:

1. **There is still NO fallback to `read-tree -m`** (the doc comment's argument
   stands): the old primitive cannot tell this clean merge from a real conflict —
   the conflict control above shows it producing the *identical* stage shape
   (`c.txt` ×3, `write-tree` 128) for a genuine same-line conflict. Falling back
   would re-install the false abstention on exactly the branches the gate exists
   for. An older git abstains, naming the reason.
2. **Both refusal wordings are recognised.** The builder was written and measured
   on git 2.43.0, whose parser answers `unknown option`; git 2.34.1 routes the
   unknown flag to revision resolution and answers `unknown rev` — measured on
   this host, exit 128 for both the clean and the conflicted pair. A
   capability check that knew only the newer wording would have read this host's
   refusal as an ordinary failure and reported a **false reason**; both spellings
   now map to the old-toolchain abstention, and the ambiguity is pinned by
   `TestMergeUnionGate_FailureClassification`.

## The defect (measured 2026-10-13, task `01a13175-bd30-7280-8e1b-81819d1c4e8f`)

`mergeUnionGateTree` built the merge result as:

```bash
git merge-base <master> <branch>                       # base
GIT_INDEX_FILE=<scratch>/union.index git read-tree -m <base> <branch> <master>
GIT_INDEX_FILE=<scratch>/union.index git write-tree    # → the tree
```

`read-tree -m` is an INDEX operation. A path whose two sides differ is recorded
in the index in its UNMERGED state (stages 1/2/3) **whether or not the two sides'
hunks overlap** — and `write-tree` refuses the whole tree. Measured on a
reproduction (one file, two branches, one editing the first line and the other
the last, nothing else anywhere):

```
$ GIT_INDEX_FILE=$W/a.idx git read-tree -m $BASE feat/split-hunk master
$ GIT_INDEX_FILE=$W/a.idx git write-tree
split.txt: unmerged (7a28df3c975fa62270a452251c4e0b24d685c4ba)
split.txt: unmerged (1e87327fd1df3181c39d4dde1c4868b9061b970b)
split.txt: unmerged (66aae18140d4401ef3bdae5b560c29e28b1d1f0d)
fatal: git-write-tree: error building trees        # rc=128

$ git merge-tree --write-tree feat/split-hunk master
92e913645ca513d15717a0a193ac565583d29533         # rc=0
```

The gate read the non-empty stage listing as "textual conflict, the resolver lane
owns it" and ABSTAINED. So on "two branches edited the same file from different
ends" — an ordinary mergeset shape, not a corner — the strongest check this lane
owns (union does not compile ⇒ refuse) was **not in force at all**. The
materialiser was wrong, not the decision rule.

## Why the old builder could not be repaired in place

The old code needed the stage listing to *name* the conflicted paths, so
"clean" vs "conflicted" had to be inferred from a side effect of the primitive.
That inference is not available: on `read-tree -m` a clean two-sided hunk split
and a real conflict leave **the same stages**. The distinction exists only in a
primitive that performs the merge and reports its verdict — `git merge-tree
--write-tree` (git ≥ 2.38), whose exit status *is* that verdict and whose output
carries both the tree id and, on a conflict, the unmerged stage records.

## The shape of the fix

1. `git merge-tree --write-tree <branch> <master>`; on exit 0 the first stdout
   line is the union tree.
2. On a non-zero exit, three named predicates, in order:
   - git's own `unknown option` refusal → ABSTAIN naming the old toolchain.
     **There is deliberately no fallback** to `read-tree -m`: that primitive
     cannot tell the two shapes apart, so falling back would re-install the
     defect on exactly the branches the gate exists for.
   - one of git's unmerged stage records (`^[0-7]{6} [0-9a-f]+ [123]\t<path>`) →
     MERGE_RESULT_UNAVAILABLE, the conflicted paths as evidence, the resolver-lane
     reason text unchanged.
   - anything else → ABSTAIN with git's own reason.
3. Everything downstream (compile-only pass, the branch-alone premise leg, the
   target leg, every ABSTAIN default) is unchanged.

The alternate-index plumbing became dead with the primitive and was deleted
rather than left as decoration (Rule 13/14): `mergeUnionGateGit` lost its
`GIT_INDEX_FILE` parameter and eight constants lost their last caller.

## How the fix is measured

- **Primitive level**: `TestMergeUnionGate_SplitHunkUnionIsMaterialised` asserts
  the materialised tree, not just the verdict — "not refused" is also what an
  abstention returns, which is how the defect survived. Red proof: with the
  PRE-FIX builder restored, `read-tree -m` + `write-tree` on that same fixture
  leaves three unmerged stages and exits 128 (script + captured output under this
  run's session `artifacts/`), which is exactly the reading the old gate took as
  "conflict". The arm is capability-aware: where the host's git can perform the
  merge it asserts the materialised union and the `PASS` line; where it cannot
  (git < 2.38, this host) the arm requires the OLD-TOOLCHAIN abstention *by
  name*, so an unstated abstention — indistinguishable from the defect — still
  fails. Both hosts therefore stay red on a regression to `read-tree -m`.
- **Paired arm**: `TestMergeUnionGate_TextualConflictAbstains` (both sides
  change the same line) still abstains with the resolver-lane reason *and* names
  the conflicted path in its log line, so "stopped abstaining on split hunks" and
  "stopped abstaining on conflicts" stay distinguishable. Strengthened here: it
  previously asserted only `Refused == false`, which a compile pass also
  satisfies.

## The rework: the consts the ⓪e anchor gate still referenced

This change deletes `mergeUnionGateGit`'s `GIT_INDEX_FILE` plumbing, and the
⓪e anchor gate (`internal/tools/merge_anchor_gate.go`) still called the sibling's
helper with an alternate-index argument and read `mergeUnionGateIndexEnvName`.
Deleted in this same change, so the tree compiles and the anchor gate's behaviour
is unchanged:

- three call sites of `mergeUnionGateGit` dropped their index argument — the two
  revision resolutions (`mergeAnchorGateTreeRun`, `mergeAnchorGateChangedPaths`)
  and the `ls-tree -r` read (`mergeAnchorGateTreeBlobs`). All three read objects
  only, so the alternate index was never load-bearing for them; the resolved
  values are identical.
- `mergeAnchorGateIndexEnvName = "GIT_INDEX_FILE"` is declared in the anchor
  gate's own const block, beside its ONE reader (`mergeAnchorGateGitEnv`, which
  drops a caller's value from every git child the gate starts). The anchor gate
  builds no index of its own, so the name exists for that drop alone.
- `mergeAnchorGateIndexFileName` lost its last caller and was deleted with it.

**The historical fixture was repaired the same way, and deliberately so.** The
copy under `internal/standards/testdata/config-doc-historical-anchor/` is a
verbatim byte-copy of `internal/tools/merge_anchor_gate.go` at `5872b341e`
(measured: identical to that revision, to master, and to this branch's base), read
only by the same-identity red proof (`config_doc_gate_test.go`), which uses — from
it — the CONFIG.md row lines, `mergeAnchorGateTimeout` and
`mergeAnchorGateDisabled`. It is a miniature of the tree, not a frozen blob of a
past. Leaving it with a reference to a constant that no longer exists anywhere
would have planted a broken copy in the corpus the doc gates read, and Rule 36's
standing is that a documented claim names code that RESOLVES. So the four edits
are the same four edits, and the lines the proof depends on (854 the bare
comment, 952, 975) are unmoved: the const block's line count is preserved through
the replacement, and the tests confirm it.

## Recorded misreading (a retraction, kept on purpose)

The first draft of this document claimed that
`TestMergeUnionGate_TextualConflictAbstains` was passing while proving nothing —
"the fixture reports a conflict but the gate abstains with `conflicts == ""`".
**That claim was wrong, and it was an inference I never measured.** The
measurement that settles it: with HEAD's implementation and HEAD's test file both
restored (verified by blob hash), the original arm runs and

```
[task-notify] merger: UNION gate ABSTAINS for feat/union-conflict ((the union tree
  could not be written — it has textual conflict stages, which the resolver lane
  owns): conflict.txt) — merging unchanged
--- PASS: TestMergeUnionGate_TextualConflictAbstains (0.04s)
```

— it reaches the conflict read and abstains for the right reason. The arm was
sound; it was my reading of it that was not.

**Where the misreading came from, because the same route is open to the next
reader**: the arm declares itself heavy (`heavytest.Require`), so on the default
path it is SKIPPED — and a skip prints the test's name in a stream full of
`--- PASS` lines. Read carelessly, "skipped" looks like "ran and passed", and the
conclusion flips from "this was never measured here" to "this is broken". Then
the round's one *real* observation — the new arm, run while the heavy signature
was stripped — was read as confirming the hypothesis instead of being asked what
it said.

The lesson is this repo's own Rule 41 one: an inference is not a judgement.
A hypothesis about another test's behaviour is settled by running that test, not
by reasoning about its source, and the cheap measurement was one command away the
whole time.

**The genuine weakness the episode did surface** (and this one IS fixed):
the original arm asserted only that the gate did not refuse. Both a conflict
abstention and the ⓪c compile pass return `Refused == false`, so the assertion
could not tell "the resolver lane owns this" from "the gate compiled the union".
The arm now captures the gate's log line and asserts the resolver-lane reason
plus the conflicted path — which is what the retraction's own evidence line shows
it doing.
