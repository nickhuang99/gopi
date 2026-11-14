<!-- doc-class: normative -->
# The recorder's obligation: what "this mode owns its own signal handlers" must be true of

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Epic `01a15bca`. This document is the mechanism window for the invariant, the gaps
it found, and the machine check that now holds it. The previous episode's document
(`sighup-gap-signalrec-vs-mode-handlers.md`, task `01a15bb9`) recorded that worker
and chat each lacked SIGHUP; this one is the general rule that episode was an
instance of.

## The mechanism, in four steps

`internal/signalrec` exists so a killed gopi leaves evidence. Its shape:

1. **`Install` registers `watchedSignals` with `os/signal` for EVERY gopi verb** —
   SIGTERM, SIGINT, SIGHUP, SIGUSR1, SIGUSR2 (`signalrec.go`, `watchedSignals`).
2. **Registering a signal with `os/signal` REPLACES its kernel default
   disposition.** That is the step that creates the obligation: once registered,
   the process no longer dies on SIGTERM by itself. Somebody must act.
3. **The recorder then decides, per mode, whether to re-raise.** For every mode in
   `modesThatOwnTheirSignalHandlers` it declines — the mode "owns its own
   handlers".
4. Therefore: **for a mode in that table, the mode's own registration is the only
   thing that can act on the signal.** If the mode's set is narrower than the
   recorder's, the difference is a signal that is *documented in the provenance
   ledger and then reaches nothing*: no exit, no drain, no stderr line.

Step 4 is the whole defect. It is not a crash and not an error return; it is
Rule 15's silent failure wearing the recorder's own evidence as cover — the ledger
says the signal arrived, and the process keeps running.

## The gaps the audit found

Measured on the epic's base (`e983a78`), one row per carrier. "Set" means what the
mode's own `os/signal` registration actually contained.

| mode | carrier | set before | verdict |
|---|---|---|---|
| `worker` | `gopi worker` drain loop | `{SIGHUP, SIGTERM, SIGINT, SIGUSR1}` (`cli_tasks_worker_stop.go`) | no gap — closed by `01a15bb9` |
| `worker` | `gopi worker --stop` control command | `{SIGINT, SIGTERM}` (`cli_worker_control.go`) | **GAP** — closed here |
| `chat` | `gopi chat` | `{SIGINT, SIGTERM, SIGHUP}` (`chatGracefulSignals`) | no gap — closed by `01a15bb9` |
| `web` | `gopi web` server | `{SIGINT, SIGTERM}` (`cli_web.go`) | **GAP** — closed here |
| `serve` | the bare-prompt arm `roleFromArgv` names `serve` | `{SIGINT, SIGTERM}` (`main.go`) | **GAP** — closed here |
| `resume` | `gopi resume` (top-level alias) | **none at all** | **GAP** — closed here |
| `session` | `gopi session resume` (the SAME runResume body) | **none at all** | **GAP** — closed here |
| `session` | `gopi session replay` (`internal/replayplay`) | `{SIGINT, SIGTERM}` | **GAP** — closed here |
| `merger` | `gopi merger` | retired: `retireMergerFace` exits before any handler | exempt, stated |
| `task merger` | `gopi task merger` / `task serve` | retired, same refusal | exempt, stated |
| `task run` | `gopi task run` | retired: `taskRemovedAdvice` + exit | exempt, stated |

Two of these matter more than the others, and they are the reason this was worth a
mechanism document rather than ten small fixes:

- **`resume` had no registration at all.** One body (`runResume`) serves two
  argv shapes, and they land on DIFFERENT table entries: `gopi resume` yields
  role `resume`, while `gopi session resume` yields role **`session`** (the
  recorder's `roleFromArgv` reads only `args[1]`, so the group word wins).
  Both are declared handler-owning, and neither registered anything. It is a
  long synchronous run
  (`ag.Resume`) in a mode the table calls handler-owning, so SIGTERM/SIGINT/SIGHUP
  were *all* recorded and dropped. A resumed task could not be stopped in a way
  that leaves a truthful ledger.
- **`session replay` losing SIGHUP leaves the operator's terminal broken.** The
  handler's whole purpose is to restore the terminal before dying; a recorded-and-
  dropped hangup means raw mode, alternate screen, and a live process — the exact
  state `internal/replayplay` exists to prevent.

## The fix: one definition, and the mode reads it

`signalrec.GracefulStopSignals()` is now the single definition of "the terminating
set" — SIGTERM, SIGINT, SIGHUP — and `watchedSignals` is *composed* from it plus
gopi's two control spellings (Rule 13: a constant that is bypassed by literals is
a defect, so there is exactly one place the three are spelled).

Every gap was closed by making the mode's own registration read that accessor
instead of carrying a hand-typed list:

```go
signal.Notify(sigCh, signalrec.GracefulStopSignals()...)          // web, the bare-prompt arm
signal.Notify(resumeSigCh, signalrec.GracefulStopSignals()...)    // resume
signal.Notify(s.sigCh, signalrec.GracefulStopSignals()...)        // internal/replayplay
var workerControlSignals = signalrec.GracefulStopSignals()        // worker --stop
```

**`modesThatOwnTheirSignalHandlers` was not touched**, and that is a correctness
requirement, not caution: removing a mode from the table would push its signals
back into the re-raise path, which kills the process with the DEFAULT disposition
mid-turn — trading a silent drop for a destroyed in-flight turn. The table states
what each mode *owes*; the fix makes the owing true.

The resume face needed more than a registration, because a handler that only sets a
flag does nothing while a synchronous turn runs on main:

```go
ag.CloseSignal = signalrec.SignalName(sig)   // the session_end names the cause
ag.Interrupt()                               // the graceful unwind runEngine honours
```

`ClaimSignalHandlers()` follows the `Notify` — never precedes it — because the
claim is what makes the recorder leave the signal alone, so a claim without a
registration would produce a process unkillable by SIGTERM.

## The machine check

`cmd/agent/signal_owning_modes_guard_test.go`:

- **`TestEveryOwningModeCoversTheRecordersTerminatingSet`** walks
  `signalrec.OwningModeNames()` — the recorder's OWN table — so a mode added there
  is audited by construction, and holds every mode to
  `signalrec.GracefulStopSignals()` — the recorder's OWN set — so there is one
  definition of "terminating" on both sides. Three kinds of evidence, each
  checked against production rather than restated from it: a **production
  accessor** (`workerStopSignals`, `chatGracefulSignals()`,
  `workerControlSignals`), a **source anchor** requiring the operand to be
  `signalrec.GracefulStopSignals()` (for the registrations that live inside large
  functions), or an **`exitsBeforeHandlers`** refusal that must still be present
  (for the retired faces — so a revived daemon cannot inherit the exemption
  silently).
- **`TestRecorderTerminatingSetCarriesTheThreeGracefulSignals`** pins the SET, so
  the guard cannot be satisfied by the definition shrinking underneath it.
- **`TestRecorderTerminatingSetIsACopyNotTheDefinition`** is the mutation guard on
  the accessor: a caller truncating the returned slice must not corrupt the
  recorder's own list.

`cmd/agent/signal_owning_modes_regression_test.go` carries one regression per gap.
The two faces that can be driven in-process are driven for real
(`TestWorkerControlCommandTakesSIGHUP` raises a real SIGHUP and asserts on the
production latch); the rest assert the source properties that make the gap
unrepeatable, because the faces load checkpoints or run models.

## Red-then-green, measured

Two independent red proofs, because a source-shape guard and a process-level effect
are different claims:

1. **Shape.** Re-planting the real gaps (worker's set losing SIGHUP,
   `cli_web.go` reverting to `os.Interrupt, syscall.SIGTERM`) fails three
   subtests with the mode named —
   `gopi web (server): cli_web.go carries no signal.Notify(sigCh, signalrec.GracefulStopSignals()...)`.
   Exit 1. Restored from the pre-mutation copies and re-run green.
2. **Process.** Two binaries from the same tree, differing only in this change:
   the pre-fix one (`git worktree add --detach HEAD`) answered `http=200`, took
   `kill -HUP`, and **stayed alive** — the silent drop. The fixed one answered
   `http=200`, took `kill -HUP`, printed
   `gopi web: hangup — shutting down, finalizing chat session…` and **exited**.

## The trap this work walked into once

A test that raises a signal the test binary has no registration for dies by the
DEFAULT disposition: the run aborts with `signal: hangup` and every later arm
silently never executes — which reads like a crash, not a failed assertion (the
same wheel that cost `internal/testutil/suiterootsignal.go` in `c9c0534`). So every
arm that raises a real signal registers a safety net (`signal.Notify` on the test
binary) BEFORE raising it, and `defer signal.Stop`s it. The net only removes the
default disposition for the test process; a notify delivers to every registered
channel, so the production handler still receives the signal.

## The stale citation this audit surfaced (filed, not fixed here)

`devdoc/worker-control-sigint-attribution.md:138` names
`cli_worker_control.go:1283` as the control installer. At HEAD, line 1283 is
blank; the real `signal.Notify` is at **1307** and
`installWorkerControlSignals` is defined at **1305**. Filed as task
`01a16e07-daa3-7006-98c2-98f0c71ec201` with the evidence
(`sessions/01a15bca-…/stale-doc-citation-evidence.md`). The same document's
`main.go:915` citations were re-derived to `main.go:926` in THIS change, because
this change moved that line — the deliberate-update case, as opposed to the stale
one.
