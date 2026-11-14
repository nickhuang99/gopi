<!-- doc-class: historical -->
# Task Report: the worker termination line is now self-attributing

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Branch: `fix/worker-termination-attribution` (worktree `~/workspace/gopi-termination-attrib`,
base master `830b025b`). Epic: *"terminated received" is unattributable — 31 lines,
0 sender identity, only 3 carry a target task id.*

## Completed

### The artifact, before and after

Before (the only termination artifact the system kept — measured 31/31 on peerbox,
`~/.gopi/logs/worker.log`):

```
worker: terminated received — draining (consumed at the claim boundary: nothing new claimed, leaving)
```

No sender, no signal, no task, no wallclock — and the SAME string for an
operator's graceful `--stop` and for an unsourced external cohort kill.

After, on the SAME line:

```
worker: terminated received — draining (consumed at the claim boundary: nothing new claimed, leaving) | SENDER pid 1633695 argv "/home/user/workspace/_tmp/attrib-stopd.KBQD/gopi worker --stop" SIGNAL SIGTERM TASK none AT 2026-09-17T07:09:55+08:00
worker: terminated received — draining (consumed at the claim boundary: nothing new claimed, leaving) | SENDER UNSOURCED SIGNAL SIGTERM TASK none AT 2026-09-17T07:09:03+08:00
```

Line 1 is the graceful operator stop (sourced: pid + argv of the requester).
Line 2 is a bare `kill -TERM` (unsourced: Linux hands a signal handler no sender
identity). `grep UNSOURCED worker.log` selects exactly the unattributable events —
the two shapes are now different strings.

### Changes

1. **`workerTerminationProvenance()`** (`cmd/agent/cli_tasks_worker_stop.go`) — ONE
   renderer for the provenance suffix, appended by BOTH `workerDrainLine` and
   `workerStopLine`, so the pre-claim leave and the after-close-out leave cannot
   describe the same event in two ways. Fields: SENDER / SIGNAL / TASK / AT
   (RFC3339), all on one line so a grep over the worker log yields the whole event.
2. **Sender identity rides the durable request file** (`cmd/agent/cli_worker_control.go`).
   The requester writes `pid=… argv=… signal=… ts=…` (key=value header) into the
   `STOP_<pid>` body BEFORE signalling; the victim's claim-boundary consume parses
   it and files it via `workerRecordStopSender`. This is the only sender identity
   a victim can hold: Linux gives a handler no `SA_SIGINFO` sender pid, and
   `signalrec` records the sender in the *victim's* session dir — a file the log
   reader never joins, which is exactly the defect.
3. **Honesty, not fabrication.** A bare signal, a session teardown, a
   pre-provenance (bare-pid) request body, or a request file already cleaned up
   all render `SENDER UNSOURCED`; a worker terminated before claiming renders
   `TASK none`. No path invents an identity (Rule 15).
4. **`signalrec.SignalName`** (`internal/signalrec/signalrec.go`) — exported the
   signal-name lookup so the stop path and the provenance record cannot drift.
5. **Target task latch** (`cmd/agent/cli_tasks_worker.go`) — `workerRecordStopTask`
   at the claim, so the termination line names the row that was running.
6. **Dead code removed**: `workerOwnSender()` had no call site (the sender is
   recorded by the boundary consume) — deleted rather than left as a decoy.
7. **Compatibility**: every in-tree reader of the request file keys on its NAME
   (`tools.ControlPidFromName`, `tools.LiveStopRequestPIDs`) and never parses the
   body, so the body change is inert for them; a legacy bare-pid body still
   delivers the stop and degrades to UNSOURCED. The addressed pid stays the body's
   LAST line.

### Regression tests (`cmd/agent/worker_termination_provenance_test.go`)

- `TestWorkerTerminationLineCarriesSenderSignalTaskAndClock` — the SOURCED shape:
  one line, the requester's pid + argv, the signal spelling, the target task and
  the wallclock; and it is NOT the unsourced marker.
- `TestWorkerTerminationUnsourcedIsADifferentString` — the UNSOURCED shape is a
  different string; the marker occurs 0× on a sourced line (the grep separator
  pinned in both directions).
- `TestWorkerStopSenderFieldCarriesNoSignal` — pins the first live defect: SENDER
  must not repeat the signal (it has its own field), and never renders empty.
- `TestWorkerStopRequestCarriesSenderHeaderAndKeepsLegacyBodyReadable` — the wire
  contract both ways: header round-trip, legacy bare-pid body → UNSOURCED + stop
  still honoured, force-spelling header, name-keyed readers untouched.

## Verification

- **Build**: `CGO_ENABLED=0 go build -o /dev/null ./...` → BUILD_OK; `go vet ./cmd/agent/` → clean.
- **Unit**: `go test -run 'TestWorkerTermination|TestWorkerStopRequest|TestWorkerStopSender|TestWorkerDrain|TestWorkerStop' ./cmd/agent/` → **PASS** (4 pinned tests verbose-PASS individually). `go test ./internal/signalrec/` → ok.
- **Live binary (real, not a unit seam)** — sandbox (`GOPI_HOME`+`HOME` in a
  `_tmp` scratch dir, `GOPI_ALLOW_AGENT_SPAWN=1` to get past the agent-spawn
  guard), binary built from this worktree:
  - sourced: `gopi worker --daemon` + real `gopi worker --stop` → the line in
    §Completed line 1 (requester pid 1633695, argv `…/gopi worker --stop`,
    SIGTERM, AT). `gopi worker: pid 1632301 exited 1.004s after the graceful stop
    request`.
  - unsourced: git-style foreground `gopi worker` + bare `kill -TERM <pid>` →
    the line in §Completed line 2 (`SENDER UNSOURCED SIGNAL SIGTERM`).
  - Note: one earlier attempt signalled the launcher pid of `worker --daemon`
    (the daemon re-execs into a systemd scope) and produced NO line — a
    measurement error, corrected by signalling the recorded lock pid / the
    foreground worker. Rules out nothing in the fix; recorded so the recipe is
    reusable.
- **Suite**: `go test ./cmd/agent/ ./internal/tools/ ./internal/signalrec/ ./internal/standards/` — see the run below.
- **Worktree clean**: `git status --porcelain` after commit must print nothing (Rule 30).

## Not Done (honest)

- **Not merged**: the branch is the deliverable; merge is the PA's call (Rule 6e/9).
- **The drain-vs-stop wording is unchanged** in the body of the line ("draining"
  vs "stopping after the in-flight round's close-out"); the acceptance asked for
  the sender/signal/task/clock on the same line and a greppable difference between
  sourced and unsourced, both delivered. A separate wording split was NOT
  attempted — the two lines already differ, and inventing a third spelling would
  be a reader-facing change nobody asked for.
- **Peerbox's 31 historical lines are not retro-attributed** — impossible: the
  identity was never recorded anywhere for them. The fix is forward-only.
- **`signalrec` is not joined into the log line** by this change; the sender
  identity is instead recorded on the path the log line already reads (the request
  file). Joining the two files at read time is a separate, larger change (a log
  reader would have to know a victim's session dir) and was deliberately not done.

## Files

- `cmd/agent/cli_tasks_worker_stop.go` — `workerTerminationProvenance`, stop-state
  fields (signal/sender/task), recorders.
- `cmd/agent/cli_worker_control.go` — request-body header, `parseWorkerStopSender`,
  `workerStopSenderFromRequest`, constants, dead `workerOwnSender` removed.
- `cmd/agent/cli_tasks_worker.go` — claim-time target-task latch.
- `cmd/agent/worker_termination_provenance_test.go` — new, the regression pin.
- `cmd/agent/worker_drain_unit_test.go` — state reset extended to the new fields.
- `internal/signalrec/signalrec.go` — exported `SignalName`.
