package tools

import (
	"os"
	"strings"
)

// The STATUS-EVENT record: who moved a row, from what state to what state, and
// on what grounds (epic 01a1a899-d05d).
//
// THE DEFECT THIS CLOSES, measured rather than supposed. `task_events.detail`
// used to carry the writeback summary on a run's own transition and the EMPTY
// STRING on every other transition (the else arm of the writer in
// taskUpdateStampedCore). Measured on row 01a1a194e1: two `failed` events, both
// with a blank detail, and the operator's question — "who turned this chat row
// into a failure, and on what grounds?" — had no answer ANYWHERE in this
// program. The `--reason` column did not help either: the transition writes it
// to tasks.reason, and an empty detail proves only "this was not a writeback".
//
// THE SHAPE, and the one rule that fixes it: every status event now records the
// three elements —
//
//	who=<identity> <from> → <to> why=<grounds>
//
// — and a writeback's digest rides BESIDE them as a MARKED trailer line rather
// than nested inside them, so the read side can keep `task show`'s `result:`
// showing exactly what the run recorded (taskEventLatestDetail strips the
// trailer; a record with no trailer is a transition, never a digest).
//
// HISTORY IS NOT REWRITTEN (append-only, Rule 35): the rows whose detail is
// already empty, and the rows this writer emitted as a bare `who=<unrecorded>`
// placeholder before a caller could name itself, are rendered on the read side
// as UNRECORDED. The alternative — "nobody wrote it here, so attribute it to the
// operator" — is the one answer this file must never give: an invented
// attribution is worse than a recorded absence.
//
// CONSTANTS (Rules 1b/13/14): every marker, label and fixed phrase below has
// ONE definition site, and the readers reference these names rather than
// re-typing the text.

const (
	// TaskStatusEventWhoLabel opens the record. A status event's detail that
	// STARTS with this marker is a transition record; that is the read side's
	// one discriminator (see taskStatusEventIsTransitionRecord), and it is
	// spelled here so the writer and every reader agree by construction.
	TaskStatusEventWhoLabel = "who="

	// TaskStatusEventMoveSeparator sits between the state the row left and the
	// state it arrived at. Same arrow the sync lane's detail uses
	// (syncStatusEventDetailFmt) so the two writers' trails read alike.
	TaskStatusEventMoveSeparator = " → "

	// TaskStatusEventWhySeparator opens the grounds.
	TaskStatusEventWhySeparator = " why="

	// TaskStatusEventFieldSeparator separates the who value from the move.
	TaskStatusEventFieldSeparator = " "

	// TaskStatusEventTrailerMarker is the LINE PREFIX of the writeback digest
	// that rides a status event. It is a whole line of its own rather than a
	// bracket on the body: the digest is multi-line prose and the marker is what
	// the read side strips (taskStatusEventStripTrailer).
	TaskStatusEventTrailerMarker = "[status-event] "

	// TaskStatusEventWhoRunPrefix names a transition written by a gopi AGENT
	// run: the suffix is that run's session id. This is the identity the ledger
	// was missing.
	TaskStatusEventWhoRunPrefix = "run:"

	// TaskStatusEventWhoAgentPrefix names a transition written by one gopi
	// agent session ON BEHALF of another row's run — reserved vocabulary for the
	// callers that can name both (the harness-side rows driven by a session that
	// is not the row's own). No writer in this build emits it yet; it exists so
	// the read side has a word for it the day one does, and so the three
	// identities stay distinguishable by name.
	TaskStatusEventWhoAgentPrefix = "agent:"

	// TaskStatusEventWhoOperator names a transition written by a gopi process
	// NOT inside an agent session — the operator's own terminal.
	TaskStatusEventWhoOperator = "operator"

	// TaskStatusEventWhoUnknown is the identity of a process this program cannot
	// name. It is NOT "operator": an unnameable caller must never be reported as
	// the operator (Rule 15 — silence about who is a fact, a guessed name is a
	// fiction).
	TaskStatusEventWhoUnknown = "unknown"

	// TaskStatusEventWhyUnrecorded is the grounds slot when the caller supplied
	// none. A real status CHANGE always carries one (taskDefaultReason), so this
	// renders only on a same-status re-write.
	TaskStatusEventWhyUnrecorded = "(not recorded)"

	// TaskStatusEventWhyNoneGiven is the why slot of a record WE wrote where the
	// caller supplied no grounds (a same-status re-write, which the state gate
	// does not give a default reason). It is distinct from
	// TaskStatusEventWhyUnrecorded on purpose: "(none given)" is a statement
	// about a write this program made and can vouch for; "(not recorded)" is a
	// statement about a historical row where the field did not exist at all. A
	// single word for both would let a reader mistake a known absence for an
	// unknown one.
	TaskStatusEventWhyNoneGiven = "(none given)"

	// TaskStatusEventPrevUnknown is the from-slot of a historical row: the state
	// it moved away from was never written down, and it cannot be reconstructed
	// without rewriting history.
	TaskStatusEventPrevUnknown = "unknown"

	// TaskStatusEventDetailFormatDate is the day the three-element record landed
	// (this epic). It is what the legacy render names, so a reader can tell
	// "nobody recorded who" from "this program did not record who yet".
	TaskStatusEventDetailFormatDate = "2026-11-05"

	// TaskStatusEventWhoUnrecorded is the WHOLE whose slot of a row from before
	// TaskStatusEventDetailFormatDate. It says two things at once — the fact is
	// absent, and it is absent because the format did not exist — and it
	// deliberately names NO executor.
	TaskStatusEventWhoUnrecorded = "(unrecorded, pre-" + TaskStatusEventDetailFormatDate + " format)"

	// TaskStatusEventWhoUnrecordedUnclassified is the who slot of a status event
	// that recorded SOMETHING, but not this regime's record — a pre-format run
	// digest, or the sync lane's own sentence. It says only what is true ("no
	// identity was recorded") and does not claim the pre-<date> shape.
	TaskStatusEventWhoUnrecordedUnclassified = "(unrecorded)"

	// TaskShowTransitionLabel is the `task show` label for a transition record.
	// It is its OWN face beside `result:`, `verdict:` and `delivery-owed:`: the
	// record is a fact about the ROW's lifecycle, and rendering it on `result:`
	// would displace the run digest the close-out chain and Rule 31's ran-line
	// check read from that line.
	TaskShowTransitionLabel = "transition:"
	// TaskShowTransitionNameLabel carries WHICH status the transition arrived at
	// (the event name IS the new status, by this writer's convention), so a
	// `where event = ...` query and the rendered line name the same thing — the
	// same contract TaskShowVerdictNameLabel and TaskShowDeliveryOwedNameLabel
	// have. It is its own constant rather than a reuse of the verdict's: two
	// faces sharing one label would make the rendered line ambiguous about which
	// face it belongs to. It stays SHORTER than taskShowLabelWidth (the same
	// alignment contract the other name labels obey: widening the shared width
	// to fit one new label would re-space every existing `task show` line).
	TaskShowTransitionNameLabel = "trans.event:"
)

// taskStatusEventDetailParts is the four elements one status event carries.
// A struct rather than four positional strings: the writer passes them by NAME,
// so a later element cannot land in the wrong slot silently.
type taskStatusEventDetailParts struct {
	// Who is the identity that performed the transition (taskStatusEventWho).
	Who string
	// From is the status the row was in (prevStatus, read under the state gate).
	From string
	// To is the status it moved to — the same value as the event name.
	To string
	// Why is the grounds: the caller's --reason, or the status-specific default
	// a real change always resolves.
	Why string
	// Digest is a writeback's run summary. Empty for a plain transition; when
	// present it rides the trailer line so `result:` can still render it.
	Digest string
}

// taskStatusEventWho resolves the identity writing a transition.
//
// THE DISCRIMINATOR IS THE AGENT-SESSION HANDLE, and it is the ONLY one that
// means "a gopi agent run": TaskExecutorSessionEnv is exported by cmd/agent at
// startup for every agent process and inherited by everything it spawns, so its
// presence says exactly "this writer is inside an agent session" — the fact the
// ledger needs. Its ABSENCE is not proof of a human terminal (a bare `gopi` in
// an operator's shell also lacks it), which is why it is still rendered as the
// operator identity: that IS the door such a process came through, and the
// three identities are told apart by the presence of the handle, not by a
// process-tree guess that would be wrong the moment a wrapper changes shape.
//
// An empty handle is not "the run that called us": an unattributable writer
// records TaskStatusEventWhoOperator, never a fabricated session id (Rule 15).
func taskStatusEventWho() string {
	if sid := strings.TrimSpace(os.Getenv(TaskExecutorSessionEnv)); sid != "" {
		return TaskStatusEventWhoRunPrefix + sid
	}
	return TaskStatusEventWhoOperator
}

// taskStatusEventDetailText renders one status event's detail.
//
// The BODY is always the three-element record; a digest, when present, is
// appended on its own marked line. Both halves exist so the two readers that had
// to share this column — the audit trail (this record) and `task show`'s
// `result:` line (the run's summary) — stop displacing each other.
func taskStatusEventDetailText(p taskStatusEventDetailParts) string {
	who := strings.TrimSpace(p.Who)
	if who == "" {
		who = TaskStatusEventWhoUnknown
	}
	why := strings.TrimSpace(p.Why)
	if why == "" {
		// NEVER an empty slot and never a filler string like "status changed"
		// (the epic's own constraint): the record states which kind of absence
		// this is.
		why = TaskStatusEventWhyNoneGiven
	}
	from := strings.TrimSpace(p.From)
	if from == "" {
		from = TaskStatusEventPrevUnknown
	}
	body := TaskStatusEventWhoLabel + who + TaskStatusEventFieldSeparator +
		from + TaskStatusEventMoveSeparator + strings.TrimSpace(p.To) +
		TaskStatusEventWhySeparator + why
	digest := strings.TrimSpace(p.Digest)
	if digest == "" {
		return body
	}
	return body + "\n" + TaskStatusEventTrailerMarker + digest
}

// taskStatusEventLegacyDetail renders the record for a row this program has
// nothing to say about: a historical event whose detail is empty, or a
// placeholder written before the caller could name itself.
//
// IT NAMES NO EXECUTOR, and that is the point. The `to` slot is real information
// (the event name IS the new status); the `from` slot and the grounds were never
// written down, and the who slot says so in its own words. A reader must be able
// to answer "unrecorded" — never "the operator did it", which would be this
// program inventing a culprit for a row it never watched.
func taskStatusEventLegacyDetail(to string) string {
	return taskStatusEventLegacyDetailWithWho(to,
		TaskStatusEventWhoUnrecorded, TaskStatusEventWhyUnrecorded)
}

// taskStatusEventUnclassifiedDetail renders a status event that recorded a
// sentence this regime does not recognise — a pre-format run digest, or the sync
// lane's own move line (syncStatusEventDetailFmt). The recorded sentence IS
// carried, in the why slot, because it is the only ground the row has; the who
// slot states plainly that no identity was recorded, and it deliberately does
// NOT borrow the pre-<date> wording: a sync move written TODAY would then be
// mis-dated, and a reader acting on that would hunt for a ledger boundary that
// is not there.
func taskStatusEventUnclassifiedDetail(to, recorded string) string {
	return taskStatusEventLegacyDetailWithWho(to,
		TaskStatusEventWhoUnrecordedUnclassified, strings.TrimSpace(recorded))
}

// taskStatusEventLegacyDetailWithWho is the ONE renderer both legacy shapes
// share, so the arrow, the from-slot and the labels cannot drift between them.
func taskStatusEventLegacyDetailWithWho(to, who, why string) string {
	return TaskStatusEventWhoLabel + who +
		TaskStatusEventFieldSeparator + TaskStatusEventPrevUnknown +
		TaskStatusEventMoveSeparator + strings.TrimSpace(to) +
		TaskStatusEventWhySeparator + why
}

// taskStatusEventIsTransitionDetail reports whether a status event's detail is a
// TRANSITION record at all — one discriminator, ONE half: the detail OPENS with
// the who label, which a run digest never does (a digest is a run summary
// rendered as prose). Both shapes count: a plain move (record only) and a
// writeback (record + digest trailer).
//
// A row written before this format existed has an empty detail and is NOT a
// record; its renderer is the caller's choice between "no record" and
// taskStatusEventLegacyDetail.
func taskStatusEventIsTransitionDetail(detail string) bool {
	return strings.HasPrefix(detail, TaskStatusEventWhoLabel)
}

// taskStatusEventIsTransitionOnly reports whether a detail is a record WITHOUT a
// digest — a LIFECYCLE fact and nothing else, so no `result:` line may render it.
func taskStatusEventIsTransitionOnly(detail string) bool {
	return taskStatusEventIsTransitionDetail(detail) &&
		!strings.Contains(detail, TaskStatusEventTrailerMarker)
}

// taskStatusEventRecordBody returns the record's own line — the three elements —
// dropping the digest trailer a writeback's record carries. The trailer belongs
// to `result:`; the lifecycle face renders the elements.
func taskStatusEventRecordBody(detail string) string {
	if idx := strings.Index(detail, "\n"+TaskStatusEventTrailerMarker); idx >= 0 {
		return detail[:idx]
	}
	return detail
}

// taskStatusEventStripTrailer returns the writeback digest a status event
// carries, or the detail unchanged when it carries none.
//
// It is the read-side counterweight to taskStatusEventDetailText: `result:` must
// keep showing the run's summary, so the three-element record that now precedes
// it is removed rather than rendered as part of the digest.
func taskStatusEventStripTrailer(detail string) string {
	idx := strings.Index(detail, TaskStatusEventTrailerMarker)
	if idx < 0 {
		return detail
	}
	return strings.TrimSpace(detail[idx+len(TaskStatusEventTrailerMarker):])
}

// taskTransitionDetailRenderLimit bounds the transition record quoted on `task
// show`. The record is three short elements; the bound exists because the WHY
// slot can carry an operator's whole sentence, and `show` renders one line per
// face. The FULL detail is one query away and nothing is truncated in the
// ledger (Rule 38's split: a rendered figure is not the record).
const taskTransitionDetailRenderLimit = 400
