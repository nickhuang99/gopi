<!-- doc-class: historical -->
# Task Report: reflow-gate contract — 3 pre-existing reds root-caused & fixed

Branch: `fix/reflow-gate-contract` (worktree `~/workspace/gopi-reflow-gate`,
shared object store with `~/work/GoPi` — commit already visible from the main repo).

## Completed

Three production defects behind the 3 pre-existing test reds, one contract:

1. **Double-prefix disarm (the actual root cause of the reds).** eef8342
   flipped both exporters to the from-<peer> gate and set the zero-arg
   fallback to `peerTag = syncSelfTag()` — but `syncSelfTag()` already
   returns the full tag (`from-<node>`), and the SQL pattern wrapped it
   again: `LIKE '%,from-"+peerTag+",%'` → searched `from-from-<node>`,
   never matched → **the zero-arg self-gate was silently dead** in both
   exporters. Every "legacy fallback protects the v1 channel" claim since
   eef8342 was vacuous. Fix: new `syncGateTag(peer)` returns the raw node
   NAME (named peer → trimmed peer; "" → GOPI_SYNC_NODE || hostname);
   the pattern adds `from-` exactly once. Regression fence:
   `TestReflowGateTagNormalization` (incl. the gate-tag ≡ provenance
   identity invariant).

2. **Peer wiring (the M4.24 dead-wire gap).** `SyncTaskDeltaOptions.Peer`
   shipped in eef8342/affc1b3 but only `syncPeerSessionFull` (M4.24
   reconcile) ever set it — session push, serve-remote pull-back, claim
   exchange, terminal broadcast and `SyncTaskPushOnly` all ran the
   (dead) fallback. Wiring now: session push `Peer: peer`; serve-remote
   pull-back `Peer: patchIn.Node`; claim exchange per peer; terminal
   broadcast **exports per peer** (a single shared export cannot express
   a per-peer exclusion); push-only per peer.

3. **Terminal homecoming (restores M4.20-E on the v2 channel).** The
   from-<peer> exclusion also swallowed a row's terminal VERDICT coming
   home (B walks A's row to completed → B's push back to A excluded it →
   the origin never learns). The gate now exempts terminal rows:
   `NOT (status IN (pending,claimed,in_progress) AND tags LIKE from-<gate>)`.
   Live rows keep the strict exclusion — that's the echo-loop face the
   gate exists for (TestReflowEchoStaleMirrorNoReturn stays green).

## Not Done
- v1 CLI file channel (`--export-tasks`) intentionally stays on the
  zero-arg fallback (no session peer concept on a file handshake).
- The three-node relay repair claim from sync-reconcile.md's residual is
  now unblocked but has no dedicated E2E sim yet.
- Merging: per Rule 9, merge to master is PA's call, not mine.

## Verification
- Build: `go build ./...` OK; `go vet ./...` clean.
- Tests: `go test -count=1 ./internal/tools/` — **PASS, full package, 64s**
  (includes the 4 previously-red sims: TestBidir2ImportSymmetry,
  TestBidir2LiveConflictNeverClobbered, TestBidir2DivergentTerminalsConverge,
  TestBidirEpic4IdempotentRoundTrip + all 6 reflow gate tests).
- `go test ./cmd/... ./internal/config/` PASS.
- Commit: `c4745d9` on `fix/reflow-gate-contract` (recovery:
  `git show fix/reflow-gate-contract --stat` from `~/work/GoPi`).

## Test contract changes (conscious, each pinned with a comment)
- `TestReflowExportExcludesFromPeerRows` — peer gate excludes a LIVE
  from-<peer> row; own + third-node rows travel.
- `TestReflowExportLegacySelfFallback` — zero-arg contract restored and
  now REAL (was dead code): self-tagged rows excluded, foreign rows travel.
- `TestReflowGateSelfOnlyBothTagsExported` → live from-<peer> row excluded
  toward its sender, travels toward a third node.
- `TestBidir2ImportSymmetry` echo expectation 2→1: the gated t-zb-new
  echo **was the reflow**.
- New: `TestReflowGateTagNormalization` (double-prefix fence).

## Root-cause notes for future agents
- Symptom-level debugging wasted ~10 probe rounds: the failure looked
  env-dependent (passed in fresh probe files, failed in reflow_test.go).
  It wasn't. Lesson: when a probe "passes" while the real test "fails",
  diff the SQL args, not the environment — the double prefix only
  manifests with the fallback path (zero-arg), and my early probes had
  set GOPI_SYNC_NODE such that the fallback built the same wrong string
  in both (masking). The disarming probe (`Peer: st` explicit) is what
  exposed it.
- GOPI_SYNC_NODE=hp lives in ~/.profile on this host — never assume a
  clean env; the identity cache (config.ResetNodeIdentityForTest) is
  orthogonal (roles), syncSelfTag is NOT cached.
