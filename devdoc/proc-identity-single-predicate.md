<!-- doc-class: historical -->
# Task Report: ONE three-factor identity predicate (ledger-verdict question)

Branch: `fix/proc-identity-single-predicate` (worktree `~/workspace/gopi-proc-identity`)

## Completed

### The defect (measured on the branch base, `a9a96cd7`)

The three-factor identity (pid + `/proc/<pid>/stat` starttime + cmdline) was
implemented **five times**, each with its own degradation ladder, so one question
got **two answers**:

| record shape | `procname.Verdict` (worker count) | `tools.taskIdentityVerdict` | `tools.finishCleanupProcessHolderLive` |
|---|---|---|---|
| stat unreadable, process dir present | `unknown` | `unknown` | **NOT LIVE** (a fabricated death) |
| incarnation matches, recorded cmdline differs | **`recycled`** (a fabricated death) | `running` | `live` |
| incarnation differs | `recycled` | `recycled` | not live |
| pid gone | `recycled` | `dead` | not live |

The two bold cells are fail-open: the value they produce ("not live") is exactly
the one the **destructive** arms act on — `worktree_sweep.go`/`task_reclaim.go`
free a directory, `task_reclaim.go:784`/`task_finish_cleanup.go:1725` stamp
`HolderDeadLocal` and print a release note for it (memory #2632: a drifting second
ladder deletes a live worktree; memory #2578: the cmdline-vs-tick precedence).

### The one predicate — `internal/procname` (leaf package, Rule 14)

`internal/procname` already owned the factor reads (`StatField`, `StartTicks`,
`Cmdline`) and the triple's doc; it is now the single source of the **comparison**
too:

- `procname.RunnerIdentity(procRoot, Liveness) (Outcome, UnmeasurableReason)` —
  the ledger-verdict question, answered in **four** values:
  `OutcomeLive / OutcomeGone / OutcomeRecycled / OutcomeUnmeasurable`.
- `procname.RunnerLive(...) (bool, UnmeasurableReason)` — the boolean form, where
  the reason is non-empty **only** when the kernel did not answer, so an
  unmeasurable factor can never share a value with "not live" (Rule 15a).
- `procname.Match(rec, live)` — the one **comparison** of the factors; a caller
  that must read procfs once for several purposes (a report that prints the
  factors it compared) calls this instead of re-inlining the ladder.
- `procname.Verdict` keeps its own three-value vocabulary for the worker count and
  is now a pure mapping of `RunnerIdentity` — so the count and the ledger cannot
  disagree.

The doc comment states the question in the epic's own terms and names the
boundary in one sentence:

> `RunnerIdentity` answers THE LEDGER-VERDICT QUESTION: "is the runner a task row
> RECORDED — pid + /proc starttime + cmdline — still, right now, the same live
> process?" It does NOT answer the CHECKOUT-OCCUPANCY question … that is a
> different question about a different channel (/proc/<pid>/cwd …), it has its own
> single implementation (`scratch.HeldByProcesses` / `scratch.LiveHolders`), and a
> caller that needs it must ask it there.

### The uniform ladder (D3) — one precedence, one named unknown

```
process directory absent                     → Gone         (measured)
pid names a zombie                           → Gone         (measured)
incarnation recorded and it differs          → Recycled     (measured: pid reused)
no incarnation recorded, cmdline differs     → Recycled     (the argv ROLE check)
all recorded factors agree, state != Z       → Live
no procfs / unparsable stat / unreadable
  cmdline the record states                  → Unmeasurable (reason NAMED)
no factor recorded at all                    → Unmeasurable (reason NAMED)
```

`UnmeasurableReason` is an exported named type with four values:
`UnmeasurableNoRecord`, `UnmeasurableNoProcfs`, `UnmeasurableStatUnparsable`,
`UnmeasurableNoCmdline`.

### Routing (D2) — every identity caller, one comparison

| site | question it now asks | reading rule |
|---|---|---|
| `taskproc.go` `taskIdentityVerdict` (task status / `gopi resume` gate / list ladder) | `procname.RunnerIdentity` | maps to `running/dead/recycled/unknown` |
| `task_finish_cleanup.go` `finishCleanupProcessHolderLive` (sweep + reclaim VETO) | `procname.RunnerLive` | unmeasurable **holds** the veto |
| `task_finish_cleanup.go` `finishCleanupDeadLocalHolder` (the RELEASE disposition) | `procname.RunnerIdentity` | only Gone/Recycled is a release; unmeasurable keeps ground |
| `task_holder_resume.go` `holderProbe.resolve` (resume probe) | `procname.RunnerLive` | unmeasurable → `holderProbeUnreadable` |
| `merger_takeover_progress.go` `TakeoverProgressRoundLive` + `progressTripleMatches` | `procname.RunnerIdentity` / `procname.Match` | unmeasurable is not a finished round |
| `worktree_sweep.go`, `task_reclaim.go`, `branch_attribution.go` | via `finishCleanupProcessHolderLive` | unchanged contracts |

One input builder (`identityLiveness`, `internal/tools/taskproc.go`) turns a
recorded row identity into the predicate's input, because the callers did **not**
share a data shape — and that is the second half of the base disagreement: the
destructive arm passed only `{pid, startTicks}` (and bailed out when no tick was
recorded, falling back to the claim pid), while the ledger passed the recorded
**cmdline** in that same shape. Same record, different facts, two answers.

The checkout-occupancy matcher is untouched: `scratch.HeldByProcesses` /
`LiveHolders` is still the single implementation of the `/proc/<pid>/cwd` scan,
and `holderProbe` still consults it — but only *after* the shared predicate says
not-live, and the predicate's doc says so explicitly.

### One assertion repaired (measured, not re-derived)

`internal/testutil/procname_triple_redproof_test.go` pinned the retired semantics:
"a same-incarnation process with a different argv is `recycled`". That is the
fabricated death itself. The assertion now requires `VerdictAlive` and carries the
reason (the incarnation tick is the identity; the runner may re-exec in place).
The red proof's own point — comm+cmdline accepts a recycled pid while the triple
rejects it — is unchanged.

## Rework onto master `9e6d7844` (this run)

The original branch's ref was pruned after the failed merge (Rule 23), but its
commits survived. The rework branch was created **at the preserved commit
`eb887c5c`** and then **master was merged into it**, so the resolution is a real
merge whose first parent is the branch — the branch's intent carried forward, not
a fresh re-typing. `master` is an ancestor of the result, so a fast-forward merge
is possible.

Three conflicts, each resolved toward the SURVIVING subject:

1. **`internal/tools/merger_takeover_progress.go` — RETIRED by master.** `93009688`
   ("refactor(merger): delete the retired daemon's implementation, keep its live
   callers") deleted the retired merger daemon's 28 files, this one included, and
   re-homed its live symbols (`merger_takeover_live.go` is gone too). The branch's
   edit routed `progressTripleMatches` through `procname.Match`; the file does not
   survive the merge (`git rm`), and the symbol is referenced nowhere on master
   outside its own doc history (`devdoc/task-process-liveness-predicate.md`,
   `devdoc/merger-design-alignment-audit.md`, which describe the pre-`93009688`
   state — this branch does not rewrite another task's docs). The agreement guard
   in `internal/tools/identity_shared_test.go` was rewritten to assert
   **`procname.Match` directly** for the same unmeasurable shape, so the guard
   keeps its subject while its retired carrier is gone.
2. **`internal/tools/taskproc.go` — master's ladder returns a struct.**
   `1dc2460c` / `71f360ba` (merged) refactored `taskIdentityVerdict` to return
   `taskProcVerdict{State, Reason}`, and master's ladder is *itself* the code this
   epic replaces. Resolved by taking the branch side (the routing) and re-typing
   the mapper: `taskProcessStateFromOutcome` now returns a `taskProcVerdict` and
   carries the ledger's one-line evidence sentence, so the machine verdict and the
   operator text cannot drift. Master's added legacy no-identity rung (the
   `pidAlive` + name-prefix ladder) is preserved verbatim.
3. **`taskShowLiveWord` existed on both sides.** Master's version (over the
   five-value verdict vocabulary: `alive` / `gone` / `alive-but-recycled` /
   `unsettled` / not-measured) survives — Rule 14, one vocabulary; the branch's
   duplicate (over the raw verdict string) was deleted, and `taskShowIdentityLine`
   derives its word from `taskRowProcessLiveness(t).State`, the same verdict the
   gate reads.

One thing the rework had to **recover**: the worktree was forked at `eb887c5c`,
while the branch's tip was `dcf16706`. `3d9547c1`'s Rule 22 scratch pin
(`internal/procname/testmain_scratchroot_test.go`) was cherry-picked in — it
matters here because this diff adds `internal/procname/identity_test.go`, a suite
that builds synthetic procfs trees in `t.TempDir()`. `dcf16706`'s fuller devdoc
body was restored as this file's base, with this rework section appended.

Also carried: the repaired `internal/testutil` red-proof assertion (the retired
"same incarnation + different argv ⇒ recycled" semantics).

## Not Done (honest)

- `internal/procstat.Alive` (session crash classification) and
  `internal/tools/taskzombie.go pidAlive` are **bare-pid** probes, not three-factor
  comparisons: they are the opposite question ("does this number answer at all",
  used where a recorded identity does not exist). They are not routed and not
  claimed as routed.
- `internal/daemoncwd` (`ProcessArgv0`, `IsDaemonProcess`) and
  `internal/tools/merger_takeover_live.go`'s argv[0] discovery filter are NAME
  probes by design (the code says so); they were left alone.
- `internal/testutil/procident.go` keeps its own test-helper triple reader — a
  test-support copy, not a production ladder.
- `progressTripleMatches` was **strengthened** in the original branch (the
  "only the pid is available on one side ⇒ accept" fallback became unmeasurable ⇒
  not a match). That file was then retired by master (`93009688`), so on the
  rework the strengthened comparison has **no caller left**: the guard survives
  only as the `procname.Match` assertion in `internal/tools/identity_shared_test.go`.
- **Not re-measured on this rework**: the base-level failure sets the original
  branch measured at `a9a96cd7` (`internal/standards`' single Rule 17 hit at
  `cmd/agent/merge_lock_test.go:200`, the `TestFinishCleanupChain*` fixture
  collision, `TestAssertionTotalityAuditGate`, and the `internal/testutil`
  six-test set) are the ORIGINAL branch's measurements, carried as such. This run
  re-measured only the control that mattered for the rework's own gate
  (`TestFinishCleanupChain` on pristine master — see Evidence).

## Verification

- Build: `go build ./...` — exit 0
- Vet: `go vet ./...` — exit 0
- Tools: `make test-budget PKGS=./internal/tools/` — see `## Evidence (self-filed)`
- Commit: `9aa885ae` on top of `f36c4da3` (branch `rework/proc-identity-single-predicate`)

## Evidence (self-filed)

### This rework (measured in the rework worktree)

- `go build ./...` → exit 0, no output.
- `go vet ./...` → exit 0, no output.
- `go test ./internal/procname/ -count=1` → `ok .../internal/procname 0.014s`.
- `go test ./internal/tools/ -run 'TestIdentityPredicateIsTheOnlyLadder|TestUnmeasurableIsNotADeathForEveryCaller|TestCallersOfOneQuestionAgreeForTheSharedRecord|TestTaskStatus|TestProcIdentity' -count=1` → `ok .../internal/tools 20.130s`.
- CONTROL on pristine master (`/home/user/workspace/gopi-proc-identity-base-control`, merge-base `9e6d7844`): `go test ./internal/tools/ -run 'TestFinishCleanupChain' -count=1` → `ok .../internal/tools 6.702s` — the two `TestFinishCleanupChain*` failures the original branch measured at `a9a96cd7` are already repaired on master, so they are NOT a rework regression.
- `make test-budget PKGS=./internal/tools/` (first attempt) → exit 3, **TIMEOUT, not an assertion failure**: `panic: test timed out after 25m0s`, `load_start=9.83 9.73 9.48 load_end=17.95 16.80 13.89 cores=4`. It started before `bin/gopi` existed, which the script's own note says makes the CLI E2E guards fail rather than skip. `bin/gopi` was then built and the package re-run with `GOPI_TEST_TIMEOUT=45m`: this time the budget did NOT trip — 12 ASSERTION failures, all in packages this diff does not touch (9 in `cmd/agent`; 1 in `internal/tools`, `TestCovToolsNotifyAlarmRegistryIsTheSourceOfTruth`, a notify-registry test at `tasknotify_alarms_test.go:221`), and each one reproduces on PRISTINE master (`~/workspace/gopi-pid-rework-control`, detached at `9e6d7844`) — pre-existing on master, not rework regressions, while the full package run is still RED and is not called green here. `internal/tools`' routed identity surface is green independently (line above).

### Carried over from the original branch (measured there, not re-run here)

- `go build ./...` (in `~/workspace/gopi-proc-identity`) → exit 0, no output.
- `go test ./internal/procname/ -count=1` →
  `ok github.com/nick/gopi/internal/procname 0.577s` — the new
  `identity_test.go` (the unmeasurable-vs-dead contract, the incarnation-wins
  rule, the worker-vocabulary agreement) plus `testmain_scratchroot_test.go`.
- `go test ./internal/tools/ -count=1` (branch-scoped: this is the package the
  routing touches) →
  `ok github.com/nick/gopi/internal/tools 31.865s` for the identity surface
  (`TestTaskStatus*`, `TestCallersOfOneQuestionAgreeForTheSharedRecord`,
  `TestUnmeasurableIsNotADeathForEveryCaller`, `TestIdentityPredicateIsTheOnlyLadder`,
  `TestTaskShow*`, `TestProcIdentity*`). The new `TestIdentityPredicateIsTheOnlyLadder`
  is the mechanical Rule 13/14 gate: no production line in the package compares a
  recorded starttime against a live one, with a read-floor so a broken glob cannot
  pass vacuously.
- `go test ./internal/testutil/ -run 'Triple|Reuse|Procname' -count=1` →
  `ok github.com/nick/gopi/internal/testutil 0.627s` (the pid-reuse red proof, with
  the repaired assertion).
- `go test ./internal/testutil/ -run 'TestEveryTempUsingSuiteIsPinned|TestPinCoverageTable' -count=1`
  → `internal/procname` no longer reported (the Rule 22 pin landed); the only
  remaining hit is `internal/heavytest`, which is reported identically at the base
  — see the pre-existing list below.
- `go test ./internal/scratch/ -count=1` → `ok github.com/nick/gopi/internal/scratch 23.206s`
  (the checkout-occupancy implementation this epic leaves alone).

### Pre-existing failures, measured at the BASE (`a9a96cd7`, detached worktree `~/workspace/gopi-procid-base`)

Every failure below is reproduced on the base commit, so none is a regression from
this branch. Each was measured, not assumed:

- `internal/standards` → `FAIL`. Rule 17 reports **the same single NEW violation on
  both trees**: `cmd/agent/merge_lock_test.go:200 [os.Setenv/os.Unsetenv in a test]`,
  and the summary line reads identically (`hits=115 baselined=91 escaped=23 NEW=1`).
  `cmd/agent` is not in this branch's diff (`git status --porcelain cmd/agent/` is
  empty). Every other gate on both trees reads `NEW=0`.
- `internal/tools`: `TestFinishCleanupChainCleanWorktree`,
  `TestFinishCleanupChainRepoIsWorktree` → `FAIL` at the base too, with the same
  message (`The branch 'feat/sync-conflict-completed' is not fully merged …`), i.e.
  a branch-naming collision in the fixture, not an identity answer.
- `internal/testutil`: full-package run on the branch →
  `FAIL` with exactly **the same six** tests as the base: `TestPinCoverageTable`,
  `TestScriptsAreBoundedAndOffTmp`, `TestEveryTempUsingSuiteIsPinned`
  (internal/heavytest only), `TestWorkerOwnershipPointerPresent`,
  `TestWorkerOwnershipGateStaysGreenAcrossSuccessiveMergeRounds`,
  `TestWorkerOwnershipSlashPairDocIsNotAnOffender`.
- `internal/tools`: `TestAssertionTotalityAuditGate` → `FAIL` at the base too
  (it reports `over-broad` verdicts for assertions it classifies, several marked
  `OWNED BY 01a0aab8` — an in-flight audit, not an identity answer). Plus the two
  `TestFinishCleanupChain*` above.
- `cmd/agent`: `go test ./cmd/agent/ -run 'ResumeGate|Takeover|Identity|Worker|Zombie'`
  → `FAIL`. NOT base-confirmed: the failures are worker-ownership / worktree-enrolment
  e2e tests that shell out to a real `gopi`, and the run's own log shows the host
  condition (`cwd repo /home/user/work/GoPi not enrolled — the ledger this process
  reads names no row carrying it`). They exercise no identity caller this diff
  routes, and `internal/tools`' equivalents of those tests pass here. Recorded as
  observed, not as cleared.

### One measured non-result, disclosed

The first full-package smoke ran while **its own worktree was deleted underneath
it**, so its `internal/tools` result (601s, a single `--- FAIL:
TestAssertionTotalityAuditGate` line) and its `internal/testutil` extra failure
`TestRealWorkerIncarnationMatchesProcStat` were produced against a tree that
stopped existing mid-run. Both were re-measured after recreating the worktree from
the branch (`git worktree add … fix/proc-identity-single-predicate`, the commits
were intact): `TestRealWorkerIncarnationMatchesProcStat` **passes** in isolation
and in the full package, and the branch's `internal/testutil` failure set is now
byte-identical to the base's. The single `--- FAIL` line in that 601s tools run is
also why the tools totals in it are not quoted as evidence.


### The two regressions the epic's own suite caught (fixed in-branch)

1. `TestTaskStatusDeadForReapedChild` / `TestTaskStatusReportAndJSON` → the legacy
   no-record fallback in `taskIdentityVerdict` had lost its `pidAlive` rung, so a
   REAPED holder pid read `recycled` instead of `dead` — which changes what
   `gopi resume` is offered. The rung is restored verbatim.
2. `TestEveryTempUsingSuiteIsPinned` → the new `internal/procname` test suite used
   `t.TempDir()` with no scratch-root pin (Rule 22). Fixed in `3d9547c1`.


## Recovery (Rule 6d)

- Branch: `rework/proc-identity-single-predicate` (linked worktree, shares the
  main repo's object store — the commits are already in `/home/user/work/GoPi`).
  `master` is an ancestor of it, so a fast-forward merge succeeds.
- `git show rework/proc-identity-single-predicate:devdoc/proc-identity-single-predicate.md`
- `git show rework/proc-identity-single-predicate:internal/procname/procname.go`
- Full diff: `git diff master...rework/proc-identity-single-predicate`
- The original branch's preserved commits (`eb887c5c`, `3d9547c1`, `d05528c6`,
  `dcf16706`) are reachable from this branch's history.
