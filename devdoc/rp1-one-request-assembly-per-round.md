<!-- doc-class: normative -->
# RP-1: one request assembly per round — the invariant that makes a first-request exemption reachable

**The rule this file states** (it is what a future editor of the task loop must
obey, which is why this page is `normative` and not a post-mortem):

> **A round assembles its request ONCE.** The request that is validated and
> printed is the request that is sent. A gate that may need a DIFFERENT request
> (the token cap, after it actually shrinks the context) gets a builder — never a
> second eager call.

The defect this closes is `RP-1`: on a plain prompt (`gopi "<prompt>"`) the
operator's text reached the model with the turn stamp glued INSIDE it —

```
got (154/158 chars): "<PROMPT>[system] [t+0s] @ 2026-11-07T06:19:26+08:00 — +0s since your previous request. session budget: 0s elapsed of 1h0m0s (0% used)."
want                : "<PROMPT>"
```

— which `gopi -h` forbids in one line: `"<prompt>"  The task text`.

## The mechanism, in the order the bytes are produced

1. `internal/agent/clock.go:448` `requestMessages` appends the current turn stamp
   as a LAST user message. On the wire that is not a separate block:
   `internal/deepseek/claude_client.go` `BuildClaudeRequest` MERGES consecutive
   user text (the Messages API requires alternating roles), so "appended" means
   "glued inside the operator's own text". That is the whole reason an
   exemption has to exist.
2. `internal/agent/clock.go:410` `turnStampForRequest` withholds the TEXT on a
   plain prompt's FIRST request — but it calls `Clock.TurnStamp()`. That call is
   a **mutating read**: `internal/agent/clock.go:215` sets
   `c.lastRequest = now` on every invocation, so the stamp it answers with is
   also the mint that turns the NEXT assembly into the session's second request.
3. Therefore **assembling after assembling is not free**. A build nobody sends
   spends the exemption. The pre-fix loop
   (`internal/agent/loop.go:4904` `req := buildRoundRequest()` for the
   pairing-invariant proof and the debug print, then the builder handed to
   `chatOnceGated`) did exactly that, and
   `internal/agent/request_tokens_cap.go:303` `req := build()` rendered it a
   second time inside the gate. The stamp that went out was the GAP arm —
   `+0s since your previous request` — which names the defect precisely: the
   exemption was **correct and unreachable**, not stale and not bypassed.

## The fix, and why it is not "make the predicate non-mutating"

`internal/agent/loop.go:4951`:

```go
builtForSend := false
buildOnceThenRebuild := func() *deepseek.ClaudeRequest {
    if !builtForSend {
        builtForSend = true
        return req
    }
    return buildRoundRequest()
}
res, err := a.chatOnceGated(ctx, buildOnceThenRebuild, i)
```

The gate keeps the re-render it genuinely needs — a request built BEFORE
compaction describes a context that no longer exists — so the second call still
happens, but only on the path where the context actually shrank. The normal
path assembles once and mints once.

**Rejected alternative, recorded so it is not re-proposed**: turning
`TurnStamp()` into an observational getter and minting elsewhere. It would make
the stamp understate the gap (the mint is what anchors
`SinceLast`), and it leaves "assembled twice" in place for the next
mint-on-read accessor added to the same path. The invariant, not the getter, was
the defect.

## Why the suite could not see it — the reusable half

`internal/agent/plainprompt_firstmessage_test.go`'s earlier arms build their
agent with `ppAgent` (`:154`), which **leaves `Clock` nil**. With a nil clock
`turnStampForRequest` returns `""` before it ever consults `.First`
(`internal/agent/clock.go:411`), so those arms assemble a stamp-free request no
matter how many times they assemble — they measure the exemption's text, never
its reachability. The defect could only ever appear out of process, where a real
run carries a real `NewSessionClock()`.

That is the lesson worth carrying: **a fixture that leaves the subject's clock
nil is not a weaker test of that subject, it is a test of a different one.**
Arm D (`:311`) is the minimal correction — one line, `ag.Clock = NewSessionClock()`
— and it fails on the pre-fix loop while arms A–C pass. A first-request
exemption can only be tested by a run that HAS a clock.

## Red proof on the fixed ground (Rule 6 / Rule 31), and the restore criterion

Mutated the one call site back to the pre-fix shape
(`chatOnceGated(ctx, buildRoundRequest, i)`), then:

```
Ran:  go test ./internal/agent/ -run 'TestPlainPrompt_LiveClockKeepsTheFirstMessageVerbatim' -count=1 -v
Ran:  go test ./internal/agent/ -run 'TestPlainPrompt_FirstRequestCarriesNoTurnStamp|TestPlainPrompt_TaskSurfaceKeepsOneStampPerRequest' -count=1 -v
Ran:  go test ./cmd/agent/ -run 'TestRunPathPlainPromptIsVerbatim' -count=1
```

```
--- FAIL: TestPlainPrompt_LiveClockKeepsTheFirstMessageVerbatim (0.10s)
    plainprompt_firstmessage_test.go:323: the first user message carries 2 text blocks, want 1
        ["PP-FIRSTMESSAGE-PLAIN-9c31" "[system] [t+0s] @ 2026-11-07T06:18:58+08:00 — +0s since your previous request. session budget: 0s elapsed of 1h0m0s (0% used)."]
    plainprompt_firstmessage_test.go:328: the first user message is not the prompt verbatim with a live clock:
         got (154 chars): "PP-FIRSTMESSAGE-PLAIN-9c31[system] [t+0s] @ 2026-11-07T06:18:58+08:00 — +0s since your previous request. session budget: 0s elapsed of 1h0m0s (0% used)."
        want (26 chars): "PP-FIRSTMESSAGE-PLAIN-9c31"
FAIL	github.com/the operator/gopi/internal/agent	4.573s

=== RUN   TestPlainPrompt_FirstRequestCarriesNoTurnStamp
--- PASS: TestPlainPrompt_FirstRequestCarriesNoTurnStamp (0.00s)      ← the nil-clock arm cannot see it
=== RUN   TestPlainPrompt_TaskSurfaceKeepsOneStampPerRequest
--- PASS: TestPlainPrompt_TaskSurfaceKeepsOneStampPerRequest (0.00s)
ok  	github.com/the operator/gopi/internal/agent	4.028s

    cli_runpath_boundary_rp1_test.go:298: the FIRST user message is not the prompt verbatim:
         got (158 chars): "RP1-BOUNDARY-PLAIN-PROMPT-4f2a[system] [t+0s] @ 2026-11-07T06:19:26+08:00 — +0s since your previous request. session budget: 0s elapsed of 1h0m0s (0% used)."
        want (30 chars): "RP1-BOUNDARY-PLAIN-PROMPT-4f2a"
FAIL	github.com/the operator/gopi/cmd/agent	7.969s
```

The three lines that matter together: arm D and the out-of-process boundary arm
go red, and the nil-clock in-process arms stay green **on the same broken tree**.
That is the measurement behind the paragraph above, not an argument for it.

**Restore is proven by blob identity, never by an index-relative diff** (a
restore seeded from the index writes back the bytes the index holds):

```
Ran:  test "$(git hash-object internal/agent/loop.go)" = "$(git rev-parse HEAD:internal/agent/loop.go)"
Result: 726c4aaa0eaac928a4f310611b12aa77ae5272bf == 726c4aaa0eaac928a4f310611b12aa77ae5272bf → RESTORE VERIFIED
Ran:  git status --porcelain -uall | wc -l
Result: 0
```

The raw streams are in this run's session directory under `artifacts/`
(`rp1-redproof-inprocess.out`, `rp1-redproof-boundary.out`).

## Where this fix came from, stated plainly

The commit is `f96e2b05` (`fix(agent): RP-1 — one request assembly per round,
so the first-request stamp exemption survives`, task
`01a1b2ce-517c-7746-89f2-c4bf9e858289`) — a **sibling row**, not this page's
author. It landed with **no devdoc** of its own (`git show --name-only f96e2b05`
lists exactly two files, both Go), which is the gap this page fills: the
reasoning above lived only in `loop.go`'s own comments, where the next editor of
the loop will meet it, and nowhere in the corpus.

`devdoc/plain-prompt-first-request-stamp.md` is the sibling record of the
exemption itself; read the two together. One line of that page is now stale —
"the ground has NO `plainprompt_firstmessage_test.go`" — and it is **left
standing on purpose**: it was true of the ground it describes, and Rule 36 makes
a `historical` page's job to say what was typed then rather than to be
retro-edited. The file exists now, and arm D lives in it.

## Not Done

- **No code change on this page's own account.** The loop already carries the
  fix; re-applying it would be a duplicate of `f96e2b05` racing the same file.
  This run's deliverable is the record, plus the red proof above which is
  re-runnable against the fixed tree.
- **The other `chatOnceGated` caller (`internal/agent/loop.go:5560`, the chat
  surface) was not red-proved.** It passes `buildChatRequest`, a builder whose
  own call count per round was not measured here; the plain-prompt surface is
  the one RP-1 named. An editor who changes that path should run the same recipe
  rather than assume this page covers it.
