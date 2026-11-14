<!-- doc-class: historical -->
# Handoff — the chat surface: self-observation, the eye/brain distinction, and what is in flight

Date: 2026-09-26 (evening, +08:00)
Repo: /home/user/work/GoPi @ master **79807932** (clean tree; no branch of this session's own)
Session: `8c836970-3ce1-4fc3-9166-f93341b54f5c`
  jsonl  `~/.gopi/sessions/8c836970-3ce1-4fc3-9166-f93341b54f5c/session.jsonl`
  raw    `~/.../8c836970-3ce1-4fc3-9166-f93341b54f5c.raw.log`
Operator surface: `gopi chat --tools` on **pts/2** (pid 3580304 at session start).

This session produced NO commits. Its deliverable is **four task rows** (five submitted,
one cancelled as superseded) plus two memory records. Everything below is measured, not recalled.

---

## 1. THE ONE IDEA THAT MATTERS

**The dump is not an EYE, it is the BRAIN** (operator's words: "it is not just for display(eye),
it is accuracy(brain)").

An agent that reads back its own output is reading its own MEMORY. A display error can be
re-looked at; a **memory error cannot be noticed** — every later inference rests on a false
premise. So the *fidelity of the self-observation channel is a correctness property*, not a
UX preference. This reframing is the reason the items in §3 are bug-class, not polish.

Corollary the operator also demanded: **close the loop**. Dump text → read back → compare →
self-correct. "this self-dump is essential for this chat feature!!!"

---

## 2. WHAT IS ALREADY TRUE (do not rebuild)

### 2a. The authoritative transcript is LOSSLESS — use it, not a screenshot

```bash
gopi log <session-id> --response [--text|--json]   # the agent's own answer, byte-exact
gopi log <session-id> --json                        # raw JSONL for programmatic diff
gopi log --last --chat                              # human view
# authority on disk: <session-dir>/session.jsonl  → assistant text blocks
```

MEASURED this session: the sentence
`证明 Doubao 说对了一半 —— 但关键那一半是错的，代码可以证明`
is present **byte-exact** in the assistant text block (`SENT in session_text == True`,
23276 chars total, no loss). The operator's own `/tmp/dump_text.txt` — copied off the
screen — is **corrupted** (lost `D`, a space, and the trailing `证明`).

**Therefore: never feed the agent a mouse-selected screen copy.** The operator named this
himself ("I can use mouse cursor to choose and click to copy screenshot, you should have a
better way to do so"). The better way already existed; it just was not being used.

### 2b. The self-observation architecture exists and is right

- `gopi chat --dump-screen[=PATH]` / `gopi ui --dump-screen` → an interpreted per-cell grid
  (`#gopi-chat-dump` / `#gopi-ui-dump` record) plus a `.json` twin.
- Reader: `gopi ui_state [--pid N] [--target ui|chat] [--json|--list]` — exit 0 read /
  1 none-or-bad-flag / 2 ambiguous. Zero pixels, zero vision, zero cross-process.
- `internal/lineedit/screendump.go` (Screen.Dump/Text/JSON) + `internal/uiclient/dump.go`
  (ScreenDumper, an io.Writer teeing the ONE bubbletea paint stream).
- Per-cell SGR exists (`internal/lineedit/screenattr.go`: Attr/StyledLine/StyledContents) —
  colour/bold ARE assertable. The old "SGR discarded" claim is STALE.

Background: memory #5045 (text-UI is perceived as text), #5068 (the eyes landed; the gap was
self-knowledge + closure), #5914 (this session's distillation).

### 2c. Gotcha — a running surface only dumps if it was STARTED with the flag

`gopi ui_state --list` found **0 dumps** during this session because the operator's
`gopi chat --tools` (pts/2) had no `--dump-screen`. To observe a LIVE surface, restart it as
`gopi chat --tools --dump-screen`. Exit 1 is the honest "no dump", never silent emptiness.

---

## 3. IN FLIGHT — four task rows (all claimed by workers at ~16:45, 2026-09-26)

| task_id | tags | status | content |
|---|---|---|---|
| `01a0dcc4-7967-7cce-8412-78e86983ab4a` | bug,ui,lineedit,renderer | **in_progress** (peerbox:3582036) | `**bold**` silently fails |
| `01a0dcd1-26f1-7830-af19-420a6bf6710a` | design,ui,status-row | **in_progress** (peerbox:3582552) | drop the money field; ctx shows absolute used/max |
| `01a0dce3-1591-7711-ae36-14a2bb5da9f5` | bug,self-improvement,ui,lineedit,self-observation | **in_progress** (peerbox:3582281) | the dump's grid geometry can be WRONG → fabricated corruption |
| `01a0dcd4-5fac-754a-a5ee-2eb3e61243c6` | ux,ui,palette,design | pending | the default must not look like a stock console (missing 256-colour tier) |
| ~~`01a0dcd1-50ff-724a-90b5-244c2f07815d`~~ | — | **cancelled** | superseded by `01a0dcd4` (payload is immutable; cancel + re-submit is the only edit path) |

### 3a. `**bold**` intermittently missing — ROOT CAUSE (measured)

- `internal/lineedit/answer.go:1267` `AnswerRenderer.rowFor` gates the whole span/bold path:
  `if ls != LinePlain || !hasBoldMarker(line) { … }` — a non-plain line drops the emphasis.
- The style comes from `LineComposer.StyleFor` (`compose.go:86`), a **cross-line stateful**
  classifier: `if isFence(trimmed) { c.inFence = !c.inFence }` — **unconditional**. `isFence`
  is `HasPrefix(trimmed, "```")`, so ONE unpaired fence line flips the state for the rest of
  the answer, and every following prose line is classified `LineCode` → bold lost.
- MEASURED probe: `StyleFor("prose")=0` → `StyleFor("```")=1` → `StyleFor("**bold title**")=1`
  (carries a bold marker, classified LineCode — the bug) → subsequent prose all `=1`.
- The intermittency is the signature: balanced fences → bold works; unpaired → it dies silently.
- **The emphasis layer itself is CORRECT** (`TestStyledLineMarksExactlyTheBoldCells` PASS,
  `bold cells [4,7] of 13`) — do NOT touch `emphasis.go`. The gate is one level above it.
- Fix must preserve: inside a real fence, `**` IS literal. Must not break
  `palette_test.go:385`'s classification table. Verify on BOTH `gopi chat` and `gopi ui`
  (`internal/lineedit` is shared; the ui path is `uiclient/model.go:860`).
- LESSON: "the renderer's core is tested green" ≠ "the screen shows bold" — the test covered
  the callee, not the CALL CONDITION.

### 3b. Status row — the money field and the context percentage

The operator's rationale (product argument, verbatim): *"You cannot just display a
'·~0.00元'. this is a fooling, and user is not a fool!"* and *"the '6%' is a percentage such
that you know the backend LLM spec. say 1M for context window, what if the model changes?"*

Code agrees with him:
- The money field is a function of (model generation × Beijing civil time × an offline price
  page): `internal/pricing/pricing.go` has `dsFlashIdle={1,0.02,4}` vs `dsFlashPeak={2,0.04,8}`,
  peak windows 09-12/14-18 Beijing, and three RETIRED cards; `CostOf(model,hit,miss,out,at)`
  takes a timestamp purely to pick the regime. It renders `~0.00元` at real magnitudes —
  uncertain AND below display precision.
- `6%` = `used*100/ContextWindow` (`chat_stream.go:1386`), and `ContextWindow` is a CONFIG
  (`internal/agent/loop.go:1984`, default 200000). The same `6%` means 12k here, while Hermes'
  `44%` means 439K of a 1M window. `statusCtxBase` ALREADY renders the honest `123.4k/200k 61%`;
  `statusCtxField` drops it because the sub-cell is 9 cells (`statusCtxCol=9`).
- Reference the operator supplied (Hermes' bar): `⚕ deepseek-chat │ 439K/1M │ [████░░░░░░] 44% │
  📦438.1K/550(99.87%) │ ↤21.9M │ 📞54 │ 10h 57m │ ⏲ 2m 14s │ ✓ 35s` — every segment is a
  directly-measured transport fact, and a mature product shows **no money at all**.
- Row geometry at the pin: `statusLayoutBudgetCols=100`; the metrics group is ONE column
  (`statusCtxCol 9 + statusCacheCol 7 + statusCostCol 7 + 2 separators`). Freeing the money
  sub-cell frees a DEFINED amount — state it; do not leave a reserved-but-undrawn cell.

### 3c. The dump's grid geometry can be WRONG → fabricated corruption — ROOT CAUSE (measured)

REPRO (real pty of KNOWN size 117x26, `stty size`):
```
gopi chat --tools --dump-screen=/tmp/pty_dump.txt   (driven via python pty.fork)
→ #gopi-chat-dump … terminal=0x0 model=0x0 grid=80x24 … frames=12
```
- `frames=12` proves the editor PAINTED; the grid is still 80x24 while the real pty is 117x26.
- Effect: the paint stream is re-interpreted at a width the layout never used → correct wraps
  read as truncation, layout bytes can be stranded → **corruption the agent cannot
  distinguish from real corruption**. This is the §1 "brain" failure made concrete.
- ROOT CAUSE: `cmd/agent/cli_chat.go:481` `chatTerminalGeometry(os.Stderr)` →
  `terminalGeometry` (`cli_ui.go:515`) does `IoctlGetWinsize(fd(out), TIOCGWINSZ)` and
  **returns 0,0 on failure** ("degrades to the model's default"), recorded only under
  `GOPI_DEBUG`. The dump's own record still says `terminal=0x0` with no statement that the
  grid is a FALLBACK. `chat_dump.go`'s comment claims layout and dump geometry are "the same
  number here BY CONSTRUCTION" — **measured false**.
- Required: the dump's grid must equal the geometry the LAYOUT actually drew into (one source
  of truth); a fallback must be LOUD in the artifact; make the transcript-vs-painted compare
  a first-class verb (ride the existing CLI — Rule 3, no new tool); a regression test at a
  KNOWN pty width asserting `grid==cols` and a CJK+latin line round-trips byte-exact.

### 3d. The palette: the default must not look like a stock console

Operator (product argument, verbatim): *"any color is better than default color!!! … if your
display interface has no difference from default console, people would think you spend no
development effort, or your product is very coarse or low … change some color, make it
eye-catching!"*

MEASURED gap — the palette layer is BINARY while terminals are THREE-TIERED:
| terminal | signal | tier | today |
|---|---|---|---|
| truecolor | `COLORTERM=truecolor` | `38;2;r;g;b` | gold (and only if `GOPI_PALETTE=hermes`; default is still basic) |
| **256-colour** (host: `TERM=xterm-256color`, no `COLORTERM`) | `TERM=*256color*` | **`38;5;n`** | **basic 8-colour = the stock-console look** |
| 8-colour / dumb | neither | 16-colour | basic (correct) |

`internal/sgr` ALREADY supports the middle tier (`ColorModePalette=5`, `Palette256=256`,
`extendedColorParams` sgr.go:540-561) but **`internal/lineedit` never references it**
(measured: zero non-test hits for `Palette256`/`ColorModePalette`/`38;5`). Also: an unknown
`GOPI_PALETTE` name IS reported on stderr (Rule 15) while the CAPABILITY downgrade is silent —
an asymmetry.

---

## 4. OPEN THREADS / NOT DONE

- **Nobody has yet proven WHERE the operator's `/tmp/dump_text.txt` corruption happened.**
  It has NO `#gopi-*-dump` header (grepped, 0 hits), so it is not a real dump artifact — it is
  a screen copy. The three-channel experiment (authoritative transcript vs a REAL
  `--dump-screen` at correct geometry vs the screen copy) is the way to settle it; §3c may be
  the whole story once the geometry is fixed. Do not assert a cause before running it.
- The three `in_progress` rows are owned by workers; check `gopi task show <id>` for their
  reports before starting anything adjacent (avoid duplicated work).
- Memory records from this session: **#5911** (the bold/fence root cause), **#5912** (the
  operator's UI-design criteria: show only deterministic measurable quantities; a percentage
  needs its denominator), **#5913** (the default-palette product argument + the missing 256
  tier), **#5914** (self-dump = brain; read back via `gopi log`/`session.jsonl`, not a
  screenshot).

## 5. IF YOU RESUME HERE

1. Read §1 first — it decides what counts as a bug.
2. `gopi task list` and `gopi task show` the four rows above; three are in flight.
3. To observe a live chat surface: restart it with `--dump-screen`, then
   `gopi ui_state --target chat`. To read back your OWN output: `gopi log <id> --response`.
4. Do not re-derive §3a/§3c — the root causes are anchored to file:line and measured probes.
