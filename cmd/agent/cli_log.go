package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nickhuang99/gopi/internal/gopilog"
	"github.com/nickhuang99/gopi/internal/sessionevent"
	"github.com/nickhuang99/gopi/internal/toolnames"
	"github.com/nickhuang99/gopi/internal/tools"
)

// roundSelectionFor parses a --http value with gopilog.ParseHTTPSelector — the
// ONE owner of the `all|last|N` round grammar, shared with `gopi session raw <round>`
// and `gopi session stats --rounds`. This file owns no grammar of its own: it only
// ASKS the shared parser, so the grammar changes in one place and every surface
// moves together. ok=false means the value is not a valid round selection.
func roundSelectionFor(httpArg string) (gopilog.HTTPRoundSelector, bool) {
	sel, err := gopilog.ParseHTTPSelector(httpArg)
	if err != nil {
		return gopilog.HTTPRoundSelector{}, false
	}
	return sel, true
}

// logUsage is the help text for the log subcommand. It is printed when the
// subcommand is missing required arguments, passes an invalid flag, or the
// user passes -h/--help.
//
// The subcommand integrates the standalone gopi-log-reader tool into the gopi
// binary: it reads the JSONL session logs written to ~/.gopi/sessions/ and
// renders them in human-readable form. The raw-HTTP-log views moved out to
// their own commands (`gopi session stats` for numbers, `gopi session raw` for bytes) — see
// logMovedPointer.
const logUsage = `gopi session log — GoPi JSONL session log reader (integrated gopi-log)

Usage:
  gopi session log <session-id>                  Read specific session (FULL id — the
                                         one ` + "`gopi session log --last`" + ` prints; a
                                         truncated id is refused, never guessed)
  gopi session log <session-id> <round>          ONE round of the session: its prompt,
                                         tool calls + results, text/thinking.
                                         <round> is the SHARED grammar — a
                                         round number (1 = the session's FIRST
                                         round), 0, or ` + "`last`" + `/-N (the last, the
                                         Nth-from-last). Out of range → refused
  gopi session log <session-id> <round> --request     That round's request BODY, the
                                              bytes as sent (pairs the round with
                                              its raw-log ordinal; the pairing is
                                              named on stderr)
  gopi session log <session-id> <round> --response    That round's response bytes (the
                                              logged stream/response lines)
  gopi session log <session-id> <round> --thinking    Round N's thinking only
  gopi session log <session-id> <round> --filter TOOL  Round N's matching calls only
  gopi session log <session-id> --turns          HUMAN TURNS table: one row per
                                         HUMAN TURN — one row the person
                                         typed to the next, with how many
                                         request/response requests it took.
                                         This is the human-turn SUBSET, not
                                         the session's turn count (that one
                                         also charges a turn an external
                                         notify or the model's own line
                                         opened). ` + "`gopi session show <id>`" + `
                                         prints the turn count
  gopi session log <session-id> --toolcall N     The Nth TOOL CALL in full (1-based,
                                         log order; 0/last for the last, -N from
                                         the end), naming the round it ran in.
                                         This counts CALLS, not rounds
  gopi session log <session-id> --response       Only final text responses
  gopi session log <session-id> --thinking       Only reasoning/thinking blocks
  gopi session log <session-id> --request        System prompt sent with the first request
  gopi session log <session-id> --request --json  Full first request body as indented JSON
  gopi session log <session-id> --chat           Human-friendly conversation view
  gopi session log <session-id> --todo           Task list status overview
                                         (--todo also shows the original task
                                         briefing — s.Task, first user row)
  gopi session log <session-id> --memory         Only memory tool calls (search/store/…)
  gopi session log <session-id> --context-trim   context_trim activity table
  gopi session log <session-id> --search         Only code_search/keyword_search calls
  gopi session log <session-id> --web            Only web_search/web_fetch calls
  gopi session log <session-id> --job            Only job tool calls (action=start/output/kill)
  gopi session log <session-id> --context        Only context_trim calls
  gopi session log <session-id> --vision         Only vision_analyze calls (image processing)
  gopi session log <session-id> --text2img       Only text2img calls (text→PNG rendering)
  gopi session log <session-id> --git            Only git_blame calls (git history)
  gopi session log <session-id> --filter TOOL    Only calls for TOOL (repeatable for multiple)
  gopi session log <session-id> --grep TEXT      Only entries whose content contains TEXT
                                         (case-insensitive; combines with the
                                         family flags above to scope the match)
  gopi session log <session-id> --json           Raw JSONL output (for piping)
  gopi session log <session-id> --text           Plain text mode (no formatting)
  gopi session log --last                        Most recent session
  gopi session log --last N                      Last N session summaries
  gopi session log --last N --memory             View (--memory/--filter/--chat/...)
                                         applied to each of the last N sessions
  gopi session log --branch BRANCH               The session the row owning BRANCH is
                                         running: the SAME input as
                                         <session-id>, named by what
                                         ` + "`git branch --show-current`" + ` prints
                                         in a task worktree (--worktree WT
                                         takes pwd instead)
  gopi session log --all --summary               Summary table of all sessions

Moved to the raw-log commands (they print a one-line pointer and exit non-zero):
  --http, --http N, --raw, --http N --request/--response
      → gopi session stats <id> [--rounds]   per-round digest / token statistics
      → gopi session raw <id> [N] [request|response]   verbatim bytes

Tool family shortcuts (mirror the agent's toolcalls, see internal/agent/loop.go):
  --memory  memory_search memory_store memory_feedback memory_update memory_delete
  --search  code_search keyword_search
  --web     web_search web_fetch
  --job     job (action=start/output/kill)
  --context context_trim
  --vision  vision_analyze
  --text2img text2img
  --git     git_blame
  (--todo is a view, not a filter: gopi session log <id> --todo)

The deliverable selectors (--branch B / --worktree PATH) address the same
input as <session-id> — the session a ledger row is running — so any view flag
combines with them exactly as it does with the id:
  gopi session log --branch BRANCH --response    The responses of the session running
                                         branch BRANCH
  gopi session log --worktree WT --todo          The task area of the session whose row
                                         owns the worktree you are standing in
Passing one of them TOGETHER with <session-id>, --last or --all is refused by
name: they name one input, and a pick between them would be silent.

Flags can be combined:
  gopi session log <id> --response --text        Plain text responses only
  gopi session log <id> --thinking --text        Plain text thinking only
  gopi session log <id> --request --text         Plain text system prompt only
  gopi session log <id> --request --json         Full first request body as indented JSON
  gopi session log <id> --memory --text          Plain text memory calls only
  gopi session log <id> --context-trim           context_trim activity table
  gopi session log <id> --filter bash            Only bash tool calls
  gopi session log <id> --filter read --filter write  Only read/write tool calls
  gopi session log <id> --search                 Only code/keyword search tool calls
  gopi session log <id> --web                    Only web_search/web_fetch tool calls
  gopi session log <id> --vision                 Only vision_analyze (image processing) calls
  gopi session log <id> --text2img               Only text2img (text→PNG rendering) calls
  gopi session log <id> --git                    Only git_blame (git history) calls
  gopi session log <id> --grep timeout           Every entry mentioning "timeout"
  gopi session log <id> --grep timeout --job     Job tool calls mentioning "timeout"
  gopi session log --last --chat                 Most recent session, chat view

The <round> positional counts the session's ROUNDS (1 = the session's first
API call, i.e. its first JSONL ` + "`request`" + ` row) and reads that one round. It is
NOT the raw log's ordinal: the raw log also numbers the sends the session JSONL
never recorded (non-streaming meta calls, retried sends), so the ROUND table
prints the pairing it derived — round number, the raw ordinal, and that round's
raw byte span — and a byte view
(--request/--response) names the raw ordinal it used on stderr.

THE OPERATOR-FACING UNIT IS THE TURN, AND THE ROUND IS AN INTERNAL CONCEPT (the
operator's ruling, 2026-10-25). A TURN is one operator row to the next — how many
asks the PERSON made. A ROUND is one HTTP request/response, and an agent's tool
call is one of those too, so a round count is never a count of what the operator
did. This is why the session's headline number, and ` + "`gopi session replay`" + `'s
summary line, print ` + "`turns=`" + ` and carry NO ` + "`rounds=`" + `: offering both side by side
invited a reader to pick the wrong one. The ROUND table below survives as a
DEBUGGING view of the transport (its flag is a compatibility alias, no longer
listed above), and the playback still walks one STEP per round.
`

// logFlagRounds is the --rounds spelling: the ROUND table — one row per JSONL
// `request` row, the quantity sessionevent.CounterUnitRound names. It is
// referenced by logFlagMember, parseLogArgs, the selectors' refusal text and
// logUsage, so the flag cannot be renamed on one surface only (Rule 1b/13).
//
// IT IS THE SAME UNIT every other surface prints — `gopi session replay <id>
// --rounds`'s `rounds=`, `gopi session stats`' round figure, the JSONL's own
// `request`-row count and the log's iteration count — from the same reader
// (gopilog.SessionRounds). The word round was briefly lent to the OPERATOR row
// (commit c04bcb345, 2026-10-11) and this flag tabulated that neighbour; the
// operator revoked the split (2026-10-12), so the flag is back on THE round and
// the operator-row quantity moved to --turns, which is its own name.
const logFlagRounds = "rounds"

// logFlagTurns is the --turns spelling (gopilog.SessionTurnFlagName): the TURN
// table — one row per OPERATOR row, with the number of ROUNDS that turn took.
// It is the table `--rounds` used to print when the word round meant the
// operator row: the quantity did not change, its NAME did, because it is not a
// round (one turn routinely takes many) and the round is the `request` row
// (Rule 14: one meaning per word).
const logFlagTurns = gopilog.SessionTurnFlagName

// logFlagToolCall is the `--toolcall N` selector: the Nth TOOL CALL of the
// session, in log order. It exists so "the Nth tool call" keeps an address of
// its own: a tool call is an event INSIDE one round (several can happen in
// one round, and a round can happen with none), so it is never counted as a
// round — `gopi session stats --rounds` printing exactly this quantity as
// "rounds" was the defect that produced the four-number session (2026-10-10).
const logFlagToolCall = "toolcall"

// logFlagFull is the --grep companion that turns the match window OFF, printing
// every matched field whole. Named (Rule 1) because the parser's arity table,
// the option struct and the value gate all key on the same spelling.
//
// IT EXISTS BECAUSE THE WINDOW IS A DEFAULT, NOT A LAW. A content search
// usually asks "where did X appear" — a window answers that and keeps the
// answer readable. The other question people really ask about their own
// session is "show me that command's complete output again", where the whole
// block IS the answer and a window would be the thing that hides it.
const logFlagFull = "full"

// logFlagMember reports whether a flag name is valid for the log subcommand.
//
// It reads the verb's ONE table (cli_session_flag_arity.go), which states each
// admitted flag's ARITY beside its admission: a hand-kept `switch` here could
// admit `--json` while the scanner still guessed that it took a value, and that
// is exactly the defect that made `gopi session log --json <id>` answer
// "missing <session-id> argument" (2026-10-27). One table, two readers.
func logFlagMember(flag string) bool {
	_, admitted := logFlagArity(flag)
	return admitted
}

// logOptions is the parsed result of a log subcommand invocation.
type logOptions struct {
	sessionID string // positional <session-id> ("" when none given)
	// dsel is the DELIVERABLE SELECTOR (--branch / --worktree): the SAME input
	// as sessionID, named by what the operator standing in a task worktree
	// holds. At most one spelling is set, and it is resolved to a session id
	// before dispatch (sessionIDForSelector) — never carried into the dispatch
	// as a second way to address a session. It is named dsel because the ROUND
	// selector (roundSel, below) came later and `sel` would be ambiguous
	// between two address spaces — the same two names `gopi session raw` uses.
	dsel tools.DeliverableSelector
	// view flags
	response bool
	thinking bool
	request  bool
	// raw / http are the MOVED flags. They are still parsed (not "unknown")
	// only so the subcommand can print the exact equivalent command instead of
	// a generic unknown-flag error; runLogSubcommand refuses them up front.
	raw     bool   // --raw: verbatim raw HTTP log → gopi session raw <id>
	http    bool   // --http [all|last|N]: per-round view → gopi session stats/raw
	httpArg string // value of --http ("" for bare --http)
	json    bool
	text    bool
	chat    bool
	todo    bool
	memory  bool
	// contextTrim is --context-trim (the activity table view).
	contextTrim bool
	// rounds is --rounds: the session's ROUND table — one row per JSONL
	// `request` row, the raw ordinal it pairs with and the raw-log byte span.
	// It is the SAME unit `gopi session replay --rounds` prints as `rounds=`.
	rounds bool
	// turns is --turns: the session's TURN table — one row per OPERATOR row,
	// with the number of ROUNDS that turn took. It is the NEIGHBOUR of the unit
	// `gopi session replay --rounds` counts, never a second spelling of it.
	turns bool
	// sel is the positional <round> SELECTOR (logOptions.sel was the deliverable
	// selector's name before the round positional existed; the two are
	// different address spaces, so the deliverable one is dsel now). The zero
	// value (Detail=false, All=false) means "no round named".
	roundSel gopilog.HTTPRoundSelector
	// toolCallSel is --toolcall N: the Nth TOOL CALL of the session (a
	// different address space from a round — see logFlagToolCall). Zero value
	// (Detail=false) means "no tool call named".
	toolCallSel gopilog.HTTPRoundSelector
	// tool-family shortcut flags (mirror toolFamilies below)
	search   bool // code_search, keyword_search
	web      bool // web_search, web_fetch
	job      bool // job (action=start/output/kill)
	context  bool // context_trim
	vision   bool // vision_analyze (image processing)
	text2img bool // text2img (text→PNG rendering)
	git      bool // git_blame (git history)
	// grep is the --grep content-search query ("" when absent).
	grep string
	// grepFull is --grep's --full companion: render every matched field WHOLE
	// instead of windowing it around the match. The window is the default
	// because a content search answers a question ("where did X appear"), and
	// measured on a real session the unwindowed answer to a 75-byte question
	// was 19393 bytes. --full is the escape hatch for the other question a
	// reader really asks — "show me that command's complete output again" —
	// where the whole block IS the answer.
	grepFull bool
	all      bool
	summary  bool
	// lastN > 0 means --last [N] was given (1 when no explicit N).
	lastN int
	// filterNames are the tool names collected from --filter (repeatable).
	filterNames []string
}

// collectLogFilters extracts every --filter VALUE / --filter=VALUE occurrence
// from the raw args. --filter is repeatable and the shared memoryFlagSet keeps
// only the last value per name, so repeatable flags must be collected here.
func collectLogFilters(args []string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--filter" && i+1 < len(args) && !strings.HasPrefix(args[i+1], "--"):
			out = append(out, args[i+1])
			i++
		case strings.HasPrefix(a, "--filter="):
			out = append(out, strings.TrimPrefix(a, "--filter="))
		}
	}
	return out
}

// parseLogArgs parses args for the log subcommand without touching the process
// (no os.Exit, no env mutation). It returns the parsed options or a
// usage-style error. Separate from dispatch so the parsing is unit-testable.
func parseLogArgs(args []string) (*logOptions, error) {
	fs := newMemoryFlagSet() // same hand-rolled `--name value` parser
	// The flags' ARITY is declared BEFORE the scan: a valueless flag (--json,
	// --rounds, --turns, the tool-family shortcuts) must never claim the next
	// token as its value, or it eats the positional <session-id> standing beside
	// it. `gopi session log --json <id>` answered "missing <session-id>
	// argument" for exactly that reason (2026-10-27, cli_session_flag_arity.go).
	// It also refuses an unknown flag by NAME, on the set actually typed.
	if err := sessionFlagDeclare(fs, args, logFlagArity); err != nil {
		return nil, err
	}
	positionals, err := fs.parse(args)
	if err != nil {
		return nil, err
	}

	opts := &logOptions{
		response:    fs.has("response"),
		thinking:    fs.has("thinking"),
		request:     fs.has("request"),
		raw:         fs.has("raw"),
		http:        fs.has("http"),
		httpArg:     fs.get("http"),
		json:        fs.has("json"),
		text:        fs.has("text"),
		chat:        fs.has("chat"),
		todo:        fs.has("todo"),
		memory:      fs.has("memory"),
		contextTrim: fs.has("context-trim"),
		rounds:      fs.has(logFlagRounds),
		turns:       fs.has(logFlagTurns),
		search:      fs.has("search"),
		web:         fs.has("web"),
		job:         fs.has("job"),
		context:     fs.has("context"),
		vision:      fs.has("vision"),
		text2img:    fs.has("text2img"),
		git:         fs.has("git"),
		grep:        fs.get("grep"),
		grepFull:    fs.has(logFlagFull),
		all:         fs.has("all"),
		summary:     fs.has("summary"),
		filterNames: collectLogFilters(args),
	}

	// --filter present but no value → error (mirrors the standalone tool).
	if fs.has("filter") && len(opts.filterNames) == 0 {
		return nil, fmt.Errorf("--filter requires a tool name argument")
	}

	// --grep present but no value → error.
	if fs.has("grep") && opts.grep == "" {
		return nil, fmt.Errorf("--grep requires a text argument")
	}

	// --last [N]: N is optional, must be a positive integer when given.
	if fs.has("last") {
		v := strings.TrimSpace(fs.get("last"))
		if v == "" {
			opts.lastN = 1
		} else {
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				return nil, fmt.Errorf("--last must be a positive integer, got %q", fs.get("last"))
			}
			opts.lastN = n
		}
	}

	// The DELIVERABLE SELECTOR (--branch / --worktree) is read BEFORE the
	// positional slots are computed, because it decides them: the selector names
	// the session, so the positionals after it shift one to the left (exactly as
	// `gopi session raw` does).
	sel, err := sessionSelectorFromArgs(args)
	if err != nil {
		return nil, err
	}
	opts.dsel = sel

	// Positionals: <session-id> [<round>].
	//
	// The <round> positional is the SHARED round selector grammar
	// (gopilog.ParseHTTPSelector — the one `gopi session raw <id> <round>` and
	// `gopi session stats --rounds` use), so a round means the same thing on every
	// surface that names one. A first positional that is not a round selector
	// is named a usage error only when it cannot be a session id either: a
	// session id is never parsed as a round, so the check below is "the SECOND
	// positional must be a round", never "the first must not be".
	//
	// WITH a deliverable selector (--branch/--worktree) the selector occupies
	// the session-id slot, so the first positional is the ROUND — the same slot
	// shift `gopi session raw` applies.
	maxPos, roundIdx := 2, 1
	if opts.dsel.Branch != "" || opts.dsel.Worktree != "" {
		maxPos, roundIdx = 1, 0
	}
	if len(positionals) > maxPos {
		return nil, fmt.Errorf("unexpected extra argument %q", positionals[maxPos])
	}
	if len(positionals) > 0 && roundIdx == 1 {
		opts.sessionID = positionals[0]
	}
	if len(positionals) > roundIdx {
		arg := positionals[roundIdx]
		roundSel, selErr := gopilog.ParseHTTPSelector(arg)
		if selErr != nil || !roundSel.Detail {
			// TWO readings of a non-round token, and which one applies depends
			// on the slot. WITHOUT a deliverable selector this is the ROUND slot
			// (the session id is the first positional), so the token must be a
			// round and the refusal says so. WITH one, the selector already names
			// the session, so a token that is not a round can only be the SAME
			// input spelled the other way — the pre-existing "one input only"
			// refusal, whose text lives in cli_session_selector.go (Rule 15:
			// name the real conflict, never guess which input was meant).
			if roundIdx == 1 {
				if selErr != nil {
					// The shared parser's own error text names the OTHER verb's
					// flag ("--http takes …"), a wrong instruction on this
					// surface, so the refusal is rendered for THIS verb — still
					// from the shared grammar constants (Rule 1b/13/14). The
					// listing pointer names the ROUND table: the positional
					// numbers ROUNDS, so the table that lists them is
					// --rounds (the operator's ruling, 2026-10-12).
					return nil, fmt.Errorf("<round> %q is not a round — a round number, 0, or %s/-N (--%s lists them)",
						arg, gopilog.RoundKeywordLast, logFlagRounds)
				}
				// ParseHTTPSelector accepts the LISTING keyword ("all", or an
				// empty value) because the raw/moved surfaces need it; as a
				// positional <round> it names no single round, so it is
				// refused — with the pointer to the listing that does answer it,
				// which is the ROUND table: the positional numbers rounds, and
				// --rounds is the table that lists them (the TURN table is a
				// different quantity — operator rows — so sending a reader there
				// would give them the neighbour's numbering).
				return nil, fmt.Errorf("<round> must name one round — a round number, 0, or %s/-N; %q asks for the whole-session view (drop <round>, or use --%s)",
					gopilog.RoundKeywordLast, arg, logFlagRounds)
			}
			opts.sessionID = arg
		} else {
			opts.roundSel = roundSel
		}
	}

	// --toolcall N: the Nth TOOL CALL of the session — a DIFFERENT address space
	// from a round, parsed with the same shared grammar (number, 0/`last`, -N
	// from the end) and refused in the same way when the token is not one.
	if fs.has(logFlagToolCall) {
		arg := strings.TrimSpace(fs.get(logFlagToolCall))
		sel, selErr := gopilog.ParseHTTPSelector(arg)
		if selErr != nil || !sel.Detail {
			return nil, fmt.Errorf("--%s takes a tool call — a number (1 = the session's first tool call), 0 or %s for the last, or -N from the end; %q names no single call",
				logFlagToolCall, gopilog.RoundKeywordLast, arg)
		}
		opts.toolCallSel = sel
		if opts.roundSel.Detail {
			return nil, fmt.Errorf("--%s and <round>/--%s are two different address spaces (%s vs rounds) — pass one",
				logFlagToolCall, logFlagRounds, sessionevent.CounterUnitToolCall)
		}
	}

	// Re-check the input exclusivity now that the positional slots are known:
	// the selector names the SAME input as the positional <session-id>, so both
	// together is a usage error rather than a silent preference — and so is a
	// selector beside --last/--all (a different input). The text comes from the
	// ONE definition in cli_session_selector.go.
	var otherInputs []string
	if opts.lastN > 0 {
		otherInputs = append(otherInputs, "--last")
	}
	if opts.all {
		otherInputs = append(otherInputs, "--all")
	}
	if err := sessionSelectorInputCheck(sel, opts.sessionID, otherInputs); err != nil {
		return nil, err
	}

	// Round-scope combination rules. A round is one API call, so the modes that
	// describe a whole session cannot be honored beside it; each refusal names
	// the real conflict (Rule 15) instead of silently preferring one.
	if opts.rounds && opts.turns {
		return nil, fmt.Errorf("--%s and --%s are two tables of two quantities — pass one", logFlagRounds, logFlagTurns)
	}
	if opts.roundSel.Detail {
		if opts.all {
			return nil, fmt.Errorf("<round> reads one session's round — drop --all")
		}
		if opts.chat {
			return nil, fmt.Errorf("<round> scopes the view to one round — --chat renders the whole conversation")
		}
		if opts.rounds || opts.turns {
			flag := logFlagTurns
			if opts.rounds {
				flag = logFlagRounds
			}
			return nil, fmt.Errorf("--%s lists the whole session at once — drop <round>, or drop --%s to read one", flag, flag)
		}
	}
	if (opts.rounds || opts.turns) && opts.all {
		// ONE refusal for both tables: each is per-session, and the flag that
		// reached it is named so the caller knows which one to drop.
		flag := logFlagTurns
		if opts.rounds {
			flag = logFlagRounds
		}
		return nil, fmt.Errorf("--%s is a per-session table — drop --all (--%s --last N covers several sessions)", flag, flag)
	}
	if opts.roundSel.Detail && opts.lastN > 0 {
		// A round is one API call of ONE session: --last names several sessions,
		// so there is no single round to read. Refused by name instead of
		// silently reading the newest session's round.
		return nil, fmt.Errorf("<round> reads one session's round — drop --last (or pass the session id to read its round)")
	}

	return opts, nil
}

// toolFamilies maps each log shortcut flag to the agent tool names it selects.
// It mirrors the agent's tool inventory (internal/agent/loop.go): a toolcall
// added there becomes log-filterable with a one-line addition here.
//
// The memory family matches the merged single `memory` tool (tool-family
// merge — search/store/update/feedback are now actions of ONE wire name)
// PLUS the PRE-MERGE legacy wire names memory_search / memory_store /
// memory_update / memory_feedback. Those four are ARCHIVAL log-filter
// tokens: sessions recorded before the merge carry the old tool names, and
// `gopi session log --memory` must still surface them. No toolnames constants exist
// for them (retired by the merge), so they are deliberate literals — the log
// filter is the one surface where the old names must keep matching.
// memory_delete and memory_list are CLI maintenance commands (not LLM tools)
// but sessions may log them via the CLI, so they stay too. The map KEYS and
// familyOrder / log-flag words ("memory"/"search"/"web"/"text2img"/…) are the
// log command's user-facing filter SHORTHAND, intentionally distinct from the
// tool names — a tool rename must not rename the `gopi session log --memory` flag.
var toolFamilies = map[string][]string{
	"memory":  {toolnames.ToolMemory, "memory_search", "memory_store", "memory_update", "memory_feedback", "memory_delete", "memory_list"},
	"search":  {toolnames.ToolCodeSearch, toolnames.ToolKeywordSearch},
	"web":     {toolnames.ToolWebSearch, toolnames.ToolWebFetch},
	"job":     {toolnames.ToolJob},
	"context": {toolnames.ToolContextTrim},
	"vision":  {toolnames.ToolVisionAnalyze},

	"capture":  {toolnames.ToolScreenCapture},
	"text2img": {toolnames.ToolText2img},
	"git":      {toolnames.ToolGitBlame},
}

// familyOrder is the canonical display/expansion order of the family flags.
var familyOrder = []string{"memory", "search", "web", "job", "context", "vision", "text2img", "capture", "git"}

// activeFamilies returns the tool families selected by the shortcut flags.
func activeFamilies(opts *logOptions) []string {
	var out []string
	for _, f := range familyOrder {
		switch f {
		case "memory":
			if opts.memory {
				out = append(out, f)
			}
		case "search":
			if opts.search {
				out = append(out, f)
			}
		case "web":
			if opts.web {
				out = append(out, f)
			}
		case "job":
			if opts.job {
				out = append(out, f)
			}
		case "context":
			if opts.context {
				out = append(out, f)
			}
		case "vision":
			if opts.vision {
				out = append(out, f)
			}
		case "text2img":
			if opts.text2img {
				out = append(out, f)
			}
		case "git":
			if opts.git {
				out = append(out, f)
			}
		}
	}
	return out
}

// filterActive reports whether a tool filter mode is requested (a family
// shortcut flag and/or explicit --filter names).
func filterActive(opts *logOptions) bool {
	return len(activeFamilies(opts)) > 0 || len(opts.filterNames) > 0
}

// resolveFilterSet returns the set of tool names to match for the family
// shortcut flags and --filter/--memory.
func resolveFilterSet(opts *logOptions) map[string]bool {
	set := map[string]bool{}
	for _, f := range activeFamilies(opts) {
		for _, n := range toolFamilies[f] {
			set[n] = true
		}
	}
	for _, n := range opts.filterNames {
		set[n] = true
	}
	return set
}

// isLogSubcommand reports whether argv begins with the log subcommand. When it
// does it runs the subcommand and returns true (the caller must not treat the
// remaining args as an input file).
func isLogSubcommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if args[0] == CliVerbLog {
		runLogSubcommand(args[1:])
		return true
	}
	return false
}

// runLogSubcommand parses the subcommand's own flags and dispatches to the
// gopilog package. It exits the process with a non-zero status on
// argument/usage errors and on missing sessions.
func runLogSubcommand(args []string) {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			fmt.Print(logUsage)
			os.Exit(0)
		}
	}

	opts, err := parseLogArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi session log: %v\n%s", err, logUsage)
		os.Exit(1)
	}

	// The raw-HTTP-log views left `gopi session log` for `gopi session stats` (numbers) and
	// `gopi session raw` (bytes). Refused — never silently reinterpreted, never kept
	// as a deprecated alias: one stderr pointer line, then exit non-zero.
	if opts.raw || opts.http {
		fmt.Fprintf(os.Stderr, "%s\n", logMovedPointer(opts))
		os.Exit(2)
	}

	// The DELIVERABLE SELECTOR (--branch / --worktree) resolves to the session
	// id the row behind it is running, and is then a SUBSTITUTION into the
	// ordinary dispatch — the log is still found by the verb's own resolver, so
	// there is one answer to "where is this session's log?" (see
	// cli_session_selector.go). It happens BEFORE the switch, where a positional
	// <session-id> would have been read (every `--session` arm below keys on
	// sessionID), so every view flag works through the selector exactly as it
	// does through the id.
	if opts.dsel.Branch != "" || opts.dsel.Worktree != "" {
		id, err := sessionIDForSelector(CliVerbLog, opts.dsel)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		opts.sessionID = id
	}

	// Dispatch (mirrors the standalone gopi-log-reader dispatch order).
	switch {
	case opts.all && opts.summary:
		runAllSummary()

	case opts.toolCallSel.Detail && opts.sessionID != "":
		// The TOOL-CALL view: a narrower scope than round (a call lives inside
		// one round). It is dispatched beside the turn view because the two
		// are the same shape of "one item of this session, in full".
		runSessionToolCall(opts.sessionID, opts)

	case opts.roundSel.Detail && opts.sessionID != "":
		// The round view is dispatched BEFORE every whole-session view: it is a
		// scope narrower than all of them, so `gopi session log <id> 3 --thinking` means
		// "round 3's thinking", and the parse step has already refused the
		// combinations that cannot nest (--chat, --all, --rounds).
		runSessionRound(opts.sessionID, opts)

	case opts.rounds && opts.sessionID != "":
		runSessionRounds(opts.sessionID)

	case opts.turns && opts.sessionID != "":
		runSessionTurns(opts.sessionID)

	case opts.contextTrim && opts.sessionID != "":
		runContextTrim(opts.sessionID)

	case opts.grep != "" && opts.sessionID != "":
		runGrep(opts.sessionID, opts)

	case filterActive(opts) && opts.sessionID != "":
		runFilter(opts.sessionID, resolveFilterSet(opts))

	case opts.lastN > 0:
		runLast(opts.lastN, opts)

	case opts.sessionID != "":
		runRead(opts.sessionID, opts)

	default:
		fmt.Fprintf(os.Stderr, "gopi session log: missing <session-id> argument\n%s", logUsage)
		os.Exit(1)
	}
}

// logMovedPointer returns the single stderr pointer line for a --http/--raw
// invocation: the equivalent command in the new surface (FINAL-USAGE-PROPOSAL
// migration table). The old invocation is named, never reinterpreted, and the
// caller exits non-zero after printing it.
//
//	gopi session log --last --http              → gopi session stats --last
//	gopi session log <id> --http                → gopi session stats <id> --rounds
//	gopi session log <id> --http N              → gopi session raw <id> N
//	gopi session log <id> --raw                 → gopi session raw <id>
//	gopi session log <id> --http N --request    → gopi session raw <id> N request
func logMovedPointer(opts *logOptions) string {
	pointer := func(old, neu string) string {
		return fmt.Sprintf("%s moved out of gopi session log — use: %s", old, neu)
	}
	switch {
	case opts.raw && opts.lastN > 0:
		return pointer(fmt.Sprintf("gopi session log --last %d --raw", opts.lastN),
			fmt.Sprintf("gopi session raw --last %d", opts.lastN))
	case opts.raw:
		return pointer("gopi session log <id> --raw", "gopi session raw <id>")
	case opts.http && (opts.request || opts.response):
		// The part keywords come from cli_raw.go's constants — one spelling
		// for the parser, the dispatch switch and this pointer text.
		part := rawPartRequest
		if !opts.request {
			part = rawPartResponse
		}
		n := roundArg(opts.httpArg)
		if n == "" {
			return pointer(fmt.Sprintf("gopi session log <id> --http --%s", part),
				fmt.Sprintf("gopi session raw <id> <round> %s", part))
		}
		return pointer(fmt.Sprintf("gopi session log <id> --http %s --%s", n, part),
			fmt.Sprintf("gopi session raw <id> %s %s", n, part))
	case opts.http && opts.lastN > 0:
		return pointer("gopi session log --last N --http", "gopi session stats --last")
	case opts.http:
		if arg := roundArg(opts.httpArg); arg != "" {
			return pointer(fmt.Sprintf("gopi session log <id> --http %s", arg), "gopi session raw <id> "+arg)
		}
		return pointer("gopi session log <id> --http", "gopi session stats <id> --rounds")
	}
	return pointer("--http/--raw", "gopi session stats <id> --rounds / gopi session raw <id> [round] [request|response]")
}

// roundArg normalizes a --http value for the pointer line: a round selector —
// positive N, or the shared grammar's last/0/negative -N, all of which
// `gopi session raw <id> <selector>` now accepts through the SAME parser — is kept
// as-is, so the pointer names the exact equivalent invocation. "" / "all" name
// no single round, so they become "" and the pointer falls back to the
// whole-log equivalent.
//
// The `all|last|N` grammar itself is NOT re-implemented here: it is parsed by
// gopilog.ParseHTTPSelector, the single source of truth shared with `gopi session raw`
// / `gopi session stats` (a second grammar in this file is exactly how the two
// surfaces would drift apart). This helper only decides how a PARSED selection
// maps onto the pointer text.
func roundArg(httpArg string) string {
	arg := strings.TrimSpace(httpArg)
	if arg == "" || arg == gopilog.RoundKeywordAll {
		return ""
	}
	if _, err := gopilog.ParseHTTPSelector(arg); err != nil {
		return ""
	}
	return arg
}

// runGrep implements --grep: a content search over one session. It renders
// every tool call / text response / thinking block whose content contains the
// query (case-insensitive). When family shortcut flags or --filter names are
// also given, only tool calls in that set are searched — the log-side analogue
// of the agent's code_search/keyword_search scoped to a session.
func runGrep(sessionID string, opts *logOptions) {
	dir, err := gopilog.ResolveSessionDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	path, err := findSessionLogPath(dir, sessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if path == "" {
		fmt.Fprintf(os.Stderr, "Session not found: %s\n", sessionID)
		os.Exit(1)
	}
	s, err := gopilog.ParseFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing %s: %v\n", path, err)
		os.Exit(1)
	}
	fmt.Print(gopilog.FormatGrep(s, opts.grep, resolveFilterSet(opts), opts.grepFull))
}

// runFilter implements --filter/--memory: a view that renders only matching
// tool calls (timestamp + tool_use_id + full input + matched tool_result).
func runFilter(sessionID string, names map[string]bool) {
	dir, err := gopilog.ResolveSessionDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	path, err := findSessionLogPath(dir, sessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if path == "" {
		fmt.Fprintf(os.Stderr, "Session not found: %s\n", sessionID)
		os.Exit(1)
	}
	s, err := gopilog.ParseFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing %s: %v\n", path, err)
		os.Exit(1)
	}
	fmt.Print(gopilog.FormatToolFilter(s, names))
}

// runContextTrim implements --context-trim: a view that renders every
// context_trim activity in the session (call_id, replace_with, outcome, and
// the before/after byte-size comparison).
func runContextTrim(sessionID string) {
	dir, err := gopilog.ResolveSessionDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	path, err := findSessionLogPath(dir, sessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if path == "" {
		fmt.Fprintf(os.Stderr, "Session not found: %s\n", sessionID)
		os.Exit(1)
	}
	s, err := gopilog.ParseFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing %s: %v\n", path, err)
		os.Exit(1)
	}
	fmt.Print(gopilog.FormatContextTrim(s))
}

// runSessionRound implements `gopi session log <session-id> <round> [view flags]`: it
// resolves the session's JSONL (the verb's own resolver — one answer to "where
// is this session's log?") and renders that one round.
func runSessionRound(sessionID string, opts *logOptions) {
	dir, err := gopilog.ResolveSessionDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	path, err := findSessionLogPath(dir, sessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi session log: %v\n", err)
		os.Exit(1)
	}
	if path == "" {
		fmt.Fprintf(os.Stderr, "Session not found: %s\n", sessionID)
		os.Exit(1)
	}
	renderSessionRound(path, opts)
}

// runSessionTurns implements `gopi session log <session-id> --turns`: the
// HUMAN-TURNS table — one row per row the person typed, with the number of ROUNDS
// that turn took. It is the NEIGHBOUR of both the round and the session's turn
// count, printed under its own name (the operator's ruling, 2026-10-27: the bare
// word `turns` is `chats.turn_count`, which this surface cannot read — see
// gopilog.FormatSessionTurnTable).
func runSessionTurns(sessionID string) {
	path, ok := resolveSessionLogPath(sessionID)
	if !ok {
		return
	}
	printSessionTurns(path)
}

// runSessionRounds implements `gopi session log <session-id> --rounds`: the
// ROUND table — one row per JSONL `request` row, the raw ordinal it pairs with
// and the raw-log byte span. It is the SAME unit (and the same number)
// `gopi session replay --rounds` prints as `rounds=` and `gopi session stats`
// reports (the operator's ruling, 2026-10-12).
func runSessionRounds(sessionID string) {
	path, ok := resolveSessionLogPath(sessionID)
	if !ok {
		return
	}
	printSessionRounds(path)
}

// resolveSessionLogPath resolves a session id to its JSONL path through the
// verb's own resolver, printing the shared diagnostics and exiting on a missing
// session. It is the ONE copy of that error handling for the table verbs (Rule
// 14): two tables answering "where is this session's log?" differently is how a
// session renders on one surface and not the other.
func resolveSessionLogPath(sessionID string) (string, bool) {
	dir, err := gopilog.ResolveSessionDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	path, err := findSessionLogPath(dir, sessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi session log: %v\n", err)
		os.Exit(1)
	}
	if path == "" {
		fmt.Fprintf(os.Stderr, "Session not found: %s\n", sessionID)
		os.Exit(1)
	}
	return path, true
}

// runRead implements `gopi session log <session-id> [view flags]`.
func runRead(sessionID string, opts *logOptions) {
	dir, err := gopilog.ResolveSessionDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// The session id must be COMPLETE. FindSession resolves an exact id only:
	// a truncated id is refused with the full ids it could have meant (epic
	// full-id-no-prefix), so its error is printed as-is and exits non-zero.
	path, err := findSessionLogPath(dir, sessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi session log: %v\n", err)
		os.Exit(1)
	}
	if path == "" {
		fmt.Fprintf(os.Stderr, "Session not found: %s\n", sessionID)
		fmt.Fprintf(os.Stderr, "Available sessions in %s:\n", dir)
		files, _ := gopilog.ListSessionFiles() //stylegate:ignore listing for a human-facing dump; an empty list is a valid answer
		for _, f := range files {
			fmt.Fprintf(os.Stderr, "  %s\n", gopilog.FileToID(f))
		}
		os.Exit(1)
	}

	// --request mode: expose the session's system prompt from the raw HTTP log.
	// Handled before the --json passthrough so --request --json means "full
	// request body as JSON", not "raw session JSONL".
	if opts.request {
		runRequest(path, opts.json, opts.text)
		return
	}

	// --json mode: raw passthrough, no parsing needed
	if opts.json {
		raw, err := gopilog.FormatJSONL(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading %s: %v\n", path, err)
			os.Exit(1)
		}
		fmt.Print(raw)
		return
	}

	s, err := gopilog.ParseFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing %s: %v\n", path, err)
		os.Exit(1)
	}

	switch {
	case opts.chat:
		fmt.Print(gopilog.FormatChat(s))
	case opts.todo:
		fmt.Print(gopilog.FormatTodo(s))
	case opts.contextTrim:
		fmt.Print(gopilog.FormatContextTrim(s))
	case opts.response:
		fmt.Print(gopilog.FormatResponse(s, opts.text))
	case opts.thinking:
		fmt.Print(gopilog.FormatThinking(s, opts.text))
	default:
		fmt.Print(gopilog.FormatSession(s))
	}
}

// runLast implements `gopi session log --last [N] [view flags]`.
func runLast(n int, opts *logOptions) {
	dir, err := gopilog.ResolveSessionDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// ONE listing, one head scan per session file: ListSessionRefs returns the
	// newest-first order AND the activity flag from the same pass (the chain
	// of per-fact scans here was the bulk of `gopi session log --last`'s wall time —
	// measured 2026-09-23 on a 3.5k-session profile).
	refs, err := gopilog.ListSessionRefs()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing sessions: %v\n", err)
		os.Exit(1)
	}

	// Degenerate sessions (a bare session_start written by a NewSessionLogger
	// that never ran a user turn — unit-test artifacts, runs that died before
	// their first turn) render as empty "0.0s 0 iter 0 tok" rows and crowd
	// real sessions out of --last N. Skip them: the summary listing is the
	// "what did my agents actually do" view, and a session with zero activity
	// did nothing. The session is still reachable by id (gopi session log <id>) and
	// still crash-dumped by BackfillOrphanCrashDumps.
	files := make([]string, 0, len(refs))
	for _, r := range refs {
		if r.Active {
			files = append(files, r.Path)
		}
	}

	if len(files) == 0 {
		fmt.Println("No sessions found in", dir)
		return
	}

	if n > len(files) {
		n = len(files)
	}

	// View flags: apply the view to EACH of the last N sessions. N must be
	// honored here too — previously this branch returned after rendering only
	// files[0], so `--last N --memory` printed the same single session for
	// every N > 1.
	if opts.response || opts.thinking || opts.json || opts.chat || opts.todo || opts.request || opts.contextTrim || filterActive(opts) || opts.grep != "" {
		for i := 0; i < n; i++ {
			if i > 0 {
				// Separate sessions for readability; --json output stays raw.
				if !opts.json {
					fmt.Println()
				}
			}
			renderSessionView(files[i], opts)
		}
		return
	}

	// If n==1, show full session. Otherwise show summaries.
	if n == 1 {
		s, err := gopilog.ParseFile(files[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing %s: %v\n", files[0], err)
			os.Exit(1)
		}
		fmt.Print(gopilog.FormatSession(s))
		return
	}

	// n > 1: show summary of last N
	for i := 0; i < n; i++ {
		s, err := gopilog.ParseFile(files[i])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing %s: %v\n", files[i], err)
			continue
		}
		fmt.Println(gopilog.FormatSummary(s))
	}
}

// renderSessionView applies one view (request/json/filter/chat/todo/response/
// thinking/context-trim) to a single session file. It mirrors runRead's
// dispatch so that `--last N` combined with view flags renders each of the
// last N sessions instead of only the most recent one.
func renderSessionView(path string, opts *logOptions) {
	if opts.request {
		runRequest(path, opts.json, opts.text)
		return
	}

	if opts.grep != "" {
		s, err := gopilog.ParseFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing %s: %v\n", path, err)
			return
		}
		fmt.Print(gopilog.FormatGrep(s, opts.grep, resolveFilterSet(opts), opts.grepFull))
		return
	}

	if filterActive(opts) {
		s, err := gopilog.ParseFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing %s: %v\n", path, err)
			return
		}
		fmt.Print(gopilog.FormatToolFilter(s, resolveFilterSet(opts)))
		return
	}

	s, err := gopilog.ParseFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing %s: %v\n", path, err)
		return
	}

	if opts.json {
		raw, err := gopilog.FormatJSONL(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading %s: %v\n", path, err)
			return
		}
		fmt.Print(raw)
		return
	}

	switch {
	case opts.chat:
		fmt.Print(gopilog.FormatChat(s))
	case opts.todo:
		fmt.Print(gopilog.FormatTodo(s))
	case opts.contextTrim:
		fmt.Print(gopilog.FormatContextTrim(s))
	case opts.response:
		fmt.Print(gopilog.FormatResponse(s, opts.text))
	case opts.thinking:
		fmt.Print(gopilog.FormatThinking(s, opts.text))
	}
}

// runAllSummary implements `gopi session log --all --summary`.
func runAllSummary() {
	summaries, err := gopilog.LoadAllSummaries(0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if len(summaries) == 0 {
		fmt.Println("No sessions found.")
		return
	}
	fmt.Print(gopilog.FormatSummaryTable(summaries))
}

// runRequest implements the --request flag: it pairs a session file with its
// raw HTTP log, extracts the authoritative (first) request body, and prints
// either the verbatim system prompt (default) or the full request body as
// indented JSON (--request --json). With --text, only the raw prompt text is
// emitted. The raw HTTP logger must have been enabled when the session ran.
// NOTE: the original TASK BRIEFING ([EPIC GOAL]/[Task summary]/[Briefing
// payload]) is NOT a system prompt — it is the first user message. It is NOT
// shown by --request/--text (system blocks only); retrieve it with
// `gopi session raw <session-id> --userprompt` (or .messages[0] under --request
// --json).
func runRequest(sessionPath string, jsonMode, textMode bool) {
	// Resolve through the SHARED resolver (gopilog.ResolveRawLogForSession) —
	// the same one `gopi session raw` and `gopi session stats` use. It tries, in order:
	//   1. the rawLogPath recorded in the session_start event (authoritative,
	//      and the only signal that survives a custom GOPI_RAW_LOG_DIR),
	//   2. a raw log with that basename BESIDE the session file, and
	//   3. timestamp pairing against the legacy flat raw dir.
	//
	// Steps 1-2 are what make this work at all since session-dir-unify S1
	// (commit 3a816775) co-locates <run-uuid>.raw.log inside the session
	// directory: the legacy flat ~/.gopi/raw dir is EMPTY for every run since
	// then, so the timestamp-only fallback alone reported "no raw logs found"
	// while the log sat next to the session JSONL (fixed 2026-09-23).
	rawLog, err := gopilog.ResolveRawLogForSession(sessionPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	p, err := gopilog.ParseFirstRequest(rawLog)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if jsonMode {
		fmt.Print(gopilog.FormatRequestJSON(p))
		return
	}
	fmt.Print(gopilog.FormatRequestSystem(p, filepath.Base(rawLog), textMode))
}
