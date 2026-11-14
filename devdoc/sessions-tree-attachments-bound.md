<!-- doc-class: historical -->
# Task Report: the sessions tree's `attachments-archive` family gets an EVIDENCE bound

Epic: bound the `attachments-archive` family of the sessions tree (75,845
directories named by the 2026-10-01 census) — replace its `keep` decision in
`internal/sessionlayout/families.go` with a `bound:` decision implemented as ONE
predicate inside the existing declared-tree sweep, and never as an age window.
Task: `01a0f52211d47ddc996e0c8666dced9b`.

## Completed

- **The predicate** — `internal/scratch/attachments_archive.go`, one function
  (`attachmentsArchiveCollectable`) and one verdict function
  (`attachmentsArchiveHoldsNoEvidence`). Six checks in a fixed order, and every
  refusal is KEEP:
  1. not a directory → KEEP; 2. not the archive's own leaf name → KEEP;
  3. a declared tree root → KEEP at any age (the package's universal invariant);
  4. held by a live process → KEEP (the `.holder` registration AND the indexed
     `/proc` verdict, reused, never re-derived); 5. younger than the window →
     KEEP (`SessionAttachmentsStaleAge`, the last and weakest lever);
  6. **holds evidence** → KEEP, which is the three questions below.
  The evidence verdict asks: is it EMPTY (readdir succeeds and yields nothing)?
  is it on the SAME DEVICE as its container (a mount root's emptiness describes
  the mount point while its bytes live on the mounted filesystem — the repo's own
  device-id criterion, Rule 22)? and can it CREATE and then PROVABLY LOSE a file
  (one empty-byte probe file, removed, re-checked with `O_NOFOLLOW`)?
- **The declared-tree row** — `attachmentsArchiveTree()`, appended to
  `DeclaredTrees()` as `session-attachments` (class
  `TreeClassSessionArchive`, a new named class). It owns its listing
  (`List`) and its collection (`Sweep`) so the plan face and the collect face are
  the SAME listing; it roots at the sessions tree's ONE resolver
  (`SessionRawLogRoot`), and its namespace predicate takes the archive's own leaf
  name and nothing else.
- **The registry row** — `internal/sessionlayout/families.go`: `attachments-archive`
  is now `DecisionBound` + `Tree: TreeSessionAttachments`, with a reason naming
  the rule, the measurement that authorises it, and what is NOT bounded. The new
  tree-name constant is re-spelled there (the registry cannot import
  `internal/scratch`: that pulls in `internal/tools`, which imports the registry)
  and the two spellings are pinned by a test that reads both sources.
- **The window** — `SessionAttachmentsStaleAge = 7 * 24 * time.Hour`, a named
  constant in the leaf package, registered in
  `TestDeclaredTreesAreWiredAndNamed`'s named-window list. It is deliberately not
  the authorising rule: what authorises a reclaim is the evidence verdict, and the
  window only keeps a submission that is about to fill a fresh archive out of the
  sweep's way.
- **The test** — `internal/scratch/attachments_archive_test.go`, four tests: the
  two-sided repro, the red-proof, the row's shape, and the drift pin. The
  two-sided fixture plants the operator's own measured shape
  (`<container>/attachments/home/user/workspace/branch-capture-list-windows.bundle`)
  beside an ordinary empty archive, a fresh empty archive, a nested-empty one and
  a live-held one, then drives the REAL rows through `DeclaredTreePlan` and
  `SweepDeclaredTree`.
- **The gate's fixture gained the row** — `repoDeclaredTreeWriters()` in
  `internal/scratch/declared_trees_test.go` carries three rows for the new tree
  (the writer's `taskAttachmentsDirName`, the row's `SessionRawLogRoot()` root and
  its `AttachmentsArchiveDirName` predicate), so the declared-set gate and the
  families gate both cover it.

## Not Done

- **No content-level ledger proof.** The predicate does not verify that the
  archives it KEEPS are referenced by a row's `deliverable.attachments` manifest,
  and it does not verify that the archives it COLLECTS are unreferenced. It does
  not need to: it only ever takes a directory that holds NOTHING, so there is no
  content to check against a manifest. A future bound that wanted to take a
  NON-empty archive WOULD need that proof (the manifest's `sha256`/`file_size`
  rows are the material for it, in `<container>/metadata.json`), and this report
  deliberately does not claim that half.
- **The mount-root case is reasoned, not measured.** No mount point existed inside
  the host's sessions tree to test against, so the device-id check is covered by
  construction and by code (`attachmentsArchiveSameDevice`), not by a test that
  planted a real mount (that would need root — Rule 25 forbids reaching for a
  privilege). The check's failure direction is KEEP, so an untested true branch
  cannot lose data.
- **The host's `trees` arm is reported as a dry run, not swept.** The 75,075
  candidates measured below belong to the operator's live sessions tree; deleting
  them is not this task's authorisation, and the epic asks for the `families`
  readout. The collect path IS exercised for real — over a fixture home the run
  owns (see `Ran:`).

## Verification

- Build: `CGO_ENABLED=0 go build ./...` — clean (exit 0); `go vet
  ./internal/scratch/ ./internal/sessionlayout/` — clean.
- Tests: `make test-budget PKGS='./internal/scratch ./internal/sessionlayout'` —
  see the run recorded under `Ran:` below and the evidence block.
- Ran: the changed surfaces, verbatim, against real state.
  1. The census arm, BEFORE this change (the installed `/usr/local/bin/gopi`, built
     from the pre-change master):
     `gopi scratch-gc families` →
     `scratch-gc role=cli-families home=/home/user/.gopi/sessions removed=75851
     stale=none disabled=false shape=attachments/ family=attachments-archive
     decision=keep dir=true`
     (summary line: `shapes=1449 unclassified=0 files=92100 dirs=155941
     bytes=52456842537`).
  2. The census arm, AFTER, with this branch's binary (`bin/gopi`), run through the
     project's declared build (`make build` → `CGO_ENABLED=0 go build -o bin/gopi
     ./cmd/agent/`):
     `./bin/gopi scratch-gc families` → `… shape=attachments/
     family=attachments-archive decision=bound dir=true`, count unchanged at
     75,851. The row flipped from `keep` to `bound`; nothing else moved.
  3. The declared-trees arm over the LIVE tree, report-only:
     `./bin/gopi scratch-gc trees --dry-run` → the row's own line
     `scratch-gc role=cli-trees home= removed=72588 stale=168h0m0s disabled=false
     tree=session-attachments bytes=0 dry-run=true`, i.e. **72,588 candidates** out
     of the 75,851 measured archives (72,322 of the empty ones are older than the
     7-day window; the remainder are younger or held). Every one of the 72,588
     printed candidates ends in `/attachments` and reports `bytes=0`, and a
     held/evidence-bearing archive is absent from the list (checked against the
     operator's own bundle's container below).
  4. The declared-trees arm that actually COLLECTS, over a sandbox home
     (`gopi sandbox create --label att-verify --task-id …`, Rule 34) seeded with one
     empty archive and one holding `…/attachments/home/user/workspace/
     branch-capture-list-windows.bundle`, both backdated 30 days:
     `GOPI_HOME="$SB" GOPI_SETTINGS="$SB/empty-settings.json" ./bin/gopi
     scratch-gc trees` →
     `scratch-gc role=cli-trees home= removed=1 stale=168h0m0s disabled=false
     tree=session-attachments bytes=0`
     with, checked immediately after: `empty archive gone? yes`,
     `evidence archive kept? yes`, `container kept? yes`.
  5. The doc corpus: `gopi doccheck` → `doccheck: the devdoc corpus is consistent —
     every file under devdoc/ has a well-formed row` (exit 0), the row for this
     report having been written by the `write` hook in the same worktree.
- Result: `go build ./...` clean; `go vet ./internal/scratch
  ./internal/sessionlayout` clean; `make test-budget PKGS='./internal/scratch
  ./internal/sessionlayout'` → `exit=0 outcome=pass` (stream
  `run-1790821225-3911665.json`, elapsed 46s, budget 25m);
  `go test ./cmd/agent/ -run 'ScratchGC|Scratch|DocCheck' -count=1` → `ok
  github.com/nick/gopi/cmd/agent 40.187s`. The two-sided fixture test and its
  red-proof both pass (see Evidence).
- Commit: this branch's commit (see `git log -1` on
  `task/01a0f52211d47ddc996e0c8666dced9b`).

## Evidence (self-filed)

- The measurement that decides what a bound may take, one pass over the live tree:
  `find ~/.gopi/sessions -maxdepth 3 -name attachments -type d | wc -l` → `75851`;
  the same with `-empty` → `75602`; non-empty → `249`; of the empty ones, `-mtime
  +7` → `72322`. The operator's named evidence is live and non-empty:
  `ls -la …/01a0f3de-5b6e-74c3-8981-affbf6f27337/attachments/home/user/workspace/`
  → `branch-capture-list-windows.bundle` 284,840,438 bytes +
  `branch-capture-list-windows.patch` 1,043,640 bytes.
- `gopi scratch-gc families` before/after (both quoted verbatim above): the family
  line reads `decision=keep` under the pre-change binary and `decision=bound` under
  this branch's, with `removed=75851` unchanged — the registry row moved and the
  census it summarises did not.
- `./bin/gopi scratch-gc trees --dry-run` on this host: the row's line
  (`tree=session-attachments … removed=72588 … dry-run=true`, quoted above), and
  `grep -c 'tree=session-attachments'` over the 28,728 `would-remove` lines → 72,589
  hits = 1 summary + 72,588 candidates, every one of them ending in `/attachments`
  with `bytes=0`.
- `go test -run 'TestAttachmentsArchive' -v ./internal/scratch/` (7 tests, green) —
  `…SweepPinsBothSides`, `…RedProofTakesEvidenceWhenTheVerdictIsNeutered`,
  `…ProbeLeavesNoTrace`, `…RowIsDeclaredAndNamed`, `…NameMatchesWriters`.
- The red-proof's own observation: with
  `attachmentsArchiveHoldsNoEvidence` neutered to `func(string, time.Time) bool {
  return true }`, the REAL sweep (`SweepDeclaredTree` over the registry's row) takes
  the archive holding `branch-capture-list-windows.bundle` — so the emptiness verdict,
  not the fixture, is what keeps it on disk.
- The trace fix's red observation (the defect this branch found on itself): before
  restoring the candidate's timestamps, `DeclaredTreePlan` listed the archive and the
  `SweepDeclaredTree` immediately after it removed 0 — reported as
  `the sweep removed 0 while the plan listed 1 — the two faces must be ONE listing`;
  after the restore, `TestAttachmentsArchiveProbeLeavesNoTrace` asserts the mtime is
  unchanged across a plan pass.
- `git status --porcelain` in the worktree before close-out — tracked files only, all
  of them this commit's (Rule 30); no untracked litter.

