<!-- doc-class: historical -->
# The fork→execve window in a cmdline read — measured window, stale red, and the guard

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task: `01a12df6-a237-7a1a-a1a0-37da52723a54` (branch `task/01a12df6a2377a1aa1a037da52723a54`).
Host: hosta. Date: 2026-10-12.

## 1. The reported defect, and why it is STALE

An intermittent red of `TestRealWorkerIncarnationMatchesProcStat` was filed:

```
2026-10-12T09:37:03.544383369+08:00 |     procname_triple_redproof_test.go:233: cannot read the cmdline of the probe process 2968646
2026-10-12T09:37:03.54466612+08:00  | --- FAIL: TestRealWorkerIncarnationMatchesProcStat (0.00s)
2026-10-12T09:38:36.476828354+08:00 | FAIL	github.com/nick/gopi/internal/testutil	116.994s
```

(stream `/home/user/workspace/gopi-test-scratch/queue/test-budget/run-1791768997-2965728.json`;
a second pid `2974687` appears in the session's own evidence file.)

The message text is the **pre-fix wording**, and the line number is where it lived:

| rev | date | `awaitProbeCmdline` | bare `Cmdline(procnameRootDefault())` |
|---|---|---|---|
| `2dda4b0bf` (the red's HEAD) | 09:38:23 | 0 | 1 |
| `6c2a254ef` (the fix) | 10:43:04 | 6 | 0 |
| `master` / this worktree HEAD | — | 6 | 0 |

`git merge-base --is-ancestor 6c2a254ef master` → **exit 0**. `git merge-base --is-ancestor 6c2a254ef HEAD` →
**exit 0**. `git merge-base --is-ancestor 6c2a254ef 2dda4b0bf` → **exit 1**.

So the red is a stale observation of code that no longer exists, and the fix is on `master` —
not stranded on an unmerged branch. The dispatching epic's premise ("still flaky") is **falsified**.

A second, independent argument that needs no git at all: the failing test reported `(0.00s)`.
The fix's retry budget is `CmdlineAwaitAttempts × CmdlineAwaitStep` = 64 × 2ms = **128ms**, so a
post-fix failure could not report `0.00s`. The red predates the retry, measured.

## 2. The mechanism, MEASURED — not quoted from a comment

`exec.Command(...).Start()` returns once the **fork** is done; the child may not have reached
**execve**. Reading `/proc/<pid>/cmdline` in that window answers "no argv" for a process that is
perfectly alive. Measured with the production reader `internal/procname` over 200 spawns:

| observation | count |
|---|---|
| `Start()` error (probe never started) | **0/200** |
| `/proc/<pid>/stat` starttime unreadable | **0/200** |
| `/proc/<pid>` directory missing | **0/200** |
| cmdline **file read error** | **0/200** |
| cmdline readable but **no argv** | **89/200 (44.5%)** |
| process state while the window is open | **`R`** (200/200) |
| raw cmdline bytes while the window is open | **0** in 37 cases, **exactly 8** in 52 |
| retry failures with the shared budget | **0/200** |
| retry attempts actually used | **1.00** average |
| window until argv answers | mean **39.7µs**, max **59.15µs** |

A second, independent 200-trial run reproduced the window at **22/200 (11%)** — the rate is
host- and load-dependent (1 in 2 to 1 in 9), never negligible.

The `state=R` row is the mechanism, not narration: the pid the read just denied argv for is a
**runnable, live** process. The two byte counts are the kernel's fork-time junk — 0 bytes, or the
comm padded to `TASK_COMM_LEN` (16) and truncated to `PID_STR_LEN` (8) by
`copy_comm_to_cmdline`, which is what fills `cmdline` *before* `begin_new_exec` replaces it.
The 8-byte cases are the ones that make `procname.Cmdline` return `ok=false`: it reads a
16-byte buffer and treats a short read as "no evidence".

## 3. The verdict on (a)/(b)/(c)

- **(b) probe startup failure — RULED OUT**: `0/200` `Start()` errors.
- **(c) environment / procfs visibility — RULED OUT**: for the same pid at the same instant the
  read said "no argv", the pid directory existed **200/200** and `stat`'s starttime was readable
  **200/200**. A process that is invisible to procfs would fail *those* too. And the red's own text
  proves it twice over: the pre-fix code reads the **starttime first** and would have reported
  `cannot read the starttime of the probe process` had the process been invisible — the observed
  message is the *cmdline* one, so the process was visible and the starttime read had succeeded.
- **(a) the fork→execve race — CONFIRMED, with the counterfactual**: the bare single read returned
  no argv in 89/200 (44.5%) and 22/200 (11%) trials, while the same read under the shared bounded
  retry failed **0/200** and averaged **1.00** attempt.

## 4. The guard

`internal/testutil/procname_spawn_cmdline_gate_test.go` refuses the exact pre-fix shape: a test
file that calls `procname.Cmdline(...)` once and turns that call's `ok` into a test-fatal call with
nothing in between. Three parts:

- `TestBareSpawnedCmdlineReadIsGated` — the green run over `internal/` + `cmd/`. It anchors its own
  coverage: it fails if the scan did not reach `procname_triple_redproof_test.go`, the file whose
  pre-fix content caused this red (a clean verdict from a scan that missed the carrier says nothing).
- `TestBareSpawnedCmdlineReadGateRedProof` — drives the gate with the **pre-fix source bytes**
  (transcribed verbatim from `2dda4b0bf`, lines 233–235) and requires exactly ONE violation; then
  drives the escape-annotated and post-fix shapes and requires zero. Red before green, measured.
- `TestAwaitProbeCmdlineBudgetCoversTheMeasuredWindow` — keeps the retry budget above the measured
  window, so a future edit that trims the budget past the window it exists for is refused.

**Honest scope (Rule 15).** It is a SHAPE detector, deliberately narrow. It is blind to a re-spelling
of the same defect: the read split across two statements, an interposed helper returning `ok`, or a
non-fatal report. It guards the regression that already happened; it is not an invariant. The audit
of the remaining call sites carries the other half and is prose a reader must judge.

### The audit of the remaining bare `procname.Cmdline` call sites

| site | just-spawned? | verdict | why |
|---|---|---|---|
| `scratch/owner_record.go:171` | no | **safe** | audits this process's own pid (a `walk` sibling writes it) |
| `scratch/units_registry.go:281` | no | **safe** | records a `gopi task run` child that is invoked later |
| `tools/task_finish_cleanup.go:3352` | no | **safe** | diagnostics for a *foreign* long-standing pid |
| `testutil/procname_triple_redproof_test.go:396` | yes | **safe** | the `awaitProbeCmdline` retry body — the fix itself |
| `testutil/procname_triple_redproof_test.go:78` | no | **safe** | reads the hand-built fixture tree, not a process |
| `scratch/owner_verdict_test.go:55` | yes | **safe in effect** | the pid came from `cmd.Start()`, but a *miss* is benign here: the record stores a short/absent cmdline and the test asserts on the pid/ticks factors, so it cannot go red this way |
| `cmd/agent/cli_scratch_gc_explain_test.go:56` | yes | **safe in effect** | the pid is a **dead, reaped** fixture; the test does not assert on `cmdText` |

Only the `procname_triple_redproof_test.go:78` site is annotated with the gate's inline escape
(`// cmdline-race-ok: …`), because it is the one bare read in a file the gate parses whose *reading*
is asserted; the other two just-spawned sites are not in a parse path that trips the gate.
