<!-- doc-class: historical -->
# Rule 41 review: "the fix never landed and the ledger cannot tell" — measured

Task under review: `01a12b49-8921-7684-8928-4c17e259417f` (call it **T0**), commit
`ffa1cf4f8`. Reporter: task `01a12b63-88b5-7199-8921-95533ff2aaa8`, artifact
`rule41-orphan-evidence.txt` (2026-10-11 22:4x, on this client HP). This review run:
`01a12b6f-14f3-7b93-a921-ae8b6823d07d`, 2026-10-11 23:0x.

Raw transcript of every command below (re-runnable verbatim):
`~/.gopi/sessions/01a12b6f-14f3-7b93-a921-ae8b6823d07d/artifacts/rule41-review-evidence.txt`,
produced by that directory's `rule41-capture.sh`. Nothing here is committed except
this document (Rule 37: evidence lives in the session directory).

## Verdict, in three parts

| reporter's claim | verdict | ground |
|---|---|---|
| the commit is reachable from no ref | **NOT SUPPORTED** — and it was already false when asserted | 8 refs reach it on HP today, incl. `refs/heads/master`; measured `for-each-ref --contains` output below. At 22:4x the reachable set was non-empty too: the HP mirror of the delivered branch |
| the fix never landed | **REFUTED** | peerbox `master` = `a3a31da42` = *Merge branch 'task/01a12b498921768489284c17e259417f'*; `merge-base --is-ancestor ffa1cf4f8 master` → rc=0 on BOTH hosts; `symbol.go` sha256 identical at branch tip and in the landed tree |
| the close-out chain deleted the branch | **TRUE, and legitimate** | `removal-audit.log` 12721/12722: `finish-cleanup-branch` + `-force`, standing = *"this chain's own push landed the ref"*; peerbox's own reflog dates that push at 22:28:51 |

So the headline is a **false positive with a real residual defect hiding behind it**:
the delivery was never lost, but *"did this row's branch reach the server?"* has no
queryable answer — and the same audit stream shows why the question had to be asked
by hand.

## The timeline (real timestamps, both hosts)

| when (+0800) | what | evidence |
|---|---|---|
| 22:10:29 | T0 claimed by `hostb-Laptop:3656808` | `gopi task show` → `claimed_by` |
| 22:10:37 | ground taken: `~…/gopi-01a12b498921768489284c17e259417f`, branch `task/01a12b49…` | audit 12276 `arm=task-ground-anchor` |
| 22:26:42 | `ffa1cf4f8` committed on that branch | `git show -s --format=%ci ffa1cf4f8` |
| 22:28:30 | **row written terminal**: `completed`, `exit=0`, `ended_at=22:28:30` | `gopi task show` / `run` line |
| 22:28:51 | **push** — branch arrives at peerbox | peerbox `git reflog show refs/heads/task/01a12b49…` → `@{2026-10-11 22:28:51 +0800}: push` |
| 22:28:55 | worktree removed | audit 12720 `arm=task-self-reclaim` |
| 22:29:00 / 22:29:04 | `git branch -d` then `-D` on the client | audit 12721 / 12722 |
| 22:41:36 | **peerbox merges the branch into its master** | `git log -1 --format=%ci a3a31da42`; peerbox master reflog `merge task/01a12b49…` |
| 22:4x | reporter looks, sees no master containing the commit, files the bug | `rule41-orphan-evidence.txt` |
| 23:02:35 | HP's own `master` finally fast-forwards to that merge (`pull`) | HP master reflog `@{2026-10-11 23:02:35}: pull: Fast-forward` |

Three observations fall out of the table.

1. **The delivery ran AFTER the row went terminal** (22:28:30 → 22:28:51–22:29:04).
   Nothing about delivery state is recorded on the row; the row is finished 21
   seconds before the first delivery act.
2. **The client's `-d` refusal was truthful and expected.** At 22:29 the client's own
   `master` was `ace80b407` (pull 22:04:04) — peerbox had not merged yet, so
   `git branch -d` refused "not fully merged here". Every client-delivered row takes
   that path; the force arm's *only* standing is the delivery point, which is why the
   audit record must carry the delivered tip (see D2).
3. **The reporter was reading a stale client.** HP's master lags peerbox's by whatever
   the operator's last `pull` left; at 22:4x it was 32 commits behind. "master does
   not contain it" was true on HP at that moment and said nothing about the delivery.

## Command-level evidence

```
$ git cat-file -t ffa1cf4f8
commit

$ git for-each-ref --contains ffa1cf4f8            # HP, 2026-10-11 23:03
a3a31da42… commit  refs/heads/master
a3a31da42… commit  refs/heads/task/01a12b6f082b77459bc29d213951e3e8
a3a31da42… commit  refs/heads/task/01a12b6f14f37b93a921ae8b6823d07d
a3a31da42… commit  refs/remotes/gitPushcheck/master
a3a31da42… commit  refs/remotes/peerbox/master
122bde0e… commit  refs/remotes/peerbox/task/01a12b365af47e299a208b4f686bdcbb
b6a71361… commit  refs/remotes/peerbox/task/01a12b40f6de72e0be7a6b739f42d5b1
e81f64fb… commit  refs/remotes/peerbox/task/01a12b498921768489284c17e259417f

$ ssh peerbox 'git for-each-ref --contains ffa1cf4f8'   # 4 rows: master + 3 branch refs, incl. task/01a12b49…
$ ssh peerbox 'git merge-base --is-ancestor ffa1cf4f8 master'; echo rc=$?
rc=0
$ git merge-base --is-ancestor ffa1cf4f8 master; echo rc=$?      # HP
rc=0

$ for r in ffa1cf4f8 e81f64fb2 a3a31da42; do git show $r:internal/tools/symbol.go | sha256sum; done
62f12770d5dd252e…   (branch tip, pre-merge tip, and the merge commit — identical)
```

`git fsck --no-reflogs` reports **0** entries for `ffa1cf4f8`: the object is not
dangling, it is reachable without leaning on the reflog at all. What the reporter
saw was not an orphan — it was a commit whose *containing master* had not reached
this client.

One honest gap in this review: **the reporter's exact output line was not
recovered.** Their artifact keeps the command and the surrounding narration but the
`for-each-ref` stdout itself is absent; the run's raw log holds the invocation, not
its result. So "which refs it listed at 22:4x" is inferred from a today-run of the
same command against the same ref set, not read off their transcript. Marked as
inference; the refs that must have been listed (`refs/remotes/peerbox/task/01a12b49…`,
created at the 22:28:51 push) are independently attested by peerbox's reflog.

## The real residual defect (this is what gets filed)

**The audit stream for one (repo, branch, row) carries three `-d`/`-D` pairs, and two
of them are empty gestures on a branch that no longer exists.**

```
12721 14:29:00Z finish-cleanup-branch        01a12b49… task/01a12b49…   ← the real one
12722 14:29:04Z finish-cleanup-branch-force  01a12b49… task/01a12b49…
12750 14:52:55Z finish-cleanup-branch        01a12b49… task/01a12b49…   ← 27 min later, same pid
12751 14:52:55Z finish-cleanup-branch-force  01a12b49… task/01a12b49…
12771 15:02:58Z finish-cleanup-branch        01a12b49… task/01a12b49…   ← 10 min later, DIFFERENT pid (3896410)
12772 15:02:58Z finish-cleanup-branch-force  01a12b49… task/01a12b49…
```

Measured now: the local ref is gone (`git reflog show task/01a12b49…` → *unknown
revision*, `git for-each-ref | grep -c 01a12b49` → `0`), so 12750/12751/12771/12772
each re-ran a close-out chain whose work was already done. Two different processes
(3656808, then 3896410) ran it; the second is `serveFinishCleanupSweep`
(`cmd/agent/cli_tasks.go:3753`) walking terminal rows. `removalaudit`'s contract is
"a record per ATTEMPT, emitted before the git call" — correct in itself, and it is
exactly why the noise is visible.

Two structural consequences, both system-layer:

- **D1 — an already-cleaned row is swept again, indefinitely.** The chain's branch
  step refuses only on a *live* row (`finishCleanupBranchLiveOwnerReason`:
  pending/claimed/in_progress) and on a missing ref returns early, so nothing records
  "this row's delivery already ran". Each sweep still executes the steps and its git
  calls, and each attempt appends a pair of records. Harm: repeated work per terminal
  row, and — worse — the audit stream stops distinguishing *"a delivery was deleted"*
  from *"an empty gesture was made"*: both say `finish-cleanup-branch-force` with the
  same sentence.
- **D2 — delivery state is not queryable.** `BranchDelete(arm, repo, branch, taskID,
  evidence)` has no field for the deleted tip or the delivery point
  (`grep -n 'delivery_point\|"sha"' internal/removalaudit/audit.go` → no hits), and the
  row carries no delivery field at all. The 2026-10-07 epic `01a11689` (P1, item 4)
  asked for exactly *"add sha + delivery_point to the removalaudit row"*; measured
  today that item did not land. This is the reason the reporter could not settle
  their own question from the ledger and had to file a bug about it.

Neither is the 2026-10-07 defect (that one force-deleted an **undelivered** branch —
`finishCleanupBranchForceDeleteReason` now gates the `-D` on the sweep's own push,
and in T0's case the push really had landed). What is missing is the *evidence
trail*, and its absence is what made a healthy run read as a lost one.

## Not done / limits of this review

- The reporter's `for-each-ref` stdout at 22:4x is not recoverable from their
  artifacts (see above) — the finding rests on the ref set plus the timelines.
- No system-layer code was changed: Rule 28 puts the close-out chain, the merger and
  the sweep on peerbox. The residual defect is filed as a bug row instead.
- `ffa1cf4f8`'s branch was read only; nothing was built on it (Rule 23).
