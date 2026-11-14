<!-- doc-class: historical -->
# Task Report: CLI Code Review — `gopi task` Dispatcher Core Verbs

Branch: `review/cli-task-core` (worktree `~/workspace/gopi-review-cli-task-core`, base 975d11e)
Scope: `gopi task list/show/archive/tag-resumed/submit/run/serve/update/verify/reopen/search`
Method: usage read → source review (cli_tasks.go 2078 lines, internal/tools/tasks.go 3204
lines, task_merge_backlog.go 348 lines) → git history (938 commits, blame on hot spots) →
LIVE E2E probes against throwaway DBs (atomic claim, double-execution, verify loop,
reopen cap, archive filter, serve lock). All findings below are verified by execution,
not speculation.

---

## P0 — double execution of an already-running task (`task run <id>`)

**Severity: P0 (data-integrity / concurrent-execution hazard).**

`TaskClaimExplicit` (internal/tools/tasks.go:2805) has two shapes:
1. `pending → claimed` CAS — RowsAffected checked, exactly-one-winner. Correct.
2. crash-recovery re-stamp: `UPDATE ... WHERE task_id=? AND status='claimed'` —
   **RowsAffected is NEVER checked.** If the row is in ANY other status
   (in_progress, completed, verified, cancelled, or nonexistent), the UPDATE
   affects 0 rows, yet the function returns `nil` (success).

Consequence: `gopi task run <already-running-id>` passes claim, then `TaskStart`
(`claimed→in_progress`) is a same-status no-op for an in_progress row
(`taskTransitionAllowed` allows status==next), so the runner proceeds to spawn a
SECOND gopi child on the same task with the same briefing.

**E2E proof** (throwaway DB):
```
$ gopi task submit "double-exec probe" --payload "payload with real content"
$ gopi task run t-... --as runner1 &          # → in_progress, child running
$ gopi task run t-... --as runner2            # ← 25s later:
[claim-lock] round 1: proceed (双锁齐 — both sides advanced to in_progress)
running gopi with task t-... as briefing...   # SECOND child spawned
```
Two concurrent executions of one task. Only the payload guard
(`taskRunBriefing` refusal) stops a payload-less row; any payload-bearing row
double-executes.

**Fix**: shape ② must check RowsAffected and, when 0, verify the row's actual
status — only `claimed` is recoverable; `in_progress`/terminal must refuse with
"already running / already finished". The state machine gate belongs in
`TaskClaimExplicit`, not deferred to TaskStart's same-status no-op.

---

## P1 — verify briefing instructs a dead flag (`update --summary`); evidence lost

**Severity: P1 (functional — the verify loop's evidence chain is broken).**

The verify child's briefing (internal/tools/tasks.go:3062-3065) tells the child:
```
PASS → `gopi task update <id> --status verified --summary "evidence: <cmd> + tail"`
FAIL → `gopi task update <id> --status failed --summary "reason: <what failed>"`
```
But `taskFlagMember("update")` (cli_tasks.go:723) whitelists only `db|status` —
`--summary` was REMOVED by M4.5 (c84d9f0, bug056 summary-immutable). The briefing
was never updated. Even if the flag were accepted, `TaskUpdate` drops the summary
argument entirely (bug056 — no summary SET clause; the digest belongs in
task_events via `TaskUpdateWithResult`, which the CLI deliberately cannot reach).

**E2E proof** (verify child session bd7d007b — verbatim from the session JSONL):
```
bash gopi task update t-... --status verified --summary "evidence: diff ... IDENTICAL ..."   ← line 32
bash gopi task update --help 2>&1 | head -30                                                 ← line 37, confused
bash gopi task update --help 2>&1 | sed -n '30,80p'                                          ← line 41, still confused
bash gopi task update t-... --status verified 2>&1; echo "exit=$?"                           ← line 46, gave up evidence
```
The child burns 3-4 tool calls discovering the flag is gone, then writes `verified`
with NO evidence digest. The parent's `verify_evidence` stamp then reads an empty
event detail → the real output was `task t-... VERIFIED — evidence: —`.
Every verify run in production has been losing its evidence this way.

**Fix**: update the briefing to `--status verified` only (the evidence rides
task_events automatically via the status move), or — better — restore a CLI
face for `TaskUpdateWithResult` (`--result` flag) so the verifier CAN record
its evidence digest. Doc `devdoc/task-verify-loop.md` §② still describes
`--summary`-era behavior and should be corrected.

**RESOLVED 2026-09-02 (feat/fix-task-p1)**: the better option shipped —
`gopi task update <id> --status X --result "evidence|reason: ..."` now
routes to `TaskUpdateWithResult` (digest rides the append-only task_events
trail; the summary column stays immutable). The FAIL verdict's
`completed → failed` move was ALSO blocked by the local transition gate
(probe-proven "illegal status transition completed → failed") while the
M4.20-B rank design (terminal tier-3 peer, 后到覆盖) and the sync layer
already allow it — the gate now admits completed↔failed peer moves. The
verify briefing was rewritten to the current surface, and the parse/flag-
member/state-machine/serve regression tests ship with the branch.

---

## P1 — `task run`/`task serve` daemon dies on a failed run

**Severity: P1 (availability — the dispatcher stops dead).**

`runTaskServe`'s main loop (cli_tasks.go:1247) calls `runTaskRun(opts)` with the
comment "exits the process on a failed run". `runTaskRun` calls `os.Exit(1)` on
claim failure, start failure, payload refusal, child failure — **every error path
kills the whole serve daemon** (the flock is released on process exit, so a
supervisor restarting it would work, but nothing does).

A single bad row (e.g. a payload-less task, or a transient claim-lock yield) takes
down the entire dispatch loop permanently. `--once` is unaffected, but the daemon
posture "single process, strictly serial, keeps going" is violated on the first
error.

**Fix**: serve's loop must catch runTaskRun's outcome (return error instead of
os.Exit for the serve path), log it, mark/release the row, and continue to the
next tick. The writeback already exists; the daemon needs a "failed run → next
iteration" branch.

**RESOLVED 2026-09-02 (feat/fix-task-p1)**: runTaskRun was split into
runTaskRunOnce (returns error on every failure path — claim/start/lock/
payload/child) + the standalone runTaskRun wrapper (logs + os.Exit(1) for
the plain `task run` CLI contract). The serve daemon loop now calls
runTaskRunOnce and logs "[serve] run failed (continuing): ..." — a failed
row lands `failed` via the existing writeback and the loop moves to the
next tick. Regression: TestRunTaskServe_SurvivesFailedRun (hermetic child
failure via the taskChildFailTestEnv escape) asserts the row is written
failed and the --once pass returns instead of dying.

---

## P2 — `task list --status failed` renders a stale count after archive filtering

**Severity: P2 (cosmetic but misleading).**

`runTasksSubcommand` list path (cli_tasks.go:1928): TaskList renders "N task(s)
with status=..." then `FilterArchivedRows` (tools) drops `[resolved]` rows — but
the count prefix is computed BEFORE the filter.

**E2E proof**: 2 failed rows, 1 archived → output:
```
2 task(s) with status=failed:
#t-...-1  ...  [failed]  count-test-2
```
The header claims 2, one row is shown. A 1-of-2 archived view lies about the
queue size. The "No tasks found." collapse path is handled, but partial filtering
is not.

**Fix**: filter rows BEFORE counting, or have TaskList accept the archive filter
and count post-filter.

---

## P2 — `[-- <extra args forwarded to the child gopi run>]` advertised but unimplemented

**Severity: P2 (dead usage text / misleading docs).**

`tasksUsage` (cli_tasks.go:141) documents `gopi task run [<task_id>|next] ... [-- <extra args>]`
but the `run` parser (cli_tasks.go:455) rejects a second positional, and no `--`
splitting exists anywhere. Live:
```
$ gopi task run next -- --foo bar
gopi task run: empty flag
GoPi task subcommands — ...   (usage dump)
```
Either implement forwarding (child argv after `--`) or delete the usage line.
Given the child is a full gopi with the payload as briefing, the feature's value
is unclear — deletion is the cheaper fix.

---

## P2 — duplicate `serveAdoptionSweep` call in the serve pass (merge artifact)

**Severity: P2 (dead code / confusion).**

`runTaskServe` calls `serveAdoptionSweep(opts.repo)` TWICE (cli_tasks.go:1200 and
1215), both with full "Branch adoption chain" comment blocks. Blame shows the
first from 0c183cc (branch-adoption-chain) and the second from 8ab7b0c (M4.18
port) — a merge that never deleted the first. The second call is throttled away
by `serveAdoptionLastTick` within the same pass (no functional double-run), but
the duplicated block is misleading maintenance debt.

**Fix**: delete the first (lines 1195-1200 + its comment), keep the M4.18 one.

---

## P2 — magic number in the atomic-claim retry loop (Rule 1)

**Severity: P2 (Rule 1 — literal at call site).**

`TaskNextFiltered` (internal/tools/tasks.go:2367): `for attempt := 0; attempt < 10; attempt++`
— the retry cap is a bare literal. The file's own convention (taskIDMaxAttempts=10,
taskVerifyReopenMaxPending=3) is named constants. A claimed-task race that never
settles is tuned by editing a magic 10.

**Fix**: `const taskNextClaimMaxAttempts = 10` alongside the other task constants.

---

## Verified-good (no action needed)

- **Atomic claim CAS**: the `UPDATE ... WHERE status='pending'` + RowsAffected==1
  ladder in TaskNextFiltered is correct; loser loops and picks the next eligible
  row; 10-attempt bound then errors. The `--skip-tag` and payload-less filters
  walk past rows instead of head-blocking (the t-1788053263 fix is sound).
- **State machine**: `taskAllowedTransitions` / `taskStatusRank` / `taskTerminalTier`
  are a coherent single-source design. `failed→pending` correctly refused
  (usage text "may retry back to pending" is stale but the machine is right);
  `completed/failed→verified` works; `verified` is terminal. The M4.20-B
  retirement of `reopened` (append-only `-r1` reissue) is the right call and
  consistently enforced.
- **Reopen**: `-r1`/`-r2`/`-r3` suffix collision handling is correct; reason
  appended to payload; bug tag auto-added; anti-loop cap (≥3 pending refuses)
  verified live — the 4th reopen was refused.
- **scanTaskRow column alignment**: SELECT projection 20 columns == scanTask 20
  dests == scanTaskRow 20 dests. The P2.5 "15 vs 16" latent bug is fully resolved
  (M4.5 merge #1133 recipe held). Scoped test run `TestTaskVerify|TestTaskNext|
  TestTaskClaim|TestTaskSubmit|TestTaskUpdateLifecycle|TestTaskListAllAndStatusFilter`
  → `ok internal/tools 76s`; `TestParseTasks*` → ok.
- **Serve single-instance**: flock (LOCK_EX|LOCK_NB) on ~/.gopi/serve.lock with
  pid stamp + holder report works — second serve refused with holder pid.
- **Child flag isolation**: `task run`'s child gets payload on stdin with NO
  dispatcher flags leaked (the `--` gap is separate); `GOPI_TASK_WRAPPER_ID` is
  set on cmd.Env before spawn (bug057 two-row linkage correct). Verify child's
  PATH is prepended with the executable's dir (stale-binary fallback fix is in).
- **`--db` plumbing**: `--db` → `GOPI_MEMORY_DB` env → `memoryDBPath()` is the
  single resolution chain, consistently used by every verb and the spawned
  children (env inherited).
- **task_submit LLM tool vs CLI**: single `TaskSubmit` engine, two faces.
  CLI defaults source=human, tool stamps source=llm+session (anti-self-reference),
  auto rows source=auto. No drift in the core path.
- **Archive**: terminal-only, idempotent `resolved` tag, non-terminal refused —
  verified live.
- **Rule 1 hygiene**: serveIntervalDefault/serveLockFilename/taskSessionPoll*/
  remoteReportTimeout/taskVerify* consts are all named. task_merge_backlog.go is
  thoroughly constantized. Only the claim-retry `10` slips.

---

## Prioritized summary

| # | Sev | Finding |
|---|-----|---------|
| 1 | P0 | `TaskClaimExplicit` shape ② ignores RowsAffected → `task run <id>` double-executes an in_progress task (E2E-proven) |
| 2 | P1 | Verify briefing tells the child to use the removed `update --summary` flag → evidence lost (E2E-proven, 3 wasted child tool calls) |
| 3 | P1 | Serve daemon os.Exit(1)s on the first failed run — the dispatch loop dies permanently |
| 4 | P2 | `list --status failed` stale count after archive filtering (E2E-proven "2 task(s)" / 1 row) |
| 5 | P2 | `[-- <extra args>]` advertised but unimplemented (E2E-proven "empty flag") |
| 6 | P2 | Duplicate `serveAdoptionSweep` call in the serve pass (merge artifact, blame-proven) |
| 7 | P2 | Magic number 10 in the claim-retry loop (Rule 1) |

No code changes were made in this review (docs-only task). All findings are
reproducible with the E2E commands listed above.

## Verification
- Build: `go build -o bin/gopi ./cmd/agent/` OK
- Tests: `go test ./internal/tools/ -run 'TestTaskVerify|TestTaskNext|TestTaskClaim|TestTaskSubmit|TestTaskUpdateLifecycle|TestTaskUpdateWithResultKeepsSummaryImmutable|TestTaskListAllAndStatusFilter'` → ok (76s); `go test ./cmd/agent/ -run 'TestParseTasks'` → ok
- E2E probes: fresh-DB runs of submit/run/verify/reopen/list/archive/serve-lock (all listed above)
- Commit: (see git log)
