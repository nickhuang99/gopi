<!-- doc-class: historical -->
# Independent Verification — ledger row `01a09250-07bd-71fe-a241-580e5477a469`

**Subject**: "worktree recycle" — bugfix branches must have their git worktree
recycled at delivery, else the merger (merge-candidate signal = "branch with no
worktree protection") never merges them; includes the foreign-owner-gate
blocking root cause.

**Verdict: PASS (verified)** — recorded once via `gopi task update`.

**Mode**: read-only. Nothing in the deliverable was edited or fixed. Two
throwaway detached worktrees were used for the pre-fix/red-proof comparison and
removed afterwards.

---

## 1. The delivered fix is located

| Item | Value |
|---|---|
| Merge commit | `6a606feae6ca841e0f96952669596d85edf6a7b2` "Merge branch 'fix/worktree-recycle-2'" (2026-09-12 07:45:11 +0800) |
| Second parent | `68a1db20` "resolve merge fix/worktree-recycle-2 into master" (first parent `504dd1d0` = master) |
| Fix commits | `50d82385` fix(worktree-recycle) · `eb9b070a` test(red-proofs) · `f95f8c1a` docs — **all four verified `git merge-base --is-ancestor … 6a606fea` → IN-MASTER** |
| Devdoc | `devdoc/worktree-recycle-fix-20260912.md`, 277 lines, present at master |
| Code surface | `internal/tools/task_finish_cleanup.go` (+201/−…), `task_merged_cleanup.go` (+177), `task_deliverable_worktree.go` (+20), `cmd/agent/cli_tasks.go` (+163), `internal/standards/baseline.go` (−1 stale permit + comment) |

Root-cause A — the pre-fix gate **is** the reported bug, read at `6a606fea^1`
`finishCleanupForeignOwnerReason`: the first owner row naming the path returns
an unconditional refusal, with no liveness classification:

```go
for _, o := range owners {
    if !finishCleanupSameWorktreePath(o.Deliverable, worktree) { continue }
    return finishCleanupOwnerMismatchMarker + ": worktree " + worktree +
        " is owned by row " + o.TaskID + " (" + o.Status + … + ")"
}
```

The fix adds `finishCleanupOwnerRowLive(status, claimedBy)` (task_finish_cleanup.go:783):
live status ⇒ holder; unresolvable foreign claim ⇒ holder (unknowable ≠ dead);
terminal + locally-resolvable pid gone ⇒ STALE (reported, not blocking).

Root cause B: the pre-fix sweep had exactly ONE scan
(`ORDER BY updated_at DESC LIMIT 10`) and `serveFinishCleanupStrandedIDs` /
`serveFinishCleanupWorktreeQuery` do not exist at `6a606fea^1` (both are `+`
lines in the merge diff), so a directory deeper than 10 rows was structurally
invisible. `cancelled` was also outside every scan set.

Root cause C: the pre-fix backfill overwrote an existing stamp
(`task_deliverable_worktree.go`, "overwrite when non-empty"); the fix returns
early when `worktreeExisting != "" && != worktree`, with a same-directory
branch-gap fill still allowed.

## 2. Build / format / tests

```
HEAD 6a606fea
CGO_ENABLED=0 go build ./...                        → exit 0, no output
gofmt -l <11 touched files>                         → no output (clean)

CGO_ENABLED=0 go test -count=1 -timeout 30m \
  -run 'Worktree|Stranded|MergedCleanup|FinishCleanup|Recycle|Deliverable|StaleOwner|LiveHolder|ForeignHost|CancelledRow|ServerRecycle' \
  ./internal/tools/ ./cmd/agent/
  → ok  github.com/nick/gopi/internal/tools  25.404s
  → ok  github.com/nick/gopi/cmd/agent        8.935s
  → TOOLS_AGENT_EXIT=0          (128 test functions matched the selector)

go test -count=1 -run 'TestStandardGates/TestGate3|TestStandardGateDataReport' ./internal/standards/
  → ok  github.com/nick/gopi/internal/standards  7.578s   (Rule 18 gate green: the gate this branch touched)
```

## 3. Independent red-proof (the negative case is REAL, not asserted)

The deliverable's own test bodies were frozen **verbatim** into a throwaway
detached worktree at `6a606fea^1` (pre-fix) and run against pre-fix production
code; then the same tree was checked out at `6a606fea` (fixed). No deliverable
file was modified.

| Probe (deliverable's own test) | PRE-FIX `6a606fea^1` | FIXED `6a606fea` |
|---|---|---|
| `TestStaleOwnerRowIsNotABlocker` (cancelled sibling, dead pid) | **FAIL** — `"a dead-holder row must not block the recycle, got \"worktree ownership mismatch: worktree …/wt is owned by row t-stale-owner (cancelled, claimed-by pid 7 gone)\""` | **PASS** |
| `TestTaskBackfillDeliverableWorktreeNeverRebindsToAnotherWorktree` (root cause C) | **FAIL** | **PASS** |
| `TestLiveHolderRowStillBlocks` (safety) | PASS | PASS |
| `TestForeignHostHolderIsNeverCalledDead`, `TestCancelledRowIsCleanable` | undefined symbol ⇒ **does not compile** (the four new production symbols: `finishCleanupOwnerRowLive`, `taskCleanupEligibleStatus`, `DeliverableWorktreeKey`, `TaskWorktreeRow.HolderLive`) — compile-red proven, then compiles clean at `6a606fea` | PASS |

The pre-fix FAIL message is the incident's own string, quoted in the devdoc —
so the negative case is a reproduction of the reported bug, not a
restatement of the fix.

## 4. Safety half (a live worktree is never removed)

| Test | Tree | Result |
|---|---|---|
| `TestLiveHolderRowStillBlocks` (in-progress row, live pid stamp → must skip, dir survives) | both | PASS |
| `TestStrandedScanIgnoresLiveRows` (in_progress/pending/claimed never selected) | fixed | PASS |
| `TestForeignHostHolderIsNeverCalledDead` (foreign-host claim = alive) | fixed | PASS |
| `TestMergedCleanupLiveCwdHolderKeepsRowOwnedWorktree` / `TestMergedCleanupDirtyWorktreeSkipped` / `TestFinishCleanupChainDirtyWorktree` | fixed | PASS |
| **e2e** `TestWorkerServerDeliveryDirtyWorktreeKeepsBranch` (uncommitted `wip.txt` must survive) | fixed | PASS |
| **e2e** `TestWorkerDeliveryRecyclesDespiteAStaleSiblingRow` (dead sibling must not block; branch survives for the merger) | fixed | PASS |
| **e2e** `TestWorkerServerDeliveryRecyclesThenMergerMerges` (…then the merger does merge the branch) | fixed | PASS |

The seven `*Worktree|Recycle*` e2e tests **skip** in a fresh checkout (they
need `bin/gopi`). To measure rather than trust a skip, an isolated worktree was
checked out at `6a606fea`, `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`
was run there (build OK, `strings bin/gopi` finds the new symbols 6×), and the
three e2e tests then **RAN and PASSED** (2.27s / 2.00s / 2.18s).

## 5. Honest boundaries of this verification

1. **Not reproduced independently**: the pre-fix RED of
   `TestSweepRecyclesADeepStrandedWorktree` (root cause B). Its mechanism is
   nevertheless established by inspection — `serveFinishCleanupStrandedIDs`
   and `serveFinishCleanupWorktreeQuery` are additions of this branch (no
   second scan existed at `6a606fea^1`) and `cancelled` was outside the old
   status set. The test passes on the fixed tree.
2. An attempted probe of the *pre-fix sweep entry condition* ("does the newest-10
   query select a live row?") returned **no rows**, so it proved nothing; it is
   reported as inconclusive rather than as evidence.
3. The full-suite runs (`go test ./...`) were deliberately not used: this
   branch's own devdoc documents pre-existing master reds
   (`TestMergeVerifyGatePassKeepsMerge`, `TestCovCrHasSessionEnd_*`,
   `TestStartupGate_SkipsTheWalkWhileTodayStamped`) and a full-run hang. The
   verification therefore targets the touched packages with the epic's own
   selector — the standard this epic's devdoc itself applied.
4. No live worker/serve daemon was driven end-to-end; the fix is verified at
   the test + binary level. `bin/gopi` on master (rebuilt 07:47) does carry the
   new symbols, i.e. the Rule 12 rebuild happened — observed, not required by
   this acceptance.

## 6. Verdict

Every acceptance criterion is met with real command output:

1. fix located (`6a606fea`, devdoc present, fix commits ancestors of master) — **OK**
2. build `CGO_ENABLED=0 go build ./...` exit 0 — **OK**
3. own tests pass; red-proof for the foreign-owner-blocked worktree is a REAL
   FAIL pre-fix and a PASS post-fix; safety cases (live/dirty/foreign-host)
   hold — **OK**
4. `gofmt` clean on all 11 touched files, Rule 18 gate green — **OK**
5. verdict recorded exactly once on the ledger row — **OK**

**PASS / verified.**
