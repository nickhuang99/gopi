<!-- doc-class: historical -->
# gopi's own sentence for a driver SQLITE_CANTOPEN at the warm-opener seam

Task: `01a12c60-9d2b-7e29-b0d7-e3c1c938f52d`, branch `task/01a12c609d2b7e29b0d7e3c1c938f52d`.

## The defect

The typing-time memory prefetch opens the operator's ledger read-only
(`tools.LedgerReadOnlyDSNIn`: `mode=ro` + `query_only(true)`, `cmd/agent/chat_prefetch.go`
`prefetchOpenHandle`). A read-only open cannot CREATE the file, so when the ledger —
or a directory in its path — does not exist, the driver answers with its own
sentence:

```
unable to open database file: out of memory (14)
```

Three things are wrong with that sentence as a diagnosis:

1. `(14)` is **SQLITE_CANTOPEN**, not an allocation failure. Measured on this
   machine: the failing error's dynamic type is `*sqlite.Error`, its `Code()` is
   `14`, its text is the sentence above, and the trigger was a `mode=ro` open of a
   path whose parent directory does not exist. `modernc.org/sqlite/lib/` carries
   `const SQLITE_CANTOPEN = 14`. The wording sends a reader after memory pressure
   while the actual defect is a PATH.
2. It names **neither the ledger** nor the probe that failed — an operator
   reading `gopi`'s stderr line cannot tell WHICH file was missing.
3. The miss is not hypothetical: `cmd/agent/cli_tasks_worker_unknown_arg_test.go`
   already documented it from the other side (a fixture that merely *named* a
   ledger path sat red in every subtest at 0.01s with exactly this string, "a
   message that names neither the missing file nor the probe").

## The decision — translate, at ONE seam, with named constants

`cmd/agent/chat_prefetch_ledger_open.go` (new) holds the whole translation:

- `prefetchLedgerCantOpenCode = 14` — named (Rule 1a). **Deliberately not
  imported** from `modernc.org/sqlite/lib`: that package's constants are generated
  per architecture, and this file's job is to recognise a CODE the driver reports
  as a plain `int`. The number is stated once, with its source named in the comment.
- `prefetchLedgerCantOpenCause` — gopi's statement of what a CANTOPEN on a ledger
  open means. Named (Rule 1b) because it is the sentence an operator reads and a
  test compares. It names the path-shaped trigger (`mode=ro` does not create the
  file, so the file or a directory in its path is missing or unreachable) and
  explicitly dispatches the misleading reading (`NOT an allocation failure`).
- `prefetchLedgerOpenCause(err, path)` — the classifier. `nil` → `nil`. Anything
  that is not a code-14 driver error is returned **unchanged, by identity**. Only
  code 14 is re-wrapped:

```go
fmt.Errorf("cannot open the memory ledger %s (SQLite code %d): %s: %w",
    path, prefetchLedgerCantOpenCode, prefetchLedgerCantOpenCause, err)
```

The call site is the ONE warm-opener seam, `prefetchOpenHandle`
(`cmd/agent/chat_prefetch.go`), on the statement that performs the lazy dial:

```go
if err := db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&h.journalMode); err != nil {
    db.Close() //stylegate:ignore a handle that failed to warm is closed here and never reused
    return nil, fmt.Errorf("warm read-only ledger: %w", prefetchLedgerOpenCause(err, path))
}
```

### Why not a new error type

No caller branches on the kind. `(*chatPrefetch).handle` discloses the error and
falls back; `prefetchLookupDetailed` records its own segment attribution. Both act
on the error as a whole, so an error-type hierarchy here would be machinery with no
consumer; what the operator was missing is a sentence. `errors.As` still reaches the
driver's own error through the `%w` chain — a caller that wants the driver's text
finds it.

### Why the translation rides on the seam rather than replacing the fallback

The disclosed fallback stays exactly as it was (Rule 15): the warm handle is an
OPTIMISATION, the prefetch is the FEATURE, so a failed warm degrades to a dial per
lookup, `handle="fresh"` lands in the record, and the reason plus the path go to
stderr. The translation adds the diagnosis to the `err` that disclosure already
prints — it does not refuse, and it does not hide the working path. Refusing here
would be a failure that deletes retrieval for the whole session because one dial
failed once.

### Scope: which seams are NOT in this change

The live run measured the same driver sentence at three other places in one chat
turn (`self-record: indexed 0 of 1 note(s)`, `chat table: register failed
(continuing)`). Those are different seams with their own callers and their own
attribution vocabulary; translating them is a separate, dispatchable task (Rule 26),
not something to sweep into a `prefetchOpenHandle` change (Rule 4).

## Verification

See the task report for the `Ran:` line and the red→green observation. Short form:

- `go build ./...` green; `go vet ./cmd/agent/` green.
- The new tests fail against the pre-fix call site with the raw driver sentence and
  pass with the translation, in the same tree (`red→green` observed verbatim).
- A live `gopi chat --once` run against a ledger whose parent directory does not
  exist — inside a standard sandbox (`gopi tool sandbox run`, Rule 34) — prints
  gopi's sentence on the disclosed-fallback stderr line, naming the ledger path, the
  code and the cause, with the driver's own text still appended.
