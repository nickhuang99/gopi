<!-- doc-class: historical -->
# Design: Foreseeable Review — Branch-Backlog Audit (foresee-review epic)

Status: design accepted, implementation phase 2 (not started in this session)
Author: session 2026-08-31, peerbox
Epic: db-sync 模式 HP 本地执行→push back feat/* 是常态；merge 决定权在
gopi verify/merge 闭环（P2.5）；serve 不常驻时分支堆积 = 模块分区漂移 +
merge 冲突面随时间放大（P2.5 教训：落后 3 代 = 8 处冲突）。
Two remediation options (epic ①②) → this doc records the decision
**② serve tick 增加分支清点 → 自动 submit 'merge backlog' 任务**
with ① partially subsumed (see §4).

## 1. Verdict

**Adopt option ②** (serve-tick branch audit → auto-submit merge tasks), with
the memsync 'branch-pushed' hint (①) as an OPTIONAL future add-on, not the
primary mechanism. Rationale:

| axis | ① memsync hint on push | ② serve-tick audit |
|---|---|---|
| fires when | only when push-back happens (harvest git phase) | every serve pass |
| catches pre-existing drift | NO (branches pushed long ago, serve dead) | YES (inventory is stateless, re-derives from git) |
| duplicate suppression | implicit (one hint per push) | explicit (hash-keyed in-patch + live-row dedup) |
| peerbox 台账可见 | via sync (needs a live sync session) | direct — row lands in LOCAL ledger |
| code surface | memsync_task.go (push path) + memsync CLI | cli_tasks.go (serve tick) + tools/task_merge_backlog.go |
| failure mode | silent when push skipped (--no-push) | git failure degrades to log-only |

② is superior on the epic's own acceptance test: 造 2 分支 → 台账出现 2 条
merge 任务, regardless of whether a push/sync event ever fired (the drift
pre-exists; the audit discovers it). ① only helps for FUTURE pushes.

## 2. Mechanism (option ②)

### 2.1 Branch discovery (engine side, `internal/tools/task_merge_backlog.go`)

```go
type MergeBacklogOptions struct {
    Repo     string   // git repo to audit ("" = cwd)
    DBPath   string   // ledger override ("" = memoryDBPath())
    Submit   bool     // true = enqueue pending rows; false = report only
    // Test seam: replaces the `git for-each-ref` discovery. nil = default.
    // Same injection pattern as HarvestOptions.Fetch.
    Discover func(repo string) ([]MergeBacklogBranch, error)
}

type MergeBacklogBranch struct {
    Name       string // branch name (feat/x)
    AheadCount int    // commits master..<branch> (0 = fully merged, skip)
    LastCommit string // short sha (summary/payload provenance)
    LastDate   string // committer date, short form
}

type MergeBacklogCounts struct {
    Scanned   int      // total local branches (excluding master/main)
    Submitted int      // new pending rows created
    Duped     int      // row already live for this branch
    Skipped   int      // ahead=0 (nothing to merge)
    Failed    []string // per-branch errors
}
```

Discovery = `git for-each-ref --format=%(refname:short) refs/heads` +
per-branch `git rev-list --count master..<branch>` + `git log -1
--format=%h %cs <branch>`. `master`/`main` excluded. A branch with
ahead=0 is Skipped (already merged, nothing to do — the tombstone rows
from earlier audits stay terminal, never re-submitted).

### 2.2 Dedup — the load-bearing rule

A re-audit MUST NOT spam the ledger. Key = `merge-backlog:<branch>`:

- **live check**: any pending/claimed/in_progress row whose summary equals
  `merge <branch> into master` (or tags carrying `merge-backlog` AND the
  branch name in summary) → duped, skip.
- **terminal check**: a completed/failed/cancelled/verified row with the
  same key → skip UNLESS the branch's tip commit changed since the row was
  stamped (the branch gained commits after the last merge-task closed —
  re-audit is legitimate). Tip commit is embedded in the row's payload
  (`branch tip: <sha>` line) for this comparison.
- This mirrors memsync's hash-gate (payload hash → duped) but keys on
  branch+tip instead of summary+payload bytes.

### 2.3 Submitted row shape (one row per unmerged branch)

```
summary:  "merge feat/crash-dump into master"
payload:  "branch: feat/crash-dump\n
           tip: fd14e8d\n
           ahead: 1\n
           last_commit_date: 2026-08-30\n
           Task: review the branch diff vs master, run scripts/safe-merge.sh
           <branch>, then merge. On conflict: report, do not force."
tags:     "merge-backlog,from-audit"
source:   "auto"
repo:     the audited repo (deliverable.repo stamp — M4.8 verify path)
priority: 5 (default; NOT bug — merge is hygiene, not a defect)
```

Tags derive from branch prefix: `fix/*` → prefix tag `merge-fix`,
`feat/*` → `merge-feat` (filterable partition, mirrors epic's 模块分区
concern — the ledger keeps branches searchable by intent).

### 2.4 Serve tick integration (cmd/agent/cli_tasks.go)

`runTaskServe` gains one pass step, AFTER serveStaleSweep, BEFORE the
dispatch attempt (same pass-step pattern as the sweep — M4.8 ①'s no-daemon
posture):

```go
if n := serveMergeBacklogAudit(); n > 0 {
    fmt.Printf("[serve] [merge-backlog] submitted %d merge task(s)\n", n)
}
```

Throttle: the audit is a git-query burst (one for-each-ref + N rev-list
calls). Throttled identically to serveStaleSweep:

```go
const serveBacklogThrottleMin = 15 * time.Minute  // Rule 1 named const
var serveBacklogLastTick time.Time
func serveMergeBacklogAudit() int {
    now := time.Now()
    if !serveBacklogLastTick.IsZero() &&
        now.Sub(serveBacklogLastTick) < serveBacklogThrottleMin {
        return 0
    }
    serveBacklogLastTick = now
    n, err := tools.AuditMergeBacklog(tools.MergeBacklogOptions{Submit: true})
    if err != nil {
        fmt.Fprintf(os.Stderr, "[serve] merge-backlog audit: %v\n", err)
        return 0
    }
    return n.Submitted
}
```

Env kill-switch (rule ② single-source): `GOPI_MERGE_AUDIT_INTERVAL` seconds,
0/absent = the 15min default; a NEGATIVE value disables the audit entirely
(an operator who wants zero git bursts in serve sets -1). Parsed in ONE
place next to syncIntervalFromEnv's pattern (cli_tasks_sync.go).

Cost: one `git for-each-ref` + N cheap local `rev-list --count` — sub-100ms
on the current 5-branch repo. No network. Throttled to ≤4/hour.

### 2.5 CLI/tool face

- `gopi task audit-merges [--repo PATH] [--db PATH] [--submit]` —
  manual/dry-run entry. Default = report-only (Submit=false, prints the
  table); `--submit` enqueues. Keeps the serve behavior inspectable.
- NO new agent tool (Rule 3) — task_submit already covers the LLM path;
  the audit is infrastructure, not an LLM-facing tool.

## 3. Acceptance (epic's 实测 gate)

E2E script (scripts/e2e-merge-backlog.sh, sandbox repo):

1. `git init /tmp/mb-e2e && commit on master`
2. `git branch feat/one; git branch fix/two` (both ahead=1 via commits
   on temp checkouts)
3. `GOPI_HOME=/tmp/mb-e2e gopi task audit-merges --repo /tmp/mb-e2e --submit`
4. assert: `gopi task list --status pending | grep -c "into master"` == 2
5. re-run step 3 → assert: 0 new rows (duped=2) — idempotency contract
6. merge one branch into master → re-run → assert: that branch's row is
   skipped (ahead=0), the other still dupes (live row remains)

Unit tests (tools package): discovery injection (no git needed), dedup
live-row, dedup terminal-row-with-same-tip, re-submit-on-new-tip, master/
main exclusion, ahead=0 skip. Serve pass test mirrors
TestServeTick_StaleSweepFiresWithoutTaskRun's shape.

## 4. Option ① (memsync 'branch-pushed' hint) — deferred, not rejected

Where it WOULD go: TaskHarvest's git phase
(memory_sync_task.go, after `opts.GitPushback()` returns
BranchesPushed non-empty) → syncTaskRow carrying a summary like
`branch-pushed: <names> → <peer>` with tags `merge-backlog,hint`. It is
redundant with ②'s audit EXCEPT for the one case the audit cannot see:
the PUSH side's knowledge of what the peer already received. If peerbox's
serve is dead for a week, ① still fires at push time on HP and lands in
HP's own ledger, ready for the next sync. Cost: one more row shape in
syncTaskImport's dedup path + a push-side hook. Deferred because ② alone
satisfies the epic gate; ① is additive, low-risk, and can ship as a
follow-up without touching the audit.

## 5. Risks

- **Ledger spam on re-runs**: mitigated by the branch+tip dedup (§2.2);
  worst case a branch whose tip moves every audit cycle gets one row per
  tip change — bounded by real work happening on that branch.
- **git not present / not a repo**: discovery error → audit degrades to
  a stderr line, serve continues (degrade-don't-die, same as dispatch).
- **Row flooding via terminal re-open**: a failed merge row followed by
  a new tip → new row is CORRECT (the rework re-enters the queue), not
  spam; the reopen row's parent linkage keeps the audit trail.
- **Multi-repo (HP + peerbox both audit)**: rows are per-node (each node's
  serve audits its OWN repo); the sync channel carries them across; the
  from-node tag disambiguates. No cross-node double-merge risk: merge
  decision stays with the integrator node (asymmetric git roles, already
  the M4.12 contract).
