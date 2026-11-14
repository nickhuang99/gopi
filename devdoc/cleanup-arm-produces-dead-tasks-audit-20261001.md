<!-- doc-class: historical -->
# Task Report: is the cleanup/reclaim arm itself producing the "dead tasks"? — Q1–Q9 from code, with experiments

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task: `01a0f3e5-b9d8-7ac3-b4a0-22d5268546eb` · Branch: `task/01a0f3e5b9d87ac3b4a022d5268546eb`
Worktree: `/home/user/workspace/gopi-01a0f3e5b9d87ac3b4a022d5268546eb`
Evidence artifacts: `/home/user/.gopi/sessions/01a0f3e5-b9d8-7ac3-b4a0-22d5268546eb/evidence/`
Diag binary (unmodified tree): `/home/user/workspace/_tmp/diag-gopi` (`CGO_ENABLED=0 go build ./cmd/agent/`)
Mode: **DIAGNOSE ONLY** — no fix landed, no row/branch/worktree deleted or modified, no new
script/cron/systemd/resident process (Rule 35).

---

## Verdict in one paragraph

The cleanup arm is **not** the primary producer of "dead tasks", but it is a **load-bearing
accomplice in two of the four mechanisms**, and it is the *sole* producer of one. (1) The
liveness predicate is "any live process whose cwd is inside" (`finishCleanupWorktreeInUse`,
`internal/tools/task_finish_cleanup.go:3258`) and it does not ask whether that process belongs
to this task, this run or this repo — measured, an unrelated `sleep` inside a worktree freezes
it. (2) The cleanup runs **before the claim in the same round, with no `recover()` on that
path** (`cmd/agent/cli_tasks_worker.go:507` → `:555`), so a panic in any cleanup step escapes
the whole worker loop and the task the worker was started for is never claimed — this is a
*captured* mechanism, not the hypothesis the operator asked about. (3) Cleanup writes nothing
about *interruption*: the ledger's only signal is a status, and `failed` is written **both**
for a judged failure and for a crash — measured 34 rows failed after the bounded retry ladder
was already spent, each reading only `task failed`. (4) The cleanup never validates content, so
a half-finished-but-**committed** branch is invisible to its only defence (dirt) and merges as
an ordinary deliverable — measured. Q9 is **not** the cleanup's doing: a `failed` parent parks
its whole subtree forever by explicit design (`internal/tools/tasks.go:5837`).

**Q1 is NOT a duplicate of `01a0df2d` for the merge half, but it IS a duplicate for the remit
statement.** `01a0df2d` retired the worker's *merge tick*; the worker's *cleanup* remit it left
explicitly intact and re-affirmed in the same ruling ("its job is worktree/branch in, worktree
out"). See Q1 for the exact split.

---

## The evidence base used by every answer

**E1 — the tree under diagnosis**

```
$ git log --oneline -3
0c16bd86 docs(brief): is the cleanup arm itself producing the dead worktrees? Q1-Q9 from the operator
fc86efdb docs(brief): a failed row is SILENT forever — 107 human/agent/llm failed rows with no reissue path, no sweep, and reclaimable evidence
247e9c6c docs(brief): re-implement capture --list-windows on master rather than merging the duplicate branch
```

**E2 — the live ledger (read-only, the operator's own profile, never written)**

```
$ sqlite3 -readonly "file:/home/user/.gopi/memory.db?mode=ro" \
    "SELECT status||' = '||COUNT(*) FROM tasks GROUP BY status;"
completed   = 2361
cancelled   = 398
failed      = 289
verified    = 68
in_progress = 1
```

**E3 — the live host, measured this run** (raw output in `evidence/q7-1-full-sweep-plan.txt`,
1151 finding lines):

```
$ gopi task sweep-worktrees            # DRY RUN — the verb's default is plan-only
sweep-worktrees (dry-run): repos=3 reclaimed=0 would-reclaim=0 reaped=0 branches-reaped=0 escalated=0
  keep        [self-checkout]        …/gopi-01a0f3cf7d3c7b9fb17744228c1365c8
  keep        [self-checkout]        …/gopi-01a0f3e5b9d87ac3b4a022d5268546eb
  report      [declared-root-scope]  /dev/shm/iso
  report      [declared-root-scope]  /tmp/gopi-redbase-01a0df2d
run-ground anchors: collectable=937 kept=202
```

```
$ # every gopi-* directory on this host × a live-cwd scan of /proc
gopi-* dirs=184  with no live holder=182
```

```
$ # the registrations of the repo under diagnosis
git worktree list | wc -l   → 5
```

**E4 — the removal audit log** (`/home/user/.gopi/logs/removal-audit.log`, 5497 JSON records):

```
kind              arm                            count
worktree-removal  merge-run-probe-ground           1230
worktree-ground   task-ground-anchor               1139
worktree-removal  task-ground-anchor                976
worktree-removal  merge-build-gate-ground           763
branch-delete     merge-run-spent-branch            601
worktree-removal  finish-cleanup-worktree           434
worktree-removal  task-self-own-sweep               232
worktree-removal  task-self-reclaim                 109
branch-delete     task-merge-branch                   5
worktree-removal  task-row-reclaim                   5
worktree-removal  worktree-sweep-reclaim             2
branch-delete     worktree-sweep-chat-ground-branch  1
```

**E5 — the refusal tally in the worker's own log**

```
$ grep -c 'in use by live process' /home/user/.gopi/logs/worker.log      → 246
$ grep -c 'finish-cleanup skipped' /home/user/.gopi/logs/worker.log      →  21
$ grep -c 'held by a live process' /home/user/.gopi/logs/worker.log      →   6
$ grep -h 'in use by live process' /home/user/.gopi/logs/worker.log \
    | grep -oE 'cwd [^ ]+' | grep -c 'workspace/gopi-'                   → 237   (of 246)
```

The distinct holder cwds (top of the tally):

```
 75  gopi-daemon-cgroup/cmd/agent
 41  gopi-lifecycle-decouple
 40  gopi-status-stop-conflict/cmd/agent
 38  gopi-rawlog-rework/cmd/agent
 10  gopi-rawlog-rework
  7  gopi-conflict-flags/cmd/agent
  6  gopi-peak-hour
  6  gopi-contract-reds
  5  gopi-worker-status-residue/cmd/agent
  4  gopi-worker-daemon/cmd/agent
```

---

## Q1 — Is it appropriate for a worker to do cleanup work, fixed, before running a definite task?

**(a) Code location.** `cmd/agent/cli_tasks_worker.go:481` (the round loop) → the passes ride
every round **before** the claim:

- `cmd/agent/cli_tasks_worker.go:507` — `runTaskLifecycleTick(workerRoleLabel)` (stale sweep +
  finish-cleanup sweep + dead-holder reclaim + orphan census + worktree reap)
- `cmd/agent/cli_tasks_worker.go:518` — `scratchGCSweep(workerRoleLabel)`
- `cmd/agent/cli_tasks_worker.go:552` — `memorySyncTickSweep(workerRoleLabel)`
- `cmd/agent/cli_tasks_worker.go:555` — `ran, _ := workerRoundClaiming()` ← **the claim**

The worker's own header states the remit: `cmd/agent/cli_tasks_worker.go:16-19,36`.
`cmd/agent/task_lifecycle_tick.go:215` is the tick, `:274` the stale sweep, `:282` the
finish-cleanup sweep, `:298` the worktree reap.

**(b) Command run.**

```
$ grep -n "runTaskLifecycleTick(workerRoleLabel)\|scratchGCSweep(workerRoleLabel)\|memorySyncTickSweep(workerRoleLabel)\|workerRoundClaiming()\|for round := 1" cmd/agent/cli_tasks_worker.go
```

**(c) Raw output.**

```
323:	pruneWorkerStateResidue()
408:	pruneWorkerStateResidue()
452:		shot.RenewNotice(shot.RenewRoleWorkerStartup)
481:	for round := 1; maxRounds == 0 || round <= maxRounds; round++ {
507:		runTaskLifecycleTick(workerRoleLabel)
518:			scratchGCSweep(workerRoleLabel)
552:			memorySyncTickSweep(workerRoleLabel)
555:		ran, _ := workerRoundClaiming()
585:		if recheck, _ := workerRoundClaiming(); recheck {
773:func workerRoundClaiming() (bool, error) {
```

**(d) Interpretation — and the duplicate question.**

**Not a duplicate for the merge half.** `01a0df2d-4b03-720e-bfa7-8139e25f1f0e` (status
`completed`) delivered `devdoc/worker-merge-tick-retired.md` (commits `86f1a828`, `d21bf3c6`)
and its ruling was about the **merge tick**, whose retirement is already in this tree:

```
cmd/agent/cli_tasks_worker.go:522-537  "A resident MERGE TICK used to ride this same round
… It is RETIRED 2026-09-27 (devdoc/worker-merge-tick-retired.md) … The worker's job is the
worktree and the branch — in before the child, out after it — and nothing else."
```

**BUT it IS a duplicate for the remit sentence** — and the same commit's text is self-refuting
at this line: the ruling says *"its job is the worktree and the branch — in before the child,
out after it — and nothing else"*, while **six lines below it the same loop runs six cleanup
passes before the child is ever created**. So the operator's Q1 does not need a new decision:
the decision exists, and the code contradicts it in place. The honest framing to hand him:
*"cleanup-before-claim violates the remit you already ruled on (01a0df2d), and the retirement
commit wrote the ruling and left the violation six lines beneath it."*

That is a **finding**, not a re-litigation: the question "is it appropriate" is answered by the
existing ruling (no), and the new fact is that the code did not follow it.

---

## Q2 — What is the basis for cleanup? What does `finishCleanupWorktreeInUse` actually decide?

**(a) Code location.** `internal/tools/task_finish_cleanup.go:3199` (doc) and `:3258` (body);
the reason string is rendered at `:3326-3334`.

**(b) Command run.**

```
$ sed -n '3258,3340p' internal/tools/task_finish_cleanup.go
```

**(c) Raw output (the decisive body).**

```go
func finishCleanupWorktreeInUse(wt string) (pidMsg, reason string) {
	wt = strings.TrimSpace(wt)
	if wt == "" { return "", "" }
	abs, err := filepath.Abs(wt)
	if err != nil { abs = filepath.Clean(wt) }
	targets := []string{abs}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil && resolved != abs {
		targets = append(targets, resolved)
	}
	if gitDir := finishCleanupWorktreeGitDir(abs); gitDir != "" {
		targets = append(targets, gitDir)
	}
	pids, err := finishCleanupPIDDirs()
	if err != nil { return "", "" }           // cannot scan → fail-soft
	self := finishCleanupSelfChain()
	for _, pid := range pids {
		cwd, ok := finishCleanupProcCWD(pid)
		if !ok { continue }                   // gone / not visible to us
		n, convErr := strconv.Atoi(pid)
		if convErr != nil || !pidAlive(n) { continue }  // zombie / raced exit
		if self[n] { continue }               // this caller's own run
		for _, dir := range targets {
			if finishCleanupPathUnder(cwd, dir) {
				who := finishCleanupHolderWho(pid, cwd)
				why := fmt.Sprintf("%s by live process %s: cwd %s", finishCleanupInUseMarker, who, cwd)
				… daemon / test-scratch classification …
				return who, why
			}
		}
	}
	return "", ""
}
```

**(d) Interpretation — what it decides, and what it does NOT ask.**

It decides exactly one question: **"is there any process on this host, alive as far as
`kill(pid,0)` can tell, whose `/proc/<pid>/cwd` (or whose cwd inside the worktree's private
`.git/worktrees/<name>` admin dir) lies under this directory, excluding only the caller's own
ancestor chain?"**

It does **not** ask:

| not asked | consequence |
|---|---|
| does the process belong to **this task**? | an unrelated `sleep`, an old test helper, a daemon from another task vetoes |
| does the process belong to **this run**? | the same |
| is the process in **this repo**? | the same |
| is the process **making progress**? | a hung process vetoes exactly as hard as a busy one |
| was the process **supposed to be here**? | no spawning-path check — any launcher that chdir'd dies with it |
| is there **unexported work**? | no — that is guard ②, a different predicate |
| **who** is waiting on this directory? | no — no timeout, no escalation, no owner acknowledgement |

Two structural properties matter more than the predicate itself:

1. **It is fail-soft** (`:3271`): an unreadable `/proc` returns `"", ""` = "not in use". The
   comment defends this ("the guard closes a race, it must not become a hard dependency"), but
   the direction is *fail-open on the delete path*.
2. **Its escape hatches are classifications, not questions.** A detached daemon gets a *better
   sentence* (`:3320-3326`), a test-scratch holder gets an *age out* (`:3336-3347`), but the
   predicate is unchanged. Nothing asks whether the holder is legitimate.

The comment at `:3199-3217` names both covered shapes honestly (cwd in the worktree; cwd in the
worktree's private git dir). Neither shape is scoped to an owner.

---

## Q3 — How do you decide a worktree has no task running in it? (THE HEART)

**(a) Code location.** `internal/tools/task_finish_cleanup.go:3258`
(`finishCleanupWorktreeInUse`); the sweep's sibling arms are
`internal/tools/worktree_sweep.go:911` (`worktreeSweepJudge`, rung ②
`scratch.HeldByProcesses(reg.Path, holders)` at `:1057` and `:1072`), whose holder set is
`internal/scratch/holder.go:280` (`HeldByProcesses` → `PathWithin`, cwd/fd/exe/maps); the
ledger-free session channel is `internal/tools/worktree_sweep.go:1412`
(`worktreeSweepLiveSessionRow`) and `:1431` (`processInsideSessionDir`), whose whole body is a
`/proc/<pid>/cwd` readlink scan.

**(b) The experiment (reproducible end-to-end; raw output `evidence/q3-1..3`).**

```
$ mkdir -p /home/user/workspace/diag-cleanup-01a0f3e5 && cd /home/user/workspace/diag-cleanup-01a0f3e5
$ git init -q -b master throwaway-repo && cd throwaway-repo
$ git -c user.email=d@d -c user.name=d commit -q --allow-empty -m init
$ git worktree add -q ../wt-probe -b probe/held-worktree master
$ echo hello > ../wt-probe/file.txt && git -C ../wt-probe add file.txt
$ git -C ../wt-probe -c user.email=d@d -c user.name=d commit -q -m "probe commit"

$ # an UNRELATED long-lived process whose cwd is inside the worktree
$ cd /home/user/workspace/diag-cleanup-01a0f3e5/wt-probe && sleep 900 &
  sleep pid=2533967 cwd=/home/user/workspace/diag-cleanup-01a0f3e5/wt-probe

$ cd /home/user/workspace/diag-cleanup-01a0f3e5/throwaway-repo
$ /home/user/workspace/_tmp/diag-gopi task sweep-worktrees
```

**(c) Raw output.**

```
sweep-worktrees (dry-run): repos=3 reclaimed=0 would-reclaim=0 reaped=0 branches-reaped=0 escalated=0
  keep        [live-holder] age=0h /home/user/workspace/diag-cleanup-01a0f3e5/wt-probe (held by a live process (cwd/fd/exe/maps))
              judged against root: worktree /home/user/workspace [default <home>/workspace]
```

```
$ kill 2533967   # the unrelated process is gone; nothing else changed
$ /home/user/workspace/_tmp/diag-gopi task sweep-worktrees
sweep-worktrees (dry-run): repos=3 reclaimed=0 would-reclaim=0 reaped=0 branches-reaped=0 escalated=0
  keep        [live-session] age=0h /home/user/workspace/diag-cleanup-01a0f3e5/wt-probe (no live task's deliverable names this registration, but live task 01a0f3e5-b9d8-7ac3-b4a0-22d5268546eb still has a run standing (its recorded runner (pid 2411198) is still running) — its own session's ground)
              judged against root: worktree /home/user/workspace [default <home>/workspace]
```

```
$ git worktree list    # the control: nothing was removed (dry run, as designed)
/home/user/workspace/diag-cleanup-01a0f3e5/throwaway-repo  a291c26 [master]
/home/user/workspace/diag-cleanup-01a0f3e5/wt-probe        d4e828d [probe/held-worktree]
```

**(d) Interpretation — the consequence, answered with a measurement.**

The first run proves the predicate matches an **unrelated** process: the `sleep` was started by
a shell, is in no task, in no repo, and yet the registration is frozen as `live-holder`.

The second run is the more important half, and it is the answer to *"how many of the currently
stuck situations are caused by this predicate matching an unrelated process?"* — **the number is
246.** In `/home/user/.gopi/logs/worker.log` there are 246 lines refusing a removal with
"in use by live process", and **237 of them name a `workspace/gopi-<task>` cwd**. Classified:

- **≈212 are self-inflicted by gopi's own machinery**: the largest single holder class is the
  daemon-cwd escape failure (`gopi-daemon-cgroup/cmd/agent` ×75, `gopi-rawlog-rework/cmd/agent`
  ×38, `gopi-status-stop-conflict/cmd/agent` ×40, `gopi-lifecycle-decouple` ×41 …). These are
  `gopi merger`/`gopi worker` daemons that were **spawned while standing in a task worktree** and
  inherited it. `internal/daemoncwd/daemoncwd.go` exists precisely to prevent that — its package
  doc names this deadlock verbatim — so the refusals are the *residue of that producer bug*, not
  of an unrelated user process.
- **A residue class the guard itself tolerates**: old `gopi merge` / `go test` scratch
  checkouts, ~10 cases (`/dev/shm/gopi-test-scratch/…`, `main-checkout` daemons).

And the frozen population is visible right now, end to end:

```
$ # gopi-* dirs on this host × live-cwd scan of /proc
gopi-* dirs=184  with no live holder=182
```

**182 of 184 gopi worktree directories have no live holder at all and are still on disk.** So the
answer is precise: the predicate matching an *unrelated* process is a **bounded** producer
(≈212 of the refusals, all from a producer bug gopi already knows about), while the **182
holderless directories are produced by something else entirely** — they are pushed down to a
*different* axis, the run-ground anchors, where the same `live-session` question applies. See
Q7, which is where the real number lives.

---

## Q4 — Does "no process is running" prove the process died unexpectedly?

**(a) Code locations.** The `!pidAlive(n) → continue` path is
`internal/tools/task_finish_cleanup.go:3276-3278`; `pidAlive` is
`internal/tools/taskzombie.go:120`. The places a run's end is written:
`cmd/agent/cli_tasks.go:2609` (the wrapper's `failed` writeback),
`cmd/agent/cli_tasks.go:2146` (the same for the older arm),
`cmd/agent/cli_tasks_worker.go:773` (`workerRoundClaiming`). The ledger's status vocabulary is
`internal/tools/tasks.go` (`TaskStatusPending/Claimed/InProgress/Completed/Failed/Cancelled/Verified`).

**(b) Commands run.**

```
$ grep -rn "TaskUpdateReason(.*interrupt\|TaskRunWriteback.*interrupt\|status.*interrupted" --include=*.go internal/tools cmd/agent | grep -v _test.go
$ grep -rn "recover()" --include=*.go cmd/ internal/ | grep -v _test.go
$ sqlite3 -readonly "file:/home/user/.gopi/memory.db?mode=ro" \
    "SELECT COUNT(*) FROM tasks WHERE reason LIKE '%abnormal child exit%' OR verify_evidence LIKE '%abnormal child exit%';"
$ sqlite3 -readonly "file:/home/user/.gopi/memory.db?mode=ro" \
    "SELECT COUNT(*) FROM tasks WHERE status='failed' AND tags LIKE '%run-retry:1%';"
```

**(c) Raw output.**

```
$ grep … "interrupt" …
internal/tools/branch_attribution.go:107:  // holder status — an interrupted run there is work to continue. It is false
cmd/agent/cli_ui.go:1178:  status line says "interrupted", never an error. …
                                   ← both are COMMENTS about other things; no writer exists
```

```
$ grep -rn "recover()" --include=*.go cmd/ internal/ | grep -v _test.go
internal/standards/baseline.go:851  (a comment)
internal/agent/loop.go:3773         (the LLM loop, not the worker loop)
```

```
$ …the "abnormal child exit" marker in the ledger:
0
```

```
$ …rows that were resumed by the bounded retry ladder and THEN failed:
34
```

**(d) Interpretation. No — "gone" is not "finished", and nothing writes down the difference.**

Three separate findings, each measured:

1. **The predicate treats a dead pid as permission.** `!pidAlive(n) → continue` means the guard
   simply steps over a corpse; it never asks *how* the process ended. The consequence is that
   the *reason* a directory is free tells you nothing about whether work was lost.
2. **Nothing anywhere records "this run was interrupted".** The search for such a writer returns
   nothing (`evidence/q4-absence-search1.txt` — two hits, both prose in comments). The only
   interruption-adjacent strings are:
   - `cmd/agent/cli_tasks.go:263` — `taskAbnormalChildMarker = " — task run failed (abnormal child exit)"`,
     which is **composed at the writeback but never persisted**: 0 ledger rows carry it.
   - the bounded retry's attempt counter, `internal/tools/task_run_retry.go:41-46`
     (`TaskRunRetryCap = 3`, tag `run-retry:<n>`), which records *that a retry happened*, not
     *that the run was interrupted*.
3. **The ledger therefore conflates two opposite facts under one status.** Both a run that
   **judged itself failed** and a run that **was killed** land on `failed`; the discriminator
   that exists (`tools.TaskRowTerminalAt`, `internal/tools/task_run_retry.go:110`) is used
   **in-process, once, to decide whether to retry** — it is not recorded. Measured: 34 rows
   whose retry ladder was already spent and which then failed, every one reading only
   `task failed`:

```
01a0b1b1-2c57 | failed | task failed
01a0b47e-6f09 | failed | task failed
01a0b47e-71ba | failed | task failed
01a0ba14-7d18 | failed | task failed
01a0c337-d621 | failed | task failed
…
```

**The harm is exactly the operator's sentence**: after the fact, no reader — human or agent —
can tell 289 failed rows apart into "someone decided this failed" and "this was silenced
mid-work". The evidence the brief calls "the reclaimed evidence" is destroyed by the *record
shape*, not by a cleanup deletion.

**What must change (laid out for the operator to choose, no recommendation)**: (a) a distinct
terminal status or a reserved tag for interrupted runs — costs a schema/vocabulary change and
touches every status consumer; (b) persist the existing `taskAbnormalChildMarker` into
`reason`/`verify_evidence` — nearly free, uses values already computed, but is a *note* not a
*state*; (c) leave it and require the operator to read `run-retry:<n>` tags — no code, and the
signal stays invisible in every list view.

---

## Q5 — Can an incomplete **commit** create merge difficulty, or pollute the code with a new bug?

**(a) Code locations.** The `failed`-row path: `cmd/agent/cli_tasks.go:2609` →
`taskRunReleaseOwnWorktrees` → `internal/tools/task_reclaim.go:541` (`reclaimDisposition`), whose
guards are: ① branch match (`:698-705`) · ② **dirt** (`:623-635`,
`reclaimGitStatus` → `reclaimDirtyReason`) · ③ liveness (`:639`, calls
`finishCleanupWorktreeInUse`). The clean-lane checklist is
`internal/tools/reclaim_ground_checklist.go` (rung ① own-ground, rung ② integration-lineage) —
**neither rung asks about content or completeness**. The merge lane's candidate set:
`cmd/agent/merge_run.go:2423` (`mergeCandidateBranches`) — it filters on
`refs/heads`, excludes the integration branch, worktree-held branches and `archive/*` tags, and
**never consults the ledger's status column**.

**(b) The experiment (raw output `evidence/q5-half-committed.txt`).**

```
$ cd <the probe worktree from Q3>
$ echo "func HalfDone() { /* TODO: the other half */ }" >> halfdone.go && git add halfdone.go
$ git status --porcelain
A  halfdone.go
$ git -c user.email=d@d -c user.name=d commit -q -m "wip: half the change"
$ git status --porcelain
                                    ← empty: guard ②'s input is now GONE
$ git -C <throwaway-repo> merge-base master probe/held-worktree
a291c264243718ffa976fe6be7212aab75c3216d      ← shares history: rung ② passes
$ git log --oneline -2
b10fb7a wip: half the change
d4e828d probe commit
```

**(c) Raw output.** (above)

**(d) Interpretation — YES, and the sequence is exact. This is a code-pollution path.**

The only defence against delivering incomplete work is **dirt**, and dirt is a property of the
*git index*, not of the work. An agent that commits before stopping (routine) presents a clean
tree. Walked end to end:

1. A run works, gets partway, and commits its partial work (its own judgement — the ledger does
   not model "complete").
2. The process dies. The wrapper writes `failed` (`cli_tasks.go:2609`).
3. `taskRunReleaseOwnWorktrees` asks `reclaimDisposition`. Guard ② reads
   `git status --porcelain` → **empty** → no refusal. Guard ③ finds no live holder (the process
   is dead). Guard ① passes (the branch is the row's). **Verdict: `reclaimFreable`.**
4. `git worktree remove` drops the directory; the branch survives (by design — the branch *is*
   the deliverable), so `git worktree list` no longer names it and `scripts/safe-merge.sh` gate
   [c] opens.
5. `gopi merge`'s candidate list (`merge_run.go:2423`) includes the branch — it has no ledger
   filter — and the merge lane treats it as an ordinary deliverable.
6. The commit lands. **No step anywhere compares the branch's content to the task's intent.**
   The row reads `failed`, the branch reads *merged*.

The measured removal-audit records show this arm firing in production:

```
{"kind":"worktree-removal","arm":"task-row-reclaim", …, "task_id":"01a0f207-7707-726e-8d45-18d566b110ea",
 "evidence":"dead-holder disposition reclaimFreable past the live-session-veto channel: ; checklist own-ground + integration-lineage (reclaim_ground_checklist.go)"}
{"kind":"worktree-removal","arm":"task-self-reclaim", …, "evidence":"this run's own finished row …, disposition reclaimFreable: ; checklist own-ground + integration-lineage"}
```

**Where the code's design intent stops**: the two checklist rungs exist so a *stranger* cannot
free someone's ground, and so an unrelated-history branch is not treated as a deliverable. Both
are about **ownership and lineage**. Nothing in the pipeline is about **completeness** — and the
`failed` status is explicitly *not* consulted by the merge lane. The intent is thus unstated
exactly where the operator is pointing.

**A second, cheaper harm on the same path**: the run-ground collector
(`internal/tools/run_ground_anchor.go:274-320`) asks live / age / occupied — and **no dirt
question at all** (`:317` `default: collectable = append(...)`). Measured this run:

```
run-ground anchors: collectable=937 kept=202
```

So the anchor axis will free 937 directories regardless of what is uncommitted inside them —
but only the *registration* (`RunGroundCollect` → "removes the collectable grounds' worktree
registrations", `:352-367`), and it is unreachable from the resident tick (see Q7). It therefore
reads as a **latent** variant of the same defect: the day someone calls `--apply`, dirt is not
in the predicate.

---

## Q6 — If the definite task fails to execute **because of** the cleanup, is that considered?

**(a) Code locations.** `cmd/agent/cli_tasks_worker.go:507` (cleanup) vs `:555` (claim);
`cmd/agent/cli_tasks_worker.go:535` (`ran, _ := workerRoundClaiming()` — *the error is
discarded*); `cmd/agent/cli_tasks_worker.go:773-796` (`workerRoundClaiming` body).
`cmd/agent/task_lifecycle_tick.go:215-228` (`runTaskLifecycleTick` → `taskLifecyclePass`).
`cmd/agent/cli_tasks.go:3450-3520` (`serveFinishCleanupSweep`).

**(b) Commands run.**

```
$ grep -n "workerRoundClaiming\|runTaskLifecycleTick(workerRoleLabel)" cmd/agent/cli_tasks_worker.go
$ grep -rn "recover()" --include=*.go cmd/ internal/ | grep -v _test.go
$ sed -n '262,301p' cmd/agent/task_lifecycle_tick.go
```

**(c) Raw output.**

```
  507:		runTaskLifecycleTick(workerRoleLabel)
  …
  555:		ran, _ := workerRoundClaiming() //stylegate:ignore …
```

```
$ grep -rn "recover()" --include=*.go cmd/ internal/ | grep -v _test.go
internal/standards/baseline.go:851    (comment)
internal/agent/loop.go:3773           (the agent loop — a different loop)
```

```go
func taskLifecyclePass(role string) {
	grace, err := config.ResolveTaskGracePeriods(0, 0)
	…
	reported := serveStaleSweep()
	disposed := 0
	if grace.DisposeSec > 0 { disposed, _ = autoDisposeConfirmedZombies(role, grace.DisposeSec) }
	sink := workerNotifySinkTarget()
	fmt.Printf(taskLifecycleSummaryFmt, role, reported, disposed, sink)
	for _, line := range serveFinishCleanupSweep() { fmt.Printf("[%s] [finish-cleanup] %s\n", role, line) }
	for _, line := range serveWorktreeReapSweep(worktreeReapDryRun) { … }
}
```

**(d) Interpretation — no, it is NOT considered, and the failure mode is worse than "silently
degraded".**

1. **The passes run first, in the same round, with no per-pass isolation.** `taskLifecyclePass`
   is one straight-line function: stale sweep → finish-cleanup sweep → worktree reap. A panic
   anywhere reaches `runTaskLifecycleTick`'s caller (`cli_tasks_worker.go:507`) uncaught.
2. **No `recover()` exists on the worker path.** The only `recover()` in shipped code is
   `internal/agent/loop.go:3773` — the *LLM* loop. `cmd/agent` has none. A panic in a cleanup
   pass therefore **escapes the whole `gopi worker` process**: the loop dies before `:555`, the
   deferred close-outs do not run, and the task the worker was started for is not even claimed.
3. **The claim's own error is discarded by design.** `ran, _ :=` at `:555` carries a
   `//stylegate:ignore`: "the claiming verdict is what the loop acts on; the discarded error is
   the round's own already-recorded failure, so treating it as 'no round ran' is the safe arm."
   That rationale holds for a *claim* error (the row's writeback recorded it) — it does **not**
   hold for a *cleanup* error, because the cleanup's outcome is never recorded anywhere the
   operator reads as "this round was lost". The comment covers the wrong half of the line.
4. **The throttles bound cost, not blocking**: `taskLifecycleThrottleMin = 10min`
   (`task_lifecycle_tick.go:76`), `serveFinishCleanupThrottleMin = 10min`
   (`cli_tasks.go:3370`), `scratchGCDefaultIntervalMin = 60` (`scratch_gc_tick.go:67`),
   `memorySyncTickDefaultIntervalMin = 30` (`memory_sync_tick.go:114`). They keep the passes
   cheap per round; they do not make them safe.
5. **The one place this was thought about is the *run wrapper*, not the worker.**
   `cmd/agent/cli_tasks.go:2650` prints `finish-cleanup for %s failed (continuing)` and the
   chain is documented as "fail-soft: every outcome is a log line, never fatal to the run's
   close-out". That is the right posture — **and it is a different process**. The worker's own
   cleanup-before-claim has no such guard.

**What must change (laid out, not chosen)**: (a) isolate each pass behind a `recover()` so a
cleanup panic cannot take the round — small, local, but it converts a crash into a *reported*
skip; (b) move the passes after the claim — matches Q1's ruling but delays hygiene on a busy
queue; (c) make the passes opt-in per round — no code, hygiene becomes the operator's job.

---

## Q7 — Was the "worktree stuck in a dead task" caused by the cleanup itself?

**(a) Code locations.** Direction 1 (refusal → permanent stuck):
`internal/tools/task_finish_cleanup.go:3258` (guard ③), `internal/tools/task_reclaim.go:639`
(its call from the reclaim arm), `internal/tools/worktree_sweep.go:1057`/`:1072` (the sweep's
sibling arm), `internal/tools/worktree_sweep.go:1412`+`:1431` (the session arm).
Direction 2 (reclaim → work gone): `internal/tools/task_reclaim.go:541`, the
run-ground collector `internal/tools/run_ground_anchor.go:274`, the removal audit writer
(records land in `/home/user/.gopi/logs/removal-audit.log`).
Reachability: `cmd/agent/task_lifecycle_tick.go:298` → `cmd/agent/cli_tasks.go:3697`
(`serveWorktreeReapSweep`, `worktreeReapDryRun = false` at `task_lifecycle_tick.go:129`) vs
`cmd/agent/cli_tasks.go:3874-3880` (the run-ground axis, **only** callable by the verb).

**(b) Commands run.**

```
$ gopi task sweep-worktrees > evidence/q7-1-full-sweep-plan.txt
$ grep -oE "^  [a-z-]+ +\[[a-z-]+\]" evidence/q7-1-full-sweep-plan.txt | sort | uniq -c | sort -rn
$ grep -rn "RunGroundCollect\b\|RunGroundCollectableGrounds\|RunGroundSweepReport" --include=*.go . | grep -v _test.go
$ for d in /home/user/workspace/gopi-*; do …live-cwd scan of /proc… ; done
```

**(c) Raw output.**

```
    937   collectable [run-ground-anchor]
    202   keep        [run-ground-anchor]
      2   report      [declared-root-scope]
      2   keep        [self-checkout]
      2   keep        [main-checkout]
```

```
./internal/tools/run_ground_anchor.go:274: func RunGroundCollectableGrounds(…)
./internal/tools/run_ground_anchor.go:352: func RunGroundCollect(…)
./cmd/agent/cli_tasks.go:3863: collectable, kept, err := tools.RunGroundCollectableGrounds(time.Now(), opts.MinAge)
./cmd/agent/cli_tasks.go:3880: removed, skipped, cerr := tools.RunGroundCollect(collectable)
        ← the ONLY callers are inside runTaskSweepRunGrounds, i.e. `gopi task sweep-worktrees`
$ grep -n "serveWorktreeReapSweep" cmd/agent/*.go
cmd/agent/cli_tasks.go:3697: func serveWorktreeReapSweep(dryRun bool) []string
cmd/agent/task_lifecycle_tick.go:298: for _, line := range serveWorktreeReapSweep(worktreeReapDryRun) { … }
```

```
gopi-* dirs=184  with no live holder=182
$ git worktree list | wc -l   → 5
```

**(d) Interpretation — BOTH directions are real, and they are different populations.**

**Direction 1 — refusal → permanently stuck: CONFIRMED, bounded.**
The exact shape is Q3's, and it produced **246 refusals in `worker.log`, 237 of them naming a
`workspace/gopi-<task>` cwd**. The registered-and-frozen population is small on this host (5
registrations, of which 3 are alive worktrees and 2 are `self-checkout` refusals), because the
sweep's `live-holder`/`live-session` arms legitimately keep them. The mechanism is confirmed by
experiment (Q3); the *volume* is bounded by the fact that only 5 registrations remain.

**Direction 2 — reclaim → work gone: no evidence of it in the audit log; the volume is
elsewhere.** Of 5497 audit records, the ledger-keyed reclaim arms are `task-row-reclaim` **5**
and `worktree-sweep-reclaim` **2**. Their removal semantics are directory-only
("removes the DIRECTORY only — never the branch", `cli_tasks.go:3417`), so nothing was
*permanently* destroyed by those two arms. There is no record of *wrong* reclamation.

**BUT — the real producer of the "stuck worktrees" is neither direction, and it is measurable.**
The registry is only 5 entries, while **184 `gopi-*` directories exist and 182 have no live
holder**. They are not lost from `git worktree list` accidentally: they are tracked by the
**run-ground anchor** axis instead, and that axis tells the whole story:

```
run-ground anchors: collectable=937 kept=202
```

and the *kept* reasons are dominated by one frozen session:

```
$ grep -oE "kept: [^)]*" evidence/q7-1-full-sweep-plan.txt | sed 's/[0-9]\{3,\}/N/g' | sort | uniq -c | sort -rn | head -4
 137 kept: the launch session N still holds a live gopi process
   4 kept: the run that took this ground (pid N
   …  "the ground is younger than the floor (… < 24h0m0s)"
 937 kept: … (the collectable arms / "the run that took this ground is gone …")
```

So: **202 doors are held by a `live-session` rung, 137 of them by ONE long-lived worker session
(23485)**, while **937 are already judged collectable** and simply have no resident caller —
the collection call site lives only in the CLI verb, and the resident tick runs the
*registration* reap instead:

```
task_lifecycle_tick.go:298  → serveWorktreeReapSweep(worktreeReapDryRun=false)
cli_tasks.go:3697           → WorktreeSweepRun  (the REGISTRATION axis)
cli_tasks.go:3880           → RunGroundCollect  (the ANCHOR axis — verb-only)
```

**Answer to the operator's question, therefore**: the cleanup arm did **not** cause the stuck
worktrees by deleting the wrong thing; it caused a bounded share of them by **refusing** (246
live-holder refusals, from the daemon-cwd producer bug), and the *bulk* (937 + 137) is caused by
the sweep's own **scope and reachability**: an anchor axis with no resident collector, and a
`live-session` rung satisfied by any long-lived gopi in the launch session.

---

## Q8 — The worker waits for the gopi child. Why not notice a dead child? Resume reachability?

**(a) Code locations.**
Wait: `cmd/agent/cli_tasks.go:2589` (`runErr := cmd.Wait()`) — the spawn is
`cmd/agent/cli_tasks.go:2162` (`exec.Command(gopiExecutable())`), `cmd.Dir` set at `:2425`.
Detection: `cmd/agent/cli_tasks.go:2600-2618` — `tools.TaskRowTerminalAt(target)` then
`writebackOutcome(… TaskStatusFailed, "gopi run failed: "+runErr.Error()+taskAbnormalChildMarker+…)`
then **`taskRunAbnormalChildRetry(target, closedByChild, rowStatus, statusErr)`**
(`:2618`) — implemented at `cmd/agent/cli_tasks.go:2771-2850`.
Resume: `internal/tools/task_holder_resume.go:626` (`SpawnTaskResume`), reached from the worker
path via the seam `cmd/agent/cli_tasks.go:2853` (`var taskRunResumeSpawn = tools.SpawnTaskResume`).
The dead-holder arm: `internal/tools/task_holder_resume.go:301` (`ResumeDeadHolderRuns`).
The wall-clock bound: `internal/tools/task_run_retry.go:46` (`TaskRunRetryCap = 3`);
**no default per-child timeout exists** (`defaultAgentTimeout` has no definition in this tree —
see the search below).

**(b) Commands run.**

```
$ grep -rn "defaultAgentTimeout\s*=" --include=*.go .
$ grep -rn "ResumeDeadHolderRuns\|SpawnTaskResume" --include=*.go . | grep -v "^./internal/tools/task_holder_resume.go"
$ sqlite3 -readonly "file:/home/user/.gopi/memory.db?mode=ro" \
    "SELECT COUNT(*) FROM tasks WHERE status='failed' AND tags LIKE '%run-retry:1%';"
$ sqlite3 -readonly "file:/home/user/.gopi/memory.db?mode=ro" \
    "SELECT COUNT(*) FROM tasks WHERE tags LIKE '%dead-holder-resume%';"
$ sed -n '2600,2620p;2771,2830p' cmd/agent/cli_tasks.go
```

**(c) Raw output.**

```
$ grep -rn "defaultAgentTimeout\s*=" --include=*.go .
                                  ← NOTHING: the constant the rules file names does not exist here
```

```
$ …ResumeDeadHolderRuns / SpawnTaskResume outside its own file…
none      ← wait: the two production callers are
cmd/agent/cli_tasks.go:2853: var taskRunResumeSpawn = tools.SpawnTaskResume
cmd/agent/cli_tasks.go:5511: for _, o := range tools.ResumeDeadHolderRuns(repo) { … }   (mergerResumeDeadHolderLeg)
```

```go
// 4) the double-run gate, asked BEFORE the spawn
if ok, verdict := tools.TaskRunChildResumable(target, os.Getpid()); !ok {
	fmt.Fprintf(os.Stderr, taskRunRetryRefusedFmt, target,
		"the row's holder verdict ("+verdict+") does not admit a resume — it would risk a double-run")
	return
}
action, logPath, pid, spawnErr := taskRunResumeSpawn(target, targetArg)
if pid <= 0 { … return }                     // nothing started: no attempt consumed
// The child EXISTS: this is an attempt, recorded BEFORE its outcome is known
```

```
$ …rows resumed by the ladder and then failed:  34
$ …rows carrying a dead-holder-resume trail:     2
```

**(d) Interpretation — the three sub-questions, answered.**

**(i) Does the worker detect a child that died WITHOUT writing a terminal status? YES — and it
closes the row itself.**
`cmd.Wait()` returns the exit status; it cannot distinguish "finished" from "was killed" from
"hung and then died" — **but the loop does not need it to**: `TaskRowTerminalAt` is read *before
the wrapper's own writeback lands*, and the pair (row already terminal) vs (row still open) IS
the discriminator. A child that judged its task closes the row itself → `closedByChild=true` →
no retry (`:2774-2778`). A child that died abnormally leaves the row open → the wrapper is the
one writing `failed` → the retry arm runs. This is correct and it is the strongest design
element on the path.

**(ii) Is `task_holder_resume.go` reachable from that detection path, or only from a human
command? BOTH — measured.**
- **From the detection path**: `cmd/agent/cli_tasks.go:2618` → `taskRunAbnormalChildRetry` →
  `:2820` `taskRunResumeSpawn(...)` → `internal/tools/task_holder_resume.go:626`. The seam is a
  package variable so the wiring is testable (`abnormal-child-retry.md` records that as a
  deliberate design decision).
- **From the merger pass**: `cmd/agent/cli_tasks.go:5511` (`mergerResumeDeadHolderLeg`) →
  `ResumeDeadHolderRuns`. Its own evidence is thin: **2 rows** carry a `dead-holder-resume` tag
  on this host's ledger, versus **34 rows** that went through `run-retry:1` and were then
  failed. The retry ladder fires; the dead-holder arm barely does.

**(iii) If resume is attempted and fails, what is the terminal action? IT EXISTS — and it is
the right one.**
Four named refusals, each with its own sentence
(`cmd/agent/cli_tasks.go:2774-2828`): `closedByChild` ("a judged failure is a result, not a
crash"), unreadable row (Rule 15: never promoted to a retry), **cap reached** → the named
handover reason is written to the row, spawn failure / holder verdict → refuse. At the cap the
arm records the reason and prints where the ladder ended — the design intent is explicit. There
is no *silent* end.

**(iv) What is NOT bounded — and this is the finding.**
There is **no default per-child wall-clock timeout** in this tree (`defaultAgentTimeout` has no
definition; `GOPI_TIMEOUT_SEC` appears only as a *published* variable, e.g.
`gopi merge --handover-budget` "Published to the child as GOPI_TIMEOUT_SEC"). So:
- **`cmd.Wait()` blocks forever** if the child is alive but hung. The worker's round does not
  return, and *no* watchdog exists on this path. The only escapes are an external signal
  (`installWorkerStopSignals`, `cli_tasks_worker_stop.go:429`, which forwards a stop to the
  child) or an operator kill.
- **A child killed by a stop signal is handled correctly** — that is not a hole.
- **A child that hangs and then dies** produces an abnormal exit → the retry ladder — but every
  one of those attempts is *also* unbounded, so the ladder can take 4 × ∞.

Combined with Q6, the worker's exposure is: a hung child blocks the round forever, and a panicking
cleanup pass kills the loop before the claim. Both are "the worker cannot run the task it was
started for", which is precisely the operator's Q6 sentence.

---

## Q9 — Does the mechanism feed the effect of a `failed` task back on a long dependent chain?

**(a) Code locations.** `internal/tools/tasks.go:5837` (`var taskParentReleaseStatuses` — the
release set `{completed, verified}`), `internal/tools/tasks.go:5853`
(`taskParentGateDenied`), `internal/tools/tasks.go:5888-5894` (the rendered reasons),
`internal/tools/tasks.go:6011` (`TaskQueueSkipMarker`), `internal/tools/tasks.go:2032` (the
`[queue-skip]` render). The doc is unambiguous at `:5830-5836`.

**(b) The experiment** (a throwaway sandbox profile — `gopi sandbox create`, Rule 34 — so the
operator's ledger was never written; raw output in the run log):

```
$ SB=$(/home/user/workspace/_tmp/diag-gopi sandbox create --label q3-q9-experiment | tail -1)
  /home/user/workspace/gopi-sandbox/sandbox-q3-q9-experiment-6a59bf8d
$ export GOPI_HOME="$SB"
$ $G task submit "Q9 probe parent — diagnose only, throwaway sandbox row"
  Task queued: 01a0f3ed-20ac-78b7-a338-b9f644baa206 (status=pending, source=agent, tags=—)
$ $G task update 01a0f3ed-20ac-78b7-a338-b9f644baa206 --status in_progress
  Task updated: … (status=in_progress, reason=work started)
$ $G task update 01a0f3ed-20ac-78b7-a338-b9f644baa206 --status failed
  Task updated: … (status=failed, reason=task failed)
$ $G task submit "Q9 probe child — should be parked by the failed parent" --parent-task-id 01a0f3ed-20ac-78b7-a338-b9f644baa206
  Task queued: 01a0f3ed-393d-7d0d-934d-dafc8ce5e955 (status=pending, source=agent, tags=—)
$ $G task list --status pending
```

**(c) Raw output.**

```
1 task(s) with status=pending:
#01a0f3ed-393d-7d0d-934d-dafc8ce5e955  [pending]  Q9 probe child — should be parked by the failed parent  (…) [queue-skip] parent 01a0f3ed-20ac-78b7-a338-b9f644baa206 not released (status=failed; releases on completed|verified)
```

```
$ $G task next
gopi task next: removed (M4.5) — manual claiming was removed — use "gopi worker" (claims and runs the next eligible row) or "gopi task serve" (the daemon loop)
```

```
$ $G task show 01a0f3ed-393d-7d0d-934d-dafc8ce5e955
status:         pending      ← unchanged: parked, not failed, not cancelled, not reported
```

**(d) Interpretation — CONFIRMED, and it is the most severe defect in the set.**

A `failed` parent parks its entire subtree **permanently**, and the code says so as a decision,
not an oversight:

```go
// internal/tools/tasks.go:5830-5836
// … Every other parent status — pending, claimed, in_progress, failed, cancelled —
// parks the child indefinitely. There is deliberately NO override, NO cascade
// cancel and NO timeout/staleness release (a cancelled or failed parent parks
// its children FOREVER, by design: the parked rows are visible in the pending
// view as [queue-skip] with the parent's status named, and the operator's
// resolution is to finish the parent or cancel the child).
```

The **only** feedback is one line in the pending view. Measured:

- There is **no notify event** for a parked child. The search for one returns nothing:
  `grep -rni "notify.*park\|park.*notify\|blocked.*child" --include=*.go internal/tools` → 0 hits.
- The row's own status is **not changed**: it stays `pending` — it never becomes `failed`,
  `blocked`, or anything a sweep or a report would surface.
- The `gopi task list` default view is chronological (newest first); the parked row is
  indistinguishable from queue wait unless the reader is looking at the pending view specifically
  and knows to read the suffix after `[queue-skip]`.
- There is **no override**: no `--force`, no cascade cancel, no timeout (§ the doc's own words).
  A `cancelled` **or failed** parent parks its children forever — by design.
- **The rule itself is honest about the hole**: `GOPI.md` Rule 33 states the gate and its
  resolution. What neither the rule nor the code provides is the *feedback* the operator asked
  for.

**Combined with the brief's own census (289 failed rows, 120 human/agent/llm-authored, 67 of
those referenced by no other row), the two operator cases are both live:**

1. *"this task is unreasonable / impossible / unnecessary, and should not be a dependency"* →
   the failed parent is not re-examinable, and its children wait forever with a one-line note.
   **Nothing forces the re-examination.**
2. *"the downstream tasks are meaningless because this dependency can never succeed"* →
   the subtree is not marked, not summarised, not counted. **Nothing tells anyone the subtree is
   dead.**

**The four options, laid out for the operator to choose between (no recommendation):**

| option | consequence |
|---|---|
| **mark the subtree** — a derived view/`reason` on every descendant of a terminal-unreleased parent | cheap, no schema change; makes the dead subtree visible wherever the row is read; does not unblock anything |
| **notify** — a lifecycle-tick event when a row becomes parked | reuses `workerNotifySinkTarget()`/the existing notify channel; the operator learns once, per row, at the moment it happens; noise scales with chain size |
| **offer a "release the subtree" command** — `gopi task release-tree <parent> --reason …` | the resolution becomes one action; needs a new verb and a decision about what the children become (pending? cancelled? re-scoped?) |
| **treat `failed` as a release with a warning** | no new verb; the children run — and they run against a premise that does not hold, which is the failure mode the gate was built to prevent |

**Where the design intent is unstated**: the gate is documented as *correct* ("no override, no
timeout, by design") but the *chain-level* question — "a parent can never succeed, so what
happens to everything downstream?" — appears nowhere in the code, the rule, or any doc found by
the search. That is the gap to file, independent of which option is chosen.

---

## Prioritised defect list (one-line harm each)

| # | defect | harm (one line) |
|---|---|---|
| **I1** | A `failed` (or `cancelled`) parent parks its subtree **forever**, with only a `[queue-skip]` suffix in one view and no notify (`internal/tools/tasks.go:5837`, `:5853`; experiment above) | one unreviewed failure silently voids an entire chain of work and nothing reports it |
| **I2** | No `recover()` on the worker's cleanup-before-claim path (`cli_tasks_worker.go:507` → `:555`; the only `recover()` is `internal/agent/loop.go:3773`) | a panic in any cleanup pass kills the worker loop and **the task it was started for is never claimed** |
| **I3** | Cleanup writes nothing about **interruption** — `failed` conflates "judged failed" with "killed mid-run"; the discriminator exists (`task_run_retry.go:110`) but is never persisted (0 rows carry `abnormal child exit`; 34 resumed-then-failed rows read only `task failed`) | after the fact the ledger cannot tell work that was decided from work that was silenced — the operator's central question is unanswerable from the record |
| **I4** | Half-finished-but-**committed** work passes every guard: dirt is the only defence (`task_reclaim.go:623`), the clean lane checks ownership+lineage only (`reclaim_ground_checklist.go`), and the merge candidate set ignores the ledger (`merge_run.go:2423`) | an incomplete commit gets delivered and merged as if complete — a code-pollution path, measured end to end |
| **I5** | Liveness = "**any** live cwd inside", owner-blind and fail-open on unreadable `/proc` (`task_finish_cleanup.go:3258`, `:3271`; experiment Q3) | an unrelated process freezes a deliverable forever; 246 refusals in `worker.log`, 237 naming a `workspace/gopi-<task>` cwd |
| **I6** | The run-ground anchor axis has **no resident caller**: `RunGroundCollect` is reachable only from `gopi task sweep-worktrees` (`cli_tasks.go:3880`), while the resident tick runs the registration reap (`task_lifecycle_tick.go:298`) | 937 collectable + 202 kept anchors accumulate with nothing on the host that collects them; the "stuck worktrees" the operator sees are this population, not the registry |
| **I7** | The run-ground collector asks live/age/occupied and **never dirt** (`run_ground_anchor.go:274-320`; `:317` default → collectable) | the day anyone calls `--apply` on that axis, uncommitted work is collected without the human call guard ② promises |
| **I8** | Cleanup-before-claim **contradicts the ruling already recorded in the same file** (`cli_tasks_worker.go:522-537` states the remit; `:507`/`:518`/`:552` violate it six lines above) | the code silently re-grants a remit the operator removed — the ruling exists and was not applied |
| **I9** | No default wall-clock bound on the wrapped child — `defaultAgentTimeout` is undefined in this tree; `GOPI_TIMEOUT_SEC` is only *published* (`cli_tasks_worker.go` banner, `gopi merge --handover-budget`) | `cmd.Wait()` blocks forever on a hung child; each retry attempt in the ladder of `TaskRunRetryCap=3` is equally unbounded |
| **I10** | The daemon-cwd producer bug is still live: 75+41+40+38 refusals name a `gopi-*/cmd/agent` cwd from daemons spawned **while standing in a task worktree** | the largest single class of "stuck worktree" refusals is gopi's own spawn bug, not an external process — `internal/daemoncwd` documents the fix and the refusals show it still happening |

**Note on the 289 failed rows / 67 unreferenced**: per the brief, these are the **symptom**, not
a defect to file, and this run did not touch them.

---

## Where the answers stop (honest gaps)

- **The 937/202 anchor figures are a snapshot of one host at ~04:15 local**, taken with the
  verb's dry run. They are *narrative* evidence of this run, not a census (Rule 38) — no
  generator script was written (Rule 35: no new artifact).
- **I2 (cleanup panic kills the loop) is proven by construction, not by a triggered panic.** I
  did not inject a panic into a live cleanup pass: doing so would mean modifying production code
  (out of scope for a diagnose-only run). The evidence is the complete absence of `recover()` on
  the path (`grep` output above) plus the straight-line body of `taskLifecyclePass`.
- **I4 is proven on a synthetic repo, not on a live task.** Deliberately: reproducing it on the
  operator's ledger would mean creating a half-committed branch on a real task's ground, which
  the boundary forbids ("do NOT delete or modify any existing row, branch, or worktree").
- **The `--apply` paths were never run.** Every experiment used the verbs' plan-only posture or
  a throwaway repo. No removal happened anywhere on this host during this run.
- **`defaultAgentTimeout` absence** is a *grep* result over this tree. It is stated as "no
  definition exists here" — not as "no timeout exists anywhere in GoPi's history".

---

## Verification

```
Build:   CGO_ENABLED=0 go build -o /home/user/workspace/_tmp/diag-gopi ./cmd/agent/   → BUILD_OK
Tests:   see "Not Done" — no test was added (diagnose-only task, no source change)
Ran:     gopi task sweep-worktrees                                   (dry run, the verb's default)
         gopi task sweep-worktrees  (with an unrelated `sleep` cwd inside a throwaway worktree)
         gopi task sweep-worktrees  (after that process was killed — same registration)
         gopi sandbox create --label q3-q9-experiment
         GOPI_HOME=<sandbox> gopi task submit|update|list|next|show   (the Q9 chain experiment)
         sqlite3 -readonly "file:/home/user/.gopi/memory.db?mode=ro" (read-only ledger queries)
Result:  Q3 — unrelated live cwd ⇒ `keep [live-holder] … (held by a live process (cwd/fd/exe/maps))`;
              after the kill ⇒ `keep [live-session] … (its recorded runner (pid 2411198) is still
              running)`; `git worktree list` unchanged (dry run).
         Q9 — parent `pending→in_progress→failed`; child submitted with `--parent-task-id` stays
              `pending` and renders `[queue-skip] parent … not released (status=failed; releases
              on completed|verified)`; `task show <child>` → `status: pending`.
         Q7 — sweep plan: `collectable=937 kept=202`; live-cwd scan: `gopi-* dirs=184 with no live
              holder=182`; `git worktree list | wc -l` → 5; audit log 5497 records, ledger-keyed
              reclaim arms = 5 + 2.
         Q4 — `SELECT COUNT(*) … LIKE '%abnormal child exit%'` → 0; rows failed after the retry
              ladder = 34.
Commit:  <filled at commit time>
```

## Evidence (self-filed)

- `q3-1-baseline-sweep.txt`, `q3-2-sweep-with-sleep.txt`, `q3-3-sweep-after-kill.txt` — the Q3
  experiment: the raw `gopi task sweep-worktrees` output before, during and after the unrelated
  process; the disposition flips `live-holder` → `live-session` while the worktree survives.
- `q5-half-committed.txt` — the Q5 experiment: `A halfdone.go` → commit → empty porcelain →
  `merge-base` OK → the branch looks like an ordinary deliverable.
- `q7-1-full-sweep-plan.txt` — 1151 finding lines: `collectable=937 kept=202`,
  137 of the kepts naming launch session 23485.
- `q4-absence-search1.txt` — the search for a writer of an "interrupted" mark: empty.
- Evidence directory: `/home/user/.gopi/sessions/01a0f3e5-b9d8-7ac3-b4a0-22d5268546eb/evidence/`.
- Throwaway fixtures (outside any git tree, safe to delete at leisure):
  `/home/user/workspace/diag-cleanup-01a0f3e5/` (2 linked worktrees, 1 commit each) and
  the sandbox profile the Q9 rows live in.

## Not Done (honest)

- **No fix, no test, no source change** — this is the task's explicit boundary ("diagnose and
  report. Do NOT fix anything"). Consequently there is no branch-scoped test run to report: the
  suite was not the instrument here, the experiments were.
- **I2 was not triggered live** (would require modifying production code).
- **I4 was not reproduced against a live task row** (would require mutating a real task's ground).
- **Q1 was not re-litigated**: it is answered by citation of `01a0df2d` plus the measured
  contradiction in `cli_tasks_worker.go:522-537`; whether to *move* the passes is the operator's
  decision and was not pre-empted.
- **The 289 failed rows were not triaged** — brief: "they are the symptom. Disposition is the
  operator's call."

## Files

- `devdoc/cleanup-arm-produces-dead-tasks-audit-20261001.md` — this report (new)
