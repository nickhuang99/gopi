// address.go — WHICH address an export is written in.
//
// THE DEFECT THIS FILE REMOVES. The artifact used to be named
// `context-turn1354.md` for the session's 1354th HTTP `→ REQUEST` record, while
// every other surface counts a TURN through ONE algorithm
// (gopilog.SessionTurnBounds over sessionevent.IsTurnOpeningRow). One word, two
// axes: `gopi session show <id>` says the session ran 2 turns and the export
// beside it says `turn 336`.
//
// WHICH SURFACE IS THE WITNESS IS PART OF THE FIX, and it was measured wrong on
// the first cut of this file. The turn axis walked here is
// gopilog.SessionTurns, and the surface that prints THAT number is
// `gopi session show <id>`'s `turns` row — not `gopi session log <id> --turns`,
// which tabulates the PERSON-ONLY SUBSET (gopilog.SessionHumanTurns) and can be
// strictly fewer. Measured on session a60787b0-9081-451f-bd5a-9e396cfc048b:
// `session show` → `turns 16 / human turns 8`, this export's out-of-range
// refusal → `holds 16 turn(s)`, `session log --turns` → 8 rows. Pointing a
// reader at the subset would have re-created the very defect this epic removes
// — two numbers, one word, no statement of which is which
// (sessionevent.CounterUnitHumanTurn exists for exactly that reason).
//
// So an export now declares its AXIS:
//
//   - TURN  — the address the rest of the system means by "turn". The file's
//     name and its page header carry the turn ordinal AND the interval
//     of ROUNDS that turn covered ("turn 2, rounds 277–332"), because
//     that interval is the one fact that stops a reader from addressing
//     a turn by a round number.
//   - ROUND — one HTTP request/response, i.e. one `request` row. It says the
//     word ROUND in its name and in its header, so a request-level
//     export can never be read as a turn again.
//
// THE BOUNDARIES ARE NOT RE-DERIVED HERE (the epic's red line): they come from
// gopilog.SessionTurnSpans, which reads SessionTurnBounds, which reads the
// reader's TurnBoundary stamps, which sessionevent.IsTurnOpeningRow decides.
// This package owns the ADDRESS, never the boundary (Rule 14).
//
// A HISTORICAL FILE IS NOT MIGRATED. `context-turn1354.md` keeps sitting where
// it was written; the name scheme is not retrofitted, because rewriting an
// operator's evidence to match a new vocabulary is the kind of "helpful" edit
// nobody can later account for. The cost is stated plainly instead: a file
// written before this axis existed carries a round number under the word
// "turn".
package ctxdump

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/gopilog"
)

// Axis is the ADDRESS SPACE an export is written in. It is a named type with
// its two values as constants so a caller cannot pass a bare string that
// happens to spell one of them (Rules 1/13/14).
type Axis string

const (
	// AxisTurn addresses a TURN: one complete input to the next, with every
	// round the agent ran inside it.
	AxisTurn Axis = "turn"
	// AxisRound addresses one ROUND: a single `request` record, i.e. one HTTP
	// request/response exchange. It is NOT a turn (a turn routinely takes many).
	AxisRound Axis = "round"
)

// ErrNoTurnRecorded is returned when a session recorded no turn boundary at
// all, so a turn-scoped export has no address to print. Naming a file "turn 0"
// would be the same defect this file exists to remove, one layer down.
var ErrNoTurnRecorded = errors.New("this session recorded no turn boundary, so there is no turn to export (pass --round N to export one request instead)")

// TurnWitnessWords names the surface a reader can take this axis's number to.
//
// IT IS ONE CONSTANT BECAUSE IT WAS WRONG IN FOUR PLACES AT ONCE. The first cut
// of this epic said `gopi session log <id> --turns` everywhere a turn count was
// cited, and that command prints the PERSON-ONLY SUBSET
// (gopilog.SessionHumanTurns) — a number that can be strictly fewer than the
// axis this export walks (gopilog.SessionTurns). Measured on session
// a60787b0-9081-451f-bd5a-9e396cfc048b: `session show` → `turns 16`,
// `session log --turns` → 8 rows, and this export's own refusal → `holds 16
// turn(s)`. A reader sent to `--turns` to check "16" would be handed "8" and
// would conclude the export was broken — two numbers, one word, no statement of
// which is which, which is EXACTLY the defect this epic removes, moved out one
// layer. `gopi session show <id>` prints the number this axis counts, under the
// same word.
const TurnWitnessWords = "run `gopi session show <id>` and read its `turns` row (NOT `session log --turns`, which is the human-turn subset)"

// Address is one artifact's address: which axis it is written in, the turn
// when the axis is AxisTurn, and the round it rendered in either case.
//
// ROUND IS ALWAYS SET. Even a turn-scoped artifact renders ONE request body —
// the turn's last one — and that body's own round number is what makes the
// page auditable: a reader can take it to `gopi session log <id> N` and get the
// same exchange back.
type Address struct {
	Axis       Axis
	Turn       int
	FirstRound int
	LastRound  int
	Round      int
}

// TurnAddress builds a turn-scoped address. first/last are the rounds the turn
// covered (0 when it covered none) and round is the request this artifact
// renders.
func TurnAddress(turn, first, last, round int) Address {
	return Address{Axis: AxisTurn, Turn: turn, FirstRound: first, LastRound: last, Round: round}
}

// RoundAddress builds a round-scoped address.
func RoundAddress(round int) Address {
	return Address{Axis: AxisRound, Round: round}
}

// FileName is the artifact's name for this address, derived from the axis's own
// constant so the name and the page header cannot drift apart (Rule 14).
func (a Address) FileName() string {
	if a.Axis == AxisRound {
		return fmt.Sprintf(RoundFileNameFormat, a.Round)
	}
	return fmt.Sprintf(FileNameFormat, a.Turn)
}

// Path is a.FileName() under dir.
func (a Address) Path(dir string) string {
	return filepath.Join(dir, a.FileName())
}

// Validate reports whether the address describes bytes that can be written:
// every axis needs a positive ordinal, and the turn axis needs its turn.
func (a Address) Validate() error {
	switch a.Axis {
	case AxisTurn:
		if a.Turn < 1 {
			// The turn the export would name is unknown. That is a MEASUREMENT
			// that failed, not an absent request, and the caller must render it
			// as unknown rather than as turn 0 (ErrTurnUnmeasurable's reason).
			return fmt.Errorf("%w: turn %d", ErrTurnUnmeasurable, a.Turn)
		}
	case AxisRound:
	default:
		return fmt.Errorf("ctxdump: unknown address axis %q", string(a.Axis))
	}
	// ROUND IS REQUIRED ON BOTH AXES: a turn-scoped artifact still renders ONE
	// request body, and that body's round number is what makes the page
	// auditable against `gopi session log <id> N`.
	if a.Round < 1 {
		return fmt.Errorf("%w: round %d", ErrTurnUnmeasurable, a.Round)
	}
	return nil
}

// RoundAliasReport is what a caller prints when the address it just exported
// has a HISTORICAL file under the old naming scheme — and ONLY when that file
// is really there.
//
// THE EXISTENCE CHECK IS THE WHOLE POINT (operator ruling, 2026-11-10). This
// sentence used to be computed from the arithmetic alone, so EVERY ordinary
// turn-scoped export on a session where turn != round printed a line about a
// pre-axis artifact that no one had ever written — and that line spelled a ROUND
// number, in the one channel the ruling cleans. The note is a diagnostic about a
// file on disk, so it is emitted only when the file is on disk: os.Stat decides,
// and every other outcome (absent, unreadable, a directory) is silence, because a
// note that fires on a guess is worse than no note.
func RoundAliasReport(dir string, a Address) string {
	if a.Axis != AxisTurn {
		return ""
	}
	legacy := fmt.Sprintf(FileNameFormat, a.Round)
	if legacy == a.FileName() {
		return ""
	}
	legacyPath := filepath.Join(dir, legacy)
	fi, err := os.Stat(legacyPath)
	if err != nil || fi.IsDir() {
		// Nothing to report: no historical artifact is there. Rule 15's "state
		// absence" applies to a failure the reader must know about; a note about
		// a file that does not exist is not one — it is noise carrying a round
		// number.
		return ""
	}
	return fmt.Sprintf(LegacyRoundNameNoteFormat, legacyPath, a.Round, a.FileName())
}

// TurnAddressFor resolves the turn-scoped address of a session's export.
//
// turn <= 0 means "the session's LAST turn" — the context the model is running
// with now — which is what both carriers mean by a bare export.
//
// The walk is: parse the session's own record (the ONE boundary algorithm), ask
// the spans which round the turn's content ends at, and hand that round to the
// round resolver. Nothing here decides a boundary (the epic's red line); the
// only arithmetic is "which request does this turn's last content belong to",
// and that answer comes from the span's own LastRound.
func TurnAddressFor(sessionPath string, turn int) (Address, error) {
	if sessionPath == "" {
		return Address{}, fmt.Errorf("ctxdump: no session record path")
	}
	sess, err := gopilog.ParseFile(sessionPath)
	if err != nil {
		return Address{}, fmt.Errorf("ctxdump: cannot read the session record %s: %w", sessionPath, err)
	}
	views := gopilog.SessionTurnViews(sess)
	if len(views) == 0 {
		return Address{}, ErrNoTurnRecorded
	}
	if turn <= 0 {
		view := views[len(views)-1]
		round := view.LastRound
		if round <= 0 {
			// The last turn ran no request at all (a session whose final input
			// was never sent). Fall forward to the previous turn that did, and
			// say which one the artifact actually holds by using ITS numbers.
			for i := len(views) - 2; i >= 0; i-- {
				if views[i].LastRound > 0 {
					view = views[i]
					round = view.LastRound
					break
				}
			}
		}
		if round <= 0 {
			return Address{}, fmt.Errorf("%w: no turn in %s ran a request", ErrNoTurnRecorded, sessionPath)
		}
		return TurnAddress(view.Turn, view.FirstRound, view.LastRound, round), nil
	}
	if turn > len(views) {
		return Address{}, fmt.Errorf("turn %d is past the end of %s, which holds %d turn(s) — "+TurnWitnessWords,
			turn, sessionPath, len(views))
	}
	view := views[turn-1]
	round := view.LastRound
	if round <= 0 {
		return Address{}, fmt.Errorf("turn %d of %s ran no request, so there is no request body to export — "+TurnWitnessWords,
			turn, sessionPath)
	}
	return TurnAddress(view.Turn, view.FirstRound, view.LastRound, round), nil
}

// RequestAtTurn reads the request body an address renders.
//
// A turn-scoped address renders the LAST request the turn sent — the body the
// model was handed at the end of that turn's work — and a round-scoped address
// renders exactly the round it names. Both end in the same round resolver, so
// the export has one decoder, one refusal vocabulary and one cost model.
func RequestAtTurn(rawLogPath string, a Address) (*deepseek.ClaudeRequest, error) {
	if err := a.Validate(); err != nil {
		return nil, err
	}
	req, round, err := FromRawLog(rawLogPath, a.Round)
	if err != nil {
		return nil, err
	}
	if round != a.Round {
		// FromRawLog answers the round it selected; a disagreement means the
		// request is not where the address says it is, and writing it under that
		// address would be the mislabelled artifact this epic removes.
		return nil, fmt.Errorf("ctxdump: requested round %d but the log answered round %d — refusing to write a mislabelled artifact", a.Round, round)
	}
	return req, nil
}
