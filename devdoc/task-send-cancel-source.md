<!-- doc-class: historical -->
# `gopi task send`: the status gate and the opt-in `--cancel-source`

Task `01a10fa2ad287ceba899cde7cac7e5c2`. Two changes to ONE verb, both of them
answers to the same failure: **a row that exists twice**.

## The defect this closes

`gopi task send --task-id T --peer P` carried a row to a peer's ledger in
whatever state the row happened to be in. The sharp case is `in_progress`:
a leg is running HERE, the peer's ledger receives a pending row, the peer's
dispatcher sees queued work and runs it — **one row, two legs**. And moving a
row between hosts was a three-step manual migration (send it, read the receipt,
remember to cancel the source) whose third step is the one a human forgets,
which produces the same two-ledger state by omission.

## 1. The status gate

Only a `pending` row travels. Every other status is refused **by name**, with
the current status and a reason that says what sending it would actually cost,
**before any peer is contacted**:

```
$ gopi task send --task-id t-x --peer P
gopi task sync: row send: task t-x is in_progress — only a pending row may be
sent: a leg is RUNNING on this row here — sending it would hand the peer's
dispatcher work already in flight (two legs, one row); finish or cancel the
local run first
$ echo $?
1
```

| status | reason the gate gives |
|---|---|
| `in_progress` | a leg is RUNNING here — the peer would run a second one |
| `claimed` | the same hazard one step earlier (a holder is stamped) |
| `completed` / `verified` | a copy carries no work — an empty husk the peer's dispatcher skips |
| `failed` / `cancelled` | terminal; a copy cannot re-run it |

Two properties of the gate are deliberate:

- **It runs before the export**, so a refused send is *provably* a no-op — the
  tests assert the exchange seam was never called. A refusal that had already
  written the row onto the peer would be no refusal at all.
- **It says nothing about an absent row.** This ledger holding no such id is
  the *export's* refusal (which names the id); two refusals for one fact would
  be two places to keep in step.

**Idempotence falls out of the gate**: once a row has moved (cancelled or
otherwise no longer pending), the second send is refused by the pending check
instead of depositing a duplicate.

## 2. `--cancel-source` — opt-in, and the order is the safety argument

Without the flag, `send` is exactly what it always was: a symmetric push-half
that leaves the source row alone (the default is pinned by a test).

With the flag, the source row is cancelled HERE — status `cancelled`, with an
auto-written reason naming the peer and the receipt — and **only after a peer's
ledger reported it PERSISTED** (`stored` / `migrated` / `already current`):

```
$ gopi task send --task-id t-x --peer P --cancel-source
send P: stored (row t-x, status=pending)
cancel-source: t-x cancelled here (peer P holds it: stored)
$ gopi task show t-x | grep -E 'status|reason'
status:         cancelled
reason:         sent to peer P by `gopi task send --cancel-source` (receipt:
                stored, row t-x) — cancelled HERE so this host does not
                dispatch it a second time; undo with `gopi task update t-x
                --status pending --reason WHY`
```

The negative half is the important one:

```
$ gopi task send --task-id t-y --peer P --cancel-source   # peer unreachable
send P: unreachable (fail-soft): row send: ssh P: exit status 1 (stderr: …readonly database…)
cancel-source: t-y LEFT PENDING here — no peer persisted the row (refused by
peer P: …); send is a push, not a move, so the source is unchanged
$ gopi task show t-y | grep '^status'
status:         pending
```

A refusal, an unreachable peer and an unparsable answer are all
**non-persisting outcomes** and all leave the row exactly where it was: still
pending here, still dispatchable. A failed migration is a no-op, never a lost
row. The reason also carries its own undo, because a cancellation here is a
local status write and nothing more (no archive, no delete).

### Three decisions worth their own paragraph

- **The decision is read from the RECEIPT, not from the text a human reads.**
  `SyncRowSendEx` returns structured receipts beside its lines, and
  `SyncRowReceipt.Persisted()` is the single comparison the cancellation stands
  on — so the printer of the receipt line and the canceller of the row cannot
  disagree about what "the peer took it" means. `SyncRowSend` (the text-only
  face) is unchanged, and still delegates.
- **"No answer" is a NAMED outcome.** A transport failure sets
  `Unreachable` on the receipt rather than leaving an empty document a caller
  could read as "already there" — that confusion would cancel a row no peer
  holds. This is the one field that made the negative tests possible.
- **The local cancel re-reads the row first.** The send takes real time (an ssh
  hop per peer), so a row that has gone `in_progress` since the gate opened is
  NOT cancelled — that is the same double-dispatch hazard the gate refuses, one
  door later. And a peer-holds-it / local-write-failed outcome is *reported*
  (the note names the window and says the row may run twice) rather than hidden;
  it is the only arm that also exits non-zero, because it is the only one where
  a script reading the exit status would otherwise believe the migration
  finished. The safe non-cancellations stay exit 0 — including the refusal case
  (a refused send must not make an operator's wrapper avoid the flag on exactly
  the sends that need no protection).

### A defect this work found in itself

Writing the named-ledger test caught a real bug in the cancellation: the
*read* honoured `--db PATH` and the *write* did not. The task family resolves
its ledger from the environment at call time, so the cancel would have landed
in the process's own ledger while the gate had read the caller's — silently
cancelling a namesake row in a different ledger. Measured (the row read as
pending, the named ledger never moved), fixed by running the write under the
same pin (`syncRowWithLedger`), and pinned by
`TestRowSendCancelSourceHonoursTheNamedLedger`, which asserts BOTH ledgers
afterwards.

## What was NOT touched

- **`gopi task push`** (the GIT channel) — a different leg, no gate, no change.
- **The default send** — no flag means no read, no write and no re-read of the
  source row.
- **`gopi task receive`** — the flag is not admitted there (pinned by a test):
  a receive moves no row, so an admitted flag could only be a typo.
- **No `--force`, no escape hatch.** The gate is a refusal, not a preference
  (Rule 35: an escape hatch is the rule reversed).

## Where it lives

| file | role |
|---|---|
| `internal/tools/memory_sync_row.go` | the receipt type + `Effect()`/`Persisted()`, `SyncRowSendEx` (receipts kept), `syncRowSendToOne`, `syncRowStatusGate`, `syncRowWhyNotPending`, `syncRowStatusOn`/`syncRowWithLedger` |
| `internal/tools/memory_sync_row_cancel.go` | `SyncRowCancelSource` + its one disposition type and the reason/note formats |
| `cmd/agent/cli_tasks.go` | the `--cancel-source` flag constant, the `tasksOptions` field, the parser arm, the admission-table row |
| `cmd/agent/cli_tasks_sync.go` | `runTaskSend` (flag → cancellation), the `--cancel-source` semantics in the shared help page |
| `internal/tools/memory_sync_row_gate_test.go` | the leg's gate/cancel tests (the negative arms) |
| `cmd/agent/cli_tasks_send_cancel_test.go` | the command's tests through the REAL binary |
| `cmd/agent/cli_tasks_sync_surface_test.go` | the rig gained an armed PEER profile + `peerRun`, so a row send can be proven against a real second ledger |

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` — OK.
- Tests: `internal/tools` gate/cancel tests + `cmd/agent`'s send tests (see the
  task report for the exact runs and their output).
- Ran: the five acceptance scenarios through the real binary against two real
  ledgers (the task report carries each invocation and its observed output).
