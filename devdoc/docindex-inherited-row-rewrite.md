<!-- doc-class: normative -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# The docindex write path answered for paths its caller never touched — measured, closed, and what remains

**Task** `01a0dc1b-542e-77ba-87df-2015ab3170bb` (worktree
`/home/user/workspace/gopi-01a0dc1b542e77ba87df2015ab3170bb`, branch
`task/01a0dc1b542e77ba87df2015ab3170bb`), 2026-09-26.

**One sentence.** The generator's incremental mode was a function of the TREE
("every devdoc file with no row") instead of a function of the CALLER's paths, and
the branch-sync's own scope refusal — the one refusal whose trigger is "somebody
else's change is in this tree" — prescribed that whole-corpus command, so a
single-path repair rewrote rows for paths nobody in the room had touched and the
rewrite rode along in the caller's own `git commit`.

## Completed

### 1. The mechanism, measured on a git fixture AND on the live corpus

`devdoc/evidence/repro-inherited-row-rewrite.py` — one fixture, two generators
(`DOCCLASS_CLASSIFY=<path>`), so the SAME tree shows the defect and then the
closure.

A. GIT FIXTURE. The caller writes ONE path (`devdoc/mine.md`) and never names
`devdoc/other-run-half-state-report.md` (another run's half-state file: on disk,
no row).

| generator | command | exit | rows written | caller's own commit carried a row for the untouched path |
|---|---|---|---|---|
| PRE-FIX (`git show HEAD:...`) | `classify.py . --add-missing` | 0 | 3 — `devdoc/DOC-INDEX.tsv`, `devdoc/mine.md`, `devdoc/other-run-half-state-report.md` | **YES** |
| POST-FIX (branch) | same | **2** | **0** | **NO** |

The pre-fix row, byte-compared (absent before, present after):

```
devdoc/other-run-half-state-report.md<TAB>historical<TAB>historical (0 standing / 0 record): the doc's own title/name declares a past run's record, and no current artifact cites it as authority<TAB>
```

B. COPY OF THE LIVE CORPUS (2422 rows). Copied read-only — `git archive HEAD
devdoc tools | tar -x` into scratch, never a checkout of the operator's tree; the
live manifest's bytes are compared before and after, so "the live corpus was not
touched" is measured, not asserted.

| generator | exit | copy manifest changed | row for the injected inherited path | LIVE manifest bytes unchanged |
|---|---|---|---|---|
| PRE-FIX | 0 | **YES** (`added: 1`) | **YES** | True |
| POST-FIX | **2** | NO | NO | True |

C. THE TARGET SHAPE, already correct: `--only P` on the live corpus wrote
**1 insertion and 0 deletions** across 2423 rows.

### 2. The fix — the incremental mode is branch-scoped / pair-atomic

`tools/docclass/classify.py`:

- **A scope, in one place.** `--only PATH` (repeatable) and `--only-file FILE`
  (`-` = stdin, `#` comments) are the caller's own paths; the AMBIENT scope (no
  path named) is the branch's **committed** additions under `devdoc/`, diffed
  three-dot against the base so it is "what this branch added", never "what
  differs from the base tip". Base precedence, resolved in ONE function:
  `--scope-base` > `GOPI_DOC_SCOPE_BASE` > the branch's upstream > `HEAD`.
- **The refusal (recorded, non-zero, nothing written).** Two classes of path
  belong to no writer this run can speak for: a corpus file with no row that the
  scope does not name, and a row (`orphan row`) whose path is absent from the
  tree. An AMBIENT run that meets either refuses outright and names every path.
  **The orphan-row branch is the incident's own symptom**: a `historical` row
  whose reason no current writer can emit, for a file that is not there.
- **A named run is pair-atomic.** It writes exactly the rows for the paths it was
  handed, and RECORDS the rest as left byte-for-byte — never a silent rewrite
  (Rule 15). A row belongs to the run that ADDS the document.
- **Every refusal and every partial answer is RECORDED on stderr** (Rule 15): the
  ambient refusal names each unattributed file and each orphan row, and a named
  run names what it left byte-for-byte. A provenance side-file was built and then
  REMOVED: it is itself a corpus file, so it needs a row (the coverage gate failed
  it, correctly) and a fifth manifest column is a wire shape this defect does not
  own. The record that closes this defect is the refusal, not a new artifact.

**Why `git status` is NOT the ambient scope.** In a shared worktree an untracked
file is *indistinguishable* from another run's half-state file — both are "on
disk, in no commit", and git cannot say whose they are. Taking the set was
precisely the defect, so the ambient reading is the committed additions only, and
the run that owns an uncommitted path names it (which is exactly what the write
hook already does, `generatorOnlyFlag`).

### 3. Corrected guidance — no whole-corpus command for a single-path repair

- `internal/docindex/scope_git.go`: `assertTreeIsScoped`'s refusal used to say
  *"classify by hand (`classify.py . --add-missing`)"*. It now names the ONE path
  it refused and the single-path command for it, and says in the sentence itself
  that a whole-corpus run is not offered because it "would answer for every path
  in the tree, which is the rewrite this check exists to refuse". The sentence is
  produced by `scopedRefusal(dir, path)` so a test drives the REAL renderer.
- `internal/docindex/docindex.go`: `WritePathReason` and `ScopeGaps.Refusal` now
  reference the shared constants `SinglePathRepairCommand` /
  `BannerRepairCommand`; `WholeCorpusAddMissingFlag` exists as the **banned
  vocabulary**, spelled once, so a gate can assert it never appears.

### 4. Tests

- `scripts/test_docclass_scope.py` — Python, the incremental/scoped behaviour
  (39 checks). RED/GREEN by pointing it at either generator.
- `internal/docindex/guidance_gate_test.go` — Go owns the string and the
  behaviour: every refusal and reason names the single-path command, none offers
  the whole-corpus shape, the constants agree with the generator's own flags, and
  the scope refusal names the path it refused.

## Not Done

- **`WritePathReason`'s wording is unchanged in substance.** Its `--only <path>`
  was already path-scoped, so it was NOT a source of the defect; it is now
  built from the shared constant. The `repo` argument of that sentence became the
  literal `<repo>` rather than a rendered path, because it is a stored MANIFEST
  CELL — a row carries the sentence, not one caller's path.
- **`EnsureDoc`'s own reason string is unchanged.** It is a renderer for a
  different door, it already ships no whole-corpus command, and changing it would
  rewrite rows for documents this defect did not touch.
- **The existing 2423-row corpus was not rewritten.** Zero rows in it are orphan
  rows today (measured: 0 rows whose path is absent from the worktree, 0 absent
  from HEAD), so there is nothing to repair; the refusal now prevents new ones.
- **Whether the manifest's own row should be inside a scoped run's write set** was
  a judgement call, resolved YES: the manifest is the file the command writes and
  it must index itself (`index-meta`), which also settles `Repair`, whose
  augmented set always includes `ManifestRel`. It costs one row on a scoped run
  and it is by construction in scope.
- **`banner.py` was not changed.** It already takes `--only` and touches only the
  paths named.

## Verification

- Build: `go build ./...` → OK; `go vet ./internal/docindex/ ./cmd/agent/` → clean;
  `gofmt -l internal/docindex/` → empty.
- Tests: `go test ./internal/docindex/` → `ok`; `go test ./internal/standards/` →
  `ok` (6.0s); `python3 scripts/test_docclass_scope.py` → `PASS 39/39`;
  red proof `DOCCLASS_CLASSIFY=<pre-fix> python3 scripts/test_docclass_scope.py` →
  `FAILED 21/39`; `go test ./internal/docindex/ -run 'TestRefusal|TestScopeRefusal'`
  → RED on the restored pre-fix sentences (both the "does not name the single-path
  command" and the "offers the whole-corpus command" assertions fired), GREEN
  after restoring the fix.
- Ran:
  1. `python3 tools/docclass/classify.py . --only devdoc/evidence/repro-inherited-row-rewrite.py`
     (the real corpus).
  2. `python3 tools/docclass/classify.py . --add-missing` against a read-only copy
     of the live corpus, with an inherited half-state doc injected into the copy,
     for both the pre-fix and post-fix generator.
  3. `python3 devdoc/evidence/repro-inherited-row-rewrite.py`, and the same with
     `DOCCLASS_CLASSIFY=/home/user/workspace/docidx-pre-fix-classify.py`.
- Result:
  1. exit 0, `{"added": 0, "updated": 1, "rows": 2423}`; `git diff --stat -- devdoc/DOC-INDEX.tsv`
     → `1 insertion(+)`; `git diff -U0 | grep -c '^-[^-]'` → `0` — one path's row,
     no other row touched, on a 2423-row manifest, after an independent `M` on
     that file.
  2. live copy, injected `devdoc/zz-inherited-probe.md`: PRE-FIX → exit 0,
     `{"added": 1, ...}`, manifest changed, the inherited row written; POST-FIX →
     **exit 2**, manifest unchanged, no row written, stderr names the scope and
     the path. In both phases the LIVE manifest's sha256 was unchanged
     (`151b66e600f943ed`) — the operator's tree was never touched.
  3. fixture A: PRE-FIX wrote 3 rows and the caller's own commit carried the row
     for the untouched path (`-> include of a path the caller never named: True`);
     POST-FIX refused (exit 2), wrote 0 rows, and the caller's commit carried 0
     manifest rows.
- Commit: see the commit on the branch (this file is added by it).

## Evidence (self-filed)

- `python3 scripts/test_docclass_scope.py` → `PASS 39/39`.
- `DOCCLASS_CLASSIFY=/home/user/workspace/docidx-pre-fix-classify.py python3 scripts/test_docclass_scope.py`
  → `FAILED 21/39`, naming `ambient: exits NON-ZERO` and the named-scope checks.
- `git diff --stat -- devdoc/DOC-INDEX.tsv` on the real corpus → `1 insertion(+)`;
  deletions count `0`.
- Live copy, post-fix `--add-missing` → exit `2`; `copy manifest changed: False`;
  `row written for the injected (inherited) path: False`;
  `LIVE manifest bytes unchanged: True`.
- The scratch roots were on device `66309`; `/` is `66307` (Rule 22), and the live
  corpus was read through `git archive` + `tar -x`, never checked out.
