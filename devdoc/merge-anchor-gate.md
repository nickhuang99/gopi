<!-- doc-class: historical -->
# The merge-time published-anchor gate (⓪e) — a stale document is refused before it lands

## The incident

2026-10-13. A branch commit moved `cmd/agent/cli_log.go`'s routing predicate
602 → 611 and regenerated `COMMANDS.md`'s generated block against a **pre-final
state of that same commit**, committing a block that names `cli_log.go:607`. The
landing that carried it (merge `c52dad2d2`, 00:57:04) is red at its own
revision:

```
$ git show c52dad2d2:COMMANDS.md | grep cli_log.go
| `gopi log` | … | `cmd/agent/cli_log.go:607` | …

$ git show c52dad2d2:cmd/agent/cli_log.go | sed -n '611p'
func isLogSubcommand(args []string) bool {
```

and in a checkout of that merge's own tree,
`go test ./internal/standards/ -run TestCommandsDoc -count=1` is **3 FAIL**:
`TestCommandsDocGeneratedBlockMatchesSource` (line 19 … `cli_log.go:607` vs
`:611`), `TestCommandsDocAnchorsResolve` (607 points at a **blank** line), and
`TestCommandsDocGateRedProofStaleGeneratedBlock`.

## Why nothing caught it

Two halves of the same pipeline had **measured** the drift and neither had
**acted** on it:

- `RebuildAfterMergeMergeset` → `mergeset_gendoc.go` runs on every landing, its
  union **did** contain `cli_log.go`, and its edge **did** answer
  `internal/standards`. But its contract is REPORT-ONLY by design (Rule 12 —
  `res.OK` is never touched by a gate): it writes notify channels and a bug row,
  it does not refuse.
- The arm that used to refuse at the delivery point (`task_finish_gendoc.go`,
  "a non-zero exit REFUSES the delivery") had been deleted **73 minutes
  earlier** by `babef3935`, when the client's trial merge was retired.

So the drift crossed a seam both halves had looked at, and left through the hole
between "measured" and "acted on".

## What this lane repairs

Not the delivery point that lost its arm (its server-side justification is gone),
and not the branch's own pre-push check (row `01a13132ec5779059c0afc1234aaa856`
owns that half: `84627a646`, `finishCleanupBranchAnchorCheck`). It repairs the
seam where the drift is a statement about **a tree nobody has checked out**: the
two revisions' answer *together*.

A branch can be internally consistent — its document regenerated on its own tip —
while its union with the target is not, because the other side regenerated the
same document separately. Measured on this repo 2026-10-12, on the master before
this ground's (three commits: `03f2d1dfe` 07:40, `93e402d36` 13:28, `721268925`
16:18): each moved `cmd/agent/cli_doccheck.go`'s predicate **on its own branch**
and regenerated `COMMANDS.md` **on its own branch**; each branch was green alone,
and the master they landed on carried `cli_doccheck.go:161` in the document and
`:194` in the source.

## Where it lives, and why there

The **⓪e** lane of the merge-decision path
(`internal/tools/merge_anchor_gate.go`), immediately after the ⓪c union gate in
`TaskMergeBranch`. Rationale:

- **No new hook** (Rules 3/35). Nothing ticks, nothing new to remember: it is one
  more member of the gate sequence that already runs before every `git merge`.
- **The edge already exists**: `internal/gendoc` is a leaf package this repo
  consumes, and it answers "which generated document does this change oblige, and
  which package blocks on it" (`GeneratedDocAnswer`).
- **The materialised union already exists** on this path in the sibling lane.
- **Master-safe**: the refusal happens *before* `git merge`, and the branch is
  archived in the same tick, exactly as the BUILD and MANIFEST gates do.

## The measurement

1. Materialise the union of target and branch with **`git merge-tree
   --write-tree`**. This is a **measured** choice, not a convenience: the ⓪c lane
   builds its union with raw `read-tree -m <base> <ours> <theirs>` over an empty
   alternate index, and for two sides changing **different hunks of one file**
   that leaves the index unmerged. Measured on this gate's own fixture pair:

   ```
   $ GIT_INDEX_FILE=$W/a.idx git read-tree -m $BASE $SIDE $MASTER
   f.txt: unmerged (03199100100b54892aac40db233f94b2402a4d8a)
   fatal: git-write-tree: error building trees
   ```

   — so the sibling lane **abstains on the exact shape this lane exists for**
   (correctly: it cannot compile a tree that does not exist). `merge-tree` is the
   union `git merge` would produce, and it touches no index, worktree or ref.

2. Ask `gendoc.GeneratedDocAnswer(unionDir, <the union's own changed paths>)`.
   The change set is **content**-based (`ls-tree -r` blob ids, union vs target),
   never membership: in the incident's shape both sides *carry* the anchor
   source, each in its own revision, and only the blob ids show the union's copy
   is a third revision. A membership test would report an empty change and abstain
   silently on the very case this lane is for — measured while building the
   fixture, which is why the comparison is by content.

3. Run the edge's gate packages **in the materialised union**, branch- and
   package-scoped (`go test -count=1 -json ./<pkg>/...`, Rule 27 — never a
   whole-tree suite), through the BUILD gate's own shared caches
   (`mergeBuildGateEnv`), with the same disk/module/compile failure vocabulary
   the two sibling gates use (`mergeBuildGateUnresolvedModules`,
   `mergeBuildGateDiskExhausted`).

4. Only when the union is **red**, pay for the **premise legs**: the same
   measurement on the **target's own tree** and on the **branch's own tree**.
   Both must PASS for a refusal. This leg is what keeps the lane from freezing a
   queue, and it is not hypothetical: measured 2026-10-13, the integration ref
   this lane was developed against (`4c3c9d047`) is **itself red** —

   ```
   --- FAIL: TestCommandsDocGeneratedBlockMatchesSource
       got:  … cli_scratch_gc.go:572 …   want: … cli_scratch_gc.go:614 …
   --- FAIL: TestCommandsDocGateRedProofStaleGeneratedBlock
       precondition: the committed document's block is already stale
   ```

   Without the premise leg, the FIRST branch offered after such a red state would
   be refused for it.

5. **Abstain is the default** (the merger's detect-never-block contract): the
   escape hatch, no edge, no anchor source in the union's change, a union the
   union merge could not build or that carries conflicts, a gate package the
   union cannot run, a closure that does not **compile** (the union-compile lane
   owns that — on a fused union two branches can make a path hold files from both
   sides, measured 2026-09-30 on master `e791c9c7`), a red target, a branch that
   fails alone, an unusable scratch tree or cache, an unreadable budget, and a
   FAIL that names no failing test (a verdict without its evidence is not a
   verdict, Rule 15). Exactly **one** shape refuses.

## What a refusal does

`internal/tools/task_merge_refuse_anchor.go`:

- `repoSweepArchiveBranch(repo, branch, ArchiveReasonPublishedAnchor)` — a new
  **automatic lane** in the archive vocabulary (`published-anchor`), so the WHY
  is recorded under `refs/gopi/archive-reason/<tag>` like every other lane's.
- A **branch-rework brief** on a new branch off master, deduped through the same
  deterministic summary as the other merge-time refusal lanes. The brief carries
  what a reworker cannot reconstruct: the document's **own failing test output**
  and the **mandate the edge rendered** (what the document is, which generator
  rewrites it, which package blocks on it).
- Master untouched, the repo's index/worktree untouched, no worktree created.

## Not done (honest)

- The **pre-push half** is the sibling row's
  (`01a13132ec5779059c0afc1234aaa856`, `84627a646`); this branch does not carry
  it and does not duplicate it.
- The gate measures **the packages the edge names**. A document whose gate
  package no longer exists in the union is an abstention naming the package, not
  a refusal.
- `mergeAnchorGate` is called from `TaskMergeBranch` only. `gopi merge` and the
  merger tick both route through that function, so both faces get it; a caller
  that reaches `git merge` by another route does not.
- This is **system-layer** work (the merge computation), so per Rule 28 it
  belongs on peerbox.
