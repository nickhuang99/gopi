<!-- doc-class: historical -->
# Task Report: claim-holder freshness + dead-holder worktree reclaim

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

Branch: `fix/claim-holder-freshness-and-dead-worktree` (worktree
`~/workspace/gopi-holder-stamp`, base master `b8c556ae`).

Two coupled defects in the task ledger / merge path, fixed as one epic.

---

## Completed

### (a) One authoritative "who holds this row" stamp — `internal/tools/tasks.go`

* **`TaskStart(taskID, by)` no longer discards `by`.** It now delegates to a
  new `taskUpdateStamped(..., holder)` which writes `claimed_by` / `claimed_at`
  **in the same UPDATE** as the `claimed→in_progress` transition. An empty `by`
  falls back to this process (`taskDefaultClaimHolder`) — the auto-register
  shape also names an executor. This was the epic's headline staleness source:
  the old body was literally `return TaskUpdate(taskID, taskStatusInProgress)`.
* **New column `tasks.in_progress_at`** (`tasksInProgressAtColumnName`,
  declared as a constant and referenced by the CREATE literal, the ALTER
  migration, the rebuild copy and the row projection — no bare literal
  anywhere: `grep '"in_progress_at"'` outside its definition returns nothing).
  It is the **referent** the claim stamp never had: the claim ladder
  (`TaskClaimExplicit`, `TaskNextFiltered`) stamps at *claim* time, long before
  the run starts. The reference implementation's `claimed_at`-as-referent idea
  was rejected because the healthy ledger *does* separate the two stamps
  (sqlite confirms it: `claim=…723 claimed_at=…688`).
  - `taskUpdateStamped` pins the referent on every real transition
    (`status=in_progress` → `now`), and **clears** it on any other transition
    (the stretch is over).
  - `TaskStampSelfClaim` (the auto-register resume path) moved its stamp and
    the referent to one `now`, so a resume can no longer write a holder stamp
    older than its own referent.
  - `TaskResumeClaimed` deliberately does **not** write the referent: the row is
    still `claimed`, its execution stretch has not begun.
* `Task`, `scanTask`, `scanTaskRow`, `taskSelectColumns` and the legacy
  `ensureTasksSchema` rebuild copy all carry the new column; `HasInProgressAt`
  distinguishes NULL (legacy row) from a stamped start.

### (b) Zombie evidence chain — `internal/tools/taskzombie.go`

* **`pidAlive` treats a zombie as DEAD.** `syscall.Kill(pid, 0)` succeeds for a
  terminated-but-unreaped process (state `Z`) — exactly what a SIGKILLed
  wrapper is, as its own child's unreaped zombie. `pidIsZombie` reads
  `/proc/<pid>/stat` and takes the state field **after the last `)`** (a comm
  may contain spaces/parens, so positional splitting is wrong). Fail-soft: an
  unreadable stat file leaves the pre-existing signal-0 verdict — the probe can
  only turn a live-*looking* pid into dead, never the reverse.
* **New freshness axis `claimStampFreshness`** → `fresh` / `stale` / `unknown`,
  compared against `in_progress_at` with a named skew tolerance
  (`claimStampSkewSec = 5` — the normal claim→start pair is milliseconds apart
  at second resolution and must stay `fresh`).
* **`zombieVerdict` no longer lets a stale stamp veto.** A claim stamp older
  than the row's `in_progress_at` is a *previous claimant's*: the pid layer is
  skipped entirely — no live veto **and no confirmed-dead claim either**; the
  verdict is `suspected` with the reason in the note. A `stale` stamp cannot
  prove life *or* death about the current run, and the code says so.
* The payload gained `stamp_freshness` so the operator/verify agent can see
  which axis produced the verdict. **Legacy rows keep their old vote**: a NULL
  referent reports `unknown`, so nothing that worked before the column existed
  changes behaviour (pinned by a test).

### (c) The merger's protected arm reports a DEAD holder — `internal/tools/repo_sweep.go`

* `RepoSweepPreview.DeadHolder []string` + `RepoSweepResult.DeadHolder` carry
  the protected branches a **provably dead** holder still occupies
  (`DeadHolderBranches`, computed from the ledger + `git worktree list`).
* `RepoSweepPreview.DescribeProtected()` is the merger log's renderer, and
  `describeDeadHolderNote()` is the **one** definition of how a dead-holder
  branch is named (Rule 14). **Both** front doors compose it: the merger's
  per-repo summary in `cmd/agent/cli_tasks.go` and
  `task merger status`'s repo block (`internal/tools/merger_status.go`), so
  the log shows
  `protected=7 (dead holder: 3 [feat/x, feat/y, feat/z] — reclaimable by the
  finish-cleanup sweep)` — and `protected: 7 branch(es) held by a live
  worktree (dead holder: 3 […])` — instead of a silent `protected=7`. The
  status door was wired **after** an audit found it still rendering the bare
  count (`MergerRepoStatus` carried only `Protected int`), which would have
  left the operator's other view showing exactly the anonymous number this
  epic exists to remove. `TestMergerStatus_NamesADeadHolderBranch` pins it
  (red-proved: reverting the status line to `%d` fails all three assertions
  while `TestMergerStatus_RenderStates` still passes — i.e. the old test could
  not see the defect).
* Uncertainty is never guessed: no ledger row, a blank/foreign stamp, or a
  live pid all leave the branch as a plain protected entry (tests pin both
  directions).

### (d) Guarded reclaim, wired into the finish-cleanup sweep

* New `internal/tools/task_reclaim.go`: `ReclaimDeadHolderWorktrees(remove)` +
  `reclaimDeadHolderRowsFrom` + three guards, each alone sufficient to refuse:
  1. **ledger/path** — a TERMINAL row (`taskCleanupEligibleStatus`) whose claim
     stamp resolves on this host, whose pid is **provably dead**, whose
     recorded worktree matches the branch checked out in the directory;
  2. **clean** — `git status --porcelain` empty (a dead holder's uncommitted
     work is left for a human);
  3. **not in use** — the existing `finishCleanupWorktreeInUse` cwd scan
     (`/proc/*/cwd`), reused verbatim: one implementation of "in use".
  Removal is plain `git worktree remove` (no `--force`) — git is the backstop.
  **The branch is NEVER deleted** — it is the deliverable that must still reach
  safe-merge.
* Kill switch `GOPI_DEAD_HOLDER_RECLAIM` (finish-cleanup env convention) →
  report-only, and the report still **names** the withheld branch.
* Wired as the **first** step of `serveFinishCleanupSweep()` (so the freed
  branch is ordinary merge input for the next sweep, and the row chain's own
  worktree step finds nothing left to tear down). It runs on every
  `workerLifecyclePasses` tick with the rest of the lifecycle passes.
* `DeadHolderBranches` is deliberately row-BASED, not `safe-merge.sh`-based, so
  the merger preview and the reclaim can never disagree about *which* branches
  are dead-holder held.

### (e) Tests

`internal/tools/claim_holder_freshness_test.go` (15 tests) and
`cmd/agent/cli_tasks_dead_holder_accept_test.go` (2 end-to-end):

* `TaskStart` stamps the **executing** process (was: the claim ladder's stamp)
  and empty `by` falls back to this pid; `in_progress_at` is stamped atomically.
* `TaskStampSelfClaim` moves the referent with its stamp.
* `claimStampFreshness` boundaries (incl. the healthy claim→start race staying
  `fresh`).
* `zombieVerdict`: stale stamp + **live** pid ⇒ reported, `suspected`,
  `stamp_freshness=stale` (the incident shape); fresh stamp + live pid ⇒ still
  vetoed; legacy (no referent) ⇒ keeps the old vote.
* `TestDetectStaleTasks_ReportsAStaleStampRow` — measured at the layer that
  actually **suppressed** the evidence: two identical stale rows, only the
  freshness of the stamp differs → exactly one is reported.
* `pidAlive`/`pidIsZombie` on a **real unreaped child** (zombie ⇒ dead) and on
  a running process (no false positive).
* Reclaim: removes a dead holder's clean worktree + keeps the branch; refuses a
  live **cwd** holder; refuses a dirty worktree; never touches a live row;
  dry-run reports only; kill switch degrades to a naming report.
* Merger arm: dead-holder branch is named in the preview; a live holder and a
  row-less worktree are not.
* **Shell acceptance**: `TestAcceptance_DeadHolderReclaimUnblocksSafeMerge` —
  real `scripts/safe-merge.sh` refuses the branch (gate `[c]`) → one real
  `serveFinishCleanupSweep()` pass → same script exits **0**, with the branch
  tip and the deliverable file byte-identical.
  `TestAcceptance_LiveHolderKeepsSafeMergeRefused` — same fixture with a live
  child `sh` cwd inside the directory: the sweep reports
  `worktree in use by live process …`, leaves the directory, and gate `[c]`
  keeps refusing.
* `TestMergerStatus_NamesADeadHolderBranch` — the operator's status front door
  names the dead-holder branch (and never labels an unattributed protected
  branch one). Red-proved by mutation (bare `%d` status line ⇒ 3 assertion
  failures; restored ⇒ green).

### (f) Red proofs (mutation evidence)

* `taskzombie.go`: `staleStamp := false` (i.e. the fix removed) ⇒
  `TestZombieVerdict_StaleStampIsNotALiveVeto` and
  `TestDetectStaleTasks_ReportsAStaleStampRow` both FAIL, and the agent log
  shows the live veto swallowing the row
  (`stale t-stamp-stale suppressed: claimed pid … alive on peerbox`). Restored ⇒
  green.
* The pre-fix `TaskStart` discarding `by` is the direct assertion of
  `TestTaskStartStampsTheExecutingProcess` (claim stamp `the-claim-ladder:12345`
  ⇒ must read `the-executor:777`).

## Not Done

* **`/proc` state probe is Linux-only** (best-effort by design, like the
  existing cwd scan): on a non-Linux host `pidIsZombie` returns false and the
  pid layer keeps its signal-0 verdict. No new dependency was added (Rule 7).
* **The reclaim does not retry a DIRTY or in-use worktree.** That is deliberate
  (uncommitted work / a live session), but it means the honest boundary below
  still strands such a directory until a human acts — the sweep now *names* it
  every pass instead of hiding it.
* **`repo_resolve_row.go`'s `pidAlive` uses are not re-audited for the Z fix.**
  They inherit it (dead zombie resolve rows now count as dead owners → the
  recovery scan can close them), which is the intended direction, but no new
  test was written for that specific path (out of this epic's deliverable list).
* **The merger's own sweep still does not reclaim.** The reclaim lives in the
  finish-cleanup/lifecycle sweep (as the epic specified); the merger only
  *reports* the dead-holder branches. A repo whose lifecycle pass is not running
  (no worker/serve) therefore still needs the manual `git worktree remove`.
* **No E2E that kills a real `gopi task run` wrapper mid-flight** (the death
  interleaving is inherently racy — the reference attempt documented the same
  limitation). The dead-holder shape is staged deterministically instead: a
  real reaped child's pid in a real ledger row.

## Honest boundary: what can still strand a worktree

0. **The incident shape itself, when the row is not yet terminal.** The reclaim
   is scoped to TERMINAL rows on purpose: for an `in_progress` row with a
   **stale** stamp we have deliberately refused to attribute any pid to the
   running process (that refusal IS the fix — a stale stamp can prove neither
   life nor death), so a dead holder there cannot be *proven* dead by this
   code, and the disposer only kills `confirmed` rows. Such a row therefore
   stays `in_progress` with its worktree until a human (or the operator's own
   close-out) makes it terminal; after that the reclaim picks it up on the next
   pass. What the fix guarantees is that this state is **reported** (the stale
   event now fires — no live veto swallows it) instead of being silent.

1. A holder whose claim stamp is **foreign-host, blank or unparseable** —
   "unknown" is never "dead", so the reclaim refuses (a multi-host ledger can
   still strand a directory on the node that can prove nothing).
2. A **dirty** worktree, or one with a **live process inside** (including an
   unrelated shell merely `cd`'d there) — by design; the sweep logs the reason.
3. A row with **no deliverable.worktree** recorded (the directory exists but
   nothing attributes it) — the preview cannot name it either.
4. A ledger that cannot be opened at all (scan error) — reported, not repaired.
5. `git worktree remove` itself refusing (locked worktree, permissions,
   nested worktree) — logged per row, pass continues.
6. **The reclaim scan is a bounded window** (`reclaimScanMax = 200`, oldest
   updated_at first). Measured on the live ledger while writing this report:
   1070 terminal rows, 156 of them carrying a `worktree` in the deliverable —
   comfortably inside the window today, but a ledger that accumulates more
   than 200 worktree-carrying terminal rows would starve the tail exactly the
   way `serveFinishCleanupStrandedIDs` was added to fix for its own scan. The
   merger's *report* half has no such window (it is driven by the actual
   `git worktree list`, not the ledger).
7. **Measured at the same time: the live ledger had ZERO dead-holder
   candidates** (every terminal row naming a currently-worktree-held branch
   whose claim pid is locally dead → 0), i.e. the incident's residue had
   already been cleaned by hand. The observable value of this change today is
   therefore the two *report* halves plus the standing repair path — the next
   time it happens it is named and repairable instead of silent.

## Verification

Independently RE-RUN on the committed tree (code tip `52a03ae0`; the later
`6128fec3` touches this report only, no code — so these numbers describe the
shipped code exactly). Worktree clean — `git status --porcelain` empty — so
the run read the commits, not a working copy:

```
gofmt -l .                     → clean (no output, whole tree)
go build ./...                 → clean (exit 0)
go test ./internal/standards/  → ok  ...internal/standards     4.564s
go vet ./internal/tools/ ./cmd/agent/  → clean (no output)
go test -count=1 ./internal/tools/ ./cmd/agent/
                               → ok  ...internal/tools       232.571s
                               → ok  ...cmd/agent            104.938s   (job-5)
```

Earlier runs of the same tree while the commits were being made:

```
go test ./internal/tools/      → ok  github.com/nick/gopi/internal/tools    176.018s   (job-2)
go test ./cmd/agent/           → ok  github.com/nick/gopi/cmd/agent           84.726s
go test ./internal/agent/      → ok  ...  47.541s   (A/B: base b8c556ae → ok 47.754s)
go test ./internal/config/     → ok  ...   0.064s
```

Notes on the run environment (honest, since it changes how the numbers read):

* tests were run with `env -u GOPI_AUTO_TASK_REGISTER -u GOPI_TASK_WRAPPER_ID
  -u GOPI_TASK_RUN_ID` — our own agent session exports these and they poison
  `internal/agent`'s start-hook tests (known pitfall);
* `cmd/agent` needs `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` in the
  worktree first: `TestSweepZombiesCLI_*` fail on a missing `bin/gopi`, and the
  SAME two failures reproduce on the untouched base worktree `/tmp/ab-base`
  (verified A/B), i.e. fixture, not regression;
* one earlier combined `./internal/agent/ ./internal/config/` run emitted a
  register dump and FAIL; both packages pass standalone and the identical
  combined invocation passes on the base tree → transient concurrency flake,
  recorded here rather than hidden;
* Rule 18's gate caught a real defect during this work: an `else if` after a
  returning `if` in `task_reclaim.go` — rewritten as a guard clause. The
  standards gate is what caught it, which is the point of Rules 16–21.

## Commit

Two commits on `fix/claim-holder-freshness-and-dead-worktree` (`git log --format=%H -1` for the tip):

1. `fix(tasks): one authoritative holder stamp + dead-holder worktree reclaim`
   — deliverables (a)–(f).
2. `fix(merger-status): name dead-holder branches on the status door too` —
   the second protected-arm renderer found by audit (§c).

The parent of commit 1 is master `b8c556ae` — the branch is a clean
fast-forwardable child of master.

---

# Follow-up (2026-09-12, session 01a09569): the vanished worktree — a reclaim line that never converged

## Symptom, exactly as the operator saw it

`gopi worker` start-up printed, pass after pass:

```
[worker] [lifecycle] stale reported=0 disposed=0 sink=cat >> /home/user/.gopi/notify/task-events.jsonl
[task-notify] dead-holder reclaim feat/verify-write-task-area (t-1788524560) remove failed: worktree remove /home/user/workspace/gopi-verify-wta-2: exit status 128: fatal: '/home/user/workspace/gopi-verify-wta-2' is not a working tree
… (7 rows: /tmp/wt-websearch-multisource, gopi-review-memaware-db, gopi-review-memaware-resume,
   gopi-stale-zombie-fix, gopi-fix-log-last-degenerate, /tmp/other-repo (chdir … no such file or directory))
```

## Root cause (5-why, proven, not inferred)

1. Every one of those seven paths **stats as absent** — the directories are gone.
2. `reclaimDisposition` treated "directory gone" as **"nothing to inspect ⇒ nothing refuses ⇒ all three guards held"** and returned `""` (freable).
3. So the sweep fell through to `git worktree remove <absent path>` → **exit 128** every time.
4. Nothing could ever clear the row: a worktree registration is dropped **only** by a successful `remove` or by `git worktree prune`, and neither ever ran. → **the failure could not converge** — hence the same burst at every start-up, forever.
5. The damage was not just noise: **`git worktree list` keeps printing a gone-but-registered worktree, and `scripts/safe-merge.sh` gate [c] greps exactly that list** — so a vanished directory permanently withheld *its own branch* from merging. The reclaim exists to end exactly that withholding.

Two other shapes in the same capture, same family: `chdir /tmp/other-repo: no such file or directory` (the row's **repo** is gone) and `'<path>' is not a working tree` for a path that **exists** but is no longer a linked worktree of the row's repo (registration freed by hand, path re-created).

## Fix (`internal/tools/task_reclaim.go`, `task_finish_cleanup.go`)

- `reclaimDisposition` now returns one of three verdicts instead of a bare reason string: `reclaimFreable` / `reclaimSkip` / **`reclaimVanished`**.
- `reclaimVanished` (directory gone) is **repaired**, not removed: `git worktree prune -v` drops the stale registration. It is reported **only when prune actually dropped something** — so pass 1 names the repair and pass 2 is silent (convergence is the assertion, not a hope).
- The **ground probe** moved *before* `git status`: a path that is not a linked worktree of the row's repo is refused in one line (`path exists but is not a linked worktree of <repo>` / `path is a linked worktree of ANOTHER repo`), never fed to a removal — that shape never exits 128 again.
- A row whose **repo directory is gone** is refused explicitly (one honest line) instead of a failed git call per pass.
- `DeadHolderBranches` (the protected arm's source) now grounds occupancy in **git + on-disk existence**: a registration is not an occupancy, so a gone directory no longer inflates the protected count.
- `ReclaimDeadHolderWorktrees` now writes the dispatch/reason line to the operator sink (`taskNotifyLog`) for refused rows (Rule 15 — a withheld branch is never silent again).

## Verification

- `internal/tools` — `TestDeadHolderReclaimRepairsAVanishedWorktree` (repair reported, registration pruned, **branch tip untouched**, second pass silent) and `TestDeadHolderReclaimRefusesAGhostDirectory` (path exists, registration gone → 0 lines, the foreign file is byte-identical afterwards), plus `TestDeadHolderReclaimUnownedRowIsNotProtected` for the occupancy half.
- `cmd/agent` — `TestAcceptance_VanishedWorktreeLeavesNoFailureLine` drives the **real** `serveFinishCleanupSweep()` and the **real** `scripts/safe-merge.sh`: gate [c] refuses before → sweep pass 1 = one `registration pruned` line and **no `remove failed`** → the same script exits **0** with the branch tip byte-identical → pass 2 prints nothing.
- **Live-ledger cross-check** (the shape is not hypothetical): the production ledger `~/.gopi/memory.db` holds **3 terminal rows whose recorded worktree directory is gone** (`t-1788554650` → `~/workspace/gopi-merge-fixlog`, `t-1788583867` → `/tmp/gopi-verify-gopi`, `01a088e0-…` → `/tmp/gopi-reframe-verdict`) — on top of the 7 paths in the operator capture, every one of them hit the doomed `git worktree remove` on every lifecycle pass.
- Full gate: gofmt clean, `go build ./...` OK, `go vet` OK, `go test ./internal/tools/ ./cmd/agent/ ./internal/standards/ ./internal/config/ ./internal/agent/` green, and the pre-existing rule-chain acceptance tests (`TestAcceptance_DeadHolderReclaimUnblocksSafeMerge`, `TestAcceptance_LiveHolderKeepsSafeMergeRefused`) still green — a live holder still keeps the branch refused.

## Honest limits

- The repair prunes **every** stale registration in the repo (`git worktree prune` has no single-target form); it never touches a directory or a branch, so the blast radius is registrations whose directory is already gone.
- Rows are still terminal-only (unchanged from the parent epic): a stale claim stamp proves neither life nor death, so an `in_progress` row stays stranded until close-out or a human.
- The reclaim still frees the **directory**, never the branch — the branch is the deliverable.

# Follow-up 2 (2026-09-12, same session 01a09569): the repair was right, its SCOPE was wrong

The first follow-up fixed the disposition ("a vanished directory is not a removal, it is a
registration to prune"). An audit of the committed fix against the REAL ledger found that the
repair ran **per row**, and `git worktree prune` is **repo-wide** — it takes no path. Three
defects followed, all invisible in the one-vanished-row fixture the first round used.

## 1. False attribution (the repair is credited to the wrong row)

Measured on a two-row fixture (`probe`, t+2366): with two vanished rows in ONE repo where
only the SECOND still had a live registration, the FIRST row's prune call dropped the
second's registration — and the first row reported

```
dead-holder worktree registration pruned: feat/dead-holder (t-attrib-a) — …
```

…while the row that was actually registered stayed silent. The first follow-up's own
"report only when prune dropped something" rule therefore *credited the wrong branch*. The
incident capture had **5 such rows in one repo**, so the misattribution was the normal case,
not the corner.

## 2. N+1 subprocesses per pass

A row whose registration git already dropped has nothing to repair, yet the old code pruned
once per such row, every pass: the real ledger holds **137** such rows. Measured before the
fix: 137 prunes ≈ 2.5 s of subprocesses per lifecycle tick, forever, for a guaranteed-empty
result (the loop could never converge because the ledger rows are never mutated).

## 3. Report-only modes were NOT report-only

The repair ran **before** the `remove`/kill-switch checks, so `ReclaimDeadHolderWorktrees(false)`
(dry run) and `GOPI_DEAD_HOLDER_RECLAIM=0` both wrote to the repo's administrative area while
promising "report only, never a removal" — `git worktree prune` is the ordinary
`git worktree remove --force` body's own prune step, so it is a mutation.

## Fix (`internal/tools/task_reclaim.go`)

- **One plan per pass** (`reclaimPrunePlan`): `git worktree list --porcelain` is read ONCE
  per repo (memoized) and the BEFORE image decides which rows are due a repair
  (`reclaimVanishedProven`). A row whose registration is already clean is a **silent skip**
  — nothing to do, nothing to say (an empty reason is deliberately not logged; a line per
  pass is the very burst this epic ends).
- **At most ONE prune per repo per pass**, memoized in the plan (a failed prune is not
  retried for every later row either).
- **Credit from git's own before/after statement**: after the prune the list is re-read
  (the cache is invalidated on prune) and a repair line is emitted only for a row whose path
  was listed BEFORE and is absent AFTER. With several rows in one repo only the ones git
  actually dropped are reported.
- **Rule 15 on the residual**: a registration that SURVIVES the prune (locked/unreadable
  administrative entry) is recorded as `registration survived prune` and the row stays a
  NAMED candidate — the branch never looks free while gate [c] still refuses it.
- **Report-only is now true for the whole arm**: neither `remove=false` nor the kill switch
  runs a prune; the row is NAMED with "stale registration left untouched (report only)".
- `reclaimPruneCall` is the counted seam (a var, so a test counts prune invocations instead
  of inferring them from output); `reclaimPlanParsePaths` is the shared porcelain parser.

## Verification (differential where it matters)

- **Pre-fix vs post-fix, measured** (throwaway probe on the PRE-FIX control worktree at
  `0f91cc7c`, then deleted): two vanished rows in ONE repo with only the SECOND registered →
  pre-fix credited **t-cred-a** (the row with nothing to repair) and stayed silent about
  **t-cred-b** (the row git really dropped); post-fix credits t-cred-b and never names
  t-cred-a. A `git`-shim on PATH counted prunes: **2 pre-fix vs 0 post-fix** for two
  already-clean rows.
- `TestDeadHolderReclaimCreditsOnlyTheRowItRepaired` (new): two vanished rows, one repo, only
  the second registered → the repair line names **t-cred-b** and NOT t-cred-a, exactly **1**
  prune runs, gate [c]'s list no longer names the branch, and both branch tips survive
  (`rev-parse`). Fails on the pre-fix code (proven by the throwaway probe: it printed the
  first row's branch).
- `TestDeadHolderReclaimDoesNotPruneForAlreadyCleanRows` (new): both registrations already
  clean → **0 lines, 0 prunes**, on the first AND the second pass (the real ledger's shape,
  137 rows → 0 subprocesses).
- `TestDeadHolderReclaimReportOnlyTouchesNothing` (new): dry run AND kill switch → 0 prunes,
  the branch still NAMED, the registration still in `git worktree list`.
- Real-ledger probe (throwaway, deleted): `candidates=137 lines=0 prunes=0` — three
  consecutive passes on `/home/user/.gopi/memory.db` are now silent and cost nothing.
- Pre-existing behaviour unchanged: `DeadHolderReclaim*` (15 tests) + the `cmd/agent`
  acceptance trio green, incl. `TestAcceptance_VanishedWorktreeLeavesNoFailureLine` and
  `TestAcceptance_LiveHolderKeepsSafeMergeRefused`.

## Differential measurement (pre-fix tree vs fixed tree — no assumption)

The three new tests assert the fixed behaviour, but "the old code was wrong" must be MEASURED,
not asserted. Two throwaway devices were run against the PRE-FIX control tree (a detached
worktree at `0f91cc7c`, the commit this change builds on) and then against the fixed tree;
both devices were deleted afterwards.

- **Attribution (a two-row fixture — the fixture the first round lacked):** two vanished rows in
  ONE repo, only the SECOND still registered. PRE-FIX output:

  ```
  dead-holder worktree registration pruned: feat/dead-holder (t-cred-a) — directory already gone; stale registration pruned; branch kept
  ```

  …i.e. row **A** (already unregistered, nothing to repair) was credited, and row **B** — the
  one git actually dropped — was SILENT. After the fix the same fixture credits **t-cred-b**
  and never mentions t-cred-a.
- **Spurious prunes (counted, not inferred):** a `git` shim on `PATH` appended one line per
  `prune` invocation. Two already-clean vanished rows in one repo: **pre-fix = 2 prunes**,
  **post-fix = 0 prunes** (no shim log file was even created). Scaled to the live ledger's 137
  such rows that is 137 subprocesses per lifecycle pass → 0.

## Boundary found while measuring (documented, not introduced here)

`git worktree prune` also drops the registration of a worktree whose **directory still
exists but whose `.git` pointer file is gone** (measured: "Removing worktrees/C: gitdir file
points to non-existent location"; the directory and its files were left intact). That worktree
becomes orphaned — git stops tracking it, so gate [c] no longer protects it. The reclaim
REFUSES such a row (the path is not a linked worktree of the repo) and never removes the
ordinary `git worktree remove`; the refusal is the pre-existing, correct verdict for a
directory this row does not own.
