package lineedit

import "strings"

// layers.go — THE FOUR-LAYER FORMAT TABLE of a chat turn: ONE place that says
// what each layer LOOKS like, so the drawn format of the transcript is a value a
// reader can consult rather than a switch spread over the render path.
//
// DRAW ORDER, and it is why the request row is drawn by BeginTurn rather than
// by the first answer line. The four layers are painted APPEND-ONLY (each row
// into the band's live row, with the band's one scroll), but the streams that
// produce them arrive in an order of their own: a tool call is announced BEFORE
// the answer's first fragment (the agent reports the call, runs the tool, then
// streams the text), so the row that physically lands first is the code-tool
// layer's. A transcript that showed `⚙ bash …` above `▸ what is in …` would be a
// lie about what happened — the call is the answer to the request, not its
// preface.
//
// The request row is therefore committed immediately after the turn header, by
// the same call that opens the turn: it is the FIRST row of the body, before any
// note and before any answer fragment, so the causal order IS the append order
// and no reordering machinery is needed at all. (The alternative — drawing the
// operator's line with the first ANSWER row, i.e. a repair that inserts a row
// above rows already painted — needs a reservation, a cap and a repair path; the
// request row being first makes all three unnecessary.)

// AnswerLayer names which of the turn's four layers one committed row belongs
// to.
type AnswerLayer int

const (
	// LayerRequest is the operator's own submitted line, drawn once at the head
	// of the turn it belongs to.
	LayerRequest AnswerLayer = iota
	// LayerThinking is the model's reasoning.
	LayerThinking
	// LayerResponse is the model's answer body.
	LayerResponse
	// LayerCodeTool is the code/tool family: a fenced or diff line of the answer
	// body, or a committed tool-activity row.
	LayerCodeTool
)

// The layer gutters — the marker column each layer opens with. They are the
// text half of the table, and they are CONSTANTS (Rule 1) because each is a
// format decision: a typo in a bare literal is a transcript that silently stops
// being scannable.
const (
	// RequestGutter opens the operator's own line. `▸` (U+25B8, "black right
	// pointing small triangle") is the marker a reader already reads as "this is
	// what was ASKED" — and, like every other gutter here, it is exactly two
	// cells wide, so the four layers share one column grid.
	RequestGutter = "▸ "
	// ToolGutter opens a tool-activity row: the tool glyph (ToolGlyph, the one
	// definition of `⚙` — palette.go) plus the same one-cell separation every
	// other layer gutter carries. It is the value cmd/agent's termToolLine opens
	// its line with, so the status row and the committed row cannot disagree
	// about how a call is marked.
	ToolGutter = ToolGlyph + " "
)

// layerMarker is THE TABLE: for each layer, the marker column its rows open
// with. It is a switch rather than a map so a layer added without an entry
// returns "" from an unhandled arm (a compile-time-complete list is what
// layers_test.go's enumeration pins: every AnswerLayer must appear here).
func layerMarker(layer AnswerLayer) string {
	switch layer {
	case LayerRequest:
		return RequestGutter
	case LayerThinking:
		return ThinkingGutter
	case LayerResponse:
		return TurnMarker
	case LayerCodeTool:
		return ToolGutter
	}
	return ""
}

// layerContinuation returns the marker a layer uses for a row that CONTINUES
// the row above it (a wrapped body row). Only the response layer has one: the
// reasoning and the request are committed line by line (their own text breaks),
// and the tool layer's continuation rows are handled by the note path, which has
// its own rule (NoteContinuation, same two cells).
func layerContinuation(layer AnswerLayer) string {
	if layer == LayerResponse {
		return TurnMarkerSplit
	}
	return ""
}

// layerForLine reports which layer a BODY line of the response belongs to: an
// ordinary prose line is the response itself, while a fenced or diff line is the
// code half of the code-tool family. It is the ONE place the composer's verdict
// (LineStyle) is translated into layer vocabulary, so the render path asks the
// table for a style with the layer the line really is.
func layerForLine(ls LineStyle) AnswerLayer {
	switch ls {
	case LineCode, LineDiffAdd, LineDiffDel:
		return LayerCodeTool
	}
	return LayerResponse
}

// layerStyleFor returns the palette field a layer draws in, as a function of the
// layer and — for the two layers whose rows carry a body — the body line's own
// style (the composer's verdict).
//
// It is the ONE mapping from layer to emphasis. A monochrome palette's fields
// are all the zero Style, so this function returns "nothing" for every layer
// there and the layer distinction survives as TEXT (the gutters above) — which
// is the monochrome rule palette.go states: colour is emphasis on top of the
// text, never the text's meaning.
func layerStyleFor(pal Palette, layer AnswerLayer, ls LineStyle) Style {
	switch layer {
	case LayerRequest:
		return pal.Prompt
	case LayerThinking:
		return pal.Thinking
	case LayerCodeTool:
		if ls == LineCode || ls == LineDiffAdd || ls == LineDiffDel {
			// A fenced/diff line is code: the body's own code style, which the
			// palette keeps distinct from the tool family's.
			return styleFor(pal, ls)
		}
		return pal.ToolCall
	}
	// The response body: its line's own kind (plain prose, code, a diff).
	return styleFor(pal, ls)
}

// ── the request row: the operator's own line on the band ─────────────────────

// requestRowMaxRows caps how many band rows a single request row may occupy.
// A pasted line of a hundred rows would otherwise push the whole turn (and the
// band's own history) off the screen; the cap is the same kind of budget the
// status row's one-screen-row invariant imposes, and a request longer than it is
// CUT WITH A MARK rather than silently truncated (cutField's own marker).
const requestRowMaxRows = 3

// requestRowBreak is what a line break inside a pasted request becomes: the same
// break-of-field marker the turn header uses between the turn number and the
// prompt, so the two chrome rows of a turn punctuate alike.
const requestRowBreak = StatusSep

// requestRowRows lays the operator's own line out as the band rows it will
// occupy, the layer's gutter on the FIRST row and a blank gutter of the same
// width on every continuation (exactly the indent writeRow gives a wrapped body
// row, so the block keeps one left edge). The body is wrapped at the width the
// gutter leaves, and a run longer than requestRowMaxRows is cut with the status
// row's own marker — never silently: a request the terminal could not hold must
// read as shortened.
func requestRowRows(prompt string, cols int) []string {
	marker := layerMarker(LayerRequest)
	body := strings.TrimSpace(strings.ReplaceAll(prompt, "\n", requestRowBreak))
	if body == "" || cols <= 0 {
		return nil
	}
	width := cols - displayWidth(marker)
	if width <= 0 {
		return nil
	}
	parts := wrapCols(body, width)
	if len(parts) > requestRowMaxRows {
		// The cap, and it is a CUT rather than a truncation: the rows the cap
		// dropped are rejoined into the last kept row and that row is cut to the
		// width with the product's own marker (cutField), so a shortened request
		// reads as shortened. Cutting the last retained row on its own would not
		// do it — that row is already within the width by construction, so the
		// marker would never appear.
		rest := strings.Join(parts[requestRowMaxRows-1:], "")
		parts = append(parts[:requestRowMaxRows-1], cutField(rest, width))
	}
	pad := strings.Repeat(" ", displayWidth(marker))
	rows := make([]string, 0, len(parts))
	for i, part := range parts {
		if i == 0 {
			rows = append(rows, marker+part)
			continue
		}
		rows = append(rows, pad+part)
	}
	return rows
}

// drawRequestRow draws the turn's request row — the operator's own line, as the
// head of the block it belongs to — if it is not on the band yet. It is called
// by BEGIN TURN, once per turn, immediately after the header row and before
// anything else the turn will paint (a tool call or the first answer fragment):
// the request is then physically above its body on the grid, and the append
// order is the causal order.
//
// EVERYTHING ABOUT IT IS THE ORDINARY APPEND-ONLY PATH: each row goes through
// writeRow, so it is cut by the ONE component that owns the geometry, and
// finalizeLiveRow promotes it with the band's one scroll. No placement is aimed
// at an existing row, no committed row is rewritten and no cursor moves
// backwards: a request row is a row the band had not shown yet.
//
// It reports whether it drew. False means the caller must not treat the request
// as on screen — no active band, no prompt, or a terminal too narrow to hold the
// gutter and one cell of the operator's line.
func (r *AnswerRenderer) drawRequestRow() bool {
	if r == nil || r.out == nil || !r.out.Active() || r.requested {
		return false
	}
	rows := requestRowRows(r.turnPrompt, r.cols)
	if len(rows) == 0 {
		return false
	}
	r.requested = true
	style := layerStyleFor(r.pal, LayerRequest, LinePlain)
	// The page is put back exactly as it was found: a caller that is already
	// suppressing the marker column (the answer did not enter through BeginTurn)
	// must not have its state stolen by the request row's own draw.
	wasNoting := r.noting
	defer func() { r.noting = wasNoting }()
	for _, text := range rows {
		// The gutter is already IN the text (the layer's own marker on the first
		// row, the aligned pad on the rest), so the row is committed with the
		// suppressed marker column: the note path's own reason applies here too,
		// a row that writes its own glyph at column 0 must not then be indented
		// by the turn's two-cell gutter.
		r.noting = true
		r.writeRow(answerRow{text: text, st: style, allowCutLabel: true}, text)
		r.finalizeLiveRow()
		r.noting = false
	}
	return true
}

// LayerMarkerForTest is the layer table read from OUTSIDE this package: the
// marker a layer's rows open with. It exists because the claim the table makes
// is measured on a REAL run's screen artifact (testsuite's pty evidence), whose
// reader lives in another package — a test-only copy of the table there would be
// the second definition this table exists to prevent.
func LayerMarkerForTest(layer AnswerLayer) string { return layerMarker(layer) }

// AnswerLayerForTest exposes the layer vocabulary to the evidence harness (see
// LayerMarkerForTest): the four names, so a reader outside this package can name
// the layer it found on the grid.
const (
	LayerRequestForTest  = LayerRequest
	LayerThinkingForTest = LayerThinking
	LayerResponseForTest = LayerResponse
	LayerCodeToolForTest = LayerCodeTool
)
