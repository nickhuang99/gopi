package replayplay

// render.go — ONE FRAME TO BYTES ON A TERMINAL, cell-exact.
//
// THE COLOUR RULE, and it has two arms that must never be confused:
//
//  1. A dump frame carries the TRUECOLOUR the surface was actually painted with
//     (lineedit's per-cell attr_runs). That is copied VERBATIM: the frame claims
//     to be the screen, so its colour must be the screen's.
//
//  2. Material with no recorded colour (scrollback, a jsonl timeline) — and the
//     player's own chrome — is drawn in a palette WE DESIGNED. It is never
//     presented as "the original colours": a replay that invented colour and
//     called it the recording's would be a fabricated observation.
//
// CELL EXACTNESS. The dump's grid stores one rune per CELL, a wide rune followed
// by its continuation cell. This renderer therefore emits the row's runes and
// TRACKS THE CELL COLUMN itself (a wide rune costs 2, a combining rune 0), which
// is what makes an attr_run's Col line up with the text it styles. Cutting a wide
// rune's continuation cell emits a space instead — never half a glyph, which
// would shift every later column on the row for real terminals too.

import (
	"strconv"
	"strings"

	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/sessionevent"
	"github.com/nickhuang99/gopi/internal/sgr"
)

// Palette is the colour scheme this package paints with, per ROLE rather than
// per position. Callers name a role; the hex behind it lives here (Rule 1b: no
// colour literal anywhere else).
type Palette struct {
	Ground       string // the background everything is drawn on
	Body         string // recorded material with no colour of its own
	Chrome       string // the status line and the frame borders
	ChromeBg     string // the status line's own ground
	Accent       string // labels, the counter, the help key column
	Scrollback   string // material replayed from the scrollback (older than the screen)
	Event        string // material rebuilt from the event timeline
	Prompt       string // the request a round answered
	Thinking     string // the round's reasoning
	Text         string // the round's own prose
	ToolCall     string // a tool invocation
	ToolResult   string // what the tool answered
	Harness      string // a harness-injected user row (the framework's own traffic)
	ErrorRole    string // a row that reports a failure or an absence
	ScrollNotice string // the status line's scroll indicator
}

// DefaultPalette is the designed palette. Its ground and body text are
// internal/lineedit's own ground constants (the pair the chat surface's gold
// chrome is composed against) — referenced, not re-invented, so a replay drawn
// in the same terminal as the chat surface sits on the same ground.
func DefaultPalette() Palette {
	return Palette{
		Ground:     lineedit.GroundBackgroundHex,
		Body:       lineedit.GroundForegroundHex,
		Chrome:     replayChromeHex,
		ChromeBg:   replayChromeBgHex,
		Accent:     replayAccentHex,
		Scrollback: replayScrollbackHex,
		Event:      replayEventHex,
		Prompt:     replayPromptHex,
		Thinking:   replayThinkingHex,
		Text:       lineedit.GroundForegroundHex,
		ToolCall:   replayToolCallHex,
		ToolResult: replayToolResultHex,
		// The harness role is the DIMMEST colour in the palette on purpose: a
		// clock tick or a checkpoint is framework traffic, and it must not read
		// as something the operator said even for one glance.
		Harness:      replayHarnessHex,
		ErrorRole:    replayErrorHex,
		ScrollNotice: replayScrollNoticeHex,
	}
}

// The palette's own two colours plus its role tints. Each is written ONCE
// and referenced by the palette above and by the tests.
const (
	// replayChromeHex is the status line's text colour (the gold of the chat
	// surface's own chrome, so the player looks like part of the same program).
	replayChromeHex = "#ffd700"
	// replayChromeBgHex is the status line's reverse ground (a dark slate).
	replayChromeBgHex = "#1f2a3a"
	// replayAccentHex is used for the key column of the help overlay and for the
	// frame-kind tag.
	replayAccentHex = "#7fd1ff"
	// replayScrollbackHex tints history — the part of the recording that is not
	// the live screen — so "what was on screen" is distinguishable at a glance.
	replayScrollbackHex = "#8a9bb0"
	// replayEventHex tints a frame rebuilt from the jsonl timeline: it is OUR
	// rendering of the events, not a recorded screen, and it says so in colour
	// as well as in the status line.
	replayEventHex = "#c9a7ff"
	// replayPromptHex tints the request a round answered, so the question and
	// the answer are never the same colour.
	replayPromptHex = "#ffd27f"
	// replayThinkingHex tints reasoning: present, but visibly not the round's
	// answer.
	replayThinkingHex = "#9aa7b8"
	// replayToolCallHex tints a tool invocation.
	replayToolCallHex = "#7fd1ff"
	// replayToolResultHex tints what a tool answered.
	replayToolResultHex = "#8fd694"
	// replayHarnessHex tints a harness-injected row. It is deliberately close to
	// the ground: DIM, so the framework's own traffic never competes with the
	// round's content.
	replayHarnessHex = "#5c6b7a"
	// replayErrorHex tints a row that reports a failure or an absence.
	replayErrorHex = "#ff8f8f"
	// replayScrollNoticeHex tints the status line's scroll indicator (an
	// inverted ground, so it is visible on the chrome row).
	replayScrollNoticeHex = "#ffd700"
)

// RenderOptions is what one repaint is told about the playback.
type RenderOptions struct {
	// RoundIndex is the 1-based round being drawn and RoundTotal the count —
	// the same units `gopi session log <id> <N>` prints, so the status line and
	// `--round N` cannot disagree about which round is on screen.
	RoundIndex int
	RoundTotal int
	// Round is the round itself: its rows are laid out already (material.go),
	// and this renderer only paints them.
	Round Round
	// DumpFrame, when set, is a SCREEN frame from the session's dump instead of
	// a round: the playback has two kinds of material and this is the second
	// one. ScreenFrame names which dump frame it is ("scrollback 2/24",
	// "screen") for the status line.
	DumpFrame   Frame
	ScreenFrame string
	// Cols / Rows are the CANVAS to paint into (the terminal's size). A round
	// wider or taller than the canvas is clipped; a narrower one is padded.
	Cols int
	Rows int
	// Scroll is how many rows the view is scrolled DOWN inside the current
	// round (0 = the top of the round). It is what PgUp/PgDn move, and it is
	// clamped by the renderer against the number of rows the round actually
	// has, so a scroll past the end shows the round's end rather than an empty
	// screen.
	Scroll int
	// Paused / Speed / Source are the status line's own facts.
	Paused    bool
	Speed     float64
	Source    string
	SessionID string
	// Notice is an optional one-line message shown on the status line (an
	// error, or a hint like "end of recording").
	Notice string
	// Palette is what the rounds are drawn in (DefaultPalette() unless a caller
	// wants its own).
	Palette Palette
	// Truecolor says whether the sink can render 24-bit colour. False degrades
	// to the 16-colour palette (see degradeStyle) — the recorded run is still
	// copied in ORDER, so the picture keeps its shape without claiming colours
	// the terminal cannot show.
	Truecolor bool
	// Help, when non-empty, is painted as a full-screen overlay INSTEAD of the
	// round (the `?` key).
	Help []string
}

// StatusRow returns the row index the status line occupies for a canvas of
// rows rows. It is the LAST row: one definition, so the renderer, the help
// overlay and the tests agree about which row is chrome.
func StatusRow(rows int) int { return rows - 1 }

// bodyRows is how many rows of a canvas the material owns: the canvas minus the
// status row. It is the ONE definition of the split, so what the material was
// laid out for (roundDims) and what the renderer paints cannot drift.
func bodyRows(rows int) int {
	if rows <= 1 {
		return 0
	}
	return rows - 1
}

// Render paints the current round (or dump frame) plus its status line and
// returns the bytes to write.
//
// The caller writes the result WITHOUT moving the cursor first: every line is
// positioned absolutely (CUP) and erased (EL), so a repaint never depends on
// where the previous one left the cursor — the property that makes a stepped
// replay free of smearing.
func Render(o RenderOptions) string {
	pal := o.Palette
	if pal == (Palette{}) {
		pal = DefaultPalette()
	}
	cols, rows := o.Cols, o.Rows
	if cols <= 0 || rows <= 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(groundSeq(pal.Ground, o.Truecolor))
	if len(o.Help) > 0 {
		paintHelp(&b, o.Help, cols, rows, pal, o.Truecolor)
		return b.String()
	}
	status := StatusRow(rows)
	visible, scroll := o.visibleRows()
	for i := 0; i < rows; i++ {
		b.WriteString(sgr.CursorSeq(i+1, 1))
		b.WriteString(sgr.EraseLineSeq())
		if i == status {
			b.WriteString(statusSeq(o, cols, pal, scroll))
			continue
		}
		if i >= len(visible) {
			continue
		}
		b.WriteString(rowSeq(visible[i], o.FrameKind(), cols, pal, o.Truecolor))
	}
	b.WriteString(sgr.Seq(sgr.ResetParams()))
	return b.String()
}

// RoundRows is the rows of the current material — the round's own rows, or the
// dump frame's.
func (o RenderOptions) RoundRows() []FrameRow {
	if len(o.Round.Rows) > 0 {
		return o.Round.Rows
	}
	return o.DumpFrame.Rows
}

// FrameKind is the kind tag the status line shows for what is on screen: the
// dump frame's kind for screen material, else the round's own kind.
func (o RenderOptions) FrameKind() FrameKind {
	if len(o.Round.Rows) > 0 {
		if o.Round.Kind == RoundPrompt {
			return FrameEvent
		}
		return FrameRound
	}
	if o.DumpFrame.Kind != "" {
		return o.DumpFrame.Kind
	}
	return FrameRound
}

// visibleRows returns the rows to paint (the canvas's worth) and the CLAMPED
// scroll offset they start at. Clamping here rather than at the key handler is
// deliberate: the round's height is known only once it is laid out, so the
// renderer is the first place the offset can be checked against it — and a
// clamped offset is what the status line reports, so the indicator can never
// claim a position the screen is not at.
func (o RenderOptions) visibleRows() ([]FrameRow, int) {
	all := o.RoundRows()
	body := bodyRows(o.Rows)
	if body <= 0 {
		return nil, 0
	}
	scroll := o.Scroll
	if max := len(all) - body; scroll > max {
		scroll = max
	}
	if scroll < 0 {
		scroll = 0
	}
	// The tail is padded with blank rows so a scrolled-to-the-end round leaves
	// no stale pixels above the status line (the renderer erases each row it
	// paints, and a row it does not paint keeps what was there).
	out := make([]FrameRow, 0, body)
	for i := scroll; i < len(all) && len(out) < body; i++ {
		out = append(out, all[i])
	}
	for len(out) < body {
		out = append(out, FrameRow{Cells: strings.Repeat(" ", o.Cols)})
	}
	return out, scroll
}

// MaxScroll is the largest scroll offset the current round admits (0 when the
// round fits). It is what the scroll keys clamp against at the point of the
// press, so the key that hits the end can SAY so instead of appearing dead.
func (o RenderOptions) MaxScroll() int {
	max := len(o.RoundRows()) - bodyRows(o.Rows)
	if max < 0 {
		return 0
	}
	return max
}

// rowSeq renders one frame row: its cells with the styles recorded for them,
// padded to cols, and never longer (so a recorded row cannot wrap and shove the
// status line off the bottom).
//
// The COLUMN a style is looked up at is the row's SLOT index, because that is
// what the dump's attr runs index (internal/lineedit's own reading: a run at
// Col=12 covers slots 12..12+Cells-1). The column the terminal ends up at is
// the accumulated WIDTH, and the two coincide only because a continuation slot
// is skipped — see isContinuationCell.
func rowSeq(row FrameRow, kind FrameKind, cols int, pal Palette, truecolor bool) string {
	var b strings.Builder
	if len(row.Runs) == 0 {
		// No recorded colour: this material is drawn in OUR palette by kind —
		// and a ROUND's row is drawn by its ROLE, which is the finer split
		// (a round's prompt and its tool output are not one colour).
		b.WriteString(seqForHex(rowHex(row, kind, pal), truecolor))
		b.WriteString(fitCells(row.Cells, cols))
		return b.String()
	}
	text := fitCells(row.Cells, cols)
	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		if isContinuationCell(runes, i) {
			continue
		}
		b.WriteString(attrSeq(styleAt(row.Runs, i), truecolor))
		b.WriteRune(runes[i])
	}
	return b.String()
}

// rowHex is the palette colour one colourless row is drawn in: a round's row by
// its ROLE, a dump frame's row by its KIND.
func rowHex(row FrameRow, kind FrameKind, pal Palette) string {
	if row.Role != RoleBody {
		return roleHex(row.Role, pal)
	}
	return kindHex(kind, pal)
}

// roleHex maps a round row's role onto the palette. An unknown role falls back
// to the body colour rather than to the accent: a role this renderer has not
// learned about must look like ordinary text, never like chrome.
func roleHex(role RowRole, pal Palette) string {
	switch role {
	case RolePrompt:
		return pal.Prompt
	case RoleThinking:
		return pal.Thinking
	case RoleText:
		return pal.Text
	case RoleToolCall:
		return pal.ToolCall
	case RoleToolResult:
		return pal.ToolResult
	case RoleHarness:
		return pal.Harness
	case RoleError:
		return pal.ErrorRole
	}
	return pal.Body
}

// kindHex is the palette role a colourless frame's material is drawn in.
func kindHex(kind FrameKind, pal Palette) string {
	switch kind {
	case FrameScrollback:
		return pal.Scrollback
	case FrameEvent, FrameRound:
		return pal.Event
	}
	return pal.Body
}

// styleAt returns the recorded style covering one CELL column, or the zero style
// (the terminal's default) when no run covers it. Runs are over the full row
// width, so a miss means the material recorded nothing for that cell — which is
// a real answer, not an error.
func styleAt(runs []lineedit.DumpAttrRun, col int) lineedit.DumpAttrRun {
	for _, r := range runs {
		if col >= r.Col && col < r.Col+r.Cells {
			return r
		}
	}
	return lineedit.DumpAttrRun{}
}

// attrSeq renders one recorded run's style as an SGR sequence, VERBATIM: the
// dump's own field vocabulary ("bold", "dim", "fg=rgb:fff8dc", "bg=rgb:131926",
// "reverse", …) is the interface internal/lineedit owns, and it is translated
// here field by field. An unknown field is ignored rather than guessed at.
//
// With truecolor false, an rgb field is degraded to the nearest of the sixteen
// named colours (see degradeRGB): the run still starts at the same cell with the
// same emphasis, so degradation changes colour and never layout.
func attrSeq(run lineedit.DumpAttrRun, truecolor bool) string {
	style := run.Style
	if strings.TrimSpace(style) == "" || style == styleDefaultWord {
		return sgr.Seq(sgr.ResetParams())
	}
	var params []string
	for _, field := range strings.Fields(style) {
		switch {
		case field == attrBold:
			params = append(params, sgr.BoldParams())
		case field == attrDim:
			params = append(params, sgr.DimParams())
		case field == attrItalic:
			params = append(params, sgr.ItalicParams())
		case field == attrUnderline:
			params = append(params, sgr.UnderlineParams())
		case field == attrReverse:
			params = append(params, sgr.ReverseParams())
		case strings.HasPrefix(field, attrFgPrefix):
			params = append(params, colourParams(field[len(attrFgPrefix):], false, truecolor))
		case strings.HasPrefix(field, attrBgPrefix):
			params = append(params, colourParams(field[len(attrBgPrefix):], true, truecolor))
		}
	}
	if len(params) == 0 {
		return sgr.Seq(sgr.ResetParams())
	}
	return sgr.Seq(strings.Join(params, string(sgr.ParamSeparator)))
}

// The dump's own style vocabulary (internal/lineedit's sgr.Style.String form).
const (
	styleDefaultWord = "default"
	attrBold         = "bold"
	attrDim          = "dim"
	attrItalic       = "italic"
	attrUnderline    = "underline"
	attrReverse      = "reverse"
	attrFgPrefix     = "fg="
	attrBgPrefix     = "bg="
	// rgbWordPrefix introduces a 24-bit value ("fg=rgb:fff8dc"); anything else
	// after the "=" is one of the sixteen NAMED colours sgr.ColorName produces.
	rgbWordPrefix = "rgb:"
)

// hexDigits is the width of an rgb word's value.
const hexDigits = 6

// colourParams renders one colour field: an rgb value when the sink can show it,
// else the nearest named colour.
func colourParams(value string, background, truecolor bool) string {
	if !strings.HasPrefix(value, rgbWordPrefix) {
		return namedColourParams(value, background)
	}
	r, g, bl, ok := parseRGB(strings.TrimPrefix(value, rgbWordPrefix))
	if !ok {
		return namedColourParams("default", background)
	}
	if !truecolor {
		idx := degradeRGB(r, g, bl)
		return sgr.FgParams(idx)
	}
	if background {
		return sgr.TruecolorBgParams(r, g, bl)
	}
	return sgr.TruecolorFgParams(r, g, bl)
}

// namedColourParams maps a dump's named colour ("red", "bright-red",
// "colour-93") onto an sgr palette call. An unreadable name paints the
// terminal's default rather than a colour nobody asked for.
func namedColourParams(name string, background bool) string {
	idx, ok := namedColourIndex(name)
	if !ok || idx == sgr.DefaultColor {
		// "default" (and a name this reader cannot resolve) restores the
		// terminal's own colour, which is sgr's DefaultFg/DefaultBg — never
		// palette entry 0, and never an extended-colour parameter (sgr's
		// DefaultColor is -1, which FgParams would render as `38;5;-1`).
		if background {
			return strconv.Itoa(sgr.DefaultBg)
		}
		return strconv.Itoa(sgr.DefaultFg)
	}
	if background {
		return sgr.BgParams(idx)
	}
	return sgr.FgParams(idx)
}

// namedColourIndex resolves one named colour through sgr's OWN naming (ColorName
// is the inverse of this lookup) rather than a second table here (Rule 13).
func namedColourIndex(name string) (int, bool) {
	if name == styleDefaultWord {
		return sgr.DefaultColor, true
	}
	for _, idx := range namedColourIndices {
		if sgr.ColorName(idx) == name {
			return idx, true
		}
	}
	if strings.HasPrefix(name, colourNumberWordPrefix) {
		if n, err := strconv.Atoi(strings.TrimPrefix(name, colourNumberWordPrefix)); err == nil && n >= 0 && n < sgr.Palette256 {
			return n, true
		}
	}
	return 0, false
}

// colourNumberWordPrefix is sgr.ColorName's prefix for an unnamed palette entry.
const colourNumberWordPrefix = "colour-"

// namedColourIndices is the sixteen named entries (plus the default), in the
// order the ANSI palette defines them. It is a LIST, not a table, because the
// name of each entry belongs to internal/sgr (namedColourIndex asks it).
var namedColourIndices = []int{
	sgr.DefaultColor,
	sgr.Black, sgr.Red, sgr.Green, sgr.Yellow, sgr.Blue, sgr.Magenta, sgr.Cyan, sgr.White,
	sgr.BrightBlack, sgr.BrightRed, sgr.BrightGreen, sgr.BrightYellow,
	sgr.BrightBlue, sgr.BrightMagenta, sgr.BrightCyan, sgr.BrightWhite,
}

// parseRGB splits a six-hex-digit value into r,g,b.
func parseRGB(v string) (r, g, b int, ok bool) {
	if len(v) != hexDigits {
		return 0, 0, 0, false
	}
	r64, err1 := strconv.ParseUint(v[0:2], 16, 8)
	g64, err2 := strconv.ParseUint(v[2:4], 16, 8)
	b64, err3 := strconv.ParseUint(v[4:6], 16, 8)
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, 0, 0, false
	}
	return int(r64), int(g64), int(b64), true
}

// degradeRGB maps a truecolour value onto the nearest of the sixteen named
// palette entries, by squared distance in RGB. It is only reached when the sink
// cannot show 24-bit colour.
func degradeRGB(r, g, b int) int {
	best, bestDist := sgr.White, -1
	for _, idx := range namedColourIndices {
		if idx < 0 || idx >= len(ansiRGB) {
			continue
		}
		c := ansiRGB[idx]
		dr, dg, db := r-c[0], g-c[1], b-c[2]
		d := dr*dr + dg*dg + db*db
		if bestDist < 0 || d < bestDist {
			best, bestDist = idx, d
		}
	}
	return best
}

// ansiRGB is the sixteen-entry reference palette the degradation measures
// against (the xterm defaults). It exists ONLY for the degrade path: whenever
// the sink can show truecolour the recorded value is used untouched.
var ansiRGB = [16][3]int{
	{0, 0, 0}, {205, 0, 0}, {0, 205, 0}, {205, 205, 0},
	{0, 0, 238}, {205, 0, 205}, {0, 205, 205}, {229, 229, 229},
	{127, 127, 127}, {255, 0, 0}, {0, 255, 0}, {255, 255, 0},
	{92, 92, 255}, {255, 0, 255}, {0, 255, 255}, {255, 255, 255},
}

// groundSeq paints the whole canvas in the palette's ground before a frame is
// drawn, so a shorter frame's leftover rows do not keep the previous frame's
// pixels (the state a stepped replay would otherwise smear).
func groundSeq(hex string, truecolor bool) string {
	var b strings.Builder
	b.WriteString(paintBackground(hex, truecolor))
	b.WriteString(sgr.EraseDisplaySeq())
	return b.String()
}

// paintBackground emits the SGR that sets a background from a "#rrggbb" value,
// degrading to the nearest named colour when the sink has no truecolour.
func paintBackground(hex string, truecolor bool) string {
	r, g, bl, ok := parseHexColour(hex)
	if !ok {
		return sgr.Seq(sgr.ResetParams())
	}
	if !truecolor {
		return sgr.Seq(sgr.BgParams(degradeRGB(r, g, bl)))
	}
	return sgr.Seq(sgr.TruecolorBgParams(r, g, bl))
}

// seqForHex is the FOREGROUND counterpart of paintBackground: the palette role
// a colourless frame's text is drawn in.
func seqForHex(hex string, truecolor bool) string {
	r, g, bl, ok := parseHexColour(hex)
	if !ok {
		return sgr.Seq(sgr.ResetParams())
	}
	if !truecolor {
		return sgr.Seq(sgr.FgParams(degradeRGB(r, g, bl)))
	}
	return sgr.Seq(sgr.TruecolorFgParams(r, g, bl))
}

// parseHexColour reads a "#rrggbb" colour (the spelling every palette constant
// in this repo uses).
func parseHexColour(hex string) (r, g, b int, ok bool) {
	return parseRGB(strings.TrimPrefix(strings.TrimSpace(hex), "#"))
}

// statusSeq renders the one chrome row: what is playing, where in it, which
// keys do what, and — when the view is scrolled inside its round — how far. It
// is held to the canvas width — a status line that wrapped would push a row off
// the bottom of a fixed canvas.
func statusSeq(o RenderOptions, cols int, pal Palette, scroll int) string {
	total := o.RoundTotal
	if total <= 0 {
		total = 1
	}
	index := o.RoundIndex
	if index <= 0 {
		index = 1
	}
	// WHAT GIVES WAY, IN ORDER. The parts of this line are not equally
	// important, and every one of them can be acted on: the rate changes with
	// `+`/`-`, the scroll offset with PgUp/PgDn, the position with n/p/Home/End,
	// and a NOTICE reports that the key just pressed DID something. So the line
	// sheds its STATIC fields first — the session id (which the operator typed)
	// and the material's kind — and only then truncates. Measured on a
	// 60-column terminal with the naive order: the notice was cut to "(top o" and
	// then `+` appeared to do nothing at all because the rate was gone — a key
	// that looks broken at exactly the width where its feedback matters.
	kind := string(o.FrameKind())
	state := statusPlaying
	if o.Paused {
		state = statusPaused
	}
	// THE POSITION NAMES ITS UNIT (operator's ruling, 2026-10-22): "turn 3/8",
	// not a bare "3/8" whose unit the reader had to infer. The word comes from
	// sessionevent, where the session surfaces' unit vocabulary lives (Rule 14),
	// so the status line and the printed tables cannot drift apart.
	pos := sessionevent.CounterUnitTurnOne + " " + strconv.Itoa(index) + "/" + strconv.Itoa(total)
	// A TURN STEP ALREADY SAYS "turn" IN ITS POSITION, so the kind field would
	// repeat it ("turn 3/8" beside "turn"). It is dropped for that one kind and
	// kept for every other, where the tag is the only thing naming what is on
	// screen (a prompt entry, a dump frame).
	if o.Round.Kind == RoundTurn {
		kind = ""
	}
	rate := strconv.FormatFloat(o.Speed, 'g', 3, 64) + "x"
	// Assembled from the least droppable outward, so the widest form that fits is
	// the one painted and nothing is ever cut mid-field.
	build := func(withID, withKind, withScroll bool) string {
		s := ""
		if withID {
			s = " " + PlaybackCommand + " " + shortID(o.SessionID)
		}
		if withKind && kind != "" {
			s += " " + kind
		}
		s += " " + pos
		if withScroll && scroll > 0 {
			s += " " + scrollIndicator(scroll)
		}
		s += "  " + state + "  " + rate
		if o.Notice != "" {
			s += "  " + o.Notice
		}
		return s
	}
	left := build(true, true, true)
	right := statusKeys
	var b strings.Builder
	b.WriteString(sgr.Seq(bgHexParams(pal.ChromeBg, o.Truecolor) + string(sgr.ParamSeparator) + fgHexParams(pal.Chrome, o.Truecolor)))
	// THE HINT IS DROPPED FIRST: it is a summary of the `?` overlay, which the
	// operator can always open.
	if lineedit.DisplayWidth(left)+lineedit.DisplayWidth(right) >= cols {
		right = ""
	}
	for _, drop := range []func() string{
		func() string { return build(false, true, true) },   // the session id (the operator typed it)
		func() string { return build(false, false, true) },  // the material's kind
		func() string { return build(false, false, false) }, // the scroll marker (PgUp/PgDn still say so)
	} {
		if lineedit.DisplayWidth(left) < cols {
			break
		}
		left = drop()
	}
	if lineedit.DisplayWidth(left) >= cols {
		left = truncateCells(left, cols)
	}
	if w := lineedit.DisplayWidth(left) + lineedit.DisplayWidth(right); w < cols {
		left += strings.Repeat(" ", cols-w)
	}
	b.WriteString(left + right)
	return b.String()
}

// scrollIndicator is the status line's scroll marker: how many rows the view is
// scrolled down inside its round. It is printed ONLY when the offset is not
// zero, so a round that fits carries no marker claiming otherwise.
func scrollIndicator(scroll int) string {
	return "↓" + strconv.Itoa(scroll)
}

// truncateCells cuts s to at most cols cells without splitting a wide rune.
func truncateCells(s string, cols int) string {
	w := 0
	var b strings.Builder
	for _, r := range s {
		cw := runeCells(r)
		if w+cw > cols {
			break
		}
		b.WriteRune(r)
		w += cw
	}
	return b.String()
}

// bgHexParams / fgHexParams render one palette hex as absolute SGR parameters
// (used together in one sequence, so the status line's colours are set in a
// single write).
func bgHexParams(hex string, truecolor bool) string {
	r, g, b, ok := parseHexColour(hex)
	if !ok {
		return sgr.ResetParams()
	}
	if !truecolor {
		return sgr.BgParams(degradeRGB(r, g, b))
	}
	return sgr.TruecolorBgParams(r, g, b)
}

func fgHexParams(hex string, truecolor bool) string {
	r, g, b, ok := parseHexColour(hex)
	if !ok {
		return sgr.ResetParams()
	}
	if !truecolor {
		return sgr.FgParams(degradeRGB(r, g, b))
	}
	return sgr.TruecolorFgParams(r, g, b)
}

// paintHelp draws the key map over the canvas (the `?` overlay). It is drawn on
// the ground like a frame, so returning from it repaints cleanly.
func paintHelp(b *strings.Builder, help []string, cols, rows int, pal Palette, truecolor bool) {
	text := sgr.Seq(fgHexParams(pal.Body, truecolor))
	accent := sgr.Seq(fgHexParams(pal.Accent, truecolor))
	for i := 0; i < rows; i++ {
		b.WriteString(sgr.CursorSeq(i+1, 1))
		b.WriteString(sgr.EraseLineSeq())
		if i >= len(help) {
			continue
		}
		line := help[i]
		if strings.HasPrefix(line, helpAccentPrefix) {
			b.WriteString(accent + truncateCells(strings.TrimPrefix(line, helpAccentPrefix), cols))
			continue
		}
		b.WriteString(text + truncateCells(line, cols))
	}
	b.WriteString(sgr.Seq(sgr.ResetParams()))
}

// helpAccentPrefix marks a help line the overlay prints in the accent colour
// (the key column), so the help TEXT and its styling are one value.
const helpAccentPrefix = "\x00"

// The status line's two words and its key hint. They are constants because the
// driver test matches on them (Rule 1b).
const (
	statusPlaying = "playing"
	statusPaused  = "PAUSED"
	// statusKeys is the hint on the right of the status line. It is short by
	// design: the full map is the `?` overlay. The two pairs are spelled by
	// their own responsibility — the vertical arrows and PgUp/PgDn move the
	// VIEW inside the round, the horizontal arrows step between rounds.
	statusKeys = " [space]pause [n/<]step [p/>]back [↑/↓][PgUp/PgDn]scroll [Home/End/G] [q]uit [?]help "
)

// shortID abbreviates a session id for the status line (the first eight
// characters, the spelling `gopi session list` teaches as a handle).
func shortID(id string) string {
	if len(id) <= shortIDLen {
		return id
	}
	return id[:shortIDLen]
}

// shortIDLen is how many characters of a session id the status line shows.
const shortIDLen = 8
