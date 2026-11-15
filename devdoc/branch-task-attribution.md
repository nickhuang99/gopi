<!-- doc-class: normative -->
# Task Report / Design: Branch ↔ Task Attribution for the Merger

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Branch**: `feat/branch-attribution` (worktree `/home/user/workspace/gopi-branch-attrib`)
**Question this answers**: *which gopi (task / session) is working in this branch
or worktree right now — and when nobody is, may the merger recycle it?*

The goal is not a general "session tracking" system. It is one answer the merger
needs at the moment it decides a branch's fate, plus the policy for the case no
detector can settle (a foreign worker using the branch as a scratch target).

---

## 1. What exists today, and what it can and cannot say

The merger's per-branch decision lives in `internal/tools/repo_sweep.go`
(`MergerRepoSweep` → `RepoSweepResult`) with two annotations that both key on a
ledger row that named a **worktree**:

| arm | entry point | what it reads | what it decides |
|---|---|---|---|
| dead-holder reclaim | `ReclaimDeadHolderWorktrees` / `DeadHolderBranches` | `finishCleanupDeadLocalHolder(status, claimed_by, ident)` — a **positive death proof** against the row's recorded runner | frees the **directory**, never the branch |
| dead-holder resume | `ResumeDeadHolderRuns` (`task_holder_resume.go`, wired into the merger pass) | `deadHolderResumeRows` → `taskLiveHolderStatus` + the same death proof | resumes the **run** (a live-status row whose runner is provably gone) |
| registration repair (age sweep) | `WorktreeSweepRun` / `worktreeSweepRowHoldsGround` | status, `!HolderProvenDead`, then age | reap / reap-branch a registration whose directory is gone |

Git itself carries **no** gopi identity. A linked worktree records a path, a
branch, a HEAD — never the task that created it (`git worktree list
--porcelain`). `git status`/`rev-parse` are used by the sweep only to find the
*git directory* back from a recorded directory (`worktreeSweepGitDirFor`), which
is why a row whose deliverable names only a branch cannot be matched to a
worktree at all.

So the only place an ownership fact can come from is the **task ledger**, and the
ledger carries it unevenly. Measured on the live ledger (2026-09-16,
`~/.gopi/memory.db`): **1455 rows**, **513** with a non-empty `deliverable.branch`,
**417** naming a `worktree`. `taskBackfillDeliverableBranch`
(`internal/tools/task_deliverable_branch.go:465`, called from
`internal/tools/tasks.go:5405`) is the writer that pairs branch **and** worktree
for an ordinary row — and it runs at **completion** time.

### 1a. The two defects this task closes (arm-level, measured)

**Defect A — an in-flight row with a dead claim pid reads UNOWNED.**
Row `01a0a996-dde5-77a4-bbb2-de6087f155e6` was live while this work was done:

```
status      = in_progress
claimed_by  = peer1:3680671            (that pid is GONE — the wrapper was killed)
deliverable = {"branch":"fix/live-runner-not-dead-holder",
               "worktree":"/home/user/workspace/gopi-live-veto"}
refs/heads/fix/live-runner-not-dead-holder   → no longer exists (already merged)
/home/user/workspace/gopi-live-veto          → no longer exists (worktree removed)
```

`worktreeSweepRowHoldsGround` requires a live status **and** `!HolderProvenDead`;
`finishCleanupDeadLocalHolder` answers *dead* here (the claim pid is a dead local
pid), so the row that is in fact still `in_progress` reads as ground-holding =
**no** → the branch is treated as unowned.

**Defect B — a branch-only row is not even loaded.** `deadHolderResumeRows`
filters `deliverable LIKE '%worktree%'` (`task_holder_resume.go:435`), so a row
whose deliverable names only a branch never enters the resume population — the
same needle-truncation class its sibling `worktreeSweepTaskRows` had already
fixed for the age sweep. Observable in the delivered model: `gopi merger
--status` prints nine protected branches with no owner named for any of them.

### 1b. Measured false positive (found while building this, fixed here)

Four **terminal** rows (`completed`/`failed`) carry a claim stamp naming a pid
that is **still alive** — the long-lived `gopi worker` process that wrote the
stamp and outlived the row (memory #2161). An implementation that ranks "the
claim pid is alive" above "the ledger says the run is over" therefore reports
finished work as *somebody is working here*. This was observed on the live host
(`fix/standards-reds`, `fix/cmdagent-repo-context-sandbox`,
`feat/chat-lineedit-m1` and one sibling listed as `live-holder`), and the fix is
pinned by `TestBranchAttributionTerminalRowWithLiveWorkerPidIsNotALiveHolder`.

---

### 1c. Defect B fixed at its SOURCE (the resume arm's loader), not by a parallel reader

The first pass of this work left defect B fixed only *inside the classifier* —
the branch axis was answered correctly, but the arm that ACTS on it
(`ResumeDeadHolderRuns`) still could not load the row, so the row was reported
`owner-record-only` instead of being resumed. A report is not a repair: the arm
that resumes an interrupted run was still blind to every branch-only row (its
loader filtered `deliverable LIKE '%worktree%'`, so a row naming only `branch`
never entered the candidate set at all — not a bad verdict, an absent candidate).

Two changes in `internal/tools/task_holder_resume.go`, both reusing the ONE
definition of each fact (Rule 14):

1. **The loader queries the BRANCH axis.** `WHERE COALESCE(deliverable,'') LIKE
   '%worktree%'` is gone; the scan takes every row carrying a deliverable and
   keeps the ones whose deliverable names a branch (`taskDeliverableBranchOrEmpty`)
   in a holder-bearing status. This is the same shape its own sibling
   `worktreeSweepTaskRows` already uses — a needle may not silently truncate an
   axis out of the evidence (memory #2632).
2. **The checkout is read from GIT, the row's path never decides it.** The loop
   compares git's registration with the row's recorded directory and *dropped*
   any row whose ledger path differed. On the live host the ledger path IS
   measured wrong (row `01a0a9b5`, `feat/merger-status-next-tick`: the ledger says
   `gopi-next-tick`, git registers `gopi-merger-next-tick`), so that equality test
   discarded a real checkout whenever the ledger was stale — the direction that
   ABANDONS an interrupted run. Git's registration is now the checkout (with an
   `os.Stat` guard for a registration whose directory is gone), and the row's own
   path is kept only as evidence for the probe (memory #2632/#2633: the
   conservative direction is to keep looking, never to stop early).

The residual case is stated, not hidden: a row that has **no** checkout anywhere
is still not resumed — there is no interrupted run in a tree to walk back into —
and the classifier reports exactly that as `owner-record-only`, which is where a
ledger-only row is owed a decision.

**Red-proofs** (`internal/tools/task_holder_resume_branch_axis_test.go`, three
tests): measured failing on the pre-change loader/filter, passing after —
`TestDeadHolderResumeRowsLoadsABranchOnlyRow` and
`TestDeadHolderResumeArmJudgesTheCheckoutGitRegisters` FAIL against the old code
(`git checkout -- task_holder_resume.go` transcript in §6), the third pins the
conservative direction (no ground → no outcome).

---

## 2. The design: one conservative classifier, six verdicts

`internal/tools/branch_attribution.go` — read-only, no merge / delete / lock /
ledger write anywhere in the file.

```
BranchAttribution(repo, branch, rows, holders, ledgerRead) → {verdict, task, reason, evidence, reclaimer}
```

| verdict | meaning | final? | remedy |
|---|---|---|---|
| `live-holder` | the row's recorded runner is alive, or its live-status claim pid is, or a live process holds its checkout | yes | leave the branch alone |
| `owner-ended` | the run is over **and** a checkout still holds the branch | no | the existing arm's own verdict: `resume` (interrupted live-status row) or `reclaim` (finished row's spent directory) |
| `owner-record-only` | a row names the branch but **no checkout holds it** | no | `registration-repair` (the age sweep's reap / reap-branch) |
| `unowned` | the ledger **was read** and no row names this branch | yes | the only verdict that licenses recycling |
| `ungoverned` | the ledger could not be read | yes | nothing may be concluded (Rule 15) |
| `integration` | master/main, `hp-unique-*`, `archive/<branch>`-tagged | yes | not a task's branch at all |

**Evidence tiers** (named constants — the tier table *is* the conservatism
argument):

```
tierProcessLive  = 100   the row's recorded pid+start-ticks+cmd incarnation is alive
tierClaimLive    =  90   its claim stamp names a pid alive on THIS host (live status only)
tierCheckoutLive =  70   a live process stands in the branch's checkout (weaker: not tied to the row)
tierLedgerEnded  =  80   the ledger declares the run over (terminal status / ended_at past grace)
tierProcessGone  =  60   a POSITIVE death proof for the runner or the claim pid
tierRecordOnly   =  20   a row names the branch; nothing proves live, nothing proves over
```

Three decisions in that table are the whole design and each is reasoned, not
tuned:

1. **`tierLedgerEnded > tierClaimLive` for a non-live status.** A terminal row's
   live claim pid is history (defect 1b). The ledger's own declaration wins.
2. **`tierCheckoutLive < tierProcessLive`.** A holder in the checkout is real but
   is *not proven* to be this row's runner, so it never outranks the row's own
   triple — but it outranks "ended", because *"a process is there"* must never be
   read as *"the ground is free"* (memory #2632: a fail-open liveness probe
   removed a live session's worktree).
3. **`tierUnknown` is not a death proof.** `finishCleanupDeadLocalHolder` answers
   `false` for a foreign/blank stamp and for a row with no identity, and the
   classifier treats every such answer as *unprovable*, never as *gone*.

**What it reuses (Rule 14 — one spelling):** `finishCleanupProcessHolderLive`
(incarnation match), `finishCleanupDeadLocalHolder` (positive death proof),
`claimHolder`, `taskLiveHolderStatus`, `worktreeSweepRowLifeEnded`,
`mergedCleanupWorktrees` (git registration), `scratch.HeldByProcesses` (holder
scan), `taskMergeExempt` / `masterBranchName` / `mainBranchName` /
`repoSweepArchiveExempt` (namespaces). No second /proc reader, no second
liveness rule, no second git parser.

**What it does NOT do:** it never relaxes a veto in either existing arm. The
occupancy rule of the age sweep is untouched — a ledger row that is only a
*record* must never protect a directory forever (memory #2145); what this adds is
the missing axis: the row that holds no ground any more but still names the
branch, which is **owed a decision** instead of silently dropped.

### 2a. The arm-boundary bug found while implementing (fixed here)

`branchAttributionOccupancy` was first written to compare the git-registered
directory against the row's *recorded* directory and to treat a mismatch as "not
occupied". The live run exposed it: `feat/merger-status-next-tick`
(`01a0a9b5-…`, status `completed`) records
`/home/user/workspace/gopi-next-tick`, but git registers that branch at
`/home/user/workspace/gopi-merger-next-tick` — the ledger's path is **wrong**.
The function therefore reported "no checkout" and handed the branch to the
registration-repair arm, while git was still refusing it to the merger. The
checkout must be measured from **git**, and the row's recorded path used only to
tie the holder to the row (`ownCheckout`) and to render the decision. The
`Checkout` field exists precisely so a consumer can tell "git refuses this branch
because a checkout exists" from "a ledger row merely remembers a directory that
is gone" — without it, the compact merger line named branches nobody was
blocking.

### 2b. Consumption (both front doors, one renderer)

* `RepoSweepPreview.Attribution` / `RepoSweepResult.Attribution` +
  `AttributionErr`: the merger's per-repo tick and its read-only preview
  (`MergerSweepPreview`) carry the branch-axis picture, and the merger's own
  `protected=` line (`RepoSweepPreview.DescribeProtected`) now ends with
  `(attributed: N [branch — task (verdict); …])` filtered to the branches a
  **checkout still holds**. `AttributionErr` is kept beside the lines so an
  unreadable ledger renders as `attribution UNAVAILABLE: <why>`, never as an
  empty list (Rule 15).
* `gopi task sweep-worktrees`: prints `BranchAttributionSummaryLines` before its
  plan — a per-branch line with the owning task id and whether a checkout holds
  it, answering the "who owns this registration" question the sweep's own
  findings cannot.
* The verdict is **reporting only**: no arm's veto is relaxed, no merge, no
  delete. `Reclaimer` names the *existing* arm whose verdict decides the remedy,
  so the classifier never renders a promise it cannot keep (memory #2324: never
  derive "reclaimable" from "dead holder").

### 2c. Live verification (real command, not a fixture)

`gopi merger --status` on the main repo with the new binary, 2026-09-16
(abridged; the branch under test appears as its own live owner):

```
repo:        /home/user/work/GoPi
  protected: 9 branch(es) held by a live worktree (attributed: 8 [feat/branch-attribution — 01a0a9f6-… (live-holder);
             feat/worker-run-entry — 01a0a9bc-… (live-holder); fix/ledger-firstopen-budget — 01a0a63e-… (owner-ended); …])
```

Every protected branch now names its owner. The `owner-ended` entries are the
rows whose work is over with a spent checkout still on disk — exactly the
population the reclaim arm is for. The earlier (pre-fix) rendering listed four
**completed** rows as `(live-holder)` via the long-lived worker pid — the
false positive of §1b, visible in the same command.

---

## 3. Policy: the "reference worktree" hazard is FORBIDDEN, not detected

A branch/worktree used as a *scratch or reference target* by a **different**
task's worker cannot be settled by detection: from outside, a foreign worker in
someone's worktree is indistinguishable from the owner working. So the briefing's
own resolution is adopted — **it is a rule, not a detector** — and written as
**Rule 29** in `GOPI.md`:

> The main repo hosts work in progress, never a scratch or reference target. For
> another branch's content, clone (or `git show <branch>:<path>` — read-only, no
> checkout); a worktree is a repo **alias** and driving commands inside a foreign
> one mutates the workspace its owner is building in.

What the mechanical half can honestly do (stated in the rule as well): the
classifier **names the checkout a process holds** and the row a branch is
attributed to, so a borrowed worktree shows up as an ownership **conflict**
(reported), not as a clean picture. It does not *prevent* the borrow, and it must
not be given the power to — a rule enforced by guessing intent would refuse
honest work.

---

## 4. Study and comparison: garbage collection vs feeding the existing arms

The briefing asked for the GC alternative to be studied and compared rather than
assumed inferior. Findings:

**Where the arms already ARE a GC.** The registration sweep
(`WorktreeSweepRun`) is generational in the ordinary sense: `reap`,
`reap-branch`, `prune` reclamation rungs (`internal/worktreerelease`) with age
thresholds, a `--max` per-run cap and a `--dry-run` default. The dead-holder
reclaim and the resume arm are its owner-aware cousins. So "GC" is not an
alternative *to* the arms — it is what the arms are.

**What a separate collector would cost, concretely.** (a) A new resident —
cronjob/daemon — which the operator explicitly rejected as the wrong shape for
this problem (and Rule 12/Rule 25 make resident processes expensive to operate
honestly). (b) A second liveness implementation, i.e. exactly the drift that
produced memory #2632/#2633. (c) A second ledger reader, which re-opens the
truncation family this task's loader removes by construction (the
`branchAttributionRowScanMax` refusal replaces the needle with a bounded scan).
(d) A GC's decision is *time*since*use*; what the merger needs is *who*, and a
timed collector cannot name the owner to resume — it can only delete.

**Chosen shape**: the classifier is the GC's missing **reachability root** —
"who still holds this" — expressed once and consumed by the arms that already
exist (Rule 26's "repair by dispatching again" and Rule 14's "one definition"
both point the same way: add the axis, reuse the actuators). The comparison is
recorded here so the next reader does not re-open it as a gap.

---

## 5. Overlap boundary with the competing epic `01a0a9b1`

Found by surveying in-development work (`gopi task list`), which the briefing
required: a **five-row epic** under parent `01a0a964` ("task has no connection
with branch", tag `reframe-roadmap`) already owns part of this territory —
root-cause report, and **claim-time stamping of branch + worktree** (its items
2/3, i.e. the *producer* of the ledger fact this classifier *consumes*).

| | `01a0a9b1` (siblings, pending) | this branch |
|---|---|---|
| producer: stamp branch/worktree at claim | **theirs** | not touched |
| consumer: attribution verdict + merger/status rendering | not in their items | **this** |
| policy rule (Rule 29) | not in their items | **this** |
| GC/cron comparison | "full-liveness" design is theirs | doc only (§4), which says the arms already are one |

The two do not contradict: this classifier degrades gracefully when their stamp
lands (a claim-time `worktree` key turns an `owner-record-only` row into
`owner-ended`/`live-holder` automatically — the field it reads is the same one
their writer will fill). **Dispatched work is not withheld for overlap** (Rule
26): the boundary is stated here, and their epic is free to land first.

---

## 6. Verification

| check | result |
|---|---|
| `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` | OK |
| `go build ./...` / `go vet ./internal/tools/` | clean |
| `go test ./internal/tools/ -run TestBranchAttribution -count=1` | **ok** (18-21s) |
| `go test ./internal/tools/ -run 'TestDeadHolder\|TestBranchAttribution\|TestResumeArm' -count=1` | **ok** (24.0s) |
| `go test ./internal/tools/ -count=1` | **ok** (full package, 2 runs: 78s / 172s) — the package is timing-sensitive on a loaded host (Rule 27's own lesson), so it was run twice |
| `go test ./internal/tools/ ./internal/standards/ ./internal/stylegate/ ./cmd/agent/` | see `## Not Done` / transcript in the task report |
| live: `gopi merger --status` (main repo) | `protected: 10 branch(es) held by a live worktree (attributed: 8 [feat/branch-attribution — 01a0a9f6-… (live-holder); feat/worker-run-entry — 01a0a9bc-… (live-holder); fix/cmdagent-repo-context-sandbox — 01a0a80b-… (owner-ended); …])` — every branch with a checkout names its task |
| live: `gopi task sweep-worktrees` | prints the attribution block before the plan |
| red-proof, **measured**: `git checkout 98939e1a -- internal/tools/task_holder_resume.go` then rerun the two §1c tests | **FAIL** (`TestDeadHolderResumeRowsLoadsABranchOnlyRow`, `TestDeadHolderResumeArmJudgesTheCheckoutGitRegisters`), then restored → PASS. The two §1c assertions are pinned against the real pre-fix source, not a mocked stand-in |

**Red-proofs** (each assertion names the pre-change behaviour it forbids):

| test | pins |
|---|---|
| `TestBranchAttributionOwnerEndedForLiveRowWithDeadRunner` | the live defect: an in-flight row with a dead claim pid is attributed, not "unowned" |
| `TestBranchAttributionLiveHolderKeepsTheBranch` | a LIVE recorded runner owns its branch even when the claim stamp names a dead pid |
| `TestBranchAttributionTerminalRowWithLiveWorkerPidIsNotALiveHolder` | §1b — FAILS on the pre-fix tier ordering |
| `TestBranchAttributionUnownedRowWithLiveCheckoutIsNotUnowned` | a live process in the checkout never yields `unowned` |
| `TestBranchAttributionBranchOnlyRowIsJudged` | the axis `deadHolderResumeRows`' `LIKE '%worktree%'` truncated |
| `TestDeadHolderResumeRowsLoadsABranchOnlyRow` | §1c — the resume arm's loader drops a branch-only row (FAILS pre-fix, measured) |
| `TestDeadHolderResumeArmJudgesTheCheckoutGitRegisters` | §1c — the arm discarded a real checkout over a stale ledger path (FAILS pre-fix, measured) |
| `TestDeadHolderResumeArmRefusesARowWithNoGround` | the conservative direction: no checkout anywhere → no outcome |
| `TestBranchAttributionRecordOnlyWhenCheckoutIsGone` | the 01a0a996 shape verbatim → owner verdict, repair arm |
| `TestBranchAttributionUnownedOnlyWhenTheLedgerSaysSo` | `unowned` requires a READ ledger; an unreadable one is `ungoverned` |
| `TestBranchAttributionIntegrationNamespaces` | master/main/`hp-unique-*`/archive-tagged are not tasks' branches |
| `TestBranchAttributionReportOverTheRealRepo` | the report removes nothing (branch + worktree intact after resolving) |

## Not Done (honest)

* **No automatic action on the branch axis.** The classifier reports and names
  the owning task; it does not itself resume, archive, delete or merge anything.
  Deliberate (a wrong automatic disposition destroys live work), but it means the
  rotation the epic describes stays **operator-driven** until someone wires a
  policy on top of a verdict this file makes trustworthy.
* **A branch-only row with NO checkout anywhere is still not resumed** (§1c) —
  there is no interrupted run in a tree to walk back into. It is reported as
  `owner-record-only` with the registration-repair arm named; the arm that
  ACTS on those rows (`ReclaimDeadHolderWorktrees` / the age sweep) keys on a
  recorded worktree, so the disposal of a pure ledger-only row remains an
  operator decision.
* **The ledger producer is not fixed here** — claim-time stamping of
  branch+worktree belongs to epic `01a0a9b1` (§5). Until their stamp lands, a row
  that never reached completion-time backfill still yields `owner-record-only`
  rather than a resumable verdict.
* **The `01a0a996` row still reads `in_progress` in the ledger** with a dead
  claim pid and no ground. This change makes it *visible and attributed*; it does
  not move the row's status (that is the resume/reclaim arms' own verdict).
* **Full package-suite run** (`./internal/tools/` in full, plus `cmd/agent/`) is
  reported by the task report's final transcript — focused packages were green
  during development; a full-tree run is long on this host (Rule 27: never make
  the host sick to shorten it).

## Files

* `internal/tools/branch_attribution.go` (new) — classifier, verdicts, tiers,
  report, both renderers
* `internal/tools/branch_attribution_test.go` (new) — 9 tests
* `internal/tools/task_holder_resume.go` — the resume arm's loader takes the
  BRANCH axis, and judges the checkout GIT registers (§1c)
* `internal/tools/task_holder_resume_branch_axis_test.go` (new) — 3 red-proof
  tests for §1c
* `internal/tools/repo_sweep.go` — `Attribution`/`AttributionErr` on both result
  types, `BranchAttributionLine`, `describeAttributionNote`, `attributionErrNote`
* `internal/tools/merger_status.go` — status renders the branch axis
* `internal/tools/worktree_sweep.go` — `tasksRowForSweep` carries `ClaimedBy` +
  `Ident` (the evidence triple, one capture)
* `cmd/agent/cli_tasks.go` — merger pass summary passes the axis through;
  `task sweep-worktrees` prints it
* `GOPI.md` — **Rule 29** (worktree = work in progress, never a scratch target)
* `devdoc/branch-task-attribution.md` — this document
