<!-- doc-class: normative -->
# Task Briefing Retrieval — where the original task request lives (and why gopi log hides it)

Source of truth: session `c6985118` (a real `task run` / child-gopi session), verified against
`gopi raw c6985118 1 request`, the session JSONL, and the formatter code.

## TL;DR

The briefing is **NOT in the system prompt** and **NOT lost**. It is the FIRST **user message**
of the first API request, stored verbatim in the raw HTTP log. `gopi log --request --text` shows
only system blocks by design, and `gopi log --chat` skips it due to a one-message stale-buffer
race — both are view gaps, not data loss.

| Retrieval path | Command | Shows the briefing? |
|---|---|---|
| First user msg of round-1 request | `gopi raw <id> --userprompt` | **YES — verbatim, clean** |
| Full request body | `gopi raw <id> 1 request` | **YES — `messages[0].content[0]`** |
| Full first request JSON | `gopi log <id> --request --json` | YES (embedded in messages) |
| System prompt, text | `gopi log <id> --request --text` | **NO — briefing is not a system prompt** |
| Chat view | `gopi log <id> --chat` | NO (gap — see below) |
| Todo view | `gopi log <id> --todo` | YES — `s.Task` = first user msg (one-line-truncated) |

## Where the briefing actually lives on the wire

Round-1 request body of `c6985118` (`gopi raw c6985118 1 request`):

```
system: 2 blocks           ← kernel prompt (block 0) + GOPI.md project rules (block 1)
messages: 1
  messages[0] role=user, 2 text blocks:
    block 0 (2859 B):  [EPIC GOAL]\n[Task summary]\n…\n[Briefing payload]\n…
                       …"re-emit on change."
    block 1  (770 B):  [system] TASK-START CHECKPOINT: this task has just begun…
```

Composition chain (verified in code):
1. `cmd/agent/cli_tasks.go` `taskRunBriefing()` builds `[Task summary]` + `[Briefing payload]`
   (constants `taskBriefingHeaderSummary/Payload`).
2. `internal/agent/loop.go` `runEngine()` (~L2423) wraps the briefing as the Epic first turn:
   `"[EPIC GOAL]\n%s\n\nBased on this Epic, %s; re-emit on change."`.
3. The task-start checkpoint is appended as a **separate user text block** (injected by the run
   framework) — it is NOT the briefing and is NOT inside the briefing.
4. The whole user turn goes out as `messages[0]`. The system array carries only the kernel prompt
   + GOPI.md (`internal/agent/context.go` BuildClaudeRequest, `claude_client.go` L464-467).

So the "briefing injected into the middle of the system prompt" impression comes from viewing
`gopi raw c6985118 1 request` unfiltered: the kernel prompt is huge, and the briefing+checkpoint
arrive immediately after in the *messages* part of the same JSON dump. It was never a system block.

## Why `gopi log --request --text` shows no briefing — by design

`--request` renders the request via `gopilog.FormatRequestSystem` (rawlog.go L481), which calls
`extractSystemPrompts` — **only** the `system[]` array (kernel + GOPI.md) and role=system message
blocks. It never prints `messages`. That matches the CLI help (`--request --text = system prompt
only`). `--request --json` prints the whole body, so it does contain the briefing under
`.messages[0].content[0].text`.

Confirmed empirically: `gopi log c6985118 --request --text | grep -c 'EPIC GOAL'` → 0;
`--request --json` → 1.

## Why `gopi log --chat` drops the briefing — reader bug (user-row not attributed)

Chat view = `FormatChat` over `gopilog.ReadSession`. The reader's `processEvents` keeps
`lastUserMsg` and only assigns it as `currentIter.UserPrompt` when `ensureIter` *creates* an
iteration. On this session, row order is:

1. `user [EPIC GOAL]…`        → `lastUserMsg` set, Task set
2. `user [system] TASK-START CHECKPOINT…` → **overwrites** `lastUserMsg` (reader.go L113)
3. `assistant` (first response) → `ensureIter` creates Iteration 0 → UserPrompt = the
   **checkpoint** (row 2), never the briefing (row 1)

Hence chat Iteration 0 shows `▸ USER [system] TASK-START CHECKPOINT…` and the briefing is absent.
Every later `ensureIter` fires with `lastUserMsg == ""` (it was cleared), so the briefing never
appears in any later iteration either. The first user row is only preserved into `s.Task` (used by
`--todo`, one-line-truncated via `summaryTaskMaxLen`).

This is a genuine extraction gap: `--chat` is the human view a user would expect to carry the
original request, and it silently loses the Epic briefing when the checkpoint (or any subsequent
user row) follows before the first assistant turn. The raw data is intact — recovery is via
`gopi raw <id> --userprompt`.

## Recommendation

- Point users at `gopi raw <id> --userprompt` — it exists (`cmd/agent/cli_raw.go` printRawUserprompt
  → `gopilog.FormatRequestUserPrompt` → `FirstUserMessage`, pipe-friendly, no header) but is
  undiscoverable; `gopi log` help never mentions it.
- Fix the `--chat` gap in `reader.go`: don't let a later `user` row overwrite an un-consumed
  `lastUserMsg` that predates the first assistant turn (or track "first user row seen" explicitly);
  add a regression test asserting Iteration 0's UserPrompt == the Epic briefing row when a
  checkpoint user row follows before the first response.
- Optionally document in `gopi log --help` that `--request --text` shows only system prompts and
  the briefing is under `--request --json .messages[0]` / `gopi raw <id> --userprompt`.

## FIXED (2026-09-07)

The `--chat` gap was a real bug in `internal/gopilog/reader.go` `processEvents`: a single
`lastUserMsg` buffer applied last-wins, so the framework's `[system] TASK-START CHECKPOINT` user
row (written immediately after the briefing, before the first assistant turn) overwrote the
briefing and became Iteration 0's `UserPrompt` — the original task request was unrecoverable from
`--chat` (only `s.Task`, one-line-truncated, survived for `--todo`).

Fix: while no iteration has started yet (`currentIter == nil && len(s.Iterations) == 0`), keep the
FIRST unconsumed user row (the briefing); once an iteration exists, last-wins remains the correct
per-round prompt semantics. Regression test `TestParseCheckpointKeepsEpicBriefing` (fixture:
briefing → checkpoint → assistant). Golden `log-chat.txt` re-recorded — the committed golden had
been locking in the buggy behavior (its diff is exactly the intended change). Verified on the real
session c6985118: `--chat` Iteration 0 now opens with the full `[EPIC GOAL]…` briefing.
