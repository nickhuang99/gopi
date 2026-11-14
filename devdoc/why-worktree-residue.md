<!-- doc-class: normative -->
# Why gopi leaves worktrees behind — a source-level diagnosis
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Branch**: `doc/why-worktree-residue` (worktree `/home/user/workspace/gopi-wt-residue-diagnosis`, base `bb67757f`).
**Epic**: `01a0a6d9-d0f0-7286-88e7-6c8b88fd4497` — *"why gopi does not remove worktree?"* (two questions: why do workers die mid-development; why does a finished worker not remove its worktree).
**Method**: read the producer/sweep/guard code, then join it against the live host (ledger rows × `git worktree list` × `/proc` × `~/.gopi/logs`). Every claim below is either **MEASURED** (command output quoted) or explicitly marked **INFERRED**.

---

## 0. Answer in one paragraph

A worktree is removed by exactly one of three parties, and each is keyed on a condition the observed directories fail to meet: **the finisher** (a defer in the process that just completed the row, keyed on "`claimed_by` == this process's `host:pid`"), **the dead-holder sweep** (keyed on a *terminal* row whose claim pid is provably dead), and **the merger's tidy** (keyed on the branch already being an ancestor of master). The residue on this host is **not** a finished worker forgetting: every finished row of the current long-lived `gopi worker` (pid 952831, alive 7 h) either released its directory already (2 rows released, 7 directories are gone from disk) or is still *actively held* — the two surviving registrations belong to (a) a **dirty** worktree whose uncommitted work is the deliverable and (b) a **row whose run is alive right now**. The single largest mechanism that strands directories is fatal: **the record `claimed_by = peer1:952831` is a 7-hour-lived PID, so while that worker lives no dead-holder sweep can ever prove the claim dead**, and the finisher path is a `defer` that any hard kill (`SIGKILL`, cgroup teardown) skips. A second, *confirmed* defect: the dead-registration reap was wired on 2026-09-14 into `serveMergedCleanupSweep` — a function **whose only production caller was deleted on 2026-09-04**, so nothing resident has run it since.

---

## 1. The lifecycle as the code actually implements it

| party | entry point | candidate predicate | acts by |
|---|---|---|---|
| **finisher** (producer) | `workerRoundOwnWorktrees` — `cmd/agent/cli_tasks_worker.go:159`, installed as a `defer` at `:992` | `internal/tools/task_reclaim_self.go:236` `reclaimSelfRowsFrom`: `claimed_by = <host:pid of THIS process>` **AND** `status IN (in_progress,completed,failed)` **AND** `deliverable LIKE '%worktree%'` | `git worktree remove` (no `--force`) — `task_reclaim.go:912` |
| **finisher** (manual `task run`) | `taskRunReleaseOwnWorktrees` — `cmd/agent/cli_tasks.go:1760`, called after `cmd.Wait()` (`:1668`) | same as above | same |
| **executor child** | `tools.TaskFinishCleanupSessionID` — `internal/agent/taskregister.go:427` (end hook) | the child's own row + `deliverable.repo/worktree` | server arm = worktree remove only (`task_finish_cleanup.go:555`); **`IsServerNode()` gate at `:543`** |
| **dead-holder sweep** | `finishCleanupDeadHolderReclaimLines` ← `mergerPassOnce` — `cmd/agent/cli_tasks.go:5879`, `:2271` | `task_reclaim.go:683-688`: `status IN (completed, failed, cancelled, verified)` only | same guarded removal |
| **merger tidy** | `MergerRepoSweep` ③ — `internal/tools/repo_sweep.go:871` ← `cmd/agent/cli_tasks.go:2398` | branch already an **ancestor of master** (`repo_sweep.go:362`) — *modulo* the merged rows its `worktree` filter excludes | `MergedCleanupSweep` |
| **generic sweep** | `serveWorktreeReapSweep` — `cmd/agent/cli_tasks.go:3424` | age ≥ 24 h, no live cwd holder, unlocked, … | reap/prune |

And the reason any of this *matters*: a branch checked out in a linked worktree is refused by `scripts/safe-merge.sh` gate **[c]** (`:62` `git worktree list | grep -q "\[$BRANCH\]"` → `deny`), and git itself refuses to check the branch out twice. **The directory is the merge gate.** (Repo-sweep documents the same invariant: a worktree-protected branch is "ACTIVE BOND WORK — never touched", `repo_sweep.go:350`.)

### The guards every removal runs (one implementation, `reclaimDisposition`, `task_reclaim.go:301`)

1. `deliverable` must carry **both** repo and worktree (`:302`);
2. the recorded repo directory must still exist (`:309`);
3. the path must be a **registered linked worktree of that repo** (`:327`);
4. **guard ②** — the worktree must be **clean** (`git status --porcelain` == "") — `:350`, reason `reclaimSkipDirtyReason` = *"worktree is DIRTY (uncommitted work) — left for a human"* (`:85`);
5. **guard ③** — `finishCleanupWorktreeInUse` (`task_finish_cleanup.go:1505`) must find **no live process whose cwd is inside it** (the caller's own process chain excluded);
6. checked-out branch must equal the row's branch (`:362`).

A directory that fails any of these is kept **and named** in the log (Rule 15 design). Two of the six are the ones this host keeps hitting.

---

## 2. Question 2 — why a finished worker does not remove its worktree

### 2a. It *does* remove it — when it can (MEASURED)

`~/.gopi/logs/worker.log` (19.9 MB, last write 03:40), all-time tallies of the producer pass:

```
released: 2      ("own worktree released: … freed for merge; branch kept")
refused : 242    ("own worktree release refused: … — <the guard's own sentence>")
  of which DIRTY (uncommitted work) = 58 occurrences
  of which "worktree in use by live process pid N (<comm>): cwd …" = 245 occurrences
ledger scan failed: context deadline exceeded = 2
```

Release is therefore **rare but real**, and the dominant failure is **not** "it forgot" — it is `reclaimDisposition` guard ③: a **live process standing in the directory**. The repeated offenders name the holder class explicitly:

```
R: fix/daemon-cgroup-detach — worktree in use by live process pid N (gopi-worker-dae): cwd /home/user/workspace/gopi-daemon-cgroup/cmd/agent   ×34
R: feat/doubao-asr-eval    — worktree is DIRTY (uncommitted work) — left for a human                                                  ×25
R: fix/worker-status-stop-conflict — … (gopi): cwd …/cmd/agent                                                                        ×16
R: fix/rawlog-test-home-leak       — … (sr-noready.test): cwd …/gopi-rawlog-rework                                                      ×5
```

`gopi-worker-dae`/`gopi-merger-dae` = **daemons spawned from inside a task worktree** that inherited that cwd for their (unbounded) lifetime. That is the documented deadlock `internal/daemoncwd` was written to break (`daemoncwd.go:5-23`: "the directory is permanently un-recyclable … an immortal process is standing in it"), and the log shows it still occurring in the fleet. `sr-noready.test` is a **Go test binary** whose cwd is inside a worktree — another immortal-ish holder class.

### 2b. The five reasons the producer pass misses a finished row (source)

1. **The pass is a `defer`** (`cli_tasks_worker.go:992`) — "so it runs on EVERY way a round can end (a normal *run failed* continue, a return, a panic)". It is *not* robust to `SIGKILL`: no defer runs. A SIGKILLed worker round leaves the row `in_progress` with a **dead** claim pid, and a `deliverable.worktree` whose directory is still registered.
2. **The candidate key is the claim stamp of a long-lived PID.** For worker rounds the stamp is `peer1:952831`, where 952831 is the *worker daemon*, not the round (MEASURED: `/proc/952831/cwd → ~/.gopi/sessions/01a0a57c-…`, cmdline `gopi worker`, alive 7 h 14 m). The consequence is double:
   * the producer pass *does* match rows it claimed **in the current round** — but a row claimed while it ran in a **previous session** (the worker was restarted; today's worker pid was 2140436, then 2140461, then 952831 — MEASURED from `[own-release] claimed by this worker:` announce lines) is **not** this process's stamp at all, so the pass skips it;
   * symmetrically, `ReclaimDeadHolderWorktrees` can never prove that stamp dead **while the worker lives** — for the whole 7 hours, every terminal row it claimed is invisible to the dead-holder arm (its query wants terminal statuses *and* a dead pid).
3. **The removal is refused while a live process sits in the directory** (guard ③, 245 log occurrences). The finisher runs *inside* the run's own process tree — but the run's **executor child** and any daemon it spawned are separate processes, and if one is still standing in the worktree at close-out time the pass refuses. Design-correct; still a stranding mechanism.
4. **A dirty worktree is deliberately kept** (guard ②, 58 occurrences). This is the one that explains the *biggest* surviving directory (below) — and it is honest-by-design: uncommitted work is work.
5. **A row whose `deliverable` carries no `worktree`** is not a candidate at all: `task_reclaim_self.go:236` (`reclaimSelfRowsFrom`) filters on `deliverable LIKE '%worktree%'`. The backfill that writes `deliverable.worktree` is a *bash-round detection seam* (`internal/tools/task_deliverable_worktree.go:17-25`: "the executor creates its worktree by RUNNING git (bash tool — there is no Go-side `git worktree add` site)"), so any run that created a worktree outside a tracked bash round — or was spawned as `gopi resume` (see §4) — leaves a directory nothing can attribute.

### 2c. The live classification of THIS host's residue (MEASURED)

`git worktree list` — 6 entries; 3 are the fleet's or this session's. The `~/workspace` survivors:

| directory | branch | row | row status | holder | dirty | verdict |
|---|---|---|---|---|---|---|
| `gopi-membase-wt-policy` | `fix/worktree-sweep-memory-base-policy` | **none in the ledger** | — | dead | **6 modified +1 untracked** | **stranded**: nothing owns it, and guard ② keeps it for a human |
| `gopi-merger-always-rebuild` | `feat/merger-always-rebuilds` | `01a0a465-1b9c` | `completed` @22:44 | `peer1:952831` **ALIVE** | 2 untracked | producer pass eligible *only in the round that finished it*; the dead-holder arm is blinded by the live pid; tidy pass runs but did not free it |
| `gopi-resume-cleanup` | `feat/resume-finish-cleanup` | `01a0a57c-388d` | `in_progress` @04:10 | `peer1:952831` **ALIVE** | clean | **actually live work** — the worker's own current task is this worktree |
| `gopi-deadholder-resume`, `gopi-tmpdir-default`, `gopi-merger-always-flock`, `gopi-ledger-busy-dsn`, `gopi-modcache-cold-control`, `gopi-merger-stop` | — | terminal rows | `deliverable.worktree` set, **directory GONE** | dead | — | **released correctly** (Proven-vanished / removed) |

So the picture is a *working* lifecycle with two demonstrated exceptions, not a broken one — with the important caveat that the two exceptions are exactly the ones the merger keeps reporting: `merger.log` prints `protected=11 (dead holder: 1 [feat/merger-stop-no-babysitter] …)` on repeated passes, i.e. the merger is itself advertising a worktree it cannot free.

### 2d. The dirty case, taken apart (the clearest single story)

`01a0a465-1b9c` was a `gopi worker` run that finished **successfully** — `task_events` shows `completed … run: elapsed=54m57s` at **22:44**. Its worktree still holds **2 untracked files** (`devdoc/merger-always-rebuilds.md`, `…-mutation-redproof.sh`, mtime **21:53/21:54**). So:

* the run's own deliverable (the report) was **never committed** — the branch is 1 commit ahead of master and the report lives only in the dirt;
* guard ② therefore refuses removal — *correctly*;
* and the operator sees a worktree parked under a `completed` row with **no live worker in it** — exactly the observation in the epic.

This is the "completed ≠ clean" gap: the close-out chain never commits the session's artifacts (`internal/tools/tasks.go:3991` documents that the worktree lifecycle has *three* points where an unrecorded/undelivered worktree stops it), so an executor that ends the run before `git commit` leaves its ground parked **and un-mergeable** (gate [c]). The **same shape** is the second survivor: `gopi-membase-wt-policy` carries 6 modified files whose row does not exist in the ledger at all (its sibling row `01a0a429-1fc3` `failed` names a *different* path, `gopi-shm-wt-policy`) — i.e. the directory was re-created/re-used by hand, so **no row can ever match it**, and both the producer pass (no row) and the dead-holder sweep (no row) skip it forever. Only the 24 h age sweep could take it, and guard ② blocks that too.

### 2e. A confirmed wiring defect: the dead-registration reap is wired to a dead function

MEASURED via git:

```
d11af9a6 (2026-09-04) feat(merge): merger pass is config-driven per-repo sweep
   −   for _, line := range serveMergedCleanupSweep() {        ← the ONLY production call, removed
35faf715 (2026-09-14) fix(serve): run the dead-registration reap on the resident merge-cleanup tick
   +   lines = append(lines, serveWorktreeReapSweep()...)      ← appended INSIDE that now-uncalled function
```

`grep` of the whole tree: `serveMergedCleanupSweep` has **no production caller** — only its own tests (`cli_tasks_merged_cleanup_test.go`) call it, which is why the wiring test (`TestServeMergedCleanupSweepAlsoReapsDeadRegistrations`) passes while nothing resident ever executes it. The commit message states the premise ("Nothing RESIDENT ever ran the reap … The reap now rides the same tick"), and that premise has been **false in production since 2026-09-04**. Consequence: registrations whose directory is gone (the `prunable` family) are collected by **nothing** on this host — `gopi task sweep-worktrees` is operator-only and dry-run by default.

---

## 3. Question 1 — why some workers are dead mid-development

The kill paths, in the order the host's own logs rank them. (Same-generation evidence: `devdoc/nightshift-cgroup-worktree-resume-20260915.md` records the operator's manual `gopi worker`/`gopi merger` dying with their ssh session's `session-*.scope` — measured, with a cgroup trace.)

1. **cgroup teardown of the launching session** (MEASURED class, and the most damaging). Rule 24 is exactly this: `setsid`/PPID do not move a process out of `session-*.scope`; when the terminal/ssh session ends, systemd kills the whole scope. MEASURED right now: `gopi worker` pid 952831 lives in `/user.slice/user-1000.slice/session-18257.scope` — **a login-session scope**, i.e. that worker dies with session 18257. The properly detached merger, by contrast, sits in `…/gopi-daemon-merger.scope` (its own cgroup). A worker that never obtained its own scope is a worker guaranteed to die mid-development whenever the session that started it goes away.
2. **`SIGKILL` / abandon**: no signal handler, no defer, no close-out. The row stays `in_progress` with a dead pid; the worktree stays registered; **the branch is stranded** until something proves the holder dead. This is the shape `devdoc/orphan-dead-local-holder-release.md` was written for ("a row whose holder was killed (`SIGKILL` → the wrapper is never reaped → the `in_progress` row is never closed out) vetoed the cleanup of its own directory … forever, until a human ran `gopi task sweep-zombies --kill`").
3. **Stall / max-run disposal** (grace-based, opt-in): `internal/tools/taskzombie_sweep.go:19-23` disposes a row only when a **three-evidence chain** confirms the runner is dead — heartbeat stopped **+** claimed pid dead (signal 0) **+** session JSONL mtime stale; "a live claimed pid vetoes disposal outright". Auto-disposal runs only when `grace.DisposeSec > 0` (config `ResolveTaskGracePeriods`), and it marks rows **failed** — a *terminal* state, so those rows do become dead-holder-sweep candidates. Report-only otherwise.
4. **Agent wall-clock cap**: `GOPI_TIMEOUT_SEC` (`internal/agent/loop.go:2925`) — **default 0 = unlimited**. The observed default kills nothing; a *configured* cap ends the run gracefully (the graceful-finish path lives in `internal/agent/loop.go`, not in this file), so it is not a silent killer.
5. **Panic / crash**: the agent's crash-dump path re-panics after recording (`internal/agent/crashdump.go` `recoverRunPanic` — the one intentionally-kept `panic` per Rule 19), so a crash exits the child non-zero and skips the defer-based producer release (`runTaskRunOnce`'s failure branch *does* call `taskRunReleaseOwnWorktrees`, but only if the **wrapper** survives).
6. **The worker's own idle exit** (not a death, but it *ends* the pid that owns rows): `workerIdleExits` (default 3) × `workerIdleInterval` (default 30 s) → the loop `exit 0`s on a drained queue ("a worker runs and self-dies", `cli_tasks_worker.go:188`). MEASURED on this host: **at least 8 distinct worker pids** have claimed rows in the log (`2140436` ×24 rounds, `2140461` ×18, `1411363` ×4, `2527977`, `1645814`, `1385083`, `1375283`, `1174689` — plus today's `952831` unannounced). Every exit orphans the claim stamps of the rounds it never got to close, and hands the next worker a batch of rows it is not entitled to release.
7. **`serve`-side signal forwarding nuance**: a stop signal is *recorded* and forwarded to the live child, and the round returns at the **claim boundary** so the close-out runs — "that is what makes 'stopped' release its ground exactly like 'finished'" (`cli_tasks_worker.go` round close-out comment). So a *graceful* stop is handled; only the un-graceful kills (1, 2) strand.

**MEASURED corroboration that the claim identity is the pivot**: every claim stamp in the ledger is a **local** `peer1:<pid>` (`taskDefaultClaimHolder` — `tasks.go:4437`), including rows whose `claimed_by` pid is long dead (`1411363`, `1385083`, `1375283`, `320470`, `1178515`, `3438277`, `776376`, …). The dead-holder sweep and its resume leg are built to resolve exactly that (`task_holder_resume.go:417` deliberately selects worktree-naming rows **of any status** and probes "pid + process start ticks + command line" — §39-42 of that file states the conservative rule: *an unreadable probe is never "dead"*), which is also why "unreadable" claims hold a directory indefinitely (Rules 15/Rule 24 read the same way).

---

## 4. Why the executor child's own finish-cleanup is not a backstop here

`internal/agent/taskregister.go:427` runs `TaskFinishCleanupSessionID` on the **child's** end hook — but `task_finish_cleanup.go:543` mode-splits on `IsServerNode()`:

* **server / single-node → `finishCleanupServerRecycle`** (`:555`): worktree remove only, branch preserved.
* **client (peer declared) → `taskFinishCleanupChain`**: push → worktree remove → `branch -d`.

MEASURED: `~/.gopi/settings.json` has **no `role`** key and no `GOPI_NODE_ROLE` in the environment, so the role is undeclared and `IsServerNode` (`internal/tools/memory_sync_delta.go:1055`) falls back to the legacy hostname-vs-`sync_peers` check, which fails-safe to **CLIENT** for an unknown host — meaning the peer-declared path is what decides here, and `finishCleanupServerTarget()` decides whether a *remote* handoff exists. Either arm removes the worktree; the difference is only whether the branch is deleted. The point for this diagnosis is narrower: **this backstop only runs for the child's own row when the child exits through its end hook.** A child killed at the process level runs no hook — that is precisely the `gopi resume` finding the operator already filed (row `01a0a57c-388d`: *"gopi resume never runs the finish-cleanup chain — worktree left behind"*, reproduced twice), which is currently guarded by the operator's own experiment and is **the worktree's second-largest stranding mechanism after the cgroup kill**.

---

## 5. What would actually change the outcome (findings, not patches)

1. **The dead-registration reap is unreachable** (§2e): re-attach `serveWorktreeReapSweep` to a **live** tick — `taskLifecyclePass` (`cmd/agent/task_lifecycle_tick.go:244-252`) already composes `serveFinishCleanupSweep` and is called by both the worker (`cli_tasks_worker.go:297`) and the merger (`cli_tasks.go:2681`). This is a one-line wiring fix with a test that fails today.
2. **`claimed_by = host:<long-lived daemon pid>` blinds the dead-holder arm for the daemon's whole life** (§2b.2). The stamp should name the **round/run**, not the daemon — or the reclaim arms should treat "the row is terminal and its *round* is over" as the release key rather than "the daemon is dead". (The producer pass exists precisely because of this; it only ever sees rows it claimed *in the same process*.)
3. **A completed-but-dirty worktree can never be freed and can never merge** (§2d). The close-out contract has a hole: nothing commits the run's artifacts before the dirty guard refuses. Either the executor's close-out commits (so guard ② passes and gate [c] opens), or the row must be marked "delivered, needs human commit" so the merger/operator sees it as an action item rather than a parked branch. Two live directories have this shape right now.
4. **A directory whose row does not exist** (`gopi-membase-wt-policy`) is unreachable by *every* arm by construction. It is the "orphan directory" class, and the only party that could take it is the age sweep — blocked by the same dirty guard.
5. **The cgroup kill (§3.1) is the root cause of "dead workers"**, and its fix is already designed (Rule 24 / `daemoncwd`): a resident worker must acquire its **own** cgroup; `setsid` alone does not detach and must never be presented as if it did. MEASURED today: the running worker is still in `session-18257.scope`.

## 6. Honest limits of this diagnosis

* The implementation is present, unit-tested, and *partly working* (2 releases, 7 freed directories measured). Nothing here says the release design is absent.
* The **dead-holder resume arm** is the log-only-by-absence part: I verified the arm is called before the reclaim in `mergerPassOnce` (`cli_tasks.go:2271`, resume at `:2835`), but `merger.log` shows `protected=11 (dead holder: 1 …)` persisting across passes with the resume arm silent on this host — §3 mechanism 3 (live pid vetoes disposal) is the INFERRED explanation, since a live `in_progress` claim matches neither the resume arm's "provably dead" bar nor the dead-holder sweep's terminal-status filter.
* Log history is **partial**: `worker.log` stops at **03:40** and holds no line for `01a0a465`'s 22:44 completion (its rounds are logged under whatever task id the round ran), so the *specific* reason that row's release was skipped at 22:44 is INFERRED from the guards plus the untracked-file mtimes, not quoted from a refusal line.

---

## Verification of this document

* Build/tests: **not run** — this is a read-only diagnosis; no production file was modified.
* Every file:line cited was read during this session against base `bb67757f`.
* Live commands used (re-runnable): `git worktree list [--porcelain]`; the ledger join
  `sqlite3 -readonly "file:~/.gopi/memory.db?mode=ro" "SELECT task_id,status,claimed_by,json_extract(deliverable,'\$.worktree') FROM tasks WHERE deliverable LIKE '%worktree%'"`
  plus a `/proc/<claimed pid>` liveness check per row; `grep -h "own-worktree-release" ~/.gopi/logs/worker.log`;
  `grep -ho "protected=[0-9]* (dead holder: [0-9]*" ~/.gopi/logs/merger.log`;
  cwd-holder census: for each `~/workspace/gopi-*`, count `/proc/*/cwd` inside it.
* Date: 2026-09-16 (host `peer1`).
