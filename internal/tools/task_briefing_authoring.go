package tools

// task_briefing_authoring.go — WHERE a briefing's TEXT was written, as opposed
// to where its ROW was created. It is the ledger accessor behind the dispatch
// seam's gate-assertion anchor (TaskBriefingGateAssertRefresh's authoredAt).
//
// THE MEASURED DEFECT IT CLOSES (2026-10-27, task 01a179e7 / row
// 01a159e5-…-r1). The seam anchored a briefing's "<surface> is RED on master"
// assertion to the ROW's created_at. That is right for a row whose payload was
// written with the row — and WRONG for a rework/resume successor, whose payload
// is the parent's text copied VERBATIM (TaskCloneWithOptions:
// `Payload: target.Payload + marker + reason`, and its tags carry
// tasktags.ReworkLineageTag) while its created_at is the successor's own. The
// seam then re-anchored a SIX-DAY-OLD assertion to a six-day-later tree and
// printed evidence that was precise and pointed at the wrong revision:
//
//	anchor as read (rework row)   epoch 1793040588 → 0d313323, master 8   commits ahead
//	the text's real writing time  epoch 1792509551 → bf40185,  master 381 commits ahead
//
// Two sessions went to reproduce a red that had never existed at the revision
// the stamp named. A stamp that is wrong ABOUT THE MOMENT is worse than no
// stamp: it spends a run's budget and it is believed.
//
// WHAT IS READ, and why it is not a guess. The ledger itself records that a
// payload was not written by the row that carries it:
//
//	1. THE LINEAGE EDGE — tasktags.ReworkLineageTag + parent_task_id. A row
//	   stamped at birth as a rework successor inherited its text, so the moment
//	   to anchor is the moment that text was written — the ancestor's.
//	2. THE TEXT ITSELF, which is the corroboration and the reason this reader
//	   does not simply trust the tag: the successor's payload is the parent's
//	   payload as a PREFIX (one string concatenation, no rewriting), and adding
//	   `-rN` does not edit the briefing body. A row carrying the lineage mark
//	   whose payload does NOT contain its parent's is therefore NOT a clean
//	   copy — the copy decision is refused and the moment is UNMEASURED rather
//	   than anchored to a row that cannot be shown to have written these bytes.
//	3. THE REVISION TRAIL — task_events(event=PayloadRevisionEventName) carries
//	   the epoch of every in-place payload revision (TaskSetPayload). A briefing
//	   rewritten after its row was created has a LATER writing moment than the
//	   row's created_at, and the trail is the only place that moment exists.
//
// THE WALK IS BOUNDED (taskBriefingLineageMaxRows) and a walk that runs out
// returns UNMEASURED naming the bound and the row it stopped at: a cycle or a
// pathological chain must not become an anchor.
//
// NO NEW MECHANISM (Rule 35): one read-only accessor beside TaskCreatedAt, on
// the same opener (openTasksDBReadOnly), reading columns and an event table the
// ledger already has. No schema change, no new file, no resident anything.
//
// WHAT IT DELIBERATELY DOES NOT DO: it does not recover a REVISION of an
// ancestor that landed before the copy. That edit is folded into the text the
// successor inherited, and its epoch is not in the successor's trail — so the
// computed moment can be EARLIER than the text's true writing time by exactly
// those revisions. That direction is the conservative one (a shorter distance,
// therefore a possible missed stamp) and never the false one this defect is
// about (a stamp pointing at a revision the text was not written at).

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/nick/gopi/internal/tasktags"
)

// BriefingPayloadCopyEventName is the `task_events.event` value the rework
// reissue appends when a successor row inherits its parent's payload. It exists
// so the derivation is a ledger FACT a reader can grep, not a rule re-applied
// from the code on every later query: the cast that followed the regression
// asked "was this payload copied?" about a row written days earlier, and the
// answer has to be on disk rather than in whichever binary read it.
const BriefingPayloadCopyEventName = "briefing_payload_copied"

// taskBriefingLineageMaxRows bounds the ancestry walk (Rule 1: a named ceiling,
// never an inline number). A chain of reissues deeper than this is UNMEASURED
// and says so — the bound protects the walk from a cycle, and an anchor minted
// at the point the walk gave up would be a guess wearing a revision.
const taskBriefingLineageMaxRows = 8

// taskBriefingAuthoringReasons are the stated reasons a briefing's writing
// moment is unavailable. Named, because a sentence rendered at a fmt site is the
// shape Rule 1 forbids, and because the seam's record is the only place a reader
// learns WHY an anchor went unmeasured (Rule 15).
const (
	taskBriefingAuthoringParentMissingRsn = "the parent row it names is not in the ledger, so the text it inherited cannot be read back"
	taskBriefingAuthoringNotACopyRsn      = "the row carries the rework lineage mark, but its payload does NOT contain the parent's payload — it is not a verbatim copy, so neither row's clock can be called the briefing's writing moment"
	taskBriefingAuthoringNoMomentRsn      = "neither the row nor the row it inherited from carries a usable payload writing moment (created_at, or a recorded payload revision)"
)

// taskBriefingAuthoringChainDepthReasonFmt is the reason for the BOUNDED walk
// (Rule 1): the number of rows walked and the row the walk stopped at.
const taskBriefingAuthoringChainDepthReasonFmt = "the ancestry walk stopped after %d row(s) at %q without reaching a row whose payload is its own text"

// The record lines. One spelling each (Rule 1/13): the seam's own record and
// the tests read the same strings.
const (
	// taskBriefingAuthoringOwnRecordFmt: the ordinary row — its payload is its
	// own text, so its created_at IS the briefing's writing moment.
	taskBriefingAuthoringOwnRecordFmt = "briefing authoring → %q: the payload is the row's OWN text (no %q lineage mark), so its created_at %s (epoch %d) is the briefing's writing moment\n"
	// taskBriefingAuthoringCopiedRecordFmt: the rework/resume successor — the
	// payload is the parent's text, so the anchor must be the PARENT's moment,
	// and the line names both readings so a reader sees which one was used.
	taskBriefingAuthoringCopiedRecordFmt = "briefing authoring → %q: the payload was COPIED VERBATIM from %q (%s, recorded as task_events %q), so the briefing's writing moment is %s (epoch %d) — NOT this row's own created_at %s (epoch %d)\n"
	// taskBriefingAuthoringRevisedRecordFmt: the payload was rewritten IN PLACE
	// after the row was created, and the revision trail holds the later moment.
	taskBriefingAuthoringRevisedRecordFmt = "briefing authoring → %q: the payload was REVISED IN PLACE at %s (epoch %d, task_events %q), later than the row's own created_at %s (epoch %d), so the revision is the briefing's writing moment\n"
	// taskBriefingAuthoringUnmeasuredRecordFmt: nothing was anchored, and the
	// reason is stated — a raised error has already been reported above this
	// line by the caller when err != nil.
	taskBriefingAuthoringUnmeasuredRecordFmt = "briefing authoring → UNMEASURED for %q — %s; nothing was anchored to an invented moment, so a gate assertion in this briefing is NOT re-measured at dispatch\n"
)

// taskBriefingLineageEdgeLabel is how a payload copy is described in the record:
// the lineage mark that says the row inherited its text, plus the edge it
// inherited it over. One spelling, used by the record line above.
const taskBriefingLineageEdgeLabel = "tasktags." + tasktags.ReworkLineageTag + " lineage mark beside parent_task_id"

// TaskBriefingAuthoringAt returns the epoch second the briefing TEXT of one row
// was written — the anchor a "SURFACE is RED on <branch>" assertion carries —
// plus the record lines explaining which row supplied it and, when nothing
// could, why not.
//
// Semantics, all of them measured off the ledger rather than assumed:
//
//   - a row whose payload is its OWN text anchors at max(created_at, its latest
//     recorded payload revision);
//   - a rework/resume successor whose payload is a verbatim copy of its
//     parent's anchors at the PARENT's moment (recursively, bounded) merged
//     with any revision the successor itself recorded after the copy;
//   - anything the walk cannot establish returns has=false and a record line
//     naming the reason. It never invents a moment, and a floor of 0 with
//     has=false is the caller's cue to leave every assertion UNMEASURED.
//
// The error return is reserved for a ledger that cannot be read at all (the
// same contract as TaskCreatedAt); "read fine, no anchor" is (0, false, …) with
// its reason in the record.
func TaskBriefingAuthoringAt(taskID string) (at float64, has bool, record []string, err error) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return 0, false, nil, fmt.Errorf("task: briefing authoring lookup: task_id must not be empty")
	}
	ctx := context.Background()
	db, err := openTasksDBReadOnly(ctx, memoryDBPath())
	if err != nil {
		return 0, false, nil, err
	}
	defer db.Close()
	row, err := taskByID(ctx, db, taskID)
	if err != nil {
		return 0, false, nil, fmt.Errorf("task: briefing authoring lookup: %w", err)
	}
	if row == nil {
		return 0, false, nil, fmt.Errorf("task: no task with task_id=%q%s", taskID, taskIDPrefixRefusal(ctx, db, taskID))
	}
	// The row's OWN moment: created_at, later-moved by a recorded revision.
	ownAt, ownRevised, err := taskPayloadWrittenAt(ctx, db, row)
	if err != nil {
		return 0, false, nil, fmt.Errorf("task: briefing authoring: %w", err)
	}
	// THE LINEAGE WALK. `cur` is the row whose text we currently believe IS the
	// briefing; it starts as the dispatched row and moves to the parent each
	// time the copy is PROVEN.
	cur := row
	curAt, _, err := taskPayloadWrittenAt(ctx, db, cur)
	if err != nil {
		return 0, false, nil, fmt.Errorf("task: briefing authoring: %w", err)
	}
	for depth := 0; ; depth++ {
		if depth > taskBriefingLineageMaxRows {
			reason := fmt.Sprintf(taskBriefingAuthoringChainDepthReasonFmt, depth, cur.TaskID)
			return 0, false, []string{fmt.Sprintf(taskBriefingAuthoringUnmeasuredRecordFmt, taskID, reason)}, nil
		}
		parentID := strings.TrimSpace(cur.ParentTaskID)
		// A row that owns its payload, or that names no parent, ends the walk:
		// its own writing moment is the briefing's.
		if parentID == "" {
			break
		}
		if !taskHasTagToken(cur.Tags, tasktags.ReworkLineageTag) {
			break
		}
		parent, perr := taskByID(ctx, db, parentID)
		if perr != nil {
			return 0, false, nil, fmt.Errorf("task: briefing authoring: read parent %q: %w", parentID, perr)
		}
		if parent == nil {
			reason := fmt.Sprintf("%s (%q)", taskBriefingAuthoringParentMissingRsn, parentID)
			return 0, false, []string{fmt.Sprintf(taskBriefingAuthoringUnmeasuredRecordFmt, taskID, reason)}, nil
		}
		if strings.TrimSpace(parent.Payload) == "" {
			// Nothing was copied: this row's briefing is its own text, and the
			// parent's clock has no claim on it.
			break
		}
		if !strings.Contains(cur.Payload, parent.Payload) {
			reason := fmt.Sprintf("%s (%q)", taskBriefingAuthoringNotACopyRsn, parentID)
			return 0, false, []string{fmt.Sprintf(taskBriefingAuthoringUnmeasuredRecordFmt, taskID, reason)}, nil
		}
		parentAt, _, err := taskPayloadWrittenAt(ctx, db, parent)
		if err != nil {
			return 0, false, nil, fmt.Errorf("task: briefing authoring: %w", err)
		}
		// The copy is PROVEN, so the moment moves back with the text. The
		// dispatch record states the pair — this is the line the defect's
		// report is built from, and a reader must be able to see which of the
		// two clocks produced the anchor.
		record = append(record, fmt.Sprintf(taskBriefingAuthoringCopiedRecordFmt,
			cur.TaskID, parent.TaskID, taskBriefingLineageEdgeLabel, BriefingPayloadCopyEventName,
			formatTaskTime(parentAt), int64(parentAt), formatTaskTime(curAt), int64(curAt)))
		cur = parent
		curAt = parentAt
	}
	if curAt <= 0 {
		return 0, false, append(record, fmt.Sprintf(taskBriefingAuthoringUnmeasuredRecordFmt, taskID, taskBriefingAuthoringNoMomentRsn)), nil
	}
	// THE REVISION MERGE for the row the text CAME FROM is already inside curAt
	// (taskPayloadWrittenAt returns max(created_at, latest recorded revision)),
	// so what remains is the DISPATCHED row's own later revision: the inherited
	// text plus an in-place rewrite is a writing moment later than the copy.
	if ownRevised && ownAt > curAt {
		record = append(record, fmt.Sprintf(taskBriefingAuthoringRevisedRecordFmt,
			row.TaskID, formatTaskTime(ownAt), int64(ownAt), PayloadRevisionEventName, formatTaskTime(row.CreatedAt), int64(row.CreatedAt)))
		curAt = ownAt
	} else if cur == row {
		// The ordinary row: its payload is its own text, and its created_at IS
		// the briefing's writing moment. Recorded, never silent (Rule 15).
		record = append(record, fmt.Sprintf(taskBriefingAuthoringOwnRecordFmt,
			row.TaskID, tasktags.ReworkLineageTag, formatTaskTime(curAt), int64(curAt)))
	}
	return curAt, true, record, nil
}

// taskPayloadWrittenAt is the moment ONE row's payload column last changed:
// created_at, moved forward by the latest recorded in-place revision. It is the
// only place that merge happens (Rules 8/13), so the origin row and the
// dispatched row are read through the same arithmetic.
//
// A row with created_at <= 0 and no revision is (0, false, nil): "this row
// carries no usable clock", which the caller states rather than measuring.
func taskPayloadWrittenAt(ctx context.Context, db *sql.DB, row *Task) (float64, bool, error) {
	at := row.CreatedAt
	var revised sql.NullFloat64
	if err := db.QueryRowContext(ctx,
		`SELECT MAX(created_at) FROM task_events WHERE task_id = ? AND event = ?`,
		row.TaskID, PayloadRevisionEventName).Scan(&revised); err != nil {
		return 0, false, fmt.Errorf("read payload revisions of %q: %w", row.TaskID, err)
	}
	revisedAt := 0.0
	if revised.Valid && revised.Float64 > 0 {
		revisedAt = revised.Float64
	}
	if revisedAt > at {
		return revisedAt, true, nil
	}
	if at <= 0 {
		return 0, false, nil
	}
	return at, false, nil
}

// TaskBriefingCopyEventNote renders the `task_events` detail text recorded when
// a reissue hands its parent's payload to a successor row: the reason the copy
// happened, the moment it happened, and the sha256 of the text it carried. It
// is written by TaskCloneWithOptions through the ledger's ONE event writer and
// is what makes "was this payload copied?" answerable from the row's own trail
// instead of from the code path that happened to run.
//
// The payload itself is NOT duplicated into the detail: the successor still
// carries those bytes, and a second copy of a briefing inside every reissue's
// audit row is storage the ledger does not need. What is kept is the digest and
// the source — enough to prove the copy, and to prove WHICH text was copied if
// the parent's payload is ever revised afterwards.
func TaskBriefingCopyEventNote(parentID, successorID, payload, reason string) string {
	return fmt.Sprintf("payload copied from %s into %s\nreason: %s\npayload sha256 %s, %d bytes",
		parentID, successorID, strings.TrimSpace(reason), payloadRevisionDigest(payload), len(payload))
}
