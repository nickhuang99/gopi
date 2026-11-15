<!-- doc-class: normative -->
# The worktree-cwd zombie: why a finished task's worktree is never removed

**Incident:** 01a09e44 (2026-09-14). Ten linked worktrees sat under
`~/workspace`, several holding the ONLY copy of a finished task's branch, and
the merger could not merge any of them. Sweep evidence + root cause below; the
code fix is deferred to its own task (see the end).

## Symptom → mechanism → norm (5-why)

1. **Symptom** — `scripts/safe-merge.sh <branch>` refuses every one:
   `❌ [c] 分支仍被 worktree 占用`. The merger agrees: `protected: 6 branch(es)
   held by a live worktree`, `pending: none — nothing to merge`. In the operator
   sink the chronic count is `protected: 999` (1106×) and `protected: 9` (453×).
2. **Why the gate refuses** — the branch is still checked out in a registered
   worktree. (Also why `git branch -d` fails and why an auto merge-resolve child
   dies instantly: `worktree add … <branch>: exit status 128` / `Cannot delete
   branch … checked out at '/home/user/workspace/gopi-…'`.)
3. **Why the worktree is still there** — the reclaim arm
   (`internal/tools/task_reclaim.go` `ReclaimDeadHolderWorktrees`, reached from
   `cmd/agent/cli_tasks.go` `finishCleanupDeadHolderReclaimLines` by BOTH the
   merger pass and the worker sweep) refused it. Guards ① (terminal row, dead
   claim pid) and ② (clean tree) held — the holders really were dead and the
   trees really were clean. **Guard ③ refused: "a live process works inside this
   directory"** (`finishCleanupWorktreeInUse`, cwd scan + `lsof +D`).
4. **Why a live process was inside** — 18 orphaned `gopi-merger merger` daemons
   had `cwd = /home/user/workspace/gopi-<finished task>/cmd/agent`, started
   00:37–07:49, i.e. **5–12 h after their task went terminal**. Because
   `cmd.Dir = execWorkdir` (`cmd/agent/cli_tasks.go:1643`) resolves the executor
   child's cwd to `tools.TaskRepoFor(task)` — the row's `deliverable.worktree`
   when one exists — everything spawned from inside a task worktree (the
   executor child, a merger daemon, an e2e test process, Xvfb) inherits that cwd.
   The cwd release `os.Chdir(tools.CwdReleasePath)` exists only on the
   `gopi task run` face (`cli_tasks.go:1833`) and never reaches the daemons it
   spawned.
5. **Why that is permanent** — guard ③ is *correct* (a directory with a live
   process inside must never be ripped out), and a daemon has no end-of-life and
   no cwd release. So a correct guard + an immortal holder = an eternal refusal.
   **The residue is a producer-side cwd-ownership bug, not a reclaim bug.**

**Norm:** the process that owns a task's worktree must be the process whose cwd
is inside it, and that process must release the cwd before its row goes
terminal — or never enter the directory at all.

## What is NOT the cause (checked, to stop a wrong fix)

- **Peak-hour deferral.** `cmd/agent/cli_tasks_worker.go:503` only *delays
  claiming*; no worker log ever says "deferring claims", and the holders
  outlived their tasks across off-peak windows and host-local night hours. Peak
  does aggravate a different symptom (the cost guard refusing non-LLM commands,
  `01a09d73`), and it can make a task sit unclaimed — but it neither creates nor
  preserves a worktree.
- **A missing caller.** Both call sites exist (`mergeDeadHolderReclaimLeg` on the
  merger face, `serveFinishCleanupSweep` on the worker face) — this was the
  defect of an earlier epic and it is fixed.
- **A dirty tree.** Every branch-holding tree here was `git status --porcelain`
  empty (guard ② held). The two dirty trees were branchless A/B fixtures and
  were backed up before `--force`.
- **A stale binary / unmerged fix.** The running merger identifies itself as the
  current master binary (`gopi version` → `b9ae5cbc` = master tip), so the
  reclaim code in effect IS the merged one.

## Recovery recipe (proven — 01a09e44)

1. INVENTORY: `git worktree list --porcelain` (branch or detached, `prunable`),
   then per entry: `git -C <path> status --porcelain`, the **ledger row**
   (`sqlite3 ~/.gopi/memory.db` — the tasks ledger lives in **memory.db**;
   `~/.gopi/tasks.db` is an empty 0-byte file), and liveness = `/proc/<pid>/cwd`
   readlink scan (NOT `pgrep -f`, which self-matches) + `lsof +D`.
2. CLASSIFY: holder alive + row live → **leave it**. Holder alive + row terminal
   → an orphaned holder: the branch is being withheld, terminate the holder
   **by explicit pid**, after checking its cmdline and cwd (repeat the scan right
   before the kill; pids are not stable). Never touch the deliberate lock holder
   (`~/.gopi/serve.lock`).
3. BACK UP before any `--force`: `git diff HEAD`, `porcelain.txt`, and the
   untracked files, into `~/.gopi/backups/wt-sweep-<date>/<name>/`.
4. REMOVE THE DIRECTORY ONLY: `git worktree remove <path>` (plain; `--force`
   only after the backup). **Never delete the branch** — it is the deliverable.
5. `git worktree prune` (a directory deleted behind git's back stays registered
   and gate [c] keeps blocking).
6. VERIFY: `git rev-parse --short <branch>` for every affected branch must be
   unchanged; `bash scripts/safe-merge.sh <branch>` must exit 0; `gopi merger
   --status` must show the branch under `pending:` instead of `protected:`.
7. RELEASE YOUR OWN worktree too — safe-merge gate [c] applies to your branch as
   well, so a task that leaves its own worktree registered blocks its own merge.

## The durable fix (submitted as its own task — do not re-derive)

- **Producer side:** daemon/exec spawns must never keep a task worktree as their
  cwd — set `cmd.Dir` to `tools.CwdReleasePath` for the merger/worker daemon
  specs (`cmd/agent/cli_merger_daemon.go:184 mergerDaemonSpec`,
  `cmd/agent/cli_daemon_spawn.go`), so a daemon started from inside a worktree
  cannot pin it.
- **Recovery side:** guard ③ should distinguish "a process of THIS task is still
  working here" from "an unrelated immortal daemon happens to have this cwd".
  A holder that owns no live ledger row and is not a descendant of a live run
  must be REPORTED as a named exception (Rule 15) rather than vetoing release
  forever — and the arm must say so, so the operator sees the name, not a bare
  protected count.
- Any change here ships with a regression test that spawns a holder with a
  worktree cwd, goes terminal, and asserts the directory is freed (or the
  exception is named) — plus the LLM-adoption proof if prompt/tool text changes.
