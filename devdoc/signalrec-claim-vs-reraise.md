<!-- doc-class: normative -->
# The recorder killed the process it was recording: bare-prompt SIGTERM and the argv-derived re-raise

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Date: 2026-09-25. Epic: `0x01a0d64f` (bare-prompt SIGTERM → exit 143/-1, no `session_end`).
Status: fixed on branch `task/01a0d64f090c791ebb677f1d2e6cb9a2`.

## Symptom

A bare-prompt `gopi "…"` killed by SIGTERM died by Go's default signal
disposition instead of unwinding gracefully:

```
$ kill -TERM <bare-prompt gopi>
EXIT_CODE=143
stderr: [gopi] terminated received — finalizing session…     ← the handler DID fire
on disk: (no session_end record)                              ← the unwind never finished
```

`143` is `128+SIGTERM`; as a Go `ProcessState` it reads `-1` (terminated by a
signal, i.e. **no handler consumed it**). The session JSONL was left open and the
auto ledger row stayed `in_progress` — exactly the zombie shape the graceful path
exists to prevent.

## Root cause (measured, not inferred)

TWO handlers were registered for the same signal, and the one that ran FIRST
killed the process out from under the other.

1. `cmd/agent/main.go`, top of `main()` — `signalrec.Install("")` runs before any
   dispatch. It is the **recorder**: it catches SIGTERM to
   file provenance, then **re-raises** with the default disposition unless it
   believes the process owns its own handler. At the time of this defect it
   decided that ONCE, at install time:

   ```go
   forward := forwardsDefault(roleFromArgv(os.Args)) && !noForward()
   …
   if forward { reRaise(sig) }   // sleep 250ms → signal.Reset → kill self
   ```

2. `cmd/agent/main.go`, the bare-prompt path — installs the REAL graceful
   handler (`signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)` →
   `"[gopi] … received — finalizing session…"` → `ag.Interrupt()`), which cancels
   the run ctx so the loop unwinds through `runEngine`'s defer
   (`session_end exit_code=1` + auto row close). It is installed **219 lines
   below** the recorder in the tree this defect was measured on. (The two call
   sites carry a line number nowhere in this document: measured 2026-10-27
   both had moved again (the recorder off the `447` written here, this
   registration off the `666`) while the ORDER between them never changed — so
   the anchors are spelled and the numbers are left to the reader's checkout
   (`grep -n 'signalrec.Install("")' cmd/agent/main.go` and
   `grep -n 'signal.Notify(sigCh' cmd/agent/main.go`). The 219 is NARRATIVE: the
   gap the measurement was taken at, not a current claim.)

**Why the table could not classify this process.** `forwardsDefault` is keyed on
a mode word from argv. A bare-prompt run has none: `argv[1]` **is the prompt
text**, so `roleFromArgv` returns the prompt verbatim and the mode reads as
unclassifiable. The policy for unclassifiable modes is `true` ("a process we
cannot classify must be no harder to kill than an ordinary unix command") — so
`forward` was armed by construction.

**Why the re-raise wins.** After the grace:

```go
time.Sleep(250 * time.Millisecond)   // defaultForwardGrace
signal.Reset(sig)                    // ← GLOBAL: disarms the bare-prompt registration too
syscall.Kill(os.Getpid(), sig)
```

`signal.Reset` removes **every** registration for that signal, including the
bare-prompt handler's own. The measured graceful unwind (0.34–0.57 s) is always
longer than the 250 ms grace, so the process died mid-unwind: banner printed,
`session_end` never written.

**The testsuite guard is a RACE, not a deterministic red.** Measuring the same
pristine master binary across runs produced both `ok` and `FAIL` (the unwind
sometimes finishes inside the grace). So "the test is red on master" is only true
in the limit — and it is true often enough to block a merge, which is why it was
reported (see `devdoc/cmd-agent-master-reds-20260925.md`). The deterministic
proof is the manual run above, and the deterministic guard is the one in this
change (the victim's unwind is **2× the grace**, so scheduling cannot decide it).

## Fix

Three parts, none of them new machinery:

1. **`signalrec.ClaimSignalHandlers()`** — a process that has just installed its
   own terminating handler SAYS SO (an `atomic.Bool`). One-way on purpose: the
   caller's handler stays installed, so there is nothing to un-claim.
2. **The predicate is evaluated AT SIGNAL TIME**, inside the recorder's handler:

   ```go
   func signalForwarding() bool {
       return forwardsDefault(roleFromArgv(os.Args)) && !handlersClaimed.Load() && !noForward()
   }
   ```

   This is the crux. `Install` runs at the top of `main()`, hundreds of lines
   before the caller's `signal.Notify`, so a decision captured at install time can
   only ever read "not claimed yet" — it would re-raise the very processes the
   claim exists to protect. Reading it at signal time also matches what the claim
   is about: the process **as it is when the signal arrives**.
3. **A declared ownership channel** (`GOPI_SIGNALREC_OWNED_HANDLERS`) for the one
   case the in-process claim cannot express: a child process cannot call
   `ClaimSignalHandlers()` before it has run `Install()`. It feeds exactly the
   same predicate — one ownership decision, two ways to state it, never two
   policies.

Call sites: `cmd/agent/main.go` (bare prompt) and `cmd/agent/cli_term.go`
(`gopi cli` — its mode word `cli` is unclassifiable too), each immediately after
its `signal.Notify`. The claim suppresses the **re-raise only**: the provenance
record is still filed, and the guard asserts that, because a fix that bought the
graceful exit by costing the evidence would undo the epic that put the recorder
there.

## Regression guard

`internal/signalrec` `TestSignalRecClaimedHandlerIsNotReRaised` (heavy:
`CriterionDeviceExec`). It drives a REAL re-executed child — a `TestMain` owner
arm, matched **before** the plain-victim arm, because `srChildEnvFor` arms the
plain marker on every victim and a late check would silently turn a claiming
child into a non-claiming one. The child's argv is a bare prompt, its unwind is
`2 × defaultForwardGrace`, and it writes a completion marker at the END of the
unwind.

Red → green on the same tree, one variable:

```
$ GOPI_HEAVY_TESTS=1 GOPI_HUMAN_TERMINAL=1 go test ./internal/signalrec/ \
    -run TestSignalRecClaimedHandlerIsNotReRaised -count=1     # claim REMOVED
--- FAIL: … owner victim exit = -1, want 1 -- the recorder re-raised SIGTERM
             and killed the graceful unwind
$ …                                                            # claim PRESENT
--- PASS
```

## Not fixed here (honest, and dispatchable)

- `gopi cli "<prompt>" --once` (`cli_term_once.go`) installs **no** SIGTERM
  handler at all. The recorder makes it *more* killable than a plain unix
  command: the signal is caught, a record is filed, and the process is re-raised
  into death with no graceful path to run. Different defect (missing handler, not
  a disarmed one), same family; it needs its own guard.
## Resolved elsewhere (2026-09-26) — the worker residual was REFUTED, not fixed

- `installWorkerStopSignals` (`cli_tasks_worker_stop.go`) still does not claim
  its handlers, and it does not need to: **the re-raise never arms for that
  argv**. `roleFromArgv` CLASSIFIES the worker face — `argv[1] == "worker"` is
  returned verbatim (signalrec.go:925-937), and `modesThatOwnTheirSignalHandlers`
  carries the row `"worker": true` (signalrec.go:238-240) — so
  `forwardsDefault("worker")` is false and `signalForwarding()` (signalrec.go:1475)
  is false **by construction, claim or no claim**. Measured on a REAL process
  rather than read (Rule 31): `gopi worker` under a real SIGTERM prints
  `worker: terminated received — draining (consumed at the claim boundary…)` and
  exits **0 within ~0.9s**, and a real `gopi worker --daemon` + `gopi worker
  --stop --wait` (`--wait` is flag-only) is **gone within 501ms** of the request,
  its own recorder filing `"role":"worker"` (the classification itself) and
  `"sourced":true`. A 250ms re-raise would have killed that process mid-drain,
  before its close-out ran. Pinned by
  `TestSignalRecWorkerStopArgvNeedsNoClaim` (`internal/signalrec/signalrec_test.go`),
  which asserts the DERIVATION over the whole worker argv grammar and is
  red-proven (drop the `"worker"` row and six argv fail by name). The correct fix
  for this class is therefore a **role**, not a claim — a claim is what the ONE
  unclassifiable face needs (a bare prompt, whose argv[1] IS the prompt text).
  Evidence: `devdoc/signalrec-worker-stop-reraise-negative.md`.
- `cmd/agent/merge_run.go` had its own `signal.Reset` + kill on the way out; it was
  a deliberate default-disposition restore on an exit path, and was not touched.
  *(Measured 2026-11-01: the file was deleted with the top-level merge verb, so the
  site no longer exists — nothing on this tree re-raises that way.)*
