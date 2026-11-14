package replayplay

// material.go — WHERE A REPLAY'S PICTURE COMES FROM, and what one FRAME of it is.
//
// A replay plays a session back as a rendered picture, so the first question is
// what the material is. The session's own directory answers it, and this file
// reads the two artifacts that can carry a frame sequence:
//
//  1. the CHAT SCREEN DUMP (uiclient.ChatDumpFileName + ".json") — the surface's
//     own record of what it drew: a cell-exact `grid` (one rune per CELL, a wide
//     rune followed by its continuation cell), per-cell `attr_runs` carrying the
//     TRUECOLOUR the surface was actually painted with, and the `scrollback` the
//     screen pushed off its first row. It is the PREFERRED material: it is what
//     the operator saw.
//
//  2. the SESSION JSONL (session.jsonl) — the event timeline. It is the FALLBACK:
//     it carries no cells and no colour, only what the conversation said, so a
//     frame built from it is our own rendering of the events, never a claim about
//     the screen. It exists so a session that recorded no dump (the surface was
//     never armed) still replays something true.
//
// THE DUMP HOLDS ONE FRAME, AND THAT IS WHY FRAMES ARE BUILT HERE. The dump sink
// installs its state with an atomic rename, so the file carries the LAST painted
// screen and nothing else — a single snapshot cannot step or step back. The
// frame sequence therefore comes from the parts the dump DOES carry: the
// scrollback pages (the history the screen pushed away, replayed a screenful at
// a time) followed by the painted grid (what was on screen when the dump was
// written). Both are the dump's own lines: nothing here is invented to make the
// playback look busier than the recording is.
//
// NOTHING IS FAKED. Neither artifact usable → the caller is handed an error that
// names both paths and what is missing; there is no empty playback and no
// synthetic material (Rule 15).

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"

	"github.com/nickhuang99/gopi/internal/gopilog"
	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/sessionevent"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
	"github.com/nickhuang99/gopi/internal/sgr"
	"github.com/nickhuang99/gopi/internal/terminalgeom"
	"github.com/nickhuang99/gopi/internal/uiclient"
)

// The artifact names a session directory is read for. They are the OWNERS'
// constants, referenced rather than re-typed (Rules 1b/13): the dump's json twin
// is the same file the sink writes beside the text dump, and the jsonl name is
// what the session writer produces.
const (
	// DumpJSONName is the structured dump the frame material is read from.
	DumpJSONName = uiclient.ChatDumpFileName + dumpJSONSuffix
	// SessionJSONLName is the session's event timeline — the BARE layout's own
	// name. It is internal/sessionlayout's RecordName referenced, not re-typed
	// (Rules 1b/13): that package is the tree's ONE resolver of "which file is
	// this session's log", so a second spelling here would be a mirror that
	// drifts the moment a layout is added.
	SessionJSONLName = sessionlayout.RecordName
	// dumpJSONSuffix is the twin's suffix (internal/uiclient's own spelling).
	dumpJSONSuffix = ".json"
)

// The two material SOURCES, as words a report and a status line print. They say
// which artifact the picture was built from, because "we rendered your screen"
// and "we rendered your event log" are different claims.
const (
	SourceScreenDump = "chat-screen.dump.json"
	SourceSessionLog = "session.jsonl"
	// SourceBoth is a session that had BOTH artifacts: the timeline's steps then
	// the recording's own screen (see LoadSession).
	SourceBoth = SourceSessionLog + "+" + SourceScreenDump
)

// FrameKind says what one frame IS — the dump's scrollback replayed a screenful
// at a time, the painted grid, or one event from the timeline. The playback's
// status line names it, so a reader can tell "this is history" from "this is the
// screen that was live".
type FrameKind string

const (
	// FrameScrollback is a screenful of the dump's scrollback history.
	FrameScrollback FrameKind = "scrollback"
	// FrameScreen is the dump's painted grid — the last screen the surface drew.
	FrameScreen FrameKind = "screen"
	// FrameEvent is material rebuilt from the session log (the opening-request
	// entry, which is an event of the session and not a model round).
	FrameEvent FrameKind = "event"
	// FrameRound is one MODEL round: the session's own unit, and what the
	// playback steps through by default.
	FrameRound FrameKind = "round"
)

// FrameRow is ONE row of a rendered round: its cells, and the ROLE the row
// plays in the round (which is what the renderer colours it by).
type FrameRow struct {
	// Cells is the row's cell text: one row of a ROUND's rendered body, or (for
	// the dump's own material) one rune per CELL exactly as the dump's Grid
	// stores it — a wide (CJK) rune followed by its continuation cell. It is
	// NOT trimmed: the row's width is what makes the column indices of Runs
	// meaningful.
	Cells string
	// Runs is the dump's own attribute runs for this row (nil for material that
	// recorded none — scrollback and rounds). A run's Col is a CELL column, so
	// it lines up with Cells by construction.
	Runs []lineedit.DumpAttrRun
	// Role says WHAT part of a round this row is (the prompt, reasoning, the
	// round's prose, a tool call, a tool result, a harness injection). It is how
	// the renderer picks a colour, and the empty value means "recorded material
	// with no role of its own" — the dump's screen rows, which are drawn in the
	// colour the dump recorded for them.
	Role RowRole
}

// RowRole is the part one rendered row plays inside its round. The renderer
// maps a role to a palette colour (render.go), and the round's rows are emitted
// with their roles, so a reader of a painted frame can tell the prompt from the
// reasoning from a tool result (Rule 1b: roles are named values, never letters
// typed at a switch).
type RowRole string

const (
	// RoleBody is the round's own prose (and recorded material with no role).
	RoleBody RowRole = ""
	// RolePrompt is the request the round answered.
	RolePrompt RowRole = "prompt"
	// RoleThinking is the round's reasoning blocks.
	RoleThinking RowRole = "thinking"
	// RoleText is the prose the model produced.
	RoleText RowRole = "text"
	// RoleToolCall is one tool invocation the round made.
	RoleToolCall RowRole = "tool-call"
	// RoleToolResult is the result that came back for one.
	RoleToolResult RowRole = "tool-result"
	// RoleHarness is a harness-injected user row (a clock tick, a checkpoint):
	// the framework's own traffic inside the round, drawn small and dim so it
	// never reads as something the operator said.
	RoleHarness RowRole = "harness"
	// RoleError is a row that reports a failure (an empty tool result is one:
	// "the tool answered nothing" is a fact a viewer must see).
	RoleError RowRole = "error"
)

// ToolCallView is one tool invocation inside a round: what was called, with
// what, and what came back. It is the round's own type rather than
// internal/gopilog's ToolCall, because a replay renders text and must not hold
// a pointer into the reader's model.
type ToolCallView struct {
	Name     string
	CallID   string
	InputRaw string
	Result   string
	// ExitCode is the bash tool's recorded exit status (nil when the tool is
	// not bash, or the result recorded none).
	ExitCode *int
}

// RoundKind says what a round IS: one model round of the session, or the
// OPENING PROMPT rows the session started with (the operator's briefing and the
// task summary), which no model round answered.
type RoundKind string

const (
	// RoundModel is one model round: what gopilog counts as a session round.
	RoundModel RoundKind = "round"
	// RoundPrompt is the session's opening request rows.
	RoundPrompt RoundKind = "prompt"
	// RoundNoContent is a CANONICAL round that carried no content: it was a
	// request/response exchange (so it is a round and it has an ordinal — the
	// numbering must stay gap-free, or `--round N` would name a different
	// exchange than `gopi session log <id> N`), and the reader holds no blocks
	// for it. It is a step of its own that SAYS SO, because the alternative —
	// skipping it — makes the replay's "round 3" a different exchange from the
	// log's (Rule 15: absence is stated, never silently renumbered).
	RoundNoContent RoundKind = "no-content"
	// RoundTurn is ONE TURN assembled from every round it contains — the unit
	// the playback STEPS through (the operator's ruling, 2026-10-22). It is a
	// Round value because a step renders exactly what a round renders (prompt,
	// reasoning, prose, calls); what changes is the SPAN it was gathered over
	// and the label: "turn 3/8  (5 request(s), 12 tool call(s))".
	RoundTurn RoundKind = "turn"
)

// Round is ONE step of the playback, and it is the session's own unit: one
// model round (internal/gopilog's Iteration — a `request` event that carried
// content), with everything the round contains gathered into one place.
//
// WHY ROUNDS AND NOT FRAMES. The playback used to advance one JSONL ROW at a
// time, which made "a step" mean "a line of the log": a turn of the conversation
// was split across several steps, thinking and tool calls were dropped
// entirely, and a harness clock tick became a step of its own (measured: 68
// steps for a session whose own round count was 50). The unit a viewer wants to
// step through is the ROUND — the same unit `gopi session log <id> --rounds`
// counts — so a step and a round are now the same thing, and one step shows the
// whole round (Rule 14: the grouping has ONE definition, internal/gopilog; this
// file reads it and does not re-derive it).
type Round struct {
	// Index is the round's number within the session — the number
	// `gopi session log <id> <N>` and `--round N` use, so the three surfaces
	// cannot disagree about which round "3" is. It is 1-1 with the position in
	// Material.Rounds for every session whose log has rounds.
	Index int
	// Kind says whether this is a model round or the session's opening request
	// (a session whose log recorded no round at all).
	Kind RoundKind
	// Label names the round in the status line.
	Label string
	// Opening is the session's original request, when the log recorded one that
	// round 1 does not already carry as its prompt (a session whose very first
	// user row the reader did not hand to the first round).
	Opening string
	// Prompt is the request the round answered ("" when none was recorded).
	Prompt string
	// HarnessPrompts are the harness-injected user rows inside this round.
	HarnessPrompts []string
	// Text is the prose the round produced, in the order it was produced.
	Text []string
	// Thinking is the round's FULL reasoning text — never truncated: the point
	// of a round-sized step is that the step can carry it.
	Thinking []string
	// Calls are the round's tool invocations, with their results.
	Calls []ToolCallView
	// Rows is the round laid out as rows, each tagged with its ROLE — baked at
	// load time so the wrap the tests assert on and the wrap the player paints
	// are the same bytes (the renderer only paints; it never re-lays out).
	Rows []FrameRow
	// Segments holds, on a TURN only, the PER-ROUND material the turn is made
	// of — in the order the rounds ran, each one a Round of its own. A turn is
	// many rounds, and this is the only structure that preserves WHICH round a
	// tool call, a result or a harness tick came from; flattening them into one
	// pile is what put a turn's eight tool calls behind 170 rows of other
	// rounds' prose (turnbody.go states the measurement).
	//
	// It is NOT a new unit and NOT a new numbering: a segment's Index is the
	// ROUND ordinal `--round N` resolves, and nothing counts segments as
	// anything else. A round row (Kind != RoundTurn) leaves it empty.
	Segments []Round
	// RoundTotal is the session's ROUND count, carried on a TURN so a segment
	// header can spell "round 143/152" without a second source for the number.
	RoundTotal int
	// FirstRound / LastRound are set on a TURN only: the interval of SESSION
	// ROUNDS the turn covered, in the ROUND ADDRESS SPACE (`--round N`). They
	// are 0 on a round row (a round is one round; its own Index says which).
	//
	// THE TWO UNITS MUST NAME EACH OTHER. The listing that printed a step's
	// ordinal and its "round 152/152" label side by side let a reader address a
	// TURN by a ROUND number, which returns one round of it; these fields are
	// what the listing and the turn's own header print so the extent of a turn is
	// visible where the two numbers meet.
	FirstRound, LastRound int
}

// Material is everything a playback needs: the session it came from, the
// geometry the rounds are laid out for, and the rounds themselves.
type Material struct {
	// SessionID is the session the material belongs to (its directory name).
	SessionID string
	// Dir is the session directory the material was read from.
	Dir string
	// Source names the artifact the rounds were built from (SourceScreenDump
	// or SourceSessionLog).
	Source string
	// Cols / Rows are the geometry the rounds are drawn for. The dump's own
	// cols/rows when it has them (that is the geometry the screen WAS), else the
	// caller's terminal and the fallback is announced by the caller.
	Cols int
	Rows int
	// GeometrySource says where Cols/Rows came from ("dump", "terminal",
	// "fallback") — a replay that resized the recording must say so.
	GeometrySource string
	// Rounds is THE ROUND SEQUENCE, oldest first: one entry per session round
	// (plus the opening prompt rows, when the log recorded any). It is what the
	// per-round ADDRESS SPACE resolves against — `--round N`, `gopi session log
	// <id> N` and the raw-log pairing — and it is NOT what the playback walks
	// through (see Turns). A round is INTERNAL/DEBUG in the replay's user
	// surface: the transport's unit, never a count of the operator's activity.
	Rounds []Round
	// Turns is THE PLAYBACK SEQUENCE, oldest first: one entry per TURN — every
	// round and tool call the agent ran between one operator row and the next,
	// assembled into ONE turn (the operator's ruling, 2026-10-22;
	// sessionevent.CounterUnitTurn). A turn's label is "turn 3/8  (5
	// request(s), 12 tool call(s))": the turn outside, the inner quantities as
	// its annotation — the shape `gopi chat --replay` already printed.
	//
	// IT IS A SECOND FIELD, NOT A REWRITE OF Rounds. A round is still an
	// addressable exchange (a byte view of ROUND 7 must keep meaning exchange
	// 7); a turn is a display and transport unit. One field cannot be both, and
	// mixing the two into one sequence is what made the old "frames" unreadable.
	Turns []Round
	// DumpFrames is the screen dump's OWN material — the scrollback replayed a
	// screenful at a time, then the painted grid — kept SEPARATE from Rounds
	// because it is a different unit: a screen is not a round, it has no prompt,
	// no thinking and no tool calls, and mixing the two into one sequence is
	// what made the old playback's "frames" unreadable. It is empty for a
	// session whose surface recorded no dump.
	DumpFrames []Frame
	// SourceLabel is the header's `source=` WORD — the claim a reader makes when
	// they read one line of this playback. It is a SEPARATE field from Source
	// because the two answer different questions: Source names the ARTIFACTS
	// (`session.jsonl+chat-screen.dump.json`), while SourceLabel names the
	// FIDELITY (`sgr-stream(verbatim)` vs `dump(interpreted, throttled)`), and the
	// fidelity is the fact that decides how much a reader may trust the picture.
	// Rule 15: these two spellings must never look alike.
	SourceLabel string
	// Stream is the verbatim leg's own reading, and nil when the session recorded
	// no screen stream (an ordinary case: a worker run paints no screen). A
	// non-nil Stream means DumpFrames came from the RENDERER'S OWN BYTES rather
	// than from the dump's reduced grid.
	Stream *ScreenStream
	// StreamNote says WHY the verbatim leg was not used when the session had one
	// to read — an unreadable artifact, or a caller's `--from dump`. It is stated
	// on the header line rather than swallowed, because "we fell back" and "there
	// was nothing to fall back from" are different findings (Rule 15).
	StreamNote string
	// Dims are the canvas dimensions each round is laid out at (the round body
	// wraps at Dims.Cols and a round is padded to Dims.Rows). They are recorded
	// here because the rows are baked at load time: a renderer that re-wrapped
	// could disagree with the rows the tests assert on.
	Dims Dims
	// RoundCount is how many ROUNDS the session has — one HTTP request/response,
	// the SAME number `gopi session log <id> --rounds` prints, read from
	// internal/gopilog's SessionRounds. It is INTERNAL/DEBUG ONLY (see
	// `gopi session replay -h`): a round is the transport's unit, one turn
	// routinely takes many, and it is never offered as a count of the
	// operator's own activity. It is 0 for a dump-only material, which
	// reconstructs no rounds, and 0 says exactly that (never a fabricated
	// count); a log that recorded NONE but did record an opening request has
	// one turn and RoundCount 0.
	RoundCount int
	// TurnCount is the length of the TURN SPANS below — each one opened at a row
	// the person typed (internal/gopilog's SessionTurns) — i.e. the `human turns`
	// SUBSET, not the session's turn count (that is `chats.turn_count`, the
	// ledger's number, which the operator's ruling of 2026-10-27 makes the one
	// the bare word names everywhere). It is named TurnCount for the spans it
	// counts; a SURFACE that prints it must print the subset's word
	// (sessionevent.CounterUnitHumanTurn). IT IS THE UNIT THIS PLAYBACK WALKS
	// THROUGH (the operator's ruling, 2026-10-22): len(Turns) == TurnCount for a
	// material with any turn, and RoundCount is the annotation's detail
	// ("turn 3/8  (5 request(s), ...)"), never a second headline. 0 for a
	// material whose source recorded no turn (a dump-only replay), never a
	// fabricated count.
	TurnCount int
	// PromptTurns is how many of the Round sequence is the session's OPENING
	// REQUEST rather than a model round (0 or 1).
	PromptTurns int
}

// Dims is the canvas one round's rows were laid out at — AND whether that
// lay-out spells a tool call's input in FULL.
//
// FullToolInput rides HERE, on the lay-out, rather than on the call sites that
// print rows. The alternative (a parameter threaded through roundRows,
// turnBodyRows, Relayout and every static view) is the shape that lets a RE-LAID
// material silently forget the option: `Relayout` rebuilds every round's Rows
// from Dims alone, so an option held anywhere else would be applied once at load
// and dropped by the first `--cols`/`--rows` — a view that says `--full` and
// prints the bounded spelling. Keeping it here means "the rows were laid out
// expanded" is a property OF THE ROWS, and nothing downstream can disagree.
type Dims struct {
	Cols int
	Rows int
	// FullToolInput expands every tool call's recorded input: the bounded
	// spelling (roundToolInputMaxRune + the printed `…(+N runes)` note) is
	// replaced by the input's whole text. The DEFAULT view stays bounded — the
	// compact spelling is what fits a narrow terminal — so this is false unless
	// a caller asked for it (`gopi session replay --full`).
	FullToolInput bool
	// FullLayout lifts the ROW CEILING: roundRows' roundRowsMax and the turn
	// body's shared per-turn budget. It is the SECOND of the two truncations a
	// debug view has to answer, and it is a separate flag from FullToolInput
	// because they bound different things — one the SPELLING of a row, the other
	// how many rows exist at all.
	//
	// THE DEFECT IT EXISTS FOR (measured 2026-11-01): `gopi session replay --full`
	// expanded every tool input and still printed `(19 row(s) of this turn's
	// context are not laid out; its tool rows are)` — the one flag a debug view
	// has did not reach the ceiling, so the operator could not obtain the turn's
	// whole picture at any flag combination. The ceiling is SHARED by every round
	// of a turn (roundRowsMax), so on a 211-request turn each round was allotted
	// about 19 rows and most of the context was reported as not laid out.
	//
	// The DEFAULT stays bounded for the reason FullToolInput's default does: the
	// compact view is what fits a terminal, and it is the contract existing
	// readers rely on. A material that never asks for this is laid out byte for
	// byte as before.
	FullLayout bool
}

// rowCeiling is how many rows THIS lay-out may emit, or 0 for NO CEILING.
//
// 0 is the "no ceiling" spelling everywhere in this package (roundRows' two
// guards, appendWrappedRows and turnBodyRows' budget all read it), so a bounded
// lay-out and a debug one go through the SAME code — there is no second
// implementation that could disagree with the bounded one about what a row is.
func (d Dims) rowCeiling() int {
	if d.FullLayout {
		return 0
	}
	return roundRowsMax
}

// A TURN COUNT IS len(Turns), A ROUND COUNT IS len(Rounds)/RoundCount, AND A
// SCREEN-FRAME COUNT IS DumpFrameCount — three quantities, three names (the
// operator's ruling, 2026-10-25). The method this file used to carry
// (`StepCount`) answered all three under one name, so a caller could not tell
// which one it had been handed; it is gone, together with its fallback. What
// remains is ONE length method, and it says which unit it counted.

// DumpFrameCount is how many SCREEN FRAMES the dump contributed: the scrollback
// pages it pushed away, then the painted grid. It is a second material, never a
// turn and never a round (a screenful of cells has no prompt, no reasoning and
// no tool calls), so it is counted under its own name.
func (m Material) DumpFrameCount() int {
	return len(m.DumpFrames)
}

// PlayableUnits is how many things the player can move between: the material's
// TURNS when its source recorded them, else — and the else is stated here rather
// than hidden in a count — the dump's screen frames, because a dump-only
// recording has no turn to walk and its screens are what the player shows.
//
// It is the ONE definition of "how long is this playback", so the player, the
// clock and the static views cannot disagree about its length.
func (m Material) PlayableUnits() int {
	if n := len(m.Turns); n > 0 {
		return n
	}
	return m.DumpFrameCount()
}

// TurnAt returns the TURN at 1-based index i, and whether i named one.
//
// It reads the TURN sequence and nothing else: a material with no turn (a
// dump-only replay) has no turn at any index, and the player falls back to its
// screen frames at THAT layer (loop.go's view update), never by asking this
// method to answer for a unit it does not hold.
func (m Material) TurnAt(i int) (Round, bool) {
	if i < 1 || i > len(m.Turns) {
		return Round{}, false
	}
	return m.Turns[i-1], true
}

// ExpandForDebug returns the material RE-LAID for the operator's DEBUG view
// (`gopi session replay --full`): every tool call's recorded input spelled IN
// FULL *and* the row ceiling lifted (`Dims.FullLayout`).
//
// IT IS ONE DOOR WITH TWO QUESTIONS, and they are the two truncations this view
// has to answer. Measured 2026-11-01, the flag answered only the first: a
// `--turn N --full` run still printed `(19 row(s) of this turn's context are not
// laid out; its tool rows are)` on every round of a large turn, because the row
// ceiling is a property of the LAY-OUT that the flag never reached. A flag whose
// name promises the debug view and whose effect is half of it sends the operator
// looking for a second flag that does not exist.
//
// The two are still SEPARATE flags on Dims, because they bound different things
// (one the spelling of a row, the other how many rows exist at all) and a future
// caller may want one without the other. This function is the one place they are
// asked for TOGETHER, which is what `--full` means.
//
// It goes through Dims like every other lay-out property (the width, the height):
// Relayout rebuilds every round's and turn's Rows from Dims ALONE, so an option
// held anywhere else would be applied once at load and then silently dropped by
// the first --cols/--rows — a view that asked for full text and printed the
// bounded spelling.
//
// A material that already carries the whole debug lay-out is returned unchanged
// (the lay-out is already right), and a material that never asks keeps the compact
// spelling, its `…(+N runes)` note and its row ceiling, byte for byte.
func ExpandForDebug(m Material) Material {
	if m.Dims.FullToolInput && m.Dims.FullLayout {
		return m
	}
	m.Dims.FullToolInput = true
	m.Dims.FullLayout = true
	return m.relayoutRows()
}

// relayoutRows rebuilds every row of the material from its own Dims. It is the
// ONE place a Dims change is turned into rows, so no two callers of it can
// disagree about which collections are re-laid.
func (m Material) relayoutRows() Material {
	for i := range m.Rounds {
		m.Rounds[i].Rows = roundOrTurnRows(m.Rounds[i], m.Dims)
	}
	for i := range m.Turns {
		m.Turns[i].Rows = roundOrTurnRows(m.Turns[i], m.Dims)
	}
	return m
}

// Relayout returns the material laid out for a canvas of cols x rows: every
// round's rows are wrapped and padded again for the new width, and the dump's
// screen frames are left as they are (they are the dump's own cell grid, sized
// by the geometry the dump recorded — re-wrapping them would be inventing a
// picture the recording does not have).
//
// It exists for the STATIC view (`--round N`): a round can be far taller than the
// terminal, and a caller reading one as plain text wants all of it, so it asks
// for a canvas exactly as tall as the round instead of for the terminal's size.
func Relayout(m Material, cols, rows int) Material {
	if cols <= 0 {
		cols = m.Cols
	}
	if rows <= 0 {
		rows = m.Rows
	}
	m.Cols, m.Rows = cols, rows
	// THE DEBUG LAY-OUT SURVIVES A RESIZE. roundDims builds a fresh Dims from the
	// geometry alone, so without these two lines a `--full` material would resize
	// itself back to the compact spelling AND to the row ceiling — the view would
	// answer the operator's flag once and then quietly stop honouring it the
	// moment a caller named a canvas (the static turn view does exactly that: it
	// re-lays at an exact height).
	wasFull, wasFullLayout := m.Dims.FullToolInput, m.Dims.FullLayout
	m.Dims = roundDims(cols, rows)
	m.Dims.FullToolInput = wasFull
	m.Dims.FullLayout = wasFullLayout
	return m.relayoutRows()
}

// TurnIndexFor resolves a `--turn` argument against this material's TURNS and
// returns the 1-based turn number, or the reason it names none.
//
// THE GRAMMAR IS internal/gopilog's, not a second one here (Rule 14): the
// `all|last|N` selector and the from-the-end counting are that package's
// (ParseHTTPSelector + SessionRoundIndexForCount), so `--turn N` resolves a
// selector exactly the way `--round N` and `gopi session log <id> N` do. The
// COUNT it resolves against is the TURN sequence's own length — this is the user
// surface's address space, and it must never be answered from the round count.
func TurnIndexFor(m Material, arg string) (int, error) {
	sel, err := gopilog.ParseHTTPSelector(arg)
	if err != nil {
		return 0, err
	}
	if sel.All {
		return 0, fmt.Errorf("%s=all lists every turn rather than picking one: use the %s view for that",
			turnFlagWord, turnFlagWord+turnFlagPluralSuffix)
	}
	if len(m.Turns) == 0 {
		return 0, fmt.Errorf("this material has no turns to select (source=%s): a recording with no operator row has no turn to number",
			m.Source)
	}
	idx, ok := gopilog.SessionRoundIndexForCount(len(m.Turns), sel)
	if !ok {
		return 0, fmt.Errorf("turn %s is out of range: this session has %d turn(s) (1-based, 0/%s for the last, -N from the end)",
			arg, len(m.Turns), gopilog.RoundKeywordLast)
	}
	return idx + 1, nil
}

// RoundTurnOf reports which TURN a ROUND belongs to, and the interval of
// rounds that turn covers: (turn, firstRound, lastRound, turnTotal, ok).
//
// It is the ONE join between the two address spaces, so a per-round view can say
// "you are looking at one round of turn 11, which covers rounds 143-152" without
// either unit borrowing the other's number (Rule 14). ok=false means no turn
// span contains that round (a material whose log recorded no operator row), and
// the caller states that rather than guessing a turn.
func RoundTurnOf(m Material, roundIndex int) (turn, first, last, total int, ok bool) {
	for _, t := range m.Turns {
		if t.Kind != RoundTurn {
			continue
		}
		if t.FirstRound <= 0 || t.LastRound <= 0 {
			continue
		}
		if roundIndex >= t.FirstRound && roundIndex <= t.LastRound {
			return t.Index, t.FirstRound, t.LastRound, len(m.Turns), true
		}
	}
	return 0, 0, 0, len(m.Turns), false
}

// turnFlagWord is the flag this package's TURN address is spelled with, and
// turnFlagPluralSuffix its listing's suffix. They mirror the CLI's own constants
// (Rule 1b keeps the SPELLING in one place per package; cmd/agent reads these so
// the -h page and the resolver cannot drift).
const (
	turnFlagWord         = "--turn"
	turnFlagPluralSuffix = "s"
)

// RoundIndexFor resolves a `--round` argument against this material's rounds and
// returns the 1-based round number, or the reason it names none.
//
// THE GRAMMAR IS internal/gopilog's, not a second one here (Rule 14): the
// `all|last|N` selector and the from-the-end counting are that package's
// (ParseHTTPSelector + SessionRoundIndexForCount), so `gopi session replay
// --round N` and `gopi session log <id> N` resolve a selector to the SAME round.
// A selector that names no round is refused by NAME — never clamped, never
// silently answered with a different round.
func RoundIndexFor(m Material, arg string) (int, error) {
	sel, err := gopilog.ParseHTTPSelector(arg)
	if err != nil {
		return 0, err
	}
	if sel.All {
		return 0, fmt.Errorf("%s=all lists every round rather than picking one: use the %s view for that",
			roundFlagWord, roundFlagWord+roundsFlagSuffix)
	}
	if m.RoundCount == 0 {
		return 0, fmt.Errorf("this material has no session rounds to select (source=%s): a session with no `request` event carrying content has nothing to number",
			m.Source)
	}
	idx, ok := gopilog.SessionRoundIndexForCount(m.RoundCount, sel)
	if !ok {
		return 0, fmt.Errorf("round %s is out of range: this session has %d round(s) (1-based, 0/%s for the last, -N from the end)",
			arg, m.RoundCount, gopilog.RoundKeywordLast)
	}
	return idx + 1, nil
}

// roundsFlagSuffix / roundFlagWord are how this package spells the two views in
// a refusal it raises itself. The CLI's own flag constants are the dispatch
// side's (cmd/agent owns its argv), so these name the WORD only.
const (
	roundsFlagSuffix = "s"
	roundFlagWord    = "round"
)

// Frame is one screenful of the DUMP's own material (see Material.DumpFrames).
type Frame struct {
	Kind FrameKind
	// Label names the frame in the status line ("scrollback 2/24", "screen").
	Label string
	// Rows is the frame's rows, top to bottom, at most Material.Rows of them.
	Rows []FrameRow
}

// The geometry provenance words (see Material.GeometrySource).
const (
	GeometryFromDump     = "dump"
	GeometryFromTerminal = "terminal"
	GeometryFromFallback = "fallback"
	// GeometryFromStream is the verbatim leg's grid: the size the recording's
	// SINK was built at, read from its own header. It is a distinct word from
	// "dump" because the two artifacts can hold different geometries (a run that
	// resized wrote both), and a replay must say which one it is painting at.
	GeometryFromStream = "screen-stream"
)

// FallbackGeometry is the geometry used when neither the dump nor the terminal
// can state one. It is a DEFAULT, not a discovery: the caller announces it.
const (
	FallbackCols = 80
	FallbackRows = 24
)

// LoadSession reads the material of one session directory, from BOTH artifacts
// when it has both, and from whichever exists when it has one.
//
// WHY BOTH. The dump holds ONE frame (its sink installs state with an atomic
// rename), so a dump alone cannot step — but the dump is also the only artifact
// that knows the SCREEN: its geometry, its cell-exact grid and the truecolour
// each cell was painted with. The JSONL timeline is the opposite: every step of
// the conversation, no cells and no colour. A playback wants both, so the frame
// sequence is the timeline's steps followed by the recording's own screen, and
// the status line names which kind is showing.
//
// GEOMETRY comes from the dump when there is one (that is the size the screen
// WAS); a timeline-only session has no screen size at all, so its frames are
// drawn at the caller's terminal and the playback says so.
//
// The returned error names BOTH artifacts and what each would have to be, since
// "there is nothing to replay" is only actionable with the reason.
//
// THE TRACE IS RESOLVED, NOT SPELLED. The session tree has more than one layout
// (a bare session dir holds session.jsonl; a task-run dir holds one record per
// run, named by that run's uuid — internal/sessionlayout's own vocabulary), and
// this reader asks that package which file is THIS dir's log rather than looking
// for one name. A reader that spelled `session.jsonl` reported "nothing to
// replay" for every task-run session while looking correct for a chat: measured
// on a real task-run dir, a three-row run trace reproduced the refusal for a
// log full of text. LoadSessionAt stays the door for a caller that has already
// resolved the path itself (the CLI does, through the same resolver
// `gopi session log` uses).
func LoadSession(dir string) (Material, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return Material{}, fmt.Errorf("no session directory was given")
	}
	if rec, ok := sessionlayout.RecordInDir(dir); ok {
		return LoadSessionAt(dir, rec.Path)
	}
	// No record in the dir: hand LoadSessionAt the bare layout's name, so the
	// refusal it raises names the file this reader would have looked for.
	return LoadSessionAt(dir, filepath.Join(dir, SessionJSONLName))
}

// LoadSessionAt is LoadSession for a session whose artifacts are NOT both in one
// directory: the screen dump is looked for under dumpDir, and the event timeline
// is read from the EXACT path jsonlPath (empty = look for it beside the dump).
//
// WHY A SECOND ENTRY POINT (measured on this host, 2026-10-06). peer1's session
// tree has TWO layouts, and only ONE of them puts the log beside the artifacts:
//
//	sessions/<session-id>/session.jsonl            a bare session/chat run
//	sessions/<task-id>/<session-id>.jsonl          a TASK run — the directory is
//	                                              named by the TASK, the file by
//	                                              the SESSION
//
// The task-run layout is the majority of the tree (3,931 bare `session.jsonl`
// files, and every task run grew a `<uuid>.jsonl` beside one). A replay that
// only knew the first layout refused every worker session — the operator's own
// criterion is that ANY session replays, so the caller resolves the log through
// the tree's ONE resolver (internal/gopilog's FindSession, the same one
// `gopi session log <id>` uses) and hands the path here.
func LoadSessionAt(dumpDir, jsonlPath string) (Material, error) {
	return loadSessionAtMeasuredFrom(dumpDir, jsonlPath, StreamFromAuto, terminalgeom.Standard())
}

// LoadSessionAtFrom is LoadSessionAt with the SOURCE CHOICE named by the caller:
// StreamFromAuto (the verbatim leg when the session has one, the dump otherwise)
// or StreamFromDump (the dump, always — see StreamFrom* below).
func LoadSessionAtFrom(dumpDir, jsonlPath string, from string) (Material, error) {
	return loadSessionAtMeasuredFrom(dumpDir, jsonlPath, from, terminalgeom.Standard())
}

// loadSessionAtMeasured is LoadSessionAt with the terminal MEASUREMENT taken as
// a parameter, so the geometry decision below can be driven against a terminal
// the caller OWNS (a pty of a known size in a test) instead of whatever the
// process happens to have. Under `go test` the standard descriptors are pipes,
// so an arm that asserted on the production probe would be asserting on the
// runner's plumbing rather than on this decision.
//
// The production entry points pass terminalgeom.Standard() — the tree's ONE
// TIOCGWINSZ answer (Rule 14) — so there is no second probe anywhere.
func loadSessionAtMeasured(dumpDir, jsonlPath string, geo sessionevent.TerminalGeometry) (Material, error) {
	return loadSessionAtMeasuredFrom(dumpDir, jsonlPath, StreamFromAuto, geo)
}

// loadSessionAtMeasuredFrom is the real body: the measured entry points above add
// the SOURCE CHOICE, which is a property of the invocation rather than of the
// terminal probe.
func loadSessionAtMeasuredFrom(dumpDir, jsonlPath, from string, geo sessionevent.TerminalGeometry) (Material, error) {
	dir := strings.TrimSpace(dumpDir)
	if dir == "" {
		return Material{}, fmt.Errorf("no session directory was given")
	}
	jsonlPath = strings.TrimSpace(jsonlPath)
	dumpPath := filepath.Join(dir, DumpJSONName)
	if jsonlPath == "" {
		jsonlPath = filepath.Join(dir, SessionJSONLName)
	}

	var (
		dump, timeline Material
		dumpErr, tlErr error
		haveDump       bool
		haveTL         bool
	)
	if st, err := os.Stat(dumpPath); err == nil && !st.IsDir() {
		dump, dumpErr = LoadDump(dumpPath)
		haveDump = dumpErr == nil
	}
	if st, err := os.Stat(jsonlPath); err == nil && !st.IsDir() {
		haveTL = true
	}

	// The rounds are laid out for the geometry the playback will use, so the
	// round body's wrap and the dump's grid agree about how wide a row is.
	//
	// THREE CHANNELS, IN THIS ORDER, and the MIDDLE one is the fix for a WORKER
	// run's replay (measured 2026-10-28, task 01a18269). A chat surface's dump
	// records the grid it painted, so its cols/rows are the screen that WAS and
	// the playback must stay cell-exact on them. A worker run records NO screen at
	// all: its session log carries long prose, paths and command output, and the
	// old fallback wrapped all of it at a hard 80 while the operator was watching
	// on a 200-column terminal — the same content the terminal could have shown
	// whole arrived as narrow strips. So a session with no dump wraps at the
	// terminal the operator is actually on (terminalgeom — the tree's ONE
	// measurement of it), and only a run with no terminal at all (a pipe, a
	// redirect, CI) falls back to the documented default, which it announces.
	cols, rows, geoSource := FallbackCols, FallbackRows, GeometryFromFallback
	if haveDump {
		cols, rows, geoSource = dump.Cols, dump.Rows, GeometryFromDump
	} else if geo.Known() {
		cols, rows, geoSource = geo.Cols, geo.Rows, GeometryFromTerminal
	}
	if haveTL {
		timeline, tlErr = LoadJSONLAt(jsonlPath, cols, rows)
		if tlErr == nil {
			timeline.GeometrySource = geoSource
		} else {
			haveTL = false
		}
	}

	// THE VERBATIM LEG IS TRIED FIRST, and the dump is the FALLBACK (see
	// stream.go's header for why, and SourceLabelStream/SourceLabelDump for the
	// two labels a reader must never confuse). It is looked for in the SAME
	// directory the dump would come from, through the ONE suffix list, so a
	// pre-rename `.sgr.stream` is found too.
	//
	// A `from` of StreamFromDump SKIPS it — an operator asking for the dump's own
	// picture is asking a different question ("what did THIS build's interpreter
	// make of it?"), and quietly answering with the verbatim leg would be the same
	// defect in the other direction.
	stream, streamNote, streamErr := loadStreamLeg(dir, from, &cols, &rows, &geoSource)
	// `--from stream` IS A REQUIREMENT (measured 2026-11-08: it fell through to the
	// dump here, so a caller who demanded the verbatim leg was silently handed the
	// interpretation — the exact defect the flag exists to make impossible). The
	// leg's error is carried up INSTEAD of the dump's picture; --from auto still
	// keeps the error only as the note for the refusal below, because there a
	// fallback IS what was asked for.
	if from == StreamFromStream && streamErr != nil {
		return Material{}, streamErr
	}

	// The material's SessionID is the DIRECTORY's name when the caller did not
	// name the session itself; a task-run layout names its directory after the
	// TASK, so a caller that resolved an id passes it in (see LoadSessionAt).
	m := Material{
		SessionID:      filepath.Base(filepath.Clean(dir)),
		Dir:            dir,
		Cols:           cols,
		Rows:           rows,
		GeometrySource: geoSource,
		// The ROUNDS are the timeline's (the only artifact that has rounds at
		// all) and the DUMP's screen frames are the dump's: two units, two
		// fields, never one mixed sequence (see Material.Rounds).
		Rounds:      timeline.Rounds,
		Turns:       timeline.Turns,
		RoundCount:  timeline.RoundCount,
		PromptTurns: timeline.PromptTurns,
		// The TURN count is the timeline's too (a dump records no operator row),
		// and it travels so the playback can print the neighbour under its OWN
		// name beside the round count (Rule 15: a reader with the other verb's
		// figure must see which is which).
		TurnCount: timeline.TurnCount,
		// THE FRAMES ARE THE VERBATIM LEG'S WHEN IT HAS ONE. `stream` is nil for a
		// session that recorded no screen (a worker run), and for a caller who
		// asked for the dump — so the dump remains the fallback it was, and the
		// two are told apart by SourceLabel, never by the frames themselves.
		DumpFrames: dump.DumpFrames,
		Dims:       roundDims(cols, rows),
	}
	if stream != nil {
		m.DumpFrames = stream.Frames
		m.Stream = stream
	}
	m.StreamNote = streamNote
	switch {
	case haveDump && haveTL:
		m.Source = SourceBoth
	case haveDump:
		m.Source = dump.Source
		// A dump alone has no rounds: it records the SCREEN, and a screen is
		// not a round. Rounds stays empty and RoundCount stays 0 — the playback
		// then has the dump's frames to step through and says so, rather than
		// inventing rounds from its own material.
	case haveTL:
		m.Source = timeline.Source
	default:
		return Material{}, fmt.Errorf("nothing to replay in %s: %s is absent or unreadable (%v) and %s is absent or unusable (%v) — the dump is written while a chat surface runs (`gopi chat` writes one by default); list the chat sessions with `gopi session list --mode chat`",
			dir, DumpJSONName, reasonOrMissing(dumpErr, !haveDump), jsonlPath, reasonOrMissing(tlErr, !haveTL))
	}
	if len(m.Rounds) == 0 && len(m.DumpFrames) == 0 {
		return Material{}, noFramesRefusal(dir, streamErr)
	}
	// THE SOURCE LABEL IS DECIDED HERE, once, from what actually loaded — never
	// from what was looked for. Rule 15: a reader trusts this one word, so it may
	// only ever claim `verbatim` when the frames came from the renderer's bytes.
	switch {
	case m.Stream != nil:
		m.SourceLabel = SourceLabelStream
	default:
		m.SourceLabel = SourceLabelDump
	}
	return m, nil
}

// roundDims is the canvas a round's body is laid out at: the whole canvas minus
// the status row, so the status line and the round's own rows cannot collide
// (StatusRow is the last row of the canvas, and it is render.go's).
func roundDims(cols, rows int) Dims {
	d := Dims{Cols: cols, Rows: rows}
	if rows > 0 {
		d.Rows = rows - 1
	}
	return d
}

// noFramesRefusal is the refusal a session deserves when the material loaded but
// carries neither a round nor a frame. It exists as its own function for ONE
// reason: the `streamErr != nil` arm is the only place in LoadSession that
// reports a stream that was RECORDED but could not be READ, and it is not
// reachable from a test — every timeline shape (a request row, an assistant row,
// a tool_result row, a blank line) still yields a round, and a session with no
// timeline at all returns from the earlier `default` arm naming both absent
// artifacts. So the error SHAPING is lifted out here, where a test can hand it a
// real error and assert the chain — see TestNoFramesRefusalKeepsTheStreamErrorReachable.
//
// %w, never %v (Rule 16): the wrapped error is the ONLY detailed diagnostic of
// WHY the recording is unusable — LoadScreenStream answers with
// *uiclient.SgrStreamContradictionError, or the open error of an unreadable path
// — so a caller must be able to reach it with errors.Is/errors.As instead of
// re-parsing this sentence. The message still names both facts: the directory and
// the fact that nothing else carries a frame.
func noFramesRefusal(dir string, streamErr error) error {
	if streamErr != nil {
		return fmt.Errorf("nothing to replay in %s: the screen stream could not be read (%w) and no other artifact carries a frame", dir, streamErr)
	}
	return fmt.Errorf("nothing to replay in %s: the material loaded but carries neither a round nor a frame", dir)
}

// reasonOrMissing spells the reason an artifact was unusable, or says it is not
// there at all — an error that named neither would send its reader looking for a
// file that exists.
func reasonOrMissing(err error, missing bool) string {
	if missing && err == nil {
		return "not present"
	}
	if err != nil {
		return err.Error()
	}
	return "not present"
}

// dumpFile is the subset of the dump's JSON this reader needs. The screen block
// is decoded into the OWNER's type (lineedit.ScreenDump) so the format has ONE
// definition and this reader cannot drift from it.
type dumpFile struct {
	Identity struct {
		Surface string `json:"surface"`
	} `json:"identity"`
	Observer struct {
		Frames   int    `json:"frames"`
		Dumps    int    `json:"dumps"`
		Geometry string `json:"geometry"`
	} `json:"observer"`
	Screen lineedit.ScreenDump `json:"screen"`
}

// LoadDump builds the frame sequence from one chat screen dump (the ".json" twin
// of the text dump).
func LoadDump(path string) (Material, error) {
	blob, err := os.ReadFile(path)
	if err != nil {
		return Material{}, fmt.Errorf("read the screen dump %s: %w", path, err)
	}
	var d dumpFile
	if err := json.Unmarshal(blob, &d); err != nil {
		return Material{}, fmt.Errorf("parse the screen dump %s: %w", path, err)
	}
	sc := d.Screen
	if sc.Cols <= 0 || sc.Rows <= 0 {
		return Material{}, fmt.Errorf("the screen dump %s states no geometry (cols=%d rows=%d): a frame with no width cannot be drawn",
			path, sc.Cols, sc.Rows)
	}
	if len(sc.Grid) == 0 && len(sc.Scrollback) == 0 {
		return Material{}, fmt.Errorf("the screen dump %s carries neither a grid nor scrollback: the surface had not painted yet",
			path)
	}
	m := Material{
		Cols:           sc.Cols,
		Rows:           sc.Rows,
		Source:         SourceScreenDump,
		GeometrySource: GeometryFromDump,
	}
	// (1) The scrollback, a screenful per frame — the history the screen pushed
	// off its first row, in the order it happened.
	pages := chunkLines(sc.Scrollback, sc.Rows)
	for i, page := range pages {
		rows := make([]FrameRow, 0, len(page))
		for _, line := range page {
			rows = append(rows, FrameRow{Cells: fitCells(line, sc.Cols)})
		}
		m.DumpFrames = append(m.DumpFrames, Frame{
			Kind:  FrameScrollback,
			Label: fmt.Sprintf("scrollback %d/%d", i+1, len(pages)),
			Rows:  rows,
		})
	}
	// (2) The painted grid — what was on screen when the dump was written, WITH
	// the dump's own per-cell truecolour.
	if len(sc.Grid) > 0 {
		rows := make([]FrameRow, 0, len(sc.Grid))
		for i, g := range sc.Grid {
			var runs []lineedit.DumpAttrRun
			if sc.AttrsRecorded && i < len(sc.AttrRuns) {
				runs = sc.AttrRuns[i]
			}
			rows = append(rows, FrameRow{Cells: fitCells(g, sc.Cols), Runs: runs})
		}
		m.DumpFrames = append(m.DumpFrames, Frame{Kind: FrameScreen, Label: frameScreenLabel, Rows: rows})
	}
	return m, nil
}

// The words a round's own rows are labelled with, and the round labels the
// status line prints. Each is spelled once (Rule 1b) because the player, the
// static views and the tests all quote them.
const (
	// frameScreenLabel names the dump's painted-grid frame.
	frameScreenLabel = "screen"
	// roundLabelPrompt names the prepended opening-request entry.
	roundLabelPrompt = "prompt"
	// roundLabelModelFmt is a model round's label: "round 3/50".
	roundLabelModelFmt = "round %d/%d"
	// The field labels inside a round.
	roundPromptLabel   = "PROMPT: "
	roundThinkingLabel = "THINKING:"
	roundTextLabel     = "TEXT:"
	// roundToolLabel / roundResultLabel open a tool CALL and its RESULT. They are
	// internal/lineedit's TWO glyphs — the same marker the live status line, the
	// chat transcript and `chat --replay` draw — because a static view that spelled
	// them "TOOL:"/"RESULT:" printed the very same content under a name nothing
	// else in the system used, so a reader comparing the two views (or grepping
	// for the marker the operator's own report used) scored this one ZERO tool
	// calls while it printed eight (measured 2026-10-26, session 80c07a7d).
	roundToolLabel        = lineedit.ToolGlyph + " "
	roundResultLabel      = lineedit.NoteContinuation
	roundHarnessLabel     = "HARNESS:"
	roundNoResultLabel    = "(the tool recorded no result)"
	roundTruncatedRowFmt  = "(round truncated: %d more row(s) of this round are not laid out)"
	roundIndent           = "  "
	roundToolInputMaxRune = 160
	// roundRowsMax bounds how many rows ONE round contributes to the layout. It
	// exists so a round carrying a very large tool result cannot make the
	// material unbounded; the truncation is PRINTED (roundTruncatedRowFmt), never
	// silent (Rule 15).
	//
	// THE ONE DOOR A DEBUG VIEW OPENS (Dims.FullLayout → Dims.rowCeiling): a
	// lay-out asked for in full emits every row and prints no truncation note,
	// because nothing was left out. The DEFAULT is this number.
	roundRowsMax = 4000
	// roundStaticCols is the fallback width for RoundsRow when the caller names
	// no canvas: the material's own width is the caller's business, so a round
	// laid out with none gets the package's documented fallback geometry rather
	// than a literal at the call site.
	roundStaticCols = FallbackCols
	// reportRowCutLabel is what a REPORT row wears when the canvas is too narrow
	// to carry it whole. A report row is this package speaking, not content it is
	// showing, so it may be SHORTENED — but never clipped, and never with the
	// `…` that means "content was cut" everywhere else in this view
	// (roundToolInputMaxRune's `…(+N runes)`, and the model's own prose).
	//
	// THE DEFECT THIS CLOSES (measured 2026-11-01, session 61f5992a
	// `session replay --turn 8 --cols 40`): the response row read
	// `(3 row(s) of this turn's context are not` — fitCells ended the row at the
	// canvas edge and the tail (`; its tool rows are)`) was dropped WITHOUT a
	// mark, so the row the package prints to admit a cut was itself cut
	// silently. A report row that cannot be clipped is the only fixed point.
	//
	// IT IS NOT U+2026, deliberately (the epic's first requirement): an ellipsis
	// here would read exactly like content being cut, which is the one meaning
	// this row must not borrow. The spelling is a WORD, so the three things the
	// view can do to a line stay distinguishable by glyph alone — drawn whole,
	// shortened by this label, or cut by roundToolInputMaxRune.
	reportRowCutLabel = " [NOT LAID OUT]"
)

// ReportRowCutLabel is reportRowCutLabel for callers OUTSIDE this package — the
// help text that has to tell a reader what a shortened report row looks like.
// It is exported so the page quotes the glyph the renderer draws instead of
// typing a second copy of it (Rules 1/13: a page saying one thing over a
// renderer drawing another is exactly the drift this accessor forbids).
func ReportRowCutLabel() string { return reportRowCutLabel }

// LoadJSONL builds the round sequence from the session's event timeline at the
// default geometry (FallbackCols x FallbackRows).
func LoadJSONL(path string) (Material, error) {
	return LoadJSONLAt(path, FallbackCols, FallbackRows)
}

// LoadJSONLAt builds the ROUND sequence from the session's event timeline: one
// round per model round, using internal/gopilog's grouping.
//
// WHY IT DELEGATES, and this is the change's whole point (Rule 14). This reader
// used to walk the JSONL itself, one row per step, keeping only the user and
// assistant rows that carried text. That was a SECOND definition of "what is in
// a session log", and it disagreed with the first one on every count that
// matters: it dropped thinking and tool calls (so a step showed neither), it
// split one round across several steps, and it numbered a harness clock tick as
// if the operator had said it (measured on a real run: 68 steps for a session
// whose own round count was 50). internal/gopilog.ParseFile already answers
// "what are this session's rounds" — it is what `gopi session log <id> --rounds`
// counts — so the playback reads THAT answer instead of re-deriving it:
//
//	Material.Rounds[i]  ←→  s.Iterations[i]    (1:1 — no row walking here)
//	Material.RoundCount ←→  gopilog.SessionRounds(s)   (what `--rounds` prints)
//
// // TWO THINGS THIS GAINS AND ONE IT KEEPS. The rounds carry what the old frames
// threw away: the FULL thinking text, the round's prose, every tool call with
// its result, and the harness rows that arrived during the round — all inside
// ONE step, because a round is the unit a viewer steps through. What it keeps is
// the refusal discipline: a log whose rounds all came out empty is refused by
// name, never played as an empty recording.
func LoadJSONLAt(path string, cols, rows int) (Material, error) {
	sess, err := gopilog.ParseFile(path)
	if err != nil {
		return Material{}, fmt.Errorf("read the session log %s: %w", path, err)
	}
	if cols <= 0 {
		cols = FallbackCols
	}
	if rows <= 0 {
		rows = FallbackRows
	}
	m := Material{
		Cols:           cols,
		Rows:           rows,
		Source:         SourceSessionLog,
		GeometrySource: GeometryFromFallback,
		RoundCount:     gopilog.SessionRounds(sess),
		TurnCount:      gopilog.SessionTurns(sess),
		Dims:           roundDims(cols, rows),
	}
	// (1) The session's OPENING REQUEST, when the log recorded one that no round
	// already carries. The reader owns the answer to "which user row is the
	// session's original request" (Session.Task — its own startup comment), so
	// this reader asks rather than re-deciding. The check is exact and textual:
	// a log whose first round already carries that text as its prompt does not
	// get a second copy of it.
	opening := strings.TrimSpace(sess.Task)
	canonical := gopilog.SessionCanonicalRounds(sess)
	switch {
	case len(canonical) == 0:
		// The log recorded a request the reader could not pair with content, so
		// there is no ROUND at all — only the opening request. It is still worth
		// replaying: it is the session's own text. It becomes the one step, in
		// the PromptTurns slot, and RoundCount stays 0 — "this session has no
		// round of its own to show".
		m.PromptTurns = 1
		m.Rounds = []Round{roundFromOpening(opening, m.Dims)}
	default:
		// ONE STEP PER TURN — the unit every session surface now prints and the
		// playback now steps through (the operator's ruling, 2026-10-22:
		// sessionevent.CounterUnitTurn). The spans come from
		// gopilog.SessionTurnSpans — the ONE place the turn boundaries are
		// decided — so the steps, `session log --rounds`' rows and `session
		// stats`' headline count are three views of one fact (Rule 14).
		//
		// The ROUND sequence is built too, and KEPT SEPARATELY (m.Rounds): the
		// per-round views (`--round N`, `gopi session log <id> N`, the raw
		// pairing) address a ROUND, and that address is unchanged. What changed
		// is which sequence the player steps through and which number the status
		// line's "k/N" names.
		for _, cr := range canonical {
			if cr.HasContent() {
				m.Rounds = append(m.Rounds, roundFromIteration(cr.Ordinal, sess, cr.Iter, m.Dims))
				continue
			}
			m.Rounds = append(m.Rounds, roundNoContent(cr.Ordinal, sess, m.Dims))
		}
		turns := gopilog.SessionTurnSpans(sess)
		for _, sp := range turns {
			m.Turns = append(m.Turns, roundFromTurn(sp, len(turns), sess, m.Dims))
		}
		// The session's OPENING REQUEST rides INSIDE the first round when round 1
		// does not already carry it as its prompt (the log's first user row was
		// not handed to the first request): it is what round 1 was answering, so
		// a step of its own would make the status line's "k/N" count something
		// the other surfaces do not.
		if opening != "" && !carriedByFirstRound(sess, opening) {
			m.Rounds[0].Opening = opening
			m.Rounds[0].Rows = roundOrTurnRows(m.Rounds[0], m.Dims)
			m.Turns[0].Opening = opening
			m.Turns[0].Rows = roundOrTurnRows(m.Turns[0], m.Dims)
		}
	}
	if len(m.Rounds) == 0 {
		return Material{}, fmt.Errorf("the session log %s carries no rounds: no `request` event kept content and no opening request was recorded", path)
	}
	// A material whose turn reader produced no step falls back to its rounds,
	// which is what it was before the TURN became the step unit — never an empty
	// playback (Rule 15: the fallback is stated here and the count of steps is
	// reported, so a caller can see which shape it got).
	if len(m.Turns) == 0 {
		m.Turns = append([]Round(nil), m.Rounds...)
	}
	return m, nil
}

// carriedByFirstRound reports whether the session's opening request is already
// the first round's prompt — the ordinary case, since the reader hands the
// first unconsumed user row to the iteration the first request opens.
func carriedByFirstRound(sess *gopilog.Session, opening string) bool {
	if len(sess.Iterations) == 0 {
		return false
	}
	return strings.TrimSpace(sess.Iterations[0].UserPrompt) == opening
}

// roundFromIteration renders one model round: the prompt it answered, the
// harness rows that arrived inside it, its reasoning, its prose, and its tool
// calls with their results.
func roundFromIteration(index int, sess *gopilog.Session, i int, dims Dims) Round {
	it := &sess.Iterations[i]
	r := Round{
		Index:          index,
		Kind:           RoundModel,
		Label:          fmt.Sprintf(roundLabelModelFmt, index, gopilog.SessionRounds(sess)),
		Prompt:         strings.TrimSpace(it.UserPrompt),
		HarnessPrompts: it.HarnessPrompts,
		Text:           it.TextBlocks,
		Thinking:       it.ThinkingBlocks,
	}
	for _, tc := range it.ToolCalls {
		r.Calls = append(r.Calls, ToolCallView{
			Name:     tc.Name,
			CallID:   tc.ID,
			InputRaw: string(tc.InputRaw),
			Result:   tc.Result,
			ExitCode: tc.ExitCode,
		})
	}
	r.Rows = roundRows(r, dims)
	return r
}

// roundFromTurn renders ONE TURN: everything the content of iterations
// [start, end) holds, as a single step.
//
// WHY A RANGE AND NOT AN INDEX — AND WHY IT IS NOW THE DEFAULT STEP. The
// operator's ruling (2026-10-22, sessionevent.CounterUnitTurn) made the TURN
// the unit a session surface prints and steps through, "because a user only
// considers one thing a request: one input they typed or spoke — how many
// HTTP request/response pairs the harness then ran against the model in between
// is not the same thing and must not be mixed up". So the person asked once,
// the harness answered with as many exchanges as the work took, and ALL of them
// are ONE step: prompt, harness rows, reasoning, prose and every tool call with
// its result, concatenated across the iterations the turn spans. Each exchange
// as its own step was the shape this replaces.
//
// The exchanges inside the turn are still counted — as the step's ANNOTATION
// ("turn 3/8  (5 request(s), 12 tool call(s))"), which is the shape
// `gopi chat --replay` already printed. The label is built by
// gopilog.FormatTurnStepLabel, the ONE place that sentence is spelled.
//
// total is the session's TURN count, so the label's "k/N" is the same N the
// status line and the material carry (Rule 14: one number, one source).
func roundFromTurn(span gopilog.SessionTurnSpan, total int, sess *gopilog.Session, dims Dims) Round {
	r := Round{
		Index:      span.Turn,
		Kind:       RoundTurn,
		Label:      gopilog.FormatTurnStepLabel(span.Turn, total, span.Requests, span.ToolCalls),
		FirstRound: span.FirstRound,
		LastRound:  span.LastRound,
	}
	start, end := span.Start, span.End
	if start < 0 || start >= len(sess.Iterations) {
		r.Rows = roundRows(r, dims)
		return r
	}
	if end > len(sess.Iterations) {
		end = len(sess.Iterations)
	}
	for i := start; i < end; i++ {
		it := &sess.Iterations[i]
		// The round's prompt is what the OPERATOR asked: the first non-empty
		// UserPrompt in the segment wins (a later exchange's prompt is the same
		// request re-sent, or empty when the reader held none).
		if r.Prompt == "" {
			r.Prompt = strings.TrimSpace(it.UserPrompt)
		}
		r.HarnessPrompts = append(r.HarnessPrompts, it.HarnessPrompts...)
		r.Thinking = append(r.Thinking, it.ThinkingBlocks...)
		r.Text = append(r.Text, it.TextBlocks...)
		// THE ROUNDS THEMSELVES ARE KEPT, not only their union: a turn is many
		// rounds, and the flattened fields beside this one cannot say which round
		// a call or a tick came from. See TurnBodyRows for the measurement that
		// made this necessary (a turn's eight tool calls printed behind 170 rows
		// of other rounds' prose).
		seg := Round{
			Index:          it.RequestOrdinal,
			Kind:           RoundModel,
			Prompt:         strings.TrimSpace(it.UserPrompt),
			HarnessPrompts: it.HarnessPrompts,
			Text:           it.TextBlocks,
			Thinking:       it.ThinkingBlocks,
		}
		for _, tc := range it.ToolCalls {
			view := ToolCallView{
				Name:     tc.Name,
				CallID:   tc.ID,
				InputRaw: string(tc.InputRaw),
				Result:   tc.Result,
				ExitCode: tc.ExitCode,
			}
			seg.Calls = append(seg.Calls, view)
			r.Calls = append(r.Calls, view)
		}
		r.Segments = append(r.Segments, seg)
	}
	r.RoundTotal = gopilog.SessionRounds(sess)
	r.Rows = roundOrTurnRows(r, dims)
	return r
}

// roundOrTurnRows lays out a row that may be a TURN: a turn goes through
// TurnBodyRows (its rounds in the order they ran, each under its own header),
// and every other row through roundRows (its own rows, in its own order). It is
// the ONE dispatch every lay-out site calls, so the player, the static view and
// a re-layout cannot disagree about what a turn contains.
func roundOrTurnRows(r Round, dims Dims) []FrameRow {
	if r.Kind == RoundTurn && len(r.Segments) > 0 {
		return turnBodyRows(r, dims)
	}
	return roundRows(r, dims)
}

// roundNoContent renders a CANONICAL round the reader holds no content for: the
// exchange went over the wire (the `request` row recorded its usage) and no
// blocks were kept for it. The step exists to keep the numbering gap-free and
// it carries the one fact it has — the round's usage — rather than being
// silently skipped or filled with a neighbour's body (Rule 15).
func roundNoContent(ordinal int, sess *gopilog.Session, dims Dims) Round {
	r := Round{
		Index: ordinal,
		Kind:  RoundNoContent,
		Label: fmt.Sprintf(roundLabelModelFmt, ordinal, gopilog.SessionRounds(sess)),
		Text:  []string{roundNoContentBody},
	}
	r.Rows = roundRows(r, dims)
	return r
}

// roundNoContentBody is what a no-content round's step says on screen. It names
// the canonical unit so a viewer reading a run of these knows they are real
// exchanges (a session that hit an error every other call shows them) and not a
// rendering fault.
const roundNoContentBody = "(this round carried no content — it was a request/response pair and the reader recorded no blocks for it)"

// roundFromOpening renders a session whose log recorded an opening request and
// no round of its own.
func roundFromOpening(text string, dims Dims) Round {
	r := Round{Index: 0, Kind: RoundPrompt, Label: roundLabelPrompt, Opening: text}
	r.Rows = roundRows(r, dims)
	return r
}

// RoundsRow re-lays ONE round — a TURN is one of them — out for a canvas
// exactly as tall as the round carries content, so nothing of it is clipped.
//
// THE STATIC VIEW NEEDS THIS AND THE PLAYER DOES NOT: the player clips to the
// terminal and PgDn reads the rest, while a caller printing a turn with no
// terminal wants all of it (its tool calls and their results included). It goes
// through the SAME lay-out function the player uses (roundRows), so the static
// view and the picture cannot disagree about what a turn contains — only about
// where the lines break, and the width is the caller's.
func RoundsRow(r Round, cols int) []FrameRow {
	if cols <= 0 {
		cols = roundStaticCols
	}
	return roundRows(r, Dims{Cols: cols, Rows: len(r.Rows) + 1})
}

// roundRows lays a round out as rows, each tagged with the ROLE the renderer
// colours it by: the opening request, the prompt, the harness rows, the
// reasoning, the prose, then each tool call followed by its result. Nothing is
// dropped except a round's overflow past the lay-out's ceiling (Dims.rowCeiling:
// roundRowsMax by default, NONE when the caller asked for the debug view) — and
// that is said in a row of its own.
func roundRows(r Round, dims Dims) []FrameRow {
	var rows []FrameRow
	// atCeiling is THE ceiling test of this lay-out, asked once per row appended.
	// A ceiling of 0 means "no ceiling" (Dims.rowCeiling), which is why the test
	// is `> 0` and not `len(rows) >= ceiling`. The ceiling is READ ONCE: it is a
	// property of the lay-out, not of the rows laid out so far.
	ceiling := dims.rowCeiling()
	atCeiling := func() bool {
		return ceiling > 0 && len(rows) >= ceiling
	}
	// addCarrying lays one row out and carries it TYPED, for the rows that wear a
	// MEANINGFUL mark at column 0 (a tool call's `⚙ `, its result's `↳ `): their
	// wrap continuation keeps the mark, because it belongs to the block. Content
	// rows go through addContent instead — the two are never the same row.
	//
	// THE MARK COMES OUT OF THE WRAP WIDTH (wrapPrefixed). Prefixing a
	// continuation AFTER the wrap pushed its tail two cells past the canvas, and
	// fitCells then cut the TEXT — silently, because fitCells cannot tell an
	// over-wide row from a long one (measured 2026-10-28: a 200-column `--turn 1`
	// of a WORKER session lost the word "no" between two rows, so "nothing to
	// pull and" / "thing to push" read as a hard chop).
	addCarrying := func(role RowRole, text string) {
		if atCeiling() {
			return
		}
		for _, line := range wrapPrefixed(text, "", roundResultLabel, dims.Cols) {
			if atCeiling() {
				return
			}
			rows = append(rows, FrameRow{Cells: fitCells(line, dims.Cols), Role: role})
		}
	}
	// addContent lays a CONTENT block out: every line it produces is inset, so no
	// line of a prompt, a reasoning block, the prose or a tool's result can begin
	// at column 0 wearing a marker that means "a call happened here".
	addContent := func(role RowRole, text string) {
		if atCeiling() {
			return
		}
		for _, line := range contentLines(text, dims.Cols) {
			if atCeiling() {
				return
			}
			rows = append(rows, FrameRow{Cells: fitCells(line, dims.Cols), Role: role})
		}
	}
	// The opening request is the session's original prompt and takes the prompt
	// role: to a viewer it is the request this round answers.
	if s := strings.TrimSpace(r.Opening); s != "" {
		addCarrying(RolePrompt, roundPromptLabel+s)
	}
	if s := strings.TrimSpace(r.Prompt); s != "" {
		addCarrying(RolePrompt, roundPromptLabel+s)
	}
	for _, h := range r.HarnessPrompts {
		if s := strings.TrimSpace(h); s != "" {
			addCarrying(RoleHarness, roundHarnessLabel+roundIndent+s)
		}
	}
	for _, th := range r.Thinking {
		if s := strings.TrimSpace(th); s != "" {
			addCarrying(RoleThinking, roundThinkingLabel)
			addContent(RoleThinking, s)
		}
	}
	for _, t := range r.Text {
		if s := strings.TrimSpace(t); s != "" {
			addCarrying(RoleText, roundTextLabel)
			addContent(RoleText, s)
		}
	}
	for _, c := range r.Calls {
		addCarrying(RoleToolCall, roundToolLabel+toolCallLine(c, dims))
		if res := strings.TrimSpace(c.Result); res != "" {
			addCarrying(RoleToolResult, roundResultLabel)
			addContent(RoleToolResult, res)
		} else {
			addCarrying(RoleError, roundResultLabel+roundNoResultLabel)
		}
	}
	// The overflow row is added only when something WAS dropped, so a round that
	// fits never carries a false note about itself.
	if dropped := countRoundRows(r, dims) - len(rows); dropped > 0 {
		rows = append(rows, FrameRow{Cells: fitReportRow(fmt.Sprintf(roundTruncatedRowFmt, dropped), dims.Cols), Role: RoleError})
	}
	return rows
}

// countRoundRows is how many rows the round WOULD lay out without the ceiling:
// it is what makes the truncation note a measured number rather than a guess.
func countRoundRows(r Round, dims Dims) int {
	var n int
	// countCarrying / countContent are the SAME two lay-outs roundRows appends,
	// measured instead of appended: the truncation note is only honest if it
	// counts the rows the layout would really produce (and the two now produce
	// different rows for the same text — a carried mark versus an inset).
	countCarrying := func(text string) {
		n += len(wrapPrefixed(text, "", roundResultLabel, dims.Cols))
	}
	countContent := func(text string) { n += len(contentLines(text, dims.Cols)) }
	if s := strings.TrimSpace(r.Opening); s != "" {
		countCarrying(roundPromptLabel + s)
	}
	if s := strings.TrimSpace(r.Prompt); s != "" {
		countCarrying(roundPromptLabel + s)
	}
	for _, h := range r.HarnessPrompts {
		if s := strings.TrimSpace(h); s != "" {
			countCarrying(roundHarnessLabel + roundIndent + s)
		}
	}
	for _, th := range r.Thinking {
		if s := strings.TrimSpace(th); s != "" {
			countCarrying(roundThinkingLabel)
			countContent(s)
		}
	}
	for _, t := range r.Text {
		if s := strings.TrimSpace(t); s != "" {
			countCarrying(roundTextLabel)
			countContent(s)
		}
	}
	for _, c := range r.Calls {
		countCarrying(roundToolLabel + toolCallLine(c, dims))
		if res := strings.TrimSpace(c.Result); res != "" {
			countCarrying(roundResultLabel)
			countContent(res)
		} else {
			countCarrying(roundResultLabel + roundNoResultLabel)
		}
	}
	return n
}

// toolCallLine renders one tool call's head: the tool's name and a spelling of
// its input.
//
// TWO SPELLINGS, ONE ADDRESS SPACE. The bounded one (a cut at
// roundToolInputMaxRune + the printed `…(+N runes)` note) is the DEFAULT: it is
// what fits a narrow terminal and what every existing view prints. The FULL one
// is the debug view — `gopi session replay --full` — and it drops only the CUT,
// never a field and never the order. Which one a run of rows wears is decided by
// the lay-out (dims.FullToolInput), so a re-laid material cannot hold rows from
// one spelling under a header claiming the other.
func toolCallLine(c ToolCallView, dims Dims) string {
	input := compactToolInput(c.InputRaw)
	if dims.FullToolInput {
		input = fullToolInput(c.InputRaw)
	}
	if input == "" {
		return c.Name
	}
	return c.Name + "(" + input + ")"
}

// ToolInputMaxRune is the bound the DEFAULT spelling puts on a tool call's
// input, in runes. It is exported so the surface that ADVERTISES the bound
// (`gopi session replay -h`) reads the number the code enforces instead of
// typing a second copy of it (Rules 1/13) — a page saying "cut at 160" over a
// renderer cutting at another number is the drift this accessor forbids.
func ToolInputMaxRune() int { return roundToolInputMaxRune }

// compactToolInput renders a tool_use input as "k=v k=v" in the order the JSON
// held it, bounded to roundToolInputMaxRune runes.
func compactToolInput(raw string) string {
	return boundRunes(spellToolInput(raw), roundToolInputMaxRune)
}

// fullToolInput is the SAME spelling without the cap: the recorded input's whole
// text, which is what a debug view must show. It exists so the two spellings
// cannot drift — a second renderer would be a second answer to "how is an input
// spelled", and the bounded one would then be compared against a spelling
// nobody reads.
func fullToolInput(raw string) string {
	return spellToolInput(raw)
}

// spellToolInput renders a tool_use input as "k=v k=v", UNBOUNDED, in the order
// the JSON held it.
//
// THE ORDER IS PRESERVED BY decoding into an ordered pair list rather than a
// map: a bash call's command and a memory search's query read very differently
// when their fields are reordered, and the log's own order is the one the model
// sent.
func spellToolInput(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == nullJSONWord {
		return ""
	}
	fields, err := orderedObjectFields(raw)
	if err != nil {
		return strings.Join(strings.Fields(raw), " ")
	}
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		parts = append(parts, f.Key+"="+strings.Join(strings.Fields(f.Value), " "))
	}
	return strings.Join(parts, " ")
}

// nullJSONWord is what a JSON null decodes to, as text: the log writes a
// no-argument call that way, and it carries no field to show.
const nullJSONWord = "null"

// orderedField is one key/value pair of a tool input, in the ORDER the JSON
// carried it. Value is already spelled compactly (a string unquoted, anything
// else as its own JSON text).
type orderedField struct {
	Key   string
	Value string
}

// orderedObjectFields decodes a tool input object into ordered fields, walking
// the JSON with a decoder's Token stream rather than unmarshalling into a map:
// Go maps have no order, and the field order is the log's — which is the order
// the model sent.
func orderedObjectFields(raw string) ([]orderedField, error) {
	dec := json.NewDecoder(strings.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != jsonObjectOpen {
		return nil, errNotAJSONObject
	}
	var fields []orderedField
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := keyTok.(string)
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, err
		}
		fields = append(fields, orderedField{Key: key, Value: spellJSONValue(value)})
	}
	return fields, nil
}

// jsonObjectOpen is the JSON object's opening delimiter. It is named because the
// decoder's delimiter comparison reads better as a word than as a character.
const jsonObjectOpen = '{'

// errNotAJSONObject is returned for an input that is not a JSON object.
var errNotAJSONObject = errors.New("the tool input is not a JSON object")

// spellJSONValue renders one JSON value compactly: a string unquoted, everything
// else as its own JSON text.
func spellJSONValue(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

// boundRunes cuts s to at most n runes and SAYS how many were left out.
func boundRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + fmt.Sprintf(" …(+%d runes)", len(r)-n)
}

// chunkLines splits lines into pages of at most n lines each (the last page may
// be shorter). It never returns a zero-length page for a non-empty input.
func chunkLines(lines []string, n int) [][]string {
	if n <= 0 || len(lines) == 0 {
		return nil
	}
	var pages [][]string
	for i := 0; i < len(lines); i += n {
		end := i + n
		if end > len(lines) {
			end = len(lines)
		}
		pages = append(pages, lines[i:end])
	}
	return pages
}

// fitCells pads or truncates s to exactly cols CELLS (never bytes, never array
// slots): a wide rune costs 2 columns, its CONTINUATION slot costs none (the
// wide rune already painted that column — see isContinuationCell), and an SGR
// sequence costs none.
//
// THE CONTROL-SEQUENCE HALF IS NOT OPTIONAL AND IT IS NOT NEW POLICY. A dump's
// scrollback rows carry the colour the surface painted with, and this function
// used to count those bytes as cells while the WRAP now measures through
// internal/lineedit (where sgr.EscapeSeqLen answers zero cells). The two
// measurements disagreeing is a TEXT-LOSING bug, not a cosmetic one, and it was
// measured the moment the wrap was unified: a row the wrap believed fit was cut
// by this function, and `Tips for getting started` came out as `│` alone in
// `gopi session replay … --turn 8 --full`. So the measure here is the same ONE
// definition — sgr.EscapeSeqLen — and a row is no longer cut to make room for
// bytes that draw nothing.
//
// A truncation stops at the last rune that fits whole, so it can never cut a
// wide rune in half and leave a spurious column behind.
func fitCells(s string, cols int) string {
	return fitCellsLabelled(s, cols, "")
}

// fitReportRow lays a REPORT row out for the canvas: the row this package prints
// to say something about the view (a cut, an omission) rather than to carry
// content. It is fitCells plus ONE property fitCells cannot have — when the row
// does not fit, it is SHORTENED and the shortening is SAID (reportRowCutLabel).
//
// The difference matters because the alternative is the defect it repairs: a
// report row clipped by the canvas edge loses its own conclusion (`are not laid
// out; its tool rows are`) and reads as content that simply stopped. Rule 15 is
// the same rule here — a cut the view performs is a cut the view admits.
//
// The label is dropped only when the canvas cannot hold it at all; a row with no
// room for one cell states nothing either way, so it is returned as fitCells
// would return it (and never padded into a lie about having spoken).
func fitReportRow(s string, cols int) string {
	return fitCellsLabelled(s, cols, reportRowCutLabel)
}

// fitCellsLabelled is fitCells with an optional CUT LABEL: when the text does
// not fit and label is non-empty and the canvas can hold it, the row is cut to
// `cols - label cells` and the label closes it.
func fitCellsLabelled(s string, cols int, label string) string {
	if cols <= 0 {
		return ""
	}
	labelW := cellsOf(label)
	keep := cols
	if label != "" && labelW < cols && cellsOf(s) > cols {
		keep = cols - labelW
	}
	var b strings.Builder
	w := 0
	prevWide := false
	for i := 0; i < len(s); {
		if n := sgr.EscapeSeqLen(s[i:]); n > 0 {
			// Zero cells, and it is CARRIED (never dropped): the colour it sets
			// belongs to the cells that follow, and dropping it would repaint the
			// row in the fallback palette.
			b.WriteString(s[i : i+n])
			i += n
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == continuationCell && prevWide {
			// The second column of the wide rune before it: it paints nothing, so
			// it is dropped rather than paying a cell or a column shift.
			i += size
			prevWide = false
			continue
		}
		cw := runeCells(r)
		if w+cw > keep {
			break
		}
		b.WriteRune(r)
		w += cw
		prevWide = cw > 1
		i += size
	}
	if label != "" && keep != cols {
		b.WriteString(label)
		w += labelW
	}
	if w < cols {
		b.WriteString(strings.Repeat(" ", cols-w))
	}
	return b.String()
}

// isContinuationCell reports whether slot i of a dump grid row is the SECOND
// column of the wide rune before it. internal/lineedit's cell grid stores one
// slot per column this way — the wide rune in its first column, a continuation
// marker in its second (internal/lineedit's own isContinuation is this
// predicate), and the marker paints NOTHING: the wide rune already covered both
// columns. Emitting the marker as well would push every later column on the row
// one to the right, which is exactly the defect this predicate exists to stop
// (measured: a 117-column row of the operator's own dump carries 4 wide runes
// and would render 121 columns wide).
func isContinuationCell(r []rune, i int) bool {
	if i <= 0 || i >= len(r) {
		return false
	}
	if r[i] != continuationCell {
		return false
	}
	return runeCells(r[i-1]) > 1
}

// continuationCell is the rune a wide rune's second column holds.
// internal/lineedit spells it `wideContinuation` (unexported); the VALUE is a
// space and the dump's JSON carries it as one, so it is named here rather than
// re-typed at the comparison (Rule 1b).
const continuationCell = ' '

// roundContentIndentCells is how many CELLS a content line is inset by. It is
// DERIVED from roundIndent rather than restated (Rule 13), and roundIndent is
// two cells — the same allowance internal/lineedit's tool mark occupies
// (`⚙ `/`↳ ` are two cells), so the inset costs the content column nothing that
// the markers do not already cost, and a row never moves by a partial cell.
func roundContentIndentCells() int {
	w := 0
	for _, r := range roundIndent {
		w += runeCells(r)
	}
	return w
}

// contentLines wraps ONE content block — a prompt, a reasoning block, the prose,
// a tool call's result — for a canvas of `cols` cells and INSETS EVERY LINE it
// produces by roundContentIndentCells.
//
// WHY EVERY LINE, and this is the whole point (measured 2026-10-26, task
// 01a178ed): a tool CALL row opens with internal/lineedit's `⚙ ` — the marker an
// operator greps for to count what a turn did — but a turn's CONTENT may itself
// be a chat transcript the operator pasted back, or a file the turn read and
// printed, and such content carries lines opening with `⚙ ` too. Column 0 then
// carried two different things under one spelling: `session replay <id> --turn
// 50 | grep -c '^⚙ '` printed 9 over a header reading `calls=7`, and 14 for
// `--turn 62` (also `calls=7`), so the cheap ruler disagreed with the header it
// was there to check. Inseting EVERY produced line — the wrap continuations AND
// the rows an embedded newline made, since the wrap splits on "\n" first —
// leaves `^⚙ ` able to be only a real call row.
//
// The inset belongs to the BLOCK, not to each wrapped row's own text (a row that
// happened to start with a space would otherwise read as one cell further in),
// and it is taken OUT of the wrap width, so no row exceeds the canvas.
//
// IT IS wrapPrefixed WITH THE SAME MARK ON BOTH SIDES, and that is not a
// coincidence to be "cleaned up" later: the inset is the block's mark, the block
// owns every row it produces, so the first row and its continuations wear the
// same two cells. Routing through wrapPrefixed is what makes this lay-out share
// the ONE break policy (internal/lineedit.WrapToCols) instead of holding a
// second one — the defect this delegation removed: a content row used to be
// hard-chopped at the cell edge while the live screen broke the same text at a
// word, so the replay printed `eader calls=8` and `sessi…` where the chat had
// `header` and `session`.
//
// A DEGENERATE CANVAS LOSES NO TEXT: a canvas narrower than the inset itself
// drops the inset and wraps at the full canvas (wrapPrefixed's Rule-15 arm), so
// the old shape — two cells of inset then fitCells trimming them off every row —
// can no longer eat a character per row.
func contentLines(bare string, cols int) []string {
	if cols <= 0 {
		return nil
	}
	return wrapPrefixed(bare, roundIndent, roundIndent, cols)
}

// runeCells is a rune's width in terminal CELLS. It is go-runewidth's answer
// (the dependency this repo already carries, and the same one internal/uiclient
// wraps with), asked per rune — the dump's grid stores one rune per cell with a
// continuation cell after a wide one, so a renderer that walked BYTES or counted
// RUNES would shift every later column on a CJK row.
func runeCells(r rune) int {
	if r == utf8.RuneError {
		return 1
	}
	w := runewidth.RuneWidth(r)
	if w < 0 {
		return 0
	}
	return w
}
