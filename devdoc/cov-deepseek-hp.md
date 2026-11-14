<!-- doc-class: historical -->
# Task Report: internal/deepseek function-level coverage closure (cov-deepseek-hp)

**Branch:** `feat/cov-deepseek-hp` · **Worktree:** `~/workspace/gopi-cov-deepseek-hp`
**Base:** `f0106d7` (master) · **Scope:** TESTS ONLY — zero product-code changes (Rule 4)

## Completed

Closed every function-level coverage gap in `internal/deepseek` reported by the
periodic audit. New file: **`internal/deepseek/hp_coverage_test.go`** (~700
lines, 14 test functions / 24 subtests).

`python3 scripts/func_coverage.py --pkg internal/deepseek --uncovered` went from
**27/39 (69.2%)** to **39/39 (100.0%)** — zero uncovered exported functions.

### The 12 functions the audit listed (public deliverable list), and their tests

| # | Function (definition) | Contract pinned | Test |
|---|---|---|---|
| 1 | `AccountLevelMessage` — `error_account.go:308` | operator text: marker + label + vendor code + status + remedy; `""` for non-account | `TestHPAccountLevelMessage_Shapes` (4 subtests) |
| 2 | `IsAccountLevelStatus` — `lasterr.go:202` | 402 provider-neutral; vendor code only inside 429/4xx; 5xx stays transient | `TestHPIsAccountLevelStatus_ExportedMatrix` (7 rows + nil body) |
| 3 | `LookupVendorAccountCode` — `error_account.go:219` | known code → label+remedy; unknown code / HTML / empty → zero value,false | `TestHPLookupVendorAccountCode` |
| 4 | `MarshalJSON` — `types.go:62` | nested `{id,type:"function",function:{name,arguments}}`, arguments a JSON-encoded **string**; unmarshalable args → error | `TestHPToolCall_WireRoundTrip`, `…_MarshalNoArguments`, `…_MarshalUnmarshalableArguments` |
| 5 | `UnmarshalJSON` — `types.go:83` | streaming fragment tolerated (RawArgs kept, Arguments → empty map); absent arguments → non-nil map; non-object frame → error | `TestHPToolCall_Unmarshal_FragmentAndMalformed` (3 subtests) |
| 6 | `AddUsage` — `types.go:135` | all four counters accumulate across calls | `TestHPCostTracker_AddUsageSummary` |
| 7 | `Summary` — `types.go:143` | exact operator line; derived-miss branch (`miss==0 && hit>0` → `prompt-hit`); zero value | `TestHPCostTracker_AddUsageSummary`, `…_SummaryMissApproximation` |
| 8 | `Unwrap` — `retry.go:320` | wrapper's `Error()` IS the operator message; `errors.As` still reaches `*HTTPStatusError` | `TestHPAccountLevelErrorValue_Unwrap` |
| 9 | `ResolveClaudeAPIKey` — `claude_client.go:636` | `ANTHROPIC_AUTH_TOKEN` > `DEEPSEEK_API_KEY` > `cfg.APIKey` > error naming the setting | `TestHPResolveClaudeAPIKey` (4 subtests) |
| 10 | `ResolveClaudeBaseURL` — `claude_client.go:654` | `ANTHROPIC_BASE_URL` > `DEEPSEEK_BASE_URL` > `cfg.BaseURL` > legacy default; derived path check | `TestHPResolveClaudeBaseURL` (4 subtests) |
| 11 | `NewClaudeClient` — `claude_client.go:670` | happy (env), settings-file lane (incl. `messages_path`), malformed settings → error, missing credential → error | `TestHPNewClaudeClient_EnvOverrides` / `_SettingsFile` / `_MalformedSettings` / `_MissingCredential` |
| 12 | `RecordForTest` — `lasterr.go:83` | seam records status+body+`/test/path`, flips `HasFailed`, timestamp; `Stream` round-trips | `TestHPLastHTTPFailure_RecordForTest` |

### Boundary + error paths covered (acceptance item)

- **Boundary:** nil `Arguments` → `"arguments":"null"` and the read side
  normalizing it to an empty map; `Summary` derived-miss + zero value; bare 402
  with a nil body; empty/HTML body in `LookupVendorAccountCode`; `Content[0]=='"'`
  string-vs-array shape is exercised through the fixture bodies; nil `*Config` in
  both resolvers.
- **Error:** `MarshalJSON` with an unmarshalable argument value; non-object
  `UnmarshalJSON` frame; unresolvable credential; malformed settings file;
  `NewClaudeClient` with no credential; 4xx/5xx/genuine-429 bodies that must
  yield `""` from `AccountLevelMessage`.

### Determinism notes (acceptance item)

1. **No real sleeps.** Every retry-enabled test sets `InitialDelay` and
   `MaxDelay` to exactly `time.Nanosecond` via `hpNanoRetryConfig()`
   (`JitterFraction: 0`), so a retry clause is observable through the send
   counter but costs no measurable wall time. (Same convention already used by
   `error_account_test.go` / `error_matrix_test.go`.)
2. **No live API calls.** The one wire path in this file
   (`client_composed_sentinel_body`) is served by `httptest.NewServer`; every
   other test is pure unit.
3. **No timing race.** The send counter is read only AFTER the synchronous
   `ClaudeChat` call returns — the send happens inside the operation closure
   `retryDo` has already joined, so no `time.Sleep`/poll is needed.
4. **Hermetic against the developer's shell.** The credential env vars are
   forced to a known state with `t.Setenv` (`hpClearCredentialEnv`), which
   restores automatically at cleanup — the tests pass whichever key is exported.
   `GOPI_SETTINGS` points at a temp file (empty or synthetic) so no developer
   settings.json is read.
5. **Proven non-flaky:** 3× `go test -count=1` plus one `go test -count=3`.

## Not Done

- **No product-code fixes** (task constraint) — see Bugs filed below; the one
  real defect found is filed as a bug ticket instead of patched.
- `func_coverage.py`'s metric is name-presence in `*_test.go` (documented as
  deliberately conservative); this task satisfies it *and* adds real
  behavioural assertions — the tests fail if the behaviour regresses (verified
  during development: three real assertion failures were hit and fixed before
  the green runs).
- No `go test ./...` repo-wide run: out of scope for this worktree (other lanes
  are landing in the same package); the package-level gate is green.

## Verification

```
$ python3 scripts/func_coverage.py --pkg internal/deepseek --uncovered
package                                     funcs    cov    pct  tests
--------------------------------------------------------------------------
internal/deepseek                              39     39 100.0%
--------------------------------------------------------------------------
TOTAL                                          39     39 100.0%

Uncovered exported funcs (by package):

NOTE: conservative metric — a func counts as covered only if its name
appears in a *_test.go file of the same package. Target: 100%.
```

Raw `go test -count=1 ./internal/deepseek/` — run 1:

```
=== run 1 ===
ok  	github.com/nick/gopi/internal/deepseek	2.043s
```

Raw `go test -count=1 ./internal/deepseek/` — run 2:

```
=== run 2 ===
ok  	github.com/nick/gopi/internal/deepseek	2.036s
```

Repeat-stability (`-count=3`, 3 identical runs in one process):

```
=== count=3 ===
ok  	github.com/nick/gopi/internal/deepseek	6.231s
```

`go vet ./internal/deepseek/`: clean. `gofmt -l internal/deepseek/`: empty.
Targeted run: `go test -count=1 -run TestHP -v ./internal/deepseek/` → 14 PASS
+ 14 subtest PASS, 0 FAIL.

### code_search evidence (definition sites, file:line)

`code_search` resolved every target symbol to its definition (this is the
"contract" anchor for each test — the sink the assertions were written against):

```
NewClaudeClient   internal/deepseek/claude_client.go:670  func  func NewClaudeClient() (*Client, error)
AddUsage          internal/deepseek/types.go:135          func  func (ct *CostTracker) AddUsage(u Usage)
MarshalJSON       ./internal/deepseek/types.go:62         func  func (t ToolCall) MarshalJSON() ([]byte, error)
Unwrap            internal/deepseek/retry.go:320          func  func (e *accountLevelErrorValue) Unwrap() error
```

The remaining targets were located with `keyword_search`/`grep -n "^func "` over
the package sources (the ctags index covers the package inconsistently):
`LookupVendorAccountCode` `error_account.go:219`, `AccountLevelMessage`
`error_account.go:308`, `IsAccountLevelStatus` `lasterr.go:202`,
`RecordForTest` `lasterr.go:83`, `UnmarshalJSON` `types.go:83`, `Summary`
`types.go:143`, `ResolveClaudeAPIKey` `claude_client.go:636`,
`ResolveClaudeBaseURL` `claude_client.go:654`.

Constants referenced instead of bare literals (Rule 1b/13):
`accountLevelMessagePrefix`, `providerLabelGeneric`, `claudeMessagesPath`,
`glmBalance429Body`, `rateLimit429Body`, `DefaultRetryConfig()`,
`DefaultVisionBaseURL` (contrast), `errorBodyProbeMax`-adjacent caps.

## Bugs filed (Rule: no product-code fixes in this task)

**1 → task `01a08c91-a1b7-793f-badd-d40a79eede13`** (tags: bug, repo
`/home/user/work/GoPi`) — *inconsistent non-200 body caps in `retryDo`*.

`internal/deepseek/retry.go:256` reads the raw non-200 body with
`io.ReadAll(io.LimitReader(resp.Body, 8192))` while `claude_client.go:899` /
`:1029` read `65536` before building the sentinel. A 429 whose account-level
code sits past byte 8192 is therefore **not** classified account-level on the
raw-response branch → retried 3× (and a same-provider fallback would re-hit the
dead account), while the sentinel branch — the one both shipping callers use —
classifies it correctly.

Measured probe (deterministic, no network; 9053-byte body with
`"code":"1113"` beyond offset 8192):

```
RAW-resp branch: calls=4 err=deepseek: API error (status 429): ... after 3 retries
SENTINEL branch (real callers): calls=1 err=[GOPI][account-level] ACCOUNT-LEVEL ERROR: ... (code 1113) [HTTP 429]. Not retrying...
```

Latent today (`ClaudeChat` and `ClaudeChatStream` both pass the sentinel), but
the raw branch is the documented public shape of `retryDo` for
"direct-retryDo callers that return (resp, nil) on non-200". Suggested fix (in
the ticket): one named constant (`non200BodyProbeMax = 64 * 1024`) used at all
three sites plus a regression test classifying an 8192+ account code on both
branches.

## Memory entries

See `memory(action=search)` at task start and the `memory(action=store)` entries
recorded at the end of this task (metric behaviour, determinism recipe, the
body-cap defect).

## Workflow feedback

- The audit metric counts a function only when its **name** appears in a
  `*_test.go` in the same package — nine of the twelve listed functions were in
  fact already *behaviourally* exercised indirectly (e.g. `AddUsage`/
  `Summary` via the agent loop, `Unwrap` via `errors.As`), so the gap was
  largely a **naming/contract-visibility** gap. Writing direct tests still paid
  off: the direct `Summary` boundary, the raw-`Unwrap` chain and the nil-body
  `IsAccountLevelStatus` case each added a real assertion, and probing the
  contract is exactly what surfaced the body-cap bug.
- `code_search`'s index covered `internal/deepseek` inconsistently: 4 of 12
  symbols resolved, the rest needed `keyword_search`. Definition lookups in this
  package are cheaper with `grep -n "^func " internal/deepseek/*.go`.
- Working in the pre-created linked worktree
  (`~/workspace/gopi-cov-deepseek-hp`) meant zero clone/delete I/O and the
  commit lands on the branch directly — no push step.

## Commit

`feat(cov-deepseek-hp): close function-level coverage gaps in internal/deepseek`
— `internal/deepseek/hp_coverage_test.go` + this devdoc, on branch
`feat/cov-deepseek-hp`.

**Deliverable location (Rule 6d):**
branch `feat/cov-deepseek-hp` in `~/work/GoPi` (worktree
`~/workspace/gopi-cov-deepseek-hp`) — recover with
`git show feat/cov-deepseek-hp:devdoc/cov-deepseek-hp.md` and
`git show feat/cov-deepseek-hp:internal/deepseek/hp_coverage_test.go`.
