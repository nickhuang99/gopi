<!-- doc-class: historical -->
# devdoc: the unbounded fixture child in the memory-sync suite — bounded runner, conversions, and the classification sweep

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task `01a0dc5e-049f-7cb0-9957-06b8526c0dde`, branch
`task/01a0dc5e049f7cb0995706b8526c0dde` (worktree
`~/workspace/gopi-01a0dc5e049f7cb0995706b8526c0dde`), whose base is `ff49687f`.

## 1. The defect, and why it is a *second*, independent defect

The task that fixed `TestMemorySyncEveryLoopE2EConvergesAcrossRounds`
(`fix/memory-sync-e2e-freshdb-stall`, merged into master as `dd82af4a` while this
run was in flight) established the root cause by measurement: the child the parent
parks on is **the test's own `gopi memory_store` fixture child** — a fresh DB,
whose `fsync` goes to the scratch device's ext4 journal. Its D-state thread sits in
`wchan=jbd2_log_wait_commit` 11/11 D-samples while the parent sits in
`wchan=do_wait`, i.e. `os/exec.Waitid`.

That is *why it is slow*. The *hang* is a separate defect: **an unbounded
`cmd.CombinedOutput()` with no deadline**. With no deadline there is no arm that
reaps a child which never returns, so the suite's only exit is its own timeout
SIGKILL — and the resulting timeout dump points at the fixture, not at the sync
loop it looks like it was testing.

`cmd/agent/cli_memory_sync_test.go` carried **11** such children.

## 2. What this branch ships

### 2.1 The bounded runner (carried, byte-identical)

`internal/testutil/runfixture.go` + `runfixture_test.go` arrived on the sibling
branch, whose merge-base with master is **the same commit as this branch's base**
(`ff49687f`), so neither file existed on my base. They are carried **verbatim**:

```
blob 9562d19811510105aac68f3eefb73294739ea916  internal/testutil/runfixture.go
blob 64a52512b6340733a833c69ab4858541be82876f  internal/testutil/runfixture_test.go
```

Both hashes are identical to master's copies (`git hash-object` vs
`git rev-parse dd82af4a:<path>`), so the eventual merge is a no-op for them. The
carry is a *transit*, not a second definition (Rule 14/13): no near-copy was
written, and nothing in the tree gained a second bounded-runner shape.

### 2.2 The conversions

`FixtureCommand` / `FixtureCommandEnv` = 90 s budget (`fixtureCommandBudget`) +
5 s `WaitDelay`. `FixtureCommandEnv` is used wherever the child needs its **own**
environment (the fake-ssh peer map, the node identity, the empty whitelist, the
sandboxed settings path) — the child's environment is byte-for-byte what it was.

| file | sites | before | after |
|---|---|---|---|
| `cmd/agent/cli_memory_sync_test.go` | 11 | 11 unbounded | **0** |
| `cmd/agent/cli_memory_sync_every_test.go` | 3 | `CombinedOutput` x3 | **0** |
| `cmd/agent/cli_memory_sync_oneway_test.go` | 4 | `CombinedOutput` x4 | **0** |
| `cmd/agent/agent_spawn_guard_test.go` | 5 | `CombinedOutput` x5 | **0** |
| `cmd/agent/agent_spawn_guard_env_isolation_test.go` | 1 | `CombinedOutput` x1 | **0** |

No assertion was weakened (Rule 15/30): every caller still fails on a non-nil
error, every output assertion is unchanged, and nothing was converted into a
"the fixture simply worked" verdict. Two sites in
`cli_memory_sync_every_test.go` had already been converted by the sibling branch;
their call shape here is identical, so the merge conflicted only in comments.

**Two deliberate exceptions, both classified in §3:**

- `runGopiInDir` (`agent_spawn_guard_test.go:371`) keeps `CombinedOutput`: its
  contract carries a per-child **working directory** (`cmd.Dir`) and **stdin**,
  which `FixtureCommand`/`FixtureCommandEnv` deliberately do not model (they are
  the one-shot capture-only shape). Its callers — `cli_tasks_worker_unknown_arg_test.go`,
  `peak_guard_removal_test.go` — depend on the pinned cwd. An earlier attempt
  converted it and silently dropped both, which the callers would have caught at
  run time; the revert is recorded in the code as a comment.
- The `--every` **loop** child (`cli_memory_sync_every_test.go:470`) keeps its own
  bound (`cmd.Start` + `cmd.Process.Signal(SIGTERM)` + `cmd.Wait`): it is
  deliberately long-lived, so the one-shot 90 s fixture budget is the wrong shape
  for it.

### 2.3 A real bug the conversions exposed: `asExitError` did not unwrap

`exitCodeOf` (`agent_spawn_guard_test.go`) resolved the exit code with a **bare
type assertion**:

```go
func asExitError(err error, target **exec.ExitError) bool {
	if ee, ok := err.(*exec.ExitError); ok { … }
}
```

The bounded runner wraps the child's own error with `%w` — on purpose, so the
cause survives (Rule 16). A bare assertion cannot see through that, so
`TestMemorySyncTimeoutFlagReachesTheLeg` failed with
`process failed to start: … exit status 1` for a **perfectly normal non-zero
exit**. `asExitError` is now `errors.As(err, target)` — which is what its name
and its ten package call sites always meant. `Run`/`ExitCode` are still asserted,
so no check was relaxed: the change only removes *false* "failed to start"
verdicts.

### 2.4 The SIGSTOP red proof

`internal/testutil/runfixture_sigstop_test.go` is **new and additive** — the
carried pair is untouched, preserving byte-identity with master. The sibling red
proofs cover a child that is merely **slow** (a plain `sleep`); the live suite
met something harder: a child the kernel has **STOPPED** — alive, making no
progress, unreachable by any signal it could act on. The proof asserts by
measurement:

1. `/proc/<pid>/stat` really reports state **`T`** (the observation polls for the
   transition — `kill(2)` does not update task state synchronously — but the
   assertion is unchanged);
2. under that stop, the bounded call still **returns** inside
   `budget + WaitDelay + slack`, with an error naming the child's **argv** (which
   carries the run's own pid-file path) and the **budget** it exceeded, plus the
   output captured so far;
3. the killed child is really **reaped** (its `/proc` entry disappears), so the
   proof leaves no orphan holding a pipe or a worktree.

The proof child publishes its pid then `exec`s the sleeper, so it has **no
descendant**: killing it cannot leak a grandchild into the suite's live-child
sweep. Rule 27: nothing here generates load — it reads `/proc` and signals
exactly one child.

## 3. The classification sweep (the epic's deliverable 3)

`grep -c CombinedOutput cmd/agent/*_test.go` post-conversion: **72 hits**, in
three classes. Every hit is either converted or carries the reason it is not a
bounded one-shot fixture child.

### (a) Not a `gopi` fixture child — a plain development tool

| first arg | hits | reason |
|---|---|---|
| `"git"` | 30 | git plumbing (`init`/`commit`/`log`/`show`/`rev-parse`) driven to build a repo fixture; no `gopi` process, no DB, no device-backed fsync. |
| `"go"` | 10 | `go build` / `go vet` / `go env` — the toolchain, invoked on purpose inside the test. |
| `node` | 3 | the web page harness (`cli_web_page_harness.js`). |
| `"bash"` | 2 | a shell driving the real binary as a launcher scenario. |
| `"sqlite3"` | 2 | a raw SQL probe against a fixture DB, not a `gopi` process. |

These are bounded by the tool's own lifetime, not by a fixture child of ours.

### (b) Not the one-shot capture shape — `CombinedOutput` is used but the call is separately bounded, or the helper's contract carries something the runner does not model

| site | why |
|---|---|
| `agent_spawn_guard_test.go:371` `runGopiInDir` | carries `cmd.Dir` + `cmd.Stdin` (see §2.2). |
| `cli_memory_sync_every_test.go:470` | the `--every` loop child; SIGTERM + `Wait` is its bound. |
| `borrowed_fd_worker_child_test.go:98` | the child is the **test binary itself** (`-test.run` probe) that must inherit an `ExtraFiles` FD; `ExtraFiles` is not modelled by the runner, and the site is inside the `fixture_spawn_inventory_test.go` covered class (`self`). |
| `merge_lock_e2e_test.go:284,370,436` | the whole test is about a **lock**: the child is expected to *contend* or be killed, and the surrounding helper already drives start/kill/wait explicitly. |
| `cli_tasks_diff_whitelist_test.go:133,171`, `cli_tasks_harvest_whitelist_test.go:46,85,125` | a fake-ssh shim + a settings/ledger fixture; these are the `Cmd.Env`-only shape and are candidates for a follow-up conversion — recorded here as **not yet converted**, not as "safe". |

### (c) Converted-eligible, mechanically identical, deliberately left for a follow-up

`cli_archive_test.go:214`, `cli_version_test.go:131`, `home_settings_test.go:115`,
`merger_test_rig_test.go:136`, `scratch_gc_stale_window_test.go:144`,
`settings_nilpanic_test.go:67`, `worker_daemon_flag_e2e_test.go:184`,
`worker_status_residue_e2e_test.go:63,172,240`, `cli_tasks_worker_test.go:161`,
`cli_verb_census_test.go:413`, `cli_tasks_sweep_zombies_test.go:180`,
`merge_run_test.go:130`, `version_identity_test.go:86`.

Each is a `Cmd.Env`-only one-shot `gopi` child of exactly the shape
`FixtureCommandEnv` handles, i.e. **convertible**. They were left out of this
branch for **blast radius**: they belong to unrelated suites (merge, worker,
scratch, version, settings), and this epic's subject is the memory-sync fixture
family. Converting them here would make this branch a repo-wide change whose
failure modes cannot be attributed to it. **They are the honest remaining gap**
and are named individually so a follow-up task needs no re-derivation.

## 4. How this was measured

- The acceptance surface was **executed**, not inferred:
  `env GOPI_TEST_TMPDIR=/home/user/workspace/tmp go test ./cmd/agent/ -run 'TestMemorySync' -count=1 -v -timeout 10m`
  → **PASS**, 58.6 s, with a live load of **14.49** caused by two real
  `gopi worker` daemons and a `gopi merge --every 5m` on the same host — the very
  contention that motivated the bound. The device arm is real:
  `stat -c %d /` = **2051**, `stat -c %d /home/user/workspace/tmp` = **2052**.
- The bounded runner's own red proofs run green
  (`go test ./internal/testutil/ -run 'TestFixtureCommand'`): the carried three
  plus the new SIGSTOP proof.
- An earlier run of the same surface (before §2.3) produced exactly **one** FAIL,
  `TestMemorySyncTimeoutFlagReachesTheLeg`, and it was a defect of the
  *conversion*, not of the assertion — recorded here because that is the shape
  this sweep exists to catch.
