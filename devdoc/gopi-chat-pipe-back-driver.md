<!-- doc-class: historical -->
# devdoc: the `gopi chat --once` pipe-back driver (`--turns`)

Task `01a0db610c7c7e8c8e44761fd7137108`. Sibling prior art:
`01a0db4812b37926964f7686dfa186b8` landed the one-shot face
(`cmd/agent/chat_once.go`, commits `4b9133c0` + census `5231428a`), which is in
the peer's master at `767f122a` — this branch starts there.

## What the driver is

`--turns N` makes the engine's own answer the next turn's user message, in ONE
process, on ONE session, until a stop condition fires. The four stops:

| stop | condition | reported on stderr |
|---|---|---|
| `turns` | `--turns N` reached | `driver stopped after N turn(s): --turns N reached` |
| `max-seconds` | `--max-seconds S` elapsed, checked **between** turns | `… --max-seconds S reached` |
| `no-progress` | the answer is identical to the one before it | `… no progress — the answer repeated the one before it` |
| `sentinel` | `--stop-on TEXT` appears in an answer as a whole word | `… --stop-on "TEXT" appeared in the answer` |

A failed turn is the fifth end and the only FAILURE (`1`). Reaching a cap is a
normal end (`0`) — it is announced, never treated as an error.

Exit contract with the driver: `0` = the loop reached its stop; `1` = a turn
failed; `2` = usage (`--max-seconds`/`--stop-on` without the arming `--turns`,
a bad cap, an empty sentinel).

## The three design decisions that mattered

**1. The driver is a line SOURCE, not a turn loop.** `chatInput` already accepts
a carrier-owned reader (`newChatInputCooked(io.Reader, …)` — the interactive
terminal editor is the other carrier), so the driver owns *what the next user
message is* and the REPL keeps owning *how a turn runs*. Nothing about the turn
is reimplemented: same agent, same session, same history semantics, same tools,
same closeout. A second turn-runner would have drifted the first time either
side changed — the exact drift `--once` was built to avoid.

**2. The schedule is the REPL's own read point — but it is LOOK-AHEAD.** The
REPL reads lines through a feeder goroutine with a buffered channel, so the
driver's reader is asked for the next prompt BEFORE the turn it must be built
from has finished. The first implementation read the answer stream directly and
therefore handed the engine a PREFIX of its own answer; measured on the real
binary: `driver stopped after 0 turn(s)`, because the read happened before any
byte had been written. The fix is a completion handshake: the REPL reports each
finished turn through `chatOnceSink.TurnFinished(result, runErr)` (called inside
the turn, after `ag.Chat` returned), and the reader waits for the count to
advance (`chatOnceDriver.awaitTurn`). The sink is also the only place the driver
learns a turn FAILED — the session records completed turns only, and a failed
turn never reaches it.

**3. Answers are streamed by the REPL, so the driver only writes the DELIMITER.**
stdout with the driver is the answers (streamed exactly as a piped `gopi chat`
streams them) with a line holding `---8<--- turn ---8<---` after each one. An
answer re-emitted by the driver would print twice. The delimiter is written by
the reader, BEFORE it returns the next prompt, so the order on stdout is
turn N's text, its delimiter, turn N+1's text — the loop's own order, no
buffering to reconcile.

`--stop-on` is a whole-word match (`chatOnceSentinelFound`): `DONE` fires on
`… DONE.` and never on `ABANDONED`. The word boundary is spelled with an explicit
delimiter set rather than `\b`, because the sentinel is a plain substring search
and a caller's text is not a regexp.

## Honest gaps

- The driver's stop conditions are evaluated **between turns**, so `--max-seconds`
  can overshoot by one turn's wall-clock. Documented in `-h` and asserted that way
  in the E2E (a slow mock makes the stop deterministic).
- `no-progress` is checked **before** the caps, deliberately: when an engine
  restates itself AND a cap is reached on the same turn, "no progress" is the
  more useful sentence. Measured live: a repeated answer masked a
  `--max-seconds 2` cap (exit 0, `no progress` reported) — the order is the
  reason, and it is the order the help documents.
- The model's live reasoning stays on **stderr** (the historic destination). A
  driver that needs it runs the same command with `2>&1`; the answers stay
  splittable because each one still ends at its own separator line.
- The dry-run idea (a throwaway turn to make the REPL write `session_start`
  before the driver's first turn) was **dropped**: measured, that a REPL session
  duplicates its `session_start` when an agent is created outside
  `runChatWithInput` could not be substantiated, and the change would have added
  a second live binary for no landed benefit.

## Verification

- Build: `go build ./...` — OK (silent).
- Tests:
  - `go test ./cmd/agent/ -run 'TestChatOnce|TestG5' -count=1` → `ok` (3.0s)
  - `GOPI_BIN=/tmp/gopi-driver go test ./testsuite/ -run 'TestChatOnce' -count=1 -v`
    → **18/18 PASS** (7 one-shot + 11 driver)
  - `go test ./cmd/agent/ -run 'Chat|Term|Lineedit' -count=1` → `ok` (5.4s)
  - `go test ./testsuite/ -run 'TestChat_' -count=1` → `ok` (interactive REPL
    byte-shape untouched)
  - `go vet ./cmd/agent/ ./testsuite/` — clean
- Ran (Rule 31, real binary `bin/gopi`, sandbox profile
  `/home/user/workspace/gopi-sandbox/sandbox-chatdriver-80c9b2fb`, local SSE mock
  provider — the operator's ledger and provider untouched):

  ```
  gopi chat --once --turns 3 "start the loop and keep going"
  EXIT=0
  stdout:
    mock turn 2 answered with a distinctive sentence of its own.
    ---8<--- turn ---8<---
    mock turn 3 answered with a distinctive sentence of its own.
    ---8<--- turn ---8<---
    mock turn 4 answered with a distinctive sentence of its own.
    ---8<--- turn ---8<---
  stderr:
    gopi chat: pipe-back driver armed — each turn's answer becomes the next
    prompt; stops at: --turns 3, no progress (a repeated answer)
    gopi chat: driver stopped after 3 turn(s): --turns 3 reached

  the mock's own record of what each request was SENT (last user message):
    request 2 LAST USER = "start the loop and keep going"
    request 3 LAST USER = "mock turn 2 answered with a distinctive sentence of its own."
    request 4 LAST USER = "mock turn 3 answered with a distinctive sentence of its own."
  ```

  The session JSONL of that run (`…/ba67ee38-…/session.jsonl`):
  `{'session_start': 1, 'user': 3, 'assistant': 3, 'request': 3, 'session_end': 1}`,
  `session_end.exit_code: 0`, with `turn 2 USER = "mock turn 2 answered …"` — the
  feedback is durable, not just on the wire.

  Other live runs: `--turns 5` against a repeating mock → exit 0, 2 separators,
  `no progress — the answer repeated the one before it`; `--turns 9 --stop-on
  "mock turn 3"` → exit 0, 2 separators, sentinel reported; `--turns 20
  --max-seconds 2` against a slow mock → exit 0, wall-clock stop; provider down →
  exit 1, `turn failed` + `driver stopped after 0 turn(s)`; `--max-seconds` without
  `--turns` → exit 2.

- Red-proof (the E2E can fail): replace the fed-back prompt with a literal
  (`d.prompt = "RED-PROOF: the answer is not fed back"`), rebuild, run
  `TestChatOnceDriver_FeedsEachAnswerBack` →
  `turn 2 SENT "RED-PROOF: the answer is not fed back", want it to carry "driver
  turn 1 answered …"` — FAIL; restored → PASS. The first version of that
  assertion passed under the mutation (the body contains earlier assistant
  messages as HISTORY), which is why it now asserts the **last user message** of
  each request (`lastUserMessage`).
- Census re-derived in the same commit: `gopi chat`'s advertised flags 6 → 9
  (`max-seconds`, `stop-on`, `turns`), group total 31 → 34, pinned in
  `g5ExpectedFlags` and transcribed in
  `devdoc/gopi-chat-web-help-surface-inventory.md`.

## Files

- `cmd/agent/chat_once.go` — `--turns`/`--max-seconds`/`--stop-on` parsing, the
  driver, the prompt reader, the delimiter, the stop policy, the help.
- `cmd/agent/cli_chat.go` — the sink call in the REPL's turn, one synopsis line.
- `cmd/agent/chat_once_test.go` — driver argv/stop-policy/sentinel/help unit tests.
- `testsuite/chat_once_driver_test.go` — 11 E2E cases on the real binary.
- `cmd/agent/cli_chat_web_help_surface_basic_test.go` + the inventory doc — the
  re-derived census.
