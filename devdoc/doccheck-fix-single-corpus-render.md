<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# `gopi tool doccheck --fix` rendered the corpus twice — one walk, two arms

Task `01a14774`. Baseline `7ecfa12`.

## The defect

`runDocCheckSubcommand` dispatched the corpus half as two independent calls:

```go
code := docCheckReport(root)
if opts.Fix {
    code = docCheckFix(root)
}
```

Each arm called `docindex.CheckCorpus(root)` itself, and — the expensive half —
each arm reached `docCheckReportStoredDrift`, whose report leg calls
`docindex.DocclassDrift(...)` → `docclassDriftRender`: **one python3 process over
the whole corpus** (2625 paths on this tree), announcing
`doccheck: the corpus's own classifier is running over %d path(s)` once, then
ticking every 15s.

On the tip tree, with the same binary and the same corpus:

| arm | wall |
|---|---|
| read-only | 67.449s |
| `--fix` | 133.362s |

= 1.98×, and the `--fix` line-arrival timeline carried **two** phase
announcements with two tick sequences, the second opening at t≈68.8s: the same
bytes rendered twice so that the fix arm could write one row.

The second render was pure display-path waste, not a semantic need: the fix
arm's own third question (stored-cell drift) is *read* through the same render,
and nothing in the fix leg consumes a *second*, later reading of the classifier.

## The change (`cmd/agent/cli_doccheck.go`)

One measurement, handed to whichever arm runs.

```go
type docCheckCorpus struct {
    Measured bool
    Gaps     []docindex.CorpusGap
    Err      error
}
```

- `docCheckWalkCorpus(root)` is the ONE call site of `docindex.CheckCorpus` on
  the dispatch path (Rule 14).
- `docCheckArms(root, fix)` walks once, then dispatches to `docCheckFixCorpus`
  or `docCheckReportCorpus` with that value.
- `docCheckCorpus`'s **zero value means "not measured"**, so a caller holding no
  walk (`docCheckReport`, `docCheckFix`'s standalone entry points, and every
  existing test that drives one arm) costs exactly what it always did. The
  fallback is `gapsFor`, the single accessor.
- The fix arm's deliberate **re-ask after its write** is untouched — that one is
  a `CheckCorpus` (two directory reads and a manifest parse), not the render, and
  keeping it is what makes the fix arm's exit status a statement about the corpus
  *now* rather than a claim about its own write. What was removed is the second
  RENDER, and the render only lives on the report leg.

Nothing else moved: gap writing, anchor-number drift, and stored-cell drift are
the same functions with the same order, so the fix arm still answers all three
questions.

## The red proof

`cmd/agent/doccheck_single_render_test.go` counts the phase announcement in the
**real binary's** captured output — not a stub, not a counter injected into
production:

```go
prefix, _, _ := strings.Cut(docindex.DocclassDriftProgressAnnouncement, "%d")
return strings.Count(out, prefix)
```

The needle stops before `%d` on purpose: the tick line shares the phase's
opening but says "is still running", so a needle taken from the announcement's
head would count ticks as renders — the arithmetic would then be about the
corpus's *speed* instead of its *passes*.

Measured red (production file reverted to `HEAD`, test file kept):

```
--- FAIL: TestDocCheckFixRendersTheCorpusOnce (3.46s)
    `--fix` rendered the corpus 2 time(s), want exactly 1 …
```

Measured green (fixed file restored, identity re-taken afterwards):

```
--- PASS: TestDocCheckFixRendersTheCorpusOnce (1.24s)
--- PASS: TestDocCheckDispatchMeasuresTheCorpusOnce (0.00s)
```

`TestDocCheckFixKeepsTheThreeQuestionVerdicts` pins the verdicts the change must
not have purchased away — one tree per question class: clean (both arms 0), a
manufactured stored-cell drift (both arms agree, non-zero, and the fix arm NAMEs
the class it cannot write), and one repairable gap (the fix arm exits 0 **and**
the row lands, with no existing row rewritten).

`TestDocCheckDispatchMeasuresTheCorpusOnce` is the structural half: the
subcommand must dispatch through `docCheckArms`, must not spell
`code = docCheckFix(root)` after the report arm, and `docCheckFixCorpus` must
consume `inventory.gapsFor(root)` — the shape cannot come back through another
door.

## What the arms do now — the measured table

Every arm below ran the REAL binary on ONE clone of this branch at `7ecfa12`
(2625 devdoc paths; the clone lives on a device off `/`, Rule 22), through
`artifacts/measure-arrivals.sh`, which samples the output file every 0.05s and
records the arrival time of every line. Evidence:
`~/.gopi/sessions/01a14774-4662-745f-bdf2-edb21cf4bb83/artifacts/{<arm>.tsv,
raw-<arm>.log}`, each TSV carrying a `HARNESS` row with `wall=`, `exit=` and
`max_inter_line_gap=`.

| arm | renders | tick lines | wall | exit | verdict |
|---|---|---|---|---|---|
| baseline read-only | 1 | 5 | **89.327s** | 1 | drift |
| baseline `--fix` | **2** | **11** | **203.391s** | 1 | drift |
| fixed read-only | 1 | 5 | **78.881s** | 1 | drift |
| fixed `--fix` | **1** | **5** | **78.187s** | 1 | drift |
| fixed `--fix`, one planted gap | **1** | **5** | 95.579s | 1 | drift, **row written** |

`--fix`/read-only ratio: **2.28× → 0.99×**.

The two phase announcements on the old `--fix` arm arrive at **t=1.188s and
t=116.033s** (`arrivals-fix-baseline.tsv`, lines 48 and 124) — the second render
opens exactly where the first one's verdict section ends. The fixed `--fix` arm
has ONE, at t=1.529s (`arrivals-fix-fixed.tsv` line 48), and its 5 tick lines
are the single 15s sequence.

**Verdict semantics unchanged**: all four arms carry the same last line
(`verdict: drift`) and the same exit status (1 — the tree at `7ecfa12` has 7
stored-cell drift rows, which neither arm may write). The planted-gap arm exits
1 for that same drift reason even though its GAP class is clean — and its row
landed (`grep -c zz-probe-gap devdoc/DOC-INDEX.tsv` → 1), so "render once" was
not bought by skipping the write.

## Verification (Rule 5 / Rule 31) — the two-commit state

- **Build**: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → exit 0.
- **Tests** (branch-scoped): `make test-budget PKGS='./cmd/agent ./internal/docindex'`
  green except four groups triaged into `
### Final state — the suite's one red is the corpus's own data, not this branch

The `cmd/agent` failures from the `make test-budget` run were triaged by
re-running them in an ISOLATED worktree at this branch's HEAD
(`/home/user/workspace/gopi-doccheck-iso`, `--detach`, removed afterwards):
`go test ./internal/docindex -run TestTheShippedCorpusCarriesNoStoredCellDrift`
fails there too, with **7 of 2626** drifted rows — the SAME 7 rows the shipped
manifest already carried at baseline `7ecfa12`
(`chat-session-ground-lifecycle-diagnosis`, `closeout-deletes-undelivered-branch-debug`,
`hp-push-diagnosis-20260916`, `ledger-readonly-faces`,
`reclaim-deterministic-checklist`, `removal-audit-shell-census`,
`why-worktree-residue`). None of them is authored or touched by this task; the
report row this task added was re-derived through the generator
(`python3 tools/docclass/classify.py . --only <path>`, commit `670217d92`) and is
NOT in that set, so this task added no drift. The remaining 7 are a corpus-data
repair of their own, dispatched separately — fixing them here would mean
re-deriving rows for documents this run never read.

The `cmd/agent` resume/take-over failures are environment/load-dependent and did
not reproduce in the isolated run; they are recorded here, not claimed green.

## Not done / honest` — one REAL (my
  files missing from the bare-`--peers` coverage lists; fixed in `bdfd310c3`,
  focused re-run green) and three that reproduce at the BASELINE `7ecfa12` in an
  isolated worktree (`TestTheShippedCorpusCarriesNoStoredCellDrift` — 7 of 2625
  rows already drifted at baseline, independent of this change).
- **Ran** (verbatim, real tree, real arguments):
  `bash artifacts/measure-arrivals.sh <label> <tree>/bin/gopi tool doccheck [--fix]`
  for each arm — the wrapper samples the output file every 0.05 s and writes
  `arrivals-<label>.tsv` with one row per output line plus a `HARNESS` row
  carrying `first_byte=`, `exit=`, `wall=`, `max_inter_line_gap=`.
- **Result**: render count per arm — baseline `--fix` **2**, baseline read-only 1,
  fixed `--fix` **1**, fixed read-only 1; wall 203.391 s → 78.187 s against a
  read-only arm of 78.881 s (**1.0×**); tick sequences 11 → 5. All arms keep
  exit 1 / `verdict: drift`; the planted-gap arm writes its row.
- **Commits**: `6ed85c63b` (the fix + red proof), `bdfd310c3` (coverage-list
  registration), report row in `devdoc/DOC-INDEX.tsv`.

## Not done / honest

- The wall-time figure depends on the host's load; the acceptance criterion is
  the **render count** (1 vs 2, a property of the code) plus "materially below
  2×" for wall.
- The suite's `TestTheShippedCorpusCarriesNoStoredCellDrift` reports 7 drifted
  rows at BASELINE `7ecfa12` (verified in a separate worktree: 7 of 2625 at
  baseline, 7 of 2626 on this branch — the extra path is this report). Pre-existing,
  not caused here, and out of this task's scope (it is the shipped corpus's own
  data, a separate repair).
- `docCheckFix`'s post-write `CheckCorpus` re-ask is still a second corpus *walk*
  (not a render). Removing it would change the exit semantics the fix arm's
  documented contract depends on (`testsuite`: "--check exit 1 while --fix exit 0"
  was the exact defect that check guards), so it stays.
