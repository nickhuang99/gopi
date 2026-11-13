// cli_session_turnround.go — the REVERSE LOOKUP: which turn holds round R, and
// which rounds turn N holds.
//
// WHY IT IS A FLAG ON `session show` AND NOT A NEW VERB (Rule 3). The question
// "which turn is this round?" is asked about ONE session, by id, and it is the
// same session `session show` already renders — so it is an ADDRESS on that
// verb, not a second entry point over the same operand. A new top-level or
// group verb would also owe the family a new usage line, a new flag table and a
// new completion entry for a question the existing verb can answer.
//
// THE NUMBERS ARE NOT COMPUTED HERE. Both directions come from
// gopilog.BuildTurnRoundIndex, which is an index over SessionTurnSpans — the ONE
// boundary algorithm `session show`'s `turns` row, the export's TURN address and
// the replay's bands already read (Rule 14). This file parses, calls and prints;
// it decides nothing about boundaries. That is the epic's red line, and it is
// why the answer this verb prints CANNOT disagree with the `turns` row above it.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/nick/gopi/internal/gopilog"
	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/tools"
)

const (
	// sessionShowTurnFlag / sessionShowRoundFlag are the FAMILY's address words,
	// taken from the members that already spell them (Rule 14: one spelling per
	// word), so `--turn` means the same thing on show, replay and context.
	sessionShowTurnFlag  = sessionReplayTurnFlag
	sessionShowRoundFlag = sessionReplayRoundFlag
	// sessionShowMapFlag asks for the WHOLE mapping rather than one lookup: one
	// line per turn, so a reader can see the two axes side by side and stop
	// addressing a turn by a round number.
	sessionShowMapFlag = "--mapping"
)

// sessionTurnRoundColumns is the mapping table's header and row shapes. They are
// constants for the reason every other table's are (Rule 1/13): the unit words
// come from sessionevent, and the widths cannot drift between the header and
// the rows.
const (
	sessionTurnRoundHeaderFormat = "%7s %16s\n"
	sessionTurnRoundRowFormat    = "%7d %16s\n"
)

// sessionTurnRoundLookup is what the parse phase hands the render: which
// question was asked. The zero value means "no lookup" — the ordinary page.
type sessionTurnRoundLookup struct {
	turn int
	// round is the ROUND whose owning turn was asked for; 0 = not asked.
	round int
	// mapping asks for every turn's interval.
	mapping bool
}

// asked reports whether any lookup flag was given.
func (l sessionTurnRoundLookup) asked() bool { return l.turn > 0 || l.round > 0 || l.mapping }

// sessionShowFlagValue reads the value of a `--flag VALUE` or `--flag=VALUE`
// spelling, and reports whether the flag was there at all.
//
// The two spellings are READ here rather than through the family's shared
// scanner because `session show` parses its own argv (it is the one session verb
// with no flag table, cli_session.go's runSessionShow), and this file must not
// introduce a second parser for the whole verb just to read two numbers. It
// consumes the value token by advancing the index, which is the caller's job
// because only the caller owns the loop.
func sessionShowFlagValue(args []string, i int, word string) (value string, matched bool, next int) {
	a := args[i]
	if a == word {
		if i+1 >= len(args) {
			return "", true, i
		}
		return args[i+1], true, i + 1
	}
	if strings.HasPrefix(a, word+sessionContextValueSep) {
		return strings.TrimPrefix(a, word+sessionContextValueSep), true, i
	}
	return "", false, i
}

// parseSessionTurnRoundLookup scans argv for the three lookup flags, returning
// the lookup and the remaining arguments (the session id, --json) untouched.
//
// A flag with no value, a non-numeric value and two competing criteria are all
// REFUSED by name here, before any session is resolved: the refusal is about the
// command line, so it must not depend on whether the session exists.
func parseSessionTurnRoundLookup(args []string) (sessionTurnRoundLookup, []string, error) {
	var lk sessionTurnRoundLookup
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if v, ok, next := sessionShowFlagValue(args, i, sessionShowTurnFlag); ok {
			n, perr := parseSessionTurnRoundOrdinal(sessionShowTurnFlag, v)
			if perr != nil {
				return lk, nil, perr
			}
			lk.turn, i = n, next
			continue
		}
		if v, ok, next := sessionShowFlagValue(args, i, sessionShowRoundFlag); ok {
			n, perr := parseSessionTurnRoundOrdinal(sessionShowRoundFlag, v)
			if perr != nil {
				return lk, nil, perr
			}
			lk.round, i = n, next
			continue
		}
		if args[i] == sessionShowMapFlag {
			lk.mapping = true
			continue
		}
		rest = append(rest, args[i])
	}
	// ONE criterion per call: two of them name two different questions, and
	// answering one while the caller asked two is how a reader ends up with the
	// other number (Rule 15: refuse by name).
	asked := 0
	for _, on := range []bool{lk.turn > 0, lk.round > 0, lk.mapping} {
		if on {
			asked++
		}
	}
	if asked > 1 {
		return lk, nil, fmt.Errorf("%s, %s and %s ask three different questions — pass one",
			sessionShowTurnFlag, sessionShowRoundFlag, sessionShowMapFlag)
	}
	return lk, rest, nil
}

// parseSessionTurnRoundOrdinal reads one ordinal: a bare decimal, 1 or greater.
// It reuses the family's digit rule (parseSessionContextTurn) so the two verbs
// cannot disagree about what a number is — a shared parser, not a second one.
func parseSessionTurnRoundOrdinal(flag, arg string) (int, error) {
	n, ok := parseSessionContextTurn(arg)
	if !ok {
		return 0, fmt.Errorf("%s needs a number (1 or greater), got %q", flag, arg)
	}
	return n, nil
}

// renderSessionTurnRound prints the answer to the lookup, one line per question
// (and one line per turn for --mapping).
//
// The whole thing goes to STDOUT as prose, never as JSON: the JSON face of this
// verb is a documented row contract (man gopi-session) and widening it for a
// debug lookup would be a new contract for a question no script has asked.
func renderSessionTurnRound(ix gopilog.TurnRoundIndex, lk sessionTurnRoundLookup) (string, error) {
	if ix.Total() == 0 {
		// NO TURNS AT ALL is not "turn 1 covers nothing": the log recorded no
		// operator row, so the session has no turn axis to join against. The
		// round side still has an answer to give (sessionevent.CounterRoundNoTurn
		// spells it), so the refusal is the same on both.
		return "", fmt.Errorf("%w — %s", gopilog.ErrRoundOutsideEveryTurn, sessionevent.CounterRoundNoTurn)
	}
	if lk.mapping {
		var b strings.Builder
		fmt.Fprintf(&b, sessionTurnRoundHeaderFormat,
			sessionevent.CounterUnitTurnOne, sessionevent.CounterUnitRound)
		for _, v := range ix.Views() {
			fmt.Fprintf(&b, sessionTurnRoundRowFormat, v.Turn,
				gopilog.FormatTurnRoundRange(v.FirstRound, v.LastRound))
		}
		fmt.Fprintf(&b, sessionevent.CounterJoinTotalFmt+"\n", ix.Total(), sessionevent.CounterUnitTurn)
		return b.String(), nil
	}
	line, err := gopilog.FormatTurnRoundJoin(ix, lk.turn, lk.round)
	if err != nil {
		return "", err
	}
	return line + "\n", nil
}

// runSessionTurnRound resolves the session and answers the lookup. It is called
// from runSessionShow AFTER the session id is known, so the operand resolution
// (a chat id, a run id, a task id) is the show verb's own — one resolver, not
// two.
func runSessionTurnRound(show tools.SessionShow, lk sessionTurnRoundLookup) int {
	path := strings.TrimSpace(show.LogPath)
	if path == "" {
		fmt.Fprintf(os.Stderr, "gopi session %s: this session records no log path, so there is no turn/round axis to join\n", sessionSubcmdShow)
		return 1
	}
	sess, err := gopilog.ParseFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi session %s: cannot read %s: %v\n", sessionSubcmdShow, path, err)
		return 1
	}
	out, err := renderSessionTurnRound(gopilog.BuildTurnRoundIndex(sess), lk)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi session %s: %v\n", sessionSubcmdShow, err)
		return 1
	}
	fmt.Print(out)
	return 0
}
