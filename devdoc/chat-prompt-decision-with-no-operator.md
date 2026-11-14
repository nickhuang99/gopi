<!-- doc-class: normative -->
# A `prompt` decision names an answer that exists — and a chat's own session directory is not a question

**Status:** landed 2026-10-22 (task `01a169b5-4c25-7d86-81f5-9f5a8dc24a56`).
**Follows:** `devdoc/chat-prompt-approval-channel.md` (76e6faf), which made a
`prompt` decision answerable *when an operator turn exists*.

## The two measured contradictions

Both were observed on real unattended runs (`gopi chat --once --tools`), and they
are one defect seen from two sides: **the write boundary told a run something that
was not true of that run.**

**(a) A run was refused its own session directory.** Rule 37 says a chat's
artifacts belong in `~/.gopi/sessions/<id>/` and never in a git-tracked path. That
directory is *outside every git work tree*, so no `write <tree>` rule in the
operator's ```chat-policy fence can mention it, so the frozen **posture** answers —
and the shipped posture is `prompt *`. The tool result was:

```
[NEEDS OPERATOR APPROVAL — command NOT run] `/…/.gopi/sessions/<id>/artifacts/x.md`
… It is therefore neither allowed nor forbidden — it needs a HUMAN decision …
The file was NOT written.
```

So the same prompt said *"keep your artifacts in your session directory"* and
*"writing to your session directory needs a human's permission"*.

**(b) The refusal asked a human who does not exist.** Its tail read

```
DO THIS NOW: ask the operator in the conversation — quote the exact path … and
wait for their answer.
```

while the same system prompt carried `chatUnattendedFailureShape`:
*"The FAILURE shape of this run is ending by ASKING instead of delivering."* Two
instructions a model reads in the same turn, and it cannot obey both. Measured on
a real one-shot run: the model re-issued the refused call until its budget ran out.

The same untruth was in the **prompt** as well as the tool result: rule (1) of the
write-boundary paragraph said every run *"STOP[s] and ask[s] the operator in the
conversation … and retr[ies] only after they say yes."*

## What replaced it

Two mechanisms, each stated where it is decided
(`internal/tools/chat_operator_presence.go`).

1. **The session directory is exempt from a PROMPT decision.**
   `chatWriteTargetIsOwnSessionArtifact` measures containment with `pathWithin`
   (cleaned segments + stat identity, not a string prefix) against
   `signalrec.SessionDir()` — the *same* resolver the prompt's `[CURRENT SESSION]`
   block is built from, so the guard and the sentence cannot disagree. It is
   applied **only on the prompt branch**: a `forbidden` rule still forbids the
   directory, and an empty resolver answer exempts nothing.

2. **Presence is a fact with two readers, not a knob.** `SetChatOperatorAbsent` /
   `ChatOperatorAbsent` are process-wide state published by the ONE place that
   knows the run's contract — `cmd/agent/cli_chat.go`, beside
   `agent.Unattended = sess.unattended` — and read by (i) the two refusal texts and
   (ii) the prompt's own `chatPromptAnswerRoute`. On an absent operator the text
   names the dead end, hands over the two routes that exist (deliver into the
   session directory; state which target was refused), and names the
   authorisation that is decided *before* a run starts
   (`--policy=yolo`, or an `allow` rule in the fence).

   **Why not an env var (Rule 35):** the fact is already in-process. A second
   channel would be an ambient artifact a child could inherit after the run it
   described had ended — and "a setting invented to avoid one sentence" is exactly
   what `chat_approval_authorization.go` rejected for the approval channel itself.
   The default is **attended**, so no surface becomes unattended by forgetting to
   say anything.

## What was deliberately NOT changed

- **`forbidden` is untouched.** It was never a question; it still refuses the
  session directory and still returns before the exemption is consulted.
- **The attended console is byte-for-byte what it was.** There a human really is
  there, and their own next message is the approval channel
  (`ChatTurnApprovesTheToolQuestion`), so "ask and retry" is true.
- **No new tool, no new flag, no new file format** (Rule 3). One leaf constant
  moved: `cliverbs.PolicyFlag` now holds `--policy` because `internal/tools` must
  spell it in a refusal and cannot import `internal/agent` (cycle); the agent-side
  name is an alias.

## The checks that hold it

| what | where |
|---|---|
| session dir exempt under `prompt *`; refused under `forbidden`; sibling-with-prefix not exempt; no session dir ⇒ no exemption | `internal/tools/chat_operator_presence_test.go` |
| both arms' unattended text never says "ask … wait"; attended text still does; presence defaults to attended | same file |
| the **dispatch** reaches the predicate (real arg map, real guard, real session dir) | `internal/agent/chat_own_session_write_test.go` |
| the **prompt's** route follows the same fact, both directions | `TestChatWriteBoundaryPromptDecisionRouteFollowsWhoIsReading` |
| the real binary, real mock LLM, real one-shot run | `cmd/agent/chat_unattended_write_e2e_test.go` |

The E2E case carries the lesson of its sibling (`edit_tool_arg_refusal_e2e_test.go`):
its "the model was told X" assertion is **sliced at the tool result**, because the
system prompt legitimately carries the attended wording — a whole-body substring
test would have been red for a reason that has nothing to do with the fix, the
mirror image of that case's vacuous green.
