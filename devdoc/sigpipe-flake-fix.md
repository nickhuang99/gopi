<!-- doc-class: historical -->
# Task Report: Fix TestTaskRunSurvivesClosedStdoutPipe SIGPIPE flake (bug t-1788305178 domain)

## Completed

Root-caused and fixed the pre-existing broken-pipe flake in
`TestTaskRunSurvivesClosedStdoutPipe` (cmd/agent/cli_tasks_test.go).

**Root cause (verified by code ordering + repro):** the process killed by
SIGPIPE in this test is the **wrapper** (`gopi task run`), not the child.
The wrapper's `signal.Ignore(syscall.SIGPIPE)` sat at cli_tasks.go L1007 —
AFTER `TaskStart` flipped the row to `in_progress` (the test's trigger to
close the pipe) and AFTER the claim-lock prints / `"running gopi with task
... as briefing..."` print. When the test slammed the pipe read-end at
`in_progress`, the wrapper's next stdout write (claim-lock/briefing lines)
hit the closed pipe BEFORE the ignore ran → Go raised SIGPIPE on fd 1/2 →
default disposition killed the wrapper (exit -1, "signal: broken pipe").
Race window ≈ between TaskStart and L1007. This is a genuine wrapper-side
race the original d3731cb fix (whose own comment admitted the ignore was
meant to protect "claim lines, writeback") left open.

**Fix:** hoisted `signal.Ignore(syscall.SIGPIPE)` to the TOP of
`runTaskRunOnce` — the single funnel for the plain `task run` wrapper, the
serve daemon and the worker loop — so it runs BEFORE any claim/start/print.
Every write the function makes after that point is EPIPE-harmless. The
mid-function ignore (and its long comment) was removed; the child-side
ignore in main.go (gated on GOPI_TASK_WRAPPER_ID) is unchanged and remains
the child's protection (Go resets SIGPIPE to default at exec).

**Additional verification finding (test-harness pitfall):** the test spawns
`bin/gopi` from the module root (`testutil.GopiBin`) — a PREBUILT binary,
not a fresh build. `go build ./...` does NOT refresh it. Initial post-fix
runs still failed 6/10 because they exercised the stale pre-fix binary.
Rebuilding with `CGO_ENABLED=0 go build -buildvcs=false -o bin/gopi
./cmd/agent` was required before re-running.

## Not Done

- Child-side race window (child startup between exec-start and main.go's
  GOPI_TASK_WRAPPER_ID-gated ignore) not separately addressed: the current
  failure signature was proven to be the WRAPPER, and after the wrapper fix
  the test is deterministic. main.go's ignore already sits at the very top
  of main() (before flag/env/subcommand handling), so no init() hoist was
  needed. If a future flake points at the child, the child's ignore could
  move to an init() in main.go, but that is not this bug.
- Pre-existing suites (testsuite/, internal/tools/ connectionOpener/memory
  #1172 hangs) not chased — out of scope per the epic.

## Verification

- Repro pre-fix: `go test ./cmd/agent/ -run 'TestTaskRunSurvivesClosedStdoutPipe' -count=5` → **5/5 FAIL** ("task run exited -1 … signal: broken pipe").
- Post-fix + rebuilt bin/gopi: `-count=10` → **10/10 PASS** (ok … 65.712s).
- Build: `go build ./...` OK; `go vet ./cmd/agent/` clean.
- Full `go test ./cmd/agent/`: `ok github.com/the operator/gopi/cmd/agent 165.086s` (PASS, exit 0).
- Commit: d3ea4fb (feat/briefing-summary-payload worktree — shared with main repo, no push needed).

## Files changed

- `cmd/agent/cli_tasks.go` — signal.Ignore hoisted to top of runTaskRunOnce; old mid-function ignore + comment removed (22 insertions, 16 deletions).
