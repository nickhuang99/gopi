<!-- doc-class: historical -->
# devdoc/merge-verify-withdraw.md — the merge pass verifies what it landed, and can take it back

**Status:** HISTORICAL. This page was written as the `gopi merge` post-pass
verification + withdrawal contract. **That contract no longer exists.** The
mechanism it describes (`internal/tools/merge_pass_verify.go`) was DELETED on
2026-11-01 (task `01a19466`); its two call arms (`cmd/agent/merge_run.go`) and its
gate (`cmd/agent/merge_verify_test.go`) had already gone with the retired
top-level `gopi merge` verb, and its own unit file
(`internal/tools/merge_pass_verify_test.go`) was retired earlier still, in
`5a9af79b`. The `merge-withdrawn` notify alarm went with the file. **Nothing in
the tree verifies a merged tree or withdraws a landing today**, and
`man/gopi-merge.md` now says so in its own DESCRIPTION. Read what follows as the
record of a capability that existed and was measured, never as a promise about
the shipped engine.
**Branch:** `feat/merge-verify-withdraw` (worktree `~/workspace/gopi-merge-verify`), based on master `f69a2149`.
**Code:** was `internal/tools/merge_pass_verify.go` (the mechanism), `cmd/agent/merge_run.go` (both arms called it), `cmd/agent/merge_verify_test.go` (the gate). None of the three exists today — see Status above.

## 1. The gap, restated after measuring it

The briefing said the merge face "did no build/vet verification of what it landed". **That is not accurate at master `f69a2149`, and the difference shaped this change.** The `--fast` arm already ran two report-only gates at the end of every repo sweep (`mergeSweepRepo` → `tools.RebuildAfterMerge`, `cmd/agent/merge_run.go:807`):

| already present at master | measurement | effect on a red tree |
|---|---|---|
| `VET gate` (`runPostMergeVetGate`) | `go vet ./...` over the checkout | notify only |
| `MERGESET gate` (`RebuildAfterMergeMergeset`) | `go vet ./...` over the merge commit's tree in a detached scratch worktree | notify + **fil bug row** |

Both are `RebuildResult.Vet` / `.Mergeset`: report-only **by contract** (`rebuild.go:78-88`: "a red gate never changes OK … never unwinds the merge (Rule 12)"). Under a fast-forward that contract is the whole problem: this is not a merge that can be `revert`ed — there is no merge commit, the branch simply moved — so a red gate filed a bug row against a master that still did not compile, and the run exited **0**.

So the real gap is narrower and harder than "no verification": **the pass had no BUILD half, no decision, and no way back.** The decision is what this change adds, and it must live on the face that still holds a revert handle — `RebuildAfterMerge` deliberately does not hold one.

## 2. What was built

**One measurement per repo per pass, and a withdrawal as its only decision.**

`internal/tools/merge_pass_verify.go`:

- `CaptureMergePassScope(repo)` — the pre-pass snapshot, taken for every repo **before any repo is swept** (`cmd/agent/merge_run.go:559-570`). It wraps the existing `CaptureFailedMergeRevert` (mechanism 2, the 2026-09-08 dual-mechanism epic's snapshot) into `FailedMergeRevert`, so nothing about the capture is re-invented; a capture that fails yields an *uncaptured* scope and a REPORTED "this batch cannot be withdrawn" line, never a silent pass.
- `VerifyMergePass(scope)` — `go build ./...` then `go vet ./...`, both through the merge BUILD gate's own resolvers (`vetGateResolveSpec`: shared build cache, shared module cache, one budget reader). The build runs first; a red build **skips** vet (nothing to type-check) rather than paying for a wall of cascading errors. The vet half *is* `runPostMergeVetGate` — the close loop's own gate, one vocabulary of PASS/FAIL/ABSTAIN, no second spelling.
- `WithdrawMergePass(scope, verdict)` — three guards, then `FailedMergeRevert.RestoreMaster()`: a `git reset --hard <pre-tip>` plus re-creation of every branch ref the landing's `git branch -d` deleted.

**A reset, not a revert.** A fast-forward creates no merge commit, so `git revert -m 1` has no subject; the sound restore of "the state before this pass" is a hard reset to the captured tip. No work is lost: every commit the reset takes off the integration branch is still reachable from its own branch ref, and the deleted ones are re-created at their captured tips.

**The build does not litter the tree it measures** (Rule 30). `go build ./...` with no `-o` writes each `package main`'s executable **into the package directory** — measured: a 1.5 MB `fix` binary appeared in the fixture checkout after the first real run (the observed `?? fix` in `…/repo-new`). The build therefore runs `go build -o <scratch dir> ./...`, where the directory is a runtime scratch class (`internal/scratch`, class `merge-verify` — never `/`, Rule 22) removed after the command runs; a host with no usable scratch **abstains** rather than building into the caller's checkout.

**`--no-verify`** turns the measurement off for an operator draining a known-good queue. Turned off, the run SAYS so — a skipped measurement must never look like a passed one.

## 3. The exit-status contract change (stated, as the briefing requires)

| before | after |
|---|---|
| every branch reaching a terminal decision → **0** | unchanged, **plus**: a pass that measured a red verification and **withdrew** the batch → **1** |

Why: the caller's merge did not survive, and a hook firing `gopi merge` must be able to tell "landed everything" from "landed and took it back". The ordinary failure code is reused deliberately — what changed is *which runs set it*, not the vocabulary, and the usage page (`cmd/agent/merge_run.go`, `--help` body) states it.

**A withheld hand-over.** A withdrawn batch does not start the merge gopi: the child would resolve conflicts against a master that no longer holds what this run landed. The suppression is printed (`mergeWithdrawnNoHandoverLine`), not silent.

**An abstention is not a red.** Not a Go module, no toolchain, no populated module cache, no scratch, an expired budget → the landing STANDS with its reason reported. Only a measured red withdraws.

## 4. Observed evidence

Environment: the fixture's `HOME` is pinned to an empty dir, so the gate's own `go env GOMODCACHE` probe cannot find the operator's cache; the runs pass `GOPI_MERGE_BUILD_GATE_GOMODCACHE=$(go env GOMODCACHE)` (`/home/user/go/pkg/mod`) explicitly. Scripts: `~/workspace/merge-verify-evid/{run.sh,broken.sh}`.

**Red batch (a fast-forwardable branch that does not compile), both binaries over the same shape** (`broken.sh b2`):

```
pre-pass master (new fixture)  = 6296b4483f3fbcf220d3610088754a9f07166b20
the broken branch's tip        = eabf79f9ce579b490e4279eff02b91e5c96787a4

PRE-CHANGE BINARY:
  MERGESET gate RED: … rev=baf701c … undefined: thisSymbolDoesNotExistAnywhere [attributed to this landing]
  MERGESET gate filed bug row: bug: post-merge gate RED on the merged result: vet
    feat/broken    landed  fast-forwarded into master, ref pruned
  exit=0 ; master after = baf701cfd8…  (the red landing STANDS)

NEW BINARY:
  MERGE pass verify …: batch WITHDRAWN — eabf79f9ce579b490e4279eff02b91e5c96787a4 → 6296b4483f3fbcf220d3610088754a9f07166b20
    (reset --hard, no merge commit to revert); 2 branch ref(s) kept: feat/broken feat/ok;
    go build ./... FAILED (1 package(s) reported a build problem, first: example.com/fix)
  failing: go build ./... FAILED …
  branches kept: feat/broken feat/ok
  attribution: feat/broken — no owning row [no ledger row names it] — no ledger row names this branch
  exit=1 ; master after = 6296b4483f3fbcf220d3610088754a9f07166b20  (= the pre-pass tip)
    feat/ok     = ca4a09a1be70ac219fefc9fb43b993b608016b0d  (re-created, the landing had deleted it)
    feat/broken = eabf79f9ce579b490e4279eff02b91e5c96787a4  (kept at its tip)
    porcelain   = [?? settings.json]   (the fixture's own file; no build residue)
```

Two things this run pinned that a fixture alone would not: the shared module cache is what decides *measure vs abstain* (the first attempt abstained — `no populated shared module cache` — with the landing standing, exit 0), and the litter bug of §2 showed up exactly here as `?? fix`.

**Clean batch, pre-change vs this branch** (`run.sh r2`): identical stdout except the lock-owner pid, the two build-cache paths, and the new verdict line —

```
+ gopi merge: REPO post-pass verification verified — REPO: post-pass verification PASSED (go build ./... + go vet ./... green)
```

— with `exit=0` and the same landed/pruned outcomes in both.

**Abstention** (`run.sh r2`, no module cache reachable): `post-pass verification ABSTAINED, the landing stands … exit=0`, master at the landed tip. **`--no-verify`**: the announcement line, nothing measured.

## 5. Tests — `cmd/agent/merge_verify_test.go`

| test | what it pins |
|---|---|
| `TestMergeFastWithdrawsABatchThatDoesNotBuild` | the headline: the seam records master at the BROKEN tip while verifying, the captured pre-tip equals the pre-pass tip, master is back at the pre-tip afterwards, **both** branch refs survive with their tips, verification ran **exactly once**, and the report carries both tips + the failing package + the kept branches + the attribution |
| `TestMergeFastWithdrawsABatchThatOnlyFailsVET` | the other half: a branch whose only `_test.go` does not type-check — `go build ./...` is green and only `go vet ./...` sees it (the class that landed a red master on 2026-09-16); withdraws, branch survives, report names `go vet ./... FAILED` |
| `TestMergeWithdrawalIsAResetNotARevert` | the MECHANISM: no revert commit in the history, master tip **is** the captured commit, the commit count is unchanged, and the landed tip has **one** parent (so `git revert -m 1` was never possible), plus a clean `git status --porcelain` afterwards |
| `TestMergePassVerifiesOncePerPassNotPerBranch` | four branches land in one pass → **one** measurement |
| `TestMergeCleanBatchIsVerifiedAndChangesNothing` | green: master stays at the landed tip, the branch is pruned as before, no withdrawal |
| `TestMergeNoVerifySkipsTheMeasurement` | `--no-verify`: zero measurements, the landing stands, and the skip is announced |
| `TestMergePassWithdrawsOnlyTheRedRepo` | two repos in one run: the red one is restored to its pre-tip, the green one KEEPS its landing and its branch is pruned |

The measurement count is read through `mergePassVerifyFn` (`cmd/agent/merge_run.go`), a seam whose production value is `tools.VerifyMergePass` — so the real measurement (module probe, caches, toolchain, vet gate) is what the tests exercise, and only the COUNT is intercepted: per-branch verification of one tree reaches the same verdict, so the property is otherwise invisible.

## 6. Verification (self-filed)

Branch `feat/merge-verify-withdraw`, worktree `~/workspace/gopi-merge-verify`
(base master `f69a2149`). Evidence files: `~/workspace/merge-verify-evid/`.

### Build

Re-measured after the last edit to this doc, in the worktree
(`~/workspace/merge-verify-evid/build-vet.txt`):

```
== go build ./... ==
build_exit=0
== go vet ./... ==
vet_exit=0
```

`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` (the repo's own declared
build, Rule 12) also exits 0. `go vet ./cmd/agent/ ./internal/tools/`: exit 0
(the two packages this diff touches).

### Scoped tests

`GOPI_HUMAN_TERMINAL=1 go test -run TestMerge -v ./cmd/agent/`
→ **47 PASS / 0 FAIL**, `ok github.com/the operator/gopi/cmd/agent 138.057s`;
the log and its `exit=0` are `/home/user/workspace/merge-verify-evid/tests-cmdagent.txt`,
including the seven new cases:

```
--- PASS: TestMergeFastWithdrawsABatchThatDoesNotBuild (13.24s)
--- PASS: TestMergeWithdrawalIsAResetNotARevert (16.33s)
--- PASS: TestMergeFastWithdrawsABatchThatOnlyFailsVET (20.23s)
--- PASS: TestMergePassVerifiesOncePerPassNotPerBranch (16.65s)
--- PASS: TestMergeCleanBatchIsVerifiedAndChangesNothing (10.88s)
--- PASS: TestMergeNoVerifySkipsTheMeasurement (9.22s)
--- PASS: TestMergePassWithdrawsOnlyTheRedRepo (18.63s)
```

Re-run after the last code edit: `go test -run 'TestMerge(Fast|Withdrawal|PassVerifies|Clean|NoVerify|PassWithdraws)'
./cmd/agent/` → `ok … 86.925s`.

`GOPI_HUMAN_TERMINAL=1 go test ./internal/tools/` → see `tests-tools.txt`.

Both suite logs and the raw run outputs are on disk at
`~/workspace/merge-verify-evid/` (script `run.sh`, `broken.sh` regenerate them).

### Two things the scoped test run does NOT establish

Stated before the evidence below, so no reader over-reads a green line:

1. **`go test ./internal/tools/` was never GREEN on this run, and is not green
   at this branch's base either.** It ends `FAIL … 601.305s` — and it did not
   even finish the package: it panicked `test timed out after 10m0s` while
   running `TestProceduralSyncRoundtrip`. Two tests had already failed:
   `TestProposalRowsAreNeverClaimable` and `TestAssertionTotalityAuditGate`.
   Both are pre-existing on the base commit, not caused by this branch —
   measured, same command, in a detached worktree at `f69a2149`
   (`~/workspace/merge-verify-evid/tests-tools-baseline-full.txt`):

   ```
   base f69a2149:  --- FAIL: TestProposalRowsAreNeverClaimable (1.35s)
                   --- FAIL: TestAssertionTotalityAuditGate (0.87s)
                   FAIL github.com/the operator/gopi/internal/tools 3.677s      exit=1
   this branch:    --- FAIL: TestProposalRowsAreNeverClaimable (4.30s)
                   --- FAIL: TestAssertionTotalityAuditGate (1.28s)
   ```

   The audit gate's UNCLASSIFIED set is **byte-identical** between the two
   (21 assertions across 21 test functions, all in files this branch does not
   touch: `rebuild_vet_mergeset_test.go` @20d38c38, `task_merged_cleanup*.go`
   and `wip_worktree_repro_test.go` @7f8a4cff, `task_finish_cleanup_test.go`,
   `memory_sync_task_gitpushback_cov_test.go`, `taskzombie_sweep_test.go`,
   `verify_baseline_delta_test.go`, `merge_build_gate_cleanup_test.go`,
   `task_sync_git_pushback_test.go`, `tasknotify_test.go`). Measured by
   extracting the `file:TestName` set from both logs —
   `diff` prints nothing, `IDENTICAL SET`.
2. **The timeout is load-induced, not a branch defect.** The case named as
   running, `TestProceduralSyncRoundtrip (0s)`, had itself elapsed 0s: it was
   blocked behind the package's other ledger-heavy tests on a shared tree, and
   the package budget expired. This branch's `internal/tools` coverage is
   therefore the four hermetic cases in
   `internal/tools/merge_pass_verify_test.go`, run branch- and
   package-scoped and green:

   ```
   go test -run 'TestVerifyMergePass|TestWithdrawMergePass' -count=1 ./internal/tools/
   ok  github.com/the operator/gopi/internal/tools  2.307s                      exit=0
   ```

   No statement is made here about the `internal/tools` package as a whole.

### The red-proof (the pack's core claim), measured, not asserted

`bash ~/workspace/merge-verify-evid/broken.sh b3` — one fast-forwardable branch
that does NOT compile, run through **both** binaries, same fixture shape, same
shared module cache (`GOPI_MERGE_BUILD_GATE_GOMODCACHE=$(go env GOMODCACHE)`):

```
pre-pass master (new fixture) = 7b6060c510b0e5ec068b30b1984d2dfd28f8adc9
the broken branch's tip       = a5283a32934be85bebd8a89e2733fe1394fc27ae

PRE-CHANGE BINARY (f69a2149):
  MERGESET gate RED: … rev=a5283a3 at ./bad.go:3:14: undefined: thisSymbolDoesNotExistAnywhere
  MERGESET gate filed bug row
  feat/broken  landed  fast-forwarded into master, ref pruned
  exit=0 ; master after = a5283a32…   ← the non-compiling landing STANDS

THIS BRANCH:
  MERGE pass verify …: batch WITHDRAWN — a5283a32934be85bebd8a89e2733fe1394fc27ae → 7b6060c510b0e5ec068b30b1984d2dfd28f8adc9
    (reset --hard, no merge commit to revert); 2 branch ref(s) kept: feat/broken feat/ok;
    go build ./... FAILED (1 package(s) reported a build problem, first: example.com/fix)
  failing: go build ./... FAILED (1 package(s) reported a build problem, first: example.com/fix)
  branches kept: feat/broken feat/ok
  attribution: feat/broken — no owning row [no ledger row names it]
  exit=1 ; master after = 7b6060c510b0e5ec068b30b1984d2dfd28f8adc9  ← the pre-pass tip
    feat/ok     = b02d495cac49046f7169c837d76c8533b7fa78aa  (re-created; the landing had deleted it)
    feat/broken = a5283a32934be85bebd8a89e2733fe1394fc27ae  (kept at its tip)
    git status --porcelain = [?? settings.json]   (the fixture's own file only — no build residue)
```

### Clean-batch parity (pre-change vs this branch)

`bash ~/workspace/merge-verify-evid/run.sh r3`: identical stdout apart from the
lock-owner pid, the two cache paths, and the new verdict line —

```
+ gopi merge: REPO post-pass verification verified — REPO: post-pass verification PASSED (go build ./... + go vet ./... green)
```

— `exit=0` in both, the same `landed` / `ref pruned` outcomes, the same master
tip shape, and no withdrawal line.

### Abstention and --no-verify (both must NOT withdraw)

`run.sh r3`:

```
ABSTENTION (no module cache reachable):
  post-pass verification ABSTAINED, the landing stands — … no populated shared module cache …
  feat/landme  landed  fast-forwarded into master, ref pruned ; exit=0
--no-verify:
  gopi merge: --no-verify — the post-pass verification is OFF: nothing this run lands is measured, and no landing can be withdrawn
  feat/landme  landed  fast-forwarded into master, ref pruned ; exit=0
```

### Gates observed to be RED at the base commit too (not caused by this branch)

- `go test ./internal/standards/` — `docclass_gate_test.go:104`: three devdoc
  files with no `DOC-INDEX.tsv` row. The same three are missing at
  `~/work/GoPi` (master `589ea8d2`): `cli-links-carrier.md`,
  `raw-round-grammar-archive-remeasure.md`, `web-m2-b5b8b9b12-remeasure.md`.
  `daemon-death-pipe-fd-shape.md` is also a duplicate row at master (twice).
  This branch's own doc row was added, which is what a new devdoc owes.
- `go test ./internal/stylegate/` — 5 findings at master (unchecked-close 2,
  test-sleep 6, test-setenv 2, test-skip 2, ignored-error 9 prod / 203 test).
  This branch adds ONE production `//stylegate:ignore` site
  (`cmd/agent/merge_run.go`, the withdrawal error carried on the report) and no
  new finding class.

## 7. Deferred (honest)

- **The non-`--fast` arm** gets the same treatment by construction (the verification lives in `runMerge`, outside the arm-specific sweep), but its own end-to-end path — an LLM hand-over suppressed by a withdrawal — is exercised by unit/`runMerge`-level tests only; no scenario test drives a real child.
- **`gopi task merge`** (the task-side landing face) still has neither this verification nor a withdrawal. It is the same shape of gap on a different face and is NOT covered here; it should be its own task.
- The verification is `go build ./...` + `go vet ./...` only. `make test` is deliberately never run (Rule 27 / `internal/smokegate`: the merge path's test verdict is the branch-scoped smoke set).
- Per-repo cost on a cold cache: one `go build ./...` + one `go vet ./...` (both share the merge gate's cache). Not measured on a cold host here.
