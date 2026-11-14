<!-- doc-class: historical -->
# Task Report: the devdoc truth-class WRITE path is branch-scoped, atomic, and refuses a moved ground

Task `01a0dc00-2d88-7933-9096-d3409711368b` — epic `docindex-write-path` (HP 2026-09-26,
measured in task `01a0dbe4`).

## Completed

- **Branch-scoped measurement (`internal/docindex/scope_git.go`, `AddedDocs`).** The
  ADDED set is now the three-dot additions **plus** only those dirty status entries that
  are an *addition* of a path **absent from the base**: untracked (`??`) or staged
  (`A`), and `git ls-tree <base>` says the path does not exist there. A modification of a
  document that lives on the integration branch is no longer counted as this branch's
  addition — that single predicate is what let an inherited row be re-decided before.
- **Scoped repair (`internal/docindex/docindex.go`, new `RepairWithScope`), along the
  line the corpus actually draws.** `Missing`/`Unbannered` are CLASS decisions (a row is
  minted; a banner is chosen from the class) and are therefore scoped to the documents
  the branch ADDS; `Malformed`/`Duplicated` are READABILITY (an unparseable row fails
  every class-driven gate on the WHOLE corpus; two rows for one path fail the coverage
  gate like a missing one) and are repaired wherever they are, because `Repair`'s
  handling of both is conservative — a malformed row is rewritten carrying the class and
  reason it already declared, a duplicate is dropped to the one row already on the
  manifest. `assertOnlyRowsTouched` then asserts the PROPERTY: a row whose CLASS is
  removed, written or moved for a path outside the scope is refused; a row whose text
  changed while its class did not is the readability repair above. (This distinction was
  forced by `internal/standards/docindex_gate_redproof_test.go`'s
  `TestDocIndexWritePathRepairsAMalformedRow`, which went red on the first cut — see
  `## Not Done`, and the fix is a fix, not a gate relaxation.)
- **Atomic pair + measured-ground refusal (`SyncBranch`).** Before the write the check
  takes (a) the sha256 of every added document, (b) the branch tip, and (c) the manifest
  rows outside the scope. It then re-measures immediately before the write (the
  `syncBranchWriteHook` seam is where a test moves the ground) and refuses, *naming the
  path*, when a document was rewritten or a new document appeared under the corpus. After
  the write, `assertOnlyRowsTouched` refuses when any inherited row's class changed, and
  `commitPaths` re-asserts the tip before committing — a tip that moved since measurement
  is a refusal with a recorded reason, never a half-state.
- **The commit can no longer swallow someone else's index
  (`commitPaths` + `gitPrivateIndex`/`runGitEnv`).** The classification commits through a
  **private index** (`GIT_INDEX_FILE` = a file inside the repo's own git dir, seeded by
  `read-tree HEAD` and populated by `update-index --add -- <allowed paths>`), so another
  tool's staged change is neither read nor carried nor disturbed — and the repository
  index is reset for exactly the committed paths afterwards, so the branch a caller
  inspects is clean (Rule 30). `git add` is deliberately not used: it merges entries into
  the *repository* index, which is the mechanism that carried the half-state in the
  measured run.
- **Refusal recording is already on the row's trail, unchanged:** a returned error is
  turned by `finishCleanupDocClassSync` into `Status=Refused` +
  `finishCleanupDocClassRefusedMarker + err.Error()`, which
  `finishCleanupDocClassReason` writes to the task event trail and the log — the reason
  outlives the run's stdout (Rule 15). No new marker was needed: the epic's "refuse when
  the ground/tip moved, recording the refusal" rides the existing refusal path.

- **The red-before proof is now COMMITTED, not remembered
  (`internal/docindex/testdata/redder/main.go` + `docindex_redproof_test.go`).** One
  driver, built twice — once resolving `github.com/nick/gopi` to a scratch clone of
  `master`, once to this tree — over the SAME three git fixtures, asserting the
  transition. Opt-in (`GOPI_DOCINDEX_RED_PROOF=1`) because it clones the repo and builds
  two binaries; the numbers are in `## Verification`. This replaces a hand-run scratch
  worktree whose evidence no longer existed, and it is what makes the report's central
  claim re-runnable by a reviewer.

- **A refusal leaves no half-state behind (`SyncBranch`, commit `ea6f3279`).** The second
  pass closed the gap the first one left: when the classification could not be committed
  (a failing pre-commit hook is the cheap reproducer) the writer used to return
  `bs.Abstained` — which the `internal/tools` wrapper turns into "NOT checked", a claim
  about the check that is simply false — while the row and the banner it had just written
  stayed on disk for the next `git commit` to swallow, which is the measured incident's
  exact shape. Now `SyncBranch` (a) SNAPSHOTS every path it may write before the repair,
  (b) VERIFIES the pair by reading it back from disk (`assertPairIsConsistent`: the
  document's banner class and the manifest row's class must agree; a bannered class with
  no banner is a refusal), and (c) on ANY post-write refusal **restores the bytes**
  (`restorePaths`, which also removes a path the write created) and returns the refusal
  as an ERROR, so the close-out chain records `Refused` with the reason instead of an
  abstention — and the corpus it was handed is byte-identical afterwards. Three refusal
  strings moved onto named constants (`refusalPrefix`, `refusalTipMoved`,
  `refusalCommitPrefix`, Rule 1/13).
- **Two more committed tests carry the unwinding, both through the public surface:**
  `TestSyncBranchLeavesNothingBehindWhenTheCommitFails` (a fixture whose `pre-commit` hook
  exits 1 → a refusal, the corpus byte-identical, `git status --porcelain` unchanged) and
  `TestSyncBranchUnwindsWhenTheBranchTipMovesUnderIt`.
- **A defect in the first pass's OWN tests, found by running the surface rather than
  reading it.** Six call sites built `filepath.Join(root, "devdoc", branchAddedRel)` where
  `branchAddedRel` already carries `devdoc/`, i.e. they addressed `devdoc/devdoc/<path>` —
  a file that does not exist — and their `strings.Contains(committed, branchAddedRel)`
  assertions still passed, because the assertion was a substring test and the real path
  contains the rel path. Fixed to `filepath.FromSlash`; all six stay green. This is
  recorded because it is the same failure mode as the fixture bug below: a test that
  agrees with itself.

## Not Done

- **The system layer the row is scoped away from (Rule 28).** No change was made to
  `internal/tools/*` beyond what already existed, to `internal/standards`, to any host
  scaffold, or to the dispatcher — the fix is entirely inside `internal/docindex`.
- **`internal/standards/docindex_gate_redproof_test.go` was not modified, and that is a
  CORRECTION rather than an omission.** The first cut of this fix made
  `TestDocIndexWritePathRepairsAMalformedRow` fail: dropping every malformed-row gap
  outside the added set left that gate's hand-typed row unreadable, which is exactly the
  corpus-wide red the write path exists to clear. The code was changed to match the gate
  (class decisions scoped, readability repairs corpus-wide), never the gate to match the
  code.
- **The full close-out chain was not driven through `gopi task finish-cleanup --apply`.**
  That verb needs a terminal ledger row whose `deliverable.repo/worktree/branch` point at
  a fixture, and `task submit` does not let a caller stamp those fields from the CLI; the
  honest position is recorded under `## Verification → Ran` — the changed surface
  (`docindex.SyncBranch`, which IS the chain's classification step) was executed for real
  against a real git fixture, and the `internal/tools` layer that wraps it is covered by
  its own suite (green below). `--dry-run` was also run (plan-only, printed 10 candidates).
- **Not every committed test is individually red on master, and the report says which
  are.** Of the four committed assertions about the inherited document, two are red on
  master (`TestSyncBranchNeverDecidesAClassForADocumentItDidNotAdd`,
  `TestSyncBranchCommitsOnlyWhatItWasScopedTo`) and two pass there
  (`TestSyncBranchLeavesAnInheritedRowAndItsBannerAlone`,
  `TestSyncBranchNeverReclassifiesADirtyInheritedDocument`). The reason is now known and
  is worth stating precisely: on master an inherited document only enters the repair when
  a gap is reported for it — a malformed row, or no row at all — and master's scope
  preflight refuses the dirty-inherited-path shapes that do NOT report a gap. So the two
  green-on-master tests are regressions guards for a shape master happened to survive,
  not red proofs, and they are kept because the fix's re-measure/hook/scope machinery
  runs over them (they prove the fix did not break the cases master handled). The red
  the epic measured is carried by the two that DO fail.

## Verification

- **Build:** `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK; `gofmt -l
  internal/docindex/` → no output.
- **Tests:**
  - `go test -count=1 ./internal/docindex/ ./internal/standards/` → `ok
    internal/docindex 0.912s`, `ok internal/standards 30.494s` (the second is the gate
    that caught the first cut — see `## Not Done`).
  - `go test -count=1 -run 'DocClass|FinishCleanup|DeliverySync' ./internal/tools/` →
    green (the close-out wrapper, unchanged; the run is in
    `/home/user/workspace/gopi-test-scratch/evidence/final-verify.txt`).
  - **Red-before / green-after, as a COMMITTED and REPRODUCIBLE comparison.**
    `GOPI_DOCINDEX_RED_PROOF=1 go test -count=1 -v -run TestSyncBranchRedBeforeOnMaster
    ./internal/docindex/` builds ONE driver (`internal/docindex/testdata/redder/main.go`,
    a generated nested module) twice — resolving `github.com/nick/gopi` to a scratch
    `git clone --branch master` of this repo and to this tree — runs both arms on the
    SAME three git fixtures, and asserts the transition. The driver computes each
    case's verdict from the corpus the fixture leaves behind, so the verdict is
    measured, never narrated:

    ```
    --- PASS: TestSyncBranchRedBeforeOnMaster (3.47s)
        RED on master (1 case(s)):                     # arm=before (master)
            OBS dirty-inherited-no-row arm=before verdict=FAIL
                minted_class="historical" refusal_named_path=false
                commit="acf4dd6fb9760e57ace123c9bf2c961330f1be55" abstained="" err=<nil>
        GREEN on this tree:                           # arm=fix
            OBS clean-closeout-pair arm=fix verdict=PASS banner="historical"
                row_class="historical" inherited_class="normative"
                commit="45a86014cce9625bea7fd26f7c40469c36ba23ee" err=<nil>
            OBS dirty-inherited-no-row arm=fix verdict=PASS minted_class=""
                refusal_named_path=false commit="" abstained="" err=<nil>
            OBS dirty-inherited-row arm=fix verdict=PASS class="normative"->"normative"
                file_identical=true refusal_named_path=true commit="" abstained=""
                err=refusing to classify: the corpus in …/case-fix-dirty-inherited-row holds a
                    change this check did not write (devdoc/inherited-normative.md), and the
                    classification commit would take it too — commit or discard it first, …
    ```

    The three fixtures, and what each arm observes:

    | fixture | `master` (before) | this tree (fix) |
    |---|---|---|
    | `dirty-inherited-no-row` — an inherited document with NO row, DIRTY | **FAIL**: `minted_class="historical"` — the write path's own default minted on a document this branch did not add, the epic's measured defect | PASS: no row minted |
    | `dirty-inherited-row` — an inherited `normative` row + a `historical` banner on the same file, DIRTY | PASS (master's scope preflight refuses the dirty inherited path here) | PASS: class `normative`→`normative`, file byte-identical, the refusal NAMES the path |
    | `clean-closeout-pair` — no dirt but the branch's own; a fresh report written with no row and no banner | PASS (this shape always worked) | PASS: banner ⇔ row agree, the classification commits, and the inherited `normative` class is untouched |

    **Honest reading (updated for the four-case driver, see the session-2 numbers below):
    TWO cases are red on master** — the one that carries the epic's measurement (a class
    decided for a document the branch did not add), plus `failing-commit-is-refused`,
    where master reports `refused=false abstained="the classification could not be
    committed: …" manifest_unchanged=false status_stable=false`, i.e. it abstains, leaves
    a WRITTEN manifest row behind and a dirty status, and the caller records "NOT
    checked". The original single-case reading follows for the case it describes (a class decided for a document the branch did not
    add). `dirty-inherited-row` is a regression guard, not a red proof: master refuses
    that ground for its own reason (its preflight compares against its own accepted
    set, so a dirty inherited document outside it is refused instead of classified) —
    the fix answers the same state with a refusal that NAMES the path, which is the
    behaviour the epic asked for. The committed unit tests in
    `docindex_scope_test.go` carry the finer-grained red/green (two of the four
    assertions about the inherited document fail on master — see `## Not Done`), and
    this opt-in test is the end-to-end version of the same claim. It is opt-in
    (`GOPI_DOCINDEX_RED_PROOF=1`) because it clones the repo and builds two binaries:
    honest work, but not the branch-scoped smoke a merge needs.

  - `go test -count=1 -run 'SyncBranch|DocClass' ./internal/docindex/ ./internal/standards/`
    (the epic's stated acceptance command) → both `ok`.
  - **Session-2/3 re-measurement, with the fourth fixture in the driver (numbers below
    are from this tree, HEAD `ea6f3279`, re-run in the closing session):**
    `GOPI_DOCINDEX_RED_PROOF=1 CGO_ENABLED=0 go test -count=1 -v -run
    TestSyncBranchRedBeforeOnMaster ./internal/docindex/` → `--- PASS: (4.03s)` and

    ```
    RED on master (2 case(s)):
      dirty-inherited-no-row:        verdict=FAIL minted_class="historical" refusal_named_path=false
                                     commit="e3a9331d…" abstained="" err=<nil>
      failing-commit-is-refused:     verdict=FAIL refused=false
                                     abstained="the classification could not be committed: git commit -m …:
                                       exit status 1: fixture: the commit is refused"
                                     manifest_unchanged=false doc_unchanged=true status_stable=false
    GREEN on this tree (arm=fix, all verdict=PASS):
      clean-closeout-pair:           banner="historical" row_class="historical"
                                     inherited_class="normative" commit="0b7a7f65…"
      dirty-inherited-no-row:        minted_class="" commit="" abstained="" err=<nil>
      dirty-inherited-row:           class="normative"->"normative" file_identical=true
                                     refusal_named_path=true commit="" err=refusing to classify: the corpus
                                       in …/case-fix-dirty-inherited-row holds a change this check did not
                                       write (devdoc/inherited-normative.md) …
      failing-commit-is-refused:     refused=true abstained="" manifest_unchanged=true
                                     doc_unchanged=true status_stable=true
    ok  	github.com/nick/gopi/internal/docindex	4.034s
    ```

    Read the two red rows against the fix's green rows: on master the un-committable
    classification leaves a WRITTEN manifest (`manifest_unchanged=false`) and a dirty
    status behind and reports an abstention; on this tree the same fixture reports
    `refused=true` with `manifest_unchanged=true`, `doc_unchanged=true`,
    `status_stable=true` — the unwinding the epic asked for, measured end to end.
  - `go test -count=1 -run 'SyncBranch|DocClass' ./internal/docindex/ ./internal/standards/`
    → `ok github.com/nick/gopi/internal/docindex 1.005s`, `ok github.com/nick/gopi/internal/standards 4.366s`
    (closing session, HEAD `ea6f3279`); `go build ./...` → exit 0.

- **Ran (Rule 31): the changed surface, for real, against a real git fixture.**
  A tiny out-of-tree driver (`/home/user/workspace/gopi-test-scratch/docclass-live/main.go`,
  `replace github.com/nick/gopi => <fix worktree>`) calls the exact function the close-out
  chain calls, `docindex.SyncBranch`:

  ```
  cadence: liveprobe <repoDir> <ground> <base>   # -> docindex.SyncBranch(repo, ground, base)

  # RUN 1 — an inherited `normative` row, its file carrying a `historical` banner, the
  #          inherited file DIRTY (another tool's uncommitted edit), branch adds its own:
  $ liveprobe /home/user/workspace/gopi-test-scratch/docclass-fixprobe <same> master
  err=refusing to classify: the corpus in /home/user/workspace/gopi-test-scratch/docclass-fixprobe
      holds a change this check did not write (devdoc/inherited-normative.md), and the
      classification commit would take it too — commit or discard it first, or classify by
      hand with this repo's own generator restricted to one path
  VERDICT: REFUSED (recorded reason above)
  $ git status --porcelain      # unchanged by the refusal
   M devdoc/inherited-normative.md
  ?? devdoc/branch-added.md
  $ grep inherited devdoc/DOC-INDEX.tsv
  devdoc/inherited-normative.md	normative	normative: standing claim about master (fixture)	

  # RUN 1b — the MEASURED shape (an inherited HAND-TYPED 3-COLUMN row + a
  #           `historical` banner on the same file, dirty, branch adds its own):
  $ liveprobe /home/user/workspace/gopi-test-scratch/docclass-probe2 <same> master
  err=refusing to classify: the corpus in /home/user/workspace/gopi-test-scratch/docclass-probe2
      holds a change this check did not write (devdoc/inherited.md), and the
      classification commit would take it too — commit or discard it first, ...
  VERDICT: REFUSED (recorded reason above)
  $ git status --porcelain          # nothing written, nothing staged
   M devdoc/inherited.md
  ?? devdoc/branch-added.md
  $ grep inherited devdoc/DOC-INDEX.tsv
  devdoc/inherited.md	normative	normative: standing claim (hand-typed, no evidence cell)
  $ head -1 devdoc/inherited.md
  <!-- doc-class: historical -->
  # ...the row and the banner are BYTE-IDENTICAL to before: neither half moved.

  # RUN 2 — no corpus dirt; ANOTHER TOOL's staged change outside the corpus
  #          (`A  internal/other.go`), branch adds its own document:
  $ liveprobe <repo> <repo> master
  err=<nil>
  VERDICT: commit="b5470aa6050a48f2f2f6a02fc5e39f77f69245c8" abstained="" fixed=[devdoc/DOC-INDEX.tsv] added=[devdoc/branch-added.md] gaps={Missing:[devdoc/branch-added.md] Unbannered:[] Malformed:[] Duplicated:[]}
  $ git status --porcelain      # the other tool's staging is exactly where it was
  A  internal/other.go
  $ git show --name-only --format='%h %s' HEAD
  b5470aa docs(devdoc): classify the documents this branch adds — devdoc/DOC-INDEX.tsv
  devdoc/DOC-INDEX.tsv
  devdoc/branch-added.md
  ...and the inherited row is byte-identical to before (not in the commit, not rewritten).
  ```

  The RUNs above are the first measurement. **The follow-up session re-ran the same
  surface through the committed harness, and the numbers are the ones this report now
  carries** — the driver lives in the tree (`internal/docindex/testdata/redder`), so the
  proof is reproducible instead of remembered:

  ```
  $ GOPI_DOCINDEX_RED_PROOF=1 CGO_ENABLED=0 go test -count=1 -v \
        -run TestSyncBranchRedBeforeOnMaster ./internal/docindex/
  --- PASS: TestSyncBranchRedBeforeOnMaster (3.47s)
  ok  	github.com/nick/gopi/internal/docindex	3.471s     # RED on master ×1, GREEN here ×3
  $ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/     # BUILD_OK
  $ gofmt -l internal/docindex/                          # (no output)
  $ CGO_ENABLED=0 go test -count=1 ./internal/docindex/  # ok 0.883s
  $ CGO_ENABLED=0 go test -count=1 -run 'SyncBranch|DocClass' ./internal/docindex/ ./internal/standards/
  ok  	github.com/nick/gopi/internal/docindex	0.772s
  ok  	github.com/nick/gopi/internal/standards	0.729s
  ```

  One fixture correction is worth recording, because it was a wrong INSTRUMENT and not a
  wrong fix: the first cut of the driver committed its "inherited" document on the
  FEATURE branch (the fixtures forked before the inherited state was committed), so
  `git ls-tree master` did not carry the path and the branch's own write path was RIGHT
  to call it an addition. The fixture now commits the inherited state on `master` and
  forks afterwards — `forkBranch` — and the arm that used to report no divergence now
  reports the epic's measured one.

- **Also run:** `./bin/gopi task finish-cleanup --dry-run` →
  `finish-cleanup (dry-run): candidates=10 would-run=2 settled=7 refused=1 — pass --apply to run the chain`
  (plan-only mode). `./bin/gopi sandbox show` → `sandbox root=/home/user/workspace/gopi-sandbox`.

- **Result:** the classification commit carries only the branch's own addition and the
  manifest; an inherited row's CLASS is never decided by another branch; a ground that
  moved during the write is refused with the path named and nothing written; another
  tool's staged change is neither carried nor disturbed, and the branch is left clean.
- **Residual, disclosed:** in the committed unit tests, the two assertions that are green
  on master as well as on the fix (`LeavesAnInheritedRowAndItsBannerAlone`,
  `NeverReclassifiesADirtyInheritedDocument`) are NOT red-before proofs — they are
  regression guards for the shapes master already refused, and the two that carry the red
  are `NeverDecidesAClassForADocumentItDidNotAdd` and `CommitsOnlyWhatItWasScopedTo`.
  The end-to-end proof (`## Verification` → the committed driver) is red on master on
  exactly one of its three cases, the one that carries the epic's measurement.

- **Commits:** `e0f75a8d` `fix(docindex): the truth-class write path is branch-scoped and
  atomic`; `cad3b0ab` `fix(docindex): scope the CLASS decision, repair readability
  corpus-wide`; `43346541` `test(docindex): the red-before proof is a committed,
  reproducible comparison against master`; `ea6f3279` `fix(docindex): a refused
  classification leaves no half-state behind`; plus the report's own commit (branch
  `task/01a0dc002d8879339096d3409711368b`).

## Evidence (self-filed)

- **Red on master, green on the fix** — the CURRENT, reproducible form is the committed
  driver: `GOPI_DOCINDEX_RED_PROOF=1 CGO_ENABLED=0 go test -count=1 -v -run
  TestSyncBranchRedBeforeOnMaster ./internal/docindex/` → `--- PASS` with `RED on master
  (1 case(s)): dirty-inherited-no-row … verdict=FAIL minted_class="historical"` and all
  three `arm=fix` cases `verdict=PASS` (the full OBS lines are in `## Verification`). It
  clones `master` itself, so it does not depend on a scratch worktree outliving the
  session. The earlier hand-run red (master `795d00f5`, a scratch worktree, sources reset
  with `git checkout -- internal/docindex/`) is what the driver was built to replace: its
  worktree was scratch and is gone, and a report must not rest on a path that no longer
  exists.
- **One wrong INSTRUMENT, found by running the driver rather than by reading it:** the
  first cut built its fixtures with the inherited document committed on the FEATURE
  branch, so `git ls-tree master` did not carry it and the fix's own write path was right
  to call it an addition — the arm reported no divergence from master. The fixtures now
  commit the inherited state on `master` and fork afterwards (`forkBranch`); the before
  arm then reports the epic's measured divergence (`minted_class="historical"`). This is
  recorded because it is the failure mode a red/green claim is most likely to hide: a
  fixture that describes a different state and a driver that agrees with itself.
- **The earlier live runs** (the `liveprobe` driver against
  `/home/user/workspace/gopi-test-scratch/docclass-fixprobe`, which still holds the
  resulting commit `b5470aa6` and `A  internal/other.go`) are pasted above with the full
  command and output; they exercise the same function the committed driver does, and the
  committed driver is the form that outlives this session.
- **Uncommitted-work declaration:** this report is written **after** the code commit; the
  report file itself plus its manifest row are committed as the follow-up commit on the
  same branch (the report is part of the deliverable, per Rule 5).
- **One incident, disclosed:** while looking for a CLI entry point, an exploratory
  `git worktree add ... -b feat/docclass-live-probe master` was created and then a shell
  redirection clobbered that worktree's `devdoc/` checkout. The worktree was removed
  (`git worktree remove --force`), the throwaway branch deleted
  (`Deleted branch feat/docclass-live-probe (was 4bf62a2a)`), and the MAIN repo's corpus
  verified intact (`wc -l devdoc/DOC-INDEX.tsv` → `2431`; the manifest's head is
  unchanged). Nothing in `/home/user/work/GoPi`'s checkout or refs was modified.

## Follow-up (2026-09-26, task 01a0dc7d): the scope preflight stopped refusing over OUT-OF-CORPUS dirt

**Symptom, measured at pristine master `dd82af4a`:** `TestDeliveryPointSyncRefusesToClassifyOverUnrelatedDirt`
(`internal/tools/task_finish_docclass_test.go:110`) failed — the close-out returned
`Status: verified` with `commit 6065e737…` on a ground that also held `someone-elses.txt`,
a file the check did not write. The epic's red-proof test (`TestSyncBranchRedBeforeOnMaster`)
was failing too, for a different reason (below).

**Root cause.** `assertTreeIsScoped` (`internal/docindex/scope_git.go`) carried the predicate
the epic wants — *refuse any dirty path not in the allowed set* — until commit `e0f75a8d`
added an early skip for every path that did not contain the corpus directory, justified as
"the commit cannot take it". That justification is TRUE of the classification's own commit
(`commitPaths` builds a private index from `HEAD` plus the allowed paths, so nothing else can
be carried) and FALSE of the step immediately downstream: the delivery-point sync's alignment
`git merge` runs in this very ground, and git refuses to start a merge over an uncommitted
change its own trees touch. The loosening was therefore made from inside one arm of a chain
whose other arm still needed the strict predicate.

**Fix.** The documented predicate is restored, with no directory carve-out and no untracked
carve-out: any dirty path outside the allowed set refuses, and the refusal names that path
(`scopedRefusal` already did). A tolerance for `??`-outside-the-corpus was written FIRST and
then **measured**: on the pin's own fixture `assertTreeIsScoped` returned `nil` and the pin
still failed with `Status: verified` — which is why the shipped form has no tolerance at all.

**Second finding — the red proof was vacuous.** With the refusal restored, `TestSyncBranchRedBeforeOnMaster`
still failed, but on its own premise: it requires `master` to be RED on at least one case, and
the two arms (`master` vs this tree) had come to agree on all four cases. The driver also had
no case for the shape the epic actually measured. A new case
(`dirty-outside-corpus-refused`) was written, and its first cut was the same trap one level
down: an **untracked** file reads `??` on both arms, so both arms skip or both refuse and the
case proves nothing. The shipped case anchors on the shape the pre-fix code demonstrably
mishandles — a file written, COMMITTED, then MODIFIED, so git reports it ` M` to both arms and
the pre-fix preflight skips it by directory. Measured, same test run:

```
RED on master (1 case(s)):
  dirty-outside-corpus-refused: arm=before verdict=FAIL refused=false refusal_named_path=false
    manifest_unchanged=false tip_unchanged=false outside_unchanged=true
    commit="a2a2d831…" abstained="" err=<nil>
```

i.e. on `master` the classification ran, wrote the manifest, minted a commit, and never
mentioned `someone-elses.txt`; with the fix it refuses and names it, with the manifest, the
tip and the unrelated file untouched.

**One test re-anchored, assertions unchanged.** `TestSyncBranchCommitsOnlyWhatItWasScopedTo`
(`internal/docindex/docindex_scope_test.go`) had asserted that a staged change outside the
corpus must NOT stop a classification — the same premise the fix overturns. Its subject, the
private-index isolation, belongs to `commitPaths`, so the driver now calls `commitPaths`
directly on the same real repository and keeps every assertion (only the classified paths are
carried; the other tool's staged change stays staged). That is a driver change the new
contract forces, not an assertion edited to match broken behaviour.
