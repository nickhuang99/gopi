<!-- doc-class: historical -->
# The Chat-Scoped Tool: `chat_surface`

> Classification: normative. A tool-enabled chat turn can see its own painted
> screen and type its own next input — and the rules that stop that from becoming
> a second, unwatched control loop.

## 1. The problem

A tool-enabled chat turn had no way to observe **itself**. It could read any file
on the machine and drive the desktop, but not answer "what did I just paint on
this console, and is the line I wrote actually there?" Nor could it continue
without a human typing. Both are needed for a loop that checks its own output —
and both are exactly what the runaway-loop incident
(`devdoc/runaway-loop-20260923.md`) says must carry a visible stop.

## 2. 约束反应: an addition, not a kernel change

The obvious shape — a new entry in `claudeToolDefs` — is the wrong one, for two
independent reasons:

1. **Rule 3.** `claudeToolDefs` is the SSOT of the ordinary surface, pinned
   against GOPI.md's 17-tool inventory by `TestGOPIRule3_MatchesClaudeToolDefs`
   and demanded to carry a class for every entry by
   `TestToolClassesCoverTheToolSurface`. An entry added there IS a new core tool.
2. **Reach.** The same loop runs under `gopi web --tools` and `gopi cli --tools`,
   neither of which owns a chat surface. Advertising `chat_surface` there spends
   a sentence on the model for a call it can only lose.

So the def lives in its own list (`chatScopedToolDefs()`), appended by
`a.chatOpsToolDefs()` at the ONE place the chat request is built — and only when
`a.chatSurface.surface != nil`. The task path's slice is untouched.

## 3. The import cycle that shapes the split

`internal/gopilog` imports `internal/agent` (the transcript formatter), so
`internal/agent/chat_surface.go` **cannot** import gopilog — reading a session
record where the tool is defined is an import cycle, and the first build failed
on exactly that.

The resolution is structural, not a workaround: the transcript/dump read is an
**injected parameter** (`ChatSurfaceDumpReader`), and the body — the part that
knows about the REPL, the editor and the session tree — lives in `cmd/agent`
(`chat_surface_surface.go`), which installs itself through one interface method,
`SetChatSurface`; `installChatSurface` is
`cmd/agent/chat_surface_surface.go:327`. The reading itself is
`chatSurfaceTranscriptLines` (`cmd/agent/chat_surface_surface.go:288`). Every
action with no surface installed answers a **sentence naming what is missing**
(`chatSurfaceUnavailableNotice`), never a silent empty success (Rule 15).

## 4. The four abilities, and what each refuses to fake

| action | answer | the dishonest version it avoids |
|---|---|---|
| `see` | the cell grid + qualification | "the screen is blank" when the run is simply **unarmed** — an absence of OBSERVATION and an empty screen are different facts |
| `compare` | `status=match\|miss\|no-transcript` + `reason:` | claiming a comparison when the authoritative side was never read; a qualified grid is refused BEFORE the transcript is fetched (asking for it would imply the transcript is the missing piece) |
| `type` | acceptance with `bound_in_force` and `turns_used` | a "typed=true" that nothing will honour — the surface's own refusal is carried through when it cannot accept a line |
| `stop` | `stopped=true pending_dropped=…` | a stop with nothing in hand is a NO-OP (`nothing to stop`), and it does **not** latch: a loop nobody started must stay startable |

`type` is rune-bounded, and the caller's `max_turns` is **clamped down** to the
declared bound — an engine bound is not the model's to raise.

## 5. The bound: one seam owns the counter and the drop

`ChatSurfaceTakePending` (`internal/agent/chat_surface.go:337`) is the only place
the bound is enforced and the only place a typed line can disappear. It returns a
typed event rather than a `(line, ok)` pair, because the interesting case is the
third one:

- `SeamNone` — nothing pending; the carrier reads its own input.
- `SeamTyped` — run this line, charged. **`Closing=true` on the last one**, so a
  single console line carries both the turn and the loop's end.
- `SeamClosed` — the loop is over and (when a line was typed) it was **dropped
  here**. The carrier prints the termination notice.

And the **carrier** owns exactly what only it can: reading its own input, and
saying out loud what happened — `[gopi] → (self-driven) …` as the line is typed,
`[gopi] self-driven turn 1/3 … this loop stops at its bound` as it becomes a
turn, `[gopi] self-driven loop CLOSED …` when it ends.

**Why the line is consumed at the TURN BOUNDARY, not mid-turn.** Injecting a
"pending note" into the running turn would make it a second turn wearing the
first one's rounds — and the bound, which counts turns, would count nothing.

**Why `stop` does not drop the pending line itself.** Two places that can drop a
line are two places that can disagree about whether it was dropped. `stop` only
latches the decision; the drop happens at the seam. The typing side is
`Enqueue` (`cmd/agent/chat_surface_surface.go:238`) and the grid side is
`ScreenGrid` (`cmd/agent/chat_surface_surface.go:98`).

## 6. Two gates this change had to repair (and one it could not)

- `TestGate_ToolDispatchCoversEveryDef` failed: it knew only `claudeToolDefs`, so
  a chat-scoped dispatch arm read as **dead code** — the opposite of the truth.
  (`runChatSurfaceTool` is `internal/agent/chat_surface.go:410`; the args
  validator is `validateChatSurfaceArgs`
  (`internal/agent/chat_surface.go:182`).)
  It now checks both wires (direction 1 and 2) and asserts the two are
  **disjoint** (a def in both would be sent twice on a chat turn).
- Four devdoc anchors whose cited line my +23-line `loop.go` shift moved were
  **re-keyed by symbol**, never by arithmetic shift.
- `TestDocAnchorsResolveInNormativeDocs` is **red on master already** (21 anchors
  fail on a pristine `git archive HEAD` extract, 15 with this change): a
  corpus-wide repair, reported rather than silently absorbed. The baseline
  permit file ratchets DOWN only, which is why the two `stale baseline entry`
  lines are also red on master.

## 7. Acceptance, on a real pty

`gopi chat --tools --dump-screen` at 100×30 against a scripted SSE stub, in a
sandbox profile. One human prompt produced all four actions; the outputs are in
the task report (`internal/agent/chat_surface_test.go` pins the contracts). Four
traps cost a run each and are recorded in memory #6279 — the sharpest two:
**a plain `gopi chat` has no tool surface at all** (its request carries no
`tools` key), and **the endpoint environment outranks settings.json** because
config publishes it fill-only.
