<!-- doc-class: historical -->
# Task Report: the serve branch-scan records the branch tip at mint time (arm ②)

Branch: `feat/scan-tip-at-mint` · worktree `/home/user/workspace/gopi-scan-tip`

## Completed

1. **Single source for the tip line** (`internal/tools/tasks.go`, next to the
   existing `MergeBacklogScanSummaryVerb` alias):
   - `const MergeBacklogTipLine = mergeBacklogTipLine` — the exported alias of
     the ONE format definition (`"branch tip: %s"`, `task_merge_backlog.go`).
2. **Single authoring helper** (`internal/tools/task_merge_backlog.go`):
   - `func MergeBacklogPayloadTipLine(tip string) string` renders
     `fmt.Sprintf(mergeBacklogTipLine, tip) + "\n"`. The audit face's
     `mergeBacklogPayload` now calls it instead of typing `"branch tip: %s\n"`
     inside its own format string, and the cmd/agent scan calls the same helper —
     so there is exactly ONE place the line is authored and ONE place its format
     is defined.
3. **The scan stamps the tip at mint time** (`cmd/agent/cli_tasks.go`,
   `agentBranchScan` + new `branchTip`):
   - `branchTip(b)` resolves the branch's real tip and
     `tools.MergeBacklogPayloadTipLine(tip)` is PREPENDED to the minted payload.
   - Unresolvable ref → the row is minted WITHOUT the line and the degradation is
     reported on stderr (never a guessed sha, Rule 15).
4. **Tests** (names below).
5. This report.

### Exact tip-resolution command

```
git rev-parse --short <branch>
```

Run with `exec.CommandContext(ctx, "git", "rev-parse", "--short", branch)` from
the scan's cwd (the scanned repo), bounded by the pre-existing
`gitBranchListTimeout` (10s, Rule 1 — no new literal). `--short` matches the
shape the audit face records (`git log -1 --format=%h`), so both faces' tip
lines carry comparable shas. Cost: **one process per SUBMITTED branch** — the
call sits after the dedupe mask's `continue`, so covered branches pay nothing
and the per-pass count is bounded by the submits the pass actually makes.

### Why arm ② and not the ref

`internal/tools/task_merge_executor.go` deletes the merged branch and *then*
writes the row completed, so every success-terminal merge-backlog row has an
already-deleted ref at verification time. The terminal guard
(`fix/merge-backlog-terminal-guard`, in the worktree parked at
`~/workspace/gopi-d2-terminal-guard`) probes in order: ref → **recorded tip** →
archive tag → Unknown. Before this change the scan face recorded no tip, so a
scan-minted row could only reach Unknown → allowed-and-recorded.

### Payload byte-stability (dedup mask)

The mask keys on the SUMMARY (`TaskOpenMergeBacklogBranches` parses
`merge-backlog: %`), not the payload, so adding a payload line is inert for
dedup — verified by the existing `TestAgentBranchScan_TwoBranchesTwoRows`
(second pass dedupe-silent) plus the new test's re-scan assertion after the
branch is deleted, and by
`TestMergeBacklogScanSummaryVerb_SingleSource` (unmodified, still green).
The line is PREPENDED so the payload's human-readable first sentence
("Branch X sits unmerged…") is preserved for `task list --grep` previews.

## Not Done (honest)

- **No live end-to-end run against the real ledger.** Verification is
  package-level (`cmd/agent` + `internal/tools`), including the fixture repo
  scan and the delete-then-probe shape; the real serve tick was not exercised.
- **Remote-tracking scan rows (`peerbox/*`).** `listUnmergedBranches` also emits
  remote names; `git rev-parse --short <remote name>` resolves them in this repo
  (e.g. `origin/HEAD`-style names are filtered earlier, other remotes resolve),
  but an unresolvable one degrades to a tip-less row — covered by
  `TestBranchTip_UnresolvableIsReportedNotGuessed`, not by a real remote fixture.
- **The terminal guard itself is unchanged** — it lives on the sibling branch and
  parses the line via `fmt.Sprintf(mergeBacklogTipLine, "")`, which stays
  byte-identical: no cross-branch change was needed.
- No `<sha>` fallback for the archive-tag arm: out of scope here.

## Verification

| check | command | result |
|---|---|---|
| build (all packages) | `go build ./...` | BUILD_EXIT=0 (`.pkgs.txt`) |
| build (the test binary the E2E guards demand) | `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` | BUILD_EXIT=0 (`.pkgs2.txt`) |
| `internal/tools` | `go test ./internal/tools/ -count=1` | **ok 415.0s — PASS** (`.pkgs2.txt`: `ok github.com/nick/gopi/internal/tools 414.992s`, `TOOLS_EXIT=0`) |
| `cmd/agent` (full, single run, `GOPI_BIN` set) | `go test ./cmd/agent/ -count=1` | 15 FAIL, **none attributable to this branch**: 9 reproduce verbatim on a clean `909d4ca9` worktree (master's own stale gates + cgroup-e2e), 6 (the dispatcher/runner E2E) pass alone on this branch **and** on `909d4ca9` — shared-scratch/shared-`/dev/shm` contention in concurrent runs; see below |
| new: scan records the real tip (with the branch deleted afterwards) | `go test ./cmd/agent/ -run TestAgentBranchScan_RecordsBranchTipAtMint -count=1 -v` | PASS (0.54s) |
| new: unresolvable ref is reported, not guessed | `go test ./cmd/agent/ -run TestBranchTip_UnresolvableIsReportedNotGuessed -count=1 -v` | PASS (0.11s) |
| new: tip line single source + reader round-trip | `go test ./internal/tools/ -run TestMergeBacklogTipLine_SingleSource -count=1` | PASS |
| new: single-definition gate (no retyped format in any other production file) | `go test ./internal/tools/ -run TestMergeBacklogTipLine_SingleDefinitionGate -count=1` | PASS |
| scan-face regression set | `go test ./cmd/agent/ -run 'TestAgentBranchScan\|TestBranchTip\|TestListUnmerged\|TestArchiveTagTips\|TestIsBranchArchived\|TestServeBranchScanThrottle\|TestNotifyBranchPushed' -count=1` | ok 7.8s |

### The `cmd/agent` failures — measured against a clean baseline, not asserted

Every failure seen in a full-package run was re-checked by **running the same
selection on a detached worktree of master's tip at this branch's own parent,
`909d4ca9`** (code without this change at all), and then re-running the failing
selection on this branch **alone** (the earlier full runs had up to three
concurrent `go test` invocations, which is what Go's 10m *package* timeout
measures here, not the code).

**(a) Nine failures are master's own — reproduced on the clean `909d4ca9`
worktree, verbatim:**

```
$ git worktree add --detach /home/user/workspace/_base-scan-tip 909d4ca9
$ go test ./cmd/agent/ -run '<the nine>' -count=1
--- FAIL: TestVerifyGateLandingCarriesTheDurableCloseLoop (0.00s)
--- FAIL: TestCloseLoopWiringIsComplete (0.00s)
--- FAIL: TestWorkerDaemon_LeavesTheLoginSessionScope (36.64s)
--- FAIL: TestMergerDaemon_LeavesTheLoginSessionScope (20.79s)
--- FAIL: TestWorkerDaemon_RequireScopeIsHardWhenUnavailable (0.43s)
--- FAIL: TestUserBusSeam_RequireScopeRefusalCarriesTheDiagnosis (0.50s)
--- FAIL: TestMergerDaemon_DetachesAndSecondIsRefused (31.07s)
--- FAIL: TestMergerFlock_DaemonLockStopsWithTheDaemon (5.61s)
--- FAIL: TestMergerSingleton_TwoConcurrentInvocations (20.14s)
FAIL	github.com/nick/gopi/cmd/agent	118.703s
```

Root cause of the two gate failures, provable on master: commit `126fad74`
("refactor(merger): ff-only or park-and-file — no child, no wait, no state")
**deleted** `cmd/agent/merger_verify_child.go` and renamed `mergerRunPass`, while
`cmd/agent/cli_tasks_rebuild_gate_test.go:38/:54/:117` still reads that file and
declaration — master's own gate is stale against master
(`git merge-base --is-ancestor 126fad74 master` → true, and
`git ls-tree master -- cmd/agent/merger_verify_child.go` → empty).

**(b) The six dispatcher/runner tests are contention, not this branch.** On the
clean `909d4ca9` worktree they are green; on this branch they are green **too,
run alone**:

```
$ cd /home/user/workspace/gopi-scan-tip && go test ./cmd/agent/ \
    -run 'TestCLIPositionalSubmitRunsThroughDispatcherE2E|TestTaskRunChildReceivesSummaryAndPayloadE2E|\
TestTaskRunSingleRowE2E|TestTaskRunSurvivesClosedStdoutPipe|TestTaskRunChildNonZeroExitClosesWrapperRowE2E|\
TestTaskRunNormalExitSingleRowE2E' -count=1
ok  	github.com/nick/gopi/cmd/agent	0.645s          # BASELINE 909d4ca9: ok 2.620s
```

They failed in the concurrent full run on `task_submit: repo "…" is not a git
work tree` / `no --payload given` — i.e. the /dev/shm fixture's git context and
scratch state, a shared-scratch effect, not a payload-shape change: the three
test files they live in (`cli_tasks_test.go`, `cli_tasks_submit_payload_test.go`,
`cli_tasks_waitpid_test.go`) are **byte-identical to `909d4ca9`**
(`git diff 909d4ca9 -- <those files>` → empty), and the changed code
(`agentBranchScan`, `branchTip`) is not on their path.

**(c) The 10m package timeout.** Both packages hit Go's package budget once
while the three full runs overlapped (`internal/tools` 602s, `cmd/agent` 600s,
timing out in *different* tests than the baseline hit). Run alone, `internal/tools`
completes and passes:

```
$ go test ./internal/tools/ -count=1
ok  	github.com/nick/gopi/internal/tools	414.992s        # TOOLS_EXIT=0  (.pkgs2.txt)
```

This is the known fixture-cost/contention effect recorded in
`devdoc/heavy-tests-rule27-gate.md`. **Per Rule 27 no load was manufactured to
reproduce it** — the instrument was the suite's own wall time and the failure
selection, never a synthetic device writer.

No failure in either package names the changed area
(`BranchScan|BranchTip|MergeBacklogTipLine`), and every test this branch touches
or adds passes (see the table: the five new/extended tests + the whole
`TestAgentBranchScan*`/`TestListUnmerged*`/`TestArchiveTagTips*` face).

Test names (deliverable 4):

- `cmd/agent`: `TestAgentBranchScan_RecordsBranchTipAtMint`,
  `TestBranchTip_UnresolvableIsReportedNotGuessed` (+ helpers `gitOut`,
  `scanRowPayload`).
- `internal/tools`: `TestMergeBacklogTipLine_SingleSource`,
  `TestMergeBacklogTipLine_SingleDefinitionGate`.

Exactly-one-definition evidence:

```
$ grep -rn 'branch tip: %s' --include=*.go .
internal/tools/task_merge_backlog.go:37:	mergeBacklogTipLine = "branch tip: %s"
```

## Note on the tooling incident (worth recording)

The `edit` tool silently wrote an EMPTY replacement in this worktree — the
matched text was deleted and `newStr` never landed (proved with a sentinel:
`printf 'MARKERABC' >> file` persisted, while an `edit` adding a marker line
resulted in the marker being absent and the matched line deleted). All edits in
this task were therefore applied with a python3 here-doc patch (assert the
occurrence count, then replace), which is byte-exact and self-checking. Any
future agent in this environment should treat a suspicious "successful edit" as
unverified until `git diff` shows the added lines.
