package main

import (
	"hash/fnv"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/fullid"
	"github.com/nickhuang99/gopi/internal/lineedit"
)

// chat_banner.go — the chat REPL's STARTUP SCREEN (the boxed logo + side panel).
//
// WHAT IT REPLACES, AND WHY THE REPLACEMENT NEEDS A CARRIER. The REPL used to
// open with ONE line (chatBannerLine, chat_stream.go) printed straight to
// stderr. A one-line banner is not a startup SCREEN: the operator (2026-09-28)
// asked for the shape the reference agent CLI opens with — a logo on the left,
// a side panel of getting-started tips and session facts on the right, both
// inside a border — and for gopi's OWN mark rather than an imitation of theirs.
//
// THE LAYOUT PROBLEM THE CARRIER SOLVES (measured, not assumed). Printing N
// rows to stderr at startup does NOT put N rows on the screen in the reserved
// layout. The editor installs its scroll region at row 1 and the band OWNS the
// top of the screen: raw stderr rows printed before it are pushed off the
// screen (into scrollback at best) the moment the layout takes over. Measured on
// a real pty (117x26, `script -qec`): the historic one-line banner sits at rows
// 3..9 before the layout installs and is GONE from the settled grid afterwards —
// a 20-row banner written the same way would be gone the same way. So the banner
// is NOT a startup print: it is the FIRST THING COMMITTED INTO THE BAND, the one
// surface the layout guarantees survives (region.go: the band's top margin is
// the screen's first row, so its rows are pushed into SCROLLBACK rather than
// destroyed). Scrollback is also where a startup screen belongs: it stays
// readable after the first answer scrolls past it.
//
// THE FALLBACK IS NOT A SECOND DESIGN. Every path that cannot carry the panel —
// the cooked/redirected run, a terminal too narrow for two readable columns —
// keeps the historic one-liner BYTE-FOR-BYTE. That is the same contract the rest
// of the chat chrome keeps (colour off is byte-identical to the historic
// output), and it is why chatBannerRows returns nil rather than a degraded box:
// a banner that changed shape on a redirected run would break every transcript
// reader, and a box drawn at half the width is a banner nobody can read.

// The banner's geometry. Named (Rule 1) because each is policy: the box's cell
// budget, the width at which the two-column shape is worth drawing at all, and
// the width the panel's own content demands.
const (
	// chatBannerLeftColMin is the floor the MARK column must clear. It is the
	// widest row of the logo art plus a cell of headroom on each side, so the
	// mark is never drawn clipped (chatBannerArtRowsMax bounds its height;
	// chatBannerLogoArt's own width bounds this).
	chatBannerLeftColMin = 22
	// chatBannerFactValueCells is how much room a fact VALUE may take. It caps
	// the panel's width so a long path cannot push the mark column off its own
	// floor; a longer value is shortened (chatBannerShortenDir) rather than
	// widening the panel.
	//
	// IT IS NOT THE IDENTITY BUDGET, and raising it would buy the identity
	// nothing. chatBannerPanelCols floors the panel at chatBannerFactCells+1+this
	// = 34 cells, and the panel is DRAWN at max(34, the widest tip) — i.e. at
	// chatBannerPanelMaxCols = 71 whenever the box is drawn at all, because the
	// box is only drawn when inner ≥ chatBannerPanelColsMin()+chatBannerLeftColMin+7,
	// i.e. at cols ≥ 100, i.e. at panel = 71. So a value of 36 cells (a full uuid)
	// is not what is missing; see chatBannerIdentityValue.
	chatBannerFactValueCells = 24
	// chatBannerPanelMaxCols bounds the width the tips are measured at: a tip
	// longer than this is cropped in the panel anyway, so measuring past it
	// would only widen the panel for text nobody sees.
	//
	// The value is the WIDEST TIP the panel may be asked to hold whole, and that
	// is the fallback newline line below: on a terminal that stayed silent the
	// operator has to be able to READ why Shift+Enter is not on offer, and a
	// panel narrower than that sentence would crop the explanation — leaving the
	// advice that the key does not work without the reason (measured: at the
	// previous cap of 44 the line rendered as "Alt+Enter inserts a newline (this
	// terminal …"). TestChatBannerPanelHoldsEveryTipWhole pins the relation
	// between this number and the vocabulary, so a longer tip fails red here
	// instead of being silently cropped on the operator's screen.
	chatBannerPanelMaxCols = 71
	// chatBannerGutterCells is every cell of a content row that is NOT one of
	// the two columns: the two outer borders, the two 1-cell paddings beside
	// them, and the inner separator column with its two 1-cell paddings
	// (`│ … │ … │`). It is the ONE definition of that arithmetic — the row
	// builder and the width tests both read it.
	chatBannerGutterCells = 7

	// chatBannerTitleLead is how many rule cells precede the title inside the
	// top border, so the title is not glued to the corner.
	chatBannerTitleLead = 2
	// chatBannerTitleTrail is how many rule cells follow the title before the
	// remaining rule runs to the corner — the title bar keeps at least this
	// much rule on its right so it reads as a title, not a clipped label.
	chatBannerTitleTrail = 2
	// chatBannerRuleCellsMin is the fewest cells a panel divider may be drawn
	// with; a shorter one is not a divider.
	chatBannerRuleCellsMin = 12
	// chatBannerFactCells is the width reserved for a fact's label (the left
	// half of "model      deepseek-flash"), so the panel's values line up.
	chatBannerFactCells = 9
	// chatBannerTipsMax bounds the tips list: a startup screen is a screen.
	// The tip chosen when the panel has room for fewer is by PRIORITY (see
	// chatBannerTips), never by truncation.
	//
	// IT IS ALSO THE ROTATION'S SLOT COUNT, and the two numbers are the same one
	// on purpose: the MUST tips are drawn first (they are the knowledge a
	// first-time user cannot guess), and every remaining slot is filled from the
	// ROTATING pool by this run's own seed (chatBannerRotateTips). A cap with no
	// room above the must list would make the rotation unreachable — the defect
	// the sibling task named ("位置满了，要按优先级取舍") read in the other
	// direction: 5 was FULL of must tips, so nothing ever varied.
	chatBannerTipsMax = 6
	// chatBannerTipWeight* are the rotation's weights: how often a rotating tip
	// is drawn relative to its siblings. A high weight is "stable/frequent", a
	// low one is the occasional surprise — and a weight is the ONLY way a tip's
	// frequency is stated, so "why is /queue always there" has one answer.
	chatBannerTipWeightHigh   = 3
	chatBannerTipWeightNormal = 2
	chatBannerTipWeightLow    = 1
	// chatBannerLogListSub is the /log SUBCOMMAND the panel's own advice names
	// when it points a reader at the FULL session id: `/log sessions`
	// (runSlashLog, commands.go) lists the profile's sessions with their full
	// uuids, which is the ONE in-UI path from an elided identity to the whole
	// one. It is the SLASH spelling and not the CLI's (`gopi session log --sessions`):
	// the two carriers answer different argument shapes for the same engine, and
	// a tip that named the CLI's flag would be a command this REPL refuses
	// (measured: runSlashLog's switch answers last|sessions|summary|read|search).
	// It is an ALIAS of the handler's own word (commands.go's logSub* block),
	// never a second spelling, so the tip and the switch cannot diverge. The
	// value is a REPL VERB, not a directory: the profile's sessions ROOT is
	// config.SessionsDirName.
	chatBannerLogListSub = logSubSessions
	// chatBannerSeedMix* are the splitmix64 constants the per-run seed is
	// stirred with. They are named (Rule 1) because the rotation's determinism
	// rests on them: a test asserts a fixed seed yields a fixed list, and that
	// assertion is about these numbers.
	chatBannerSeedMixA  = 0x9e3779b97f4a7c15
	chatBannerSeedMixB  = 0xbf58476d1ce4e5b9
	chatBannerSeedMixC  = 0x94d049bb133111eb
	chatBannerSeedShift = 30
	// chatBannerArtRowsMax bounds the logo art: every art row costs a screen
	// row before the first answer, and the art is decoration (it is also the
	// first thing to give way when the box must shrink — see chatBannerRows).
	chatBannerArtRowsMax = 7
	// chatBannerStatusRows / chatBannerEditorRows are the rows the reserved
	// layout holds BELOW the band: the editing line and the status row
	// (internal/lineedit/region.go's statusRows and minEditorRows). They are
	// spelled here because the banner's own row budget — how much of the screen
	// it may occupy before the layout takes the rest — is the terminal's height
	// less exactly these, and the box must never be drawn over the chrome.
	chatBannerStatusRows = 1
	chatBannerEditorRows = 1
)

// The box's own glyphs, named once (Rule 1b/13). They are the box-drawing
// vocabulary the rest of the chat chrome already uses (lineedit's TurnRuleGlyph
// is "─"), so the banner is not a second drawing style.
const (
	chatBannerTopLeft     = "╭"
	chatBannerTopRight    = "╮"
	chatBannerBottomLeft  = "╰"
	chatBannerBottomRight = "╯"
	chatBannerRule        = "─"
	chatBannerEdge        = "│"
)

// chatBannerSpace is the filler cell — one constant rather than a bare " ", so
// the padding arithmetic reads as padding.
const chatBannerSpace = " "

// chatBannerCutMarker marks a line the box could not hold whole, so a reader
// knows a value continues rather than believing it simply ends there.
const chatBannerCutMarker = "…"

// chatBannerLogoArt is gopi's OWN mark — the startup screen's logo, drawn in the
// half-block vocabulary this interface already uses (the status row's own
// full-cell glyph is █, chat_stream.go's statusBarFullCell). It is the WORDMARK
// read off the product name — "GO", the first two letters, which is also the
// short name everyone types — built as a 3D plate rather than as a flat one:
//
//   - the FACE is the full block █, so the letters read as solid matter;
//   - the THIRD DIMENSION is the medium-shade drop shadow ▓, cast one cell down
//     and one cell right of the face and MASKED TO THE OUTSIDE of the letter,
//     so the shadow never crosses a counter (the hole in the "O", the mouth of
//     the "G") and the letter stays legible at a glance. That mask is what makes
//     this a 3D plate instead of a doubled outline;
//   - the shadow carries the medium shade rather than the dark one because the
//     block vocabulary already spends the darkest cells on the face: a ▓ cast
//     against a █ face is a grey plate standing off a black one, which is what
//     the terminal can actually render.
//
// WHY THE MARK IS NOT ANOTHER PRODUCT'S WORDMARK. The reference startup screen
// the operator pointed at (2026-10-03) opens with a 3D-shadow wordmark of ITS own
// name; the lesson taken from it is the TECHNIQUE — a solid face with a
// down-right cast shadow — never the letters. This mark spells gopi's own name,
// which is why the wide variant below (chatBannerLogoArtWide) can exist at all:
// "GO" is both the short name and the first two letters of the long one, so the
// same art family covers both.
//
// ROWS ARE CENTRED in the left column, and a variant is drawn WHOLE — never
// cropped — or not at all: a mark with its side cut off is a rendering defect,
// not decoration (chatBannerLogoVariant is the one place the choice is made).
var chatBannerLogoArt = []string{
	" ██████     ██████",
	"██▓▓▓▓██   ██    ██",
	"██▓    ▓▓  ██    ██▓",
	"██▓ ████   ██    ██▓",
	"██▓  ▓██▓  ██    ██▓",
	" ██████▓▓   ██████▓▓",
	"  ▓▓▓▓▓▓     ▓▓▓▓▓▓",
}

// chatBannerLogoArtWide is the SAME mark spelled in full: "GOPI", the product
// name (chatProductName), for the rooms where the short wordmark's 20 cells would
// strand a wide left column. It is the identical face/shadow construction and the
// identical row count (chatBannerArtRowsMax) — the two variants are ONE DESIGN at
// two lengths, so a terminal that earns the wide one is not shown a different
// logo, only more of it.
//
// It is drawn only when the mark column can hold it AND the panel has already
// been paid for out of the same inner width (chatBannerLogoVariant), which is the
// arithmetic that keeps the wide mark from ever squeezing the panel beside it.
var chatBannerLogoArtWide = []string{
	" ██████    ██████   █████   ████",
	"██▓▓▓▓██  ██    ██  ██  ██   ▓██▓",
	"██▓    ▓▓ ██    ██▓ ██  ██▓   ██▓",
	"██▓ ████  ██    ██▓ █████▓▓   ██▓",
	"██▓  ▓██▓ ██    ██▓ ██▓▓▓▓    ██▓",
	" ██████▓▓  ██████▓▓ ██▓     ████▓",
	"  ▓▓▓▓▓▓    ▓▓▓▓▓▓   ▓▓      ▓▓▓▓",
}

// chatBannerLogoVariant picks the mark for a column of width leftCols: the WIDE
// wordmark when the column can hold it whole, the SHORT one otherwise, and
// nothing when even the short one is cut — never a cropped mark (see
// chatBannerLogoArt's own note).
//
// THE ORDER IS THE DESIGN, and it is why the wide variant costs nothing: the
// short art is the FLOOR every two-column box is built on (chatBannerLeftColMin),
// so the wide art can only APPEAR on a terminal that would otherwise leave the
// mark's column half empty. There is no width at which choosing the wide variant
// takes cells from the side panel — the panel's width is decided by
// chatBannerPanelCols, which never consults the mark beyond its floor.
func chatBannerLogoVariant(leftCols int) []string {
	if chatBannerArtFitsArt(chatBannerLogoArtWide, leftCols) {
		return chatBannerLogoArtWide
	}
	if chatBannerArtFitsArt(chatBannerLogoArt, leftCols) {
		return chatBannerLogoArt
	}
	return nil
}

// The left column's prose. chatBannerWelcome is the greeting — the one line that
// makes the box read as a STARTUP SCREEN rather than as a table of internals —
// and chatBannerTagline states what this surface is, in the vocabulary the usage
// text already uses.
const (
	chatBannerWelcome = "Welcome back!"
	chatBannerTagline = "interactive chat, in this terminal"
)

// The panel's two section headers: the operator-facing names of what it holds.
const (
	chatBannerTipsHeader    = "Tips for getting started"
	chatBannerSessionHeader = "This session"
	// chatBannerNoticesHeader heads the section the run's OWN start-up notices
	// are moved to (chat_panel_notice.go): the lines the REPL used to commit
	// into the answer band, where they read as transcript. The operator's own
	// instruction is the reason the section exists on a STARTUP SCREEN at all —
	// "those are tips for user", and this screen is where a tip belongs.
	chatBannerNoticesHeader = "Notices"
	// chatBannerNoticesMore is what the section says when the screen cannot hold
	// every notice: the words that did not fit are then printed through the
	// historic stream by the caller (chatBannerNoticeFit reports how many were
	// stated), so a NOTICE is never lost — only moved (Rule 15).
	chatBannerNoticesMore = "… more below"
)

// The notice section's own geometry. chatBannerNoticeGutterCells is every cell
// of a notice row that is not text: the two outer borders plus one padded cell
// beside each of them (`│ … │`) — the same arithmetic the two-column rows do
// with a separator in the middle.
const (
	chatBannerNoticeGutterCells = 4
	// chatBannerNoticeTailRows is the chrome a notice section always ends with:
	// one blank content row above the closing border. It is reserved BEFORE the
	// notices are fitted, so a section can never end with the box cut open.
	chatBannerNoticeTailRows = 1
)

// The newline-shortcut tip's wording: ONE constant per state of the keyboard
// negotiation (internal/lineedit/keyprotocol.go).
//
// The screen states the key THIS terminal makes work, because the two differ by
// terminal and a tip that is wrong for the terminal in front of the operator is
// worse than no tip at all: the operator presses it, the message submits, and
// then nothing in the panel is believed again.
const (
	// chatBannerTipNewlineKitty is the tip when the terminal ANSWERED the
	// keyboard-protocol query: it reports Shift+Enter as its own event, so the
	// key the operator would reach for anyway is the key to name.
	chatBannerTipNewlineKitty = "Shift+Enter inserts a newline"
	// chatBannerTipNewlineFallback is the tip on a terminal that stayed SILENT —
	// the operator's own terminal (xterm; measured 2026-10-02, see
	// devdoc/kitty-keyboard-protocol.md). It says BOTH things on purpose: the key
	// that works, and why the obvious one cannot be offered. "Alt+Enter inserts a
	// newline" alone leaves the operator who reaches for Shift+Enter with a
	// submitted message and no explanation, and the reason is exactly what the
	// negotiation measured.
	chatBannerTipNewlineFallback = "Alt+Enter inserts a newline (this terminal cannot report Shift+Enter)"
)

// chatBannerTipPriority ranks a MUST tip on the panel: LOWER IS EARLIER, and the
// list is drawn in this order (chatBannerMustTips). It is the panel's own
// statement of what the screen LEADS with.
//
// WHY IT EXISTS AT ALL, when every must tip is drawn anyway. The order of these
// lines is not a preference, and two of them are a CONTRACT: this surface's e2e
// reads the pty stream and stops at the exit sentence's marker
// (testsuite/chat_pty_interrupt_test.go), so the interrupt sentence has to reach
// the screen BEFORE the exit one, or that reader gets a block with the interrupt
// sentence still unpainted. Naming the ranks is how "the order is deliberate"
// stops being a comment nobody can test — before this type the sequence was the
// slice literal's, and the literal's order IS the thing that broke the contract.
//
// The VALUES are spaced by ten (not one) so a future line can be slotted between
// two neighbours without renumbering the ones already argued about.
type chatBannerTipPriority int

// The priorities themselves, with the reasoning each one encodes.
const (
	// chatBannerTipPriorityDiscover — the pointer to everything else, and the
	// line a first-time user needs FIRST: until they know /help exists, the rest
	// of the screen is a list of things they cannot look up.
	chatBannerTipPriorityDiscover chatBannerTipPriority = 10
	// chatBannerTipPriorityInterrupt ranks SECOND, and it MUST precede the exit
	// sentence — the stream-order contract above. Do not "tidy" the two.
	chatBannerTipPriorityInterrupt chatBannerTipPriority = 20
	// chatBannerTipPriorityExit — how to LEAVE. The most load-bearing line on the
	// screen for a human (an operator who cannot leave is not using the program),
	// and second only to the ordering rule above.
	chatBannerTipPriorityExit chatBannerTipPriority = 30
	// chatBannerTipPriorityNewline — how to write more than one line in a
	// message. It is the one line on this panel that is PERISHABLE: the commands
	// are listed by /help and the two keys are the ones every terminal shares,
	// while this one depends on what THIS terminal can report — an operator who
	// does not learn it about their own terminal has nothing else to read that
	// would tell them. It is also the only tip whose ABSENCE is itself a fact
	// (the cooked path), so it has to earn the row it costs.
	chatBannerTipPriorityNewline chatBannerTipPriority = 40
	// chatBannerTipPriorityTools — /tools, BELOW the pointer because /help is the
	// door to this and every other command. It is marked toolsOnly: on a run
	// without tools it is not "ranked last", it is INAPPLICABLE, and its row goes
	// to the rotation instead of shrinking the panel.
	chatBannerTipPriorityTools chatBannerTipPriority = 50
)

// chatProductName is the binary's own name, spelled once: the title bar and the
// mark are both stated in it.
const chatProductName = "gopi"

// The session panel's fact labels. Named because they are the panel's own
// vocabulary and because their longest one sets chatBannerFactCells.
const (
	chatBannerFactVersion = "version"
	chatBannerFactModel   = "model"
	chatBannerFactNode    = "node"
	chatBannerFactDir     = "dir"
	chatBannerFactTools   = "tools"
	// chatBannerFactSession is THIS session's own identity — the row that lets
	// a reader name the session they are typing into without leaving the screen
	// or knowing anything in advance (see chatBannerFacts' order, and
	// chatBannerIdentityValue for the two shapes the value takes).
	chatBannerFactSession = "session"
	// chatBannerFactSurface is THIS run's own OBSERVABILITY: whether its screen
	// can be read back, the session directory its identity claim names, and the
	// key that inserts a newline on this terminal. It is the panel's answer to
	// the startup notices that used to be printed into the answer band
	// (chat_panel_notice.go carries the measurement) — the SAME facts, stated
	// where the operator asked for them.
	chatBannerFactSurface = "surface"
	// chatBannerSessionFactCount is how many fact rows the panel can print at
	// most — the capacity the panel's row slice is sized from. One per label
	// above; a row budget that has to shrink sheds the LAST fact first, so this
	// number and chatBannerFacts' order are one decision (see chatBannerFacts).
	chatBannerSessionFactCount = 7
)

// The tools fact's two values. The OFF word is the status row's own
// (statusNoToolsLabel), so the startup screen and the live row cannot describe
// the same configuration with two different spellings; the ON word is its
// opposite and is named here for the same reason.
const (
	chatBannerToolsOn  = "tools on"
	chatBannerToolsOff = statusNoToolsLabel
)

// bannerSpan is one styled run of a row. The banner is composed as spans rather
// than as one string per row because the box's hierarchy is carried by COLOUR
// (the rule/edge chrome is secondary, the title and the mark take the banner's
// own emphasis, the panel's values are reading matter) and because the column
// arithmetic must run on the PLAIN text: a width measured over a string that
// already carries an SGR sequence counts the sequence's own cells — the measured
// defect cutCols' header records ("counting `\x1b[2m` as four cells shortened
// every row after it").
type bannerSpan struct {
	text  string
	style lineedit.Style
}

// bannerRow is one finished row: its spans and the plain cell width they
// occupy. The width is accumulated from lineedit.DisplayWidth — the same
// measure the band's own layout uses — so the banner cannot disagree with the
// layout about how wide its text is.
type bannerRow struct {
	spans []bannerSpan
	cells int
}

// add appends spans and accumulates their plain width.
func (r *bannerRow) add(style lineedit.Style, parts ...string) {
	for _, p := range parts {
		if p == "" {
			continue
		}
		r.spans = append(r.spans, bannerSpan{text: p, style: style})
		r.cells += lineedit.DisplayWidth(p)
	}
}

// addBounded appends ONE span that must fit in the room left. It is the one
// place a banner line is clipped, so every row's width invariant is enforced by
// construction rather than re-checked per call site.
func (r *bannerRow) addBounded(style lineedit.Style, text string, budget int) {
	if budget <= 0 || text == "" {
		return
	}
	r.add(style, chatBannerCrop(text, budget))
}

// padTo appends blank cells until the row occupies exactly width cells. It
// reports false when the row is ALREADY wider — the caller's cue that its
// arithmetic is wrong, never something to paper over with a negative pad (the
// box's alignment depends on every row being exactly the terminal's width).
func (r *bannerRow) padTo(style lineedit.Style, width int) bool {
	if r.cells > width {
		return false
	}
	if r.cells < width {
		r.add(style, strings.Repeat(chatBannerSpace, width-r.cells))
	}
	return true
}

// render draws the row's spans. The monochrome table's zero styles emit nothing,
// so the colour-off path produces the plain text and nothing else.
func (r *bannerRow) render() string {
	var b strings.Builder
	for _, s := range r.spans {
		b.WriteString(s.style.Wrap(s.text))
	}
	return b.String()
}

// chatBannerSpec is everything the banner draws from. It is a struct rather
// than a long parameter list because the caller assembles it once and the
// renderer is a PURE function of it — which is what makes the banner testable
// without a terminal.
type chatBannerSpec struct {
	// cols is the terminal's width in cells. The banner is exactly this wide.
	cols int
	// version is this binary's version (buildVersion).
	version string
	// model is the conversation model (agent.Agent.Model) — the same string the
	// status row's first column carries, so the two cannot disagree.
	model string
	// node is the declared node identity (chatNodeIdentity's answer), or the
	// "undeclared" spelling when the topology was never declared.
	node string
	// dir is the session's ground directory. Empty means the session has no
	// ground, and the fact is then omitted rather than faked.
	dir string
	// sessionID is THIS session's own identity — the run's uuid
	// (agent.SessionLogger.SessionID, the same id `gopi session log` and `gopi chat
	// --resume` resolve), or "" when there is no session record to name. It is a
	// field of the spec for the same reason the others are: the identity fact is
	// rendered by a PURE function (chatBannerIdentityValue) that must render
	// exactly what the spec carries, and the elision is a decision about the room
	// the panel has rather than about the id.
	sessionID string
	// chatBannerSurface is what the panel says about THIS run's own
	// observability (the "surface" fact and the section it heads), or the zero
	// value when the panel must say nothing about it — see chatBannerSurfaceFact.
	surface chatBannerSurface
	// notices are the run's OWN startup notices — the `[gopi] …` lines the REPL
	// would otherwise commit into the answer band (chat_panel_notice.go). A
	// non-empty list makes the box grow a NOTICES section below its panel, which
	// is where the operator asked to read them: they are tips, not transcript.
	notices []bannerNotice
	// toolsOn reports whether this run has tools, which is what the /tools tip
	// and the panel's tools fact are about.
	toolsOn bool
	// keyboardProtocol is the keyboard negotiation's verdict for THIS terminal,
	// read through the same accessor the startup notice uses
	// (internal/lineedit/keyprotocol.go; chatInput.keyboardProtocol). It selects
	// the newline tip's wording, and its zero value
	// (lineedit.KeyboardProtocolUnavailable) means "no terminal, nothing was
	// measured" — the cooked path, where the tip is not printed at all.
	keyboardProtocol lineedit.KeyboardProtocol
	// maxRows is the band's own height — the rows the startup screen may
	// occupy before it would be pushed straight into scrollback by its own
	// carrier. Zero means "unbounded" (a caller with no layout), and the
	// renderer then draws the full box.
	maxRows int
	// tipSeed selects WHICH of the rotating tips this run draws. It is a field
	// of the spec — and not a call to a clock inside the renderer — because the
	// renderer is a PURE function of the spec (see the struct's own comment):
	// both the panel's width measurement and the rows it finally draws call
	// chatBannerTips, and a seed read twice from the clock could pick two
	// different lists, measuring the panel for a tip it then does not draw.
	tipSeed uint64
	// pal is the resolved colour table. The monochrome table emits nothing on
	// every style, which is what keeps a redirected run's bytes unchanged.
	pal lineedit.Palette
}

// bannerNotice is one notice the startup screen draws in its own NOTICES
// section: the panel's short copy of a start-up line the run would otherwise
// commit into the answer band (chat_panel_notice.go carries the report, the
// design and the fallback).
type bannerNotice struct {
	text string
}

// chatBannerScreen renders the whole startup screen — the box, plus the section
// this run's own start-up notices are moved into — and reports the notices the
// screen could NOT hold, in their original order, so the caller prints them
// through the historic stream instead of losing them (Rule 15).
//
// WHY THE BOX AND THE NOTICE SECTION ARE FITTED SEPARATELY. chatBannerRows owns
// the two-column box's arithmetic and its row budget; the notice section is
// prose that must be readable whole, so it is fitted against the room the box
// left and is dropped WHOLE (never half-wrapped) when the screen cannot hold it.
// The two callers of chatBannerNoticeFit — this one, which draws the section,
// and the REPL, which prints what did not fit — pass the same spec and the same
// box height, so they cannot disagree about which notice went where.
func chatBannerScreen(spec chatBannerSpec) ([]string, []bannerNotice) {
	base := chatBannerRows(spec)
	if len(base) == 0 {
		return nil, spec.notices
	}
	if len(spec.notices) == 0 {
		return base, nil
	}
	section, stated := chatBannerNoticeFit(spec, len(base), spec.maxRows)
	if len(section) == 0 {
		return base, spec.notices
	}
	out := make([]string, 0, len(base)+len(section))
	out = append(out, base[:len(base)-1]...)
	out = append(out, section...)
	out = append(out, base[len(base)-1])
	return out, spec.notices[stated:]
}

// chatBannerRows renders the startup screen, one entry per screen row. It
// returns nil when the terminal cannot carry it (too narrow for two readable
// columns, or an over-wide composition), and the caller then prints the
// historic one-liner: nil is a DECISION, not a failure, and it is why the box
// never appears half-drawn.
func chatBannerRows(spec chatBannerSpec) []string {
	inner := spec.cols - chatBannerGutterCells
	if inner <= 0 {
		return nil
	}
	// THE PANEL'S WIDTH IS DERIVED FROM WHAT IT MUST SAY, not from a fixed
	// share of the terminal. A share looks reasonable until the sentence that
	// must survive — chatBannerCtrlCTail, the statement the interrupt e2e case
	// reads off the screen — is a cell longer than the share allows, and the
	// panel then CROPS the one line that is not decoration (measured at 100
	// columns with a 34% share: the tip rendered as "Ctrl+C interrupts the
	// running …"). The floor is therefore the sentence's own width, and the
	// panel grows past it only as far as the content asks and the mark column
	// can spare.
	rightCols := chatBannerPanelCols(spec, inner)
	leftCols := inner - rightCols
	if rightCols < chatBannerPanelColsMin() || leftCols < chatBannerLeftColMin {
		return nil
	}

	artFits := chatBannerArtFits(leftCols)
	// THE MARK IS CHOSEN LONG-FIRST: chatBannerLogoVariant answers the WIDE
	// wordmark when the column can hold it and the SHORT one otherwise, so the
	// logo is not a fixed number of cells pasted into a variable room — a wide box
	// shows the whole name, a narrow one shows the mark the narrow geometry was
	// derived from. Both variants cost the same rows (chatBannerArtRowsMax), so
	// the row-budget loop below does not care which one answered; and BOTH are
	// drawn whole or not at all, which is chatBannerLogoVariant's own contract.
	mark := chatBannerLogoVariant(leftCols)
	tips := chatBannerTips(spec, rightCols)
	facts := chatBannerFacts(spec, rightCols)

	// THE ROW BUDGET. The startup screen is committed into the answer band, and
	// the band holds only so many rows: a banner taller than its own carrier
	// would be pushed into scrollback by the very block that draws it, so the
	// screen would never be seen. The content is therefore reduced by PRIORITY —
	// the tips first, then the least load-bearing facts, the mark LAST — until
	// the box fits, and it is never cropped to fit. The order is the design
	// decision: the mark is the thing the operator asked for, so the tips give
	// way before it, and the identity facts (which model answers) outlive the
	// cosmetic ones.
	for nTips := len(tips); nTips >= 1; nTips-- {
		for _, withArt := range []bool{true, false} {
			if withArt && !artFits {
				continue
			}
			for nFacts := len(facts); nFacts >= 1; nFacts-- {
				rows, ok := chatBannerCompose(spec, leftCols, rightCols, tips[:nTips], mark, withArt, facts[:nFacts])
				if !ok {
					// A composition that will not pad to the terminal's width has
					// a broken column arithmetic: that is a programming error, and
					// the fallback keeps a sheared box off the screen instead of
					// padding the arithmetic into looking right.
					return nil
				}
				if spec.maxRows <= 0 || len(rows) <= spec.maxRows {
					return rows
				}
			}
		}
	}
	return nil
}

// chatBannerPanelCols is the side panel's width for a given inner width: the
// widest line the panel will actually print, capped so the mark column keeps its
// floor. The cap is the whole reason the panel cannot swallow the screen — a
// long directory in a fact value is SHORTENED (chatBannerShortenDir) rather than
// paid for with the mark's cells.
func chatBannerPanelCols(spec chatBannerSpec, inner int) int {
	want := chatBannerPanelColsMin()
	if w := lineedit.DisplayWidth(chatBannerTipsHeader); w > want {
		want = w
	}
	if w := lineedit.DisplayWidth(chatBannerSessionHeader); w > want {
		want = w
	}
	// The tips, at their natural length: the panel is what the tips are written
	// for, so a tip is not cropped by a panel narrower than the tip's author
	// assumed. chatBannerFactCells + a value's budget is the facts' own demand.
	for _, tip := range chatBannerTips(spec, chatBannerPanelMaxCols) {
		if w := lineedit.DisplayWidth(tip); w > want {
			want = w
		}
	}
	// The facts' own demand: the label field plus the room a value needs. THE ROOM
	// IS THE IDENTITY'S when this run has one, and it is DERIVED from
	// fullid.UUIDLen rather than typed (Rule 1): the session fact is the panel's
	// FIRST, so a panel sized below "label + a full uuid" would print its own
	// leading fact ELIDED on a terminal where the tip list happens to be short —
	// measured: on a kitty-capable terminal the newline tip is 29 cells instead of
	// the silent-terminal fallback's 71, every other tip is shorter still, and the
	// panel would come out 45 cells wide, i.e. a 35-cell value column for a
	// 36-cell uuid. With the identity in play the panel is therefore at least
	// chatBannerFactCells+1+fullid.UUIDLen = 46, which is under
	// chatBannerPanelMaxCols, so this demand never enlarges the panel past the
	// tip-sized maximum — it only stops a short tip list from starving the fact
	// column.
	factValue := chatBannerFactValueCells
	if spec.sessionID != "" && fullid.UUIDLen > factValue {
		factValue = fullid.UUIDLen
	}
	if w := chatBannerFactCells + 1 + factValue; w > want {
		want = w
	}
	if max := inner - chatBannerLeftColMin; want > max {
		want = max
	}
	return want
}

// chatBannerPanelColsMin is the floor a side panel must clear: the contract
// sentence plus one cell of trailing air. chatBannerCtrlCTail is the ONE line the
// panel may never crop — it is the statement the interrupt e2e case reads off the
// screen — so the floor is derived from that line rather than chosen.
func chatBannerPanelColsMin() int {
	return lineedit.DisplayWidth(chatBannerCtrlCTail) + 1
}

// chatBannerMinCols is the narrowest terminal that gets the BOX: the row's own
// gutters plus the two columns' floors. It is derived, not chosen, so the two
// floor constants cannot drift apart from the widths they describe.
func chatBannerMinCols() int {
	return chatBannerGutterCells + chatBannerLeftColMin + chatBannerPanelColsMin()
}

// chatBannerArtFits reports whether the SHORT mark may be drawn in a column of
// width leftCols — the floor the two-column box is built on.
func chatBannerArtFits(leftCols int) bool {
	return chatBannerArtFitsArt(chatBannerLogoArt, leftCols)
}

// chatBannerArtFitsArt reports whether ONE variant may be drawn in a column of
// width leftCols: the WHOLE art must fit, because a mark with its side cut off is
// a rendering defect rather than decoration.
func chatBannerArtFitsArt(art []string, leftCols int) bool {
	if len(art) == 0 || len(art) > chatBannerArtRowsMax {
		return false
	}
	for _, line := range art {
		if lineedit.DisplayWidth(line) > leftCols {
			return false
		}
	}
	return true
}

// chatBannerCompose draws one box from the content it is given. It reports ok
// false when a row cannot be padded to the terminal's exact width.
func chatBannerCompose(spec chatBannerSpec, leftCols, rightCols int, tips []string, mark []string, withArt bool, facts []bannerFact) ([]string, bool) {
	left := chatBannerLeftColumn(spec, leftCols, mark, withArt)
	right := chatBannerRightColumn(spec, rightCols, tips, facts)
	rows := len(left)
	if len(right) > rows {
		rows = len(right)
	}

	out := make([]string, 0, rows+2)
	out = append(out, chatBannerTitleRow(spec))
	for i := 0; i < rows; i++ {
		row := &bannerRow{}
		row.add(spec.pal.Hint, chatBannerEdge, chatBannerSpace)
		row.add(spec.pal.Hint, chatBannerCell(left, i, leftCols, spec.pal))
		row.add(spec.pal.Hint, chatBannerSpace, chatBannerEdge, chatBannerSpace)
		row.add(spec.pal.Hint, chatBannerCell(right, i, rightCols, spec.pal))
		row.add(spec.pal.Hint, chatBannerSpace, chatBannerEdge)
		if !row.padTo(spec.pal.Hint, spec.cols) {
			return nil, false
		}
		out = append(out, row.render())
	}
	out = append(out, chatBannerBorderRow(spec, chatBannerBottomLeft, chatBannerBottomRight))
	return out, true
}

// chatBannerCell renders row i of a column, already padded to the column's
// width — a column with fewer rows than its neighbour is padded rather than
// left ragged, because the box's right edge is where a missing pad shows.
func chatBannerCell(col []bannerRow, i, width int, pal lineedit.Palette) string {
	row := &bannerRow{}
	if i < len(col) {
		for _, s := range col[i].spans {
			row.add(s.style, s.text)
		}
	}
	if !row.padTo(pal.Hint, width) {
		// Unreachable for a correct column builder; the guard exists so a bad
		// column degrades to a blank padded row rather than to a sheared box.
		row = &bannerRow{}
		row.add(pal.Hint, strings.Repeat(chatBannerSpace, width))
	}
	return row.render()
}

// chatBannerTitleRow is the top border with the title set into it — the shape
// the reference CLI's startup screen uses, spelled with the names THIS program
// has (its product name and this binary's version).
func chatBannerTitleRow(spec chatBannerSpec) string {
	title := chatBannerSpace + chatBannerVersionLabel(spec.version) + chatBannerSpace
	rule := spec.cols - 2 - lineedit.DisplayWidth(title)
	if rule < chatBannerTitleLead+chatBannerTitleTrail {
		// No room for a title bar: the top border keeps its own shape and the
		// title is dropped rather than clipped through the corner.
		return chatBannerBorderRow(spec, chatBannerTopLeft, chatBannerTopRight)
	}
	row := &bannerRow{}
	row.add(spec.pal.Hint, chatBannerTopLeft, strings.Repeat(chatBannerRule, chatBannerTitleLead))
	row.add(spec.pal.Banner, title)
	row.add(spec.pal.Hint, strings.Repeat(chatBannerRule, rule-chatBannerTitleLead), chatBannerTopRight)
	row.padTo(spec.pal.Hint, spec.cols)
	return row.render()
}

// chatBannerBorderRow is a full-width border row: the corner glyphs plus
// `cols-2` rule cells.
func chatBannerBorderRow(spec chatBannerSpec, leftGlyph, rightGlyph string) string {
	row := &bannerRow{}
	row.add(spec.pal.Hint, leftGlyph, strings.Repeat(chatBannerRule, spec.cols-2), rightGlyph)
	row.padTo(spec.pal.Hint, spec.cols)
	return row.render()
}

// chatBannerVersionLabel is the title bar's product-and-version field. It is a
// function rather than an inline concatenation so the title bar has one place
// to read, and so a version already carrying its "v" is not given a second one.
func chatBannerVersionLabel(version string) string {
	if version == "" {
		return chatProductName
	}
	if strings.HasPrefix(version, "v") || strings.HasPrefix(version, "V") {
		return chatProductName + chatBannerSpace + version
	}
	return chatProductName + " v" + version
}

// chatBannerLeftColumn builds the mark column: the greeting, the logo (the
// variant chatBannerLogoVariant chose for this column's width), the tagline, and
// the identity lines (which model answers, on which node, over which directory).
// It answers "what am I talking to", and every line it cannot hold whole is
// CROPPED rather than wrapped — a wrapped fact would take a second row and the
// box's two columns would then disagree about the banner's height.
func chatBannerLeftColumn(spec chatBannerSpec, leftCols int, mark []string, withArt bool) []bannerRow {
	rows := make([]bannerRow, 0, chatBannerArtRowsMax+8)

	addCentred := func(style lineedit.Style, text string) {
		row := bannerRow{}
		body := chatBannerCrop(text, leftCols)
		pad := leftCols - lineedit.DisplayWidth(body)
		if pad < 0 {
			pad = 0
		}
		row.add(style, strings.Repeat(chatBannerSpace, pad/2), body)
		row.padTo(spec.pal.Hint, leftCols)
		rows = append(rows, row)
	}

	rows = append(rows, bannerRow{})
	addCentred(spec.pal.Answer, chatBannerWelcome)
	rows = append(rows, bannerRow{})
	if withArt {
		for i := 0; i < len(mark); i++ {
			addCentred(spec.pal.Banner, mark[i])
		}
		rows = append(rows, bannerRow{})
	}
	addCentred(spec.pal.Hint, chatBannerTagline)
	rows = append(rows, bannerRow{})
	if spec.model != "" {
		addCentred(spec.pal.StatusLabel, spec.model)
	}
	if spec.node != "" {
		addCentred(spec.pal.Hint, spec.node)
	}
	if spec.dir != "" {
		addCentred(spec.pal.Hint, chatBannerShortenDir(spec.dir, leftCols))
	}
	return rows
}

// chatBannerFacts is the session panel's facts, priority-ordered: WHAT this
// session is (its own identity), what is answering (the model, which the status
// row also carries), where this session writes (the dir), which node it is (the
// topology), and finally the version and the tools mode — the two that a reader
// can also get from the title bar and the status row, which is why they are the
// first to give way when the box has to shrink.
//
// THE ORDER IS THE ROW BUDGET'S SHED ORDER, and it is read the other way round
// from the tips: chatBannerRows takes facts[:nFacts], i.e. the HEAD of this list
// is drawn FIRST and the TAIL is what a short terminal loses. So the head is
// what the panel must say when it can say only one thing.
//
// WHY THE IDENTITY LEADS (this fact's own argument, replacing the order that put
// the model first). Every other fact here is obtainable ELSEWHERE on this screen
// or from a reader's prior knowledge: the model is the status row's first column
// and the answer band's own byline, the version is in the title bar one row
// above, the tools mode is in the status row (`tools on`/`no tools` — and the
// historic one-liner, which every fallback prints, carries it too), the node is
// the `[gopi] node: …` notice this run prints at startup, and the dir is the
// ground notice. The session's OWN ID is the one fact the operator cannot get
// from this screen in any other way, and the one they need in order to do the
// three things they actually do from here: `gopi session log <id>`, `gopi chat
// --replay <id>`, `gopi chat --resume <id>`. It is also the fact with no prior:
// every other value is a NAME a reader can recognise, while an id is
// unguessable — which is exactly the epic's "read the CURRENT session's identity
// without prior knowledge".
//
// A SHORT ID IS NOT AN IDENTITY, and this surface never prints one as if it were
// (fullid's own rule: a prefix names more than one thing — 2409 ledger rows on
// this host produce prefixes that are not unique, and this repo has already been
// bitten by a listing that printed 8 hex characters). So the identity is the
// FULL uuid when the panel's value column can hold it — 36 cells, and the panel
// is 71 cells wide whenever the box is drawn at all (see
// chatBannerFactValueCells) — and an ELIDED uuid, marked as elided, when the
// geometry genuinely cannot (see chatBannerIdentityValue for the two shapes and
// why the elided one is eyeball-only rather than a usable operand).
func chatBannerFacts(spec chatBannerSpec, cols int) []bannerFact {
	return []bannerFact{
		{label: chatBannerFactSession, value: chatBannerIdentityValue(spec.sessionID, cols-chatBannerFactCells)},
		{label: chatBannerFactModel, value: spec.model},
		{label: chatBannerFactDir, value: chatBannerShortenDir(spec.dir, cols-chatBannerFactCells)},
		{label: chatBannerFactNode, value: spec.node},
		{label: chatBannerFactVersion, value: spec.version},
		{label: chatBannerFactTools, value: chatBannerToolsValue(spec.toolsOn)},
		{label: chatBannerFactSurface, value: spec.surface.value()},
	}
}

// chatBannerIdentityValue renders the session fact's value: the FULL uuid when
// the value column can hold it, and a VISIBLY ELIDED uuid when it cannot.
//
// THE TWO SHAPES, and the honest difference between them:
//
//   - budget >= fullid.UUIDLen (36): the uuid verbatim. It is the identity, it
//     is an OPERAND — it can be copied off the screen into `gopi session log <id>`,
//     `make replay SESSION=<id>` or `gopi chat --resume <id>` — and it is the
//     shape the panel draws on every terminal that draws the box at all.
//   - budget < 36: the uuid is cut in the MIDDLE and the cut is marked with the
//     banner's own elision marker, so what is on the screen is visibly not a
//     whole identity. This shape is EYEBALL-ONLY: a reader matches it against a
//     listing, and types nothing from it. It is deliberately NOT a shortened
//     OPERAND — turning it into one would be the "shortest unique prefix" design
//     this epic bans, and the ban is right for the reason fullid records: a
//     prefix is an identifier that silently names more than one thing. A shorter
//     operand is never fabricated here; when the column cannot hold the whole
//     identity, the panel says so and the whole one is reached through /log.
//
// WHY THE MIDDLE IS KEPT. The uuid's first group is a timestamp and its last
// group is the random tail, so the head alone is the least identifying part: two
// sessions of one host share it (and the panel's own rotation seed depends on
// the whole thing). The kept halves are the two the reader sees in a listing —
// the leading group and the trailing one — which is what makes the elided shape
// matchable by eye against `/log <id>`'s output.
//
// The renderer's own crop (chatBannerCrop, called by the row builder with the
// same budget) is a BACKSTOP here rather than the mechanism: this function is
// the ONE place the identity's shape is decided, so the drawn value is a
// decision instead of a crop that happens to land at 35 cells.
func chatBannerIdentityValue(id string, budget int) string {
	if id == "" || budget <= 0 {
		// No session record to name: the fact is OMITTED (the renderer skips an
		// empty value) rather than faked with a blank or a made-up id.
		return ""
	}
	if lineedit.DisplayWidth(id) <= budget {
		return id
	}
	// Not enough room for the whole identity: keep the uuid's head and tail and
	// mark the middle as missing. The kept counts are derived from the marker's
	// own width so the result can never exceed the budget it was asked for.
	keep := budget - lineedit.DisplayWidth(chatBannerCutMarker)
	if keep < chatBannerIdentityKeepMin {
		// Even an elided uuid would be a fragment nobody can match: print the
		// marker alone rather than a stub that looks like a value. (Reached only
		// by a caller with a budget far below the panel's floor; the panel's own
		// floor is 34 cells.)
		return chatBannerCutMarker
	}
	head := keep / 2
	tail := keep - head
	if tail > lineedit.DisplayWidth(id)-head {
		tail = lineedit.DisplayWidth(id) - head
	}
	if head+tail >= lineedit.DisplayWidth(id) {
		return id
	}
	// TruncateToCols is the same measure the crop uses, so the head cannot be cut
	// mid-glyph and the two halves are measured with ONE width function.
	return lineedit.TruncateToCols(id, head) + chatBannerCutMarker + id[lineedit.DisplayWidth(id)-tail:]
}

// chatBannerIdentityKeepMin is the fewest cells an ELIDED identity may occupy
// before the panel stops pretending it is one: below this there is no room for a
// head AND a tail, and a one-sided fragment of a uuid is not matchable by eye
// against the same id in a listing — which is the only thing the elided shape is
// for. The value is derived from the uuid's own two groups (the leading 8-char
// time group and a 4-char tail): 8 + 1 + 4.
const chatBannerIdentityKeepMin = 13

// bannerFact is one label/value row of the session panel. A fact with an empty
// value is OMITTED by the renderer rather than printed blank: an absent fact
// says nothing, while a blank one invites the reader to read a value into it.
type bannerFact struct {
	label string
	value string
}

// chatBannerRightColumn builds the side panel: the getting-started tips, then
// the session's facts under a rule. Its LENGTH participates in the banner's
// height, so the caller hands in the tips and facts that fit rather than the
// panel growing past the box's row budget.
func chatBannerRightColumn(spec chatBannerSpec, cols int, tips []string, facts []bannerFact) []bannerRow {
	rows := make([]bannerRow, 0, len(tips)+len(facts)+4)
	add := func(style lineedit.Style, text string) {
		row := bannerRow{}
		row.addBounded(style, text, cols)
		row.padTo(spec.pal.Hint, cols)
		rows = append(rows, row)
	}
	// addFact prints one label/value pair: the label padded into
	// chatBannerFactCells so the panel's values line up in a column, and the
	// value bounded to the room actually left.
	addFact := func(f bannerFact) {
		if f.value == "" {
			return
		}
		row := bannerRow{}
		row.add(spec.pal.StatusLabel, chatBannerPadRight(chatBannerCrop(f.label, chatBannerFactCells), chatBannerFactCells))
		row.addBounded(spec.pal.Hint, f.value, cols-row.cells)
		row.padTo(spec.pal.Hint, cols)
		rows = append(rows, row)
	}

	add(spec.pal.StatusLabel, chatBannerTipsHeader)
	for _, tip := range tips {
		add(spec.pal.Hint, tip)
	}
	if cols >= chatBannerRuleCellsMin {
		rows = append(rows, bannerRow{})
		rule := bannerRow{}
		rule.add(spec.pal.Hint, strings.Repeat(chatBannerRule, cols))
		rule.padTo(spec.pal.Hint, cols)
		rows = append(rows, rule)
		rows = append(rows, bannerRow{})
	}
	add(spec.pal.StatusLabel, chatBannerSessionHeader)
	for _, f := range facts {
		addFact(f)
	}
	return rows
}

// chatBannerNoticeFit renders the notice section's rows for a box that already
// occupies boxRows rows, and reports how many notices it could state: the caller
// prints the rest through the historic stream so no notice is ever lost (Rule
// 15). avail <= 0 means "no ceiling" (a caller with no layout).
//
// It is a PURE function of the spec and the box's height, so the two callers —
// the renderer, which appends the rows, and the REPL, which prints what did not
// fit — cannot disagree about which notice went where.
func chatBannerNoticeFit(spec chatBannerSpec, boxRows, avail int) ([]string, int) {
	if len(spec.notices) == 0 {
		return nil, 0
	}
	body := chatBannerNoticeBody(spec.cols)
	if body <= 0 {
		return nil, 0
	}
	out := []string{}
	// The section's own chrome: the header, its rule, and one blank row before
	// the closing border. It is spent BEFORE any notice, because a section with
	// no header is a paragraph glued to the panel.
	out = append(out, chatBannerNoticeHeaderRow(spec, body))
	out = append(out, chatBannerNoticeTextRow(spec, chatBannerRule, body))
	stated := 0
	room := func() int {
		if avail <= 0 {
			return -1 // unbounded
		}
		return avail - (boxRows + len(out))
	}
	// The closing chrome has to be reserved up front, so a section that runs out
	// of room ends with the "continues" line rather than with the border cut off.
	for i, notice := range spec.notices {
		wrapped := lineedit.WrapToCols(notice.text, body)
		if len(wrapped) == 0 {
			wrapped = []string{""}
		}
		need := len(wrapped)
		if i > 0 {
			need++ // the blank row separating two notices
		}
		if r := room(); r >= 0 && need+chatBannerNoticeTailRows > r {
			// Not enough room for this notice AND the section's tail: stop here
			// and say so in one line, so the screen never ends mid-sentence.
			if r >= chatBannerNoticeTailRows+1 {
				out = append(out, chatBannerNoticeTextRow(spec, chatBannerNoticesMore, body))
			}
			break
		}
		if i > 0 {
			out = append(out, chatBannerNoticeTextRow(spec, "", body))
		}
		for _, line := range wrapped {
			out = append(out, chatBannerNoticeTextRow(spec, line, body))
		}
		stated = i + 1
	}
	if stated == 0 {
		return nil, 0
	}
	out = append(out, chatBannerNoticeTextRow(spec, "", body))
	return out, stated
}

// chatBannerNoticeBody is the content width of a notice-section row: the box's
// OWN border, one cell of padding on each side, and the text between them — the
// full width the two columns share, because a notice is prose rather than a
// two-column fact.
func chatBannerNoticeBody(cols int) int { return cols - chatBannerNoticeGutterCells }

// chatBannerNoticeHeaderRow is the notice section's header, styled like the
// panel's own two section headers so the box reads as one screen.
func chatBannerNoticeHeaderRow(spec chatBannerSpec, body int) string {
	row := &bannerRow{}
	row.add(spec.pal.Hint, chatBannerEdge, chatBannerSpace)
	row.add(spec.pal.Banner, chatBannerPadRight(chatBannerCrop(chatBannerNoticesHeader, body), body))
	row.add(spec.pal.Hint, chatBannerSpace, chatBannerEdge)
	row.padTo(spec.pal.Hint, spec.cols)
	return row.render()
}

// chatBannerNoticeTextRow renders one notice-section content row: the box's
// edges with body cells between them, exactly so the right border lines up. An
// empty text is a blank content row (the section's own spacing).
func chatBannerNoticeTextRow(spec chatBannerSpec, text string, body int) string {
	row := &bannerRow{}
	row.add(spec.pal.Hint, chatBannerEdge, chatBannerSpace)
	row.addBounded(spec.pal.Hint, text, body)
	row.padTo(spec.pal.Hint, body+2)
	row.add(spec.pal.Hint, chatBannerSpace, chatBannerEdge)
	row.padTo(spec.pal.Hint, spec.cols)
	return row.render()
}

// chatBannerTip is one getting-started line, in PRIORITY order.
type chatBannerTip struct {
	// text is the rendered line. Its command words are the REPL's OWN
	// constants (commands.go), so a rename cannot leave the banner advertising
	// a command the REPL no longer answers — the one-reading rule the help
	// text follows too.
	text string
	// priority is the line's RANK on the panel (lower = earlier). It decides the
	// sequence the must tips are drawn in, and it is what a future capacity trade
	// would cut by; the must list is sized so that no must tip is cut today, and
	// the varying lines are the ROTATING pool's (each with a weight). The reasons
	// behind each value are on the constants.
	priority chatBannerTipPriority
	// applies reports whether this tip is about THIS run. A tip that does not
	// apply is not a low-priority tip: it is not a tip at all, and the
	// distinction matters because it decides whether the panel shows five lines
	// or four — see chatBannerTipNewline below for the case that made it a field
	// (the cooked path, where the newline key is not the operator's to know
	// through this screen).
	applies func(spec chatBannerSpec) bool
}

// chatBannerAlways marks a tip that applies to every run: the ones that are
// about the REPL itself rather than about the terminal it is running in.
func chatBannerAlways(chatBannerSpec) bool { return true }

// chatBannerToolsOnly marks a tip that only makes sense on a tool-enabled run.
func chatBannerToolsOnly(spec chatBannerSpec) bool { return spec.toolsOn }

// chatBannerTipNewline is the newline-shortcut tip for THIS terminal, and it is
// a function rather than a table entry because its WORDING is a three-way
// decision. The panel says which key inserts a newline, and the honest answer
// depends on what the keyboard negotiation measured (lineedit.KeyboardProtocol —
// keyprotocol.go asks `CSI ? u` at raw-mode entry and waits a bounded window):
//
//   - Supported — the terminal answered, so it reports Shift+Enter as its own
//     event and that is the key to name;
//   - Unsupported — asked, silent. Shift+Enter reaches the editor as the same
//     byte a bare Enter sends, so the panel names the key that does work
//     (Alt+Enter) AND says why, rather than leaving the operator to guess;
//   - Unavailable — there was no terminal to ask (a cooked/redirected run).
//     Nothing was measured and the newline key is not this screen's business:
//     no tip is printed.
//
// The middle case is the operator's own terminal: xterm implements no kitty
// keyboard protocol (measured 2026-10-02; devdoc/kitty-keyboard-protocol.md).
func chatBannerTipNewline(spec chatBannerSpec) (chatBannerTip, bool) {
	tip := chatBannerTip{priority: chatBannerTipPriorityNewline}
	switch spec.keyboardProtocol {
	case lineedit.KeyboardProtocolSupported:
		tip.text = chatBannerTipNewlineKitty
	case lineedit.KeyboardProtocolUnsupported:
		tip.text = chatBannerTipNewlineFallback
	default:
		// Not probed: nothing measured, nothing to say (Rule 15 — advice about a
		// negotiation that never happened would be a claim this screen cannot
		// support).
		return chatBannerTip{}, false
	}
	return tip, true
}

// The MUST tips: the lines a first-time user cannot guess, drawn on EVERY run
// and never rotated away. The FIRST is the one thing a new user needs (how to see
// everything else), and the TWO KEY SENTENCES come next — in the SAME ORDER the
// historic one-liner states them (chatBannerLine: "Ctrl+C …, Ctrl+D …"). The order
// is not cosmetic: this surface's e2e reads the pty stream and stops at the exit
// sentence's marker (testsuite/chat_pty_interrupt_test.go), so a panel that
// printed the exit line FIRST would hand that reader a block with the interrupt
// sentence still unpainted — the feature's own statement would never be read, on
// a screen that shows it perfectly.
//
// THE INPUT-KEYS LINE IS A MUST TIP TOO: the operator went looking for it on a
// real terminal (2026-10-02) and neither half is guessable from the screen. Its
// WORDING, though, is the keyboard negotiation's three-state verdict, so it is
// built by chatBannerTipNewline rather than written here — see that function for
// the per-state sentences and devdoc/kitty-keyboard-protocol.md for the
// measurement behind them.
//
// chatBannerTipPriority ORDERS this list, and because every must tip is drawn
// the ordering is a statement about what the screen LEADS with rather than a
// capacity trade. The trade belongs to the ROTATING pool below, where each line
// carries its own weight.
func chatBannerMustTips(spec chatBannerSpec) []chatBannerTip {
	all := []chatBannerTip{
		{text: "/" + helpCommandName + " lists every command", priority: chatBannerTipPriorityDiscover},
		{text: chatBannerCtrlCTail, priority: chatBannerTipPriorityInterrupt},
		{text: chatBannerExit, priority: chatBannerTipPriorityExit},
		{text: "/" + toolsCommandName + " flips tools on and off", priority: chatBannerTipPriorityTools, applies: chatBannerToolsOnly},
	}
	// The newline line is the one must tip that can be ABSENT — on a run with no
	// terminal to ask, nothing was measured and there is nothing to say (the
	// cooked path). It is not "ranked last" there: it is not a tip at all, and
	// the row goes to the rotation.
	if newline, ok := chatBannerTipNewline(spec); ok {
		all = append(all, newline)
	}
	// Ordered by priority rather than written in the panel's order, so the
	// sequence the reader sees is the one the constants argue for.
	sort.SliceStable(all, func(i, j int) bool { return all[i].priority < all[j].priority })
	return all
}

// The rotating pool: the tips that come and go. Each carries a WEIGHT, so the
// panel's own variety has one knob per tip instead of a hard-coded rotation.
// Every line here is checked against the code it describes — a tip is a claim
// about the surface, and a wrong one is worse than a missing one (the operator's
// own rule: 没有人去看手册, so the screen IS the manual).
var chatBannerRotatingTips = []chatBannerRotatingTip{
	// The input keys' OTHER half. readline's Ctrl+A/E are what key.go maps
	// (Ctrl+A → KeyHome, Ctrl+E → KeyEnd), and the completion menu is driven by
	// the shape the line starts with: "/" is the command menu
	// (chatCompletionCandidates answers from the command table), and Tab/Esc are
	// the accept/close keys the completion state handles.
	{text: "Ctrl+A / Ctrl+E jump to line start / end", weight: chatBannerTipWeightHigh},
	{text: "type / for the command menu (Tab completes)", weight: chatBannerTipWeightNormal},
	{text: "Up / Down recall earlier messages", weight: chatBannerTipWeightNormal},
	// A pasted block keeps its own newlines: the editor's bracketed-paste framing
	// (sgr.BracketedPasteMarkerSeq) marks the paste as CONTENT, so it stays ONE
	// message instead of submitting on its first line.
	{text: "a pasted block stays ONE message", weight: chatBannerTipWeightNormal},
	// The code-block fact, from the sibling fix (commit 7e92b038): a fenced
	// block's rows are drawn with NO turn gutter, so selecting a command out of
	// the block copies the command itself — the gutter glyph used to ride along
	// and the paste died on `▌`.
	{text: "copy a command out of a code block — no gutter", weight: chatBannerTipWeightLow},
	// The path from an ELIDED identity to the whole one, ON THIS SCREEN's own
	// terms: it is the tip the elision fallback leans on (see
	// chatBannerIdentityValue), so it is a rotating line rather than a must one
	// only because the panel's must list is full of things a first-time user
	// cannot guess — and a reader who needs the id has the label right there.
	{text: "/" + logCommandName + " " + chatBannerLogListSub + " lists every session id in full", weight: chatBannerTipWeightLow},
	// /new clears the conversation (commands.go: webClearCommandName = "new"),
	// and /quit is the exit this panel already names (chatBannerExit).
	{text: "/" + webClearCommandName + " starts a fresh conversation", weight: chatBannerTipWeightLow},
	// The queue tip, KEPT from the pre-restructure panel rather than dropped with
	// it: queueing is real knowledge about this REPL (chatQueueCommandName parks
	// the next turn), and a restructure that silently deletes a fact is a
	// regression wearing a new layout. Its weight is LOW because it is the most
	// advanced line in the pool — queueing presupposes the turn model the must
	// tips teach.
	{text: "/" + chatQueueCommandName + " <prompt> parks the next turn", weight: chatBannerTipWeightLow},
}

// chatBannerTips renders the getting-started list: the MUST tips in priority
// order, then as many rotating tips as the cap leaves room for, drawn by THIS
// run's rotation (chatBannerRotateTips). A tip the panel cannot hold is cropped,
// never dropped for another's sake — a must tip is refused by nothing except the
// panel's own width or by not APPLYING to this run (the newline line on a cooked
// run, /tools on a tool-less one), and a must tip that does not apply frees its
// row to the rotation instead of shrinking the panel.
func chatBannerTips(spec chatBannerSpec, cols int) []string {
	out := make([]string, 0, chatBannerTipsMax)
	for _, tip := range chatBannerMustTips(spec) {
		if tip.applies != nil && !tip.applies(spec) {
			continue
		}
		out = append(out, chatBannerCrop(tip.text, cols))
		if len(out) >= chatBannerTipsMax {
			return out
		}
	}
	for _, tip := range chatBannerRotateTips(spec, chatBannerTipsMax-len(out)) {
		out = append(out, chatBannerCrop(tip, cols))
	}
	return out
}

// chatBannerRotateTips is the rotation: WHICH of the pool's tips this run draws,
// with the number of slots the must tips left free. It is a pure function of
// (pool, weights, slots, seed) — no clock, no map order, no global state — so the
// same seed always yields the same list and a test can pin both ends.
//
// WHY weighted sub-sampling rather than a stored rotation. A rotating counter
// would have to live SOMEWHERE (a file beside the session, an env var), and a
// persistent artifact that makes a TIP vary is precisely what Rule 35 forbids:
// the tips are the surface's own text with no state behind them, so the variety
// is derived from whatever entropy the run already has (chatBannerTipSeed) and
// nothing is written down. The consequence, stated rather than hidden: two runs
// inside the SAME second can draw the same list (the seed is not a random
// generator), and that is a fair price for a tip mechanism that costs no state.
//
// The weights are read as an EXPONENTIAL-KEY sampler (Efraimidis–Spirakis): each
// candidate gets the key key(u, w) = -ln(u)/w and the top `slots` keys win, where
// u is drawn UNIFORMLY from (0,1] off the splitmix64 stream. A higher weight
// therefore scales that candidate's key DOWN, i.e. makes it MORE likely to be
// drawn — "high weight = stable/frequent" — while leaving every candidate a
// non-zero chance, which is exactly "rotate, but keep the important ones
// common".
func chatBannerRotateTips(spec chatBannerSpec, slots int) []string {
	if slots <= 0 {
		return nil
	}
	type keyed struct {
		text  string
		score float64
	}
	cands := make([]keyed, 0, len(chatBannerRotatingTips))
	state := uint64(spec.tipSeed)
	for _, tip := range chatBannerRotatingTips {
		u := chatBannerNextUnit(&state)
		weight := float64(tip.weight)
		if weight < 1 {
			weight = 1
		}
		cands = append(cands, keyed{text: tip.text, score: -math.Log(u) / weight})
	}
	// Ascending key, then take the HEAD: a SMALLER key wins, which is what makes a
	// larger weight a frequency (the key is -ln(u)/w, so a bigger w scales the key
	// down). Sorting only by key would let two equal keys (impossible here unless a
	// caller pins the seed to a degenerate stream, but not worth the risk) settle
	// on the map's order, so SliceStable keeps the pool's own index as the tie-break.
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].score < cands[j].score })
	if slots > len(cands) {
		slots = len(cands)
	}
	out := make([]string, 0, slots)
	for _, c := range cands[:slots] {
		out = append(out, c.text)
	}
	// The winners are in KEY order, which is a random order a reader would read as
	// the panel's own priority — and the panel HAS a priority (the must tips are
	// ordered on purpose). The rotating lines are therefore put in the pool's
	// declaration order, so the extra lines read as a list rather than as noise.
	rank := make(map[string]int, len(chatBannerRotatingTips))
	for i, tip := range chatBannerRotatingTips {
		rank[tip.text] = i
	}
	sort.Slice(out, func(i, j int) bool { return rank[out[i]] < rank[out[j]] })
	return out
}

// chatBannerNextUnit draws the next u from (0,1] off a splitmix64 stream. 0 is
// excluded because the sampler takes ln(u): a zero would make the key infinite
// and hand that tip every slot it is compared against.
func chatBannerNextUnit(state *uint64) float64 {
	*state += chatBannerSeedMixA
	z := *state
	z = (z ^ (z >> chatBannerSeedShift)) * chatBannerSeedMixB
	z = (z ^ (z >> chatBannerSeedShift)) * chatBannerSeedMixC
	z ^= z >> chatBannerSeedShift
	return float64((z>>11)+1) / float64(uint64(1)<<53)
}

// chatBannerTipSeed is THIS run's rotation seed. It is the session's own
// identity and the startup instant, mixed into one 64-bit value: the seed
// differs between runs (the session id is fresh per run) and is IDENTICAL for
// two calls within a run (so the box is composed once, not drawn twice differently
// — the panel's width measurement and its rendering both call chatBannerTips).
//
// It is deliberately NOT a random number: nothing here needs unpredictability,
// only variation, and a derived seed keeps the surface reproducible from the two
// facts the panel already prints (the session and the time its screen was built).
func chatBannerTipSeed(now time.Time, sessionID string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(sessionID))                          //stylegate:ignore a hash never fails; the write cannot report anything a caller could act on
	_, _ = h.Write([]byte(now.UTC().Format(time.RFC3339Nano))) //stylegate:ignore a hash never fails
	return h.Sum64()
}

// chatBannerToolsHint is the "(tools ON)" suffix the historic one-liner carries
// on a tool-enabled run. It is spelled once here so the fallback line and the
// panel's own tools fact cannot drift.
const chatBannerToolsHint = "  (tools ON)"

// The input-keys tip's fragments, named so the sentence and the decoder cannot
// drift apart: each half is a claim about internal/lineedit's own key map.
const (
	// chatBannerNewlineKey is the key that inserts a newline WITHOUT submitting
	// on EVERY terminal gopi can run on: the decoder turns ESC + CR/LF into
	// KeyNewline (lineedit/keys.go decodeEscape), which is the same key the
	// encoder-free path depends on. It is the operator's own measured fact
	// (2026-10-02: pressed on a real terminal, the newline went in).
	chatBannerNewlineKey = "Alt+Enter"
	// chatBannerEnterSendsFact is the other half of the sentence, and it names
	// only what the DECODER does: a bare CR is Enter (keys.go), so the keystroke
	// the user already knows is the one that sends.
	//
	// WHY THIS AND NOT Shift+Enter. A terminal that never answers the kitty
	// keyboard query hands Shift+Enter as a bare CR, so on this operator's
	// terminal the two are the same byte — but the surface cannot yet say which
	// of the three cases THIS terminal is in, and asserting one of them on the
	// screen would be a guess printed as a fact. The tri-state wording is the
	// sibling task's own deliverable (task 01a0fadf: "应反映本终端实测的三态而非写死
	// 文案"), so this line stays on the two facts that hold on every terminal
	// rather than pre-empting a sentence that task owns.
	chatBannerEnterSendsFact = "Enter sends"
)

// chatBannerRotatingTip is the rotating pool's entry: the line plus how often it
// should come up (chatBannerTipWeight*).
type chatBannerRotatingTip struct {
	text   string
	weight int
}

// chatComposeStartupScreen renders the startup screen for THIS run and returns
// the block to commit into the band, or "" when the terminal cannot carry it
// (the caller then prints the historic one-liner).
//
// It is the ONE place the banner's inputs are gathered, so every fact it draws
// is read from the same source the rest of the chrome reads it from:
//   - the version, from buildVersion (the binary's own accessor);
//   - the model, from the agent — the SAME field the status row's first column
//     carries (newChatStatusSource(ag.Model, …)), so the two cannot disagree;
//   - the node, from chatNodeIdentity (internal/config's declared role);
//   - the directory, from the session's own ground (sess.ground), falling back
//     to the process cwd when the ground is degraded — the session writes where
//     it stands, and that is the honest answer.
func chatComposeStartupScreen(ag *agent.Agent, sess *chatSession, input *chatInput, pal lineedit.Palette) string {
	spec, ok := chatBannerSpecFor(ag, sess, input, pal)
	if !ok {
		return ""
	}
	rows := chatBannerRows(spec)
	if len(rows) == 0 {
		return ""
	}
	return strings.Join(rows, "\n")
}

// chatComposeStartupScreenWithNotices composes the screen AND the section this
// run's own start-up notices are moved into, reporting the notices the screen
// could not hold so the caller can print them through the historic stream (Rule
// 15 — a notice is moved, never lost).
//
// It exists beside chatComposeStartupScreen rather than replacing it because the
// two answer different callers: the plain composition is what a run with no
// notices (and every in-package test) asks for, and this one is the REPL's own
// path, where the notices were gathered before the screen was drawn.
func chatComposeStartupScreenWithNotices(ag *agent.Agent, sess *chatSession, input *chatInput, pal lineedit.Palette, notices chatPanelNotices) (string, []bannerNotice) {
	spec, ok := chatBannerSpecFor(ag, sess, input, pal)
	if !ok {
		return "", notices.bannerNotices()
	}
	spec.notices = notices.bannerNotices()
	spec.surface = notices.surface
	rows, remaining := chatBannerScreen(spec)
	if len(rows) == 0 {
		return "", notices.bannerNotices()
	}
	return strings.Join(rows, "\n"), remaining
}

// chatBannerSpecFor gathers everything the startup screen draws from, and reports
// false when the band cannot carry it at all (no reserved layout, or no geometry
// to draw at).
//
// It is the ONE place the banner's inputs are gathered, so every fact it draws
// is read from the same source the rest of the chrome reads it from:
//   - the version, from buildVersion (the binary's own accessor);
//   - the model, from the agent — the SAME field the status row's first column
//     carries (newChatStatusSource(ag.Model, …)), so the two cannot disagree;
//   - the node, from chatNodeIdentity (internal/config's declared role);
//   - the directory, from the session's own ground (sess.ground), falling back
//     to the process cwd when the ground is degraded — the session writes where
//     it stands, and that is the honest answer.
func chatBannerSpecFor(ag *agent.Agent, sess *chatSession, input *chatInput, pal lineedit.Palette) (chatBannerSpec, bool) {
	if input == nil || input.Answer() == nil {
		// No reserved layout: the band is not there to carry a screen, so the
		// caller keeps the historic one-liner (chat_banner.go's header: the box
		// is never printed down the flow path).
		return chatBannerSpec{}, false
	}
	// The geometry comes from the band ITSELF, not from a second probe of the
	// terminal: the block is committed into the band, so the band's own width is
	// the width the banner must fit — a box drawn at any other width would be
	// re-cut by the band's own row loop and the split would land inside the
	// border (Rule 14: one wrap policy, one definition). The rows come from the
	// same place, and a block LONGER than the band would be pushed into
	// scrollback by its own carrier — the screen would never be seen (see
	// chatBannerRows' row budget).
	cols, rows := input.bandGeometry()
	if cols <= 0 || rows <= 0 {
		return chatBannerSpec{}, false
	}
	maxRows := rows - chatBannerStatusRows - chatBannerEditorRows
	if maxRows < 1 {
		maxRows = 1
	}
	// The keyboard negotiation's verdict, read from the SAME accessor the
	// startup notice reports through (chat_keyboard_protocol.go), so the panel's
	// advice and the notice can never contradict each other about this terminal.
	keyboardProtocol, _, _ := input.keyboardProtocol()
	spec := chatBannerSpec{
		cols:    cols,
		version: buildVersion(),
		model:   ag.Model,
		node:    chatBannerNodeIdentity(),
		dir:     chatStartupDir(sess),
		// THIS session's own identity, read from the SAME accessor the session
		// log, `gopi session log` and `gopi chat --resume` name it by (agent.SessionLogger.
		// SessionID) — so the id on the screen is the operand, not a rendering of
		// one. It is empty only when there is no session record (the cooked path
		// still has one; a logger-less caller does not), and the fact is then
		// omitted rather than faked.
		sessionID: chatStartupSessionID(ag),
		toolsOn:   sess.toolsEnabled,
		// The keyboard negotiation's verdict, read from the SAME accessor the
		// startup notice reports through (chat_keyboard_protocol.go), so the
		// panel's advice and the notice can never contradict each other about
		// this terminal.
		keyboardProtocol: keyboardProtocol,
		maxRows:          maxRows,
		pal:              pal,
		// The rotation's seed is fixed ONCE, here, from this run's own identity:
		// every later call in this composition (the panel's width measurement,
		// the rows themselves) reads the same value off the same spec.
		tipSeed: chatBannerTipSeed(time.Now(), sess.sessionDir),
	}
	return spec, true
}

// chatBannerNodeIdentity renders this node for the startup screen:
// internal/config's own node line with its "node: " prefix removed, so the
// banner and the `[gopi] node: …` notice that printLogPathsQuiet emits state ONE
// fact in ONE reading (the panel's label already says "node"). It reads the SAME
// resolver, so the two surfaces cannot disagree about which node this is.
func chatBannerNodeIdentity() string {
	line := config.NodeIdentityLine()
	line = strings.TrimPrefix(line, chatBannerNodeLinePrefix)
	if line == "" {
		return ""
	}
	return line
}

// chatBannerNodeLinePrefix is the prefix internal/config's node line carries and
// the banner's own label replaces. Spelled once so the trim and the label cannot
// drift apart.
const chatBannerNodeLinePrefix = "node: "

// chatStartupDir is the directory the startup screen names: the session's own
// ground (the thing the [gopi] ground notice also names), or the process cwd
// when no ground was resolved — the session does write there, and the banner
// states where it writes rather than where it would have liked to.
func chatStartupDir(sess *chatSession) string {
	if sess != nil && sess.ground.Dir != "" {
		return sess.ground.Dir
	}
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return ""
}

// chatStartupSessionID is the identity the startup screen states: THIS run's
// session uuid, read from the logger the whole run already writes through
// (agent.SessionLogger.SessionID — the same value the session_start event on
// disk carries and `gopi session log <id>` resolves). One reader, so the id printed on
// the screen and the id the log answers by cannot disagree; a run with no logger
// returns "" and the panel omits the fact (never a fabricated or blank id).
func chatStartupSessionID(ag *agent.Agent) string {
	if ag == nil || ag.SessionLog == nil {
		return ""
	}
	return ag.SessionLog.SessionID()
}

// chatBannerToolsValue renders the panel's tools fact.
func chatBannerToolsValue(on bool) string {
	if on {
		return chatBannerToolsOn
	}
	return chatBannerToolsOff
}

// chatBannerCrop cuts text to at most width cells, marking a real cut with the
// banner's own marker. It is a CROP, not a wrap: see chatBannerLeftColumn. It
// rules on the PLAIN text (the caller styles the result), which is what keeps
// the width arithmetic honest.
func chatBannerCrop(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if lineedit.DisplayWidth(text) <= width {
		return text
	}
	head := lineedit.TruncateToCols(text, width-lineedit.DisplayWidth(chatBannerCutMarker))
	return head + chatBannerCutMarker
}

// chatBannerPadRight pads a label with spaces to exactly cells, so the panel's
// values align. A label already at or past the field is returned unchanged (the
// value then starts one cell later — never overlapped).
func chatBannerPadRight(label string, cells int) string {
	w := lineedit.DisplayWidth(label)
	if w >= cells {
		return label
	}
	return label + strings.Repeat(chatBannerSpace, cells-w)
}

// chatBannerShortenDir renders a directory for the banner: this node's home
// directory becomes "~", and an over-long path keeps its LAST components (the
// ones that identify it) behind the cut marker. It is display-only — nothing
// reads the path back — and an empty input returns "" so the caller OMITS the
// fact rather than printing a blank one.
func chatBannerShortenDir(dir string, cells int) string {
	if dir == "" {
		return ""
	}
	short := dir
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(dir, home) {
		short = "~" + strings.TrimPrefix(dir, home)
	}
	if lineedit.DisplayWidth(short) <= cells {
		return short
	}
	keep := cells - lineedit.DisplayWidth(chatBannerCutMarker)
	if keep <= 0 {
		return chatBannerCutMarker
	}
	runes := []rune(short)
	for i := range runes {
		tail := string(runes[i:])
		if lineedit.DisplayWidth(tail) <= keep {
			return chatBannerCutMarker + tail
		}
	}
	return chatBannerCutMarker
}
