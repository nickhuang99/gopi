<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# Task Report: the truth class of `devdoc/cli-help-user-audit.md`, and the two floors that outlive it

Task `01a11478-bd20-7652-b155-19727aafbf64` (2026-10-07, peerbox). One defect, one
commit: master's `TestDocClassBannerMatchesManifest` was RED because this document
carried the `historical` class banner while its `devdoc/DOC-INDEX.tsv` row classed
it `normative` — both halves landed together in `ac35c924f` and were never
re-derived.

## Completed

- **The class was re-derived, not chosen.** Direction: the doc is **historical**,
  and the ROW was the stale half. `tools/docclass/classify.py` (the corpus's own
  classifier, the authority `tools/docclass/classify.py`'s docstring and
  `internal/docclass/docclass.go` both point at) independently returns
  `historical` for this path, with the reason *"the doc's own title/name declares a
  past run's record, and no current artifact cites it as authority"*.
- **The row was written by the writer, never by hand** (Rule 36):
  `python3 tools/docclass/classify.py . --only devdoc/cli-help-user-audit.md` →
  `1 updated`, `2086 rows`. `git diff --numstat` on the manifest: `1 1`.
- **The doc's own class declaration was re-stated** — the paragraph that said
  `Truth class: **normative** — a standing claim about master, each row re-runnable
  at any commit` now says what the re-derived class is, names the commit the rows
  were measured at, and states that only the classification line changed. The
  run's account (method, table, findings, fix, Not Done, Verification) is
  **untouched** — no past conclusion was rewritten.
- **The two surfaces agree**, and the banner needed no edit: the file already
  opened with the `historical` banner, which is what the re-derived class requires
  (`internal/docclass.ClassBannered` bannes exactly `historical`).

## Why the row was the stale half — the evidence

`ac35c924f` wrote the row and the file together, and the file has carried the
`historical` banner since that commit, so the file cannot be the half that went
stale: the row contradicted a banner written in the same commit. What that row's
reason cell actually asserts is a property of the **repair**, not of the document:
its spot-check (`path cmd/agent/usage_test.go`) resolves, but nothing in that file
cites the document — `grep -n 'merger' cmd/agent/usage_test.go` shows the merger
row's ABSENCE pinned by an anchored regex and this doc named in neither form. The
only reference to the path in the tree is the row that classifies it
(`grep -rn 'cli-help-user-audit'` outside `DOC-INDEX.tsv` → two sibling devdocs
that argue about the same mismatch). A `path` tier that resolves proves the named
file still exists; it does not make this document a standing claim about master.

The precedent for exactly this repair is in the tree, by the same tool:
`ac29d0a6b` dropped the stale `historical` banner from
`devdoc/task-payload-revision.md` because the classifier re-derived `normative`
and a LIVE citation backed it (`internal/tools/tasks.go`'s `TaskSetPayload` doc
comment points a maintainer at that document). This document has no such citation
and no such re-derivation.

**The reverse repair is unreachable, measured, not assumed** — the sequence a
reader would expect if the row were the deliverable:
`python3 tools/docclass/banner.py . --only …` (with the row still `normative`)
*removes* the banner (`0 applied, removed from 1`), and the gate then goes red the
OTHER way (`banner.py --check`: *"no class banner; the manifest classes it
normative"*) until a hand edit re-adds the banner the writer just removed.

## Re-measured on the merged base (master had moved 15 commits ahead)

The numbers below were re-taken **after** `git merge master` (`6dfe21dda`, master
tip `485881ab4`) because the first pass ran on a 15-commit-old base — a green gate
measured before an alignment is not a green gate. The merge moved `devdoc/DOC-INDEX.tsv`
(append-only row union lane, 9 lines) and added three docs, so every count is
restamped here; **the two surfaces still agree** and the epic's gate is still PASS.

- epic gate, merged base: `--- PASS` / `ok ... 0.237s`, log line
  **`1170 markdown docs of a bannered class carry their banner (0 mislabelled)`**
  (was `1167` pre-merge; master's own control: `1168 … (1 mislabelled)`, FAIL).
- `classify.py . --check` (merged base) → `0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv`.
- `gopi tool doccheck` (merged base) → `doccheck: the devdoc corpus is consistent —
  every file under devdoc/ has a well-formed row`, exit **0**. **Spelling note
  (measured):** the bare verb is RETIRED — `gopi doccheck` prints *"「doccheck」 is
  RETIRED — use `gopi tool doccheck`"* and exits **1**; the working door is
  `gopi tool doccheck`.
- `go test ./internal/standards/ -count=1` (merged base) → FAIL with **four** gates:
  the two `TestCommandsDoc*`, plus `TestStandardGates` (its Rule-20 sub-red,
  `cmd/agent/cli_tool_spelling_table.go:170 isUnderscoreException` — a file that
  arrived ON MASTER in the merge) and `TestStandardGateDataReport`. **All four
  reproduce, reason-for-reason, on the pristine-master control arm** — including the
  Rule-20 site by name — so the merge imported master's own reds and added none.
- `go test ./internal/agent/ -run 'DocAnchor|DocAnchorCoverage' -count=1` (merged
  base) → FAIL, `3005 across 663 normative docs`, `132 new, 15 stale permits`; the
  control arm reads `3005 across 664`, `132 new, 15 stale permits` with the **same
  15 stale permits and the same 132 drift diagnostics** — the doc count differs by
  exactly ONE, the doc this change moved out of the `normative` class, and the error
  set is unchanged. (The anchor test ALSO fails on `master`, so it is not this
  epic's gate: it was already red before this branch existed.)
- **Control-arm caveat, measured:** `git archive` exports have no `.git`, so four
  further `TestRepoHygiene*` / `TestRepoRootJunk*` gates fail there that pass here.
  A control arm can only be read for gates that do not require the repository — the
  four I attribute to master were each checked to be repository-independent.
  `TestCapdemoExternalBundleAnchorsResolve` and `TestCensusGateJudgesTheRealCorpus`
  fail on the CONTROL ONLY: they are clean on this branch (artifacts absent from a
  `git archive`), the opposite of an attribution to master.

## Not Done (honest)

- **A second instance of the same defect exists and is NOT fixed here.**
  `devdoc/chat-status-hitrate-two-readings.md` carries a `normative` banner on line
  3 (a blank title line above it) while the classifier re-derives `historical` for
  it (`5 standing / 2 record` — two hits from the doc's own `measured on` / `verdict`
  prose, near the `recordy` threshold). Its banner is invisible to the Go gate (the
  gate reads line 1) but **not** to `banner.py --check`, which reports it on master
  and on this branch. Repairing it changes a second document and a second manifest
  row, which is not this epic's stated defect; falsifying a second run's
  classification line to make a side-check green is the thing this epic forbids.
  It is filed as its own pending row instead.
- **`go test ./internal/standards/` is NOT wholly green — and EVERY red in it fails
  identically on pristine master.** Pre-merge that was two gates
  (`TestCommandsDocGeneratedBlockMatchesSource` and
  `TestCommandsDocGateRedProofStaleGeneratedBlock`, the latter's own precondition
  saying *"the committed document's block is already stale"*); after the alignment
  it is four, because the merge imported `TestStandardGates` (the Rule-20
  `isUnderscoreException` site) and `TestStandardGateDataReport` from master's own
  tip. Measured with a control arm: `git archive master | tar -x -C
  /dev/shm/master-ctl-dwXf` → the same four FAIL there, reason for reason, and
  `TestDocClassBannerMatchesManifest` FAILs there too. Pre-existing, unrelated, and
  filed on the existing row for master's red gates. I did not "fix" them:
  regenerating a verb census is another epic's subject (Rule 26 — the honest
  partial, dispatched, not overstated).
- **`internal/agent`'s anchor gate is red too, and it is not this change's** — see
  the evidence arm below.

## Verification

- **Build:** `CGO_ENABLED=0 go build -o /dev/null ./cmd/agent/` → `BUILD_OK`; the
  binary the `Ran:` line used was built the same way into this run's artifacts dir.
- **Tests (pre-merge pass; the restamped numbers are in the section above):**
  - `go test ./internal/standards/ -run 'TestDocClassBannerMatchesManifest' -count=1 -v`
    → `--- PASS` / `ok github.com/nick/gopi/internal/standards 0.307s`, log line
    `1167 markdown docs of a bannered class carry their banner (0 mislabelled)`
    (master: `1 mislabelled`, FAIL).
  - `go test ./internal/standards/ -count=1` → exit 1, exactly the two
    `TestCommandsDoc*` failures above, both reproduced on pristine master.
  - `go test ./internal/agent/ -run 'DocAnchor' -count=1` → exit 1 with 408
    diagnostics; on pristine master: 408 diagnostics too.
- **Ran:** the corpus's own two writers, incrementally, on the named path (Rule 31 —
  the surface this change moves is the manifest and the gate that reads it):
  - `python3 tools/docclass/classify.py . --only devdoc/cli-help-user-audit.md` →
    `{"updated": 1, "rows": 2086}`; `--check` afterwards → `0 gap(s)`.
  - `python3 tools/docclass/classify.py . --dry-run --only devdoc/cli-help-user-audit.md`
    → still `historical`, so the committed row is the generator's own output and not
    a hand-written value.
  - `<session>/artifacts/gopi tool doccheck` → `the devdoc corpus is consistent —
    every file under devdoc/ has a well-formed row`, exit 0.
  - `python3 tools/docclass/banner.py . --check` → 1 mismatch
    (`chat-status-hitrate-two-readings.md`, the sibling above), exit 1 — identical
    verdict and count on the pristine-master control.
- **Result:** the two surfaces agree (first line `historical`, row `historical`),
  the epic's gate is green, `classify.py --check` is 0 gaps, and every remaining red
  in the neighbourhood was **measured on master's own bytes** through a control arm
  rather than assumed to be pre-existing.

## Evidence (self-filed)

- **The control arm** (a pristine master export, not a claim): `git archive master |
  tar -x -C /dev/shm/master-ctl-dwXf` (off `/`, Rule 22) → in that tree
  `go test ./internal/standards/ -run 'TestDocClassBannerMatchesManifest|TestCommandsDoc'`
  → FAILs on all three, while the same command in this worktree FAILs on the two
  `TestCommandsDoc*` only. The red-to-green transition for the epic's gate is
  therefore measured on both sides, not inferred. Re-run after the alignment
  (`/dev/shm/master-ctl-final`): `TestDocClassBannerMatchesManifest` FAIL
  (`1168 … 1 mislabelled`) vs PASS here (`1170 … 0 mislabelled`), and the four reds
  I attribute to master carry the same reasons on both sides, down to
  `cli_tool_spelling_table.go:170 isUnderscoreException` and the `%v` escape at
  `internal/tools/worktree_sweep.go:2431`.
- **`internal/agent`'s diagnostics are identical in both arms** — the control's log
  and this branch's log, reduced to their diagnostic lines, agree on all 15 stale
  permits and all 132 new drift entries; only the scope line's doc count differs
  (`663` here vs `664` on master) and that gap is exactly the one document this
  change moved out of the `normative` class. The anchor count (`3005`) is identical.
- **The doc's own account was not rewritten:**
  `git diff --stat devdoc/cli-help-user-audit.md` → the body edit is confined to
  the four-line classification paragraph; `git diff` shows no other body line
  changed.
- **The manifest change is exactly one row:** `git diff -- devdoc/DOC-INDEX.tsv`
  → one `-`/`+` pair, both for `devdoc/cli-help-user-audit.md`.
- **The generator's verdict is stable across the body edit:** the dry-run after the
  classification paragraph was rewritten returns the same class and the same
  `7 standing / 2 record` counts as before it, so the row was not fitted to the
  new prose.
