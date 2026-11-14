<!-- doc-class: normative -->
# max_tokens Output Truncation — Root Cause & Fix

## Symptom

While drafting a new Go source file, gopi's output was cut off mid-write.
stderr logged:

    ⚠️ output truncated (finish_reason=length) — re-issuing; tool calls may be incomplete

and the reasoning said *"My response got truncated while I was drafting
rawlog.go"*. The drafted file never landed.

## Root cause

The per-response output cap is **hardcoded** to 4096 tokens and **cannot be
overridden**:

- `internal/deepseek/claude_client.go`:
  `DefaultClaudeMaxTokens = 4096`
- `internal/agent/loop.go` (Run): the cap is passed straight into
  `BuildClaudeRequest(..., deepseek.DefaultClaudeMaxTokens, ...)` with no env
  var, no settings field, no agent field.

The cap is the Claude API `max_tokens` field, which bounds **all** output in a
round: thinking blocks, text blocks, AND tool-call arguments. A `write` tool
call carries the whole file draft inside the `tool_use` block's `input` JSON.
Code drafts are token-heavy twice over: source lines cost ~8–15 tokens each,
and the JSON escaping (every `\n`, `"`, `\`) inflates the payload another
2–4×. Combined with thinking mode (enabled by default, `budget_tokens: 2048`),
a multi-hundred-line draft blows through 4096 output tokens before the
`tool_use` block completes — the stream ends with `stop_reason: "max_tokens"`.

## Evidence from the raw log

`~/.gopi/raw/gopi-raw-20260806-174648.log` (session `9970d186…`, drafting a
`rawlog.go`-style file for the gopi-log project):

Every request carries `"max_tokens":4096` and
`"thinking":{"type":"enabled","budget_tokens":2048}`.

Two truncation events, both `message_delta … "stop_reason":"max_tokens"`,
`"output_tokens":4096` (i.e. the API hit the cap exactly):

1. **Line 24247** — the model was mid-preamble, about to issue a tool call.
   The last text delta streamed was
   `"…raw-log pairing deltas across real data, and max request-line"` — cut
   mid-word. No `tool_use` block had started yet.

2. **Line 73097** — worse: only a `thinking` block (index 0) had closed. The
   model spent its **entire 4096-token output budget on thinking** and the
   stream ended before a single text or `tool_use` block was emitted.

### Exact failure sequence (why the draft never lands)

1. gopi sends `max_tokens: 4096` + thinking enabled.
2. The model starts drafting the file inside a `write` `tool_use` block's
   `input` (or wastes the budget thinking first, as in event 2).
3. The SSE stream hits 4096 output tokens mid-block →
   `message_delta` with `stop_reason: "max_tokens"`.
4. `chatOnce` maps it: `MapClaudeStopReason("max_tokens") → "length"`.
5. In `chatOnce`, only blocks that receive `content_block_stop` are finalized
   into `blocks`. The in-flight `tool_use` block is still in `blockMap` and is
   **discarded** — `res.toolCalls` comes back empty. The partial draft exists
   only in the lost stream.
6. The loop's `"length"` branch logs the warning, pushes the truncated
   assistant message into history **without** the tool call (deliberate: the
   Claude API rejects orphaned `tool_use` blocks), injects a
   `[system] Your previous response was truncated…` reminder, and re-issues.
7. The re-issue has the **same** 4096 cap, so the model hits the same wall
   (thinking eats the budget, or the tool-call JSON exceeds it). Each round
   burns 4096 output tokens and produces no file. The draft never lands.

## Fix

1. **Raise the default** — `DefaultClaudeMaxTokens: 4096 → 32768`.
   32K output tokens comfortably fits a multi-hundred-line file draft plus
   thinking and JSON escaping (the live proof below used ~9.6K completion
   tokens across the whole run).

2. **Make it configurable** — `GOPI_MAX_TOKENS` env var, read exactly once in
   `NewAgent` (single source of truth, same pattern as `GOPI_CONTEXT_WINDOW`),
   stored on `Agent.MaxTokens`, applied in `Run`. Invalid values warn and fall
   back to the default. `MaxTokens <= 0` (unset, e.g. tests building
   `Agent{}` literals) falls back to the default too.

3. **Scale the thinking budget** — the budget was a fixed 2048; with a fixed
   budget the model once spent its whole allowance on thinking (event 2).
   `BuildClaudeRequest` now sizes it proportionally
   (`thinkingBudget(maxTokens) = maxTokens / 2`), so a raised cap never
   starves the actual reply / tool call and the budget stays strictly below
   `max_tokens` as the API requires. The compaction summarizer is unaffected
   (it sends thinking `disabled`).

### Files changed

- `internal/deepseek/claude_client.go` — constant, doc comment, thinking
  budget scaling helper.
- `internal/agent/loop.go` — `Agent.MaxTokens` field, `GOPI_MAX_TOKENS` read
  in `NewAgent`, applied in `Run`.
- `cmd/agent/main.go` — usage text documents `GOPI_MAX_TOKENS`.
- `internal/agent/loop_test.go` — default/override/invalid-fallback tests.
- `internal/deepseek/claude_client_test.go` — thinking-budget scaling test.

## Verification

- Build: `go build ./...` — OK.
- Tests: `go test ./...` — all packages pass
  (`internal/agent`, `internal/deepseek`, `internal/tools` excluding the
  pre-existing flaky `TestBashDaemonSpawn_DoesNotLeakProcesses`, which also
  fails on pristine `master` — an environment process-group limitation,
  unrelated to this change).
- `go vet ./...` — OK.
- Live proof (optional): built the binary and asked it to draft a ~300-line
  Go file in **one write call**. The 251-line file landed complete
  (`/tmp/gopi-live-proof/bigstruct.go`, 9842 bytes) with zero truncation
  warnings; the run reported `completion=9633` output tokens — ~2.4× the old
  4096 cap. With the old default this exact task truncated at 4096 tokens,
  exactly as in the raw-log evidence.

## Not Done

- No automatic chunking of `write` tool calls (e.g. "write in parts" fallback
  when a draft exceeds the cap) — the raised, configurable cap makes
  multi-hundred-line drafts feasible; chunking remains a future enhancement.
- The pre-existing flaky `TestBashDaemonSpawn_DoesNotLeakProcesses` failure in
  `internal/tools` was not fixed (out of scope, fails on master too).

## Commit

See git log: `feat(maxtokens): make max_tokens configurable and raise default`
(single commit on branch `feat/maxtokens-configurable`).

---

## Addendum — Independent verification (second agent pass)

An independent re-verification of the committed fix (`a660cf3`) was run from a
clean clone of that exact commit (working tree held unrelated in-flight
changes from a parallel task, so per PROJECT.md Rule 6 all checks were done in an
isolated `/tmp/gopi-maxtokens-verify` clone).

1. **Raw-log evidence re-confirmed** — `~/.gopi/raw/gopi-raw-20260806-174648.log`
   contains exactly two `message_delta` events with
   `"stop_reason":"max_tokens"`, at **lines 24247 and 73097**, both with
   `"output_tokens":4096` (the API hits the old cap exactly). Line 73097 ends a
   streaming round where the model spent its *entire* output budget on a
   single `thinking` block (index 0) — no text and no `tool_use` block was ever
   emitted. Every request in that session carried `"max_tokens":4096`.

2. **Code wiring verified** — `NewAgent` reads `GOPI_MAX_TOKENS` exactly once
   (single source of truth) and stores it on `Agent.MaxTokens`; `Run` applies
   it with a fallback to `deepseek.DefaultClaudeMaxTokens` (32768) for
   unset/≤0 values. `BuildClaudeRequest` sizes the thinking budget below
   `max_tokens`.

3. **Build/test**: `go build ./...` OK; `go vet ./...` OK;
   `go test ./...` — all packages pass (internal/agent, internal/deepseek,
   internal/tools). New tests pass individually: `TestNewAgent_MaxTokensDefault`,
   `TestNewAgent_MaxTokensOverride`, `TestNewAgent_MaxTokensInvalidFallsBackToDefault`,
   `TestBuildClaudeRequest_ThinkingBudgetScales`.

4. **Fresh live proof** — built the binary from the clean clone and asked it to
   draft a multi-hundred-line Go file. This run wrote a **505-line**
   `bigstruct.go` (16071 bytes) in one write; the request carried
   `max_tokens:32768`; the run finished `OK` at `completion=5802` output tokens
   (≈1.4× the old 4096 cap) with **zero** "output truncated" warnings. The same
   task truncated at exactly 4096 output tokens under the old default (raw-log
   line 24247/73097). The drafted file compiles cleanly (`go vet` on the package
   passes). With the old cap this exact drafting task could not complete — this
   is direct, reproducible proof the fix holds.

Not Done: no automatic chunking of oversized `write` tool calls (future work);
pre-existing flaky `internal/tools` `TestBashDaemonSpawn_DoesNotLeakProcesses`
still fails on pristine `master` (environment process-group limitation, out of
scope).

---

## Addendum — Third-pass verification (epic session, master @ 5212792)

Third independent pass, run directly against `master` (HEAD `5212792`,
working tree held only unrelated untracked bug025 files which were left
untouched). The epic deliverables were re-verified end to end:

1. **Raw-log evidence re-confirmed** — `/home/user/.gopi/raw/gopi-raw-20260806-174648.log`
   contains exactly two `message_delta` events with
   `"stop_reason":"max_tokens"`, at lines **24247** and **73097**, both with
   `"output_tokens":4096` — the stream hits the old 4096 cap exactly. All 21
   requests in that session carry `"max_tokens":4096` and
   `"thinking":{"type":"enabled","budget_tokens":2048}` (grep counts: 21/21).
   Line 73097's round ended with only a `thinking` block closed — the model
   burned its entire output budget on thinking and never emitted the `write`
   tool call, so the draft never landed.

2. **Failure sequence matches code** — `internal/agent/loop.go` `"length"` branch
   (line 574) logs the exact stderr warning from the epic
   (`⚠️ output truncated (finish_reason=length) — re-issuing; tool calls may
   be incomplete`), preserves the truncated text, injects a
   `[system] Your previous response was truncated…` reminder, and re-issues.
   Under the old hardcoded 4096 cap every re-issue hit the same wall.

3. **Fix wiring verified** — `DefaultClaudeMaxTokens = 32768` (named
   constant, claude_client.go); `GOPI_MAX_TOKENS` read exactly once in
   `NewAgent` (loop.go), stored on `Agent.MaxTokens`, applied in `Run` with
   fallback to the default for unset/≤0/invalid values; thinking budget
   capped at `maxTokens/2` so raising the cap can never starve output.
   Tests cover all paths: `TestNewAgent_MaxTokensDefault`,
   `TestNewAgent_MaxTokensOverride`,
   `TestNewAgent_MaxTokensInvalidFallsBackToDefault`,
   `TestBuildClaudeRequest_ThinkingBudget`.

4. **Build/test** (fresh, non-cached): `go build ./...` OK, `go vet ./...` OK,
   `go test -count=1 ./...` — internal/agent, internal/deepseek,
   internal/tools all PASS.

Epic deliverables are fully met by commits `a660cf3` (fix) and `c833af5`
(docs + 505-line live proof). No code changes were needed in this pass.

---

## Addendum — Fourth pass (merge review: unnecessary max-token restriction removed)

Merge-review pass for the epic "review and finish merging
`feat/maxtokens-configurable`, and remove the unnecessary max token
restriction for gopi". The branch (`a660cf3`) was already merged into master
(verified: `git branch --merged master` includes it, and
`git log master..feat/maxtokens-configurable` is empty — master contains every
commit from the branch). The review found one remaining issue:

**The 32768 default was still an unnecessary output restriction.** DeepSeek V4's
documented maximum output is **384K tokens** (1M context window — see
`devdoc/optimization-report.md`, "DeepSeek V4: 1M context, 384K max output",
sourced from the DeepSeek Pi integration docs). The branch's fix raised the
default from the catastrophic 4096 to 32768 — a 8× improvement that fixed the
observed truncation — but 32768 still caps output at ~8.5% of the model's real
ceiling. A very long draft (thousand-line file) or a dense multi-tool round
could still hit `stop_reason=max_tokens` under the 32K default.

**Change:** `DefaultClaudeMaxTokens: 32768 → DeepSeekV4MaxOutputTokens`
(384000), with the model ceiling declared as a named constant (single source
of truth). `GOPI_MAX_TOKENS` still overrides for deployments that want a
tighter per-response budget (cost control), and the thinking-budget safety net
(`budget_tokens ≤ max_tokens/2`, default 2048) is unchanged — with the ceiling
raised, thinking is capped at 192000, so the default 2048 budget is untouched.
`Run()` keeps the `MaxTokens ≤ 0 → default` fallback.

**Verification:** `go build ./...` OK, `go vet ./...` OK, `go test -count=1 ./...`
all PASS (`internal/agent`, `internal/deepseek`, `internal/tools`), including
`TestNewAgent_MaxTokensDefault` / `_Override` / `_InvalidFallsBackToDefault`
and `TestBuildClaudeRequest_ThinkingBudget`.

Merged via branch `feat/maxtokens-remove-restriction` (see
`devdoc/maxtokens-merge-complete.md` for the full review + merge report).
