package main

// turn_ordinal_baseline.go — THE ONE ANSWER to "how many turn-opening rows does
// this session's log already hold?", which is the anchor every turn-numbering
// surface must start from.
//
// WHY IT IS THE LOG AND NOT THE LEDGER (the defect this file closes, measured
// 2026-11-04 on session e1e22e9f-eee0-4c33-a238-0fca04534ad9, all four read at
// the same instant):
//
//	gopi session show <id>         turns 619   ← chats.turn_count (the LEDGER)
//	gopi session log <id> --turns  rows to 621 ← SessionTurnBounds (the LOG)
//	gopi session replay --turns    turns=621   ← the same LOG producer
//	the chat screen's own heading   620        ← LEDGER baseline + this run
//
// The screen's number is what the operator copies into `replay --turn N`, so a
// baseline one behind the ordinals makes that lookup land on a DIFFERENT turn —
// the epic's own reproduction. And the gap was not noise: 16 of 16 readable
// banners sat exactly ONE below the row carrying the same prompt.
//
// THE TWO NUMBERS ARE DIFFERENT QUANTITIES, not two readings of one. The ledger
// counts turns that FINISHED (`RecordChatTurnOpened` runs only on
// `runErr == nil && !interrupted`, so an interrupted or failed turn registers
// nothing); a turn-opening row is a POSITION in the log, and a position cannot
// have a hole. Anchoring an ORDINAL on a COMPLETED-COUNT is a category error
// (Rule 14: one meaning per word), so the anchor is the row count.
//
// THE OLDER RULING THIS SUPERSEDES: on 2026-10-27 the ledger was chosen BECAUSE
// the log counter was then the person-only SUBSET (session a60787b0-9081-451f-
// bd5a-9e396cfc048b: log 8 operator rows, ledger 16 turns). That split is now
// explicit (SessionTurns is the total, SessionHumanTurns the subset — see
// internal/gopilog/sessionround.go), so the reason to reach for the ledger is
// gone and only its one-behind drift was left. `session show`'s `turns` line
// keeps printing the ledger's number: it is the COMPLETED-turn count and its own
// provenance sentence says so.

import (
	"fmt"

	"github.com/nickhuang99/gopi/internal/gopilog"
)

// chatTurnOrdinalAt is the derivation itself, over a session JSONL path: the
// number of turn-opening rows the reader counts — the SAME producer that numbers
// the rows of `session log --turns` and selects `session replay --turn N`.
//
// A record that EXISTS and cannot be READ is an error, never a quiet zero: zero
// would put the operator back into the defect this file removes, with nothing
// saying so (Rule 15).
func chatTurnOrdinalAt(path string) (int, error) {
	s, err := gopilog.ParseFile(path)
	if err != nil {
		return 0, fmt.Errorf("cannot count the turns this session already holds from its own log %s: %w", path, err)
	}
	return gopilog.SessionTurns(s), nil
}

// chatTurnOrdinalBaseline is chatTurnOrdinalAt for a caller that holds only a
// session id (`gopi ui`'s engine reports one; nothing else is in hand there).
//
// `ok == false` covers exactly two absences, each announced by the caller rather
// than substituted: the sessions root could not be resolved, or no record exists
// for that id (nothing to continue, so the counter legitimately starts at 0).
func chatTurnOrdinalBaseline(sessionID string) (int, bool, error) {
	path, err := gopilog.FindSessionPathForID(sessionID)
	if err != nil {
		return 0, false, nil
	}
	n, err := chatTurnOrdinalAt(path)
	if err != nil {
		return 0, false, err
	}
	return n, true, nil
}
