<!-- doc-class: historical -->
# Resume policy after an abnormal end — the decision table

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Branch `task/01a0f3f2476c78fe9efebc3f3e4a91bf`. Reads `internal/runend/runend.go`
(the reason vocabulary), `cmd/agent/cli_tasks.go` (the `task run` wrapper's
close-out tail), `internal/tools/task_run_retry.go` + `internal/tools/task_holder_resume.go`
(the two bounded ladders), `cmd/agent/cli_resume.go` (the operator face).

This is a DESIGN document. Nothing here is implemented by this branch.

---

## 1. Executive summary — the briefing's premise is FALSE

The briefing asserts: *"Nothing on the worker/close-out path calls it [resume].
Verify that claim by search and paste the empty result."*

That search does **not** come back empty. **Three bounded automatic resume arms
exist on master today.** "Nothing auto-resumes" was true when `runend` was
written (its own comment at `runend.go:145-146` still says *"nothing
auto-resumes (the operator decides)"* — that sentence is now stale prose), but
two ladders were built beside it afterwards.

The honest answer to the operator's question 「如果发现非正常退出，是否 resume 呢？」
is therefore **not** "no, we never do" and **not** "yes", but:

> **Three arms already resume automatically, each on its own trigger, each with
> its own cap — and every single one of them is blind to the engine-forced ends
> that `runend` was built to name.**

That last clause is the finding this document is about. It converts the design
question from "should we add auto-resume?" into **"which ends fall through the
arms that already exist?"**

---

## 2. The search (verbatim, with output)

### 2.1 The briefing's claim, tested

```
$ grep -rn "autoResume\|auto_resume\|AutoResume\|resumeTask\|ResumeTask" --include=*.go .
./cmd/agent/cli_resume.go:221:// ADOPT a row — see resumeTargetRowID. ...
./cmd/agent/cli_resume.go:236:	if raw, st, ok := resumeTaskRawLog(target); ok && raw != "" {
./cmd/agent/cli_resume.go:266:// WHY IT EXISTS (the ledger-context fallback): resumeTaskRawLog answers through
./cmd/agent/cli_resume.go:320:// resumeTaskRawLog resolves the raw-log checkpoint of a task row ...
./cmd/agent/cli_resume.go:326:func resumeTaskRawLog(target string) (rawLog string, row *tools.TaskProcessStatus, ok bool) {
...
```

The literal token `autoResume` is absent — **but that is a naming search, not a
behaviour search**, and taking its emptiness as proof is exactly the mistake the
briefing invites. The behaviour lives under a different name. The correct probe
is the **spawn seam**:

```
$ grep -rn "func SpawnTaskResume" --include=*.go internal/tools/ | grep -v _test
internal/tools/task_run_retry.go:290:var taskRunResumeSpawn = tools.SpawnTaskResume
internal/tools/task_holder_resume.go:626:func SpawnTaskResume(taskID, target string) (action, logPath string, pid int, err error) {

$ grep -rn "ResumeDeadHolderRuns(" --include=*.go internal/ cmd/ | grep -v _test
internal/tools/task_holder_resume.go:301:func ResumeDeadHolderRuns(repo string) []deadHolderResumeOutcome {
cmd/agent/cli_tasks.go:5511:		for _, o := range tools.ResumeDeadHolderRuns(repo) {
```

**ARM 1 — the merged dead-holder ladder.** `cmd/agent/cli_tasks.go:5511` calls
`tools.ResumeDeadHolderRuns(repo)` *"wired into the merger pass"*
(`internal/tools/branch_attribution.go:215`). It spawns
`gopi resume <target> --task <task-id>` detached, with an ownership handoff
(`SpawnTaskResume`, `task_holder_resume.go:626-644`). Cap: `deadHolderResumeMaxAttempts = 3`
(`task_holder_resume.go:66`), counted as an **append-only ordinal tag**
`dead-holder-resume:<n>` so a crash between two spawns cannot outrun its own cap.
Its row selection is the key fact (§3.3).

**ARM 2 — the wrapper's abnormal-child ladder.** `cmd/agent/cli_tasks.go:2623`
calls `taskRunAbnormalChildRetry(...)`, whose spawn seam is
`var taskRunResumeSpawn = tools.SpawnTaskResume` (`task_run_retry.go:290`).
Cap: `TaskRunRetryCap = 3` (`task_run_retry.go:46`), counted as the append-only
tag `run-retry:<n>` — a *second, deliberately separate* counter namespace
(`task_run_retry.go:29-32`: sharing a prefix "would let one arm's attempts consume
the other's budget").

**ARM 3 — the worker's claim-arm resume.** `cmd/agent/cli_tasks.go:1911` prints
`"worker: resumed claimed task %s"` — a claimed row with no live holder is
re-claimed and run again by the worker round. This is a resume in the
operational sense (the row's work is re-entered), reached by re-claiming rather
than by the `resume` verb.

```
$ grep -rn "TaskResumeCount(" --include=*.go . | grep -v "_test.go:"
./internal/tools/tasks.go:6439:func TaskResumeCount(taskID string) int {
```

**A further gap, and the reason Arms 1–3 are not "handled":** `TaskResumeCount`
(`tasks.go:6434`) is *defined and unit-tested but has ZERO production callers*.
The `resumed:<session-id>` tag convention the briefing asks to "verify" exists
and is stamped (`cli_resume.go:463`, `tasks.go:6121` `taskTagResumedPrefix`,
rendered by `tasks.go:3182` as `resumed: ×N`) — but **nothing bounds on it**. The
attempt cap on any arm that goes through `SpawnTaskResume` is the arm's own tag
namespace, not a global resume count. So there is no single ledger fact that
answers "how many times has this row already been resumed, by anyone".

### 2.2 The end-reason read, and where it is available

```
$ grep -rn "func RunEndReasonFor" -A 8 --include=*.go internal/tools/
internal/tools/tasks.go:3664:func RunEndReasonFor(taskID string) runend.Reason {
```

Already defined on the ledger, and already consumed at the wrapper's writeback:

```
$ grep -n "runEnd := tools.RunEndReasonFor" cmd/agent/cli_tasks.go
3089:	if runEnd := tools.RunEndReasonFor(taskID); runEnd.Truncated() && status == tools.TaskStatusCompleted {
```

**This is the crucial composition fact:** the reason-aware read the design needs
already exists, is already called in the wrapper's failure+success path, and is
*not* called by the retry arm sitting five lines below it (`cli_tasks.go:2623`).
The two mechanisms are in the same function, one above the other, and the second
does not consult the first.

---

## 3. The decision table

Every row: **where the reason is produced → what survives → what a resume
re-enters → is resume sound → who acts → cap.**

The `runend` vocabulary is `runend.go:36-61`. `Truncated()` (`runend.go:95`)
returns true for the five engine-forced reasons and false for `Finished`
(the zero value, `runend.go:36`).

### Row 0 — `Finished` (not an abnormality; the control row)

| | |
|---|---|
| **Produced** | `runend.go:36` `Finished Reason = ""` — the zero value. Set at `loop.go:3763` and left alone when the model ends the run normally. |
| **Survives** | Everything: session raw log, worktree, branch, row `completed` (via the ordinary exit-code mapping). |
| **A resume re-enters** | A finished conversation. Pointless, and the CLI refuses a terminal row. |
| **Sound?** | **No — and it is not reachable.** Not an abnormal end. |
| **Who acts** | Nobody. |
| **Cap** | n/a. |

### Row 1 — `WallclockCap` (`wallclock-cap`)

| | |
|---|---|
| **Produced** | `runend.go:43`. Two sites, both in the loop's round boundary/round-trip handling: `internal/agent/loop.go:3949` (cap expired at the round boundary) and `internal/agent/loop.go:4120` (cap expired *during* an LLM round-trip). Env knob `GOPI_SESSION_MAX_WALLCLOCK` (`loop.go:413`; `0` = unlimited). |
| **What survives** | **Everything, and this is the best-surviving case in the table.** The loop returns `lastSubstantiveText(a), nil` (`loop.go:3950`) — **a nil error** — so the session closes normally: the raw log holds the full conversation, the worktree and branch are untouched, the row is stamped `failed` + `TRUNCATED run (wallclock-cap)` by the child's end hook, and `runend.RecoveryHint` (`runend.go:147`) is appended telling the operator the row is recoverable. |
| **What a resume re-enters** | `gopi resume <task-id>` → `TaskResumeTarget` (`internal/tools/taskproc.go:888`) resolves the checkpoint from the row's **run directory** (`sessions/<task_id>/`, newest run with a raw log — deliberately not the `session_id` column; three measured reasons in its doc comment at `taskproc.go:866-884`) → the full context is replayed → `resumeChdirToWorktree` (`cli_resume.go:534`, impl `:915`) chdirs back into `deliverable.worktree`. The work is genuinely unfinished and the context intact. |
| **Sound?** | **Yes — the prime candidate.** This is the operator's 「killed run = interrupted, to be resumed」 case: the engine hit a ceiling, nothing judged the work. |
| **Who acts** | Operator today. **This is the reason Option B exists** — it is the one reason with no verdict behind it. |
| **Cap** | None today. `TaskResumeCount` is defined but read by nothing (§2.1), so unbounded by the ledger. Any auto-arm must bring its own cap. |

### Row 2 — `MaxIterations` (`max-iterations`)

| | |
|---|---|
| **Produced** | `runend.go:61`; site `internal/agent/loop.go:4550`, immediately before `return "", fmt.Errorf("max iterations (%d) reached", a.MaxIterations)`. |
| **What survives** | Same as Row 1 — raw log, worktree, branch, `failed` + marker. **One difference: the error is NON-nil**, so this run does *not* present as a "graceful nil" exit. |
| **What a resume re-enters** | Same as Row 1. |
| **Sound?** | **Yes, conditionally** — the brief asks "verify it is a ceiling and not a symptom". It is a genuine ceiling, but unlike the wallclock cap it is *not* an environment boundary in production: `MaxIterations` is 0 = unlimited in production (`runend.go:59-61`, "the loop ran out of rounds with the task still running"), so hitting it means the iteration budget was **explicitly configured** and the run burned it. Resuming re-enters with a *fresh* budget — which makes it the row most likely to burn budget twice. Sound, but it earns the cap more than Row 1 does. |
| **Who acts** | Operator today. |
| **Cap** | None. |

### Row 3 — `StuckLoop` (`stuck-loop`)

| | |
|---|---|
| **Produced** | `runend.go:47`; site `internal/agent/loop.go:4153`, guard `if stuck.tripped()` — threshold `maxStuckRounds = 5` (`loop.go:291`), N consecutive no-tool rounds with an identical text prefix (incident t-1788173679). Returns a non-nil error (`loop.go:4154`). |
| **What survives** | Raw log (full), worktree, branch, `failed` + marker. |
| **What a resume re-enters** | **The same conditions.** The checkpoint is a conversation in which the model produced N consecutive rounds with no tool call and the same text. That context *is* the stall. |
| **Sound?** | **Suspect — do not auto-resume (brief's explicit ban).** The predicted outcome is a second full run arriving at the same stall. The `RecoveryHint` text ("recoverable: `gopi resume …`") is appended to this row by the generic path (`runend.go:147`, called from the writeback at `cli_tasks.go:3090`) and is, for this reason, **an over-promise** — see §7. |
| **Who acts** | Operator, deliberately. |
| **Cap** | n/a — banned from automation. |

### Row 4 — `StuckToolLoop` (`stuck-tool-loop`)

| | |
|---|---|
| **Produced** | `runend.go:51`. **TWO sites**: `internal/agent/loop.go:4172` (identical `tool_use` — same name + normalised param hash + text; bug055 / Bond 718319) and `internal/agent/loop.go:4434` (identical **poll results** — the frozen-polled-state net). It fires through the same notify channel as the other stuck nets. |
| **What survives** | Same as Row 3. |
| **What a resume re-enters** | Same as Row 3, *unless* the trigger was a legitimate poll. The brief asks to check the polling whitelist: the detector **does** have one — `pollingTools` / `observeResults` (`loop.go:2691`), added after tool-polling false positives, and the toolStreak state is fed at the same single point as the other detectors so no `continue` path bypasses it. **Consequence: the whitelist makes the detector less likely to fire on a legitimate poll, but it does not make a resume sound for the cases that DO fire** — the whitelist governs whether the *first* run was wrongly stopped, not whether re-entering its context is a good idea. A run that genuinely froze on `bash` in a poll loop will freeze again. |
| **Sound?** | **Suspect.** Same argument; the whitelist is not a licence to resume. |
| **Who acts** | Operator. |
| **Cap** | n/a. |

### Row 5 — `StuckObservation` (`stuck-observation`)

| | |
|---|---|
| **Produced** | `runend.go:57`; site `internal/agent/loop.go:4194`, guard `if observation.tripped()`. Fed at the same single point (`loop.go:4191`). The shape: rounds whose tool calls are *all* observers (`read`/`code_search`/`bash`/…) and never a mutator (`write`/`edit`/`task`/`memory`/…); `job` is excluded because detector ② owns polling. |
| **What survives** | Same as Row 3. |
| **What a resume re-enters** | The same look-without-act loop. |
| **Sound?** | **Suspect — the most suspect of the three.** The detector names the loop shape structurally: the run re-enters a conversation whose entire character is "observe, never act". There is no ceiling that expired and no crash; the context itself produced the ending. |
| **Who acts** | Operator. |
| **Cap** | n/a. |

### Row 6 — the ABNORMAL-DEATH case (no `runend` value exists)

This is the case with **no reason row at all**, which is precisely why task
`01a0f3ef` exists ("can we detect it"). The brief's own table says resume is
*"plausible but only after detection works"*.

| | |
|---|---|
| **Produced** | **Nowhere.** `endReason` is a local of the loop (`loop.go:3763`) and is threaded into the close-out only when the loop *reaches* its end hook — `a.finishSessionTaskRecorded(..., endReason)` at `loop.go:3861`. A child SIGKILLed (or crashing) mid-round never executes that line, so **the row carries no reason, no `run-end` trail note, and no `TRUNCATED` marker**. Its `endReason` is lost with the process. The wrapper's only evidence is the exit status. |
| **What survives** | Whatever was on disk at the kill instant: the session raw log **up to the last flushed write** (partial — this is the asymmetry with Rows 1–5, where the log is complete), the worktree with its commits, the branch, and a row still `in_progress`/`claimed` — because the `failed` writeback is the *wrapper's*, and the wrapper reads the row **before** writing it (`cli_tasks.go:2607`). |
| **What a resume re-enters** | `TaskResumeTarget` on a row whose run directory holds a raw log. If the log was flushed, a real checkpoint exists and context is recovered; if the run died before its first flush, `TaskResumeTarget` returns an empty path — **"a definite absence, reported as such rather than papered over"** (`taskproc.go:897-900`). Rows that cannot be resumed at all are real, and §4.3 handles them. |
| **Sound?** | **Yes — this is Arm 2's case, and Arm 2 already covers it.** The discriminator is `TaskRowTerminalAt` (`task_run_retry.go:110`): a row still OPEN at the exit instant means the child died without judging its task, so resuming recovers work rather than repeating a judgement. |
| **Who acts** | **The wrapper, automatically, today** (`cli_tasks.go:2623`) — up to `TaskRunRetryCap = 3`. |
| **Cap** | `run-retry:<n>`, append-only (`task_run_retry.go:49`), which survives a wrapper crash by construction. |

---

## 4. Classification against the EXISTING ladder — what falls through

This section answers the question the corrected premise makes central.

### 4.1 The structural reason nothing engine-forced is covered

Read the retry arm's own input, `cli_tasks.go:2588` and `:2600-2623`:

```go
if runErr != nil {
    closedByChild, rowStatus, statusErr := tools.TaskRowTerminalAt(target)   // :2607
    writebackOutcome(target, tools.TaskStatusFailed, ...)                     // :2609
    reconcileChildMirror(target, tools.TaskStatusFailed, ...)                 // :2610
    taskRunReleaseOwnWorktrees(target, runCloseoutReapScope(target))          // :2618
    taskRunAbnormalChildRetry(target, closedByChild, rowStatus, statusErr)    // :2623
```

**The entire arm is inside `if runErr != nil`.**

Now compare the two classes of engine-forced end:

- **Stuck nets** (`loop.go:4154`, `:4173`, `:4195`, `:4435`) return a non-nil
  error → `runErr != nil` **is** true → the arm runs → but the child's end hook
  has *already* closed the row `failed` with the truncation marker → so
  `TaskRowTerminalAt` reports `terminal == true` → the arm hits its first branch
  (`task_run_retry.go:2774`) and refuses: *"the child closed its own row
  (status failed) — a judged failure is a result, not a crash"*.
- **`WallclockCap`** (`loop.go:3950`, `:4121`) returns **`nil`** — deliberately,
  so the run closes gracefully ("the end-of-run defer closes the session +
  ledger as a normal completion", `loop.go:3942-3944`). So `runErr == nil`,
  `cmd.Wait()` reports success, and the wrapper takes the **success** path
  (`cli_tasks.go:2626+`) — where **the retry arm does not exist at all**.
- **`MaxIterations`** (`loop.go:4551`) returns an error, so it reaches the arm —
  and is refused as terminal, exactly like the stuck nets.

**Result: every engine-forced reason is refused, by one of two opposite
mechanisms.** The stuck nets are refused for being *too terminal*; the wallclock
cap is refused for being *too successful*. Neither refusal is wrong on its own
terms — both are correct implementations of "judged failure is not a crash" and
"a graceful exit is not a crash". It is the **combination** that leaves the
engine-forced ends uncovered. And the two mechanisms are one line apart, reading
the same ledger.

### 4.2 The table

| Reason | Reaches the retry arm? | Outcome | Why |
|---|---|---|---|
| `Finished` | success path | no arm | not abnormal |
| `WallclockCap` | **NO — nil error** | Falls through silently to the **success** path | the graceful-close design (`loop.go:3942-3944`) is indistinguishable from real completion at the wrapper's `runErr != nil` gate |
| `MaxIterations` | yes (non-nil error) | **refused: terminal** | child's own close-out already wrote `failed` + marker → judged, not crashed |
| `StuckLoop` | yes | **refused: terminal** | same |
| `StuckToolLoop` | yes (two sites) | **refused: terminal** | same |
| `StuckObservation` | yes | **refused: terminal** | same |
| **abnormal death** | yes | **RESUMED — the one working case** | row still open at exit ⇒ no judgement ⇒ recoverable |

### 4.3 Which arms cover which reasons

| Arm | Trigger | Reasons covered | Reasons falling through |
|---|---|---|---|
| 1 — `ResumeDeadHolderRuns` (`cli_tasks.go:5511`, merger pass) | live-holder row whose process is provably dead | **only abnormal death, and only for rows that carry a `deliverable`** — see below | all five engine-forced reasons |
| 2 — `taskRunAbnormalChildRetry` (`cli_tasks.go:2623`) | `gopi task run` wrapper, child non-zero exit, row open | abnormal death (wrapper path only) | all five engine-forced reasons |
| 3 — worker claim-arm (`cli_tasks.go:1911`) | claimed row, no live holder | abnormal death, on the worker path | all five engine-forced reasons |

**Arm 1's selection is narrower than it looks, and this matters for any Option B
design.** `deadHolderResumeRows` (`task_holder_resume.go:483`) selects:

```sql
WHERE COALESCE(deliverable,'') <> ''          -- :498
```
```go
if !taskLiveHolderStatus(r.Status) { continue }   // :512 — "terminal / holding: no run to continue"
if taskDeliverableBranchOrEmpty(...) == "" && taskDeliverableWorktree(...) == "" { continue }  // :515
```

So Arm 1 **skips terminal rows outright** (`taskTerminalStatus` =
`completed|failed`, `task_finish_cleanup.go:1040`) and **skips rows with no
deliverable**. A `wallclock-cap` row is `failed` ⇒ terminal ⇒ skipped. Therefore
Arm 1 is structurally incapable of covering the cap, no matter what is added to
it.

**Arm 3 is not available on the in-process worker path.** The retry comment
(`cli_tasks.go:2739-2745`) describes `gopi task run` waitpid-ing "its executor
child". But a worker round runs **in-process** — `runTaskWorker` claims and calls
`runTaskRunOnce` directly (`cli_tasks.go:2907`, and the run-method comment at
`:2904-2906`), with no wrapper and no child. So on the worker path there is no
`cmd.Wait()` and no Arm 2; the worker's own claim-arm (Arm 3) is the only resume,
and it does not consult `RunEndReasonFor` either.

### 4.4 Arms 1–3 and the claim gate interact — a Rule 33 consequence

`taskRowClaimable` (`internal/tools/tasks.go:5982`, Rule 33) makes a child
claimable only once its parent is `completed|verified`. A truncated run's row is
`failed`. So a **truncated parent parks its children forever** (`cancelled` and
`failed` parents park by design, with no bypass). Resuming the parent is
therefore not only about saving the parent's work — it is the only route that
releases its dependents. This consequence belongs in Option A/B/C's ledger.

---

## 5. The ordering question

**Answer: the ledger read already sits EARLY — before the release and before the
writeback — and that order is correct and must not be inverted. What is missing
is not its position but its CONSUMER.**

The call sites, in execution order, on the wrapper's failure path
(`cmd/agent/cli_tasks.go`):

| # | Line | Call | Note |
|---|---|---|---|
| 1 | `2607` | `tools.TaskRowTerminalAt(target)` | **the abnormality/ledger read.** Reads the row's status *at the child's exit instant*, before the wrapper writes anything. |
| 2 | `2609` | `writebackOutcome(target, failed, ...)` | writes the wrapper's `failed` (+ the `Truncated` refusal at `:3089`) |
| 3 | `2610` | `reconcileChildMirror(...)` | legacy mirror close-out |
| 4 | `2618` | `taskRunReleaseOwnWorktrees(target, ...)` | **the release** |
| 5 | `2623` | `taskRunAbnormalChildRetry(target, closedByChild, rowStatus, statusErr)` | consumes (1) |

And the release's own body (`cli_tasks.go:2697`) reaps descendants, then runs
`tools.ReclaimOwnWorktreesAtCloseoutWithin()`, then
`worktreerelease.ReleaseReporting(...)` — i.e. step 4 is the destructive one.

**The ordering answer in three parts:**

1. **The read must sit before `taskRunReleaseOwnWorktrees` — and it already
   does** (`2607` < `2618`). This is not an accident: the comment at `:2600-2605`
   states the intent ("the ROW'S STATUS IS READ FIRST, before the wrapper's own
   writeback lands"). The brief's worry ("if the release happens before the
   abnormality is detected, the evidence is gone") **does not hold on the
   failure path.**
2. **The read must also sit before `writebackOutcome`** — and it does. Reading
   after the writeback would make every exit look like "this wrapper's own
   report", destroying the verdict/crash discriminator. `cli_tasks.go:2604-2605`
   says so verbatim.
3. **The release is deliberately AFTER the writeback** (`:2609` < `:2618`).
   Ordering 1–4 as written is load-bearing and `taskRunReleaseOwnWorktrees`'s own
   comment (`:2720-2730`) explains why its internal order is also fixed (reap →
   reclaim → cwd release, and it needs a usable cwd).

**What is actually broken is therefore the CONSUMER, not the order.** At `2623`
the arm is handed `rowStatus`/`closedByChild` — **but the wrapper never hands it
the end reason**, even though `RunEndReasonFor` is already being called at `:3089`
inside `writebackOutcome`, five lines away in the same file. So:

> **The design requirement is: `tools.RunEndReasonFor(target)` must be read at
> the same pre-writeback point as `TaskRowTerminalAt` (`:2607`) and passed to
> `taskRunAbnormalChildRetry` as a third classifier input.** It must be read
> there, not after the release, for the same reason (1) holds: the release arms
> the ground for collection and the retry's spawn is the last act that can still
> use it.

**Coordination with `01a0f3ef` (detection) — no duplication.** This document does
not attempt detection; it classifies consumption. `01a0f3ef` owns "can the worker
tell an abnormal death happened", which is Row 6. `01a0f3ef`'s brief itself
states the boundary ("Do NOT auto-resume without the operator's decision"). The
overlap is exactly one artefact — the reason value read at `:2607` — and the
division is: `01a0f3ef` makes the read *reliable* for the abnormal-death case
(where today no reason exists at all, because the hook never ran); **this
document says which reason values, once read, must and must not lead to a
resume.** If `01a0f3ef` lands a reason for abnormal death, it lands a value in
the Row-6 slot of my table and changes nothing else here.

### 5.1 One residual ordering defect, stated honestly

The `WallclockCap` case does not reach the failure path at all — it takes the
**success** path (`loop.go:3950` returns nil ⇒ `runErr == nil` ⇒
`cli_tasks.go:2626+`). On that path `taskRunReleaseOwnWorktrees` runs at
`:2673`, **after** `TaskFinishCleanupSessionID` at `:2663` — i.e. on the success
path the finish-cleanup chain (which can push and delete the branch) runs before
the release. **If Option B extends the retry arm to the cap, it must first
establish that the cap's row is still resumable at that point** — the child's end
hook has already stamped it `failed` and the cleanup chain has been given its
turn. This is a real, verifiable ordering hazard for Option B, and I flag it as
such rather than asserting it is safe. It is *not* verifiable without running a
truncated task end-to-end, which the 60-minute budget does not allow alongside
the rest of this deliverable — see `## Not Done`.

---

## 6. Options A / B / C — consequences, for the operator to choose

The operator's framing: *"A killed run and a stalled run look identical from
outside — both stop. One is worth continuing and the other will waste a full run
to stop in the same place."*

### Option A — operator-only (today's shape, plus honesty)

The worker detects and reports; a human runs `gopi resume <id>`.

- **Consequence — in favour:** no new authority, no new cap, no new failure
  mode. Nothing to weaken.
- **Consequence — against:** a human must be watching. Rows that end while
  nobody is looking stay parked, and — because of §4.4 — **park their children
  with them, forever** (Rule 33: a `failed` parent never releases its child).
  The resume-cap gap (§2.1: `TaskResumeCount` read by nobody) is harmless here
  because the human is the bound.
- **What it costs to adopt:** nothing. It is the status quo.
- **Honest note:** A is *not* "nothing auto-resumes" — Arms 1–3 keep running
  under A. Choosing A means accepting three ladders with no shared bound.

### Option B — auto-resume the "unfinished, not stalled" reasons only

`wallclock-cap`, `max-iterations`, and detected-abnormal-death — **never
`stuck-*`**.

- **Consequence — in favour:** it recovers exactly the two rows whose context is
  intact and whose ending carried no judgement, and it is the only option that
  releases their parked children (§4.4) without a human.
- **Consequence — against, and these are the ones that decide it:**
  1. **`max-iterations` is the riskier half.** A run that burned its configured
     iteration budget re-enters with a *fresh* budget, so a row that is
     structurally too big for its budget will burn it again, capped at 3 ⇒ up to
     **4× the budget consumed** before an operator sees anything. `wallclock-cap`
     does not have this property to the same degree (its ceiling is time on the
     clock, not work volume), but it does re-enter with a fresh clock, so the
     same argument applies in weaker form. Splitting Row 2 out of Row 1 is worth
     considering.
  2. **The success-path ordering hazard of §5.1 is unproven.** Extending to the
     cap means acting on a path this document has read but not executed.
  3. **The cap already exists in a usable shape** — `run-retry:<n>` is
     append-only and crash-survivable (`task_run_retry.go:15-22`) — so B does
     *not* need a new counter, but it **must not share the namespace** with Arm 2
     for the reason `task_run_retry.go:29-32` gives. That means a third tag
     namespace, or extending Arm 2's.
  4. **B silently resumes across a changed model** unless it also acts on §7.
- **What it costs to adopt:** one reason read at `:2607` + a classification
  passed to the existing arm + a cap namespace decision + the §5.1 check. Small
  in code, load-bearing in consequence. **Not a two-line change**, so per the
  brief's boundary it would be *filed*, not implemented here.

### Option C — detect and surface, never act

The row is marked failed with the reason named, the worktree is preserved, and
the notify channel carries it. Resume stays manual.

- **Consequence — in favour:** it closes the *reporting* gap at zero risk to the
  budget. Most of it already exists: the row carries `TRUNCATED run (<reason>)`
  in both `task_events` and the `reason` column (`runend.go:81`, `:99-110`), and
  `gopi task show` renders it. The notify channel already fires for the stuck
  nets (`notifyLoopStuck`, `notifyLoopStuckToolful`, `notifyLoopStuckObservation`,
  `loop.go:4152`, `:4170`, `:4192`) and for the cap via `GOPI_TASK_NOTIFY_EVENTS`.
- **Consequence — against:** it still leaves children parked (§4.4) and still
  needs a human. It buys legibility, not throughput.
- **What it costs to adopt:** near zero — the gap C closes is the **stale
  `RecoveryHint`** (§7), which is a text-level defect, and it is the one part of
  this design that *could* be a two-line change with a fixture.

### Recommendation

**C as the immediate step, then A→B only for `wallclock-cap` after the §5.1 check
is executed.**

Reasoning, in the order the constraints bite:

1. **C is honest and cheap, and it fixes a defect that is live right now.** Today
   every truncated row — including all three `stuck-*` reasons — is stamped with
   `RecoveryHint`'s unconditional *"recoverable: `gopi resume <id>` continues
   this run's context"*. For a stuck reason that is the exact trap the operator
   named: it invites a resume that re-enters the stall. Telling an operator the
   truth costs one conditional. Since resume's soundness is a property of the
   *reason*, and the reason is already in the ledger at the moment the hint is
   composed, this is the smallest change with the largest correctness gain.
2. **B should not be adopted for the stuck reasons, and should not be adopted for
   the cap until §5.1 is measured.** The brief bans the former outright. The
   latter is the one place where this document must report "read, not run".
3. **`max-iterations` should be split from `wallclock-cap` if B is chosen** — it
   is the row where a resume is most likely to repeat a resource failure, which
   is the operator's stated cost model ("waste a full run to stop in the same
   place").
4. **Arm consolidation is worth a follow-up regardless of A/B/C.** Three ladders,
   three triggers, three tag namespaces, two of which go through the same
   `SpawnTaskResume` seam, and *no* production reader of `TaskResumeCount` — that
   is the kind of duplication Rule 14 exists against, and it is why the answer to
   "does anything auto-resume?" was mis-stated in the first place.

**The choice is the operator's.** This document does not pick B or C for them; it
records that C is free, B is not, and the reason the briefing thought B was
"new" is that the three existing arms are invisible to a name-based search.

---

## 7. The changed-model case (confirmed behaviour + its mid-chain consequence)

**Confirmed.** `cmd/agent/cli_resume.go:602-607`:

```go
// Validate the checkpoint's model against the resolved one (settings drift
// would silently change the resumed context's behaviour — warn, do not
// halt, mirroring the feasibility study's risk #5).
if req != nil && req.Model != "" && req.Model != ag.Model {
    fmt.Fprintf(os.Stderr, "[gopi] resume: checkpoint model %q differs from current %q — context may behave differently\n",
        req.Model, ag.Model)
}
```

and `cli_resume.go:582-583`: *"Resume always runs the resolved model (no image
auto-switch here — the checkpoint carries the conversation; the model warning
below guards it)."*

So: **the resume re-enters with the currently-resolved model; the checkpoint
supplies the conversation, not the model. It warns on stderr and does not
halt.** Confirmed as existing, documented behaviour — not a defect I am
introducing or proposing to change.

**The mid-chain consequence, which is the part that matters for auto-resume.**
The warning is a *warning on stderr of a foreground command*. Two consequences:

1. **In an automatic arm, nobody reads stderr.** Arms 1–3 spawn a **detached**
   child (`SpawnTaskResume` → `spawnDeadHolderResume`, `task_holder_resume.go:626`),
   whose log goes to a file (`deadHolderResumeLogPath`, `:654-656`). So under
   Option B a model change would take effect **silently**, and the resumed run
   would be judged by a different model than the one whose work it is
   continuing — a halfway state where the fix was written under model X, the
   verification and the report under model Y. The brief's ban ("do NOT resume
   across a changed model silently") is therefore a *live* constraint on B, not a
   hypothetical one.
2. **The warning is not a gate, so B cannot simply "respect" it.** Making it a
   refusal in the automatic arm would be a behaviour change to `cli_resume`
   shared with the operator path — and the operator's own door should stay open
   (a human who changes the model deliberately is doing a deliberate thing).
   The clean shape is therefore a **pre-spawn check in the arm**, not a change to
   the resume verb: the arm reads the checkpoint's model and refuses (with its
   reason named, like every other refusal in `taskRunAbnormalChildRetry`) when it
   differs from the resolved one. That keeps the human path unchanged and closes
   the silent-resume hole. Recorded here as a design requirement, not
   implemented.

---

## 8. Not Done (honest)

1. **No implementation.** Correct per the brief's boundary (nothing here is a
   two-line change except §6's `RecoveryHint` fix, which I did not land: the
   brief says "Do not implement unless..." and the operator has not chosen).
2. **§5.1's success-path ordering is READ, not RUN.** I did not execute a
   truncated task end-to-end to observe where the cap's row stands relative to
   `TaskFinishCleanupSessionID` (`:2663`) and the release (`:2673`). Option B's
   safety on the cap path depends on that measurement. **This is the single
   largest gap in this deliverable.**
3. **`max-iterations`' "ceiling or symptom" question is answered by reading the
   code path, not by measurement** — `MaxIterations` is 0 = unlimited in
   production, so a run that hits it had an explicitly configured budget. I did
   not find a recorded run that hit it to confirm the shape empirically.
4. **Arms 1 and 3 are described from their call sites and selection queries; I
   did not drive either end-to-end.** Arm 1's behaviour is pinned by its own
   tests (`internal/tools/task_holder_resume_e2e_test.go`), which I read but did
   not re-run in full (the e2e file is large; my branch-scoped run targeted the
   classifier/ladder tests).
5. **I did not read `01a0f3ef`'s branch or report.** The coordination in §5 is
   written against its *briefing* (`briefings/brief-worker-trusts-exit-code-not-ledger.md`),
   which I did read. If that task has already landed a reason read, §5's "what is
   missing" changes to "where it is consumed".
