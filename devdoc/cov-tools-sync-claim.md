<!-- doc-class: historical -->
# Task Report: function-level coverage for the peer-sync claim faces (internal/tools)

Epic: `feat/cov-tools-sync` — raise function-level test coverage for the
internal/tools peer-sync subsystem (16 uncovered exported funcs across five
files). This report covers the **claim** file's 5 functions.

Metric: `python3 scripts/func_coverage.py --pkg internal/tools --uncovered`
(counts a func covered only when its NAME appears in a same-package
`*_test.go` file). Package before: 132/181 = 72.9%. After all five files:
148/181 = 81.8%, zero of the 16 target names listed.

## Completed

New test file: `internal/tools/memory_sync_claim_cov_test.go`.

| Function | Definition (code_search) | Tests added | Boundary / error cases named |
|---|---|---|---|
| `WaitClaimLockVerdict` | internal/tools/memory_sync_claim.go:224 | `TestWaitClaimLockVerdictReturnsFirstVerdict`, `TestWaitClaimLockVerdictLockRoundAnswersProceedSingleNode`, `TestWaitClaimLockVerdictClosesImmediately` | verdict carried verbatim incl. `Lines`; single-node round → PROCEED (never retry/yield); closed channel → zero verdict instantly |
| `NotifyTerminalPushFailed` | internal/tools/memory_sync_claim.go:916 | `TestNotifyTerminalPushFailedExportedWrapper`, `TestNotifyTerminalPushFailedAsyncIsSilentWithoutSubscriber` | subscribed → payload (event/task/error text/status); unsubscribed → no delivery (async or not) |
| `TaskClaimOwner` | internal/tools/memory_sync_claim.go:922 | `TestTaskClaimOwnerOverrideAndDefault` | override trimmed and wins; `""`/spaces/tab-newline → `hostname:pid` (never empty) |
| `LiveRowsOwnedByOthers` | internal/tools/memory_sync_claim.go:934 | `TestLiveRowsOwnedByOthersCountsForeignHoldersOnly` | empty ledger → 0; foreign holder → 1; self (bare host AND `host:pid`) → excluded; pending/completed/unclaimed → excluded |
| `OpenWorkerProbeDB` | internal/tools/memory_sync_claim.go:957 | `TestOpenWorkerProbeDBResolvesLedgerAndFailsSoft` | same env-resolved ledger as the tools layer (write visible through `taskRowByID`); closer really closes; foreign file → `(nil, nil, err)` |

Callers (usages verified, non-test): `WaitClaimLockVerdict` →
`internal/tools/memory_sync_claim.go:658` (inside `RunClaimLockProtocol`);
`NotifyTerminalPushFailed` → `internal/agent/taskregister.go:327`;
`TaskClaimOwner` → `cmd/agent/cli_tasks.go:1201`;
`LiveRowsOwnedByOthers` → `cmd/agent/cli_tasks_worker.go:171,185`.
`OpenWorkerProbeDB` has no in-tree production caller (test/introspection seam
by its own doc comment) — that is the contract asserted, not a defect.

## Invariants asserted

* **Slow-peer ≠ abandon / ≠ double-run**: the claim-lock wait's contract is
  that a round which does not answer re-acquires (retry). Asserted for the
  two hermetically reachable paths — no peers declared (PROCEED) and a
  closed channel (zero verdict → the caller's switch retries).
* **Single-node deployments stay runnable**: a no-`sync_peers` round reports
  PROCEED, so a one-box install cannot stall on a lock nobody contests.
* **Worker-exit predicate is about OTHERS**: self-held `in_progress` rows in
  both stamp shapes (`host`, `host:pid`) never count, so a drained worker
  does not see itself as "other holder active".
* **Worker provenance is additive**: the writeback tag union preserves the
  dispatcher's tags (asserted in the delta file).

## Not Done / Deliberate gaps (recorded, not silently skipped)

1. ~~**The claim-lock timeout → RETRY branch is NOT covered.**~~ **CLOSED
   by `feat/sync-transport-seams`** (devdoc/sync-transport-seams.md):
   `claimLockWait` is no longer a const — it is a documented
   test-shortenable **var** (production default unchanged: `syncSSHTimeout`
   + `syncSSHConnectTimeoutSeconds` + `claimLockRoundSlack` = ~35s), exactly
   the `claimLockTimeout` precedent. `TestWaitClaimLockVerdictTimesOutToRetry`
   now asserts the expiry → `ClaimLockRetry` ("lock round wait exceeded")
   leg with a 30ms shortened var — no sleep, whole call < 2s — plus a
   boundary test proving the shortened var still reads an arriving verdict.
2. **The ALOHA backoff sleep** (`RunClaimLockProtocol` round > 1 →
   `time.Sleep(ClaimLockBackoff(round))`) is not driven through a test, for
   the same reason: the backoff durations are package vars but the full
   exhaustion path costs real wall-clock seconds and needs peer collisions
   that only the ssh leg can produce. The collision CLASSIFICATION
   (`classifyClaimLockRound`) is already covered by the existing
   `memory_sync_claim_test.go` matrix; this file adds the exported faces
   around it.
3. `OpenWorkerProbeDB`'s success path is asserted through the shared
   `taskSyncDB` contract rather than by constructing a caller: a synthetic
   probe caller would only re-implement the assertion.

## Verification

- Build/vet: `go vet ./internal/tools/` → clean (no output).
- Target runs (raw):
  `go test -count=1 -run 'TestWaitClaimLockVerdict|TestNotifyTerminalPushFailed|TestTaskClaimOwner|TestLiveRowsOwnedByOthers|TestOpenWorkerProbeDB' ./internal/tools/`
  → `ok github.com/nick/gopi/internal/tools 0.167s`
- Two full-suite `go test -count=1 ./internal/tools/` runs: see the epic
  report `devdoc/cov-tools-sync-summary.md` for the pasted RAW output (the
  suite is ~590s, run detached).
- Coverage: `python3 scripts/func_coverage.py --pkg internal/tools
  --uncovered` no longer lists any of the 5 names; package 148/181 = 81.8%.

## Determinism

No network, no sleeps, no live API. The only non-injected transport touches
are deliberate fail-fast negatives: a nonexistent peer alias in
`TestSyncAnchorGuardFailClosedOnUncomparablePeer` is in the reconcile file,
not here. This file's db work is an isolated `GOPI_MEMORY_DB` temp ledger per
test (`newDeltaTestDB`), which the delta suite's existing helpers pin.

## Bugs filed (tests-only task — product code untouched)

* **01a08c35-3bb0-7476-9753-634d8ac838c6** (tags: bug,testability) — "peer-sync
  transport legs are untestable offline: ③ anchor guard has no injection seam +
  `claimLockWait` is a const". Payload carries the two blockers, the incident
  reference (t-1788329099) and the suggested seam/var fix.

## Workflow feedback

* The `func_coverage.py` metric is *name-mention* based, so a test that
  merely references a function name counts — I deliberately wrote assertions
  per function (behaviour + boundary + error) so the number reflects real
  coverage, and pasted the raw `go test` output so a reviewer can see the
  assertions ran.
* Discovering "this leg is not testable without a 35s sleep" is exactly the
  finding worth reporting: it converts an untested invariant into a filed
  bug instead of a fake test.
