package tools

// task_successor_shared.go — THE SUCCESSOR AXES, shared by the verbs that MINT
// a new row out of an existing one.
//
// WHY THIS FILE EXISTS (and why its name is not a verb). These symbols used to
// live inside the retired `gopi task reopen` engine, under `taskReopen*` names,
// and two of
// them had a SECOND production caller that had nothing to do with that verb —
// `gopi task clone` (clean-room copy) and `gopi task receive` (cross-node row
// pull) both mint a successor row and both need the same two things: a name that
// cannot collide (`<judged>-r<ordinal>`, never a hand-typed pattern) and the
// judged row's attachment manifest re-materialised in the NEW row's own session
// dir. When the reopen verb was deleted, the reference census
// (`git grep -n -w <symbol>`) said these had callers outside the mechanism, so
// they were MOVED here, in the same commit as the deletion, under names that say
// what they do rather than which verb first needed them (Rules 14/43 — a name
// that outlives its verb is a second reason for it to exist, and a reader who
// greps for `reopen` should find nothing).
//
// `-r` IS THE REISSUE SHAPE, and it is deliberately distinct from clone's `-c`
// (`taskCloneIsSuccessorID`): the two answer different questions ("this row's
// work was reissued" vs "this row was re-copied into a clean room"), and a
// shared predicate would have to guess which.
//
// ⇒ NEITHER SHAPE IS MINTED ANY MORE (operator ruling 2026-11-05, memory #27273:
// an id is an identity, and an appended ordinal destroys it for every downstream
// reader). What remains here is the SHAPE, kept because the rows written in it
// are on disk under ids that are primary keys and are therefore never renamed:
// taskSuccessorIsID and taskCloneIsSuccessorID are migration readers, and
// internal/scratch's IsMintedSessionContainerName recognises the container names
// those rows' directories carry. A grep for a live `<jid>-r<N>` WRITER finds
// none — the reissue row `gopi worker --resume` continues lives at its own id,
// and the copy that replaces one is `gopi task clone`, which mints a fresh uuid
// through taskIDGenerate.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// TaskSuccessorRepoCarriedMarker is the phrase a successor line carries when the
// new row WAS given a repo (inherited or overridden). One string for an operator
// or a test to match without re-parsing the blob (Rule 14).
const TaskSuccessorRepoCarriedMarker = "deliverable.repo="

// TaskSuccessorNoRepoMarker is the phrase a successor line carries when the new
// row could carry NO repo — neither an override nor a source repo. The row is
// still minted (the work is wanted), but the absence is STATED with its
// consequence and its remedy: an operator reading the line learns the successor
// is not startable as-is, instead of discovering it at `gopi worker` time
// (Rule 15 — the silent absence is what hid this defect for so long: the mint
// "succeeded" and the row was dead on arrival).
const TaskSuccessorNoRepoMarker = "carries no deliverable.repo"

// taskSuccessorNoRepoDetail is the rest of the stated-absent line: WHY it matters
// and the ONE remedy, spelled once.
const taskSuccessorNoRepoDetail = " — a successor with no repo cannot resolve a ground, so `gopi worker` will refuse it; re-run with --repo PATH (the source row records no repo to inherit)"

// TaskSuccessorAttachmentsCarriedMarker is the phrase a successor line carries
// when the new row received the source row's attachment manifest AND the replica
// bytes were re-materialised in the new row's own session dir.
const TaskSuccessorAttachmentsCarriedMarker = "attachments="

// TaskSuccessorNoAttachmentsMarker is the phrase a successor line carries when
// the source row had NO attachments to carry. Stated, never silent (Rule 15): an
// operator asking "did the successor get its evidence?" reads the answer here
// instead of running `task show` on the child.
const TaskSuccessorNoAttachmentsMarker = "carries no attachments (the source row archived none)"

// TaskSuccessorAttachmentNotCarriedMarker is the phrase a successor line carries
// for an attachment the new row could NOT receive — the source's manifest entry
// had no replica path, or the replica is gone from disk by the time the mint
// runs. The line names the ORIGINAL path and the source row, so the reader is
// sent to the one place the evidence still is.
const TaskSuccessorAttachmentNotCarriedMarker = "could NOT carry attachment"

// taskSuccessorIDFmt is the ONE spelling of the LEGACY reissue row's id: the
// judged row, the successor marker, and the ordinal.
//
// NOBODY MINTS IT (see this file's header): it is read by the shape test below
// and by internal/tools' session-container pin, and by nothing that writes a row.
// It stays because the `<jid>-r<N>` rows already on disk must keep being
// recognised — an id is the ledger's primary key, so those rows are not renamed.
const taskSuccessorIDFmt = "%s-r%d"

// taskSuccessorFirstOrdinal is the ordinal of the first successor (`<id>-r1`).
// The mint loops start here and the shape test accepts this or any higher
// ordinal.
const taskSuccessorFirstOrdinal = 1

// taskSuccessorIsID reports whether candidate is a successor row id minted off
// judgedID by taskSuccessorIDFmt (`<judged>-r<ordinal>`, ordinal >= the first).
// It is the shape half of the live-successor gate: a row that merely happens to
// carry judgedID as its parent (an ordinary child) is NOT a successor and must
// never be read as one (`<id>-r1x` is not a successor either).
func taskSuccessorIsID(judgedID, candidate string) bool {
	prefix := fmt.Sprintf(taskSuccessorIDFmt, judgedID, taskSuccessorFirstOrdinal)
	prefix = strings.TrimSuffix(prefix, strconv.Itoa(taskSuccessorFirstOrdinal))
	if !strings.HasPrefix(candidate, prefix) {
		return false
	}
	ordinal := candidate[len(prefix):]
	if ordinal == "" {
		return false
	}
	for _, r := range ordinal {
		if r < '0' || r > '9' {
			return false
		}
	}
	n, err := strconv.Atoi(ordinal)
	return err == nil && n >= taskSuccessorFirstOrdinal
}

// taskLiveSuccessorFind returns the OLDEST live successor of judgedID
// (status ∈ pending/claimed/in_progress — the ledger's own non-terminal set,
// taskLiveStatusSQLList, never a hand-typed list), or "" when the row has none.
//
// WHY THE GATE EXISTS. A mint is a SEARCH for a free `<id>-rN`: the first reissue
// finds `-r1` taken and silently takes `-r2`. Two sessions reissuing the same
// judged row (or one doing it twice) therefore produce two pending rows carrying
// the same payload — the same work dispatched twice, each with its own ground,
// racing to deliver. The work already exists in the ledger; the honest answer is
// to NAME it and stop, not to mint a twin (Rule 15). `rerouteMintSuccessor` is
// the live caller: it reads a candidate SET and mints one at a time, so a
// successor minted by ANOTHER writer between the SELECT and its mint must not be
// doubled.
//
// The predicate is the EDGE **and** the ID SHAPE: an ordinary child of the judged
// row also carries `parent_task_id = judgedID`, and moving those is
// ReworkReparentChildren's job — refusing a reissue because a child exists would
// make every judged row with children unreissuable.
func taskLiveSuccessorFind(ctx context.Context, db *sql.DB, judgedID string) (id, status string, err error) {
	shapePrefix := fmt.Sprintf(taskSuccessorIDFmt, judgedID, taskSuccessorFirstOrdinal)
	shapePrefix = strings.TrimSuffix(shapePrefix, strconv.Itoa(taskSuccessorFirstOrdinal))
	rows, err := db.QueryContext(ctx,
		`SELECT task_id, status FROM tasks
		  WHERE status IN (`+taskLiveStatusSQLList()+`)
		    AND (parent_task_id = ? OR substr(task_id, 1, ?) = ?)
		  ORDER BY id`,
		judgedID, len(shapePrefix), shapePrefix)
	if err != nil {
		return "", "", fmt.Errorf("read the live successors of %s: %w", judgedID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var candidate, candidateStatus string
		if err := rows.Scan(&candidate, &candidateStatus); err != nil {
			return "", "", fmt.Errorf("scan the live successors of %s: %w", judgedID, err)
		}
		// The substr already narrowed it; the shape test is what makes the id
		// pattern a fact rather than a prefix guess.
		if taskSuccessorIsID(judgedID, candidate) {
			return candidate, candidateStatus, nil
		}
	}
	if err := rows.Err(); err != nil {
		return "", "", fmt.Errorf("walk the live successors of %s: %w", judgedID, err)
	}
	return "", "", nil
}

// taskSuccessorLostAttachment is one manifest entry the new row could NOT
// receive, with the measured reason (a struct, not two parallel slices — a
// reason can never drift onto another entry's path).
type taskSuccessorLostAttachment struct {
	original string
	reason   string
}

// taskSuccessorCarryAttachments rebuilds a successor row's deliverable so it
// CARRIES the source row's attachment manifest, re-materialising every replica
// in the SUCCESSOR's OWN session dir, and returns the line fragment the caller
// appends to its own result.
//
// WHY THIS EXISTS (the epic's remaining half). The repo axis shipped earlier,
// and the deliverable builder deliberately carried nothing else — with the
// attachments array listed among the things "belonging to the source's run".
// That reading is right about the RUN DIRECTORY and wrong about the EVIDENCE:
// the successor's payload is the source's payload verbatim ("see attachment
// such that there exists some branches…" — measured on the operator's own
// reissue), so a successor without the manifest is dispatched to do work whose
// INPUT it does not have, and the only way back to the evidence is to open the
// source row. The successor then is not a rerun at all; it is a pointer.
//
// WHAT IS CARRIED, AND WHAT IS NOT:
//   - the manifest ENTRIES, in the source's order (the [attachmentN:] numbering
//     a briefing cites — "see attachment2" — must still name the same file);
//   - the BYTES, copied from the source's replica into the successor's own
//     session dir through taskAttachmentCopyTo, and the digest/size RE-MEASURED
//     from the copy that actually landed (a manifest entry copied but not
//     verified would be the same false claim one layer down);
//   - NOT the source's `worktree`/`branch` — run-allocated, always the
//     successor's own to mint.
//
// THE THREE-WAY OUTCOME IS STATED ON THE LINE (Rule 15), exactly like the repo
// axis: carried (with the count and the source it came from), none (the source
// archived nothing), or the per-entry loss — an entry whose replica path is empty
// or whose file is gone names its ORIGINAL path and the source row that still
// holds it, and is left OUT of the manifest rather than written as a path
// `task show` would print and the reader would then fail to open.
func taskSuccessorCarryAttachments(newID, parentID, deliverable, parentDeliverable string) (string, string, error) {
	manifestJSON := taskDeliverableParse(parentDeliverable).Attachments
	if len(manifestJSON) == 0 {
		return deliverable, " " + TaskSuccessorNoAttachmentsMarker, nil
	}
	var manifest []TaskAttachment
	if err := json.Unmarshal(manifestJSON, &manifest); err != nil {
		// Never a silent drop: an unparsable source manifest is a defect in the
		// SOURCE row, and the successor cannot know what to carry.
		return "", "", fmt.Errorf("the source row %s carries an unparsable attachments manifest: %w", parentID, err)
	}
	if len(manifest) == 0 {
		return deliverable, " " + TaskSuccessorNoAttachmentsMarker, nil
	}
	parentDir, err := taskSessionDir(parentID)
	if err != nil {
		return "", "", err
	}
	carried := make([]TaskAttachment, 0, len(manifest))
	var lost []taskSuccessorLostAttachment
	for _, a := range manifest {
		rel := strings.TrimSpace(filepath.ToSlash(a.ReplicaRelPath))
		if rel == "" {
			lost = append(lost, taskSuccessorLostAttachment{a.OriginalAbsPath, "the manifest entry records no replica path"})
			continue
		}
		// The replica path is the source's mapping key (<area-rel> under the
		// session dir's attachments area); the COPY lands at the SAME relative
		// place under the successor's own area, so the original path structure
		// (and the [attachmentN:] order) is preserved. Relay through the ONE
		// helper so the prefix is stripped in exactly one place.
		areaRel, aerr := taskAttachmentsAreaRel(rel)
		if aerr != nil {
			lost = append(lost, taskSuccessorLostAttachment{a.OriginalAbsPath, aerr.Error()})
			continue
		}
		sha, size, cerr := taskAttachmentCopyTo(newID, filepath.Join(parentDir, filepath.FromSlash(rel)), areaRel)
		if cerr != nil {
			lost = append(lost, taskSuccessorLostAttachment{a.OriginalAbsPath, cerr.Error()})
			continue
		}
		a.ReplicaRelPath = filepath.ToSlash(filepath.Join(taskAttachmentsDirName, areaRel))
		a.SHA256 = sha
		a.FileSize = size
		a.SubmittedAt = time.Now().Unix()
		carried = append(carried, a)
	}
	d := taskDeliverableParse(deliverable)
	d.Attachments = nil
	if len(carried) > 0 {
		b, err := json.Marshal(carried)
		if err != nil {
			return "", "", fmt.Errorf("render the carried attachments manifest: %w", err)
		}
		d.Attachments = b
	}
	out := taskDeliverableRebuild(d)
	var line strings.Builder
	if len(carried) > 0 {
		fmt.Fprintf(&line, " %s%d (carried from %s)", TaskSuccessorAttachmentsCarriedMarker, len(carried), parentID)
	}
	for _, l := range lost {
		fmt.Fprintf(&line, "; %s %s (%s — recover it from parent %s)", TaskSuccessorAttachmentNotCarriedMarker, l.original, l.reason, parentID)
	}
	return out, line.String(), nil
}
