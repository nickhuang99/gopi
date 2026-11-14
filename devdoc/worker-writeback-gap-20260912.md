<!-- doc-class: normative -->
# Worker/run writeback gap — normal exit never closed the row (2026-09-12)

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

Task: `01a09431-fca8-7806-9894-ff2010b75821` (diagnosis) and this epic (fix) —
*worker 正常退出却写不回台账：根因 + 可复现的修复*. The diagnosis half landed on
the unmerged branch `diag/worker-writeback-gap` (`bb1b40e6`, reverted from master
by `2edf6338`); **this document supersedes its §A trigger** (corrected below) and
adds the fix, the tests, and the honest boundary.

Tree of record: worktree `~/workspace/gopi-wbfix`, branch
`fix/worker-writeback-normal-exit`, base master `2edf6338`.
All file:line references are against that tree.

Short version: the terminal ledger write lives **after `cmd.Wait()`** in the run
wrapper, and the one process that *does* survive a dead wrapper — the run's own
child — was **forbidden** from writing the row. The fix gives the child that write
back at its normal exit, guarded so the healthy majority path stays untouched.

---

## A. Root cause — which line does not run, and under what condition

### A.1 The terminal write is post-run-only, with no `defer` and no signal path

`runTaskRunOnce` (`cmd/agent/cli_tasks.go:1225`) is the ONE wrapper that owns the
human row (`GOPI_TASK_WRAPPER_ID` — `internal/agent/taskregister.go:187-190`).

```
runErr := cmd.Wait()                                                 // :1400
if runErr != nil {
    writebackOutcome(target, TaskStatusFailed,   …)                  // :1412  ← terminal write
    reconcileChildMirror(target, TaskStatusFailed, …)                // :1414
    return fmt.Errorf(…)                                             // :1415
}
writebackOutcome(target, TaskStatusCompleted, "gopi run completed…")  // :1417  ← terminal write
reconcileChildMirror(target, TaskStatusCompleted, …)                 // :1426
tools.TaskFinishCleanupSessionID(target)                             // :1440  ← worktree/branch recycle
```

None of `:1412` / `:1417` / `:1440` sits in a `defer`, and there is **no**
`signal.Notify` anywhere in `runTaskRunOnce` — a wrapper killed at ANY instant
between `cmd.Start()` (`:1379`) and `:1417` leaves the row `in_progress` forever,
*even though the child's work is complete on disk*.

**Why not "just add a defer":** a `defer` does not run on SIGKILL, and the measured
failure mode is precisely a run whose wrapper session is cut mid-stream. The fix
below therefore does not depend on the wrapper's exit path at all (§B).

### A.2 Why the child cannot close the row in the parent's place (pre-fix)

The child of a run is a **separate process** with its own, independent exit path —
`internal/agent/taskregister.go` `finishSessionTask` is called from the runEngine
defer, so a normally-exiting child reaches it. Pre-fix, that hook could not help:

1. The wrapper-child gate (`taskregister.go:187-190`) sets
   `autoTaskRowID = wrapperID` and **returns before latching
   `autoTaskRegistered`**, with the comment *"no auto row — execution tracked on
   the human wrapper row"*.
2. `finishSessionTaskRecorded` (`taskregister.go:275-277`) therefore early-returns:
   `if !a.autoTaskRegistered || a.SessionLog == nil { return }`.
3. `reconcileChildMirror` (`cli_tasks.go:1426`) only closes a *legacy auto mirror*
   row, which task-single-row-exec no longer mints — a defensive no-op for new runs.

**Net (pre-fix):** the wrapper row was closable by exactly one process — the
wrapper — and that process was the only `gopi` verb with no signal handler and no
defer around its terminal write.

### A.3 Corrected trigger (measured — supersedes `bb1b40e6` §A.3)

The earlier diagnosis attributed the 7 rows to "the glmwatch probe SIGTERM'd every
`bin/gopi worker`, and the worker has no SIGTERM handler". Re-measuring the evidence
corrects the attribution while keeping the mechanism:

- All 7 rows' **children exited 0** (their `session_end` carries `exit_code=0`), so
  no child was killed.
- Their **worker logs are truncated mid-stream** — mtime `07:33:43 / 07:44:31 /
  08:13:57 / 08:24:57 / 09:02:28`, ending inside the run, with no
  `task <id> completed (elapsed …)` line (which is printed at `:1448`, i.e. *after*
  the writeback). The truncation is the fingerprint of the post-Wait window.
- The claimed pids on those rows (`463556 / 463612 / 463445 / 1192935 / 1223574`)
  are all **lower** than the pid of the session that killed the workers at 07:44
  (`950367` — logged in `worker-20260912-050217-3.log:13574-13581`), i.e. the rows
  were claimed by *earlier* sessions and re-used by later worker rounds. The epic's
  own wording — a **`worker`-driven** task — is what put them in this class: the
  row was later `resumed` by a worker round (`TaskResumeClaimed`), whose pid stamp
  then loaded the "worker SIGTERM" story.

So the trigger is: **a run whose wrapper dies inside the post-Wait window, for any
reason** (kill, session teardown, launcher stop). The failed path is
`cli_tasks.go:1412/1417` never executing; the missing *coverage* is the child,
which was alive and finished normally in every one of the 7 cases.

### A.4 Second-order damage: an occupied worktree is a merge block

Because `:1440` (`TaskFinishCleanupSessionID`) also never ran, each killed wrapper
left its linked worktree behind. `scripts/safe-merge.sh` gate **[c]** refuses a
branch still checked out in a worktree, and the merger's protected-branch list
(`internal/tools/repo_sweep.go`) skips them — so the missing writeback did not
merely mislabel the ledger, it withheld the deliverables from the merge pipeline.
The fix does not repair this directly (the worktree recycle rides the wrapper), but
the child's close-out sets the row terminal, so the stale/finish-cleanup pass can
now find it.

---

## B. The fix — the child closes the row it ran, idempotently

### B.1 Mechanism (three surgical pieces, no new daemon, no trunk rewrite)

1. **Pre-spawn export** — `cmd/agent/cli_tasks.go` (`runTaskRunOnce`, the
   `cmd.Env` seam): the wrapper exports, on the child's argv env, the row it owns
   and what that row becomes:

   ```go
   if envTaskID, envOutcome := tools.RunCloseoutPair(target, tools.TaskStatusCompleted); envTaskID != "" {
       cmd.Env = append(os.Environ(),
           tools.TaskWrapperEnv+"="+envTaskID,
           tools.TaskRunIDEnv+"="+envTaskID,
           tools.TaskRunOutcomeEnv+"="+envOutcome,
           tools.TaskRunWrapperPIDEnv+"="+strconv.Itoa(os.Getpid()),
       )
   }
   ```

   `tools.RunCloseoutPair` is the single constructor for the pair, so writer
   (wrapper) and reader (child) agree by construction rather than by two
   hand-assembled string literals (Rule 13/14). Consts live in
   `internal/tools/tasks.go` (`TaskRunIDEnv`, `TaskRunOutcomeEnv`,
   `TaskRunWrapperPIDEnv`); the outcome value is `tools.TaskStatusCompleted` — a
   dated ledger value referenced, never typed (Rules 1b/2).

2. **Child-side close-out** — `internal/agent/taskregister.go`,
   `finishSessionTaskRecorded` now calls, **before** the auto-row gate (the gate
   returns early for exactly these rows):

   ```go
   a.closeOutRunRow(float64(exitCode), elapsedSec, iterations, tokens)
   ```

   `closeOutRunRow` resolves `closeOutRunEnabled()` (kill switch
   `GOPI_TASK_RUN_CLOSEOUT`, default ON, Rule 2: one reader), maps
   `exitCode → completed|failed` (honoring `TaskRunOutcomeEnv == failed` so a
   refused run is never "corrected" to completed), and calls the tool.

3. **Idempotent ledger write** — `internal/tools/tasks.go`,
   `TaskRunCloseoutStaleRun(RunCloseoutRequest) (out string, applied bool, err error)`:

   - empty run id → no-op;
   - a non-terminal outcome (`in_progress`…) → **error** (the fallback closes rows,
     it never re-opens one);
   - row missing → no-op;
   - row already terminal (or cancelled) → **no-op, `applied=false`**;
   - **claim evidence** required (see B.3) else refusal logged (`taskNotifyLog`,
     Rule 15 — the refusal is recorded, not swallowed) and the row is left for the
     stale sweep;
   - otherwise `TaskUpdateWithResult` + a log line.

### B.2 Idempotency — how a double-write is made impossible

Two guards, BOTH at the close-out's caller — the ledger's own write path is left
exactly as it was:

1. **The wrapper must be dead** (`runCloseoutClaimEvidenceHolds`). A live claim pid
   refuses outright, so the fallback never races a wrapper that is still working.
   This is the guard that keeps the healthy path intact: the wrapper writes first,
   with the richer detail, and the child never competes. (Measured before this
   guard: closing underneath a live wrapper silently replaced its
   `exit status 1` result with the fallback digest and closed a running run's row
   before `TestTaskRunSurvivesClosedStdoutPipe` had finished watching it.)
2. **The row must still be non-terminal** (`TaskRunCloseoutStaleRun` re-reads and
   returns `applied=false` when the status is already terminal/cancelled). This is
   the pure double-write guard: whichever writer lands second sees a terminal row
   and appends nothing.

**A correction worth recording (Rule 15 — the honest version of this section).**
The first cut pushed guard 2 down into the LEDGER (`taskUpdate` gained "append no
event for a same-terminal-status write"). That was reverted, because it silently
changed a documented contract: `TestTaskUpdateWithResultKeepsSummaryImmutable`
step ④ states it explicitly — *"A second writeback (idempotent same-status no-op)
still keeps the summary untouched and **appends another event row** (the audit
trail is append-only — history is never rewritten)."* Five other
`cmd/agent`/`tools` tests and a `cmd/agent` E2E
(`TestTaskRunChildNonZeroExitClosesWrapperRowE2E`: the child's close-out wrote
`failed` first, so the wrapper's richer `exit status 1` detail was dropped) failed
on that change. The lesson: the audit trail records WRITES, not effective state —
"one effective write" is enforced where the decision is made, not by muting
history. The ledger code now carries a comment saying exactly that, so the next
agent does not re-introduce it.

`taskAllowedTransitions` treats a same-status move as legal, so the ledger itself
permits a repeat write; the caller-side guards are what make this fallback
idempotent, and they are where the tests pin it.

### B.3 Alternatives considered and rejected

- **A bare `defer writebackOutcome` around `:1400-1440`.** Rejected as the primary
  fix: it cannot run if the wrapper is SIGKILLed inside the post-Wait window, which
  is the measured failure. (It would only help traps that already have a handler.)
- **Forward SIGTERM to the child and let the child's failure path write back.**
  Rejected: the child is a *separate session*, not a process-group sibling we own;
  a group-scoped signal would reach the wrapper itself (self-kill, since it shares
  the group), and the measured evidence shows the children exited **normally** — the
  missing coverage was the normal-exit write, not a signal-unwind write.
- **A new daemon/tick that disposes confirmed zombies.** Rejected by the epic
  ("no new daemon"), and it would not have closed these rows any faster than the
  child itself does. Note the *related* real defect (detector without an automatic
  disposer) is filed as a bug task instead of being smuggled into this fix.

---

## C. Why `internal/tools/taskzombie.go` did not report the 7 rows

**It did report them — this is a detector with no automatic disposer, plus a
write-only sink.** Measured, not inferred:

- The notify sink `~/.gopi/notify/task-events.jsonl` (45 MB, 124 927 concatenated
  JSON objects) carries **1 297 `stale` events**; **352 distinct rows** have
  `confidence=confirmed` (i.e. `pid_dead=true`) — the detector's highest-confidence
  verdict. For the incident rows:

  | row | stale events | first | last |
  |---|---|---|---|
  | `01a092a6-6011` | 9 | 2026-09-12 08:03:26 | 14:50:25 |
  | `01a092b2-4f21` | 8 | 08:06:56 | 14:50:26 |
  | `01a092b3-592e` | 8 | 08:10:57 | 14:50:27 |
  | `01a092a6-43a2` | 8 | 08:16:01 | 14:50:25 |
  | `01a092ab-db9b` | 7 | 08:22:18 | 14:50:26 |
  | `01a0924c-5201` | 6 | 08:22:15 | 14:50:24 |
  | `01a09260-f5c8` | 6 | 08:56:32 | 14:50:24 |

  Every event carries `zombie: {"confidence":"confirmed","pid_dead":true,…}`.

- Mechanism: the worker tick calls `serveStaleSweep` →
  `internal/agent/taskheartbeat.go` → `tools.DetectStaleTasks` →
  `zombieVerdict` (`internal/tools/taskzombie.go:135`, confirmed ⇔ the claimed pid is
  dead). It then **only** notifies and re-arms `updated_at` (the `taskZombieBumpSQL`
  dedup guard, at most one event per `GOPI_TASK_STALL_SEC`, default 1800 s). **No
  tick calls the disposer** — disposition needs the manual
  `gopi task sweep-zombies --kill` (`internal/tools/taskzombie_sweep.go`), and the
  notify sink is an append-only file with no automated consumer.

So `taskzombie.go` is **not blind to this class** — it classified all 7 correctly and
kept re-reporting every ~30 min for 6-9 h. What was missing is (a) an automatic
disposer for confirmed rows and (b) a sink anyone reads. Filed as a bug task rather
than folded into this fix — see §E.

---

## D. Honest boundary — when close-out can still be missed

The fix covers **the run's normal end** (the epic's wording). It does **not** cover
these, and none of them is fixed here:

1. **The child is killed before its END hook.** `finishSessionTask` runs in the
   runEngine defer; `SIGKILL` (or an OOM kill) skips it, so neither the child's own
   writeback nor this fallback runs. The row then stays `in_progress` until the stale
   sweep — which today only *notifies* (§C). The fallback raises the floor, it does
   not remove the need for a disposer.
2. **No claim evidence.** If the wrapper dies before `TaskStart` stamped `claimed_by`
   *and* after spawn, and the child session id never got backfilled, the close-out
   refuses by design (a log line, never a false close). This is the safe direction,
   but it means the fallback is not unconditional.
3. **The kill lands before the child exists** (`cmd.Start()` → child env not yet
   read): there is no child to write anything, so only the disposable-sweep path can
   close the row.
4. **Close-out writes `completed` for a run that "ended" mid-work.** Exit 0 is the
   only signal available here; a child that exits 0 *without* delivering still reads
   as completed. That is the pre-existing meaning of the wrapper's own writeback too
   (this fix does not make the ledger's success semantics stricter), but it is worth
   stating: the fallback inherits the child's notion of success.
5. **The other half of the incident** — the abandoned worktree that blocks
   `safe-merge.sh` gate [c] and the merger's protected list (§A.4) — is untouched.
   The child cannot recycle the wrapper's worktree (it holds no deliverable
   reference for it); that remains a separate, already-identified defect.
6. **Multi-node rows.** The close-out is a local DB write. A row claimed on another
   host is refused by the claim-evidence check (a foreign pid is meaningless here),
   as the pre-existing zombie evidence chain also does.
7. **Operator kill switch.** `GOPI_TASK_RUN_CLOSEOUT=0` disables the fallback (loud
   log line); with it off, the pre-fix behaviour returns exactly — which is what the
   red-proof in §E.1 uses.
8. **The fallback needs a DEAD wrapper** (§B.3): a wrapper that is alive but wedged
   (blocked forever, not dead) is deliberately NOT overridden — its row stays open
   until the stale chain disposes it. That is the correct trade (a live wrapper's
   write is the richer one), but it means "the wrapper is stuck" is still handled by
   the sweep path, not here.
9. **The wrapper-death E2E is not staged** (§E.3) — a test-side race would make it
   flaky, so the dead-holder decision is proven at the tools layer and the wiring
   end-to-end with a live wrapper. Coverage of the exact interleaving in a single
   process is therefore by construction, not by a test.

---

## E. Verification, and the red proof

The proof is split by LAYER on purpose: the decision is proven deterministically
where it is made (the ledger write), and the WIRING is proven end-to-end with the
real binary. Staging a real wrapper death from a test was attempted and abandoned —
see "why the wrapper-death E2E is not staged" below; a flaky proof would be worse
than none.

### E.1 The decision — deterministic, `internal/tools`

<!-- Retired mechanism (2026-11-05): the close-out reap and its tests were
     deleted whole (`git log --oneline --grep=closeout-reap`). The command
     below now exits 0 with `[no tests to run]` — read it as the evidence that
     WAS taken on 2026-09-12, not as a command a reader can re-run today. -->

The `TestRunCloseout*` tests in `internal/tools` → **5/5 PASS**

| test | what it pins |
|---|---|
| `TestRunCloseoutClosesOpenRow` | our run's row, claimed pid provably DEAD → closed terminal, `applied=true` |
| `TestRunCloseoutIdempotentAfterChildSelfReport` | wrapper already wrote it → `applied=false`; the close-out appends **no second `completed` event** (guard 2 at the caller — the ledger's own append-only contract is untouched) |
| `TestRunCloseoutRefusesUnclaimedRow` | no claim evidence → refused, row left open (never a guess) |
| `TestRunCloseoutRejectsNonTerminalOutcome` | a non-terminal outcome → error, row untouched |
| `TestRunCloseoutRefusesLiveWrapper` | **live** claimed pid → refused, row stays `in_progress` |

The last row is the regression guard for the fix's first cut: closing a row
underneath a LIVE wrapper won a race it must lose — measured, it silently replaced
the wrapper's `exit status 1` result with the fallback digest and closed a running
run's row before the observer (`TestTaskRunSurvivesClosedStdoutPipe`) had finished
watching it. Two `cmd/agent` E2Es caught that; both are green again.

The dead-claim fixture is `deadClaimPid()`: `pid_max + 1` (`/proc/sys/kernel/pid_max`),
a pid no process can hold, so `pidAlive` is false **by construction** — a
freshly-reaped child was tried first and FLAPPED, because the `go test` binary forks
constantly and the recycled pid made the fixture non-deterministic.

**RED proof** — the same decision, with the fallback switched off, on a real worker
round (`GOPI_TASK_RUN_CLOSEOUT=0`), a scripted mock LLM, child exits 0:

```
[gopi] task ledger: run closeout for t-wb-worker-e2e skipped (GOPI_TASK_RUN_CLOSEOUT is off)
--- FAIL: TestWorkerRunRowClosesWhenTheWorkerNeverWritesBackE2E
    row status = "in_progress", want "completed"
      — the run child exited normally but the row was never written back (worker-writeback gap)
```

…and with the fallback ON (default), same pinned shape, the close-out fires and the
audit trail carries its reason:

```
[task-notify] task run closeout: t-wb-worker-e2e → completed (child normal exit, wrapper writeback never landed)
[gopi] task ledger: run closeout run closeout: t-wb-worker-e2e → completed — …
        result:         gopi run completed — run: elapsed=20s
```

(The RED run was produced while the wrapper-death fixture was still staged; the
assertion it proved — child ends, row would stay open without the fallback — is the
same one the deterministic case now pins.)

### E.2 A THIRD defect this work found: a zombie read as alive

`pidAlive` (`internal/tools/taskzombie.go`) probed with signal 0 — and a process
that has EXITED but not been REAPED still answers signal 0. Measured
consequence: a `task run` wrapper SIGKILLed in its post-Wait window was its own
child's unreaped corpse, so it kept reading as a **live holder**; the close-out
refused and the stale sweep's pid veto suppressed the verdict. That is a
detector-side blind spot in the SAME incident, found by the fix's own negative
test.

Fix: `pidAlive` now consults `/proc/<pid>/stat` first and treats state `Z` as
dead, falling back to the signal-0 answer where `/proc` is unavailable (a read
failure must never silently turn a live holder into a dead one — the predicate
returns false = "not provably a zombie"). Constants: `procStatPathFormat`.

Regression guard (Rule 10.5): `TestPidAliveZombieIsNotAlive` — fork, SIGKILL,
**do not reap**, poll until `/proc` reports `Z`, then assert `pidAlive == false`
and reap. Skips loudly if the kernel never publishes the zombie state, so it can
never pass vacuously. `TestPidAlive` (existing) stays green.

### E.3 The wiring — end to end with the real binary

`TestWorkerRunHealthyPathKeepsTheWrapperResultE2E`: a real `gopi worker` round, the
hermetic child-failure escape, the wrapper SURVIVING (the majority case) → the row
is `failed`, carries **the wrapper's own detail** (no fallback digest in its
trail), has **exactly ONE** failed transition event, and the round leaves no
`[auto]` row. This is the guard against the fix's first cut.

**Why the wrapper-death E2E is not staged.** The obvious fixture — the child
SIGKILLs its own parent at a chosen instant — was implemented and abandoned: it is
inherently racy, because the child's run and the kill timer compete and the Go
runtime tears the timer down with the process (measured: the timer never fired
before the child's END hook, and shortening it only moved the race). Rather than
ship a flaky test, the dead-holder case is proven where it is decidable (E.1) and
the wiring is proven where it is observable (E.3). **This is stated as a limit, not
dressed up as coverage.**

### E.4 Regression suite

The four pre-existing run-path E2Es stay green — the child's close-out is a no-op
behind a live wrapper and the child self-report path is untouched code:

```
go test -count=1 -run 'TestTaskRunChildNonZeroExitClosesWrapperRowE2E|TestTaskRunSurvivesClosedStdoutPipe|TestTaskRunNormalExitSingleRowE2E|TestTaskRunChildSIGKILLClosesWrapperRowE2E' ./cmd/agent/
ok  	github.com/nick/gopi/cmd/agent	20.7s
```

## E.5 Environment caveat — what was NOT verified, and why (2026-09-12 late session)

The host reached sustained resource exhaustion during the final acceptance pass:
`fork/exec ... resource temporarily unavailable` on git / vet / link, load average
up to 94. Two consequences, both recorded rather than papered over:

1. **Full-package `go test ./internal/tools/` did not complete.** 36 tests in
   `task_merged_cleanup_test.go` — files this change never touches — failed with
   fork/exec EAGAIN and nothing else. **Zero non-resource failures were observed.**
   A clean full-suite run is still owed.
2. Even `cd ~/work/GoPi && git log` could no longer fork a shell at the very end.
   The branch tip (`001b09f8` on `fix/worker-writeback-normal-exit`) was verified
   committed EARLIER in the same session, before exhaustion set in, and a worktree
   commit is shared with the main repo by construction (no push required, Rule 6c).

The targeted verification in E.1–E.4 all ran green in this session.

## F. Defects found while fixing this (filed, not silently absorbed)

1. **Stale-zombie detection has no automatic disposer.** `DetectStaleTasks` reaches
   `confidence=confirmed, pid_dead=true` for a row and then only notifies; nothing in
   the tick calls `SweepZombies`. 352 rows have been re-reported every ~30 min, some
   for days. **Filed: `01a0948b-85b3-72be-8d56-ca5f6bb8c32b`** (`tags=bug priority=1`).
2. **The notify sink is write-only.** `notify.cmd = cat >> ~/.gopi/notify/task-events.jsonl`
   — 45 MB / 124 927 events with no consumer; "reported" means "appended to a file
   nobody reads". Same bug task as (1) (`01a0948b-85b3-72be-8d56-ca5f6bb8c32b`), since
   fixing either alone leaves the loop open.
   Measurement note: the sink is a **concatenated JSON stream on one line** — a
   line-based parser sees 2 lines and 2 "bad" objects; decode it by streaming
   `json.JSONDecoder().raw_decode` in a loop to get the real 124 927 objects.
3. **`session_id` is frequently empty at close-out time.** The bug054 stamp is a
   best-effort poll of the child's session JSONL (`waitChildSessionJSONL`) taken
   right after spawn; measured here as empty in the first seconds of a run, so the
   close-out's session evidence (guard #3) is the weakest of the three. Not filed
   separately — recorded here; the ppid/parent-pid evidence carries the case today.
4. **The claim trail can name the wrong holder pid**, and a dead holder's worktree
   silently blocks `safe-merge.sh` gate [c] + the merger's protected list (the
   second-order damage of §A.4, which this fix does not repair — the child holds no
   deliverable reference for the wrapper's worktree; measured `merged=0 …
   protected=7`). **Filed: `01a0948b-a6dc-78b1-9973-854b1bffe254`**
   (`tags=bug priority=1`).

---

## Constraints honored

- Developed only in `~/workspace/gopi-wbfix` (branch `fix/worker-writeback-normal-exit`);
  the main checkout at `/home/user/work/GoPi` was never written to.
- The running merger was never touched; no `pkill -f gopi` / `pkill -x gopi` was run.
- The 7 already-merged rows were not re-closed here (that is `01a09431`'s close-out);
  no old task was re-run.
- The child self-report writeback path (the majority case) is unchanged — the new
  call sits *before* the existing gate and is a no-op when the row is already
  terminal; nothing in the auto-register/auto-row-close logic was rewritten.
- No new daemon, no new tool, no new top-level process.
- Rules 1/2/13/14: every env name and every ledger value comes from a declared,
  referenced constant; Rule 15: every refusal/failure path logs.
