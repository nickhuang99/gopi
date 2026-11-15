<!-- doc-class: normative -->
# Task Report: function-level coverage for the peer-sync session faces (internal/tools)

Epic: `feat/cov-tools-sync`. This report covers the **session** file's 2
functions (the bidirectional peer session + the status line).

New test file: `internal/tools/memory_sync_session_cov_test.go`.

## Completed

| Function | Definition (code_search) | Tests added | Boundary / error cases named |
|---|---|---|---|
| `SyncPeerSessionTags` | internal/tools/memory_sync_session.go:64 | `TestSyncPeerSessionTagsRefusesUndeclaredPeer`, `TestSyncPeerSessionTagsRefusedWhenNoPeersDeclared`, `TestSyncPeerSessionTagsTransportFailureLeavesAnchorUntouched`, `TestSyncPeerSessionTagsPartitionRidesThePatch`, `TestSyncPeerSessionTagsHappyPathAnchorAndStats`, `TestSyncPeerSessionTagsEmptyPatchStillAcks` | undeclared peer refused (names `sync_peers` + `GOPI_SYNC_PEERS` + the peer, nil stats); empty topology refuses everything; transport failure → error, NO anchor/last_synced movement; `--tags` stamped into the patch so the peer mirrors the partition (only the tagged row is pushed); happy path stats (pushed rows, push anchor = max updated_at, push ack, pull anchor = peer envelope anchor, staged file cleaned up); empty ledger still acks; committed anchor is forward-only against a stale re-commit |
| `SyncPeerStatus` | internal/tools/memory_sync_session.go:282 | `TestSyncPeerStatusNeverSyncedAndAnchored`, `TestSyncPeerStatusLedgerErrorPath` | unknown peer → "never synced"; anchored peer → peer-domain anchor + `last_synced=` local stamp and never the never-synced wording; foreign ledger → error with an empty line |

Callers (usages verified): `SyncPeerSessionTags` →
`cmd/agent/cli_tasks_sync.go:163` (`gopi task sync --peer p --tags …`);
`SyncPeerStatus` → `cmd/agent/cli_tasks_sync.go:92` (`task sync --status`).

## Invariants asserted

* **Topology is declared, never discovered**: a peer that is not in the
  whitelist (settings `sync_peers` / `GOPI_SYNC_PEERS`) is refused even
  though it is a perfectly well-formed alias; an empty whitelist refuses
  every peer. No code path syncs "with whoever is reachable".
* **锚点最后写 (anchor last)**: a failed transport leg leaves the anchor AND
  the local last-synced stamp at 0 — the anchor is the proof the peer applied
  our rows, so a failed leg proves nothing and must re-pull the window.
* **Anchors are forward-only**: after a successful commit, a stale anchor
  re-commit cannot roll it back.
* **Partition symmetry (epic ③)**: the `--tags` filter rides IN the patch
  (`patch.TagsFilter`), so the peer's push back is partitioned identically —
  only the tagged row is exported, and `PushedRows` reflects the partition.
* **A peer answer is always accounted**: an EMPTY patch is still a valid
  session (`PushAck` true, empty stats, merged peer delta reported) — never
  an error.

## Not Done / Deliberate gaps

1. **The anchor guard had to be disabled to test the anchor commit.**
   `syncPeerSession` commits through `guardSyncStateCommit` →
   `syncAnchorGuard` → `reconRemotePendingSet`, which shells out to
   `ssh` unconditionally (no injection seam). So the two tests that commit an
   anchor set the documented sandbox escape hatch `GOPI_ANCHOR_GUARD=0` — the
   same thing `memory_sync_bidir2_e2e_test.go:354` already does. **Filed as a
   testability bug ticket 01a08c35-3bb0-7476-9753-634d8ac838c6 (bug,testability)**
   with the seam fix; the guard's own behaviour is
   covered in `devdoc/cov-tools-sync-reconcile.md`.
2. **No real ssh leg** (by design: the epic forbids network tests). The
   transport is replaced by the package's OWN seam
   (`syncSSHExchangeHook`), which runs the peer's real `SyncRemoteServe`
   in-process — the session's whole engine path minus the byte pipe. The
   byte pipe itself is the existing `TestSyncRemoteServeRoundTrip` +
   `TestClaimSSHExchangeUnreachableFailsSoft`'s territory.
3. `DurationSec` is only asserted non-negative; timing assertions would be
   non-deterministic on a loaded host.

## Verification

- `go vet ./internal/tools/` → clean.
- Raw target run:
  `go test -count=1 -run 'TestSyncPeerSessionTags|TestSyncPeerStatus' ./internal/tools/`
  → `ok github.com/the operator/gopi/internal/tools 0.308s`
- Coverage: both names gone from
  `python3 scripts/func_coverage.py --pkg internal/tools --uncovered`.
  Package 148/181 = 81.8%.
- Two-run full-suite raw output: `devdoc/cov-tools-sync-summary.md`.

## Determinism

No network, no ssh, no sleeps. The ssh byte pipe is the package's documented
`syncSSHExchangeHook` (saved + restored per test); the peer's answer is the
real `SyncRemoteServe` executed in-process against the isolated temp ledger.
Peers come from a sandbox `settings.json` written into `t.TempDir()` with
`GOPI_NODE_ROLE=client` and node identity reset, so no ambient developer
settings can leak into the assertions. Success/failure transport shapes are
fully controlled by the hook, so both branches are deterministic.

## Workflow feedback

* Reusing the product's own hook seam (rather than adding one) is what made
  a "network" function testable at all: the seam already existed for the E2E
  sims, and pointing it at `SyncRemoteServe` gives a real peer answer with
  zero network. Worth remembering for any other `ssh`-shaped function.
* The anchor rollback assertion (stale re-commit after a real session) is the
  kind of invariant that only a coverage pass surfaces — it is the
  never-clobber rule at the anchor level, and it is now locked.
