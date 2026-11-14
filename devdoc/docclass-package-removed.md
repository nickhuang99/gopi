<!-- doc-class: historical -->
# `internal/docclass` removed — what survived, what deliberately did not

Task `01a16043-2b38` (slice 3/5 of the document-pipeline teardown). The deleted
package had grown to 45 files / ~16k lines, most of it gates policing a corpus
(`devdoc/DOC-INDEX.tsv` + per-file class banners) rather than anything a running
gopi needs. This page is the account of the removal, so a later reader does not
mistake the survivors for an oversight.

## What was deleted

- **The package itself** — `internal/docclass/` in full: anchor-number provenance
  and baseline machinery, prose path/line scanners, banner-body scanners, the
  evidence tiers, the exit-code table, the manifest writer, and the ~25 test files
  that held the gates together.
- **Gates whose only subject was that corpus**: `cmd/agent/merge_union_regen*.go`,
  `mergeset_docclass*`, `merge_target_doc_gate*`, `merge_config_anchor_gate*` (this
  last one is the item below that outlived its accounting — read the correction that
  follows this list before running anything on this page),
  `internal/tools/task_finish_docclass*`, `internal/agent/docanchor_test.go` plus
  its 878-line `testdata/docanchor-baseline.tsv`.
- **The two scripts that exercised it**: `scripts/docclass-red-proofs.sh`,
  `scripts/test_docclass_scope.py`.

**One of those deletions outlived its own accounting, and this is the correction.**
`merge_config_anchor_gate*` was NOT purely a corpus gate: its subject — the
`path.go:NNN` anchors `CONFIG.md` publishes — is not a `devdoc/` manifest, and the
census table `internal/standards/testdata/config-anchor-census.tsv` (with its
generator and `internal/configanchor`) survived the slice intact. So the tree kept a
re-derivable census and lost every check that drove it, while a dozen devdocs went on
citing `go test ./internal/tools/ -run TestMergeConfigAnchorGate` as acceptance — a
selector that matched nothing and printed `[no tests to run]` at exit 0. The arms **did**
come back under those documented names and drove the live generator
(`config-anchor-census --check`) rather than a merge-time lane — and **all of it was then
deleted** (2026-11-05): `78bdf05a` (operator ruling) removed the documentation-citation
family (`config-anchor-census.go`, its `.tsv`, `citation-census.go`,
`citation-selector-census.go`, the gates that drove them) and `b8cad761` removed
`internal/tools/merge_config_anchor_gate_test.go` with the last census arm. So a merge-time
CONFIG-ANCHOR signal does not exist, is not planned here, and neither does an offline one:
`go run` on the generator prints `stat … no such file or directory` and `-run
TestMergeConfigAnchorGate` prints `[no tests to run]` at exit 0. The anchor rules are prose.

## What survived, and why — `internal/devdoc`

A stdlib-only leaf package (`internal/devdoc`) now carries exactly what LIVE paths
still ask for: the manifest name/path constants, the row parser, the closed class
vocabulary, the line-1 banner grammar and a first-line reader. It holds no cli,
no ledger, no git — so a consumer importing it cannot drag a gate in behind it.

Live consumers re-pointed to it: `cmd/agent/merge_union.go`,
`merge_union_rows.go`, `merge_preland_gate.go`, `internal/tools/manifest_union_gate.go`,
`internal/tools/header_hook.go`, `internal/agent/chat_write_boundary.go`, and the
two help-surface inventory tests.

## The narrowing that is on purpose

`mergeUnionCheckRows` (in `cmd/agent/merge_union_rows.go`) now asks ONE question:
**does the doc a normative manifest row names exist in the merged tree?** The three
evidence tiers the deleted gate enforced (symlinked path, anchor resolution, cite
presence) are gone, and the file header says so — a merge can now land a normative
row whose spot-check column was never verified by the merger. That is the slice's
intent: the merge path checks that a manifest row is not dangling, not that the
document is well-formed.

`internal/tools/manifest_union_gate.go` keeps its MANIFEST-DUP check unchanged in
behaviour (one row per path key in the merged manifest) — it was already the part
that mattered.

## Verification performed

`gopi task merge` was run against throwaway repos under a scratch root (device
`66305` vs `/` on `66308`, Rule 22), with a real settings document naming the repo:

- repo with `devdoc/DOC-INDEX.tsv` + two branches each appending a row →
  `MANIFEST-DUP gate PASS for feat/x (the union … writes every devdoc/DOC-INDEX.tsv
  row key once)`; after `feat/x` merged, `feat/y` reported
  `MANIFEST-DUP gate ABSTAINS for feat/y (merge-file failed (1))` because the
  manifest now conflicts textually — the resolver lane owns it, unchanged by this
  slice.
- repo with no manifest → `MANIFEST-DUP gate ABSTAINS … (no manifest at
  devdoc/DOC-INDEX.tsv on all three sides)`, merge proceeds clean.
- `gopi tool doccheck` is gone: the verb list refuses it by name.
- `gopi task merge -h`, `gopi session -h` print.

## Bookkeeping carried by the same change

The repo-hygiene permit rows for the deleted files were dropped from
`internal/standards/testdata/repo-hygiene-baseline.tsv` (a permit that no longer
fires is an error, Rule 39), the testbudget exemption for the removed package was
dropped, `PROJECT.md`'s Rule 36 section was re-pointed at the header convention
that survived, and the rework briefing's test instruction now names
`-run TestManifestUnionGate`.

## Known-broken, not from this slice

`go test ./internal/tools/` and `./internal/testutil/` carry 4 + 2 pre-existing
reds (`toolversion`, `mcp*`, `scratch` arms) measured identically at base
`718244b`; they are filed as bug rows `01a161f3-67e5` and `01a161f9-0b27` rather
than repaired here.
