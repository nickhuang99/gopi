<!-- doc-class: historical -->
# Task Report: the post-merge gate re-run measured over the MERGED result

Rule 2 of `devdoc/stale-merge-audit-20260916.md` §5 — *re-run the repo's own
gates on the MERGED result* — as the merger close loop's second measurement.

## Completed

- **`internal/tools/rebuild_vet_mergeset.go` (new).** The close loop gains a
  step that materialises the MERGE COMMIT in a detached scratch worktree
  (`git worktree add --detach <scratch>/<rev> <mergeRev>`, the merge-TIME build
  gate's own scratch class, checkout naming, cache resolvers and reap — no
  second mechanism) and measures the repo's cheap deterministic gates **over
  that commit**:
  - `go vet ./...` — module-wide, the one command that COMPILES the test trees
    while `go build` stays green (the gate the epic's premise rests on);
  - `go test -count=1 ./internal/standards/ ./internal/stylegate/ ./internal/testutil/`
    — the git-era gate set §5.2 names, run **only when the merge's own diff
    touched one of those packages** (`git diff --name-only <mergeRev>^ <mergeRev>`,
    the worktree bytes with `core.worktree` removed). BRANCH-SCOPED, so a landing
    that touched none of them pays one vet and nothing else (Rule 27 /
    `internal/smokegate`; no whole-tree command is prescribed or run anywhere).
- **Reported, never rolled back (Rule 12), never silent (Rule 15).** A red gate
  fires the close loop's existing notify channel (`VetFailedEvent`, the same
  synthetic event the rebuild failure and the sibling gate use) whose summary
  names the gate, the repo, the revision, the scoped packages and the first
  position — and files a **bug row** (`TaskSubmit`, tag `bug`) so the defect is a
  queue item rather than a line in a log nobody re-reads.
- **Red the BASE already had is not blamed on the landing.** The step measures the
  merge's FIRST PARENT with the same gate set and attributes: a base that was red
  too downgrades the verdict to an abstention carrying the attribution (no notify,
  no row); a green base PROVES the regression and the fail summary says so. This
  is what keeps the gate usable rather than a permanent alarm — see the measured
  finding below.
- **Ordered and reachable.** The step rides `RebuildAfterMerge`'s deferred
  close-out (right after the existing checkout vet gate), so it runs on every
  exit path of the rebuild — and therefore on the durable drain path
  (`markRebuildAfterMerge` → `serveRebuildAfterMerge`) as well as a direct call.
  A new `RebuildResult.Mergeset` field carries the verdicts; it never touches
  `OK`, `Err` or the rebuild's own outcome.
- **`internal/tools/rebuild_vet_mergeset_test.go` (new).** Nine arms listed under
  Verification, each building a real repo with a real merge and the real toolchain
  — no mocked toolchain (the step's whole job is to run one and read it).
- **Escape hatch** `GOPI_POSTMERGE_MERGESET_GATE=0` silences the step, so an
  unforeseen false report is one env var away from gone (Rule 2: one reader, one
  declaration).

## Not Done

- **The epic's literal `go vet ./...`-only reading was already shipped** before
  this run: `4fdb6684` (2026-09-16) landed the checkout vet gate and `b9538031`
  (2026-09-18) the revert classification, and both are in master (`git merge-base
  --is-ancestor 4fdb6684 master` → true). This run therefore delivers the
  **unshipped half** the epic also asked for: the measurement over the MERGE
  RESULT (the earlier gate's own header states its scope is "the tree the round
  closes on … no worktree"), the `standards/stylegate/testutil` gate set, and the
  bug row. Recorded here rather than claimed as an independent re-implementation.
- **`internal/testutil` is RED on master today** (measured, see Evidence):
  `TestScriptsAreBoundedAndOffTmp` fails on `/tmp` literals in
  `devdoc/evidence/google-x11-ab/*.sh`, and `TestEveryTempUsingSuiteIsPinned`
  fails too. That is NOT this branch's defect and is NOT fixed here (it is a
  `devdoc/evidence/` cleanup or a gate-skip-list decision, and it touches no file
  this epic owns). It is the reason attribution exists — without it, every landed
  merge would file a bug row naming a pre-existing defect. **Dispatchable as its
  own task.**
- **`internal/testutil` also carries a ~300s test** (`TestRule27NoDeviceLoadGenerators`,
  heavytest-gated: it SKIPS without `GOPI_HEAVY_TESTS`). The gate packages are
  therefore only run when the merge diff touched one of them — a landing that
  touches `internal/standards/` pays that package's real test time inside the
  close loop. Whether the scoped leg should also be heavytest-suppressing (and
  what that would hide) is left open deliberately: it is a policy question about
  coverage, not a mechanism.
- **No live merger-tick run was recorded.** The red-then-green evidence below is
  the step itself executed over real merge commits (including through
  `RebuildAfterMerge`, i.e. the real close loop) — not a whole merger tick with a
  live landing. The tick's own end-to-end behaviour (notify → operator, row →
  queue) is asserted at the seam the tick uses, which is the same seam.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK (exit 0);
  `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK (exit 0).
- `gofmt -l` on the changed files → clean.
- `go vet ./internal/tools/` → clean (exit 0).
- New tests: `scripts/test-budget.sh -run 'TestMergesetGate' -v ./internal/tools/`
  → **12 arms, 0 failures, 98s** (per-arm times in Evidence).
- Standards gates: `go test -count=1 ./internal/standards/` → **ok, 12.8s**
  (after recording the two census arms this change derived).
- `TestGOPIRule3_MatchesClaudeToolDefs` → **ok** (`./internal/agent/`, 1.1s),
  and `TestSmokePathPrescribesNoWholeTreeSuite` → **ok** (the prompt edit added
  no whole-tree command span).
- Branch/paths: `fix/merger-postmerge-cheap-gates` in worktree
  `/home/user/workspace/gopi-postmerge-gates`, commit `20d38c38`;
  `git status --porcelain` empty at close (the report/prompt edits below are the
  second commit).

## Verification — what is NOT green, and why it is not this branch

- `./internal/testutil` → **RED at master, unchanged by this branch.** Two
  failures measured on the pristine tree: `TestScriptsAreBoundedAndOffTmp`
  (the report names `/tmp` literals in `devdoc/evidence/google-x11-ab/*.sh`) and
  `TestEveryTempUsingSuiteIsPinned`; plus a ~300s (heavytest-gated)
  `TestRule27NoDeviceLoadGenerators`. `git diff --stat master...HEAD` over
  `internal/testutil` is EMPTY — no file this branch touches lives there. Named
  here as a dispatchable defect, not fixed (see Not Done).
- `./internal/stylegate` → **RED at master, unchanged by this branch.** Measured
  `ceiling` violations: `errorf-verb` 8>7, `ignored-error` 12>0 production and
  200>163 test, plus `test-sleep`/`test-skip` at ceiling 0. A grep of the
  failure output for `rebuild_vet_mergeset` returns **0 hits** — this change
  adds no stylegate violation. The package is non-empty and carries pre-existing
  sweeps, so this is not an empty-package artifact.

## Evidence (self-filed)

The measured case the whole epic rests on — `go build` green while the merged TEST
tree is red. Fixture: a real module, one package, a duplicate `TestMain` planted
on a branch and landed by a real `git merge`:

```
$ git worktree add --detach <scratch>/<rev> <mergeRev>   # the merged tree
$ go build ./...    # exit 0   ← the declared build is GREEN
$ go vet ./...
# mgfixture/p
vet: p/z_main_test.go:9:6: multiple definitions of TestMain
                 # exit 1   ← the merged test tree is RED
```

The step over that merge commit (recorded run, red half):

```
[task-notify] merger: MERGESET gate RED: post-merge gate RED on the MERGED result:
  gate=vet repo=<repo> rev=<mergeRev> at multiple definitions of TestMain (0 finding(s))
[task-notify] cmd ok (stdout: )
[task-notify] merger: MERGESET gate filed bug row:
  bug: post-merge gate RED on the merged result: vet — <repo> multiple definitions of TestMain
```

Attribution (recorded run): the same repo with the collision planted on MASTER
first — the base is red too, so the merged step downgrades:

```
merger: MERGESET gate verdict: post-merge gate ABSTAINED on the merged result:
  gate=vet repo=<repo> (the vet gate was already RED on the base revision
  (multiple definitions of TestMain) — this red PRE-EXISTED the landing, so it is
  not reported as this landing's defect) — the round stands
```
(NOTIFY sink empty, ledger bug rows = 0 for that arm.)

The branch-scoped leg (recorded run): a merge that touched
`internal/standards/` selects exactly `[./internal/standards]`; a merge that
touched only `p/` selects nothing.

The twelve arms, from the recorded run (98s, 0 failures, `exit=0 outcome=pass`):

```
17.13  TestMergesetGate_ReportOnlyNeverARollback
12.77  TestMergesetGate_FilesABugRowOnRed
10.76  TestMergesetGate_CleanResultFilesNoBugRow
10.67  TestMergesetGate_CleanMergeResultPasses
 9.48  TestMergesetGate_RegressionIsAttributedToTheLanding
 9.00  TestMergesetGate_RedMergeResultIsReported
 6.85  TestMergesetGate_TouchedGatePackagesDriveTheGatesLegWithTheRealToolchain
 2.24  TestMergesetGate_GatesLegIsScopedToTheMergesDiff
 1.57  TestMergesetGate_EscapeHatchSilences
 1.20  TestMergesetGate_PreexistingRedIsNotBlamedOnTheLanding
 0.00  TestMergesetGate_BugSummaryFitsTheLedgerCeiling
 0.00  TestMergesetGate_AbstainsWithoutARevisionOrModule
stamp: pkg=-run TestMergesetGate -v ./internal/tools/ exit=0 outcome=pass
```

(Named individually rather than counted, so a reader can see which arm covers
which contract — a count would hide a removed arm.)

The gate that bit on this change's first landing (recorded):

```
--- FAIL: TestGateCwdReleaseCensusArmsAreListed
    census derived 2 removal arm(s) that this gate neither triggers on nor records as
    deliberately exempt: [RebuildAfterMerge RebuildAfterMergeMergeset] — a caller of one
    removes a linked worktree with no release obligation attached. Add the arm to
    cwdReleaseRemovalTriggers, or record it in cwdReleaseArmsNotListed with its reason.
```
Both arms are now recorded with their reason (the `ClassifyVetGateRevert`
private-scratch precedent), and `go test ./internal/standards/` is ok.

## Files

- `internal/tools/rebuild_vet_mergeset.go` (new) — the merged-result gate step.
- `internal/tools/rebuild_vet_mergeset_test.go` (new) — twelve arms.
- `internal/tools/rebuild.go` — `RebuildResult.Mergeset` + the deferred call.
- `internal/standards/cwd_release_gate_test.go` — the census's two derived arms
  (`RebuildAfterMergeMergeset`, `RebuildAfterMerge`) recorded with their reasons,
  which the gate demanded on this change's first landing.
- `cmd/agent/merger_takeover_prompt.md` — the prompt's `go vet` section now says
  the close loop runs the measurement and that a RED GATE IS NOT A REVERT TRIGGER
  (the prompt already told a tick to "revert immediately"; Rule 12 forbids that,
  so the two sentences are reconciled where the reader meets them).
- `devdoc/merger-postmerge-gates.md` (this report).

## Not Done (honest)

See the "Not Done" section above — in short: the vet half pre-dated this run; the
pre-existing `internal/testutil` red is named and dispatchable, not fixed; the
heavytest-scoping policy for the scoped leg is left open on purpose.
