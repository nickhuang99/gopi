<!-- doc-class: historical -->
# The Pre-Land Gate — `gopi merge`'s hand-over lane is verified before it hands over

Task: epic `hand-resolve-does-not-verify-master` (2026-09-27).
Branch: `feat/merge-resolve-preland-gate` · worktree `~/workspace/gopi-preland`.

## The defect, as measured

`gopi merge`'s hand-over lane is the ONLY merge path that edits the MAIN
checkout: the child runs `git -C <repo> merge --no-commit --no-ff <branch>`
(`merge_prompt.md` §3.3 — "the ONLY reliable conflict test"), resolves the
conflicts by hand, and **commits that resolution straight to the integration
branch**. Nothing verified what it wrote.

Commit **f068d075** is the cited case, and its own message claims it was
verified:

```
go test ./internal/standards/ -> ok
```

while the same commit

- **dropped** the `<!-- doc-class: historical -->` banner from
  `devdoc/assertion-totality-gate-master-red.md`, and
- **reclassified** that file's row in `devdoc/DOC-INDEX.tsv` (`historical` →
  `normative`, `--` for the evidence column),

leaving master's doc-anchor gate RED. The union lane refuses exactly that shape
by name — *"not unionable: 2 classes in one doc"*. The hand lane landed it.

Measured with git alone, before anything was written:

```
$ git show f068d075:devdoc/DOC-INDEX.tsv | grep assertion-totality
devdoc/assertion-totality-gate-master-red.md	normative	r: ...	--
$ git show f068d075:devdoc/assertion-totality-gate-master-red.md | head -1
# Assertion Totality Gate — master is RED right now      ← no banner
$ git show 562d6dcb:devdoc/DOC-INDEX.tsv | grep assertion-totality
devdoc/assertion-totality-gate-master-red.md	historical	r: ...	devdoc/...   ← AGREES with its own banner
```

## Why a gate, and not a louder paragraph

The child's `go build` / `go vet` / `go test` run **already exists as a report**:
the prompt asks for it, the commit message quotes it, and neither STOPPED the
commit. A claim is not evidence; what was missing is a mechanism that keeps the
commit from happening at all.

## Why not "run the declared gates on the resolved tree and refuse the commit"

The resolved tree does not exist while the command can still refuse. Measured
(git 2.34.1, this host):

```
git merge --no-commit --no-ff side     # conflict: stages 1/2/3, MERGE_HEAD set
<the CHILD edits the files and `git add`s them>   ← its resolution, its index
git commit                              # moves the branch DIRECTLY
```

The tree is produced by the child's own `git add` **after** control has left the
process, and the commit that ends the lane is `git commit`. There is no instant
in between at which the command could read the tree and still withhold the
commit. The declared gates are not cheap readings either — measured here: the
doc-anchor gate 2.4s, assertion-totality 13.3s, build 10.4s, vet 13.3s, per
branch, in a loop. Handing the child back to the command to re-enter a gate
session is also unavailable: the run takes its single-instance lock once and the
child inherits it, so a re-entrant check would deadlock on the command's own
guard. (The post-merge half still exists and still runs:
`internal/tools/rebuild_vet_mergeset.go` measures a merge commit's tree against
its first parent and downgrades a red the base already had.)

## What the gate decides, and how

Two arms, both read from git alone, both answering *"is this hand-resolution
being asked to decide something a merge must not decide?"*

### ① The pre-state

`mergeMeasureConflict` runs its probe in a THROWAWAY worktree, so the main
checkout must be untouched when the child starts. A main checkout already holding
an unfinished merge or staged changes is not that tree — the child's
`git merge --no-commit --no-ff` would run ON TOP of it and commit it. This is the
residue a killed child leaves (the 2026-09-23 incident). **REFUSED.**

### ② The pair the conflict is over

> **Retired mechanism (merge verb removal, 2026-11-01).** The `gopi merge` lines
> below (the invocation and the `gopi merge:` output it printed) are the RECORD of
> what this run executed, kept verbatim; the top-level verb was removed afterwards
> and answers **exit 1** today (`gopi task merge` runs the same engine). The gate
> they demonstrate is unchanged.

The conflicted paths are read in a scratch checkout (detached, at the repo's own
`HEAD`), and for each **documentation path** the manifest's class is read from
**each side of the merge** — via the index stage when that side modified the
manifest, and that side's own revision otherwise. The refusal fires when the two
sides **class the same document differently**:

```
$ ./bin/gopi merge
gopi merge: feat/incident: pre-land gate: FAIL
gopi merge: feat/incident: pre-land gate: refusing to hand feat/incident over —
  the conflict over devdoc/incident-doc.md is a CLASS decision, not a merge:
  the integration branch's copy classifies it "historical" while the branch's own
  copy classifies it "normative". A truth-class is a standing claim about the
  integration branch, so the merge would be choosing which claim master makes —
  an operator's decision, not a merge's (the same shape the union lane refuses as
  "not unionable: 2 classes in one doc"). Master was not touched and no ref was
  moved (master is still its pre-merge tip). Send the branch down the ordinary
  ladder: archive its diff into a task, submit the rework row, delete the ref once
  its diff is archived — and decide the classification deliberately before
  re-running the merge.
[task-notify] merger: rework row "resolve merge conflict: rework: feat/incident" …
```

**Why the class disagreement, and not the file's own banner.** An earlier draft
compared a side's manifest row against that side's document banner and refused
when THEY disagreed. That produces false refusals on the true incident shape: at
f068d075 the branch's row said `normative` and its file carried no banner — which
a row-vs-banner check calls consistent (a `normative` document must not carry a
banner), while the incident is precisely the reclassification `historical` →
`normative` that the diff records. Comparing the two SIDES of the merge is the
fact the conflict is about: taking the integration branch's file leaves master
making a claim the branch's own record denies; taking the branch's class discards
a standing claim master already makes.

**Three-valued, comparative.** A pre-existing disagreement does not refuse the
merge that inherits it — that trap is what `internal/tools/merge_build_gate.go`
was written to avoid, and refusing on it would deadlock the queue on a red master
already has.

| state | verdict |
|---|---|
| the main checkout holds a state this run did not create | **FAIL** (refuse) |
| the two sides class a conflicted document differently | **FAIL** (refuse) |
| every conflicted document carries the same class on both sides | ABSTAIN |
| nothing conflicted / no document involved / a side unreadable | ABSTAIN |
| the operator's hatch is set | ABSTAIN |

## The escape hatch (one knob)

`GOPI_MERGE_PRELAND_GATE=0` turns the gate off — the same contract
`GOPI_MERGE_BUILD_GATE` and `GOPI_MERGE_UNION_GATE` already carry for their lanes.
Its only reader is `mergePrelandDisabled()`.

## The ladder is the existing one

The refusal does not invent an arm. It attaches the branch's diff — already
measured by `mergeMeasureConflict` — to the rework row this command already files
when its ceiling stops a branch (`mergeSubmitRework` →
`tools.MergeCeilingFileRework`, deduped by the lane's own summary, so a re-run
never mints a second row). **The ref is left exactly where it is**: a branch is
never deleted by a refusal; that is the ladder's last rung and belongs to the row
that fixes it.

## Master is byte-identical after a refusal

By construction: nothing has been written to the integration branch yet and the
gate only withholds the child. Measured in the real run below.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK
- Build (all): `go build ./...` → OK
- Vet: `go vet ./cmd/agent/` → OK
- Tests: `go test ./cmd/agent/ -run 'TestMergePrelandGate|TestMergePrelandFilter' -count=1`
  → `ok github.com/nick/gopi/cmd/agent 4.400s` (9 tests)
- Regression: `go test ./cmd/agent/ -run 'TestMerge' -count=1`
  → `ok github.com/nick/gopi/cmd/agent 113.375s` (the whole merge suite)
- Standards: `go test ./internal/standards/ -count=1` → **one FAIL,
  `TestDocClassBannerMatchesManifest`, on `devdoc/full-id-no-prefix.md`** — a
  pre-existing red on master, not this branch's (see `## Not Done`). The
  removal-audit gate that the first draft tripped is now GREEN.
- **Ran (Rule 31)**, the changed surface for real, in a repo built for it:

```
$ cd /home/user/workspace/gopi-test-scratch/preland-e2e/r
$ git log --oneline --all
427c8f7 branch: reclassify normative and drop the banner
7a55435 master: prose
f9753f3 base
$ git rev-parse master
7a554358e35db1d09fbbdd7689cac8be74c10e4b     ← BEFORE

$ GOPI_HOME=… GOPI_SETTINGS=…/s.json ./bin/gopi merge
gopi merge: feat/incident: pre-land gate: FAIL
gopi merge: feat/incident: pre-land gate: refusing to hand feat/incident over — … (as quoted above)

$ git rev-parse master
7a554358e35db1d09fbbdd7689cac8be74c10e4b     ← AFTER: BYTE-IDENTICAL
$ git rev-parse feat/incident
427c8f7…                                      ← the branch SURVIVES
```
- **Result**: the gate refused, named the gate + the file + both disagreeing
  halves, master did not move, the branch survived, and the rework row was filed
  with the branch's diff attached.

## Not Done (honest)

- **A resolution that is wrong without being a class disagreement is not caught
  here** — a semantic edit, a silently dropped hunk. That half is measured after
  the landing by `rebuild_vet_mergeset.go`, which reports rather than rolls back.
  The gate does not claim to cover it.
- **`TestDocClassBannerMatchesManifest` is RED on this branch AND on master**
  (`devdoc/full-id-no-prefix.md`: class `historical`, no banner). Pre-existing,
  untouched by this change, and not repaired here (Rule 26: it needs its own
  dispatch, not a drive-by edit inside a gate change). It is also red in the
  main repo's worktree, i.e. it is master's state, not this branch's.
- **The positive control at the `gopi merge` CLI level was not completed**: the
  CLI arm that reaches hand-over needs a live agent child, and the run was
  stopped after 2 minutes with **master byte-identical (0985ee3d, unchanged)** —
  no partial landing. The control is instead pinned deterministically at the
  gate's own boundary (`TestMergePrelandFilterKeepsACleanBranch`: a code-only
  conflict comes out of `mergePrelandFilter` unchanged, so the child still
  receives it).
- **The gate reads the pair, it does not run the declared gates on the resolved
  tree** — and cannot, for the reason measured in `## Why not …` above. If a
  future change makes the child's resolution available to the parent before the
  commit, the stronger check becomes possible and should be preferred.

## Files

- `cmd/agent/merge_preland_gate.go` (new, ~640 lines)
- `cmd/agent/merge_preland_gate_test.go` (new, 9 tests)
- `internal/tools/merge_build_gate.go` (exported status aliases
  `MergeBuildGateStatusPass/Fail/Abstain` — the three spellings shared with the
  existing gates rather than re-invented)
- `cmd/agent/merge_run.go` (one call site: `mergePrelandFilter(handovers)`
  immediately before the lane claim in `mergeHandOver`, plus the
  `mergePrelandNoHandoverLine` constant)

## Evidence (self-filed)

- `go build ./...` → exit 0, no output.
- `go vet ./cmd/agent/` → exit 0, no output.
- `go test ./cmd/agent/ -run 'TestMergePrelandGate|TestMergePrelandFilter' -count=1`
  → `ok github.com/nick/gopi/cmd/agent 4.400s`.
- `go test ./cmd/agent/ -run 'TestMerge' -count=1` →
  `ok github.com/nick/gopi/cmd/agent 113.375s`.
- `go test ./internal/standards/ -count=1` →
  `--- FAIL: TestDocClassBannerMatchesManifest` (`devdoc/full-id-no-prefix.md`),
  plus `731 markdown docs of a bannered class carry their banner (0
  mislabelled)`; the removal-audit gate's earlier failure on
  `cmd/agent/merge_preland_gate.go:534` is GONE (the site now emits
  `removalaudit.WorktreeRemove` with `ArmMergeRunProbeGround` BEFORE the verb).
- Real run: `git rev-parse master` printed
  `7a554358e35db1d09fbbdd7689cac8be74c10e4b` before AND after the refused merge;
  `git rev-parse feat/incident` printed the branch's tip after it.
