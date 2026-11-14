<!-- doc-class: normative -->
# Branch ↔ Task Linkage: what exists, what it resolves, and why the gaps are where they are

**Baseline**: `master` @ `69975030` ("Merge branch 'feat/merger-status-next-tick'"), read in the
linked worktree `~/workspace/gopi-branch-linkage` (branch `docs/branch-task-linkage`).
This is the evidence report for epic `01a0a964` item **[1/5]** (`01a0a9b1-6b3e-7c5c-9b4c-f171b735a3f8`).

**Method**: every claim below is a reading of the source at a cited `file:line`, or the output of a
command run against the live ledger. Nothing is inferred from the briefing. Where the briefing's own
premises do not survive contact with the code, §5 says so explicitly — the report's job is the truth
about the linkage, not agreement with the briefing that asked for it.

**Ledger**: `~/.gopi/memory.db` (table `tasks`). `~/.gopi/gopi.db` and `~/.gopi/tasks.db` are
**0-byte files** — a reader who guesses those paths gets `no such table: tasks` and concludes the
ledger is empty. The tasks table lives in the memory DB
(`openTasksDB(ctx, memoryDBPath())`, e.g. `internal/tools/task_deliverable_branch.go:472`).

---

## 1. (a) The completion-time backfill — the only write that pairs a branch to a terminal row

`taskBackfillDeliverableBranch` (`internal/tools/task_deliverable_branch.go:465`) merges a branch
into a row's deliverable JSON. Its contract is stated in its own doc block
(`:340-346`): merge `{"repo":...}` → `{"repo":...,"branch":...}`, **only** when the row is
completed, still carries a repo, has **no branch yet**, and the branch is non-empty — "never
overwrites a branch another writer stamped, never invents one".

The gate is `:363`:

```go
if t == nil || t.Status != taskStatusCompleted {
    return nil // row gone or not completed — nothing to backfill
}
```

The write is a CAS-by-value single `UPDATE` keyed on the read-back raw blob (`:375-377`), so a
concurrent writer cannot be clobbered.

**It has exactly one call site**, `internal/tools/tasks.go:5405`, behind its own outer gate at
`:5403`, inside the completion writeback path:

```go
// bugfix epic 2026-09-02 (Option A): only a COMPLETED run pairs a branch
// — a failed run's branch holds no adoptable delivery. ...
if status == taskStatusCompleted {
    if branch := taskWritebackBranch(); branch != "" {
        if berr := taskBackfillDeliverableBranch(taskID, branch); berr != nil {
            taskNotifyLog("task %s: deliverable.branch backfill skipped: %v", taskID, berr)
        }
    }
}
```

So the **completion-time** pairing is single-sited, completed-only, and needs a branch recovered
from the child session's recorded workdir (`taskWritebackBranch()`). Two consequences follow
directly:

- A row that never reaches `completed` — pending, in-flight, or `failed`/`cancelled` — never gets a
  branch **from this path**. A failed run is deliberately excluded: "a failed run's branch holds no
  adoptable delivery" (`tasks.go:5397-5399`).
- A row whose deliverable has no `repo` (auto mirror rows, `:367-369`) is skipped entirely.

### 1.1 The half the briefing misses: a **creation-time** writer already stamps branch + worktree

The briefing's premise — "a claimed/in_progress row's deliverable is the repo-only placeholder and
names no branch" — is **not true as stated**. There is a second writer, on the *creation* axis, and
it is status-agnostic on purpose:

- writer: `taskBackfillDeliverableWorktree` (`internal/tools/task_deliverable_worktree.go:509`),
  whose doc block (`:470-477`) says so: *"Any status: the creation-time stamp must land on a row that
  is still pending/claimed/in_progress."*
- entry seam: `TaskDeliverableWorktreeBashForRow` (`:208`), reached from the agent's bash tool
  dispatch at `internal/agent/loop.go:6092`:
  ```go
  tools.TaskDeliverableWorktreeBashForRow(a.autoTaskRowID, workdir, command)
  ```
- it fires only on a bash round that *can* have moved the executor into a worktree — a cheap needle
  pre-filter over `git` / `worktree` / `checkout` / `branch` / `cd ` / `push`
  (`task_deliverable_worktree.go:250-257`), then probes workdir candidates and stops at the first
  hit (`:227-233`).

**Measured on the live ledger** (SQL in §6): all 4 `in_progress` rows carry a branch **and** a
worktree; all 7 `pending` rows carry only `{"repo":"/home/user/work/GoPi"}`. This very task's own row
is the counterexample in the other direction — `gopi task show
01a0a9b1-6b3e-7c5c-9b4c-f171b735a3f8` while it was still `in_progress`:

```
task_id:        01a0a9b1-6b3e-7c5c-9b4c-f171b735a3f8
status:         in_progress
deliverable.repo: /home/user/work/GoPi
deliverable.branch: docs/branch-task-linkage
deliverable.worktree: /home/user/workspace/gopi-branch-linkage
```

Corrected statement of the gap (§5): **an in-flight row names its branch iff its own bash shell has
already created (or moved into) a linked worktree** — i.e. iff the round that ran `git worktree add`
went through this seam. The rows that stay unnamed mid-flight are those whose bash never carried a
worktree-creating command: a wrapper row whose worktree was created outside bash, a row executing in
the main checkout, `git worktree add` run in a round whose command did not match the needle, or a row
whose deliverable had no repo to pair with (`:500`, `:165`).

---

## 2. (b) The channels that DO exist day-to-day — and exactly what each resolves

| # | channel | written by | where | resolves | does NOT resolve |
|---|---|---|---|---|---|
| ① | `deliverable.branch` + `deliverable.worktree` at **creation** | `taskBackfillDeliverableWorktree` | `task_deliverable_worktree.go:509`, seam `:208`, caller `internal/agent/loop.go:6092` | row → (branch, worktree) for a row whose bash created/moved into a linked worktree, at **any** status | rows that never ran such a bash round; nothing about *who is alive now* |
| ② | `deliverable.branch` at **completion** | `taskBackfillDeliverableBranch` | `task_deliverable_branch.go:465`, sole caller `tasks.go:5405` | row → branch for a **completed** row with a repo blob and no branch yet | not-completed rows (pending / in-flight / failed / cancelled) |
| ③ | `resumed:<session-id>` tag | `TaskTagResumed` | const `tasks.go:7263` (`taskTagResumedPrefix = "resumed:"`), caller `cmd/agent/cli_resume.go:400-409` | row → the **fresh session id** that continued it, and the count (`TaskResumeCount`, `tasks.go:5468`) | needs `--task` at resume time (opt-in; "opt-in ledger linkage", `cli_resume.go:429-434`); names a session, **never a branch or a directory** |
| ④ | merge-backlog row (`merge-backlog: <branch> unmerged …`) | `agentBranchScan` / `TaskSubmitMergeBacklog` | summary verb `tasks.go:3779`, scan submit `runTaskAuditMerges` `cmd/agent/cli_tasks.go:4710`, payload `task_merge_backlog.go:188-206`, tags `:214-226` | **branch → a task row** for an *unmerged* branch that is not already tracked (`TaskOpenMergeBacklogBranches` dedupe) | only exists for branches the scan noticed as unmerged & untracked; carries no worktree, no session, no owner row |
| ⑤ | migration provenance tags (`migrated-from-peer1`, `from-<host>`) | operator, at dispatch | seen live, e.g. `01a0a981-2858-…` | which host/session a row came from | not a branch link at all |

### Evidence (each channel, by real command)

**①② creation/completion stamps, completed row with a branch:**

```
$ gopi task show 01a0aa0e-88ca-7625-9943-d73c68a09d28
task_id:        01a0aa0e-88ca-7625-9943-d73c68a09d28
status:         completed
deliverable.repo: /home/user/work/GoPi
deliverable.branch: fix/finish-cleanup-single-node-wording
deliverable.worktree: /home/user/workspace/gopi-cleanup-singlenode
```

**① absent, mid-flight row that names no branch** (a sibling of this epic, still unclaimed — the
submit-time placeholder named by the briefing, observed on `pending`, not on `claimed`):

```
$ sqlite3 "file:$HOME/.gopi/memory.db?mode=ro" \
  "select task_id,status,deliverable from tasks where status='pending' limit 4;"
01a0a9b1-6dc6-7184-9132-f185fb912af4|pending|{"repo":"/home/user/work/GoPi"}
01a0a9b1-7050-795f-812f-c3a1733ea425|pending|{"repo":"/home/user/work/GoPi"}
01a0a9b1-72f6-7b80-a9fe-d3d12989dff8|pending|{"repo":"/home/user/work/GoPi"}
01a0a9b1-7581-7e36-ba63-39771926bba9|pending|{"repo":"/home/user/work/GoPi"}
```

and the contrasting `in_progress` row quoted in §1.1 (branch **and** worktree present).

**③ resumed tags:**

```
$ sqlite3 "file:$HOME/.gopi/memory.db?mode=ro" \
  "select task_id,status,substr(tags,1,60) from tasks where tags like '%resumed:%' limit 3;"
01a0a416-df82-7e99-9288-5a3df37147e9|failed|bug,render,gopilog,binary-summary,worker:peer1,resumed:1ff54
01a09732-2cb2-7680-9c43-4543b57401d1|verified|bug,worker:peer1,resumed:060c9825-cb59-4715-8955-acd6c0268b7
01a097f8-79bf-73d6-ad4c-1dc14abe0086|failed|bug,resumed:0a4a1f96-4716-43fb-a6af-801b7c149bb4
```

Note the shape: the tag carries a **session id**, so it resolves *lineage*, and it exists on rows
whose status is `failed`/`verified` — it is the one channel that survives a failure.

**④ merge-backlog rows (the only branch → row channel that needs neither a worktree nor a status):**

```
$ sqlite3 "file:$HOME/.gopi/memory.db?mode=ro" \
  "select task_id,status,substr(summary,1,80) from tasks where tags like '%merge-backlog%' limit 3;"
01a09462-1402-7186-944c-5c2ebde486e5|completed|merge backlog: land the 5 branches freed by the writeback-gap closeout (old zomb
t-1788441091|completed|merge-backlog: feat/maps-visual-verify unmerged — merge/archive decision needed
t-1788441090-1|completed|merge-backlog: feat/maps-inventory unmerged — merge/archive decision needed
```

**A correction on the briefing's spelling of this channel**: the briefing calls it "merge-backlog
`to-<host>:<id>` rows". No such spelling exists in the tree — a literal search for `to-` as a row
marker finds nothing (`grep -rn '"to-"' --include=*.go internal/ cmd/` → no hits), and the scan
summary is `"merge-backlog: %s unmerged"` (`tasks.go:3779`). The mechanism the briefing is pointing
at is real; the marker string it names is not (§5).

**What the union cannot do.** Channels ①②③④ are all *evidence attached to a row*. None of them
answers "which live process is holding this row's worktree right now" — that is the merger's
question, and §3 is why neither arm can answer it from the ledger alone.

---

## 3. (c) Why the merger's two axes fail

### 3.1 The sweep's owner-ended arm: empty branch never matches

`worktreeSweepOwnerEnded` is **not** in `repo_sweep.go` — it lives at
`internal/tools/worktree_sweep.go:1044` (briefing correction, §5). Its tie predicate is `:1049-1050`:

```go
if sameWorkingPath(row.Worktree, reg.Path) ||
    (reg.Branch != "" && row.Branch == reg.Branch && !worktreeSweepRowNamesOtherDir(row, reg.Path)) {
    return true, row.TaskID
}
```

The row side (`row.Branch`) comes from the parsed deliverable blob (`tasksRowForSweep.Branch`,
`:502`); the registration side (`reg.Branch`) comes from `git worktree list` (`worktreeRegistration`,
`:421-431`). Two independent ways this predicate fails to fire:

1. **A branch-only row cannot match a detached registration.** `reg.Detached` rows (git's own
   annotation, `:426-430`) carry `reg.Branch == ""`, and the guard short-circuits on
   `reg.Branch != ""` — so **only the path arm** can ever speak for a detached registration.
   Measured on this host: 19 registrations, **8 detached** (`git worktree list | grep -c 'detached
   HEAD'`), all 8 named only by path:
   `/dev/shm/…/gopi-verify-…/wt`, `~/workspace/gopi-master-ab`, `gopi-master-baseline`,
   `gopi-master-check`, `gopi-salv-textguard`, `gopi-salv-web`, `gopi-salvage-textguard`,
   `gopi-tgverify`.
2. **A row with no branch cannot match a branch registration.** `""` never equals a real branch —
   this is the briefing's point and it is correct as far as it goes, but it is the *narrower* half:
   the empty side is on the **registration** wherever the executor works detached, which is the
   common shape for verify/salvage worktrees.

The row arm also requires the row's life to be over (`worktreeSweepRowLifeEnded`, `:945`: terminal
status, or a live status whose `ended_at` is older than the grace). A row mid-flight with a matching
branch is protected **by the liveness arm instead** — `worktreeSweepRowHoldsGround` (`:923`),
consulted at `:869`; and a dirty registration with no owning row escalates rather than being removed
(`:831` → `worktreeSweepOwnedByAnyRow` `:973` → `worktreeSweepOrphanReason` `:996`). So the honest
statement is: **the branch arm is a missing protection, not a destructive one** — its failure mode is
"cannot see the owner", and the sweeper's other arms decide the outcome (age floor, holder proof,
dirt) rather than acting on a false owner match.

### 3.2 `deadHolderResumeRows`: the loader is blind to a branch-only row

`deadHolderResumeRows` (`internal/tools/task_holder_resume.go:419`, its only caller `:295`) loads
candidates with a single needle (`:430-435`):

```go
rows, err := db.QueryContext(ctx,
`SELECT task_id, COALESCE(session_id,''), status, COALESCE(claimed_by,''), COALESCE(deliverable,''),
COALESCE(`+tasksProcPidColumnName+`,0), ... FROM tasks
WHERE COALESCE(deliverable,'') LIKE ?`,
"%"+finishCleanupWorktreeVerb+"%")
```

`finishCleanupWorktreeVerb = "worktree"` (`internal/tools/task_finish_cleanup.go:391`). A branch-only
blob — `{"repo":"/home/user/work/GoPi","branch":"feat/x"}`, exactly what §1's completion-time
backfill produces — contains **no** `"worktree"` substring, so it is never returned, and the arm
cannot protect what the loader never loaded. That class is **not hypothetical**: the live ledger has
**102 rows** whose deliverable carries a branch and no worktree (§6). NB: ①'s creation-time stamp
writes both keys together, so in practice a branch-only row is one written by the **completion**
backfill on a row that never had a worktree stamped (or whose stamp was empty at the time,
`task_deliverable_worktree.go:512-515`).

The row-side blindness is real but currently **latent**, not active: there are **0** live-status
(taskLiveHolderStatus) branch-only rows right now — the 102 are on terminal rows, which the loader's
own doc block (`:415-418`) declares out of scope on purpose ("a terminal row's work is finished or
failed — there is nothing to continue"). So this is a hole in the predicate, not a live failure:
it becomes active the moment an in-flight row gets a branch stamped without a worktree, which is
precisely what epic item **[2/5] (claim-time stamping)** would introduce if it stamped a branch alone.

The sibling asymmetry is the cleanest way to state the whole defect: **the sweep's loader takes every
row with any deliverable** (`worktree_sweep.go:1274` — `WHERE COALESCE(deliverable,'') <> ''`, with
the measurement that motivated removing the needle recorded at `:1262-1271`: *"with the worktree
needle alone, a live row that named ONLY a branch never reached rung ③ — the arm cannot protect what
the loader never loaded"*), **while the resume arm still uses the needle.** The same lesson was
already learned once, on one of the two arms; the other arm never got the fix.

### 3.3 The merger's merge-candidate protection is a *different axis* — and it never reads the ledger

The briefing attributes the merger's branch axis to the sweep predicate. Worth stating plainly,
because it changes what a fix must do: **the MERGE-CANDIDATE protection does not consult the ledger
at all.** `repoSweepProtectedBranches` (`internal/tools/repo_sweep.go:573`) is the merger's "never
sweep active work" list, and it is built **entirely from git**:

```go
wtByBranch, _, err := mergedCleanupWorktrees(repo)   // git worktree list
...
protected[b] = true
```

`mergedCleanupWorktrees` (`internal/tools/task_merged_cleanup.go:778`) shells out to
`git worktree list` (`:783-786`) and maps branch → path, main checkout excluded. So the merger's
branch protection is keyed on **git registering a branch in a linked worktree** — and therefore:
(a) it is immune to ledger gaps (good), and (b) it is **blind to every detached registration**, for
the same reason as §3.1: a detached worktree has no branch to protect. The two axes must not be
conflated: a ledger-side fix (§3.2) does nothing for the merger's candidate list, and a git-side fix
does nothing for the resume arm.

---

## 4. (d) Rejected options, with reasons

### 4.1 `/proc/<pid>/cwd` scanning — rejected: this host blanks it, and it fails *open*

The only channel that can see another session's process is `/proc/<pid>/cwd`, and `ptrace_scope`
makes it unreadable for exactly the processes a sweep must not disturb. Measured **now** on this
host:

```
$ cat /proc/sys/kernel/yama/ptrace_scope
1
$ for p in /proc/[0-9]*; do readlink "$p/cwd" >/dev/null 2>&1 || bad=$((bad+1)); done
pids=346 cwd_readable=181 cwd_UNREADABLE=165
```

`cmdline`, `stat`, `status` and `cgroup` stay readable across sessions; `cwd`, `fdinfo`, `maps` and
`environ` do not. An earlier measurement of the same probe recorded 185 of ~360 unreadable,
with the diagnosis that it "answered 'no live holder' for **exactly** the processes a sweep must not
disturb, and failed **open**" (`devdoc/live-claim-worktree-veto.md:41-50`). A probe whose failure
mode is "assume nobody is there" cannot be a liveness input. Rejected.

### 4.2 Hidden sidecar session-id files — rejected: no precedent, and a crash makes them lie

The proposal is a file beside the worktree (or in the session dir) naming the owning session. Two
reasons to reject it as the *linkage* mechanism:

- **No precedent** in the tree: the ledger is the single record of task state, and every consumer in
  §2/§3 already resolves linkage through `tasks` (`openTasksDB(ctx, memoryDBPath())` — e.g.
  `task_deliverable_branch.go:472`, `task_holder_resume.go:422`, `worktree_sweep.go:1268`). A second
  on-disk record of the same fact is a mirror by construction — the exact drift Rule 14 forbids, with
  no merge path when the two disagree.
- **A crash makes it stale, and stale is indistinguishable from wrong**: SIGKILL/OOM leaves the
  sidecar behind with no writer to retract it, so the file asserts an ownership it can no longer
  evidence. The ledger at least has `ended_at`, `claimed_by` and the holder classifier to
  cross-check against; a bare sidecar has nothing to contradict it.

### 4.3 An at-exit handler that retries the backfill — rejected: it cannot run for the failures that matter

`internal/agent/crashdump.go:228 recoverRunPanic` is a **defer/recover** guard, and it fires from
exactly one recover site: `internal/agent/loop.go:2849` (`if r := recover(); r != nil { … a.recoverRunPanic(r, a.finishSessionTask) }`). Its contract (`crashdump.go:221-227`) is
in-process: write the crash dump, a `session_end(error)` event, close the session log, flip the row
to failed, then **re-panic** so the exit code stays non-zero.

That covers Go **panics** in the run loop and nothing else. `SIGKILL` cannot be caught by any
handler — the kernel does not run user code for it — and neither OOM-kill (which is SIGKILL) nor a
runtime fatal (fatal error, stack exhaustion) routes through a Go `recover`. The sweep's own
vocabulary is full of the SIGKILL shapes this leaves behind: abandoned `initializing` locks
(`worktree_sweep.go:211`, `:549`), registrations "killed in the middle of creating" (`:1668`,
`:1702`), and a sweeper age default chosen around "an hour of the SIGKILL that made it" (`:117`).
So an at-exit retry would fire on the failures that were already handled, and be absent for the
failures the epic exists to repair — a guarantee that holds only where it is not needed. Rejected.

---

## 5. Corrections to the briefing (stated, not silently absorbed)

1. **`worktreeSweepOwnerEnded` is in `internal/tools/worktree_sweep.go:1044`, not `repo_sweep.go`.**
   `repo_sweep.go`'s branch involvement is a *different* mechanism — `repoSweepProtectedBranches`
   (`:298`), which never reads the ledger (§3.3).
2. **"a claimed/in_progress row's deliverable is the repo-only placeholder and names no branch" is
   only true for `pending`.** A creation-time stamp (`task_deliverable_worktree.go:509`, seam
   `:208`, caller `internal/agent/loop.go:6092`) writes branch+worktree at any status; measured, all 4 live
   `in_progress` rows carry a branch, all 7 `pending` rows do not (§1.1, §6). The real discriminator
   is "did this row's bash ever create/move into a linked worktree", not "is it claimed".
3. **`deadHolderResumeRows` "cannot even load a branch-only row" is correct as a predicate
   statement and currently latent as a live failure** — 102 such rows exist but all on terminal
   statuses, which the arm excludes by design; there are 0 live-status ones (§3.2). The claim is
   right about the hole; it is wrong if read as "this is failing in production today".
4. **The `to-<host>:<id>` merge-backlog marker does not exist.** The channel is real (a
   `merge-backlog:` summary + `merge-backlog`/`merge-fix`/`merge-feat` tags), the string is not
   (§2, correction there).
5. **The briefing's `/proc` figure (165/353) is from the live-claim-veto measurement era, not this
   host's current reading** — re-measured now as 165/346. The conclusion is unchanged and is the
   point: the probe fails open for a large minority of processes, and the mix shifts with load.

---

## 6. Live ledger census (the query and its output)

```sql
sqlite3 "file:$HOME/.gopi/memory.db?mode=ro" "
select status, count(*), sum(deliverable like '%\"branch\"%'),
       sum(deliverable like '%\"worktree\"%') from tasks group by status order by 2 desc;"

completed|1145|422|332
cancelled|187|11|9
failed|121|48|39
verified|64|61|61
pending|7|0|0
in_progress|4|4|4
```

Totals: **1528 rows / 546 with a branch / 445 with a worktree / 102 branch-only**. Reading it:

- `pending` (7): **0** with a branch — the repo-only placeholder is real, and confined to this status.
- `in_progress` (4): **4** with a branch — the creation-time channel works for the rows that used it.
- `verified` (64): 61 with a branch — the highest linkage density, because verification runs go
  through a wrapper whose worktree is created for them.
- `cancelled` (187): 11 with a branch — a cancelled row keeps whatever the creation stamp wrote, and
  the completion backfill never touches it (status gate, `task_deliverable_branch.go:481`).
- `failed` (121): 48 with a branch, **none** from the completion backfill (completed-only) — all 48
  came from the creation-time seam or manual stamps.
- **102 branch-only rows**: the population §3.2's needle excludes.

---

## 7. Verification

- **Diff shape (doc-only, as required)**: this change adds exactly one path, this report.
  See the `git show --stat` output recorded below.
- **Build**: `CGO_ENABLED=0 go build ./...` — recorded below.
- **Tests**: the code described above is exercised by the `internal/tools` suite; the targeted runs
  are recorded below. Because the diff touches **no** `.go` file, no test result can change — the
  runs are recorded as the honest form of "unchanged", not as new evidence.

<!-- VERIFY:BEGIN -->
```
$ git diff --cached --stat
 devdoc/branch-task-linkage.md | 407 ++++++++++++++++++++++++++++++++++++++++++
 1 file changed, 407 insertions(+)

$ CGO_ENABLED=0 go build ./...
build_exit=0

$ go test -run 'TestWorktreeSweep|TestDeadHolder|TestResume.*Row|TestBackfillDeliverable|TestTaskDeliverableWorktree' ./internal/tools/
ok  	github.com/nick/gopi/internal/tools	3.841s
```

The stat line is the acceptance condition itself: **one path, a `.md`, zero `.go` files** — the diff
cannot change a build or a test outcome, and the two runs above confirm the tree they describe is
green at the baseline this report was written against.
<!-- VERIFY:END -->

---

## 8. What this report implies for the rest of the epic (not done here)

- **[2/5] claim-time stamping** must stamp the pair **together**, or it re-creates §3.2: a branch
  without a worktree is invisible to the resume arm's loader.
- **[3/5] `deadHolderResumeRows`** has a worked precedent to copy rather than invent: drop the needle
  the way `worktree_sweep.go:1274` already did, and let the parsers decide.
- **[4/5] retry of the wrapped executor child** is the honest alternative to §4.3's at-exit handler:
  the wrapper already `waitpid`s (`cmd/agent/cli_tasks.go:1772`), so a non-zero exit is a *sighting*
  of the failure the crash handler cannot see.
- **[5/5] `gopi task for-branch <branch>`** is answerable from channels ①③④ for the common cases, and
  from the merge-backlog row for an unmerged branch; it should say *which* channel answered, and
  answer empty rather than guess when none did (§3.3's git-only axis must not be silently merged
  into a ledger answer).
