<!-- doc-class: historical -->
# The ledger DSN contract and the in-memory class — a predicate that was too broad by ONE class

**Branch**: `task/01a0f4b2d1b973f29257bec7700d8d93`.
**Subject**: `TestLedgerOpenersUseProductionDSNContract` was RED on clean master with
exactly one hit — `internal/tools/memory_split_invariant_test.go:52`,
`sql.Open("sqlite", ":memory:")`.

## The verdict: the PREDICATE was wrong, not the caller

The alternative — "the caller must adopt the contract" — cannot be carried out,
and the reason is a measurement rather than a preference. The contract has three
parts (`internal/tools/ledger_dsn.go`): a busy timeout, WAL journal mode, and
(single-source) `synchronous(NORMAL)`. **Two of the three are REFUSED by the
engine on an in-memory database**, so no DSN spelling can carry them:

```
$ sqlite3 ':memory:' "PRAGMA journal_mode=WAL;"   → memory    # refused
$ sqlite3 /tmp/dsnprobe.db "PRAGMA journal_mode=WAL;" → wal   # accepted
```

The same answer came out of the Go driver in the test added alongside this
change (`TestLedgerMemoryDSNOpensAPrivateDatabaseThatRefusesWAL`,
`internal/tools/ledger_dsn_test.go`), which additionally shows a second handle
cannot see the first handle's table — the privacy that makes an in-memory opener
**unable to race the resident daemon** the contract exists for.

So the intended subject of the gate — a ledger FILE opened without the contract,
which fails `database is locked (5) (SQLITE_BUSY)` instead of queueing — was
never at risk at that site. The site asks a bare `unicode61` fts5 tokenizer a
question on a throwaway table; it opens no ledger and resolves no path. It was a
false positive, and the documented reason for the false positive is now in the
gate's own header.

## Why BOTH halves were changed

The epic allowed either half. Doing only one leaves a real defect:

1. **Only widening the gate** (recognise `:memory:` by value) gives the exemption
   a *content* predicate: it cannot tell a deliberate in-memory fixture from a
   ledger opener handed that string by a variable, and every future in-memory
   fixture re-types the DSN — the drift Rule 14 exists to prevent.
2. **Only changing the call site** leaves the class unnamed. The next in-memory
   opener reddens the gate again (Rule 26's duplicate-dispatch shape: the same
   repair rediscovered), because there is nothing to route through.

The remedy is therefore:

- **the value gets ONE definition** — `internal/tools.LedgerMemoryDSN()`, backed
  by `const ledgerMemoryDSN = ":memory:"` (`ledger_dsn.go`), so a DSN value is
  never re-typed at a call site (Rule 2/13/14); and
- **the gate recognises the helper** — `ledgerMemoryDSNHelperName` joins
  `LedgerDSN`/`LedgerReadOnlyDSN` in `ledgerIsDSNHelperName`, so the predicate
  stays one question: *is the DSN argument built by a ledger DSN helper?*

The literal is deliberately **still flagged**, and the synthetic half pins it
(`the bare in-memory literal is STILL flagged — the exemption is the helper, not
the value`). That case is what makes this a class exemption rather than a
weakened predicate.

## Red → green, reproduced (not asserted)

```
### RED ARM — call site reverted to the bare literal ###
internal/tools/memory_split_invariant_test.go:59: db, err := sql.Open("sqlite", ":memory:")
--- FAIL: TestLedgerOpenersUseProductionDSNContract (1.27s)
    ledger_dsn_contract_test.go:247: ../../internal/tools/memory_split_invariant_test.go:59
    opens a ledger with a bare sqlite DSN — no busy timeout, no WAL: ...
### GREEN ARM — restored from a file backup (not git checkout) ###
internal/tools/memory_split_invariant_test.go:59: db, err := sql.Open("sqlite", LedgerMemoryDSN())
ok  	github.com/nick/gopi/cmd/agent	3.179s
$ git status --porcelain   → (empty)
```

The green arm restored from `cp` of a pre-edit backup, so the transition does not
depend on a commit boundary.

## Rejected alternative (recorded so it is not re-proposed)

Opening the fts5 probe on `os.DevNull` (the "no filesystem object either way"
shape) was considered and dropped: `os.DevNull` is a **file** as far as SQLite is
concerned, so the contract's WAL clause becomes *expressible and silently not
applied* — a worse lie than the exemption — and the rig would need a real path
under the scratch root, which is exactly the apparatus the site does not need.

## Rule 35 check (a new exported helper, nothing persistent)

`LedgerMemoryDSN` creates no persistent dependency: no new config key, no file,
no daemon, no script, no env var, nothing that outlives the process. It is one
pure function over one constant in an existing file. The four questions do not
apply to it — there is no artifact to own or reclaim.

## Not Done (honest)

- **The gate's third root is still `internal/ledgerwindow`-adjacent only by
  omission.** `internal/ledgerwindow/window_test.go:98` carries the SAME value in
  its own `fixtureMemoryDSN = ":memory:"`, and that tree is NOT in the gate's
  `ledgerScanRoots`, so it is neither a hit today nor covered tomorrow. It is
  correct there (its package carries its own fixture-naming convention and the
  constant is already the single definition for that package). Whether the gate
  should add that root — i.e. whether two packages may each own an in-memory
  constant, or the value should move to a leaf constants package per Rule 14 —
  is a genuine open question and is left to a human rather than decided by a
  silent edit here.
- The gate's read floor still counts files, not opener sites; a tree whose
  openers all vanish would be caught, a tree whose openers are all rewritten to a
  *variable* driver argument would not (the same partial-coverage boundary the
  gate's header already states for `ledgerIsSQLiteDriverArg`).
