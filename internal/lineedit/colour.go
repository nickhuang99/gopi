package lineedit

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/nick/gopi/internal/sgr"
)

// colour.go — THE capability decision, in exactly one place.
//
// Every consumer of the palette asks THIS file whether colour is available and
// takes the returned table; no other site reads an environment variable, a flag
// or a terminal probe for that purpose. The reason is not tidiness: the
// acceptance for this layer is byte-exact in both directions (a disabled run
// emits ZERO SGR bytes), and a second decision point anywhere would reintroduce
// the one case nobody tested — the surface that colours its banner because it
// only checked the flag, or only the terminal.
//
// Precedence, highest first (each rule is a named constant below):
//
//  1. the caller's explicit setting — the --no-color flag, or any caller that
//     knows its sink is captured (the flag wins over everything: an operator
//     who asked for no colour gets no colour even on a colour terminal);
//  2. an explicit environment NO_COLOR (the https://no-color.org convention:
//     presence is the signal, whatever its value);
//  3. TERM=dumb, the terminal's own declaration that it renders no attributes;
//  4. the sink: not a terminal (a pipe, a file, a test buffer) is never
//     coloured, which is what keeps ANSI bytes out of redirected output;
//  5. otherwise colour.
//
// On the colour-ON side the SAME function reads how DEEP the terminal can paint
// (`COLORTERM` for 24-bit, the `256color` marker in `TERM` for the 256 palette).
// Depth is a question with a different answer from "may I emit at all", which is
// why it is a second tier on the capability rather than a third value of the
// colour switch, and why the table is then chosen by PaletteForCapability.
//
// The decision is a pure function of (flag, environment, sink) — it holds no
// state and writes nothing — so a test can assert any combination directly, and
// production calls it ONCE per process and hands the table down.

// Environment variables and values this decision reads. Named (Rule 1b): these
// spellings are a contract with the outside world, and a typo yields a
// colour-off run that silently stays colour-on.
const (
	// EnvNoColor is the standard "no colour anywhere" variable. Its PRESENCE
	// is the signal, including an empty value.
	EnvNoColor = "NO_COLOR"
	// EnvTerm names the terminal type; TermDumb is the value that declares a
	// terminal with no attribute support.
	EnvTerm = "TERM"
	// TermDumb is TERM's "no capabilities at all" value.
	TermDumb = "dumb"
	// FlagNoColor is the command-line spelling of "I do not want colour". It is
	// defined here so the CLI parser and the offer in the usage text compare
	// one constant, never a bare literal in two files.
	FlagNoColor = "--no-color"
)

// ColourMode is the resolved capability: colour is either on (the whole
// palette emits SGR) or off (the palette is the zero table and emits nothing).
// It is deliberately a two-valued type rather than a "how many colours" count:
// the layer's contract is byte-exact absence when disabled, and COLOUR DEPTH is
// a separate question with its own answer (the palette256 / truecolor tiers on
// Capability below) rather than a third value that would blur "may I emit" with
// "how much may I emit".
type ColourMode int

const (
	// ColourOff means no SGR byte may reach the sink.
	ColourOff ColourMode = iota
	// ColourOn means the colour table is in use.
	ColourOn
)

// ColourWanted resolves whether colour may be used for sink, given the caller's
// explicit request to disable it (noColor). It is THE capability decision: see
// this file's header for the precedence it implements.
//
// A nil sink counts as "not a terminal" (a caller that has no file handle — a
// captured writer, a test buffer — is never coloured).
func ColourWanted(noColor bool, sink *os.File) ColourMode {
	if noColor {
		return ColourOff
	}
	if _, set := os.LookupEnv(EnvNoColor); set {
		return ColourOff
	}
	if strings.EqualFold(os.Getenv(EnvTerm), TermDumb) {
		return ColourOff
	}
	if sink == nil || !IsTerminal(sink) {
		return ColourOff
	}
	return ColourOn
}

// PaletteFor returns the palette table for a resolved capability. It is the
// ONLY mapping from capability to table, so a surface cannot render "mostly
// coloured" by picking its own.
func PaletteFor(mode ColourMode) Palette {
	if mode == ColourOff {
		return MonoPalette
	}
	return BasicPalette
}

// PaletteForEnv resolves the capability AND returns the matching table — the
// one call a producer needs at startup. It is a convenience over ColourWanted +
// PaletteFor, never a second decision: it delegates to both, so a change to the
// precedence is one edit.
func PaletteForEnv(noColor bool, sink *os.File) Palette {
	return PaletteFor(ColourWanted(noColor, sink))
}

// ---------------------------------------------------------------------------
// The second half of the ONE decision: WHICH table, and whether the terminal can
// draw it
// ---------------------------------------------------------------------------

// EnvColorTerm names the environment variable by which a terminal advertises its
// colour depth. Its presence-with-a-known-value is the ONLY signal gopi has that
// 24-bit colour will be understood: there is no terminfo lookup here and no probe
// (a probe would have to write bytes to the terminal before the first frame).
const EnvColorTerm = "COLORTERM"

// ColorTermTruecolor is the value COLORTERM carries when the terminal advertises
// 24-bit colour. It is a VALUE of the outside world's vocabulary, so it is named
// once here (Rule 1b) and compared case-insensitively — the convention is spelled
// both ways in the wild.
const ColorTermTruecolor = "truecolor"

// Term256Color is the substring a TERM value carries when the terminal
// renders the 256-entry palette. It is the whole of gopi's 256-colour detection
// and it is deliberately a NAME, not a list: the convention is `xterm-256color`,
// `screen-256color`, `tmux-256color`, `rxvt-unicode-256color`, `linux-256color` —
// the TI/terminfo spelling puts the capability in the name, and gopi has no
// terminfo reader (Rule 7's dependency preflight: a terminfo database read is a
// new dependency for one bit of information the terminal already states).
//
// WHY SUBSTRING AND NOT EQUALITY. The suffix is what is standardised, not the
// whole value: terminals put their own name in front of it, and a `TERM=screen`
// login sets its own variants. A prefix match over a list of known names would
// miss the next emulator's; `strings.Contains` asks the question the convention
// actually answers ("does this name declare 256 colours?").
//
// It is declared here (the capability decision) and read in exactly ONE place
// (CapabilityForEnv), per Rule 2.
const Term256Color = "256color"

// EnvPalette names the environment knob that SELECTS the palette table. The
// value domain is PaletteNameBasic | PaletteNameHermes | PaletteNameHermesLight |
// PaletteNamePalette256 (palette.go).
//
// WHY AN ENV VAR AND NOT A FLAG. A palette is a property of the TERMINAL the
// operator is sitting at — it is set once in a shell profile, not typed per
// invocation, and `gopi chat` already forwards all three of its other
// presentation knobs the same way (NO_COLOR, TERM, COLORTERM are read by this
// file). It is read through config.EnvBool-free direct comparison because its
// domain is a name, not a switch; the reader is PaletteSelection below and it is
// the ONLY reader in the tree.
const EnvPalette = "GOPI_PALETTE"

// Capability is the resolved presentation decision: how much colour the
// terminal can render, and which table the operator asked for. Callers ask for
// this ONCE (CapabilityForEnv) and then take everything from it, so no surface
// re-derives a colour decision from the environment.
//
// TWO DEPTHS, ONE DECISION (the epic this struct's measurement half exists for).
// The depth a terminal DECLARES lives in the environment (COLORTERM, the
// `256color` marker in TERM) and can be wrong in both directions. The depth a
// terminal MEASURED lives in CapabilityMeasurement (probe.go) and is the
// terminal answering questions about itself. Both are kept — the declaration
// because it is the fallback when the probe got nothing, the measurement because
// it is the tie-breaker when both exist — and the DECISION (evidenceDepth) is
// one function, so no surface can decide for itself which of the two it trusts.
type Capability struct {
	// colour is the existing two-valued switch (ColourOff means no SGR byte may
	// reach the sink).
	colour ColourMode
	// declaredDepth is what the environment claims (COLORTERM, the TERM
	// marker). It is the FALLBACK evidence: used when no measurement exists.
	declaredDepth TermDepth
	// declaredTruecolor / declaredPalette256 keep the two declaration bits as
	// they were read, so a report can quote the claim beside the measurement
	// even when the measurement overruled it (the `declared=` half of a dump
	// record).
	declaredTruecolor  bool
	declaredPalette256 bool
	// measured is the probe's answer, empty when no probe ran or it read
	// nothing. It is evidence, not a claim: CapabilityMeasurement.Measured()
	// is what decides whether it may displace the declaration, so "the terminal
	// answered the liveness query" never becomes "the terminal has no deep
	// colour".
	measured CapabilityMeasurement
	// palette is the operator's requested table (EnvPalette), or PaletteNameUnset
	// when nobody chose one.
	palette string
}

// The depth decisions' own vocabulary: WHICH evidence settled the depth. It is
// reported in the dump record, because "256 colours" means something different
// depending on whether the terminal said so or the environment did.
const (
	// DepthSourceMeasured: the terminal answered a depth query.
	DepthSourceMeasured = "measured"
	// DepthSourceDeclared: no measurement, so the environment's claim stands.
	DepthSourceDeclared = "declared"
	// DepthSourceDefault: neither — no measurement and nothing declared, so the
	// floor is in force and nothing claimed it.
	DepthSourceDefault = "default"
)

// The tier names the report prints. They are words rather than the enum's
// numbers because a record a human reads must not need the enum (Rule 1b).
const (
	DepthNameBasic     = "basic"
	DepthName256       = "256"
	DepthNameTruecolor = "truecolor"
)

// DepthName renders a tier as the word a record uses.
func DepthName(d TermDepth) string {
	switch d {
	case DepthTruecolor:
		return DepthNameTruecolor
	case Depth256:
		return DepthName256
	default:
		return DepthNameBasic
	}
}

// CapabilityReport renders the DECISION as the lines a record carries: which
// evidence settled the depth, what the environment claimed, and — as its own
// line — what the probe measured, evidence and verdict together. It is one
// function so the artifact and any future reader cannot spell the decision
// differently (Rule 13/14).
//
// The declared half is reported even when the measurement overruled it: a
// record that showed only the winner could not answer "the environment said
// truecolor and the terminal said 16 — which did the run use?", which is the
// whole question a capability artifact exists to answer.
func CapabilityReport(c Capability) []string {
	lines := []string{
		CapabilityDecisionField + "=" + c.DepthSource() +
			" depth=" + DepthName(c.Depth()) +
			" declared_truecolor=" + boolOnOff(c.declaredTruecolor) +
			" declared_256=" + boolOnOff(c.declaredPalette256) +
			" colour=" + colourWord(c.colour) +
			" palette=" + c.paletteNameOrDefault(),
	}
	if c.measured.Answered || c.measured.Bytes > 0 {
		lines = append(lines, c.measured.Summary())
	}
	return lines
}

// CapabilityDecisionField is the `name=` prefix of the report's decision line.
// It is a constant because the record's readers (a test, a report, an agent
// grepping a dump) key on it.
const CapabilityDecisionField = "capability"

// CapabilityNotice is the ONE sentence a chat run prints when the terminal's own
// answer OVERTURNS the environment's declaration, or "" when the two agree (or
// when no measurement was made). A disagreement is announced because it changes
// what the operator will see on their screen: the tables differ, and the reason
// is that their terminal and their TERM/COLORTERM do not describe each other
// (Rule 15 — a downgrade nobody is told about is a silent failure).
func CapabilityNotice(c Capability) string {
	if c.DepthSource() != DepthSourceMeasured {
		return ""
	}
	measured, declared := c.Depth(), c.declaredDepth
	if measured == declared {
		return ""
	}
	return fmt.Sprintf("[gopi] terminal colour measured as %s (TERM/COLORTERM declare %s) — the terminal's own answers win: %s\n",
		DepthName(measured), DepthName(declared), c.measured.Summary())
}

// colourWord renders the colour switch in the record's vocabulary.
func colourWord(c ColourMode) string {
	if c == ColourOn {
		return "on"
	}
	return "off"
}

// boolOnOff renders a switch as "on"/"off" — the record's own words, never a
// bare "true" a reader has to know the convention of. (The name is the
// capability report's own: the package's tests already carry a `boolWord`, and a
// production helper that collided with a test helper would be a rename waiting
// to happen.)
func boolOnOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

// paletteNameOrDefault renders the selection, naming the implicit default
// rather than printing an empty field (an empty `name=value` is ambiguous
// between "absent" and "empty").
func (c Capability) paletteNameOrDefault() string {
	if c.palette == "" {
		return PaletteNameUnset
	}
	return c.palette
}

// WithMeasurement folds a capability probe's result into the decision and
// returns the capability that will actually be used. It is the ONE place the
// measurement meets the declaration, so "measurement first, declaration as the
// fallback" is a property of this function rather than a discipline every
// caller is asked to observe (Rule 13).
//
// It never changes the colour switch: whether colour may be emitted at all is
// decided by the flag, NO_COLOR, TERM=dumb and the sink (ColourWanted), and a
// terminal answering questions is not permission to colour a pipe.
func (c Capability) WithMeasurement(m CapabilityMeasurement) Capability {
	c.measured = m
	return c
}

// Measurement returns the probe's result this capability was decided with (the
// zero value when none was folded in).
func (c Capability) Measurement() CapabilityMeasurement { return c.measured }

// MeasuredDepth is the depth the terminal's OWN answers support. A measurement
// that read nothing answers DepthBasic — the floor — but it is reported with
// the declaration's evidence word rather than the measurement's (see
// DepthSource), so "measured basic" is never claimed about a silent terminal.
func (c Capability) MeasuredDepth() TermDepth { return c.measured.Depth() }

// DeclaredDepth is the depth the environment claims.
func (c Capability) DeclaredDepth() TermDepth { return c.declaredDepth }

// DeclaredTruecolor reports the COLORTERM advertisement as read, regardless of
// what the measurement then decided (so a record can show the claim).
func (c Capability) DeclaredTruecolor() bool { return c.declaredTruecolor }

// DeclaredPalette256 reports the TERM marker as read, regardless of the
// measurement.
func (c Capability) DeclaredPalette256() bool { return c.declaredPalette256 }

// evidenceDepth is THE resolution: the measured depth when the terminal
// answered something a depth can rest on, the declared depth otherwise. One
// function, so a surface that asks "how deep?" cannot answer it differently from
// the table it draws with.
func (c Capability) evidenceDepth() TermDepth {
	if c.colour != ColourOn {
		// Colour off: no SGR is emitted at all, so reporting a depth would
		// invite a caller to emit one (the same discipline the declaration read
		// below follows).
		return DepthBasic
	}
	if c.measured.Measured() {
		return c.measured.Depth()
	}
	return c.declaredDepth
}

// DepthSource names the evidence behind Depth. It is the field that stops
// "256 colours" from being read as a fact about the terminal when it was in fact
// a string in the environment.
func (c Capability) DepthSource() string {
	if c.colour != ColourOn {
		return DepthSourceDefault
	}
	if c.measured.Measured() {
		return DepthSourceMeasured
	}
	if c.declaredDepth != DepthBasic {
		return DepthSourceDeclared
	}
	return DepthSourceDefault
}

// TermDepth is how many colours the terminal can render, as the one number the
// capability decided. It is a REPORTING type: the table selection asks
// Capability.AtLeast (a question about one tier), never the numeric order of the
// tiers, so a future tier cannot be placed wrongly by a comparison that was
// written for ordering rather than for capability.
type TermDepth int

const (
	// DepthBasic is the 8/16 named-colour terminal: the floor that always works.
	DepthBasic TermDepth = iota
	// Depth256 is a terminal that declares the 256-entry palette.
	Depth256
	// DepthTruecolor is a terminal that advertises 24-bit colour.
	DepthTruecolor
)

// AtLeast reports whether the terminal's capability reaches tier. The relation
// is SPELT (a switch), not derived from the constants' numeric order, so
// inserting a tier later cannot silently make a basic terminal satisfy it.
func (c Capability) AtLeast(tier TermDepth) bool {
	if c.colour != ColourOn {
		return false
	}
	return c.evidenceDepth() >= tier
}

// Depth reports the deepest tier this capability reaches — for reporting (the
// downgrade notice, a diagnostic), not for table selection.
func (c Capability) Depth() TermDepth { return c.evidenceDepth() }

// Palette256 reports whether the terminal's decided depth reaches the 256-entry
// palette. On a colour-off capability it is false: a terminal that gets no SGR
// gets no colour depth either (the same discipline Truecolor follows).
func (c Capability) Palette256() bool { return c.AtLeast(Depth256) }

// Colour returns the two-valued colour switch.
func (c Capability) Colour() ColourMode { return c.colour }

// Truecolor reports whether the DECIDED capability reaches 24-bit colour. On a
// colour-off capability it is false: a terminal that gets no SGR gets no
// truecolour either — and a measurement that says 24-bit cannot put it back.
func (c Capability) Truecolor() bool { return c.AtLeast(DepthTruecolor) }

// PaletteName returns the requested palette selection (its default when the
// operator named none).
func (c Capability) PaletteName() string { return c.palette }

// CapabilityForEnv resolves the WHOLE presentation decision — colour on/off,
// whether the terminal advertises truecolour, and which table was asked for — in
// this one place. It is the capability's single entry point; every consumer takes
// a Palette from PaletteForCapability and reads nothing else.
//
// It reads DECLARATIONS only. A caller that has a terminal and can ask it folds
// the answer in with WithMeasurement (the chat wiring does, before its first
// frame); a caller with no descriptor to question (the `gopi ui` surface, a
// test) keeps exactly this behaviour, which is why this function's contract did
// not change when the probe was added.
func CapabilityForEnv(noColor bool, sink *os.File) Capability {
	c := Capability{
		colour:  ColourWanted(noColor, sink),
		palette: PaletteSelection(),
	}
	// The depth advertisements are read only when colour is on: with colour off
	// there is no sequence to emit, and reporting "256 colours" there would
	// invite a caller to emit one.
	//
	// Both tiers are read HERE and nowhere else (Rule 2): the TERM marker is the
	// terminal's own declaration of the 256-entry palette, COLORTERM is its
	// advertisement of 24-bit colour.
	if c.colour == ColourOn {
		c.declaredTruecolor = strings.EqualFold(os.Getenv(EnvColorTerm), ColorTermTruecolor)
		c.declaredPalette256 = strings.Contains(strings.ToLower(os.Getenv(EnvTerm)), Term256Color)
		c.declaredDepth = declaredTermDepth(c)
	}
	return c
}

// declaredTermDepth renders the two declaration bits as the tier they claim.
// It lives beside the bits so the claim and its tier cannot drift apart.
func declaredTermDepth(c Capability) TermDepth {
	switch {
	case c.declaredTruecolor:
		return DepthTruecolor
	case c.declaredPalette256:
		return Depth256
	default:
		return DepthBasic
	}
}

// PaletteSelection returns the operator's requested palette name, and
// PaletteNameUnset when the knob is unset or empty. It reads the knob in EXACTLY
// this one place (Rule 2).
//
// THE DEFAULT IS THE TABLE THE TERMINAL CAN DRAW, not a fixed name: the operator
// named none, so the answer must be settled by the CAPABILITY, and the capability
// is not known here. The EMPTY selection is what carries "nobody chose" across
// that gap: PaletteForCapability resolves it per tier (gold 24-bit where
// advertised, the 256 approximation where the terminal declares 256 colours, the
// 8-colour attribute table on the floor). A hard-coded default here would have to
// pick a tier before the terminal has been consulted — which is the defect this
// epic exists to fix (the host's TERM declares 256 colours and was served the
// stock 8-colour ANSI set).
//
// An UNRECOGNISED name is not silently obeyed (Rule 15): it is reported on
// stderr, once, naming the value and the legal set; the run then follows the
// terminal exactly as an unset knob does. The alternative — treating a typo as a
// silent "basic" — leaves an operator staring at a palette that ignores their
// setting with nothing to read.
//
// The message deliberately does NOT name the table that will be drawn: the
// default depends on the terminal's depth, which is decided further down
// (PaletteForCapability), and if that resulting table is itself a downgrade the
// one-line notice there names it. One line per cause, never two lines for one
// cause.
func PaletteSelection() string {
	raw := strings.TrimSpace(os.Getenv(EnvPalette))
	if raw == "" {
		return PaletteNameUnset
	}
	if _, ok := PaletteByName(raw); !ok {
		fmt.Fprintf(os.Stderr, "[gopi] %s=%q is not a known palette (legal values: %s) — following the terminal's depth instead\n",
			EnvPalette, raw, strings.Join(PaletteNames(), ", "))
		return PaletteNameUnset
	}
	return raw
}

// PaletteForCapability maps a resolved capability to the table to draw with. It
// is the ONLY mapping from (colour, depth, requested name) to a table, so a
// surface cannot render "mostly coloured" by picking its own, and the DEFAULT
// lives here because this is the first point at which both halves of the answer
// are known: which table was asked for, and what the terminal can draw.
//
// THE RULE, in one line: every table is drawn where the terminal can draw it;
// where it cannot, the deepest table the terminal CAN draw is used instead, and
// the substitution is announced (once, on stderr) rather than silently made.
//
// The returned table is NEVER empty while colour is on: there is always a table
// the current depth supports (BasicPalette is the floor that needs nothing
// beyond "colour on"), so the fallback chain cannot bottom out in silence.
func PaletteForCapability(c Capability) Palette {
	pal, notice := PaletteForCapabilityNoticed(c)
	if notice != "" {
		fmt.Fprint(os.Stderr, notice)
	}
	return pal
}

// PaletteForCapabilityNoticed is the mapping WITHOUT the side effect: the table
// to draw with, and the ONE sentence the operator is owed ("" when the request
// was honoured, or when nobody asked for a table they are not getting). It
// writes nothing.
//
// THE SENTENCE IS A VALUE, not a write, because an announcement belongs to the
// run's FINAL decision rather than to each resolution. A producer that decides
// the same run in STAGES — `gopi chat` resolves the environment's declaration,
// builds its input layer on it, and re-derives the table once the terminal has
// answered, and only the last of those is the run's decision — holds the
// sentence of a stage, drops it when a later stage overrules it, and says
// exactly one. Taking the two halves apart is what keeps a run's sentence
// assertable without counting lines in a child's output (task 01a10e2b: the
// staged shape printed one decision's notice three times, and its screen showed
// one table).
func PaletteForCapabilityNoticed(c Capability) (Palette, string) {
	if c.colour == ColourOff {
		return MonoPalette, ""
	}
	// The operator asked for nothing in particular: the terminal's own depth
	// picks the table, and no notice is due — nobody asked for the table they are
	// not getting (the "no silent upgrade" twin of the ban on a silent
	// downgrade). If the default itself cannot be drawn at this depth the
	// capability gate below is what catches it, which is why the default is
	// resolved to a NAME and then goes through the same gate as any request.
	want := c.palette
	if want == PaletteNameUnset {
		want = defaultPaletteFor(c)
	}

	pal, known := PaletteByName(want)
	switch {
	case !known:
		// An unknown name is reported by PaletteSelection; this branch exists for
		// a Capability built by hand (a caller, a test) and still may not end in
		// an empty table.
		return BasicPalette, ""
	case c.canDraw(pal):
		return pal, ""
	default:
		// The recursion takes the NOTICED form, so the fallback's own resolution
		// can never announce: it lands on the table a run with no GOPI_PALETTE
		// would have drawn at this depth, and one downgrade is one sentence.
		fallback, _ := PaletteForCapabilityNoticed(defaultCapability(c))
		return fallback, paletteDowngradeNotice(c, want, pal, nameOfTable(fallback))
	}
}

// PaletteDowngradeNotice answers what sentence a capability owes the operator
// WITHOUT drawing anything, for a producer that must HOLD the sentence while it
// finishes deciding — the declaration's half of a staged decision: the caller
// knows what the environment asked for before the terminal has answered, and
// needs that sentence only if nothing later overrules it.
//
// It is the same computation as the noticed resolution's second result, reached
// through it, so a sentence a caller HOLDS and a sentence a resolution RETURNS
// cannot be two spellings of one downgrade (Rule 13). "" means the request is
// honoured (or nobody asked), which is why it is a string and not a bool plus a
// message.
func PaletteDowngradeNotice(c Capability) string {
	_, notice := PaletteForCapabilityNoticed(c)
	return notice
}

// defaultCapability is c with its palette selection cleared: it is how the
// fallback table is obtained from the ONE mapping (defaultPaletteFor), so a
// downgrade lands on exactly the table a run with no GOPI_PALETTE would have
// drawn at this depth — never on a second, parallel notion of "the next table
// down".
func defaultCapability(c Capability) Capability {
	c.palette = PaletteNameUnset
	return c
}

// nameOfTable is the name of an already-resolved table, for a notice that must
// say what was drawn. The mapping is the reverse of PaletteByName's for the four
// named tables; a table that is none of them (a caller's own) is reported as the
// floor it is not, which is why the four cases are exhaustive here rather than a
// default.
func nameOfTable(pal Palette) string {
	switch {
	case pal == MonoPalette:
		return PaletteNameBasic
	case pal == BasicPalette:
		return PaletteNameBasic
	case pal == Palette256:
		return PaletteNamePalette256
	case pal == HermesLightPalette:
		return PaletteNameHermesLight
	case pal == HermesDarkPalette:
		return PaletteNameHermes
	default:
		return PaletteNameBasic
	}
}

// PaletteNameFor is the name of an already-resolved table, for a caller that
// must REPORT which table a run drew with (the dump's `palette=` field, a
// downgrade notice). It is nameOfTable exported under its reporting name, so a
// report and a notice cannot name the same table differently.
func PaletteNameFor(pal Palette) string { return nameOfTable(pal) }

// defaultPaletteFor is the DEFAULT table for a depth: the deepest table that
// depth can draw. It is the "nobody chose a palette" half of the mapping, and it
// lives beside the mapping so the default and the gate are read together.
func defaultPaletteFor(c Capability) string {
	if c.AtLeast(DepthTruecolor) {
		return PaletteNameHermes
	}
	if c.AtLeast(Depth256) {
		return PaletteNamePalette256
	}
	return PaletteNameBasic
}

// canDraw reports whether this capability can render the table — the gate, in
// ONE place, stated against the table's own required depth (Rule 13: the property
// travels with the data, so a new table is gated the moment it is written).
func (c Capability) canDraw(pal Palette) bool {
	return c.AtLeast(pal.RequiredDepth())
}

// paletteDowngradeNotice RETURNS the ONE stderr line that says a requested table
// was not drawn, WHY, and what would have enabled it (Rule 15: no silent failure
// — the same discipline PaletteSelection applies to an unknown name).
//
// Every part of it is load-bearing, because the message is the whole of what the
// operator gets:
//
//   - the palette they ASKED for, so the run is not reading as if it obeyed;   - the depth that table needs (the table's own RequiredDepth, not a claim
//     about which table it is — a table that grows a deeper colour needs no edit
//     here);
//   - the observed environment (TERM, COLORTERM), so "not drawable here" is
//     checkable rather than mysterious;
//   - the table actually drawn, by name;
//   - the ENV VAR (and the exact value) that would enable the requested table —
//     the one thing the operator can act on without reading this source.
//
// It is built here, from a capability, and RETURNED rather than written: the
// sentence belongs to the run's decision (see PaletteForCapabilityNoticed),
// and a function that both decides and speaks cannot be held by a caller that
// must finish deciding first.
func paletteDowngradeNotice(c Capability, want string, pal Palette, drew string) string {
	need := pal.RequiredDepth()
	return fmt.Sprintf(
		"[gopi] palette %q needs %s and is not drawable here (TERM=%q %s=%q) — drawing %q; %s\n",
		want, termDepthName(need), os.Getenv(EnvTerm), EnvColorTerm, os.Getenv(EnvColorTerm),
		drew, enableHint(need))
}

// enableHint is the actionable half of the downgrade notice: what to set, and to
// what, for the depth the requested table needs.
func enableHint(need TermDepth) string {
	switch need {
	case DepthTruecolor:
		return fmt.Sprintf("set %s=%s for %s", EnvColorTerm, ColorTermTruecolor, termDepthName(DepthTruecolor))
	default:
		return fmt.Sprintf("use a terminal whose %s declares it", EnvTerm)
	}
}

// termDepthName renders a depth for a human-readable notice.
func termDepthName(d TermDepth) string {
	switch d {
	case DepthTruecolor:
		return "24-bit colour"
	case Depth256:
		return "the 256-colour palette"
	default:
		return "8-colour support"
	}
}

// CapabilityForEnvMeasured resolves the capability AND folds in what the
// terminal itself answered (probe.go). It is the entry point a caller that has a
// measurement IN HAND uses, and it delegates the declaration half to
// CapabilityForEnv, so the precedence between the environment's knobs is still
// decided in ONE place.
//
// A caller with a LIVE descriptor does not build the measurement itself — it
// calls CapabilityForOpenTerminal, which is the one place "a terminal was opened,
// so ask it" lives. This function is the half below that: the fold, for the rare
// caller that obtained the measurement some other way.
//
// A caller that has no terminal to question (the `gopi ui` surface builds its
// sink from a configured output, a test drives a buffer) calls CapabilityForEnv
// and gets exactly the pre-probe behaviour.
func CapabilityForEnvMeasured(noColor bool, sink *os.File, m CapabilityMeasurement) Capability {
	return CapabilityForEnv(noColor, sink).WithMeasurement(m)
}

// CapabilityForOpenTerminal resolves the whole classification for a caller that
// HAS a terminal open: the declaration it already resolved (CapabilityForEnv),
// with the terminal's own answers folded in (WithMeasurement), in the one order
// the precedence allows.
//
// IT ASKS THE TERMINAL, so it is the entry point a caller with a live descriptor
// must use, and the reason it exists is a measured defect rather than a taste.
// `gopi-replay` drew a whole film with the env-only resolver while it held an
// open tty: its launcher's environment declared
// neither a 256-colour TERM nor COLORTERM, so the declaration landed on
// DepthBasic, the table fell to BasicPalette, and the product's own coloured
// renderer produced a grey-scale recording. The terminal was right there and was
// never asked — the information was not missing, the code path was.
//
// THE DECLARATION IS THE CALLER'S, and that is deliberate rather than a
// convenience: a run resolves the environment ONCE (Rule 2), because a
// capability that decides anything on stderr — the palette downgrade notice does
// — would otherwise say the same thing twice. Its only production caller now is
// `gopi chat`'s lineedit_wire.go: the second one lived in the abolished
// self-drawn replay renderer, which was removed with the `replay` verb on
// 2026-10-03 (replay is `gopi chat --replay`), and its duplicate probe went with
// it. The caller hands in the
// capability it already built with CapabilityForEnv, and this function adds
// exactly one thing: the question the caller could not ask for itself.
//
// WHY THE PROBE IS GATED ON THE COLOUR SWITCH, and why that gate is honest rather
// than a shortcut. A probe writes query bytes onto the operator's screen and
// takes answers off their input path, so it may only run when an answer could
// change a decision. With colour OFF (--no-color, NO_COLOR, TERM=dumb, a sink
// that is not a terminal) no SGR byte may reach the sink at all, so no depth can
// change the table — the gate is the SAME condition CapabilityForEnv uses to skip
// its declaration reads, not a second policy about the probe.
//
// A SHORT WRITE IS NOT "NOTHING CAME BACK": the probe's own vocabulary keeps
// silence and a partial reply apart (CapabilityMeasurement), and a measurement
// that read nothing is still recorded on the returned capability, so a report can
// say "asked, and here is what the terminal said" rather than implying the
// question was never put.
//
// THE TWO CLASSIFICATIONS A REVIEWER CAN SEE are therefore distinguishable at
// the type level rather than by a grep: the value returned here may carry an open
// terminal's answer (Measurement().Measured()), while the one from
// CapabilityForEnv carries none. Nothing about the ENVIRONMENT can make a
// colour-capable call site claim the terminal half, because no environment
// variable is read here at all.
func CapabilityForOpenTerminal(declared Capability, term *Terminal) (Capability, CapabilityMeasurement) {
	if term == nil || declared.Colour() != ColourOn {
		return declared.WithMeasurement(CapabilityMeasurement{}), CapabilityMeasurement{}
	}
	m := term.ProbeCapability()
	return declared.WithMeasurement(m), m
}

// PaletteForEnvCapability is the one call a producer needs at startup: resolve
// the capability and take the table in a single step, so the two can never be
// resolved against different environment states.
func PaletteForEnvCapability(noColor bool, sink *os.File) Palette {
	return PaletteForCapability(CapabilityForEnv(noColor, sink))
}

// truecolorMarker is the parameter prefix a 24-bit foreground carries
// (`38;2;…`), composed from internal/sgr's own names rather than spelled here
// (Rule 13/14). It is what the depth derivation looks for in a table's sequences.
func truecolorMarker() string {
	return strconv.Itoa(sgr.ExtendedFg) + string(sgr.ParamSeparator) + strconv.Itoa(sgr.ColorModeTruecolor)
}

// palette256Marker is the parameter prefix a 256-palette foreground carries
// (`38;5;…`), the middle tier's wire spelling — `38;5;n` addresses one of the
// Palette256 entries. Composed from internal/sgr's constant names, never typed.
func palette256Marker() string {
	return strconv.Itoa(sgr.ExtendedFg) + string(sgr.ParamSeparator) + strconv.Itoa(sgr.ColorModePalette)
}

// RequiredDepth reports the deepest tier the table's own sequences need — the
// table's declaration of where it stops being understood, DERIVED from the bytes
// it emits rather than declared beside them, so a table that grows a deeper
// colour is gated automatically (Rule 13: the property cannot drift away from the
// data).
//
// The order matters: a 24-bit table is checked first, because `38;2` is the
// deeper form of the same extended-colour syntax a 256 table uses and a table
// carrying any 38;2 needs the deeper tier regardless of what else it carries.
func (p Palette) RequiredDepth() TermDepth {
	if p.carries(truecolorMarker()) {
		return DepthTruecolor
	}
	if p.carries(palette256Marker()) {
		return Depth256
	}
	return DepthBasic
}

// carries reports whether ANY field of the table emits this parameter prefix.
func (p Palette) carries(marker string) bool {
	for _, st := range p.fields() {
		if strings.Contains(st.open, marker) {
			return true
		}
	}
	return false
}

// NeedsTruecolor reports whether any field of the table paints a 24-bit colour.
// It is the head of RequiredDepth's ladder, kept as its own name because that is
// the question the capability gate asks most often and the spelling the existing
// tests and callers use.
func (p Palette) NeedsTruecolor() bool { return p.RequiredDepth() == DepthTruecolor }

// fields returns the table's styles in a fixed order, so a walker over "every
// field" is written once (NeedsTruecolor, the tests) instead of listing the
// fields again at each site — the mirror that goes stale when a field is added.
func (p Palette) fields() []Style {
	return []Style{
		p.Banner, p.Prompt, p.StatusLabel, p.StatusLive, p.Thinking, p.ToolRow,
		p.Answer, p.AnswerBold, p.Code, p.DiffAdd, p.DiffDel, p.Hint, p.Warning, p.Error,
		p.StatusGood, p.StatusFair, p.StatusBad,
	}
}

// PaletteNames returns the legal values of the palette knob, in the order the
// usage text and the error above print them.
func PaletteNames() []string {
	return []string{PaletteNameBasic, PaletteNamePalette256, PaletteNameHermes, PaletteNameHermesLight}
}
