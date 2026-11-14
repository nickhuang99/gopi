<!-- doc-class: normative -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# Task Report: devdoc truth classification — every file classed, the anchor gate driven by the manifest

> 日期：2026-09-19 · 分支：`feat/doc-truth-classification` · 模型：GoPi
> 范围：**只分类，不改文档**（除类横幅一行）。锚点漂移只记录、只排名，不重写。

## Completed

### 1. The artifact — `devdoc/DOC-INDEX.tsv`

One row per file under `devdoc/`, four tab-separated columns:

```
path    class   reason  evidence
```

**2012 data rows = 2012 files** (measured: `awk -F'\t' '!/^#/' devdoc/DOC-INDEX.tsv | wc -l` = 2012; `find devdoc -type f | wc -l` = 2012 — the manifest's 8 leading `#` comment lines are its own documentation, and the parser skips them). This report is one of them —
this report is one of them, a `normative` row like any other, which is why the
gate it describes also gates it).

| class | rows | markdown rows | meaning |
|---|---|---|---|
| `historical` | 1595 | 651 | a past run's own record — frozen by nature, and falsified by "fixing" it |
| `normative` | 404 | 404 | makes a standing claim about current master, plus a recorded spot-check |
| `roadmap` | 12 | 12 | states intended future work; nothing in the tree to check yet |
| `index-meta` | 1 | 0 | routes or describes the corpus itself (`DOC-INDEX.tsv`) |

944 of the 945 non-markdown rows are recorded artifacts (captures, JSON, logs
under `evidence/`, `assets/`, `webcam-capture-evidence/`) — classed `historical`
by construction: an artifact is a record, not prose that can drift. The 945th is
the manifest's own `index-meta` row.

Normative rows carry a spot-check in the evidence column, in tier order:

| tier | rows | what the gate re-verifies |
|---|---|---|
| `sym` | 85 | the named file still **declares** that symbol (AST parse of top-level decls) |
| `anchor` | 132 | the file exists and that line is in range and non-blank |
| `path` | 145 | that repo path still exists |
| `cite` | 41 | a non-devdoc artifact still cites this doc as the live statement |

### 2. Policy — content-first, never a filename heuristic

`normative` requires **a standing claim about current master AND a spot-check
that resolves**. Everything else defaults to `historical`. Signals, in order:
a resolving code anchor/symbol/path; a non-devdoc artifact citing the doc as
the live statement (this is how a landed ruling keeps its force — `GOPI.md`
Rule 24 cites `devdoc/detach-judgment-rule.md`); standing language vs
record language; a record title or a date in the name. A title like
`# Task Report: …` (57 docs) is a seed, never decisive on its own.

The policy lives as the docstring of `tools/docclass/classify.py` (provenance:
recorded once, so the classification is reproducible), while **the Go gates
re-verify the manifest itself** — nothing in the build runs Python.

### 3. The anchor gate's scope is now the manifest

`internal/agent/docanchor_test.go` used to hardcode exactly two files
(`devdoc/hippocampus.md`, `devdoc/agent-five-organs-ontology.md`) while
`devdoc/` held 2012 — anchor coverage was a claim about two files wearing the
name of a repo-wide guarantee. Scope now = every **normative** markdown row.
Adding a normative row extends coverage with no edit to the gate; moving a doc
to `historical` removes it. Historical docs are deliberately **not** gated.

Measured coverage after the change (the gate's own log line):

```
doc anchors verified: 1948 across 404 normative docs
  (1585 explicit + 363 bare continuations; 207 bare out of row/table/heading scope;
   201 pre-existing drift entries permitted, 0 new, 0 stale permits)
```

Both anchor forms the corpus uses are covered, with the bare `:NNN`
continuation resolved by the documented chain **same row → same table block →
nearest heading** — the shape §4.2/§4.5 of the ontology spec uses, and the shape
the old gate did not read at all. A bare anchor with no `.go` token in reach is
reported as **out of scope**, never silently counted as verified.

The gate also re-verifies each normative row's recorded spot-check, which is
what makes the class *held* rather than *asserted*: pointing a row's evidence at
a symbol its file does not declare is red (proof 5 below).

### 4. The banner convention (implemented for `historical`)

A `historical` markdown doc opens with:

```
<!-- doc-class: historical -->
```

An HTML comment, so it is invisible when rendered, and the **first line**, so a
reader meets the class before the title. 651 markdown docs carry it; every one
of those 651 diffs is **+1/−0** (`git diff --numstat` — verified: no other line
of any document is touched). `tools/docclass/banner.py` applies, removes and
`--check`s it; on the clean tree it reports `applied 0, removed 0`.

### 5. Two new machine gates (`internal/standards/docclass_gate_test.go`)

* **coverage** — the manifest and the tree must agree exactly. A new devdoc file
  with no row is red (it would otherwise be invisible to every class-driven
  gate); a row naming a file that no longer exists is red (a stale permission).
* **shape** — a declared class, a reason that actually says why (≥24 chars), and
  a spot-check exactly where the class requires one (normative) and nowhere else.
* **banner** — a markdown doc of a bannered class carries its banner, and a
  banner that disagrees with the manifest is red (the class is visible in the
  FILE, not only in a TSV).

Anti-vacuity floors are built in (≥1900 rows, ≥300 normative docs, ≥1200
resolved anchors, ≥600 bannered docs), so a broken read fails instead of
printing a green over nothing.

### 6. Red proofs — `scripts/docclass-red-proofs.sh` (exit 0)

Thirteen proofs, each mutating the tree, running the ONE test that must notice,
recording the verdict, and restoring (the restore is itself checked, and says
so: `restore check: 4 file(s) compared, 4 file(s) held mutations`):

| # | mutation | expected | observed |
|---|---|---|---|
| 1 | bogus anchor in an ordinary normative doc (not one of the two old hardcoded files) | RED | RED — "past EOF" |
| 2 | same bogus anchor in a historical doc | GREEN | GREEN (deliberately ungated) |
| 3 | keep it, re-class that row `normative` (no test edit) | RED | RED — the row **is** the scope |
| 4 | ambiguous bare basename (a bare line anchor on `cmd/agent/main.go` — eleven files share that name) | RED | RED — "AMBIGUOUS" |
| 5 | normative row's evidence → a symbol its file does not declare | RED | RED — "no longer DECLARES" |
| 6a/6b | normative row with an empty evidence cell | RED | RED in both manifest-shaped gates |
| 7 | new devdoc file with no manifest row | RED | RED — "no row in DOC-INDEX.tsv" |
| 8a/8b | banner deleted / banner disagreeing with the manifest | RED | RED — "does not open with its banner" / "banner says … classes it" |
| 9 | baseline permit that matches nothing | RED | RED — "stale baseline entry" |
| 10 | control run, tree as committed | GREEN | GREEN |
| 11 | **SIGTERM a run mid-mutation** (no normal path to cleanup) | exit 130, no residue | exit 130, `residue=0` |

Shape 11 is a defect this task produced and then closed. Proof 1's first run was
launched by a harness that killed its whole process group; only an EXIT runs an
EXIT trap, so the script died with the bogus anchor still appended to two
`devdoc/` files — a dirty worktree, i.e. the exact defect this work exists to
catch, manufactured by its own proof. The fix is threefold and measured: INT/TERM
handlers that turn a signal into an exit (the child logs
`interrupted by SIGTERM — restoring before exit`), restore **re-checked inside
the EXIT trap** as well, and a `docclass-redproof` marker stamped into every
mutation so a start-up scan refuses to run on residue from an earlier death
(rather than faithfully snapshotting it as if it were the committed text).

A live demonstration, and it is worth recording: the first version of THIS
report wrote that mutation in backticks, and the gate immediately reported a new
failure on the report itself (the ambiguous-basename diagnostic, naming ten candidate files) — the doc that describes
the gate is gated by it, and a report that quotes a broken anchor without saying
so is exactly the rot this work removes. The phrasing above is the corrected one.

Two further design points in that script are there because the measurements forced them
(both recorded in the file): restore is **by copy, never `git checkout`** (a
restore that fails while its error is unread turns every later verdict into
noise — measured: an unreported duplicate baseline line made the control run
red), and **both samples come from the manifest** (an earlier version named a
doc it assumed was normative, reported a false failure, and — worse — became an
"authority" the generator counted as citing it).

## Findings — the ranked drift list

Pre-existing drift that the widened scope measured for the first time: **201
entries across 56 of the 404 normative docs** (`eof` 59, `blank` 51,
`missing-path` 44, `unknown-basename` 25, `ambiguous` 22). One finding here was
not drift but a **defect in the pre-existing gate itself**:

### F0 — the anchor gate was already RED on master (defect, not drift)

Measured in a clean detached worktree at master:

```
--- FAIL: TestHippocampusDocAnchorsResolve
    devdoc/hippocampus.md: anchor (bare) internal/tools/memory.go:2780 points at a BLANK line
    devdoc/agent-five-organs-ontology.md: anchor (bare) internal/tools/memory.go:2780 points at a BLANK line
    devdoc/agent-five-organs-ontology.md: anchor (bare) internal/tools/memory.go:2855 points at a BLANK line
```

So master's only doc-anchor guard has been failing, and nothing noticed. These
are the same 3 blank-line entries the earlier audit found; they are permitted in
the baseline **with that reason**, not hidden. *Bug-worthiness: HIGH — a red gate
nobody sees is worse than no gate; it is also cheap (re-key 3 lines to their
symbol names).*

### Findings that mislead a reader about LIVE state

Ranked by (a) whether the named artifact is **gone** and (b) how much of the
remaining prose still reads as current. The per-doc number is the manifest's own
`stale=` field (explicit anchors that no longer resolve).

| rank | doc | stale | what is actually wrong | bug-worthiness |
|---|---|---|---|---|
| 1 | `devdoc/verify-responsibility-spec-20260912.md` | 9 | 8 `missing-path` + 2 blank: names a spec/artifact set that is gone while the prose reads as the verifier's contract | **HIGH** — a live-sounding spec whose evidence trail is gone |
| 2 | `devdoc/requirements/merge-branch-lifecycle.md` | 4 | 3 `missing-path`, 4 `unknown-basename`: a *requirements* doc (the module-map series) whose named tests no longer exist | **HIGH** — requirements series is the doc set a reader treats as current |
| 3 | `devdoc/coding-standards-checklist.md` | 7 | 7 `missing-path`: cites files by path that are gone | **HIGH** — it is cited from `internal/standards`' rule text |
| 4 | `devdoc/requirements/02-merge-and-branches.md` | 5 | 2 `missing-path`, 2 `unknown-basename`, 1 blank | **HIGH** — same series |
| 5 | `devdoc/daemon-e2e-leak.md` | 13 | 11 `unknown-basename` (`*_test.go` names the repo no longer has) + 2 blank — the largest count | MEDIUM — the count is inflated by a repeated table; fix = one pass re-keying the test names |
| 6 | `devdoc/docanchor-gate-bare-lineno.md` | 3 | 24 `eof` + 2 blank + 1 ambiguous — the doc that DESCRIBES the bare-anchor gate | **HIGH for a reader**: it is the doc a future agent reads to understand this very gate, and its own anchors are drifted; also the single largest permit block (27) |
| 7 | `devdoc/assert-the-bug-audit-20260917.md` | — | 19 `eof` + 4 blank (23 entries) | MEDIUM — a recent audit whose line anchors moved; symbol names in the same rows still resolve |
| 8 | `devdoc/tmpdir-merge-resolution.md`, `devdoc/merger-ledger-writeback-audit-20260912.md`, `devdoc/merge-resolve-effort-audit.md` | 8/8/5 | `eof`/`blank` heavy | MEDIUM |
| 9 | `devdoc/cov-shot-hp.md`, `devdoc/verify-boundary-audit-20260912.md`, `devdoc/review2-cli-config-options-variant-20260903.md` | 4/4/5 | mixed | LOW–MEDIUM |

40 of the 404 normative docs carry ≥1 stale explicit anchor. The rest of the
normative corpus is clean.

### Findings that are FALSE claims of currency (no anchor involved — the class is the defect)

| # | finding | evidence | bug-worthiness |
|---|---|---|---|
| F1 | `devdoc/requirements/README.md`'s "measured on master" block is stale on **file counts**: it says **932** `devdoc/` files; the tree has **2012** | `sed -n 15,22p devdoc/requirements/README.md` vs `find devdoc -type f \| wc -l` | **HIGH — a bug in the doc, not drift.** It is a normative requirements doc stating a measured count that is now off by 2.2×; a reader has no way to tell which half of the block still holds |
| F2 | same block: `GOPI.md` = **1,085 lines, 29 numbered rules**; the file is **1,187 lines, 30 rules** | `wc -l GOPI.md`; `grep -cE '^#+ Rule [0-9]+' GOPI.md` (unique headings: 30) | HIGH — the *rules* half is the part readers act on; being one rule behind is exactly the failure the block was written to prevent |
| F3 | the same block also names 40 `internal/` packages and 47 `cmd/agent/cli_*.go` surfaces; measured **45** and **150** | `ls -d internal/*/ \| wc -l`; `ls cmd/agent/cli_*.go \| wc -l` | MEDIUM — same block, same defect: three of its five counts are stale |
| F4 | `devdoc/00-module-map.md:342` marks the module-map series `M15 summary | pending`, and `requirements/README.md:11` routes the reader to `99-summary.md` — **which does not exist on master** | `ls devdoc/requirements/99-summary.md` → absent; both routing rows present | MEDIUM — see the decision below: the map itself says `pending`, so the *route* is the defect, not the absence |
| F5 | 41 of 404 normative rows are held up by a `cite` only — the weakest tier (a non-devdoc file mentions the doc; nothing checks a code claim) | `awk -F'\t' '$2=="normative" && $4 ~ /^cite /' devdoc/DOC-INDEX.tsv \| wc -l` | LOW — recorded so the tier is not mistaken for a proof; a future pass can promote or re-class them |

### 99-summary decision (asked explicitly)

**Do not create it, and do not re-class the routers — re-point the route.**

Evidence: `00-module-map.md:342` records the row as `| M15 summary |
99-summary.md (does not exist yet) | the [15/15] row | pending |` — i.e. the map
already says it is unwritten, and the series' own `README.md:11` lists it as a
table row of the series. So this is **not** a missing authoritative doc; it is a
declared deliverable of a series in progress. The defect is narrower: a reader
following `README.md` is sent to a file with no "not yet written" marker on that
route. Classed `roadmap` would be wrong twice (it is not a plan, it is an
absence), so the recommendation is a one-line edit *outside this task's
boundary*: make the router say "pending (M15)" the way the map does.

### Independent product-facing bugs surfaced but NOT fixed (this task is classification-only)

| # | finding | evidence | bug-worthiness |
|---|---|---|---|
| B1 | `scripts/docclass-red-proofs.sh` (mine) and any future proof script can silently become an "authority" that promotes a doc's class — I excluded it by path; the general shape (tooling text counting as a citation of authority) is unfixed | `grep -rn AUTHORITY_EXCLUDE tools/docclass/classify.py` | MEDIUM — worth a follow-up task: authority should require a citation in a build-agent (`.go`/`Makefile`), not any file |
| B2 | the anchor gate cannot check a line's **content**, only that it resolves; the symbol name in the same row is the real authority | design note in `internal/agent/docanchor_test.go` | LOW — documented deliberately |
| B3 | 207 bare anchors (10.6% of the corpus' anchors) are **out of scope**: no `.go` token in the row, the table block or the heading above them | the gate's own counter, printed every run | MEDIUM — a follow-up pass can attribute them by adding the file token to their row, which would extend real coverage |
| B4 | **master's own Rule 17 gate is RED**: `internal/standards` `TestGate2TestEnvRedProof` reports `cmd/agent/merge_lock_test.go:200` as a new violation | `go test -run TestStandardGates ./internal/standards/` in a clean detached worktree at master → `--- FAIL: TestStandardGates/TestGate2TestEnvRedProof` | **HIGH** — the second red gate found in this task (see F0); FIXED in this branch's commit `44cfb05a` by the gate's own prescribed remedy (a reasoned inline `env-leak-ok:` escape: the child lock holder inherits `GOPI_HOME` at exec time, where `t.Setenv`'s restore would come too late, and the `SnapshotEnv` pair above the call is the restore) |
| B5 | two `fmt.Errorf(… %v, err)` sites in this task's own new `internal/docclass/evidence.go` were caught by master's Rule 16 gate (`TestGate1ErrorWrapRedProof`) | `go test -run TestStandardGates/TestGate1ErrorWrapRedProof ./internal/standards/` → `NEW violation … internal/docclass/evidence.go:87`, `:93` | FIXED in this branch: both wrap with `%w` (and the two `%v, rerr` sites beside them), so the gate is green |

## Not Done (honest)

- **No document was rewritten.** Every one of the 201 drift entries is *recorded*
  in `internal/agent/testdata/docanchor-baseline.tsv` with its reason and ranked
  above — the epic's boundary is classification. The baseline is a **ratchet**:
  a new failure is red and a permit that matches nothing is red, so it can only
  shrink. It is not a class exemption: the intended remedy for a doc whose
  anchors describe a tree that no longer exists is to re-class it `historical`.
- **The 651 banner diffs are the only document edits in this branch** (+1/−0
  each). No title, sentence, table or anchor was changed anywhere.
- **The banner convention covers `historical` only.** `roadmap` and `index-meta`
  are deliberately unbannered (a banner there would claim something the doc does
  not), and a `normative` banner would only restate the class its spot-check
  already proves. `ClassBannered` is one function, so a second bannered class is
  a one-line change.
- **The classifier is not wired into the build.** It is a generator recorded for
  provenance; the Go gates read the manifest. Deliberate (Rule 10.7: no new
  runtime dependency, and a Python generator in the build would be one).
- **F1–F4 and B1–B3 are not fixed** — they are reported here with their evidence
  and their bug-worthiness so they are dispatchable (Rule 26). B4 and B5 are
  fixed (they were red gates in this task's own way); F0 is recorded with its
  reason, not re-keyed.
- **The evidence tier counts (85/132/145/41) are not independent proofs**; they
  are what each row records. A `cite` row is the weakest and says so.

## Verification

- Build: `make build` → OK (`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`).
- Vet: `go vet ./...` → no output (clean).
- Format: `gofmt -l` on every touched file → empty. The 5 repo-wide hits
  (`internal/tools/…` ×3, `devdoc/voice-zerodep-poc/model_embed*.go`) are
  **pre-existing on master** — `git status --porcelain` lists none of them.
- Tests: `go test -count=1 ./internal/agent/ ./internal/standards/` → observed in
  this session, verbatim: `ok github.com/nick/gopi/internal/agent 469.768s` /
  `ok github.com/nick/gopi/internal/standards 25.479s`.
  The anchor gate logs `1948 anchors across 404 normative docs … 0 new, 0 stale
  permits`; the standards gates log `2012 rows cover 2012 files`,
  `404 normative rows (each carrying a spot-check)`, `651 markdown docs … carry
  their banner (0 mislabelled)`.
- Red proofs: `bash scripts/docclass-red-proofs.sh` → exit 0,
  `PASS: every proof produced the expected verdict and every mutation was restored`
  (13/13; the table above is its output), followed by
  `restore check: 4 file(s) compared, 4 file(s) held mutations` from the EXIT
  trap. Shape 11 was run and observed in this session: `OBSERVED: exit 130,
  residue=0` with the child logging
  `interrupted by SIGTERM — restoring before exit`.
- Branch: `feat/doc-truth-classification` on master `0317d09a`; the commits are
  `git log --oneline --reverse master..HEAD` — `5faf7daa` (manifest + generator
  + banners), `208665d8` (manifest-scoped anchor gate + `internal/docclass`),
  `83030f2d` (three measured defects fixed), `ac8e8d58` (red proofs),
  `7e0956c2` (this report), `44cfb05a` (the two red master gates this branch
  cleared), `6d1234e7` (report → B4 commit), `b16c028f` (the signalled-run fix),
  `5d3ef2f8` (proof 11 recorded), `0816aaee` (shape 11 bounded against
  recursion), `23a2997a` (the residue needle). Deliberately **no commit count
  here**: a number written into prose drifts the moment another commit lands,
  which is the class of defect this whole task exists to find — the count is a
  fact about the branch, so it is read from git, never restated. The report is a
  manifest row like every other file, so the coverage gate refuses a branch
  whose report is untracked — which is why the report-carrying commits are
  separate from the code commits.
  Working tree: `git status --porcelain` prints nothing.
- Reproduce the classification: `python3 tools/docclass/classify.py .` then
  `python3 tools/docclass/banner.py .` (idempotent: `applied 0, removed 0`).

## Evidence (self-filed)

- Manifest counts: `awk -F'\t' '!/^#/{print $2}' devdoc/DOC-INDEX.tsv | sort | uniq -c`
  → `1595 historical, 404 normative, 12 roadmap, 1 index-meta`.
- Coverage gate, verbatim: `devdoc/DOC-INDEX.tsv: 2012 rows cover 2012 files under devdoc/`.
- Anchor gate, verbatim: `doc anchors verified: 1948 across 404 normative docs (1585 explicit + 363 bare continuations; 207 bare out of row/table/heading scope; 201 pre-existing drift entries permitted, 0 new, 0 stale permits)`.
- Master was already red: the `TestHippocampusDocAnchorsResolve` failure block quoted in F0, observed in a detached worktree at master (`git worktree add --detach … master`, since removed).
- Banner diffs are pure additions:
  `git diff --numstat master...HEAD -- devdoc | awk '$1!=1 || $2!=0'` → only
  `devdoc/DOC-INDEX.tsv` (2021 rows) and `devdoc/doc-truth-classification-report.md`
  (302 lines) — the report is itself a devdoc file, so it is the one other
  non-banner diff; the 651 banner rows are all +1/−0.
- Red-proof run: `~/workspace/_docclass-scratch/redproof-final.log` — 13
  `OBSERVED:` lines, 0 `UNEXPECTED`, exit 0, final line `PASS: every proof
  produced the expected verdict and every mutation was restored`, then
  `restore check: 4 file(s) compared, 4 file(s) held mutations`. Its shape-11
  block reads `OBSERVED: exit 130, residue=0` /
  `interrupted by SIGTERM — restoring before exit`. (The run cited by an earlier
  draft, `redproof7.log`, predated shape 11 and stopped at shape 10 — a log that
  does not contain the proof it is cited for is not evidence.)
- The first run of the script after shape 11 was added **refused to start**:
  a false positive. The residue scan matched the bare marker word
  `docclass-redproof`, which the report mentions in prose — so the script would
  not run on the very document describing it. Fixed by scanning for the stamp
  line (`red proof (docclass-redproof):`) a mutation actually appends, which is
  the distinction between a mention and a leftover. That same run printed
  `scripts/docclass-red-proofs.sh: line 54: assert_restored: command not found`
  — the EXIT trap calls a function defined below it, so an early exit reported a
  missing command instead of a verdict; the call is now guarded on a mutation
  having been recorded.
