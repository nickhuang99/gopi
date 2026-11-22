package lineedit

import (
	"fmt"
	"strings"

	"github.com/nickhuang99/gopi/internal/sgr"
)

// palette.go — the chat surfaces' colour/emphasis layer.
//
// IT IS A DEGRADING LAYER, NOT A SURFACE TREATMENT. Every field below is a
// NAME for an emphasis (a named SGR emitter), and the layer has exactly two
// states: a table that emits SGR bytes and the ZERO table, which emits
// NOTHING — not "a reset", not an empty-looking sequence, zero bytes. That is
// what makes the "colour off" acceptance byte-exact rather than
// approximately-clean, and it is why MonoPalette is the zero value of Palette
// rather than a hand-written table of no-ops: a style that was never set
// cannot leak a byte.
//
// NO INFORMATION MAY BE CARRIED BY COLOUR ALONE (the monochrome rule, checked
// by TestPaletteCarriesNoInformationAlone): the palette may distinguish the
// prompt from the answer, the label from the live hint, a diff addition from a
// removal — but every distinction it draws is also legible as text in the
// NO_COLOR path, because the same glyphs and the same words are emitted either
// way. Colour is emphasis on top of the text, never the text's meaning.
//
// Sizing is untouched by construction: a style is applied AFTER the layout has
// computed rows and columns from the RAW text (see Renderer.Redraw,
// statusLine, AnswerRenderer.writeLive), so an SGR byte can never be counted
// as a display cell (the shear defect this ordering exists to prevent).

// sgrOverhead is the byte headroom a styled string reserves when it is built:
// an opening sequence plus a reset, the most a single styled span can add. It is
// a Builder capacity hint only — a string longer than the hint still grows
// normally, so the value can never cut text.
const sgrOverhead = 16

// SGR sequences. They are named (Rule 1b) because a typo in a bare "\x1b[31m" is
// a terminal that silently ignores us.
//
// They are built from escCSI — the editor's ONE name for the escape introducer
// (raw.go: escCSI = sgr.CSI, the vocabulary internal/sgr owns) — so the package
// spells the introducer once and a change to the wire bytes cannot leave one
// emitter composing the old shape (Rule 13/14).
//
// They are VARS, not consts, because escCSI is now a VALUE (raw.go, composed
// from internal/sgr) rather than a literal: Go requires a constant expression
// for a const, so a var block is the price of ONE definition of the introducer.
// They are assigned once at init and never written afterwards.
var (
	sgrReset   = escCSI + "0m"
	sgrBold    = escCSI + "1m"
	sgrDim     = escCSI + "2m"
	sgrItalic  = escCSI + "3m"
	sgrUnder   = escCSI + "4m"
	sgrReverse = escCSI + "7m"

	sgrBlack   = escCSI + "30m"
	sgrRed     = escCSI + "31m"
	sgrGreen   = escCSI + "32m"
	sgrYellow  = escCSI + "33m"
	sgrBlue    = escCSI + "34m"
	sgrMagenta = escCSI + "35m"
	sgrCyan    = escCSI + "36m"
	sgrGray    = escCSI + "90m"
)

// statusGold is the FLOOR palette's gold for the status row: SGR 33, the named
// "yellow" foreground.
//
// WHY A NAMED 8-COLOUR CODE RETURNS TO THE ROW the theme-dependence rule bans.
// The two spellings that PIN a colour need a terminal that speaks them: `38;2;r;g;b`
// needs a 24-bit advertisement and `38;5;n` needs a TERM that declares the 256
// palette (both are derived, not declared — see Palette.RequiredDepth). An
// 8-colour terminal has neither, and SGR 33 is the only yellow the floor can
// name: the alternative is the SGR 39 "default colour" the operator's screen
// showed, which is a colour too and the one they reported as missing.
//
// IT IS A DIFFERENT RISK FROM THE CODE THAT WAS FIXED. 80a53890's defect was SGR
// 90 and SGR 2: 2 is a BLEND TOWARD THE BACKGROUND (faint by construction on any
// theme, and on a light theme a blend toward a light background is close to
// invisible), and 90 is the theme's "bright black", a grey by convention. A hue
// has no such property — a theme may call 33 a different yellow, a mustard or an
// amber, but it stays a warm, saturated colour at the theme's normal foreground
// weight, which is the contrast the user's own profile promises. The choice is
// therefore: the ACTUAL Hermes gold where the terminal can express it (the two
// tables below), and the name of gold — never grey, never a blend — on the floor.
//
// It is not `sgrYellow` at the call site because the two uses are different
// decisions that happen to share one code: Warning is the amber of "a degradation
// you can continue past", and a later change to either must not drag the other.
// Naming this one is what keeps that edit honest.
var statusGold = escCSI + "33m"

// Style is one named emphasis: the SGR sequence that opens it, and nothing
// else. The ZERO VALUE IS THE MONOCHROME STYLE — it opens nothing, so Wrap
// returns its input unchanged and Open is empty. Every consumer therefore
// checks nothing and branches nowhere: a monochrome palette needs no special
// case anywhere in the render paths, which is what makes "colour off" one
// decision (see ColourWanted) instead of a condition sprinkled over the code.
type Style struct {
	// open is the SGR sequence the style emits, or "" for the monochrome
	// style. It is unexported so the only ways to obtain a style are the
	// named fields of BasicPalette and the zero value.
	open string
}

// Open returns the sequence that opens the style (empty for the monochrome
// style). It is what a caller that composes its own sequence (rather than
// wrapping a whole string) writes.
func (s Style) Open() string { return s.open }

// Close returns the sequence that ends the style — the reset, or empty for the
// monochrome style, so a caller that emitted Open never emits a reset it did
// not open.
func (s Style) Close() string {
	if s.open == "" {
		return ""
	}
	return sgrReset
}

// Empty reports whether the style emits nothing (the monochrome style).
func (s Style) Empty() bool { return s.open == "" }

// Wrap renders text under the style: open, text, reset — or text unchanged for
// the monochrome style, and unchanged for empty text (an empty string wrapped
// would otherwise be three bytes of styling around nothing, which the
// zero-SGR assertion would rightly count as a violation).
func (s Style) Wrap(text string) string {
	if s.open == "" || text == "" {
		return text
	}
	return s.open + text + sgrReset
}

// StyleFromSGR is the BRIDGE from the cell-grid vocabulary (sgr.Style, what a
// screen cell records) to this package's drawing style: the style whose Open is
// that cell style's ABSOLUTE sequence.
//
// WHY IT EXISTS. A caller that composes a row out of styled RUNS — cmd/agent's
// tool-activity row, which must colour a glyph and a tool name differently from
// the command they mark — has its emphasis in sgr.Style, because that is the ONE
// definition of "what colour is this cell" and the dumped grid is compared
// against it. The band draws with Style, and Style's `open` is unexported so
// there is exactly one way to obtain one. This is that way, and it is a
// conversion rather than a second colour source: the sequence is
// sgr.Style.Seq(), which resets first, so a converted style's bytes do not
// depend on what was in force before it.
//
// A cell style that carries NOTHING (sgr.Default) converts to the monochrome
// style, so the colour-off invariant holds through this bridge too: an
// unemphasised run costs no escape byte.
func StyleFromSGR(st sgr.Style) Style {
	if st.Equal(sgr.Default()) {
		return Style{}
	}
	return Style{open: st.Seq()}
}

// Palette is the ONE named source of the chat surfaces' emphasis: one field per
// element the surfaces draw. Callers ask for a field by name ("the prompt", "a
// diff removal"); nobody composes an SGR sequence at a call site.
//
// The zero value is MonoPalette; PaletteFor returns the table for a capability
// decision, so a caller resolves colour ONCE and then renders without further
// conditions.
type Palette struct {
	// Banner is the startup banner (the one line above the prompt).
	Banner Style
	// Prompt is the editing line's prompt ("you> ").
	Prompt Style
	// Buffer is the editing line's USER INPUT: every cell of the editing line
	// that follows the prompt — what the user is typing right now.
	//
	// WHY THE USER'S OWN TEXT IS COLOURED (the C1 reversal, 2026-09-27). The
	// surface shipped with the buffer unstyled "by design": the prompt took the
	// palette and the buffer fell through to the terminal's DEFAULT foreground.
	// Measured on the operator's screen — row `you> …` carried
	// `0:5=fg=colour-186 bold 5:112=default` — the editing line therefore held a
	// pale-gold prompt and default-coloured input on the SAME row as a gold
	// status row, so the one surface the user looks at while typing was the only
	// chrome on screen that was not part of the system's colour. The operator
	// reversed the verdict (internal/lineedit/render.go's header records the why
	// in full).
	//
	// THE OLD OBJECTION IS ANSWERED BY WHAT THIS FIELD CARRIES, NOT BY IGNORING
	// IT. "Colouring it puts the palette's judgement on the user's input" was
	// the reason for the default colour; the value here is the SAME gold family
	// the rest of the interface is made of (see each table — the floor's named
	// gold, the cube's #FFD700, Hermes' primary), so it marks "this is the
	// editing line", never "this input is good/bad". Nothing of the buffer's
	// MEANING is encoded, so the monochrome rule above still holds: the gold
	// carries no information the text does not already carry.
	//
	// IT DEGRADES THROUGH THE SAME TIER MACHINERY AS THE STATUS ROW — one value
	// per table, and the ZERO value on MonoPalette, which is what keeps
	// colour-off output byte-identical: the field was added to a table whose
	// zero value emits nothing, so the monochrome path cannot leak a byte.
	Buffer Style
	// Draft is the editing line's DRAFT SEGMENT (draft.go): text a streaming
	// producer is still refining and that the operator has not accepted yet —
	// live dictation, drawn where it will land but visibly not yet theirs.
	//
	// WHY IT IS A FIELD OF ITS OWN rather than a reuse of Buffer or Hint. The
	// role is a THIRD one: Buffer is "this is the editing line" (the operator's
	// own text), Hint is chrome the surface writes about itself, and this is
	// CONTENT that is not the operator's yet. A reader must be able to tell
	// "what I said is already in my line" from "the recogniser is still guessing",
	// and that distinction is exactly what the finish key acts on — so the two
	// cannot share an emphasis. The PRECEDENT is Reasoning a few fields up: two
	// decisions that may happen to share a colour stay two names.
	//
	// IT CARRIES NO INFORMATION ALONE (the monochrome rule at the head of this
	// file): the draft's own text says what it is, and the settle key's note says
	// the rest. On the monochrome tier the field is zero and the segment is
	// drawn exactly as the buffer is, which is honest — the words are the same
	// words either way.
	Draft Style
	// StatusLabel is the status row's left part (the rolling hint: model,
	// cadence, turn count, token budget).
	StatusLabel Style
	// StatusLive is the status row's right part (what is happening now).
	StatusLive Style
	// Thinking is the live hint while a turn is in flight — a stronger form of
	// StatusLive, so "the agent is working" is visible at a glance rather than
	// only as different words.
	Thinking Style
	// ToolRow is the tool-activity row on the status line ("⚙ bash …").
	ToolRow Style
	// ToolCall is a COMMITTED tool-activity row in the answer band (the durable
	// record of a call, as opposed to ToolRow's transient one-line hint). It is
	// a separate field rather than a reuse of ToolRow because the two surfaces
	// want a different COLOUR for the same role: the status row is chrome and
	// takes the GOLD shade of its table, while a committed band row is the
	// transcript — part of the answer's own reading matter — and takes the gold
	// of a heavier weight (see each table, and the rule on BasicPalette below).
	ToolCall Style
	// Answer is the model's answer body.
	Answer Style
	// AnswerBold is the answer body's INTRA-LINE emphasis: a span the body's
	// emphasis layer marked (internal/lineedit's EmphasisBold). It is the
	// "stronger form of the body" the palette already has a precedent for
	// (Thinking is the stronger form of StatusLive), and it is a field of its
	// own rather than a flag on Answer because the two are chosen by different
	// decisions: the LINE's style by the composer, the SPAN's by the tokenizer.
	AnswerBold Style
	// Code is a fenced code line inside the answer.
	Code Style
	// DiffAdd is a line ADDED by a diff inside the answer.
	DiffAdd Style
	// DiffDel is a line REMOVED by a diff inside the answer.
	DiffDel Style
	// Hint is secondary chrome (the tools-ON hint, key reminders).
	Hint Style
	// Reasoning is THE REASONING ROW's style, in both places the transcript
	// draws one: a turned-in chat's landed body row (LayerThinking —
	// internal/lineedit's layers.go layerStyleFor) and a carrier's live
	// transcript row fed by the engine's reasoning stream on the wire
	// (`event: thinking` — see internal/uiclient's EventThinking and
	// Agent.StreamThinkingDeltas).
	//
	// IT IS NOT Thinking. Thinking is the STATUS row's live hint ("the agent is
	// working"): chrome, deliberately loud, and it competes with nothing. A
	// reasoning row is CONTENT — the model's own aside — and it is read beside
	// the answer it belongs to, so it must be QUIETER than that answer rather
	// than shouting over it. The two therefore take different attributes on
	// every table and the reasoning row reads pal.Reasoning for that reason; a
	// column that wants the live hint reads Thinking.
	//
	// WHY IT IS A FIELD OF ITS OWN RATHER THAN A REUSE OF Hint. The role is
	// different even where the colour coincides: Hint is CHROME (a reminder the
	// surface writes about itself), this is CONTENT the model produced — the
	// reader must be able to tell the model's aside from the model's answer at a
	// glance, and a later edit to either must not drag the other. The precedent
	// is statusGold vs sgrYellow a few lines up: two decisions that happen to
	// share one code stay two names.
	//
	// IT IS THE LOW-BRIGHTNESS ROLE, and that is a MEASURED choice, not a taste
	// one: the reasoning is drawn UNDER the answer in importance, so each table
	// gives it that table's muted end (Hermes' `muted` gold, the cube's muted
	// gold) rather than the body's own colour. The rows it paints are the ones
	// the `· ` reasoning gutter already marks, so the emphasis is the SECOND
	// signal and never the only one — on the monochrome tier the field is zero
	// and the gutter still says what the row is.
	Reasoning Style
	// Warning is a degradation the user must know about but can continue past.
	Warning Style
	// Error is a failure.
	Error Style
	// StatusGood, StatusFair and StatusBad are the STATUS ROW's MEASURED-QUALITY
	// tiers: the three emphases a stated score of the row's own read-out can wear
	// (the prompt cache's hit rate — statusCtxField in cmd/agent).
	//
	// WHY A TRIO AND NOT A BOOLEAN. The score has two ENDS worth naming, not one:
	// a cache hit rate near the top is the healthy steady state and near the
	// bottom is the state that costs money on every round, and a single "warn"
	// emphasis would leave the reader to infer which end they are near from the
	// number alone. The three tiers are the same three the row's other quality
	// statements already use (the Budget column's warn and danger marks), and the
	// boundaries between them are the CALLER's measurement (Rule 1: the row names
	// its own thresholds; the palette names only emphases).
	//
	// IT IS STILL EMPHASIS, NOT INFORMATION (the monochrome rule at the head of
	// this file, TestPaletteCarriesNoInformationAlone): the score prints its own
	// digits — "89%" — in every one of the three tiers, in the NO_COLOR path
	// included, so a reader who sees no colour loses nothing but the emphasis. A
	// palette that carried the score's MEANING would be the defect that rule
	// names, which is why the tier is applied to a number the row draws anyway.
	StatusGood Style
	StatusFair Style
	StatusBad  Style
}

// MonoPalette is the table used whenever colour is unavailable: every field is
// the zero Style, so the whole layer emits nothing. It is the zero value, and
// that is deliberate — there is no way to build a "monochrome" table that
// accidentally carries a sequence, because the table that carries none IS the
// zero value.
var MonoPalette = Palette{}

// BasicPalette is the table used when the terminal can render colour.
//
// NO CODE THE USER'S THEME RESOLVES (measured 2026-09-26, tasks 01a0dacf and
// 01a0e2e6). The chat's status row rendered "dim" because its label was SGR 90
// and its live hint SGR 2 — and on this machine's own terminal that row measured
// 4.39:1 (SGR 90, the terminal's "bright black") and 4.39:1 (SGR 2, whose dimming
// is a blend toward the background) against the dark background, with only 36.3%
// of each glyph's pixels at the band's maximum contrast — the faintest text on
// the whole screen while SGR 0 text beside it measured 17.58:1. Both codes are
// theme-dependent, and nothing in this repo can pin what a user's terminal does
// with them.
//
// The FIRST fix removed the row's colour (SGR 39, "the terminal's default
// foreground") — which is a colour too, and the wrong one: 39 is whatever the
// theme declares for plain text, so the operator saw "default color" where the
// Hermes-derived design puts GOLD. THE RULE THAT HOLDS, and the one this table
// obeys:
//
//   - NO CODE THE USER'S THEME RESOLVES. Banned on the status row: SGR 2 (dim),
//     SGR 7 (reverse) and every 8/16 named colour (SGR 30-37 and 90-97). Those
//     are the codes a terminal remaps — 90 to "bright black" and 2 to a blend
//     with the background — and 90/2 are precisely what measured 4.39:1.
//   - A NAMED COLOUR ON EVERY ROW THIS TABLE PAINTS. The row is GOLD (SGR 33,
//     the named "yellow" spelling — see statusGold below for why a NAMED colour
//     is a way out of the remapping trap, and what it costs on a theme that
//     calls 33 a different hue).
//   - THE ATTRIBUTES SURVIVE: bold (label), bold+italic (live) or
//     bold+italic+underline (thinking) and bold+underline (tool activity) ON TOP
//     of the colour, so the emphasis fixed by 80a53890 is kept.
//
// Colour that carries a meaning the TEXT does not already carry (diff add/remove,
// an error) stays colour, because there the reader has the words as well.
//
// The table is also chosen so the emphasis is legible on both a light and a dark
// background (colour carries no meaning on its own — see the monochrome rule in
// this file's header).
var BasicPalette = Palette{
	Banner: Style{open: sgrBold + sgrCyan},
	Prompt: Style{open: sgrBold + sgrGreen},
	// The FLOOR's buffer: the same named gold the row carries (statusGold). An
	// 8/16-colour terminal can name no other gold, and the alternative the
	// reversal rejected is exactly the SGR 39 "default colour" the operator's
	// screen showed — a colour too, and the one reported as missing. The buffer
	// takes no underline: the input is marked as the editing line, not as
	// something wrong.
	Buffer:      Style{open: sgrBold + statusGold},
	Draft:       Style{open: sgrDim},
	StatusLabel: Style{open: sgrBold + statusGold},
	StatusLive:  Style{open: sgrBold + sgrItalic + statusGold},
	Thinking:    Style{open: sgrBold + sgrItalic + sgrUnder + statusGold},
	ToolRow:     Style{open: sgrBold + sgrUnder + statusGold},
	// The COMMITTED band row is the transcript rather than chrome: it keeps its
	// attributes and takes no colour — the band's own text is the baseline here
	// (Answer is unstyled in this table, so a coloured ToolCall would be the
	// loudest thing in the band).
	ToolCall:   Style{open: sgrBold + sgrUnder},
	Answer:     Style{},
	AnswerBold: Style{open: sgrBold},
	Code:       Style{open: sgrCyan},
	DiffAdd:    Style{open: sgrGreen},
	DiffDel:    Style{open: sgrRed},
	Hint:       Style{open: sgrGray},
	// The live reasoning row on the FLOOR tier: this table has no shade of its
	// own to spend, so the emphasis is the ONE attribute that says "quieter"
	// wherever a terminal draws it (SGR 2, dim) — the code that is BANNED on the
	// status row (see BasicPalette's header) and legitimate here: this row's
	// content is a model aside the reader may skip, and the `· ` gutter states
	// what it is even where the terminal draws dim as nothing at all.
	Reasoning: Style{open: sgrDim},
	Warning:   Style{open: sgrYellow},
	Error:     Style{open: sgrBold + sgrRed},
	// The measured-quality tiers on the 8/16-colour floor: the three NAMED codes
	// the palette already uses for the same roles (green / yellow / red). The
	// floor cannot do better than the named set — and it does not need to: the
	// score's digits are drawn either way, so a reader on this tier loses the
	// emphasis and keeps the measurement (the monochrome rule at the head of this
	// file).
	StatusGood: Style{open: sgrGreen},
	StatusFair: Style{open: sgrYellow},
	StatusBad:  Style{open: sgrBold + sgrRed},
}

// ---------------------------------------------------------------------------
// The Hermes-derived gold tables
// ---------------------------------------------------------------------------
//
// WHERE THESE COME FROM. They are the DESIGN of /home/user/work/hermes-agent's
// terminal theme (ui-tui/src/theme.ts: DARK_THEME / LIGHT_THEME), ported — not
// imported: that project is TypeScript with a node_modules tree, this is a
// zero-dependency Go renderer, and what crosses the language boundary is the
// COLOUR VALUES and the role each one plays. The headline is the answer BODY:
// Hermes draws body text in `text` (#FFF8DC, "cornsilk") on a dark terminal and
// #3D2F13 on a light one, while gopi's BasicPalette deliberately leaves the body
// UNSTYLED (it is the baseline). These tables give the body a colour, which is
// the visible change the epic asks for.
//
// WHY 24-BIT AND NOT THE 8-COLOUR NAMES. The gold family is not expressible in
// the 16 named colours: #DAA520 (goldenrod) is neither SGR 33 (which the user's
// theme resolves to whatever it calls "yellow") nor SGR 90. Emitting these
// tables therefore REQUIRES a truecolour terminal, and they are only ever
// selected when the capability says the terminal advertises 24-bit colour (see
// CapabilityForEnv); on an 8/16-colour or silent terminal the resolver falls
// back to BasicPalette. Emitting `38;2;r;g;b` at a terminal that does not
// understand it would print its parameters as text, which is why the gate is a
// hard condition rather than a preference.
//
// THE STATUS ROW CARRIES THE GOLD HERE. The four status-row fields are styled
// with Hermes' OWN gold roles as 24-bit foregrounds, on top of the attributes
// 80a53890 fixed: `label` (#DAA520) for the label, `primary` (#FFD700 — the
// brightest gold the theme has) for what is happening now and for the
// tool-activity row, and `muted` (#CC9B1F) for the thinking state, which is the
// one status-row role with no Hermes counterpart of its own.
//
// WHY THESE ARE SAFE WHERE SGR 90/2 WERE NOT: `38;2;r;g;b` names an absolute
// colour. The terminal cannot remap it the way it remaps the 8/16 named codes,
// so the value measured here is the value drawn on the operator's screen — which
// is exactly the property 80a53890 could not get from a code, and which this
// table buys by requiring a truecolour terminal (see CapabilityForCapability's
// gate; this table is only ever drawn where 38;2 is understood).
//
// palette_status_salience_test.go scans THESE tables too, so the invariant — no
// theme-resolved code on the row — survives the palette change.

// The Hermes palette's colours, packed 0xRRGGBB. Named (Rule 1b/14): a bare
// literal here would be a colour nobody can find the role of.
const (
	// Dark-terminal table (Hermes DARK_THEME). The role each value plays is
	// Hermes' own: text for the body, primary for the banner, prompt for the
	// editing line's prompt, label for the status row's own label — and, since
	// task 01a0e2e6, label/primary for the status row itself.
	hermesDarkText    = 0xFFF8DC // "cornsilk" — DARK_THEME.text, the answer body
	hermesDarkPrimary = 0xFFD700 // "gold" — DARK_THEME.primary
	hermesDarkLabel   = 0xDAA520 // "goldenrod" — DARK_THEME.label, the status row's label
	hermesDarkMuted   = 0xCC9B1F // DARK_THEME.muted — the quiet end: Reasoning (the live reasoning row), Draft, Code, Hint, and the Thinking status hint's colour
	hermesDarkOk      = 0x4CAF50 // DARK_THEME.ok — a diff addition
	hermesDarkError   = 0xEF5350 // DARK_THEME.error — a diff removal / an error
	hermesDarkWarn    = 0xFFA726 // DARK_THEME.warn

	// Light-terminal table (Hermes LIGHT_THEME), the same roles with the
	// darker golds/ambers that stay legible on a white background.
	hermesLightText  = 0x3D2F13 // LIGHT_THEME.text — the answer body
	hermesLightPrim  = 0x8B6914 // LIGHT_THEME.primary
	hermesLightLabel = 0x7A5A0F // LIGHT_THEME.label
	hermesLightOk    = 0x2E7D32 // LIGHT_THEME.ok
	hermesLightError = 0xC62828 // LIGHT_THEME.error
	hermesLightWarn  = 0xE65100 // LIGHT_THEME.warn

	// Channel shifts for unpacking a packed colour into sgr's three channels.
	// Named rather than spelled at each call site (Rule 1).
	rgbRedShift   = 16
	rgbGreenShift = 8
)

// paletteHexFormat is the ONE decision of how a packed colour is written down
// for a human: the "#" prefix and the zero-padded six digits. It exists so a
// surface that must NAME a colour in prose (the `gopi chat -h` help page names
// the gold answer body) derives the string from the table's own constant
// instead of restating the value — a restated colour is a second definition
// that no test can keep in step (Rules 1/13/14).
const paletteHexFormat = "#%06X"

// paletteHexSpelling renders a packed 0xRRGGBB colour the way an operator
// writes it (`#RRGGBB`) — for documentation and for a theme file, never for the
// wire (the wire sequence is internal/sgr's, see truecolorParams below).
func paletteHexSpelling(rgb int) string { return fmt.Sprintf(paletteHexFormat, rgb) }

// HermesDarkAnswerHex is the gold DARK table's answer-body colour, in the
// operator's spelling. It derives the string from hermesDarkText — the same
// constant HermesDarkPalette.Answer is built from — so the help page and the
// rendered body cannot disagree: change the colour and the page that advertises
// it changes with it.
func HermesDarkAnswerHex() string { return paletteHexSpelling(hermesDarkText) }

// HermesLightAnswerHex is HermesLightAnswerHex's light-background twin, derived
// from hermesLightText for the same reason.
func HermesLightAnswerHex() string { return paletteHexSpelling(hermesLightText) }

// truecolorFg builds the style that paints the FOREGROUND with a packed 0xRRGGBB
// colour. The sequence itself is composed by internal/sgr — the package that owns
// the wire spelling of `38;2;r;g;b` — so this file names colours and never
// assembles an escape (Rule 13/14).
func truecolorFg(rgb int) Style {
	return Style{open: sgrSeq(truecolorParams(rgb))}
}

// truecolorFgBold is truecolorFg with bold in front of it: the "stronger form of
// the same colour" the table uses for emphasised spans, so emphasis is weight and
// never a second hue.
func truecolorFgBold(rgb int) Style {
	return Style{open: sgrBold + sgrSeq(truecolorParams(rgb))}
}

// statusFg composes a STATUS-ROW style out of the row's attributes and one
// absolute colour: attributes first, then the `38;2;r;g;b` foreground. It exists
// because the four status-row fields share one shape — every one of them is
// "some weight/slant/underline AND its own gold" — and spelling that shape once
// is what let 80a53890's two hand-written rows be replaced by four that cannot
// drift from each other (Rule 13/14: one composition, referenced).
func statusFg(attrs string, rgb int) Style {
	return Style{open: attrs + sgrSeq(truecolorParams(rgb))}
}

// truecolorParams renders the bare parameter list for a packed colour.
func truecolorParams(rgb int) string {
	return sgr.TruecolorFgParams(rgb>>rgbRedShift&0xFF, rgb>>rgbGreenShift&0xFF, rgb&0xFF)
}

// sgrSeq wraps bare SGR parameters into a full sequence. It is a one-line alias
// of internal/sgr's own Seq so this file has exactly ONE spelling of "the
// sequence introducer plus params" (Rule 14).
func sgrSeq(params string) string { return sgr.Seq(params) }

// HermesDarkPalette is the gold table for a DARK terminal: the palette Hermes
// itself ships as its default, and the one the epic's headline names (body
// #FFF8DC). It may only be used where the capability says the terminal renders
// 24-bit colour (see PaletteForCapability) and the background is dark.
var HermesDarkPalette = Palette{
	Banner: truecolorFgBold(hermesDarkPrimary),
	Prompt: truecolorFgBold(hermesDarkText),
	// The buffer takes the gold family's own gold — `primary` (#FFD700), the
	// value Hermes uses for the banner and for the status row's live half. Both
	// halves are bold (that is what stops the input reading as lighter than the
	// prompt); what separates them is the COLOUR — Hermes' `text` for the prompt,
	// `primary` for the buffer — and neither is a colour the theme resolves.
	Buffer:     truecolorFgBold(hermesDarkPrimary),
	Draft:      truecolorFg(hermesDarkMuted),
	Answer:     truecolorFg(hermesDarkText),
	AnswerBold: truecolorFgBold(hermesDarkLabel),
	Code:       truecolorFg(hermesDarkMuted),
	DiffAdd:    truecolorFg(hermesDarkOk),
	DiffDel:    truecolorFg(hermesDarkError),
	Hint:       truecolorFg(hermesDarkMuted),
	// The live reasoning row: this table's muted end (see Palette.Reasoning —
	// the role is content, the emphasis is "quieter than the answer").
	Reasoning: truecolorFg(hermesDarkMuted),
	Warning:   truecolorFg(hermesDarkWarn),
	Error:     truecolorFgBold(hermesDarkError),

	// The status row: Hermes' own gold roles, as absolute 24-bit colours, under
	// the attributes 80a53890 fixed (see this section's header).
	StatusLabel: truecolorFgBold(hermesDarkLabel),
	StatusLive:  statusFg(sgrBold+sgrItalic, hermesDarkPrimary),
	Thinking:    statusFg(sgrBold+sgrItalic+sgrUnder, hermesDarkMuted),
	ToolRow:     statusFg(sgrBold+sgrUnder, hermesDarkPrimary),
	// The COMMITTED row is band content: the label gold at bold weight, exactly
	// the role it had, so the status row and the transcript row read as the same
	// family with the band's row the lighter of the two.
	ToolCall: truecolorFgBold(hermesDarkLabel),

	// The status row's MEASURED-QUALITY tiers (the cache hit rate): Hermes' own
	// semantic roles — `ok` green, `warn` amber, `error` red — rather than gold,
	// because this is the one part of the row whose colour states a JUDGEMENT
	// about a measurement rather than the row's family. They are the same three
	// hues the diff and warning roles use, which is what keeps the terminal's own
	// vocabulary consistent: a reader who has seen `ok` green on a diff addition
	// has seen this green.
	StatusGood: truecolorFg(hermesDarkOk),
	StatusFair: truecolorFg(hermesDarkWarn),
	StatusBad:  truecolorFgBold(hermesDarkError),
}

// HermesLightPalette is the same table for a LIGHT background: Hermes'
// LIGHT_THEME values, which are darker for the same roles because the contrast
// runs the other way.
var HermesLightPalette = Palette{
	Banner: truecolorFgBold(hermesLightPrim),
	Prompt: truecolorFgBold(hermesLightText),
	// The light table's own primary — the darker gold that stays legible on a
	// white background (see this table's header).
	Buffer:     truecolorFgBold(hermesLightPrim),
	Draft:      truecolorFg(hermesLightLabel),
	Answer:     truecolorFg(hermesLightText),
	AnswerBold: truecolorFgBold(hermesLightLabel),
	Code:       truecolorFg(hermesLightLabel),
	DiffAdd:    truecolorFg(hermesLightOk),
	DiffDel:    truecolorFg(hermesLightError),
	Hint:       truecolorFg(hermesLightLabel),
	// The live reasoning row (see Palette.Reasoning): the light table's own
	// label gold, the quiet end of this table's roles.
	Reasoning: truecolorFg(hermesLightLabel),
	Warning:   truecolorFg(hermesLightWarn),
	Error:     truecolorFgBold(hermesLightError),

	StatusLabel: statusFg(sgrBold, hermesLightLabel),
	StatusLive:  statusFg(sgrBold+sgrItalic, hermesLightPrim),
	Thinking:    statusFg(sgrBold+sgrItalic+sgrUnder, hermesLightLabel),
	ToolRow:     statusFg(sgrBold+sgrUnder, hermesLightPrim),
	ToolCall:    truecolorFgBold(hermesLightLabel),

	// The measured-quality tiers, in the light theme's own darker hues: the same
	// three roles as the dark table's, tuned for a white background (see that
	// table's note; the roles are `ok` / `warn` / `error`).
	StatusGood: truecolorFg(hermesLightOk),
	StatusFair: truecolorFg(hermesLightWarn),
	StatusBad:  truecolorFgBold(hermesLightError),
}

// The xterm-256 cube indices the 256-colour table is built from. Named
// (Rule 1b/14): a bare `220` at a call site is a colour whose role nobody can
// find, and the ROLE is what these tables are made of.
//
// WHERE THE NUMBERS COME FROM. They are the xterm 256-colour CUBE approximations
// of the same Hermes roles the 24-bit tables express exactly — the cube is
// 6×6×6 stepped at 0,95,135,175,215,255 per channel, so `cubeGoldAccent = 220`
// resolves to #FFD700, which IS Hermes' DARK_THEME.primary, and `cubeGoldBody =
// 230` resolves to #FFFFD7, the cube's nearest neighbour of #FFF8DC cornsilk. The
// cube therefore loses a few percent per channel and keeps the IDENTITY: the
// warm gold family, not the stock 8-colour ANSI set whose meaning the user's
// terminal theme decides.
//
// The four chroma roles (add/del/warn/error) keep the SAME hue as their 24-bit
// counterparts, because there the colour carries a meaning the reader acts on
// (a diff addition, a removal, a failure) rather than a mood.
const (
	// cubeGoldAccent is #FFD700 — Hermes' gold, the banner's accent.
	cubeGoldAccent = 220
	// cubePaleGold is #D7D787 — the prompt's accent, deliberately a different
	// spelling from the banner's so the two chrome surfaces are not one colour.
	cubePaleGold = 186
	// cubeGoldBody is #FFFFD7 — the cube's nearest cornsilk, the answer body.
	cubeGoldBody = 230
	// cubeDeepGold is #D7AF00 — goldenrod, the emphasised span inside the body.
	cubeDeepGold = 178
	// cubeMutedGold is #D7AF87 — the tan used for code and secondary chrome.
	cubeMutedGold = 180
	// cubeGreen, cubeRed and cubeAmber are the diff/warning roles: #87D787,
	// #FF8787 and #FFAF5F, the cube neighbours of the 24-bit tables' own
	// #4CAF50 / #EF5350 / #FFA726.
	cubeGreen = 114
	cubeRed   = 210
	cubeAmber = 215
)

// palette256Fg builds the style that paints the FOREGROUND with one entry of the
// 256-colour palette. The sequence is composed by internal/sgr — the package that
// owns the wire spelling of `38;5;n` and the one place that knows the first 16
// entries have a shorter equivalent spelling — so this file names colours and
// never assembles an escape (Rule 13/14).
func palette256Fg(idx int) Style {
	return Style{open: sgrSeq(sgr.FgParams(idx))}
}

// palette256FgBold is palette256Fg with bold in front: the "stronger form of the
// same colour" the tables use for emphasised spans, so emphasis is weight and
// never a second hue.
func palette256FgBold(idx int) Style {
	return Style{open: sgrBold + sgrSeq(sgr.FgParams(idx))}
}

// palette256StatusFg is statusFg's counterpart for this tier: the status row's
// attributes plus one `38;5;n` foreground. Two spellings for one role because the
// two tiers address different colour spaces (38;2 vs 38;5), and the composition
// of each is still written once.
func palette256StatusFg(attrs string, idx int) Style {
	return Style{open: attrs + sgrSeq(sgr.FgParams(idx))}
}

// Palette256 is the table for a terminal that declares the 256-entry palette but
// does NOT advertise 24-bit colour — the middle tier, and on a modern terminal
// without COLORTERM (xterm-256color, tmux, screen) the DEFAULT one.
//
// WHY IT EXISTS. The layer was binary: 8 named colours, or 38;2 truecolour. A
// 256-colour terminal therefore received the stock ANSI set (SGR 32/33/36/90) —
// the same bytes `ls --color` emits — so the interface was indistinguishable from
// an unthemed console. `internal/sgr` had supported `38;5;n` all along
// (ColorModePalette / FgParams); nothing in this package used it.
//
// THE 256 CUBE IS A REAL COLOUR SYSTEM, not a fancier spelling of the 16: entry
// 220 is #FFD700 ON EVERY TERMINAL, because the cube's colours are defined by the
// xterm standard rather than resolved by the user's theme. That is the property
// the 8-colour codes lack and the reason this tier is worth having (see
// BasicPalette's header for the measured defect it caused on the status row).
//
// THE STATUS ROW CARRIES THE CUBE GOLD HERE. Same rule as the gold tables
// (below): a colour the terminal cannot remap, expressed in this tier's own
// spelling — `38;5;220`, the cube's #FFD700, the same colour Hermes calls
// primary. It is the ONE value for the whole row, where the 24-bit tables can
// afford two shades: the 256 tier has no amber as legible as 220 for the
// thinking state, and a second entry chosen for the sake of variety would be a
// colour picked to look different rather than to be the family.
var Palette256 = Palette{
	Banner: palette256FgBold(cubeGoldAccent),
	Prompt: palette256FgBold(cubePaleGold),
	// The buffer takes the cube's #FFD700 (entry 220) — the colour this table
	// builds its chrome from; the prompt keeps its own lighter entry
	// (cubePaleGold, 186), which is what separates the two halves here. The 256
	// tier has no second gold as legible as 220, so the buffer does not get a
	// shade of its own the way the 24-bit tables can afford one.
	Buffer:     palette256FgBold(cubeGoldAccent),
	Draft:      palette256Fg(cubeMutedGold),
	Answer:     palette256Fg(cubeGoldBody),
	AnswerBold: palette256FgBold(cubeDeepGold),
	Code:       palette256Fg(cubeMutedGold),
	DiffAdd:    palette256Fg(cubeGreen),
	DiffDel:    palette256Fg(cubeRed),
	Hint:       palette256Fg(cubeMutedGold),
	// The live reasoning row: the cube's muted gold, the entry this table already
	// uses for the light-weight roles (see Palette.Reasoning).
	Reasoning: palette256Fg(cubeMutedGold),
	Warning:   palette256Fg(cubeAmber),
	Error:     palette256FgBold(cubeRed),

	StatusLabel: palette256StatusFg(sgrBold, cubeGoldAccent),
	StatusLive:  palette256StatusFg(sgrBold+sgrItalic, cubeGoldAccent),
	Thinking:    palette256StatusFg(sgrBold+sgrItalic+sgrUnder, cubeGoldAccent),
	ToolRow:     palette256StatusFg(sgrBold+sgrUnder, cubeGoldAccent),
	// The committed band row carries the gold family at the entry the body's own
	// emphasised spans use.
	ToolCall: palette256FgBold(cubeDeepGold),

	// The measured-quality tiers (the cache hit rate): the cube's own green /
	// amber / red, the same entries the diff and warning roles use, so the tier's
	// meaning is the one the rest of the surface already draws.
	StatusGood: palette256Fg(cubeGreen),
	StatusFair: palette256Fg(cubeAmber),
	StatusBad:  palette256FgBold(cubeRed),
}

// The named palette selections. They are the VALUE domain of the GOPI_PALETTE
// knob (colour.go), and the reason that knob is a name rather than a boolean:
// "which palette" has four answers here, and a boolean would have made three of
// them unreachable.
const (
	// PaletteNameBasic is the FLOOR: the 8-colour attributes-only table that
	// works on every terminal and needs no capability beyond "colour on". It is
	// unchanged by this epic — an operator who explicitly asks for it (or a
	// terminal that cannot do more) gets exactly the bytes it always emitted.
	PaletteNameBasic = "basic"
	// PaletteNamePalette256 selects the 256-colour table. On a terminal that
	// cannot draw 38;5;n this request is downgraded to BasicPalette WITH a notice
	// (colour.go's paletteDowngradeNotice, said once by the run's final decision),
	// never silently.
	PaletteNamePalette256 = "palette256"
	// PaletteNameHermes selects the gold table for a dark background.
	PaletteNameHermes = "hermes"
	// PaletteNameHermesLight selects the gold table for a light background.
	PaletteNameHermesLight = "hermes-light"
	// PaletteNameUnset is the EMPTY selection: the knob is not set, or set to
	// nothing. It is the sentinel for "nobody chose, follow the terminal", and it
	// is deliberately the empty string rather than a word like "auto": a word
	// would be a legal VALUE of the knob that does not resolve to a table
	// (PaletteByName could not answer it honestly, since the answer is the
	// capability's), and a name that is listed among the legal values but refused
	// by the resolver is a trap for the operator and for the next reader.
	//
	// The empty value is also what the environment hands back for an unset knob,
	// so there is exactly one spelling of "nobody chose" between the shell and
	// here.
	PaletteNameUnset = ""
)

// palette256Aliases are the spellings of the 256 table that name the same table.
// They exist because two vocabularies meet at this knob: the operator's (who
// thinks "the 256-colour variant of the gold look I asked for") and the
// capability's (which knows the table as "the 256-colour one"). Both are legal
// values of GOPI_PALETTE, and neither is a fallback wearing the other's name.
var palette256Aliases = []string{"hermes-256", "hermes256", "256"}

// PaletteByName resolves a palette selection to the data table it names. The
// second result reports whether the name was recognised — a caller that must not
// fail silently can say so; the capability resolver falls back to BasicPalette
// (the documented fallback) rather than rendering a half-chosen table.
//
// A name that needs more than the terminal offers still returns its intended
// table here: this function answers "which table", and the CAPABILITY that gates
// whether it may be drawn is PaletteForCapability's decision, in exactly one
// place.
func PaletteByName(name string) (Palette, bool) {
	switch name {
	case PaletteNamePalette256:
		return Palette256, true
	case PaletteNameHermes:
		return HermesDarkPalette, true
	case PaletteNameHermesLight:
		return HermesLightPalette, true
	case PaletteNameBasic:
		return BasicPalette, true
	}
	for _, alias := range palette256Aliases {
		if name == alias {
			return Palette256, true
		}
	}
	return BasicPalette, false
}

// styledFields wraps the leading fields of a composed label according to the
// palette's per-field styles. The label is composed of StatusSep-separated
// fields (the one definition of the separator, Rule 14); a field that is the
// tool-activity row is styled as such and every other field as the label. The
// styling happens AFTER any fitting/truncation, on display cells already
// decided — it can therefore never move a column.
func styledFields(label string, pal Palette) string {
	if label == "" || (pal.StatusLabel.Empty() && pal.ToolRow.Empty()) {
		return label
	}
	fields := strings.Split(label, StatusSep)
	for i, f := range fields {
		if isToolRow(f) {
			fields[i] = pal.ToolRow.Wrap(f)
			continue
		}
		fields[i] = pal.StatusLabel.Wrap(f)
	}
	return strings.Join(fields, StatusSep)
}

// isToolRow reports whether a status field is the tool-activity row. The test
// is the row's own glyph (ToolGlyph) — the marker the web UI's `event: tool`
// rows use as well — so the palette styles exactly the row the surfaces label
// as tool activity, and a re-spelling of the glyph moves both at once.
func isToolRow(field string) bool {
	return strings.HasPrefix(field, ToolGlyph)
}

// ToolGlyph is the marker that begins a tool-activity row on the status line.
// It lives HERE (the package that owns the palette and the status row's
// composition) rather than in the caller that builds the label, so the row's
// marker, its emphasis and the surfaces that read it cannot drift (Rule 14).
const ToolGlyph = "⚙"

// WHERE THE GOLD CHROME IS LEGIBLE: the terminal ground this layer's palettes
// were composed against.
//
// WHY THIS IS DECLARED HERE AND NOT BY EACH PAINTER. Every palette this package
// builds is a FOREGROUND table: Hermes' four roles (body #FFF8DC, primary
// #FFD700, label, muted) are golds and pale ambers whose contrast was measured
// against a DARK ground, and HermesLightPalette exists precisely because those
// same roles are illegible on white. The surface therefore carries an assumption
// about the SCREEN it is drawn on, and until this constant existed the assumption
// was nowhere in the code — the terminal's own default decided it, which is a
// white ground on an unconfigured xterm. The measured consequence (2026-10-04,
// `gopi chat --replay <id> --film DIR --out f.gif`): every frame was a WHITE
// window carrying pale-gold text at ~1.2:1 contrast — a film of a surface nobody
// would recognise, on a ground the palette's own header says the colours were
// tuned against (`HermesDarkPalette`: "the gold table for a DARK terminal").
//
// THE VALUES ARE THE OPERATOR'S OWN DESKTOP GROUND, unmodified: xfce4-terminal's
// ColorBackground / ColorForeground on this host (`#131926`, `#b7b7b7` — the
// blue-tinted dark the film subsystem's own comments already name as "this
// film's background is (19,25,38)"). Naming the surface's declared ground is not
// a new design; it writes down the one the product has been drawn against all
// along, so a painter that must materialise it (a filmed terminal on a private
// display) can ask for it instead of inheriting a stranger's default.
//
// THEY ARE GROUPED — AND THE PAIR IS THE POINT. A caller that sets one and not
// the other gets default-black text on a dark ground (illegible in the opposite
// direction from the defect above), so the two travel together: every channel
// that materialises the ground takes BOTH, and the two constants below are only
// ever read as a pair (see internal/shot's FilmTerminalSpec.groundArgs, the one
// production reader of both).
const (
	// GroundBackgroundName / GroundForegroundName are the KNOB names — the
	// vocabulary a refusal and a report use, and what a colour flag's value is
	// called in the surface's own words rather than in one terminal's spelling.
	GroundBackgroundName = "background"
	GroundForegroundName = "foreground"
	// GroundBackgroundHex is the dark ground the gold chrome is legible on.
	GroundBackgroundHex = "#131926"
	// GroundForegroundHex is the plain-text colour on that ground.
	GroundForegroundHex = "#b7b7b7"
)
