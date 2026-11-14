package agent

// chat_exit_marker_read.go — the READ side of the chat exit marker: what a
// sweep that finds a session with no `session_end` can say about HOW the
// process got there.
//
// WHY A SEPARATE FILE FROM THE WRITER (cmd/agent/chat_exit_marker.go). The
// writer lives on the chat surface (it needs the REPL's own instants); the
// reader lives where the orphan sweep already runs (BackfillOrphanCrashDumps,
// internal/agent), and package main cannot be imported. The two halves meet at
// the ONE value internal/sessionlayout owns — ExitMarkerName — so neither can
// drift from the other's spelling (Rule 14).
//
// WHAT IT IS FOR, exactly: the 2026-10-20 postmortem of session 8616b46c could
// prove that four exit guards wrote nothing, but not WHY. That answer belongs in
// the dump the sweep writes, i.e. on the artifact a reader already opens
// (`gopi session log --crashes`), rather than in a second place nobody looks.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nickhuang99/gopi/internal/sessionlayout"
)

// ChatExitMarker is the marker as a READER sees it — the fields a verdict is
// made from, decoded leniently: an unreadable or half-written marker is a
// FINDING, not an error that swallows the report (Rule 15).
type ChatExitMarker struct {
	Phase      string `json:"phase"`
	SessionID  string `json:"session_id"`
	PID        int    `json:"pid"`
	StartTicks uint64 `json:"start_ticks"`
	Started    string `json:"started"`
	Updated    string `json:"updated"`
	ExitCode   int    `json:"exit_code"`
	Reason     string `json:"reason"`
	Signal     string `json:"signal"`
	EOFKind    string `json:"eof_kind"`
}

// ChatExitMarkerPath is where the marker for a session record lives: in the
// record's OWN directory, under the ONE name sessionlayout owns.
func ChatExitMarkerPath(recordPath string) string {
	return filepath.Join(sessionlayout.RecordContainer(recordPath), sessionlayout.ExitMarkerName)
}

// ReadChatExitMarker returns the marker beside a session record. ok is false
// when there is no marker at all — a pre-marker binary, or a death before the
// REPL armed it — and the caller states that absence rather than guessing.
//
// A marker that EXISTS but cannot be parsed returns ok=true with Phase "" —
// deliberately distinct from "no marker": a file on disk that no reader can
// interpret is evidence of a write that was interrupted, which is a different
// death from "the process never got as far as arming one".
func ReadChatExitMarker(recordPath string) (ChatExitMarker, bool) {
	var m ChatExitMarker
	raw, err := os.ReadFile(ChatExitMarkerPath(recordPath))
	if err != nil {
		return m, false
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return ChatExitMarker{}, true // present, unreadable: phase stays ""
	}
	// THE MARKER'S OWN CLAIM IS CHECKED AGAINST THE RECORD IT SITS BESIDE.
	// A marker's PATH is not proof of whose death it certifies: in the legacy
	// FLAT layout two records share one directory, so `<root>/chat-exit.marker`
	// belongs to whichever run wrote it last. Trusting the path would file one
	// session's phase against another's death — the same self-agreeing mistake
	// that let 142 orphan dumps claim `session_id: "session"` (the wrong claim
	// agreed with the wrong expectation). The identity is resolved from the
	// RECORD by the ONE function that owns it, and a marker that names a
	// different session is reported as belonging to that other session, never
	// silently borrowed.
	if want := sessionlayout.SessionID(recordPath); want != "" && m.SessionID != "" && m.SessionID != want {
		return m, false
	}
	return m, true
}

// chatExitMarkerVerdict renders the marker as the ONE line a dump carries: the
// phase (the verdict), plus the closeout's own words when there were any. An
// absent marker, an unreadable one and every phase each get their own sentence,
// because a reader acting on this must be able to tell them apart.
func chatExitMarkerVerdict(recordPath string) string {
	m, ok := ReadChatExitMarker(recordPath)
	if !ok {
		return orphanDumpExitMarkerAbsent
	}
	if m.Phase == "" {
		return "present-but-unreadable"
	}
	var b strings.Builder
	b.WriteString(m.Phase)
	if m.SessionID != "" {
		b.WriteString(" session=")
		b.WriteString(m.SessionID)
	}
	if m.PID != 0 {
		b.WriteString(" pid=")
		b.WriteString(strconv.Itoa(m.PID))
	}
	if m.Updated != "" {
		b.WriteString(" updated=")
		b.WriteString(m.Updated)
	}
	if m.Reason != "" {
		b.WriteString(" reason=")
		b.WriteString(m.Reason)
	}
	if m.Signal != "" {
		b.WriteString(" signal=")
		b.WriteString(m.Signal)
	}
	if m.EOFKind != "" {
		b.WriteString(" eof_kind=")
		b.WriteString(m.EOFKind)
	}
	return b.String()
}
