<!-- doc-class: historical -->
# Task Report: review-fix-low

Fix 2 low-priority bugs from the dual-model (DS/GLM) cross-review Top5 list —
the remaining low-severity items after the 3 Medium fixes (merge 2a9bb50).

## Fix 1 — [UX] parseProxyArgs double-dash error text

**File:** `cmd/agent/cli_proxy.go` (commit `4ad90ab`)

- **Bug** (GLM finding): `fmt.Errorf("unknown flag --%s", unknown[0])`
  prepended `--` to a token that already carries a leading dash (the `default`
  case only appends args whose first char is `-`). Result: `gopi proxy --foo`
  reported the misleading `unknown flag ---foo` (three dashes).
- **Fix:** `fmt.Errorf("unknown flag %s", unknown[0])` — report the token as-is.
  Now `gopi proxy --foo` errors with exactly `unknown flag --foo`.
- **Test:** `cmd/agent/cli_proxy_test.go`
  - `TestParseProxyArgs_UnknownFlagSingleDash` — asserts error text is exactly
    `unknown flag --foo` (two dashes) and that `---foo` never appears.
  - `TestParseProxyArgs_ValidArgs` — guards the happy path (`--upstream`,
    `--openai-key`, `--verbose`).

## Fix 2 — [死代码] compact.go dead first-pass recomputation

**File:** `internal/agent/compact.go` (commit `eb7dc97`)

- **Bug** (DS⑥ + GLM✓): `boundSummaryInputMax` had an 8-line first pass that
  computed `marker → remaining → headChars → tailChars → omitted`, then a
  second pass recomputed the same variables from the corrected marker. All
  first-pass outputs were overwritten except `omitted`.
- **Fix:** replaced the whole chain with the single value that actually feeds
  the marker message — `omitted` derived directly from the provisional budget
  (`max - len(unexpanded marker)`, floor `max`) — and kept **one** real
  head+tail computation. The redundant recompute block is gone.
- **Correctness:** performed a differential test of the new function vs the
  original across caps `{1..20000}` and input sizes `{0..5000}` with varied
  head/tail strings — **byte-identical output** in every case. Existing compact
  tests (`TestBoundSummaryInput_CapsAndKeepsHeadTail` et al.) stay green.

## Not Done
- Nothing deferred. Both listed low-priority bugs are fixed and committed.
  Med-higher findings (`--keep-thinking`, edit.go atomic write, parseTTF
  bounds) were already fixed in the previous batch (merge 2a9bb50).

## Verification
- Build: `go build ./...` — **OK**
- Tests: `go test ./...` — **all green** (cmd/agent 8.4s, internal/agent 20.1s,
  tools 83.8s incl. real 120s bash-timeout regressions; checkpoint/config/
  deepseek/gopilog/proxy/text2img all ok)
- gofmt / go vet: **clean** on all modified files
- Commits (branch `feat/review-fix-low`, worktree `~/workspace/gopi-fix-low`):
  - `4ad90ab` fix(proxy): parseProxyArgs no longer double-dashes unknown flag
  - `eb7dc97` refactor(agent): drop dead recompute block in boundSummaryInputMax
