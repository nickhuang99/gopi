package replayplay

// turnbody.go — ONE TURN, LAID OUT ROUND BY ROUND.
//
// THE DEFECT THIS FILE EXISTS FOR (measured 2026-10-26, session 80c07a7d, task
// 01a1788f): `gopi session replay <id> --turn 11` printed a header reading
// "turn 11/11  (10 request(s), 8 tool call(s))  calls=8" over a body a reader
// counting the `⚙` marker scored ZERO — while `gopi chat --replay` of the SAME
// turn showed every one of those eight calls with its result.
//
// Two faults, and neither was a missing byte:
//
//  1. NOTHING NAMED A TOOL ROW THE WAY THE REST OF THE SYSTEM DOES. The static
//     view labelled a call `TOOL: bash(command=…)` and its result `RESULT:`,
//     while the live status line, the chat transcript and the view the operator
//     compares against all use internal/lineedit's TWO GLYPHS — `⚙` for the call
//     and `↳` for the result. A reader (and an operator grepping for `⚙`) could
//     not see tool calls that were printed in full. The labels now ARE those
//     glyphs (material.go), so the static view, the player and the transcript
//     agree by construction.
//
//  2. THE TURN'S TEN ROUNDS WERE FLATTENED INTO ONE PILE. roundRows lays out ONE
//     round in its own order; a TURN is many rounds, and building it by
//     concatenating every round's harness rows, then every round's prose, then
//     every round's calls put a turn's eight tool calls at the END of the view,
//     behind rows belonging to OTHER rounds. Measured on turn 11: rows 3-19 were
//     all harness ticks, rows 20-171 all prose, and the eight calls only began at
//     row 172. On a turn large enough to hit the row ceiling the harness rows
//     spent the whole budget and the calls never printed at all (measured:
//     session 0945c8bd turn 2, header `calls=69`, zero call rows in the body).
//
// So the turn body is laid out THE WAY IT HAPPENED — round 1's own rows, then
// round 2's, and so on — each round under a one-row header naming it, and each
// round's TOOL ROWS RESERVED their share of the row budget so a chatty round can
// never crowd out another round's calls.
//
// NOTHING HERE ADDS A UNIT: a header names a ROUND in the round address space
// (`--round N`, `gopi session log <id> N`) — the same number the listing's "turn"
// column points at. There is no third numbering (the retired `steps` word stays
// retired), and nothing is reconstructed from dump frames.

import (
	"fmt"
	"strings"
)

const (
	// turnBodySegmentSeparator is the ONE row between two of a turn's rounds.
	//
	// IT NAMES NOTHING, deliberately. `--turn N` is the operator surface and its
	// address space holds ONE unit (the operator's ruling, 2026-10-22): a header
	// naming a round here would put a second numbering on the user surface — the
	// defect this sub-verb already paid for once. The separator carries the
	// GROUPING (which rows came from one exchange, which from the next) and no
	// number at all; a reader who wants a single exchange asks `--round N`, whose
	// view says what it is.
	turnBodySegmentSeparator = "· · ·"
	// turnBodySegmentDroppedFmt is the row a segment carries when its CONTEXT rows
	// had to be cut to keep the turn inside the ceiling. The count is measured,
	// and the tool rows are never part of what is dropped. It says "this turn",
	// not "this round", for the same reason the separator names nothing.
	turnBodySegmentDroppedFmt = "(%d row(s) of this turn's context are not laid out; its tool rows are)"
)

// TurnBodyRows lays out ONE TURN for a canvas of the given width: every round of
// the turn IN THE ORDER IT RAN, each under a header naming the round, each row
// tagged with the role the renderer colours by, and every tool call with its
// result.
//
// The row ceiling of a turn's body is internal/replayplay's own (roundRowsMax),
// and it applies PER ROUND — the same bound `--round N` has always used
// (roundRows). Each of the turn's rounds therefore gets its own full ceiling, and
// the tool rows are reserved on top of it, so a round carrying many calls is never
// clipped by a budget a different round spent. A round whose CONTEXT had to be cut
// says so in a row of its own.
//
// IT WAS ONCE SHARED, and that was a defect with a measured cost (2026-11-22,
// session 1c42c121, task 01a1fff6): one ceiling split among all of a turn's
// rounds, first come first served, meant a 28-round turn (4961 rows needed
// against a 4000 ceiling) spent it in its first ~25 rounds and gave every later
// round ZERO — the FINAL round, which is where a run records its `[IMPORTANT]`
// self-record note, laid out 1 row of 88. `--turn 1` then printed a body whose
// ending was missing while every shorter turn showed its own, and the note was in
// session.jsonl all along (the loss was this allocation, not the data). A turn is
// many rounds; borrowing the word "rounds-left" from a different problem did not
// make one round's bound into a material-wide one.
//
// With Dims.FullLayout the ceiling is LIFTED (Dims.rowCeiling) and no round is
// cut at all: the debug view's whole point is that nothing was left out, so it
// prints no truncation note either — a "0 row(s) … not laid out" note would be a
// statement about nothing.
//
// A turn with no recorded segments (a material built before Round.Segments
// existed, or a turn whose span held no content) falls back to its own laid-out
// rows: the view is never empty, and there is no second implementation of
// roundRows here.
func TurnBodyRows(r Round, cols int) []FrameRow {
	if cols <= 0 {
		cols = roundStaticCols
	}
	return turnBodyRows(r, Dims{Cols: cols})
}

// TurnBodyRowsAt lays ONE TURN out with a caller's own lay-out (Dims): the same
// entry point the material's load and every re-lay use, exported so a caller
// that re-builds a turn's body outside the material (the static `--turn N` view)
// cannot honour one lay-out property and forget the other. The width in dims is
// the caller's; a zero width falls back to the package's documented geometry.
func TurnBodyRowsAt(r Round, dims Dims) []FrameRow {
	if dims.Cols <= 0 {
		dims.Cols = roundStaticCols
	}
	return turnBodyRows(r, dims)
}

// TurnBodyRowsFull lays ONE TURN out as the DEBUG body: every tool call's
// recorded input spelled IN FULL (no `…(+N runes)` note) and the row ceiling
// LIFTED (Dims.FullLayout), so no round of the turn reports context it did not
// lay out.
//
// It exists for the operator's debug view (`gopi session replay --turn N --full`)
// and for nothing else: the DEFAULT spelling stays bounded and the default body
// stays clipped, because the compact view is the one that fits a narrow terminal.
// The width is the caller's, and both properties are properties of the LAY-OUT
// (Dims), so a caller that re-lays the same material at another width keeps them
// by asking again here — or, better, by handing the material's own Dims to the
// lay-out (cmd/agent's static turn view does the latter, so the two properties
// cannot be honoured by one path and forgotten by the other).
func TurnBodyRowsFull(r Round, cols int) []FrameRow {
	if cols <= 0 {
		cols = roundStaticCols
	}
	return turnBodyRows(r, Dims{Cols: cols, FullToolInput: true, FullLayout: true})
}

// turnBodyRows is TurnBodyRows with the material's own Dims, for the lay-out
// sites that already hold them (the load, a re-layout, the player).
func turnBodyRows(r Round, dims Dims) []FrameRow {
	if dims.Cols <= 0 {
		dims.Cols = roundStaticCols
	}
	if len(r.Segments) == 0 {
		return roundRows(r, dims)
	}
	var rows []FrameRow
	// ceiling is this lay-out's row ceiling for the WHOLE turn, or 0 for none
	// (Dims.rowCeiling): the debug view lifts it, so no round of the turn is cut.
	ceiling := dims.rowCeiling()
	// The session's opening request rides inside the turn's first segment when the
	// log handed it to no round of its own; it is what the turn was answering, so
	// it opens the body rather than being dropped by this lay-out.
	if s := strings.TrimSpace(r.Opening); s != "" {
		rows = appendWrappedRows(rows, RolePrompt, roundPromptLabel+s, dims, ceiling)
	}
	for i := range r.Segments {
		seg := r.Segments[i]
		// THE TOOL ROWS ARE BUILT FIRST AND NEVER CLIPPED — they are the rows this
		// view exists for. The CONTEXT (prompt, harness ticks, reasoning, prose) is
		// what a ceiling may cut, never the calls: a round whose harness ticks alone
		// reach the ceiling used to lose its calls entirely (measured: session
		// 0945c8bd turn 2, `calls=69`, zero call rows).
		tools := toolRows(seg, dims)
		// THE CONTEXT IS LAID OUT UNCAPPED HERE, AND CUT ONCE, BELOW. roundRows
		// applies the same per-round ceiling AND prints its own `(round truncated:
		// N more row(s) …)` row, so letting it cap this segment first produced TWO
		// notes on one segment and a count that could be wrong: the second clip then
		// spent its quota on the first note and reported "1 row(s) not laid out" for
		// a round that was 500 rows over (measured with a 4500-tick segment). Cutting
		// in ONE place — clipContextRows, which is the pass that knows the tool rows
		// are exempt — makes the printed count the whole truth.
		context := roundRows(contextOnly(seg), dims.uncapped())
		// THE CEILING IS PER ROUND, NOT PER TURN (measured 2026-11-22, session
		// 1c42c121, task 01a1fff6). It used to be the turn's shared remainder —
		// `ceiling - len(rows) - len(tools)` — so a turn of many rounds spent the
		// whole budget in its first rounds and every LATER one got zero rows: on
		// that turn (28 rounds, 4961 rows needed against a 4000 ceiling) the FINAL
		// round laid out 1 row of 88, so `--turn 1` printed a body whose ending was
		// missing while `--turn 2` and every shorter turn showed theirs. The note
		// that vanished was the run's `[IMPORTANT]` self-record block: present in
		// session.jsonl, printed by `--full`, lost by this allocation.
		//
		// roundRowsMax's own contract is "how many rows ONE round contributes", and
		// `--round N` has always applied it that way (roundRows). A turn is many
		// rounds; sharing one round's ceiling among all of them was the defect, and
		// it never bounded what it claimed to — the TOOL rows are exempt from it by
		// design, and they are the rows that dominate a material.
		kept, dropped := withBudget(context, ceiling, ceiling)
		rows = appendSegmentSeparator(rows, i, dims)
		rows = append(rows, kept...)
		if dropped > 0 {
			rows = append(rows, FrameRow{
				Cells: fitReportRow(fmt.Sprintf(turnBodySegmentDroppedFmt, dropped), dims.Cols),
				Role:  RoleError,
			})
		}
		rows = append(rows, tools...)
	}
	return rows
}

// withBudget clips rows against quota, or returns them WHOLE when the lay-out has
// no ceiling (ceiling 0, Dims.rowCeiling). It is the one place "no ceiling" is
// translated for clipContextRows, whose own quota means the opposite thing.
func withBudget(rows []FrameRow, quota, ceiling int) (kept []FrameRow, dropped int) {
	if ceiling == 0 {
		return rows, 0
	}
	return clipContextRows(rows, quota)
}

// contextOnly is the same segment WITHOUT its tool calls: what a row ceiling is
// allowed to cut. Everything else about it is unchanged, so the context rows are
// laid out by roundRows exactly as before.
func contextOnly(seg Round) Round {
	c := seg
	c.Calls = nil
	return c
}

// appendSegmentSeparator puts the between-rounds row in front of every segment
// but the first — the row that makes a turn read as the sequence of exchanges it
// was, without naming a second unit on the operator surface.
func appendSegmentSeparator(rows []FrameRow, i int, dims Dims) []FrameRow {
	if i == 0 {
		return rows
	}
	return append(rows, FrameRow{Cells: fitCells(turnBodySegmentSeparator, dims.Cols), Role: RoleHarness})
}

// toolRows is a round's laid-out CALL and RESULT rows, in order — the rows a
// turn's view may never drop.
//
// THE MARK IS THE BLOCK'S, NOT THE FIRST ROW'S. A call row opens at column 0 with
// `⚙ ` (the glyph the transcript, the live status line and the operator's own
// `grep -c` count), so an operator counting `^⚙ ` counts CALLS. Its wrap
// continuation therefore carries `↳ ` rather than moving to column 0 — a
// continuation is part of the same call, and a bare continuation would let a
// count of `^⚙ ` be right while the row below it looked like a different row.
// The RESULT rows lay their content out through contentLines, whose inset is the
// same cells, so a result that itself contains `⚙ `-leading lines (a pasted chat
// transcript, a file the call printed) cannot be counted as calls — the defect
// this lay-out now forbids (measured 2026-10-26: `--turn 50 | grep -c '^⚙ '` = 9
// over a header reading `calls=7`).
func toolRows(seg Round, dims Dims) []FrameRow {
	var out []FrameRow
	for _, c := range seg.Calls {
		for _, line := range wrapPrefixed(roundToolLabel+toolCallLine(c, dims), "", roundResultLabel, dims.Cols) {
			out = append(out, FrameRow{Cells: fitCells(line, dims.Cols), Role: RoleToolCall})
		}
		if res := strings.TrimSpace(c.Result); res != "" {
			out = append(out, FrameRow{Cells: fitCells(roundResultLabel, dims.Cols), Role: RoleToolResult})
			for _, line := range contentLines(res, dims.Cols) {
				out = append(out, FrameRow{Cells: fitCells(line, dims.Cols), Role: RoleToolResult})
			}
			continue
		}
		out = append(out, FrameRow{Cells: fitCells(roundResultLabel+roundNoResultLabel, dims.Cols), Role: RoleError})
	}
	return out
}

// clipContextRows keeps the round's rows in their own order while never dropping
// one of its TOOL rows, and reports how many CONTEXT rows were left out.
//
// Tool rows are preserved by COUNT first, so a round that spends its budget on
// harness ticks still shows its calls — the fault this file was written for.
func clipContextRows(rows []FrameRow, quota int) (kept []FrameRow, dropped int) {
	if quota >= len(rows) {
		return rows, 0
	}
	if quota < 0 {
		quota = 0
	}
	toolCount := 0
	for _, r := range rows {
		if isToolRow(r.Role) {
			toolCount++
		}
	}
	contextAllowance := quota - toolCount
	if contextAllowance < 0 {
		// The tool rows alone exceed the quota: they are kept anyway (the quota was
		// raised for them upstream) and the context gets nothing.
		contextAllowance = 0
	}
	for _, r := range rows {
		if isToolRow(r.Role) {
			kept = append(kept, r)
			continue
		}
		if contextAllowance > 0 {
			kept = append(kept, r)
			contextAllowance--
			continue
		}
		dropped++
	}
	return kept, dropped
}

// isToolRow says whether a row is a tool call or its result — the class this
// lay-out refuses to drop.
func isToolRow(role RowRole) bool {
	return role == RoleToolCall || role == RoleToolResult
}

// appendWrappedRows wraps one text into rows and appends as many as the ceiling
// allows. It is for a row whose FIRST line wears a label of its own (the opening
// request's `PROMPT: `) and whose continuation therefore carries the result mark,
// the same discipline toolRows keeps. A ceiling of 0 is NO CEILING
// (Dims.rowCeiling), which is why the test is `> 0`.
func appendWrappedRows(rows []FrameRow, role RowRole, text string, dims Dims, ceiling int) []FrameRow {
	for _, line := range wrapPrefixed(text, "", roundResultLabel, dims.Cols) {
		if ceiling > 0 && len(rows) >= ceiling {
			return rows
		}
		rows = append(rows, FrameRow{Cells: fitCells(line, dims.Cols), Role: role})
	}
	return rows
}
