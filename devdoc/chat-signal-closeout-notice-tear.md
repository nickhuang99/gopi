<!-- doc-class: historical -->
# The chat signal-closeout notice: a second writer on the layout's descriptor

Task `01a0eb0b-872d-7252-a0b7-9bdfd5798be9`. Fix commit `fd520852`.
Full report: `~/.gopi/sessions/01a0eb0b-872d-7252-a0b7-9bdfd5798be9/task-report.md`.

## The one-line version

While the reserved layout is installed, the cursor is parked on the status row
and `os.Stderr` **is** the editor's paint descriptor. Any `fmt.Fprintf(os.Stderr, …)`
on the chat path therefore writes ON a chrome row and its newline **scrolls the
screen**, desynchronising the terminal from the layout's own row indices.

## Why it is worth a devdoc

Two reasons, and the second is the reusable one.

**It is the same defect twice.** `cmd/agent/chat_flow_seam.go` already carries
this exact reasoning, in prose, about this exact class:

> It rides the layout's own writer (WriteNotice), never a bare Fprintf: with the
> reserved band installed a flowed stderr write lands UNDER the band and the next
> repaint erases it — the very defect this marker exists to avoid, one layer down.

And `lineedit.Editor.WriteLines`' doc comment names the general rule:

> with the layout installed the terminal is inside the scrolling region with the
> cursor parked on the editing line, so a flowed write lands on the editing row
> and every LF scrolls the band onto the caller's previous line

Both statements were in the tree. The signal handler still did the wrong thing —
because **the rule lived in prose at two call sites and had no gate**, and the
third call site simply did not read them.

**The instrument was wrong for the symptom.** Every existing test of this class
drives the *flow seam* (`ag.OnThinking`, the answer renderer, the logger
re-point). The signal closeout is a different entry point, so `TestChatFlow_*`
stayed green while the screen tore. And `--dump-screen` **cannot see this defect
at all**: it observes the paint stream, while a bare stderr write never enters
it — so the artifact the operator would naturally attach is blind to the very
byte that did the damage.

## The fix

`cmd/agent/cli_chat.go`:

```go
chatSignalCloseoutNotice(input, sig)   // → input.WriteNotice(fmt.Sprintf(chatSignalCloseoutNoticeFormat, sig))
```

`WriteNotice` hands the row to the editor — the layout's only writer — and falls
back to the historic bytes when no layout is installed, so a cooked run,
`--no-editor`, a pipe and the `testsuite` greps all read the same stream as
before. A nil `input` (the signal can arrive before the input layer exists) still
prints to stderr rather than dropping the notice.

## The recipe (reusable)

To reproduce a "two writers, one descriptor" tear you need **both** halves of the
topology, and the assertion must be on the **rendered grid** (Rule 10.2):

1. Build the harness so the paint writer and `errOut` are the **same object** —
   that is production (`newChatInputWithPalette` passes `os.Stderr` as both), and
   a harness that splits them exercises a seam no run has.
2. Because that object is often `*os.File`, use a **pipe** plus a copy goroutine
   into the recorder the grid is replayed from; a `recordingWriter` cannot be
   assigned to `os.Stderr`.
3. Assert on the grid, not on "the bytes were written": with the defect in place
   the notice IS written, and only the grid shows the damage. The failure mode to
   state is the pair — *no chrome row carries the notice* **and** *the chrome is
   still where the layout's indices say* (a bare write breaks both: it lands on a
   row and it scrolls).
4. Wait on a marker that a **torn** write still leaves (the sentence's head, not
   the whole sentence), or the tear is reported as a timeout — the wrong
   property, and a reader chases an unrelated bug.

Pre-fix failure text (verbatim):

```
the chrome is no longer where the layout says it is: the layout plans the status
row at 25, but that row reads "" (want the model "deepseek-flash").
```

## The other half: "crash" was a SIGTERM

The live session's `signal-provenance.jsonl` records `SIGTERM`, `sourced: false`,
`parent_name: bash`; the raw log ends on an ordinary `message_delta`/`end_turn`
(`output_tokens: 1981`), and the 37 MB log contains no `panic:` and no
`goroutine N [`. A `crash_dump` with `category: "unknown"` and `exit_code: 1` is
the closeout's verdict about a run that died *under a signal*, not evidence of a
fault inside gopi. Worth recording because the two arrive bundled: the operator
sees "it crashed", and the honest answer splits into "you (or your shell)
signalled it" and "and here is the screen defect that made it look like a crash".

## Where the next such call site will be

Any new chat-path writer that reaches `os.Stderr` (or any destination the editor
paints on) directly. Rule 3 keeps new tools out, so the risk is new *notices* on
existing faces. The guard that would have caught this one is a test that walks
the chat path's `os.Stderr` Fprintf sites and asserts each is either
layout-routed or carries a reason — no such gate exists yet; this doc names it as
the gap rather than claiming coverage.
