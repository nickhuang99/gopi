<!-- doc-class: historical -->
# Task Report: worker provenance tag — reported dead code is NOT dead (evidence) + the missing hermetic guard

Task: `bug: --worker flow never stamps the worker provenance tag (TaskRunWritebackTagged is dead code under the serve daemon)`
Branch: `fix/worker-provenance-tag` (tip below) · Worktree: `~/work/gopi-wprov`

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md**
>
> The `gopi worker` / `gopi task run` lines below are the commands the evidence
> was collected with; running one is the operator's decision, not this doc's.

## Verdict up front

**The reported defect does not reproduce.** There is **no `TaskWrapperEnv` env gate** in the
worker-provenance path — not on `master`, and not anywhere in `memory_sync_delta.go`'s history.
The tag IS written end-to-end by a real worker run. The submission's repro line
(`memory_sync_delta.go:257` early-return on `os.Getenv(TaskWrapperEnv)`) is a **misattribution**:
the gate it quotes exists, but in a DIFFERENT file — `internal/tools/task_deliverable_worktree.go:104`,
added by `0fc22d5` ("backfill worktree+branch at worktree-CREATION time"), where it guards the
worktree-backfill seam, not the writeback. (`memory_sync_delta.go`'s `TaskRunWritebackTagged` is at
line **305**, not 257, and has no gate.)

**Correction to an earlier draft of this report:** the cited test
`TestCovToolsTaskRunWritebackTagged` **does exist** — on the sibling branch `feat/cov-tools-rest`
(tip `3d13ded`, *tests only*), not on `master`. It **PASSES** there (§3). The first draft checked
`ls` in this worktree + code_search on the master tree and wrongly concluded "the file exists
nowhere"; the correct check is `git show <named-branch>:<path>`. The report's *evidence* was real;
only its inference (gate ⇒ tag suppressed) was wrong.

What the report got right: the flow-level behavior was **unguarded by tests** — the only guard was
`TestWorkerWritebackTagUnionsProvenance`, which calls the primitive directly
(`tools.TaskRunWritebackTagged(...)`) and never exercises the `workerWriteback` latch or the
`writebackOutcome` routing that a worker actually goes through. This deliverable closes that hole.

## Completed

1. **Refutation with evidence** (§1–§3 below): repo history + live E2E + A/B control.
2. **New hermetic regression guards** for the exact behavior the report claimed broken:
   - `internal/tools/worker_provenance_tag_test.go`
     - `TestWorkerProvenanceTagNoWrapperEnv_HostFromProcessEnv` — pins that `worker:<host>` is
       resolved from the **process's own** environment (`os.Hostname`), explicitly with
       `TaskWrapperEnv` unset (the daemon case the report describes).
     - `TestWorkerProvenanceTagReachesLedgerWithoutWrapperEnv` — with no wrapper env, a writeback
       lands a ledger row whose tags **end with `worker:<host>`**, existing tags preserved in
       front; a re-run is idempotent (no doubled tag).
   - `cmd/agent/cli_tasks_worker_tag_flow_test.go`
     - `TestWorkerWritebackOutcomeStampsProvenanceTag` — the **flow** face: `writebackOutcome`
       routes through the tagged writeback only when the `workerWriteback` latch is set
       (workerRound's state); a plain run stays tag-free; clearing the latch de-tags the next row.
3. **Mutation probe** (proof the guards bite): injecting exactly the reported gate shape
   (`if strings.TrimSpace(os.Getenv(TaskWrapperEnv)) == "" { return "", nil }` into
   `TaskRunWritebackTagged`) makes BOTH new tests fail with the reported symptom
   (`tags = "repro", want suffix "worker:hostb-Laptop"`); reverting restores green.
4. **Root cause of the cited-but-absent repro** (§2) — reported as a separate work item.

## Not Done

- **No production BEHAVIOR change.** None is warranted: the acceptance is already satisfied by the
  current code. A "fix" here would have been a fabricated change to satisfy a stale report. The only
  production-file edit is a **doc comment** on `TaskRunWritebackTagged` recording the no-wrapper-env
  contract (comment-only — zero behavior delta; the mutation probe below proves the guards, not the
  comment, carry the contract). If the intent was the *residual* gap in §4, say so and it becomes a
  real task.
- The residual gaps in §4 (A/B below) are **documented, not implemented** — out of this bug's scope
  and each carries its own acceptance question.

## §1 The reported gate does not exist (repo evidence)

```
$ git log --oneline -S 'os.Getenv(TaskWrapperEnv)' -- internal/tools/memory_sync_delta.go
(no output — zero commits ever contained it)

$ git log --oneline -S 'TaskRunWritebackTagged' -- internal/tools/memory_sync_delta.go
60ab0a3 feat(worker): M4.20-A worker loop — ... worker:<host> provenance tag

$ grep -n 'TaskWrapperEnv' internal/tools/memory_sync_delta.go        # master AND branch tip
(no output)

$ git show feat/cov-tools-rest:internal/tools/covtools_writeback_test.go | grep -n '^func Test'
20:TestCovToolsWorkerTaskTag
34:TestCovToolsTagWorkerClaim
72:TestCovToolsDefaultGitPushbackValidation
101:TestCovToolsTaskRunWritebackTags
150:TestCovToolsTaskRunWritebackTagged        ← the cited test DOES exist (branch-only, tests-only tip)
(the branch tip is `3d13ded`, one commit past the master merge-base; the file is NOT on master)

$ git show feat/cov-tools-rest:internal/tools/memory_sync_delta.go | grep -n 'TaskWrapperEnv'
(no output — still no gate; the file is identical to master's: `git diff master feat/cov-tools-rest -- …/memory_sync_delta.go` is empty)

Where the quoted gate really lives (misattribution root cause):
$ grep -rn 'GOPI_TASK_WRAPPER_ID' --include=*.go internal/ cmd/          # production code
internal/tools/tasks.go:3202:const TaskWrapperEnv = "GOPI_TASK_WRAPPER_ID"   ← the definition
$ sed -n '104p' internal/tools/task_deliverable_worktree.go                  ← 0fc22d5
	if strings.TrimSpace(os.Getenv(TaskWrapperEnv)) == "" {    // worktree-backfill seam, not the writeback
```

The production function at the cited file is (branch tip, unchanged from `60ab0a3`):

```go
func TaskRunWritebackTagged(taskID, status, resultSummary string) (string, error) {
	tags, err := taskTagsByID(taskID)      // ← no gate; reads the ledger row
	if err != nil { return "", err }
	merged := TagWorkerClaim(tags)
	return TaskRunWritebackTags(taskID, status, resultSummary, merged)
}
```

`TaskWrapperEnv` (`GOPI_TASK_WRAPPER_ID`) is genuinely absent in the worker/serve path — but it is
never consulted **by this function**, so its absence cannot suppress the tag.

## §2 Live E2E: the worker flow stamps the tag

Sandbox (no network, stub key), real binary built from the branch tip:

```
$ export GOPI_HOME=$(mktemp -d) GOPI_SKIP_CLAIM_BROADCAST=1 GOPI_API_KEY=stub
$ gopi task submit "provenance repro" --payload "reply OK only" --tags repro
$ gopi worker --once                     # GOPI_TASK_WRAPPER_ID unset in this shell
$ sqlite3 $GOPI_HOME/.gopi/memory.db "select task_id,status,tags from tasks;"
01a08ce5-…-8b17971500cc|completed|repro,worker:hostb-Laptop      ← worker:<host> PRESENT
```

A/B control on the same sandbox — a non-worker delivery must stay tag-free:

```
$ gopi task submit "plain run A/B" --payload "reply OK only" --tags repro
$ gopi task run next
$ sqlite3 … "select status,tags from tasks;"
completed|repro                                                      ← tag-free, as designed
```

(Acceptance shape confirmed: tags **end with** `worker:<host>`, existing tags preserved in front.)

### §2b Independent production evidence — the LIVE ledger already carries the tag

Not just a sandbox: the machine's own ledger (rows executed by real `gopi worker` rounds,
unrelated to this repro) shows the tag as the row's **trailing** element:

```
$ sqlite3 ~/.gopi/memory.db "select substr(task_id,1,8),status,tags,substr(summary,1,40)
                           from tasks where tags like '%worker:%' order by rowid desc limit 4;"
01a08cd5|completed|review,coverage,worker:hostb-Laptop|Code-review campaign: close internal/tools remaind
01a08c69|completed|bug,worker:hostb-Laptop|bug(deepseek): 1214 death-dump filename has 1-seco
01a08c68|completed|bug,worker:hostb-Laptop|internal/config.EnvBool doc claims agent.boolEnvTr
01a08c5b|completed|bug,worker:hostb-Laptop|bug(shot): /shot/json/list queried once right after chr
```

These rows were closed by the serve daemon's worker loop on this host — i.e. the `--worker` flow
stamps `worker:<host>` in production, with no wrapper env involved.

## §3 Acceptance status

| Acceptance criterion (from the epic) | Status |
|---|---|
| worker-executed task's ledger row tags end with `worker:<host>` | **already true on master** — verified E2E (§2) and now pinned hermetically (§Completed 2) |
| `TestCovToolsTaskRunWritebackTagged` still passes | **PASSES** (corrected): the test lives on `feat/cov-tools-rest` (tests-only tip `3d13ded`), not master. Run there: `git worktree add /tmp/gopi-covrest feat/cov-tools-rest && cd /tmp/gopi-covrest && go test ./internal/tools/ -run TestCovToolsTaskRunWritebackTagged -v` → **PASS** (0.05s). Unaffected by this branch (no source file touched). |
| hermetic guard that the tag does not depend on the wrapper env | **added** (new tests, mutation-probed) |

## §4 Residual gaps found while refuting (honest, out of scope)

1. **`runTaskVerify` is latently uncovered by the worker tag.** `writebackOutcome` checks
   `workerWriteback == taskID` and the verify child inherits `GOPI_TASK_WRAPPER_ID`, so — if
   `task verify` is ever run from inside a worker process — its terminal row write would be
   tag-free. `gopi worker` never calls verify today; flagged, not changed.
2. **Infrastructure terminal writes never carry executor provenance.** `TaskRunWriteback`'s own
   family callers (server merger / zombie sweep `task audit-merges`, `task adopt`, `task
   merge-execute`, the serve stale-sweep) close rows with no `worker:` tag — by design for
   server-side infra. Whether server/merger-executed rows *should* carry their own executor tag
   is a design question, not this bug.
3. **Pitfall discovered the hard way (devdoc'd for future agents):** running the sandbox worker
   E2E *from inside one's own worktree* gets that worktree deleted — the child engine's cwd is
   backfilled as the row's `deliverable.worktree`, and the close-out chain then runs
   `git worktree remove --force` (the repo is derived from cwd too). Re-run such E2E from
   `~/work/GoPi` (or any path outside the worktree).

| 4 — filed upstream | task `01a08cea-f074-7774-88f0-34908057c363` (tags bug,docs, repo /home/user/work/GoPi): contains the corrected attribution evidence and asks the `feat/cov-tools-rest` author to re-derive which file:line their quoted gate came from, so the false "dead code" record does not persist. |


<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->
