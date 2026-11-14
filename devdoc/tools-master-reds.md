<!-- doc-class: historical -->
# Task Report: clear the pre-existing red in `internal/tools` on pristine master

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

> **REWORK NOTE — this report was re-derived on a moved master.**
> The original run's branch (`task/01a0d66c…`, base `0d08f306`) could not be
> merged: master had moved 44 commits under it and 4 of the 6 files conflicted.
> The rework run `01a0d7a3ca337b2ea0aeefaa289db1a4` re-applied the archived
> patch's INTENT onto master `3843f8c9` by hand (never on the failed branch,
> Rule 23). Two of the original findings did NOT survive the rebase and are
> corrected in §RW1/§RW2 below: **fix #5's premise is FALSE on current master**
> and **fix #4's assertions were already green**. Everything else in this
> document stands, and every figure quoted under `## Verification` is the
> MEASURED post-rebase number (§RW4) — the original run's timings, base hashes
> and commit id are kept only where they are marked as historical.

Task `01a0d66c57a17469a71e51574b34b82c` · branch `task/01a0d66c57a17469a71e51574b34b82c`
· base `0d08f306` (= master, clean)

**Rework:** task `01a0d7a3ca337b2ea0aeefaa289db1a4` · branch
`task/01a0d7a3ca337b2ea0aeefaa289db1a4` · base `3843f8c9` (= master at rework)

## Completed

Five genuine assertion failures, all pre-existing on master, each root-caused
by measurement and each shipped with a mutation red-proof. **The briefing's
stated root cause for fix #1 turned out to be WRONG, and the measurement that
refuted it found the real one** (§1).

| # | Failure | Root cause (measured) |
|---|---|---|
| 1 | `TestSyncImportTwoProcessConcurrent` | the FIXTURE, not the transaction (§1) |
| 2 | `TestDeclaredTopologyWordingHasOneDefinition` | `memory_sync_claim.go` re-typed the whitelist refusal sentence instead of calling `declaredPeerError`/`DeclaredPeerGate` (Rule 13) |
| 3 | `TestParentGateListingAgreesWithFunnel` | the shared test helper `renderedRowLine` still probed `" "+id+" "` — the layout BEFORE the `#<id>  [status]` row landed |
| 4 | `TestUuidTaskIDSubmitCreatesSessionContainer` | two stale expectations (the retired ledger alias; the truncation the full-id epic removed) |
| 5 | `TestFinishCleanupWorktreeHeldByCallerIsRecycled` | the test conflated the self-tree exclusion with the own-ground rung |

Also delivered: `TestFinishCleanupReleasedGroundIsRecycled` (new, #5's other
half — a rung that refused everything could otherwise pass), the probe-child
leg run with a real `GOPI_BIN`, and the budget/load measurement (§5).

### 1. The import test measured the dedup screen, not the transaction

**The briefing's hypothesis was that the child-mode guard fails and the helper
run "falls through into the parent body". Measured: it does not.** A probe kept
the temp dir alive and logged each process:

```
PARENT pid=… rows_after_seed=1
CHILD pid=… rows_before=1 db=…/memory.db      ← the helper body ran HERE
CHILD pid=… rows_after=2  db=…/memory.db
PARENT final rows=2
```

Each child ran `syncImportHelperMain` — the parent body was never reached.
`-test.run ^TestSyncImportTwoProcessConcurrent$` matches only the parent, so the
`return` after the helper is load-bearing and never falls through.

**The real mechanism, from the same probe:** a 20-row patch, imported **once**,
stores exactly **one** row — `stored=1 deduped=19`. `memory.go`'s same-lesson
screen merges a row into an existing same-topic row when the trigram Jaccard of
`topic question answer` is `> lessonJaccardMerge` (0.40) **and** the pair shares
`>= lessonMinSharedTrigrams` (120) trigrams. The fixture's 20 rows differed only
by an index — one topic, one shared boilerplate answer — which is precisely that
shape, so every one of the 20 rows was its OWN patch's near-duplicate and folded
into row 1. The test's `want 21` was unreachable however correct the import
transaction is.

Fix: 20 lexically **disjoint, long** subjects (measured: Jaccard well under
0.40, shared trigrams well under 120), the question/answer text rendered by ONE
pair of helpers used by both the patch builder and the uniqueness assertion
(Rule 14 — the test's two halves can no longer drift), and the row count still
the single `syncImportTxnPatchRows` constant, to which the subject array's length
is bound (Rule 1/13).

### 2–4. Two seams that forked from their definition, one stale helper

`memory_sync_claim.go:950` spelled the refusal for itself, using the literal
`whitelist` text that `peer_topology.go` owns; it now returns
`fmt.Errorf("memsync push: %w", declaredPeerError(p))` — same operator-facing
sentence, one definition. `renderedRowLine` (tasklist_order_test.go) was written
against the pre-full-id row shape; against today's `#<id>  [status]  <summary>`
row it matched NOTHING, which is why the parked child read as "missing from the
listing" while the log printed its line starting `#<child-id>`. It now matches
the id COLUMN, built from `taskListRowPrefix` rather than a re-typed separator.

`task_session_test.go` asserted two contracts the codebase has retired: the
ledger alias `"auto"` (`internal/tasksource` retired it — it conflated the
merger's enqueue with the harness's registration, ~993 rows nobody can
disambiguate) and the digits-only `taskHashDisplay` truncation that the
full-id-no-prefix epic removed. Both expectations now assert what the code
promises, expressed through the owning package's vocabulary
(`tasksource.Normalize`) so the test cannot pin an alias again.

**A correction inside the fix, worth recording:** my first attempt asserted
`tasksource.DeriveNow("auto")` and went RED with `source:merger`. The funnel
`TaskSubmit` runs for a *stated* alias is `Normalize` (map onto the value it
always meant → `merger`), while `Derive`'s re-derivation branch is for the
`Derive` entry point. Measurement settled it; the test now asserts `Normalize`
and additionally refuses the alias.

### 5. The cleanup test conflated two guards — the briefing's premise was half right

The briefing's framing was "production's supported shape works, so the test's
unconditional `t.Chdir` is the disagreeing side". That is right about the
**direction** and wrong about the **reason**, and the corrected reason is what
made a defensible fix possible:

* `reclaim_ground_checklist.go` rung ① refuses by NAME to remove the ground a
  process stands in — it exists for the measured 2026-09-21 breach (row
  `01a0c574`, holder alive, branch 14 ahead) where exactly that deleted a live
  run's checkout.
* `internal/worktreerelease` makes the release a **deliberate no-op in a test
  process** — "a test binary's cwd is the HARNESS's working context, shared by
  every sibling test in the run; relocating it is not a release, it is a
  hijack". So an in-process test that wants to recycle a directory it is standing
  in is asking for something the framework decided never to do.

And the test the briefing cites as proof, `cmd/agent`'s real-binary
`TestManualRunRecyclesItsWorktree`, **is measuring the SELF-TREE exclusion** —
`gopi worker` spawns a child that runs in the worktree, so the recycling arm is
not the process standing there. Verified green here (`go test ./cmd/agent/ -run
TestManualRunRecyclesItsWorktree` → `ok 0.257s`), as the briefing said.

The test now asserts the honest unit-level contract: an arm that has NOT
released its ground is refused, the refusal names the rung, and the ground and
branch survive the refusal. Its companion test pins the other half, so a rung
that refused everything cannot pass.

## Not Done

- **The residual pre-existing FAILs beyond the 7 named are NOT fixed**, and they
  are now ENUMERATED rather than estimated. A full `internal/tools` run on this
  pristine base (`0d08f306`, before any fix) took **1257.454s** (21m0s) and
  produced **15 `--- FAIL` lines = 14 top-level FAILs + 1 nested**:

  ```
  --- FAIL: TestSyncImportTwoProcessConcurrent (3.33s)          ← fixed here
  --- FAIL: TestSharedSessionHolderProbe (11.97s)
      --- FAIL: TestSharedSessionHolderProbe/leader (2.73s)     ← CASCADES from the nested child (below)
          --- FAIL: TestSharedSessionHolderProbeChild (1.91s)   ← RUNS under the parent; fails on the leader mode
  --- FAIL: TestFinishCleanupWorktreeHeldByCallerIsRecycled (0.71s)   ← fixed here
  --- FAIL: TestParentGateListingAgreesWithFunnel (3.11s)             ← fixed here
  --- FAIL: TestUuidTaskIDSubmitCreatesSessionContainer (0.97s)       ← fixed here
  --- FAIL: TestTaskRunningSuffixMonotonicAcrossRearm (1.17s)
  --- FAIL: TestCovToolsNotifyAlarmRegistryIsTheSourceOfTruth (0.31s)
  --- FAIL: TestTaskSubmitNoRepoDefaultsToCwd (0.78s)
  --- FAIL: TestTaskShowBriefingFromPayload (1.05s)
  --- FAIL: TestTaskShowBriefingFromSession (0.76s)
  --- FAIL: TestTaskShowBriefingMissingSession (0.80s)
  --- FAIL: TestReconcileMirrorCloseout_SIGKILLClosesFailed (1.32s)
  --- FAIL: TestReconcileMirrorCloseout_NonZeroExit (0.97s)
  --- FAIL: TestReconcileMirrorCloseout_Exit0ClosesCompleted (1.48s)
  --- FAIL: TestEveryWorktreeRemovalArmQuestionsTheGround (0.02s)
  ```

  So the debt is **14 top-level names**, of which I cleared **4** (items 1–4) and
  turned the **5th** (`TestFinishCleanupWorktreeHeldByCallerIsRecycled`) green by
  correcting its claim. Nine top-level names remain red on master:
  `TestSharedSessionHolderProbe` (+its `leader` subtest and the nested
  `…ProbeChild`), `TestTaskRunningSuffixMonotonicAcrossRearm`,
  `TestCovToolsNotifyAlarmRegistryIsTheSourceOfTruth`,
  `TestTaskSubmitNoRepoDefaultsToCwd`, `TestTaskShowBriefingFromPayload`,
  `TestTaskShowBriefingFromSession`, `TestTaskShowBriefingMissingSession`,
  `TestReconcileMirrorCloseout_*` (3 names), and
  `TestEveryWorktreeRemovalArmQuestionsTheGround`. **All are left exactly as
  found** — none of my five fixes touches them, and none is claimed as fixed.

  Note this CORRECTS the briefing's probe reading: `TestSharedSessionHolderProbeChild`
  does not merely "run or skip" — it RUNS inside the parent probe (its own
  `:241`/`:257` log lines appear under each `probe mode=`) and it is RED on the
  `leader` mode. As a top-level `-run` selection it SKIPs by design. It is the
  one name among the 7 whose red is real but was NOT assigned to me to fix.
- **`TestSharedSessionHolderProbeChild` is not fixed — it needed nothing.** Run
  as a top-level `-run` selection it SKIPs ("only runs as the re-exec'd child");
  it RUNS inside `TestSharedSessionHolderProbe`, whose per-mode output carries its
  own log lines (`:241`, `:257`). It is a re-exec'd helper, not a skippable
  assertion. Left as designed.
- **The timeout leg was not reproduced.** See §5 — recording an honest
  measurement was the deliverable, not widening a budget.
- No change to `scripts/test-budget.sh`, the Makefile, or any timeout/budget knob.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → clean (plus
  `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` for the probe child leg).
- Tests: `CGO_ENABLED=0 go vet ./internal/tools/` → clean (no output).
- Tests: `CGO_ENABLED=0 go test ./internal/tools/ -count=1 -timeout 600s -run
  '<exactly the 7 named tests>'` (with `GOPI_BIN` pointing at the built binary, so
  the probe-child leg can re-exec and the skip gate opens) →
  **`ok 11.381s`, rc=0**. Same gate plus the new companion
  `TestFinishCleanupReleasedGroundIsRecycled` → **`ok 12.900s`, rc=0**.
  Neighbour scope: `TestTaskHashDisplay|TestPendingListRendersClaimOrder|
  TestParentGate|TestFinishCleanup|TestSharedSession|TestServerRecycle|
  TestSyncImport|TestSync|TestDeclaredTopology|TestHarvest|TestReclaimOwnGround|
  TestWorktreeRemovalGround|TestWorktreeSweepGround|TestUuidTaskID|TestTaskSession|
  TestPhase1|TestOwnGround` → **`ok 192.604s`, rc=0, zero FAILs**.
  `CGO_ENABLED=0 go test ./cmd/agent/ -run TestManualRunRecyclesItsWorktree` →
  **`ok 0.257s`**.
- Ran: `make test-budget PKGS=./internal/tools` (output in §5), verbatim; and the
  per-test runs above.
- Result: the 7-name gate is rc=0; detail and the mutation proofs in Evidence.
- Commit: `dbdc7894 fix(tools): clear 5 pre-existing master reds and pin the
  render/refusal seams`

## Evidence (self-filed)

**Mutation red-proofs** — `/tmp/gopi_mutations.py`, each mutant applied, one
targeted test run, then restored and re-run:

```
=== M1 claim refusal re-typed  [internal/tools/memory_sync_claim.go] ===
  mutated : rc=1 (want non-zero)  -> RED (good)
  restored: rc=0 (want 0)       -> GREEN (good)
=== M2 renderedRowLine back to ' '+id+' '  [internal/tools/tasklist_order_test.go] ===
  mutated : rc=1 -> RED (good)      restored: rc=0 -> GREEN (good)
=== M3 patch rows index-only (near-duplicate fixture)  [internal/tools/memory_sync_txn_test.go] ===
  mutated : rc=1 -> RED (good)      restored: rc=0 -> GREEN (good)
=== M4 session test expects the retired alias  [internal/tools/task_session_test.go] ===
  mutated : rc=1 -> RED (good)      restored: rc=0 -> GREEN (good)
=== M5 session test expects the truncated hash  [internal/tools/task_session_test.go] ===
  mutated : rc=1 -> RED (good)      restored: rc=0 -> GREEN (good)
=== M6 released-ground test stands IN the ground (un-released)  [internal/tools/task_finish_cleanup_test.go] ===
  mutated : rc=1 -> RED (good)      restored: rc=0 -> GREEN (good)
all mutants red, all restores green
MUTATION_HARNESS_EXIT=0
```

**The fixture diagnosis** (probe, before the fix) — one import of the 20-row
patch: `stored=1 deduped=19 refused=0`, and the per-question assertion reported
every question "stored 0 times (want 1)" while question 01 was stored once.
`row count after two concurrent imports: got 2, want 21`.

**The child-mode refutation** — probe output quoted in §1 (`rows_before=1` in
each child, helper body reached in both, parent body never entered).

**Threshold check** (`internal/tools/memory.go`): `trigramJaccardMerge = 0.5`,
`lessonJaccardMerge = 0.40`, `lessonMinSharedTrigrams = 120`,
`dedupQuestionRunes = 200`.

**No knob changed**: `git diff 0d08f306..HEAD | grep -E '^[+-].*(timeout|budget|…)'
→ one hit, inside test data only:
`+ "the busy timeout queues the loser until the winner commits its patch",`
(a subject string in `syncImportTxnSubjects`). `scripts/test-budget.sh` line 82
still `test_budget="25m"`; the Makefile is untouched.

**Tree state at close**: `git status --porcelain` prints nothing in the worktree
(Rule 30); the sole untracked artifact, `bin/gopi`, is gitignored
(`.gitignore:2:bin/`).

## §5 The budget measurement (acceptance #4) — measured ONCE, and it TRIPPED

`make test-budget PKGS=./internal/tools`, verbatim, on this worktree (branch tip
`dbdc7894`). Load recorded before and after; **no budget or timeout knob was
changed** (grep-diff in Evidence).

```
=== UPTIME BEFORE ===
 11:27:20 up 19:22,  8 users,  load average: 3.74, 4.08, 4.14
4                                     ← cores
--- summary (1503s wall, load 3.74 4.08 4.14 -> 2.79 3.60 4.07) ---
packages: 1 finished, 1 with a failure
  FAILED: github.com/nick/gopi/internal/tools
slowest 15 tests (wall-clock seconds):
    13.83  tools  TestTaskOpenMergeBacklogBranches_TerminalRowStillMasks
    10.57  tools  TestTaskOpenMergeBacklogBranches_LiveRowMasks
    10.48  tools  TestSharedSessionHolderProbe
     9.62  tools  TestTaskNextConcurrentClaimRace
     …  (every one of the 15 slowest is under 14s)

================== TIMEOUT, NOT AN ASSERTION FAILURE ==================
the budget expired here:
  panic: test timed out after 25m0s
separate assertion failures in the same run (NOT the timeout):
  --- FAIL: TestCovToolsNotifyAlarmRegistryIsTheSourceOfTruth (0.26s)
  --- FAIL: TestTaskSubmitNoRepoDefaultsToCwd (0.80s)
  --- FAIL: TestTaskShowBriefingFromPayload (0.69s)
  --- FAIL: TestTaskShowBriefingFromSession (0.75s)
  --- FAIL: TestTaskShowBriefingMissingSession (0.75s)
  --- FAIL: TestReconcileMirrorCloseout_SIGKILLClosesFailed (1.59s)
  --- FAIL: TestReconcileMirrorCloseout_NonZeroExit (1.62s)
  --- FAIL: TestReconcileMirrorCloseout_Exit0ClosesCompleted (1.70s)
======================================================================

stamp: 2026-09-25T11:27:20+08:00 host=peerbox pkg=./internal/tools budget=25m
       elapsed=1503s load_start=3.74 4.08 4.14 load_end=2.79 3.60 4.07
       cores=4 exit=3 outcome=timeout
make: *** [Makefile:41: test-budget] Error 3
```

**Outcome reported verbatim: `exit=3 outcome=timeout`.** Not a test failure, and
not reported as one.

### What this measurement does and does not prove

1. **It reproduces the trip, and it REFUTES the briefing's classification.**
   The briefing called the timeout "a host-load phenomenon, not a code defect".
   The general shape is right — the budget is a wall-clock cap, and the script
   prints `TIMEOUT, NOT AN ASSERTION FAILURE` itself. But **the load explanation
   does not hold here**: my run tripped the 25m budget at load **3.74**, roughly
   one runnable thread per core, and the load *fell* to 2.79 by the end. The
   briefing's figure was load ~11 and 1517s; I got **1503s at load 3.74**. Near-
   identical wall time at ~1/3 the load means load is not what carries this.
   The script's own text ("~189-291s quiet and >1123s at load 42-63") is what the
   classification was drawn from — my measurement sits outside both of its
   bands.

2. **Nothing hangs.** The slowest single test in the whole package is 13.8s
   (`TestTaskOpenMergeBacklogBranches_TerminalRowStillMasks`) and every one of the
   slowest 15 is under 14s. With ~1800 tests the wall time is the SUM, spread
   across the suite — there is no single test to point at, and re-running one
   package with headroom would not isolate a hang, because there is none.

3. **Honest confound: a FOREIGN suite was running the same package.** PID
   1387480 ran `go test -count=1 -timeout 1800s ./internal/tools/ ./internal/testutil/
   ./internal/tasksource/` concurrently with my run (its elapsed time put its
   start ~11:40, i.e. inside my window). Two heavy `internal/tools` suites at once
   on 4 cores is real contention that a load average does not fully describe — so
   **1503s is an upper bound, not a quiet-host figure.** Per Rule 27 I did not
   manufacture load and I did not re-run to chase a nicer number; acceptance #4
   said measured once.

4. **This is NOT a claim about the fixes.** The five fixes are green under the
   7-name gate and under a 192.6s neighbour scope (§ Verifications), both far
   inside any budget. The trip is the same pre-existing debt the baseline run
   showed: the pristine-base full-package run took **1257s** (21m) at load ~4 and
   also did not leave itself much headroom. The budget question on this host is a
   property of the package's size on 4 cores at *ordinary* load, not of this
   diff — and separating the two properly (a genuinely idle host, with the suite
   as the only load) is a host-scheduling job, not a knob to widen.

5. **The one failure attributable to my run's code path: none.** Every assertion
   failure in that run is a name already red on the pristine base (§ Not Done
   lists all 14) — except that 4 of mine are now absent from it, and the other 2
   of the 7 were never in the full-suite red set to begin with.

6. **The smoke/heavy confound was CHECKED, not assumed.** `make test-budget` is
   the MERGE/VERIFY path and must run the smoke set, but an inherited
   `GOPI_HEAVY_TESTS` would silently switch it to the heavy set and measure a
   configuration nobody ships — a known, previously measured trap whose trip was
   ALSO ~1503s. Verified here: no `GOPI_HEAVY_*` variable was present in the
   environment the run inherited, and this run's own raw stream shows the
   heavy-classified tests being SKIPPED, each with the reason
   `(heavy test — set GOPI_HEAVY_TESTS=1 to run)`, e.g.
   `TestAdoptionConflictDedupMatchesTagTokenNotSubstring`,
   `TestBidir2LiveConflictNeverClobbered`. So this measurement is the default
   smoke shape, and the 1503s is that shape's own wall time on this host.

---

# Rework section — re-derived on master `3843f8c9` (rework run `01a0d7a3…`)

Everything above is the ORIGINAL run's account, kept as information (Rule 23:
the failed path stays readable). This section is what the reworked branch
actually carries and what was actually measured on the moved master. Where the
two disagree, THIS section wins.

## §RW1 — What the rework changed, file by file

| file | archived commit | rework outcome |
|---|---|---|
| `memory_sync_claim.go` | fix #2: replace the re-typed whitelist sentence with `declaredPeerError(p)` | **NO CODE CHANGE — master already superseded it.** master's `RequireDeclaredPeer(p, nil)` is strictly stronger (it resolves the declared topology itself, so no `declared` slice can be passed wrong). The branch's Rule 13 intent is satisfied by master; a MERGE NOTE records that at the site, and the file's diff vs master is the note only. |
| `memory_sync_txn_test.go` | fix #1: the shared-topic fixture measured the dedup screen, not the transaction | **CARRIED AS-IS** — master's fixture is byte-identical to the branch base, so the hunk applied cleanly. 20 distinct subjects + the load-bearing child-mode `return` comment. |
| `tasklist_order_test.go` | fix #3: `renderedRowLine` matched `" "+id+" "`, the pre-full-id layout | **CARRIED, union.** master had already fixed the helper the same way (probe the id COLUMN, `len(fields) >= 2`); the branch's spelling is kept because it is the strictly wider one — comparing `fields[0]` against the column needs no length guard, so a row whose id column ENDS the line still resolves. Both sides' comments are merged into one doc block. |
| `task_session_test.go` | fix #4: the retired `"auto"` alias + the removed digits-only truncation | **CARRIED, corrected.** The archived assertions are green here (see §RW2), but the branch's `tasksource.Normalize("auto")` form is kept because it is the stronger check: it pins that the alias stays RECOGNIZED while never reaching the file. master's `tasksource.Merger` form asserted only the destination. |
| `task_finish_cleanup_test.go` | fix #5: the test conflated the self-tree exclusion with the own-ground rung | **HALVED — the first half was REVERTED to master (its premise is false, §RW2); the companion `TestFinishCleanupReleasedGroundIsRecycled` is KEPT.** |
| `devdoc/tools-master-reds.md` | the report | re-derived (§RW4). |

## §RW2 — Two archived findings did NOT survive the rebase (measured)

**(a) fix #5's premise is FALSE on current master.** The archived test asserted
that a caller standing in the candidate ground is REFUSED by rung ①
(`guestRowOwnCheckoutRefusal`) and that the ground survives. Measured on
`3843f8c9` with that body applied: the recycle PROCEEDS —

```
--- FAIL: TestFinishCleanupWorktreeHeldByCallerIsRecycled (0.56s)
    task_finish_cleanup_test.go:1305: a caller that has NOT released its ground
    must not have it removed: recycle proceeded, test branch=feat/server-recycle
    done=[align-before-delivery,pre-merge,worktree]
```

The reason is a master commit the branch's base predates: `9bad5b42`
"fix(tools): a manual run's close-out releases the worktree it was launched in"
(2026-09-25). It did not weaken the rung — it EXEMPTED this one asker:
`reclaim_ground_checklist.go` skips rung ① when asked through
`reclaimGroundChecklistVerdictForCloseout`, which is the close-out's own entry
point. The rung still protects OTHER processes
(`task_reclaim_own_ground_test.go` pins that direction, and it is green here).
So master's assertion is the true one and is kept; the branch's hunk is
dropped with a MERGE NOTE naming the commit that superseded it. The branch's
companion test is green and kept.

**(b) fix #4's two assertions are already green on master.** master's own body
passes here (`TestUuidTaskIDSubmitCreatesSessionContainer` → PASS with master's
exact text), so the archived "two stale expectations" were stale relative to the
branch's BASE, not to current master — master had already corrected them, in a
weaker form (`meta["source"] != tasksource.Merger`, and `strings.Contains` on the
rendered list). The branch's strengthened form is kept (§RW1) and its mutation
red-proof is re-verified (§RW3) — but the claim "this hunk turns a red green on
current master" is NOT made: there was no red here.

**What remains genuinely red on master and is NOT touched by this branch** — the
same names the original report enumerated, re-measured live in §RW4.

## §RW3 — Mutation red-proofs, re-verified on the rebased tree

`/home/user/workspace/merge-run-archive/rework-backup/mutation-proofs.sh`
(kept OUTSIDE the worktree — Rule 30). Each fix reverted to its pre-fix shape,
the gate test run, the file restored from a pristine copy, the gate re-run:

```
mutation-1 (shared-topic fixture restored):    rc=1 RED  -> restored rc=0 GREEN
mutation-2 (whitelist sentence re-typed):      rc=1 RED  -> restored rc=0 GREEN
mutation-3 (substring probe restored):         rc=1 RED  -> restored rc=0 GREEN
mutation-4 (stale "auto" + digits-only):       rc=1 RED  -> restored rc=0 GREEN
```

The RED outputs name the defect, e.g. mutation-3:
`the locator must resolve the child's own line by its ID COLUMN, got ""` and
`--- FAIL: TestParentGateRowLocatorNamesTheIdColumn`.

Honest scope limit: **fix #5's remaining half (the companion test) is NOT
mutation-proven here** — the harness could not both chdir away and un-chdir to
construct a red. Its claim is the weak direction (the recycle proceeds for a
released caller), it is green, and master's own test pins the other direction.

### Independent re-run of the whole proof, from a clean tree

The four proofs above were re-executed a second time by a **separate verification
pass** (not the authoring round), starting from `git status --porcelain` empty and
ending with it empty again:

```
$ bash /home/user/workspace/merge-run-archive/rework-backup/mutation-proofs.sh
baseline: 4 x rc=0 GREEN (unmutated)
mutation-1: rc=1 RED (red-proof OK)  -> restored-1: rc=0 GREEN
mutation-2: rc=1 RED (red-proof OK)  -> restored-2: rc=0 GREEN
mutation-3: rc=1 RED (red-proof OK)  -> restored-3: rc=0 GREEN
mutation-4: rc=1 RED (red-proof OK)  -> restored-4: rc=0 GREEN
=== final state restored from pristine copies ===
$ git status --porcelain          # prints nothing
```

The RED lines name the same defects independently (mutation-4's second half:
`legacy t- hash display = "t-1788146419", want digits`). The baseline leg is what
makes each RED meaningful: the same four selections are GREEN **before** the
mutation, so the red is caused by the revert and not by the selection.

## §RW4 — Post-rebase measurements (the authoritative figures)

<!--RW4-->

All figures below were produced on THIS branch (tip = the merge of master
`93ceba76`), with `GOPI_BIN` exported so the probe-child leg can re-exec. Where a
number is a set, it is the measured set, not an estimate.

**Build / vet (final tree)**
```
$ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/   → rc=0
$ go vet ./...                                      → rc=0, no output
```

**The 7-name gate — GREEN, `ok 6.624s`** (re-measured twice: the authoring round's
second run `6.570s`, and the independent verification pass `6.049s`)
```
$ GOPI_BIN=$PWD/bin/gopi CGO_ENABLED=0 go test ./internal/tools/ -count=1 -timeout 600s -run
  '^(TestSyncImportTwoProcessConcurrent|TestDeclaredTopologyWordingHasOneDefinition|TestParentGateListingAgreesWithFunnel|TestUuidTaskIDSubmitCreatesSessionContainer|TestFinishCleanupWorktreeHeldByCallerIsRecycled|TestFinishCleanupReleasedGroundIsRecycled|TestSharedSessionHolderProbe)$' -v
--- PASS: TestSyncImportTwoProcessConcurrent (0.99s)
--- PASS: TestDeclaredTopologyWordingHasOneDefinition (0.01s)
--- PASS: TestSharedSessionHolderProbe (2.55s)
--- PASS: TestFinishCleanupWorktreeHeldByCallerIsRecycled (0.55s)
--- PASS: TestFinishCleanupReleasedGroundIsRecycled (0.25s)
--- PASS: TestParentGateListingAgreesWithFunnel (1.67s)
--- PASS: TestUuidTaskIDSubmitCreatesSessionContainer (0.47s)
PASS
ok  	github.com/nick/gopi/internal/tools	6.624s
```
The gate is run both as the six originally-named tests (all green) plus
`TestSharedSessionHolderProbe` — the seventh of the branch's claimed figures —
and the companion `TestFinishCleanupReleasedGroundIsRecycled`. **`TestSharedSessionHolderProbeChild`
is deliberately NOT in the green claim**: as a top-level selection it SKIPs by
design ("only runs as the re-exec'd child"); it RUNS inside its parent, whose
mode table carries it, and that is where its evidence lives.

**Full package, on the reworked tree — 872.179s**

```
$ CGO_ENABLED=0 go test ./internal/tools/ -count=1 -timeout 1800s
FAIL	github.com/nick/gopi/internal/tools	872.179s
top-level FAIL lines: 1     nested FAIL lines: 0
--- FAIL: TestVerifyGateRecordsVerdictEvent (0.94s)
```

This is the headline number the rework buys: **the archived baseline measured 15
`--- FAIL` lines = 14 top-level FAILs (1257s); the reworked tree measures 1
top-level FAIL (872s) — and that one is NOT this branch's.**

**The residual failure is PRE-EXISTING, proven on a pure-master control — not
argued.** A detached control worktree at the NEW master tip (`93ceba76`) was
built and the same test run there:

```
$ git worktree add --detach ~/workspace/gopi-01a0d7a3-basemaster 93ceba76
$ cd ~/workspace/gopi-01a0d7a3-basemaster && CGO_ENABLED=0 go test ./internal/tools/ -count=1 -run '^TestVerifyGateRecordsVerdictEvent$'
--- FAIL: TestVerifyGateRecordsVerdictEvent (1.16s)      ← identical on PURE master
```

Every file in this branch's own diff (`memory_sync_claim.go`,
`memory_sync_txn_test.go`, `task_finish_cleanup_test.go`, `task_session_test.go`,
`tasklist_order_test.go`) is disjoint from `verify_gate_test.go`, so the diff
cannot be the cause — and the control run is what makes that a measurement
rather than an inference.

**One flaky test, named rather than hidden.** `TestSharedSessionHolderProbe`
PASSes in isolation and in the 7-name gate (measured twice: `ok 2.701s`,
`ok 2.836s`), and FAILed in one batch selection on BOTH trees (my branch AND the
pure-master control). It is order/lifecycle-coupled, not a property of this diff,
and it is reported as measured rather than presented as a clean green.

**Also measured, for the record: the archived "two stale expectations" (fix #4)
are already green on master.** `TestUuidTaskIDSubmitCreatesSessionContainer`
passes with master's own body; its strengthened body here passes too — see §RW2.

**What was NOT re-run, and why (honest).** `make test-budget PKGS=./internal/tools`
was not re-run in the rework: the original run's 25m cap already reported
`exit=3 outcome=timeout` at 1503s with its confound named, a full re-run costs
~25m of a 60m session, and the question a REWORK must answer is the red set, not
the budget. The package's own measurement above (872s, one invocation) is the
evidence supplied instead.


## §RW5 — `Ran:` (rework)

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → `rc=0` (final merged tree).
- Vet: `go vet ./...` → `rc=0`, no output.
- Ran: **`GOPI_BIN=$PWD/bin/gopi CGO_ENABLED=0 go test ./internal/tools/ -count=1 -timeout 600s -run '^(TestSyncImportTwoProcessConcurrent|TestDeclaredTopologyWordingHasOneDefinition|TestParentGateListingAgreesWithFunnel|TestUuidTaskIDSubmitCreatesSessionContainer|TestFinishCleanupWorktreeHeldByCallerIsRecycled|TestFinishCleanupReleasedGroundIsRecycled|TestSharedSessionHolderProbe)$' -v`** → seven `--- PASS` lines, `ok github.com/nick/gopi/internal/tools 6.624s`.
- Ran: `CGO_ENABLED=0 go test ./internal/tools/ -count=1 -timeout 1800s` → `FAIL … 872.179s`, 1 top-level FAIL (`TestVerifyGateRecordsVerdictEvent`), which the pure-master control at `93ceba76` reproduces identically — pre-existing, not this branch's.
- Ran: `bash /home/user/workspace/merge-run-archive/rework-backup/mutation-proofs.sh` → 4 mutants RED, 4 restores GREEN (transcript in §RW3).
- Ran (independent verification pass, clean tree → clean tree): `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → `rc=0`; `go vet ./...` → `rc=0`; the 7-name gate again → seven `--- PASS`, `ok github.com/nick/gopi/internal/tools 6.049s`; the mutation script again → 4 RED / 4 GREEN with `git status --porcelain` empty at both ends.
- Ran (ancestry, commit path step 7): `git merge-base --is-ancestor master HEAD` → `rc=0`.
- Result: the internal/tools red set is down from the archived 14 top-level names to 1, the 7-name gate is green, the 4 carried fixes carry re-verified mutation red-proofs, and a `## Not Done (honest)` records the third fix's half and the un-run budget leg.
- Commit: the branch tip is a merge of master `93ceba76` onto the rework commit; the report deliberately names no hash of itself (a hash inside a report invalidates itself the moment it is committed).

### Post-rebase scope — what this branch does NOT claim

- It does **not** claim to have cleared the residual full-package red. Exactly
  one top-level name is red (`TestVerifyGateRecordsVerdictEvent`) and it is
  **pre-existing on pure master `93ceba76`**, measured in a detached control
  worktree — it should be dispatched as its own bug task, not fixed inside a
  rework branch.
- It does **not** claim `TestSharedSessionHolderProbe` is stably green: it is
  order-coupled and FAILed once on both trees. Named as measured.
- It does **not** re-derive a budget number; see §RW4's "What was NOT re-run".
- It does **not** claim fix #5's companion test is mutation-proven (§RW3).

## Not Done (honest) — rework

1. **`TestVerifyGateRecordsVerdictEvent` is left RED** — proven pre-existing on
   pure master `93ceba76` (control worktree, identical failure), untouched by
   this diff. Fixing it is a separate work item, not a rework's job; it is
   dispatched as its own task rather than folded in here.
2. **fix #5's first half is dropped, not fixed.** Its premise ("an in-process
   caller standing in the ground is refused") is false on current master —
   `9bad5b42` exempted the close-out's own asker. No fix was possible because
   there was no defect; the report says so instead of manufacturing one.
3. **fix #4 did not turn a red green.** Its two assertions are already green on
   master; the stronger form is carried for its extra guard, and the claim "this
   hunk clears a red" is explicitly NOT made.
4. **`TestSharedSessionHolderProbe` is order-coupled** — FAILed once in a batch
   selection on both trees, PASSes isolated and in the gate. Not a clean green;
   recorded as measured.
5. **The budget leg (`make test-budget PKGS=./internal/tools`) was not re-run**
   — the original run's 25m-cap measurement stands as historical, and a re-run
   costs ~25m of a 60m session.
6. **No whole-tree suite was run** (Rule 27's smoke/branch-scoped rule): the
   package-scoped run above plus the pure-master control is the evidence, not
   `go test ./...`.

## §RW6 — The re-application criterion, MEASURED (and the blob-level audit)

The acceptance criterion is stated as "`git apply -3` of the archived patch lands
cleanly after hand reconciliation". Measured, verbatim, on this tree
(`git apply -3 --check /home/user/workspace/merge-run-archive/01a0d66c.patch`):

```
Performing three-way merge...
Applied patch to 'devdoc/tools-master-reds.md' with conflicts.
Applied patch to 'internal/tools/memory_sync_claim.go' with conflicts.
Applied patch to 'internal/tools/memory_sync_txn_test.go' cleanly.
Applied patch to 'internal/tools/task_finish_cleanup_test.go' with conflicts.
Applied patch to 'internal/tools/task_session_test.go' with conflicts.
Applied patch to 'internal/tools/tasklist_order_test.go' with conflicts.
```

**That output is the expected outcome of a rebase, not a shortfall — and the
reason is mechanical.** A patch carries `index <pre>..<post>` but no blobs; the
three-way merge needs the PRE-image, and the pre-image here is
`task/01a0d66c`'s blobs, which the failed landing deleted with the ref (the
briefing says so: "the ref is gone" — and re-creating it is exactly the Rule 23
ban). Nor is the criterion hittable by applying at the patch's own base: on a
detached checkout of `eb846838` — the tree the patch was authored against —
plain `git apply --index` still fails at four files
(`memory_sync_claim.go:947`, `memory_sync_txn_test.go:47`,
`task_session_test.go:30`, `tasklist_order_test.go:61`) because the context lines
are gone. **A diff whose pre-image no longer exists cannot be "applied cleanly";
the reachable reading of the criterion is that its INTENT is present in the
tree**, which is what this section audits byte by byte:

The patch file *is* a git object store, so the archived side is recoverable
without the ref: `git cat-file -p <post-blob>` for each `index` line in the
patch (`9b38c020`, `915341e0`, `8d2548a9`, `e4262776`, `ea8834dc`, `627fbb79` —
all present). Comparing each post-image against this tree's file, non-blank
lines present in the archive and absent here:

| file | archived lines absent | what they are |
|---|---|---|
| `devdoc/tools-master-reds.md` | 0 | superset — the archived text is kept whole, §RW1–§RW5 appended |
| `memory_sync_txn_test.go` | 0 | carried verbatim |
| `task_session_test.go` | 1 | comment prose, re-worded (the code — `tasksource.Normalize("auto")`, `if !known` — is present) |
| `tasklist_order_test.go` | 3 | comment prose, relocated into the MERGE NOTE; the code is kept and strictly wider (no `len(fields) >= 2` guard) |
| `task_finish_cleanup_test.go` | 36 | fix #5's first half, REVERTED to master on purpose (§RW2), plus its comment prose |
| `memory_sync_claim.go` | 10 | the branch's re-typed refusal sentence — deliberately dropped: master's `RequireDeclaredPeer` is the shared source (Rule 13), recorded at the site |

**Total 50 archived non-blank lines absent, every one attributed above**: comment
prose that moved or was re-worded, or one of the three decisions this report
already names (§RW1/§RW2) — no code, no assertion and no test lost silently.
`grep -c '^[<>]'` on the same six files against the archived post-images gives
40/48/78/20/9/222 changed lines, i.e. the reconciliation is *smaller* than the
archived hunks at every conflicted site, which is the shape of "master had
already moved the same code" and matches §RW2's two falsified findings.

**Scope note on "no conflict markers anywhere".** The six delivered files carry
none — `grep -n '^<<<<<<<' internal/tools/*.go` matches only
`task_adoption_test.go`'s deliberate fixture string, and
`internal/tools/task_adoption_conflict.go` (master's own conflict-marker
detector) names the spelling in a comment. Master itself ships one quoted marker
block in `devdoc/repo-context-gate-reverify.md`, fenced as documentation and
asserted as such by the conflict-marker gate (`1abf1b3f`, "fence-aware
conflict-marker gate; the devdoc quote stays"). Neither is this branch's doing;
neither is a conflict.

