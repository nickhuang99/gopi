package tools

// THE ROW'S OWN SESSION LOG — one resolver, three answers, one refusal (Rule 14).
//
// THE DEFECT THIS CLOSES (measured on this host's live ledger, 2026-10-29).
// `gopi session log <task-id>` and `gopi session resume <task-id>` resolved the
// same operand through two different paths, and they disagreed:
//
//	$ gopi session log 01a14b50-8df4-7d92-a4f9-fc1b40a7f9a9
//	exit=1  Session not found: 01a14b50-…
//	$ gopi session resume 01a14b50-8df4-7d92-a4f9-fc1b40a7f9a9
//	exit=0  [gopi] resume: continuing session cee08beb-… in ~/.gopi/sessions/cee08beb-…
//
// The ROW was one row. Its session directory (sessions/<task-id>/) did not
// exist — the run had been re-homed — while its `session_id` column named a
// record that did exist. `resume` reached that record through the column
// (TaskResumeTarget's legacy fallback), `session log` did not, because the
// operand path only knew the three FILE layouts a session id names and a task
// id names none of them.
//
// So the resolution is by ROW, and it is the same answer for every reader:
//
//  1. THE ROW'S OWN RUN DIRECTORY — sessions/<task-id>/. It is keyed by the row
//     id, so whatever record it holds belongs to this row by construction. When
//     several records are there (a killed run plus its resume) the newest by
//     mtime wins, and an EXACT mtime tie is a named refusal: two records
//     written in the same second name no single run, and picking the readdir
//     first would make the answer depend on the filesystem's ordering.
//  2. THE LEDGER'S `session_id` COLUMN — only for a row whose own directory
//     holds no record at all. The column is a weaker pointer (internal/tools/
//     task_resume_session.go documents it: for a `source=agent` row it names
//     the SUBMITTER), so the record's own `session_start.taskId` is checked
//     when it carries one: a record that names a DIFFERENT row is refused by
//     name rather than served. A record that names no task at all (a chat run,
//     a pre-row layout) is served with a NOTE saying its ownership rests on the
//     ledger column — measured: 223 rows on this host are that shape, and every
//     one of their records carries no taskId, so refusing them would refuse the
//     only pointer those rows have.
//  3. NOTHING — no record anywhere. The caller refuses with a non-zero exit;
//     this file never substitutes another session (Rule 15: a resolution
//     failure is a HARD error, never a silently different conversation).

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
)

const (
	// TaskOwnLogSourceRowRunDir is the strong answer: the record lives in the
	// row's own directory, so it belongs to the row by the address it was
	// found at.
	TaskOwnLogSourceRowRunDir = "row-run-dir"

	// TaskOwnLogSourceRowColumn is the weaker answer: the ledger's session_id
	// column named the record. See TaskOwnSessionLog's step 2.
	TaskOwnLogSourceRowColumn = "row-session-column"
)

// taskOwnLogScanBuf bounds the first-line read: a session_start event is a few
// hundred bytes (it names model, id, raw-log path and workdir), and a record
// whose FIRST line is larger than this is not one (the writer emits it before
// any turn).
const taskOwnLogScanBuf = 1 << 20

// taskOwnLogStartEvent is the subset of a record's session_start event this
// file reads — the record's OWN statement of which row and session it is.
type taskOwnLogStartEvent struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionId"`
	TaskID    string `json:"taskId"`
}

// TaskOwnLog is one row's own session log, with WHERE the answer came from and
// what a reader must know about it (Note is "" for the strong answer).
type TaskOwnLog struct {
	Path   string
	Source string
	Note   string
}

// TaskOwnSessionLog resolves the session LOG a ledger row owns. The zero value
// with a nil error means "this operand is not a ledger row we can prove" — the
// caller then keeps its own path/session-id resolution untouched, so a chat
// target and a raw-log path are never affected by this file.
//
// An unreadable ledger is deliberately that same answer: this resolver exists to
// ADD a row-keyed answer, never to take away the resolution a caller already
// had, and Rule 15's "no silent failure" is satisfied by the caller refusing
// (non-zero exit) when every source answers nothing.
func TaskOwnSessionLog(taskID string) (TaskOwnLog, error) {
	tid := taskIDNormalize(taskID)
	if tid == "" {
		return TaskOwnLog{}, nil
	}
	gd, err := config.GopiDir()
	if err != nil {
		return TaskOwnLog{}, fmt.Errorf("cannot resolve the gopi data root: %w", err)
	}
	// STEP 1 ASKS NOBODY. The row's own directory is keyed by the row id, so a
	// record found inside it belongs to this row by the ADDRESS it was found at
	// — the ledger does not have to answer, and must not: recovery is exactly
	// the situation where the ledger is unavailable, and a resolution that
	// refused then would be refusing the one source that cannot be wrong.
	runDir := filepath.Join(gd, config.SessionsDirName, tid)
	if fi, statErr := os.Stat(runDir); statErr == nil && fi.IsDir() {
		p, err := rowDirRecord(runDir, tid)
		if err != nil {
			return TaskOwnLog{}, err
		}
		if p != "" {
			return TaskOwnLog{Path: p, Source: TaskOwnLogSourceRowRunDir}, nil
		}
	}
	col, colErr := TaskSessionID(tid)
	if colErr != nil {
		return TaskOwnLog{}, nil
	}
	col = strings.TrimSpace(col)
	if col == "" {
		return TaskOwnLog{}, nil
	}
	p := taskSessionLogPath(col)
	if fi, statErr := os.Stat(p); statErr != nil || !fi.Mode().IsRegular() {
		return TaskOwnLog{}, nil
	}
	owner := sessionStartTaskID(p)
	switch {
	case owner != "" && owner != tid:
		return TaskOwnLog{}, fmt.Errorf("row %s: the ledger's session_id %s names the record at %s, which records itself as TASK %s — refusing to serve another row's session as this row's log",
			tid, col, p, owner)
	case owner == "":
		return TaskOwnLog{
			Path:   p,
			Source: TaskOwnLogSourceRowColumn,
			Note: fmt.Sprintf("row %s: this is the record the ledger's session_id column names (%s); the record carries no taskId of its own (a chat or pre-row run), so its ownership rests on the ledger column",
				tid, col),
		}, nil
	}
	return TaskOwnLog{Path: p, Source: TaskOwnLogSourceRowColumn}, nil
}

// rowDirRecord answers "which record does this row's run directory stand for":
// the bare session record when the directory has one (it IS the directory's
// session), else the newest run file by mtime. An exact mtime tie between two
// run files is a REFUSAL naming both — a second resolution in the same second
// names no single run.
func rowDirRecord(dir, tid string) (string, error) {
	bare := filepath.Join(dir, sessionlayout.RecordName)
	if fi, err := os.Stat(bare); err == nil && fi.Mode().IsRegular() {
		return bare, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", nil // an unreadable row directory is "no record", not an error
	}
	var newest string
	var newestMod int64
	var tied []string
	for _, e := range entries {
		if e.IsDir() || !sessionlayout.IsJSONL(e.Name()) || sessionlayout.IsSideFile(e.Name()) {
			continue
		}
		p := filepath.Join(dir, e.Name())
		fi, statErr := os.Stat(p)
		if statErr != nil || !fi.Mode().IsRegular() {
			continue
		}
		mod := fi.ModTime().UnixNano()
		switch {
		case mod > newestMod:
			newest, newestMod, tied = p, mod, nil
		case mod == newestMod:
			tied = append(tied, p)
		}
	}
	if newest == "" {
		return "", nil
	}
	if len(tied) > 0 {
		cands := append([]string{newest}, tied...)
		return "", fmt.Errorf("row %s: its run directory %s holds %d records written at the SAME instant (%v) — they name no single run, so the log to read is ambiguous. Pass one directly: gopi session log <path>",
			tid, dir, len(cands), cands)
	}
	return newest, nil
}

// sessionStartTaskID reads the `taskId` field of a record's session_start event
// — the record's OWN statement of which row it ran. "" means it declares none
// (a chat run, or a run from before the field existed).
func sessionStartTaskID(path string) string {
	ev := sessionStartFields(path)
	if ev == nil {
		return ""
	}
	return strings.TrimSpace(ev.TaskID)
}

// sessionStartFields returns the session_start event of a record.
func sessionStartFields(path string) *taskOwnLogStartEvent {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, taskOwnLogScanBuf), taskOwnLogScanBuf)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev taskOwnLogStartEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			return nil
		}
		return &ev
	}
	return nil
}
