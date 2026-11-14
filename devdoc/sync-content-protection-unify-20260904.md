<!-- doc-class: historical -->
# Task Report: sync content-protection unify (A/B #3 audit follow-up)

Fixes the HP audit's four task-id-collision CONTENT-protection defects
(devdoc/audit-ab3-hp-20260904.md, commit 637c4f3): the anti-silent-loss
gate was applied inconsistently across the three cross-node sync channels.
Import was protected; harvest was partial; claim had none.

Branch: `fix/sync-content-protection-unify`
Commits (base 0b3dff5 = audit HEAD):

| commit | defect(s) | change |
|---|---|---|
| 7cf4e9b | DEFECTS 1 + 4 (harvest) | strictly-newer terminal adoption content gate |
| 8563867 | DEFECTS 2 + 3 (claim) | acquire content gate + terminal-verb leg |
| ca25397 | DEFECT 2 companion | ghost-peer terminal push inserts history |

## Per-channel gate matrix — BEFORE

| path | shape | gate |
|---|---|---|
| import (v1 `importTaskExisting`) | terminal sync-back over divergent pending | REFUSED ("refusing to fabricate a completion") |
| import (v1) | same-id different-work live row | relabel / never rewrite |
| harvest equal-stamp ladder | terminal > live (blocked) | `harvestSameContent` gate |
| harvest pending-collision relabel | remote pending + local pending divergent | `harvestSameContent` gate |
| harvest **strictly-newer terminal** | terminal report over live/pending row | **NONE** — `harvestUpdate` overwrote unconditionally (DEFECTS 1+4) |
| claim acquire (M4.21) | pending → claimed → in_progress | **NONE** — blind status CAS (DEFECT 3) |
| claim verb switch | terminal push (`PushTerminalTaskSync`) | **REFUSED** "unsupported claim verb status" (DEFECT 2) |

## Per-channel gate matrix — AFTER

| path | shape | gate |
|---|---|---|
| import (v1) | terminal sync-back over divergent pending | unchanged (REFUSED — the reference model) |
| harvest strictly-newer terminal | terminal report over live/pending row | NEW: gate on work identity (summary). Divergent → Skipped + refusal recorded in `counts.Errors`, local row untouched. Same work → migrate as before. |
| claim acquire | divergent pending row on peer | NEW: gate on work identity (summary). Divergent → refused ("same task_id id collision"), the remote claimer reverts+yields; the peer's row stays claimable. Same work / self-preseed / mirrored preseed → proceed as before. |
| claim terminal verb | peer HOLDS the row | NEW `syncClaimRemoteTerminal`: same-work terminal report advances the row through the rank ladder (`syncDeltaApplyNewer` — content never rewritten); divergent → refused. |
| claim terminal verb | ghost peer (never saw row) | NEW: insert terminal history verbatim (never pending), `from-<node>` provenance — no more accepted-but-lost. |

## The content gate

The audit's fix direction: "Every status-only/terminal transition that could
land on a row whose content differs from the incoming payload must compare
content BEFORE applying, and REFUSE (leave the row untouched, record the
refusal) when they differ."

Gate key = the **summary** (the immutable work identity), via the shared
`sameWorkIdentity` comparator (internal/tools/memory_sync_task.go) used by
all three new gates. Summary is the canonical identity (payload is the
briefing's free-text restatement; the test harness routinely varies payload
across nodes for the SAME work, so a payload-hash gate would false-positive
on the legitimate same-work-terminal-adoption paths —
TestHarvestNewerWinsMigration, TestHarvestLiveLocalSkipped, the equal-stamp
ladder). A same-second id collision is two DIFFERENT submitted tasks →
differing summaries → caught.

Recording: harvest records refusals in `HarvestCounts.Errors` + counts as
Skipped (never Failed — a refusal is a permanent adjudication, not a
transient row error; the anchor must not rewind into an infinite refusal
re-fetch). Claim records via the refusal `reason` string in the
`ClaimPushResponse` (the claimer sees it and reverts+yields).

## DEFECT-by-DEFECT verdict

1. **harvest strictly-newer terminal-over-LIVE overwrite** —
   `TestHarvestStrictlyNewerTerminalOverDivergentLive` now PASSES.
   A divergent terminal report no longer rewrites a locally-live row.
2. **PushTerminalTaskSync refused by the claim verb switch** —
   `TestSyncClaimRemoteRefusesTerminalPush` now PASSES. Terminal verbs have
   a handler again (same-work advance via the rank ladder).
3. **claim acquire hijacks a divergent pending row** —
   `TestSyncClaimRemoteAcquireHijacksDivergentPending` now PASSES. Acquire
   refuses divergent content.
4. **harvest terminal-over-PENDING fabricates a completion** —
   `TestHarvestNewerTerminalOverDivergentPendingFabricatesCompletion` now
   PASSES. The local pending row stays pending; no ghost completion.

## Test-spec repairs (audit repro tests were self-contradictory)

Two audit repro tests encoded the DEFECT as the wanted behavior in their
assertions, contradicting their own spec comments:

- `TestHarvestStrictlyNewerTerminalOverDivergentLive`: after asserting
  status must stay in_progress (the protection), it ALSO asserted the bug
  fired (`row.Summary == "local live work"` → "row SURVIVED (unexpected)").
  Post-fix a surviving row is exactly the desired outcome. Repaired to
  assert content survived + refusal recorded.
- `TestSyncClaimRemoteRefusesTerminalPush`: asserted `resp.Accepted` must
  be FALSE while the message says the push "must be accepted". Repaired to
  assert accepted + mirror converged.

Both repairs preserve the audit's spec comments (the test comments still
name the wanted behavior).

## Regression notes

- Surrounding families stay green: `go test ./internal/tools/ -run
  "Harvest|Claim|Sync|Import"` PASS (~82s).
- Two probe tests that called `syncClaimRemoteAcquire` directly with the
  old 3-arg signature updated to pass the wire row (matching content —
  self-preseed semantics unchanged): `m421_lock_probe_test.go`,
  `selfpreseed_probe_test.go`.
- The acquire gate runs BEFORE the self-preseed fast path and the Phase-2
  CAS: a same-work acquire is untouched (self-preseed, mirrored preseed,
  genuine competitor refusal all behave as before — M4.21 lock suite
  green).
- syncDeltaApplyNewer's 0-row re-read/refusal paths are unchanged (a
  concurrent writer outranking the push reports via RowsRefused → the
  claim leg surfaces it as a refusal reason).

## Not Done

- No live cross-node E2E rerun (peerbox↔hp); the unit repro tests exercise
  the full handler paths (SyncClaimRemote end-to-end over the real ledger;
  TaskHarvest through harvestApplyTx).
- DEFECT 2's stale comment at db22503:625 ("the applyNewer ladder accepts
  it") was superseded by the new terminal leg's own comment — the ladder
  claim is accurate again now that terminal rows route to it.

## Verification

- Build: `go build ./...` OK
- Targeted: 4 audit repro tests + ghost-terminal regression PASS
- Family: `go test ./internal/tools/ -run "Harvest|Claim|Sync|Import"` PASS
- Full tools suite: (see commit message / job output)
- Commits: 7cf4e9b, 8563867, ca25397 on `fix/sync-content-protection-unify`

## Recovery

- Branch: `fix/sync-content-protection-unify` (worktree
  ~/workspace/gopi-sync-content-gate, shared with ~/work/GoPi — commits
  already on the branch in the main repo; no push needed)
- `git show fix/sync-content-protection-unify:devdoc/sync-content-protection-unify-20260904.md`
