<!-- doc-class: normative -->
# A `prompt` decision is a question — and the operator's own words are its answer

**Status:** implemented (`internal/tools/chat_approval_authorization.go` + the two
guard arms). Task `01a16885-6754`.

## The defect, measured

The chat write boundary (`internal/tools/chat_write_guard.go`,
`internal/tools/chat_policy_gate.go`) decides `allow` | `prompt` | `forbidden` for
every bash / write / edit call a chat surface makes. Two of those three had a
destination: `allow` ran the call, `forbidden` returned a refusal. `prompt`
returned text:

```
[NEEDS OPERATOR APPROVAL — command NOT run] `echo probe-two` — matched rule `*`
(built-in chat-policy defaults:2)
… this surface has no approval channel … ask the operator … retry only on a yes
```

The retry is the SAME rule, the same decision, the same text. Nothing the
operator could say changed the answer, so `prompt` was not a question — it was a
permanent deadlock wearing a question's clothes.

Under the shipped defaults this made every bash and write call dead on any surface
whose profile carries no `settings.json` document: the built-in table is
`prompt *` and the built-in posture is on-request, so *everything* lands on the
`prompt` branch. Measured 2026-10-22 on the real binary:

```
$ gopi chat --once --tools --policy=ask -p "Use the write tool to create …/probe-w2.txt …"
⚙ call write …/probe-w2.txt
↳ result write [NEEDS OPERATOR APPROVAL — command NOT run] …/probe-w2.txt — …
$ ls …/probe-w2.txt
ls: cannot access '…/probe-w2.txt': No such file or directory
```

Two suite cases were red for exactly this reason and neither said so: the PTY
escape case reported "the bash command never published its pid … the tool round did
not start a command", and the `--once --tools` case reported a missing result row
(it was looking for a glyph the product has never drawn — see §4).

## The fix: the answer is the operator's own next message

`ChatTurnApprovesTheToolQuestion(userInput)` (`internal/tools/chat_approval_authorization.go`)
is a **pure function of one turn's text**. The agent publishes the turn with
`publishChatTurn` (`internal/agent/chat_write_guard.go`) — machinery that already
existed for the cleanup gate — and both guard arms consult it on the `prompt`
branch only:

| decision | no approval in this turn | approval in this turn |
|---|---|---|
| `allow` | runs | runs |
| `prompt` | approval text, NOT run | runs — then the same remaining gate an `allow` meets |
| `forbidden` | refused | **still refused** |

The design constraints, and why each alternative was rejected:

1. **No stored state — it cannot become an always-on switch.** Nothing is
   remembered; the guard re-derives the answer from the turn's text on every call,
   and the publisher restores the previous value when the turn ends. A turn that
   says nothing approves nothing, and a previous turn's approval cannot leak into
   a later one. The rejected designs were a settings key or an env var: both are
   persistent dependencies (Rule 35) invented to make one sentence unnecessary,
   and both would be a switch somebody leaves on.
2. **A negation always wins.** "don't run it" and "不要执行" both contain an
   approval word; reading either as consent is the failure that would matter most.
   The negation list is the cleanup gate's own (`chatCleanupNegations`),
   referenced rather than re-typed (Rules 13/14).
3. **It never reaches `forbidden`.** The predicate is consulted only inside the
   `prompt` arm, so there is no spelling in which an affirmation turns a hard no
   into a yes. An approved prompt then runs the same remaining gate an `allow`
   does — the cleanup faces' destructive half still needs the operator to have
   named cleanup.
4. **Word boundaries for English, substrings for CJK.** "ok" is an answer;
   "token", "look" and "broke" are not. Chinese approvals are runs of characters
   inside an unspaced sentence ("可以，跑吧"), so they match as substrings. The
   asymmetry is the languages', not a preference, and it is pinned by test cases
   named for each false positive a naive matcher would produce.

**The honest limit, stated rather than hidden**: the boundary holds no memory of
*what* it asked, so an affirmative message answers the prompt decision it meets in
that turn. That is bounded by what a prompt decision already is — a command the
operator's own rules marked "ask me first" — and the approval text tells the model
to retry the SAME command, never a re-spelt one. Correlating an answer to one
specific question would need state, which is the debt this channel exists to
avoid.

## The two spellings, so a caller cannot open the channel by accident

- `ChatBashWriteRefusal(command)` / `ChatWritePathRefusal(path, root)` — the
  original signatures, unchanged, holding the channel **closed** (they pass an
  empty turn). Everything that measures the *asking* behaviour, including the
  default guard path and the existing tests, uses these.
- `ChatBashWriteRefusalAuthorized(…)` / `ChatWritePathRefusalAuthorized(…)` — the
  forms that take the turn message. The agent's dispatch
  (`chatModeWriteRefusal` → `chatToolWriteGuard`) uses these, passing
  `a.chatTurnInput`.

Adding a parameter to the existing functions was rejected: it would have compiled
to silence at every call site that already existed, turning "measuring the
questioning behaviour" into "accidentally approving".

## The suite cases: a precondition and one stale assertion

- `TestChatPTY_EscapeKillsARunningBashCommand` now starts the chat with
  `--policy=allow` (a named constant in the file). The case is about an Escape
  reaching a **running** command; with the fixture's empty profile the default
  posture questions the bash call, so no command starts and there is no pid to
  Escape out of. That is a fixture precondition, not a product defect, and it is
  now stated at the call site.
- `TestChatOnce_ToolsTurnReallyCallsATool` asserted `⚙ result read`. The product
  draws the tool-call row with the call marker and the RESULT row as a
  continuation of the same note (`internal/lineedit.NoteContinuation`, `↳ `), so
  the assertion demanded a shape the surface has never printed and failed with
  "stderr does not report the tool result" while the result was right there in the
  output. It now asserts the two real glyphs by symbol (`lineedit.ToolGlyph`,
  `lineedit.NoteContinuation`) rather than re-typing them (Rule 13/14).

## Verification

Run from the ground `01a16885-6754-7a8b-b2b3-5cbfe15305b0`; the evidence files
named below are under that task's session directory.

```
$ go test ./internal/tools/ -run 'TestChatWriteBoundaryAPromptIsAnsweredByThisTurnsOperator|TestChatBashBoundaryAPromptIsAnsweredByThisTurnsOperator|TestApprovalNeverOpensAForbiddenRule|TestChatTurnApprovesTheToolQuestion' -count=1
ok  	github.com/nick/gopi/internal/tools	0.415s

$ GOPI_BIN=./bin/gopi go test ./testsuite/ -run '^TestChatOnce_ToolsTurnReallyCallsATool$' -count=1
ok  	github.com/nick/gopi/testsuite	1.660s

$ GOPI_BIN=./bin/gopi go test ./testsuite/ -run '^TestChatPTY_EscapeKillsARunningBashCommand$' -count=1
ok  	github.com/nick/gopi/testsuite	15.934s
```

**RED proof** (the minimal reproduction, and the restore criterion): with the
approval read deleted from the write arm the reproduction fails —

```
$ go test ./internal/tools/ -run 'TestChatWriteBoundaryAPromptIsAnsweredByThisTurnsOperator' -count=1
--- FAIL: TestChatWriteBoundaryAPromptIsAnsweredByThisTurnsOperator (0.00s)
    chat_approval_authorization_test.go:69: the operator's approval did not release the write
        got "[NEEDS OPERATOR APPROVAL — command NOT run] …"
```
— and with the arm restored:
```
$ go test ./internal/tools/ -run 'TestChatWriteBoundaryAPromptIsAnsweredByThisTurnsOperator' -count=1
ok  	github.com/nick/gopi/internal/tools	1.902s
$ test "$(git hash-object internal/tools/chat_write_guard.go)" = "$(git hash-object /tmp/cwg.bak)"
RESTORE IDENTICAL to pre-mutation backup
```

**The real surface, both directions** (Rule 31, verbatim):

```
$ gopi chat --once --tools --policy=ask -p "Use the bash tool to run exactly: echo probe-four. Report what it printed."
⚙ call bash echo probe-four
↳ result bash [NEEDS OPERATOR APPROVAL — command NOT run] `echo probe-four` — matched rule `*` …

$ gopi chat --once --tools --policy=ask -p "Yes, go ahead — approved. Use the bash tool to run exactly: echo probe-five. Report what it printed."
⚙ call bash echo probe-five
↳ result bash exit=0

$ gopi chat --once --tools --policy=ask -p "Yes, approved, go ahead. Use the write tool to create …/probe-w.txt containing exactly: hello-approval"
⚙ call write /home/user/workspace/gopi-approval-real/probe-w.txt
↳ result write Successfully wrote 14 bytes to …/probe-w.txt
$ cat …/probe-w.txt
hello-approval
```

The same three probes against the PRE-fix binary: the second and third returned the
approval text and the file was never written — the deadlock, reproduced end to end.
