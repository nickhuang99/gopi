<!-- doc-class: normative -->
# The chat write boundary is frozen by EVERY face that executes tools — not only by `gopi chat`

**Class**: normative (which startup paths must call `tools.SetChatPolicy`, and why)
**Landed**: 2026-10-22 · task `01a16225-bc8a-78d2-9012-527a7cc8ecd5`, base `cb14fad`
**Files**: `cmd/agent/chat_policy_flag.go` (`chatFreezeChatPolicyState`),
`cmd/agent/cli_web.go`, `cmd/agent/cli_term.go`, `cmd/agent/cli_term_once.go`,
`cmd/agent/cli_ui.go`, `cmd/agent/cli_web_align_test.go` (the R2 fixture),
`cmd/agent/cli_web_policy_freeze_test.go` (the new real-surface regression)
**Read with**: `devdoc/chat-policy.md` (the rule layer + the posture) —
this page owns one question that doc's §4 states but does not place: *which
processes freeze the pair.*

## 1. The defect, measured

`internal/tools` holds the judgement in ONE frozen pair (`chatPolicyFrozen`, set
by `tools.SetChatPolicy` / `tools.SetChatOpCategories`). Its DEFAULT is the
shipped `prompt *` table with the on-request posture — i.e. **every write/edit is
a question for a human**.

`gopi chat` froze the pair at startup (`chatAnnounceChatPolicy` →
`chatFreezeChatPolicyState`). The other faces that EXECUTE TOOLS — `gopi web`,
`gopi cli`, `gopi cli --once`, `gopi ui` — built their `webServer`/agent and
served turns **without ever calling it**. So on those surfaces the boundary never
saw the operator's ```chat-policy fence nor the posture the startup channels
resolved; it judged by the package default.

The observable (task `01a16225`, red at `cb14fad`):

```
--- FAIL: TestWebChat_R2_ToolsFlagRoutesThroughChatOpsAndStreamsToolFrames
    cli_web_align_test.go:350: tool round did not really write …/made-by-web-test.txt: no such file or directory
```

and the tool result the model received, from a debug run of the same fixture:

```
event: tool
data: {"kind":"result","name":"write","summary":"[NEEDS OPERATOR APPROVAL — command NOT run] `…/dbg-made.txt` — matched rule `*` (bu…"}
```

That is the boundary answering *“built-in chat-policy defaults:2”* — the
package default, not the operator's rules. The tool round itself was genuine
(`executeToolBatchCtx` → `executeToolDefaultCtx` → `tools.Write`); the write was
stopped before it ran.

## 2. The fix: one resolution function, called by every tool-executing face

`chatFreezeChatPolicyState(facePrefix, policyFlagValue)` is the extracted
resolution+load half of `gopi chat`'s startup (Rule 2: one reader). It resolves
the posture from the channels (`--policy` → `GOPI_CHAT_POLICY` → settings.json
`chat_policy` → the legacy `chat_op_auto` mapping → default), loads the
```chat-policy and ```chat-op-category fences from the client's own rule file,
freezes both, and prints every note/parse error prefixed with the face that owns
the run.

`chatAnnounceChatPolicy` is now just the `gopi chat` caller of it (it adds the
one startup line under `--verbose`).

Each face calls the freeze immediately before building its server:

| face | call site |
|---|---|
| `gopi web` | `runWeb` (`cli_web.go`, after the listener binds) |
| `gopi cli` | `runTermChat` (`cli_term.go`) |
| `gopi cli --once` | `runTermOnce` (`cli_term_once.go`) |
| `gopi ui` | `uiStartServer` (`cli_ui.go`) |

**Nothing was weakened to make a fixture pass.** The boundary is unchanged: a
`prompt` rule still asks, a `forbidden` rule still refuses, and the default
posture is still on-request. What changed is that these faces now ASK THE
OPERATOR'S CHANNELS instead of a constant — which is the behaviour
`devdoc/chat-policy.md` §3 already declares for the chat surface.

## 3. The R2 fixture also froze it — and that is the fixture being corrected too

`webServerForTestTools` constructs a `webServer` by hand, so it stood in for a
web server that skipped the freeze. It now declares the posture through the SAME
channel a real run uses (`GOPI_CHAT_POLICY=yolo`) and calls the same
`chatFreezeChatPolicyState`, then restores the defaults with
`t.Cleanup(tools.ResetChatPolicyToDefaults)`.

Both halves are needed: the production call site is the fix, and the fixture's
freeze keeps the in-process case honest about what it is measuring (frame shape
and session-JSONL evidence) rather than accidentally measuring the boundary.

## 4. Evidence — red then green, on the REAL surface

`cmd/agent/cli_web_policy_freeze_test.go` drives a live `gopi web --tools`
(testutil.GopiBin, the pin built from the tree under test) over HTTP with a
scripted tool round, in two arms that differ ONLY in the posture channel:

* `GOPI_CHAT_POLICY` unset → on-request: the file must NOT appear, and the
  streamed tool result must carry `[NEEDS OPERATOR APPROVAL`. (This arm is what
  makes the other arm evidence: a binary that ignored the boundary would pass the
  allow arm and fail here.)
* `GOPI_CHAT_POLICY=yolo` → the file appears with the requested content.

Red proof: with `chatFreezeChatPolicyState(chatFacePrefixWeb, "")` removed from
`runWeb` (production file only, test kept), the allow arm fails and the streamed
result is the approval text:

```
event: tool
data: {"kind":"result","name":"write","summary":"[NEEDS OPERATOR APPROVAL — command NOT run] `…/made-by-real-web-run.txt`…"}
--- FAIL: TestWebProcessFreezesTheChatWriteBoundary/declared_allow_posture_runs_the_write
```

Restore measured with an explicit revision, not an index-relative diff:
`git diff HEAD -- cmd/agent/cli_web.go` shows only the 13 added lines (the call +
its comment), i.e. the file is the tree's version plus the fix.

## 5. Residue — where the same hole may still exist

`merge_run.go` (deleted 2026-11-01 with the top-level merge verb),
`cli_resume.go` and `main.go` build agents too. They do not
serve chat turns, so the write boundary does not gate them (it fires only when
`Agent.ChatMode` is set). The rule this page states is therefore scoped by the
question it answers: **any NEW face that runs tool rounds with `ChatMode` set
must call `chatFreezeChatPolicyState` at startup**, or it will judge every write
against `prompt *` and silently stop the model instead of executing it.
