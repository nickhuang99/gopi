package agent

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/inputsource"
	"github.com/nick/gopi/internal/replaymode"
	"github.com/nick/gopi/internal/requestusage"
	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/sessionid"
	"github.com/nick/gopi/internal/sessionlayout"
	"github.com/nick/gopi/internal/terminalgeom"
	gopiversion "github.com/nick/gopi/internal/version"
)

// sessionLogSyncThreshold is the buffer size (in bytes) at which the session
// logger auto-syncs without an explicit Sync() call. Below it, events stay in
// the memory buffer until the round boundary, the task end, or a tool result
// (R5 — devdoc/perf-vs-claude-fix-plan.md §R5: one fsync per round instead of
// per event; each fsync was ~0.1-1ms and a round produced 100-600 of them via
// session events + every SSE chunk).
const sessionLogSyncThreshold = 64 * 1024

// SessionLogger writes JSONL session logs to a per-session directory under
// ~/.gopi/sessions/ (session-dir-unify S1):
//
//	task run child (GOPI_TASK_WRAPPER_ID set) →
//	    <gopi-dir>/sessions/<task_id>/<run-uuid>.jsonl
//	bare prompt / chat / resume →
//	    <gopi-dir>/sessions/<session-uuid>/session.jsonl
//
// Each line is a JSON object with at least "type" and timestamp fields,
// modeled after Claude Code's session format. The run-uuid IS the session id
// minted by NewSessionLogger; repeated runs of one task (task run, resume,
// re-run) each mint a fresh uuid, so multiple <run-uuid>.jsonl files
// coexist inside the task dir — a run never overwrites an earlier one.
type SessionLogger struct {
	file      *os.File
	buf       *bufio.Writer // event buffer — flushed+fsynced via Sync()
	buffered  int           // bytes written since the last Sync
	sessionID string
	taskID    string // "" for bare sessions; the ledger task_id for task-run children
	startTime time.Time
	path      string // full path to the JSONL file
	// geometry is the terminal this run was ON, measured once when the logger
	// was created and written into session_start (and metadata.json) as
	// sessionevent.TerminalGeometry. It is the record a replay/film reads to
	// reproduce the canvas the session was actually drawn at, instead of the
	// built-in ideal size — measured, never assumed, and UNKNOWN (with the
	// reason) when there was no terminal to ask (Rule 15a: a zero here would be
	// indistinguishable from a real 0-cell terminal, which does not exist).
	geometry sessionevent.TerminalGeometry
	// artifact is the identity of the EXECUTABLE THIS RUN IS (path + the
	// revision the binary is the build of + who says so), resolved once when
	// the logger was created and written into session_start (and
	// metadata.json).
	//
	// WHY A RUN RECORDS ITS OWN BINARY. A task run's tool surfaces are
	// executed by the WORKER's executable, and that binary may come from
	// ANOTHER checkout than the run's ground — measured 2026-10-14 (task
	// 01a1355d): the worker was `gopi worker` with exe
	// /home/user/work/GoPi/bin/gopi at revision adafe54b while the run's
	// ground was a linked worktree whose source that very run was editing. The
	// two facts are about two code versions, and a report's `Ran:`/`Result:`
	// lines cannot be pinned to one after the run is gone — unless the run's
	// own record carries the identity. It is never absent (an artifact with no
	// revision records THAT, with its reason — Rule 15) and it is a plain JSON
	// object with no "message" key, so every existing session reader tolerates
	// it structurally.
	artifact sessionevent.ArtifactIdentity
	// requests counts the `request` events this logger has written — the
	// session's own round count (see RequestsSoFar; guarded by mu like every
	// other field of this struct).
	requests int
	// turns counts the `user` rows THIS LOGGER wrote that OPEN A TURN — the
	// display half of the session's own turn numbering (see TurnsSoFar).
	//
	// IT IS COUNTED AT THE WRITER, NOT DERIVED BY THE CALLER, because the
	// caller that needs it mid-turn is holding no reader: `gopi web` must put
	// "turn N" on the wire before the turn runs, and the only authority for N
	// is this record. Deriving it by re-parsing the file would read a record
	// whose tail is still in the buffer (sessionLogSyncThreshold), i.e. the
	// number would lag the row that produced it — the exact drift the web turn
	// banner exists to remove. Guarded by mu like every other field here.
	turns int
	// err is the header write's outcome — a non-nil value means this run's
	// session_start never made it to disk (a short write, ENOSPC, a closed
	// descriptor). It is kept because the header write is the ONE write whose
	// failure must not be silent: the record every later reader depends on
	// would be missing its first line, and that is indistinguishable from a
	// session that never started (Rule 15). Callers that build a logger check
	// it (newSessionLoggerOnFile's two callers); callers that log one event at
	// a time get the error from the log call itself.
	err error
	mu  sync.Mutex
}

// SessionDir resolves the per-session run directory for a session: given the
// optional task_id ("" for bare), it is
//
//	<gopi-dir>/sessions/<task_id>/        (task run child)
//	<gopi-dir>/sessions/<session-uuid>/   (bare prompt/chat)
//
// sessionDirForID returns the run directory (which may not exist yet) and
// the sessions root. The directory is created by the caller that opens a
// file inside it.
func sessionDirForID(sessionID, taskID string) (string, error) {
	root, err := config.SessionsRoot()
	if err != nil {
		return "", err
	}
	if taskID != "" {
		// THE SHAPE GATE (bug row 01a12f3c, 2026-10-12): the task id becomes a
		// path element here, and the caller MkdirAll's the answer. Joining an id
		// this tree does not spell as a task id is how a second container was
		// born SILENTLY — a caller holding the branch/worktree spelling (a uuid
		// with every dash dropped) got no error and a directory. The rule is the
		// same one internal/tools' task container applies, from one definition
		// (internal/sessionid), so the two sides of the agent/tools boundary
		// cannot disagree about what a task id is.
		if !sessionid.IsAdmissible(taskID) {
			return "", sessionid.Refuse(taskID)
		}
		return filepath.Join(root, taskID), nil
	}
	return filepath.Join(root, sessionID), nil
}

// SessionLogFileName returns the JSONL filename inside a session run dir.
// Task runs name their log files by run-uuid (several runs coexist); bare
// sessions use the fixed bareSessionLogName (one run per session dir).
func SessionLogFileName(sessionID, taskID string) string {
	if taskID != "" {
		return sessionID + sessionJSONLSuffix
	}
	return bareSessionLogName
}

// NewSessionLogger creates a new session logger. It generates a UUID v4
// session ID (the run-uuid), creates the session's run directory under the
// sessions root (auto-created on first use; the gopi data root is
// $GOPI_HOME/.gopi when GOPI_HOME is set — the test-isolation knob — else
// ~/.gopi), opens the session JSONL file, writes the session_start event and
// a base metadata.json.
//
// taskID, when non-empty, selects the task-dir layout
// (sessions/<task_id>/<run-uuid>.jsonl — the executor of a `task run` child
// passes the wrapper row's task_id); empty selects the bare layout
// (sessions/<session-uuid>/session.jsonl). NewSessionLogger keeps the
// variadic form so existing single-run callers (tests, chat) stay on the
// bare layout without threading an explicit "".
//
// Session logging is best-effort: if creation fails (e.g. no home
// directory), nil is returned with an error that the caller can log and
// continue without.
// rawLogPath, when non-empty, is recorded in the session_start event so a
// later `gopi session resume <session-id>` can resolve the session's raw log EXACTLY
// (no timestamp heuristics — the path is authoritative, and works for custom
// GOPI_RAW_LOG_DIR sessions too).
func NewSessionLogger(model string, rawLogPath string, taskID ...string) (*SessionLogger, error) {
	return NewSessionLoggerForRun(model, rawLogPath, newUUIDv4(), taskID)
}

// NewSessionLoggerForRun creates a session logger with a PRE-MINTED run uuid
// (NewAgent's seam: the raw logger and the session JSONL must share the run
// directory and the run-uuid filename — NewRawLoggerForRun(taskID, uuid) is
// called first, then this with the same uuid). taskID selects the layout as
// in NewSessionLogger.
func NewSessionLoggerForRun(model string, rawLogPath string, sessionID string, taskID []string) (*SessionLogger, error) {
	return NewSessionLoggerForResumedRun(model, rawLogPath, sessionID, taskID, "")
}

// openResumeSessionRecord appends to the session RECORD a resumed run
// continues — the record sibling of the raw log's openRawLoggerAppending, with
// the same contract: it grows the files the continued session owns.
//
// IT NEVER CREATES A DIRECTORY (that is the whole point, row 01a17dd8).
// Deriving the directory from the id is exactly what mints a second run
// directory beside the row's own when the id cannot spell the first one (a
// task-run record lives in sessions/<task-id>/ while its session id spells
// sessions/<uuid>/), so this writer only APPENDS: the parent must already
// exist, and a missing one is an error naming the path (Rule 15).
//
// THE FILE ITSELF MAY BE CREATED (O_CREATE), and that is deliberate: a run
// whose raw-log checkpoint survived while its record was pruned is a resume of
// the run that owns this directory, so the segment's header belongs in THAT
// directory — the invariant is "no new session directory", not "no new file".
// A record that vanished between resolution and open therefore still yields a
// continuable session instead of a refusal with nothing to continue.
func openResumeSessionRecord(path string, model string, sessionID, tid, rawLogPath, resumedFrom string) (*SessionLogger, error) {
	dir := filepath.Dir(path)
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("session log: the continued session's directory %s is gone — a resume grows an existing run, it never mints one: %w", dir, err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("session log: cannot append to the continued session's record %s: %w", path, err)
	}
	return newSessionLoggerOnFile(f, path, model, sessionID, tid, rawLogPath, resumedFrom)
}

// refuseUnterminatedRecordTail is the WRITER-SIDE half of the torn-tail repair:
// a record whose last byte is not a newline ends inside an unreadable row, and
// appending this run's header behind it would FUSE the dead fragment with live
// JSON. The result is not a bigger tail — it is mid-file corruption, which no
// reader can separate back into its two records, so a truncation an operator
// could have seen and re-derived becomes permanent noise in the middle of a
// file.
//
// WHAT IT IS NOT. It is not "repair the file": repairing means truncating
// somebody's record to a byte offset, and a writer that silently discards
// another process's bytes is the silent data loss Rule 15 forbids. The operator
// decides; this refuses and names what they need to decide with.
//
// MEASURED, NOT HYPOTHESISED (2026-10-12, this host): three session records were
// left torn by a short write — their mtimes 18:16:20 / 18:16:42 / 18:16:55 fall
// inside an 8,504-line "No space left on device" window (journal boot -3) — and
// a resumed run appends a second session_start, which without this guard lands
// directly behind the fragment.
func refuseUnterminatedRecordTail(path string) error {
	unterminated, err := sessionlayout.RecordTailUnterminated(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // nothing there yet: the open below creates it, and an empty record has no tail
		}
		return fmt.Errorf("session log: cannot inspect the tail of %s before appending — refusing to append blind (an uninterrogated record may be torn): %w", path, err)
	}
	if !unterminated {
		return nil
	}
	fi, statErr := os.Stat(path)
	if statErr != nil {
		return fmt.Errorf("session log: %s ends mid-record and its size cannot be read: %w", path, statErr)
	}
	return &UnterminatedRecordTailError{Path: path, Size: fi.Size()}
}

// UnterminatedRecordTailError is the refused append: a session record whose last
// byte is not a newline. It is exported so a test asserts the SHAPE (the file,
// its size, the reason) instead of matching English, and so a caller above can
// recognise the one condition that is repairable by hand.
type UnterminatedRecordTailError struct {
	Path string
	Size int64
}

func (e *UnterminatedRecordTailError) Error() string {
	return fmt.Sprintf(
		"session log: REFUSING to append to %s (%d bytes) — its last byte is not a newline, so the record ends mid-line. Appending this run's header would bury that unreachable fragment in the MIDDLE of the file, turning a truncation a reader can still describe into corruption nothing can separate. The session cannot be continued until the record is examined: `gopi session log <id>` names the torn line, its byte offset and the intact lines before it; recover the readable prefix from that offset, then archive the record and start a new one",
		e.Path, e.Size)
}

// NewSessionLoggerForResumedRun is NewSessionLoggerForRun for a run that
// CONTINUES an existing session record: the JSONL is opened in the same
// directory, APPENDED to (it already exists), and its session_start carries the
// two resume witness keys (sessionevent.SessionStartFieldResumedFrom /
// …ResumedFromRawLog) so the file says why it holds more than one header.
//
// WHY APPENDING A SECOND session_start IS THE RIGHT RECORD rather than a defect
// (epic: `gopi chat --resume <id>` minted a second session). The alternative —
// rewriting the file's first line, or writing nothing — is worse in both
// directions: a rewrite destroys the run that created the session, and writing
// nothing leaves the record unable to answer "was this ever resumed, and by
// whom", which is the first question anyone asks of a session that grew after
// its session_end. Every reader already tolerates the row structurally: they
// switch on the event type and either take the FIRST session_start
// (sessionlayout.SessionID — the id, constant across resumes by construction)
// or overwrite their fields with the latest one (gopilog's reader/model), and
// the last-run fields are the ones a reader wants.
//
// resumedFrom empty is the ordinary fresh-session path: NO witness keys are
// written, so absence keeps its single meaning ("this run started the session")
// — see RunIdentity's doc and the keys' declaration (Rule 15a).
//
// WHAT IS NOT SEEDED, stated rather than implied: the `iteration` field this
// run's `request` rows carry restarts at 0, because the seed is the count of
// request rows already in the file and reading it needs a session-event parser —
// internal/gopilog owns that, and it IMPORTS this package, so calling it here
// would be a cycle. The turn count a reader sees is unaffected (turns are derived
// from the appended rows themselves, see gopilog.SessionTurnBounds), and the
// round ORDINAL a reader sees is the position of the request row in the file,
// not this field. The field is a within-run index; a resumed file therefore has
// one index per run. Making it continuous means a seeding message that does not
// exist yet — a second parser in this package is the defect that would introduce.
func NewSessionLoggerForResumedRun(model string, rawLogPath string, sessionID string, taskID []string, resumedFrom string) (*SessionLogger, error) {
	tid := ""
	if len(taskID) > 0 {
		tid = strings.TrimSpace(taskID[0])
	}

	// A REPLAY RUN MINTS NO SESSION (the other half of rawlog.go's guard, and
	// the reason cmd/agent/chat_sessions_root.go no longer exists). Everything
	// below this line is the MINT: the run directory, the JSONL opened
	// append-or-create, session_start, metadata.json. A replay reads a session
	// that already happened; the session it would write here is a discarded
	// copy — and until this guard existed, hiding that copy took a whole
	// relocation of the sessions root into a scratch tree, which in turn made
	// "wrote to a different place" look like "wrote nothing". With the writes
	// refused on the profile side (internal/replaymode) and the mint refused
	// here, the replay lands NOWHERE: no directory, no JSONL, no metadata, and
	// the operator's own sessions tree is untouched by construction.
	//
	// The check sits AFTER the taskID parse (the announcement names the layout
	// it did not create) and BEFORE sessionDirForID, i.e. before anything is
	// created — a guard one line lower would leave the empty directory behind.
	//
	// A nil logger with a nil error is the caller's documented "best-effort"
	// shape: NewAgent prints a non-nil error and continues without a logger, and
	// every reader below that point already tolerates a nil SessionLog (52
	// guard sites). The refusal is announced anyway, so the absence is a stated
	// fact rather than a silent hole (Rule 15).
	if replaymode.Active() {
		layout := "sessions/<session-uuid>/session.jsonl"
		if tid != "" {
			layout = "sessions/<task_id>/<run-uuid>.jsonl"
		}
		replaymode.Noticef("%s\n", replaymode.Refuse("mint this run's session record ("+layout+", plus its metadata.json)"))
		return nil, nil
	}

	dir, err := sessionDirForID(sessionID, tid)
	if err != nil {
		return nil, fmt.Errorf("session log: %w", err)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("session log: cannot create %s: %w", dir, err)
	}

	path := filepath.Join(dir, SessionLogFileName(sessionID, tid))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("session log: cannot create %s: %w", path, err)
	}
	return newSessionLoggerOnFile(f, path, model, sessionID, tid, rawLogPath, resumedFrom)
}

// newSessionLoggerOnFile builds the logger on an ALREADY-OPEN record file and
// writes the run's header rows into it. It is the ONE header writer, shared by
// the mint path (NewSessionLoggerForResumedRun, which creates the file) and the
// continue path (openExistingSessionRecord, which only appends) — two spellings
// of "what a session_start says" would let a resumed segment's header drift
// from a fresh one, and that header is the only thing that says WHY a record
// holds more than one of them.
func newSessionLoggerOnFile(f *os.File, path string, model string, sessionID, tid, rawLogPath, resumedFrom string) (*SessionLogger, error) {
	// THE APPEND GUARD LIVES HERE, at the ONE seam both writers pass through
	// (this function is the only place a record's header is written, so the
	// guard cannot be bypassed by a caller that picks the other constructor).
	// It runs BEFORE the header reaches the buffer, because the damage it
	// prevents is caused by the header itself: see
	// refuseUnterminatedRecordTail. On refusal the freshly opened descriptor is
	// closed here — the caller's file was opened for this writer alone.
	if err := refuseUnterminatedRecordTail(path); err != nil {
		f.Close() //stylegate:ignore close on the refusal path; the write never happened, and the error being returned is the one that matters
		return nil, err
	}

	workdir, _ := os.Getwd() //stylegate:ignore cwd label recorded with a session; empty degrades to the documented fallback

	sl := &SessionLogger{
		file:      f,
		buf:       bufio.NewWriterSize(f, sessionLogSyncThreshold),
		sessionID: sessionID,
		taskID:    tid,
		startTime: time.Now(),
		path:      path,
		// THE TERMINAL IS MEASURED HERE, before the first line is written, so
		// session_start is the record's own header rather than a field a later
		// reader has to hunt for. The measurement is a property of THIS process
		// (terminalgeom.Standard asks stdout, stderr, stdin in that order), and
		// it is taken once: a SIGWINCH mid-session changes the terminal, not the
		// session's recording of where it began.
		geometry: terminalgeom.Standard(),
		// THE BINARY IS RESOLVED HERE, for the same reason: which artifact ran
		// this run is a fact about the PROCESS, knowable only at start-up (the
		// path is what it is, and the artifact it points at may be rebuilt or
		// replaced the moment this run ends). The declared half comes from
		// gopiversion.Revision, the variable the build recipe's `-X` operand
		// writes; the attested half is read from the running artifact inside
		// sessionevent.ArtifactIdentityOf, which resolves the precedence.
		artifact: sessionevent.ArtifactIdentityOf(executablePathForSessionRecord(), gopiversion.Revision),
	}

	// Write session_start event, then sync immediately — must be the first
	// line on disk (a crash before the first round must still show it).
	start := map[string]interface{}{
		"type":      sessionevent.EventTypeSessionStart,
		"sessionId": sessionID,
		"timestamp": time.Now().Format(time.RFC3339Nano),
		"model":     model,
		"workdir":   workdir,
		// Liveness stamp: the PID that owns this file, so a LATER gopi run
		// can tell "still being written" from "writer died" (see
		// ClassifyOrphanSession). Without it every live session looks like a
		// crash the moment the process is a second old.
		ProcessAliveStampField: os.Getpid(),
		// The terminal this run was ON, as sessionevent.TerminalGeometry: a
		// MEASURED cols×rows, or an explicit "unknown" with the reason. It is
		// written as the struct, never as two bare ints, so the zero value
		// cannot be mistaken for a size (Rule 15a) — and a session recorded
		// before this field existed simply has no key here, which every reader
		// treats as "no geometry recorded" rather than as a fallback size.
		sessionevent.FieldTerminalGeometry: sl.geometry,
		// Which BINARY this run is, so a later reader can pin every `Ran:`/
		// `Result:` line of the run's report to a code version. Written as the
		// struct, never as loose fields: the source is a required discriminant,
		// so "the record says something I do not know" is decodable as distinct
		// from "the record says nothing" (Rule 15).
		sessionevent.FieldArtifactIdentity: sl.artifact,
	}
	if tid != "" {
		start["taskId"] = tid
	}
	if rawLogPath != "" {
		start["rawLogPath"] = rawLogPath
	}
	// THE RESUME WITNESS (epic: `gopi chat --resume <id>` minted a second
	// session). Written only when this run CONTINUES a session — absence keeps
	// its single meaning ("this run started the session"), so a reader can tell
	// the first run of a session from its third without counting headers.
	if resumedFrom != "" {
		start[sessionevent.SessionStartFieldResumedFrom] = resumedFrom
		if rawLogPath != "" {
			start[sessionevent.SessionStartFieldResumedFromRawLog] = rawLogPath
		}
	}
	// THE HEADER WRITE AND ITS FLUSH ARE THE ONE PAIR WHOSE FAILURE MUST NOT BE
	// SILENT. Until this guard, both returns were discarded:
	//
	//	sl.log(start)
	//	sl.Sync()
	//
	// so a short write (ENOSPC, EINTR) wrote NOTHING and reported nothing: the
	// record kept no first line, and the first symptom surfaced hours later as an
	// anonymous parse error in a reader (or, worse, as a session that simply
	// looked as if it had never started). The measured case is the torn-tail
	// family in torn_tail.go — the write leg and the read leg of the same defect.
	// Both errors now travel: on the logger, for the two constructors that can
	// hand them to the run, and on stderr, because a session record that lost its
	// header is a fact the operator must see even from a caller that ignores the
	// error.
	if err := sl.log(start); err != nil {
		sl.err = fmt.Errorf("session log: this run's session_start could not be written to %s — the record has NO header for this run: %w", path, err)
	} else if err := sl.Sync(); err != nil {
		sl.err = fmt.Errorf("session log: this run's session_start could not be flushed to %s — it may exist only in the page cache: %w", path, err)
	}
	if sl.err != nil {
		fmt.Fprintf(os.Stderr, "[gopi] %v\n", sl.err)
	}
	sl.writeMetadataJSON(model, rawLogPath)

	return sl, sl.err
}

// executablePathForSessionRecord resolves the path of the executable THIS
// process is, for the session's artifact-identity block.
//
// A failure here is NOT swallowed (Rule 15): the block is a session record
// whose whole purpose is to make "which binary ran this run" answerable, so a
// record that cannot name the binary must say so somewhere — the run's stderr,
// where a reader of the run's log sees it. The returned empty path makes the
// block carry a stated absence (the block's Source/Reason arm still describes
// the artifact's revision), never a silent one.
func executablePathForSessionRecord() string {
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[gopi] session artifact identity: os.Executable failed (%v) — the record cannot name the binary that ran this run\n", err)
		return ""
	}
	return self
}

// writeMetadataJSON merges the run-time session metadata into metadata.json
// in the session run dir (session-dir-unify S1: task_id / session_id /
// model / created; uuid-taskid: the file may ALREADY hold the submit-time
// skeleton — summary/payload/status/source/manifest written by
// taskWriteSubmitMetadata — so this is a read-modify-write MERGE, never a
// blind overwrite, or the submit fields would vanish on first run). A
// missing/absent file starts fresh; a corrupt file is replaced (the
// run-time fields are the authoritative layer). Best-effort — a metadata
// write failure is logged and never fails the session.
func (sl *SessionLogger) writeMetadataJSON(model, rawLogPath string) {
	meta := map[string]interface{}{
		"session_id": sl.sessionID,
		"model":      model,
		"created":    sl.startTime.UTC().Format(time.RFC3339),
		// The SAME geometry object session_start carries (one measurement, two
		// readers): the JSONL is the recording a replay reads, and metadata.json
		// is what a human or a tool greps for the run's own facts. Both name the
		// field through sessionevent, so the two can never disagree about the
		// key — a merge that overwrote one and not the other would be a
		// divergence nothing else would catch.
		sessionevent.FieldTerminalGeometry: sl.geometry,
		// The SAME artifact-identity object session_start carries — one
		// computation, two readers, one key (the terminal-geometry precedent
		// directly above). metadata.json is what a human or a tool greps for
		// the run's own facts, and "which binary ran this run" is one of them.
		sessionevent.FieldArtifactIdentity: sl.artifact,
	}
	if sl.taskID != "" {
		meta["task_id"] = sl.taskID
	}
	if rawLogPath != "" {
		meta["raw_log_path"] = rawLogPath
	}
	path := filepath.Join(sessionlayout.RecordContainer(sl.path), "metadata.json")
	if b, err := os.ReadFile(path); err == nil {
		var prev map[string]interface{}
		if json.Unmarshal(b, &prev) == nil && prev != nil {
			// Preserve submit-time skeleton fields; run-time fields win on
			// a key collision (session_id/model/created/task_id are the
			// session's own truth).
			for k, v := range meta {
				prev[k] = v
			}
			meta = prev
		}
	}
	out, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "[gopi] metadata.json write failed (continuing): %v\n", err)
	}
}

// TaskID returns the ledger task_id this session belongs to ("" for bare
// prompt/chat sessions).
func (sl *SessionLogger) TaskID() string {
	return sl.taskID
}

// SessionID returns the session ID string (UUID v4).
func (sl *SessionLogger) SessionID() string {
	return sl.sessionID
}

// Path returns the full path to the session JSONL file.
func (sl *SessionLogger) Path() string {
	return sl.path
}

// log marshals event to JSON and appends it as a single line to the session
// buffer (R5). Thread-safe via mutex. The buffer is flushed to disk at the
// round boundary (loop.go Sync), at the task end (Close), when it reaches
// sessionLogSyncThreshold, and always right before a tool result is returned
// (LogToolResult — crash safety for the tool_use/tool_result pair). A SIGKILL
// loses at most the buffered tail of the log.
func (sl *SessionLogger) log(event map[string]interface{}) error {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	// Add timestamp if the caller hasn't set one.
	if _, ok := event["timestamp"]; !ok {
		event["timestamp"] = time.Now().Format(time.RFC3339Nano)
	}

	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	n, err := sl.buf.Write(append(data, '\n'))
	sl.buffered += n
	if err == nil && sl.buffered >= sessionLogSyncThreshold {
		err = sl.syncLocked()
	}
	return err
}

// Sync flushes the buffer to the file and fsyncs it to disk. Called at each
// round boundary (before the next LLM round-trip) and at the task end. No-op
// when nothing is buffered (avoids empty fsyncs).
func (sl *SessionLogger) Sync() error {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	return sl.syncLocked()
}

// syncLocked flushes the buffer and fsyncs the file. Caller must hold mu.
// Skips the fsync when the buffer is empty — the common case at round
// boundaries after a LogToolResult already synced.
func (sl *SessionLogger) syncLocked() error {
	if sl.buffered == 0 {
		return nil
	}
	if err := sl.buf.Flush(); err != nil {
		return err
	}
	sl.buffered = 0
	return sl.file.Sync()
}

// LogUser writes a user message event whose source is not known to the caller.
//
// THE SOURCE IS THE WHOLE POINT OF LogUserFrom, and this wrapper is the
// backward-compatible half of that split: before 2026-10-25 the JSONL recorded
// no source at all, so a reader could not tell an operator's own line from a
// line the model typed into its own input (`chat_surface action=type`) or one a
// queued job notice supplied. That is not cosmetic — a self-driven turn is
// counted as a TURN (the operator's ruling: from the outside the two are
// indistinguishable), so the LEDGER must be able to say which one it was even
// when the reader cannot.
//
// Callers that DO know the face (cmd/agent's chat seal, which computes it via
// chatSealSource) call LogUserFrom; every other caller arrives here and is
// recorded as inputsource.Human. That default is the honest one for the
// pre-2026-10-25 rows too: every user row written before this change came from
// the person, so a reader meeting a row with no `source` field is reading a
// human turn, not a guess.
func (sl *SessionLogger) LogUser(promptID, content string) error {
	return sl.LogUserFrom(promptID, content, inputsource.Human)
}

// LogUserFrom writes a user message event stamped with the FACE the line came
// from (internal/inputsource: Human, LLM, Queue, …).
//
// The field is named `source` and sits beside `promptId` rather than inside the
// message body: the body is what goes on the wire to the model, and the model
// must not learn that a line was self-typed. This is bookkeeping for the
// operator's own reader (gopi session log), not part of the conversation.
func (sl *SessionLogger) LogUserFrom(promptID, content, source string) error {
	return sl.LogUserFromAt(promptID, content, source, time.Time{})
}

// LogUserFromAt is LogUserFrom for a caller that has ALREADY decided the moment
// the row is written at, and needs that exact value back for a banner.
//
// WHY THE CALLER MAY OWN THE STAMP. A turn banner is painted when the turn
// STARTS, and the row is written a moment later inside the runner — so a banner
// that painted its own clock and a row that minted its own would disagree by
// however long the gap was, and could straddle a second boundary. Handing the
// moment IN makes the two the same value: one mint, two readers (the record and
// the screen), which is the only shape in which "the banner shows when this turn
// opened" is a fact rather than a coincidence.
//
// A ZERO at IS THE ORDINARY CASE (every caller that does not care) and it mints
// the moment here — the same single `time.Now()` `log` used to add, moved up so
// this function can state it explicitly instead of leaving it to the writer's
// default.
func (sl *SessionLogger) LogUserFromAt(promptID, content, source string, at time.Time) error {
	if at.IsZero() {
		at = time.Now()
	}
	ev := map[string]interface{}{
		"type": "user",
		"message": map[string]interface{}{
			"role":    "user",
			"content": content,
		},
		"promptId": promptID,
		// THE ONE MOMENT, spelled in the one layout `log` uses for every other
		// row — so this row is not a second timestamp format in the same file.
		"timestamp": at.Format(time.RFC3339Nano),
	}
	// Rule 15a: an unknown face stays ABSENT rather than being stamped with a
	// spelling this vocabulary did not choose — the reader's default (human)
	// then applies, and it is the same default an old file gets.
	if source != "" {
		ev["source"] = source
	}
	// THE TURN COUNTER ADVANCES UNDER THE WRITER'S OWN PREDICATE, BEFORE the
	// row is written: a surface that asks "which turn is starting now?" right
	// after this call returns must get the number this row IS. The row's TEXT
	// is passed with the reader's own face default (sessionevent.TurnRowSource)
	// so the writer and internal/gopilog's reader cannot disagree about whether
	// a given row opened a turn.
	if sessionevent.IsTurnOpeningRow(content, sessionevent.TurnRowSource(source)) {
		sl.mu.Lock()
		sl.turns++
		sl.mu.Unlock()
	}
	return sl.log(ev)
}

// TurnsSoFar reports how many turn-OPENING `user` rows this logger has written
// — this run's own contribution to the session's turn numbering.
//
// WHY IT IS HERE, beside RequestsSoFar and for the same reason. `gopi web` puts
// "turn N" on the page, and N has to be the number `gopi session log --turns`
// gives the same row: the log's turn axis is internal/gopilog's
// SessionTurnBounds (one boundary per row sessionevent.IsTurnOpeningRow
// accepts). A surface that re-parsed the file could not count the row it is
// about to render — the writer's buffer has not been flushed — so the count
// rides the writer, and "the session's turn ordinal for the turn now starting"
// is turnBase + TurnsSoFar() + 1 (turnBase being whatever the record already
// held before this run wrote anything).
//
// BOTH HALVES USE ONE PREDICATE (internal/sessionevent), which is what makes
// the two arithmetic halves add up to the reader's own number instead of to a
// second opinion about it.
func (sl *SessionLogger) TurnsSoFar() int {
	if sl == nil {
		return 0
	}
	sl.mu.Lock()
	defer sl.mu.Unlock()
	return sl.turns
}

// LogAssistantText writes an assistant text content block event.
func (sl *SessionLogger) LogAssistantText(content string) error {
	return sl.log(map[string]interface{}{
		"type": "assistant",
		"message": map[string]interface{}{
			"role": "assistant",
			"content": []map[string]interface{}{
				{"type": "text", "text": content},
			},
		},
	})
}

// LogTurnInterrupted writes the per-turn interrupt witness: one turn of a
// still-open session was abandoned by the operator (a run-scoped cancel — Ctrl+C
// at the incoming prompt of `gopi chat`), NOT by a process-level signal.
//
// It is the reader's answer to three questions a partial assistant record cannot
// answer on its own: that the truncation was DELIBERATE (reason), WHO ended it
// (source), and HOW MUCH of the answer had reached the screen (partialChars). The
// event carries no "message" key, like the session's other structural events, so
// every formatter that switches on event type without a default skips it
// structurally (internal/gopilog).
func (sl *SessionLogger) LogTurnInterrupted(reason, source string, partialChars int) error {
	return sl.log(map[string]interface{}{
		"type":          sessionevent.EventTypeTurnInterrupted,
		"reason":        reason,
		"source":        source,
		"partial_chars": partialChars,
	})
}

// LogResumeOptions records the OPTIONS this run was started with, so a later
// run can restore them without the operator replaying the command line
// (sessionevent.EventTypeResumeOptions carries the whole rationale).
//
// It is called ONCE, at start-up, and the caller SYNCs: the record has to be
// on disk BEFORE the first turn — the whole point is that a run which crashes
// mid-turn leaves behind the flags it was started with.
//
// An empty flags slice records NOTHING and returns nil: a run with no options
// has no options to restore, and an event carrying an empty array would make
// "nothing to record" indistinguishable from "recorded an empty command line"
// for every future reader. The absence is the statement (Rule 15).
func (sl *SessionLogger) LogResumeOptions(flags []string, workdir string) error {
	if len(flags) == 0 {
		return nil
	}
	return sl.log(map[string]interface{}{
		"type":                                   sessionevent.EventTypeResumeOptions,
		sessionevent.ResumeOptionsFieldFlags:     flags,
		sessionevent.ResumeOptionsFieldSessionID: sl.sessionID,
		sessionevent.ResumeOptionsFieldWorkdir:   workdir,
	})
}

// LogAssistantThinking writes a thinking content block event.
// Modeled after Claude JSONL format: {"type": "assistant", "message": {
//
//	"role": "assistant", "content": [{"type": "thinking", "thinking": "..."}]}}
func (sl *SessionLogger) LogAssistantThinking(content string) error {
	return sl.log(map[string]interface{}{
		"type": "assistant",
		"message": map[string]interface{}{
			"role": "assistant",
			"content": []map[string]interface{}{
				{"type": "thinking", "thinking": content},
			},
		},
	})
}

// LogAssistantToolUse writes an assistant tool_use content block event.
func (sl *SessionLogger) LogAssistantToolUse(tc deepseek.ToolCall) error {
	return sl.log(map[string]interface{}{
		"type": "assistant",
		"message": map[string]interface{}{
			"role": "assistant",
			"content": []map[string]interface{}{
				{"type": "tool_use", "id": tc.ID, "name": tc.Name, "input": tc.Arguments},
			},
		},
	})
}

// LogToolResult writes a tool_result event and syncs before returning — the
// result must be on disk before it is handed to the caller, so a crash can
// never leave a session log that ends on a tool_use without its result.
func (sl *SessionLogger) LogToolResult(toolUseID, content string) error {
	if err := sl.log(map[string]interface{}{
		"type": "tool_result",
		"message": map[string]interface{}{
			"role": "user",
			"content": []map[string]interface{}{
				{"tool_use_id": toolUseID, "type": "tool_result", "content": content},
			},
		},
	}); err != nil {
		return err
	}
	return sl.Sync()
}

// LogRequest writes a request metadata event whose four counters are THIS
// REQUEST'S OWN INCREMENT (requestusage.BaseDelta) — the task path's quantity.
// It also counts the event (requests), which is what makes the round-level note
// index's round number EXACTLY the definition this repository already uses for
// "which round was it": the ordinal of this session's `request` events
// (sessionevent's numbering, read by `gopi session log <id> <round>` and by
// gopilog's reader).
//
// THE QUANTITY IS WRITTEN ON THE ROW, not left for a reader to guess: see
// internal/requestusage for the defect that cost every chat session its
// headline (epic 01a195b0). A caller holding the CostTracker's RUNNING TOTALS
// must call LogRequestCumulative instead.
func (sl *SessionLogger) LogRequest(iteration int, promptTokens, cacheHit, cacheMiss, completionTokens int) error {
	return sl.logRequest(iteration, promptTokens, cacheHit, cacheMiss, completionTokens, requestusage.BaseDelta)
}

// LogRequestCumulative writes a request metadata event whose four counters are
// the writer's RUNNING TOTALS since its run segment began
// (requestusage.BaseCumulative) — the chat path's quantity, which is what its
// CostTracker holds.
//
// A reader folds these by taking each segment's LAST reading
// (requestusage.Counted), so a reader that summed them — which is what both
// session summary surfaces did — reported a session's own running totals added
// together: measured 48.6x on 34bb9adc-0618-4e05-b849-6bd15757dec6
// (393,314,088 against 8,086,768).
func (sl *SessionLogger) LogRequestCumulative(iteration int, promptTokens, cacheHit, cacheMiss, completionTokens int) error {
	return sl.logRequest(iteration, promptTokens, cacheHit, cacheMiss, completionTokens, requestusage.BaseCumulative)
}

// logRequest is the ONE writer of a `request` row (Rule 14): the marker keys
// and the four counters are written together so a row can never carry one
// without the other.
func (sl *SessionLogger) logRequest(iteration int, promptTokens, cacheHit, cacheMiss, completionTokens int, base requestusage.RequestUsageBase) error {
	sl.mu.Lock()
	sl.requests++
	sl.mu.Unlock()
	return sl.log(map[string]interface{}{
		"type":                        "request",
		"iteration":                   iteration,
		"prompt_tokens":               promptTokens,
		"cache_hit":                   cacheHit,
		"cache_miss":                  cacheMiss,
		"completion_tokens":           completionTokens,
		requestusage.FieldTaggedUsage: true,
		requestusage.FieldUsageBase:   string(base),
	})
}

// LogTokenReconciliation writes this REQUEST's local-vs-server prompt count, so
// the engine's own prediction and the API's answer land side by side in the one
// place both already live (the session JSONL).
//
// predicted is what the LOCAL tokenizer priced for the body that was just sent;
// server is the prompt-side count the response reported. delta is server minus
// predicted — spelled by the caller because only the caller knows which pair of
// readings it has (internal/agent/context_tokens.go's CalibratedTokens is the
// prediction the STATUS ROW shows, and the row is the number a reader compares
// against the server, so that is the pair this event reconciles).
//
// A ZERO IS NEVER FABRICATED (Rule 15): a caller with no server measurement does
// not call this at all — the row would otherwise claim the server charged nothing,
// which is the exact failure mode `gopilog` and CalibratedTokens both refuse. The
// caller also owns "is this a measured frame", the same way it owns it for
// SetPromptTokens.
func (sl *SessionLogger) LogTokenReconciliation(predicted, server, delta int, frame string, anchored bool) error {
	row := map[string]interface{}{
		"type":                       sessionevent.EventTypeTokenReconciliation,
		reconFieldPredictedTokens:    predicted,
		reconFieldServerPromptTokens: server,
		reconFieldDeltaTokens:        delta,
		reconFieldFrame:              frame,
		reconFieldPredictionAnchored: anchored,
	}
	// THE DERIVED PERCENTAGE IS WRITTEN ONLY WHEN IT EXISTS. A field carrying a
	// placeholder would be read as a reading — "delta_pct_bp": 0 says the two
	// numbers agreed exactly, which is the opposite of "no percentage could be
	// taken". So the key is ABSENT instead (Rule 15: absence is the only spelling
	// of "not measured" that cannot be mistaken for a measurement).
	if bp, ok := deltaPercentBasisPoints(predicted, delta); ok {
		row[reconFieldDeltaPctBasisPoints] = bp
	}
	return sl.log(row)
}

// RequestsSoFar reports how many `request` events this logger has written.
//
// WHY THE COUNT LIVES HERE, and why a caller adds ONE to it. Every path that
// walks a round logs its request AFTER the round's own response has been handled
// (loop.go's task loop logs at the end of the iteration, both chat paths log
// after the round's text was accepted), so at the moment a round's note is
// recorded the session has logged every request EXCEPT the one for the round in
// flight. "RequestsSoFar + 1" is therefore the ordinal of the round being
// recorded — the same numbering sessionevent.RoundNumberOf defines from the
// loop's 0-based iteration, computed from the session's own log rather than from
// a per-path cursor. That matters on the chat surface, where the round cursor is
// a property of an agent that may be re-created per turn while the SESSION LOG
// is the one thing that spans the conversation.
func (sl *SessionLogger) RequestsSoFar() int {
	if sl == nil {
		return 0
	}
	sl.mu.Lock()
	defer sl.mu.Unlock()
	return sl.requests
}

// LogSessionEnd writes the session_end event. Called once at agent exit.
// failure, when non-nil, extends the event with an "error" block (crash-dump
// epic ⑤ at_exit semantics: a normal exit_code=0 session_end carries NO
// error block — the block's presence is itself the failure marker).
//
// THE ENDING CAUSE IS A REQUIRED ARGUMENT, not an optional one (task 01a163f8).
// It used to be derivable from nothing at all: exit_code=0 covers /quit, a
// closed stdin and a natural completion alike, so the record could say THAT a
// session ended and never WHY. Passing it in is what makes the caller state what
// it knows; a caller that does not know passes EndCause{} and the event records
// unknown rather than silently borrowing another leg's meaning.
func (sl *SessionLogger) LogSessionEnd(exitCode int, totalIterations int, totalTokens int, elapsedSec float64, failure *ErrorRecord, cause EndCause) error {
	reason := cause.Reason
	if reason == sessionevent.SessionEndReasonNone {
		reason = sessionevent.SessionEndReasonUnknown
	}
	event := map[string]interface{}{
		"type":                             sessionevent.EventTypeSessionEnd,
		"exit_code":                        exitCode,
		"total_iterations":                 totalIterations,
		"total_tokens":                     totalTokens,
		"elapsed_sec":                      elapsedSec,
		sessionevent.SessionEndFieldReason: reason,
	}
	// The signal rides ONLY the reason it elaborates: one axis, so a record
	// cannot claim reason=quit beside signal=SIGHUP.
	if reason == sessionevent.SessionEndReasonSignalled && cause.Signal != "" {
		event[sessionevent.SessionEndFieldSignal] = cause.Signal
	}
	// Same discipline for the EOF kind: written only beside reason=eof.
	if reason == sessionevent.SessionEndReasonEof {
		kind := cause.EOFKind
		if kind == "" {
			kind = sessionevent.SessionEndEOFKindUnknown
		}
		event[sessionevent.SessionEndFieldEOFKind] = kind
	}
	if failure != nil {
		event["error"] = failure.SessionEndBlock()
	}
	return sl.log(event)
}

// EndCause is HOW a run ended — the fact session_end carried no field for until
// task 01a163f8, and the one an operator reads a kill afterwards to find.
//
// It is not the exit code and not the error block: a SIGTERM unwinds through a
// clean close-out (exit 1, category "signal") while a closed stdin exits 0 with
// no error at all, so three genuinely different deaths produced records a reader
// could not tell apart. Zero value = "the caller does not know", recorded as
// unknown (never omitted — an absent key has its own meaning: an old binary).
type EndCause struct {
	// Reason is one of the sessionevent.SessionEndReason* values. Empty means
	// unknown.
	Reason string
	// Signal is the terminating signal's name in signalrec's spelling
	// ("SIGHUP", "SIGTERM" — Rule 14: internal/agent never spells it itself).
	// Read only when Reason is SessionEndReasonSignalled.
	Signal string
	// EOFKind refines Reason == SessionEndReasonEof: WHICH end of input it was
	// (a key, or the source going away). Read only for that reason; empty is
	// recorded as unknown rather than omitted, because the two are different
	// events about the run and an operator reading the record afterwards cannot
	// reconstruct which happened from anything else.
	EOFKind string
}

// task_row_closed event field names (Rule 1b: the wire spelling of the
// terminal-write witness lives here, in its single definition site).
const (
	taskRowClosedKeyTaskID = "task_id"
	taskRowClosedKeyStatus = "status"
	taskRowClosedKeyReason = "reason"
	taskRowClosedKeyError  = "error"
)

// LogTaskRowClosed writes the task_row_closed event: the session JSONL's
// witness that the END hook performed the ledger's terminal write for this
// run's own row, with the resulting status and the run's end reason.
//
// It exists because the row's terminal status must be provable from the run's
// own record (see sessionevent.EventTypeTaskRowClosed): a hook that returns
// early — the truncated-run defect, where the row stayed `in_progress` forever
// while the status rule itself was already correct — left no trace at all, so
// "never wrote" was indistinguishable from "wrote somewhere else".
//
// A FAILED write is recorded with its error, never dropped (Rule 15): the
// event's absence and its presence-with-an-error are different facts.
func (sl *SessionLogger) LogTaskRowClosed(taskID, status, reason, writeErr string) error {
	event := map[string]interface{}{
		"type":                 sessionevent.EventTypeTaskRowClosed,
		taskRowClosedKeyTaskID: taskID,
		taskRowClosedKeyStatus: status,
	}
	if reason != "" {
		event[taskRowClosedKeyReason] = reason
	}
	if writeErr != "" {
		event[taskRowClosedKeyError] = writeErr
	}
	return sl.log(event)
}

// ErrorRecord is the collected error context of a failed session (crash-dump
// epic ①: one collection path for LLM errors, stream errors and panics).
// It feeds both the session_end error block (bounded) and the dump file
// (complete). Built by the agent loop; nil on healthy sessions.
type ErrorRecord struct {
	// Category is the bug036 taxonomy value: account | network | 5xx | 4xx |
	// vendor | panic (account = the provider ACCOUNT is unusable: no retry
	// and no resume can help, see Remedy).
	Category string
	// HTTPStatus of the failed response (0 = transport/vendor/panic).
	HTTPStatus int
	// ErrorBody is the bounded error body / error text for session_end.
	ErrorBody string
	// TransportErr is the raw transport error text (network failures).
	TransportErr string
	// LastRequestDigest summarizes the last LLM request: model + message
	// count + last message role + last request bytes (bounded).
	LastRequestDigest string
	// FailedIteration is the round index the failure happened on.
	FailedIteration int
	// RecoverableHint: whether resume plausibly helps (deepseek taxonomy).
	RecoverableHint bool
	// Remedy is the operator-facing condition + remedy text for an
	// ACCOUNT-LEVEL failure ("account out of balance (code 1113) … top up or
	// switch provider"), empty for every other category. It rides the
	// session_end error block, the crash dump and the ledger digest so the
	// reader never has to re-derive the remedy from a status code.
	Remedy string `json:",omitempty"`
	// LastRequest is the COMPLETE last request body JSON (dump file only —
	// never goes into session_end, it can be megabytes after compact).
	LastRequest json.RawMessage
	// LastResponse is the COMPLETE error response body (dump only).
	LastResponse string
	// PanicStack is the recovered panic stack trace (panic path only).
	PanicStack string
	// RecentToolCalls: names of the last N tools before the failure.
	RecentToolCalls []string
	// ElapsedSec / Iterations / Tokens — the same numbers session_end carries.
	ElapsedSec float64
	Iterations int
	Tokens     int
	// DumpPath is set after WriteCrashDump succeeds — recorded in the
	// session_end error block so the reader can find the full dump.
	DumpPath string `json:",omitempty"`
}

// recoverableKnown reports whether this record's recoverability is an
// ASSERTION a reader may act on, rather than the not-classified default
// (Rule 15a — false ≠ unknown).
//
// It is a METHOD and not a second field because the stored struct is already
// written to the ledger/session line and its shape is not this change's to
// migrate; the verdict is therefore DERIVED, from the one thing that
// distinguishes the two cases: whether the failure was classified at all.
// deepseek.CategoryUnknown is defined as "failure with neither status, body,
// nor transport text" — an unknowable error — so a record that carries it has
// had no verdict issued about it, while every other category was decided by
// the taxonomy (and a true hint can only come from a decided one).
func (er *ErrorRecord) recoverableKnown() bool {
	return er.RecoverableHint || er.Category != string(deepseek.CategoryUnknown)
}

// SummaryDigest renders the one-line failure digest appended to the
// task-ledger failed summary (notification pipe: PA reads the summary, not
// the JSONL). Bounded and greppable: "error(category=4xx http=400 …)".
//
// THE ZERO ARMS ARE LOAD-BEARING, NOT TERSENESS (measured 2026-10-29). The two
// fields that describe a failure which never reached the network are exactly the
// ones whose meaningful value is ZERO — `http=0` (no response ever arrived) and
// `iter=0` (the run died on its first iteration) — and this renderer used to
// OMIT both of them (`if er.HTTPStatus != 0`, `if er.FailedIteration > 0`). So
// the digest for the one failure shape a dispatcher could act on carried
// neither field, and a reader could not tell it from a digest that simply did
// not record them (Rule 15a: absence ≠ zero). Measured against the live ledger:
// 481 failed rows, ZERO of them carrying ` http=0` or ` iter=0`, and the
// transport-dead shape therefore invisible to every consumer of this pipe.
//
// So both are rendered ALWAYS. The cost is a longer line on the common
// failures; the benefit is that "the server never answered" and "no work was
// started" are STATED rather than inferred from silence, which is the whole
// contract of the digest. dump= stays conditional — an empty path is genuinely
// absent, not a meaningful empty.
func (er *ErrorRecord) SummaryDigest() string {
	var b strings.Builder
	fmt.Fprintf(&b, "error(category=%s", er.Category)
	fmt.Fprintf(&b, " http=%d", er.HTTPStatus)
	fmt.Fprintf(&b, " iter=%d", er.FailedIteration)
	switch {
	case er.RecoverableHint:
		fmt.Fprintf(&b, " recoverable=%t", er.RecoverableHint)
	case er.recoverableKnown():
		// Decided, and the verdict is "no" (a classified failure the
		// taxonomy scores unrecoverable — account-level, by construction).
		b.WriteString(" recoverable=false")
	default:
		// Never decided. Printing `false` here is the defect this arm exists
		// for: the two measured rows read `category=unknown
		// recoverable=false`, an unknowable error stamped as certainly
		// unrecoverable (Rule 15a).
		b.WriteString(" recoverable=unknown")
	}
	if er.DumpPath != "" {
		fmt.Fprintf(&b, " dump=%s", er.DumpPath)
	}
	if er.Remedy != "" {
		// The account-level remedy rides the ledger digest too: the notify
		// pipe carries only the summary, so a failed task must be triageable
		// from the summary alone (no JSONL/dump read required).
		fmt.Fprintf(&b, " remedy=%q", tailRunes(er.Remedy, remedyTextMax))
	}
	if er.ErrorBody != "" {
		digest := tailRunes(strings.Join(strings.Fields(er.ErrorBody), " "), autoTaskErrDigestMax)
		fmt.Fprintf(&b, " msg=%q", digest)
	}
	b.WriteString(")")
	return b.String()
}

// Named constants — Rule 1 (no magic numbers).
const (
	// sessionEndErrorBodyMax bounds the error body carried INSIDE the
	// session_end line (the dump file keeps the full body).
	sessionEndErrorBodyMax = 4 * 1024

	// dumpRequestMax caps the last-request body inside the dump file
	// (head-trim is wrong for requests — keep the TAIL: the last messages
	// are the failure context; the system prompt head is the least useful).
	dumpRequestMax = 1 << 20 // 1 MiB

	// dumpResponseMax caps the full error response body in the dump.
	dumpResponseMax = 64 * 1024

	// recentToolCallsMax caps how many tool names the record remembers.
	recentToolCallsMax = 10

	// panicStackMax bounds the recovered panic stack in the dump.
	panicStackMax = 16 * 1024

	// dumpFileSuffix completes the session file family: <id>.jsonl /
	// <id>.dump.json (the orphan-detection scan keys on this suffix).
	dumpFileSuffix = ".dump.json"

	// sessionJSONLSuffix completes the other half of the session file family
	// (<id>.jsonl / <id>.dump.json). Every scan/trim in this package goes
	// through it; the dump flag file is the SAME base name with
	// dumpFileSuffix instead — TrimSuffix(sessionJSONLSuffix) is what keeps
	// the two families from drifting into each other. Named because FOUR
	// sites must agree on it — the logger's path builder, the orphan scan's
	// filter, and the dump↔session path mapping in both directions (Rule
	// 1b/13: a bare ".jsonl" in any of them silently breaks the mapping that
	// retires stale dumps). The VALUE comes from internal/sessionlayout, the
	// single exported home (Rule 14) — this alias keeps the package's own
	// call sites reading naturally without respelling the value.
	sessionJSONLSuffix = sessionlayout.JSONLSuffix

	// bareSessionLogName is the bare-session run file name (a session dir
	// holds exactly one: <uuid>/session.jsonl; task-run dirs hold
	// <task_id>/<run-uuid>.jsonl instead — see SessionLogFileName). The value
	// comes from internal/sessionlayout.RecordName, the SSOT the session-tree
	// READERS classify by (Rule 14: one value, one home).
	bareSessionLogName = sessionlayout.RecordName

	// orphanLiveWindow is the quiet period a session JSONL must have been
	// UNTOUCHED for before its missing session_end is read as a death (Rule
	// 1/2: named default, one env override reader in
	// BackfillOrphanCrashDumps).
	//
	// Why a window at all: a session with no session_end is either DEAD
	// (kill -9 / panic /power loss) or simply STILL RUNNING — its JSONL is
	// appended turn by turn and its session_end is written only at exit, so
	// "no end" alone cannot tell the two apart. The mtime can: any live
	// session writes an event well within the window (each LLM round logs
	// the request/assistant/tool events, and a single round is bounded by
	// the tool-call timeout, DefaultBashTimeout=120s). 10 minutes is ~5×
	// that bound, so the guard never hides a real crash: after a kill -9 the
	// mtime freezes at the moment of death and the next startup past the
	// window still backfills the dump.
	//
	// 15 minutes rather than the 10 above: the incident's false positives
	// (14 dumps) were dumps written for sessions that were alive at the
	// moment of SOME OTHER start (including the same task's child), and the
	// concurrent long-task case (an agent in a single long bash/turn) is the
	// remaining flake risk — a wider window costs only a delay in
	// visibility of a genuine crash, never its loss.
	orphanLiveWindow = 15 * time.Minute

	// orphanLiveWindowSecEnv overrides orphanLiveWindow (seconds, > 0;
	// 0 or unset = the default; invalid = default + stderr warning). It
	// exists so a test — or an operator triaging right after a crash — can
	// pin the boundary without waiting out the window.
	orphanLiveWindowSecEnv = "GOPI_ORPHAN_LIVE_WINDOW_SEC"

	// autoTaskErrDigestMax bounds the error-message digest inside the
	// task-ledger failed summary (the summary is a single-line ledger row).
	autoTaskErrDigestMax = 200

	// remedyTextMax bounds the operator remedy text carried by the
	// session_end error block, the crash dump and the ledger digest (the
	// message is one greppable line — the cap is a runaway backstop).
	remedyTextMax = 1024
)

// tailRunes returns at most max trailing runes of s (head-trimmed).
func tailRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return "…(head trimmed)…" + string(r[len(r)-max:])
}

// SessionEndBlock renders the bounded error block for the session_end event.
func (er *ErrorRecord) SessionEndBlock() map[string]interface{} {
	block := map[string]interface{}{
		"category":            er.Category,
		"failed_iteration":    er.FailedIteration,
		"recoverable_hint":    er.RecoverableHint,
		"last_request_digest": er.LastRequestDigest,
	}
	if !er.recoverableKnown() {
		// Asserted "no", or nothing asserted? The stored field cannot say
		// (false is both), so the reader is given the distinction rather than
		// having to guess it (Rule 15a — false ≠ unknown). Read as: the
		// failure was CLASSIFIED and recoverability was decided by that
		// classification. An unclassified failure carries
		// recoverable_hint=false with recoverable_known=false, and no reader
		// may act on it as a verdict.
		block["recoverable_known"] = false
	}
	if er.HTTPStatus != 0 {
		block["http_status"] = er.HTTPStatus
	}
	if er.ErrorBody != "" {
		block["error_body"] = tailRunes(er.ErrorBody, sessionEndErrorBodyMax)
	}
	if er.TransportErr != "" {
		block["transport_error"] = tailRunes(er.TransportErr, sessionEndErrorBodyMax)
	}
	if er.Remedy != "" {
		// Account-level only: the operator condition + remedy, bounded.
		block["remedy"] = tailRunes(er.Remedy, remedyTextMax)
	}
	if er.DumpPath != "" {
		block["dump_path"] = er.DumpPath
	}
	return block
}

// dumpPath returns the dump file path for this session — the sibling of the
// session JSONL in its run directory (crash-dump epic naming family):
// <run-dir>/<jsonl-basename-without-.jsonl>.dump.json. For a task run that is
// <task_id>/<run-uuid>.dump.json; for a bare session
// <session-uuid>/session.dump.json (both co-located with the JSONL).
func (sl *SessionLogger) dumpPath() string {
	return strings.TrimSuffix(sl.path, sessionJSONLSuffix) + dumpFileSuffix
}

// WriteCrashDump writes <session-id>.dump.json next to the session JSONL:
// the complete last request body + complete error response + run context
// (elapsed/iterations/tokens/recent tool calls). Best-effort — a dump write
// failure never masks the real error; the path is recorded on the ErrorRecord
// when it succeeds so session_end and the task summary can point at it.
func (sl *SessionLogger) WriteCrashDump(er *ErrorRecord) {
	if er == nil || sl == nil {
		return
	}
	doc := map[string]interface{}{
		"type":                crashDumpDocType,
		"session_id":          sl.sessionID,
		"timestamp":           time.Now().UTC().Format(time.RFC3339),
		"category":            er.Category,
		"http_status":         er.HTTPStatus,
		"error_body":          tailRunes(er.ErrorBody, dumpResponseMax),
		"request":             json.RawMessage(er.LastRequest),
		"last_request_digest": er.LastRequestDigest,
		"failed_iteration":    er.FailedIteration,
		"recoverable_hint":    er.RecoverableHint,
		"elapsed_sec":         er.ElapsedSec,
		"iterations":          er.Iterations,
		"total_tokens":        er.Tokens,
	}
	if er.TransportErr != "" {
		doc["transport_error"] = er.TransportErr
	}
	if er.LastResponse != "" {
		doc["error_response"] = tailRunes(er.LastResponse, dumpResponseMax)
	}
	if er.Remedy != "" {
		// Account-level only: the operator remedy is what makes the dump
		// actionable without re-deriving intent from the status code.
		doc["remedy"] = tailRunes(er.Remedy, remedyTextMax)
	}
	if er.PanicStack != "" {
		doc["panic_stack"] = tailRunes(er.PanicStack, panicStackMax)
	}
	if len(er.RecentToolCalls) > 0 {
		doc["recent_tool_calls"] = er.RecentToolCalls
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		sl.warnDumpWrite(err)
		return
	}
	if err := os.WriteFile(sl.dumpPath(), out, 0o644); err != nil {
		sl.warnDumpWrite(err)
		return
	}
	er.DumpPath = sl.dumpPath()
}

// warnDumpWrite reports a dump write failure to stderr (best-effort channel —
// the session log itself may be the thing that failed).
func (sl *SessionLogger) warnDumpWrite(err error) {
	fmt.Fprintf(os.Stderr, "[gopi] crash dump write failed (continuing): %v\n", err)
}

// sessionEndScanBackMax is how far (in bytes) the tail walk may travel back
// from the file end looking for the session_end line. The last events are all
// we need, and a bounded walk keeps a pathological (huge, event-free) file
// from degenerating into a full-file scan.
const sessionEndScanBackMax = 4096

// sessionEventTypeOf parses a candidate JSONL line and returns its "type"
// field ("" when the line is not a well-formed event object).
func sessionEventTypeOf(line []byte) string {
	var ev struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(line, &ev) != nil {
		return ""
	}
	return ev.Type
}

// HasSessionEnd reports whether the session JSONL on disk contains a
// session_end event within the last sessionEndScanBackMax bytes (orphan
// detection: a file without one = the process died before the graceful path —
// kill -9, panic before recover, power loss).
//
// The tail is walked backwards with an explicit MOVING end pointer: each
// candidate line is data[lineStart:lineEnd] bounded by the newline that
// terminates it, so a half-written trailing line (SIGKILL mid-write, no
// trailing newline) is dropped instead of being spliced onto the session_end
// line that precedes it. Pinning the end to the file end instead would make
// every candidate unparseable and misreport a cleanly-ended session as an
// orphan. Best-effort: unreadable/empty files report false.
//
// COST (this change): the bytes the walk sees come from sessionTailRead — a
// bounded window at the file end (sessionTailWindowBytes, ≤ 4 KB + 1 byte),
// never from os.ReadFile of the whole file. Same walk, same verdict, O(tail)
// IO instead of O(file): on the reference tree (28.5 k entries / 405 MB of
// JSONL) the sweep's 430.9 MB of allocation churn collapsed to a pooled 4 KB
// window per file (devdoc/crashdump-scan-cost.md — measured before/after).
// The equivalence is pinned by sessionend_tail_equivalence_test.go, which runs
// the OLD whole-file read (kept in that test file) and this function over the
// same real+fixture inputs and compares the verdicts.
func HasSessionEnd(path string) bool {
	data, ok := sessionTailRead(path)
	if !ok {
		return false
	}
	return hasSessionEndBytes(data)
}

// hasSessionEndBytes applies the orphan predicate to an already-read buffer:
// does it carry a session_end event within sessionEndScanBackMax bytes of its
// end? Split out from HasSessionEnd so the predicate has TWO callers — the
// production tail read, and the pre-change whole-file read kept in the test
// binary only (sessionend_tail_equivalence_test.go), where the two are compared
// input by input to prove the bounded read changed the COST and nothing else.
//
// The split IS the cost/classification boundary of this change: HasSessionEnd
// now reads a bounded tail and hands the pre-existing walk the pre-existing
// bytes it used to receive from os.ReadFile. No predicate line moved — the
// sibling task that owns classification semantics (what counts as a crash)
// still owns this function (Rule: this change is cost/IO only).
func hasSessionEndBytes(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	lineEnd := len(data)
	if data[lineEnd-1] == '\n' {
		lineEnd-- // a trailing newline is a terminator, not content
	}
	for lineEnd > 0 {
		lineStart := bytes.LastIndexByte(data[:lineEnd], '\n') + 1
		if lineStart < lineEnd {
			if sessionEventTypeOf(data[lineStart:lineEnd]) == sessionevent.EventTypeSessionEnd {
				return true
			}
		}
		if len(data)-lineStart > sessionEndScanBackMax {
			break // walked back far enough — stop scanning
		}
		lineEnd = lineStart - 1 // step past the newline that opened this line
	}
	return false
}

// orphanLiveWindowEffective resolves the quiet-period a JSONL must have been
// untouched for before its missing session_end counts as a death: the
// orphanLiveWindow default, overridable once here via
// orphanLiveWindowSecEnv (Rule 2 — exactly one reader).
func orphanLiveWindowEffective() time.Duration {
	return envDurationSeconds(orphanLiveWindowSecEnv, orphanLiveWindow)
}

// DetectOrphanSessions scans the sessions tree (session-dir-unify S1: flat
// legacy <uuid>.jsonl files AND per-session directories holding run files —
// <task_id>/<run-uuid>.jsonl for task runs, <uuid>/session.jsonl for bare
// sessions) and returns the paths of the sessions whose writer provably died
// without a clean exit (crash-dump epic ② kill -9 case), newest first.
//
// The crash test is ClassifyOrphanSession — NOT "the file has no session_end
// tail". The old tail-only test reported every live session (still appending)
// and every unstamped/legacy session as a crash, which is what made a plain
// `gopi` start print a phantom orphan count. Only OrphanCrashed is returned:
// live and unclassifiable sessions are silent, never counted (Rule 15).
//
// The live-window mtime probe below is the coarse first filter (the path is
// still being written ⇒ not a crash, whatever the classification would say);
// ClassifyOrphanSession is the decisive one. Both are kept: the probe keeps a
// merely-slow session out of the classification entirely, and the
// classification covers what the probe cannot — a quiet JSONL whose writer
// is demonstrably GONE with no session_end.
//
// excludeSessionIDs filters out sessions that must NOT be treated as orphans
// — above all the CURRENT process's own session; the liveness clause already
// covers it (our own PID is alive), so the parameter is now belt-and-braces
// and stays for callers that pass an id whose process is a parent/ancestor.
// Directory members (attachments/, artifacts/, dump files) are ignored.
//
// The candidates come from internal/sessionlayout.Records — the shared
// layout-keyed scan — NOT from a bare "*.jsonl" glob: a session dir's SIDE
// FILES (signal-provenance.jsonl, written by the signal recorder for the very
// session beside it) are not sessions, and offering one to
// ClassifyOrphanSession would let a side file be reported (or dumped) as a
// crashed session.
func DetectOrphanSessions(excludeSessionIDs ...string) []string {
	excl := make(map[string]bool, len(excludeSessionIDs))
	for _, id := range excludeSessionIDs {
		excl[strings.TrimSpace(id)] = true
	}
	quietSince := nowUTC().Add(-orphanLiveWindowEffective())
	dir, err := config.SessionsRoot()
	if err != nil {
		return nil
	}
	records, err := sessionlayout.Records(dir)
	if err != nil {
		return nil
	}
	var orphans []string
	for _, r := range records {
		orphanCheckJSONL(r.Path, excl, quietSince, &orphans)
	}
	// Newest first — ReadDir returns lexical order which for UUID names is
	// creation order (v4 has a time component in some UUIDs but ours are
	// random; use mod time for recency).
	sort.Slice(orphans, func(i, j int) bool {
		fi, ei := statModTime(orphans[i]), statModTime(orphans[j])
		return fi.After(ei)
	})
	return orphans
}

// orphanCheckJSONL appends p to orphans when its basename-minus-.jsonl is not
// excluded, the file has been QUIET since at least quietSince, and the session
// then classifies as a crash (writer provably gone AND no session_end). Any
// other verdict — live, unclassifiable — is silent. An unreadable mtime is
// treated as LIVE (the zero time is not "ancient"): a stat failure must never
// manufacture a crash report, and the next startup re-checks.
func orphanCheckJSONL(p string, excl map[string]bool, quietSince time.Time, orphans *[]string) {
	sessionID := strings.TrimSuffix(filepath.Base(p), sessionJSONLSuffix)
	if excl[sessionID] {
		return
	}
	mtime := statModTime(p)
	if mtime.IsZero() || mtime.After(quietSince) {
		return
	}
	if ClassifyOrphanSession(p).Status == OrphanCrashed {
		*orphans = append(*orphans, p)
	}
}

// statModTime returns the mtime of path, zero time when unreadable.
func statModTime(path string) time.Time {
	fi, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}

// Close flushes any buffered events, fsyncs, and closes the session log file.
// Safe to call multiple times — the task-end fallback that guarantees zero
// buffered loss on normal exit.
func (sl *SessionLogger) Close() error {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	syncErr := sl.syncLocked()
	closeErr := sl.file.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

// LogHippocampusBrief writes the Lv-0 archive event of the hippocampus host
// (epic: hippocampus child 3/4, N7). It is the ONLY session event without a
// "message" key, and that is deliberate — the two existing JSONL consumers
// ignore it structurally:
//
//	internal/agent/trajectory.go  — switches on ev.Type; no branch matches
//	                                the archive type, and there is no default.
//	internal/tools/memory_brew.go — brewRenderEvent returns "" when Message is
//	                                nil, i.e. before its role switch.
//
// The record carries the raw organ signals (confidence, memory weight) plus
// the scheduler's decision (level) — "online had it but the log did not" must
// never be indistinguishable from "it never happened".
func (sl *SessionLogger) LogHippocampusBrief(r hippocampusBriefRecord) error {
	return sl.log(map[string]interface{}{
		"type":                        hippocampusBriefEvent,
		hippocampusLogKeyLevel:        r.Level,
		hippocampusLogKeySummary:      r.Summary,
		hippocampusLogKeyConfidence:   r.Confidence,
		hippocampusLogKeyMemoryWeight: r.MemoryWeight,
		hippocampusLogKeyMemories:     r.Memories,
		hippocampusLogKeyDurationMS:   r.DurationMS,
		hippocampusLogKeyRound:        r.Round,
	})
}

// newUUIDv4 generates a UUID v4 string using crypto/rand.
func newUUIDv4() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// NewRunUUID mints a run id for a caller that must NAME a record file before an
// agent exists (a resume whose run directory holds a checkpoint but no record:
// the segment is the same SESSION, but the record file has to be named, and the
// only writer of that name is this package). It is the exported face of
// newUUIDv4 — one minter, so a caller outside this package cannot invent a
// second spelling of "a run id".
func NewRunUUID() string { return newUUIDv4() }

// mintRunID answers the run id a FRESH run writes under — the ONE place the
// task-run identity is decided (epic: one task row, one id).
//
//	`task run` child (taskID non-empty, admissible) → the TASK ID
//	bare prompt / chat (taskID empty)               → a fresh uuid
//
// WHY THE TASK ID AND NOT A UUID (measured before this change on this host,
// row 01a17de9's own run: sessions/<task-id>/df8d3009-….jsonl). The run's
// directory was already named by the row (sessionDirForID), so a minted uuid
// made ONE run answer to TWO identities: the directory said the task id, the
// record's own session_start — and therefore the ledger's session_id column,
// `gopi session log <task-id>`, and every resume operand — said the uuid. The
// four values this epic unifies are (directory, record file name,
// session_start.sessionId, ledger session_id); taking the id from the row makes
// them one string by construction instead of by four readers agreeing.
//
// A SECOND RUN OF THE SAME ROW CONTINUES THAT SESSION, which is the second half
// of the same rule: same task id ⇒ same run directory, same record file, same
// id. That is why the task-run writers open append-or-create (rawlog.go's
// newRawLoggerInTaskRunDir, session.go's NewSessionLoggerForResumedRun) rather
// than O_EXCL — exclusive creation is the rule for a run that MINTS an identity,
// and a row does not mint one.
//
// An INADMISSIBLE task id (another spelling of an id this tree does not accept
// as a path element) is NOT adapted here: sessionDirForID refuses it by name,
// and inventing a uuid for it would hide that refusal behind a run that wrote
// somewhere else.
func mintRunID(taskID string) string {
	if taskID != "" && sessionid.IsAdmissible(taskID) {
		return taskID
	}
	return newUUIDv4()
}
