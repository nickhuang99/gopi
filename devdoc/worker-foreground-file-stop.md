<!-- doc-class: historical -->
# The foreground worker's stop channel — the file is the delivery, the signal an optimisation

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Class**: reference
**Task**: `01a13279-9d6c-776c-8fc1-4a479d264871`
**Commit cut**: `8356a49` (the state this was measured against)

## The defect (measured 2026-10-13)

`gopi worker --stop <pid>` — whose own help promises "finish the in-flight round,
then exit" — refused to act on a FOREGROUND worker (`gopi worker`, no `--daemon`)
whenever the target ran a different binary than the stopping command, degrading to

```
gopi worker: pid N runs a DIFFERENT binary than this command (A vs B) — NOT signalling: …
             a foreground worker has no request file to deliver either; stop it yourself:
             `kill -SIGTERM N`
```

That state is not an edge case: **it is the normal state after any rebuild**, because
`/proc/<pid>/exe` then reads `.../bin/gopi (deleted)` (the running image's inode was
replaced on disk), so `workerSameBinary` — a device+inode comparison — reports a
mismatch against the freshly built binary the operator is running `--stop` from.

**Root cause (5-why).** Why the refusal? → because the only door the control face had
to a foreground worker was a SIGNAL. → Why was a signal unacceptable? → because a
worker whose signal contract is unknown may forward the stop into its in-flight RUN
CHILD and kill the very round the request exists to protect (measured 2026-09-13 on
the daemon face). → Why was there no other door? → because the foreground worker had
**no delivery channel at all**: `workerCtxFor` returned an INERT `workerDaemonCtx`
for every non-daemon process, so the foreground worker resolved no control dir,
published no state file, watched no request file, and cleaned up nothing.

The daemon face has the durable request FILE, consumed at the CLAIM BOUNDARY
(`consumeDrainRequests`) — which a worker honours **with no signal at all**. The fix
is not a new mechanism: it is giving the foreground worker the channel that already
exists.

## The change

### 1. Two independent capabilities, not one `writer == nil` (Rule 15)

`workerDaemonCtx` grew a second field and two predicates, because "publishes a state
file" and "consumes a stop request" are different questions:

| capability | field | predicate | who gets it |
|---|---|---|---|
| PUBLISH — the daemon state file | `writer` | `publishes()` | the resident DAEMON only |
| REQUESTS — the durable stop/force files | `dir` | `consumesRequests()` | **both faces** (`workerCtxFor`) |

Every method now asks the capability it actually needs (`markDraining` / `setRound*`
/ `setIdle` / `markClaiming` on `publishes`; `consumeDrainRequests` /
`installFileWatcher` / `cleanupRequests` / `drainRequestPending` on
`consumesRequests`) — so the two cannot be conflated again. The old `c.writer == nil`
guard was the exact conflation: it answered "this process writes no state file" and
was read as "this process watches no request".

**`consumesRequests` is DERIVED from `dir`, not a stored flag** — and that shape was
chosen by a measured failure, not by taste. A `requests bool` field was tried first:
the production constructors set it, but the existing test fixture (`drainTestCtx`)
built the struct literally and did not — so every request path went inert and the
whole `cmd/agent` package **hung** (a worker waiting forever for a stop no channel
could deliver). One field every constructor must remember is one field a fixture can
forget; deriving the capability from the directory makes the zero value the only
inert shape, and `dir != ""` is precisely "this context has a request directory".

**The daemon-detection semantics are UNCHANGED, deliberately.** The state file is what
`--start`/`--restart`/`--status` read to answer "is a worker already running?", so a
foreground worker publishing one would make `--start` refuse as "already running" and
`--status` report a daemon that does not exist. `workerRequestCtxFor` builds the
foreground shape with `writer == nil, requests == true` — no state file, no lock,
exactly as before, plus the file channel.

### 2. The `--stop` foreground arm delivers by FILE

`workerForegroundAsk` now mirrors the daemon arm's two-shape order:

1. write the durable request file (the delivery; the worker reads it at its claim
   boundary — `TERM_<pid>`, or `FORCE_<pid>` under `--force`),
2. add the signal **only** on proven binary identity (`workerSameBinary`),

and the already-standing check comes FIRST, so a repeated `--stop` is a reported
no-op that neither rewrites the file nor signals twice (the daemon arm's own
idempotency contract).

A different binary is therefore **not** a refusal — it is file-only delivery, the same
conservative direction `workerForeignDrainBinary` already chooses for the daemon. No
`kill` dead-end is printed. The ONE remaining nothing-delivered case is a control
directory that cannot be resolved (`workerForegroundNoDirFmt`), which names the
failure and the operator's own door rather than pretending a request exists.

## Why this is safe (the drain guarantee)

The `--force` spelling uses `SIGUSR1` for a proven target and `FORCE_<pid>` for an
unproven one, so the escalated path keeps working across a mismatch too; the signal
set itself is untouched (`installWorkerStopSignals`).

## Verification (see the task report for the full evidence)

- Unit: `TestWorkerStopForegroundDeliversByFileWhenTheBinaryDiffers` (file delivered at
  `consumeDrainRequests`' own path, sender = this process, no `stop it yourself` /
  `NOT signalling` wording, target **not** signalled),
  `TestWorkerStopForegroundSignalsAProvenTarget` (same inode → file + signal → pid
  reaped), `TestWorkerForegroundStopIsIdempotent` (standing request not rewritten).
- Real run (Rule 31), 2026-10-13: `bin/gopi` ran a foreground worker; the stop was
  issued by `/dev/shm/gopi-second-build` (a copy — **different device:inode**). Result:
  the worker's own line names the file as the consumed request with the requester's
  sender provenance, the process exited **0** in 501 ms, the next queue row stayed
  **pending**, the sibling rows were untouched, the control dir was empty afterwards,
  and `signal: terminated` appears **0** times.

## Red proof (the pre-fix behaviour)

Re-inserting the old refusal arm in `workerForegroundAsk` made
`TestWorkerStopForegroundDeliversByFileWhenTheBinaryDiffers` fail with
`the ask must name the file as the delivery` — and the mutation was restored
byte-for-byte (`diff` against the pre-mutation copy: identical).

## What this is NOT

- Not a second reader of the request file: the foreground face consumes through the
  same `consumeDrainRequests` (Rule 14) — there is still exactly ONE reader.
- Not a state file for the foreground face: `--status`/`--start` still see a
  foreground worker only through the process table (`foregroundWorkerPIDs`).
- Not a graceful-stop redesign: the signal set, the drain semantics and the
  claim-boundary consume are untouched.
