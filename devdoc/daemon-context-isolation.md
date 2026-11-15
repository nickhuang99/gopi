<!-- doc-class: historical -->
# Task Report: daemon context/lifecycle isolation (worker + merger)

Epic: *each task a resident daemon executes must start COMPLETELY FRESH — no
context, env, cwd or session leakage from the previous task or from the
daemon's own launch context — and a failing/policy-refusing child must degrade
into a patient wait (tick), never a crash loop or an infinite retry.*

Branch: `fix/daemon-context-isolation` (worktree `~/workspace/gopi-daemon-ctx`).

---

## 1. The study — what is actually true about freshness

### 1.1 Per-task freshness is structural, not a discipline

**Verified in code, not assumed.** Neither the worker nor the merger ever runs
the agent loop in-process:

| process | what it does per task | evidence |
|---|---|---|
| `gopi worker` | `workerRound()` → `runTaskRunOnce()` → `exec.Command(gopiExecutable())` | `cmd/agent/cli_tasks_worker.go:463`, `cmd/agent/cli_tasks.go:1473` |
| merger resolve | `mergerResolveConflict` spawns a child on a private worktree | `cmd/agent/merge_resolve_child.go` |
| merger verify | independent verify child, `mergeVerifyBaselineEnv` | `cmd/agent/merger_verify_child.go` |

No `internal/agent` import exists in `cli_tasks_worker.go`, `cli_merger_daemon.go`
or `merge_resolve_child.go` (grep over the package: the only `internal/agent`
importers in `cmd/agent` are `cmd/agent/main.go`, `cli_chat.go`, `cli_web*.go`,
`cli_resume.go`, `cli_tasks_bridge.go` — front/chat/resume surfaces, never a
daemon loop).

Consequences, stated plainly:

- **There is no daemon-side LLM context to explode.** The "context explosion"
  framing describes an architecture in which the daemon accumulates an agent
  conversation across tasks. That architecture does not exist here: the
  conversation belongs to the child `gopi` process, which is born per task
  (`NewSessionLogger` → a fresh UUID session dir per run,
  `internal/agent/session.go:105`) and dies with it. The daemon holds no
  messages, no summary, no KV state.
- **The tick/retry model already exists and is already the failure path.** A
  round that fails writes the row back and returns; the loop logs and continues
  (`cli_tasks_worker.go`: *"worker: run %s failed (continuing)"*). A drained
  queue sleeps `workerIdleInterval` (default 30 s,
  `GOPI_WORKER_IDLE_SEC`) and counts `idleStreak` toward
  `workerIdleExits` (default 3, `GOPI_WORKER_IDLE_EXITS=0` = wait forever);
  `--once` is the drain-exit. There is **no crash loop**: a child that exits
  non-zero is a *task* failure, handled by the writeback, never a daemon
  restart. This is the requested "patient wait instead of crash loop", already
  implemented — the epic's second half needed no code.

So the real, remaining exposure is the **isolation surface**: what a child
inherits from the process that spawned it.

### 1.2 What actually leaked (measured before the fix)

The spawn seam was `cmd/agent/cli_tasks.go` (`runTaskRunOnce`), and it built the
child's world from the *spawner*:

```go
execWorkdir := taskExecutorWorkdir()          // the DAEMON's launch directory
cmd.Env = append(os.Environ(), ...)           // the DAEMON's whole environment
```

Two leaks followed, both concrete:

**(a) Wrong project.** `internal/agent` reads `GOPI.md` from the **cwd**
(`internal/agent/loop.go:2331`), `code_search`/`keyword_search` index the cwd,
and the bash dangerous-command guard resolves relative targets against the cwd
(`cmd/agent/task_session_cwd.go`, the file's own doc comment lists all three).
A worker started in `/home/user` (or in repo B), claiming a task whose
`deliverable.repo` is repo A, therefore ran the task **under B's rules, B's
search root and B's guard targets** — the wrong rules file for the work being
done. This was invisible on the GoPi host because the operator habitually
launches the worker *from the task's own repo*, which makes the accident look
correct.

**(b) Foreign session handle.** `printLogPaths` exports
`GOPI_EXECUTOR_SESSION_ID` (the process's own agent-session handle) and
`GOPI_AGENT_SESSION` (the agent-chain anchor, valued with the exporting pid)
for **every agent process** (`cmd/agent/main.go:947`,
`cmd/agent/agent_spawn_guard.go:exportAgentSessionAnchor`). A worker launched
from an agent's shell carries both; `cmd.Env = append(os.Environ(), …)` passed
them to every executor child. That is the *same inheritance* the
agent-spawn-guard treats as its signal (`config.AgentSpawnSignature.AgentRun`
returns true on `AnchorInherited`) — i.e. every task-by-daemon child read as a
descendant of a foreign agent session, and one whose handle named a session
that was not its own.

**Not leaks** (checked, so the report does not over-claim):
`GOPI_TASK_WRAPPER_ID` / `GOPI_TASK_RUN_ID` / `GOPI_TASK_RUN_OUTCOME` are *set
for* the child by the same seam (its auto-register gate + SIGPIPE latch);
`GOPI_TASK_CHILD_SESSION` is a **parent-side read** (the child mints it);
`bash`/`job` already strip the wrapper identity from the shells *they* spawn
(`internal/tools/childenv.go`).

### 1.3 The peak-hour guard — history and what a daemon would need

Referenced by the briefing and **not on master**: it landed and was reverted.

- Landed: `bd42e827 Merge branch 'feat/peak-hour-guard'` (branch tip
  `af9575ae`, whose own message says *"does NOT build yet (2 small gaps)"* —
  `undefined: atomic` in `cmd/agent/cli_tasks_worker.go` and a dropped
  `remaining :=` binding in `cli_tasks_worker_stop.go`).
- Reverted: `9cbeb9e5 Revert "Merge branch 'feat/peak-hour-guard'"` (10:55 on
  2026-09-13, ~8 minutes after the merge), restoring `internal/config/workhours.go`
  -`internal/peakhours/`. The reverting commit is a plain `git revert`, so
  *why* is not recorded in-tree; the WIP-branch compile state is the only
  in-tree evidence bearing on it. **Honest limit: the revert's rationale is
  not recoverable from this repo** (no devdoc explains it, and the branch is
  gone — `git branch -a | grep peak` is empty); the archive tag/PR message that
  would carry it lives outside the tree.

The contract it defined, for whoever re-lands it (`git show af9575ae:<path>`):

- `config.PeakGuardExitCode = 4` (`internal/config/workhours.go:66`) — its own
  class: `2` is a usage error, `3` is the agent-spawn guard refusal, `1` is a
  failed run. A launcher/acceptance test must be able to tell "money would be
  doubled" apart from all three.
- One predicate, two callers: the start gate in `main()` (before the
  subcommand chain, so no verb escapes) and the resident loop's claim deferral
  — `workerPeakWait()` in `workerRound()`, which claims **nothing** inside a
  window (a claimed row is owned, so holding it would starve other runners)
  and returns `false` — the ordinary "empty round", which is exactly the tick
  path. The drained-wait pacer similarly sleeps to the window boundary instead
  of spinning a 1 s poll.
- Exempt: cost-free argv — a bare usage query (`isUsageQuery`), `<verb> -h`,
  and the version query (`gopi version` / `--version` / `-v`). The guard's
  subject is TOKEN cost: these provably reach no model, and help must stay
  readable from inside a window because it documents the escape hatch. The
  version query is the zero-token smoke test (epic
  peak-guard-token-free-verbs, 2026-09-14); local report verbs (`log`, `stats`,
  `code-search`, …) are deliberately NOT exempt — a per-verb token-spend
  classification would rot the moment one of them gains a model call.

**What a resident daemon needs for it** (the delta the reverted branch did not
finish): (1) the compile gaps above fixed and the branch rebased — master has
since gained `signalrec.Install` at the same `main()` anchor the guard's hunk
touched (the merge resolution of `23426eea` combined them, and that resolution
is *not* what was reverted); (2) the operator's reason for the revert taken
into account before re-landing; (3) a worker-side **deferral record** so the
idle path prints "peak hours — deferring claims for 42m" rather than the
generic drained line (the branch had this), and the count of deferred rounds
must not be mistaken for `idleStreak`'s self-exit accounting (the branch reused
the drained path, which is why its deferral returned `false`).

---

## 2. Fix A — the child runs in the TASK's repo, not the daemon's

`tools.TaskRepoFor(taskID)` (new, `internal/tools/tasks.go`) is the one reader
of "which directory does this task's work belong in": the row's
`deliverable.worktree` when the executor already created one (Rule 6a — that is
where its commits land), else `deliverable.repo`; both are existence-probed
(`liveDir`), so a worktree the close-out already released falls through instead
of sending a child into a removed directory.

`taskExecutorCwd(taskID, launchWorkdir)` (`cmd/agent/task_session_cwd.go`) is
the seam decision; `runTaskRunOnce` consumes it for `cmd.Dir` **and**
`GOPI_TASK_WORKDIR`, and latches the answer in `taskRunRepoDir`, which the
report-back **push leg** now uses as its `git -C` anchor (before: the
launcher's directory — the same wrong-project bug on the writeback side).

### 2.1 The precedence that the first cut got wrong (found by an existing e2e)

The first version resolved the cwd as *"the task's repo, else the launch dir"*.
That is wrong, and an existing acceptance test caught it
(`TestWorkerServerDeliveryRecyclesThenMergerMerges` — green on master, red on
the branch): a stable ground for the branch is part of the contract.

Root cause, in one chain:

1. The deliverable **branch backfill** reads the branch at the child's cwd
   (`taskBranchFromWorkdir` → `git -C <cwd> branch --show-current`).
2. A worker in **worktree mode** is launched *inside* the task's linked
   worktree — that directory is where its branch is checked out.
3. Rewriting the cwd to the row's `deliverable.repo` (master checked out) made
   the child read `master`, which the backfill **exempts**
   (`taskBranchExempt`).
4. No `deliverable.branch` → the close-out's recycle skips (unprotected work)
   → the worktree survives and the merger adoption gate finds nothing →
   starvation. Symptom: *"worker delivery left its worktree behind"*,
   *"merger pass landed no merge work"*.

The fix is `tools.TaskRepoForLaunch(taskID, launchDir)`: the launch directory
wins **when it is provably the task's own ground**, because then it satisfies
both jobs of the cwd at once (the child's project contract *and* the branch
authority). Ground is decided by evidence, not by an assumption about the test
fixture — and note the fixture parks its worktree as a **sibling** of the repo
(`git worktree add ~/workspace/gopi-x` does exactly this), so containment
cannot see it:

- (a) the launch dir IS the ground, or lies inside it (`pathWithin`);
- (b) it **shares the task's git repository** — one `--git-common-dir`, the
  object store every linked worktree of a repo shares (`sharesGitRepo`);
- (c) unless the row RECORDS a worktree of its own: then that is the ground,
  and a sibling worktree must not be mistaken for it (the "a worktree
  genuinely moved to the row's repo is safe NOW" case is false — writing the
  deliverable *work* into the shared repo does not move its *branch* read).

Anything else — the resident-daemon leak: a spawner launched from the
operator's shell or from an unrelated project — is not the task's ground and
the row's own repo is used instead. Nothing usable → the launch workdir, the
reported backward-compatible fallback (Rule 15).

Backward compatibility is explicit and reported: `taskCwdSourceTaskGround`
("already the task's own ground"), `taskCwdSourceRepo` ("deliverable repo") or
`taskCwdSourceLaunch` ("no deliverable repo ground") — one line on the round's
stderr in every case. A silent fallback is the defect class this epic removes.

## 3. Fix B — the child gets a fresh execution context

`tools.DaemonContextEnvVars` (exported constant list, Rule 14) =
`GOPI_EXECUTOR_SESSION_ID` + `GOPI_AGENT_SESSION`. For want of a reverse
import, `config.DaemonEnvLeakVars` mirrors it **loudly**: a test asserts the two
lists agree name-for-name (Rule 14 forbids silent mirrors; a test makes the
mirror a build failure rather than a hole).

`cmd/agent/task_child_env.go` owns the seam: `taskChildExecEnv` subtracts the
list from `os.Environ()` (a subtraction, never a whitelist — `GOPI_HOME`, the
model credentials and PATH must survive, and an omission there would be a
silent breakage), and `taskEnvStripLine` renders the record for **both**
outcomes (*"child env stripped [GOPI_EXECUTOR_SESSION_ID GOPI_AGENT_SESSION]"*
vs *"child env is already free of the daemon execution context"*), so an
operator can tell "nothing leaked" from "nothing was checked".

Deliberate non-strips are documented in the file itself, one reason per name
(wrapper identity, workdir handoff, the parent-side child-session read, loop
configuration) so a future reader does not "complete" the list.

---

## Completed

- The study above (§1), including the peak-hour-guard history and the exit-code
  4 contract.
- Fix A: `tools.TaskRepoFor` + `TaskRepoForLaunch` (ground precedence:
  inside-the-ground / same-git-repository / recorded-worktree-wins) +
  `taskExecutorCwd` + the spawn seam
  (`cmd.Dir`, `GOPI_TASK_WORKDIR`, source report) + the push-leg anchor
  (`taskRunRepoDir`).
- Fix B: `tools.DaemonContextEnvVars` + `config.DaemonEnvLeakVars` mirror +
  `stripDaemonContextEnv` / `taskChildExecEnv` / `taskEnvStripLine` and the
  seam's strip record. The **merger** needed it too and got it in the same
  change: `merge_resolve_child.go` and `merger_verify_child.go` both built
  their child's env as `append(os.Environ(), …)` — the identical leak, in the
  daemon the epic names. Both now subtract the list (the verifier's baseline
  sweep through `isDaemonContextEnv`, the resolve child through the seam
  helper) and the spawner's own additions (auto-register latch, role marker,
  baseline path) are appended AFTER, so the strip can never touch them.
- Ten tests: five e2e walks on the real binary/spawn path (worker launched
  from a *different* repo than the task's; a two-task, two-project freshness
  walk; the executor env-strip walk; the merger resolve-child env-strip walk,
  which reuses the existing stubbed-resolver env dump — the resolve child's
  ACTUAL environment, not a re-derivation) and four hermetic seam tests.
- Red proofs: each half disabled in turn, rebuilt, re-run → the guard test
  fails and names the leaked directory (§Verification).

## Not Done

- **A pre-existing master bug was found while verifying, and is NOT fixed
  here** (out of scope; reported so it is not lost):
  `internal/tools/TestTaskStatusRunningForOwnRow` fails on **master itself**
  (reproduced in a clean detached master worktree, no branch code involved).
  The assertion is inverted relative to the unit under test:
  `taskVerdictResumable` correctly returns false for a running holder
  (double-run guard) and the test then errors with *"a row with a verified-live
  holder must NOT be resumable"* while asserting `if !st.Resumable` — a
  live row IS resumable only if the guard is broken, so the check fires
  exactly when the guard works. Effect: `go test ./internal/tools/` is red on
  master. Filed from this session as its own task (`task_submit`), not
  smuggled into this branch's diff.
- **No code change for the "tick instead of crash loop" half**: verified
  already implemented (idle wait + `idleStreak`/`workerIdleExits` + `--once`
  drain-exit + fail-soft round). Recorded as a finding, not implemented twice.
- **The peak-hour guard was not re-landed** — it is out of scope (it is a cost
  policy, not an isolation one) and re-landing it needs the operator's reason
  for the revert plus the rebase described in §1.3.
- **The resolve child's cwd stays its private conflict worktree** (deliberate:
  a resolver *must* stand in the tree it is resolving, and `child.Dir = wtdir`
  is already explicit). Fix A's repo resolution is not applied to it — the
  merger resolves a branch OF a configured repo, which it already knows.
- **`HERMES_SESSION_ID` is not stripped**: it is the *spawner's* foreign agent
  signature (the guard's second signal), and stripping it from a task child
  would be a hygiene improvement with a behavioural edge (a legitimate nested
  hermes run inside a task). Left as a documented, deliberate gap rather than
  a silent call.

## Verification

```
go build ./...                                  → OK (clean)
go vet ./cmd/agent/ ./internal/tools/ ./internal/config/ → clean
gofmt -l cmd/agent internal/tools internal/config → no output
go test ./internal/standards/                   → ok (12.9s)
go test ./cmd/agent/ -run '<the ten isolation + regression tests>' -v
   → 10/10 PASS
```

`TestTaskExecutorCwdPrefersTheTasksRepoOverTheLaunchDir` additionally covers
the **regression** shape (the task's own ground: inside it, and a sibling
linked worktree), and the branch's acceptance arm
`TestWorkerServerDeliveryRecyclesThenMergerMerges` — the existing test that
caught the first cut — is green on the corrected precedence.

The isolation tests: `TestStripDaemonContextEnvRemovesTheSpawnersOwnHandle`,
`TestDaemonEnvLeakVarsMirrorTools`,
`TestTaskExecutorCwdPrefersTheTasksRepoOverTheLaunchDir`,
`TestTaskEnvStripLineReportsBothOutcomes` (hermetic);
`TestWorkerExecutorChildStandsInTheTasksRepoNotTheLaunchers`,
`TestWorkerServesEachTaskInItsOwnRepoAcrossConsecutiveRounds`,
`TestWorkerChildDoesNotInheritTheDaemonsSessionHandle` (e2e).

The freshness walk's observable is the child's own `session_start.workdir`
(per task, from that task's JSONL) rather than a bash `pwd` probe: the mock
LLM's script is a GLOBAL request counter (`NewMockLLM` repeats only its last
entry), so which entry a given round receives is not knowable across two runs
— measured while writing the test.

Red proofs (each measured by editing production, rebuilding `bin/gopi`,
re-running, then restoring):

1. **Fix A disabled** (`taskExecutorCwd` resolving a bogus id → always the
   launch workdir):
   `TestWorkerExecutorChildStandsInTheTasksRepoNotTheLaunchers` → **FAIL**, with
   the child's `pwd` report naming the launcher's repo instead of the task's.
   Restored → **PASS**.
2. **Fix B disabled** (seam env rebuilt as `stripDaemonContextEnv(nil)` +
   `os.Environ()`):
   `TestWorkerChildDoesNotInheritTheDaemonsSessionHandle` → **FAIL** (no strip
   record, and the child ran with the daemon's handle). Restored → **PASS**.
3. **Merger strip disabled** (`child.Env = append(os.Environ(), …)` restored in
   `merge_resolve_child.go`):
   `TestMergerResolveChildDropsTheMergersOwnSessionHandle` → **FAIL**, naming
   the leaked values —
   `GOPI_EXECUTOR_SESSION_ID="session-of-the-mergers-launcher"` and
   `GOPI_AGENT_SESSION="2405928"`. Restored → **PASS**.
4. **The ground clause disabled** (`launchDirIsTaskGround` short-circuited to
   false — i.e. the first cut's unconditional "the task's repo"):
   `TestTaskExecutorCwdPrefersTheTasksRepoOverTheLaunchDir` → **FAIL** naming
   both worktree shapes (inside-it and sibling) and
   `TestWorkerServerDeliveryRecyclesThenMergerMerges` → **FAIL** reproducing
   the whole starvation chain by name (*"worker delivery left its worktree
   behind"*, *"merger pass landed no merge work … starvation"*). Restored →
   **PASS**.

Evidence on the real binary (throwaway profile, manual run of the built binary,
worker launched from `/tmp/.../launcher`, task submitted with
`--repo /tmp/.../owned`) — the round's own `output.log`:

```
gopi task run: executor cwd → …/owned (task t-probe: deliverable repo)
gopi task run: child env stripped [GOPI_AGENT_SESSION GOPI_EXECUTOR_SESSION_ID] (the spawning process's own execution context must not reach task t-probe)
```

Note the ordering in that run: `GOPI_AGENT_SESSION` came from the *grandparent*
(the command that launched the worker), `GOPI_EXECUTOR_SESSION_ID` from the
worker's own shell — i.e. both halves of the inheritance were live and both
were removed.

## Files

- `internal/tools/tasks.go` — `DaemonContextEnvVars`, 
  `TaskRepoFor`, `TaskRepoForLaunch` (+ `launchDirIsTaskGround`, `pathWithin`,
  `sharesGitRepo`, `gitCommonDir`, `SamePath`, `SameDir`), `liveDir`.
- `internal/config/agentguard.go` — `DaemonEnvLeakVars` (loud mirror).
- `cmd/agent/task_child_env.go` (new) — the seam's constants, `taskChildExecEnv`,
  `stripDaemonContextEnv`, `isDaemonContextEnv`, `taskEnvStripLine`, and the
  documented non-strip set.
- `cmd/agent/task_session_cwd.go` — `taskRunRepoDir`, `taskExecutorCwd`.
- `cmd/agent/cli_tasks.go` — the spawn seam (cwd + env + reports) and the
  push-leg anchor.
- `cmd/agent/merge_resolve_child.go` — the resolve child's env subtraction.
- `cmd/agent/merger_verify_child.go` — the verify child's env + baseline sweep
  subtraction.
- `cmd/agent/task_child_env_test.go` (new) — hermetic seam tests.
- `cmd/agent/task_executor_repo_cwd_e2e_test.go` (new) — the e2e walks.
- `cmd/agent/merge_resolve_e2e_test.go` — the merger strip walk (appended).

## Commit

`8701b599 fix(daemon-isolation): the executor child runs in the TASK's repo with
a fresh execution context` on `fix/daemon-context-isolation`.
