package replayplay

// turnbody_test.go — A TURN'S BODY CARRIES EVERY TOOL CALL IT DECLARES.
//
// THE DEFECT THIS FILE PINS (measured 2026-10-26, session 80c07a7d, task
// 01a1788f): `gopi session replay <id> --turn 11` printed a header reading
// "turn 11/11  (10 request(s), 8 tool call(s))  calls=8" over a body an operator
// counting the `⚙` marker scored ZERO — while `gopi chat --replay` of the SAME
// turn showed all eight calls with their results.
//
// Two faults, and this file pins BOTH, each in the arm where it can be seen:
//
//  1. the static view labelled a call `TOOL: …` and a result `RESULT:`, names
//     nothing else in the system used, so a reader comparing it against the
//     transcript (whose marker is internal/lineedit's `⚙`/`↳`) saw no tool calls
//     at all;
//  2. a turn's many rounds were FLATTENED into one pile — every round's harness
//     rows, then every round's prose, then every round's calls — so a turn's
//     calls printed at the END, behind rows belonging to other rounds, and on a
//     turn whose context reached the row ceiling the harness rows spent the whole
//     budget and the calls never printed (measured: session 0945c8bd turn 2,
//     `calls=69`, zero call rows).
//
// The fixture is the smallest that can tell the shapes apart: ONE operator row
// answered with THREE exchanges, each carrying its own tool call, and with enough
// harness ticks in the FIRST exchange to spend a naive row budget — so a lay-out
// that flattened the rounds, or that let round 1's context eat the ceiling, loses
// the calls and fails here.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/lineedit"
)

// turnBodyCallCount counts the tool-call rows in a laid-out body: rows OPENING
// with the tool glyph, which is what an operator's `grep -c "⚙"` counts.
func turnBodyCallCount(rows []FrameRow) int {
	n := 0
	for _, r := range rows {
		if r.Role == RoleToolCall && strings.HasPrefix(r.Cells, lineedit.ToolGlyph) {
			n++
		}
	}
	return n
}

// turnBodyResultCount counts the result rows: rows opening with the continuation
// glyph the transcript draws a result under.
func turnBodyResultCount(rows []FrameRow) int {
	n := 0
	for _, r := range rows {
		if r.Role == RoleToolResult && strings.HasPrefix(r.Cells, lineedit.NoteContinuation) {
			n++
		}
	}
	return n
}

// TestTurnBodyCarriesEveryToolCallUnderTheTranscriptsGlyph is the PAIRED
// assertion the epic asks for: the header's `calls=` and the body's call rows
// agree, and they agree at the glyph the rest of the system uses.
func TestTurnBodyCarriesEveryToolCallUnderTheTranscriptsGlyph(t *testing.T) {
	const calls = 3
	dir := t.TempDir()
	writeTimeline(t, dir, calls)
	// The lay-out the VERB prints, from the material the verb loads.
	mat, err := LoadSession(dir)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if len(mat.Turns) != 1 {
		t.Fatalf("turns = %d, want 1", len(mat.Turns))
	}
	turn := mat.Turns[0]
	if len(turn.Calls) != calls {
		t.Fatalf("turn 1 declares %d tool call(s), want %d — the fixture is wrong, not the code", len(turn.Calls), calls)
	}
	rows := TurnBodyRows(turn, FallbackCols)
	bodyCalls := turnBodyCallCount(rows)
	if bodyCalls != calls {
		t.Fatalf("the turn's body carries %d tool-call row(s) opening with %q for %d declared call(s) — a view that prints a count it does not show is the defect this file exists for\n%s",
			bodyCalls, lineedit.ToolGlyph, calls, joinRowCells(rows))
	}
	if got, want := turnBodyResultCount(rows), turnResults(turn); got != want {
		t.Fatalf("the turn's body carries %d result row(s) opening with %q for %d recorded result(s) — each call's return belongs with it\n%s",
			got, lineedit.NoteContinuation, want, joinRowCells(rows))
	}
	// THE SAME BODY through the material's own lay-out, so a re-layout cannot
	// quietly disagree with the static view (they are one function).
	relaid := Relayout(mat, FallbackCols, FallbackRows)
	if got := turnBodyCallCount(relaid.Turns[0].Rows); got != calls {
		t.Fatalf("after a re-layout the turn's body carries %d call row(s), want %d", got, calls)
	}
	// And the OLD spelling is GONE: a row labelled "TOOL: " is the defect, not a
	// synonym (the whole point is that the two views must agree on one marker).
	for _, r := range rows {
		if strings.HasPrefix(r.Cells, "TOOL: ") || strings.HasPrefix(r.Cells, "RESULT:") {
			t.Fatalf("a row still carries the retired label %q:\n%s", r.Cells, joinRowCells(rows))
		}
	}
}

// TestTurnBodyKeepsItsCallsWhenAContextFloodHitsTheCeiling is the ceiling arm:
// a turn whose FIRST exchange carries more context rows than the whole row
// budget must still print that exchange's tool calls (and every later one's).
//
// It is the measured failure (session 0945c8bd turn 2, `calls=69`, zero call
// rows) reduced to a fixture whose flood is computable: a first exchange whose
// harness rows alone exceed roundRowsMax.
func TestTurnBodyKeepsItsCallsWhenAContextFloodHitsTheCeiling(t *testing.T) {
	turn := Round{Index: 1, Kind: RoundTurn, RoundTotal: 2, Prompt: "ask"}
	flood := Round{Index: 1, Kind: RoundModel, Prompt: "ask"}
	for i := 0; i < roundRowsMax; i++ {
		flood.HarnessPrompts = append(flood.HarnessPrompts, fmt.Sprintf("tick %d", i))
	}
	flood.Calls = []ToolCallView{{Name: "bash", InputRaw: `{"command":"echo flooded"}`, Result: "flooded result"}}
	later := Round{Index: 2, Kind: RoundModel}
	later.Calls = []ToolCallView{{Name: "read", InputRaw: `{"path":"/tmp/x"}`, Result: "later result"}}
	turn.Segments = []Round{flood, later}

	rows := TurnBodyRows(turn, FallbackCols)
	if got := turnBodyCallCount(rows); got != 2 {
		t.Fatalf("a context flood dropped tool calls: the body carries %d call row(s), want 2 — the row ceiling must never spend itself on harness ticks before a turn's calls", got)
	}
	if got := turnBodyResultCount(rows); got != 2 {
		t.Fatalf("a context flood dropped tool results: the body carries %d result row(s), want 2", got)
	}
	// The cut CONTEXT is stated, never silent (Rule 15), and the statement names
	// a number: a note without one cannot be checked.
	if !containsRowRole(rows, RoleError) {
		t.Fatalf("the body cut context rows without saying so — the truncation must be PRINTED:\n%s", joinRowCells(rows))
	}
	for _, r := range rows {
		if r.Role == RoleError && !strings.ContainsAny(r.Cells, "0123456789") {
			t.Fatalf("the truncation note %q carries no measured count", r.Cells)
		}
	}
}

// TestTurnBodySeparatesTheTurnsExchangesWithoutNamingANumber pins the ONE thing
// the body must NOT do: put a second address space on the operator surface. The
// separator groups the rows by exchange and carries no number, so `--turn N`'s
// view stays in ONE unit (the operator's ruling, 2026-10-22 — `steps` is retired
// and a round header here would be the same mistake under a new word).
func TestTurnBodySeparatesTheTurnsExchangesWithoutNamingANumber(t *testing.T) {
	dir := t.TempDir()
	writeTimeline(t, dir, 3)
	mat, err := LoadSession(dir)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	rows := TurnBodyRows(mat.Turns[0], FallbackCols)
	seps := 0
	for _, r := range rows {
		if strings.TrimSpace(r.Cells) == turnBodySegmentSeparator {
			seps++
		}
	}
	if want := len(mat.Turns[0].Segments) - 1; seps != want {
		t.Fatalf("the body prints %d separator(s) between %d exchange(s), want %d — the grouping must be visible",
			seps, len(mat.Turns[0].Segments), want)
	}
	for _, r := range rows {
		if strings.HasPrefix(r.Cells, "── round ") {
			t.Fatalf("a turn's body names a round (%q): the operator surface has ONE unit, and a round number here is the second numbering the sub-verb already retired once", r.Cells)
		}
	}
}

// TestRoundRowStillHasItsOwnOrder is the ABSENCE arm: the per-round views keep
// the round's own lay-out exactly as it was — this change added a TURN body, it
// did not move a round's rows around.
func TestRoundRowStillHasItsOwnOrder(t *testing.T) {
	dir := t.TempDir()
	writeTimeline(t, dir, 3)
	mat, err := LoadSession(dir)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	round := mat.Rounds[0]
	if len(round.Segments) != 0 {
		t.Fatalf("a ROUND row carries %d segment(s) — only a TURN is made of rounds", len(round.Segments))
	}
	rows := RoundsRow(round, FallbackCols)
	if got, want := turnBodyCallCount(rows), len(round.Calls); got != want {
		t.Fatalf("the round's own view carries %d call row(s) for %d call(s)", got, want)
	}
	// The separator is a TURN concept: a single round's rows never carry one.
	for _, r := range rows {
		if strings.TrimSpace(r.Cells) == turnBodySegmentSeparator {
			t.Fatalf("a single round's view carries the turn separator:\n%s", joinRowCells(rows))
		}
	}
}

// turnResults is how many of a turn's calls recorded a result — the number the
// body must print, counted from the material rather than from the fixture.
func turnResults(turn Round) int {
	n := 0
	for _, c := range turn.Calls {
		if strings.TrimSpace(c.Result) != "" {
			n++
		}
	}
	return n
}

// containsRowRole reports whether any laid-out row carries the given role.
func containsRowRole(rows []FrameRow, role RowRole) bool {
	for _, r := range rows {
		if r.Role == role {
			return true
		}
	}
	return false
}

// joinRowCells renders a body for a failure message.
func joinRowCells(rows []FrameRow) string {
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(r.Cells)
		b.WriteByte('\n')
	}
	return b.String()
}

// TestOnlyARealCallRowOpensWithTheToolGlyph pins the RULER, not just the
// content: a turn's own text may quote a chat transcript, and a tool result may
// print a file — both can carry lines that BEGIN with the tool glyph. When those
// lines sit at column 0 the cheap check an operator (and this very file) uses to
// count what a turn did — `grep -c '^⚙ '` — disagrees with the header it exists
// to verify.
//
// MEASURED (2026-10-26, session 41e5dade, task 01a178ed): `--turn 50 | grep -c
// '^⚙ '` printed 9 over a header reading `calls=7`, and `--turn 62` printed 14
// for the same `calls=7`. The extra rows were CONTENT: a pasted transcript and a
// read file's output. So the fixture below is the smallest thing that reproduces
// it — a turn whose PROSE and whose tool RESULT each carry a line opening with
// `⚙ ` — and the assertion is the operator's own ruler at column 0.
//
// The inset is two cells (`roundIndent`, the same allowance the `⚙ `/`↳ ` marks
// take), so nothing shifts by a partial cell on a CJK row.
func TestOnlyARealCallRowOpensWithTheToolGlyph(t *testing.T) {
	const quotedCall = "⚙ bash(command=\"echo pasted\")"
	const resultLine = "⚙ bash(command=\"echo from-a-file\")"
	// BOTH lay-outs a turn may take: no segments (the round's own rows) and
	// segments (the turn body's tool rows). They are different code paths and the
	// defect lived in both.
	base := Round{Index: 1, Kind: RoundModel, Prompt: "count what I did"}
	base.Text = []string{"here is my transcript:\n" + quotedCall + "\nit answered"}
	base.Calls = []ToolCallView{{
		Name:     "read",
		InputRaw: `{"path":"/tmp/dump.txt"}`,
		Result:   "file body:\n" + resultLine + "\nend",
	}}
	cases := []struct {
		name string
		turn Round
	}{
		{"round-rows", base},
		{"turn-body-tool-rows", func() Round {
			r := Round{Index: 1, Kind: RoundTurn, RoundTotal: 1}
			r.Segments = []Round{base}
			return r
		}()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows := TurnBodyRows(tc.turn, FallbackCols)
			// The operator's ruler, verbatim: rows that OPEN with the tool glyph at
			// column 0.
			opensWithGlyph := 0
			for _, r := range rows {
				if strings.HasPrefix(strings.TrimRight(r.Cells, " "), lineedit.ToolGlyph) {
					opensWithGlyph++
				}
			}
			if want := declaredCalls(tc.turn); opensWithGlyph != want {
				t.Fatalf("`grep -c '^⚙ '` on this body scores %d row(s) for a header reading calls=%d — a quoted transcript at column 0 is what makes the ruler lie:\n%s",
					opensWithGlyph, want, joinRowCells(rows))
			}
			// …and the quoted content IS still printed, inset — the fix must not be
			// "drop what confuses the count".
			for _, want := range []string{strings.TrimSpace(quotedCall), strings.TrimSpace(resultLine)} {
				found := false
				for _, r := range rows {
					if strings.HasPrefix(strings.TrimRight(r.Cells, " "), roundIndent+want) {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("the quoted line %q is not printed as an inset content row — the content must move, never vanish:\n%s", want, joinRowCells(rows))
				}
			}
			// EVERY content row opens at the content column: one cell used twice
			// would make the body a staircase, so the opening space is asserted as a
			// PREFIX of cells, not as "some indentation".
			indent := strings.Repeat(" ", roundContentIndentCells())
			if !strings.HasPrefix(roundIndent, indent) || roundContentIndentCells() != displayCells(roundResultLabel) {
				t.Fatalf("the content inset (%d cell(s), %q) is not the same width as the result mark (%q) — the body would shift",
					roundContentIndentCells(), roundIndent, roundResultLabel)
			}
		})
	}
}

// declaredCalls is how many tool calls the laid-out rows are ABOUT: a turn the
// fixture handed segments declares its calls on the segments (that is the
// lay-out's own source), a plain round declares them on itself.
func declaredCalls(r Round) int {
	if len(r.Segments) == 0 {
		return len(r.Calls)
	}
	n := 0
	for _, seg := range r.Segments {
		n += len(seg.Calls)
	}
	return n
}

// displayCells is the cell width of a marker string, measured the way the canvas
// measures: one wide rune occupies two cells.
func displayCells(s string) int {
	n := 0
	for _, r := range s {
		n += runeCells(r)
	}
	return n
}

// TestTurnBodyFullSpellsTheInputWholeAndTheDefaultStaysBounded is the UNIT arm
// of the `--full` pair the operator asked for (2026-10-29): the DEFAULT lay-out
// keeps the bounded spelling AND its printed `…(+N runes)` note, while
// TurnBodyRowsFull spells the recorded input whole. Both are asserted on ONE
// fixture so neither can pass by accident: the full arm must CONTAIN what the
// bounded arm CUTS, and the bounded arm must still say how much it cut.
func TestTurnBodyFullSpellsTheInputWholeAndTheDefaultStaysBounded(t *testing.T) {
	// The recorded command is longer than the bound, and its LAST word appears
	// nowhere else in the fixture — so finding it proves the whole input printed.
	const tail = "the-last-word-nobody-can-reach-under-the-bound"
	turn := Round{Index: 1, Kind: RoundTurn, RoundTotal: 1, Prompt: "run it"}
	call := Round{Index: 1, Kind: RoundModel}
	call.Calls = []ToolCallView{{
		Name:     "bash",
		InputRaw: `{"command":"` + strings.Repeat("long-and-boring ", 20) + tail + `"}`,
		Result:   "ok",
	}}
	turn.Segments = []Round{call}

	bounded := joinRowCells(TurnBodyRows(turn, 400))
	if !strings.Contains(bounded, "…(+") {
		t.Fatalf("the DEFAULT turn body no longer says it cut the input — the bounded spelling and its note are the contract --full must not change:\n%s", bounded)
	}
	if strings.Contains(bounded, tail) {
		t.Fatalf("the DEFAULT turn body already carries the input's tail %q — the compact spelling is supposed to cut it:\n%s", tail, bounded)
	}

	full := joinRowCells(TurnBodyRowsFull(turn, 400))
	if !strings.Contains(full, tail) {
		t.Fatalf("TurnBodyRowsFull cut the recorded input: the tail %q is missing:\n%s", tail, full)
	}
	if strings.Contains(full, "…(+") {
		t.Fatalf("TurnBodyRowsFull still carries a truncation note — the whole input prints, so there is nothing to count:\n%s", full)
	}
	if got := turnBodyCallCount(TurnBodyRowsFull(turn, 400)); got != 1 {
		t.Fatalf("TurnBodyRowsFull laid out %d call row(s), want 1 — expanding an input must not change how many calls the body shows", got)
	}
}
