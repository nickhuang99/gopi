<!-- doc-class: historical -->
# The 20s stop budget was eaten by a disk-hygiene /proc scan (WS6, task 01a0ff43)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task: `01a0ff4322407d97ba34abaa2911ad87` · branch `task/01a0ff4322407d97ba34abaa2911ad87`
· commit `5d60acd4` · measured 2026-10-03, on peerbox (4 cores).

## The symptom

`TestWorkerDaemon_RestartReplacesTheDaemon` (WS6) failed intermittently:

```
gopi worker: pid 3635196 is STILL running 20s after the graceful stop request
--- FAIL: TestWorkerDaemon_RestartReplacesTheDaemon (30.20s)
```

...on a daemon that had **already consumed** the stop request. The state record read
`"state":"draining since <t>"`, `"rounds":0`, `"note":"daemon starting"` — the
request was taken, and the process simply did not get back to a boundary inside the
operator's window. The sibling task `01a0ff15` fixed a *different* bug in the same
fixture (the `GOPI_HOME` read — LAST wins) and made WS5 green; WS6 stayed red.

## Root cause (5-why, measured)

1. **Why did the daemon not exit within 20s of the request?** It never reached a
   round boundary: a drain request is honoured at the round **TOP**
   (`consumeDrainRequests`), and the loop was busy before the first one.
2. **Why was it busy?** The resident scratch sweep ran **INLINE at the top of every
   round** of the worker's own loop (`cli_tasks_worker.go:518`, the
   `scratchGCSweep(workerRoleLabel)` call).
3. **Why does that take so long?** The sweep's liveness rule walks **every live
   process**: `/proc/<pid>/{cwd,fd,exe}` plus the pid's **whole `/proc/<pid>/maps`**
   (bounded at `ProcMapsReadLimit` = 4 MiB), then `PathWithin` per mapped file
   (`internal/scratch/holder.go:354` `procMapsPaths`, `:385` `PathWithin`). Its cost
   grows with the host's process count and scratch-tree size; on this host (345
   `/proc` entries, 83+ suite roots under the `/dev/shm` memory base) one firing
   costs **12–18s**.
4. **Why is it not merely slow but BLOCKING?** It was a synchronous call in the loop
   goroutine — the SIGQUIT dump put goroutine 1 inside it:
   `runTaskWorkerLoop(cli_tasks_worker.go:518)` → `scratchGCSweep` → `scratchGCTick`
   → `scratch.SweepHome`/`SweepSandboxes` → `HeldByProcesses` / `LiveHolders` →
   `procMapsPaths` / `PathWithin`.
5. **Why is that a *rule* violation and not a tuning problem?** The worker was
   waiting on **wall clock consumed by host hygiene** rather than on an observable it
   owns (its own round boundary / drain channel). A pass that decides nothing about
   the task must not be able to stand between the operator and the boundary that
   consumes their request.

**The load it was measured under (this host is 4 cores, with resident sessions
pushing load to 8–20):**

| observation | load (1/5/15) | result |
|---|---|---|
| pre-fix, `-count=5`, fixture spelling `GOPI_TEST_SCRATCH_GC_MIN=100000` | 15.01 15.31 14.04 | **1/5 FAIL** (30.20s, the WS6 message); the 4 passes took 37.8/35.5/27.2/29.8s — i.e. every round paid the scan |
| pre-fix, rig, single daemon, sweep on | 18.73 15.97 13.73 | `--restart --wait 25s` observed the state record still `rounds:0 note:"daemon starting"` seconds after the stop was issued |
| pre-fix, same rig, `GOPI_TEST_SCRATCH_GC_MIN=0` (the documented off switch) | 19.28 16.54 14.58 | stop returned in **5.2s** — the sweep is the whole difference |
| post-fix, `-count=5` | 9.45 12.26 12.54 | **5/5 PASS**, 3.28s then 0.81s ×4 (acceptance A, exit 0) |

Sweep cost, measured directly (`probe-goroutine-dump.sh`, SIGQUIT during the window):
`cpu_ticks 1 → 93 → 287 → 505 → 676` over 5s, ≈1.4 cores, CPU-bound, with the loop
goroutine caught in `scratch.readProcFile → procMapsPaths`.

## The fix — the worker waits on a boundary it owns, not on host scanning

`scratchGCSweepAsync(role)` (`cmd/agent/scratch_gc_tick.go`) runs the **same** sweep
body on its own goroutine:

* `scratchGCSweepBody` is the one seam both faces call, so a stub blocks the
  synchronous *and* the asynchronous form alike (this is what makes the regression
  test non-vacuous);
* single-flight per process (`scratchGCSweepState` latch) — a tick arriving mid-sweep
  is a no-op, never a second concurrent `/proc` scan;
* the throttle and the off switch are untouched (same `scratchGCInterval`, same
  `GOPI_TEST_SCRATCH_GC_MIN` / `scratch_gc` semantics);
* when one firing exceeds `scratchGCSweepSlowWarn` (5s) it **reports its own
  duration** — the invisibility is what let a 12–18s pass eat stop budgets (Rule 15:
  a cost that is never printed is a failure that got quieter as it was measured);
* the synchronous `scratchGCSweep` is kept for the one-shot/diagnostic faces.

`cli_tasks_worker.go:518` now calls the async form, with the round-boundary rationale
in the comment (`if !once { scratchGCSweepAsync(workerRoleLabel) }`).

**Design note (Rule 10, "先根因再参数"):** the fix is *not* raising the 20s bound, and
not lowering the sweep's cost. The bound was correct; the waited-on quantity was
wrong. Nor does it move the sweep out of WS6's window by disabling it — the sweep
still fires on the same cadence; it simply no longer *occupies the boundary*.

## The regression guard — and the proof it still bites

`cmd/agent/worker_scratch_sweep_offboundary_test.go`:
`TestWorkerLoopDoesNotWaitOnTheResidentSweep` drives the **real** `runTaskWorker`
loop with the sweep body stubbed to **BLOCK** on a channel, and asserts three
things, so the proof cannot be vacuous:

1. the stub was actually **reached** (the seam is exercised — never assumed);
2. the loop reached its **drained wait** while the stub was still blocked;
3. the loop **returned** while the stub was still blocked.

The stop travels the production path: a durable request file for this pid written
from inside the loop's own idle-drain observer, consumed by `consumeDrainRequests`
at the next round top — the very boundary the sweep used to stand in.

**RED proof (measured, with the pre-fix synchronous call site restored):**

```
go test ./cmd/agent/ -run TestWorkerLoopDoesNotWaitOnTheResidentSweep
--- FAIL: TestWorkerLoopDoesNotWaitOnTheResidentSweep (30.04s)
    the worker loop never reached its drained wait while the resident sweep was in flight
```

The synchronous call site cannot reach the boundary while the stub blocks — that is
exactly the WS6 failure mode, reproduced at the unit level with no wall clock and no
host load. With the fix the same case passes 3× at ~1.0s each.

Two traps this test had to clear: (a) process-wide state (`scratchGCLastTick`,
`scratchGCSweepState`, the worker stop flag) leaks across `-count>1` runs and makes a
*correct* loop look hung — the test resets all three; (b) cleanup must **join** the
stub goroutine (a closed channel without a join trips the suite's parked-goroutine
guard).

## Not Done (honest)

* The sweep's own cost (12–18s per firing on a loaded host; ~1.4 cores) is
  **unchanged** — it is now off the boundary, not cheaper. Making the /proc scan
  itself cheaper (e.g. memoising per-pid maps within one sweep, or skipping pids that
  hold nothing under the scratch root before reading all of `maps`) is a separate,
  measurable task.
* **The suite-level flake that acceptance C tripped on was fixed on master while this
  branch was open — and this branch now carries that fix.** Acceptance C's first two
  runs on this branch each failed on a test this diff does not touch:
  `TestTaskShowRendersTheSameDigestForAllThreeAddresses` (0.22s), which compared the
  `task show` digest reached by three addresses **character for character** while the
  digest carries a wall-clock field — `liveness: … (clock 10:10:21)` vs
  `(clock 10:10:22)` (`taskShowTickTockLine`, `internal/tools/tasks.go:3616`,
  `taskTickTockClockFormat = "15:04:05"`). Measured here: `-count=1` green,
  `-count=60` red twice, `-count=80` red twice, both digests 1708 bytes differing on
  exactly ONE line. That is a pre-existing master defect in a file this branch does not
  modify (`cmd/agent/cli_tasks_show_selector_test.go`), and master repaired it
  (`daf52854` "the task-show digest comparison is the row's, not the wall clock's"
  — the clock masked, with a second-boundary regression test so the mask cannot hide a
  real difference). **The delivery-point sync (master merged into this branch,
  `1eb9e04c`) brought it in**, so acceptance C was re-run on the synced tip
  (`$GOPI_SESSION/acceptance-postsync.txt`). Nothing in `internal/tools` was changed by
  this branch: no clock seam was invented, and the flake is not this task's to own.
* The FIRST acceptance C run of this session also failed ten `bin/gopi`-driven e2e
  tests on a stale artefact, not on a defect: the rig's `bin/gopi` mtime was older than
  a `merge upstream` that bumped `cmd/`. Rebuild and all ten PASS
  (`bin/gopi` is git-ignored; nothing was committed).
* `markClaiming`'s missing publish (`claiming:true` was not observed in the state
  record of the pre-fix run, and the claim/publication order sits *after*
  `consumeDrainRequests`) is **reported, not fixed**: with the boundary no longer
  blocked, it does not decide this test's outcome, and changing the publish order
  touches the daemon state contract. Evidence for whoever picks it up: pre-fix state
  reads showed `"rounds":0,"note":"daemon starting"` with no `claiming` field at
  several samples inside the stop window.
* The fixture spelling `GOPI_TEST_SCRATCH_GC_MIN=100000` (a large number used as "off
  for this test") is left as the sibling branch `task/01a0ff15…` has it; with the fix
  the test is green with the sweep genuinely enabled, so no fixture change was needed.

## Verification

* Build: `go build ./...` — OK.
* Tests: `go test ./cmd/agent/ -run 'TestWorkerDaemon_RestartReplacesTheDaemon' -count=5`
  → **5/5 PASS** (`ok github.com/nick/gopi/cmd/agent 6.728s`, exit 0) at load
  9.45 12.26 12.54; `go test ./cmd/agent/ -run TestWorkerDaemon -count=5` → PASS;
  `make test-budget PKGS='./cmd/agent'` → PASS (see `## Evidence`).
* Ran: the acceptance command verbatim (`go test … -v 2>&1 | tail -30`, then the
  `TestWorkerDaemon` set, then `make test-budget PKGS='./cmd/agent'`), plus the
  pre-fix rigs described in the table above (`probe-sweep-ab.sh`, `rig-ws6.sh`,
  `rig2-ws6.sh`) and the red-proof run named above.
* Result: pre-fix 1/5 fail with the exact WS6 message at load 15.01; post-fix 5/5
  pass; with `GOPI_TEST_SCRATCH_GC_MIN=0` the same pre-fix binary stopped in 5.2s,
  isolating the sweep as the cause.
* Commit: `5d60acd4` (and the red-proof was produced from that commit by restoring
  the pre-fix call site in the worktree, then reverting it — `git status --porcelain`
  is empty now).

## Evidence (self-filed)

* Pre-fix red, verbatim: `go test ./cmd/agent/ -run TestWorkerDaemon_RestartReplacesTheDaemon -count=5 -v`
  → `gopi worker: pid 3635196 is STILL running 20s after the graceful stop request` /
  `--- FAIL: TestWorkerDaemon_RestartReplacesTheDaemon (30.20s)`;
  load line at that moment `15.01 15.31 14.04 27/886 3633364`. Captured in
  `red-ws6-count5.txt`.
* Post-fix green, verbatim: same command → 5 `PASS` lines (3.28s, 0.81s ×4),
  `ok github.com/nick/gopi/cmd/agent 6.728s`, `A_EXIT=0`; load at start
  `9.45 12.26 12.54 nproc=4`. Captured in `acceptance.txt`.
* Sweep cost: SIGQUIT dump shows goroutine 1 in
  `main.runTaskWorkerLoop(cli_tasks_worker.go:518)` → `scratchGCSweep` →
  `scratchGCTick` → `scratch.SweepHome`/`SweepSandboxes` → `procMapsPaths` /
  `PathWithin`; `cpu_ticks 1 → 93 → 287 → 505 → 676` over 5s (~1.4 cores).
  Captured in `probe-goroutine.txt`.
* Isolation of the cause: pre-fix binary, same rig, same host state — sweep
  cadence `100000` → the stop is not honoured inside the window; cadence `0`
  (the documented off switch) → stop returns in `5.2s`, exit 0. Captured in
  `probe-sweep-ab.txt`.
* Red proof for the regression guard: with the pre-fix synchronous call site
  restored, `go test ./cmd/agent/ -run TestWorkerLoopDoesNotWaitOnTheResidentSweep`
  → `--- FAIL: TestWorkerLoopDoesNotWaitOnTheResidentSweep (30.04s)` with
  "the worker loop never reached its drained wait while the resident sweep was in
  flight"; with the fix, 3× PASS at ~1.0s.
* Final acceptance C: `make test-budget PKGS='./cmd/agent'` on `1eb9e04c` (post-sync)
  → **PASS, exit 0, `outcome=pass`, 1000s,** `packages: 1 finished, 0 with a failure`;
  stamp `2026-10-03T10:34:37+08:00 … elapsed=1000s load_start=5.44 11.21 13.29
  load_end=12.02 12.06 13.16 cores=4 exit=0 outcome=pass`. Captured in
  `acceptance-postsync.txt`.
* Delivery-point sync: `git merge master` → `1eb9e04c`; `git rev-list --count
  HEAD..master` = 0 at that moment. The sync is the LAST step (Rule 30a) and it ran
  AFTER the work was finished and committed, not as a milestone.
* `gopi doccheck` (report-only): `0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv`,

  exit 0 — this doc's row is in the same commit (Rule 36).

Evidence files (session dir, not committed): `red-ws6-count5.txt`,
`probe-goroutine.txt`, `probe-timeline.txt`, `probe-sweep-ab.txt`, `rig2-trace.txt`,
`acceptance.txt` under
`/home/user/.gopi/sessions/01a0ff43-2240-7d97-ba34-abaa2911ad87/`.
