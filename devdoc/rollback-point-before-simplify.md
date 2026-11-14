<!-- doc-class: normative -->
# Rollback point before the 2026-10-29 simplification refactor

> **CORRECTED 2026-10-29 (epic `01a186d6`).** The worker round no longer carries
> a peer leg: `workerSyncPeers()` (the per-round db-sync) and
> `workerTakeOverAttempt()` (the peer-row transfer) are DELETED from
> `cmd/agent/cli_tasks_worker.go`. Statements below that describe either of
> them as the round's own behaviour describe the tree BEFORE that epic. What
> the round does today: `devdoc/worker-round-audit.md`.


## The point

```
cdfa9c0c12c8cb5d88f41b341bbc3f0f996e3b33
cdfa9c0c1  remove rule 6a to disallow branch creation
```

Measured 2026-10-29 on host-b in `/home/user/work/gopi`:

```
$ git rev-parse HEAD
cdfa9c0c12c8cb5d88f41b341bbc3f0f996e3b33

$ git merge-base --is-ancestor HEAD origin/master && echo YES
YES

$ git status --porcelain
(empty)
```

**This commit is on `origin/master`, the tree is clean, and it is the state
BEFORE the refactor described below.** Everything after it is the refactor.

## How to roll back

```bash
cd ~/work/gopi
git fetch origin
git reset --hard cdfa9c0c12c8cb5d88f41b341bbc3f0f996e3b33
make build
./bin/gopi --version | grep revision   # must print cdfa9c0c12c8
```

If the refactor was pushed and must be undone on the server as well, the push
is a REF MOVE and is the operator's call, not a worker's:

```bash
git push --force-with-lease origin cdfa9c0c12c8cb5d88f41b341bbc3f0f996e3b33:master
```

**`--force-with-lease`, never a bare `--force`**: the lease refuses the push if
somebody else moved `master` in the meantime, whereas a bare force would
silently destroy their work.

## What the refactor changes

The simplification this point exists to protect, as ruled by the operator on
2026-10-29:

### The premise

**The old code is built on `git worktree`, which is not a simple clone.** A
worktree shares its object store and refs with its parent repository, so it
cannot be deleted the way a directory can — it must be deregistered, its branch
dropped, its losses archived. That is WHY the rescue machinery below exists at
all: the complexity was never about tasks, it was about worktree's impossibility
to be thrown away.

**The new shape is a clone.** A clone is a full, independent repository. Push,
then delete the whole directory. No deregistration, no branch drop, no archive
tag — so every check that exists to survive a worktree has lost its premise, and
running one today is not merely useless but wrong.

### The task model, reduced

A task has exactly three live states: **success / failed / in_progress**. The
latter two both admit `resume`. When a resume fails again, the task is **cloned**
into a fresh, clean workspace and run again. There is no fourth thing, and no
sequence of diagnostic steps between them.

### The verbs being removed

Nine commands whose entire subject is worktree/branch rescue, plus `reopen`
(which is `clone` wearing the wrong name) plus `verify`:

```
for-branch       branch → row lookup
reconcile        ledger-reconciliation pass
finish-cleanup   terminal-row close-out chain
sweep-worktrees  worktree registration reclaim
retry-delivery   re-push a blocked delivery
branch-refs      does the peer hold this delivery branch
orphans          terminal rows' branch disposition audit
stranded         ledger-vs-row contradiction audit
verify           independent subprocess verdict on a delivery
reopen           reissue a judged row as a new <id>-r1 row
```

### Why `verify` goes with them, although it touches no worktree

Because the whole idea of gopi judging whether its own worker's work is good is
the wrong layer. The operator's reasoning, which this document exists to
preserve:

> 能够编译不冲突就可以算过了。Git 这个工具做得很好，它已经把逻辑上有冲突那种暴露出来了。
> 我们要相信程序员和 worker 做了很实在的工作去测试了。他唯一没有测试的是别人也改了他的代码。
> 如果他的代码提交的时候能够 fast forward merge 的话，我们应该相信他的测试是可以通过的。
>
> 从软件工程来讲，纠结 worker 的测试是否完备是没有意义的 —— worker 的测试都是局部测试，
> 因为他的代码跟最后集成的代码不一致。所以即便他做了完整的测试也没有意义。
>
> 与其反复在这个地方把门禁设得这么高，不如把集成测试做好。这是软件公司的一贯做法：
> 不要相信每个程序员写的测试有多完备，最终相信 QA 部门写的端到端测试。
>
> 不要忘记了我们的终极目的不是 gopi 的 bootstrap 自我开发，而是使用它开发一般性的项目，
> 那么根本就不存在这些测试逻辑。项目一定有自己的测试程序，它压根就不一定要用这些门禁来检验。

**A worker's tests are LOCAL tests of a tree that does not yet contain the work
it will be merged with.** The combination that ships has never been tested by
them, and no gate inside gopi can change that. What actually catches the
problem is the integration suite, and a fast-forward merge that compiles.
Moves that can be merged forward and compile are what "passes"; the rest is the
test suite's business, not gopi's.

### The verbs that remain

```
list  show  status  submit  update  search  archive
+ resume   (continues a failed or dead-holder row IN PLACE: same task id,
            same session id, same directory — `internal/agent/session.go:181`
            opens the log with O_APPEND, so a resume grows an existing run and
            never mints one)
+ clone    (copies a row into a FRESH, clean workspace — this is the rescue a
            resume that failed again deserves)
```

### The other thing being removed

`workerSyncPeers()` — `cmd/agent/cli_tasks_worker.go:1235` — the one automatic
leg still carried by the worker's own round (gated by
`config.TaskSyncEnabled()`, default OFF). It is the `task sync` nobody knew was
running, and it is not the worker's job.

## What is NOT being removed

- **`worker_round_dispatch_only_test.go`** — the guard that fails the build if
  any `*Tick` / `*Sweep` / `*GC` / `*Reap` call reappears inside the claim
  loop. It stays, because after the deletion it is the only thing that keeps
  the worker from growing these passes back.
- **The test suite itself.** Removing gopi's internal gates is the opposite of
  removing tests: the point is that QUALITY BELONGS TO THE SUITE, not to a
  verb gopi runs on itself.
- **`gopi task archive`.** Archiving a row is a ledger decision, not worktree
  machinery.

## If the refactor has to be abandoned

Roll back to the point above, then read this document's "What the refactor
changes" section: the work is re-dispatchable from that description, and the
reasoning — which is the expensive part — is preserved here rather than in a
worker's context window that no longer exists.
