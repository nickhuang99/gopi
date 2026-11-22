// selfrecord_reload.go — THE RECORD FILE IS THE TRUTH SOURCE OF THE NOTES.
//
// THE DEFECT THIS CLOSES (epic row 01a1fae1-2aab, measured 2026-11-21). The
// note list in selfRecordState.notes was only ever APPENDED to by the process
// that recorded the round (recordSelfNotes), so a process that CONTINUED an
// existing session started with an EMPTY list while the durable file beside the
// transcript still held every note the earlier process wrote. The first
// compaction after such a resume therefore re-rendered the carrier from the
// near-empty list and the older record vanished from the context while it was
// still on disk — measured: 49 notes in the file, 2 in the carrier.
//
// The repair is the one the epic names and no other: the FILE is the truth
// source, the in-memory slice is its CACHE, and a run that may be continuing an
// existing record REBUILDS the slice from the file once, at initialisation.
// Serialising the memory slice somewhere else would be a second source of truth
// that drifts from this one.
//
// NOT A REWRITER (Rule 15). The reader never writes, never repairs and never
// guesses: a missing file means "this run has no earlier notes" (the ordinary
// first-run state, not a failure), and an unreadable one is REPORTED with the
// path and the reason by the caller. A silent empty list is exactly what made
// the original defect invisible.
package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// selfRecordMayHoldEarlierNotes reports whether the session directory this run
// will write into may ALREADY contain a record, i.e. whether rebuilding the note
// list from the file can find anything.
//
// TWO shapes qualify, and both are properties of the DIRECTORY rather than of
// the caller's intent:
//
//   - resuming — the identity continues an existing session (the measured
//     defect: `gopi --session`, `session resume`, the chat --resume faces).
//   - a `task run` CHILD (taskID != "") — its directory is
//     sessions/<task-id>/, which is keyed by the ROW and therefore outlives one
//     run of it, so a re-dispatch writes its record beside its predecessor's.
//
// A bare new session mints a fresh uuid and lands in an empty directory, so it
// has nothing to read; the check costs one stat that misses.
func selfRecordMayHoldEarlierNotes(resuming bool, taskID string) bool {
	return resuming || taskID != ""
}

// reloadSelfRecordNotes REBUILDS the run's in-memory note list from the durable
// record file, and returns how many notes it now holds.
//
// It REPLACES the slice rather than appending to it: at initialisation nothing
// has been recorded by this process yet, so the file — append-only evidence —
// already holds everything this run would otherwise have to remember, and
// appending would double up if the call ever ran twice.
//
// The ceiling is maxSelfRecordNotes, applied to the LAST N notes on disk, so a
// pathological record cannot put an unbounded carrier into the context; that
// constant's own doc states the capacity derivation and why the ceiling is not a
// retention policy.
//
// A MISSING FILE IS NOT AN ERROR: a run whose record was never written (no note
// recorded yet, or a fresh directory) legitimately has nothing to restore, and
// reporting that as a failure would train readers to ignore the report. Anything
// else — a permission refusal, an I/O error, a directory where the file should
// be — comes back as an error for the caller to log (Rule 15).
func (a *Agent) reloadSelfRecordNotes() (int, error) {
	dir, err := a.selfRecordDir()
	if err != nil {
		return 0, err
	}
	path := filepath.Join(dir, selfRecordFileName)
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, fmt.Errorf("self-record: cannot read %s: %w", path, err)
	}
	notes := selfRecordNotesFromBody(string(body))
	a.selfRecord.notes = notes
	return len(notes), nil
}

// selfRecordNotesFromBody parses a whole record file's text into the note list,
// capped at the last maxSelfRecordNotes entries.
//
// EVERY LINE GOES THROUGH THE ONE PARSER (selfRecordNoteFromLine, Rule 14), so
// the reader and `gopi session log`/`session notes` cannot disagree about what a
// record line is — the file holds three stamp shapes (`t<n> `, the retired
// `r<n> `, the original `- `) forever, and this function adds no fourth rule.
//
// A blank tail and any line the parser refuses are skipped, never invented into
// a note: the append path ends the file with a newline, so the last split
// element is always empty.
func selfRecordNotesFromBody(body string) []string {
	if body == "" {
		return nil
	}
	var notes []string
	for _, line := range strings.Split(body, "\n") {
		if _, note, ok := selfRecordNoteFromLine(line); ok {
			if note = strings.TrimSpace(note); note != "" {
				notes = append(notes, note)
			}
		}
	}
	if len(notes) > maxSelfRecordNotes {
		notes = notes[len(notes)-maxSelfRecordNotes:]
	}
	return notes
}

// restoreSelfRecordNotes is the ONE call site's body, called from the production
// constructor after the session logger exists: rebuild the list when the run may
// be continuing a record, and REPORT every outcome on the run's log.
//
// It is not a fatal path. A run that cannot read its record is degraded (the
// carrier will hold only what this process records from now on), and the honest
// response to that is a named line in the run's own log — never a silent empty
// list, which is the state the defect looked like from the outside.
func (a *Agent) restoreSelfRecordNotes(resuming bool, taskID string) {
	if a == nil || a.SessionLog == nil {
		// No session logger means no run directory was opened at all (a minimal
		// test agent, a headless caller). There is nothing to read, and
		// selfRecordDir's own refusal covers the recording side.
		return
	}
	if !selfRecordMayHoldEarlierNotes(resuming, taskID) {
		return
	}
	n, err := a.reloadSelfRecordNotes()
	if err != nil {
		if a.Logger != nil {
			a.Logger.Printf("⚠️ self-record: could not restore the notes from this run's record — the re-injection carrier will only hold notes recorded from now on: %v", err)
		}
		return
	}
	if a.Logger == nil {
		return
	}
	if n == 0 {
		a.Logger.Printf("📝 self-record: no earlier note(s) in %s (this run starts the record)", selfRecordFileName)
		return
	}
	a.Logger.Printf("📝 self-record: restored %d note(s) from %s — the file is the truth source, the slice is its cache", n, selfRecordFileName)
}
