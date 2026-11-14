<!-- doc-class: historical -->
# Task Report: anchor-guard refusal root cause — claim-lock gate + rollback gap

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Branch: `feat/fix-anchor-guard-claim-gate` (worktree
`~/workspace/gopi-anchor-guard-fix`, shared object store with `~/work/GoPi`).

## The incident

Live peerbox↔hp sync hit the M4.24 anchor-advance guard:

```
gopi task sync --peer hp
anchor guard: refusing advance to 1788329099 — pending counts differ
(local=1248 peer=1246 rows)  →  repair: gopi task sync --full hp
```

`task diff --peer hp` showed the real divergence: 2 rows
(t-1788305267, t-1788329099) were **pending on peerbox but in_progress on
HP with DEAD claim pids** (hostb-Laptop:890052 / 889942 — not in the
process list, no session_id/model). The guard was working exactly as
designed — the pending-count premise was genuinely broken by stuck rows.

## Root cause — two code defects, one compounding incident

### Defect A: the claim-lock acquire patch is emptied by the M4.24 anti-reflow gate

`exportClaimPatch` (internal/tools/memory_sync_claim.go) built its
one-row claim patch through `SyncTaskExportDelta(SyncTaskDeltaOptions{Peer: peer})`.
Since c4745d9 wired `Peer`, the exporter's anti-reflow gate excludes
**live rows tagged from-<peer>** — but those are precisely the rows a
cross-node claim must reach:

- HP imports a peerbox-origin row via sync → HP's copy is tagged `from-peerbox`.
- HP runs `gopi task run t-...` → TaskClaimExplicit (pending→claimed) →
  TaskStart (claimed→in_progress) → RunClaimLockProtocol(peer=peerbox).
- exportClaimPatch(peer=peerbox) → the gate drops the live `from-peerbox`
  row → the acquire patch has 0 rows → `"task ... not found in the
  ledger (row vanished between claim and lock)"`.
- Every round errors → 3 retries exhausted → **yield**.
- The yield's RollbackLocalClaim only matched `status='claimed'` — the
  row was already in_progress → `"raced a writeback — left as-is"` →
  **permanent zombie** (in_progress, dead pid, no legal local exit).

### Defect B: RollbackLocalClaim can't revert the in_progress rung

runTaskRunOnce (cmd/agent/cli_tasks.go) calls TaskStart BEFORE
RunClaimLockProtocol — the acquire patch must ride the row as
in_progress (the peer's `--claim-remote` handler switches its acquire
verb on `in_progress`). But when the remote leg then fails, the local
rollback (`UPDATE ... WHERE status='claimed'`) hits 0 rows on the
already-advanced row. The state machine has no in_progress→pending
edge, so the row is stranded until manual cleanup — and its
pending-count presence on the origin (peerbox) vs zombie in_progress on
the claimant (HP) wedges the anchor guard forever.

**The discriminator**: HP-origin rows (peerbox copies tagged
`from-hostb-Laptop`) claimed fine — peerbox's export to HP never gates
on from-HP rows the same way — while peerbox-origin rows claimed on HP
always died. Rows whose origin == the claim target peer could never
complete a two-phase lock.

## Fix (two surgical changes + regression tests)

1. **`SkipReflowGate` export option** (internal/tools/memory_sync_delta.go):
   `SyncTaskDeltaOptions.SkipReflowGate` disables the anti-reflow
   exclusion. Used ONLY by the claim channel.
2. **exportClaimPatch bypasses the gate** (memory_sync_claim.go): the
   claim patch now exports with `SkipReflowGate: true, AfterSource: 0`
   (full single-row fetch). A claim patch is a lock-protocol VERB aimed
   at the row's origin — it must carry the target row regardless of
   provenance. The peer's `--claim-remote` handler owns the CAS/refusal
   decision; an echo row no-ops idempotently, so carriage is safe.
   Terminal-push caller unchanged in behavior (terminal rows were
   already gate-exempt).
3. **RollbackLocalClaim reverts in_progress too** (memory_sync_claim.go):
   `WHERE status IN (claimed, in_progress)`. Safe: the only in_progress
   caller is runTaskRunOnce's yield, which precedes the child spawn —
   no genuinely-executing task can be reverted. Terminal rows still never
   roll back (the `RowsAffected==0` refusal keeps that contract).

## Regression tests (both fail on pre-fix code with the exact incident errors)

- `TestExportClaimPatchCarriesPeerOriginRow` — a live from-peerbox row
  must be carried by exportClaimPatch(peer=peerbox). Pre-fix:
  `task ... not found in the ledger (row vanished between claim and lock)`.
- `TestRollbackLocalClaimRevertsInProgress` — a started (in_progress)
  row must revert to pending on rollback. Pre-fix:
  `claim rollback: task ... is no longer claimed (raced a writeback — left as-is)`.

## Verification

- Build: `go build ./...` OK; `go vet ./internal/tools/` clean.
- Regression tests: pass post-fix, fail pre-fix (git-stash A/B — both
  reproduce the EXACT incident error strings).
- Full suites (machine shared with 2 other concurrent full-suite runs):
  - `go test -count=1 ./internal/tools/ -timeout 20m` — **PASS, 910s**
    (earlier 600s FAIL was the default 10m package timeout under
    3-concurrent-suite load, not a code failure — same package passes
    with the longer timeout).
  - `go test ./cmd/... ./internal/config/` — **PASS** (cmd/agent 220s;
    the first run's FAIL was again the 10m package timeout).
  - Targeted reflow/claim/bidir2 subsets: PASS.
- Commit: on `feat/fix-anchor-guard-claim-gate` (recovery:
  `git show feat/fix-anchor-guard-claim-gate --stat` from `~/work/GoPi`).
  HEAD 1e8e8e3.

## Live convergence (post-fix) — 2026-09-02 实测

The two/three HP zombie rows (dead pids hostb-Laptop:890052/906933/889942)
were the pending-count divergence source. Cleanup sequence on the LIVE
system (both serve daemons running):

1. `gopi task sync --full hp` from peerbox — converged 19 → 2 differences
   (the full replay pulled peerbox's completed/failed verdicts onto HP;
   t-1788329099's HP zombie was overwritten to completed by peerbox's
   newer terminal verdict).
2. The 2 remaining (t-1788305267/5268: HP in_progress zombie with NEWER
   updated_at vs peerbox's OLD pending) could NOT be overwritten by sync —
   newer-wins kept the zombie. Released via `task update --status
   cancelled` on HP (the only legal in_progress exit; no sanctioned
   release-to-pending CLI exists — RollbackLocalClaim's in_progress
   revert is internal to the yield path).
3. `sync --full hp` again → **diff identical (local=1256 peer=1256)**,
   anchor 1788329099 → 1788335567, normal `sync --peer hp` advances
   cleanly (exit 0, no guard refusal). 0 zombies remain on HP.

### ⚠️ Honest note — 2 cancelled rows

t-1788305267 ([code-review] text2img) and t-1788305268 ([code-review]
shot) were GENUINE peerbox-origin pending review tasks whose HP zombie
copies I cancelled. Cancelling the zombie propagated the terminal state
to peerbox (its old pending was overwritten by the newer cancelled — the
cleanup tradeoff). Both were LLM-submitted code-review items whose
subject code appears already reviewed+merged in the 2026-09-02 history
(review/cli-vision 557a3b8 covers the image-input surface; shot reviewed
in completed t-1788305225), so impact is low, but if the review is still
wanted the reissue is `gopi task reopen <id> --reason ...` once the
pending queue drains below the cap of 3 (currently 7 pending).
Lesson: for future zombie cleanup of peer-origin rows, prefer leaving
the origin's copy untouched and only fix the zombie side, then let the
origin's own full sync carry its verdict — cancel only when the origin
has no live intent for the row.

## Not Done

- No dedicated E2E for the claim-lock cross-origin path yet (the m421
  sim drives the lock functions in-process and never exercised the ssh
  export gate — that blind spot is why c4745d9's regression slipped).
  Follow-up: extend scripts/e2e-m421-claim-lock.sh with an A-origin-row-
  claimed-by-B round.
- Merging: per Rule 9, merge to master is PA's call.
