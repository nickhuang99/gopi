<!-- doc-class: normative -->
# `gopi chat -h` — the compression of a 615-line page, and where its narrative went

Task `01a10bb7da7b7ccca401538211f0c005` — layering the chat usage page:
measure its segments, classify flag-contract vs narrative, compress the page,
move narrative here.

## What the measurement found

The page (`chatUsage`, `cmd/agent/cli_chat.go`) was **615 lines** before this
change. Segmenting it at blank lines gave 54 segments; the page is a manual, not
a flag table. What the segments are, and what held them:

| segment | lines | what it is |
|---|---|---|
| header + `--once` face | 1–131 | the one-shot face's full manual (pipeline shape, unattended default, scripted sessions, the pipe-back driver, the delimiter, exit statuses) |
| `Interactive-session flags` | 133–168 | `--no-color`, `GOPI_PALETTE` (a 27-line palette essay) |
| `--dump-screen` | 170–264 | **95 lines** — arming, the reader (`chat_surface` tool + `gopi ui_state`), the exit contract, the measured cost, and what the dump is not |
| `--no-editor`, `-v`, `--tools` | 266–285 | three ordinary flag paragraphs |
| `--chat-ideal-geometry`, `--replay`, `--resume` | 287–332 | the geometry knob and the two session-recovery flags |
| crash recovery | 334–362 | 29 lines of worked recovery commands |
| film / beat protocol / worked examples | 364–445 | the film's own 30-line block, the beat protocol, five numbered command lines, and the pixel essay |
| slash commands + shared table | 472–499 | the in-chat command list |
| **`THE STATUS ROW`** | 501–599 | **99 lines** — the legend for a status row whose glyphs must be documented (see below) |
| exit status + examples | 604–615 | the verb's own contract |

**How much of it is pinned by a test was measured, not guessed.** Every string
literal of 8+ characters in the package's (and `testsuite/`'s, and
`internal/**`'s) test files was matched against the page's own text, twice: once
over all test files, once restricted to the 21 files that reference the chat help
surface. The result is the finding that shaped this change:

- **Over all test files: 157 required substrings, spread thin.** Relaxing the
  floor to 12+ characters leaves 464 of 558 non-blank lines with *no*
  test-pinned line at all — a first pass that suggests the page is almost entirely
  free to cut. **That pass is misleading**: most of those "pins" are generic
  English (`' and the '`, `' without '`) matched by unrelated tests, and a line
  carrying one is not a contract.
- **Restricted to the 21 chat-help test files: 34 real pins.** These name flag
  spellings (`--no-dump-screen`, `--ack-timeout-ms N`, `--max-beats N`,
  `-v, --verbose`, `--max-seconds S`, `--stop-on TEXT`, `--prompt TEXT`), the
  reader of the dump (`gopi ui_state --target chat --all`), the recovery recipe
  (`gopi chat <session-id>`, `gopi chat --tools`), the one-shot contract
  (`1 = a turn FAILED`, `1 = the turn failed`, `2 = usage error`,
  `never a silent success`, `NOT a failure`), and one phrase whose *exact* spelling
  the dump contract test requires (`type a line starting with '/'`).

The conclusion both passes support: **the page's length is not protected by its
contracts — it is 615 lines because it accumulated prose.** The pins are flag
spellings and exit-status sentences; the paragraphs around them are explanation,
and explanation belongs in a devdoc.

The one genuinely pinned *block* is `THE STATUS ROW`: `chatStatusRowLegend` +
`chatStatusRowLiveLegend` are held by `TestStatusRowSymbolsHaveAReaderVisibleDefinition`
(`cmd/agent/chat_status_symbols_legend_test.go`), which resolves every glyph the
row can print and demands each one be defined in `chatUsage + /help` — so a glyph
cannot lose its definition. What that test does *not* require is the row's design
history, and that is what was compressed out.

## What changed

The page went **615 → 533 lines**, all of it inside three named help constants
(no test, dispatcher or parser line changed):

- `cmd/agent/cli_chat.go` — `chatUsage`
- `cmd/agent/chat_geometry_usage.go` — untouched
- `cmd/agent/chat_replay_help.go` — `chatReplayExamples`

The rule applied per paragraph: **keep the contract, drop the derivation.**

1. **`--dump-screen` (95 → 81 lines).** Kept: the arming sentence, the `PATH` /
   joined-form rule, the two readers by name (`chat_surface` with `action=see` /
   `action=compare`, and the one-call audit `gopi ui_state --target chat --all`),
   the full exit contract (`0` / `3` / `1` and why `1` is not agreement), the
   damage vocabulary, `GOPI_CHAT_DUMP` and its falsey values, the
   `--no-dump-screen` OFF switch and `chatDumpWriter`'s unchanged-writer
   guarantee, the `frames=` caveat, and the pointer to this file. Dropped: the
   design rationale for being on by default, the throttle arithmetic restated
   three ways, the measured byte/frame/write figures, and the "what it is not"
   recapitulation.
2. **`THE STATUS ROW` (99 → 58 lines).** Kept: every glyph and every word the row
   can print, with the meaning a reader needs to read the row. Dropped: the
   prefill-cost derivation of the budget, the argument for the pair's order, the
   story of the two-column mode+activity layout that was replaced, and the
   restatements of which fact yields at which width.
3. **`chatReplayExamples` (the film/beat/worked-command block).** Kept: all five
   worked command lines (they are the discovery deliverable and are asserted by
   `TestChatHelpLeadsToTheReplaySurface`), the pixel reference and the three
   size→pixel rows (derived from `chatRefCellW/H`, `chatRefBorderPx`). Dropped:
   the parenthetical justification after each step, the `COLSxROWS` philosophy
   paragraph, and the CJK-terminal aside that belonged to a different task.
4. **`GOPI_PALETTE` (27 → 14 lines).** Kept: every palette name, the depth rule
   that selects the default, `COLORTERM=truecolor`, `#FFF8DC`, and the fallback
   contract. Dropped: the four-name indented sub-table (the names now sit in the
   prose that explains them) and the "not per invocation" framing.

## Where the dropped material lives

The figures and tables that the page used to carry inline, so a reader who wants
the *reason* can still find it:

**The status row's context pair.** The denominator is gopi's own trim threshold,
set from **prefill cost** on this stack: a 290K-token prefill was measured at
**~16s**, and trimming begins at **75%** of that — so the budget is a
throughput decision, not the backend model's advertised window. The pair is
drawn in the row's own bounded ladder (whole below 1000, one unit above: `12k`,
`1M`), which is why it stays inside its reserved cells at any counter value. The
pair LEADS the group because it is the quantity that predicts a compaction; where
the cells run out the label yields and the pair is shown whole;

**The cache sub-cell and its rate are one value.** `↑12k↓4k hit 74.5%` is drawn
as one read-out — the pair leads, the rate rides it, and they are shed together
(a whole sub-cell at a time, never a sub-cell's tail), so a cut percentage is
never printed beside a pair it no longer describes. Neither number is a ratio's
denominator: a session's writes accumulate across turns far past any single
prompt. Nothing is printed until the cache has actually answered, so a session
whose prompts were all new shows an EMPTY sub-cell rather than an invented
"0/0" — `statusMetricsUnmeasured` is the word for "nothing measured yet", and it
is a statement about the group's own state, never a measurement of a counter.

**The tools fact used to cost two columns.** A mode column naming the mode beside
an activity column naming the absence is the same bit of information in two
opposite signs, nine cells apart, and it read as a contradiction. One column
replaced them.

**The dump's measured cost.** The sink re-interprets the surface's own paint
stream through `uiclient.ScreenDumper`. Measured: **5793 bytes** for a 117×26
frame and **6.0 KB/s** of writes over a short interactive run — that is the
CEILING, not the rate, because writes are throttled to at most one per
`dumpMinInterval` (100 ms) and the layout paints only when the screen changes. A
long run measured **18408 frames against 2718 file writes** — about one read-back
write per 7 frames — and an idle chat freezes at `dumps=1` and then writes
nothing. The path is printed before the REPL takes the terminal (the
`screen dump ->` line), so the record is never written in silence. The observed
`frames`/`dumps` pairs: an idle 117×26 chat reads `frames=3 dumps=1` and does not
change again (byte-identical header for 10s), a turn that painted reads
`frames=117 dumps=10`, a chat that answered reads `frames=687 dumps=45`.

**Why the page was 615 lines at all.** Because a help page has no size gate of
its own: `TestTopLevelUsageIsAlignedAndTerse` bounds the TOP-LEVEL page
(`topLevelUsageMaxLines = 176`) and `TestEveryHelpConstantFitsTheWrapBudget`
bounds each line's display width, but nothing measured a subcommand page's
LENGTH. Every paragraph added to answer a reader's question was cheap, and none
of them was ever removed. This change is the removal pass; the table above is
what makes it auditable rather than a judgement call.
