<!-- doc-class: historical -->
# Fix brief for next session (recovery: this file + devdoc/tools-redlist-round6.md @ 2fb6bc3)

Fix task t-1788210015 got sync-yielded-cancelled and reopen is blocked by
the pending cap (15 merge-backlog rows). When the queue drains, resubmit with:

  gopi task reopen t-1788210015 --reason "re-run with completed root cause evidence"

or submit fresh (tags=bug) with the payload below.

## Fix payload (verified evidence — full table in devdoc/tools-redlist-round6.md)
PRIMARY root cause: anti-reflow export gate (56fd84a, memory_sync_delta.go ~L117,
`NOT LIKE '%'+syncSelfTag()+'%'`) collides with the bidir e2e same-host sim:
simulated node names (peerbox/hp) vs host hostname (peerbox) → syncSelfTag()='from-peerbox'
matches imported rows' from-peerbox tag → next export SQL-filters all of them (Scanned=0).
Probe proof: GOPI_SYNC_NODE=hp-alias before b's export returns the 2 missing rows.
SECONDARY: 236fb03 made config role primary — the 83ed846 GOPI_SYNC_PEERS pin alone
no longer fixes server/client; need explicit GOPI_NODE_ROLE pin + config.ResetNodeIdentityForTest
(recipe: TestSyncDeltaConflictEqualAnchor, memory_sync_delta_test.go:218).

## Fix (minimal, test-only)
1. newBidirNode (or use()): t.Setenv("GOPI_SYNC_NODE", n.name) — node identity follows
   the simulated name (documented semantics, memory_sync_delta.go:134).
2. pushTo dst pin: peerbox→GOPI_NODE_ROLE=server, else client; + ResetNodeIdentityForTest
   before/after (t.Cleanup).
3. tickSync: same two pins.
4. Keep ALL semantic assertions (mirrored counters, live guard, completed-wins) intact.
   Do NOT touch production gate or N1 fail-safe client default.

## Verify
- go build ./... green
- The 3 tests green twice consecutively: job_start `go test ./internal/tools/ -run
  'TestBidir2ImportSymmetry|TestBidir2LiveConflictNeverClobbered|TestBidir2DivergentTerminalsConverge'
  -count=1 -timeout 600s` (serialized; parallel pkg runs deadlock SQLITE on this host).
- Branch feat/bidir2-fix + devdoc/bidir2-fix.md (root-cause table + Rule 6d declaration). No merge.
