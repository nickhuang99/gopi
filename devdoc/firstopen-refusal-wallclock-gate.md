<!-- doc-class: roadmap -->
# The first-open refusal: a wall-clock guess used as a gate

**Task** `01a0b314` — the residual after `01a0920b` (atomic publication).
**Tip** `9ce8e159` (master at the time) / branch `fix/memory-missing-table-refusal`.
**Symptom** ~1 in 20 repetitions of `TestMemorySearchNonBlocking`:

```
concurrent store: memory_store: …/001/memory.db is not a distilled knowledge base
(missing distilled_qa table)
```

## The mechanism (deterministic, file:line)

Four concurrent `MemoryStore` calls race the FIRST OPEN of a fresh ledger. The
winner's `initKnowledgeBase` (`internal/tools/memory.go:926`) is **a sequence of
write transactions**:

1. `ensureSchema` — the atomic schema batch (`memory.go:604`, one tx),
2. `ensureFeedbackSchema` (`memory.go:694`),
3. `ensureTasksSchema` (`tasks.go`).

On a block-backed ledger every WAL commit is a device fsync. **Measured on this
host (`internal/tools/firstopen_probe_test.go`), n=121: median ~131ms, p90
~185ms, max 837ms.**

The losers' recovery was a **bounded wall-clock poll**:

```go
// internal/tools/memory.go:1139
func waitForDistilledSchema(ctx context.Context, db *sql.DB) bool {
    for i := 0; i < dbSchemaWaitTries; i++ {      // 20
        ok, err := tableExists(ctx, db, "distilled_qa")
        if err == nil && ok { return true }
        time.Sleep(dbSchemaWaitInterval)          // 25ms
    }
    return false
}
```

`20 × 25ms = 500ms` — and the build legitimately exceeds that. When it does,
the loser falls through to

- `memory.go:1271` (`memory_search`), `memory.go:1324` (`memory_store`),
  `tasks.go:1138` (`gopi task`) → the refusal message.

So the refusal was not about the file being foreign; it was about the poll's
budget having been spent. **A wall-clock guess about another process's progress
was being used as a gate.** The 500ms constant was the whole defect: no value
of it is correct, because it does not bound the thing it is used to bound.

Rule 27 discipline: the only instrument used is **repetition**
(`go test -count=N`); no load was manufactured.

## The fix: the fact replaces the guess

The key observation that makes a *fact* available: `ensureSchema` publishes the
whole schema in **one transaction**, so a concurrent initializer is **never
observable as a half-built schema** — it is observable as **nothing at all**
(SQLite flips the schema cookie at COMMIT). Therefore:

> A file with **zero schema objects** (`sqlite_master` = 0 rows) is one whose
> owner has not published yet — nobody's data is in it. That is exactly the
> shape a concurrent first open is observed in, and the opener may **join** the
> initialization instead of refusing.

`hasNoSchemaObjects` (`internal/tools/memory.go:1169`) reads that fact from the
file; `adoptUninitializedKnowledgeBase` (`memory.go:1207`) joins the
initialization under the **existing** per-path lock (`initKnowledgeBaseOnce` →
`ensureKnowledgeBase`). All three openers call it **before** the poll:
`openSearchDB` (`memory.go:1257`), `openStoreDB` (`memory.go:1310`),
`openTasksDB` (`tasks.go:1123`). The poll survives only for shapes this probe
cannot recognize (a file with objects but no `distilled_qa`).

Where a loser's own build runs, SQLite serializes the two on the file and every
statement is `IF NOT EXISTS` — the bound is the build's own, not a timeout.

### What did NOT change: the foreign-DB contract

The refusal must stay for a database with content ("never mutate foreign DBs" —
hermes' `distilled_knowledge.db`, `state.db`). `hasNoSchemaObjects` is the
discriminator, and it is strict: a **used** file can never read zero objects, and
a probe error (garbage bytes: `file is not a database`) reports **false**, so it
refuses too. Asserted byte-for-byte-unchanged in `TestMemorySearchForeignFileRejected`
and in the negative half of `TestEmptyFileStoreIsInitializedNotRefused`.

### The one contract change, stated plainly

An **empty** file at a **custom** path (`GOPI_MEMORY_DB`) used to be refused; it
is now adopted and initialized. That is the intended change: zero schema objects
means there is no state to protect. It surfaced in `TestMemorySearchDefaultPathHeals`,
whose "custom path" half wrote an *empty* file and expected the refusal — that
half now seeds a table instead, so it tests the stricter boundary (content, not
path) rather than the accident of an empty file.

## Regression guard

`internal/tools/firstopen_refusal_regression_test.go`

- `TestEmptyFileStoreIsInitializedNotRefused` — the minimal reproducer: an empty
  custom-path file must be initialized, not refused (+ the negative probe).
- `TestConcurrentFirstOpenWaiterIsNotRefused` — drives the exact window: a
  publisher is parked inside its schema transaction (the `schemaBatchProbeHook`
  seam) while a second opener arrives. The publisher is released only **after**
  the opener's observation window closes, so the opener's verdict was provably
  reached on an EMPTY file — established by the test's own ordering, not by a
  timing guess. The window is a **deadline**, not a wait: pre-fix the refusal
  arrives inside it (red), post-fix the opener is still **waiting on the peer**
  (green) — waiting on the peer is itself the discriminator, because the
  refusal path returns instantly.

Both tests compile and FAIL on the pre-fix production code with the exact
signature; both PASS after.

## Evidence

See `tasks/01a0b314-report.md` (`## Evidence (self-filed)`).
