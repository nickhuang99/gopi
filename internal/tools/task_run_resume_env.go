package tools

import (
	"os"
	"strings"
)

// task_run_resume_env.go — THE DOOR'S RESUME DECLARATION (epic: land the resume
// witness on the RE-DISPATCH door).
//
// THE DEFECT THIS CLOSES. `gopi worker <task-id>` on a row that has already run
// re-enters the row: the executor child it spawns writes a SECOND `session_start`
// into the same record file, and that header said nothing about why there is
// more than one — the record of a re-dispatched row was indistinguishable from a
// corrupt one. The sibling witness exists and works on the RESUME face
// (cmd/agent/cli_resume.go's resumeIdentityOnRecord sets agent.RunIdentity's
// ResumedFrom), but the re-dispatch door passed `RunIdentity{}` — nothing to
// thread, so nothing was written.
//
// WHY THE DECLARATION COMES FROM THE DOOR AND NOT FROM THE WRITER. The raw-log
// side decides "this is a continuation" by measuring the FILE (`fi.Size() == 0`),
// and it then INVENTS the id it records (`ResumedFrom: runID`). The session
// record must not copy that shape: the raw log is lossy by design —
// `gopi session gc --trim-raw-log` deletes the middle of the file, resume
// headers included (measured: a record with three headers, two witnessed, whose
// raw log carries no resume header at all) — so a record that reverse-infers
// "resumed" from it loses the witness exactly when the file is trimmed. The DOOR
// is the only place that knows the fact without measuring anything: the wrapper
// is the process that decided to run a row which already has a run session.
//
// THE CHANNEL IS A SINGLE ENV VAR, because the door and the record writer are
// two PROCESSES (the wrapper spawns the executor child). Rule 21: the constant
// has exactly one reader, TaskRunResumeFrom, and exactly one writer, the spawn
// seam in cmd/agent/cli_tasks.go.
const TaskRunResumeFromEnv = "GOPI_TASK_RESUME_FROM"

// TaskRunResumeFrom reports the session a re-dispatched run continues, as
// declared by the door that spawned it. Empty means NO declaration — the
// ordinary first run of a row — so absence keeps its single meaning and the
// writer can write no witness rather than a guessed one (Rule 15a: a witness
// whose semantic value could be its zero value must not be omitted from the
// presence check, which is why this returns "" only for a genuinely absent or
// blank declaration).
func TaskRunResumeFrom() string {
	return strings.TrimSpace(os.Getenv(TaskRunResumeFromEnv))
}

// TaskRedispatchResumeFrom answers the DOOR's question: is the run I am about to
// spawn a RE-DISPATCH of a row that has already run, and if so which session is
// it continuing?
//
// THE PREDICATE IS "the row's own run directory ALREADY HOLDS A SESSION RECORD",
// and it is asked through the one resolver that owns it (TaskResumeSessionFor →
// taskRunSessionFromRowDir → sessionlayout.NewestRecordInDir), not by a second
// spelling here. A row whose first run has not yet written its record answers
// "" — that is what keeps the witness from appearing on an ordinary first run,
// where it would be a false claim.
//
// WHY NOT ASK THE LEDGER: a row is `claimed`/`in_progress` on BOTH its first and
// its second run, so the status cannot tell them apart, and the attempt-counter
// tags (run-retry:N) are written by the ABNORMAL-EXIT ladder, not by the door.
// The run directory is the one place a previous run is a fact rather than an
// inference.
func TaskRedispatchResumeFrom(taskID string) string {
	s := TaskResumeSessionFor(taskID)
	if s.Source != taskResumeSessionSourceRunDir {
		return "" // no record of this row yet: this is its FIRST run, not a re-dispatch
	}
	return strings.TrimSpace(s.SessionID)
}

// taskRunResumeWitness is THE ONE READER of the door's declaration for the
// NOTIFY layer (Rule 21: an env-name constant needs a real reader).
//
// WHY IT READS THE CHILD'S ENVIRONMENT AND DOES NOT ASK THE DOOR AGAIN. This
// function runs wherever the payload is built, and asking
// TaskRedispatchResumeFrom there would re-derive the fact from the row's run
// directory — and by the time a terminal write fires, that directory ALREADY
// holds this run's own record, so the predicate "a record exists" answers YES
// for a first run too: a false positive on every ordinary completion. The env
// var is the value the door decided BEFORE the spawn, frozen; it cannot drift
// as the run writes.
//
// WHY NO EXTRA "IS THIS A TASK RUN?" GATE: the declaration has exactly ONE
// writer (the spawn seam in cmd/agent/cli_tasks.go) and is therefore only ever
// present because a door put it there, so a non-empty value IS the claim. The
// hazard the gate would have covered — a bash tool inheriting the name and
// re-launching gopi that then announces somebody else's resume — is closed at
// the strip, one level up: TaskRunResumeFromEnv rides wrapperIdentityEnvVars
// (tools/childenv.go), so no shell gopi spawns can carry it at all. Gating on
// TaskWrapperEnv instead would have silently disabled the witness on the
// `gopi worker --resume` face, whose child (session resume) carries the
// declaration WITHOUT the wrapper id — a gate that costs a real witness to
// cover a hazard the strip already removed.
func taskRunResumeWitness() string {
	return TaskRunResumeFrom()
}

// RunResumeDeclaration is the ONE statement a spawn seam makes when the process
// it is about to start RE-ENTERS a row that has already run: `resumedFrom` is
// the session that run continues, and Env renders the single environment pair
// that carries it to the child (Rule 2/21: one writer per fact).
//
// WHY A TYPE AND NOT A PAIR OF STRINGS AT EACH CALL SITE. There are now TWO
// doors — `gopi worker`'s executor child (cmd/agent/cli_tasks.go) and the clone
// channel's closing/delivery round (cloneRoundChildEnv) — and both face the same
// question about the same row. Leaving each to spell
// `TaskRunResumeFromEnv+"="+tools.TaskRedispatchResumeFrom(id)` would make the
// declaration a thing two files build by hand, which is exactly how the second
// door came to make NO declaration at all: the first one's spelling was inline,
// so the second one had nothing to call. The declaration is a value here, and a
// seam that wants it can only get it from TaskRunResumeDeclare.
type RunResumeDeclaration struct {
	// ResumedFrom names the session the spawned run continues — the row's own
	// newest record, resolved through the one resolver (TaskResumeSessionFor).
	ResumedFrom string
}

// Env renders the declaration as the ONE environment entry the child reads
// (tools.TaskRunResumeFrom is its single reader). A zero declaration renders
// nothing: a seam that has nothing to say must add no name rather than an empty
// one, because absence is what keeps "this run started the session" a single
// meaning (Rule 15a).
func (d RunResumeDeclaration) Env() string {
	if d.ResumedFrom == "" {
		return ""
	}
	return TaskRunResumeFromEnv + "=" + d.ResumedFrom
}

// TaskRunResumeDeclare answers a SPAWN SEAM's question — "is the process I am
// about to start re-entering this row, and which session does it continue?" —
// and returns the declaration only when there is something to declare.
//
// It exists so the answer is DERIVED once and CONSUMED by every seam that
// spawns a run against a row (Rule 14): the re-dispatch door and the clone
// channel's delivery round both call this, so neither can be the one that
// forgot. The predicate itself still belongs to TaskRedispatchResumeFrom (one
// place decides what counts as a re-entry), and this function adds no second
// opinion — it packages that answer for the wire.
func TaskRunResumeDeclare(taskID string) (RunResumeDeclaration, bool) {
	resumedFrom := TaskRedispatchResumeFrom(taskID)
	if resumedFrom == "" {
		return RunResumeDeclaration{}, false
	}
	return RunResumeDeclaration{ResumedFrom: resumedFrom}, true
}
