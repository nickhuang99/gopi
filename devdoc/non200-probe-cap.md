<!-- doc-class: historical -->
# Task Report: unified non-200 body-probe cap (`non200BodyProbeMax`)

**Epic**: fix/non200-probe-cap — one named constant for the non-200 body probe,
plus a regression test proving an over-8192 account code is seen on both
branches.

## Completed
- **ONE named constant** `non200BodyProbeMax = 64 * 1024` declared in
  `internal/deepseek/retry.go` (with the drift history in its doc comment —
  GOPI.md Rule 1a/Rule 13: one definition site, no bare literals).
- **Referenced at every probe site** (no duplicated value remains):
  - `internal/deepseek/retry.go:273` — retryDo's RAW `*http.Response` non-200
    branch (was the drifted `8192`).
  - `internal/deepseek/claude_client.go:899` — ClaudeChat (non-streaming)
    sentinel branch (was `65536`).
  - `internal/deepseek/claude_client.go:1029` — ClaudeChatStream sentinel
    branch (was `65536`).
  - `internal/deepseek/claude_client.go:1101` — `claudeStreamLadderRoundtrip`
    (the 1214-guard/ladder probe; same body-probe class, was `65536`).
- **Regression tests** `internal/deepseek/non200_probe_cap_test.go`:
  - `TestNon200ProbeCap_RawBranch_AccountCodeBeyond8192` — drives retryDo with
    a hand-built `*http.Response` (no server): 429 whose `"code":"1113"` sits
    at offset >8192 → exactly **1 call** + account-level error.
  - `TestNon200ProbeCap_SentinelBranch_AccountCodeBeyond8192` — same body
    through the public `ClaudeChat` → **1 send** + account-level error.
  - `TestNon200ProbeCap_StreamSentinelBranch_AccountCodeBeyond8192` — same
    through the public `ClaudeChatStream`.
  - `TestNon200ProbeCap_Genuine429StillRetried` — both-ways guard: a genuine
    429 with the same over-8192 padding still retries `MaxRetries+1` times.
  - `TestNon200ProbeCap_NoBareLimitReaderLiteral` — Rule 13 gate: any
    `io.LimitReader(` line in a production `.go` file of this package that does
    NOT reference `non200BodyProbeMax` fails the build (re-inlining is caught).

## Not Done
- The two other `io.ReadAll(io.LimitReader(...))`-style sites in the package
  that read a **complete body without a cap** (`claudeChatLadderRoundtrip`
  `io.ReadAll(resp.Body)`, the success-path reads) were left alone — they are
  not probes with a cap value, so unifying them would be a behaviour change,
  not a constant reference.
- The `8192` mentions that remain in `claude_client.go:82-89` are the
  **max_tokens** truncation-saga comment (an unrelated, historical number), not
  body-cap literals; `claude_client_test.go:838/868` pass max_tokens fixtures.
  The acceptance grep therefore matches comments/fixtures only — no bare
  body-cap literal survives.

## Verification
- Build: `go build ./...` → OK.
- Pre-fix reproduction (the acceptance "FAIL before / PASS after" evidence):
  temporarily restoring `8192` in retry.go's raw branch gives
  `--- FAIL: TestNon200ProbeCap_RawBranch_AccountCodeBeyond8192
   raw branch retried an account-level 429: 4 calls, want 1` — exactly the
  predicted `MaxRetries+1 = 4` calls; with the constant in place the same test
  passes with `calls == 1`.
- Tests: `go test -count=1 ./internal/deepseek/` → ok (whole package, includes
  the pre-existing genuine-429 / 5xx retry-count suites in
  `error_account_test.go`, which stay green — the deeper probe did not make
  large bodies account-level).
- Root cause (5-why): drifted literals (`8192` raw vs `65536` sentinel) →
  `isAccountLevelStatus` only classifies the bytes it is handed → the account
  code past offset 8192 was invisible on the raw branch → the 429 classified
  transient → the dead GLM account was retried `MaxRetries` times
  (`devdoc/night-report-20260910.md`, 23:00 HP worker death).
- Wire behaviour of `ClaudeChat` / `ClaudeChatStream` is unchanged except that
  the raw branch's error/log body now reaches the same 64 KiB depth the
  sentinel branches already used.

## Deliverable / recovery
- **Branch**: `fix/non200-probe-cap` (worktree `~/workspace/gopi-non200-probe`).
- **Commit**: tip of `fix/non200-probe-cap` — read it with
  `git log --oneline -1 fix/non200-probe-cap`; the single commit on the branch
  carries code + tests + this report (a doc cannot pin the hash of the commit
  that contains it — amending in the hash would change it). Already in the
  MAIN repo's object store (worktree-shared, **no push needed**).
  Recovery: `git show fix/non200-probe-cap:devdoc/non200-probe-cap.md`
  (or `git show fix/non200-probe-cap:internal/deepseek/non200_probe_cap_test.go`).
- Files: `internal/deepseek/retry.go`, `internal/deepseek/claude_client.go`,
  `internal/deepseek/non200_probe_cap_test.go`, `devdoc/non200-probe-cap.md`.
