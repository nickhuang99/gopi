package agent

// selfrecord_noteindex.go — the SAME call that appends the side-channel file
// also indexes the note (epic: round-level notes become searchable).
//
// THE ONE WRITE POINT. recordSelfNotes is where a round's [IMPORTANT] block
// becomes durable (selfrecord.go). That function now does BOTH halves of the
// recording — the append to `self-record.md` and the INSERT into the
// chat_notes table (internal/tools/chat_notes.go) — because two writers on two
// paths would eventually disagree about what was recorded, and the file is the
// EVIDENCE while the table is the RETRIEVAL layer over the same facts.
//
// SEMANTICS ARE UNCHANGED (the epic's constraint, stated as an invariant): the
// file keeps exactly the notes and the order it always had, and a failed INSERT
// never removes, rewrites or delays a line of it. The index is best-effort: an
// error is returned so the caller can LOG it (Rule 15b — never a silent drop),
// and the round continues.
//
// WHERE THE PROVENANCE COMES FROM — nothing is asked of the model:
//
//	session_id  the run's own session log id (storeSessionID — the same
//	            resolution every memory-row stamp uses; a run with no session
//	            log headers its notes with no session rather than a made-up one)
//	round_index the ordinal of this session's `request` events
//	            (SessionLogger.RequestsSoFar + 1 — the numbering
//	            sessionevent.RoundNumberOf defines from the loop's 0-based
//	            iteration, computed from the session log itself rather than from
//	            a per-path round cursor; a run that never logged a request
//	            stores the explicit absence, RoundIndexUnknown)
//	source_host tools.OriginHostName() — the node-identity override then the
//	            hostname, the SAME single definition the memory provenance
//	            column uses (Rule 14), so a note and a distilled row from one
//	            run can never disagree about which machine produced them
//	source_path the note file's FULL local path, which is what tells two
//	            machines whose home directories are the same string apart
//
// Rule 35: no file, no timer, no second store — an INSERT through the opener
// that already exists, on a path the loop already walks.

import (
	"path/filepath"
	"strings"

	"github.com/nickhuang99/gopi/internal/sessionevent"
	"github.com/nickhuang99/gopi/internal/tools"
)

// indexSelfNotes writes the turn's notes into the retrieval index. It returns
// the number of rows written plus the FIRST error, so the caller reports one
// line and the round is never lost.
//
// turn is GIVEN, not resolved here: the same value stamped the file lines
// (`t<turn> <note>`), evaluated once by recordSelfNotes (Rule 14 — one
// evaluation, two writers, so the evidence line and the searchable row can never
// disagree about which turn a note belongs to).
//
// dir is the run's session directory (the same one the record file lives in,
// already resolved by the caller's append); the stored path is the record file
// inside it — the exact file a reader would open on that machine.
func (a *Agent) indexSelfNotes(turn int, notes []string, dir string) (int, error) {
	if len(notes) == 0 {
		return 0, nil
	}
	session := a.storeSessionID()
	path := ""
	if strings.TrimSpace(dir) != "" {
		path = recordPathIn(dir)
	}
	stored := 0
	var firstErr error
	for _, note := range notes {
		if err := tools.ChatNoteRecord(tools.ChatNote{
			SessionID: session,
			Turn:      turn,
			Note:      note,
			Path:      path,
		}); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		stored++
	}
	return stored, firstErr
}

// recordPathIn names the record file inside a run's session directory: the same
// join appendSelfRecordFile performs, kept as ONE spelling so the path stored
// in the index and the path actually written can never drift apart (Rule 14).
func recordPathIn(dir string) string {
	return filepath.Join(dir, selfRecordFileName)
}

// noteTurnNumber resolves the TURN a recorded note belongs to — the number the
// file line and the index row both carry.
//
// IT IS NOT A SECOND COUNT OF ANYTHING (Rule 14). A turn begins at every
// COMPLETE INPUT, and the ONE predicate that decides that is
// sessionevent.IsTurnOpeningRow: internal/gopilog's reader turns it into
// TurnBoundary, SessionTurnBounds numbers the boundaries it produces, and
// `gopi session log <id> --turns` / `gopi session replay --turn N` select by
// that number. This function adds NO rule of its own; it asks the WRITER half of
// that same count for the number THIS run is on:
//
//		turn = turnBaseline + SessionLogger.TurnsSoFar()
//
//	  - TurnsSoFar is the writer's count of the turn-opening `user` rows it has
//	    written, advanced in LogUserFromAt under the very same predicate the
//	    reader applies (session.go) — which is why the two halves cannot drift and
//	    why the chat surface, whose per-turn agent is re-created, still numbers
//	    consecutively.
//	  - turnBaseline is how many turn-opening rows the record ALREADY HELD when
//	    this run started. It is 0 for a fresh session, and for a resumed one the
//	    caller stamps Agent.TurnBaseline from the log's own count
//	    (chatResumeTurnBaseline → chatTurnOrdinalAt → gopilog.SessionTurns, the
//	    exact producer the display surfaces use). Without it a resumed run would
//	    stamp every note from turn 1 while the reader calls the same note turn
//	    N+1 — the permanent offset task 01a16fb3 removed for the screen.
//
// A note is recorded BEFORE its turn's `request` row is logged (every recording
// path runs at the end of a response), and the turn-opening row was already
// written when the turn began, so the count at this instant IS the current turn
// — there is nothing "in flight" to add.
//
// A run with no session log has nothing to count: it stamps the explicit absence
// (sessionevent.RoundIndexUnknown, "no number recorded") rather than fabricating
// a turn 1.
func (a *Agent) noteTurnNumber() int {
	if a.SessionLog == nil {
		return sessionevent.RoundIndexUnknown
	}
	return a.TurnBaseline + a.SessionLog.TurnsSoFar()
}
