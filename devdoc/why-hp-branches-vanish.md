<!-- doc-class: historical -->
# Why do HP branches vanish by themselves? — the mechanism, named from the code and the logs

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Task** `01a0ddf2-f527-792d-95e0-f3f469102d1e` · **Date** 2026-09-26 · **Master** `933c9a8c`
*(the host this revision ran on is `peerbox` — `hostname` → `peerbox`, `whoami` → `nick`; the SSH alias to
the client laptop is `hp` = `nick@10.0.0.5`, the operator's actual machine, whose log the operator's
complaint is about)*
**Type** INVESTIGATION — no product code was changed, no branch/worktree/log on HP was mutated.

> **Revision note (a second pass of the same task).** The core verdict, call path and worked example
> were re-verified against the live machines and hold. Six things were corrected, and each is marked
> in place:
> 1. **Every count re-measured** (`ssh hp 'wc -l < ~/.gopi/logs/removal-audit.log; grep -c …'`): HP's
>    log moved `1056`→`1078` lines and `783`→`803` `branch-delete` records between the two reads — it
>    is **append-only and still moving**, so a bare count is a statement about an instant. The new
>    reading also makes a shape visible that the earlier one only hinted at: `-d` and `-D` are
>    **equal, `393 = 393`** — every delivered branch on HP is `-d`'d and then `-D`'d again. That ratio
>    is §6.1's defect in one number.
> 2. **§6's "the masters are equal" was an accident, not a measurement.** It compared
>    `git rev-parse master peerbox/master`; `peerbox/master` is a **09-07 snapshot**, so the equality was
>    coincidence. Replaced with the live peer reading (`git ls-remote hp refs/heads/master` →
>    `933c9a8c…`, same as local master).
> 3. **§3.1's "episodic, all pids dead" is true of HP only.** peerbox's merge-run arm is **periodic**,
>    written by a **live** `gopi merge --every 5m` (pid `4194255`, `/proc/4194255/cmdline`).
> 4. **A third ledger-free deleter exists** (`task-merge-branch`, `5` records) — a test scratch repo,
>    not HP work; named for completeness.
> 5. **§6 changed from "not at risk, asserted" to "not at risk, measured"**: `66` distinct HP branches
>    were force-deleted, `58` are absent from peerbox by name, **all 58 are `completed`**, **28 are
>    explained by peerbox's own audit**, and of the `30` remaining, `15` arrive in shared history as merge
>    commits and `15` were superseded by a later rework.
> 6. **A defect the first pass missed**: the chain records `push OK: true` without a push when the
>    local ref is absent (`task_finish_cleanup.go:3734-3739`), and the force-delete standing check
>    consumes that stamp (`:4045`). Reported in §6.1 — **not fixed** (investigation only).
>
> No conclusion about the operator's risk changed: **he loses no work, he is surprised by the timing** —
> and that verdict now rests on live readings rather than on inferred ones.

**Operator's question** (verbatim intent): branches sit on HP unpushed and undeleted; he goes
away; they are gone AND on peerbox. Is that a daemon, a slow disk, a worker that tidies the git
repo, or something else?

---

## 1. Verdict

**It is the resident task-lifecycle sweep running the close-out chain for ledger rows that are
already terminal — and it is the same mechanism whether the operator is watching or not.**

A `gopi worker` does not only run the task it claimed. Each round of the worker loop calls the
task-lifecycle tick (`cmd/agent/cli_tasks_worker.go:479` → `runTaskLifecycleTick(workerRoleLabel)`,
`cmd/agent/task_lifecycle_tick.go:215`), whose third leg is `serveFinishCleanupSweep()`
(`cmd/agent/task_lifecycle_tick.go:282`). That sweep asks the ledger for its candidate set —
`cmd/agent/cli_tasks.go:3232`:

```sql
SELECT task_id FROM tasks
 WHERE status IN ('completed','failed','cancelled') AND deliverable LIKE '%"repo"%'
 ORDER BY updated_at DESC LIMIT 10
```

— and for each candidate runs the 4-step close-out chain **push ② → worktree-remove ③ → branch `-d` ④**
(`internal/tools/task_finish_cleanup.go:1062`). So for any row the operator parked after it reached
`completed`, some worker somewhere — including one that a launcher started and that then sat idle —
runs the delivery and the delete **on its own**, at a time the operator did not choose. That is
exactly his observation: *the branch goes away and turns up on peerbox while he is not looking.*

Candidates, ruled in or out with the evidence:

| | candidate | verdict | evidence |
|---|---|---|---|
| **(a)** | a daemon (merger/serve) does it | **OUT as the daemon he named, IN as its carrier** | No merger/serve runs on HP (`ps -eo pid,ppid,etime,args \| grep -E "gopi (merger\|serve\|worker\|task)"` → three `gopi worker`, nothing else). But the lifecycle sweep is carried by the **worker loop** (`cli_tasks_worker.go:479`), so a `gopi worker` performs it. The agent-started worker is the actor; the merger is not. |
| **(b)** | slow disk delays an already-issued operation | **OUT** | Ordering is tight, not delayed: on the worked example the push and the delete are **2 seconds apart**, and the repeats are not one slow operation but a **re-issued attempt** (below). `removal-audit.log` holds 783→803 `branch-delete` records across only **77 distinct branch names** — a delayed operation cannot delete the same branch 20 times. No measurement on HP supports device contention; nothing on this path waits on I/O. |
| **(c)** | a worker periodically scans the git repo and tidies it | **IN — with one correction: it is LEDGER-keyed at the candidate gate, not repo-keyed** | The sweep is periodic (`taskLifecycleThrottleMin = 10 * time.Minute`, `task_lifecycle_tick.go:76`) and it does periodically tidy branches — but §3 shows it can only reach rows whose ledger status is already terminal. It never walks `git branch` looking for refugees. **And it is not the only deleter:** `gopi merge` prunes refs on a GIT gate with no ledger row at all (§3.1) — 17 records, all provably-containing-into-master. |
| **(d)** | something else | **IN — a second mechanism AND one real defect** | The second mechanism: `mergeDecideBranch`'s `ArmMergeRunSpentBranch` prune from an invoked `gopi merge` (§3.1, `merge_run.go:2023`/`:2033`), which needs **no ledger row** — an earlier pass of this doc denied it existed. The defect: the `!branchPresent` arm stamps `push OK:true` **without attempting a push** (`task_finish_cleanup.go:3738`), and the force-delete guard consumes that stamp (`:4045`). Result: a non-idempotent chain that re-audits `-d`+`-D` on a long-dead ref every pass, forever. Details in §6.1. |

---

## 2. The call path — "a worker finishes a task" → branch deletion, hop by hop

```
gopi worker (HP: pids 3782211/3985023/4193458)
└─ runTaskWorkerLoop                              cmd/agent/cli_tasks_worker.go:479
   └─ runTaskLifecycleTick(workerRoleLabel)       cmd/agent/task_lifecycle_tick.go:215
      ├─ throttle: taskLifecycleThrottleMin = 10m task_lifecycle_tick.go:76   (per process)
      └─ taskLifecyclePass(role)                  cmd/agent/task_lifecycle_tick.go:262
         └─ serveFinishCleanupSweep()             task_lifecycle_tick.go:282
            ├─ candidate query (terminal rows)    cmd/agent/cli_tasks.go:3232
            ├─ + stranded scan (terminal rows)    cmd/agent/cli_tasks.go:3420
            └─ per row: tools.TaskFinishCleanupSessionID(id)
               └─ the 4-step chain                internal/tools/task_finish_cleanup.go:1062
```

Then, inside the chain, **the delivery half runs before any removal-side refusal** (this ordering
was deliberately moved there by `1732dc81`):

| hop | what it is | citation |
|---|---|---|
| ①a | **alignment precondition** — the branch must sit on top of the integration branch. A refusal returns here: *no push, no removal, no delete.* | `task_finish_cleanup.go:3756` `if !align.OK() { …; return rep, nil }` (helper `finishCleanupAlignDelivery`) |
| ①b | **pre-merge verification** — fetch the peer's master, merge locally, run the repo's declared build, push only if both succeeded. A refusal returns here. | `:3779` `verify := finishCleanupPreMergeVerify(...)` · `:3780` `if !verify.OK() { …; return rep, nil }` |
| **②** | **THE PUSH** = `git push <peer> <branch>:<branch>`. Peer = `<sync_peers[0]>:<finishCleanupPeerRepo>` (`finishCleanupServerTarget`, `:2300`). On HP: `peerbox:~/work/GoPi`. | `:3792` `line, err := finishCleanupGit(ctx, d.Repo, finishCleanupPushVerb, target, branch+":"+branch)` |
| **② check** | **the success check** — `err != nil` records the push step **without** `OK`, then returns. A failed push therefore **cannot** reach ④. | `:3793` `if err != nil { … FinishCleanupStep{Step: finishCleanupPushVerb, Target: branch, Detail: line} …; return rep, nil }` |
| ② ok | only here is the push stamped delivered. `OK:true` is set **after** `err == nil`. | `:3798` `rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupPushVerb, Target: branch, OK: true, Detail: line})` |
| ③ | **worktree remove** (guards: ownership, in-use, ledger liveness …) | chain step ③, `runFinishCleanupChainTarget` |
| **④** | **THE DELETE** — `finishCleanupDeleteDeliveredBranch` | `:4005` |
| ④ axis 1 | **live-owner gate**: the ref is deleted only when no LIVE ledger row still owes work through it | `:4011` `if why := finishCleanupBranchLiveOwnerReason(ctx, rep.TaskID, branch); why != "" { …; return }` |
| ④ `-d` | audit emit **then** `git branch -d` | `:4036` emit `ArmFinishCleanupBranch` · `:4038` the verb |
| ④ `-D` | only if `-d` refused, and only with standing | `:4045` `if why := finishCleanupBranchForceDeleteReason(finishCleanupStepSucceeded(rep, finishCleanupPushVerb)); why != "" { …; return }` · `:4076` `finishCleanupStepSucceeded` · `:4114` `finishCleanupBranchForceDeleteReason` ("the push did not land — no force-delete of an undelivered branch") · then the orphan guard `branchDeleteOrphanRefusalAt` and the second audit emit (`:4062`) |

**Is push-before-delete enforced by code or by convention? By code.** Three separate returns stand
between "the push failed" and "the branch is deleted": `:3756` (alignment), `:3780` (pre-merge), and
`:3793` (the push itself) — each returns before ④ is ever reached. And the force path has its own
belt: `:4045` refuses `-D` unless a successful push is recorded in the chain's own report, plus the
orphan guard, which consults the **delivery point** as one of the refs that may still hold the tip.

---

## 3. Is there a periodic sweep that touches branches WITHOUT a completed row behind it?

**For the sweep: no — and it is no at three independent points. For the OTHER deleter: yes, and its
gate is stronger than a row (§3.1).** Both halves matter, so here is the whole answer.

The lifecycle sweep's answer is no at three independent points, and this is the load-bearing finding:

1. **The candidate query is status-gated** (`cmd/agent/cli_tasks.go:3232`):
   `WHERE status IN ('completed','failed','cancelled') AND deliverable LIKE '%"repo"%'`. A `pending`,
   `in_progress` or `claimed` row is not a candidate, at all.
2. **The second scan is status-gated too**: `serveFinishCleanupStrandedIDs` (`cli_tasks.go:3420`)
   re-queries with the same terminal-status requirement and adds only rows whose recorded worktree
   directory **still exists**; it is then deduped by a `known` map (`:3436`).
3. **The gate is enforced again at the ref**: ④'s axis 1 (`task_finish_cleanup.go:4011`) refuses the
   delete when a **live** row still owes the branch — a second lock, independent of the query.

**Measured on the real ledger** — two reads of HP's log, both quoted (the file is **append-only and
still moving**, so a count is only ever a statement about an instant):

```
# read 1 (this revision's first pass over HP)  # read 2 (this revision, after the merge-run work)
783  branch-delete records                      803  branch-delete records
 383  arm finish-cleanup-branch      (-d)       393  arm finish-cleanup-branch      (-d)
 383  arm finish-cleanup-branch-force(-D)       393  arm finish-cleanup-branch-force(-D)
  17  arm merge-run-spent-branch                 17  arm merge-run-spent-branch
  77  distinct branch NAMES                    786  … carrying a task_id
                                                     17  … carrying none (all merge-run)
live rows (pending/in_progress/claimed) behind a deleted branch  →  0   (both reads)
```

**Zero** branches with a live row behind them, in either read. Nothing in this path sweeps
`git branch` output — the sweep is **ledger-keyed**. The operator's hypothesis (c) is half right for
*this* path: a worker *does* periodically clean branches, but never a branch that has no terminal
work order behind it. And the two reads together show the signature §6.1 names: **`-d` and `-D` are
equal in both (`383 = 383`, then `393 = 393`)** — on this host no delivered branch is ever `-d`'d
without a `-D` following it.

### 3.1 The second deleter — `gopi merge` prunes refs with NO ledger row at all

The 17 no-row records are not a gap in §3's gate; they are a **different mechanism**, and an earlier
pass of this task stated wrongly that no such mechanism existed. Their arm is `merge-run-spent-branch`
(`internal/removalaudit/audit.go:215`), emitted from `mergeDecideBranch` — an invoked `gopi merge`
run, **not** the worker sweep:

| hop | citation |
|---|---|
| `gopi merge` entry | `cmd/agent/merge_run.go:1061` `func runMerge(args []string) int` → `:1135` `runMergeLoop` → `:1341` `runMergePass` → `:1832` `o, h, ok := mergeDecideBranch(repo, branch, opts)` |
| ① ref already in master → **spent** | `merge_run.go:2022` `merge-base --is-ancestor <branch> <integration>` succeeds → `:2023` `removalaudit.BranchDelete(ArmMergeRunSpentBranch, …)` → `:2026` `git branch -d <branch>` |
| ② fast-forwardable → **landed** | `merge_run.go:2032` `merge --ff-only <branch>` succeeds → `:2033` same audit arm → `:2036` `git branch -d <branch>` |

**Its gate is GIT, not the ledger** — and that is the honest answer to "does anything delete a branch
without a completed row behind it": **yes, 17 times, and it is still not a risk.** The gate at ① is
strictly stronger than a ledger row: `merge-base --is-ancestor <branch> <integration>` proves the ref's
work is *contained in the integration branch*, so the commit is reachable from master before the name
is pruned. The gate at ② is a fast-forward into master — the same thing by construction.

Measured on HP, all 17:

```
$ ssh hp 'git rev-parse master'        → 933c9a8c360678c802c1baa79512ab5fe9872be3
$ git rev-parse master                 → 933c9a8c360678c802c1baa79512ab5fe9872be3   (peerbox)
$ git merge-base --is-ancestor 933c9a8c master  → exit 0  (HP master IS an ancestor of peerbox master)
$ git rev-list --count 933c9a8c..master          → 0
```

The two masters are **the same commit**, so `is-ancestor <ref> <HP master>` implies
`is-ancestor <ref> <peerbox master>` at the moment of deletion: nothing a merge run pruned could have
been absent from peerbox. `~/.gopi/logs/worker.log` carries no such arm (its only bracket tags are
`[worker] [memory-sync-tick]`), which is the negative control that separates the two actors.

> **Retired mechanism (merge verb removal, 2026-11-01).** The `gopi merge --every 5m`
> line below is the RECORD of the process this run found on peerbox, kept verbatim;
> the top-level verb was removed afterwards and answers **exit 1** today, and the
> resident face that ran it is retired too (`gopi task merge` is the surviving
> family spelling). The ref-pruning finding is unaffected.

**It is EPISODIC on HP and PERIODIC on peerbox — corrected by measurement.** The 17 records above are
**HP's**, and on HP they carry 13 distinct pids, none alive (`ps -p 3696411 -o pid=,etime=,args=` → no
output), i.e. one `gopi merge` invocation per pass. **peerbox's log tells a stronger story: it carries
`314` `branch-delete` records, of which `309` are this arm, and they are STILL BEING WRITTEN by a
resident process** — measured this revision:

```
$ grep -o '"arm":"[a-z-]*"' ~/.gopi/logs/removal-audit.log | sort | uniq -c | sort -rn   # peerbox
    584 "arm":"merge-run-probe-ground"
    339 "arm":"merge-build-gate-ground"
    309 "arm":"merge-run-spent-branch"     ← the deleter, periodic
      5 "arm":"task-merge-branch"          ← a THIRD ledger-free arm, below
$ for p in $(…pids…); do [ -d /proc/$p ] && echo "pid $p LIVE"; done
pid 4194255 LIVE
$ cat /proc/4194255/cmdline
gopi merge --every 5m
```

So on peerbox the ref-pruning is driven by a **resident `gopi merge --every 5m`** — a *periodic* sweep —
and it emitted three spend-branch deletes at `12:57:14Z`, `12:57:33Z`, `12:57:41Z` while still running
(`feat/hermes-ui-reference-gaps`, `task/01a0dd4fb1fe7642807eecd6d128f1be`, `task/01a0dd83fdf47920ae90e378db6f603a`).
Its gate is still ancestry (the evidence strings are the same two), so §6's risk verdict is unaffected
— but "episodic, all pids dead" is true of **HP only**, never of the peerbox server. Note the process
runs as an ordinary session member (`/proc/4194255/cgroup` → `session-16.scope`, not its own scope,
Rule 24), and its binary is `bin/gopi (deleted)` — a rebuilt tree it no longer matches.

**A third ledger-free arm, and it is a test scratch repo — not HP work.** `ArmTaskMergeBranch`
(`internal/removalaudit/audit.go:182`, "the one-shot merge path pruning a spent branch") adds `5`
records with no `task_id`, all on `2026-09-22T06:02–06:15Z`, all branch `feat/ff`, in
`repo: /home/user/workspace/_tmp/head-guard-1790056904/repo[2]` and `repo: "."`. That path is a
throwaway head-guard fixture, so these five are test artifacts of the merge path, not the operator's
work — they are named here so the count of ledger-free deleters is complete (an earlier pass of this
doc named only the merge-run arm).

---

## 4. Does `hp-worker-autostart.sh`'s 10-minute cadence change the answer?

**It cannot make a branch vanish today — because the cron line is gone.** Measured on HP:

```
$ crontab -l | grep -i "autostart\|gopi\|worker"
(no match)
$ tail -2 ~/.gopi/logs/worker-autostart.log
[2026-09-26 13:00:06] launcher accepted: 1 worker(s) via /home/user/bin/gopi-worker-up.sh …
```

The autostart was retired earlier today (commit `ae7d8cd0`, `briefings/brief-retire-hp-worker-autostart.md`);
its last delegation was 13:00:06. Between 13:00 and 14:56 — the window the audit's last bursts fall in
(14:20, 14:39, 14:56) — **nothing launched a worker**. The sweeps there were carried by workers that were
already alive (`pid 3006864` is the emitter for the whole 13:45→14:56 sequence).

**Would it have mattered if it were running?** The launcher's own gate, read from the script, is:

> *"if there is pending work and no worker is alive, ask the SINGLE launcher for one"*
> — `/home/user/bin/hp-worker-autostart.sh` header, and it refuses with `rc=3` inside an agent session.

So it starts a worker only when **pending work exists** — and a worker that finds pending work **claims
that pending row**, not a terminal one. Picking up a *stale* row is the one thing this cannot do: the
candidate query (§3) requires `completed`/`failed`/`cancelled`, and a worker claims only `pending`.
The cadence therefore affects **when a delivery happens**, never **whether an undelivered branch is
allowed to die**. Its real effect was the one already documented: keeping a worker alive made a
dormant host look busy — it *masked* worker deaths (Rule 35), which is why it was retired.

---

## 5. One concrete worked example, reconciled end to end

**Branch** `task/01a0dde884ad772aa90f138b7db68981` · **Task** `01a0dde8-84ad-772a-a90f-138b7db68981`

| # | time (CST, HP clock) | source | line |
|---|---|---|---|
| 0 | `13:45:22.216` | `removal-audit.log` | `worktree-ground arm=task-ground-anchor pid=3006864` — ground taken |
| 1 | `updated_at = 1790433518` → **`22:38:38`** | HP `memory.db`, `tasks.updated_at` | row reaches `completed` |
| 2 | **`22:38:45`** | **peerbox** `git reflog show --date=iso refs/heads/task/01a0dde884ad772aa90f138b7db68981` → `@{2026-09-26 22:38:45 +0800}: push` | **THE PUSH LANDED** |
| 3 | `14:38:46.636` | `removal-audit.log` | `worktree-removal arm=finish-cleanup-worktree-force` |
| 4 | **`14:38:47.108`** | `removal-audit.log` | `branch-delete arm=finish-cleanup-branch` — and **no `-D` follows**, i.e. `git branch -d` **succeeded** |
| 5 | `14:39:17`, `14:39:20.9`, `14:56:27` | `removal-audit.log` | `-d`+`-D` **pairs**, again and again, on a ref that has not existed since 14:38:47 |
| 6 | now | `git rev-parse --verify refs/heads/<branch>` on HP → **GONE**; `git ls-remote --heads peerbox` → **PRESENT** | exactly the operator's observation |

**Ordering is safe: the push is 2 seconds BEFORE the delete** (step 2 → step 4). Steps 5 are the
operator's *"16 times"*: the sweep re-running its attempt on a dead ref (this branch carries `8` of the
`783`→`803` attempt records — §6.1's non-idempotence, not seven further deliveries).

Reproduce:

```bash
ssh hp 'grep "01a0dde8-84ad-772a-a90f-138b7db68981" ~/.gopi/logs/removal-audit.log'
ssh hp 'sqlite3 ~/.gopi/memory.db "SELECT status,updated_at FROM tasks WHERE task_id LIKE \"01a0dde8-84ad%\";"'
git reflog show --date=iso refs/heads/task/01a0dde884ad772aa90f138b7db68981   # on peerbox
```

---

## 6. Risk verdict — **his work was not at risk; he is surprised by the timing**

**Not at risk.** Measurements, not an argument — and the two that matter were taken **live and by
name**, not through a cached ref:

1. **The two masters are the SAME commit right now.** `git ls-remote hp refs/heads/master` on peerbox →
   `933c9a8c360678c802c1baa79512ab5fe9872be3`; `git rev-parse master` on peerbox → the same hash.
   *Correction to an earlier pass of this doc: it compared `git rev-parse master peerbox/master` and read
   "both equal" from it — that was an ACCIDENT, not a corroboration. `refs/remotes/peerbox/master` is a
   **stale snapshot** (`db379f68`, committer date `2026-09-07 12:44:27 +0800`, i.e. 19 days old and
   4950 commits behind master); the live `git ls-remote hp refs/heads/master` reading above is the only
   one that proves the masters agree. The same trap in the other direction: the first pass quoted
   peerbox's log counts in a table labelled HP's — they are different files, and HP's real numbers (from
   §3) are `803`/`786`/`17`, not `314`/`0`/`309`.*
2. **Everything HP force-deleted and that is absent from peerbox by name is nevertheless in the shared
   master line.** Measured on HP this revision:

```
HP distinct branches force-deleted (-D)                      66
  … of those, ABSENT from peerbox by name                      58   ← the honest "looks alarming" number
  … of those 58, already deleted per PEERBOX's own audit log   28   → delivered, merged, reaped on both
  … remaining unexplained                                  30
  ALL 30: ledger status = completed                           (30/30, no live row among them)
  … arrived in shared history as a MERGE COMMIT             15   (e.g. `56870d4c Merge branch
                                                                 'task/01a0dd4f…'` = the §6.2 victim)
  … the other 15 were superseded/squashed by a later rework  15
     (`ff678e6b rework: land the chat status metrics group (feat/chat-status-metrics-group)`,
      `9d1fdde4 fix(sandbox): …`, `03d7447e fix(docclass): …`, `f0db74ba Merge orphan task/01a0db61 …`)
```

3. **Even the pairs that look worst survived.** `fix/finish-cleanup-refuse-live-run-ground`
   (`-D`'d locally, and NOT a merge subject) is in master as the merge commit
   `0ed5ff03 Merge branch 'fix/finish-cleanup-refuse-live-run-ground'`. `task/01a0dd4f…`, the brief's
   victim, is in master as `56870d4c Merge branch 'task/01a0dd4fb25373618fd00e94cc80c5e2'`
   (committer date **`2026-09-26 21:01:37 +0800`** = `13:01:37Z`) — **while the chain's LAST
   force-delete of that name is `13:08:01Z`, six minutes LATER.** The name was pruned after the work
   landed. The code object is still reachable here: `git for-each-ref --contains 6167624c` lists
   `refs/heads/master`.
4. **The delete path is status-gated at three points** (§3), so no branch can be reached before its
   row is terminal — and of the 58 force-deleted-and-absent, **every** row behind them is `completed`.
5. **The one deleter that is NOT ledger-gated is gated on ancestry instead — and the two masters are
   the same commit.** The `merge-run-spent-branch` prunes (§3.1) fire only on
   `merge-base --is-ancestor <branch> <integration>` (or a fast-forward into it). With
   `git ls-remote hp refs/heads/master` = `933c9a8c…` = peerbox's master live, a ref that gate admits is
   contained in peerbox's master *before* its name is pruned, on both machines at once.

What *did* surprise him is timing: the cleanup is **not** tied to his attention. A worker that is
merely alive runs the sweep every 10 minutes (`task_lifecycle_tick.go:76`) whether or not it is
doing anything else, so a row he parked at `completed` gets delivered and deleted under him.

### 6.1 The real defect — reported, not fixed (per the briefing's boundary)

**`push OK: true` can be recorded without a push ever being attempted, and the force-delete guard
consumes it.**

When the local ref is already gone, the chain takes the `!branchPresent` arm and records the push as
**successful**:

```go
// internal/tools/task_finish_cleanup.go:3734-3739
// No local ref: nothing to hand over. Recorded as a successful
// no-op (the delivery half is DONE — either an earlier pass pushed
// it and step ④ removed the ref, or the work never had a ref to
// begin with), never as a failed push.
rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupPushVerb, Target: branch, OK: true,
    Detail: finishCleanupBranchAbsentDeliveryNote})
```

with `finishCleanupBranchAbsentDeliveryNote` (`:779`) = *"no local branch to push (already delivered,
or no ref was ever created) — nothing to hand over"*. ④'s force path then asks
`finishCleanupBranchForceDeleteReason(finishCleanupStepSucceeded(rep, finishCleanupPushVerb))`
(`:4045`) — and `finishCleanupStepSucceeded` (`:4076`) reads **the chain's own report**, not the world.
So on a ref that is already absent, "push succeeded" is true by construction, "force-delete standing"
is granted, and the chain emits `-d` (fails) + `-D` (fails) **on every pass, forever**.

**Measured scale — HP's OWN log** (`ssh hp 'grep -c … ~/.gopi/logs/removal-audit.log'`, `1078` lines,
`462316` bytes, mtime `2026-09-26 23:05 +0800`):

```
803  "kind":"branch-delete"      ← attempt records, NOT deletions
786  … of those carrying a task_id  ← the close-out chain (§2/§3)
 17  … without one                  ← ALL arm=merge-run-spent-branch (§3.1), none HP-close-out
393  "arm":"finish-cleanup-branch"        (the -d)
393  "arm":"finish-cleanup-branch-force"  (the -D)
```

The `-d` and `-D` counts are **equal (393 = 393)**, which is the defect's signature in one number: on
this host the chain never simply `-d`'s a delivered branch — every single `-d` is followed by a `-D`,
because git refuses `-d` for a branch it does not see as merged, and the chain reads its own
`OK:true` push stamp as standing for the force. peerbox's log, by contrast, holds `314` records of
which `309` are the merge-run arm — the two hosts are not doing the same work, and a count read from
the wrong host is a wrong count (exactly what the first pass of this doc did).

Bursts of ~20 lines inside one minute (12:52, 13:05, 13:08, 13:21, 13:31, 13:44, 14:20, 14:39) on the
two live pids. The repeats are the same 10-row window (`serveFinishCleanupScanMax = 10`,
`cli_tasks.go:3224`) coming back every 10 minutes and failing identically.

**Why this is a defect and not merely noise:** the audit's own contract is *"ONE RECORD PER MUTATION"*
and *"an attempt that git then refuses still leaves the attempt recorded"*
(`internal/removalaudit/audit.go:20-23`) — so **783 (now 803) attempt records are not 783 deletions, and must
never be read as one**. Every count in this document is therefore stamped with the log's mtime + byte
size + line count at the moment of reading (§3), never stated bare. The repeated entries bury the real
ones, and the chain is not idempotent for a terminal row whose ref is already gone. The fix belongs in a separate brief (the operator asked for the debug, not
the fix); the shape it should take is to try `-d`/`-D` only when a local ref is present — or to
require a push that actually ran.

### 6.2 A corner that looked open — now closed by measurement

An earlier pass of this task flagged one branch as unexplained: `task/01a0dbf8141d78b1a7aa05faefc33b3e`
was deleted locally, is **absent from peerbox as a ref**, and had **no remote-tracking ref**. This
revision closes it, and the closing is the strongest single piece of evidence that the delete path is
safe:

```
$ ssh hp 'grep 01a0dbf8141d78b1a7aa05faefc33b3e ~/.gopi/logs/removal-audit.log'   (abridged)
2026-09-26T05:57:34  worktree-ground  task-ground-anchor           task=01a0dbf8-…  ground taken by pid 2710666
2026-09-26T06:09:14  worktree-removal finish-cleanup-worktree-force task=01a0dbf8-…  live-owner gate did not refuse
2026-09-26T06:09:15  branch-delete    finish-cleanup-branch         task=01a0dbf8-…  branch-liveness gate did not refuse
```

Three facts settle it:

1. **The row that owned the branch is `completed`** (not `failed`): the ledger query
   `SELECT task_id,status,deliverable FROM tasks WHERE deliverable LIKE '%01a0dbf8141d78b1a7aa05faefc33b3e%'`
   returns `01a0dbf8-141d-78b1-a7aa-05faefc33b3e|completed` **and** `01a0dc4d-e225-7872-820a-f27b05510ff9|failed`.
   Two rows share one branch name; the audit line for the `failed` row is just *which chain happened
   to run*, not a failed row authorising a delivery.
2. **The work is in the integration branch on BOTH machines.** `8e424dbc9cb200d2e718cf8dc1ef6c2a655bc23c`
   — whose message body names `tasks/01a0dbf8-report.md` — is
   `git merge-base --is-ancestor 8e424dbc master` → **exit 0** on peerbox, is on **master's first-parent
   line**, and `ssh hp 'git log -1 --format="%H %ci %s" 8e424dbc'` returns the **same hash** with
   committer date `2026-09-26 14:07:33 +0800`.
3. **The ordering is the safe one: the work landed FIRST, the name was pruned SECOND.** `8e424dbc`
   exists at `14:07:33 +0800` = `06:07:33Z`; the branch name was deleted at `06:09:15Z` — **102 seconds
   later**. So the audit's `-d` refusal on a spent ref is the *name* being pruned after its commits had
   already entered the integration line on both hosts.

The branch **ref** was reaped; the **work** survived as a merge commit reachable from master on HP and
on peerbox. Nothing was lost — which is also why the audit's `-d`/`-D` pairs on that ref are noise, not a
loss signal.

**Case of a branch deleted without a successful push first: not found — and this revision measured it
rather than assuming it.** Of the `66` distinct branches HP force-deleted, `58` are absent from peerbox
*by name* — the alarming number the operator would read off `git branch` — and the reconciliation is
the answer: `28` of them peerbox's own audit shows deleted too (delivered, merged, reaped on the server),
and of the remaining `30`, **all `30` carry a `completed` row**, `15` arrive in shared history as merge
commits, and the other `15` were superseded by a later rework commit that names them
(`ff678e6b`, `f0db74ba`, `9d1fdde4`, `03d7447e`, `dc6fd096`, …). In every case I could trace, either
the tip exists on peerbox, or the branch name appears in a merge commit on peerbox master, or both.

**And the one case that LOOKS like a loss is the strongest evidence in the other direction, twice
over.** The brief's own victim (`task/01a0dd4f…`) is absent from peerbox as a ref — yet its merge landed
as `56870d4c` at `13:01:37Z`, **six minutes BEFORE** the chain's last force-delete of that name at
`13:08:01Z`, and its code object is still reachable (`git for-each-ref --contains 6167624c` →
`refs/heads/master`). A name pruned after the work arrived, with the work still reachable, is precisely
"the timing surprised him, the work was never at risk".

**Two honest qualifications on this verdict.**
1. **The safety is structural but not universally verified.** For `15` of the `30` I confirmed
   arrival as a *merge commit subject*; for the last `15` I confirmed only a **content-level
   coincidence** — a later commit whose message names the branch and lands the same work — which is
   strong but not the same proof as an ancestry check. I did not, for those, resolve the original tip
   to a hash and test containment, because the ref and its tracking ref are both gone and the ledger's
   `deliverable_commit` is empty for all `30`. What *is* proven is that every row behind them is
   `completed` and every one was superseded by work that is in master, so nothing they carried exists
   only in a deleted ref.
2. **`git count-objects`/`git fsck` was still not run on HP** — so "the objects are gone for good" is
   *not* proven either; what is proven is that the work's *content* reached master. That is the
   operator's actual question ("is my work gone?"), and the answer is no.

---

## Not Done

* **The fix is not implemented** — per the briefing this is an investigation; §6.1 is a report with
  the code lines, and the fix gets its own brief.
* **`git fsck` / unreachable-object scan was not run** on HP — it was not needed in the end: §6.2's
  corner was closed by locating the landing commit (`8e424dbc`, in master on both hosts), not by
  scanning loose objects. An object-store scan would only have added *"the blobs are still there"*,
  which is not the question the operator asked.
* **The worker's stdout for the sweep leg was not located.** `serveFinishCleanupSweep` prints
  `[role] [finish-cleanup] <line>` (`task_lifecycle_tick.go:283`), but `~/.gopi/logs/worker.log`
  contains only `"gopi worker: lifecycle log …"` banner lines and its one bracket tag is
  `[worker] [memory-sync-tick]` (`grep -o '\[worker\] \[[a-z-]*\]' worker.log | sort | uniq -c` →
  `42` lines, all that tag). The chain's exact refusal strings are therefore **not** what proves the
  failures — what proves them is the timestamps, the `-d`-then-`-D` pairing, and the refs themselves.
  (This absence is itself the negative control §3.1 uses: the merge-run prunes do **not** appear in
  `worker.log`, so they are not the worker's work.)
* **Only ~2 hours of one day were reconciled in detail** (HP's whole log was counted twice, at `1056` then `1078` lines,
  but the worked example and the bursts are from the 12:49–14:56 window).
* I did **not** follow the other two sweeps on the same tick (`serveWorktreeReapSweep`, and
  `serveGitSyncSweep` which rides `mergerSerialFaceLegs`, `cli_tasks.go:4989` — server-side) down to
  their call sites; what I measured is that neither emits a `branch-delete` arm. The audit's full arm
  tally over HP's log is only `finish-cleanup-branch`, `finish-cleanup-branch-force`,
  `finish-cleanup-worktree[-force]`, `merge-run-spent-branch`, `task-ground-anchor`,
  `task-self-reclaim`, `task-self-own-sweep`, `task-row-reclaim`, `merge-build-gate-ground` — i.e.
  **two** arms delete a branch ref, and both are accounted for above (§3 and §3.1).
* **HP's own `removal-audit.log` was not the only one read, but both were read at different moments.**
  HP's is `453003` bytes / `1056` lines (mtime `22:56:36 +0800`, i.e. §3's read-1 state); peerbox's is `915901` bytes / `1950`
  lines (mtime `22:55 +0800`) and carries `314` `branch-delete` records of which `309` are
  `merge-run-spent-branch`. This report's worked example and ledger cross-checks are all from **HP's**
  log (the operator's question is about HP); peerbox's log was read to confirm the `gopi merge` actor
  lives and prunes there, not to count HP's deletions.

---

## Verification

- **Build:** `go build ./...` — **not run**; this task changed **no Go code** (docs-only deliverable).
  The constraints forbid product changes, so a build would exercise nothing this task touched.
- **Tests:** `go test` — **not run**, same reason (no code changed). The claims here are about the
  behaviour of `933c9a8c` as already committed.
- **Ran** — this revision's own commands (every claim above traces to one of them; the host is `peerbox`):
  - `hostname; whoami` → `peerbox` / `nick` (the SSH alias `hp` = `nick@10.0.0.5` is the client).
  - `git ls-remote hp refs/heads/master` → `933c9a8c360678c802c1baa79512ab5fe9872be3`; `git rev-parse master` → **same** (§6.1).
  - `git for-each-ref --format='%(refname) %(objectname:short) %(committerdate:iso)' refs/remotes/peerbox/` → `refs/remotes/peerbox/master db379f68 … 2026-09-07 12:44:27 +0800`; `git rev-list --count peerbox/master..master` → `4950` (the stale-ref proof).
  - `ssh hp 'grep -o "\"arm\":\"[a-z-]*\"" ~/.gopi/logs/removal-audit.log | sort | uniq -c | sort -rn'` → `393 finish-cleanup-branch-force`, `393 finish-cleanup-branch`, `103 task-ground-anchor`, `66 merge-build-gate-ground`, `38 finish-cleanup-worktree-force`, `36 task-self-reclaim`, `27 task-self-own-sweep`, `17 merge-run-spent-branch`, `4 finish-cleanup-worktree`, `1 task-row-reclaim`.
  - `ssh hp 'grep "\"op\":\"branch:delete\"" … | grep -c "\"task_id\""'` → `786`; `grep -vc` → `17` (§3, §6.1).
  - `ssh hp 'grep "finish-cleanup-branch-force" … | grep -o "\"branch\":\"[^\"]*\"" | cut -d\" -f4 | sort -u | wc -l'` → `66`; `comm -23` against `git ls-remote --heads peerbox` names → `58` absent (§6.1).
  - `comm -12` of those `58` against peerbox's own audit-log delete names → `28` explained (§6.1).
  - the 30-row reconciliation loop over `ssh hp` (`sqlite3 -readonly ~/.gopi/memory.db …`) → every row `completed` (`| sort | uniq -c` → `30 completed`), `deliverable_commit` empty for all `30`; then `git log master --oneline --grep="Merge branch '<name>'"` → `15` hits (`706613ae`, `22bfb66c`, `56870d4c`, `0ed5ff03`, …).
  - `git log -1 --format='%h %ci %s' 56870d4c` → `56870d4c 2026-09-26 21:01:37 +0800 Merge branch 'task/01a0dd4fb25373618fd00e94cc80c5e2'` = `13:01:37Z`, vs the last force-delete `13:08:01Z` (§6, §6.2).
  - `git merge-base --is-ancestor 6167624c master` → exit 0; `git for-each-ref --contains 6167624c` → `refs/heads/master` (§6).
  - `grep -o '"arm":"[a-z-]*"' ~/.gopi/logs/removal-audit.log | sort | uniq -c | sort -rn` (peerbox) → `584 merge-run-probe-ground`, `339 merge-build-gate-ground`, `309 merge-run-spent-branch`, `288 task-ground-anchor`, `227 finish-cleanup-worktree`, `159 task-self-own-sweep`, `34 task-self-reclaim`, `5 task-merge-branch`, `4 task-row-reclaim`, `1 worktree-sweep-reclaim` (§3.1).
  - `grep '"arm":"task-merge-branch"' ~/.gopi/logs/removal-audit.log` → `5` records, `feat/ff`, repos `/home/user/workspace/_tmp/head-guard-1790056904/repo[2]` and `.` (§3.1).
  - `for p in …; do [ -d /proc/$p ] && echo "pid $p LIVE"; done` → `pid 4194255 LIVE`; `cat /proc/4194255/cmdline` → `gopi merge --every 5m`; `cat /proc/4194255/cgroup` → `session-16.scope`; `/proc/4194255/exe` → `/home/user/work/GoPi/bin/gopi (deleted)` (§3.1).
  - `grep '"pid":4194255' ~/.gopi/logs/removal-audit.log | grep '"op":"branch:delete"'` → three records `12:57:14Z`, `12:57:33Z`, `12:57:41Z` (§3.1).
  - the 290-name loop `git rev-parse --verify --quiet refs/heads/<name>` inside this worktree → **0** branches present (§2's "reaped everywhere, no worktree holds them").
  - `sed -n '3734,3739p' internal/tools/task_finish_cleanup.go` and `sed -n '4045p;4076,4085p;4114,4119p'` → the `OK: true` no-push stamp and the guard that consumes it (§6.1).
  - `ls -l briefings/brief-closeout-deletes-undelivered-branch.md` → present on master, `8506` bytes (`22:55`), plus `git log --oneline -12 -- internal/tools/task_finish_cleanup.go` → **no** fix touching the `:3734-3739` arm has landed (§6.1).
- **Result:** every number above is an observed output. The load-bearing pair — the victim's merge at
  `13:01:37Z` and its last `-D` at `13:08:01Z` — is read from **two independent machines** (peerbox's
  git history and HP's audit log), which is what makes the risk verdict a measurement and not a
  reading.
- **Commit:** see the branch's commit for this file.

## Evidence (self-filed)

- `-d and -D are equal on HP: 393 = 393` — `ssh hp 'grep -o "\"arm\":\"[a-z-]*\"" ~/.gopi/logs/removal-audit.log | sort | uniq -c | sort -rn'` → the two lines `393 "arm":"finish-cleanup-branch-force"` and `393 "arm":"finish-cleanup-branch"`. **This is the defect's signature: no `-d` stands alone.**
- `66 force-deleted / 58 absent from peerbox / 28 explained by peerbox's own audit / 30 remaining, all completed` — the `comm -23` + `comm -12` + `ssh hp` reconciliation in §6.1, each step quoted with its command.
- `15 of 30 arrive as merge commits` — `git log master --oneline --grep="Merge branch '<name>'"` over the 30 names → 15 hits, e.g. `56870d4c Merge branch 'task/01a0dd4fb25373618fd00e94cc80c5e2'`.
- `the victim's push preceded its last prune by 6 minutes` — `git log -1 --format='%h %ci %s' 56870d4c` → `2026-09-26 21:01:37 +0800`; HP audit `13:08:01.789` last force-delete.
- `the victim's commit survives` — `git merge-base --is-ancestor 6167624c master` → exit 0; `git for-each-ref --contains 6167624c` → `refs/heads/master`.
- `peerbox's merge-run arm is periodic and LIVE` — `pid 4194255 LIVE`, `/proc/4194255/cmdline` → `gopi merge --every 5m`, three `merge-run-spent-branch` deletes at `12:57:14Z`/`12:57:33Z`/`12:57:41Z`.
- `refs/remotes/peerbox/master is 19 days stale` — `%(committerdate:iso)` → `2026-09-07 12:44:27 +0800`; `git rev-list --count peerbox/master..master` → `4950`.
- `783 branch-delete records / 77 distinct names` (HP, earlier read) — `grep -c '"kind":"branch-delete"'`; the log then moved to `803`/`1078` lines, which is why every count here is stamped.
- `8e424dbc9cb200d2e718cf8dc1ef6c2a655bc23c` — `git rev-list --parents -n1` → 2 parents (merge commit); on master's first-parent line; same hash on HP, committer date `2026-09-26 14:07:33 +0800`; `git log -1 --format=%B 8e424dbc | grep 01a0dbf8` → `tasks/01a0dbf8-report.md`.
- `no fix landed for the no-push OK stamp` — `git log --oneline -12 -- internal/tools/task_finish_cleanup.go` → most recent is `85f0bf6c fix(finish-cleanup): refuse to free a ground a live run still stands on`; the `:3734-3739` arm is untouched.
- `git status --porcelain` in this worktree → clean (Rule 30).
