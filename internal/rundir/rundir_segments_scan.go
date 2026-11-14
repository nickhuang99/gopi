package rundir

// rundir_segments_scan.go — WHICH SEGMENT A RESUMED LOG ENDED IN, AND WHOSE PID.
//
// THE DEFECT THIS CLOSES. `applyStart` parses the log's FIRST line only, so
// every record carried the FIRST segment's `pid` stamp; `applyTail` reads a
// bounded window from the END, so it sees the last `session_end` but no header
// at all once a segment is longer than the window. Measured on session
// 66243525-16ab-4ce7-9133-84d74c539840 (21398592 bytes, three segments): the
// last `session_start` sits 18601354 bytes from the end, i.e. 17.7 windows away
// from a 1 MiB tail read. So `gopi session show` printed the pid of a process
// that had been gone for days while the session's real last writer was a
// different process entirely.
//
// WHY A WHOLE-FILE SCAN IS NOT A REGRESSION FOR THE LISTING. Every listing path
// (RecordsDir / HeadRecordsDir) keeps its bounded windows: those walks read
// thousands of logs and a full scan there would turn a listing into a slurp.
// This scan exists for the ONE-ROW question (`session show <id>`), where the
// caller already knows which single file it is asking about.
//
// WHAT IT DOES NOT DO: it does not decide liveness (internal/procstat does), it
// does not interpret a verdict (exit codes, reasons), and it never invents a
// value a log does not carry — `HasLastStart` / `HasLastEnd` say whether the
// event was seen at all (PROJECT.md Rule 15: an absence renders as an absence).

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"time"

	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// The two event names this scan looks for. They reference the OWNER's
// constants (Rule 13: a value is referenced, never re-typed), and they are the
// cheap pre-filter before a line is decoded — a log line is a JSON object and
// `type` is the only field this pass reads.
var (
	segmentStartProbe = []byte(sessionevent.EventTypeSessionStart)
	segmentEndProbe   = []byte(sessionevent.EventTypeSessionEnd)
)

// SegmentStamp is what a WHOLE-FILE scan of one run log says about its run
// segments: how many headers and verdicts it holds, and the LAST of each. The
// last start's PID is the liveness stamp that matters (the process that wrote
// the tail), and the last end's TIMESTAMP is when the conversation really
// finished — a `chats` row's `ended_at` is stamped by a CLOSE HOOK and keeps
// the FIRST ending, so it is not that fact for a resumed session.
type SegmentStamp struct {
	// Starts / Ends count the events the scan saw, over the whole file.
	Starts int
	Ends   int
	// LastStartPID is the `pid` of the LAST session_start (0 when no header
	// carried the stamp — a log written before the field existed).
	LastStartPID int
	// LastStartTime is the LAST header's timestamp.
	LastStartTime time.Time
	HasLastStart  bool
	// LastEndTime is the LAST session_end verdict's timestamp.
	LastEndTime time.Time
	HasLastEnd  bool
	// LastEndReason is the LAST verdict's recorded cause ("" when the log's
	// writer predates the key — read as "not recorded", never as normal).
	LastEndReason string
	// LastEndExit / HasLastEndExit are the LAST verdict's exit code and
	// whether it carried one (0 is a real value, Rule 15a).
	LastEndExit    int
	HasLastEndExit bool
	// WitnessedStarts is how many headers CARRIED a resume witness
	// (`resumedFrom`), and SegmentWitness says whether that number answers a
	// question at all. The two exist because "this log holds no witness" and
	// "this log holds three headers and NONE of them says why" are different
	// statements, and a reader that folded them would report an unwitnessed
	// resume as no resume (Rule 15: an absence renders as an absence).
	WitnessedStarts int
	SegmentWitness  SegmentWitness

	// EndElapsedSec / EndPasses / EndTokens are the RECORDED totals of every
	// `session_end` the scan saw, SUMMED over the segments — the numbers the
	// run itself wrote when it finished, never a re-derivation from the
	// per-request rows.
	//
	// WHY A SUM AND NOT THE LAST VERDICT'S. A resumed log holds one
	// `session_end` per run segment, and each reports only ITS OWN run, so the
	// segment that ran no turn writes `total_iterations: 0` and would erase the
	// passes an earlier segment recorded (the identical defect
	// internal/gopilog's reader repairs — Session.TotalIterations, task
	// 01a17ae2). The span of these three fields is therefore the WHOLE LOG,
	// the same span that reader reports, so `session show` and `session log`
	// cannot disagree about the session's recorded cost.
	//
	// HasEndTotals says whether any verdict was read at all: a log with none
	// (a live run, a kill before the end hook) reports NO totals, and every
	// render must state that absence rather than print a 0 (PROJECT.md Rule 15).
	EndElapsedSec float64
	EndPasses     int
	EndTokens     int
	HasEndTotals  bool
}

// SegmentWitness is the three-state answer to "does this log SAY it was
// resumed?" — never a bool, because the honest answers are three.
type SegmentWitness int

const (
	// SegmentWitnessNotCounted is the zero value: no whole-file scan ran, or
	// the log holds no header to carry a witness. It must render as "not
	// counted", never as "was not resumed".
	SegmentWitnessNotCounted SegmentWitness = iota
	// SegmentWitnessAbsent means headers WERE scanned and not one carried a
	// resume witness. That is a fact about the WRITERS (a producer with no
	// resume identity appends a header it cannot witness — measured on every
	// task-run retry), NOT a claim that the session was never resumed.
	SegmentWitnessAbsent
	// SegmentWitnessCarrying means at least one header named the session it
	// continued. Only this state lets a reader SAY "resumed".
	SegmentWitnessCarrying
)

// String is the word each state renders as. The three spellings are constants
// because the human page and the JSON face must agree (Rule 1/13).
func (w SegmentWitness) String() string {
	switch w {
	case SegmentWitnessCarrying:
		return segmentWitnessWordCarrying
	case SegmentWitnessAbsent:
		return segmentWitnessWordAbsent
	}
	return segmentWitnessWordNotCounted
}

const (
	segmentWitnessWordCarrying   = "carrying"
	segmentWitnessWordAbsent     = "absent"
	segmentWitnessWordNotCounted = "not-counted"
)

// Resumed reports whether the log holds more than one run segment.
func (s SegmentStamp) Resumed() bool { return s.Starts > segmentSingleCount }

// segmentSingleCount is the segment count of a session that was never resumed
// — one header. It is named because `Starts > 1` is a QUESTION ("was this
// resumed?") and a bare 1 beside it would not say which side of the comparison
// it belongs to (Rule 1).
const segmentSingleCount = 1

// ScanSegments walks the WHOLE log at path and reports its segment boundaries.
// ok is false when the file could not be opened or the walk failed — the caller
// must then keep whatever it learned from its bounded windows rather than
// present an unscanned file as a single-segment one.
func ScanSegments(path string) (stamp SegmentStamp, ok bool) {
	if path == "" {
		return SegmentStamp{}, false
	}
	f, err := os.Open(path)
	if err != nil {
		return SegmentStamp{}, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, runScannerBufBytes), runStartLineMaxBytes)
	for sc.Scan() {
		line := sc.Bytes()
		switch {
		case bytes.Contains(line, segmentStartProbe):
			var ev startEvent
			if json.Unmarshal(line, &ev) != nil || ev.Type != sessionevent.EventTypeSessionStart {
				continue
			}
			stamp.Starts++
			stamp.LastStartPID = ev.PID
			if ev.ResumedFrom != "" {
				stamp.WitnessedStarts++
			}
			if ts, err := time.Parse(time.RFC3339Nano, ev.Timestamp); err == nil {
				stamp.LastStartTime = ts
				stamp.HasLastStart = true
			}
		case bytes.Contains(line, segmentEndProbe):
			var ev endEvent
			if json.Unmarshal(line, &ev) != nil || ev.Type != sessionevent.EventTypeSessionEnd {
				continue
			}
			stamp.Ends++
			stamp.LastEndReason = ev.Reason
			// The RECORDED totals, accumulated — see EndElapsedSec/EndPasses/
			// EndTokens: one verdict per run segment, each reporting only its
			// own run, so the session's figure is their sum.
			stamp.EndElapsedSec += ev.ElapsedSec
			stamp.EndPasses += ev.TotalIterations
			stamp.EndTokens += ev.TotalTokens
			stamp.HasEndTotals = true
			if ev.ExitCode != nil {
				stamp.LastEndExit, stamp.HasLastEndExit = *ev.ExitCode, true
			}
			if ts, err := time.Parse(time.RFC3339Nano, ev.Timestamp); err == nil {
				stamp.LastEndTime = ts
				stamp.HasLastEnd = true
			}
		}
	}
	if err := sc.Err(); err != nil {
		// A partial walk cannot answer "which segment is the last one": the
		// answer would be whatever the truncation happened to stop on.
		return SegmentStamp{}, false
	}
	// THE WITNESS STATE is decided here, from what the walk actually saw: a
	// scan that read headers says "carrying" or "absent"; a log with no header
	// at all answered nothing and keeps the zero state (Rule 15).
	if stamp.Starts > 0 {
		stamp.SegmentWitness = SegmentWitnessAbsent
		if stamp.WitnessedStarts > 0 {
			stamp.SegmentWitness = SegmentWitnessCarrying
		}
	}
	return stamp, true
}
