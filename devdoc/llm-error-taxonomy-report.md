<!-- doc-class: historical -->
# Task Report: account-level provider failures in the crash-dump / session_end taxonomy

Branch: `fix/llm-error-taxonomy` (new branch off `fix/llm-account-error-classification`
@8d204e1 — `error_account.go` is not on master). Worktree: `~/workspace/gopi-llm-taxonomy`.

Base note (honest): the branch was later rebased onto master's tip `4843f6a`
(which is the merge of `fix/llm-account-error-classification`), so the epic's
base branch **is** an ancestor of every commit here — `git merge-base --is-ancestor
4843f6a HEAD` passes, and `git log --oneline -1 fix/llm-account-error-classification`
resolves to that same merge commit `4843f6a` (the branch name itself is gone;
its content is the merge). `error_account.go` is **untouched** by this branch
(`git diff 4843f6a HEAD -- internal/deepseek/error_account.go` is empty) —
it is reused as the single predicate, exactly as the epic requires.

## Problem (what was wrong)

The retry path already fails fast on account-level provider errors, but the
taxonomy that feeds the crash dump / session_end error block / ledger digest
still derived everything from the HTTP status alone. The GLM "账户余额不足"
death of the 23:00 HP worker (HTTP **429** + `{"error":{"code":"1113"}}` —
`devdoc/night-report-20260910.md`) was therefore recorded as:

```json
{ "category": "4xx", "http_status": 429, "recoverable_hint": true }
```

i.e. **"rate limited, retry later"** — telling a resumer to retry an account
that is out of balance, and hiding the one actionable fact (top up / switch
provider) from the operator.

## Completed

### 1. `internal/deepseek/lasterr.go` — the new taxonomy value + remedy text

- **`CategoryAccount ErrorCategory = "account"`** (new value; every existing
  category string is untouched, so the PA/resumer contract stays stable).
- `FailureSnapshot.Category()` checks account-level **first**:
  `IsAccountLevelStatus(f.StatusCode, []byte(f.Body))` → `account`. It is a
  window onto the *existing single predicate* (`isAccountLevelStatus`, the same
  one `retryDo` / `ClaudeChat` / the agent's fallback skip consult) — no second
  copy of the vendor-code knowledge (Rule 13/14).
- `FailureSnapshot.RecoverableHint()` returns **false** for `account`, checked
  **before** the existing `CategoryHTTP4xx` + 429 clause — that ordering is the
  entire fix: a 429 wearing an account code is not a rate limit. Genuine 429s
  keep their path (the `CategoryHTTP4xx` branch is unchanged).
- **`IsAccountLevelStatus(status, body)`** — exported wrapper so the agent layer
  AND the tests use the one definition.
- **`FailureSnapshot.RemedyText()`** — the operator condition + remedy for an
  account-level snapshot, `""` for everything else. Built by handing the raw
  status+body to `AccountLevelMessage`, so the dump and the retry path word the
  failure **identically** (reuse, not a re-implementation).
- **`FailureSnapshot.Provider`** — new snapshot field (base-URL host via
  `ProviderLabelForBase`), so the remedy can name *which* account is dead; a
  dump travels without the client that produced it.

### 2. `internal/agent/session.go` + `crashdump.go` — the evidence surfaces

`ErrorRecord.Remedy` (new, `omitempty`) carries the operator text and lands on
**all three** consumers of the taxonomy — enumerated before changing anything:

| consumer | field written | reader |
|---|---|---|
| `SessionEndBlock()` (session_end `error` block) | `error.remedy` | resumer / `gopi log` |
| `WriteCrashDump()` (`<id>.dump.json`) | `remedy` | PA / resumer |
| `SummaryDigest()` (task-ledger failed summary) | `remedy="…"` | notify pipe → PA |

Enumeration (every `Category()`/`RecoverableHint()` consumer, `grep -rn` over
`internal/ cmd/` — no other reader of the crash-dump JSON exists in Go; the
dump's other consumers are the resumer/PA reading the file):

- `internal/deepseek/lasterr.go` — `Category()`, `RecoverableHint()`,
  `RemedyText()` (**changed**: new value + account-first derivation);
- `internal/agent/crashdump.go:62` `collectErrorRecord` — the single collection
  path (**changed**: also fills `Remedy`);
- `internal/agent/session.go:422` `SummaryDigest` (**changed**),
  `:475` `SessionEndBlock` (**changed**), `:500` `WriteCrashDump` (**changed**);
- `internal/agent/taskregister.go:240/:286` `finishSessionTaskRecorded` /
  `finishSessionTask` — append the digest to the ledger row (**changed**: the
  resume hint follows the taxonomy, see below);
- `internal/agent/loop.go:2648/:3459` — the two `collectErrorRecord` entry
  points (unchanged: they consume an `*ErrorRecord`, not the taxonomy);
- tests: `internal/agent/crashdump_test.go` `TestErrorCategoryMatrix`
  (**changed**) + the two new evidence tests; `internal/deepseek/lasterr_test.go`
  (**changed**, 4 new tests).

Every pre-existing category string (`network`/`4xx`/`5xx`/`vendor`/`panic`/
`unknown`) and every pre-existing JSON key is untouched — `account` is purely
additive, and `remedy` is emitted only for account-level failures.

`collectErrorRecord` populates it from the snapshot (`tailRunes(..., remedyTextMax)`),
new named constant `remedyTextMax = 1024` (Rule 1). The field is emitted **only
when non-empty**, so the dump/session_end JSON of every non-account failure is
byte-identical to before — the account failure is the only shape that grows.

### 4. `internal/agent/taskregister.go` — the resume verdict follows the taxonomy

The ledger's FAILED hint used to be unconditional: **every** failed row read
``FAILED — recoverable: `gopi resume <id>` replays session context``. For an
account-level death that is the same misreport one layer deeper: the operator
was told to replay a task into an account that is out of balance.

- `autoTaskTerminalHintFormat` (new named constant) is the account-level
  verdict — `FAILED — NOT recoverable by resume: … would replay into the same
  failure — fix the cause first`. It keeps the JSONL/raw-log pointer (the
  evidence still matters) but flips the instruction.
- `autoTaskResumeHintFormat` — the existing string, **byte-identical**, is what
  every non-account failure still gets.
- Selection is `failure.Category == string(deepseek.CategoryAccount)` — the one
  taxonomy value, not a re-derived status check (Rule 13/14).
- The failure is now collected **before** the hint is built (it used to be
  collected after), so the digest is still one pass, one record.

The account remedy from §2 lands immediately after the hint
(`… NOT recoverable by resume: … — error(category=account … remedy="…")`), so
a PA reading only the ledger summary sees **verdict + cause + action**.

### 5. Tests (the acceptance gates)

- `internal/agent/crashdump_test.go`:
  - `TestErrorCategoryMatrix` — two new rows (`429`+1113 → `account`/false;
    `402` → `account`/false) sitting directly under the existing
    `429`+`"rate limited"` → `4xx`/true row, so the contrast is the test.
  - `TestAccountLevelEvidence_EndToEnd` — drives the **real client** against an
    httptest server returning the verbatim GLM body, then asserts category
    `account`, `recoverable_hint:false` and the remedy text on the digest, the
    dump file and the session_end block.
  - `TestGenuine429Evidence_NoRemedy` — regression: a genuine 429 keeps
    `4xx`/recoverable true and the dump grows **no** `remedy` key.
- `internal/deepseek/lasterr_test.go`:
  - `TestFailureSnapshot_AccountCategory` (429+1113 → account/false + remedy
    names code, condition and action),
  - `TestFailureSnapshot_Genuine429StaysRetryable` (regression),
  - `TestFailureSnapshot_402PaymentRequiredIsAccount` (vendor-neutral path),
  - `TestLastErrDetail_AccountLevelThroughClient` (the category survives the
    real send path, not just a constructed snapshot).
- `internal/agent/taskregister_test.go`:
  - `TestFailedLedgerHint_AccountLevelNotResumable` — the ledger row of an
    account-level failure carries the NOT-resumable verdict + `category=account`
    + `remedy=`;
  - `TestFailedLedgerHint_GenuineFailureKeepsResumePointer` — regression: every
    other failure keeps the original resume wording verbatim.

### 6. Docs

- this report;
- `devdoc/crash-dump-design.md` — taxonomy list + remedy field + digest shape.

## Not Done
- **No new vendor codes.** Only GLM 1113 exists today; adding one is still a
  single `vendorAccountErrorCodes` map entry (documented, not exercised).
- **`BackfillOrphanCrashDumps`** still writes `category:"crash"` /
  `recoverable_hint:true` — by construction it has no request, no response and
  no provider, so it cannot be account-level. Untouched.
- The `--chat` / human log view does not specially render `remedy`; it surfaces
  through the existing error-block dump (`RawEvent` has no error field — a
  pre-existing gap recorded by memory #1289, out of scope here).
- No change to `staticcheck`-level refactors and no new dependencies (zero-dep).

## Verification

Raw acceptance evidence (the two shapes the epic names, printed from the
taxonomy itself):

```
429+1113 -> category="account" recoverable=false remedy="[GOPI][account-level] ACCOUNT-LEVEL ERROR: provider account unusable — account out of balance (no usable resource pack) (code 1113) [HTTP 429]. Not retrying and no provider fallback can help: top up the provider account or switch to another provider."
429 genuine -> category="4xx" recoverable=true remedy=""
```

- Build: `go build ./...` → OK
- Tests: `go test -count=1 ./internal/deepseek/ ./internal/agent/` →

```
ok  	github.com/nick/gopi/internal/deepseek	2.106s
ok  	github.com/nick/gopi/internal/agent	134.550s
```

  (the six acceptance tests verbatim: `--- PASS:
  TestFailureSnapshot_AccountCategory_OpenAIWireShape`,
  `TestFailureSnapshot_Genuine429StaysRetryable`,
  `TestAccountLevelEvidence_EndToEnd`, `TestGenuine429Evidence_NoRemedy`,
  `TestErrorCategoryMatrix`, `TestFailedLedgerHint_AccountLevelNotResumable`,
  `TestFailedLedgerHint_GenuineFailureKeepsResumePointer`)
- Commit: `5324130` (taxonomy + evidence surfaces) and `2cb7d9c`
  (ledger resume hint + acceptance-shape test)
- Whole repo (earlier round): `go test -count=1 ./...` → no failing package.

## Deliverable declaration

- Branch: `fix/llm-error-taxonomy` in `/home/user/work/GoPi` (worktree
  `~/workspace/gopi-llm-taxonomy`, shared object store — the commit is already
  on the branch in the main repo, **no push needed**).
- Recovery: `git show fix/llm-error-taxonomy:devdoc/llm-error-taxonomy-report.md`
  and `git log --oneline fix/llm-error-taxonomy`.
