<!-- doc-class: historical -->
# Single-Node Refactor — Master Verification + Acceptance-1 Gap Closure (2026-09-02)

Branch: `feat/singlenode-verify-audit` (worktree `~/workspace/gopi-singlenode-verify`)
Epic: single-node workflow refactor — code review + refactor so gopi's runtime
matches SINGLE-NODE mode declared in settings.json.

**Finding up front**: the epic's refactor was ALREADY executed and merged to
master via `feat/singlenode-refactor` (commits 4988df1→1d76143, all ancestors
of master through the `feat/memory-assets-sync` merge a91dace). Settings are
cleaned (`sync_peers: []`, `compute_nodes: []`). This audit verifies every
acceptance criterion against CURRENT master (`fa8d5ff`), and closes the ONE
coverage gap found: the serve-boundary acceptance-1 regression test.

---

## 1. Master carries the full refactor (evidence)

| Commit | Content | In master? |
|--------|---------|-----------|
| 4988df1 | code-review report (per-machinery classification) | ✅ ancestor |
| 9482db2 | `config.HasSyncPeers()` topology gate | ✅ ancestor |
| cb7b329 | serve git-sync pass pure no-op + regression test | ✅ ancestor |
| 5bcc9ea | single-node claim = local CAS only | ✅ ancestor |
| 34ac572 | config-shape doc (`devdoc/singlenode-config.md`) | ✅ ancestor |
| bc2858d | HasSyncPeers settings-file test cases | ✅ ancestor |
| 1d76143 | task report (`tasks/singlenode-refactor-report.md`) | ✅ ancestor |

Settings on this machine (peerbox, `~/.gopi/settings.json` → `.ds_settings.json`):
`compute_nodes: []`, `sync_peers: []`, `role`: absent — the single-node shape.

## 2. Acceptance-criterion audit (current master)

### A1. `task run next` with empty topology = no ssh, claim local-only — **code present, test gap CLOSED by this branch**

Code: `cmd/agent/cli_tasks.go` `runTaskRunOnce` — `if !config.HasSyncPeers()`
prints `[claim-lock] single-node: no sync peers declared — local claim only
(no remote leg)` and SKIPS `RunClaimLockProtocol` entirely (takes precedence
over `GOPI_SKIP_CLAIM_BROADCAST`). Tools-level no-op paths (defense in depth):
`StartClaimLockBroadcast` Proceed-on-empty-peers, `PushTerminalTaskSync`
vacuous-nil.

Pre-existing tests (tools-level): `TestRunClaimLockProtocolNoPeers`
(memory_sync_claim_test.go:383), `TestStartClaimBroadcastEmptyPeers` (:437),
`TestPushTerminalTaskSyncNoPeersIsSuccess` (:528).

**Gap found**: NO serve/CLI-boundary test drove the actual `!HasSyncPeers()`
branch in `runTaskRunOnce` — the existing serve tests used the
`GOPI_SKIP_CLAIM_BROADCAST=1` env escape, which exercises a DIFFERENT branch
(5bcc9ea shipped code with no direct regression test at its call site).

**Closed by this branch**: `TestRunServeClaimLocalOnlyWithNoPeers`
(cmd/agent/singlenode_serve_test.go:75) — drives a full `runTaskServe` pass
with empty topology (no env escape), asserts:
1. the single-node gate print fires (proving the `!HasSyncPeers()` branch,
   not the env kill-switch);
2. the claim + hermetic-child writeback completes the row terminal.
Hermetic child escape (`taskChildFailTestEnv`) keeps the run child-free; the
claim path is real. PASS (7.6s).

### A2. Serve tick with empty topology: no fetch/dispatch/remote-sync; keeps stale+adoption+merge-backlog — **verified**

- `TestServeGitSyncSweepNoPeerIsPureNoOp` (singlenode_serve_test.go:24):
  sweep returns WITHOUT advancing its throttle clock (provably top-of-function
  no-op). PASS.
- `TestServeGitSyncSweepWithPeerAdvancesClock` (:49): declared peer advances
  clock — the gate keys on HasSyncPeers. PASS.
- Dispatch: `runDispatchOnce` (cmd/agent/dispatch.go:374) — `ResolveComputeNodes`
  empty → returns `(false, nil)`; serve runs locally.
- Pass order in `runTaskServe` (cmd/agent/cli_tasks.go:1278+): stale sweep →
  adoption → finish-cleanup → merged-cleanup → adoption(dup, throttled no-op)
  → branch-scan → git-sync (**no-op w/o peers**) → merge-backlog audit →
  merge-exec → dispatch (no-op w/o nodes) → claim+run. Matches epic §6 exactly.

### A3. Client push-back pushes only its own branch — **verified (2252 regression carried)**

`task_sync_git_pushback.go` `gitPushbackPlan` skips already-integrated
branches. Tests on master: `TestPushbackSkippedMergedStaleBranchNotResurrected`
(task_sync_git_pushback_test.go:101), `TestPushbackNoBranchPushesNothing`
(:164), `TestPushbackCollisionRenamesAndSucceeds` (:176) + git-pushback
harvest tests. Targeted run: PASS (18.5s tools subset incl. these).

### A4. Merged-branch cleanup + worktree remove + rebuild loop — **verified (9004 regression carried)**

`serveMergedCleanupSweep` (cli_tasks.go:1518) → `tools.MergedCleanupSweep`
(task_merged_cleanup.go:105), role-independent serve pass, `GOPI_CLEANUP_INTERVAL`
(default 15min). Tests: `TestMergedCleanupRemovesFrozenPair` + 8 siblings in
task_merged_cleanup_test.go (real-git fixture: merge feat→master THEN
worktree-add — the exact frozen shape), assert worktree gone + branch gone.
The merge lander itself: `serveMergeExecSweep` (TestMergeExecCleanMerge /
TestMergeExecCleanMergeRepoIsWorktree), adoption (TestAdoptionCleanBranchAdopts),
merge-backlog audit. "Rebuild bin/gopi" remains an operator step after master
pull (nick's client loop: pull → rebuild) — not in-code machinery; the
9004/2252 fixes + tests are the merge→cleanup half the epic scoped to code.

### A5. Memory-assets sync path untouched — **verified**

No commit in the refactor touches `internal/tools/memory_sync.go`,
`memory_sync_session.go`, `cmd/agent/cli_memsync.go`, `cli_memory_sync.go`.
The memsync subcommand + `memory_sync_assets.go`/`memory_sync_procedural.go`
path builds and its tests exist on master (cli_memsync_test.go,
cli_memory_sync_test.go incl. `TestMemorySyncE2EBidirectional`).

### A6. `go test ./internal/... ./cmd/agent/` green — **verified for the changed package**

- cmd/agent full suite (worktree, includes the new acceptance-1 test):
  `go test ./cmd/agent/ -count=1 -timeout 600s` → **PASS (454.5s, job-2 exit 0)**.
- cmd/agent on master (job-1 leg): **PASS (237.5s)**.
- internal/config full suite: PASS (0.13s).
- internal/tools: PRE-EXISTING full-suite hang in `TestImportReRunIsIdempotent`
  (memory_sync_task_dedup_test.go:179) — tripped at 900s (job-1 concurrent),
  900s (job-4 SOLO), and 1800s (job-5 SOLO), always with the same
  connectionOpener goroutine dump. The test PASSES alone in 3.8s (job-3).
  **NOT caused by this branch**: it touches ZERO internal/tools code; the
  hang reproduces identically on master fa8d5ff (job-4, job-5) and pre-rebase
  master 9b02ec9 (job-1). Full-suite-order interaction (a prior test leaving
  a shared sql.DB/global in a state that deadlocks this one) is suspected —
  submitted as a bug task for dedicated investigation. Acceptance-6 for THIS
  branch's change is fully green (cmd/agent both runs + new regression test).
  **RESOLVED by the suite-hang fix landed in the same merge** (WAL-first DDL +
  per-path init locks + schema-wait on concurrent first-open, t-1788361069 —
  see devdoc/suite-hang-fix-t1788361069.md).
- `go build ./internal/tools/ ./cmd/agent/` OK (A5 memsync path compiles).
- New test + no-op tests: `go test ./cmd/agent/ -run
  "TestRunServeClaimLocalOnlyWithNoPeers|TestServeGitSyncSweepNoPeerIsPureNoOp"`
  → PASS (7.6s), on rebased master fa8d5ff.

### A7. devdoc committed — ✅ (4988df1 review, 34ac572 config shape, 1d76143 report)

## 3. Files changed by this branch

- `cmd/agent/singlenode_serve_test.go` — adds
  `TestRunServeClaimLocalOnlyWithNoPeers` (acceptance-1 serve-boundary
  regression; the only genuine gap found in the audit).

No production code changed — the refactor is verified complete on master; this
branch closes the test-coverage gap in the single-node claim gate.

## 4. Verification

- `go vet ./cmd/agent/` OK.
- New test + no-op tests: `go test ./cmd/agent/ -run
  "TestRunServeClaimLocalOnlyWithNoPeers|TestServeGitSyncSweepNoPeerIsPureNoOp"`
  → PASS (7.6s), on rebased master fa8d5ff.
- Worktree cmd/agent full suite (with new test): PASS (454.5s, job-2 exit 0).
- Master cmd/agent leg: PASS (237.5s).
- Master internal/tools leg: pre-existing full-suite hang in
  TestImportReRunIsIdempotent — tripped 900s/900s/1800s (job-1/4/5), passes
  alone 3.8s (job-3); unrelated to this branch (zero internal/tools edits;
  reproduces on master w/o change). Submitted as a bug task — **resolved by
  the suite-hang fix landed in the same merge (t-1788361069)**.

## 5. Commit

- Commit `test(singlenode): acceptance-1 serve-boundary regression — claim
  local-only with empty topology (epic 2026-09-02)` — on branch
  `feat/singlenode-verify-audit` (head commit; worktree-shared, in main repo
  refs — recover with `git show feat/singlenode-verify-audit:devdoc/
  singlenode-master-verify-2026-09-02.md`).

## 6. Not Done / Notes

- `role: "server"` still absent from settings — deferred by the original
  refactor (recommended in devdoc/singlenode-config.md); in pure single-node
  (no peers) role is moot. Not re-opened.
- PRE-EXISTING master bug surfaced by this audit (NOT caused by this branch,
  but worth fixing): `go test ./internal/tools/` full suite hangs in
  `TestImportReRunIsIdempotent` (internal/tools/memory_sync_task_dedup_test.go:179)
  — tripped at 900s (job-1 concurrent, job-4 solo) and 1800s (job-5 solo),
  always a connectionOpener goroutine dump; passes alone 3.8s. Suspected
  suite-order interaction with a prior test's shared sql.DB. Submitted as a
  bug task with repro evidence (job ids, timings, master fa8d5ff).
  **Root cause found + fixed by the suite-hang branch (t-1788361069)**:
  fresh-DB init ran 40+ DDL statements each fsyncing under rollback journal
  + synchronous=FULL — one stalled fsync blocks the whole suite for minutes.
  Fix: WAL+synchronous=NORMAL before schema DDL, per-path init locks, and a
  schema-wait window for concurrent first-open (devdoc/suite-hang-fix-
  t1788361069.md). This note is historical; the hang no longer reproduces.
- Duplicate `serveAdoptionSweep(opts.repo)` call (cli_tasks.go:1278,1306)
  left as-is — harmless throttled no-op, shared-edit-zone risk with in-flight
  fix branches.
- Master advanced (9b02ec9 → fa8d5ff, claim-lock 13min blowout fix
  t-1788329099) mid-audit; this branch rebased onto fa8d5ff cleanly — no
  overlap with the new test's domain.
