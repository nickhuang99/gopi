<!-- doc-class: normative -->
# `gopi merge --stop` — a durable, out-of-band stop for the `--every` loop

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

> **Retired mechanism (merge verb removal, 2026-11-01).** Every `gopi merge …`
> below is the RECORD of what this run executed in 2026-09-24: the top-level verb
> was removed afterwards (its implementation and flag parser deleted) and answers
> exit **1** with a retirement sentence today. The stop protocol this page
> documents is unaffected — what is gone is its only user, the `--every` loop,
> which went with the same verb. The engine a reader reaches today is the family
> spelling `gopi task merge`, documented by the FILE `man/gopi-merge.md`.

Class: `devdoc` | Reason: the design decisions, the rejected alternatives and the
measured evidence behind the stop protocol — the reasoning a future reader needs
and the code cannot carry | Evidence, as it stood on 2026-09-24:
`cmd/agent/merge_stop.go` (the request vocabulary `StopRequestPrefix` /
`ForceRequestPrefix`, reused from `internal/tools/service_control.go`; the state
record `mergeLoopState` and the predicate `speaksStopProtocol`; the loop's consumer
`mergeLoopControl.consume`), `cmd/agent/merge_run.go` (`runMergeLoop`'s two
boundaries). **Measured 2026-11-01: neither file is in this tree** — `ls
cmd/agent/merge_stop.go cmd/agent/merge_run.go` → both absent, and
`mergeLoopState` / `speaksStopProtocol` / `mergeLoopControl` / `runMergeLoop` have
zero hits under `--include=*.go`. What SURVIVES is the shared vocabulary:
`StopRequestPrefix` / `ForceRequestPrefix` in `internal/tools/service_control.go`,
now consumed only by the WORKER's foreground control
(`cmd/agent/cli_worker_control.go`, `cli_worker_foreground.go`, `cli_worker_ack.go`).

## The defect

`gopi merge --every D` (epic merge-every-flag, 2026-09-24) is a foreground loop
that repeats the merge pass inside one process. Stopping it was an IN-PROCESS
affair only: `runMergeLoop` installs a `signal.Notify` channel for
SIGINT/SIGTERM, so the only way to end the loop was to signal the process — and
nothing could ASK it to leave.

Three concrete consequences, all of them observed rather than imagined:

1. **A wrapper with no signal rights cannot stop the loop.** A script that
   launched the loop under a service manager, a peer host, or a different
   `su`/`sudo` identity holds no permission to `kill` it, and had no file, no
   verb and no record to work with.
2. **The signal is not a request — it is a race.** A SIGTERM delivered while the
   pass runs is answered by the loop's own handler with "finish this pass, stop".
   That is the right BEHAVIOUR, but it is indistinguishable from an accident: an
   operator who did not send it cannot tell a deliberate graceful end from a
   stray `kill` of the wrong pid.
3. **The merge file stated the gap as a virtue.** `merge_run.go`'s header said
   "no daemon, no supervisor, no progress record, no **state file**, no **stop
   flag**" as a deliberate property. That was a true description of the
   one-shot command; once `--every` existed it described a loop an operator
   could start and then not address.

## What was built, and where

| piece | file | responsibility |
|---|---|---|
| the request vocabulary (TERM_/FORCE_) | `internal/tools/service_control.go` | **reused, not re-implemented** |
| the state record + the loop's consumer | `cmd/agent/merge_stop.go` (`mergeLoopControl`) | publish, consume, drain, clear |
| the control invocation | `cmd/agent/merge_stop.go` (`runMergeStop`) | resolve, deliver, report |
| the boundaries | `cmd/agent/merge_run.go` (`runMergeLoop`) | top of iteration + inter-pass wait |
| the help page block | `cmd/agent/merge_stop.go` (`mergeStopUsageBlock`) | spliced into `mergeRunUsage` |

## The one decision that shaped everything: the request is a FILE

The mechanism is the protocol `gopi worker --stop` already uses
(`internal/tools/service_control.go`: `StopRequestPrefix` = `TERM_`,
`ForceRequestPrefix` = `FORCE_`, `ControlDir`, `ControlStatePath`,
`ControlRequestPath`, `WriteControlText`, `ClearControlText`, `PidAlive`,
`LiveStateFiles`, `LiveStopRequestPIDs`). **No new mechanism, no new file
format, no new dependency** (Rule 35): the loop's request is the same kind of
durable per-pid file the worker's is, in a directory of its own name.

**Why a file and not a signal.** A signal has three defects this protocol does
not: it carries no provenance (Linux hands a handler no sender identity — the
premise of `internal/signalrec`), it requires permission the caller may not
have, and it is not durable — delivered to a process that is not yet listening,
it is gone. A file is read at the reader's own convenience, by a reader that can
be a different user, and it carries a body: `mergeStopRequestBody` records the
sender's pid, argv and moment, so the victim of a stop can name who asked, which
a signal handler cannot.

**The signal is the NUDGE, and it is conditional.** `--stop` writes the file and
THEN signals, because the file alone would not be noticed until the loop's next
boundary. But a signal is sent **only when the target's own published record
proves it runs this same build** (`mergeLoopState.speaksStopProtocol`, the
version compared against `buildVersion()`). The reason is the one thing this
protocol must never do: a signal into a binary that does not expect it could be
forwarded into the pass in flight. To anything else the file alone is delivered,
and the degradation is **announced** (`mergeStopFileOnlyNote`), not hidden
(Rule 15).

### The rejected alternative, and why

The obvious design is a **STOP file in the control directory, checked at the top
of each iteration** — no pid, no state record, no version — and it was rejected
for a reason worth recording: a global file cannot say WHICH loop it addresses.
Two loops can run in one profile (different repos, a test's private `GOPI_HOME`,
an operator's second shell), and a global stop file would take both — including
the one the operator did not mean to stop. The per-pid request plus a published
state record is what makes the command **addressable** rather than merely
effective.

## The loop's contract, kept inviolable

The brief's hard constraint: **the in-flight pass is never interrupted.** No
context is threaded into the pass, no cancellation is plumbed into it, and
nothing is signalled into it or its children. `runMergeLoop` runs the pass in
its own goroutine and only ever decides whether to WAIT for it:

- a **TERM** request ⇒ let it finish, then exit 0;
- a **FORCE** request ⇒ stop waiting for it and exit 0 now — which is exactly
  what the loop's own overrun bound already produced at the settle of the
  merger's retire (`mergeLoopForceWaitLine` versus `mergeLoopOverrunLine`).

`FORCE` is therefore a distinct **file**, never "a second signal": escalation
must be spelled, or a stop-and-retry wrapper escalates by accident.

The loop takes the merge lock **per pass**, never across the sleep
(`merge_lock.go`: process exit is the flock release), so a stop during the
inter-pass wait leaves nothing held.

## The measured defect that produced the re-read

**The first end-to-end run failed the acceptance, and the failure is the most
useful thing in this document.** The rig started a real `gopi merge --every 2s`,
ran a real `gopi merge --stop`, and the loop exited 0 — but it printed the
GENERIC stop line (`mergeLoopStopLine`, "stopping after the in-flight pass"),
not the DRAIN line the acceptance measures:

```
[t+1036s] loop stdout:
  gopi merge: every 2s — Ctrl+C / SIGTERM stops the loop...
  gopi merge: stopping after the in-flight pass
loop exit=0
```

The cause is a fact about the two halves of one request: `--stop` writes the
file **and then** signals, and the loop installs a SIGTERM handler — so **the
fast path almost always wins the race**. The signal was delivered while the pass
ran; the loop's handler fired; the durable file was standing on disk and was
never consulted. On the ordinary path the durable half of the contract was
**invisible**, and the operator read a line that says nothing about what they
asked for.

The fix is one rule applied at all three signal-capable boundaries: **the signal
and the file are ONE request, not two** — when the signal arm fires, the loop
re-reads the request file, and a standing request is what it reports. A bare
Ctrl+C with no file standing keeps exactly its pre-existing behaviour. This is
the same shape the worker's own stop forwarder already uses (a bare-signal nudge
first settles, then re-reads the file).

## The exit-code contract

Mirroring `gopi worker --stop`:

- **0** = the request reached a live loop. A requested stop is a SUCCESS — the
  same contract the loop's own exit 0 carries.
- **1** = there was nothing to stop, reported **by name** with the record prefix
  it looked for and the directory it searched (`mergeStopNoTargetErr`) — never
  a silent success (Rule 15).

`--wait D` deliberately does **not** change the exit code: delivery is the
contract, and the bound is an observation whose two ends are reported
distinctly (`mergeStopGoneLine` / `mergeStopStillRunningLine`). The default is
`mergeStopWaitDefault = 0` — no wait — because a default that blocked would make
the command unusable from a script while a long pass finishes.

## The control directory

The stop protocol lives under `<GopiDir>/merge/`, and the name is
**deliberately not** the existing `merger` directory
(`internal/tools/merger_graceful.go`): that one holds the retired daemon's log
and the rebuild-pending marks the merge path drains, and a stop request written
there would be read by nothing while looking like a merge control file
(Rule 1b/14 — one name per subject).

## A claim the code had to stop making

`TestMergeEveryLoopWritesNoStateFile` asserted the opposite of the new
behaviour, and `runMergeLoop`'s header and the help page both stated "no state
file, no stop flag" as deliberate. Both are now false, and both were **corrected
in the same change** rather than left standing: the header states the one record
a LOOP keeps and why (a one-shot run still writes nothing), and the help page's
NO-DAEMON paragraph distinguishes "no supervisor, no progress record" — still
true — from the addressing a cross-process stop needs.

## Verification

The two halves of the acceptance, and the exact observations, are in the task
report (`## Verification` / `## Evidence`). In short: a real
`gopi merge --every 2s` over a FIXTURE repo in a private `GOPI_HOME`, a real
second-process `gopi merge --stop`, the loop printing the drain line and exiting
0 **after** the in-flight pass, and the control directory empty afterwards; plus
the no-live-loop report, the four refusals, and the `--force` arm.

## Not done (honest)

- **A second live loop in the same profile was addressed, but not selected.**
  `runMergeStop` delivered to EVERY live loop it resolved. A `--pid`/`--all`
  selector would have been the natural refinement; it was never built. *(The whole
  face — the loop, `runMergeStop`, `--every`, `--stop` — went with the top-level
  verb on 2026-11-01; measured, no `runMergeStop` in this tree and
  `gopi task merge -h` lists no `--stop`/`--every`.)*
- **The state record was not a progress record.** It carried the pass count and
  the drain flag, not which branch a pass was deciding. The loop's `--status`
  reader was the surface for that question — it went with the top-level verb
  in the merge-removal epic (2026-11-01), so no spelling answers it today.
- **No peer-host stop.** The request was a local file; stopping a loop on another
  host was `ssh <host> gopi merge --stop`, which the operator composed rather
  than the command. There is no loop to stop now, on any host.
