package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nick/gopi/internal/rundir"
	"github.com/nick/gopi/internal/tools"
)

// The TWO absences behind ONE old refusal (measurement: 2026-11-05, this host).
//
// `gopi worker --resume <task-id>` used to answer a row with no raw log with a
// SINGLE sentence — "… has no session checkpoint to resume: its run directory
// (…) holds no run whose raw log still exists, and no run of it recorded one" —
// for two situations whose operational meaning is opposite:
//
//	(a) NOTHING EVER STARTED. The row was filed (and often claimed and
//	    dispatched) but no run of it ever opened a session. There is nothing to
//	    continue, and the operator's way out is a NEW row (a new UUID), never a
//	    resume. The operator's ruling of 2026-11-05 (memory #27273) says exactly
//	    that: 「从未运行」的行不属于 resume 适用范围 — resume is for an
//	    interrupted run with a checkpoint to append to.
//	(b) A RUN EXISTED AND ITS CHECKPOINT IS GONE. The row really ran: its
//	    directory holds session logs, in which the run recorded a raw-log path,
//	    and the file named is no longer on disk (pruned, or the profile moved).
//	    Here a resume is the RIGHT verb on the right row; only the checkpoint was
//	    lost, and the remedy is a fresh attempt of the SAME work.
//
// Rendering (a) and (b) with one sentence sent the reader to the wrong remedy
// half the time, which is a Rule 15 defect: the two states are distinguished by
// a MEASUREMENT, so the refusal is required to make it.
//
// THE CRITERION IS THE RUN LOG, AND IT IS MEASURED RATHER THAN ASSUMED. A run
// opens its session log (*.jsonl) before it sends anything, so:
//
//	no run log AND no *.raw.log  →  no run of this row ever began  (a)
//	a run log exists             →  the row ran; the checkpoint is what is
//	                                missing, whether or not its record named
//	                                one                                  (b)
//
// MEASURED ON THIS HOST'S 3 768 DIRECTORY SESSIONS TREE (2026-11-05):
//
//	$ … for every dir with NO *.raw.log: does it hold a *.jsonl?
//	NO_RAW_LOG: dirs_with_jsonl(run began)=975  dirs_without_jsonl(nothing started)=1582
//
// so the axis separates two large, real populations — this is not a corner case
// being carved out of a single incident.
//
// THE SECOND HALF OF THE BRIEF'S SUGGESTION WAS MEASURED AND IS **NOT** USED,
// because it does not discriminate. "no record of a child process starting" was
// proposed as corroboration, spelled as the absence of the `executor cwd →`
// line in the run's output log. Both halves of that fail here:
//
//	$ grep -c 'executor cwd →' …/output.log   # rows with no run log at all
//	01a19075-80b5-7a85-b0df-02a55f1c8b16 → 1   (and this row NEVER RAN: no
//	    *.jsonl, no *.raw.log, its ground ahead=0 behind=61, zero output — see
//	    memory #22282, whose evidence is the same directory)
//	01a1a60e-b419-76d3-ae2c-0a368bdc09f3 → 1   (child env prepared, then nothing)
//	  52 of the 54 no-run-log dirs that have an output.log DO NOT have the line
//
// The line is written by the WORKER WRAPPER after it resolved the ground and
// BEFORE it spawned the child (cmd/agent/cli_tasks.go, taskCwdResolvedFmt), so
// it records "the wrapper got this far", never "a run began" — using it would
// classify a launched-then-died row as having run. The run log is written by the
// RUN side and is the honest discriminator, so it is the only one applied.
//
// WHAT THIS FILE DELIBERATELY DOES NOT DO. It does not relax "no checkpoint"
// into "start over": an unstarted row is refused as firmly as a lost one. The
// 2026-10-28 incident (an unstarted row's resume adopted the SUBMITTER chat's
// checkpoint and closed the row out from that other conversation's statistics)
// is what the surrounding resolveResumeTarget guards exist for, and this file
// adds wording, never a fall-through.

// Names for the two verdicts (Rule 1b: the verdict a test asserts is spelled
// once, and the refusal carries it, so "which branch did this take" is a grep
// rather than a reading of prose).
const (
	// resumeNoCheckpointNeverStarted is the a-branch verdict: no run of the row
	// ever opened a session.
	resumeNoCheckpointNeverStarted = "never-started"
	// resumeNoCheckpointRanNoCheckpoint is the b-branch verdict: the row ran, and
	// the checkpoint it recorded is no longer on disk.
	resumeNoCheckpointRanNoCheckpoint = "ran-no-checkpoint"
)

// The evidence vocabulary (Rule 1: the spellings a reader greps live here).
const (
	// resumeNoCheckpointDirAbsent is the measured state "the row's run directory
	// is not on disk", which is itself the strongest form of (a): a row that was
	// never claimed has no directory at all, and saying so is more useful than
	// saying "it holds nothing".
	resumeNoCheckpointDirAbsent = "is not on disk at all"
	// resumeNoCheckpointEntriesFmt renders what the directory DOES hold, capped
	// at resumeNoCheckpointEntryCap names — with the remainder COUNTED rather
	// than dropped, and the way to see the rest named (a listing is a screenful,
	// not a quotation).
	resumeNoCheckpointEntriesFmt = "holds %d entr(ies): %s"
	resumeNoCheckpointMoreFmt    = " and %d more (list them with: ls %s)"
	// resumeNoCheckpointNoEntries is the "the directory exists and is empty"
	// reading — stated rather than rendered as an empty list.
	resumeNoCheckpointNoEntries = "holds no entries at all"
	// resumeNoCheckpointEntryCap bounds the entry listing (Rule 1).
	resumeNoCheckpointEntryCap = 8
)

// The two refusals (Rule 1b/13: the sentence an operator reads and the tests
// assert lives in ONE constant each, never inline at the call site).

// resumeNoCheckpointNeverStartedFmt is the (a) refusal: the row NEVER STARTED,
// so there is nothing to resume and the remedy is a NEW row with its own UUID.
//
// THE REMEDY IS `gopi task submit`, because the operator's ruling (memory #27273)
// makes a NEW row with its OWN uuid the honest way out of an unstarted row, and
// `task submit` is the verb that does exactly that with nothing carried over —
// which is all an unstarted row has to offer.
//
// (`task clone` ALSO mints a fresh uuid now, since the same ruling reached its
// mint — taskCloneMintID, 2026-11-05. It is not named here because clone
// REFUSES an unstarted row by design: "it has not run yet, so its ground is not
// suspect — it is UNUSED", so pointing an unstarted row at it would be a door
// that answers with a refusal.)
const resumeNoCheckpointNeverStartedFmt = "resume target %q is task %s, and NO RUN OF IT EVER STARTED — verdict %s: its run directory (%s) %s, and it holds neither a session log (*.jsonl) nor a *.raw.log. " +
	"A run opens its session log before it sends anything, so an empty run directory means this row has no run to continue: it is not interrupted, it is UNSTARTED, and " +
	"\"resume\" is the wrong verb for it (resume continues a run's OWN recorded context; there is none here). " +
	"CORRECT WAY OUT — dispatch the work as a NEW row with its own UUID: gopi task submit with this row's payload (gopi task show %s prints it), or cancel this one first (gopi task update %s --status cancelled --reason \"unstarted\"). " +
	"Do NOT expect this id to resume, and do not treat \"no checkpoint\" as \"start over\": that substitution is the 2026-10-28 defect that continued an unrelated session."

// resumeNoCheckpointRanFmt is the (b) refusal: the row RAN, its run log(s) are
// here, and the raw-log checkpoint they name is GONE. The semantics are the
// ones this path already had (a recovery lands on the row's own work or does not
// run); what is added is the measured evidence and the verdict, so an operator
// can tell it apart from (a) without visiting the directory.
const resumeNoCheckpointRanFmt = "resume target %q is task %s, which has no session checkpoint to resume — verdict %s: its run directory (%s) holds %d run log(s) (%s), and none of them still names a raw log on disk. " +
	"The row DID run; the checkpoint was lost (pruned, or the profile moved), so there is no recorded context to continue from. " +
	"Nothing was started — a resume must continue the row's own work rather than an unrelated session. " +
	"Recover with: gopi task show %s (row state), gopi task clone %s --repo <repo> (a clean-room copy that does run), or resume a KNOWN session id / raw-log path directly"

// resumeRowEvidence is the measured state of a row's run DIRECTORY — the facts
// the two verdicts are decided on, gathered once so the refusal and its test
// cannot measure different things (Rule 2: one reader of the filesystem).
type resumeRowEvidence struct {
	// Dir is the directory that was measured (rundir.DirFor(taskID)).
	Dir string
	// Exists is "the directory is on disk" — false is a definite absence, not a
	// read failure (rundir.Exists draws that line).
	Exists bool
	// Entries are the directory's top-level entry NAMES, sorted.
	Entries []string
	// RunLogs are the session-log basenames in the directory: the record that a
	// run BEGAN. Non-empty rules out the never-started verdict.
	RunLogs []string
	// RawLogs are the *.raw.log basenames in the directory: a checkpoint that is
	// still here. Non-empty means nothing is missing (the caller would have
	// resolved it), so it is carried for the evidence line rather than the
	// decision.
	RawLogs []string
}

// neverStarted is the ONE predicate: no run log of any shape AND no raw log.
// It is a method (not a "get"/"is"-prefixed niladic function) because it answers
// about the evidence it is called on.
func (e resumeRowEvidence) neverStarted() bool {
	return len(e.RunLogs) == 0 && len(e.RawLogs) == 0
}

// verdict names which branch this evidence takes — the string the refusal
// carries and the tests assert.
func (e resumeRowEvidence) verdict() string {
	if e.neverStarted() {
		return resumeNoCheckpointNeverStarted
	}
	return resumeNoCheckpointRanNoCheckpoint
}

// entriesText renders the directory's contents for the refusal: counted, capped,
// and with the way to see the rest named (no silent truncation).
func (e resumeRowEvidence) entriesText() string {
	if !e.Exists {
		return resumeNoCheckpointDirAbsent
	}
	if len(e.Entries) == 0 {
		return resumeNoCheckpointNoEntries
	}
	shown := e.Entries
	rest := 0
	if len(shown) > resumeNoCheckpointEntryCap {
		rest = len(shown) - resumeNoCheckpointEntryCap
		shown = shown[:resumeNoCheckpointEntryCap]
	}
	text := fmt.Sprintf(resumeNoCheckpointEntriesFmt, len(e.Entries), strings.Join(shown, ", "))
	if rest > 0 {
		text += fmt.Sprintf(resumeNoCheckpointMoreFmt, rest, e.Dir)
	}
	return text
}

// runLogsText names the run logs the (b) verdict rests on.
func (e resumeRowEvidence) runLogsText() string {
	if len(e.RunLogs) == 0 {
		return ""
	}
	return strings.Join(e.RunLogs, ", ")
}

// resumeRowRunEvidence measures a row's run directory. It reads the directory
// ONCE and answers every fact the verdict needs, so "which branch" and "what the
// refusal says about it" cannot be decided from two different listings.
//
// The listing helpers are internal/rundir's OWN resolvers (Rules 13/14): a fresh
// glob here would be a second answer to "what is a run log in a run directory",
// free to drift from the one RecordsDir/RunDirRawLog already give the resume
// path.
func resumeRowRunEvidence(dir string) resumeRowEvidence {
	ev := resumeRowEvidence{Dir: dir, Exists: rundir.Exists(dir)}
	if !ev.Exists {
		return ev
	}
	for _, rec := range rundir.RecordsDir(dir) {
		ev.RunLogs = append(ev.RunLogs, filepath.Base(rec.Path))
	}
	// RunDirRawLog answers the newest co-located checkpoint; its existence is
	// the "there IS a raw log here" fact. The basename is what the evidence line
	// quotes, so a directory with several is still honest about one of them.
	if raw := rundir.RunDirRawLog(dir); raw != "" {
		ev.RawLogs = append(ev.RawLogs, filepath.Base(raw))
	}
	if subs, err := os.ReadDir(dir); err == nil {
		for _, se := range subs {
			ev.Entries = append(ev.Entries, se.Name())
		}
	}
	sort.Strings(ev.Entries)
	sort.Strings(ev.RunLogs)
	return ev
}

// resumeNoCheckpointError builds the refusal for a row that resolved NO
// checkpoint, having MEASURED which of the two absences this is (see the file
// header): never-started names the unstarted row and its own way out (a new
// UUID), ran-no-checkpoint keeps the recovery this path always offered and adds
// the run logs the verdict rests on.
//
// It is a HARD ERROR on both branches — the caller returns it and the resume
// does not run. Making the two branches differ in WORDING only is the point:
// relaxing an unstarted row into a fresh start is the 2026-10-28 defect, and
// nothing here may become that.
func resumeNoCheckpointError(target string, st *tools.TaskProcessStatus) error {
	data := resumeRowRunEvidence(rundir.DirFor(st.TaskID))
	if data.neverStarted() {
		return fmt.Errorf(resumeNoCheckpointNeverStartedFmt,
			target, st.TaskID, data.verdict(), data.Dir, data.entriesText(),
			st.TaskID, st.TaskID)
	}
	return fmt.Errorf(resumeNoCheckpointRanFmt,
		target, st.TaskID, data.verdict(), data.Dir,
		len(data.RunLogs), data.runLogsText(),
		st.TaskID, st.TaskID)
}
