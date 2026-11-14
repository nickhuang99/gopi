<!-- doc-class: historical -->
# The status row's symbols must be readable — bug #3 (`≡`, `⧉`)

**Task** `01a0e4c7-765b-72cd-a2a1-3b8af5059498`. **Branch**
`task/01a0e4c7765b72cda2a13b8af5059498`.

## The defect, as the operator filed it

The chat status row prints two symbols the product does not explain to the
reader who sees them: `≡` glued to the context pair, and `⧉2` — the
decoration/compaction counter. The operator's own line:

```
fake-model     · iter 2  ·  no tool  · ctx 31.7k/20.0k≡ ·↑0↓1M   ⧉5 · ready
```

## What was actually true when it was measured

Measured on master HEAD `2044ae98` (worktree tip `3c1289e6`), real pty
117x26, `bin/gopi` built from that tree, six scripted turns against a local
SSE stub:

- `≡` **was already named** — `gopi chat -h` carried the status-row legend,
  whose context-pair paragraph reads: *"the CONVERSATION's estimated tokens
  (92.1k) against GOPI'S OWN working budget (200.0k) — the `≡` says the
  denominator is that budget."* The operator's filing is one symbol too broad:
  the qualifier had a definition, and nothing else in the row did.
- `⧉` had **no definition anywhere**: `grep -c '⧉'` on the real `gopi chat -h`
  output = **0**, and 0 in `commands.go`'s in-chat `/help` too. A reader met
  `⧉5 · ready` on every row of a long session and nothing in the product said
  what the number counted.
- The row can print **five** symbols, and only two had definitions (the `≡`
  paragraph and the `↑`/`↓` cache paragraph, which names `↑` implicitly by
  glossing "`↑12k↓4k` the prompt cache's transport").

So the real defect was a **class**, not two glyphs: symbols reach the row
faster than the legend does, and nothing checked.

## The fix

1. **The legend is now a named constant** (`chatStatusRowLegend` in
   `cmd/agent/cli_chat.go`), joined into `chatUsage` instead of typed inline —
   the text and the row can no longer be changed apart (Rule 13).
2. **`⧉` is named**, in its own named block (`chatStatusRowLiveLegend`), with
   the form written from the row's OWN constants (`statusCompactionGlyph`,
   `lineedit.StatusSep`, `statusLiveIdle`) rather than typed as a sample.
3. **`…` is named** — the layout's cut marker (`lineedit.StatusEllipsis`). It
   is not "decoration": the doctrine in `internal/lineedit/answer.go` says the
   difference between "the terminal is narrow" and "this is corrupted data"
   must be VISIBLE to the reader, and that only works if the reader knows the
   mark.
4. **The definition points at an event the reader can go and see.** The legend
   quotes the line the product prints when a compaction happens, and that line
   is now a shared constant — `agent.CompactionAnnouncement`, the same string
   `compactContext` logs through (before: a format string typed inline at the
   call site, invisible to any doc).
5. **A gate for the class.** `cmd/agent/chat_status_symbols_legend_test.go`
   resolves every symbol the row can print **from the source constants** (never
   from a literal list in the test) and asserts each one is defined in the help
   the reader can ask for (`gopi chat -h` + in-chat `/help`). It is
   bidirectional: a legend for a glyph no composer draws also fails.

The row's **pixels are deliberately unchanged**. The doctrine's first branch is
"name it where the reader can see it"; the second (do not print it) costs a
read-out. `⧉N` is a real fact about the session (how many times the context was
summarised away), so it was named rather than deleted. This is the same choice
the row already made for `≡`.

## Why the row was not shrunk instead

The status row's budget is `statusPinCols == 100` with **zero slack** (inherited
from the `statusCountFractionDigits` work), so there is no cell to trade for a
legend *on the row*. Naming it in the help costs the row nothing and reaches
every reader who asks "what is this?" — the doctrine's own arrangement.

## The one thing a reviewer should be suspicious of

The test's symbol list is hand-enumerated *call sites* (five composers), because
Go cannot enumerate "strings that can reach a field". It is not a compiler
guarantee: a NEW composer added without a list entry is not covered. What the
gate does guarantee is that the five known symbols cannot silently lose their
definitions, and `TestStatusRowSymbolGateGoesRedOnAnUndefinedSymbol` proves the
predicate still discriminates (it is asserted both ways, so a help text that
stopped composing cannot make the gate vacuous).

## Verification recipe (reusable)

- A real pty is required: a pipe is not a terminal, so the reserved layout
  (answer band + status row) never exists there.
- The scene: a local scripted Anthropic-SSE stub (`fake_llm.py`) that answers
  **both** shapes — SSE for streaming turns AND a plain JSON message for the
  non-streaming summarizer call. Measured trap: answering SSE to the
  summarizer produced `memory compaction failed: ... invalid character 'e'`
  and latched a 10-minute cooldown, so the compaction count stayed 0.
- The stub must report a REAL input-token count (`len(body)/4`). Measured trap:
  a stub that always answered `input_tokens: 10` calibrated the session to 10
  tokens and the compaction guard never fired at any window size.
- The session must actually get above its window **after** a compaction, or
  the count stops at 1 (the breaker latches after two ineffective strikes).
- Geometry must be set on the pty (`TIOCSWINSZ`) *before* the child paints,
  the prompts typed **after** the editor's `you> ` prompt appears (a line typed
  during boot is eaten by the cooked-mode read), and the child killed with
  SIGTERM **while the idle frame is still painted** — a `/quit` repaints on the
  way out and the dump freezes the last frame.
- Scripts live in the session dir, not the worktree (Rule 30/35).
