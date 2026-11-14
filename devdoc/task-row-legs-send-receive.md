<!-- doc-class: historical -->
# The row legs: `gopi task send` / `gopi task receive`

**Status:** landed (branch `task/01a0feb6204b7652ba0d17bcb9a96f19`).
**Code:** `internal/tools/memory_sync_row.go`, its test
`internal/tools/memory_sync_row_test.go`, CLI wiring in
`cmd/agent/cli_tasks.go` + `cmd/agent/cli_tasks_sync.go`, verb names in
`internal/taskverbs/verbs.go`.
**Report:** `tasks/01a0feb6204b7652ba0d17bcb9a96f19-report.md`.

## The defect this closes

`gopi task sync --push-only --task-id X --peer hp` was the family's only
single-row push. It rides `--claim-remote`, which is a **claim broadcast**: the
peer mirrors the row's lock and answers `accepted/refused`. It never stores the
row. Measured on this bench (same binary, same peer):

```
push hp: accepted (1 row(s))
$ ssh hp 'gopi task show <X>'
gopi task error: no task with task_id="<X>"
```

The receipt was true about the lock and false about the row. That is Rule 15b's
shape — a claim about an effect nobody observed — except the effect being
claimed (a row travelling) is not the effect being produced (a lock mirror).

## The design, and why it is this shape

**One transport, no second engine.** The push exports the row with
`SyncTaskExportDelta` (the same exporter every leg uses) and ships it to a new
peer-side handler; the pull SELECTs one row and applies it with
`SyncTaskImportDelta`. The merge authority is called verbatim on both sides, so
"newer wins", "a terminal row never regresses", "content is never overwritten"
and the divergence warning are inherited rather than restated. Nothing in
`memory_sync_row.go` decides whether a row should move — that decision has
exactly one owner and it is not this file.

**The receipt reports the ledger, not the exchange.** The peer handler applies
the patch and then reads the row's status BACK out of its own ledger; the
receipt is built from the import's own counters. A receipt that cannot
distinguish "stored" from "the peer answered politely" is the bug being fixed,
so the new one has no such word as `accepted`.

**A row leg carries ONE row, and says so.** The handler refuses a patch with any
row count other than one, by name. That is not paranoia: `--push-only
--task-id` once shipped the whole window (see
`cmd/agent/cli_tasks_sync_pushonly_rowfilter_test.go`), and a row handler that
silently took the first row of a crowd would turn that sender bug into a silent
partial transfer.

**No anchor moves.** The pull is a targeted read, not a window advance.
`harvest`'s anchor means "everything before this is identical on both sides";
one row proves nothing of the kind, so advancing it would silently skip every
row between the anchor and the id you asked for.

## `--remote-home`: the isolation channel, and what it binds

A cross-machine experiment must not write the peer operator's ledger
(Rule 34). Two things had to be bound, and they are the same directory:

```
GOPI_HOME='<dir>' PATH='<dir>/bin':$PATH gopi task sync --row-remote
```

`GOPI_HOME` decides which **ledger** the handler writes; `<dir>/bin` decides
which **build** writes it. Before the PATH half was added, the peer ran its
installed `gopi` — so a branch's new wire format would have been executed by a
binary that did not have it, and the probe would have measured the peer's
version, not this branch's. The `:$PATH` tail keeps the ordinary deployment
working: a sandbox with no `bin/` runs the peer's own `gopi`, exactly as before.

## The naming verdict

`gopi task push` was unavailable: it is the family's **git** channel
(`runTaskPushGit` pushes the task branch). Two objects — a branch and a ledger
row — must not share one word inside one family. `send` / `receive` name the
transfer honestly and leave the git verb alone. Full impact list in the report.

## Commands

```bash
gopi task send    --task-id T [--peer A | --peers [a,b]] [--db PATH] [--remote-home DIR]
gopi task receive --task-id T --peer ALIAS [--remote-db PATH] [--db PATH]
gopi task sync --row-remote          # the PEER side; invoked over ssh, not by a human
```

## Verification recipe (reusable)

```bash
SB=$(gopi sandbox create --label probe --task-id "$TASK_ID" | tail -1)
ROW=$(GOPI_HOME=$SB gopi task submit "probe row" --payload "…" | sed -n 's/.*Task queued: \([^ ]*\).*/\1/p')
GOPI_HOME=$SB GOPI_SYNC_PEERS=hp gopi task send --task-id $ROW --peer hp --remote-home <peer-sandbox>
ssh hp "GOPI_HOME=<peer-sandbox> <peer-sandbox>/bin/gopi task show $ROW"   # the ROW leg's real proof
```

The last line is the point: the push is proven by the **peer's own** reader
printing the row, never by the sender's receipt. Two ledgers, two profiles, no
operator ledger touched.
