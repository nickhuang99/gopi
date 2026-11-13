package agent

// RunIdentity is WHICH SESSION a run is — the three facts a resumed run must
// carry instead of minting its own (epic: `gopi chat --resume <id>` minted a
// second session).
//
// THE DEFECT THIS TYPE CLOSES (measured on master 3430292a). The resume arm of
// `gopi chat` loaded the old conversation into `sess.history`, read the old turn
// count as a DISPLAY baseline, and then called NewAgent like any other run:
// NewAgent minted a fresh uuid, which minted a fresh sessions/<uuid>/ directory,
// a fresh <uuid>.raw.log opened with O_EXCL and a fresh session.jsonl. The
// resumed session therefore never grew a byte — the conversation the operator
// asked to continue sat untouched in a directory nothing wrote to any more —
// and every `--resume` left one more stranded directory behind.
//
// WHAT A RUN SAYS INSTEAD. Three fields, each with its own consequence, and each
// empty meaning "the ordinary behaviour":
//
//	SessionID   the id to CONTINUE (no fresh uuid). Its consequences are the run
//	            directory, the JSONL name and the raw-log name — all three are
//	            derived from it, so reusing it is what keeps one session to one
//	            directory, however many runs it takes.
//	ResumedFrom the session this run continues, recorded in the appended
//	            session_start so the record says WHY there is more than one
//	            session_start in one file (a resumed file that cannot say so is
//	            indistinguishable from a corrupt one).
//	RawLogPath  the raw HTTP log to APPEND to, opened WITHOUT O_EXCL. Exclusive
//	            creation stays the rule for a run that mints its own log (see
//	            createRawLogFile: two runs in the same wall-clock second must not
//	            share a file); it is exactly wrong here, where sharing the file
//	            with the PREVIOUS run of the same session is the point.
//
// The zero value is a fresh run and is what every existing constructor passes,
// so no other face changes behaviour.
type RunIdentity struct {
	// SessionID is the session id to reuse. Empty ⇒ mint a new uuid (NewAgent's
	// historical behaviour).
	SessionID string
	// ResumedFrom names the session this run continues; written into the
	// appended session_start. Empty ⇒ this run starts a session.
	ResumedFrom string
	// RawLogPath is an existing raw log to append to. Empty ⇒ open a new one in
	// this run's session directory.
	RawLogPath string
	// SessionLogPath is an existing session RECORD (the JSONL) to append to —
	// the record sibling of RawLogPath, and it exists for the same reason.
	// Empty ⇒ derive the path from SessionID/task id as before.
	//
	// WHY THE ID ALONE IS NOT ENOUGH (measured 2026-10-28, row 01a17dd8). The
	// record's directory is NOT a function of the session id in every layout:
	// a task run keeps its record inside sessions/<task-id>/<run-uuid>.jsonl
	// while a bare session keeps sessions/<uuid>/session.jsonl, and the task
	// id that selects the first layout comes from GOPI_TASK_WRAPPER_ID — an
	// environment variable a resume CHILD does not carry. Reusing the session
	// id alone therefore re-derived the BARE layout and appended to
	// sessions/<uuid>/session.jsonl, i.e. it created a second run directory
	// beside the row's own: the caller's log shows the row's run directory
	// untouched while a brand-new uuid directory was written (the exact defect
	// row 01a17dd8 records). Naming the record FILE is how a resumed run
	// continues the file that exists instead of one its id can spell.
	SessionLogPath string
}

// resuming reports whether this identity continues an existing session record.
// It is the ONE predicate the constructors branch on, so "a run that reuses an
// id but opens a new log" cannot arise by two call sites disagreeing.
func (id RunIdentity) resuming() bool {
	return id.ResumedFrom != "" && id.SessionID != ""
}
