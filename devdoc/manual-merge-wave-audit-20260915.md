<!-- doc-class: historical -->
# Manual Merge Wave Audit — 2026-09-15

**Author:** PA (asked by nick after he manually merged a batch of stale branches)
**Scope:** the 12 manual merges `feb0e334..ac4f2c74`, the 8 remaining unmerged branches,
and whether any merge was wrong/stale.

## Context

Prior session the PA stopped responding (DeepSeek API outage); nick killed hermes by
force and then manually merged a wave of stale branches himself. He asked for a
judgement call on (a) what was merged incorrectly or is stale, and (b) whether the
still-unmerged branches are still valid for development.

All merges below are authored `Qingzhe Huang` — i.e. **manual by nick**, not by the
merger daemon (`adoption: merge …` commits are the daemon's fingerprint).

## 1. The merge wave (12 merges, master `feb0e334..ac4f2c74`)

```
ac4f2c74 Merge branch 'fix/version-dirty-witness'
d8555aec Merge branch 'fix/daemon-e2e-leak'
2766fa80 Merge branch 'fix/merge-verify-rework-staleness-guard'
61c24b46 Merge branch 'fix/task-list-inprogress-doc-pointer'
519746b6 Merge branch 'fix/remove-peak-guard'
1ee634be Merge branch 'fix/peakhours-merge-verify-record'
c3abd9fa Merge branch 'fix/liveclaim-worktree-veto'      <-- source of both master-reds
72b81152 Merge branch 'fix/ambient-profile-gate'
0477bd3a Merge branch 'fix/cwd-release-coverage'
2bc93022 Merge branch 'fix/verify-no-verdict-keeps-merge'
da4e49d4 Merge branch 'fix/proc-triple'
feb0e334 Merge branch 'fix/worker-ownership-9files'
```

### Verified correct

- **`gopi build ./...` → exit 0.** Master builds. This is the single most important
  check given the history of a build-broken master (`82b05c4f`, 2026-09-12).
- **Peak-hour cost guard removal is provably complete.** `internal/peakhours/` is gone
  from master; only `cmd/agent/peak_guard_removal_test.go` remains (the removal test).
  This is exactly what was asked for in `01a0a08b` (nick's decision: delete the whole
  feature — 4 bugs + 5 merge conflicts). **Correct merge, nothing stale re-added.**
- **Worker-cwd release is present.** The self-report "missing in `gopi worker` path"
  is **no longer true on master**: `defer workerRoundOwnWorktrees()` is wired on the
  worker path, and the release now lives in the shared `internal/worktreerelease`
  package used by six producers (worker, task run, merger, resume, verify-child,
  executor). Task `01a0a094` (cwd coverage) is `completed`+merged.

### Genuinely wrong: one branch caused two master-reds

**`c3abd9fa` Merge branch `fix/liveclaim-worktree-veto`** (commit `e292af26`,
"fix(worktree): a live session's worktree is never reclaimed (01a09d67)") brought in
two files that fail master's own gates:

**RED 1 — `TestWorkerOwnershipPointerPresent`**
```
files that teach a worker-launch / task-run command without the operator-only pointer:
      devdoc/live-claim-worktree-veto.md
fix: insert the ONE canonical pointer line —
      # WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md
or add the file to workerOwnershipDismissed with a reason.
```

**RED 2 — `TestTestBudgetHasASingleSourceAndNamesATimeout`**
```
scripts/repro-live-holder-worktree-veto.sh:142 runs `go test` with no `-timeout` —
it inherits Go's 10m per-package cap, which a loaded host trips on internal/tools
(a 600s FAIL that proves nothing). Pass a budget, or route through scripts/test-budget.sh
```

Both files come from the **same** commit `e292af26`. The branch was written before the
ownership gate and the test-budget gate existed, then merged after — so it landed
"green at authoring time, red at merge time". This is exactly the gap
`t-... merger verify gate` is meant to close, and the reason
"merge it and see" is not sufficient for branches this far behind master
(`fix/liveclaim-worktree-veto` is one of the branches ~200 commits behind).

**Assessment:** the *fix* is fine and wanted; the *branch contents* were stale against
master's newer gates. Remedy is small (add the pointer line + pass a budget), not a
revert. A branch is safe to merge only after re-running master's gates on the *merged*
result.

## 2. The 8 remaining unmerged branches

All 8 are `NOT-MERGED` and far behind master (`behind` 97–231 commits). Judgement per
branch, based on whether master already carries the *substance*:

| Branch | ahead | Verdict | Evidence |
|---|---|---|---|
| `fix/cwd-and-lifetime` | 6 | **RIVAL — supersede, do not merge** | Adds `internal/tools/process_cwd_release.go`, `internal/procname/liveness.go`, `internal/tools/daemon_lifetime.go` + `identity.go`. Master took a **different route** (`internal/worktreerelease` + `internal/worktreeroots`); this branch mentions `worktreerelease` **nowhere**. Merging it would create two competing release mechanisms. Its task `01a0a096` is `verified` but unmerged — **the winning implementation was chosen by master**, so treat this as reference/salvage material. |
| `fix/daemoncwd-scratch-pin` | 1 | **REDUNDANT — delete** | Master's `740f28a6` already IS this content (restore `internal/daemoncwd` scratch-root suite pin). |
| `fix/worker-ownership-pointer-line` | 1 | **REDUNDANT** | Restores a pointer on one transcript; master already carries the gate + dismiss table. |
| `fix/worker-ownership-pointer-audit` | 1 | **SUPERSEDED** | Same goal as merged `fix/worker-ownership-9files`; adds doc annotations only. |
| `fix/worker-ownership-10files` | 2 | **SUPERSEDED** | Same family as above; `feb0e334` (9files) landed first. Keep one, drop the rivals. |
| `fix/worker-status-foreign-attribution` | 2 | **CHECK — likely real but partial** | Its fix ("attribute a worker by ITS OWN environment") already appears in master at `internal/testutil/procident.go:259` (#2606). Verify whether the branch adds anything beyond that before spending CI. |
| `fix/reclaim-fossil-converge` | 3 | **LIKELY REAL — evaluate** | Suite-ledger pin + reclaim convergence for the `/tmp/other-repo` fossil. No matching symbol on master. |
| `fix/task-list-address-no-scan` | 7 | **LIKELY REAL — evaluate** | Resolves a row's session log by ledger-named dir instead of walking the sessions tree (perf + correctness). No matching marker on master. |

**Summary of the 8:** 2 likely-real (`reclaim-fossil-converge`,
`task-list-address-no-scan`), 1 needs a look
(`worker-status-foreign-attribution`), 4 redundant/superseded doc+pin branches,
1 **rival that must NOT be merged** (`cwd-and-lifetime`).

The rivals are the important finding: the worker-ownership subjects and the
cwd-release subject were each developed on **multiple parallel branches**, and master
has already picked a winner for both. Merging the losers would reintroduce competing
mechanisms — this is the "contradictory code" nick suspected.

## 3. Root cause: several branches raced on the same subject

Branches are cheap (GOPI.md Rule 6a: each task opens its own), so two tasks working the
same subject produce competing implementations. Master resolved each subject by taking
one; the losers stay `NOT-MERGED` forever and look like pending work. **The merge
decision is not "does this branch have commits" but "does master already own this
subject"** — and that can only be answered by grepping master for the branch's
distinctive symbols, not by looking at the branch name.

## 4. Recommended actions

1. **Fix the two reds from `c3abd9fa`** (small: pointer line + `-timeout` budget) so
   master's gates are green again. Do not revert the fix itself.
2. **Do not merge `fix/cwd-and-lifetime`** — archive it; master owns the subject via
   `internal/worktreerelease`.
3. **Delete the 4 redundant/superseded branches** after confirming their substance is
   in master (they are, per the table).
4. **Evaluate the 2–3 likely-real branches** properly: rebase onto current master,
   re-run master's gates on the merged result, then merge.
5. Forward-looking: the hazard is *stale branch × newer gates*, not the merge itself.
   A branch merged after ~200 commits of master movement must be re-gated at merge
   time — the merger verify gate exists for exactly this and should cover
   **gate re-run**, not just conflict resolution.
