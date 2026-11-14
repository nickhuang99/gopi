<!-- doc-class: normative -->
# Testsuite Design Report: last-2-days feature coverage extension

Branch `feat/testsuite-coverage` (worktree `~/workspace/gopi-testsuite`, based on master `6b1cef3`).
Commit: `a6c0f07`. Date: 2026-08-31.

## 0. Scope method

Read 12 devdocs from the last 2 days (memsync family ×5, task family ×4,
error matrix, adhoc-1214, test-reds-fix, testsuite-skeleton) and mapped
each feature → its shipped tests → its uncovered seams. Per new feature
the audit found coverage was generally GOOD (each feature branch shipped
its own regression tests); the gaps were mostly **helper-level** and
**future-refactor fences** — exactly what a testsuite extension should add.

## 1. Coverage matrix (feature → shipped tests → added here)

| Feature (devdoc) | Shipped tests | Gap found | Added in this branch |
|---|---|---|---|
| task-id-collision (`memory_sync_task.go`) | 3 E2E (import relabel / terminal refuse / harvest relabel) | the `taskIDRelabel` helper itself had ZERO direct unit tests (sanitize, escalate -1..-9, exhaust, exists-error propagation) | `TestTaskIDRelabel*` ×4 (testsuite_extension_test.go) |
| task-submit-model (`cli_tasks.go`) | `TestTaskUpdateWithResultBackfillsModel` (tools side) | `newestSessionJSONLIn` (package main) — the newest-window selection algorithm — untested | `cmd/agent/newest_session_test.go` ×3 (window / parent-exclude / pre-window + zero-window + missing-dir) |
| M4.12 repo guard (`tasks.go`) | 5 cases | nil-task path, banned token in PAYLOAD-only, case-insensitivity contract unpinned | `TestTaskRepoGuard*` ×3 added |
| error matrix + minimal core (`error_matrix.go`, `minimal_core.go`) | 11 tests incl. matrix + degrade E2E | (a) no "vendor code in body must not flip class" fence — the 1214 tunnel-vision could return as a body-sniff; (b) no whitelist-leak guard — a future optional field added to the `MinimalCore` literal would silently enter the degrade path | `TestMinimalCore_NewFieldLeakGuard`, `TestDegradePath_VendorMarkerBodyStillDegrades`, `TestClassifyHTTPError_VendorShapesStaySemantic` (future_field_regression_test.go) |
| anti-reflow export gate (`memory_sync_reflow_test.go`) | 3 tests | (a) `syncSelfTag` identity rule (override + hostname fallback) untested directly — a drift between the two exporters' identity would silently re-open the gate; (b) dual-tagged (self+peer) row contract unpinned | `TestSyncSelfTagIdentityOverrideAndFallback`, `TestReflowGateSelfOnlyBothTagsExported` (reflow_extension_test.go) |
| memsync version gate | `TestSyncPatchVersionCompatSemantics` + E2E brew refusal | the DELTA envelope face (`SyncDeltaEnvelopeFromPatch` with a newer Version) untested | `TestDeltaEnvelopeNewerVersionRefused` |
| import-txn, priority M4.11, bidir E2E | good coverage (two-process E2E, 6 priority tests, 4 epic E2E) | — | none needed; verified green |

## 2. Not Done (honest)

- `newestSessionJSONLIn` lives in package main; Rule 3 (minimal tool/surface)
  forbids moving it to internal/ — so its tests live in `cmd/agent/`, a
  documented exception, not a refactor.
- The 3 known environment-dependent reds remain out of scope (they
  reproduce on clean master, root-caused in devdoc/preexisting-test-reds-fix.md):
  `TestMemorySearchNonBlocking` (tmpdir race under concurrent suites — passes
  serialized), `TestEnsureTagsRegeneratesForNewerTS` (ctags JS/TS env).
- No new tests for `testsuite/` E2E skeleton modes — out of this task's scope.

## 3. Potential issues noted while reading source (for the record, NOT fixed)

1. **`taskIDRelabel` sanitization asymmetry** (memory_sync_task.go:416):
   all-illegal node "!!!" sanitizes to "_" ×len ("n___"), while the empty
   string falls back to "node". Two different degraded forms — cosmetically
   inconsistent, functionally fine (both unique). Test pins actual behavior.
2. **`TaskRepoGuardWarnings` is substring-based** (`strings.Contains(hay, tok)`,
   tasks.go:1987): "vcmirror" warns. The devdoc says "token", the code is
   substring — a defensive over-warn is acceptable (advisory only), but the
   doc/code wording should be reconciled. NOT changed (behavior change is a
   product decision).
3. **`minimalCoreDumpDir` default writes to shared `/tmp/gopi-dumps`** —
   concurrent agents on one host share the dump dir; dumps accumulate
   unbounded (no rotation). Suggest a per-user subdir or TTL sweep later.
4. **`newestSessionJSONLIn` ties are broken by ReadDir order** on equal
   modtimes (two sessions created the same second) — the parent-exclusion
   flag mitigates the common case; documented, not changed.
5. **Reflow gate is self-absolute**: a row tagged by BOTH self and peer
   (genuinely relayed work) never re-exports even though the peer tag
   carries legit status-forward. Current 2-node topology is safe; a 3-node
   relay topology would need the dual-tag exception. Pinned by test so a
   future change is conscious.

## 4. Verification

- Build: `go build ./...` OK; `CGO_ENABLED=0 go build ./cmd/agent/` OK; `go vet` clean.
- New tests: tools 11 + deepseek 3 + cmd/agent 3 = 17 new test funcs, all PASS.
- Full suite: `go test ./internal/tools/ ./internal/deepseek/ ./cmd/agent/`
  → deepseek ok, cmd/agent ok, tools ok on a serialized run
  (TestMemorySearchNonBlocking passes standalone/serialized; the earlier FAIL
  run was the tmpdir race documented in memory #1058 / preexisting-test-reds-fix).
- Commit: `a6c0f07` on `feat/testsuite-coverage` (worktree-shared with main repo).

## 5. Recovery

```
git -C ~/work/GoPi show feat/testsuite-coverage:devdoc/testsuite-extension-report.md
git -C ~/work/GoPi log feat/testsuite-coverage --oneline -2
```
