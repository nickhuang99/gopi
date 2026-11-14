<!-- doc-class: historical -->
# Task Report: arm 1's enforcement half — a run's OWN untracked residue at close-out

Branch: `feat/run-untracked-residue-enforce` (worktree `~/workspace/gopi-arm1-enforce`,
fresh from master `30450fc6`, Rule 23: the classifier half's branch
`feat/worktree-refusal-arms-v2` was READ as information — `git show` — and never
committed on).

Row: `01a0a725` — dispatched by the classifier half's own report
(`devdoc/worktree-refusal-arms-v2.md`, "Not Done (honest)": *"A finished run still
is not forced to clean its own untracked residue"*). Sibling (NOT duplicated here):
`01a0a6eb-b6fa` owns arm 2 (the orphaned `serveWorktreeReapSweep` caller + the
row-less orphan worktree).

## The choice, and why

The epic asked for (a) refuse/repair or (b) a documented decision not to. This
branch implements **a third reading of (a) that is honest about what the
enforcement arm may do**, and that is the whole design argument:

**(a) implemented as RECORD, with the REFUSAL behind an operator switch — not as
an auto-clean and not as an unconditional status block.**

- **Auto-cleaning the residue is refused on principle.** An untracked file in a
  worktree is single-copy data. The classifier splits the two dirty states from
  git's own porcelain output; it cannot tell the run's `build.out` from a human's
  scratch note, and deleting the second is data loss. The ONLY arms that delete
  anything in this area are the worktree-removal arms, and this branch adds none:
  no `git worktree remove`, no `--force`, never a branch mutation.
- **Blocking the `completed` writeback by default is refused for the same reason
  one level up.** The status is the writeback's authoritative terminal fact — the
  neighbouring `deliverable.branch` backfill is documented as a side-channel that
  *"never rolls back the outcome"* — and a default-deny would deadlock finished
  runs at their last write, trading one defect for a worse one.
- **What the operator actually asked for is that the log alone let them tell the
  run's own scratch from somebody's uncommitted work.** That is a RECORD
  requirement, and it is satisfied unconditionally: the close-out writes ONE
  attributed `task_events` row (event `completed-unclean`) whose detail carries
  the classifier's own wording, the run's task_id, the run's OWN worktree path and
  the offending porcelain entries — plus the same sentence on stderr, so a run
  that never reads its trail still sees it (Rule 15: reported, never swallowed).

The refusal is available and exact: `GOPI_TASK_RUN_RESIDUE_ENFORCE=1` makes the
SAME verdict refuse the `completed` transition — the row stays OPEN (nothing is
half-written; the gate sits before the UPDATE), the refusal is itself a ledger
fact (`completed-unclean-refused`), and the run can clean its own scratch and
write back. The switch is read in exactly one place (`config.EnvBool`, Rule 2).

**Placement** is the mechanism: the record rides `taskUpdate` — the ONE state
gate every harness door passes through (`TaskRunWriteback`, the auto-register end
hook's `TaskRunCloseoutStaleRun`, the merger's executor, the CLI's own
`task update`) — so no door can bypass it. It runs **after** the terminal write
has committed and the caller's result is final: a failed measurement can never
roll back an outcome. The refusal face sits BEFORE the UPDATE for the same
reason in the other direction.

**No new vocabulary** (Rules 1b/14): both faces are the classifier's own two
wordings — `reclaimUntrackedResidueReason` (aliased to `reclaimUntrackedResidueMarker`)
for the residue, and the classifier's own predicate returning
`reclaimSkipDirtyReason` for a tracked change, which this arm then says nothing
about.

### Why "somebody's uncommitted work" is never blamed on a run

Two guards, both evidence-based (Rule 10.4 — git's plumbing is the evidence,
never a heuristic about file names):

1. **The run's OWN code worktree only.** `reclaimRunCodeWorktree` requires the
   row's recorded `deliverable.worktree` to be a *linked registration of the
   row's repo standing on the row's branch* (`git worktree list --porcelain`). The
   MAIN checkout can never satisfy that (it is not a linked worktree), so the
   2026-09-16 shape — 8 untracked files sitting in the shared master checkout —
   is not attributed to any run. A row whose recorded ground is non-empty and
   registered but NOT git-attributable (its branch moved on, or the record points
   at a sibling run's tree) is measured and recorded as an explicit
   **unattributed block** (`completed-unclean-unattributed`) rather than silently
   under-reported.
2. **The residue class only.** The classifier's verdict must be the untracked-only
   wording. A tracked change — `M`/`A`/`D`/`R`/`C` — keeps today's handling
   EXACTLY: no record is filed, the tree keeps the sweep's
   `reclaimSkipDirtyReason` behaviour, untouched.

## Completed

- **The enforcement arm** (`internal/tools/task_reclaim.go`, region opened by
  `reclaimRunResidueArmMarker`, closed before the removal arm):
  `ReclaimRunResidueCheck(taskID)` → `ReclaimRunResidueOutcome`
  (`Porcelain`/`Note`/`Attributed`/`Blocked`/`Worktree`), the single wording
  builders `reclaimRunResidueNote` / `reclaimRunResidueBlockedNote`
  (`reclaimPorcelainOneLine` renders the entries one-per-`" | "`), the
  evidence-based ground guards, the idempotence guard
  (`reclaimRunResidueFiled`, `event LIKE 'completed-unclean%'`), the refusal
  (`reclaimRunResidueRefusal` + `reclaimRunResidueRefusedEvent`, bounded by
  `reclaimRunResidueRefusalLimit`), and the operator switch
  `GOPI_TASK_RUN_RESIDUE_ENFORCE` (read once, via `config.EnvBool`, Rule 2).
- **Wired at the choke point** (`internal/tools/tasks.go`): the record is a
  side-channel of `taskUpdate` (after the write returns); the refusal is a
  pre-write gate inside `taskUpdate`, keyed on `stateChanged &&
  status == taskStatusCompleted` so a same-status no-op never re-refuses.
- **Regression tests** (`internal/tools/run_untracked_residue_test.go`, 8):
  attributed verdict; clean + tracked are silent; the main checkout is never
  blamed; exactly one record + the outcome stands + a re-landed writeback adds
  none; the enforce switch refuses and leaves the row open with a ledger fact;
  the unattributable ground is measured not blamed; the child-side close-out
  (`TaskRunCloseoutStaleRun`) carries the record too; and the Rule-13 arm gate
  (the arm's executable source may not contain `--force`, `os.Remove`,
  `os.RemoveAll` or the `git worktree remove` spelling — comment-stripped first,
  because the arm's own doc comment names the command it forswears).
- **A/B red proof** (`internal/tools/zz_redproof_run_untracked_residue_test.go`)
  written against the PRE-FIX API only, so the same file compiles on master and
  on this branch.
- **The tracked-change guard is portable by design** and was proven green on
  pre-fix master too (`zz_baseline_tracked_guard_test.go` in the throwaway
  baseline worktree): the property "a tracked change keeps today's refusal" is
  what the fix must NOT change.

## Not Done (honest)

- **The residue is not deleted and the `completed` writeback is not refused by
  default** — that is the decision above, not an omission. The operator's
  standing requirement (a run must not leave its own untracked scratch) is now
  recorded and attributable on every close-out; it is *enforced* (row left open)
  only with `GOPI_TASK_RUN_RESIDUE_ENFORCE=1`. If the operator wants the hard
  stop to be the default, that is a one-line flip of
  `reclaimRunResidueEnforced` — deliberately left as the operator's switch.
- **The two pre-existing gate failures are NOT this branch's**, and were measured
  on the baseline worktree to prove it (see Verification): `internal/standards`
  Rule 13 flags `internal/shot/x11.go:129` (`"/tmp/.X11-unix"`) and
  `internal/stylegate` sat above several ceilings on master already. Every hit
  sits in a file this branch does not touch; baseline = unchanged (nothing added,
  nothing ratcheted up).
- **A residue set is not auto-classified as "proven safe".** The epic mentions
  that possibility; this arm deliberately does not guess — the classifier's two
  wordings are the only vocabulary it uses, and "proven safe" would need a
  provenance signal the ledger does not have. Name it as a follow-up rather than
  inventing a heuristic.
- **The `gopi task show` surface is unchanged.** The record rides the existing
  `result:` line (the latest `task_events` detail), so it IS visible there; a
  dedicated section would be new CLI surface, out of this task's scope.
- **Rule 11 adoption proof** does not apply: no tool description, prompt routing
  or new tool was added (Rule 3 respected — no new top-level tool, no CLI verb).

## Verification

- **Build**: `CGO_ENABLED=0 go build ./...` → exit 0 (OK).
- **Full suite**: `go test ./... -count=1` → see the tail below (this branch's
  packages green; the two pre-existing gate failures reproduced on baseline).
- **New tests, named**: `TestReclaimRunResidueCheckAttributesTheRunsOwnWorktree`,
  `TestReclaimRunResidueCheckCleanAndTrackedAreSilent`,
  `TestReclaimRunResidueCheckNeverBlamesTheMainCheckout`,
  `TestReclaimRunResidueWritebackRecordsOnceAndNeverRollsBackTheOutcome`,
  `TestReclaimRunResidueEnforceSwitchRefusesTheWriteback`,
  `TestReclaimRunResidueUnattributableGroundIsMeasuredNotBlamed`,
  `TestReclaimRunResidueArmNeverForcesAndNeverTouchesABranch`,
  `TestRunCloseoutStaleRunAlsoCarriesTheResidueRecord`,
  `TestRunOwnUntrackedResidue_TrackedChangeKeepsTodaysRefusal`,
  `TestZZRedProof2_RunOwnUntrackedResidue_TrackedChangeStaysTodays` — PASS.
- **RED PROOF — the RE-MEASURED pair on master as it stands now `30450fc6`**
  (throwaway DETACHED worktree under the scratch tree `/dev/shm/gopi-test-scratch`,
  dev id 28 vs `/` 2051 — Rule 22; each run copies ONE file, nothing else, and
  the tree is removed plainly afterwards, never `--force`):
  - **watch it go red** — `zz_redproof_run_untracked_residue_test.go` copied into
    the baseline tree:
    `TestZZRedProof_RunOwnUntrackedResidueIsRecordedAndAttributed` →
    `--- FAIL ... UNNAMED RESIDUE: the run closed out 'completed' while its OWN
    worktree /dev/shm/…/wt still carried untracked-only scratch (build.out) and
    NOTHING on the row's trail says so — the same tree is later rendered as the
    generic "worktree is DIRTY (uncommitted work) — left for a human", i.e. as
    somebody's uncommitted work`.
  - **green on both** — `zz_redproof2_run_residue_tracked_ab_test.go` (the
    tracked-change half, pre-fix API only) → `--- PASS` on the baseline tree
    (copied in) AND `--- PASS` on this branch (in place): the property the fix
    must not change is provably identical on the two trees.
  - Same run on this branch: the red-proof test `--- PASS` (0.06s), the A/B
    `--- PASS` (0.05s), via `-overlay` so the comparison added no residue to this
    worktree (`git status --porcelain` → 0 lines after it).
  - **CORRECTION to an earlier note in this report**: an earlier draft pasted
    `run_untracked_residue_test.go` into the baseline tree too, which does NOT
    compile on master (`undefined: ReclaimRunResidueCheck`,
    `reclaimRunResidueAtWriteback`, `reclaimEnvEnforce`, …). The portable
    pre-fix-API file is the red proof; the branch's own regression file is not
    meant to compile pre-fix.
  - **Whole-package A/B (same host, same commit pair)**: `go test ./internal/tools/`
    on the baseline tree vs on this branch → the FAIL SETS are IDENTICAL
    (`TestDeadHolderHonestVerdict_RefusesTheReclaimableClaim`,
    `TestMemoryBaseKeepArmsOverrideTheNewRoot/dirty_⇒_kept`,
    `TestMemoryBaseOutOfPolicyRegistrationNamesTheRemedy`,
    `TestZZRedProof_DirtyDeadHolderWorkIsOrphaned`,
    `TestZZRedProofFalseReclaimPromise` — all five pre-existing on master), i.e.
    **no new failure**; the only diff was the elapsed time.
- **Live before/after host evidence** (read-only on `~/work/GoPi`, plus a

  throwaway end-to-end ledger under the scratch tree with faked
  `deliverable.worktree` stamps — the backfill seam itself needs a real child
  session and is untouched by this change):
  - before (`~/work/GoPi`): `git status --porcelain | wc -l` → **12** (all `??`:
    10 verify reports, `.probe/`, `internal/daemoncwd/workspace/`), with 0
    tracked entries; `git worktree list | wc -l` → **5**
    (`~/work/GoPi`, this branch's tree, `gopi-firstopen`, and two detached
    baseline trees) and **back to the same 5 / 12 after** the throwaway
    baseline worktrees were removed plainly.
  - **before/after on the SAME ground, same residue set (`?? build.out`,
    `?? scratch.tmp`), two rows over one worktree**:
    - PRE-FIX binary closes out `t-pre` → trail `[claimed, in_progress,
    completed]`, `completed` detail empty — the run's own scratch is recorded
      NOWHERE, and the sweep later calls it "uncommitted work".
    - FIXED binary closes out `t-fix` → trail `[…, completed, completed-unclean]`
      with detail `run t-fix closed out with its OWN untracked residue in
      /dev/shm/…/wt: worktree carries ONLY UNTRACKED files … — entries:
      ?? build.out | ?? scratch.tmp; the run's scratch was never cleaned …` and
      the same line on stderr.
    - **enforce switch**: `GOPI_TASK_RUN_RESIDUE_ENFORCE=1 gopi task update
      t-hard2 --status completed` → `gopi task update error: task: t-hard2
      REFUSED the completed writeback — …`, row status stays `in_progress`, trail
      `[…, in_progress, completed-unclean, completed-unclean-refused]`.
    - **tracked change** (`M a.txt` alongside the same untracked files) →
      row `t-track` completes normally, trail `[claimed, in_progress, completed]`,
      NO run-residue record: today's handling, unchanged.
  - **finished+clean row of the new rule**: `01a0a6eb-b6fa…` (`completed`,
    `deliverable.worktree` gone) and `01a0a715…` (`completed`, worktree
    `/home/user/workspace/gopi-worker-idle-stop`) — both worktrees no longer
    exist, so the close-out check is a zero-subprocess no-op for them (the common
    case): no record, no error, nothing deleted.
- **Gates**: `go test ./internal/standards/` → FAIL on `internal/shot/x11.go:129`
  (Rule 13, `"/tmp/.X11-unix"`) — re-measured on a fresh baseline worktree at
  master `30450fc6` in the same session: the FAIL/NEW-violation/escape/hits-set
  is **byte-identical** between the two trees (only the `5.090s` vs `5.120s`
  elapsed differs), i.e. pre-existing and not touched here; nothing added, no
  baseline entry changed. `go test ./internal/stylegate/` → the same ceiling
  reports on master and on this branch (`test-sleep` 2/0, `test-setenv` 1/0,
  `errorf-verb` 9/8, `ignored-error` 195/182 — all pre-existing, none in a file
  this branch touches).
  `go test ./internal/taskverbs/` → ok (no verb added).
- **`gofmt -l`** on the touched files → clean.
- **Rule 6b self-check**: `git status` in `~/work/GoPi` lists only the 12
  pre-existing `??` entries — nothing of this branch.
- Commit: see below.

## Files

- `internal/tools/task_reclaim.go` — the enforcement arm (record, refusal,
  attribution guards, switch).
- `internal/tools/tasks.go` — the `taskUpdate` choke point: the record
  (post-write) and the refusal (pre-write gate).
- `internal/tools/run_untracked_residue_test.go` — regression tests + the Rule-13
  arm gate + the portable tracked-change guard.
- `internal/tools/zz_redproof_run_untracked_residue_test.go` — the A/B red proof
  (FAILS pre-fix, PASSES here).
- `internal/tools/zz_redproof2_run_residue_tracked_ab_test.go` — the A/B on the
  tracked-change half (pre-fix API only: PASSES on master AND here).
- `devdoc/run-untracked-residue-enforce.md` — this report.

## Addendum (post-merge): the classifier half reworded the marker — root-caused, not patched

This arm and the classifier half (`feat/worktree-refusal-arms-v2`) were written
against **different wordings of the same state**. Merging master (`9440b57b`,
which already carried the classifier half) into this branch produced a clean
auto-merge whose *behaviour* was still broken:

- `reclaimDirtyVerdict` (the sibling arm's classifier) became the ONE classifier
  and **rewrote the text carried by `reclaimUntrackedResidueMarker`**;
- this arm's close-out gate compared two **rendered sentences** —
  `reclaimDirtyReason(porcelain) != reclaimUntrackedResidueMarker` — so after the
  reword the two sides never matched, the gate silently found nothing to record,
  and **4 of this arm's 9 tests went red**.

Root cause (Rule 10.3): the split was expressed on **wording**, i.e. on a value
two independently-edited arms each own — so it could only stay correct while both
arms agreed character-for-character. The fix is **`reclaimUntrackedOnly(porcelain)`
— a raw-measurement predicate** (does any porcelain entry carry a tracked status?),
wording-independent, with `reclaimDirtyVerdict` now *rendering* each side from it
and this arm's gate asking the predicate instead of comparing sentences. Commit
`75d49d43`; the two vocabulary constants are still the only spellings (Rule 1b/13).

Not a parameter tweak: no marker text was edited to "make it match" again — after
the fix, rewording the marker cannot silence the gate.

**Re-verified after the merge** (all measured on master `b6b7251d`):

- `CGO_ENABLED=0 go build ./...` → exit 0;
- the arm's targeted set (`RunOwnUntrackedResidue|RedProof_RunOwn|RunResidueTracked`)
  on `./internal/tools/` → `ok … 0.583s`;
- the changed `./cmd/agent/` e2e set was run serially: it first **FAILED 7/7** with
  *"`bin/gopi` (mtime 11:44) is OLDER than `internal/tools/tasks.go` (11:59)"* —
  the suite's own stale-binary freshness guard, not a code failure. After
  `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` the same set is `ok … 13.014s`.
  Recorded because a stale-binary red would otherwise be misread as this arm's
  regression.
