<!-- doc-class: normative -->
# CLI chat UI: feasibility study & development ceiling

Status: **study only — no production code changed.** Every claim below is a
measured fact from this checkout (`git rev-parse --short HEAD` = `e3195ca6`),
with a file:line anchor or a command that reproduces it.

Trigger: operator report (2026-09-19) — *"I can hardly read your full response
because the line is beyond displaying and my typed text is wrapped without
displaying in multi-line; I cannot input Chinese; no highlight colour, no
blocks, no fonts."* This study evaluates how far the current text-mode
approach can be developed, and whether it is worth developing at all.

---

## 0. TL;DR — the verdict up front

| Question | Answer |
|---|---|
| Is the reported breakage real? | **Yes.** Reproduced: input is silently clipped at 4 rows; the answer stream is never wrapped by the app. |
| Is it a layout-math bug? | **No** — the math is correct (CJK width, wrapping, cursor cells all pinned by tests). The breakage is two *deliberate* caps plus one *missing* feature. |
| Can it be fixed? | **Yes**, and the wrap/clip items are cheap (0.5–1.5d each). |
| Is "colour / blocks / fonts / rich layout" reachable? | **Partly.** Colour: yes, cheap. Fonts *inside a terminal*: **impossible** — the terminal owns the font. Blocks: yes. |
| Did the agent's "I can see the layout" claim survive scrutiny? | **Yes, and it is verified below** (§3) — with a caveat on styled output that changes the verdict materially. |
| Is the text-mode approach worth developing further? | **For fixing the reported defects: yes, and it is overdue. As a platform to chase browser-grade UI: no — the ceiling is the terminal protocol, not our code.** |

The honest one-line framing: **the CLI's problem is not that it is primitive; it
is that two caps were set for a *layout* budget and one entire output path was
never wired to the width.** Those are ~3 days of work, not a rewrite.

---

## 1. Reproduction of the operator's report

The editor is driven through its scripted source and replayed into the vt100
cell grid (`Screen`), then the screen is printed as text. Harness used:
`internal/lineedit/scripted_test.go` (`newScriptedEditor`) +
`internal/lineedit/render_test.go:16` (`renderScreen`).

### 1.1 Input is clipped, not wrapped

`internal/lineedit/editor.go:745-752`:

```go
rows := e.val.RowsFor(e.render.prompt, e.width())
if rows > editorRowsCap {   // editorRowsCap = 4  (region.go:57)
    rows = editorRowsCap
}
e.region.SetEditorRows(rows)
```

Measured (100-char single-line input, `SetEditorRows` wish vs. what is drawn):

```
cols= 40 rows= 6 -> nonblank screen rows=3 cursor=(2,24)
cols= 40 rows=10 -> nonblank screen rows=3 cursor=(2,24)
cols= 40 rows=24 -> nonblank screen rows=3 cursor=(2,24)
cols= 80 rows= 6 -> nonblank screen rows=2 cursor=(1,24)
cols= 80 rows=10 -> nonblank screen rows=2 cursor=(1,24)
cols= 80 rows=24 -> nonblank screen rows=2 cursor=(1,24)
cols=200 rows= 6 -> nonblank screen rows=1 cursor=(0,104)
cols=200 rows=10 -> nonblank screen rows=1 cursor=(0,104)
cols=200 rows=24 -> nonblank screen rows=1 cursor=(0,104)
```

Read the last column: the cursor advances to column 104 on a 200-col terminal —
i.e. the *buffer* holds the whole line, but at 40 cols only 3 rows are painted
and **rows 4+ of the user's own typing are gone from the screen**. Note the row
count does not change with terminal height: `editorRowsCap = 4` is an absolute
constant, not derived from `rows`. `region.go:57`.

Latent bug (independent of the report): at `cols=200`, `cursor=(0,104)` — the
cursor column exceeds the 4-row editor's *width-derived* row count, so the
cursor row is under-reported for the wide case. Worth a test when fixing.

**This is the direct cause of "I can't read my own typing."** The editor *has*
a multi-line mechanism (`editor.go:2469` grows to `len(rows)` on submit, with an
explicit comment: *"the display cap above clips long input, so the submitted
line is redrawn uncapped here"*) — so the authors knew about the cap and chose
to lift it only *after* submission. While typing, you cannot see what you typed.

### 1.2 The streamed answer is never wrapped by the application

`cmd/agent/chat_stream.go:303` — the fallback renderer:

```go
func (r *chatRenderer) write(delta string) {
	r.buf.WriteString(delta)
	s := r.buf.String()
	if idx := strings.LastIndexByte(s, '\n'); idx >= 0 {
		fmt.Fprint(r.out, s[:idx+1])   // flush raw — no width consulted
		...
	}
```

`grep -n 'wrap\|Wrap' cmd/agent/chat_stream.go` finds **4 hits, none of them a
wrapping loop**; grep for `Width`/`TIOCGWINSZ` in that file finds nothing. The
renderer emits raw model deltas to `out`. Whatever wrapping you see is the
*terminal's* auto-wrap (which is what makes a long paragraph unfollowable and a
code block unreadable), not the app's.

Contrast: the reserved-layout path (`internal/lineedit/answer.go`) *does* wrap
correctly — `internal/lineedit/answer.go:388` documents it, and
`answer_test.go:859` asserts *"a line wider than the terminal must wrap, not
truncate"*. So wrapping exists in one path and is absent in the other. **This
asymmetry — not a missing algorithm — is the defect.**

### 1.3 Colours / blocks / fonts are *absent*, not broken

`grep -rn 'SGR\|color' cmd/agent/chat_stream.go internal/lineedit/answer.go
internal/lineedit/render.go` → **zero hits.** The only SGR the editor emits is
whatever `render.go` uses for erase/cursor; there is no theme, no role-tinted
text (user vs assistant vs tool), no bold/dim, no reverse-video block. The
answer path is monochrome by construction.

Fonts: **not a code defect and not fixable in code.** A terminal emulator owns
the font; an application selects at most one font *family* by escape sequence
(and most emulators ignore even that). See §4.

### 1.4 CJK: the layout math is already correct — the gap is elsewhere

The original hypothesis ("no multi-language support") is **wrong in the layout
layer and right in the UX layer.** Measured:

* `internal/lineedit/render.go:334-348` (`layoutRows`, `runeCells`) returns **2** for East Asian wide/fullwidth
  runes, 0 for combining marks (`unicode.Mn/Me/Cf`).
* `internal/lineedit/render.go:353-373` `wideRanges` carries the real W/F blocks (Hangul Jamo,
  CJK radicals, Kana, CJK Ext-A, CJK Unified, Yi, Hangul syllables, CJK
  compat, Fullwidth forms, emoji planes, SMP CJK).
* `internal/lineedit/render.go:260-264` — wrap **before** the rune, never splitting a wide char.
* Tests pin it: `value_test.go:86` (delete one wide rune = `"知知"`),
  `internal/lineedit/render_test.go:72-90` (cursor column 9 for wide runes), plus CJK in
  `answer_test.go`.

So printing 中文 is *already correct*. "I cannot input Chinese" is therefore a
different layer: **the editor decodes UTF-8 bytes (`keys.go:134-137`) but has no
IME/composition support** — and an IME is not something a raw-mode TTY program
can conjure: the terminal/IME hands it pre-composed characters, and while a
composition is in flight the app sees nothing. That is a *terminal capability*
question, not a gopi gap (§4). Note also: if a CJK-mangled *answer* was
observed, the cause is §1.2 (no wrapping) mis-positioning wide glyphs, not the
cell math.

---

## 2. What the layout machinery actually is (ceiling assessment)

Before judging "how far can this go", state what exists — it is much more than
"primitive":

| Component | File | What it gives |
|---|---|---|
| vt100 cell grid | `screen.go` (`Feed`, `Line`, `Contents`, `Scrollback`, `setScrollRegion`) | a real emulator: CUP/CUU/CUD/CUF/CUB/EL/ED/CR/LF/SGR + scroll regions |
| Scroll-region band layout | `region.go` (`computePlan`, `SetEditorRows`, `SetMenuRows`, `plan`) | reserved rows for a live answer band + editor + menu, anchored for scrollback |
| Wrapping + raw-mode editor | `render.go` (`layoutText`, `Redraw`, `displayWidth`) | the line editor, multi-row, wide-rune aware |
| Streaming answer band | `answer.go` (`AnswerRenderer`, `drain`, `writeLive`, `Tick`) | scrollback-safe live output, status line, commit-on-row-fill |
| History / completion / paste | `history.go`, `menu.go`, `keys.go` | recall ring, completer menu, paste-burst classification |
| Raw termios | `raw.go`, `termios_ioctl_linux.go`, `pty_linux.go` | zero-dep raw mode, full snapshot/restore |

13 render tests pass on this checkout, asserting **exact screen text and cursor
cells** (not "a callback fired"):

```
--- PASS: TestLineEditorRender      (row0 == "you> hello", cursor (0,10))
--- PASS: TestLineEditorRenderMidLine / UnicodeCursorWidth / Shrink
--- PASS: TestLineEditorRenderMultiline / Wraps / SigwinchRedraw
--- PASS: TestLineEditorScreenGrid / TestStatusRow_IsExactlyOneScreenRowWithALongToolSummary
```

**Ceiling judgement:** this is not a toy. It is a small but *genuine* terminal
application framework with a render-truth test harness. The defects in §1 are
three specific holes in it, not evidence the foundation is unsound.

---

## 3. Re-testing the agent's "I can see the layout" claim

The claim (previous turn) was that a text UI is agent-inspectable without a
screenshot. **Verified true, via the same harness, in one tool call** — a
40-col editor replaying a wrapping line:

```
--- AGENT-READABLE LAYOUT (40 cols x 24 rows) ---
row  0 |you> a long line that will definitely wr|
row  1 |ap past forty columns so we can see it|
row  2 ||
cursor=(1,38)
```

No PTY, no browser, no pixels: `Screen.Line(r)` / `Contents()` / `Scrollback()`
return strings the agent reads and asserts on. `internal/lineedit/render_test.go:9` names the
rule: *"the assertion is on RENDERED CONTENT, so a cursor one column off, a
stale row, or a line never drawn is visible to the test."*

**Caveat that materially qualifies the claim:** the harness models a cell grid
with **text only**. It does not model SGR *state* as an assertable attribute
(e.g. "this run is red/bold") in the assertions above, and it cannot see
*actual* pixels. So:

* "Can the agent see the layout?" → **yes, decisively.**
* "Can the agent see the *styling*?" → only as far as we teach `Screen` to
  expose it. That is a **small, well-scoped addition** (record SGR per cell,
  expose `Cell(row,col).Attr`), and it is the single highest-leverage enabler
  for everything the operator asked for in §1.3 — because it makes colour
  *testable from text*, preserving the CLI's one real advantage while adding
  the feature.

Consequence: **the CLI's advantage is real but conditional** — it survives only
if styling is expressed in, and asserted through, the same text-readable model.
Bolt on colour as raw escape bytes with no Screen support and you keep the
feature but lose the advantage.

---

## 4. Ceiling: what a terminal *cannot* do (do not plan around it)

These are not gopi limitations; they are the medium's:

| Operator wish | Terminal reality |
|---|---|
| **Different fonts** | Impossible in-app. The emulator owns the font; rich typography is out of scope permanently. |
| **True "blocks"/layout** | Partially. You can draw box-drawing glyphs, reverse-video regions, and colour bands — but no overlapping layers, no arbitrary positioning outside the cell grid, no smooth animation. Our scroll-region band is the right primitive. *Richer layout is implementable and worth doing* (see §5.3). |
| **IME input (中文)** | Not the app's to solve. The IME composes *before* the app; while composing, the app receives nothing. A raw-mode app can build its own composition UI (huge) — **not worth it**; rely on the terminal. Verify on a real terminal before scoping anything. |
| **Images** | Only via the kitty/iTerm2/sixel graphics protocol — emulator-dependent, not portable. Already partially reachable: the repo has `internal/text2img` + `internal/shot`. Low priority. |
| **Clickable / scrollback UX** | Terminal-dependent; mouse reporting is opt-in and widely disliked. |

**Therefore the "worth developing" question splits cleanly:**

* Chasing browser-grade UI in the terminal → **no.**
* Fixing the reading/writing defects and adding colour + better blocks →
  **yes, cheap, and it is the actual complaint.**

---

## 5. Scoped work items (with effort)

Effort anchored to the same scale as `devdoc/gui-chat-mode-effort.md` and the
A-series in `devdoc/gui-chat-roadmap.md`.

### 5.1 P0 — the reported defects (≈3–5d total; all of it overdue)

| ID | Goal | Files | Effort | Acceptance |
|---|---|---|---|---|
| **U1** | Make `editorRowsCap` **derived from terminal rows**, not the constant 4; let the editor grow while typing (band yields), so typed text is never invisible | `region.go:57`, `editor.go:745-752` | 1–1.5d | Scripted test at 40 cols asserts **all** wrapped input rows are on screen; a 24-row terminal shows ≫4 rows; no band starvation |
| **U2** | Wrap the **streamed answer** in the fallback `chatRenderer` to the real terminal width | `chat_stream.go:303` (+ width source per turn) | 1–1.5d | Test: a 200-char unbroken delta at 40 cols is emitted as width-bounded rows; code blocks/paragraphs reflow; `Screen` replay asserts row contents |
| **U3** | Fix the wide-terminal cursor-row under-report found in §1.1 (`cols=200 → cursor=(0,104)`) | `render.go` `layoutText` / caller | 0.5d | Regression test pinning cursor row at `cols > width*r` boundaries |
| **U4** | Verify IME/中文 input on a **real** terminal (SSH + local emulator); record the by-emulator matrix | — (investigation) | 0.5d | Doc with per-terminal result; **only then** any input-layer work is scoped |

### 5.2 P1 — the enabler that keeps the CLI's advantage (≈2–3d)

| ID | Goal | Files | Effort | Acceptance |
|---|---|---|---|---|
| **V1** | Extend `Screen` to **record SGR as per-cell attributes** and expose them (`Cell(row,col).Attr` / a styled `Contents`) | `screen.go` | 1–1.5d | Test asserts "the assistant's text is coloured, the error line is red" **as text**, no pixels |
| **V2** | Role-tinted output: colour/bold per message role (user / assistant / tool / error) + a `--no-color` / `NO_COLOR` kill switch | `answer.go`, `chat_stream.go` | 1–1.5d | V1-style assertions per role; piped/non-TTY output byte-identical to today (protects scripts + tests) |

### 5.3 P2 — block/visual improvements (≈2–4d, optional)

| ID | Goal | Effort |
|---|---|---|
| **W1** | Box/rail framing for tool calls and code blocks using box-drawing + reverse video (block-level separation without a real layout engine) | 1–2d |
| **W2** | Folding/eliding long tool output with a "… N lines" summary row | 1–2d |

### 5.4 Explicit non-goals (record the refusal)

* No in-app font control (impossible, §4).
* No custom IME/composition engine (not worth it; terminal's job, §4).
* No portability-breaking graphics protocol work (sixel/kitty) unless the
  operator names a target emulator.

---

## 6. Recommendation

1. **Do U1–U3 now.** They are the operator's actual complaint, they are small,
   and U1 in particular has a one-line root cause with a test-shaped fix. Ship
   them with render-truth tests per Rule 10.2.
2. **Then V1, before V2.** Without a `Screen` that can see SGR, adding colour
   trades away the CLI's only structural advantage (agent-assertable layout).
   V1 is the cheapest way to keep both.
3. **Re-frame the comparison for the operator:** `gopi web` remains the right
   surface for reading long answers, rich layout, and any browser-side
   accessibility. `gopi chat` should be *good at being a terminal app*, not a
   worse browser. The two are complementary access surfaces — confirmed
   dependency-wise in the prior exchange (both are zero-extra-Go-dep; only
   runtime needs differ: TTY+`/dev/ptmx` vs browser+port).
4. **Treat "primitive" as a defect list, not a verdict.** The foundation is
   real (§2); three named holes explain the entire observed experience.

## 7. Reproduce this study

```bash
# input clipping (§1.1) and cursor-row bug (§2): temporary scripted-source test
#   internal/lineedit/*_test.go, newScriptedEditor + editingScreen
go test ./internal/lineedit/ -run 'Render|Layout|Screen' -v   # 13 pass, exact-text asserts
grep -n 'editorRowsCap = 4'            internal/lineedit/region.go
grep -n 'rows > editorRowsCap'         internal/lineedit/editor.go
grep -n 'func (r \*chatRenderer) write' cmd/agent/chat_stream.go
grep -rn 'SGR\|color' cmd/agent/chat_stream.go internal/lineedit/answer.go internal/lineedit/render.go  # 0 hits
```

**Provenance:** study produced 2026-09-19 on `e3195ca6`; no production files
modified (the two probe tests used were removed after measurement — `git status`
clean).
