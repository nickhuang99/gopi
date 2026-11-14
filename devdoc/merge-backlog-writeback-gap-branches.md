<!-- doc-class: historical -->
# Merge Backlog — Writeback-Gap Branches: Per-Branch Terminal Outcome

Task: `01a09462-1402-7186-944c-5c2ebde486e5` — "merge backlog: land the 5 branches
freed by the writeback-gap closeout (old zombie row ids attached)".
Repo: `/home/user/work/GoPi`. Worktree/branch: `/home/user/workspace/gopi-merge-backlog-wbgap`
on `feat/merge-backlog-writeback-gap-branches`.

## Headline

**All 5 branches were already landed before this task ran — by the merger sweep of
2026-09-12 14:52–15:22.** No merge remained to perform, so this task performed **no
merge** (Rule 6 respected: no merge in the main dir; the merger owned the landing).
The deliverable is therefore the verification chain + this terminal outcome record,
not a landing.

Every acceptance gate is empirically green (evidence below):

| # | Branch | Tip sha | Old ledger row id | Outcome | Landing commit on master | devdoc on master | Branch deleted |
|---|--------|---------|-------------------|---------|--------------------------|------------------|----------------|
| 1 | `feat/pa-never-runs-tasks` | `7d5b033f` | `01a09260-f5c8-72a6-98b0-ba1f6846a08f` | **landed** (clean merge) | `7f56c049` | `devdoc/pa-never-runs-tasks-20260912.md` (blob `6e889e97`) | yes (`-d`) |
| 2 | `fix/glm-night-watch-delegates` | `141a6aa8` | `01a092a6-43a2-7bc4-9c13-30f0ff80196e` | **landed** (self-resolved conflict) | `55296ffb` | `devdoc/glm-night-watch-single-launcher.md` (blob `2ba84f0b`) | yes (`-d`) |
| 3 | `docs/worker-ownership-pointer` | `42e8809e` | `01a092a6-6011-7463-8600-121fee745213` | **landed** (clean merge) | `00eec377` | `devdoc/worker-ownership-pointer-report.md` (blob `6436ae2b`) | yes (`-d`) |
| 4 | `fix/merger-foreground-flock` | `87e99268` | `01a092ab-db9b-74a2-8e45-e1f5b988b0a1` | **landed** (clean merge) | `d259403e` | `devdoc/merger-foreground-flock-20260912.md` (blob `a1e83fcf`) | yes (`-d`) |
| 5 | `fix/merger-status` | `518d3bdb` | `01a092b2-4f21-7d41-9bee-56a8480f150f` | **landed** (self-resolved conflict) | `f9b6e8c0` | `devdoc/merger-status.md` (blob `ed255c8f`) | yes (`-d`) |

No branch needed **rework-filed** for a conflicted merge: the two conflicts (rows 2 and 5)
were resolved **inside the merger's merge sub-process** and committed on master with
parents = branch tip + then-master, which is the correct landing shape (the resolution
commit *is* the merge commit, not a detached rework).

## Per-branch evidence

Evidence command (run against live `master`, tip `23e6da05`):

```sh
git rev-parse --verify -q <tip>                    # object exists:        obj=OK
git merge-base --is-ancestor <tip> master          # ancestor of master:   exit 0
git cat-file -e master:<devdoc>                    # devdoc on master:     exit 0
git show-ref --verify -q refs/heads/<branch>       # branch still exists?  absent
```

Result — every line `ancestor=OK doc=OK branch=GONE`:

```
feat/pa-never-runs-tasks           tip=7d5b033f obj=OK ancestor=OK doc=OK branch=GONE
fix/glm-night-watch-delegates      tip=141a6aa8 obj=OK ancestor=OK doc=OK branch=GONE
docs/worker-ownership-pointer      tip=42e8809e obj=OK ancestor=OK doc=OK branch=GONE
fix/merger-foreground-flock        tip=87e99268 obj=OK ancestor=OK doc=OK branch=GONE
fix/merger-status                  tip=518d3bdb obj=OK ancestor=OK doc=OK branch=GONE
```

Second, independent check — the **landing commits** (not just the tips) are ancestors of
master, and their parentage proves they are real landings:

```
7f56c049 Merge branch 'feat/pa-never-runs-tasks'      parents=00eec377 7d5b033f  2026-09-12T14:52:23  on-master=YES
55296ffb resolve merge fix/glm-night-watch-delegates into master
                                                      parents=141a6aa8 2edf6338  2026-09-12T15:22:29  on-master=YES
00eec377 Merge branch 'docs/worker-ownership-pointer' parents=49ff4082 42e8809e  2026-09-12T14:52:23  on-master=YES
d259403e Merge branch 'fix/merger-foreground-flock'   parents=fd92cc27 87e99268  2026-09-12T14:52:23  on-master=YES
f9b6e8c0 resolve merge fix/merger-status into master  parents=518d3bdb d259403e  2026-09-12T14:54:33  on-master=YES
```

Third check — the devdoc is **byte-identical** master-vs-branch-tip (so the doc on master
is the branch's doc, not a stale ancestor copy):

```
feat/pa-never-runs-tasks         SAME master=6e889e97 tip=6e889e97
fix/glm-night-watch-delegates    SAME master=2ba84f0b tip=2ba84f0b
docs/worker-ownership-pointer    SAME master=6436ae2b tip=6436ae2b
fix/merger-foreground-flock      SAME master=a1e83fcf tip=a1e83fcf
fix/merger-status                SAME master=ed255c8f tip=ed255c8f
```

Fourth check — `git show --stat` on each landing commit shows the branch's added files
(`cmd/agent/agent_spawn_guard.go` + test, `internal/config/agentguard.go` + test,
`cmd/agent/merger_lock_ownership_e2e_test.go`, the 100+ `devdoc` pointer edits, the
`devdoc/*.md` files themselves) — i.e. the content **is** on master, and no revert of any
of these 5 merges exists in master's history.

Fifth check — the **exact devdoc paths** asserted by the epic acceptance
(`git cat-file -e master:<devdoc>`, not just "some devdoc is present"):

```
devdoc/pa-never-runs-tasks-20260912.md             PRESENT  blob=6e889e97
devdoc/glm-night-watch-single-launcher.md          PRESENT  blob=2ba84f0b
devdoc/worker-ownership-pointer-report.md          PRESENT  blob=6436ae2b
devdoc/merger-foreground-flock-20260912.md         PRESENT  blob=a1e83fcf
devdoc/merger-status.md                            PRESENT  blob=ed255c8f
```

(`devdoc/merger-status.md` is the shared infra doc of the merger programme and
therefore appears in the `--name-only` deltas of several landings; it is listed
against branch #5 because that branch is where it was authored/extended.)

Sixth check — the accepted-branch gate re-run at the end, on live master
(`23e6da05`), the five tips and the five refs in one pass:

```
feat/pa-never-runs-tasks           GONE  ancestor=OK   doc=PRESENT
fix/glm-night-watch-delegates      GONE  ancestor=OK   doc=PRESENT
docs/worker-ownership-pointer      GONE  ancestor=OK   doc=PRESENT
fix/merger-foreground-flock        GONE  ancestor=OK   doc=PRESENT
fix/merger-status                  GONE  ancestor=OK   doc=PRESENT
```

## Why the branches are gone (deletion mechanism)

The branches were deleted by the merger's own repo sweep, whose ③ tidy pass runs
`MergedCleanupSweep` (`internal/tools/repo_sweep.go` ③ → `internal/tools/task_merged_cleanup.go:508`,
`git branch -d`) — a `-d`, never `-D`, so deletion could only succeed
because each tip is an ancestor of master. That is a *machine-enforced* confirmation of
the same fact the ancestry gate asserts, independent of this report. `git branch` on the
live repo no longer lists any of the five.

Corroborating merger log (last sweep lines, `/home/user/.gopi/logs/merger.log`) show a
later state of the same backlog (`merged=0 protected=15..17`, i.e. an empty eligible
backlog), with no `contract-refused` / `rework-submitted` line for any of the 5 branches.

## Post-merge gates (Rule 5 / epic acceptance)

Run in `/home/user/workspace/gopi-merge-backlog-wbgap` at tree `23e6da05`
(= master tip; `git status --porcelain` → 0 lines, clean):

```
TREE=23e6da051c294d01c04dd6b859b563e5ba0f7782 BRANCH=feat/merge-backlog-writeback-gap-branches DIRTY=0
BUILD_EXIT=0                      # CGO_ENABLED=0 go build ./...
STD_EXIT=0                        # CGO_ENABLED=0 go test ./internal/standards/
ok  	github.com/nick/gopi/internal/standards	46.187s
```

## Residual findings (out of scope, recorded not acted)

1. **`fix/glm-night-watch-delegates` rework rows are still pending.** Two auto-filed rows
   exist — `01a0948e-219c-7c6c-8af4-24ebb2cc7d60` and
   `01a094a0-deb3-7c36-bc23-a213e7f07e0c`, both summarised "merge verify rework:
   fix/glm-night-watch-delegates (was `01a092a6-43a2-7bc4-9c13-30f0ff80196e`)" — created
   by the merger's merge-verify wait-cap, which reverted and re-dispatched before the
   final verification passed. Row `01a092a6-…` itself is terminal (`verified`, reason
   "independent re-verification of merged fix 141a6aa8: 5/5 acceptance criteria PASS"),
   so these two rows are **stale duplicates of an already-verified delivery**. They are
   NOT part of this task's 5 rows and must not be confused with a rework-filed outcome
   for branch #2. Left untouched (no merge, no closure) — flagging for the operator /
   backlog-dedup task rather than silently closing another row.
2. **Sibling branch with an un-landed devdoc:** `docs/glm-night-watch-verify-01a092a6`
   (tip `999ae086`, held by worktree `/home/user/workspace/gopi-verify-rpt`) carries
   `devdoc/glm-night-watch-single-launcher-verification-01a092a6.md`, which is **not** on
   master. It is a verification-report branch, not one of the 5 rows, and is out of scope
   here — recorded so a future agent does not mistake it for a lost deliverable.

3. **Old row `01a092b2-4f21-7d41-9bee-56a8480f150f` carries a stale
   `deliverable.branch` field.** `gopi task show` on that row (status `completed`,
   result "writeback-gap closeout: deliverable = branch fix/merger-status tip 518d3bdb
   (17 ahead) + devdoc/merger-status.md") nonetheless prints
   `deliverable.branch: feat/pa-never-runs-tasks` / `deliverable.worktree:
   /home/user/workspace/gopi-paguard` — the *previous* dispatch's deliverable block,
   never overwritten when the row was re-dispatched for the merger-status work. The
   landed content is unambiguous (`518d3bdb` is that landing's parent, `devdoc/merger-status.md`
   is byte-identical on master), so the row's `result` text — not its deliverable block —
   is the authoritative record here. Recorded, not fixed: rewriting a terminal ledger
   row's deliverable block is not a merge-backlog action. **Do not use the deliverable
   block on this row as a branch-name source.**

## Deliverable location (Rule 6 — this task performed no merge)

This report is committed on branch `feat/merge-backlog-writeback-gap-branches`
(`/home/user/workspace/gopi-merge-backlog-wbgap`), **not** on master: merging into
master is the merger's/PA's job, and this task deliberately ran no merge. Recovery:

```sh
git show feat/merge-backlog-writeback-gap-branches:devdoc/merge-backlog-writeback-gap-branches.md
```

## Not Done

- **No merge was performed** — none was needed; all 5 tips were already ancestors of
  master with their devdocs present. A merge would have been a duplicate/no-op.
- **No `-D` cleanup of the two stale rework rows** (residual finding 1) — those are ledger
  rows, not branches, and closing them is the ledger's/merger's business, not a merge
  backlog task's.
- **Branch #5's devdoc was extended on the branch** (blob `ed255c8f`) — that extension
  landed with the merge; nothing further was added here.

## Verification (summary)

| Gate | Command | Result |
|------|---------|--------|
| Ancestor of master (×5) | `git merge-base --is-ancestor <tip> master` | exit 0 (×5) |
| Landing commit on master (×5) | `git merge-base --is-ancestor <merge-commit> master` | on-master=YES (×5) |
| devdoc on master at the **exact asserted path** (×5) | `git cat-file -e master:<devdoc>` | PRESENT (×5), blobs identical to tips |
| Branch deleted (×5) | `git show-ref --verify -q refs/heads/<branch>` | absent (×5) |
| Build | `CGO_ENABLED=0 go build ./...` | exit 0 |
| Standards gate | `CGO_ENABLED=0 go test ./internal/standards/` | `ok ... 46.187s` (exit 0) |

Re-verified after the final two commits (`3d774129`, `32a23c1d`) that touched **only**
this report file (`git status --porcelain` → 0 lines, clean):

```
TREE=03b8b11c1f53626e63906effbf204ff0eced59a0 HEAD=960c7c1f DIRTY=0 MASTER=23e6da05
BUILD_EXIT=0
ok  	github.com/nick/gopi/internal/standards	48.355s
STD_EXIT=0
```

and `git diff --name-only 960c7c1f..HEAD` (the tree the gates ran at, vs the final
tip) → `devdoc/merge-backlog-writeback-gap-branches.md` only, so the green result
still holds for the delivered tip. `devdoc/merge-backlog-writeback-gap-branches.md`
is absent from the main repo's status (`/home/user/work/GoPi`: 0 lines) — the work
stayed entirely inside the assigned worktree (Rule 6b).
