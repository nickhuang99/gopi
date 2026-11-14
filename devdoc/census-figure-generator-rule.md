<!-- doc-class: normative -->
# Rule 38 — a census figure names its generator and its commit (the boundary, the gate, the evidence)

**Task**: `01a0e67f-ca6f-73aa-9aa0-a5530decc115`. **Branch**:
`task/01a0e67f-ca6f-73aa-9aa0-a5530decc115`. **Deliverable**: PROJECT.md Rule 38 +
`internal/standards/census_evidence_gate.go` (+ `_test.go`).

## 1. The boundary: census claim vs narrative record

A figure's class is decided by its **scope**, not by its size or its look.

**CENSUS CLAIM** — counts the project AS A WHOLE and a reader ACTS on it; must
name the generator that prints it and the commit it was measured at. Real
examples from this repo:

| Example | The figure | Why it is a census |
|---|---|---|
| `devdoc/gopi-statistics-20260927.md` §1 | `538 files / 248,423 LOC` production Go | a whole-tree count, re-derivable by `scripts/gopi-stats.sh` at its base commit |
| `devdoc/gopi-statistics-20260920.md` §1 | `Tracked files 4,060` | master's own file count — the row the epic measured going stale (1,197 → 1,220) |
| `devdoc/gopi-verb-census.md` | `35 verbs dispatched, 30 of them driven by a test` | the WHOLE dispatch chain, re-derived by `scripts/g7-verb-census.sh ./bin/gopi` |
| `devdoc/gopi-tools-maintenance-help-surface-report.md` | `123 advertised forms across 16 keys` | the binary's whole advertised surface, printed by `scripts/g6-tools-maintenance-help-surface.sh` |
| PROJECT.md Rule 3 / Rule 11 | `GoPi has 17 tools`, `git_blame 0 次/21435` | hand-typed figures about the whole project — the Rule 11 row was already false when it was quoted as current |

**NARRATIVE RECORD** — evidence of ONE run; stays in prose, because "fixing" it
destroys the record it is, and no generator could reproduce it:

| Example | The figure | Why it stays prose |
|---|---|---|
| `devdoc/merger-ghost-ref.md` | `~/.gopi/logs/merger.log:27630` | a line in one log file, at one moment |
| `devdoc/removal-gate-window-and-inventory.md` | the `41`-line window (`:1675` vs `:1716`) | a measurement of one file at one commit; the diff IS the evidence |
| `devdoc/fixture-child-lifetime-binding.md` | `pid 1503983`, `11h+` | a process that existed once |
| `devdoc/worktree-cwd-release-inventory-verified.md` | `13` removal arms over 94 files | a hand census of one package's call sites |
| `devdoc/ledger-snapshot-ship.md` | `2057 tasks`, the two differing checksums | an incident's own transcript of a live database |

The mechanical consequence, and the reason the gate is cheap: a narrative figure
inside a code span (a task id, a log offset, a path) is not read as a quantity,
and a figure with no project dimension near it is not read as project-wide. A
census figure has a dimension (`tracked files`, `commits`, `packages`, …) beside
it by construction.

## 2. The rule

GOPI.md **Rule 38** (three sentences of practice, the four properties of
`scripts/gopi-stats.sh` as its test, and the gate as its check): a census figure
must name the generator that prints it and the commit it was measured at, or
carry `census-evidence-ok: <reason>`.

The generator's four properties, all observable in `scripts/gopi-stats.sh`:
NAMES a base commit (`base commit: $BASE_COMMIT`); PRINTS one number per
dimension (no arithmetic typed in prose); RE-RUNS at any rev out of the OBJECT
STORE (`git grep -c ''` at both revs — never a second checkout, Rule 29);
READ-ONLY over the repo, warning rather than failing on a dirty worktree.

## 3. The gate

`internal/standards/census_evidence_gate.go` judges `devdoc/**.md` through the
SAME corpus predicate the truth-class gate uses (`internal/devdoc`'s `Root`,
`MarkdownExt` and `IsProse`; no second manifest, no second corpus vocabulary). A
document is JUDGED when

1. its **title** (first 40 lines' H1) carries a census cue — `census|statistic|
   inventor|snapshot` (any suffix, matched as a WORD) — AND a project-scope cue —
   `project(s)|repositor(y|ies)|repo(s)|codebase|entire|whole|every`; and
2. its **body** carries a project-wide figure: a two-digit-or-longer number, a
   comma-grouped number, a four-or-more-digit number, or a percentage, on a line
   that also names a project dimension within `censusQuantityWindow` (24)
   characters, outside a code span, and not a bare year (1900–2099).

A judged document must show a GENERATOR (`scripts/*.sh|py` or the word
"generator") AND a PIN — a 7–40 hex token that RESOLVES in this repository via
`git cat-file -e <tok>^{commit}` — or carry `census-evidence-ok: <reason>`
(reason ≥ 24 chars; escapes are printed and the escape count is ratcheted at its
measured value, `censusMaxEscapedDocs = 0`).

**Scope, honestly**: title-declared project censuses under `devdoc/` only. A
census figure inside an ordinary report is NOT caught; `tasks/`, `bugs/` and
`briefings/` are excluded (per-task records and instructions, not standing
claims); the figure window under-reports rather than guesses. The evidence check
is **document-level, not figure-level**: the generator may appear anywhere in the
body, and the pin must be a resolving commit on a `commit`-ish line anywhere in
the body — so a document that still names *some* generator and *some* resolving
commit passes, even when an individual figure came from elsewhere. That limit was
measured, not assumed: weakening the 09-27 snapshot by deleting its two headline
lines (`**Base commit measured:**`, `**Generator:**`) did **not** turn the gate
red, because the document names `scripts/gopi-stats.sh` and a resolving commit
elsewhere in its body. What the gate does refuse is the document with no
re-derivation at all — which is the failure mode the class was created for.

Two further calibration findings are recorded in the source: substring cue
matching made `repo` match "Report" (so every "Task Report: …" title looked like
a census — fixed by word-boundary matching), and the first cue list's
`full`/`top-level` dragged two generator-less reports in; the measured corpus
settled both, and the gate's test pins the judged population at exactly the three
named documents.

## 4. Evidence (self-filed)

Positive controls — the three documents the epic names, judged and passing:

```
census gate over 1499 markdown documents: 3 judged
        pass     devdoc/gopi-statistics-20260920.md — generator scripts/gopi-stats.sh, pin 7e34258976e2f768f87df9b30b32f559bf9b6205
        pass     devdoc/gopi-statistics-20260927.md — generator scripts/gopi-stats.sh, pin 574505bf6cd4dece18668d162b405455e30c11f2
        pass     devdoc/gopi-verb-census.md — generator scripts/g7-verb-census.sh, pin 216d3d41
```

RED proof (the same run, three independent weakenings — pin, generator, escape):

```
RED (pin):       RED      devdoc/census-fixture.md — figure 5,258, generator scripts/gopi-stats.sh, pin MISSING
                 (restore the fixture's own HEAD → GREEN, pin f3960101…)
RED (generator): RED      devdoc/census-fixture.md — figure 5,258, generator MISSING, pin MISSING
                 (add `scripts/gopi-stats.sh` + a base-commit line → GREEN)
escape:          no escape → RED · stub reason (`hist`) → RED · stated reason → escaped (recorded, not judged)
```

The whole-corpus sweep (a synthetic census doc dropped into the real corpus and
then removed) is pasted in the task report; the gate refuses it and the three
controls stay green in the same run.

## 5. Verification

Full `Build:`/`Tests:`/`Ran:`/`Result:` block lives in the task report at
`$GOPI_TASK_REPORT_PATH` (Rule 5). The gate: `go build ./...` OK, `go vet ./...`
OK, `go test ./internal/standards/ -run TestCensusGate -count=1 -v` PASS,
`gofmt -l` empty.

> **Correction (2026-10-22, restoration).** This document was written against the
> ORIGINAL gate, which commit `29882b3` (the document-pipeline slice) deleted
> whole — it was the package's only importer of `internal/docclass`, which that
> slice removed. The gate named in PROJECT.md Rule 38 therefore matched no test
> and exited 0 for a month. It is restored on `internal/devdoc`, with the same
> predicate and the same escape ratchet; the numbers in §4 above are the
> ORIGINAL run's narrative record (1499 documents walked) and are kept as such —
> Rule 38's own boundary: a past run's figure is evidence of that run, not a
> census of the tree. The restored gate's measured run is in the task report, and
> `gopi doccheck` is **not** part of the verification: that verb is not
> registered (`cmd/agent/cli_doccheck.go` does not exist).
