// Package rundir is the single source of truth for a RUN's DIRECTORY — the one
// place that maps a run's id to the directory holding that run's logs, and the
// one place that enumerates the runs inside one of those directories.
//
// WHY IT EXISTS (epic run-identity-unify, 2026-09-20). Every gopi run is
// already named the same way — by an id that names its directory:
//
//	<gopi-dir>/sessions/<task_id>/<run-uuid>.jsonl   a ledger ROW's run
//	<gopi-dir>/sessions/<session_id>/session.jsonl   a bare prompt / chat run
//
// The ledger's `tasks.session_id` column is a SECOND, weaker pointer at the
// same run: it is 1:1 while the directory is 1:N (one task, several serial
// runs — a killed run plus its resume/retry), it is often the run that did NOT
// execute the row, and it is not an address at all (the directory is named by
// the run id, never by that column). `internal/agent/session.go` MINTS the
// directory from the run id (sessionDirForID); this package is the ADDRESS
// side, so every reader — internal/tools, internal/agent, cmd/agent — resolves
// it through ONE mapping instead of re-deriving it (PROJECT.md Rules 13/14).
//
// It is deliberately a LEAF: stdlib plus internal/config (where the gopi data
// root lives), internal/sessionlayout (which classifies the sessions tree's
// shapes) and internal/sessionevent (the event-type spellings). No business
// logic, no imports of project packages that could form a cycle — so a caller
// in any of those layers can depend on it.
package rundir

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/sessionevent"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
)

const (
	// runStartLineMaxBytes bounds the FIRST-LINE read of a run log: the
	// session_start event is always the first line (the logger writes and
	// syncs it before any other event), and it is small — the session's
	// workdir and raw-log path are the largest things it carries. A window
	// larger than this would only be paid by a corrupt file.
	runStartLineMaxBytes = 1 << 20

	// runScannerBufBytes is the scanner's initial read buffer: a session log
	// line is a whole JSON event, so the scanner must GROW with a line rather
	// than cap the read at bufio's default 64KiB (a large tool_result line is
	// routine). The cap that stops the growth is runStartLineMaxBytes below.
	runScannerBufBytes = 1 << 16

	// runHintTailBytes bounds the TAIL read that answers "did this run
	// finish, and how did it end". Same window as the sibling per-row probe
	// (internal/tools taskStateSessionTailBytes, 1MB) for the same reason:
	// session_end is the LAST event a finished session writes, and an
	// unfinished run's most recent progress event sits within a few events
	// of the end. A window is read, never the whole file — a 50MB session
	// must not be slurped to render one ledger row.
	runHintTailBytes int64 = 1 << 20

	// stateTailBytes is the SHALLOW window HeadRecordsDir reads: enough for a
	// terminal session_end line (plus the fragment the seek may cut), small
	// enough that a listing over the whole sessions tree stays a listing.
	// Measured on this profile: 1967 records × 1 MiB of verdict window is the
	// ~2 GB read runHintTailBytes would cost a LISTING; this one is ~32 MiB.
	// The number is not a quality trade — a caller that renders a run's TOTALS
	// uses RecordsDir (runHintTailBytes) and checks ScaleTrusted; this window
	// answers only "is there a verdict at all".
	stateTailBytes int64 = 1 << 15
)

// Root returns the sessions root — <gopi dir>/sessions — as every run's
// directory's parent. The gopi dir is config.GopiDir()'s answer
// ($GOPI_HOME/.gopi when set, else ~/.gopi), the SAME resolution
// internal/agent's sessionDirForID uses to mint the directory, so the writer
// and this reader cannot disagree about where runs live.
func Root() (string, error) {
	return config.SessionsRoot()
}

// RootAt is Root for an explicit gopi data dir (the $GOPI_HOME value, not the
// .gopi dir under it) — the seam tests use to drive a fixture tree without
// mutating the process environment.
func RootAt(gopiDir string) string {
	return filepath.Join(gopiDir, config.SessionsDirName)
}

// Dir returns the run directory of runID under an explicit gopi data dir. The
// directory may not exist yet (a run that has not started) — the caller
// decides whether that absence is meaningful.
func Dir(gopiDir, runID string) string {
	return filepath.Join(RootAt(gopiDir), strings.TrimSpace(runID))
}

// DirFor is Dir with the gopi data dir resolved from config — the ordinary
// production spelling.
func DirFor(runID string) string {
	root, err := Root()
	if err != nil {
		return ""
	}
	return filepath.Join(root, strings.TrimSpace(runID))
}

// Record is ONE run log of a run directory, with the facts the ledger views
// need read off it: which run it is, when it started, whether it finished, and
// how it ended. Nothing here is guessed — a field the log does not carry stays
// zero (HasStart / HasEnd say whether it was seen at all), because an absence
// must render as an absence (PROJECT.md Rule 15).
type Record struct {
	// Path is the run log's own path — the run's audit trail.
	Path string
	// Dir is the directory holding it (the run directory).
	Dir string
	// RunID is the run's own id: the log's base name without its suffix for
	// the task-run layout (<run-uuid>.jsonl), and the DIRECTORY name for the
	// bare layout (whose single record is fixed-named session.jsonl, so only
	// the directory names the run).
	RunID string
	// SessionID / RawLogPath / Model / Workdir come from the log's
	// session_start event; "" when the file carries no such event (a torn or
	// non-session jsonl).
	SessionID  string
	RawLogPath string
	Model      string
	// Workdir is the directory the run executed in, as its own session_start
	// recorded it — the fact `gopi session show <id>` needs to answer "where
	// was this run?" without asking the ledger (which carries the row's repo,
	// not the run's cwd).
	Workdir string
	// WriterPID is the liveness stamp from the same event (0 for a record that
	// predates the stamp, or one whose start line could not be read). It is a
	// STAMP, not a claim: whether that process is still alive is a separate
	// probe (internal/procstat), and a caller that renders it must say which of
	// the two it measured (PROJECT.md Rule 15).
	WriterPID int
	// TaskID is the ledger row this run executed for, as the run's own
	// session_start recorded it ("" for a chat, a web conversation or a
	// one-shot prompt — those runs have no row, and that absence is a fact).
	//
	// It is read from the LOG, not inferred from the directory name: the
	// unified layout makes the two agree, but a directory written before the
	// unification (or by a caller that names its own dir) carries a run uuid
	// while its log still names the row, so the log is the authority and the
	// directory is a second, weaker witness.
	TaskID string
	// Started is the session_start timestamp; HasStart reports whether it was
	// seen and parsed (a run mid-creation may not have written it yet).
	Started  time.Time
	HasStart bool
	// LastWrite is the log file's mtime — the run's last recorded activity.
	// It is the ordering fallback for a log whose session_start could not be
	// read, never a liveness claim (the log is written by the run, and a
	// finished run keeps its final mtime).
	LastWrite time.Time
	// HasEnd / ElapsedSec / Turns / Tokens / ExitCode come from the session_end
	// event in the log's tail. Turns falls back to the LAST request event's
	// iteration number, so a still-running log still reports its progress
	// honestly ("turns so far"), with HasEnd=false marking it unfinished.
	HasEnd     bool
	ElapsedSec float64
	Turns      int
	Tokens     int
	// StartSegments is how many `session_start` headers the READ saw. It is
	// the segment BOUNDARY marker: one header means the read began inside the
	// run's only segment, two or more means the log was resumed, and a single
	// header inside a TRUNCATED read means at least one segment's tail is
	// outside the window. It is counted because a resumed log's second header
	// is proof of a second segment even when its `session_end` was cut off.
	StartSegments int
	// EndSegments is HOW MANY `session_end` events the READ saw — 0 (no
	// verdict), 1 (the ordinary finished run) or MORE, which is the shape a
	// `gopi session resume` leaves behind: the killed run's verdict AND the
	// resumed segment's, appended in that order. A tail read is
	// last-writer-wins, so with two segments Turns/Tokens/ElapsedSec describe
	// the LAST segment only — the resumed segment routinely reports 0 passes
	// of its own, and printing that as "the run finished 0 passes" is a lie
	// the reader cannot detect. Reporting the COUNT is how the record says
	// which segment its numbers came from.
	EndSegments int
	// TailTruncated reports that the read did NOT begin at the log's first
	// byte: the window is bounded (runHintTailBytes), so a segment that ended
	// before it is invisible here. It says nothing about what was missed —
	// only that something may have been, which is why it is a separate fact
	// from EndSegments rather than folded into it.
	TailTruncated bool
	// ScaleTrusted reports whether Turns/Tokens/ElapsedSec describe the RUN or
	// merely its last visible segment. True exactly when the whole log was
	// read (nothing truncated) AND it holds exactly one `session_end`: only
	// then is the terminal block the run's own total. It is NOT a claim that
	// the numbers are complete — a caller that renders them under a
	// run-total word must check it (internal/tools/taskrunlog.go does, and
	// prints a dash instead of a candidate number when it is false).
	ScaleTrusted bool
	// HasExit reports whether session_end carried an exit code (ExitCode is
	// then meaningful; 0 is a real value).
	HasExit  bool
	ExitCode int

	// Segments is the WHOLE-FILE segment stamp: how many headers and verdicts
	// the log holds, and the LAST of each. It is filled only by the one-row
	// read (ShowRecord), never by the bounded listing windows — a listing walks
	// thousands of logs and a whole-file scan there would turn it into a slurp.
	//
	// SegmentsScanned says whether it was filled at all, so "this log holds one
	// segment" and "nothing scanned this log" cannot look alike (Rule 15).
	Segments        SegmentStamp
	SegmentsScanned bool
}

// LastWriterPID is the liveness stamp of the process that wrote the log's
// TAIL — the LAST segment's `session_start` pid when the whole file was
// scanned, else the header pid the bounded read got. It is the stamp a resumed
// session's `session show` must print: WriterPID alone is the FIRST segment's
// process, which for a session resumed three times has been gone for days
// (measured: session 66243525-16ab-4ce7-9133-84d74c539840).
func (r Record) LastWriterPID() int {
	if r.SegmentsScanned && r.Segments.Starts > 0 {
		return r.Segments.LastStartPID
	}
	return r.WriterPID
}

// LastEndTime is when the log's LAST segment ended — the instant a resumed
// session's `ended` reports, and a different fact from a `chats` row's
// `ended_at` (a close hook's stamp that keeps the FIRST ending by design).
// ok is false when no verdict time was recorded anywhere (an unscanned log, a
// log with no session_end, or one written before the timestamp was carried).
func (r Record) LastEndTime() (t time.Time, ok bool) {
	if r.SegmentsScanned && r.Segments.HasLastEnd {
		return r.Segments.LastEndTime, true
	}
	if r.HasEnd && !r.Started.IsZero() && r.ElapsedSec > 0 {
		// The bounded read saw a verdict but no timestamp: the segment's own
		// start plus its elapsed time is the same instant, DERIVED rather than
		// recorded. It is offered only when both operands were read.
		return r.Started.Add(time.Duration(r.ElapsedSec * float64(time.Second))), true
	}
	return time.Time{}, false
}

// sortKey orders runs: the start time when the log carries one (the run's own
// beginning — what "latest run" means for a serial re-run), else the log's
// last write (the best available proxy for a log whose first line is
// unreadable). Ties break on Path so the order is deterministic for two runs
// that began in the same second.
func (r Record) sortKey() time.Time {
	if r.HasStart {
		return r.Started
	}
	return r.LastWrite
}

// SortKey is sortKey's exported form: the instant a run is ordered by (its
// session_start when the log carries one, else the log's mtime). A caller that
// must pick the NEWEST of several records for one id (a directory holding a
// killed run and its resume) reads this rather than re-deriving the fallback —
// one definition of "which run is later" (Rule 13/14).
func (r Record) SortKey() time.Time { return r.sortKey() }

// Records enumerates the run logs held by one run directory, newest LAST, and
// reads each one's session_start + terminal facts. A dir that does not exist
// is not an error — it is an empty list (the caller distinguishes "no run
// directory" from "a run directory with no run log" by calling Exists).
//
// The classification is internal/sessionlayout's, never a glob: a run's side
// files (signal-provenance.jsonl) are artifacts of the SAME run and must never
// be counted as a second one, and a BARE session dir holds exactly one record
// however many artifacts sit beside it.
func Records(gopiDir, runID string) []Record {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil
	}
	return RecordsDir(Dir(gopiDir, runID))
}

// RecordsDir is Records for an already-resolved directory — the form a caller
// that holds the path (a test fixture, a resume hint) uses.
func RecordsDir(dir string) []Record {
	subs, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	// A BARE session dir holds ONE session: its fixed-named record is the
	// only run log, and any other jsonl beside it is an artifact of that run
	// (the unregistered ones are reported elsewhere, never counted here).
	bare := sessionlayout.HasBareRecord(dir)
	var out []Record
	for _, se := range subs {
		name := se.Name()
		if se.IsDir() || !sessionlayout.IsJSONL(name) || sessionlayout.IsSideFile(name) {
			continue
		}
		if bare && name != sessionlayout.RecordName {
			continue
		}
		out = append(out, readRecord(dir, name, bare))
	}
	sort.SliceStable(out, func(i, j int) bool {
		ki, kj := out[i].sortKey(), out[j].sortKey()
		if !ki.Equal(kj) {
			return ki.Before(kj)
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// readRecord reads one run log's two decisive lines: its first (session_start)
// and its tail (session_end, else the last request event's progress).
func readRecord(dir, name string, bare bool) Record {
	path := filepath.Join(dir, name)
	rec := Record{Path: path, Dir: dir, RunID: runIDOf(dir, name, bare)}
	if fi, err := os.Stat(path); err == nil {
		rec.LastWrite = fi.ModTime()
	}
	if line := firstLine(path); line != nil {
		rec.applyStart(line)
	}
	if tail, truncated := tailBytes(path, runHintTailBytes); tail != nil {
		rec.applyTail(tail, truncated)
	}
	if rec.RunID == "" {
		// The log named no session and the layout names no run (an
		// unreadable task-run file): the file's own basename is still the
		// run's identity on disk.
		rec.RunID = strings.TrimSuffix(name, sessionlayout.JSONLSuffix)
	}
	return rec
}

// runIDOf is the run's id for one record: the bare layout names the run by its
// DIRECTORY (its record has a fixed name), the task-run layout by the log's
// own file name.
func runIDOf(dir, name string, bare bool) string {
	if bare {
		return filepath.Base(dir)
	}
	return strings.TrimSuffix(name, sessionlayout.JSONLSuffix)
}

// The session log's own event shapes this package reads (Rule 1b: the field
// names live once, so the start read and the tail read cannot drift apart).
type startEvent struct {
	Type       string `json:"type"`
	SessionID  string `json:"sessionId"`
	RawLogPath string `json:"rawLogPath"`
	Model      string `json:"model"`
	Timestamp  string `json:"timestamp"`
	Workdir    string `json:"workdir"`
	// PID is the liveness stamp — the key internal/agent writes it under
	// (agent.ProcessAliveStampField). It is spelled HERE as the wire name,
	// which is the same value: the field tag is what the writer emits, and a
	// reader that typed the key twice could drift from it. The package-level
	// assertion in rundir_test.go pins the two equal.
	PID int `json:"pid"`
	// TaskID is the ledger row this run was dispatched for — the key
	// internal/agent writes as `start["taskId"]`, and the ONE field that lets a
	// reader go from a run directory back to its row (and the reverse: the row's
	// id is the run directory's name in the unified layout). Absent for a chat,
	// a web conversation or a one-shot prompt, which is a fact, not a gap.
	TaskID string `json:"taskId"`
	// ResumedFrom / ResumedFromRawLog are the RESUME WITNESS: the session this
	// run continued, and the raw log it appended to. The wire names are the
	// values of sessionevent.SessionStartFieldResumedFrom /
	// …SessionStartFieldResumedFromRawLog (typed here because a struct tag
	// cannot reference a constant; the package-level assertion in
	// rundir_segments_witness_test.go pins each tag equal to its constant,
	// Rule 13).
	//
	// BOTH ARE ABSENT on the first run of a session — and, as of this reading,
	// ALSO absent on every header written by a producer that holds no resume
	// identity (a task run retried in place appends a header it cannot witness).
	// So "no witness on this header" says something about the WRITER, never that
	// no resume happened; SegmentStamp.SegmentWitness keeps the two apart
	// (Rule 15a: an absence is not a false).
	ResumedFrom       string `json:"resumedFrom"`
	ResumedFromRawLog string `json:"resumedFromRawLog"`
}

type endEvent struct {
	Type            string  `json:"type"`
	ElapsedSec      float64 `json:"elapsed_sec"`
	TotalIterations int     `json:"total_iterations"`
	TotalTokens     int     `json:"total_tokens"`
	ExitCode        *int    `json:"exit_code"`
	// Timestamp is WHEN this run segment ended — the fact a resumed session's
	// `ended` must report from its LAST verdict (a `chats` row's ended_at is a
	// close hook's stamp and keeps the FIRST ending, so the two are different
	// facts and this record must not borrow the row's). Absent on a log written
	// before the field existed, which every reader treats as "no time
	// recorded" rather than as a zero instant (PROJECT.md Rule 15).
	Timestamp string `json:"timestamp"`
	// Reason is HOW this segment ended (normal|interrupt|crash|quit|eof|
	// signalled|unknown — sessionevent's vocabulary). It is the LAST segment's
	// cause that describes how the SESSION finished; the `chats` row's
	// end_reason is a close hook's stamp and keeps the FIRST ending by design.
	// A log written by a binary that predates the key carries none, which is
	// read as "not recorded" (Rule 15) — spelled the same way `startEvent`'s
	// `pid` is (a struct tag cannot hold a constant; rundir_test.go pins it to
	// sessionevent.SessionEndFieldReason).
	Reason string `json:"reason"`
}

type requestEvent struct {
	Type      string `json:"type"`
	Iteration int    `json:"iteration"`
}

// applyStart parses the session_start line into the record.
func (r *Record) applyStart(line []byte) {
	var ev startEvent
	if err := json.Unmarshal(line, &ev); err != nil {
		return
	}
	if ev.Type != sessionevent.EventTypeSessionStart {
		return
	}
	r.SessionID = strings.TrimSpace(ev.SessionID)
	r.RawLogPath = strings.TrimSpace(ev.RawLogPath)
	r.Model = strings.TrimSpace(ev.Model)
	r.Workdir = strings.TrimSpace(ev.Workdir)
	r.WriterPID = ev.PID
	r.TaskID = strings.TrimSpace(ev.TaskID)
	if ts, err := time.Parse(time.RFC3339Nano, ev.Timestamp); err == nil {
		r.Started = ts
		r.HasStart = true
	}
}

// applyTail parses the tail window: session_end (the run's verdict) when it is
// there, else the LAST request event's iteration as the progress-so-far turn
// count. Whole lines only — the window's first line is a fragment a mid-file
// seek may have cut.
func (r *Record) applyTail(tail []byte, truncated bool) {
	r.TailTruncated = truncated
	// Whole lines only: a read that began past byte 0 may have cut the first
	// line in half, so it is dropped. A read that began AT byte 0 has no cut
	// fragment — its first line is a whole line, and dropping it would lose
	// whatever that line says (a log whose only line is a verdict).
	start := 0
	if truncated {
		i := bytes.IndexByte(tail, '\n')
		if i < 0 {
			return
		}
		start = i + 1
	}
	lastReqIter := -1
	sc := bufio.NewScanner(bytes.NewReader(tail[start:]))
	sc.Buffer(make([]byte, 0, runScannerBufBytes), runStartLineMaxBytes)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		// Two cheap pre-filters before decoding: a log line is a JSON object
		// and the type field is the only thing this pass reads.
		switch {
		case bytes.Contains(line, []byte(sessionevent.EventTypeSessionEnd)):
			var ev endEvent
			if err := json.Unmarshal(line, &ev); err == nil &&
				ev.Type == sessionevent.EventTypeSessionEnd {
				r.HasEnd = true
				r.EndSegments++
				r.ElapsedSec = ev.ElapsedSec
				r.Turns = ev.TotalIterations
				r.Tokens = ev.TotalTokens
				if ev.ExitCode != nil {
					r.HasExit = true
					r.ExitCode = *ev.ExitCode
				}
			}
		case bytes.Contains(line, []byte(sessionevent.EventTypeSessionStart)):
			var ev startEvent
			if err := json.Unmarshal(line, &ev); err == nil &&
				ev.Type == sessionevent.EventTypeSessionStart {
				r.StartSegments++
			}
		case bytes.Contains(line, []byte(requestEventType)):
			var ev requestEvent
			if err := json.Unmarshal(line, &ev); err == nil && ev.Type == requestEventType {
				lastReqIter = ev.Iteration
			}
		}
	}
	// The terminal block describes the RUN only when the read saw the whole
	// log AND the run wrote one verdict. Two segments (a resume) make every
	// field of it the LAST segment's own, and a truncated read cannot rule
	// out a segment that ended outside the window — either way the number is
	// a candidate, never a total, and saying so is this field's whole job.
	r.ScaleTrusted = r.HasEnd && r.EndSegments == 1 && r.StartSegments <= 1 && !truncated
	if !r.HasEnd && lastReqIter > 0 {
		// Unfinished run: its own progress event is the honest turn count so
		// far (the log has no terminal total to read). HasEnd stays false, so
		// the caller renders it as unfinished, never as a completed run.
		r.Turns = lastReqIter
	}
}

// requestEventType is the session log's per-round API event — the progress
// marker an unfinished run's turn count is read from. It lives in
// internal/gopilog's own vocabulary (RawEvent's "request" case) and is spelled
// once here (Rule 1b) rather than typed inline at the parse site.
const requestEventType = "request"

// HeadRecordsDir is RecordsDir's CHEAP sibling: it reads each run log's
// session_start line and mtime (identity, workdir, liveness stamp, start time)
// and a bounded STATE window at the end, and NOT the whole 1 MiB verdict tail.
//
// WHY IT EXISTS (measured on this host, 2026-10-28). The sessions tree holds
// 1967 records; readRecord's verdict window is runHintTailBytes (1 MiB), so
// RecordsDir over the tree is a ~2 GB read to answer a LISTING. A listing needs
// identity plus "is this run still going", and the second question is answered
// by the liveness stamp (a /proc probe) and by whether a session_end exists
// within a few KiB of the end — not by the run's token totals.
//
// It is the SAME classification and the SAME sort as RecordsDir (this delegates
// the layout rule to sessionlayout, never to a glob), so the two cannot
// disagree about which files are runs; only the read depth differs, which is
// why the cheap one is named for what it gives up.
func HeadRecordsDir(dir string) []Record {
	subs, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	bare := sessionlayout.HasBareRecord(dir)
	var out []Record
	for _, se := range subs {
		name := se.Name()
		if se.IsDir() || !sessionlayout.IsJSONL(name) || sessionlayout.IsSideFile(name) {
			continue
		}
		if bare && name != sessionlayout.RecordName {
			continue
		}
		out = append(out, headRecord(dir, name, bare))
	}
	sort.SliceStable(out, func(i, j int) bool {
		ki, kj := out[i].sortKey(), out[j].sortKey()
		if !ki.Equal(kj) {
			return ki.Before(kj)
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// HeadRecords is HeadRecordsDir for one run id under an explicit gopi data dir
// (the seam tests drive a fixture tree with).
func HeadRecords(gopiDir, runID string) []Record {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil
	}
	return HeadRecordsDir(Dir(gopiDir, runID))
}

// headRecord is readRecord without the verdict tail: the first line plus the
// mtime, then a bounded STATE window (stateTailBytes) that is large enough to
// hold a run's session_end and far too small to be a scan of a 50 MB log.
func headRecord(dir, name string, bare bool) Record {
	path := filepath.Join(dir, name)
	rec := Record{Path: path, Dir: dir, RunID: runIDOf(dir, name, bare)}
	if fi, err := os.Stat(path); err == nil {
		rec.LastWrite = fi.ModTime()
	}
	if line := firstLine(path); line != nil {
		rec.applyStart(line)
	}
	if tail, truncated := tailBytes(path, stateTailBytes); tail != nil {
		rec.applyTail(tail, truncated)
	}
	if rec.RunID == "" {
		rec.RunID = strings.TrimSuffix(name, sessionlayout.JSONLSuffix)
	}
	return rec
}

// HeadRecord is headRecord for an already-resolved record path — the form a
// caller that found the file by id uses (it holds a path, not a dir + name).
func HeadRecord(path string) Record {
	dir, name := filepath.Dir(path), filepath.Base(path)
	return headRecord(dir, name, name == sessionlayout.RecordName)
}

// ShowRecord is HeadRecord PLUS the whole-file segment stamp — the one-row read
// `gopi session show <id>` needs, and the only caller that can afford it (it
// asks about ONE file it already resolved).
//
// WHY THE LISTING KEEPS ITS WINDOWS. headRecord reads the log's first line and
// a 32 KiB tail; that is the right shape for a walk over thousands of logs and
// the wrong shape for this question: measured on session
// 66243525-16ab-4ce7-9133-84d74c539840 (21398592 bytes, three segments), the
// LAST `session_start` is 18601354 bytes from the end — 567 windows past a
// 32 KiB tail and 17 past a 1 MiB one — so a bounded read prints the FIRST
// segment's pid no matter how wide the window is made.
func ShowRecord(path string) Record {
	rec := HeadRecord(path)
	stamp, ok := ScanSegments(path)
	if !ok {
		// An unreadable/unwalkable file keeps the bounded answer and SAYS the
		// scan did not happen: every derived field above then reports the first
		// segment, which is what was actually read (Rule 15).
		return rec
	}
	rec.Segments, rec.SegmentsScanned = stamp, true
	return rec
}

// TreeHead is the answer of ONE root-level scan: every run record the tree
// holds, plus the counts that make the scan's coverage a published fact rather
// than something a reader has to assume (PROJECT.md Rule 15 — "no rows" and
// "nothing was scanned" must not look alike).
type TreeHead struct {
	// Records is every run log found under the root, one entry per record
	// file (a run directory may hold several — a serial re-run appends one).
	Records []Record
	// Dirs is how many directories the scan entered.
	Dirs int
	// RecordlessDirs is how many of them held no run log at all: a session
	// directory whose log was swept, a container whose only child is another
	// directory, a directory of run by-products (measured on this host
	// 2026-10-28: 2980 directories, 1817 records, 1517 recordless).
	RecordlessDirs int
}

// HeadRecordsTree scans a sessions ROOT and returns every run it holds.
//
// ADMISSION IS THE RECORD'S OWN FACT, never the directory's name. A directory
// is scanned whatever it is called and contributes rows exactly when it holds a
// file sessionlayout recognises as a run log — so a run filed under a name this
// package has never heard of is still listed, and an empty directory is simply
// counted. Refusing by name would drop precisely the runs whose naming went
// wrong; the writer that mints a bad name is a bug elsewhere, and a READER must
// not inherit it.
func HeadRecordsTree(root string) TreeHead {
	root = strings.TrimSpace(root)
	if root == "" {
		return TreeHead{}
	}
	subs, err := os.ReadDir(root)
	if err != nil {
		return TreeHead{}
	}
	th := TreeHead{}
	for _, se := range subs {
		if !se.IsDir() {
			continue
		}
		th.Dirs++
		recs := HeadRecordsDir(filepath.Join(root, se.Name()))
		if len(recs) == 0 {
			th.RecordlessDirs++
			continue
		}
		th.Records = append(th.Records, recs...)
	}
	return th
}

func Latest(recs []Record) (Record, bool) {
	if len(recs) == 0 {
		return Record{}, false
	}
	return recs[len(recs)-1], true
}

// WithCheckpoint returns the NEWEST record that carries a resolvable raw log —
// the only kind of run a `gopi session resume` can continue. ok is false when no run
// of the directory has one (the caller reports that as a definite absence
// rather than falling back to a weaker guess, Rule 15).
func WithCheckpoint(recs []Record) (Record, bool) {
	for i := len(recs) - 1; i >= 0; i-- {
		if ExistingRawLog(recs[i]) != "" {
			return recs[i], true
		}
	}
	return Record{}, false
}

// ExistingRawLog resolves a record's raw-log checkpoint: the path recorded in
// its session_start, VERBATIM (authoritative — it survives custom
// GOPI_RAW_LOG_DIR setups static resolution cannot reconstruct), else the same
// BASENAME beside the run log (the re-home sweep moves a raw log into its
// run's directory while the record keeps naming it by basename). Returns ""
// when neither exists — a definite "no checkpoint", never a guess.
func ExistingRawLog(r Record) string {
	if p := strings.TrimSpace(r.RawLogPath); p != "" {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
		if r.Path != "" {
			sib := filepath.Join(sessionlayout.RecordContainer(r.Path), filepath.Base(p))
			if sib != p {
				if fi, err := os.Stat(sib); err == nil && !fi.IsDir() {
					return sib
				}
			}
		}
	}
	return ""
}

// Exists reports whether a run directory is on disk — the difference between
// "this row never ran" and "this row ran and its logs are gone", which a
// ledger view must not blur.
func Exists(dir string) bool {
	fi, err := os.Stat(strings.TrimSpace(dir))
	return err == nil && fi.IsDir()
}

// RunDirRawLog returns the raw-log checkpoint that lives INSIDE a run
// DIRECTORY: sessions/<run-id>/<what>.raw.log. Newest first when the directory
// holds more than one (a serial re-run leaves one per run — the same "newest
// is the run a resume continues" ordering WithCheckpoint uses). "" is a
// definite absence: no directory, no co-located raw log.
//
// WHY IT IS A DIRECTORY READ, NOT A RECORD READ (epic resume-target-surface,
// 2026-09-26; measured on this host: 274 raw logs sit inside session
// directories). The session log's session_start event records a rawLogPath,
// and ExistingRawLog resolves the checkpoint from THAT record — which requires
// reading a session log, and a session log is written by the RUN PATH. A run
// directory holding ONLY its raw log (the interrupted/first-round shape, and
// the shape every isolated profile produces) therefore has a checkpoint no
// record-based lookup can find: RecordsDir lists no run at all, so a
// well-formed target falls through to session-id resolution and is reported
// unresolvable while its checkpoint sits one directory away. The file's own
// location is the address, and the suffix is the DECLARED one
// (scratch.SessionRawLogFileSuffix — the value the writer that mints these
// files builds the name from), so writer and reader cannot drift.
//
// The run id is deliberately NOT matched against the file's basename: a ledger
// row's directory is named by the TASK id while its raw logs are named by the
// RUN's uuid (sessions/t-1788721024/4411022b-....raw.log, measured), so a
// basename match would refuse exactly the layout this resolver is for.
func RunDirRawLog(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	subs, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	newest, newestMod := "", time.Time{}
	for _, se := range subs {
		name := se.Name()
		if !strings.HasSuffix(name, scratch.SessionRawLogFileSuffix) {
			continue
		}
		path := filepath.Join(dir, name)
		fi, err := os.Stat(path)
		if err != nil || !fi.Mode().IsRegular() {
			// A directory named like a raw log, or a file that vanished
			// between the listing and the stat, is not a checkpoint.
			continue
		}
		if newest == "" || fi.ModTime().After(newestMod) {
			newest, newestMod = path, fi.ModTime()
		}
	}
	return newest
}

// firstLine returns a file's first line (nil when unreadable or empty).
func firstLine(path string) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, runScannerBufBytes), runStartLineMaxBytes)
	if !sc.Scan() {
		return nil
	}
	return append([]byte(nil), sc.Bytes()...)
}

// tailBytes returns the last maxBytes of a file (nil when unreadable). The
// window may begin mid-line; callers tolerate the fragment.
// tailBytes reads at most maxBytes of the file's END. truncated reports that
// the read began past byte 0 — the caller must know, because a bounded window
// is an INCOMPLETE view of the log and a record that hid that would present a
// partial scan as a whole one (PROJECT.md Rule 15).
func tailBytes(path string, maxBytes int64) (data []byte, truncated bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, false
	}
	off := fi.Size() - maxBytes
	if off < 0 {
		off = 0
	}
	if _, err := f.Seek(off, 0); err != nil {
		return nil, false
	}
	buf := make([]byte, fi.Size()-off)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return nil, false
	}
	return buf[:n], off > 0
}
