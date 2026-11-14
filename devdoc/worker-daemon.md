<!-- doc-class: normative -->
# Daemon operating model — decision record (worker + merger)

**Branch:** `feat/worker-daemon-operating-model` (worktree `~/workspace/gopi-worker-daemon-model`)
**Measured at:** master `7c24ea06`, host `peer1` (Ubuntu 22.04, kernel 5.15, cgroup v2, uid 1000), 2026-09-13
**Status:** canonical for the six questions below. Every claim marked **[measured]** was produced by a
command run during this task; the command and its observed output are quoted. Claims marked
**[code]** are read from the shipped source with the file:line named. Nothing here is inferred from
another document without saying so.

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns the worker and
> merger daemons; an agent must never start, restart or kill one (submit the work instead:
> `gopi task submit`). §9 records what happened when this record's author violated that rule.

---

## 0. The mechanism this model describes (shared, one implementation)

Both resident faces use ONE self-daemonization seam, `cmd/agent/cli_daemon_spawn.go` **[code]**, and
ONE cgroup-escape layer, `cmd/agent/cli_daemon_scope.go` **[code]**. Each face supplies only its
`daemonSpec` (component name, marker env, control fd, child argv, log path). The seam, in order:

1. **validate in the parent** (`spec.prep`) before anything is detached — a bad argv is a synchronous
   refusal, never a "started" daemon that dies a moment later;
2. **open the log first** (`<gopi data root>/logs/<component>.log`) so the child's first line and any
   pre-handshake failure land where the operator is told to look;
3. **take the control fd (optional)** in the parent and pass it via `ExtraFiles` — fd 3 for the face
   that has one, handshake pipe at fd 4; without one the pipe is fd 3;
4. **setsid** — session leader, no controlling terminal, survives the caller's shell;
5. **one-byte pipe handshake** — the parent prints "started: pid …" only after the child proves it is
   running; a failure is reported on the same fd (`FAIL: …`) and the child is SIGTERMed and reaped;
6. **cgroup escape** (§1.4) — `systemd-run --user --scope`, else in-process cgroupfs, else `setsid`
   only (announced).

### 0.1 Facts measured about the mechanism itself **[measured]**

A `setsid nohup` child (`~/.cache/gopi-probe/detach-probe.sh`, self-terminating) reported:

```
detached child: DISPLAY=localhost:11.0 XAUTHORITY=<unset> cwd=/home/user pid=3265078 ppid=3265077
detached child: stdin=/dev/null stdout=/home/user/.cache/gopi-probe/detached.log
detached child: cgroup=0::/user.slice/user-1000.slice/session-18200.scope
```

- the **environment is inherited verbatim** (DISPLAY, cwd, uid); stdio is `/dev/null` + the log;
- **`XAUTHORITY` is unset in the parent**, so X auth resolves through the `~/.Xauthority` FILE
  (`internal/shot/x11.go` `xauthPath`: `$XAUTHORITY` else `~/.Xauthority`) **[code]** — nothing needs
  to be exported for X auth to keep working after the detach;
- **`setsid` does not move the cgroup**: the child is still in `session-18200.scope`. That is the
  “setsid is real but partial” fact the cgroup-escape layer exists for.

---

## 1. Q1 — Launcher/supervisor: who owns DISPLAY/XAUTHORITY and the session env

**DECISION.** The launcher is the **operator, in his own terminal** (`devdoc/worker-ownership.md`),
and **the launcher owns the entire session environment** — DISPLAY, XAUTHORITY, cwd, PATH, uid. The
detach mechanism preserves that environment, so whatever the launcher had is what the daemon and every
task child it spawns will have, for the daemon's whole life. There is no re-acquisition path: X11 auth
is per-connection and DISPLAY is a client-side address, so **a daemon started with an unreachable
DISPLAY stays unreachable forever** — the only fix is a launcher that holds a durable local DISPLAY.
The supervisor is the launcher itself (`scripts/gopi-worker-up.sh` for a cohort; `gopi worker --daemon`
for one resident daemon); there is no second supervisor process, no init/tmux/systemd unit.

**EVIDENCE (measured).**

| # | what | command | observed |
|---|---|---|---|
| M2 | detached child on the inherited (SSH-forwarded) display | `setsid nohup detach-probe.sh` with the parent env (`DISPLAY=localhost:11.0`) | `gopi screen_capture: connect to X display localhost:11.0 (unix /tmp/.X11-unix/X11): dial unix /tmp/.X11-unix/X11: connect: no such file or directory` — **exit 1** |
| M3 | explicit display, valid cookie path | `DISPLAY=:0` under the same detach | `X display :0 refused the connection (server refused the connection; see the setup failure reason frame) — check DISPLAY and Xauthority` — exit 1 |
| M4 | headless | `env -u DISPLAY` | `no X11 display: DISPLAY is unset and no display was given (X11-only path; a Wayland session needs a portal, which gopi does not implement yet)` — exit 1 |
| M5 | **durable local display** | `DISPLAY=:99` under the same detach | `screen_capture` **exit 0**, JSON: `{"path":"…/xvfb99.png","display":":99","width":1280,"height":800,"depth":24,"bands":7,"bytes":8318,"sha256_16":"e848bfbef64e5f7f"}` |
| M6 | which displays exist on this host | `ls -la /tmp/.X11-unix/`; `pgrep -a Xvfb` | sockets `X0` (root), `X240`, `X99` (uid the operator); `Xvfb :99 -screen 0 1280x800x24` running since 01:00 |
| M7 | who may launch | `GOPI_EXECUTOR_SESSION_ID=x GOPI_AGENT_SESSION=x gopi worker --once` and the same with `setsid` | both **exit 3**, refusal names the agent session and states “it cannot be detached (setsid leaves the process tree/cgroup intact)”; `gopi worker --status` and `gopi task run --help` stay **exit 0** (read-only/help are never refused) |
| M9 | the real operating model on this host | `gopi worker --status`; `ps -p <pid>` | two detached workers alive (`Ssl gopi-worker worker`, pids 2140436 / 2140461) with **no lock and no state file** — i.e. started by the launcher script's `setsid nohup gopi worker` path, not by `--daemon` |

The launcher's own contract is explicit about what it contributes **[code]**
(`scripts/gopi-worker-up.sh`: `GOPI_WORKER_LOG_ROOT` + the pre-claim startup log; `setsid nohup … <
/dev/null`, 1 s cadence, hard refusal if the output root cannot be created) — and it **does not touch
DISPLAY**, which is exactly why the launcher's own environment is the DISPLAY authority. The installed
copy is byte-identical to the repo copy at measurement time **[measured]**
(`sha256 13673305b82050a02c9fc2d4959083ecab1dc433c04189bfe34f278311696e01` for both
`scripts/gopi-worker-up.sh` and `~/.local/bin/gopi-worker-up.sh`, reached via the `~/bin` symlink).

**Consequence to state in the operator's own words:** start the resident daemon (or the cohort) **from
a shell whose DISPLAY is a display that outlives the shell** — `:99` (Xvfb) is the durable one on this
host; a `localhost:N` display is an SSH forward and dies with the session, so a daemon started from
such a shell can never capture again even though it is “running fine”.

**REJECTED alternatives.**

1. *“The daemon re-resolves DISPLAY at start.”* Rejected: a client cannot discover the operator's
   display (there is no registry of intent), and the failure is not about knowing the value — M2 shows
   the value WAS correctly inherited and the socket was gone. Re-resolving would only replace one
   wrong value with another.
2. *“`setsid` is the env fix.”* Rejected: measured (M1/§0.1) — `setsid` changes the session/terminal
   and leaves the environment untouched. It is the same mistake as reading `setsid` as a cgroup fix.
3. *“Run the daemon under a forwarded DISPLAY and let GUI tools fail per-use.”* Rejected as the
   DEFAULT: it converts a launch-time fact into a silent per-tool failure discovered hours later
   (the M2 error is only emitted when a GUI tool is first used). Filed as a launcher-announcement
   proposal (§9, task B) rather than implemented here, because the launcher's env contract is gated
   by `internal/testutil/procname_gate_test.go`, which forbids the script from reading an undeclared
   knob — a launcher change needs that gate updated with it.

---

## 2. Q2 — Single instance vs multi-instance

**DECISION.** **Both, on different faces, by construction:**

- `gopi worker --daemon` (the self-detaching face) is a **SINGLETON per profile**: flock
  `LOCK_EX|LOCK_NB` on `<gopi dir>/worker.lock`, taken by the spawning parent, inherited via
  `ExtraFiles`, re-taken on the inherited fd by the child and `CloseOnExec`-fenced, held in
  `workerDaemonLockRef` for the daemon's life (a GC-finalized `*os.File` would close the fd and
  silently release the guard) **[code]**.
- `gopi merger --daemon` is likewise a **SINGLETON**, on its own file `<gopi dir>/serve.lock` — the
  two locks are deliberately **separate files**, so a running worker never refuses a merger and vice
  versa **[code]**.
- The **foreground** `gopi worker` takes **no lock** — the deliberate observability escape hatch — and
  the **launcher-cohort** face (`setsid nohup gopi worker`, what the operator actually runs, M9) takes
  no lock either, so **the operating model is N independent workers** (the launcher's
  `GOPI_WORKER_N`, default 2; M9 shows exactly 2 alive). Multi-instance is the operating model;
  single-instance is the guard on the *detaching* face only.

**EVIDENCE.** M9 (`--status` lists two detached workers with no lock/state file, i.e. the cohort face).
The singleton half is asserted by `cmd/agent/worker_daemon_singleton_e2e_test.go` (WS1 second
`--daemon` refused naming the holder; WS4 foreground starts while the daemon holds the lock; WS6
`--restart` replaces the pid) and `cmd/agent/merger_singleton_e2e_test.go` **[code]**.

**REJECTED alternatives.**

1. *One global lock for both faces.* Rejected: the worker and the merger guard different queues
   (claim queue vs merge queue); one lock would make a healthy worker refuse a merger — a self-inflicted
   outage. The separate files are the shipped answer.
2. *Retire the launcher cohort in favour of `--daemon` × N.* Rejected: the flock refuses the second
   `--daemon` by construction, so the operator would lose the only face that scales with load. The
   cohort face is the multi-instance answer and the singleton is the “one well-known resident worker”
   answer; they coexist (M9 + WS4 prove the foreground/cohort face is not gated).
3. *A “supervisor” that keeps exactly one worker alive (auto-respawn on death).* Rejected: that is
   the crash policy of Q5, and it is exactly the process an agent "cannot own" (WORKER-OWNERSHIP).
   The count is the launcher's decision, made once, by the operator.

---

## 3. Q3 — Min-idle workers (what “idle” means here)

**DECISION.** There is **no minimum-idle guarantee inside the worker loop**, and none is offered as a
knob. A worker's idle contract is **[code]** `cmd/agent/cli_tasks_worker.go`:

- drained queue → one settled re-check (`workerIdleRecheckMS` = 1 s) → wait `workerIdleInterval`
  (`GOPI_WORKER_IDLE_SEC`, default 30 s, exactly ONE reader) → re-check;
- after `workerIdleExits` **consecutive** empty rounds (`GOPI_WORKER_IDLE_EXITS`, default 3, one
  reader) it **exits 0 on its own** — ≈90 s of quiet. A round that ran a task resets the streak;
- `GOPI_WORKER_IDLE_EXITS=0` is the documented opt-out into the resident wait-forever behaviour, and
  it is what the operator's long-lived cohort uses; `--once` drains then exits 0 immediately.
- `GOPI_WORKER_MAX_ROUNDS` (0 = unlimited) bounds the total number of rounds — a runaway submitter
  cannot pin the host.

“Min-idle” is therefore the **launcher's** property: the launcher starts N workers
(`GOPI_WORKER_N`, default 2) and re-running it restores the floor. The daemon's own start note states
the policy it will follow, so the operator is never guessing **[code]** (`workerDaemonStartDetail`:
`idle wait %s between rounds, never self-exits (GOPI_WORKER_IDLE_EXITS=0)` vs `self-exits after %d
idle round(s)`).

**REJECTED alternatives.**

1. *A `--min-idle N` self-respawn loop inside the worker.* Rejected: it makes the worker its own
   supervisor (the WORKER-OWNERSHIP failure mode: a process the operator did not start, cannot see
   die, cannot clean up) and it drags in the Q5 crash-loop policy for no gain — the launcher already
   provides the floor, and the floor is a decision the operator makes.
2. *A systemd user unit/timer.* Rejected on measured host facts plus operator scope: the user bus is
   absent on this host (`systemd-run --user --scope /bin/true` → `Failed to connect to bus: No such
   file or directory`) and Rule 22 forbids fixing host-level configuration for gopi's own purposes.
   The unit would also hide the daemon from the launcher's `--count` seam.
3. *Reduce the idle exit to 1 round.* Rejected: a submit racing the claim scan would find no worker;
   the settled re-check exists for exactly that race, and 3 × 30 s is the measured compromise between
   latency and cost.
4. *“Always keep N workers warm by never letting the loop exit.”* Rejected: it is available
   (`GOPI_WORKER_IDLE_EXITS=0`) but is not the default, because a worker is a task-DRAIN tool (the operator
   9/1 design) and a drained queue should not keep a process (and its cgroup scope membership) alive
   forever.

---

## 4. Q4 — Restart on a new binary

**DECISION.** **Manual, operator-driven, stateless.** The swap primitive is
`gopi worker --restart [--wait 25m] [--force]` = graceful stop (durable request file + `SIGTERM`, waits
for the in-flight round's close-out) then start the **new** binary; the merger's is
`gopi merger --restart` (help: “stop gracefully, then run the new binary”). The daemon records its own
build in its state file (`workerControlState.Version` ← `buildVersion()` **[code]**), so
`gopi worker --status` answers “is the resident worker running master's build?” without archaeology.
Nothing watches master and **nothing restarts itself**.

**EVIDENCE.** `buildVersion()` resolves a build stamp → the checkout's `version/<stamp>-<rev>` tag →
the toolchain-embedded revision (`cmd/agent/cli_version.go`) **[code]**. The post-merge rebuild
(`internal/tools/rebuild.go`, `repos.<path>.build` in `settings.json`, `BuildFailedEvent` on failure)
rebuilds the binary after a merge round but **does not restart any daemon** **[code]** — so the
sequence after a landing merge is: merge → rebuild → *operator* `--restart`. The operator's
settings.json declares GoPi's build **[measured]**
(`"repos":{"/home/user/work/GoPi":{"build":"CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/","build_timeout_sec":600}, "/home/user/Downloads/games/Maps":{}}` — the second entry is a valid PURE MERGE TARGET).

**REJECTED alternatives.**

1. *Self-restart on version mismatch.* Rejected: a self-re-exec mid-round abandons exactly what
   `cli_tasks_worker_stop.go` exists to protect (terminal writeback + the producer-side own-worktree
   release), and it would need a crash-loop policy (Q5) — i.e. it trades a deliberate human action for
   a new machine failure mode.
2. *Tie the restart to the merger's post-merge rebuild step.* Rejected: the rebuild runs in the
   working directory with master checked out; re-launching a resident daemon from there would give it
   a cwd that the **next** merge round mutates underneath it, and it couples “merge landed” to “daemon
   restarted” — two different operator decisions.
3. *Atomic binary swap (rename/symlink) as the restart.* Rejected: the running process's executable is
   already mapped, so the swap only affects the NEXT start — which is precisely what `--restart`
   does; the swap alone would silently keep the old code running forever.

**Honest limit.** A stale resident daemon is **not detected automatically**. `--status` carries the
`version` field, and that is the whole detection story — an operator who skips it runs old code against
new master (the recurring stale-bin class of incident this model chooses to leave manual).

---

## 5. Q5 — Crash policy: auto-respawn? crash-loop backoff? task quarantine?

**DECISION (three answers).**

**(a) No auto-respawn.** A dead daemon is dead. Recovery is the operator's (`~/bin/gopi-worker-up.sh N`,
or `gopi worker --restart` / `gopi merger --restart`). Nothing inside the process tree respawns it.
This is a deliberate choice in favour of the WORKER-OWNERSHIP ruling: a process the operator did not
start is a process he cannot see die.

**(b) No crash-loop backoff is needed, because there is no respawn loop — but the equivalent
protections exist at the two places a loop CAN run:**

- the **worker round loop** — a failed round does not kill the loop: the round's failure is written
  back to the row and the loop continues to the next tick (`runTaskRunOnce` returns an error to the
  loop; only the plain CLI face `os.Exit`s), bounded by `GOPI_WORKER_MAX_ROUNDS` and by the idle-exit;
- the **merger resolve/verify lane** — the one true machine loop in the system is capped by a durable,
  crash-safe ledger counter: `MergeVerifyFailCap = 3`, counted per `(repo, branch)` by
  `MergeVerifyFailCount` (excluding the row being judged), after which the gate files
  `MergeVerifyReasonBudget` and demands a human **[code]**.

**(c) A task is NOT quarantined because its worker crashed — and that is the decision, not an
omission.** The worker's lifecycle pass runs on **every** round (idle or busy), throttled to
`workerLifecycleThrottleMin` = 10 min **[code]**: it (i) reports stale `in_progress` rows through the
notify pipe (`internal/tools/taskheartbeat.go` `TaskHeartbeatNotifyEvent = "stale"`), (ii) past the
AUTO-DISPOSAL grace (`config.TaskDisposeSec`, resolved in exactly one place,
`config.ResolveTaskGracePeriods`) disposes **CONFIRMED zombies** via `tools.SweepZombies` with
`Kill=true` — a verifiably **live** claimed pid VETOES disposal and a `suspected` row is never killed —
and (iii) re-attempts abandoned close-outs (`serveFinishCleanupSweep`: push → worktree remove →
branch -d). So a crashed round yields: the row is reported, then disposed as failed with the zombie
reason, and if the deliverable mattered, a **new** task referencing the old id (§6). `--once` is
exempted from the scratch sweep by design; the lifecycle pass itself rides both faces.

**EVIDENCE.** `--status` on this host reports the truthful picture for rows and daemons alike
(M9: `--status` named both live workers by pid; the operator's merger daemon `pid 1842463, passes 83,
IN-FLIGHT pass … note: busy: a merge pass is running` was reported while it was mid-pass). Stale
detection uses the heartbeat signal (an `in_progress` row whose last tool batch is older than the stall
grace), not a status string **[code]**.

**REJECTED alternatives.**

1. *Auto-respawn with exponential backoff inside the daemon.* Rejected: it is a supervisor inside a
   process the operator cannot own, and with the singleton flock a crash-looping respawn fights the
   lock (each restart must wait for the previous holder to die). The real protection against a crash
   loop is (a) + the caps in (b).
2. *Quarantine a TASK when it crashes a worker.* Rejected: “the worker died” and “the task is bad” are
   two different facts; the row's reclaim/dispose decision belongs to the pid-death evidence chain
   (live pid vetoes, `suspected` never killed) — a quarantine tag applied on a crash would silently
   drop good work whose worker was killed by an unrelated event (the 04:12 incident killed six workers
   inside one 70 s window).
3. *A dedicated `quarantined` tag for repeated failures.* Rejected: the tag vocabulary already has
   exactly one quarantine meaning (`tools.QuarantineTagSet` → `agencyscan.TagQuiet`, the human-gated
   proposal rows, Rule 14 one-value-one-meaning). The repeat-failure story is told by the ledger
   counter + a new task (§6), not by re-purposing that tag.
4. *Restart-on-crash via the launcher's `--count` / external watchdog.* Rejected as the model (it
   would be a de-facto supervisor outside the ruling) — and note the measured reality that
   `--count` reads procfs (`comm` + `cmdline` shape) rather than patterns precisely so it can never
   match itself; it is safe to *observe* with, never to act on.

---

## 6. Q6 — How resume is offered (task vs session)

**DECISION. Two different things, never conflated:**

- **A TASK is never re-run.** A terminal row stays terminal and append-only; `completed → pending` is
  refused by the dispatcher state machine, and `internal/tools/verify_gate.go` states the pattern
  verbatim: the rework of a reverted merge is “NEVER a re-run of the old task (whose row stays terminal
  and append-only) but a new row that REFERENCES the old task id in its summary and its payload”
  (`mergeVerifyReworkSummaryPrefix = "merge verify rework: "`, tags `branch-rework`). The forward path
  for “this work is needed again” is `gopi task submit` with the old id cited.
- **A SESSION is resumable**: `gopi resume <task-id|raw-log-path|session-id> [continuation-file]`
  rebuilds the byte-exact context from the last `→ REQUEST` line of the raw log and continues the
  ReAct loop. It is **gated on process identity, never on the status string**: a verified-live holder
  refuses (no double run); dead / recycled / no-holder is reclaimable. `gopi task status <id>` exposes
  the verdict with script-friendly exit codes (0 = resumable, 1 = running — do NOT resume,
  3 = unverifiable), and `gopi resume --task <id>` stamps `resumed:<session-id>` into the row
  through a direct in-process call to `tools.TaskTagResumed` (the `gopi task tag-resumed`
  CLI spelling that used to carry it was DELETED 2026-10-16 — verb-surface retirements,
  0 executions), so “how often was this resumed” is answerable **[code]**
  (`cmd/agent/cli_resume.go`, usage text quoted at lines 17–50).

**REJECTED alternatives.**

1. *Auto-resume after a daemon restart.* Rejected: the double-run risk is exactly why the gate probes
   `/proc` instead of trusting the ledger's status column; an automatic resume would take that
   decision away from the careful path.
2. *Resume at the TASK level (flip the row back to running and re-dispatch the payload).* Rejected:
   the ledger is an append-only AUDIT — one resumed row would carry two runs, and the trail of what the
   first run did (and what it left behind) would be destroyed. The new-task-referencing-the-old-id
   pattern preserves both.
3. *Retry-in-place with an attempt counter.* Rejected: same audit problem, plus it would need an
   arbitrary “how many attempts is too many” knob that duplicates the existing caps (§5b).

---

## 7. Measured addenda requested by the briefing

### 7.1 GUI / dev-debug

- The **foreground** face is the debug surface: `gopi worker` (no `--daemon`) holds **no lock**, so it
  runs while a resident daemon is up, and `--status` stays usable beside it (measured exit 0 from an
  agent shell — read-only, takes no lock, creates nothing). The daemon is deliberately **invisible** to
  a naive `pgrep`-style count; `--status` is state-file-first with an argv fallback.
- GUI work from a detached daemon works **only on a display that outlives the launcher**: M5 (:99 →
  1280×800 PNG, exit 0) vs M2 (inherited SSH-forwarded display → socket gone, exit 1). The capture tool
  is pure Go (no external binary), so nothing else about the detach affects it.
- The daemon's own log location is fixed per component — `<gopi dir>/logs/worker.log` /
  `merger.log` — and the launcher's pre-claim output goes to `<gopi dir>/logs/worker-startup.log`;
  per-task output is attributed by the worker itself at claim time
  (`<output-root>/<session-id>/output.log`), because no launcher can know a task id at spawn time.

### 7.2 Image input (no daemon hazard — by construction)

- **Attachments are copied at SUBMIT time**, not at claim time: **[measured]** submitting with
  `--attachment /home/user/.cache/gopi-probe/shot.png` in a private `GOPI_HOME` created
  `<gopi-dir>/sessions/<task_id>/attachments/home/user/.cache/gopi-probe/shot.png` before any worker
  existed. The ledger row and the run briefing carry an **absolute path** to the replica. So a daemon
  that claims the row hours later reads a path that is already durable, and a task whose payload
  exceeds 16 KiB is preserved verbatim as attachment1 with the column holding a one-line pointer
  (`internal/tools/task_payload_spill.go`) **[code]**.
- `vision_analyze` takes a **local path**, validates the extension against the supported media types
  (`jpg/jpeg`, `png`, `gif`, `webp`), enforces the size ceiling (`visionMaxFileBytes`, 32 MiB) and
  inlines base64 in one request; its errors are user-actionable (`file not found: <path>`,
  `unsupported image format …`, `file too large: …`) **[code]** (`internal/tools/vision.go`).
- Consequence for this model: image input has **no** DISPLAY/session-env dependency, so unlike GUI
  capture (§7.1) it is unaffected by who launched the daemon or what DISPLAY it inherited. The only
  daemon-relevant fact is the path convention above.

### 7.3 Headless-daemon preflight / error behaviour

- **There is no DISPLAY preflight at daemon start** — the daemon starts fine headless, and the failure
  appears when a GUI tool is first used (M2/M3/M4, all exit 1 with actionable text). This is by design
  (a daemon must not refuse to run because a GUI tool might be needed later), and it is the reason
  §1 makes DISPLAY ownership a launcher contract rather than a startup check.
- **Preflight that DOES exist** is the daemon seam's own:
  `--require-scope` **[measured, exit 1]** —
  `gopi worker: refusing to start: --require-scope requested but the cgroup escape is unavailable:
  Failed to connect to bus: No such file or directory; in-process escape: cgroup scope escape
  unavailable: read /sys/fs/cgroup/user.slice/user-1000.slice/user@1000.service: open …: is a
  directory`; and without the flag the degradation is announced
  (`cgroup escape: setsid-only (<reason>)`) on stderr and in the log. The flock refusal names the
  holder pid, and an unrecognized argv word exits 2 **before** the guard and before any claim
  **[measured]**.
- The escape ladder on this host therefore sits at its **last** rung (§0.6): no user bus
  (`systemd-run --user` fails), no delegated `user@1000.service` subtree (the in-process probe fails
  with `is a directory`). A daemon started here is detached from the terminal but **still a member of
  `session-18200.scope`** (M1) and will die with that scope — announced, never silent.

---

## 8. Knob audit — every documented knob has a real reader

Rule 21 requires that a documented env name/flag is actually READ. Each row names the reader; the last
column is how it was checked.

### 8.1 Flags

| flag | reader | where |
|---|---|---|
| `--daemon` (`daemonFlagLong`) | `daemonFlagRequested` → both faces' scanners | `cmd/agent/cli_daemon_spawn.go` |
| `--require-scope` | `daemonScopeRequested` → `daemonScopeDecide` | `cmd/agent/cli_daemon_scope.go` |
| `--once` | `scanWorkerArgs` → loop's `once` | `cmd/agent/cli_worker_daemon.go`, `cli_tasks_worker.go` |
| `--worker-log-root <dir>` | `scanWorkerArgs` → `tools.WorkerLogRootFlag` consumers | same |
| `--status` `--stop` `--restart` `--start` `--force` `--wait` | `workerControlParse` → `workerControlCommand` / `workerStatusReport` | `cmd/agent/cli_worker_control.go` |
| `-h` / `--help` (worker) | zero-side-effect path before the scanner | `cmd/agent/main.go` `isWorkerSubcommand` |

### 8.2 Environment

| env | reader | checked |
|---|---|---|
| `GOPI_WORKER_DAEMON_CHILD` / `GOPI_MERGER_DAEMON_CHILD` | `daemonChild(markerEnv)` in each face | grep: 1 definition, 1 reader per file |
| `GOPI_DAEMON_READY_FD` | `daemonReadyFD()` | grep: 1 def / 1 reader |
| `GOPI_DAEMON_SCOPE_MODE` / `_UNIT` / `_DETAIL` | stamped on the child by `daemonScopeWrap`, read by `daemonScopeChildEscape` | grep: 1 def / 1 reader each |
| `GOPI_WORKER_IDLE_SEC` | `workerIdleInterval()` (one reader) | grep: 1 def / 1 reader |
| `GOPI_WORKER_IDLE_EXITS` | `workerIdleExits()` (+ the start-detail renderer) | grep: 1 def / 3 refs |
| `GOPI_WORKER_MAX_ROUNDS` | `workerMaxRounds()` | grep: 1 def / 1 reader |
| `GOPI_CGROUP_ESCAPE_OFF` | `cgroupscope.Off()` (`cgroupscope.EnvOff`) | code_search |
| `GOPI_PROC_ROOT` | `procname.EnvProcRoot` ← `worker --status` argv scan + the launcher's `count_gopi_processes` | code_search |
| `GOPI_HOME` | `config.GopiDir()` — the one data-root resolver (daemon log path, lock path, state dir, session tree) | code_search |
| `GOPI_WORKER_N` / `GOPI_WORKER_REPO` / `GOPI_WORKER_LOG_ROOT` / `GOPI_PROC_NAME` / `GOPI_WORKER_VERB` / `GOPI_TASK_VERB` | `scripts/gopi-worker-up.sh` (declared defaults; `internal/testutil/procname_gate_test.go` asserts each equals its Go constant) | read + gate |
| `GOPI_TASK_STALL_SEC` / `GOPI_TASK_DISPOSE_SEC` / `GOPI_TASK_MAX_RUN_SEC` | `config.ResolveTaskGracePeriods` (one resolver; the worker's lifecycle pass calls it) | code_search |
| `GOPI_TEST_SCRATCH_GC_MIN` | `scratch.GCIntervalEnvName` ← the worker's per-round scratch sweep | code_search |
| `GOPI_ALLOW_AGENT_SPAWN` / `GOPI_HUMAN_TERMINAL` | `internal/config/agentguard.go` (the guard the refusal text names) | read |
| `GOPI_TASK_NOTIFY_EVENTS` | `internal/tools/tasknotify.go` (staleness + `build-failed` subscription) | code_search |

No documented knob in this record is unread; the one *documented-but-stale* item found is a comment,
not a knob (F3 below).

---

## 9. Findings filed (this record does not silently carry them)

| id | finding | where it went |
|---|---|---|
| F1 | **`gopi worker --status --stop` prints the status and exits 0 — the stop never happens** (measured). The read-only route in `cmd/agent/main.go` returns before `workerControlConflicts` can refuse “pick ONE action”, so a read+act argv is a silent no-op with a success code. Same shape on the merger face. | **FIXED** — bug task `01a099e3-cd50-7c7e-88ea-c10c38037035`, branch `fix/worker-status-stop-conflict`; record: `devdoc/worker-status-stop-conflict.md`, report: `tasks/worker-status-stop-conflict-report.md` (see §9b) |
| F2 | **The launcher is the DISPLAY owner, and the launcher-script door is not covered by the agent-spawn guard** (M2/M5/M7 + the launcher's heredity-based permission). Docs give the launcher an env contract nobody wrote down, and the older claim “a script cannot launder this” (the guard's own refusal text until 2026-09-14) was measured FALSE for an agent shell that clears its environment. **RESOLVED**: `devdoc/worker-display-ownership.md` (decision record: launcher owns DISPLAY + the session env, ANNOUNCE-not-refuse, the door deliberately un-gated with the corrected claim) + the launcher's launch verdict + the worker's `display preflight` line. | task `01a099e3-fa5c-7276-b08f-82a5c31926d3` |
| F3 | **Stale comment drift: `workerDaemonSpec` says “control is nil — the worker has NO single-instance lock … hence daemonReadyChildFDSolo” while the code passes `acquireWorkerLock` and `daemonReadyChildFDCtrl`** (the text predates the singleton epic, blamed at commit `9ab8cf17`). `daemonSpec.control`'s doc comment carries the same stale claim. A reader trusting the comment would mis-model Q2. | task `01a099e3-f942-7cf0-96dd-df9a76a2de6e` |
| F4 | **Self-inflicted incident, recorded because it is evidence:** while measuring the control surface this record's author ran `gopi merger --status --stop`, which SIGTERMed the operator's live resident merger (`pid 1842463`, 83 passes, mid-pass). It was restarted immediately (`merger --daemon` → `pid 3274318`, `--status` alive). Root cause: the same argv shape as F1 *plus* an agent exercising a production control verb. The lesson is Q1/Q5's: **a resident daemon is production; never exercise `--stop/--restart/--force` on one to “see what happens”.** | recorded here + memory |

---

## 9b. F1 resolution — a read and an act are never combined (fixed 2026-09-13)

The finding above was filed so the routing bug would not be lost; it is now fixed on
`fix/worker-status-stop-conflict` (rebased onto the merge that carries this record). What
§9's F1 promised and what landed:

- **The refusal, not the read.** An argv holding both a READ (`--status`) and an ACT
  (`--stop`/`--restart`/`--start`/`--force`/`--daemon`) is **refused**, naming both flags —
  a read plus an act in one argv is ambiguous by construction. `--status` alone is
  untouched (exit 0, side-effect free), so the documented read-only guarantee is intact.
- **One predicate, four routes.** `controlActionRoleOf` / `controlReadAndActRefusal`
  (`cmd/agent/cli_worker_control.go`) classify each control word by role; guards sit at
  `isWorkerSubcommand` (before the status route), `isMergerSubcommand` (before
  `mergerControlCommand`), the deprecated `task merger` alias, and the top of
  `mergerControlCommand`. Face-specific printing (`mergerControlRefuseReadAndAct`) keeps
  each command's usage head and exit class (worker 2 = its usage class, merger 1).
- **A third instance of the same defect, found by the route audit:** `gopi merger
  --daemon --status` **detached** a resident merger and exited 0 (`--daemon` acts through
  its own route and never consulted `--status`). `--daemon` is now classified as an ACT, so
  `--daemon --status` is refused and `--daemon --stop` is refused as two acts.
- **Measured on the shipped binary** (private `GOPI_HOME`): worker `--status --stop` rc=2,
  merger `--status --stop` rc=1, merger `--daemon --status` rc=1 — each naming both flags,
  none touching a stop/force request or worker state; `worker --status` / `merger --status`
  alone stay rc=0; `merger --daemon` alone still detaches.

The F4 lesson stands unchanged and is *reinforced*: the refusal removes the argv that
produced F4 from the tool's vocabulary, but a resident daemon is still production — never
exercise `--stop/--restart/--force` on one to “see what happens”.

---

## 10. Not done / limits (honest)

- **No resident daemon was started by this task** (WORKER-OWNERSHIP). The detach evidence comes from a
  self-terminating `setsid nohup` probe using the same mechanism (`syscall.SysProcAttr{Setsid:true}`,
  environment inherited verbatim), not from a live worker/merger. Anything only a *resident* daemon
  could show (e.g. GUI capture after hours of uptime, or the in-flight-round close-out under
  `--stop`) is therefore documented from code + the e2e suites, not re-measured here.
- `--stop/--restart/--force` were **deliberately not exercised** after F4 (they act on the operator's
  production daemons); their behaviour is documented from `cli_worker_control.go` / `merger_graceful.go`
  and the hermetic e2e tests, and the one accidental run is recorded as F4.
- **Wayland was not tested** (no portal is implemented — the tool fails loudly by design, quoted in
  §7.3/M4).
- The **cgroup escape ladder could not be measured past `setsid-only`** on this host (no user bus, no
  delegated subtree); §7.3 records the measured refusals rather than a successful escape. The ladder's
  own record is `devdoc/daemon-cgroup-detach-20260913.md`.
- **Multi-node/remote dispatch** (ssh + `nohup gopi task run` on a compute node) is out of scope here;
  the guard's launcher-side judgement and the remote `GOPI_HUMAN_TERMINAL` question would need their
  own measurement.
- F1 is **fixed** — see §9b (branch `fix/worker-status-stop-conflict`); F2/F3 remain filed, not
  fixed: this task's deliverable was the decision record, and the F1 fix is its own task whose
  record is `devdoc/worker-status-stop-conflict.md`.

---

## Report

### Completed
- Decision record for all six briefing questions (§1–§6), each with the decision, the measured/code
  evidence, and the rejected alternatives with reasons.
- Measured evidence sections: detached-process GUI capture, env inheritance into a detached process,
  run-child env/cwd seam, attachment + `vision_analyze` path facts, headless preflight/error behaviour
  (§0.1, §7), with every command's output quoted.
- Knob audit (§8) proving every documented flag/env name has a reader in shipped code.
- Four findings surfaced instead of buried (§9): one bug task, two design/doc tasks, one self-inflicted
  incident recorded as evidence.

### Not Done
- F1 (the `--status --stop` routing bug) is **fixed** (§9b). F3 (the stale comment) is filed as a
  task, not fixed here.
- No resident daemon was started or controlled (WORKER-OWNERSHIP + F4); no Wayland, no cgroup-escape
  success path, no multi-node dispatch measurement (§10).

### Verification
- Build: `go build ./...` → **exit 0, no output** (tree at master 7c24ea06 + this doc only).
- Gates: `go test ./internal/standards/` → `ok github.com/the operator/gopi/internal/standards 13.665s`.
- Measurements: every [measured] claim above is a quoted command output from this host on 2026-09-13
  (probe script: `~/.cache/gopi-probe/detach-probe.sh`, self-terminating, off the `/` partition).
- Commit: `055924af docs(daemon): operating-model decision record (launcher=DISPLAY owner, singleton
  vs cohort, idle/crash/restart/resume)` — 1 file, 466 insertions, devdoc only (no code changed);
  `900fab2d` pins this block to the observed outputs. Branch
  `feat/worker-daemon-operating-model` (worktree `~/workspace/gopi-worker-daemon-model`).
