<!-- doc-class: normative -->
# A Plain Prompt's First User Message Is the Prompt Alone — the Self-Record Contract Is Scoped to Task Runs

**Status:** normative — this is the standing statement of where the harness's
self-recording contract may appear, and of the wire behaviour that made it
appear in the wrong place.

## The red

`cmd/agent` arm A failed deterministically on clean master (`14128dc9`):

```
--- FAIL: TestRunPathPlainPromptIsVerbatim (8.35s)
    cli_runpath_boundary_rp1_test.go:246: the FIRST user message is not the prompt verbatim:
         got (883 chars): "RP1-BOUNDARY-PLAIN-PROMPT-4f2a[system] SELF-RECORDING: end EVERY response …"
        want (30 chars): "RP1-BOUNDARY-PLAIN-PROMPT-4f2a"
```

The assertion is at **`cmd/agent/cli_runpath_boundary_rp1_test.go:246`** — the
request boundary, where the payload that actually left the process is read back
by a local recording endpoint. Nothing about the run was load-sensitive: it
reproduced at `-count=1` and at low load average (1-min ≈ 2.25 at the control
run), and 20/20 at every count in between.

## Root cause

RP-1's fix (2026-09-27, `ee8aff27`) confined the **EPIC frame** to task surfaces,
in `internal/agent/loop.go`:

```go
epicMessage := userInput
if !a.plainPrompt {
    epicMessage = fmt.Sprintf(epicGoalFramingFormat, userInput, emissionContract, reframeDirective)
}
```

So on the shell path (`Agent.RunPrompt`, `plainPrompt == true`) the *frame
builder* was already correct — which is exactly why the defect looked like
nothing when this file was read.

Three days later, self-recording 第一期 (`4f25e11e`, 2026-10-01) added a second,
independent user message to the same turn. `startOfRunFor`
(`internal/agent/hippocampus.go:883`) called `maybeInjectSelfRecordRule()`
**unconditionally**, and that function does:

```go
a.Ctx.AddUserMessage(selfRecordRule)   // "[system] SELF-RECORDING: end EVERY response …"
```

**The API requires alternating roles, so the client does not send two
consecutive user messages — it merges them.** `internal/deepseek/claude_client.go:560`:

```go
// Merge consecutive user text messages — the API requires
// alternating roles and accepts extra text blocks in a
// tool-result user message (verified live).
if m.Image == nil {
    if n := len(req.Messages); n > 0 && req.Messages[n-1].Role == "user" {
        req.Messages[n-1].Content = append(req.Messages[n-1].Content, userBlocks...)
        continue
    }
}
```

So `selfRecordRule` never arrived as a second message. It became **text block #2
of the prompt's own first user message**. The prompt survived — as the first 30
of 883 characters — and the sentence `gopi -h` publishes (`"<prompt>"  The task
text`) quietly stopped being true at the request boundary.

That is the whole mechanism: **a correctly-scoped first turn plus a later,
unscoped injector equals an unscoped first turn.** The frame checker in `internal/agent/loop.go`
cannot see it; the injector does not know the run is a plain prompt; only the
wire shows the merge.

## The fix

One predicate, already defined, threaded to the injector — no second notion of
"this run is a task" (Rule 14):

- `internal/agent/hippocampus.go` — `startOfRunFor` now reads
  `a.maybeInjectSelfRecordRule(a.plainPrompt)`, with the wire-coalescing reason
  named at the call site.
- `internal/agent/selfrecord.go` — `maybeInjectSelfRecordRule(plainPrompt bool)`
  returns early on a plain prompt.

`a.taskChildRowID == ""` **is** the plain-prompt run (set in `Agent.RunPrompt`),
the same value that gates the EPIC framing, so the two decisions cannot drift:
there is one definition of "this run is a task", read in both places.

The feature is untouched everywhere it applies: a `task run` child, and every
in-process task caller keep the contract. *(The third face this sentence used to
name — the merge run, then `merge_run.go` — went with the top-level merge verb on
2026-11-01; the file is not in this tree.)*

## The regression test

`internal/agent/plainprompt_firstmessage_test.go` — in process, no child
process, no profile, no ledger:

- **arm A** `TestPlainPrompt_FirstUserMessageIsThePromptAlone` — red before the
  fix (3 text blocks, 2001 chars, `selfRecordRule` present), green after.
- **arm B** `TestPlainPrompt_TaskChildStillGetsTheSelfRecordContract` — the
  counterweight: a fix that simply stops injecting would pass arm A while
  deleting the feature, and this arm refuses that.

Both assert the injected text against the **same constants** the injector uses
(`selfRecordRule`), never a retyped phrase.

It is the fast instrument; `cmd/agent`'s out-of-process probe remains the
acceptance instrument (it spawns the real binary, see Rule 31's boundary).

## Verified at the real boundary (Rule 31)

The changed surface is the CLI run path, so it was run — the real binary, real
arguments, a local recording endpoint — in two shapes:

| shape | user messages | first user message text blocks | first message |
|---|---|---|---|
| plain prompt (`GOPI_NO_ENFORCE_MEMORY=1`) | 1 | 1 | `RULE31-PLAIN-PROMPT-c7d9` (24 chars) |
| `task run` child (`GOPI_TASK_WRAPPER_ID`) | 1 | 2 | `[EPIC GOAL] …` + `[system] SELF-RECORDING …` |

Evidence: `rule31-boundary-requests.jsonl` in this run's session directory.
(The checkpoints are off in the plain case only because a second reviewed
mechanism — the TASK-START memory checkpoint — also appends to that turn; that
behaviour is documented on the help page and is not this defect.)

## What to take from it

**An injected message is not a message until the wire says it is.** This repo
injects harness text from several sources (`maybeInjectTaskStartSearch`,
`maybeInjectSelfRecordRule`, the consult checkpoint, hippocampus round-boundary
injections). Every one of them lands in the *same* first user turn, and the
Claude client merges consecutive user text blocks by design. So when a boundary
promises "the text, as typed", the promise is kept or broken by the SUM of the
injectors — not by the frame builder that has the `if` in it.

Check for this class by reading the first user message's **text blocks**, at the
boundary, not by counting `Context.Messages` before the send.
