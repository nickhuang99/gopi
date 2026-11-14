<!-- doc-class: normative -->
# The note as a SUBSTITUTE, not an addition (`internal/agent/turnfold.go`)

**What was there before.** `selfrecord.go` recorded a note per turn and left the
turn in the conversation — the note was a memo BESIDE the transcript, so a
session's request grew as `Σ(turns × their tool traffic)` and the notes rode on
top of it. The compaction machinery existed because of that growth.

**What this file does.** At every round boundary each COMPLETED turn is REPLACED
by one note. The turn's own input row, every round's `tool_use` / `tool_result`
and its assistant text leave `Ctx.Messages`; what remains is one harness user row
that states what the turn was asked, what it concluded, and the anchors a later
round can search for. The raw history is not lost — it was already written to the
session JSONL, which is the layer an on-demand search reads.

⇒ The conversation becomes **O(turns) notes** instead of
**O(rounds × results) raw history**, and the growth stops being a function of how
many tools a turn ran.

## The five decisions worth knowing, each one measured

1. **A turn is removed as a UNIT.** The only invariant the Claude wire requires is
   `tool_use` ↔ `tool_result` PAIRING, not presence. Removing both sides of a pair
   leaves `verifyToolUsePairs` untouched, and the note arrives as a USER row so no
   two consecutive assistant messages are created. The rebuilt array is then
   PROVEN with that same verifier before it is installed (`FAIL CLOSED`): a
   violation leaves the conversation exactly as it was and logs why.
   *The arm:* `TestTurnFoldRefusalLeavesTheConversationIntact` builds the one
   history where a boundary lands between a call and its result; the fold must
   refuse.

2. **The boundary is `sessionevent.IsTurnOpeningRow`, and NOTHING is added beside
   it.** The first version added `!isHarnessInjectedUserMessage` as a local guard
   and measured **0 folded turns on a 4-turn fixture** — because that guard also
   refuses the `gopi message boundary` row, which the shared predicate counts as a
   turn opening. The predicate already answers both halves (a `[system] `
   checkpoint opens no turn; a briefing, a boundary row and a new input do), so a
   second opinion beside it can only drift from `gopi session log --turns`.

3. **The fold does not re-fold a session that already holds notes.** An
   already-folded turn is its own range (`isTurnFoldNoteMessage` cuts there).
   Without that cut, every note was absorbed into the range that began before it,
   so the whole folded head became ONE range starting at index 0 — the range the
   fold exempts — and a resumed session with several completed turns could never
   fold any of them (measured: 0). *The arm:*
   `TestTurnFoldResumesWithSeveralCompletedTurns`.

4. **The anchors are the tool ARGUMENTS, not the tool's prose.** A parsed
   `deepseek.ToolCall` keeps its arguments in `Arguments map[string]interface{}`
   and leaves `RawArgs` empty. A scan reading only `Content` + `RawArgs` produced a
   note that named `read` and the error text but NOT the path the read was about
   (measured). `turnTextParts` therefore iterates the argument map.

5. **The note's four slots are the briefing's spec, filled in priority order:**
   `asked:` (the turn's input, one line) → `what this turn did:` (round/tool
   counts) → `conclusion:` (the last substantial assistant text) →
   `anchors:` → per-call recaps, which are the FIRST thing cut when the
   `maxTurnFoldNoteChars` (1000) budget binds, because the transcript has every
   result.

## The one consumer the fold would have starved

`lastSubstantiveText` (the wallclock cap's graceful finish) asks for "the most
recent non-trivial assistant text". After a fold a completed turn's last word is
no longer an assistant message — it is the `conclusion:` line of its note. The
fix is ONE backward walk that accepts either shape at the position it appears;
scanning for assistant text first and falling back to notes afterwards answered
with the OLDEST unfolded turn's word (measured: a 4-turn fixture answered
`understood` — turn 1's — while turn 3's conclusion was the real last word).
*The arm:* `TestTurnFoldConclusionIsReadableAfterTheFold`.

## A text-only turn is NOT folded — and that rule was forced by three shipped arms

The fold's job is to stop `Σ(turns × their tool traffic)`: a tool result is a whole
file's contents or a command's output, while a TEXT turn is one message **whose
value is its own wording**. A note caps the conclusion at 240 runes and the ask at
180, so folding a text-only turn keeps a paraphrase of the answer and DISCARDS the
answer, to save one message.

Three arms that shipped before this file caught exactly that, and the fix was the
rule, not the tests:

| arm | what it asserts | why it was right |
|---|---|---|
| `TestChatOps_TextTurn_UnchangedShape` | a plain `ChatOps` turn leaves `Ctx.Messages` at seed + user + assistant | nothing was large; a reply cut to 240 runes is a loss |
| `TestChatOps_HistorySeededAndContinuity` | turn 2's seed still carries turn 1's `tool_use`/`tool_result` | **this one is legitimately CHANGED by the fold** — see below |
| `TestSessionInboxIsDeliveredOncePerTurn` | the second turn carries the inbox statement exactly once | the statement lived in a text turn's user row |

⇒ `turnCarriesToolTraffic` gates the fold: a range with no `tool_use` and no
`tool_result` is left alone. A turn that ran tools still folds, which is where
the whole saving is.

**`TestChatOps_HistorySeededAndContinuity` is the one contract the fold
deliberately breaks, and its NAME is the requirement that changes**: "turn 2 can
reference what turn 1's tools found" was implemented as "the raw `tool_result`
survives into the seed". After the fold that is no longer true by design — turn 2
references what turn 1 found through the NOTE's conclusion and anchors. That arm
must be restated to assert the note, not the raw block.

## The exemption that had to become face-aware

The premise exemption above (index 0) is a TASK-run rule. A chat has no premise:
its message 0 is one of the operator's inputs, and it sits at index 0 for the
life of the conversation — so exempting it made the FIRST turn of every chat
permanently unfoldable. Measured through the real binary: the last request still
carried turn 1's raw `tool_use` / `tool_result` pair with a complete turn behind
it. The exemption is now `r.start == 0 && !a.ChatMode`.
*The arm:* `TestTurnFoldChatModeFoldsItsFirstTurn` (the same history, opposite
verdicts on the two faces) and `TestTurnFold_SecondTurnCarriesNoRawToolTraffic`
(the wire measurement).

## Where it is called

Three boundaries, all of them a round boundary and all of them BEFORE the round's
injections (an injected notice belongs to the turn about to run):

- `internal/agent/loop.go` — the task run's tool loop;
- `internal/agent/chat_ops.go` — the chat surface's tool loop;
- `internal/agent/loop.go` (`runChat`) — the no-tools chat turn.

The fold is idempotent by construction (a note is not a raw turn), so a boundary
that finds nothing new costs one pass over the messages.

## Measured note sizes (the fixture in `turnfold_test.go`)

| turn | folded messages | note chars |
|---|---|---|
| 1 | 2 | 256 |
| 2 | 4 | 255 |
| 3 | 4 | 328 |
| 4 | — | in flight, not folded |

Comfortably inside the epic's 1000-character ceiling; the cap arms prove the
ceiling binds when the content is larger (`capTurnFoldNote` cuts on a RUNE
boundary so a CJK note is never split).

## The threshold regression, and why the fold is unconditional again

Commit `4c8c44df` replaced "fold every completed turn" with "fold only past the
compaction trigger, and even then keep the newest 8 turns raw"
(`turnFoldDue` + `turnFoldRecentTurns = 8`). **It was measured inert on the real
path**: session `a596a78e-9594-4b4f-8768-3135e42f4835` has 39 turns, and its
request was 285 messages / 93,059 estimated tokens against a
`0.75 × 200000 = 150000` trigger — so `gopi session context --last --user`
carried **143 raw user rows, 135 raw `tool_result` blocks and ZERO `turn folded`
notes**. The substitute form had become the addition form plus a note nobody saw.

The fold is therefore **unconditional again**, and the two beliefs behind the
threshold do not survive measurement:

- *"a fold needs a size trigger to be safe"* — a trigger is right for
  `trimContext`, which **summarises** (lossy, so it must be rationed). The fold
  **copies the turn's own recorded words**, so its only failure mode is losing
  information, and that is guarded by `foldableTurnRanges`' exclusions
  (premise / already-a-note / text-only / image / compaction / record carrier),
  not by a size.
- *"keep the last 8 turns raw"* — that IS the addition form for the 8 newest
  turns, and 8 is the number `DefaultCompactKeepRecent` uses for the SUMMARISER.
  Copied onto a substitute it means a conversation of ≤9 turns folds nothing.

`GOPI_TURN_FOLD_KEEP_TURNS` survives as an **operator escape hatch** whose
default is `0` (no window): `n > 0` keeps that many newest turns verbatim. Zero is
a valid setting (`n >= 0`); a negative or unparseable value warns and falls back.

**The A/B on the real binary** (mock Anthropic SSE, one `gopi chat --tools` run
per arm, same three inputs, `GOPI_CONTEXT_WINDOW=200000`):

| arm | request sizes (nmsgs) | folded notes |
|---|---|---|
| `GOPI_TURN_FOLD_KEEP_TURNS=0` (shipped) | 1, 3, 1, 3, 1, 3 | 0, 0, 1, 1, 1, 1 |
| `GOPI_TURN_FOLD_KEEP_TURNS=8` (`4c8c44df`'s default) | 1, 3, 5, 7 | 0, 0, 0, 0 |

With the window off the request does not grow with the turn count and every
completed turn is gone from it; with the window on it grows monotonically and
nothing is ever replaced. The evidence files are in this run's session directory
(`e2e-wire-requests.jsonl`, `e2e-ab.txt`).

**The arm that pins it**: `TestTurnFoldIsUnconditionalUnderTheShippedPolicy` and
`TestTurnFoldEngagesInASessionFarBelowTheTrigger`. Both were run against the
pre-change `turnfold.go` first and both went RED with the measured numbers in the
failure text (`Used:134 Threshold:150000`, `Folded:0`), which is the red proof.

## What is NOT done here

- **`session_search` (row `01a1baf6-634e`) is not in this row's scope and is still
  `in_progress`.** The fold is safe independently: the raw history was ALWAYS on
  disk — the session JSONL is written by `LogAssistantText` / `LogToolResult`
  before any context management touches the array — so nothing this file removes
  from the request is unrecoverable. The search tool changes how CONVENIENT that
  recovery is, not whether it is possible.
- **Compaction is untouched** (row `01a1bafb-8a8f`). The fold makes the context
  smaller sooner, which is why that row's "stop compressing, warn instead"
  becomes reachable — but it is a separate change.
