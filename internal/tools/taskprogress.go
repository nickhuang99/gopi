package tools

// THE PROGRESS AXIS — "is this row RUNNING, or merely OCCUPIED BY A PROCESS?"
// (bug epic 01a1ac0f, 2026-11-05).
//
// THE DEFECT THIS ANSWERS. Every holder judgement in this tree asks one
// question — is the pid in /proc? — and that question CANNOT tell these two
// apart. Both were measured on this host on the same evening, minutes apart,
// with the SAME refusal sentence:
//
//	(a) 22:12 — holder pid 518745 had been up 5h18m and used 24 s of CPU
//	    (0.1%), all 36 threads parked in futex, and its session transcript had
//	    not been written since 16:57:18. It was making NO progress; refusing a
//	    resume was WRONG.
//	(b) 22:17 — holder pid 2680836 had been up 2m25s and was working. Refusing
//	    was RIGHT.
//
// So the honest statement of the defect is not "the system cannot judge death"
// — `kill -9` produced `verdict: dead / resumable: yes` immediately, because
// the pid left /proc. It is that "the pid exists" was read as "the run is
// working". A pid answers EXISTENCE; progress is a different measurement.
//
// WHAT EVIDENCE IS ADMISSIBLE (Rule 35: no new resident mechanism, no new
// config key, no new state — the answer must come from artifacts the run
// already produces):
//
//  1. the run's OWN TRANSCRIPT — <sessions>/<dir>/session.jsonl, the file the
//     agent appends a record to on every turn. Its last write is a DIRECT
//     measurement of when this run last did anything at all, and it survives
//     the process (a killed run leaves its transcript behind, which is what
//     makes it readable for a holder that is already gone).
//  2. its RUN LOG (the .raw.log beside it) — the same reading from the second
//     file the run writes, taken so a run whose JSONL is momentarily behind
//     its raw log is not accused of sleeping.
//  3. its CPU time, read through internal/procname's CPUTicks — the ONE reader
//     of the utime/stime columns (Rules 13/14). It never decides on its own; it
//     CORROBORATES, and it is printed, because "used 24 s over 5h18m" is what
//     makes a wedged run obvious to a human at a glance.
//
// NOTHING HERE IS A SECOND LIVENESS READER. Process existence stays where it
// is (pidAlive → PidAlive → internal/procstat); this file only asks how long
// the run has been SILENT, which is a question about files, not about /proc.
//
// THE THREE STATES ARE THE POINT (Rule 15b: no fourth word, and "not measured"
// is never spelled as an answer):
//
//	running      — the transcript advanced within taskProgressIdleAfter.
//	idle         — it has not, and the silence is named in duration.
//	unmeasurable — there is no transcript/log to read, or the pid cannot be
//	               read. The absence is STATED, never returned as `running`.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/procname"
	"github.com/nick/gopi/internal/rundir"
	"github.com/nick/gopi/internal/runlock"
)

// The progress vocabulary. The words are the vocabulary — a renderer must not
// invent a fourth, and the refusal text branches on these values alone.
const (
	// TaskProgressRunning: the run's own transcript advanced recently, so a
	// process holding this row is DOING something.
	TaskProgressRunning = "running"
	// TaskProgressIdle: the row has a live holder but its transcript has not
	// advanced for at least taskProgressIdleAfter. This is the measured shape
	// the epic calls "卡死": occupied, and making no progress.
	TaskProgressIdle = "idle"
	// TaskProgressUnmeasurable: the axis could not be measured (no transcript
	// or log to read, unreadable pid). Never an alias for running or idle.
	TaskProgressUnmeasurable = "unmeasurable"
)

// taskProgressIdleAfter is how long a run's transcript may stay untouched
// before the row is called `idle` (Rule 1: one named threshold, not a literal
// at the comparison).
//
// WHY 10 MINUTES. It must exceed the LONGEST ordinary silence inside a healthy
// run — one model call plus one tool call — with room to spare, because a false
// `idle` invites an operator to start a SECOND run on a live one. This host's
// measured single-turn latencies are seconds to a couple of minutes (the 22:17
// holder above was busy at 2m25s of wall clock), so ten minutes is roughly a
// 5x margin. It errs toward "running": the cost of mislabelling a working run
// as idle (a double-run) is far higher than the cost of waiting ten minutes
// before calling a wedged run idle.
const taskProgressIdleAfter = 10 * time.Minute

// The render vocabulary of the axis (Rule 1b: the label constants live beside
// the report that prints them, so a script greps one spelling).
const (
	taskStatusProgressLabel  = "progress:    "
	taskStatusRunLockLabel   = "run_lock:    "
	taskStatusLockStampLabel = "lock_stamp:  "
	// taskStatusLockNoteLabel prefixes the RECONCILIATION line (taskLockScopeNote),
	// printed only when the two axes above could be read as pulling apart.
	taskStatusLockNoteLabel = "lock_note:   "

	// taskLockStampDiagnosis is the sentence that keeps a reader from mistaking
	// the stamp for the verdict — the exact confusion this epic was filed for.
	// It is printed on the stamp's own line, after the values, because the
	// reader who has just seen a pid is the reader who needs it.
	//
	// IT NO LONGER SAYS "the kernel flock above is the verdict", AND THAT IS A
	// CORRECTION, NOT A TASTE CHANGE (bug epic 01a1b5f2, measured 2026-11-07).
	// The old sentence asserted a data flow this tree does not have:
	//
	//	$ grep -n "RunLockState" internal/tools/*.go | grep -v _test
	//	taskproc.go:905:  st.RunLockState = live.State      <- ASSIGNED
	//	taskproc.go:1858: if st.RunLockState != "" {        <- RENDERED
	//	taskproc.go:1861,1863: ...                          <- RENDERED
	//
	// — one assignment and the render site, nothing else. `verdict:` is computed
	// by taskHolderVerdictFor from the RECORDED PID's identity and never reads
	// this struct's lock fields at all, so calling the flock "the verdict" was
	// false about the very line printed above it. The two axes answer DIFFERENT
	// questions about DIFFERENT subjects (a PROCESS vs a RUN DIRECTORY), and
	// taskLockScopeNote is what states that where a reader needs it.
	taskLockStampDiagnosis = "the PREVIOUS holder's diagnosis only — NOT a verdict (the file is never deleted, so a stamp outlives its writer)"
	// taskLockStampNone is what an empty stamp says (Rule 15: an absence is
	// stated, never rendered as an empty value).
	taskLockStampNone = "(no stamp written — the file exists but no holder ever recorded itself)"

	// taskLockNoteRunningButFreeFmt is the RECONCILIATION for the shape this
	// epic was filed on: `verdict: running` beside `run_lock: free`. The two
	// lines are not in conflict, and the reader's one decision ("may I touch
	// this row now?") is answered in the last clause — but only after the
	// note says WHY the word "free" was never about the row.
	//
	// It is ONE string with two verbs because the two halves are useless apart:
	// naming the subjects without the per-round fact leaves a reader asking
	// "then when IS it held", and the per-round fact without the subjects
	// leaves "free" reading as "nobody is executing this row" — the exact
	// misreading. %d is the pid the verdict was measured against.
	taskLockNoteRunningButFreeFmt = "`free` is NOT a contradiction of `running` — the two lines have different SUBJECTS: " +
		"`verdict:` measures the PROCESS (pid %d is the row's executor), while `run_lock:` measures this RUN DIRECTORY's flock, " +
		"and that flock is taken PER ROUND and released at every round end (cmd/agent/cli_tasks_worker.go: `defer relRunLock()`), " +
		"so a live executor between two rounds legitimately holds nothing. DECIDE ON `verdict:` — this row IS being executed; " +
		"do not start a second one."

	// taskLockNoteFreeButVerdictHeldFmt is the MIRROR shape, and it is the
	// dangerous one: a flock that is HELD while the verdict is anything but
	// `running`. The flock cannot outlive its holder, so something IS executing
	// this run directory at this instant even though the row's own recorded
	// executor could not be confirmed as that process. %d is the flock's
	// holder, %q the verdict.
	taskLockNoteFreeButVerdictHeldFmt = "`run_lock:` reports a live flock held by pid %d while `verdict:` is %q — " +
		"the flock cannot outlive its holder, so SOMETHING is executing this run directory right now even though the row's " +
		"recorded executor could not be confirmed as that process. Treat the row as OCCUPIED until pid %d is identified " +
		"(a worker between rounds holds this lock for another row only in the inverse case; here the directory itself is held)."
)

// taskLockScopeNote is the reconciliation between the `verdict:` line and the
// `run_lock:` line, rendered only where the two could be read as pulling apart
// (bug epic 01a1b5f2).
//
// WHY IT EXISTS AT ALL. Both lines were already TRUE and measured; what was
// missing was ONE sentence saying they answer different questions about
// different subjects. A report that prints two true measurements with no
// arbitration is not neutral — it hands the reader a contradiction to resolve,
// and the reader's decision ("may I touch this row now?") is exactly what the
// report exists to make. So the reconciliation is derived HERE, in the same
// package that owns the vocabulary, rather than left to prose in a doc.
//
// IT IS NOT A FOURTH LOCK STATE. The vocabulary stays runlock's three words;
// this function only annotates pairs of them. A case it does not recognise
// prints nothing rather than an invented explanation (Rule 15b: an unknown
// shape must not be narrated as a known one).
func taskLockScopeNote(st *TaskProcessStatus) string {
	if st == nil || st.RunLockState == "" {
		return ""
	}
	running := st.Verdict == TaskProcessStateRunning
	switch {
	case running && st.RunLockState == runlock.LockStateFree:
		// The measured shape of this epic. The subject of the sentence is the
		// pid the verdict was measured against; with no subject (a verdict that
		// rests on no measurable pid) it names the holder instead of printing a
		// meaningless 0.
		pid := st.JudgedPid
		if pid <= 0 {
			pid = st.HolderPid
		}
		if pid <= 0 {
			return ""
		}
		return fmt.Sprintf(taskLockNoteRunningButFreeFmt, pid)
	case !running && st.RunLockState == runlock.LockStateHeld:
		return fmt.Sprintf(taskLockNoteFreeButVerdictHeldFmt, st.RunLockPid, st.Verdict, st.RunLockPid)
	}
	return ""
}

// taskProgressIdleText is the tail of an idle verdict, and it carries the one
// thing an operator acting on it needs: the exact command that recovers the row.
// The flag is built from the ONE spelling constant (Rule 1b/13) rather than
// typed here, so this sentence can never name a flag the parser stopped
// accepting.
const taskProgressIdleText = "no transcript advance for %s — the holder is OCCUPIED BUT IDLE (not working). Recover it with: gopi worker %s --" + TakeOverFlagSpelling

// TaskProgress is the axis' answer, carried as its own fields (never folded
// into Verdict): a row's holder can be alive AND idle, and collapsing the two
// questions is the defect this axis exists to fix.
type TaskProgress struct {
	// State is one of TaskProgressRunning / TaskProgressIdle /
	// TaskProgressUnmeasurable.
	State string `json:"state"`
	// Why is the evidence sentence — what was read, and what it said.
	Why string `json:"why"`
	// IdleSeconds is how long ago the transcript last advanced (0 when the
	// axis could not measure it: a duration that was never read must not be
	// rendered as "instant").
	IdleSeconds float64 `json:"idle_seconds,omitempty"`
	// Evidence is the file the reading came from ("" when none was found).
	Evidence string `json:"evidence,omitempty"`
	// LastAdvance is that file's mtime in unix seconds (0 when unmeasured).
	LastAdvance float64 `json:"last_advance,omitempty"`
	// CPUSeconds / CPUKnown are the holder's own CPU time — corroboration,
	// printed, never the decision (see the file comment).
	CPUSeconds float64 `json:"cpu_seconds,omitempty"`
	CPUKnown   bool    `json:"cpu_known"`
	// ElapsedSeconds is how long this row has been in_progress (0 when the row
	// records no in_progress_at referent). It is what turns the CPU number into
	// a ratio a human can judge.
	ElapsedSeconds float64 `json:"elapsed_seconds,omitempty"`
}

// Idle reports whether the row is occupied but making no progress.
func (p TaskProgress) Idle() bool { return p.State == TaskProgressIdle }

// taskProgressAxisFor measures the progress axis for a row whose holder is pid,
// at instant now. It is the ONE function (the epic's "单一函数"): the status
// view, the worker's refusal and the resume's refusal all read its answer, so
// the three surfaces cannot disagree about whether a row is working.
//
// A pid <= 0 (no holder address at all) is UNMEASURABLE, not idle: nothing was
// judged, and saying "idle" there would be a claim about a process that was
// never named.
func taskProgressAxisFor(t *Task, pid int, now time.Time) TaskProgress {
	p := TaskProgress{State: TaskProgressUnmeasurable}
	if t == nil {
		p.Why = "no row was supplied, so there is no run to judge"
		return p
	}
	if t.HasInProgressAt && t.InProgressAt > 0 {
		p.ElapsedSeconds = now.Sub(time.Unix(int64(t.InProgressAt), 0)).Seconds()
	}
	if pid <= 0 {
		// NOTHING WAS JUDGED. The axis asks what the process HOLDING this row is
		// doing; with no holder there is no subject, and returning `idle` here
		// would be a claim about a process that was never named — the exact
		// fabricated-verdict shape Rule 15b forbids. A caller with no pid has the
		// holder ladder's answer already (dead / free / unknown).
		p.Why = "the row records no holder pid, so there is no process whose progress could be measured"
		return p
	}
	if ticks, ok := procname.CPUTicks(procname.ProcRootDefault, strconv.Itoa(pid)); ok {
		p.CPUKnown = true
		p.CPUSeconds = float64(ticks) / float64(procname.ClockTicksPerSecond)
	}
	path, when, found := taskProgressNewestAdvance(t.TaskID, t.SessionID)
	if !found {
		p.Why = fmt.Sprintf("no transcript or run log exists for this row (%s), so whether it is advancing cannot be measured", taskProgressAbsence(t))
		return p
	}
	p.Evidence = path
	p.LastAdvance = float64(when.Unix())
	idle := now.Sub(when)
	if idle < 0 {
		// A stamp in the future is clock skew, not negative silence: report the
		// measurement as 0 rather than a duration that reads as nonsense.
		idle = 0
	}
	p.IdleSeconds = idle.Seconds()
	cpu := taskProgressCPUPhrase(p)
	if idle < taskProgressIdleAfter {
		p.State = TaskProgressRunning
		p.Why = fmt.Sprintf("its transcript advanced %s ago (threshold %s)%s", taskFormatDuration(idle), taskFormatDuration(taskProgressIdleAfter), cpu)
		return p
	}
	p.State = TaskProgressIdle
	p.Why = fmt.Sprintf(taskProgressIdleText, taskFormatDuration(idle), t.TaskID) + cpu
	return p
}

// taskProgressCPUPhrase renders the corroborating CPU reading as a clause —
// "used 24s of CPU over 5h18m (0.1%)" — or "" when it could not be measured.
// It never decides anything: it is here because the ratio is what makes a
// wedged run legible.
func taskProgressCPUPhrase(p TaskProgress) string {
	if !p.CPUKnown {
		return ""
	}
	clause := fmt.Sprintf("; cpu %s", taskFormatDuration(time.Duration(p.CPUSeconds*float64(time.Second))))
	if p.ElapsedSeconds > 0 {
		clause += fmt.Sprintf(" over %s of wall clock (%.2f%%)",
			taskFormatDuration(time.Duration(p.ElapsedSeconds*float64(time.Second))),
			100*p.CPUSeconds/p.ElapsedSeconds)
	}
	return clause
}

// taskProgressNewestAdvance finds the newest write among the files the run
// itself produces: its session transcript (the primary evidence), and its raw
// log beside it (the second writer, so a run whose JSONL lags its raw log is
// not called idle).
//
// Both addresses come from the EXISTING resolvers — taskFindSessionJSONLForRow
// (the one the run-end reader and the ledger views already use) and
// rundir.RunDirRawLog — so this axis can never name a different file than the
// rest of the tree does (Rules 13/14).
func taskProgressNewestAdvance(taskID, sessionID string) (string, time.Time, bool) {
	var best string
	var when time.Time
	consider := func(path string) {
		if strings.TrimSpace(path) == "" {
			return
		}
		fi, err := os.Stat(path)
		if err != nil || fi.IsDir() {
			return
		}
		if best == "" || fi.ModTime().After(when) {
			best, when = path, fi.ModTime()
		}
	}
	if gd := gopiDataRoot(); gd != "" && strings.TrimSpace(sessionID) != "" {
		consider(taskFindSessionJSONLForRow(filepath.Join(gd, config.SessionsDirName), taskID, sessionID))
	}
	if dir := taskRunDir(taskID); dir != "" {
		consider(rundir.RunDirRawLog(dir))
	}
	return best, when, best != ""
}

// taskProgressAbsence names WHAT was searched when nothing answered — the
// difference between "this row never ran" and "the reader looked nowhere"
// (Rule 15).
func taskProgressAbsence(t *Task) string {
	if strings.TrimSpace(t.SessionID) == "" {
		return "it records no session id"
	}
	dir := taskRunDir(t.TaskID)
	if dir == "" {
		return "its run directory cannot be resolved"
	}
	return "searched under " + dir + " and under the session id " + t.SessionID
}

// taskFormatDuration is REUSED from tasks.go, not redefined here (Rule 14): one
// compact duration renderer for the whole ledger, so the same silence can never
// print as two different durations on two lines of one report.
