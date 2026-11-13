package tools

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// task_resume_new_session.go — the TRAIL of the ONE downgrade a resume allows:
// the operator explicitly asked for a FRESH session because the target held no
// session record (config.ResumeAllowNewSessionEnv).
//
// WHY THE TRAIL AND NOT ONLY STDERR (epic 01a17df9-99c4). The warning is a
// visible line on stderr for the operator standing there — and stderr belongs to
// a process that has exited by the time anyone reads the row. The row is what a
// reviewer, a second dispatcher and the ledger's own history read, so a run that
// did NOT continue the session behind the row must say so ON the row: otherwise
// `resumed:<session>` (written later by TaskTagResumed) is the only record, and
// it names the NEW session — the lineage gap the epic exists to close, restored
// in a different column.
//
// Membership test (verdictTrailEventNames): this is a JUDGMENT ON the row — the
// run measured that the named session could not be continued and proceeded on
// the operator's explicit word — so `task show` must render it.
const (
	// ResumeNewSessionEvent is the event name appended to a resumed row's trail
	// when the run starts a NEW session instead of continuing the row's own.
	ResumeNewSessionEvent = "resume-new-session"

	// resumeNewSessionRecordLimit bounds the identical-detail dedup: a wrapper
	// that retries the same resume three times must not drip three copies of one
	// sentence. It matches the other one-line close-out records (Rule 1: the
	// number is named, not typed at the call site).
	resumeNewSessionRecordLimit = 1

	// resumeNewSessionTimeout bounds the ledger write. A trail entry is a
	// courtesy to a later reader; it must never hold up the run it describes.
	resumeNewSessionTimeout = 5 * time.Second
)

// TaskResumeNewSessionNote appends ONE attributed trail entry recording that this
// resume started a fresh session because its target held no record. An EMPTY
// taskID is a no-op with a nil error: a session-only resume (no --task, no row
// resolved from the target) has no trail to write into, and that is the normal
// case rather than a failure — the stderr warning still names what happened.
//
// A ledger failure is RETURNED, not swallowed (Rule 15b): the caller decides
// whether to report it, and a caller that wants the run to continue can say so on
// its own line. Nothing here can undo the run.
func TaskResumeNewSessionNote(taskID, detail string) error {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return nil
	}
	note := strings.TrimSpace(detail)
	if note == "" {
		return fmt.Errorf("task: resume-new-session note for %s: the detail must say WHICH session was not continued", taskID)
	}
	ctx, cancel := context.WithTimeout(context.Background(), resumeNewSessionTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return fmt.Errorf("task: resume-new-session note for %s: open ledger: %w", taskID, err)
	}
	defer db.Close()
	if taskEventCountForDetailOn(ctx, db, taskID, ResumeNewSessionEvent, note) >= resumeNewSessionRecordLimit {
		return nil
	}
	taskEventWrite(ctx, db, taskID, ResumeNewSessionEvent, note)
	return nil
}

// TaskResumeNewSessionNoteText renders the sentence the trail carries, so the
// writer, the row and any test share ONE spelling of it (Rule 14: the reader and
// the writer parse the same text).
func TaskResumeNewSessionNoteText(target, runDir, switchName string) string {
	return fmt.Sprintf("resume of %s started a NEW session instead of continuing it: %s holds no session record and the operator opted in with %s=1 — the session behind the target does not grow",
		target, runDir, switchName)
}

// ResumeRecordRebuiltEvent is the SECOND, milder member: the run kept the
// checkpoint's own id and directory and rebuilt the missing record from it, so
// the session DID continue — but a record was written by a resume rather than by
// the run that created it, and a reader of the row is entitled to know that
// without having been standing at the terminal when the notice was printed.
const ResumeRecordRebuiltEvent = "resume-record-rebuilt"

// TaskResumeRecordRebuiltNote appends that entry (same dedup, same timeout, same
// empty-taskID no-op as TaskResumeNewSessionNote).
func TaskResumeRecordRebuiltNote(taskID, detail string) error {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return nil
	}
	note := strings.TrimSpace(detail)
	if note == "" {
		return fmt.Errorf("task: resume-record-rebuilt note for %s: the detail must say WHICH directory was rebuilt", taskID)
	}
	ctx, cancel := context.WithTimeout(context.Background(), resumeNewSessionTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return fmt.Errorf("task: resume-record-rebuilt note for %s: open ledger: %w", taskID, err)
	}
	defer db.Close()
	if taskEventCountForDetailOn(ctx, db, taskID, ResumeRecordRebuiltEvent, note) >= resumeNewSessionRecordLimit {
		return nil
	}
	taskEventWrite(ctx, db, taskID, ResumeRecordRebuiltEvent, note)
	return nil
}

// TaskResumeRecordRebuiltNoteText is its one spelling.
func TaskResumeRecordRebuiltNoteText(target, dir string) string {
	return fmt.Sprintf("resume of %s rebuilt the session RECORD from the checkpoint under the same id and directory (%s): the conversation continued, but the record was written by this resume",
		target, dir)
}
