<!-- doc-class: historical -->
# devdoc: daemon-cwd-escape — the producer side of the worktree-reclaim deadlock

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

Epic: `daemon-cwd-escape` (2026-09-16). Branch: `fix/daemon-cwd-escape`.

## 1. The defect (what was actually broken)

A spawned process **inherits its launcher's working directory**, and `exec.Command`
with no `Dir` set says exactly that. GoPi's two resident faces — `gopi merger
--daemon`, `gopi worker --daemon` — were spawned **with no `Dir`**, so a daemon
started from inside a task's linked worktree kept that worktree as its cwd for
its whole (unbounded) lifetime.

Guard ③ of the reclaim arm — `finishCleanupWorktreeInUse` in
`internal/tools/task_finish_cleanup.go` — refuses to `git worktree remove` a
worktree with a **live process inside it**. That refusal is *correct*: the
2026-09-10 incident (row `01a088ef`) was a worktree torn out from under a running
session. But when the live process is an **immortal daemon**, the guard can never
stop refusing: the holder never exits, so the branch is delivered, the merge
happened, and the worktree still sits there forever.

Live evidence on this host: a running `gopi-merger` whose `/proc/<pid>/cwd`
**could no longer be resolved at all** (a deleted-directory holder — the kernel
prints `… (deleted)`), i.e. a process holding an already-removed worktree's inode
open, and others reporting `cwd` inside worktrees via `ps`.

**The fix is a producer obligation, never a guard weakening.** Guard ③ is
untouched: its predicate, its refusal direction and its tests are unchanged (see
§5 for the proof).

## 2. Design

New leaf package **`internal/daemoncwd`** decides the cwd a long-lived child must
start in. Two invariants, both *measured*:

1. **off `/`** (GOPI.md Rule 22) — `statfs` device id vs the device id of `/`,
   never a spelling. `/tmp` is on `/` on this host, so "not /tmp" is not the
   criterion.
2. **outside every task worktree** — outside the worktree the child descends
   from (found by walking the launcher's ancestors for the linked-worktree
   marker: a `.git` **file**, not a directory), and outside every declared
   worktree root (`worktreeroots.PermitsAny`).

Resolution order:

| # | candidate | condition |
|---|---|---|
| ① | `$GOPI_DAEMON_CWD` | the operator's explicit choice — honoured, and still *reported* if it lands in the escaped worktree |
| ② | `<scratch home>/runtime/process` | provably off `/` **and** outside every declared root |
| ③ | procfs (`/proc`) | same proof, plus an explicit existence check |
| ④ | keep the inherited directory | nothing provable — **reported**, never silently defaulted |

Rule 14 was applied to the one value two packages must agree on: a daemon's
identity is its **argv[0]**, and `cmd/agent/cli_daemon_spawn.go`'s
`daemonArgv0Prefix` is now an **alias of `daemoncwd.Argv0Prefix`** rather than a
second literal.

### 2a. A measured platform fact that changed the design

The recovery half originally identified a daemon by `/proc/<pid>/comm`. That was
**measured and found wrong** on a real daemon:

```
comm       = probe-gopi        # the kernel names comm after the EXECUTABLE
cmdline[0] = gopi-merger       # the stable name the spawn seam assigns
```

`comm` is taken from the executable path at exec, so a renamed/rebuilt binary
reports the binary's name — a comm-based check would miss exactly the daemons the
report exists to name. The check now reads **argv[0]** (`daemoncwd.ProcessArgv0`
/ `IsDaemonArgv0`), and the first spelling (`IsDaemonComm`) was **deleted** rather
than kept as an alias: a name that says "comm" while meaning "argv[0]" is how the
wrong channel returns.

## 3. Changes

| file | change |
|---|---|
| `internal/daemoncwd/daemoncwd.go` | **new** — `Resolve`, `Report`, `EscapeFromWorktree`, the measured predicates (`offRootFS`, `ResolvePrefix`, `WithinWorktree`), the procfs measurements (`ProcessCwd`, `ParentOf`, `ProcessArgv0`, `DescendsFrom`), `Argv0Prefix` |
| `cmd/agent/cli_daemon_spawn.go` | the shared daemon seam sets `child.Dir` from `daemoncwd.Resolve`; reports the escape, and **re-reads `/proc/<pid>/cwd` after the handshake** (`daemonCwdMeasureNote`) so every start prints where the daemon *actually* stands; imports the package |
| `cmd/agent/cli_tasks.go` | the executor spawn seam (`runTaskRunOnce`) hands the child `executorChildDir(...)` — the resolved cwd, escaped when it is a foreign linked worktree (a row with no deliverable repo used to hand the child the *launch* workdir, which for a daemon is wherever the daemon was started) |
| `cmd/agent/task_session_cwd.go` | **new** `executorChildDir` — the executor cwd decision (resolved cwd is the default; an escape overwrites it) |
| `cmd/agent/task_executor_child_dir_test.go` | **new** — the decision's own guard (see §3b) |
| `cmd/agent/task_child_env.go` | `taskProgName` (the report prefix) + the escape's comment block |
| `internal/tools/task_finish_cleanup.go` | recovery half: the refusal now **names the holder's kind** when it is a detached daemon (`finishCleanupDaemonHolderMarker` / `finishCleanupDaemonHolderFmt`) — guard predicate unchanged |

### 3a. Why the executor case needed its own predicate

`taskExecutorCwd` (`cmd/agent/task_session_cwd.go`) already resolves the child's
cwd from the *task* — the daemon-context-isolation epic (2026-09-13) fixed the
wrong-rules-file bug. But for a row with **no `deliverable.repo`** it falls back
to the launch workdir (`taskCwdSourceLaunch`) — and for a daemon's child that
fallback is exactly the pinned worktree.

`EscapeFromWorktree` therefore escapes **only** when the child would stand in a
**foreign** worktree:

- the task's own ground (`tools.TaskRepoForLaunch(target, launchWorkdir)`, the
  worktree the spawner stands in or the row's repo) → kept;
- a linked-worktree ancestor that is **not** the task's ground → escaped.

`TaskRepoForLaunch` (not the bare `TaskRepoFor`) is the own-ground answer, and
that distinction is load-bearing: the row's repo and the worktree the spawner
stands in are different directories, and a spawner launched inside the worktree
it serves makes that worktree the task's own ground
(`launchDirIsTaskGround` shape ①/c). The bare row repo would read the same
directory as *foreign* and escape it — which starves branch backfill (the
regression the daemon-context-isolation epic fixed). Measured: the `task run`
recycle e2e and both executor-cwd walks all launch from inside the worktree they
serve.

The self-chain exclusion the guard already makes (a worktree whose only occupant
is the run itself is recyclable) is what keeps "keep the own ground" correct
rather than a new veto.

### 3b. The regression this cycle found — and the guard it now carries

The first wiring read the predicate's result as if it were always the answer:

```go
childDir, escapedWt, escaped := daemoncwd.EscapeFromWorktree(execWorkdir, ownGround)
if escaped { …report… }
cmd.Dir = childDir            // ← "" on every non-escaping exit
```

`EscapeFromWorktree`'s contract is the opposite: its **first return is meaningful
only with `ok=true`**, and `ok=false` spells *"keep the directory you passed in"*
as `""`. Assigning it unconditionally therefore set an EMPTY `cmd.Dir`, and the
wrapper fell back to its session dir — every executor child ran in
`<sessions>/<task-id>` instead of the row's repo.

**Measured** (red on this branch, green on base `16f871c3`, same host): five
`cmd/agent` e2e walks — `TestManualRunRecyclesItsWorktree`,
`TestWorkerServerDeliveryRecyclesThenMergerMerges`,
`TestWorkerDeliveryRecyclesDespiteAStaleSiblingRow`,
`TestWorkerExecutorChildStandsInTheTasksRepoNotTheLaunchers`,
`TestWorkerServesEachTaskInItsOwnRepoAcrossConsecutiveRounds`,
`TestTaskRunStandsInTheSessionDirAndHandsTheWorkdirToTheChild` — each one
reporting a child in the session dir or a worktree left behind. The pattern to
carry forward: **a predicate whose "no" answer is `""` must never be assigned as
the answer**, only consulted for `ok`.

Two guards now exist so the class cannot return silently:

- `internal/daemoncwd/daemoncwd_test.go::TestEscapeFromWorktreeContract` pins the
  contract itself (ok=false ⇒ root == "" for the neutral, own-worktree and
  inside-own-worktree cases; a foreign worktree still escapes and is named);
- `cmd/agent/task_executor_child_dir_test.go` pins the CALLER's shape:
  `executorChildDir` returns the resolved directory unchanged when nothing
  escapes, `""` only when the resolved cwd was already `""`, and a measured
  off-`/` root outside the worktree when it escapes. Milliseconds instead of the
  five e2e walks.

## 4. Verification

Acceptance (a) is a **`/proc` measurement of a real spawned daemon**, not an
assertion that a field was assigned:

`cmd/agent/cli_daemon_cwd_test.go::TestDaemonSpawnFromWorktreeEscapesMeasuredCwd`
creates a real linked worktree (`git worktree add`), starts the **real built gopi
binary** (`gopi merger --daemon --every 1h`) with the child's cwd set **inside**
that worktree, and then asserts, from the kernel's own answer:

- `/proc/<pid>/cwd` is **not** the worktree and **not** inside it;
- it is not inside the operator's declared worktree root
  (`worktreeroots.DefaultRoot`);
- it does **not** share `/`'s filesystem (`scratch.SharesRootFS` → false);
- the process measured is the daemon this test started (identity from
  `/proc/<pid>/cmdline[0] = gopi-merger` — ancestry is deliberately *not* the
  check: a detached daemon's launcher exits, so the child is reparented).

Measured output of the passing test:

```
measured daemon cwd: pid 3606967 → /dev/shm/gopi-test-scratch/runtime/process
  (worktree .../wt-feat-escape escaped, shares /: false)
```

Two more measured facts from the same run: the daemon is **unprivileged**
(`GOPI_CGROUP_ESCAPE_OFF=1`, no `sudo` anywhere — Rule 25), and the production
line the operator sees is

```
gopi merger daemon cwd: pid 3608956 stands in /dev/shm/gopi-test-scratch/runtime/process
```

### 4a. The defect and the fix, A/B at `/proc` level on ONE host (the red proof)

A test that only asserts the *fixed* behaviour cannot show it is measuring the
defect; the same launch was therefore run against BOTH binaries on this host —
the base of this epic (`16f871c3`, `/dev/shm/gopi-test-scratch/ab-master`) and
this branch — with the launcher's cwd inside a real task worktree
(`/home/user/workspace/gopi-daemon-cwd`, a linked worktree of `/home/user/work/GoPi`):

```
BASE  (16f871c3):  pid 4109890 argv0=gopi-merger
                     /proc/4109890/cwd -> /home/user/workspace/gopi-daemon-cwd
                     => the daemon IS inside the task worktree (the defect)
BRANCH (39faf280): pid 4111658 argv0=gopi-merger
                     /proc/4111658/cwd -> /dev/shm/gopi-test-scratch/runtime/process
                     stat -c %d of that cwd = 23   vs   / = 2051  (measured off /)
                     => outside the task worktree (the fix)
```

and the branch's start line reports the decision it made (Rule 15):

```
gopi merger: daemon cwd /dev/shm/gopi-test-scratch/runtime/process (left the inherited
  directory /home/user/workspace/gopi-daemon-cwd behind: a process standing in a task
  worktree vetoes its recycle for its whole lifetime)
gopi merger daemon cwd: pid 4111658 stands in /dev/shm/gopi-test-scratch/runtime/process
```

The base daemon was killed by its measured pid immediately after the measurement
(Rule 24 — never a pattern kill).

Recovery-side verification (`internal/tools/task_finish_cleanup_test.go`):

- `TestFinishCleanupWorktreeInUseNamesADaemonHolder` — a daemon-shaped holder
  (child re-exec'd with argv[0] = `gopi-merger`) is refused **and named** as an
  unrelated immortal holder, with the lever for a future daemon (`GOPI_DAEMON_CWD`)
  in the reason;
- `TestFinishCleanupWorktreeInUseStaysSilentForATaskSession` — the contrast case:
  an ordinary live session gets the plain refusal, so the distinction keeps
  carrying information;
- `TestWorktreeReclaimableOnlyAfterDaemonHolderLeaves` — the sweeper's **own
  predicate** refuses while the daemon holder is inside and reports the worktree
  recyclable once it leaves, which is what makes the producer-side fix sufficient
  to unblock the recycle.

Package tests (`internal/daemoncwd/daemoncwd_test.go`) pin the decision branches
against an injected device table — the branches "the scratch tree is on `/`" and
"the scratch tree is inside a declared worktree root" cannot be produced with a
real directory on a host where `/tmp` **is** on `/`.
`TestEscapeFromWorktreeContract` (same file) pins the caller-facing contract, and
`cmd/agent/task_executor_child_dir_test.go` pins the caller's shape (§3b).

**The executor seam's regression check, A/B on one host** (the shape every
regression claim in this devdoc is held to): the six walks above were run
`-count=1` on this branch (RED) and on base `16f871c3` in a throwaway worktree
(`/dev/shm/gopi-test-scratch/ab-master`, `bin/gopi` built there first — the suite
refuses to skip when the binary is missing) where they are GREEN. After the fix
the same six are GREEN here, together with the daemon-cwd acceptance test.

```
=== RUN   TestDaemonSpawnFromWorktreeEscapesMeasuredCwd
    measured daemon cwd: pid 3828388 → /dev/shm/gopi-test-scratch/runtime/process (worktree .../wt-feat-escape escaped, shares /: false)
--- PASS: TestDaemonSpawnFromWorktreeEscapesMeasuredCwd (7.54s)
--- PASS: TestManualRunRecyclesItsWorktree (1.91s)
--- PASS: TestWorkerServerDeliveryRecyclesThenMergerMerges (4.31s)
--- PASS: TestWorkerDeliveryRecyclesDespiteAStaleSiblingRow (3.32s)
--- PASS: TestExecutorChildDirKeepsTheResolvedDirectoryWhenNothingEscapes (0.21s)
--- PASS: TestExecutorChildDirEscapesAForeignWorktreeAndReportsIt (0.11s)
--- PASS: TestWorkerExecutorChildStandsInTheTasksRepoNotTheLaunchers (3.39s)
--- PASS: TestWorkerServesEachTaskInItsOwnRepoAcrossConsecutiveRounds (4.65s)
--- PASS: TestWorkerChildDoesNotInheritTheDaemonsSessionHandle (3.21s)
--- PASS: TestTaskRunStandsInTheSessionDirAndHandsTheWorkdirToTheChild (2.61s)
```

## 5. Guard ③ is unweakened

- `finishCleanupWorktreeInUse`'s predicate is unchanged: same pid scan, same
  `finishCleanupPathUnder` containment, same `finishCleanupSelfChain` exclusion,
  same fail-soft on an unreadable procfs;
- the **only** change inside it is an *appended* sentence to the *reason string*
  when the holder is a daemon — the refusal itself is identical, and the worktree
  still survives (asserted);
- its pre-existing tests (`TestFinishCleanupWorktreeInUseDetectsLiveCWD`,
  `…NestedCWD`, `…HeldByCallerIsRecycled`, and the two merged-cleanup liveness
  cases) pass unchanged. No test was edited to accommodate this change.

## 6. Not done / deliberately declined

- **Imposing a repo-build gate on the daemon cwd.** Considered and rejected: it
  would make a host whose declared worktree root contains the scratch tree fail
  to start a daemon at all. Degrading to procfs (announced) keeps the invariant
  and the availability.
- **A `GOPI_DAEMON_CWD`-style override for the *executor* seam.** The executor's
  cwd is a *correctness* input (the task's project and its branch-backfill
  authority), not hygiene; exposing a knob there invites the wrong-rules
  regression back. The executor's escape is therefore automatic and narrow
  (foreign worktrees only).
- **Removing the already-recorded `.holder`/comm-based legacy** — out of scope;
  the report distinguishes the daemon by argv[0], which is what a real daemon
  carries.
- **Weakening guard ③ to let a known daemon holder through** — declined by
  construction: the guard's refusal is the correct answer to a live process
  inside a worktree, and the producer-side fix is what removes the holder. The
  recovery half only *names* the holder (Rule 15b), it never adds an exception
  that would recycle a directory a live process stands in.

## 7. Follow-ups worth filing

- The live host still has daemons started **before** this fix; they hold their
  worktrees (or a deleted inode) until stopped. The recovery report now says so
  in the operator's own words. A one-shot sweep that names pre-fix daemons
  (`gopi worker status`-style, read-only) would close the loop for them.
- `os.Getwd()` in `cmd/agent/task_session_cwd.go` (the launch-workdir
  resolution) still returns the raw cwd; `daemoncwd.ResolvePrefix` normalisation
  is applied at the escape decision, so the two spellings can differ in the log.
  Harmless today, worth unifying if a third consumer appears.

## 8. Environment facts measured while verifying (why the evidence reads as it does)

These are host facts a future verifier will re-encounter; none of them is a
property of the fix, and each one *would* have produced a wrong conclusion if
taken at face value.

- **A "did we avoid privilege" check keyed on any uid-derived *label* is
  vacuous on this fleet**, so the epic's unprivileged claim deliberately rests
  on *components*, not on a label: the daemon is started with the cgroup escape
  OFF (`GOPI_CGROUP_ESCAPE_OFF=1`) and no code or text in the diff prescribes
  `sudo`/root (Rule 25 — the diff is scanned, not trusted:
  `git diff master...HEAD | grep -in -E 'sudo|as root|privile'` returns only
  lines that DENY a privilege). No step of this epic needs one.
- **`go test ./cmd/agent/` is red on this host for reasons that predate the
  branch.** Established by an A/B against the epic's own base commit
  (`1335daed`, in a throwaway worktree): `TestSweepZombiesCLI_*` fail only
  because `bin/gopi` was not built (they refuse to skip, by design), and
  `TestWorkerDaemon_StatusReportsTheLiveDaemon` fails identically on the BASE
  commit. With `bin/gopi` present the sweep-zombies pair passes. The
  `internal/tools` and `internal/daemoncwd` packages are green on both.
- **The suite's E2E halves need `bin/gopi`** (`CGO_ENABLED=0 go build -o bin/gopi
  ./cmd/agent/`); without it the "no binary" refusal is a failure, not a skip —
  which is exactly the guard `cli_tasks_sweep_zombies_test.go` documents.

## 9. Verification runbook (Rule 10.6: reuse, don't redo)

```bash
cd <worktree>
export TMPDIR=<a directory MEASURED off />          # Rule 22
CGO_ENABLED=0 go build ./...                        # build gate
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/     # the E2E tests' binary
CGO_ENABLED=0 go test -run TestDaemonSpawnFromWorktreeEscapesMeasuredCwd -v ./cmd/agent/
CGO_ENABLED=0 go test ./internal/daemoncwd/ ./internal/tools/ ./internal/standards/ ./internal/stylegate/
```

The acceptance test prints the measured line itself:

```
measured daemon cwd: pid <n> → /dev/shm/gopi-test-scratch/runtime/process
  (worktree .../wt-feat-escape escaped, shares /: false)
```

Re-check the claim by hand on any live daemon the fix started:

```bash
ls -l /proc/<pid>/cwd        # must not be inside any task worktree
stat -c %d /proc/<pid>/cwd   # must differ from: stat -c %d /
```

