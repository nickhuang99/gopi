package tools

// tasksessionfacts.go — THE SESSION LAYER PRINTED BESIDE THE LEDGER WORD.
//
// THE DEFECT THIS CLOSES. Two layers record facts about one run and the read
// side never printed them side by side:
//
//   - the LEDGER records `status`, which is a PURE DECLARATION — a row says
//     `failed` because something wrote that word, not because anything was
//     measured;
//   - the SESSION LAYER records what actually happened — `rundir.Record`
//     (a `session_end` verdict, the writer's pid stamp), the raw log's request
//     count, and a `session.dump.json` crash category when the process died
//     without writing its verdict.
//
// So an `[auto]` row that never got as far as its first model call rendered as
// exactly one word, `status: failed`, and a reader could not tell it from a run
// that executed for twenty minutes and then failed (measured 2026-11-01 on
// 01a194e1-5e79-7ad5-a8b3-bde0a9236b0e: `session.jsonl` holds ONE line, a
// `session_start`; the raw log holds ONE line, its creation header; no request
// was ever logged; and `session.dump.json` says `orphan-no-session-end`).
// "Never started" and "ran and failed" are different facts with different
// remedies, and the read side printed neither.
//
// WHAT THIS FILE IS — A READER, NOT A SECOND DERIVATION. Every value is taken
// from the mechanism that already owns it:
//
//	state    → sessionRunState (internal/tools/sessionruns.go — the ONE
//	           derivation the table, the JSON face and `session show` read,
//	           Rule 13/14: this file must not re-decide running/ended/stopped)
//	crash    → the `category` field of <session dir>/session.dump.json, read as
//	           JSON. NOT through internal/agent: it writes that document
//	           (crashdump.go) and imports this package, so importing it back
//	           would be a cycle. The file is the interface.
//	turns    → rundir.Record.Turns (session_end's total_iterations, or the last
//	           request event's iteration for a run with no verdict)
//	requests → checkpoint.CountRequests (bounded head sample; the count is a
//	           LOWER BOUND when it reports truncation, and the line says so)
//
// NOTHING HERE INVENTS A STATE VALUE, A TABLE OR AN EVENT (Rule 35): a
// `never-started` row is a DERIVED READING of the four facts above — not a
// fifth state — and the ledger is never written to. The nine-status contract is
// untouched: `status:` still prints the row's own word, unchanged and unmerged.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nickhuang99/gopi/internal/checkpoint"
	"github.com/nickhuang99/gopi/internal/procstat"
	"github.com/nickhuang99/gopi/internal/rundir"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
)

// The `session:` line's vocabulary. Every word a reader greps for lives here
// once (Rule 1/1b: a label or a keyword is never typed inline at its use site).
const (
	// TaskShowSessionLabel is the label of the line this file renders. It sits
	// directly under `status:` because that is the comparison a reader needs:
	// the ledger's declaration, then what the session layer measured.
	TaskShowSessionLabel = "session:"

	// The `key=value` words of the line's value.
	taskSessionStateKey    = "state="
	taskSessionCrashKey    = "crash="
	taskSessionTurnsKey    = "turns="
	taskSessionRequestsKey = "requests="
	taskSessionFieldSep    = "  "

	// TaskSessionNeverStarted is the DERIVED word for the shape this epic
	// exists for: the session layer holds no request, no turn, and a writer
	// that is gone without a verdict. It is a reading, never a stored value.
	TaskSessionNeverStarted = "never-started"

	// taskSessionUnrecorded is what a count renders when the evidence for it
	// does not exist. It is a WORD rather than a 0 because a 0 is a
	// measurement and this is the absence of one (Rule 15).
	taskSessionUnrecorded = "unrecorded"

	// taskSessionRequestsLowerBound marks a request count that came from a
	// bounded head sample of a large raw log: the number is real but it is a
	// floor, and a reader who needs the exact total must be told so.
	taskSessionRequestsLowerBound = "+"

	// taskSessionCrashNone is what `crash=` renders when the session left no
	// crash document — the ordinary case for a healthy run, and for a run
	// killed before it could write one.
	taskSessionCrashNone = "none"

	// The crash document's two spellings this reader must agree with its
	// WRITER about (Rule 1/14: a reader and a writer may never carry two
	// spellings of the same value). The file name comes from
	// internal/sessionlayout, which owns the artifact taxonomy; the field name
	// is pinned here and asserted against a real document by the tests.
	taskSessionCrashFieldCategory = "category"
)

// TaskSessionFacts is the session layer's measurement of one row, rendered
// beside the ledger's `status:` word. Every field says whether it is a
// measurement or an absence — never a zero that could be mistaken for one.
type TaskSessionFacts struct {
	// SessionID is the session the facts were read from.
	SessionID string
	// Resolved reports whether ANY session-layer record answered. False means
	// every field below is empty because nothing was found, and the renderer
	// states that rather than printing a block of zeros.
	Resolved bool
	// State is sessionRunState's word (running/ended/stopped/unstamped/
	// live-unknown) — the same value `gopi session show` prints.
	State string
	// CrashCategory is the `category` field of the session's crash document
	// ("" when there is none — see HasCrash).
	CrashCategory string
	HasCrash      bool
	// Turns is rundir's loop-pass count. HasEnd says whether it is a finished
	// run's TOTAL (from session_end) or the progress a still-open log shows.
	Turns int
	// Requests is the raw log's request count, RequestsKnown whether a raw log
	// existed to count at all, and RequestsTruncated whether the count is a
	// lower bound (a head sample of a large log).
	Requests          int
	RequestsKnown     bool
	RequestsTruncated bool
	// NeverStarted is the derived reading: a stopped session that never logged
	// a request and never accumulated a turn.
	NeverStarted bool
	// Absence says WHY nothing could be measured, when Resolved is false.
	Absence string
}

// TaskSessionFactsFor resolves one ledger row's session-layer facts. taskID and
// sessionID are the two addresses the row carries; either may be empty and the
// other still answers.
//
// THE RESOLUTION ORDER IS THE RUN'S OWN, NOT THE COLUMN'S: the row's run
// directory is asked first (it holds the run that actually EXECUTED the row),
// and the `session_id` column is the fallback — it is documented as "the
// session that submitted or executed", and for an `[auto]` row it names a
// PARENT session whose directory exists while the row-keyed one never will.
// Both roads are rundir's; this function only picks which to walk.
func TaskSessionFactsFor(taskID, sessionID string) TaskSessionFacts {
	f := TaskSessionFacts{SessionID: strings.TrimSpace(sessionID)}
	rec, ok := taskSessionRecord(taskID, f.SessionID)
	if !ok {
		f.Absence = taskSessionAbsence(taskID, f.SessionID)
		return f
	}
	f.Resolved = true
	if id := sessionRecordID(rec); id != "" {
		f.SessionID = id
	}
	// THE STATE IS sessionRunState's ANSWER, NEVER A SECOND DECISION (Rule
	// 13/14). The liveness probe is the same one `session show` runs, against
	// the LAST segment's stamp (a resumed log carries one per run).
	f.State = sessionRunState(rec, procstat.Alive(rec.LastWriterPID()))
	f.Turns = rec.Turns
	f.CrashCategory, f.HasCrash = sessionCrashCategory(rec.Dir)
	// The raw log is the only place a request count lives; rundir resolves
	// which file that is (a re-home sweep moves it, the record may name it
	// verbatim, or it may be absent altogether).
	if raw := rundir.ExistingRawLog(rec); raw != "" {
		if n, truncated, err := checkpoint.CountRequests(raw); err == nil {
			f.Requests, f.RequestsTruncated, f.RequestsKnown = n, truncated, true
		}
	}
	f.NeverStarted = sessionNeverStarted(f)
	return f
}

// taskSessionRecord finds the run record the facts are read from: the row's own
// run directory first, then the session_id-keyed lookup. Both go through
// rundir's resolvers — this is a choice of ADDRESS, not a second parse.
func taskSessionRecord(taskID, sessionID string) (rundir.Record, bool) {
	if recs := taskRuns(taskID); len(recs) > 0 {
		return rundir.Latest(recs)
	}
	if strings.TrimSpace(sessionID) == "" {
		return rundir.Record{}, false
	}
	return sessionRunByID(sessionID, true)
}

// taskSessionAbsence names what WAS searched when nothing answered — the
// difference between "this row has no session" and "nothing looked" (Rule 15).
func taskSessionAbsence(taskID, sessionID string) string {
	dir := taskRunDir(taskID)
	switch {
	case sessionID == "" && (dir == "" || !rundir.Exists(dir)):
		return "no run log and no session id recorded for this row"
	case sessionID == "":
		return "run directory holds no run log and no session id is recorded for this row"
	default:
		return "no run log under " + dir + " and none under the session id " + sessionID
	}
}

// sessionNeverStarted is the ONE definition of the derived word (Rule 13): a
// session counts as never-started only when EVERY one of these is a
// measurement, never when one of them is missing:
//
//   - the state is `stopped` — the writer is gone and left no `session_end`.
//     An `unstamped` record is NOT enough (the pid stamp is missing evidence,
//     and a run whose start line could not be read must not be accused of
//     never having run, Rule 15); `ended` / `running` are other facts;
//   - the log accumulated no turn;
//   - a raw log EXISTS and logged no request (a missing raw log is an absence
//     of evidence, so `requests` unknown never yields the claim).
func sessionNeverStarted(f TaskSessionFacts) bool {
	return f.State == SessionRunStateStopped &&
		f.Turns == 0 &&
		f.RequestsKnown && f.Requests == 0
}

// sessionCrashCategory reads the `category` of a session directory's crash
// document, and reports whether the document is there at all.
//
// IT IS READ AS JSON, NOT THROUGH internal/agent. That package WRITES this file
// (crashdump.go) and imports this one, so the document is the only interface
// available here — and it is enough: the writer's own field name is the
// contract, and the test asserts it against a document shaped like the real one
// on this host.
func sessionCrashCategory(dir string) (string, bool) {
	if strings.TrimSpace(dir) == "" {
		return "", false
	}
	raw, err := os.ReadFile(filepath.Join(dir, sessionlayout.ShapeSessionCrashDump))
	if err != nil {
		return "", false
	}
	var doc struct {
		Category string `json:"category"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		// A dump that cannot be decoded is still a dump: say the file is there
		// and that its category is unreadable, rather than reporting "no crash"
		// about a death that left evidence (Rule 15).
		return taskSessionUnrecorded, true
	}
	return strings.TrimSpace(doc.Category), true
}

// taskSessionFactsLine renders the `task show` line for one row, always — an
// absent session layer is STATED, never a line that quietly disappears, so a
// reader can tell "this row has no session" from "this view did not look".
func taskSessionFactsLine(taskID, sessionID string) string {
	return taskShowField(TaskShowSessionLabel, taskSessionFactsValue(TaskSessionFactsFor(taskID, sessionID)))
}

// taskSessionFactsValue renders one fact set as the line's value:
//
//	<session-id>  state=stopped  crash=orphan-no-session-end  turns=0  requests=0  never-started
func taskSessionFactsValue(f TaskSessionFacts) string {
	if !f.Resolved {
		id := f.SessionID
		if id == "" {
			id = taskSessionNoID
		}
		return id + taskSessionFieldSep + f.Absence
	}
	parts := []string{f.SessionID, taskSessionStateKey + f.State}
	crash := taskSessionCrashNone
	if f.HasCrash {
		crash = f.CrashCategory
		if crash == "" {
			crash = taskSessionUnrecorded
		}
	}
	parts = append(parts, taskSessionCrashKey+crash)
	parts = append(parts, taskSessionTurnsKey+strconv.Itoa(f.Turns))
	if f.RequestsKnown {
		req := strconv.Itoa(f.Requests)
		if f.RequestsTruncated {
			req += taskSessionRequestsLowerBound
		}
		parts = append(parts, taskSessionRequestsKey+req)
	} else {
		parts = append(parts, taskSessionRequestsKey+taskSessionUnrecorded)
	}
	if f.NeverStarted {
		parts = append(parts, TaskSessionNeverStarted)
	}
	return strings.Join(parts, taskSessionFieldSep)
}

// taskSessionNoID stands in for the session id of a row that recorded none —
// the line still renders, and it says which fact is missing.
const taskSessionNoID = "(no session id)"
