<!-- doc-class: historical -->
# master's doc-anchor red: 3 permit rows for the 3 landed-record drifts

Task 01a0e63ee-cd87-06f8-85a2-b4e076898c5, branch `task/01a0e63eecd8706f885a2b4e076898c5`,
measured 2026-09-28 on a tree whose tip is a delivery-point sync over
1407be38 / 44ce51c4.

## What was red

`TestDocAnchorsResolveInNormativeDocs` (internal/agent/docanchor_test.go) was RED on
master's own tree with THREE new drift entries, all kind=blank — the anchor points
at a line that is now EMPTY:

| doc | anchor key the gate named |
|---|---|
| devdoc/assertion-totality-gate-master-red.md | `cmd/agent/merge_run.go:939` |
| devdoc/cli-ui-feasibility-study.md | `internal/lineedit/editor.go:745` |
| devdoc/cli-ui-feasibility-study.md | `editor.go:745` (the bare-continuation spelling) |

Both docs are DATED LANDED RECORDS. Re-pointing their line numbers would present an
earlier measurement as today's — the exact falsification Rule 26's corollary and the
permit list's own header forbid. So the remedy is the one sibling commit 1f664ca0
applied to its own three drifts: record each key in the permit list with a per-row
reason that names the refactor which moved the symbol.

## What moved each anchor (measured, not inferred)

**`cmd/agent/merge_run.go:939`** — at the doc's own landing commit (387f9f29,
2026-09-22) line 939 was the **`func mintAdvancedPositionAtRunClose`** line (938 its
doc comment), and the doc's `:943` / `:947` continuations were its `if` guards —
i.e. the anchor resolved exactly. Line 939 is blank today, and the symbol is GONE
from the tree: `grep -rn '^func mintAdvancedPositionAtRunClose' cmd/ internal/` finds
nothing. The move is **ddbc3df1** (2026-09-24, "feat(version): the binary's identity is
its own, internal, constant, checkout-independent"), whose message says the merge-path
version-tag mint is deleted, not hidden — it leaves merge_run.go carrying 0 such
functions where its parent carried 1. The edit that *left :939 blank* is 1407be38
(2026-09-28, the `merge --every` / signalrec profile fix), confirmed by walking the
commits between the doc and HEAD and taking the first whose line 939 reads blank.
The permit list already carries the same event for this doc's audit fixtures
(`merge_run_test.go:845` / `:902`, "the anchor names the test master MOVED out of
cmd/…"), so the new row sits beside its siblings.

**`internal/lineedit/editor.go:745`** — the doc (e3e1035e, 2026-09-20) quotes the
rows-cap block it measured:

```go
rows := e.val.RowsFor(e.render.prompt, e.width())
if rows > editorRowsCap {   // editorRowsCap = 4  (region.go:57)
    rows = editorRowsCap
}
e.region.SetEditorRows(rows)
```

at 745-752, and on that tree the quoted `if rows > editorRowsCap` was exactly :749.
Two edits after it are the move: **882cb1ec** (2026-09-20, "derive the editing line's
row budget from the terminal height") replaced the constant `editorRowsCap` with
`e.region.EditorRowsCap()` and re-indented the block, and **65d7e278** (2026-09-21,
the small-height flake fix — a draw never overtakes queued input) is the edit that
left :745 blank (the block moved to :802 there, and to :1658 today). The block the
doc measured is ALIVE; only its line moved, which is precisely why re-pointing would
misattribute: it would claim the 2026-09-20 measurement was taken at :1658.

**The bare spelling `editor.go:745`** — the same doc cites the same site a SECOND
time, in its U1 prioritisation table (`| **U1** | … `region.go:57`, `editor.go:745-752` |`),
so the gate keys the one site twice: once resolved through the row's file token, once
through the basename. Same move, same remedy.

## The change

Three rows appended to internal/agent/testdata/docanchor-baseline.tsv (4 fields each,
tab-separated, key = doc|kind|detail), with a comment header naming this run. No doc
was re-keyed; no existing row was edited, reordered or reflowed — the diff is
10 insertions and 0 deletions (1 comment block + blank, 3 permits).

Per-entry reasons name the refactor and the date, and each is written so it cannot be
confused with a bare "it drifted": a reader sees WHICH commit moved the symbol, that
the symbol is deleted (merge_run) or alive elsewhere (editor), and why the anchor was
not re-pointed.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → exit 0
- Before (this worktree, pre-edit):
  `go test ./internal/agent/ -run TestDocAnchorsResolveInNormativeDocs -count=1` → FAIL,
  exited 1, with 3 `t.Errorf` lines at docanchor_test.go:203 naming the three keys, and
  the tally `236 pre-existing drift entries permitted, 3 new, 0 stale permits`
- After (the same command, same worktree):
  → `--- PASS: TestDocAnchorsResolveInNormativeDocs`, tally
  `239 pre-existing drift entries permitted, 0 new, 0 stale permits`
  (2661 anchors across 568 normative docs), and **0** lines at docanchor_test.go:203
- The three old drift lines are absent from the after-run and appear instead as
  `BASELINE DRIFT (<my reason>)` at docanchor_test.go:199 — i.e. permitted, not dropped.
- Hygiene guard: `TestDocAnchorBaselineHasNoDuplicateKeys` → PASS, "239 permit rows, no
  duplicate key, no duplicate line" (this is the regression guard for the duplicate-key
  defect 1f664ca0 fixed; a fourth copy of an existing key would have been rejected here)
- Whole gate family on the touched package: `go test ./internal/agent/ -run DocAnchor -count=1` → ok
- Corpus classification: `gopi doccheck` → "the devdoc corpus is consistent — every file
  under devdoc/ has a well-formed row"

## Not Done (honest)

- The two docs were **not** re-keyed to their symbols' current lines, and that is the
  deliverable's boundary, not an omission: both are dated landed records. The
  consequence is recorded, not hidden: the permit list now carries 239 rows and the
  merge_run anchor is permanently un-re-keyable by line (its symbol exists nowhere),
  so a future reader who wants that claim re-verified must re-take the measurement
  against a live guard, not against the row.
- The gate keys the editor.go site twice (explicit + bare). I added both, because the
  gate names both and a permit key that matches nothing is itself red. If the bare
  family and the explicit family are ever unified upstream, one of the two rows becomes
  a stale permit and its deletion is the correct repair — that is the ratchet working.
- No production code was changed, so nothing outside the gate's own test surface was
  executed; the surface changed here IS the permit list, and the gate that reads it was
  run for real before and after.

## Files

- internal/agent/testdata/docanchor-baseline.tsv (+3 permits, +comment header)
- devdoc/master-docanchor-permit-20260928.md (this report)
