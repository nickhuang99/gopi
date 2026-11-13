package lineedit

import (
	"strconv"
	"strings"
	"testing"
)

// turn_header_block_test.go — the MULTI-LINE turn header, asserted as ROWS OF A
// BLOCK and as CELLS ON THE GRID.
//
// The defect this closes: the header was one row by construction, so a long
// prompt (a pasted paragraph, a spilled paste's reference) became a cut prefix
// with no statement of what was missing. The fix gives the prompt a LINE BUDGET
// and makes the last row SAY what it left out (`... (+N more line(s))`).
//
// Three claims are asserted here, each as a criterion rather than a snapshot:
//
//	no-silent-cut — the rows the block DRAWS are a PREFIX of the label, and when
//	                the label does not fit, the block's last row is the counted
//	                marker row. There is no third state.
//	total-in-cols — every row of the block is at most cols display cells AND
//	                occupies exactly one screen row (a wrapping row would steal a
//	                band row the per-row finalize did not count).
//	paste-room    — a spilled paste's prompt gets MORE rows than a typed prompt of
//	                the same width, and the reference itself (its path and its
//	                size) is legible on row 0 — the operand of the reader's next
//	                action is not the part that gets cut.

// headerBlockPrompt builds a prompt of roughly n display cells: whole words, so
// the block's cutter has the boundaries a real prompt has.
func headerBlockPrompt(n int) string {
	var b strings.Builder
	for b.Len() < n {
		b.WriteString(headerBlockWord)
		b.WriteString(" ")
	}
	return strings.TrimRight(b.String(), " ")
}

// headerBlockWord is the fixture's word. It carries the CJK a real prompt carries
// and is long enough that the cutter has to make decisions inside it.
const headerBlockWord = "从日志里找出失败的步骤并解释原因，然后给出修复建议 placeholder"

// headerBlockPasteRef is the reference a spilled paste leaves on the input line,
// in the fixed spelling PasteRefFor renders: `[paste <runes>: /abs/path]`.
func headerBlockPasteRef(runes int, path string) string {
	return PasteRefFor(PasteRef{Runes: runes, Path: path})
}

// headerBlockPastePrompt is a paste-shaped prompt: the reference FIRST, then a
// question about the payload — the shape the chat boundary produces, and the one
// where the reference is what the reader must be able to see.
func headerBlockPastePrompt() string {
	return headerBlockPasteRef(headerBlockPasteRunes, headerBlockPastePath) + " " +
		headerBlockPrompt(turnHarnessCols*headerBlockPasteTail)
}

// The paste fixture's numbers: a path short enough that the reference is legible
// inside one row's label budget at the widths the cases use, and a size large
// enough that the reference states something a reader needs.
const (
	headerBlockPasteRunes = 51200
	headerBlockPastePath  = "/tmp/pastes/paste-1.txt"
)

// headerBlockPasteTail repeats after the reference: the reference alone is short
// enough that a wide row would carry the whole paste prompt in one continuation
// row, and the paste leg is about the BUDGET (turnHeaderPasteContinuationLines),
// which only shows once there is more text than a row.
const headerBlockPasteTail = 12

// headerBlockCols are the widths every property below is asserted at: the
// operator-facing 80/117/120, the package's own narrow harness width (40), and
// the two degradation boundaries of the single-row chain.
var headerBlockCols = []int{40, 41, 60, 80, turnDumpNone, a5WideCols, 120}

// turnDumpNone is "no width" — the case that must draw nothing at all.
const turnDumpNone = 0

// TestTurnHeaderLines_EveryRowIsOneScreenRow is the total-in-cols claim at every
// width the cases use: each row of the block is inside cols AND occupies exactly
// one screen row, and the FIRST row is byte-identical to TurnHeader's own answer
// (so the block is a strict superset of the header, never a second spelling of
// it).
func TestTurnHeaderLines_EveryRowIsOneScreenRow(t *testing.T) {
	prompts := map[string]string{
		"typed-long":  headerBlockPrompt(turnHarnessCols * 4),
		"paste":       headerBlockPastePrompt(),
		"short":       "why?",
		"empty":       "",
		"cjk-no-word": strings.Repeat("中文目录", 40),
	}
	for name, prompt := range prompts {
		for _, cols := range headerBlockCols {
			info := TurnInfo{Index: 3, Prompt: prompt}
			lines := TurnHeaderLines(info, cols)
			if len(lines) == 0 {
				t.Fatalf("%s: TurnHeaderLines(cols=%d) returned no rows", name, cols)
			}
			if lines[0] != TurnHeader(info, cols) {
				t.Fatalf("%s: row 0 = %q, want TurnHeader's own answer %q — the block must not re-spell the header row",
					name, lines[0], TurnHeader(info, cols))
			}
			for i, line := range lines {
				if cols == 0 {
					if line != "" {
						t.Fatalf("%s: cols=0 drew %q, want nothing (an unknown width draws nothing)", name, line)
					}
					continue
				}
				if w := displayWidth(line); w > cols {
					t.Fatalf("%s: row %d of the block is %d cells wide (cols=%d): %q", name, i, w, cols, line)
				}
				if rows := wrappedRows(line, cols); len(rows) != 1 {
					t.Fatalf("%s: row %d of the block occupies %d screen rows (cols=%d): %q\n--- rows ---\n%s",
						name, i, len(rows), cols, line, strings.Join(rows, "\n"))
				}
			}
			if cols == 0 && len(lines) != 1 {
				t.Fatalf("%s: cols=0 returned %d rows, want the single empty row", name, len(lines))
			}
		}
	}
}

// TestTurnHeaderLines_NoSilentCutAndTheLastRowCounts is the first claim: whatever
// the block draws is a PREFIX of the label, and when the label does not fit, the
// block's last row is the counted marker row — so a reader can always tell
// whether the header holds the whole question, and if not, how much is missing.
func TestTurnHeaderLines_NoSilentCutAndTheLastRowCounts(t *testing.T) {
	prompt := headerBlockPrompt(turnHarnessCols * 6)
	label := turnLabel(TurnInfo{Index: 7, Prompt: prompt})
	for _, cols := range []int{40, 60, 80, a5WideCols, 120} {
		lines := TurnHeaderLines(TurnInfo{Index: 7, Prompt: prompt}, cols)
		last := lines[len(lines)-1]

		if strings.HasPrefix(last, TurnHeaderMoreMarker) {
			// The cut state: the last row counts what was not drawn.
			if !strings.Contains(last, turnHeaderMoreTail) || !strings.Contains(last, turnHeaderMoreClose) {
				t.Fatalf("cols=%d: the block's last row %q is not the marker sentence shape", cols, last)
			}
			n, err := headerBlockCount(last)
			if err != nil {
				t.Fatalf("cols=%d: the last row %q carries no readable count: %v", cols, last, err)
			}
			if n < 1 {
				t.Fatalf("cols=%d: the last row %q counts %d unseen rows, want at least 1", cols, last, n)
			}
			if len(lines) < 2 {
				t.Fatalf("cols=%d: a marker row without a preceding continuation row: %q", cols, strings.Join(lines, "\n"))
			}
			// Every row ABOVE the marker is part of the label: the drawn text is a
			// prefix of it, so no row carries text that is not the question.
			drawn := headerBlockChars(headerBlockDrawn(lines[:len(lines)-1]))
			if !strings.HasPrefix(headerBlockChars(label), drawn) {
				t.Fatalf("cols=%d: the rows the block drew are NOT a prefix of the label.\ndrawn: %q\nlabel: %q",
					cols, headerBlockDrawn(lines[:len(lines)-1]), label)
			}
			if drawn == headerBlockChars(label) {
				t.Fatalf("cols=%d: the block says %d rows are missing although it drew the whole label %q",
					cols, n, label)
			}
			// The count must be about rows the READER cannot see: the same text at
			// the same width still takes that many continuation rows.
			if got := turnHeaderMoreRows(headerBlockRemainder(lines, label), turnHeaderContinuationWidth(cols)); got != n {
				t.Fatalf("cols=%d: the marker counts %d row(s) but the remainder takes %d at a continuation row's width — the count and the block disagree",
					cols, n, got)
			}
			continue
		}

		// The third state: the label did not fit row 0, but the hidden remainder was
		// not worth a continuation row, so the block declines and IS the single-row
		// header — which must then wear the header's own cut marker, never end on a
		// silent cut.
		drawn := headerBlockDrawn(lines)
		if len(lines) == 1 && headerBlockChars(drawn) != headerBlockChars(label) {
			if !strings.Contains(last, statusEllipsis) {
				t.Fatalf("cols=%d: a single-row block ended on a SILENT cut — no marker row and no cut marker:\n%q", cols, last)
			}
			if strings.Contains(last, TurnHeaderMoreMarker) {
				t.Fatalf("cols=%d: a single-row block claims a marker row it does not draw:\n%q", cols, last)
			}
			continue
		}
		// Otherwise: no marker row at all, and the rows reconstruct the WHOLE
		// label — the claim that makes a block wider than one row worth anything.
		if strings.Contains(strings.Join(lines, "\n"), TurnHeaderMoreMarker) {
			t.Fatalf("cols=%d: the block shows the whole label and still wears the marker row:\n%s", cols, strings.Join(lines, "\n"))
		}
		if headerBlockChars(drawn) != headerBlockChars(label) {
			t.Fatalf("cols=%d: the block ends without the marker and without the whole label.\ndrawn: %q\nlabel: %q",
				cols, headerBlockCollapse(drawn), headerBlockCollapse(label))
		}
	}
}

// TestTurnHeaderLines_ShortPromptIsExactlyOneRow is the degradation half: a prompt
// that fits the header row produces EXACTLY one row — the same string TurnHeader
// returns — so nothing about the single-row header changed for the callers and
// prompts that were already whole.
func TestTurnHeaderLines_ShortPromptIsExactlyOneRow(t *testing.T) {
	for _, prompt := range []string{"why?", "", "one two three", strings.Repeat("a", turnHarnessCols-1)} {
		for _, cols := range []int{40, 80, a5WideCols, 120} {
			info := TurnInfo{Index: 1, Prompt: prompt}
			lines := TurnHeaderLines(info, cols)
			if len(lines) != 1 {
				t.Fatalf("prompt %q at cols=%d produced %d rows, want exactly 1:\n%s",
					prompt, cols, len(lines), strings.Join(lines, "\n"))
			}
			if lines[0] != TurnHeader(info, cols) {
				t.Fatalf("prompt %q at cols=%d: row 0 = %q, want TurnHeader's %q", prompt, cols, lines[0], TurnHeader(info, cols))
			}
		}
	}
}

// TestTurnHeaderLines_NarrowWidthKeepsTheHeaderChain is the narrow-width leg: below
// turnLabelHalfCells the single-row chain (half rule, then bare label) is what the
// block returns — one row, no continuation, no marker — because a block's rows
// could not be labelled at that width.
func TestTurnHeaderLines_NarrowWidthKeepsTheHeaderChain(t *testing.T) {
	prompt := headerBlockPrompt(turnHarnessCols * 4)
	for _, cols := range []int{1, 2, 6, turnHeaderMinCells, 12, 23, turnLabelHalfCells - 1} {
		info := TurnInfo{Index: 1, Prompt: prompt}
		lines := TurnHeaderLines(info, cols)
		if len(lines) != 1 {
			t.Fatalf("cols=%d produced %d rows, want 1 (the narrow chain is a single row):\n%s",
				cols, len(lines), strings.Join(lines, "\n"))
		}
		if lines[0] != TurnHeader(info, cols) {
			t.Fatalf("cols=%d: %q, want TurnHeader's own %q", cols, lines[0], TurnHeader(info, cols))
		}
	}
}

// TestTurnHeaderLines_PastePromptGetsMoreRoom is the paste leg: a spilled paste's
// prompt draws MORE rows than a typed prompt at the same width, and the reference
// itself — its size and its path — is legible on the block (it is the operand of
// the reader's next action).
func TestTurnHeaderLines_PastePromptGetsMoreRoom(t *testing.T) {
	const cols = 80
	ref := headerBlockPasteRef(headerBlockPasteRunes, headerBlockPastePath)
	pasteInfo := TurnInfo{Index: 2, Prompt: headerBlockPastePrompt()}

	typed := TurnHeaderLines(TurnInfo{Index: 2, Prompt: headerBlockPrompt(len([]rune(headerBlockPastePrompt())))}, cols)
	paste := TurnHeaderLines(pasteInfo, cols)

	if len(paste) <= len(typed) {
		t.Fatalf("the paste block drew %d row(s) and the typed one %d: the paste leg must get MORE room (turnHeaderPasteContinuationLines)\n--- paste ---\n%s\n--- typed ---\n%s",
			len(paste), len(typed), strings.Join(paste, "\n"), strings.Join(typed, "\n"))
	}
	// The reference is on the block, size and path together: the reader can see
	// WHICH file the turn is about without opening the transcript.
	if !strings.Contains(strings.Join(paste, "\n"), ref) {
		t.Fatalf("the paste block does not carry the whole reference %q:\n%s", ref, strings.Join(paste, "\n"))
	}
	if paths := PasteRefPaths(strings.Join(paste, "\n")); len(paths) == 0 || paths[0] != headerBlockPastePath {
		t.Fatalf("the block's reference no longer parses (paths=%v): the paste statement is prose the reader cannot act on\n%s",
			paths, strings.Join(paste, "\n"))
	}
}

// TestTurnHeaderMoreRow_KeepsTheCountOnANarrowRow is the count row's own narrow
// leg: at a width too small for the whole sentence the row degrades in WORDS —
// dropping the closing `)`, then the plural `s`, then ` more line` — and the two
// facts it exists for (the marker head and the COUNT) survive every one of them.
// The count is what tells the reader how much is missing, so a count row that
// loses it is the silent cut this block was built to end.
func TestTurnHeaderMoreRow_KeepsTheCountOnANarrowRow(t *testing.T) {
	counts := []int{1, 2, 9, 12, 123, 1234}
	// The block only composes at cols ≥ turnLabelHalfCells (TurnHeaderLines
	// declines below it), so that is the floor the guarantee must hold from.
	for cols := turnLabelHalfCells; cols <= a5WideCols; cols++ {
		for _, n := range counts {
			row := turnHeaderMoreRow(n, cols)
			if w := DisplayWidth(row); w > cols {
				t.Fatalf("cols=%d n=%d: the row %q is %d cells — a row wider than the width would wrap", cols, n, row, w)
			}
			if !strings.HasPrefix(row, TurnHeaderMoreMarker) {
				t.Fatalf("cols=%d n=%d: the row %q lost the marker head %q", cols, n, row, TurnHeaderMoreMarker)
			}
			if !strings.Contains(row, strconv.Itoa(n)) {
				t.Fatalf("cols=%d n=%d: the row %q does not state the count — the one fact that separates this row from a silent cut", cols, n, row)
			}
			if rows := wrappedRows(row, cols); len(rows) != 1 {
				t.Fatalf("cols=%d n=%d: the row %q takes %d screen rows", cols, n, row, len(rows))
			}
		}
	}
}

// TestTurnHeaderMoreRow_DegradesInWordsBeforeItCuts pins the WHOLE degradation
// chain as a property rather than three examples: at any width where the count
// row can exist at all, the row is the WIDEST spelling that fits — the whole
// sentence, else without the closing `)`, else without the plural `s`, else
// without the tail ` more line` — and the count itself is dropped only below
// turnHeaderMinContinuationCells, where a row's head no longer fits whole (a
// width TurnHeaderLines never composes a block at). Every stage is reached, so
// no arm of the chain can rot unnoticed while the widest spelling keeps passing.
func TestTurnHeaderMoreRow_DegradesInWordsBeforeItCuts(t *testing.T) {
	counts := []int{1, 2, 9, 12, 123, 1234}
	stages := map[string]bool{}
	for _, n := range counts {
		full := turnHeaderMoreRow(n, a5WideCols)
		head := TurnHeaderMoreMarker + strconv.Itoa(n)
		mid := strings.TrimSuffix(full, turnHeaderMoreClose)
		if mid == full {
			t.Fatalf("n=%d: the wide row %q does not end in the closing mark %q", n, full, turnHeaderMoreClose)
		}
		for cols := DisplayWidth(head); cols < turnLabelHalfCells; cols++ {
			want, stage := head, "head-only"
			if DisplayWidth(mid) <= cols {
				want, stage = mid, "no-close"
			}
			if DisplayWidth(full) <= cols {
				want, stage = full, "full"
			}
			if got := turnHeaderMoreRow(n, cols); got != want {
				t.Fatalf("cols=%d n=%d: the row = %q, want the widest spelling that fits %q", cols, n, got, want)
			}
			if !strings.Contains(want, strconv.Itoa(n)) {
				t.Fatalf("cols=%d n=%d: the %s stage %q dropped the count", cols, n, stage, want)
			}
			stages[stage] = true
		}
	}
	for _, want := range []string{"full", "no-close", "head-only"} {
		if !stages[want] {
			t.Fatalf("the chain's %q stage was never reached by the widths driven here", want)
		}
	}
}

// TestTurnHeaderMoreRow_SingularAndPlural is the row's grammar: one unseen line
// reads `line`, two read `lines`, and the count is always the same number the
// block's own measurement produced.
func TestTurnHeaderMoreRow_SingularAndPlural(t *testing.T) {
	const cols = 80
	if got := turnHeaderMoreRow(1, cols); got != TurnHeaderMoreMarker+"1 more line"+turnHeaderMoreClose {
		t.Fatalf("the one-line row = %q, want the singular spelling", got)
	}
	if got := turnHeaderMoreRow(2, cols); got != TurnHeaderMoreMarker+"2 more lines"+turnHeaderMoreClose {
		t.Fatalf("the two-line row = %q, want the plural spelling", got)
	}
}

// TestTurnHeaderLines_BlockRowsAreCommittedOnePerRow drives the block through the
// REAL renderer: BeginTurn commits one band row per block row (the same
// writeLive/finalizeLiveRow path an answer line takes), so the band's row ledger
// counts the header exactly as the block drew it — the accounting the epic
// requires not to break.
//
// BeginTurn draws TWO things, and the ledger after it is their SUM: the header
// block, and the turn's REQUEST row (drawRequestRow, layers.go) — the operator's
// own line, capped at requestRowMaxRows and wrapped at the same width. The
// request row is drawn HERE (not with the first answer line) so a tool call can
// never open a block whose request header arrives afterwards, and that is why
// this case names the block's share rather than reading the turn's opening total:
// measuring one leg with the other leg's rows in the same counter is the stale
// expectation this case was repaired from (measured: 8 rows = the block's 5 + the
// request's 3 at cols=40; the earlier form asserted 5 and read 8).
func TestTurnHeaderLines_BlockRowsAreCommittedOnePerRow(t *testing.T) {
	h := newA5HarnessSized(t, nil, nil, turnHarnessRows, turnHarnessCols)
	h.answer.SetCols(turnHarnessCols)
	prompt := headerBlockPrompt(turnHarnessCols * 5)
	want := TurnHeaderLines(TurnInfo{Index: 1, Prompt: prompt}, turnHarnessCols)
	if len(want) < 2 {
		t.Fatalf("the fixture prompt produced %d header row(s) at cols=%d — the case needs a block", len(want), turnHarnessCols)
	}
	// The request leg is read from the ONE composer BeginTurn draws it with, so
	// the two legs cannot be counted by two spellings of the same arithmetic.
	reqRows := requestRowRows(prompt, turnHarnessCols)
	if len(reqRows) == 0 {
		t.Fatalf("the fixture prompt drew no request row — the case would not exercise the leg BeginTurn commits beside the block")
	}
	h.answer.BeginTurn(TurnInfo{Index: 1, Prompt: prompt})
	if got := h.answer.Committed(); got != len(want)+len(reqRows) {
		t.Fatalf("BeginTurn committed %d row(s), want %d (the block's %d row(s), one per block row, + the request's %d)",
			got, len(want)+len(reqRows), len(want), len(reqRows))
	}
	h.answer.Delta("answer\n")
	h.answer.Finish()
	h.answer.EndTurn()
	h.replay()

	lines := bandLines(h, h.rowsTo(h.region.Plan().BandTop, h.region.Plan().BandBottom))
	// Every block row is on the grid, in order, and none of them wrapped (a
	// wrapped row would appear as two rows and shift the rest).
	at := rowOf(lines, strings.TrimRight(want[0], " "))
	if at < 0 {
		t.Fatalf("the block's first row is not on the grid\n--- band ---\n%s", strings.Join(lines, "\n"))
	}
	for i, row := range want {
		grid := strings.TrimRight(row, " ")
		if got := strings.TrimRight(lines[at+i], " "); got != grid {
			t.Fatalf("block row %d on the grid = %q, want %q\n--- band ---\n%s", i, got, grid, strings.Join(lines, "\n"))
		}
	}
	if !strings.HasPrefix(want[len(want)-1], TurnHeaderMoreMarker) {
		t.Fatalf("the fixture's block does not end in the marker row: %q", want[len(want)-1])
	}
}

// ── block helpers (the assertions' instruments) ─────────────────────────────

// headerBlockCount reads N out of a `... (+N more line(s))` row.
func headerBlockCount(line string) (int, error) {
	rest, ok := strings.CutPrefix(line, TurnHeaderMoreMarker)
	if !ok {
		return 0, errHeaderBlockNoMarker
	}
	end := strings.Index(rest, turnHeaderMoreTail)
	if end < 0 {
		return 0, errHeaderBlockNoMarker
	}
	return strconv.Atoi(rest[:end])
}

// errHeaderBlockNoMarker is the parse failure headerBlockCount reports (Rule 15:
// the error is named and returned, never swallowed).
var errHeaderBlockNoMarker = errorString("turn header block: not a marker row")

// errorString is the smallest error value this file needs, so the test file adds
// no dependency for one failure path.
type errorString string

func (e errorString) Error() string { return string(e) }

// headerBlockDrawn reconstructs the label text a block's rows show: row 0 keeps
// everything from the turn word onward (its lead rule is chrome) minus its own
// closing rule, the ellipsis a cut row wears and the spacing; every later row
// loses its indent. The result is the character stream the reader sees, so a
// prefix comparison against the label is a statement about the PROMPT, not about
// the rules around it.
func headerBlockDrawn(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	var b strings.Builder
	for i, row := range lines {
		text := row
		if i == 0 {
			if at := strings.Index(text, turnLabelTurn); at >= 0 {
				text = text[at:]
			}
			text = headerBlockTrimRowChrome(text)
		} else {
			text = strings.TrimLeft(text, " ")
		}
		b.WriteString(text)
	}
	return b.String()
}

// headerBlockTrimRowChrome removes the chrome a FILLED header row ends with: the
// closing rule run, the spacing that separates it from the label, and the cut
// marker the row wears when it had to be shortened (the marker is a statement
// about the row, not a character of the question).
func headerBlockTrimRowChrome(row string) string {
	trimmed := strings.TrimRight(row, TurnRuleGlyph)
	trimmed = strings.TrimRight(trimmed, turnLabelTail)
	trimmed = strings.TrimRight(trimmed, statusEllipsis)
	return strings.TrimRight(trimmed, " ")
}

// headerBlockChars is the comparison form of a text: every character that draws a
// cell but whitespace, i.e. the letters of the question. Whitespace is dropped on
// BOTH sides because a block's break CONSUMES the space it broke at (noteTail):
// the space is a boundary, and a comparison that kept it would report a
// difference exactly where the block legitimately moved a row.
func headerBlockChars(s string) string {
	return strings.Join(strings.Fields(s), "")
}

// headerBlockCollapse keeps the words but normalises their spacing — the form a
// human reads a prefix in.
func headerBlockCollapse(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// headerBlockRemainder is the part of the label the block did NOT draw, derived
// from the DRAWING (never from a second cut): the label minus the drawn prefix,
// as characters.
func headerBlockRemainder(lines []string, label string) string {
	drawn := headerBlockChars(headerBlockDrawn(lines[:len(lines)-1]))
	chars := []rune(headerBlockChars(label))
	suffix := []rune(drawn)
	if len(suffix) > len(chars) || string(chars[:len(suffix)]) != drawn {
		return ""
	}
	return string(chars[len(suffix):])
}
