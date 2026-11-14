<!-- doc-class: historical -->
# Task Report: the two docclass freeze tables regenerated with their own generators (task 01a133af9)

## Completed

- **`internal/docclass/testdata/anchor-number-baseline.tsv` regenerated through
  `anchor-number-census.go`** (Rule 38 — the generator is the only writer; no byte
  was hand-edited). Three lines moved, all of them consequences of the tree, none
  of them a hand edit:
  - the header figure `measured 244 anchor row(s) scanned` → **245** (a doc carrying
    an anchor row landed on master since the last regeneration),
  - two permits whose recorded line stopped being blank and now lands on unrelated
    code, so their CLASS moved `recorded-line-blank` → `number-drifted` and their
    reason tail moved with it:
    `devdoc/handoff-20260922-bill-analysis.md → internal/tools/tasks.go:3093` and
    `devdoc/merger-graceful-restart-20260912.md → cmd/agent/main.go:738`.
  These two rows are exactly the "2 unreproducible reason(s)" the census reported:
  the stored tail described the class the row no longer had. Both were **stale class
  tails**, not bad provenance — the keys and the `pre-existing at the 0b3c1de29
  freeze` claims never changed.
- **`internal/docclass/testdata/prose-path-line-baseline.tsv` regenerated through
  `prose-path-line-census.go`** — the SAME run proved it stale (`--check` exit 1,
  3 unpermitted citations), so the epic's "any other docclass baseline table the
  same run proves stale, same way" clause applies. Three `real-drift` permits added
  (`doubaostream-session-test-coverage.md:717`, `merge-resolve-peak-hour-guard.md:1226`,
  `merger-graceful-restart-20260912.md:738`, all cited by
  `tasks/01a13379-b0e1-714b-b967-71b5424fb9f5-report.md` as past-EOF references).
- **The third table was tested and is NOT stale**: `prose-anchor-baseline.tsv`
  `--check` exits 0 (`UP TO DATE — 0 frozen citation(s), 12 scanned`). No edit made.
- **A regression defect found by the regeneration itself, and fixed**:
  `TestProsePathLineOneStaleRowEventOneVerdict`'s counterfactual arm picked its
  victim from the **shipped** table. That selection rule has a hidden subject
  requirement — the shipped table must still hold a permit that FIRES but is NOT in
  the freeze table. Permitting the last three past-EOF citations **removed the last
  such subject**, so the pre-existing arm became `victim found → theft reported →
  assertion red` on the healthy tree the regeneration had just produced. The arm now
  picks the victim as *(fires) AND (present in BOTH the freeze and the shipped
  table)*, which is what it always meant: a **withdrawal** is a permission that
  exists and is deleted, and the freeze assertion can only see one of those. The
  counterfactual polarity is preserved — a still-firing permit's deletion is still
  the theft being caught.

## Not Done

- The two anchor permits are still **number-drifted**; the regeneration froze their
  correct class, it did not re-point them. That is by design: the generator writes
  the number out of the doc's own inline `path.go:NNN` prose (`tools/docclass/classify.py`),
  so a whole-corpus regeneration moves the TEXT of a row and can **never** repair its
  NUMBER — re-pointing is a human edit to the citing devdoc, a separate task.
- The three past-EOF citations in `tasks/01a13379-…-report.md` are permitted as
  `real-drift`, not repaired. Same reason: a report documenting a measurement is
  record, not authority, and its numbers are allowed to age.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → **OK** (no output)
- Tests: `go test ./internal/docclass/` → **ok github.com/nick/gopi/internal/docclass 18.440s**
  and `go test ./internal/docindex/ ./cmd/agent/ -run 'DocCheck|Doccheck'` → **ok / ok**.
  Branch-scoped to the packages the diff touches.
- Ran: `./bin/gopi tool doccheck` → exit **0**
  ```
  doccheck: anchor NUMBER coherence (recorded text still present, line number stale):
  doccheck:   163 of 163 are frozen in internal/docclass/testdata/anchor-number-baseline.tsv (each permit states whether its drift predates the freeze or was first frozen here; the gate fails only on drift NOT listed there)
  doccheck: the devdoc corpus is consistent — every file under devdoc/ has a well-formed row
  ```
- Ran: `go run internal/docclass/testdata/anchor-number-census.go . internal/docclass/testdata/anchor-number-baseline.tsv --check` → exit **0**
  ```
  anchor-number-census: scanned 245 anchor row(s), 163 incoherent; table carries 163 row(s)
  anchor-number-census: table matches the tree — the freeze is exact
  ```
- Ran: `go run internal/docclass/testdata/prose-path-line-census.go . internal/docclass/testdata/prose-path-line-baseline.tsv --check` → exit **0**
  ```
  …: 16 frozen citation(s) in the table, 1778 mention(s) scanned, 875 resolvable, 20 past EOF, 903 unresolved, 317 distinct cited path(s), repo root .
  …: UP TO DATE — 16 frozen citation(s) match the tree by key
  ```
- Ran: `go run internal/docclass/testdata/prose-anchor-census.go . internal/docclass/testdata/prose-anchor-baseline.tsv --check` → exit **0**
- Result: the worktree carries only the three intended files
  (`git status --porcelain` → the two `.tsv` tables + the one test file, all committed below).

## Evidence (self-filed)

- **Idempotence, measured** (the acceptance's own criterion — the regenerated table
  must be the generator's fixpoint): `sha256sum -c` over both tables after running
  each generator a SECOND time in place → `OK / OK`, and `git status --porcelain`
  printed nothing. Byte-identical, no cleanup needed.
- Red→green, observed on the SAME tree: with both tables restored to HEAD,
  `go test ./internal/docclass/ -run 'AnchorNumber|ProsePathLine'` → **FAIL**, with
  `TestAnchorNumberCensusShippedTableIsTheWritersFixpoint` printing
  `stored "…the recorded line is blank/past EOF and the text sits elsewhere" / would be "…the recorded text is present in its file, but not on the recorded line"`
  and `TestProsePathLineGateOnTheRealTree` printing the three UNPERMITTED citations.
  After the regeneration, the same command → **ok 20.148s**. The gates ARE the
  minimal repro: they call the same predicate the generator's `--check` uses, on the
  shipped artifact, so no new test file was written for the table drift.
- `sha256sum` before/after: anchor table `4cf166e182baae0c3791253fca1628f9e9aad669eda85d36dc1d01a2b485b1b2` (HEAD)
  → `cb368a16fc03f493331e4f384f3d4bede222ba04ff4248abf1e90e0b4faa474b`; prose-path-line
  `da6e7d02d1595c38d57b206584a9dbb7081b1304a9052bdc700dfeff81e5ab04` (HEAD) → `16c08541176efaf1b51e52e7f74ed0c00b6fb4c85ae0c9cff7fd1427c45d2223`.
- Class flip confirmed against the tree, not inferred from the reason text:
  `cmd/agent/main.go:738` prints `// config.ResolveTmpDirConfigured, one place, and is documented in`
  and `internal/tools/tasks.go:3093` prints `}` — both non-blank, so
  `AnchorNumberBlank` is the wrong class for both rows and `number-drifted` is right.
- Generator identity (Rule 38): `git diff --stat` shows the tables changed ONLY in the
  three lines the generator writes; every other data row is byte-identical across the
  regeneration, i.e. the committed artifact is the generator's own fixpoint.

## Commit

Branch `task/01a133af98117dc59055516958379404` (no push needed — a worktree shares
the main repo's refs, so the commits are already the branch's deliverable):

- **`361ca340c`** `fix(docclass): regenerate both stale freeze tables with their own generators`
  — the deliverable: the two `.tsv` tables + the one test file + this report +
  the `devdoc/DOC-INDEX.tsv` row the landing hook wrote.
- `666940dd1` / `1d09cd0f0` — report-only follow-ups (naming the code commit;
  recording the idempotence measurement). A report cannot name its own commit's
  hash, so the code commit's hash is quoted from the previous revision.
