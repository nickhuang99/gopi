<!-- doc-class: historical -->
# The prefetch rigs' DSN contract, the gate that was blind to them, and the warm-handle fallback decision

Task `01a12c0f-9874-7278-a0f7-9871f2d769f9` · worktree `~/workspace/gopi-01a12c0f98747278a0f79871f2d769f9`
branch `task/01a12c0f98747278a0f79871f2d769f9` · 2026-10-12 · commit `fddb5fd34`

Three findings, each of which is a reusable lesson rather than a diff summary.
The one-line-takeaway version is in memory record **#9137**.

---

## 1. Why eight bare DSNs escaped the contract gate — and why the gate's own doc had already predicted it

**Symptom.** `TestPrefetchTopNShapeKeepsProductionRanking` failed under
`make test-budget PKGS='./cmd/agent'` with
`seed fixture (CREATE TABLE distilled_qa (…)): database is locked (5) (SQLITE_BUSY)`,
and passed in isolation. The bug row named one rig site.

**Measurement.** Eight sites in `cmd/agent/chat_prefetch*_test.go`, all the same
shape — the driver arg was a *const*, the DSN was *bare*:

```go
db, err := sql.Open(prefetchDriverName, path+"?_txlock=immediate")  // no busy_timeout, no WAL
```

**Why the gate did not catch them.** `TestLedgerOpenersUseProductionDSNContract`
AST-scans for `sql.Open` whose driver arg names sqlite, then inspects the DSN. Its
predicate was:

```go
lit, ok := arg.(*ast.BasicLit)          // STRING LITERAL ONLY
return ok && lit.Kind == token.STRING && strings.Trim(lit.Value, `"`) == ledgerSQLiteDriverName
```

`prefetchDriverName` is an `*ast.Ident`, so `ok` was false and **the DSN arm was
never reached**. The gate did not miss these sites by accident; it never considered
them its business.

**The part worth remembering: the blindness was already written down, in the file
the gate guards.** `internal/tools/ledger_dsn.go` carries a warning whose middle
clause predicts this exact failure, and names the mechanism:

> Replacing it with a package const (ledgerDriverName) turned the contract gate in
> cmd/agent BLIND — it would report every PRODUCTION site (which spells the driver
> as a literal) and MISS exactly the rig sites the rule exists for. … a
> re-introduced bare DSN at such a site passes unnoticed.

The const was introduced anyway when the prefetch family was written. **Prose in a
comment did not survive a later author's convenience** — which is the general
lesson: a warning that names a future defect is worth nothing until something
checks it. The repair therefore did not just convert eight sites; it made the
predicate reach where they are:

- recognise the driver const **by name** (`ledgerSqliteDriverConstName`) so the site
  becomes the gate's business at all;
- read that const's **value live in the same package** —
  `if prefetchDriverName != ledgerSQLiteDriverName { t.Fatalf(…) }` — because a
  name-only widening is a gate any future edit can switch off by pointing the const
  at another driver. A gate that widens its predicate on a name must verify the
  name's value, or it has traded one blind spot for a weaker one.

**Honest remaining scope**: a *third* spelling (a second const, a helper returning
the driver name, a variable) is still invisible. The gate's true scope is "a string
literal, or this one const".

---

## 2. WAL makes lock-holder rigs vacuous — and the seed's fix exposed it

The established recipe (memory #3473) is "one exported DSN helper carrying a busy
timeout, referenced everywhere". That helper carries **more** than a timeout:
`journal_mode(WAL)` is part of production's contract. So converting the seed
silently changed every fixture's journal mode — and two rigs went red:

```
--- FAIL: TestPrefetchBusyIsRetriedWithinTheSameBudget
    session shape: outcome = "hits", want "gave-up" (err=<nil>)
--- FAIL: TestPrefetchBusyGiveUpIsReportedAsBusy
    try 0: outcome = "hits", want "gave-up" (err=<nil>)
```

Not a regression in the product: **a regression in the rig's ability to observe
its own subject.** Those rigs hold `BEGIN EXCLUSIVE` and assert a read-only lookup
is REFUSED with SQLITE_BUSY. Under WAL a read-only reader never contends with a
writer — it reads a consistent snapshot — so the lookup returned `hits` in ~0ms and
the assertion could not be true.

Measured, both arms, in one test
(`internal/tools/ledger_dsn_test.go` — `TestLedgerRollbackJournalPragmaIsWhatMakesAHolderRefuse`):

| holder's mode | `BEGIN EXCLUSIVE` held | read-only `SELECT` |
|---|---|---|
| WAL (production's) | yes | **rows returned**, `err=nil` |
| DELETE (rollback journal) | yes | `database is locked (5) (SQLITE_BUSY)`, after the DSN's own busy timeout |

**The repair channel matters as much as the repair.** The rigs get the rollback
journal back through `tools.LedgerRollbackJournalPragma()` — a **HANDLE-level
pragma**, never a second DSN spelling. Two reasons, and both are structural:

1. the contract gate recognises *the DSN helpers that carry production's contract*;
   a rig inventing its own DSN would be a second, unreviewed answer to "what does a
   ledger opener look like" — and, as finding 1 shows, such a spelling is also what
   the gate is worst at seeing;
2. it is the channel production itself already uses for its own deviation
   (`ledgerNoWaitPragma`, whose comment records the same reasoning).

**Order is load-bearing, and this is the trap.** `PRAGMA journal_mode = DELETE`
requires EXCLUSIVE access to the file and is REFUSED with SQLITE_BUSY while *any*
other connection is open — including the fixture's own seed handle. Measured three
times: two rigs failed with `rollback journal on the holder: database is locked (5)
(SQLITE_BUSY)` when they flipped *after* warming their reader, and a probe of mine
failed the same way with its seed handle still open. So the rule for such a rig is:

> flip the journal **before** any reader attaches, and close the seed handle first.

---

## 3. `(14)` is SQLITE_CANTOPEN, not ENOMEM — and a Rule 15 fallback can be the *right* answer

**The bug row read the message as memory exhaustion.** The message was:

```
warm read-only ledger: unable to open database file: out of memory (14)
```

Measured: `(14)` is **SQLITE_CANTOPEN** (`const SQLITE_CANTOPEN = 14`,
`modernc.org/sqlite/lib/sqlite_linux_amd64.go:1606`; also 14 at `v1.34.1:1587`). The
driver renders that code in a sentence that *reads* as an allocation failure. The
measured trigger is a **path** defect: a `mode=ro` open of a path that does not
exist. This repo already recorded the trap —
`cmd/agent/cli_tasks_worker_unknown_arg_test.go:59`:

> a read-only open of a path that does not exist fails inside the driver with
> `unable to open database file: out of memory (14)`, a message that names neither
> the missing file nor the probe.

**Lesson**: a SQLite `(14)` on a ledger open is a PATH question first. Reading it as
memory pressure sends the next agent hunting for a resource problem that is not
there.

### The decision, recorded where the code lives

The warm handle is an optimisation; the prefetch is the feature. Refusing to
retrieve anything because one dial failed would delete the feature for a whole
session — a failure that HIDES a working path, which is worse than the fallback. So
the decision is **deliberate fallback, disclosed**, and Rule 15 is satisfied by
disclosure with **two required halves** (both now pinned by a test):

1. stderr names the **reason** (named const) and the **ledger path** — a reader can
   tell which ledger failed without re-running anything;
2. the **consequence** reaches the record: lookups report `handle: "fresh"`, the
   field that says the dial was NOT amortised. The trace of this fallback in a
   session's record is `grep '"handle":"fresh"'`.

The general shape, worth reusing: **when a fallback is correct, Rule 15 is met by
naming the path and the reason AND writing the consequence into the machine-readable
output — never by a comment, and never by a refusal that is louder than the defect.**

Guard: `TestPrefetchWarmHandleFailureIsADisclosedFallbackNotASilentOne`
(`cmd/agent/chat_prefetch_test.go`). Its failure is produced by a STATE (an
unopenable ledger path), not by manufactured load (Rule 27).

---

## What is NOT established by this work (so the next reader does not over-read it)

- **The original failure was not reproduced, and "N=3 green" would overstate what
  was measured.** What is measured (three full-package budget runs from this
  worktree, at head `fddb5fd34` — `n3/postfix-runs-summary.txt` in the session's
  artifacts, each run's tree confirmed by the path inside its own `tested-bin`
  binary): `TestPrefetchTopNShapeKeepsProductionRanking` PASSED in all three, and
  the new warm-handle guard PASSED in all three. The PACKAGE was not green in any
  of them: 10-11 failures per run, all but one in the help-surface / `TestChat-*`
  family that row `01a12c4b-dc88` was opened for (a master-baseline condition,
  `in_progress` at the time of writing). The pre-fix rate could not be measured
  here because the pre-fix attempt ran from a `git archive` copy, which is not a
  git repo, so ~10 unrelated tests failed for environmental reasons (the bug row
  itself records `TestChatCookedFallback_NonTTY` failing on "not a git work
  tree"). Those results were discarded rather than reported as a pre-fix rate.
  **The honest claim is therefore narrow: the subject test passed 3/3 after the
  fix; the run that ORIGINALLY failed was not replayed, so this is not a before/after
  rate.**
- **The cause of the original simultaneous pair is not pinned.** Both events landed
  1.8 ms apart (`.635242871` → `.636896069`). Two readings survive: a transient
  scratch-root serving failure, or a path-shaped failure. Neither is measured; both
  are inferences.
- **The `(14)` message is not translated on the product surface.** The caller's
  stderr line now names the path, but the driver's own sentence still says "out of
  memory (14)". Whether `prefetchOpenHandle` should translate SQLITE_CANTOPEN into
  "the ledger path could not be opened" is a product decision this task did not take.
