<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# The derived manifest merges by UNION — and lag is counted, not gated

## The problem

`devdoc/DOC-INDEX.tsv` is a **derived** file: one row per file under `devdoc/`,
rendered by `tools/docclass/classify.py`. Two branches that each add a document
each hold one row the other has never seen. There is no disagreement *about* the
file — but git's three-way merge calls two insertions at the end of the same file
a content conflict, and it does so on a file **nobody authored**. Measured on a
2026-10-19 fixture clone of this repo (base `d61db336f`):

```text
$ git merge --no-edit side-a
CONFLICT (content): Merge conflict in devdoc/DOC-INDEX.tsv
CONFLICTED: [devdoc/DOC-INDEX.tsv ]
```

A conflict on a derived file is worse than an ordinary one: the resolution is
usually "keep both", which is *correct* here only by luck, and "keep one side"
merges **cleanly while losing a row** — a tree that says one thing and a manifest
that says another, with no conflict marker anywhere to notice.

## The mechanism (chosen by measurement, not by preference)

`.gitattributes`, repo root, ONE path:

```text
devdoc/DOC-INDEX.tsv merge=union
```

Same fixture, same merge, with the attribute:

```text
$ git check-attr merge -- devdoc/DOC-INDEX.tsv
devdoc/DOC-INDEX.tsv: merge: union
$ git merge --no-edit side-a
 devdoc/zz-fixture-alpha.md | 3 +++
 2 files changed, 4 insertions(+)
CONFLICTED: []
ROWS: 2                     # both fixtures' rows
MARKERS: 0                  # no <<<<<<< anywhere in the manifest
```

**Why a merge attribute and not a re-derivation hook.** The epic's constraint is
that the *system layer* (the merger) is out of scope on this host (Rule 28), so
the mechanism had to be one the branches themselves carry. An attribute travels
inside the branch, is version-controlled with the file it describes, and changes
nothing about how anything *runs* — no daemon, no cron, no state file (Rule 35).

**Why `union` is safe HERE and nowhere else.** Union concatenates conflicting
hunks. That is correct exactly where the file is a **set of independent lines**
whose order carries no meaning — this manifest already holds rows appended out of
sequence (one inversion at `4a1b0c0`), and its writer splices a new row at the
sort position without moving any existing line. Applied to code it would join two
edits of one function into a result no reviewer can read, so the attribute is
spelled for ONE path, never a pattern, and
`internal/standards/docindex_union_merge_gate_test.go` fails the build if it ever
reaches another path (it asks `git check-attr` — what git RESOLVES — for the
manifest and for a `*.go` file).

## What a union merge does NOT do: converge

Conflict-free is not converged. After a union merge the result can carry

- a **duplicated path** (both sides re-derived the same row), or
- a **dropped line** (a resolution that kept one side).

Neither is a conflict, so nothing else names them. Three shapes are therefore
measured by `internal/docindex.MeasureBehind` — `missing-row`, `stale-row`,
`duplicate-row` — and printed by `gopi tool doccheck` on **every** run:

```text
doccheck: 0 row(s) behind — a re-derivation would change that many line(s) of
devdoc/DOC-INDEX.tsv (missing 0, stale 0, duplicated 0); this is REPORTED, not
fatal — it is not a verdict and it changes no exit status. Clear it with
`python3 tools/docclass/classify.py <repo>` (or the per-path form
`python3 tools/docclass/classify.py <repo> --only <path>`)
```

**It votes on nothing, by construction**: `docCheckReportBehind` in
`cmd/agent/cli_doccheck.go` returns **no value**, so no exit status can depend on
it; the report arm's code is still the OR of the gap / anchor / stored-cell legs.
The epic's two requirements — *reported* and *does not block* — hold at once
because the counter lives in a channel that does not vote. Every shape it counts
is still **fatal** in the GAP arm and in `internal/standards`: nothing was
weakened to make this line non-blocking.

**The one command that clears it** is the writer with no flags — the only shape
that is idempotent by construction (carry existing rows byte-for-byte, classify a
path with no row, drop a row whose file is gone, converge a duplicate):

```bash
python3 tools/docclass/classify.py .        # whole-corpus re-derivation
```

Measured: two consecutive runs on one clean tree produce **identical bytes**
(`sha256 dc4c4fcb…` both times, 28s each, `git diff` empty after both) — see the
report's Verification.

## Where the pieces live

| Piece | File |
|---|---|
| the merge attribute | `.gitattributes` |
| lag measurement + sentence | `internal/docindex/docindex_behind.go` |
| the verb's non-voting arm | `cmd/agent/cli_doccheck.go` (`docCheckReportBehind`) |
| the writer (unchanged, one implementation) | `tools/docclass/classify.py` |
| the attribute gate | `internal/standards/docindex_union_merge_gate_test.go` |
| lag proofs (3 shapes + convergence + abstention) | `internal/docindex/docindex_behind_test.go` |
| process-level proofs (prints, does not vote) | `cmd/agent/doccheck_lag_report_test.go` |

Rule 36 is untouched: a new devdoc is still classified by the writer, and the lag
counter is what tells a reader when a **merge** (not a write) left the manifest
behind its tree.
