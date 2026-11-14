<!-- doc-class: normative -->
# The chat surface is told its OWN context — and two limbs that never lie

**Task:** `01a0e167-d493-77d0-a404-9a0b5cb1cdc9` (briefing: *the chat surface is
not told its own context — the plain path sends 3 sentences forbidding any
mention of internals, `--tools` sends the full TASK prompt, and neither is told
where it is running*).
**Worktree:** `/home/user/workspace/gopi-01a0e167d49377d0a4049a0b5cb1cdc9`, branch
`task/01a0e167d49377d0a4049a0b5cb1cdc9`, commit `6dc8173b`.
**Evidence:** `/home/user/.gopi/sessions/01a0e167-d493-77d0-a404-9a0b5cb1cdc9/evidence/`.
**Report:** `/home/user/.gopi/sessions/01a0e167-d493-77d0-a404-9a0b5cb1cdc9/task-report.md`.

---

## The defect, stated as a measurement

Before this change (base `78d0aa83`), three surfaces carried three different
truths about the same process:

| path | system prompt | tools | project context |
|---|---|---|---|
| plain chat (`Agent.Chat`) | `internal/agent/loop.go:1127` `chatSystemPrompt`, whose **last sentence forbade mentioning tools/internals** | **none** (`internal/agent/loop.go:4386` `nil, // no tools`) | none |
| chat `--tools` (`ChatOps`) | `chat_ops.go:503` — `a.SystemPrompt`, i.e. the **full TASK prompt** (`assembleSystemPrompt`, `internal/agent/loop.go:2936`→`3145`) | all 17 (`chat_ops.go:504`) | none |
| task run | same task prompt | same 17 | yes (`internal/agent/loop.go:2947` → `3142`) |

Neither chat path knew its hostname, its node role or its repo — and the plain
path's own prompt told it not to ask. Instruction and gap pointed the same way,
which is why the defect survived review: nothing on the surface was *wrong*, it
was simply *silent about itself*.

## The design decision worth recording

**The chat block lives BESIDE `promptSections`, never inside it.**
`promptSections` is the TASK prompt's spine and is asserted elsewhere
(`TestSystemPrompt…` suite, the density gate). Appending the chat block there
would tell every background task run that it is an interactive console with an
operator watching — false, and false in a way that changes behaviour. So
`internal/agent/chat_context.go` owns the block and the two chat composition
points call it; `TestTaskPromptCarriesNoChatContext` asserts the task prompt
still carries **neither** the block nor either limb. This is the same shape as
the existing rule in this repo: a section that describes a surface is wired into
the path that IS that surface.

**Runtime values ride `PromptSection.Render`, at assembly time.** Where:
`host <os.Hostname()> · node <config.ResolveNodeRole()> · repo <cwd-or-git-root>`.
Nothing is cached at package level and nothing mutates mid-session, so the
kernel prompt stays cache-stable — which is exactly the property the epic named,
and the reason not to "just" fix this with a `fmt.Sprintf` at the send site.

**One block, two limbs — because an instruction the agent cannot act on is
worse than none.** The epic's own sentence. The plain path has no tools, so
telling it to run `gopi ui_state --target chat` would be an instruction it can
only fail at:

- `chatNoToolsLimb` — *"This path carries no tools: you can NAME these surfaces
  but cannot run them — say that plainly instead of guessing what the screen
  says or asking the operator to read it for you."*
- `chatToolsLimb` — *"With tools on this path, run each of these through bash
  yourself."*

`TestChatOpsPromptCarriesChatContextBlock` asserts the `--tools` path has the
tools limb and **not** the no-tools limb, so a mis-wired pair turns the suite
red rather than producing a confidently wrong sentence.

**Verb spellings come from `internal/cliverbs`, not from literals** (Rule 1/13/14):
`cliverbs.DumpScreen`, `cliverbs.TargetFlag`, `cliverbs.SessionFlag`. A drift
guard (`cliverbs_test.go`) pins each to a real occurrence in `cmd/agent`, so
renaming the flag on the command surface without renaming the constant fails the
build instead of teaching the model a flag that no longer exists.

## The operator's rule, in the block and in this doc

On the chat surface the cell grid is the observation channel and
**`screen_capture` / `capture` + vision is not** — a pixel snapshot answers a
question `gopi ui_state --target chat` answers for free, at a fraction of the
tokens. The block states it as the operator's rule for this surface; the reason
it is a *rule* and not a preference is that a vision round-trip on every
self-check would cost more than the checks are worth. When a check IS worth the
tokens: when the **presence, width or wrap of a line is the question** —
otherwise answer.

## The write boundary — the same block, a later rule (2026-09-28)

This block is the chat-specific extension point, and a SECOND rule has since
landed in it: **what this surface may write, and where** — no git mutation, no
artifact into a git-tracked path, this run's own session directory for what the
operator asks to keep, `gopi task submit` for anything that must be committed.
It carries the same two-limb parameterisation as the limbs above (a false
instruction on the tool-less path is worse than none) and a mechanism half that
refuses a crossing call at the tool dispatch. The full record, including the
rejected alternative (giving chat its own worktree) and the Rule 31 live
acceptance, is `devdoc/chat-write-boundary.md`; the prompt half is
`internal/agent/chat_write_boundary.go`, the enforcement half
`internal/tools/chat_write_guard.go`.

## The honest non-reach (and why it is recorded, not smoothed over)

The prompt-content claim is evidenced from **emitted bytes** (a local
Anthropic-SSE stub captured each request body, so the pasted prompt is what went
on the wire, not what a function returned), and a real model turn reaches the
values (`host peer1`, the repo path, and both commands named correctly).

What did **not** reach: an interactive pty turn's **answer band**. Six attempts
painted the typed line (`you> Where are you running, …`) and wrote the dump
(`bytes=5778 pid=3251068 dumps=5 frames=11 grid=117x26`), but the status row
stayed `0s · ready` with `iter 0` — **the Enter never submitted the turn**. The
earlier hypothesis ("the request went somewhere else") is superseded: the paint
and the dump are both real, and the missing half is the SUBMIT. That is a
question about `gopi chat`'s interactive input on a non-tty-launched pty, not
about this change, and it is handed to a fresh task rather than asserted as a
pass. A report that claimed the band would be claiming a gate it did not
observe (Rule 5's measured case).

## Reusable recipe

Driving a TUI under a real pty from a script: `pty.fork()` + `TIOCSWINSZ`
(rows/cols) — a pipe is not a terminal and the TUI will not paint into it — then
`ASK=<file>` into the master fd, pump with `select`, and read the dump path out
of the stream's own `#gopi-chat-dump-report` line instead of guessing the
session directory. Send `\r`, not `\n`, for Enter. Evidence:
`evidence/pty_chat.py` (`ASK=… python3 pty_chat.py log.raw ./bin/gopi chat --dump-screen`).
