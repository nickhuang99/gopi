package tools

// ROW-KEYED RUN RESOLUTION (epic run-identity-unify, 2026-09-20).
//
// A ledger row's run directory is named by the ROW's OWN id: sessions/<task_id>.
// Everything a reader needs — the run logs, the raw-log checkpoints, how many
// runs there were and how they ended — is inside that one directory, so nothing
// here reads the row's session_id column. That column is a second, weaker
// pointer at the same run: it is 1:1 while the directory is 1:N (one task,
// several serial runs: a killed run plus its resume/retry), it frequently names
// the run that did NOT execute the row, and it is not an address at all.
//
// WHAT THE COLUMN USED TO COST (measured before this file, on this host): the
// row-keyed question "which raw log do I resume" was answered by
// task_id → row.session_id → taskRawLogPathFromSession(sid) →
// taskFindSessionJSONL(sessionsDir, sid) — a whole-tree search for a file whose
// DIRECTORY the task id already names (~70k session dirs here). Resolution is
// now one os.ReadDir of one known directory plus two bounded line reads per run
// log, and it answers with the run a resume should actually continue.
//
// The legacy session-keyed passes are deliberately KEPT (taskSessionLogPath /
// taskRawLogPathFromSession / taskFindSessionJSONL) as the fallback for the two
// shapes the row-keyed path cannot speak for: a row whose run directory holds no
// run log at all (the run was re-homed, or the row predates the layout), and a
// BARE session-id target (a chat resume), which has no task row to name it.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/rundir"
	"github.com/nick/gopi/internal/sessionevent"
)

// taskRunTimeLayout renders a run's start on the run lines. It is the RFC3339
// spelling minus the zone-less ambiguity risk: the ledger's other time columns
// are local wall clock (formatTaskTime), and a run line is read next to them —
// same clock, one shape (Rule 1b: the layout is named, never inlined).
const taskRunTimeLayout = "2006-01-02 15:04:05"

// task show run-block labels (Rule 1b: the renderer's own spelling lives once,
// so a caller that must read the resolved ground back — a test, a script's
// grep — references the constant rather than a re-typed copy).
const (
	TaskShowRunDirLabel = "run_dir:"
	TaskShowRunLogLabel = "run_log:"
	TaskShowRunsLabel   = "runs:"
	TaskShowRunLabel    = "run:"
)

// TaskRunDirMissingMark is what the `run_dir:` line says about a directory that
// does not exist (Rule 15: an absence is STATED, never rendered as a path the
// reader is invited to open).
//
// WHY IT EXISTS (measured 2026-10-29 on this host's live ledger): `run_dir:`
// was computed from the row's own id and never stat'ed, so 32 of 39
// in_progress rows printed a path that was not there. The correlation was
// exact — every one of them carried the `[auto]` marker, because an auto row's
// run executes in the PARENT session's directory and the row-keyed directory is
// never created; the same output already printed that true address on its
// `session_log:` line. Two addresses in one block, one of them false, and the
// false one introduced as the run's home.
const TaskRunDirMissingMark = "MISSING — run directory not created"

// TaskRunDirSessionMark introduces the address that DOES open, on a row whose
// own directory is absent. It names `session_log` on purpose: the reader can
// check the two lines against each other instead of trusting this one.
const TaskRunDirSessionMark = "the run lives in"

// TaskRunDirSessionNote is the tail of that clause — the pointer at the sibling
// line that already carried this address.
const TaskRunDirSessionNote = "(see session_log)"

// TaskRunDirNoSessionNote is the honest answer when not even the session column
// names a directory: the row's runs are re-homed or gone (Rule 15 — the absence
// is stated, and NOT papered over with the row-keyed path standing alone).
const TaskRunDirNoSessionNote = "no session directory is recorded for this row"

// taskStatusReportLabelWidth is the label column of `gopi task status`'s report
// (its other lines — status:/summary:/resumable: — pad to the same width). Named
// once so the run_dir line can share the renderer without changing the shape of
// the block it sits in (Rule 1b).
const taskStatusReportLabelWidth = 13

// taskStatusRunDirField renders the status report's `run_dir:` line from the ONE
// value renderer: same label, same value, that report's own column width.
func taskStatusRunDirField(value string) string {
	return TaskShowRunDirLabel +
		strings.Repeat(" ", maxInt(1, taskStatusReportLabelWidth-len(TaskShowRunDirLabel))) + value
}

// taskRunDirValue renders the VALUE of a `run_dir:` line — the ONE function
// both faces of the view read (`task show`'s run block and `task status`'s
// report), so they cannot disagree about the address this row's runs live at.
//
// It is HONEST BY MEASUREMENT, not by construction: the row-keyed directory is
// stat'ed, and when it is absent the line says so and names the session
// directory the row's `session_log` really points at. Nothing here changes what
// DISPATCH does — an auto row having no directory of its own is another defect
// (it is why these rows were never created); this function only guarantees that
// what a reader sees is true.
func taskRunDirValue(taskID, sessionID string) string {
	dir := taskRunDir(taskID)
	if dir == "" {
		return ""
	}
	if rundir.Exists(dir) {
		return dir
	}
	if sess := taskSessionDirAddress(sessionID); sess != "" {
		return dir + "  " + TaskRunDirMissingMark + " — " + TaskRunDirSessionMark +
			" " + sess + " " + TaskRunDirSessionNote
	}
	return dir + "  " + TaskRunDirMissingMark + " — " + TaskRunDirNoSessionNote
}

// taskSessionDirAddress resolves the SESSION's own directory from the ledger
// column — the address an auto row's run actually occupies. It answers only a
// directory that exists, so the line never names a second place that is also
// missing (Rule 15 cuts both ways: a guess is as dishonest as a silent hole).
//
// It is a DIRECT construction, never taskSessionLogPath's tree walk: this value
// is rendered on every `task show`, including the listing's rows, and the walk
// costs ~287k openat calls on this host (see taskFindSessionJSONLForRow). The
// directory layout is the one internal/agent mints and internal/sessionlayout
// classifies: <gopi dir>/sessions/<session-id>/.
func taskSessionDirAddress(sessionID string) string {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return ""
	}
	gd := gopiDataRoot()
	if gd == "" {
		return ""
	}
	dir := filepath.Join(gd, config.SessionsDirName, sid)
	if !rundir.Exists(dir) {
		return ""
	}
	return dir
}

// The per-run statuses the run block renders. "unfinished" is a statement
// about the LOG (no session_end event), never about the process — a live run
// and a SIGKILLed run read the same here, and claiming more would be a guess.
const (
	runStatusCompleted   = "completed"
	runStatusUnfinished  = "unfinished"
	runStatusNoSessionSt = "(no session_start)"
)

// The terminal block's UNTRUSTED rendering (Rule 1b: one spelling each).
//
// A run line prints its scale numbers under the word "run", so a number that
// describes only the LAST segment of a resumed log would be a fabricated
// run-total. The dash is what takes that number's place — and the reason
// beside it is what keeps the dash from reading as "measured zero" (Rule 15:
// an absence must be stated, never implied).
const (
	// taskRunPassNoTotalMarker is the value printed where a run-level pass
	// count belongs when the record cannot supply one.
	taskRunPassNoTotalMarker = "—"
	// taskRunPassSegmentedFmt names the PROVEN plurality: how many segments
	// the read saw. It is a format because the count is the fact.
	taskRunPassSegmentedFmt = "%d segments in the read window, so the last session_end is not this run's total"
	// taskRunPassTruncated is the honest unknown: the window did not reach the
	// log's first byte, so a segment before it is invisible here.
	taskRunPassTruncated = "session_end outside the read window, so this run's total is unread"
	// taskRunTokenUnit is the token count's unit word on the run line.
	taskRunTokenUnit = "tokens"
)

// taskRunDir returns the row's run directory — the ONE address a row can
// always name, because the directory is named by the row's own id. Directories
// created by the session layout use task_id verbatim, so no normalization
// beyond the shared trim applies (taskIDNormalize).
func taskRunDir(taskID string) string {
	tid := taskIDNormalize(taskID)
	if tid == "" {
		return ""
	}
	gd := gopiDataRoot()
	if gd == "" {
		return ""
	}
	return rundir.Dir(gd, tid)
}

// taskRuns enumerates the row's runs, newest last. An absent directory, or one
// holding no run log, yields an empty slice — the caller distinguishes the two
// with rundir.Exists (a ledger view must not blur "never ran" into "logs gone").
func taskRuns(taskID string) []rundir.Record {
	dir := taskRunDir(taskID)
	if dir == "" {
		return nil
	}
	return rundir.RecordsDir(dir)
}

// taskRunDirHoldsRuns reports whether the row's run DIRECTORY holds at least
// one run log — the difference between "this row ran and its runs kept no
// checkpoint" (a definite absence about THIS row) and "this row's directory is
// not there at all" (the pre-layout / re-homed shape, where the session_id
// column is the only pointer that exists).
//
// WHY A RESOLVER NEEDS THIS (measured 2026-10-28): without it, a row whose runs
// recorded no raw log fell through to the session_id column, which is
// documented as "the session that SUBMITTED or executed the row" and therefore
// frequently names a CHAT session. A resume then continued that other
// conversation — see tools.TaskResumeTarget's own comment for the run that
// demonstrated it.
func taskRunDirHoldsRuns(taskID string) bool {
	return len(taskRuns(taskID)) > 0
}

// taskRunResumeRawLog resolves the raw-log checkpoint a `gopi session resume <task-id>`
// should continue: the NEWEST run of the row's directory that still has one on
// disk.
//
// WHY newest-with-a-checkpoint rather than simply the newest run: a row's
// directory holds every serial run it ever had, and the last one may be a
// short probe that never recorded a raw log (measured on this host: row
// t-1788773479 holds a 68-turn run with a checkpoint AND a 2-turn run whose
// raw log is gone). Resuming the probe would replay the wrong context, so the
// newest run that CAN be resumed wins; when none can, the newest run's raw log
// is returned anyway if the file exists beside the log (the re-home case), and
// "" otherwise — a definite absence the caller reports rather than guesses
// past (Rule 15).
func taskRunResumeRawLog(taskID string) string {
	recs := taskRuns(taskID)
	// THE RUN DIRECTORY IS CONSULTED EVEN WITH NO RECORD YET (2026-10-28). The
	// early `return ""` that used to sit here made the fallback below
	// UNREACHABLE for exactly the case its own comment names — a run whose
	// session_start line was never flushed, or one interrupted before it
	// recorded its rawLogPath — because such a run leaves a checkpoint FILE
	// and no JSONL record, so RecordsDir answers empty and the function
	// answered empty too. Measured on the G4 help-surface walk
	// (TestG4EveryAdvertisedSurfaceRuns): the fixture installs
	// sessions/<task-id>/<task-id>.raw.log, `gopi session resume <task-id>`
	// then refused with "holds no run whose raw log still exists" about a
	// directory that was holding one, and the advertised example was
	// unrunnable — the surface was advertised and did not work.
	if len(recs) == 0 {
		return rundir.RunDirRawLog(taskRunDir(taskID))
	}
	if rec, ok := rundir.WithCheckpoint(recs); ok {
		return rundir.ExistingRawLog(rec)
	}
	if rec, ok := rundir.Latest(recs); ok {
		if raw := rundir.ExistingRawLog(rec); raw != "" {
			return raw
		}
	}
	// THE RUN LOG'S OWN ADDRESS (measured 2026-10-28): the directory's newest
	// run LOG names the run a resume continues, and that run's checkpoint may
	// be co-located under a DIFFERENT name than the log — a run interrupted
	// before it recorded its rawLogPath, or one whose session_start line was
	// never flushed. Both answers come from internal/rundir's ONE resolver
	// family (Rules 13/14), never from a fresh glob here.
	//
	// WHY IT BELONGS IN THIS FUNCTION rather than in the caller's fallback: the
	// caller (cmd/agent resolveResumeTarget) now REFUSES a row that resolves no
	// checkpoint, so a shape answered only by the caller's filesystem branch
	// would turn from "resolved" into "hard error". One function answers "what
	// is this row's checkpoint" for every layout the sessions tree has.
	return rundir.RunDirRawLog(taskRunDir(taskID))
}

// taskRunRecordedRawLog is the newest run's RECORDED raw-log path, whether or
// not the file still exists — the last resort that keeps a pruned checkpoint
// from becoming a whole-tree search.
//
// MEASURED (live ledger + sessions tree, 2026-09-20):
//   - row t-1788773479 records raw logs for BOTH of its runs and NEITHER file is
//     on disk any more. Answering "" there (the honest "no checkpoint") sent the
//     resume caller into the session-keyed fallback, whose whole-tree search cost
//     1m30s (cold) / 3.27s (warm) on this host to conclude the same thing.
//     Naming the recorded-but-missing path makes the caller fail in 14ms and say
//     which checkpoint is gone.
//   - 27 of the 238 identity-matched bare chat raw logs on this host have NO jsonl
//     beside them, so "the recorded path is gone" is ALSO the ordinary chat shape,
//     not an edge case: resolving it fast matters on the resume path a chat takes.
func taskRunRecordedRawLog(taskID string) string {
	recs := taskRuns(taskID)
	for i := len(recs) - 1; i >= 0; i-- {
		if p := strings.TrimSpace(recs[i].RawLogPath); p != "" {
			return p
		}
	}
	return ""
}

// taskRunNewestRawLog is the newest run's raw-log path REGARDLESS of whether it
// still exists — the display form, so `task show` can render the recorded path
// together with a "(not found)" marker instead of hiding the fact (the same
// honest-absence posture the session_log/raw_log lines already take). A run that
// RECORDED no raw log at all still falls through to the run directory's
// same-named sibling when one is there (the re-home case).
func taskRunNewestRawLog(taskID string) string {
	recs := taskRuns(taskID)
	rec, ok := rundir.Latest(recs)
	if !ok {
		return ""
	}
	if p := strings.TrimSpace(rec.RawLogPath); p != "" {
		return p
	}
	return rundir.ExistingRawLog(rec)
}

// CarryForwardHasConclusions reports whether a session wrote any committed
// working-area conclusions — the test that lets the resume path pick the run
// whose conclusions should actually be re-injected instead of arming the
// carry-forward with a session that wrote none. A read failure is "no
// conclusions" (SessionConclusions' own contract: this read must never fail a
// recovery).
func CarryForwardHasConclusions(sessionID string) bool {
	concs, err := SessionConclusions(sessionID)
	return err == nil && len(concs) > 0
}

// TaskCarryForwardSessionIDs returns the session ids a resume of this row should
// try to carry committed conclusions forward from, BEST FIRST: the sessions of
// the row's own runs, newest first (the run that just crashed is the one whose
// write_task_area conclusions belong in the resumed context), then the ledger
// column as the last resort (a row whose run directory holds no run log).
//
// WHY THE RUNS COME FIRST (epic run-identity-unify): conclusions are keyed by
// the SESSION that wrote them (session_conclusions.session_id — the run id, not
// the row id), and the column is 1:1 while a row's directory is 1:N. For a row
// that ran more than once the column routinely names the OTHER run — a
// submitter session that wrote no conclusions at all, or a run that ended before
// the work started — so carrying forward from the column alone silently drops
// the conclusions a recovery exists to re-inject.
func TaskCarryForwardSessionIDs(taskID string) []string {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return nil
	}
	var out []string
	seen := make(map[string]bool)
	add := func(sid string) {
		sid = strings.TrimSpace(sid)
		if sid == "" || seen[sid] {
			return
		}
		seen[sid] = true
		out = append(out, sid)
	}
	recs := taskRuns(taskID)
	for i := len(recs) - 1; i >= 0; i-- {
		// The run's own file name IS its session id in the task-run layout;
		// the recorded session_start field is preferred when present.
		add(recs[i].SessionID)
		if recs[i].RunID != "" {
			add(recs[i].RunID)
		}
	}
	// Last resort: the ledger column, for a row whose run directory holds no
	// run log (a re-homed or pre-layout run). Read here — and ONLY here, on the
	// carry-forward path — never on the resolution path the epic moved.
	if sid, err := TaskSessionID(taskID); err == nil {
		add(sid)
	}
	return out
}

// taskRunNewestLog returns the newest run's own log path — the file an operator
// tails. "" when the row's directory holds no run log.
func taskRunNewestLog(taskID string) string {
	rec, ok := rundir.Latest(taskRuns(taskID))
	if !ok {
		return ""
	}
	return rec.Path
}

// taskModelFromRunDir resolves the model of the run that executed a row, read
// off the newest run log in the row's OWN directory — the model a legacy row
// (model column empty) display should name. It is the row-keyed half of the
// model fallback: the session_id-keyed lookup (taskModelFromSession) remains
// for a row whose directory holds no run log. "" when neither the directory
// nor the log answers.
func taskModelFromRunDir(taskID string) string {
	rec, ok := rundir.Latest(taskRuns(taskID))
	if !ok {
		return ""
	}
	return rec.Model
}

// taskRunLines renders the row's run block for `task show`:
//
//	run_dir:        <the row's run directory, VERIFIED to exist — a directory
//	                 that is not there is labelled MISSING and the line names
//	                 the session directory the run actually occupies instead
//	                 (see taskRunDirValue)>
//	run_log:        <the newest run's log (with a (not found) marker when gone)>
//	runs:           <N> run(s) — <c> completed, <k> with a raw-log checkpoint
//	run:            <id>  <start>  <status>  <turns> turns  <tokens> tokens
//	                 (one line per run, oldest first; the run a resume would
//	                  continue is marked — see taskRunResumeMark)
//
// The address was ONCE "always rendered, so the reader sees WHERE the runs
// live" — and that sentence stopped being true the moment it was measured: the
// path was joined from the row's id and never stat'ed, so for an `[auto]` row
// it named a directory that has never existed, in the same block whose
// `session_log:` line named the real one. The line is still always rendered;
// what changed is that it now MEASURES its address (measured 2026-10-29: 32 of
// 39 in_progress rows were missing their directory, all of them `[auto]`).
//
// It replaces the bare `session_id: <uuid>` line this view used to print: a
// reader asked "which run is this?" and got an id that named no directory, no
// file and no lineage — the operator's word for it was "completely
// meaningless". Every value here is read off the run's own log, never guessed.
func taskRunLines(taskID, sessionID string) []string {
	dir := taskRunDir(taskID)
	if dir == "" {
		return nil
	}
	recs := rundir.RecordsDir(dir)
	out := make([]string, 0, len(recs)+3)
	out = append(out, taskShowField(TaskShowRunDirLabel, taskRunDirValue(taskID, sessionID)))
	if len(recs) == 0 {
		// Honest absence, and it distinguishes the two shapes: a directory
		// that exists without run logs vs one that was never created.
		if !rundir.Exists(dir) {
			out = append(out, taskShowField(TaskShowRunsLabel, "none (run directory not created)"))
		} else {
			out = append(out, taskShowField(TaskShowRunsLabel, "none (no run log in the directory)"))
		}
		return out
	}
	// The log line: the newest run's own file, with the absence made explicit
	// rather than silent (the session_log line's contract).
	newest := recs[len(recs)-1]
	logLine := newest.Path
	if _, err := os.Stat(logLine); err != nil {
		logLine += " (not found — run log pruned)"
	}
	out = append(out, taskShowField(TaskShowRunLogLabel, logLine))

	done, ckpt := 0, 0
	for _, r := range recs {
		if r.HasEnd {
			done++
		}
		if rundir.ExistingRawLog(r) != "" {
			ckpt++
		}
	}
	out = append(out, taskShowField(TaskShowRunsLabel,
		strconv.Itoa(len(recs))+" run(s) — "+strconv.Itoa(done)+" "+runStatusCompleted+
			", "+strconv.Itoa(ckpt)+" with a raw-log checkpoint"))

	resumeRec := ""
	if rec, ok := rundir.WithCheckpoint(recs); ok {
		// The mark sits on the record taskRunResumeRawLog resolves — the
		// newest run that still owns a checkpoint. When the newest run kept
		// none, the mark belongs on the older one it falls back to: the
		// reader must not have to re-derive the choice.
		resumeRec = rec.Path
	}
	for _, r := range recs {
		out = append(out, taskShowField(TaskShowRunLabel, taskRunLine(r, r.Path == resumeRec)))
	}
	return out
}

// taskRunResumeMark suffixes the run a `gopi session resume <task-id>` would continue,
// so the reader does not have to re-derive the choice this package made.
const taskRunResumeMark = "  ← resume target"

// taskRunLine renders ONE run: its id, when it began, how it ended, its own
// scale numbers. A field the log does not carry renders as an explicit absence
// (never a zero that reads as a measurement), so an unfinished or truncated run
// is visible AS unfinished.
func taskRunLine(r rundir.Record, resumeTarget bool) string {
	var b strings.Builder
	id := r.RunID
	if id == "" {
		id = filepath.Base(r.Path)
	}
	b.WriteString(id)
	if r.HasStart {
		b.WriteString("  " + r.Started.Format(taskRunTimeLayout))
	} else {
		b.WriteString("  " + runStatusNoSessionSt)
	}
	switch {
	case r.HasEnd && r.HasExit:
		b.WriteString("  " + runStatusCompleted + " exit=" + strconv.Itoa(r.ExitCode))
	case r.HasEnd:
		b.WriteString("  " + runStatusCompleted)
	default:
		b.WriteString("  " + runStatusUnfinished)
	}
	// THE TERMINAL BLOCK: printed only when rundir says it describes the RUN.
	//
	// rundir reads a bounded tail (1MB) last-writer-wins, so a log that
	// `gopi session resume` continued holds TWO `session_end` verdicts and
	// the second one — the resumed segment's own, routinely 0 passes — won.
	// The pre-fix line tested `r.Turns > 0` and so printed NOTHING: the pass
	// block, the token block and the elapsed block all vanished, silently, on
	// exactly the rows a reader most wants to size (measured 2026-10-27 on
	// session 4a0957a3: total_iterations [29, 0] ⇒ the row rendered
	// "completed exit=0" with no scale numbers at all).
	//
	// Two wrong answers were available and both are refused: printing the
	// last segment's 0 as the run's total (a fabricated measurement) and
	// printing the tail window's SUM (which silently drops every segment
	// outside the window — a new lie in place of the old one). What is left
	// is to SAY the number is not the run's total and why (Rule 15).
	switch {
	case r.ScaleTrusted:
		// The count is rundir's Turns field = session_end's total_iterations, i.e.
		// the LOOP PASS count — NOT operator rows. It is named for what it is:
		// printing it as "turns" made a run's progress line (31) disagree with
		// `gopi session log <id> --turns` (16) on the same session, one word for
		// two quantities (Rule 14; measured 2026-10-16).
		//
		// A real 0 prints as 0: with the whole log read and one verdict, zero
		// passes IS the measurement, and `turns > 0` used to swallow it.
		b.WriteString("  " + strconv.Itoa(r.Turns) + " " + sessionevent.CounterUnitLoopPass)
		b.WriteString(tokenBlock(r.Tokens, r.ElapsedSec))
	case r.HasEnd:
		b.WriteString("  " + sessionevent.CounterUnitLoopPass + " " + taskRunPassNoTotalMarker +
			" (" + taskRunScaleUntrustedReason(r) + ")")
	default:
		// UNFINISHED: no verdict at all. rundir's Turns is then the LAST
		// request event's iteration — progress-so-far, not a total — and this
		// branch keeps the wording it had before the multi-segment fix:
		// "which segment did this verdict come from" and "what word does an
		// unfinished run's progress print under" are two questions, and one
		// edit must not quietly answer both.
		if r.Turns > 0 {
			b.WriteString("  " + strconv.Itoa(r.Turns) + " " + sessionevent.CounterUnitLoopPass)
		}
		b.WriteString(tokenBlock(r.Tokens, r.ElapsedSec))
	}

	if resumeTarget {
		b.WriteString(taskRunResumeMark)
	}
	return b.String()
}

// tokenBlock renders the two scale numbers that arrive IN the same
// session_end as the pass count (so they share its trust) — each only when it
// is a positive measurement.
func tokenBlock(tokens int, elapsedSec float64) string {
	var b strings.Builder
	if tokens > 0 {
		b.WriteString("  " + strconv.Itoa(tokens) + " " + taskRunTokenUnit)
	}
	if elapsedSec > 0 {
		// Whole seconds: a run line is scanned, not measured — the precise
		// value stays in the run log's own session_end event.
		b.WriteString("  " + strconv.FormatFloat(elapsedSec, runElapsedFormat, 0, 64) + runElapsedSuffix)
	}
	return b.String()
}

// taskRunScaleUntrustedReason says WHY the terminal block is not the run's
// total, in the words of the fact that made it untrusted — never a bare dash,
// because a reader must be able to tell "resumed" from "unreadable".
func taskRunScaleUntrustedReason(r rundir.Record) string {
	// PROVEN plurality first: more than one segment verdict, or a second
	// `session_start` header (a resume's own proof, which survives even when
	// its `session_end` was cut off).
	if r.EndSegments > 1 {
		return fmt.Sprintf(taskRunPassSegmentedFmt, r.EndSegments)
	}
	if r.StartSegments > 1 {
		return fmt.Sprintf(taskRunPassSegmentedFmt, r.StartSegments)
	}
	// Then the honest unknown: the window did not reach the log's first byte,
	// so a segment before it is invisible here.
	return taskRunPassTruncated
}

// The run line's elapsed rendering: whole seconds, one named shape (Rule 1b).
const (
	runElapsedFormat = 'f'
	runElapsedSuffix = "s"
)

// taskShowField renders one `task show` label/value line at the block's shared
// gutter (taskShowLabelWidth) — one formatter, so a new line cannot drift out
// of the table the block draws.
func taskShowField(label, value string) string {
	return label + strings.Repeat(" ", maxInt(1, taskShowLabelWidth-len(label))) + value
}

// maxInt returns the larger of two ints.
//
// IT LIVES HERE (Rule 43) because its consumer is THIS file and not the
// cross-node sync mechanism: :93 and :596 pad a report label with maxInt(1, …).
// It used to be declared in memory_sync_procedural.go, the node→node
// procedural patch channel the sync retirement deletes; a definition MOVED is
// not a definition mirrored (Rules 13/14), so there is still exactly one in
// this package. internal/gopilog carries its own separate definition for its
// own package.
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
