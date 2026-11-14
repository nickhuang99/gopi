<!-- doc-class: historical -->
# The dependency edge did not travel — `task send` shipped a chain as N free rows

Task: `01a11463-f389-743c-9fd2-8eb15ba1bb9b` · 2026-10-07 · peerbox

## The scene (what was measured, before any change)

`gopi task send` moves ONE ledger row to a peer. Sending the five links of a
sequential chain one row at a time left every peer row with an EMPTY
`parent_task_id`, so a five-step sequential job arrived as five claimable tasks —
the peer's dispatcher took the whole chain at once.

That is not a theory. It is the shape of HP's ledger today: **2637 rows tagged
`from-peerbox`, and not one of them carries a parent** (read-only
`sqlite3 ~/.gopi/memory.db` on HP; the chain the send was built for is
`01a113e4-6730 → 6776 → 67e0 → 6846 → 6887`, all five `completed`, all five with
`parent_task_id` empty). The rows were ordered by the operator, not by the ledger.

## The verdict: a GAP, on the sending half — and the receiving half was already right

Three candidate causes were on the table. Each was answered by running something:

| candidate | how it was answered | verdict |
|---|---|---|
| **the edge is deliberately node-local** (a dependency is a fact about a machine, not a row) | `internal/tools/memory_sync_task.go` — `SyncTaskRow`, the delta wire type, had NO parent field at all, while `memory_sync_delta_test.go` / `memory_sync_harvest_test.go` INSERT `parent_task_id` *as a column of `tasks`* — i.e. the row's schema has it, the wire does not | **falsified**: the column is a column of the LEDGER, and the LEDGER is what sync exists to replicate |
| **the receiver drops it** | `syncDeltaInsertRow` and `harvestInsert` hard-coded `parent_task_id` to `''` in their INSERT — but they had nothing else to write, because the wire never carried the value | **falsified as the CAUSE, confirmed as an accomplice**: the receiver's `''` was honest about an input it never received |
| **the edge exists but does not travel** | the mesh wires are `SyncTaskRow` (delta: session/receive/claim-broadcast) and `harvestRemoteRow` (anchor window + row receive); **both lacked the field** | **CONFIRMED — this is the whole defect** |

So: **design, but an unfinished one.** Of the two halves, the RECEIVING half was
already correct-by-design in the one place it matters — `taskParentGateDenied` /
`taskRowClaimable` (Rule 33) reads the receiving ledger's own `parent_task_id`, so
a row that arrives WITH an edge is parked by the peer's own dispatcher, with the
peer's own `[queue-skip]` marker. Nothing about the gate had to change. The
sending half was the gap, and it is a gap in **two** transports.

## What was changed

One judgement, three lanes, no new policy (**Rule 14 — one definition**):

**`syncParentVerdict`** (`internal/tools/memory_sync_delta.go`) is THE answer to
"can this ledger honour the edge this row carries", and it has four arms:

- `syncParentOK` — no edge at all, or a parent this ledger holds with a
  **releasing** status, or a parent that is merely **live** (`pending`/`claimed`/
  `in_progress`). A live parent is a PASS: the dependency is real and unmet, which
  is exactly what Rule 33 models, and the peer's dispatcher must decide it — not
  the sender's guess about it.
- `syncParentDangling` — this ledger holds no such row. **Rule 33 parks such a
  row forever** (no override, and `cancelled`/`failed` never release), so writing
  it writes an unclaimable row. **Healable**: the parent may be another row of the
  same patch, so the import **spools** it and re-asks after the patch's own rows
  have settled. Still dangling on the second pass → **refused by name**.
- `syncParentTerminal` — the row exists with `cancelled`/`failed`. **Not
  healable by waiting**, so it is refused immediately, and the refusal names the
  repair that exists: a **REISSUE** of the parent (its successor row carries the
  work — the `superseded` verdict is in the release set for exactly this).
- `syncParentUnreadable` — the lookup could not be read. Never assumed satisfied
  (`taskParentGateDenied`'s own nil-lookup rule), and never retried, because the
  retry asks the same broken ledger.

The **release set itself was not touched**: `taskParentReleaseStatuses` still
means exactly what it meant. The change asks it; it does not redefine it.

**The field now travels on both wires.** `SyncTaskRow.ParentTaskID` and
`harvestRemoteRow.ParentTaskID`, read through ONE column expression
(`syncParentColumnExpr`, `COALESCE(parent_task_id,'')`) — the COALESCE is the
compat channel, so a peer running a pre-fix build answers `''` instead of taking
the whole transport down with a `no such column`.

Five write sites now honour the edge: the delta INSERT, the delta MERGE (an
already-present local row **adopts** an edge it lacks — the repair arm for rows
that migrated before this fix), the harvest INSERT, the harvest UPDATE (same
adopt, inside its transaction), and the terminal-mirror INSERT. The delta import
counts the adopts in their own field, **`RowsParented`**, because "the row was
already current" and "the row was already current AND the edge landed" are
different answers for an operator deciding whether a re-send repaired the chain.

**Refusals are loud and quotable.** A refused row reports through the existing
per-row channel (`RowsRefused` + `stats.Errors`) and the peer's receipt carries
the sentence; the row is NOT written. Measured, in the real end-to-end run below:

```
send hp: REFUSED: row 0 (01a114d6-…-fe6a): parent_task_id "01a114d6-…-54a3" is not
in this ledger — a pending row whose parent names no row is parked permanently
(Rule 33 releases only on completed|superseded|verified), so the edge is refused
rather than written as an unclaimable row: send the parent to this node first,
then this row
```

## The correct chain-migration path

**Rule 33's gate is enforced by the RECEIVER.** There is no bypass, no cascade,
no staleness release and no `--force`, so a chain that arrives in the wrong order
or with a missing parent produces permanently parked rows. Three routes, in order
of how much of the chain they carry:

1. **`gopi task sync --peer <P>` (the window legs) — preferred for a migration.**
   The anchor window ships many rows in ONE patch, the settlement loop is
   order-tolerant, and a child listed before its parent is simply spooled and
   re-asked. Good for "move this batch to the other node".
2. **`gopi task send --task-id <child>` one row at a time, IN CHAIN ORDER.**
   Parent first, then each child. A child sent before its parent is **refused by
   name** (the sentence above) — never written parked, and never silently shorn
   of its edge. This is the right route when the sender holds the chain and the
   receiver should not see unrelated rows.
3. **Repairing a row that already arrived without its edge.** Re-send the row
   (or let a session sync reach it) — the delta/harvest MERGE now **adopts** an
   edge onto a local row that has none, so the repair is a re-send rather than
   ledger surgery.

The one thing that needs no repair path: a row that arrives with an edge IS
handled by the peer's own Rule 33 machinery, and its `[queue-skip] parent <id>
not released (status=…; releases on completed|superseded|verified)` line names
WHY it is waiting (Rule 15 — the operator must never have to ask).

## Disposal of the rows already broken

State the scope honestly first: **the fix repairs the mechanism, not the history.**
HP's 2637 `from-peerbox` rows carry no edge, and nothing in this change guesses at
one. Specifically:

- **Terminal rows with no edge are harmless and need NOTHING.** Measured: a
  `completed` row whose edge names a nonexistent parent renders fine in `task
  show` and never appears in the pending view. The gate is not consulted for a
  terminal row.
- **A pending row on the peer with no edge is the real damage**: it is claimable
  NOW, in parallel with its siblings and with whatever the origin node still
  holds. The disposal is a RE-SEND of its edge, not a cancellation:

  ```bash
  # on the origin node, for each link of the chain, in chain order:
  gopi task send --task-id <child> --peer hp
  # the MERGE adopts the edge onto the peer's existing row (RowsParented),
  # and the peer's own dispatcher then parks it correctly.
  ```

- **If the origin row is already terminal** (`completed`), the peer's copy is
  history and its missing edge costs nothing — leave it. Only live, claimable
  rows need the re-send.
- **Never "fix" this with `gopi task update --parent-task-id`**: that flag is
  SUBMIT-only (it is not a reparent path), and Rule 33's gate is not repaired by
  editing a parent's status either (`cancelled` is terminal).

## Verification

See the Rule 5 report (`task-report.md`, same session directory) for the
`Ran:` lines and the measured output: the RED→GREEN pair, the real `ssh hp` chain
send with the operator's ledger untouched, and the peer-side read-back.

## Not done

- **The 2637 existing HP rows were not re-sent.** That is an operator decision
  (the peer's rows are HP's data and this task's brief puts HP's ledger under
  read-only discipline); the commands above are the recipe.
- **No sender-side pre-flight.** The refusal happens at the RECEIVING side, so a
  child sent child-first costs one ssh round trip before it is refused. A
  sender-side probe was written and then removed: it would have needed a second
  spelling of "which file is the peer's ledger" (Rule 14), and the receiving-side
  refusal is where the ledger under judgement actually is.
