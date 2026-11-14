package tools

// task_fixture_reclaim.go — a fixture row's CREATOR is machine-readable, and a
// fixture PENDING row whose creator is provably TERMINAL is collected.
//
// THE DEFECT THIS CLOSES (measured on this host's ledger, 2026-11-05): of 87
// historical `fixture`-tagged rows, 3 sat in `pending` for ever and were
// cleaned by hand. The self-healing predicate "the creator is terminal ⇒ the
// row is its decoration and may be collected" could not even be EVALUATED,
// because the row's creator was unreadable by any machine: `task show` rendered
// the empty placeholder (`submitted_by:   LLM session — (task_submit)`),
// `metadata.json` carried neither a submitter nor a session key, and
// `parent_task_id` was empty. A row whose creator is anonymous can never be
// proved abandoned.
//
// THE TWO HALVES, in the order they matter:
//
//  ① THE STAMP (taskSubmitCreatorSessionOf). `gopi task submit` with no
//    `--session-id` used to record the submitter as "" — the CLI never fills it
//    and nothing fell back to the chain that already knows who is asking
//    (GOPI_TASK_CHILD_SESSION / GOPI_EXECUTOR_SESSION_ID, the SAME pair
//    taskReportSessionID reads). A row filed by a run therefore named NOBODY.
//    It now names the run, and the same value lands in the row's session
//    metadata.json under the one key below (taskWriteSubmitMetadata).
//
//  ② THE RECLAIM (FixtureReclaimSweep). A `fixture` PENDING row whose recorded
//    creator has provably reached a terminal state is transitioned to
//    `cancelled` — the ONLY terminal status legal from `pending`
//    (taskAllowedTransitions), through THE ONE pending→cancelled writer
//    (taskCancelPendingCAS, the same conditional UPDATE the cross-ledger move
//    uses, so a claim racing this sweep wins and this arm reports the winner).
//
// WHAT THE ARM MUST REFUSE TO DO, and says so (Rule 15):
//
//   - a creator that is LIVE keeps its row: it is a probe in active use, and
//     the funnel already refuses to dispatch it (Rule 33) — nothing is wrong.
//   - a creator that cannot be RESOLVED keeps its row. An unreadable ledger, a
//     malformed session id, a session this host has no record of: each is a
//     KEPT row WITH its reason, never a guess. "I could not tell" is not
//     "abandoned".
//   - the dispatch funnel is untouched. This arm REMOVES pending fixture rows
//     once their author is gone; it never makes one claimable, and it never
//     touches a row that is not `pending` + `fixture`.
//
// NO NEW PERSISTENT SURFACE (Rule 35). There is no timer, no cron line, no
// config key and no state file here: the arm is a function, and it hangs on the
// upkeep tick this host already fires — `gopi tool scratch-gc tick`, whose
// resident cadence is scratchGCDefaultIntervalMin (60 minutes) and whose
// host-level carrier is the scratch-gc watchdog unit (OnUnitActiveSec=30min).
// The throttle is that tick's own; this file keeps no clock of its own.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/tasktags"
)

const (
	// TaskSubmitCreatorSessionKey is the metadata.json key a submit writes the
	// filing session into. ONE spelling (Rule 1b): the writer below and every
	// reader cite this constant, so the row's ledger column and the session's
	// metadata cannot disagree about WHICH key answers "who filed this".
	TaskSubmitCreatorSessionKey = "submitted_by_session"

	// taskSessionMetadataFileName is the submit skeleton's own file name — the
	// same file the run-time logger merges into (task_session.go owns the
	// writer; this constant names it here only for the READ side of the reclaim,
	// so the two cannot drift).
	taskSessionMetadataFileName = "metadata.json"
)

// CreatorOutcome is the verdict of asking "is the session that filed this row
// still around?" — a VALUE that always carries its own reason, because the
// reclaim's entire safety rests on the difference between LIVE and UNKNOWN
// (Rule 15: an unknown that reads like a terminal would reap a live probe).
type CreatorOutcome struct {
	// SessionID is the creator this verdict is about ("" when the row records
	// none at all).
	SessionID string
	// Live reports the creator is PROVABLY still running.
	Live bool
	// Terminal reports the creator is provably FINISHED (its own run row is
	// terminal, or its recorded transcript carries an ending).
	Terminal bool
	// Evidence is the sentence that justifies the verdict — the source it was
	// read from and the value found there. Always non-empty.
	Evidence string
}

// CreatorsKnown reports whether a verdict is a decision at all: a Live or
// Terminal answer is a measurement, everything else is "could not tell".
func (o CreatorOutcome) Known() bool { return o.Live || o.Terminal }

// TaskSubmitCreatorSession resolves the session that FILED a submit. The
// explicit --session-id wins; otherwise the run's own identity, from the pair
// taskReportSessionID reads (task child session, then executor session), and
// finally the bare agent-session anchor a CLI run inherits. Empty means this
// process really has no session to name — and a row filed by such a process
// keeps recording that absence rather than inventing an id (Rule 15).
func TaskSubmitCreatorSession(explicit string) string {
	if s := strings.TrimSpace(explicit); s != "" {
		return s
	}
	if s := taskReportSessionID(); s != "" {
		return s
	}
	return strings.TrimSpace(os.Getenv(TaskRunIDEnv))
}

// taskCreatorOutcome answers the reclaim's ONE question about a row: was the
// session that filed it still alive, provably finished, or neither?
//
// The order is deliberate and it is the whole safety argument:
//
//  1. NO recorded creator → UNKNOWN. Not terminal (that would reap a row whose
//     author we never identified), not live. The row is kept with this reason.
//  2. THE LEDGER's own answer first: a `tasks` row whose session_id is this
//     session and whose status is terminal is a FINISHED run — the ledger is
//     the one record every run writes. A non-terminal run row means the session
//     is a RUN on this host, and a run row that is claimed/in_progress is LIVE.
//     (A pending run row names a run that has not started; the session itself
//     is then unmeasured, so we fall through to the transcript below.)
//  3. THE TRANSCRIPT: the session's own metadata.json/session log. A recorded
//     ending (ended_at / end_reason, the chat index's two columns) is terminal
//     evidence.
//
// Anything that errors, reads empty, or does not match is UNKNOWN — kept, and
// said. There is deliberately no "assume terminal" arm: a wrong TERMINAL reaps
// a live probe, which is the one outcome this arm must never produce.
func taskCreatorOutcome(creator string) CreatorOutcome {
	sid := strings.TrimSpace(creator)
	if sid == "" {
		return CreatorOutcome{Evidence: "the row records no creator session, so its author's state cannot be measured"}
	}
	out := CreatorOutcome{SessionID: sid}

	// ② the ledger's run-row verdict.
	status, found, err := taskSessionRunStatus(sid)
	if err != nil {
		return CreatorOutcome{SessionID: sid,
			Evidence: fmt.Sprintf("the ledger could not be read for creator session %s (%v), so its state is unknown", sid, err)}
	}
	if found {
		switch status {
		case taskStatusPending:
			// A run row that has not started: the session exists on this host
			// but has done nothing yet. Fall through to the transcript.
		default:
			if taskStatusIsTerminal(status) {
				out.Terminal = true
				out.Evidence = fmt.Sprintf("creator session %s owns run row %s with status=%s (a finished run)", sid, sid, status)
				return out
			}
			out.Live = true
			out.Evidence = fmt.Sprintf("creator session %s owns run row %s with status=%s (a run that has not concluded)", sid, sid, status)
			return out
		}
	}

	// ③ the transcript's own ending.
	ended, detail := taskSessionTranscriptEnded(sid)
	switch {
	case ended:
		out.Terminal = true
		out.Evidence = fmt.Sprintf("creator session %s's transcript records an ending (%s)", sid, detail)
	case detail != "":
		out.Live = true
		out.Evidence = fmt.Sprintf("creator session %s has a transcript on this host with no recorded ending (%s)", sid, detail)
	default:
		out.Evidence = fmt.Sprintf("creator session %s is recorded in neither the ledger nor this host's transcripts, so its state is unknown", sid)
	}
	return out
}

// taskStatusIsTerminal is the TERMINAL set a creator's run row is judged by —
// the same four words every reader of "is this row over?" uses (Rule 14: one
// definition, referenced). It is deliberately NOT taskStatusTerminalPair
// (completed|failed), which exists for the writeback's exit-code arithmetic and
// would call a verified or superseded run unfinished.
func taskStatusIsTerminal(s string) bool {
	switch s {
	case taskStatusCompleted, taskStatusFailed, taskStatusCancelled, taskStatusVerified, taskStatusSuperseded:
		return true
	default:
		return false
	}
}

// taskSessionRunStatus answers "what is the status of the RUN whose session is
// sid?" — and the lookup column is task_id, NOT session_id.
//
// That is not an optimisation, it is the identity: a dispatched run's session
// id IS its task id (GOPI.md §4.12, "worker 的 session id 就等于它的 task id" —
// the claim writes the same string into both). The row's own `session_id`
// column is something else entirely: it is empty for a row filed by a process
// that had no run identity to give it (measured on the probe below —
// `session_id:` blank while the row's task_id is the very session that filed
// the fixtures). Looking up `session_id = sid` therefore found NOTHING for a
// real creator and the arm fell through to the transcript for every row,
// reporting "unknown" for a creator whose run row was demonstrably cancelled.
// Measured, then fixed: this is the whole reason the ledger arm is read first.
func taskSessionRunStatus(sid string) (string, bool, error) {
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return "", false, err
	}
	defer db.Close() //stylegate:ignore the handle is this read's own; every failure is returned to the caller
	var status string
	err = db.QueryRowContext(ctx,
		`SELECT status FROM tasks WHERE task_id = ? ORDER BY updated_at DESC LIMIT 1`, sid).Scan(&status)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return status, true, nil
}

// taskSessionTranscriptEnded reads the session's own metadata.json for a
// recorded ending. detail is a human sentence (non-empty whenever the file was
// READ, even if it recorded no ending) so the caller can distinguish "read it,
// still open" from "could not read it at all".
func taskSessionTranscriptEnded(sid string) (ended bool, detail string) {
	root, rerr := config.GopiDir()
	if rerr != nil || strings.TrimSpace(root) == "" {
		return false, ""
	}
	dir := filepath.Join(root, config.SessionsDirName, sid)
	b, err := os.ReadFile(filepath.Join(dir, taskSessionMetadataFileName))
	if err != nil {
		return false, ""
	}
	var meta map[string]any
	if err := json.Unmarshal(b, &meta); err != nil {
		return false, "metadata.json present but unreadable"
	}
	if v, ok := meta[taskSessionEndedAtKey]; ok {
		if f, ok := v.(float64); ok && f > 0 {
			return true, taskSessionEndedAtKey + " present"
		}
	}
	if v, ok := meta[taskSessionEndReasonKey]; ok {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			return true, taskSessionEndReasonKey + "=" + s
		}
	}
	return false, "metadata.json read, no ending recorded"
}

// The two keys a session transcript uses to record that it is OVER. They match
// the chat index's own column names (internal/tools/chat.go's chats table:
// `ended_at` and `end_reason`), named here so a reader greps ONE spelling.
const (
	taskSessionEndedAtKey   = "ended_at"
	taskSessionEndReasonKey = "end_reason"
)

// FixtureReclaimResult is one pass's outcome, in the shape an operator reads:
// what was collected, what was KEPT and why. Nothing here is a success counter
// alone — a kept row without its reason would be the silence this arm exists to
// end (Rule 15).
type FixtureReclaimResult struct {
	Cancelled []FixtureReclaimRecord
	Kept      []FixtureReclaimRecord
}

// FixtureReclaimRecord is one row this pass judged.
type FixtureReclaimRecord struct {
	TaskID  string
	Creator string
	Verdict string // "cancelled" or "kept"
	Reason  string
}

// fixtureReclaimReason is the ONE sentence a collected row carries. It names
// the creator, the evidence, and what the row was (Rule 15: the trail is the
// only place a reader can learn why a row vanished from pending).
func fixtureReclaimReason(o CreatorOutcome) string {
	return fmt.Sprintf("fixture row reclaimed: its creator session %s is terminal (%s) — the seeded decoration outlived the run that filed it", o.SessionID, o.Evidence)
}

// FixtureReclaimSweep collects every pending `fixture` row whose creator is
// provably terminal, and KEEPS — with its reason — every row it cannot prove.
//
// It is a PASS, not a scheduler (Rule 35): the caller decides when it runs
// (`gopi tool scratch-gc tick` is the resident carrier; `gopi task reclaim-fixtures`
// runs one pass by name). Fail-soft per row: one unreadable creator never
// aborts the pass, and the pass never fails a host task.
func FixtureReclaimSweep(dbPath string) (*FixtureReclaimResult, error) {
	res := &FixtureReclaimResult{}
	err := syncRowWithLedger(dbPath, func() error {
		rows, qerr := fixturePendingRows()
		if qerr != nil {
			return qerr
		}
		for _, row := range rows {
			creator := taskSubmitterSessionOf(row)
			o := taskCreatorOutcome(creator)
			rec := FixtureReclaimRecord{TaskID: row.TaskID, Creator: o.SessionID}
			if !o.Terminal {
				rec.Verdict = "kept"
				rec.Reason = o.Evidence
				res.Kept = append(res.Kept, rec)
				continue
			}
			outcome, cerr := taskCancelPendingCASByID(row.TaskID, fixtureReclaimReason(o))
			if cerr != nil {
				rec.Verdict = "kept"
				rec.Reason = fmt.Sprintf("the cancel write failed (%v) — the row is unchanged", cerr)
				res.Kept = append(res.Kept, rec)
				continue
			}
			if !outcome.Cancelled {
				rec.Verdict = "kept"
				rec.Reason = fmt.Sprintf("another writer reached the row first (it is now %s, not %s) — nothing was cancelled here", outcome.Observed, taskStatusPending)
				res.Kept = append(res.Kept, rec)
				continue
			}
			rec.Verdict = "cancelled"
			rec.Reason = fixtureReclaimReason(o)
			res.Cancelled = append(res.Cancelled, rec)
		}
		return nil
	})
	if err != nil {
		return res, err
	}
	return res, nil
}

// fixturePendingRows returns every PENDING row carrying the reserved fixture
// tag. The filter is done in SQL on status and re-checked in Go by the ONE
// tag-token predicate (taskHasTagToken), so a tag spelling like `fixture-x` or
// `notaf-fixture` cannot be mistaken for the reserved token.
func fixturePendingRows() ([]*Task, error) {
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return nil, err
	}
	defer db.Close() //stylegate:ignore the handle is this read's own; every failure is returned to the caller
	rows, err := db.QueryContext(ctx,
		`SELECT `+taskSelectColumns+` FROM tasks WHERE status = ? AND tags LIKE ?`,
		taskStatusPending, "%"+tasktags.FixtureTag+"%")
	if err != nil {
		return nil, fmt.Errorf("fixture reclaim: pending fixture scan: %w", err)
	}
	defer rows.Close() //stylegate:ignore closing a read-only result set cannot fail in a way the caller could act on
	var out []*Task
	for rows.Next() {
		t, serr := scanTaskInto(rows)
		if serr != nil {
			return nil, fmt.Errorf("fixture reclaim: scan row: %w", serr)
		}
		if t == nil || !taskHasTagToken(t.Tags, tasktags.FixtureTag) {
			continue
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fixture reclaim: pending fixture scan: %w", err)
	}
	return out, nil
}

// taskCancelPendingCASByID is the ledger-opening face of THE ONE
// pending→cancelled conditional write, reusing taskCancelPendingCAS so this arm
// and the cross-ledger move share a single spelling of the transition (Rule 14).
func taskCancelPendingCASByID(taskID, reason string) (*PendingCancelOutcome, error) {
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return nil, err
	}
	defer db.Close() //stylegate:ignore the handle belongs to this write; every error is returned to the caller
	return taskCancelPendingCAS(ctx, db, taskID, reason)
}
