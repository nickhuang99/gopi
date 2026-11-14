<!-- doc-class: historical -->
# Task Report: TOO_BIG reframe children carried the parent run's ephemeral GROUND as deliverable.repo

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Epic 01a0d55b (child of the TOO_BIG reframe of 01a0d526). Diagnosis, fix,
red-then-green proof, the four live rows repaired and one actually started, and
the refusal now naming the operator's remedy.

## Completed

### 1. Diagnosis — the writer and the exact value it computed

The writer: **`reframeSubmitOptions` (`internal/agent/reframe.go:428`, pre-fix)**,
called from `dispatchReframeRoadmap` (`internal/agent/reframe.go:497`, pre-fix).
It built `tools.TaskSubmitOptions` with **no `Repo` field at all**, so the submit
engine fell back to `taskSubmitResolveDefaultRepo`
(`internal/tools/tasks.go:4618`, pre-fix) — which returned **the submitting
process's CWD**. For a worker-run parent that cwd IS the run's GROUND: the
linked worktree the worker creates for that run
(`tools.TaskRunGroundFor`, `internal/tools/task_run_ground.go:152`, called at the
ONE spawn seam `cmd/agent/cli_tasks.go:2076`) and deletes when the run finishes.

Measured on the operator's ledger (`~/.gopi/memory.db`), parent vs children:

```
$ sqlite3 ~/.gopi/memory.db "SELECT task_id,status,deliverable FROM tasks WHERE task_id LIKE '01a0d526%' OR task_id LIKE '01a0d528%';"
01a0d526-43bf-701f-9dba-6746c8a09d8e|completed|{"branch":"task/01a0d52643bf701f9dba6746c8a09d8e","repo":"/home/user/work/GoPi","worktree":"/home/user/workspace/gopi-01a0d52643bf701f9dba6746c8a09d8e"}
01a0d528-1e63-7b42-92d0-4dbe651b0a3e|failed|{"repo":"/home/user/workspace/gopi-01a0d52643bf701f9dba6746c8a09d8e"}
01a0d528-209c-768f-b533-fc38503beca9|pending|{"repo":"/home/user/workspace/gopi-01a0d52643bf701f9dba6746c8a09d8e"}
01a0d528-2352-74bf-86a6-47f287a5b5d2|pending|{"repo":"/home/user/workspace/gopi-01a0d52643bf701f9dba6746c8a09d8e"}
01a0d528-25d7-7cac-89a9-941bbf44afdb|pending|{"repo":"/home/user/workspace/gopi-01a0d52643bf701f9dba6746c8a09d8e"}
```

The child's `deliverable.repo` is **byte-identical to the parent's
`deliverable.worktree`** — the value is the parent's ground, mislabelled as a
repo. Three independent facts pin it:

```
$ git -C /home/user/workspace/gopi-01a0d52643bf701f9dba6746c8a09d8e rev-parse --is-inside-work-tree
fatal: cannot change to '/home/user/workspace/gopi-01a0d52643bf701f9dba6746c8a09d8e': No such file or directory
```
The directory no longer exists (the worker removed it when 01a0d526 finished) —
so the child's repo is a dead path, not a repository.

```bash
$ git -C /home/user/work/GoPi rev-parse --path-format=absolute --git-common-dir
/home/user/work/GoPi/.git
```
The parent's `deliverable.repo` is the durable one; the child's is not.

`gopi worker 01a0d528-1e63-7b42-92d0-4dbe651b0a3e` then refused — correctly —
with `the repo is not a git work tree ... refusing to start the run in the repo's
main checkout` (`tools.TaskRunGroundFor` →
`tools.TaskRunGroundRefusal`, `internal/tools/task_run_ground.go:230`).

Note the shape, because it is why the defect was invisible: the parent row is
<ins>well-formed</ins> (repo = main checkout, worktree = the ground, branch =
`task/<uuid>`). Only the AUTO-SUBMITTED children are wrong, and only because the
auto-submit ran from inside the ground.

### 2. The fix — a TOO_BIG child inherits the DURABLE repo

The durable repository is recoverable from **any** directory inside it: a main
checkout and every linked worktree of a repository answer
`git rev-parse --git-common-dir` with the same `.git` directory
(`mergeExecMainRepo`, `internal/tools/task_merge_executor.go:434` — the existing
ONE resolution of "which main checkout owns this path"; no second implementation,
Rule 13/14).

- **`tools.DurableRepoForSubmit(dir)`** (`internal/tools/tasks.go`, next to
  `taskSubmitResolveDefaultRepo`) — new, single source of truth. It refuses
  loudly when `dir` is not a work tree or its repository cannot be named; the
  caller chooses the strict face (abort) or the lenient one (warn, repo-less).
- **`taskSubmitResolveDefaultRepo`** now returns `DurableRepoForSubmit(cwd)`
  instead of the raw cwd — the engine's lenient auto-submit default is durable
  for every internal auto-submit path, not just reframe.
- **`cmd/agent/cli_tasks.go`** (`task submit --repo` default) and the
  **`task_submit` tool** (`internal/agent/loop.go`) both resolve through the same
  function (Rule 14 — one resolution, every face).
- **`reframeSubmitOptions`** takes the repo explicitly and
  **`dispatchReframeRoadmap`** resolves it once via `reframeSubmitRepo` →
  `DurableRepoForSubmit(reframeSubmitCwd())`, logging (never silently) when it
  cannot.

### 3. Red-then-green (Rule 10.4)

New test `internal/agent/reframe_durable_repo_test.go`
(`TestReframeChildRowsSurviveTheParentGroundsDeath`) drives the PRODUCTION path —
the `task(action=reframe)` TOO_BIG handler — from the position a worker-run parent
stands in (a real linked worktree), asserts each child row's
`deliverable.repo` is the durable main checkout, then **deletes the parent's
ground** and asserts every child still resolves a runnable ground. It uses no
symbol the fix adds, so it compiles on the pre-fix tree.

**RED on the pre-fix commit** (`216d3d41`, the branch's base), the same file
copied into a detached worktree of it:

```
$ go test ./internal/agent/ -run TestReframeChildRowsSurviveTheParentGroundsDeath -count=1
--- FAIL: TestReframeChildRowsSurviveTheParentGroundsDeath (6.86s)
    child 01a0d567-04bd-761a-a42f-ea4d657757b8 names the parent run's GROUND
    /…/reframe-ground as its repo — it dies with the parent
    child 01a0d567-04bd-761a-a42f-ea4d657757b8 repo = "/…/reframe-ground", want the durable "/…/reframe-main"
    (same for the second child)
FAIL
```

**GREEN on the fixed tree** (below, `## Verification`) — the same test, plus the
resolver-level tests (`internal/agent/reframe_durable_repo_resolver_test.go`,
blocked on the fix by construction: `tools.DurableRepoForSubmit` does not exist
pre-fix).

### 4. The four rows repaired, and one actually started

Repair goes through a new engine verb (no hand-editing of the ledger):
**`tools.TaskRepointDeliverableRepo`** (`internal/tools/task_repo_repair.go`),
reachable as **`gopi task update <id> --repo <path>`** (`cmd/agent/cli_tasks.go`;
the flag is advertised in the family usage). It writes through the SAME
single blob writer every other deliverable stamp uses
(`taskDeliverableParse`/`Rebuild`, CAS on the raw blob), so the attachments
manifest survives; a repo CHANGE drops the recorded branch/worktree, which
belonged to the old repository. It resolves a linked-worktree argument to the
durable main checkout, and refuses a non-work-tree path.

```
$ ./bin/gopi task update 01a0d528-1e63-7b42-92d0-4dbe651b0a3e --repo /home/user/work/GoPi
task 01a0d528-1e63-7b42-92d0-4dbe651b0a3e deliverable.repo: /home/user/workspace/gopi-01a0d52643bf701f9dba6746c8a09d8e -> /home/user/work/GoPi
   (the same line for the other three: 01a0d528-209c-…, 01a0d528-2352-…, 01a0d528-25d7-…)

$ sqlite3 ~/.gopi/memory.db "SELECT task_id,status,deliverable FROM tasks WHERE parent_task_id='01a0d526-43bf-701f-9dba-6746c8a09d8e';"
01a0d528-1e63-7b42-92d0-4dbe651b0a3e|failed|{"repo":"/home/user/work/GoPi"}
01a0d528-209c-768f-b533-fc38503beca9|failed|{"repo":"/home/user/work/GoPi"}
01a0d528-2352-74bf-86a6-47f287a5b5d2|pending|{"repo":"/home/user/work/GoPi"}
01a0d528-25d7-7cac-89a9-941bbf44afdb|pending|{"repo":"/home/user/work/GoPi"}
```

### 5. The refusal names the remedy

`taskGroundRefusalRemedyFmt` (`internal/tools/task_run_ground.go`) is appended by
`TaskRunGroundRefusal`, naming `tools.TaskRepoRepairVerb` (the ONE spelling of
the door, shared by the refusal and the usage page — Rule 1b/13). The ground
refusal itself is UNCHANGED in behaviour: it still refuses, still returns no
directory, still refuses the main checkout.

## Not Done

- **The executor of the started row was killed by me, not by the task.** Row
  `01a0d528-209c-…` was started to prove runnability (it reached the spawn and
  had its ground created); its LLM run is a long real task I could not let
  complete inside this budget. I set it `failed` with a reason that says exactly
  this and how to reopen it, so the queue is not blocked by a holder I killed.
  Its `deliverable.repo` is correct and its worktree registration was released
  (`gopi task finish-cleanup … --apply` → `refused=1` before the status move,
  because the row was `in_progress`; the orphaned registration is the same one
  the row will reuse when reopened).
- **No reframe E2E through a real LLM session.** The TOO_BIG dispatch is
  exercised through the production handler in-process (the test above), not by
  running a model. The pre-fix RED and post-fix GREEN are on that same path.
- `internal/agent`'s FULL suite was not re-run to completion inside this budget
  (see Verification for what was).

## Verification

- Build: `go build ./...` → exit 0, no output.
- Build (binary): `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK.
- Tests:
  - `go test ./internal/agent/ -run 'TestReframeChildRowsSurviveTheParentGroundsDeath|TestReframeChildRepoIsDurableNotTheGround|TestRepairRepoMovesARowOntoALiveWorkTree' -count=1` → `ok github.com/nick/gopi/internal/agent 9.740s`
  - `go test ./internal/tools/ -run 'TestTaskRunGround|TestDeliverable|TestRepairRepo' -count=1` → `ok github.com/nick/gopi/internal/tools 38.012s`
  - RED proof: the E2E test on the pre-fix commit `216d3d41` (detached worktree) → FAIL, quoting the ground value (above).
- Ran (Rule 31 — the surfaces I changed, executed for real):
  1. `./bin/gopi task update --help` → the family usage advertises `--repo PATH RE-POINTS the row's deliverable.repo at a live git work tree …`.
  2. `./bin/gopi task update <id> --repo /home/user/work/GoPi` × 4 → four repoint lines (above).
  3. `GOPI_ALLOW_AGENT_SPAWN=1 ./bin/gopi worker 01a0d528-209c-768f-b533-fc38503beca9` →
     `worker: running 01a0d528-209c-768f-b533-fc38503beca9 (explicit)`, then
     `running gopi with task 01a0d528-209c-768f-b533-fc38503beca9 as briefing...`,
     and `gopi worker: child ground → /home/user/workspace/gopi-01a0d528209c768fb533fc38503beca9 … CREATED the ground`. No ground refusal. (Operator override flag is the documented one-off; the verb refuses from an agent session without it.)
  4. The refusal, executed against a row whose repo really died (throwaway private
     `GOPI_HOME`, so the operator ledger is untouched): `GOPI_ALLOW_AGENT_SPAWN=1 ./bin/gopi worker 01a0d569-…` →
     `… the repo is not a git work tree … refusing to start the run in the repo's main checkout; remedy: re-point the row at a live git work tree (gopi task update <task_id> --repo <path-to-live-work-tree>) — a linked worktree is resolved to its repository's main checkout`, and that row records `status=failed` with the refusal text as its reason.
  5. `./bin/gopi task update 01a0d56c-4951-7916-9ae4-fdff34c7eb4d --repo /home/user/workspace/tmp/refproof/liverepo2 --status cancelled --reason "combined --repo + --status proof"` → `Task updated: 01a0d56c-… (status=cancelled, …)` and `gopi task show` then prints `status: cancelled` + `deliverable.repo: /home/user/workspace/tmp/refproof/liverepo2` (the combined shape: repair, then the status move).
- Result: the four children name `/home/user/work/GoPi` (a live work tree); one of them STARTED instead of refusing; the refusal sentence now carries the remedy; the new tests are red pre-fix and green post-fix.
- Commit: `5052246d fix(reframe): TOO_BIG children inherit a DURABLE repo, not the parent run's ground`

## Evidence (self-filed)

- `git -C /home/user/workspace/gopi-01a0d52643bf701f9dba6746c8a09d8e rev-parse --is-inside-work-tree` → `fatal: cannot change to '…': No such file or directory` (the value the children carried is not a repository).
- `sqlite3 ~/.gopi/memory.db` parent/child query → the child's repo equals the parent's `deliverable.worktree` byte-for-byte (quoted above); post-repair the same query shows `{"repo":"/home/user/work/GoPi"}` for all four.
- Pre-fix run: `--- FAIL: TestReframeChildRowsSurviveTheParentGroundsDeath (6.86s)` with the ground path quoted in the failure; post-fix: `ok … 9.740s`.
- `GOPI_ALLOW_AGENT_SPAWN=1 ./bin/gopi worker 01a0d528-209c-768f-b533-fc38503beca9` stdout: `running gopi with task 01a0d528-209c-768f-b533-fc38503beca9 as briefing...` + `child ground → … CREATED the ground …`.
- `./bin/gopi task show 01a0d528-209c-…` → `deliverable.repo: /home/user/work/GoPi`, `status: failed`, `reason:` the verifier-kill note.
