<!-- doc-class: historical -->
# Re-landing an archived tip whose two conflicts were an append-only index and a duplicate fix

Task `01a12b85-8576-73ec-a648-f7787958f199` · host **peerbox** · measured 2026-10-13.
Re-lands `8fafbafee88258bb0c01b9af6033fee29c3cd627` (branch
`task/01a12b6388b57199892195533ff2aaa8`, ref already deleted) onto master
`edd69493c`. The archived diff is `~/workspace/merge-run-01a12b/branch3-01a12b6388b5.diff`.

## The situation, and why it was not a rework of a semantic clash

The merge run's verdict was `CONFLICT (content)` on `devdoc/DOC-INDEX.tsv` **and**
`internal/tools/symbol.go`. Measured facts before touching anything:

```
$ git cat-file -t 8fafbafee                                   → commit   (tip survived the deleted ref)
$ git rev-parse --verify --quiet refs/heads/task/01a12b63…    → exit 1   (the branch ref is gone)
$ git merge-base 8fafbafee HEAD                               → deb60d879
$ git log --oneline -1 -S tagPseudoTagLineOffset HEAD -- internal/tools/symbol.go
  ffa1cf4f8 fix(code_search): accept a ctags index whose pseudo-tag block is not at byte 0
$ git log -1 --format='%s' ffa1cf4f8
  fix(code_search): accept a ctags index whose pseudo-tag block is not at byte 0
```

`ffa1cf4f8` and `8fafbafee` carry the **same subject and the same body** and share
the same base — they are two generations of one change, which had reached master
through a different task. So the `symbol.go` conflict was never a disagreement
about behaviour: **both sides had already fixed the same defect, with the same
predicate.** `git diff 8fafbafee ffa1cf4f8 -- internal/tools/symbol.go` was the
only thing that mattered, and what it shows is comment and spelling drift:

- master's later commits in the same area (`9e2e57750`, `dc38ae016`, `eca5a9bce`,
  `bff29510a`) had re-measured the constants (the index size and the probe's
  cost ratio), so master's `tagFileHeaderPrefix` / `tagPseudoTagSeparator` /
  `tagHeaderProbeBytes` comment blocks are the newer text;
- the loop in `tagPseudoTagLineOffset` was spelled `for off := 0; ;` on master and
  `for off := 0; off < len(head);` in the archived diff. Both are accepted by
  `gofmt -l` (measured: neither form is listed, and neither is rewritten by
  `gofmt -d`) and both implement the same predicate;
- master had gained a **different API** in the neighbourhood: `noteCompleteTagsIndex`
  now takes an `os.FileInfo` (the file identity is what separates an atomic
  publish from an in-place rewrite) where the branch's world passed a bare size.

## The resolution

**`devdoc/DOC-INDEX.tsv` — union.** The manifest is append-only: master's rows are
untouchable, the branch's new row is inserted at its path-sorted position. Taken
verbatim from `HEAD:devdoc/DOC-INDEX.tsv`, then the one branch row spliced in after
`devdoc/crosscutting-doc-truth-audit.md`. No opportunistic repair of master's own
pre-existing disorder (Rule: the union may not reorder or delete master rows).

```
$ git show edd69493c:devdoc/DOC-INDEX.tsv | wc -l       → 2404
$ wc -l < devdoc/DOC-INDEX.tsv                          → 2405
$ git diff edd69493c HEAD -- devdoc/DOC-INDEX.tsv | grep -c '^-[^-]'   → 0
$ git diff edd69493c HEAD -- devdoc/DOC-INDEX.tsv | grep -c '^+[^+]'   → 1
$ cut -f1 devdoc/DOC-INDEX.tsv | sort | uniq -d | wc -l → 0
$ awk -F'\t' 'NF!=4{c++} END{print c+0}' devdoc/DOC-INDEX.tsv          → 8   (master: 8 — parity)
$ grep -n 'ctags-index-block-off-byte-zero\|ctags-index-layout-probe' devdoc/DOC-INDEX.tsv
  566:devdoc/ctags-index-block-off-byte-zero.md   ← the re-landed branch's row
  567:devdoc/ctags-index-layout-probe.md          ← master's own row for the same fix, untouched
```

The 8 malformed rows are **master's own** (the same 8 on master) — a property of
the file, not of this merge, and not this run's business to fix. Both paths present
is the point: the two attempts' devdocs are two records of one defect, and neither
row's removal was this run's to make (the manifest is the docclass writer's).

**`internal/tools/symbol.go` — keep both, prove it.** Master's three
comment/constant regions and the branch's function body. The resolution is a
comment-region + loop-spelling choice, not a behaviour choice, and a **MERGE NOTE**
at the function states exactly that so a later reader does not read the surviving
spelling as a discarded behaviour.

**`internal/tools/tags_index_layout_test.go` — one adaptation.** The branch's
verification test called `noteCompleteTagsIndex(dir, int64(len(idx)))`; master's
API takes an `os.FileInfo`. Adapted with `os.Stat` (the file identity is what that
API is for), which is the only line that differs from the archived test file.

**Proof the branch delivered nothing less than it had** (the criterion the union
resolutions exist to satisfy):

```
$ git diff deb60d879 8fafbafee | grep '^+' | … (code lines, comments stripped, sorted)  → 18 lines
$ comm -23 <those 18> <the merged file's code lines, sorted>                            → EMPTY
```
Every non-comment line the archived diff adds is present in the merged file.

## The probe, and the ancestry

```
$ git merge --no-commit --no-ff task/01a12b85857673eca648f7787958f199
Automatic merge went well; stopped before committing as requested
EXIT=0
$ git diff --name-only --diff-filter=U        → (empty)
$ git merge --abort ; git status --porcelain  → (empty)
```

The merge commit is a **real two-parent merge** (`edd69493c 8fafbafee…`), not a
squash or a cherry-pick, so the archived tip's provenance is recorded and the
re-landed work descends from master by construction (`git merge-base --all` =
master tip alone).

## A measurement that did NOT hold up, recorded so nobody re-derives it

An early draft of this doc (and of the merge commit message) claimed the branch's
explicit-condition loop was kept because the semicolon form fails `gofmt -l`.
**Measured and refuted:** both forms are `gofmt -l` clean (`gofmt -l` on a
two-line probe of each spelling prints nothing; `gofmt -d` on the pre-merge master
file produced 0 lines). The claim was retracted from both texts. The reason the
explicit form was kept is simply that it is the archived diff's own text and it is
clearer; no defect is being fixed by the choice.

## Not done, honestly

- The orphaned object question — `ffa1cf4f8` and `8fafbafee` are two unreferenced
  generations of one fix, and the archived branch's ref was deleted while its
  commit stayed reachable only by SHA. That observation is filed separately; this
  run did not investigate the merge run that produced it.
- The 8 malformed `DOC-INDEX.tsv` rows are master's own and were left alone.
