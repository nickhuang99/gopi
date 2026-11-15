<!-- doc-class: normative -->
# The Compaction Anchor Trigger: a Harness Follow-up Must Not Be the Instruction Floor

Follow-up to `devdoc/compact-livelock-fix.md`. That fix (commit 903c0d5c, merged
8a9f9a99) made an anchor-jammed window **converge** (windowIsMemoryBlocks →
structural no-op → bounded trim) and left one item in its "Not Done (honest)"
section: **the trigger** — *why* the window is anchored right behind the head.

This note is that trigger's measurement, the fix, and the proof.

## 1. Measurement — what the pinned floor actually is

Field session `c2eff914-c37c-48aa-9fd9-7eea831c6f37` (agent session dir
`~/.gopi/sessions/01a0a93c-…`, 1762 JSONL rows, 157 MB raw log). Counts over
its live log:

| evidence | count |
|---|---|
| `anchored at last user` | ~230 in the last 6 MB of a 157 MB log (5034+3130+~300 in the prior full count) |
| `summarized 1 oldest message(s)` | 7490 |
| `summarized 2 oldest message(s)` | 773 |
| `anchored at memory block floor` | 106 |
| the fixed pair `before=157995 after=118285` | 15× verbatim |

The last row matters: the log carries **both** the pre-fix cycle and the
post-fix behaviour, which is why the merged livelock fix is visible in it
("compaction skipped … freeing %.1f%% (< %.0f%%)", 8 hits). What it does not
remove is the trigger.

**Which message is the floor.** The 13 user-role rows of the session are:

```
idx 1   [EPIC GOAL] …                                  ← the operator, ONCE
idx 2   [system] TASK-START CHECKPOINT: …
idx 3   [system] Hot-tier resident candidates …
idx 480/723/850/1122/1581/1612/1696/1732/1753
        [WRITE_TASK_AREA — COMMITTED CONCLUSIONS (authoritative)]
idx 481 [MEMORY-CONSULT CHECKPOINT]
```

There is **no operator follow-up anywhere in the session** — every user-role
message after the epic is framework output. So every "last user" floor that
session ever had was a harness message.

**Where the injections sit.** Walks over all 397 request dumps in the raw log
(478 user-role messages carry text; the remaining user frames are tool results):
the only two texts that ever appear are the epic and harness messages, and in
the dumps that carry a checkpoint it sits a handful of messages from the **head**
(a start checkpoint lands within the first few messages of a run, a hot-tier
digest right after it) — never at the tail:

```
dump#30 len=57  idx=0 epic | idx=56 "[system] You ended your turn without calling any tools. "
dump#80 len=155 idx=0 epic | … the checkpoint has already fallen behind dozens of messages
```

That is the mechanism: a framework injection is appended **early**, then the
run's own work piles up behind it. `TakeOldest` scans backwards and stops at the
newest user message — the injection near the head — so the pass can take only
the sliver in front of it, while everything after it (the session's whole body of
assistant/tool work) is unreachable. The context stays above the limit and the
next round repeats the same sliver: `anchored at last user` + `summarized 1
oldest message(s)`, precisely the field pair.

A second, independent line of evidence for Rule 1b: **the framework never had a
name for its own marker.** `grep '\[system\]' internal/agent/*.go` found the
literal inlined at 18 append sites, with no constant — the same string that the
operator-facing log renders and that tests assert on.

## 2. Decision — which of (a)/(b)/(c)

**Chosen: (a), with (c) as the safety net.** (b) was rejected because "keep the
floor but make the reachable window compressible" cannot be satisfied when the
floor is the head itself: the window in front of a near-head floor is exactly the
sliver, and enlarging it would mean deleting the operator's own instruction —
the invariant the floor exists to protect (Hermes #10896/#29824).

(a) is not "ignore user messages": the exemption is **structural**. A message is
a harness injection when its own first line carries the framework marker
(`harnessMessagePrefix` = `"[system] "`, or one of the two bracketed markers).
A real operator instruction — the epic at index 0, or a follow-up typed later —
still floors the deletion, so the "compaction must not swallow the current
instruction" invariant is intact (pinned by a test).

(c) exists because (a) cannot create a window where none exists. Once the
operator's instruction (or the last visible answer) sits at the head, the pass
truly has nothing to take; the context then stays above the target for the rest
of the run. That state is now **operator-visible**: one warning per session
naming the pinned anchor.

## 3. The change

| file | what |
|---|---|
| `internal/agent/context.go` | `harnessMessagePrefix` + `harnessMessageMarkers` (single source of truth); `isHarnessInjectedUserMessage`; the three `anchorStop*` reason constants (replacing the bare `"last user"` / `"last visible assistant"` literals); the anchor scan now requires `!isHarnessInjectedUserMessage(m)` |
| `internal/agent/compact.go` | `compactPinnedAnchorWarning` + `Agent.warnPinnedAnchor` (once per session, names the anchor); `maxCompactAnchorSliverMessages`; the warning fires on an anchor-pinned empty window and on an anchor-pinned sliver that frees < `compactEffectiveSavingsPct` |
| `internal/agent/loop.go` | 13 injection-message constants now build on `harnessMessagePrefix`; `taskAreaConclusionsMarker` named (historical `…MessageMarker` kept as an alias); `compactPinnedAnchorWarned` latch |
| `internal/agent/chat_ops.go`, `internal/agent/reframe.go` | the remaining 4 injection sites reference `harnessMessagePrefix` |

Behaviour of the harness messages themselves is unchanged: they are appended as
before, they still reach the model, and they are **not** rewritten or dropped.
Only their eligibility as a *floor* changed.

The 18 sites are the whole set of framework-appended user messages (checkpoints,
job/truncation notices, task-list nudges, carry-forward and hot-tier blocks,
end-turn continuations, resume continuation, reframe nudge).

## 4. Red → green proof

Test files: `internal/agent/compact_followup_anchor_test.go`,
`internal/agent/compact_pinned_anchor_test.go`.

The tests are **behaviour-level — no test-only flag exists in production**, so
the red proof is a plain stash of the touched production files (the new test file
also declares its own `anchorStopLastUserWire` copy of the reason string, so it
compiles against the pre-fix sources):

```bash
cd ~/workspace/gopi-compact-followup
git stash push -- internal/agent/context.go internal/agent/compact.go \
    internal/agent/loop.go internal/agent/chat_ops.go internal/agent/reframe.go
go test ./internal/agent/ -run \
    'TestCompact_HarnessFollowUpDoesNotPinTheWindow|TestTakeOldest_HarnessFollowUpIsNotTheInstructionFloor' -count=1 -v
git stash pop
```

**RED (master production source + new tests):**

```
--- FAIL: TestCompact_HarnessFollowUpDoesNotPinTheWindow
    the harness follow-up pinned the compaction window: the context moved 151269 → 150438
    heuristic tokens (0.5%, < 10%) in 1 summarizer call(s), AnchorStop="last user" — the
    instruction floor is a framework message, not the operator's, so the session's own work
    behind it is unreachable; log:
        🛡️ compaction anchored at last user — keeping est above target rather than losing the task anchor
        🧠 memory compaction: summarized 4 oldest message(s) into a memory block
        ⚠️ 🧠 context window: est 157995 tokens (measured 0) > 150000 (79.0% of 200000) —
           compacted to est 150438 (75.2%, target 35.0%) — assertion: after ≤ target+summary = false
--- FAIL: TestTakeOldest_HarnessFollowUpIsNotTheInstructionFloor
    the deletion stopped on the harness injection ("last user") — a framework message must not
    be the instruction floor
FAIL  github.com/the operator/gopi/internal/agent  1.865s
```

That failure output is the field symptom reproduced in a unit test: the context
ends **over the limit** (150438 > 150000) after a compaction that took a sliver,
with `AnchorStop="last user"` naming a framework message.

**GREEN (with the fix):**

```
--- PASS: TestCompact_HarnessFollowUpDoesNotPinTheWindow (0.14s)
--- PASS: TestTakeOldest_HarnessFollowUpIsNotTheInstructionFloor (0.00s)
ok  github.com/the operator/gopi/internal/agent  1.456s
```

Measured with the fix (probe run, same fixture): `calls=1 before=151269
after=65286 saved=56.8% AnchorStop="" msgs=29` — one summarizer call frees 56.8%
instead of 0.5% with no working path.

What the green run also pins: a real operator instruction still floors the
deletion, the newest visible assistant answer survives (reply floor), and the
compacted head is the memory-block summary.

## 5. Verification

```
go build ./...                                               → BUILD_OK
gofmt -l internal/agent/                                     → (clean)
go test ./internal/agent/ -run 'TestCompact|TestTakeOldest|TestTrimContext' -count=1  → ok 4.389s
go test ./internal/agent/ -count=1                           → ok 111.823s   (full package)
go test ./internal/agent/ -run <the 3 new tests> -count=1 -v → 3 PASS
go test ./internal/standards/ -count=1                       → ok 22.611s (no new gate hits)
```

## 6. Not Done (honest)

- **No live-LLM adoption run.** The change is anchored in the **message-content
  marker the framework itself writes**, not in a prompt/description an LLM must
  be persuaded by, so Rule 11's LLM-adoption proof does not apply to it. The
  guard is the deterministic test pair above.
- **The residual pinned state is reported, not solved.** After the fix, a
  session whose head *is* the operator's instruction (or whose tail is the only
  window) still cannot compact below the anchor. That is correct — the
  alternative is deleting the instruction — and it is now visible via the
  once-per-session warning (§3). Making that state *compressible* would require
  a policy for rewriting the pinned instruction, which is a separate design
  question and is deliberately not attempted here.
- **Tool-result-only user frames were not made non-anchors.** On the Claude wire
  a tool result is a user-role message (`{"type":"tool_result"}`), and this
  session's newest user frame at the very end of the run was one. Master's
  scan is role-only there too. It is not the observed trigger (the injections at
  the head are), so the change is scoped to the measured defect; a tool-result
  frame cannot precede an assistant turn and therefore cannot pin the window the
  way an injection near the head does.
- **The tool-result path is untested** by the new tests; the fixture covers the
  measured shape (injection near the head + bulk behind it + visible answer at
  the tail).
