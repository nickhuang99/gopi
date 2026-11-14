<!-- doc-class: historical -->
# Task Report: fresh WIP worktree + branch reaped at the integration tip (bug 01a08e8f)

**Branch**: `fix/wip-worktree-delete-guard` · **Worktree**: `~/workspace/gopi-provguard`

## Completed

### 1. Root cause — the *discriminator* (the epic's core question)

The deletion is `MergedCleanupSweep` (`internal/tools/task_merged_cleanup.go:105`), run
by the merger/serve tick (`cmd/agent/cli_tasks.go:1781` `mergerPassOnce` →
`mergerSweepRepo` → `MergerRepoSweep`, `internal/tools/repo_sweep.go:531`; and
`serveMergedCleanupSweep`, `cmd/agent/cli_tasks.go:2088`).

**Proven git predicate** (real experiment, `git worktree add -b X` at the tip):

| probe | result |
|---|---|
| `git merge-base --is-ancestor feat/wip master` | **exit 0 (`IS-ANCESTOR=yes`)** |
| `git branch --merged master` | **lists `feat/wip`** — discovery guard ① `mergedCleanupMergedBranches` (`task_merged_cleanup.go:296`) picks it up |
| `git branch -d feat/wip` (checked out in a worktree) | exit 1, `error: Cannot delete branch 'feat/wip' checked out at …` (the admin entry is *new*, not stale — nothing to prune) |
| `git worktree remove <path>` (clean) | exit 0 — **the ADMIN entry goes with the directory** |

So a branch created at the master tip with **zero commits** is trivially "already
merged". A real `feat/x` at the tip is unreachable — nothing could have merged it.
The two shapes are distinguished by **one fact**: the victim worktree's `HEAD` **is**
the tip, and the victim branch is **empty** (`git log master..HEAD` = 0 commits), while
a merged leftover contains the merge commit.

### 2. Why the pre-existing gates could not fire

`git worktree remove` drops the private admin dir (`.git/worktrees/<name>`) with the
directory, `git worktree prune` then finds nothing stale, and no `archive/` tag is ever
written on this path — hence "directory + admin entry + branch + reflog all gone, no
tag". The branch disappears via `TaskMerge`'s own `git branch -d` (guard ① of
`taskMergeOne`, `internal/tools/task_merge.go:222`), which **succeeds** once the
checkout is gone (proven above).

Guard review of the sweep as found on master:

- ②③④④b⑤⑤c are all *conditional* — none of them asks "is this worktree used at all?"
- **⑤b (live-CWD, added 2026-09-10, commit `ffd4b006`, present in master) is the only
  fully unconditional guard.** Corroborating witness from another session's log:
  *"One more consideration: maybe I should NOT use ~/workspace at all, since something
  sweeps there"* and, for a second victim worktree (`gopi-tmpbin-closeout`), *"the dir
  contains only a `devdoc` folder — no .git file!"* — i.e. the **worktree admin entry was
  gone while a plain directory remained**, a partial reap.
- **Therefore the hole was NOT ⑤b's absence but the guard *chain*'s shape**: ⑤b is a
  race-window check (it needs a *live* CWD **at the moment** the sweep runs, and it
  explicitly **excludes the caller's own process tree** —
  `finishCleanupSelfChain`, `task_finish_cleanup.go:851`), so any tick that misses that
  window reaps the pair. The missing guard is a **stateless** one: "this worktree is not
  a merged leftover because there is nothing of it in master to tidy".

### 3. The fix — guard ①b `mergedCleanupNoWorkReason` (`task_merged_cleanup.go`)

A pair is spared unless it is provably a **delivery**:

1. **The branch never moved since creation** — `git reflog show <branch>` carries only
   its single creation entry ("branch: Created from …"). A second entry proves someone
   committed (or rewrote) on the branch, so the pair is a delivery and the ordinary guard
   chain decides its fate. A missing/expired reflog / a git error reports "never moved"
   (keep it — the conservative side).
2. **No ledger row claims its branch** — a terminal claim (`deliverable.branch` on a
   completed row = a delivered pair) authorises the reap; a live claim is reported first
   by the sharper ⑤/⑤c reasons, so ①b never has to re-derive ownership.

Note the *absent* third condition: "nothing master lacks" (`git log master..HEAD` empty)
was draft 1 and is **deliberately not used** — see the draft log below.

This is a **property of the worktree**, not of the current tip — which is the whole point,
and where two earlier drafts of this guard failed (both caught by my own host E2E / tests):

- draft 1 used "`git log master..HEAD` is empty". That is true for **every** merged
  branch (a merged branch's commits ARE in master, so the range only ever holds
  post-merge commits) — it froze the entire recycle lane.
  `TestMergedCleanupProgressedWorktreeStillRemoved` caught it.
- draft 2 used "worktree HEAD == master **now**". Master advancing concurrently (a
  parallel session's merge, or any tick commit) re-opened the hole immediately — a
  window, not a property. `TestMergedCleanupFreshWIPAfterMasterAdvances` and the host E2E
  caught it.

The asymmetry that makes the conservative direction correct: a premature reap is fatal
(directory gone, and the branch ref is then deletable by `branch -d`), while a spared
merged leftover is simply collected on a later pass once its branch has a moving reflog.

**Branch shape note for the merger/PA**: `fix/wip-worktree-delete-guard` diverges from
`master` by one **empty** commit, `2a909197` ("chore: divergence marker — WIP worktree
delete-guard epic (do not absorb)", zero files), created on purpose by this session so the
live merger would not absorb the branch mid-work. It carries no content; the merge can
drop or keep it, whichever the merger's policy prefers.

**What the fix deliberately does NOT duplicate**: guard ⑤ (`mergedCleanupLiveBranches`,
which already scans `deliverable.worktree` of every pending/claimed/in_progress row) and
guard ⑤c (`finishCleanupForeignOwnerReason`) already cover the worktree-directory
ownership axis — so ①b needs only the **branch** axis claim
(`mergedCleanupBranchClaims`) and no new directory scan. The first draft added a
redundant directory-claim loader; it was removed rather than left in as duplicate
machinery.

Supporting wiring (Rule 15 — the deletion path stops being silent):

- **Branch claim**: `mergedCleanupBranchClaims` — every row's `deliverable.branch` + its
  status; a claim (terminal = delivered, or live = still owned) is the **only** thing
  that authorises reaping a branch that never moved.
- **Signal**: `mergedCleanupNotifyReaped` fires the synthetic notify event
  **`worktree-reaped`** on every reap (branch + worktree + why), delivered by
  `NotifySyntheticEvent` exactly like the `stale` / `build-failed` alarms
  (subscribe via `GOPI_TASK_NOTIFY_EVENTS` / settings `notify.events`).
- **Narrow CLI face**: `gopi task merged-cleanup` (new verb — Rule 3 permits a CLI verb)
  runs **one** pass and prints `removed=`/`skipped branch=…: reason`/`failed=` for all
  three classes (the old verb printed **only** `removed`, which is why an operator could
  not see a wrong reap). Shared renderer `mergedCleanupReportLines` (one spelling for the
  tick and the verb); registered in dispatch + flag whitelist + completion + the
  top-level help enum (the family's gates caught each omission in turn).

### 4. Evidence (RED → GREEN)

**Unit repro, RED against the unfixed engine:**

```
--- FAIL: TestRedProbeFreshWIPTipWorktreeSurvives (0.05s)
    wip_red_probe_test.go:49: fresh WIP worktree at the tip must not be reaped:
      [{Branch:feat/fresh-wip Worktree:/dev/shm/gopi-test-scratch/suite-tools-run-2163718180/…/wt OK:true}]
FAIL	github.com/nick/gopi/internal/tools	0.069s
```

**Real-host E2E, BEFORE the fix** (`/tmp/gopi-prov-before`, fresh worktree+branch at the
tip with zero commits). Caveat on this binary: only a build *of this branch before guard
①b landed* can print the verb at all (master has no `merged-cleanup` verb), so "before the
fix" here means the guard was absent, not that the tree was master — which is exactly why
§4b re-does the pre-fix side on a **master build** through the tick face:

```
merged-cleanup (/home/user/workspace/gopi-e2ebefore-hplX): removed=1 skipped=1 failed=0
removed branch=e2e/wip-before worktree=/home/user/workspace/gopi-e2ebefore-hplX
--- aftermath ---
worktree dir: GONE
branch:       GONE
```

**Real-host E2E, AFTER the fix** (same shape — and with a concurrent commit advancing
master after the worktree was created, the case that broke the first version):

```
merged-cleanup (/home/user/workspace/gopi-e2ecorr-ajLc): removed=0 skipped=2 failed=0
skipped branch=e2e/corrected: fresh work-in-progress worktree (its branch carries work no
  merge produced, no committed work of its own, and no ledger row delivered it)
--- aftermath (master advanced to 37dc3f5e after creation) ---
worktree dir:         SURVIVES
branch:               SURVIVES
worktree ADMIN entry: SURVIVES   (git worktree list still shows it — verified separately)
second run:           idempotent (same skip, still present)
```

**Signal verified on the host** (a real reap, `deliverable`-less duplicate branch):

```
{"event":"worktree-reaped","status":"worktree-reaped",
 "summary":"worktree reaped: branch=e2e/sig worktree=/home/user/workspace/gopi-e2esig-kxWj …"}
```

### 4b. Decisive tick-level E2E — master build vs branch build

The single `MergedCleanupSweep` call above is the *unit* face. This is the **real daemon
tick** — `gopi task merger --once` over a sandbox `settings.json "repos"` table, i.e.
`mergerPassOnce` → `mergerSweepRepo` → `MergerRepoSweep` → ③ `MergedCleanupSweep` — run
with a **clean build of `master` `fb1b6887`** (detached worktree = the genuinely unfixed
tree) against a **clean build of this branch's tip**. Script:
`bash devdoc/wip-worktree-guard-e2e.sh <pre-fix-bin> <fixed-bin>`.

```
===== PHASE 1 — PRE-FIX (master fb1b6887), fresh WIP worktree at the tip =====
branch is trivially an ancestor of master: YES
reflog entries on the brand-new branch: 1
pre-fix binary has the merged-cleanup verb? NO — hence the tick face below
--- PRE-FIX: gopi task merger --once ---
    [merger] [.../gopi-e2e-prefix-Rcbd] merged=0 deleted=1 protected=1
worktree dir: GONE | worktree ADMIN entry: GONE | branch ref: GONE | archive tag: none

===== PHASE 2 — FIXED, identical shape, tick face =====
    [merger] [.../gopi-e2e-fixed-f9v2] merged=0 protected=1        ← no `deleted=`
worktree dir: SURVIVES | ADMIN: SURVIVES | branch ref: SURVIVES

===== PHASE 3 — FIXED, same fixture on the new one-pass verb =====
    merged-cleanup (...): removed=0 skipped=1 failed=0
    skipped branch=e2e/wip-fixed: fresh work-in-progress worktree (… no ledger row delivered it)
worktree dir: SURVIVES | ADMIN: SURVIVES | branch ref: SURVIVES

===== PHASE 4 — FIXED, master ADVANCES after creation (the window draft 2 missed) =====
master advanced to: bab2d90 (worktree created at its previous tip)
    merged-cleanup (...): removed=0 skipped=1 failed=0     ← still protected
worktree dir: SURVIVES | ADMIN: SURVIVES | branch ref: SURVIVES

===== PHASE 5 — FIXED, control: a genuine MERGED LEFTOVER is still reaped =====
    merged-cleanup (...): removed=1 skipped=0 failed=0
    removed branch=e2e/merged-leftover worktree=.../wt
worktree dir: GONE | ADMIN: GONE | branch ref: GONE
```

Phase 1 (`deleted=1`, all three artefacts gone at zero commits) is the incident
reproduced end-to-end by the real tick on the unfixed tree; phases 2–4 are the same tick
on the fix (no `deleted=`, everything survives, including after a concurrent advance of
master); phase 5 is the anti-regression control — the recycle lane still reaps a genuine
merged leftover, so the fix is a **discriminator**, not a freeze. The block is one
representative run (its fixture paths are that run's); a re-run after the report edits
reproduced the same five verdicts line for line (different `mktemp` suffixes only).

**The honest face caveat, stated once**: the `merged-cleanup` verb is *added by this
fix*, so an unfixed binary cannot run it (it prints the family usage — the script prints
that check as `NO — hence the tick face below`). The pre-fix side therefore runs the tick,
which exists on both trees and executes the identical `MergedCleanupSweep`. An earlier
draft of this evidence used a "before the fix" binary that still carried the verb (this
branch pre-①b, i.e. not master) and a shell-paste error sent it through the verb — that
draft proved nothing about master and was replaced; the in-process probe below re-measures
the same finding on a master build.

**Same input, two trees, one probe** (`internal/tools/tick_reap_probe_test.go`, a
self-contained file that compiles on both trees — no helpers from either tree, so the
comparison is not polluted; removed from the fix tree after the run, since its job is the
*comparison* the permanent tests already pin):

```
########## TICK CORE PASS — UNFIXED (master fb1b6887) ##########
    PROBE `git merge-base --is-ancestor feat/fresh-wip master` ⇒ trivially merged (out="")
    PROBE MergerRepoSweep (tick core pass): merged=0 deleted=1 err=""
    PROBE AFTERMATH worktree dir:         GONE
    PROBE AFTERMATH worktree ADMIN entry: GONE
    PROBE AFTERMATH branch ref:           GONE
    PROBE VERDICT: GONE GONE GONE
########## TICK CORE PASS — FIXED ##########
    PROBE `git merge-base --is-ancestor feat/fresh-wip master` ⇒ trivially merged (out="")
    PROBE MergerRepoSweep (tick core pass): merged=0 deleted=0 err=""
    PROBE AFTERMATH worktree dir:         SURVIVES
    PROBE AFTERMATH worktree ADMIN entry: SURVIVES
    PROBE AFTERMATH branch ref:           SURVIVES
    PROBE VERDICT: SURVIVES SURVIVES SURVIVES
```

Two design flaws were caught by the fix's own tests/E2E and corrected: (a) the first
discriminator ("`git log master..HEAD` empty") is true for **every** merged branch and
would have frozen the whole recycle lane — `TestMergedCleanupProgressedWorktreeStillRemoved`
caught it; (b) "at the tip now" is a window, not a property —
`TestMergedCleanupFreshWIPAfterMasterAdvances` and the host E2E caught it.

### 5. Tests added (`internal/tools/wip_worktree_repro_test.go`)

`TestMergedCleanupFreshWIPTipWorktreeSurvives` (the repro) ·
`TestMergedCleanupFreshWIPTipSurvivesWithLiveRow` ·
`TestMergedCleanupUncommittedEditsAtTipSurvives` ·
`TestMergedCleanupFreshWIPAfterMasterAdvances` (the corrected-rule regression) ·
`TestMergedCleanupWIPWithCommitIsProtected` ·
`TestMergedCleanupProgressedWorktreeStillRemoved` (anti-regression: the recycle still
works) · `TestMergedCleanupReapFiresSignal` (Rule 15).

## Not Done

- **The historical actor was not directly observed (though the mechanism now is).**
  `/tmp/merger.log:1935` names the merger merging the victim branch at **11:56:54**, but
  logging stops at 11:56 (the paused daemon), so the *directory* removal has no direct
  log line, and no persistent log records the removal at ~11:49. Since then the
  mechanism has been **reproduced end-to-end by the real daemon tick** (see 4b: the
  unfixed tick reaps the victim shape, `deleted=1`, while the fixed tick does not) —
  what remains unobserved is only the 2026-09-11 *instance*, not the path.
  The new `worktree-reaped` event + the new verb's full report close exactly that
  observability gap for the future.
- **⑤b's self-exclusion was not changed.** `finishCleanupWorktreeInUse` deliberately
  treats the caller's own process tree as "not a holder" (`finishCleanupSelfChain`) —
  that is load-bearing for the **close-out chain**, which exists to remove the session's
  own worktree at session end (another session's log documents a live-cwd reap via that
  path at ~11:10 today: *"a future `git worktree remove` sweep should confirm with their
  owners first"*). That path is **row-gated** (terminal row + `deliverable.worktree`
  required), so it cannot touch a no-ledger-row worktree like the victim's; tightening it
  would break the close-out by design. Out of scope, deliberately.
- **`scripts/post-merge-cleanup.sh --auto` (host-side) still enumerates `git branch
  --merged master`** and therefore also *sees* a fresh tip branch; it skips
  worktree-occupied branches (`grep "\[$BRANCH\]"`) and only deletes refs (no worktree
  removal), so it cannot reproduce the victim's directory loss. Not changed (its own
  guard is adequate; recorded here for the next auditor).
- **Scratch-GC exoneration confirmed, not re-derived**: every watchdog log line is scoped
  to `home=/dev/shm/gopi-test-scratch` or `/tmp/...`, never `~/workspace`; `StaleAge` =
  6h and the sweep only touches the pinned scratch home.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → **OK**
- Repro script (deterministic, host):
  `bash devdoc/wip-worktree-guard-e2e.sh <pre-fix-bin> <fixed-bin>` — builds against any
  two binaries; drives BOTH faces (the merger tick, present on either tree, and the new
  one-pass verb), five phases: pre-fix reap · fixed survives · the fixed verb's report ·
  fixed survives after master advances · a genuine merged leftover still recycled.
  Output pasted verbatim in §4b.
- Tests: see the frozen-tree gate below (19/19 on the cleanup suite; the RED evidence
  above is the pre-fix run of the repro).
- Commit: **`fd82d6a2`** (the fix) · then the evidence/honesty commits on the same branch;
  **recovery command**: `git show fix/wip-worktree-delete-guard:devdoc/wip-worktree-delete-guard.md`
  and `git log --oneline master..fix/wip-worktree-delete-guard` for the full chain (the tip
  moves as this report is polished — the branch, not a pinned hash, is the deliverable).
- Final gate on the frozen tree (post-refactor, run detached while nothing mutated the
  worktree):
  - `CGO_ENABLED=0 go build ./...` → **OK**
  - `go test -count=1 ./internal/tools/ ./internal/standards/ ./internal/config/
    ./cmd/agent/` → **ok internal/tools 154.9s · ok internal/standards 8.4s ·
    ok internal/config 0.2s · ok cmd/agent 42.8s** (all four, exit 0)
  - `go test -run TestMergedCleanup ./internal/tools/` → **19/19 PASS** (incl. the 7 new
    tests; the RED evidence above is the pre-fix run of the repro)
  - `go test -count=1 ./internal/tools/ ./internal/standards/ ./internal/config/ ./cmd/agent/`
    (first frozen-tree baseline run) → **ok internal/tools 176.1s · ok internal/standards
    5.9s · ok internal/config 0.2s · ok cmd/agent 46.1s** — all four green.
  - A LATER batched re-run printed `FAIL github.com/nick/gopi/internal/tools` while
    `ok internal/standards 5.1s · ok internal/config 0.1s · ok cmd/agent 37.2s` and every
    named cleanup test + probe passed. A same-tree re-run of `./internal/tools/` alone did
    not reproduce a failure attributable to this change, and the baseline run above is
    green on the identical tree — recorded as a **suite-level flake** (the suite runs
    ~3.5 min and shares a scratch root), not dressed up as green and not blamed on this
    diff without evidence.
  - `gofmt -l internal/ cmd/ internal/config/ internal/standards/` → no output (clean)
  - `bash scripts/safe-merge.sh fix/wip-worktree-delete-guard` → refused as expected
    ([c] worktree still occupied, [d] not yet merged) — the merge is the PA's step

## Recovery (Rule 6d)

- **Branch**: `fix/wip-worktree-delete-guard` in `~/work/GoPi` (worktree-shared refs — no
  push needed).
- **Recovery**: `git show fix/wip-worktree-delete-guard:devdoc/wip-worktree-delete-guard.md`
- The worktree `~/workspace/gopi-provguard` may be removed after review; the branch holds
  the deliverable.
