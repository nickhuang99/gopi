<!-- doc-class: historical -->
# The abstention gets a name, a status, and a next step

Task: `01a136d7-2e20-7944-87c8-566de6f3414d` (branch `task/01a136d7-2e20-7944-87c8-566de6f3414d`).

## The defect, measured before anything changed

Two command faces ask the same question — *is the committed artifact still the
tree?* — and on a tree with **no readable history** both refused with the
**generic finding status**. The tree used is the one Rule 29 recommends for a
read-only comparison: a `git archive HEAD | tar -x` export, which carries the
shipped table and no repository.

```
$ git archive HEAD | tar -x -C $EXPORT        # no .git, the shipped table shipped with it
$ cd $EXPORT
$ go run internal/docclass/testdata/anchor-number-census.go . \
      internal/docclass/testdata/anchor-number-baseline.tsv --check
anchor-number-census: provenance probe: resolve 0b3c1de29: git rev-parse --verify --quiet
  0b3c1de29^{commit}: exit status 128 (stderr: fatal: not a git repository ...)
  ⏎ exit status 1                       ← the same status a real finding exits

$ gopi tool doccheck --repo $EXPORT
doccheck:   PROVENANCE NOT MEASURED (NOT checkable here …)
doccheck: this run exits 1 on provenance NOT MEASURED (no verdict about the
  permits was reached) (see the section above), independently of the 0 gap(s)
  ⏎ exit 1                              ← identical status, and no next step
```

Two facts with nothing in common shared one number:

| fact | what is true | what a caller must do |
|---|---|---|
| **drift** | the permits were judged; the table is not the tree | change the corpus |
| **abstention** | **no** permit was judged; the corpus may be perfect | supply the missing history |

And the abstention carried **no executable next step**. The verb's sentence said
"run this verb where that history is readable" — true and unusable, because it
does not say *how*, and on an export nothing does. The generator printed
nothing at all: it forwarded git's stderr and exited.

The advice the corpus *does* have for a corpus problem ("regenerate the table")
is actively wrong here, and that is measured rather than argued: the generator's
regeneration path runs the **same probe over the same tree through the same
judge**, so it fails identically. An operator following the corpus's own advice
on an export loops.

## What changed

**One vocabulary, one definition site** — `internal/docclass/docclass_exit_codes.go`
(Rules 1/13/14). Both faces read it, so they cannot answer one state two ways:

| status | name | meaning |
|---|---|---|
| `0` | `clean` | the artifact is the tree |
| `1` | `drift` | judged, and something needs work |
| `2` | — | usage (the generator's argv refusal) |
| `4` | `NOT MEASURED` | **no permit was judged** — history unreadable |

`4` and not `3`: `3` is already the agent-spawn guard's
(`config.AgentGuardRefusalExitCode`), a value a caller can observe from a gopi
process, so reusing it would make two unrelated refusals indistinguishable.

**Both channels carry the same name.** Each face prints a machine-readable
`verdict: <name>` line on **stdout** even when it fails, alongside the status.
The mapping is one function (`docclass.DocclassVerdictForClass`), and the
verbs' closing sentences take the **status as an argument** instead of typing
`exit 1` — the sentence and the status cannot disagree (Rule 15: words are a
claim). The two closing verdict strings in `cli_doccheck.go` became `%d`-carrying
formats for exactly this reason.

**The abstention gets an executable next step** — one sentence, shared
(`docclass.AnchorNumberNotMeasuredRepair`), printed by both faces under their own
prefix:

```
the permits' claims could not be judged in THIS tree — its history is not readable
here (no repository, or the commits the reasons name are absent). Regenerating the
artifact here cannot help: it runs this same probe over this same tree. Point
GIT_DIR at a repository that carries that history (GIT_DIR=<repo>/.git) and run the
same command again
```

`GIT_DIR` is the *measured* remedy, not a guess — see the evidence section. A
second sentence (`AnchorNumberHistoryEnvNote`) answers what the channel already
did: unset (omitted), resolves (named, so "the commits are not in *that*
repository" is actionable), or does not resolve (said plainly). A fallback that
fails silently is worse than no fallback, and this one is the step printed one
line above it.

**Nothing was weakened.** NOT MEASURED still fails: it is a non-zero status, as
before. It is a *different* failure, on both channels, which is the whole epic.

## Evidence — the surface, run for real (Rule 31)

Export at `~/.gopi/sessions/01a136d7-…/artifacts/export`, `HEAD` = `46f2ba714`.

**1. Abstention, both faces, distinct status + readable verdict.** Same command
that used to exit 1:

```
$ cd $EXPORT && /tmp/census-bin $EXPORT $EXPORT/internal/docclass/testdata/anchor-number-baseline.tsv --check
verdict: NOT MEASURED
  ⏎ CENSUS_RC=4
$ /tmp/gopi-bin tool doccheck --repo $EXPORT
doccheck: this run exits 4 on provenance NOT MEASURED (…) (see the section above), independently of the 0 gap(s)
verdict: NOT MEASURED
  ⏎ DOCCHECK_RC=4
```

Both faces print the *same* verdict name and exit the *same* status, where
before the generator exited 1 silently and the verb exited 1 in prose.

**2. The next step works — the measured remedy.** The printed step, executed
verbatim against this run's worktree:

```
$ cd $EXPORT && GIT_DIR=<worktree>/.git /tmp/census-bin $EXPORT …/anchor-number-baseline.tsv --check
anchor-number-census: scanned 252 anchor row(s), 167 incoherent; table carries 167 row(s)
anchor-number-census: table matches the tree — the freeze is exact
verdict: clean
  ⏎ CENSUS_BORROWED_RC=0

$ GIT_DIR=<worktree>/.git /tmp/gopi-bin tool doccheck --repo $EXPORT
doccheck: the devdoc corpus is consistent — every file under devdoc/ has a well-formed row
verdict: clean
  ⏎ DOCCHECK_BORROWED_RC=0
```

This is what makes the sentence a step rather than a wish: the export's corpus is
**coherent** (167 of 167 permits hold), and the borrow is how a reader gets that
verdict out of a history-less tree. It is also why the abstention is honest — the
corpus is fine, so calling it drift would have been a false accusation.

**3. Drift keeps the finding status (positive control).** A copy of the shipped
table with one permit deleted, same borrowed history:

```
$ /tmp/census-bin $EXPORT /tmp/baseline-drift.tsv --check
  HEADER FIGURE IS NOT THE MEASUREMENT: the header declares frozen=167 while this run re-derived frozen=166 …
  NOT PERMITTED (drifts, absent from the table): devdoc/IMMEDIATE-FEEDBACK-REGRESSION.md …
verdict: drift
  ⏎ CENSUS_DRIFT_RC=1
```

`1` and `4` are therefore two facts a script can tell apart, both reachable on
the same tree.

**4. Usage and clean paths.** `/tmp/census-bin` with no args → `⏎ 2`. The
worktree, history readable and table in step → `verdict: clean` `⏎ 0`.

**5. This tree's own tree is unchanged where it must be.** After the change, on
the worktree: `go run …anchor-number-census.go . …tsv --check` → `scanmed 252
anchor row(s), 167 incoherent; table carries 167 row(s)` / `verdict: clean` `⏎ 0`
— the anchor-line shift caused by new lines in `cli_doccheck.go` stayed *above*
the two published anchors (`:161`, and the manifest row), so **no regeneration
was needed** and the baseline table is byte-identical to `HEAD` (`git diff` on it
is empty, verified by `git checkout` then a clean re-check).

## Rework (case-④, task `01a1376e420d7b9c8ecf5f71e05eb3c6`)

This document was landed by a case-④ rework: its branch
(`task/01a136d7-2e20-7944-87c8-566de6f3414d`, tip `af0b86c0`) had its ref
**deleted** while the tip object stayed in the store, and master had moved
**13 commits** past the merge base `46f2ba714`. The work did not go through a
fresh re-implementation: the tip was merged for real
(`git merge --no-ff af0b86c0`, `rescue/docclass-abstention` tags the moved tip),
and the three conflicted files were resolved as a **union of both sides**.

The briefing predicted **one** conflict (`cmd/agent/cli_doccheck.go`); the real
re-probe found **three** — `COMMANDS.md` and `devdoc/DOC-INDEX.tsv` as well.
The numbers in the brief were stale, as it said they would be.

| file | resolution |
|---|---|
| `cmd/agent/cli_doccheck.go` | 4 hunks, hand-resolved: master's stored-cell leg and its constants block **and** the tip's exit vocabulary, verdict line and shared next step. Every deliberate drop carries an inline `MERGE NOTE`. |
| `COMMANDS.md` | generated block → the generator (`go run internal/standards/testdata/commands-census.go . COMMANDS.md`) owns it; the anchor now reads `cli_doccheck.go:381`, measured on the MERGED file. |
| `devdoc/DOC-INDEX.tsv` | union keyed on the row's PATH: master's five rows (its later re-derivation of the same rows wins) plus this document's one row — exactly one row for this path. |

An inline `MERGE NOTE` is carried by the three hand-resolved hunks of
`cmd/agent/cli_doccheck.go`, where a reviewer reads the diff and cannot infer the
decision. It is deliberately **not** written into `COMMANDS.md` or
`devdoc/DOC-INDEX.tsv`: both are GENERATED (their own headers name the generator
that owns them, Rule 36), so a comment there is reverted by the next regeneration
and, in the TSV's case, would break the row shape the gate parses. The decision
for those two is recorded here, which is the durable place for it — and the
resolution itself is machine-checked: the generator's `--check` exit 0 and the
manifest's `uniq -d` (no duplicate path) are the proof that the union is the union.

What the rework **adds** on top of this document as written, because the merged
tree has a third leg this document did not know about (master's
`docCheckReportStoredDrift`):

- the report arm's exit value is unchanged for every path (`0`, `1`, and the
  abstention's `4`); what changed is that each failure arm now also prints the
  `verdict:` line, so the status a script reads and the line a human reads cannot
  disagree;
- the abstention's repair sentence is the **shared** one
  (`docclass.AnchorNumberNotMeasuredRepair`), i.e. master's second, verb-bearing
  spelling of the same sentence was dropped deliberately as a duplicate channel —
  the behaviour (report, fail the run, name the missing capability) is kept
  intact, which is why the drop is a `MERGE NOTE` and not a silent edit;
- the third leg's two drift constants and the tip's abstention class **coexist**:
  they are different facts about different corpora ("nothing was compared" vs
  "the stored text is not what the classifier renders"), and both are reported.

**Carried claim corrected by re-measurement (Rule 36's reconciliation).** This
document's evidence section was measured on the tip's own tree
(`HEAD = 46f2ba714`). On the merged tree the shipped freeze table is
**EMPTY** (`frozen=0`; the numberless render, `anchor <path> at <text>`, owns the
shape), and the census's `--check` reports `verdict: clean` with `0 incoherent`
of `271` scanned rows. The document's `252`/`167` figures describe that older
table, not this one — they are the record of the run that produced the fix, and
they are left as measured.

## Not done (honest)

- **A gate on the new status is not added.** Nothing in the repo *consumes* the
  corpus's exit status today (`mergeset_docclass.go` names the generator, it does
  not read its status), so there is nothing to re-point; a new consumer would be
  new machinery (Rule 35). The statuses exist for the caller who arrives next.
- **The figures in `docs`/`COMMANDS.md` that quote the verb's exit convention**
  were checked by eye; `gopi tool doccheck` and the table gate are green, but I
  did not search every document for the string "exit 1" near a doccheck mention.
- **The fixture-based test needs the ambient repository** for its *measurable*
  arms (a pre-existing, documented limit in `docCheckPermitsFixture`); the two new
  tests deliberately use the *unmeasurable* shape, which needs no history.

## Files

- `internal/docclass/docclass_exit_codes.go` (new) — the vocabulary, the verdict
  names + line, the ONE class→verdict mapping, the shared repair sentence, and
  the channel note.
- `internal/docclass/testdata/anchor-number-census.go` — `os.Exit` at every seam
  now reads the shared constants; a new `abstention()` reports the state once for
  all three paths that reach it; `--check` prints `verdict:` on both arms; the
  usage document carries the status table.
- `cmd/agent/cli_doccheck.go` — the report and fix arms return the mapped status,
  print the `verdict:` line, take the status as a format argument, and print the
  shared next step plus the channel note; the usage names both statuses.
- `cmd/agent/cli_doccheck_test.go` — two new tests (distinguishable statuses on a
  history-less tree, positive control on a drifted one) and one text guard, plus
  `TestDocCheckUnmeasurableProvenanceIsNamedAndNotCalledFalse` updated to the
  abstention status.
