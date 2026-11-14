<!-- doc-class: normative -->
# Task Report: the merge-time duplicate-manifest-key gate

## Completed

- **`internal/tools/manifest_union_gate.go`** — a new merge-time check (⓪d) beside
  the merger's existing three (contract ⓪/⓪a, union-compile ⓪c, build). Before a
  branch's union lands, it predicts the manifest the MERGE WOULD WRITE and refuses
  a row key that appears TWICE, naming the key with **both** line numbers in the
  standards gate's own shape: `devdoc/DOC-INDEX.tsv:<line>: duplicate row for <key>`.
- **Wired into BOTH real merge faces**, immediately before `git merge`:
  - `internal/tools/task_merge.go` — the manual face (today spelled `gopi task
    merge`; when this gate was written the same face also had a top-level
    spelling, `gopi merge`, which the merge-removal epic later deleted).
  - `internal/tools/repo_sweep.go` — the merger's automatic tick.
- **Refusal arm** `taskMergeRefuseManifest` (+ `manifestUnionGateSubmitRework`):
  archives the branch under the new reason `ArchiveReasonManifestDuplicate`
  (a fourth AUTOMATIC lane, counted with its siblings), and files exactly ONE
  branch-rework row whose payload carries the measured evidence. The branch is
  refused, not merged: master is untouched.
- **Regression test** `internal/tools/manifest_union_gate_test.go` — the incident
  shape reproduced with real git in a temp repo (common base → master moves the
  row below the header → the branch independently adds its own row for the SAME
  key at the far end), plus a control (different key ⇒ no refusal), a conflict
  abstention, the disabled-via-env abstention, the no-manifest abstention, the
  already-contained short-circuit, and an end-to-end refusal through the real
  `TaskMerge` surface asserting master unmoved + branch archived + one pending
  rework row.
- **The pre-existing duplicate on master is repaired**: `devdoc/DOC-INDEX.tsv`
  carried the row for `devdoc/docindex-write-path-row-ownership.md` at BOTH line
  10 and 762 (from the real merge `d7f931ec`); the stale copy was deleted and this
  doc's row added. `go test ./internal/standards/` was RED on master before this
  change and is green after it.

## Why a merge is the one writer that duplicates a manifest key

Measured on this repo, not inferred. `devdoc/DOC-INDEX.tsv`'s key is column 1.
The row for `docindex-write-path-row-ownership.md` stood **ABOVE the column
header** on the common base (`dd82af4a`). Master's side (`a77ac88e`) moved it
below the header; the branch (`03d7447e`, `feat/docindex-scope-classification`)
had independently classified the same document and appended its own row at the
far end. `git show --stat d7f931ec` reports **`+1 -0` against EACH parent** — the
two hunks do not overlap, so git UNIONED them instead of raising a conflict, and
the landed table carries the key at line 10 and line 761. No gate looked at the
merged table before it landed.

## Why the sibling union gate does not already catch this

`mergeUnionGate` (⓪c) builds its merge result with `git read-tree -m`, and that
builder **cannot see this shape at all**: on a reproduction of the incident,
`read-tree -m` leaves `DOC-INDEX.tsv` unmerged and `write-tree` fails, so the ⓪c
lane abstains (`MERGE_RESULT_UNAVAILABLE`) on exactly the union it would have to
inspect. This gate therefore materialises the union with `git merge-file -p` —
git plumbing's own three-way merge — from the three blobs the merge would use.

## Verification

- Build: `go build ./...` → OK; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK; `go vet ./internal/tools/` → OK (silent).
- Tests: `go test ./internal/tools/ -run 'TestManifestUnionGate|TestTaskMerge_ManifestRefusal|TestMergeUnionGate|TestMergeContractGate|TestMergeBuildGate' -count=1` → **ok**.
  `go test ./internal/standards/ -count=1` → green **after** the DOC-INDEX repair (it was red on master before it: `docclass_gate_test.go:91: devdoc/DOC-INDEX.tsv:762: duplicate row for …`).
- Ran: `go test ./internal/tools/ -run TestMdupEvidenceRealRefs -count=1 -v` — a temporary harness (deleted before commit) invoking `manifestUnionGate` **against this repo's real historical pair** `a77ac88e…` (master side) and `03d7447e…` (the branch d7f931ec merged), i.e. the exact refs whose union produced the live duplicate.
- Result: `refused=true manifest=devdoc/DOC-INDEX.tsv`;
  `key=devdoc/docindex-write-path-row-ownership.md union-lines=(10,761)`;
  detail = `devdoc/DOC-INDEX.tsv:10: duplicate row for devdoc/docindex-write-path-row-ownership.md | devdoc/DOC-INDEX.tsv:761: duplicate row for devdoc/docindex-write-path-row-ownership.md — … (both sides carry identical text for this key)`.
  The two line numbers are the landed merge's own (verified: `git show d7f931ec:devdoc/DOC-INDEX.tsv | grep -n …` → 10 and 761), and the byte-equality of the gate's union prediction against a real `git merge` is asserted by the regression test.
  The end-to-end refusal arm was executed through the real surface: `go test ./internal/tools/ -run TestTaskMerge_ManifestRefusalArchivesAndNotifies -count=1` → master tip unchanged, `archive/candidate` tag created, exactly one pending branch-rework row carrying the key.
- Commit: `b42f78d0` feat(merger): refuse a merge whose union duplicates a manifest row key

## Evidence (self-filed)

- `git show --no-patch --format='%P' d7f931ec` → `a77ac88e… 03d7447e…`; `git show d7f931ec:devdoc/DOC-INDEX.tsv | grep -c docindex-write-path-row-ownership.md` → 2 (lines 10, 761); per parent → 1 row each. The union-dup is real, not hypothetical.
- `git read-tree -m <base> <branch> <master>` on the reproduction → exit 0 with `ls-files -u` showing three stages and `write-tree` → `fatal: error building trees`: the ⓪c lane cannot build this union.
- `diff <(git merge-file -p ours orig theirs) <(the file a real git merge wrote)` → identical (30 lines, the duplicate at lines 4/30 in the probe; md5-equal in the probe run).
- Two defects in my own first cut were caught by measurement, not review, and are fixed: (a) the package's shared `repoSweepGit` runner TRIMS output, and a manifest's last row ends in a TAB — the trimmed read turned the duplicate row into an unparsable `3 fields, want 4` and produced a false abstention; the gate now reads blobs byte-exactly (`manifestUnionGateCatFileBlob`). (b) `git ls-tree <rev> devdoc/DOC-INDEX.tsv` resolves the pathspec against the PROCESS CWD, so a run from `internal/tools` silently found no manifest; the gate now passes `--full-tree`.

## Not Done (honest)

- **No live merger tick was run against this repo.** The refusal arm was executed
  through the exported `TaskMerge` (the function both CLI faces and the tick
  call) on a real temp repo and a real ledger, but the CLI face itself (then
  `gopi merge`; removed 2026-11-01, today `gopi task merge`) was not
  pointed at a repository, because doing so would mutate this host's repo
  configuration and merge state. The untested remainder is the CLI's own flag
  parsing and rendering, which this diff does not touch.
- **The gate judges ONE declared manifest** (`docclass.Path`). A repo whose
  second table declares its own key column is not covered: the corpus has exactly
  one such table, and generalising the key-column discovery is a separate task.
- **Attribution when both sides carry identical text** is deliberately not
  attempted (the real incident's own case): the refusal says so outright instead
  of guessing which side to edit.
- The duplicate on master is repaired here out of necessity (the standards suite
  was red), but the OTHER 2435-row coverage/banner state was not audited.
