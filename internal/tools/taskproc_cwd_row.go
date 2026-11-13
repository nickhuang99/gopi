package tools

// THE CWD ROW RUNG of the process-identity ladder.
//
// WHY IT EXISTS (the bug this file answers). The tri-factor identity
// (pid + starttime + cmdline, taskproc.go) answers "is this pid the process the
// row recorded?". It does NOT answer the question a HOLDER actually raises:
// "which ROW is that process serving RIGHT NOW?". A `gopi worker` pid that has
// finished row A and gone on to drain the queue for row B still matches A's
// recorded identity perfectly — same pid, same incarnation, same argv — so A
// read `verdict: running` FOREVER, `resumable: no`, and A's recovery path was
// closed by a process that never once touched A again. Measured on this host
// 2026-11-01: row 01a19075-80b5's holder pid 1604177 had
// /proc/1604177/cwd = ~/.gopi/sessions/01a1954e-…, i.e. row 01a1954e's session
// container, and `gopi worker --status` reported that process as idle for
// 01a1954e.
//
// WHAT THE EVIDENCE IS, AND WHY IT IS READABLE. /proc/<pid>/cwd is the ONE
// per-process fact that survives across sessions on this host: stat, cmdline
// and comm pin an INCARNATION, and cwd is the only channel that additionally
// names the row the process is standing in (memory #2636 measured ~185 of ~360
// /proc entries EACCES for other users' processes under yama ptrace_scope=1 —
// the numbers match, so a holder of OUR OWN row is readable, which is the only
// case this rung needs).
//
// THE ROW IDENTITY IS CARRIED BY THE DIRECTORY NAME, NEVER BY A PATH PREFIX,
// and that is a deliberate refusal of an assumption that looks safe and is not:
// a row's run may stand in <gopi dir>/sessions/<row-id> (a worker) OR in
// <workspace root>/<row-id> (a task run) OR in a delivered ground that has been
// REGISTERED under a different path than it was created at — this host has the
// same clone registered at /home/nick/workspace/<row> and
// /home/nick/workspaces/<row>. A prefix test against one configured root would
// silently answer "not a row directory" for the other, which is a silent
// fallback of exactly the kind Rule 15 forbids. So the predicate is:
//
//	basename(dir) is a MINTED session-container name (internal/scratch
//	.IsMintedSessionContainerName: a canonical uuid, optionally carrying ONE
//	minted ordinal — `<uuid>-r1` reissue, `<uuid>-c1` clone)  ⇒  it names that row
//
// Every directory shape this system mints for a row is named after the row id,
// so the basename IS the row identity and no path is spelled here.
//
// WHAT IT MAY DO TO A VERDICT: exactly one thing, and it is to make it DEFENSIBLE
// WORSE — `running` → `recycled`. It never promotes anything, never touches a
// row the pid-level evidence already called dead/recycled/unknown, and never
// fires on a row that recorded no row identity (a legacy record has no claim to
// contradict).
//
// THE UNREADABLE CASE IS NAMED, NEVER SILENT (Rule 15). A readlink that fails
// (EPERM under ptrace_scope, a container, an X-forwarded pid namespace, a race
// with the process exiting) leaves the pid-level verdict STANDING — it is not a
// failure the ladder may manufacture a death from — and the reason line gains a
// sentence naming the missing evidence, which the `cwd:` line of
// `gopi task status` prints in full. A reader must be able to tell "I measured
// the cwd and it names another row" from "I could not measure the cwd".

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/nick/gopi/internal/scratch"
)

// The step markers of the one sentence this rung appends to a Running verdict's
// reason. They are the ledgers of Rule 15 here: each states which of the three
// possible readings was taken, so a reader is never left to infer it.
const (
	// taskCwdEvidenceAgrees: the directory's name IS this row.
	taskCwdEvidenceAgrees = "cwd %s names this row (%s)"
	// taskCwdEvidenceNamesAnotherRow: the directory's name is ANOTHER row.
	taskCwdEvidenceNamesAnotherRow = "cwd %s names row %s, NOT this row (this row is %s)"
	// taskCwdEvidenceNamesNoRow: read, but the name is not a minted row name.
	taskCwdEvidenceNamesNoRow = "cwd %s does not name a row — it neither corroborates nor contradicts this one"
	// taskCwdEvidenceUnreadable: the kernel did not answer (Rule 15).
	taskCwdEvidenceUnreadable = "cwd could NOT be read (%v) — the row evidence is MISSING, so this verdict rests on pid identity alone"
	// taskCwdEvidenceNoRecordedRow: the row recorded nothing to compare against.
	taskCwdEvidenceNoRecordedRow = "cwd %s is not compared — the row records no row identity to check it against"
)

// taskCwdRowReading is what /proc/<pid>/cwd says about WHICH ROW a live process
// is serving. It is the ONE answer both consumers read: the ladder (which turns
// a contradiction into a verdict) and the status view (which prints the
// measurement). Keeping one struct is what stops the two from drifting into a
// second implementation (Rule 14).
type taskCwdRowReading struct {
	// Read reports whether the kernel answered the readlink.
	Read bool
	// Dir is the directory the readlink named, as the kernel spelled it (an
	// unresolved link target; the ROW comparison uses its base name, so a
	// symlinked path cannot hide the row from it).
	Dir string
	// Row is the row id the directory's name carries, or "" when the name is
	// not a minted row-container name (or the read failed).
	Row string
	// Recorded is the row the RECORD says the process belongs to ("" when it
	// records none). It rides the reading so contradictsRow is a comparison of
	// two facts held in one value — the first draft of this file compared the
	// row to EMPTINESS instead of to the record, so every readable row
	// directory "contradicted" its own row and a healthy runner read
	// `recycled`. That shape is caught by the suite's positive control, which
	// is why the control exists.
	Recorded string
	// Note is the evidence sentence — non-empty in EVERY case, so no caller has
	// to invent a description of a reading that was not taken (Rule 15).
	Note string
}

// taskHolderCwdReading takes the reading for one pid against the row the record
// says the process belongs to. recordedRow == "" means the record names no row
// (a legacy row, or a direct caller with no ledger row): the cwd is then READ
// and REPORTED but never compared, because there is nothing to compare it to.
func taskHolderCwdReading(pid int, recordedRow string) taskCwdRowReading {
	dir, err := procCwdAt(identityProcRoot, pid)
	if err != nil {
		return taskCwdRowReading{
			Note: fmt.Sprintf(taskCwdEvidenceUnreadable, err),
		}
	}
	reading := taskCwdRowReading{Read: true, Dir: dir, Recorded: recordedRow}
	if recordedRow == "" {
		reading.Note = fmt.Sprintf(taskCwdEvidenceNoRecordedRow, dir)
		return reading
	}
	row, ok := sessionContainerRowOf(dir)
	if !ok {
		reading.Note = fmt.Sprintf(taskCwdEvidenceNamesNoRow, dir)
		return reading
	}
	reading.Row = row
	if row == recordedRow {
		reading.Note = fmt.Sprintf(taskCwdEvidenceAgrees, dir, row)
		return reading
	}
	reading.Note = fmt.Sprintf(taskCwdEvidenceNamesAnotherRow, dir, row, recordedRow)
	return reading
}

// contradictsRow reports whether the reading PROVES the process is serving a
// different row. Only a READ directory whose name is a minted row name that is
// NOT this row qualifies: an unreadable cwd proves nothing, and a directory
// name that is not a row name is not evidence either way.
func (r taskCwdRowReading) contradictsRow() bool {
	return r.Read && r.Row != "" && r.Recorded != "" && r.Row != r.Recorded
}

// procCwdAt is the ONE reader of /proc/<pid>/cwd in this package, parameterised
// by procfs root like its three siblings (pidAlive / procStartTicks /
// procCmdline), so a test can drive the REAL callers over a fixture and so a
// caller cannot hand-join the path (Rules 13/14).
func procCwdAt(root string, pid int) (string, error) {
	target, err := os.Readlink(filepath.Join(root, strconv.Itoa(pid), procCwdName))
	if err != nil {
		return "", err
	}
	if target == "" {
		return "", fmt.Errorf("%s is empty", procCwdName)
	}
	return target, nil
}

// sessionContainerRowOf answers "which row does this directory's NAME name?" —
// the basename form of the row identity (see this file's header for why the
// name, and never a path prefix, is the predicate).
//
// The vocabulary is internal/scratch's, which is the ONE definition of the
// names this system mints for a session container / a row's ground directory
// (canonical uuid, optionally one ordinal suffix `<uuid>-r1` / `<uuid>-c1`).
// A name outside that vocabulary is reported as "names no row" by the caller,
// never guessed at.
func sessionContainerRowOf(dir string) (string, bool) {
	base := filepath.Base(filepath.Clean(dir))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "", false
	}
	if !scratch.IsMintedSessionContainerName(base) {
		return "", false
	}
	return base, true
}

// taskHolderCwdRung applies the cwd row evidence to a verdict the pid-level
// ladder already reached. It is called ONLY from the one implementation of the
// ladder (taskIdentityVerdict), so every consumer — `gopi task status`, the
// list renderer, `task show`, the run door and the resume gate — gets this rung
// for free and none of them can re-roll it (Rule 14).
//
// The transition it owns, spelled out:
//
//	pid-level: running (incarnation matched)  +  cwd names ANOTHER row
//	    → recycled, and the reason names BOTH facts: the process is the
//	      recorded one, and it is serving a different row. `recycled` is the
//	      vocabulary's word for "the pid is alive but is NOT the recorded
//	      runner's row any more" — exactly the measured shape, and precisely the
//	      word every consumer already treats as reclaimable
//	      (taskVerdictResumable: true, taskRunProcessStateVerdict: free), so the
//	      handoff row becomes resumable without touching the state machine.
//
//	everything else                                → unchanged
//	running + cwd unreadable / not a row name / no recorded row
//	                                               → running, reason EXTENDED
//	                                                 with the named gap
func taskHolderCwdRung(pid int, recorded taskProcIdentity, v taskProcVerdict) taskProcVerdict {
	if v.State != TaskProcessStateRunning {
		return v
	}
	reading := taskHolderCwdReading(pid, recorded.CwdRow)
	if reading.contradictsRow() {
		return taskProcVerdict{
			State: TaskProcessStateRecycled,
			Reason: fmt.Sprintf(
				"pid %d IS the recorded process (pid+starttime match) but it is serving ANOTHER row: the row records row %s and the process's %s is %s — the recorded runner has handed this row's pid on, so the row is reclaimable",
				pid, recorded.CwdRow, procCwdName, reading.Dir),
		}
	}
	v.Reason = v.Reason + "; " + reading.Note
	return v
}
