<!-- doc-class: historical -->
# memsync mechanism 2 — the asset import no longer write-locks the peer ledger for the whole corpus

**Date:** 2026-09-26 · **Epic:** memsync failure family, mechanism 2
**Code:** `internal/tools/memory_sync_assets.go` (`MemoryAssetsImportPatch`),
`cmd/agent/cli_memory_sync.go`, `internal/tools/memory_sync_assets_batch_test.go`

## The defect

`MemoryAssetsImportPatch` applied an entire peer export inside ONE
`db.BeginTx` (BEGIN IMMEDIATE, via `_txlock=immediate`). A
`gopi memory sync --peer <peer>` leg therefore **write-locked the PEER's
`~/.gopi/memory.db` for the whole apply**, and every concurrent gopi writer on
that peer queued for `dbBusyTimeoutMSec` (15 s) and then died with
`SQLITE_BUSY`.

Measured on the real pair (2026-09-26, peerbox → hp, probe on hp with production's
15 s busy timeout, 51-batch/1621-row apply, ~90 s session):

```
   t=+19.4s BUSY after 15.06s: database is locked
   t=+34.5s BUSY after 15.07s: database is locked
   t=+49.6s BUSY after 15.02s: database is locked
   t=+64.7s BUSY after 15.02s: database is locked
   t=+79.8s BUSY after 15.02s: database is locked
   probe done: attempts=1645 busy=8 max_wait=15.071s
```

Five consecutive 15 s refusals = the peer's ledger was continuously
unavailable for ~80 s.

## The fix — and the part that did NOT work on its own

1. **Bounded transactions.** The patch is applied in transactions of at most
   `memoryAssetImportBatchRows = 32` rows (`stats.Batches`,
   `stats.RowsCommitted`, `stats.BatchesFailed` report the tally, merged only
   after a successful COMMIT).
2. **An explicit idle interval — `memoryAssetImportBatchPause = 200ms` — between
   batches.** This is the finding worth reading: **batching alone did not free
   the peer ledger.** Measured with the batched binary on the peer
   (`probe_D.txt`): 51 back-to-back batches leave microsecond gaps, the lock's
   duty cycle stays ~100 %, and the probe still recorded 2 × `BUSY after
   15.02s`. The pause is *outside* the transaction (after the commit, before the
   next BEGIN), so it separates two lock windows instead of widening one, and it
   is not paid after the last batch.

```
before (one transaction)   busy=8   max_wait=15.071s      ← the defect
batched, no pause          busy=2   max_wait=15.018s      ← NOT ENOUGH
batched + 200ms pause      busy=0   max_wait=0.930s       ← accepted
```

## Measurement table (real peer, rule 31)

Probe: a real second process on hp running `BEGIN IMMEDIATE` … `ROLLBACK` every
50 ms against `~/.gopi/memory.db` with **production's 15 s busy timeout** (the
queue-then-fail behaviour a concurrent gopi writer gets). "peer apply by" names
the binary hp actually executed for `--serve-assets`.

| arm | peer-side binary | session | probe attempts | **BUSY** | max_wait |
|---|---|---|---|---|---|
| A (baseline) | pre-change (`7c7fd53a`, 86 MB) | 59.0 s | 1203 | **9** | 15.067 s |
| B (baseline, clean) | pre-change (`7c7fd53a`) | 89.3 s | 1645 | **8** | 15.071 s |
| C | pre-change (`7c7fd53a`) | 44.6 s | 2541 | **4** | 15.020 s |
| D | batched, **no pause** (`b52eb0c8`) | 43.8 s | 2428 | **2** | 15.018 s |
| E | batched **+ 200 ms pause** (`f690d0d0`) | 92.1 s | 3139 | **0** | 0.730 s |
| acceptance | final batched **+ pause** (`8d0571e9`) | 99.9 s | 1989 | **0** | 0.930 s |

Per-transaction hold = `batch rows × per-row cost`. The client's own apply is
the honest per-row unit for the peer's own ledger (same rows, same store path,
`_txlock=immediate`), and it reports its batches directly:
`rows=1619/1619 batches=51` for a 42.4 s apply → **≈0.83 s per 32-row
transaction** — versus the previous **whole-corpus hold (≈42–90 s measured,
~40 s per the briefing)**.

Arms A–C ran the peer's real pre-change binary: `/usr/local/bin/gopi` is the
**first `gopi` on the non-login ssh PATH** (`~/.local/share/pi-node/...`,
`/usr/local/{sbin,bin}`), so a linked worktree file or a `~/bin` copy is NOT
what the ssh leg executes — a trap worth remembering (the first three attempts
all silently measured the old binary).

## Contract ruling (Rule 7 / Rule 15)

Per-batch atomicity **replaces** per-patch atomicity, and the old contract is
genuinely weaker. That is stated, not glossed:

- **What is lost** — after a mid-corpus failure the ledger holds a *prefix* of
  the patch (whole batches only; the failing batch is rolled back) instead of
  nothing. There is no way to keep the old guarantee and shrink the lock: a lock
  window and a transaction boundary are the same thing.
- **What is kept** — every batch is atomic, so a partial state is always a whole
  number of batches, and a failure is still a FAILURE (non-nil error + a message
  that names rows committed/total, the failing batch, **what happened to that
  batch**, and the recovery action).
- **Why it is safe** — this channel's merge is unique-only + insert-only: the
  identity is the exact question, an existing row is NEVER updated, and a row is
  written only after a probe finds its question absent. The merge is therefore
  **idempotent and monotone**: re-running the same sync completes a partial
  patch (already-applied rows are skipped as `existing` — counted, not
  duplicated) and no re-run can corrupt what a previous run wrote. The
  all-or-nothing promise was never load-bearing for correctness here (unlike the
  task ledger's import, whose rows carry state machines and a UNIQUE arbiter
  key): the recovery action for BOTH contracts is the same sentence — re-run the
  sync. Verified: the re-run after a partial apply imports exactly the
  uncommitted remainder (`imported == total − committed`, `deduped == 0`), and
  the run after that imports 0.

The failure message distinguishes three failure points, because an operator
resuming a partial sync needs to know whether the failing batch is in the ledger:
pre-commit (`rolled back cleanly`), commit error (`AMBIGUOUS — may or may not
have landed`), post-commit boundary (`COMMITTED and durable`).

## Verification instrument (and two designs that failed)

The lock property can only be measured by a SECOND connection asking "can you
take the write lock now?" — there is no answer to it from inside the import. Two
sampler designs were built and measured before the one committed; both are
recorded because each looks reasonable and neither works:

- **production busy timeout (15 s)** parks the probe inside ONE refused attempt
  for the whole apply — measured `first_ok_at = 7.1 s` of a 1.5 s apply: a number
  with no relation to the lock.
- **50 ms, then a paced 250 µs poll at busy_timeout=0**: the gap between two
  batches is narrower than one poll period, so a *correct* per-batch
  implementation was reported as a defect (`lock_free_episodes=1`, `oks=2`), and
  an unpaced loop starved the import it was measuring (1 ms/row stretched to
  4–5 ms/row).

The committed instrument is **synchronous and race-free**: the import CALLS it
from the batch-boundary seam (`memoryImportBatchBoundary(batch, phase)`, a var
whose production value allows everything), on both sides of every commit —
`memoryImportBeforeCommit` (the batch's write lock is live) and
`memoryImportAfterCommit` (the commit has returned). No goroutine, no pacing, no
timing assumption.

## Red proof (the batching is what fixes it)

`internal/tools/memory_sync_assets_batch_test.go`, re-run against an arm that
wraps the whole loop in ONE transaction (the pre-change shape, boundary calls
kept so both shapes meet the same instrument):

```
lock_live_at_after_commit: 4 of 4   →  "the import is holding the ledger across
                                       batches (the pre-2026-09-26 defect)"
TestMemoryAssetsImportBatchLockScopeIsOneBatch: batch 2 locked_after_commit=true
   → "a committed batch must release the peer's ledger"
TestMemoryAssetsImportPartialRerunCompletes: target holds 0 patch rows after the
   failure, want the committed prefix 32
```

Restored, all four pass (`redproof-unbatched.txt` holds the transcript).

## Tests added (`go test ./internal/tools/ -run 'MemoryAssets'`)

| test | what it pins |
|---|---|
| `TestMemoryAssetsImportBatchesBoundTheLock` | the live lock at EVERY before-commit boundary and gone at every after-commit boundary (≤1 straggler) |
| `TestMemoryAssetsImportBatchLockScopeIsOneBatch` | the lock's scope is one batch: held → released, plus a control acquisition after the run |
| `TestMemoryAssetsImportPartialRerunCompletes` | seam-injected pre-commit failure **and** a `RAISE(ABORT)` trigger at row 40 (memory #5589 recipe); honest message; idempotent recovery re-run |
| `TestMemoryAssetsImportRejectsGarbage` (existing) | unchanged behaviour of the per-row path |

The deterministic injection is the memory-recorded recipe: a trigger on
`distilled_qa` counting inserts in a side table and `RAISE(ABORT)`ing on the Nth
— no SQLITE_BUSY is manufactured (Rule 27).

## Not done / honest scope

- **The 200 ms pause costs wall clock.** The apply pays ≈10 s extra on a
  51-batch session (≈92–100 s vs ≈44 s measured). That is the price of the peer
  actually being able to write during a sync leg; it is one constant
  (`memoryAssetImportBatchPause`) if a different trade is wanted, and it is not
  paid after the last batch. The raw per-batch hold is unchanged by it.
- **Both hosts still need the new binary** for the pair to be protected: the
  fix protects the ledger of whichever side APPLIES a patch (hp here). hp's
  binaries were left exactly as found (checked out at `a30de028`; no `~/bin/gopi`
  shadow) — deploying it there is a separate deliberate step.
- **`gopi memsync --import` is NOT this path** — it goes through `SyncImport`,
  which still applies its whole patch in one transaction. Same failure family,
  different mechanism; not touched here.
- **Stylegate** has pre-existing failures in this tree (`lineedit/palette_test.go`
  `os.Setenv`, several `time.Sleep` sites) in files this diff does not touch; the
  new test file contributes no stylegate hit.
