<!-- doc-class: normative -->
# A dry run must be read-only AT THE OPEN — the layer where the write happens

`gopi chat backfill --dry-run` promised to report without writing. It reported
correctly and wrote anyway: on a copy of the operator's own ledger, ONE run
moved `memory.db`'s sha256 (`38056aa0ea3dfab4…` → `0a1135c8b7fcf2d7…`) and left
`memory.db-wal` **gone** (checkpointed away). The `dryRun` flag was guarding
every `UPDATE` and nothing else, and the write was never a statement.

## The mechanism, measured, in the order it was found

1. **The write is the OPEN.** `openChatDB` → `openLedgerIndexDB` →
   `openDB(path, false)` — a writable connection with `_txlock=immediate`, on
   which `ensureChatSchema` then runs ~8 DDL batches. `CREATE TABLE IF NOT
   EXISTS` is a logical no-op and still opens a write transaction, and the
   close checkpoints the WAL away.

2. **`openDB(path, true)` is NOT the fix** — this is the part that cost a
   measurement, because the package already had a read-only leg. On a
   device-backed ledger copy, that opener **closed the copy's `-wal` and `-shm`
   entirely** and rewrote the main file (`7c08da3c…` → `4bbb8467…`), because
   its DSN pins `_txlock=immediate` for every transaction and its first
   statement is a PRAGMA **write** (`busy_timeout`). A `query_only` pragma does
   not stop the SQLite library from checkpointing at close.

3. **The DSN-based read-only opener is the one that holds.** `mode=ro` +
   `_pragma=query_only(true)` (`LedgerReadOnlyDSN`) left the main file and the
   WAL byte-identical. Only `-shm` moved: that is the shared wal-index a reader
   is entitled to touch, and it carries no ledger data.

Measured, same copy, same order (a throwaway probe, deleted before the run
ended; its run is preserved verbatim in the task's session directory —
`evidence/open-shape-bytes.txt`):

```
[openDB-readonly-pragma] BEFORE db=7c08da3c533870e8 wal=df68215a1e0167c0
[openDB-readonly-pragma] AFTER  db=4bbb846758cfd796 wal=MISSING rows=1
[dsn-mode-ro]            BEFORE db=7c08da3c533870e8 wal=1a2fd49b16fe9e4c
[dsn-mode-ro]            AFTER  db=7c08da3c533870e8 wal=1a2fd49b16fe9e4c rows=1
```


| open shape | memory.db | memory.db-wal |
|---|---|---|
| `openDB(path, true)` (query_only pragma) | rewritten | **deleted** |
| `sql.Open(LedgerReadOnlyDSN)` (mode=ro) | unchanged | unchanged |
| `openDB(path, false)` + DDL | rewritten | **deleted** |

## The fix — a mode parameter on the ONE opener, not a second opener

`ledger_schema_union.go` grew `ledgerOpenMode` (`ledgerOpenReadWrite` /
`ledgerOpenReadOnly`) and `openLedgerIndexDBFor`. There is the same function,
not a parallel one, because a face's write has exactly three places it can
happen — the union build, the face's `ensureIndexSchema`, and the connection's
own writability — and a mode switch at the top keeps all three readable in one
screen. `openLedgerIndexDB` (unchanged signature) forwards `ledgerOpenReadWrite`;
`openLedgerIndexDBReadOnly` is the new shape.

Three properties of the read-only arm, each deliberate:

- **Nothing is created.** A dry run on a ledger that is not there reports it and
  exits 0 with `migrated=0 … dryrun=true`. The writable opener would have built
  the file; that build IS the write this mode exists to avoid.
- **Nothing is ensured.** The union question is skipped: completing a partial
  ledger is a write, and a dry run may not fix a foreign file.
- **A failure is loud.** No fallback to the writable shape — that fallback is
  the defect (Rule 15).

`chat.go` grew one funnel, `chatIndexOpenDB(ctx, readOnly bool)`, and both chat
faces route through it: `ChatBackfill` (`chatIndexOpenDB(ctx, opts.DryRun)`) and
`ChatRepairFirstMsgs` (`chatIndexOpenDB(ctx, dryRun)`). The repair arrived as
the funnel's second caller on the branch that carries the first_msg work
(01a17a8a) — its own dry run had the same defect, measured the same way.

## The judgement is BYTES, not a counter

"repaired=0" is green on the broken code: nothing was ever repaired, because the
UPDATEs were already guarded. Only the ledger's own bytes can tell a read from a
write, so the regression test compares size + sha256 of `memory.db` and
`memory.db-wal` across two dry runs, and demands the run still REPORT work
(`total=1`) — and it does this once per face, because the two faces reach
different openers: `TestChatBackfillDryRunLeavesTheLedgerBytesUntouched` for
backfill, `TestChatFirstMsgRepairDryRunLeavesTheLedgerBytesUntouched` for the
first_msg repair. Each has its own red proof (point that face back at
`openChatDB`) recorded in its header, so a green cannot be inherited from the
sibling.

The test skips (never passes) where the scratch host is memory-backed: a tmpfs
ledger carries no `-wal` at all, so the subject would not exist.

## Second finding: the repair flag sits ON a writable verb

`gopi chat backfill --repair-first-msg --dry-run` runs TWO commands: the legacy
migration (`runChatBackfill`) and then the repair. Routing only the repair's
opener to read-only left the FIRST half writing — measured on the verification
tree: `--dry-run` still moved `2f1a10ba…` → `f923fdbe…` and checkpointed the WAL
away, while `-h` and `--version` on the same ledger changed nothing. So the fix
is only complete when BOTH halves route on the flag; a later `--dry-run` face
added to this verb must route too.

## Why the migration half runs at all in a dry run

`ChatBackfill` runs unconditionally before the repair branch, so a
`--repair-first-msg --dry-run` still scans 366 legacy candidates and reports
`migrated=366 … dryrun=true`. That is arguably noise for a reader who asked
about `first_msg`, and it is NOT changed here: the shape of what the verb does
is a separate decision, and this task's subject was the write, not the report's
scope. Recorded in the task report under `## Not Done`.

## Check

```bash
go test ./internal/tools/ -run 'TestChatBackfillDryRunLeavesTheLedgerBytesUntouched|TestChatIndexOpenDBReadOnlyRefusesToWrite'
```

Red proof: put the dry-run arm back on the writable opener
(`chatIndexOpenDB`'s `readOnly` branch returning `openChatDB(ctx)`) and the
first arm fails with `dry run 1 DELETED memory.db-wal`, while `stats` still
says `migrated=1 … dryrun=true` — the counter is green on the broken code.
