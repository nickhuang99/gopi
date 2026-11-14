<!-- doc-class: historical -->
# Task Report: `gopi task verdicts` — out-of-vocabulary verdicts must land in the UNKNOWN bucket

Branch: `feat/task-verdicts-unknown-bucket` (worktree `~/workspace/gopi-verdict-unknown`, base master `6a316b7`).

## Symptom

`gopi task verdicts` (`TaskReframeStats`, `internal/tools/reframe.go`) renders a header
total that counts **every** `task_events(event=reframe)` row, but the VERDICT
distribution table iterated only `append(taskverdict.All(), reframeVerdictUnknown)`.
`parseReframeVerdict` fell back to UNKNOWN only when the `verdict=` **field was absent**
(`v == ""`) — a token that was present but out of vocabulary (a retired
`NEEDS_RESEARCH`, or a corrupt `BANANA`) became **its own dist key**: counted in
`total`, invisible in the table. `sum(table counts) != header total` = a recorded row
silently dropped (Rule 15).

## Root cause (5-why)

1. Table rows missing while the total counted them → the table's key set was a **closed
   list** (vocabulary ∪ UNKNOWN), while `dist` was keyed by whatever the reader returned.
2. `dist` could hold a key outside that list → the reader returned the raw token.
3. The reader returned the raw token because it validated only **presence** of the
   `verdict=` field, never membership in the vocabulary.
4. Membership had a single source of truth available (`taskverdict.Valid`, the same gate
   the writer applies at `internal/agent/reframe.go`) but the reader did not consult it.
5. Norm: historical rows are **append-only and never re-validated** — a verdict retired
   after rows were written (NEEDS_RESEARCH, retired 2026-09-10), or a writer that
   bypassed the gate, leaves tokens on disk the current vocabulary no longer owns. The
   READER of such a trail must classify what it finds the way the writer would have.

## Completed

- `parseReframeVerdict` now returns `""` (the caller's UNKNOWN bucket) when the parsed
  token is non-empty **and** `!taskverdict.Valid(v)` — out-of-vocabulary tokens are
  classified through the vocabulary single source of truth (Rule 14), never by a mirrored
  spelling (Rule 1b/13).
- New `reframeVerdictTableOrder(dist)` + hardened render loop: every observed dist key is
  printed — vocabulary order (`taskverdict.All`) first, then the UNKNOWN bucket, then any
  residual key **sorted** for determinism. `sum(table counts) == total` holds **by
  construction**, independent of the reader (Rule 15).
- `reframeVerdictUnknown` documented as a RENDER-only bucket and pinned by test:
  `taskverdict.Valid("UNKNOWN")` must stay `false` (it is the harness wire gate at
  `internal/tools/reframe.go` `TaskReframeRecord` and `internal/agent/reframe.go`), and
  `taskverdict.All()` must not carry it either. `taskverdict.Valid` was **not** weakened.

## Not Done

- No change to `internal/agent/reframe.go` — the write gate already rejected these tokens
  (`TaskReframeRecord` → `taskverdict.Valid`); the defect was read-side only.
- No migration/rewrite of historical `NEEDS_RESEARCH` rows: the trail is append-only audit
  data, and the fix is precisely that the reader now renders them honestly.
- No unrelated failures were observed: the full `internal/tools` package suite (183.7s,
  which exceeds a single foreground bash call's 2-minute cap — hence the first invocation
  was cut and immediately re-run as a background job) and `internal/agent` (120.2s) both
  pass on this branch.

## Verification

**Build**
```
go build ./...                                   → BUILD OK
CGO_ENABLED=0 go build -o /tmp/verdict-bin/gopi ./cmd/agent/   → FIX BIN BUILT
```

**Tests** — `go test -count=1 -run 'Reframe' ./internal/tools/`: **7/7 PASS**
```
--- PASS: TestReframeRecord_StampsOriginPerWriter
--- PASS: TestTaskReframeRecordAndStats
--- PASS: TestTaskReframeRecordRejectsGarbage
--- PASS: TestTaskReframeStatsSurfacesMalformedDetail
--- PASS: TestReframeVerdicts_OutOfVocabularyLandsInUnknownEndToEnd   (new)
--- PASS: TestReframeVerdictTableOrder_NoCountedKeyIsInvisible         (new)
--- PASS: TestReframeVerdicts_GoldenRenderIsStable                     (new)
ok  github.com/nick/gopi/internal/tools  0.125s
```

**Whole-package suites** (background job, `-count=1 -timeout 20m`):
```
ok  github.com/nick/gopi/internal/tools   183.736s
ok  github.com/nick/gopi/internal/agent   120.225s
?   github.com/nick/gopi/internal/taskverdict  [no test files]
```
`gofmt -l internal/tools/` → clean; `go vet ./internal/tools/` → clean.

**The fix reddens its repro (Rule: a bug fix ships a minimal repro that actually fails
pre-fix).** Proof 1 — the new test run against the **pre-fix** tree (`/tmp/vd-prefix`,
master `6a316b7`, new test file copied in):
```
--- FAIL: TestReframeVerdicts_OutOfVocabularyLandsInUnknownEndToEnd
    no UNKNOWN row in the table — the out-of-vocabulary rows were dropped:
        4 reframe record(s) across 4 task(s)
        VERDICT       COUNT  ITEMS
        DOABLE            1      0
        TOO_BIG           1      3
```
(header total 4, table summed 2 — `NEEDS_RESEARCH`/`BANANA` keys unreachable.)

Proof 2 — the **pre-fix binary** on a scratch DB seeded with `verdict=NEEDS_RESEARCH
items=2`, `verdict=BANANA items=5`, `verdict=DOABLE items=0`:
```
PRE-FIX : 3 reframe record(s) across 3 task(s)   → table: DOABLE 1 only (sum 1 != 3)
POST-FIX: 3 reframe record(s) across 3 task(s)   → table: DOABLE 1, UNKNOWN 2 / items 7
                                                    (sum 3 == 3 ✓)
```
The out-of-vocabulary rows land in UNKNOWN; the table accounts for the header total.

**Commit**: see `git log --oneline -1` on `feat/task-verdicts-unknown-bucket`.

## Recovery

```bash
git log --oneline -3 feat/task-verdicts-unknown-bucket
git show feat/task-verdicts-unknown-bucket:devdoc/task-verdicts-unknown-bucket.md
```
Worktree `~/workspace/gopi-verdict-unknown` shares the main repo's object store, so the
commit is already on the branch in `~/work/GoPi` — no push required (Rule 6c).
