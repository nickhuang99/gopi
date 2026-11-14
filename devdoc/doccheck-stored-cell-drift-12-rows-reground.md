<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# Stored-cell drift, re-grounded: 12 rows re-derived by the corpus's own generator

**Task**: `01a142d1-699e-7027-bce1-1a4b7c85f76f` · **Host**: hosta (client — Rule 28 keeps
`internal/docindex` / `tools/docclass` system-layer work on peerbox; this run measures on its own
ground and ships the measured data + this report on its own branch) · **Ground**:
`/home/user/workspace/gopi-01a142d1699e7027bce11a4b7c85f76f`, branch
`task/01a142d1699e7027bce11a4b7c85f76f`, base = peer master tip
**`4d792f64413df0b68cd892944ac5b4fbc5cf1841`**.

Every figure below is a real run on that ground; the raw outputs are under this run's session
directory (`~/.gopi/sessions/01a142d1-699e-7027-bce1-1a4b7c85f76f/artifacts/`), never in the tree.

## ① What was red, measured

On the untouched ground (before any edit):

```
$ ./bin/gopi tool doccheck
doccheck: stored-cell drift: 12 of 2583 rendered row(s) whose stored text is not what the classifier renders today
doccheck: 0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv
verdict: drift
doccheck_exit=1

$ python3 tools/docclass/classify.py . --check
DOCCLASS STALE … (12 lines, one per path)
12 row(s) whose stored text is not what the classifier renders today (reported, NOT fatal: re-derive ONE with `--only <path>`)
classify_check_exit=0
```

The two faces agree on the population (**12 of 2583**) and disagree on the exit code by design:
the Go face treats drift as fatal, the Python `--check` says drift is *not* its exit status (its
exit is the GAP count's alone). Raw captures: `artifacts/doccheck-before.txt`,
`artifacts/classify-check-before.txt`, `artifacts/drift-paths.txt`.

The briefing said **4 rows** at its own base `61b0846`; the population moves with the tree, and
`61b0846` is an ancestor of this ground's base, so 4 there and 12 here are both true statements
about different commits — the same shape the corpus already documents.

## ② Root cause: the rows are SNAPSHOTS, and the fix that landed does not reach them

A `devdoc/DOC-INDEX.tsv` row is derived from the live tree, but it is frozen at the moment the
document was last written. `classify.py` re-renders the same cell from the tree **today**; when the
tree moved underneath a document that nobody wrote again, the two readings part company and the
row becomes a stored-cell drift.

The decisive measurement is an ancestry test, not a reading of the code: the write-path fix
`55c9bcfb0` ("re-render a stored row after the document's LAST write", landed on master
2026-10-16 14:22) is an ancestor of THIS ground's base — and **every one of the 12 drifted paths was
last written BEFORE that fix**:

```
BEFORE-fix     27181dcbd devdoc/PRINCIPLE-deterministic-lifecycle-ownership.md
BEFORE-fix     762bc8ebd devdoc/branch-lifecycle-ruling.md
BEFORE-fix     0d5c5aee4 devdoc/compute-nodes-unwired-20260930.md
BEFORE-fix     5ca088731 devdoc/gopi-task-ledger-architecture.md
BEFORE-fix     c02d7c0df devdoc/ledger-readonly-faces.md
BEFORE-fix     7946a836f devdoc/local-submit-from-client.md
BEFORE-fix     670be9148 devdoc/main-checkout-deposit-arm.md
BEFORE-fix     aec9ae8f2 devdoc/merge-resolve-effort-audit.md
BEFORE-fix     b5c863772 devdoc/merger-ledger-writeback-audit-20260912.md
BEFORE-fix     21912f8b2 devdoc/pa-never-runs-tasks-20260912.md
BEFORE-fix     670be9148 devdoc/requirements/11-build-and-repos.md
BEFORE-fix     61705a55d devdoc/why-worktree-residue.md
```

So the population is exactly "documents written before the hook could re-render them and never
written again" — a **stale-stock** population, which fixes at the write seam cannot clear by
construction (the same limit the fix's own author reported: "写路径修复天然不覆盖"). Clearing it
is a one-shot re-derivation, i.e. this task. The root cause itself (a row that is a snapshot while
its generator is a function of the live tree) is **system-layer** content and is already carried by
the filed rows below — this run does not re-file it, and does not touch Go source.

## ③ The repair: the corpus's own generator, twice, and nothing else

`DOC-INDEX.tsv` is generated data — never hand-edited (Rule 36/38). The only writer used here is
the corpus's own generator, in the two granularities it offers:

```
# minimal sample first: ONE path
$ python3 tools/docclass/classify.py . --only devdoc/PRINCIPLE-deterministic-lifecycle-ownership.md
 "updated": 1, "rows": 2583
$ git diff --numstat devdoc/DOC-INDEX.tsv
1	1	devdoc/DOC-INDEX.tsv            ← exactly the one cell, both directions

# then the remaining eleven in one write
$ python3 tools/docclass/classify.py . --only-file <the other 11 paths>
 "updated": 11, "rows": 2583
$ git diff --numstat devdoc/DOC-INDEX.tsv
12	12	devdoc/DOC-INDEX.tsv
```

**Idempotence is the independent cross-check**: a *second* generator process re-rendered all 12
paths and `updated: 12` again produced the **identical file** —
`cmp artifacts/DOC-INDEX.derived.tsv devdoc/DOC-INDEX.tsv` → identical, blob
`8c759b2254562b5745ff5615e1037fa21b3d236d`. A hand-written cell would not survive that.

Both directions of every changed cell are line-number updates (`…audit.go:188`→`:222`,
`…merge_prompt.md:709`→`:742`, `…cli_tasks.go:6305`→`:6357`, `stale=1`/`stale=2`/`stale=6` counts) —
the cite target moved, the row's reading did not.

## ④ What this run did NOT change, and what it did not prove

- **No criterion was relaxed.** No permit was minted, `internal/docclass/testdata/*.tsv` is
  untouched, no threshold or floor was edited, and the drift check is still fatal on the Go face.
  The acceptance run below is the evidence that the corpus got *cleaner*, not the check weaker.
- **The root cause is not fixed here** — the next silent edit to an old devdoc can re-open drift
  for that one row until the write seam covers it. That is system-layer (Rule 28) and is already
  filed: `01a141a7-30fb-7473-b944-c6103912e304` (the write hook stores a cell the generator
  disagrees with — with its own 8↔9 bidirectional red arm) and, for the Go face's exit-code
  contract, `01a142ed-23d3-7901-971d-1dc464421232`. The same defect is also carried by
  `01a143d8-1cae-70b7-b045-d975325a2b76` (filed from another thread: "`doccheck` exits 1 on a clean
  tree — stored-cell drift, 9/2580 — no gate catches corpus cell drift"); this run re-files none of
  them (Rule 41's threshold is deliberately low, but a duplicate adds no evidence here, and all
  three were verified present on this ledger with `gopi task show`).

**What this repair did NOT reach, measured rather than assumed** — the do-nothing re-check: the
report (this document) was written AFTER the repair, through the same write hook, and
`doccheck` still returned `verdict: clean` with 0 drift. So the 12 were the whole reachable
population on this base, not a prefix of a longer list, and the repair has no follow-up rows queued
behind it. What it does NOT promise is the future: any later change to what a render *reads* (an
anchor line moving in a source file is the shape every one of the 12 had) re-opens drift on the
paths whose inputs moved, until the write seam covers rows the tree — not a writer — moved.

## ⑤ Verification (Rule 31 — the surfaces really ran)

```
$ ./bin/gopi tool doccheck            # after, re-taken on the tree at this branch's tip
  doccheck: stored-cell drift: 0 of 2584 rendered row(s) whose stored text is not what the classifier renders today
  doccheck: stored-cell drift: none — every rendered row matches the manifest byte for byte
verdict: clean
doccheck_exit=0                        # was exit 1 / verdict: drift / 12 of 2583

$ python3 tools/docclass/classify.py . --check
classify_check_exit=0
0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv
0 row(s) whose stored text is not what the classifier renders today     # was 12
DOCCLASS STALE lines: 0

$ go build ./...              → OK
$ go test ./cmd/agent/ -run TestDocCheck -count=1   → ok 5.370s
$ git diff --numstat 4d792f644..HEAD                 → 13  12  devdoc/DOC-INDEX.tsv
                                                        182  0  devdoc/doccheck-…-reground.md
$ git diff --name-only 4d792f644..HEAD | grep -v '^devdoc/'   → (empty: devdoc-only)
```

The denominator rose 2583 → 2584 between the two readings because THIS report's own row joined the
corpus; the reading above is the one taken at the tip, after that write.

**Two readings that did not move, so they are not effects of this repair.** `doccheck` reports
`41 of 276 scanned anchor row(s) are text-ambiguous (reported, not frozen)` — **41 in the
before capture and 41 in the after capture**, i.e. a pre-existing condition of the anchor check,
untouched here (freezing them would be exactly the criterion change this task forbids).

Raw captures: `artifacts/doccheck-after-final.txt`, `artifacts/classify-check-after-final.txt`.

With THIS report in the tree the manifest carries 2584 rows and the same two commands still answer
clean (`artifacts/doccheck-with-report.txt`): the new row was written by the **write hook** — the
generator's own writer, not a hand edit — and the two readings agree on it, which is this run's own
observation that the landed write-path fix works for a document whose last write goes through the
hook.

**Pre-existing, NOT this run's: `internal/standards` carries one red banner defect.**
`TestDocClassBannerMatchesManifest` fails on `devdoc/removal-audit-shell-census.md` (file bannered
`historical`, manifest row `normative`). Neither that file nor its row is in this run's diff
(`git diff -U0 devdoc/DOC-INDEX.tsv | grep -c removal-audit-shell-census` → 0), so the red predates
this branch and is already filed: `01a1432e-4441-71a6-bd4c-f94fce6cbe11`. Not re-filed.

**Red arm — the cell is still checked, and the criteria are still strict.** With the derived
manifest in place, the *pristine* (pre-repair) cell of exactly one row was planted back into
`devdoc/DOC-INDEX.tsv` by a byte copy of the `HEAD` line, and one command was run:

```
$ ./bin/gopi tool doccheck
  doccheck: stored-cell drift: 1 of 2583 rendered row(s) whose stored text is not what the classifier renders today
verdict: drift
ARM_A_doccheck_exit=1                  ← one planted cell is caught, by the row

$ python3 tools/docclass/classify.py . --check
DOCCLASS STALE devdoc/PRINCIPLE-deterministic-lifecycle-ownership.md
1 row(s) whose stored text is not what the classifier renders today
ARM_B_classify_check_exit=0            ← named; exit is the GAP count's, as the mode documents
```

Restore, anchored on a **revision** (Rule 5's timeliness half), not on a snapshot pair:

```
plant      : the pristine cell was taken from the BASE revision — `git show 4d792f644:devdoc/DOC-INDEX.tsv`
restore    : `git show HEAD:devdoc/DOC-INDEX.tsv > devdoc/DOC-INDEX.tsv`
identity   : worktree_blob=c9aeeb1eca5124b506cd3bc5a9e42c2d4e64f1ab  HEAD:devdoc/DOC-INDEX.tsv=c9aeeb1eca5124b506cd3bc5a9e42c2d4e64f1ab  same=YES
idempotence: the generator re-rendered all 12 paths AFTER the restore → blob UNCHANGED, `git diff --numstat` 0 lines
```

**Re-take, because the file moved after the first reading.** The reading above was first taken when
`HEAD:devdoc/DOC-INDEX.tsv` hashed to `8c759b2…`; the report's own row was written afterwards, so the
tip's blob is now `c9aeeb1…` — a content-identity reading that still said `same=YES` across that write
would have been true only about the moment it was taken, so both the green reading and the red arm
were **re-taken on the tip**: the green reading was repeated at the tip (drift 0 of 2584, `verdict:
clean`, exit 0 — `artifacts/doccheck-final-tip.txt`) and the red arm was re-run at the tip with its
pristine cell planted from the base revision and removed with the restore command above
(`artifacts/redarm2-doccheck.txt`, `artifacts/redarm2-classify-check.txt`).

Red-arm captures: `artifacts/redarm-doccheck.txt`, `artifacts/redarm-classify-check.txt`; the
planted manifest never left this worktree and its pristine copy is `artifacts/DOC-INDEX.derived.tsv`.

## Second pre-existing red, same shape (re-verified at the tip)

`make test-budget PKGS='./internal/docindex ./internal/standards'` at the tip `e26f85acf` reports
**two** failures in `internal/standards`, and neither is reachable from this diff:

- `TestDocClassBannerMatchesManifest` — `devdoc/removal-audit-shell-census.md` carries the
  `historical` banner while its manifest row reads `normative`. Measured at the BASE revision
  (`git show 4d792f644:…`) the pair is *already* `historical`-banner / `normative`-row, so the
  disagreement predates this ground; the path occurs **0** times in this diff.
- `TestCommandsDocGeneratedBlockMatchesSource` — `COMMANDS.md` line 24 points at
  `cmd/agent/cli_notify.go:573` while the source renders `:605`; the base revision's `COMMANDS.md`
  already carried `:573` and `cmd/agent/cli_notify.go` occurs **0** times in this diff (the source
  moved without regenerating its verb index, exactly the snapshot shape this report is about).

Neither is worked around, and neither is re-filed here. Evidence:
`git diff --name-only 4d792f644..HEAD` prints exactly two devdoc paths, so **a failure in a
non-devdoc gate cannot be caused by a devdoc-only diff**.
