<!-- doc-class: historical -->
# Devdoc: the sweep-E2E resolver builds its own gopi binary

Task `01a0c049`. Branch `fix/tasklist-e2e-selfbuilt-bin` (own linked worktree
`~/workspace/gopi-e2e-bin`, from master `2fdc65e4`).

## The defect (root cause, not symptom)

`cmd/agent/cli_tasks_sweep_zombies_test.go`:

```go
func sweepZombiesRequireBin(t *testing.T) string {
    bin := os.Getenv("GOPI_BIN")
    if bin == "" {
        bin = filepath.Join("..", "..", "bin", "gopi")   // ← the defect
    }
    if _, err := os.Stat(bin); err != nil {
        t.Fatalf("sweep-zombies E2E needs a gopi binary: ...")
    }
```

Five whys:

1. The three callers fail in a fresh worktree → 2. the resolver needs a file
that is not there → 3. it falls back to `../../bin/gopi` → 4. `bin/` is
**gitignored**, so it exists only in a tree where somebody ran `make build`
(never in a fresh `git worktree add`) → 5. **the resolver depends on an ambient
build product instead of producing the one it needs.** `make build` is not a
prerequisite of `go test`, so "did you run make build?" is not a fixable
precondition — the dependency itself is the bug.

The second half of the failure is worse than a red test: on a shared host the
same resolver *passes* against **another tree's** `bin/gopi`, so the E2E
silently executes a binary that is not the branch under test. Observed live
2026-09-12 (see the comment retained in the file): the acceptance test "passed"
against the main repo's pre-fix binary with no failure signal.

Callers sharing the resolver (all in `cli_tasks_list_sweep_test.go` /
`cli_tasks_sweep_zombies_test.go`): `TestTaskListReadPathE2E`,
`TestSweepZombiesCLI_DryRunTouchesNoNotifySink`,
`TestSweepZombiesCLI_ReportNotifiesOperator`.

## The fix (surgical: the resolver's own file only)

`sweepZombiesRequireBin` now **builds what it needs** on first use:

- an explicitly set `GOPI_BIN` is still honoured (the operator's pinned binary
  wins, exactly as before) — but a set-and-unusable `GOPI_BIN` is now FATAL
  with its own message instead of being reported as "needs a gopi binary";
- otherwise `sweepE2EBinary(t)` runs `CGO_ENABLED=0 go build -o <scratch>/gopi .`
  **once per test binary** (`sync.Once`), into
  `filepath.Join(testutil.SuiteTempRoot(), sweepE2EBinDir)`.

Design choices, each against a specific rule:

| choice | why |
| --- | --- |
| `testutil.SuiteTempRoot()`, not the worktree | Rule 22/30 — build product lives OFF `/` (measured: scratch device `2052` vs `/` device `2051`) and outside the worktree, so `git status --porcelain` stays empty in EVERY tree; the suite's TestMain removes the run root, so the binary never outlives the run (measured: run dir gone after the run). |
| `CGO_ENABLED=0 go build` of `.` | reuses the Makefile's own target (`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`), not an invented one; `.` IS the binary because this test is in `package main`. |
| `exec.LookPath("go")` FIRST | a missing toolchain is reported as itself, never as an opaque exit status. |
| FATAL, never `t.Skip` | Rule 15 + the file's own history: a skipped E2E reads as a passing one in CI output — a green regression guard that never ran. |
| empty compiler output not interpolated | an ordinary missing-toolchain failure must not read as `error: ` with a blank body. |
| non-executable result refused | a `go build` of a non-main package leaves an archive; stamping that path fails much later and much less clearly ("permission denied" deep inside the E2E). |
| named constants (`sweepE2EBinDir`, `sweepE2EBinFileName`, `sweepE2EBuildOutputLimit`) | Rules 1a/1b/13 — no inline path or limit literals. |
| `t.Setenv("GOPI_BIN", abs)` | Rule 17 — never `os.Setenv`; the resolved path is pinned so a later `testutil.RunGopi` cannot re-resolve to another tree's binary. |

Precedent followed exactly: `reportSeamBinary` (`cli_tasks_report_seam_test.go`).

## The regression guard (deliverable 2)

`TestSweepE2EBinaryIsBuiltInTheSuiteScratchRoot` asserts the **resolved path**
lies inside `testutil.SuiteTempRoot()` and is an executable file.

Asserting on the path, not on caller behaviour, buys the arm a
fresh-worktree-only run cannot provide: **the guard fails in the MAIN repo too**
(where `../../bin/gopi` does exist and would resolve happily), so a revert to
the tree-relative fallback cannot hide behind "it works here".

**Red-proof** (mutation: the `else` branch restored to
`filepath.Join("..", "..", "bin", "gopi")`), in the worktree with no `bin/`:

```
--- FAIL: TestTaskListReadPathE2E (0.65s)
--- FAIL: TestSweepE2EBinaryIsBuiltInTheSuiteScratchRoot (0.00s)
    cli_tasks_sweep_zombies_test.go:253: the sweep E2E binary resolved to
    /home/user/workspace/gopi-e2e-bin/bin/gopi, OUTSIDE the suite scratch root
    .../suite-cmd-agent-run-1998617433: the resolver is depending on an ambient
    build product again. ...
REDPROOF_EXIT=1
```

The guard carries a `t.Skipf` when the ambient environment itself sets
`GOPI_BIN` (the resolver honours it by design, so the scratch-root claim does
not apply) — a VISIBLE skip with its reason, never a silent pass.

## Evidence (self-filed)

BEFORE — fresh linked worktree, no `bin/`, `GOPI_BIN` unset:

```
$ CGO_ENABLED=0 go test ./cmd/agent/ -count=1 -run 'TestTaskListReadPathE2E|TestSweepZombiesCLI_...'
--- FAIL: TestTaskListReadPathE2E
    sweep-zombies E2E needs a gopi binary: set GOPI_BIN to THIS tree's build
    ... — ../../bin/gopi: stat ../../bin/gopi: no such file or directory
```
(three callers failed identically)

AFTER — same worktree, still no `bin/`, `GOPI_BIN` unset:

```
=== RUN   TestTaskListReadPathE2E
--- PASS: TestTaskListReadPathE2E (2.70s)
--- PASS: TestSweepE2EBinaryIsBuiltInTheSuiteScratchRoot (0.00s)
--- PASS: TestSweepZombiesCLI_DryRunTouchesNoNotifySink (0.65s)
--- PASS: TestSweepZombiesCLI_ReportNotifiesOperator (0.93s)
PASS   ok  github.com/nick/gopi/cmd/agent  5.311s      (TARGETED_WALL=19.02s)
broader: go test ./cmd/agent/ -run 'TestTaskList|TestSweepZombies|TestSweepE2E' → ok 12.288s
ls bin → No such file or directory ; git status --porcelain → " M cmd/agent/cli_tasks_sweep_zombies_test.go"
(only the deliverable, committed below; nothing else in the worktree)
BUILD_ALL_OK ; gofmt -l cmd/agent/ → pre-existing only (cli_config.go,
serve_lock_replacement_test.go, worker_stop_forward_regression_test.go) ;
go test ./internal/standards/ -run TestGate[1-6] → ok 5.160s
```

Resolution + teardown, from inside the run:

```
during: /home/user/workspace/gopi-test-scratch/suite-cmd-agent-run-983483126/sweep-e2e-bin/gopi
        mode -rwxrwxr-x, 24168877 bytes
after : ls -d .../sweep-e2e-bin → 0 (the suite removed its run root)
```

Build cost: one build per test binary, **≈1.1 s** warm (full package link,
24.1 MB artifact); it is the same cost the sibling `reportSeamBinary` seam
already pays, and `sync.Once` keeps it to ONE per `go test` invocation however
many cases share the resolver.

## Which tests this made deterministic

The three resolver callers — `TestTaskListReadPathE2E`,
`TestSweepZombiesCLI_DryRunTouchesNoNotifySink`,
`TestSweepZombiesCLI_ReportNotifiesOperator` — are now green by construction in
any tree, worktree or main, with or without `bin/`, and execute a binary built
from the branch under test rather than whichever tree's prebuilt happened to
exist.

## Not Done (honest)

- Sibling ambient-bin resolvers in the same package are **not** touched (out of
  the epic's scope, which named the sweep-E2E resolver): `cli_tasks_rebuild_test.go`,
  `cli_tasks_rebuild_pass_test.go`, `task_session_cwd_freshness_scope_test.go`
  and `cli_web_align_test.go` still read a repo-relative `bin/gopi`
  (`filepath.Join(repo, "bin", "gopi")` / `../../bin/gopi`). Some of those stat
  it deliberately (freshness/guard semantics), so each needs its own reading
  before a fix; a fresh worktree may see them fail for the same class of reason.
  This is a dispatchable follow-up, not a claim of coverage.
- `cli_brew.go` / `cli_brew_test.go` untouched (task `01a0c032`'s landed fix).
- No change to `internal/testutil` (its `GopiBin` skip semantics are a different
  contract, and `gopibin_test.go` already owns the package-wide build cache).
