package agent

// chat_exit_marker_read_test.go — the READ side, pinned where the two halves
// meet: the writer's file name is internal/sessionlayout.ExitMarkerName, the
// reader resolves the marker from a RECORD path, and the orphan sweep's own dump
// must carry the verdict.
//
// The middle assertion is the one that matters for a future change: if the
// reader and the writer ever spell the marker differently, the sweep would file
// `absent` for a death that left a perfectly good marker — and `absent` is
// exactly the sentence the 2026-10-20 postmortem had to write.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nick/gopi/internal/sessionlayout"
)

// TestReadChatExitMarkerResolvesBesideTheRecord pins the path resolution: the
// marker lives in the RECORD's own directory under the ONE name the layout
// package owns.
func TestReadChatExitMarkerResolvesBesideTheRecord(t *testing.T) {
	dir := t.TempDir()
	record := filepath.Join(dir, sessionlayout.RecordName)
	if err := os.WriteFile(record, []byte(classifyStartLine("abc", 0)+"\n"), 0o644); err != nil {
		t.Fatalf("write record: %v", err)
	}
	if got, want := ChatExitMarkerPath(record), filepath.Join(dir, sessionlayout.ExitMarkerName); got != want {
		t.Fatalf("ChatExitMarkerPath = %q, want %q — a reader looking elsewhere reads nothing", got, want)
	}
	if _, ok := ReadChatExitMarker(record); ok {
		t.Fatalf("ReadChatExitMarker reported a marker where none was written")
	}
	blob := []byte(`{"phase":"live","session_id":"abc","pid":42,"start_ticks":7}`)
	if err := os.WriteFile(ChatExitMarkerPath(record), blob, 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	m, ok := ReadChatExitMarker(record)
	if !ok {
		t.Fatalf("ReadChatExitMarker found no marker at %s", ChatExitMarkerPath(record))
	}
	if m.Phase != "live" || m.SessionID != "abc" || m.PID != 42 || m.StartTicks != 7 {
		t.Fatalf("marker decoded as %+v, want the bytes written", m)
	}
}

// TestOrphanDumpCarriesTheExitMarkerVerdict pins the sweep's own artifact: when
// a session has no session_end and left a marker, the dump STATES how far the
// run got; when it left none, the dump says `absent` rather than omitting the
// field (an omitted field reads as "not applicable", and here the absence IS the
// finding).
//
// The dump is produced by the production function (BackfillOrphanCrashDumps),
// not hand-written, so this test measures the writer a reader will meet.
func TestOrphanDumpCarriesTheExitMarkerVerdict(t *testing.T) {
	dir := orphanTestSessionsDir(t)

	// A session with a marker at its armed phase: the shape a killed chat leaves.
	record := writeSessionJSONL(t, dir, "marker-session.jsonl", deadStampStartLine("marker-session"), testOrphanAge)
	if err := os.WriteFile(ChatExitMarkerPath(record), []byte(`{"phase":"live","session_id":"marker-session","pid":99,"updated":"2026-10-20T00:04:38Z"}`), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	// …and a session with NO marker at all (a pre-marker binary).
	record2 := writeSessionJSONL(t, dir, "bare-session.jsonl", deadStampStartLine("bare-session"), testOrphanAge)

	written, _ := BackfillOrphanCrashDumps()
	if written < 2 {
		t.Fatalf("BackfillOrphanCrashDumps wrote %d dumps, want both quiet sessions", written)
	}

	doc := dumpDocOf(t, orphanDumpPath(record))
	verdict, _ := doc[orphanDumpExitMarkerKey].(string)
	if !containsString(verdict, "live") {
		t.Fatalf("dump exit_marker = %q, want the marker's phase (live) — the sweep must say HOW FAR the run got, not only that it stopped", verdict)
	}
	if !containsString(verdict, "marker-session") {
		t.Errorf("dump exit_marker = %q, want it to name the session it certified", verdict)
	}

	doc2 := dumpDocOf(t, orphanDumpPath(record2))
	if got, _ := doc2[orphanDumpExitMarkerKey].(string); got != orphanDumpExitMarkerAbsent {
		t.Fatalf("dump for a marker-less session = %q, want %q — the absence IS the finding", got, orphanDumpExitMarkerAbsent)
	}
}

// containsString is a local substring probe: this package's tests do not import
// strings for one call (and the dump is small enough that readability wins).
func containsString(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
