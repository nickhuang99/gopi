<!-- doc-class: roadmap -->
# The first-open guard asserted the host, not the code

**Task** `01a0b3b9`. **Branch** `fix/firstopen-property-not-wallclock`.
**File reworked** `internal/tools/memory_db_init_regression_test.go`
(`TestFreshDBInitIsFast` / `TestManyFreshDBsNoHang`).

## 1. The defect

`TestFreshDBInitIsFast` opened a fresh ledger and failed if the call took longer
than `2*time.Second`:

```go
if elapsed := time.Since(start); elapsed > 2*time.Second {
    t.Fatalf("fresh DB init took %v — per-DDL fsync regression (rollback journal still active?)", elapsed)
}
```

The message names a mechanism (a durable sync per DDL statement) but the
condition measures the **host**. The t-1788361069 regression it was written for is
a statement about the **pragmas in force** at first open — the rollback journal
instead of WAL, `synchronous=FULL` instead of `NORMAL` — and those two
mechanisms have the *same symptom* (a fresh open is slow). No wall-clock
threshold can separate them, so the old assertion turned "this disk is busy" into
"the code regressed" — the misdiagnosis Rule 15 forbids.

## 2. The measurement (2026-09-18, this host, repetition only — Rule 27: no load was manufactured)

**a. The old guard is a coin flip on the pristine tip.** 12 repetitions of
`go test -count=1 -run '^TestFreshDBInitIsFast$' ./internal/tools/` on the
unmodified tree: **2 red** (`3.207s`, `2.931s`), 10 green. Nothing in the branch
touched the production path at that point.

**b. The budget measures the device, because the device is the suite's scratch
root.** 20 fresh in-process `openTasksDB` calls wall times (ms):

```
627.25 710.279 908 884.419 718.195 724.434 946.713 1992.954 1266.507 928.966
1442.499 625.371 4010.179 2941.905 3472.385 2492.898 1292.906 3203.253 1763.507 1914.964
min 625.4  p50 1292.9  p90 3472.4  max 4010.2   (n=20)
```

`t.TempDir()` under the suite's TestMain lands on
`~/workspace/gopi-test-scratch/suite-tools-run-*/…`, and
`internal/scratch.MemoryFilesystem` measures it as **`block-backed filesystem
type 0xef53` (ext4, NOT memory-backed)** — the pin `devdoc/scratch-shm-role-split.md`
§7 records as deliberate. `firstOpenSyncMode` therefore chooses
`ledgerSyncMode` (`NORMAL`) and the build legitimately pays a real device sync.
The 2s budget sits *inside* the distribution, which is the whole finding.

**c. Where the time goes.** One fresh ledger, phase by phase:

```
phase enableWALWithSync(NORMAL)     564.4ms
phase ensureSchema                  292.2ms
phase ensureFeedbackSchema            3.5ms
phase ensureTasksSchema               5.2ms
phase close                         442.3ms
```

Two things this run established:

- **Repetition, not batching, is the cost driver** — and `PRAGMA journal_mode` on the built ledger already reports `wal`, i.e. the state the guard was *trying* to protect. The guard's premise was intact; only its instrument was wrong.
- **`synchronous` must be read on the BUILD's own connection.** The same probe read `PRAGMA synchronous` = 2 (FULL) back after reopening the ledger — not because the build regressed, but because a fresh connection inherits SQLite's default. `synchronous` is per-connection: no later handle can report the build's level. That is why the guard replays the production build sequence itself (`firstOpenBuildLedger`) instead of re-opening the file, and why `TestFirstOpenKeepsProductionDurabilityOffTheWorkingPath` exists to keep the *leak* direction covered.

## 3. What the guard asserts now

Both arms are measured **on the real production path**, and both are facts about
the code:

| arm | measurement | why it is the regression's signature |
|---|---|---|
| journal mode | `PRAGMA journal_mode` read off the ledger `openTasksDB` produced | `delete` = rollback journal = every one of the 40+ schema DDLs commits on its own |
| level | `PRAGMA synchronous` read on the **build's own connection** | the level must be the one the chosen mode NAMES (see below) |

**The levels, precisely — this is where the first draft of the guard was wrong.**
`PRAGMA synchronous` reports 0=OFF, 1=NORMAL, 2=FULL, 3=EXTRA.

- **OFF** is what `firstOpenSyncMode` hands a build whose ledger provably lives on
  a **memory filesystem** (measured by device id, never a path spelling): there is
  no device to be durable to, so the dropped sync protected nothing.
- **NORMAL is the durable device ledger's level, not a weakened one.** Under WAL,
  NORMAL syncs the WAL at COMMIT — at the commit boundary. What it removes is the
  rollback journal's extra sync. It is the level `enableWAL`/`ledgerSyncMode`
  apply on purpose (`ledger_dsn.go:80`), and this host's measurement confirms it:
  `journal_mode=wal, synchronous=1` is the state a fresh open comes out in.
- **FULL is the duplicate the fix removes under WAL** (`ledger_dsn.go:77-79`:
  "under the default FULL every COMMIT fsyncs"). WAL already syncs the WAL at
  commit, so FULL only adds a further per-commit sync of the WAL
  header/directory — the extra syscall behind the ~155ms-per-DDL signature.

So the level arm compares **equality with the level the mode names**
(`ledgerSyncModeLevel`), not a floor ≥ FULL. A floor would have *inverted* the
guard: production's first-open level is NORMAL (measured `synchronous=1`), so a
`>= FULL` rule would have failed the healthy path — and it would have passed a
re-inlined `synchronous=FULL`, the very duplicate this fix removes. Equality
catches both directions and the `delete` + `FULL` pre-fix state reds on both arms.

`synchronous` is **per-connection** in SQLite: no later handle can report it, and
the public entry point closes its handle before returning. So the guard replays
the production build sequence (`enableWALWithSync` + the three schema phases)
itself — `firstOpenBuildLedger` — and reads the level there. The complementary
claim (the relaxed first-open mode must not leak onto later handles) was already
pinned by `TestFirstOpenKeepsProductionDurabilityOffTheWorkingPath`.

The verdict is a **pure function** of the measured facts
(`ledgerPragmaGuardError`, `firstopen_pragma_guard.go`), so it is unit-testable
and a slow device cannot reach it: identical facts give an identical answer on a
quiet and on a loaded host, and `TestFirstOpenGuardDoesNotMeasureTheHost` asserts
exactly that (it logs a real open's duration as evidence without asserting on it:
measured 0.85s, block-backed root, `journal_mode=wal`).

The journal mode is asserted *beside* the level because a handle left at SQLite's
default reads FULL accidentally — level alone can be right for the wrong reason,
and `delete` alone restores one durable sync per DDL commit whatever the level.

## 4. The red proof (the part that is normally missing)

An assertion over pragmas could easily be a tautology: `enableWALWithSync` asks
for `WAL` in the same statement the guard reads. So the guard ships a **seam** —
`ledgerPragmaDDLRenderer` (`firstopen_pragma_guard.go`), nil in production —
that replaces the pragma statement sequence, and
`TestFirstOpenGuardIsRedOnTheRegression` runs the A/B:

1. production path as shipped → **green**;
2. seam armed with the pre-fix state (`journal_mode=DELETE`,
   `synchronous=FULL`, both from named constants) → **red**, and the message
   names `delete`;
3. seam removed → **green** again.

Without step 2 the guard would only be proven to agree with itself. The seam is
the same shape as the package's other test seams (`schemaBatchProbeHook`,
`syncSSHExchangeHook`) and costs one nil check per pragma statement.

`TestFirstOpenGuardSeamRendersProductionStmts` closes the other direction: with
the seam unarmed, `ledgerPragmaStmts` must render exactly the pair production has
always issued, so the seam cannot be where the production behaviour actually
lives (which would make step 2 vacuous).

## 5. Honest limits

- **`TestManyFreshDBsNoHang` is no longer a "promptness" test either.** It asserts
  the pragma state per ledger. Its 40-iteration shape (and the suite-order hang it
  reproduces) is unchanged; only the assertion moved from the stopwatch to the
  property. A genuinely slow device makes it slow, never red.
- **The absolute-budget option was refused, not overlooked.** A named-constant
  budget with a "host slow vs fsync regressed" failure message was considered and
  rejected: the two causes are indistinguishable from a duration, so the message
  would have to guess. The property is the only assertion that is actually about
  the regression.
- **The guard's `journal_mode` arm cannot see a per-statement regression that
  keeps the mode.** If a future change batched WAL+synchronous correctly but
  reintroduced a durable sync some OTHER way, this guard would not catch it —
  `TestFirstOpenBlockDeviceCostIsTheSync` (`firstopen_sync_test.go`, a
  noise-tolerant paired A/B on a device scratch dir) is the instrument for the
  cost itself, and it skips rather than asserting when the host cannot show a
  signal.
- **A PRE-EXISTING red was found in the same area and NOT "fixed" here:**
  `TestFirstOpenSyncModeDecisionTable/memory-filesystem-ledger` is red on the
  pristine base commit too — `t.TempDir()` under the suite's TestMain resolves to
  the block-backed `~/workspace/gopi-test-scratch`, so the case that expects a
  memory-backed scratch root no longer holds. Reproduced on an untouched
  worktree at the same master tip. It is a fixture-assumption defect of that
  test, not a production defect, and it is out of this task's scope: fixing it
  means changing what the suite's scratch pin promises, which is a system-layer
  decision. Flagged here and left red exactly as found (no test was weakened or
  skipped to hide it). `TestFirstOpenKeepsProductionDurabilityOffTheWorkingPath`
  skips for the same measured reason, which the base does too.
- **`internal/stylegate` is red on the pristine base as well** (8 `%v wraps
  error` sites + a `t.Skipf` reason-literal site); this branch's hit set is
  byte-identical to the base's (162 hits, `diff` empty), so the branch neither
  adds nor removes a gate hit. `internal/standards` is green.
- **Nothing about the device cost was mitigated here.** The suite's scratch root
  is block-backed by the operator's decision (`devdoc/scratch-shm-role-split.md`
  §7); this task only stopped the suite from misreporting that cost as a code
  defect.
