<!-- doc-class: historical -->
# A Round Waits for the Resume Its Retry Ladder Hands Off

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Scope**: `cmd/agent/cli_tasks_worker.go` (the round's tail),
`cmd/agent/cli_tasks.go` (the retry ladder), and the new
`cmd/agent/worker_round_retry_orphan.go`.

## The measurement

A foreground `gopi worker <row>` ran one round on 2026-10-21. The round's
executor child exited 1, and the worker's own account of the round was complete:

```
worker: run 01a15bb9-b231-747b-ae57-7737658dc9e2-r1 failed (continuing):
        gopi task run: task … FAILED (child: exit status 1)
```

8m27s later a process still stood in that row's worktree:

```
PID     PPID  PGID    SID     ELAPSED STAT NLWP
3380552 54349 3380552 3380552 08:27   Ssl  37
→ ppid = 54349 = /lib/systemd/systemd --user, sid == pgid == pid
→ cgroup still …/vte-spawn-….scope   (setsid'd, NOT detached — Rule 24)
```

It was `gopi --session 01a15bb9-… --task 01a15bb9-…`, and the worker's log
carries the line that started it, one line before the failure:

```
task 01a15bb9-…: abnormal child exit (attempt 1 of 3) — resuming the same
session: … (pid 3380552, log: …/dead-holder-resume.log)
```

## Root cause

Not "the wrapper waits on the wrong process" (the epic's ① — it does not: the
executor child it `cmd.Wait()`s is the direct child it spawned). It is a third
shape the epic's two options did not name:

1. the failure path runs the retry arm (`taskRunAbnormalChildRetry`);
2. the retry arm SPAWNS A NEW RUN with `SysProcAttr{Setsid: true}`
   (`tools.SpawnTaskResume`) and **returns without waiting for it — by design**;
3. the round therefore ends while a process it started is still running, and
   nothing reaps it: `run_closeout_reap.go` deliberately vetoes a process that
   leads its own session and carries no lineage record of the run, and this
   resume carries none (GOPI_TASK_RUN_WRAPPER_PID is stamped into the EXECUTOR
   child's env only, not into this process's own).

The detachment is right for the merger (a resident pass must not be bound by a
recovery run). It is wrong for a foreground worker, where it composes into "the
round returned and a process of that round kept running".

## Why the ladder still fires at HEAD (the reachability measurement)

A healthy round does **not** spawn: the wrapper's own `writebackOutcome(failed)`
runs a few lines above the ladder, and the ladder's pre-flight gate
(`tools.TaskRunChildResumable` → `TaskRunResumeStatusGate`) refuses a `failed`
row — measured on the sandbox ledger, the refusal line printed and nothing
survived (`e2e/baseline.worker.log`). The ladder admits a row that is still
`in_progress` when the wrapper looks, and that is exactly the case where the
wrapper's writeback FAILED (`writeback failed (continuing): attempt to write a
readonly database`). Reproduced on the baseline binary by making the ledger
unwritable at the moment the child died:

```
CENSUS base2 (the instant the ROUND returned)
2719770  54349 2719770 2719770  …  /bin/bash …/slow-resume.sh --session … --task …
pid=2719770 cgroup=…/vte-spawn-….scope      ← setsid'd, still in the login scope
pid=2719770 cwd=/                          ← and pid 54349 is systemd --user
```

The fix therefore belongs in the ROUND (the thing that must not end early), not
in the gate, and it is exercised on every round that hands off — failed
writeback or not.

## The fix

The round waits for the resume its ladder handed off, bounded and reported:

- `workerRoundNamed` clears the latch at the start of every round
  (`workerRoundResetRetryResume`) — a recycled pid is somebody else's process;
- the ladder publishes the pid at the one seam that learns it
  (`workerRoundNoteRetryResume(pid)`, right after `tools.TaskRunRetryNote`);
- the round's tail waits (`workerRoundAwaitRetryResume`) **before**
  `defer workerRoundOwnWorktrees()` — the ground release is the step a
  survivor's cwd blocks, so "the round is over" must mean the same thing to the
  release pass as it does to the operator.

The wait is a DEADLINE (`workerRoundRetryWaitDefault` = 60 min,
`GOPI_WORKER_ROUND_RETRY_WAIT_SEC` overrides in seconds, `0` = no deadline), and
every exit states which fact it observed:

```
worker: the round's retry resume (pid N) exited — the round now ends; nothing it started is left running
worker: the round's retry resume (pid N) is STILL RUNNING after 3s — this round is leaving it behind
worker: stop escalated — the wait for the round's retry resume (pid N) is abandoned
```

It is a WAIT, not an ownership: nothing kills the resume. A worker killed
outright still leaves the recovery running — that is what makes recovery survive
its launcher — and the round that does end waits instead of pretending.

## Verification

```
Ran:  bash e2e/e2e-repro2.sh <bin> <label>     (real `gopi worker <row>`, real ledger, real child)
base:  census taken at the round's return → pid …/slow-resume.sh alive, cgroup …vte-spawn-….scope
fixed: "the round's retry resume (pid N) is RUNNING — waiting …" then "exited — the round now ends"
       and the census is EMPTY
fixed (deadline arm, GOPI_WORKER_ROUND_RETRY_WAIT_SEC=3, resume lives 25s):
       "is STILL RUNNING after 3s — this round is leaving it behind" and the census SHOWS the survivor
```

A detail worth keeping: `pgrep -f "slow-resume.sh"` self-matches the census
shell that mentions the name — the Rule 24 sibling trap. The trustworthy probe is
`ps -eo pid,ppid,etime,args | grep "slow-resume.sh --session"`.

## What is NOT changed

`tools.SpawnTaskResume` still `Setsid`s and still does not wait; the merger's
dead-holder arm still gets the detachment it needs. The new code reads /proc for
liveness through `procname.StatStatPath` (field numbering is not re-derived) and
treats a zombie as gone — `kill(pid, 0)` would call an unreaped child alive
forever and the wait would hang on it.
