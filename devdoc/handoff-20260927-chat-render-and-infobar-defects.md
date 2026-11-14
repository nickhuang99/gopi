<!-- doc-class: normative -->
# Handoff — `gopi chat` surface: text rendering & info-bar defects

**Author:** gopi (self-observation session)
**Date:** 2026-09-27
**Repo:** `/home/user/work/GoPi` · **master HEAD at filing:** `6936c787`
**Session (evidence source):** `39fa9b4e-e0ad-4cad-81b4-099ecf168355`
**Operator's request:** *"如果你都file bug的话，你写一个handoff doc, 我下一次等全部fix再运行你读了自己来检验。"*
→ This doc is the checklist for a **re-verification pass driven by gopi reading its OWN screen** after the fixes land.

---

## 0. How to re-verify (the method that found all of these)

The suite was **green over every defect in this doc**. They were found only by
reading the live painted screen. Re-verify the same way — **do not trust the green suite**:

```sh
# 1. Is THIS chat run armed? (no dump is written by an unarmed run)
gopi chat --dump-screen          # arm at launch (the operator's run already does)

# 2. Read your OWN screen as a cell grid (no pixels, no vision)
env -u GOPI_CHAT_DUMP gopi ui_state --target chat
#   -> rows NNN|… + a .json twin with per-cell ATTRS (colour/highlight)

# 3. Audit one line: which side lost it (grid vs transcript)?
env -u GOPI_CHAT_DUMP gopi ui_state --compare "<line>" \
    --session 39fa9b4e-e0ad-4cad-81b4-099ecf168355 --target chat
#   exit 0 = on grid AND in transcript · 3 = they disagree · 1 = check could not run

# 4. Audit a whole answer, one shell line
env -u GOPI_CHAT_DUMP gopi ui_state --all --session 39fa9b4e-e0ad-4cad-81b4-099ecf168355 --target chat
```

**Known limits of the oracle (measured this session — do not misread as a fix or as damage):**
- The grid is a **26-row window**. Anything scrolled out reads `in_grid=false` with
  `damage=paint_loss` / `damage=cjk_wrap` — that is **scrolling, not loss**. In this
  session's `--all` audit, `matched=0, emphasis_checked=0` purely because the
  checked lines had scrolled off. **To catch a defect, read the grid WHILE the
  evidence is still on screen** (e.g. read right after the turn that shows it).
- `GOPI_CHAT_DUMP=""` (empty) makes `ui_state` emit a signal-provenance warning
  ("not filed"); prefix with `env -u GOPI_CHAT_DUMP` for a clean read.

---

## 1. Bug index (10 tasks filed this session)

| # | Task ID | Kind | One-line |
|---|---------|------|----------|
| **T1** | `01a0e2f5-bc4f-7355-bf37-52ba32727b8d` | **BUG render (worst)** | Live answer band **duplicates** a wrapping/scrolling answer (partial repeat, truncated tail). Rendering-only — transcript holds it once. |
| **T2** | `01a0e2f3-91b2-75b6-95ad-2204bccddeb5` | **BUG render** | A **wrapping** answer line's emphasis commit glues the provisional paint onto the committed row → duplicated text, stray/unbalanced `**`/backticks, bold+code spans broken **mid-word**. |
| **T3** | `01a0e2e6-5034-74dc-b5a2-bf14a23c2fa0` | BUG ui | Slash menu: **Arrow-Down is a no-op** when the prefix matches exactly one candidate (`/q` → `/quit`). |
| **T4** | `01a0e2e6-5300-7369-bc42-adcd7cb17c81` | BUG config | `--output-language <invalid>` prints a warning, then **exits 0 and proceeds** — no hard refusal. |
| **T5** | `01a0e2e9-478a-7ecc-b14c-6a15b6e11358` | BUG ui | Info-bar **cache column** (`↑`/`↓`) reserved 7 cells but renders 10–12 at high counts → `↓` truncated to `↑1662k…`. |
| **T6** | `01a0e2ed-8123-7e96-b8b8-f54b58f2ce39` | BUG ui | Info bar (**status row**) renders attribute-only (default fg), **not gold** — inconsistent with the golden reply body. **SUPERSEDED in part — see the addendum at the end of this doc: the premise was already stale when this row was written.** |
| **T7** | `01a0e2eb-ea31-7bf7-abba-06898df203c3` | BUG ux | Info bar `ctx 92.1k/200.0k!` — denominator is gopi's **own trimming budget**, but nothing on the row says so. |
| **T8** | `01a0e2e6-553f-76cc-bab4-164393671105` | UX | `--dump-screen` help asks the operator to "type"/"compare" — reads as asking the user to debug gopi. |
| **C1** | `01a0e2eb-ffaf-72bb-a021-ee3cc6855bd5` | **CORRECTION → REVERSED 2026-09-27** | Close T9 as **not-a-bug** (the golden reply colour IS intact — that half stands). **But its second clause — "the user's typed buffer is still unstyled (render.go:15-20)" — was OVERTURNED by the operator**: the buffer now takes the palette's gold. See the addendum at the end of this doc. |
| **C2** | `01a0e2e6-5742-76cf-8d49-fe445497912a` | **SUPERSEDED** | The earlier "golden colour regression" claim — **wrong**; superseded by C1 + T6. Close it. |

---

## 2. Re-verification checklist (one row per task — run AFTER all fixes)

For each: **what to do**, then **what the operator must SEE**. Mark PASS only on rendered truth.

### T1 — live band duplication (worst)  ★ do this one first
- **Do:** ask for an answer long enough to **wrap and scroll** (multi-paragraph CJK). While it streams, read the grid repeatedly: `ui_state --target chat`.
- **Also do:** `ui_state --compare "<a line from that answer>" --session <id> --target chat`.
- **PASS:** every visible row equals its transcript line; **no substring of the visible band appears twice**; the grid copy equals the transcript copy; `--compare` exits **0**.
- **Before (baseline, this session):** grid row `▌ 的屏幕才看得见。\`. I did看得见。\`. I did not generate a duplicate.` — duplicated + truncated; `whatthe` collapse; fragment of row N as head of row N+1. Evidence: `/tmp/zz_dup_grid.txt` (dump-line 1205 = the operator's verbatim paste).

### T2 — wrap/commit marker + mid-word span corruption
- **Do:** produce a wrapping line with bold **and** inline-code spans straddling the wrap; read the grid.
- **PASS:** no literal `**` and no stray backtick on **any** row; no duplicated fragment; the styled run is **continuous across the row boundary** (no half-styled word).
- **Before:** `▌ live/partial row (\`rowForPartial\`) paints markers literally —partial row** ...` (dup + stray `**`); `` `mark`/`ers` `` (code span mid-word). Root self-admitted by `9c6b8da8` "Not done: a line that WRAPS…".

### T3 — slash menu arrow-down
- **Do:** type `/q`, press **Down**, read grid; then Enter.
- **PASS:** the menu must not read as broken. Depending on the chosen design: either the sole row is clearly selected and Down is a *documented* no-op, or Down does something visible. Enter accepts `/quit`.
- **Before:** grid `you> /q` + `> /quit (alias of /exit)`; `\x1b[B` → **screen byte-identical**.
- **Also:** a regression test must exist (current tests only use a multi-candidate stub).

### T4 — output-language hard refusal
- **Do:** `/tmp/gopibin --output-language "not a lang!!" version; echo EXIT=$?` (rebuild first), and `GOPI_OUTPUT_LANGUAGE=chinese gopibin version; echo EXIT=$?`.
- **PASS:** **EXIT ≠ 0** with the refusal on stderr, before the subcommand dispatches (flag fatal at minimum; env/settings per the task's decision — must be stated and tested).
- **Before:** refusal printed, **EXIT=0**, version output proceeds.

### T5 — cache column truncation
- **Do:** reach a high cache count (or unit-compose with `cacheRead=cacheWrite=1662000`); read the info-bar row.
- **PASS:** the **whole** `↑1662k↓1662k` is visible (both arrows) and the row still measures **≤ 100 cells**.
- **Before:** `↑1662k↓1662k` = 12 cells vs 7 reserved → `↑1662k…`, `↓` gone. Only **1 cell of slack** at `BudgetWidth=100` → needs a **re-partition** (shares cells with T7).
- **Note:** T5 and T7 **compete for the same 100 cells** — verify together.

### T6 — info bar not gold
- **Do:** read the info bar's cells **with attrs** (`ui_state --target chat --json`); compare to the answer body's colour.
- **PASS:** the info bar carries a **gold truecolour** from the Hermes family (e.g. `#DAA520` label / `#FFD700` primary), reads as visibly gold, and carries **no banned theme-dependent code** (`sgrDim`, `sgrGray`, 8-colour names).
- **Before:** `StatusLabel = Style{sgrBold}`, `StatusLive = Style{sgrBold+sgrItalic}` — default foreground. Locked by `palette_hermes_test.go:434-439` (asserts **no** truecolour on the status row) → that test must be **updated**, not worked around.

### T7 — ctx denominator unlabelled
- **Do:** read the info bar.
- **PASS:** the row **states what the denominator is** (gopi's working/trimming budget, not the model's ctx limit) — a word/glyph; no bare `92.1k/200.0k`.
- **Before:** bare pair; reader assumes it is the model limit and sees 200K vs DeepSeek's advertised 1M. Semantics: `EstimatedTokens()` / `DefaultContextWindow=200000` (the WHY is in `internal/agent/context.go:13-18`).

### T8 — dump-screen help vocabulary  *(operator should confirm intent)*
- **Do:** read `gopi chat --help` / `gopi ui_state -h`.
- **PASS:** no imperative addressed to the user to run "type"/"compare"; the text says the flag **arms an observation gopi itself consumes**.
- **Open question for the operator:** is `--dump-screen` an operator tool or an agent-internal capability? (Affects whether the compare loop is auto-driven.)

### C1/C2 — close, do not "fix" — **REVERSED 2026-09-27 (buffer half only)**
- **Do:** confirm `HermesDarkPalette.Answer` still carries the gold text (`#FFF8DC`) and the user's typed buffer is still unstyled (`render.go:15-20`).
- **PASS:** unchanged; T9/C2 closed as **not-a-bug**; `TestHermesAnswerBodyCarriesTheGoldText` green.
- **REVERSED (2026-09-27, task `01a0e439`):** the SECOND clause is no longer a
  pass condition. The answer-body half stands exactly as written (do not touch
  the answer body, `#FFF8DC` intact, that test green). The buffer half — "the
  user's typed buffer is still unstyled" — was OVERRULED by the operator, and
  the re-verification below must now expect the buffer to be GOLD. The full
  reasoning, the evidence and the new pass condition are in the addendum at the
  end of this doc.

---

## 3. Verified evidence summary (what is proven vs open)

| Claim | Status | Evidence |
|-------|--------|----------|
| Renderer is correct on a whole line (`**bold**` → bold gold, markers gone) | **PROVEN** | unit-driving shipped `AnswerRenderer`: whole-line / byte-chunked / split-marker / thinking+answer all render bold (`\x1b[1m\x1b[38;2;218;165;32m`), markers removed. |
| T1 duplication is RENDERING, not generation | **PROVEN** | `session.jsonl` L454 (my turn) = **1 occurrence**, clean tail; L455 (operator msg) = 2; grid dump-line 1205 = the duplicated+truncated copy. |
| T2 root = the wrap path | **PROVEN (by the fixer's own admission)** | `9c6b8da8` commit message: *"Not done: a line that WRAPS across band rows still carries its markers…"*; that commit is an **ancestor of HEAD** and predates the running process → not a stale binary. |
| T3 `MoveSelection` no-op at n==1 | **PROVEN** | `internal/lineedit/menu.go:164` `(Index+delta)%n`; `/q` → exactly 1 candidate; live screen unchanged after Down. |
| T4 exits 0 on invalid tag | **PROVEN** | built binary: refusal on stderr + **EXIT=0** + normal output. |
| T5 12-cells-in-7 | **PROVEN** | measured `statusCacheField` = `↑1662k↓1662k` (12) vs `statusCacheCol=7`. |
| T7 200.0k = gopi's own budget | **PROVEN** | `internal/agent/context.go:13-19` (prefill-cost rationale). |
| T6 status row attribute-only | **STALE — do not act on this row** | the claim cited `internal/lineedit/palette.go:307-347`, which by then already carried `statusFg`/`palette256StatusFg` (commit `4265324c`, merged before the row was written). See the addendum at the end of this doc. |

**OPEN / honest gaps (NOT yet proven):**
- **T1/T2 root cause is not yet pinned to the exact repaint lines** — mechanism is characterised (wrap + scroll/repaint row-offset bookkeeping), but no unit-test repro exists yet. T1 & T2 likely **share a root** — a fixer should read them together.
- **T5/T7 cell re-partition** is a recommendation, not a decided number.
- **T8** intent question is for the operator.

---

## 4. Suggested fix order

1. **T1 + T2 together** (same family: wrap/scroll/repaint of the answer band) — these are the operator-visible "rendering is in chaos" defects.
2. **T5 + T7 together** (both need cells from the same fixed 100-cell status row).
3. **T6** (info-bar gold; touches `palette.go` + 2 tests).
4. **T3**, **T4** (small, isolated).
5. **T8** (docs/help) + **close C1/C2**.

---

## 5. Re-verification record (fill in on the next pass)

| Task | Date | Verdict | Evidence read | Notes |
|------|------|---------|---------------|-------|
| T1 | | | | |
| T2 | | | | |
| T3 | | | | |
| T4 | | | | |
| T5 | | | | |
| T6 | 2026-09-27 | **CLOSED — premise stale (verified, not fixed)** | `git show 4265324c --stat`; `git merge-base --is-ancestor 4265324c master` → YES; live PTY run of `bin/gopi chat` (session `01a0e2ed-8123-7e96-b8b8-f54b58f2ce39`, `live-chat.raw` + `live-chat-screen.dump`); `go test ./internal/lineedit/` green | the status row already carried gold; the deliverable was verification + the stale prose this doc itself was part of. See the addendum below. |
| T7 | | | | |
| T8 | | | | |
| C1/C2 | | | | |

**Reminder to the future self reading this:** the suite being green means nothing here.
Read your own screen. If you cannot see the defect you are verifying, you have not verified it.

---

## 6. Addendum — T6 was written against a STALE premise (2026-09-27, task `01a0e2ed`)

T6 above says the status row "renders attribute-only (default fg), **not gold**", and
cites `internal/lineedit/palette.go:307-347`. **That citation already contradicted the
row it was supporting.** By the time this doc was written, commit **`4265324c`**
("feat(lineedit): the chat status row carries gold again — a colour the terminal cannot
remap", 2026-09-27 21:26) had replaced the attribute-only four fields with
`statusFg(attrs, rgb)` (`palette.go:350`) and `palette256StatusFg(attrs, idx)`
(`palette.go:470`), giving every tier its own gold:

| tier | status-row colour |
|---|---|
| `HermesDarkPalette` | label `#DAA520`, tool row `#FFD700`, thinking `#CC9B1F` (`38;2;r;g;b`) |
| `HermesLightPalette` | `#7A5A0F` / `#8B6914` (`38;2;r;g;b`) |
| `Palette256` | `38;5;220` (`cubeGoldAccent`) |
| `BasicPalette` (floor) | the named `SGR 33` (`statusGold`), never `SGR 39` / `90` / `2` |

**What was actually wrong was not the code but the prose around it** — this table, and
four devdocs that had recorded "the status row is attribute-only" as a standing claim. A
`grep` for that phrase is the check; all of them were corrected in the same commit as
this addendum (see `devdoc/chat-status-row-gold-verification.md`).

**How the stale premise survived**: the phrase "attribute-only" *was* true of HEAD
`80a53890`, and every document that repeated it inherited the claim without re-reading
the emitter. The lesson is the one this doc's own reminder states — read your own screen —
applied one level up: **re-read the code the claim cites before repeating the claim**.


---

## Addendum — C1's buffer clause is REVERSED (2026-09-27, task `01a0e439`)

**What changed.** C1 closed the "golden colour regression" report as
not-a-bug on two grounds: (a) `HermesDarkPalette.Answer` still carries the gold
text, and (b) the user's typed buffer is unstyled **by design**
(`render.go:15-20`). Ground (a) stands — nothing here touches the answer band.
Ground (b) **no longer holds**: the editing line's buffer is painted.

**Why (the operator's ruling).** The surface was re-read on the live screen, and
the editing row read `0:5=fg=colour-186 bold 5:112=default` — a pale-gold prompt
next to default-coloured input, on a screen whose status row is gold. The rule
that produced that was not careless; it was answering a different question. Its
argument was *"what the user typed is theirs, and colouring it would put the
palette's judgement on their input"* — a good reason to refuse a JUDGEMENT, and
no reason at all to leave the text at the terminal's default colour: **SGR 39 is
a colour too** (whatever the theme declares for plain text), so choosing it for
the input was a colour decision made by omitting one. The objection is answered
by the VALUE, not by the old rule: the buffer takes the same gold family the rest
of the interface is made of, so it marks "this is the editing line" and nothing
about the input's worth. The operator reversed the verdict after reading the
screen twice in a row.

**How it is implemented.** One new `Palette` field, `Buffer`, with one value per
table — the floor's named gold (`statusGold`, SGR 33 — an 8-colour terminal can
pin no other, and the alternative was the SGR 39 that was reported as missing),
the 256 cube's `#FFD700` (`38;5;220`, `cubeGoldAccent`), and Hermes' `primary`
(`#FFD700` dark / `#8B6914` light). `MonoPalette` keeps the zero value, so the
colour-off path emits not one byte more than it did. The renderer draws it
through `editingRowSegs` (the prompt's row-local span, then the buffer's for the
rest of the row, on every row). No new colour constant was introduced and no
call site composes an SGR sequence.

**The new PASS condition for re-verification.** The editing row's buffer cells
must carry the table's gold on the CELL GRID (not merely "some SGR was written"),
and must NOT be the default foreground:

```sh
gopi chat --dump-screen
env -u GOPI_CHAT_DUMP gopi ui_state --target chat --json   # read the editing row's attr_runs
# row `you> …`:  0:5=fg=colour-186 bold  5:N=fg=colour-220 bold
#   ^ the prompt (unchanged)              ^ the buffer — GOLD, and NOT `default`
```

With `NO_COLOR=1` the same row is a single `default` run (colour is off, and the
bytes are identical to the pre-change tree).

**Evidence class — what C1 got wrong is worth keeping.** C1's "by design" was
*true*: the code did what the doc said, the tests were green, and the reasoning
was written down. Reading the code cannot decide whether a design is what the
operator wants; only the operator can, and only the SCREEN shows them what they
are deciding about. This row is the standing example: a green suite over a
correct implementation of a design the user did not want.
