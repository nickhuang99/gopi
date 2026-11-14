<!-- doc-class: historical -->
# The conflict-marker gate — and why the marker in the tree stays

Gate: `Rule 16-21 family (no unresolved conflict marker in a text file)`
Code: `internal/standards/conflict_marker_gate.go` (registry entry, empty baseline),
`internal/standards/gate.go` (`conflictMarkerFind`, the text-file scan),
`internal/standards/conflict_marker_gate_test.go` (red proof + the tree check).
Run: `go test ./internal/standards/ -run 'TestGateConflictMarker|TestStandardGates' -count=1`

## The dispatch, and what the tree actually held

The task said: master ships `devdoc/repo-context-gate-reverify.md` with a literal
unresolved conflict marker pair, delete the marker lines and the rejected side,
acceptance `grep -c '^<<<<<<<' … → 0`.

Measured on master (`a9a96cd7`):

```text
$ git show master:devdoc/repo-context-gate-reverify.md | grep -n '^<<<<<<<\|^>>>>>>>'
204:<<<<<<< HEAD
208:>>>>>>> fix/heavy-tests-classification
```

Both hits sit INSIDE a fenced code block — the fence opens at line 203, the block
closes at 209 — and the surrounding prose (lines 199-201) *introduces* them:

> The conflicts are the important half. Both are the SAME 3-line shape — the
> sibling inserts its `heavytest.Require(...)` call at exactly the offset where
> this branch inserts `SandboxRepoContext(t)`

So the pair is the document's own quoted evidence — the verbatim unmerged output
of the `fix/heavy-tests-classification` merge, recorded because the document's
subject is *that merge's conflict resolution* ("the gate catches the wrong
resolution"). There is no half-resolved merge and no document corruption:

```text
$ git grep -n -E '^(<<<<<<<|>>>>>>>) ' master -- .
master:devdoc/repo-context-gate-reverify.md:204:<<<<<<< HEAD
master:devdoc/repo-context-gate-reverify.md:208:>>>>>>> fix/heavy-tests-classification
```

Two hits, one file, one quote. (Same finding independently recorded in memory
#3164 §5, #3170 and #3247 — the "grep `^<<<<<<<` false positive on committed
prose" family.)

**Therefore the briefing's prescribed fix was refused.** Deleting the two marker
lines and the rejected side would have deleted the evidence the section exists to
carry, to satisfy a scan — the exact inversion Rule 27's sibling case describes
(never weaken the subject to fit the instrument). The acceptance criterion
`grep -c '^<<<<<<<' → 0` is *unsatisfiable by deletion* for a document whose job
is to show what a conflict looks like.

## What was done instead

**1. The quote is now unmistakable (non-destructive).** One paragraph was
inserted before the fenced block in `devdoc/repo-context-gate-reverify.md`,
naming the block as quoted verbatim, the marker lines as git's own unmerged
output rather than residue, and the gate that reads it as a quote. The two marker
lines are untouched — the file's `grep -c '^<<<<<<<'` is **still 1**, on purpose.

**2. The real gate concern was implemented — fence-aware.** The briefing's second
half ("consider whether a cheap repo-wide conflict-marker gate belongs in
internal/standards") is sound and is the genuinely useful deliverable: a merged
file that silently accepted an unresolved marker has no other instrument.

The gate is a text gate: it walks the whole module tree (the motivating case
lives under `devdoc/`, so `prodRoots` — `internal` + `cmd`, Go-only — cannot
express it), skips `.git`, skips files above 8 MiB and files with a NUL in their
head, and reports a marker only when the line is **outside a fenced block**.

Design points, each with a reason:
- **Fence-aware, or it is worse than nothing.** `conflictMarkerFind` tracks the
  opening fence run and closes only on a run at least as long (a nested fence
  inside a document does not close its parent). Without this, the only way to
  satisfy the gate on the tree as it stands would be to delete the quote — i.e.
  the gate would *cause* the defect it was written to prevent.
- **Vocabulary over substring.** A hit is a column-0 run of exactly seven `<`/`>`
  followed by end-of-line or ` <ref>` (`conflictMarkerRe`). Indented runs (a
  nested quote), a run inside a sentence, a longer run, and the `=======`-only
  banner shape are all silent — measured on the tree: candidate hits go from 2
  (naive `^<<<<<<<` grep) to **0** with the real predicate.
  `=======` is deliberately NOT in the vocabulary: it is a legitimate Markdown
  setext heading underline and appears in this repo's own documents, and a git
  marker with no `<<<<<<<`/`>>>>>>>` partner is not what a half-resolved merge
  looks like.
- **The gate does not satisfy itself.** The marker runs are assembled from
  `conflictMarkerOpener`/`conflictMarkerCloser`/`conflictMarkerRunLen` (Rule 14:
  one definition), so no file in the repo — including the gate's own source and
  tests — contains the seven-character literal run.
- **Empty baseline, and that is the point.** An unresolved marker is never an
  acceptable state, so there is nothing to permit. The one marker shape in the
  tree is not a hit at all (it is fenced), and a baseline entry for it would be
  the mechanism by which a future agent deletes the quote.

## Verification (self-filed observations)

```text
$ go build ./...                      # exit 0, no output
$ go test ./internal/standards/ -run 'TestGateConflictMarker' -count=1 -v
--- PASS: TestGateConflictMarkerRedProof (0.00s)
--- PASS: TestGateConflictMarkerNeverSatisfiedByDeletingTheQuote (0.05s)
$ go test ./internal/standards/ -run 'TestStandardGates/TestGateConflictMarkerRedProof' -count=1 -v
    gate_test.go:100: Rule 16-21 family (no unresolved conflict marker in a text file):
                      0 total hits, 0 baselined, 0 escaped, 0 new (unannotated), 0 baseline entries
--- PASS: TestStandardGates/TestGateConflictMarkerRedProof
```

**The red proof on a REAL file** (not only a synthetic probe). A throwaway
`devdoc/zz-marker-probe.md` carrying one unfenced `<<<<<<< HEAD` line was written
into the worktree and the gate run:

```text
gate_test.go:112: … NEW violation at devdoc/zz-marker-probe.md:3
  [conflict marker "<<<<<<< HEAD" in a text file — resolve the merge, or
   (if the marker is QUOTED evidence) put it inside a fenced block …]
--- FAIL: TestStandardGates/TestGateConflictMarkerRedProof
```

The probe file was then deleted (`git status --porcelain` clean of it). This is
the observation that matters: the walker reads real files off disk, and the gate
fires on one — a probe-only proof could pass while the walker was broken.

## Not done (honest)

- **`grep -c '^<<<<<<<' devdoc/repo-context-gate-reverify.md` is 1, not 0.** That
  is the deliberate deviation from the briefing's acceptance criterion, with the
  reason above. If the operator wants the literal count at zero, the only honest
  way is to move the sample out of the document (delete the evidence) — this
  branch does not do that, and says so rather than quietly deleting two lines.
- **`TestStandardGates/TestGate2TestEnvRedProof` is RED — identically on master.**
  The gate reports one Rule 17 violation, `cmd/agent/merge_lock_test.go:200`
  (`os.Setenv("GOPI_HOME", home)`, added by `5c8a8fac`). Measured on a clean
  detached worktree of master (`a9a96cd7`, `/home/user/workspace/gopi-master-probe`,
  removed afterwards):

  ```text
  --- FAIL: TestStandardGates/TestGate2TestEnvRedProof (0.45s)
      gate_test.go:99: Rule 17 (test env isolation): 115 total hits, 91 baselined,
                       23 escaped, 1 new (unannotated), 86 baseline entries
      gate_test.go:111: … NEW violation (checklist 5.2) at cmd/agent/merge_lock_test.go:200
  ```

  The figures are IDENTICAL on this branch's base (`1b3d54b2`) and on master, so
  the red is pre-existing and unrelated to this change (a different rule, a
  different file, a different task's commit). It is left untouched here — not
  rebased away, not baselined into this branch — and filed as its own bug
  (`01a0b5b6-0957-7e98-99fb-635930f0baad`, tags=[bug], payload carries the repro,
  the measured output and the shape judgement: the `t.Setenv` "fix" would invert
  the test, so the reviewed per-line `// env-leak-ok:` escape is the answer).
  Every OTHER gate in the registry reports
  `NEW=0`, and the new conflict-marker gate reports `hits=0 NEW=0`.
- **A tree check that the document KEEPS its quote** is not added outside the
  gate: the property is pinned where a reviewer reads it, in
  `TestGateConflictMarkerNeverSatisfiedByDeletingTheQuote` (the file must exist,
  must still carry a fenced block, must be inside the gate's scan, and must
  report zero hits). A separate documentation-lint pass would be a second place
  for the same invariant (Rule 14) and is not worth a new gate.
- **The `grep`-based gate is not itself git-aware.** It reads the working tree
  (a conflict marker is a file property, and untracked files are exactly where a
  tool-generated marker has been found before), so it reports a marker in an
  untracked scratch file too. That is intended.
