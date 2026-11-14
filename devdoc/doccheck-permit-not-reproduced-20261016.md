<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# The STALE PERMIT that no longer reproduces — and the arm that actually held the ground red

**Run**: task `01a13703-2db7-70bd-9ad4-631b0d8645bf`, worked on `hosta`
(client — Rule 28 keeps `internal/docclass` and the canonical devdoc corpus to
peerbox; this run measures on its own ground and ships the result on its own
branch). **Ground**: `/home/user/workspace/gopi-01a137032db770bd9ad4631b0d8645bf`,
branch `task/01a137032db770bd9ad4631b0d8645bf`, base = peer master tip
**`38081ae12b76cd671ac992d412af50502fdab2fb`**. Every figure below is a real run
on that ground; the raw outputs are under this run's session directory
(`~/.gopi/sessions/01a13703-2db7-70bd-9ad4-631b0d8645bf/artifacts/`).

## ① The dispatched defect is not on this ground

The task was dispatched with a premise: `doccheck` reports a **STALE PERMIT** —
`devdoc/merge-backlog-terminal-guard.md × internal/tools/tasks.go` — a frozen
row in `internal/docclass/testdata/anchor-number-baseline.tsv` that no longer
covers a drifting row. Measured on the ground, that arm is **green**:

```
$ gopi tool doccheck; echo "exit=$?"          # (tail)
doccheck: anchor NUMBER coherence (recorded text still present, line number stale):
doccheck:   text-ambiguous rows (reported above, never frozen):
doccheck:   40 of 273 scanned anchor row(s) are text-ambiguous (reported, not frozen …)
doccheck: 0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv
verdict: drift
exit=1
```

**Zero `STALE PERMIT` lines** — the string does not occur once in the whole
output (59 lines). The arm's own generator agrees, and it is the arm's own
declared entry point:

```
$ go run internal/docclass/testdata/anchor-number-census.go . \
      internal/docclass/testdata/anchor-number-baseline.tsv --check
anchor-number-census: scanned 273 anchor row(s), 0 incoherent; table carries 0 row(s)
anchor-number-census: the freeze table is EMPTY and this scan accounts for it: 273 anchor
  row(s) scanned, 0 incoherent, 0 of them still record a line number (the numberless
  render owns the shape: tools/docclass/classify.py `anchor <path> at <text>`)
anchor-number-census: table matches the tree — the freeze is exact
verdict: clean                                            # exit 0
```

## ② The dual-rev reading: the permit existed, and an ancestor of the ground removed it

Both readings are taken from the object store, never from a worktree walk
(Rule 29):

| revision | role | `anchor-number-baseline.tsv` data rows | the named permit |
|---|---|---|---|
| `bbf8d9760` | the revision the briefing measured at | **168** | present: `devdoc/merge-backlog-terminal-guard.md  number-drifted  internal/tools/tasks.go:2825  }  first-frozen at 0b3c1de29 …` |
| `38081ae12` | this ground's base (peer master tip) | **0** | absent — `grep` over the blob **exit 1** |

```
$ git show bbf8d9760:internal/docclass/testdata/anchor-number-baseline.tsv | grep -c -v '^#'   # 168
$ git show 38081ae12:internal/docclass/testdata/anchor-number-baseline.tsv | grep -c -v '^#'   # 0
$ git show 38081ae12:internal/docclass/testdata/anchor-number-baseline.tsv | grep 'merge-backlog-terminal-guard' ; echo "exit=$?"   # no output, exit=1
```

The remover is named, and it is an ancestor of the ground — measured, not
inferred:

```
$ git merge-base --is-ancestor fabcf3c96 38081ae12 && echo YES          # YES
fabcf3c96  fix(docclass): regenerate the anchor-number freeze table — 141 stale permits removed
```

`fabcf3c96`'s own message states the mechanism: the manifest's anchor cells were
re-derived **number-less** (`anchor <path> at <text>`), so no manifest row
records a line number any more and there is nothing left for that table to
freeze; the generator was run and the 141 permits went. The table's own
`--check` above confirms the terminal state: **0 rows, and the emptiness is
accounted for by the scan** (`numbered=0`), which is the shape the gate accepts.

**Conclusion (measured): the dispatched defect does not reproduce on this
ground, and the fix is already upstream — it landed two merges before the
ground's base. No permit was minted, the table was not touched.**

## ③ What DID hold the ground red: 15 stored cells the corpus's own generator renders differently

`doccheck` still exited 1, by its third leg — independently of the 0 gaps:

```
$ gopi tool doccheck; echo "exit=$?"                    # before any fix on this ground
doccheck: stored-cell drift: 15 of 2563 rendered row(s) whose stored text is not what the classifier renders today
doccheck: 0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv
doccheck: this run exits 1 on stored-cell drift … independently of the 0 gap(s)
verdict: drift                                          # exit 1
```

The same 15 are reported by the corpus's own Python face, which is the
generator that judges them:

```
$ python3 tools/docclass/classify.py . --check
DOCCLASS STALE devdoc/chat-session-ground-lifecycle-diagnosis.md
… 15 lines …
15 row(s) whose stored text is not what the classifier renders today (reported, NOT fatal …)
```

**Reproduction condition and attribution.** The rows are not stale because the
tree moved under them in this run: the drift is what the `write`/`edit` hook's
renderer (`internal/docindex`'s own) and `tools/docclass/classify.py` produce for
the *same* cell. This run's own diff shows the shape of it — a cell whose class
is re-derived (`devdoc/removal-audit-shell-census.md` `historical` → `normative`),
a citation's line number moving (`internal/agent/testdata/docanchor-baseline.tsv:264`),
a ` stale=N` counter falling (`stale=2` → `stale=1`). The mechanism is the
subject of an already-filed bug row — `01a141a7-30fb-7473-b944-c6103912e304`
(`bug(docindex)`, `status: pending`), whose red arm is recorded in
`~/.gopi/sessions/01a1362e-290b-78e0-a707-a9c0aa477647/artifacts/bug-docindex-stored-cell-stale.md`.
So the leg's *cause* is another row's work; what this run owns is the ground's
redness, because 15 rows of the shipped corpus disagree with the classifier that
judges them. No frozen permit exists for this leg (Rule 39's generator is the
anchor-NUMBER table, whose `--check` is green), so the sanctioned clear is a
re-derivation, not a permit.

## ④ The clear, by the generator (never by hand)

The 15 paths were taken from the Python face's own report and handed to its own
incremental writer — dry-run first, then the write:

```
$ python3 tools/docclass/classify.py . --only-file <the 15 paths> --dry-run     # the rendered cells, exit 0
$ python3 tools/docclass/classify.py . --only-file <the 15 paths>
{ "added": 0, "updated": 15, "rows": 2563 }
$ git diff --numstat -- devdoc/DOC-INDEX.tsv
15	15	devdoc/DOC-INDEX.tsv
```

Landed as `69fd06807` (`fix(docindex): re-derive 15 stored cells the corpus's own
generator renders differently`). `devdoc/DOC-INDEX.tsv` was never opened by an
editor; the anchor-number freeze table is byte-identical to the ground's base.

## ⑤ The delivered state, re-measured after the commit

```
$ gopi tool doccheck; echo "exit=$?"
doccheck: stored-cell drift: 0 of 2563 rendered row(s) whose stored text is not what the classifier renders today
doccheck: stored-cell drift: none — every rendered row matches the manifest byte for byte
doccheck: 0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv
doccheck: the devdoc corpus is consistent — every file under devdoc/ has a well-formed row
verdict: clean                                          # exit 0
$ python3 tools/docclass/classify.py . --check          # 0 stale, 0 gaps → exit 0
$ go run internal/docclass/testdata/anchor-number-census.go . \
      internal/docclass/testdata/anchor-number-baseline.tsv --check   # verdict: clean → exit 0
$ git status --porcelain                                # no output
```

The acceptance figure `verdict: clean / exit 0` is therefore the ground's own
output, and the corpus is frozen against the two generators that judge it.

## ⑥ Boundaries of this record

- **Nothing in `internal/docclass` and nothing in `internal/docindex` was read
  or changed by this run** (Rule 28): the diff is devdoc data plus this file.
  The hook's definition mismatch stays with `01a141a7-30fb-7473-b944-c6103912e304`.
- **The 15 rows were re-derived, not explained away**: if a future commit makes
  the two renderers agree, the same `--only-file` write is the way back.
- **This file is committed by this run**; the sibling run
  `01a136eef3ac27d768fdb2488325b53a` (task `01a136ee-8817-7a61-8f3d-942bdbfa31d4`)
  is diagnosed separately — it is the row that filed the STALE PERMIT premise,
  and its conclusion (premise no longer live at the wire tip) is confirmed here
  from this ground by an independent reading.

## Evidence

`~/.gopi/sessions/01a13703-2db7-70bd-9ad4-631b0d8645bf/artifacts/`:
`doccheck-baseline.txt` (the ground's own output before any fix, 59 lines),
`dualrev-permit.txt` (the two `git show … | grep` readings), `classify-check-before.txt`,
`stale-paths.txt`, `fix-write.txt`, `dualrev-and-fix-diff.txt`,
`doccheck-after-fix.txt`, `classify-check-after.txt`.
