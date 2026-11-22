package main

// cli_web_turn.go — THE TURN BANNER: which turn of the SESSION a browser turn
// is, numbered by the session record itself.
//
// THE GAP THIS CLOSES. The page could tell "a turn is running" (the status
// badge) but never "which turn this is". A long web conversation scrolled back
// through was an undifferentiated column of bubbles: the reader could not say
// where turn 7 ended and turn 8 began, and — the part that makes this a defect
// rather than a nicety — no number the page could invent would have been the
// RIGHT one, because the only authority for a session's turn ordinal is its own
// record (`gopilog.SessionTurns`, the same axis `gopi session log --turns`
// prints and `session replay --turn N` selects).
//
// THE NUMBER IS NOT A PAGE COUNTER, AND NOT THIS SERVER'S EITHER. It is
//
//	turnBaseline + agent.SessionLogger.TurnsRecorded() + 1
//
// where turnBaseline is how many turn-opening rows the record ALREADY held when
// this run started (0 for a fresh session) and TurnsRecorded counts the rows THIS
// run has written that `sessionevent.IsTurnOpeningRow` accepts. Both halves use
// the ONE predicate, at the ONE writer, so the number on the wire is the number
// the record gives the same row — that equality is what the regression test
// asserts, and it is the whole deliverable.
//
// IT RIDES THE CHANNEL THAT ALREADY EXISTS. A POSTed turn's frames go out on
// that turn's own SSE response and a server-started turn's go out on
// `/api/events`; the `turn` frame is a frame on those two streams, not a third
// transport and not a new poll (Rule 35: nothing new schedules).

import (
	"fmt"
	"os"
	"time"

	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/gopilog"
	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// webTurnFrameField is the `turn` frame's one field: the session's ordinal for
// the turn that is starting (1-based). It is named, not typed inline, because
// the page carries its own copy of every wire name inside its Go raw string
// (Rule 1b) and the regression test pins the pair.
const webTurnFrameField = "turn"

// webTurnReport prints a turn-numbering fault. It exists so a number that could
// not be anchored is never silent (Rule 15): a banner that quietly restarts at
// 1 on a resumed session is exactly the defect this file removes.
const webTurnReport = "[gopi web] turn: "

// webTurnOrdinal answers "which turn of the session is the turn that is
// STARTING now?" — and whether there IS one.
//
// ok==false is a real answer, not a failure: a message the record charges no
// turn to (a harness carrier, a briefing frame) opens no turn, so a banner over
// it would be a number the record does not give it. The predicate is applied to
// the same (text, face) pair the writer will stamp, with the reader's own face
// default (`sessionevent.TurnRowSource`), so the two sides cannot disagree
// about whether this row is a turn.
//
// THE COUNT IS TAKEN AT THE WRITER (`SessionLogger.TurnsRecorded`), never by
// re-reading the file: at the instant a turn starts, the record's tail is still
// in the writer's buffer, so a parse would read the number one turn behind —
// the drift this file exists to end.
func (s *webServer) webTurnOrdinal(text, face string) (int, bool) {
	if !sessionevent.IsTurnOpeningRow(text, sessionevent.TurnRowSource(face)) {
		return 0, false
	}
	if s.agent == nil || s.agent.SessionLog == nil {
		// NO RECORD, NO ORDINAL: this server writes no session log, so no
		// authority exists for the number. Reported once, never substituted.
		fmt.Fprintf(os.Stderr, "%s no session record is open, so this turn has no session ordinal\n", webTurnReport)
		return 0, false
	}
	return s.turnBaseline + s.agent.SessionLog.TurnsRecorded() + 1, true
}

// webTurnPayload encodes the `turn` frame body: the ordinal, and WHEN that turn
// opened in the banner's shared spelling.
//
// THE MOMENT IS ON THE OPENING FRAME because that is the frame the band is
// mounted from — a page that learned the time only at `done` would show the
// banner without it for the whole turn and then rewrite it, which is worse than
// showing it once.
//
// at is the moment the caller minted and ALSO published to the writer, so the
// frame and the record state one instant. A zero at (no record behind this turn)
// encodes the shared placeholder rather than omitting the field: an absent
// moment and an unknown moment are the same fact to a reader, and a missing key
// would leave the page unable to tell "old server" from "no timestamp".
func webTurnPayload(n int, at time.Time) []byte {
	return []byte(fmt.Sprintf(`{"%s":%d,"%s":%q}`, webTurnFrameField, n,
		webSessionTurnOpenedField, sessionevent.FormatTurnStamp(at)))
}

// webSessionTurnPrompts reads the turn-OPENING prompts a session record already
// holds, in order — the material the transcript replay is numbered against.
//
// WHY THE REPLAY READS THE RECORD. A refreshed page rebuilds its log from
// `/api/transcript`, i.e. from the server's in-memory history, and that history
// carries no turn numbers of its own: the faces and the boundaries live in the
// JSONL. Numbering the replay from the record is what makes "history turns
// still carry their numbers after a resume" a measurement rather than a
// counting of bubbles on the page.
//
// An unreadable record returns the error and NO prompts: the caller then
// numbers the replay by position and SAYS so, which is the honest degradation
// (Rule 15) — the alternative is a silent 1-based renumbering of a conversation
// whose real turns are 20..30.
func webSessionTurnPrompts(path string) ([]string, error) {
	turns, err := webSessionTurns(path)
	out := make([]string, 0, len(turns))
	for _, t := range turns {
		out = append(out, t.prompt)
	}
	return out, err
}

// sessionTurnPrompt is one turn of the record, as the replay needs it: WHAT was
// asked, and WHEN the row that asked it was written.
//
// THE PAIR TRAVELS TOGETHER because a banner states both, and a surface that
// fetched the prompts here and the moments somewhere else would be one lookup
// away from mismatching them.
type sessionTurnPrompt struct {
	prompt   string
	openedAt time.Time
}

// webSessionTurns reads the turns a session record already holds, in order —
// the material the transcript replay is numbered AND stamped from.
//
// The boundaries and the moments BOTH come from their one materialisation
// (`SessionTurnBounds` over `Iteration.TurnOpenedAt`), never from a second walk
// over `TurnBoundary` here: that walk is how a surface ends up disagreeing with
// the reader about where a turn starts — including the reader's own "content but
// no mark gets ONE turn" case, which a bare loop would silently report as zero
// turns.
//
// An unreadable record returns the error and NO turns: the caller then numbers
// the replay by position and SAYS so, which is the honest degradation (Rule 15)
// — the alternative is a silent 1-based renumbering of a conversation whose real
// turns are 20..30.
func webSessionTurns(path string) ([]sessionTurnPrompt, error) {
	if path == "" {
		return nil, fmt.Errorf("no session record path is known to this server")
	}
	sess, err := gopilog.ParseFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read the session record %s: %w", path, err)
	}
	// THE BOUNDARIES COME FROM THEIR ONE MATERIALISATION (`SessionTurnBounds`),
	// never from a second walk over `TurnBoundary` here: that walk is how a
	// surface ends up disagreeing with the reader about where a turn starts —
	// including the reader's own "content but no mark gets ONE turn" case, which
	// a bare loop would silently report as zero turns.
	bounds := gopilog.SessionTurnBounds(sess)
	out := make([]sessionTurnPrompt, 0, len(bounds))
	for _, i := range bounds {
		out = append(out, sessionTurnPrompt{
			prompt:   sess.Iterations[i].UserPrompt,
			openedAt: sess.Iterations[i].TurnOpenedAt,
		})
	}
	return out, nil
}

// webTurnBaseline answers "how many turns did the session record already hold
// when this server started?" — the fixed half of the banner's number — for the
// caller that holds the record's PATH.
//
// IT IS THE ONE DERIVATION, NOT A FOURTH COPY OF IT (Rule 14). The counting
// itself is `chatTurnOrdinalAt` (turn_ordinal_baseline.go), which is the same
// function `gopi ui`'s bar anchors on: one meaning per word, and the surface
// that asks with a path and the surface that asks with a session id cannot get
// different answers to the same question. This wrapper adds only what the web
// side knows and that function does not — that an unreadable record here
// degrades to 0 WITH A REPORT rather than to an error the HTTP path has no room
// for (the caller would still have to number its turn).
//
// IT IS MEASURED FROM THE FILE ONCE, AT STARTUP, and that is the only instant it
// can be measured: one turn later the file holds this run's own rows too, and
// "how many of these 21 turns were here before me?" is no longer a question the
// file answers. A fresh conversation measures 0 (its record holds a
// session_start and nothing else), so the fresh path's number is unchanged by
// the resume path existing.
//
// AN UNREADABLE OR ABSENT RECORD IS 0 AND IS REPORTED — never a guess and never
// a silent restart at 1: on the resumed path that would number new turns 1..N
// while the record calls them 21..N+20, which is the drift the banner removes.
func webTurnBaseline(path string) int {
	if path == "" {
		return 0
	}
	n, err := chatTurnOrdinalAt(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s cannot count the turns already in the record (%v) — this run's turns are numbered from 1\n", webTurnReport, err)
		return 0
	}
	return n
}

// webTranscriptTurnOrdinals assigns a session turn ordinal to each turn-opening
// USER message of the server-owned transcript, in order — the replay half of
// the banner.
//
// THE ORDINAL IS THE RECORD'S POSITION, and the match is by CONTENT so a
// mismatch is detectable: the k-th turn-opening user row of the transcript is
// the k-th turn of the record, and if its text does not equal the record's
// prompt for that position this function reports it once instead of printing a
// number it cannot stand behind (a dumped checkpoint can carry a truncated
// history, and then "position in the history" and "position in the record"
// genuinely differ).
//
// The count is capped at the record's own length: history longer than the
// record is the mismatch above, and history shorter (a truncated checkpoint) is
// numbered from 1 by position, which is right for the rows that ARE there.
func (s *webServer) webTranscriptTurnOrdinals(history []deepseek.Message) (map[int]int, error) {
	ords, _, err := s.webTranscriptTurnMarks(history)
	return ords, err
}

// webTranscriptTurnMarks is the ONE derivation behind both replay facts: each
// turn-opening user row's ORDINAL and the MOMENT that turn opened, keyed by the
// row's index in the history.
//
// THEY COME FROM THE SAME PASS on purpose. A banner states "turn 7 · <moment>",
// and two lookups (one for the number, one for the time) that could disagree
// about which record row they mean would let a page print turn 7 beside turn 8's
// clock — a defect that looks like a rendering bug and is really two readers.
//
// The moments are the RECORD's (`Iteration.TurnOpenedAt`, i.e. the timestamp of
// the row itself), so a replay of a session from last week prints last week's
// instants. A row with no record behind it has no moment and is absent from the
// map — the page then prints the shared placeholder.
func (s *webServer) webTranscriptTurnMarks(history []deepseek.Message) (map[int]int, map[int]string, error) {
	var recordPath string
	if s.agent != nil && s.agent.SessionLog != nil {
		// FLUSH BEFORE READING. The writer buffers a turn's rows and flushes at
		// the round boundary, so a replay that arrives mid-buffer would read a
		// record one turn behind and number the history against a conversation
		// it has not finished writing. The sync is the writer's own (one lock,
		// no second reader), so this costs one fsync and not a poll.
		if err := s.agent.SessionLog.Sync(); err != nil {
			fmt.Fprintf(os.Stderr, "%s the session record could not be flushed before the replay was numbered (%v)\n", webTurnReport, err)
		}
		recordPath = s.agent.SessionLog.Path()
	}
	turns, err := webSessionTurns(recordPath)
	out := make(map[int]int)
	opened := make(map[int]string)
	k := 0
	for i := range history {
		if history[i].Role != "user" {
			continue
		}
		if !sessionevent.IsTurnOpeningRow(history[i].Content, sessionevent.TurnRowSource("")) {
			continue
		}
		k++
		out[i] = k
		if err != nil || k > len(turns) {
			continue
		}
		if turns[k-1].prompt != history[i].Content {
			fmt.Fprintf(os.Stderr, "%s the replayed turn %d says %q, but the record's turn %d opened with %q — the numbers come from the RECORD\n",
				webTurnReport, k, webTranscriptPromptDigest(history[i].Content), k, webTranscriptPromptDigest(turns[k-1].prompt))
		}
		// THE MOMENT IS ONLY ATTACHED WHERE THE CONTENT AGREES, because a
		// moment beside the wrong prompt is worse than no moment: it would date
		// this bubble with another turn's row. A mismatch above is already
		// reported; leaving the time off here is the same honest degradation,
		// one field further.
		opened[i] = sessionevent.FormatTurnStamp(turns[k-1].openedAt)
	}
	return out, opened, err
}

// webTranscriptPromptDigest shortens one prompt to a single readable line for a
// mismatch report. It is display only — the record keeps the full text.
func webTranscriptPromptDigest(text string) string {
	const digestMax = 40
	runes := []rune(text)
	if len(runes) <= digestMax {
		return text
	}
	return string(runes[:digestMax]) + "…"
}
