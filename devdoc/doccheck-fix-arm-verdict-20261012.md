<!-- doc-class: historical -->
# Task Report: the doccheck fix arm's verdict agrees with the report arm's

**Task**: `01a12f1640ad73e79544f6bc0ba3b066` · **Branch**:
`task/01a12f1640ad73e79544f6bc0ba3b066` · **Host**: hosta · **Rule refs**:
15 (no silent failure), 31 (run the surface), 35/38 (one policy one place; DATA
is regenerated, never rewritten by a verb), 41 (evidence before prose).

## Completed

- **`cmd/agent/cli_doccheck.go` — the fix arm now asks BOTH questions.** Before
  this task `--fix` asked only the gap question (`docindex.CheckCorpus`) and, on
  a corpus with 0 gaps, printed
  `doccheck: nothing to repair — the corpus is already consistent` and exited 0
  while `--check` exited 1 on the *other* question (anchor line-number
  coherence). The arm now **borrows the report arm's own function**
  (`docCheckReportDrift`, the same code, not a second reading of the same table),
  so two doors give one verdict on one corpus:
  - 0 gaps → `doccheck: nothing to repair in the GAP class (0 gaps between
    devdoc/ and devdoc/DOC-INDEX.tsv)` — the claim is now scoped to the class
    this arm OWNS, instead of claiming the whole corpus from one question.
  - drift arm failing → the drift section is printed (by the report arm's own
    code) and the run closes with
    `doccheck: this run exits 1 on anchor NUMBER drift — the class --fix CANNOT
    write: it is DATA, regenerated only by the census command above, and the same
    class the report arm refuses on (see the section above)`, **exit 1**.
- **The exit-1 class is spelled ONCE** (`docCheckDriftVerdictClass = "anchor
  NUMBER drift"`) and composed into both arms' closing lines
  (`docCheckReportDriftVerdictFormat` / `docCheckFixDriftVerdictFormat`), so a
  renamed class cannot reach one arm and miss the other (Rule 1b/14). The report
  arm's closing line is textually unchanged — it is now rendered from the shared
  constant.
- **`-h` states the scope difference verbatim**: the `--fix` paragraph now says
  the arm owns the GAP question only, that it *still asks* the second question so
  the arms never disagree, and names `anchor NUMBER drift` as the class it exits 1
  on because it is DATA repaired only by the census command.
- **The missing guard**: `cmd/agent/cli_doccheck_test.go` gains
  `TestDocCheckFixAndReportAgreeOnTheSameCorpus` (ONE fixture, both arms run on
  it, verdicts asserted equal; the old false sentence asserted absent; the class
  and the census command asserted named; the two arms' drift sections asserted
  byte-identical because they come from the same function) and its other
  direction `TestDocCheckCleanCorpusKeepsBothArmsAtZero` (an equality check
  satisfied by two arms that refuse everything would be worthless).
  - `docCheckArmsFixture` builds the measured shape of the defect: the FULL
    corpus (generators copied in, manifest, repository identity — the arm runs
    through `RepairGap`, so a fixture without a repository would measure its own
    missing repository), plus one anchor row whose recorded text is present but
    not on the line its cell names (via `docindex.Row` + `docclass`'s own tier
    and separator constants), plus a well-formed frozen table carrying one
    readable permit that covers nothing (the stale-permit arm — which also proves
    the table was READ, not skipped). Two preconditions are asserted before the
    comparison: the fixture carries 0 gaps, and it DOES make the drift arm fail.

## Not Done

- **The drifted row on master is not repaired here, on purpose.** The live
  unpermitted drift (`devdoc/rule17-gate-report.md:1558`) is repaired by
  regenerating `internal/docclass/testdata/anchor-number-baseline.tsv` with
  `go run internal/docclass/testdata/anchor-number-census.go . <table>` — DATA
  (Rule 38), and `--fix` still refuses to touch it. This task's acceptance is
  the two arms' verdicts agreeing, not the corpus becoming green; making the verb
  write that file would have been the defect, not the fix.
- The permit that now covers nothing on master (the same regeneration ratchets
  it away) is likewise left to the census generator.

## Verification

- **Build**: `go build ./...` → OK; `go vet ./cmd/agent/` → clean.
- **Tests**: `go test ./cmd/agent/ -run 'TestDocCheck'` → `ok … 3.210s`, all 10
  doccheck tests PASS (8 pre-existing + the 2 new).
  Full branch-scoped suite `make test-budget PKGS='./cmd/agent'` → see the
  job's own output (recorded beside this report in the session's artifacts).
- **Ran** (Rule 31 — verbatim, in the worktree, on the REAL corpus, both
  revisions; binaries written **outside** the checkout, into the session's
  artifacts dir):
  - pre-fix (`git checkout HEAD -- cmd/agent/cli_doccheck.go`, binary
    `$SB/artifacts/gopi-doccheck-prefix`):
    `… tool doccheck --repo .` → drift section, `0 gap(s)`, exit **1**;
    `… tool doccheck --repo . --fix` → `doccheck: nothing to repair — the corpus
    is already consistent`, exit **0**.
  - fixed (binary `$SB/artifacts/gopi-doccheck`, sha256 prefix `c49a7e863d436a10`):
    `… tool doccheck --repo .` → drift section, `0 gap(s)`, exit **1**;
    `… tool doccheck --repo . --fix` → `nothing to repair in the GAP class (0
    gaps …)` + the drift section + the `CANNOT write` closing line, exit **1**;
    `… tool doccheck -h` → exit 0, help carries the scope paragraph above.
- **Result**: the two arms agree on the real corpus (1 and 1) where before they
  disagreed (1 and 0). `git status --porcelain` in the worktree printed exactly
  the two files this task edits both before and after `--fix` — the fix arm
  wrote nothing (and in particular did not touch the baseline `.tsv`).
- **Final, on the COMMITTED revision** (`721268925`, worktree clean — appended to
  `fixed-both-arms.txt`): `--check` exit **1**, `--fix` exit **1** naming the
  class, `git status --porcelain` empty before AND after `--fix`, and
  `git diff --stat -- internal/docclass/testdata/anchor-number-baseline.tsv`
  empty (the DATA table was not rewritten by the verb).
- One observation from those runs, recorded rather than fixed: the drifted row
  is reported as `devdoc/rule17-gate-report.md:1558` before the commit and
  `:1559` after it. That number is the row's line INSIDE `DOC-INDEX.tsv`, and
  this run legitimately inserted a row (the new report's) above it — a permit's
  identity is keyed on the doc + verdict + anchor + text and never on a manifest
  line (`AnchorFinding.Key`), so the drift report is not affected by the shift.
  Not a defect and not this task's to change; noted because a reader comparing
  the two captures will see it.
- **Evidence (self-filed)** — all under `$SB/artifacts/` where
  `$SB=/home/user/.gopi/sessions/01a12f1640ad73e79544f6bc0ba3b066`:
  - `prefix-both-arms.txt` — both arms on the pre-fix revision (1 / 0, with the
    false sentence).
  - `fixed-both-arms.txt` — both arms + `-h` on the fixed revision, each with its
    exit code, plus `git status --porcelain` before and after `--fix`.
  - `redproof-test.txt` — the new guard against the PRE-FIX `docCheckFix` (with
    only the shared class constant declared so the test still compiles), failing
    on BEHAVIOUR: `THE TWO ARMS DISAGREE ON ONE CORPUS: report=1 fix=0`.
  - `green-tests.txt` — the 10 doccheck tests, `test_exit=0`.
  - `cli_doccheck.go.fixed`, `cli_doccheck_test.go.fixed` — the exact revisions
    the evidence above was produced from.
- **Commit**: see the task branch tip.

## Evidence (self-filed)

- pre-fix, real corpus: `--check` exit 1 / `--fix` exit 0 printing
  `nothing to repair — the corpus is already consistent` (`prefix-both-arms.txt`).
- fixed, real corpus: `--check` exit 1 / `--fix` exit 1 naming
  `anchor NUMBER drift — the class --fix CANNOT write` (`fixed-both-arms.txt`).
- new guard on pre-fix code: FAIL `report=1 fix=0` (`redproof-test.txt`);
  on the fix: PASS (`green-tests.txt`).
- `git status --porcelain` before and after `--fix` on the real corpus: identical
  (the two edited files only) — `--fix` wrote nothing (`fixed-both-arms.txt`).
