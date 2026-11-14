<!-- doc-class: historical -->
# Raw-log verification: memory_feedback, thinking trim, context_trim

Date: 2026-08-26. Branch: feat/log-rawlog. Companion feature: `gopi log
<id> --raw` (verbatim raw HTTP log view). All checks below are what the
Epic's `--raw` view now makes trivially repeatable by any user.

## 1. memory_feedback usage — the prompt is the problem, not the loop

Scan: all 15 most recent session JSONLs (`~/.gopi/sessions/*.jsonl`) for
`"name":"memory_feedback"` tool_use entries.

| measure | value |
|---|---|
| sessions scanned | 15 |
| sessions with ≥1 memory_feedback call | 1 (56bf5b51…) |
| sessions with ≥1 memory_store call | 7 |
| the single call | `memory_feedback(id:888, rating:"useful", tool_use_id:…)` — a *useful* vote with reason |

Conclusion: the feedback loop works end-to-end (the vote landed, the record
id and originating tool_use_id were correct), but the system prompt only
ever motivates the *useless* direction ("hit contradicts your results →
memory_feedback(useless)"). The useful direction — the one that actually
reinforces good memories — is never asked for, and the "you are the only
quality judge" framing (tool description) reads as advisory rather than a
per-search obligation.

Fix committed (feat(prompt)): tool routing rule `d` now reads "judge every
search hit: used it → memory_feedback(useful); contradicts your results →
memory_feedback(useless) then memory_update. 不投票=知识库失去质量信号."
Effectiveness will show up in the same `--memory` / `--filter
memory_feedback` scan after the prompt ships.

## 2. Thinking trim — wire evidence exists and is checkable

The Epic worried "recent thinking has been trimmed" cannot be verified.
It can, two ways, both now one command:

1. **Thinking-disabled rounds**: when the harness latches
   `thinkingForceDisabled` (2 consecutive over-budget aborts, loop.go), the
   request wire carries `"thinking":{"type":"disabled"}` and no thinking
   blocks. `gopi log <id> --raw | grep '"thinking":{"type":"disabled"'`
   proves the downgrade happened.
2. **Empty-placeholder replay** (bug024 class): tool rounds generated
   thinking-disabled or with streamed-empty thinking replay a
   `{"type":"thinking","thinking":" ","signature":"gopi-empty-thinking"}`
   placeholder on the wire — required by the Anthropic-compatible endpoint
   (400 without it). `gopi log <id> --raw | grep -c gopi-empty-thinking`
   counts exactly how many tool rounds lost their real thinking.

Live evidence (this session's raw log, 53 requests):
- 54× `"thinking":{"type":"enabled"}` (first request of each round incl.
  the current one) — all thinking-enabled, so no session-level downgrade.
- 1920× `gopi-empty-thinking` — the session's tool rounds replayed empty
  thinking placeholders. The last request carried 113 messages / 56
  thinking blocks, ALL empty placeholders (the session streamed 0 real
  reasoning — consistent with the `[thinking]`-less wire this run shows).
- Deep inspection of the last → REQUEST body (via `--request --json`)
  shows the trimmed/historical context: the request's message array is the
  ground truth of what the model actually saw — filters cannot mislead here.

Caveat: auto context-window trimming (TrimToFit, context.go) is visible as
fewer old messages in later → REQUEST bodies (compare message counts across
requests), not as a marker. `--raw` exposes exactly this comparison.

## 3. context_trim activity — session view + raw cross-check

`gopi log <id> --context-trim` renders every context_trim call with
call_id, replace_with, and before/after byte comparison, parsed from the
tool_use/tool_result pairs. The current session has no context_trim calls
("No context_trim activity"), which matches the raw log — cross-check:
`gopi log <id> --raw | grep '"name":"context_trim"'` (raw request bodies
contain every tool call; a session view showing none must also show none
in the raw log). Both agree → the filter view is not dropping data.

## 4. Bonus bug found while testing

`FindRawLogForSession` returned a false "ambiguous raw log match" when the
best candidate was an exact-second match (delta 0) and no second candidate
existed — the runner-up sentinel initialized to 0 tied with bestDelta 0.
This would have made `--raw`/`--request` fail for exactly the sessions it
pairs best (second-precision filenames vs session_start). Fixed with a -1
sentinel + explicit first-iteration branch (commit d3d0656).

## How to re-run every check with the new feature

    gopi log <id> --raw | grep '"thinking":{"type":"disabled"'   # thinking downgrade
    gopi log <id> --raw | grep -c gopi-empty-thinking            # empty-replay count
    gopi log <id> --raw | grep '"name":"memory_feedback"'        # feedback calls in raw
    gopi log --last 15 --memory | grep -c memory_feedback        # per-session usage
    gopi log <id> --context-trim                                  # trim activity view
