<!-- doc-class: normative -->
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

# Task Report: `gopi worker --stop` / `--restart` with graceful drain

Epic: worker-graceful-drain (2026-09-13). Branch `feat/worker-graceful-drain`
(worktree `/home/user/workspace/gopi-worker-drain`).

## Completed

### 1. The drain contract (durable request, consumed at the claim boundary)

`gopi worker --stop` / `--restart` deliver the leave request as a **durable
file** (`<control dir>/TERM_<pid>`, the merge-runtime `merger_graceful` pattern)
that ANY process can write, not as a signal:

- `workerDaemonCtx.consumeDrainRequests()` runs at the TOP of every worker round
  **and again inside `workerRound` immediately before the claim CAS**, so the
  boundary is a real boundary: nothing new is claimed once a request stands.
- The consume is **idempotent** by construction — the request file is the fact,
  the in-memory record is its projection (`workerRequestStop` keeps the first
  reason; `MarkDraining` keeps the first timestamp).
- `MarkDraining` publishes `{"draining":true,"draining_since":…,"version":…}`
  into the worker's state file, which is what makes `--stop` honest while it
  waits ("draining since …").

### 2. The force semantics are now spelled, not counted (D1)

The predecessor rule was "ANY second signal escalates to a kill". That made a
**repeated** graceful request (`--stop` twice, a service manager's stop+retry)
SIGKILL the in-flight round — precisely the outcome the graceful path exists to
prevent. Escalation is now the operator's explicit spelling: `SIGUSR1`, a
`FORCE_<pid>` request file, or `--force`. A repeated SIGTERM/SIGINT re-affirms
the SAME graceful request and changes nothing.
(`TestWorkerForceSpellingAloneEscalates` is the regression guard.)

### 3. A graceful request never kills the round it protects (root cause, D3/D4)

The real run found a second, deeper defect: both binaries **forwarded** the stop
signal into the in-flight *run child*, so even a single graceful `--stop` ended
the round `failed: signal: terminated`. `awaitWorkerStopChild(drain bool)` now
splits the two shapes:

- **DRAIN** (durable request standing): wait for the round to finish — report
  every 5s, terminate only on the force spelling.
- **bare signal** (a supervisor's clock is running): forward at once, bounded
  wind-down, then terminate. The old signal contract is preserved verbatim
  (`worker_stop_accept_test.go` pins it).

### 4. CLI idempotency and honesty

- Standing request ⇒ the invocation is a **reported no-op** ("pid N is already
  draining (… ) — nothing to do"), then it waits for the same end state. No
  re-signal, no rewritten file.
- `--start` on a live daemon reports and exits 0.
- `--restart` re-resolves live state after the drain and skips the spawn when a
  daemon already returned; a spawn refused by a **concurrent** restart's flock
  is not a failure (`workerControlEnsureDaemon` — the end state "exactly one
  fresh daemon" holds), so both racers exit 0. `daemonSpawn` now returns its
  error; `daemonSpawnOrExit` keeps the plain `--daemon` contract.
- The restart report names the new daemon's **binary version** beside the one it
  replaced, so "the new worker runs the current binary" is readable fact.

### 5. Draining a worker running a DIFFERENT binary (acceptance-v4 finding)

Measured: `--restart` from the new CLI against a **master-tip worker** left the
in-flight task row `failed: signal: terminated`. The pre-drain worker does not
handle SIGTERM as "finish the round" — it **forwards** the signal into the
in-flight run child, so the request that promised to protect the round killed
it. (It DOES consume the durable request file, with no signal, at its next loop
tick — also measured.)

Fix — one order, two arms (`workerControlStopOne`):

1. The durable STOP REQUEST FILE is written FIRST, always. Both generations
   consume it; it never enters a process through a signal handler.
2. The **signal is added only when the target provably shares this binary's
   semantics**: `workerForeignDrainBinary` compares the version recorded in the
   worker's state file against this process's own. Equal ⇒ SIGTERM fast path as
   before. Different (or unreadable) ⇒ **file only, no signal**, and the line
   names both versions so the operator sees the evidence, not an assertion.

The direction is conservative on purpose: a file-only drain can only ever be
slower to exit, never destructive. It is evidence rather than proof (two builds
can both report `dev`), which is exactly what the message says.

## Not Done

- No change to the **foreground** `gopi worker`: it publishes no state file by
  design, so the control surface cannot address it; signals remain its only
  interface (documented in `workerNoDaemonNote`).
- `--force` against a pre-drain binary still ends its round (that binary's
  SIGTERM forwards): forcing is an explicit "interrupt it now", so that is the
  contract, not a defect.
- The full `cmd/agent` package is red **in this session's environment** (see
  Verification) — that is pre-existing bug task 01a09950, not this branch.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK (gofmt clean).
- Unit: `go test ./cmd/agent/ -run 'TestWorkerDrain|TestWorkerForce|TestWorkerStateFile|TestWorkerRound|TestWorkerOtherDaemon|TestWorkerLegacyDrainBinary|TestWorkerAlreadyOrDelivered|TestWorkerStop'`
  → ok. Includes the claim-boundary refuse (positive + negative control), the
  force-spelling-only escalation, the version-carrying state file, and the
  legacy-binary predicate.
- Real runs (rig `/home/user/scratch/drain-accept`, sandboxed GOPI_HOME + repo,
  transcripts beside this report):
  - transcripts + the scripts that produced them are committed under
    `devdoc/worker-graceful-drain-evidence/`.
  - `transcript-stop.txt`: `--stop --wait 10m` on a BUSY
    daemon → drains, in-flight row `completed` (session
    `c0ac3d3f-…`), worker exits only after the boundary.
  - `transcript-restart.txt`: `--restart` with a task in flight → task
    completed, old worker exited 19.5s after the request, NEW daemon on the
    current binary.
  - `accept3-out.txt`: repeated `--restart` while draining = honest no-op;
    exactly ONE daemon remains; the in-flight task `drain-long-6` closed
    `completed` (session `c42acc61-…`).
  - `accept4-out.txt`: the upgrade path — master-binary worker replaced by the
    branch binary (`new daemon pid 3245108, binary dev (replaced pid 3244029,
    binary 7c24ea065580)`). This run exposed §5: its in-flight task
    `drain-long-7` closed **`failed`**, which is the defect the version-aware
    arm fixes.
  - `accept5-out.txt`: the pre-drain binary consumes the file by itself, idle
    and mid-round (its in-flight task `drain-long-8` closed `completed`,
    session `6730ba9e-…`).
  - `accept6-out.txt`: the version-aware arm — `--restart` from the new CLI
    against a master-binary worker with a task in flight ⇒ file-only request,
    task completed, exactly one new daemon; and a same-binary `--stop` still
    takes the SIGTERM fast path.

## Files

- `cmd/agent/cli_worker_control.go` — request/idempotency/wait/legacy arm.
- `cmd/agent/cli_tasks_worker_stop.go` — consume at the boundary, drain vs
  bare-signal child waits, force-by-spelling.
- `cmd/agent/cli_worker_daemon.go`, `cli_daemon_spawn.go`,
  `cli_tasks_worker.go`, `cli_merger_daemon.go` — spawn seam, state version.
- `cmd/agent/worker_drain_unit_test.go` — the unit half, including
  `TestWorkerForeignDrainBinaryDecidesTheSignal` (evidence: equal version ⇒
  signal; different/unknown ⇒ file only).
- `cmd/agent/worker_stop_accept_test.go` — the preserved signal contract.

## Environment note (honesty)

`go test ./cmd/agent/` is red **in a worker-daemon-child session** because the
session inherits `GOPI_DAEMON_READY_FD=3` / `GOPI_WORKER_DAEMON_CHILD`, which
`TestAgentSpawnGuard*` and the daemon handshake read as real state. Proof:
`go test ./cmd/agent/ -run TestAgentSpawnGuard` FAILs with those two variables
set and is `ok` with `env -u GOPI_DAEMON_READY_FD -u GOPI_WORKER_DAEMON_CHILD`.
Traced to pre-existing task 01a09950 (fix `guardTestEnv`), not this branch.
`internal/stylegate` hits in `internal/testutil/tmproot_assert_test.go` are
likewise pre-existing and untouched by this branch.
