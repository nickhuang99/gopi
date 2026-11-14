<!-- doc-class: historical -->
# The chat write boundary, seen through a fixture that skipped it

*Task `01a1625b` (2026-10-22). A repair run whose first deliverable was a
refutation.*

## What was reported, and what was there

A `cmd/agent` test —
`TestWebChat_R2_ToolsFlagRoutesThroughChatOpsAndStreamsToolFrames` — was
described as red at base `b880d8a` with this diagnosis:

> the parent dir `/003` does not exist at assert time (推断, unverified)

At this ground's HEAD the test is **green** (in isolation, and at `-count=5`).
The reason is upstream: commit `56736d8` *"fix(chat): every tool-executing face
freezes the chat write boundary"* touched exactly this file. `git show
b880d8a:cmd/agent/cli_web_align_test.go` carries **zero** occurrences of the
freeze markers (`GOPI_CHAT_POLICY`, `chatFreezeChatPolicyState`,
`t.Cleanup(tools.ResetChatPolicyToDefaults)`); at HEAD it carries three.

And the stated root cause is **wrong** — measurably, not by argument. Deleting
the one freeze line from the fixture and re-running reproduces the briefing's
exact failure text, and the SSE stream in that same run names the mechanism:

```
event: tool
data: {"kind":"result","name":"write","summary":"[NEEDS OPERATOR APPROVAL — command NOT run] …"}
event: done
…
tool round did not really write …/003/made-by-web-test.txt: no such file or directory
```

The `/003` in the message is the **`t.TempDir()` segment** printed inside the
path, not a directory that is missing. The parent exists; the **write was never
run**. `os.ReadFile`'s `no such file or directory` is the *symptom* of a refused
tool call, and reading it as "the directory is absent" is the trap this page
exists to keep shut: **a file-absence assertion and a directory-absence
hypothesis produce the same errno**, so the errno can never choose between them.
The stream — the tool RESULT — is what distinguishes them.

## Why a hand-built webServer loses the boundary

`webServerForTestTools` constructs a `webServer` directly instead of going
through `runWeb`. The chat write boundary is frozen **once per process** by
`tools.SetChatPolicy` (Rule: freeze, not configuration), and the *freezing* is a
startup act of the face — `runWeb` calls `chatFreezeChatPolicyState` before
serving its first turn. A fixture that skips it does not run against "no
policy": it runs against the `internal/tools` **package default** — the built-in
`prompt *` table with the on-request posture — which answers every write with an
approval request addressed to an operator who is not there. The tool round
"executes"; it executes into a refusal.

This is worth stating because the failure mode is silent by construction: a
refused tool call is a perfectly normal, fully streamed tool result. Nothing
errors, nothing warns; the only divergence is a file that does not appear.

## What this task shipped

Nothing in production. The repair was already on master, and re-landing it would
have been a duplicate. What was missing was the **guard**:

`cmd/agent/cli_web_align_writeboundary_test.go` →
`TestWebTestToolFixtureFreezesTheChatWriteBoundary`, which reads the frozen pair
the dispatch itself judges by (`tools.ChatPolicyFrozen()`) rather than
duplicating the R2 file assertion. It is two-sided on purpose:

- **baseline arm** — the shipped default must still ASK. Without it, a package
  whose default already allowed everything would satisfy the second arm with no
  freeze at all, and the guard would be vacuous.
- **fixture arm** — after `webServerForTestTools` returns, the frozen posture
  must allow everything, i.e. the fixture really did freeze what it declares.

Red-proof: delete the freeze line → the guard fails with
`frozen posture = "prompt" (source "default"), want … ("yolo")`. Restore → green,
verified content-identical to `HEAD` (`git hash-object` vs
`git rev-parse HEAD:<path>`, Rule 5's restore criterion).

## The transferable lesson

When a "file was not created" assertion fails, **read the tool result before
forming a filesystem hypothesis**. In this codebase a chat-surface tool call has
four outcomes — ran, asked, refused, forbidden — and only the first creates the
file; the other three are all silent at the file layer. The tool-result frame
(SSE `kind:result`, or the session JSONL `tool_result`) names which one happened,
and it is produced by the same dispatch the assertion is about.

Corollary for fixtures: a test double that constructs a production struct by
hand inherits every *package default* the real constructor would have overridden
at startup. The boundary, the policy table and the category table are all frozen
that way — so a hand-built fixture must perform the startup acts its face
performs, or it is testing a process that never existed.
