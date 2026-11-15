<!-- doc-class: normative -->
# gopi chat exited without notice — a failed turn ended the session

**Bug**: task `bbc4efd8-6bdd-4d6c-aa16-86bae2cecc00` (reported as session
`bbc4efd8-6bdd-4d6c-aa16-86bae2cecc00`; the operator's own transcript is the
attachment `chat_exit_bug.txt`). Symptom, verbatim: *"this chat exit without
notice"* — the transcript stopped mid-work with the input prompt on screen and
the shell prompt back. No error, no `bye.`, no next `you>`.

## Root cause (from the session's own logs, not from reasoning)

`~/.gopi/sessions/<sid/session>.jsonl` for that session shows:

- **39 tool rounds** in ONE turn, ~**1.55M** cumulative prompt tokens, **288 s**,
  and then `session_end` with `exit_code: 1` and `crash_dump` `category:
  unknown` — the session's own record of a DEATH, not of a user's `/quit`.
- The turn ended because it hit the **bounded ReAct cap**:
  `maxChatOpsToolRounds = 40` (`internal/agent/chat_ops.go`), i.e.
  `chat turn exceeded 40 tool rounds without a final answer`.

The exit was `cmd/agent/cli_chat.go`'s turn-error branch:

```go
if runErr != nil {
    fmt.Fprintf(os.Stderr, "\n[gopi] error: %v\n", runErr)
    return finishChat(ag, sess, sessionStart, iterCount, totalTokens, 1)
}
```

That was the **only** `return` in the turn loop on a model error. Every other
exit there is deliberate: an explicit `/quit`/`/exit`, Ctrl+D, a signal, a real
read failure. So one unlucky turn — and the cap is trip-able by the model alone,
by keeping its ReAct loop open — killed the whole process.

Two aggravators made it read as "without notice":

1. **Nothing warned the model.** The cap was invisible until it fired; the model
   had no instruction to wrap up, so a turn that *could* have landed a partial
   answer died with nothing.
2. **The error text never said the session survived.** It named the failure and
   then the process was gone, so the operator could not tell a dead REPL from a
   live one — and the message was written with a bare `Fprintf` to stderr, which
   with the reserved band installed lands *under* the band and is erased by the
   editor's repaint (the same defect the other notices in this file were routed
   for).

## The fix

`cmd/agent/cli_chat.go` — the error branch no longer returns:

- the failure is written through the layout's OWN writer (`input.WriteNotice`),
  so it is visible above the band instead of erased under it;
- the agent's `OnText`/`OnThinking` seams are dropped and restored
  (`prevThinking`, captured at turn setup), so a renderer that belonged to the
  finished turn can never receive the next turn's text;
- counters advance and the status row is republished, then `continue` — the
  prompt comes back and the session stays open.

`internal/agent/chat_ops.go`:

- `chatOpsRoundsWarnAt = maxChatOpsToolRounds - chatOpsRoundsWarnLead` (40 − 5):
  the last 5 rounds inject a harness message (`chatOpsRoundsWarnMessage`, marker
  `"Round budget almost spent"`) telling the model to stop calling tools and
  answer with what it has — converting "the turn dies" into "the turn lands a
  partial answer";
- the cap error now names the cap size, distinguishes an interrupt/timeout from
  the model looping (`chatOpsCapNote`), and ends with `chatOpsCapAdvice`:
  *"— the session is still open; rephrase, /clear to reset, or /quit to exit"*.

## Verification (red → green on the real surface)

Both directions were driven with **two real binaries** on a **real PTY**. They
live OFF the source tree, under this run's scratch home
(`/home/user/workspace/gopi-test-scratch/queue/<run>/gopi-before` /
`gopi-after`, Rule 22 — Rule 30 forbids them in the worktree, and the queue
directory is the harness-supplied home for exactly this kind of artifact):
`gopi-before` built from master before the change, `gopi-after` from the branch
(rebuilt at hand-over, same commit as the branch tip).

| test | before | after |
|---|---|---|
| `TestChatPTY_FailedTurnKeepsThePrompt` (PTY, `--tools`, 40-round looping model) | FAIL — screen ends `[gopi] error: chat turn exceeded 40 tool rounds…`, PTY closes | **ok** |
| `TestChat_RoundCap_FailsOneTurnButKeepsTheSession` (real binary, piped) | FAIL | ok |
| `TestChat_ErrorScenario_TurnFailure` (400 on the first turn) | FAIL (it *pinned* the old exit-1 behaviour) | ok |

The PTY test asserts on the SCREEN: the failure is named (with the cap size) and
`you>` is re-rendered, the next turn is accepted and answered, and both turns sit
in ONE session whose `session_end` exit code is 0 (of the deliberate `/quit`).

`testsuite/chat_pty_test.go` also carried a pre-existing red unrelated to this
change: `TestChatPTY_EditorRendersAndSubmits` expected `"line editor active"`,
which `cli_chat.go` only prints under `--verbose` — the test started `chat`
without it. Measured identical on the BEFORE binary, so it was not a regression
here. Repaired in this branch rather than filed: the test now starts
`chat --verbose` (matching the gate `cmd/agent/cli_lineedit_test.go` already
pins for the non-TTY side) and asserts the flag-free half of the same claim —
`\x1b[?2004h`, the bracketed-paste announcement the raw-mode editor emits with or
without `--verbose`. All four PTY tests are green.
