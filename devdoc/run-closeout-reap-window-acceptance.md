<!-- doc-class: historical -->
# Task Report: the run-closeout reap-window arms — measured acceptance, the red lever, and the same-family census

**Task** `01a12e58-8171-79f2-9130-f8a8000d425b` · branch `task/01a12e58817179f29130f8a8000d425b` ·
host **hosta** · 2026-10-12 · HEAD at measurement `1ec289781`

## What this run is, and what it is NOT

A Rule 41 bug row was filed from task `01a12e1e`: three `internal/tools` reap-window
tests failed **deterministically** on this host, each burning 8s on the same premise
assertion — the fixture member's parent stayed `3100` (`systemd --user`, this host's
`PR_SET_CHILD_SUBREAPER` ancestor) instead of reaching init(1), which the fixture waited
for. The evidence is preserved at
`~/.gopi/sessions/01a12e1e-16e6-730d-a566-c7940d5616b0/closeout-repro.out`.

**The defect was already fixed before this run started.** Sibling task `01a12e1a` landed
commit **`951e2b03f`** ("the hole arms' premise is the reaping ancestor, never the literal
init"), and `git merge-base --is-ancestor 951e2b03f HEAD` is **true** in this worktree.
So this run is the **honest verification half** — it does not redo, revert or rewrite
anyone's fix (Rule 23/26). What it adds is the thing a green run alone cannot give:
a measured acceptance, a **red-lever proof that the green is not a weakened assertion**,
and a census of the rest of the same family.

## Completed

1. **Measured acceptance on THIS tree** — `go test -count=1 -run 'TestRunCloseout' ./internal/tools/`
   exits **0** (12/12 PASS, 1.88s), with the mechanism anchor `reaping ancestor 3100`
   present in the verbose log.
2. **Red-lever proof (two mutations, both observed FAIL)** — the green is not a weakening:
   - **A** — predicate reverted to the literal init: **3 FAIL**, each reproducing the
     original bug verbatim (`still parented by 3100 after 8s`).
   - **B** — the reaper measurement forced to `0`: **3 FAIL** immediately (0.07–0.10s),
     naming the reason ("an undecidable premise is never a pass", Rule 15).
3. **Same-family census** — every site whose premise is the literal init / a reparent
   wait, enumerated with a per-site measured verdict (below), including the silent-`t.Skipf`
   form. **10 residual skip sites measured live on this host**, all owned by an
   in-flight sibling row.
4. **The three `finishCleanupInitPid()` consumers each measured against a real orphan**
   (this run's addition to Class D; the earlier text named only the classifier):
   `:3502` `finishCleanupHolderParentSuffix` is a **residual gap at the rendering site**
   (it prints `ppid=3100` instead of the ORPHAN word for a pid the kernel says IS an
   orphan); `:3330` `finishCleanupSelfTree` is **not a defect — and the reason is
   measured, not read**: `members_not_in_chain=[]` proves 3100 enters as an ancestor,
   never through the expansion, and the file's own comment declares that intent;
   `runCloseoutReapInitPid` in `run_closeout_reap.go` is **not this family at all** (a
   lower bound, and `3100 > 1` holds), so its class stays A.
5. **devdoc report** (this file) + worktree clean.

## Not Done (honest)

- **The 6 residual site files are NOT touched.** They were being fixed by sibling row
  `01a12e43-d004-7432-b0e0-b99d0e7c0664`, whose branch
  `task/01a12e43d0047432b0e0b99d0e7c0664` carried commit `0631db7ad` "fix(fixtures): the
  orphan reparent premise is the MECHANISM, not ppid 1" (`git show 0631db7ad --stat`: it
  changes exactly those 6 files + a new `internal/procname` `OrphanReparentTargets`).
  Editing them here would be a two-writers-one-diff collision (Rule 23/29). This run's
  contribution is the **measured verdict + the bottleneck bound**, which the sibling row
  lacked.
- **Ownership correction (measured at close-out, after the paragraph above was written).**
  Row `01a12e43` is now **`completed`** and — the fact that matters — its branch
  **no longer exists**: `git for-each-ref | grep 01a12e43` → **NONE**, `git reflog show
  task/01a12e43…` → `unknown revision`, and its recorded worktree is gone (`gopi task
  show`: "the worktree directory no longer exists", verdict
  `finish-cleanup-removal-refused`). So the fix commit `0631db7ad` is currently reachable
  from exactly **one** ref: `task/01a12e6423c37c10a07cb85aef54924d` (row
  `01a12e64-23c3-7c10-a07c-b85aef54924d`, status `in_progress`, claimed by
  `hosta:3904017`, worktree alive, 1 owner — `gopi task for-branch`), plus
  `refs/remotes/gitPushcheck/master`. That branch still carries it
  (`git grep OrphanReparentTargets task/01a12e64…`: `internal/procname/procname.go`,
  `internal/tools/task_finish_cleanup.go`, `..._holder_orphan.go`, two test files) and is
  **not an ancestor of this HEAD and not an ancestor of master** (both `merge-base
  --is-ancestor` → non-zero; `git grep -c OrphanReparentTargets master` → 0 hits). The
  deliverable is thus **resident, not merged**, and it is in flight — which is exactly why
  this run still does not touch those files: the collision this run refused is against a
  live, claimed branch, not against a finished one.
- **Correction, recorded rather than erased:** this item previously read
  "`make test-budget PKGS='./internal/tools'` did not complete within this session".
  That sentence was written while the run was still in flight; the run has since
  **COMPLETED GREEN** (exit 0, `outcome=pass`, 3299 tests, 475s — `## Verification` and
  `test-budget-tools.out`). Corrected in place, with the superseded sentence kept here so
  a reader can see what changed.
- **SUPERSEDED — see "Post-merge re-measurement … Consequence 4": after the delivery-point
  sync BOTH trees exit 1 with an identical verdict, so the branch-vs-master asymmetry
  described in the bullet below no longer holds. Kept as written.** The bullet as originally
  measured:
- **`gopi tool doccheck` exits 1 on this branch (0 on master) — not this run's defect, and
  not fixed here.** Found on close-out: two frozen anchor-number permits are reported
  `STALE PERMIT` (`devdoc/archive-mint-outside-writer.md`, `devdoc/task-source-provenance.md`),
  because commit `c6e93569e` — reachable from this HEAD (`master..HEAD`) but not from master
  — re-classified the `task-source-provenance.md` row `number-drifted` →
  `recorded-line-blank` after the freeze. Raw output:
  `evidence/doccheck-stale-permit.out` (branch `EXIT=1`; main repo `EXIT=0`). Filed as
  `01a12e8e-fa6b-75aa-aa13-f88547d954d9` (Rule 41) rather than edited here: the table is
  DATA (regenerate, never hand-edit) and the row belongs to another branch's change.
  **This run's own devdoc is consistent — `doccheck: 0 gap(s) between devdoc/ and
  devdoc/DOC-INDEX.tsv`.**
- **`testdata/flakes.tsv` row 74 prose is stale** (observation, not fixed here): it
  describes the fix as "measures the premise … **until init is the parent**", which is
  exactly the predicate commit `951e2b03f` superseded. The row is a point-in-time record
  of task `01a12cf4` owned by the flaketest machinery, so it is left for its owner; a
  future reader should not take its "FIXED:" clause as the current contract.

## Verification

- Build: `go build ./...` — OK (the acceptance runs below compile the touched package).
- Tests: `go test -count=1 -run 'TestRunCloseout' ./internal/tools/` → **12/12 PASS, exit 0**
  - Ran (pre-merge, frozen tree): `go test -count=1 -v -run 'TestRunCloseout' ./internal/tools/`
  - Result: `ok github.com/nick/gopi/internal/tools 1.881s`, `EXIT=0`. Raw lines for the
    three formerly-red arms, verbatim from `evidence/acceptance-green.out` — the premise
    now resolves to the host's real reaper (3100) in **milliseconds**, where the filed bug
    burned 8s per arm and then failed:
- Tests (POST-merge, the tree a reader will actually get — merge `01d4184d5`, tip `318836e08`):
  - Ran: `go test -count=1 -run 'TestRunCloseout' ./internal/tools/` → `ok … 1.675s`, `EXIT=0`
  - Ran: `go test -count=1 -v -run 'TestFinishCleanup|TestSharedSessionHolder|TestHolderRefusal|TestReclaimOwnFinished|TestOrphanTree|TestOrphanCwd|TestCwdHolder|TestLiveRunCwd' ./internal/tools/`
  - Result: `101 PASS / 3 SKIP / 0 FAIL`, `EXIT=0` (`evidence/post-merge-family.out`) — the 3
    remaining SKIPs are declared `heavytest.Require` gates plus one 0.00s probe-child helper,
    i.e. **none is the literal-init premise**; the mechanism anchor `reaper 3100` appears
    **14×** in that log.
  - Ran: `gopi tool doccheck` (branch, post-merge) → `EXIT=1`, and the same command on the
    pristine main repo at `master 88efcaa97` → `EXIT=1` (`diff` of the two logs: one line).
- Tests (FINAL tip of this branch, `8a8275706` — the ref a reader gets):
  - Ran: `go test -count=1 -run 'TestRunCloseout' ./internal/tools/` → `ok … 1.974s`, `EXIT=0`
  - Ran: `git status --porcelain` → prints nothing; `git log --oneline -1` → `8a8275706`
  - Result: `evidence/rerun-closeout-final.out` (host `hosta`, timestamp in the file header).
- Tests (branch-scoped budget): `make test-budget PKGS='./internal/tools'` → `EXIT=0`,
  `outcome=pass`, 3299 run / 3182 pass / 118 skip / 0 fail, 475s (`evidence/test-budget-tools.out`).
- **Red lever (Rule: a green is not evidence the assertion still bites):** two mutations on
  the frozen file, both observed FAIL — see `### A2`/`### A3` below. The mutated file is
  **byte-identical** on the post-merge tree (`git diff --stat 57270b1e3 HEAD -- internal/tools/run_closeout_reap_window_test.go` → empty), so the recorded FAILs are the proof for the tree handed over.
- **Ran** (Rule 31 — the surface this run touched is a DOCUMENT; the honest statement is that
  there is no executable surface to invoke): the deliverable is `devdoc/run-closeout-reap-window-acceptance.md`,
  and what was executed *for* it is the four commands above plus the two probes. No CLI verb
  or code path was added or changed by this run — measured: `git diff --name-only master...HEAD`
  is `devdoc/DOC-INDEX.tsv` + the report file, nothing else.


```
=== RUN   TestRunCloseoutReapWatchesAPidItCannotMeasureYet
    run_closeout_reap_window_test.go:103: the hole arm: pid 1080240 is no longer the fixture's — the kernel handed it to this host's reaping ancestor 3100 after 8.064137ms
--- PASS: TestRunCloseoutReapWatchesAPidItCannotMeasureYet (0.34s)
=== RUN   TestRunCloseoutSelectionReportsAHoleAndNotAMiss
    run_closeout_reap_window_test.go:198: the hole-vs-miss arm: pid 1080394 is no longer the fixture's — the kernel handed it to this host's reaping ancestor 3100 after 17.943599ms
--- PASS: TestRunCloseoutSelectionReportsAHoleAndNotAMiss (0.11s)
=== RUN   TestRunCloseoutSelectionDoesNotCountAnotherProcessAsItsOwnHole
    run_closeout_reap_window_test.go:324: the hole-scope guard: pid 1080456 is no longer the fixture's — the kernel handed it to this host's reaping ancestor 3100 after 11.788238ms
--- PASS: TestRunCloseoutSelectionDoesNotCountAnotherProcessAsItsOwnHole (0.13s)
ok  	github.com/nick/gopi/internal/tools	1.881s
```

  The comparison is the measurement: the same three arms took **8.08s / 8.09s / 8.12s and
  FAILED** before the fix (`closeout-repro.out`) and take **8.06ms / 17.94ms / 11.79ms and
  PASS** now — the premise stopped being a wall-clock wait on an unsatisfiable condition.
- Tests (package): `make test-budget PKGS='./internal/tools'` → **exit 0, outcome=pass**
  - Ran: `make test-budget PKGS='./internal/tools'`
  - Result: `packages: 1 finished, 0 with a failure`; `elapsed=475s` (32 cores, load
    1.21→1.02), `exit=0 outcome=pass`, `3299 test(s) ran in this run`. The stream's own
    package line: `ok github.com/nick/gopi/internal/tools 462.800s`.
  - This is the whole package, not a filter — and it is the run that also carries the
    residual same-family skips, whose cost is visible in the same report's slowest-test
    table (`TestSharedSessionHolderProbe` 122.67s, five subtests at 23.9–25.1s each,
    all `t.Skipf` on the literal-init premise: 186s of the 475s wall is the unsatisfiable
    wait).
- Ran (red lever A):
  `sed -i 's/\t\tif ppid == reaper {/\t\tif ppid == runCloseoutReapInitPid {/' internal/tools/run_closeout_reap_window_test.go`
  → `go test -count=1 -run 'TestRunCloseout' ./internal/tools/`
  - Result: `EXIT=1`; `--- FAIL` on all three arms:
    `the hole arm: pid 1075650 was still parented by 3100 after 8s` (and the two sibling
    arms at `:198` and `:324`). This is the filed bug reproduced by mutation.
- Ran (red lever B):
  `sed -i 's/\treaper, reason := testutil.ProcReaperOf()/\treaper, reason := 0, "injected mutation: unmeasurable reaper"/'`
  → `go test -count=1 -run 'TestRunCloseout' ./internal/tools/`
  - Result: `EXIT=1`; 3 FAIL in 0.07–0.10s:
    `this host's reaping ancestor could not be measured (injected mutation: …), so "the
    member has left the fixture's tree" is undecidable … an undecidable premise is never
    a pass`. The `reaper <= 0` arm bites.
  - Both mutations reverted with `git checkout --`; the file's sha256 was verified
    identical before and after (`26b8140009204f84`), and `git status --porcelain` was
    empty at that point.
- Commit: `031c2bf95` — the commit that landed this report and the `DOC-INDEX.tsv`
  classification in the same change (Rule 36). Subsequent commits fill the corrections
  above (the package-run line, the evidence table, the Class D census); the last one is
  named in the deliverable declaration at the end of this file.
- Ran (production-site probe, Class D): a throwaway `internal/tools/zz_a12e58_probe_test.go`
  → `go test -count=1 -v -run TestZZA12e58ProbeReparentTargets ./internal/tools/`
  - Result: `PROBE: production finishCleanupInitPid()=1` / `measured ProcReaperOf()=3100`
    / `real orphan pid=… reparented to ppid=3100` / `classify(initPid=1) → inheritor=false`
    (output `evidence/probe-reparent-targets.out`). The probe file was **deleted in the
    same run**; `git status --porcelain` is empty.
- Ran (Rule 41 filing): `gopi task submit --tags bug --payload @…/artifacts/bug-production-initp-binds-the-literal-init.md`
  - Result: `Task queued: 01a12e8d-7ad5-7e35-8e64-6ad3090c7ad9 (status=pending, source=agent, tags=bug)`;
    `[bug-flow] tasks with tag bug: 352 (verified 0, reopened 7, other 345)`.

## Evidence (self-filed)

All raw output is under
`/home/user/.gopi/sessions/01a12e58-8171-79f2-9130-f8a8000d425b/evidence/`:

| file | what it is |
|---|---|
| `acceptance-green.out` | `go test -count=1 -v -run 'TestRunCloseout'`, 12 PASS, `EXIT=0` |
| `mutation-A-literal-init.out` | red lever A: 3 FAIL, `EXIT=1`, original 8s/ppid=3100 shape |
| `mutation-B-unmeasurable.out` | red lever B: 3 FAIL in 0.10s, `EXIT=1` |
| `family-measure.out` | the family census run: 106 PASS, 11 SKIP, 0 FAIL, `EXIT=0` |
| `test-budget-tools.out` | `make test-budget PKGS='./internal/tools'` — exit 0, pass, 3299 tests, 475s |
| `final-acceptance.out` | the acceptance command re-run on the frozen tree at the final HEAD (`edd687979`): `ok … 1.951s`, `EXIT=0` |
| `tree-census.out` | the census query A/B/C output at HEAD `ed9cf37c7` (Rule 38: re-runnable, names the commit) |
| `tree-census-predicate.out` | the predicate-level scan: literal comparisons, wait helpers, and every site already on `ProcReaperOf`/`OrphanReparentTargets` |
| `probe-reparent-targets.out` | the throwaway `/proc`-level probe: production `finishCleanupInitPid()=1` vs kernel reality `3100` (probe file deleted; `git status` clean) |
| `probe-initpid-consumers.out` | the second throwaway probe: the two remaining `finishCleanupInitPid()` consumers measured against a real orphan (`ppid=3100`) — the rendering site fails to say ORPHAN; `finishCleanupSelfTree` measured via `members_not_in_chain=[]` (probe file deleted; `git status` clean) |
| `probe-selftree-membership.out` | the decisive self-tree measurement: `finishCleanupSelfChain()` `contains(3100)=true` and `members_not_in_chain=[]` — so 3100 enters as an ancestor, never through the expansion (probe file deleted; `git status` clean) |
| `acceptance-rerun-tip.out` | the acceptance command re-run at the final tip `57270b1e3`, probe removed, with `git status --porcelain` captured empty in the same log |
| `post-merge-family.out` | the family command re-run POST-merge (`01d4184d5`): `101 PASS / 3 SKIP / 0 FAIL`, the 3 SKIPs all declared gates — the measurement that reversed the census |
| `doccheck-postmerge.out` | `gopi tool doccheck` post-merge on this branch: `EXIT=1`, identical verdict to pristine master |
| `doccheck-master-drift-culprit.out` | the anchor-line readings across `8c423e627^` / `8c423e627` / `a3bc9bf96` that narrow the master-side culprit window |
| `final-acceptance-tip-d6816bd8e.out` | the acceptance at tip `d6816bd8e` with `EXIT=0`, 12 `--- PASS`, and the clean-worktree capture in the same log |

**The cover commit, measured (not inferred):**

```
$ git merge-base --is-ancestor 951e2b03f HEAD && echo ANCESTOR
ANCESTOR: fix 951e2b03f IS in my tree
$ git log --oneline -1 951e2b03f
951e2b03f fix(tools): the hole arms' premise is the reaping ancestor, never the literal init
```

**The reparent target on this host, measured from /proc, not read from a doc:**

```
$ sh -c 'sleep 300 & echo $!'      # → e.g. 1023881
$ awk '{print $4}' /proc/1023881/stat    # → 3100
$ cat /proc/3100/comm              # → systemd ; argv carries --user (this host's subreaper)
```

Hence the premise "wait until ppid == 1" is unsatisfiable here — and this is also what
the two mutations above independently demonstrate.

## The same-family census (first pass)

> **Superseded by "The same-family census — the full enumeration" below.** This first pass
> is kept because it is the run's own earlier reading; the authoritative enumeration adds
> Class D (the **production** site, measured with a throwaway probe) and re-states the
> gate sites with file:line verdicts. Where the two differ, the later section wins.

Query: `git grep -n "reparented to init\|never reparented\|was not reparented" -- '*.go'`,
plus a scan of every `finishCleanupInitPid()` consumer. The family is confined to
`internal/tools` (test fixtures) + five mechanism sites elsewhere.

### Class A — already migrated to the mechanism (no action)

| site | premise now | evidence |
|---|---|---|
| `internal/tools/run_closeout_reap_window_test.go:454` (`runReapTestWaitAdopted`) | `testutil.ProcReaperOf()` | commit `951e2b03f`; **measured green** (above) |
| `internal/testutil/procident.go` | *is* the mechanism (`ProcReaperOf` / `Orphan()`) | `mechanism_hits=15` |
| `internal/testutil/procident_test.go` | registers against it | `mechanism_hits=15` |
| `internal/testutil/orphansweep_test.go` | `ProcReaperOf`/`Orphan()` | `mechanism_hits=2`, `literal_init_hits=0` |
| `cmd/agent/daemon_cgroup_e2e_test.go` | cgroup + `ProcReaperOf` (Rule 24) | `literal_init_hits=0` |
| `cmd/agent/worker_daemon_flag_e2e_test.go:373` | migrated to cgroup-only; the `ppid == 1` text is a **comment recording the removal** ("These two assertions were `ppid == 1` and `sid == pid`, and BOTH fail…") | read at `:368`,`:373` |

### Class B — legitimately skipped (declared, listed, not silent)

| test | skip reason |
|---|---|
| `TestFinishCleanupServerArmEmitsTheAuditRecord` (`removal_audit_record_test.go:164`) | `heavytest.Require(CriterionDiskFixture)` — 0.00s, prints its reason |
| `TestFinishCleanupActionBudgetOutlivesTheLedgerRead` (`task_finish_deliverysync_test.go:472`) | `heavytest.Require(CriterionDiskFixture)` — 0.00s, prints its reason |

### Class C — RESIDUAL GAP: premise still the literal init, verdict **SKIP** (measured live)

Measured with `go test -count=1 -v -run 'TestFinishCleanup|TestSharedSessionHolder|TestHolderRefusal|TestReclaimOwnFinished|TestOrphanTree|TestOrphanCwd|TestCwdHolder|TestLiveRunCwd' ./internal/tools/`
→ `106 PASS, 11 SKIP, 0 FAIL, EXIT=0` (`family-measure.out`). **The skip is not a pass**:
the green light is on and the assertion never executed.

| # | gate site | test that is lost | measured |
|---|---|---|---|
| 1 | `task_finish_cleanup_holder_naming_test.go:112` | `TestHolderRefusalNamesAnOrphanAsAnOrphan` | SKIP 20.02s — `orphan probe 1021908 was not reparented to init 1 within 20s` |
| 2 | `task_finish_cleanup_orphan_test.go:166` | `TestOrphanCwdInheritorIsNotAHolder`, `TestOrphanCwdInheritorReleaseDoesNotReclaimLiveWork`, `TestCwdHolderWithoutALauncherRecordKeepsTheVeto` | SKIP 10.36 / 10.39 / 10.67s |
| 3 | `task_finish_cleanup_orphan_tree_test.go:146` | `TestOrphanTreeDescendantIsReleasedWithItsAncestor` (+ the third-party control arm) | SKIP 10.68s |
| 4 | `task_finish_cleanup_holder_shared_session_test.go:618` | `TestSharedSessionHolderProbeChild` (5 subtests: release / no-records / foreign-ground / third-party / leader) | SKIP 21.07 / 20.75 / 20.74 / 21.06 / 21.05s |
| 5 | `task_reclaim_self_orphan_test.go:210` | `TestFinishCleanupSelfTreeSparesThirdParties` | SKIP 3.02s |
| 6 | `task_reclaim_own_release_selfmatch_test.go:362` | `TestReclaimOwnFinishedWorktreesFreesItsOwnGroundAndKeepsAForeigns` | SKIP 3.08s |

**Measured bottleneck the fix must beat**: these sites spend **3–21s each and ~186s
total** per constrained run — the cost is the `ttl` × `poll` product of a wait that can
never be satisfied on this host (`orphanSettleWait`, `sharedSessionSettleWait` = 20s).
The sibling branch's `OrphanReparentTargets` predicate returns on the **first poll**.

**Ownership — the reason this run does not fix them.** The fix exists on a **live,
in-flight** branch rather than on the branch of the row that first measured the sites.
`0631db7ad` ("fix(fixtures): the orphan reparent premise is the MECHANISM, not ppid 1")
was authored by row `01a12e43-d004-7432-b0e0-b99d0e7c0664` (now `completed`, **its branch
deleted** — `git for-each-ref | grep 01a12e43` → NONE, its worktree removed) and is
reachable today only from `task/01a12e6423c37c10a07cb85aef54924d` — row
`01a12e64-23c3-7c10-a07c-b85aef54924d`, `in_progress`, claimed by `hosta:3904017`
(`gopi task for-branch`). It adds the leaf package `internal/procname`
(`OrphanReparentTargets`) plus a `task_finish_cleanup_reparent_targets_test.go` guard, and
`git show 0631db7ad --stat` names **exactly the 6 files** in the table above. That commit
is **not an ancestor of this worktree's HEAD, nor of master** — the fix exists but is
unmerged. Inspected read-only (`git show <branch>:<path>`), per Rule 29.

**The census holds on the executable path, not only in a filtered run.** The package-wide
`make test-budget` run counts it independently: `3299 run`, `3182 pass`, **`118 skip`**,
`0 fail` (Action counters read from that run's own stream
`run-1791781592-1078216.json`), and all **nine** residual top-level tests listed here
appear in it as `"Action":"skip"` — i.e. in the ordinary, unfiltered package run on this
host every one of these assertions is **still not executed**. That is the measured reason
this census is the deliverable: a green package light says nothing about them.

## The same-family census — the full enumeration, with per-site verdicts (AUTHORITATIVE **at that reading**)

> **READ THE "Post-merge re-measurement" SECTION FIRST.** The delivery-point sync
> (`01d4184d5`) brought master's `0631db7ad` into this tree, which **closes Class C as
> MIGRATED and Class D as already-satisfied-by-master**. The enumeration below is this
> run's reading of the pre-merge tree — kept because the run's own measurement trail is
> the record, and superseded because the tree moved under it.

**Query (re-runnable, Rule 38):**

```bash
git grep -nE '(reparented|adopted|handed)[^"]*to init|ppid *[=!<>]=? *1|PPID *[=!<>]=? *1|reparentedToInit|neverReparented' -- '*.go'
git grep -nE 'ppid[^/]*[=!<>]= *1( |\)|$|\{)|== *initPid|!= *initPid|<= *initPid|ppid *<= *runCloseoutReapInitPid' -- '*.go'
git grep -nE 'ProcReaperOf|OrphanReparentTargets' -- '*.go'
```

Raw output: `evidence/tree-census.out`, `evidence/tree-census-predicate.out`. HEAD at
census time `ed9cf37c7`.

### Class A — the executable premise, migrated to a MEASURED mechanism (no action)

| site | evidence |
|---|---|
| `internal/tools/run_closeout_reap_window_test.go:454` (`runReapTestWaitAdopted`) | `reaper, reason := testutil.ProcReaperOf()`; `tests: 12/12 PASS`, mechanism anchor `reaping ancestor 3100` in the log |
| `internal/testutil/procident.go:335` (`ProcReaperOf`) | **is** the mechanism; 4 production consumers |
| `internal/testutil/procident.go` (`Orphan()`) | judges by the measured reaper, never the literal |
| `internal/testutil/orphansweep.go:176` | `reaper, reason := ProcReaperOf()`, required `> 0` |
| `internal/testutil/subreaper_chain_test.go:288`, `procident_test.go:266` | register against the mechanism, `Fatalf` when unmeasurable (Rule 15) |
| `cmd/agent/daemon_cgroup_e2e_test.go:383`, `daemon_test_leak_e2e_test.go:355` | migrated (Rule 24 family) |
| `cmd/agent/worker_daemon_flag_e2e_test.go:373` | migrated; the `ppid == 1` text is a **comment recording its removal** |

### Class B — skipped by a DECLARED, printed gate (not silent — no action)

| test | gate |
|---|---|
| `removal_audit_record_test.go:164` → `TestFinishCleanupServerArmEmitsTheAuditRecord` | `heavytest.Require(CriterionDiskFixture)`, 0.00s, prints its reason |
| `task_finish_deliverysync_test.go:472` → `TestFinishCleanupActionBudgetOutlivesTheLedgerRead` | same, 0.00s |

### Class C — RESIDUAL: premise still the literal, measured **SKIP** in the ordinary package run

Nine top-level tests, six gate call sites, all measured live on this host — see the
table in the section above and `evidence/family-measure.out`. Independently confirmed on
the **unfiltered** package stream (`evidence/test-budget-tools.out`, Action counters
`3299 run` / `3182 pass` / **`118 skip`** / `0 fail`): every one of the nine appears there
as `"Action":"skip"`. The green light is on and the assertion never executed.

Gate call sites and their verdict:

| site | verdict |
|---|---|
| `task_finish_cleanup_holder_naming_test.go:75,104,112` | **residual gap** — `initPid := finishCleanupInitPid()` (`=1` here), `ppid == initPid`, else `t.Skipf` |
| `task_finish_cleanup_holder_shared_session_test.go:606,612,618` | **residual gap** — same shape, via `sharedSessionWaitReparented` |
| `task_finish_cleanup_orphan_test.go:155,162` | **residual gap** — same shape |
| `task_finish_cleanup_orphan_tree_test.go:135,142,146` | **residual gap** — same shape |
| `task_reclaim_own_release_selfmatch_test.go:350,356` and `:362`,`:390` | **residual gap** — same shape, two arms |
| `task_reclaim_self_orphan_test.go:190` (`reparentedToInitStandIn`), used at `:158`/`:210` | **residual gap** — the helper itself waits for the literal init |
| `task_finish_cleanup_holder_orphan.go:669` (`if initPid <= 0 \|\| ppid != initPid`) | **production site** — see the production finding below |

### Class D — the PRODUCTION site: a measured gap, and the honest label on it

`internal/tools/task_finish_cleanup_holder_orphan.go:510` binds
`initPid := finishCleanupInitPid()`, and rung 2 of the classifier (`:669`) holds the veto
unless `ppid == initPid`. `finishCleanupInitPid` (`:152`) returns **1** on this host
(`/proc/1/comm == systemd`, `ppid == 0`), while the kernel hands orphans to **3100**
(`systemd --user`, the subreaper).

**Measured (a throwaway probe, `evidence/probe-reparent-targets.out`, deleted after
use — `git status` clean):**

```
PROBE: production finishCleanupInitPid()=1
PROBE: measured testutil.ProcReaperOf()=3100 ()
PROBE: they agree=false
PROBE: real orphan pid=1147562 reparented to ppid=3100 (readable=true)
PROBE: classify(initPid=1)        → who="pid 1147562 (sleep)" why="" inheritor=false
PROBE: classify(reaper=3100)      → who="pid 1147562 (sleep)" why="" inheritor=false
```

**What this measurement does and does NOT establish — labelled, per Rule 41:**

- **Measured**: the production binding is `1`; the kernel's actual reparent target here
  is `3100`; they disagree; a real orphan lands at `3100`.
- **Measured**: both classifier calls return `inheritor=false`.
- **Inference (not measured — the two calls return the same value):** the two refusals
  are reached at **different rungs**. With `initPid=1` the call returns at rung 2
  (`ppid 3100 != 1`); with `reaper=3100` it returns at rung 3 (the probe passed `raw=""`,
  so there is no launcher record to match). The probe alone cannot separate them, and the
  rung is read from the code, not observed. A follow-up that wants the rung *measured*
  would inject the env record so that rung 3 passes, and see the two calls diverge.
- **Consequence, stated as the inference it is**: on this host the classifier's rung 2
  refuses **every** honestly-orphaned holder, i.e. the release direction the ladder exists
  for is unreachable here — which is consistent with the six residual test sites all
  landing in SKIP.

The production change is
exactly what branch `task/01a12e6423c37c10a07cb85aef54924d` carries: it replaces the
scalar `initPid` with `reparentTargets map[int]bool` from
`procname.OrphanReparentTargets`, threads it through
`finishCleanupClassifyOrphanHolder(pid, cwd, raw, reparentTargets)` (rung 2 becomes
`if len(reparentTargets) == 0 || !reparentTargets[ppid]`), and rewrites the two census
sites (`pid <= 1 || reparentTargets[pid]`) and the rendering site. That branch is
`in_progress` and **unmerged**; the row that first measured the family (`01a12e43`) is
`completed` with its branch deleted. That is why this run does not touch them.

**Filed as a row rather than fixed here** (Rule 41): `gopi task submit --tags bug` →
**`01a12e8d-7ad5-7e35-8e64-6ad3090c7ad9`** (payload
`~/.gopi/sessions/01a12e58-8171-79f2-9130-f8a8000d425b/artifacts/bug-production-initp-binds-the-literal-init.md`,
which states the measured/inferred split and names the in-flight branch so the executor
can decide whether it is a duplicate — Rule 26). Rule 23/29 forbid this run from touching
those paths: another live claim owns them.



### Class D (continued) — the OTHER two `finishCleanupInitPid()` consumers, measured

The census table's Class-D row named the classifier. Two further production call sites
exist, and the earlier text named them only inside the in-flight branch's description —
so they were **measured** this run, with a second throwaway probe deleted after use
(`evidence/probe-initpid-consumers.out`, `evidence/probe-selftree-membership.out`;
`git status --porcelain` empty after each removal):

| site | what it does with the value | measured verdict on THIS host |
|---|---|---|
| `task_finish_cleanup.go:3502` `finishCleanupHolderParentSuffix` | `if ppid == finishCleanupInitPid()` → render `"ORPHAN — reparented to init, nothing will reap it"` | **RESIDUAL GAP (rendering)** — for a real orphan (`pid 1189190`, `/proc`-read ppid **3100**) it printed `" ppid=3100"` and **not** the ORPHAN word: `says ORPHAN=false` while the kernel fact is that this pid IS an orphan. Same family, same literal, diagnostic-only (it renders a line; it does not decide a release). Covered by the same filed row's in-flight branch, which "rewrites … the rendering site" |
| `task_finish_cleanup.go:3330` `finishCleanupSelfTree` | `if pid <= 1 \|\| (initPid > 0 && pid == initPid) { continue }` — the notThirdParty seed | **NO DEFECT, measured (and the reason is the interesting part)** — `initPid(1) present=false` yet `reaper(3100) present=true`: 3100 is a member *because it is an ancestor* on `finishCleanupSelfChain()` (`contains(3100)=true`), which the file's own comment declares intended ("ancestors stay EXCLUDED … nothing is attributed to them"). The decisive measurement is `members_not_in_chain=[]` — **no member entered through the expansion**, so the literal-init exclusion and the would-be `reparentTargets` exclusion produce the identical set on this host. Labelled as such: the *equality of the two sets here* is measured; *that they stay equal on a host whose reaper is neither 1 nor an ancestor* is an inference, not observed |
| `run_closeout_reap.go:84` `runCloseoutReapInitPid = 1` | a **lower bound**, never a wait premise (`Leader <= …`, `cur > …`, `pid <= …` guards) | **NOT THE SAME FAMILY** — measured: a real orphan's ppid `3100 > 1` is **true**, so every bound holds; the constant answers "is this a signalable process pid" (pid ≤ 1 is never one), which is host-independent. Class remains A |

The landed fix moved a **host-specific literal to a measured mechanism**: the premise is
now "the kernel handed the member to *this host's reaping ancestor*", where that pid is
measured from `/proc`. Two things could have made that a weakening, and both are covered:

1. **It could have stopped asserting the property.** It did not — WAITING IS NOT
   WEAKENING: the seam is still replaced for the whole selection and every assertion is
   unchanged. Mutation **A** proves the arms still bite on the very thing they pin.
2. **It could have turned an unmeasurable premise into a pass or a skip.** It did not —
   `reaper <= 0` is an immediate `t.Fatalf` naming the reason (Rule 15). Mutation **B**
   proves that arm bites too.

## Post-merge re-measurement — the delivery-point sync changes the ANSWER (this is the section to read)

Written **after** the delivery-point sync (`git merge master` → merge commit
`01d4184d5`), because the merge moved master from the tree the census was taken
against. Measured, not assumed:

**What arrived in the merge: commit `0631db7ad`** ("fix(fixtures): the orphan reparent
premise is the MECHANISM, not ppid 1") — the very fix the census above recorded as
**resident but unmerged** on the in-flight branch `task/01a12e64…`. It is on master now.

**Consequence 1 — Class C is no longer a residual gap; it is MIGRATED.** Re-measured
per file on this post-merge tree:

| Class-C site (per the census above) | pre-merge | post-merge (measured) |
|---|---|---|
| `task_finish_cleanup_holder_naming_test.go` | literal init, `t.Skipf` | `waitOrphanReparentTarget` (`t.Skipf`-on-reparent **0**) |
| `task_finish_cleanup_holder_shared_session_test.go` | `t.Skipf` | `waitOrphanReparentTarget` (0) |
| `task_finish_cleanup_orphan_test.go` | `t.Skipf` | `waitOrphanReparentTarget` (0) |
| `task_finish_cleanup_orphan_tree_test.go` | `t.Skipf` | `waitOrphanReparentTarget` (0) |
| `task_reclaim_own_release_selfmatch_test.go` | `t.Skipf` | `waitOrphanReparentTarget` (0) |
| `task_reclaim_self_orphan_test.go` | `t.Skipf` (helper `reparentedToInitStandIn`) | `waitOrphanReparentTarget` (0) |

and the **live** consequence — the same family command that reported **`106 PASS / 11 SKIP
/ 0 FAIL`** before the merge now reports **`101 PASS / 3 SKIP / 0 FAIL`**
(`evidence/post-merge-family.out`), and the 3 remaining SKIPs are all **Class B**
(declared `heavytest.Require` gates + one 0.00s probe-child helper), not the premise.
The eight tests that used to light a green lamp without running their assertion now
**run**: e.g. `TestHolderRefusalNamesAnOrphanAsAnOrphan 0.02s PASS`,
`TestOrphanCwdInheritorIsNotAHolder 0.52s PASS`,
`TestOrphanTreeDescendantIsReleasedWithItsAncestor 0.67s PASS`. The mechanism anchor
`reaper 3100` appears **14 times** in that log. **Class C is therefore closed as MIGRATED,
and the correct verdict today is: no site in the tree still skips on the literal-init
premise.**

**Consequence 2 — Class D is no longer a residual gap either.** `git grep finishCleanupInitPid`
over `internal/` (non-test) on the post-merge tree returns **nothing**: the production
literal-init binding is **GONE**, replaced by the target set
(`finishCleanupOrphanReparentTargets() → procname.OrphanReparentTargets`,
`task_finish_cleanup_holder_orphan.go:144`) with rung 2 reading
`if len(reparentTargets) == 0 || !reparentTargets[ppid]`, and the rendering site this run
measured (`:3521`) now printing
`(ORPHAN — reparented to this host's reaper %d, nothing will reap it)`. So the row this run
filed for it (`01a12e8d`) is **already satisfied by master** — its executor should verify
against master first and close it as a duplicate rather than redo the work (Rule 26; the
row's own payload names that in-flight branch precisely so this can be judged).

**Consequence 3 — the red lever's premise was NOT invalidated by the merge.** The red-lever
proof (§`## Verification`) mutated `run_closeout_reap_window_test.go` on the pre-merge tree.
On the post-merge tree that file is **byte-identical** to the revision the mutations were
performed on (`git diff --stat 57270b1e3 HEAD -- internal/tools/run_closeout_reap_window_test.go`
→ **empty**) and its mutation target is still present verbatim (`:469 if ppid == reaper {`),
so the recorded FAIL outputs remain the proof for **this** tree; the acceptance was
additionally re-run post-merge (`EXIT=0`, 12/12 PASS, `ok … 1.675s`).

**Consequence 4 — the doccheck observation reverses (Rule 41: correct in place).** The
bullet in `## Not Done` claiming "`doccheck` exits 1 on this branch, 0 on master" was
measured **before** the merge, when the branch carried `c6e93569e` and master did not. On
the **post-merge** trees the opposite is measured: **both** the pristine main repo at
`master 88efcaa97` and this branch exit **1**, with an **identical** verdict
(`3 NOT PERMITTED + 4 STALE PERMIT`, differing only in one anchor's *line number*:
`:1938` vs `:1939`) — `diff` of the two raw logs shows that single line and nothing else.
`c6e93569e` **is** in master now (`merge-base --is-ancestor` → 0), so this is a
**master-side** drift, not a branch-side one, and it is not this run's to fix (the table is
DATA: regenerate, never hand-edit). `evidence/doccheck-postmerge.out`. The claim
"`0 gap(s)` between `devdoc/` and `devdoc/DOC-INDEX.tsv`" holds in both trees and is
unaffected.

**Honest boundary on this section**: the pre-merge census text above is kept **as
written** (it is the run's own earlier reading, and the rule is to record supersession,
not to erase it) — but a reader acting on it must use this section instead, because the
answer to "is there a residual same-family gap?" changed from **yes** to **no** when the
delivery-point sync landed.

### Rule 41 filings this run made (all with `/proc`-level or command-level evidence)

| row | subject | status at close-out |
|---|---|---|
| `01a12e8d-7ad5-7e35-8e64-6ad3090c7ad9` | the production `finishCleanupInitPid` literal-init binding | **CANCELLED by measurement** — master now carries the fix (`0631db7ad`); the cancel reason records the four measured points |
| `01a12e8e-fa6b-75aa-aa13-f88547d954d9` | doccheck red on branch `c6e93569e` | **CANCELLED by measurement** — the premise flipped after the merge (both trees exit 1 identically); superseded by the row below |
| `01a12e98-c1cd-7042-a64a-a6726187ce16` | **`gopi tool doccheck` is red on PRISTINE master `88efcaa97`** — 3 `NOT PERMITTED` + 4 `STALE PERMIT`, so a green doccheck is currently unfalsifiable; culprit window narrowed to `a3bc9bf96` (`loop.go` anchors) and the `cli_tasks.go` window | **pending**, payload carries the measurement |

The `pending` row is the fileable outcome of the last two cancels. **Both cancels were
written *after* the measurement that falsified them, and their reasons carry the numbers** —
a row is not closed by an opinion here, it is closed by pointing at the output that decided
it (Rule 41).

## Appendix — raw output committed with this report (Rule 5)

The full acceptance log and both mutation logs are reproduced here so the evidence
travels with the branch rather than living only in a session directory. The family
census is reproduced as its verdict lines; the full 42 KB log is at
`/home/user/.gopi/sessions/01a12e58-8171-79f2-9130-f8a8000d425b/evidence/family-measure.out`.

### A1 — acceptance: `go test -count=1 -v -run 'TestRunCloseout' ./internal/tools/`

```
### ACCEPTANCE: go test -count=1 -run 'TestRunCloseout' ./internal/tools/   (HEAD=1ec2897814b0386f219f0fd4579e0bd7af029457)

testutil: suite "tools": scratch root /dev/shm is tmpfs (memory filesystem) (placement memory-base, 26.6 GiB free) — fresh ledgers under it are built on a memory filesystem, so their first opens pay no block device's journal commits
=== RUN   TestRunCloseoutReapTerminatesTheRunsLeakedTree
    run_closeout_reap_test.go:593: RED state (the leak, before the reap): pid 1079981 ppid=3100 pgrp=1079974 sid=1079974 wrapper="1079974" cwd="/dev/shm/suite-tools-run-2022099767/TestRunCloseoutReapTerminatesTheRunsLeakedTree134867637/001"
    run_closeout_reap_test.go:601: reap lines: [run-closeout reap: terminated pid 1079981 (sleep)]
    run_closeout_reap_test.go:238: cleanup: member pid 1079981 is already dead — nothing to signal
--- PASS: TestRunCloseoutReapTerminatesTheRunsLeakedTree (0.15s)
=== RUN   TestRunCloseoutReapKeepsAThirdPartyInTheSameWorktree
    run_closeout_reap_test.go:629: control state (must survive): pid 1080050 ppid=3100 pgrp=1080042 sid=1080042 wrapper="999999998" cwd="/dev/shm/suite-tools-run-2022099767/TestRunCloseoutReapKeepsAThirdPartyInTheSameWorktree3822707369/001"
    run_closeout_reap_test.go:636: reap lines: [run-closeout reap: terminated pid 1080047 (sleep)]
    run_closeout_reap_test.go:238: cleanup: member pid 1080047 is already dead — nothing to signal
--- PASS: TestRunCloseoutReapKeepsAThirdPartyInTheSameWorktree (0.23s)
=== RUN   TestRunCloseoutReapLeavesADaemonisedDescendantAlone
    run_closeout_reap_test.go:661: detached member state (must survive): pid 1080127 ppid=3100 pgrp=1080127 sid=1080127 wrapper="999999997" cwd="/dev/shm/suite-tools-run-2022099767/TestRunCloseoutReapLeavesADaemonisedDescendantAlone641699588/001"
    run_closeout_reap_test.go:668: reap lines: [run-closeout reap: terminated pid 1080126 (sleep)]
    run_closeout_reap_test.go:238: cleanup: member pid 1080126 is already dead — nothing to signal
--- PASS: TestRunCloseoutReapLeavesADaemonisedDescendantAlone (0.25s)
=== RUN   TestRunCloseoutReapWatchesAPidItCannotMeasureYet
    run_closeout_reap_window_test.go:86: member, measured normally: pid 1080240 ppid=3100 pgrp=1080222 sid=1080222 wrapper="1080222" cwd="/dev/shm/suite-tools-run-2022099767/TestRunCloseoutReapWatchesAPidItCannotMeasureYet147616417/001"
    run_closeout_reap_window_test.go:103: the hole arm: pid 1080240 is no longer the fixture's — the kernel handed it to this host's reaping ancestor 3100 after 8.064137ms
    run_closeout_reap_window_test.go:134: inside the window: pid 1080240 is unmeasured (owned=[])
    run_closeout_reap_window_test.go:138: reap lines: [run-closeout reap: terminated pid 1080240 (sleep)]
    run_closeout_reap_window_test.go:140: after the reap, the same rules once more: owned=[] unmeasured=[] ok=true (lies spent=3)
    run_closeout_reap_test.go:238: cleanup: member pid 1080240 is already dead — nothing to signal
--- PASS: TestRunCloseoutReapWatchesAPidItCannotMeasureYet (0.34s)
=== RUN   TestRunCloseoutSelectionReportsAHoleAndNotAMiss
    run_closeout_reap_window_test.go:198: the hole-vs-miss arm: pid 1080394 is no longer the fixture's — the kernel handed it to this host's reaping ancestor 3100 after 17.943599ms
--- PASS: TestRunCloseoutSelectionReportsAHoleAndNotAMiss (0.11s)
=== RUN   TestRunCloseoutSelectionDoesNotCountAnotherProcessAsItsOwnHole
    run_closeout_reap_window_test.go:324: the hole-scope guard: pid 1080456 is no longer the fixture's — the kernel handed it to this host's reaping ancestor 3100 after 11.788238ms
    run_closeout_reap_window_test.go:363: stray pid 1080466 is a hole for the host's census (2 holes) and NOT for this arm's scope (map[1080456:true 1080457:true 1080458:true])
--- PASS: TestRunCloseoutSelectionDoesNotCountAnotherProcessAsItsOwnHole (0.13s)
=== RUN   TestRunCloseoutStaleRunAlsoCarriesTheResidueRecord
[task-notify] run t-stale closed out with its OWN untracked residue in /dev/shm/suite-tools-run-2022099767/TestRunCloseoutStaleRunAlsoCarriesTheResidueRecord2611626680/002/wt: worktree is DIRTY (untracked-only residue, no tracked change) — the run must clean its own scratch before it reports ready — entries: ?? final.out; the run's scratch was never cleaned (the outcome stands, nothing deleted)
[task-notify] task run closeout: t-stale → completed (child normal exit, wrapper writeback never landed)
--- PASS: TestRunCloseoutStaleRunAlsoCarriesTheResidueRecord (0.10s)
=== RUN   TestRunCloseoutClosesOpenRow
[task-notify] task run closeout: t-run-close → completed (child normal exit, wrapper writeback never landed)
--- PASS: TestRunCloseoutClosesOpenRow (0.05s)
=== RUN   TestRunCloseoutIdempotentAfterChildSelfReport
--- PASS: TestRunCloseoutIdempotentAfterChildSelfReport (0.03s)
=== RUN   TestRunCloseoutRefusesUnclaimedRow
[task-notify] task run closeout t-run-unclaimed: no live claim evidence (claimed_by="" session_id="") — left open for the stale sweep
--- PASS: TestRunCloseoutRefusesUnclaimedRow (0.03s)
=== RUN   TestRunCloseoutRejectsNonTerminalOutcome
--- PASS: TestRunCloseoutRejectsNonTerminalOutcome (0.01s)
=== RUN   TestRunCloseoutRefusesLiveWrapper
[task-notify] task run closeout t-run-live: no live claim evidence (claimed_by="hosta:1079851" session_id="") — left open for the stale sweep
--- PASS: TestRunCloseoutRefusesLiveWrapper (0.02s)
PASS
ok  	github.com/nick/gopi/internal/tools	1.881s
EXIT=0
```

### A2 — red lever A: predicate reverted to the literal init

```
### MUTATION A: runReapTestWaitAdopted predicate reverted to literal init (ppid == runCloseoutReapInitPid == 1)
$ sed -i 's/if ppid == reaper {/if ppid == runCloseoutReapInitPid {/' internal/tools/run_closeout_reap_window_test.go

testutil: suite "tools": scratch root /dev/shm is tmpfs (memory filesystem) (placement memory-base, 26.5 GiB free) — fresh ledgers under it are built on a memory filesystem, so their first opens pay no block device's journal commits
--- FAIL: TestRunCloseoutReapWatchesAPidItCannotMeasureYet (8.07s)
    run_closeout_reap_window_test.go:86: member, measured normally: pid 1075650 ppid=1075649 pgrp=1075649 sid=1075649 wrapper="1075649" cwd="/dev/shm/suite-tools-run-1495061836/TestRunCloseoutReapWatchesAPidItCannotMeasureYet3392269452/001"
    run_closeout_reap_window_test.go:103: the hole arm: pid 1075650 was still parented by 3100 after 8s — the kernel has not handed the fixture's member to this host's reaping ancestor (3100, reaper of an abandoned child of this process: ), so R3 can still claim it and this arm's premise does not hold yet
--- FAIL: TestRunCloseoutSelectionReportsAHoleAndNotAMiss (8.08s)
    run_closeout_reap_window_test.go:198: the hole-vs-miss arm: pid 1075719 was still parented by 3100 after 8s — the kernel has not handed the fixture's member to this host's reaping ancestor (3100, reaper of an abandoned child of this process: ), so R3 can still claim it and this arm's premise does not hold yet
--- FAIL: TestRunCloseoutSelectionDoesNotCountAnotherProcessAsItsOwnHole (8.13s)
    run_closeout_reap_window_test.go:324: the hole-scope guard: pid 1076368 was still parented by 3100 after 8s — the kernel has not handed the fixture's member to this host's reaping ancestor (3100, reaper of an abandoned child of this process: ), so R3 can still claim it and this arm's premise does not hold yet
[task-notify] run t-stale closed out with its OWN untracked residue in /dev/shm/suite-tools-run-1495061836/TestRunCloseoutStaleRunAlsoCarriesTheResidueRecord577530598/002/wt: worktree is DIRTY (untracked-only residue, no tracked change) — the run must clean its own scratch before it reports ready — entries: ?? final.out; the run's scratch was never cleaned (the outcome stands, nothing deleted)
[task-notify] task run closeout: t-stale → completed (child normal exit, wrapper writeback never landed)
[task-notify] task run closeout: t-run-close → completed (child normal exit, wrapper writeback never landed)
[task-notify] task run closeout t-run-unclaimed: no live claim evidence (claimed_by="" session_id="") — left open for the stale sweep
[task-notify] task run closeout t-run-live: no live claim evidence (claimed_by="hosta:1075552" session_id="") — left open for the stale sweep
FAIL
FAIL	github.com/nick/gopi/internal/tools	25.614s
FAIL
EXIT=1
```

### A3 — red lever B: the reaper measurement forced to 0

```
### MUTATION B: the reaper measurement forced to 0 (Rule 15: an undecidable premise must FAIL, never pass/skip)
$ sed -i 's/reaper, reason := testutil.ProcReaperOf()/reaper, reason := 0, "injected mutation: unmeasurable reaper"/' internal/tools/run_closeout_reap_window_test.go

testutil: suite "tools": scratch root /dev/shm is tmpfs (memory filesystem) (placement memory-base, 26.6 GiB free) — fresh ledgers under it are built on a memory filesystem, so their first opens pay no block device's journal commits
--- FAIL: TestRunCloseoutReapWatchesAPidItCannotMeasureYet (0.09s)
    run_closeout_reap_window_test.go:86: member, measured normally: pid 1078077 ppid=3100 pgrp=1078076 sid=1078076 wrapper="1078076" cwd="/dev/shm/suite-tools-run-39669844/TestRunCloseoutReapWatchesAPidItCannotMeasureYet1950232462/001"
    run_closeout_reap_window_test.go:103: the hole arm: this host's reaping ancestor could not be measured (injected mutation: unmeasurable reaper), so "the member has left the fixture's tree" is undecidable — the arm's premise cannot be established (Rule 15), and an undecidable premise is never a pass
--- FAIL: TestRunCloseoutSelectionReportsAHoleAndNotAMiss (0.07s)
    run_closeout_reap_window_test.go:198: the hole-vs-miss arm: this host's reaping ancestor could not be measured (injected mutation: unmeasurable reaper), so "the member has left the fixture's tree" is undecidable — the arm's premise cannot be established (Rule 15), and an undecidable premise is never a pass
--- FAIL: TestRunCloseoutSelectionDoesNotCountAnotherProcessAsItsOwnHole (0.10s)
    run_closeout_reap_window_test.go:324: the hole-scope guard: this host's reaping ancestor could not be measured (injected mutation: unmeasurable reaper), so "the member has left the fixture's tree" is undecidable — the arm's premise cannot be established (Rule 15), and an undecidable premise is never a pass
FAIL
FAIL	github.com/nick/gopi/internal/tools	1.602s
FAIL
EXIT=1
```

### A4 — family census verdict lines (`family-measure.out`, 106 PASS / 11 SKIP / 0 FAIL, EXIT=0)

```
41:    removal_audit_record_test.go:164: HEAVY TEST SKIPPED (set GOPI_HEAVY_TESTS=1 to run): TestFinishCleanupServerArmEmitsTheAuditRecord	disk-fixture: minutes of real device work (ledger/scratch fsync per fixture)
42:    removal_audit_record_test.go:164: HEAVY TEST SKIPPED (set GOPI_HEAVY_TESTS=1 to run): disk-fixture: minutes of real device work (ledger/scratch fsync per fixture)
43:--- SKIP: TestFinishCleanupServerArmEmitsTheAuditRecord (0.00s)
52:    task_finish_cleanup_holder_naming_test.go:112: orphan probe 1021908 was not reparented to init 1 within 20s
53:--- SKIP: TestHolderRefusalNamesAnOrphanAsAnOrphan (20.02s)
63:            task_finish_cleanup_holder_shared_session_test.go:432: shared-session fixture: holder 1023881 was not reparented to init 1 within 20s
72:            task_finish_cleanup_holder_shared_session_test.go:432: shared-session fixture: holder 1024678 was not reparented to init 1 within 20s
81:            task_finish_cleanup_holder_shared_session_test.go:432: shared-session fixture: holder 1027528 was not reparented to init 1 within 20s
90:            task_finish_cleanup_holder_shared_session_test.go:432: shared-session fixture: holder 1027976 was not reparented to init 1 within 20s
99:            task_finish_cleanup_holder_shared_session_test.go:432: shared-session fixture: holder 1028267 was not reparented to init 1 within 20s
110:--- SKIP: TestSharedSessionHolderProbeChild (0.00s)
162:    task_finish_cleanup_orphan_test.go:229: orphan fixture: holder 1041899 was not reparented to init 1 within 10s (ppid=3100 ok=true)
163:--- SKIP: TestOrphanCwdInheritorIsNotAHolder (10.36s)
166:    task_finish_cleanup_orphan_test.go:262: orphan fixture: holder 1049362 was not reparented to init 1 within 10s (ppid=3100 ok=true)
167:--- SKIP: TestOrphanCwdInheritorReleaseDoesNotReclaimLiveWork (10.39s)
170:    task_finish_cleanup_orphan_test.go:297: orphan fixture: holder 1055120 was not reparented to init 1 within 10s (ppid=3100 ok=true)
171:--- SKIP: TestCwdHolderWithoutALauncherRecordKeepsTheVeto (10.67s)
177:    task_finish_cleanup_orphan_tree_test.go:204: orphan tree fixture: launcher 1059738 was not reparented to init 1 within 10s (ppid=3100 ok=true)
178:--- SKIP: TestOrphanTreeDescendantIsReleasedWithItsAncestor (10.68s)
303:    task_finish_deliverysync_test.go:472: HEAVY TEST SKIPPED (set GOPI_HEAVY_TESTS=1 to run): TestFinishCleanupActionBudgetOutlivesTheLedgerRead	disk-fixture: minutes of real device work (ledger/scratch fsync per fixture)
304:    task_finish_deliverysync_test.go:472: HEAVY TEST SKIPPED (set GOPI_HEAVY_TESTS=1 to run): disk-fixture: minutes of real device work (ledger/scratch fsync per fixture)
305:--- SKIP: TestFinishCleanupActionBudgetOutlivesTheLedgerRead (0.00s)
327:    task_reclaim_own_release_selfmatch_test.go:292: the stand-in pid 1069785 never reparented to init on this host
328:--- SKIP: TestReclaimOwnFinishedWorktreesFreesItsOwnGroundAndKeepsAForeigns (3.08s)
330:    task_reclaim_own_release_selfmatch_test.go:390: the session-directory stand-in 1070013 never reparented to init
331:--- SKIP: TestReclaimOwnFinishedWorktreesKeepsAGroundAForeignCwdHolderStandsIn (10.06s)
333:    task_reclaim_self_orphan_test.go:158: the stand-in pid 1070114 never reparented to init on this host
334:--- SKIP: TestFinishCleanupSelfTreeSparesThirdParties (3.02s)
352:PASS
353:ok  	github.com/nick/gopi/internal/tools	200.528s
354:EXIT=0
```
