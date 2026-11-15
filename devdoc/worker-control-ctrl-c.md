<!-- doc-class: historical -->
# `gopi worker --stop` could not be stopped by its own operator (Ctrl-C, task 01a14316-b7cb)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Status:** fixed in the task branch. **Measured:** 2026-10-16, host peer-host.

## The report

The operator ran, on HP:

```
gopi worker --stop --pid 504233 --wait 10m
```

and pressed Ctrl-C four times while it waited. The process did not exit and the
terminal printed **no** termination/Interrupt line at all. The command whose
whole job is to stop something else could not itself be stopped, and with the
default `--wait` (10m, and 25m in the reference spelling) that is up to
twenty-five minutes of a terminal that does not answer its operator.

The briefing's starting hypothesis was that the two serial polling waits in the
sender (`workerClaimLoopOpened`, `workerWaitForExit`, both plain `time.Sleep`)
had no signal exit, and that the worker side — which does install a handler
(`cli_tasks_worker_stop.go`) — had one. That is true and it is half the story.
**The other half is why a plain `time.Sleep` mattered at all**, because by Go's
ordinary disposition an unhandled SIGINT should have killed the process.

## What was actually measured

`/proc/<pid>/status` on a live, waiting sender, on a real pty, at two moments:

| reading | SigCgt | SigIgn |
|---|---|---|
| at fork, before exec | `0000000000000002` | `0000000001001000` |
| while waiting (same pid) | `fffffffd7fc1fefd` | `0000000000000002` |

Bit 1 is SIGINT. The second row is the whole bug: **SIGINT is in `SigIgn` and
absent from `SigCgt`** for the waiting process. Ctrl-C was not being "handled
badly" — the kernel was **discarding** it.

A four-panel probe over a two-line Go program (kept in this task's evidence)
settles the mechanism, because the answer depends on a disposition the process
**inherits** rather than on anything in the code:

| inherited SIGINT | `signal.Notify`? | result |
|---|---|---|
| SIG_DFL | no | exit -2, 0.001s — the kernel kills it |
| **SIG_IGN** | **no** | **never dies; ran to its own 8s timeout** |
| SIG_IGN | **yes** | handler ran, exit 7, 0.001s |
| SIG_DFL | yes | handler ran |

So, three findings, each of which the fix depends on:

1. **`signal.Notify` is what resets an inherited `SIG_IGN`.** Panel C is the
   fix's licence: installing a handler in the sender is not merely nicer than a
   signal-aware `select` — it is the only thing that makes the signal observable
   at all when the disposition was inherited.
2. **On the SIG_DFL path Ctrl-C "works" and that is the trap.** The kernel kills
   the command in 1ms, so on an ordinary interactive shell the operator sees no
   hang — measured here: exit -2 with **no `TERM_<pid>` written**, i.e. the
   Ctrl-C also prevented the request from being delivered. The defect hid behind
   a path that looks like success.
3. The operator's shape was the SIG_IGN one. Reproduced end-to-end on the
   pre-fix binary: four real Ctrl-C bytes on a pty, `^C^C` echoed by the tty,
   **no** termination line, sender still alive at a 45s ceiling.

## Root cause, in one line

> **CORRECTION (2026-10-16, task 01a1431c-5846-713c-83fe-9c9fecd45766).** The
> line below is right; the ATTRIBUTION this document inherited from the original
> bug row is not. The bare-prompt branch's
> `signal.Notify(sigCh, signalrec.GracefulStopSignals()...)` is on the bare-prompt
> path and `gopi worker --stop` returns from `main()` 287 lines earlier (the
> chain's head carries `isWorkerSubcommand(dispatchArgs)`; its `return` is the
> very next line), so that `signal.Notify` is unreachable
> on this verb. The process-wide SIGINT takeover on this path is
> `signalrec.Install("")` at the top of `main()` — eighteen lines BEFORE the chain
> that returns. See `devdoc/worker-control-sigint-attribution.md` for the source
> position, the `strace` record and the `SigCgt` readings. (Line numbers are gone
> from this correction on purpose: every one of them drifted three times between
> 2026-10-16 and 2026-10-27, and the anchors they named are spelled here instead.
> Resolve them in your own checkout with
> `grep -n 'signalrec.Install("")' cmd/agent/main.go` and
> `grep -n 'signal.Notify(sigCh' cmd/agent/main.go`.) And one reading in
> the table below does not reproduce: on a real pty the inherited `SIG_IGN` is
> reset ~4.8s in **by the pre-fix binary as well**, so panel B's "ran to its own
> 8s timeout" is a property of the two-line probe, not of this command. The
> surviving discriminator is the BEHAVIOUR (pre-fix: no exit, killed at the
> ceiling; post-fix: exit 3 + abort line), not the mask.

`workerControlCommand` (the sender) installs **no** `signal.Notify` of its own
while `gopi worker` itself does — so the sender's Ctrl-C behaviour was decided
entirely by a signal disposition it never chose, and the two waits it can spend
ten minutes inside (`workerClaimLoopOpened`, `workerWaitForExit`) could not wake
for a signal that the kernel was dropping on the floor.

## The fix

`cmd/agent/cli_worker_control.go`:

- `installWorkerControlSignals` — `signal.Notify` over
  `workerControlSignals` (`SIGINT`, `SIGTERM`) at the top of
  `workerControlCommand`, plus `signalrec.ClaimSignalHandlers()` so the
  provenance recorder does not re-raise mid-abort.
- **`workerControlSignals` is deliberately NOT `workerStopSignals`**: the
  worker's set carries `SIGUSR1`, which means *interrupt the round*. The sender
  must never read the escalation spelling as "stop waiting", or a `--force`
  aimed at it would silently consume itself.
- Both waits became interruptible: `select { <-time.After(poll) | <-workerControlInterrupted() }`.
- The abort prints the delivered/reaffirmed state and exits
  `workerControlAbortExit` (**3**) — deliberately neither 0 (the end state was
  never observed) nor 1 (which already means "the bound expired"), so a wrapper
  can tell "the operator gave up" from "the stop failed", and below 128 so it
  cannot be confused with a signal death.
- The durable `TERM_<pid>` is **never** withdrawn, and a repeat does not
  escalate: the request is printed by path so the operator can check it.

### The two shapes that had to be got wrong first (both measured)

**A. The handler printed, and lost the race.** The first version had the handler
goroutine print the abort and `os.Exit(3)` while the main goroutine, woken by
the same channel, returned `false` from the wait and called `os.Exit(1)`. The
observed result was exactly `exit 1`, elapsed 6s, **with no abort line** —
a silent failure that the acceptance criterion would have caught only as a
wrong exit code. Fixed by making the **waiter** the only printer and the only
exit path: the handler now only latches (`workerControlAborted`) and wakes.

**B. The waiter consumed the very channel the handler needed.** The guard was
`select { case <-workerControlSignalWaitCh: }`, reading the same 1-slot channel
the wait had already drained — so by the time the caller asked "did a signal
arrive?" the answer was always no, and the ordinary verdict ran first. Recorded
here because *the /proc mask of a process whose own `/proc/self/status` says the
opposite is what pointed at it*: the sender's own status showed SIGINT caught
(`SigCgt` bit 1 set, `SigIgn` 0) while an external read of the same pid showed it
ignored, which is only possible if the two were read at different moments — the
guard was firing, the process was exiting, and the reason was this consumed
channel. One latch, written by the handler, read by everyone else, consumed by
no one.

## Verification (what is in the tree)

- `cmd/agent/worker_control_sigint_e2e_test.go` — three arms, real binary:
  1. one Ctrl-C ends the wait, exit 3, `TERM_<pid>` standing, target alive;
  2. three Ctrl-C presses = re-affirm, same code, request still standing;
  3. `--wait 0` unaffected (returns at once, ordinary code).
- The SIG_IGN condition is produced **without a pty**: a child inherits ignored
  dispositions, so the test ignores SIGINT on itself and every child it starts
  inherits `SIG_IGN` — the operator's shape, from the kernel's own rule.
- The stop target is a seeded state record for a live non-worker process: a real
  daemon consumes `TERM_<pid>` at its first round boundary and exits (~1s when
  idle), which would make "the request survived" and "the target is alive"
  vacuous rather than true.
- Red-proof: the same file on the pristine baseline fails — the command runs to
  its 10s ceiling and is killed by the fixture (see the task report's `Ran:` /
  `Result:` lines).
- End-to-end against a **real worker in a sandbox profile**, with real pty ^C
  bytes: pre-fix `exit=null` (still running at ceiling), post-fix `exit=3`,
  elapsed 6.07s against a 1500s bound, abort line printed, `TERM_29959` still
  standing, worker still alive.

## A second defect, filed separately (Rule 41)

The mechanism turned up a signal-handling property that is outside this fix's
scope and is **not** changed here: `signalrec.Install`'s handler drops every
**repeat** of a signal kind for the life of the process (`seen[key] → continue`).
For a long-lived process that means a second Ctrl-C — the reflex an operator
uses when the first appears not to have worked — produces **no** record and, in
the non-forwarding case, no action either. Filed as its own bug row; the branch
for this task does not touch it.
