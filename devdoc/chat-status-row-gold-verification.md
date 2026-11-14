<!-- doc-class: historical -->
# Devdoc: the chat status row's gold — verified at the live surface, and the stale prose it left behind

**Task:** `01a0e2ed-8123-7e96-b8b8-f54b58f2ce39` (epic T6: "the info bar renders
attribute-only, not gold — inconsistent with the golden reply body").
**Verdict of this run: the epic's premise was STALE. The palette half was already
delivered by commit `4265324c`; this run VERIFIED it and closed the residue — the
prose, in three code comments and four docs, that still claimed the row was
colourless.**

Nothing in `internal/lineedit/palette*.go` was changed by this run. The one
production-comment change is in `cmd/agent/chat_stream.go` (comments only).

## 1. What `4265324c` already delivered (confirmed, not redone)

`git rev-parse --verify 4265324c^{commit}` → present; `git merge-base
--is-ancestor 4265324c 9f22bcda` → 0 (it is in this worktree's HEAD, which equals
master). It replaced the attribute-only status row with a per-tier gold:

| tier | status-row foreground | where |
|---|---|---|
| `HermesDarkPalette` | label/tool `38;2;218;165;32` (`#DAA520`), live `38;2;255;215;0` (`#FFD700`), thinking `38;2;204;155;31` (`#CC9B1F`) | `palette.go` `statusFg`, `statusGold`'s neighbours |
| `HermesLightPalette` | `#7A5A0F` / `#8B6914` | same helper |
| `Palette256` | `38;5;220` (`cubeGoldAccent`) | `palette.go` `palette256StatusFg` |
| `BasicPalette` (floor) | the NAMED `SGR 33` (`statusGold`) — never `39`, `90`, `2` | `palette.go:68-93` |

## 2. The verification this epic asked for (evidence, not prose)

### 2a. The REAL emitted bytes — live `gopi chat` under a pty

Built at this worktree's HEAD (`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`,
`REBUILD_OK`), run through a pty driver (`drive_chat.py`, stdlib only) with
`GOPI_HOME` = a gopi sandbox, and the row's own byte span isolated from the
stream (the surface erases the row with `ESC[2K` immediately before painting it,
so the scope is recovered from the stream rather than guessed):

| tier (`TERM` / `COLORTERM`) | status-row colour selections | banned set (`2`, `7`, `30-37`, `90-97`, `39`) |
|---|---|---|
| `xterm-256color` + `COLORTERM=truecolor` | `rgb:218;165;32`, `rgb:255;215;0` | **none** |
| `xterm-256color` | `idx256:220` | **none** |
| `xterm` (8-colour floor) | none — `SGR 33` only (`floor_gold(33)=True`) | none but the floor's own `33` |

`BANNED_PRESENT=none` on the two colour tiers; on the floor the single named code
is `33`, which is `statusGold` **and** the exception the scanner is given by name
(it is passed explicitly, never silently dropped).

### 2b. The CELL-GRID / style layer (a token set, not a substring)

The surface's own `--dump-screen` interpreted state, `attr_runs` of the status row:

```
truecolor  row 29: 0:14 fg=rgb:daa520 bold | 17:8 fg=rgb:daa520 bold | 28:6 fg=rgb:daa520 bold
                   37:17 fg=rgb:daa520 bold | 57:29 fg=rgb:daa520 bold | 86:12 fg=rgb:ffd700 bold italic
                   (the 14:3 / 25:3 / 34:3 / 54:3 / 98:2 runs are `default`)
256        row 29: same runs, fg=colour-220
8-colour   row 29: same runs, fg=yellow      (the dump renderer's name for SGR 33)
```

Every status FIELD carries gold, in its tier's own spelling; the only unstyled
runs are the `StatusSep` interpuncts — unstyled **by construction**
(`styledFields` never styles the separator), and each is 3 cells, so no field is
colourless. Verified at palette-table level too, through the in-repo capture
emitter (`GOPI_PALETTE_CAPTURE_DIR`): `status-row-{hermes-dark,hermes-light,
palette256,basic}.ansi` + their grid dumps agree field-for-field with the live
capture.

### 2c. The LIVE surface resolves to a gold table (capability → palette)

`GOPI_PALETTE=hermes` on `TERM=xterm-256color` with `COLORTERM` empty printed
exactly ONE downgrade notice naming its cause and the enabling variable, and drew
`palette256` — whose status row is gold (`38;5;220`). With `COLORTERM=truecolor`
the SAME binary drew the truecolour gold. So the live surface reaches a gold
table on every host shape; there is no capability that lands on a colourless row.

### 2d. Test-level acceptance

`go test ./internal/lineedit/` → **ok** (23.6s), including
`TestPaletteChangeNeverMovesACell`, `TestStatusRowCarriesNoThemeResolvedColour`,
`TestStatusRowColourIsGoldAndNotThemeResolved` (4/4 tiers),
`TestGoldTablesLeaveTheStatusRowThemeIndependent`,
`TestGoldPalettesCoverEverySurface`, `TestStatusRowEmphasisSurvivesTheColour`,
`TestStatusRowSalienceIsAttributeBased`.

## 3. The three briefing guards — all already re-pointed by `4265324c`

Re-read against the new intent, no fix needed in any of them:

- `palette_status_salience_test.go` — `TestStatusRowCarriesNoThemeResolvedColour`
  keeps the banned set and **added** `SGR 39` to it; `statusRowGoldSeq()` was
  introduced by `4265324c` and returns the constant `statusGold`; the NEW
  `TestStatusRowColourIsGoldAndNotThemeResolved` states the per-tier gold.
- `palette_hermes_test.go:437` — the old `TestGoldTablesLeaveTheStatusRowThemeIndependent`
  (briefing's `:434-439`) banned `truecolorMarker()`/`palette256Marker()` on the
  status row ("it may not name a colour"). HEAD requires exactly the table's own
  gold in its tier's own spelling. The ban was **inverted**, not deleted.
- The hermes status-row test at its new intent **is** that test.

## 4. The genuine residue this run closed (Rule 26: the prose caught up with the code)

`grep -rn "attribute-only"` still found sites asserting the stale claim. All were
corrected **in the same commit as this doc** (comments/prose only, no behaviour):

- `cmd/agent/chat_stream.go` — `statusCtxHeavyMarker`'s own doc comment said the
  row's palette "is attribute-only"; `statusCtxLabelCellsPinned`'s said the same.
  Both now state the row is GOLD and that what must survive colour-off is the
  GLYPH/WORD, which was always the real reason.
- `internal/lineedit/palette256_test.go` — the invariant list said "the status row
  stays attribute-only"; now "keeps its own gold (`38;5;220` on this tier)".
- `devdoc/chat-tool-activity-band-row.md` — the `ToolRow` vs `ToolCall` contrast
  gave the stale reason for a contrast that still stands.
- `briefings/brief-status-row-must-say-what-it-is.md`,
  `briefings/brief-tables-unreadable-at-width.md` — both asked the reader to
  design against "the row is attribute-only"; the requirement (no distinction by
  colour alone) is unchanged, only the reason was.
- `devdoc/handoff-20260927-chat-render-and-infobar-defects.md` (T6's own row) —
  marked STALE at the row, with a §6 addendum naming the commit that superseded
  it. **T6 should be closed as "premise stale (verified, not fixed)"**, not
  re-fixed.

**Why the stale premise survived**: "attribute-only" WAS true of `80a53890`, and
each doc inherited the phrase without re-reading the emitter it cited. The
lesson, one level up from this repo's own "read your own screen": **re-read the
code a claim cites before repeating the claim.**
