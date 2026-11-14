<!-- doc-class: historical -->
> **CORRECTION (2026-09-19, epic `merger-delete-dead-impl`).** This record's
> subject (`--stop`, `--restart`, the loop they signal) was deleted with the
> resident merger (`f9ff1a89`, `55c4d6f3`); the whole verb now refuses and the
> one-tick merge is the bare `gopi merge`. The design reasoning below is kept for
> the re-development; `devdoc/merger-retired.md` is the record and
> `devdoc/merger-self-driving.md` carries the flag table with the same dated
> correction.

# The merger control face stops the LOOP, never the ROUND

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Task**: `gopi merger --stop` (and `--restart`) must conclude correctly while a
take-over spawned by the loop's own tick is still alive. **Measured on the base
(2026-09-18/19), it did not**: `--stop` burned its whole 60s budget and exited 1
with *"the loop has not reached a tick boundary yet"* — while the loop it had
signalled was dead 0.1s after the signal.

## The defect, in one sentence

`mergerStopLiveLoop` observed **"the flock is free"** as its evidence that the
**LOOP** had stopped, but the flock **outlives the loop by design**: a merger tick
launches a take-over and hands it the single-instance descriptor
(`mergerTakeoverExtraFiles`: `ExtraFiles[0] = fd 3`), and an `flock` lives on the
**open file description** — so the ROUND, not the loop, is the lock's holder for
the rest of the round.

## Measurement on the base (worktree `gopi-base-stop-takeover`, 9ce8e159)

Private profile, a real resident `gopi merger --every 30s` loop whose take-over is
a stub published through `GOPI_BINARY` (the rig shape of
`merger_restart_e2e_test.go`'s C9). Two independent runs:

```
### [base] loop pid=1685522   serve.lock stamped: 1685522
### [base] take-over stub pid=<child>   stub self-report: fd 3 -> <home>/.gopi/serve.lock
--- /proc/locks rows naming the lock inode:
    FLOCK  ADVISORY  WRITE 1685522 08:04:<ino> 0 EOF        <- the CREATOR (the loop)
--- fdinfo holders of the inode:
    pid 1685522 (gopi):         fdinfo/3: lock: 1: FLOCK ADVISORY WRITE 1685522 …
    pid <child> (gopi-merger-tak): fdinfo/3: lock: 1: FLOCK ADVISORY WRITE 1685522 …
--- ls -l /proc/<child>/fd/3  ->  <home>/.gopi/serve.lock     (the INHERITED descriptor)
### running: gopi merger --stop
### --stop exit=1 wall=60.24s
[merger] gopi merger: stop requested — pid 1685522, terminated (answered at the tick boundary …)
[merger] gopi merger: pid 1685522 still holds …/serve.lock after 1m0s — the loop has not reached
                        a tick boundary yet; run the stop again, or signal it directly with SIGINT
```

Both runs: `exit=1`, `wall=60.24s`, the same message. The loop's own log said
`terminated received — stopping after the current tick` **0.1s** after the signal.
The lock row names the loop because `/proc/locks` attributes a lock to the open
file description's **origin**; the process that actually *keeps it held* is the
take-over, named by the fdinfo scan.

A second, independently visible consequence: a **fresh loop is refused** while the
round is alive (`serve lock held by a live daemon … refusing double-serve`), so a
`--restart` that stopped the loop first would leave the operator with **no merger
at all**.

## The fix

| half | before | after |
|---|---|---|
| stop / wait | `mergerWaitLockFree` — the FLOCK | `mergerWaitLoopGone` — the SIGNALLED PID (`tools.PidAlive`; a zombie counts as gone, its loop is over) |
| "what is left holding it?" | implied failure | `mergerLiveLockHolder` — the **LIVE** holder from `tools.LockEvidences` (the evidence probe, not the one-pid convenience probe), named in the line |
| `--restart` while a round runs | stop first, then fail to start | **REFUSE before signalling** (`mergerRestartRoundInFlightFmt`): exit 1, naming the flag and the round's pid; the loop keeps running and nothing is signalled |
| which pid to signal | the lock's holder | the **argv-carrying loop** (`tools.MergerProcessIsLoopPID` / `MergerRequestedLoopPID`): only a poll loop accepts the period flag; a take-over's argv is the single marker token |
| the failure message | "pid N still holds <lock> … not reached a tick boundary" — every clause false on the reported case | names the PROCESS and the mechanism that still works, or reports the take-over holding the lock as the **designed** end state |

`mergerWaitLockFree` is **kept** for the START half, where the flock *is* the
subject: the fresh loop may only begin once the guard is actually free, whatever
process was holding it. One lock, one guard (`acquireServeLock`) — the waits are
waits, never guards.

## Verification

**CLI, end to end** (`scripts/lib`-free shell rig, stub take-over, private
profile; `run-worktree/`), the fixed binary built from this branch's worktree:

```
### [fixed] loop pid=2399925   serve.lock stamped: 2399925
### [fixed] take-over stub pid=2401345     fd 3 -> <home>/.gopi/serve.lock
### [fixed] running: gopi-worktree merger --stop
### [fixed] --stop exit=0 wall=0.338s
[merger] the loop pid 2399925 has exited; …/serve.lock is still held by the take-over it spawned
         (pid 2401345) and stays held for the rest of that round — the round ends by itself, and
         the next merger starts then
### [fixed] take-over pid 2401345 ALIVE after stop: yes
### [fixed] loop pid 2399925 alive after stop: no (exited)
```

`exit 0` in **0.34s** (base: `exit 1` in 60.24s), take-over **still alive**, loop
gone, and the line names the holder.

**Tests** — see the task report (`## Verification`). The four cases this branch
owns (`TestMergerRestartDoesNotTouchTheTakeoverInFlight` and the three
`TestMergerStopContract_*`) are **green**; `TestMergerRestart` C4 is green too.
`TestMergerStopEndsALiveLoop` (C2) is red on this branch **and on the base** for a
reason this diff does not own — read the section below before reading that red as
a regression.

**Stylegate delta is ZERO** — the branch adds no swept violation. Measured by
diffing the gate's own site list between the base worktree and this one
(`go test -run TestStyleGate ./internal/stylegate/`): the only rows that differ
are the line numbers the same pre-existing sites moved to. `internal/stylegate`
is **red on the base too** (`test-sleep` 5, `errorf-verb` 8/ceiling 7,
`ignored-error` 11 prod), so it is red here for the identical reason and not as a
consequence of this branch; the five sites this branch *did* add are annotated
`//stylegate:ignore <reason>` rather than baselined, so the ceiling is untouched.

**Anti-regression, minimal samples** (`cmd/agent/merger_restart_takeover_contract_test.go`,
~100ms each, no helper binary — `syscall.Flock` + a re-exec of the suite's own
binary):

* `TestMergerStopContract_LockOutlivesItsCreator` — creator dies, the lock is
  **still held** by the inheriting child, and the evidence probe names the
  **survivor**, never the dead creator.
* `TestMergerStopContract_ProbeReportsFreeOnceTheLockIsReleased` — the negative
  half, with a positive control (held → both probes say held; released → both say
  free), so a probe that never reports free cannot pass.
* `TestMergerStopContract_TheStopWaitsOnTheLoopNotOnTheLock` — the defect itself:
  **the wait on the flock cannot conclude** in this state (the base's observable)
  while the wait on the signalled pid **does**, and the reported holder is the
  survivor.

## A pre-existing red this branch did NOT cause (measured, so nobody blames the diff)

`TestMergerStopEndsALiveLoop` (case C2) is **red on the base too**, in this
environment, at a high rate — the failure it reports is that the **LOOP does not
exit within the 60s budget**, independently of which evidence the stop waits on:

| tree | runs | verdict | symptom |
|---|---|---|---|
| base `9ce8e159` | 3 | **3 FAIL** (61.9s / 63.7s / 62.5s) | `pid N still holds …/serve.lock after 1m0s` |
| this branch | 4 | **4 FAIL** (61.7s / 61.3s / 62.1s / 61.7s) | `pid N is still running after 1m0s` |

The two messages differ only because the two trees wait on different evidence;
the substance is identical — **the signalled loop is still alive when the budget
expires**. The loop's own log ends on its sleep line with **no
`terminated received`** line, and `os.Signal` is caught for its whole life, so
the signal was either never delivered to the handler or the process is wedged in
its shutdown path (`SigPnd` reads 0, all threads in futex/`ep_poll`, state `S` —
sampled 1/s for the whole window).

**The mechanism is NOT this branch's, and not the stop logic's**: in an otherwise
idle private rig, a real `gopi merger --every 30s` exits **0.51s** after a direct
`SIGTERM` — 3 runs, 3 times (`.509s / .509s / .509s`) — and the earlier
end-to-end CLI measurement on this same branch has `--stop` concluding in
**0.338s**. The variable that differs between the green and the red runs is the
HOST: the failing runs share the machine with several concurrent gopi workers,
and the suite's own banner names the cost — the ledger sandbox is on a
**block-backed** filesystem (`0xef53`) where "every fresh ledger's first open pays
that device's journal commits". A shutdown path that flushes a log/ledger under
that contention is the shape that fits every observation; it is a **separate
defect and it is filed as one** (see the task report's `## Not Done`), not
papered over here. Per Rule 27 nothing was loaded to produce it: every number
above is an observation of the process under test.

## Rules this touched

* **Rule 1b/13/14**: every new spelling is a named constant; the loop/round
  discriminator lives ONCE in `internal/tools` (`MergerProcessIsLoopPID`), read by
  both readers (`MergerRequestedLoopPID`, `MergerRoundHolders`).
* **Rule 15**: the message asserts only what the probe produced — the old sentence
  asserted three things that were each false.
* **Rule 10.5 / 10.4**: the fix ships its own repro test, verified on the smallest
  state (two processes and one flock).
* **Rule 27**: no load was generated; every measurement is of the process under
  test.
