<!-- doc-class: historical -->
# The worker-stop residual is REFUTED — measured, not a defect

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Task** `01a0d9e0-b463-7b81-a25d-e9a4c7862997` · branch
`task/01a0d9e0b4637b81a25de9a4c7862997` · 2026-09-26

**Question.** `devdoc/signalrec-claim-vs-reraise.md` (lines 132-144) flagged a
residual: `installWorkerStopSignals`
(`cmd/agent/cli_tasks_worker_stop.go:429`) installs its own
`signal.Notify(ch, workerStopSignals...)` (line 431) and never calls
`signalrec.ClaimSignalHandlers()`, so the early recorder (`signalrec.Install`,
`cmd/agent/main.go:466`) might still see handlers unclaimed, keep
`signalForwarding()` true for an unclassifiable argv, and re-raise SIGTERM after
`defaultForwardGrace` (250 ms) — killing the forwarder mid-`awaitWorkerStopChild`.

The doc's own words were *"Unverified — that path's timing was not measured.
Flagged, not asserted."*

**Verdict: REFUTED.** No code change is warranted on that path. The re-raise never
arms for the worker argv, because the worker face is **classifiable** — and the
flag assumed it was not.

## 1. The reading (step 1, before any measurement)

`signalForwarding()` is the one predicate that decides the re-raise
(`internal/signalrec/signalrec.go:1475`):

```go
func signalForwarding() bool {
	return forwardsDefault(roleFromArgv(os.Args)) && !handlersClaimed.Load() && !noForward()
}
```

Its first conjunct is already false for the worker argv, and the claim half is
therefore never reached:

| fact | file:line | value for the worker argv |
|---|---|---|
| `roleFromArgv` returns `argv[1]` verbatim when it is not `task`/`merger`/`merge` | `internal/signalrec/signalrec.go:925-937` | `"worker"` — **classified**, not `RoleUnknown` |
| `modesThatOwnTheirSignalHandlers` carries the row | `internal/signalrec/signalrec.go:238-240` | `"worker": true` |
| `forwardsDefault(role) = !modesThatOwnTheirSignalHandlers[role]` | `internal/signalrec/signalrec.go:262-264` | **`false`** |
| ⇒ `signalForwarding()` | `internal/signalrec/signalrec.go:1475` | **`false`**, claim or no claim |
| ⇒ `reRaise(sig)` | `internal/signalrec/signalrec.go:1146-1147` (call site), `:1874` (impl) | **never called** |

Note what `argv[0]` is: `roleFromArgv` starts at `args[1]`, so the two spellings of
argv[0] a worker can carry — the path a shell types (`/usr/local/bin/gopi`) and the
stable detached-process name a daemon is re-exec'd with — are the *same*
derivation. `cli_daemon_spawn.go:353-357` sets `child := exec.Command(exe, ...)`
then overwrites `child.Args = childArgv` (argv[0] = `"gopi-worker"`, built by
`workerDaemonChildArgs` → `daemonSpecArgv0` → `Argv0Prefix = "gopi-"`), but
`os.Args[0]` is discarded by the classifier on every path.

This is the same table the doc's own fix row rests on (`cmd/agent/main.go:693`,
`cmd/agent/cli_term.go:244` claim because a **bare prompt** is the one face whose
argv[1] IS the prompt text, so it genuinely reads as unclassifiable). The worker
face is the opposite case: its argv[1] is the mode word.

## 2. The measurement (a real process, not a reading)

Rule 31: the flagged surface was executed. Two rigs, both against a real binary
built from this tree (`CGO_ENABLED=0 go build -o /tmp/gopi-rig/gopi ./cmd/agent/`)
in a private sandbox profile (`gopi sandbox create --label rig1`) with an empty
queue. Rule 27: nothing generates device load — the workers idle on an empty queue
and the measured quantities are process liveness, exit code and wall time.

### 2a. The foreground worker under a bare SIGTERM

Script: `/home/user/workspace/01a0d9e0-evid/rig.sh` (the daemon/stop rig is
`rig2.sh`). The signal is sent to the explicit pid — never a pattern kill.

```
worker pid=2416267 argv=worker
--- banner reached (pid alive: yes) ---
exit_code=0
signal_to_exit_ms=884.6
--- worker output (excerpt) ---
worker: queue drained — waiting 2m0s (new tasks picked up next tick; kill to stop)
worker: terminated received — draining (consumed at the claim boundary: nothing
        new claimed, leaving) | SENDER UNSOURCED SIGNAL SIGTERM TASK none AT 2026-09-26T03:19:11+08:00
worker: signal provenance — SIGTERM (UNSOURCED — no gopi stop path announced this kill)
--- provenance record ---
{"...","signal":"SIGTERM","pid":2416267,"role":"worker","argv":"/tmp/gopi-rig/gopi worker",
 "note":"resident role with no task and no session log (mode worker) — process-keyed session dir",
 "sourced":false}
```

Three facts, each of which the flagged defect contradicts:

- **exit code 0** — the graceful path ran to completion. A re-raise calls
  `signal.Reset` + `syscall.Kill(self)`, i.e. the process dies by signal: the
  observed code would be 143 (`128+15`), as the doc's own red run records for the
  bare-prompt case ("owner victim exit = -1, want 1").
- **884 ms wall** ≥ the 250 ms grace, and the drain line is on stderr — so if a
  re-raise had armed, it would have landed *before* this exit and this line.
- **`"role":"worker"`** in the record: the recorder itself wrote down the
  classification. This is the value `forwardsDefault` reads, captured in the same
  process that decided not to re-raise.

### 2b. The residual's literal shape: `gopi worker --stop` against a live daemon

```
$ gopi worker --daemon              # a real detached daemon, pid 2419005
worker daemon started: pid 2419005, log …/.gopi/logs/worker.log
$ gopi worker --stop --wait         # `--wait` is FLAG-ONLY (a value is refused)
gopi worker: asking pid 2419005 to finish the in-flight round and exit (SIGTERM, bound 10m0s)
gopi worker: current state — pid 2419005, …. rounds 0, binary v0.1.0, idle since …, note: queue drained — waiting 5m0s
gopi worker: pid 2419005 exited 501ms after the graceful stop request
stop_exit=0
signal_to_gone_ms=1535.0
```

and the daemon's own log, at the same instant:

```
worker: terminated received — draining (consumed at the claim boundary: nothing
        new claimed, leaving) | SENDER pid 2419534 argv "/tmp/gopi-rig/gopi worker --stop --wait"
        SIGNAL SIGTERM TASK none AT 2026-09-26T03:20:17+08:00
worker: signal provenance — SIGTERM (announced by a gopi stop path)
```

with the recorder's two records for that kill (the intent, mirrored into the
victim's session dir, and the signal) — the victim's own line classifies the
sender and the kill:

```json
{"kind":"intent","role":"worker","argv":"/tmp/gopi-rig/gopi worker --stop --wait","target_pid":2419005,…}
{"kind":"signal","signal":"SIGTERM","pid":2419005,"role":"worker","argv":"/tmp/gopi-rig/gopi worker",
 "cgroup":"0::/user.slice/…/gopi-daemon-worker-sandbox-rig1-…scope",
 "sourced":true,"intent_evidence":"… worker → gopi worker: operator ran gopi worker --stop [by pid 2419534]"}
```

**501 ms.** The daemon finished its drain and its close-out, and the stop command
read back its exit code. Under the flagged defect the recorder's 250 ms timer
would have fired roughly midway and the daemon would have died by signal mid-drain
— exactly the harm the doc describes — and `worker --stop` would have reported
that death instead. Both the exit code and the wall time rule it out.

## 3. The pin (so a future edit goes red, not quiet)

`TestSignalRecWorkerStopArgvNeedsNoClaim` (`internal/signalrec/signalrec_test.go`)
asserts the **derivation** over the whole worker argv grammar — the foreground
faces (`worker`, `worker --once`, `worker <task-id>`), the detached daemon shape
(`gopi-worker worker …`), and both argv[0] spellings:

```go
role := roleFromArgv(args)
if role != procname.VerbWorker { … }          // classified, not unclassifiable
if forwardsDefault(role) { … }                // the re-raise stays disarmed
…
if signalForwarding() { … }                   // with NO claim and NO env disarm
```

Why it pins the derivation and not the table row: a bare row is a value a later
edit can drop with nothing going red. This guard fails **by name, on six argv**,
the moment the worker stops being classified — which is the exact edit that would
re-arm the re-raise. The last block (`EnvNoForward` and `envOwnsHandlers` cleared,
`os.Args` set to a worker argv, `handlersClaimed` false) is what keeps the test
honest: the predicate must be false because the **role** owns handlers, not
because forwarding is globally disarmed in the test environment — a test that
passed for the latter reason would pin nothing.

`procname.VerbWorker` (not a literal, Rule 1b) is the shared const
`internal/procname/procname.go:48` — the same word `internal/config/agentguard.go`
and `cli_worker_status.go` read.

## 4. Red → green on the same tree, one variable

```
$ go test ./internal/signalrec/ -run TestSignalRecWorkerStopArgvNeedsNoClaim -count=1   # "worker" row REMOVED
--- FAIL: TestSignalRecWorkerStopArgvNeedsNoClaim (0.00s)
    signalrec_test.go:1333: forwardsDefault(roleFromArgv("/usr/local/bin/gopi worker")) = true …
    signalrec_test.go:1333: … ("gopi-worker worker") …
    signalrec_test.go:1333: … ("gopi worker --once") …
    signalrec_test.go:1333: … ("gopi worker <task-id>") …
    signalrec_test.go:1333: … ("gopi-worker worker --once") …
    signalrec_test.go:1333: … ("gopi-worker worker") …
    signalrec_test.go:1351: signalForwarding() = true for a worker argv with NO claim — the recorder
        would re-raise SIGTERM one grace after the signal and kill the forwarder mid-awaitWorkerStopChild
FAIL
$ go test ./internal/signalrec/ -run TestSignalRecWorkerStopArgvNeedsNoClaim -count=1   # row RESTORED
ok  	github.com/nick/gopi/internal/signalrec	0.645s
```

## 5. The general lesson

The doc's flagged family has two sub-shapes, and they take **different fixes**:

- a process whose argv **cannot be classified** (a bare prompt — `argv[1]` IS the
  prompt text) needs the in-process declaration, `ClaimSignalHandlers()`;
- a process whose argv **is** classified as a handler-owning mode needs **nothing**
  — `forwardsDefault` already answers for it. Adding a claim there is harmless but
  redundant, and *relying* on a claim to cover a mode that the table could
  classify would paper over a missing row.

So the classifier is the fix for this class, and a "missing claim" is not
automatically a defect: it is a defect only when the role is also unclassifiable
(which is why the guard here asserts the role, not the claim).

## 6. Sibling row

`cli_term_once.go` (row `01a0d9e0-3bfb-7a2b-a113-a54145c59fb5`) is a **different
defect** — `gopi cli "<prompt>" --once` installs no handler at all — and is not
touched here. Its argv is the bare-prompt shape (the claim case), so the two cases
must not be merged.
