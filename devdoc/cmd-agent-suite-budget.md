<!-- doc-class: historical -->
# cmd/agent's Package Budget — Measured, Then Reduced

**Epic**: `cmd/agent`'s test package cannot finish inside Go's 10-minute package
budget on this host. Not the timeout, not a skip: the package must EXIT 0 with
its full test set while a live merger + worker run.

**Branch**: `docs/cmd-agent-suite-budget` (this worktree).
**Date**: 2026-09-26. **Host**: 4 cores, load ~8 from 4 live `gopi` daemons
(3 `worker`, 1 `merge --every 5m`).

---

## 1. The baseline, measured (not quoted)

```
go test ./cmd/agent/ -count=1 -timeout 10m -v      # live merger + workers
FAIL	github.com/nick/gopi/cmd/agent	604.205s
```
- **2186 `=== RUN` entries / 1430 `--- PASS` entries**, **580.1 s of in-test
  time**, **zero failures**.
- The panic arrived with tests still queued: the package's own work exceeds the
  budget. There is no hang in this number (the previously reported 100 s
  `Waitid` stall is a separate, filed defect, task `01a0dc30-…`).

Go's package timeout is wall clock; the in-test sum is what the tests
themselves spent. The gap between 580 s and 604 s is the harness's own setup
(`TestMain`'s pins, the scratch sweep, the child/goroutine sweeps, the initial
binary build).

## 2. Per-category accounting

Attribution method: a call-graph tag over the package's 283 `_test.go` files —
each test function is tagged from its own body plus the bodies of the helpers it
names (one hop), then each test's measured `--- PASS` duration is summed by tag.
That one-hop shape is what this package needs: the builders
(`mdBin`, `wdBin`, `memorySyncE2EBin`, …) sit one call away from the test body,
so a file-level count reads a driver call as "in-process".

| category | time | share | n (top-level+subtests) |
|---|---:|---:|---:|
| B — child `gopi` CLI process (drive the built binary) | **196.3 s** | 33.8 % | 133 |
| G — in-process (no child process spawned) | **221.3 s** | 38.2 % | 1149 |
| F — other child process (`ssh` shim, shell, pty child, …) | 63.2 s | 10.9 % | 54 |
| C — git fixture child (`git init/add/commit/worktree`) | 61.0 s | 10.5 % | 50 |
| B′ — child CLI whose builder sits 2 hops away | 38.1 s | 6.6 % | 44 |
| **total** | **580.1 s** | 100 % | 1430 |

A coarser, file-level read of the same log attributes **292.9 s / 50.5 %** to the
204 entries whose FILE contains a `go build` of the package under test — the
difference is the point: at file granularity every sibling test in those 26
files inherits the build tag.

### Unit costs behind those categories (measured on this host)

| operation | cold | warm |
|---|---:|---:|
| `go build -o <tmp> .` (the package under test) | 26.9 s | 3.44–4.71 s |
| `go build -cover -o <tmp> .` | 44.9 s | 2.45 s |
| `gopi memory_store` into a FRESH ledger (schema + first open) | — | 0.49–0.96 s |
| … the same ledger reopened once warm | — | 8–9 ms (first reopen after creation: 205 ms) |
| `gopi task list` (ledger already open once) | 0.53 s | 12–14 ms |
| `gopi -h` (private home, no ledger) | — | 6–8 ms |

The fresh-ledger cost is the device, not the code: the suite's
scratch home is a real ext4 directory by operator ruling (nothing is mounted,
`testutil.ScratchHomeDir`), and each fresh DB's first open pays that device's
journal commits — the suite's own TestMain prints exactly this at startup. A
decomposed probe puts the ledger work ITSELF at ~0.4–0.9 s per fresh DB (the
schema batch plus its first open; reopening the same DB once warm is 8 ms) and
the remainder of a spawn at `gopi`'s own process start. `/dev/shm` was measured
at 16 ms per fresh open, but a tmpfs scratch home is RETIRED (operator ruling,
2026-09-18); Rule 22 keeps scratch off `/`, and the accepted cost is recorded,
not re-litigated. Prior attribution of the seconds to ledger first-open alone
was wrong for the same reason: it is one cost inside a child process, not the
category.

## 3. What was reduced

**The top offender was not "child processes" — it was ELEVEN separate `go build`s
of the same package.** `gopibin_test.go` already owns the ONE build slot
(`gopiTestBinPath` → `$GOPI_TEST_BIN` or a build into the suite scratch root,
cached per test binary), and the package's own history says the four original
copies of that helper were collapsed into it because duplicated build caches
made the binary each test executed a function of test ORDER. Ten more copies
were added afterwards, each justified in a comment:

| file | what it built | why it claimed it had to |
|---|---|---|
| `cli_memory_sync_test.go` | `go build` into `t.TempDir()`, per CALL | "builds the gopi binary once per run" — the code did not |
| `cli_tasks_worker_test.go` | own slot + build | "the cache is PACKAGE-level, a t.TempDir() would be removed" |
| `cli_daemon_cwd_test.go` | own slot + build | "other tests drive that binary with their own build flags" |
| `cli_tasks_report_seam_test.go` | own slot + build | "a silent skip reports a green seam that never ran" |
| `merger_test_rig_test.go` | own slot + build | "a t.TempDir() binary would be unlinked under a live daemon" |
| `task_lifecycle_tick_test.go` | build per call | same lifetime argument |
| `worker_daemon_flag_e2e_test.go` | own slot + build | same lifetime argument |
| `peak_guard_removal_test.go` | own slot + build | "a skip reports a green removal that never ran" |
| `write_tool_ownership_e2e_test.go` | `bin/gopi` in the CHECKOUT | "the declared artifact must be demonstrated every run" |
| `cli_tasks_sweep_zombies_test.go` | own slot + build | the resolver-guard test asserts the build lands in the scratch root |

Every one of those requirements is met by the ONE slot, and none of them
conflicts with it: it builds into `testutil.SuiteTempRoot()` (so it outlives
each test — the detached-daemon `argv[0]` requirement), it is built from THIS
tree on every run (so the freshness claim in `assertGopiBinIsFresh` holds), it
keeps the plain and `-cover` artifacts in two distinct slots (so "another test's
build flags" cannot reseat it), and it FAILS a test that cannot build rather
than skipping it (the caller keeps its own skip/fatal policy). The one option it
does not offer is `CGO_ENABLED=0`; that is recorded under "Not Done".

The reduction is therefore: **eleven child `go build` sites → one**, and the
package's build is now spawned by a single call site instead of ten.

## 4. Verification

See the report's `## Verification` block for the exact invocations and outputs;
this devdoc records the numbers it produced.

## 5. Not Done (honest)

- The package's residual in-test sum is still larger than the budget suggests is
  comfortable; the reductions above remove the duplicated builds, not the
  package's fundamental shape (hundreds of tests that genuinely fork a real
  `gopi`).
- `cli_tasks_sweep_zombies_test.go` keeps its own build because a test in that
  same file asserts the resolved binary lies inside the suite scratch root. The
  shared slot satisfies that assertion, so folding it in is possible; it was left
  alone to keep this change's blast radius inside what was measured.
- `write_tool_ownership_e2e_test.go` no longer writes `bin/gopi` into the
  checkout (that was repository residue and, on a shared host, a second
  package's binary). Its `CGO_ENABLED=0` build flag is dropped with it.
