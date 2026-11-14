<!-- doc-class: historical -->
# Task Report: the anchor-number census writer and the gate reader are ONE reason predicate

Task `01a131b1-fe13-7cec-ab1b-52d2ff67d208`, branch `task/01a131b1fe137cecab1b52d2ff67d208`,
cut point `e2271f8`.

## Completed

- **`docclass.AnchorNumberBaselineKeptReason` is the ONE keep predicate**
  (`internal/docclass/anchor_number_baseline_table.go`), spelled as exactly the equality
  the checker accepts a stored reason by — `stored == AnchorNumberBaselineReasonFor(finding,
  claim.Base, measuredPreExisting)`. It is exported and lives beside the comparison, so a
  writer that keeps a reason and a reader that accepts one are one expression (Rules 13/14).
- **The generator's weaker copy is deleted.** `reasonsFor` in
  `internal/docclass/testdata/anchor-number-census.go` now calls the exported predicate; its
  own half-matching rule (keep a stored reason whenever its *provenance* half still matched,
  then carry the **whole** row forward, class tail included) is gone.
- **The checker judges every permit by the same equality** — `anchorNumberReasonMismatches`
  now (a) judges the row under **the claim the row itself makes** (`first-frozen` and
  `pre-existing` are both decidable), instead of skipping every row without the
  `pre-existing` mark, and (b) reports an **unreadable** reason as a mismatch rather than a
  silent skip (Rule 15).
- **The report is event-driven** — `reasonsDelta.Expected[key]` carries the expectation
  computed *where the judgement is made*, and `compare()` prints that, never the writer's
  carried-forward value. The old line printed `reasons[p.Key]`, i.e. the row's own
  carried-forward reason on **both** halves, which is how three rows were reported as
  `REASON NOT REPRODUCIBLE` while `stored` and `produced` showed identical bytes.
- **Table regenerated in place by its own generator** (Rule 36/38: DATA, never hand-edited):
  4 lines changed — the scanned-row figure in the header and **3 reason tails, 0 keys**.
- **Regression tests** (`internal/docclass/anchor_number_census_predicate_test.go`): the
  writer's kept reason is accepted by the reader; the pre-fix rule's is refused; a
  `first-frozen` row is judged like any other; the shipped table is a fixpoint of the
  writer's predicate.
- **Sibling family checked**: `prose-anchor-census` and `prose-path-line-census` are **not
  the same disease** — measured below.

## Not Done / honest partials

- **The epic's face (A) did NOT reproduce at this base.** The epic (and the sibling row
  `01a13141614272db8d96e0ede7ddb436`) reports that seeding a *copied* out path and running
  twice flips three reason tails. Measured on a **pre-fix worktree detached at my own base
  `e2271f8`**, the two copied-path runs were **byte-identical** (`cmp` empty, rc=0), and
  `--check` on that pre-fix tree printed exactly **3** `REASON NOT REPRODUCIBLE` rows and
  exited 1. Face (B) — the pristine-tree red that regeneration cannot repair — is the live
  face at this base.
  **Inference, labelled as such (not measured here):** a flip needs the *seeded* table to
  carry a provenance claim the measurement contradicts (then run 1 keeps the stale row and
  run 2 re-derives it). The pre-fix writer cannot produce that state from **its own** output
  — it is a fixpoint on what it writes — so the flip requires a seed written by a different
  rule revision. That is consistent with both measurements; I did not construct the seed
  (a hand-mutated table copy is not evidence I am willing to call measured).
- **Two branches fix this defect concurrently.** Sibling task
  `01a131ac-f2d7-724e-ac04-0b4848cfdfa4` (branch
  `task/01a131acf2d7724eac040b4848cfdfa4`, commits `43c3cd5b4`/`59c7bf76b`) is **not** an
  ancestor of my BASE_REF, measured the same defect at base `2505061`, and fixes it the same
  way. I adopted the identical predicate name and shape so the merge is a no-op wherever the
  two agree — but the two branches edit the **same lines** of
  `anchor_number_baseline_table.go`, the same 3 table rows and the same generator function,
  so a merge WILL conflict textually. Whoever merges second reconciles by hand; the
  reconciled result should be the exported predicate + `Expected`, which both sides carry.
- This branch does **not** touch the sibling generators' code (nothing to change — see below).

## Verification

- **Build**: `go build ./internal/docclass/ && go vet ./internal/docclass/` → OK (no output).
- **Tests**: `make test-budget PKGS='./internal/docclass/'` →
  `outcome=pass exit=0 … elapsed=12s` (stamp `2026-10-13T04:37:27+08:00 host=hosta`).
  Includes `TestAnchorNumberGateOnTheRealTree` (0.11s) and the new
  `TestAnchorNumberCensusShippedTableIsTheWritersFixpoint` (0.11s).
- **Ran** (verbatim, on this tree):

  **(a) copy-out-path fixpoint**
  ```
  cp internal/docclass/testdata/anchor-number-baseline.tsv <session>/artifacts/copy-seed.tsv
  go run internal/docclass/testdata/anchor-number-census.go . <session>/artifacts/copy-seed.tsv
  cp …/copy-seed.tsv …/copy-run1.tsv
  go run internal/docclass/testdata/anchor-number-census.go . <session>/artifacts/copy-seed.tsv
  cp …/copy-seed.tsv …/copy-run2.tsv
  cmp …/copy-run1.tsv …/copy-run2.tsv
  ```
  **Result**: `cmp: identical (empty)`; both `sha256 =
  14eaed91ade8d28cf134abdf3a156edfdf86024031b6c636e365ef8c9ee57ee0`. The only body
  difference from the committed artifact is the header's own usage line (it names the out
  path) — no data row differs.

  **(b) in-place recipe, then clean worktree**
  ```
  go run internal/docclass/testdata/anchor-number-census.go . internal/docclass/testdata/anchor-number-baseline.tsv
  sha256sum internal/docclass/testdata/anchor-number-baseline.tsv
  git status --porcelain
  ```
  **Result**: sha `41d0b2bad8ac74534bac143e3a915e11b118fae48302277b768dfd50a0c2ca62` both
  before and after the re-run; `git status --porcelain` listed only the three files this
  change edits — no table churn, so the regeneration is a fixpoint.

  **(c) red-green on one tree** — perturb one anchor number, then restore it:
  ```
  sed -i '16s|at if contentReturning {|at package agent|' devdoc/DOC-INDEX.tsv
  go run internal/docclass/testdata/anchor-number-census.go . internal/docclass/testdata/anchor-number-baseline.tsv --check   # rc=1
  cp <session>/artifacts/DOC-INDEX.orig.tsv devdoc/DOC-INDEX.tsv
  test "$(git hash-object devdoc/DOC-INDEX.tsv)" = "$(git rev-parse HEAD:devdoc/DOC-INDEX.tsv)" && echo RESTORED
  go run internal/docclass/testdata/anchor-number-census.go . internal/docclass/testdata/anchor-number-baseline.tsv --check   # rc=0
  ```
  **Result**: perturbed → `1 unpermitted, 1 stale permit(s)` and rc=1; restored byte-exact by
  `git hash-object` against the HEAD blob; `--check` then prints
  `table matches the tree — the freeze is exact` and rc=0. The comparison was **not**
  loosened: a real key delta still reds (a key delta means a different recorded text, i.e. a
  different claim — a pure number mutation stays permitted because the number is deliberately
  not part of a permit's identity, `AnchorFinding.Key`).

  **(d) the package suite** — see Tests above (`make test-budget PKGS='./internal/docclass/'`).

  **(e) mutation arm 1 — the pre-fix keep rule back in the exported predicate**
  ```
  # AnchorNumberBaselineKeptReason body replaced by: keep `stored` whenever
  # claim.Base == freeze && claim.PreExisting() == preExisting   (the old half rule)
  go test ./internal/docclass/ -run 'TestAnchorNumberCensus|TestAnchorNumberGate' -count=1
  ```
  **Result**: `--- FAIL: TestAnchorNumberCensusStaleClassTailIsReplaced` +
  `--- FAIL: TestAnchorNumberCensusFirstFrozenRowIsJudged` → `FAIL … 1.883s`.
  Restore: `git checkout -- internal/docclass/anchor_number_baseline_table.go` then
  `test "$(git hash-object …)" = "$(git rev-parse HEAD:…)"` → `RESTORE OK:
  6ed315838504cf76aebabcee8767b6d66b893213 == HEAD`, and the same test set went green.

  **(e′) mutation arm 2 — the old mark gate back in the checker**
  ```
  # one added line: if !strings.Contains(p.Reason, AnchorNumberBaselineReasonMark) { continue }
  go test ./internal/docclass/ -run 'TestAnchorNumberCensus' -count=1
  ```
  **Result**: `--- FAIL: TestAnchorNumberCensusFirstFrozenRowIsJudged … the mark gate is
  back` → FAIL. Restore verified by `git hash-object` = HEAD again.

  **Sibling family (deliverable 3)**
  ```
  go run internal/docclass/testdata/prose-anchor-census.go     . …/prose-anchor-baseline.tsv     --check   # rc=0, "UP TO DATE — 0 frozen citation(s), 12 scanned"
  go run internal/docclass/testdata/prose-path-line-census.go  . …/prose-path-line-baseline.tsv  --check   # rc=0, "UP TO DATE — 13 frozen citation(s) match the tree by key"
  ```
  **Result — a different disease, and the reason is mechanical**: neither generator has a
  writer-side carry-forward rule to disagree with its reader. `prose-anchor` writes ONE
  package constant as every reason (`proseAnchorReason`) and `prose-path-line` likewise
  (`pastEOFReason`) — both are `"pre-existing: measured at the freeze commit, awaiting a human
  re-point"`, a sentence with **no class tail**, so there is no second half for a writer to
  keep while the reader compares the whole string. `prose-path-line` additionally states, in
  its own header, that its compared artifact is the permit **population** and never the
  measured figures. Neither needs a change; both `--check` arms exit 0 on this tree.

## Evidence (self-filed)

- `make test-budget PKGS='./internal/docclass/'` → `outcome=pass exit=0`, stamp
  `2026-10-13T04:37:27+08:00 host=hosta pkg=./internal/docclass/ budget=25m elapsed=12s`.
- `go build ./internal/docclass/ && go vet ./internal/docclass/` → exit 0, no output.
- Copy-path `cmp` → empty (`sha256 14eaed91…` twice); in-place sha `41d0b2bad8…` unchanged by
  the re-run.
- Pre-fix red, same tree: `--check` printed `3 unreproducible reason(s)` and **exit 1**, and on
  each of the three lines the `stored` and `produced` halves were **byte-identical** — the report
  was unfalsifiable and regeneration could not repair it. With the fix's two files in place and the
  OLD table still on disk the same command still exits 1, but the two halves now **differ** (in the
  class tail alone, which is the real delta); after regenerating, `--check` prints
  `table matches the tree — the freeze is exact` and exits 0, and the regenerated table is
  byte-identical to this branch's.
- Pre-fix worktree at base `e2271f8` (detached; re-measured this run at
  `/dev/shm/gopi-prefix-01a131b1`, removed afterwards, and the earlier run used a worktree under
  `/home/user/workspace/`): `--check` → 3 unreproducible rows, rc=1; copied-path two runs →
  `cmp` rc=0 (face (A) did not reproduce).
- Restores: `git hash-object internal/docclass/anchor_number_baseline_table.go` →
  `6ed315838504cf76aebabcee8767b6d66b893213` = `git rev-parse HEAD:…` (both mutation arms).
- Corpus figure quoted anywhere in this report (`242 anchor rows scanned, 159 incoherent`) was
  produced by `internal/docclass/testdata/anchor-number-census.go` at commit `b8ecae820` on
  branch `task/01a131b1fe137cecab1b52d2ff67d208` — regenerate it with the `--check` form above
  (Rule 38: a census figure names its generator and the commit).
- `git status --porcelain` after the in-place recipe listed only this change's own files; the
  verification artifacts live outside the worktree under
  `/home/user/.gopi/sessions/01a131b1-fe13-7cec-ab1b-52d2ff67d208/artifacts/` (Rule 30/37).

## Commit

`b8ecae820` — `fix(docclass): the census writer and the gate reader are ONE reason predicate`.

## Files

- `internal/docclass/anchor_number_baseline_table.go` — exported keep predicate; checker
  judged by the same equality; `Expected` on the mismatch report.
- `internal/docclass/testdata/anchor-number-census.go` — writer calls the exported predicate;
  report prints the judgement's expectation.
- `internal/docclass/testdata/anchor-number-baseline.tsv` — regenerated in place (3 tails).
- `internal/docclass/anchor_number_census_predicate_test.go` — new regression tests.
