<!-- doc-class: normative -->
# Bugfix Report: task run "signal: broken pipe" (t-1788305178)

Date: 2026-09-02 · Branch: `fix/task-run-broken-pipe` (worktree `~/workspace/gopi-fix-broken-pipe`)

## Symptom

`gopi task run t-1788305178` (CLI code-review child of the review epic) was
claimed 07:37:28 (claimed_by peer1:913343), FAILED 07:37:47:

```
result: gopi run failed: signal: broken pipe — run: elapsed=18s
```

First run of the task, dead ~18s in. The twin's resubmission could fail the
same way — root cause required.

## Root Cause

**The task-run child's stdout/stderr ARE the external launcher's pipe. When
the launcher closes the pipe read-end mid-run, the child's next stderr write
gets EPIPE → Go raises SIGPIPE on fd 1/2 → default disposition kills the
child.** The writeback then records a spurious FAILED even though the run was
healthy.

### Evidence chain (all read-only)

1. **The child was NOT dead from startup** (epic's evidence #1 was wrong):
   `/home/user/.gopi/sessions/4d410346-…jsonl` has **26 lines** — session_start
   + 3 full tool-use rounds (12 assistant / 8 tool_result / 3 request). The
   raw log shows 3 complete `message_stop` cycles and a 4th request being
   built. Death happened **while writing the 4th request's response stream**.
2. **Pipe topology (live /proc evidence, 07:52)** — a concurrent task run
   chain was still alive:
   - `hermes chat --cli --yolo` (PID 912964) held the pipe **read-end** (fd 22)
   - `bash -lic "set +m; cd … && gopi task run … 2>&1"` (916712), the
     `gopi task run` wrapper (916721) and the child `gopi` (916733) all held
     **write-ends** of the same pipe `[21359030]`
3. **Launcher mechanism** (hermes-agent source): `terminal_tool` →
   `LocalEnvironment._run_bash` spawns `[bash, "-lic", "set +m; <cmd>"]` with
   `stdout=PIPE, stderr=STDOUT` (tools/environments/local.py:1035). The
   `_wait_for_process` poll loop (tools/environments/base.py) kills the whole
   process group AND closes the pipe on: interrupt (`is_interrupted()`),
   timeout, or `KeyboardInterrupt`/`SystemExit` (turn end / SIGTERM /
   session teardown).
4. **gopi side** (cmd/agent/cli_tasks.go:880-881): the wrapper pipes the child
   straight through — `cmd.Stdout = os.Stdout; cmd.Stderr = os.Stderr`. No
   intermediate buffer; the child writes directly into the launcher's pipe.
5. **Live reproduction**: a Go program writing to a pipe whose read-end closed
   dies **exit 141** (128+13 = SIGPIPE) — byte-for-byte the observed failure.

### Why the epic's workdir anomaly is a symptom, not the cause

The child's session workdir was `~/workspace/gopi-review-task-sync` (the
launcher's then-cwd), but the child's **3rd tool round correctly created the
cli-config worktree** (`git worktree add -b review/cli-config-options` — the
branch now exists). The wrong initial cwd is the launcher's env; it did not
cause the death.

## Fix

Two surgical changes (Rule 4):

1. **Child-side (the effective one)** — cmd/agent/main.go, top of `main()`:
   when `GOPI_TASK_WRAPPER_ID` is set (the bug057 pre-spawn seam the wrapper
   exports), `signal.Ignore(syscall.SIGPIPE)`. **Go resets SIGPIPE to default
   at exec** (verified empirically: a child spawned with the parent ignoring
   SIGPIPE still dies exit 141 on a closed-pipe write), so the wrapper's
   ignore does NOT reach the child — the child must ignore it itself. The
   guard is scoped to task-run children only: `gopi | head` top-level
   semantics are untouched.
2. **Wrapper-side (belt-and-braces)** — cmd/agent/cli_tasks.go `runTaskRun`
   before `cmd.Start()`: `signal.Ignore(syscall.SIGPIPE)`, so the wrapper's
   own post-spawn writes (claim lines, writeback) survive a vanished reader.

Effect: a closed launcher pipe makes writes return EPIPE (a harmless error
the stream code swallows) instead of killing the process — the run continues,
the writeback lands `completed`, no spurious FAILED.

## Regression Test

`TestTaskRunSurvivesClosedStdoutPipe` (cmd/agent/cli_tasks_test.go):

- MockLLM + isolated GOPI_HOME/GOPI_MEMORY_DB, submits `t-e2e-pipe`.
- Spawns `gopi task run` with stdout/stderr on a **real os.Pipe** (the
  launcher-reader shape), waits for the row to reach `in_progress`, then
  **closes the read end** mid-run — the launcher-vanished moment.
- Asserts the run **exits 0** and the row lands **completed**.

Verified both directions:
- Without the fix (stashed): `FAIL` — `signal: broken pipe`, row failed.
- With the fix: `PASS` — run completed, elapsed=1s, row `completed`.

## Completion Chain — the missing export link (2026-09-02, second half)

PA's gap analysis ("wrapper NEVER exports GOPI_TASK_WRAPPER_ID") was based on
a grep that predated the merge of `feat/bug057-two-row-linkage`
(`8ad11ef`): the export line was already present on the branch via that
ancestor —

```go
// cmd/agent/cli_tasks.go runTaskRun, just before cmd.Start():
cmd.Env = append(os.Environ(), tools.TaskWrapperEnv+"="+target)
```

`git merge-base --is-ancestor 8ad11ef d3731cb` confirms the export is in the
tree, and the value exported IS the wrapper row's task id (`target`) —
exactly what `internal/agent/taskregister.go:121-127` reads
(`os.Getenv(tools.TaskWrapperEnv)` → mirror-row `wrapper:<task-id>` tag).
Both gates read the same constant (`tools.TaskWrapperEnv`), Rule 2: one
writer (runTaskRun), one reader per consumer.

**The real remaining gap was a STALE BINARY, not source.** The regression
test `TestTaskRunSurvivesClosedStdoutPipe` spawns the REAL `bin/gopi`
(testutil.GopiBin → module-root bin/gopi), and that binary was built
**2026-09-02 08:18 — 5 hours before d3731cb (13:37)**. Running the test
against the stale binary gave exactly PA's observed failure:

```
--- FAIL: TestTaskRunSurvivesClosedStdoutPipe (2.01s)
    cli_tasks_test.go:494: task run exited -1 (want 0 — SIGPIPE death leaves 141); err=signal: broken pipe
```

After `go build -o bin/gopi ./cmd/agent/` (rebuild from d3731cb):

```
--- PASS: TestTaskRunSurvivesClosedStdoutPipe (2.83s)
```

Pitfall (E2E-with-real-binary hygiene): a code fix on a branch does NOT reach
the E2E tests until `bin/gopi` is rebuilt — always rebuild before running any
test that spawns GopiBin. bin/gopi is gitignored (02fcfa2), so the stale copy
was invisible to git status.

## Verification

- Build: `go build ./cmd/agent/` OK; `go build -o bin/gopi ./cmd/agent/` OK.
- Regression: `go test ./cmd/agent/ -run TestTaskRunSurvivesClosedStdoutPipe` PASS
  (2.83s) — FAILs against the pre-rebuild stale bin/gopi (2.01s, exit -1 /
  signal: broken pipe), proving the test is a real guard for the binary too.
- Full package: `go test ./cmd/agent/` → `ok ... 86.848s` (whole package green,
  not just the new test).
- Disposition A/B (clean `trap - PIPE` shell + minimal env, read /proc/<pid>/status
  SigIgn bit 13 while a `task serve` child was alive):
  - TOP-LEVEL (no env): `SigIgn=0000000000000000` — SIGPIPE default disposition
    preserved; `gopi | head` death semantics untouched (acceptance #4).
  - CHILD (`GOPI_TASK_WRAPPER_ID` set): `SigIgn=0000000000001000` — SIGPIPE
    ignored, exactly the gate in cmd/agent/main.go:54-56.
  - Earlier inconclusive runs traced to TWO probe artifacts: (a) inherited
    SIG_IGN from the agent tree (Go preserves a start-time SIG_IGN through
    exec — clean disposition requires `bash -c 'trap - PIPE; exec …'`);
    (b) small CLI outputs (< 64KB pipe buffer) never generate an EPIPE at
    all, so `| head` exit-code probes of short help text prove nothing.
    Short-output probes must check SigIgn, not EPIPE survival.
- Wrapper-side pre-spawn prints (claim lines, claim-lock protocol, writeback)
  are covered by the wrapper's own `signal.Ignore(syscall.SIGPIPE)` in
  runTaskRun (cli_tasks.go:927) — present in d3731cb, exercised by the same
  regression test (the wrapper writes its claim/broadcast lines into the same
  pipe the test closes).
- Commit: see git log on `fix/task-run-broken-pipe` (worktree = shared with
  main repo, no push needed).

## Not Done / Notes

- The **hermes side** (launcher kills the group on turn end / SIGTERM) is a
  separate product decision — this fix makes the gopi task-run path resilient
  regardless of launcher behavior. Hermes could additionally stop killing
  detached task runs, but that is out of this repo's scope.
- The epic's claim that "the session JSONL contains ONLY session_start" was
  incorrect; corrected here (the JSONL has 26 lines / 3 tool rounds).
