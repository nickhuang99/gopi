<!-- doc-class: normative -->
# The `cmd/agent` suite's real reds — and why they were reported as "no assertion line"

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Date:** 2026-09-17 · **Branch:** `fix/cmd-agent-suite-red` · **Worktree:** `~/workspace/gopi-agent-red`
**Base:** master `17cec86b` · **Host:** idle (`hostb-Laptop`, load 1–3, 8 cores)

## The report that started this, and what it actually was

Two independent sessions closed with the same claim:

```
go test ./cmd/agent/ -count=1
  branch tree:          FAIL github.com/nick/gopi/cmd/agent 482.724s
  pristine master:      FAIL github.com/nick/gopi/cmd/agent 445.639s
  "both with NO --- FAIL: line (a package-level budget red under host contention, load ~8-12)"
```

The absence of `--- FAIL:` was read as evidence of a wall-clock timeout. It was
not. **It was output truncation**: the agent's bash tool cut the transcript
before the FAIL block, and the summary line (`FAIL <pkg> <elapsed>`) is printed
*after* it. Both numbers were real readings of a genuinely slow, genuinely
**assertion-failing** run.

Measured on the idle host, with the raw stream redirected to a file (`go test
-json > stream.json` — nothing between the tool and the evidence):

| claim | measurement |
|---|---|
| "package-level budget red" | `grep -c 'panic: test timed out' stream.json` → **0** |
| "no assertion failures" | **16** `"Action":"fail"` test events, named below |
| "under host contention" | the same failures reproduce at load 1.3 |
| real elapsed | package event `FAIL … 423.968s` (matches the reported 445.6 / 482.7) |

So the earlier verdict named the wrong cause for a real red, and all 16
failures were left unexplained. This document is the correction: the suite does
**not** hang, does **not** trip Go's 10-minute package budget, and does not need
the budget widened. It needs six fixture/wiring drifts fixed.

**The transferable lesson** (stored as memory `#1849`): a package-level `FAIL`
with no assertion line is a *claim* — verify it from the `-json` stream
(`panic: test timed out` present or absent; per-test `Elapsed`), never from an
agent transcript's tail, and never by re-deriving it from the summary elapsed.

## The six causes (a correction to this document's own first draft)

The first draft of this file said "three causes" and stopped at the three
families `43dfe379` fixed. That was an undercount written before the second
commit existed, and it is corrected here rather than left standing (Rule 26:
an incomplete fix must be narrow, never wrong). Causes ④⑤⑥ are the same
*shape* as ①–③ — a test or assertion that outlived the machinery it described
— and were fixed in `45727d43`.

| # | cause | fixed in |
|---|---|---|
| ① | stale anchors onto deleted merger symbols + the landing never wrote the durable rebuild mark | `43dfe379` |
| ② | host model fanned two rungs of the detach ladder into one boolean | `43dfe379` |
| ③ | renamed tick marker `[merger] pass start` → `[merger] tick start` | `43dfe379` |
| ④ | detach asserted as `PPID==1` / `SID==pid` — forbidden by Rule 24, and false under a systemd scope | `45727d43` |
| ⑤ | renamed once-marker `single pass done` → `[merger] --once: single tick done` | `45727d43` |
| ⑥ | the flock case asserted session leadership (`getpgid == pid`) for ④'s reason | `45727d43` |
| ⑦ | **the flock case's "free at once" premise** — a real 1-in-3 flake, fixed after ④ exposed it | (this commit) |

### ④ The detach assertion was the very criterion Rule 24 forbids

`merged 1.43s TestWorkerDaemon_DetachesAndKeepsRunning` (before) asserted
`PPID==1` **and** `SID==pid`. Since the seam's ladder gained rung ① the daemon
is started by the transient service manager, so its parent is the user manager
(not init) and its cgroup leader pid is the runner's choice (never a session
leader). Both assertions failed against a daemon that had detached *more*
thoroughly than setsid can manage — measured: pid 1372993 alive, `SID=1354878`
= the suite's own session, cgroup genuinely outside `session-*.scope`. Rule 24
exists precisely because PPID and SID say nothing about the cgroup; the test
was asserting the forbidden proxy. Replaced with `cgroupscope.RelOfPID` +
`InSessionScope` — the package that owns that judgement — and an unreadable
cgroup is a **failure**, never a pass (Rule 15b).

### ⑤ and ⑥ The other half of the renamed-marker and detach families

`gopi task merger --once` prints `[merger] --once: single tick done`; the
singleton E2E still waited on the retired `single pass done`, so it reported a
healthy merger as failing to complete its pass. The line is now the named
constant `mergerOnceDoneLine` in `cli_tasks.go` and both sites reference it
(Rule 1b/14 — one definition for reader and writer). Cause ⑥ is ④'s rule
applied to the flock E2E, which asserted session leadership the same way.

---

### The first three, in detail

All three are drift left by **`126fad74` — "refactor(merger): ff-only or
park-and-file — no child, no wait, no state"** (2026-09-16), which deleted 24
test files and 6 production files (including `merger_verify_child.go` and
`merge_resolve_child.go`) and renamed the merger's tick boundary.

### ① Stale anchors onto deleted symbols — and an incomplete invariant

`cmd/agent/cli_tasks_rebuild_gate_test.go` failed with:

```
cli_tasks_rebuild_gate_test.go:54:  read merger_verify_child.go: open merger_verify_child.go: no such file or directory
cli_tasks_rebuild_gate_test.go:117: declaration "func mergerRunPass(" not found — was the function renamed? (update this test deliberately)
```

Both symbols died with `126fad74`. But the invariants those cases existed for
**survived the refactor** and were left pinned to nothing:

- **the landing must leave the durable rebuild mark** — the landing is the exact
  instant master advances, and the rebuild is a *later* step of the same tick
  (`mergerSweepRepo` reaches `serveRebuildAfterMerge` only after the whole branch
  surface has been decided). The gap between the two is a window a pass can die
  in, and that is the entire reason `internal/tools/rebuild_pending.go` exists.
- **the pass wiring** — the close loop's drain, the pass's defer arm, the
  pass-start drain, and the runner chain that reaches the pass.

After `126fad74`, **no production code wrote a mark at all**
(`grep -rn 'MarkRebuildPending' --include=*.go .` → the definition and two test
callers; zero production callers). The durable close loop the epic built had
become entirely inert. That is a genuine regression hiding behind a "stale test"
label — which is exactly why Rule 26 says a stale-looking red is investigated,
not deleted.

**Fix:** re-anchor the two cases at the same strength onto the symbols that
*replaced* the deleted ones (see the mapping table in the test file's header),
**and** restore the missing production half: the `--ff-only` landing in
`repoSweepDecideBranch` now calls `MarkRebuildPending(repo,
MarkRebuildAfterLandingReason)`.

### ② A host model that fanned two rungs of one ladder into a boolean

Five cases in `cmd/agent` failed like this:

```
daemon_cgroup_e2e_test.go:237: an unavailable escape must be announced by the parent, got:
    gopi worker: cgroup escape: systemd-scope (/usr/bin/systemd-run)
daemon_cgroup_e2e_test.go:240: with no escape available, /proc/1363004/cgroup = "/user.slice/…/gopi-daemon-worker-001-….scope":
    the cgroup must still be the session scope (a different answer means the test's model of the host is wrong)
```

The test's own last line names the defect exactly. `escapable` was computed as

```go
escapable := cgroupscope.Available(os.Getuid()) == nil && !cgroupscope.Off()
```

`cgroupscope.Available` answers for the **in-process cgroupfs rung only**. The
seam's ladder is `① systemd-run --scope --user → ② the in-process escape → ③ an
announced setsid-only degradation`, and **on this host rung ① wins** (the user
bus answers; `Available` returns `ErrNoEscape` because
`/sys/fs/cgroup/user.slice/user-1000.slice` is `root:root 0755`). Production
therefore escaped correctly while the test took the "cannot escape" branch and
asserted against a daemon that *had* escaped.

**Fix:** read the host state from the seam itself —
`daemonScopeDecide(nil).scopeEscaped()` — the same decision production makes
before a detach. A test can then never disagree with the binary it drives.

### ③ A renamed log marker

```
merger_daemon_flag_e2e_test.go:348: daemon log …/merger.log never showed a pass
```

`126fad74` renamed the merger's tick boundary from `[merger] pass start` to
`[merger] tick start`. The E2E still waited on the old spelling and burned its
entire `mdLogWait` (20s) against a perfectly healthy daemon. The *reader* of
that marker (`internal/tools/merger_schedule.go`) had already learned both
spellings (`mergerTickMarker` / `mergerTickMarkerLegacy`); the test had not.

**Fix:** the scheduler now exports `MergerTickMarker` / `MergerTickMarkerLegacy`
(aliases, not second copies), and the E2E waits through those — one definition
for reader and writer (Rule 14).

## Measured cost, before and after

`go test ./cmd/agent/ -count=1 -json`, idle host, `-count=1` (no cache):

| | before (`17cec86b`) | after (`45727d43`) |
|---|---|---|
| package result | **FAIL 423.968s** | PASS |
| failing tests | **16** | **0** |
| slowest test | `TestMergerRestart_IdleExitsAtOnce` 61.27s | (unchanged — real work, not a bug) |
| timeout panics | 0 | 0 |

Of the 16 failures, **10 are the six causes** above. The other **6 are NOT
bugs — they are the suite's stale-binary freshness gate firing on my own
measurement setup**: these E2E cases drive the BUILT `bin/gopi` and refuse to
run when a source file is newer than that binary, which is exactly what happens
while a fix is being written. (This document's first draft listed them as
"setup: ②" — a mis-attribution, corrected here after reading their actual
failure lines out of the `-json` stream: `bin/gopi (mtime …) is OLDER than
…internal/tools/repo_sweep.go — this test drives the BUILT binary`. A table that
blames the wrong cause is worse than no table.) Rebuilding `bin/gopi` before the
run removes them; the honest count of distinct causes is **6**.

| test | before | after |
|---|---|---|
| `TestVerifyGateLandingCarriesTheDurableCloseLoop` | FAIL 0.00s | renamed → `TestLandingLeavesTheDurableRebuildMark` PASS |
| `TestCloseLoopWiringIsComplete` | FAIL 0.00s | PASS |
| `TestWorkerDaemon_LeavesTheLoginSessionScope` | FAIL 22.15s | PASS 5.04s |
| `TestMergerDaemon_LeavesTheLoginSessionScope` | FAIL 21.06s | PASS 2.63s |
| `TestMergerDaemon_DetachesAndSecondIsRefused` | FAIL 21.40s | PASS 3.49s |
| `TestWorkerDaemon_RequireScopeIsHardWhenUnavailable` | FAIL 0.07s | PASS 2.79s |
| `TestUserBusSeam_RequireScopeRefusalCarriesTheDiagnosis` | FAIL 0.08s | PASS 2.49s |
| `TestWorkerDaemon_DetachesAndKeepsRunning` | FAIL 1.43s | PASS (④ — was asserting `PPID==1`) |
| `TestMergerFlock_DaemonLockStopsWithTheDaemon` | FAIL 0.97s | PASS (⑥ — was asserting `getpgid==pid`) |
| `TestMergerSingleton_TwoConcurrentInvocations` | FAIL 4.45s | PASS (⑤ — was waiting on `single pass done`) |
| `TestWorkerExecutorChildStandsInTheTasksRepoNotTheLaunchers` | FAIL 0.00s | PASS (stale-binary gate, not a bug) |
| `TestWorkerServesEachTaskInItsOwnRepoAcrossConsecutiveRounds` | FAIL 0.00s | PASS (stale-binary gate, not a bug) |
| `TestWorkerChildDoesNotInheritTheDaemonsSessionHandle` | FAIL 0.00s | PASS (stale-binary gate, not a bug) |
| `TestTaskRunStandsInTheSessionDirAndHandsTheWorkdirToTheChild` | FAIL 0.00s | PASS (stale-binary gate, not a bug) |
| `TestFrontRunConsoleOutputLandsInItsSessionDir` | FAIL 0.00s | PASS (stale-binary gate, not a bug) |
| `TestTmpDirSettingsValueReachesTheSpawnedChild` | FAIL 0.00s | PASS (stale-binary gate, not a bug) |

**Lesson for the next measurement:** `bin/gopi` is part of the test fixture, not
just a build artifact — a `-json` run taken while source files are newer than it
reports unrelated E2E failures. Rebuild first (`CGO_ENABLED=0 go build -o
bin/gopi ./cmd/agent/`) or the failure list mixes real bugs with a freshness
gate.

Note the wall-clock effect of ③: the two merger E2E cases spent `mdLogWait`
(20s) each waiting for a marker that could never appear; they now complete in
~3s each. The suite gets faster as a side effect of telling the truth.

## Red proofs (Rule 10.5 / Rule 27-compliant: no load, no thresholds)

Every fix carries a falsification actually executed, not asserted:

1. **Landing mark** — deleting the single `MarkRebuildPending(...)` line from
   `repo_sweep.go` turns BOTH the source-level case and the new fixture-level
   case red, with the message naming the consequence:
   - `TestLandingLeavesTheDurableRebuildMark`: "…does not leave the durable mark…"
   - `TestRepoSweepLandingLeavesTheDurableRebuildMark` (real git repo, real
     `MergerRepoSweep`): `RebuildPendingRepos() = [], want exactly [/…]`.
   Restoring the line greens both.
2. **Host model** — the value now comes from `daemonScopeDecide`, so the
   "escapable" premise and the binary's own behaviour cannot diverge; the five
   cases that asserted the wrong world pass and the other world (no escape) is
   still asserted on a host that cannot escape (the code path is unchanged, only
   which branch this host takes).
3. **Marker** — the E2E now waits on the string the daemon actually prints; the
   `mdWaitLogContainsEither` helper keeps the legacy spelling legal input for a
   pre-`126fad74` binary.

## What was deliberately NOT done

- **No test deleted, no test skipped.** The two stale cases were re-anchored
  (with the mapping recorded in the file header) — a silent `t.Skip`, or a
  deletion, would have hidden cause ① and left the inert close loop in place.
- **No budget widened.** `scripts/test-budget.sh` (`test_budget="25m"`) is
  untouched: the suite never approached Go's 10m default (424s under the worst
  measured contention), so widening it would have hidden nothing and fixed
  nothing.
- **No `t.Parallel` introduced**, no device load generated, no
  `/proc/pressure` consulted (Rule 27).
