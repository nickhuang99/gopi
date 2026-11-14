<!-- doc-class: historical -->
# Task Report: Close function-level test coverage for `internal/hippocampus` (6 uncovered → 0)

**Branch:** `feat/cov-hippocampus-hp` · **Worktree:** `~/workspace/gopi-cov-hippocampus-hp`
**Base:** `bea4ad4` (master) · **Charter:** tests only — no product code modified.

---

## Completed

- Read every target source in full: `types.go`, `host.go`, `engine.go`, `config.go`,
  `brief.go`, `decision.go`, plus the two existing test files (`engine_test.go`,
  `config_test.go`) and the metric script `scripts/func_coverage.py`.
- Mapped every target function's definition, its callers, and its dependent types with
  `code_search` + `keyword_search` (evidence table below).
- Wrote **one** new test file, `internal/hippocampus/cov_hippocampus_test.go`
  (10 test functions, 30 named `t.Run` cases), closing all 6 uncovered functions with **real
  calls + real assertions** — happy path, boundaries (nil/zero/empty/max/non-positive),
  and error paths per function. Helper prefix `covHipp*` (merge-collision guard).
- Verified: full package suite green (3 consecutive runs), `func_coverage.py` 100.0%
  (0 uncovered), `CGO_ENABLED=0 go build ./cmd/agent/` OK, `go vet` clean.
- Filed the real defect found during the mapping (bug task `01a08c75-9d17-714a-9cb4-c2114ac0da79`).
- Stored 2 memory records (id 1707, 1708).

## Not Done

- **No product-code fix.** The defect below is filed, not fixed — the charter is tests-only
  (Rule 4 surgical edits allowed, brief says do not modify product code).
- `NewWithConfig` was left **out** of the new file on purpose: it was already honestly covered
  by `engine_test.go:76` and a mention-only test would have been vacuous padding.
- Rule 1b pre-existing raw literals in `engine_test.go` (`"cold"`, `0.4`, `"GOPI_MEMORY_DB"`)
  were **not** touched (test-only slice; would be churn in a shared file). The new file
  references `tools.TierCold` / `SourceTag` / `DefaultRetrievalLimit` / `HostEngine*Timeout`
  instead. The one literal the product forces is stated under "Constants" below.
- `make fmt` not run (explicitly forbidden by the brief). `gofmt -w` was run on the new file only.

---

## Source inventory read (RAW)

```
  266 internal/hippocampus/brief.go        (read: Brief + BriefFromResult)
  191 internal/hippocampus/brief_test.go
  157 internal/hippocampus/config.go       (read in full)
  133 internal/hippocampus/config_test.go
   28 internal/hippocampus/decision.go     (read in full)
  431 internal/hippocampus/engine.go       (read in full)
  580 internal/hippocampus/engine_test.go  (read in full)
  119 internal/hippocampus/host.go         (read in full)
  132 internal/hippocampus/types.go        (read in full)
 2037 total
```

## `func_coverage.py` semantics (read, not assumed)

`scripts/func_coverage.py` matches `^func\s+(?:\([^)]*\)\s+)?([A-Z]\w*)\s*\(` and counts a
function covered iff its **name appears** in a same-package `*_test.go` (comments included).
So the metric is inflatable by a bare mention — the tests below therefore each assert real
observable behaviour, and the per-function **call-site counts** in the new file are printed in
the verification section as proof there is no mention-only coverage.

**Red → green transition (raw):**

```
BEFORE:  internal/hippocampus   14   8   57.1%
  Uncovered exported funcs (by package):
    internal/hippocampus (6):
      - Feedback
      - Get
      - NewHostEngine
      - Search
      - Store
      - Update

AFTER:   internal/hippocampus   14  14  100.0%
  Uncovered exported funcs (by package):
  (empty)
```

---

## Plan vs. reality — the briefing's uncovered set was only half the story

The 6 uncovered names are not 6 independent functions; 4 of them are **one-line forwarding
adapter methods** on `ToolsMemory`, and the other 2 are a host constructor and its adapter.

| # | Uncovered | Definition (file:line) | Kind |
|---|-----------|------------------------|------|
| 1 | `ToolsMemory.Search` | `internal/hippocampus/types.go:35` | adapter → `tools.MemorySearchHits` |
| 2 | `ToolsMemory.Store` | `internal/hippocampus/types.go:40` | adapter → `tools.MemoryStore` |
| 3 | `ToolsMemory.Get` | `internal/hippocampus/types.go:45` | adapter → `tools.MemoryGet` |
| 4 | `ToolsMemory.Update` | `internal/hippocampus/types.go:48` | adapter → `tools.MemoryUpdate` |
| 5 | `ToolsMemory.Feedback` | `internal/hippocampus/types.go:53` | adapter → `tools.MemoryFeedback` |
| 6 | `NewHostEngine` | `internal/hippocampus/host.go:107` | production-binding constructor |

Only 6 were uncovered, but the package has **14** exported functions — 11 real functions and 3
struct receivers (the metric counts the `ToolsMemory` *type* plus its 5 methods as 6 entries).
The new file exercises the whole surface so the 100% rests on explicit call sites, not on
`New`/`Context` being reached incidentally through `Cycle`:

`New` (engine.go:24) · `NewWithConfig` (engine.go:28, pre-existing coverage) · `Context`
(engine.go:34) · `DefaultConfig` (config.go:112) · `LoadConfig` (config.go:125) ·
`BriefFromResult` (brief.go:137) · `NewHostEngine` (host.go:107) · `ClientAdapter.Chat`
(host.go:55) · the 5 `ToolsMemory` methods (types.go:35–53).

### `code_search` evidence (definitions + callers)

```
$ code_search Search        → Search  internal/hippocampus/types.go:35  func (ToolsMemory) Search(query string, limit int, intent string) ([]tools.MemoryHit, error)
$ code_search Store         → Store   internal/hippocampus/types.go:40  func (ToolsMemory) Store(topic, question, answer, source string, keywords []string) (string, error)
$ code_search ToolsMemory   → ToolsMemory  internal/hippocampus/types.go:32  struct
$ code_search NewHostEngine → NewHostEngine  internal/hippocampus/host.go:107  func(c HostClient) (*Engine, bool)
$ code_search memoryDBEnv   → internal/tools/memory.go:59  const (also mirrored in two test-local consts)
$ bash grep -n 'e\.memory\.' internal/hippocampus/engine.go   # the ONLY callers of the 5 adapter methods
      engine.go:180  found, err := e.memory.Search(q, e.cfg.RetrievalLimit, RetrievalIntent)
      engine.go:245  out, err := e.memory.Store(act.Topic, act.Question, act.Answer, act.Source, act.Keywords)
      engine.go:252  out, err := e.memory.Get(act.ID)
      engine.go:263  out, err := e.memory.Update(act.ID, f)
      engine.go:275  out, err := e.memory.Feedback(act.ID, act.Rating, act.Reason, req.TaskID)
```

The 5 adapter methods are reachable **only** through `Engine.execute` (engine.go:216) /
`Engine.fanOut` (engine.go:173) — i.e. through the `Memory` **interface**, which is why the
concrete `ToolsMemory` implementations had never been named by a test. That is the coverage
hole in one sentence.

---

## Tests written, and the boundaries / error paths each covers

New file: `internal/hippocampus/cov_hippocampus_test.go` — helper names all carry the
task-unique prefix `covHipp*` (`covHippDB`, `covHippSeed`, `covHippHostClient`,
`covHippTimeoutEnv`) to avoid a duplicate-symbol merge collision with a sibling coverage
slice of the same package (memory #1694).

| Test | Function under test | Happy path | Boundary | Error path |
|------|--------------------|-----------|----------|-----------|
| `TestCovHippToolsMemorySearchRecallAndBoundaries` | `ToolsMemory.Search` | seeded row recalled, structured (`ID`/`Topic`/`Question`/`Answer`/`Tier` all non-zero) | `limit=0` → product default applies (recalls anyway); `limit=1` → no more than 1 hit; **unrelated query → empty slice and NO error** (a miss is not an error) | blank query `"   "` refused |
| `TestCovHippToolsMemoryStoreReadsBackAndRefusesGarbage` | `ToolsMemory.Store` | row written, read back via Search; `Sources` carries `SourceTag`; answer carries the `keywords:` breadcrumb tail | empty topic refused | garbage payload (`"? ?"`) refused by the isGarbage gate |
| `TestCovHippToolsMemoryGetBoundaries` | `ToolsMemory.Get` | returns the requested row | — | `id=0` and `id=-1` refused; (also asserts the **neighbour row is absent** from the render — a one-sided "contains" assertion would pass with a leaking implementation) |
| `TestCovHippToolsMemoryUpdateBoundaries` | `ToolsMemory.Update` | topic+tier+confidence edit persists (read back with Get) | — | `id=0` refused; unknown tier `"frozen-solid"` refused; (neighbour row asserted **untouched**) |
| `TestCovHippToolsMemoryFeedbackBoundaries` | `ToolsMemory.Feedback` | `RatingUseful` vote → `FbUseful > 0` on read-back | `FbUseless` stays 0 (no cross-contamination) | `id=0` refused; unknown rating `"ambivalent"` refused |
| `TestCovHippNewHostEngineBoundaries` | `NewHostEngine` | bound engine: `Context()` is `ToolsMemory`, a full cycle runs against the injected transport, action lands | `nil` client → `(nil, false)` — the *only* accepted unbound case | transport error is **recorded** on the Result (`StopClientError`, message present), never swallowed (Rule 15) |
| `TestCovHippNewHostEngineRaisesTimeoutFloors` | `NewHostEngine` floors | — | tiny configured timeout (`GOPI_HIPPOCAMPUS_TIMEOUT_SEC=1`) is **raised** to `HostEngineCycleTimeout` / `HostEngineClientTimeout` | the inverse direction: a configured value **above** the floors is **preserved** (floors never lower an operator's setting) |
| `TestCovHippClientAdapterRetryContract` | `ClientAdapter.Chat` (host.go:55) | usable reply costs exactly 1 attempt | `Retries<0` disables retrying (1 attempt, raw reply handed over) | JSON-less reply → exactly `1 + ClientAdapterDefaultRetries` attempts then handed over; transport error propagates **unretried**; nil-receiver and nil-client adapters refuse |
| `TestCovHippEngineSurfaceAndNilCollaborators` | `New`, `Context` | `New` resolves `LoadConfig()` (asserts `cfg.MaxRounds == DefaultMaxRounds`) and `Context()` returns `ToolsMemory` | — | nil client / nil memory both rejected loudly |
| `TestCovHippConfigSurfaceBoundaries` | `DefaultConfig`, `LoadConfig`, `BriefFromResult` | `DefaultConfig()` equals the documented constant set field-by-field | `LoadConfig` ignores an unparseable (`"not-a-number"`) and a zero (`"0"`) override; `BriefFromResult(nil) == nil` | — (determinism asserted: two identical inputs → identical `LeadsKey`/`Summary`) |

**Constants used (Rule 1b/13).** `tools.TierCold`, `tools.RatingUseful`, `tools.RatingUseless`,
`SourceTag`, `RetrievalIntent`, `VerbNone`, `StopNoneEmitted`, `StopClientError`,
`StopEmptySearch`, `DefaultRetrievalLimit`, `HostEngineClientTimeout`, `HostEngineCycleTimeout`,
`DefaultMaxRounds`. **Honest gap:** `internal/tools`' `memoryDBEnv` (`"GOPI_MEMORY_DB"`) is
**unexported**, so no importable constant exists for it — the new file states the value once in
`covHippDB` (mirroring `engine_test.go`'s existing `setMemoryDB`) and says so here rather than
pretending a constant was available. Same for `GOPI_HIPPOCAMPUS_TIMEOUT_SEC`, which is
`envTimeoutSec` (unexported) — pinned once as `covHippTimeoutEnv`.

**ANTHROPIC_AUTH_TOKEN:** not needed for these tests. Every test injects a stub transport
(`covHippHostClient` / `stubClient`) and a temp DB, so the whole suite is network-free and
token-free; no `ANTHROPIC_AUTH_TOKEN` export is required. (`scripts/func_coverage.py` is pure
AST/regex, also token-free.)

---

## Bug filed (real defect found by the mapping — NOT fixed, tests-only charter)

**Task `01a08c75-9d17-714a-9cb4-c2114ac0da79`** (tags `bug,testability,dead-code`).

`internal/hippocampus/host.go` contains what its own doc calls the **production binding** —
`NewHostEngine` (host.go:107), `ClientAdapter` (host.go:44/55) and the timeout floors
`HostEngineClientTimeout = 90s` / `HostEngineCycleTimeout = 4min` (host.go:80/84) — but **no
production code calls it**. The live organ is built with the raw constructor:

```
internal/agent/hippocampus_agent.go:160
    engine := hippocampus.New(&hippoClaudeClient{c: c, model: model}, hippocampus.ToolsMemory{})
   (reached via bindHippocampusRecall → SetHippocampusRecall → newHippocampusRecallFunc,
    internal/agent/hippocampus_agent.go:159/:450, called from internal/agent/loop.go:2349)

grep -rn 'NewHostEngine' --include='*.go' --include='*.md' --include='*.json' . | grep -v .git
   → host.go:98 (doc), host.go:107 (declaration), new test file. ZERO production callers.
```

This is **not** merely dead code. `hippocampus.New` calls `LoadConfig()`, so production runs
with `DefaultClientTimeout = 10s` — the batch-pipeline value that `host.go:78–84` exists to
override ("a live run's host call must survive a slow flash endpoint", floor 90s). The
`ClientAdapter` bounded reply retry is likewise unapplied on the live path. `git_blame` shows
the cause: commit `76e492bf` *salvaged* `host.go` from a parallel branch ("master's parallel
host implementation kept (no duplicate host merged)") — two host implementations were merged,
one kept live, the other never wired.

Filed with the repro, the consequence (declared-but-unapplied 90s floor ⇒ live 10s timeout),
and the two acceptable resolutions. Fixing it is a product change, out of this slice's scope.

---

## Verification (RAW)

```
$ go test -count=1 -timeout 600s ./internal/hippocampus/
ok  	github.com/nick/gopi/internal/hippocampus	21.549s

$ go test -count=1 -timeout 600s -run TestCovHipp -v ./internal/hippocampus/ | grep -E '^(--- (PASS|FAIL)|ok|FAIL)'
--- PASS: TestCovHippToolsMemorySearchRecallAndBoundaries (1.08s)
--- PASS: TestCovHippToolsMemoryStoreReadsBackAndRefusesGarbage (0.82s)
--- PASS: TestCovHippToolsMemoryGetBoundaries (1.21s)
--- PASS: TestCovHippToolsMemoryUpdateBoundaries (1.57s)
--- PASS: TestCovHippToolsMemoryFeedbackBoundaries (1.71s)
--- PASS: TestCovHippNewHostEngineBoundaries (0.91s)
--- PASS: TestCovHippNewHostEngineRaisesTimeoutFloors (0.00s)
--- PASS: TestCovHippClientAdapterRetryContract (0.00s)
--- PASS: TestCovHippEngineSurfaceAndNilCollaborators (0.00s)
--- PASS: TestCovHippConfigSurfaceBoundaries (0.00s)
ok  	github.com/nick/gopi/internal/hippocampus	7.319s

$ python3 scripts/func_coverage.py --pkg internal/hippocampus --uncovered
package                                     funcs    cov    pct  tests
--------------------------------------------------------------------------
internal/hippocampus                           14     14 100.0%
--------------------------------------------------------------------------
TOTAL                                          14     14 100.0%

Uncovered exported funcs (by package):

NOTE: conservative metric — a func counts as covered only if its name
appears in a *_test.go file of the same package. Target: 100%.
```

Three consecutive full-package runs were green (`31.173s`, `27.009s`, `21.549s` — non-flaky).
Build + vet: `CGO_ENABLED=0 go build -o /tmp/gopi-covhipp ./cmd/agent/` → **build OK**;
`go vet ./internal/hippocampus/` → clean; `gofmt -l internal/hippocampus/` → clean.

**Metric ≠ tested — call-site counts in the new file** (no mention-only coverage):

```
NewHostEngine: 5   Search: 10   Store: 4   Get: 10   Update: 5   Feedback: 5
New: 4   Context: 6   DefaultConfig: 2   LoadConfig: 3   BriefFromResult: 4
```

## memory_store entries

| id | topic | why |
|----|-------|-----|
| **1707** | *Declared-but-uncalled "production binding" constructor hides a live-path regression (GoPi hippocampus host floors bypassed)* | The reusable lesson: `grep -rn '<ExportedCtor>' --include='*.go' . \| grep -v _test` → declaration + tests only = zero production callers; then diff the unused constructor's guards against the live path (an unapplied timeout floor is a silent Nx-shorter timeout). Includes the salvage-commit root cause and the fix shape. |
| **1708** | *GoPi internal/hippocampus coverage slice recipe: covHipp* prefix, adapter round-trip contracts, real boundary/error matrix, constants and traps* | The retrieval breadcrumbs for the next slice of this package: exact uncovered list, the adapter-vs-interface insight, the per-function boundary/error matrix, which constants exist vs which do **not** (`GOPI_MEMORY_DB` unexported), and the traps (`ToolsMemory{}.Store(...)` in an `if` init is a parse error; `Brief` has no `Body`; same-package tests may read `engine.cfg`; this package runs in ~30s, no background job needed). |

## Workflow feedback

1. **The briefing's name list was the symptom, not the map.** "6 uncovered exported functions"
   turned out to be 5 methods on ONE adapter struct + 1 constructor. Reading `types.go` first
   reframed the task from "write 6 unrelated tests" to "test one adapter's five-method contract
   and one constructor's floors" — and the adapter round-trip is what surfaced the real defect.
2. **`code_search` answered "who calls this?" better than reading.** `code_search ToolsMemory`
   + `grep -n 'e\.memory\.' engine.go` proved in one round that the five concrete methods were
   reachable *only* through the `Memory` interface — that IS the coverage hole, and it is also
   why the tests must call the adapter directly (through the interface they are only
   name-appearances).
3. **`keyword_search 'NewHostEngine'` returning "host.go:98, host.go:107" and nothing else was
   the bug found.** A single-caller-set search that returns only the declaration is a *finding*,
   not a null result. Worth wiring into the coverage workflow explicitly: for each newly covered
   exported symbol, check the caller set once and record it.
4. **Cost of the honest check was ~1 minute.** `func_coverage.py` cannot distinguish a mention
   from a call, so the per-function call-site count (`grep -oE "\bFn\("`) is the only cheap
   guard — it should be a standing step, not an afterthought (memory #1694 / #1680).
5. **Friction worth noting:** the one-line forwarding adapters are exactly the code the metric
   under-reports and reviewers skip. A `MemorySearchHits`-style in-process surface with an
   unexported adapter would have hidden this class of bug entirely; the exported `ToolsMemory`
   type is what made the mapping tractable.
6. **Time:** ~6 min wall clock from reframe to green (t+15s worktree → t+379s evidence),
   single-digit tool rounds. No 120s-kill issue: this package's suite is ~30s (unlike
   `internal/tools`' 250s+), so no `job(action=start)` was needed.

---

## Deliverable declaration (Rule 6d)

- **Branch:** `feat/cov-hippocampus-hp` (in the shared repo — the worktree shares master's refs).
- **Recovery command:** `git show feat/cov-hippocampus-hp:devdoc/cov-hippocampus-host-adapter.md`
  (test file: `git show feat/cov-hippocampus-hp:internal/hippocampus/cov_hippocampus_test.go`).
- Nothing was committed to `master`; no push needed (worktree semantics, Rule 6c).
- Worktree left in place for review (PA merges/removes it; no self-merge).
