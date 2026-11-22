// chat_context.go — `/context [N] [--out PATH]`: export the context the model
// received at one turn, as a FILE, and print ONE line.
//
// WHY REPL-NATIVE AND NOT A slashCommands ENTRY (the same reasoning /screen,
// /info, /queue and /voice record). This command's subject is per-PROCESS state
// the shared table cannot see: the LIVE agent's raw HTTP log — the file THIS run
// is appending its request bodies to — plus this session's own directory. A
// table entry would have to be handed the agent and a session directory, and
// every other carrier of the table (the web UI) would then have to document a
// command it cannot answer. The word, its parser, its renderer call and its
// usage text therefore live in ONE file (this one), it joins chatSessionWords so
// the completion menu, the refusal vocabulary and /help learn it from one place,
// and it never consumes a model turn.
//
// ONE RENDERER, TWO DOORS (Rule 14). The export itself is internal/ctxdump:
// `gopi session context` reaches it through cli_session_context.go, this arm
// reaches it directly. There is no second renderer, no second truncation rule
// and no second tokenizer here — only the argument parsing (which follows
// /screen's shape: a bare turn number, one at most, unknown flags refused by
// name, a help word) and the console notes.
//
// WHY THE SCREEN GETS ONE LINE. The export is tens of thousands of tokens. The
// operator's ruling (2026-11-07) is explicit: printing it into the conversation
// re-sends the whole context AND leaves it in the history, so every later turn
// pays for it again. The screen carries the path and the size; the file carries
// the payload. Unlike /screen there is NOTHING to inject into the next turn —
// this command is for the PERSON reading, so it parks no pending note.

package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/ctxdump"
)

// The command's vocabulary. Every word the switch, the parser, the usage page
// and the completion menu compare against or print is declared once
// (Rules 1/13/14).
const (
	// chatContextCommandName is the slash word that owns this file.
	chatContextCommandName = "context"
	// chatContextOutFlag names the destination override: `--out PATH`.
	chatContextOutFlag = "--out"
	// chatContextHelpWord / chatContextHelpFlagShort / chatContextHelpFlagLong all
	// answer from chatContextUsage (the /screen convention).
	chatContextHelpWord      = "help"
	chatContextHelpFlagShort = "-h"
	chatContextHelpFlagLong  = "--help"
	// chatContextValueSep joins a flag to its value (`--out=PATH`).
	chatContextValueSep = "="
	// chatContextFlagPrefix is what makes a token a flag rather than a turn.
	chatContextFlagPrefix = "-"
	// chatContextMaxTurnDigits bounds the digits a turn number may occupy; a
	// session cannot reach 10^9 requests, so a longer run is not one.
	chatContextMaxTurnDigits = 9
)

// chatContextUsage is `/help context` and `/context help` — the ONE definition
// (chatNativeUsage and chatReplSlashVocabulary both answer from it).
var chatContextUsage = `Usage: /context [N] [--round N] [--out PATH] [--system | --user]
Export the context the model ACTUALLY RECEIVED at one TURN — the system prompt
VERBATIM as it was sent, then every message in order (text, thinking, tool calls
with their input, tool results, images summarized as type+size+sha256) — to a
FILE, and print one line. Nothing is clipped and there is no ` + "`--full`" + ` flag: the
file's body is the request's own bytes.

  N            the TURN to export, written as a bare number — the SAME number
               ` + "`gopi session stats <id>`" + ` prints on its ` + "`turns`" + ` row (NOT
               ` + "`session log --turns`" + `, which prints the human-turn subset).
               ` + "`stats`" + ` is named rather than ` + "`session show`" + ` because ` + "`show`" + `
               prints NO turns row for a ` + "`kind=run`" + ` session — every
               dispatched task — while ` + "`stats`" + ` answers for both species.
               With no N, the last TURN is exported: the context the model is
               running with now
  --round N    DEBUG AXIS, not the ordinary address: export ROUND N instead —
               one HTTP request/response record, written to
               ` + ctxdump.RoundFileNamePattern + ` and headed ROUND. Use it to drill
               into a turn's internals; the ordinary export names TURNS only,
               because a round is a model-layer detail (measured: a 2-turn
               session held 336 rounds, and this verb used to call the last of
               them "turn 336")
  --system     ONE SECTION of the request: ` + ctxdump.SystemHeading + ` and
               ` + ctxdump.ToolsHeading + ` and the ` + ctxdump.RequestHeading + ` table, with
               NO messages. It is the model's fixed prefix — the half that stays
               identical across a session's turns — and it lands in its OWN file,
               ` + ctxdump.SystemFileNamePattern + `, never on the full page
  --user       THE OTHER SECTION: the ` + ctxdump.RequestHeading + ` table and
               ` + ctxdump.MessagesHeading + `, with no system prompt and no tool
               definitions. Lands in ` + ctxdump.UserFileNamePattern + `
               ` + ctxdump.SystemSelector + ` with ` + ctxdump.UserSelector + ` is REFUSED: an export shows ONE
               side, so the two flags together would be the full page under a
               filtered name
  --out PATH   write to PATH instead of this session's own
               ` + ctxdump.FileNamePattern + ` (a TURN) or
               ` + ctxdump.RoundFileNamePattern + ` (a ROUND), with
               ` + ctxdump.SystemFileNamePattern + ` / ` + ctxdump.UserFileNamePattern + `
               for the two section views — each named for the address and the
               section the file HOLDS

The screen shows ONE line:

  ` + fmt.Sprintf(strings.TrimRight(ctxdump.LineFormat, "\n"), "<path>", 0, 0) + `

and nothing else — the payload is tens of thousands of tokens, so printing it
here would re-send it into the conversation and charge every later turn for it.
A section export extends that same line with a clause naming the section.

YOU DO NOT HAVE TO CHOOSE AN AXIS. With no argument this exports the turn that
just finished, so the ordinary case is the bare word /context.

THE FULL PAGE IS THE EVIDENCE and its name and bytes are unchanged:
` + ctxdump.FileNamePattern + ` still holds the request's own bytes, so it can be
diffed against the raw HTTP log. A section export is a DERIVED VIEW under a
DIFFERENT name — the two never share a file.

` + sessionContextInFlightNotice + `

Nothing is injected into the next turn (this is a file for YOU to read), and no
model turn is spent: the export is local and immediate. The same export is
reachable from a shell as ` + "`gopi session context <id> --last`" + ` — one
renderer, two doors.

A run whose raw HTTP log is disabled (` + "`--no-raw-log`" + `, or one that failed to
open) has no request bodies to export, and /context says so instead of writing
an empty file.`

// chatContextRequest is one parsed `/context` argument list.
type chatContextRequest struct {
	// Turn is the TURN to export; 0 means "the last one". A turn number is a
	// POSITION, so 0 is refused as an argument rather than read as "newest".
	Turn int
	// Round is the ROUND (`--round N`) to export instead of a turn; 0 means the
	// caller named none. A ROUND is one HTTP request/response — a turn
	// routinely takes many — so the two are separate fields, never one number
	// whose meaning depends on which flag produced it.
	Round int
	// Path is the `--out` override, "" when the caller did not name one.
	Path string
	// Section is WHICH PART of the request to export: the full page (the zero
	// value, the only behaviour before sections existed), the system half, or
	// the message half. It is resolved from the two selectors by
	// ctxdump.SectionFor, so the refusal of "--system --user" is one sentence in
	// one place rather than a branch in each carrier's parser.
	Section ctxdump.Section
	// Help asks for the usage block.
	Help bool
}

// parseChatContextArgs parses the argument list, refusing every shape it does
// not accept BY NAME — the caller who typed `--outt=x` believed they chose a
// destination, and the caller who typed `58x` believed they chose a turn;
// silently exporting something else would file their evidence where they will
// not look (Rule 15).
func parseChatContextArgs(args []string) (chatContextRequest, error) {
	var req chatContextRequest
	turnSet := false
	systemSel, userSel := false, false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == chatContextHelpWord || a == chatContextHelpFlagShort || a == chatContextHelpFlagLong:
			req.Help = true
		case a == ctxdump.SystemSelector:
			systemSel = true
		case a == ctxdump.UserSelector:
			userSel = true
		case a == chatContextOutFlag:
			if i+1 >= len(args) {
				return req, fmt.Errorf("%s needs a PATH", chatContextOutFlag)
			}
			i++
			req.Path = args[i]
		case strings.HasPrefix(a, chatContextOutFlag+chatContextValueSep):
			req.Path = strings.TrimPrefix(a, chatContextOutFlag+chatContextValueSep)
			if req.Path == "" {
				return req, fmt.Errorf("%s needs a PATH", chatContextOutFlag)
			}
		case a == chatContextRoundFlag:
			if i+1 >= len(args) {
				return req, fmt.Errorf("%s needs a NUMBER", chatContextRoundFlag)
			}
			i++
			n, rerr := chatContextParseTurn(args[i])
			if rerr != nil {
				return req, rerr
			}
			if req.Round != 0 {
				return req, fmt.Errorf("two round numbers (%d and %d) — /context takes ONE: the context of a single round", req.Round, n)
			}
			req.Round = n
		case strings.HasPrefix(a, chatContextRoundFlag+chatContextValueSep):
			n, rerr := chatContextParseTurn(strings.TrimPrefix(a, chatContextRoundFlag+chatContextValueSep))
			if rerr != nil {
				return req, rerr
			}
			if req.Round != 0 {
				return req, fmt.Errorf("two round numbers (%d and %d) — /context takes ONE: the context of a single round", req.Round, n)
			}
			req.Round = n
		case strings.HasPrefix(a, chatContextFlagPrefix):
			return req, fmt.Errorf("unknown argument %q — usage: /context [N] [--round N] [--out PATH] [--system | --user]", a)
		default:
			turn, err := chatContextParseTurn(a)
			if err != nil {
				return req, err
			}
			if turnSet {
				return req, fmt.Errorf("two turn numbers (%d and %d) — /context takes ONE: the context of a single turn", req.Turn, turn)
			}
			req.Turn = turn
			turnSet = true
		}
	}
	if turnSet && req.Round != 0 {
		return req, fmt.Errorf("a bare number (%d) and %s %d name two different address spaces — a TURN routinely takes many ROUNDS, so say which you want", req.Turn, chatContextRoundFlag, req.Round)
	}
	// THE TWO SECTION SELECTORS ARE RESOLVED HERE, THROUGH THE PACKAGE THAT OWNS
	// THE SECTIONS — so "--system --user" is refused by ONE sentence at ONE place
	// and both carriers (the REPL and the shared table's /context) inherit it.
	sec, serr := ctxdump.SectionFor(systemSel, userSel)
	if serr != nil {
		return req, serr
	}
	req.Section = sec
	return req, nil
}

// chatContextParseTurn reads the ONE positional: a bare decimal, 1 or greater.
// The three shapes that will really be tried are refused separately because
// they mean different things (zero is a position, a sign is a counting
// convention this command has not got, a letter is a typo).
func chatContextParseTurn(arg string) (int, error) {
	if arg == "" {
		return 0, fmt.Errorf("an empty argument — /context takes a number, or nothing for the last turn")
	}
	sign := ""
	digits := arg
	if strings.HasPrefix(digits, "+") || strings.HasPrefix(digits, "-") {
		sign = digits[:1]
		digits = digits[1:]
	}
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			// AXIS-NEUTRAL WORDING ON PURPOSE: this parser serves BOTH the bare
			// turn operand and `--round`, and a message that said "turn" while
			// the caller typed --round would name the wrong address space back
			// at them (the defect this epic removes, one layer down).
			return 0, fmt.Errorf("%q is not a whole number — /context takes a TURN (e.g. /context 2) or a ROUND (e.g. /context --round 332), or nothing for the last turn", arg)
		}
	}
	if len(digits) > chatContextMaxTurnDigits {
		return 0, fmt.Errorf("%q is not a whole number — past %d digits", arg, chatContextMaxTurnDigits)
	}
	if digits == "" {
		return 0, fmt.Errorf("%q is not a whole number — it carries a sign and no digits", arg)
	}
	if sign == "-" {
		return 0, fmt.Errorf("%q is not a whole number — there is no counting back from the end; name the turn or round you want, or pass none for the last turn", arg)
	}
	n := 0
	for i := 0; i < len(digits); i++ {
		n = n*10 + int(digits[i]-'0')
	}
	if n == 0 {
		return 0, fmt.Errorf("%q is not a turn number — requests are numbered from 1; pass no argument for the LAST one", arg)
	}
	return n, nil
}

// The notes this command writes to the console. Each names the file, its size
// and the turn it HOLDS, because an artifact the operator cannot find — or
// whose turn they have to guess — is a command that did nothing (Rule 15).
const (
	// chatContextNoLogReason is the honest degradation: this run records no
	// request bodies, so there is nothing to export. It is a REASON, not a whole
	// console line — the one refusal format below wraps it, so the sentence an
	// operator reads is the same shape whatever went wrong.
	chatContextNoLogReason = "this run is not recording its raw HTTP log (--no-raw-log, or the log could not be opened), so there are no request bodies to export"
	// chatContextRefusalFormat reports a failed export (or the degradation above).
	chatContextRefusalFormat = "[gopi] /context: %v\n"
	// chatContextNoDirReason is the refusal for a session with no directory and no
	// `--out`: there is nowhere to write, and inventing a destination would file
	// the artifact where the operator will not look.
	chatContextNoDirReason = "this session has no directory to write the export into; pass %s PATH"
	// chatContextNoRecordReason is the refusal for a carrier that has a raw HTTP
	// log but no session RECORD: turn boundaries live in the record, so without it
	// this run can export a ROUND and nothing else — and it says which flag does
	// that instead of filing a request body under a turn number it never measured.
	chatContextNoRecordReason = "this run has no session record, so it cannot resolve a TURN (turn boundaries are recorded in the session's JSONL, not in the raw HTTP log) — pass " + chatContextRoundFlag + " N to export one ROUND instead"
	// chatContextRoundFlag selects one HTTP request: `/context --round N`.
	chatContextRoundFlag = "--round"
)

// chatContextHandleLine is the REPL arm's whole body: parse, export, report. It
// never calls the model and never blocks on one.
func chatContextHandleLine(input *chatInput, ag *agent.Agent, sess *chatSession, msg string) {
	args := strings.Fields(strings.TrimSpace(msg))
	if len(args) > 0 {
		// Drop the command word the switch already matched.
		args = args[1:]
	}
	req, err := parseChatContextArgs(args)
	if err != nil {
		input.WriteNotice(fmt.Sprintf(chatContextRefusalFormat, err))
		return
	}
	if req.Help {
		input.WriteLines(chatContextUsage)
		return
	}
	summary, err := chatContextExport(ag, sess, req)
	if err != nil {
		input.WriteNotice(fmt.Sprintf(chatContextRefusalFormat, err))
		return
	}
	// THE CONSOLE GETS THE ONE LINE AND NOTHING ELSE — including
	// Summary.Notes(). The historical-collision note NAMES the pre-axis file
	// (`context-turn6.md`), and on a 3-turn/6-round session that literal is
	// exactly what a reader of the console must not meet: it is a round number
	// wearing the word "turn", in the very channel the axis fix cleans. The
	// note is a diagnostic about a directory, so it belongs to the door that
	// has a second stream for diagnostics — `gopi session context` writes it to
	// stderr. A console has no such stream, so it carries the export and no
	// commentary (TestContextSlashWritesTheArtifactAndPrintsOneLine pins this).
	input.WriteNotice(summary.Line())
}

// chatContextExport writes the artifact for one turn of THIS run's raw log.
//
// The log path comes from the AGENT's own raw logger — the file this process is
// appending to — and never from a re-derivation: a second derivation is how two
// surfaces come to describe one session differently, and here it would export
// some other run's context while saying it was this one's.
func chatContextExport(ag *agent.Agent, sess *chatSession, req chatContextRequest) (ctxdump.Summary, error) {
	rawLog, record, dir := "", "", ""
	if ag != nil && ag.RawLog != nil {
		rawLog = ag.RawLog.Path()
	}
	if ag != nil && ag.SessionLog != nil {
		record = ag.SessionLog.Path()
	}
	if sess != nil {
		dir = sess.sessionDir
	}
	return chatContextExportFrom(rawLog, record, dir, req)
}

// chatContextExportFrom is the export with the inputs named as VALUES rather
// than as the console's own types, so the SHARED table entry (/context, via
// CommandContext.Session) reaches exactly this code without a second copy — one
// engine, two doors, and the doors differ only in where the paths come from.
//
// THE THIRD PATH IS THE SESSION RECORD, and it is what makes the export
// TURN-scoped: the turn boundaries live in the session's own JSONL (the ONE
// algorithm, gopilog.SessionTurnBounds), and the raw log only carries request
// bodies. A carrier that supplied a raw log but no record can export a ROUND and
// nothing else, and it says so by name rather than filing a request body under a
// turn number it never measured.
//
// The refusal for a run with no directory comes BEFORE the log is even read: a
// caller who named no destination and has no session directory has nowhere to
// write, and reading a 40 MB log first would only delay the same answer.
func chatContextExportFrom(rawLogPath, recordPath, sessionDir string, req chatContextRequest) (ctxdump.Summary, error) {
	if rawLogPath == "" {
		return ctxdump.Summary{}, errors.New(chatContextNoLogReason)
	}
	if sessionDir == "" && req.Path == "" {
		return ctxdump.Summary{}, fmt.Errorf(chatContextNoDirReason, chatContextOutFlag)
	}
	var addr ctxdump.Address
	if req.Round > 0 {
		addr = ctxdump.RoundAddress(req.Round)
	} else {
		if recordPath == "" {
			return ctxdump.Summary{}, errors.New(chatContextNoRecordReason)
		}
		var aerr error
		addr, aerr = ctxdump.TurnAddressFor(recordPath, req.Turn)
		if aerr != nil {
			return ctxdump.Summary{}, aerr
		}
	}
	reqx, err := ctxdump.RequestAtTurn(rawLogPath, addr)
	if err != nil {
		return ctxdump.Summary{}, err
	}
	return ctxdump.WriteToSection(req.Path, sessionDir, reqx, addr, req.Section)
}
