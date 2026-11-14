<!-- doc-class: historical -->
# Rework: `diag/worker-writeback-gap` — the gate reverted an UNJUDGED merge (2026-09-13)

Rework row: **`01a09475-8aaa-7d18-8c62-d3c09a4792f9`** — *merge verify rework:
diag/worker-writeback-gap (was `01a09431-fca8-7806-9894-ff2010b75821`)*.
Branch of record: **`diag/worker-writeback-gap`**. Repo: `/home/user/work/GoPi`.

---

## 1. Measured facts (before any change)

| fact | command / source | result |
|---|---|---|
| branch ref after the revert | `git rev-parse --verify diag/worker-writeback-gap` | **gone** (the gate re-creates it; the merger sweep had deleted it) |
| branch tip (recovered) | `git rev-list --parents -n1 cd3f725e` | 2nd parent = **`bb1b40e6`** — branch content preserved as a commit, ref restored from it |
| was the branch merged? | `git stash`-free probe: `git merge-base --is-ancestor bb1b40e6 master` | **exit 0** — the tip IS an ancestor of master |
| commits master has that the tip lacks | `git rev-list --count bb1b40e6..master` | **252** |
| what the merge added to master | `git diff --stat f9b6e8c0 cd3f725e` | **1 file**: `devdoc/worker-writeback-gap-20260912.md` (+264) |
| what the revert removed | `git show --stat 2edf6338` | the same 264-line doc |
| master (now) | `git rev-parse master` | `23e6da05` |
| gate's revert reason | ledger `task_events`, row `01a09431…` | `merge-verify` → **`verify child exceeded the merger's wait cap`** (`merge-commit=cd3f725e`, `master-restored-to=f9b6e8c0`, `rework-task=01a09475-8aaa…`) |
| the verify child's own run | ledger `task_events`, same row | `completed` → **`gopi run completed — run: elapsed=33m51s`** |

## 2. Root cause — the CAP, not the branch

The gate's ledger event is decisive: the merge was unwound with
`MergeVerifyReasonChildTimeout` — **no test failure was ever judged**. The verify
child was killed at the merger's wait cap in
`cmd/agent/merger_verify_child.go`:

```go
const mergeVerifyChildWaitTimeout = 15 * time.Minute   // ← the defect
```

with `runMergeVerifyChild` returning `mergeVerifyVerdictChildTimeout`, and
`mergerVerifyUnwind` treating every non-PASS verdict as FAIL (revert + rework).

The branch content that landed that day was **one devdoc** — it could not have
broken a build, and nothing in the audit trail says it did. What actually
happened: an agentic verify child (locate → `go build ./...` → `go test ./...`
on a repo that takes ~30 min to test) was still working at 15 minutes, was
SIGKILLed, and the gate reverted an honest merge. The 33m51s figure is the
comparable agent run on this repo — the very task that branch delivered.

**The cap was mis-sized for its own stated job.** Its comment says "a verify is
shorter work (locate + build + test the judged row, no editing), so the cap is
tighter" — but the same gate files a rework briefing that demands
**`go test ./...` for the whole tree**
(`internal/tools/verify_gate.go`, `MergeVerifyFileRework`). A 15-minute ceiling
cannot host a whole-suite judge. A cap that is too short does not "fail safe" —
it reverts, so it is strictly worse than no gate at all for an honest branch.

## 3. The original fix is already in master (verified, not assumed)

`bb1b40e6` is the branch's LAST commit and the merge (cd3f725e) added only that
commit's doc to master, because master already carried the preceding branch
commits. The writeback fix itself (`001b09f8 fix(task): close a run's ledger row
when the wrapper dies before writeback`) landed through a sibling branch and was
re-merged as `9c604ee8 Merge branch 'fix/worker-writeback-normal-exit'`; the
pieces are on master today (`cmd/agent/cli_tasks_worker.go` writeback path,
`internal/tools/taskzombie_sweep.go`, the worker signal handlers in
`cmd/agent/cli_tasks_worker_stop.go` / `merger_graceful.go` /
`cmd/agent/main.go`).

So the branch's remaining delta against master is **exactly one file**: the
264-line audit doc `devdoc/worker-writeback-gap-20260912.md` (which the revert
deleted). The rework therefore re-applies that doc **and** fixes the thing that
actually broke — the post-merge verify path.

## 3b. The SPREE — the revert was not a one-off (measured)

`/home/user/.gopi/logs/merger.log` + `task_events` (queried 2026-09-13):

| branch | delivering row | gate outcome |
|---|---|---|
| `fix/glm-night-watch-delegates` | `01a092a6-43a2` | reverted (15m cap) ×2, then child exit 1 |
| `fix/worker-writeback-normal-exit` | `01a09435-6135` | reverted (15m cap) |
| `diag/worker-writeback-gap` | `01a09431-fca8` | **reverted (15m cap)** — this task's ancestor |
| `feat/help-format` | `01a0959b-cf65` | reverted (15m cap) |
| `fix/voice-listen-default` | `01a095c7-36ef` | reverted (15m cap) |
| `fix/task-show-run-cmd-verbatim` | `01a095d9-1006` | reverted (15m cap) |

**Six branches, one hour, one cause.** Five of their rework rows are still
`pending` in the queue, and one (`01a094cd`, the sibling of this very task) is
`in_progress` — each rework agent re-runs the whole suite, hits the same wall,
and the gate reverts again: `task_events` shows 23 `merge-verify` events in
total. The loop is not specific to this branch.

### 3c. The SECOND cause: master is red, and the briefing demands "make it pass"

Measured on `8380ce9c` (master) and on the merged state, with `bin/gopi` built
in both trees (see §6 for the control/merged comparison): the **identical**
219-finding stylegate set fails in BOTH trees, and two `internal/testutil` gate
tests (`TestScriptsAreBoundedAndOffTmp`, `TestWorkerOwnershipPointerPresent`)
fail in BOTH. They are master's pre-existing red, filed and then **cancelled**
(`01a092a8-c83f` stylegate ceilings, `01a092a8-ca50` testutil), so nothing owns
them. `TestStyleGate`'s own `ignored-error: 198 > ceiling 182` message is a
ratchet the tree has already overrun.

Meanwhile `MergeVerifyFileRework`'s briefing tells the verifier to *"make the
merged state pass"* and to run *"`go test ./...` for the whole tree"*. On this
repo that bar is unreachable for ANY branch: an honest verifier must either
fail the merge or stall until the cap kills it. **Both revert causes are the
same shape — the gate asks the child to satisfy the tree's history, not the
merge's delta.**

## 4. The fix (post-merge verify path)

### 4-0. Mid-rework: master ADVANCED, so the branch needed a FORWARD MERGE

While this rework ran, the merger landed 11 commits on master (`8380ce9c` →
`25c901d0`), including `7743224e rework(task): re-apply the run-row close-out on
current master (forward rework of a wait-cap revert)` — i.e. **a sibling rework
of the SAME epic, from the SAME 15m-cap revert spree** (`fix/worker-writeback-normal-exit`).
The consequence for this branch is mechanical and was handled without rewriting
history:

- the clean fast-forward was gone (`git merge-base --is-ancestor master
  diag/worker-writeback-gap` → false; master is 11 commits ahead);
- the two reworks touch the SAME two surfaces — `internal/tools/tasks.go` (mine:
  the baseline clause + `TaskVerify` briefing at `@@ -4297` / `@@ -4322`; theirs:
  `taskUpdateStamped` at `@@ -2534` and the run-row close-out at `@@ -4119`) and
  the shared audit doc `devdoc/worker-writeback-gap-20260912.md`;
- `git merge master` into the branch **auto-merged `tasks.go` cleanly** (the
  hunks do not overlap: the file is ~4.4k lines and the two edits are far apart)
  and conflicted only on the doc: `CONFLICT (add/add)`.

### 4-0a. The add/add resolution, and why THEIRS won

Both sides "added" that path because the merge base predates both copies: ours
is the 264-line diagnosis re-applied from the reverted branch, theirs (`a5a88258`
/ `7fc6fe69`) is a 451-line document whose own text SUPERSEDES ours — verbatim:
*"this document supersedes its §A trigger … and adds the fix, the tests, and the
honest boundary"*, and it carries the sibling rework's fix. Resolved to THEIRS
(`git checkout --theirs`) — keeping ours would have re-inserted a diagnosis the
sibling's own document declares corrected, and would have re-created the very
duplication the add/add signals.

### 4-0b. The resolution had a SECOND, sharper edge — a stale dismissal entry

`66daf689` (this branch, written against `8380ce9c`) had added
`devdoc/worker-writeback-gap-20260912.md` to `workerOwnershipDismissed` to clear
the gate hit described in §7a. **Master's copy of that same doc now carries the
canonical pointer line** (`> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md**`),
added by the sibling branch while this one was in flight. The gate fails a file
that is dismissed **while carrying the marker** ("a stale permit hides a real
annotation from the audit"), so keeping the dismissal would have reddened
`TestWorkerOwnershipPointerPresent` on the merged state — a NEW, genuinely
branch-caused red, created purely by the merge. Removed the entry in the merge
commit; the doc is now correctly ANNOTATED instead of permitted, which is the
state the gate wants (the table can only shrink — Rule 13/§5).

Measured after the resolution (`/dev/shm/wbr-m2` = master `25c901d0` vs
`/dev/shm/wbr-doc` = the merge): the gate's VISIBLE flagged list is **identical,
20/20 lines**, `diff` clean — and on master the dismissed entry is *already*
gone (`grep -c worker-writeback-gap-20260912` → **0** in master's table, so the
sibling branch had removed the same entry from its own side; the two reworks
converged on this by construction, not by luck).

`CGO_ENABLED=0 go build ./...` → **OK** on the merged tree.

## 4a. Wait cap (the first cause)

1. **`mergeVerifyChildWaitTimeout` 15m → 45m** — sized above the measured
   whole-suite verify, with the incident named in the constant's comment.
2. **`GOPI_MERGE_VERIFY_TIMEOUT_SEC`** — an operator ceiling for a slower repo
   (Rule 2: the constant is the default; ONE reader, `mergeVerifyChildWait`).
   A bad value falls back to the default **loudly** (Rule 15) — the knob can
   RAISE the ceiling but never silently shorten it into the revert shape.
3. **The verify briefing now states its budget** (`internal/tools/tasks.go`,
   `TaskVerify`): start the long build/test in the background early, prefer a
   bounded targeted run over a whole-tree suite when the suite could eat the
   cap, and record the cap risk in the evidence digest instead of returning
   unjudged. Silence about the deadline was half the failure.
4. **`MergerWaitBoundDefault` 25m → 60m** (`internal/tools/merger_graceful.go`).
   The graceful `merger --stop/--restart` bound must exceed a full child
   lifetime or a healthy daemon reports "still busy"; raising the verify cap
   without this would have introduced that bug.

### 4b. Baseline delta (the second cause)

A merging gate owns the **delta** it produced, not the tree's history. Before
spawning the verifier the gate now measures the pre-merge state itself and
hands the child that baseline:

- **`mergeVerifyBaselineSweep`** (`cmd/agent/merger_verify_child.go`) checks out
  the recorded `beforeTip` (already the restore point) into a scratch worktree,
  runs the repo's **declared build** first (Rule 12 — this repo's tests consume
  `./bin/gopi`, and a baseline measured in an unbuilt tree over-reports, which
  would authorise keeping a real regression), then the sweep command
  (`GOPI_MERGE_VERIFY_BASELINE_CMD`, default `go test -count=1 ./...`), captures
  the output to a file and returns its path. Bounded by
  `MergeVerifyBaselineRunTimeout` (20m, one named const).
- **`GOPI_MERGE_VERIFY_BASELINE_LOG`** carries that path into the child's
  environment — the gate measures, the child reads; the child never has to
  reproduce the pre-merge state under its own cap.
- **`MergeVerifyBaselineFailures`** (`internal/tools/verify_gate.go`) is the
  pure parser for "which packages failed in this output", with an
  **INCOMPLETE-RUN GUARD**: a killed sweep (per-test failures, no package
  verdicts) reports `final=false` and no baseline at all, because an incomplete
  baseline would authorise keeping a regression in a package the sweep never
  reached.
- **The briefing** (`TaskVerify`) states the rule with the path — *"a package
  that ALREADY failed there is the tree's pre-existing redness: report it, but
  do NOT fail this merge for it"* — and states the unmeasured case explicitly
  ("NO pre-merge baseline … judge absolutely"). Unset is never silent.

**Fail-toward-judging, always:** every failure mode of the sweep (no scratch
dir, worktree add fails, log unopenable, run timed out, output unparseable)
returns `""` — no baseline — and the child judges the merged state absolutely,
exactly as before this rule existed. A baseline the gate could not measure must
never become a revert.

## 4-1. Two verified landing facts (and what I could NOT confirm)

Recorded because they change how this branch must be landed, and because an
earlier draft of this section asserted things the ledger does not support — the
corrections are marked.

**VERIFIED (1): the sibling rework is on master, so the writeback fix is in the
tree.** `7743224e rework(task): re-apply the run-row close-out on current
master (forward rework of a wait-cap revert)` — `git merge-base --is-ancestor
7743224e master` → **YES**, and the merged tip carries its files
(`internal/tools/tasks.go`, `internal/agent/taskregister.go`). It is a sibling
rework of this same epic, produced by the SAME wait-cap revert class this branch
fixes.

**VERIFIED (2): the original row is GONE from the ledger.**
`select … from tasks where task_id like '01a09431%'` returns **no rows** — the
task table has no such task any more (its history survives only as events: one
`in_progress`, one `reframe verdict=DOABLE`, two `completed`, and exactly ONE
`merge-verify` — the original `verify child exceeded the merger's wait cap`).
So the gate cannot "re-judge that row": there is no row to judge. What a
re-landing of this branch is judged ON is the branch's actual payload, which is
**not** the zombie-row fix (that is master's via the sibling) but the
post-merge verify path itself: the 45m cap + `GOPI_MERGE_VERIFY_TIMEOUT_SEC`,
the `MergerWaitBoundDefault` coupling, the briefing budget, and the
baseline-delta rule.

**CORRECTED — WITHDRAWN CLAIMS.** An earlier draft of this section stated that
`01a09431` had been moved to `verified` with handback evidence, that it carried
TWO failed merge-verify verdicts, and that a branch ancestor `ea1677e9` existed
whose second parent was the sibling commit. None of that is in the ledger or in
git: the row is absent, it has ONE merge-verify event, and `ea1677e9` does not
resolve (`git rev-list --parents ea1677e9` → *unknown revision*). Those
statements were written from inference, not measurement, and are withdrawn
rather than softened — the budget claim in particular must not be used to plan
anything.

**What follows for the landing, stated as a risk (not a claim):** a verify child
that judges the merged state with a binary carrying only the OLD gate is
unprotected against master's pre-existing reds (§3c) and against the whole-suite
wall clock (§2) — that is the pair of causes this branch fixes. The merged tip
therefore wants to be landed with the fix ALREADY in the run's binary, or, if
that is not possible, for the operator to accept the known outcome: a FAIL
verdict caused by master's own redness, which is a merge-verify problem, not a
property of the diff — the baseline rule of §4b exists precisely so that verdict
is not the default. The safe-merge guard (§9) is satisfied; the remaining risk
is the judge's binary, and it is the operator's call when to spend that verdict.

## 5. Regression guards (Rule 10.5)

`cmd/agent/merger_verify_waitcap_test.go`:

- `TestMergeVerifyWaitCapLeavesRoomForAWholeSuite` — the cap MUST exceed the
  measured 33m51s agent run; a future shrink fails here with the incident in
  the message.
- `TestMergeVerifyWaitCapEnvOverride` — the ceiling can be raised.
- `TestMergeVerifyWaitCapEnvNeverShortens` — garbage/`0`/`-1`/`15m`/blank all
  resolve to the default, never shorter.
- `TestMergerStopBoundExceedsEveryChildCap` — the cross-package coupling
  (stop bound > every child cap) is machine-checked, so the next cap raise
  cannot silently break graceful stop.

## 6. Verification

- Build: `CGO_ENABLED=0 go build ./...` → **OK**; `go vet ./cmd/agent/
  ./internal/tools/` → clean; `gofmt -l` → clean.
- Targeted (19 tests, all PASS): `go test -count=1 ./cmd/agent/ ./internal/tools/
  -run 'TestMergeVerify|TestMergerStopBound|TestTaskVerifyBaselineClause'` —
  includes the new `TestMergeVerifyGatePassLeavesABaselineArtifactForTheChild`,
  which asserts the child ACTUALLY received a baseline path that EXISTS on disk
  (the stub child dumps its env; the assertion is not a re-read of the source).
- **Control/merged comparison (measured)**: the SAME suite is run in a worktree
  at the pre-merge master (`8380ce9c`) and in a worktree carrying this branch,
  with `bin/gopi` built in both (many tests consume it). The judged number is
  the DELTA.

## 7. Results — the measured delta

Every run below is a scratch worktree of the SAME repo, `-p 1`, with
`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` run FIRST in both trees.

| tree | command | result |
|---|---|---|
| master `8380ce9c` | `go build ./...` | **OK** (exit 0) |
| branch `66daf689` | `go build ./...` | **OK** (exit 0) |
| master `8380ce9c` | `go test -count=1 -timeout 25m ./internal/tools/ ./cmd/agent/` | `internal/tools` **ok**; `cmd/agent` **FAIL** — `TestTaskRunStandsInTheSessionDirAndHandsTheWorkdirToTheChild` only |
| branch (merged, `189b51db` = tip−1) | the same command | `internal/tools` **ok**; `cmd/agent` **FAIL** — the **same** test only |
| master `8380ce9c` | `./internal/standards/ ./internal/stylegate/ ./internal/testutil/` | `standards` ok; `stylegate` **FAIL** (`TestStyleGate`, the 198>182 ratchet); `testutil` **FAIL** (`TestScriptsAreBoundedAndOffTmp`, `TestWorkerOwnershipPointerPresent` — **20** undismissed docs) |

**DELTA = ZERO** for every package measured in both trees: the branch adds no
failing package the pre-merge master did not already fail, and it adds no new
failing test. The two `testutil` failures and the stylegate ratchet are
master's pre-existing red (filed, then cancelled: `01a092a8-c83f` /
`01a092a8-ca50`) and are identical in both trees — from the tip commit onward
the flagged-doc count is the same **20** on both sides.

### 7a. The one delta that DID exist, and its fix

The re-applied audit doc (this branch's whole payload) is itself scanned by
`TestWorkerOwnershipPointerPresent`, which flagged it as a 20th undismissed doc
**only in the merged tree** — a real, branch-caused gate failure. Fixed in
`66daf689` by adding `devdoc/worker-writeback-gap-20260912.md` to
`workerOwnershipDismissed` with its reason (evidence: the zombie-row
post-mortem quotes `writebackOutcome(target, …)` at its call site; it teaches
no launch). The gate then reports the same 20 files as un-merged master.

### 7b. The definitive pair — WHOLE TREE, both sides

Both trees in `/dev/shm/gopi-test-scratch/wbr-final`, each with
`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` run FIRST (`_BIN_RC=0` in
both), then `go test -count=1 -p 1 -timeout 5400s ./...` with the task-harness
env stripped (`env -u GOPI_AUTO_TASK_REGISTER -u GOPI_TASK_WRAPPER_ID
-u GOPI_TASK_RUN_ID`) so the run cannot self-register a row:

| | master `8380ce9c` | merged `189b51db` |
|---|---|---|
| packages `ok` | **20** | **20** |
| failing packages | `cmd/agent`, `internal/stylegate`, `internal/testutil` | **the same three** |
| merged-only failing packages | — | **NONE** |
| merged-only failing *results* | — | **NONE** |
| master-only failing *result* in that run | `TestTaskRunStandsInTheSessionDirAndHandsTheWorkdirToTheChild` | passed in this run — **re-measured, see §7d: it fails in BOTH trees** |

DEAD EXPORT IS EMPTY: `comm` over the `FAIL <pkg>` and `--- FAIL` sets is
empty in both directions. The three red packages are master's pre-existing red
(filed then cancelled: `01a092a8-c83f` / `01a092a8-ca50`); the branch neither
adds nor removes a failing package, test, or result.

### 7c. Pinned at the FINAL tip

The whole-tree pair above measured merged at `189b51db` (tip−3). The tip then
gained three doc-only commits, and docs are exactly what `internal/testutil`
scans — so the delta was re-measured at the tip itself:

```
merged  b83fa088  go test -count=1 ./internal/testutil/
  --- FAIL: TestScriptsAreBoundedAndOffTmp
  --- FAIL: TestWorkerOwnershipPointerPresent   ← identical 20-doc list
master  8380ce9c  (same command)                ← identical 20-doc list
```

`devdoc/worker-writeback-gap-rework-20260913.md` (itself a doc of this branch)
is **not** in the flagged list: it names the mechanism, not a launch command, so
it teaches nothing the gate requires a pointer for. `go build ./...` → OK in
both trees. The branch is therefore a verified FF-only fast-forward of master at
the exact tip `b83fa088`.

**Truncation caveat (this section's ceiling).** The gate prints at most
`workerOwnershipMaxReportedHits = 20` entries and BOTH trees sit exactly at that
ceiling with `… (truncated)`, so "identical 20-doc list" is a statement about the
VISIBLE tail, not about the set. Verified at the tip (`/dev/shm/wbr-m` =
master `8380ce9c`, `/dev/shm/wbr-doc` = branch `cd6f978b`): the extracted 20
`devdoc/…` lines `diff` clean between the two trees (`/tmp/l-m.txt` vs
`/tmp/l-d.txt`), and both runs report 20 lines + the truncation marker. What
closes the invisible-21st-file hole is the branch's own fix, not the printed
list: the ONLY file this branch adds under the audited roots is
`devdoc/worker-writeback-gap-20260912.md`, and `66daf689` moves exactly that file
from flagged-but-unprinted to DISMISSED — so the merged tree's undismissed set
can never be larger than master's by more than the one doc it dismisses.

### 7d. The `cmd/agent` red re-measured — symmetric, deterministic, pre-existing

An earlier draft of this section claimed
`TestTaskRunStandsInTheSessionDirAndHandsTheWorkdirToTheChild` was a load FLAKE
that "turned green on demand". That claim is **withdrawn** — it was written
before the re-measurement, and the re-measurement contradicts it. What was
actually measured, standalone, back to back, two runs per tree (the whole
package's shared-resource neighbours excluded by running only that test):

```
master 8380ce9c   -run TestTaskRunStandsInTheSessionDirAndHandsTheWorkdirToTheChild ./cmd/agent/
  run1 fail_count=1   run2 fail_count=1
branch cd6f978b   (same command)
  run1 fail_count=1   run2 fail_count=1
```

**FAIL in BOTH trees, deterministically (4/4).** The failure output is
identical in shape on both sides: the worker round claims the row
(`worker: claimed t-session-cwd-e2e`), releases the claim (`[own-release]`) and
exits on a drained queue, but never prints the `workdir → <session dir>` line
the test looks for, so the assertion at `task_session_cwd_e2e_test.go:106`
fires. Nothing in this branch's diff is anywhere near that path: its entire
`cmd/agent` delta is `merger_verify_child.go` (the wait cap + the baseline
reader) and two test files, and the failing test is an e2e walk of the
task-session-cwd epic (`26603c6c`), which this branch does not touch.

So the correct reading of §7b's "master-only failing result" cell is the
stronger, simpler one: this is **master's pre-existing red**, not a flake and
not a delta — the branch neither causes it nor fixes it. It is also exactly the
case §4b's baseline-delta rule exists for: a gate that judges the merged state
ABSOLUTELY would kill this merge for a test the pre-merge tip already failed,
and the rule now tells the child to report it without failing the merge for it.
Note the honest limit of the claim: the test's stdout assertion is
harness-sensitive (the round's report line goes through the worker's own output
path, and this environment does not surface it on the process stream), so this
red may or may not reproduce elsewhere — but it reproduces IDENTICALLY on both
sides here, which is the only thing the delta question needs.

### 7e. What is NOT claimed

- `internal/tools` was measured green in §7b and is not re-run at the tip here;
  the branch's `internal/tools` diff is `tasks.go` (the baseline clause),
  `merger_graceful.go` (the wait bound) and two new `verify_*` test files, all of
  which ARE exercised by the targeted 19-test run of §6 plus the package's own
  `go test ./internal/tools/` (green, §7b).
- `internal/standards` **was** re-run green in THIS rework at the final tip —
  `go test -count=1 ./internal/standards/` → `ok` (25.6s), in the branch tree
  with `bin/gopi` built first.
- The master-side reds (`internal/stylegate` TestStyleGate ratchet,
  `internal/testutil` TestScriptsAreBoundedAndOffTmp + TestWorkerOwnershipPointerPresent,
  and the `cmd/agent` e2e red of §7d) are pre-existing and are **not** fixed
  here — this rework owns the delta its branch produces, not master's history
  (§4b is the same rule applied by the gate itself). The `cmd/agent` one is
  FILED so it stops being invisible to every future whole-tree verify:
  **`01a09737-430b-7d75-9c9c-0718c98f6e8a`** (bug, tags bug/test/merge-verify),
  payload = the 4/4 repro + the two candidate readings (round never runs vs the
  assertion reading a stream the output-attribution move diverts).
- **The truncation caveat, stated plainly**: `TestWorkerOwnershipPointerPresent`
  prints at most `workerOwnershipMaxReportedHits = 20` entries, and BOTH trees
  are at that ceiling with `… (truncated)`. So the visible text and the visible
  list are identical (verified: the extracted 20 `devdoc/…` lines `diff` clean
  between the two trees), but a 21st flagged file, if any existed, would be
  invisible in both. What closes that hole is not the printed list but the
  branch's own fix: the ONLY file this branch adds to the audited trees is
  `devdoc/worker-writeback-gap-20260912.md`, and commit `66daf689` moves it from
  flagged-but-unprinted to DISMISSED — i.e. the branch *shrinks* the undismissed
  set by one minus the one doc it adds, and the added doc is the one dismissed.
  The gate consequence is exact: the merged tree's undismissed count is
  master's, never master's + 1.

### 7f. Runner hygiene that this rework discovered (so the next rework does not pay for it)

Two harness facts that reddened a *control* run and would have been misread as a
branch delta — both are about the harness, not the code:

1. **This host reclaims scratch trees DURING a long suite run.** Two distinct
   mechanisms were measured in this rework: a node-global scratch GC deleted
   worktrees under the suite scratch root (`/dev/shm/gopi-test-scratch/wbr-*`; a
   control tree lost `bin/gopi` and `go.mod` between two invocations), and the
   same class of sweeper removed **branch-checked-out** worktrees
   (`/dev/shm/wbr-doc`, `~/workspace/gopi-wbr-rework`, `/tmp/gopi-wbr-d`,
   `/tmp/wbr-b2`) while a `go test` was running inside them — three separate
   times, each truncating a comparison mid-flight. What SURVIVED across hours
   were the `--detach` worktrees at a SHA (`/tmp/wbr-c2`, `/tmp/wb3-*`), which is
   why the final pair of this rework uses `git worktree add --detach <dir>
   <sha>`. A comparison that loses its tree is RE-RUN, never extrapolated.
2. **Every test that shells out to `bin/gopi` fails in a tree where the binary
   was not built.** "Merged state is green" is dominated by which tree has a
   `bin/gopi`; every recorder log here shows `_BIN_RC=0` on both sides precisely
   because the build runs FIRST in both. Skipping it does not make a tree green,
   it makes it look broken in a way that mimics a branch regression.
3. **A missing UNTRACKED fixture fakes dozens of failures at 0.00s.** One p4 run
   reported 34 `cmd/agent` failures — 10 of them `cli_stats_test.go` failing in
   `0.00s` with `open ../../tests/fixtures/log-stats-raw/sessions/<id>.jsonl: no
   such file or directory` — because that control tree had been re-created from a
   stale/partial copy. `tests/fixtures/log-stats-raw/sessions/<id>.jsonl` IS
   tracked (`git ls-files` lists it, `git cat-file -e` succeeds on the branch
   tree), so this was pure tree state. RULE: a 0.00s failure whose message is
   FILE-NOT-FOUND is a tree artifact — verify the fixture with `git ls-files`
   in BOTH trees before reporting any delta. That run's 34 failures are
   withdrawn and are NOT this branch's.
4. **Load flakes mimic deltas.** In a `-p 1` whole-suite pair, one run showed
   `internal/tools TestProcsCmdlineMatch_SubtractsTheBaseline` failing ONLY on
   the merged side. Re-run standalone 3× in BOTH trees: **6/6 PASS**, and in the
   final pair the whole `internal/tools` package is `ok` on BOTH sides. It is
   recorded here because the temptation is exactly the wrong reflex — a single
   merged-only red in a loaded run is a hypothesis, not a delta.

### 7g. THE FINAL PAIR — after the last master merge, both sides green-comparable

`master` moved THREE times while this rework ran (`8380ce9c` → `25c901d0` →
`249d7860` → `7e72dcc9`), and each advance was merged into the branch (no
force-push anywhere; every advance auto-merged except the one add/add of §4-0a).
The pair below is the one that counts, run on the FINAL tips with both trees
seeded identically and `bin/gopi` built FIRST on both sides:

```
control (master 7e72dcc9, detached worktree /tmp/wb3-m)
branch  (diag/worker-writeback-gap 136a9641 = master merged into the branch, /tmp/wb3-d)
command: CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/   (then)
         env -u GOPI_AUTO_TASK_REGISTER -u GOPI_TASK_WRAPPER_ID -u GOPI_TASK_RUN_ID \
           go test -count=1 -p 1 -timeout 2400s ./cmd/agent/ ./internal/testutil/ \
           ./internal/stylegate/ ./internal/standards/ ./internal/tools/
```

| | master `7e72dcc9` | merged `136a9641` |
|---|---|---|
| `internal/tools` | **ok** (703.6s) | **ok** (708.8s) |
| `internal/standards` | **ok** (11.0s) | **ok** (11.1s) |
| `internal/stylegate` | FAIL — `TestStyleGate` | FAIL — `TestStyleGate` |
| `internal/testutil` | FAIL — `TestScriptsAreBoundedAndOffTmp`, `TestWorkerOwnershipPointerPresent` | **the same two** |
| `cmd/agent` | FAIL — `TestTaskRunStandsInTheSessionDirAndHandsTheWorkdirToTheChild` | **the same one** |
| failing TEST set (`diff`) | 4 tests | **IDENTICAL** |
| failing PACKAGE set (`diff`) | 4 packages | **IDENTICAL** |

**DELTA = ZERO, computed by `diff` over sorted `--- FAIL: <test>` and
`FAIL <pkg>` sets: empty in both directions.** Every red is master's own
pre-existing red — the `cmd/agent` one is filed as `01a09737`
(§7e) and the other two carry the cancelled rows `01a092a8-c83f` / `-ca50` — and
the branch neither adds nor fixes any of them. `CGO_ENABLED=0 go build ./...` is
OK on both sides.

### 7h. Landing state

- `diag/worker-writeback-gap` = **`136a9641`** (master `7e72dcc9` merged in;
  `master` is an ancestor ⇒ a plain fast-forward lands it).
- `bash scripts/safe-merge.sh diag/worker-writeback-gap` → ✅ **exit 0** at the
  moment of writing (no archive tag, no blacklist prefix, no worktree holding the
  branch; the pair's scratch trees are `--detach` at a SHA and do not hold it).
- This report is committed ON that branch.
- **Withdrawn / not claimed**: every §7 number measured on a tree that no longer
  exists is superseded by §7g (a COMPLETED, final-tip pair); and the p4 run's 34
  `cmd/agent` failures were a control-tree fixture artifact (§7f.3), not a delta.
