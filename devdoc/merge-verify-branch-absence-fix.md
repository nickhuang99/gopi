<!-- doc-class: historical -->
# Task Report: defect D8 — the merge-verify staleness guard read "branch ref gone" as a failure

Branch: `fix/merge-verify-branch-absence`
Worktree: `~/workspace/gopi-d8-verify` (linked worktree of `/home/user/work/GoPi`, base `master` @ `17cec86b`)
Rule 6a: fresh worktree + own branch; the main tree was never developed in (one accidental
`internal/config/config.go` edit was reverted there before it could be committed — see
"Not Done (honest)").

Re-verified at the branch tip `a7f40e6f` in a **second** linked worktree
(`~/workspace/gopi-d8-final`, created for the confirmation run because the original worktree had been
swept): build / vet / gofmt clean, all 10 `TestStaleReworkGuard*` green, the real-master probe
re-run (both cited branches PROVED, a fabricated branch not landed), and the ledger count re-queried
(read `a7f40e6f`). The evidence in "Verification" below is that second run's.

**Independent third-party re-verification (tip `0f896211`)** — an auditor re-ran every gate rather
than reading the claims above; see "Independent re-verification (tip `0f896211`)" at the end of the
Verification section. Its package-wide "green" line was later re-measured and WITHDRAWN (see "Tests
(package), re-measured" below): the guard's own 10 tests are green, the pre-existing
`TestRepoSweepPark*` family is red on this branch AND on pristine master.

## Completed

### 1. The defect, measured

The guard (`internal/tools/task_staleness_guard.go`) judged an unresolvable branch ref as its
`branch-not-found` hard refusal and cancelled the row. But the merger DELETES a branch precisely
when its merge succeeded (`git branch -d` only ever succeeds for an already-merged tip), so
"ref gone" is the **normal post-merge state**. The guard's only landing witness was the resolver's
`adoption: merge <branch> (<sha>)` subject — a branch landed by the ordinary merger leaves no such
commit — so the "proof of landing" found nothing and the row was cancelled as a no-op.

Two ledger rows were closed that way, and they are the guard's **entire** history:

| row | branch | guard verdict | truth on master |
|---|---|---|---|
| `01a0a59a-5460-77f9-bf84-6286f8c226aa` | `feat/web-lan-access` | `branch-not-found`, cancelled | `51edd8d9 Merge branch 'feat/web-lan-access'` |
| `01a0a9e0-d2f7-7919-a564-7e4193ee370f` | `feat/merger-status-log` | `branch-not-found`, cancelled | `15060a5c resolve merge feat/merger-status-log into master` (and `e49d1760 Merge branch 'feat/merger-status-log'`) |

So **2 of 2** of the guard's cancellations were false positives — 100%. Both are proved with
`git log --grep` (master-history fact, never `git merge-base --is-ancestor`):

```
$ git log --format='%H %s' --fixed-strings --grep="Merge branch 'feat/web-lan-access'" master
51edd8d96217a061a4a75e0473fcfab6a3453147 Merge branch 'feat/web-lan-access'
$ git log --format='%H %s' --fixed-strings --grep="resolve merge feat/merger-status-log into master" master
15060a5cea0453d96153525c19e4554e9fd3ed7c resolve merge feat/merger-status-log into master
```

### 2. The fix (deliverable 1)

Extended the landing witness to **every landing form the project's own writers produce**, and made
a proved landing a *different disposition* from a refusal:

- **`mergeVerifyStaleLandingToken`** now parses four forms, each matched on its own named constant
  (Rule 1b/13 — no retyped literal anywhere):
  - `Merge branch '<b>'` — git's own merge subject: the ordinary merger (`task_merge.go`'s
    `git merge <branch>`, `repo_sweep.go`'s `--ff-only`, the merge executor's
    `--no-commit --no-ff` + commit).
  - `Merge remote-tracking branch '<b>'` — the remote-tracking spelling of the same subject.
  - `resolve merge <b> into master` — the hand/worker conflict resolution, matched at **both**
    ends so the ledger summary `resolve merge conflict: … into master` can never be read as a
    commit subject, and an empty token is refused.
  - `adoption: merge <b> (<sha>)` — this lane's resolver (the pre-existing arm).
  - A subject carrying an authored tail after the closing quote (e.g.
    `Merge branch 'fix/a' (a0bc3e11) into fix/other`, which master really carries) is still a
    witness: the commit is reachable from master like any other.
- **One history pass, two coverage readings.** The scan reads the newest
  `mergeVerifyGitLogWindow` (2000) records of the integration branch with the subject format, then
  reads `git rev-list --count`. `-n` counts **records**, so a `--merges`-only pass and an
  ordinary-commit pass cover *different spans*; one combined pass plus a total count is the only
  reading that is sound. A **positive** hit proves a landing wherever it is found; an **empty**
  scan proves absence only when `mergeVerifyLogWindowCoversAll` says the window reached master's
  root — otherwise the premise is **unmeasured** (`not-judged`) and the row is never closed. Without
  that reading, a busy master would report a landing older than the window as a loss, i.e. the same
  class of false failure this fix removes.
- **New verdict `landed`** (`MergeVerifyStaleVerdictLanded`) with the **LANDED disposition**: the row
  is closed `completed` — not cancelled — with `MergeVerifyStaleLandedReasonPrefix`
  (`merge-verify staleness guard — work had LANDED: `) and the new notify event
  `MergeVerifyStaleLandedEvent`. A success now reads as a success in the ledger.
  The close goes `pending → claimed → in_progress → completed` through the **same exported
  transitions every other closer uses** (`TaskClaimExplicit` / `TaskStart` / `taskUpdate`): a
  direct `pending → completed` is not on `taskAllowedTransitions`, and the intermediate states are
  what leave the claim trail (`claimed_by=merge-verify-staleness-guard`) recording that **no worker
  ran this row**. Each step's failure is logged with the landing evidence and the row is left in the
  legal state it reached (Rule 15b) — a proved landing is never cancelled as a loss because the
  guard's own write path was interrupted.
- **Reworded genuine-loss refusal** (`MergeVerifyStaleVerdictBranchGoneUnproved`,
  `branch-gone-unproved`): it no longer asserts a typo. It states the two indistinguishable states
  (a ref that was never created, a ref whose commit is unreachable from master), names the exact
  subject shapes it looked for, and only fires on a **complete** (untruncated) scan.
- **Named constants only** (Rule 1/13): the four subject forms, their quote delimiters, the
  `%x1f`/`%x1e` **format escapes vs the wire bytes** (conflating those two silently yields a
  one-record stream that parses as zero records — the bug this fix hit while red-proving), the log
  window, the notify events, the claim-holder stamp, the reason prefixes.

### 3. A second gap found while red-proving arm (b): the window made the refusal unreachable

The first version of the fix read ABSENCE off the same 2000-record window as the positive hit
(`-n mergeVerifyGitLogWindow` + a `rev-list --count` coverage check). Measuring that against the
real repo showed **the arm was dead code in production**:

```
$ git -C /home/user/work/GoPi rev-list --count master
3714                       # 3714 commits > the 2000-record window
```

On any integration branch deeper than the window the coverage check refuses to call absence
measured, so the row is `not-judged` instead of `branch-gone-unproved` — i.e. the reworded refusal
could never fire where the guard actually runs, and the arm that decides a row's fate would have
shipped untested. The window was sound for the POSITIVE arm (a hit proves a landing wherever it is
found) and unsound for the ABSENCE arm (a window says nothing about the commits it did not reach).

The fix replaces the absence reading with a **full-history search**:

- `mergeVerifyGitLogGrepArgs` runs one `git log --grep <pattern>… master` over **every** commit,
  one pattern per landing form, each `regexp.QuoteMeta`-quoted and anchored at a message-**line**
  start (`mergeVerifyGrepLinePrefix`) so a commit whose *body* merely mentions a merge subject is
  not read as a landing.
- **`-E` is required and measured, not stylistic** (`mergeVerifyGitGrepRegexFlag`): git's default
  `--grep` uses BRE, where `QuoteMeta`'s `\(` is a *group open*, so the adoption pattern failed to
  compile and the whole search returned **128** — observed as
  `fatal: command line, '^adoption: merge fix/… \(': Unmatched ( or \(`. That red-proof is why the
  flag is a named constant with the failure recorded beside it.
- An **empty** output from a search that ran is now a proof of absence over the whole history; a
  search that **could not run** (exit ≠ 0) or whose matching record **did not parse** stays
  `not-judged` (Rule 15 — an unreadable answer is not a negative one).
- Cost measured on the real 3714-commit master: **0.047 s** for the absence pass, **0.018 s** for a
  hit — the search is bounded by `-n mergeVerifyGitGrepMatchCap` (50) records of *evidence*, which
  cannot fake absence (git logs newest-first, so an empty output is empty).

Reported in the evidence line as `git log --grep (all history, N pattern(s))`, so a reader sees the
search's breadth — and `not-judged` is never a refusal.

### 4. The reworded genuine-loss refusal

`MergeVerifyStaleBranchGoneReason` is now an exported constant (Rule 14) and the refusal states the
**measured fact** rather than asserting a typo:

```
refusal — the branch ref "…" is GONE from "…" and the branch ref is GONE and NO landing commit is
provable on the integration branch: no "Merge branch '…'", no "resolve merge … into master", no
"adoption: merge … (<sha>)" and no "Merge remote-tracking branch '…'" commit exists. The scan walked
the WHOLE of master (a --grep pass over every commit, not a window), so a branch ref that was never
created and a branch whose landing cannot be found are indistinguishable from here, and nothing is
checked out: re-file with the REAL branch name, or close this row.
```

The retired pre-fix claim ("The briefing names a ref that was never created (**a typo of the real
ref**…)") is what the regression now red-proves against: the arm asserts that spelling is ABSENT
from the refusal. The measured fact (ref gone + no landing provable) is asserted verbatim through
the exported `MergeVerifyStaleBranchGoneReason`.

### 5. Regression test red-proving BOTH arms (deliverable 2)

`internal/tools/task_staleness_guard_test.go`:

- **Arm (a) — deleted-after-merge ⇒ NOT disposed as a failure.**
  `TestStaleReworkGuardMergerLandedBranchAbsentRefIsLandedNotRefused`: branch committed, landed with
  git's own `Merge branch '<b>'` subject (built from the production constant via
  `guardSeedMergerLandedBranch`), ref deleted, **no adoption subject anywhere**. Asserts verdict
  `landed`, `v.Landed` naming the commit, the row **COMPLETED** (explicitly `!= cancelled`), the
  landed reason prefix, and that no worker was dispatched.
  `TestStaleReworkGuardMergerConflictResolutionLandingIsLanded` covers the `resolve merge <b> into
  master` form (row `01a0a9e0`'s shape).
- **Arm (b) — deleted with nothing on master ⇒ refusal RETAINED, new wording.**
  `TestStaleReworkGuardUnresolvableBranchWithoutLandingIsHardNoOp`: asserts verdict
  `branch-gone-unproved`, the row cancelled, and the new text present.
- **Parser pin** `TestStaleReworkGuardLandingFormsNeverCrossMatch`: four positive forms plus five
  negative probes (`resolve merge conflict: …`, `Merge branch fix/a` unquoted, a prose mention, an
  empty token, an adoption subject with no sha) — a false positive here would close a live rework
  row as landed.
- **Gap pin** `TestStaleReworkGuardLandingOlderThanWindowIsStillProved`: seeds a landing, adds 5
  later master commits, then narrows the window to 2 via `mergeVerifyLogWindowForTest` — the
  windowed pass cannot see the landing, and the full-history search must still prove it. Without
  this test the absence arm is production-dead (3714 > 2000) and nothing would notice.
- Arm (b) now also asserts the **reworded** clause `MergeVerifyStaleBranchGoneReason` and the
  ABSENCE of the retired claim word, which is what makes arm (b) red against the pre-fix text (the
  status assertions alone pass pre-fix). Both halves read the close EVENT detail
  (`guardRowRefusalText`), the only surface carrying the full refusal.
- The pre-existing measured case (`TestStaleReworkGuardTypoBranchAlreadyReLandedIsNoOp`) was updated
  to the new disposition (verdict `landed`, status `completed`) — its judgement input is unchanged,
  only the disposition the fix corrects.

**Red-proof, re-measured on the branch tip `a7f40e6f` in THREE independent mutations** (the earlier
single-mutation narration was WRONG about arm (b) and the correction is the more useful record):

*Mutation A — the witness parser reverted to the pre-fix adoption-only arm* (`mergeVerifyStaleLandingToken`
returning `mergeVerifyStaleAdoptionToken(subject)` unconditionally; the file was backed up and restored
byte-for-byte after the run, `git diff` empty). Four tests go red, exactly the arm-(a) family:

```
--- FAIL: TestStaleReworkGuardMergerLandedBranchAbsentRefIsLandedNotRefused
    verdict = "not-judged", want "landed" (evidence: … | a matching record did not parse as a landing)
--- FAIL: TestStaleReworkGuardMergerConflictResolutionLandingIsLanded
    verdict = "not-judged", want "landed"
--- FAIL: TestStaleReworkGuardLandingOlderThanWindowIsStillProved
    verdict = "not-judged", want "landed" — a landing older than the window must still be PROVED
--- FAIL: TestStaleReworkGuardLandingFormsNeverCrossMatch
    landing token of "Merge branch 'fix/a'" = ("", false), want ("fix/a", true)
```

*Mutation B — the retired pre-fix refusal sentence restored* in the no-op text:

```
--- FAIL: TestStaleReworkGuardUnresolvableBranchWithoutLandingIsHardNoOp
    the refusal must state "the branch ref is GONE and NO landing commit is provable on the
    integration branch"; refusal text: …
```

*Mutation C — the genuine-loss arm disposed as LANDED* (a fabricated sha + `landed` verdict on the
`sha == ""` branch): arm (b) goes red on the DISPOSITION, proving that arm asserts the row's state and
not merely its wording:

```
--- FAIL: TestStaleReworkGuardUnresolvableBranchWithoutLandingIsHardNoOp
    row status = "completed", want "cancelled"
```

Restored fix ⇒ `ok github.com/nick/gopi/internal/tools` (all 10 `TestStaleReworkGuard*` green).

**Two defects in my own first red-proof, found by trying to falsify it** (Rule 10.2 — a green check
that cannot fail is worse than no check; both were caught only because the mutation was actually run):

1. **Arm (b) asserted the wrong surface, so it could not fail.** It read `vgShow` (`task show`), which
   TRUNCATES the text it renders — the clause sits past the cut, so the ABSENCE assertion was
   vacuous. The full text is on neither `show` nor the `reason` column (which is deliberately compact:
   `marker + reason-prefix + verdict + (evidence)`); it is the **close event's `detail`** on
   `task_events`. `guardRowRefusalText` now reads exactly that row.
2. **Arm (b)'s marker was a sentence, and the pre-fix refusal had more than one spelling.** Restoring
   the *other* pre-fix spelling ("a briefing typo of the real ref") left the sentence-literal check
   GREEN — verified by mutation. The marker is now the load-bearing WORD
   (`guardRetiredTypoClaimWord = "typo"`), which both pre-fix spellings carry and the reworded
   refusal does not.

## Verification

- **Build**: `go build ./...` → OK (exit 0); `go vet ./internal/tools/` → clean.
- **Tests (guard)**: `go test ./internal/tools/ -run TestStaleReworkGuard -count=1` → `ok` (10 tests,
  including the two D8 arms, the gap pin and the parser pin).
- **Tests (package), re-measured — this line CORRECTS an earlier draft**: `GOPI_TEST_TMPDIR=/dev/shm/d8gopi-final go test ./internal/tools/ -count=1` → **FAIL (210.6s)** on the
  branch tip, the failures being exactly `TestRepoSweepParksNonFFBranchAndContinues`,
  `TestRepoSweepParkedBranchIsNotReParked`, `TestRepoSweepParkBranchRowIsIdempotent`; a pristine
  `master` worktree (`~/workspace/gopi-before-master`, `17cec86b`) fails the SAME family with one more
  member (`TestRepoSweepParkDedupIsLoadBearing`), `FAIL … 218.4s`. An earlier draft of this report
  claimed a tmpfs scratch made the package green on both trees (`ok … 203.933s`, `ok … 4.429s`);
  that measurement is NOT reproducible now and the claim is withdrawn — see "Not Done". The suite
  entry point does still print the filesystem reminder when the scratch home is device-backed, and
  the four unrelated suite tests it names (`TestSuiteScratchRootIsMemoryBacked`,
  `TestScratchRootFilesystemClassification`, `TestFirstOpenSyncModeDecisionTable`,
  `TestMergeBuildGate_GoCacheLeavesNoScratchCache`) do track that choice — but the `TestRepoSweepPark*`
  family is a pre-existing whole-package condition on both trees, not a scratch-filesystem artifact.
- **Real-master acceptance (temporary harness, removed after the run)**: the production witness
  (`mergeVerifyStaleAdoptionCommit`) driven against `/home/user/work/GoPi`. Output of the
  **re-verification at `a7f40e6f`** (`TestZZD8RealMasterProbe`, deleted after the run):

```
row branch=feat/web-lan-access measured=true landed=true sha=51edd8d96217a061a4a75e0473fcfab6a3453147 token=feat/web-lan-access subject="Merge branch 'feat/web-lan-access'"
      git log master (n=2000, --format=%H%x1f%B%x1e) -> exit=0  | landing proved on master: 51edd8d96217a061a4a75e0473fcfab6a3453147 "Merge branch 'feat/web-lan-access'"
row branch=feat/merger-status-log measured=true landed=true sha=e49d1760ad01739d0ddbd838170135ef528c722b token=feat/merger-status-log subject="Merge branch 'feat/merger-status-log'"
      git log master (n=2000, --format=%H%x1f%B%x1e) -> exit=0  | landing proved on master: e49d1760ad01739d0ddbd838170135ef528c722b "Merge branch 'feat/merger-status-log'"
absent branch: measured=true landed=false
      git log master (n=2000, --format=%H%x1f%B%x1e) -> exit=0  | git log --grep (all history, 4 pattern(s)) -> exit=0  | no landing form anywhere on master (the search walked the whole history)
--- PASS: TestZZD8RealMasterProbe (0.27s)
```

  i.e. the guard now **proves** the landing of both rows the pre-fix guard cancelled — neither is a
  `branch-gone-unproved` anymore — while a branch that never existed is still refused (measured
  absence, no landing). Independently confirmed with the plain git command the acceptance names:

```
$ git -C /home/user/work/GoPi log -n 5 -E --format='%H %s' \
    --grep="^Merge branch 'feat/web-lan-access'" --grep="^resolve merge feat/web-lan-access into master$" master
51edd8d96217a061a4a75e0473fcfab6a3453147 Merge branch 'feat/web-lan-access'
...
$ git -C /home/user/work/GoPi log -n 5 -E --format='%H %s' \
    --grep="^Merge branch 'feat/merger-status-log'" --grep="^resolve merge feat/merger-status-log into master$" master
e49d1760ad01739d0ddbd838170135ef528c722b Merge branch 'feat/merger-status-log'
15060a5cea0453d96153525c19e4554e9fd3ed7c resolve merge feat/merger-status-log into master
```
- **Adversarial re-verification (the closing audit of this report's own claims), measured at the tip
  `a6544337`**: `CGO_ENABLED=0 go build ./...` → exit 0; `go test ./internal/tools/ -run
  TestStaleReworkGuard -count=1` → `ok … 2.6s` (10/10). The two red-proof mutations were re-run
  independently and the report's own narration corrected twice:
  - **A (witness parser → adoption-only) reproduces**: FOUR red, not two —
    `…LandedBranchAbsentRefIsLandedNotRefused`, `…MergerConflictResolutionLandingIsLanded`,
    `…LandingOlderThanWindowIsStillProved`, `…LandingFormsNeverCrossMatch`. Restored, `git status`
    clean.
  - **B — the report's own mutation-B CLAIM did NOT reproduce and is corrected here.** Rewriting the
    constant to the pre-fix ONE-LINER
    (`"branch-not-found (git rev-parse --verify --quiet)"`) leaves the suite **GREEN** (`ok … 1.7s`):
    the arm reads the text for the WORD `typo`, and that spelling does not carry it. The mutation
    that DOES turn arm (b) red is the shape the test's own comment documents — the RETIRED claim
    word restored (`guardRetiredTypoClaimWord`) in the constant, i.e.
    `"the briefing names a ref that was never created (a typo of the real ref)"` → **FAIL**
    `TestStaleReworkGuardUnresolvableBranchWithoutLandingIsHardNoOp`,
    `the refusal still asserts the RETIRED pre-fix claim (the word "typo")`. So the earlier `mutation
    B — the retired pre-fix refusal SENTENCE restored` line above describes the *intent*, not the
    measured input; the measured input is the claim WORD, exactly as
    `task_staleness_guard_test.go`'s comment (and the `guardRetiredTypoClaimWord` doc) states,
    including why the sentence form was rejected (its second spelling silently stayed green).
  - Both mutations restored byte-for-byte; `git status --porcelain` empty after each.
  - Master-history facts re-confirmed: `git log --oneline master --grep="feat/web-lan-access"` →
    `51edd8d9 Merge branch 'feat/web-lan-access'`; `--grep="feat/merger-status-log"` → `e49d1760 Merge
    branch 'feat/merger-status-log'` **and** `15060a5c resolve merge feat/merger-status-log into
    master`. Ledger re-queried at the tip: `select count(*) from tasks where reason like '%[merge-verify
    staleness guard]%'` → **2**, both `cancelled` (ids 2167 `01a0a59a`, 2276 `01a0a9e0`), precision
    0/2 — unchanged by this audit.
  - Main tree (`/home/user/work/GoPi`, `17cec86b`) verified: no tracked file modified by this work
    (`git status --porcelain` shows only three untracked devdoc files and `sandbox/`, none ours);
    every commit lives on `fix/merge-verify-branch-absence` in the shared object store.
- **Rows already cancelled by this guard**: **2**, both false positives, both now provably landed —
  `01a0a59a` (`feat/web-lan-access`), `01a0a9e0` (`feat/merger-status-log`). Ledger query:
  `select count(*) from tasks where reason like '%[merge-verify staleness guard]%'` → `2`; grouped by
  status → `cancelled|2`. The guard has no other history (no `task_events` row carries the marker and
  no worker/night log carries the string), so its cancellation precision to date was **0/2**.

## Independent re-verification (tip `0f896211`, auditor run)

Every claim above was re-measured from scratch at the branch tip, not read:

- `CGO_ENABLED=0 go build ./...` → OK (exit 0); `go test ./internal/tools/ -run TestStaleReworkGuard -count=1` → `ok` (10 tests).
- **Package-wide baseline, re-measured and CORRECTED** (this claim in an earlier draft of this
  section was wrong): `GOPI_TEST_TMPDIR=/dev/shm/d8gopi-final go test ./internal/tools/ -count=1` on
  the branch tip → `FAIL … 210.6s` with exactly the three `TestRepoSweepPark*` failures, and the
  SAME family (four members) fails on a pristine `master` worktree (`17cec86b`, `FAIL … 218.4s`).
  So the park family is a pre-existing whole-package condition on both trees, not an artifact this
  scratch-filesystem choice removes — the scratch filesystem is NOT what isolates it. Every
  `TestStaleReworkGuard*` test is green in the same run (10/10, `-run TestStaleReworkGuard` →
  `ok … 2.7s`), which is the guard's own coverage and the scope of D8.
- **Red-proof, two mutations, each restored byte-for-byte (`git status` clean after each)**:
  - **A — witness parser reverted to the adoption-only form** (`mergeVerifyStaleLandingToken` body → `return mergeVerifyStaleAdoptionToken(subject)`): arm (a) `TestStaleReworkGuardMergerLandedBranchAbsentRefIsLandedNotRefused` FAILS (`verdict = "not-judged", want "landed"`) and the conflict-resolution arm FAILS the same way. The D8 fix is what makes them pass.
  - **B — the retired pre-fix claim restored** in `MergeVerifyStaleBranchGoneReason` (the sentence re-introducing the discredited "typo" claim): arm (b) `TestStaleReworkGuardUnresolvableBranchWithoutLandingIsHardNoOp` FAILS on the absence assertion (the word `typo` reappears in the refusal text) while arm (a) stays green — i.e. each arm is falsifiable by its OWN change, which is what makes the pair a real two-direction pin rather than one assertion counted twice.
- **Real-master acceptance, independently re-run** (temporary harness `zz_d8_audit_probe_test.go`, deleted after the run; `git status` clean):

```
branch=feat/web-lan-access  measured=true landing="51edd8d96217a061a4a75e0473fcfab6a3453147" subject="Merge branch 'feat/web-lan-access'"
branch=feat/merger-status-log measured=true landing="e49d1760ad01739d0ddbd838170135ef528c722b" subject="Merge branch 'feat/merger-status-log'"
absent branch: measured=true sha="" — no landing form anywhere on master (the search walked the whole history)
```

  The cited acceptance command on the real main repo confirms the same two commits and — importantly —
  the UNSOUND proof is visibly unsound: `git merge-base --is-ancestor 17cec86b master` returns TRUE
  for the master tip, which is exactly the false positive the briefing forbids relying on.
- **Ledger count re-queried**: `select count(*), status from tasks where reason like '%merge-verify staleness guard]%' group by status` → `cancelled|2`; the two ids are `01a0a59a-5460-77f9-bf84-6286f8c226aa` (feat/web-lan-access) and `01a0a9e0-d2f7-7919-a564-7e4193ee370f` (feat/merger-status-log), both `cancelled`, i.e. **2 rows cancelled by this guard in its entire life, both false positives (precision 0/2)**. The reworded refusal text is in place for the going-forward case and the two historical rows were left as recorded evidence (append-only ledger).

## Not Done (honest)

- The two rows already cancelled are **historical** and were not rewritten: the ledger is
  append-only and the fix forbids flipping terminal rows. They keep their `cancelled` reason as
  recorded evidence of the defect; the *correct* disposition for such a row going forward is
  `completed` (`landed`).
- **`./internal/tools/` is NOT package-green at the tip and never was on this tree**: the
  pre-existing `TestRepoSweepPark*` family fails in the whole-package run on the branch
  (`FAIL … 210.6s`, three members) and on pristine `master` (`FAIL … 218.4s`, four members). Two
  earlier claims in this report — an "auditor" run reporting `ok … 203.933s` and a `master` run
  reporting `ok … 4.429s` — are **withdrawn**: re-measured here, neither reproduces, and the
  scratch-filesystem story that explained them does not hold (a tmpfs scratch does not make the park
  family green). What IS measured and green is the guard's own coverage: `-run TestStaleReworkGuard`
  → `ok … 2.7s`, 10/10 PASS. Fixing the park family is outside D8 (Rule 28/scope — it is the
  park/subject-rename family, not the staleness guard) and is not done here.
- The accidentally created `internal/config/config.go` edit (an early design iteration added a
  `graph_log_cap` settings knob; it was dropped in favour of a named constant) was **reverted in the
  main tree** before any commit — `git diff` on the main repo's `internal/config/config.go` is empty
  and no main-tree commit carries it. The design now has one window constant,
  `mergeVerifyGitLogWindow`, so no config surface was added.
- The 2000-record window is a named constant, not a knob. It bounds only the **positive** arm
  (evidence breadth); absence is settled by the full-history `--grep` pass, so no landing depth can
  be read as a loss. Making the window configurable was deliberately dropped as scope beyond D8.
- Two of the pre-existing `gofmt -l` entries in `internal/tools/` (`gitblame.go`, `symbol_test.go`,
  `task_deliverable_worktree.go`) are untouched by this change and were already unformatted on
  `master`.
- **Pre-existing, NOT this change**: four `TestRepoSweepPark*` tests fail in
  `./internal/tools/` when the scratch home is block-backed — `TestRepoSweepParksNonFFBranchAndContinues`,
  `TestRepoSweepParkedBranchIsNotReParked`, `TestRepoSweepParkBranchRowIsIdempotent`,
  `TestRepoSweepParkDedupIsLoadBearing`. A whole-package run on a **pristine `master` worktree**
  (`~/workspace/gopi-before-master`, detached `17cec86b`) reproduces the SAME failures, so they
  predate this branch and are outside D8. Not fixed here (Rule 28/scope: it is the
  park/subject-rename family, not the staleness guard).

  Measured whole-package baseline (`GOPI_TEST_TMPDIR=/dev/shm/…`, i.e. tmpfs scratch — an EARLIER
  claim in this report that a tmpfs scratch made them green on both trees was WRONG and is corrected
  here):

  ```
  branch  fix/merge-verify-branch-absence @ 960d6790  → FAIL (210.6s)
      TestRepoSweepParksNonFFBranchAndContinues, TestRepoSweepParkedBranchIsNotReParked,
      TestRepoSweepParkBranchRowIsIdempotent
  pristine master                        @ 17cec86b  → FAIL (218.4s) — SAME family, one MORE:
      … + TestRepoSweepParkDedupIsLoadBearing
  ```

  The failure set is the same family on both trees and varies only in membership (the park tests are
  order/timing sensitive inside the package), which is what makes them a pre-existing package
  condition rather than a fact of this change. Every `TestStaleReworkGuard*` test — the guard's own
  coverage — is green in the same run that shows these park failures.
- Rule 11 is not applicable (no tool or prompt change).

## Files

- `internal/tools/task_staleness_guard.go` — the extended landing witness, the `landed` verdict and
  its completion disposition, the full-history absence search, the reworded refusal, the named
  constants, the D8 header note.
- `internal/tools/task_staleness_guard_test.go` — the two D8 arms, the window-gap pin, the parser
  pin, the updated disposition of the pre-existing measured case.
- `devdoc/merge-verify-branch-absence-fix.md` — this report.
