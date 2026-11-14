<!-- doc-class: historical -->
# Task Report: why cannot remove worktree — the three refusal arms

Branch: `feat/worktree-refusal-arms-v2` (worktree `~/workspace/gopi-wt-refusal-v2`).
Sibling (do not duplicate): task `01a0a6eb-b6fa` owns **arm 2** (the orphaned
`serveWorktreeReapSweep` caller + the orphan dirty worktree with no ledger row).
Prior attempt `fix/worktree-refusal-arms` (80f721f0) is UNMERGED and was read
as information only (Rule 23) — this branch opens fresh from master and
implements its own way.

## The operator's three questions, answered

**Q1 — "the first case is real: each gopi must clean up by not modifying
untracked files in the worktree."** Confirmed and fixed at the classifier. In
master the refusal was ONE string: guard ② rendered *any* non-empty
`git status --porcelain` body as `reclaimSkipDirtyReason` ("uncommitted work"),
so a tree whose only entries were `??` (a run's own report, build output, a
scratch checkout) reported itself as somebody's uncommitted work — and the file
list was thrown away, so the operator could not even see which files held it.
Measured live before the fix: the main checkout carried 6 untracked
`tasks/01a0a4*-verify-report.md` files, and the dead-holder arm printed
"[…] skipped: worktree is DIRTY (uncommitted work)" with no names.

**Q2 — "who creates a second worktree? is it old or new?"** Answered from
source, not guessed: **`git worktree add` has no Go call site** — a *run*
creates worktrees, and `internal/tools/task_deliverable_worktree.go` stamps
only the FIRST `git worktree add ... -b <branch>` a child session performs and
**never rebinds**, so a run's second registration (a scratch/baseline checkout)
is owned by no row; a row's deliverable records exactly ONE path. Every
ledger-keyed arm (dead-holder sweep, producer pass, finish-cleanup chain) is
therefore blind to it by construction. The ownership-independent instrument
already exists (`WorktreeSweepRun`), but its only production caller
(`serveWorktreeReapSweep`) hangs off `serveMergedCleanupSweep`, whose
production caller was deleted 2026-09-04 — i.e. the arm that WOULD have seen it
is unreachable. **Fix location: not this branch** — it is exactly the pending
task `01a0a6eb-b6fa` ("fix: dead-registration worktree reap is unreachable
(serveWorktreeReapSweep orphaned) + orphan dirty worktree with no ledger row"),
which is the right owner and is already queued. This branch's contribution to
arm 2 is the mechanism answer above plus the classifier that makes the residue
distinguishable when that task lands.

**Q3 — "for a verifying gopi who only verifies: do they commit? same worktree?
who signals the merger?"** Answered and fixed the leak:
- *Do they commit?* No. The merger's gate spawns `gopi task verify` →
  `TaskVerify` (`internal/tools/tasks.go`), a child gopi that only measures and
  writes a verdict into the **ledger**.
- *The signal is the LEDGER ROW, not a directory.* The gate waits on the child
  and reads the verdict from the row (`verified`/`failed`; `verify_gate.go`).
  So a lingering worktree was never the merge signal, and a detached-tip
  checkout holds no branch — it never blocked `safe-merge` gate [c] either.
  There was no contradiction to resolve; there was an unowned directory.
- *Who created it?* The MERGER, not the verify child. The full production
  inventory of `git worktree add` (grep over non-test `.go`, this branch) is
  **three call sites, all the merger's own, all under the scratch tree, all
  removed by a `defer`**:
  1. `cmd/agent/merger_verify_child.go:577` — the pre-merge BASELINE sweep
     `git worktree add --detach <dir> <beforeTip>` (tip = the pre-merge tip so
     the gate can prove the merged state against it). Measured residue that no
     arm owns: `~/workspace/gopi-verify-01a0a4ae/wt-tip` (the defer's
     `worktree remove --force` silently failed);
  2. `cmd/agent/merge_resolve_child.go:130` — the conflict RESOLVER,
     `git worktree add -q <dir> <branch>`;
  3. `internal/tools/merge_build_gate.go:521` — the merge-time BUILD gate,
     `worktree add --detach <workRoot>/wt-<rev> <rev>`.
  Every additional worktree is a **run's own** (`git worktree add` typed by
  the agent child — the `~/-workspace/gopi-<task>` convention), which is the
  one the row DOES record. (All were missed by
  the prior attempt, which read `task_deliverable_branch.go`'s session JSONL
  scan as if it were a call site: the stamping seam READS
  `git worktree add ... -b <branch>` out of a child's tool_use, so it sees a
  linkage only when the child typed the command under `-b`.)
  It is removed best-effort by a `defer` (`worktree remove --force`), and the
  remove **can silently fail** — leaving a dirty, row-less tree that no arm
  owns (arm 2's population again).
- *The real leak this branch fixes:* the child was spawned with
  `child.Dir = <main repo>`, so the verifier's whole filesystem pass — build
  output, probes, its report — landed in the SHARED master checkout. That is
  what made master permanently dirty (Q1's trigger). The verifier is now put
  **in the delivered worktree** (the row already records it) and told not to
  mint another one.

## Completed

- **Arm 1 — one dirty-state classifier** (`internal/tools/task_reclaim.go`):
  `reclaimDirtyVerdict(porcelain)` splits guard ② into `reclaimSkipDirtyReason`
  (≥1 TRACKED change — still refused, still "uncommitted work", and the
  offending entries are now quoted back) and `reclaimUntrackedResidueReason`
  (untracked-only — the same refusal, with the KIND named and the run told to
  clean its own scratch). Both keep the operator word `DIRTY`; nothing is
  removed under either verdict. `reclaimDirtyHeld(reason)` is the salvage
  lane's predicate over BOTH shapes.
- **All duplicate gates now share it** (Rule 14 — the three copies of
  `strings.TrimSpace(dirty) != ""` were the drift surface):
  `task_finish_cleanup.go` (×3 sites) and `task_merge_executor.go` (the
  `mergeExecSafeMergeBlock` main-checkout gate; the unrelated tag guard keeps
  the raw sentinel check).
- **Arm 3 — the verify child's ground** (`taskVerifyGround`, tasks.go):
  `TaskVerify` used to spawn the child with no `Dir`, so it inherited the
  MERGER's cwd — the shared master checkout — and its whole filesystem pass
  (build output, probes, `tasks/<id>-verify-report.md`) landed there. That is
  arm 1's trigger, i.e. "worktree cannot be removed" and "the verifier
  littered master" are the same bug from two ends. New resolution, with the
  dominant case handled: the row's recorded worktree **when it still exists**;
  otherwise a **private DETACHED checkout of the merged tip created under the
  scratch tree** (`scratch.RuntimeClassVerify`, GC-bounded, off `/` — Rule 22),
  removed by the returned cleanup func. The dominant case IS the fallback,
  because releasing the delivered worktree is what unblocks the merger
  (measured: all 49 `verified` rows record a worktree that no longer exists).
  `--detach` is deliberate: the verifier judges, never commits, so it needs no
  branch — and holding a branch there would re-block the very merger it serves.
  Degradations are REPORTED, never silent (no repo / unusable scratch ⇒
  caller's cwd, on stderr). The briefing carries `taskVerifyWorktreeClause`:
  name the ground, `git worktree add` forbidden (the child is a full agent and
  its habits mint a new tree — measured `~/workspace/gopi-verify-01a0a4ae/wt-tip`),
  and leave no untracked residue behind.
- **Regression tests** (`internal/tools/worktree_dirty_split_test.go`): the
  classifier table (`??`-only vs ` M`/`A `/`R ` shapes, the clean sentinel,
  the mixed body), the salvage predicate, the end-to-end untracked-only
  refusal on a real worktree (directory untouched, operator line makes no
  reclaim promise), and the verify cwd/clause behaviours.

## Not Done (honest)

- **Arm 2's fix is NOT in this branch** — it belongs to the already-queued task
  `01a0a6eb-b6fa` (orphaned `serveWorktreeReapSweep` + orphan dirty worktree
  with no ledger row). This branch deliberately does not duplicate it; it
  supplies the mechanism (stamp-stops-at-first-hit + one-path row) that task
  should build on.
- **A finished run still is not forced to clean its own untracked residue.**
  The classifier now NAMES it (and names the files), which is the operator's
  "basic requirement" made visible and attributable, but the enforcement half —
  refusing a run's `completed` transition when its own worktree carries
  untracked residue, or auto-cleaning a proven-safe residue set — is a
  behavioural change to the run lifecycle and is NOT implemented here —
  **DISPATCHED as its own row: `01a0a725-1faf-785e-abd3-db1fd73a6e55`**
  ("enforce a run's own untracked residue: refuse `completed` on a dirty own
  worktree"), because the operator's "basic requirement" deserves an
  enforcement half, not just a name (Rule 26: repair by dispatching, never by
  waiting).
- **The verify child's `defer`-removed detached worktree remains best-effort**
  (a failed `worktree remove --force` is still only a stderr line). Now that
  the child runs in the delivered tree this only matters for the baseline
  sweep's scratch tree; a row-less mop-up arm is arm 2's territory.
- **Rule 11 adoption proof is NOT run for the briefing clause.** The clause is
  a prompt change, and Rule 11 asks for real-invocation evidence (a `gopi task
  verify` whose child log shows it worked in the given ground and minted no
  second worktree). It was not run here: the evidence available is MECHANICAL
  (unit tests pinning both clause shapes + the ground resolver) and HISTORICAL
  (the child of 01a0a4ae minted `gopi-verify-01a0a4ae/wt-tip` from the
  main-checkout cwd, and six verify reports landed in that same checkout). The
  next real merger verify round on master is the moment to read the child's
  JSONL — treat that as an OPEN item, not as verified.

## Live measurement (the host, 2026-09-16)

Read-only observations, taken while writing this report:

- `git -C ~/work/GoPi worktree list --porcelain` → **7 registrations**: master,
  the branch worktrees of this task and `feat/daemon-scope-unit-per-profile`,
  plus THREE detached scratch checkouts — `gopi-master-baseline`,
  `queue-runtime-merge-build-*/…/wt-fix-mergebuildgate-modcache-cold-control`,
  and `vfy-01a0a4ae/wt` (its directory is **GONE**: `git worktree prune -n -v`
  says "gitdir file points to non-existent location") — a registration no
  sweep's discovery can see (no directory ⇒ nothing to remove); the prune leg
  that drops it is arm 2's unreachable reap.
- `git -C ~/work/GoPi status --porcelain` → **8 entries, ALL `??`**: six
  `tasks/01a0a4*-verify-report.md`, `.probe/`,
  `internal/daemoncwd/workspace/`. Zero tracked changes. That is precisely the
  state master's guard ② could not name (it called it "uncommitted work"), and
  on the MERGER's own checkout it is what an untracked-inclusive gate refuses.
- `~/workspace/gopi-verify-01a0a4ae/` is NOT a worktree (it holds
  `home-shared/`); the measured `wt-tip` checkout the verify child minted is
  gone, so the leak's residue is a plain directory, not a registration.
- The 49 `verified` rows' recorded worktrees: none exists — i.e. the branch's
  protection is released BEFORE the merger verifies it, which is why
  `taskVerifyGround` treats "delivered tree gone" as the DOMINANT case.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → exit 0, no output (OK).
- `go vet ./internal/tools/ ./internal/scratch/` → exit 0.
- Tests: `go test ./internal/tools/ ./internal/scratch/ -count=1` →
  `ok github.com/nick/gopi/internal/tools 150.811s` / `ok …internal/scratch 12.724s`.
- New tests, named: `TestReclaimDirtyVerdictClassifiesUntrackedOnlyResidue`
  (9 sub-cases), `TestReclaimDirtyHeldCoversBothShapes`,
  `TestReclaimUntrackedOnlyWorktreeStillRefused` (a REAL untracked-only
  worktree: the directory survives, the operator line promises no reclaim, and
  the offending file name is quoted back),
  `TestTaskVerifyChildDirPrefersRecordedWorktree`,
  `TestTaskVerifyWorktreeClauseForbidsASecondWorktree`,
  `TestTaskVerifyGround*` (private detached ground is a real checkout of the
  merged state, never the main checkout, removed by its cleanup), plus the
  pre-existing red-proof `TestZZRedProof_DirtyDeadHolderWorkIsOrphaned` — all
  PASS (`-count=1 -v`).
- Gates: `go test ./internal/standards/` → ok (Rules 16-21 green, baseline
  unchanged); `go test ./internal/stylegate/` → FAIL, but every site it reports
  is PRE-EXISTING on master (`cmd/agent/suite_stall_watchdog_test.go` sleeps,
  `internal/tools/testmain_test.go:65` os.Setenv) and **no hit names a file
  this branch touches** (checked by grepping the gate output for each touched
  path). `gofmt -l` on the touched packages reports only
  `internal/tools/task_deliverable_worktree.go`, which this branch does not
  modify (a stray blank line from master commit 44605f00).
- `git status` in the MAIN repo lists only the 8 pre-existing `??` entries —
  none of this branch's files (Rule 6b self-check).
- Commit: `53a2b9da fix(worktree): one dirty-state classifier + a verifier
  ground of its own (arms 1 and 3)`; the devdoc update is the commit after it.

## Files

- `internal/tools/task_reclaim.go` — the classifier + both wordings + `reclaimDirtyHeld`.
- `internal/tools/task_finish_cleanup.go` — 3 gates share the classifier.
- `internal/tools/task_merge_executor.go` — main-checkout gate shares it.
- `internal/tools/dirty_holder_salvage.go` — salvage scope over both shapes.
- `internal/tools/tasks.go` — `taskVerifyChildDir`, `taskVerifyWorktreeClause`, `child.Dir`.
- `internal/tools/worktree_dirty_split_test.go` — regression tests.
