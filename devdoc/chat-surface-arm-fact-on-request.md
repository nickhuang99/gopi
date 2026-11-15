<!-- doc-class: normative -->
# devdoc: the arm fact rides the REQUEST, not the screen

**Date**: 2026-10-11 · **Task**: `01a12b14-c54a-7e84-93c0-8530e794ae8b` ·
**Branch**: `task/01a12b14c54a7e8493c08530e794ae8b` (the ledger id without its
dashes — the spelling the ref actually carries; `gopi task for-branch
task/01a12b14c54a7e8493c08530e794ae8b` resolves it back to the row above)

## The problem, and why the previous fix did not close it

`gopi chat --tools` used to tell the operator, ON THE SCREEN, whether this run's
own screen dump is armed. The operator's ruling (2026-10-11):

> 屏幕上只留和人有关的东西 — the screen is for the HUMAN; the arm state is the
> MODEL's business.

Two spellings of that notice existed, and a previous task moved the sentence
between them instead of removing it:

| spelling | where it lived | rendered by |
|---|---|---|
| `[gopi] chat_surface tool: this run's own screen is readable → <path> …` | the answer band | `chatSurfaceArmedNotice` (a stream write at the arm seam) |
| `[gopi] chat_surface tool: this run's own screen dump is OFF …` | the answer band | `chatSurfaceUnarmedNotice` |
| `screen dump → <path>` / `screen dump off — restart without --no-dump-screen` | the **startup screen's** notices section | `chatBannerNoticeScreenArmed` / `chatBannerNoticeScreenOff` |

The previous fix moved the notice into the startup screen and taught its e2e case
to look for `[gopi] chat_surface` **in the band** — while the surviving line was
the panel copy `screen dump → …`, whose stream spelling carries no such prefix.
That is this task's measured blind spot, and the gate below is written against
the CHANNELS (the whole pty stream, both spellings) rather than against one
wording.

## The disposition (方案 A): the fact becomes a system note on the request

The fact does not disappear — it moves to the carrier that was always meant for
it. The model reads it in the tool's schema on the wire, in the tool's own
refusal sentences, and (on a successful `see`) verbatim in the observation; the
arm STATE is now stated once, on the system prompt of the very request it
describes:

```
[CHAT SURFACE TOOL — ARMED] this run writes its own screen: every frame it paints
lands in <path>, and the `chat_surface` tool's `see`/`compare` actions read that
artifact back as a CELL GRID (attrs per cell-range — no pixels, no vision).
`type` enqueues your next turn; `key` injects a keystroke into this run's line
editor. These are the FACTS behind the tool whose def is on this wire.
```

and, for a declined dump, the ABSENCE OF OBSERVATION in words — never "the screen
is blank" (a reader that cannot tell "no observer" from "nothing painted" will
report the second when it has the first).

### Where it is assembled, and why not derived in `internal/agent`

`package main` owns the surface, and `internal/agent` cannot import it (Rule
14/3: the dependency direction is fixed). So the fact travels as a
PUBLISH-AND-RENDER pair:

- `cmd/agent` PUBLISHES what only it knows, from the arm seam
  (`installChatSurface` → `ag.SetChatSurfaceFact(armed, path, toolsOn)`) — the one
  place where the resolved dump sink and the run's own session both exist;
- `internal/agent` RENDERS it (`internal/agent/chat_surface_fact.go`) and both
  chat assemblies append it (`assembleChatSystemPrompt`, `chatOpsSystemPrompt`).

The zero value means "nothing was published" and renders as NOTHING, which is
what every task run (`Run`) and every tool-less chat needs: the note describes a
TOOL, and an instruction the agent cannot act on is worse than none. That is also
why `toolsOn` is part of the publish contract rather than a call-site
convenience: the plain `Agent.Chat` path carries no tools at all (its own block
says so), so it is never handed a note about a capability it does not have.

### Why the note is assembled rather than a frozen string (Rule 2)

The arm state is a property of the RUNNING PROCESS and the path embeds this run's
identity, so a package-level `Text` would freeze whatever an init-time evaluation
saw. The two sentences are constants; the rendering happens per assembly.

## What deliberately did NOT move

- **The panel's `surface` FACT row** (`screen readable` / `screen off`). A fact
  about this run belongs beside the session id and the model — it is a FACT, not a
  NOTICE, and it is what makes the stream's cleanliness a MOVE rather than a
  deletion (the pty case asserts it).
- **The artifact-plumbing line** `gopi chat: screen dump → <path> …`, printed
  BEFORE the layout takes the terminal. It states where the artifact went, which
  the operator can act on, and it is what the pty case's occurrence accounting
  leans on to distinguish "the operator was told where the dump is" from "the arm
  state is on the screen again".
- **The ANOMALIES** (Rule 15): a dump that cannot be ARMED stops the run with its
  reason on stderr; a surface identity that cannot be PUBLISHED keeps its notice
  (stream line + the one surviving notices-section copy
  `surface identity not published — …`). A human has to be able to act on those.

## The gate

`testsuite/chat_surface_fact_pty_test.go` — `TestChatPTY_ArmFactRidesTheRequestNotTheScreen`,
a REAL pty run of the REAL binary (`testutil.GopiBin`, `chat --tools`), in three
parts:

1. no chat-surface chrome anywhere in the startup stream — the whole stream is
   swept for both spellings, because the band-only assertion was the blind spot;
2. the box was DRAWN (`chatPanelBoxBottomGlyph`), so (1) cannot pass vacuously,
   plus occurrence accounting on `screen dump →` against the plumbing line;
3. the fact MOVED, on all three carriers it now has: the panel row (operator), the
   mock LLM's received request body (model), and the run's OWN raw log
   (`gopi session raw`'s record).

**Red proof** (revert = red, measured against a base binary built from `master`
at `4f7484122` in a detached worktree at `/dev/shm/gopi-base-redproof-wt`):

```
GOPI_BIN=/dev/shm/gopi-base-redproof go test ./testsuite/ -run 'TestChatPTY_ArmFactRidesTheRequestNotTheScreen' -count=1
--- FAIL: TestChatPTY_ArmFactRidesTheRequestNotTheScreen (12.48s)
    chat_surface_fact_pty_test.go:141: the arm-state panel copy ("screen dump →") appears 2 time(s)
      in the startup stream but the plumbing line only 1 — an armed/unarmed copy is on the screen again.
    chat_surface_fact_pty_test.go:180: the request's system prompt does not carry the arm fact
      (marker "\n\n[CHAT SURFACE TOOL — ") — the fact left the screen and did not arrive on the wire
```

Both halves of the design fail on the base: the chrome is still on the screen AND
the fact is absent from the wire — which is exactly what "moved, not deleted"
means as an executable claim.

## Non-obvious traps hit while building this

- **A pty CR must be its own write.** `chat.write(t, "text\r")` in one burst is
  read by the line editor as a PASTE (bracketed-paste framing), so the trailing CR
  lands inside the framing and the line is never submitted — the run then issues
  no request and the model-side half of the case is untestable. The case types the
  text with `writeGapped` and sends `"\r"` separately.
- **The startup flush has to be COLLECTED, not just awaited.** `waitFor(prompt)`
  returns at the moment the prompt is painted; the screen is committed after it,
  which is what `collect` + `chatPanelCollectWindow` exist for.
- **The marker must be the prompt's own.** The outside assertion reads
  `agent.ChatSurfaceFactMarker()` — a re-worded head would leave an outside check
  passing against text nothing paints any more (the same contract
  `ChatSurfacePromptHead` already has on the chat block).
