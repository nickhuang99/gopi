// turnround.go — the JOIN between the two address spaces, in both directions.
//
// WHY THIS FILE EXISTS. A TURN and a ROUND are two counters over one session
// (sessionevent.CounterUnitTurn vs CounterUnitRound), and every surface that
// prints one of them must be able to answer the reader's next question: "which
// turn is this round?", "which rounds does this turn contain?". Before this
// file there was exactly ONE such join reachable from a program
// (internal/replayplay.RoundTurnOf, which needs a MATERIAL — a rendered
// playback), and none reachable from the CLI without building one: an operator
// asking either question had to read the `--rounds` table and count rows by
// eye, which is the class of mistake the axis split exists to stop.
//
// THE BOUNDARIES ARE NOT RE-DERIVED HERE. Every number in this file comes from
// SessionTurnSpans, which reads SessionTurnBounds, which reads the reader's
// TurnBoundary stamps, which sessionevent.IsTurnOpeningRow decides. This type
// is an INDEX over that one derivation, never a second walk: an index built
// independently would be the "two answers to one question" Rule 14 forbids,
// and it would drift the first time the boundary predicate changed (Rule 14's
// measured cost in this very package: the turn count and the turn table
// disagreeing about one session).
package gopilog

import (
	"errors"
	"fmt"

	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// The three ways a reverse lookup can have no answer. They are THREE errors and
// not one, because each names a different fact the caller must state:
//
//   - ErrRoundOutsideEveryTurn: the session recorded no operator row, so no
//     turn owns any round (sessionevent.CounterRoundNoTurn is that sentence for
//     a round-scoped VIEW; this is the same fact on the lookup path).
//   - ErrTurnOutsideSession: the turn ordinal is past the end of this session.
//   - ErrNoJoinCriterion: neither operand was given, which is a caller error,
//     not a property of the data — it can only be reached by a bug.
var (
	// ErrRoundOutsideEveryTurn is returned when no turn span covers the round.
	ErrRoundOutsideEveryTurn = errors.New("no turn spans this round (the log recorded no operator row to open one)")
	// ErrRoundOutsideSession is returned when the round ordinal is past the end
	// of this session's own round numbering.
	ErrRoundOutsideSession = errors.New("this session holds no such round")
	// ErrTurnOutsideSession is returned when the turn ordinal names no turn.
	ErrTurnOutsideSession = errors.New("this session holds no such turn")
	// ErrNoJoinCriterion is returned when a lookup names neither a turn nor a
	// round.
	ErrNoJoinCriterion = errors.New("ask for a turn or a round")
)

// TurnRoundIndex is one session's turns expressed in the ROUND address space,
// indexed so BOTH directions are a lookup rather than a scan.
//
// It is built once per session (BuildTurnRoundIndex) and answers from its own
// immutable slices, so a caller that needs several answers — a table, a
// refusal, a test — pays for the derivation once.
type TurnRoundIndex struct {
	// views is the ONE derivation, in turn order, 1-based by position: the
	// ordinal of views[i] is i+1 (SessionTurnView.Turn states the same number).
	views []SessionTurnView
	// totalRounds is how many canonical rounds the session recorded
	// (SessionRounds), so a lookup can tell "this round does not exist" apart
	// from "this round exists and no turn owns it" — two different facts, and
	// conflating them would tell a reader their typo was a session property.
	totalRounds int
	// byRound maps a canonical round ordinal to its 1-based turn. A round no
	// span covers (a session whose log recorded no operator row, or a round
	// that landed outside every span) is ABSENT from the map rather than
	// present with a zero: absence and "turn 0" are different answers, and the
	// zero would be a numbering the session does not have (Rule 15).
	byRound map[int]int
}

// BuildTurnRoundIndex indexes s's turns from the ONE boundary algorithm.
//
// A nil or empty session yields an EMPTY index, never nil: every method then
// answers "not measured" through its ok result instead of dereferencing. That
// is why this returns a value rather than a pointer — an absent session is a
// fact about the data, not a programmer error to panic on (Rule 19).
func BuildTurnRoundIndex(s *Session) TurnRoundIndex {
	if s == nil {
		return TurnRoundIndex{}
	}
	views := SessionTurnViews(s)
	ix := TurnRoundIndex{views: views, totalRounds: SessionRounds(s), byRound: make(map[int]int, len(views))}
	for _, v := range views {
		// A view with no round interval covers no request (FirstRound and
		// LastRound are both 0, per SessionTurnSpan). It still OCCUPIES its
		// turn ordinal — the turn exists, it just ran nothing — so it is kept
		// in views and contributes no round entries.
		if v.FirstRound <= 0 || v.LastRound <= 0 {
			continue
		}
		for r := v.FirstRound; r <= v.LastRound; r++ {
			// FIRST WRITER WINS, and the answer is the same round's turn on
			// every call. Overlapping spans cannot occur under the boundary
			// predicate (each turn owns [start, nextBound)), but if a future
			// derivation ever produced them, a stable first-wins rule keeps
			// this lookup total and reproducible rather than map-order random.
			if _, taken := ix.byRound[r]; !taken {
				ix.byRound[r] = v.Turn
			}
		}
	}
	return ix
}

// TotalRounds is how many canonical rounds the session recorded — the
// denominator a round lookup is checked against.
func (ix TurnRoundIndex) TotalRounds() int { return ix.totalRounds }

// Total is how many turns the session recorded — the number
// `gopi session show <id>` prints on its `turns` row (both read
// SessionTurnBounds through this package's ONE derivation).
func (ix TurnRoundIndex) Total() int { return len(ix.views) }

// RoundsOfTurn answers "turn N covers which rounds?": the interval in the ROUND
// address space (`--round N`, `gopi session log <id> N`).
//
// ok is false when the index holds no such turn (out of range) — a refusal,
// never a clamp. covered is false when the turn exists but ran no request
// (FirstRound and LastRound are 0): the turn is real and its interval is
// empty, which is a different answer from "no such turn" (Rule 15).
func (ix TurnRoundIndex) RoundsOfTurn(turn int) (first, last int, covered, ok bool) {
	if turn < 1 || turn > len(ix.views) {
		return 0, 0, false, false
	}
	v := ix.views[turn-1]
	if v.FirstRound <= 0 || v.LastRound <= 0 {
		return 0, 0, false, true
	}
	return v.FirstRound, v.LastRound, true, true
}

// TurnOfRound answers "which turn does round R belong to?".
//
// ok is false when no turn span covers that round — a session that recorded no
// operator row (SessionTurnBounds answers nil, so every round is unowned), or a
// round ordinal outside the session's own numbering. The caller STATES that
// absence (sessionevent.CounterRoundNoTurn) rather than guessing a turn.
func (ix TurnRoundIndex) TurnOfRound(round int) (turn int, ok bool) {
	turn, ok = ix.byRound[round]
	return turn, ok
}

// View returns one turn's round-space address, ok=false when out of range.
func (ix TurnRoundIndex) View(turn int) (SessionTurnView, bool) {
	if turn < 1 || turn > len(ix.views) {
		return SessionTurnView{}, false
	}
	return ix.views[turn-1], true
}

// Views is every turn's address in order, for a caller that renders the whole
// mapping (the CLI's lookup line, a test's table).
//
// The slice is this index's own state and is handed out WITHOUT a copy, so it
// is a READ-ONLY view: SessionTurnViews allocates freshly per build, so the
// index already owns these bytes and a second copy would only cost the walk it
// is meant to save.
func (ix TurnRoundIndex) Views() []SessionTurnView { return ix.views }

// FormatTurnRoundJoin renders the ONE line that answers a reverse lookup.
//
// Its two shapes are the two questions, and each names BOTH spaces so neither
// number can be read as the other:
//
//	round 5 belongs to turn 1 — turn 1 covers rounds 1–12
//	turn 1 covers rounds 1–12 (of 36 turns)
//
// The ROUND word is spelled ROUND, never "turn": this is the debug/address
// face, the one place a round number is the subject, and the epic's rule is
// that a round can only appear where it says it is a round
// (sessionevent.CounterRoundIsOneSpotFmt already spells that sentence for a
// round-scoped VIEW; this file's lines are the same rule for a LOOKUP).
func FormatTurnRoundJoin(index TurnRoundIndex, turn, round int) (string, error) {
	switch {
	case round > 0:
		if round > index.TotalRounds() {
			// THE ROUND DOES NOT EXIST: out of range in the session's own
			// numbering, which is a different answer from "no turn owns it".
			return "", fmt.Errorf("%w: round %d (this session recorded %d)", ErrRoundOutsideSession, round, index.TotalRounds())
		}
		owner, ok := index.TurnOfRound(round)
		if !ok {
			return "", fmt.Errorf("%w: round %d", ErrRoundOutsideEveryTurn, round)
		}
		first, last, covered, ok := index.RoundsOfTurn(owner)
		if !ok || !covered {
			// Unreachable through TurnOfRound (a round only enters the index
			// through a covered span), but a refusal that names its cause beats
			// a sentence printed with two zeroes if that ever changes.
			return "", fmt.Errorf("%w: round %d", ErrRoundOutsideEveryTurn, round)
		}
		return fmt.Sprintf(sessionevent.CounterRoundOwnerFmt,
			round, sessionevent.CounterUnitTurnOne, owner,
			sessionevent.CounterUnitTurnOne, owner,
			FormatTurnRoundRange(first, last)), nil
	case turn > 0:
		first, last, covered, ok := index.RoundsOfTurn(turn)
		if !ok {
			return "", fmt.Errorf("%w: turn %d", ErrTurnOutsideSession, turn)
		}
		if !covered {
			return fmt.Sprintf(sessionevent.CounterTurnNoRoundsFmt,
				sessionevent.CounterUnitTurnOne, turn, index.Total()), nil
		}
		return fmt.Sprintf(sessionevent.CounterTurnOwnsFmt,
			sessionevent.CounterUnitTurnOne, turn, FormatTurnRoundRange(first, last),
			index.Total(), sessionevent.CounterUnitTurn), nil
	}
	return "", ErrNoJoinCriterion
}
