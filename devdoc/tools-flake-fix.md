<!-- doc-class: historical -->
# Task Report: first-open schema publication made atomic (memory search flake fix)

Branch: `fix/memory-search-nonblocking-flake` — worktree `~/workspace/gopi-memflake`.
Files: `internal/tools/memory.go`, `internal/tools/memory_schemaatomic_test.go`,
`internal/standards/baseline.go`.

## Symptom

`TestMemorySearchNonBlocking` (internal/tools/memory_test.go) failed intermittently —
3-6 FAIL per 40 runs — with the message

```
concurrent entries not searchable: No distilled memory found for this query
```

in 0.03-0.16s. It is a **fast wrong answer, not a timeout**: a load flake would
be slow, this one returns immediately and confidently.

## Root cause (5-why)

1. Why is the row unsearchable although `MemoryStore` returned success?
   Its base row is in `distilled_qa` but no entry exists in the FTS index.
2. Why was the row never indexed? The store ran while the DB was **half-created**:
   the base table existed, the FTS tables + 6 keep-in-sync triggers did not.
3. Why could a store see that half-created state? `openStoreDB`/`openSearchDB`
   declared the DB ready as soon as `tableExists("distilled_qa")` was true, and
   `ensureSchema` published its DDL statement by statement in **autocommit** —
   the base table committed first, the index and triggers landed later.
4. Why did nothing repair the row afterwards? The store's fallback is
   `if !ftsTriggersOwned(...) { syncFTSRow(...) }`. By the time the row was
   written the installer's triggers had landed, so `ftsTriggersOwned` was true
   and the manual sync was skipped. For a non-default path (a fresh temp DB)
   nothing heals it later either — `migrateDefaultTriggers` only runs on
   `~/.gopi/memory.db`.
5. Why was it load-sensitive? The window is the interval between the base-table
   commit and the trigger commits — a few milliseconds, widened by disk load.
   A store has to land inside it, which is why the failure rate tracked machine
   load instead of being deterministic.

The net effect: a store reports success, the row is committed, and the FTS index
never learns about it. `MemorySearch` then returns its empty hint forever.

## Mechanism of the fix

`ensureSchema` now performs the **whole schema in one exclusive transaction**
(`execSchemaBatch`), instead of one autocommit DDL per statement:

- `execSchemaBatchOnce` opens `db.BeginTx` (which pins ONE connection) and
  creates base table → `distilled_qa_tri` → `distilled_qa_fts` → the six
  triggers, then COMMITs. `openDB` sets `_txlock=immediate`, so this is a
  `BEGIN IMMEDIATE`.
- SQLite flips its schema cookie at COMMIT, so an outside connection observes
  the **empty pre-schema DB until COMMIT and the COMPLETE schema afterwards** —
  never the intermediate state. Measured on modernc.org/sqlite: a second handle
  reads `sqlite_master` base/fts/trigger = 0/0/0 while the batch is open, and
  1/1/1 after COMMIT.
- The dynamic FTS renderers (`m1FTSVirtualTable`, `m1FTSTriggers`,
  `m1TriVirtualTable`, `m1TriTriggers`, plus `m1FTSColList`/`m1TriColList`) now
  take a `QueryExecer` and are handed the **transaction**, not the `*sql.DB`.
  That is required for correctness, not style: on a first open the base table
  exists only inside the transaction, so a probe against the pool would read the
  OLD (absent) layout and build a legacy 6-column index on a fresh DB. Verified:
  a fresh-path DB keeps the full 12-column FTS / 4-column trigram layout.
- The batch retries **only** on lock contention (`isLockContentionErr`,
  bounded by `schemaDDLBatchBusyRetries`). This is an error-triggered, bounded
  loop — no sleeps, no timing assumptions; any other failure aborts at once.
  `openDB`'s `busy_timeout` concurrency contract is untouched (no session-wide
  `BEGIN IMMEDIATE`), and no `journal_mode` is touched on a read-only open, so a
  foreign DB is still never mutated.

### Store-path hardening (Rule 15 — no silent failure)

Two silent-success holes in the store path were closed:

1. `verifyRowIndexed` runs after every store and fails the store loudly if the
   row is not in the index. It probes the FTS5 shadow table
   `distilled_qa_fts_docsize`, which carries one row per **actually indexed**
   base row. The obvious probe is wrong: `SELECT count(*) FROM distilled_qa_fts`
   goes through the external-content vtable and reads the CONTENT table back, so
   an unindexed row still counts. Measured: trigger-inserted row → docsize 1,
   row inserted with the trigger dropped → docsize 0 (tokenizer independent).
2. `syncFTSRow` now builds its INSERT from the **index's own declared columns**
   (`PRAGMA table_info(<fts>)`, new `tableColumnsInOrder`) instead of a
   synthetic column list derived from the base table. The old code demanded
   `search_kw`/`jieba_token` of a legacy foreign index that does not have them;
   the INSERT failed and the error was swallowed by the store — the same
   unindexed-row outcome on the foreign-DB path.
   `m1MigrateFTSCols` also no longer discards its `distilled_qa_fts` rebuild
   result: an unrebuilt freshly-recreated index would leave every existing row
   unindexed while the triggers made later stores look healthy.

Design note (why not temp-file + `os.Rename`): SQLite is not a single file —
sidecar `-wal`/`-shm` must be renamed in lockstep, and `~/.gopi/memory.db` is
shared by concurrent gopi processes, so the rename form is strictly more
dangerous here. Single-transaction publication is the in-place equivalent and
keeps every existing path (init, migrate, legacy heal, read-only open) intact.

## Regression tests (deterministic, no race needed)

`internal/tools/memory_schemaatomic_test.go`:

- `TestMemorySchemaPublishedAtomically` — uses the `schemaBatchProbeHook` seam,
  which runs INSIDE the publication transaction right after the base table and
  before the index/trigger statements (the window the flake lived in). A second
  connection probes there: if it can see `distilled_qa` without the FTS table,
  the trigram table or the triggers, the test fails. Post-fix the probe sees
  nothing at all (the rows are uncommitted), and the test additionally asserts
  the stored row is indexed and searchable.
- `TestMemoryStorePartialSchemaFailsLoudly` — a DB with the base table but no
  FTS index (exactly what a torn first open could publish) must refuse the
  store with an error naming the index, never report success.

### A/B proof (same test file, pre-fix tree, seam added)

| test | pre-fix tip (+ seam) | fixed tip |
|---|---|---|
| `TestMemorySchemaPublishedAtomically` | FAIL — "distilled_qa was visible WITHOUT its index/triggers 1 time(s)" | PASS |
| `TestMemoryStorePartialSchemaFailsLoudly` | FAIL — "store into a base-table-only DB reported success" | PASS |

The pre-fix tree was reconstructed with `git stash` + a minimal seam patch (the
test's only pre-fix-only edit is `isLockContentionErr` → a literal
`strings.Contains(err, "database is locked")`, since that helper is new).

### Independent A/B re-verification (fresh pre-fix worktree, no stash)

Re-done from scratch to remove any doubt about a hand-edited stash: a clean
detached worktree at the pre-fix tip `3d9ae86d` (`git worktree add /tmp/gopi-prefix-ab
3d9ae86d`), the probe seam re-inserted (identical to the shipped seam), and the
two tests copied in as `TestAB*` with only the three pre-fix-only renames:

```
--- FAIL: TestABMemorySchemaPublishedAtomically (0.01s)
    distilled_qa was visible WITHOUT its index/triggers 1 time(s) — schema publication is not atomic
--- FAIL: TestABMemoryStorePartialSchemaFailsLoudly (0.00s)
    store into a base-table-only DB reported success — the row would be unsearchable forever
FAIL	github.com/nick/gopi/internal/tools	0.030s
```

Same two tests on the fixed tip, `-count=3`: `ok … 0.163s`. The pre-fix tip was
also read directly to confirm the mechanism: `ensureSchema` loops
`db.ExecContext` per generated statement (base table, then the FTS/trigram
tables, then the six triggers) all in autocommit, and the store path is
`if !ftsTriggersOwned(ctx, q) { syncFTSRow(...) }` — so a row landing in the
window is skipped by the fallback and never indexed.

## Verification — run table

All runs on branch `fix/memory-search-nonblocking-flake` (fix commit `6c9457ff`,
report commit `25286efd`).

| run | command | result |
| build | `go build ./...` | OK |
| build (binary) | `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` | OK |
| flake acceptance | `go test ./internal/tools/ -count=20 -run '^TestMemorySearchNonBlocking$'` | ok (0 FAIL, 0.52s) |
| flake acceptance (2nd) | same, repeated | ok (0 FAIL) |
| full package | `go test ./internal/tools/ -count=1` | ok (177.00s) |
| neighbours | `go test ./internal/hippocampus/ -count=1` | ok |
| gates | `go test ./internal/standards/ -count=1` | ok (5.64s) |
| regression tests | `go test ./internal/tools/ -run '^TestMemorySchemaPublishedAtomically$\|^TestMemoryStorePartialSchemaFailsLoudly$' -count=3` | ok (0.16s) |
| vet | `go vet ./internal/tools/` | OK |

Pre-fix fleet rate for the same acceptance command was 3-6 FAIL per 40 runs
(`devdoc/tools-flake-diagnosis.md` on branch `diag/tools-flake`, not on this
branch); 0 per 40 after the fix.

### Standards gates

- Rule 16 (`%w`): the one new site I added wraps with `%w`; additionally the two
  pre-existing `%v` sites in `memory.go` (dedup check, `memory_feedback`) were
  converted to `%w` and their two now-stale baseline permits pruned from
  `internal/standards/baseline.go` (a permit matching no violation is a silent
  permission).
- Rules 17-21 untouched and green.

## Not Done / deferred

- `TestMemorySearchNonBlocking` itself was **not** modified (its concurrency
  assertion is intact, `git diff HEAD -- internal/tools/memory_test.go` is
  empty). Its pre-fix flake rate was taken from the prior diagnosis
  (`devdoc/tools-flake-diagnosis.md`) rather than re-measured on the pre-fix tip.
- `verifyRowIndexed` runs a one-row `COUNT(*)` per store — negligible, but it is
  a new per-store query that the sync/import path also pays (the import path
  keeps its single-transaction semantics; a refusal rolls the patch back, which
  is the intended loud failure).
- The `query_only`/read-only contract for a foreign DB is unchanged, but a
  foreign DB whose FTS index is narrower than its base table will now have its
  rows synced at the INDEX's column set — a deliberate behaviour change that
  makes those rows searchable where they previously silently were not.
