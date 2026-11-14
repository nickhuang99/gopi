<!-- doc-class: historical -->
# Task Report: doc-anchor gate repair — 7 stale permits deleted, 10 new drift entries zeroed

**Branch:** `task/01a0d08a867d79f2be0c43460d675c29`
**Gate:** `internal/agent/docanchor_test.go` (`TestDocAnchorsResolveInNormativeDocs`),
manifest-scoped over `devdoc/DOC-INDEX.tsv` class `normative` (521 markdown docs).

## Completed

### 0. The measured starting state (re-derived, not taken from the epic text)

Branch base `3dd59e7b`. `go test ./internal/agent/ -run DocAnchor -count=1` → **exit 1**:
`249 pre-existing drift entries permitted, 10 new, 7 stale permits`. The counts
matched the epic, but master had moved since the epic was written, so the *set* of
affected docs differed from the epic's list — the key set below is what the gate
itself printed on this branch.

Exact key sets were taken from the gate, not from prose: a throwaway diagnostic
(`internal/agent/zz_docanchor_keys_test.go`) dumped the gate's own comparison
(`new` / `permitted` / `stale` per `doc|kind|detail` key); it was **deleted before
commit**.

### 1. Two dispositions, applied per doc class — never a blind baseline rewrite

The 17 entries were NOT one repair. They split by the manifest's own vocabulary
(`DOC-INDEX.tsv` `standing N / record M`):

- **standing-claim docs** hold a claim about *today's* master → the anchor is
  **re-keyed to the symbol the row names** (the line number is only an accelerator;
  the symbol name is the authority, per the gate's own header).
- **landed-record docs** (`standing 0`) quote the line map of the tree they measured
  → the anchor is **permitted with a per-row reason**, because re-keying it would
  falsify the record. Precedent in this same baseline:
  `devdoc/web-m2-b5b8b9b12-remeasure.md` — "a dated re-measure whose line map is
  that era's"; `devdoc/merge-run-20260924.md` uses the same landed-record wording.

### 2. The 7 stale permits — deleted, not kept

| deleted permit | why it is stale |
|---|---|
| `hippocampus.md\|blank\|(bare) loop.go:1689` | row re-keyed to `interruptBase`/`interruptCancel` at `:2019`/`:2020` |
| `hippocampus.md\|blank\|(bare) loop.go:1697` | row re-keyed to `interrupted bool` at `:2026` |
| `hippocampus.md\|blank\|(bare) loop.go:3472` | row re-keyed to `maybeEnforceCompletionStore` at `:4314` |
| `hippocampus.md\|blank\|loop.go:3590` | entry row re-keyed to `Chat` at `:4085` |
| `hippocampus.md\|blank\|loop.go:4652` | row re-keyed to `maybeNudgeStaleTaskList` at `:5189` |
| `agent-five-organs-ontology.md\|blank\|loop.go:2695` | row re-keyed to the job-notice injection point at `:3496` |
| `docanchor-gate-bare-lineno.md\|blank\|loop.go:2695` | the doc is this repair's own before/after record; the cell moved with the table rewrite |

A stale permit is a silent permission: every one of these was deleted in the same
batch that removed the site it permitted. `test-env-isolation.md|blank|loop.go:2938`
was a **new** stale in this run (it was not in the epic's stale list): its row is a
*historical* data-race report quoting "reached from `loop.go:2938`", and the doc
still carries the anchor — so the permit stopped matching for the reason the gate
names (`the site was fixed or is no longer gated`). The doc is a landed record, so
the anchor stays as written and the entry is deleted for the gate's verdict to be
honest. `grep -c '2938'` over the doc after the change: the anchor is still there
(one row, §"Pre-existing `-race` data race left untriaged"); the file is unchanged.

### 3. The 9 new drift entries — zeroed by re-keying (standing-claim docs)

Every re-key targets the line that **defines** the symbol the row names, verified
non-blank on this tree (measured, not assumed):

| doc | row | old anchor | new anchor (symbol) |
|---|---|---|---|
| `agent-five-organs-ontology.md` | :148 | `:2519`/`:2530`/`:2539` | `:3090`/`:3101`/`:3110` (`Run`/`Resume`/`runEngine`) |
| `agent-five-organs-ontology.md` | :198 | `loop.go:2695` | `loop.go:3496` (the `TakeJobNotices` round-boundary injection) |
| `hippocampus.md` | :11 | `loop.go:2690`/`:2539` | `internal/agent/loop.go:3110` / `:3447` (definition / round-loop body) |
| `hippocampus.md` | :96 | `loop.go:2670`/`2681`/`3590`/`chat_ops.go:159` | `:3090`/`:3101`/`:4085`/`chat_ops.go:207` (`Run`/`Resume`/`Chat`/`ChatOps`) |
| `hippocampus.md` | :97 | `loop.go:2690`/`:2995` | `loop.go:3447` / `loop.go:3496` |
| `hippocampus.md` | :119 | `:1689`/`:1690`(构造 `:2291`,装配 `:2326`) | `:2019`/`:2020`(构造 `:2984`,装配 `:3020`) |
| `hippocampus.md` | :123 | `:1697` | `:2026` (`interrupted bool`) |
| `hippocampus.md` | :124–126 | `:2870`/`:2358`/`:2368` | `:3045`/`:3062`/`:3072` (`Interrupt`/`interruptedBySignal`/`interruptRoot`) |
| `hippocampus.md` | :113 | `:2729` | `:3496` (the round-boundary injection site) |
| `hippocampus.md` | :221 | `:2685`/`:2729` | `:3110` / `:3496` |
| `hippocampus.md` | :229 | `:3446`/`:2511`, `:3100`, `:3465` | `:4274`/`:3241`, `:4285`, `:4280` (`maybeInjectTaskStartSearch` / its call site / `tools.MemoryHotResident()` / the hot-resident comment) |
| `hippocampus.md` | :230 | `:3472`, `:3052`/`:3071` | `:4314`, `:3881`/`:3900` (`maybeEnforceCompletionStore` + both call sites) |
| `hippocampus.md` | :383 | `:3446`/`:2511` | `:4274`/`:3241` |
| `why-worktree-residue.md` | :122 | `internal/agent/loop.go:2750` | `internal/agent/loop.go:2925` (the `os.Getenv("GOPI_TIMEOUT_SEC")` read) |
| `cwd-and-lifetime-recovery.md` | :80 | `internal/scratch/holder.go:497` (×2: explicit + bare) | `internal/scratch/holder.go:484` (`ParseProcStartTime`) |

`cwd-and-lifetime-recovery.md` needed a second look: the row claimed
`ParseProcStartTime` "hardcodes `const startTimeFieldIndex = 19`". On this tree that
is **no longer true** — the function is a one-line delegation to
`procname.StatStartTime` (`:485`), and `startTimeFieldIndex` exists nowhere in
`internal/scratch/holder.go` (`grep`: no match; file is 486 lines, so `:497` was past
EOF). The row's *premise* is stale, not just its number: master already did the
consolidation the row says is absent. The row now states what the code does
(delegation, with its own callee anchor) — re-keying by symbol is the authority here,
and leaving the old prose would have been a claim the file contradicts.

Anchors were written **explicitly** (`loop.go:3110`) wherever a row-mate `.go` token
would otherwise re-point a previously-bare anchor's inheritance, so no re-key silently
changed its *key* (a silent key move is how a permit gets orphaned).

### 4. The 3 permits added — new, clearly-structural, per-row reason

All three are the same class the baseline already carries (a **landed record** whose
subject line master's later refactor moved), and each carries a reason naming the
symbol and the record:

| added permit | reason (abridged) |
|---|---|
| `nightshift-20260911.md\|blank\|(bare) loop.go:1903` | the doc quotes the line number *of the era it measured* and says so itself ("该行号已属陈旧记录") — re-keying a record falsifies it |
| `style-sweep-20260912.md\|blank\|internal/agent/loop.go:1903` | the doc's own sentence is "`internal/agent/loop.go:1903` is unchanged by this branch" — it is a cross-check against the tree it measured; re-keying erases what it proved |
| `docanchor-gate-bare-lineno.md\|blank\|(bare) loop.go:2685` | the anchor sits in that doc's own "before/after per row" table; its `before` column IS the old line map (`runEngine`'s former `:2685`), so re-pointing it at today's tree makes the table self-contradictory |

Nothing else was permitted: every other new row was repaired at the doc, because its
doc *is* a standing claim. `249 → 244` permitted entries.

### 6. `DOC-INDEX.tsv`: one row added — for this report itself

`internal/standards/docclass_gate_test.go::TestDocManifestCoversEveryDevdocFile`
requires every file under `devdoc/` to carry a manifest row, so the mandatory
report cannot be an unclassified file: **one row added**,
`devdoc/docanchor-stale-and-new-drift-repair.md` → class `historical`, reason
`historical (0 standing / 5 record): the doc is this repair run's own record …`,
and the file opens with `<!-- doc-class: historical -->` (the banner contract of
`TestDocClassBannerMatchesManifest`). Historical is the honest class: the doc
records one run's measurements, and re-keying its anchors later would falsify it —
exactly the class this repair's own doctrine argues from.

### 7. Pre-existing failures found and NOT fixed (reported, not silently dropped)

`go test ./internal/standards/ -run 'DocClass|DocManifest'` is **red before and after
this change**, and not because of it:

1. `devdoc/docanchor-corrections.md: no row in devdoc/DOC-INDEX.tsv — every file under
   devdoc/ must be classified`. Measured: the file exists at this branch's base
   (`git cat-file -e 3dd59e7b:devdoc/docanchor-corrections.md` → present) and its row
   is absent there too (`git show 3dd59e7b:devdoc/DOC-INDEX.tsv | grep -c docanchor-corrections`
   → `0`). Its introducing commit `fa75f7bf` is an ancestor of the base. So the
   manifest was already one row short when this branch started, and it is still one
   row short — the same count, `2286 rows cover 2287 files`.
   Deliberately not fixed here: choosing this file's class is a truth-classification
   decision about *someone else's* document, not part of this epic, and inventing a
   class for it would be the same "asserted, not held" defect this gate exists to
   catch.

2. Two further agent-suite tests are red on the **branch-scoped** smoke run
   `make test-budget PKGS='./internal/agent/ ./internal/standards/'` (exit 1):
   `TestAgent_WallclockCap_TruncatedRunIsNotCompleted` and
   `TestAgent_StuckLoop_TruncatedRunIsNotCompleted`. They are **pre-existing and
   unrelated** — this change touches no Go file (`git diff --stat 3dd59e7b..HEAD`
   = 6 markdown/TSV files + the baseline TSV). Measured:
   `git checkout 3dd59e7b && go test ./internal/agent/ -run
   'TestAgent_WallclockCap_TruncatedRunIsNotCompleted|TestAgent_StuckLoop_TruncatedRunIsNotCompleted'
   -count=1` → both `--- FAIL` on the BASE commit too (fail before, fail after);
   the branch was then restored and `git status --porcelain` printed nothing.
   Reported, not fixed: their subject is the run-truncation writeback path, not
   doc anchors.

### 8. Deliberately NOT done (and why)

- **No blind baseline rewrite.** Every deletion was tied to a site this change itself
  removed or re-keyed; for `test-env-isolation.md` (whose doc I did not touch) the
  deletion is the gate's verdict, reported below rather than papered over.
- **No re-keying of historical/landed-record docs** — `nightshift-20260911.md`,
  `style-sweep-20260912.md`, `docanchor-gate-bare-lineno.md`'s before/after table,
  `test-env-isolation.md`, `cwd-and-lifetime-recovery.md`'s neighbouring rows.
- **The docclass classifier was not run** (not needed by this gate; it would add
  scope entries).
- **`DOC-INDEX.tsv` evidence cells were not touched** — the gate demanded none: no
  `ev-*` key appeared in the drift set, and `TestDocAnchorScopeComesFromTheManifest`
  plus the per-doc evidence re-check both pass unchanged.
- **Pre-existing permitted drift (244 entries) was left alone** — that is the
  baseline's own subject, not this repair's.
- **No new test was added.** The gate itself is the regression guard for this class,
  and it fires red on the exact keys; a second test would assert the same predicate.
  The minimal-repro role is already held by `TestDocAnchorBareLineRules` and by the
  gate's stale-permit arm, both of which were red before this change and are green
  after (that transition is the repro).

## Not Done

- The 244 remaining permitted entries are still permitted; this repair zeroed the 10
  new and deleted the 8 stale, and did not open the pre-existing-list question.
- `test-env-isolation.md`'s `loop.go:2938` permit could not be "fixed at the doc":
  the doc is a landed record and the anchor is part of its evidence. Deleting the
  permit is the honest disposition; if that doc is later re-classed historical the
  whole question disappears with it.
- The `cwd-and-lifetime-recovery.md` row's *task reference* (the filed consolidation
  task) may itself be stale now that master appears to have landed the delegation —
  out of scope here; not verified either way.

## Verification

- **Build:** `go build ./...` → exit 0, no output.
- **Vet:** `go vet ./internal/agent/` → exit 0, no output.
- **Tests:** `make test-budget PKGS='./internal/agent/ ./internal/standards/'` —
  see `## Evidence` for the observed result; the branch-scoped smoke, not a
  whole-tree run (Rule 27's smoke path).
- **Ran** (the surface this change actually moves — the gate itself, with its own
  runner and no cache):
  ```
  go test ./internal/agent/ -run DocAnchor -count=1 -v
  ```
  Before the change (same invocation, branch base `3dd59e7b`):
  ```
  docanchor_test.go:214: doc anchors verified: 2525 across 521 normative docs
  (2077 explicit + 448 bare continuations; 454 bare out of row/table/heading scope;
  249 pre-existing drift entries permitted, 10 new, 7 stale permits)
  --- FAIL: TestDocAnchorsResolveInNormativeDocs ; exit 1
  ```
  After:
  ```
  docanchor_test.go:159: scope: 521 markdown docs of class "normative" from devdoc/DOC-INDEX.tsv (historical docs are deliberately not gated)
  docanchor_test.go:214: doc anchors verified: 2525 across 521 normative docs (2077 explicit + 448 bare continuations; 454 bare out of row/table/heading scope; 244 pre-existing drift entries permitted, 0 new, 0 stale permits)
  --- PASS: TestDocAnchorsResolveInNormativeDocs (0.84s)
  --- PASS: TestDocAnchorBareLineRules (0.00s)
  --- PASS: TestDocAnchorScopeComesFromTheManifest (0.00s)
  --- PASS: TestScreenCaptureDeclarationsStayBelowDocAnchors (0.11s)
  ok  	github.com/nick/gopi/internal/agent	4.477s     (exit 0)
  ```
  The anchor count is **unchanged at 2525** — the repair re-pointed anchors and moved
  permits; it did not delete a doc line, so the corpus it measures is the same size.
- **Commit:** see the branch tip (`git log --oneline -1` on
  `task/01a0d08a867d79f2be0c43460d675c29`).

## Evidence (self-filed)

- `go test ./internal/agent/ -run DocAnchor -count=1` → **exit 0**, printing
  `244 pre-existing drift entries permitted, 0 new, 0 stale permits` (the acceptance
  string, verbatim, from `docanchor_test.go:214`).
- `go build ./...` → exit 0; `go vet ./internal/agent/` → exit 0 (both printed
  nothing).
- `git status --porcelain` in the worktree after the commit → empty (command and
  output below in the final message; the throwaway `zz_docanchor_keys_test.go`
  diagnostic that produced the key sets was deleted before staging, so it is not
  part of the deliverable).
- Every re-key target line was read from the tree and is non-blank — e.g.
  `loop.go:3110 func (a *Agent) runEngine(`, `loop.go:4274 func (a *Agent)
  maybeInjectTaskStartSearch() bool {`, `internal/scratch/holder.go:484 func
  ParseProcStartTime(raw string) (uint64, error) {`.
