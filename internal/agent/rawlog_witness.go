package agent

import (
	"bufio"
	"os"
	"strings"
)

// WHY THIS FILE EXISTS SEPARATELY (bug 01a190ba).
//
// The witness below answers a question the raw-log door used to answer from
// `fi.Size() == 0`: "did a PRIOR run of this session already open this log?" It
// lives in its own file for two reasons, both about the reader rather than the
// compiler:
//
//  1. The question is not "how big is the file". A task run's session id IS its
//     row id, so every second and later run of a row opens the SAME path; byte
//     count cannot distinguish "a prior run was here" from "someone left
//     bytes". Keeping the witness out of rawlog.go keeps that distinction
//     visible at the place it is made.
//  2. CONFIG.md anchors the two raw-log knob rows to line numbers in
//     rawlog.go (81/116/145). Adding ~180 lines of witness code above them
//     moved those anchors and turned the anchor census red for a change that
//     had nothing to do with configuration. A separate file leaves the
//     anchored lines where the document says they are.

// The FIELD KEYS both header writers emit, declared once because a second
// consumer now reads them back: rawLogHeaderLineNamesSession parses the log's own
// header lines to answer "which session does this file already hold", and a
// reader that spelled "session=" differently from the writer would find nothing
// and report the file as unidentifiable rather than as belonging to a prior run
// (Rules 1/13/14).
const (
	rawLogSessionFieldKey     = "session="
	rawLogResumedFromFieldKey = "resumedFrom="
	rawLogLogFieldKey         = "log="
)

// rawLogWitnessScanLines bounds how much of an existing log
// rawLogHeaderNamesSession reads. A raw log grows to hundreds of megabytes (one
// observed on this host is 210 MB) and is APPENDED to by every run of the row,
// so the header lines are all near the FRONT: the creation header at byte 0 and
// one resume header per later run, each written before any HTTP round. A bounded
// window keeps "which runs wrote this file" an O(window) question instead of a
// scan of the whole file — and an unterminated tail (a kill mid-round) cannot
// make it run away.
const rawLogWitnessScanLines = 4096

// rawLogWitnessLineBuf is the maximum length of one line the witness scanner
// will accept. A raw log line is one direction marker plus one HTTP payload, and
// payloads do run long (a request body with a big tool result), so the buffer is
// generous — but it is BOUNDED, so a corrupt file cannot make the probe allocate
// without limit; a line over this size simply ends the scan, and "not witnessed"
// is the safe answer (the file is then treated as one this run creates).
const rawLogWitnessLineBuf = 1024 * 1024

// rawLogResumeSource is the ONE rule for "which session may a resume header
// name" — and it exists as a function so the rule cannot be repeated wrong.
//
// THE RULE: a resume source is a session this run CONTINUES. Within one task row
// the id being written IS the row's id, so a run that states its own id as its
// resume source claims something no run can do — pre-exist itself. That is bug
// 01a190ba, and this function is why it cannot come back: the caller supplies the
// identity the DOOR gave it (id.ResumedFrom, from a re-dispatch declaration or a
// resume face), and anything equal to the session being written is REFUSED HERE
// rather than at four call sites.
//
// The refusal is silent by design and it is announced where it must be: an empty
// source means "no source" — the resume header still records that the log was
// continued, so no information is lost and nothing false is written. A caller
// that keeps handing in the run's own id gets a continuing header with no
// `resumedFrom` field, not a self-resume.
func rawLogResumeSource(id RunIdentity) string {
	if id.ResumedFrom == "" || id.ResumedFrom == id.SessionID {
		return ""
	}
	return id.ResumedFrom
}

// rawLogPriorRun is the VERDICT type of the witness below — three answers, not
// two, because "the file has bytes" is one fact and "a prior run of this row
// opened it" is another (bug 01a190ba: the second was inferred from the first).
type rawLogPriorRun int

const (
	// rawLogSessionNotOpened: the file is empty, so this run is the one that
	// creates the log. The ONLY state that earns a creation header.
	rawLogSessionNotOpened rawLogPriorRun = iota
	// rawLogSessionOpenedByPriorRun: a header line, already on disk, names THIS
	// session — positive evidence that an earlier run of this row was here, and
	// the one witness that may justify calling the file a continuation.
	rawLogSessionOpenedByPriorRun
	// rawLogSessionOpenedUnknown: the file holds bytes but no recognisable header
	// (a trimmed head, a hand-edited file, a foreign fragment). Still NOT this
	// run's creation — the bytes were there first — but nothing identifies who
	// wrote them, so no resume SOURCE may be inferred from them.
	rawLogSessionOpenedUnknown
)

// rawLogPriorRunOf is the ONE witness of "does this log already belong to a
// session" — the question `fi.Size() == 0` used to stand in for, and the reason
// it was wrong.
//
// It answers rawLogSessionOpenedByPriorRun only on positive evidence: a HEADER
// line, already on disk, that names THIS session (rawLogHeaderNamesSession). A
// hit means the file's own history says an earlier run of this row opened it.
// An unreadable file, one whose only content is HTTP traffic, and one whose
// headers name some OTHER session all answer rawLogSessionOpenedUnknown —
// "unknown" is never reported as "a prior run exists", or the witness is the
// byte count again under a new name (Rule 15).
//
// The file is being APPENDED to, so it is read through a separate descriptor and
// never through f: this function must not move the writer's offset, and O_APPEND
// already fixes each write at the end. The read is BOUNDED
// (rawLogWitnessScanLines) because headers live at the front of a file that
// grows to hundreds of megabytes.
func rawLogPriorRunOf(f *os.File, path, runID string) rawLogPriorRun {
	if fi, err := f.Stat(); err == nil && fi.Size() == 0 {
		return rawLogSessionNotOpened
	}
	if rawLogHeaderNamesSession(path, runID) {
		return rawLogSessionOpenedByPriorRun
	}
	return rawLogSessionOpenedUnknown
}

// rawLogHeaderNamesSession reports whether any header line of the log at path
// names runID — the whole of the prior-run witness, in one readable predicate
// (newRawLoggerInTaskRunDir is its caller; the tests pin it directly, so a
// returning defect is caught by a failing assertion and not by a log that
// quietly stopped saying "continued").
func rawLogHeaderNamesSession(path, runID string) bool {
	want := rawLogSessionFieldKey + runID
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = file.Close() }() //stylegate:ignore read-only probe; a close error on an O_RDONLY descriptor changes nothing about a boolean already derived from the bytes scanned, and the answer is returned before this runs
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, rawLogWitnessLineBuf), rawLogWitnessLineBuf)
	for line := 0; line < rawLogWitnessScanLines && scanner.Scan(); line++ {
		if rawLogHeaderLineNamesSession(scanner.Text(), want) {
			return true
		}
	}
	return false
}

// rawLogHeaderLineNamesSession is the ONE predicate over a single line: an
// ordinary log line legitimately CONTAINS `session=<row>` (every REQUEST body
// carries `"sessionId"`, and a header line's own text can appear inside a logged
// payload), so the test is header-shaped AND field-boundary-shaped. `log=` is
// the field that ends both header forms, so a field equal to `session=<runID>`
// is a whole field of a header, never a substring of a longer id or of a body.
func rawLogHeaderLineNamesSession(line, want string) bool {
	if !strings.HasPrefix(line, rawLogHeaderPrefix) && !strings.HasPrefix(line, rawLogResumeHeaderPrefix) {
		return false
	}
	for _, field := range strings.Fields(line) {
		if field == want {
			return true
		}
	}
	return false
}
