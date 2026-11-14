<!-- doc-class: normative -->
# Task Report: the deterministic checklist before ANY arm removes a run's worktree

**Task:** `01a0c677-cc74-73a4-b977-5ff094084047` (filed by this session; the epic is
the RECLAMATION end of *"the worker owns the child's ground"*).
**Branch:** `fix/reclaim-deterministic-checklist`
**Worktree:** `/home/user/workspace/gopi-reclaim-check`

## Why this exists

On 2026-09-21 the arm `task-self-reclaim` freed `~/workspace/gopi-jieba-searchkw`
while its ledger row `01a0c574-cb48-76ef-9d76-ac93d756e833` was **`in_progress`**,
its worker pid **1191072 alive**, and branch `fix/jieba-token-absorbs-search-kw`
**14 ahead** of master. The same night `finish-cleanup-worktree` freed
`fix/stash-guard-compound-chain` with the evidence string *"delivery chain's local
sync landed: branch already contains the integration branch — nothing to merge."*

**Root cause (5-why).** `reclaimDisposition` guards the **directory** (dirty /
cwd-liveness / branch-match) and `reclaimLiveSessionRefusal` — its first guard —
is gated on `taskLiveHolderStatus(r.Status)`, so it only fires for rows whose
status is *still* live. `reclaimSelfRowsFrom` explicitly includes `in_progress`
plus the three terminal states, releasing on a **claim-stamp match alone**. So
both arms' standing was *"a stamp matched"*, never *"the work is finished"* — and
`sync.OK()` / *"local sync landed"* is a statement about a **merge**
("the branch already contains the integration branch"), which was being read as a
statement about the **work**.

## Completed

- **`internal/tools/reclaim_ground_checklist.go` (new)** — `reclaimGroundChecklistVerdict(repo, worktree, branch)`: the ONE deterministic checklist, returning `""` or a NAMED refusal sentence. Two rungs:
  - **① the ground is not mine** — a process must never free the checkout it is standing in (`git worktree remove` deletes the directory under the running process; the loss stays invisible until something asks for its own cwd).
  - **② the branch shares history with the integration branch** — `git merge-base <integration> <branch>` (the direct measurement; `--is-ancestor` is its one-way projection and would additionally refuse a healthy branch that is merely *behind*). This is the **same question** `cmd/agent/merge_run.go`'s `mergeHasCommonAncestor` asks for the merge lane, so the two lanes cannot disagree about a branch's lineage. **git failing for any reason is a refusal too** (Rule 15: an unmeasurable tree is never rendered as a measurement) — this arm removes directories, so the conservative direction is keep.
- **`internal/tools/guest_row_guard.go`** — `guestRowOwnCheckoutRefusal(worktree)`, the ground rung's predicate. The actor's position comes from the **shared memoized reader** `worktreeSweepSelfOwnedRepo()` (the same source the guest rungs read through `guestRowRepoVerdict`); it is deliberately **not** asked through `guestRowRepoVerdict("")`, which answers an *empty* verdict with no candidate repo and would have made the rung a silent no-op (measured while wiring it). One refusal sentence, `guestRowOwnCheckoutRefusalFmt`.
- **Every removal arm routed through the checklist** (grep census, `internal/tools`):

  | arm | `removalaudit.WorktreeRemove` call site | checklist | evidence string extended |
  |---|---|---|---|
  | `task-self-reclaim` | `task_reclaim_self.go:210` | ✅ before the audit (`:180`) | ✅ |
  | `task-self-own-sweep` | `task_reclaim_self.go:647` | ✅ before the audit (`:640`) | ✅ |
  | `task-row-reclaim` | `task_reclaim.go:441` | ✅ before the audit (`:431`) | ✅ |
  | `worktree-sweep-reclaim` | `worktree_sweep.go:2248` | ✅ **before** the audit (`:2245`) | ✅ |
  | `finish-cleanup-worktree` | `task_finish_cleanup.go:1637` | ✅ **before** the removal (`:1609`) | ✅ |
  | `finish-cleanup-worktree-force` | `task_finish_cleanup.go:3292` | ✅ before the plan is rendered (`:3264`) | ✅ |
  | `task-ground-anchor` | `run_ground_anchor.go:414` | ✅ before the audit (`:391`, added 2026-09-26) | ✅ |

  **A tenth arm arrived after this report was written** (`run_ground_anchor.go`,
  `ArmTaskGroundAnchor` — the run-ground sweep's collection step, commit
  `9bad5b42`), and it arrived UNGUARDED. It is a sweeper like `worktree-sweep-reclaim`,
  so it asks the checklist in its **plain** form, not the close-out form: its
  liveness ladder proves the run that *anchored* the ground is over, which is a
  statement about the TAKER and never a licence to delete a directory this
  process is standing in. Its refusal SKIPS the ground (`skipped = append(…)`)
  and leaves the worktree, the registration and the branch untouched; the rung is
  asked with the **anchor's own branch**, so an anchor carrying none skips rung ②
  — the same "a registration without a branch has no lineage to judge" reading
  the checklist documents elsewhere.

  Line numbers re-measured at hand-over against the final tree
  (`grep -rn "removalaudit.WorktreeRemove(\|reclaimGroundChecklistVerdict(" internal/tools/*.go`
  — **10** production `WorktreeRemove` sites (re-measured 2026-09-26), **7** of them
  preceded by a checklist call, the other three being the reviewed
  exemptions in the table below; the only remaining `WorktreeRemove` string in
  the package is the gate test's own marker constant). A first pass of this
  census carried line numbers that had drifted and put the sweep arm's cell at
  "on the reclaim branch" — re-measured here, because a census whose rows cannot
  be resolved is a census nobody can check (Rule 13's discipline applied to
  prose).

  **The gate's marker was a WHOLE CALL SPELLING and that is what drifted**
  (measured 2026-09-26, epic `master-reds-internal-tools`): it matched
  `reclaimGroundChecklistVerdict(` — the plain rung — while the delivery chain's
  two arms legitimately ask `reclaimGroundChecklistVerdictForCloseout(`, a
  spelling the literal does NOT match. The gate therefore read those two arms as
  UNGUARDED from the day the close-out entry point was introduced, and the
  missing anchor-arm rung was reported together with them as one three-arm red.
  The fix enumerates the call shapes (`guardMarkers`, a declared list of the
  checklist's entry points) instead of loosening the match to a bare identifier:
  the trailing paren is kept, so a comment naming the predicate still counts for
  nothing — `TestEveryWorktreeRemovalArmQuestionsTheGround` now also asserts that
  every occurrence of `reclaimGroundChecklistVerdict` in the package's non-test
  sources is a CALL, not prose. The reason the bare identifier was rejected is
  worth keeping: `grep` finds the predicate named on ~10 comment lines in
  `reclaim_ground_checklist.go` and `task_finish_cleanup.go` alone, and a marker
  satisfiable by prose is a gate that measures nothing. A THIRD entry point must
  be added to `guardMarkers` deliberately — until it is, the arms asking it read
  unguarded and fail loudly rather than passing blind.

  All six reference `reclaimGoalChecklistSummary` (`"own-ground + integration-lineage (reclaim_ground_checklist.go)"`) in their `removalaudit` evidence string — one spelling, so a reader can grep every record an arm wrote and see whether the checklist was consulted. **This is the "record the verdict per arm" deliverable, and it cost no new mechanism:** `removalaudit.WorktreeRemove` already takes the evidence string; the change is the string.

  **The other two removal sites in the package, each reviewed and exempted in-tree** (not oversights — the guard test pins the reasons). **It was three until 2026-10-30**: the `verify-ground` row that used to sit here named `internal/tools/tasks.go:7463`, the verifier's own private detached ground. Slice 2/4 of epic `01a18799` retired the `verify` verb and deleted `taskVerifyGround` — the function that both created and freed that checkout — so there is no site left to exempt and `removalaudit.ArmVerifyGround` went with its emit (`internal/removalaudit/audit.go`). The count below is the re-measured 11/9/2, not the pre-retirement 12/9/3:

  | arm | call site | why no rung applies |
  |---|---|---|
  | `merge-build-gate-ground` | `merge_build_gate.go:658` | the gate's OWN scratch checkout (`--detach`ed, no branch) — rung ① has no subject, rung ② has no branch |
  | `merged-cleanup-worktree` | `task_merged_cleanup.go:577` | its ladder is **strictly stronger** on both questions: ⑤b `finishCleanupWorktreeInUse` answers rung ① per-worktree, ④'s `merge-base --is-ancestor` recheck answers rung ② (merged ⇒ descended) |
- **A failed check never removes anything.** Every refusal path appends a NAMED line and `continue`s / `return`s; the worktree and the branch stay exactly as they are, and no arm reaches `--force` to get past a check.
- **Two real defects the Rule 31 transcript caught on the sweep arm, both fixed** (neither `go build` nor the suite could see either — the arm's refusal path was never driven):
  - the refusal finding was appended **twice** — the switch's own `res.Findings = append(...)` *plus* the trailing append below the switch (the same finding, printed twice in the plan);
  - the refusal inherited the **judge's** rule string (`unclaimed-reclaim`), so `task sweep-worktrees` rendered the keep under the very rule that had said reclaim — it read as "the judge changed its mind" instead of naming the arm that refused it. Fix: a new arm constant `worktreeRuleGroundChecklist` (`"ground-checklist"`) is stamped and the switch is **left** (`break`); the ONE append below the switch is now the only place a finding joins the report. The new constant tripped the ladder's own citation gate (`assertEveryJudgeArmIsCited` — 3 post-judge arms now, not 2), which is that gate working; it was updated and cited.
  - the rung-② sentence also printed the git argv **twice** (`…: git merge-base master X: exit status 1` → the doubled prefix was dropped: git's own error text already names the verb and both revisions).
- **`internal/tools/worktree_removal_ground_gate_test.go` (new) — the exhaustive anti-regression guard.** Every `removalaudit.WorktreeRemove(` site in the package must be either checklist-guarded or carry a reviewed `reclaim-checklist-exempt: <reason>` above it; read floors stop the scan from reading green-blind (they now read **≥10 arms, ≥7 guarded** — re-measured 2026-09-26; they were ≥9/≥6 before the anchor arm landed), and it red-proofs (a 7th, unguarded arm fails it: *"zz_redproof_tmp.go:6 frees a worktree without the ground checklist"*). Measured on 2026-10-30 (`go test ./internal/tools -run TestEveryWorktreeRemovalArmQuestionsTheGround -count=1 -v`): **11 arms (inventory 11) — 9 checklist-guarded, 2 reviewed-exempt**. The TWO exemptions are the arms whose ground is their OWN and which therefore have no rung to ask: `ArmMergeBuildGateGround` (the build gate's own scratch checkout, detached, no branch), and `ArmMergedCleanupWorktree` (its ladder is strictly stronger on both questions: ⑤b `finishCleanupWorktreeInUse` answers rung ① per-worktree, and ④'s `merge-base --is-ancestor` recheck answers rung ② — merged *is* descended). Each reason sits in the comment block above the call, in-tree, where the next reader meets it. A third exempt arm, `ArmVerifyGround`, is **no longer a site** — see the note under the exempt table below.
- **Four new tests** in `internal/tools/task_reclaim_own_ground_test.go`: the ① end-to-end case (a row the process holds, naming the directory the process stands in → refuse, directory survives, branch survives; with the guard disabled the same run frees it — the red proof), the ① predicate's narrowness (`TestOwnGroundRungIsNarrow`: the actor's own checkout refused, any other path — including `""` — not), and the ② case (`TestReclaimSelfRefusesAnUnrelatedHistoryBranch`: an orphan-root branch whose `git merge-base master <branch>` genuinely reports no common ancestor → refused, worktree left alone).

## Not Done (honest)

- **The liveness veto proper is NOT in this change — and as of the 01a0c677 run it is ALSO NO LONGER OPEN.** Both halves are recorded honestly here, because the second supersedes the first:
  - *This change's standing (2026-09-22, unchanged):* the veto lived in `reclaimDisposition` / `reclaimLiveSessionRefusal` / `reclaimSelfRowsFrom` — the functions the sibling ticket **`01a0c642`** (the disposition/status lie) already changed. Per constraint 8 I did **not** duplicate it: I kept this change to the two rungs neither that ticket nor any existing guard answers. At the moment this report was written, **the live-holder veto itself was still open.**
  - *Superseded 2026-09-23 (measured, not assumed):* the veto **is now on master and is load-bearing**. `reclaimDisposition`'s Guard ⓪ is `reclaimLiveSessionRefusal` (`internal/tools/task_reclaim.go:502`, predicate at `:222`), sitting beside Guard ⓪′ `guestRowReleaseRefusal`, and `reclaimSelfRelease` reaches it through `reclaimDisposition` (`task_reclaim_self.go:180-183`); the sweep arm reaches it at `worktree_sweep.go:1913`. **Red proof, run:** disabling Guard ⓪ (`if why := reclaimLiveSessionRefusal(r); why != "" && !true {`) turns **four** tests red — `TestSelfReclaimRefusesALiveInProgressRowsGround`, `TestReclaimRefusesALiveRunnersGroundWhenTheRowIsTerminal`, `TestReclaimDispositionSkipsAnInProgressRowWithALiveRunner`, `TestReclaimRefusesADirectoryALiveRowsSessionStandsIn` — and the first prints the incident verbatim: *"a live in_progress run's ground was FREED … the pass reported: [own worktree released: feat/redproof-live-runner (t-self-live-inprogress) freed for merge; branch kept]"*. Restored → green. So the epic's end state (*"a non-terminal row, or any row with a live holder, must refuse"*) is **satisfied on master**; what this branch added is the two rungs above **plus the coverage that drives the two `finish-cleanup` arms into their refusal**, which is where the census table below was previously honest about having none.
- **The coverage gap named here has since been CLOSED** (2026-09-23, this epic's second pass): `internal/tools/reclaim_arm_refusal_coverage_test.go` drives **both `finish-cleanup` arms into their refusal** — `TestFinishCleanupForceArmRefusesAnUnrelatedHistoryBranch` asserts the rung-② sentence, the survival of directory/registration/branch, and that a refused plan never renders `--force`; `TestFinishCleanupServerArmRefusesAnUnrelatedHistoryBranch` drives the server recycle in APPLY mode; `TestFinishCleanupServerArmRefusesALiveOpenRowsGround` drives the completion fact (*open row + live holder ⇒ still names the ground*) in both plan and apply mode. It also supplies the epic's T1 at the real producer pass (`TestSelfReclaimRefusesALiveInProgressRowsGround`). **Red-proved both ways:** the T1/Rule-31 shape by killing the holder, and the lineage rung by disabling it — with rung ② stubbed to `return ""` the force arm, the self-reclaim arm and the sweep arm all go red (`the --force arm must NOT free an unrelated-history branch's ground`), restored → all green. *(The earlier text of this bullet — "`finish-cleanup-worktree[-force]`'s refusal remains unexercised at any level" — was true when this report was written and is kept visible here rather than deleted, per Rule 26's "an incomplete fix must be narrow, never wrong".)*

## Verification

- **Build:** `CGO_ENABLED=0 go build ./...` → exit 0, no output.
- **Vet:** `go vet ./internal/tools/ ./internal/removalaudit/` → exit 0.
- **gofmt:** `gofmt -l` on the six touched files → no output. (`gofmt -l internal/tools/` lists three files — `memory_sync_assets.go`, `task_staleness_guard_test.go`, `taskledger_cost_test.go` — **pre-existing on master, untouched by this branch**. Never `make fmt`.)
- **Tests:** `go test ./internal/tools/ -run 'TestReclaimSelf|TestOwnGroundRung' -count=1` → **ok 20.458s**.
- **Rule gates:** `go test ./internal/standards/ -run 'TestGate1|…|TestGate6|TestTestDBScaffold|TestSmokePath' -count=1` → **ok 3.754s**.
- **Full standards suite:** RED, but **pre-existing and not mine** — `docclass_gate_test.go` fails on `devdoc/handoff-20260922-0503.md` and three siblings (missing `<!-- doc-class: historical -->` banners). **Measured on the main repo `/home/user/work/GoPi` at master: the identical failure (`741 markdown docs … 1 mislabelled`).** No file this branch touches is named in that gate.
- **Rule 31 — the real surface, run.** Surface changed: the `worktree-sweep-reclaim` arm (its route into the shared checklist, its finding stamp, its audit evidence string). Invocation, verbatim (fixture: a throwaway repo with a NORMAL-lineage registration `wt-a` and an ORPHAN-root registration `wt-b`, both aged 72h; ledger / settings / worktree root / audit sink all pinned off the operator's profile; cwd inside the repo, as an operator would be):
  ```
  CGO_ENABLED=0 go build -o /home/user/workspace/_evidence/gopi-reclaim ./cmd/agent/
  cd /home/user/workspace/gopi-reclaim-check && bash /home/user/workspace/_evidence/rule31-sweep.sh
  # (the script's own step: cd <fixture-repo> && gopi task sweep-worktrees --apply --min-age-h 1)
  ```
  **Result, observed** (`exit 0`):
  ```
  sweep-worktrees (APPLY): repos=1 reclaimed=1 would-reclaim=0 reaped=0 branches-reaped=0 escalated=0
    keep   [main-checkout] age=0h /…/_reclaim-rule31/repo (the main checkout (removing it would delete the repository))
    reclaim [unclaimed-reclaim] age=72h /…/wt-a (no task row owns it, no live holder, clean, age 72h0m0s …)
    keep   [ground-checklist] age=72h /…/wt-b (refusing to free the ground of a branch sharing no ancestor with master — an unrelated-history branch is not an ordinary deliverable: git merge-base master fix/orphan-root: exit status 1: (no output))
  --- after ---   GONE /…/wt-a        EXISTS /…/wt-b
  --- removal-audit records ---  1 "arm":"worktree-sweep-reclaim"
  "evidence":"no task row owns it, no live holder, clean, age 72h0m0s (…); checklist own-ground + integration-lineage (reclaim_ground_checklist.go)"
  --- the branch the freed registration was on (must survive) ---  95742f449f7bf2c7cf8a74c18a20b0b1bef56102
  ```
  The plan NAMES `wt-b` **once**, under the arm's own rule `[ground-checklist]` (not the judge's `unclaimed-reclaim`), the branch of the freed registration survives, and the checklist summary rides the arm's **existing** audit evidence string. The registration on the orphan branch is still there afterwards — a refusal never removes.
- **Rule 31 — the stale-artifact trap that produced a false transcript first (recorded so it is not repeated).** The first transcript was captured from a copy of the binary built *before* the fixes; the re-run printed the pre-fix behaviour (the judge's rule string, the doubled argv prefix) and the fix was momentarily treated as not working. The truth was in the artifact, not the code: `BIN` pointed at an earlier build whose timestamp nobody checked. Every transcript above now builds the binary and runs it **in one chained command**. A memory record was stored (`#4729`).

## Acceptance evidence (self-filed)

- `grep -rn "guestRowOwnCheckoutRefusal\|reclaimGroundChecklistVerdict" --include=*.go internal cmd` **on master → 0 hits**: the rungs do not exist there, which is why the ① test's `own-ground rung disabled` half and the ② test are red on the pre-fix tree.
- ① test output, rung wired: `OWN own worktree release refused: feat/self-release (t-reclaim-own-ground) — … OUTCOME … exists=true`.
- ① test output, guard disabled: `OWN own worktree released: feat/self-release (t-reclaim-own-ground) freed for merge; branch kept` + `exists=false` — the defect reproduced from the fixed source.
- **Conflicts: zero.** Rung ② is `git merge-base`, which measures lineage, not mergeability; nothing in this diff calls `mergeMeasureConflict`, reports a conflict, or carries a conflict field. `grep -rn "mergeMeasureConflict" <this branch's diff>` → the only hits are the pre-existing ones in `cmd/agent/merge_run.go`/`merge_union.go`, untouched.
- Rule 30: `git status --porcelain` in the worktree before hand-over — printed below.

## Hand-over re-verification (run at the end, on the final tree)

Chained, in `/home/user/workspace/gopi-reclaim-check`:

```
CGO_ENABLED=0 go build ./...                       → exit 0, no output
go vet ./internal/tools/                           → exit 0, no output
gofmt -l <the 9 touched/added .go files>           → no output (all clean)
go test ./internal/tools/ \
  -run 'TestReclaimSelf|TestOwnGroundRung' -count=1   → ok 22.969s
go test ./internal/tools/ \
  -run 'TestReclaimSelfRefusesItsOwnCheckout|TestReclaimSelfRefusesAnUnrelatedHistoryBranch|TestOwnGroundRungIsNarrow|TestSweepRefusesAnUnrelatedHistoryBranchFromTheRealVerb|TestSweepGroundChecklistStampsItsOwnRule|TestEveryWorktreeRemovalArmQuestionsTheGround' \
  -count=1 -v                                      → ok 12.614s, 6/6 PASS
git status --porcelain                             → (empty — tree clean)
```

The arm census the table above cites is that same run's output, not a memory of
an earlier one. The report's own duplicated line ("The live-holder veto itself is
still open", printed twice in `## Not Done`) was removed in the hand-over commit
— the statement appears once, where it belongs.
