package main

// chat_once.go — `gopi chat --once` (and the familiar `-p`): ONE prompt in,
// ONE turn of the EXISTING chat REPL, the answer on stdout, an honest exit
// status, nothing left running. No TTY required.
//
// WHY THIS IS THE SAME CHAT, NOT A SECOND ENGINE. The flywheel the operator
// described (prompt → chat turn → read the answer → feed the next prompt) is
// only worth building if the turn a script drives IS the turn a human types:
// the same agent, the same single session/JSONL/raw log, the same tools, the
// same history semantics. So this face does not reimplement the turn — it
// calls runChat(), the REPL, with the prompt already in hand. `gopi chat` and
// `gopi chat --once` therefore cannot drift: a change to the turn changes
// both. The only thing this file adds is how the prompt ARRIVES, how the
// answer is observed, and how the run ENDS.
//
// WHAT A CALLER SEES (the contract a driver scripts against):
//
//	stdin   the prompt. -p/--prompt/positional win when given; otherwise the
//	        lines are drained from stdin, so the invocation the operator asked
//	        for works: `echo 'bootstrap prompt' | gopi chat --once --tools`.
//	        The drain is bounded (chatOncePromptLines, chatOncePromptReadTimeout)
//	        on purpose — an unbounded read is exactly the hang a one-shot face
//	        must not have.
//	stdout  the ANSWER — the turn's text, streamed exactly as a piped
//	        `gopi chat` streams it. One turn, then the process exits.
//	stderr  the plumbing: the banner, the `you>` prompt, the tool-activity
//	        lines (⚙ call … / ⚙ result …), a failed turn's reason, the goodbye.
//	        Never the answer.
//	exit    0 ONLY for a turn that completed and produced text; 1 for a failed
//	        turn or an empty answer; 2 for a usage error (no prompt, a bad
//	        flag). Rule 15: a caller seeing 0 can trust there is an answer on
//	        stdout — a silent 0 is never produced for a run that did nothing.
//
// THE TURN IS EXACTLY ONE. The REPL is fed the prompt and then EOF; the EOF is
// the "one turn". The REPL's own end-of-input path runs the closeout
// (session_end + the CHATS row) and prints its goodbye, so there is no second
// close-out path to keep in step and nothing survives the run.
//
// THE EDITOR IS NEVER ENTERED on this face, whatever stdin is. A one-shot face
// takes a prompt, never a keystroke, and the cooked path is the one whose byte
// stream is already the contract scripts parse (`gopi chat` piped has always
// written the answer to stdout and the `you>` prompt to stderr). A human who
// wants the reserved answer band uses the ordinary interactive `gopi chat`;
// a human who wants one turn gets one turn's text, readable, on stdout.

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nickhuang99/gopi/internal/agent"
)

// The `gopi chat --once` vocabulary (Rule 1b: every word this face compares
// on, accepts or prints is declared once and referenced). The spellings
// deliberately MIRROR `gopi cli --once` — the same flags meaning the same thing
// on the two carriers of one engine, so a caller learns the pair once.
const (
	// chatOnceFlag is the canonical single-turn switch.
	chatOnceFlag = "--once"
	// chatOnceShortFlag is its short spelling (the familiar prompt-in shape).
	chatOnceShortFlag = "-p"
	// chatOncePromptFlag is the explicit prompt spelling, alongside the bare
	// positional one.
	chatOncePromptFlag = "--prompt"
	// chatOnceMaxTurnsFlag caps the pipe-back loop at a turn count. It is the
	// ARMING knob of the driver (chatOnceDriver): without it, `--once` runs
	// exactly one turn, byte for byte as before.
	//
	// WHY IT IS `--max-turns` AND NOT `--turns`. `--turns` already means ONE
	// thing on gopi's own surfaces — a LISTING word that prints the turns and
	// plays nothing (`gopi session log <id> --turns`, `gopi session replay <id>
	// --turns`). A cap that borrowed the same spelling made one flag name two
	// different things depending on the verb in front of it, and a reader who
	// learned the listing meaning first would read `gopi chat --once --turns 5`
	// as "show me five turns" — the opposite of what it does. The name now says
	// CAP, and it sits beside its companion chatOnceMaxSecondsFlag.
	chatOnceMaxTurnsFlag = "--max-turns"
	// chatOnceLegacyTurnsFlag is the RETIRED spelling of the cap above. It is
	// still RECOGNIZED — only in order to be REFUSED BY NAME with the working
	// spelling in the sentence: `--turns` is a LISTING word on gopi's other
	// surfaces, so reading it here as a cap (the old behaviour) or ignoring it
	// (a silent no-op) would both leave a reader with the wrong idea. It is
	// never read as a cap (see chatOnceParseArgs).
	chatOnceLegacyTurnsFlag = "--turns"
	// chatOnceMaxSecondsFlag caps the same loop by wall-clock seconds. It is a
	// companion of the turn cap, never a switch on its own (see
	// chatOnceParseArgs).
	chatOnceMaxSecondsFlag = "--max-seconds"
	// chatOnceStopFlag names the sentinel that ends the loop when an answer
	// carries it (the other stop conditions — the caps, no-progress — need no
	// text of their own).
	chatOnceStopFlag = "--stop-on"
)

// chatOnceIsREPLFlag reports whether a dashed word is one of the INTERACTIVE
// face's flags. Both faces share ONE page (`gopi chat -h`) and one command line,
// so a dashed word the caller wrote is checked against the union of the two
// vocabularies before it is called an unknown flag: an `--once`-face-only parser
// that refused the REPL's `--tools` would break a documented invocation. The
// check reads chatREPLFlags (cli_chat.go), so a flag added to the REPL is
// recognized here without a second list (Rule 13). The dump flag is matched by
// PREFIX: `--dump-screen=PATH` is the joined spelling of an advertised flag.
func chatOnceIsREPLFlag(a string) bool {
	for _, f := range chatREPLFlags() {
		if a == f || strings.HasPrefix(a, f+chatDumpFlagSep) {
			return true
		}
	}
	return false
}

// chatOnceFlags is this face's accepted flag vocabulary, in one list: the source
// of the refusal message when a dashed word matches no definition above. Its
// sibling is chatREPLFlags() (the interactive face's vocabulary, cli_chat.go) —
// the two lists are what make each face's argv CLOSED.
func chatOnceFlags() []string {
	return []string{
		chatOnceFlag,
		chatOnceShortFlag,
		chatOncePromptFlag,
		chatOnceMaxTurnsFlag,
		chatOnceMaxSecondsFlag,
		chatOnceStopFlag,
		HelpFlag,
		HelpFlagLong,
	}
}

// The exit statuses this face returns (Rule 1a). They agree with
// `gopi cli --once`'s on purpose: two one-shot faces of one engine that
// disagreed about what 1 means would be a trap for the driver loop.
const (
	// chatOnceExitOK is a COMPLETED turn that produced text.
	chatOnceExitOK = 0
	// chatOnceExitTurnFailed is a turn that failed, or that completed with no
	// text at all.
	chatOnceExitTurnFailed = 1
	// chatOnceExitUsage is a usage error (a bad flag, or no prompt).
	chatOnceExitUsage = 2
)

// The prompt-source bounds.
const (
	// chatOncePromptLines bounds how many stdin lines a piped one-shot drains
	// for its prompt. More than one line is deliberate (a bootstrapping prompt
	// is rarely one line); the bound is what keeps the face from waiting with
	// no writer left.
	chatOncePromptLines = 200
	// chatOncePromptReadTimeout bounds the stdin drain. A piped prompt arrives
	// immediately; a terminal that never sends a line (the human walked away)
	// must not hold the process forever, and a bound is what makes the exit
	// deterministic (Rule 15 — an honest timeout beats a silent hang).
	chatOncePromptReadTimeout = 30 * time.Second
	// chatOncePromptScanBuf bounds one prompt line. A bootstrapping prompt is
	// text; this is headroom, not a limit anyone should meet.
	chatOncePromptScanBuf = 1024 * 1024
)

// chatOncePrefix is the prefix of this face's own human lines, so an operator
// can tell the plumbing from the model's output.
const chatOncePrefix = "gopi chat: "

// chatOnceUsage is the `--once` section of the help. It is a const joined into
// chatUsage (one page, one source), not a second page a reader must know to
// ask for.
const chatOnceUsage = `
--once, -p [PROMPT]  run ONE turn and exit — no prompt loop, no terminal
                   required, no editor (the scripted/agent-driven face of
                   ` + "`gopi chat`" + `). The prompt comes from -p/--prompt, from
                   the positional argument, or from stdin when neither is
                   given:

                     gopi chat --once "say OK"
                     gopi chat --once -p "summarise the git log" --tools
                     echo 'bootstrap prompt' | gopi chat --once --tools

                   Pipeline shape: stdout carries this turn's ANSWER only
                   (streamed exactly as a piped ` + "`gopi chat`" + ` streams it); the
                   ` + "`you>`" + ` prompt, the banner, the tool-activity lines
                   (` + "`⚙ call`" + `/` + "`⚙ result`" + `), a failed turn's reason and the
                   goodbye all go to stderr. So a driver can pipe the answer
                   straight into the next prompt:

                     echo 'find a bug' | gopi chat --once --tools | gopi chat --once

                   A multi-line prompt is joined into ONE message (a chat turn
                   is one message). The process exits when the ONE turn is done
                   and leaves nothing running.

                   UNATTENDED BY DEFAULT. A one-shot face's answer is consumed
                   by a script or an agent — nobody who can reply — so the run's
                   prompt STATES that: no human will answer, the model is
                   authorised to choose the conventions the task left open and
                   continue, and ending by ASKING is named as the failure shape.
                   This is the difference between a run that stops after one
                   turn to ask a question and one that delivers (measured: the
                   same model and toolset, with and without that sentence). It
                   costs the model nothing and it is on this face only:

                     gopi chat --once --answer-alone "what does this regex do?"

                   ` + "`--answer-alone`" + ` declines it for the case where a HUMAN
                   really is reading the one answer — a single question, a
                   scripted step someone is watching. An interactive ` + "`gopi chat`" + `
                   never carries the context at all (a human IS watching it).

                   SCRIPTED SESSIONS: if the input's FIRST line is a slash
                   command, the input is read as REPL LINES instead — the same
                   lines a human types at the ` + "`you>`" + ` prompt, dispatched by the
                   SAME command table the interactive REPL uses (never a second
                   one, and never sent to the model). A non-TTY driver can then
                   script the surface a human types into:

                     gopi chat --once /help
                     printf '/task list\n/quit\n' | gopi chat --once
                     gopi chat --once /tools "review the diff"  # cmd + message

                   A scripted session ends the way an interactive one does (a
                   ` + "`/quit`" + ` line, or end of input). Exit status is then the
                   SCRIPT's verdict: 0 = every line ran and the session ended
                   the caller's own way; 1 = a line was NOT honoured (named on
                   stderr) or a turn failed; 2 = usage error. A clean ` + "`/quit`" + ` is
                   a 0, not a failure — but a script that quit after a refused
                   command is still a 1.

                   Exit status: 0 = the turn completed AND produced text;
                   1 = the turn failed, or produced no text; 2 = usage error
                   (no prompt, or a bad flag). A caller seeing 0 can trust
                   there is an answer on stdout — never a silent success.

--prompt TEXT      the explicit prompt spelling of --once (same precedence as
                   the positional: --prompt > positional > stdin).

` + chatOnceMaxTurnsFlag + ` N, ` + chatOnceMaxSecondsFlag + ` S, ` + chatOnceStopFlag + ` TEXT
                   PIPE-BACK DRIVER — the engine's own answer becomes the next
                   turn's prompt, until a stop condition fires:

                     gopi chat --once ` + chatOnceMaxTurnsFlag + ` 5 "find a bug, then fix it"
                     gopi chat --once ` + chatOnceMaxTurnsFlag + ` 20 ` + chatOnceMaxSecondsFlag + ` 300 "more"
                     gopi chat --once ` + chatOnceMaxTurnsFlag + ` 10 ` + chatOnceStopFlag + ` "DONE" "work"

                   The cap is ` + chatOnceMaxTurnsFlag + `, NOT ` + chatOnceLegacyTurnsFlag + `:
                   everywhere else in gopi ` + chatOnceLegacyTurnsFlag + ` is a LISTING
                   word that prints the turns and plays nothing
                   (` + "`gopi session log <id> " + chatOnceLegacyTurnsFlag + "`" + `,
                   ` + "`gopi session replay <id> " + chatOnceLegacyTurnsFlag + "`" + `).
                   One spelling, one meaning — so this face REFUSES
                   ` + chatOnceLegacyTurnsFlag + ` BY NAME and points here, rather
                   than reading it as a cap or ignoring it.

                   After each turn the answer is fed back as the next user
                   message, in THIS process, on ONE session — no shell loop,
                   so the JSONL holds the whole run: session_start, user×N,
                   assistant×N, session_end.exit_code. (A shell loop would make
                   every turn its own session, with no shared history.)

                   The loop stops at the FIRST of:
                     * ` + chatOnceMaxTurnsFlag + ` N    N turns done (0 = uncapped)
                     * ` + chatOnceMaxSecondsFlag + ` S  S wall-clock seconds elapsed, checked
                                        between turns (at most one turn over)
                     * no progress      the answer is identical to the one
                                        before it — the engine restating itself
                                        would spin forever, billing as it goes
                     * ` + chatOnceStopFlag + ` TEXT   TEXT appears in an answer as a WHOLE
                                        WORD (` + "`DONE`" + ` never matches ` + "`ABANDONED`" + `)

                   ` + "`--max-turns`" + ` is REQUIRED: it is what arms the driver. Without
                   it every word after ` + "`--once`" + ` is ordinary turn text, and this
                   face is exactly what it was. ` + "`--max-seconds`" + ` and ` + "`--stop-on`" + `
                   may be combined with it in any order (N/S > 0; 0 = uncapped).

                   Reaching a cap is NOT a failure: exit stays 0 and stderr
                   carries the reason, e.g.

                     gopi chat: driver stopped after 5 turn(s): --max-turns 5
                     gopi chat: driver stopped after 3 turn(s): no progress — …
                     gopi chat: driver stopped after 4 turn(s): --stop-on "DONE"

                   Exit status WITH the driver: 0 = the loop reached its stop
                   normally (every stop above); 1 = a turn FAILED — the loop
                   ends there and stderr carries the failure AND the reason;
                   2 = usage error.

                   stdout with the driver is a DELIMITED machine channel — the
                   answers only, one per turn, each ending with a line holding

                     ` + chatOnceTurnSep + `

                   so a driver can split turns (` + "`awk -v RS='…'`" + `, ` + "`csplit`" + `, a
                   readline loop). The model's live reasoning stays on stderr,
                   so the channel carries no half-answers: a driver that needs
                   the reasoning too runs the same command with ` + "`2>&1`" + ` and still
                   splits on the separator lines.

                   Each turn of the loop is durable in the SESSION JSONL before
                   the next one starts: session_start, then a user/assistant row
                   per turn, then ONE session_end carrying the run's exit_code.
`

// chatOnceToolActivityOut, when non-nil, receives ONE line per tool call and
// per tool result — the one-shot face's tool-activity channel.
//
// WHY IT IS NEEDED AT ALL. On the interactive editor path a tool call is shown
// on the status row; on the cooked path (piped stdin, --no-editor — i.e. every
// scripted run) that row does not exist, so a tool-enabled one-shot turn
// executed tools with NO visible sign that it had. Measured on the first live
// `--once --tools` run: the mock's second round carried the tool_result and the
// caller's stderr said nothing about a tool at all. A driver that cannot see
// "the model chose to act" cannot distinguish a tool turn from a chat turn, and
// that distinction is the whole reason to pass --tools.
//
// It is nil by default, so the interactive bytes are untouched: the one-shot
// face is the only writer, and it writes to stderr (stdout stays the answer).
var chatOnceToolActivityOut io.Writer = nil

// chatOnceToolActivity prints one tool line through the channel above, using
// the terminal carrier's OWN line builder so both carriers describe one call
// identically (termToolLine: "⚙ kind name digest").
func chatOnceToolActivity(ev agent.ToolEvent) {
	if chatOnceToolActivityOut == nil {
		return
	}
	fmt.Fprint(chatOnceToolActivityOut, termToolLine(ev))
}

// The failure CLASSES a face driving this REPL can observe (Rule 1b: the words
// the observer and its callers compare on are declared once). A command the REPL
// could not honour and a model turn that failed are different events with
// different remedies, so a driver is told WHICH happened.
const (
	chatOnceFailureCommand = "command"
	chatOnceFailureTurn    = "turn"
	// chatOnceFailureSeal is a turn the MESSAGE BOUNDARY refused: the input was
	// too large to send and the boundary had nowhere to bill it, so no request
	// was made at all. It is its own class because it is not the model failing —
	// a scripted face must be able to tell "the provider errored" from "gopi
	// declined to send this line", and an operator's fix differs completely
	// (see chatSealMessage).
	chatOnceFailureSeal = "seal"
)

// chatOnceFailureObserver, when non-nil, receives every failure the REPL's OWN
// arms hit during this process — a slash command it could not honour, a turn that
// failed. It is the seam the scripted-sequence face (chat_once_seq.go) reads its
// exit status from: a refused command is printed by the REPL and then `continue`d,
// so without this the caller gets a 0 for a script line that never ran — the
// silent success Rule 15 forbids.
//
// Nil by default and writing NOTHING of its own: the refusal is already on stderr
// through the REPL's own notice, so the interactive `gopi chat` keeps its
// historic bytes. This seam carries the CLASS, not a second message.
var chatOnceFailureObserver func(kind string, err error)

// chatOnceReportFailure publishes one failure through the observer above. The
// REPL's command arms call it; the face that installed the observer decides what
// the exit status must say.
func chatOnceReportFailure(kind string, err error) {
	if chatOnceFailureObserver == nil || err == nil {
		return
	}
	chatOnceFailureObserver(kind, err)
}

// isChatOnceRequested reports whether this argv (the tail AFTER the `chat`
// verb) selects the non-interactive single turn. It reads the ONE list of
// spellings, so the dispatch and the parser cannot disagree about what "once"
// means.
func isChatOnceRequested(args []string) bool {
	for _, a := range args {
		if a == chatOnceFlag || a == chatOnceShortFlag || a == chatOncePromptFlag ||
			strings.HasPrefix(a, chatOncePromptFlag+"=") {
			return true
		}
	}
	return false
}

// chatOnceOptions is one resolved `--once` invocation, after the one-shot words
// are stripped from argv.
type chatOnceOptions struct {
	// prompt is the prompt given on the command line (-p/--prompt/positional).
	// Empty means "read it from stdin".
	prompt string
	// rest is the argv tail with the one-shot words removed, for the REPL's own
	// parser (--tools, --no-editor, -v/--verbose, --dump-screen, a session seed).
	rest []string
	// turns is the pipe-back loop's turn cap. Negative means the caller asked
	// for the driver and accepted the absence of a turn cap (see turnsSet).
	turns int
	// turnsSet reports that the CAP was given (chatOnceMaxTurnsFlag): it is the
	// driver's ARMING flag, kept separate from the value so a cap of 0 (uncapped)
	// is not the same
	// invocation as "no driver at all".
	turnsSet bool
	// maxSeconds is the loop's wall-clock cap (0 = none).
	maxSeconds int
	// sentinel ends the loop when an answer carries it ("" = none).
	sentinel string
}

// chatOnceParseArgs splits the one-shot words from the argv tail the REPL
// parses. --once/-p are switches; --prompt takes the next word (or
// `--prompt=VALUE`). Everything else is the REPL's, including a bare word when
// no flag promised it a value — that is the positional prompt, the familiar
// spelling.
//
// The driver's own knobs are resolved here too, in this one place, so the
// command line has ONE reader (Rule 1: the flag spellings come from the
// constants above, never typed twice) and a bad value fails as a USAGE error
// before any turn runs.
//
// A repeated prompt is a USAGE ERROR, not a silent pick: a caller that passed
// two prompts meant something by it, and guessing would answer a question
// nobody asked (Rule 15).
func chatOnceParseArgs(args []string) (chatOnceOptions, error) {
	// rest starts NON-nil: a nil slice and an empty slice behave alike in a
	// range, but `chatOnceHelpSpelled(opts.rest)` and the REPL's own parser read
	// it as a list, and appending to nil is the kind of difference that only
	// shows up on one caller's invocation.
	opts := chatOnceOptions{rest: []string{}}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == chatOnceFlag || a == chatOnceShortFlag:
			// A switch: the prompt is the positional/--prompt value.
		case a == chatOncePromptFlag:
			if i+1 >= len(args) {
				return opts, fmt.Errorf("%s needs a prompt (or write %s=TEXT)", chatOncePromptFlag, chatOncePromptFlag)
			}
			i++
			if err := chatOnceSetPrompt(&opts, args[i]); err != nil {
				return opts, err
			}
		case strings.HasPrefix(a, chatOncePromptFlag+"="):
			if err := chatOnceSetPrompt(&opts, strings.TrimPrefix(a, chatOncePromptFlag+chatDumpFlagSep)); err != nil {
				return opts, err
			}
		case a == chatOnceLegacyTurnsFlag || strings.HasPrefix(a, chatOnceLegacyTurnsFlag+chatDumpFlagSep):
			// REFUSED BY NAME, never reinterpreted: the word is now a LISTING
			// word on gopi's other surfaces, so accepting it here (what this
			// face used to do) keeps one spelling meaning two things, and
			// ignoring it would silently run a single turn while the caller
			// believed they had armed a loop. Naming the working spelling is
			// the only answer that leaves the caller with a runnable command.
			return opts, fmt.Errorf("%s is not this face's cap any more — write %s N (a CAP: it stops the pipe-back loop after N turns). On this face %s would read as a LISTING word, which is what it means in `gopi session log`/`gopi session replay`",
				chatOnceLegacyTurnsFlag, chatOnceMaxTurnsFlag, chatOnceLegacyTurnsFlag)
		case a == chatOnceMaxTurnsFlag:
			if i+1 >= len(args) {
				return opts, fmt.Errorf("%s needs a turn count (or write %s=N)", chatOnceMaxTurnsFlag, chatOnceMaxTurnsFlag)
			}
			i++
			if err := chatOnceSetTurns(&opts, args[i]); err != nil {
				return opts, err
			}
		case strings.HasPrefix(a, chatOnceMaxTurnsFlag+chatDumpFlagSep):
			if err := chatOnceSetTurns(&opts, strings.TrimPrefix(a, chatOnceMaxTurnsFlag+chatDumpFlagSep)); err != nil {
				return opts, err
			}
		case a == chatOnceMaxSecondsFlag:
			if i+1 >= len(args) {
				return opts, fmt.Errorf("%s needs a duration in seconds (or write %s=S)", chatOnceMaxSecondsFlag, chatOnceMaxSecondsFlag)
			}
			i++
			if err := chatOnceSetMaxSeconds(&opts, args[i]); err != nil {
				return opts, err
			}
		case strings.HasPrefix(a, chatOnceMaxSecondsFlag+chatDumpFlagSep):
			if err := chatOnceSetMaxSeconds(&opts, strings.TrimPrefix(a, chatOnceMaxSecondsFlag+chatDumpFlagSep)); err != nil {
				return opts, err
			}
		case a == chatOnceStopFlag:
			if i+1 >= len(args) {
				return opts, fmt.Errorf("%s needs a sentinel text (or write %s=TEXT)", chatOnceStopFlag, chatOnceStopFlag)
			}
			i++
			if err := chatOnceSetSentinel(&opts, args[i]); err != nil {
				return opts, err
			}
		case strings.HasPrefix(a, chatOnceStopFlag+chatDumpFlagSep):
			if err := chatOnceSetSentinel(&opts, strings.TrimPrefix(a, chatOnceStopFlag+chatDumpFlagSep)); err != nil {
				return opts, err
			}
		default:
			// A bare word is the PROMPT unless it is a COMMAND LINE: a slash
			// word is the scripted-driver spelling (`gopi chat --once /tools
			// "review the diff"` — a command, then the message), and reading it
			// as the prompt would hand the model a line the caller meant this
			// surface to EXECUTE (chat_once_seq.go). It is the SAME "/" trigger
			// the interactive carriers branch on, so the split cannot drift.
			//
			// A bare word IS the prompt on this face — the familiar spelling.
			// There is deliberately no second positional: the REPL's other
			// positional is a session SEED, and accepting one here would make
			// `-p a b` mean "prompt a, resume b" when the caller almost
			// certainly typed two prompts. Mirroring `gopi cli --once`, a second
			// bare word is refused by name rather than reinterpreted.
			// A BARE word is the prompt. A DASHED word that reached here matched
			// no flag of THIS face — but it may be one of the REPL's, which
			// shares the page and whose argv tail this parser hands on. So the
			// word is checked against the REPL's list (chatREPLFlags, the same
			// constants the REPL parses with — Rule 13, one definition): if it is
			// a flag of the surface the caller is invoking, it rides through and
			// the REPL decides on it. Only a word that belongs to NEITHER face is
			// refused here, by name, before anything runs (Rule 15): the loop
			// would otherwise hand it to the REPL as the session SEED — the
			// silent reinterpretation `gopi chat --once do-x --bogus` used to
			// produce (measured on the pre-change binary: exit 1 with a "resume
			// target" message about a positional the caller never wrote).
			//
			// The refusal rides this face's own usage status (chatOnceExitUsage,
			// asserted equal to the REPL's chatExitUsage), so `gopi chat --bogus`
			// and `gopi chat --once --bogus` answer with one status.
			if strings.HasPrefix(a, chatFlagPrefix) && !chatOnceIsREPLFlag(a) {
				return opts, fmt.Errorf("unknown %s %s — this face's flags are: %s",
					chatWordFlag, a, strings.Join(chatOnceFlags(), chatFlagListSep))
			}
			// A VALUED REPL FLAG CARRIES ITS VALUE IN THE NEXT SLOT, SO THE SLOT
			// RIDES THROUGH WITH IT. Without this arm the value fell into the
			// BARE-WORD branch below and was recorded as the PROMPT, so the
			// spaced spelling the help page advertises
			// (`gopi chat --resume <session-id>`, chatUsage) was refused with
			// "more than one prompt given (<id> and <text>)" while the joined
			// `--resume=<session-id>` worked — a reader who followed the page
			// wrote a command line that could not run. Measured on the shipped
			// binary for task 01a1adab:
			//
			//	$ gopi chat --once --resume 01a1adab-… -p hi
			//	gopi chat: more than one prompt given
			//	  ("01a1adab-…" and "hi") — one turn takes ONE prompt
			//
			// The word in the slot is taken only when it is NOT another flag: a
			// valued flag's parsers (cli_chat.go) treat a `-`-word as "no value
			// given", so a slot holding one stays a slot and the REPL answers it
			// — that is how `gopi chat --once --resume -h` keeps printing the
			// page instead of resuming a session called "-h". The list is
			// chatResumeValuedFlags, the parser's own (Rule 13), so a flag that
			// gains a value slot joins this arm by construction.
			if chatValuedFlagSpelling(a) != "" {
				opts.rest = append(opts.rest, a)
				// The JOINED spelling carries its value inside the word
				// (`--resume=<id>`), so it has no slot to hand on.
				if !strings.Contains(a, chatFlagValueSep) &&
					i+1 < len(args) && !strings.HasPrefix(args[i+1], chatFlagPrefix) {
					opts.rest = append(opts.rest, args[i+1])
					i++ // the value was in the next slot: consume it
				}
				continue
			}
			if !strings.HasPrefix(a, "-") && a != "" {
				if chatOnceSeqIsScriptedLine(a) {
					opts.rest = append(opts.rest, a)
					continue
				}
				if err := chatOnceSetPrompt(&opts, a); err != nil {
					return opts, err
				}
				continue
			}
			opts.rest = append(opts.rest, a)
		}
	}
	if (opts.maxSeconds > 0 || opts.sentinel != "") && !opts.turnsSet {
		// A companion knob without its arming flag is far more likely to be a
		// typo (a forgotten --turns) than an intended no-op, and a silent no-op
		// is what a caller would spend an afternoon on (Rule 15).
		return opts, fmt.Errorf("%s and %s are the pipe-back driver's stop conditions — they need %s N to arm it (%s 0 = uncapped)",
			chatOnceMaxSecondsFlag, chatOnceStopFlag, chatOnceMaxTurnsFlag, chatOnceMaxTurnsFlag)
	}
	return opts, nil
}

// chatOnceSetTurns records the loop's turn cap. A negative value is the explicit
// "uncapped" spelling (a driver whose only stop is time or the sentinel); a
// non-numeric value is a usage error.
func chatOnceSetTurns(opts *chatOnceOptions, value string) error {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return fmt.Errorf("%s expects a number of turns, got %q", chatOnceMaxTurnsFlag, value)
	}
	opts.turns = n
	opts.turnsSet = true
	return nil
}

// chatOnceSetMaxSeconds records the loop's wall-clock cap. The ceiling exists so
// an absurd value (one that ends the loop at a time no run reaches) is refused
// by name instead of being accepted and ignored.
func chatOnceSetMaxSeconds(opts *chatOnceOptions, value string) error {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return fmt.Errorf("%s expects a number of seconds, got %q", chatOnceMaxSecondsFlag, value)
	}
	if n < 0 || n > chatOnceMaxSecondsCeiling {
		return fmt.Errorf("%s expects 0 (uncapped) or 1..%d seconds, got %d",
			chatOnceMaxSecondsFlag, chatOnceMaxSecondsCeiling, n)
	}
	opts.maxSeconds = n
	return nil
}

// chatOnceSetSentinel records the stop sentinel. An empty value is refused: a
// caller who typed the flag meant a text, and "" would match every answer (the
// loop would end at turn 1 for no visible reason).
func chatOnceSetSentinel(opts *chatOnceOptions, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s needs a non-empty sentinel text", chatOnceStopFlag)
	}
	if opts.sentinel != "" {
		return fmt.Errorf("more than one %s given (%q and %q) — one sentinel ends the loop",
			chatOnceStopFlag, opts.sentinel, value)
	}
	opts.sentinel = value
	return nil
}

// chatOnceSetPrompt records one prompt word, refusing a second (see above).
func chatOnceSetPrompt(opts *chatOnceOptions, value string) error {
	if opts.prompt != "" {
		return fmt.Errorf("more than one prompt given (%q and %q) — one turn takes ONE prompt",
			opts.prompt, value)
	}
	opts.prompt = value
	return nil
}

// runChatOnce is the one-shot face: resolve the prompt, run exactly ONE turn
// through the ordinary REPL, report the honest exit status.
func runChatOnce(args []string) int {
	opts, err := chatOnceParseArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s%v\n\n%s", chatOncePrefix, err, chatUsage)
		return chatOnceExitUsage
	}
	// Help stays the REPL's: `gopi chat --once -h` prints the same page (which
	// now carries the one-shot and the driver sections), so there is ONE help
	// surface.
	if chatOnceHelpSpelled(opts.rest) {
		return runChat(opts.rest)
	}
	prompt := opts.prompt
	stdinText := ""
	if strings.TrimSpace(prompt) == "" {
		stdinText, err = chatOnceReadPrompt(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s%v\n\n%s", chatOncePrefix, err, chatOnceUsage)
			return chatOnceExitUsage
		}
	}

	// REPL LINES, OR ONE PROMPT? The input is a scripted transcript as soon as
	// its FIRST line is a slash command — the caller is driving a session, not
	// asking a question (see chat_once_seq.go for why that rule is this narrow).
	// Anything else is the one-prompt face, byte-for-byte as it was.
	seq := chatOnceSeqFromArgv(opts.rest)
	if chatOnceSeqIsScriptedLine(chatOnceSeqFirstLine(seq, stdinText)) {
		return runChatOnceSequence(chatOnceSeqRest(opts.rest), chatOnceSeqAppend(seq, stdinText))
	}

	prompt = chatOnceOneLine(chatOnceSeqPick(prompt, stdinText))
	if prompt == "" {
		fmt.Fprintf(os.Stderr, "%s%s needs a prompt — pass it as %s TEXT, as the positional argument, or on stdin\n\n%s",
			chatOncePrefix, chatOnceFlag, chatOnceShortFlag, chatOnceUsage)
		return chatOnceExitUsage
	}

	// The input is built HERE, not by the REPL, so the answer stream can be
	// observed while it is written to stdout (see chatOnceAnswer), and the
	// tool-activity channel is turned on for the duration of this run (see
	// chatOnceToolActivityOut: without it a piped tool turn shows no sign that
	// a tool ran, which is exactly what a driver needs to see).
	//
	// THE PIPE-BACK DRIVER rides the same seam: with --turns the input layer is
	// the driver's own reader (chatOncePromptReader), which feeds the REPL one
	// prompt per turn — the engine's own answer — and the sink below is where the
	// REPL reports each finished turn back (chatOnceSink). Without --turns both
	// are absent and this face is byte-for-byte what it was.
	driverOpts, driverArmed := chatOnceDriverFromOptions(opts)
	var driver *chatOnceDriver
	var input *chatInput
	var answer *chatOnceAnswer
	var closeInput func()
	if driverArmed {
		driver = newChatOnceDriver(driverOpts, prompt)
		input, answer, closeInput = chatOnceDriverInput(driver)
	} else {
		input, answer, closeInput = chatOnceInput(prompt)
	}
	defer closeInput()
	prevActivity := chatOnceToolActivityOut
	chatOnceToolActivityOut = os.Stderr
	defer func() { chatOnceToolActivityOut = prevActivity }()
	prevSink := chatOnceSink
	chatOnceSink = nil
	if driver != nil {
		chatOnceSink = driver
	}
	defer func() { chatOnceSink = prevSink }()

	// Arm the failure seam: a turn the REPL reports as failed CONTINUES the loop
	// by design (bug 01a0d513), so the exit status is the only place this face can
	// learn of it — without the seam a failed turn would be a silent 0 here.
	var turnErr error
	restoreFailures, _ := chatOnceObserveFailures(&turnErr)
	defer restoreFailures()

	code := runChatWithInput(opts.rest, input)
	if driver != nil {
		// The stop notice is the operator's answer to "why did it end?" — a cap,
		// no progress or the sentinel — and it belongs on stderr, which is where
		// the chrome lives (stdout is the answers).
		driver.notice()
		if driver.stopReason == chatOnceStopTurnFailed {
			// A failed turn is the driver's ONE failure: the loop ended at a turn
			// that produced no answer to feed back. The REPL reports the failure
			// itself; this is the exit contract's 1.
			if code == chatOnceExitOK {
				code = chatOnceExitTurnFailed
			}
			return code
		}
		// A normal stop (a cap, no progress, the sentinel) is NOT an error: the
		// loop did what it was asked, and only the session's own failure changes
		// the status (Rule 5's exit contract).
		return code
	}
	if code != chatOnceExitOK {
		return code
	}
	if turnErr != nil {
		return chatOnceExitTurnFailed
	}
	if strings.TrimSpace(answer.Answer()) == "" {
		// The same honesty `gopi cli --once` carries: a completed turn with no
		// text is a FAILURE, never an empty success — a driver loop must not be
		// handed "" and believe it got a turn.
		fmt.Fprintf(os.Stderr, "%sthe turn completed without producing any text\n", chatOncePrefix)
		return chatOnceExitTurnFailed
	}
	return chatOnceExitOK
}

// chatOnceObserveFailures arms the failure seam (chatOnceFailureObserver) for the
// duration of ONE run and returns the restore func plus the caller's own box for
// the failure it recorded. Both faces of this file use it, so "the REPL hit a
// failure the caller must hear about" has ONE implementation.
//
// The kinds are kept apart on purpose: `command` is a scripted line the REPL
// refused (it says why through its own notice and continues), `turn` is a model
// turn that failed. A face reports them differently, and only the caller knows
// which one its exit status owes the driver.
func chatOnceObserveFailures(dst *error) (restore func(), record func(error)) {
	prev := chatOnceFailureObserver
	record = func(err error) {
		if err != nil {
			*dst = err
		}
	}
	chatOnceFailureObserver = func(kind string, err error) {
		if kind == chatOnceFailureCommand || kind == chatOnceFailureTurn {
			record(err)
		}
	}
	return func() { chatOnceFailureObserver = prev }, record
}

// chatOnceInput builds the REPL's input layer for a one-shot run: the cooked
// reader over the prompt (then EOF — that IS the one turn), with the answer
// stream teed so this face can tell an empty turn from a real one.
//
// It goes through newChatInputCooked, the SAME constructor the piped
// interactive REPL uses, so the bytes a one-shot run produces are the bytes a
// script already knows how to read.
func chatOnceInput(prompt string) (*chatInput, *chatOnceAnswer, func()) {
	answer := &chatOnceAnswer{}
	input, closeInput := newChatInputCooked(strings.NewReader(prompt+"\n"), io.MultiWriter(os.Stdout, answer))
	return input, answer, closeInput
}

// chatOnceDriverInput builds the input layer for a PIPE-BACK run: the dry
// conversation opener, the driver's prompt feeder (which turns "the next line"
// into "the engine's own answer"), and the answer stream teed to stdout so the
// driver can also DUMP what the channel was given.
//
// The reader's order is the whole loop's schedule: an answer only ever becomes
// a prompt while the REPL is waiting for one, so a prompt can never be built
// from an answer that is still streaming.
func chatOnceDriverInput(d *chatOnceDriver) (*chatInput, *chatOnceAnswer, func()) {
	input, closeInput := newChatInputCooked(&chatOncePromptReader{driver: d}, os.Stdout)
	// The returned accumulator is empty on this face BY DESIGN: the driver takes
	// each turn's text from the REPL's own report (chatOnceSink), which is the
	// text the session records — a second copy teed off stdout could disagree
	// with it, and the tee exists for the one-shot face's "was there any text at
	// all?" question, which the driver answers per turn instead.
	return input, &chatOnceAnswer{}, closeInput
}

// chatOnceHelpSpelled reports whether the caller asked for help (the REPL owns
// the answer to that question).
func chatOnceHelpSpelled(args []string) bool {
	for _, a := range args {
		if a == HelpFlag || a == HelpFlagLong {
			return true
		}
	}
	return false
}

// chatOnceOneLine folds a prompt into the ONE line a chat turn's message is: a
// chat message is a single line on this carrier, so a multi-line prompt is
// joined with spaces rather than truncated at its first break (silently
// dropping two thirds of a prompt is the defect this avoids).
func chatOnceOneLine(text string) string {
	fields := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		if s := strings.TrimSpace(f); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " ")
}

// chatOnceReadPrompt drains up to chatOncePromptLines stdin lines and joins
// them — the `echo '…' | gopi chat --once` shape. A read that does not arrive
// within chatOncePromptReadTimeout, or a read error that produced nothing, is
// reported rather than silently turning into an empty prompt.
func chatOnceReadPrompt(in *os.File) (string, error) {
	type readResult struct {
		text string
		err  error
	}
	done := make(chan readResult, 1)
	go func() {
		var lines []string
		sc := bufio.NewScanner(in)
		sc.Buffer(make([]byte, 0, chatOncePromptScanBuf), chatOncePromptScanBuf)
		for sc.Scan() {
			if line := strings.TrimSpace(sc.Text()); line != "" {
				lines = append(lines, line)
			}
			if len(lines) >= chatOncePromptLines {
				break
			}
		}
		if err := sc.Err(); err != nil && len(lines) == 0 {
			done <- readResult{err: fmt.Errorf("reading the prompt from stdin: %w", err)}
			return
		}
		done <- readResult{text: strings.Join(lines, "\n")}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), chatOncePromptReadTimeout)
	defer cancel()
	select {
	case r := <-done:
		return r.text, r.err
	case <-ctx.Done():
		return "", fmt.Errorf("no prompt on stdin within %v — pass it as %s TEXT or as the positional argument",
			chatOncePromptReadTimeout, chatOnceShortFlag)
	}
}

// chatOnceAnswer accumulates the bytes written to the answer stream. The REPL
// is the only writer of a turn's answer, so no lock is needed; the tee exists
// because "did this turn produce text?" cannot be answered by inspecting the
// caller's stdout (it may be a pipe that has moved on, or a terminal).
type chatOnceAnswer struct {
	buf strings.Builder
}

// Write records one answer fragment.
func (a *chatOnceAnswer) Write(p []byte) (int, error) {
	a.buf.Write(p)
	return len(p), nil
}

// Answer is what the turn produced.
func (a *chatOnceAnswer) Answer() string { return a.buf.String() }

// chatOnceTurnSink is the seam the REPL reports each finished turn through — the
// driver's ONLY inbound channel (see the section header below). It is nil on
// every face but the pipe-back driver, so the interactive carriers keep their
// bytes and cost exactly one nil check per turn.
//
// The REPL calls TurnFinished when a turn returns, so the sink sees BOTH
// outcomes: a completed turn (with the exact text the session records) and a
// FAILED one (which never reaches the session, and would otherwise end the loop
// silently).
type chatOnceTurnSink interface {
	// TurnFinished reports the turn's text and the error, if any. It is the
	// driver's completion signal as well as its content source: the REPL calls it
	// on its own goroutine, INSIDE the turn, and the driver's reader waits on it
	// — which is what makes the next prompt a COMPLETE answer rather than the
	// prefix the REPL's look-ahead feeder would otherwise ask for (see
	// chatOnceDriver.awaitTurn).
	TurnFinished(result string, runErr error)
}

// chatOnceSink is the installed sink; nil unless the pipe-back driver armed it.
var chatOnceSink chatOnceTurnSink = nil

// ---------------------------------------------------------------------------
// THE PIPE-BACK DRIVER (`--turns`)
//
// WHAT IT IS. Without the driver, `--once` runs ONE turn and exits; a shell loop
// (`out=$(… --once); gopi chat --once "$out"`) is the only way to feed the
// engine its own answer. That loop has two defects this face removes: every turn
// is a NEW process (a new session id, a new JSONL, no shared history, so the
// exchange is not one durable run) and nothing bounds it (an engine that keeps
// saying the same thing spins forever, billing as it goes). With `--turns N` the
// SAME process, the SAME agent and the SAME session run up to N turns, each
// answer becoming the next user message, until a stop condition fires. ONE
// session JSONL therefore holds the whole run: session_start, user×N,
// assistant×N, session_end.exit_code.
//
// THE REPL IS STILL THE ENGINE. The driver does not run turns itself: it OWNS
// the REPL's line source (chatInput accepts a carrier-owned reader — the
// interactive terminal editor is one, this is another), so it decides what the
// next user message is, and the REPL runs every turn exactly as it always did
// (same history semantics, same tools, same closeout, same session). A second
// turn-runner would drift from the REPL the first time either changed, which is
// the drift the one-shot face was built to avoid.
//
// WHERE THE SCHEDULE LIVES. A turn is one iteration of the REPL's loop, and that
// loop reads its next line ONLY at the top of an iteration — i.e. after the
// previous turn has been rendered, recorded in `sess.history` and written to the
// session JSONL. So the driver needs no polling, no turn-completion signal and
// no second writer to know when an answer is finished: by the time its reader is
// asked for a line, the answer it is about to build a prompt from is COMPLETE,
// durable, and the session already holds it. That is the whole correctness
// argument of this section, and it is why the driver is a reader rather than a
// turn loop.
//
// WHAT THE SINK IS FOR. One thing the reader cannot see is a turn that FAILED
// (`RunChat` never records a turn whose `Chat` returned an error, and the REPL
// continues to the next line), so the REPL reports that through chatOnceSink —
// the driver's only inbound seam, one call per turn.
// ---------------------------------------------------------------------------

// chatOnceTurnSep delimits one turn's answer from the next on stdout. A line of
// its own between two answers is what makes "split the stream into turns" a
// one-liner for any downstream driver (`awk -v RS=…`, `csplit`, a readline
// loop) — an empty line would not do, because an answer may contain blank lines
// of its own.
const chatOnceTurnSep = "---8<--- turn ---8<---"

// The driver's own bounds (Rule 1a).
const (
	// chatOnceTurnWaitPoll is how often the driver's reader re-checks whether the
	// turn it is waiting on has finished. It bounds the gap between "the REPL
	// finished the turn" and "the next prompt is ready", which is the loop's own
	// overhead — invisible beside a model turn, and small enough that the
	// handshake is not what a caller measures.
	chatOnceTurnWaitPoll = 20 * time.Millisecond
	// chatOnceTurnWaitMax is the failsafe ceiling on that wait: an engine turn
	// that never returns must end the loop with a named reason rather than park
	// the reader forever (Rule 15).
	chatOnceTurnWaitMax = 30 * time.Minute
	// chatOnceMaxSecondsCeiling bounds the wall-clock cap. It is not a policy
	// limit — the caller chooses the cap — but a value beyond a day is a typo (or
	// a stop no run reaches), and refusing it by name beats accepting a loop
	// nothing will end.
	chatOnceMaxSecondsCeiling = 24 * 60 * 60
)

// chatOnceDriverStop is WHY the pipe-back loop ended. It is a named value, not a
// bare string: the reason is printed to the operator (Rule 1b), and a typo in
// one of the two sites would silently change what a driver reads.
type chatOnceDriverStop string

const (
	// chatOnceStopTurns: the --turns cap was reached.
	chatOnceStopTurns chatOnceDriverStop = "turns"
	// chatOnceStopMaxSeconds: the --max-seconds cap was reached.
	chatOnceStopMaxSeconds chatOnceDriverStop = "max-seconds"
	// chatOnceStopNoProgress: an answer identical to the one before it — the
	// engine is restating itself, so feeding it back would loop forever.
	chatOnceStopNoProgress chatOnceDriverStop = "no-progress"
	// chatOnceStopSentinel: the --stop-on sentinel appeared in an answer.
	chatOnceStopSentinel chatOnceDriverStop = "sentinel"
	// chatOnceStopTurnFailed: a turn failed. The loop ends there — the next
	// prompt would be an answer that does not exist — and it is a FAILURE, not a
	// normal stop (the exit contract's 1).
	chatOnceStopTurnFailed chatOnceDriverStop = "turn-failed"
)

// chatOnceDriverOptions are the driver's caps, resolved ONCE from argv.
type chatOnceDriverOptions struct {
	// turns caps the loop. It is also the ARMING value: a negative number is the
	// caller's explicit "uncapped" (the loop then ends on time, no progress or
	// the sentinel), and the caller only reaches here by passing --turns at all.
	turns int
	// maxSeconds caps the loop's wall-clock (0 = no cap).
	maxSeconds int
	// sentinel ends the loop when an answer carries it ("" = none).
	sentinel string
}

// chatOnceDriver is the pipe-back loop's state. It is built ONCE per run and
// consumed by the reader the REPL pulls its lines from.
type chatOnceDriver struct {
	// opts are the driver's caps.
	opts chatOnceDriverOptions
	// emit is where a turn is closed with its delimiter (stdout).
	emit io.Writer
	// turn counts the turns whose answer has been consumed by the driver.
	turn int

	// mu guards the handshake fields below: the REPL writes them from its own
	// goroutine (TurnFinished) while the driver's reader waits on them, and they
	// are the ONLY state two goroutines share.
	mu sync.Mutex
	// finished is how many turns the REPL has completed; consumed is how many of
	// them the driver has turned into a prompt. The reader's wait is exactly the
	// gap between the two (awaitTurn).
	finished int
	consumed int
	// lastResult/lastErr are the most recent completed turn's outcome.
	lastResult string
	lastErr    error
	// start is when the loop began, for the --max-seconds cap.
	start time.Time
	// lastAnswer is the previous turn's answer, for the identical-answer stop.
	lastAnswer string
	// prompt is the message the NEXT turn will send (the caller's prompt, then
	// each answer in turn).
	prompt string
	// seeded reports that the caller's own prompt has been handed to the REPL.
	seeded bool
	// announced reports that stderr has carried the driver's start line.
	announced bool

	// stopped/stopReason/stopText are the loop's end, once a condition fired.
	stopped    bool
	stopReason chatOnceDriverStop
	stopText   string
	// stopErr is the failing turn's error when the stop was chatOnceStopTurnFailed.
	stopErr error
}

// chatOnceDriverFromOptions resolves the driver's caps from a parsed argv.
func chatOnceDriverFromOptions(opts chatOnceOptions) (chatOnceDriverOptions, bool) {
	d := chatOnceDriverOptions{turns: opts.turns, maxSeconds: opts.maxSeconds, sentinel: opts.sentinel}
	return d, opts.turnsSet
}

// newChatOnceDriver builds the loop's state for one run.
func newChatOnceDriver(opts chatOnceDriverOptions, prompt string) *chatOnceDriver {
	return &chatOnceDriver{opts: opts, start: time.Now(), prompt: prompt, emit: os.Stdout}
}

// chatOnceTurnOutcome is one completed turn as the REPL reported it.
type chatOnceTurnOutcome struct {
	result string
	runErr error
}

// awaitTurn waits for the turn the REPL is running and returns its outcome. It
// is the fix for the one hazard this design has: the REPL reads its lines
// through a LOOK-AHEAD feeder goroutine (cli_chat.go — a buffered channel so a
// typed line is never dropped while a turn is in flight), so the driver's reader
// is asked for the next prompt BEFORE the turn it must be built from has
// finished. Reading the answer stream directly would therefore hand the engine a
// PREFIX of its own previous answer (measured: the driver stopped at turn 0 with
// an empty answer, because the feeder asked before any byte had been written).
// Waiting on the sink's own completion signal removes the race instead of
// narrowing it.
func (d *chatOnceDriver) awaitTurn() (chatOnceTurnOutcome, bool) {
	for waited := time.Duration(0); waited < chatOnceTurnWaitMax; waited += chatOnceTurnWaitPoll {
		d.mu.Lock()
		if d.finished > d.consumed {
			d.consumed++
			out := chatOnceTurnOutcome{result: d.lastResult, runErr: d.lastErr}
			d.mu.Unlock()
			return out, true
		}
		d.mu.Unlock()
		time.Sleep(chatOnceTurnWaitPoll)
	}
	// The ceiling is a failsafe, not a stop condition: without it a turn that
	// never returns would park the reader with no diagnostic at all (Rule 15).
	return chatOnceTurnOutcome{}, false
}

// announce prints the driver's ONE start line on stderr: what the loop will do
// and what could end it. A redirected stderr is the only place a caller learns
// that a long-running run is a DRIVER — and therefore that the caps exist.
func (d *chatOnceDriver) announce() {
	if d.announced {
		return
	}
	d.announced = true
	stops := fmt.Sprintf("%s %d", chatOnceMaxTurnsFlag, d.opts.turns)
	if d.opts.turns < 0 {
		stops = chatOnceMaxTurnsFlag + " uncapped"
	}
	if d.opts.maxSeconds > 0 {
		stops += fmt.Sprintf(", %s %d", chatOnceMaxSecondsFlag, d.opts.maxSeconds)
	}
	stops += ", no progress (a repeated answer)"
	if d.opts.sentinel != "" {
		stops += fmt.Sprintf(", %s %q", chatOnceStopFlag, d.opts.sentinel)
	}
	fmt.Fprintf(os.Stderr, "%spipe-back driver armed — each turn's answer becomes the next prompt; stops at: %s\n",
		chatOncePrefix, stops)
}

// TurnFinished is a turn's outcome as the REPL saw it: the driver's completion
// signal (awaitTurn) and the content of the next prompt. It is called from the
// REPL's goroutine, after the answer has been rendered and recorded, so a turn
// the driver reads here is complete, durable, and identical to the row the
// session holds.
func (d *chatOnceDriver) TurnFinished(result string, runErr error) {
	d.mu.Lock()
	d.finished++
	d.lastResult = result
	d.lastErr = runErr
	d.mu.Unlock()
}

// stop records the loop's end; the first reason found wins.
func (d *chatOnceDriver) stop(reason chatOnceDriverStop, text string) {
	if d.stopped {
		return
	}
	d.stopped = true
	d.stopReason = reason
	d.stopText = text
}

// next consumes the turn that just finished and returns the next prompt.
//
// It is called by the reader at the ONE moment the REPL is between turns, so an
// answer here is complete and durable. The answer is: dumped with its delimiter
// (emit — the machine channel), evaluated against every stop condition (the
// sentinel, no progress, the two caps), and then — only if none fired — adopted
// as the next user message.
func (d *chatOnceDriver) next() (string, bool) {
	outcome, ok := d.awaitTurn()
	if !ok {
		// The turn never completed inside its failsafe ceiling: end the loop with
		// a named reason rather than a prompt built from nothing.
		d.stopErr = fmt.Errorf("no turn completed within %v", chatOnceTurnWaitMax)
		d.stop(chatOnceStopTurnFailed, d.stopErr.Error())
		return "", false
	}
	if outcome.runErr != nil {
		// A failed turn: the loop's ONE hard error (an answer that does not exist
		// cannot become the next prompt), and the exit contract's 1.
		d.stopErr = outcome.runErr
		d.stop(chatOnceStopTurnFailed, outcome.runErr.Error())
		return "", false
	}
	answer := strings.TrimSpace(outcome.result)
	if answer == "" {
		// Nothing to feed back and nothing to report: the one-shot face treats a
		// completed turn with no text as a failure, and a loop cannot run a turn
		// whose prompt would be empty (Rule 15 — no silent spin).
		d.stopErr = fmt.Errorf("the turn completed without producing any text")
		d.stop(chatOnceStopTurnFailed, d.stopErr.Error())
		return "", false
	}
	// Every consumed turn is closed on the machine channel, the stopping one
	// included: a driver reads the loop as a sequence of turns, so a final turn
	// without its delimiter would be the one a naive splitter appends to its
	// predecessor. The emission order is the loop's own order — the separator for
	// turn N is written before turn N+1's text is streamed, because the REPL
	// cannot start turn N+1 until this reader returns its line.
	stopping := d.resolveStop(answer)
	d.emitTurn()
	if stopping {
		return "", false
	}
	d.prompt = answer
	return d.prompt, true
}

// emitTurn closes one turn on the machine channel with the turn's delimiter.
//
// It writes the DELIMITER ONLY, never the answer: the answer is already there —
// the REPL streamed it to stdout as it was produced (OnText → the renderer →
// flow) — so re-emitting it here would print every answer twice, which is
// exactly the kind of "looks fine until a driver parses it" defect this face
// exists to avoid. The separator follows the answer's own final newline (a
// streamed answer does not guarantee a trailing one), so the delimiter is always
// alone on its line and a downstream splitter needs no lookahead.
func (d *chatOnceDriver) emitTurn() {
	if d.emit == nil {
		return
	}
	fmt.Fprintf(d.emit, "\n%s\n", chatOnceTurnSep)
}

// resolveStop evaluates the stop conditions against the turn that just finished
// and reports whether the loop ends. It is the driver's whole policy in one
// place, so "which condition fired" has exactly ONE answer.
func (d *chatOnceDriver) resolveStop(answer string) bool {
	// The turn counter belongs HERE, not at the call site: this is the ONE place
	// a finished turn is evaluated, so the counter the --turns cap and the stop
	// notice read cannot disagree with the number of turns actually consumed.
	d.turn++
	if d.opts.sentinel != "" && chatOnceSentinelFound(answer, d.opts.sentinel) {
		d.stop(chatOnceStopSentinel, d.opts.sentinel)
		return true
	}
	// No progress is checked BEFORE the caps: when the engine restated itself AND
	// a cap was reached on the same turn, "the engine was not making progress" is
	// the more useful sentence (the cap would have fired anyway).
	if d.lastAnswer != "" && answer == d.lastAnswer {
		d.stop(chatOnceStopNoProgress, "")
		return true
	}
	d.lastAnswer = answer
	if d.opts.turns >= 0 && d.turn >= d.opts.turns {
		d.stop(chatOnceStopTurns, strconv.Itoa(d.opts.turns))
		return true
	}
	if d.opts.maxSeconds > 0 && time.Since(d.start) >= time.Duration(d.opts.maxSeconds)*time.Second {
		d.stop(chatOnceStopMaxSeconds, strconv.Itoa(d.opts.maxSeconds))
		return true
	}
	return false
}

// chatOnceSentinelFound reports whether the answer carries the sentinel as a
// WHOLE WORD: `DONE` matches `… DONE.` and never `ABANDONED`. The word boundary
// is spelled with characters an answer's prose does not glue a word to, because
// \b cannot be relied on across the regexp dialects a caller may be thinking in.
func chatOnceSentinelFound(answer, sentinel string) bool {
	if sentinel == "" {
		return false
	}
	for i := 0; i < len(answer); {
		k := strings.Index(answer[i:], sentinel)
		if k < 0 {
			return false
		}
		at := i + k
		before := at == 0 || strings.IndexByte(chatOnceSentinelDelims, answer[at-1]) >= 0
		end := at + len(sentinel)
		after := end >= len(answer) || strings.IndexByte(chatOnceSentinelDelims, answer[end]) >= 0
		if before && after {
			return true
		}
		i = at + 1
	}
	return false
}

// chatOnceSentinelDelims delimits a whole-word sentinel match (see above).
const chatOnceSentinelDelims = " \t\n\r.,;:!?()[]{}\"'`*_—–-"

// notice is the ONE line the operator reads when the loop ends: the reason, and
// (for a cap) the value it stopped at. It goes to stderr, never stdout — stdout
// is the answers.
func (d *chatOnceDriver) notice() {
	if !d.stopped {
		return
	}
	switch d.stopReason {
	case chatOnceStopTurns:
		fmt.Fprintf(os.Stderr, "%sdriver stopped after %d turn(s): %s %s reached\n",
			chatOncePrefix, d.turn, chatOnceMaxTurnsFlag, d.stopText)
	case chatOnceStopMaxSeconds:
		fmt.Fprintf(os.Stderr, "%sdriver stopped after %d turn(s) and %v: %s %s reached\n",
			chatOncePrefix, d.turn, time.Since(d.start).Round(time.Second), chatOnceMaxSecondsFlag, d.stopText)
	case chatOnceStopNoProgress:
		fmt.Fprintf(os.Stderr, "%sdriver stopped after %d turn(s): no progress — the answer repeated the one before it\n",
			chatOncePrefix, d.turn)
	case chatOnceStopSentinel:
		fmt.Fprintf(os.Stderr, "%sdriver stopped after %d turn(s): %s %q appeared in the answer\n",
			chatOncePrefix, d.turn, chatOnceStopFlag, d.stopText)
	case chatOnceStopTurnFailed:
		fmt.Fprintf(os.Stderr, "%sdriver stopped after %d turn(s) — turn failed: %v\n",
			chatOncePrefix, d.turn, d.stopErr)
	}
}

// chatOncePromptReader is the driver's line source: io.MultiReader drains the
// dry run's line first, then reaches this reader, which supplies ONE prompt per
// turn for the rest of the run. It satisfies lineedit's io.Reader seam (the same
// one the cooked input constructs over any reader), so the driver needs no new
// input type — the REPL's own convention reads it.
type chatOncePromptReader struct {
	driver *chatOnceDriver
	// pending holds the prompt of the turn that has just been fed, so a turn's
	// second read (there is none today, but the contract must not depend on that)
	// can never re-emit it as a new message.
	pending string
}

// Read returns the next line the REPL will treat as a user message: the caller's
// own prompt on the first read, then each turn's answer (see chatOnceDriver.next)
// until a stop condition fires — at which point the reader reports EOF and the
// REPL ends the session through its own closeout.
func (r *chatOncePromptReader) Read(p []byte) (int, error) {
	d := r.driver
	if r.pending != "" {
		n := copy(p, r.pending)
		r.pending = r.pending[n:]
		d.announce()
		return n, nil
	}
	if d.stopped {
		return 0, io.EOF
	}
	var (
		line string
		ok   bool
	)
	if !d.seeded {
		d.seeded = true
		line, ok = d.prompt, true
	} else {
		line, ok = d.next()
		if !ok {
			return 0, io.EOF
		}
	}
	r.pending = line + "\n"
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	d.announce()
	return n, nil
}
