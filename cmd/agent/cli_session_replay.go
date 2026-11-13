package main

// cli_session_replay.go — `gopi session replay <session-id>`: the SESSION group's
// PLAYBACK sub-verb. It paints a recorded session back onto a terminal as the
// SCREEN it was — the dump's cell-exact grid with the truecolour every cell was
// painted in — and lets the viewer drive the transport.
//
// WHERE THIS CAME FROM, and why it is a SUB-VERB and not a verb. The playback was
// written once as the top-level `gopi replay`, which the replay consolidation of
// 2026-10-03 ABOLISHED (devdoc/replay-abolished-verb-single-route.md): the verb
// list must not grow for a reader of a recording, because `gopi log` (the
// recording as text), `gopi raw` (the wire bytes) and `gopi stats` (the numbers)
// are all readers of ONE session id — and the SESSION GROUP is where this binary
// already keeps that family (`gopi session list|show|log|stats|raw|resume`).
// `gopi session -h` is the page that teaches the family together, and
// `gopi session list` prints the very operand every one of them takes.
//
// WHAT IT IS NOT, and the two spellings that must never be confused:
//
//	`gopi chat --replay <id>`   RE-RUNS a recorded conversation against a stub —
//	                            it answers "what happened when the model ran
//	                            again"; the recording is the INPUT.
//	`gopi session replay <id>`  PLAYS the recording back — it answers "what was
//	                            on the screen"; the recording is the MATERIAL.
//
// The top-level `gopi replay` word stays un-dispatched; the regression gate that
// keeps it that way is cmd/agent/session_replay_abolition_test.go.
//
// TWO TARGETS, and the default one is the terminal the operator is standing in:
//
//	(no flag)                paint into the CURRENT tty (works over ssh; we emit
//	                         ANSI and the local terminal renders it)
//	--terminal NAME          start a NEW terminal window (libvte by default)
//	                         running this same sub-verb inside it
//
// The new-window path goes through internal/shot's terminal REGISTRY, which is
// the project's one description of how each emulator is started — including which
// ones cannot be (gnome-terminal's row is a REFUSAL, and this sub-verb prints it
// rather than silently falling back to another emulator; Rule 15).

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nick/gopi/internal/cliverbs"
	"github.com/nick/gopi/internal/gopilog"
	"github.com/nick/gopi/internal/replayplay"
	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/shot"
	"github.com/nick/gopi/internal/tools"
	"github.com/nick/gopi/internal/uiclient"
)

// sessionReplayUsage is the sub-verb's own -h page (reached as
// `gopi session replay -h`). The KEY MAP is spliced in from the package that
// owns the bindings (replayplay.KeyMapText), and the TERMINAL rows from
// internal/shot's registry, so neither a binding nor a usable emulator can be
// advertised here while the code disagrees (Rules 1/13).
//
// It is a plain (non-raw) string because the page names CLI words in backticks,
// which would close a raw literal.
var sessionReplayUsage = sessionReplayUsageHead + "\n" +
	"\n" +
	"A session's own screen dump records the cell grid it drew and the truecolour\n" +
	"every cell was painted with, so it can be drawn BACK — as the screen it was,\n" +
	"not as log text. That is what this sub-verb does, with transport controls.\n" +
	"\n" +
	"Usage:\n" +
	"  " + sessionReplaySynopsis + "                  Play it in THIS terminal\n" +
	"  " + sessionReplaySynopsis + " --terminal NAME  Play it in a NEW terminal window\n" +
	"  gopi " + CliVerbSession + " " + sessionSubcmdReplay + " --branch BRANCH               Play the session the row owning\n" +
	"                                            BRANCH is running (--worktree PATH\n" +
	"                                            takes pwd instead — the same\n" +
	"                                            deliverable selector gopi log,\n" +
	"                                            gopi raw and gopi stats take)\n" +
	"  " + sessionReplaySynopsis + " --turns         List the TURNS (one line per turn), no playing\n" +
	"  " + sessionReplaySynopsis + " --turn N        Print turn N in FULL as plain text, no controls\n" +
	"  " + sessionReplaySynopsis + " --turn N --full  …the debug view: no input cut, no row cut\n" +
	"  " + sessionReplaySynopsis + " --rounds        [internal] List the ROUND sequence, no playing\n" +
	"  " + sessionReplaySynopsis + " --round N       [internal] Render round N as plain text, no controls\n" +
	"  " + sessionReplaySynopsis + " " + sessionReplayVerifyFlag + " OTHER   Compare two screen-stream recordings, frame by frame\n" +
	"  " + sessionReplaySynopsis + " -h              This page\n" +
	"\n" +
	"Options:\n" +
	"  " + sessionReplayFromFlag + " SOURCE    which artifact to draw, one of:\n" +
	"                    (the header line's source= WORD says which one was\n" +
	"                    used: " + replayplay.SourceLabelStream + " is the renderer's own\n" +
	"                    bytes and " + replayplay.SourceLabelDump + " is this program's\n" +
	"                    reduced picture of them — never read one as the other)\n" +
	"                    auto (default)  the VERBATIM screen stream when the session\n" +
	"                    has one (<session>.screen.stream, or the pre-rename\n" +
	"                    .sgr.stream); the dump only when there is none\n" +
	"                    stream          the screen stream, and REFUSE if the session\n" +
	"                    recorded none (never a silent fallback)\n" +
	"                    dump            the dump — this program's INTERPRETATION of\n" +
	"                    the paint bytes (throttled, reduced to grid cells). A\n" +
	"                    playback built this way says so on its own line; a wrong\n" +
	"                    cell in it cannot be blamed on the renderer\n" +
	"  " + sessionReplayVerifyFlag + " OTHER     does not play: compares THIS session's recording with\n" +
	"                    OTHER (a session id, or the path of a screen-stream file)\n" +
	"                    and prints where they first differ — the frame number,\n" +
	"                    the byte offset on each side, and both payloads. Prints\n" +
	"                    `first divergence: none` when they are byte-identical.\n" +
	"                    This is the one check the dump cannot answer (it is\n" +
	"                    throttled) and `gopi session log` cannot either (it\n" +
	"                    reads events, not render bytes)\n" +
	"  --terminal NAME   start a NEW terminal window (NAME from the list below) and\n" +
	"                    play inside it, at the geometry the recording was drawn\n" +
	"                    at. The inner process is this same sub-verb with the same\n" +
	"                    material, so the window's keys drive the same player.\n" +
	"                    Usable terminals: " + shot.FilmTerminalUsableList() + "\n" +
	"                    " + shot.FilmTerminalGnomeRefusal() + "\n" +
	"  --cols N          canvas width for the default target. Default, in this\n" +
	"                    order: the DUMP's own cols (a chat surface recorded the\n" +
	"                    grid it painted, and its replay stays cell-exact on it),\n" +
	"                    else THIS terminal's width (a worker run records no\n" +
	"                    screen, so its log is wrapped to the terminal you are\n" +
	"                    reading it on), else " + strconv.Itoa(replayplay.FallbackCols) + " when no standard descriptor is a\n" +
	"                    terminal at all (a pipe or a redirect). The header line\n" +
	"                    always names which of the three it used, so a wrapped\n" +
	"                    replay never passes for the recorded geometry\n" +
	"  --rows N          canvas height likewise (same three channels, same\n" +
	"                    announcement)\n" +
	"  --turns           print the TURNS — one line per TURN, and no playing.\n" +
	"                    A TURN is one input the person typed or spoke, with\n" +
	"                    every request/response and tool call the harness ran\n" +
	"                    to answer it gathered INSIDE that one turn; each label\n" +
	"                    spells the turn and its detail\n" +
	"                    (\"turn 3/8  (5 request(s), 12 tool call(s))\") and the\n" +
	"                    column beside it is the transport's span of rounds that\n" +
	"                    turn covered (\"rounds 143-152\") — an internal/debug\n" +
	"                    detail, printed so the two units name each other's\n" +
	"                    extent on one line instead of leaving a reader to guess\n" +
	"                    that a turn is not a round number\n" +
	"  --turn N          print turn N in FULL as plain text and exit: the turn's\n" +
	"                    request, its reasoning (thinking, in full), its prose,\n" +
	"                    and EVERY tool call of that turn with its result — the\n" +
	"                    same content the player shows for that turn, without a\n" +
	"                    terminal. 1-based, 0 or last for the last turn, -N from\n" +
	"                    the end; out of range is REFUSED by name, never clamped\n" +
	"  --toolcall N      print the Nth TOOL CALL of the session in full and exit\n" +
	"                    (1-based, in log order; 0 or last for the last, -N from\n" +
	"                    the end). A tool call is an EVENT INSIDE a turn —\n" +
	"                    several can happen in one turn and a turn can carry\n" +
	"                    none — so this is not a turn number: --turns counts\n" +
	"                    turns, this selects calls\n" +
	"\n" +
	"  --full            THE DEBUG VIEW: lift BOTH of this surface's truncations.\n" +
	"                    They are independent, and both are correct defaults.\n" +
	"                    (1) A tool call's recorded INPUT is cut at " + strconv.Itoa(replayplay.ToolInputMaxRune()) + " runes\n" +
	"                    and the row says how many it left out (`…(+N runes)`).\n" +
	"                    With " + sessionReplayFullFlag + " the SAME row carries the recording's\n" +
	"                    whole input: no cut, no note (the field order stays the\n" +
	"                    log's, and `k=v` is how both spellings put it).\n" +
	"                    (2) A turn's BODY is capped by a ROW CEILING shared by\n" +
	"                    all of its rounds — a cut round prints `(N row(s) of\n" +
	"                    this turn's context are not laid out; its tool rows\n" +
	"                    are)`. On a canvas too narrow to hold that sentence\n" +
	"                    the row is SHORTENED and closed with" + replayplay.ReportRowCutLabel() + "\n" +
	"                    rather than clipped at the edge: a report row NEVER\n" +
	"                    wears `…`, which means the CONTENT was cut. That\n" +
	"                    ceiling exists so one huge tool result cannot make\n" +
	"                    the material unbounded, and the rows it\n" +
	"                    never cuts are the tool calls; with " + sessionReplayFullFlag + " it is\n" +
	"                    LIFTED, so a debug run of a large turn prints no such\n" +
	"                    note at all. Before this, " + sessionReplayFullFlag + " answered only (1),\n" +
	"                    so a --turn N --full of a 211-request turn still\n" +
	"                    reported 19 rows not laid out per round.\n" +
	"                    It applies to the views that print ROWS (--turn N,\n" +
	"                    --round N and the interactive player) and is REFUSED on\n" +
	"                    the list verbs (--turns/--rounds), which print counts\n" +
	"                    with no row to expand and no body to cap\n" +
	"\n" +
	"INTERNAL/DEBUG views (the transport's own numbering, not the operator's):\n" +
	"  --rounds          print the ROUND sequence (number, kind, prompt, how many\n" +
	"                    tool calls, how many rows) and exit. A ROUND is ONE HTTP\n" +
	"                    request/response (one JSONL `request` row). This is a\n" +
	"                    DEBUGGING view of the transport: one turn routinely takes\n" +
	"                    many rounds, so a round count is never a count of the\n" +
	"                    operator's activity and is never the playback's unit. It\n" +
	"                    stays because it is the ADDRESS SPACE `gopi session log\n" +
	"                    <id> N` shares\n" +
	"  --round N         [internal/debug] print ONE round in full as plain text\n" +
	"                    and exit. 1-based, 0 or last for the last round, -N from\n" +
	"                    the end; out of range is REFUSED by name, never clamped.\n" +
	"                    Its first line says this is ONE round of a turn, which\n" +
	"                    turn it belongs to, the ROUNDS that turn covers and the\n" +
	"                    --turn N that prints the whole of it: a round is never a\n" +
	"                    turn, and a turn routinely covers many rounds\n" +
	"\n" +
	"A TURN is the ONE unit of this playback: one input the person typed or spoke,\n" +
	"with EVERYTHING the harness ran to answer it — every HTTP request/response\n" +
	"and every tool call — gathered inside that one turn. That is the whole point:\n" +
	"a person asks once, and how many times the model was called in between (and\n" +
	"whether the harness looped on its own to keep going) is the DETAIL of that one\n" +
	"ask, not another ask. The status line and the label spell it that way —\n" +
	"\"turn 3/8  (5 request(s), 12 tool call(s))\": the turn outside, the inner\n" +
	"quantities in parentheses. A self-driven stretch of work between two operator\n" +
	"rows counts INSIDE the turn it belongs to, never as one of its own.\n" +
	"\n" +
	"A turn's content is its request, its reasoning (thinking, in full), its prose\n" +
	"and its tool calls with their results; the ↑/↓ arrows read the part of a long\n" +
	"turn that does not fit on screen a row at a time, and PgUp/PgDn a page at a\n" +
	"time. The horizontal arrows (←/→) are the turn pair, so every arrow has one\n" +
	"job.\n" +
	"\n" +
	"A ROUND is NOT a playback unit. It is the transport's own numbering, kept\n" +
	"addressable for debugging (`--rounds`, `--round N` — both marked internal in\n" +
	"the options above); it shares its numbering with `gopi session log <id> N`.\n" +
	"The harness's own [system] rows are inside the turn they belong to and drawn\n" +
	"dim: they are the framework talking, not you.\n" +
	"\n" +
	"Keys (in the playing terminal):\n" +
	replayplay.KeyMapText() +
	"\n" +
	"Material, in order of preference:\n" +
	"  1. " + replayplay.DumpJSONName + "\n" +
	"     the surface's own record: a cell-exact grid with per-cell truecolour (the\n" +
	"     colour is copied VERBATIM) plus the scrollback the screen pushed away —\n" +
	"     replayed a screenful at a time, then the painted screen itself.\n" +
	"  2. " + replayplay.SessionJSONLName + "\n" +
	"     the event timeline, when the surface recorded no dump. It has no cells\n" +
	"     and no colour, so the turns are OUR rendering of the events, and the\n" +
	"     playback says so (status line and colour both).\n" +
	"\n" +
	"A session that has neither is REFUSED, naming both paths: this sub-verb never\n" +
	"invents material to have something to play.\n" +
	"\n" +
	"NOT `gopi chat --replay`, which RE-RUNS a recorded conversation against a stub.\n" +
	"This one plays the recording back; the command that produced it is not re-run.\n" +
	"\n" +
	"The terminal is restored on every exit path — quit key, error, ^C or\n" +
	"SIGTERM (raw mode, alternate screen and cursor).\n"

// sessionReplayUsageHead is the usage page's first line. It is a constant so the
// -h page and the CLI contract suite cannot disagree about which page is printed
// (cmd/agent/cli_contracts_test.go fingerprints this exact sentence).
const sessionReplayUsageHead = "gopi session replay — play a recorded session back as a PICTURE."

// sessionReplaySynopsis is how the page spells a full invocation of this
// sub-verb: the group word, the sub-verb and the operand. It is composed from
// the two dispatch-side constants so a rename cannot leave the page teaching a
// command line the binary refuses (Rule 13).
const sessionReplaySynopsis = "gopi " + CliVerbSession + " " + sessionSubcmdReplay + " <session-id>"

// The flag names this sub-verb accepts. Each is spelled once (Rule 1b) and
// compared in ONE place (parseSessionReplayArgs), so the usage page and the
// parser cannot drift.
//
// --rounds/--round N are `gopi session log`'s OWN grammar (`logFlagRounds` is
// that surface's spelling of the plural; its singular is the positional <round>,
// whose value grammar is gopilog.ParseHTTPSelector): this sub-verb accepts the
// SAME two flags so a round means the same round on both surfaces, and it
// resolves them through gopilog's resolver rather than its own arithmetic
// (replayplay.RoundIndexFor).
const (
	sessionReplayTerminalFlag = "--terminal"
	sessionReplayColsFlag     = "--cols"
	sessionReplayRowsFlag     = "--rows"
	sessionReplayRoundsFlag   = "--" + logFlagRounds
	// sessionReplayTurnsFlag lists THE PLAYBACK SEQUENCE — one line per TURN, the
	// unit the player walks through (the operator's ruling, 2026-10-22:
	// sessionevent.CounterUnitTurn). It is a SEPARATE flag from --rounds on
	// purpose, and the reason is a contract, not tidiness: `--rounds` lists the
	// ROUND sequence, and a round is the ADDRESS SPACE `--round N` and `gopi
	// session log <id> N` resolve against. Renaming or re-meaning that listing
	// would silently renumber an address a caller already passes — so the turn
	// listing carries its own name, and the two listings print DIFFERENT
	// sequences that the help page names (the round one marked internal).
	//
	// sessionReplayTurnsRetiredFlag is the name this listing had while it was
	// spelled in the ROUND's vocabulary. It is REFUSED BY NAME, with the new
	// spelling in the message: the user surface has one unit, the TURN, and a
	// retired spelling that quietly worked would keep the word alive in scripts
	// and in muscle memory after the surface stopped offering it.
	sessionReplayTurnsFlag        = "--turns"
	sessionReplayTurnsRetiredFlag = "--steps"
	sessionReplayTurnFlag         = "--turn"
	// sessionReplayRoundFlag is the INTERNAL/DEBUG address: one HTTP
	// request/response, shared with `gopi session log <id> N`. The -h page marks
	// it (and --rounds) as internal/debug so a reader cannot mistake the
	// transport's numbering for the user surface's unit.
	sessionReplayRoundFlag = "--round"
	// sessionReplayToolCallFlag is the SAME address space `gopi session log
	// <id> --toolcall N` takes (the Nth tool call, log order) — a sub-verb does
	// not get to renumber the family's units, so the flag name comes from the
	// log surface's constant and the numbering comes from internal/gopilog.
	sessionReplayToolCallFlag = "--" + logFlagToolCall
	// sessionReplayFullFlag expands every tool call's recorded INPUT: the view
	// then prints the whole recording instead of the bounded spelling with its
	// `…(+N runes)` note. It is the DEBUG spelling and deliberately NOT the
	// default — the compact view is the one that fits a narrow terminal, and the
	// bounded row carries the count it left out, so a reader can tell the two
	// apart (Rule 15). It applies to the two views that print ROWS (--turn N and
	// the interactive player); the list verbs print counts, never an input.
	sessionReplayFullFlag = "--full"
	// sessionReplayFromFlag chooses the DATA PLANE: the verbatim screen stream
	// (the renderer's own bytes) or the dump (this program's interpretation of
	// them). Its default is the whole point of this surface — the verbatim leg
	// wins whenever the session has one — and the flag exists so the other
	// question ("what did THIS build make of the dump?") stays askable.
	sessionReplayFromFlag = "--from"
	// sessionReplayVerifyFlag is the MEASUREMENT this reader exists for: two
	// recordings, frame by frame, byte by byte. `session log` cannot answer it at
	// all (it reads events) and the dump cannot either (it is throttled and
	// reduced), so this flag is the reason a reader should prefer this verb.
	sessionReplayVerifyFlag   = "--verify"
	sessionReplayHelpFlag     = "-h"
	sessionReplayHelpFlagLong = "--help"
)

// sessionReplayDefaultInnerGeometry is the geometry an inner player is told to
// use when the material could not state one (the dump is absent and the timeline
// has no terminal size). It is a DEFAULT, and the inner run announces it.
const sessionReplayDefaultInnerGeometry = "80x24"

// sessionReplayTitleIDLen is how much of the session id a new window's title
// carries.
const sessionReplayTitleIDLen = 8

// sessionReplayFrameHeadRunes bounds the head excerpt in the --frames list.
const sessionReplayFrameHeadRunes = 60

// sessionReplayOptions is the parsed invocation.
type sessionReplayOptions struct {
	sessionID string
	terminal  string
	cols      int
	rows      int
	sel       tools.DeliverableSelector
	rounds    bool
	turns     bool
	roundArg  string
	hasRound  bool
	// turnArg is --turn N: the USER surface's address (1-based, 0/last for the
	// last turn, -N from the end), resolved through replayplay.TurnIndexFor so
	// it can never be answered from the round count.
	turnArg string
	hasTurn bool
	// toolCallSel is --toolcall N: the Nth tool call of the session (a
	// different address space from a round — see cmd/agent/cli_log.go's
	// logFlagToolCall).
	toolCallSel gopilog.HTTPRoundSelector
	// full is --full: spell each tool call's input IN FULL instead of the
	// bounded spelling. It is carried into the LAY-OUT (replayplay.Dims.
	// FullToolInput) rather than into a printer, because the rows are built at
	// load and rebuilt by every re-lay: an option the rows do not carry would
	// be printed once and then lost.
	full bool
	// from is --from: replayplay.StreamFromAuto (the default: the verbatim leg
	// when the session has one), StreamFromStream (require it, refuse otherwise)
	// or StreamFromDump (force the dump). Resolved through ParseStreamFrom, so an
	// unknown word is refused BY NAME.
	from string
	// verify is --verify <path|session-id>: the OTHER recording to compare this
	// one against, frame by frame. It is not a playback — it prints a verdict and
	// exits — so it is mutually exclusive with the views below.
	verify string
	// hasVerify records that --verify was given (an empty operand is refused, so
	// the flag's presence cannot be inferred from a non-empty string).
	hasVerify bool
}

// runSessionReplay is the sub-verb's entry point, called by runSessionGroup with
// the arguments AFTER the sub-verb word.
//
// IT ANSWERS -h ITSELF, like the log/stats/raw verbs, so `gopi session replay
// -h` pages even beside other flags. The family page's promise is that a
// sub-verb's flags are that sub-verb's own (cli_session.go), and this is where
// that promise is kept for this one.
func runSessionReplay(args []string) int {
	for _, a := range args {
		if a == sessionReplayHelpFlag || a == sessionReplayHelpFlagLong {
			fmt.Print(sessionReplayUsage)
			return 0
		}
	}
	opts, err := parseSessionReplayArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n%s", replayplay.PlaybackCommand, err, sessionReplayUsage)
		return 1
	}
	if err := sessionReplayRun(opts); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", replayplay.PlaybackCommand, err)
		return 1
	}
	return 0
}

// parseSessionReplayArgs parses the sub-verb's argv. It refuses unknown flags BY
// NAME rather than ignoring them (an accepted-and-ignored option is a defect).
func parseSessionReplayArgs(args []string) (sessionReplayOptions, error) {
	var opts sessionReplayOptions
	sel, err := sessionSelectorFromArgs(args)
	if err != nil {
		return opts, err
	}
	opts.sel = sel
	var positionals []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--"+sessionBranchFlag || a == "--"+sessionWorktreeFlag ||
			strings.HasPrefix(a, "--"+sessionBranchFlag+"=") || strings.HasPrefix(a, "--"+sessionWorktreeFlag+"="):
			// Consumed by sessionSelectorFromArgs; a value-carrying spelling
			// skips its operand here so it cannot be misread as a positional.
			if (a == "--"+sessionBranchFlag || a == "--"+sessionWorktreeFlag) && i+1 < len(args) {
				i++
			}
		case a == sessionReplayTerminalFlag:
			if i+1 >= len(args) {
				return opts, fmt.Errorf("%s needs a terminal name", sessionReplayTerminalFlag)
			}
			opts.terminal = strings.TrimSpace(args[i+1])
			i++
		case strings.HasPrefix(a, sessionReplayTerminalFlag+"="):
			opts.terminal = strings.TrimSpace(strings.TrimPrefix(a, sessionReplayTerminalFlag+"="))
		case a == sessionReplayColsFlag:
			n, err := sessionReplayIntArg(args, i, sessionReplayColsFlag)
			if err != nil {
				return opts, err
			}
			opts.cols, i = n, i+1
		case a == sessionReplayRowsFlag:
			n, err := sessionReplayIntArg(args, i, sessionReplayRowsFlag)
			if err != nil {
				return opts, err
			}
			opts.rows, i = n, i+1
		case a == sessionReplayRoundsFlag:
			opts.rounds = true
		case a == sessionReplayTurnsRetiredFlag:
			// RETIRED BY NAME (the operator's ruling, 2026-10-25): the user
			// surface has one unit, and it is the TURN. A retired spelling that
			// still worked would keep the word alive in scripts and in muscle
			// memory long after the page stopped offering it, so it is refused
			// with the new spelling in the message — never silently accepted as
			// an alias, never a "did you mean" that then does the old thing.
			return opts, fmt.Errorf("%s is retired: the playback has ONE unit and it is the TURN — use %s to list the turns, or %s N to print one",
				sessionReplayTurnsRetiredFlag, sessionReplayTurnsFlag, sessionReplayTurnFlag)
		case a == sessionReplayFullFlag:
			opts.full = true
		case a == sessionReplayTurnsFlag:
			opts.turns = true
		case a == sessionReplayTurnFlag:
			if i+1 >= len(args) {
				return opts, fmt.Errorf("%s needs a turn: N (1-based), 0 or %s for the last, or -N from the end",
					sessionReplayTurnFlag, gopilog.RoundKeywordLast)
			}
			opts.turnArg, opts.hasTurn, i = args[i+1], true, i+1
		case a == sessionReplayToolCallFlag:
			if i+1 >= len(args) {
				return opts, fmt.Errorf("%s needs a tool call: N (1-based), 0 or %s for the last, or -N from the end",
					sessionReplayToolCallFlag, gopilog.RoundKeywordLast)
			}
			arg := strings.TrimSpace(args[i+1])
			sel, selErr := gopilog.ParseHTTPSelector(arg)
			if selErr != nil || !sel.Detail {
				return opts, fmt.Errorf("%s takes a tool call — a number (1 = the session's first tool call), 0 or %s for the last, or -N from the end; %q names no single call",
					sessionReplayToolCallFlag, gopilog.RoundKeywordLast, arg)
			}
			opts.toolCallSel, i = sel, i+1
		case a == sessionReplayFromFlag:
			if i+1 >= len(args) {
				return opts, fmt.Errorf("%s needs a source: %s, %s or %s",
					sessionReplayFromFlag, replayplay.StreamFromAuto, replayplay.StreamFromStream, replayplay.StreamFromDump)
			}
			from, ferr := replayplay.ParseStreamFrom(args[i+1])
			if ferr != nil {
				return opts, ferr
			}
			opts.from, i = from, i+1
		case strings.HasPrefix(a, sessionReplayFromFlag+"="):
			from, ferr := replayplay.ParseStreamFrom(strings.TrimPrefix(a, sessionReplayFromFlag+"="))
			if ferr != nil {
				return opts, ferr
			}
			opts.from = from
		case a == sessionReplayVerifyFlag:
			if i+1 >= len(args) {
				return opts, fmt.Errorf("%s needs another recording: a <session-id> or the path of a %s file",
					sessionReplayVerifyFlag, uiclient.ScreenStreamSuffix)
			}
			opts.verify, opts.hasVerify, i = strings.TrimSpace(args[i+1]), true, i+1
		case strings.HasPrefix(a, sessionReplayVerifyFlag+"="):
			opts.verify = strings.TrimSpace(strings.TrimPrefix(a, sessionReplayVerifyFlag+"="))
			opts.hasVerify = true
			if opts.verify == "" {
				return opts, fmt.Errorf("%s needs another recording, and %q names none", sessionReplayVerifyFlag, a)
			}
		case a == sessionReplayRoundFlag:
			if i+1 >= len(args) {
				return opts, fmt.Errorf("%s needs a round: N (1-based), 0 or %s for the last, or -N from the end",
					sessionReplayRoundFlag, gopilog.RoundKeywordLast)
			}
			opts.roundArg, opts.hasRound, i = args[i+1], true, i+1
		case strings.HasPrefix(a, "-"):
			return opts, fmt.Errorf("%s is not an option of `%s`", a, replayplay.PlaybackCommand)
		default:
			positionals = append(positionals, a)
		}
	}
	if len(positionals) > 1 {
		return opts, fmt.Errorf("one session at a time: got %d positional inputs (%s)",
			len(positionals), strings.Join(positionals, " "))
	}
	if len(positionals) == 1 {
		opts.sessionID = positionals[0]
	}
	// The selector and the positional name the SAME input: passing both is
	// refused by name, never silently preferred (the shared check, so this
	// sub-verb cannot disagree with log/raw/stats).
	if err := sessionSelectorInputCheck(opts.sel, opts.sessionID, nil); err != nil {
		return opts, err
	}
	if opts.sessionID == "" && opts.sel.Branch == "" && opts.sel.Worktree == "" {
		return opts, fmt.Errorf("which session? pass a <session-id> (see `gopi %s %s --mode chat`), or --%s/--%s",
			CliVerbSession, sessionSubcmdList, sessionBranchFlag, sessionWorktreeFlag)
	}
	if opts.turns && (opts.rounds || opts.hasRound) {
		return opts, fmt.Errorf("%s lists the playback's turns and %s/%s address one round — pass one",
			sessionReplayTurnsFlag, sessionReplayRoundsFlag, sessionReplayRoundFlag)
	}
	if opts.hasTurn && opts.turns {
		return opts, fmt.Errorf("%s N prints ONE turn and %s lists them all — pass one",
			sessionReplayTurnFlag, sessionReplayTurnsFlag)
	}
	if opts.hasTurn && (opts.rounds || opts.hasRound) {
		return opts, fmt.Errorf("%s N and %s/%s are two different address spaces (turns vs rounds) — pass one",
			sessionReplayTurnFlag, sessionReplayRoundFlag, sessionReplayRoundsFlag)
	}
	if opts.rounds && opts.hasRound {
		return opts, fmt.Errorf("%s and %s are two views of the same material — pass one",
			sessionReplayRoundsFlag, sessionReplayRoundFlag)
	}
	// --full expands a TOOL ROW, so it belongs to the views that print rows. The
	// list verbs (--turns/--rounds) print counts and never an input, so accepting
	// the flag there would be an option that does nothing — refused by name
	// instead of silently ignored.
	if opts.full && (opts.turns || opts.rounds) {
		return opts, fmt.Errorf("%s expands a tool call's row, and %s/%s list counts with no row to expand — pass one",
			sessionReplayFullFlag, sessionReplayTurnsFlag, sessionReplayRoundsFlag)
	}
	if opts.from == "" {
		opts.from = replayplay.StreamFromAuto
	}
	// --verify is a MEASUREMENT, not a playback: it prints a verdict and exits, so
	// every view flag is refused beside it by name rather than silently ignored
	// (an accepted-and-ignored option teaches a caller something false).
	if opts.hasVerify && (opts.turns || opts.rounds || opts.hasRound || opts.hasTurn || opts.toolCallSel.Detail || opts.terminal != "") {
		return opts, fmt.Errorf("%s compares two recordings and prints a verdict — it plays nothing, so it takes no view flag (%s/%s/%s/%s/%s)",
			sessionReplayVerifyFlag, sessionReplayTurnsFlag, sessionReplayRoundsFlag, sessionReplayTurnFlag, sessionReplayRoundFlag, sessionReplayTerminalFlag)
	}
	if opts.toolCallSel.Detail && (opts.hasRound || opts.rounds || opts.turns || opts.hasTurn) {
		return opts, fmt.Errorf("%s and %s/%s are two different address spaces (%s vs rounds) — pass one",
			sessionReplayToolCallFlag, sessionReplayRoundFlag, sessionReplayRoundsFlag, sessionevent.CounterUnitToolCall)
	}
	return opts, nil
}

// sessionReplayIntArg reads the integer operand of a flag.
func sessionReplayIntArg(args []string, i int, flag string) (int, error) {
	if i+1 >= len(args) {
		return 0, fmt.Errorf("%s needs a number", flag)
	}
	n, err := strconv.Atoi(args[i+1])
	if err != nil {
		return 0, fmt.Errorf("%s needs a number, got %q", flag, args[i+1])
	}
	return n, nil
}

// sessionReplayRun resolves the session, loads its material and either plays it
// here, starts a terminal window for it, or renders a static view.
func sessionReplayRun(opts sessionReplayOptions) error {
	id := opts.sessionID
	if id == "" {
		resolved, err := sessionIDForSelector(replayplay.PlaybackCommand, opts.sel)
		if err != nil {
			return err
		}
		id = resolved
	}
	// --verify is answered BEFORE any playback material is built: it compares two
	// RECORDINGS, and building a picture first would both cost a full parse and
	// let a dump-only session reach a comparison it has no bytes for.
	if opts.hasVerify {
		return sessionReplayVerify(opts.verify, id)
	}
	mat, err := sessionReplayMaterial(id, opts.from)
	if err != nil {
		return err
	}
	// --full is a property of the LAY-OUT, so it is applied to the material's own
	// Dims and then re-laid: the rows are built at load time, and a flag that only
	// reached a printer would be lost by the first re-lay (--cols/--rows, or the
	// static turn view's own exact-height re-lay).
	//
	// IT LIFTS BOTH CEILINGS — the tool-input spelling AND the row ceiling. An
	// earlier version lifted only the first, so `--turn N --full` on a large turn
	// still printed `(19 row(s) of this turn's context are not laid out; its tool
	// rows are)` on every round: the operator's one debug flag could not obtain
	// the turn's whole picture at any combination of flags (measured 2026-11-01).
	if opts.full {
		mat = replayplay.ExpandForDebug(mat)
	}
	if opts.cols > 0 || opts.rows > 0 {
		// A caller that names a canvas re-lays the rounds out for it (the rows
		// were baked at load time, so a resize has to re-wrap them — otherwise
		// the static view and the player would disagree about where a round's
		// lines break).
		mat = replayplay.Relayout(mat, opts.cols, opts.rows)
	}
	switch {
	case opts.toolCallSel.Detail:
		// A tool call is addressed by the SESSION's call sequence, so the
		// rendering comes from the surface that owns that numbering (the same
		// one `gopi session log <id> --toolcall N` prints) rather than from the
		// playback material, whose unit is the TURN (a tool call is an event
		// INSIDE a turn, so the playback's rows are not that numbering).
		return printSessionReplayToolCall(id, opts.toolCallSel)
	case opts.rounds:
		printSessionReplayRounds(mat)
		sessionReplayDegradeLines(mat)
		return nil
	case opts.turns:
		printSessionReplayTurns(mat)
		return nil
	case opts.hasTurn:
		// A TURN is as tall as it is: the static view lays it out for a canvas
		// exactly as tall as the turn, so nothing of it is clipped and no pager
		// is needed to read the tool calls it carries.
		return printSessionReplayTurn(mat, opts.turnArg)
	case opts.hasRound:
		// A round is as tall as it is: the static view lays it out for a canvas
		// exactly as tall as the round so the whole round is readable, and keeps
		// the caller's width (or the material's own).
		return printSessionReplayRound(mat, opts.roundArg)
	case opts.terminal != "":
		return startSessionReplayTerminal(opts.terminal, mat, id)
	}
	// A PLAYBACK IS THE VIEW THAT CLAIMS A PICTURE, so it is where the degrade
	// sentence belongs: a reader stepping frames is the one who could otherwise
	// mistake a reconstructed screen for the renderer's own. It is silent on a
	// verbatim run (sessionReplayDegradeLines).
	sessionReplayDegradeLines(mat)
	return replayplay.Run(replayplay.RunOptions{Mat: mat, In: os.Stdin, Out: os.Stdout})
}

// sessionReplayMaterial resolves a session ID (or an absolute path) to the
// material to play.
//
// TWO LAYOUTS, ONE RESOLVER. A session's artifacts live under
// `<sessions>/<session-id>/` when it is a bare chat/prompt run (session.jsonl
// beside the dump), and a TASK run's log lives at
// `<sessions>/<task-id>/<session-id>.jsonl` — the directory named by the TASK,
// the FILE by the SESSION (3,931 bare `session.jsonl` files, and every task run
// grew one of the second shape; both measured on this host 2026-10-06). The log
// is therefore resolved through internal/gopilog's FindSession — the tree's ONE
// resolver, the SAME call `gopi session log <id>` makes — so the two readers
// cannot disagree about which file a session id names.
//
// IT IS EXACT: an id that names no session is an ERROR naming what it looked
// for, and a truncated id is refused by the resolver, not resolved to the
// closest match (a prefix names more than one session).
func sessionReplayMaterial(id, from string) (replayplay.Material, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return replayplay.Material{}, fmt.Errorf("no session id was given")
	}
	// An absolute path operand is the session DIRECTORY itself: the same shape
	// `gopi session log` accepts, and what a caller holding a captured session
	// under a scratch root has.
	if filepath.IsAbs(id) {
		st, err := os.Stat(id)
		if err != nil || !st.IsDir() {
			return replayplay.Material{}, fmt.Errorf("%s is not a session directory", id)
		}
		mat, err := replayplay.LoadSessionAtFrom(id, "", from)
		if err != nil {
			return replayplay.Material{}, err
		}
		mat.SessionID = filepath.Base(filepath.Clean(id))
		return mat, nil
	}
	sessions, err := gopilog.ResolveSessionDir()
	if err != nil {
		return replayplay.Material{}, err
	}
	logPath, err := gopilog.FindSession(sessions, id)
	if err != nil {
		return replayplay.Material{}, err
	}
	dir := filepath.Join(sessions, id)
	// The resolver answered "" with no error: no record in this tree is named by
	// that id. The error the loader raises then must NAME a real path, so the
	// fallback is the bare session layout's own name rather than an empty string
	// (an empty one would quietly look beside the dump and report the wrong
	// file — Rule 15).
	if logPath == "" {
		// Nothing in the tree records that id. When the id names no directory
		// either, the operand simply names no session — say so by name (the
		// refusal a mistyped id deserves) rather than letting the loader report
		// two absent FILES for an operand that was never a session.
		if st, statErr := os.Stat(dir); statErr != nil || !st.IsDir() {
			return replayplay.Material{}, fmt.Errorf("no session %s under %s — list the chat sessions with `gopi %s %s --mode chat`",
				id, sessions, CliVerbSession, sessionSubcmdList)
		}
		logPath = filepath.Join(dir, replayplay.SessionJSONLName)
	}
	mat, err := replayplay.LoadSessionAtFrom(dir, logPath, from)
	if err != nil {
		return replayplay.Material{}, err
	}
	mat.SessionID = id
	return mat, nil
}

// printSessionReplayToolCall renders one tool call of the session, through the
// SAME renderer and numbering `gopi session log <id> --toolcall N` uses
// (internal/gopilog.FormatSessionToolCall / SessionToolCalls) — a sub-verb that
// re-implemented the numbering would be a fifth answer to "which one is call 3".
func printSessionReplayToolCall(sessionID string, sel gopilog.HTTPRoundSelector) error {
	path, err := gopilog.FindSessionPathForID(sessionID)
	if err != nil {
		return err
	}
	s, err := gopilog.ParseFile(path)
	if err != nil {
		return err
	}
	calls := gopilog.SessionToolCalls(s)
	n, ok := gopilog.SessionToolCallIndex(s, sel)
	if !ok {
		return fmt.Errorf("tool call %d does not exist: session %s recorded %d %s",
			sel.Round, s.ID, len(calls), sessionevent.CounterUnitToolCall)
	}
	fmt.Print(gopilog.FormatSessionToolCall(s, calls[n], len(calls)))
	return nil
}

// printSessionReplayRounds prints the ROUND sequence without playing it — the
// INTERNAL/DEBUG view, and it says so in its own output rather than relying on a
// reader to remember the -h page.
//
// A ROUND is one JSONL `request` row / one HTTP request-response: the transport's
// unit. The USER-FACING unit of this playback is the TURN, and only the turn
// (the operator's ruling, 2026-10-25) — one operator row to the next, however
// many rounds and tool calls the harness ran to answer it. So this listing keeps
// the rounds it exists to tabulate (it shares the numbering `gopi session log
// <id> N` resolves against) and labels itself internal; the turn listing does
// not count rounds and does not print a round total.
//
// The dump frames are a THIRD material altogether (a screen is neither a turn nor
// a round: it has no prompt, no reasoning and no tool calls), counted under
// their own name.
func printSessionReplayRounds(mat replayplay.Material) {
	// THE COUNT IS NAMED FOR WHAT IT IS. This listing walks the material's TURN
	// SPANS — each one opened at a row the person typed — so its length is the
	// `human turns` subset, NOT the session's turn count (`chats.turn_count`,
	// which also charges the turns an external notify or the model's own
	// `chat_surface type` opened). Printing that length under the bare word is
	// how 66 came to sit beside 200 on one machine (measured on
	// 41e5dade-71e1-4efc-a012-456d0532e762), so the word is the subset's own
	// (the operator's ruling, 2026-10-27) and the session's count is named as
	// belonging to another surface.
	fmt.Printf("[internal] session %s  source=%s  geometry=%dx%d (%s)  %s=%d  dump frames=%d\n",
		mat.SessionID, mat.SourceLabel, mat.Cols, mat.Rows, mat.GeometrySource,
		sessionevent.CounterUnitHumanTurn, mat.TurnCount, mat.DumpFrameCount())
	fmt.Printf("  %s\n", sessionReplayRoundsInternalNote)
	fmt.Printf("  %s=%d  — %s\n", sessionevent.CounterUnitHumanTurn, mat.TurnCount, gopilog.HumanTurnUnitWords())
	fmt.Printf("    the session's TURN count is the LEDGER's (`gopi session show <id>`) — %s\n",
		sessionevent.CounterUnitTurnIsNot)
	if mat.PromptTurns > 0 {
		fmt.Printf("  turn 1 is the session's OPENING request (the log recorded no round of its own)\n")
	}
	fmt.Printf("  rounds=%d  the ROUND (one HTTP request/response) — INTERNAL/DEBUG: the transport's unit, shared with `gopi %s log <id> N`\n",
		mat.RoundCount, CliVerbSession)
	// THE TWO COLUMNS ARE NAMED, because the defect this listing carried was two
	// units printed side by side with neither saying which it was: the ordinal
	// on the left was a row count and the label's "round 152/152" was the
	// transport's number. The header names them (TURN | ROUND) and the label
	// column says WHICH TURN the round belongs to, so a reader can see the ten
	// rounds of one turn as ten adjacent rows.
	turnOf := make(map[int]int, len(mat.Rounds))
	for _, t := range mat.Turns {
		if t.Kind != replayplay.RoundTurn || t.FirstRound <= 0 {
			continue
		}
		for i := t.FirstRound; i <= t.LastRound; i++ {
			if _, dup := turnOf[i]; !dup {
				turnOf[i] = t.Index
			}
		}
	}
	fmt.Printf("%*s  %-7s %-16s %-5s  calls rows  prompt\n",
		sessionReplayRowWidth(mat.RoundCount), sessionReplayRoundOrdinalColumn, sessionReplayKindColumn,
		sessionReplayLabelColumn, sessionReplayTurnOfColumn)
	for _, r := range mat.Rounds {
		turnCol := sessionReplayNoTurnColumn
		if t, ok := turnOf[r.Index]; ok {
			turnCol = strconv.Itoa(t)
		}
		fmt.Printf("%*d  %-7s %-16s %-5s calls=%d rows=%d  prompt=%q\n",
			sessionReplayRowWidth(mat.RoundCount), r.Index, r.Kind, r.Label, turnCol,
			len(r.Calls), len(r.Rows), sessionReplayHead(r.Prompt))
	}
	for _, f := range mat.DumpFrames {
		fmt.Printf("%4s  %-10s %-18s rows=%d  first=%q\n", "-", f.Kind, f.Label, len(f.Rows), sessionReplayFrameHead(f))
	}
}

// The internal round listing's column heads. They are constants because the
// header and the rows must agree about which column carries which unit — the
// defect this listing had was two units printed side by side with neither one
// named (Rule 13).
const (
	// sessionReplayRoundOrdinalColumn heads the ROUND's own number — the number
	// `--round N` and `gopi session log <id> N` resolve.
	sessionReplayRoundOrdinalColumn = "round"
	// sessionReplayKindColumn heads the round's kind.
	sessionReplayKindColumn = "kind"
	// sessionReplayLabelColumn heads the round's own label ("round 152/152").
	sessionReplayLabelColumn = "label"
	// sessionReplayTurnOfColumn heads the TURN this round belongs to — the
	// column that makes a turn's ten rounds read as ten adjacent rows.
	sessionReplayTurnOfColumn = "turn"
	// sessionReplayNoTurnColumn marks a round no turn span covers.
	sessionReplayNoTurnColumn = "-"
)

// sessionReplayRoundsInternalMark is the heading the -h page puts over the round
// views. It is a constant so the page and the check that every round paragraph
// is marked internal cannot disagree about the wording (Rule 13).
const sessionReplayRoundsInternalMark = "INTERNAL/DEBUG views"

// sessionReplayRoundsInternalNote is the one sentence the internal round view
// prints about itself. It is a constant so the listing and the -h page cannot
// tell a reader two different things about who owns a round.
const sessionReplayRoundsInternalNote = "the ROUND is INTERNAL/DEBUG: one turn routinely takes many, so a round count is never a count of the operator's own activity"

// printSessionReplayTurns lists THE PLAYBACK SEQUENCE — one line per TURN, the
// unit the player walks through (the operator's ruling, 2026-10-22).
//
// It prints the TURN count and nothing else as a headline: `turns=` IS the
// length of the playback (Material.PlayableUnits), so there is no second number
// beside it for a reader to confuse with it. A material that recorded no turn (a
// dump-only replay) has no turn sequence of its own; its screen frames are what
// the player shows, and the line says exactly that rather than printing an empty
// list (Rule 15).
func printSessionReplayTurns(mat replayplay.Material) {
	fmt.Printf("session %s  source=%s  geometry=%dx%d (%s)  turns=%d\n",
		mat.SessionID, mat.SourceLabel, mat.Cols, mat.Rows, mat.GeometrySource,
		len(mat.Turns))
	fmt.Printf("  turns=%d  the TURN — %s\n", len(mat.Turns), gopilog.TurnUnitWords())
	fmt.Printf("  artifacts=%s%s\n", mat.Source, sessionReplayStreamNote(mat))
	// THE LEDGER'S COMPLETED COUNT, UNDER ITS OWN WORD — printed ONLY when it
	// differs from the playback length above, which is the OPENED count.
	//
	// This line used to be a two-axes essay, because `turns=` and the ledger
	// disagreed and nothing said why. The bare word now carries one quantity on
	// every surface (epic 2026-11-04, measured on session
	// 3a93ac1c-c0a3-448d-a14f-9eb673b64f9f: `session list` said 29 while this
	// playback held 30 rows), so a sentence accounting for two bare totals is no
	// longer needed. What remains is the fact a reader may still want — how many
	// of those turns the ledger CHARGED — stated as a number under its own word,
	// and silent when the two agree (Rule 15: the gap in numbers, never noise on
	// a session that has none).
	if ledger, ok, _ := gopilog.LedgerTurnCountFor(mat.SessionID); ok && ledger != len(mat.Turns) {
		fmt.Printf("  %s=%d  — %s\n", sessionevent.CounterUnitTurnCompleted, ledger,
			sessionevent.CounterUnitTurnCompletedIsNot)
	}
	if len(mat.Turns) == 0 {
		fmt.Printf("  this material recorded no turn boundary, so the player walks its dump's screen frames (%d) — a screen is not a turn\n",
			mat.DumpFrameCount())
	} else {
		fmt.Printf("  the numbers inside each label are the turn's DETAIL (%s) — never a second turn count\n",
			gopilog.SessionRoundUnitWords())
	}
	// THE ROW NUMBER IS THE TURN, AND THE COLUMN BESIDE IT NAMES THE ROUNDS it
	// covers: an operator who reads "11" here and then asks for the turn as
	// `--round 152` gets ONE round of the ten, because the two units are
	// adjacent numbers that never met on one line. The width comes from the row
	// count, so a three-digit session's numbers line up (Rule 1: the cap is a
	// constant, the width is measured).
	width := sessionReplayRowWidth(len(mat.Turns))
	for _, t := range mat.Turns {
		// The kind column is dropped for a TURN row: its label already opens with
		// the word, and printing "turn" twice on one line reads as two facts (a
		// kind and a position) where there is one.
		kind := string(t.Kind)
		if t.Kind == replayplay.RoundTurn {
			kind = ""
		}
		fmt.Printf("%*d  %-7s %-46s %-24s calls=%d rows=%d  prompt=%q\n",
			width, t.Index, kind, t.Label, sessionReplayRowRoundSpan(t),
			len(t.Calls), len(t.Rows), sessionReplayHead(t.Prompt))
	}
}

// sessionReplayRowRoundSpan is the ROUND interval a listing row covers.
//
// A TURN row carries the span its own turn covered (Material's FirstRound /
// LastRound, from gopilog's ONE turn-boundary decision). A row that is not a turn
// — a material whose log recorded no operator row falls back to its ROUND
// sequence as the playback's steps — is exactly ONE round, so its interval is
// itself; saying "no rounds inside this turn" there would be false (it IS a
// round), and saying nothing would leave the column blank on every row.
func sessionReplayRowRoundSpan(t replayplay.Round) string {
	if t.Kind == replayplay.RoundTurn {
		return gopilog.FormatTurnRoundRange(t.FirstRound, t.LastRound)
	}
	if t.Index > 0 {
		return gopilog.FormatTurnRoundRange(t.Index, t.Index)
	}
	return gopilog.FormatTurnRoundRange(0, 0)
}

// sessionReplayRowWidth is how wide a listing's leading ordinal is printed:
// every row's number is padded to the width of the LAST one, so the columns to
// its right line up on a 3-digit session exactly as on a 2-digit one. The floor
// keeps a 1-digit session's table from starting at the left edge.
func sessionReplayRowWidth(count int) int {
	w := len(strconv.Itoa(count))
	if w < sessionReplayRowWidthMin {
		w = sessionReplayRowWidthMin
	}
	// The column head is the unit's own word, so it must fit in the same cell the
	// numbers are right-aligned in — otherwise the header and its rows disagree
	// about where the column is (measured: `round  kind` above `   1  round`).
	if head := len(sessionReplayRoundOrdinalColumn); w < head {
		w = head
	}
	return w
}

// sessionReplayRowWidthMin is the narrowest leading-ordinal column the static
// listings print (Rule 1).
const sessionReplayRowWidthMin = 4

// printSessionReplayTurn prints ONE turn in FULL as PLAIN text (no escapes): the
// static view of the operator surface's ONE unit, for a caller with no terminal.
// It is the same content the player draws for that turn — the request, the
// reasoning (thinking, in full), the prose, and EVERY tool call of the turn with
// its result — laid out for a canvas exactly as tall as the turn, so no row of it
// is clipped and no pager is needed to read its tool calls.
//
// The content comes from Material.Turns (the TURN sequence) and never from the
// round sequence: a turn containing five rounds must print all five rounds'
// tool calls as ONE turn, which is precisely what addressing a round cannot do.
func printSessionReplayTurn(mat replayplay.Material, arg string) error {
	n, err := replayplay.TurnIndexFor(mat, arg)
	if err != nil {
		return err
	}
	turn := mat.Turns[n-1]
	// The label ALREADY opens with "turn k/N" and carries the detail in
	// parentheses, so the header adds the one fact the label does not name: how
	// many tool calls this turn's rows are about to print.
	fmt.Printf("%s  calls=%d\n", turn.Label, len(turn.Calls))
	// THE BODY IS THE TURN'S ROUNDS IN THE ORDER THEY RAN, each under a header
	// naming it (replayplay.TurnBodyRows). Round-by-round is the whole point: a
	// turn is many rounds, and a lay-out that concatenated every round's harness
	// rows, then every round's prose, then every round's calls printed this
	// turn's eight tool calls behind 170 rows of other rounds' prose — and on a
	// round that spent the row ceiling on harness ticks, never printed them at
	// all (measured 2026-10-26: session 0945c8bd turn 2, `calls=69`, zero call
	// rows).
	for _, row := range sessionReplayTurnRows(mat, turn) {
		fmt.Println(strings.TrimRight(row.Cells, " "))
	}
	return nil
}

// sessionReplayTurnRows lays a turn's static body out — WITH the lay-out the
// material was loaded under, both its parts: the tool-input spelling
// (Dims.FullToolInput) and the row ceiling (Dims.FullLayout).
//
// The lay-out is read off the MATERIAL (its Dims), never off a second flag
// carried beside it: the rows already baked in the material were laid out with
// it, so a body re-laid here that re-derived the lay-out from ONE of the two
// properties would print a `--full` turn with its inputs expanded and its context
// still clipped — the exact half-honoured flag this epic repairs. The default is
// the bounded spelling and the bounded body, unchanged.
func sessionReplayTurnRows(mat replayplay.Material, turn replayplay.Round) []replayplay.FrameRow {
	return replayplay.TurnBodyRowsAt(turn, mat.Dims)
}

// printSessionReplayRound prints ONE round in FULL as PLAIN text (no escapes):
// the INTERNAL/DEBUG static view, for a caller with no terminal. It is
// deliberately not the player's rendering, so reading a round never requires
// interpreting ANSI, and its first line says it is the internal address space.
func printSessionReplayRound(mat replayplay.Material, arg string) error {
	n, err := replayplay.RoundIndexFor(mat, arg)
	if err != nil {
		return err
	}
	if len(mat.Rounds) == 0 {
		return fmt.Errorf("this material has no rounds to render (source=%s): it recorded no operator row either, so its content is the dump's screen frames — list them with `%s %s --turns`",
			mat.Source, replayplay.PlaybackCommand, sessionReplayTurnsFlag)
	}
	if n < 1 || n > len(mat.Rounds) {
		return fmt.Errorf("round %d is out of range: this material has %d round(s) to render (the session has %d round(s) in total)",
			n, len(mat.Rounds), mat.RoundCount)
	}
	full := replayplay.Relayout(mat, mat.Cols, len(mat.Rounds[n-1].Rows)+1)
	r := full.Rounds[n-1]
	// THE FIRST LINE A READER NEEDS: this is ONE round, not a turn. The address
	// `--round 152` reaches one of the ten rounds "turn 11" covers, and its eight
	// tool calls live in the other nine — so before printing a byte of the round
	// the view says which turn it belongs to, the interval that turn covers, and
	// the flag that prints the whole thing. It goes FIRST because a reader who
	// skims the header and stops has already asked for the wrong unit.
	if turn, first, last, total, ok := replayplay.RoundTurnOf(mat, r.Index); ok {
		fmt.Printf("%s\n", fmt.Sprintf(sessionevent.CounterRoundIsOneSpotFmt,
			turn, total, gopilog.FormatTurnRoundRange(first, last), sessionReplayTurnFlag, turn))
	} else {
		fmt.Printf("%s\n", sessionevent.CounterRoundNoTurn)
	}
	fmt.Printf("[internal] round %d/%d  kind=%s  label=%s  calls=%d  rows=%d\n", r.Index, mat.RoundCount, r.Kind, r.Label, len(r.Calls), len(r.Rows))
	for _, row := range r.Rows {
		fmt.Println(strings.TrimRight(row.Cells, " "))
	}
	return nil
}

// sessionReplayHead is a one-line excerpt of a round's prompt for the list view.
func sessionReplayHead(s string) string {
	return truncateSessionReplayHead(strings.Join(strings.Fields(s), " "), sessionReplayFrameHeadRunes)
}

// sessionReplayFrameHead is the first non-blank row of a frame, for the --frames
// list.
func sessionReplayFrameHead(f replayplay.Frame) string {
	for _, row := range f.Rows {
		if s := strings.TrimSpace(row.Cells); s != "" {
			return truncateSessionReplayHead(s, sessionReplayFrameHeadRunes)
		}
	}
	return ""
}

// truncateSessionReplayHead cuts s to at most n runes (the excerpt in the frame
// list).
func truncateSessionReplayHead(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// startSessionReplayTerminal starts a NEW terminal window running this same
// sub-verb, at the geometry the recording was drawn at.
//
// The terminal's identity is resolved through internal/shot's REGISTRY — the
// project's one table of how each emulator is started, and the one place that
// says which cannot be. A refused terminal (gnome-terminal) is refused HERE,
// loudly, with the registry's own reason: never a silent fallback to another
// emulator (Rule 15).
func startSessionReplayTerminal(name string, mat replayplay.Material, id string) error {
	spec, err := shot.FilmTerminalSpecByName(name)
	if err != nil {
		return err
	}
	geometry := sessionReplayInnerGeometry(mat)
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve this binary to start a terminal for it: %w", err)
	}
	// The inner process is THIS binary invoked with the same sub-verb, so the
	// window's keys reach the same player (and the same material): one spelling
	// of the invocation, in one argv.
	argv := []string{
		self, CliVerbSession, sessionSubcmdReplay,
		id, sessionReplayColsFlag, strconv.Itoa(mat.Cols), sessionReplayRowsFlag, strconv.Itoa(mat.Rows),
	}
	title := sessionSubcmdReplay + " " + shortSessionReplayID(mat.SessionID)
	args, err := spec.TerminalArgs(geometry, argv, title, "")
	if err != nil {
		return err
	}
	exe, err := exec.LookPath(spec.Binary)
	if err != nil {
		return fmt.Errorf("the terminal %s is NOT INSTALLED: no executable named %s on PATH — usable terminals are %s",
			spec.Name, spec.Binary, shot.FilmTerminalUsableList())
	}
	cmd := exec.Command(exe, args...)
	// The new terminal is the operator's: it inherits the display and the
	// environment, and this process does not wait for it (the window outlives
	// the invocation that opened it).
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", spec.Name, err)
	}
	fmt.Printf("%s: playing session %s in a new %s window at %s (pid %d)\n",
		replayplay.PlaybackCommand, mat.SessionID, spec.Name, geometry, cmd.Process.Pid)
	return nil
}

// sessionReplayInnerGeometry is the WxH an inner player is started at: the
// material's own geometry when the recording stated one, else the default —
// announced, never presented as the recording's (Rule 15).
func sessionReplayInnerGeometry(mat replayplay.Material) string {
	if mat.Cols > 0 && mat.Rows > 0 && mat.GeometrySource != replayplay.GeometryFromFallback {
		return strconv.Itoa(mat.Cols) + "x" + strconv.Itoa(mat.Rows)
	}
	return sessionReplayDefaultInnerGeometry
}

// shortSessionReplayID abbreviates a session id for a window title.
func shortSessionReplayID(id string) string {
	if len(id) <= sessionReplayTitleIDLen {
		return id
	}
	return id[:sessionReplayTitleIDLen]
}

// sessionReplayStreamNote is the clause that rides beside `artifacts=` when the
// verbatim leg was passed over OR when a reading of it could not be taken. It is
// empty when there was nothing to pass over (a session that recorded no screen at
// all), because that is not a DEGRADE — it is the ordinary shape of a worker run;
// the degrade is announced only when a recording existed and was not used, or was
// used with one of its provenance questions unanswered (Rule 15).
func sessionReplayStreamNote(mat replayplay.Material) string {
	note := ""
	if mat.StreamNote != "" {
		note = "  (verbatim leg skipped: " + mat.StreamNote + ")"
	}
	return note + sessionReplayHeaderReadingNote(mat)
}

// sessionReplayHeaderReadingNote reports a screen stream whose HEADER LINE could
// not be read. The stream's frames are steppable either way, so the picture is
// correct — which is exactly why the gap has to be said out loud: the legacy-spelling
// reading is a BOOL, and a `false` produced by a read that never happened would look
// identical to "this recording was written by the current build". It is empty when
// the reading was taken, and empty when the material carries no screen stream at all.
func sessionReplayHeaderReadingNote(mat replayplay.Material) string {
	if mat.Stream == nil || mat.Stream.HeaderUnreadable == "" {
		return ""
	}
	return "  (header reading unavailable, so the legacy-spelling answer is UNKNOWN not `no`: " +
		mat.Stream.HeaderUnreadable + ")"
}

// sessionReplayDegradeLines prints the SELF-REPORTED DEGRADE, on any view that
// claims to show a picture. Rule 15 is the whole reason: a reader who cannot tell
// a verbatim replay from a reconstructed one will trust the wrong one, and the
// difference is precisely what this mechanism exists to measure. It is silent
// when the material is verbatim — noise on a good run is its own defect.
func sessionReplayDegradeLines(mat replayplay.Material) {
	if mat.SourceLabel == replayplay.SourceLabelStream {
		return
	}
	fmt.Printf("  DEGRADED: source=%s — the frames were rebuilt from the dump, which is this\n",
		mat.SourceLabel)
	fmt.Printf("  program's INTERPRETATION of the paint bytes (throttled and reduced to grid\n")
	fmt.Printf("  cells). No screen stream was used, so a wrong cell cannot be blamed on the\n")
	fmt.Printf("  renderer from this picture: record one with %s and replay again.\n",
		cliverbs.ScreenStreamEnv)
}

// sessionReplayVerify is `--verify <other>`: TWO RECORDINGS, FRAME BY FRAME.
//
// THIS IS THE MEASURABLE REASON THIS VERB EXISTS. `gopi session log` reads events
// and cannot answer "are the bytes the renderer wrote identical?"; the dump cannot
// either (it is throttled by dumpMinInterval and reduced to the cells the grid
// model expresses). Two screen streams are the only pair that can, and this prints
// the ONE line a reviewer acts on: either `first divergence: none`, or the frame
// number, the byte offset on each side, and both payloads.
//
// The operand is a session id OR a path, and both sides resolve through the SAME
// rule, so `--verify <sid> --verify <path>` is possible without a second spelling.
func sessionReplayVerify(other, selfID string) error {
	selfPath, err := sessionReplayStreamPath(selfID)
	if err != nil {
		return err
	}
	otherPath, err := sessionReplayStreamPath(other)
	if err != nil {
		return err
	}
	cmp, err := uiclient.CompareSgrStreams(selfPath, otherPath)
	if err != nil {
		return err
	}
	fmt.Printf("%s %s --verify\n", replayplay.PlaybackCommand, selfID)
	fmt.Printf("  A=%s\n  B=%s\n", selfPath, otherPath)
	fmt.Printf("  A: frames=%d bytes=%d\n", cmp.FramesA, cmp.BytesA)
	fmt.Printf("  B: frames=%d bytes=%d\n", cmp.FramesB, cmp.BytesB)
	if cmp.Identical() {
		fmt.Printf("first divergence: none\n")
		return nil
	}
	d := cmp.First
	fmt.Printf("first divergence: frame %d — %s\n", d.Frame, d.Reason)
	if d.OffsetA >= 0 {
		fmt.Printf("  A: frame %d at byte offset %d — %s\n", d.Frame, d.OffsetA, sessionReplayVerifyPayload(d.A))
	}
	if d.OffsetB >= 0 {
		fmt.Printf("  B: frame %d at byte offset %d — %s\n", d.Frame, d.OffsetB, sessionReplayVerifyPayload(d.B))
	}
	return nil
}

// sessionReplayStreamPath resolves one side of a `--verify` comparison to a
// screen-stream FILE: an absolute or relative path that exists is taken as
// itself, and anything else is a session id resolved through the same directory a
// playback would read.
func sessionReplayStreamPath(operand string) (string, error) {
	operand = strings.TrimSpace(operand)
	if operand == "" {
		return "", fmt.Errorf("--%s needs another recording", strings.TrimPrefix(sessionReplayVerifyFlag, "--"))
	}
	if st, err := os.Stat(operand); err == nil && !st.IsDir() {
		return operand, nil
	}
	dir, err := sessionReplayDir(operand)
	if err != nil {
		return "", err
	}
	path, ok := replayplay.ScreenStreamPath(dir)
	if !ok {
		return "", fmt.Errorf("session %s recorded no screen stream: %s holds no %s file (a session that painted no screen has no bytes to compare — a worker run is the ordinary case)",
			operand, dir, uiclient.ScreenStreamSuffix)
	}
	return path, nil
}

// sessionReplayDir resolves a session id to its directory, through the tree's ONE
// resolver (the same one `gopi session log <id>` uses) so the two readers cannot
// disagree about which directory an id names.
func sessionReplayDir(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("no session id was given")
	}
	if filepath.IsAbs(id) {
		if st, err := os.Stat(id); err != nil || !st.IsDir() {
			return "", fmt.Errorf("%s is not a session directory", id)
		}
		return id, nil
	}
	sessions, err := gopilog.ResolveSessionDir()
	if err != nil {
		return "", err
	}
	if _, ferr := gopilog.FindSession(sessions, id); ferr != nil {
		return "", ferr
	}
	return filepath.Join(sessions, id), nil
}

// sessionReplayVerifyVerifyPayload renders one payload for the divergence line,
// bounded so a 4 KB frame cannot flood a terminal. The bound is a CONSTANT (Rule
// 1) and the truncation is stated with the count it left out (Rule 15).
func sessionReplayVerifyPayload(p []byte) string {
	if p == nil {
		return "(no frame at this position)"
	}
	if len(p) > sessionReplayVerifyPayloadMax {
		return fmt.Sprintf("%q …(+%d byte(s))", p[:sessionReplayVerifyPayloadMax], len(p)-sessionReplayVerifyPayloadMax)
	}
	return fmt.Sprintf("%q", p)
}

// sessionReplayVerifyPayloadMax bounds the payload spelling in a --verify line.
const sessionReplayVerifyPayloadMax = 120
