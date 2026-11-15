<!-- doc-class: normative -->
# Feasibility: the CLI as the model's eyes and hands (self-development of the terminal UI)

**Study, no production code changed.** Base: `5ca75219` (master, 2026-09-20). Worktree
`~/workspace/gopi-cli-eyes-hands`, branch `feat/cli-eyes-hands-feasibility`.

Labels used throughout:
- **MEASURED** — read or run in this repo/checkout, with `file:line` or pasted output.
- **CITED** — an external source, URL fetched (route named when it failed).
- **ESTIMATE** — my inference; the weakest link is named.

---

## 0. Decision summary (one screen)

**Verdict: partly closed already; the gap is not "the model cannot see the UI" — it is
that no *product* system includes the UI-state channel after `--once` lands. Ship the
next step as a skill/recipe over tools the model already uses (`bash` + a pty session),
not as a new tool.**

| Question | Answer | Evidence |
|---|---|---|
| Is the readable model real? | **Yes, and stronger than the earlier study said.** The vt100 grid now records **SGR per cell** (`Screen.Cell/Attr/StyledLine/StyledContents`) — "is this cell red" is assertable as data. | `internal/lineedit/screenattr.go:103,120,157,175`; `internal/sgr/sgr.go:234`; commit `4f56ccd2` is an ancestor of master (MEASURED) |
| Is driving + reading a real pty closed? | **Yes — on a branch, as a test/study script.** `scripts/ui/drive_ui.py` forks a pty, replays bytes through a screen model, and waits on a **marker** (`idle · turns N`) rather than the clock. | `scripts/ui/drive_ui.py:125` (`pty.fork`), `:66` (`STATUS_RE`), `:95-105` (step markers) on `hp/feat/gopi-cli-bubbletea` (MEASURED) |
| What is the single missing link? | **Model-initiated observation with a settled-state predicate** — a *state channel*. Today the only readiness signal is an **echo of the prompt text**; there is no machine-readable mode/drawer line a driver can wait on, and no script the model can invoke at the moment it chooses to look. | `testsuite/chat_pty_test.go:52-58` (the wait marker is the literal prompt text `you>`); no `--once` on master (`grep --once cmd/agent/cli_chat.go cmd/agent/cli_term.go` → empty) |
| Smallest tool surface? | **Zero new tools.** `bash` (+ a recorded pty session, or `tmux` if the operator allows the attach model) covers read/send/act; `vision_analyze` already reads a PNG. The one genuinely missing *artifact* is a **settled-state contract**, not a tool slot. | 17-tool budget `GOPI.md:87`, pinned by `TestGOPIRule3_MatchesClaudeToolDefs` (`internal/agent/gopidoc_sync_test.go:54`) |
| Does the text-readable model suffice? | **For layout: yes. For colour: yes now (per-cell SGR). For *fidelity*: do not use vision on a screen when text is available** — measured 80% character agreement on a dense 100-col row, and one call returned **empty content** with no error. | MEASURED below §4 (three real `gopi vision_analyze` calls) |
| Adoption risk | **Severe, and measured.** In the 400 most recent sessions (54,511 tool_use blocks): `bash` 32,758 (273 sessions), `screen_capture` 7 (5), `vision_analyze` 13 (4), **`text2img` 0**, and **`tmux`/`drive_ui`/`TIOCSWINSZ` appear in 0 bash calls**. | MEASURED §5 |
| Cost | A screen read as text ≈ **600 tokens** for a 100×24 screen (`EstimateTokens` chars/4, `internal/agent/context.go:353`); the vision route cost 587/192/271 input tokens in three calls **plus** a 250 KB PNG and a page-truncation failure. | MEASURED §6 |
| Prior art | The **tmux `send-keys`/`capture-pane` + poll-for-ready-marker** pattern is the standard harness answer for TUIs; the **structured-state-not-pixels** doctrine is standard for browsers (accessibility tree). Model-initiated TUI observation *as a product tool*: **thin — I found none.** | CITED §7 |

**Recommendation (detail in §8):** after `gopi cli "prompt" --once` lands, add **the state
channel** (one machine-readable line: mode + what is open + `idle|busy`), then ship a
**scripted observation recipe** (`scripts/ui/observe.sh` + a devdoc) whose driver lives
outside the CLI. Promote a tool only if models measurably reach for it — that is Rule 11
evidence, and today the honest baseline is **0 bash calls mentioning `tmux`/`drive_ui` in
400 sessions**.

---

## 1. What is already closed (anchors)

### 1.1 The readable screen model — closed, and better than recorded

**MEASURED** (`internal/lineedit/`, package `lineedit`; all lines verified in this worktree):

| Capability | Anchor |
|---|---|
| `Feed([]byte)` interprets CUP/CUU/CUD/CUF/CUB/EL/ED/CR/LF/DECSTBM/SGR | `screen.go:127`, `controlSequence:255`, `setScrollRegion:378` |
| Read the screen as text | `Line(row):411`, `Contents():427`, `Scrollback():394`, `ScrollbackText():397` |
| Autowrap, wide-rune cells, cursor cell | `put:356` (wrap before the rune), `runeCells` `internal/lineedit/render.go:869` |
| Constrain a band's scrolling | `ScrollRegion():389`; region semantics in `newline():298` |
| **SGR recorded PER CELL** | `screenattr.go:103` `Cell(row,col) Cell`, `:120` `Attr`, `:157` `StyledLine`, `:175` `StyledContents`; `sgr.Style` at `internal/sgr/sgr.go:234` (Bold/Dim/Italic/Underline/Reverse, palette or 24-bit fg/bg) |
| Tests | `go test ./internal/lineedit/ -v` → **88 `--- PASS`** (MEASURED, this worktree) |

The earlier study's own careful qualification —

> "the cell grid stores RUNES ONLY — SGR is parsed and DISCARDED (`screen.go:34-36` …). So
> colour is INVISIBLE to the agent until you add per-cell attribute recording."

— is **now stale as a statement about master** (`devdoc/cli-ui-feasibility-study.md:196-203`,
written at `e3195ca6`; its §5.2 V1 row, `:253`, is the change that made it stale). Commit **`4f56ccd2` "feat(lineedit): record per-cell SGR attributes
in the vt100 cell grid" (2026-09-20)** is an ancestor of HEAD (`git merge-base --is-ancestor
4f56ccd2 HEAD` → true). **MEASURED** consequence: the CLI's one structural advantage
(agent-assertable state) now extends to colour/emphasis, and the report's V1 item
(`devdoc/cli-ui-feasibility-study.md` §5.2) is **done**.

Two other items in that study's defect list are also **closed on master**, so this report
does not re-list them as gaps:

- **input clipping**: `editorRowsCap` is now a cosmetic ceiling **12** and the budget is
  derived from terminal height — `internal/lineedit/region.go:62` (constant) +
  `region.go:525` `EditorRowsCap()` / `region.go:542` `editorRowsBudget` (`:549-550` clamp),
  commit `882cb1ec`.
- **streamed answer wrapping**: still the fallback path's flush-at-newline at
  `cmd/agent/chat_stream.go:303-315` (no width consulted) — **NOT** closed; the reserved-layout
  band is the wrapping path — asserted at `internal/lineedit/answer_test.go` (`a5Cols` band replay) ("a line wider
than the terminal must wrap, not truncate"). This matters to §4: a
  *wrapped* answer is only assertable if the band is the renderer in use.

### 1.2 The pty driver — closed as a script, on a branch

**MEASURED** on `hp/feat/gopi-cli-bubbletea` (read-only, fetched to `refs/hpremote/…`;
not merged into master — `git ls-tree` of master has no `scripts/ui/`):

- `scripts/ui/drive_ui.py` (**289 lines**): `pty.fork()` `:125`, fixed geometry
  `ROWS=200 COLS=100` `:52-53`, `TIOCSWINSZ` `:55-57`, non-blocking `select.select` `:176`,
  screen reconstruction `screen_lines/screen_text/screen_tail` `:154-166`, keystrokes
  `ENTER=b"\r"` / `CTRL_X=b"\x18"` / `CTRL_C=b"\x03"` `:61-63`, **marker-gated steps**
  (`Step(label, marker, send, timeout, delay)`) `:78`, the session table `:95-105`, write
  `:228`, timeout → SIGKILL + exit 2 `:171-175`.
- `scripts/ui/render_transcript.py` (**174 lines**): a minimal screen model
  (`Screen:26`, `feed:49`, `lines:123`) so the raw ANSI capture is evidence, not escape soup.
- `internal/uiclient/model.go` (**685 lines**): the surface's state as testable Go —
  `View():502`, `statusLine():560` (`"idle"` / `"streaming…"` / `"stopping…"` +
  `turns N` + `tool events M`), `allLines():577`, `ScreenWriter` frame counter `:63-84`.
- `internal/uiclient/run.go` (**94 lines**): `Stats{RenderFrames, RenderBytes, TurnFrames, Turns}` `:38-52`.
- Tests: `internal/uiclient/client_test.go` (**923 lines**, 37 `Test…` functions, MEASURED
  by `grep -c 'func Test'`).
- Live evidence, quoted in the branch's own report (`devdoc/cli-bubbletea-carrier.md`):
  7/7 driver steps twice, `ui-session.raw` 6390 bytes/31 frames and `ui-session2.raw`
  6920 bytes/35 frames (frame marker `ESC[22A`), and a real tool call rendered in the pty.
  *(CITED-from-repo: the raw files themselves live on the HP working tree; I did not have
  them locally — see §9.)*

**What this proves:** a pty can be driven, the screen can be reconstructed from the bytes
without a terminal emulator dependency, and a **marker** — not a sleep — decides when to
type. That is the hard part of the driver, and it is done.

### 1.3 The test-only harness (in-repo, no pty-less gap)

**MEASURED** `testsuite/chat_pty_test.go` (341 lines): `startPTYChat` `:92` execs the gopi
binary (`exec.Command(testutil.GopiBin(t), args…)` `:100`), `Setsid+Setctty` `:105`,
`drain` `:119`, `write` `:134`, `writeGapped` `:142`, `writeBurst` `:151`, `waitFor` `:160`,
`close` `:182`, `resize` (TIOCSWINSZ + SIGWINCH) `:211`; bounds are named constants
`ptyPromptTimeout=60s`, `ptyTypeGap=300ms`, `ptyPostBurstGap=1s`, `ptyResizeCols/Rows=100/30`
(`:17-50`). The readiness marker is the **prompt text** (`chatPromptMark = "you>"`,
`:52-58`). This is production-quality pty plumbing — but it is **test scaffolding**, not a
model-reachable surface.

### 1.4 Capture and actuation — closed as verbs/tools, and their adoption is the warning

- `screen_capture` is an LLM tool (the "eye" of the computer-use loop) —
  `internal/toolnames` `ToolScreenCapture`, description `internal/agent/loop.go:1382`;
  CLAUDE-parity list `GOPI.md:87`, `claudeToolDefs` `internal/agent/loop.go:856`.
- `text2img` is an LLM tool that renders text to PNG with a zero-dep TrueType rasterizer —
  description `internal/agent/loop.go:1339`.
- `vision_analyze` is an LLM tool that base64s an image into one standalone vision request —
  `internal/tools/vision.go:34` (32 MiB cap), usage line `:132`, default model
  `deepseek-v4-flash-vision-exp` `internal/deepseek/claude_client.go:132`.
- `gopi act` (XTEST pointer/keyboard) and `gopi shot --self` (CDP screenshot of the web chat)
  are **verbs, not tools** — deliberately: `cmd/agent/cli_act.go:10-14` cites Rule 3 and
  requires Rule 11 adoption evidence before a tool slot; the verbs are
  `CliVerbAct` `cmd/agent/cli_act.go:38`, `CliVerbShot` `cmd/agent/cli_verbs.go:126`,
  `CliVerbScreenCapture` `cmd/agent/cli_verbs.go:132`.

**The interesting part is the adoption record (§5): the eye already exists and the model
does not use it.**

---

## 2. The single missing link

> **Missing: a settled-state contract the model can ask for *at a moment it chooses*.**
> Everything else (drive, read, reconstruct) is closed; what is missing is that "the UI is
> settled, and here is what is open" is currently only inferable from an **echo of the
> prompt string**.

Concretely, today:

1. **No non-interactive turn.** `gopi cli "prompt" --once` does not exist on master
   (MEASURED: `grep -n 'once' cmd/agent/cli_chat.go cmd/agent/cli_term.go` → no `--once`;
   the verb boots the vendored text browser via `cmd/agent/cli_term.go`). Task
   `01a0be86-be69-…` ("gopi cli \"prompt\" --once — the non-interactive single turn") is
   **in_progress** in the ledger (MEASURED `gopi task list`). Without it, a *turn* is not
   addressable by a script at all.
2. **No machine-readable state.** The only "settled" signal anywhere is a **text marker**:
   `drive_ui.py`'s regex over the *rendered status line*
   (`STATUS_RE = (idle|streaming…|stopping…) · turns (\d+) · tool events (\d+)`, `:66`) and
   the in-repo harness's wait for the literal prompt `"you>"`
   (`testsuite/chat_pty_test.go:52-58`). Both are **echoes**: a driver can only wait by
   pattern-matching human-facing prose. There is no mode/drawer/keymap line a program can
   parse — this is exactly why `drive_ui.py`'s header documents two failed first attempts
   (`:5-27`: `\n` instead of `\r`; typing on a timer scrambled the capture).
3. **No script the model reaches for.** The driving recipe exists on a **branch whose
   deliverable is a controlled experiment** (7 fixed steps), not a tool: with its marker
   table hardcoded (`STEPS`, `:95-105`) it drives *this* session, and the model never
   invokes it (0 bash calls in 400 sessions, §5).

**Boundary statement (what "closed" vs "missing" means precisely):** *the driver* drives on
a fixed schedule and waits on a regex I wrote; *the model* would have to choose the moment,
choose what to look at, and decide when to stop. Point 2 is what makes that difference
mechanical: without a state channel the model's look is prose-matching, which is exactly the
fragile part.

---

## 3. Proposed minimal tool surface (existing vs new)

Ordered by cost, with the smallest set first. **All three are wrappers over things that
already exist — nothing here needs new Go code to be *usable*, only to be *ergonomic*.**

| Function | Wrapping | New code needed |
|---|---|---|
| **(a) read the screen** | `tmux capture-pane -p` over a session started by `bash`, **or** the recorded pty output replayed through the existing screen model (`scripts/ui/render_transcript.py:26/49/123`, MEASURED — same directory as the driver on the HP branch). For the *web* surface, `gopi shot --self` + `vision_analyze` already exist. | **none** to start; a `gopi ui screen <session>` verb would be convenience only |
| **(b) send input** | `tmux send-keys` (or the pty write path `drive_ui.py:228` / `ptySession.write` `testsuite/chat_pty_test.go:134`) through `bash`. Key semantics are already known: Enter is `\r` (`drive_ui.py:61`), interrupt `\x18`, quit `\x03`. | **none** |
| **(c) wait for a settled state** | Today: a `timeout`-bounded poll on a text marker (the tmux pattern, CITED §7). Honest form: `timeout N bash -c 'until tmux capture-pane -p -t S \| grep -q "<marker>"; do sleep 0.2; done'` — bounded by `bash`'s own 120 s absolute timeout (`internal/tools/bash.go:38`). | **the state channel is the only real new work** — see §8 |

**Why not a new tool now:** the surface is 17 tools, the count is deliberately defended
(`GOPI.md:87` + `TestGOPIRule3_MatchesClaudeToolDefs`), and this repo has spent a whole
dispatch on exactly this decision before: `cmd/agent/cli_act.go:10-14` says actuation ships
as a **verb first**, and a tool slot is promoted **only with Rule 11 adoption evidence**
(citing `devdoc/screen-capture-and-actuation-tools.md` §6). The measured record (§5) says
the missing ingredient is not a slot.

**If a tool is later wanted**, the smallest shape that is *not* a duplicate: one tool
`ui` with `action=screen|send|wait` — each action delegating to a verb (`gopi ui …`) so the
CLI path and the model path cannot drift (the same "one code path" seam as
`internal/agent/loop.go:1739` for `screen_capture`). ESTIMATE: ~2-3 days including tests and a prompt
narrative; the expensive part is (c), not (a)/(b).

---

## 4. Is the text-readable model sufficient?

Splitting the claim three ways, because the answer differs per question.

### 4.1 Layout — sufficient, decisively

**MEASURED (this worktree):** `Screen.Line/Contents/StyledContents` return strings; the
render harness replays the editor's own bytes and asserts exact rows and cursor cells
(`internal/lineedit/render_test.go:16` `renderScreen`, `:30` `TestLineEditorRender`, plus
`TestLineEditorRenderWraps:146`, `TestLineEditorScreenGrid:238`) alongside 12
per-cell-attribute tests (`internal/lineedit/screenattr_test.go:34,73,145,176,291,313`);
**88 `--- PASS`** for the package. The
autowrap rule is implemented (`screen.go:356-375`), and the wrap point is pinned by tests
(`internal/lineedit/render_test.go:146`). **No pty, no browser, no pixels are needed to assert a layout.**

### 4.2 Style — sufficient **now** (the earlier caveat is stale)

Per-cell SGR is recorded and exposed (`screenattr.go:103/120/157/175`), with tests asserting
"which cell is which style" (`internal/lineedit/screenattr_test.go:34` records, `:73` round-trips
through `Feed`, `:145` extended colours, `:176` wide runes, `:291` erase keeps the pen, `:313`
scroll keeps styles). Additionally, the
**standard external tool agrees**: MEASURED on this host —
`tmux capture-pane -p -e` renders a red run as `^[[31mREDLINE^[[39m plain`, i.e. per-run SGR
tokens a driver can assert on without any emulator of our own. Caveat to keep honest: colour
is only assertable **if the app emits SGR**; master's answer path is still monochrome (the
palette commit `a975270c` is on `hp/feat/chat-colour-palette`, **not** an ancestor of master
— MEASURED). So the *capability* to assert colour exists; the *product* has nothing to
assert yet.

### 4.3 Fidelity — vision on a screen is demonstrably worse than reading the text

Three real calls, **MEASURED** (this host, `./bin/gopi vision_analyze`, model
`deepseek-flash`):

| # | Input | Reported cost | Result |
|---|---|---|---|
| 1 | `text2img` render of a 100×24 random-letter screen — **2 pages** emitted for one screen (`screen100x24-1.png` 249,662 bytes, `-2.png`) | 输入 587 tokens | **empty content, exit 0** — no text, no error (silent truncation) |
| 2 | same PNG, default prompt | 192 in / 980 out | partial transcription; the model states it converted only the opening ("以下是图片开头部分的内容提取") and stopped |
| 3 | a 21-line chat-like screen rendered at 60 cols (`chatlike.png`, 39,124 bytes) | 271 in / 273 out | asked for the last 5 lines: **byte-exact** (`7 / 8 / 9 / 10 / idle - turns 2 - tool events 2`) |

And a character-level check against ground truth (MEASURED, `python3` diff of call 2's
extract vs the source row): **80 of 100 characters matched (80 %)**, the 100-char row came
back as **109 characters**, first divergence at **column 0**. The vision model even
rationalised the content as "无规律的随机字符 … 乱码" — it could not tell a terminal screen
from noise.

ESTIMATE (weakest link: n=3 calls, one model): vision is **usable for "is it roughly right /
what does the last line say"** and **unusable as ground truth**; the text model is ground
truth by construction. The cost direction is the same (a page PNG cost 250 KB and *two*
pages for one screen; see §6).

**Conclusion:** the text-readable model suffices for everything the operator asked for
(layout, and now colour), and the pixel route is strictly worse *for a terminal UI*. The
pixel route stays right for the **web** surface (`gopi shot` + `vision_analyze`), where no
text model exists.

---

## 5. Adoption risk (the honest section)

**MEASURED, this host, method: walk the 400 most recent `~/.gopi/sessions/*/*.jsonl` by
mtime, count `tool_use` blocks and, separately, count bash calls whose input string contains
each pattern.** (`~/.gopi/sessions` = 6.8 GB, 3485 jsonl files; 400 sessions sampled;
54,511 tool_use blocks.)

| tool | calls | sessions |
|---|---|---|
| `bash` | **32,758** | 273 |
| `job` | 5,934 | 183 |
| `edit` | 4,161 | 224 |
| `task` | 4,032 | 262 |
| `read` | 3,644 | 250 |
| `memory` | 2,011 | 271 |
| `write` | 1,012 | 240 |
| `code_search` | 274 | 102 |
| `context_trim` | 260 | 181 |
| `keyword_search` | 160 | 90 |
| `task_submit` | 157 | 65 |
| `web_fetch` | 49 | 2 |
| `web_search` | 20 | 2 |
| `vision_analyze` | **13** | 4 |
| `git_blame` | 12 | 11 |
| `screen_capture` | **7** | 5 |
| `text2img` | **0** | 0 |

Bash-input patterns, same 400 sessions:

| pattern in the bash command | calls | sessions |
|---|---|---|
| `pty` (any mention) | 429 | 97 |
| `script` | 154 | 49 |
| `gopi chat` | 10 | 2 |
| `screen_capture` | 9 | 6 |
| `gopi web` | 6 | 3 |
| `gopi shot` | 3 | 2 |
| `gopi act` | 3 | 2 |
| `gopi cli` | 1 | 1 |
| **`tmux`** | **0** | **0** |
| **`drive_ui`** | **0** | **0** |
| `TIOCSWINSZ` / `vt100` / `lineedit` / `libtmux` | **0** | **0** |

Notes for honesty: the briefing's baseline (`git_blame` 4/400, `bash` 31,471) is **not**
reproduced by my window (`git_blame` 12/400, `bash` 32,758) — different sessions sampled;
the *conclusion* is identical (a 3-order-of-magnitude gap between the general tool and the
specialised one). `web_fetch` is 49 here, not 0 — again a window artefact, and small either
way.

**Read this as the design input it is:** the model reaches for `bash` 32,758 times and for a
specialised observation surface ~20 times. Two mechanisms explain it, and both are
reproducible in prior art (§7):

1. **`bash` is a general-purpose outlet.** `tmux send-keys/capture-pane` (CITED §7) is a
   *recipe*, not a tool, and it rides the outlet the model already uses. A new tool has to
   beat that default, not merely exist.
2. **Structured state beats pixels when both are available.** Playwright MCP's own README:
   "enables LLMs to interact with web pages through structured accessibility snapshots,
   **bypassing the need for screenshots or visually-tuned models** … **No vision models
   needed** … **Deterministic tool application**" (CITED §7). Claude in Chrome's `read_page`
   description: "Get an accessibility tree representation of elements on the page … limited
   to 50000 characters by default … pass a larger max_chars, or use depth/ref_id to focus"
   (CITED-local §7). Our terminal equivalent of the accessibility tree is **`Screen`** —
   already built, already per-cell.

**Verification plan (what would make an adoption claim honest — Rule 11):**
1. Land `--once` (separate dispatch, in flight).
2. Ship the **recipe** (skill-style doc + script) and *only then* measure: N real tasks that
   require looking at the CLI; count sessions whose bash input mentions the recipe path;
   the count must be >0 and the tasks must be unachievable by the model's default route.
3. If a tool is proposed later: run the 2-scenario Rule 11 protocol, keep the session-id +
   tool_use excerpts as evidence, and **compare against the recipe's rate** — if the recipe
   already gets adopted, the tool slot is not earned.

---

## 6. Cost and risk

### 6.1 Cost per observation (rough, with the weak link named)

- **Text path:** a 100×24 screen is 2,400 characters → `EstimateTokens` (chars/4,
  `internal/agent/context.go:353`) ≈ **600 prompt tokens** per look (ESTIMATE; the heuristic
  under-counts CJK, so treat as a lower bound). No API call, no bytes on the wire beyond the
  tool result. This is the cheap shape, and it is the shape the readable model gives us.
- **Vision path (MEASURED):** one screen needed **two PNG pages** (249,662 B + a near-empty
  second page) and one vision call reporting **587 input tokens** — and it returned
  **nothing**. The useful call cost 271 in / 273 out on a 39 KB PNG. Add the render step
  (`text2img` pagination) and the fact that a screenshot of a *live* terminal needs X11 or a
  pty capture first (X11 capture is separately recorded as broken on this host pair:
  `memory #4043`, "peer1 :1 draws black; HP is rootless Xwayland").
- **ESTIMATE:** text ≈ 600 tokens/look, deterministic; screenshot+vision ≈ 1 render + 1 API
  round-trip + a PNG per page, and **fidelity that cannot be trusted for assertions** (§4.3).
  Weakest link: no per-token price is applied here because `vision_analyze` reports usage,
  not 元; the price table lives in `devdoc/deepseek-v4.1-flash-model-naming-and-kv-architecture-20260910.md:41-53`.

### 6.2 Capture-vs-turn timing

- On the branch, the driver solves this: **wait for a marker, then type** (`drive_ui.py:66`,
  `:95-105`), with a per-step `timeout` and SIGKILL on expiry (`:171-175`). This is the
  correct pattern and it is proven (7/7 steps, twice).
- In-repo, the same idea is in the test harness (`waitFor`, `testsuite/chat_pty_test.go:160`).
- **The model-driven case has no script to wait in.** The only *bounded* primitive available
  to the model today is `bash` itself: default 120 s absolute, and a 120 s **idle-output**
  timeout that kills a silent process (`internal/tools/bash.go:38` `bashDefaultTimeoutSec=120`, `:45` `bashNoOutputTimeoutSec=120`, doc `:146-154`).
  A model-initiated "wait for settled" over `bash` therefore works, but it polls a **text
  marker** — i.e. §2's missing link is felt precisely here: the poll target is prose.
- **Timing risk that is real:** a look taken **between** two frames of a diff renderer sees a
  torn screen. The branch's own caution (`drive_ui.py:5-27`) is the evidence: typing on a
  timer interleaved with repaint and produced a scrambled capture that *looked like* a TUI
  bug. Bubble Tea repaints as diffs (the report counts 31/35 full frames via `ESC[22A` for
  four turns); `Screen` models whole-screen state, so a torn read is possible unless the
  observer waits for a settled marker or a full frame. **ESTIMATE:** the cheapest mitigation
  is the state channel (§8), because "settled" then becomes a fact the app publishes rather
  than an inference.

### 6.3 The look-without-fixing loop, and whether today's guards catch it

The operator's named risk. **MEASURED — today's loop detectors do NOT catch it:**

- `maxStuckRounds = 5` (`internal/agent/loop.go:254`) trips on **N consecutive no-tool rounds
  whose text shares a 50-rune prefix** (`stuckTextPrefixChars`, `internal/agent/loop.go:263`; `tripped()`
  `internal/agent/loop.go:1906`). A model that *is* observing emits a tool_use every round → the counter resets.
- `maxStuckToolfulRounds = 10` (`internal/agent/loop.go:321`) trips on **identical tool_use
  fingerprints (name + normalized param hash + text prefix)** (`toolStreakState`, `internal/agent/loop.go:1969`).
  Its own doc says the intent is byte-identical repeats, and it **exempts polling tools**
  when the polled result *changes* (`internal/agent/loop.go:1978-1990`). A UI observation whose result changes
  every round (a live screen) is therefore **explicitly treated as "alive"** — correct for
  `job` polling, blind for a UI loop.
- `MaxIterations` is `0` = unlimited (`internal/agent/loop.go:3508`, `MaxIterations: 0`), so nothing else caps
  round count except `sessionWallclockCap()` (`internal/agent/loop.go:1851`, called at `:2683`).

**So the bound must be created, not found.** The smallest honest ones, in order:

1. **A per-tool-call bound:** `bash`'s 120 s absolute timeout (`internal/tools/bash.go:38`) + 120 s idle-output
timeout (`:45`) already bounds every single look — a loop is bounded *per iteration*, never
   globally.
2. **A per-session budget** on the observation entry point (e.g. "at most N observations per
   session", or "N consecutive observations with an unchanged screen hash") — this is new
   logic wherever it lands (verb or tool). Note the *ingredient* exists: `Screen`'s content
   is a hashable string (`Contents()` `screen.go:427`) — the harness already has the pattern
   in `toolStreakState.lastPollResult` (sha256 of the polled result, `internal/agent/loop.go:2527`).
3. **A human-visible failure**: `notifyLoopStuck` (`internal/agent/loop.go:1920`) is the existing channel to
   make a stuck loop an **event** (`GOPI_TASK_NOTIFY_EVENTS=loop-stuck`) — but it fires only
   on detector 1/2, so an observation loop would need its own trigger.

**ESTIMATE (weakest link: no measurement of a real model in an observation loop exists —
nobody has run it):** a look-without-fixing loop is *likely* (the model can see a defect it
cannot fix, and each look is cheap and informative), and it would be **invisible** to every
guard in the tree today. Any dispatch that ships observation must ship the bound with it.

---

## 7. Prior art (with the routes I actually took)

**Verdict on prior art:** *(a) driving a TUI by sending keys and capturing the pane is
standard practice; (b) "structured state, not pixels" is the mainstream doctrine for
browsers; (c) exposing terminal-UI observation as a **first-class model tool for
self-development** is thin — I found the pieces, never the product.*

| What | Source (fetched today) | What it says |
|---|---|---|
| Driving a TUI from an agent | CITED-local primary source: `~/work/Downloads/claude-code-system-prompts/system-prompts/skill-run-tui-interactive-terminal-app-example.md` | "Interactive terminal apps … **can't be driven directly by an agent's bash tool** — they take over the terminal. The skill must show how to wrap them in `tmux`". The pattern: `tmux new-session -d -s app -x 120 -y 40 './myapp'`; poll `until tmux capture-pane -t app -p \| grep -q "Ready"; do sleep 0.2; done` (**"faster + more reliable than a fixed sleep"**); `tmux send-keys -t app 'Down' 'Down' 'Space'`; `tmux kill-session`. Also documents terminal size, a keybinding table ("this is the 'API' of a TUI"), and `capture-pane -e` for escapes / `-J` to join wrapped lines |
| Same, at the product level | CITED-local: `skill-run-app.md` (the `/run` skill: "Running means launching the actual app and interacting with it"; the shape table maps "TUI / interactive terminal" → "tmux `send-keys` / `capture-pane`"; "Drive it, don't just launch it") | A **skill** (prose + recipe) is the shipment vehicle, not a tool |
| UI work must not be declared done from tests | CITED-local: `system-prompt-frontend-browser-verification.md` | "For UI or frontend changes, start the dev server and **use the feature in a browser** before reporting the task as complete … **if you can't test the UI, say so explicitly rather than claiming success.**" |
| Structured state beats pixels (browser) | CITED: https://raw.githubusercontent.com/microsoft/playwright-mcp/main/README.md (fetched) | "interact with web pages through structured accessibility snapshots, **bypassing the need for screenshots or visually-tuned models**"; "Uses Playwright's accessibility tree, **not pixel-based input**"; "**No vision models needed**, operates purely on structured data"; "**Deterministic tool application**". Also: CLI+SKILLS recommended over MCP for token efficiency in coding agents |
| The terminal analogue in a shipped product | CITED-local: `tool-description-claude-in-chrome-read-page.md` | `read_page` = "an accessibility tree representation of elements on the page … limited to 50000 characters … truncated at a line boundary, with a note giving the full size — pass a larger max_chars, or use depth/ref_id" |
| Tool-tier doctrine (use the DOM, not pixels) | CITED-local: `skill-computer-use-mcp.md` | Tiering: dedicated MCP → **Chrome MCP ("DOM-aware, much faster than clicking pixels")** → computer use. And: "**Look before you assert** … take a screenshot and check before answering. Don't answer from memory" — with terminals/IDEs at tier "**click**" (typing blocked; "For shell commands, use the Bash tool") |
| Per-cell colour from a standard tool | CITED: https://github.com/tomklino/tmux-mcp (README fetched) | An MCP server for reading/driving tmux sessions: "Send Command … Uses **bracketed paste** so accidental execution can't occur", `prompt_verify_string` context check, "Capture Output: Read the last N lines", "**Interactive Detection**: automatically detects when a terminal enters an interactive state (e.g. vim, nano, less) so the agent doesn't need to wait for a time-out", "detects completion and returns immediately" |
| Same idea, official Chrome tooling | CITED: https://raw.githubusercontent.com/ChromeDevTools/chrome-devtools-mcp/main/README.md (fetched) | MCP server giving a coding agent "control and inspect a live Chrome browser … screenshots … console messages … **automatically wait for action results**" |
| Render-truth TUI tests without a pty | CITED-local: `~/work/codex/codex-rs/tui` — `Cargo.toml:164` (`vt100 = { workspace = true }`), `tests/test_backend.rs` (`VT100Backend`), `tests/suite/vt100_history.rs:51` (`scenario.term.backend().vt100().screen().contents()`) | The **same design** as `internal/lineedit` (a cell grid as the assertion surface) — arrived at independently |
| Library-level | CITED: https://pkg.go.dev/github.com/charmbracelet/x/exp/teatest (fetched) | `WaitFor(tb, r, condition)` ("keeps reading until the condition matches", default 1 s / 50 ms), `RequireEqualOutput` golden files, `WithInitialTermSize`, `Type(s)` — the packaged version of our in-process harness |

### Where prior art is **thin** (the explicit statement)

I found **no** project or product that exposes *terminal-UI observation* as a
**model-initiated tool for self-development**. What exists is:

- generic terminal control (`tmux send-keys`/`capture-pane`, `tmux-mcp`) reached **through
  the model's general shell tool** — i.e. the model *chooses* to look, but the surface is a
  recipe, not a tool;
- structured-state observation for the **browser** (Chrome DevTools MCP, Playwright MCP,
  Chrome extension) — a first-class tool, but for the DOM/accessibility tree, not a terminal
  screen;
- per-framework test harnesses (codex's vt100 backend, `teatest`) — render truth for the
  *developer*, never the *model*;
- computer-use/xdotool-class actuation with a screenshot eye, which is the pixel path we
  measured to be strictly worse for text (§4.3).

So the specific triangle — **model-initiated observation of a terminal UI, text-model-backed,
zero-dependency** — appears to have **no product precedent**. That is not a "we are first"
claim (route failure is recorded in §9); it is the honest finding that the *pieces* are
mainstream and the *assembly* is not.

---

## 8. Recommendation and the smallest next step

**Recommendation: ship the state channel before the tool.**

After `gopi cli "prompt" --once` (already dispatched: ledger row
`01a0be86-be69-7495-b891-72da0ab71410`, `in_progress` at the time of this study), the
smallest useful step is:

> **Make "settled" and "what is open" machine-readable on the CLI surface** — one line with
> a stable vocabulary (`idle|busy|error`, the open drawer/menu/overlay, the modality), so a
> driver — a script the model runs through `bash`, or the next `--once` call — has a
> predicate that is *not* prose. Everything else already exists: `Screen` (text + per-cell
> SGR) to read what was drawn, `bash` to drive, `vision_analyze` for pixels when there is no
> text model.

Why this and not a tool:

1. **It is the one thing that is missing by measurement, not by opinion** (§2): the driver's
   wait is a regex over human text; the test harness waits for `"you>"`.
2. **It is cheap and testable in-tree.** The surface already publishes a status string
   (`internal/uiclient/model.go:560` — branch-only) and the CLI already has a status row
   (`cmd/agent/chat_stream.go:161-204`, `lineedit.Status`). A machine-readable variant is a
   format decision plus tests asserting the exact line — the same render-truth discipline as
   `internal/lineedit/render_test.go`.
3. **It is the precondition for the tool being adopted** (§5): structured state is what makes
   a look *deterministic* (the Playwright MCP doctrine), and determinism is what makes a
   model reach for it. It also removes the torn-read risk (§6.2).
4. **It respects the tool budget and the repo's own precedent** (`cmd/agent/cli_act.go:10-14`:
   verb first, tool only with adoption evidence).

**Then, in order:** (i) ship the observation *recipe* (script + devdoc, tmux or pty — the
operator's choice of attach model, `memory #4043` records that tmux was absent on peer1 at
one point and is present now, MEASURED `tmux 3.2a`); (ii) measure adoption on real UI tasks
(Rule 11 protocol, §5); (iii) only if the recipe is not adopted *and* the tasks are
observably harder without a tool, promote `gopi ui …` to a tool — with the per-session
observation bound (§6.3) in the same dispatch.

**What I recommend against:** (a) screenshot+vision as the primary terminal-UI eye (measured
worse: 80 % character agreement, one silent empty result, two PNG pages per screen, §4.3);
(b) buying a TUI toolkit to get this — the Bubble Tea POC's own honest cost list is **+1
direct / +15 indirect modules** (`devdoc/cli-bubbletea-carrier.md`), while the zero-dep
carrier's model already gives an assertable screen; (c) enumerating a second chat engine —
`gopi cli --once` must go through the existing `POST /api/chat` path.

---

## 9. What I could not determine (and which route failed)

1. **The HP branch's live state.** I read `hp/feat/gopi-cli-bubbletea` read-only by
   `git fetch` (its tip `145738c1`); I did not create a worktree on HP, and the evidence files
   the branch's report quotes (`ui-session2.raw`) are on an HP working tree I could not
   locate from here (a filesystem-wide `find` timed out). So the pty driver's live numbers
   (§1.2) are **quoted from the branch's own report**, not re-measured by me.
2. **No end-to-end run of `drive_ui.py` from this host.** It needs the Bubble Tea carrier
   built (16 modules); the module cache has no `charmbracelet/*` and the box's module route
   was not exercised for this study. The driver's *design* is read line by line;
   its *runtime* is not re-verified here.
3. **Academic prior art: route failure.** The arxiv method returned `HTTP 406 Not Acceptable`,
   and the merged google route timed out; bing returned Chinese-language generalities. So the
   "no precedent" finding in §7 is a **web/github-docs finding**, not a literature survey —
   treat the absence as "not found via the routes I could use".
4. **Adoption of a future tool is unmeasurable now** — the tool does not exist. §5 gives the
   protocol, not a result. Any claim that "the model will use it" would be unfounded.
5. **The exact cost of one *observation* in a real session** is an ESTIMATE: no session exists
   in which a model observed the UI through a bounded surface, so the 600-token/look figure is
   derived (chars/4 over one screen), not observed.
6. **`--once` state** is a moving target: the ledger row is `in_progress` (MEASURED at
   `t+451s`), so §2's "missing" item may be closed by the time this is read — that would
   *strengthen* the recommendation (it is the stated precondition), not weaken it.
7. **tmux presence on HP** — I measured `tmux 3.2a`, `script` present, `expect`/`socat`
   absent on **peer1** only.

---

## 10. Reproduce

```bash
# worktree (this study): ~/workspace/gopi-cli-eyes-hands, branch feat/cli-eyes-hands-feasibility
go test ./internal/lineedit/ -v | grep -c '^--- PASS'            # 88
git log -1 --format='%h %ad %s' --date=short 4f56ccd2            # SGR per-cell recording
git merge-base --is-ancestor 4f56ccd2 HEAD && echo ON_MASTER
grep -n 'editorRowsCap = \|func (r \*region) EditorRowsCap' internal/lineedit/region.go
grep -n 'func (s \*Screen) Cell\|func (s \*Screen) Attr\|StyledContents' internal/lineedit/screenattr.go
grep -n 'maxStuckRounds = \|maxStuckToolfulRounds = ' internal/agent/loop.go
grep -n 'func (r \*chatRenderer) write' -A6 cmd/agent/chat_stream.go

# the HP branch (read-only)
git fetch hp feat/gopi-cli-bubbletea:refs/hpremote/gopi-cli-bubbletea
git show refs/hpremote/gopi-cli-bubbletea:scripts/ui/drive_ui.py | sed -n '52,105p'
git show refs/hpremote/gopi-cli-bubbletea:internal/uiclient/model.go | sed -n '502,576p'

# adoption audit (400 most recent sessions, 54,511 tool_use blocks) — §5's numbers
# walks ~/.gopi/sessions/*/*.jsonl by mtime, counting tool_use blocks by name:
python3 - <<'EOF'
import json, collections, glob, os
files = sorted(glob.glob(os.path.expanduser('~/.gopi/sessions/*/*.jsonl')),
               key=os.path.getmtime, reverse=True)[:400]
c, sess = collections.Counter(), collections.defaultdict(set)
for i, f in enumerate(files):
    for line in open(f, errors='replace'):
        if 'tool_use' not in line: continue
        try: o = json.loads(line)
        except Exception: continue
        stack = [o]
        while stack:
            x = stack.pop()
            if isinstance(x, dict):
                if x.get('type') == 'tool_use' and x.get('name'):
                    c[x['name']] += 1; sess[x['name']].add(i)
                stack.extend(x.values())
            elif isinstance(x, list): stack.extend(x)
print('sessions', len(files), 'tool_use blocks', sum(c.values()))
for n, k in c.most_common(20): print(f'{n:16s} calls={k:6d} sessions={len(sess[n])}')
EOF

# the cost/fidelity experiment (§4.3)
gopi text2img /path/screen.txt --size 20 --cols 100 --lines 24
gopi vision_analyze <png> --prompt "Transcribe exactly the last 5 lines, verbatim."

# tmux per-run SGR is readable without any emulator of our own (§4.2)
tmux new-session -d -s sg -x 60 -y 6; tmux send-keys -t sg 'printf "\033[31mREDLINE\033[0m plain\n"' Enter
tmux capture-pane -p -e -t sg | cat -v     # -> ^[[31mREDLINE^[[39m plain
tmux kill-session -t sg
```
