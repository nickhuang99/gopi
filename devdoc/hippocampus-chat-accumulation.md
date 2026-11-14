<!-- doc-class: historical -->
# `gopi chat` accumulates hippocampal knowledge — four decisions

Task `01a0daab-afd7-7ebd-ac8c-ee511bb6eddc`. Branch
`task/01a0daabafd77ebdac8cee511bb6eddc`.

The epic asked for one thing: make the **chat** path deposit knowledge the way a
**task run** does. Answering it needed four decisions, recorded here — including
the one that is a deliberate NO.

---

## 1. The chat turn had no round number (fixed)

`Agent.hippocampusRound` was written in exactly ONE place, `loop.go`'s `runEngine`
(`a.hippocampusRound = i + 1`, the top of the task loop's round body). The
tool-enabled chat loop `Agent.ChatOps` never touched it, so:

* the field held **0** for the whole chat session — a round number no round ever
  has;
* the shared boundary helper is where the value is *read*
  (`hippocampus.go`'s `AtRoundBoundary`: `round := a.hippocampusRound`, passed to
  `deliver` / `launchRecall` / `adjudicate` / `archive`), so **every chat archive
  landed under `round: 0`** and a chat session's audit trail could not be ordered
  by round at all.

**Fix** — a SESSION-cumulative counter at the top of the chat round body
(`chat_ops.go`), held on the host:

```go
a.Hippocampus.sessionRound++
a.hippocampusRound = a.Hippocampus.sessionRound
```

The first version of this fix used the per-turn `round + 1`, which the live run
then falsified: `gopi chat` shares ONE session log across turns
(`PersistChatSession`), so a per-turn counter restarts at 1 every turn and the
archive trail came out `[1,2,3,1,1,2]` for a three-turn session — a trail whose
round numbers repeat is exactly as unordered as one stamped 0. Monotonic across
the session is the property that makes the stamp useful, so the counter advances
per round and no turn boundary resets it. `TestHippocampus_ChatOpsRoundStampIsSessionCumulative`
pins it and goes red on the per-turn form with `turn 1 ended at round 3, turn 2
started at 1 (trail=[1 2 3 1 2 3])`.

It must live in `ChatOps` and not in the shared helper for a reason the file
states: `AtRoundBoundary` *reads* the stamp; only the loop knows which round it is.

**Why the round even mattered — a second, larger defect this exposed.** Driving the
real path showed the chat session archived **zero** `hippocampus_brief` events, not
merely mis-stamped ones: `hippoScheduler.started` is set only by
`StartOfRun`, whose only caller was `startOfRunFor` from the task loop's
start-of-run seam — a seam the chat path never walks. So the chat path's first
boundary drained an empty queue, `launchRecall` never ran, and the boundary had
nothing to stamp. The start condition is now the scheduler's own: at the top of
`AtRoundBoundary`, `if !h.started { a.startOfRunFor(ctx) }` — round 1 launches the
first retrieval, and the `started` latch (already set by the task loop) keeps the
two from both firing. `hippocampus.go`'s comment carries the measurement.

## 2. The per-turn one-shot `memory(action=store)` checkpoint

A harness message in the `chatOpsEndTurnContinuation` family, with its own stable
marker:

```go
const chatOpsTurnStoreMarker = "CHAT TURN CHECKPOINT"
var   chatOpsTurnStoreCheckpoint = fmt.Sprintf(harnessMessagePrefix+"%s: …", chatOpsTurnStoreMarker)
```

`maybeChatOpsStoreCheckpoint()` (`chat_ops.go`) injects it and returns whether it
did. **Four conditions, all required:**

| condition | why |
|---|---|
| `a.Hippocampus != nil` | the **gate**. The nil host IS the `GOPI_HIPPOCAMPUS`-off guarantee (devdoc/hippocampus.md §4.2 口径 1): with the gate off this function cannot inject anything, so chat behaviour is byte-identical. |
| `a.chatOpsToolRounded` | the turn ran at least one TOOL round — real work. A turn that only talked has nothing to store, and prompting it would invite a **fabricated write**. |
| `!a.chatOpsTurnStored` | the per-TURN latch: **one shot, never per round**. |
| `!a.memoryStored` | a store already succeeded (this turn, or an earlier turn of the same persistent session) — the same "no redundant store" rule the task loop's completion checkpoint applies. |

**Latch discipline.** `chatOpsTurnStored` is set **before** the injection, so a
store that is later refused does not re-open the prompt (a rejected prompt must not
become a loop). Both latches are reset at the **top of every `ChatOps` turn**
(`a.chatOpsToolRounded = false; a.chatOpsTurnStored = false`) because one session
log spans many turns (`PersistChatSession`) — a turn's checkpoint state must never
leak into the next turn and silently remove its checkpoint. `memoryStored` is
deliberately **not** reset: it is the run-level latch shared with the task loop, and
a persistent chat's earlier turn really did satisfy the store.

**Channel.** The checkpoint rides `Ctx.Messages` through the existing injection
channel — **no new top-level tool** (Rule 3) and **no new promptSection**. It is
logged 1:1 to the session JSONL with `LogUser(newUUIDv4(), …)`, exactly like
`roundBoundaryInjections`, so a Rule 11 check reads it as a logged `user` event. It
is injected **after** the tool results of the round, so it can never separate a
`tool_use` from its `result` — orphan-safety by construction (the same slice
reasoning `trimCommittedTaskArea` relies on).

The checkpoint cannot fire on the round-cap exit: at `maxChatOpsToolRounds` the loop
ends and returns the cap error, and the injection point is inside the tool round. A
cap-killed turn is a failed turn; asking it to store on the way out would be the
harness prompting a store for work that never landed. (Considered and rejected: an
extra `continue` after the loop to give a cap-killed turn one more round. It adds an
answer round to every ordinary capped turn, and the "give it a partial answer"
notice already owns that exit — `chatOpsRoundsWarnMessage` warns 5 rounds ahead.)

## 3. Plain `Agent.Chat` (no tools on the wire): DELIBERATELY NOTHING

`Agent.Chat` is the no-tools chat turn — one round-trip, no round boundary, no tool
round. There is **no retrieval to stamp, no action the organ could take, and no
honest store to ask for**. The checkpoint's entire value is that it names a real
knowledge gain; a turn that read and wrote nothing has none, so a store there would
be the model inventing a lesson to satisfy a prompt — a **fake write** into the
knowledge base, worse than the silence it replaces because every later task would
retrieve it. So: no note injected, no latch armed, the organ not even woken.

This is a **decision, not an omission**, and it is stated as such in the code
(`loop.go`, above `a.ChatMode = true`). The chat path that *does* real work — the
tool-enabled `ChatOps` loop — carries both the round stamp and the per-turn
checkpoint. A user who wants their chat turns to accumulate hippocampal knowledge
uses `gopi chat --tools`.

## 4. The Lv-1 chat clamp is PRESERVED

`ChatOps` already calls `a.Hippocampus.clampMaxLv(hippocampusLv1Append)` before the
loop, and that stays exactly as it is: this path HAS a round boundary but no task
flow to soft-interrupt, so **Lv-3 is unreachable by rule, not by surprise**
(devdoc/hippocampus.md §4.1). Nothing in this change raises it —
`TestHippocampus_ChatPathClampsToLv1` still pins both halves (the clamp lowers, and
it never raises a configured ceiling).

---

## Tests

New file `internal/agent/chat_ops_hippocampus_test.go`, all driving the real
`ChatOps` loop against an `httptest` SSE server and a real session JSONL:

| test | pins |
|---|---|
| `TestHippocampus_ChatOpsRoundStamp` | the launched retrievals carry rounds `0,1,2,…,N` (N = rounds the turn ran) and every archived `hippocampus_brief` carries `round > 0`, monotonic from 1 |
| `TestHippocampus_ChatOpsRoundStampIsSessionCumulative` | two turns of one persistent session: the archive trail strictly increases ACROSS the turn boundary (red on per-turn numbering: `turn 1 ended at round 3, turn 2 started at 1`) |
| `TestChatOps_HippocampusStoreCheckpointFiresOnceWithWork` | a 3-tool-round turn: checkpoint appears **exactly once** in the context, on the wire, and in the JSONL — a per-round implementation would leave three copies |
| `TestChatOps_HippocampusStoreCheckpointSkippedWithoutWork` | a zero-tool round (one answer round): **no** checkpoint anywhere |
| `TestChatOps_HippocampusStoreCheckpointSkippedWhenAlreadyStored` | a real `memory(action=store)` latches `memoryStored` ⇒ the checkpoint a later tool round would otherwise arm stays silent |
| `TestChatOps_StoreCheckpointRefusedStoreStillReportsTheToolUse` | the checkpoint's ask leads to a REAL recorded tool_use, and a refused store is a recorded `tool_result`, never silence — while the per-turn latch still holds it at one shot |
| `TestChatOps_HippocampusStoreCheckpointAbsentWhenGateOff` | with no host (the gate off) the marker appears in **no** session record and on no request body |

## Files

* `internal/agent/chat_ops.go` — round stamp, marker + message, `maybeChatOpsStoreCheckpoint`, latch reset, tool-round arm, injection.
* `internal/agent/hippocampus.go` — the chat path's start condition at the top of `AtRoundBoundary`.
* `internal/agent/loop.go` — the two latch fields, and the plain-`Chat` decision comment.
* `internal/agent/chat_ops_hippocampus_test.go` — the tests above.

## Live proof (Rule 31) — the real binary, not a unit test

`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`, a 10-line stand-in for the
model endpoint (Claude SSE for the model's streaming call, JSON for the
hippocampus organ's non-streaming call), a real `gopi sandbox create` profile, and
three real chat turns piped into `./bin/gopi chat --tools --no-editor` with
`GOPI_HIPPOCAMPUS=1`:

```
$ printf 'read /etc/hostname and tell me the hostname.
just say hello, no tools.
...
/exit
' | \
  timeout 90 env -u ANTHROPIC_BASE_URL GOPI_HOME=$SB GOPI_SETTINGS=$SB/settings.json \
  GOPI_HIPPOCAMPUS=1 DEEPSEEK_BASE_URL=http://127.0.0.1:8861 ... ./bin/gopi chat --tools --no-editor
[gopi] 🧠 hippocampus brief archived (lv-0-archive, confidence=0.10, memories=0)
[gopi] 🧠 chat turn checkpoint: asking for one memory_store before the final answer
[gopi] 🧠 hippocampus brief archived …
turn one done.
you> …
turn two: just talking.          ← a NO-TOOL turn: no checkpoint line for it
you> …
done.
```

and the session JSONL it wrote:

```
checkpoint records: 1 | memory tool_use: 1
  memory tool_use: {"action": "store", "answer": "Yes: the per-turn checkpoint rides
                    the tool-result channel, the model stores for real, and the
                    session JSONL shows both the logged user event and the
                    memory tool_use …
```

That single run observed all four claims at once: a chat turn really did archive
under a nonzero round, the harness checkpoint really did fire once and reach the
model, the model really did call `memory(action=store)` in response, and the
tool-less turn really did get no checkpoint at all.
