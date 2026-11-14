<!-- doc-class: normative -->
# Which component owns the merge landing path — the decision

**Measured at `9733416d12ab66b5777446b439d1c23775718607`** (this ground's cut
point, branch `task/01a19051dd7176428884a9d78f2455e4`). Every number below was
taken in this run; the commands and raw output are in the session evidence
directory named at the end.

## Verdict: Option B — the `gopi task merge` engine owns the landing path

`internal/tools/task_merge.go` (`TaskMerge`, reached from
`cmd/agent/cli_tasks_merge.go:20`) **is** the merge landing path. The merger
sweep in `internal/tools/repo_sweep.go` is a second CONSUMER of the same
compile-time gates, driven by a pass function that **nothing calls**. The 33
files that cannot be deleted **stay**, and the retirement epic ends here.

No file was deleted in this row. The decision is the deliverable.

**RE-MEASURED 2026-11-01 (task `01a19466`, tree `52657bb6`), and the verdict
holds.** This row re-tested the reachability claim on a current base rather than
trusting the numbers above, and added the compiler as the instrument (a `git grep`
counts text; a rename makes the compiler name every reference that still exists):

```
$ sed -i 's/^func mergerPassOnce(/func mergerPassOncePROBE(/' cmd/agent/cli_tasks.go
$ CGO_ENABLED=0 go build ./...          # EXIT=0 — nothing in the compiled package names it
$ sed -i 's/^func mergerSweepRepo(/func mergerSweepRepoPROBE(/' cmd/agent/cli_tasks.go
$ CGO_ENABLED=0 go build ./...
cmd/agent/cli_tasks.go:6444:3: undefined: mergerSweepRepo      # exactly ONE reference
$ sed -i 's/^func MergerRepoSweep(/func MergerRepoSweepPROBE(/' internal/tools/repo_sweep.go
$ CGO_ENABLED=0 go build ./...
cmd/agent/cli_tasks.go:6478:15: undefined: tools.MergerRepoSweep   # exactly ONE reference
```

Both named references sit inside `mergerPassOnce`, whose own rename builds green —
so the chain is `MergerRepoSweep ← mergerSweepRepo ← mergerPassOnce ← (nothing)`,
confirmed by the compiler rather than by a text count. The same probe run answered
the question this row was dispatched for — is the dormancy DELIBERATE or an
ACCIDENT? — and the answer is **deliberate, with a documentation gap of one
generation**: `runTaskMerger`/`runTaskMergerWithLock` were deleted on purpose by
epic merger-delete-dead-impl (2026-09-19), and the surviving prose kept describing
the pass as live ("What remains live is mergerPassOnce"). That prose is what the
row corrected; `repo_sweep.go`'s BEHAVIOUR was not touched (Rule 30/31).

The probe also measured MORE than the briefing claimed, and the extra half is the
part a reader must not infer away: the pass's serial-face ladder is unreachable
with it, and two of its legs have **live second carriers** while two do not.

| leg | reachable today from |
|---|---|
| the durable-mark drain (`mergerPassCloseLoop`) | **LIVE** — `mergerCloseLoopAfterLanding` ← `runTaskMergeExecute` (`cli_tasks.go:7173`) |
| the cross-node passes (`serveAdoptionSweep`, `serveGitSyncSweep`) | **LIVE** — `gopi task sync-pass --once` (`cli_tasks_sync_pass.go:141-142`) |
| the dead-holder RESUME (`mergerResumeDeadHolderLeg`) | **nothing** — only its test |
| the dead-holder RECLAIM (`finishCleanupDeadHolderReclaimLines` via `mergeDeadHolderReclaimLeg`) | **nothing** — only its test |

The last two are the gap this row could not close from inside its safety boundary
(no behaviour change), so it is stated in the code where the arms are declared
(`cmd/agent/cli_tasks.go`) and recorded here: a terminal row whose holder died
still keeps its branch PROTECTED, and no resident face reclaims its worktree.
Re-homing that ladder onto a live face is re-development work, not a comment fix.

Evidence: `~/.gopi/sessions/01a19466-5201-7d31-a1b4-6c2295fd70d5/evidence/`
(`reachability.txt`, `delete-probe.txt`, `leg-reachability-probe.txt`,
`leg-reachability-redo.txt`, `cli-reachability.txt`).

## 1. The measurement that decides it: which of the two is REACHABLE

The briefing's two options both assume two live landing paths. There is one.

**The sweep's driver is unreachable in this tree.**

```
$ grep -rn 'mergerPassOnce' --include=*.go . | grep -v devdoc | grep -v _test.go | grep -v '//'
./cmd/agent/cli_tasks.go:6316:func mergerPassOnce(opts *tasksOptions) {
$ grep -rn 'mergerSweepRepo' --include=*.go . | grep -v _test.go | grep -v '//'
./cmd/agent/cli_tasks.go:6382:		mergerSweepRepo(repo)
./cmd/agent/cli_tasks.go:6408:func mergerSweepRepo(repo string) tools.RepoSweepResult {
```

`mergerPassOnce` (`cmd/agent/cli_tasks.go:6316`) is its own only mention — **zero
call sites**. `mergerSweepRepo` (`:6408`) is called at `:6382`, which is inside
that unreachable function. `MergerRepoSweep`
(`internal/tools/repo_sweep.go:1276`) is reached from `cmd/agent/cli_tasks.go:6416`,
also inside `mergerSweepRepo`. Its only other production reference in the whole
tree is that declaration.

Its historical driver is gone and the code says so: `runTaskMerger` has **no
function definition left** — `grep -rn 'func runTaskMerger' --include=*.go .`
prints nothing; the surviving mentions are comments
(`cmd/agent/cli_tasks_rebuild.go:10`: "the retired merger tick (runTaskMerger —
deleted 2026-09-19)"). `cmd/agent/cli_verbs.go:115` marks `CliVerbMerger` RETIRED
and `cmd/agent/daemon_retired.go:88` (`retireMergerFace`) is what a run of that
spelling gets.

**The engine's face is live and was executed in this run.**

```
$ case taskverbs.Merge:            # cmd/agent/cli_tasks.go:7031 → runTaskMerge(opts)
$ go run ./cmd/agent task merge -h
gopi task merge — merge accepted branches (the git atom).
  The surviving merge trigger: 'gopi merge' was removed (epic
  merge-verb-removal, 2026-11-01) and this family spelling is what remains —
  the same engine (tools.TaskMerge), the same single-instance lock, the same
  counters, under the spelling the git post-receive hook types.
$ go run ./cmd/agent task merge --repo "$PWD"
task merge (no-submit): merged=0 deleted=0 dispatched=0 duped=0 concluded=0 rework-owned=0 conflict=0 exempt=0 contract-refused=0 build-refused=0 head-off-integration=0 failed=0
```

Handing ownership to the sweep would hand it to a component nothing executes.
**That is not a design choice available to this row.**

## 2. Option A's premise does not exist: the gates are already in the same package

Option A was stated as "gates physically move into the sweep's package
(`internal/tools/repo_sweep.go`) and `gopi task merge` then has no engine".

There is one package. Every one of the 37 files in the merge-named set declares
`package tools`, and so does `repo_sweep.go`:

```
$ head -1 internal/tools/repo_sweep.go internal/tools/merge_build_gate.go \
          internal/tools/merge_contract_gate.go internal/tools/task_merge.go
package tools
package tools
package tools
package tools
$ for f in <the 37 non-test files>; do head -1 "$f"; done | sort | uniq -c
     37 package tools
$ for f in <all 53 files incl. tests>; do head -1 "$f"; done | sort | uniq -c
     50 package tools        # the other 3 start with a file-header comment above `package`
```

A file move between two file names in the SAME package directory is not a
move. **What Option A would actually require is a HARVEST of engine-side symbols
out of `task_merge.go`**, not a relocation of files — measured, the sweep calls
six symbols that are DEFINED in the engine's own file:

```
$ for s in TaskMergeBranchOutcome taskMergeExempt taskMergeRefuseUncompilable \
           taskMergeRefuseManifest repoSweepRouteBuildRefusals repoSweepRouteContractRefusals; do
    grep -rn "^func $s(\|^type $s " internal/tools/*.go | grep -v _test; done
internal/tools/task_merge.go:249:type TaskMergeBranchOutcome struct {
internal/tools/task_merge.go:398:func taskMergeExempt(branch string) bool {
internal/tools/task_merge.go:889:func taskMergeRefuseUncompilable(repo, master, branch, gateName, detail, buildErr, buildCmd string) TaskMergeBranchOutcome {
internal/tools/task_merge.go:976:func taskMergeRefuseManifest(repo, master, branch, manifest, detail string) TaskMergeBranchOutcome {
internal/tools/task_merge.go:1114:func repoSweepRouteBuildRefusals(repo string, outcomes []TaskMergeBranchOutcome, res *RepoSweepResult) {
internal/tools/task_merge.go:1147:func repoSweepRouteContractRefusals(repo string, outcomes []TaskMergeBranchOutcome, res *RepoSweepResult) {
```

Two of those are named for the SWEEP and live in the ENGINE's file — the
dependency already runs sweep→engine, in the direction Option A wanted to
reverse. And the four gates are called by both consumers at symmetric sites:

```
internal/tools/task_merge.go:621:   mergeContractGate(...)     internal/tools/repo_sweep.go:1433: mergeContractGate(...)
internal/tools/task_merge.go:646:   mergeBuildGate(...)        internal/tools/repo_sweep.go:1437: mergeBuildGate(...)
internal/tools/task_merge.go:681:   mergeUnionGate(...)        internal/tools/repo_sweep.go:1441: mergeUnionGate(...)
internal/tools/task_merge.go:696:   manifestUnionGate(...)     internal/tools/repo_sweep.go:1445: manifestUnionGate(...)
```

No gate file is referenced from outside those two call sites.

## 3. Option A would also have to move capabilities the sweep refuses BY DESIGN

If the engine's file were gutted, the sweep would have to take on what the
engine uniquely does. It does none of these today, and three of them are
negative design decisions recorded in the sweep's own comments:

| capability | engine (`task_merge.go`) | sweep (`repo_sweep.go`) |
|---|---|---|
| landing verb | `taskMergeGit(repo, "merge", branch)` — a REAL merge (ort), conflicts possible — `:767` | `repoSweepGit(repo, "merge", "--ff-only", branch)` — fast-forward only, cannot conflict — `:1468` |
| ref deletion | `taskMergeGit(repo, "branch", "-d", branch)` at `:754` and `:777`, each preceded by a `removalaudit.BranchDelete(removalaudit.ArmTaskMergeBranch, …)` record (`:742`, `:771`) | **zero** branch deletions — the landing line's own detail is "the branch ref is REPORTED, never deleted by the sweep" (`:1496`) |
| non-ff branch | real merge, abort + ledger dispatch (`taskMergeDispatchConflict`, `:1179`, called `:807`) | leaves it alone and reports: `repoSweepNotFFOutcome`, `:374`, whose header (`:354`) explains why the park lane was removed |
| conflict resolution | files a rework/conflict task | **no task dispatch at all** — `grep -n 'TaskSubmit(' internal/tools/repo_sweep.go` → no hit |
| build-queue mark after landing | rebuild runs from the CLI face (`runTaskMergeRebuild`, `cli_tasks_merge.go:56`) | `MarkRebuildPending(repo, MarkRebuildAfterLandingReason)` at `:1494` |

And the sweep's own source states the delegation in words:

> `:1501-1506` — "Resolution is the merge command's job (the foreground
> `gopi merge`, whose shipped prompt is `cmd/agent/merge_prompt.md` …), and the
> branch must SURVIVE for that hand-over to diff against. See
> `repoSweepNotFFOutcome`."

*(The quotation above is verbatim, and the source comment it quotes is itself
stale: it still names the removed top-level `gopi merge`. The delegation it
records is live — the branch survives for the hand-over — and the face that
performs it today is `gopi task merge`; the file `man/gopi-merge.md` is the
engine's contract page. Quoted text is not edited; the stale comment is the
source's to fix.)*

Option A would require the sweep to grow a real merge, ref deletion with its
removal-audit arm, and conflict dispatch — i.e. to become the engine — while its
own comments give reasons for not doing so. That is not a retirement; it is
rewriting the landing path and reversing three recorded decisions, with the
removal-audit obligation (`internal/removalaudit/audit.go:277`,
`ArmTaskMergeBranch = "task-merge-branch"`) moving onto a component that has
never written one.

## 4. Every named caller, and its consequence under Option B

Counts are whole-word symbol mentions in this run's `evidence-5`/`evidence-12`;
`repo_sweep.go`'s 18 DISTINCT symbols are the briefing's figure and it
reproduces exactly.

| caller | refs | what it needs from the set | consequence under Option B |
|---|---:|---|---|
| `internal/tools/repo_sweep.go` | **18 distinct** / 28 mentions | `TaskMergeBranchOutcome`, `branchDisposition(s)`, `manifestUnionGate`, `mergeBuildGate(+ActionRefused,Name)`, `mergeContractGate(+ActionRefused)`, `mergeUnionGate(+Name)`, `mergedCleanupWorktrees`, `repoSweepRouteBuildRefusals`, `repoSweepRouteContractRefusals`, `taskMergeExempt`, `taskMergeRefuseManifest`, `taskMergeRefuseUncompilable`, `verdictOperatorWords` | **Unchanged: stays a consumer.** It keeps calling the gates and the refusal wrappers. It is NOT the owner (zero call sites reach it — §1), and it does not call `TaskMerge` at all: `grep -nw TaskMerge internal/tools/repo_sweep.go` hits only comments at `:687` and `:707`. |
| `internal/tools/rebuild.go` | **7 distinct** / 7 mentions | `MergeHeadRev`, `RebuildAfterMergeMergeset`, `VetGateResult`, `mergeBuildGateStatusAbstain`, `mergesetGateResult`, `runPostMergeVetGate`, `vetGateName` | **Unchanged: stays.** `RebuildAfterMerge` (`:150`) is reached from `cmd/agent/cli_tasks_rebuild.go:315` and `:424`; the gate re-run is armed as a defer at `:164`/`:171`. |
| `internal/tools/rebuild_pending.go` | **1 distinct** / 3 mentions | `MergerGracefulDir` (`:45`, `:111`, `:113`) | **Unchanged.** `merger_graceful.go:64` defines it; `merger_graceful.go` is one of the 33 that cannot be deleted (probe: `NEEDED … <- deploy_drift.go, finish_cleanup_survivors.go, git_peer_fetch.go, integration_claim_target.go`). |
| `internal/tools/deploy_drift.go` | **1 distinct** / 3 mentions | `IntegrationBranchName` (`:66`, `:67`, `:68`) | **Unchanged.** It is `merger_graceful.go:83` (`const IntegrationBranchName = "master"`), reached live through `gopi version --drift` → `cmd/agent/cli_version.go:194` → `printDeployDrift` → `tools.DeployDriftAt` (`:249`). |
| `scripts/git-hooks/post-receive` | **0 symbols / 1 verb** | types `"$GOPI_BIN" task merge --all --repo "$GOPI_REPO" --submit` (`:14`); its `:3` comment says "ALL merge logic lives in `gopi task merge --all`" | **Unchanged: still the engine's caller — and it is why Option B is the answer.** The hook is the mechanism by which a push lands. Retiring the engine turns every push into a `--all`-less no-op. **Honest status, measured:** the hook is installed on NEITHER host — `ls ~/work/gopi/.git/hooks/post-receive` and the same on `peerbox` both print `No such file or directory` (rc=2). It is a LIVE CALLER IN THE TREE, a LATENT one on the hosts. Either way its consequence is the same: update it *with* any future verb removal, never before. |
| `cmd/agent/cli_tasks_merge.go` | the CLI face | `tools.TaskMerge` (`:28`), `tools.MergedBranchNames` (`:56`) | **Unchanged: this IS the landing face.** Dispatched at `cmd/agent/cli_tasks.go:7031` (`case taskverbs.Merge:`). |

## 5. What Option B closes, and what it does NOT close

**Closes:** the retirement epic's central question — the landing path's owner is
`internal/tools/task_merge.go`, and the sweep is a consumer. The **33 files stay**:
measured this run with the alone-deletion probe (Rule 43's recipe), 4 of the 37
build green alone and 33 do not, reproducing the sibling row's tally exactly.
One of the 4 (`merger_ancestors.go`) is green alone but is NOT orphaned —
`SelfAncestorPIDs` (`:72`) is called from `cmd/agent/lock_single_instance.go:264`.
So the honest figure is **33 files that cannot be deleted and 3 that are already
scheduled for deletion elsewhere**; the 34th is a build-alone false positive.

**And the 3 are already deleted upstream, by a commit whose own title agrees
with this verdict.** The sibling row's work is published: `origin/master` in the
dev clone is `c8452ee06 remove(merge): delete three orphaned merge lanes, keep
the ones still called`, whose parent is exactly this ground's base `9733416d1`.
So the set arithmetic is closed, measured, not assumed:

```
37  files in the merge-named set at 9733416d1
 −3  deleted by c8452ee06 (merge_ceiling_rework.go, merge_handover_escalate.go,
     merger_status_inventory_block.go — all three DEAD-ALONE in evidence-11)
 −1  merger_ancestors.go: DEAD-ALONE but called from lock_single_instance.go:264
=33  cannot be deleted  ← the figure the briefing names, reproduced
```

Whole-set census at this base (`-gcflags=-e`, all 37 removed at once) reproduces
the sibling row's numbers exactly: **227 undefined-symbol lines, 70 distinct
undefined symbols, 54 distinct files, 0 non-undefined errors**; the ranking is
topped by `repo_sweep.go` (29), `finish_cleanup_survivors.go` (22),
`task_briefing_gate_assert.go` (13), `verify_gate.go` (11),
`rework_landing_probe.go` (10), `rebuild.go` (10). See
`evidence-13-whole-set-census.txt`.

**Does not close — two findings this row measured and did NOT fix (Rule 41):**

1. **`gopi task merge` with no arguments does nothing, while its help page says
   it sweeps the repos table.** `cmd/agent/cli_tasks_merger_help.go:83-84` reads
   "gopi task merge — merge every repo in the settings.json "repos" table (the
   full sweep)", but the CLI stores only `opts.mergeBranches = positionals` and
   `opts.mergeAll = fs.has("all")` (`cli_tasks.go:1890-1891`), and
   `TaskMerge` returns before deciding anything when the list is empty and
   `all` is false (`task_merge.go:467-469`). Measured:
   `go run ./cmd/agent task merge --repo "$PWD"` → every counter 0, rc=0, no
   repo resolved — while the repos table is resolved only inside
   `mergerPassOnce` (`cli_tasks.go:6372`) and
   `runTaskMergeRebuild` (`cli_tasks_rebuild.go:227`). A no-op that reports
   success is the shape Rule 15 exists against. The counters say where the
   early return happened: `head-off-integration=0`, i.e. the engine never
   reached a per-branch decision (this ground's checkout is
   `task/01a19051…`, not `master`, so a per-branch pass would have counted 1
   there) — it returned at `task_merge.go:467-469` with an empty list. Filed as
   its own row; it is a DOC/CODE divergence, not a reason to move the landing
   path.
2. **`mergerPassOnce` is dead code with three tests pinning its TEXT.**
   `cmd/agent/cli_tasks_rebuild_pass_test.go:116` and
   `cli_tasks_rebuild_gate_test.go:108` assert against the literal
   `"func mergerPassOnce("` in the source. A source-text assertion keeps a
   function alive in review while nothing executes it. That is a separate
   question (does the pass get a driver, or does it go?) and it is NOT answered
   by which component owns the landing path — but it must not be answered by
   deleting the engine either, because the pass is one of the engine's callers.

## 6. Why this is the answer and not a deferral

"Retire the 33 files" cannot be reached from here in either direction:

- **Via Option A** it requires the sweep to become the engine (§3) — new merge
  semantics, ref deletion with its audit arm, conflict dispatch — while the
  sweep itself is unreachable (§1). Work spent there lands on code nothing runs.
- **Via plain deletion** it is blocked by 70 undefined symbols across 54 files
  (the sibling row's `-gcflags=-e` census, `evidence-C` in that session), of
  which `repo_sweep.go` alone raises 18 and `rebuild.go` 7. Neither is a
  deletion; both are a redesign.

So the decision is not "we could not decide" — it is that **the engine is the
only landing path anything reaches, the sweep is dormant, and the gates are
already shared by both in one package.** Option B. The epic ends.

## Evidence

Session directory: `~/.gopi/sessions/01a19051-dd71-7642-8884-a9d78f2455e4/artifacts/`

| file | what it holds |
|---|---|
| `evidence-1-landing-arms.txt` | the two landing verbs, both actors' package clause, the four gate call sites on each side, the hook line |
| `evidence-3-driver-reachability.txt` | `mergerPassOnce` / `mergerSweepRepo` / `runTaskMerger` reachability across the whole tree |
| `evidence-5-caller-census.txt` | every named symbol, every production reference site |
| `evidence-6-reachability.txt` | `MergerRepoSweep` and `mergerSerialFaceLegs` call sites |
| `evidence-8-verb-live.txt` | `go run ./cmd/agent task merge -h` — the live face |
| `evidence-10-noarg-run.txt` | `go run ./cmd/agent task merge --repo "$PWD"` — the no-op finding |
| `evidence-11-alone-probe.txt` | Rule 43 alone-deletion probe over all 37 files, plus the restore proof (`git status --porcelain` = 0 lines) |
| `evidence-12-decision-core.txt` | the decisive reachability pair, and the six engine-side symbols the sweep calls |
