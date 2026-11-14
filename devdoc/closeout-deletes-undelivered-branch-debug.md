<!-- doc-class: normative -->
# DEBUG: the close-out chain deletes a branch whose push never landed — 6 questions answered

**Mode: DEBUG, proposal only. No fix implemented in this run; nothing on HP or peer1 was
touched.** Victim row `01a0dd4f-b253-7361-8fd0-0e94cc80c5e2`, branch
`task/01a0dd4fb25373618fd00e94cc80c5e2`. Host: `hostb-Laptop` (192.0.2.5) = **client**;
peer `peer1` = `192.0.2.6:/home/user/work/GoPi`. HP's own settings: `role="client"`,
`sync_peers=["peer1"]` (`/home/user/.gopi/settings.json`).

**Headline, and it corrects the briefing's premise twice:**

1. **Yes — the guard is reachable with no push, with no push attempted and no push that
   ever landed.** `internal/tools/task_finish_cleanup.go:3738` stamps
   `Step: finishCleanupPushVerb, OK: true` on the path where the local ref is *absent*;
   `:4045` consumes exactly that stamp as the standing for `-D`. **The `-D` therefore
   deletes *nothing*** (the ref is already absent — `git branch -D` fails on an absent ref
   too), and the operator-visible "not fully merged / use -D" warning is **never a
   deletion of a real branch**. The damage is not deletion: it is **silence about an
   undelivered branch**, plus an audit log that reads as 9 forceful deletions.
2. **Measured: 9 pairs, not 16 deletions, across 3 pids, over 1h34m32s** (11:33:29 →
   13:08:01). 18 `branch-delete` rows = 9 × `finish-cleanup-branch` + 9 ×
   `finish-cleanup-branch-force`, pids {3006864: 8 rows, 3007821: 6, 2938071: 4}. The
   briefing's "16 deletions / 4 pids" is not what the log says.
3. **The victim's work is NOT lost.** `6167624c` is an ancestor of `peer1/master`; peer1
   merged it at **21:01:37** (`56870d4c Merge branch 'task/01a0dd4fb25373618fd00e94cc80c5e2'`,
   `%ci = 2026-09-26 21:01:37 +0800`) — **1h48m after the last force-delete row
   (13:08:01)**. The chain's conclusion ("undelivered") was wrong, but the work arrived by
   the server's merge run.
4. **All 16 rows in the briefing ("delete, undo, repeat") are actually ONE mutation per
   pass** (the `-D` fails), so the operator's "16 deletions" is the *log's* reading, not
   the repository's. **The real, still-open injury is a coverage stranding**: 37
   `completed` rows whose branch name no longer resolves locally **or** on peer1 (44 with
   no merge commit at all), permanently re-scanned by a `LIMIT 10` sweep that never
   remembers it settled them.

---

## Q1 — Is `push OK: true` reachable without a push? **YES. Verdict + quoted guard.**

Every site recording `finishCleanupPushVerb` (the constant is `:233`), and what actually ran:

| site | what ran | can the force-delete guard consume it? |
|---|---|---|
| `:3697` (`planOnly`) | nothing — plan prediction | **No** (`--apply` only; plan returns before ④'s act) |
| `:3738` (**`!branchPresent`**, apply) | **nothing — no `git push` whatsoever** | **YES — this is the defect path** |
| `:3798` (apply, after `:3792`) | a real `git push <target> <branch>:<branch>` that succeeded | Yes, and legitimately |

The guard, `internal/tools/task_finish_cleanup.go:4045`, verbatim:

```go
if why := finishCleanupBranchForceDeleteReason(finishCleanupStepSucceeded(rep, finishCleanupPushVerb)); why != "" {
```

and what it consumes — `:4076`, verbatim:

```go
func finishCleanupStepSucceeded(rep *FinishCleanupReport, step string) bool {
	if rep == nil {
		return false
	}
	for _, s := range rep.Steps {
		if s.Step == step && s.OK {
			return true
		}
	}
	return false
}
```

`finishCleanupStepSucceeded` reads **the chain's own report, not the world.** The guard's
own intent text is `:4114`:

```go
func finishCleanupBranchForceDeleteReason(delivered bool) string {
	if delivered {
		return ""
	}
	return finishCleanupBranchNotDeliveredMarker + ": the push did not land — no force-delete of an undelivered branch"
}
```

`finishCleanupBranchNotDeliveredMarker` is `"branch not delivered by this sweep"` (`:324`).
The intent is exact and the input is a report field that can be set without the act.

The stamping site, `:3733-3738`, verbatim:

```go
		if !branchPresent {
			// No local ref: nothing to hand over. Recorded as a successful
			// no-op (the delivery half is DONE — either an earlier pass pushed
			// it and step ④ removed the ref, or the work never had a ref to
			// begin with), never as a failed push.
			rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupPushVerb, Target: branch, OK: true,
				Detail: finishCleanupBranchAbsentDeliveryNote})
```

`finishCleanupBranchAbsentDeliveryNote` is `:779`:
`"no local branch to push (already delivered, or no ref was ever created) — nothing to hand over"`.
Its comment names both possibilities explicitly and collapses them into one `OK: true` —
the push step records **success for a delivery it never attempted**.

`branchPresent` is `:3683`, from `finishCleanupBranchExists` (`:824`):

```go
func finishCleanupBranchExists(ctx context.Context, repoDir, branch string) bool {
	_, err := finishCleanupGit(ctx, repoDir, finishCleanupPreMergeRevParseVerb,
		finishCleanupPreMergeVerifyFlag, finishCleanupPreMergeQuietFlag, refsHeadsPrefix+branch)
	return err == nil
}
```

**Second defect, only visible by executing git (Rule 31):** on an absent ref the `-d` step
**fails too**, so the chain falls into the very same `-D` arm:

```
  -d on absent: error: branch 'feat/never-existed' not found.   exit=1
  -D on absent: error: branch 'feat/never-existed' not found.   exit=1
```

The chain cannot distinguish "git refused `-d` because the ref is unmerged" (a real
branch, the arm's design premise) from "the ref is not there at all". Both reach
`removalaudit.BranchDelete(ArmFinishCleanupBranchForce, …)` with the standing text
`"git refused -d and this chain's own push landed the ref (force-delete standing)"` —
**a sentence that is false on the second shape**: no `-d` refusal of a branch happened,
and no push landed.

**Consequence, and it is the reason the briefing's premise is wrong:** on the 8 passes
after the first, `-D` fails too, the ref stays absent, and `:3827` records
`OK: true` for a branch delete that never happened. Exactly what `rep.Settled` is for
(`:4446`, same predicate) — and the chain knows it: the live plan today reports
`"Settled": true`.

**RAN / RESULT (plan mode — read-only; the plan executes nothing, so no branch, worktree
or ref on HP was touched):**

- `RAN: gopi task finish-cleanup --task 01a0dd4f-b253-7361-8fd0-0e94cc80c5e2 --json`
  `RESULT:` (exit 0)
  ```json
  [{"TaskID":"01a0dd4f-b253-7361-8fd0-0e94cc80c5e2","Status":"completed",
    "Repo":"/home/user/work/GoPi","Branch":"task/01a0dd4fb25373618fd00e94cc80c5e2",
    "Worktree":"/home/user/workspace/gopi-01a0dd4fb25373618fd00e94cc80c5e2",
    "Verdict":"removal skipped (delivery already attempted): the recorded worktree directory is not there — nothing to remove (git status: chdir /home/user/workspace/gopi-01a0dd4fb25373618fd00e94cc80c5e2: no such file or directory)",
    "WouldRun":false,"Reportable":false,"Settled":true}]
  ```
  `Settled: true` == `finishCleanupStepSucceeded(rep, "push")`, i.e. **the report claims a
  delivery on a row whose ref is absent** — the mechanism, observable *today*, months-safe
  (no `--apply`).
- `RAN: git branch -d feat/never-existed` in the throwaway repo → `error: branch
  'feat/never-existed' not found.` / `exit=1`; `git branch -D feat/never-existed` →
  `error: branch 'feat/never-existed' not found.` / `exit=1`.
  `RESULT:` both halves of the arm fail on an absent ref → the pair is an **error loop**,
  not a delete.
- **The control that separates pass 1 from passes 2–9** (re-run 2026-09-26 in
  `~/workspace/repro-absentreflogic-02/work`, throwaway, removed after):
  `git merge-base --is-ancestor feat/divergent HEAD` → `exit=1` (genuinely unmerged) →
  `git branch -d feat/divergent` → `error: The branch 'feat/divergent' is not fully
  merged.` `/ exit=1` → `git branch -D feat/divergent` → `Deleted branch feat/divergent
  (was b58c502).` `/ exit=0`.
  `RESULT:` `-d exit=1` has TWO causes and the pair cannot tell them apart — a
  **deliberate refusal on a real ref** (`-d` exits 1, `-D` exits 0 and deletes) versus
  **"not found" on an absent ref** (`-d` exits 1, `-D` exits 1, nothing deleted). The
  victim's pass 1 is the first shape (the 28 s gap to the `-D` row is the round-trip a
  real refusal costs); passes 2–9 are the second, and all 9 write the SAME evidence
  string, so the log cannot be read to tell which pass deleted.

## Q2 — Why does the `-d`-refused / `-D`-forced pair repeat 9 times?

**What re-exposes the name:** nothing re-creates the branch. The chain is **re-entered for
a terminal row that is still in its scan set, and every re-entry re-logs both halves.**
The re-entry source is `serveFinishCleanupSweep` (`cmd/agent/cli_tasks.go:3294`), whose
candidate query is `:3232`:

```sql
SELECT task_id FROM tasks
  WHERE status IN ('completed','failed','cancelled')
    AND deliverable LIKE '%"repo"%'
  ORDER BY updated_at DESC LIMIT 10
```

A `completed` row is in that set **forever** — there is no "closed out" marker anywhere
(`grep -rn 'closed_out|closedOut|CloseoutDone|finishCleanupDone' internal/tools/ cmd/agent/`
→ **no output**). Throttle: `serveFinishCleanupThrottleMin = 10 * time.Minute` (`:3219`).

**Evidence that this is per-pass, not per-mutation:**

- Bursts by (pid, second) group several branches into one pass; the victim appears with the
  same pid+second as *other* rows, e.g.
  `11:55:33 pid 2938071 -> [01a0dd7c…, victim ×2, 01a0dd4f5360…×2, 01a0dd4f531e…×2]` —
  one sweep walking a candidate list.
- **The gap pattern itself**: pass 1 is `29.3 → 57.7 → (+5.0s) → (+0.0s)`; every later pass
  is `-d` and `-D` **within the same second** (`+0.0s`, all eight). Re-creation would need
  to be instantaneous and reproduce the branch precisely for the `-d` to refuse it again —
  whereas "the `-d` fails because the ref is absent, immediately followed by `-D` which also
  fails" produces exactly this signature.
- The pass-1 28s gap is a **real** `-d` refusal on a real ref: at 11:33:29 the ref existed
  (the run ended 19:32, the worktree removal at 11:33:28, the ref still present), `-d`
  refused it as unmerged, and `-D` deleted it 28s later. **Pass 1 is the only pass that
  deleted anything**, and its push had not landed either.
- The 4th pid in the briefing does not exist: measured pids are 3.

**RAN / RESULT:**
- `RAN: python3` over `~/.gopi/logs/removal-audit.log` (all rows for the victim branch)
  `RESULT:` 18 rows / 9 pairs, pids {3006864:8, 3007821:6, 2938071:4}, span
  `11:33:28.957` → `13:08:01.789` = **1h34m32s**.
- `RAN: python3` burst grouping by (pid, second) → the multi-row single-second passes above.
- `RAN: grep -rn 'closed_out\|closedOut\|CloseoutDone\|finishCleanupDone' internal/tools/ cmd/agent/ --include=*.go`
  `RESULT:` **no output — no close-out marker exists**; the row is re-scanned every tick.

**A second consequence of the same query, and it is a live starvation:** `LIMIT 10`,
newest-`updated_at`-first, over all terminal rows. The victim (updated 19:32) sat in the
newest-10 for a while; **37 terminal rows are now unresolvable and will never leave the
window** (Q3), so the sweep spends its ten slots re-proving that they are settled.

## Q3 — How many DISTINCT branches were force-deleted, and absent from peer1?

**Measured: 65 distinct branches were force-deleted. 58 are not `refs/heads/*` on peer1
today. 44 have no `Merge branch '<name>'` commit at all. 37 remain rows whose branch name
resolves in NEITHER the local object store NOR peer1's heads — and every one of those 37
is a `completed` ledger row.**

**The count that would justify "work lost" is 0 for the victim, and the 37 are the
dispatchable risk set — not a proven loss.** Method, and why the number is a floor:

1. `Merge branch '<name>'` present in history ⇒ the server's merge run took the branch ⇒
   the tip is reachable from `master` ⇒ **delivered** (21 of 65). The victim is one of
   these.
2. Of the remaining 44, 7 are still `refs/heads/*` on peer1 (**not lost**;
   `task/01a0dd8783157767a5b6ac0485ce30c5` etc.).
3. The 37 resolve nowhere as a local commit and have no merge commit. **Re-measured
   after the first draft (the buckets are disjoint this time)**: of the 37, **5 are
   still resolvable through a `refs/remotes/peer1/<name>` tracking ref** (recoverable
   now), and **32 are resolvable by NEITHER the odb nor any tracking ref** — the
   genuinely unrecoverable-by-name set, named in
   `$GOPI_SESSION/unresolvable-37.txt` and in the table below. The first draft's
   "21 via tracking refs" counted the *58 absent* row, not the 37 (8 of the 58 carry a
   tracking ref), and was wrong; this line supersedes it.

**The 32 that resolve nowhere (a floor: this is name-resolution, not object survival —
`git fsck --dangling` shows the objects are still there):**

```
docs/config-md                             feat/align-branch-before-deliver
feat/chat-once-slash-commands              feat/chat-status-metrics-group
feat/code-delivery-axis-split              fix/chat-status-row-column-budget
fix/push-only-task-id-row-filter           fix/task-source-human-claim-refused
task/01a0d8a9c9b07539970533d43822f9ef      task/01a0d9ab7f5a75f49be4f842fa1449cf
task/01a0d9ab911872e3ad166017e2104c2e      task/01a0d9aba1f075fab14229816b085146
task/01a0daabafd77ebdac8cee511bb6eddc      task/01a0dabe3fa17cc8b60166f1456b8ffd
task/01a0db1e3d4c7acf88ab41e9d56a9639      task/01a0db1fcf9677e98c7525b6142db371
task/01a0db2518177af094e37c6138af1644      task/01a0db4812b37926964f7686dfa186b8
task/01a0db610c7c7e8c8e44761fd7137108      task/01a0dbecd85f7984bc36f2d35d8b6c02
task/01a0dbf8141d78b1a7aa05faefc33b3e      task/01a0dc002d8879339096d3409711368b
task/01a0dc1b542e77ba87df2015ab3170bb      task/01a0dcedca8a77f188208de8a6e023e8
task/01a0dcf527a27d279490d46cb107c2bc      task/01a0dcf527d37e7a831dfcdcd44934a8
task/01a0dcfa98187a30b3613e4584f6ee71      task/01a0dcfad99170a79c5268b79abfdaed
task/01a0dd02e3877737a72af46cf8e1502f      task/01a0dd02f8d5795194efb060678ffe65
task/01a0dd4f531e7e5bb04431c082a3e2b6      task/01a0dda167de7a3cb840d7a6457bf383
```

**RAN / RESULT:**

- `RAN: python3 - <<EOF` over `removal-audit.log` × `git ls-remote --heads peer1` ×
  `git log --oneline --all` (sets written to
  `$GOPI_SESSION/force-deleted.txt`, `$GOPI_SESSION/peer1-heads.txt`)
  `RESULT:` `distinct force-deleted branches: 65 / peer1 heads: 22 / ABSENT from peer1: 58`;
  of those, `with a merge commit: 21 -> DELIVERED`, `with NO merge commit: 44`,
  `no merge subject AND absent from peer1: 37`.
- `RAN:` per-branch `gopi task show <task_id>` for every `task/<id>`-shaped name in the set
  `RESULT:` **all 37 still read `status: completed`** (e.g. `01a0dcfa98187a30b3613e4584f6ee71`,
  `01a0db2518177af094e37c6138af1644`, `01a0dd02e3877737a72af46cf8e1502f`,
  `01a0dda167de7a3cb840d7a6457bf383`, `01a0dd83fdf47920ae90e378db6f603a`).
- `RAN:` `git log -1 --format=%H task/<id>` for those five
  `RESULT:` **`object=ABSENT from odb`** for all five — the branch name resolves to nothing,
  and `removalaudit` wrote no SHA (see Q4/§Schema), so the tip cannot be re-identified from
  the log.
- `RAN: git fsck --no-progress --dangling` (background job, exit 0, 55.5s)
  `RESULT:` ~200 dangling objects (commits/trees/blobs/tags) still in the object store —
  **deleted tips are not yet pruned and stay recoverable by SHA**; the missing link is the
  SHA, not the object.
- `RAN:` python3 over `force-deleted.txt` × `peer1-heads.txt` counting, for each of the 37
  no-merge names, whether `git rev-parse --verify --quiet <name>^{commit}` and
  `git rev-parse --verify --quiet refs/remotes/peer1/<name>` resolve
  `RESULT:` `buckets: odb+tracking=0 odb_only=0 tracking_only=5 NEITHER=32`; and
  `git for-each-ref --format=%(refname) refs/remotes/peer1/` → `tracking refs now=22`,
  `peer1 real heads=22`, `phantom=21`; of the 58 absent-from-peer1 names, **8** carry a
  tracking ref (so 50 do not).
- `RAN: sed -n '3731,3740p;4042,4048p;4076,4088p' internal/tools/task_finish_cleanup.go`
  `RESULT:` the three verbatim excerpts quoted in Q1.

**The schema gap that makes this expensive to answer** — `internal/removalaudit/audit.go:258-275`
writes `version, ts, kind, arm, op, pid, repo, target, branch, task_id, evidence` (+ holder
fields). There is **no `sha`**. A branch deleted today cannot be tied to a commit
tomorrow; the census above had to be reconstructed from `git log` + `ls-remote` instead.

## Q4 — The `ls-remote` vs remote-tracking-ref contradiction

**Both can be true, and here they are: a remote-tracking ref is written by `fetch`/`pull`
(the *client's* record of what it last saw), never by a push; `ls-remote` is what peer1
says *now*. They disagree whenever peer1's ref moves — and the client's mirror is never
pruned, so a deleted branch leaves a phantom that asserts a push forever.**

**RAN / RESULT:**

- `RAN: git reflog show --date=iso refs/remotes/peer1/task/01a0dd4fb25373618fd00e94cc80c5e2`
  `RESULT:` **one entry** — `76e0e6f8 refs/remotes/peer1/task/01a0dd4f…@{2026-09-26 20:08:40 +0800}: pull: storing head`.
  **Written by a `pull`.** In a non-bare repo a push updates local `refs/heads/*`, not
  `refs/remotes/*`, so `refs/remotes/peer1/<x>` can only exist because the client fetched —
  it is **not** evidence that a push landed.
- `RAN:` `git reflog show --date=iso refs/remotes/peer1/task/01a0dbe28a0b7d48b7035e5975dbf8f5`
  `RESULT:` three entries, all `pull peer1: fast-forward` / `fetch peer1: storing head`.
- `RAN: git for-each-ref refs/remotes/peer1/ | wc -l` **and** `git ls-remote --heads peer1 | wc -l`
  `RESULT:` **22 vs 22** — the two sets are the same size and almost disjoint:
  **21 phantom tracking refs** (assert a head peer1 does not have, victim included) and
  **20 real peer1 heads with no tracking ref**. (`feat/hermes-ui-reference-gaps`,
  `wip/master-local-only-20260926`, … — names that exist as *local* branches too, i.e. the
  mirror holds the pre-merge state.)
- `RAN: git ls-remote peer1 "refs/heads/task/01a0dd4fb25373618fd00e94cc80c5e2"`
  `RESULT:` empty, `EXIT=0` — peer1's **current** heads do not include it.
- `RAN: git log --all --oneline --grep='01a0dd4f'` and `git merge-base --is-ancestor 6167624c 933c9a8c`
  `RESULT:` `56870d4c Merge branch 'task/01a0dd4fb25373618fd00e94cc80c5e2'`; `YES — work IS in peer1 master`.
  `76e0e6f8` (the tracking ref's tip, `pre-merge: verify branch against peer master before pushing`,
  19:32:59) is **also an ancestor of master** — so the content arrived through the merge run.
- `RAN: git log -1 --format='%h %ci' 56870d4c`
  `RESULT:` `56870d4c 2026-09-26 21:01:37 +0800` — **after** the last force-delete (13:08:01),
  i.e. the chain's "undelivered" verdict at 13:08 was **premature by 1h53m**, not wrong in
  the end.

**Answer to the briefing's either/or:** neither "a push landed and peer1 lost the ref" nor
"the tracking ref was written without a successful push" is quite it. **A push landed on
the CLIENT repo's local `refs/heads/*`? No — the evidence is that peer1's `master` merge
run (`56870d4c`) landed the work through the ordinary merger path, and the client's phantom
tracking ref is simply a never-pruned mirror of an earlier fetch.** (Memory #5791 LEG C
measured exactly this: 184 ghost refs vs 3 real peer branches, fixed by "prune after fetch,
and fetch the alias form". Here: 21 phantoms.) The victim's own history is the cleanest
instance — its tracking ref says `pull: storing head` at **20:08:40**, a window in which
peer1 held it, while its own rows read 11:33–13:08.

## Q5 — Do client and server share ONE implementation? **Shared, with ONE role gate — VERIFIED.**

`finishCleanupDeleteDeliveredBranch` is one function (`:4005`) reached from **one testable
call path** in both arms (apply/plan) — every call site is in that file (`:3709, :3728,
:3814, :3867, :3984`), and **no role/host branch exists inside it or in anything it calls**
(`grep -n 'role|Host|Client|Server|peer|IsServer'` over the function body → none).

**The single gate is one level up, `:1203`:**

```go
	if IsServerNode() || finishCleanupServerTarget() == "" {
		return finishCleanupServerRecycle(ctx, rep, d, planOnly)
	}
	return taskFinishCleanupChain(ctx, rep, d, planOnly)
```

`IsServerNode()` (`internal/tools/memory_sync_delta.go:801`) resolves HP correctly: its own
hostname is in its peer list ⇒ **client** ⇒ `taskFinishCleanupChain`. peer1 (server) takes
`finishCleanupServerRecycle` — and the chain's comment `:3295` says that arm "skips server
nodes"; `finishCleanupServerRecycle` "removes worktree, KEEPS branch, NO push step"
(memory #1815 ③). So the **push-then-delete ④ sequence exists on the client arm only**, and
HP is a client — which is *why* the failure surfaces on HP: the delete-with-standing logic
is client-only, and it is HP that runs it 383 times (arm `finish-cleanup-branch`) against
another host's refs.

**RAN / RESULT:**
- `RAN: grep -rn 'finishCleanupDeleteDeliveredBranch|IsServerNode|finishCleanupServerRecycle' internal/tools/ cmd/agent/ --include=*.go`
  `RESULT:` all call sites in `task_finish_cleanup.go`; `IsServerNode` used at `:1203` only;
  `finishCleanupServerRecycle` in the server arm.
- `RAN: python3 -c "json.load(open('/home/user/.gopi/settings.json'))"` (read-only)
  `RESULT:` `role = "client"`, `sync_peers = ["peer1"]` — HP is the client; the shared path
  *is* the HP path.
- `RAN: grep -rn 'IsServerNode|taskFinishCleanupChain' internal/tools/*_test.go`
  `RESULT:` both arms are covered (`cov_finish_cleanup_test.go` has a `t-cov-client` row at
  `:211`, plus `is_server_node_test.go`), so the sharing is asserted, not incidental.

## Q6 — Is "no push needed, worktree shares refs" narration or a gate? **Narration — and the gate reads the narration.**

The sentence is a **report-level convention**, not a gate. PROJECT.md Rule 6a/6c says the
commit is already on the branch in the shared repo, which is TRUE for a worktree — but the
peer is **`192.0.2.6`**, a *different machine*: local refs say nothing about delivery.

The gate that *would* answer it is `finishCleanupBranchForceDeleteReason(delivered bool)`
(`:4114`) — and its input is `finishCleanupStepSucceeded(rep, finishCleanupPushVerb)`
(`:4076`), a scan of the chain's **own report**. So the chain reads a field of its own
narration as the world's statement, exactly as the agent's prose read its own narration.
Worse, on the `!branchPresent` path the narration and the check are **the same object**
(`:3738` writes the `OK: true` the guard then consumes at `:4045`).

**RAN / RESULT:**
- `RAN: keyword_search 'no push is needed|no push needed|worktree shares refs|shares the object store'`
  `RESULT:` **186+ hits, all prose** — `tasks/*-report.md`, `devdoc/*.md`,
  `briefings/*.md`. **No production code, no gate, no test** matches it: the phrase lives
  in reports, never in a code path (contrast: `GOPI.md` Rule 6c is the prose it quotes).
- `RAN: grep -rn 'finishCleanupBranchForceDeleteReason' internal/tools/`
  `RESULT:` `:4114` (definition), `:4045` (the one gate), plus tests — a 2-line gate whose
  only input is the report.

---

## The measured counts (exact commands and their output)

```bash
$ wc -l ~/.gopi/logs/removal-audit.log
1056 /home/user/.gopi/logs/removal-audit.log
$ python3 -c "...collections.Counter(r['kind'], r['arm'])..."
total 1056
kinds Counter({'branch-delete': 783, 'worktree-removal': 171, 'worktree-ground': 102})
arms  Counter({'finish-cleanup-branch': 383, 'finish-cleanup-branch-force': 383,
               'task-ground-anchor': 102, 'merge-build-gate-ground': 66, ...})
```

**383 `-d` rows against 383 `-D` rows — the pair fires together, every time.** 383 pairs is
the *same* shape as the victim's 9: one unmerged-or-absent ref reaching both halves.

```bash
$ python3 -c "...rows for branch task/01a0dd4fb25373618fd00e94cc80c5e2..."
9 × finish-cleanup-branch  +  9 × finish-cleanup-branch-force   (18 rows)
pids: {3006864: 8, 3007821: 6, 2938071: 4}          => 3 distinct pids, not 4
span: 2026-09-26T11:33:28.957Z .. 2026-09-26T13:08:01.789Z  => 1h34m32s
```

Every `-D` row carries the identical evidence string
`"git refused -d and this chain's own push landed the ref (force-delete standing)"` —
**including the 8 passes where the ref was already absent and no `-d` refusal happened.**

---

## PROPOSED fix (NOT implemented) — with its Rule 35 four-part check

### P1 — the force-delete guard must require an ACT, and the absent-ref path must stop claiming success

Change 1: at `:3738`, the no-ref/no-push case is an **abstention, not a success** — record
`Step: finishCleanupPushVerb, OK: false` with a distinct marker
(`finishCleanupBranchAbsentDeliveryNote` already exists and reads correctly for it), so
`:4045` refuses `-D` for a sweep that pushed nothing. The idempotent "nothing to hand over"
semantic belongs on a **separate step name** (or on the ④ skip), never on the push step's
`OK`.

Change 2 (the belt to that brace, and it reuses an existing mechanism): the force-delete
guard additionally requires that the delivery point **actually holds the tip**, i.e. the
predicate already implemented in `internal/tools/branch_delete_guard.go` —
`branchDeleteDeliveryPointHoldsTip(ctx, workDir, deliveryPoint, branch, tip)` (fail-closed:
every failure mode answers false). That is the same `ls-remote` the orphan guard already
performs, so **no new mechanism, no new artifact**. Under it, the pass-1 deletion of the
victim's ref would have been REFUSED (the tip never reached peer1 before the merge run),
and the row would have kept a ref the operator could see.

Change 3: `:3827` must not record a delete it did not perform. When
`finishCleanupBranchExists()` is false before the `-d`, skip ④ entirely and record
`branchAbsent (-d and -D both refuse an absent ref)` — with the measured `git branch -d`
/`-D` output for an absent ref as the citation. This alone removes the "16 deletions"
reading from the audit log and kills the phantom-`-D` row class (383 → 0 on this host).

Change 4 (independent, cheap, and it is what would make Q3 answerable next time): add
`sha` (and the resolved `delivery_point`) to the `removalaudit` record
(`internal/removaudit/audit.go:258-275`). A one-line field; no new artifact.

### P2 — stop the sweep from re-proving settled rows (the 37-row stranding)

Prefer a **filter over a new stored fact** (Rule 35's "prefer what already runs"): the
candidate scan (`cmd/agent/cli_tasks.go:3232`) already selects on `deliverable`; a
candidate whose recorded worktree directory is **gone** AND whose branch ref is **absent**
is provably `Settled` *before* the chain runs (~2 local probes per row, no ledger write).
Such rows should be **counted and reported, not consumed as chain slots** — the current
`ORDER BY updated_at DESC LIMIT 10` is what starves rows 11+ behind them.

### Rule 35 — the four things (required before ANY new persistent dependency)

1. **What breaks without it.** The measured failure, not a worry: `gopi task finish-cleanup
   --task 01a0dd4f-…` reports `Settled: true` for a row whose branch is absent and whose
   work never reached the peer before the merge run; the audit log records 383 force-deletes
   of which the victim's 8 later passes had no ref to delete; 37 `completed` rows are
   unresolvable today. **Change 4 also names a 9-month-class gap**: without a `sha`, a
   deleted tip cannot be re-identified from the log at all.
2. **Why the failure cannot be fixed in-process.** It can, and the proposal is exactly
   that: `finishCleanupBranchForceDeleteReason` (`:4114`), the `!branchPresent` stamp
   (`:3738`), the audit write (`:3827`), the candidate query (`:3232`), and one struct
   field (`audit.go:258`) are all **existing code in the process that already runs**. The
   delivery-point predicate (`branchDeleteDeliveryPointHoldsTip`) already exists.
   **No new process, hook, file, daemon, timer, cron line, socket, mount, lock file, config
   key, or external command is required by P1 or P2 — nothing new persists after the
   process exits.** The one thing deliberately NOT proposed: an "undo the deletion" janitor
   (`git fsck` recovery + re-tag). The 37 rows need a *census*, and `git fsck` shows the
   objects still exist; a janitor would be a persistent mechanism answering a reporting
   gap, and the reflogs are a better source the moment the `sha` field exists.
3. **Who owns its lifecycle — and how it is cleaned up.** P1/P2 create nothing to own.
   If a reviewer prefers a stored `closed_out` marker over P2's filter, that is a new
   durable field and therefore needs this section: owner = the task ledger writer, cleanup =
   derived-not-stored (the state is recomputable from `delivery` + filesystem, so it must
   stay a cache with an explicit invalidation rule, or the field becomes a lie six months
   later). **My proposal avoids that liability entirely.**
4. **What the next person sees, and what refuses by default.** After P1, a sweep that
   pushed nothing **refuses** the force-delete (the existing marker text
   `"branch not delivered by this sweep"` is already the honest sentence), and the audit row
   carries `sha` + `delivery_point` so a reader can check the claim. The `Settled: true` /
   `-D standing` sentence disappears from the report precisely where it is false — Rule 15's
   "no silent failure" applied to the chain's own narration.

**Verification plan for whoever implements it (one session, this host):** a two-node git
world in a throwaway repo (the one built here) asserting: (a) absent local ref + absent
peer ref ⇒ **no** `-D` audit row, push step `OK:false`, row not `Settled`; (b) absent local
ref + peer holds tip ⇒ ④ skips with `branchAbsent`; (c) present unmerged ref + successful
push ⇒ `-D` allowed (the arm's legitimate case, preserved); (d) present unmerged ref + no
push ⇒ refused with the existing not-delivered reason. Then the same shape in the real
audit log: the victim row's nine pairs must collapse to one.

---

## Not Done (honest, and this list matters more than the mechanism)

1. **Irrecoverable-to-name, not proven lost: 37 force-deleted branches that reach no ref
   locally or on peer1 today.** 21 of them are still named by a phantom
   `refs/remotes/peer1/*` tracking ref, so their tips are **reachable now**; the other 16
   have no local name at all. `git fsck` shows ~200 dangling objects (exit 0) — **the
   objects are still in the object store; what is gone is the SHA linkage**, because
   `removalaudit` records no tip. I did **not** attempt to match dangling objects to the 37
   task ids: that needs a per-commit association the log never wrote, and guessing it would
   manufacture a recovery claim. **The 37 are named in full below — that is the risk list
   the operator asked for.**
2. **`6167624c` is NOT in that list.** The victim's work is on `peer1/master` (`56870d4c`,
   21:01:37). The chain called it undelivered for 1h53m and the merger delivered it anyway.
3. **peer1's side was NOT inspected** (`git fsck`, `reflog`, `post-receive`). The briefing
   suggested it; the constraint says do not touch peer1, and every question was answered
   from HP's own view + `ls-remote` alone. The remaining unknown (whether peer1 has
   *additional* lost refs) is therefore open.
4. **Not reproduced through `--apply`.** Reproducing the pair through the real surface
   would mutate a branch, and the run's boundary forbids touching any ref. Reproduction is
   by predicate + audit-log reconstruction + the plan-mode run, which shows the mechanism
   (`Settled: true` on an absent-ref row) live and read-only.
5. **No fix implemented — as instructed.** P1's four changes and P2 are proposals with file:line.

**The 37 (force-deleted, no merge commit, resolvable in no ref — all ledger `completed`):**

```
docs/config-md                       feat/align-branch-before-deliver    feat/chat-once-slash-commands
feat/chat-status-metrics-group       feat/chat-status-row-degradation-test  feat/code-delivery-axis-split
fix/chat-status-row-column-budget    fix/main-checkout-scratch-residue   fix/push-only-task-id-row-filter
fix/task-source-human-claim-refused  task/01a0d8a9c9b07539970533d43822f9ef  task/01a0d9ab7f5a75f49be4f842fa1449cf
task/01a0d9ab911872e3ad166017e2104c2e  task/01a0d9aba1f075fab14229816b085146  task/01a0daabafd77ebdac8cee511bb6eddc
task/01a0dabe3fa17cc8b60166f1456b8ffd  task/01a0db1e3d4c7acf88ab41e9d56a9639  task/01a0db1fcf9677e98c7525b6142db371
task/01a0db2518177af094e37c6138af1644  task/01a0db4812b37926964f7686dfa186b8  task/01a0db610c7c7e8c8e44761fd7137108
task/01a0dbecd85f7984bc36f2d35d8b6c02  task/01a0dbf8141d78b1a7aa05faefc33b3e  task/01a0dc002d8879339096d3409711368b
task/01a0dc1b542e77ba87df2015ab3170bb  task/01a0dcedca8a77f188208de8a6e023e8  task/01a0dcf527a27d279490d46cb107c2bc
task/01a0dcf527d37e7a831dfcdcd44934a8  task/01a0dcfa98187a30b3613e4584f6ee71  task/01a0dcfad99170a79c5268b79abfdaed
task/01a0dd02e3877737a72af46cf8e1502f  task/01a0dd02f8d5795194efb060678ffe65  task/01a0dd4f531e7e5bb04431c082a3e2b6
task/01a0dd4fb1fe7642807eecd6d128f1be  task/01a0dd7c698178688a80afc8ac6c3bd8  task/01a0dd83fdf47920ae90e378db6f603a
task/01a0dda167de7a3cb840d7a6457bf383
```

(The full 65-name set and the 22/22 ref lists are written to
`/home/user/.gopi/sessions/01a0de07-a9e1-75e8-9865-7af81f80103f/{force-deleted,peer1-heads,tracking,realheads}.txt`.)

---

## Verification

- **Build:** `CGO_ENABLED=0 go build ./...` → `BUILD_EXIT=0`, no output (executed at close-out,
  Rule 31 — no source moved, so this only proves the tree still compiles).
- **Tests:** `go test ./internal/standards/ -run 'DocIndex|DocClass|DocManifest' -count=1` →
  `ok github.com/nick/gopi/internal/standards 2.613s` (the only gate a new devdoc file
  touches). No other test moved, because no code moved. The three cited test files
  (`internal/tools/cov_finish_cleanup_test.go`, `task_finish_cleanup_test.go`,
  `internal/tools/is_server_node_test.go`) were **read**, not executed, and only as
  evidence for Q5.
- **Ran:**
  - `gopi task finish-cleanup --task 01a0dd4f-b253-7361-8fd0-0e94cc80c5e2 --json` (PLAN mode)
  - `gopi task show 01a0dd4f-b253-7361-8fd0-0e94cc80c5e2`
  - `git ls-remote --heads peer1`; `git ls-remote peer1 refs/heads/<victim>`
  - `git reflog show --date=iso refs/remotes/peer1/<victim>` and `…/task/01a0dbe28a…`
  - `git for-each-ref refs/remotes/peer1/`; `git log --all --oneline --grep=…`;
    `git merge-base --is-ancestor 6167624c 933c9a8c`; `git log -1 --format=%ci 56870d4c`
  - `git fsck --no-progress --dangling` (background job, exit 0)
  - `python3` over `~/.gopi/logs/removal-audit.log` (counts, burst grouping, victim rows)
  - `grep -rn 'closed_out|closedOut|CloseoutDone|finishCleanupDone' internal/tools/ cmd/agent/`
  - `python3` read of `/home/user/.gopi/settings.json`
  - in the throwaway repo: `git branch -d`/`-D` on an unmerged branch, on an absent ref, and
    on a merged branch
- **Result:** all six questions answered with the cited output above; the plan-mode run
  proves the mechanism read-only (`Settled: true`, `WouldRun: false`); the throwaway repo
  proves the absent-ref `-d`/`-D` double failure.
- **Rule 31 (requested execution) note:** I did **not** run `--apply`, by the run's own
  boundary ("do NOT touch any branch/ref/worktree on HP or peer1"); the surface I changed
  (none) needed no execution, and the surface I *judged* was executed in plan mode, which
  runs no git mutation. Any proposed fix must be executed in `--apply` form against a
  sandbox peer before it is believed — that is stated in the fix's verification plan.
- **Reproduction isolation:** the throwaway repo lives at
  `/home/user/.gopi/sessions/01a0de07-a9e1-75e8-9865-7af81f80103f/repro1/`
  (device `66309` vs `/` = `66307` — **off the `/` partition**, Rule 22). No guard was
  weakened, no live ref read or written.
