<!-- doc-class: historical -->
# devdoc: `gopi task show --branch` / `--worktree` — addressing a row by its deliverable

**Status:** implemented. **Branch:** `task/01a0c86fced273dc8837751ce0bc820d`.

## The problem

`gopi task show` addressed a ledger row by its **task id** alone. The operator
standing in a task worktree holds the opposite: `git branch --show-current`
prints the branch, `pwd` prints the worktree — and the row id is the one thing
that workflow never carries. So "which row is this checkout?" required a
`gopi task list` scan and an eyeball match.

The ledger already records both facts. A row created through the dispatch path
carries a deliverable blob:

```json
{"repo":"/home/user/work/GoPi","branch":"feat/x","worktree":"/home/user/workspace/gopi-…"}
```

So the missing piece was never data — it was a **selector**.

## What shipped

```
gopi task show [<task_id>] [--branch B | --worktree PATH] [--db PATH]
```

Three addresses, one digest:

| address | what the operator has |
|---|---|
| `<task_id>` | the canonical id |
| `--branch B` | `git branch --show-current` **in the task worktree** |
| `--worktree PATH` | `pwd` there |

### Files

- `internal/tools/task_deliverable_selector.go` (new) —
  `ResolveTaskIDByDeliverable(DeliverableSelector{Branch|Worktree, Repo}, dbPath)`
  returns the ONE row a selector addresses, plus
  `DeliverableResolutionNote` (the plural-owner note).
- `internal/tools/task_for_worktree.go` (new) — `TaskForWorktree`, the sibling
  of the existing `TaskForBranch`: the same rows, the same fields, only the
  matcher differs.
- `internal/tools/task_for_branch.go` — the scan (`taskDeliverableScanRows`) and
  the owner ordering (`orderTaskForBranchOwners`) were **extracted** out of
  `TaskForBranch` so the worktree lookup calls the SAME ones.
- `cmd/agent/cli_tasks.go` — flag admission (`taskFlagMember`), the parse case
  for `show`, and two new `tasksOptions` fields.
- `cmd/agent/cli_tasks_show.go` — `runTaskShow` resolves the selector before
  rendering.

## The four design decisions worth keeping

### 1. ONE resolution policy — borrowed, not re-invented

A branch is **commonly** named by more than one row (measured on the live
ledger: `feat/merger-always-rebuilds` is named by **8** rows — the in-flight run
plus the completed rows the completion-time backfill stamped). So "who owns this
branch" is a *policy* question, and the repo already had an answer:
`TaskForBranchResolvedRow`, the **live-owner-outranks-decided** rule the merge
fence uses.

`ResolveTaskIDByDeliverable` calls **that function** rather than writing a
second tie-break. A second rule is exactly how `task show --branch X` and
`gopi merge --status` would come to disagree about who owns `X` (Rules 13/14).

### 2. Plurality is a NOTE, not an error

An ambiguity refusal would fail on precisely the branch whose owner is most
interesting. So the resolver picks the owner by the shared policy and the other
rows are **named** — on **stderr**, so stdout stays exactly the digest a script
already parses:

```
$ ./bin/gopi task show --branch feat/merger-always-rebuilds
8 rows name branch feat/merger-always-rebuilds; showing 01a0a7d4-….
Other owner(s): 01a0a7ca-… (completed), 01a0a7b2-… (completed), …   ← stderr
task_id:        01a0a7d4-29cc-796f-991d-8001ce5a14e4                ← stdout
status:         completed
…
```

### 3. The worktree comparison is by PATH IDENTITY, not string equality

`~/workspace/wt`, `/home/user/workspace/wt/` and `/home/user/workspace/./wt`
are one worktree, not three — `taskWorktreePathKey` is `Abs` + `Clean`.

It deliberately does **NOT** resolve symlinks: the ledger records what the
dispatch path recorded, and resolving at query time would make the answer
depend on the filesystem's state *now* (a worktree removed and recreated
elsewhere would keep matching a row it no longer belongs to).

### 4. A row with a worktree but no branch is not an owner

The shared scan's unit is "a row that names a branch" (that is the branch
lookup's unit). Worktree matching therefore sees **every row the branch lookup
sees — no wider, no narrower**, so the two answers cannot describe different
universes.

## Pitfall recorded (cost ~1 rebuild)

A **backtick inside a Go raw-string literal terminates it**. The help page for
the new flags lives in the `tasksUsage` raw-string const, so an inline
`` `git branch --show-current` `` in the new help text broke the build. The
help text now uses plain words ("the branch the row records"), and any future
edit of that const has to do the same — or switch to a quoted string.

## Verification

See `tasks/<id>-report.md` (the run's own report) for the Rule 31 evidence:
the real binary invoked with `--branch`, `--worktree`, a shared selector, an
unknown selector, both selectors, and the id form — verbatim commands and
observed output.
