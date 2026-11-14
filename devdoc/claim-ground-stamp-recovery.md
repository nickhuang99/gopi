<!-- doc-class: historical -->
# Recovery investigation: claim-time ground stamp — ALREADY ON MASTER (the archive-sweep verdict was a race, not a loss)

Task: recovery dispatch from the 88-tag archive sweep
(`devdoc/archive-tag-sweep-88-20260916.md`, branch `feat/archive-why-gate`).
Briefing claim under test: *"archive tag `archive/feat/claim-time-deliverable-stamp`
(tip `a80866e9`) holds epic branch↔task-linkage item [2/5] — the CLAIM-TIME ground
stamp — and it is ABSENT from master (`cmd/agent/task_claim_ground.go` defines
`taskClaimGroundStamp`, a symbol defined NOWHERE on master)."*

**Verdict: the premise is false. Nothing was lost and no re-implementation was
needed.** The item is on master byte-for-byte, wired end-to-end, with all 8 of its
tests. The sweep scored it RECOVER **against the master tip it measured (2026-09-17
00:27)**, and the row landed the change **4h22m later (03:50–04:49)** — a
measurement-vs-landing race, correctly reported at the time and superseded by the
landing. Deliverable of this task is therefore the disproof below (an explicitly
acceptable outcome per the briefing), not new code.

## 1. Blob-level proof — every file the archived commit touched is identical on master

`git rev-parse master:<f>` vs `git rev-parse a80866e9:<f>` (a80866e9 = the archived
tip; blob hash equality is byte equality):

| file | master | a80866e9 |
|---|---|---|
| `cmd/agent/task_claim_ground.go` | `5d61d4cf` | `5d61d4cf` |
| `cmd/agent/task_claim_ground_test.go` | `2be1ef79` | `2be1ef79` |
| `internal/tools/task_deliverable_claim_stamp_test.go` | `4feab2e7` | `4feab2e7` |
| `devdoc/claim-time-ground-stamp.md` | `f5a19de1` | `f5a19de1` |
| `internal/tools/task_deliverable_worktree.go` | `b3e07ce5` | `b3e07ce5` |

So the archived tip's content **is** master's content for this feature: the archived
tip was never the only holder of anything. (`a80866e9` = 2026-09-16 22:17:06
`feat(task): stamp branch+worktree at claim time (epic branch<->task linkage 2/5)`.)

## 2. Wired end-to-end on master, not merely present

- `cmd/agent/task_claim_ground.go:53` — `func taskClaimGroundStamp(taskID, workdir string) (worktree, branch string, written bool)`;
  body at `:62` resolves the row's own ground (`tools.TaskDeliverableRepoFor`) then
  calls `tools.TaskWorktreeStampClaimerFor(taskID, repo, workdir)`.
- `internal/tools/task_deliverable_worktree.go:668` — `TaskWorktreeStampClaimerFor`
  → `:502 taskWorktreeStampClaimer` → `:360 taskWorktreeOwningDir` (the linked
  worktree owning the claim dir; refuses when ownership is unproven).
- **Call site (the whole point of item 2/5):** `cmd/agent/cli_tasks.go:1810`
  `taskClaimGroundStamp(target, childDir)`, immediately after `executorChildDir`
  resolved `childDir` and before `cmd.Dir = childDir` — i.e. the in-flight row names
  its branch + worktree at claim time, which is exactly the window the epic said
  named no ground.
- Tests present on master: `cmd/agent/task_claim_ground_test.go`
  `TestClaimGroundStampNamesBranchAndWorktreeInFlight:119`,
  `...RespectsWorktreeOwnership:170`, `...NoRepoIsANoop:254`,
  `...CallSiteIsTheSpawnFunnel:356`; `internal/tools/task_deliverable_claim_stamp_test.go`
  `TestClaimTimeGroundStampNamesGroundInFlight:65`,
  `...LeavesMainCheckoutRowRepoOnly:172`, `...NeverRebindsAnExistingWorktree:197`,
  `...RefusesWhenOwnershipIsUnproven:248`. 8/8 = the archived commit's suite.

## 3. Why the briefing says "absent": a race, with the timeline of the row that landed it

| when (2026-09-17, +0800) | what |
|---|---|
| 00:27:32 `eac3b475` | the 88-tag sweep commit — "re-measure the 88-tag table live, decide the UNKNOWN row" (branch `feat/archive-why-gate`); scored the tag **RECOVER** against that instant's master tip |
| 03:50:42 `6dc03ad7` | `feat(task): stamp branch+worktree at claim time (epic branch<->task linkage 2/5)` — **first master commit adding `cmd/agent/task_claim_ground.go`** |
| 03:50:42 `defba5f1` | `Merge branch 'master' into merge/claim-time-deliverable-stamp` (tag `backup/claim-time-deliverable-stamp-mergetip`) |
| 03:59:11 `b175e193` | "the claim-time-deliverable-stamp landing — FF-ability flatten, the mid-task master move, and the tests" |
| 04:45:27 `676d34b3` | "master's pre-existing reds — the same-commit A/B evidence" |
| 04:49:46 `0412f677` | "freeze the tip and record the merge-window constraint" — FF of the archived tip's flatten, **now an ancestor of master** |

So the item's row — the sweep's table records it as `01a0a9b1-6dc6` (the `[2/5]`
sub-row `01a0a9b1-6dc6-7184-9132-f185fb912af4`, status `failed`), while this
briefing names `01a0aaa4-3897-78e7-9453-e193e0346c3a`
(`status: completed`, summary *"resolve merge conflict for
feat/claim-time-deliverable-stamp in /home/user/work/GoPi, parked at
archive/feat/claim-time-deliverable-stamp"*, `deliverable.branch
merge/claim-time-deliverable-stamp`, `deliverable.worktree
/home/user/workspace/gopi-claimstamp`, tags `merge-conflict,auto-dispatch`) — the
**merge-conflict resolver** row did the landing: it landed a **flattened
fast-forward** of the archived intent (master `118a1de0` → `0412f677`, confirmed
ancestor of master `9ce8e159`), and the
`archive/feat/claim-time-deliverable-stamp` tag was dropped after — the only tags
left are `backup/claim-time-deliverable-stamp-original` (still `a80866e9`, the
superseded tip, kept) and `backup/claim-time-deliverable-stamp-mergetip`
(`defba5f1`, the `Merge branch 'master' into merge/…` tip). The sweep's own
record (`…:97`) gives the row as `01a0a9b1-6dc6` with status `failed`, and its
headline already warns about the mechanism — *"it measures every row against the
master tip you hand it, because both the tag set and master move while a sweep is
running"* — so the fix is not a better measurement, it is a **re-check at dispatch
time** (one `git rev-parse master:<path>` per RECOVER row, seconds per row).

Per Rule 23 this task did **not** check out or commit on the archived branch, and
**no archive tag was deleted**; a fresh branch off master carries this
investigation instead. Nothing was un-parked on that branch by this task either.

## 3b. The sweep's own "smallest sample" was correct — at 00:27, and false 3h23m later

The sweep doc (`archive/feat/archive-why-gate:devdoc/archive-tag-sweep-88-20260916.md:97`)
records the row as `| archive/feat/claim-time-deliverable-stamp | a80866e9 | 1 | 1
| 01a0a9b1-6dc6 | failed | cmd/agent/task_claim_ground.go |` and says at `:109`
*"defined nowhere on master, master's `cmd/agent/cli_tasks.go` has no claim-time
caller"* — a hand-validated claim (1 commit, 1 symbol) that was **true when
measured** and false by the time the recovery was dispatched. The report was not
wrong; acting on a stale table is what cost this dispatch. The minimal fix is a
dispatch-time re-verification per RECOVER row — one `git rev-parse master:<path>`
against the file the row names, seconds per row, and it would have closed this one
with a two-line note instead of an agent run.

## 3c. The one half that is still open already has its own live row — do not duplicate it

A sibling row created 2026-09-18 07:20 (today, after the sweep):
`01a0b1ab-935f-7736-a943-9e36153e7eac` — *"Redo [2/5] stamp the branch↔worktree
linkage at CLAIM time (predecessor 01a0a9b1 FAILED)"*, status `completed`,
deliverable branch `feat/claim-stamp-live-proof` (worktree
`/home/user/workspace/gopi-claim-stamp-live`). That branch is **not merged** at
master `9ce8e159` and carries exactly the extra thing the item's acceptance wanted
(the live two-sided writer→reader observation):

```
git diff --stat master...feat/claim-stamp-live-proof
 devdoc/claim-time-stamp-live-proof.md                 | 214 ++++++
 internal/tools/task_deliverable_claim_reader_test.go  | 207 ++++++
 internal/tools/task_deliverable_worktree.go           |  54 ++-
```

So the correct reading of this dispatch: **the stamp itself needs no recovery**
(§1–§2 — it is on master byte-for-byte), and the remaining open half of item [2/5]
rides a live branch master has not merged yet. Re-implementing either here would
be a third copy of the same item. What this task adds is the disproof, so the next
reader does not spend another agent run on it.

## 4. What this task did NOT do

- **No re-implementation**, deliberately. `cmd/agent/task_claim_ground.go:53`
  already defines `taskClaimGroundStamp` on master; re-landing it would collide at
  compile time and create a second source of truth (Rules 13/14).
- No checkout or commit on the archived branch, no cherry-pick, **no archive tag
  deleted** (Rule 23) — the only history written is `526734b9` on this fresh
  branch.
- Not fixed here (out of scope, recorded instead): the sweep's RECOVER verdicts are
  not re-validated against the current master tip at dispatch time — cheap to add
  (`git rev-parse master:<path>` per row) and it would have avoided this dispatch.
- The live render observation the item's acceptance asked for is NOT produced here;
  it belongs to row `01a0b1ab-935f-…` / branch `feat/claim-stamp-live-proof`
  (unmerged, §3c).
