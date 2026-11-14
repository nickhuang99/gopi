<!-- doc-class: historical -->
# Task Report: an absent base directory is SKIPPED by the published-anchor scan, an unreadable one still fails loudly

**Task:** `01a12bb0-a7d2-7c4e-87cb-77653d6e23bf`
**Branch:** `task/01a12bb0a7d27c4e87cb77653d6e23bf`
**Files:** `internal/standards/generated_doc_deps.go`,
`internal/standards/generated_doc_deps_test.go`

## Completed

- **The defect.** `generatedDocGatePackages` walked two hard-coded bases —
  `"internal"` and `"cmd"` — with `filepath.WalkDir` and returned the walk's
  error. On a tree that HAS `internal/` but has no `cmd/` (a fixture repository
  shaped like the real tree minus one half, a consumer tree of another shape),
  `WalkDir` answers `lstat …/cmd: no such file or directory` and the whole edge
  became a hard error. A caller could then only **abstain** — which is how a
  touch of a published anchor source in such a tree came back carrying raw
  `lstat` text (`task_finish_gendoc_test.go:189`,
  `run-1791734278-4009675.json`) instead of a measured answer.
- **The fix.** The base list is now a package-level value
  (`generatedDocBaseDirs`, Rule 1/13/14 — the two directory names are named once,
  not spelled at the call site), and each base is measured **before** it is
  walked by one predicate, `generatedDocBaseAvailable(dir, base)`, which separates
  two facts the pre-fix code ran together (Rule 15):

  | condition | disposition |
  |---|---|
  | absent (`fs.ErrNotExist`) | **skipped, and recorded** — not an error |
  | exists but is not a directory | **hard error** naming the base and the path |
  | any other `stat` error (permission, I/O) | **hard error**, wrapped with `%w` |

  A base that is *absent* is a tree of a different shape; a base that is *there
  but unreadable* is a tree this scan could not read. Only the first may pass —
  the second keeps failing loudly, so a scan that never ran cannot report itself
  as "no package reads this document".
- **The skip is visible, not silent.** The skipped bases are recorded on the
  discovered edge (`generatedDocDep.SkippedBases`) and rendered into the finding
  they would otherwise shorten — the "no test package reads this document"
  finding now ends with *"(this tree has no base dir cmd, so the scan could not
  look there)"* (`generatedDocSkippedBaseNote` +
  `generatedDocAbsentBaseNote`).
- **Doc comment corrected** on `GeneratedDocGates` and on `generatedDocBaseDirs`:
  the shape of the answer on a differently-shaped tree is stated where a consumer
  reads the contract.
- **The read floors are untouched**: `generatedDocMinDeps` (1) and
  `generatedDocMinNodes` (2) are unchanged, and every pre-existing assertion
  (`TestGeneratedDocDepsCoverTheReferenceDocuments`,
  `TestGeneratedDocAnchorChangeRequiresItsGate`, `TestGeneratedDocImpactRedProof`,
  `TestGeneratedDocCandidatesAreTheReferenceDocuments`,
  `TestGeneratedDocMandateNamesTheGeneratorAndTheGate`) is unchanged and green.

## Not Done

- **The chain's other end.** `GeneratedDocGates` is exported for the
  delivery/verify path, but **no consumer of it exists on this tree yet** — the
  only callers are in `internal/standards/generated_doc_deps_test.go`. The
  briefing names a consumer surface in `internal/tools`
  (`task_finish_gendoc_test.go`, `TestPublishedAnchor*`); **neither file nor test
  exists anywhere in this tree** (`git ls-files | grep -i gendoc` → empty across
  all 655 refs). So the abstention the briefing reports was produced by a
  consumer that is not on this baseline, and the fix was verified against the
  **exported surface itself** driven for real (see `Ran:` below) rather than
  against a consumer that is not here.
- **The pre-existing failure is not mine and is not fixed here.**
  `TestRemovalAuditGateEveryRemovalSiteEmits` fails on this branch **with my
  changes reverted and the worktree clean** — it is a standing defect in
  `internal/tools/task_finish_cleanup.go:4310` / `:4339`, filed as its own bug
  task `01a12bcc-1fff-7d8d-9409-f5d56277da05` with the two-run evidence attached
  (Rule 41). The branch-scoped suite is therefore RED by exactly that one
  pre-existing name, before and after.

## Verification

- **Build:** `go build ./...` → OK (exit 0). `go vet ./internal/standards/` → OK.
- **Tests:** `make test-budget PKGS='./internal/standards'` → 65s, exit 1,
  `outcome=fail`, **exactly one** failure — `TestRemovalAuditGateEveryRemovalSiteEmits`
  (pre-existing, reproduced on the clean tree with my two files reverted).
  Stream: `/dev/shm/gopi-01a12bb0-tb/queue/test-budget/run-1791736086-4126550.json`.
  Focused re-run of the touched surface:
  `go test -count=1 -run 'GeneratedDoc' ./internal/standards/` → `ok`.
- **Ran:** the real exported surface, on a real tree outside the repository, from
  a throwaway module that imports it the only way an `/internal/` package may be
  imported (a sibling module with a `replace`):

  ```
  go run -C /dev/shm/gopi-01a12bb0-probe/probe . \
      /dev/shm/gopi-01a12bb0-probe/consumer-tree internal/standards/consumer_anchor.go
  ```

  The tree `/dev/shm/gopi-01a12bb0-probe/consumer-tree` holds `go.mod`,
  `COMMANDS.md` (generated region + `go run` instruction + a published anchor),
  `internal/standards/{gate_test.go,consumer_anchor.go}` and
  `internal/standards/testdata/consumer-census.go` — **`ls` shows `COMMANDS.md
  go.mod internal` and no `cmd/`**.
- **Result:** four real runs of that surface, all exit 0 for the probe:

  **(1) tree with no `cmd/`** — the defect's exact shape:
  ```
  required=[internal/standards]
  findings=[]
  err=<nil>
  ```
  The anchor touch is **MEASURED** (the fixture's gate package comes back as
  required) and there is **no error** — the pre-fix code returned
  `generated-doc deps: scan cmd: lstat …/cmd: no such file or directory` here.

  **(2) base PRESENT but unusable** (`cmd` replaced by a regular file):
  ```
  err=generated-doc deps: base cmd is not a directory (/dev/shm/…/consumer-tree/cmd)
  ```
  — the hard error is intact, and `GeneratedDocMandate` refuses too.

  **(3) no `cmd/` AND no gate package** — the skip is STATED, not swallowed:
  ```
  findings=[COMMANDS.md: no test package reads this document — a line moved under
  one of its anchors would reach the integration branch uncaught (this tree has
  no base dir cmd, so the scan could not look there)]
  err=<nil>
  ```

  **(4) the same tree with the consumer's gate package restored** →
  `findings=[]`, `required=[internal/standards]`, `mandate` naming the document,
  the generator and the gate package.
- **RED → GREEN, recorded both runs.**
  - **RED** (fix reverted to the pre-fix mutation — the walk runs
    unconditionally): `TestGeneratedDocGatesAnswersOnATreeWithoutTheBaseDir` and
    `TestGeneratedDocGatesStatesTheBaseItCouldNotWalk` both fail with the raw
    `generated-doc deps: scan cmd: lstat /dev/shm/suite-standards-run-…/cmd: no
    such file or directory` text, and
    `TestGeneratedDocGatePackagesRefusesAnUnusableBase` fails its "must fail
    loudly" arm. Evidence: `artifacts/red-run.txt` (session dir).
  - **GREEN** (fix restored, `sha256sum -c` over the saved files OK): all four new
    tests PASS. Evidence: `artifacts/green-run.txt`.
  - The reverted run was a **temporary working-tree mutation** — the fixed file
    was restored byte-for-byte and re-verified with `sha256sum -c`; the mutation
    never entered a commit.
- **Commit:** `31a9e34e9` — `fix(standards): an absent base dir is skipped by the
  published-anchor scan, an unreadable one still fails loudly` (branch
  `task/01a12bb0a7d27c4e87cb77653d6e23bf`; `git status --porcelain` in the
  worktree prints nothing after it).

## Evidence (self-filed)

- **The defect is real and reproduces as briefed** — reverted-fix run prints
  `generated-doc deps: scan cmd: lstat /dev/shm/suite-standards-run-2465788277/TestGeneratedDocGatesAnswersOnATreeWithoutTheBaseDir2834826987/001/cmd: no such file or directory`
  (`artifacts/red-run.txt`). Same text shape as the briefing's measurement.
- **The fix is measured on a real tree, not asserted** — probe output in (1)–(4)
  above; the tree's own `ls` is `COMMANDS.md go.mod internal`.
- **The pre-existing failure is pre-existing, measured twice** — with my files
  reverted (`git status --porcelain` empty) the same test fails with the same two
  line numbers: `artifacts/removal-audit-clean-tree.txt`, and the bug row
  `01a12bcc-1fff-7d8d-9409-f5d56277da05` carries it.
- **Floors intact** — `generatedDocMinDeps` / `generatedDocMinNodes` are byte-identical
  to `HEAD` (`git diff HEAD -- internal/standards/generated_doc_deps.go` shows only
  the documented changes; the constants themselves are untouched).
- **Clean worktree (Rule 30)** — `git status --porcelain` in this worktree lists
  exactly the two files of this deliverable, both committed; no untracked litter
  (probe trees live under `/dev/shm`, the evidence under the session directory).
