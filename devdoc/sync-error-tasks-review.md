<!-- doc-class: historical -->
# Task Report: Sync / Error / Task-Queue Domain Code Review (commit range 632c4c4..HEAD)

Date: 2026-08-31 · Branch: `feat/review-suite` · HEAD reviewed: `bd838bf`
Reviewer: gopi (review-suite session) · Method: memory precedents 1025 (worktree discipline), 1094 (verdict template), 1092 (inventory methodology)

## Scope & Method

- Range: `632c4c4..bd838bf` = 97 commits, 79+ Go files, ~15k+ added lines (grew from 93 commits during the review — 4 finish-cleanup commits landed mid-flight; re-established the worktree at bd838bf and re-verified everything against it).
- Diff concentrations read line-by-line: `internal/tools/memory_sync*.go` (bidir delta/session/task-sync), `internal/deepseek/{error_1214,minimal_core,error_matrix,claude_client,lasterr}.go`, `internal/agent/{crashdump,session,taskregister,taskheartbeat}.go`, `internal/tools/{tasks,task_finish_cleanup,taskzombie,tasknotify,taskheartbeat}.go`, `cmd/agent/cli_memsync.go`, `cmd/agent/cli_tasks_sync.go`.
- Coverage evidence: `go test -coverprofile` on internal/deepseek (60.7% total).

## Verdict: SHIP-QUALITY, no release blockers

The sync domain's five epic mechanics (delta protocol, forward-only anchors, never-clobber, conflict rulings, anchor-last-commit) are correctly implemented and each has a dedicated test. The error matrix is fully covered by tests. The task queue has CAS claim races and state-machine tests. Zero correctness blockers found.

## Completed (verified claims, with evidence)

1. **Build/vet clean**: `go build ./...` OK, `go vet ./...` clean on bd838bf.
2. **Full suite green** (background job-2, 694s): all 12 packages `ok`, incl. `internal/tools` 589.9s (the old 600s timeout failure is gone — was test-run ordering, not a product bug).
3. **Sync delta mechanics** — all 13 `TestSyncDelta*` + 8 `TestBidir*` PASS inline:
   - Anchor commit is forward-only (`syncStateCommit`, memory_sync_delta.go:545: `if peerAnchor < prev { peerAnchor = prev }`).
   - Server-vs-client conflict split verified: `IsServerNode()` (memory_sync_delta.go:393) — hostname-in-own-peer-list → client; empty/failed hostname → client (fail-safe, keeps legacy tag path, never loses data). The earlier concern "with empty ResolveSyncPeers both nodes claim server" is REFUTED: empty peers → loop never matches → returns true only when peers exist and exclude self... wait — with zero peers the loop body never runs and it returns **true**. That is a real edge: a node with no sync_peers configured claims to be a server. Benign in practice (no peers = no sync sessions at all, so the ruling path is unreachable), but see Finding F1.
   - Equal-anchor conflict: server writes completed + `sync-resolved` tag; client keeps row + `conflict` tag; verdict forward-migrates on next pull (server `updated_at=now` passes receive anchor). Idempotency asserted by TestSyncDeltaConflictEqualAnchor + TestSyncDeltaServerResolvesConflict.
   - Delta insert of a remote terminal row keeps `ended_at` (r.EndedAt, else UpdatedAt fallback) — the old "endedAt reset" concern is resolved in current code (memory_sync_delta.go:253-259).
   - Push cutoff uses `AfterSource - syncAnchorMarginBackdate (5s)` (memory_sync_task.go:135, delta.go:93) — clock-skew backdate margin exists and is tested (TestSyncDeltaSkewMargin).
4. **Error domain**: error_matrix fully covered (classifyHTTPError/retryableStatus/isDeterministicHTTPError all 100%); `TestDegradePath_GLM1214` pins the historical incident; 61 tests in the package PASS.
5. **Task queue**: CAS claim race test (TestTaskNextConcurrentClaimRace), state-machine illegal-transition test, liveness/stale/zombie/notify suites all green (67.7s batch run).
6. **finish-cleanup 4-step chain** (new in 4a9b604..bd838bf): branchDir resolved via `--git-common-dir` BEFORE worktree removal (fixes the live t-1788157112 exit-128); dirty-worktree gate protects live bonds; `-d` then guarded `-D`; 6/6 chain tests PASS.

## Findings (no blockers; 3 minor + 1 hygiene)

**F1 (minor, edge): `IsServerNode()` returns true when sync_peers is empty.**
memory_sync_delta.go:393-410: hostname check passes, `ResolveSyncPeers` returns nil (no env, no settings), the client-detection loop never matches, function returns true. Unreachable in practice (a zero-peer node never runs a sync session, so no conflict ruling happens), but the semantics invert silently if a future caller uses `IsServerNode()` for anything else (finish-cleanup already does — it *skips* on server, so a mis-configured server with no peers silently skips hand-off, which is the safe direction). Suggest: return false when `len(peers)==0`. Test to add: `TestIsServerNode_NoPeersIsNotServer`.

**F2 (minor, dead code): `internal/deepseek/error_1214.go` is 100% dead code with 0% test coverage.**
`5c6f5ca` ("minimal-core degradation replaces 1214 vendor special-case") removed every call site: `run1214Ladder`, `is1214Error`, `stripOptionalFor1214`, `degradeMessagesFor1214`, `guardDumpDir`, `dump1214Request`, `truncateRunes` — all 0.0% coverage, zero callers (verified by grep + cover profile). The file's own comment says "AD-HOC GUARD — temporary". Meanwhile `minimal_core.go` (the replacement) is 77-100% covered. Decision needed: delete error_1214.go (history preserved in git) or explicitly keep it as reference. Keeping dead 0%-covered code contradicts the repo's own test-discipline standard.

**F3 (minor, resilience): `syncSSHExchange` discards stderr on success paths and the ssh timeout constant pairing.**
memory_sync_session.go:194-222: on `cmd.Run()` error, stderr is included (good). But on success, stderr content (e.g. ssh MOTD/warnings) is dropped silently. Also the whole exchange is bounded by one `syncSSHTimeout` — good Rule-1 discipline — but a slow *push-apply* on the peer (internal/tools suite takes 590s under load) could exceed it; a lost session just re-syncs next tick (anchors never advance on failure), so impact is bounded. No action required; worth remembering when tuning `syncSSHTimeout`.

**F4 (hygiene): test-runtime concentration in internal/tools (589.9s full run).**
Chat tests each pay ~5-7s schema-init (measured: TestChatListQueryAndModelFilters 5.36s standalone), sync bidir E2E ~8-16s each. Full `go test ./internal/tools/` is the long pole of every CI run. Mitigations already exist (busy_timeout 15s, WAL, per-test temp GOPI_HOME). Suggest a follow-up: `testing.Short()` gate on the slowest E2E bidir tests, or schema-init memoization — but only after confirming no test depends on fresh schema per test (the isolation contract suggests they do; measure first).

## Verification

- Build: `go build ./...` OK (twice: at 4a9b604 baseline and bd838bf final)
- Vet: `go vet ./...` clean
- Full suite: job-2 = all 12 packages `ok` (694s, `go test ./... -timeout 25m`)
- Targeted green: TestSyncDelta* 13/13, TestBidir* 8/8, TestFinishCleanup* 6/6, TestMinimalCore/TestDegradePath 5/5, TestChat* 7/7, task-queue batch (dispatch/verify/recovery/stale/priority) ok 67.7s, internal/agent 348 PASS, internal/deepseek 61 PASS
- Coverage: deepseek 60.7% stmt; error_matrix 100%; minimal_core 77.8-100%; error_1214.go 0% (dead, F2)

## Not Done (honest deferred)

- The remaining plan items from the 12-task review plan (testsuite gap inventory, E2E error-matrix deep-dive, testsuite/ fixture reorganization) were partially subsumed: testsuite/ is small (4 files, all green) and the error matrix turned out fully covered. The findings above are the residual value.
- No fix commits made: all findings are advisory (no blocker justifies touching the tree mid-review). F2's decision (delete vs keep) belongs to nick.
- Worktree incident note: the original `feat/review-suite` worktree was removed by another agent mid-session (~t+1830s); re-created from the then-current master bd838bf and re-verified. Branch re-created, not orphaned — `git worktree list` confirms it.

## Commits

None made by this review (advisory only). Deliverable = this document + the branch `feat/review-suite` (currently identical to bd838bf; the worktree exists to keep the branch alive for any follow-up fix commits).

Recovery: `git show feat/review-suite:devdoc/sync-error-tasks-review.md` (after this file is committed on that branch).
