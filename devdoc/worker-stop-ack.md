<!-- doc-class: historical -->
# The stop request gets an ANSWER — a receipt the sender can read

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Epic**: worker-stop-ack. **Branch**: `task/01a14314366e7e239ea2e4da180a4d52`.
**Files**: `cmd/agent/cli_worker_ack.go` (new), `cli_worker_ack_test.go` (new),
`cli_worker_control.go`, `cli_worker_foreground.go`.

## The defect

`gopi worker --stop --pid N` had ONE direction. The sender wrote the durable
request file (`TERM_<pid>`) and then **blind-waited** the whole bound — 10m
foreground, 25m daemon — with no way to tell

- "the worker read your request and is leaving at its round boundary" from
- "the worker never saw it".

The code said so itself, in the sender's own note: *the stop still lands, it just
cannot report the sender.* An operator could not distinguish a healthy drain from
a lost request without waiting the bound out, and a **script** (a unit's
`ExecStop`, a restart) had no signal at all.

## Why a FILE and not a signal

A signal carries no reply — Linux hands the receiver no sender identity and gives
it no way back. That is the premise `internal/signalrec` and the request file
already rest on. A receipt **file** has neither limitation, and it has two
properties the acceptance turns on:

1. **It needs no shared binary.** The receipt is readable by a sender running a
   *different file* — a foreign inode, or `(deleted)` after a rebuild, which is
   the shape **every rebuild produces**. That is exactly the shape where the
   signal contract cannot be verified, so the request file is the only door, and
   before this change such a stop was the blindest case of all.
2. **It outlives the writer.** A receipt written an instant before the worker
   exits is still observable by the sender afterwards.

## The shape

```
request   operator → worker   <control dir>/TERM_<pid>     (existing)
receipt   worker → operator   <control dir>/ACK_<pid>      (this change)
```

Both are key=value bodies in the **same vocabulary**: `workerAckSep` IS
`workerStopFieldSep`, so a reader that parses one parses the other.

The receipt body:

```
req_id=<sender pid>          which request this answers
req_ts=<sender timestamp>    when it was asked
acked_at=<RFC3339>           when the worker consumed it
acked_by=<worker pid>        who answered
mode=drain|force             which request shape was read (never invented)
round=<n>                    the claim-loop round it was answered at (omitted when
                             the face has no round counter — the foreground one)
exit=exit at the next round boundary (in-flight round is not interrupted)
```

The sender prints the raw body **and** two sentences that make it actionable, then
returns — it does not wait for the round to finish.

## Semantics that were deliberate

- **Written ONCE per request (idempotent).** A stop request has three consumers
  (the claim loop's boundary, the 2s watcher, the forwarder). The receipt is
  published on the **fresh transition only**, and the writer checks for a
  standing receipt for the same requester first — so three consumers produce one
  file. Nothing here **re-arms** anything: the 2026-10-15 self-waking hot loop
  (514 ticks of CPU in 5s from a wake-up that re-armed itself) is not
  reintroduced.
- **The sender owns the receipt after reading it — and now ARCHIVES it.** The
  worker never deletes it: a receipt deleted at exit could be missed by the very
  poll it exists for. The READER used to delete it once consumed, which destroyed
  the only evidence that the stop was acknowledged; since epic `01a17df9-524a`
  the reader MOVES it out of the live namespace
  (`ACK_<pid>.<req_id>.consumed`) and appends one line to `acks-consumed.log`.
  The live name is still free afterwards (a later request cannot read this
  answer), and the evidence is still on disk — see
  `devdoc/worker-stop-receipt-archive.md`. The archived receipts are collected
  by age on the EXISTING resident tick; no new scheduler.
- **`--wait 0` is unchanged**: the command returns as soon as the request is
  delivered and says so, without a receipt wait.
- **Multiple foreground workers**: the wait returns when **every** pid is either
  gone or has answered; a partial acknowledgement does not end it.

## Backward compatibility — both directions

- **An OLD worker writes no receipt.** Its behaviour is **unchanged**: the
  operator's own bound still governs, and the old end state (`exit 1`, "STILL
  running") is still printed. What is new is that the sender now **says why**
  instead of waiting in silence: *"this worker does not acknowledge stop requests
  (a worker from an older binary writes none)"* — once, mid-wait, and again as the
  verdict when the wait ends without an answer. Rule 15: a silence must name
  itself.
- **An old SENDER is unaffected**: the new worker still reads the same request
  file, and an `ACK_` file is read by nobody who does not know it.

## The measurement that changed the design (the pitfall worth remembering)

The first shape anchored "is this receipt MINE, and not stale residue from a
recycled pid?" on the **file's mtime** compared against the instant the poll was
armed. It failed, and the failure was silent:

```
start=2026-10-16T22:58:21.614284697+08:00 ack=2026-10-16T22:58:21.612692503+08:00 before=true
```

A file created **1.6 ms AFTER** the reading still carried an mtime **BEFORE** it —
on this filesystem (and in this suite's scratch root, a tmpfs) mtime is truncated
to a granularity coarser than `time.Now()`. The legit receipt was refused, the
sender waited out the bound, and the e2e measured **32.08s** against an 8s
expectation. The rewrite anchors on the receipt's **own `req_ts`** (the sender's
timestamp as the worker read it back), which no filesystem can truncate; a
receipt whose `req_ts` is missing or unparsable is **refused** rather than
trusted, because an unknown provenance cannot be this process's answer.

**Generalised**: never compare a file's mtime against a `time.Now()` taken
moments apart as an ordering proof — the clock resolution of the *filesystem* is
the weaker side, and the failure is one-directional (real receipts refused, never
fake ones accepted), so it reads as a timeout, not as a bug.

## Both arms, measured for real (Rule 31)

Three runs against a real worker, on a sandbox-shaped profile, with the STOPPER
running a **different file** from the worker (`gopi-A` / `gopi-B`, measured
inodes `31123514` / `31123527`):

| run | what | bound | elapsed | verdict |
|---|---|---|---|---|
| 1 | `--stop --pid` (drain), foreign inode | 2m | **0.208s** | `ACKNOWLEDGED the drain request`, `mode=drain`, control dir empty afterwards |
| 2 | a worker that consumes nothing (older-binary shape) | 6s | **6.02s** | `exit 1`, `STILL running`, *and* "does not acknowledge stop requests" |
| 3 | `--stop --pid --force`, foreign inode | 2m | **0.218s** | `ACKNOWLEDGED the force request`, `mode=force` |

Run 2 is the backward-compatibility proof: the bound is the operator's, unchanged,
and the only difference an operator sees on that path is that the silence now
names itself.

## How it was verified

Unit + e2e in `cmd/agent/cli_worker_ack_test.go`; the real acceptance in
`devdoc/worker-stop-ack-report.md` (the report carries run 1 — a real foreground
worker with a real `--stop` from a **different file** — and run 2 — the
no-acknowledgement path, bound unchanged).

```bash
go build ./... && CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
go test ./cmd/agent/ -run 'Ack|WorkerStop|StopRequest|DrainRequest|WorkerForeground|WorkerControl'
```
