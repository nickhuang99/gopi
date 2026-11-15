<!-- doc-class: normative -->
# The `merge` capability: what is in the tree, and what has to be harvested

> **Status (2026-11-01): the removal this inventory planned is DONE.** The
> top-level `gopi merge` verb is gone; `./bin/gopi merge` answers exit **1**. This
> page is read as the PLAN and its measurement, not as a description of the tree
> — the "nothing was deleted" sentence below is the state at the revision it
> names, and the deletions it schedules have since happened. The engine itself
> survives under `gopi task merge`.

**Scope of this document.** Inventory only. This is item 1 of the epic "remove
the `gopi merge` capability"; nothing was deleted, no production file was
changed, and `go build ./...` is green at this revision. Everything here is a
measured reading of ONE tree — the ground's `HEAD = 546f20f` — with the command
that produced it. Where a statement is an inference rather than a measurement it
says so in the sentence.

**Why an inventory exists at all.** A removal is not "delete the files whose
name contains `merge`". Rule 43 is the reason: a file name says WHY a file was
created, not WHO ELSE lives inside it, and this family is where that trap has
been measured before (`deliverableRepo`: 24 false hits from
`deliverableRepoFromShow`, `deliverableRepoOnly`, and a test helper's
*parameter*). Three more instances turned up here and are recorded in §5.

---

## 1. The census — every merge-named Go file, measured

```bash
git ls-files -- '*.go' | grep -Ei 'merg.*\.go$' | sort
```

73 files: **51 non-test files / 25,993 LOC** and **22 test files / 8,091 LOC**,
**34,084 LOC in total**. LOC is `git show HEAD:<path> | wc -l`.

The disposition column is the Rule 43 answer for the file as a whole:

- `dies-with-mechanism` — every symbol it defines is referenced only inside this
  file set (or by its own tests). Delete the file with the mechanism.
- `contains-harvest` — it also defines at least one symbol with a **code**
  reference from a file that SURVIVES the removal. The file dies, but those
  symbols must be moved out FIRST, in the same commit (§4). A reference from
  `cmd/agent/cli_tasks.go` or `cmd/agent/main.go` does not count: those two files
  are the dispatcher HOOK, so they die with the mechanism (§3).
- `owns-external-face` — the file's *purpose* is a surface something else needs.

Two files are marked `owns-external-face` by reading, not by the grep: see §4.4.

**ONE ROW ABOVE WAS CORRECTED BY A LATER MEASUREMENT (2026-10-31, before item 3
ran).** `cmd/agent/cli_tasks_merger_help_surface_test.go` is listed
`dies-with-mechanism`, and for the CANONICAL half of it that is right. But the
file held TWO populations with opposite lifetimes: the canonical `gopi merge`
page's guards (die with the face) and the SURVIVING atoms' nine content guards
(`gopi task merge` / adopt / audit-merges / merged-cleanup / merge-execute —
pages that keep shipping under the family spelling the post-receive hook
types). Deleting the file whole would have left
`taskMergerAtomUsage` / `mergerAtomUsageHead` / `mergerAtomLines` with no
content guard at all: the width and note-region gates in other files measure
line LENGTH and pointer STRUCTURE, never what a page SAYS, and the runner
reports the vacuum as `[no tests to run]` — a green indistinguishable from a
clean package. The survivors now live in
`cmd/agent/cli_tasks_merger_help_atoms_test.go`; the remaining 294-line file
holds only the refusal half and may be deleted with the face. Rule 43's answer
for that row is therefore `contains-harvest`, and the harvest has been done.


The full table (73 rows) is generated from the census, not typed. Disposition
column: `dies-with-mechanism` / `contains-harvest` / `owns-external-face` (§2
defines them, and how each was derived).

> **THE TABLE IS A READING OF ONE TREE, AND SEVEN OF ITS ROWS HAVE SINCE DIED.**
> The census was measured at `546f20f`; the epic it belongs to then deleted what
> the dispositions called for. This inventory is NOT re-derived at HEAD (that
> would be a different document with a different population), so a `LOC` below
> is that revision's size, never the file's size now. What changed, measured
> 2026-11-01:
>
> * `internal/tools/merge_handover_counter.go` (row `dies-with-mechanism` **and
>   the disposition was RIGHT** — its own symbols had no production caller once
>   `cmd/agent/merge_run.go` went) — **deleted**, with `taskNotifyAlarmEvents`'
>   `MergeHandoverEvent` entry, because an alarm with no writer is dead the same
>   way. `merge_handover_escalate.go` (`dies-with-mechanism`, listed at the
>   `citation-baseline.tsv` too) had already gone in `c8452ee0`.
> * Everything else in the table is still the sentence it was: a snapshot.
>
> The one row this note CORRECTS rather than confirms: `cmd/agent/merge_run.go`
> itself is absent from the table's own list because §2 measures merge-named
> `*.go` under `git ls-files`, and item 3 had already cut it — the page's §2
> and §5 both still treat it as present. Read those two sections as history.

| file | LOC | disposition | symbols to harvest before it dies |
|---|---:|---|---|
| `cmd/agent/cli_tasks_merge.go` | 68 | dies-with-mechanism | — |
| `cmd/agent/cli_tasks_merge_exec_test.go` | 63 | dies-with-mechanism | — |
| `cmd/agent/cli_tasks_merged_cleanup_test.go` | 243 | dies-with-mechanism | — |
| `cmd/agent/cli_tasks_merger_help.go` | 556 | owns-external-face | — |
| `cmd/agent/cli_tasks_merger_help_carrier_split_test.go` | 266 | dies-with-mechanism | — |
| `cmd/agent/cli_tasks_merger_help_note_reader_gate_test.go` | 620 | dies-with-mechanism | — |
| `cmd/agent/cli_tasks_merger_help_surface_test.go` | 1131 | dies-with-mechanism | — |
| `cmd/agent/merge_already_landed.go` | 157 | dies-with-mechanism | — |
| `cmd/agent/merge_backstop.go` | 494 | dies-with-mechanism | — |
| `cmd/agent/merge_cwd_repo.go` | 130 | dies-with-mechanism | — |
| `cmd/agent/merge_dispatch.go` | 24 | dies-with-mechanism | — |
| `cmd/agent/merge_handover_budget.go` | 123 | dies-with-mechanism | — |
| `cmd/agent/merge_lock.go` | 170 | dies-with-mechanism | — |
| `cmd/agent/merge_preland_gate.go` | 689 | dies-with-mechanism | — |
| `cmd/agent/merge_run.go` | 4183 | dies-with-mechanism | — |
| `cmd/agent/merge_stop.go` | 856 | dies-with-mechanism | — |
| `cmd/agent/merge_stop_nudge_esrch_test.go` | 186 | dies-with-mechanism | — |
| `cmd/agent/merge_tick_retired.go` | 114 | contains-harvest | `reportRetiredMergeTickKnobs` |
| `cmd/agent/merge_union.go` | 636 | dies-with-mechanism | — |
| `cmd/agent/merge_union_commit_seam_test.go` | 235 | dies-with-mechanism | — |
| `cmd/agent/merge_union_rows.go` | 91 | dies-with-mechanism | — |
| `cmd/agent/merger_retired.go` | 101 | dies-with-mechanism | — |
| `cmd/agent/merger_work_lock.go` | 282 | contains-harvest | `mergerWorkLock`,`inheritedLockHandleForExemptFace` |
| `cmd/agent/retired_merge_suite_survivors_test.go` | 118 | dies-with-mechanism | — |
| `internal/standards/testdata/config-doc-historical-anchor/internal/tools/merge_anchor_gate.go` | 1004 | dies-with-mechanism | — |
| `internal/tools/finish_cleanup_merge_lock_test.go` | 68 | dies-with-mechanism | — |
| `internal/tools/merge_anchor_gate.go` | 1078 | dies-with-mechanism | — |
| `internal/tools/merge_anchor_gate_trail.go` | 276 | dies-with-mechanism | — |
| `internal/tools/merge_build_gate.go` | 1245 | contains-harvest | `mergeBuildGate`,`mergeBuildGateScratch`,`mergeBuildGateReapRegistrations`,`mergeBuildGateBranchAlreadyInTarget`,`mergeBuildGateWorktreeDirName`,`mergeBuildGateEnv`,`mergeBuildGateUnresolvedModules`,`mergeBuildGateDiskExhausted`,`mergeBuildGateDiskExhaustedErr`,`mergeBuildGateTail` |
| `internal/tools/merge_build_gate_gocache.go` | 590 | contains-harvest | `mergeBuildGateGoCache` |
| `internal/tools/merge_build_gate_modcache.go` | 152 | contains-harvest | `mergeBuildGateSharedModCache` |
| `internal/tools/merge_build_gate_scratch_worktree_helpers_test.go` | 169 | dies-with-mechanism | — |
| `internal/tools/merge_build_gate_scratch_worktree_test.go` | 181 | dies-with-mechanism | — |
| `internal/tools/merge_ceiling_rework.go` | 160 | dies-with-mechanism | — |
| `internal/tools/merge_config_anchor_gate_test.go` | 1619 | dies-with-mechanism | — |
| `internal/tools/merge_contract_gate.go` | 404 | contains-harvest | `mergeContractGate` |
| `internal/tools/merge_handover_counter.go` | 332 | dies-with-mechanism | — |
| `internal/tools/merge_handover_escalate.go` | 178 | dies-with-mechanism | — |
| `internal/tools/merge_pass_verify.go` | 626 | dies-with-mechanism | — |
| `internal/tools/merge_revert.go` | 232 | contains-harvest | `FailedMergeRevert`,`RevertNotify`,`mergeRevertGit`,`FailedMergeRevert.Retriable`,`FailedMergeRevert.RestoreMaster` |
| `internal/tools/merge_union_gate.go` | 797 | contains-harvest | `mergeUnionGate`,`errTextOr` |
| `internal/tools/merger_ancestors.go` | 74 | dies-with-mechanism | — |
| `internal/tools/merger_graceful.go` | 179 | contains-harvest | `MergerGracefulDir` |
| `internal/tools/merger_status.go` | 623 | contains-harvest | `LockHolderNote`,`LockProbe`,`LockEvidences`,`ReadStampedPID`,`OrphanLockHolder` |
| `internal/tools/merger_status_candidate.go` | 346 | dies-with-mechanism | — |
| `internal/tools/merger_status_disposition.go` | 492 | contains-harvest | `verdictOperatorWords`,`branchDisposition`,`branchDispositions` |
| `internal/tools/merger_status_inventory_block.go` | 83 | contains-harvest | `RepoSweepPreview.InventoryBlock`,`RepoSweepPreview.inventoryProtectedNote` |
| `internal/tools/mergeset_cmdagent.go` | 296 | dies-with-mechanism | — |
| `internal/tools/mergeset_cmdagent_test.go` | 305 | dies-with-mechanism | — |
| `internal/tools/mergeset_gendoc.go` | 532 | dies-with-mechanism | — |
| `internal/tools/mergeset_gendoc_base.go` | 182 | dies-with-mechanism | — |
| `internal/tools/mergeset_stylegate.go` | 195 | dies-with-mechanism | — |
| `internal/tools/mergeset_stylegate_helpers_test.go` | 178 | dies-with-mechanism | — |
| `internal/tools/mergeset_stylegate_test.go` | 428 | dies-with-mechanism | — |
| `internal/tools/rebuild_vet_mergeset.go` | 997 | contains-harvest | `mergesetGateResult`,`mergesetGateResult.Failed`,`mergesetGateResult.Ref`,`mergesetGateResult.Summary`,`RebuildAfterMergeMergeset`,`MergeHeadRev` |
| `internal/tools/retired_merge_suite_survivors_test.go` | 479 | dies-with-mechanism | — |
| `internal/tools/safe_merge_contract.go` | 142 | dies-with-mechanism | — |
| `internal/tools/task_finish_premerge.go` | 869 | owns-external-face | `finishCleanupBranchCheckVerify`,`finishCleanupBranchCheckGroundOnBranch`,`finishCleanupPreMergeRev`,`finishCleanupBranchCheckReason`,`finishCleanupPreMergeRefusedReason` |
| `internal/tools/task_finish_premerge_census.go` | 115 | dies-with-mechanism | — |
| `internal/tools/task_finish_premerge_census_test.go` | 128 | dies-with-mechanism | — |
| `internal/tools/task_finish_premerge_checks_test.go` | 508 | dies-with-mechanism | — |
| `internal/tools/task_finish_premerge_prune_scope_test.go` | 180 | dies-with-mechanism | — |
| `internal/tools/task_finish_premerge_smoke.go` | 479 | dies-with-mechanism | — |
| `internal/tools/task_merge.go` | 1260 | contains-harvest | `resolveConflictRowIdentity`,`TaskMergeBranchOutcome`,`taskMergeExempt`,`isGitExit1`,`taskMergeRefuseUncompilable`,`taskMergeRefuseManifest`,`reworkSummaryLive`,`repoSweepRouteBuildRefusals`,`repoSweepRouteContractRefusals` |
| `internal/tools/task_merge_backlog.go` | 371 | dies-with-mechanism | — |
| `internal/tools/task_merge_backlog_dispatch_guard.go` | 433 | contains-harvest | `mergeBacklogGhostSweepPending`,`MergeBacklogGhostSweepSummary` |
| `internal/tools/task_merge_backlog_terminal_guard.go` | 621 | contains-harvest | `mergeBacklogFamilyBranch`,`mergeBacklogGuardRepo`,`plural`,`mergeBacklogTerminalRefusalOn`,`mergeBacklogTerminalRecord` |
| `internal/tools/task_merge_executor.go` | 691 | contains-harvest | `mergeExecMainRepo`,`mergeExecGit` |
| `internal/tools/task_merge_refuse_anchor.go` | 127 | dies-with-mechanism | — |
| `internal/tools/task_merged_cleanup.go` | 1138 | contains-harvest | `isLiveTaskStatus`,`MergedCleanupStep`,`SetFinishCleanupSweepOwnerRemover`,`mergedCleanupWorktrees` |
| `internal/tools/task_merged_cleanup_orphan_test.go` | 199 | dies-with-mechanism | — |
| `internal/tools/task_merged_cleanup_stale_registration_test.go` | 83 | dies-with-mechanism | — |
| `internal/tools/task_merged_cleanup_test.go` | 704 | dies-with-mechanism | — |

Totals: 25993 non-test LOC + 8091 test LOC = 34084 LOC; 19 rows are `contains-harvest`, 2 are `owns-external-face`, the rest die outright.

The disposition of a file was **derived**, not chosen: `contains-harvest` means
at least one symbol it defines has a code reference in a non-test file outside
this 73-file set (§2 measures that); `dies-with-mechanism` means none does. Two
rows are `owns-external-face` by reading: `cmd/agent/cli_tasks_merger_help.go`
(the merge help atom — a *help page* the `gopi task` dispatcher prints) and
`internal/tools/task_finish_premerge.go` (the branch's own verify gate that
finish-cleanup calls at the delivery point — Rule 5's check, not the merge
verb's). Both are explained in §4.4.

One row is a **fixture**, not production code:
`internal/standards/testdata/config-doc-historical-anchor/internal/tools/merge_anchor_gate.go`
(1,004 LOC). It is a copy planted under `testdata/` so the config-anchor census
has a stable file to point at, and it dies with the mechanism it mirrors.

## 2. The measurement method, and why the naive count is wrong

Rule 43 step 2 says `git grep -c "<Symbol>" <ref> -- '*.go'`, and its step 3 says
**read the hit line**. Doing step 3 is not optional here: the bare count is
wrong three different ways in this family, all measured.

**(a) Prefixes.** `plural` is defined at
`internal/tools/task_merge_backlog_terminal_guard.go:443`. `git grep -c -w
plural HEAD -- '*.go'` is 53 whole-word hits, and **51 of them are the English
word in a comment or a `t.Errorf` message** — "a singular/plural tail", "the
plural spelling". Only two are the function. A count that stopped at the number
would have declared 31 external references and kept a file alive for nothing.

**(b) Same spelling, different symbol, different package.** Rule 43's outer
question is *visibility*: Go lets an **unexported** package-level name be
referenced only from its own package directory, so an outside hit in a
different directory is a **different declaration that happens to share a
spelling**. Measured instances:

| name | owner (in the merge set) | outside hits | what the outside hits actually are |
|---|---|---|---|
| `pathWithin` | `cmd/agent/merge_run.go:2485` | 12 | `internal/tools/tasks.go:5711` defines its own `pathWithin`; `internal/testutil/scratchroot.go:875` is a third. Three packages, one spelling, no reference between them. |
| `shortTip` | `internal/tools/merger_graceful.go:122` | 2 | BOTH outside lines are inside `internal/tools/rework_landing_probe.go:430`'s signature — a **parameter name**, not a call. Its only caller is `merger_graceful.go:104`, inside the set, so this symbol is NOT a harvest (§5.4). |
| `plural` | see (a) | 53 | 51 English-word uses in `internal/lineedit/` and elsewhere. |

So the classifier used here applies the visibility rule mechanically: an
**unexported** name is counted only where the referring file shares its package
directory; an **exported** name is counted everywhere. That dropped **75**
cross-package homonym hits, and the per-symbol verdict for every symbol is in a
machine dump, not in a human's recollection:

```
definitions in INSIDE non-test files: 630
  with an OUTSIDE reference (a face to keep): 137
  internal-only (dies with the mechanism):    493
  cross-package homonym hits discarded:       75
```

630 definitions, **493 die with their file**, 137 have a face that must be
answered for. The 137 is where §3 and §4 come from.

**A method is classified by its RECEIVER TYPE, not by its own name.** A method
name (`RestoreMaster`, `InventoryBlock`) is a field name any other type may
reuse; the receiver is what a call site names. The 7 methods in this family were
classified by `FailedMergeRevert`, `RepoSweepPreview`, `mergesetGateResult`.

## 3. The externally-referenced symbols

88 symbols carry at least one **code** reference (a non-comment line) from a
non-test file outside the merge-named set — but "outside the set" is not the same
as "survives", so each row also carries a VERDICT. The test is Rule 43's own: a
reference from a file that exists only to ARM the mechanism
(`cmd/agent/cli_tasks.go`, `cmd/agent/main.go`) is the mechanism's hook, not a
client. Splitting on that gives **67 HARVEST** symbols and **21 hook-only**
ones, and the split is in the table because it IS the difference between
"delete" and "move first". The list, with the owning file and every production
call site, is generated from the measurement:
| # | symbol | kind | owning file | verdict | outside refs | production call sites (outside the merge set) |
|---:|---|---|---|---|---:|---|
| 1 | `AuditMergeBacklog` | func | `internal/tools/task_merge_backlog.go` | hook-only → dies | 3 | `cmd/agent/cli_tasks.go:7190` |
| 2 | `CaptureFailedMergeRevert` | func | `internal/tools/merge_revert.go` | hook-only → dies | 3 | `cmd/agent/cli_tasks.go:6536` |
| 3 | `mergesetGateResult.Failed` | method | `internal/tools/rebuild_vet_mergeset.go` | **HARVEST** | 2 | `internal/tools/rebuild.go:98` |
| 4 | `FailedMergeRevert` | type | `internal/tools/merge_revert.go` | **HARVEST** | 30 | `cmd/agent/cli_tasks.go:6617`<br>`cmd/agent/cli_tasks.go:6619`<br>`cmd/agent/cli_tasks_rebuild.go:83`<br>`cmd/agent/cli_tasks_rebuild.go:258`<br>`internal/tools/rebuild_vet_revert.go:145`<br>`internal/tools/rebuild_vet_revert.go:347`<br>`internal/tools/rebuild_vet_revert.go:443` |
| 5 | `InodeOfFileInfo` | func | `internal/tools/merger_status.go` | hook-only → dies | 4 | `cmd/agent/cli_tasks.go:5400` |
| 6 | `RepoSweepPreview.InventoryBlock` | method | `internal/tools/merger_status_inventory_block.go` | **HARVEST** | 16 | `cmd/agent/cli_tasks.go:6580`<br>`internal/tools/repo_sweep.go:769`<br>`internal/tools/repo_sweep.go:831`<br>`internal/tools/repo_sweep.go:878`<br>`internal/tools/repo_sweep.go:1026`<br>`internal/tools/repo_sweep.go:1027`<br>`internal/tools/repo_sweep.go:1107`<br>`internal/tools/repo_sweep.go:1114` |
| 7 | `KernelFlockRecords` | func | `internal/tools/merger_status.go` | hook-only → dies | 4 | `cmd/agent/cli_tasks.go:5404` |
| 8 | `LockEvidences` | func | `internal/tools/merger_status.go` | **HARVEST** | 9 | `cmd/agent/serve_lock_self_hold.go:91` |
| 9 | `LockHolderNote` | func | `internal/tools/merger_status.go` | **HARVEST** | 7 | `cmd/agent/cli_tasks.go:5636`<br>`cmd/agent/cli_worker_control.go:2193`<br>`cmd/agent/cli_worker_control.go:3219` |
| 10 | `LockProbe` | func | `internal/tools/merger_status.go` | **HARVEST** | 3 | `cmd/agent/cli_worker_control.go:3214` |
| 11 | `MergeBacklogBranch` | type | `internal/tools/task_merge_backlog.go` | hook-only → dies | 2 | `cmd/agent/cli_tasks.go:7175` |
| 12 | `MergeBacklogGhostSweepSummary` | func | `internal/tools/task_merge_backlog_dispatch_guard.go` | **HARVEST** | 1 | `internal/tools/tasks.go:7909` |
| 13 | `MergeBacklogOptions` | type | `internal/tools/task_merge_backlog.go` | hook-only → dies | 1 | `cmd/agent/cli_tasks.go:7190` |
| 14 | `MergeExecLanded` | func | `internal/tools/task_merge_executor.go` | hook-only → dies | 6 | `cmd/agent/cli_tasks.go:7231` |
| 15 | `MergeHeadRev` | func | `internal/tools/rebuild_vet_mergeset.go` | **HARVEST** | 1 | `internal/tools/rebuild.go:171` |
| 16 | `MergedCleanupOptions` | type | `internal/tools/task_merged_cleanup.go` | hook-only → dies | 8 | `cmd/agent/cli_tasks.go:4829`<br>`cmd/agent/cli_tasks.go:5149` |
| 17 | `MergedCleanupReport` | type | `internal/tools/task_merged_cleanup.go` | hook-only → dies | 1 | `cmd/agent/cli_tasks.go:4898` |
| 18 | `MergedCleanupScanStatus` | func | `internal/tools/task_merged_cleanup.go` | hook-only → dies | 1 | `cmd/agent/cli_tasks.go:4902` |
| 19 | `MergedCleanupStep` | type | `internal/tools/task_merged_cleanup.go` | **HARVEST** | 2 | `internal/tools/repo_sweep_report.go:310`<br>`internal/tools/repo_sweep_report.go:312` |
| 20 | `MergedCleanupSweep` | func | `internal/tools/task_merged_cleanup.go` | hook-only → dies | 19 | `cmd/agent/cli_tasks.go:4829`<br>`cmd/agent/cli_tasks.go:5149` |
| 21 | `MergerGracefulDir` | func | `internal/tools/merger_graceful.go` | **HARVEST** | 3 | `internal/tools/rebuild_pending.go:113` |
| 22 | `OrphanLockHolder` | func | `internal/tools/merger_status.go` | **HARVEST** | 12 | `cmd/agent/cli_tasks.go:5239`<br>`cmd/agent/cli_tasks.go:5365`<br>`cmd/agent/cli_tasks.go:5504`<br>`cmd/agent/cli_tasks.go:5565`<br>`cmd/agent/cli_worker_control.go:2118`<br>`cmd/agent/cli_worker_daemon.go:791` |
| 23 | `ReadStampedPID` | func | `internal/tools/merger_status.go` | **HARVEST** | 3 | `cmd/agent/cli_tasks.go:5628`<br>`cmd/agent/cli_tasks.go:5838`<br>`cmd/agent/cli_worker_control.go:2188` |
| 24 | `RebuildAfterMergeMergeset` | func | `internal/tools/rebuild_vet_mergeset.go` | **HARVEST** | 3 | `internal/tools/rebuild.go:171` |
| 25 | `mergesetGateResult.Ref` | method | `internal/tools/rebuild_vet_mergeset.go` | **HARVEST** | 2 | `internal/tools/rebuild.go:98` |
| 26 | `FailedMergeRevert.RestoreMaster` | method | `internal/tools/merge_revert.go` | **HARVEST** | 30 | `cmd/agent/cli_tasks.go:6617`<br>`cmd/agent/cli_tasks.go:6619`<br>`cmd/agent/cli_tasks_rebuild.go:83`<br>`cmd/agent/cli_tasks_rebuild.go:258`<br>`internal/tools/rebuild_vet_revert.go:145`<br>`internal/tools/rebuild_vet_revert.go:347`<br>`internal/tools/rebuild_vet_revert.go:443` |
| 27 | `FailedMergeRevert.Retriable` | method | `internal/tools/merge_revert.go` | **HARVEST** | 30 | `cmd/agent/cli_tasks.go:6617`<br>`cmd/agent/cli_tasks.go:6619`<br>`cmd/agent/cli_tasks_rebuild.go:83`<br>`cmd/agent/cli_tasks_rebuild.go:258`<br>`internal/tools/rebuild_vet_revert.go:145`<br>`internal/tools/rebuild_vet_revert.go:347`<br>`internal/tools/rebuild_vet_revert.go:443` |
| 28 | `RevertNotify` | func | `internal/tools/merge_revert.go` | **HARVEST** | 3 | `cmd/agent/cli_tasks_rebuild.go:357`<br>`internal/tools/rebuild_vet_revert.go:464` |
| 29 | `SetFinishCleanupSweepOwnerRemover` | func | `internal/tools/task_merged_cleanup.go` | **HARVEST** | 3 | `cmd/agent/task_lifecycle_tick.go:227` |
| 30 | `mergesetGateResult.Summary` | method | `internal/tools/rebuild_vet_mergeset.go` | **HARVEST** | 2 | `internal/tools/rebuild.go:98` |
| 31 | `TaskMergeBacklogExecute` | func | `internal/tools/task_merge_executor.go` | hook-only → dies | 3 | `cmd/agent/cli_tasks.go:7219` |
| 32 | `TaskMergeBranchOutcome` | type | `internal/tools/task_merge.go` | **HARVEST** | 15 | `internal/tools/repo_sweep.go:374`<br>`internal/tools/repo_sweep.go:375`<br>`internal/tools/repo_sweep.go:386`<br>`internal/tools/repo_sweep.go:477`<br>`internal/tools/repo_sweep.go:1400`<br>`internal/tools/repo_sweep.go:1407`<br>`internal/tools/repo_sweep.go:1432`<br>`internal/tools/repo_sweep.go:1493`<br>`internal/tools/repo_sweep_ref_guard.go:263`<br>`internal/tools/repo_sweep_ref_guard.go:267`<br>`internal/tools/repo_sweep_report.go:233`<br>`internal/tools/repo_sweep_report.go:241`<br>`internal/tools/repo_sweep_report.go:248`<br>`internal/tools/repo_sweep_report.go:268` |
| 33 | `branchDisposition` | type | `internal/tools/merger_status_disposition.go` | **HARVEST** | 1 | `internal/tools/repo_sweep.go:1107` |
| 34 | `branchDispositions` | func | `internal/tools/merger_status_disposition.go` | **HARVEST** | 1 | `internal/tools/repo_sweep.go:1108` |
| 35 | `canonicalMergeFlagSpelling` | func | `cmd/agent/cli_tasks_merger_help.go` | hook-only → dies | 1 | `cmd/agent/cli_tasks.go:6855` |
| 36 | `errTextOr` | func | `internal/tools/merge_union_gate.go` | **HARVEST** | 1 | `internal/tools/manifest_union_gate.go:238` |
| 37 | `finishCleanupBranchCheckGroundOnBranch` | func | `internal/tools/task_finish_premerge.go` | **HARVEST** | 2 | `internal/tools/task_finish_cleanup.go:1565` |
| 38 | `finishCleanupBranchCheckReason` | func | `internal/tools/task_finish_premerge.go` | **HARVEST** | 5 | `internal/tools/task_finish_cleanup.go:2454`<br>`internal/tools/task_finish_cleanup.go:5332` |
| 39 | `finishCleanupBranchCheckVerify` | func | `internal/tools/task_finish_premerge.go` | **HARVEST** | 10 | `internal/tools/task_finish_cleanup.go:2451`<br>`internal/tools/task_finish_cleanup.go:5315` |
| 40 | `finishCleanupPreMergeRefusedReason` | func | `internal/tools/task_finish_premerge.go` | **HARVEST** | 3 | `internal/tools/task_finish_cleanup.go:2512`<br>`internal/tools/task_finish_cleanup.go:2529`<br>`internal/tools/task_finish_cleanup.go:2551` |
| 41 | `finishCleanupPreMergeRev` | func | `internal/tools/task_finish_premerge.go` | **HARVEST** | 2 | `internal/tools/task_finish_cleanup.go:3394`<br>`internal/tools/task_ground_delivery_reading.go:290` |
| 42 | `inheritedLockHandleForExemptFace` | func | `cmd/agent/merger_work_lock.go` | **HARVEST** | 4 | `cmd/agent/cli_tasks.go:5219`<br>`cmd/agent/serve_lock_self_hold.go:126` |
| 43 | `RepoSweepPreview.inventoryProtectedNote` | method | `internal/tools/merger_status_inventory_block.go` | **HARVEST** | 16 | `cmd/agent/cli_tasks.go:6580`<br>`internal/tools/repo_sweep.go:769`<br>`internal/tools/repo_sweep.go:831`<br>`internal/tools/repo_sweep.go:878`<br>`internal/tools/repo_sweep.go:1026`<br>`internal/tools/repo_sweep.go:1027`<br>`internal/tools/repo_sweep.go:1107`<br>`internal/tools/repo_sweep.go:1114` |
| 44 | `isGitExit1` | func | `internal/tools/task_merge.go` | **HARVEST** | 3 | `internal/tools/repo_sweep_report.go:159` |
| 45 | `isLiveTaskStatus` | func | `internal/tools/task_merged_cleanup.go` | **HARVEST** | 9 | `internal/tools/rework_handoff.go:148`<br>`internal/tools/rework_reparent_children.go:257`<br>`internal/tools/task_finish_cleanup.go:6102`<br>`internal/tools/task_for_branch.go:573`<br>`internal/tools/task_ground_reconcile.go:240`<br>`internal/tools/task_rework_successor.go:176` |
| 46 | `isMergeSubcommand` | func | `cmd/agent/merge_dispatch.go` | hook-only → dies | 2 | `cmd/agent/main.go:704` |
| 47 | `isMergerSubcommand` | func | `cmd/agent/merger_retired.go` | hook-only → dies | 2 | `cmd/agent/main.go:704` |
| 48 | `mergeBacklogFamilyBranch` | func | `internal/tools/task_merge_backlog_terminal_guard.go` | **HARVEST** | 5 | `internal/tools/archive_row_reconcile.go:245`<br>`internal/tools/archived_branch_retirement.go:231` |
| 49 | `mergeBacklogGhostSweepPending` | func | `internal/tools/task_merge_backlog_dispatch_guard.go` | **HARVEST** | 1 | `internal/tools/tasks.go:7909` |
| 50 | `mergeBacklogGuardRepo` | func | `internal/tools/task_merge_backlog_terminal_guard.go` | **HARVEST** | 1 | `internal/tools/task_transfer.go:475` |
| 51 | `mergeBacklogTerminalRecord` | func | `internal/tools/task_merge_backlog_terminal_guard.go` | **HARVEST** | 1 | `internal/tools/memory_sync_task.go:1544` |
| 52 | `mergeBacklogTerminalRefusalOn` | func | `internal/tools/task_merge_backlog_terminal_guard.go` | **HARVEST** | 1 | `internal/tools/tasks.go:6313` |
| 53 | `mergeBuildGate` | func | `internal/tools/merge_build_gate.go` | **HARVEST** | 1 | `internal/tools/repo_sweep.go:1435` |
| 54 | `mergeBuildGateBranchAlreadyInTarget` | func | `internal/tools/merge_build_gate.go` | **HARVEST** | 1 | `internal/tools/manifest_union_gate.go:227` |
| 55 | `mergeBuildGateDiskExhausted` | func | `internal/tools/merge_build_gate.go` | **HARVEST** | 1 | `internal/tools/rebuild_vet_gate.go:558` |
| 56 | `mergeBuildGateDiskExhaustedErr` | func | `internal/tools/merge_build_gate.go` | **HARVEST** | 1 | `internal/tools/rebuild_vet_gate.go:559` |
| 57 | `mergeBuildGateEnv` | func | `internal/tools/merge_build_gate.go` | **HARVEST** | 1 | `internal/tools/rebuild_vet_gate.go:516` |
| 58 | `mergeBuildGateGoCache` | func | `internal/tools/merge_build_gate_gocache.go` | **HARVEST** | 1 | `internal/tools/rebuild_vet_gate.go:436` |
| 59 | `mergeBuildGateReapRegistrations` | func | `internal/tools/merge_build_gate.go` | **HARVEST** | 1 | `internal/tools/rebuild_vet_revert.go:242` |
| 60 | `mergeBuildGateScratch` | func | `internal/tools/merge_build_gate.go` | **HARVEST** | 1 | `internal/tools/rebuild_vet_revert.go:235` |
| 61 | `mergeBuildGateSharedModCache` | func | `internal/tools/merge_build_gate_modcache.go` | **HARVEST** | 1 | `internal/tools/rebuild_vet_gate.go:440` |
| 62 | `mergeBuildGateTail` | func | `internal/tools/merge_build_gate.go` | **HARVEST** | 7 | `internal/tools/rebuild_vet_revert.go:253`<br>`internal/tools/task_briefing_master_reading.go:227`<br>`internal/tools/task_briefing_master_reading.go:252`<br>`internal/tools/task_briefing_master_reading.go:346`<br>`internal/tools/task_ground_baseline.go:110`<br>`internal/tools/task_ground_cutpoint.go:152`<br>`internal/tools/task_ground_cutpoint.go:156` |
| 63 | `mergeBuildGateUnresolvedModules` | func | `internal/tools/merge_build_gate.go` | **HARVEST** | 1 | `internal/tools/rebuild_vet_gate.go:549` |
| 64 | `mergeBuildGateWorktreeDirName` | func | `internal/tools/merge_build_gate.go` | **HARVEST** | 1 | `internal/tools/rebuild_vet_revert.go:243` |
| 65 | `mergeCanonicalFlagPointer` | func | `cmd/agent/cli_tasks_merger_help.go` | hook-only → dies | 1 | `cmd/agent/cli_tasks.go:6859` |
| 66 | `mergeCanonicalOnlyFlag` | func | `cmd/agent/cli_tasks_merger_help.go` | hook-only → dies | 1 | `cmd/agent/cli_tasks.go:6856` |
| 67 | `mergeContractGate` | func | `internal/tools/merge_contract_gate.go` | **HARVEST** | 1 | `internal/tools/repo_sweep.go:1431` |
| 68 | `mergeExecGit` | func | `internal/tools/task_merge_executor.go` | **HARVEST** | 3 | `internal/tools/task_submit_clone_ground.go:163` |
| 69 | `mergeExecMainRepo` | func | `internal/tools/task_merge_executor.go` | **HARVEST** | 13 | `internal/tools/archive_reason.go:496`<br>`internal/tools/task_finish_cleanup.go:2752`<br>`internal/tools/tasks.go:7098` |
| 70 | `mergeRevertGit` | func | `internal/tools/merge_revert.go` | **HARVEST** | 11 | `internal/tools/verify_gate.go:315`<br>`internal/tools/verify_gate.go:331`<br>`internal/tools/verify_gate.go:339`<br>`internal/tools/verify_gate.go:356`<br>`internal/tools/verify_gate.go:391`<br>`internal/tools/verify_gate.go:397`<br>`internal/tools/verify_gate.go:403`<br>`internal/tools/verify_gate.go:406`<br>`internal/tools/verify_gate.go:430`<br>`internal/tools/verify_gate.go:434`<br>`internal/tools/verify_gate.go:437` |
| 71 | `mergeUnionGate` | func | `internal/tools/merge_union_gate.go` | **HARVEST** | 2 | `internal/tools/repo_sweep.go:1439` |
| 72 | `mergedCleanupWorktrees` | func | `internal/tools/task_merged_cleanup.go` | **HARVEST** | 13 | `internal/tools/archive_ref_reclaim.go:472`<br>`internal/tools/branch_attribution.go:493`<br>`internal/tools/branch_attribution.go:716`<br>`internal/tools/branch_attribution.go:820`<br>`internal/tools/repo_sweep.go:574`<br>`internal/tools/task_holder_resume.go:311`<br>`internal/tools/task_reclaim.go:1699`<br>`internal/tools/task_reclaim.go:1788`<br>`internal/tools/task_reclaim.go:2280`<br>`internal/tools/task_reclaim.go:2487` |
| 73 | `mergerLockHeldByAncestor` | func | `cmd/agent/merger_work_lock.go` | hook-only → dies | 3 | `cmd/agent/cli_tasks.go:5213` |
| 74 | `mergerWorkLock` | func | `cmd/agent/merger_work_lock.go` | **HARVEST** | 4 | `cmd/agent/cli_tasks.go:4948`<br>`cmd/agent/cli_tasks.go:5143`<br>`cmd/agent/cli_tasks.go:7218`<br>`cmd/agent/cli_tasks_adopt.go:146` |
| 75 | `mergesetGateResult` | type | `internal/tools/rebuild_vet_mergeset.go` | **HARVEST** | 2 | `internal/tools/rebuild.go:98` |
| 76 | `plural` | func | `internal/tools/task_merge_backlog_terminal_guard.go` | **HARVEST** | 31 | `internal/tools/delivery_content_landed.go:159`<br>`internal/tools/task_delivery_proof_content.go:893`<br>`internal/tools/task_delivery_proof_content.go:906`<br>`internal/tools/task_delivery_proof_content.go:909`<br>`internal/tools/worktree_sweep_undelivered.go:294` |
| 77 | `repoSweepRouteBuildRefusals` | func | `internal/tools/task_merge.go` | **HARVEST** | 1 | `internal/tools/repo_sweep.go:1364` |
| 78 | `repoSweepRouteContractRefusals` | func | `internal/tools/task_merge.go` | **HARVEST** | 1 | `internal/tools/repo_sweep.go:1363` |
| 79 | `reportRetiredMergeTickKnobs` | func | `cmd/agent/merge_tick_retired.go` | **HARVEST** | 2 | `cmd/agent/tmpdir_inject.go:103` |
| 80 | `resolveConflictRowIdentity` | func | `internal/tools/task_merge.go` | **HARVEST** | 5 | `internal/tools/task_adoption.go:379` |
| 81 | `retireMergerFace` | func | `cmd/agent/merger_retired.go` | hook-only → dies | 7 | `cmd/agent/cli_tasks.go:6814`<br>`cmd/agent/cli_tasks.go:7151` |
| 82 | `reworkSummaryLive` | func | `internal/tools/task_merge.go` | **HARVEST** | 7 | `internal/tools/dirty_holder_salvage.go:176` |
| 83 | `runTaskMerge` | func | `cmd/agent/cli_tasks_merge.go` | hook-only → dies | 2 | `cmd/agent/cli_tasks.go:7153` |
| 84 | `taskMergeExempt` | func | `internal/tools/task_merge.go` | **HARVEST** | 5 | `internal/tools/branch_attribution.go:364`<br>`internal/tools/repo_sweep.go:705` |
| 85 | `taskMergeRefuseManifest` | func | `internal/tools/task_merge.go` | **HARVEST** | 1 | `internal/tools/repo_sweep.go:1444` |
| 86 | `taskMergeRefuseUncompilable` | func | `internal/tools/task_merge.go` | **HARVEST** | 3 | `internal/tools/repo_sweep.go:1436`<br>`internal/tools/repo_sweep.go:1440` |
| 87 | `taskMergerAtomUsage` | func | `cmd/agent/cli_tasks_merger_help.go` | hook-only → dies | 3 | `cmd/agent/cli_tasks.go:6765` |
| 88 | `verdictOperatorWords` | func | `internal/tools/merger_status_disposition.go` | **HARVEST** | 2 | `internal/tools/branch_attribution.go:915`<br>`internal/tools/repo_sweep.go:981` |

**67 HARVEST, 21 hook-only.** A hook-only symbol is one whose every
out-of-set reference sits in `cmd/agent/cli_tasks.go` or `cmd/agent/main.go` —
the two dispatcher files that ARM the mechanism. They belong to the mechanism,
so they are deleted with it (Rule 43: the file that only wires the mechanism is
not a client of it). The HARVEST set is the rows marked **HARVEST**.

Three groups matter for the removal, and they are separated by **who calls them
and whether the caller itself survives**:

**(i) Called by code that survives — these must be HARVESTED before the file
dies.** They are the 67 rows marked **HARVEST** above. The rule is mechanistic (Rule 43 step 3): a
symbol whose production reference sits in a file that is NOT in the merge-named
set is a general tool that happens to live in a merge-named file.

**(ii) Called only from the merge set — these die with it.** `TaskMerge`,
`TaskMergeBacklogExecute`, `MergeExecLanded`, `mergedCleanupWorktrees`,
`mergeUnionGate`, `mergerWorkLock`, `runTaskMerge`, `parseMergeArgs`,
`isMergeSubcommand`, `retireMergerFace`, `reportRetiredMergeTickKnobs`,
`canonicalMergeFlagSpelling`. That list is illustrative, not exhaustive: §3's
88-row table is the complete, machine-generated set. The dispatcher arms in
`cmd/agent/cli_tasks.go`
and `cmd/agent/main.go` are the "hook" of Rule 43's table: they belong to the
mechanism, so they are deleted in the same commit, not harvested.

**(iii) Named by a comment or a test only — neither a face nor a symbol.** The
measured trap in this family: `mergeBuildGateTail` is referenced 7 times
out-of-set and **all seven are `fmt.Sprintf` argument positions in live code**
— the exact seven, measured: `internal/tools/rebuild_vet_revert.go:253`,
`internal/tools/task_briefing_master_reading.go:227`, `:252`, `:346`,
`internal/tools/task_ground_baseline.go:110`,
`internal/tools/task_ground_cutpoint.go:152`, `:156`. All seven are
argument positions of the form `mergeBuildGateTail(err.Error())` /
`mergeBuildGateTail(detail)` / `mergeBuildGateTail(e.Error())` in live code — a real harvest.
By contrast `taskMergedCleanup` (exact spelling) has **zero** hits in the whole
tree: the live name is `runTaskMergedCleanup` and the similarity is a homonym,
not a reference. `grep taskMergedCleanup` → nothing; `grep taskMergedCleanup
-i` → `runTaskMergedCleanup` only. This is the `deliverableRepo` trap again, and
it is why every verdict here came from a whole-word search plus a read of each
hit line.

## 4. The delivery question — the ONE design decision this inventory owes

The epic removes the **merge verb**. It does **not** remove — and must not
remove — the machinery that makes a delivered branch *land*. Those are two
different things, and in this tree they are three different code paths that
merely share a name. Measured, one by one.

### 4.1 What each of the five items is, in the tree as it stands

| verb / chain | dispatcher (measured) | engine | what it does |
|---|---|---|---|
| `gopi task merge` | `cmd/agent/cli_tasks.go:7152` → `:7153 runTaskMerge(opts)` | `tools.TaskMerge` (`internal/tools/task_merge.go:444`) | merges named branches into master, prints one line per branch, then `runTaskMergeRebuild` |
| `gopi task merge-execute` | `:7138` → `:7139 runTaskMergeExecute(opts)` | `tools.TaskMergeBacklogExecute` (`task_merge_executor.go:160`) | lands ONE merge-backlog row's branch, then `mergerCloseLoopAfterLanding(rep.Repo)` when `tools.MergeExecLanded(rep)` |
| `gopi task merged-cleanup` | `:7116` → `:7117 runTaskMergedCleanup()` | `tools.MergedCleanupSweep` (`task_merged_cleanup.go:405`), `ReportOnly: true` | reaps merged branches/worktrees, report-only |
| `gopi task audit-merges` | `:7131` → `:7132 runTaskAuditMerges(opts)` | `tools.AuditMergeBacklog` (`task_merge_backlog.go`) | audits the merge backlog and can submit rows |
| `runTaskMerge` / `TaskMerge` | the first row | the first row | the first row |

And the closeout→merge chain, which is not a verb at all:

- **`mergerPassOnce`** (`cmd/agent/cli_tasks.go:6437`) is the resident merger
  pass. `grep -rn 'mergerPassOnce(' --include=*.go .` returns **one** hit — its
  own declaration. Nothing calls it. Its former caller `runTaskMerger` is gone
  (deleted 2026-09-19, `devdoc/merger-retired.md`; the file
  `cmd/agent/cli_tasks_worker.go:8` says so in prose).
- **`mergerSweepRepo`** is called only from inside `mergerPassOnce`
  (`cli_tasks.go:6503`), so it is dead with the same reachability.
- **`serveMergedCleanupSweep`** (`cli_tasks.go:4811`) has **no production
  caller** either: `grep -rn 'serveMergedCleanupSweep()' --include=*.go .`
  returns `func` (its declaration) plus five hits in
  `cli_tasks_merged_cleanup_test.go`. Its production caller was deleted on
  2026-09-04 (`d11af9a6`), and the function's own comment at `:4832` records
  that fact.
- **`taskMergedCleanup`** as a Go identifier does not exist. The trio the
  briefing names maps to `runTaskMergedCleanup`, `tools.MergedCleanupSweep`
  and `tools.MergedCleanupOptions`.

### 4.2 The behaviour that MUST survive, and where it actually lives

The delivery of a finished branch is **not** `TaskMerge`. It is the finish
chain, and it lives in files whose names do not contain `merge`:

- `internal/tools/task_finish_cleanup.go` — the chain. It calls
  `finishCleanupBranchCheckVerify(ctx, d.Repo, checkGround, branch)` at
  `:2451` and `:5315` — the branch's own build+test check at the delivery
  point (Rule 5), which is defined in a merge-named file
  (`task_finish_premerge.go:264`) and is therefore a **harvest**, not a delete.
- `internal/tools/task_sync_git_pushback.go` — the push leg
  (`gitPushbackPlan`, `gitPushbackPush`, `gitPushbackFastForwardsOntoMaster`,
  `gitPushbackSharesHistoryWithMaster`). Not merge-named, not in scope.
- The clone channel's own loop — clean → `pull --rebase` → declared build →
  push `master` (`internal/devworkspace`). Not merge-named, not in scope.

**So the answer to "what happens to the merged-delivery behaviour" is: it stays,
and none of it is reached THROUGH the merge verb.** A run's branch is delivered
by its own close-out; `TaskMerge` was the *manual, out-of-band* face that could
also advance master, and that is exactly the capability being removed.

### 4.3 The decision, item by item

Each row below is decided by the MEASURED verdict in §3, not by the raw outside
count — and that distinction changed three of them. A symbol whose every
out-of-set reference sits in `cmd/agent/cli_tasks.go` or `cmd/agent/main.go` is
**hook-only**, and the hook dies with the mechanism. So the question for each
verb is not "does something reference this engine" but "does a file that
SURVIVES reference it".

| item | verdict | why (measured, and the numbers are §3's) |
|---|---|---|
| `gopi task merge` (`runTaskMerge` → `TaskMerge`) | **DELETE the verb; HARVEST 9 symbols out of `task_merge.go`** | `TaskMerge` itself has one caller (`cmd/agent/cli_tasks_merge.go:28`) and that file is in the set, so the ENGINE dies with the verb. Its file survives only as a SOURCE of the 9 harvests: `TaskMergeBranchOutcome` (14 real refs, `repo_sweep.go:374,375,386,477,1400`, `repo_sweep_report.go`), `taskMergeExempt` (2: `branch_attribution.go:364`, `repo_sweep.go:705`), `isGitExit1` (1: `repo_sweep_report.go:159`), `resolveConflictRowIdentity` (1: `task_adoption.go:379`), `reworkSummaryLive` (1: `dirty_holder_salvage.go:176`), `repoSweepRouteBuildRefusals`, `repoSweepRouteContractRefusals`, `taskMergeRefuseManifest`, `taskMergeRefuseUncompilable` (all from `repo_sweep.go`). |
| `gopi task merge-execute` (`runTaskMergeExecute` → `TaskMergeBacklogExecute`) | **DELETE the verb AND its engine; HARVEST the backlog ROW TYPE and its guards** | `TaskMergeBacklogExecute` and `MergeExecLanded` are **hook-only** — the one executor face is the mechanism, so it dies. What survives is the ledger vocabulary the rows are recorded in: `MergeBacklogGhostSweepSummary` (`internal/tools/tasks.go:7909`), `mergeBacklogGhostSweepPending` (`tasks.go:7909`), `mergeBacklogFamilyBranch` (`archive_row_reconcile.go:245`, `archived_branch_retirement.go:231`), `mergeBacklogGuardRepo` (`task_transfer.go:475`), `mergeBacklogTerminalRecord` (`memory_sync_task.go:1544`), `mergeBacklogTerminalRefusalOn` (`tasks.go:6313`), `plural` (`delivery_content_landed.go:159`, `task_delivery_proof_content.go:893,906,909`, `worktree_sweep_undelivered.go:294`). |
| `gopi task merged-cleanup` (`runTaskMergedCleanup` → `MergedCleanupSweep`) | **DELETE the verb AND the sweep engine; HARVEST the branch/worktree READERS it shares** | `MergedCleanupSweep`, `MergedCleanupOptions`, `MergedCleanupReport`, `MergedCleanupScanStatus` are **hook-only** (all 19 references are `cli_tasks.go:4829` and `:5149`, both of them this mechanism's own faces) — so the sweep itself is part of the capability. What must be harvested is the part other code already uses for its own reasons: `mergedCleanupWorktrees` (10 real refs — `archive_ref_reclaim.go:472`, `branch_attribution.go:493,716,820`, `repo_sweep.go:574`) and `isLiveTaskStatus` (6 — `rework_handoff.go:148`, `rework_reparent_children.go:257`, `task_finish_cleanup.go:6102`, `task_for_branch.go:573`, `task_ground_reconcile.go:240`), plus `MergedCleanupStep` (`repo_sweep_report.go:310,312`) and `SetFinishCleanupSweepOwnerRemover`. **This is the row where the raw count lied most loudly**: 19 looked like "a live shared engine"; it is 19 references from the mechanism to itself. |
| `gopi task audit-merges` (`runTaskAuditMerges` → `AuditMergeBacklog`) | **DELETE the verb AND `AuditMergeBacklog`** | `AuditMergeBacklog`, `MergeBacklogBranch` and `MergeBacklogOptions` are all **hook-only** (one site each, `cli_tasks.go:7175,7190`), so the audit engine is the mechanism. The backlog *data* vocabulary survives — but it survives through the guard symbols listed in the `merge-execute` row, not through this function. |
| closeout→merge chain (`runTaskMerge`, `taskMergedCleanup`, `TaskMerge`) | **`runTaskMerge`: DELETE (1 hook site, `cli_tasks.go:7153`). `TaskMerge`: DELETE with its verb (§row 1). `taskMergedCleanup`: no such symbol** | The chain this briefing names is a naming artifact, not a code path: `taskMergedCleanup` has zero hits at any case, its live name is `runTaskMergedCleanup`, and that function is the `merged-cleanup` verb above. The **delivery** behaviour is not in this table at all — it lives in `task_finish_cleanup.go` / `task_sync_git_pushback.go` / the clone channel (§4.2) and does not go through any merge verb. |

**The rule that decides every row above**, stated once: *the VERB dies; an
ENGINE is harvested **only** when a file that SURVIVES references it; a
reference from the dispatcher hook is the mechanism naming itself.* The measured
split is 67 HARVEST / 21 hook-only (§3), and the three rows above that changed
after the split — `MergedCleanupSweep`, `AuditMergeBacklog`, `TaskMergeBacklogExecute`
— are the reason the split exists.

### 4.4 Two files whose survival is a READING, not a grep

- **`internal/tools/task_finish_premerge.go`** — exactly 5 symbols with an
  outside code reference, and no others: `finishCleanupBranchCheckGroundOnBranch`,
  `finishCleanupBranchCheckReason`, `finishCleanupBranchCheckVerify`,
  `finishCleanupPreMergeRefusedReason`, `finishCleanupPreMergeRev` (the full
  per-symbol distribution is §3's table and `symbol-classification.json`). Its
  name says "merge" because the check used to be a trial merge; the code no
  longer merges. It is the branch's Rule 5
  verify at the delivery point and `internal/tools/task_finish_cleanup.go:2451`
  and `:5315` call it. **It stays** — renamed or not is a later item's decision;
  as an inventory finding it is: this file must NOT be swept into the removal.
- **`cmd/agent/cli_tasks_merger_help.go`** (556 LOC, 15 definitions, 4 with
  outside references: `mergeCanonicalFlagPointer`, `mergeCanonicalOnlyFlag`,
  `canonicalMergeFlagSpelling`, `taskMergerAtomUsage`). It is the *help atom*
  the `gopi task` dispatcher prints for the git-infrastructure spellings
  (`cli_tasks.go:243,370,400,501,523,535`). When the verbs die, the pages that
  describe them die **with** them — but the atom is also what the retired-verb
  advice points at, so the ordering matters: delete the pages, then delete the
  atom, in one commit, and keep the refusal sentences.

## 5. Four measured traps, recorded so item 2 does not re-derive them

1. **`taskMergedCleanup` does not exist.** The live names are
   `runTaskMergedCleanup` and `tools.MergedCleanupSweep`. A briefing that names
   the former must not be read as naming a symbol.
2. **`plural` (53 hits) is 51 English words.** A whole-word grep still finds
   prose; the function has 2 references and both are in its own package.
3. **`pathWithin` exists three times** (`cmd/agent/merge_run.go:2485`,
   `internal/tools/tasks.go:5711`, `internal/testutil/scratchroot.go:875`) and
   the three do not reference each other. Deleting the merge-named copy changes
   nothing in the other two.
4. **`shortTip` looked like a harvest and is not one.** Reading its two
   out-of-set lines showed BOTH sit inside
   `func reworkLandingAncestryProof(ctx context.Context, repo, landed, shortTip string) string`
   (`internal/tools/rework_landing_probe.go:430`) — the spelling is a
   **parameter name**, not a call. The symbol has exactly one caller
   (`merger_graceful.go:104`, inside the set), so it dies with its file. It was
   IN the 88-row harvest table until that line was read; the withdrawal is why
   the table says 88 and not 89. **The grep count was right and the conclusion
   was wrong until the line was read** — this is Rule 43 step 3 doing its job
   on the person doing step 3.

## 6. Reachability finding — report it, do not act on it

Measured, and **out of scope for this epic**: three merge-family functions in
`cmd/agent/` are already unreachable from production code —

| symbol | file:line | production callers |
|---|---|---|
| `mergerPassOnce` | `cmd/agent/cli_tasks.go:6437` | none (only its own declaration; former caller `runTaskMerger` retired 2026-09-19) |
| `mergerSweepRepo` | `cmd/agent/cli_tasks.go:6529` | only `mergerPassOnce` (`cli_tasks.go:6503` calls it), itself unreachable |
| `serveMergedCleanupSweep` | `cmd/agent/cli_tasks.go:4811` | none (only its own declaration + 5 test hits; caller deleted 2026-09-04 in `d11af9a6`) |

This changes the SHAPE of item 2's job: these three are not "delete a live
capability", they are "delete code nothing calls". It does not change the
inventory's verdicts, and this document does not remove them. Filed as an
observation (Rule 41), with the greps above as the evidence.

## 7. What item 2 must do, in order

1. **Harvest first, delete second, in ONE commit** (Rule 43). The 67 **HARVEST** symbols in
   §3 are the harvest set; `go build ./...` is the check that proves nothing was
   missed, and it must be run in the removal commit.
2. **The harvest destinations by package**: `internal/tools` symbols stay in
   `internal/tools` (new, non-merge-named files — e.g. `branch_liveness.go` for
   `isLiveTaskStatus`/`mergedCleanupWorktrees`, `git_exit_status.go` for
   `isGitExit1`, `pluralize.go` for `plural`); `cmd/agent` symbols stay in
   `cmd/agent`. No symbol changes package: a package change is a redesign, and
   this epic is a removal.
3. **Never delete `task_finish_premerge.go`'s verify family** (§4.4).
4. **Order the help atom last**, in the same commit as the pages it prints
   (§4.4) — and keep the refusal sentences for the retired spellings.

## Verification of THIS document

```
Build:   go build ./...            → exit 0 (nothing changed except devdoc/)
Status:  git status --porcelain    → devdoc/merge-verb-removal-inventory.md only
Ran:     git ls-files -- '*.go' | grep -Ei 'merg.*\.go$' | sort   → 73 files
         (per-file LOC: git show HEAD:<path> | wc -l → 25,993 + 8,091 = 34,084)
         git grep -c -w <Symbol> HEAD -- '*.go'  → per-symbol dump, 88 symbols
         git grep -c -w plural HEAD -- '*.go'    → 53 hits, 2 are the function
         grep -rn 'mergerPassOnce(' --include=*.go .  → 1 hit (its declaration)
         grep -rn 'serveMergedCleanupSweep()' --include=*.go . → 1 declaration + 5 test hits
         grep -rn 'taskMergedCleanup' --include=*.go . → 0 hits
Result:  630 definitions classified: 493 internal-only, 137 with an outside
         reference, 75 cross-package homonym hits discarded. 88 carry a CODE
         reference from outside the set; split by whether the referring file
         merely ARMS the mechanism (cmd/agent/cli_tasks.go, cmd/agent/main.go):
         67 HARVEST, 21 hook-only.
```

Raw dumps (the exact command + its raw output for every symbol, plus every hit
line that was read) live in this run's session directory:
`/home/user/.gopi/sessions/01a18d61-4fe3-747e-ba1f-109a0caaec20/evidence/` —
`merge-named-files.txt`, `symbol-classification.json`, `harvest-list.txt`,
`harvest-git-grep.txt`, `hit-lines.txt`, `consumers.txt`, `classification.txt`,
`census-table.txt`, `homonym-proof.txt`, `reach-final.txt`, `verb-dispatch.txt`,
`finish-chain.txt`, `verb-merge-top.txt`, `dead-serve-sweep.txt`,
`file-headers.txt`.
