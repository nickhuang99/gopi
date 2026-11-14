<!-- doc-class: historical -->
# Task Report: merger repeat-dispatch of resolvers (bug 01a089b7)

Branch `feat/merger-resolver-dedup` (merged to master as `1983c3a`) +
`fix/resolver-dedup-correction` (this correction).
Design/root-cause detail: `devdoc/merger-resolver-repeat-dispatch.md`.

## Completed

- **Root cause from the ledger, not the narrative — and the narrative was
  wrong.** Pulled `~/.gopi/memory.db` task rows + the resolver session JSONLs.
  Two filed claims are refuted by the data:
  - the branch got **two resolvers** — **false**: the `[auto]` row the PA
    cancelled as a "duplicate resolver" carries `session_id = 98d7ba16`, i.e.
    it is the **same** resolver child's auto-registration row (Defect A); and
  - **two merger processes** were alive (Defect C) — **refuted**: pid 3128439
    is that child's own pid (`TaskStampSelfClaim` = `os.Getpid()`). Defect C is
    withdrawn; bug `01a089ce` should be closed invalid.

  The real waste came from two defects, both fixed here:
  - **A. The auto-register kill switch was inverted and never worked.**
    `cmd/agent/merge_resolve_child.go` wrote `GOPI_AUTO_TASK_REGISTER=1`, while
    the reader (`internal/agent/taskregister.go`) is **default-ON, off only on
    a declared OFF value** — so every resolver child minted its own
    `[auto] … conflict resolver` row, created `in_progress` and never closed
    (the "zombie rows"). `git_blame` proves it was wrong since the file's first
    commit (`bbc7503`, 2026-09-05). Second hole: `append(os.Environ(), …)`
    cannot override an inherited key (`os/exec` keeps the **first** occurrence).
  - **B. An error return from the resolver was trusted as "nothing landed".**
    The resolver committed its resolution (`19a71da`, 12:51:15) and then polled
    the repo's hanging full suite until the merger's 20-min cap killed it at
    13:08; the merger recorded *"child exited signal: killed (branch left
    untouched, retried next tick)"* while git held the finished resolution — so
    the branch was re-resolved from scratch. Git (the branch tip) is the only
    honest arbiter, and the **briefing** is what made the failure likely: it
    asked for the full suite **before** the commit.
- **Fix A** — `mergeResolveAutoRegisterOff = "0"` (named constant, Rule 1) plus
  `mergeResolveChildEnv(branch)`, which **replaces** the env keys in place
  instead of blind-appending (closes the first-wins hole).
- **Fix B (dispatch guard)** — `resolveRowHasLiveResolve(repo, branch)`
  (merger-owned `merge-conflict,auto-spawn` row `in_progress` **and** a live
  owner; unknown owner = conservative in-flight; dead owner = not in flight so
  a crashed merger never deadlocks a branch) + a new first case in the sweep's
  resolve switch that skips and counts `ResolveInFlight`, plus the merger
  summary line `resolve-in-flight=N`.
- **Fix B (landed-resolve arbitration, commit `5846f5c`)** — the sweep records
  the pre-dispatch branch tip and, when the resolver errors, probes it
  (`repoSweepBranchTipAdvanced`): a **proven advance** means the resolution is
  committed, so the row is finished `failed` with the distinct
  `resolveRowReasonResolveLanded` (never the plain-failure reason that says the
  work must be re-done) and the branch is left to the next tick's ordinary
  merge — no second resolver, no re-detect window. An unchanged / absent /
  unknown tip keeps the historical full-retry semantics (conservative). The
  merger's own error texts no longer claim "branch untouched"; the summary
  reports `resolve-landed=N`.
- **Fix B (briefing, commit `5846f5c`)** — `MergeResolveBriefing` now orders
  **COMMIT FIRST** (the commit is the deliverable; a fixed session cap means an
  uncommitted resolution is lost work), then a **bounded** verification (build
  + touched-package tests), states that a full-suite run is **NOT** required
  and that a hanging check never blocks the exit, and the ACCEPTANCE line is
  the COMMITTED resolution.
- **Fix D (found while re-verifying the merged tree)** — a **landed** resolve
  (resolver committed, then died) was finished with status `failed`, and
  `resolveRowFailedCount` counts exactly the merger-owned rows with status
  `failed` — so three landed resolves for one branch pushed the counter to
  `resolveRowMaxFailedBeforeRework = 3` and the sweep escalated a branch whose
  every resolve had actually SUCCEEDED: branch-rework submit, and (after a
  concluded rework) the `archive/<branch>` conflict-loser treatment. The landed
  row is now finished `taskStatusCompleted` with the unchanged distinct
  `resolveRowReasonResolveLanded` reason; only a resolver that failed without
  landing keeps `failed` and feeds the counter. Regression tests
  `internal/tools/repo_resolve_landed_escalation_test.go`
  (`TestMergerRepoSweep_LandedResolvesNeverEscalate` — three real landed cycles
  through the sweep, asserting no rework/archive; `…_LandedResolveRowCountsAsSuccess`
  — row-level pin + an in-test negative control proving the counter still sees
  genuine failures).
- **Fix direction (b) from the filing is already satisfied by design**: `Resolve`
  is **blocking** in the merger, so the sweep merges the branch in the *same*
  tick right after the resolver commits — there is no re-detect window.
  Direction (c) likewise already holds: on child failure/timeout the merger's
  own row is finished `failed` with the child's error text; the *orphan* rows
  were the `[auto]` ones, which Fix A stops minting.
- **Regression tests** (all green, committed):
  `internal/agent TestAutoTaskRegisterOffValueVocabulary` (pins the OFF
  vocabulary — `"1"` is **not** OFF), `cmd/agent
  TestResolveChildAutoRegisterValueIsOff` +
  `TestMergerResolveChildEnvDisablesAutoRegister` (runs the **real spawn path**
  with a stub child that dumps its env: asserts the child's
  `GOPI_AUTO_TASK_REGISTER` is OFF, the role marker is the right branch, and
  the key appears **exactly once**), `internal/tools
  TestMergerRepoSweep_SkipsBranchWithLiveInFlightResolve` (2 sweeps: in-flight →
  0 dispatches, row stays `in_progress`, branch intact; once terminal → normal
  resolve+merge — the no-deadlock counter-case) +
  `TestResolveRowHasLiveResolve_DeadOwnerIsNotInFlight`.
- **Negative controls (real sensitivity, not vacuous tests)**, each fix reverted
  **in isolation** on the committed tree and restored after:
  `mergeResolveAutoRegisterOff = "1"` → the two `cmd/agent` tests FAIL;
  in-flight guard short-circuited → the sweep test FAILS with
  *"sweep dispatched 1 resolver(s) for a branch with a live in-flight resolve,
  want 0"*.

## Not Done

- **Defect C (two merger processes / serve-lock enforcement)** — **withdrawn**:
  later evidence refuted it (pid 3128439 was the resolver child's own pid, not
  a second merger; see the CORRECTION in `merger-resolver-repeat-dispatch.md`).
  Bug `01a089ce` should be **closed as invalid**. `acquireServeLock()`'s
  singleton guarantee is still worth a direct test as hardening, but it is not
  this bug's cause.
- **`feat/hippocampus-host` timeout-then-retry was not changed** — on the
  evidence, retrying after a genuinely dead child is correct; only the
  in-flight bookkeeping was missing.
- **No liveness/progress signal for a slow resolver** — a resolver is still
  killed at the 20-min cap (`mergeResolveChildWaitTimeout`) even if it is making
  progress.
- **Whole-package suite state (honest)**: `go test ./internal/tools/` as a whole
  package does not complete — a long-documented pre-existing full-suite hang
  (bug `t-1788361069`; now also filed as `01a089cf`). Targeted runs are the
  gate, exactly as the recorded memories prescribe.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK
- Vet:  `go vet ./internal/tools ./cmd/agent ./internal/agent` → clean
- Tests (targeted gate; the whole-package `internal/tools` run is a
  pre-existing hang, see *Not Done*):
  - `go test ./cmd/agent/ -run 'AutoRegister|ResolveChild|Merger' -count=1` → `ok 12.1s` (18.4s on the merged tree)
  - `go test ./internal/agent/ -run AutoTaskRegister -count=1` → `ok 20.0s`
  - `go test ./internal/tools/ -run 'Resolve' -count=1` → `ok 61.8s` (includes the
    in-flight guard + landed-resolve tests)
  - `go test ./internal/tools/ -run 'MergeResolveBriefing' -count=1 -v` → `PASS` (2/2)
  - `go test ./internal/agent/ -count=1 -timeout 15m` → `ok 156.0s`
- Landed-resolve tests (`internal/tools/repo_resolve_landed_test.go`), 3/3 PASS:
  `TestMergerRepoSweep_ResolverCommittedThenDied_KeepsResolution` (tip advanced +
  resolver error → `ResolveLanded=1`, row reason is the *landed* one, branch
  survives, and the **next tick merges it with no second resolver** — the
  resolver stub calls `t.Fatalf` if it is ever invoked again),
  `TestMergerRepoSweep_ResolverFailedWithoutCommit_RetriesInFull` (negative
  control: no tip advance → unchanged plain-failure retry) and
  `TestRepoSweepBranchTipAdvanced_Conservative` (absent branch / empty pre-tip /
  unchanged tip never report an advance).
- Briefing tests (`internal/tools/repo_resolve_briefing_test.go`), 2/2 PASS:
  `TestMergeResolveBriefing_CommitsBeforeVerifying` (COMMIT FIRST precedes
  THEN-verify precedes ACCEPTANCE; the bounded commands are named) and
  `TestMergeResolveBriefing_NoFullSuiteAcceptance`.
- Negative controls: see above — FAIL as expected when each fix is reverted.
  The landed-resolve guard was also reverted in isolation:
  `landed/resolved/merged = 0/0/0, want 1/0/0`. The briefing control was
  reverted on the committed tree (old wording restored) → `FAIL` with
  *"briefing still makes a full test-suite pass an acceptance bar"*, then the
  tree was restored and re-run green.
- Mergeability: `git merge-tree $(git merge-base master HEAD) master HEAD`
  (git 2.34.1 form) → 0 conflict markers.
- Commits: `7e968d1` (fix A+B), `5044deb` + `75b5c18` (devdoc), `ded83fc`
  (report), `5846f5c` (landed-resolve + briefing) — merged to master as
  `1983c3a`; this correction on `fix/resolver-dedup-correction`:
  `a4b482a` (briefing tests) + `1abfb21` (root-cause correction in report +
  both devdocs + in-flight test touch-up).
- **Correction-round gate** (whole deliverable, run on `1abfb21`):
  `CGO_ENABLED=0 go build ./cmd/agent/` → BUILD_OK; `go vet ./internal/tools/
  ./cmd/agent/` → VET_OK; `go test ./internal/tools/ -run
  'Briefing|Resolve|InFlight|ResolveLanded' -count=1` → `ok 43.8s`;
  `go test ./cmd/agent/ -run 'Resolve|Merger' -count=1` → `ok 5.4s`.
- **Fix D gate** (final round, on this working tree):
  `CGO_ENABLED=0 go build ./...` → BUILD_OK; `go vet ./internal/tools
  ./cmd/agent ./internal/agent` → VET_OK; `go test ./internal/tools/ -run
  'Rework|Resolve|Briefing|InFlight|Inflight|Landed|TipAdvanced' -count=1
  -timeout 900s` → `ok 55.2s` (includes the three pre-existing rework-threshold
  tests as the counter's positive control); re-run after the negative control →
  `ok 27.0s`; `go test ./internal/tools/ -run 'Resolve|AutoRegister'` on
  `cmd/agent` → `ok 3.7s`.
- **Fix D negative control** (reverted in isolation:
  `resolveRowFinish(rowID, taskStatusCompleted, resolveRowReasonResolveLanded)` →
  `taskStatusFailed`): `TestMergerRepoSweep_LandedResolvesNeverEscalate` FAILS
  with `resolveRowFailedCount = 3 after 3 LANDED resolves, want 0`, and
  `…LandedResolveRowCountsAsSuccess` FAILS with `status = "failed", want
  "completed"`; tree restored and re-run green.
- **Final merge-readiness gate vs CURRENT master `41f1158`** (a REAL merge, not
  a merge-tree prediction — a temp worktree branched from `41f1158` and merged
  `fix/resolver-dedup-correction` with `--no-ff`):
  `git merge --no-ff` → **no conflicts** (`git diff --name-only
  --diff-filter=U` empty), 6 files / +281 −4; then on the merged tree
  `CGO_ENABLED=0 go build ./...` → OK; `go vet ./internal/tools ./cmd/agent
  ./internal/agent` → clean; `go test ./internal/tools -run
  'Rework|Resolve|Briefing|InFlight|Inflight|Landed|TipAdvanced' -count=1` →
  `ok 27.9s`; `go test ./cmd/agent -run
  'Merger|ResolveChild|MergeResolve|AutoRegister|Register|Singleton' -v` →
  6/6 PASS (`TestResolveChildAutoRegisterValueIsOff`,
  `TestMergerSweepRepoSelfResolvesConflict_SpawnPath`,
  `TestMergerResolveChildEnvDisablesAutoRegister`,
  `TestMergeResolveEnv_DisablesAutoRegister`,
  `TestMergerSingleton_TwoConcurrentInvocations`,
  `TestMergerOnceLeavesPendingTaskUntouched`); `internal/tools` landed/briefing
  set 6/6 PASS + `internal/agent -run AutoTaskRegister` → `ok 8.9s`.
  The temp branch (`tmp/mergecheck-master`) and its worktree were removed after
  the gate; no stray branch or worktree remains.
- **Correction commits** `a4b482a` + `1abfb21` add no production-code change
  (the shipped fixes A/B/landed-resolve/briefing are untouched): they pin the
  corrected root cause and the briefing contract with tests, so the
  false "two resolvers / two mergers" narrative cannot silently return.
  `59661de` is the Fix D commit (landed row → `completed` + escalation
  regression tests + docs): the first production-code change of this
  correction round, made because re-verification of the merged tree exposed
  the counter defect above.

## Merge state (honest, verified against master `41f1158`)

What is on master and what is not — checked with `git branch --contains` and
by reading the master blobs, not from memory:

- **ON master** (`8a0b99b` = merge of `8a87210`): Fix A
  (`mergeResolveAutoRegisterOff = "0"` at `cmd/agent/merge_resolve_child.go:72`),
  Fix B (`resolveRowHasLiveResolve` guard at `internal/tools/repo_sweep.go`),
  the landed-resolve arbitration + COMMIT-FIRST briefing (`5846f5c`, in via
  `1983c3a`), and their tests (`repo_resolve_inflight_test.go`,
  `repo_resolve_briefing_test.go`).
- **NOT on master — this branch only** (`fix/resolver-dedup-correction`,
  4 commits ahead at `c7e9411`: `59661de` fix, `4e10552` + `2ac1822` reports,
  `c7e9411` this section): **Fix D** (`59661de`, landed resolve → `taskStatusCompleted`
  so it stops feeding `resolveRowFailedCount` → `resolveRowMaxFailedBeforeRework`
  → branch-rework/archive), its regression file
  `internal/tools/repo_resolve_landed_escalation_test.go` (absent on master),
  and the two devdoc updates.

So the epic's *duplicate-dispatch* half (A + B + landed arbitration) is live in
production; the *escalation* half (Fix D) still needs the merge. Master and this
branch differ by 6 files (M `repo_sweep.go`, M `repo_resolve_row.go`,
A `repo_resolve_landed_escalation_test.go`, M `repo_resolve_landed_test.go`,
M 2 devdocs); no conflict — proven by a real `--no-ff` merge in a temp worktree,
not a merge-tree prediction.

**Zombie rows**: the ledger carries no open resolver row any more — the
surviving `[auto] … conflict resolver` rows is 1630/1633/1635/1615/1613, all
`cancelled`, and no resolver in `pending`/`claimed`/`in_progress`. Fix A is what
keeps it that way. (Row 1639, the unrelated `internal/tools` whole-package hang,
is the one open non-terminal row and is a separate filed bug.)

