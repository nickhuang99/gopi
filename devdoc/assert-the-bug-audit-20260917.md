<!-- doc-class: normative -->
# Task Report: Assert-the-bug audit — where the suite pins the wrong specification

**Task:** assert-the-bug-audit (epic dispatched 2026-09-17; branch `feat/assert-the-bug-audit`).
**Host:** HP (client). Rule 28 layer: this is an **audit + test-level** change — no
system-layer production code was touched, no gate/guard/baseline weakened, no test mass-edited.

---

## 1. The incident this audit is about

A gopi **client** host finished tasks whose branches never reached the server, and nothing
reported a problem. Root cause (diagnosed 2026-09-16, HP→peer1, devdoc/peer-push-diagnosis-20260916.md,
memory records #1815/#1816/#1817): in the client close-out chain the **dirty-worktree guard ran
BEFORE the push**, so one untracked `.evid/` scratch file skipped the whole chain
(`internal/tools/task_finish_cleanup.go`: the dirty gate at `:1007-1010` returns `Skipped` for the
chain; the push is only reached later, at `:2028` inside `runFinishCleanupChainTarget`).

The guard is RIGHT for the destructive steps (③ `worktree remove --force`, ④ `branch -d`) and
WRONG for ② (an idempotent `git push <peer> <branch>:<branch>` that moves only a PEER ref).

**What made it stick is the test suite.** `internal/tools/task_finish_cleanup_test.go` asserted the
wrong specification:

| site | assertion |
|---|---|
| `:188-201` `TestFinishCleanupChainDirtyWorktree` | `if len(rep.Steps) != 0 && !onlyUntrackedResidueNotes(rep.Steps) { t.Errorf("no steps may run on a dirty worktree: %+v", …) }` — header comment literally says *"skip, NOTHING touched (worktree + branch + **no push**)"* |
| `:904-917` `TestServerRecycleDirtyWorktreeSkipped` | the same sentence, server/single-node arm |
| `:1758-1774` `TestFinishCleanupPlanKeepsEveryGuard` | the same claim on the dry-run path (*"the refused plan must leave the worktree alone"*) |

That is a **bundle**: one true claim (destructive steps must not run on a live bond) welded to a
false one (an idempotent push must not run either). The bundle is why moving the push above the
guard looks like a regression to whoever edits the chain next. The three sites were **owned by task
01a0aab8** and were deliberately not edited by this audit.

> **ALL THREE SPLIT (2026-09-19, epic client-push-rebase @1732dc81)** — the fix the cells were
> waiting on is the same change, so it landed with it rather than across two branches:
> `TestFinishCleanupChainDirtyWorktree` asserts the push ran (and the peer holds the tree) AND the
> removal did not; `TestServerRecycleDirtyWorktreeSkipped` is reworded to say its claim is about
> THAT arm's step set (it has no push step, so the negation is legitimate); and
> `TestFinishCleanupPlanKeepsEveryGuard` is split the same way on the plan path — the refusal plan
> predicts NO removal and STILL predicts the push. `devdoc/client-push-rebase-20260919.md` §"audit's
> three over-broad assertions" carries the evidence. The §3 correction note below is therefore fully
> discharged; the summary rows in §2a are kept as the audit's own record with this pointer.

### The defect SHAPE this audit generalised

> A **negative assertion over a set of steps** ("no step ran") whose message **names a behaviour
> with an action verb**. The action verb is the tell: it claims *nothing happened*, when the
> correct claim is *nothing destructive happened*. Correct only when **no member of the guarded
> set is idempotent/safe**.

That predicate is what §5 mechanises.

---

## 2. The audit table

Verdicts: **correct** (nothing is wrongly excluded) · **over-broad** (the guarded set contains a
safe/idempotent member) · **UNCERTAIN** (no evidence found either way — reason given, never a
guess). Every row below was checked against the production code path it names; no verdict is
inferred from the test's own prose.

### 2a. `internal/tools/task_finish_cleanup_test.go` — every negation over `rep.Steps`

| file:line | assertion (claim) | bundled claim | verdict | evidence |
|---|---|---|---|---|
| `task_finish_cleanup_test.go:188-201` | "no steps may run on a dirty worktree" | dirty ⇒ no push **and** no removal | **over-broad** | the chain's step ② is `finishCleanupPushVerb` (`:2028`), an idempotent push of a peer ref (`branch:branch`); the dirty guard exists for `worktree remove --force` (`:2065`) + `branch -d` (`:2127`). The test's own comment (`:189`) names the false half: *"no push"*. **01a0aab8** |
| `task_finish_cleanup_test.go:905-918` | "no steps may run on a dirty worktree" (server recycle) | dirty ⇒ nothing at all | **over-broad** | `finishCleanupServerRecycle` (`:821`) is chosen when `IsServerNode() \|\| finishCleanupServerTarget()==""` (`:807`) and has **no push step** — its only act is the worktree removal (`:937-944`). The negation is true of THAT arm, but the message is word-for-word the client arm's and imports the client arm's false half. UNCERTAIN *which* repair is owed (narrow the set vs. reword) — 01a0aab8 decides |
| `task_finish_cleanup_test.go:1758-1774` | "the refused plan must leave the worktree alone" | refusal ⇒ nothing at all | **over-broad** | third copy of the same claim on the dry-run path; `planChain()`/`runChain()` differ by one bool (`:1705`) so the plan pins the run's set. Not in the gate's hit list (no action verb in the message) — carried here because it is the same bundle. **01a0aab8** |
| `task_finish_cleanup_test.go:1249-1263` | "no step may run against a worktree in use" | live hold ⇒ nothing at all | **correct** | this arm's whole step set IS the destructive removal; the guard is `finishCleanupWorktreeInUse` before the `--force` removal (data-loss path). The client arm's counterpart `TestFinishCleanupChainStep3RefusesWorktreeInUse` (`:1305`) already asserts the **split** (`assertStep(rep,"push",true)` *then* the refused worktree step) — the split exists on this axis |
| `task_finish_cleanup_test.go:266-275` | `if rep2.Skipped && len(rep2.Steps)==0 { return }` | a clean re-run may be a silent pass | **correct** | post-completion idempotency: `rep2` is the *second* run over a fixture the first run already emptied. `return` is an early pass, not a totality claim |
| `task_finish_cleanup_test.go:1310` | `if len(rep.Steps) == 0 { t.Fatalf }` | positive (a step must be reported) | **correct** | guard-③ fixture sanity |
| `task_finish_cleanup_test.go:1915-1947` | `FinishCleanupReportSettled` table, incl. `"dirty bond is a refusal"` | refused ≠ settled | **correct** | classification only; asserts that a dirty refusal is *counted* as a refusal, which is exactly what the fixed chain must still do for ③/④ |

### 2b. `internal/tools/task_reclaim*` (the dead-holder reclaim arm)

| file:line | assertion | bundled claim | verdict | evidence |
|---|---|---|---|---|
| `task_reclaim.go:405-423` (guard ②) consumed by `zz_redproof_reclaim_promise_test.go:49` | "a DIRTY dead-holder worktree is refused" | dirty ⇒ nothing on that directory | **correct** | the reclaim arm has **no push step at all**; its whole act is the directory removal. Same guard, different set, different verdict — this is the audit's **control row** |
| `task_reclaim_self_test.go:179-191` | "a dirty directory must be refused BY NAME" | dirty ⇒ kept | **correct** | same reasoning; the test is about the *name* split (tracked vs untracked) |
| `worktree_refusal_arms_test.go:39-100` | `reclaimDirtyReason` table + `TestReclaimDispositionUntrackedResidueStillRefuses` | untracked-only is still a refusal | **correct** | refusal semantics unchanged, name split only; the test asserts the file that holds the tree is NAMED (Rule 15) |

### 2c. `repo_sweep` / `task_merged_cleanup` / merger

| file:line | assertion | bundled claim | verdict | evidence |
|---|---|---|---|---|
| `task_merged_cleanup_test.go:193-212` | "merged branch + DIRTY worktree → skipped untouched" | dirty ⇒ not removed | **correct** | the sweep's act is `worktree remove` (`task_merged_cleanup.go:431` is its only dirty gate); **no push step exists in it** — delivery belongs to the pair lane / owner chain |
| `repo_sweep.go:328-384` (`repoSweepUnprotectedBranches`) consumed by `repo_sweep_test.go:93-119` | "feat/bond must NOT have been merged (worktree = active bond work)" | checked-out ⇒ never merged | **correct** | the merger merges *branches*, never touches the worktree; there is no idempotent member to exclude — and `TestSweepDryRunChangesNothing` (`worktree_sweep_test.go:439`) pins the same posture for plan mode |
| `claim_holder_freshness_test.go:934-941`, `worktree_dirty_split_test.go:120-131` | "the operator line must not promise a release" | a false promise is worse than a refusal | **correct** | these *fixed* the inverse bug (a refusal advertised as reclaimable) — the audit's precedent that the suite does get corrected when the bundle is spotted |
| `worktree_sweep_test.go:419-427` | "prune ran … want exactly ONE per repo" | exact-count (positive) | **correct** | not a negation of an action set |

### 2d. `internal/agent/taskregister.go` — the close-out hook

| site | statement | verdict | evidence |
|---|---|---|---|
| `taskregister.go:408-431` (end hook: terminal push → `TaskFinishCleanupSessionID` → CWD release) | "finish-cleanup: fail-soft, reported, never fatal" | **correct as written, untested in its failure half** | the hook does not *assert* the chain's step set; it logs `finishCleanupRepSummary(rep)`. But `internal/agent/taskregister_test.go` (15 tests) has **zero** coverage of this seam: `grep -n "dirty\|finish-cleanup\|Push" taskregister_test.go` → no hits. The hook is where the client's only delivery attempt lives, so a wrong `rep` interpretation there has no test either way |

### 2e. Every `Skipped = true` early-return surface (the chain's 26 sites)

Walked exhaustively (`internal/tools/task_finish_cleanup.go`, 26 sites; plus `rebuild.go:110`).
A site matters only when the **guarded set contains a safe step** — for this chain, every site
except the dirty gate sits *before* the push, so each one is a potential stranded-branch path if
its skip reason is neither reportable nor settled:

| line | reason | tests naming it | verdict |
|---|---|---|---|
| `:749` | `disabled (GOPI_FINISH_CLEANUP)` | kill-switch tests | correct (opt-out) |
| `:767` | `no such task row` | `cov_finish_cleanup_test.go` | correct |
| `:773` | `status=… not cleanable` | **none** | correct behaviour, **no test** |
| `:781` | `no deliverable` | many | correct |
| `:787` | `deliverable has no repo` | `cov_finish_cleanup_test.go` | correct |
| `:833`, `:970`, `:1938` | `resolve HEAD branch: …` | **none** | correct behaviour, **no test** |
| `:840`, `:977`, `:1945` | `branch … is exempt` | 3+ files | correct |
| `:863`, `:913`, `:923`, `:937`, `:1975` | ownership / live-owner / in-use gates | `task_finish_cleanup_test.go` | correct (splits already asserted) |
| `internal/tools/task_finish_cleanup.go:1515` | `no linked worktree …` | `task_finish_cleanup_test.go` | correct (settled) |
| `internal/tools/task_finish_cleanup.go:846` | `git status: …` (unreadable status) | **none by name** | correct behaviour, **no test** |
| `:820` | **the dirty gate** (the ONE reading of its three arms) | 3 tests (§2a) | **over-broad** — the bundled claim |
| `internal/tools/task_finish_cleanup.go:1533` | `worktree unreadable: …` | **none** | correct behaviour, **no test** |
| `:1017` | `no sync_peers resolved — no server to push to` | **none** by this spelling (the reason is asserted by substring elsewhere) | correct (single-node) |
| `rebuild.go:110` | `Skipped` (no build contract) | `rebuild_test.go:73` | correct (Rule 12: no entry = no build) |

**Uncovered reason strings (identical name in no `_test.go`):** `not cleanable`, `resolve HEAD
branch`, `worktree unreadable`, `no sync_peers resolved`. These are *named-but-untested* refusals:
cheap to add, and each is a path where a finished row is refused. Listed, not silently accepted.

### 2f. Guard reachability (the unit-gap half)

`finishCleanupOwnerDeclaredReason` and `mergedCleanupDirtyReason` have **zero test mentions**
(the first is the ownership-disagreement gate whose *effect* is tested via
`TestServerRecycleBranchWorktreeResolver` / the mismatch tests; the second is the merged sweep's
dirty reader, covered only through `TestMergedCleanupDirtyWorktreeSkipped`'s end-to-end path).
Everything else in the chain's guard family is named by at least one test (`finishCleanupOwnerRowGate`,
`finishCleanupLiveOwnerGate`, `finishCleanupWorktreeInUse`, `finishCleanupBranchExempt`,
`finishCleanupServerTarget`, `finishCleanupMissingDirReason`, `finishCleanupForeignOwnerReason`,
`finishCleanupOwnerStaleMarker`, `finishCleanupWorktreeOwnerRows`, `TaskFinishCleanupSkipIsLiveOwner`,
`finishCleanupBranchForceDeleteReason`, `finishCleanupWorktreeDirtyReason`, `reclaimDirtyReason`).

---

## 3. Correction note for task 01a0aab8 (which half is wrong)

**DISCHARGED 2026-09-19** — all four items landed in the client-push-rebase epic
(`devdoc/client-push-rebase-20260919.md`, `feat/client-push-rebase` @1732dc81), which is the change
the cells were waiting on: the ordering fix and the assertion split are one commit, so the items are
not left split across two branches. Item 4's red evidence was re-measured against the pre-fix
production files (`step push missing from report`), and the skipped cell was un-skipped as its green
proof. The note is kept verbatim below as the audit's instruction record.

01a0aab8 owns the fix; this is the audit's instruction to it, derived from §2a. **Split the set,
never the tolerance:**

1. **`TestFinishCleanupChainDirtyWorktree` (:188)** — split the assertion in two:
   - *destructive half (already true, keep)*: the worktree directory SURVIVES and the local branch
     still resolves; no `worktree` step ran with `OK:true`.
   - *delivery half (currently false, must be asserted)*: the push step ran **and** the peer holds
     the ref — assert with `git -C <server> rev-parse --verify <branch>` (a struct check cannot
     prove "the peer has it"; the fixture already has a real bare server, and
     `TestFinishCleanupChainCleanWorktree` uses exactly this probe).
   Do **not** relax the dirty guard itself: `finishCleanupWorktreeDirtyReason` stays untouched.
2. **`TestServerRecycleDirtyWorktreeSkipped` (:904)** — this arm has no push step, so either
   narrow the claim to "the recycle's only step (the removal) did not run" or reword it to name
   the arm. The current sentence teaches the client rule inside a server test.
3. **`TestFinishCleanupPlanKeepsEveryGuard` (:1758)** — same split for the plan path: a refusal
   plan must predict **no removal**, and (after the fix) **still predict the push**.
4. **The red evidence for the fix already exists** and is reproduced in §7.3: with the current
   chain, the correct cell's assertion fails with `the finished branch must be DELIVERED even from
   a dirty worktree … exit status 128`, the report showing `skipped=true, steps=[worktree OK:false]`.
5. The chain's *fix* is the system layer (**Rule 28 → peer1**); the test split above is the
   feature-host half 01a0aab8 already carries.

---

## 4. The unit-test gaps the incident exposed

### 4.1 `IsServerNode()` had zero direct assertions

`internal/tools/memory_sync_delta.go:786`. It decides which of the two close-out arms runs
(`task_finish_cleanup.go:807`: `IsServerNode() || finishCleanupServerTarget()==""`), so it is the
predicate that decides whether a client pushes at all.

**Reachability vs. correctness.** All four previous mentions of the name in test files are
**comments** (`memory_sync_bidir_e2e_test.go:141,234`, `memory_sync_delta_test.go:240`,
`cov_finish_cleanup_test.go:196`). Measured with the project's own metric:

```
$ python3 scripts/func_coverage.py --pkg internal/tools
package                                     funcs    cov    pct  tests
internal/tools                                336    250  74.4%

$ python3 scripts/func_coverage.py --uncovered --pkg internal/tools | grep -c '^    - '
86
$ python3 scripts/func_coverage.py --uncovered --pkg internal/tools | grep -n 'IsServerNode'
IsServerNode: NOT reported as uncovered (name appears in test files -> counted covered)
source anchor: internal/tools/memory_sync_delta.go:786
```

**This is the metric's own blind spot, measured:** `IsServerNode` is counted *covered* because the
identifier appears in test files — in comments. 336 functions, 250 "covered", 86 uncovered, and the
predicate at the centre of the incident is not among them and is not actually asserted. The
`--uncovered` line for it is therefore: **"absent from the list — a false positive of the
name-mention metric"** (the honest finding; the acceptance asked for the line, and the line does
not exist because the metric cannot see the difference).

**The four decision paths, now each asserted directly** (`internal/tools/is_server_node_test.go`):

| path | source | test |
|---|---|---|
| ① role declared (server/client) | `ResolveNodeRole()` non-empty → `role==server` (`:790`) | `TestIsServerNodeRoleDeclaredServer`, `…Client` (2 sub-cases) |
| ② legacy hostname fallback | own hostname among peers → client (`:800-804`) | `TestIsServerNodeLegacyHostnameAmongPeers` |
| ③ fail-safe default (N1) | no role + hostname not among peers, or `os.Hostname` failed → **client** (`:797-799`,`:806`) | `TestIsServerNodeHostnameNotAmongPeersIsFailSafeClient` |
| ④ unreadable settings | `ResolveSyncPeers` error → **client**, never a panic (`:802-804`) | `TestIsServerNodeUnreadableSettingsIsClient` (fixture: settings path is a **directory**; a *missing* settings file is a valid empty config and does **not** reach this arm — recorded in the test) |
| plus: unknown role spelling ⇒ undeclared | `node_role.go:71-75` | `TestIsServerNodeUnknownRoleSpellingIsUndeclared` |
| plus: memoisation stability | `ResolveNodeRole` caches per process | `TestIsServerNodeRoleDeclaredIsStableAcrossCalls` |

### 4.2 ROLE × STATE matrix

|  | **clean** | **dirty** |
|---|---|---|
| **client** (peer declared) | `TestFinishCleanupChainCleanWorktree` (`:161`) — push ✓ remove ✓ `-d` ✓ | **COVERED (since 2026-09-19).** `TestFinishCleanupClientDirtyStillDelivers` (`is_server_node_test.go`), previously skipped with the owner named and RED against master (§7.3), is now un-skipped and green: the destructive steps stay refused AND the idempotent push delivers. The fix landed in the client-push-rebase epic (`devdoc/client-push-rebase-20260919.md`, `feat/client-push-rebase` @1732dc81) |
| **server / single-node** | `TestCovFinishCleanupLinkedWorktreeRemovedBranchKept`, `TestServerRecycleRemovesWorktreeKeepsBranch` — removal only, branch kept | `TestServerRecycleDirtyWorktreeSkipped` (`:904`) — nothing runs (true for this arm: no push step exists) |

Also present outside the chain: client+dirty **destructive** half is covered by
`cmd/agent/cli_resume_cleanup_e2e_test.go:196-204` ("a DIRTY worktree must survive the resume
cleanup" + the reason is reported). Missing there is the same delivery half — no test anywhere
asserts that a dirty tree still gets its branch to the peer.

The missed cell is **enumerated, not prose**: `TestFinishCleanupClientDirtyStillDelivers` exists as
a `t.Skip` carrying the reason and the owner task, with its assertion body written out
(unreachable while skipped) so un-skipping is a one-line change. It prints in every
`go test ./internal/tools/` run instead of passing silently.

---

## 5. The audit-method repair (what is mechanised, what is handed to review)

**Mechanised** — `internal/tools/assertion_totality_audit_test.go` + `assertion_totality_allow_test.go`:

- `TestAssertionTotalityAuditGate` walks `internal/**` + `cmd/**` test files, parses them with
  `go/parser` (stdlib, zero deps), and reports every **negative-totality assertion**:
  an `if` whose condition asserts a zero count (`len(x) != 0`, `!(len(x) == 0)`, or either of
  those `&&`-ed with more — the incident's own shape) **and** whose failure message both negates
  (`no|not|never|nothing|none|without|cannot`) **and** names an action verb
  (`run|delete|remove|push|merge|write|kill|prune|commit|apply|exec|touch|fire`).
- Every hit must be **classified** in `auditTotalityAllow["path:TestFunc"]` (key = file+**function**,
  never a line number, so sibling edits and line shifts do not invalidate the audit) with a
  verdict and the evidence. An unclassified hit **fails the build** and prints the exact key to
  add — so a future agent writing the same over-broad shorthand meets the question *while writing
  it*, which is the repeatable check the epic asked for.
- Short reasons (<25 chars) fail too (anti rubber-stamp), an inline escape
  `// audit-allow: <reason>` exists for a site where the totality is correct and obvious, and the
  gate **logs every classified site with its reason** (a data report, never silence).
- `TestAssertionTotalityAuditGateRedProof` is the predicate's own red proof: 7 synthetic sources
  pin what fires and — as importantly — what does not (a count of one is *positive polarity* and
  out of scope; a negated count with an inert message is not a claim).
- `TestAssertionTotalityAuditGateClassifiesTheLiveTree` is the read-floor: the scan must be
  non-empty **and** must contain the incident's own site, so a broken glob or a rotted predicate
  fails instead of passing vacuously.

**Handed to human/agent review** (and why):

- The audit table itself (§2). The predicate finds assertions of one *shape*; whether a given
  expected value is *right* is not mechanical. Rows carry evidence, and UNCERTAIN rows are
  labelled as such.
- The **positive** half of the same bug family: a positive assertion bundles two claims just as
  easily (`assertStep(rep,"push",true)` says "the push ran" — it cannot say "and the peer holds
  the ref"; only a peer-side probe can). A reviewer's checklist item:
  *when a test asserts a delivery/merge/push happened, ask the peer, not the local report.*
- The **guard-set question**, which is the actual decision: *does the guarded set contain a
  safe/idempotent member?* — §2b (reclaim) and §2c (merged cleanup) are the two controls where the
  same dirty guard is correct because the set is destructive-only. That judgement is a review
  activity; the gate's job is to force it to be written down.

**Why the split (mechanise this, review that):** the gate can be *deterministic* only over
syntax and vocabulary; the verdict needs the production step set, which is a reading task. Putting
the reading task in the gate would mean guessing — the failure mode that produced three
over-broad assertions in the first place.

**Placement note (Rule 28):** the gate lives in `internal/tools` as a **test-only** file, not in
`internal/standards` (the system layer, peer1's). Promotion to a standards gate
(`RuleAssertionTotality`) is a follow-up for that host; the allow-map is already shaped for the
baseline/escape convention the standards gates use.

---

## 6. Limits of the method (honest)

1. **It finds a shape, not a wrong value.** The incident's family is "a bundle that should be a
   split"; the gate only sees bundles expressed as *negations over a collection with an action
   verb in the message*. A bundle written positively (`assertStep(push, true)` + `assertStep(worktree,
   false)` in one test that should be two tests) is invisible to it.
2. **The message vocabulary is a heuristic.** A totality claim worded without an action verb
   (`"the sweep did nothing"`) is missed; that is why the gate's negation+verb pair was chosen
   (precision over recall) and why the allow-map is *documented*, not merely silent. The measured
   precision on this tree: 18 hits, 3 in the incident's own family, 15 classified correct/UNCERTAIN
   after reading each — no obvious false positive among them, but the vocabulary was tuned once
   (the first draft matched positive `!= 1` claims and was narrowed).
3. **Function-granularity keys.** A second totality assertion added *inside* an already-classified
   function is silently covered by that key. Accepted trade-off: line keys break on every unrelated
   edit, and a sibling branch (01a0aab8) is editing exactly these tests. Mitigation: the data
   report prints each hit's current `path:line`+message, so a re-read after any edit shows the drift.
4. **Comments are invisible.** The incident's false half was in a *comment* (`:189 "…and no push"`).
   No predicate reads prose. Machine-checkable proxies: the `Skipped = true` inventory (§2e) and
   the covered-reason list.
5. **`func_coverage.py` cannot distinguish a mention from an assertion** (§4.1) — the gate above
   compensates for one function family only; the metric's blind spot stays for the rest.
6. **The audit is a snapshot.** It classifies the tests as of `master 69975030`; the incident
   family is intentionally left RED (01a0aab8 owns the fix), so re-running the gate after that
   branch lands will still pass (same keys) while the *reasons* need re-reading — stated in the map
   header.

---

## 7. Verification evidence

### 7.1 Baseline vs. branch — the same FAILs, measured on both trees

This branch's base is `69975030`; a **separate pristine worktree**
(`/home/user/workspace/gopi-before-master`, detached at `17cec86b`'s ancestor `69975030`) was run
with the identical commands, so no failure below is attributed to the branch without a baseline row.

```
$ git -C /home/user/workspace/gopi-before-master log --oneline -1
69975030 Merge branch 'feat/merger-status-next-tick'          # same commit as the branch base

BASELINE (untouched master)                       BRANCH (this audit)
internal/tools                                    internal/tools
  FAIL TestSuiteScratchRootIsMemoryBacked           FAIL TestSuiteScratchRootIsMemoryBacked
  FAIL TestScratchRootFilesystemClassification      FAIL TestScratchRootFilesystemClassification
  FAIL TestMemoryBaseOutOfPolicyRegistration…       FAIL TestMemoryBaseOutOfPolicyRegistration…
internal/agent                                    internal/agent
  FAIL TestSuiteTempRootIsPinned                    FAIL TestSuiteTempRootIsPinned
  FAIL TestHippocampusDocAnchorsResolve             (passes on the branch — order-dependent)
internal/stylegate                                internal/stylegate
  FAIL TestStyleGate                                FAIL TestStyleGate
```

**Root cause of the shared set** (measured, not assumed): `/home` is on `ext4` backed by
`/dev/nvme0n1p7` (`findmnt -no FSTYPE,SOURCE /home` → `ext4 /dev/nvme0n1p7`), and this host's
scratch policy wants a memory-backed root; the block-backed scratch root fails the filesystem
classification assertions. The `TestStyleGate` FAIL is likewise present on pristine master — its
own listing names `internal/tools/write_test.go:222` and `testsuite/web_test.go:93`, neither of
which this branch touches.

**Conclusion: the branch introduces ZERO new package-level failures.** Every FAIL above is
reproduced on the untouched baseline; the branch's three new files are green in isolation (§7.2).
Set difference = ∅ (branch-only failures) and the sole asymmetry is a pre-existing
order-dependent test that is *less* flaky on the branch, not more.

### 7.2 The new tests, green in isolation

```
$ go test ./internal/tools/ -run 'TestAssertionTotalityAudit' -count=1
ok  	github.com/the operator/gopi/internal/tools	1.660s

$ go test ./internal/tools/ -run 'TestIsServerNode' -count=1
ok  	github.com/the operator/gopi/internal/tools	1.090s        # 7 tests / 8 cases, incl. arm ④

$ go test ./internal/tools/ -run 'TestFinishCleanupClientDirtyStillDelivers' -count=1 -v
--- SKIP: TestFinishCleanupClientDirtyStillDelivers (0.00s)   # the enumerated missing cell
```

### 7.3 Red evidence for the missing ROLE×STATE cell (client + dirty)

Run with the skip disabled (temporary file, deleted before the commit):

```
$ go test ./internal/tools/ -run 'TestZZTmpClientDirtyCellRed' -count=1
--- FAIL: TestZZTmpClientDirtyCellRed (0.16s)
    the finished branch must be DELIVERED even from a dirty worktree (push is idempotent):
    exit status 128 fatal: Needed a single revision

    report: skipped=true
      reason="worktree dirty (live bond protected) — carries ONLY UNTRACKED files …: ?? wip.txt"
      steps=[{Step:worktree Target:…/wt OK:false Detail:untracked-only residue in the worktree of a finished run …}]
FAIL	github.com/the operator/gopi/internal/tools	1.240s
```
The peer arm of the assertion (branch survives locally, worktree survives) **passes**; the delivery
arm fails — i.e. the false half is precisely "an idempotent push must not run". The same body is
stored in the skipped test, so un-skipping it after 01a0aab8 is the green proof.

> **The green proof, measured (2026-09-19, `feat/client-push-rebase` @1732dc81).**
> The cell was un-skipped and the same body asserted directly:
> ```
> $ go test ./internal/tools/ -run 'TestFinishCleanupClientDirtyStillDelivers|TestClientPush|TestClientDelivery' -count=1
> ok  github.com/the operator/gopi/internal/tools  7.078s
> ```
> The recorded red run above is the pre-fix observation, and re-running the new
> contract tests against the pre-fix production files reproduces it (`step push
> missing from report` — the report says `skipped=true` and shows NO push step).
> See `devdoc/client-push-rebase-20260919.md` for the ordering contract.

### 7.4 The gate is not green-blind (its own red proof)

```
$ go test ./internal/tools/ -run 'TestAssertionTotalityAuditGateRedProof' -count=1 -v
--- PASS: TestAssertionTotalityAuditGateRedProof (0.00s)   # 7 sub-cases, positive polarity excluded
--- PASS: TestAssertionTotalityAuditGateClassifiesTheLiveTree (0.44s)   # sees the incident's own site
```
Confirmed negative control: with `auditTotalityAllow` emptied the gate fails and prints all 18
keys with the "UNCLASSIFIED" instruction (observed during development).

### 7.5 Gates

```
$ make build        # CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/  → BUILD_EXIT=0
$ go vet ./...      # VET_EXIT=0 — clean tree-wide
$ gofmt -l <the 3 new files>   # empty
```
Package suites carry the pre-existing host-geometry FAILs enumerated in §7.1 — on BOTH trees,
with an empty branch-only set. Green in isolation: see §7.2.

---

## 8. Not done (honest)

- **The three known assertions are NOT fixed** — deliberately: task 01a0aab8 owns them. §3 is the
  written instruction instead.
- **`TestFinishCleanupClientDirtyStillDelivers` is SKIPPED, not green.** It is the missing cell
  made visible; turning it green requires the chain fix (system layer, Rule 28 → peer1 /
  01a0aab8). Its red run is recorded (§7.3).
- **The gate is not promoted to `internal/standards`** (system layer). Follow-up, with the
  allow-map already in the baseline/escape shape.
- **The positive-assertion half of the family is a review item only** (no mechanised check).
- **Untested skip reasons** (`not cleanable`, `resolve HEAD branch`, `worktree unreadable`,
  `no sync_peers resolved`) are listed, not covered — each is a small, dispatchable addition.
- **`internal/agent/taskregister_test.go` has no coverage of the close-out seam** (§2d) — named
  here as a gap; fixing it belongs with the close-out work, not with this audit.
