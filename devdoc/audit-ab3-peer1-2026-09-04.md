<!-- doc-class: normative -->
# Task Report: Audit — AB3 分支 (peer1 build, memory-aware features present)

- Branch: `feat/audit-ab3-peer1-2026-09-04`
- Base: master `7098ce5` (clean except untracked devdoc/maps-visual-verify-phantom-close.md, unrelated)
- Scope: defect hunt across cmd/agent (cli_tasks, dispatch, worker, verify, sync), internal/tools (tasks, memory_sync_delta), internal/agent (loop), internal/deepseek (retry/error matrix), internal/config.

## Headline

**1 confirmed defect (P2)** + **1 low-severity display inconsistency (P3)**.
Prior review #1237's P0s/P1s were **verified FIXED on this head** (no re-report);
the peer1 build's task-family code is heavily hardened (CAS claims, 23-col
scan alignment, provenance stamping, worker-vs-daemon split).

---

## D1 (FIXED 2026-09-05) — `retryDo`'s Retry-After honoring is dead code; the doc-comment contract is false

**Files/lines:**
- `internal/deepseek/retry.go:141-146` — doc comment: `HTTP 429 / 5xx → transient: retry with backoff (Retry-After honored)`
- `internal/deepseek/retry.go:215-236` — the "HTTP error" branch that honors Retry-After
- `internal/deepseek/retry.go:186-193` — the classTransient branch actually hit on 429/5xx
- `internal/deepseek/retry.go:61-105, 225-233` — `retryAfterDelay`/`parseRetryAfter`/`retryableStatus`
- `internal/deepseek/claude_client.go:904, 1027` — both `retryDo` callers' non-200 handling
- Introduced by `5c6f5ca` (2026-08-31, error-matrix refactor)

**The bug:** `retryDo` only honors a server `Retry-After` header on the
"operation returned a raw non-200 `*http.Response`" branch (retry.go:215-236).
That branch is **unreachable**: both `retryDo` callers
(ClaudeChat at claude_client.go:904, ClaudeChatStream at :1027) return
`&sentinelAPIError{status, body}` for ANY non-200 response — never
`(resp, nil)`.

So a real 429/5xx arrives as `sentinelAPIError` → classified `classTransient`
by `classifyHTTPError` (error_matrix.go:75, `status == 429 || status >= 500`)
→ the retry at retry.go:186-193 sleeps `cfg.backoff(attempt)` (≤30 s
exponential, jittered) and **ignores the server's Retry-After**. The code's
own contract ("Retry-After honored") is therefore false for every live caller.

**Effect:** on a 429 whose Retry-After exceeds the client's backoff estimate,
the client re-hammers before the server's requested wait elapses → repeated
429s, wasted request budget, slower overall convergence. Bounded by
`MaxRetries=3` (no infinite dogpile), hence P2 not P1.

**Dead code:** with both callers returning the sentinel, `retryableStatus()`
(retry.go:225 call site), `retryAfterDelay()` (retry.go:233) and
`parseRetryAfter()` become reachable only from the unreachable branch. The
pre-`5c6f5ca` code returned `(resp, nil)` on non-200 — the branch was live
then and the matrix refactor silently orphaned it.

**Verification (root cause, per mechanism):** sentinel construction exists
only at claude_client.go:904 + :1027; `.retryDo(` has exactly those two call
sites; `classifyHTTPError` maps 429/5xx → classTransient; the classTransient
arm (retry.go:190) uses `cfg.backoff(attempt)` only — no Retry-After read.
git blame confirms the classTransient arm + sentinel unwrap were added by
`5c6f5ca`; the Retry-After branch (lines 215-236) predates it (cc0d68ad,
0f40e558).

**Suggested fix (either):**
1. In the classTransient arm (retry.go:186-193): `delay := max(cfg.backoff(attempt), retry-after of apiErr)`. Problem: the sentinel carries only `{status, body}` — the `Retry-After` header is dropped at claude_client.go:904/1027. So extend `sentinelAPIError` to carry the header value, OR
2. read `Retry-After` in the operation closures and fold it into the sentinel.

Note: the streaming caller (ClaudeChatStream) intentionally fails fast on
4xx for the agent-layer fallback — the fix must keep that contract (streaming
429/5xx stays classTransient, so it is unaffected by the minimal-core
degradation path).

### Fix shipped (2026-09-05)

Implemented fix option 1+2 combined, in this task:

- `sentinelAPIError` gained a `retryAfter int` field (delta-seconds);
  both operation closures (ClaudeChat claude_client.go:904, ClaudeChatStream
  :1027) now set `retryAfter: parseRetryAfter(resp)` when constructing the
  sentinel — the header is no longer dropped before retryDo sees it.
- `retry.go` gained one shared helper `retryDelay(retryAfterSecs, attempt)`
  = `max(backoff(attempt), retryAfter)`, Retry-After capped at `MaxDelay`
  (mirrors the pre-5c6f5ca retryAfterDelay cap — a multi-hour rate-limit
  window must not stall the bounded retry loop). The classTransient arm
  (retry.go:215) now sleeps `cfg.retryDelay(apiErr.retryAfter, attempt)`
  instead of `cfg.backoff(attempt)` only.
- Single source of truth: the still-reachable raw-resp branch's
  `retryAfterDelay` now delegates to `retryDelay`, and the stale comment on
  the doc contract ("Retry-After honored") is now TRUE for the live
  sentinel path.
- Streaming contract preserved: 4xx still fails fast (deterministic class,
  returned before the retry arm); only 429/5xx timing changed.
- Regression tests (`internal/deepseek/retry_after_regression_test.go`):
  `TestRetryDelay_MaxSemantics`, `TestParseRetryAfter_Formats`, and a timed
  end-to-end `TestSentinel429_RetryAfterHeaderHonored` (a real 429 with
  `Retry-After: 2` on a 1ms backoff measured ~2s wait — pre-fix it was ~1ms).
  All pass.

---

## D3 (P3, display-only) — `task list` header count is stale when archived rows are filtered

**Files/lines:**
- `cmd/agent/cli_tasks.go:2488-2492` — CLI list: `TaskList` then `FilterArchivedRows`
- `internal/tools/tasks.go:1230-1239` — TaskList renders `N task(s):` BEFORE any filter
- `internal/tools/tasks.go:7585` — `FilterArchivedRows` drops `[resolved]` lines

**The bug:** `TaskList` counts every matching row (including archived ones)
and renders the header `"%d task(s):"`. The CLI's default view (no `--all`)
then runs `FilterArchivedRows`, which drops `[resolved]` rows but **leaves the
header count unchanged** on a partial filter. E.g. 5 rows where 2 are
archived → `"5 task(s):"` followed by 3 rows. Only the fully-filtered case
is handled (degrades to "No tasks found.").

Already documented in review #1237 (`review-cli-task-core.md:131`), **still
live on master**. Genuine but cosmetic — the header is a summary line; each
row remains truthful.

---

## Already-fixed verification (from review #1237 / earlier audits — NOT re-reported)

These were the headline findings of the prior review passes and are verified
**fixed** on this head:
- P0 TaskClaimExplicit shape-② RowsAffected check — fixed.
- P1-1 worker os.Exit(1) on failed child run — **fixed**: workerRound calls
  `runTaskRunOnce` directly + logs + continues (cli_tasks_worker.go:214-241);
  `runTaskRun` (os.Exit wrapper) survives only for the standalone CLI verb
  (cli_tasks.go:2450) and no longer runs inside serve or the worker.
- P1 serve daemon dies on failed run — fixed (`runTaskRunOnce` + continue).
- runTaskServe = runTaskMerger alias (task execution moved to the worker —
  the serve-worker split); merger os.Exit(1)s only on bad `--every`/lock.
- 23-column scanTaskRow/scanTask alignment; TaskStart proper CAS; run_cmd
  provenance mirrored to auto rows (324b54b).

## Scan coverage notes (non-defects, recorded for context)

- **writebackOutcome report-back** (cli_tasks.go:1413): `ssh <report-to>
  gopi task update <id> --status X` sends a bare status poke — no
  `--result`/`--reason` — so the center row's report step does not carry the
  run digest/why. Assessed **not a defect**: the child's own auto-mirror row
  (bug057) syncs back full evidence, the center converges via db-sync, and
  dispatch.go documents this status-poke shape. The local writeback always
  keeps the full digest first.
- **runTaskVerify / --result evidence writeback**: routed through
  `TaskUpdateReason` (bugfix-task-p1) → digest lands in task_events, never
  over the immutable summary (bug056). Sound.
- **Heartbeat/zombie sweep**: `maybeTaskListSweep` runs the stale sweep
  throttled (once per taskListSweepThrottleMin) from the list read path —
  reads stay cheap; stale-event tests present.
- **merge-backlog / runTaskAuditMerges / TaskArchive**: consistent with the
  safe-merge + archive-tag rules.

## Verification
- Build: `git log` head clean; no code changed by this audit (report-only).
- Tests: not run (docs-only change); no code touched.
- Commit: `git add devdoc/audit-ab3-peer1-2026-09-04.md && git commit`

## Not Done
- No P0/P1 beyond D1 (retry/Retry-After) surfaced in the scanned surfaces.
- Candidate (b) (report-back drops result/why) deliberately NOT reported as
  a defect — see scan-coverage note. D3 (count header) left unfixed (docs-
  only audit; fix belongs to a code task).
