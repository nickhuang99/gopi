<!-- doc-class: historical -->
# Task Report: PILOT — function-level coverage campaign on `internal/taskverdict`

Branch: `feat/cov-taskverdict-gopi` · worktree `~/workspace/gopi-cov-taskverdict`
Author: gopi (pilot run, 2026-09-10/11) · Product code: **unmodified** (test file only)

## 1. Summary

Piloted the periodic function-level test-coverage campaign on the smallest
possible target: `internal/taskverdict` — the zero-dep leaf package that holds
the reframe verdict vocabulary (2 exported funcs, 0 test files, 0% covered).

Delivered a 270-line test file with 9 test functions / 23 rejection subtests,
a 12-mutant mutation run that judged the tests' actual strength, 2 distilled
memory records, 1 real bug ticket, and the scale-up assessment in §8.

Headline finding for the campaign: **`func_coverage.py` is a name-based
metric. It measures whether a function's *name* appears in a same-package
`*_test.go`, never whether anything is asserted. I proved empirically that a
package whose test file contains ZERO test functions and only the prose
`var notes = "Alpha Beta"` still reports 100.0%.** The metric is a useful
backlog enumerator, but as the campaign's success gate it is self-deceiving
unless paired with (a) real `go test -cover` and (b) a cheap mutation probe
per function (both were run here — §5).

## 2. File + function inventory (read completely, no skimming)

Target package — the entire package is 69 lines in ONE file:

| file | lines | contents |
|---|---|---|
| `internal/taskverdict/verdict.go` | 69 | package doc (lines 1–27, the SPEC §3 rationale), 3 exported consts, `All()`, `Valid()` |
| (no other `.go` file) | — | no test file existed → 0% covered |
| `scripts/func_coverage.py` | 175 | the campaign's metric tool (read in full — needed to score the gate honestly) |

Exported API (the campaign's unit of work) — both funcs, both previously
untested:

| symbol | definition | signature | contract read from the source |
|---|---|---|---|
| `Doable` / `TooBig` / `Impossible` | `verdict.go:36` / `:40` / `:43` | `const = "DOABLE"` / `"TOO_BIG"` / `"IMPOSSIBLE"` | the **wire** words sent by the LLM; `TooBig` requires a roadmap, `Impossible` requires a reason (payload enforced by the harness, not here) |
| `All` | `verdict.go:49` | `func All() []string` | returns `[]string{Doable, TooBig, Impossible}` — **in schema/enum order**, a fresh slice literal per call |
| `Valid` | `verdict.go:56` | `func Valid(s string) bool` | linear exact match over `All()`; returns false for anything else — the harness validation gate |

`code_search` evidence (definitions, one structured call each):

```
$ code_search All  -> All  internal/taskverdict/verdict.go:49  func  func All() []string
                      All  internal/gopilog/httpview.go:35     method (unrelated homonym)
$ code_search Valid -> Valid internal/taskverdict/verdict.go:56  func  func Valid(s string) bool
```
`Valid` is **unique in the repo** (only one definition, any case) — `code_search`
returned exactly one hit, which is itself the evidence that no sibling/method
variant of the gate exists (e.g. no `ValidStrict`, no receiver-based validator).

### Caller inventory (outside the package — all found by `keyword_search taskverdict\.`)

There are **no callers inside** the package (only `Valid` → `All`). Every real
caller is outside it:

| caller | file:line | how it uses the vocabulary |
|---|---|---|
| `toolnamesVerdictEnum()` | `internal/agent/loop.go:4837` | `strings.Join(taskverdict.All(), "|")` — renders the task tool's schema enum text |
| task tool schema — `action` description | `internal/agent/loop.go:1200` | embeds `toolnamesVerdictEnum()` **plus** `taskverdict.TooBig` / `.Impossible` prose (the LLM-facing contract) |
| task tool schema — `verdict` property description | `internal/agent/loop.go:1219` | `"…ONE of " + toolnamesVerdictEnum() + " — never empty."` |
| task tool schema — `roadmap` / `reason` property descriptions | `internal/agent/loop.go:1246`, `:1250` | interpolates `taskverdict.TooBig` / `taskverdict.Impossible` |
| `reframeVerdictList` | `internal/agent/reframe.go:62` | `strings.Join(taskverdict.All(), " | ")` — human-facing list in the round-1 `[EPIC REFRAME]` directive (`reframe.go:83–87`, `:133–134`) |
| `validateReframe` — **the gate** | `internal/agent/reframe.go:206` | `if !taskverdict.Valid(in.Verdict) { return fmt.Errorf("…invalid verdict %q…") }`; the empty case is rejected one step earlier (`:201`) |
| dispatch switch | `internal/agent/reframe.go:424–429` | `case taskverdict.Doable` / `case taskverdict.TooBig` / `default: // taskverdict.Impossible` |
| `ApplyReframe…` (roadmap/impossible dispatch) | `internal/agent/reframe.go:157`, `:220`, `:222`, `:229`, `:231`, `:425–428`, `:452`, `:461–472` | interpolates the constants into user-visible text and the ledger reason |
| `TaskReframeRecord` — **second gate** | `internal/tools/reframe.go:135` | `if !taskverdict.Valid(verdict) { return fmt.Errorf("task: reframe telemetry: invalid verdict %q", verdict) }` (telemetry write path) |
| `TaskReframeStats` (distribution render) | `internal/tools/reframe.go:267` | `for _, v := range append(taskverdict.All(), reframeVerdictUnknown)` |
| agent-side tests (pre-existing) | `internal/agent/reframe_test.go:59,65,104,133,153…`, `reframe_gate_test.go:122`, `reframe_telemetry_test.go:48–64` | assert against the constants, not literals |
| CLI surface | `cmd/agent/cli_tasks.go:142`, `:554`, `:566` | `gopi task verdicts` (the meter the campaign's anti-bureaucracy proof reads) |

Two facts from that inventory shaped the tests:

1. **`All()`'s order is a wire contract**, not cosmetics — the enum text the
   LLM is taught, the human-facing directive list, and the distribution table
   all render in slice order. A reorder is a behaviour change → pinned.
2. **`Valid()` is the ONLY thing standing between the LLM and a fabricated
   verdict**: the tool schema's `verdict` property deliberately carries no JSON
   `enum` (it is shared with `action=review`'s A–E codes — see the doc comment
   at `internal/agent/reframe_gate_test.go` "The `verdict` property deliberately
   carries NO JSON enum"), and the dispatch switch at `reframe.go:428` ends in
   `default: // taskverdict.Impossible`. So the schema *teaches*, the harness
   gate *enforces* — and if `Valid()` ever loosens, an unknown string silently
   becomes `IMPOSSIBLE` (a fabricated honest exit) or a malformed telemetry row.
   That is why the rejection table is the centre of gravity of this suite.

## 3. Test file: `internal/taskverdict/verdict_test.go` (new, 270 lines)

Anti-drift discipline (Rules 1b/13): **the file contains zero verdict
literals** — every expectation is built from `Doable` / `TooBig` / `Impossible`
or from named test constants. Verified mechanically:

```
$ grep -nE '"(DOABLE|TOO_BIG|IMPOSSIBLE)"' internal/taskverdict/verdict_test.go
(no matches)
```
and a repo-wide audit shows the three literals exist in exactly ONE production
site (`verdict.go:36/40/43`, the definition) — no bypass:
```
$ grep -rn '"DOABLE"\|"TOO_BIG"\|"IMPOSSIBLE"' --include=*.go . | grep -v _test.go
./internal/taskverdict/verdict.go:36:	Doable = "DOABLE"
./internal/taskverdict/verdict.go:40:	TooBig = "TOO_BIG"
./internal/taskverdict/verdict.go:43:	Impossible = "IMPOSSIBLE"
```
(the same grep hits only `devdoc/poc-reframe-verdict/*.json` — recorded POC
request/response evidence, not code.)

Test-local named constants (Rule 1a/1b — no magic values, no bare keywords):
`enumSeparator`, `gateDefinitionFile`, `retiredResearchVerdict`,
`telemetryUnknownBucket`, `callerMutation`, `callerAppendedValue`,
`wantVocabularyLen`.

### Per-test boundary / error-path mapping

| # | test | path | what it pins | why it can fail (not a name-drop) |
|---|---|---|---|---|
| 1 | `TestAll_ReturnsFullVocabularyInSchemaOrder` | happy + boundary (order, length) | exact 3 elements **in order** `Doable, TooBig, Impossible` | compares element-by-element against constants → reorder/insert/delete all fail |
| 2 | `TestAll_VocabularyIsClosedAndClean` | boundary (hygiene) | `SPEC §3`: exactly 3 verdicts; no empty; no whitespace-padded; no `|` inside (would split the enum into two schema options); no duplicate | sentinel values, each asserted individually; the `|` check is the enum-injection guard |
| 3 | `TestAll_ReturnsAFreshSlice` | boundary (aliasing) | `All()` hands out a NEW slice every call, so a caller that sorts/mutates it (the distribution report sorts by popularity) cannot corrupt the next caller | mutates + appends, then re-reads → a shared backing array fails |
| 4 | `TestValid_AcceptsEveryVocabularyEntry` | happy | every value `All()` advertises is accepted — schema and gate agree (driven by `All()`, so a future 4th constant is auto-covered) | the cross-check between the two exported funcs |
| 5 | `TestValid_RejectsNonVocabulary` | **error/invalid input** (23 subtests) | exact-match semantics: `empty`, `whitespace only`, `tab and newline`, `lowercase` (×2), `title case`, `trailing space`, `leading space`, `surrounding spaces`, `trailing newline`, `separator-mangled` (`DOABLE|TOO_BIG` — the enum string itself), `space-mangled too-big`, `underscore-stripped too-big`, `prefix only`, `prefix of impossible`, `superset` (`DOABLEDOABLE`), `dropped legacy research verdict` (`NEEDS_RESEARCH`), `telemetry unknown bucket` (`UNKNOWN`), `unknown word`, `review code` (`A`), `numeric string`, `null-ish`, `unicode lookalike` (fullwidth), `zero byte` | 23 independent rejections; the `UNKNOWN` case was added *because* a mutant survived without it (§5) |
| 6 | `TestValid_RejectionIsExhaustiveOverTrimmedAll` | error (property) | the two-sided contract: no whitespace-padded form of a valid verdict is accepted ⇒ `Valid()` never trims | generated from `All()`, so it generalises over the vocabulary |
| 7 | `TestWireContract_EnumDescriptionOrder` | boundary (consumer coupling) | `strings.Join(All(), "|")` equals the constant-built expectation, and the joined enum is itself rejected as a single verdict | the only test that reasons about the consumer's rendering |
| 8 | `TestGate_PackageSourcesCarryNoVerdictLiterals` | boundary (Rule 13 gate) | no sibling production file in this package re-types a verdict literal (derives the literals from `All()`, so the gate holds no literals itself) | proven live: injecting `const shadow = "TOO_BIG"` into a new sibling file fails at `verdict_test.go:234` (currently vacuous — only `verdict.go` exists — and it logs that fact) |
| 9 | `TestGate_VocabularyStaysMinimal` | boundary (API drift) | the package exports **exactly** `All` + `Valid` (AST parse of `verdict.go`) — a 3rd exported func means the vocabulary surface widened | AST-based, so it fails on any added exported func |

Honest scope note: these tests **pin contracts, not behaviour implementation** —
that is the right altitude for a constants+2-accessor leaf package. There is no
complex logic to unit-test here; what *can* go wrong is drift in the shared
vocabulary, and that is exactly what tests 1–3 and 7–9 detect.

## 4. Verification (raw output, unedited)

```
$ python3 scripts/func_coverage.py --pkg internal/taskverdict
package                                     funcs    cov    pct  tests
--------------------------------------------------------------------------
internal/taskverdict                            2      2 100.0%
--------------------------------------------------------------------------
TOTAL                                           2      2 100.0%

NOTE: conservative metric — a func counts as covered only if its name
appears in a *_test.go file of the same package. Target: 100%.
```

```
$ python3 scripts/func_coverage.py --pkg internal/taskverdict --uncovered
package                                     funcs    cov    pct  tests
--------------------------------------------------------------------------
internal/taskverdict                            2      2 100.0%
--------------------------------------------------------------------------
TOTAL                                           2      2 100.0%

Uncovered exported funcs (by package):

NOTE: conservative metric — a func counts as covered only if its name
appears in a *_test.go file of the same package. Target: 100%.
```

```
$ go test -count=1 -cover ./internal/taskverdict/
ok  	github.com/nick/gopi/internal/taskverdict	0.056s	coverage: 100.0% of statements
```

```
$ go test -count=1 -v -cover ./internal/taskverdict/     [names only, timings stripped]
--- PASS: TestAll_ReturnsFullVocabularyInSchemaOrder
--- PASS: TestAll_VocabularyIsClosedAndClean
--- PASS: TestAll_ReturnsAFreshSlice
--- PASS: TestValid_AcceptsEveryVocabularyEntry
--- PASS: TestValid_RejectsNonVocabulary
    --- PASS: TestValid_RejectsNonVocabulary/empty
    --- PASS: TestValid_RejectsNonVocabulary/whitespace_only
    --- PASS: TestValid_RejectsNonVocabulary/tab_and_newline
    --- PASS: TestValid_RejectsNonVocabulary/lowercase
    --- PASS: TestValid_RejectsNonVocabulary/lowercase_too-big
    --- PASS: TestValid_RejectsNonVocabulary/title_case
    --- PASS: TestValid_RejectsNonVocabulary/trailing_space
    --- PASS: TestValid_RejectsNonVocabulary/leading_space
    --- PASS: TestValid_RejectsNonVocabulary/surrounding_spaces
    --- PASS: TestValid_RejectsNonVocabulary/trailing_newline
    --- PASS: TestValid_RejectsNonVocabulary/separator-mangled
    --- PASS: TestValid_RejectsNonVocabulary/space-mangled_too-big
    --- PASS: TestValid_RejectsNonVocabulary/underscore-stripped_too-big
    --- PASS: TestValid_RejectsNonVocabulary/prefix_only
    --- PASS: TestValid_RejectsNonVocabulary/prefix_of_impossible
    --- PASS: TestValid_RejectsNonVocabulary/superset
    --- PASS: TestValid_RejectsNonVocabulary/dropped_legacy_research_verdict
    --- PASS: TestValid_RejectsNonVocabulary/telemetry_unknown_bucket
    --- PASS: TestValid_RejectsNonVocabulary/unknown_word
    --- PASS: TestValid_RejectsNonVocabulary/review_code
    --- PASS: TestValid_RejectsNonVocabulary/null-ish
    --- PASS: TestValid_RejectsNonVocabulary/unicode_lookalike
    --- PASS: TestValid_RejectsNonVocabulary/zero_byte
--- PASS: TestValid_RejectionIsExhaustiveOverTrimmedAll
--- PASS: TestWireContract_EnumDescriptionOrder
--- PASS: TestGate_PackageSourcesCarryNoVerdictLiterals
--- PASS: TestGate_VocabularyStaysMinimal
PASS
coverage: 100.0% of statements
```

Supporting gates:

```
$ CGO_ENABLED=0 go build ./...          -> exit 0
$ go vet ./internal/taskverdict/        -> clean
$ gofmt -l internal/taskverdict/        -> (no output = clean)
$ go test -count=1 ./internal/agent/ -run 'Reframe|Verdict|Retired|Schema'  -> ok 5.778s   (no regression)
$ go test -count=1 ./internal/tools/ -run 'Reframe'                         -> ok 0.061s   (no regression)
$ git diff 587624f -- internal/taskverdict/verdict.go   -> (empty: product code byte-identical)
```

## 5. The metric, honestly scored — mutation evidence

The gate the campaign measures itself with is name-based (§2 of
`scripts/func_coverage.py`: `covered_names()` collects every `[A-Z]\w+`
identifier that occurs in a `*_test.go` and checks membership). Measured
directly:

```
$ python3 scripts/func_coverage.py --root /tmp/cov-metric-probe-gopi --pkg probepkg
package                                     funcs    cov    pct  tests
--------------------------------------------------------------------------
probepkg                                        2      2 100.0%
```
…where `probepkg/pkg_test.go` contains **zero test functions** — only
`var notes = "Alpha Beta"` and a comment. 100.0% reported for entirely
untested code. Conclusion for the campaign: **`--uncovered` is a fine todo
list; the percentage is not evidence.** Real evidence = `go test -cover`
(statement truth) + a mutation probe (assertion truth).

So the suite was mutation-tested: 12 mutants of `verdict.go` run against it in
a throwaway module (`/tmp/cov-mut-gopi`, a copy of the package + test, so the
real repo is untouched; compile-error mutants are classified INVALID and prove
nothing).

```
=== baseline (fixed suite) ===
ok  	mutprobe/taskverdict	0.003s
caught: Valid accepts UNKNOWN bucket -> 1 assertion line(s)
caught: reorder: TooBig,Doable,Impossible -> 3 assertion line(s)
caught: 4th verdict appended -> 5 assertion line(s)
caught: drop Impossible -> 4 assertion line(s)
caught: duplicate Doable -> 5 assertion line(s)
caught: Valid: prefix match -> 2 assertion line(s)
caught: Valid: always true -> 28 assertion line(s)
caught: All shares one backing array -> 2 assertion line(s)
*** MUTANT SURVIVED: constant value changed
caught: extra exported func added -> 1 assertion line(s)
*** MUTANT SURVIVED: literal duplicated in a sibling prod file
caught: constant value dropped from All() -> 4 assertion line(s)
--- additional probes ---
caught: Valid() trims input (compile-valid) -> 7 assertion lines,
        e.g. Valid("DOABLE ") = true, want false — the harness gate must reject anything outside All()
caught: new sibling prod file re-types "TOO_BIG" -> gate fires:
        verdict_test.go:234: helper.go re-types the verdict literal "TOO_BIG" — reference the constant in verdict.go instead
```

Reading of the run:

- **10 of 12 behavioral mutants caught.** The suite has real teeth on
  contents/order/membership/aliasing/API-surface.
- **2 survivors, both traced to a cause worth stating:**
  - `constant value changed` (`DOABLE` → `DOABLE_NOW`) survives because every
    test derives its expectation from the constants — that is precisely the
    anti-drift property requested by the briefing (Rules 1b/13). Detecting a
    *value* change is the job of the wire-contract record (memory #1681) and of
    the schema-coupling test at the consumer level, not of this package.
  - `literal duplicated in a sibling prod file`: the mutant that survived had
    its `const shadow = "TOO_BIG"` planted **inside `verdict.go` itself** —
    which is the gate's one *exempt* file (the definition site; the exemption
    is what stops the gate from flagging its own constants). The same literal
    planted in a **new sibling file** was **caught, live**:
    `verdict_test.go:234: helper.go re-types the verdict literal "TOO_BIG" — reference the constant in verdict.go instead`.
    So the survivor was a probe artifact, not a hole — but it does document the
    gate's deliberate blind spot (§8).
- One mutant (`Valid accepts UNKNOWN`) **initially survived** — the suite as
  first written did not reject the telemetry bucket. The test was fixed
  (subtest `telemetry_unknown_bucket` added, with the constant
  `telemetryUnknownBucket` and a comment on why the bucket must stay
  telemetry-only), and the mutant is now caught. **This is the single strongest
  argument for putting mutation probing into the campaign: my first, "complete"
  version would have passed the 100.0% gate while leaving a real hole.**

Post-recheck note (same session, after the report was first drafted): the
file's one remaining bare occurrence of a verdict word was a *prose* mention
inside the `telemetryUnknownBucket` comment — never compared, but it would
trip a reviewer's `grep 'DOABLE\|TOO_BIG\|IMPOSSIBLE'` anti-drift check and
make the "zero literals" claim arguable. Reworded so the file now matches the
strict pattern **zero times**, and all gates were re-run on the amended file:

```
$ gofmt -l internal/taskverdict/          → (clean)
$ go test -count=1 -cover ./internal/taskverdict/
ok  github.com/nick/gopi/internal/taskverdict  0.005s  coverage: 100.0% of statements
$ python3 scripts/func_coverage.py --pkg internal/taskverdict             → 2/2 100.0%
$ python3 scripts/func_coverage.py --pkg internal/taskverdict --uncovered → 2/2 100.0%, empty uncovered list
$ grep -c 'DOABLE\|TOO_BIG\|IMPOSSIBLE' internal/taskverdict/verdict_test.go → 0
```

## 6. Defect found → bug ticket

Product code was not touched (constraint). One real defect was found in a
*consumer* of the vocabulary and filed:

**`01a08c20-b97f-7c66-8bcd-816e8b8da364`** — *`gopi task verdicts` silently
drops out-of-vocabulary verdict rows from the distribution table* (tags
`bug`, `reframe`, `telemetry`).

Reproduced deterministically (throwaway test, deleted afterwards — `git status`
clean):

```
NEEDS_RESEARCH / BANANA row seeded into task_events, then:
$ go test -count=1 -run TestZZProbeLegacyRetiredVerdict -v ./internal/tools/
    RENDER:
        2 reframe record(s) across 2 task(s)
        VERDICT       COUNT  ITEMS
        DOABLE            1      0
        per task:
          t-legacy ×1 (NEEDS_RESEARCH)
          t-ok ×1 (DOABLE)
    total_lines_head=2 table_rows=1  => header total vs rendered rows mismatch=true
```

Root cause (both halves named in the ticket): `parseReframeVerdict`
(`internal/tools/reframe.go:98`) does no membership check, while the render loop
(`reframe.go:267`) iterates the closed list `append(taskverdict.All(),
reframeVerdictUnknown)` — so a *retired* or corrupt token becomes its own
`dist` key, increments the header total, and is then never printed. The row
is invisible in the very meter the reframe campaign cites as its
anti-bureaucracy proof (`reframe.go:211–216`). Same failure family as bug
`01a089c9` (reframe telemetry double-count).

Live-database context (read-only): the current `~/.gopi/memory.db` holds
`verdict=DOABLE` ×158, `verdict=TOO_BIG` ×1, `verdict=IMPOSSIBLE` ×1, no
`verdict=`-less row — so today's damage is latent, but any pre-retirement
`NEEDS_RESEARCH` row (or a future 4th value) is silently dropped.

## 7. Memory entries recorded (with the reason for each)

| id | topic | why it was worth storing |
|---|---|---|
| **1680** | *`func_coverage.py` is a name-based metric — 100% ≠ tested* | The single most important fact for scaling the campaign: the gate can be satisfied by prose. Stored with the empirical counter-example (probepkg) + the three-guard recipe (real `go test -cover`, real assertions, mutation probe) so the next 124 functions are not "covered" by name-drops. Keywords span 中英 terms (`假覆盖`, `name-drop`, `mutation testing`, `覆盖campaign`) so it is retrievable from either phrasing. |
| **1681** | *taskverdict verdicts are a wire protocol — constant → schema enum → `Valid()` gate → dispatch `default`* | The subtle contract that a future editor of this package must not break: order is the wire contract; the schema deliberately has no JSON enum so `Valid()` is the only enforcement; the dispatch switch's `default` swallows unknown values as `IMPOSSIBLE`; `Valid()` must never trim; `UNKNOWN` is a render-only bucket. Includes the caller file:line chain so it is verifiable, and the anti-drift instruction (expectations from constants). |

Both records carry 检索面包屑 spanning WHAT/WHY/WHEN/WHERE/WHO/HOW with
synonyms, Chinese + English variants and Unicode-normalised alternatives, per
the store discipline.

## 8. Candid assessment: is this workflow fit to scale to ~125 functions / 159 files?

Method context (so the numbers mean something): the pilot was run with the
mandated method — read the source fully, `code_search` the definitions,
`keyword_search` **all** callers incl. outside the package, inventory existing
tests, then write happy-path + boundary + error-path tests, then page the
metric gate. Repo-wide today: **479 exported funcs, 348 covered = 72.7%, 131
gaps in 11 packages** (`internal/tools` 49, `internal/testutil` 29,
`internal/agent` 15, `internal/deepseek` 12, `internal/config` 6, …).

**What worked / should be kept**

1. **Leaf-package-first ordering is right.** This package had zero call-graph
   risk, so the whole method (read + inventory + test) fit in one pass, and the
   caller inventory still surfaced real coupling (schema enum, two `Valid()`
   gates, distribution render). Cheap target, real lesson.
2. **`keyword_search taskverdict\.` as the caller sweep is the highest-value
   single step.** `code_search` gives definitions (one call each); the *usages*
   call is what found the wire contract — and it is what found the bug. Cost:
   two calls. Keep it mandatory for every function in the campaign.
3. **Tests-as-contract at the right altitude.** For a constants+accessor leaf,
   "boundary/error" means exact-match semantics and drift gates, not branch
   coverage. Deciding the altitude once per package (leaf vs logic-bearing)
   avoided writing 40 vacuous tests.
4. **The anti-drift rule forces better tests.** Building every expectation from
   the constants (instead of re-typing the wire strings) is not just a Rule 13
   formality — it is what made the enum/order/wire tests write themselves.
5. **Mutation probing is what made the gate honest** — and it was cheap here
   (12 mutants, one scripted loop, ~1 min, zero-dep throwaway module). It
   caught a real hole in a suite that the mandated gate had already blessed at
   100.0%.

**What was awkward — fix before scaling**

1. **The metric is name-based and can be gamed by prose (§5).** At 125
   functions this is the dominant risk: a fatigued agent can bank 100% with
   name-drops. *Change:* make the campaign's definition-of-done
   `func_coverage.py` **plus** `go test -cover` **plus** a mutation probe for
   each newly covered function (cheap ones only — 1–2 mutants: change a
   constant, flip a condition, drop a case). Consider teaching
   `func_coverage.py` a `--strict` mode that requires the *function* to appear
   as a call (`Name(`) rather than as any identifier, and that skips
   `Test*`-named funcs.
2. **The metric counts non-package noise.** Example found here:
   `devdoc/archive` reports 5 uncovered "funcs" — all of them `Test…`
   functions inside an archived `.go` file (`devdoc/archive/taskingest-cas-test-archived.go`,
   the only package with `has_tests=False`). That is 5 of the 131 gaps being
   phantom. *Change:* exclude `devdoc/**` (and any `*archived*.go`) from the
   walk, or treat `Test*` names as non-units.
3. **Production-only-scope assumption is untested.** This repo has 439 `.go`
   files but the metric only walks packages that have non-test `.go` files; a
   test-only package would be invisible. Minor, but decide it explicitly.
4. **Consumer-side defects are abundant and out of scope.** 4 of my ~6
   non-trivial lookup steps found a consumer weakness (the stats drop; the
   `default:` branch that would turn a valid-but-unknown verdict into
   `IMPOSSIBLE`; the `reframeVerdictUnknown` bucket that cannot be imported
   because it is unexported — a Rule 14 smell; and `TestGate_PackageSources…`
   being run-time-scan dependent). That is good news for the campaign's value
   but bad news for its bookkeeping: **the campaign will generate bug tickets
   faster than the queue can drain them** (the bug flow is already at 152
   tasks). *Change:* budget for it — the report must contain the tickets, and
   the campaign needs an explicit "tested around" convention (done here: tests
   only exercise product code from the outside) so tickets never block a
   package's closure.
5. **Report cost dominates.** This report is longer than the test file it
   describes. At 125 functions, requiring this depth per function produces
   ~125 long documents nobody reads. *Change:* make the mandated report
   proportional — a per-package one-pager (function table, boundary/error
   mapping, raw gate output, mutants, tickets) plus a campaign-level rollup,
   and reserve full depth for packages where the method found something
   surprising. Also: the pilot's "candid assessment" section is the part that
   actually transfers knowledge — keep it mandatory, drop the restatement of
   the file inventory for self-evident packages.
6. **Mechanical friction worth removing before the run:** two shell attempts
   died on the sandbox's dangerous-command guard (`$VAR` in an `rm -rf`
   argument) — harmless, but a scaling run should use a pre-written harness
   script (`scripts/cov-pilot.sh <pkg>`) rather than ad-hoc shell, and the
   mutation loop belongs in that script (it is exactly the same shape for every
   package).
7. **The test-only constraint creates a blind spot** for defects *inside* the
   function under test: if `All()` had a real bug here, the method says "test
   around it" — which means at scale a package can be declared 100% covered
   while its own defect sits filed but unfixed. That is a defensible campaign
   rule, but it should be stated in the campaign's charter, not discovered.

**Verdict:** the *method* scales (it is cheap, mechanical and it finds real
defects); the *metric* does not — as the sole gate it invites exactly the
name-drop inflation it claims to prevent. Scale the campaign with the mutation
probe as a required second gate, the metric noise filtered out, per-package
(not per-function) reporting, and a standing budget for the consumer-side bug
tickets this method will keep finding.

## 9. Not done / deferred (honest list)

- Only the 2 exported funcs of the target package are covered — the ~125
  remaining functions are the campaign's job, not this pilot's.
- No product-code change for the filed defect (`01a08c20…`) — out of scope by
  constraint; it needs its own task.
- `TestGate_PackageSourcesCarryNoVerdictLiterals` is vacuous today (this
  package has a single production file) and it **exempts `verdict.go` by
  design** (the definition site must hold the literals). So a literal re-typed
  *inside* `verdict.go` — e.g. a second `const shadow = "TOO_BIG"` beside the
  real one — passes the gate; that is the gate's documented blind spot and the
  reason the mutation probe (not the gate) is what judges this package. The gate
  does fire for any *other* file present when the test binary starts
  (`-count=1` recommended).
- The mutation harness was ad-hoc (a scripted loop in `/tmp`), not committed as
  a repo script — recommended in §8.6 but not delivered here.
- `func_coverage.py` was **not** modified (product tooling; the pilot's mandate
  is tests + report).

## 10. Deliverable location

- Branch: **`feat/cov-taskverdict-gopi`** (worktree `~/workspace/gopi-cov-taskverdict`,
  shared with the main repo — the commit is already reachable from
  `/home/user/work/GoPi`; no push needed).
- Commit order on `feat/cov-taskverdict-gopi` (`git log --oneline`): `eef1b1a`
  (tests) → `32c6c25` (report + mutation section) → `e63819a` (hardening: zero
  verdict-token occurrences in the test file) → a docs-only hash-fix on top.
  Every gate below was re-run green after the last test-bearing commit
  (`e63819a`); the only later commits touch this report.
- Recovery: `git show feat/cov-taskverdict-gopi:internal/taskverdict/verdict_test.go`
  · `git show feat/cov-taskverdict-gopi:devdoc/cov-pilot-taskverdict-gopi.md`
- Product code unmodified: `git diff 587624f -- internal/taskverdict/verdict.go` is empty.
