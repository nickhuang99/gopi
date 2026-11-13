package gopilog

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/nick/gopi/internal/sessionevent"
)

// The wire field names of the resume_options event live in the LEAF package
// (sessionevent.ResumeOptionsField*) — declared once for the writer
// (internal/agent) and this reader, which cannot share a definition because
// this package is imported BY the writer's package (gopilog/formatter.go ->
// internal/agent). They are NOT redeclared here: a second spelling is the
// drift the leaf package exists to prevent.

// ResumeOptions is the recorded command line of a resumable run: the options a
// session was STARTED with, read back from its own session JSONL.
//
// It exists because a checkpoint restores a conversation, never a command line
// (see sessionevent.EventTypeResumeOptions): a tool-enabled chat resumed
// without its `--tools` flag keeps the history but loses the capability, and
// answers by describing tool calls it cannot make. The record is what makes
// `gopi chat --resume <session-id>` mean something.
type ResumeOptions struct {
	// Flags are the recorded argv words, verbatim and in order.
	Flags []string
	// SessionID is the session the record belongs to.
	SessionID string
	// Workdir is the directory the recorded run was started in ("" when the
	// writer could not read it — it is a report, not a gate).
	Workdir string
	// Path is the session JSONL the record was read from.
	Path string
}

// ResumeOptionsForSession resolves a session id the same way every other
// id-taking verb does (the ONE resolver in this package) and returns the
// options that session recorded.
//
// The return distinguishes the three states a caller must not conflate
// (Rule 15 — each is a different sentence to the operator):
//
//	found=false, err=nil   the session has NO resume_options record: it
//	                       predates the event, or the run had nothing to
//	                       record. The caller says so; it never invents a
//	                       default.
//	found=true,  err=nil   the record is in hand.
//	err != nil             the session id could not be resolved, or its file
//	                       could not be read.
func ResumeOptionsForSession(sessionID string) (opts ResumeOptions, found bool, err error) {
	if sessionID == "" {
		return ResumeOptions{}, false, fmt.Errorf("no session id given")
	}
	dir, err := ResolveSessionDir()
	if err != nil {
		return ResumeOptions{}, false, fmt.Errorf("cannot resolve session dir: %w", err)
	}
	sessionPath, err := FindSession(dir, sessionID)
	if err != nil {
		return ResumeOptions{}, false, fmt.Errorf("cannot resolve session id %q: %w", sessionID, err)
	}
	if sessionPath == "" {
		return ResumeOptions{}, false, fmt.Errorf("no session %q under %s", sessionID, dir)
	}
	return ResumeOptionsFromFile(sessionPath)
}

// ResumeOptionsFromFile reads the resume_options record out of one session
// JSONL. It is the reader half of the format and the single place the field
// names above are decoded.
//
// TOLERANCE IS THE CONTRACT, and it is the same contract the event's own
// documentation states: a line that does not parse, or parses to another
// event type, is SKIPPED — never fatal. Session files are written by many
// binaries and by every past version, and the session file an operator opens
// after a crash is exactly the one most likely to be torn mid-line. A reader
// that failed the file on such a line would make the recovery path unusable
// at the only moment it is needed.
//
// The LAST record wins: a session can legitimately carry more than one (a
// resumed run records its own options in the file it CONTINUES, so the newest
// statement is the newest fact).
func ResumeOptionsFromFile(sessionPath string) (opts ResumeOptions, found bool, err error) {
	f, err := os.Open(sessionPath)
	if err != nil {
		return ResumeOptions{}, false, err
	}
	defer f.Close()

	scanner := newJSONLScanner(sessionPath, f, jsonlLineSafetyCeiling)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev RawEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue // torn/incompatible line: skip, never fail the file
		}
		if ev.Type != sessionevent.EventTypeResumeOptions {
			continue
		}
		// Decoded through the leaf package's key constants, never through struct
		// tags: a tag is a second spelling of the wire key, and the writer (in
		// another package) would have no way to share it.
		var rec map[string]json.RawMessage
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		var flags []string
		if raw, ok := rec[sessionevent.ResumeOptionsFieldFlags]; ok {
			if err := json.Unmarshal(raw, &flags); err != nil {
				continue
			}
		}
		opts = ResumeOptions{
			Flags:     append([]string(nil), flags...),
			SessionID: resumeOptionsField(rec, sessionevent.ResumeOptionsFieldSessionID),
			Workdir:   resumeOptionsField(rec, sessionevent.ResumeOptionsFieldWorkdir),
			Path:      sessionPath,
		}
		found = true
	}
	if err := scanner.Err(); err != nil {
		return ResumeOptions{}, false, fmt.Errorf("reading %s: %w", sessionPath, err)
	}
	return opts, found, nil
}

// resumeOptionsField reads one string field of a decoded record: absent or
// not-a-string yields "" — a report field, never a gate.
func resumeOptionsField(rec map[string]json.RawMessage, key string) string {
	raw, ok := rec[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}
