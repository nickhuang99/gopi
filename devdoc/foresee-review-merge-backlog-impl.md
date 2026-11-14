<!-- doc-class: historical -->
# Task Report: Foreseeable Review — Branch-Backlog Audit Implementation

Epic: foresee-review (design: devdoc/foresee-review-branch-backlog.md, accepted in c21d33d)
Branch: `feat/merge-backlog` (worktree `/home/user/workspace/gopi-merge-backlog`, shared with main repo)
Design doc: `devdoc/foresee-review-branch-backlog.md` §2 (engine) §2.4 (serve pass)

## Completed

### 1. Engine — `internal/tools/task_merge_backlog.go` (new)
- `AuditMergeBacklog(MergeBacklogOptions) (MergeBacklogCounts, error)`:
  - **Discovery (§2.1)**: `git for-each-ref refs/heads` (local branches,
    master/main excluded) + per-branch `git rev-list --count master..<branch>`
    + tip (`%h %cs`). Each git call has a 10s timeout (`mergeBacklogGitTimeout`);
    discovery is injectable via `MergeBacklogOptions.Discover` (test seam).
  - **Dedup (§2.2, the load-bearing rule)**: `mergeBacklogDedupCheck` —
    - live row (pending/claimed/in_progress) with summary
      `merge <branch> into master` → duped;
    - terminal row whose payload stamps the SAME tip
      (`branch tip: <sha>`) → duped;
    - terminal row with a DIFFERENT tip → NOT duped (branch gained commits
      after its merge-task closed — re-audit is legitimate).
  - **Submit (§2.3)**: one pending task per uncovered branch with
    `source=auto`, `repo=<audited repo>`, summary `merge <branch> into
    master`, tags `merge-backlog[,merge-fix|merge-feat]` (branch-prefix
    partition), payload = execution briefing (branch/tip/ahead/date +
    "review diff → safe-merge.sh → merge; on conflict report, don't force").
  - Report-only mode (default, `Submit=false`) never writes the ledger.
  - ahead=0 branches are skipped (Skipped count; tombstone rows stay terminal).
- Constants at top per Rule 1 (git timeout, summary verb, tag, tip line).

### 2. Serve pass — `cmd/agent/cli_tasks.go` + `cli_tasks_sync.go`
- `runTaskServe` tick now calls `serveMergeBacklogAudit()` after the stale
  sweep, before dispatch (design §2.4 option ②).
- `serveMergeBacklogAudit`: env-resolved cadence, 0-submitted on
  throttle/disable/error (errors logged to stderr, serve continues —
  degrade-don't-die), returns submitted count for the serve log line
  `[serve] [merge-backlog] submitted N merge task(s)`.
- `GOPI_MERGE_AUDIT_INTERVAL` (single source `mergeAuditIntervalFromEnv`,
  Rule 2): unset/0 → 900s default; negative → audit fully disabled;
  malformed → error logged, audit skipped (never a silent fallback).
- First pass always audits (fresh daemon inventories drift); later passes
  throttled by the cadence clock (`serveBacklogLastTick`).

### 3. CLI — `gopi task audit-merges [--repo PATH] [--db PATH] [--submit]`
- Full wiring: usage text, flag allowlist, `parseTasksArgs` (unknown flags
  and stray positionals rejected; new `tasksOptions.submitAudit` field),
  dispatcher case, `runTaskAuditMerges` runner.
- Output: one-line tally (`report-only`/`submit`, scanned/submitted/duped/
  skipped/failed) + a branch table (BRANCH/AHEAD/TIP/DATE) from
  `taskAuditMergesTable` — grep-friendly for scripts.

### 4. Tests
- `internal/tools/task_merge_backlog_test.go`: 9 tests on a temp ledger with
  injected discovery — report-only (no writes), submit row shape (pending +
  summary + payload facts + safe-merge instruction), live-row dedup,
  terminal-same-tip dedup, terminal-new-tip resubmit (the §2.2 escalation
  path), ahead=0 skip, discovery-error propagation, tag partitioning, live
  status classification.
- `cmd/agent/cli_tasks_audit_test.go`: env table (default/override/0/negative/
  garbage), disabled serve pass, throttle suppression, CLI arg parsing
  (flags + positional rejection), table rendering.

### 5. E2E — `scripts/e2e-merge-backlog.sh`
Real binary vs a real temp git repo (master + unmerged `feat/e2e` + merged
`feature/merged`), temp ledger, three assertions:
1. report-only lists the unmerged branch and writes 0 ledger rows;
2. `--submit` enqueues exactly one pending row `merge feat/e2e into master`
   with tags `merge-backlog,merge-feat` (and NOT one for the merged branch —
   `submitted=1`, not 2);
3. re-audit with `--submit` submits 0 and dupes 1; ledger stays at 1 row.

## Not Done
- Option ① (daemon-side periodic audit independent of serve ticks) remains
  deferred per the design doc — the serve pass covers the db-sync loop.
- No `manual` merge playbook / PA notification channel beyond the serve log
  line (design leaves that to the existing task claim flow).

## Verification
- Build: `go build ./...` — OK (CGO_ENABLED=0 build also exercised by e2e).
- Unit tests: `go test ./internal/tools/ ./cmd/agent/ -run
  'TestAuditMergeBacklog|TestMergeBacklog|TestMergeAuditInterval|
  TestServeMergeBacklogAudit|TestParseTasksArgs_AuditMerges|
  TestTaskAuditMergesTable'` — 12/12 PASS.
- E2E: `bash scripts/e2e-merge-backlog.sh` — ALL E2E PASS (3/3).
- Commit: see feat/merge-backlog (one feat commit, files added individually
  per Rule 8).

## Design notes
- The audit is **infrastructure, not an LLM tool** (GoPi Rule 3): it lives in
  `internal/tools` for ledger access but is driven only by the serve pass and
  the CLI — no tool_use routing entry.
- Terminal-vs-new-tip is the behavioral crux: without it, a branch that keeps
  receiving commits after one merge-task closure would be silently dead
  (tombstoned forever) or infinitely re-queued (no dedup at all). The payload
  tip line is the join key.
- Report table includes ahead=0 branches on purpose: a human scanning
  `gopi task audit-merges` sees the full merge posture, not just the queue.
