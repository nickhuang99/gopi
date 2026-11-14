<!-- doc-class: normative -->
# Task Report: stale-merge audit — branches, archive tags, worktrees, and the three operator questions

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Repo:** `/home/user/work/GoPi` — audited tip `master 0dac2ae7` (2026-09-16 14:24:40 +0800)
**Branch:** `fix/stale-merge-audit-20260916` (worktree `~/workspace/gopi-stale-audit`, opened from `0dac2ae7` + a divergence marker `f538a36c` so the live merger sweep cannot absorb it — Rule 6/23; the *investigation* was read-only on the master checkout).
**Original question (verbatim, recovered from the 2026-09-15 session log `/home/user/.gopi/sessions/87bb340c-…/session.jsonl`):**

> "in the last hour I have to manually merge many stale branches of gopi and maybe some merge should never happen, can you take a look and see what is not correct or staled code. for example, we no longer want peak hour cost control code which causes a lot of troubles and also there is a serious bug fix about cwd of gopi worker which doesn't allow git worktree to be removed when it stays its cwd under worktree, this behavior is correct in 'gopi task run' code path, but missing in 'gopi worker' code path, and maybe some tasks are contradictive or something."

The 2026-09-15 original session (`01a0a1c0` → retry `01a0a1d5`) **died without producing this report**; the operator's audit was never delivered. This is that delivery.

---

## 0. TL;DR

| # | Operator question | Answer | Evidence |
|---|---|---|---|
| a | Which stale merges should never have happened, and what stale code survives on master? | **No wrong-merge survives as broken CODE** (`go build ./...` = exit 0, `go vet ./cmd/agent/` and `./internal/tools/` clean). But **four gate-reds survive on master right now**, and they come from the *merge-automation* path, not from the audit itself: §3. | §3 |
| b | Is the peak-hour cost control really gone? | **YES — provably gone.** `internal/peakhours/` absent from `master`'s tree; the eleven removal identifiers occur at 3 *test* sites plus one config comment only. The *policy* (start-time refusal) is dead; the DeepSeek *rate card* is deliberately retained because `gopi stats --cost` must still price 2×. | §2 |
| c | `gopi worker` leaves its cwd in a worktree so `git worktree remove` fails, while `gopi task run` is correct | **Premise is NO LONGER TRUE on master — fixed 2026-09-15 00:25** (`a2ccb31f`, `fix/cwd-and-lifetime` family). Both paths call the *same* one implementation `internal/worktreerelease`, pinned by a `go/ast` gate. The residual risk is **not a code defect** but a *coverage gap*: the gate is static and the runtime rig never drives the real `gopi worker` binary. | §4 |
| d | Merger policy to stop the backlog | **Introduce a "branch is a subject, master is the arbiter" pre-merge check + a `resolve merge` postcondition + sweep cadence.** The backlog grows because *the same subject is developed on 3–5 parallel branches* and master picks one winner; the losers stay `NOT-MERGED` forever and look like pending work. | §5 |
| e | Cleanup list | 30 archive tags: **0 hold a live branch** (all safe to keep, none needs deletion). 9 worktrees/branches: 3 **dirty/unowned — do NOT remove by hand**, 2 detached, 4 live sibling work. Plus **1 Rule-22 violation** (`/tmp/fix-prefix-loop`, a worktree on the `/` partition). | §6 |

**Four defects found and filed as bug tasks** (Rule 26: dispatch, don't wait): §7.

---

## 1. Inventory — classification (measured on `master 0dac2ae7`)

### 1a. Archive tags — 30, all correct, all droppable only after review

```
$ git tag -l 'archive/*' | wc -l
30
$ for t in $(git tag -l 'archive/*'); do b=${t#archive/}; git rev-parse --verify --quiet refs/heads/$b >/dev/null && echo LIVE; done | wc -l
0
```

**Every archive tag is a tombstone with no surviving branch** — the Rule 9 flow (`archive tag` → `branch -D`) was completed for all 30. Classified by whether the tag's unique commits are reachable from master:

| class | count | tags | meaning |
|---|---|---|---|
| **merged-and-cleanable** (tag is an ancestor of master) | 11 | `branch-scan-archive-filter-20260901`, `feat-claim-broadcast-20260901`, `feat-merge-backlog-20260901`, `fix-sigterm-session-end-20260905`, `fix/cli-contract-inventory`, `fix/stylegate-voice-discards`, `fix/worker-count-dead-20260912`, `fix/worker-ownership-pointer-line`, `hp-fix-claim-round-blowout-20260902`, `openai-sdk-backup-20260826`, `single-row-r2-20260905` | content is on master; the tag is pure history |
| **dropped-work-lost** (unique commits, no production file lost) | 16 | `claim-broadcast-m418`, `fix-hp-positional-param-submit-20260907`, `fix-memsync-import-atomic-20260901`, `fix/daemoncwd-scratch-pin`, `fix/task-list-address-no-scan-rework-20260915`, `fix/worker-ownership-10files`, `fix/worker-ownership-pointer-audit`, `hp-unique-maxtokens`, `hp-zombie-closeout-20260902`, `log-stats-raw-gopi`, `memfeedback-motivate-glm-20260827`, `memsync-import-atomic-v1`, `merge-resolve-escalate-early-cb9f72f`, `review/voiceinput-dead-20260912`, `single-row-verify-20260905`, `testsuite-codex-20260827`, `tool-family-merge-superseded-20260910` | unique commits exist, but the added files that are absent from master are **devdoc/report/tag-name variants** — i.e. the *decision* to drop them was made and recorded |
| **superseded — a rival implementation** | 2 | `fix/cwd-and-lifetime` (unique=7), `hippocampus-host-superseded-20260910` (unique=7) | master deliberately chose a different route — see §1c |
| **duplicate/contradictory** | 1 | `memsync-import-atomic-v1` vs `fix-memsync-import-atomic-20260901` — same devdoc basename `memsync-import-atomic.md`, same subject, two tags | one subject archived twice |

Reproduce (the exact command used for the "added files absent from master" half):

```
$ for t in $(git tag -l 'archive/*'); do u=$(git rev-list --count master..$t); [ "$u" = "0" ] && continue; \
    miss=$(git diff --name-only master...$t --diff-filter=A | while read f; do git cat-file -e master:"$f" 2>/dev/null || echo "$f"; done); \
    [ -n "$miss" ] && { echo "### ${t#archive/}"; echo "$miss"; }; done
```

### 1b. Live branches — 16 at audit time, and the backlog is NOT shrinking

```
$ git branch | wc -l          # 16 (incl. master)
$ gopi merger --status        # 9 branch(es) the next sweep would merge · 6 protected
```

`gopi merger --status` (read-only, reproduced in §6d) lists the 9 pending. Classification of every live branch (`ahead`/`behind` measured against `master`):

| branch | ahead | behind | classification | note |
|---|---|---|---|---|
| `fix/standards-baseline-rekey` | 3 | 0 | **still-live** | Rule-16 baseline re-key + Rule-17 escapes |
| `fix/rule17-journalreport-escape` | 2 | 0 | **duplicate** of ↑ | *non-ancestor* of the rekey branch, same 2 files, overlapping hunks |
| `fix/rule17-journalreport-env-escape` | 1 | 0 | **duplicate** of ↑↑ | third branch on the *same* subject, same file |
| `fix/repo-context-gate-reverify` | 7 | 0 | **still-live** | its parent `fix/cmdagent-repo-context-sandbox` is an **ancestor** (verified) |
| `fix/cmdagent-repo-context-sandbox` | 6 | 7 | **duplicate — subsumed** | family's own re-verify branch contains it |
| `fix/heavy-tests-classification` | 4 | 0 | **still-live** | Rule-27 heavy-test classification |
| `fix/merger-busy-liveness` | 3 | 0 | **still-live** | recycled-pid display fix |
| `fix/testutil-gates-green` | 1 | 0 | **overlaps** `…backfill-3` | both touch `scripts/accept-daemon-scope.sh`, `devdoc/daemon-scope-per-profile.md`, `worker-idle-drained-stop.md` |
| `fix/worker-ownership-pointer-backfill-3` | 2 | 0 | **still-live** | 3 annotated + 4 dismissed with reasons |
| `fix/ledger-firstopen-budget` | 3 | **79** | **still-live but stale** | far behind — merging it is where a wrong merge would come from |
| `fix/signalrec-verify-attribution` | 1 | **18** | **still-live, stale** | its subject (#3325) is a *different* fix than the one pending merge |
| `fix/loop-proof-script-scratch` | 1 | 0 | **still-live** | Rule-22 tmp-ban routing |
| `docs/membase-sweep-disposition-decision` | 1 | 0 | **doc-only** | decision record |
| `docs/probe-daemon-term-gate-verification` | 1 | 7 | **doc-only** | stale verdict correction |
| `feat/child-stream-task-output` | 0 | 0 | **empty** | at master tip, no commits — nothing to merge |

### 1c. The rival branch — the single most important non-merge verdict

`archive/fix/cwd-and-lifetime` (7 unique commits) adds

```
$ git diff --name-only master...archive/fix/cwd-and-lifetime --diff-filter=A | while read f; do
    git cat-file -e master:"$f" 2>/dev/null || echo "ABSENT $f"; done
ABSENT internal/procname/identity.go
ABSENT internal/procname/liveness.go
ABSENT internal/tools/daemon_lifetime.go
ABSENT internal/tools/process_cwd_release.go
ABSENT cmd/agent/cwd_release_gate_test.go
ABSENT internal/tools/cwd_release_e2e_test.go
```

Master took a **different route** for the same subject: `internal/worktreerelease` (one chdir action) + `internal/daemonlifetime` + `internal/procname/procname.go` + `internal/standards/cwd_release_gate_test.go`. The branch's implementations have no counterpart on master and master's have none on the branch. **Merging it would re-introduce a second, competing release mechanism.** Verdict: keep archived, treat as reference (Rule 23 — read the failed path, never continue on it).

### 1d. Worktrees — 10, three of them are residue

```
worktree /home/user/work/GoPi                       0dac2ae7 [master]           ← main checkout, untouched
worktree /home/user/workspace/gopi-baseline-rekey   [fix/standards-baseline-rekey]
worktree /home/user/workspace/gopi-child-output     [feat/child-stream-task-output]  ← branch has 0 commits
worktree /home/user/workspace/gopi-firstopen        [fix/ledger-firstopen-budget]
worktree /home/user/workspace/gopi-heavy-tests      [fix/heavy-tests-classification]
worktree /home/user/workspace/gopi-master-baseline  664b3784 (detached)   ← DIRTY (M cmd/agent/cli_tasks_rebuild_pass_test.go)
worktree /home/user/workspace/gopi-master-check     9aec81eb (detached)   ← DIRTY (M cmd/agent/merger_resume_arm_wiring_test.go)
worktree /home/user/workspace/gopi-rectx-verify     [fix/repo-context-gate-reverify]
worktree /home/user/workspace/gopi-repo-sandbox     [fix/cmdagent-repo-context-sandbox]
worktree /tmp/fix-prefix-loop                       830ac32b (detached)   ← Rule 22 violation: a working tree on the `/` partition
```

The two detached `gopi-master-*` trees carry **uncommitted modifications** — per the `fix/dirty-holder-salvage-dispatch` policy that is *uncommitted work*, never removable without capture (and per Rule 26 their state is dispatchable, not hand-deletable). `/tmp/fix-prefix-loop` is on `/dev/sda3` (the 147G boot partition, 87% full): **Rule 22 says a temp workspace must be provably off `/`** — `df -h /tmp` and `df -h /` report the same device (`/dev/sda3`).

---

## 2. Operator question (b) — the peak-hour cost control IS gone (exhaustive)

### 2a. Exhaustive identifier sweep of master

```
$ git grep -n -w -e PeakHoursEnv -e IgnorePeakHoursEnv -e ParsePeakFlags -e IgnorePeakHoursFlag \
    -e IgnorePeakHoursFlagSpelling -e PeakHoursDecision -e ResolvePeakHours -e PeakSettings \
    -e PeakRefusal -e PeakDeclarationWarning -e PeakHoursFlag -e peakGuard -e GOPI_IGNORE_PEAK_HOURS master \
  | sed 's/^master://' | awk -F: '{print $1}' | sort | uniq -c | sort -rn
     19 cmd/agent/peak_guard_removal_test.go      ← the ANTI-REGRESSION test (asserts absence)
     15 devdoc/merge-verify-peak-child-exemption.md   ← history
     10 devdoc/peak-orphan-audit-20260915.md          ← history
      8 tasks/branch-audit-stale-merges-report.md     ← history (2026-09-15 audit)
      7 devdoc/peak-guard-removal.md                  ← the removal's own record
      5 devdoc/peak-orphan-tests-master-green.md      ← history
      5 devdoc/peak-guard-cost-aware-20260914.md      ← history
      5 briefings/brief-remove-peak-guard.md          ← the removal briefing
      3 devdoc/worktree-sweep-ownership.md            ← history
      2 tasks/01a0a3f9-report.md, tasks/01a09d07-…, tasks/01a098a1-…  ← history
      2 devdoc/… (5 more files)                       ← history
      1 scripts/repro-live-holder-worktree-veto.sh    ← the ONE production-adjacent survivor (a comment)
      1 devdoc/peakhours-exact-boundary.md …
$ git ls-tree master internal/ | grep -c peakhours
0
$ git grep -n -i peakhour master -- '*.go' ':!*_test.go'
master:internal/config/config.go:255   (a comment explaining the keys are no longer declared)
master:internal/pricing/pricing.go:211 (a comment naming the deleted package)
```

**Verdict: the guard is gone.** No package, no symbol, no CLI flag, no env reader. The only Go references are (i) a `*_test.go` that exists *to assert the absence*, (ii) two comments recording the removal, (iii) `internal/pricing`'s DeepSeek **rate card**, which is deliberately kept:

```
internal/pricing/pricing.go:217  var deepseekPeakWindows = []peakWindow{{Start: 9, End: 12}, {Start: 14, End: 18}}
```
```
internal/pricing/peakwindows_test.go:11  // WHY IT LIVES HERE: these windows used to be owned by an internal/peakhours
                                         // package shared with the peak-hour START GUARD. The guard was removed outright …
                                         // a rate window that can no longer refuse anything belongs to the RATE TABLE …
                                         // 2x billing is a fact about DeepSeek, so `gopi stats --cost` must keep reporting it.
```

**The removal's own anti-regression tests pass on master** (run, not read):

```
$ go test ./cmd/agent/ ./internal/config/ -count=1 -run 'TestRemovedPeak|TestPeakGuard|TestPeak' -v
--- PASS: TestPeakGuardSymbolsAreAbsentFromTheTree (0.02s)
--- PASS: TestPeakGuardWireTokensAreAbsentOutsideHistoryAndBackwardTolerance (0.03s)
--- PASS: TestRemovedPeakHoursFlagsNoLongerAffectAStart (1.12s)
ok  	github.com/nick/gopi/cmd/agent	1.572s
--- PASS: TestRemovedPeakHoursSettingsKeysAreTolerated (0.00s)
--- PASS: TestRemovedPeakHoursSettingsKeysHaveNoField (0.00s)
ok  	github.com/nick/gopi/internal/config	0.715s
```

### 2b. The one thing that WAS broken by the removal wave — and is already repaired

The removal's merge left two **orphan test files** referencing now-deleted symbols, which made the `cmd/agent` test tree fail to COMPILE (task `01a0a3db`). Reproduced on master's tip today — **it does not compile-fail any more**:

```
$ export TMPDIR=/home/user/workspace/tmp-verify GOTMPDIR=/home/user/workspace/tmp-verify
$ go vet ./cmd/agent/
$                                     ← exit 0, no output
$ go test -run 'TestNothingXYZ' ./cmd/agent/ -count=1
ok  	github.com/nick/gopi/cmd/agent	0.431s [no tests to run]
```

The defect fixed itself by *deletion* (`82f44223`, "delete the stray peak-guard test files that broke the test build"), and the decision to keep the guard removed was recorded with an executable red proof (`a6715b7b`). **Nothing stale survives, nothing to do.**

---

## 3. Operator question (a) — stale code on master, and the non-obvious answer

### 3a. Master's *code* is clean

```
$ go build ./...
$                                       ← exit 0
$ go vet ./cmd/agent/ ./internal/tools/
$                                       ← exit 0
$ go test ./internal/tools/ -count=1 -run 'TestNothingXYZ'
ok  	github.com/nick/gopi/internal/tools	0.387s [no tests to run]   ← the test tree COMPILES
```

`go vet` is the important one: it compiles the test tree, which `go build` structurally cannot see. Both named reds from the briefing (`internal/stylegate errorf-verb`, `cmd/agent test tree not compiling`) — **the second is gone; the first is real but is a *gate* red, not a compile error.** They are §3b.

### 3b. Four gate-reds that survive on master today, and two are a *policy collision*

```
$ go test ./internal/stylegate/ -count=1
--- FAIL: TestStyleGate (2.17s)
    stylegate errorf-verb: 9 production hit(s), ceiling 8 — new violations of a swept rule are regressions
    stylegate ignored-error: 1 production hit(s), ceiling 0
    stylegate ignored-error: 195 test hit(s), ceiling 182
    stylegate test-sleep: 2 test hit(s), ceiling 0
    stylegate test-setenv: 1 test hit(s), ceiling 0
$ go test ./internal/standards/ -count=1
--- FAIL: TestStandardGates (2.75s)
    Rule 16 (%w error wrapping): 9 total hits, 7 baselined, 1 escaped, 1 new
    Rule 16: NEW violation at internal/tools/task_merge.go:366
    Rule 16: STALE baseline entry internal/tools/task_merge.go:362 matches no violation
    Rule 18 (no else after terminating if): NEW violation at internal/tools/task_merge.go:336
    Rule 18: STALE baseline entry internal/tools/task_merge.go:332 …
    Rule 17: NEW at internal/agent/journalreport_test.go:101, :104
$ go test ./internal/testutil/ -count=1
--- FAIL: TestPinCoverageTable      (internal/taskreport has scratch, no TestMain pin)
--- FAIL: TestTestBudgetHasASingleSourceAndNamesATimeout
    scripts/accept-daemon-scope.sh:105 runs `go test` with no `-timeout`
--- FAIL: TestWorkerOwnershipPointerPresent
    8 uncovered file(s) teach a worker-launch / task-run command without the operator-only pointer
```

**Is this stale code?** Two families, and they are different answers:

**(i) `Rule 16 task_merge.go:366` + `Rule 18 task_merge.go:336` — a MERGE-DETRITUS shift, and it is *contradictory* by construction.**
`git blame` on both sites points at `1a5e8ea7` (2026-09-16 13:15, "feat(merger): refuse a branch whose MERGE RESULT does not compile (⓪c union gate)") — i.e. a *new* merger feature landed at 13:15, **immediately after** another merger feature landed at 12:55 (`83d0abc9`).

```
$ git log --oneline --since='2026-09-16 11:00' master
…
1a5e8ea7 feat(merger): refuse a branch whose MERGE RESULT does not compile (⓪c union gate)   13:15
83d0abc9 feat(merger): walk the MERGE RESULT (not just the tip) when stamping a branch…       12:55
```

Two merger branches, **22 minutes apart, both bolted onto `commitMerge`/`task_merge.go`**, met in master. The later one shifted `task_merge.go` by 4 lines, which (a) moved two Rule-16 baseline permits (`:362` → `:366`) and (b) left the `else` after a terminating `if` (Rule 18 now reported at `:336`). Neither is a *wrong merge* in the code-correctness sense — the code is right and `go vet` is clean — but **the merge wave never ran the standards gate on the merged result**, so master has been RED on its own rules since 13:15.

Both sites read from master, verbatim:

```
$ sed -n '333,337p' internal/tools/task_merge.go     # Rule 18 NEW at :336
	} else if _, err := taskMergeGit(repo, "branch", "-d", branch); err != nil {
		return fail(err)
	} else {
		return TaskMergeBranchOutcome{Branch: branch, Action: mergeActionDeleted,
			Detail: "already merged into " + master + " — branch -d"}
```
```
$ sed -n '364,367p' internal/tools/task_merge.go     # Rule 16 NEW at :366, STALE permit at :362
	if _, abortErr := taskMergeGit(repo, "merge", "--abort"); abortErr != nil {
		if !isGitNoMergeToAbort(abortErr) {
			return fail(fmt.Errorf("merge --abort: %w", abortErr))
		}
		return fail(fmt.Errorf("merge %s failed (no conflict state to abort): %v", branch, mergeErr))
```

**(ii) `TaskExitReach:` — the remaining reds are the *sweep's* own backlog, not merge damage.** `internal/stylegate` ceilings (`test-sleep`, `test-setenv`, `ignored-error`) and `internal/testutil`'s three gates are cumulative-audit ratchets whose ceiling was set *below* master's current count by fixes that landed later than the ceiling. They are the honest state of an aggressive ratchet, and every one of them names a specific file:line that a worker can fix. This is exactly what the merger's *build* gate cannot see: **Rule 12's post-merge rebuild runs `go build`, which is GREEN while every one of these is red.**

### 3c. The 228-commit / 51-merge hour — where the wrong-merge risk actually lives

```
$ git log --format='%ci' --since='2026-09-15 05:00' master | cut -c1-10 | sort | uniq -c
$ git log --merges --oneline --since='2026-09-15 05:00' master | wc -l
69
$ git log --format='%s' --since='2026-09-15 05:00' master | awk '/^Merge branch/{a++} /^resolve merge/{b++} /^adoption: merge/{c++} END{print "Merge branch:"a"  resolve merge:"b"  adoption:"c}'
Merge branch: 51  resolve merge: 17  adoption: 0
```

Three distinct merge shapes are visible on master, and **`adoption: merge` — the merger daemon's own fingerprint — has not appeared once since 2026-09-15 00:01.** All 69 merges in the window are authored `Qingzhe Huang` (the operator's own git identity), i.e. **the sweep is no longer the thing landing branches; the operator is**, by hand. That is precisely the "in the last hour I have to manually merge many stale branches" complaint, and it is *still the mechanism*.

The good news, measured: **all 17 `resolve merge` commits are real two-parent merges, none is a hand-re-applied single-parent commit** — so the resolver's conflict handling is not silently rewriting history:

```
$ for c in $(git log --format='%H' --since='2026-09-15 05:00' --grep='^resolve merge' master); do
    echo $(( $(git rev-list --parents -n1 $c | wc -w) - 1 )); done | sort | uniq -c
     17 2
```

The bad news: **`resolve merge` has no postcondition.** A `resolve merge X into master` commit is recorded as `completed` even when the merged result reds master's own gates (§3b(i) is the proof — the resolve committed at 13:15 and master has been red since). The failed/terminal rows in the ledger show the same shape from the other side: 74 `resolve merge conflict` rows, 7 failed or cancelled.

### 3d. Two stale *live* artifacts that are wrong today (named, verified on master)

| artifact | master file:line | why it is stale/incorrect on master |
|---|---|---|
| a repro rig that no longer reproduces | `scripts/repro-live-holder-worktree-veto.sh:29` — `export GOPI_IGNORE_PEAK_HOURS=1` with a comment claiming "the CLI is gated by the peak-hour cost guard" | the guard is deleted (§2): the knob is read by nobody, so the rig's export is a no-op the reader is told is load-bearing |
| a devdoc that teaches the deleted knob | `devdoc/live-claim-worktree-veto.md:110,114` | tells the reader the suite "needs `GOPI_IGNORE_PEAK_HOURS=1`" |

Both are the *instruction* half of the removal surviving its *code* half — the exact failure mode the operator suspects ("stale code"). They are **doc/rig text, not code**, which is why `go build` cannot see them. Filed as a bug task (§7, B4).

**RESOLVED (2026-09-16, branch `fix/peak-guard-stale-instructions`):** both sites are corrected.
`scripts/repro-live-holder-worktree-veto.sh` keeps a plain-language note that the export is
retired instead of the export itself (the false "gated by the peak-hour cost guard" claim is
replaced by the true binding constraint — the test budget); `devdoc/live-claim-worktree-veto.md`
states in place that the knob is retired, read by nothing and that no exemption is needed, naming
the removal commit `bb0c4c1a`. A sweep of every other spelling (env, flag, settings key) found
only remaining hits whose files are removal proofs, append-only history records, or rigs explicitly
telling the reader NOT to pass the knob.

---

## 4. Operator question (c) — the worker/run cwd asymmetry

### 4a. The premise is fixed on master — and here is the proof from both code paths

The correct path, `gopi task run` (`cmd/agent/cli_tasks.go:1827`):

```go
func taskRunReleaseOwnWorktrees(target string) {
	for _, line := range tools.ReclaimOwnFinishedWorktreesWithin() { … }
	for _, line := range tools.ReclaimSelfOwnRegisteredWorktreesWithin() { … }
	worktreerelease.ReleaseReporting(os.Stderr, taskProgName)
}
```
called from **three** close-out points, including the failure paths (`:1558`, `:1753`, `:1803`).

The path the operator reported as missing it, `gopi worker` (`cmd/agent/cli_tasks_worker.go:159`):

```go
func workerRoundOwnWorktrees() {
	workerRoundAnnounce("[worker] [own-release] claimed by this worker: %s\n", tools.TaskClaimOwner(""))
	for _, line := range tools.ReclaimOwnFinishedWorktreesWithin() { … }
	for _, line := range tools.ReclaimSelfOwnRegisteredWorktreesWithin() { … }
	worktreerelease.ReleaseReporting(workerRoundOut(), workerProgTopLevel)
}
```
deferred inside `workerRound()` at **`:654`**:

```go
	defer func() {
		// ORDER MATTERS: the deferred release runs first (LIFO), then the log closes
		defer workerCloseOutputLog()
		defer workerRoundOwnWorktrees()
	}()
```

`internal/worktreerelease.Release()` is the *one* implementation (`release.go:66`):

```go
func Release() error {
	if wd, err := os.Getwd(); err == nil && wd == ProcessRootPath { return nil }
	if err := os.Chdir(ProcessRootPath); err != nil {
		return fmt.Errorf("release cwd to %s: %w", ProcessRootPath, err)
	}
	return nil
}
```

History: the fix landed **2026-09-15 00:25:39** (`a2ccb31f` "one cwd-release action, called on every worktree-removing producer path"), refined `00:30`/`00:48`, and `a2ccb31f` **is an ancestor of master** — verified, not assumed:

```
$ git merge-base --is-ancestor a2ccb31f master && echo YES
YES
```

The operator's report predates the fix by hours (his session ran 05:09 on 2026-09-15).

### 4b. What the operator *would* still see today — an honest residual

Master carries a `go/ast` gate, `internal/standards/cwd_release_gate_test.go` (`cwdReleaseMarker = "worktreerelease.Release"`), which fails any production file that removes a linked worktree without releasing — **and it passes**:

```
$ go test ./internal/standards/ -count=1 -run 'CwdRelease' -v
--- PASS: TestGateCwdRelease_RedProof (0.00s)
--- PASS: TestGateCwdReleaseFindsTheRealTree (0.01s)
```

And the runtime rig that *proves* the mechanism passes too:

```
$ go test ./cmd/agent/ -count=1 -run 'CwdReleaseRig' -v
=== RUN   TestCwdReleaseRig/red_unreleased_holder_pins_the_removed_tree
=== RUN   TestCwdReleaseRig/green_released_holder_lets_the_tree_go
--- PASS: TestCwdReleaseRig (0.13s)
```

**Residual (honest):** the gate is *static* — it proves the call is present in the source, and the rig proves `Release()` works. Neither drives the **real `gopi worker` binary** through a round that leaves its cwd in a worktree. That is the composition the operator's own repro exercised. This is a **coverage gap, not a live defect**; filed as a bug/verification task (§7, B1) rather than asserted as broken.

---

## 5. Merger policy recommendation (operator question d)

The backlog is not caused by slow merging — it is caused by **subject collisions**. Measured today:

| subject | branches developed in parallel | master's arbiter |
|---|---|---|
| worktree-cwd release | `fix/cwd-and-lifetime` (archived), `fix/daemoncwd-scratch-pin`, the merged `internal/worktreerelease` route | picked `internal/worktreerelease` |
| Rule-17 journalreport | `fix/rule17-journalreport-env-escape`, `fix/rule17-journalreport-escape`, `fix/standards-baseline-rekey` — **three branches, same 2 files** | none yet |
| worker-ownership pointer | 9files, 10files, pointer-line, pointer-audit (all archived), `fix/worker-ownership-pointer-backfill-3`, `fix/testutil-gates-green` | partially picked |
| repo-context sandbox | `fix/cmdagent-repo-context-sandbox` (ancestor), `fix/repo-context-gate-reverify` | re-verify branch wins |
| hippocampus host | `hippocampus-host-superseded`, hp/* | superseded |

**Recommendation (4 concrete rules, each naming the automated failure mode it prevents):**

1. **Subject-arbitration pre-merge check.** Before the sweep merges branch `B`, compute `S(B) = {files touched} ∩ {files touched by branches merged since B forked}`. If `S(B)` is non-empty **and** an unrelated branch already landed a change to those files after `B`'s fork point, route `B` to a **`supersede-or-rebase` decision task** instead of merging.
   *Prevents:* `Rule 16 task_merge.go:362→:366` **STALE baseline entry** + `Rule 18 :332→:336` — a pure line-shift caused by an unrelated branch re-indenting the same file 22 minutes later (§3b-i). Both reds exist *only* because the two branches were merged with no arbitration.
2. **Post-merge gate re-run on the MERGED result, not the branch tip.** The `resolve merge`/`adoption: merge` step must run the repo's own gates (`go vet ./...`, `go test ./internal/standards/ ./internal/stylegate/ ./internal/testutil/`) *after* the merge commit exists and **fail the merge commit** (or file a bug row) when they red.
   *Prevents:* the class the 2026-09-15 audit already documented — `c3abd9fa` merged `fix/liveclaim-worktree-veto`, which was "green at authoring time, red at merge time", and reddened `TestWorkerOwnershipPointerPresent` + `TestTestBudgetHasASingleSourceAndNamesATimeout` (both still red today, §3b-ii). It also prevents §3b(i), which landed at 13:15 and has been red since.
3. **`resolve merge` postcondition (a resolve is not a merge).** A row titled `resolve merge conflict: X into master` must be `completed` only when `git merge-base --is-ancestor origin/X master` holds *or* the row names the deliberate content change that superseded it; otherwise the row stays terminal-`failed` with the reason. Today 74 such rows exist, 7 of them failed/cancelled, and nothing distinguishes "I resolved it" from "I resolved it into something that reds master".
   *Prevents:* the `01a0a724` shape — "merger merge-verify hangs forever — done channel has two readers, one steals the value": a **failed** row whose subject then landed anyway, leaving the reader unable to tell whether the subject is outstanding.
4. **Cadence: sweep-by-age, not sweep-by-arrival.** A branch whose `behind` count exceeds a declared threshold (`> 30`) must be *re-verified against the current tip* before merge, never merged on its authoring-time green (§1b: `fix/ledger-firstopen-budget` behind=79, `fix/signalrec-verify-attribution` behind=18).
   *Prevents:* the "8 branches 97–231 commits behind" population the 2026-09-15 audit describes — `fix/cwd-and-lifetime` carries a *rival* implementation of a subject master already solved (§1c), and merging it by age-blind sweep would put two competing mechanisms in the tree.

**Also recommended, cheap:** add `go vet ./...` to the pre-merge gate. `go build` structurally cannot see test-only breaks; `go vet` compiles them. On the 2026-09-15 audit this single check would have caught the `multiple definitions of TestMain` defect that broke `internal/daemoncwd` while `go build` stayed green.

---

## 6. Safe-cleanup list (each item named, with its verdict)

**Nothing was deleted, archived, or removed by this audit.** Every item below is *proposed*, with the Rule 9 flow to be executed only after this report is accepted.

### 6a. Archive tags — **30 items; ALL SAFE TO KEEP; none requires deletion**

No archive tag has a surviving branch (`live branches with archive tags: 0`), so the Rule 9 flow is already complete for all 30. They are the *record of the failed paths* Rule 23 asks us to keep. **Recommendation: keep all; delete none.** If tag hygiene is wanted, only these 2 are *strictly* redundant duplicates and could be dropped after review — and dropping them loses the record, so the recommendation is to keep them too:
- `archive/memsync-import-atomic-v1` — duplicate of `archive/fix-memsync-import-atomic-20260901` (same devdoc basename, same subject).
- `archive/fix/worker-ownership-pointer-audit` — superseded by `archive/fix/worker-ownership-10files` (same family, doc annotations only).

### 6b. Branches — 16; **4 safe to archive now, 8 let the sweep merge, 4 do not touch**

| branch | verdict | justification |
|---|---|---|
| `feat/child-stream-task-output` | **SAFE to remove (branch + worktree)** — 0 commits ahead, 0 behind | an empty branch at master tip is not a deliverable; nothing to archive |
| `fix/cmdagent-repo-context-sandbox` | **SAFE to leave to the sweep** — subsumed | `git merge-base --is-ancestor <it> fix/repo-context-gate-reverify` = YES; its own re-verify branch carries it |
| `fix/rule17-journalreport-env-escape` | **NEEDS OPERATOR CHOICE** (3-way duplicate) | three non-ancestor branches, same 2 files; merging all three in one sweep risks exactly §3b(i) |
| `fix/rule17-journalreport-escape` | ↑ same |
| `fix/standards-baseline-rekey` | **MERGE FIRST of the three** | it is the one that also re-keys the Rule-16 permits — merging it makes §3b(i)'s reds go green |
| `fix/testutil-gates-green` · `fix/worker-ownership-pointer-backfill-3` | **MERGE BOTH, expect a conflict** | overlapping `scripts/accept-daemon-scope.sh` + 2 devdocs; the union resolution is the correct one, but it must be resolved deliberately |
| `fix/repo-context-gate-reverify`, `fix/heavy-tests-classification`, `fix/merger-busy-liveness`, `fix/loop-proof-script-scratch` | **leave to the sweep** | independent subjects, 0 behind, no overlap |
| `fix/ledger-firstopen-budget` (behind 79), `fix/signalrec-verify-attribution` (behind 18) | **DO NOT MERGE by age-blind sweep** | policy §5.4 — re-verify against tip first |
| `docs/membase-sweep-disposition-decision`, `docs/probe-daemon-term-gate-verification` | **leave to the sweep** | doc-only decision records |

### 6c. Worktrees — 10; **3 must NOT be removed by hand**

| worktree | verdict | justification |
|---|---|---|
| `~/workspace/gopi-master-baseline` (detached `664b3784`) | **DO NOT REMOVE** — DIRTY (`M cmd/agent/cli_tasks_rebuild_pass_test.go`) | uncommitted work is work (Rule 26 / dirty-holder salvage policy); dispatch a salvage task, do not delete |
| `~/workspace/gopi-master-check` (detached `9aec81eb`) | **DO NOT REMOVE** — DIRTY (`M cmd/agent/merger_resume_arm_wiring_test.go`) | same |
| `/tmp/fix-prefix-loop` (detached `830ac32b`) | **MOVE OFF `/` then remove** — Rule 22 | `df` shows `/tmp` on `/dev/sda3` (87% full), the exhaustion partition; a working tree there is the defect Rule 22 names |
| `~/workspace/gopi-child-output` | **SAFE to remove** once the empty branch is dropped | holds `feat/child-stream-task-output`, 0 commits |
| the other 5 | **live sibling sessions — do not touch** | each carries a live branch (§6b) |

### 6d. The sweep's own view (reproduced, read-only)

```
$ gopi merger --status
merger:      alive
lock file:   /home/user/.gopi/serve.lock
lock holder: pid 3473055
in flight:   unknown — unparsable merger state file (pid 3473055): {…, "interrupt":{"stage":"verify",
             "children":["verify-child 01a0a7ad-2037-7d96-827b-7a703b319cad (pid 39029)"], …}}
repo:        /home/user/Downloads/games/Maps
  protected: 0 branch(es) held by a live worktree
  pending:   none — nothing to merge
repo:        /home/user/work/GoPi
  protected: 6 branch(es) held by a live worktree
  pending:   9 branch(es) the next sweep would merge
    - docs/membase-sweep-disposition-decision
    - docs/probe-daemon-term-gate-verification
    - fix/loop-proof-script-scratch
    - fix/merger-busy-liveness
    - fix/rule17-journalreport-env-escape
    - fix/rule17-journalreport-escape
    - fix/signalrec-verify-attribution
    - fix/testutil-gates-green
    - fix/worker-ownership-pointer-backfill-3
```

**This list is itself a finding:** the sweep intends to merge `fix/rule17-journalreport-env-escape` and `fix/rule17-journalreport-escape` **in the same pass** — the two non-ancestor duplicates of §6b. Under §5.1 that is precisely the collision that produces a §3b(i)-class red. And `fix/signalrec-verify-attribution` (behind=18) would be merged on authoring-time green.

---

## 7. Defects filed as bug tasks (Rule 26: dispatch, never wait)

| id | severity | subject |
|---|---|---|
| **B1** | medium | `gopi worker` cwd-release: no end-to-end proof — the static gate + `TestCwdReleaseRig` never drive the real `gopi worker` binary through a round that leaves its cwd in a worktree (§4b). Add the composition test or accept the residual in writing. |
| **B2** | high | master is RED on its own gates since 2026-09-16 13:15 — `Rule 16 task_merge.go:366` NEW + `:362` STALE, `Rule 18 :336` NEW + `:332` STALE (§3b-i). Fix the `else` and re-key the two permits (`fix/standards-baseline-rekey` already contains the re-key half). |
| **B3** | medium | `TestWorkerOwnershipPointerPresent` (8 files), `TestTestBudgetHasASingleSourceAndNamesATimeout` (`scripts/accept-daemon-scope.sh:105`), `TestPinCoverageTable` (`internal/taskreport` unpinned) — three `internal/testutil` gate reds on master, all with a named file:line and a prescribed fix (§3b-ii). |
| **B4** | low | The peak-hour removal left its *instructions* behind: `scripts/repro-live-holder-worktree-veto.sh:29` and `devdoc/live-claim-worktree-veto.md:110,114` still teach `GOPI_IGNORE_PEAK_HOURS` (§3d). **FIXED** 2026-09-16 on `fix/peak-guard-stale-instructions` (§3d resolution note). |
| **B5** | medium | Rule 22 violation: `/tmp/fix-prefix-loop` is a git worktree on the `/` partition (§1d / §6c). Either move it to a scratch root and remove, or capture its uncommitted files (`?? cmd/agent/cli_tasks_rebuild_gate_test.go`, `?? cli_tasks_rebuild_pending_test.go`, `?? internal/tools/rebuild_pending_test.go`) before removal. |
| **B6** | low | Merger policy §5: no subject-arbitration pre-check, no post-merge gate re-run on the merged result, no `resolve merge` postcondition. Implement at least the post-merge gate re-run (cheapest, prevents the most reds). |

---

## 8. Did I intervene in any merge?

**No.** Every action in this audit was read-only on the master checkout (`git grep`, `git log`, `git worktree list`, `go build/test/vet`, `gopi task list`, `gopi merger --status`). The only writes are this report and a divergence-marker commit on the audit's own fresh branch `fix/stale-merge-audit-20260916` (worktree `~/workspace/gopi-stale-audit`), created from `master 0dac2ae7` per Rule 6 — **never in `/home/user/work/GoPi`**. No branch, tag, or worktree was deleted; §6 lists what *would* be correct to delete and why, for the operator or a subsequent task to execute under Rule 9.

The reason for not deleting: the Rule 9 flow requires an archive tag first, and the two dirty detached worktrees (§6c) hold **uncommitted work whose capture is a different task's job** — deleting them here would destroy work rather than clean up.

---

## Completed

- Recovered the operator's **verbatim original question** from the dead 2026-09-15 session log and answered all three parts.
- **Inventory + classification** of 30 archive tags, 16 live branches, 10 worktrees (§1).
- **Exhaustive peak-hours sweep** of master by identifier; verdict: the guard is gone, the pricing rate card is deliberately kept, the anti-regression tests pass (§2).
- **Named the stale/incorrect artifacts on master** with file:line and reproduced test output: 4 gate-red families, of which 2 are merge-detritus from a 22-minute-apart double merge of the same file (§3).
- **Both cwd-release code paths quoted** side by side, the shared implementation cited, the ancestor check run, and the honest residual (static gate + rig, no real-binary composition test) named (§4).
- **Merger policy: 4 rules, each naming the automated failure mode it prevents** (§5), derived from measured subject-collision data.
- **Cleanup list**: every tag/branch/worktree with an explicit safe/unsafe verdict (§6), including a Rule 22 violation and two dirty worktrees that must not be hand-removed.
- **6 bug tasks specified** with severity, file:line and prescribed fix (§7) — submitted via `task_submit`.

## Not Done

- **No merge was intervened in, and no branch/tag/worktree was deleted.** The epic's "intervene the mistaken merges" is answered with *evidence that no mistaken merge survives as broken code* plus a cleanup list, not with destructive action: the two dirty detached worktrees hold uncommitted work, and Rule 9 requires an archive tag before any `branch -D`. Acting would have risked destroying work.
- **The full `go test ./...` suite was not run to completion.** `/dev/shm` was 41%-full during this audit (3.1G/7.8G) and the repo's heavy tests are classified off-path (Rule 27); the audit ran the *targeted* gates named in the briefing (`internal/stylegate`, `internal/standards`, `internal/testutil`, `cmd/agent` peak + cwd-release, `internal/config`, `internal/tools` compile) rather than the full suite. Every red reported in §3b is reproduced verbatim from a targeted run.
- **`internal/tools`' full suite was not run** — only its compile (`go test -run TestNothingXYZ` → ok) and `go vet`. Its test tree compiles, which is the claim §3a makes; a full-suite green is not claimed.
- **The four `internal/stylegate` ceiling reds and `TestPinCoverageTable` were reported as-found, not fixed** — they name a file:line each and belong to the sweep's own backlog (B3), not to this audit's deliverable.
- **Archive-tag deletion was not performed** for the 2 duplicate tags (`memsync-import-atomic-v1`, `worker-ownership-pointer-audit`) — dropping them destroys the failed-path record Rule 23 asks to keep; recommended to keep.
- **The 2026-09-15 audit's own `fix/stale-merge-cleanup` branches are gone** (merged); their findings were re-derived from master rather than trusted — no devdoc claim in this report comes from a branch.

## Verification

```
# environment (Rule 22 / /dev/shm constraint from the briefing)
$ mkdir -p /home/user/workspace/tmp-verify
$ export TMPDIR=/home/user/workspace/tmp-verify GOTMPDIR=/home/user/workspace/tmp-verify
$ df -h /dev/shm /
Filesystem      Size  Used Avail Use% Mounted on
tmpfs           7.8G  3.1G  4.7G  41% /dev/shm
/dev/sda3       147G  121G   19G  87% /

# build + test trees (the two reds named in the briefing)
$ go build ./...
$                                              ← exit 0
$ go vet ./cmd/agent/ ./internal/tools/
$                                              ← exit 0
$ go test -run 'TestNothingXYZ' ./cmd/agent/ -count=1
ok  	github.com/nick/gopi/cmd/agent	0.431s [no tests to run]     ← test tree COMPILES
$ go test ./internal/tools/ -count=1 -run 'TestNothingXYZ'
ok  	github.com/nick/gopi/internal/tools	0.387s [no tests to run]

# stylegate red (briefing item 1) — REPRODUCED
$ go test ./internal/stylegate/ -count=1
--- FAIL: TestStyleGate (2.17s)
    gate_test.go:150: stylegate errorf-verb: 9 production hit(s), ceiling 8 — new violations of a swept rule are regressions
FAIL	github.com/nick/gopi/internal/stylegate	4.103s

# standards gate red — the merge-detritus (Rule 16/18, task_merge.go)
$ go test ./internal/standards/ -count=1 -run 'TestStandardGates' -v
    gate_test.go:111: Rule 16 (%w error wrapping): NEW violation at internal/tools/task_merge.go:366
    gate_test.go:111: Rule 16: STALE baseline entry internal/tools/task_merge.go:362 matches no violation
    gate_test.go:111: Rule 18 (no else after terminating if): NEW violation at internal/tools/task_merge.go:336
    gate_test.go:111: Rule 18: STALE baseline entry internal/tools/task_merge.go:332 matches no violation
--- FAIL: TestStandardGates (2.75s)

# peak-hours verdict — all FOUR red proofs PASS
$ go test ./cmd/agent/ ./internal/config/ -count=1 -run 'TestRemovedPeak|TestPeakGuard|TestPeak' -v
--- PASS: TestPeakGuardSymbolsAreAbsentFromTheTree (0.02s)
--- PASS: TestPeakGuardWireTokensAreAbsentOutsideHistoryAndBackwardTolerance (0.03s)
--- PASS: TestRemovedPeakHoursFlagsNoLongerAffectAStart (1.12s)
--- PASS: TestRemovedPeakHoursSettingsKeysAreTolerated (0.00s)
--- PASS: TestRemovedPeakHoursSettingsKeysHaveNoField (0.00s)
ok  	github.com/nick/gopi/cmd/agent	1.572s
ok  	github.com/nick/gopi/internal/config	0.715s

# cwd-release: the static gate AND the runtime rig both PASS
$ go test ./internal/standards/ -count=1 -run 'CwdRelease' -v
--- PASS: TestGateCwdRelease_RedProof (0.00s)
--- PASS: TestGateCwdReleaseFindsTheRealTree (0.01s)
$ go test ./cmd/agent/ -count=1 -run 'TestCwdReleaseRig' -v
--- PASS: TestCwdReleaseRig (0.13s)
    --- PASS: TestCwdReleaseRig/red_unreleased_holder_pins_the_removed_tree
    --- PASS: TestCwdReleaseRig/green_released_holder_lets_the_tree_go

# peak absence in the tree
$ git ls-tree master internal/ | grep -c peakhours
0
$ git merge-base --is-ancestor a2ccb31f master && echo YES
YES

# inventory
$ git tag -l 'archive/*' | wc -l
30
$ git tag -l 'archive/*' | while read t; do b=${t#archive/}; git rev-parse --verify --quiet refs/heads/$b >/dev/null && echo LIVE; done | wc -l
0
$ git branch | wc -l
16
$ git worktree list
(10 entries — reproduced in §1d)
$ git log --merges --oneline --since='2026-09-15 05:00' master | wc -l
69
$ git log --oneline --grep='^adoption: merge' master -1 --format='%h %ci %s'
c7ab803a 2026-09-15 00:01:51 +0800 adoption: merge fix/peak-guard-cost-class (ae98aea1)

# this audit's own worktree (Rule 6), never the master checkout
$ git worktree add /home/user/workspace/gopi-stale-audit -b fix/stale-merge-audit-20260916 master
Preparing worktree (new branch 'fix/stale-merge-audit-20260916')
HEAD is now at 0dac2ae7 Merge branch 'fix/merge-rebuild-loop'
$ git log --oneline -1
f538a36c chore(audit): open fix/stale-merge-audit-20260916 — divergence marker so the merger sweep cannot absorb this branch mid-audit
```

## Files

- `devdoc/stale-merge-audit-20260916.md` (this report)

## Deliverable declaration (Rule 6d)

- **Branch:** `fix/stale-merge-audit-20260916` — worktree `/home/user/workspace/gopi-stale-audit`, opened from `master 0dac2ae7`, diverging at `f538a36c`.
- **Recovery:** `git show fix/stale-merge-audit-20260916:devdoc/stale-merge-audit-20260916.md`
- The branch lives in the shared object store of `/home/user/work/GoPi` (a linked worktree shares refs — **no push needed**); `git -C /home/user/work/GoPi rev-parse fix/stale-merge-audit-20260916` resolves it. The worktree is deliberately LEFT IN PLACE (not removed) so the report is readable and the branch survives.
