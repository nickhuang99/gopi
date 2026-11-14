<!-- doc-class: normative -->
# Reframe Flow (epic-task-reframe Phase 2) — Implementation Devdoc

> Branch: feat/reframe-flow · Master phase-1: 13ce217 (session-dir + attachments + [attachmentN] index).

## What was built

The reframe flow turns a giant raw `[EPIC GOAL]` briefing into a compact
**in-memory** goal that the LLM itself produces in its task-list response:

1. **Directive** (`reframeDirective`, internal/agent/reframe.go) is interpolated
   into the `[EPIC GOAL]` framing (runEngine's round-1 user message,
   loop.go). It tells the model: *if this Epic is large/verbose (long briefing
   or attachments), the task-list response must ALSO open with a
   `[REF RAMED EPIC]` block* — a formal concise precise restatement
   (background / constraints / inputs / deliverable / acceptance) that
   **replaces the raw briefing as the referenced goal** for every later review.
   One-line tasks skip reframing (raw text already is the reframed form).
2. **Extraction** (`extractReframedEpic`): pulls the block body (runs to the
   next `[TASK_LIST_BEGIN]` or end of content). Body must be ≥40 chars or the
   emission is ignored.
3. **Swap** (`maybeApplyReframe`, called every loop round after the task-list
   parse): on a confirmed reframe, replaces the raw `[EPIC GOAL]` message
   (found by prefix scan — index 0 NOT assumed, compaction may prepend a
   memory block) with `[EPIC GOAL]\n<reframed>\n(archive note)`. One-shot
   latch (`reframeApplied`).
4. **Guards**:
   - briefing < `reframeMinRawLength` (4000 chars) → swap REFUSED even if the
     model emitted a block (small task: zero positive-value reduction, no
     detail-loss risk);
   - no block emitted → raw briefing stays (directive is a prompt, not a
     harness requirement).
5. **No file persistence**: the reframed epic IS the `[EPIC GOAL]` message in
   memory. The raw text remains in the session JSONL (evidence, per Phase-2
   boundary). No reframed_epic.md.

## Design decisions

- **Directive in the EPIC framing, not the system prompt**: the reframe
  instruction is task-framing context (applies only to task runs, and only on
  round 1). Putting it in the fixed system prompt would grow every request of
  every mode past the 4.5K density gate. Interpolated once into the round-1
  user message, it exits per-round injection automatically when the swap runs
  (the message is rebuilt without the framing tail + directive).
- **Swap replaces message[prefix] in Ctx.Messages**: because the loop builds
  each wire request from `Ctx.Messages` (`WireMessages` →
  `BuildClaudeRequest`), the compact goal reaches the wire on the very next
  round — and a resume checkpoint (which snapshots the last → REQUEST
  verbatim) inherently carries the reframed goal. Resume carry-forward needs
  NO special code: the checkpoint already holds the swapped text. Acceptance
  #5 falls out of the mechanism.
- **Review anchor unchanged**: `injectPostCommitReview` says "the epic at the
  top of this context" — after the swap that epic IS the reframed text, so the
  post-commit review automatically compares against the reframed goal
  (acceptance #3). `trimCommittedTaskArea` keeps index 0 — the swapped goal
  is equally protected from compaction.
- **Keep the `[EPIC GOAL]` prefix**: mechanisms match on the prefix
  (`wfContractEpicMsg` checks, wfcontract_test, trim keep-0 logic).
- **LLM-judged trigger** (spec §3 "always let the LLM decide"): the directive
  asks the model to judge; the harness-side length guard is only a
  detail-loss safety net, not a trigger.

## Context-reduction accounting (acceptance #2)

- Round 1 request: raw briefing (N chars) + directive.
- Round 2+ request (after a reframe): `[EPIC GOAL]\n<reframed body (~200-600
  chars)> + archive note`. The raw giant exits per-round injection.

## Files

- `internal/agent/reframe.go` — new: marker/directive/extraction/swap.
- `internal/agent/loop.go` — `reframeApplied` Agent field; directive
  interpolation in epic framing; `maybeApplyReframe` loop hook after the
  task-list parse.
- `internal/agent/reframe_test.go` — new: directive wiring, extraction,
  message shape, big-briefing swap over a real Run (round-1 reframe+tool →
  round-2 request carries compact goal), small-task no-swap, no-marker
  no-swap, review-anchor text, prefix-scan swap when epic is not at index 0.

## Verification

- `go build ./...`: OK
- `go test ./internal/agent/ -run TestReframe -count=1`: 8/8 PASS
- full suite: `go test ./internal/agent/ -count=1` — see report.
- `go test ./...` — see report.

## Not done (boundary)

- No reframed_epic.md file persistence (in-memory only; raw text stays in
  session JSONL).
- No attachment-copy changes (Phase 1 owns that).
- No session-management commands (Phase 4).
- No reframe of already-compact goals (deliberate: refusal guard).
