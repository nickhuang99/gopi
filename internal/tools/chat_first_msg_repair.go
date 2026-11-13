package tools

// chat_first_msg_repair.go — THE RE-COMPUTABLE REPAIR for rows whose head column
// was written by the old rule.
//
// WHAT WENT WRONG IS ALREADY IN THE DATA. Before the fix in chat_first_msg.go,
// `ChatEnsureRow` stored whatever non-empty text the turn carried, so a
// conversation whose last input was a `gopi notify` delivery ended up titled
// with the framework's own sentence — measured on the operator's profile
// 2026-10-28:
//
//	66243525-16ab-4ce7-9133-84d74c539840  [chat]  human  [system] 16 notices are pending: …
//	4a0957a3-bb4e-4337-a761-20606c87d1fd  [chat]  human  [system] An external process sent this session a message: …
//
// Those rows need no archaeology: the AUTHORITATIVE record is the session JSONL
// the row already points at, and it carries the face of every `user` row. The
// repair therefore RE-DERIVES the head with the same predicate the live path
// uses (chatLineIsOperatorOwn over the recorded source), which makes it:
//
//   - RECOMPUTABLE — running it twice is the same as running it once, and its
//     answer for a given log never drifts, because both sides read one function;
//   - HONEST ABOUT GAPS — a row whose log is not on this machine is REPORTED and
//     left alone (Rule 15). It is not blanked, not guessed, and not counted as
//     repaired: "I could not re-derive this one" is its own line in the report.
//
// IT DOES NOT DISCARD ANYTHING. The text being replaced is the framework's
// sentence about an input that is still in the JSONL, verbatim, and still counted
// by turn_count. The chats table is an INDEX over that file; this repair corrects
// the index entry, never the record.
//
// WHO CALLS IT: `gopi chat backfill --repair-first-msg` (cmd/agent), the verb
// that already owns "make the chats index agree with the session logs". No new
// verb, no new resident process, no scheduled job (Rule 35: prefer what already
// runs).

import (
	"context"
	"fmt"
	"strings"
)

// ChatFirstMsgRepair is one row's verdict — the before/after a reader needs to
// audit a repair run, and the reason when a row was left alone.
type ChatFirstMsgRepair struct {
	SessionID string
	// Before is the stored value at scan time.
	Before string
	// After is what the repair wrote ("" when it wrote nothing).
	After string
	// Reason is "" for a repaired row, and otherwise says why it was left as it
	// was: already the operator's own line, or the log could not be read.
	Reason string
}

// ChatFirstMsgRepairReasons — the two ways a scanned row is NOT rewritten, spelled
// once so the report and its tests read the same words (Rule 1/13).
const (
	// ChatFirstMsgRepairKept is: the stored value already passes the live
	// predicate, so there is nothing to fix.
	ChatFirstMsgRepairKept = "already the operator's own line"
	// ChatFirstMsgRepairNoLogPrefix opens the note for a row whose session log
	// this machine does not hold — the re-derivation has nothing to read.
	ChatFirstMsgRepairNoLogPrefix = "cannot re-derive — "
	// ChatFirstMsgRepairChangedUnderScan is: the column moved between the scan
	// and the write (a turn landed meanwhile), so this run skipped it. A re-run
	// picks it up; it is NOT the same cause as ChatFirstMsgRepairKept.
	ChatFirstMsgRepairChangedUnderScan = "changed during the scan — re-run"
	// ChatFirstMsgRepairNothingToChange is: the re-derived value IS what the row
	// already holds (its log has no operator line, and it says so). A second run
	// meets this for every row the first run repaired the placeholder onto.
	ChatFirstMsgRepairNothingToChange = "nothing to change — the log has no operator line yet"
)

// ChatRepairFirstMsgs re-derives the head column of every chat row that does not
// already hold the operator's own first line, and reports one row per scan
// (repaired or not). dryRun reports without writing.
//
// A row is CANDIDATE when its stored first_msg fails chatLineIsOperatorOwn under
// the human face — i.e. it is empty, it is the placeholder, or it carries a
// harness marker. That test is deliberately the TEXT half of the one predicate:
// the stored string does not record which face wrote it (the old writer never
// stored one), and the marker is exactly what separates the two cases.
func ChatRepairFirstMsgs(dryRun bool) ([]ChatFirstMsgRepair, error) {
	ctx := context.Background()
	db, err := chatIndexOpenDB(ctx, dryRun)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT session_id, first_msg, log_path FROM chats ORDER BY started_at`)
	if err != nil {
		return nil, fmt.Errorf("chat first-msg repair: select: %w", err)
	}
	type scanned struct{ sessionID, firstMsg, logPath string }
	var scannedRows []scanned
	for rows.Next() {
		var r scanned
		if err := rows.Scan(&r.sessionID, &r.firstMsg, &r.logPath); err != nil {
			rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover
			return nil, fmt.Errorf("chat first-msg repair: scan: %w", err)
		}
		scannedRows = append(scannedRows, r)
	}
	rowsErr := rows.Err()
	rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover
	if rowsErr != nil {
		return nil, fmt.Errorf("chat first-msg repair: rows: %w", rowsErr)
	}

	out := make([]ChatFirstMsgRepair, 0, len(scannedRows))
	for _, r := range scannedRows {
		stored := strings.TrimSpace(r.firstMsg)
		if chatLineIsOperatorOwn(stored, "") && stored != ChatFirstMsgPlaceholder {
			out = append(out, ChatFirstMsgRepair{SessionID: r.sessionID, Before: stored, Reason: ChatFirstMsgRepairKept})
			continue
		}
		facts := chatSessionFactsOf(r.sessionID)
		if facts == nil {
			out = append(out, ChatFirstMsgRepair{
				SessionID: r.sessionID, Before: stored,
				Reason: ChatFirstMsgRepairNoLogPrefix + "this machine does not hold the session log",
			})
			continue
		}
		after := ChatFirstMsgPlaceholder
		if facts.FirstMsg != "" {
			after = normalizeChatText(facts.FirstMsg)
		}
		if after == stored {
			// NOTHING TO CHANGE: the log has no operator line yet and the row
			// already says so. This is the row shape a second run meets, and
			// counting it as a repair made the first run's report overstate its
			// work — measured 2026-10-30: a re-run printed
			// `repair bf1ae7ff-…: "(no operator line yet)" -> "(no operator line yet)"`
			// and `repaired=24`. A repair that "wrote" the same bytes is not a
			// repair, and a report that says it is has lied about it.
			out = append(out, ChatFirstMsgRepair{
				SessionID: r.sessionID, Before: stored, Reason: ChatFirstMsgRepairNothingToChange,
			})
			continue
		}
		rec := ChatFirstMsgRepair{SessionID: r.sessionID, Before: stored}
		if dryRun {
			rec.After = after
			out = append(out, rec)
			continue
		}
		// The write is a COMPARE-AND-SWAP on the value just scanned: it lands
		// only while the column still holds exactly that, so a line the operator
		// typed during the scan is never overwritten.
		res, uerr := db.ExecContext(ctx, chatRepairFirstMsgSQL, after, r.sessionID, r.firstMsg)
		if uerr != nil {
			return out, fmt.Errorf("chat first-msg repair: write %s: %w", r.sessionID, uerr)
		}
		if n, aerr := res.RowsAffected(); aerr == nil && n == 0 {
			// The column moved under us (a turn landed during the scan). Report
			// THAT, not "already the operator's line": two causes never share one
			// sentence (Rule 15), and this one is a skipped row a re-run fixes.
			rec.Reason = ChatFirstMsgRepairChangedUnderScan
			out = append(out, rec)
			continue
		}
		rec.After = after
		out = append(out, rec)
	}
	return out, nil
}
