<!-- doc-class: normative -->
# The regenerate lane — resolving a generated table whose conflict is a SCALAR collision

**Task:** 01a133a5-8ba5-71f6-9645-d15f940c7bae (2026-10-14, peer1).
**Decision:** the merge machinery resolves conflicts on
`internal/docclass/testdata/anchor-number-baseline.tsv` by **running the
artefact's own generator over the merged tree**, never by unioning rows.

## 1. The cost, measured

`gopi task list` on this ledger, 2026-10-14:

| figure | value |
|---|---|
| pending rows naming this file family | 62 |
| pending rows naming **this** table | 6 |
| pending rows naming `devdoc/DOC-INDEX.tsv` (which already had a lane) | 37 |

Every one of the six carries the same instruction — *"regenerate, do not
hand-merge"* — and the mechanism is the one `devdoc/merge-union-lane.md` already
records for the manifest: the deciding pass (then spelled `gopi merge`; today
`gopi task merge`) decides branch 1, lands it, then decides branch 2 **against a
master branch 1 has already moved**. A branch that was
text-clean when the run started rewrites the same whole file.

The union lane was extended, in the same epic, to `DOC-INDEX.tsv` — and it does
**not** transfer to this artefact. That is the design question this change
answers.

## 2. Why a row-UNION is wrong here (the anatomy, measured)

A union of the two sides' rows is the right operation for an artefact whose
meaning **is** its row set. This table's meaning is not:

1. **Its header carries SCALARS.** The one count-bearing line is
   `# figures: frozen=… pre-existing=… first-frozen=… ambiguous=…`
   (`internal/docclass/anchor_number_baseline_header.go`), and `--check`
   **re-derives every one of them** and compares. Measured on the lane's own
   fixture (`evidence/build-regen-e2e.log`), two sides that insert one line each
   into two different files produce:

   ```
   branch: # figures: frozen=143 pre-existing=124 first-frozen=19 ambiguous=38
   master: # figures: frozen=143 pre-existing=124 first-frozen=19 ambiguous=38
   ```

   — i.e. the population is a function of the **merged tree**, and no row-level
   operation over two sides can compute it. `frozen` is the row count the union
   could count; `pre-existing` / `first-frozen` come from a **provenance probe
   over a commit**; `ambiguous` is a live property of the anchored files. Three
   of the four are unreachable from two texts.

2. **Its rows are a RATCHET-DOWN allow-list.** A permit whose site no longer
   drifts is an **ERROR**, refused by the generator's own `--check` as
   `STALE PERMIT (in the table, no longer drifts)`. A union cannot know whether
   the merged tree still exhibits a permit, so it would either **resurrect** a
   permit one side deliberately deleted (loosening a gate) or **mint** one
   neither side voted on (tightening it by a convenience — Rule 36/38 forbid
   both).

3. **A permit's reason is a claim about a COMMIT.** `first-frozen at <C>` /
   `pre-existing at <C> freeze` is decided by `AnchorNumberProvenanceProbe` over
   that commit's own tree; a second writer can only copy or invent it.

So the honest resolution is the one the ledger rows already spell: **regenerate
by construction**. What was missing was not the instruction but the
**machinery** — every landing that hit the conflict became a human/agent rework
row.

## 3. What changed

`mergeUnionArtefact` gains a declared `kind` (Rule 13: stated, never inferred at
the call site), and the census table is declared on the new shape:

```go
{path: docclass.Path,                        kind: mergeUnionKindUnion,      …}
{path: docclass.AnchorNumberBaselineRel,     kind: mergeUnionKindRegenerate, cmd: docclass.AnchorNumberCensusCmd}
```

`mergeUnionResolve` is the ONE dispatch between the two shapes. The lane
(`cmd/agent/merge_union_regen.go`) then runs, inside the merge probe's own
scratch worktree and with a pinned checkout of the integration branch as its
toolchain:

1. **① the pinned trust gate** — `go run <generator> . <pinned>/<table> --check`
   in the pinned checkout of the **integration branch's own commit**. The
   question is the one the lane actually needs answered before it may trust a
   regeneration: *is the target's committed table the tree it ships?* A target
   that is **already incoherent by itself** — a sibling moved a line and did not
   regenerate — is **REFUSED**, and the branch follows the ordinary ladder.
   Regenerating on top of a wrong target would freeze a permit for a drift
   **neither side ratified**, which Rule 36/38 forbid. This is why the pinned
   checkout is a separate worktree: the merged working tree is mid-conflict and
   the generator cannot be pointed at it yet.
2. **② the generator over the merged tree** — `go run <generator> <wt> <wt>/<table>`
   with the toolchain taken from the pinned checkout (a checkout of this repo, so
   `go run` finds the module). No second implementation of the table (Rule 14).
3. **③ the permit-set biconditional** — the regenerated permit **keys** must
   EQUAL the union of the two sides' keys: *nothing missing* (a permit one side
   holds was dropped — the ratchet loosened) and *nothing extra* (a permit for a
   drift no side carries — the merge ratifying what nobody voted on).
4. **④ the row-inflation bound** — the permit population is inside the sum of
   the two sides' populations, so a generator fault cannot be read as a
   resolution.

Only then are the bytes written into the merge, staged, and committed — with a
commit message that names the rule (`merge: regenerate <path> (N permit(s),
mechanical, gopi merge regenerate lane)`), so `git log` on the integration
branch says a machine regenerated this and which rule said it may.

**A DECLINE IS NOT A FAULT (Rule 15).** The lane returns the reason as a value;
the run prints
`union lane declined <artefact> (<reason>) — the branch follows the ordinary
conflict ladder` and the branch keeps its ordinary chance. Only a failure of the
lane's **own** machinery (a worktree that could not be created, a toolchain that
could not be started) is an error.

**Non-declared paths are untouched.** `mergeUnionSidesMergeable` still requires
**every** conflicted path to be a declared artefact and still caps the set at
`mergeUnionMaxUnmerged`; one genuine conflict leaves the whole branch to the
hand-over ladder — pinned by the pre-existing
`TestMergeUnionLaneRefusesAGenuineSemanticConflict`.

## 4. What was measured, and by which command

The lane's end-to-end arm needs a fixture repository, so it is **gated**:
`GOPI_REGEN_E2E_REPO` names one and the test
(`TestMergeUnionRegenLandsAgainstARealRepositoryWithTheRealGenerator`) drives the
lane's real entry point against it, then re-runs the project's own check on the
landed tree and requires exit 0 with
`table matches the tree — the freeze is exact`. The fixture recipe is
`~/.gopi/sessions/01a133a5-8ba5-71f6-9645-d15f940c7bae/evidence/build-regen-e2e.sh`
and its transcript is `evidence/build-regen-e2e.log`. The fixture is
**self-calibrating**: the only lever this artefact has is its row/key set, and
two colliding landings must produce **different** permit sets — so the script
measures each candidate file's effect on the frozen count and picks two disjoint
files whose deltas differ. (Measured on the first attempt: moving ONE line in
each of two different files produced **byte-identical** tables, because the
permit identity keyed the row and the shifted permits only appear when the 5th
line-wrap coincidence changes. The calibration is what makes the fixture
reproduce the ledger's real collisions rather than a coincidence of equals.)

The `--check` RED the fixture reproduces is a real, pre-existing property of
master, not of this change: at the fixture's base tip, master's own table was
already incoherent (2 unpermitted rows, `--check` exit 1). The fixture's first
step regenerates and commits, so that the target is coherent — which is exactly
what arm ① requires, and why arm ① exists.

## 5. Verification of the change itself

```bash
# red at the pre-change revision (master), green after — ONE test file, both revs
git worktree add --detach /home/user/workspace/gopi-regen-red-01a133a5 master
cp cmd/agent/merge_union_regen_decl_test.go /home/user/workspace/gopi-regen-red-01a133a5/cmd/agent/
cd /home/user/workspace/gopi-regen-red-01a133a5 && go test ./cmd/agent/ -run TestMergeUnionRegenTableIsADeclaredMechanicalArtefact
#   --- FAIL: … is NOT a declared mechanical artefact — a conflict on it is filed as a branch-rework row
cd - && go test ./cmd/agent/ -run TestMergeUnionRegenTableIsADeclaredMechanicalArtefact   # ok
```

The green proof is deliberately written in the **pre-change symbol vocabulary**
(`mergeUnionArtefactFor` and the field names the union lane already had), so it
compiles at both revisions and its failure at `master` is a **behavioural** red
naming the defect — not a compile error.

## 6. Not done / boundaries

- The lane is proven **for this one artefact**. Sibling shapes exist
  (`prose-anchor-baseline.tsv`, `prose-path-line-baseline.tsv`, 0 pending rows
  today) and are **not** declared — a new artefact costs one declaration plus an
  argument that its conflict is mechanical, which is the point of the closed
  list.
- **The lane cannot repair a drifted target.** Arm ① refuses instead, by design;
  the target's own drift is `internal/tools/merge_target_doc_gate.go`'s job
  (it files a deduped `bug` row and proceeds).
- The regenerate arm runs the **Go toolchain** inside the merge probe, so a
  landing on this artefact is measurably more expensive than a fast-forward. It
  is bounded by the probe's own scratch lifetime and is paid only on this one
  conflict shape.

## 7. Refs

Rules 1, 13, 14, 15, 22, 28, 30, 31, 36, 38.
Prior art reused rather than rebuilt: `cmd/agent/merge_union.go` (the lane, its
probe, its proof discipline) and `devdoc/merge-union-lane.md` (the mechanism).
