package tools

import (
	"fmt"
	"strings"
	"time"

	"github.com/nick/gopi/internal/procstat"
	"github.com/nick/gopi/internal/rundir"
)

// sessionruns_show.go — ONE SESSION, RESOLVED ACROSS BOTH SOURCES.
//
// THE DEFECT THIS CLOSES. `gopi session show <id>` asked ONE table (chats) and
// answered from it. A task run has no chats row, so the page that exists to
// explain a session refused the very sessions an operator debugs ("what is task
// X doing right now, and where?" — the pid, the workdir, the log). Those facts
// were on disk in the run's own `session_start` and nothing printed them.
//
// WHAT THIS IS. A RESOLVER, not a second reader: it asks the chats table first
// (through ChatShow, the same reader the list view's chat half uses) and the RUNS
// tree for the same id, then merges. A run's own recorded facts (pid, workdir,
// raw log) are attached to a chat row too when the row names a run this machine
// holds — which is how the page stops printing a dash for a fact the machine
// knows.
//
// NOTHING HERE INVENTS AN ID SPACE. The id is the operand `session list`
// printed: a chat's session_id, a run's session id, or a task/run id naming a
// directory in the sessions tree. Those spellings are one space; this file
// consumes it.

// SessionShow is one session's full fact set, whichever species it is. It is a
// UNION by Kind rather than two types because the show page prints ONE page: a
// reader must not have to know which source answered before they can read it.
type SessionShow struct {
	Kind      string
	SessionID string
	// Mode is the chat mode, or SessionModeRun.
	Mode   string
	Source string
	Model  string
	State  string
	// Started / LastWrite are the run's own start and the log's mtime (a chat
	// row's clock is in Chat.StartedAt / Chat.LastMsgAt).
	Started   time.Time
	HasStart  bool
	LastWrite time.Time
	// Workdir, LogPath, RawLogPath are the run's recorded paths; for a chat row
	// Workdir/LogPath come from the row's columns and are only filled from the
	// record when the row carries none.
	Workdir    string
	LogPath    string
	RawLogPath string
	GitBranch  string
	GitRepo    string
	// PID is the liveness STAMP (0 = none recorded) and Alive is what the probe
	// measured about it — two facts, never folded (Rule 15). It is the LAST
	// segment's stamp when the whole log was scanned (Record.LastWriterPID),
	// because a resumed session is written by a different process each run.
	PID   int
	Alive procstat.Verdict
	// Segments is how many `session_start` headers the WHOLE log holds (a
	// resume appends one per run), and SegmentsScanned says whether the log was
	// read end to end at all. It is the disclosure that lets a reader know the
	// pid/ended above describe the LAST segment while a `chats` row's
	// `ended_at` keeps the FIRST ending (Rule 15: state the span).
	Segments        int
	SegmentsScanned bool
	// SegmentWitness says whether the scanned headers SAY they were resumed —
	// `carrying` (at least one named the session it continued), `absent` (the
	// headers carry no witness: a producer with no resume identity writes such
	// a header, so this is NOT "never resumed", Rule 15a) or `not-counted` (no
	// whole-file scan ran).
	SegmentWitness rundir.SegmentWitness
	// WitnessedSegments is how many headers carried the witness, when the scan
	// found any (0 is a real, rendered value only beside `absent`).
	WitnessedSegments int
	// RecordedEnd is when the log's LAST session_end says the session
	// finished — a recorded instant, never derived from a row's close hook.
	RecordedEnd    time.Time
	HasRecordedEnd bool
	// RecordedEndReason / RecordedEndExit are the LAST verdict's cause and exit
	// code (RecordedEndExitOK says the code was carried: 0 is a real value).
	// They are the SESSION's ending; the chats row's end_reason keeps the FIRST
	// ending on purpose and is therefore a different fact.
	RecordedEndReason string
	RecordedEndExit   int
	RecordedEndExitOK bool
	// TaskID is the ledger row the run executed for ("" for a chat, a web
	// conversation, a one-shot prompt, or a run that recorded no row).
	TaskID string
	RunID  string
	// The RECORDED COST — what the session's own `session_end` verdicts said
	// about themselves, summed over the log's run segments. This is the ONE
	// input of the show page's cost block (internal/gopilog/FormatRecordedCost)
	// and it exists here because the alternative — re-summing the per-request
	// rows — is the defect the cost block moved here to remove.
	//
	// HasEndTotals distinguishes "the session recorded nothing" from "it
	// recorded zeroes": a live run, or one killed before its end hook, wrote no
	// verdict at all (Rule 15).
	HasEndTotals bool
	// EndPasses is session_end.total_iterations summed over segments;
	// EndTokens its total_tokens; EndElapsedSec its elapsed_sec.
	EndPasses     int
	EndTokens     int
	EndElapsedSec float64
	// Recorded says whether a run record was found and READ — i.e. whether
	// Workdir/PID/TaskID are a recording or an absence.
	Recorded bool
	// Chat is the chats row, when a chat row answered (zero value otherwise).
	Chat ChatSessionDetail
	// HasChat reports whether Chat is populated.
	HasChat bool
}

// SessionSourceShow resolves one session id across the chats table and the runs
// tree. The chats table is asked FIRST (it is the index, and a chat row carries
// columns a run log has not got); the runs tree answers when the table has no
// row, and its answer is still merged onto a chat row when one answered.
//
// An id neither source can resolve is a NAMED error naming what WAS searched
// (Rule 15: "no such session" and "nothing looked" must not look alike).
func SessionSourceShow(id string) (SessionShow, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return SessionShow{}, fmt.Errorf("session: show: id must not be empty")
	}
	show := SessionShow{SessionID: id}
	chatErr := error(nil)
	if d, err := ChatShow(id); err == nil {
		show.Kind = SessionKindChat
		show.Mode = d.Mode
		show.Source = d.Source
		show.Model = d.Model
		show.State = chatStateWord(d)
		show.Workdir = d.Workdir
		show.LogPath = d.LogPath
		show.GitBranch = d.GitBranch
		show.GitRepo = d.GitRepoRoot
		show.Chat, show.HasChat = d, true
	} else {
		chatErr = err
	}

	// THE RUNS HALF. The DIRECTORY lookup is always tried (it is a handful of
	// reads and covers every id `session list` prints for a run); the full tree
	// scan is the fallback for an id that names a RECORD inside a directory —
	// skipped when a chat row already answered, because then the log is known to
	// live in the directory that was just probed and scanning 2 982 directories
	// would only re-find it.
	rec, found := sessionRunByID(id, !show.HasChat)
	if found {
		show.Recorded = true
		show.RunID = rec.RunID
		show.Started, show.HasStart = rec.Started, rec.HasStart
		show.LastWrite = rec.LastWrite
		show.PID = rec.LastWriterPID()
		show.Alive = procstat.Alive(show.PID)
		show.Segments, show.SegmentsScanned = rec.Segments.Starts, rec.SegmentsScanned
		show.SegmentWitness, show.WitnessedSegments = rec.Segments.SegmentWitness, rec.Segments.WitnessedStarts
		show.RecordedEnd, show.HasRecordedEnd = rec.LastEndTime()
		show.RecordedEndReason = rec.Segments.LastEndReason
		show.RecordedEndExit, show.RecordedEndExitOK = rec.Segments.LastEndExit, rec.Segments.HasLastEndExit
		// THE RECORDED TOTALS, from the same whole-file scan. They are read
		// here, once, so no renderer downstream can re-derive them from rows.
		show.HasEndTotals = rec.Segments.HasEndTotals
		show.EndPasses = rec.Segments.EndPasses
		show.EndTokens = rec.Segments.EndTokens
		show.EndElapsedSec = rec.Segments.EndElapsedSec
		show.RawLogPath = rundir.ExistingRawLog(rec)
		show.TaskID = rec.TaskID
		fillIfEmpty(&show.Workdir, rec.Workdir)
		fillIfEmpty(&show.LogPath, rec.Path)
		fillIfEmpty(&show.Model, rec.Model)
		if !show.HasChat {
			show.Kind = SessionKindRun
			show.Mode = SessionModeRun
			show.State = sessionRunState(rec, show.Alive)
			show.SessionID = sessionRecordID(rec)
		}
	}

	if show.Kind == "" {
		// Neither source answered. The message names BOTH searches, so a reader
		// can tell a mistyped id from a machine that holds nothing.
		if chatErr != nil {
			return SessionShow{}, fmt.Errorf("session: show %s: not in the chat index and not in the runs tree (%w)", id, chatErr)
		}
		return SessionShow{}, fmt.Errorf("session: show %s: not in the runs tree", id)
	}
	// The git facts are MEASURED against the workdir whichever species answered:
	// a chat row's branch column is what the row recorded, while the directory on
	// disk is what git says TODAY. Neither overwrites the other — the page labels
	// the row's own column and this measurement apart.
	branch, repo := SessionRunGitFacts(show.Workdir)
	if show.GitBranch == "" {
		show.GitBranch = branch
	}
	if show.GitRepo == "" {
		show.GitRepo = repo
	}
	return show, nil
}

// fillIfEmpty sets *dst to v only when dst is blank — the merge rule for a value
// two sources can both answer (the chats row is the index, so it wins; the run
// record fills a column the row left empty). It is a function so the direction of
// precedence is stated once rather than a dozen `if x == ""` in the resolver.
func fillIfEmpty(dst *string, v string) {
	if strings.TrimSpace(*dst) == "" {
		*dst = v
	}
}

// sessionRecordID is the id a run record is addressed by, preferring the log's
// own session id and falling back to the run id.
func sessionRecordID(rec rundir.Record) string {
	if s := strings.TrimSpace(rec.SessionID); s != "" {
		return s
	}
	return rec.RunID
}

// sessionRunByID finds ONE run record by id. The DIRECTORY lookup is tried first
// because it is O(1) reads and covers every id `session list` prints for a run
// (the unified layout names the directory by the run/row id); the tree scan is
// the fallback for a record whose id differs from its directory name (a run
// directory holding several serial runs, a legacy layout), and only when the
// caller has no answer yet (allowTree).
func sessionRunByID(id string, allowTree bool) (rundir.Record, bool) {
	if dir := rundir.DirFor(id); dir != "" {
		if rec := pickShowRecord(rundir.HeadRecordsDir(dir), id); rec != nil {
			return *rec, true
		}
	}
	if !allowTree {
		return rundir.Record{}, false
	}
	root, err := runSessionsRoot()
	if err != nil {
		return rundir.Record{}, false
	}
	if rec := pickShowRecord(rundir.HeadRecordsTree(root).Records, id); rec != nil {
		return *rec, true
	}
	return rundir.Record{}, false
}

// pickRunRecord returns the NEWEST record among recs that the id names — by the
// record's session id, its run id, or the directory it lives in. It returns nil
// rather than the first record when nothing matches: handing a reader the wrong
// run is worse than the named refusal.
func pickRunRecord(recs []rundir.Record, id string) *rundir.Record {
	var best *rundir.Record
	for i := range recs {
		r := recs[i]
		if r.SessionID != id && r.RunID != id && dirBase(r.Dir) != id {
			continue
		}
		if best == nil || r.SortKey().After(best.SortKey()) {
			best = &recs[i]
		}
	}
	return best
}

// pickShowRecord is pickRunRecord's ONE-ROW form: it picks the record as the
// listing does and then re-reads that single file end to end (rundir.ShowRecord)
// so the segment stamp — how many segments, the LAST one's pid, the LAST
// verdict's time — is on the answer.
//
// WHY THE SECOND READ IS NOT WASTED WORK. The candidate scan must stay a bounded
// window (it walks a directory, or in the tree fallback ~2980 of them); the
// chosen file is ONE, and only the whole-file read can see the last
// `session_start` in a log whose segments are megabytes long (measured:
// 18601354 bytes between that header and the end). Finding the row twice is the
// price of keeping the search cheap — deliberately, not by accident.
func pickShowRecord(recs []rundir.Record, id string) *rundir.Record {
	best := pickRunRecord(recs, id)
	if best == nil {
		return nil
	}
	full := rundir.ShowRecord(best.Path)
	return &full
}

// dirBase is the last path element — filepath.Base without the import, because
// this is called once per record across a tree of thousands.
func dirBase(dir string) string {
	if i := strings.LastIndexByte(dir, '/'); i >= 0 {
		return dir[i+1:]
	}
	return dir
}
