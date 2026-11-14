<!-- doc-class: historical -->
# Task Report: the anchor-number freeze table's question, asked on the landing path

Task `01a134fd-aedd-79ef-b9ea-d2b7676743aa` · branch `task/01a134fdaedd79efb9ead2b7676743aa`
Tip measured: `6ef428c16` · commit: `5fc3504fb`

## Completed

- **The mechanism, in the close-out step that already ticks** (Rule 3/35 — no new
  tool, no script, no resident dependency): `internal/docindex/anchor_drift.go`
  asks the freeze table's question on **every** landing, not only on a branch that
  adds a document, and `SyncBranch` (`internal/docindex/scope_git.go`) runs it
  **before anything is written** and propagates its verdict as
  `BranchSync.AnchorRefusal` / `.AnchorAbsent`.
- **The verdict reaches the chain**: `finishCleanupDocClassSync`
  (`internal/tools/task_finish_docclass.go`) turns a refusal into
  `finishCleanupPreMergeStatusRefused` with a sentence naming the finding, the
  table, and **both** writers (the corpus's own classifier for the cell, the
  generator for the table).
- **The arm refuses, it never re-derives** — the table is byte-identical after a
  refusal and carries no new permit. Measured reason: run by hand on this tip the
  generator answers `13 new permit(s)`, i.e. it **absorbs** the drift instead of
  stopping it, and a landing that permitted its own drift is exactly what the
  artifact exists to prevent (Rule 39's ratchet; Rule 38's single-writer rule).
- **Attribution is three-dot and the branch's own work**: a finding already
  incoherent at the branch's BASE is the integration branch's to answer and does
  not refuse this landing. The base is measured by `docclass`'s own probe (which
  materialises the base tree out of the object store with `git show` — never a
  checkout, Rule 29).
- **A tree with no freeze table abstains** (`AnchorAbsent`), stated rather than
  silently passed (Rule 15), while a table the arm cannot **read** refuses — an
  unasked question is never a pass.
- Tests: 4 arms in `internal/docindex/anchor_drift_test.go`
  (refuse / do not refuse what the base already had / release on a re-pointed cell
  / record an absent table) + 2 at the surface the chain calls
  (`internal/tools/task_finish_docclass_anchor_test.go`).

## Not Done (honest)

- **The two standing findings are NOT permitted, and `gopi tool doccheck` therefore
  still exits 1 on this branch's tip.** That is the pre-existing state of
  `master` (`6ef428c16`), **unchanged** by this branch — the deliverable here is
  the mechanism, not the two rows. Permitting them is a **ratchet-UP** the epic
  explicitly forbade as the fix, and the generator's own answer on this tip is
  `13 new permit(s)`, not 2 — a reviewer's decision, not a landing's.
  Concretely: the fix stops the **recurrence**; the two historical rows need one
  deliberate generator run with someone reading the 13.
- **The arm runs at the close-out of a `task/` run.** A landing that reaches the
  integration branch by another route (a hand `git push`, a foreign host's merge,
  a chat) is not covered — the same honest limit `gitPushbackSharesHistoryWithMaster`
  carries (Rule 6's step 7). The read-side gate (`doccheck`) remains the backstop.
- The `merge` path's union leg (`mergeset_docclass.go`) was **not** extended: the
  close-out leg is the one every dispatched run passes through, and adding a
  second call site would be a second answer to the same question.

## Verification

- **Build**: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → `BUILD OK`
  (also `go build ./internal/...`, clean).
- **Tests**: `go test ./internal/docindex/ -run AnchorDrift -count=1` →
  `ok github.com/nick/gopi/internal/docindex 7.569s`;
  `go test ./internal/tools/ -run DocClassStep -count=1` →
  `ok github.com/nick/gopi/internal/tools 8.839s`.
  Branch-scoped budget run of the changed package:
  `make test-budget PKGS='./internal/docindex'` →
  `stamp: 2026-10-13T19:58:16+08:00 … pkg=./internal/docindex budget=25m elapsed=28s exit=0 outcome=pass`
  (`artifacts/test-budget-docindex.txt`), with the two new arms listed in its
  per-test stream. The other touched package was run directly and green:
  `go test ./internal/tools/ -run 'DocClass|FinishCleanupDocClass' -count=1` → ok,
  `go test ./internal/tools/ -run 'FinishCleanup' -count=1` → ok (44.5s).
  The six machine gates stay green too:
  `go test ./internal/standards/ -run 'TestGate1…|TestGate6…' -count=1` → ok.
- **Ran** (verbatim, on the real corpus):
  - `./bin/gopi tool doccheck` → exit **1**:
    ```
    doccheck: anchor NUMBER coherence (recorded text still present, line number stale):
    doccheck:   NOT PERMITTED (drifts, absent from the baseline): devdoc/hp-push-topology-audit.md#996: anchor cmd/agent/cli_tasks_worker.go:638 at serveGitSyncSweep(workerRoleLabel) — number-drifted (recorded text is at :650)
    doccheck:   NOT PERMITTED (drifts, absent from the baseline): devdoc/scratch-gc-flight-join.md#1698: anchor cmd/agent/cli_worker_control.go:2167 at func (c workerDaemonCtx) installFileWatcher() (uninstall func()) { — number-drifted (recorded text is at :2183)
    doccheck: regenerate the table with:
      go run internal/docclass/testdata/anchor-number-census.go . internal/docclass/testdata/anchor-number-baseline.tsv
    doccheck: 0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv
    ```
  - `go run internal/docclass/testdata/anchor-number-census.go . internal/docclass/testdata/anchor-number-baseline.tsv --check`
    → exit **1**, `scanned 249 anchor row(s), 165 incoherent; table carries 163 row(s)`
    plus the same 2 `NOT PERMITTED` rows.
  - `go run internal/docclass/testdata/anchor-number-census.go . internal/docclass/testdata/anchor-number-baseline.tsv`
    → `249 anchor row(s) scanned, 165 incoherent (13 new permit(s), 152 pre-existing claim(s))` —
    the measurement behind "the generator would absorb the drift", `git restore`d
    in the same breath (below).
  - `./bin/gopi tool doccheck --fix` → exit **0**, and it REFUSES the class in its
    own words: `this run exits 1 on anchor NUMBER drift — the class --fix CANNOT
    write: it is DATA, regenerated only by the census command the report prints,
    and the same class the report arm refuses on`. It wrote the GAP half only (my
    new document's manifest row, committed with this report) — a second,
    independent confirmation that the number table has exactly one writer.
  - `./bin/gopi task finish-cleanup --dry-run --task 01a134fd-…` → the chain's own
    view of this branch: `refused=1 … not cleanable (terminal or cancelled required)`.
  - **The arm on the REAL corpus, with the two real landing commits as bases**
    (`artifacts/anchor-arm-realtree-probe.txt`, a temporary probe run against this
    worktree's 249 anchor rows and deleted in the same round):
    ```
    base=6ef428c16 scanned=249 incoherent=165 new=0 stale=0 absent="" refusal=""
    base=30350b652 scanned=249 incoherent=165 new=2 stale=0
      refusal="… 2 anchor claim(s) THIS branch's work made incoherent are absent from
      internal/docclass/testdata/anchor-number-baseline.tsv: devdoc/hp-push-topology-audit.md#997:
      anchor cmd/agent/cli_tasks_worker.go:638 … — number-drifted (recorded text is at :650);
      devdoc/scratch-gc-flight-join.md#1699: anchor cmd/agent/cli_worker_control.go:2167 … :2183) …"
    base=eabca844d scanned=249 incoherent=165 new=2 stale=0   (same two findings)
    ```
    Read against the history measured above, this is the mechanism proved end to
    end: with the two commits that actually INSERTED above those two cited sites as
    the branch's base, the arm refuses those landings **by name** — and with the
    current tip as the base it stays silent (both findings are the base's, not this
    branch's), which is the attribution working, not the arm failing.
  - **Result**: the arm's verdicts are reached and observable at both surfaces
  (`BranchSync.AnchorRefusal`, `finishCleanupPreMergeStatusRefused`), the freeze
  table is byte-identical after a refusal, and the real corpus still reports the
  same two findings it reported before the change — i.e. the change is a
  behaviour **added on the landing path**, with no mutation of the artifact.
- **Commit**: `5fc3504fb`.

### Red proof (and its restore)

Deleting the arm's wiring leaves both tests failing **exactly** as the pre-fix
world behaved (the red proof was produced by making the arm answer "nothing to
report" — the pre-fix behaviour, since no landing path asked the question at all):

```
--- FAIL: TestAnchorDriftArmRefusesALandingThatMovedAPublishedNumber
    a landing that moved a published number with no permit must be REFUSED, got
    {… AnchorAbsent: AnchorRefusal: Abstained:}
--- FAIL: TestAnchorDriftArmRecordsAnAbsentFreezeTable
    the abstention must be STATED, never a silent pass (Rule 15)
--- FAIL: TestDocClassStepRefusesALandingThatMovedAPublishedAnchorNumber
    a landing that moved a published anchor number with no permit must be REFUSED, got
    {Status:verified Detail:the devdoc corpus this branch adds is already classified}
```

Restore verified with an explicit revision (an index-relative diff is not
evidence of a restore, Rule 5): `git diff HEAD --stat` reports only the arm's own
`38 insertions(+)` in `scope_git.go`, `grep -c REDPROOF` is 0 in both files, and
both suites are green again. The freeze table was restored the same way and
verified **index-free**:
`test "$(git hash-object <table>)" = "$(git rev-parse HEAD:<table>)"` → true.

## Evidence (self-filed)

- `git show eabca844d:cmd/agent/cli_worker_control.go | grep -n 'func (c workerDaemonCtx) installFileWatcher'`
  → `2167` — and `eabca844d`'s own `DOC-INDEX.tsv` cell says `:2167` ⇒ that cell
  was **correct where it landed**.
- `git show ba7592966:cmd/agent/cli_worker_control.go | grep -n …` → `2183`; its
  hunk is `@@ -2164,26 +2164,60 @@` ⇒ a **different** branch's insertion moved it.
- `git show 30350b652:cmd/agent/cli_tasks_worker.go | sed -n 638p` →
  `serveGitSyncSweep(workerRoleLabel)`; the cell stamped in that same commit says
  `:638` ⇒ also **correct where it landed** (later landed below → `:650`).
- `go run … --check` exit 1 with stdout 614 B / stderr 163 B ⇒ the answer is on
  both streams (the runner's stderr is `exit status 1` plus the summary line).
- `git status --porcelain` before the final commit: only the five changed paths,
  all tracked; no untracked litter.

## Correction to the record (Rule 41 — evidence over inference)

An earlier reading of this task held that a cell is "**born wrong**" (that the
re-stamping writes an already-drifted number in the same landing). **Both landing
commits refute that**: each cell is exact in the tree that stamped it, and the
drift arrives from a *later, unrelated* landing. The measured shape is
`insert-above-the-cited-site, elsewhere in the tree` — which is why the fix
belongs on the landing path and not on the author's diligence.
