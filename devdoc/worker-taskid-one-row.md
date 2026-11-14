<!-- doc-class: historical -->
# `gopi worker <task-id>` — one row, then exit (structural, not a mode flag)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Epic**: `worker-taskid-one-row` (briefing: *"gopi worker taskid should exit —
it is observed gopi worker taskid is not following help explanation of executing
taskid once and exit, more like executing taskid first and then retrieve next to
execute until task queue is drained. confirm code is executing one task only and
fix if otherwise"*)

## The finding (measured, not argued)

`gopi worker <task-id>` **did** run exactly one row on master `812f9a60`, and its
one-row behaviour was already *structural* — but only accidentally so:

- the named row entered `workerRound(explicit, opts)` and took the
  `if target == ""` short-circuit, i.e. **the claim scan was one branch away**
  (`cmd/agent/cli_tasks_worker.go` — `case !strings.HasPrefix(a, "-")` in
  `scanWorkerArgs` ⇒ `wa.explicit` ⇒ `runTaskWorkerExplicit` ⇒ `workerRound`).
  A one-character edit (`if explicit == "" { res, err := tools.TaskNextFiltered(...) }`
  losing its guard, or a refactor that "simplifies" the two faces back into one
  mode argument) restores queue draining without touching any named-row code.
- `runTaskRunOnce` carries a **second** claim scan (`internal` claim arm for the
  `target == "" || target == Next` case) that the named row never reached because
  `workerRound` only called it with a non-empty target.

Measured against a sandbox ledger holding **three** pending rows (named row
`vA` with the *highest* priority number, so it is the last row a claim scan would
pick):

```
$ gopi worker vA                      # sandbox ledger: vA(30) vB(10) vC(20) pending
worker: running vA (explicit)
gopi worker: task vA FAILED (child: synthetic child failure (test escape))
exit=1
$ gopi task list
#vC  [pending]   #vB  [pending]   #vA  [failed] [via worker]
```

One row ran; the other two were untouched. **So the reported symptom was not
reproduced on master.** Two candidate explanations remain open and are recorded
in `## Not Done` — the most likely being a binary that predates the fix.

## What changed (make it impossible, not merely currently true)

`cmd/agent/workerRound(explicit, opts)` became three functions, so the claim arm
is no longer *reachable* from the named face at all:

| function | who calls it | may claim? |
|---|---|---|
| `workerRoundClaiming()` | the loop (`runTaskWorkerLoop`), twice | **yes** — the only `tools.TaskNextFiltered` call in the file |
| `workerRoundNamed(taskID, opts)` | `runTaskWorkerExplicit` | **no** — takes the row as an input, and has no path to `workerRoundClaiming` |
| `workerRoundRun(target, opts, source)` | both of the above | no (it never selects a row) |

`workerRowSource` is a closed two-value enumeration (`workerRowClaimed` /
`workerRowNamed`) that also owns the two announce lines — there is deliberately
**no** value meaning "claim if nothing was named", which is the shape that let
the named face grow a second round in the first place. The run failure line
("continuing") is now printed only for a CLAIMED round: a named round's failure
is the process's exit code, and promising a continuation the named face does not
have was its own misleading text.

`runTaskWorker` copies the caller's options and stamps `taskID` for the named
face, so `workerRoundNamed` is never handed an option set that could re-express
the row as "pick one".

## Regression test (with its red-proof)

`cmd/agent/worker_taskid_one_row_test.go`:

- `TestWorkerExplicitRunsExactlyOneRow` — seeds THREE pending rows in one
  sandbox ledger, names the one a claim would pick **last** (priority 30 vs
  10/20), runs the named round, and asserts the named row left `pending` while
  **every other row is still `pending`**. An empty queue cannot distinguish a
  one-row face from a drain face; the fixture therefore guarantees the queue is
  non-empty and the claim order differs from the naming order.
- `TestWorkerExplicitNamesNoLoop` — source-text gate: `workerRoundNamed` must
  not mention `tools.TaskNextFiltered` / `workerRoundClaiming` /
  `runTaskWorkerLoop`, the file must carry exactly ONE `tools.TaskNextFiltered(`
  call, and `runTaskWorker` must route the named face **before** the loop.

**Red-proof (measured)**: mutating `workerRoundNamed` to `return
workerRoundClaiming()` — i.e. putting back a drain-capable named face — turns
the test RED:

```
--- FAIL: TestWorkerExplicitRunsExactlyOneRow (4.93s)
    the named row one-row-1 is still pending — the round did not run it
    (it ran a row the operator did not name?)
```

Restored, the same test is green. (The first version of this test named the row
the claim would have chosen anyway and **passed on the mutated code** — the
priority inversion above is what makes the mutation observable.)

## Help/contract text corrected

`--once` is a *drain-until-empty* spelling whose name suggests "one task"; the
page said "drain the queue once, then exit 0", which is what a reader can
miscount. Now:

- `cmd/agent/main.go` `workerUsageHead`: `--once` = "stay in the foreground until
  the queue is empty … **NOT a one-task mode: use `<task-id>` for a single row**";
  `<task-id>` = "run exactly this **ONE** row … **It never touches the rest of
  the queue**"; the Notes "One round" paragraph is scoped to the QUEUE faces and
  states that a NAMED row never claims a second row.
- `internal/config/help.go` command index: `--once is the unambiguous launcher
  spelling; a bare <task-id> runs exactly that ONE row in the foreground and
  never claims another`.

## Blast radius

- `cmd/agent/cli_tasks_worker.go` — the round split, the source enumeration, the
  failure-line scoping, the named-face opts stamping.
- `cmd/agent/main.go`, `internal/config/help.go` — help text.
- `cmd/agent/worker_taskid_one_row_test.go` — new.
- `cmd/agent/worker_drain_unit_test.go` — call sites follow the rename.
- comment-only follow-ups: `internal/tools/worker_output.go`,
  `cmd/agent/task_session_cwd.go`, `cmd/agent/cli_tasks.go`,
  `cmd/agent/cli_tasks_worker_tag_flow_test.go` (they named `workerRound`).

## Upstream defect found while probing (not fixed here)

The named round's own-worktree release pass enumerates the repositories of the
process's **cwd** and frees every clean, unheld, older-than-5-minutes worktree
that no ledger row owns. Run from the main repo with a sandbox ledger, it freed
two **live** worktrees of this host:

```
worker: own-worktree-release: own registered worktree freed: /home/user/workspace/gopi-req-b2b
worker: own-worktree-release: own registered worktree freed: /home/user/workspace/gopi-req-ledger-m3
```

Both were restored (re-attached at each branch's current tip; no branch, commit
or file was lost — the branches were clean). This is `internal/scratch`/
`ReclaimSelfOwnRegisteredWorktrees` reaching into the live repository when the
ledger is sandboxed but the cwd is not; the hermetic test pin
(`SandboxRepoContext`) exists precisely to keep test rigs out of it, but a
sandboxed *operator* invocation has no such pin. Filed as a separate concern —
it is unrelated to the row-selection contract this task pins.
