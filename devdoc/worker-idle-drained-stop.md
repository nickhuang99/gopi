<!-- doc-class: normative -->
# Worker Idle Drained Wait Must Be Stop-Aware (epic worker-idle-drained-stop)
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

## The defect (measured 2026-09-16)

`gopi worker --daemon` started with `GOPI_WORKER_IDLE_EXITS=0` (the explicit
"never self-exit on an idle queue" opt-out) could not be stopped by either
documented door within the idle interval:

- `kill -TERM <pid>` — survived the whole interval (measured: still alive 5s
  after SIGTERM with `GOPI_WORKER_IDLE_SEC=3600`; the epic's operator report
  says 1h).
- `gopi worker --stop --wait 20s` — printed
  `pid NNN is STILL running 20s after the graceful stop request` and exited 1,
  with the daemon's own state note reading `queue drained — waiting 1h0m0s`.

### Root cause (5-why, code-level)

1. **Symptom**: SIGTERM neither kills nor wakes the daemon.
2. **Mechanism**: `installWorkerStopSignals` replaces SIGTERM's default
   disposition, so SIGTERM only sets an in-process flag
   (`workerRequestStop`) — the process does not die from the signal.
3. **Why the flag did not help**: the `idleExits == 0` branch of
   `runTaskWorkerLoop` waited with a **raw `time.Sleep(workerIdleInterval())`**
   (`cmd/agent/cli_tasks_worker.go`), which never looks at the flag.
4. **Why only that branch**: the sibling `idleExits > 0` branch already used
   the stop-aware `waitWorkerIdleOrStop` (added by the earlier graceful-drain
   epic), so the *default* face never exposed the bug — only the `EXITS=0`
   opt-out, which is exactly the long-lived-daemon configuration.
5. **Norm**: a graceful stop is a flag + a durable request FILE
   (`TERM_<pid>`); *any* wait that can run for an hour must observe both. A raw
   sleep in a resident loop is a stop-latency decision, not an implementation
   detail.

So the effective stop latency was the idle interval (1h), i.e. the daemon was
practically unkillable, and `--stop`'s bounded wait (20s) could never succeed.

## The fix

Two doors, one wait — `workerIdleDrainedWait(d)` in
`cmd/agent/cli_tasks_worker_stop.go`:

- polls `workerStopRequested()` (the signal flag) every
  `workerStopPollInterval` (~1s, the existing constant — no new magic number),
- polls the durable request file through the new
  `workerStopRequestFileExists(dir, pid)` (STAT only, **never consumed**),
- returns as soon as either door is open, and otherwise serves the whole span
  (the negative case is pinned by test).

The loop's `idleExits == 0` branch now calls it instead of `time.Sleep`.

### Why the file probe is in the wait (the second half of the epic's question)

`worker --stop` writes `TERM_<pid>` **and** signals. With the flag poll alone
the measured case works, but the file is the contract's *durable record* and
the only door when the signal never arrives (a foreign/older binary, a caller
that can only write a file, a signal that raced start-up). Two design points:

1. **The wait STATs, the boundary consumes.** Exactly one reader turns a file
   into a flag (`workerDaemonCtx.consumeDrainRequests`, at the top of each
   round); the wait only notices it and returns. A wait that both noticed and
   consumed would be a second reader (Rule 2) and could *invent* a stop reason
   for the drain line.
2. **Placement**: `runTaskWorkerLoop` starts each round with
   `workerCtx.consumeDrainRequests()` → `if workerStopRequested() { print
   workerDrainLine(...); return }`, so returning from the wait lands on the
   consume and the daemon exits with the correct reason and a clean
   `cleanupRequests` (no residue file).

### Error handling (Rule 15b)

`workerStopRequestFileExists` returns the stat error instead of folding it
into `false`; the wait reports it **once per wait** on stderr and keeps
serving its span (the flag door must still work when the profile's control dir
is unreadable). A `workerStopRequestLocation()` that cannot resolve the control
dir yields `""`/`0` — no file door, no crash, no silent "the file is absent".

### Contract preservation

`waitWorkerIdleOrStop` is **untouched** — it must sleep the WHOLE span when no
stop is pending, which `TestWorkerIdleWaitDoesNotDeferToAPeakWindow`
(`peak_guard_removal_test.go`) asserts (a peak window covering the current hour
must not change the wait). The new helper is a separate function precisely so
that contract cannot be weakened from here.

## Verification

Branch `fix/worker-idle-drained-stop` in the worktree
`~/workspace/gopi-worker-idle-stop`:

| Check | Result |
|---|---|
| `go build ./...` and `CGO_ENABLED=0 go build -o /tmp/wis-bin ./cmd/agent/` | exit 0 |
| `go vet ./cmd/agent/` + `gofmt -l` on the five touched files | clean |
| Unit, 4 cases (`worker_idle_drained_stop_test.go`) | PASS — flag door 1.00s, file door 1.00s (file left standing, flag NOT flipped), whole-span negative 0.30s, unreadable location 0.10s |
| `TestWorkerIdleWaitDoesNotDeferToAPeakWindow` | PASS (0.30s) — the untouched contract still holds |
| E2E, 3 cases (`worker_idle_drained_stop_e2e_test.go`, real binary, `GOPI_WORKER_IDLE_SEC=3600 GOPI_WORKER_IDLE_EXITS=0`) | PASS — SIGTERM 3.36s, `--stop --wait 20s` 2.37s, no residue 2.50s (`ok github.com/nick/gopi/cmd/agent 8.466s`) |
| **RED PROOF** — the same e2e file on pristine `master` in the detached worktree `~/workspace/gopi-wis-redproof` | **FAIL as it must**: `pid 3337587 is STILL ALIVE 5.03s after the stop … serves out its whole 3600s idle interval`; `--stop --wait 20s` → `exited 1`, `pid 3337755 is STILL running 20s after the graceful stop request`; all three cases FAIL in 125.9s |
| `go test ./internal/stylegate/` vs pristine master | identical hit set (only durations differ) — **no new violation** |
| `go test ./internal/standards/` vs pristine master | byte-identical escape list (33 lines) — **no new violation**; the gate's pre-existing red (`/tmp/.X11-unix` literal, `getRootImage` accessor) is on master too |
| `go test ./cmd/agent/` (whole package) | the failure set is reproducible on pristine master: running the same failing subset there reproduces `TestCLIContractTaskServeAliasPrintsOneLinePointer`, `TestDedupeActions_ReturnsTheFullDistinctList`, `TestStatusWithActingActionIsRefused`, `TestWorkerDaemon_LeavesTheLoginSessionScope`, `TestWorkerDaemon_RequireScopeIsHardWhenUnavailable`, `TestUserBusSeam_RequireScopeRefusalCarriesTheDiagnosis`, `TestMergerFlock_DaemonLockStopsWithTheDaemon`, `TestMergerDaemon_LeavesTheLoginSessionScope`, `TestTaskListReadPathE2E`, `TestSweepZombiesCLI_*`, `TestTasksCLIListHeaderCountMatchesRows`, `TestTmpDirValueProbeLeavesNothingBehind` (stale `/dev/shm` probe dirs), `TestWorkerDaemon_DetachesAndKeepsRunning` (setsid). None touches the drained wait. |

### Operator-level acceptance (real binary, private `GOPI_HOME`, empty queue)

Both doors, driven by `/tmp/wis-acceptance.sh`. The launcher guard refuses a
worker launched from an agent session, so the script runs in the operator
environment (it unsets `GOPI_AGENT_SESSION` / `GOPI_EXECUTOR_SESSION_ID`) and
stops each daemon by its **explicit pid** — never a pattern kill.

```
### DOOR 1 — SIGTERM
reached the drained wait: worker: queue drained — waiting 1h0m0s (new tasks picked up next tick; kill to stop)
DOOR1 PASS: pid 3357519 gone 505ms after SIGTERM
--- worker --status after the stop:
  why: no detached `gopi worker --daemon` holds the lock, and no `gopi worker` process is in the process table

### DOOR 2 — worker --stop --wait 20s
reached the drained wait: worker: queue drained — waiting 1h0m0s (new tasks picked up next tick; kill to stop)
gopi worker: asking pid 3357657 to finish the in-flight round and exit (SIGTERM, bound 20s)
gopi worker: pid 3357657 exited 501ms after the graceful stop request
--- worker --stop rc=0 elapsed=507ms
DOOR2 PASS: pid 3357657 gone
--- worker --status after the stop:
  why: no detached `gopi worker --daemon` holds the lock, and no `gopi worker` process is in the process table
--- residue in the worker state dir: (none — the daemon cleaned its request file)
```

Both stops land in ~0.5s against a **1h** idle interval (pre-fix: the whole
interval, i.e. the daemon appeared unkillable), and `worker --status`
independently reports no live daemon afterwards.

### Red-proof excerpt (pristine master, same e2e file)

```
--- FAIL: TestWorkerDaemon_IdleDrainedStopLandsOnSIGTERM (37.56s)
    worker daemon pid 3337587 is STILL ALIVE 5.028716706s after the stop — the drained wait does not wake on a
    stop request, so a GOPI_WORKER_IDLE_EXITS=0 daemon serves out its whole 3600s idle interval before it will die
--- FAIL: TestWorkerDaemon_IdleDrainedStopLandsOnStopCommand (51.47s)
    `worker --stop --wait 20s` exited 1 — the stop did not reach its end state:
      gopi worker: pid 3337755 is STILL running 20s after the graceful stop request
--- FAIL: TestWorkerDaemon_IdleDrainedStopLeavesNoResidue (36.63s)
FAIL	github.com/nick/gopi/cmd/agent	125.926s
```

The RED proof is the load-bearing one: the e2e case fails on pristine master
within seconds *because* the idle interval is one hour — only a stop-aware wait
can pass it.

## Load / isolation discipline (Rules 27, 6, 17)

- No test generates device load: the daemon idles on an EMPTY queue and the
  measurements are process liveness plus elapsed wall time.
- Each case runs the REAL binary against a private `GOPI_HOME` (empty repos
  table, empty ledger, no sync peers) and stops the daemon **by its explicit
  pid** — never a pattern kill (live operator daemons run on this host).
- `t.Setenv` only (never `os.Setenv`); no sleep is tuned to a device; the two
  `time.Sleep` calls in the unit file are annotated
  `//stylegate:allow-sleep` (they are start-up handshakes that must land the
  stop INSIDE the wait under test).

## Files

- `cmd/agent/cli_tasks_worker.go` — the loop's drained wait now calls the
  stop-aware helper (surgical: one line + comment).
- `cmd/agent/cli_tasks_worker_stop.go` — new `workerIdleDrainedWait`.
- `cmd/agent/cli_worker_control.go` — new `workerStopRequestLocation` /
  `workerStopRequestFileExists` (read side of the stop contract).
- `cmd/agent/worker_idle_drained_stop_test.go` — 4 unit cases.
- `cmd/agent/worker_idle_drained_stop_e2e_test.go` — 3 real-binary cases.
- `devdoc/worker-idle-drained-stop.md` — this report.

## Not Done

- The four pre-existing red test families listed above (stylegate, standards,
  `cmd/agent` e2e/cgroup/tmpdir cases) are **not** fixed here — they are red on
  pristine master for unrelated reasons (a stale `/dev/shm` probe residue, a
  host where `systemd-run --scope` succeeds and `setsid` does not apply, a
  `/tmp/.X11-unix` literal, a `getRootImage` accessor name). Fixing them is
  separate work; this epic's diff does not touch them (verified by the
  identical-master comparisons recorded above).
- The full `cmd/agent` package cannot be run green on this host today, so the
  acceptance evidence rests on (a) the targeted branch run, (b) the RED proof on
  master, (c) the operator-level acceptance, and (d) the master-vs-branch
  comparison of the failing sets.
