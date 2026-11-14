<!-- doc-class: historical -->
# The whole-tree scanner probe carries its own clock

**File**: `internal/tools/search_fallback_test.go` ·
**Branch**: `task/01a1375960917a6dbab97b81368a8a5a` ·
**Class**: test-surface fix (production behaviour untouched)

## Symptom (as filed)

`TestSearchFallbackKeywordFindsRealTextWithoutRG` went red on a 4-core host under
load with a wall-clock failure, not an assertion failure:

```
--- FAIL: TestSearchFallbackKeywordFindsRealTextWithoutRG
    search_fallback_test.go:126: KeywordSearch with no backend on PATH returned
    an error (keyword_search: timed out after 30s) — the scanner leg did not answer
```

## Root cause

The probe called the production entry point `KeywordSearch`, which bounds the call
with the TOOL's clock — `FileToolTimeout()`, `DefaultFileToolTimeout = 30s`,
overridable by `GOPI_TOOL_TIMEOUT_SEC` (`internal/tools/timeout.go`, single reader).
That clock is a promise about a **user-facing call**, but the probe's workload is
the **whole checkout** (measured at the fix: 7782 tracked files, 135.7 MB of tracked
blobs, 172 MB of worktree — `git ls-files | wc -l`, `git ls-files -z | xargs -0 stat -c%s`),
so the probe's wall time is a function of the host's page cache and load, not of the
scanner. Measured, this 4-core host (`hosta`), load 1.9–5.3 (re-measured under
load 9.45 during the fix):

| measurement | value |
|---|---|
| scanner leg, first run of the day (cold-ish cache) | **5.63 s** |
| scanner leg, subsequent runs (warm) | **0.76 s / 1.04 s** |
| test binary end-to-end wall (suite startup included) | **8.35 s / 8.38 s** |
| same probe on the discovering host (briefing) | **22.5 s** |

30 s − 22.5 s leaves **~7 s of margin** on the host that filed the defect: the test
was deciding red/green from host load, and the assertion it exists for ("with no rg
on PATH, the in-process scanner still returns a real hit in
`internal/tools/search_fallback.go`") never ran.

## Reproduction of the filed shape, on this host

The absolute default (30 s) is not reachable here — this host is warm and fast. The
shape is reproducible by tightening the same knob the failure came through:

```bash
GOPI_TOOL_TIMEOUT_SEC=1 go test ./internal/tools/ \
    -run TestSearchFallbackKeywordFindsRealTextWithoutRG -count=1
```

filed text, captured verbatim (5 attempts, 1 red — the run at which the cache was
cold enough to cross 1 s):

```
--- FAIL: TestSearchFallbackKeywordFindsRealTextWithoutRG (1.00s)
    search_fallback_test.go:126: KeywordSearch with no backend on PATH returned an error (keyword_search: timed out after 1s) — the scanner leg did not answer
```

Evidence: `~/.gopi/sessions/01a13759-6091-7a6d-bab9-7b81368a8a5a/artifacts/red-proof-1s-x5.txt`.

## The fix

**Three probes, one leg.** `TestSearchFallbackFindsRealDefinitionWithoutAnyBackend`,
`TestSearchFallbackKeywordFindsRealTextWithoutRG` and
`TestSearchFallbackMissIsAnEmptyAnswerNotAnError` all walk the same whole-checkout
scope with no backend on PATH, so all three inherited the tool's clock and all three
failed together under the tightened knob:

```
--- FAIL: TestSearchFallbackFindsRealDefinitionWithoutAnyBackend (1.00s)
    search_fallback_test.go:93: CodeSearch with no backend on PATH returned an error (code_search: timed out after 1s)
--- FAIL: TestSearchFallbackMissIsAnEmptyAnswerNotAnError (1.00s)
    search_fallback_test.go:167: a miss must not be an error, got code_search: timed out after 1s
```

(The keyword probe was the one FILED; the other two are the same defect, and a fix
that covered only the filed one would have left the suite red on the same host.)

The probes now carry their OWN budget and call the already-existing, already-tested
inner entry points (`codeSearchWithTimeout` / `keywordSearchWithTimeout`) instead of
the production wrappers (`search.go:60–88` — the wrappers are one line each:
`return codeSearchWithTimeout(scope, symbol, FileToolTimeout())`):

```go
hits, dropped, err := codeSearchWithTimeout(searchFallbackScope(root), symbol, searchFallbackProbeTimeout(t))
hits, _, err := keywordSearchWithTimeout(scope, "scanTextHits", searchFallbackProbeTimeout(t))
```

- `searchFallbackProbeBudget = 45 * time.Second` — 2× the worst measurement that
  exists (22.5 s) and 1.5× the production clock, so a heavy host is *measured*
  rather than assumed idle.
- `searchFallbackProbeTimeout(t)` additionally reads the runner's own
  `t.Deadline()` and, when less than budget + `searchFallbackProbeDeadlineReserve`
  (5 s) remains, **skips naming the numbers** — the probe must not be killed
  mid-scan by `go test -timeout`, which would report "test binary timed out" and
  hide which leg hung.
- `searchFallbackProbeBudgetCeiling = 2 * DefaultFileToolTimeout` bounds the budget
  from ABOVE, so "big" cannot become "tolerant of a hung scanner".

Rule 1/13: every duration is a named constant, and the ceiling is derived from the
one definition of the production clock (`DefaultFileToolTimeout`), never a second
literal 30.

**Deliberately NOT read from `GOPI_TOOL_TIMEOUT_SEC`.** The knob bounds the
user-facing call; a probe inheriting it lets an operator (or a sibling suite) move
the tool's contract and turn this assertion red from outside the file. The
production default is untouched — this change is test-surface only.

## The assertions did not move

`git diff` on this file is three replaced call lines (one per probe) plus added
constants/comment. Every assertion is byte-identical: `err != nil` → fail,
`len(hits) == 0` → fail, and the loop requiring a hit under
`internal/tools/search_fallback.go` (keyword) / a real definition of
`scanSymbolHits` (code_search) plus a non-zero `LineNo`.

## Anti-regression pin (both directions)

`TestSearchFallbackProbeBudgetExceedsTheProductionClock`:

- budget ≤ production clock → fail;
- budget > ceiling → fail;
- non-positive deadline reserve → fail.

Red proofs (mutation, restored and verified identical to its pre-mutation backup,
`sha256 9a0a8b08f52966f2e371e4cf2f39526929584b73601e576131bac28ecd8ee29c`):

```
budget=10s → "probe budget 10s does not exceed the production tool clock 30s"
budget=90s → "probe budget 1m30s exceeds the ceiling 1m0s — a hung scanner would be waited out"
```

## Why not the alternatives

- **Raise the production default / add a knob**: the 30 s default is the tool's
  contract for a user-facing call, and the epic forbids touching it. A test-local
  knob would be a second, unmeasured answer to the same question (Rule 2).
- **Shrink the probe's scope** (scan a subtree, not the whole checkout): it would
  stop proving the thing the epic asked for — that the no-rg leg answers over the
  REAL repository, hitting a real file. Budget, not scope, was the wrong quantity.
- **Retry the whole probe until it fits**: hides a genuinely slow host behind a
  loop instead of reporting it (Rule 15's silent-failure shape), and would multiply
  a 40 s+ leg inside a shared suite.
