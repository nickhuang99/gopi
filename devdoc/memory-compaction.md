<!-- doc-class: historical -->
# Memory Compaction — LLM-assisted context management

Branch: `merge/clean-compaction` (feature complete — see
`devdoc/merge-readiness-feat-memory-compaction.md` for merge status).

## Problem

The ReAct loop grows its history by ~2-3K tokens per iteration. Past the
context window, `trimContext` **dropped** the oldest messages — lossy and
incoherent: the agent silently forgot its goal, decisions, and findings
(and could re-do work or contradict itself).

## Design

When the estimated context exceeds 90% of the window (`TrimThresholdRatio`),
the agent now calls the LLM to **summarize the oldest messages into a
compact memory block** instead of dropping them:

1. `Context.TakeOldest(target)` pops the oldest messages until the estimate
   is ≤ `CompactionTargetRatio` (50%) of the window — same drop semantics
   and orphan-tool safety as `TrimToFit`, but returns the messages.
2. One cheap summarizer call (`ClaudeChat`, no tools, thinking disabled,
   ≤ 2048 output tokens) distills them: goal, decisions, findings, file
   paths, commands/test outcomes, task list, next steps.
3. The memory block is prepended as a `[MEMORY COMPACTION — EARLIER WORK]`
   user message; the recent history stays verbatim (cache-friendly).
4. If the summarizer fails, the oldest messages stay removed — equivalent
   to the previous trim behavior (graceful degradation).

The summarizer runs only when the 90% threshold trips — at most a few times
per very long session.

### Measured-token feedback loop

The chars/4 heuristic only guesses the context size, so compaction could
trigger too early (or too late) on real traffic. Every LLM response reports
the authoritative prompt-side token count
(`input_tokens` + `cache_creation_input_tokens` + `cache_read_input_tokens`);
the loop feeds it back into the estimate via `Context.SetPromptTokens`:

- `Context.PromptTokens` = measured count, `PromptTokensIndex` = how many
  conversation messages that count covers.
- `EstimatedTokens()` uses the measured count for the covered messages and
  only adds the heuristic for messages appended since — so the compaction
  trigger fires on the *measured* size, not guesswork.
- Trimming invalidates the measurement (the index is stale), so the pure
  heuristic takes over until the next response re-measures.
- `trimContext`'s log line now prints both the estimated and the measured
  count, making it obvious which one drove the trigger.

## Config

- `GOPI_COMPACT=off` disables compaction → falls back to plain trimming.
- Default: on. Reads the env var in exactly one place (NewAgent, Rule 2).
- `GOPI_CONTEXT_WINDOW` still controls the window; compaction target is
  `CompactionTargetRatio` (0.5) of it.

## Files

- `internal/agent/compact.go` — `compactContext`, `summarizeMessages`,
  `compactionSystemPrompt`, `memorySummaryMarker`, constants
- `internal/agent/context.go` — `Context.TakeOldest`,
  `Context.SetPromptTokens` (measured-token feedback), calibrated
  `EstimatedTokens`
- `internal/agent/loop.go` — `trimContext(ctx)` compaction-first, new
  `CompactionEnabled` field, feeds API-measured prompt tokens back after
  every streaming and non-streaming response
- `internal/agent/compact_test.go` — 7 tests (compaction, fallback,
  no-trigger, disabled, TakeOldest semantics, token-calibration ×2)
- `cmd/agent/main.go` — usage doc

## Verification

- `go build ./...`, `go vet ./...`, `go test ./...` all pass.
- `go test -race ./...` — all packages PASS (no data races).
- Live run with `GOPI_CONTEXT_WINDOW=6000` (threshold trips in a few
  rounds): raw log shows 1 summarizer request (7 input messages), its
  summary response, and the `[MEMORY COMPACTION — EARLIER WORK]` block
  present in all subsequent requests. Run completed OK.

## Evaluation (hermes-agent deep analysis, merge/clean-compaction)

Long task runs (~700-900s, GOPI_CONTEXT_WINDOW=20000-25000) confirmed:

- **Trigger measures, not guesses**: the compaction log shows both
  `est` and `measured` (API-reported) counts, e.g. "est 22921 tokens
  (measured 18149) > 18000".
- **Coherence survives compaction**: the agent kept referencing earlier
  findings across 16-19 compactions in a single run (it even discovered
  and preserved a meta-finding about gopi's own logs mid-run).
- **Summarizer quality was the weak link**: the first prompt let the
  model echo raw tool-call syntax into the memory block. Strengthened
  compactionSystemPrompt with hard no-echo rules; blocks went from
  ~300-700 chars (echoing) to ~2000 chars of dense notes.
- **Tight-window costs** (inherent, not bugs): each compaction breaks
  the DeepSeek cache prefix (cache hit dropped to ~52-57% vs 94% in an
  un-compacted run) and adds one extra LLM call. With the real 900K
  window compaction fires once or twice per very long session, so these
  costs vanish.
- Both evaluation runs ended on the GOPI_TIMEOUT clock (the hermes-agent
  analysis needs more than 700-900s), with the agent still working
  coherently — the limiter was time, not memory.

## Not Done / Future

- Calibrate the heuristic tail: after a measured response, the chars/4
  estimate of the newest round's thinking + tool outputs overshoots, so
  compaction can fire early under tight windows.
- Streaming the summarizer output (not needed — call is short).
- Rolling the summary into the system prompt instead of a user message
  (would break the shared cache prefix — current placement is deliberate).
