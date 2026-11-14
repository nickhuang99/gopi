<!-- doc-class: historical -->
# The merger's daemon machinery is removed — the merger is a cronjob

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

> **SUPERSEDED (2026-09-19, epic `merger-delete-dead-impl`).** Both notes above
> describe intermediate states; the end state is that **the merger's resident
> face is gone entirely**. `gopi merger` — every spelling, `--every` included —
> is a closed entry point that prints one recorded sentence (`mergerRetiredCause`,
> `cmd/agent/merger_retired.go`) and exits 1; the loop, the tick and the
> `--stop`/`--restart` control face were deleted (`f9ff1a89` closed the entries,
> `55c4d6f3`'s merge deleted the code). `devdoc/merger-retired.md` is the
> record; `devdoc/merger-self-driving.md` carries the flag-classification table
> with the same correction. **The one-tick merge is now the bare `gopi merge`**,
> and the merge WORK LOCK survived (`cmd/agent/merger_work_lock.go`). Everything
> this document says about REMOVED machinery (no detached start, no state or
> request files, no child registry, no watchdog) is true again, and more so: the
> test list below (`merger_retired_flags_test.go`) was itself deleted at the
> retirement, and its claim is now carried by `merger_retired_face_test.go`'s
> `TestRetiredMergerFacesRefuseLoudly` (the whole VERB refuses, not a flag class).

> **PARTIALLY SUPERSEDED (2026-09-18, epic `merger-self-driving`).** The
> machinery removal below STANDS — no detached start, no state/request files, no
> child registry, no watchdog, no control flags (`--daemon`/`--stop`/`--restart`/
> `--start`/`--force`/`--wait` all still refused). What changed is ONE flag:
> `--every D` came back, as a SLEEP PARAMETER rather than a control face — it
> repeats the same cron tick in-process, in the foreground, under a flock taken
> once and held for the loop's whole life, and a signal ends it. See
> `devdoc/merger-self-driving.md` for the classification table and the measured
> lock window. Statements in this document that call the merger a "one-tick"
> process remain accurate about everything except that flag.

**Epic**: `daemon-machinery-removal` (2026-09-17, host **peerbox** — the system
layer, Rule 28).
**Branch**: `feat/daemon-machinery-removal` off `master@64f4345a`.
**Net effect**: 56 tracked files changed, **+714 / −5259**, plus 4 new files
(`cmd/agent/merger_tick.go`, `cmd/agent/merger_retired_flags_test.go`,
`cmd/agent/merger_test_rig_test.go`, this devdoc).

---

## 1. What was decided (the architecture this lands)

A cron tick starts a **SHORT-LIVED** gopi. It reads its prompt, decides whether
there is work, does it serially, reports, and **exits**. Nothing is resident.
Nothing waits on a child. Nobody outside judges a process dead or stuck.

History: the merger grew up the other way. It was a **resident daemon** — one
that detached itself (`--daemon` = fork/exec-self + `setsid` + stdio redirect +
pipe handshake + inherited flock), escaped its login session scope, was started,
stopped and restarted by an operator, held a handover window for its successor,
and was supervised from outside. All of that is machinery for **being a
service**. A cronjob is not a service. So the layer is not "stale" — it is
**dead weight with live failure modes**, and it goes.

## 2. The tick, in full (the shape that replaced it)

`cmd/agent/merger_tick.go` — the whole process, extractable into one file:

```
runTaskMerger(opts)
  ├─ acquireServeLock()              single-instance flock on <data root>/serve.lock
  └─ runTaskMergerWithLock(opts, lock)
       ├─ attachHostLifecycleLog(...)   mirror lifecycle lines into logs/merger.log
       ├─ mergerMasterSafetySweep()     repair a repo a predecessor died inside
       ├─ scratchGCSweep()              ┐ resident sweeps that used to ride the
       ├─ runTaskLifecycleTick()        ┘ poll loop now ride the tick (not on --once)
       ├─ ensureServeLockIdentity()     the flock is still the file serve.lock NAMES
       ├─ mergerPassOnce(opts)          ONE full pass over every configured repo
       └─ return                        the lock closes with the process
```

**What a tick still does** (all of it kept — this is category **C**): land every
fast-forwardable branch, **park** every branch that does not fast-forward (a
merge-backlog ledger row + one ordinary worker rework), refresh the CLI
contract/test-token maps, clean up already-merged branches, rebuild every repo
whose tick landed work. Conflict *resolution* remains a worker's job.

**Why master safety is the load-bearing part now**: no merger exits mid-merge by
its own hand — a merger that dies, dies the way any process dies (SIGKILL, OOM,
power cut). The process that can repair the tree is therefore the one that comes
**next**, and the next one is a **new process**, because there is no loop to
return to. The cron form loses nothing here: the tick IS the recovery point.

## 3. The escaped-daemon root cause, and why it cannot recur

**Measured root cause** (4+ observed that day). Several test classes started
**real resident gopi daemons** (`gopi merger --daemon`, `gopi worker --daemon`)
and stopped them only in `t.Cleanup`. `t.Cleanup` does **not** run when the test
is SIGKILLed at its 20m/25m/40m package timeout — so the daemon survived with
`PPID=1`, kept its (already deleted) `/dev/shm` sandbox open, and ran for hours
until the tmpfs filled. Nothing in the process's world could notice: the sandbox
was gone, the test binary was gone, and the daemon had no controlling terminal.

There was a mitigation (the daemon-lifetime pipe: a read end inherited by every
gopi process the test starts, so a daemon dies when the test dies — confirmed
live in this run's output, `daemon lifetime: armed — read end fd 200 …`). That
mitigation is **real, but it is a net under the actual defect**: it protects
against the test process dying, not against the class of "a test holds a
resident service".

**What this change does to the class** — by construction, not by net:

| vector | before | after |
|---|---|---|
| merger E2E starts a resident merger daemon | many tests | **zero** — the merger has no daemon face and `--daemon` is refused by name |
| merger E2E waits on a daemon's log/tick from outside | `mdParseDaemonStart` + `mdProcAlive` + `mdStopPID` in a defer | **synchronous run + ledger assertion**: the process has already exited before any assertion can fail |
| the singleton-guard test needs a long-lived lock holder | a resident `--every 5m` daemon, reaped by `mockproc` | **this test holds the real flock in-process** (`acquireServeLock()`), released by `defer lock.Close()` |
| the cwd-escape test measures a detached daemon | `gopi merger --daemon` | `gopi worker --daemon` — **the surviving face**, same mechanism, same measurement |
| the worker daemon face | resident | **still resident** (see D1 — deliberate, not an oversight) |

The claim I can make with evidence: **no merger test path can leave a resident
gopi behind**, because no merger test path starts one. The **worker** daemon face
still exists and its tests still start real resident workers — that is D1 below,
an explicitly recorded decision, not a silent remainder.

## 4. Classification (A delete / B keep-under-cron / C merge logic / D undecidable)

### A — daemon-only, DELETED (4428 lines)

| file | lines | why it is A |
|---|---|---|
| `cmd/agent/cli_merger_daemon.go` | 446 | the `--daemon` fork/exec-self + setsid + pipe handshake + inherited flock of the **merger** |
| `cmd/agent/cli_merger_stop.go` | 722 | the merger's stop/restart/start/force/wait control surface — acts on a resident service |
| `cmd/agent/merger_graceful.go` | 220 | the resident loop's stop-signal vocabulary, handover wait, stop-request plumbing |
| `cmd/agent/merger_daemon_flag_e2e_test.go` | 586 | subject = the `--daemon` face (its **rig** survives, see below) |
| `cmd/agent/merger_graceful_restart_e2e_test.go` | 841 | subject = graceful restart/handover of a resident merger |
| `cmd/agent/merger_lock_ownership_e2e_test.go` | 781 | subject = lock ownership across daemon generations |
| `cmd/agent/merger_stop_target_e2e_test.go` | 181 | subject = `--stop` target selection |
| `cmd/agent/merger_stop_force_modifier_test.go` | 133 | subject = the stop request's `--force` modifier |
| `cmd/agent/merger_status_next_tick_e2e_test.go` | 254 | subject = "when does the resident loop tick next" — a one-shot has no next tick |
| `cmd/agent/daemon_scope_restart_e2e_test.go` | 103 | subject = scope escape on **restart** of a resident merger |
| `cmd/agent/control_conflict_flags_test.go` | 161 | subject = the **merger's** read+act flag collision (the surface that collided is gone) |

Also trimmed inside surviving files (not whole-file deletions):
`internal/tools/merger_graceful.go` — `MergerPidAlive` (a delegate read by
nobody once the stop path went), `MergerSignalName` / `MergerSignalNames` /
`mergerStopSignals` ("exit at the tick boundary" needs a next boundary), and
`MergerHandoverWaitDefault` (the successor-restart window; nothing restarts a
merger now).

### B — needed under cron, KEPT and REACHED by the tick

`runTaskMerger` / `runTaskMergerWithLock` (rewritten, not deleted),
`acquireServeLock` + `ensureServeLockIdentity` + `OrphanLockHolder` (the
single-instance guard and the 2026-09-12 stray-holder refusal), the `--status`
read-only surface, `attachHostLifecycleLog`/`closeHostLifecycleLog`,
`scratchGCSweep`, `runTaskLifecycleTick`, `mergerMasterSafetySweep` +
`tools.MergerForcedSafety`, `mergerLogPath`/`mergerLogBaseName`.

### C — merge business logic, UNTOUCHED

`task_merge`, the merge gates (`merge_build_gate*`, `merge_contract_gate*`,
`merge_union_gate*`, `merge_revert`), `repo_sweep*` + `repo_sweep_park*`,
`task_reclaim*`, `task_finish_cleanup`, `worktree_sweep`, `branch_attribution`,
`rebuild*`, `task_adoption`. Every safety/correctness guard survived; nothing
was weakened to make a deletion convenient (the reclaim dirty-worktree refusals
are untouched).

### D — NOT judged, needs nick (never guessed, never deleted)

- **D1. The `gopi worker --daemon` face and its daemonizer.**
  `cmd/agent/cli_daemon_spawn.go` is **shared** by the merger and the worker.
  Evidence I gathered: production workers on this host are launched by
  `scripts/gopi-worker-up.sh` as `setsid nohup gopi worker` — **not**
  `--daemon`; and the running workers here are `gopi worker <uuid>` (positional
  since epic worker-positional-taskid) under an
  operator login shell. But the worker's daemon face has a full live control
  surface (`cli_worker_control.go`, `cli_worker_status.go`), its own tests, and
  `devdoc/worker-ownership.md` documents it as OPERATOR-ONLY. **Whether the
  worker keeps a resident form is a different architecture decision** from
  "merger is a cronjob", so I did not take it. Consequence, stated honestly: the
  worker's tests still start real resident worker daemons, and the
  daemon-lifetime net is still what protects those. If the answer is "no", the
  follow-up is the same shape as this epic, scoped to the worker.
- **D2. `internal/daemonlifetime` + `internal/testutil/daemonlifetime.go`,
  `lifetimeprobe.go`, `orphansweep.go`, `daemonlifetestsupport.go`.** All of
  this is bound to the worker daemon face (D1). It becomes A the moment D1 is
  answered "no resident worker".
- **D3. `internal/cgroupscope` + `cli_daemon_scope.go` + `daemon_cgroup_e2e_test.go`'s
  worker cases.** Same dependency: the scope escape is a daemon concern and the
  only daemon left is the worker's.
- **D4. `internal/tools/merger_schedule.go` + `merger_schedule_test.go` (525 lines
  of tests).** This derives "when does the next tick happen" from a **measured
  sleep boundary** in the holder's log/argv. Under the cron form the schedule is
  the **host's** (crontab), not the process's, so the honest answer is probably
  "delete, and let `--status` report the lock holder without predicting a tick".
  I did **not** do it because the render has a live reader (`merger_status.go`
  and its five test files) and the replacement is a **user-visible** surface
  change — it belongs in the same breath as D1's answer, and guessing it would
  have meant rewriting the status contract unasked.
- **D5. `cmd/agent/merger_top_level_e2e_test.go`.** Surviving subject (the bare
  verb, `--once`, `--no-branch-scan`), so I kept it — review worth a look when
  D1/D4 land.

### The wording fence (what was deliberately NOT reworded, and why)

`internal/signalrec` (`signalrec.go`, 8 occurrences), `internal/daemoncwd`,
`internal/testutil/mockproc.go` and `internal/standards/cwd_release_gate_test.go`
describe the **process family** a resident role belongs to ("a worker loop, a
merger daemon, a `gopi task run` child"). The worker still HAS a resident form
(D1), so the family and the wording are both still true — rewriting them would
be pre-empting D1's epic from inside this one. The fence is: **reword where the
text is about the MERGER specifically; leave the family descriptions to the epic
that answers whether the family still has two members.**

### Incidental matches — deliberately NOT in scope (the honest boundary)

`grep -l 'daemon|merger'` hits ~200 files. Only a small set belongs to the
**lifecycle layer**. These mention merger/daemon and are **not** lifecycle:
`internal/agent/taskheartbeat.go`, `taskregister.go`, `crashdump_sweep_gate.go`
(worker lifecycle records); `internal/shot/x11.go`, `internal/procname`,
`internal/signalrec`, `internal/runcmd` (process utilities the worker uses);
`internal/tools/bash_daemon_test.go`, `job.go` (the *bash tool's* long-running
job); `internal/heavytest`, `internal/scratch/*`, `internal/testutil/run.go`,
`mockproc.go`, `orphansweep.go` (test infrastructure); every
`merge_build_gate*`/`merge_contract_gate*`/`task_merge*`/`repo_sweep*` file
(**C**, the merge logic itself); `internal/config/*` (the `repos` table);
`internal/standards/baseline.go` (gate baselines). Touching any of these would
have been scope creep against a live merge path.

## 5. Tests — nothing, silently, dropped

**A test of an A capability is deleted with its capability.** Each of the 11
deleted files above has a subject that no longer exists.

**Every test of a B capability is kept or rewritten** — no silent drops:

| test | what happened |
|---|---|
| `merger_tick_no_child_gate_test.go` | kept; **re-pointed** from the deleted `merger_graceful.go` to the extracted `merger_tick.go` (its presence control caught a wrong assumption of mine and failed red — the gate works) |
| `task_lifecycle_tick_test.go` | **rewritten** for the cron form; both real-process acceptances now run a **synchronous** tick (`mdRun(t, bin, env, CliVerbMerger)`) and assert the ledger, so *the call returning at all* is the non-residency proof — the old resident form's acceptance had to wait on a live process, which is exactly what no longer exists |
| `merger_singleton_e2e_test.go` | **rewritten**: the guard now takes the real flock in-process |
| `cli_daemon_cwd_test.go` | **re-pointed** to the surviving `worker --daemon` face (same mechanism, same measurement) |
| `daemon_cgroup_e2e_test.go` | worker cases kept intact; the **merger** cgroup case deleted with its subject |
| `status_action_conflict_e2e_test.go` | worker face kept; the merger rows went with the surface, so `sscFaces` is now worker-only and the dead merger scaffolding (`sscFaceMerger*`, `sscDaemonRefusalTokens`, `sscRefusalTokensFor`, the now-unreachable `CliVerbMerger` arm of `sscFaceVerb`) was removed rather than left as a table nobody reads. `TestAliasDaemonStatusIsRefused` is **kept and still green**: it is the one merger-shaped case whose property survives the machinery ("never a silent no-op with exit 0, decided before anything is written") |
| `cli_tasks_exec_console_test.go` | merger marker row deleted; the worker row kept |
| `host_lifecycle_log_test.go`, `usage_test.go`, `merger_status_cli_test.go`, `merger_work_lock_e2e_test.go` | kept; symbol renames (`mergerDaemonLogBaseName` → `mergerLogBaseName`, `mergerDaemonFlagLong` → `daemonFlagLong`). `merger_status_cli_test.go` moved off its own private rig and onto the **worker** rig (`wdBin`/`wdSettings`/`wdEnv`/`wdRun`/`wdPromptBudget`) — it only ever needed "a private profile with no daemon", which is exactly what that rig mints (Rules 2/13) |
| `internal/tools/merger_graceful_test.go` | the `MergerPidAlive` unit test went with the delegate; **replaced** by two `MergerForcedSafety` cases (clean repo untouched; unfinished merge repaired, branch ref untouched) |
| `task_child_fail_guard_test.go` | the deleted exit code dropped from the distinctness guard; siblings still covered |
| `merger_daemon_flag_e2e_test.go`'s **rig** (`md*` helpers) | **extracted to `merger_test_rig_test.go`** (9 decls: `mdSettings`, `mdBin`, `mdRun`, `mdProcAlive`, `mdIsZombie` + the `mdLogWait` / `mdPollInterval` / `mdScratchGCThrottleMin` consts and the `mdBinCache`/`mdBinBuildEr` pair) — three surviving suites depend on it, so deleting the file wholesale would have taken their rig and silently taken the tests with it. Extraction was **by use**, not by copy: `mdEnv`, `mdPromptBudget`, `mdForegroundProbe`, `mdStopPID`, `mdParseDaemonStart`, `mdWaitLockPid` and `mdWaitLogContains*` were **dropped** (verified: no `func md<name>` for any of them survives anywhere in `cmd/agent`), because their only callers were the deleted daemon cases. Dropping them is the point — a helper that waits for a RESIDENT process to flush a later line has no meaning against a process that has already exited, and leaving it in the rig would re-advertise the capability the epic removed |

**NEW** — `merger_retired_flags_test.go`, on the real binary: every retired flag
(`--daemon --stop --restart --start --wait --force --every`) is **refused with a
non-zero exit that names it and says RETIRED**, the usage head documents the
class, and a **bare `gopi merger` is not refused** (one tick, exit 0). This is
Rule 15's own failure mode: an operator's cron line must never be answered with
exit 0 + a single tick while they believe a service runs.

## 6. Verification

```
go build ./...      → ZERO OUTPUT
go vet ./...        → ZERO OUTPUT   (vet compiles _test.go — the master-breaking lesson)
go test ./...       → see section 8
```

No deleted symbol is referenced by a live path. The exhaustive check is a grep
for every deleted symbol over the WHOLE tree (not just `*.go`):

```
$ grep -rnE 'cliMergerDaemon|runMergerGraceful|mergerGracefulStop|mergerGracefulRestart|
             mergerGracefulHandover|mergerStopSignals|MergerSignalName|MergerSignalNames|
             MergerHandoverWaitDefault|MergerPidAlive|mergerStopTarget' . | grep -v '^./salvage/'
(only historical comments that NAME what was removed and why)
```

### 6a. The daemon-ERA WORDING sweep (a stale comment is a wrong instruction)

Deleting the code is not enough: ~30 surviving comments, help texts and
user-visible refusal strings still described the merger as a **resident daemon**
and told an operator to run flags that the binary now refuses. A comment that
says "the merger daemon is already running" is not stale decoration — it is the
message an operator reads while debugging a lock refusal, so it was swept in the
same change:

| surface | was | now |
|---|---|---|
| `serveLockHeldRefusal` (user-visible refusal) | "another merge **daemon** is already running … two mergers never double-merge" | "another merge **process** is already running … two merger passes never double-merge" |
| `serveLockStrayHolderRefusal` (user-visible refusal) | "another merge **daemon** (pid %d) … a second daemon must not start" | "another merge **process** (pid %d) … a second merger must not start" |
| `gopi task merger` alias notice (stderr) | "the merge daemon is now the top-level …" | "the merger is now the top-level …" |
| `internal/config/help.go` command index | `gopi merger [--daemon]` … "--status \| --stop \| --restart \| --start [--force] [--wait D] drive the running daemon" | `gopi merger [--once]` … "Merge one tick and exit (a CRONJOB, not a daemon) … --status reports read-only whether a tick holds the lock" |
| `tasksUsage` "Deprecated spellings" paragraph | "including the daemon's action controls, whose one documented home is 'gopi merger -h'" | "The merger has no action controls any more (it is a one-tick cronjob)" |
| `gopi merger -h` / `tasksUsage` / README-class comments in `cli_tasks.go`, `cmd/agent/main.go`, `cli_verbs.go`, `scratch_gc_tick.go`, `task_lifecycle_tick.go`, `cli_tasks_rebuild.go`, `cli_tasks_worker.go`, `cli_tasks_worker_stop.go`, `cli_tasks_exec_output.go`, `internal/tools/merger_status.go`, `internal/tools/rebuild_vet_gate.go`, `internal/tools/service_control.go`, `internal/tools/merger_graceful.go`, `internal/taskverbs/verbs.go`, `internal/runcmd/runcmd.go` | "resident daemon", "merge daemon", "SERVER-ONLY merge daemon", "the daemon's action controls" | "one-tick pass", "the merger", "SERVER-ONLY merge pass", "no action controls any more" |
| test comments in `daemon_cgroup_e2e_test.go`, `task_lifecycle_tick_test.go`, `cli_tasks_zombie_dispose_test.go`, `worker_idle_drained_stop_e2e_test.go`, `merger_singleton_e2e_test.go`, `merger_work_lock{,_e2e}_test.go`, `merger_top_level_e2e_test.go`, `serve_worker_split_test.go`, `cli_contracts_test.go`, `usage_test.go`, `cli_tasks_advert_gate_test.go`, `internal/tools/{lock_evidence,merger_status}_test.go` | the merger described as a daemon | the merger described as a one-tick pass |

Each pinned by a test, and the pins moved with the wording:
`serve_lock_errno_test.go` and `serve_lock_diagnosis_liveness_test.go` assert the
refusal text; `usage_test.go` now asserts the index row carries `--once` and
**does not** carry `--daemon`; `cli_tasks_advert_gate_test.go` dropped `--every`
from `documentableServiceFlags`, because a flag no face accepts cannot be
re-advertised.

**A wording sweep is a scope decision, not a free win.** The line drawn: a text
is swept when it is (a) reachable by an operator, or (b) a comment in a file this
change already touches. `internal/signalrec/signalrec.go`'s "the merger daemon"
(a resident-role argv spelling), `internal/testutil/mockproc.go` and
`internal/daemoncwd` describe the process family generically and are **D1**'s
territory — the worker still has a resident form, so the wording is still true
for it, and pre-emptively rewording another epic's surface is how a scope grows.

### 6b. Two gate interactions, both handled by root cause

**(i) The Rule-16 baseline is line-addressed, and `cmd/agent/main.go` shrank.**

`internal/standards` gate 1 reported `hits=9 baselined=6 escaped=2 NEW=1` — one
baselined `%v` wrap had become unbaselined. Root cause, in 5-why order: the gate
baselines a **file:line**, the merger dispatch block it removed was 30 net lines
*above* the baselined call in `cmd/agent/main.go`, so the call moved
`987 → 1008` and the permit stopped matching. The call itself is byte-identical
and its reason is unchanged (its only `%v` arguments are the `payloadFilePrefix`
STRING constant, twice; the error IS `%w`-wrapped).

The fix is a **re-key**, not a permit-by-deletion: deleting the entry would have
licensed a genuinely new `%v` wrap at that address later. The entry is re-pointed
with the reason re-measured (one grep: the marker string occurs on exactly one
line), and the count is unchanged — 9 hits, 7 baselined, 2 escaped, **0 NEW, 0
STALE, 0 permits added** (verified against the pre-change commit as a control:
`64f4345a` reports `9/7/2`, this branch reports the same).

**And then it went stale a second time, one commit later — which is the actual
lesson.** The re-key above was measured on `fb877170` and written as `1008`. The
next commit on this same branch (`75a6d989`, restore the `--once` completion line
and the report digest surface) added 3 net lines to `cmd/agent/main.go` *above* the call,
so the FINAL tree has it at `1011`: the gate then reported the SAME call as
**NEW at 1011** and the permit as **STALE at 1008**, in one run. A line-addressed
baseline has to be **re-measured in the branch's final tree**, not at the commit
that first moved the site — a re-key written mid-branch is a permit that expires
on the next commit. (Symptom to recognise: `NEW` and `STALE` at *neighbouring*
lines of one file is always a shift, never a genuinely new violation — the
neighbouring pair is the signature of a misplaced permit, and the fix is a
re-key.) Re-keyed to `1011` on the final tree; master's key for the same
byte-identical call is `1038`.

**(ii) `internal/stylegate` fails — and it fails IDENTICALLY without this change.**

Measured, not assumed. A control worktree at the **base commit** (`64f4345a`)
and this branch were run through the same command,
`go test -run TestStyleGate ./internal/stylegate/`, and their reported rule
lines are **byte-identical** (empty `diff`):

```
stylegate errorf-verb:     8 production hit(s), ceiling 7
stylegate ignored-error:   5 production hit(s), ceiling 0
stylegate ignored-error: 170 test hit(s),      ceiling 163
stylegate unchecked-close: 1 test hit(s),      ceiling 0
```

So the stylegate ceiling drift is **pre-existing on master**, not caused here —
the same conclusion `internal/testutil`'s failures reach below. Fixing it is a
separate task (the ceilings are a ratchet someone else's merges outran); claiming
it here would mean re-baselining another epic's violations.

### 6c. `internal/testutil` fails on the CONTROL too

`TestEveryTempUsingSuiteIsPinned`, `TestPinCoverageTable`,
`TestScriptsAreBoundedAndOffTmp`, `TestWorkerOwnership*` fail at the base commit
as well (control run recorded in the task report). Same conclusion as 6b(ii):
pre-existing, unrelated to this change, not touched.

## 6d. The one thing this change did that it did not intend (an incident, recorded)

While verifying the new tick against the REAL binary, the verification command
was sandboxed the only way gopi's docs make obvious — a private `GOPI_HOME` and
a private `TMPDIR`. **That is not enough, and the sandbox swept the operator's
live repositories.**

The mechanism: the repos a tick sweeps do NOT come from `GOPI_HOME`. They come
from `config.ResolveMergerRepos()` → the `repos` table of the **user's**
settings.json (`GOPI_SETTINGS` / `DefaultSettingsPath`), i.e.
`/home/user/work/GoPi` and `/home/user/Downloads/games/Maps`. A private
`GOPI_HOME` sandboxes the DATA ROOT (the lock, the log, the ledger) and nothing
else — the sweep targets were still the real ones.

What the accidental tick did to `/home/user/work/GoPi` (measured; master was
never moved — `merged=0`, no ref updated, no ledger row written):

| effect | state now |
|---|---|
| 4 branches parked → an `archive/<branch>` tag added | **reverted**, and the record corrected: this master's park KEEPS the branch ref (`parked at archive/<branch> (branch ref kept, resolvable by an ordinary worker task)` — measured in the tick's own stdout), so no branch was lost. The 3 tags it created for branches that are NOT decided losers (`archive/feat/merger-park-tip-dedup`, `archive/feat/unsourced-kill-provenance`, `archive/fix/worker-termination-attribution`) were deleted; in reverting I also deleted their branch refs by mistake (reading the tag list as proof a branch had been dropped), and re-created all four from their own commits — the four refs are byte-identical to their pre-tick tips |
| 1 worktree released (`/home/user/workspace/gopi-d2-terminal-guard`, a live worker's) | **recovered by the worker itself**: it re-created the worktree two minutes later, its commits are intact (`a86094c2`, a descendant of the parked tip), and its branch → worktree binding is what it was |
| a `version/v20260917-0159-c890b0fe` tag minted | tolerated — that is a factual name for the current master tip, and a later round mints nothing for a position already named (`already named by …`) |
| `bin/gopi` rebuilt to follow master | correct, not damage: it was rebuilt FROM master at the tip it claims (`v20260917-0159-c890b0fe (c890b0fe0ccb)`) |

**The rule this incident leaves behind (stored in memory, id 3432).** A merger
tick cannot be sandboxed by `GOPI_HOME`; the only safe sandbox is a settings
file whose `repos` table points at temp repos (`GOPI_SETTINGS`), and the only
risk-free invocation for argv/flag verification is one that never reaches a
tick at all (the retired-flag refusals, `-h`, `--status`). This is a real trap
for the NEXT agent too, because the obvious isolation reads as sufficient.

**It is also evidence FOR this change, not against it.** The incident is the
daemon-era failure mode reproduced by the *hand*: a merger that nobody asked to
run acted on the live repo. Under the cron form the same sweep runs on a
schedule its owner set — but the blast radius is now visible in one place (one
tick's stdout, one `[merger] /path … merged=N parked=M` line per repo) instead
of hidden behind a resident process's background poll.

**A second, sharper trap, found the same way.** Running the suite from the
worktree made it delete ITSELF: `go test ./cmd/agent/` twice removed
`/home/user/workspace/gopi-daemon-removal` mid-run, and ~53 tests then failed
with `getwd: no such file or directory` (the test binary's cwd was gone). The
cause is the same one as above and it is NOT specific to my change — the
surviving E2E suites build a REAL binary and run REAL ticks
(`merger_top_level_e2e_test.go`, `merger_singleton_e2e_test.go`), and a tick's
own-worktree-release pass enumerates the repos from the USER's settings table.
Some suites call `SandboxRepoContext(t)`; the ones that reach the live repo
anyway (and any suite path that takes the tick without that call) can recycle
the developer's checkout underneath the running test.

The lesson is operational and it is the rule this session leaves behind:

- **Run this package's suite from a CLONE, not from the worktree.**
  `git clone --no-hardlinks --branch <branch> <repo> <dir> && cd <dir> &&
  go test ./cmd/agent/` — a clone registers no linked worktree, so nothing can
  release it. Measured: the same suite is green in 159s from a clone, and it
  destroyed the worktree twice when run in place.
- **A worktree is a checkout, not the deliverable.** The commits live on the
  branch (Rule 6c); if a run recycles the checkout, `git worktree add <path>
  <branch>` restores it exactly. Nothing was lost either time — but only
  because the work was committed first, which is the habit to keep.

## 7. Deferred / NOT done (honest)

1. **D1–D5**: judged undecidable, listed above with the evidence I did gather.
   None was deleted, none was guessed.
2. **`internal/tools/merger_schedule.go`** is now unreachable from the tick but
   still read by `--status`; splitting it is D4.
3. The **worker daemon face** still starts resident processes in tests, protected
   by the daemon-lifetime net rather than by construction (D1).
4. **`internal/tools/merger_schedule.go` + `merger_status.go`'s "next tick"
   render** still predicts a tick from a measured sleep boundary. Under the cron
   form the schedule belongs to the host, so this render is the most likely next
   wrong answer an operator reads — D4, and it wants D1's answer first.
5. The **worker daemon E2E still holds its own private rig** (`wd*`), near-duplicated
   from the merger rig this change extracted. The two were deliberately NOT merged
   here: that would tie a surviving worker suite's compile to a file about the
   deleted merger face. Merging them belongs to D1's epic.
6. **`salvage/`** holds a stale checkout referenced by nothing; untouched.
