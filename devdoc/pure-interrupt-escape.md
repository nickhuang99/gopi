<!-- doc-class: historical -->
# devdoc/pure-interrupt-escape.md — Esc is the PURE INTERRUPT: cancel the turn, keep the line, keep the session

**Task:** 01a0e84b-5bd2-7eba-8996-b22078ef76aa
**Branch:** `task/01a0e84b5bd27eba8996b22078ef76aa`
**Class:** historical (this run's own record: what changed, what its gates measured)

## What the epic asked for, and what was already there

The epic (01a0e84b) asked for gopi's interrupt mechanism: Esc / Ctrl-C as a **pure
interrupt** — cancel the current LLM turn, carry no new input, keep the input line,
recover the state immediately, no cooldown, idempotent — plus `/queue <text>` that
parks a prompt during a turn without interrupting, is visible, and does not
reproduce Hermes's duplicate-bubble / leaked-notice race.

**Two thirds of it had already landed on master before this run** (found by the
pre-implementation survey, and it is the fact that re-scoped the work):

| piece | commit | state at this run's start |
|---|---|---|
| Ctrl-C per-turn interrupt (`turn_interrupted` witness, single-claim latch, `Result.Interrupted` split) | `23fc6d6a` | in master |
| `/queue <prompt>` (FIFO, bound, list, clear, reader-goroutine park) | `ff76758c` | in master |

The remaining gap was therefore exact: **Esc**, and the end-to-end evidence for
the acceptance criteria. That is what this branch adds.

## The defect this closes

A raw-mode terminal clears `ISIG`, so Ctrl+C arrives as the **0x03 byte** and is
classified by the editor's key decoder — it is never a signal on that path. The
seam was installed for that one byte, and it worked. A lone **Escape** had only
its historic meaning: *close the completion menu; otherwise inert*. So while an
answer was streaming, the operator's two obvious keys did nothing useful —
Ctrl+C cancelled the turn (and cleared the line), Escape did nothing at all.

## The design decision that the feature turns on: the two keys differ in ONE way

This is the criterion the whole change is organised around, and it is deliberately
**not** symmetry:

| key, during a turn | effect on the turn | effect on the LINE |
|---|---|---|
| **Ctrl+C** | claimed, turn cancelled | **buffer DROPPED** — the abandoned turn's half-typed line must not become the next message |
| **Escape** | claimed, turn cancelled | **buffer KEPT** — the "pure interrupt": cancel, carry no input, leave the draft exactly where it was |

Both keep the session open and both are idempotent. The line is the difference.

## What changed, file by file

- **`internal/lineedit/turnkey.go`** (new). `KeyOrigin` — *which input carried the
  key*: `OriginCtrlC` (the 0x03 byte), `OriginEscape` (a lone ESC, reported only
  after the decoder's bounded ESC wait resolves it), `OriginSignal` (a real
  SIGINT, i.e. the same key on a cooked transport). `KeyOriginSet` + `Valid()` —
  a **closed** set, so the vocabulary cannot silently grow a member nobody maps.
- **`internal/lineedit/editor.go`**. The seam's action now takes the origin
  (`func(KeyOrigin) bool`) and the Escape arm **claims it while a turn is in
  flight** before falling through to `MenuClose()`. The order is the whole
  difference between the two meanings, and the fall-through (a claim that comes
  back `false` because the turn ended in the same instant) keeps Rule 15: the key
  is never swallowed.
- **`internal/agent/loop.go`**. `InterruptReasonEscape` alongside
  `InterruptReasonCtrlC` (+ `InterruptReasonSet` / `InterruptReasonKnown`);
  `InterruptRunBy(reason)` records the origin **under the same lock as the latch**;
  `TurnInterruptReason()` reads it back; the witness writer uses it.
- **`cmd/agent/cli_chat.go`**. `chatInterruptReason(origin)` is the ONE place the
  two vocabularies meet; `chatInterruptMarkerFor(reason, n)` puts the reason in
  the operator's line; `claimTurnInterrupt` / `endTurn` / `beginTurn` carry the
  origin through the turn window.
- **`cmd/agent/lineedit_wire.go`**. The SIGINT half states `OriginSignal`.

### Two deliberate non-decisions

1. **Ctrl+C's two transports collapse to ONE reason.** A raw 0x03 byte and a real
   SIGINT are *the same key*; a reader grouping ctrl-c interruptions must not lose
   half of them to a transport detail. The transport is honest in `lineedit`'s own
   vocabulary and mapped at the one meeting point. `InterruptReasonSet` therefore
   has two members, not three — and a test asserts that.
2. **The origin travels into the agent rather than being re-derived by the
   carrier.** Only the cancel knows which key claimed it when the two transports
   race, and a carrier that did not see the key (the web route, a signal-driven
   turn) has nothing to derive from.

## What the gates measured

Unit (`internal/lineedit`, `cmd/agent/`):

- idle Escape is **inert** — it does not exit, does not edit the buffer, and does
  not even *ask* the caller to claim it (the predicate is consulted first);
- a claimed Escape reports its origin and **keeps the line**; a claimed Ctrl+C
  reports its origin and **drops** it;
- an **unclaimed** key (either one) falls back to its historic meaning rather than
  vanishing;
- with **no seam installed** (`nil` action) both keys keep their pre-feature
  meanings byte for byte — this is what makes the change an addition rather than a
  change of contract;
- **exhaustiveness**: every member of `lineedit.KeyOriginSet` maps to a member of
  `agent.InterruptReasonSet`, and the marker names the reason it was built with;
- **the NAME path** (`TestChatInject_EscapeByNameReachesTheTurnSeam`): the tool's
  advertised spelling `escape` is read from `lineedit.InjectableKeyNames()`,
  injected through the production injector on a real pty, and lands on the turn
  seam once, as `OriginEscape`, with the draft still on the line. **Measured
  4/4 runs with zero case failures**; the package verdict flakes independently of
  it (see the pre-existing reds below).

  ```
  Ran:    go test ./cmd/agent/ -run 'TestChatInject_EscapeByNameReachesTheTurnSeam' -count=1 -timeout 300s
  Result: 4/4 runs, 0 `--- FAIL: TestChatInject_Escape` lines
  Ran:    go test ./internal/lineedit/ -count=1 -timeout 900s
  Result: ok  github.com/nick/gopi/internal/lineedit  24.047s   (EXIT=0)
  ```

Independent of this change, the package `cmd/agent` carries **pre-existing** reds
and a fixture flake, both measured on the pre-fix base so neither is attributed to
this feature:

| observation | this branch | pre-fix base `d1209fe0` |
|---|---|---|
| `internal/agent` failing tests, by name | 7 | **the same 7** |
| `cmd/agent` `parked repository goroutines` sweep (unmodified pair of cases, 3 runs) | 2/3 red | — (fixture's own reader goroutine) |
| `internal/standards` red gates (`TestStandardGates`, `TestStandardGateDataReport`, `TestGate7WorktreeRootLiteralRealTreeIsClean`) | 3 red | **the same 3**, same hits (`internal/shot/x11.go:193`, `internal/memoryprefetch/standalone_test.go:110`) |

```
Ran:    git merge-base HEAD master → d1209fe07536f37fe4e49ac84c9def2b336fc591
        git archive $BASE | tar -x -C /home/user/workspace/_tmp/prefix   (Rule 29: a throwaway tree, no worktree touched)
        cd /home/user/workspace/_tmp/prefix && go test ./internal/agent/ -run '<the 7 names>' -count=1
Result: 7/7 FAIL, name for name identical on both trees (EXIT=1)

The 7 `internal/agent` names are `TestChatPromptCarriesChatContextBlock`,
`TestDocAnchorsResolveInNormativeDocs`, `TestAssembleSystemPromptOrder`,
`TestAssembleSystemPromptEmptySectionDropped`, `TestAssembleSystemPromptStableOrder`,
`TestSystemPromptDensity`, `TestUnlimitedBudgetRendersNoCeiling` — re-run against
a `git archive` of the merge base and reproduced name for name. **This commit
touches no prompt/context file**, which is the same fact from the other side.

The `internal/standards` table row was measured the same way — `go test
./internal/standards/ -run TestGate7WorktreeRootLiteralRealTreeIsClean` inside the
same `git archive` of `d1209fe0`, byte-identical output — and neither file the
gate names (`internal/shot/x11.go`, `internal/memoryprefetch/standalone_test.go`)
appears in this commit's `--name-only`.

A run of the whole `cmd/agent` package produced 19 failures and a 10-minute
timeout **while the testsuite pty suite was running concurrently** — several
(`TestLedgerOpenersUseProductionDSNContract`, the `TestUIState*` family, the merge
residue cases) are visibly environmental. That run is recorded as INVALID rather
than as evidence: the honest reading is the serial re-run, whose pre-existing
failures are the table above.

End to end, on a real pty and the real binary (`testsuite`):

- **Escape during a stream** → the marker reads `turn interrupted by escape
  (operator, N chars shown)`, the answer's sentinel never arrives, the partial
  answer is on screen, the draft typed *during* the turn is still the buffer (the
  **second request's body** carries it — the wire, not the screen, is the ground
  truth), the JSONL has `turn_interrupted` with `reason=escape` and a positive
  `partial_chars`, and `session_end` appears exactly once, exit 0, from `/quit`.
- **`/queue` during a held turn** → no interrupt marker anywhere and the parked
  prompt appears in exactly **one** user bubble (the Hermes duplicate is absent).

### The red proof (the pre-fix tree, the same test file)

`TestChatPTY_EscapeDuringATurnIsAPureInterruptAndKeepsTheLine` was run against a
binary built from the **committed tip** (`git archive HEAD` into a throwaway tree
under `/home/user/workspace/`, so no other worktree's state was touched — Rule 29
/ 23):

```
--- FAIL: TestChatPTY_EscapeDuringATurnIsAPureInterruptAndKeepsTheLine (74.15s)
    chat_pty_escape_interrupt_test.go:187: timed out waiting for "[gopi] turn interrupted by "
```

Pre-fix: Escape was inert, the answer streamed to its sentinel, no marker ever
came — **red**. Post-fix: the same file passes — **green**. The test is therefore
about this change, not about the tree it happens to run in.

## A trap the fixture taught (worth reusing)

The first version of the pty case **failed for a fixture reason, not a product
one**: the draft under test is *typed* (`writeGapped`, one byte per 300 ms, so the
editor reads keystrokes rather than a paste), which costs ~1.8 s for six
characters — while the stream lasted only 1.1 s. The turn was already over when
the Escape arrived. The window is now 120 fragments × 60 ms (7.2 s) and the draft
is six characters. **The same lesson one layer up**: the ESC byte must be followed
by nothing, because that is how the decoder resolves it as the key — and a
`waitFor` on a partially written line returns the *prefix*; the test reads the
rest of the line before asserting on the reason.

## Not done (honest)

- `chat_surface action=key escape` **CLOSED** — the follow-up the first version of
  this document named is now a case:
  `TestChatInject_EscapeByNameReachesTheTurnSeam` (`cmd/agent/chat_inject_pty_test.go`).
  It injects the NAME (`lineedit.KeyNameEscape`, read from the injectable table
  rather than typed, so a renamed row is caught), asserts the turn seam is called
  **once** with `OriginEscape`, that `chatInterruptReason` maps that origin to
  `agent.InterruptReasonEscape` (the value the marker and the JSONL are built
  from), and that the draft on the line **survives** — the pure half. What it does
  NOT do is drive a live model round through the tool; the pty acceptance case
  covers that path with the raw `0x1b` byte, so the two rungs are "the NAME
  resolves and reaches the seam" (here) and "the key works end to end" (there).
  The gap left open is honest and small: no single case does both at once.
- The SIGINT (`OriginSignal`) leg is covered at the **mapping** level
  (unit) and in the pre-existing `23fc6d6a` pty case, not by a new pty case of its
  own.
- A cooldown/timer knob was deliberately **not** added (the epic bans the Hermes
  shape); idempotence is structural — the single-claim latch plus "no run in
  flight ⇒ no claim" — rather than time-based.
