<!-- doc-class: historical -->
# Task Report: peer-sync function-level coverage (epic feat/cov-tools-sync)

## Completed

Covered all **16** target exported functions across the five named files with
new test files under `internal/tools/` (tests only — `git diff` against the
base commit touches **no** product code).

| File | Functions covered | New test file |
|---|---|---|
| memory_sync_claim.go | `WaitClaimLockVerdict`, `NotifyTerminalPushFailed`, `TaskClaimOwner`, `LiveRowsOwnedByOthers`, `OpenWorkerProbeDB` | `memory_sync_claim_cov_test.go` |
| memory_sync_delta.go | `WorkerTaskTag`, `TagWorkerClaim`, `TaskRunWritebackTagged`, `TaskRunWritebackTags`, `SyncStateList` | `memory_sync_delta_cov_test.go` |
| memory_sync_reconcile.go | `TaskDiffPeer`, `SyncTickShouldRunFull`, `MustSyncFullEvery` | `memory_sync_reconcile_cov_test.go` |
| memory_sync_session.go | `SyncPeerSessionTags`, `SyncPeerStatus` | `memory_sync_session_cov_test.go` |
| memory_sync_task.go | `DefaultGitPushback` | `memory_sync_task_gitpushback_cov_test.go` |

Per-file detail (function inventory, code_search definitions + non-test
callers with file:line, per-test boundary/error naming, determinism, gaps) is
in the five devdocs:
`devdoc/cov-tools-sync-claim.md`, `-delta.md`, `-reconcile.md`, `-session.md`,
`-task.md`.

### code_search evidence (definition lookups — one structured call each)

All 16 returned a single definition, no ambiguity:

```
WaitClaimLockVerdict    internal/tools/memory_sync_claim.go:224   func
NotifyTerminalPushFailed internal/tools/memory_sync_claim.go:916  func
TaskClaimOwner          internal/tools/memory_sync_claim.go:922   func
LiveRowsOwnedByOthers   internal/tools/memory_sync_claim.go:934   func
OpenWorkerProbeDB       internal/tools/memory_sync_claim.go:957   func
WorkerTaskTag           internal/tools/memory_sync_delta.go:279   func
TagWorkerClaim          internal/tools/memory_sync_delta.go:291   func
TaskRunWritebackTagged  internal/tools/memory_sync_delta.go:305   func
TaskRunWritebackTags    internal/tools/memory_sync_delta.go:318   func
SyncStateList           internal/tools/memory_sync_delta.go:1035  func
TaskDiffPeer            internal/tools/memory_sync_reconcile.go:80  func
SyncTickShouldRunFull   internal/tools/memory_sync_reconcile.go:460 func
MustSyncFullEvery       internal/tools/memory_sync_reconcile.go:465 func
SyncPeerSessionTags     internal/tools/memory_sync_session.go:64  func
SyncPeerStatus          internal/tools/memory_sync_session.go:282 func
DefaultGitPushback      internal/tools/memory_sync_task.go:1448   func
```

Non-test callers (verified with keyword/grep after code_search):

```
WaitClaimLockVerdict   internal/tools/memory_sync_claim.go:658   (RunClaimLockProtocol)
TaskRunWritebackTags   internal/tools/memory_sync_delta.go:311   (TaskRunWritebackTagged)
TagWorkerClaim         internal/tools/memory_sync_delta.go:310   (TaskRunWritebackTagged)
NotifyTerminalPushFailed internal/agent/taskregister.go:327
TaskClaimOwner         cmd/agent/cli_tasks.go:1201
TaskRunWritebackTagged cmd/agent/cli_tasks.go:1545
LiveRowsOwnedByOthers  cmd/agent/cli_tasks_worker.go:171,185
SyncPeerStatus         cmd/agent/cli_tasks_sync.go:92
SyncPeerSessionTags    cmd/agent/cli_tasks_sync.go:163
TaskDiffPeer           cmd/agent/cli_tasks_sync.go:182
SyncTickShouldRunFull  cmd/agent/cli_tasks_sync.go:315
MustSyncFullEvery      cmd/agent/cli_tasks_sync.go:330
DefaultGitPushback     cmd/agent/cli_tasks.go:2833
WorkerTaskTag / SyncStateList / OpenWorkerProbeDB — in-package + documented
        export faces (no out-of-package caller; asserted as their contract)
```

### Protocol invariants asserted

* **forward-only rank / anchors**: a stale anchor re-commit after a real
  session cannot roll the anchor back (`TestSyncPeerSessionTagsHappyPathAnchorAndStats`).
* **never-clobber / anchor-last**: a failed transport leg leaves the anchor
  AND last-synced at 0 (`TestSyncPeerSessionTagsTransportFailureLeavesAnchorUntouched`);
  the tags write rides a SUCCESSFUL terminal write only
  (`TestTaskRunWritebackTagsRefusesIllegalStatusBeforeTagWrite`).
* **anti-reflow (from-&lt;peer&gt; exclusion)**: the session pushes the
  partitioned segment only, with the filter stamped in the patch so the peer
  mirrors it, and every push carries the exporter's node identity
  (`TestSyncPeerSessionTagsPartitionRidesThePatch`); the reflow-gate
  semantics themselves are locked by the existing `TestReflowGateTagNormalization`
  / delta suite that this file's fixtures reuse rather than duplicate.
* **claim-lock rollback + ALOHA retry**: contract asserted on the reachable
  paths — no-peers round PROCEED (never retry/yield), closed channel → zero
  verdict → the caller retries (`memory_sync_claim_cov_test.go`); the
  collision classification + rollback matrix stays with the existing
  `memory_sync_claim_test.go`. The unreachable timeout→RETRY leg is a filed
  testability bug (below).
* **reconciliation beats increments (③)**: the anchor guard fails CLOSED when
  it cannot compare the pending sets (`TestSyncAnchorGuardFailClosedOnUncomparablePeer`).
* **stale-branch resurrection guard**: `DefaultGitPushback` skips an
  already-integrated branch, leaves server master byte-identical, still
  pushes the unmerged deliverable, and is a no-op when nothing is unmerged
  (`TestDefaultGitPushbackSkipsIntegratedPushesUnmerged` and siblings).

## Not Done / Deliberate gaps (honest list)

1. **`claimLockWait` timeout→RETRY** — not covered: `claimLockWait` is a
   **const** (~35s), so the assertion would need a real 35s sleep (forbidden
   by the determinism rule). Filed: `01a08c35-3bb0-7476-9753-634d8ac838c6`.
2. **ssh legs with no injection seam** (`reconRemotePendingSet`, `reconRemoteRowSets`,
   `claimSSHExchange`): their happy paths are not driven in-process. The
   session path IS driven (the package's own `syncSSHExchangeHook` →
   `SyncRemoteServe`); the diff/guard paths fold the real local half against
   an injected peer catalog instead. Same filed ticket.
3. **Anchor guard's mismatch/repair-hint branch** — unreachable without ssh
   (same seam gap); its wording is asserted nowhere, deliberately.
4. **git push-back collision-rename retry** (`<name>-<HHMMSS>`) — not driven:
   reaching it needs a colliding non-ancestor branch on the peer and the
   rename name is wall-clock derived. Determinism chosen over touching it.
5. **`gitPushbackLoadBranches`' 20-branch cap** — not driven (seconds of
   fixture cost for an already-named constant).
6. **`SyncStateList` lists the internal full-counter row** — observed and
   documented as a latent coupling; no caller mis-uses it today, so it is a
   devdoc finding, not a bug ticket (no reproduction).

## Verification

### Coverage (acceptance criterion)

```
$ python3 scripts/func_coverage.py --pkg internal/tools --uncovered
package                                     funcs    cov    pct  tests
--------------------------------------------------------------------------
internal/tools                                181    148  81.8%
--------------------------------------------------------------------------
TOTAL                                         181    148  81.8%

Uncovered exported funcs (by package):
  internal/tools (33):
    - AdoptionGitStatus
    - FinishCleanupReportLine
    - GitBlame
    ...
```

**0 gaps for the five named files** — filtering that list for all 16 target
names returns nothing (grep count = 0). Baseline before this work: 132/181 =
72.9% (same 16 names listed); after: 148/181 = 81.8% (+16 = exactly the
target set).

### Two-run full package suite (RAW, `go test -count=1 ./internal/tools/`)

```
=== RUN 1 ===
ok  	github.com/nick/gopi/internal/tools	340.378s
RUN1_EXIT=0
=== RUN 2 ===
ok  	github.com/nick/gopi/internal/tools	219.941s
RUN2_EXIT=0
```

Non-flaky: both runs pass, zero failures. (An earlier run of mine had two
failures: one was MY test bug — `|` is not in the peer-alias metacharacter
guard, fixed by driving only the designed guard and recording the wider gap
in the security ticket; the other was `TestBashDaemonSpawn_DoesNotLeakProcesses`,
an unrelated process-leak test that passes 2/2 in isolation and never
reproduced — consistent with the known load-sensitive E2E flake class, and it
does not fail in either of the two clean runs above.)

Other gates: `go vet ./internal/tools/` → clean;
`gofmt -l internal/tools/*.go` → empty.

### Commit

```
926c983 test(tools): cover 16 exported peer-sync faces (claim/delta/reconcile/session/git-pushback)
 10 files changed, 2115 insertions(+)
```
On branch **feat/cov-tools-sync** (worktree `~/workspace/gopi-cov-tools-sync`),
tests + devdocs only.

## Determinism

* **No network, no ssh in any new test.** The one ssh-shaped success path
  uses the package's own seam (`syncSSHExchangeHook`) pointed at the real
  `SyncRemoteServe`, executed in-process.
* **No real sleeps.** Timing-dependent branches were either avoided (as
  documented gaps) or driven through the fail-fast negative (unresolvable
  alias / nonexistent path), never a timeout wait.
* **Isolated ledgers**: every DB test pins `GOPI_MEMORY_DB` to a
  `t.TempDir()` ledger via the suite's existing helpers
  (`newDeltaTestDB` / `newDeltaTestDBClient` / `claimTestDB`), with CLIENT
  role pinned and node identity reset (`config.ResetNodeIdentityForTest`) so
  ambient developer settings cannot leak in.
* **Pinned settings**: the session tests write a sandbox `settings.json`
  under `t.TempDir()` and point `GOPI_SETTINGS` at it, so the sync whitelist
  is never read from the host.
* **git fixtures are hermetic**: local repos with pinned
  `user.email`/`user.name` and `commit.gpgsign=false`; assertions compare
  `git rev-parse` (content-derived), never timestamps; the process CWD change
  is restored in `t.Cleanup`.
* **Process-global state** (the ssh hook, CWD, env) is saved and restored per
  test; the suite runs without `t.Parallel()`.

## Bugs filed (tests-only task — no product code changed)

1. `01a08c35-3bb0-7476-9753-634d8ac838c6` (bug,testability) — peer-sync
   transport legs untestable offline: the ③ anchor guard has no injection
   seam + `claimLockWait` is a const.
2. `01a08c35-40cf-77b9-aa3a-eeb87e26f173` (bug,security) — the ssh peer-alias
   guard accepts an option-shaped alias (`-oProxyCommand=…`) across the
   harvest/claim/recon/git-pushback legs; verified live
   (`TaskDiffPeer("-oProxyCommand=x")` → `diff: ssh -oProxyCommand=x: exit
   status 255` instead of `invalid peer`). Payload carries the shared-validator
   fix (Rule 14) for all four legs.

## memory_store entries

* **#1686** (warm, conf 0.85) — "GoPi internal/tools function-level coverage:
  metric rule, reusable test fixtures, hermetic seams, and the un-testable
  spots". Topic: how to raise func-coverage in internal/tools deterministically
  and which functions cannot be made hermetic. Keywords cover
  `func_coverage.py`, `syncSSHExchangeHook`, `GOPI_ANCHOR_GUARD`,
  `claimLockWait const`, git-pushback resurrection, determinism.
* Search at task start hit #1687 (a sibling coverage slice) and #1332
  (internal/tools full-suite flake class) — both used, and #1332 was voted
  `useful` after it correctly predicted the flake/full-suite relationship.

## Workflow feedback

* **The briefing's function→file mapping held up**, but verifying it against
  the source (and reading `scripts/func_coverage.py` first) was still worth
  it: the metric is name-mention based, so writing real assertions per
  function was a deliberate choice, not a side effect of the metric.
* **The most valuable output of a coverage slice is the filed bug, not the
  green test.** Two of the 16 functions turned out to have an untestable
  invariant (transport seam / const clock), and one guard turned out to accept
  an option-shaped alias. Both would have stayed invisible if I had written
  tests that only asserted the happy path.
* **First full-suite run failed on a test of mine** because I assumed `|` was
  in the alias guard's metacharacter set. Running the guard with hostile
  input — rather than reading it — is what caught this; the fix was to assert
  only the designed guard and record the wider gap in the security ticket
  instead of widening an assertion into a wrong claim.
* **Process note**: `claimLockWait` being a const while its sibling is
  deliberately a test-shortenable var is exactly the asymmetry that makes a
  regression (incident t-1788329099) untestable — worth checking for that
  asymmetry pattern in other protocol code.
* The full `internal/tools` suite takes 220–340s and must run detached
  (job), confirming the memory note; two full runs cost ~9 minutes of wall
  clock and should be planned for, not squeezed in.

## Deliverable location

* Branch: **feat/cov-tools-sync** (commit `926c983`), shared with the main
  repo at `~/work/GoPi` via the linked worktree
  `~/workspace/gopi-cov-tools-sync` — no push needed.
* Recovery: `git show feat/cov-tools-sync:internal/tools/memory_sync_claim_cov_test.go`
  (and the four sibling `*_cov_test.go` + five `devdoc/cov-tools-sync-*.md`).
