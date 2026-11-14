<!-- doc-class: historical -->
# Task Report: un-park `fix/finish-cleanup-single-node-wording` onto master (FF-able tip)

**Deliverable branch:** `fix/finish-cleanup-single-node-wording` @ `b9009ee2`
**Park tag:** `archive/fix/finish-cleanup-single-node-wording` (tip `7264cc5a`)
**Master at resolution:** `d2b1e217` (two earlier windows, `118a1de0` and `bf4d1941`, superseded — §2)
**Verification tree:** `/home/user/workspace/gopi-fc-verify` (the original worktree was swept mid-task; the branch ref survived in the shared repo — Rule 6d)

## Completed

### 1. The merge was NOT a textual conflict — it was pure fast-forward-ability

The briefing anticipated conflict hunks. Measured reality: merging master into
the parked tip auto-resolves with **0 conflicted files** (`git diff
--name-only --diff-filter=U` empty; 0 conflict markers in every touched file).
The branch tip was simply not a descendant of master (`git merge-base
--is-ancestor master <tip>` exited 1, 115 commits behind), so the merger parked
it. The work was the FF-shape, not side-picking.

Resolution path (memory #3300/#3305/#3329 recipe):

1. Worktree on the branch ref (Rule 6a/23/29); the park tag was read as
   evidence, never committed onto.
2. `git merge --no-commit --no-ff <master tip>` → commit → tag the two-parent
   record `backup/finish-cleanup-single-node-wording-mergetip3`.
3. `git reset --soft <master tip>` + recommit → the SAME tree re-parented as a
   one-parent child of master. Verified: `git diff <mergetip3> HEAD` **empty**
   (byte-identical tree), `git rev-list --count master..HEAD` = **1**,
   `git merge-base --is-ancestor master HEAD` exit **0**.

Nothing was force-pushed; the park tag and branch ref are untouched.

### 2. The trap that cost this task two attempts: master moved MID-TASK (twice)

- Window A merged `118a1de0`. `git diff --name-status master HEAD` then showed
  **deletions of master's own newest work** (`cmd/agent/task_claim_ground.go`,
  `internal/tools/task_deliverable_claim_stamp_test.go`, `taskClaimGroundStamp`
  in `cli_tasks.go`). Root cause (5-why, not a merge bug): master had advanced
  to `bf4d1941` (landing `merge/claim-time-deliverable-stamp`) while the task
  ran; `git diff master HEAD` re-resolves `master` at call time, so the "deleted"
  files were the whole new advance. Decisive evidence:
  `git cat-file -e 118a1de0:<file>` was **absent** — the old tip never had them.
  The stale merge would have reverted the claim-time stamp.
- Window B merged `bf4d1941`; master advanced to `d2b1e217` during the very
  verification run (the same trap, so re-merged a third time rather than
  reporting a stale tip).

Each window was re-merged from scratch and the stale result discarded. The final
tree carries master's post-118a1de0 files (`task_claim_ground.go`,
`task_deliverable_claim_stamp_test.go`, `task_finish_premerge.go` all present).
`bf4d1941..d2b1e217` touches only `internal/standards` + briefs/devdocs — no
`internal/tools` code — so the merged-tip verification below remains valid.

### 3. Both sides' intent preserved (the "keep BOTH" requirement)

| side | feature | evidence in `b9009ee2` |
|---|---|---|
| master (`d2b1e217`) | pre-merge verification gate ①b (`finishCleanupPreMergeStep`) | 4 occurrences in `internal/tools/task_finish_cleanup.go`; `internal/tools/task_finish_premerge.go` present |
| branch (`7264cc5a`) | the two-cause no-server reason renderer | `noServerTargetSkipReason` (4 occurrences) at the chain arm, the recycle arm and the git-sync face (`task_sync_git.go:200`) |

Net delta vs master = exactly the branch's 4 paths + this report:
`A devdoc/finish-cleanup-single-node-wording.md`, `A <this merge report>`,
`M internal/tools/task_finish_cleanup.go`, `M internal/tools/task_finish_cleanup_test.go`,
`M internal/tools/task_sync_git.go` (the parked delta's own +446/−7).

## Not Done

- `git tag -d archive/fix/finish-cleanup-single-node-wording` is recorded, not
  executed: the tag may be dropped only once the branch is actually LANDED on
  master (the merger's step). Command for the lander:
  `git tag -d archive/fix/finish-cleanup-single-node-wording`
- No whole-tree `go test ./...` (cmd/agent exceeds Go's 10m package budget on
  this host). Verification is the changed package on the merged tip vs a
  same-commit baseline.
- The baseline arm of the final A/B did not finish: the host was at load ~26
  (several other agents' `go test` runs). It is moot — the merged arm is fully
  green, and a regression would require a FAIL on the merged tip.

## Verification

- **Build (merged tip `b9009ee2`):** `CGO_ENABLED=0 go build -o bin/gopi
  ./cmd/agent/` → exit 0; `CGO_ENABLED=0 go build ./...` → exit 0.
  Baseline `bf4d1941` build also exit 0.
- **FF-ability:** `git rev-list --count master..HEAD` = 1;
  `git merge-base --is-ancestor master HEAD` exit 0; `git diff <mergetip3> HEAD`
  empty (tree identity); 0 conflict markers in the delta files.
- **Tests (merged tip `f4359bd5`, code-identical tree):**
  focused `go test -count=1 -run 'TestFinishCleanup|TestGitSyncClientPass|TestWorktreeOwnership' ./internal/tools/`
  → `ok internal/tools 23.782s`;
  full `go test -count=1 -p 1 ./internal/tools/` → **`ok ... 497.934s`, 0 failures**
  (`grep '^--- FAIL'` = 0 lines) — i.e. even the four host-geometry failures the
  branch's own devdoc recorded at `6a105a48` are gone on this master.
- **gofmt:** set-delta vs master empty (no new offenders).
- **Main-repo hygiene:** none of the delta files appear in `/home/user/work/GoPi`'s
  `git status` (Rule 6b self-check OK).

## Landing facts for the merger / next agent

- Branch `fix/finish-cleanup-single-node-wording` @ `b9009ee2` (in the shared
  repo — worktree commit, no push needed).
- Recovery: `git show fix/finish-cleanup-single-node-wording:devdoc/finish-cleanup-single-node-wording-merge.md`
- Resolution records: tags `backup/finish-cleanup-single-node-wording-mergetip`
  (118a1de0, superseded), `...-mergetip2` (bf4d1941, superseded),
  `...-mergetip3` (`f4706f2e`, current).
