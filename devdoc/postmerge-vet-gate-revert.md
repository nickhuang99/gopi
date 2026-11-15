<!-- doc-class: normative -->
# Task Report: the post-merge VET gate stops being report-only — it unwinds a merge it can PROVE introduced the breakage

Branch: `fix/postmerge-vet-gate-revert` (worktree `~/workspace/gopi-vet-gate-revert`)

## The question, answered first

**Why was the gate report-only?** Because its author applied **Rule 12** to it, and
said so in the landing commit itself — `4fdb6684` (2026-09-16), body verbatim:

> REPORT-ONLY: never touches OK/Err, never unwinds a good merge (Rule 12).

Rule 12 governs the **close loop** (merge → cleanup → **rebuild**): *"Build failure
≠ rollback: the merge is done and good — git history is fine."* The gate took that
sentence as its contract. The reasoning is not a mistake in the abstract — it is a
mistake **once the gate can see a defect the landing build cannot**, which is
exactly the case it was written for.

### The side-by-side the epic asked for

| | red **BUILD** (Rule 12's subject) | red **VET** (this task's subject) |
|---|---|---|
| what is broken | the **artifact** master's contract says it produces | **master's own source tree** |
| who is blocked | every consumer of the deployed binary, now | the next `go test ./...` in that repo |
| is master broken? | **NO** — master still IS what the round made it; the *binary* did not follow | **YES** — master does not hold together |
| remedy | none (the merge is good; the build is a separate failure) | take the merge back; the branch's work is preserved |
| Rule 12 verdict | a notify, never a rollback | a rollback **is** the remedy — for a branch-introduced red only |

The discriminating question is not "how bad is it" but **"is the thing master now
holds defective on its own terms?"** A failed `go build` is a statement about the
artifact. A red `go vet` is a statement about master's own content, and master's
contract with the rest of the merge queue is that `go test ./...` works there.

**What that cost while the gate stayed report-only** (measured, not theorised):
2026-09-17, `syncTaskMigrateFrom` — a merge left `internal/tools`'s **test** package
uncompilable, `go build ./...` stayed **green** (so the round declared success), the
gate went red and notified, and master stayed broken until a human reverted by hand
(`9a10a844` is the re-land on a fresh branch, `830b025b` the repair note).

## Completed

- **The determination** above, with the landing commit's own words and the
  build-vs-vet table (`internal/tools/rebuild_vet_revert.go` header).
- **Comparative classification** (`classifyVetFailure`): a red gate is measured
  against the **recorded pre-merge tip**, in a temporary **detached worktree** under
  the merge-build scratch class (never `/tmp`), reusing the merge-time build gate's
  resolvers (shared build cache, shared module cache, same env, same bound, same
  worktree reap). Three outcomes, and only one may unwind:
  - `introduced` — pre-merge tip **vets**, merged tree does not → **revert**;
  - `pre-existing` — the tip was **already red** → **keep**, quoting the baseline's
    own finding so "pre-existing" is an observation, not an assertion;
  - `unknown` — no recorded tip / a tip from another repo / an unreadable revision /
    no scratch tree → **keep** (fails safe; per the 2026-09-13 lesson an unjudged
    result is *not* a failure, and that gate's absolute-redness rule reverted six
    honest branches in an hour).
- **The unwind, wired into the merger's own close loop**: `ClassifyVetGateRevert`
  decides and reports; `RevertIntroducedVetFailure` calls the **existing**
  `FailedMergeRevert.RestoreMaster()` (branch refs re-created, `reset --hard` to the
  recorded tip, the round's minted `version/*` tag dropped). No new revert machinery.
- **The ordering** (the mechanical half of the design): `cmd/agent`'s `serveRebuild`
  asks the pre-build half (`tools.RebuildAfterMergePrebuild`) **before** the build,
  because a revert restores the pre-merge tip and a build issued afterwards would
  stamp a binary at a merge master no longer has. It runs *after* the dirty-tree
  guard: a dirty tree is not ours to measure.
- **`RebuildAfterMerge` keeps its Rule 12 contract** — still report-only, never
  touches `OK`/`Err`, never unwinds. The pre-build half is a separate narrow call so
  no existing caller's signature changed.
- **Tests**:
  - `internal/tools/rebuild_vet_revert_test.go` — both arms through the real
    toolchain and real git revisions; a differential test that fails if the two arms
    collapse into one; the no-baseline/unreadable-revision/mis-wired-handle cases;
    the guard's named refusals; the seam's disclosure; the wire spellings.
  - `cmd/agent/cli_tasks_rebuild_vet_revert_test.go` — both arms driven through the
    **real close loop** (`serveRebuild`), asserting the disposition on the **repo**
    (master's tip), which is the only place an ordering or wiring regression is
    visible.
- **A contradiction found and fixed by the tests** (worth recording, and it took
  two tries — the first fix was wrong): the gate's own `Summary()` hardcoded
  `— merge kept, this is a REPORT` for **every** failure. The naive fix was to render
  the sentence from the classifier's answer — but the tests immediately showed that
  is impossible: the gate's failure line is delivered the moment the gate runs,
  **before** a classification can exist (attributing the red needs a *second*
  measurement of the pre-merge tip). The root-cause fix is the split that follows
  from that timing:
  - the gate announces the **MEASUREMENT** (`build-failed`: gate, repo, position,
    counts — no disposition in the sentence, because none is known yet);
  - the classifier announces the **DISPOSITION** once it knows — kept with
    `ReportOnlyBanner`, reverted with `RebuildVetRevertedWord` — through the event
    the operator already subscribes to.
  Each sentence is now true at the moment it is sent; before, a round that was in
  fact reverted still told a subscriber "merge kept". The test asserts the
  contradiction directly: the reverted round's payload contains the banner
  **nowhere**.
- **Devdoc**: this file.

## Verification

- Build: `go build ./...` — **OK** (exit 0, no output).
- Vet: `go vet ./internal/tools/ ./cmd/agent/` — **clean** (exit 0, no output).
- gofmt: the three touched files are `gofmt`-clean (`gofmt -l` lists only
  pre-existing unrelated files in those packages).
- Tests (branch-scoped, the diff's two packages):
  - `go test -count=1 -run 'TestVetGateRevert|TestPostMergeVetGate' ./internal/tools/`
    → `ok github.com/the operator/gopi/internal/tools 189.326s`
  - `go test -count=1 -run 'TestServeRebuild' ./cmd/agent/`
    → `ok github.com/the operator/gopi/cmd/agent 33.809s`
- `go test -count=1 ./internal/standards/` → `ok ... 22.597s` (after registering the
  census arm it correctly reported — see Evidence).
- Commit: see the final message's deliverable declaration.

## Evidence (self-filed)

Observed by this run, quoted as it appeared:

- `go build ./...` → `exit=0`, no output.
- `go vet ./internal/tools/ ./cmd/agent/` → `exit=0`, no output.
- The **pre-existing** arm's own delivered payload (a `go test -v` line from the
  notify sink during `TestVetGateRevert_KeptRenderingsCarryTheReportBanner`):
  `VET gate NOT attributed to this merge (pre-existing): ... the pre-merge tip
  6e9cb0811507740792ef814931fa0e01ca8364fc was ALREADY vet-red before this merge
  (p/z_broken.go:4:17: undefined: vgrNeverDefinedSymbol vs
  p/z_broken.go:4:17: undefined: vgrNeverDefinedSymbol) (merge kept, this is a REPORT)`
  — i.e. the comparison measured the SAME finding in both trees and kept the merge.
- The **introduced** arm's notify, seen in the same run:
  `VET gate INTRODUCED BY THIS MERGE: ... p/z_broken.go:4:17: undefined:
  vgrNeverDefinedSymbol (1 finding(s), 0 package(s)) — reverting the landing (the
  branch's work is preserved)`.
- The contradiction the tests caught, in the run BEFORE the fix (verbatim from the
  failing assertion's dump): the payload carried
  `"summary":"post-merge gate FAILED: ... (1 finding(s), 0 package(s) — merge kept,
  this is a REPORT)"` on the round the classifier had classified `introduced`.
- A second fixture bug the FIRST cmd/agent arm run caught, verbatim:
  `cli_tasks_rebuild_vet_revert_test.go:157: the merge must be KEPT (master advanced
  past 53eaab35...), but the tip is still 53eaab35...` — the pre-existing arm's
  branch carried no commit, so `git merge --no-ff` reported "Already up to date" and
  there was no landing to judge. The fixture now gives that branch a benign commit
  (a real landing, the red still master's).
- The census gate caught a REAL omission this change introduced:
  `cwd_release_gate_test.go:742: census derived 1 removal arm(s) ... [ClassifyVetGateRevert]`.
  Correct report: the classification reaches a scratch-worktree reap
  transitively, so the arm is now recorded in `cwdReleaseArmsNotListed` with its
  reason (same private-scratch class as TaskMerge/TaskVerify).
- `go test ./internal/standards/` → `ok github.com/the operator/gopi/internal/standards 22.597s`.
- `go test ./internal/stylegate/` → **FAIL**, but on lines this diff does not touch
  (`cmd/agent/merger_restart_e2e_test.go`, `cmd/agent/merger_takeover_*`); none of
  the three modified files appear in its reported sites. Reported, not claimed as
  mine.
- Test verdicts (both suites): recorded from the `go test` runs in the section below.

## Not Done (honest)

- **The gate is still not a BLOCKER.** It unwinds; it never refuses a merge in
  advance. That is deliberate (the merger's detect-never-block contract), but it
  means the cost of one round of breakage is still paid before the revert lands.
- **The baseline costs a second `go vet`** (a detached checkout + a warm-cache run).
  It only runs when the gate is already RED, so the steady-state cost is zero — but
  it is a real cost on a red round, and it is NOT yet an incrementally-cached
  measurement.
- **`Builder`/`-race` are not covered**: the gate runs `go vet ./...` only. A tree
  that vets but whose TESTS fail to RUN is still invisible to this gate (a red
  `go test` was not in scope).
- **The seam is env-driven** (`GOPI_VET_GATE_BASELINE_OVERRIDE`). It is documented,
  single-reader, and can only move a verdict toward KEEPING a merge — but it is a
  test hook in production code, and a reader who wants it gone should replace it
  with an injectable measurement function.
- **No end-to-end run of a real merger tick** was performed: the close loop was
  driven through `serveRebuild` directly (its real body), not through a full pass.
  A full pass additionally needs a settings repos entry, a sweep and a merge — the
  pieces between `serveRebuild` and its caller are unchanged by this work.
