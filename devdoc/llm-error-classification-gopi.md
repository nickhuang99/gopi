<!-- doc-class: historical -->
# Task Report: LLM account-level error classification (fail fast, never retry a dead account)

Branch: `fix/llm-account-error-classification` · Worktree: `~/workspace/gopi-llm-acc-err` (base `1555b5c`)

## The incident this fixes

`~/.gopi/worker-night-1.log`, 23:00 (quoted verbatim in `devdoc/night-report-20260910.md` §附录 A):

```
Error: LLM call failed (stream: deepseek: API error (status 429):
  {"type":"error","error":{"type":"rate_limit_error","code":"1113",
   "message":"[1113][余额不足或无可用资源包,请充值。]"}}
  after 3 retries, fallback: deepseek: API error (status 429): ... after 3 retries)
```

Two independent wastes on a credential that could never answer:

1. **3 retries** — a balance exhaustion was treated as a rate limit (both arrive as HTTP 429).
2. **A same-provider "fallback"** — the agent's stream→non-stream recovery re-sent the request to the *same* endpoint with the *same* dead key. In a single-provider configuration (HP ran GLM only) the fallback is pure wall-clock loss.

Root cause (5-why): symptom = 3 retries + fallback on a dead credential → mechanism = classification was **status-only** (`retryableStatus`/`classifyHTTPError`: 429/5xx transient, 4xx deterministic) → provider signals *account exhaustion* with the **transient** status 429 and puts the real meaning in the **body** (`code:"1113"`) → the classification never looked at the body → norm: an error class exists (account-level) that is neither "transient" nor "request-deterministic"; it must be named and must own its own control flow.

## What was implemented

### 1. One named vendor-code set behind one provider-neutral predicate (`internal/deepseek/error_account.go`)

```go
const vendorAccountCodeGLMBalance = "1113"        // GLM 余额不足或无可用资源包 (arrives as 429)

var vendorAccountErrorCodes = map[string]VendorErrorInfo{
    vendorAccountCodeGLMBalance: {
        Remedy: "top up the provider account or switch to another provider",
        Label:  "account out of balance (no usable resource pack)",
    },
}
```

Adding a provider code is **one map entry** — no predicate edits, no `if vendor` branch anywhere. `isAccountLevelStatus(status, body)` is the single predicate:

- **HTTP semantics first (provider-neutral):** `402 Payment Required` is account-level for *every* provider, no vendor knowledge needed.
- **Vendor account codes second, and only inside a status the request could not have earned:** a 429 (where GLM hides 1113) or another 4xx. **A code appearing in a 5xx envelope stays transient** — a false "account dead" would suppress a retry that a healthy provider needed (tested: `1113 in a 5xx stays transient`).
- The body probe tolerates every wire shape (`{"error":{"code":"1113"}}`, `{"code":"1113"}`, `{"code":1113}`, `{"error_code":"1113"}`) and is strict about the rest: a top-level `"error"` that is not an object is *prose*, never a code (`{"error":"plain string"}` → `""`), so arbitrary text cannot impersonate an account code.

### 2. Retry/fallback call graph (file:line)

```
run / chatTurn
└─ agent.chatOnce                       internal/agent/loop.go:3591   (thinking-budget re-issue loop)
   └─ agent.chatOnceAttempt             internal/agent/loop.go:3626
      ├─ deepseek.ClaudeChatStream      internal/deepseek/claude_client.go:1003   (stream attempt)
      │  └─ Client.retryDo              internal/deepseek/retry.go:183
      │     ├─ [NEW] isAccountLevelStatus → accountLevelError      retry.go:216-217   (sentinel branch)
      │     ├─ [NEW] isAccountLevelStatus → accountLevelError      retry.go:268-269   (raw-4xx branch)
      │     ├─ classifyHTTPError → classDeterministic | classTransient (unchanged)
      │     └─ retryDelay → Retry-After honoured (unchanged, 5c6f5ca behaviour kept)
      ├─ [NEW] deepseek.IsAccountLevelError(streamErr) → SKIP same-provider fallback,
      │        return actionable message           internal/agent/loop.go:3649-3655
      ├─ deepseek.ClaudeChat            internal/deepseek/claude_client.go:873   (non-streaming fallback)
      │  ├─ Client.retryDo              (same gate as above)
      │  └─ [NEW] isAccountLevelStatus → fail fast BEFORE minimal-core  claude_client.go:922-923
      ├─ deepseek.runMinimalCoreDegrade internal/deepseek/minimal_core.go:71
      │  └─ [NEW] isAccountLevelStatus → fail fast (stripping params cannot fix an empty balance)  minimal_core.go:82-86
      └─ [NEW] deepseek.IsAccountLevelError(chatErr) → actionable message  loop.go:3663-3669
```

Classification entry points: `internal/deepseek/error_account.go:167` (`isAccountLevel(err)`), `:201` (`isAccountLevelStatus(status, body)`), `:308` (`AccountLevelMessage(err)`), exported for the agent layer via `IsAccountLevelError` / `AccountLevelMessageOf` (`retry.go:325`,`:331`).

### 3. The wire error carries the raw status+body (why the agent layer *can* decide)

`accountLevelError` (`retry.go:299`) wraps an exported `*HTTPStatusError{Status, Body, ProviderBase}` whose `Error()` **is** the operator message. Every layer above returns it unchanged, so "do not retry, do not fall back to the same provider" is a property of the error itself, not a convention each caller must remember. `errors.As`/`errors.Is` keep working through the wrapper.

### 4. Operator-facing message (condition + remedy, greppable anchor)

```
[GOPI][account-level] ACCOUNT-LEVEL ERROR: open.bigmodel.cn account unusable — account out of balance
(no usable resource pack) (code 1113) [HTTP 429]. Not retrying and no provider fallback can help:
top up the provider account or switch to another provider.
```

(Shape: `[GOPI][account-level] ACCOUNT-LEVEL ERROR: <provider-host> account unusable — <label> (code <vendor-code>) [HTTP <status>]. Not retrying and no provider fallback can help: <remedy>.` — the provider host comes from the failing base URL, so a credential swap is visible in the message.)

## Not Done / deliberately out of scope

- **Vendor account codes beyond GLM 1113.** The epic named one code; the map is the extension point. Codes I could *not* verify from a primary source were **not** invented (web search for the Zhipu code table returned an AI-synthesised answer whose 1301–1304 meanings conflict with the GoPi codebase's own notes — see below). Only 1113 is pinned by the incident's raw evidence.
- **`RecoverableHint` / session_end taxonomy for account-level errors.** ~~`FailureSnapshot.RecoverableHint()` (`internal/deepseek/lasterr.go:151`) still derives from status alone, so a 429-1113 failure is reported as recoverable and the crash dump says `4xx/http_status:429` with no account-level marker.~~ **RESOLVED** on `fix/llm-error-taxonomy`: `CategoryAccount = "account"` derived from the same `isAccountLevelStatus` predicate, `RecoverableHint()` false for it, and the operator `remedy` text on the session_end block, the crash dump and the ledger digest — see `devdoc/llm-error-taxonomy-report.md`.
- **Provider-level automatic switching.** The epic asks only that the *same*-provider fallback be skipped and the remedy be surfaced; "switch provider automatically" is a provider-selection design (settings-level), not a retry-path change.
- **`error_1214.go`'s ad-hoc ladder** is untouched (its own root cause is a separate documented saga).

## Verification

Build:

```
$ go build ./...
(no output — OK)
```

Commit: `316edde` on `fix/llm-account-error-classification` (worktree `/home/user/workspace/gopi-llm-acc-err`; the commit is already visible from the main repo — worktrees share refs).

Whole-repo regression (gate): `go test -count=1 ./...` → every package `ok`
(`cmd/agent 116.9s`, `internal/agent 124.1s`, `internal/deepseek 2.1s`, `internal/tools 215.8s`, …), exit 0.

Deepseek corpus (acceptance gate):

```
$ go test -count=1 ./internal/deepseek/
ok  	github.com/nick/gopi/internal/deepseek	2.166s
```

Targeted raw output (`go test -count=1 -v`):

```
--- PASS: TestIsAccountLevel_StatusAndCodeMatrix (0.00s)          [15 subtests, incl. GLM 1113-in-429, genuine 429, 1113-in-5xx]
--- PASS: TestAccountLevel_NotRetriedAndSentOnce (0.00s)
--- PASS: TestAccountLevel_StreamingPathNotRetried (0.00s)
--- PASS: TestGenuineRateLimit_StillRetried (0.00s)
--- PASS: TestServerError_StillRetried (0.00s)
--- PASS: TestAccountLevelMessage_402ProviderNeutral (0.00s)
--- PASS: TestSentinel429_RetryAfterHeaderHonored (2.00s)          [pre-existing regression guard — Retry-After still honoured]
--- PASS: TestProviderLabel (0.00s)
--- PASS: TestParseVendorErrorCode_Shapes (0.00s)
--- PASS: TestIsAccountLevelError_ErrorShapes (0.00s)
ok  	github.com/nick/gopi/internal/deepseek	2.032s
```

Agent layer (same-provider fallback skip + fallback regression):

```
--- PASS: TestAgent_Run_AccountLevelSkipsSameProviderFallback (0.01s)
--- PASS: TestAgent_Run_GenuineRateLimitStillFallsBack (0.01s)
--- PASS: TestAgent_Run_StreamFallback (0.61s)                     [pre-existing stream→non-stream fallback still works]
ok  	github.com/nick/gopi/internal/agent	0.662s
```

Full agent corpus: `ok github.com/nick/gopi/internal/agent 104.510s`.

Acceptance-criterion mapping:

| Criterion | Test | Result |
|---|---|---|
| insufficient balance is NOT retried | `TestAccountLevel_NotRetriedAndSentOnce` (asserts **1** HTTP send with `MaxRetries=3`) | PASS |
| genuine 429 IS retried, Retry-After honoured | `TestGenuineRateLimit_StillRetried` (`MaxRetries+1` sends) + `TestSentinel429_RetryAfterHeaderHonored` (measured wait ≥ 1s against a 2s Retry-After) | PASS |
| same-provider fallback skipped on account-level | `TestAgent_Run_AccountLevelSkipsSameProviderFallback` (**1** HTTP send end-to-end, no `"fallback:"` wrap) | PASS |
| operator text names condition + remedy | asserted substring-by-substring in the two tests above | PASS |
| no real sleeps / no live API | httptest everywhere; retry delays `time.Nanosecond`; the only wall-clock assertion is the pre-existing deterministic Retry-After test | PASS |
| `go test -count=1 ./internal/deepseek/` | raw output above | PASS |

## Findings filed, not fixed (Rule: out-of-scope → ticket)

- ~~`deferred`~~ **done on `fix/llm-error-taxonomy`** — `CategoryAccount` ("account") + `recoverable_hint:false` + the operator `remedy` text on session_end / crash dump / ledger digest (`devdoc/llm-error-taxonomy-report.md`).

## Memory + workflow notes

- `memory(action=store)`: account-level classification recipe (vendor code map + 402-first predicate + fail-fast wiring + nanosecond-delay test pattern) stored for reuse.
- **Workflow feedback:** the pre-existing corpus mixed four "recovery ladders" (1214 guard, minimal-core degrade, agent stream→non-stream fallback, retry) and the account-level gate had to be inserted at **four** seams; a fifth would have been missed by inspection alone — the 402 test failing with "2 sends" is what surfaced `runMinimalCoreDegrade`'s extra send. Lesson: when adding a failure class, drive it end-to-end through the *public* client method and assert the HTTP send COUNT, not just the returned error.
