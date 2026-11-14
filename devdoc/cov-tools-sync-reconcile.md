<!-- doc-class: historical -->
# Task Report: function-level coverage for the peer-sync reconcile faces (internal/tools)

Epic: `feat/cov-tools-sync`. This report covers the **reconcile** file's 3
functions plus the ③ anchor-guard invariant they exist to protect.

New test file: `internal/tools/memory_sync_reconcile_cov_test.go`.

## Completed

| Function | Definition (code_search) | Tests added | Boundary / error cases named |
|---|---|---|---|
| `TaskDiffPeer` | internal/tools/memory_sync_reconcile.go:80 | `TestTaskDiffPeerRefusesInvalidAlias`, `TestTaskDiffPeerUnreachablePeerErrors`, `TestTaskDiffPeerComparesCatalogBothDirections`, `TestTaskDiffPeerLocalCatalogReadsIsolatedLedger` | `""`/blank/space/tab/newline/`;`/`|` aliases refused as "invalid peer" with nil stats; unresolvable alias → error (never a silent "clean"); three diff classes each NAMED per side; identical and both-empty catalogs → clean; local half reads the pinned ledger; foreign ledger → error |
| `SyncTickShouldRunFull` | internal/tools/memory_sync_reconcile.go:460 | `TestSyncTickShouldRunFullExportedCounterCycle`, `TestSyncTickShouldRunFullMalformedEnvErrors` | N=3: ticks 1–2 not full (count persists), tick 3 FULL + reset, next cycle restarts; N=1 always full; N=0 never full and the counter parks at 0; counter is per-peer; malformed env (`abc`, `-1`, `1.5`, `3x`) → error; whitespace-only env → default |
| `MustSyncFullEvery` | internal/tools/memory_sync_reconcile.go:465 | `TestMustSyncFullEveryFallsBackToDefault` | absent / `0` / malformed / negative / whitespace → named default; explicit `1`, `7`, `" 12 "` → parsed; the returned value is NEVER ≤ 0 |

Guard invariant (③ reconcile-before-increment):
`TestSyncAnchorGuardFailClosedOnUncomparablePeer` — zero anchor commits
freely; a positive anchor with an unreadable peer REFUSES (fail closed, error
names `anchor guard` + the failing transport); the documented sim escape
hatch `GOPI_ANCHOR_GUARD=0` bypasses the comparison only.

Callers (usages verified): `TaskDiffPeer` → `cmd/agent/cli_tasks_sync.go:182`
(`task sync --diff`); `SyncTickShouldRunFull` →
`cmd/agent/cli_tasks_sync.go:315` (serve tick); `MustSyncFullEvery` →
`cmd/agent/cli_tasks_sync.go:330` (the FULL-reconciliation report line).

## Invariants asserted

* **A diff never lies**: every divergence is enumerated per side
  (`MissingOnPeer` / `MissingOnLocal` / `StatusMismatch`) rather than reduced
  to a count, and an unreachable peer ERRORS — a diff that reported "clean"
  because it could not read the peer would defeat the ① pass entirely.
* **Invalid topology names never reach ssh**: the alias guard runs before the
  transport (see the filed security ticket for the residual option-shaped
  alias hole).
* **Reconciliation beats increments (③)**: a positive anchor advance is
  refused when the pre-anchor pending sets cannot be compared — fail closed,
  because a wrong advance silently skips the pre-anchor timeline forever.
* **The periodic full backstop cannot be disabled by a typo** (Rule 2): a
  malformed `GOPI_SYNC_FULL_EVERY` is an error everywhere it is parsed, and
  the report line falls back to the NAMED default instead of printing
  `0`/garbage.

## Not Done / Deliberate gaps

1. **`TaskDiffPeer`'s happy path (both catalogs over ssh) is not exercised
   end to end** — `reconRemoteRowSets` has no injection seam, so asserting
   the remote half would require a real peer. The test instead folds the
   LOCAL half (the real `reconLocalRowSets`) against a peer catalog through
   `reconCompare`, i.e. the exact composition `TaskDiffPeer` performs. The
   seam gap is part of the filed testability bug.
2. ~~**The anchor guard's mismatch branch + repair hint is unreachable
   hermetically** (same seam gap).~~ **CLOSED by `feat/sync-transport-seams`**
   (devdoc/sync-transport-seams.md): `reconRemotePendingSet` now has the
   `reconRemotePendingSetHook` seam (nil = production ssh, the
   `syncSSHExchangeHook` pattern), so the guard's own branches run
   in-process: equal pending sets → advance (`TestSyncAnchorGuardEqualPendingSetPasses`),
   mismatch → refuse with the `gopi task diff --peer X` / `gopi task sync
   --full X` hint (`TestSyncAnchorGuardRefusesOnMismatchWithRepairHint` —
   the wording is now locked), unreadable peer → wrapped transport error
   (`TestSyncAnchorGuardWrapsUnreadablePeerTransportError`). No
   `GOPI_ANCHOR_GUARD=0` is needed for any of them.
3. `reconRemotePendingSet`'s own JSON parsing (empty output → 0 rows,
   unparsable → error) is not driven: it is only reachable through ssh.

## Verification

- `go vet ./internal/tools/` → clean.
- Raw target run:
  `go test -count=1 -run 'TestTaskDiffPeer|TestSyncTickShouldRunFull|TestMustSyncFullEvery|TestSyncAnchorGuardFailClosed' ./internal/tools/`
  → `ok github.com/nick/gopi/internal/tools 0.219s`
- Coverage: none of the 3 names listed by
  `python3 scripts/func_coverage.py --pkg internal/tools --uncovered`.
  Package 148/181 = 81.8%.
- Two-run full-suite raw output: `devdoc/cov-tools-sync-summary.md`.

## Determinism

No network and no sleeps. The two negative transport cases point at aliases
that cannot resolve (`gopi-cov-…-no-such-host.invalid`) and fail FAST — the
test asserts the failure classification, never a timeout wait; both skip
when no `ssh` binary exists so the suite stays portable. Everything else is
an isolated temp ledger or pure in-process comparison; the counter tests pin
`GOPI_SYNC_FULL_EVERY` and read the count back through the package's own
accessor.

## Bugs filed / findings

* **01a08c35-40cf-77b9-aa3a-eeb87e26f173** (tags: bug,security) — option-shaped
  ssh alias is accepted (`-oProxyCommand=…`): the metacharacter guard covers
  space/tab/newline/`;` but not a leading `-`, so the alias is parsed by ssh as
  an OPTION. Verified live: `TaskDiffPeer("-oProxyCommand=x")` → `diff: ssh
  -oProxyCommand=x: exit status 255` instead of `invalid peer`. Payload carries
  the shared-validator fix suggestion (the same re-implemented guard exists in
  the harvest, claim and git-pushback legs).
* **01a08c35-3bb0-7476-9753-634d8ac838c6** (tags: bug,testability) — the ③
  guard's peer-side pending read has no injection seam, so its mismatch/repair-
  hint branch is unreachable without ssh (see the claim devdoc for the pair).
* The test file documents this explicitly next to the alias loop so nobody
  "fixes" the test into asserting a behaviour the product does not have.

## Workflow feedback

* This file is where "write a test" turned into two real findings (the
  unsegmentable local-only argument, the un-guarded option-shaped alias).
  The second one is a security-relevant finding that only appears when you
  actually RUN the guard with hostile input rather than eyeballing it.
* Writing the counter cycle test worth doing twice: the first version did
  not assert the persisted count between ticks, which would have missed a
  "never persists" regression.
