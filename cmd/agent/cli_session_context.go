// cli_session_context.go — `gopi session context <id>`: export the context the
// model ACTUALLY RECEIVED at one turn, as a file, and print one line.
//
// WHAT QUESTION IT ANSWERS. Not "what happened in this session" (that is
// `session log` / `session replay`) but "what was in front of the model at this
// round" — the system prompt as sent, every message in order, every tool call
// and result, every image summarized. The source is the raw HTTP log, whose
// REQUEST record IS the exact body handed to the provider
// (internal/checkpoint's package doc states that contract; resume depends on
// it). So this verb adds no capture: it renders one already-recorded request.
//
// WHY THE SCREEN GETS ONE LINE AND THE FILE GETS EVERYTHING. The export is tens
// of thousands of tokens. Printing it into the conversation re-sends the whole
// context AND leaves it in the history, so every later turn pays for it again.
// The operator's ruling is the same one `/screen` records: the screen carries a
// path and a size, never the payload.
//
// WHERE THE TURN COMES FROM — AND WHY IT IS NOT "THE Nth REQUEST".
// A turn is not a round: one turn routinely takes many HTTP requests (measured:
// a 2-turn session held 336 requests), so `--turn N` resolves through the SAME
// turn algorithm every other surface reads (gopilog.SessionTurnSpans ←
// SessionTurnBounds ← sessionevent.IsTurnOpeningRow), against the session's own
// JSONL record — the raw log only carries request bodies. The artifact is then
// named and headed for THAT turn, with the interval of rounds it covered, and
// the page renders the last request it sent. A caller who really means one HTTP
// exchange says `--round N` and gets an artifact that says ROUND.
//
// That split is the repair of one measured defect: this verb used to name its
// output `context-turn336.md` on that 2-turn session — the 336th request under
// the word every other surface reserves for a turn.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nickhuang99/gopi/internal/ctxdump"
	"github.com/nickhuang99/gopi/internal/gopilog"
)

// The sub-verb's vocabulary. Every word the dispatcher, the argument parser,
// the usage page and the contract test compare or print is declared once
// (Rules 1/13).
const (
	// sessionSubcmdContext is the sub-verb word.
	sessionSubcmdContext = "context"

	// sessionContextLastFlag selects the newest request (the current context).
	sessionContextLastFlag = "--last"
	// sessionContextTurnFlag selects one turn by number: `--turn N`.
	sessionContextTurnFlag = "--turn"
	// sessionContextRoundFlag selects one HTTP request by number: `--round N`.
	// It exists because a turn is not a request: a turn routinely takes many
	// rounds, so a caller who wants round 336 of a 2-turn session has an address
	// for it — and it is spelled with a DIFFERENT word, so it can never be read
	// as a turn number.
	sessionContextRoundFlag = "--round"
	// sessionContextOutFlag overrides the destination: `--out PATH`.
	sessionContextOutFlag = "--out"
	// sessionContextValueSep joins a flag to its value (`--turn=58`), the same
	// spelling family the other session verbs use.
	sessionContextValueSep = "="
	// sessionContextFlagPrefix is what makes a token a flag rather than the
	// session operand.
	sessionContextFlagPrefix = "-"

	// sessionContextUsageFingerprint is the usage page's first line — the
	// string the help contract pins (cli_contracts_test.go).
	sessionContextUsageFingerprint = "gopi session context — export the context the model received at one turn"

	// sessionContextAxisRules is the page's statement of the TWO AXES this verb
	// has, and it is a CONSTANT rather than prose inside the page for one
	// reason: the claim is the thing that drifted, so a contract test has to be
	// able to pin the exact words (cli_session_context_axis_test.go references
	// it — Rule 13: a constant that exists is referenced, never only defined).
	//
	// What it replaces: the page used to say "The default selector is --last."
	// That sentence names ONE FLAG as if it were the whole default for a verb
	// with two independent axes, and it is wrong in BOTH directions — --last
	// never picks the session (a session id is required), and the selector the
	// parser actually defaults is the turn AXIS TAKEN WHOLE (all three flags
	// omitted), which happens to name the same page --last does.
	sessionContextAxisRules = `TWO AXES, AND ONLY ONE OF THEM EVER DEFAULTS:

  SESSION — NEVER DEFAULTED. The <session-id> operand is required on EVERY
  call; no flag supplies it. "gopi session context --last" on its own is
  REFUSED — "which session? give a session id" — because "the newest session"
  would be a guess, and this verb exports EVIDENCE: a page silently selected
  from an unknown session cannot be told apart from a page about the one you
  meant.

  TURN/ROUND — the axis --last, --turn N and --round N address, and the ONLY
  axis with a default. Omit ALL THREE and the export takes the session's NEWEST
  TURN, the same page --last names. So --last is the EXPLICIT spelling of that
  default: never the default itself, and never a selector for the session.`

	// sessionContextInFlightNotice is the ONE statement of a property of the
	// TURN axis that nothing on either page used to say: a turn that has not
	// ENDED is still SENDING requests, so exporting it more than once yields
	// pages that GROW.
	//
	// MEASURED, and the reason it is stated rather than left to be discovered
	// (session 3965695b-6f70-4067-8b61-68628d7a6dfc, 2026-11-17): the same
	// command with the same N — `session context <id> --user` — wrote 147419,
	// 151555 and 153127 bytes within three minutes. The address did not change;
	// the turn had not finished. The page for a COMPLETED turn is stable, which
	// is why the ordinary case (export the turn that just finished) does not hit
	// this, and why silent discovery of it is expensive: a reader diffing two
	// of those pages reasonably concludes the export is non-deterministic.
	//
	// WHY IT IS A CONSTANT: both carriers render it — the sub-verb's page here
	// and the chat REPL's `/context` page (chat_context.go) — and a property
	// stated twice drifts (Rule 14). One definition, two readers.
	sessionContextInFlightNotice = `A TURN THAT HAS NOT ENDED IS STILL GROWING. An export renders the LAST
request that turn has sent so far, so exporting a turn that is still running
twice gives two pages of different sizes (measured: 147419 → 151555 → 153127
bytes in three minutes on one unended turn). The address is stable; the
content is not, until the turn ends. A finished turn exports a page that is
identical every time.`

	// sessionContextRetiredDefaultClaim is the sentence the page carried before
	// the two-axis statement, kept as a named constant because the contract
	// test asserts its ABSENCE (a discarded wording has to stay discarded).
	sessionContextRetiredDefaultClaim = "The default selector is --last."

	// sessionContextNoSessionMessage is the refusal the SESSION axis raises when
	// no operand names a session. It is a constant because TWO carriers quote it
	// — the parser, which prints it, and the help page, which must not
	// paraphrase its own error — and the axis contract test compares them
	// (cli_session_context_axis_test.go; Rule 14: one definition, both readers).
	sessionContextNoSessionMessage = "which session? give a session id"
)

// sessionContextUsage is the sub-verb's own page. It is a VAR so the claims it
// makes are rendered from the constants the code enforces — the page cannot
// promise a spelling the parser does not accept, nor state an axis rule the
// parser contradicts (sessionContextAxisRules is the two-axis statement, and it
// is rendered here rather than typed inline so the contract test can pin the
// exact words; cli_session_context_axis_test.go).
var sessionContextUsage = sessionContextUsageFingerprint + `

The artifact is the CONTEXT, not the conversation: the system prompt VERBATIM as
it was sent, then every message in order (text, thinking, tool calls with their
input, tool results, images summarized as type+size+sha256).

VERBATIM IS THE ONLY MODE. Nothing is clipped and there is no ` + "`--full`" + ` flag to
add: the page's body is the request's own bytes, so it can be compared against
the raw HTTP log with diff. (A retired per-block budget,
DefaultSystemChars=20000, used to cut the system prompt to 6% of itself while the
heading claimed the whole size. It is gone.)

Usage:
  gopi session context <session-id> [--out PATH]
      --last      the newest TURN — the context the model is running with now
      --turn N    TURN N of this session (1-based — the SAME number
                  ` + "`gopi session stats <id>`" + ` prints on its ` + "`turns`" + ` row,
                  whose boundaries come from one algorithm shared by every
                  surface; NOT ` + "`session log --turns`" + `, which prints the
                  human-turn SUBSET and can be fewer).
                  THE WITNESS IS ` + "`stats`" + `, NOT ` + "`show`" + `: a ` + "`kind=run`" + ` session
                  (every dispatched task) prints NO turns row in ` + "`session show`" + `
                  at all, so ` + "`show`" + ` cannot witness this count — ` + "`session list`" + `
                  says so on the row itself ("turns none (no chats row)").
                  ` + "`stats`" + ` answers for BOTH species, and the refusal below
                  names it for that reason.
                  A TURN is one input line — a turn is NOT a request: the page
                  holds the LAST such input's request, one request per request
                  row in the session record — measured: a 42-turn session held
                  211 request rows, a 16-turn one held 25. So --turn 3 and
                  --turn 4 are NOT three requests apart, and page N is NOT a
                  prefix of page N+1: a turn that folds its context re-sends a
                  SHORTER list, so the pages nest only where no fold happened.
                  A probe is a real send with no request row, so it is not
                  addressable here — this walk counts request rows only.
      --round N   DEBUG AXIS, not the ordinary address: ROUND N is one HTTP
                  request/response record in the log's own request sequence,
                  written to ` + ctxdump.RoundFileNamePattern + ` and headed ROUND.
                  Use it to drill into a turn's internals; the ordinary export
                  names TURNS only, because a round is a model-layer detail
                  (measured: a 2-turn session held 336 rounds, and this verb used
                  to call the last of them ` + "`turn 336`" + `)
      --out PATH  write to PATH instead of this session's own
                  ` + ctxdump.FileNamePattern + ` (a TURN) or
                  ` + ctxdump.RoundFileNamePattern + ` (a ROUND) — each named for
                  the address the file HOLDS
      --system    export ONE SECTION of the request instead of the whole body:
                  ` + ctxdump.RequestHeading + ` + ` + ctxdump.SystemHeading + ` + ` + ctxdump.ToolsHeading + ` and
                  NO messages — the model's fixed prefix, the half that stays
                  identical across a session's turns. Written to its OWN name,
                  ` + ctxdump.SystemFileNamePattern + ` (or
                  ` + ctxdump.RoundSystemFileNamePattern + ` on the round axis), so it
                  never lands on the full page's file
      --user      the OTHER SECTION: ` + ctxdump.RequestHeading + ` + ` + ctxdump.MessagesHeading + ` and
                  NO system prompt and NO tool definitions. Written to
                  ` + ctxdump.UserFileNamePattern + ` (or
                  ` + ctxdump.RoundUserFileNamePattern + ` on the round axis)
                  ` + ctxdump.SystemSelector + ` WITH ` + ctxdump.UserSelector + ` IS REFUSED: an export
                  shows ONE side of a request, and the pair together would be the
                  full page under a filtered name

` + sessionContextAxisRules + `

` + sessionContextInFlightNotice + `

  A section export is a DERIVED VIEW, not a smaller version of the evidence:
  ` + ctxdump.FileNamePattern + ` still holds the request's own bytes and is still the
  file to diff against the raw HTTP log. The section file says in its own first
  paragraph that it is a view, which matters because --out replaces its name.

WHAT IS ON THE SCREEN: one line.

  ` + fmt.Sprintf(strings.TrimRight(ctxdump.LineFormat, "\n"), "<path>", 0, 0) + `

WHAT IS IN THE FILE: markdown, one section per message. Every block is written
VERBATIM — no length cut, no truncation label, no counter line — so the body can
be diffed against the raw log. Image blocks are summarized (type, size, sha256);
the base64 never appears, because a page carrying it would be the JSON again.
An export OVERWRITES its destination: exporting the same address twice leaves one
page, not two.

The export is read-only: it opens this session's raw log, writes one file under
the session directory, and never touches the session, the ledger or the ground.

The chat carrier has the same command as
` + "`/context [N] [--round N] [--system | --user]`" + ` — one renderer, two doors
(internal/ctxdump), and ONE parser (parseChatContextArgs), so a flag added here
reaches both doors at once.`

// runSessionContext implements `gopi session context <id> [flags]`.
//
// Exit codes follow the family's rule: 0 = an export was written (or -h was
// answered), 1 = a usage error, 2 = the export was refused and NOTHING was
// written (no raw log, a turn past the end, a session that does not resolve).
func runSessionContext(args []string) int {
	var (
		sessionID string
		turn      int
		round     int
		last      bool
		out       string
		turnSet   bool
		roundSet  bool
		systemSel bool
		userSel   bool
	)
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == HelpFlag || a == HelpFlagLong:
			fmt.Print(sessionContextUsage)
			return 0
		case a == sessionContextLastFlag:
			last = true
		case a == ctxdump.SystemSelector:
			systemSel = true
		case a == ctxdump.UserSelector:
			userSel = true
		case a == sessionContextRoundFlag:
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "%s needs a round number\n%s", sessionContextRoundFlag, sessionContextUsage)
				return 1
			}
			i++
			n, ok := parseSessionContextTurn(args[i])
			if !ok {
				fmt.Fprintf(os.Stderr, "%s needs a round number (1 or greater), got %q\n%s", sessionContextRoundFlag, args[i], sessionContextUsage)
				return 1
			}
			round, roundSet = n, true
		case strings.HasPrefix(a, sessionContextRoundFlag+sessionContextValueSep):
			raw := strings.TrimPrefix(a, sessionContextRoundFlag+sessionContextValueSep)
			n, ok := parseSessionContextTurn(raw)
			if !ok {
				fmt.Fprintf(os.Stderr, "%s needs a round number (1 or greater), got %q\n%s", sessionContextRoundFlag, raw, sessionContextUsage)
				return 1
			}
			round, roundSet = n, true
		case a == sessionContextTurnFlag:
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "%s needs a turn number\n%s", sessionContextTurnFlag, sessionContextUsage)
				return 1
			}
			i++
			n, ok := parseSessionContextTurn(args[i])
			if !ok {
				fmt.Fprintf(os.Stderr, "%s needs a turn number (1 or greater), got %q\n%s", sessionContextTurnFlag, args[i], sessionContextUsage)
				return 1
			}
			turn, turnSet = n, true
		case strings.HasPrefix(a, sessionContextTurnFlag+sessionContextValueSep):
			raw := strings.TrimPrefix(a, sessionContextTurnFlag+sessionContextValueSep)
			n, ok := parseSessionContextTurn(raw)
			if !ok {
				fmt.Fprintf(os.Stderr, "%s needs a turn number (1 or greater), got %q\n%s", sessionContextTurnFlag, raw, sessionContextUsage)
				return 1
			}
			turn, turnSet = n, true
		case a == sessionContextOutFlag:
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "%s needs a PATH\n%s", sessionContextOutFlag, sessionContextUsage)
				return 1
			}
			i++
			out = args[i]
		case strings.HasPrefix(a, sessionContextOutFlag+sessionContextValueSep):
			out = strings.TrimPrefix(a, sessionContextOutFlag+sessionContextValueSep)
			if out == "" {
				fmt.Fprintf(os.Stderr, "%s needs a PATH\n%s", sessionContextOutFlag, sessionContextUsage)
				return 1
			}
		case strings.HasPrefix(a, sessionContextFlagPrefix):
			fmt.Fprintf(os.Stderr, "gopi session %s: %q is not an option\n%s", sessionSubcmdContext, a, sessionContextUsage)
			return 1
		default:
			if sessionID != "" {
				fmt.Fprintf(os.Stderr, "gopi session %s: one session id, got %q and %q\n%s", sessionSubcmdContext, sessionID, a, sessionContextUsage)
				return 1
			}
			sessionID = a
		}
	}
	if sessionID == "" {
		fmt.Fprintf(os.Stderr, "gopi session %s: %s\n%s", sessionSubcmdContext, sessionContextNoSessionMessage, sessionContextUsage)
		return 1
	}
	// THREE selectors, TWO axes, and each refusal names the ACTUAL conflict:
	// `--last` and `--turn` are the SAME axis (both name a TURN), so pairing them
	// is a duplicate selector — not a turn/round clash. Saying "a TURN vs a ROUND"
	// there would teach the reader the wrong model in the one message they get.
	if last && turnSet {
		fmt.Fprintf(os.Stderr, "gopi session %s: %s and %s both select a TURN (%s means the newest one) — pick one\n%s",
			sessionSubcmdContext, sessionContextLastFlag, sessionContextTurnFlag, sessionContextLastFlag, sessionContextUsage)
		return 1
	}
	if last && roundSet {
		fmt.Fprintf(os.Stderr, "gopi session %s: %s selects a TURN and %s selects a ROUND — two different address spaces, so pick one\n%s",
			sessionSubcmdContext, sessionContextLastFlag, sessionContextRoundFlag, sessionContextUsage)
		return 1
	}
	if turnSet && roundSet {
		fmt.Fprintf(os.Stderr, "gopi session %s: %s and %s name two different address spaces — a turn routinely takes many rounds, so pick one\n%s",
			sessionSubcmdContext, sessionContextTurnFlag, sessionContextRoundFlag, sessionContextUsage)
		return 1
	}

	// THE SECTION SELECTORS, resolved through the package that owns them — the
	// SAME call the slash command's parser makes, so the refusal of
	// `--system --user` is one sentence in one place and the two carriers cannot
	// disagree about it.
	section, serr := ctxdump.SectionFor(systemSel, userSel)
	if serr != nil {
		fmt.Fprintf(os.Stderr, "gopi session %s: %v\n%s", sessionSubcmdContext, serr, sessionContextUsage)
		return 1
	}

	summary, err := sessionContextExport(sessionID, turn, round, out, section)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi session %s: %v\n", sessionSubcmdContext, err)
		return 2
	}
	fmt.Print(summary.Line())
	// The historical-collision note rides stderr, beside the ONE line: a reader
	// looking for the pre-axis `context-turn<N>.md` needs to know it is still
	// there under the old word, and the screen's own promise is one line.
	for _, note := range summary.Notes() {
		fmt.Fprintln(os.Stderr, note)
	}
	return 0
}

// sessionContextExport resolves the session, resolves the ADDRESS (a TURN, or a
// ROUND when round > 0), selects the request and writes the artifact. It is the
// CLI's WHOLE effect, in one call, so the chat carrier's arm can be compared
// against it line by line.
//
// turn <= 0 means the session's LAST TURN — the context the model is running
// with now. round > 0 asks for one HTTP request/response instead, and the two
// are different address spaces (ctxdump/address.go states why one word cannot
// serve both). section selects WHICH PART of that request is written; the zero
// value is the full page, i.e. the export this verb has always produced.
func sessionContextExport(sessionID string, turn, round int, out string, section ctxdump.Section) (ctxdump.Summary, error) {
	sessionPath, err := gopilog.FindSessionPathForID(sessionID)
	if err != nil {
		return ctxdump.Summary{}, err
	}
	rawLog, err := gopilog.ResolveRawLogForSession(sessionPath)
	if err != nil {
		return ctxdump.Summary{}, fmt.Errorf("cannot pair %s with a raw HTTP log: %w", sessionID, err)
	}
	var addr ctxdump.Address
	if round > 0 {
		addr = ctxdump.RoundAddress(round)
	} else {
		// THE TURN IS RESOLVED THROUGH THE ONE BOUNDARY ALGORITHM
		// (gopilog.SessionTurnBounds via SessionTurnSpans) — never by counting
		// `→ REQUEST` records. That count is the ROUND axis, a different
		// quantity, and confusing the two is the defect this verb used to have.
		addr, err = ctxdump.TurnAddressFor(sessionPath, turn)
		if err != nil {
			return ctxdump.Summary{}, err
		}
	}
	req, err := ctxdump.RequestAtTurn(rawLog, addr)
	if err != nil {
		return ctxdump.Summary{}, err
	}
	return ctxdump.WriteToSection(out, defaultContextDir(sessionPath), req, addr, section)
}

// defaultContextDir is the directory an unnamed export lands in: the SESSION's
// own directory, which is what the operator will look in.
//
// It is derived from the session FILE rather than re-composed from a base path
// and an id, so a session stored anywhere (a resumed run, a sandbox profile)
// writes beside its own log instead of into a directory derived from a guess.
func defaultContextDir(sessionPath string) string {
	return filepath.Dir(sessionPath)
}

// parseSessionContextTurn reads one turn operand: a bare decimal, 1 or greater.
//
// Every other shape is refused by the CALLER with the value quoted, so this
// returns (0, false) rather than an error — there is one refusal sentence and
// it lives at the parse site that also knows which flag was typed.
func parseSessionContextTurn(arg string) (int, bool) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return 0, false
	}
	for i := 0; i < len(arg); i++ {
		if arg[i] < '0' || arg[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(arg)
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}
