<!-- doc-class: historical -->
# Task Report: re-measure `archive/fix/merge-verify-landing-witness` — OBSOLETE per unit, one residual arm SALVAGED

Task: `01a0b51d-6509-7012-841f-c6cf034cb214`
Repo: `/home/user/work/GoPi` (host `peerbox`)
Worktree: `/home/user/workspace/gopi-landing-witness-rem`, branch `fix/merge-verify-landing-witness`, off `master` @ `249b7938`

## Verdict (one line)

**The tag is OBSOLETE per unit** — master settled the same defect D8 independently, ~8 minutes before
the tag's kernel commit and ~21 minutes before the tag's tip, in `7bfdc6a5` (the landing witness reads
every real merge subject) + `1a6622cd` (absence is settled over the whole history, not a window).
**One residual arm of the audit's "unlanded" verdict is SALVAGE**: the merge-backlog **executor's** own
merge subject (`merge-backlog: merge <branch>`) is read by **neither** master's witness nor master's
search, and it is reachable in production. That arm is re-derived on the fresh branch (below); the rest
of the tag is closed as superseded.

## Instrument output (observed, this run)

```
$ git cherry -v master archive/fix/merge-verify-landing-witness    # exit 0
+ c327fd52985de76f38f0d87be8dbaa65aac21604 fix(merge-verify): the staleness guard's landing witness searched a window, not a name
+ 654674be51e3023ab5c9098ba105330f559f18d1 docs(merge-verify): correct the D8 evidence numbers and the stale --fixed-strings comment
+ b0008c3bda19c91004e26beb6da9ce5410cc03ef test(merge-verify): checked-in red-proof transcript for the landing-witness fix
+ b3a4f8c0fa1d094614544e1eb26a8a878a623f3e docs(merge-verify): red-proof transcript is checked in and reproducible
+ 6064f635f3fc6104e3a330d17741c6358a9c482b docs(merge-verify): align every counted subject figure with a reproducible command
+ 480ac14bddd3435cacfff1c09646025ead8b2656 docs(merge-verify): the witness search is a widened --grep pattern, not --fixed-strings; dirty-tree guard on the red-proof script
+ a0319ccd2e30020e756c76cf82f617a8a49d8310 test(merge-verify): regenerate the red-proof transcript at the final tree
+ 9307c970475fa53ab128989a942161398a4ff17b docs(merge-verify): record the whole-package verification and the raised-timeout condition
+ fd952ac6bf25192acb60c20253a180f8476f79a3 docs(merge-verify): count the subject census with a written-down query, not quoted figures
+ 5db36b1194802779f43822ccd78c9c77677e125e docs(merge-verify): align the report with the checked-in red-proof artifact and pin the witness cost

$ git rev-list --count master..archive/fix/merge-verify-landing-witness
10
$ grep -c '^+' /tmp/cherry.out   → 10      # cherry +10
$ grep -c '^-' /tmp/cherry.out   → 0       # cherry  -0  (no equivalent on master, by CHERRY's patch-id)

Tip sha:     5db36b1194802779f43822ccd78c9c77677e125e
Fork point:  909d4ca9   (git merge-base master <tag>)
Master tip:  249b793810e10bcb3bb4bba1605c4a69ec94e107   (measured)

Files the tag would add:  .evid/red.txt, .evid/redproof.sh,
                          devdoc/merge-verify-staleness-landing-witness.md,
                          internal/tools/task_staleness_guard.go (+283/-50), internal/tools/task_staleness_guard_test.go (+346)
```

`git cherry` reports **+10/-0** — but cherry compares **patch-ids**, and it is the exact instrument the
audit's name list rested on. The audit's "UNLANDED" is a statement about *content*, and the content test
below is what decides it.

## The content test (the decision instrument)

`git cherry` cannot see that master implemented the same fix in **different commits**, so the test is
per-function: for each unit the tag introduces, ask the git history of `internal/tools/` for the symbol,
and ask master's tree whether the symbol exists at all.

```
$ git log --oneline -S<symbol> master -- internal/tools/     # where the symbol's content entered master
$ git grep -w <symbol> master -- internal/tools/             # whether master's tree carries it today

symbol                                    | -S on master                                          | in master's tree
------------------------------------------+-------------------------------------------------------+-----------------
mergeVerifyStaleAdoptionCommit (SHAPE)    | 1a6622cd (also 7bfdc6a5, 5552a631)                    | yes (windowed + full-history)
mergeVerifyLandingSubjectCandidates       | 1a6622cd                                              | yes
mergeVerifyGitLogGrepArgs                 | 1a6622cd                                              | yes
mergeVerifyLogWindowForTest               | 1a6622cd                                              | yes
mergeVerifyGitLogWindowTestNarrow         | 1a6622cd                                              | yes
mergeVerifyLogWindow()                    | 1a6622cd                                              | yes
mergeVerifyStaleLandingToken              | 7bfdc6a5                                              | yes
mergeVerifyStaleAdoptionToken             | 5552a631                                              | yes
mergeVerifyMergeBacklogPrefix / …         | <no commit> — NEVER LANDED                             | ABSENT  ← the residual
mergeVerifyStaleLandingGrep               | <no commit> — NEVER LANDED                             | ABSENT  (tag's own name; master re-derived the idea)
mergeVerifyStaleLandingCommit             | <no commit> — NEVER LANDED                             | ABSENT  (tag's own name; master re-derived the idea)
mergeVerifyStaleSepClass                  | <no commit> — NEVER LANDED                             | ABSENT  (see "deliberately not carried")
mergeVerifyStaleSubjectHead               | <no commit> — NEVER LANDED                             | ABSENT  (see "deliberately not carried")
```

All three tag-specific *names* are absent from master — which is exactly what cherry reported — while the
**capability** each one carries is present under master's own names. That gap between "the name landed"
and "the capability landed" is the whole of this re-measurement.

## Per-unit verdicts

| # | Unit (tag commit) | Verdict | Evidence |
|---|---|---|---|
| 1 | `c327fd52` + `654674be` — **kernel**: landing witness by name, not window | **(b) OBSOLETE** | Master `7bfdc6a5` (2026-09-17 02:22:12) added `mergeVerifyStaleLandingToken` reading **four** subject forms; `1a6622cd` (02:32:33) added the whole-history `--grep` absence arm + `mergeVerifyLandingSubjectCandidates`/`mergeVerifyGitLogGrepArgs`. The kernel commit is `02:30:12` — **8 minutes after** `7bfdc6a5`. Red proof: the tag's own witness tests FAIL on clean master, and fail *because master proves the landing*. |
| 2 | `b0008c3b`, `b3a4f8c0`, `a0319ccd`, `480ac14b` — `.evid/redproof.sh` + `.evid/red.txt` | **(b) OBSOLETE** | The red proof is now a **checked-in Go test** on master (`TestStaleReworkGuardMergerLandedBranchAbsentRefIsLandedNotRefused`, `…ConflictResolutionLandingIsLanded`, `…LandingOlderThanWindowIsStillProved`, `…LandingFormsNeverCrossMatch`) — the mutation is asserted inside `go test`, not replayed by a shell transcript (master's devdoc §"the D8 fix is what makes them pass"). Not re-derived: a transcript of a mutation that no longer exists. |
| 3 | `6064f635`, `fd952ac6`, `9307c970`, `5db36b11` — devdoc + counted figures | **(b) OBSOLETE** | Master documents the same defect as **D8** in `devdoc/merge-verify-branch-absence-fix.md` (02:32-round), with its own subject census (§"`TestZZD8RealMasterProbe`" evidence: "no landing form anywhere on master (the search walked the whole history)"). The tag's census instrument (`git log --format=%s master \| grep -cF`) re-run at this measurement: `Merge branch '` **699**, `resolve merge ` **73**, `adoption: merge ` **62**, `merge-backlog: merge ` **0**; master `rev-list --count` **4654** vs window `mergeVerifyGitLogWindow = 2000`. Different numbers from the tag's (3719/580/68/58) — master moved — but the same conclusion, already written down. |
| 4 | `merge-backlog: merge <branch>` — the executor's merge form (the ONE arm the audit needed and did not get) | **(a) SALVAGE** | Reachable in production: `internal/tools/task_merge_executor.go` stages the merge then commits with `mergeExecMergeMsg = "merge-backlog: merge %s"` (`branch` = the full ref the executor merges), and deletes the ref (`branch -d`) — i.e. the guard's exact end state (ref gone, landing on master). Master's witness reads 4 forms, **none** of them this one, and the master tree contains `mergeVerifyStaleMergeBacklogPrefix` **nowhere**. Measured: the tag's own `…/merge-backlog-executor` subtest fails on master with `no landing form anywhere on master (the search walked the whole history)`. Re-derived on branch `fix/merge-verify-landing-witness` (below). |

## The red proof (tag's own tests transplanted onto CLEAN master)

Transplant = master's tree + the tag's witness tests only (the tag's production file is NOT copied), so a
failure is master's reading of the *premise*, not the tag's code. Method: my worktree, master's test file
backed up, the tag's test file overlaid, run, then `git restore`.

First result (import block only — disclosed for honesty, not a claim):

```
internal/tools/task_staleness_guard_test.go:628:24: undefined: strconv
internal/tools/task_staleness_guard_test.go:633:9: undefined: exec
```

Master's test file does not import `strconv`/`exec`; the tag's does. After adding the two imports:

```
--- FAIL: TestStalenessLandingWitnessFindsLandingDeepInMasterHistory (0.92s)
    task_staleness_guard_test.go:685: verdict = "landed" stale=true, want "branch-not-found" stale=true
      (evidence: git rev-parse --verify --quiet refs/heads/feat/web-lan-access ⇒ exit=1
       | git log master (n=2000, …) -> exit=0
       | landing proved on master: 40dd2511ab43773f7110ad0b4d93da2201349ebe "Merge branch 'feat/web-lan-access'")
--- FAIL: TestStalenessLandingWitnessDepthIndependence (2.36s)      # "landed", want "branch-not-found"
--- FAIL: TestStalenessLandingWitnessDoesNotCollapseRefusalIntoLanding (0.79s)  # "branch-gone-unproved"
--- FAIL: TestStalenessLandingWitnessReadsEveryRealMergeSubject (4.16s)
    --- FAIL: …/git-default-merge        (1.55s)  # "landed"
    --- FAIL: …/merger-resolve           (0.70s)  # "landed"
    --- FAIL: …/resolver-adoption        (1.19s)  # "landed"
    --- FAIL: …/merge-backlog-executor   (0.72s)
        task_staleness_guard_test.go:802: subject "merge-backlog: merge feat/web-lan-access":
        Landed = "", want the landing 64c1b17e9073c739670d8fba40a68a3a023269c9
        (evidence: … | git log --grep (all history, 4 pattern(s)) -> exit=0
         | no landing form anywhere on master (the search walked the whole history))
FAIL   github.com/nick/gopi/internal/tools   13.401s
```

Reading: the tag's tests assert the **pre-fix** verdict (`branch-not-found`) on the two arms master now
proves (`landed`), and **only one** arm is a genuine gap — `merge-backlog-executor`. That is the
per-unit split in one run, and it is why the verdicts are (b) with one (a).

## (a) SALVAGE — the residual arm, re-derived

Branch `fix/merge-verify-landing-witness` (fresh worktree off `249b7938`). `internal/tools/task_staleness_guard.go`
+89/-5, `internal/tools/task_staleness_guard_test.go` +141. Nothing else resurrected: no `.evid/`
transcript, no `devdoc/merge-verify-staleness-landing-witness.md`, no rename back to the tag's symbols.

What it adds (minimal, one capability):

- `mergeVerifyMergeBacklogPrefix` — the executor's subject prefix, and
  `mergeVerifyMergeBacklogSubject(branch) = fmt.Sprintf(mergeExecMergeMsg, branch)` **derived from the
  executor's own message constant** (Rule 14: the writer and the witness cannot drift).
- `mergeVerifyMergeBacklogSubjects(branch)` — the GREP candidate set: `--grep` matches subject TEXT, so
  the search must carry **both** spellings the writers produce (`merge-backlog: merge refs/heads/<b>` as
  the executor commits it, and the bare `merge-backlog: merge <b>` a hand-written subject may carry).
- `mergeVerifyLandingTokenNormalize(tok)` — reduces a token's **ref spelling** (`refs/heads/` prefix,
  stray separators) to the branch name.
- `mergeVerifyStaleLandingToken` — a fifth arm for the executor's subject.
- `mergeVerifyStaleLandingIn` — **one** normalization at the single comparison site, for both the exact
  and the separator-insensitive arm.

Design decisions worth a future reader's time:

1. **The comparison stays an EQUALITY.** The normalization touches the *spelling* only
   (`refs/heads/fix/a` → `fix/a`); matching is still `token == branch` and, for the fuzzy arm, a name-key
   equality. No containment/substring test was introduced — a landing for `fix/a/sub` is still not the
   row's `fix/a` (asserted by `TestStaleReworkGuardLandingTokenNormalizationIsAnEquality`).
2. **The pattern must carry the spelling the commit really has.** The first cut derived the pattern from
   the *normalized* name; a mutant test proved that insufficient (the search found nothing because the
   commit says `refs/heads/…`). Both spellings are now pattern candidates while the parser normalizes
   either — the search is a selector, the parser decides.
3. **Deliberately NOT carried from the tag** (and said so, rather than quietly dropped):
   `mergeVerifyStaleSepClass` (the tag's regex-level separator folding). The capability the D8 typo case
   needs already lives on master as `MergeVerifyStaleNameKey` in the separator-insensitive arm, so the
   salvage does not duplicate it; and a widened BRE applied to EVERY name character would make the
   pattern far broader than needed (e.g. it can match across a `/`), which the tag's own doc recognised
   needed per-character justification. Scope: the residual arm only.
4. `mergeVerifyStaleSubjectHead` (the tag's whitespace tokenizer) is likewise not carried — master reads
   subjects with `strings.HasPrefix`/`CutPrefix` and needs no tokenizer.

## Verification (branch-scoped, smoke — Rule 27)

- `CGO_ENABLED=0 go build ./...` → **BUILD_ALL_OK** (see Evidence)
- `CGO_ENABLED=0 go vet ./internal/tools/` → **VET_OK**
- `gofmt -l internal/tools/task_staleness_guard.go internal/tools/task_staleness_guard_test.go` → prints
  **only** `task_staleness_guard_test.go`, and `gofmt -d` on it shows **exactly one hunk** (`@@ -91,11`),
  which is **master's own pre-existing comment drift** in `guardRowRefusalText` (a `·` bullet list) —
  present on master before this branch and untouched here. Every line this branch adds is gofmt-clean
  (`gofmt -l` on `task_staleness_guard.go` prints nothing). The other two `gofmt -l` hits in the package
  (`task_finish_premerge.go`, `taskledger_cost_test.go`) are likewise pre-existing on master (verified by
  running `gofmt -l` on master's own copies).
- Guard surface (`TestStaleReworkGuard|TestStalenessLandingWitness`) → `ok github.com/nick/gopi/internal/tools`
- Full touched package `./internal/tools/` → see Evidence (job output)

### Red proof for the SALVAGE (three independent mutants, each a real runtime failure)

| mutant | injected change | observed |
|---|---|---|
| A — pattern half | executor form dropped from `mergeVerifyLandingSubjectCandidates` | `FAIL` `verdict = "branch-gone-unproved"`, evidence `4 pattern(s) … no landing form anywhere on master` |
| B — parser half | executor arm dropped from `mergeVerifyStaleLandingToken` | `FAIL` `verdict = "branch-gone-unproved"`, evidence `5 pattern(s) …` |
| D — normalization half | normalization removed at **both** comparison arms | `FAIL` `verdict = "not-judged"` (`stale=false`), evidence `6 pattern(s) … a matching record did not parse as a landing` |

Each half of the fix is therefore load-bearing on its own. One further mutant was **not** red and is not
claimed as such: removing normalization from the exact arm **only** (mutant C) still passes, because the
separator-insensitive arm normalizes the same token and attributes the landing correctly — a stronger
verdict on the same git fact, not a defect.

## Not Done (honest)

- The tag's `.evid/` pair and `devdoc/merge-verify-staleness-landing-witness.md` are **not** resurrected:
  judged OBSOLETE, and re-checking-in an obsolete red-proof transcript would be a second source of truth
  for a mutation master already asserts inside `go test`.
- The tag's `mergeVerifyStaleSepClass` widening is **not** carried (design note 3 above) — if a future
  defect needs regex-level separator folding, it is a fresh task with its own red proof, not this branch.
- The `resolve merge` suffix remains spliced from the literal `" into master"` on master rather than from
  `IntegrationBranchName`. I did **not** change it: it is outside this unit's scope and would broaden the
  diff into a refactor (a `-S`-visible behaviour change) that the audit did not ask for.
- No merge, no tag deletion (`archive/fix/merge-verify-landing-witness` and its sibling
  `…-20260917` are untouched).

## Evidence (self-filed)

- `git cherry -v master archive/fix/merge-verify-landing-witness` → exit 0, 10 lines, `grep -c '^+'` = 10, `grep -c '^-'` = 0. `git rev-list --count master..<tag>` → `10`.
- Content test: `git log --oneline -S<symbol> master -- internal/tools/` per symbol (table above);
  `git grep -w mergeVerifyStaleLandingGrep master -- internal/tools/` → **no output** (absent);
  `git merge-base --is-ancestor 1a6622cd master` → exit 0; `git show -s --format=%ci` → `1a6622cd 2026-09-17 02:32:33 +0800`, `7bfdc6a5 2026-09-17 02:22:12 +0800`, `c327fd52 2026-09-17 02:30:12 +0800`, `5db36b11 2026-09-17 02:53:29 +0800`; fork `909d4ca9 2026-09-17 01:21:26`.
- Census: `git log --format=%s master | grep -cF "<prefix>"` → `Merge branch '` 699, `resolve merge ` 73, `adoption: merge ` 62, `merge-backlog: merge ` 0; `git rev-list --count master` → 4654; `git grep -n 'mergeVerifyGitLogWindow =' master` → `2000`.
- Red proof: the transplant command block above, run in my worktree; failures quoted verbatim; the
  tag's test file restored with `git restore` and `git status --porcelain` re-checked.
- Build/tests: `CGO_ENABLED=0 go build ./...` → `BUILD_ALL_OK`; `go vet ./internal/tools/` → `VET_OK`;
  guard-surface run → `ok github.com/nick/gopi/internal/tools`; full package run → see the job output
  quoted in the run's close-out.
- Rule 22: `TMPDIR=/dev/shm/gopi-test-scratch/tmp`, `stat -c %d /` = 2051 vs `/dev/shm` = 28 — the test
  scratch root is off the `/` partition.
- Rule 30: `git status --porcelain` in the worktree after the commit → no output.

## Deliverable declaration (Rule 6d)

- Branch: **`fix/merge-verify-landing-witness`**, in worktree `/home/user/workspace/gopi-landing-witness-rem`
  (a linked worktree of the main repo — the commit already lives on that branch in the shared object store;
  **no push needed**).
- Recovery: `git show fix/merge-verify-landing-witness:devdoc/merge-verify-landing-witness-residual.md`
  and `git show fix/merge-verify-landing-witness:internal/tools/task_staleness_guard.go`.
- Tag `archive/fix/merge-verify-landing-witness` left intact and unmodified; no merge performed.
