<!-- doc-class: normative -->
# A claiming door must not outrun the recorder: the record-filed handshake

> **PARTLY REVERSED 2026-11-06 — the command this page names is BACK, minus one flag.**
> `gopi memory sync` was restored the day after its deletion, as a MANUAL door:
> one invocation = one session, exit. `--peer` / `--push` / `--pull` / `--dry-run` /
> `--serve-assets` / `--timeout` all run again; the runnable shape and its measured
> two-way session are in `devdoc/memory-sync-manual-door-restored.md`. **What is
> still gone is the `--every <dur>` cadence flag**, whose reader and loop went in
> batch 5 — so a line below that carries `--every` is a RECORD, not a command to
> paste (it exits 1 with `unknown flag`). The one-way copy `gopi memory backup`
> is the other manual cross-host door and its guarantees differ: it REPLACES the
> peer ledger instead of merging it.

*Measured 2026-10-12 on host hosta, pre-fix tree `45b54d81f`. Task
`01a12f64-45e0-7108-9839-43a729f1b3e0`.*

## The defect

A gopi verb that owns its own terminating-signal handler — the class that must
call `signalrec.ClaimSignalHandlers`, because the recorder's argv table cannot
classify it — unwinds on **its own goroutine** and then exits the process. The
provenance record is filed on a **different** goroutine, woken by the same signal
delivery, which still has real work to do first: resolve a session destination,
read `/proc`, tail the session log, append a line.

Nothing synchronises the two. `os.Exit` does not wait for a goroutine.

Measured, `gopi memory sync --every 5s --push` in a sandbox profile:

```
run=1  exit=0 stop_line=1 refusal=0 record=LOST
...
run=12 exit=0 stop_line=1 refusal=0 record=LOST
tally: 12 record=LOST
```

`stop_line=1` proves the door's unwind ran (the loop printed
`… stop signal — the loop stops here`); `refusal=0` proves the recorder did not
refuse the destination (the session dir was seeded and filable). The record simply
never reached disk.

**The differential that isolates the cause.** Same verb, same profile, same
destination, same two-second wait:

- `SIGHUP` → process stays alive, record **FILED** (`[('SIGHUP','signal')]`).
- `SIGTERM` → **LOST** 12/12.

`SIGHUP` is watched by the recorder but wakes no verb-side stop channel;
`SIGTERM` wakes both. So the loss is not "the recorder cannot write here" — it is
the **scheduling race** between the recorder's goroutine and the door's own
handler. The door's path is short and ends in `os.Exit`; the recorder's is not.

**Why this re-opened the epic's own hole.** The provenance file is the
deliverable of the signal-provenance epic. Before this change, a claiming door
could lose it and nothing in the tree reported the loss, so a reader could not
tell *never signalled* from *signalled, the record lost the race* — which is the
distinction the whole epic exists to make.

## The fix — one publication step, one waiter

**Publication (§ in `internal/signalrec`).** `write()` calls `noteRecordFiled()`
immediately after its successful `O_APPEND` write, and nowhere else. So the fact
published is "the bytes are in the file", never "we tried": a refused or failed
record can never be published. No `fsync` is involved and none is needed — the
kernel keeps the page cache after the process dies, which is exactly the level of
durability an evidence line needs (and an fsync would be a device-load-shaped
cost on a shutdown path).

**The waiter (§ in `internal/signalrec`).** `AwaitRecordFiled(budget)` is the ONE
synchronisation point:

- returns `true` the instant `RecordFiledNow()` is true;
- if **no** signal has reached the recorder it gives up after
  `recordFiledDeliverySettle` (5 ms) — an ordinary exit must not pay the budget,
  and there is nothing to wait for;
- if a signal **has** arrived, it waits out `DefaultRecordFiledWait` (500 ms) and
  then returns `false`, which the caller must report.

The short settle is what keeps the two facts apart: the recorder's goroutine can
be scheduled *after* the door's handler even though the signal has landed, so the
wait needs a small window to distinguish "not yet" from "never" — a few
milliseconds, not a budget.

**The door-side half (§ in `cmd/agent/signal_verdict.go`).** `settleProvenanceRecord(role)`
calls that waiter and, when a signal had arrived and the record did not land,
prints `recordLostLine`. The finding is printed rather than swallowed because an
invisible loss is the defect; an ordinary exit prints nothing.

**Where it is invoked.** At each claim boundary's unwind, after the door's own
stop/closeout line and before the process leaves:

| surface | file |
|---|---|
| `gopi memory sync --every` | `cmd/agent/cli_memory_sync_every.go` |
| `gopi merge --every` (stop, overrun, post-pass) — **removed 2026-11-01**, `merge_run.go` is gone; the surviving face is `gopi task merge` | `cmd/agent/merge_run.go` (deleted) |
| `gopi cli` terminal chat | `cmd/agent/cli_term.go` |
| `gopi cli --once` | `cmd/agent/cli_term_once.go` |
| bare-prompt `gopi "<prompt>"` (success and error arms) | `cmd/agent/main.go` |

The barrier is *not* duplicated: every one of those calls the same
`AwaitRecordFiled`/`settleProvenanceRecord` pair supplied by the claim boundary.
The door that does not claim its handlers (the recorder re-raises instead) does
not need the settle — post-claim the recorder no longer re-raises, and the
recorder files the record before it would have.

## The invariant

> If a signal reached a process that claims its own handlers, then by the time
> that process exits, the recorder's record is on disk — or the process has said
> so, naming the loss.

Against the pre-fix tree the same guard is red, so the arm is decided by the CODE
and not by the scheduler.

## The regression guard

`cmd/agent/signal_record_filed_guard_test.go` drives a **real re-executed
binary** (`gopiTestBinPath`, the suite's one build) in a **real sandbox**
(`gopi tool sandbox create`, Rule 34 — never a hand-built directory), waits for
the loop's start line (not a blind sleep), sends a real `SIGTERM`, and then asks
the **filesystem** whether the record grew. It repeats 5×, because the defect is
a race and one green run proves nothing (Rule 27: bounded repetition, no load).

The assertion is a file and the *content* of the last line, never an exit code:
exit 0 was true on the pre-fix tree too, which is precisely why the loss went
unnoticed.

- Red on master's sources: `run 1/5 … did not grow: 0 -> 0`, child output shows
  only the start line.
- Green on the fixed tree, repeated: 3 consecutive `ok` runs (plus the first),
  and 12/12 `LANDED` via the real binary outside the test.

The waiter's contract has its own unit test
(`TestAwaitRecordFiledSettlesOnTheRecordAndNeverOnTheAttempt`): no-signal →
false and no budget paid; filed → true immediately; signal-without-record →
false only at the bound.

## Not done / not fixed here

- **The refusal is a different finding.** Measured in the same session: a claiming
  verb launched *by an agent* inherits `GOPI_TASK_RUN_ID` (another profile's task
  id) and then **refuses** its record outright — same verb, same SIGTERM, no
  record, with a `NOT FILED` line naming a directory that belongs to a different
  profile. That is a task-env heredity question, not a race, and it was filed
  separately (bug row `01a12f76-c01f-77eb-8dcf-ccd21a7a53b9`). Fixing the race
  does not fix it: with a filable destination the record lands (12/12 here), with
  an inherited foreign id it is refused by design.
- **Doors that do not call the settle.** The claim boundaries in the tree are the
  five above; a future door that claims its handlers without settling would
  reintroduce the loss for its own surface. The one-place rule (Rule 2) is what
  keeps that cheap to notice, but nothing mechanically forces a *new* claim site
  to settle — the guard covers the surface it drives.
- **`write` is still best-effort within its goroutine.** A recorder whose
  destination is unwritable still falls back to its refusal report; the handshake
  never turns a refusal into a wait (a refusal publishes nothing, so the waiter
  simply expires and the door reports the loss).
