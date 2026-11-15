<!-- doc-class: normative -->
# The record-filed handshake needed THREE states, not two

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

**Task** `01a12fba-6942-770d-acc9-a01c3fb994da` · **Branch** `task/01a12fba-6942-770d-acc9-a01c3fb994da`
**Date** 2026-10-12 · **Files** `internal/signalrec/signalrec.go`,
`internal/signalrec/signalrec_test.go`, `cmd/agent/signal_verdict.go`,
`cmd/agent/signal_record_outcome_guard_test.go`

## The defect

The record-filed handshake has two halves — the **publication**
(`signalrec.noteRecordFiled()`, called only after a successful append inside
`write()`) and the **wait** (`cmd/agent settleProvenanceRecord()` →
`signalrec.AwaitRecordFiled`). Its only published fact was *bytes on disk*, and
`settleProvenanceRecord` decided **how to report** a failure from a single other
check:

```go
if !signalrec.AwaitRecordFiled(signalrec.DefaultRecordFiledWait) {
    if signalrec.KillVerdictNow().Received {   // a signal arrived
        print recordLostLine                   // "this kill is recorded NOWHERE"
    }
}
```

A **refused** record satisfies exactly that condition. `reportUnfiled` prints
`[gopi] signal provenance NOT FILED (no public-dir fallback): …` — a deliberate
rejection with its own reason — and publishes nothing, so `RecordFiledNow()`
stays false while `KillVerdictNow().Received` is true. The door then burned the
whole `DefaultRecordFiledWait` budget and printed the loss line for a decision
the recorder had already explained, on purpose, one line above it.

Two different facts wore one sentence: *the recorder has not answered yet* and
*the recorder answered — refuse*. That is the same collapse the provenance epic
exists to end (a sourced drain vs an unsourced kill), one level up.

## The fix

**A third state, published where the decision is made.**

```go
type RecordOutcome string
const (
    RecordOutcomeNothingYet RecordOutcome = ""        // the zero value
    RecordOutcomeFiled      RecordOutcome = "filed"
    RecordOutcomeRefused    RecordOutcome = "refused"
)
func (o RecordOutcome) Refused() bool   // Rule 20: named for the value
func RecordOutcomeNow() RecordOutcome   // the door's reader
func RecordRefusedNow() (bool, string)  // refusal + the reason already printed
```

* `noteRecordRefused(reason)` is called from **`reportUnfiled`** — the ONE path
  that makes the decision — so the reason travels with the state and the door and
  the `NOT FILED` line cannot disagree about why.
* `noteRecordFiled()` **clears** a refusal and wins: a process with bytes on disk
  has lost nothing.
* `RecordFiledNow()` is unchanged — bytes on disk, nothing else. A refused record
  still answers `false` there; the refusal is a third state, never a redefinition
  of that one.
* `AwaitRecordFiled` is unchanged. **The wait's primary fact stays "bytes on
  disk"** — the refusal cannot make it answer earlier (settling on "we tried"
  would reinstate the silent loss the handshake exists to remove). What changed
  is only the door's *report*, read **after** the wait.

**The door** now reads the state outside the wait (Rule 2: the recorder owns the
fact, the door renders it):

| state | door prints |
|---|---|
| filed (or no signal reached the recorder) | nothing — the ordinary exit |
| refused | `recordRefusedLine` + the recorder's own reason; **never** `recordLostLine` |
| nothing yet + a signal received | `recordLostLine`, unchanged |

`recordLostLine` keeps its exact wording for the genuine loss. A refusal is
stated rather than silent, because a door that said nothing would be read as
"the record is still on its way".

## What the reachable refusal is, measured

The refusal shape an operator actually meets is *the launcher handed a directory
this profile did not mint*. Its destination could still be reachable one branch
away: `processLayoutEligible` admits the process-keyed layout for a role that
either is resident by argv **or** called `ClaimSignalHandlers()` — and a claiming
one-shot such as `gopi memory sync --every` qualifies. So a passed-but-unusable
`GOPI_SIGNALREC_SESSION_DIR` is refused (`handedDest` → `isSessionDir`), while a
missing session dir falls through to the process layout instead.

## Verification

See the task report (`task-report.md` in the run's session dir) for the exact
`Ran:` lines. The guard drives the **real binary** in a **real sandbox profile**
under a **real SIGTERM**, N≥3 repeats per arm, and asserts:

* **refused** — the `NOT FILED` reason is printed, `recordLostLine` is **not**,
  `recordRefusedLine` is; nothing lands on disk;
* **filed** — the record IS on disk (the last line is this kill's `SIGTERM`),
  `recordLostLine` is **not**;
* **nothing yet** — a signal arrived, no record, no refusal → `recordLostLine`
  **still prints**. This arm is the floor: a change that silenced the loss by
  settling on "we tried" would leave the other two green.

Arm (c) uses an **existing knob** and adds no dependency (Rule 35): the
recorder's own classification budget (`signalrec.IntentWaitEnv`) is raised past
the door's budget, which is a real ordering (the recorder is still working when
the door's unwind expires), not a synthetic stall. Arms are killed at the loop's
own start line, never on a blind sleep. Rule 27: each repeat is one tiny process
that fails fast against an unresolvable peer — no device, nothing saturated.
