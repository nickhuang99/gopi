<!-- doc-class: historical -->
# The chat palette and status row, proved on a live screen

Task 01a0db1f-cfb1-71db-be51-7cbeea12d63f — child 3/3 of the epic "Restyle
`gopi chat`'s terminal UI to a Hermes-derived warm gold palette": the END-TO-END
LIVE PROOF of what child 1 landed, plus the record of what the epic asked for and
did **not** get.

Everything quoted here was measured by ONE run of a real binary on a real X11
display in a real terminal, driving a real model turn. The harness is
`scripts/ui/chat-live-proof.sh` (committed by this child); its artifacts are under
`devdoc/evidence/chat-palette-live-proof/`.

## 1. The run, and what it observed

```
$ bin/gopi build                        # CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
$ scripts/ui/chat-live-proof.sh --bin "$PWD/bin/gopi" \
      --out devdoc/evidence/chat-palette-live-proof
```

The harness boots Xvfb on a private display (`:99`), starts `xterm -u8` at
`120x34` holding the 1204x684 window under test, runs the SHIPPED `bin/gopi chat`
in it with `GOPI_PALETTE=hermes COLORTERM=truecolor TERM=xterm-256color` and the
operator's own `settings.json` copied into a throwaway HOME (the model call is
REAL — a mock would prove nothing about what a user sees), types a prompt, presses
Enter, waits for the turn, and captures the ACTIVE WINDOW with `gopi capture
--window` on both sides of the turn.

| observed | value |
|---|---|
| `gopi capture --window` BEFORE | `before.png`, `sha256_16=3debc40ce0b4178c`, 27081 bytes, window `0x20000c` (`window_route=QueryPointer`) |
| `gopi capture --window` AFTER | `after.png`, `sha256_16=9b19103cd7caea8a`, 77457 bytes, window `0x20000c` |
| the screen changed | **yes** — the two hashes DIFFER, on the SAME window, same geometry, same palette |
| the turn | real model, `stop_reason=end_turn`, `usage: input_tokens 86 / cache_creation 0 / cache_read 0 / output_tokens 188` |
| the answer on screen | `▌ ▌ A goldfinch is mainly bright yellow with black and white markings, and European goldfinches also have a red face.` |
| the status row while working | `deepseek-flash   ·  iter 1  ·        ·                       ·                         thinking…` |
| the status row when done | `deepseek-flash   ·  iter 1  ·        ·                       ·  274 tokens             ready` |

The status row lines above are the chat's OWN interpreted cells
(`gopi chat --dump-screen`), not OCR: at `120x34` the row is screen row `033`, the
one reserved bottom row. A vision read of the same PNG returned *nothing* while
the dump returned the exact text — which is why this child's read-back is the
cell grid.

### The palette, measured in pixels and in cells

`scripts/ui/chat-ui-measure.py` on the two PNGs (band = one text row; the
terminal's own background here is `#300A24`):

| capture | band | dominant bg | brightest pixel | WCAG | median | core% |
|---|---|---|---|---|---|---|
| before | banner (rows 41-56) | `#300A24` | **`255,215,0` = `#FFD700`** | 12.53 | 230,190,4 | 43.0 |
| before | prompt line (645-655) | `#300A24` | **`255,248,220` = `#FFF8DC`** | 16.50 | 255,248,220 | 50.0 |
| after | answer body (rows 382-456, 4 bands) | `#300A24` | **`255,248,220` = `#FFF8DC`** | 16.50 | 189,172,161 | 28.0-28.5 |

And the same two colours in the chat's own ATTRIBUTE runs (so the cell that was
painted is identified, not merely a band average):

```
013|0:120=fg=rgb:fff8dc                                  ← the turn header
018|0:2=default 2:11=fg=rgb:fff8dc dim 13:107=fg=rgb:fff8dc   ← thinking body
026|0:120=fg=rgb:fff8dc                                  ← the closing rule
032|0:4=fg=rgb:fff8dc bold 4:116=default                 ← the prompt "you> "
033|0:16=bold 16:3=default 19:8=bold 27:3=default 30:6=bold 36:3=default 39:21=bold 60:3=default 63:24=bold 87:5=bold italic 92:28=default
```

Row `033` is the whole salience argument in one line: **five `bold` spans
(`0-15`, `19-26`, `30-35`, `39-59`, `63-86`) and one `bold italic` span
(`87-91`), no colour code at all.** The status row took the gold palette
without taking a colour — the invariant task 01a0dacf won (`palette_status_
salience_test.go`) survives in a live render, not just in the table.

## 2. The palette decision (as landed by child 1, verified here)

`Palette.Answer` was `Style{}` — the unstyled baseline. It now carries Hermes'
own body text: `#FFF8DC` on a dark terminal (`HermesDarkPalette.Answer`), `#3D2F13`
on a light one. The gate that makes that safe: the gold family is 24-bit because
it does not exist in the 16 named colours, and `38;2;r;g;b` at a terminal that does
not understand it prints its parameters as TEXT. So the table is drawn only where
the terminal advertises 24 bits (`COLORTERM`), and the second half of the ONE
capability decision lives beside the first:

```
ColourWanted(noColor, sink)      flag > NO_COLOR(presence) > TERM=dumb > sink is not a tty   [colour.go]
  └─ capability: colour on? · which table (GOPI_PALETTE)? · truecolor advertised?
PaletteForCapability(cap)        off → MonoPalette · gold asked, no 24-bit → BasicPalette · else the table
```

`GOPI_PALETTE` is environment-only: a palette is a property of the terminal the
operator is SITTING AT, so it belongs in a shell profile, not in a shared
`settings.json` that would colour runs at terminals that are not this one.

## 3. Per-field data provenance (what the row shows, and where the number comes from)

| cell group | cells @120 col | value in this run | produced by | read by the row through |
|---|---|---|---|---|
| model | 0-15 | `deepseek-flash` | `settings.json` `model` | `newChatStatusSource(model, …)` at REPL start |
| iter | 19-26 | `iter 0` / `iter 1` | the REPL's own turn counter `iterCount` | `publishChatStatus(input, status, iterCount, live, tokens)` → `update()` under the row's mutex |
| mode | 30-35 | blank (`--tools` off) | `sess.toolsEnabled` | `newChatStatusSource`'s `modeHint` |
| tool | 39-59 | blank (no tool ran) | `Agent`'s tool callback | `status.setTool(name, summary)` / `setToolDone` / `clearTool` |
| tokens | 63-86 | `274 tokens` | `agent.Agent.Cost` (`deepseek.CostTracker`) | `totalTokens = Cost.TotalPromptTokens + Cost.TotalCompletionTokens` after each turn — **verified live: 86 + 188 = 274** |
| live | 87-98 | `thinking…` / `ready` | the REPL's phase state | `publishChatStatus(…, live, …)`: `iterCount+1` while a turn runs, `statusLiveIdleTurn` when idle |

The epic's headline field list — context used/max/percent + fill bar, cache
read/write, cost marked estimated-vs-exact, session elapsed, compression count —
is a **DISPLAY gap, not a data gap**, and the provenance of each is:

| epic field | the counter already in the tree | why it is not on the row today |
|---|---|---|
| context used / max / percent + bar | `agent.Agent.ContextWindow` (`DefaultContextWindow = 200000`, `internal/agent/context.go:19`) + the context's own `EstimatedTokens()` (already used by the compaction trigger) | no status column, no renderer for a fill bar, and no field on `chatStatusSource` |
| cache read / write | `deepseek.CostTracker.TotalCacheHitTokens` / `TotalCacheMissTokens` (`internal/deepseek/types.go:127-132`) | exists and is already fed per round; the row never reads it |
| cost (estimated vs exact) | pricing is an OFFLINE, date-aware path (`gopi stats --cost` / `--bill`, `internal/sessionevent/events.go:35`) | the chat path has no per-turn money figure, so a live segment would have to invent one |
| session elapsed | `time.Since(sessionStart)` — computed today only at close | never published to the row |
| compression count | the compaction mechanism exists (`internal/agent/compact.go`); only tests tick a counter (`compact_sim_test.go`) | **no production counter exists** — this one is the only true data gap of the five |

## 4. Width reservation and degradation

Every field has a reserved width and the whole geometry is pinned, so a field's
START CELL is a constant instead of a function of its value:

```
model 16 + iter 7 + mode 5 + tool 20 + tokens 21 + 4 separators × 4 cells
      + gap 2 + live 12                              = 99 cells of content
statusLayoutBudgetCols = 100                          → 1 cell of headroom
const _ = uint(statusLayoutBudgetCols - statusColumnsAtCols)   ← compile-time guard
```

Measured: on the 120-column terminal the live hint's TEXT begins at cell **87**
(its reserved block is the 12 cells `87-98`), and the row's LAST attributed cell is
`99` — the composed row is 99 cells wide, and the dump's row `033` shows all six
reserved columns, i.e. the ALIGNED form.

| condition | what the user gets | observed this run? |
|---|---|---|
| terminal ≥ the pinned width | ALIGNED row: six reserved columns, fixed start cells | **yes** — the 120x34 capture above |
| terminal < the pinned width | UNALIGNED: `model · iter N · mode` label + live hint, fitted by `fitField` (the historical composition) | no — a code path (`statusLineColumns` returns `ok=false`), not exercised here |
| `cols <= 0` | `Status.Text()` — the label alone | no |
| colour off (`--no-color` / `NO_COLOR` / `TERM=dumb` / sink not a tty) | ZERO SGR bytes | no (proved by the `testsuite` e2e test on a piped run, not by this capture) |
| gold asked, no `COLORTERM=truecolor` | `BasicPalette`; the answer body UNSTYLED | no (proved by `testsuite` on a pty, not by this capture) |
| unknown `GOPI_PALETTE` value | reported on stderr, run continues on `basic` | no |

The three "no" rows in the capability table are honest gaps of THIS session: they
are covered by the committed e2e tests, not by a live capture taken here.

## 5. What was deliberately NOT copied from Hermes

Hermes is `/home/user/work/hermes-agent` (`ui-tui/src/theme.ts`,
`ui-tui/src/components/appChrome.tsx`). The DESIGN (colour values, role mapping,
width-budgeted segments) crosses; the stack does not.

| Hermes has | not copied, because |
|---|---|
| theme detection by background luminance (`HERMES_TUI_LIGHT`, `HERMES_TUI_BACKGROUND` → Rec.709 luma, `COLORFGBG`, a `TERM_PROGRAM` allow-list) | four heuristics plus a colour-space conversion, each wrong on a terminal that answers none of them; gopi uses an explicit knob with a named light table, and the dark table as the default (as Hermes' own default is dark) |
| the React/Ink full-screen layout, per-token syntax highlighting, box-drawing chrome | gopi's chat is a streaming line renderer whose band, editing line and status row are three writers over one terminal; copying the layout is a renderer rewrite, not a palette change |
| a new dependency for colour | the 24-bit sequences are composed by `internal/sgr`; this epic added ZERO modules |
| HUD segments with no gopi counterpart: subagent fleet + parked-background reassurance, voice-mode label, live-session count, dev-credits Δ | gopi's chat has no subagent fleet, no voice mode and no credits meter; a segment that can never have a value is a permanently blank column |
| mouse interactions on the status rule (click the session count, hover overlays) | the row is a line the editor repaints; there is no hit-testing and no overlay layer |
| an animated spinner/face ticker occupying the live slot | gopi's live hint is a word; a ticker would repaint the terminal continuously for no information |
| a live USD cost segment marked `estimated`/`exact` (`cost_status`) | gopi's pricing is an offline date-aware path (`gopi stats --cost`); the chat path has no per-turn figure, and printing one would fabricate a number |
| themed COLOUR on the status row | measured theme-dependent before (task 01a0dacf: SGR 90/2 → 4.39:1 on the operator's own theme), so the row carries a colour the theme CANNOT reinterpret — its table's own gold (commit `4265324c`), which is what this doc's own rule turns into a requirement rather than a ban on colour — the rule was misread as "attribute-only" here and corrected by task `01a0e2ed` |

## 6. What this child could NOT deliver, and why (honest gaps)

1. **The expanded status bar did not land.** The epic's item (B) — context
   used/max/percent + fill bar, cache read/write, cost, elapsed, compression count
   on the row — is not in this branch, and a survey of every local branch found no
   commit that lands it (`git grep` for the field names across all branches: the
   only hit anywhere is a test file in `cmd/agent`). Its ledger row reads
   `completed`, but no code carries it: the row in this capture is the six-field
   row, exactly as `statusColumnsAtCols` defines it. The provenance table in §3
   is the reason this is a DISPLAY gap: four of the five counters already exist.
2. **A future field must RE-PARTITION, never append.** §4's arithmetic leaves
   ONE cell of headroom under the 100-column budget and the guard is a compile
   error, not a warning — so the next field forces the author to decide which
   existing reservation shrinks.
3. **Turn-completion latency, recorded because it shaped the capture.** The
   endpoint's `max_tokens` (32768) exceeds what it will serve as a thinking budget,
   so the round streamed thinking until the harness aborted it at 8192 tokens and
   re-issued the call thinking-disabled (`[gopi] ℹ thinking budget 16384 clamped`).
   The answer landed at about 150s, i.e. AT the poll deadline: `after.png` is the
   screen of that instant, and the same state read a few seconds later shows the
   completed row (`iter 1 · 274 tokens · ready`) quoted in §1. Both instants are
   reported; neither is presented as the other.
4. **Two harness defects of the first attempt, kept in the open** (they are why
   the proof is credible — a capture that looks right for the wrong reason is
   worth nothing): `xdotool type --window` sends SYNTHETIC events, which xterm
   DISCARDS, so the first "turn" never reached the app and the two captures were
   byte-identical; and the child process inherited this session's
   `GOPI_TASK_REPORT_PATH`, which gopi correctly refused by name. The harness now
   types into the FOCUSED window (XTEST — indistinguishable from a keyboard),
   verifies the text landed on the editing line BEFORE submitting, and unsets both
   report variables in the child's environment.

## 7. Reproducing this

```bash
cd <worktree>
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
scripts/ui/chat-live-proof.sh --bin "$PWD/bin/gopi" --out /some/dir/off/root
```

It needs `Xvfb`, `xterm`, `xdotool` and `python3` (all present on this host), a
real `~/.gopi/settings.json` for the model call, and it writes nothing inside the
repository unless `--out` says so (Rule 22: the scratch root must be off `/`).
