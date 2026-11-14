<!-- doc-class: historical -->

> **RETIRED 2026-10-26 — this page describes machinery that NO LONGER EXISTS.**
> `gopi task sweep-zombies` and the automatic zombie disposer were deleted whole; a stuck
> row is now REPORTED and left exactly where it is. Read this page as history, not as
> instructions: any command below that names `sweep-zombies` exits 1 with a pointer.
> The current behaviour is `devdoc/zombie-sweep-removed.md`.

# Task Report: zombie auto-dispose fixture — t-zd-confirmed (live row) classification

**Task:** `t-zd-confirmed` — "zombie dispose t-zd-confirmed" (fixture: zombie auto-dispose).
**Branch:** `verify/zombie-zd-confirmed` (worktree `/home/user/workspace/gopi-zd-confirmed`, based on master `cec425cc`).
**Posture:** the live ledger was NEVER mutated. Every classification experiment ran on a
**schema-only copy** of `~/.gopi/memory.db` in a throwaway dir with a sandbox sink.

## Completed

- Independent classification of the live fixture row `t-zd-confirmed` on a schema-only
  ledger copy (live ledger never mutated): live claim stamp ⇒ veto (no report, no kill);
  reaped pid ⇒ `confirmed` + disposed past the dispose grace. Both proven with the REAL
  binary in a sandbox.
- Fixture-fidelity gap identified (no sweep fixture wrote the claim-stamp referent) and
  closed with a regression test; non-vacuity proven by a throwaway dead-pid probe.
- Devdoc + report written; no production code needed changing.

## The question

The live ledger carries the fixture row `t-zd-confirmed` — `status=in_progress`,
`confidence=confirmed` (as a zombie verdict), heartbeat (`last_toolcall_at`) 3426 s
stale, `claimed_by=peerbox:<live worker pid>`. Two claims needed independent proof:

1. a **live claim stamp** must veto the sweep (no report, no disposal) even when the
   heartbeat is arbitrarily stale — zero false positives;
2. once that claim is genuinely gone, the row must be **confirmed + disposed** by the
   ordinary sweep once past the dispose grace — no manual verb.

## Findings

### 1. Live-row classification (row copy of the REAL row, `sqlite3`/`python3`, sandbox only)

Method: schema-only clone of the live DB + `INSERT` of the row verbatim (no write ever
reached the live ledger; the row stayed `in_progress` with its own live claim throughout).

**(a) row exactly as taken — live claim stamp, heartbeat backdated 7200 s**
`gopi task sweep-zombies --stall-sec 600 --kill`:

```
stdout: no zombie rows
stderr: zombie sweep t-zd-confirmed suppressed: claimed pid 2140436 alive on peerbox — runner live, stale heartbeat is a false positive
ledger: t-zd-confirmed = in_progress      sink: EMPTY
```

⇒ the live claim stamp vetoes **both** the report and the disposal leg. A stale heartbeat
alone is never evidence.

**(b) same row, claim stamp re-pointed at a reaped pid (`1325417`), dispose grace 600 s**

```
stdout: 1 candidate(s) … t-zd-confirmed confirmed (stalled) [KILLED]
ledger: t-zd-confirmed = failed, ended_at set
task_events.detail: zombie disposal (zombie: heartbeat stalled past grace (last_toolcall_at idle));
                    heartbeat stop + claimed pid 1325417 dead (peerbox) — confirmed zombie
sink:   failed event delivered
```

⇒ with the claim genuinely dead the row is classified `confirmed` and disposed by the
ordinary sweep — the epic's two claims hold end-to-end on the REAL binary.

### 2. Fixture-fidelity gap found and closed (regression test added)

Every pre-existing sweep/lifecycle fixture (`sweepFixture`, `disposeFixture`,
`seedSweepZombieCLI`, the detect-stale fixtures) writes `claimed_by` +
`created_at`/`updated_at`/`last_toolcall_at` and leaves **`in_progress_at`/`claimed_at`
NULL**. With `HasInProgressAt == false` the b′ claim-stamp freshness axis reads
`claimStampUnknown` — so the only veto those tests ever exercised was the **pid** layer.

The live row is *not* that shape: a real task start stamps **both** clocks
(`claimed_at == in_progress_at == 1789315573`, 205 s after `created_at = 1789288456`) —
the de-facto **FRESH** shape, which routes the veto down the `claimStampFresh` path. No
sweep-level test covered that path.

Closed by `TestSweepZombies_LiveLedgerShapeKeepsItsVeto`
(`internal/tools/taskzombie_sweep_test.go`): stalled heartbeat + `host:<this pid>` +
`claimed_at == in_progress_at`, swept with the disposal leg's exact options
(`Kill=true`, `Notify=&false`, `IgnoreRearm=true` — what
`autoDisposeConfirmedZombies` passes) ⇒ `reported=0 killed=0`, row stays `in_progress`.

**Non-vacuity proven** (throwaway probe, deleted before commit): the identical fixture
with a **dead** pid (`host:<reaped pid>`) gave `reported=1 killed=1` with the note
`claimed pid 1356797 dead (peerbox) — confirmed zombie`. The test therefore fails if the
fresh-stamp veto ever regresses.

**Production verdict:** the veto holds on the live shape; the mismatch observed live
(`created_at` 3426 s before the claim) never reaches the verdict, because `stallHit`
uses `last_toolcall_at` for rows that have it.

## Files

- `internal/tools/taskzombie_sweep_test.go` — +48 lines (one regression test; no production code changed).

## Verification

- Build: `go build ./...` → exit 0 (`/home/user/workspace/gopi-zd-confirmed`).
- Format: `gofmt -l internal/tools/taskzombie_sweep_test.go` → empty.
- Tests: `go test ./internal/tools/ -run TestSweepZombies -count=1` → `ok … 58.4s`;
  targeted `-run 'TestSweepZombies_LiveLedgerShapeKeepsItsVeto|TestSweepZombies_LivePidVetoesKill|TestSweepZombies_SuspectedNeverKilled' -v` → 3/3 PASS.
- Real-binary A/B (row copy): sandbox `no zombie rows` + suppression note (live stamp) vs
  `1 candidate(s) … [KILLED]` → `failed` + audit reason (reaped pid).

## Not Done

- No production code was changed (none was needed — the veto verified correct on the live shape).
- Full `go test ./...` was not run; the touched package (`./internal/tools/`) was, in full.
- The live row `t-zd-confirmed` was deliberately NOT disposed: its claim names a live
  worker, so disposal would have been the very false positive this task exists to rule out.
  Whoever owns that row can start it / cancel it through the normal verb.
