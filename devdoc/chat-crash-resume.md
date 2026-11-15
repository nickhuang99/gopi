<!-- doc-class: normative -->
# Chat Crash Recovery — what a `gopi chat` crash actually leaves, and how it resumes

Task: `01a0eb0ec673771a937ecb4efbcebb1b` (branch `task/01a0eb0ec673771a937ecb4efbcebb1b`).
Measured 2026-09-29 against `bin/gopi` built from this branch.

## The question

When a `gopi chat` session crashes mid-run, how does it resume? Four things
were unknown and are now measured rather than assumed: (a) does the resume
path work at all, (b) does passing the *same command line back* resume
anything, (c) is the argv the crashed run used recorded anywhere (so a bare
`resume` could replay it), (d) does the help text tell an operator what to do.

## The answer (measured, not inferred)

**A crashed chat resumes by re-running the verb with the session id as its
seed argument. No new verb, no `--resume` flag.**

```bash
gopi log --sessions              # find the session (a crashed one has no END row)
gopi chat --tools <session-id>   # resume it — with the flags the run had
```

The seed loads the last `→ REQUEST` checkpoint in the run's raw log as
conversation history. Verified live: a chat with two answered turns was
SIGKILLed, then resumed with `gopi chat --tools <id>` → stderr said
`seeded 7 history message(s) from …raw.log`, and the model recalled the killed
run's *first user question verbatim* from context alone (a question it could
only answer from the seeded history). Evidence:
`~/.gopi/sessions/01a0eb0e-…/live1/resume1.log`.

### What is NOT recorded: the command line

The checkpoint carries the **conversation and the tool definitions**; it does
not carry the argv. So "the user just types `resume`" is not a thing that can
work today — the flags must be replayed by hand. On the briefing's three
options, the measured state is the middle one: **the session is recorded, the
argv is not**, and there is no `chat --resume` subcommand (nor is one needed —
the positional seed already is the resume path).

## Three defects found by driving the real thing (Rule 31)

All three were invisible from reading the code; each one was reproduced live
before it was fixed.

### 1. A killed chat was reported as `EXIT: SUCCESS (exit=0)`

`gopi log <killed-chat-session>` printed `EXIT: SUCCESS (exit=0)`. The
mechanism: `ParseFile`'s live-session fallback derives cost totals from the
per-iteration `request` events when there is no `session_end`, and leaves
`ExitCode` at its zero value — "still running: no failure recorded". The
formatter then read `ExitCode` alone, so *"nothing recorded how this ended"*
rendered as *"it ended successfully"*. A SIGKILLed run is the case that makes
this a defect: it did real work, it has no end marker, and the one row an
operator reads to judge the run said the opposite of the truth.

Fix: `Session.EndUnrecorded` (set at exactly one place, the log reader, as
`!sawEnd`) and an `EXIT:` row that branches on it —
`EXIT: UNKNOWN (no session_end recorded)`. `ExitCode` is unchanged on the wire
and for every recorded exit; only the no-end shape gains words.
`internal/gopilog/{types,reader,formatter}.go`; tests
`TestParseNoSessionEnd` / `TestParseRecordsEndWhenSessionEndPresent` /
`TestFormatSessionNoEndIsNotSuccess`.

### 2. Resuming a `--tools` session without `--tools` silently dropped the tools

Measured on the crashed session: resuming it **without** `--tools` seeded the
history *including* `tool_use`/`tool_result` blocks, but sent a request with no
`tools` array — and the model answered by *describing the call it could not
make* (`resolveResumeLogPath 是做什么的（基于 … 的 read 结果回答）`) instead of
making it. Nothing said a word: the conversation recovered, the capability did
not, and the only symptom was a worse answer.

Fix: the checkpoint's `Tools` array is now read (the seed call already parses
the whole `ClaudeRequest`; it was being discarded with `_`), and a mismatch
prints one line naming what was lost and the flag that restores it:
`⚠ the seeded session was TOOL-ENABLED (18 tool definition(s) …) but this run
has tools OFF … Re-run with --tools`. Silent when nothing is lost (a plain
chat resumed plainly, or a tool-enabled one resumed with `--tools`).
`cmd/agent/cli_chat.go`; `chatSeededToolsMismatch` is a pure predicate so the
rule is testable without driving the REPL —
`cmd/agent/chat_resume_tools_test.go`.

### 3. The recovery path was undiscoverable, and a killed-before-first-reply chat is in no index

`gopi chat -h` documented `[session-id]` as a seed but never said that this
*is* the crash-recovery path, never said the flags must be replayed, and never
said where to find a crashed session. Worse, `gopi chat list` reads the **chats
ledger**, and the row is written by `RegisterChatSession` on the first
*successful turn* — so the SIGKILLed session had **no row at all** and was
absent from the very list an operator would search. (Cross-checked: the
sandbox ledger's `chats` table held rows for all four completed chats and none
for the killed one.)

Fix: a `Recovering a chat that CRASHED` section on the help page naming the
exact commands, the flag-replay rule, and the `chat list` blind spot.
`cmd/agent/cli_chat.go`; `TestChatHelpStatesTheCrashRecoveryRecipe`.

## What was deliberately NOT done

- **No new verb, no `--resume` flag.** The positional seed already resumes;
  a second spelling would be a second path through the same code (Rule 3/Rule
  13 spirit). The gap was disclosure, not capability.
- **No argv replay.** Recording the command line so a bare `resume` could
  replay it is a new persistent artifact with a lifecycle question (Rule 35)
  and it is not needed to answer the operator's question — `gopi task status`
  already carries the argv for *task* rows, and a chat is not a task row. Not
  done, and named here rather than half-built.
- **Not fixed (found, out of this task's scope):** the killed chat's ledger row
  is never written, so `gopi chat list` cannot show a crash *before* a first
  reply. Making the row appear earlier (on session start rather than on first
  turn) is a behaviour change to the chats ledger; it is reported, not smuggled
  in. `gopi log --sessions` finds such a session today (help says so).

## Test-suite note

`cmd/agent`: `TestChatStream_RepaintsStayLinearInTheAnswer` and
`TestChatTurnHeader_PaintsAMultiRowCountedHeaderOnARealPty` fail **on this
branch and on a pristine `HEAD` worktree alike** (verified: a detached worktree
at `HEAD` produced the same two FAILs) — pre-existing, unrelated to this
change. The rest of `-run TestChat` is green.
