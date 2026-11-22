package agent

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/rawlogfmt"
	"github.com/nickhuang99/gopi/internal/replaymode"
	"github.com/nickhuang99/gopi/internal/scratch"
)

// rawLogSyncThreshold is the buffer size (in bytes) at which the raw logger
// auto-syncs without an explicit Sync() call. The SSE chunk callback (LogFunc)
// fired once per streamed chunk — a long thinking/text round produced hundreds
// of chunks, each with its own fsync (R5 — one sync per round instead of per
// chunk; each fsync was ~0.1-1ms).
const rawLogSyncThreshold = 64 * 1024

// rawLogHeaderPrefix opens the creation header of a raw log (writeRawLogHeader)
// — one spelling for the writer and every test that asserts on it (Rule 13).
const rawLogHeaderPrefix = "# gopi session raw HTTP log — created "

// rawLogResumeHeaderPrefix opens the APPENDED header of a raw log that a
// RESUME run continues (writeRawLogResumeHeader). It is a DIFFERENT prefix on
// purpose: a reader who greps for the creation header must not find a resumed
// run (the session was not created here), and a reader asking "was this file
// continued, by whom" has exactly one line to look for.
//
// It stays OUT of the round vocabulary for the same reason the creation header
// does: the round parser matches "→ REQUEST" / "← RESPONSE" / "← CHUNK" by whole
// line, and a `#` line carries no request, no response and no round number — so
// appending it mid-file cannot be mistaken for a round, and a resume's first
// real round is still the next ✓ marker after it.
const rawLogResumeHeaderPrefix = "# gopi session raw HTTP log — resumed "

// Raw log names are second-granularity wall clock, so two gopi processes
// started inside one second (parallel task children, an epic fan-out) resolve
// the SAME name. Exclusive creation makes the loser retry instead of silently
// O_APPENDing into the winner's file; these constants bound that retry.
//
//   - rawLogCreateAttempts: the "-1".." -N" numeric ladder — keeps names short
//     and readable for the realistic case (a handful of agents).
//   - rawLogUniqueAttempts: the "-u<pid>-<n>" ladder that follows it. It is
//     seeded by an in-process counter plus the pid, so it is unique by
//     construction among an agent's siblings and across concurrent processes;
//     it exists so raw-log creation can never fail, and therefore never has to
//     trade "share a file" against "lose the log".
const (
	rawLogCreateAttempts = 8
	rawLogUniqueAttempts = 4
)

// rawLogUniqueSeq hands every pid-suffixed fallback name its own number, so two
// raw loggers created in the SAME process inside one second never contend.
var rawLogUniqueSeq atomic.Uint64

// rawLogEnableEnv is the raw-http-log switch (Rule 2/21: ONE declared name,
// ONE reader — newRawLoggerInDir). It is a constant rather than a literal
// inside the reader because a SECOND site now has to name the same variable:
// chatSessionBlock tells the model WHY this run has no wire log, and a
// hand-typed "GOPI_RAW_LOG" there would be the mirror-literal defect Rules
// 1/13 name (the prompt could go on naming a switch the reader no longer
// reads).
const rawLogEnableEnv = "GOPI_RAW_LOG"

// rawLogDirEnv relocates the raw-log tree; same single-definition reason: the
// prompt explains where this run's log lives, and the relocation channel it
// names must be the one the constructor reads.
const rawLogDirEnv = "GOPI_RAW_LOG_DIR"

// rawLogDisabledValue is the value of rawLogEnableEnv that turns capture OFF
// (compared case-insensitively by the one reader).
const rawLogDisabledValue = "OFF"

// RawLogger writes raw HTTP request/response data to a log file.
// It is the foundation for hardcore debugging — no formatting, no timestamps,
// no levels. Just raw bytes with minimal directional markers.
//
// The file NEVER STARTS EMPTY: the constructor writes a short `#` header
// naming the run it belongs to. A zero-byte raw log used to be unrecoverable
// evidence — the file was created at session start but the first byte only
// landed at the first HTTP round, so a session that did no round (aborted
// setup, a refused request, a crash before the first send) left an empty file
// that answered nothing: was raw logging on? did the session start? which run
// wrote this? The header is written at the SAME moment the file is created
// (the earliest point in the session's life), so the log always carries the
// fact that raw capture was engaged, even when no round ever followed.
//
// Always enabled unless GOPI_RAW_LOG=OFF.
// GOPI_RAW_LOG_DIR overrides the default ~/.gopi/raw/ directory.
// The log file is named gopi-raw-YYYYMMDD-HHMMSS.log; an agent that starts
// inside a wall-clock second another agent already claimed takes
// gopi-raw-YYYYMMDD-HHMMSS-<n>.log (then the unique -u<pid>-<n> form), so one
// raw log file always belongs to exactly one session.
type RawLogger struct {
	file     *os.File
	buf      *bufio.Writer // chunk buffer — flushed+fsynced via Sync()
	buffered int           // bytes written since the last Sync
	path     string
	// format is the on-disk framing THIS file writes (rawlogfmt.Format). It is
	// decided ONCE, when the file is opened, and never changes: a raw log is
	// created by one run and appended to by later runs of the same row, and a file
	// that mixed two framings would force every reader into a mid-stream mode
	// switch. A run that CONTINUES a file therefore writes that file's own framing;
	// a run that writes a file's FIRST RECORD writes the length-prefixed one.
	//
	// The zero value is rawlogfmt.FormatLegacy, so a RawLogger built by hand in a
	// test (RawLogger{file: f}) keeps the framing its literal bytes would have.
	format rawlogfmt.Format
	mu     sync.Mutex
}

// rawLogContinueFormat answers which framing a RawLogger opened on path must
// write — the ONE decision point, used by all three creation doors.
//
// THREE ANSWERS, and the middle one is the whole reason this function exists
// rather than a bare rawlogfmt.Detect call:
//
//	the file already carries a record   → that record's framing (never mix)
//	the file carries NO record yet      → FormatFramed (this run writes the first
//	                                      one, and the first record FIXES the file)
//	the head cannot be read             → the error (never guess: guessing is how
//	                                      a mixed file gets written)
//
// A file with only `#` header lines is the ordinary case for the middle answer,
// not an oddity: every raw log is created with its header and its first record
// lands at the first HTTP round, so a run that dies before that round leaves a
// header-only file. That file has nothing to be compatible WITH — continuing it
// in the length-prefixed framing cannot contradict a single byte already on disk
// — so it takes the new framing, and no reader ever sees a file whose first
// record disagrees with its second.
func rawLogContinueFormat(path string) (rawlogfmt.Format, error) {
	format, decided, err := rawlogfmt.DetectExact(path)
	if err != nil {
		return rawlogfmt.FormatLegacy, err
	}
	if !decided {
		return rawlogfmt.FormatFramed, nil
	}
	return format, nil
}

// NewRawLogger creates a RawLogger in the legacy flat raw directory
// (~/.gopi/raw/ by default, GOPI_RAW_LOG_DIR to override). Logging is
// disabled by GOPI_RAW_LOG=OFF. It is retained for tests and legacy callers;
// NewAgent uses NewRawLoggerForRun (session-dir-unify S1) so new sessions'
// raw logs co-locate with their session JSONL.
func NewRawLogger() (*RawLogger, error) {
	dir := os.Getenv(rawLogDirEnv)
	if dir == "" {
		gd, err := config.GopiDir()
		if err != nil {
			return nil, fmt.Errorf("raw log: %w", err)
		}
		dir = filepath.Join(gd, "raw")
	}
	return newRawLoggerInDir(dir, "")
}

// NewRawLoggerForRun creates a RawLogger co-located with the run's session
// directory (session-dir-unify S1): the raw HTTP log lives next to the
// session JSONL so the whole run's assets share one directory.
//
//	task run child (taskID != "") → <sessions>/<task_id>/<run-uuid>.raw.log
//	bare prompt / chat (taskID == "") → <sessions>/<run-uuid>/raw.log
//
// GOPI_RAW_LOG_DIR, when set, keeps overriding (the operator's explicit
// relocation wins over the default layout); the session_start event records
// whichever path was used, so resume/raw resolution stays authoritative.
//
// A REPLAY RUN OPENS NOTHING (epic: `gopi chat --replay` must be really
// read-only; this file and session.go's NewSessionLoggerForRun are the two
// minter halves). The replay's stub answers every request in-process, so there is
// no HTTP exchange to record — and more to the point, the run's whole purpose is
// to READ a recording, so the `.raw.log` it would open is a file nobody asked for
// inside a session tree that must not exist at all. The check sits FIRST, before
// GOPI_RAW_LOG_DIR and before any os.MkdirAll, because a guard after the
// directory is created would leave the empty session directory behind in the
// operator's sessions root — the exact artifact this epic removes (measured
// 2026-10-20: a replay minted `sessions/<uuid>/` holding a `.raw.log` and a
// `session.jsonl`, which is what forced a scratch relocation to hide it).
//
// The refusal is RETURNED AS nil, not as an error: the caller's contract is
// "best-effort raw log" (NewAgent prints a non-nil error and continues), and a
// replay is not a failure — it is a run that needs no HTTP record. It is
// ANNOUNCED on stderr so "there is no raw log" cannot be confused with "the log
// went somewhere I did not look" (Rule 15).
func NewRawLoggerForRun(taskID, runUUID string) (*RawLogger, error) {
	return NewRawLoggerForResumedRun(taskID, RunIdentity{SessionID: runUUID})
}

// NewRawLoggerForResumedRun is NewRawLoggerForRun with the run's identity: when
// id.RawLogPath names an existing log, it is opened in APPEND mode, inside the
// session directory it already lives in, and stamped with the resume header
// (writeRawLogResumeHeader) instead of the creation one.
//
// WHY APPEND AND WHY NOT O_EXCL. Exclusive creation is the rule for a log this
// run MINTS (createRawLogFile states it: two runs in one wall-clock second must
// not share a file, and a shared file interleaves their HTTP traffic). A resume
// is the opposite case by definition — the file belongs to the session being
// continued, and appending to it is the deliverable. O_EXCL here would refuse
// the only file resume wants and fall back to a fresh uuid, which is exactly the
// defect this epic removes.
//
// The directory the file lives in is LEFT ALONE: a resumed run writes into the
// session directory that already exists, so no MkdirAll of a new location and no
// chance of a second container appearing beside the first.
func NewRawLoggerForResumedRun(taskID string, id RunIdentity) (*RawLogger, error) {
	if replaymode.Active() {
		replaymode.Noticef("%s\n", replaymode.Refuse("open this run's raw HTTP log ("+id.SessionID+scratch.SessionRawLogFileSuffix+")"))
		return nil, nil
	}
	if id.RawLogPath != "" {
		return openRawLoggerAppending(id.RawLogPath, id)
	}
	if dir := os.Getenv(rawLogDirEnv); dir != "" {
		return newRawLoggerInDir(dir, "")
	}
	dir, err := sessionDirForID(id.SessionID, taskID)
	if err != nil {
		return nil, fmt.Errorf("raw log: %w", err)
	}
	// The session dir must exist before the raw file opens inside it.
	if err := scratch.MkdirAllWithSuiteRunOwner(dir); err != nil {
		return nil, fmt.Errorf("raw log: cannot create %s: %w", dir, err)
	}
	if taskID != "" {
		// A TASK RUN'S RAW LOG CONTINUES THE ROW (epic: one task row, one id).
		// Its name is the task id, so the SECOND run of the same row opens the
		// SAME file — and O_EXCL (newRawLoggerInDir) would refuse it, silently
		// costing that run its wire log. See newRawLoggerInTaskRunDir.
		return newRawLoggerInTaskRunDir(dir, id)
	}
	return newRawLoggerInDir(dir, id.SessionID)
}

// newRawLoggerInTaskRunDir opens the raw log of a `task run` CHILD —
// <sessions>/<task_id>/<task_id>.raw.log — APPEND-OR-CREATE.
//
// WHY NOT O_EXCL, which is the rule everywhere else (createRawLogFile states
// it): exclusive creation exists so two runs that MINT the same name never
// interleave. A task run no longer mints its identity — its id is the row's
// (NewAgentResuming → mintRunID) — so the second run of a row lands on the
// first one's name BY DESIGN, and that is a continuation, not a collision: the
// raw log grows, exactly like the session record beside it. Refusing here would
// turn the unified id into a missing wire log for every resumed row.
//
// THE HEADER STILL SAYS WHICH CASE THIS IS, and the WITNESS of the first case
// is the file's own run history — not its size.
//
// WHAT USED TO HAPPEN (bug 01a190ba-e4ac: "a run claiming it resumed itself"). A
// task run's session id IS the row's id (mintRunID), so the second and every
// later run of a row opens this SAME path. `fi.Size() == 0` was the only
// witness the door had, and every non-empty file therefore took the continuation
// branch — which passed `ResumedFrom: runID`, i.e. the id of the session being
// written. The header said
//
//	# gopi session raw HTTP log — resumed <ts> pid=N session=<row> resumedFrom=<row>
//
// so a run declared that it resumed ITSELF, while the session record the very
// same process appended carries no resumedFrom at all (session.go writes the
// key only for a caller-supplied witness). Two artifacts by one process,
// disagreeing about one event — and a reader asking "which session did this run
// continue" got an answer that cannot be true: a run cannot pre-exist itself.
//
// THE TWO CASES ARE NOW TOLD APART BY A WITNESS OF A PRIOR RUN
// (rawLogPriorRunOf → rawLogHeaderNamesSession: does the file ALREADY carry a
// header naming this session?), never by byte count:
//
//	no header line for this session → this run creates the log
//	                                  (writeRawLogHeader)
//	a header line for this session  → a prior run of THIS row opened the log,
//	                                  so this run continues it (resume header,
//	                                  continuation stated, no resume SOURCE)
//
// and RESUMEDFROM IS EMPTY IN BOTH CASES, deliberately. Within one task row the
// continuation's source is the session being written — the row id the file is
// named by — so there is nothing a resume header could name that would be true.
// A resume header still ANNOUNCES the continuation (that is what keeps one file
// from claiming two creations, and the creation header's own contract says so);
// it just stops inventing a source. Naming a PRIOR session is the job of a
// different door — openRawLoggerAppending, where the caller's identity really is
// another session's.
//
// A NON-EMPTY FILE WITH NO RECOGNIZABLE HEADER (a hand-edited log, a fragment
// whose head is gone) is NOT a prior run either: bytes are not a witness. The
// file is continued — appending is the only way to keep the wire log — but the
// header claims no source, exactly as the file's own history justifies nothing
// (Rule 15a: a witness whose zero value reads as "not recorded" is never
// guessed).
func newRawLoggerInTaskRunDir(dir string, id RunIdentity) (*RawLogger, error) {
	if v := os.Getenv(rawLogEnableEnv); strings.ToUpper(v) == rawLogDisabledValue {
		return nil, nil
	}
	if err := scratch.MkdirAllWithSuiteRunOwner(dir); err != nil {
		return nil, fmt.Errorf("raw log: cannot create %s: %w", dir, err)
	}
	runID := id.SessionID
	path := filepath.Join(dir, runID+scratch.SessionRawLogFileSuffix)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("raw log: cannot open %s: %w", path, err)
	}
	// The framing is the FILE's, decided before this run appends a byte (see
	// rawLogContinueFormat): a row's second run writes what its first run wrote.
	format, ferr := rawLogContinueFormat(path)
	if ferr != nil {
		_ = f.Close() //stylegate:ignore the open handle is abandoned because the framing could not be decided; that error is the one returned
		return nil, ferr
	}
	if rawLogPriorRunOf(f, path, runID) == rawLogSessionNotOpened {
		writeRawLogHeader(f, path, runID)
	} else {
		writeRawLogResumeHeader(f, path, RunIdentity{SessionID: runID, ResumedFrom: rawLogResumeSource(id)})
	}
	return &RawLogger{file: f, buf: bufio.NewWriterSize(f, rawLogSyncThreshold), path: path, format: format}, nil
}

// openRawLoggerAppending opens an EXISTING raw log and appends to it — the one
// writer a resumed run uses (NewRawLoggerForResumedRun). It never creates
// anything: a path that does not open is an ERROR naming the path, because a
// resume whose raw log silently became a new file is the defect, not a
// degradation (Rule 15).
//
// GOPI_RAW_LOG=OFF is honoured here too, through the ONE reader of that switch
// (newRawLoggerInDir would honour it, but this path does not go through it any
// more — a second spelling of the comparison would be the mirror-literal defect
// Rules 1/13 name).
func openRawLoggerAppending(path string, id RunIdentity) (*RawLogger, error) {
	if v := os.Getenv(rawLogEnableEnv); strings.ToUpper(v) == rawLogDisabledValue {
		return nil, nil
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("raw log: cannot append to the continued session's log %s: %w", path, err)
	}
	// The continued file's own framing — a resumed run must append bytes
	// indistinguishable from the ones already there.
	format, ferr := rawLogContinueFormat(path)
	if ferr != nil {
		_ = f.Close() //stylegate:ignore the handle is abandoned because the framing could not be decided; that error is returned
		return nil, ferr
	}
	writeRawLogResumeHeader(f, path, id)
	return &RawLogger{file: f, buf: bufio.NewWriterSize(f, rawLogSyncThreshold), path: path, format: format}, nil
}

// writeRawLogResumeHeader appends the resume witness to a raw log this run
// CONTINUES — the same facts the creation header carries (when, which pid, which
// session, which file) plus the two that only a resumed run can state: the
// session it continues and the log it is appending to.
//
// A DIFFERENT LINE, ON PURPOSE. The creation header says "this file was created
// by this run"; a resume did not create it. Writing the creation header again
// would make one file claim two creations, and a reader dating a session by its
// creation header would read the SECOND run's timestamp as the session's start.
//
// It goes straight to the file, not through the bufio writer, for the reason
// writeRawLogHeader does: a run that dies before its first round must still leave
// the fact that it resumed, and Close never runs on a kill.
func writeRawLogResumeHeader(f *os.File, path string, id RunIdentity) {
	fields := []string{
		rawLogResumeHeaderPrefix + time.Now().UTC().Format(time.RFC3339),
		fmt.Sprintf("pid=%d", os.Getpid()),
		rawLogSessionFieldKey + id.SessionID,
	}
	if id.ResumedFrom != "" {
		// THE SELF-RESUME GUARD LIVES IN THE ONE PLACE THAT ANSWERED THE
		// QUESTION (bug 01a190ba): a resume header may never name the session it
		// belongs to. rawLogResumeSource normalises the source so this writer
		// cannot emit `resumedFrom=<its own session>` even if a caller hands it
		// in — openRawLoggerAppending is the door where the identity really is
		// another session's, and it passes the same rule.
		if src := rawLogResumeSource(id); src != "" {
			fields = append(fields, rawLogResumedFromFieldKey+src)
		}
	}
	fields = append(fields, rawLogLogFieldKey+filepath.Base(path))
	// Both results are deliberately dropped, for the reason writeRawLogHeader
	// states: the header is an aid to a reader, never a precondition for the
	// round capture that follows (Rule 15b — the discard is the design).
	_, _ = fmt.Fprintln(f, strings.Join(fields, " ")) //stylegate:ignore the raw log is a best-effort audit channel; a refused line cannot be reported without re-entering the same writer
	_ = f.Sync()                                      //stylegate:ignore sync on the raw log's resume path; nothing reads the result
}

// newRawLoggerInDir opens a raw logger in dir. When runUUID is non-empty the
// file is <run-uuid>.raw.log (deterministic per run — the session dir is
// per-run, so no collision ladder is needed, but O_EXCL still guards a
// stale re-run); when runUUID is empty the legacy timestamped
// gopi-raw-<YYYYMMDD-HHMMSS>.log name with its collision ladder is used.
func newRawLoggerInDir(dir, runUUID string) (*RawLogger, error) {
	if v := os.Getenv(rawLogEnableEnv); strings.ToUpper(v) == rawLogDisabledValue {
		return nil, nil
	}

	if err := scratch.MkdirAllWithSuiteRunOwner(dir); err != nil {
		return nil, fmt.Errorf("raw log: cannot create %s: %w", dir, err)
	}

	var f *os.File
	var path string
	var err error
	if runUUID != "" {
		// The suffix is the DECLARED tree's own value (scratch.SessionRawLogFileSuffix):
		// the sweep that bounds this tree and the writer that fills it must agree on
		// exactly one spelling, or the GC bounds a tree nobody writes to
		// (Rules 13/14).
		path = filepath.Join(dir, runUUID+scratch.SessionRawLogFileSuffix)
		f, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			// A stale file from a re-run with the same uuid (uuid reuse is
			// not possible in practice) — fall back to exclusive creation
			// failure reporting like the legacy ladder would.
			return nil, fmt.Errorf("raw log: cannot create %s: %w", path, err)
		}
	} else {
		f, path, err = createRawLogFile(dir)
		if err != nil {
			return nil, err
		}
	}

	writeRawLogHeader(f, path, runUUID)
	// A file this door MINTED (O_EXCL, both branches) is empty, so the framing is
	// this run's to decide — and one decision point answers it for all three
	// creation doors (rawLogContinueFormat; Rules 13/14).
	format, ferr := rawLogContinueFormat(path)
	if ferr != nil {
		_ = f.Close() //stylegate:ignore the handle is abandoned because the framing could not be decided; that error is returned
		return nil, ferr
	}
	rl := &RawLogger{file: f, buf: bufio.NewWriterSize(f, rawLogSyncThreshold), path: path, format: format}
	// THE HEADER IS ALREADY ON DISK (writeRawLogHeader writes straight to the
	// file), so the framing detector above sees a header-only file and answers
	// "this run decides" — which it does. Nothing else has to be re-decided.
	return rl, nil
}

// writeRawLogHeader stamps the freshly created raw log with the facts of the
// run that owns it — the earliest honest record of the session's life, written
// at the moment the file exists and BEFORE any HTTP round can. See RawLogger's
// doc for why the file must never start empty.
//
// The line is NOT in the raw HTTP logger's own vocabulary (→ REQUEST /
// ← RESPONSE / ← CHUNK are the markers the round parser matches by whole-line
// equality), so it can never be mistaken for a round: it carries no request,
// no response and no round number.
//
// It goes straight to the FILE, not through the bufio writer: the header's
// whole purpose is to define a log whose session ends before the first round,
// and a buffered header would be lost with the very process it is meant to
// explain (Close never runs on a crash or a kill). One small write at
// creation, and Sync keeps the bytes on disk.
func writeRawLogHeader(f *os.File, path, runUUID string) {
	fields := []string{
		rawLogHeaderPrefix + time.Now().UTC().Format(time.RFC3339),
		fmt.Sprintf("pid=%d", os.Getpid()),
	}
	if runUUID != "" {
		fields = append(fields, rawLogSessionFieldKey+runUUID)
	}
	fields = append(fields, rawLogLogFieldKey+filepath.Base(path))
	// Both results are deliberately dropped: a header that cannot be written
	// must not cost the caller its log. The file is open either way and the
	// session's rounds are still captured — the header is an aid, never a
	// precondition (Rule 15b: the discard is the design, and it is commented).
	_, _ = fmt.Fprintln(f, strings.Join(fields, " ")) //stylegate:ignore the raw log is a best-effort audit channel; a refused line cannot be reported without re-entering the same writer
	_ = f.Sync()                                      //stylegate:ignore sync on the raw log's exit path; the process is ending and nothing reads the result
}

// createRawLogFile opens this agent's own raw log file EXCLUSIVELY and returns
// it with its path. The name is second-granularity wall clock, so a bare
// O_CREATE|O_APPEND open would silently attach two agents started in the same
// second to one file: O_APPEND makes both writers "work", the sessions' HTTP
// traffic interleaves with no boundary, and raw log round numbering stops
// being session-scoped — `gopi session raw <session> 1 request` then hands back
// another session's first request and the user's own prompt/epic goal is
// invisible in the raw view. O_EXCL closes that hole: the first agent wins the
// bare name and the loser retries a suffixed name, so one file always belongs
// to exactly one session. The candidate names run bare → numerically suffixed
// → counter-and-pid suffixed (see the constants above); the last ladder is
// unique by construction, so the only error left is an unwritable directory.
func createRawLogFile(dir string) (*os.File, string, error) {
	stamp := time.Now().Format("20060102-150405")
	// The name shape is the DECLARED tree's (scratch.RawLogNamePrefix /
	// scratch.RawLogFileSuffix): the sweep that bounds this directory requires
	// exactly this prefix, so a second spelling here would make the writer's own
	// files invisible to the GC (Rules 13/14).
	base := scratch.RawLogNamePrefix + stamp

	candidates := make([]string, 0, 1+rawLogCreateAttempts+rawLogUniqueAttempts)
	candidates = append(candidates, base+scratch.RawLogFileSuffix)
	for i := 1; i <= rawLogCreateAttempts; i++ {
		candidates = append(candidates, fmt.Sprintf("%s-%d.log", base, i))
	}
	seq := rawLogUniqueSeq.Add(1)
	pid := strconv.Itoa(os.Getpid())
	for i := 0; i < rawLogUniqueAttempts; i++ {
		candidates = append(candidates, fmt.Sprintf("%s-u%s-%d.log", base, pid, seq+uint64(i)))
	}

	var lastErr error
	for _, name := range candidates {
		path := filepath.Join(dir, name)
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			return f, path, nil
		}
		lastErr = err
		if !errors.Is(err, os.ErrExist) {
			return nil, "", fmt.Errorf("raw log: cannot create %s: %w", path, err)
		}
	}
	return nil, "", fmt.Errorf("raw log: cannot create %s: %w (no free name after %d candidates)",
		filepath.Join(dir, candidates[0]), lastErr, len(candidates))
}

// Write writes raw bytes to the log. Thread-safe; buffered (R5) — flushed at
// the round boundary via Sync(), at Close(), or when the buffer reaches
// rawLogSyncThreshold.
//
// REMOVED. This was the SECOND write path on the type and the only one that
// bypassed the record framing: it appended bare bytes with no header, which is
// exactly the ambiguity this package exists to remove — a payload that happened to
// contain a marker line was indistinguishable from a direction, and a framed file
// could not describe those bytes at all. It had no production caller (measured:
// the only references were three tests in this file, all of which built their
// fixture through it). A log that has two writers, one of them unframed, has no
// format. Records go through LogFunc; nothing else appends.
//
// Path returns the full path to the raw log file.
func (rl *RawLogger) Path() string {
	return rl.path
}

// Sync flushes the buffer to the file and fsyncs it to disk. Called at each
// round boundary (loop.go) and at Close(). No-op when nothing is buffered
// (avoids empty fsyncs).
func (rl *RawLogger) Sync() error {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return rl.syncLocked()
}

// syncLocked flushes the buffer and fsyncs the file. Caller must hold mu.
func (rl *RawLogger) syncLocked() error {
	if rl.buffered == 0 {
		return nil
	}
	if err := rl.buf.Flush(); err != nil {
		return err
	}
	rl.buffered = 0
	return rl.file.Sync()
}

// Close flushes any buffered bytes, fsyncs, and closes the raw log file.
func (rl *RawLogger) Close() error {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	syncErr := rl.syncLocked()
	closeErr := rl.file.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

// LogFunc returns a callback suitable for use as deepseek.Client.RawLog.
//
// ONE RECORD PER CALL, written in THIS FILE'S framing (rl.format):
//
//	framed  →  "<tag> <len>\n" + payload verbatim + "\n"
//	legacy  →  "<marker line>\n" + payload verbatim + "\n"
//
// The tag names the record's DIRECTION and nothing else (rawlogfmt.TagRequest /
// TagProbeRequest / TagResponse / TagChunk / TagError). In the framed framing the
// direction is METADATA — a length prefix — so no byte of a payload is ever
// scanned for text and no payload can be mistaken for a marker; in the legacy
// framing it stays the marker line the file's own history was written with, so a
// run continuing an old log appends bytes indistinguishable from its neighbours.
//
// THE PAYLOAD IS NEVER TOUCHED, in either framing: the encoder adds a header and
// removes nothing (rawlogfmt.Encode states it, and devdoc/rawlog-record-framing.md
// carries the measured before/after).
//
// An unknown tag is DROPPED rather than guessed (Rule 15: a record whose
// direction no reader can recover must not be written at all — and there is no
// error return here, because a raw log is a best-effort audit channel and losing
// one malformed record must not cost the caller its round).
func (rl *RawLogger) LogFunc() func(tag, annotation string, payload []byte) {
	return func(tag, annotation string, payload []byte) {
		record, err := rl.encodeRecord(tag, annotation, payload)
		if err != nil {
			return
		}
		rl.mu.Lock()
		defer rl.mu.Unlock()
		n, werr := rl.buf.Write(record)
		rl.buffered += n
		if werr == nil && rl.buffered >= rawLogSyncThreshold {
			rl.syncLocked()
		}
	}
}

// encodeRecord renders one record in this logger's framing — the ONE encoder both
// the callback and any test shares, so the bytes a test asserts on are the bytes
// a run writes.
func (rl *RawLogger) encodeRecord(tag, annotation string, payload []byte) ([]byte, error) {
	if !rawlogfmt.IsTag(tag) {
		return nil, fmt.Errorf("raw log: refusing to write an unknown record tag %q", tag)
	}
	if rl.format == rawlogfmt.FormatFramed {
		return rawlogfmt.EncodeAnnotated(tag, annotation, payload), nil
	}
	return rawlogfmt.EncodeLegacy(tag, annotation, payload)
}
