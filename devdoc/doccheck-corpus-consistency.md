<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# Task Report: the devdoc truth-class corpus can be ASKED — and a doc that missed the write hook is classified in the same commit

Task `01a0de4c-4e8b-7737-b670-2e51b6fcab83`. Branch `task/01a0de4c4e8b7737b6702e51b6fcab83`
(commit `c804d68e`).

## Completed

- **`gopi doccheck`** (`cmd/agent/cli_doccheck.go`, `CliVerbDocCheck`) — the operator's
  door to the corpus, two hands:
  - **report (default / `--check`)**: `internal/docindex.CheckCorpus` names every gap
    and exits 1 when there is anything to report. **Writes nothing** — enforced by
    control flow (the report path returns before any writer runs), not promised.
  - **`--fix`**: `internal/docindex.RepairGap` writes the missing half. A bare
    invocation never reaches it.
- **`internal/docindex.CheckCorpus` / `RepairGap` / `RepairableGaps` / `CorpusGap`** —
  the ONE reader, over the gates' own reader (`ReadManifest`), walk (`docclass.Walk`,
  git's ignore rule) and class vocabulary, so the verb's verdict and the gates' verdict
  cannot disagree.
- **`tools/docclass/classify.py --check`** — the same report-only surface in the repo's
  own generator, in `banner.py --check`'s shape and exit convention (1 = gap, 2 = usage).
- **`internal/docclass/shape.go`** — `ManifestCols` (4), `MinReasonLen` (24),
  `EvidenceTiers`, one home each (Rule 13/14). The corpus check made the retyping
  expensive: three surfaces now ask "is this row readable?".
- **`GOPI.md` Rule 36** — a devdoc that missed the write hook is classified in the same
  commit that lands it, with the two commands.
- Tests: 3 Go contract tests (`cmd/agent/cli_doccheck_test.go`, RED-proven) + 13 Python
  assertions (`scripts/test_docclass_scope.py`, suite now 50/50).

## Not Done (honest)

- **Not wired into any automatic path.** The check is invoke-only by design (Rule 35:
  no new timer/daemon/hook). A session that lands a devdoc by hand still has to run it —
  Rule 36 makes that a standing practice, and the check makes it cheap, but nothing
  *forces* it. That gap is real; closing it would mean a hook on a door that has none
  (the operator's editor), which is not ours to build.
- **Two gap shapes are reported and deliberately NOT written**: a row whose file is gone
  (a stale permission — deleting another writer's row is their decision) and a reason too
  short / an undeclared evidence tier (statements about a row's *content*, which a writer
  would have to invent). `--fix` names them, exits 1, changes nothing.
- **I did not run the tool against another host's ledger** (peerbox/HP split, Rule 28) —
  only this worktree.
- **`GOPI.md`'s Rule 36 has no machine gate of its own.** It is prose like Rules 1–15;
  the gates it names (`internal/standards`, `TestDocCheck`) exist, but nothing fails the
  build for a *person* who ignores the practice.

## Verification

- **Build**: `CGO_ENABLED=0 go build ./...` → OK; `go vet ./cmd/agent/ ./internal/docindex/ ./internal/docclass/` → no output.
- **Tests** (branch-scoped, the smoke path):
  - `go test -count=1 -run TestDocCheck ./cmd/agent/` → `ok` (3/3 PASS)
  - `go test -count=1 ./internal/docindex/ ./internal/docclass/` → both `ok`
  - `go test -count=1 ./internal/standards/` → `ok` (31.9s)
  - `go test -count=1 -run 'DocClass|DocIndex|DocManifest|Scope|Classify' ./internal/docindex/ ./internal/docclass/ ./cmd/agent/` → all `ok`
  - `python3 scripts/test_docclass_scope.py` → **PASS 50/50** (13 new assertions)
  - `gofmt -l` on every file I touched → no output; `cli_doccheck.go`/`_test.go`, `docindex.go`, `docclass.go`, `shape.go` all clean.
- **Ran** (the exact invocations, on real trees):

  On **this worktree's real corpus**:

  ```
  $ /tmp/gopi-dc doccheck
  doccheck: the devdoc corpus is consistent — every file under devdoc/ has a well-formed row
  exit=0
  ```

  On a **real fixture corpus** (`gopi sandbox create` → git repo → generators copied
  from this repo → one document landed with no row):

  ```
  $ /tmp/gopi-dc doccheck --repo "$R"
  doccheck: file-with-no-row devdoc/just-landed.md — every file under devdoc/ must be classified (class + reason)
  doccheck: 1 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv
  exit=1
  $ git diff --quiet -- devdoc/DOC-INDEX.tsv && echo "OK: report wrote nothing"
  OK: report wrote nothing

  $ /tmp/gopi-dc doccheck --repo "$R" --fix
  doccheck: repaired 1 path(s): devdoc/just-landed.md
  exit=0
  $ git diff --numstat -- devdoc/DOC-INDEX.tsv
  1	0	devdoc/DOC-INDEX.tsv            <-- EXACTLY "1 0"
  $ git diff -U0 -- devdoc/DOC-INDEX.tsv | grep '^\+devdoc'
  +devdoc/just-landed.md	historical	historical (0 standing / 0 record): the doc's own title/name decla…
  $ /tmp/gopi-dc doccheck --repo "$R"
  doccheck: the devdoc corpus is consistent — every file under devdoc/ has a well-formed row
  exit=0
  ```

- **Result** — the non-repairable arms (a stale row, a stub reason), `--fix` on them:

  ```
  doccheck: row-with-no-file devdoc/gone.md — the tree does not contain it — a stale permission
  doccheck: row-with-no-file devdoc/stub.md — the tree does not contain it — a stale permission
  doccheck: reason-too-short devdoc/stub.md — devdoc/DOC-INDEX.tsv line 14: 4 chars, want >= 24 — say WHY this class
  doccheck: 3 gap(s) … — none of these can be written by this command
  exit=1
  OK: --fix left the manifest byte-identical (nothing repairable)
  ```
- **Report-only, in the generator** — `python3 tools/docclass/classify.py . --check`:
  `0 gap(s)` / exit 0 on the clean fixture, exit 1 naming the gap on the broken one, and
  `md5sum -c` of `devdoc/DOC-INDEX.tsv` printed `OK` across both (the bytes are the assertion).
- **Commit**: `c804d68e` feat(doccheck): a report-only corpus check and an operator fix arm…

## Evidence (self-filed)

- **RED proof (Go)**, my test file against the pre-change tree `f7841805`
  (`git archive f7841805` into a sandbox + `cp cmd/agent/cli_doccheck_test.go`):

  ```
  $ CGO_ENABLED=0 go test -count=1 -run TestDocCheck ./cmd/agent/
  cmd/agent/cli_doccheck_test.go:133:44: undefined: docCheckGoModName
  …:188:37: undefined: docindex.GapFileWithNoRow
  …:285:38: undefined: docindex.SelfReasonForTest
  …:317:23: undefined: docCheckFixFlag
  …:319:15: undefined: parseDocCheckArgs
  …:330:11: undefined: docCheckFix
  FAIL	github.com/nick/gopi/cmd/agent [build failed]
  ```

  11 undefined symbols, every one a surface this branch adds. (An earlier attempt
  exported the tree with `git archive HEAD` **after** my own `git add` had run — it
  produced a green run because it contained my code. Re-done against `HEAD~1`.)
- **RED proof (Python)**: `DOCCLASS_CLASSIFY=<f7841805's classify.py> python3 scripts/test_docclass_scope.py`
  → the 13 new assertions FAIL (`--check` unknown flag, no gap reported, exit 2/1 mix).
- **`--check` md5 proof**: `md5sum devdoc/DOC-INDEX.tsv` unchanged across a clean run, a
  3-gap run and a duplicate-row run (asserted in the Python suite, observed above).
- **Thin-face proof** (asserted in `TestDocCheckFixWritesTheGapAndOnlyTheGap`): after
  `--fix`, running the repo's own generator on the same path produces a **byte-identical**
  row — `--fix` does not compose a row of its own.
- **The self-row defect I found and fixed**: `internal/docindex.Repair` rewrote the
  manifest's own `index-meta` row with `WritePathReason` (observed in the first test run:
  `before: index-meta: part of the corpus index itself…` → `after: index-meta: the
  truth-class manifest itself…`). `RepairableGaps` now drops `ManifestRel` by name, and
  the fix run reports `repaired 1 path(s)` (the document) where it previously wrote 2
  paths and 12 lines.
- **Rule 16 gate**: my first placement of `internal/docclass/shape.go`'s constants inside
  `docclass.go` shifted a *baselined* `%v`-wrap line and the gate went red naming it. Moved
  the constants to their own file → `go test -run TestGate1ErrorWrapRedProof ./internal/standards/` `ok`.
  (I did not edit the baseline.)
- **Pre-existing, untouched**: `gofmt -l cmd/agent internal/docindex internal/docclass` lists
  `chat_stream.go`, `cli_config.go`, `cli_voice_server.go` — not files this branch touched.

## The two readings — (A) doors are the defect, and the evidence

The briefing asked which reading the evidence supports. **It supports (A)**, and the
three symptom files reached disk by **two different doors, neither of which is fixed by
tuning the net**:

| # | file | door | evidence |
|---|---|---|---|
| 1 | `devdoc/handoff-20260926-chat-perception-verification.md` | **the hooked `write` tool — its own call ERRORED after the bytes landed** | session `a5e1c3e9…`, `[t+3023s]`: `Error writing …/handoff-20260926-chat-perception-verification.md: write: … timed out after 30s` |
| 2 | `devdoc/handoff-20260926-chat-self-observation-brain.md` | same | session `8c836970…`, `[t+3836s]`: identical `timed out after 30s` |
| 3 | `devdoc/proxy-banner-truthful.md` | **a person with an editor** — the document in one commit, its row in the next | `917f5cd5` (doc, author `qingzhe huang`) → `dcf1c0e3` (row, +1 line). The row has been present ever since; it was **not** missing at the repair commit. |

**Falsified hypothesis, recorded so nobody re-derives it.** I first read the missing row
as a *deletion* — a merge that unioned two writers. `git log -S` on the path shows only
`dcf1c0e3` (the add), and the `-devdoc/proxy-banner-truthful.md` line I had seen in
`83030f2d` (2026-09-18, "measure the class, then let the banner follow it") is part of that
commit's **whole-file rewrite** of the manifest — 683 `-devdoc/` lines across history,
i.e. every row, deleted and re-added as the class changed. There is no measured row
loss. The stale-permission shape my check reports (`row-with-no-file`) is real and
common, but it is not what happened here.

**Why (A) and not (B).** The net DID catch the incident — a standards test failed,
2452 rows for 2454 files — so the net is not blind; what it lacks is a **schedule and an
invocation**. It fires when someone runs the suite, not when a document lands, and it
offers no way to *ask the question* without running the whole test tree. The door that
produced files 1 and 2 is meanwhile **structural**: the hook runs *after* the write
(`ClassifyWrittenFile`'s ordering is deliberate — banner.py edits the file on disk), so
there is an unavoidable window in which the bytes exist and the classification does not;
any error there (a 30 s write timeout) leaves exactly this state. No net change closes
that window — only an invocable check plus the practice of running it does. That is what
this branch adds, and Rule 36 is the practice.

**Current uncovered-file count on this tree: 0.** `CheckCorpus` reports 0 gaps over
2471 rows / 2470 corpus files (the one non-row file is the manifest, which indexes
itself). All three symptom files carry well-formed rows.
