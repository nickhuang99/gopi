package replaydrive

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// writeHeader writes a session JSONL whose first row is a session_start
// carrying the given geometry field (raw JSON, so a malformed shape can be
// exercised too), followed by one recorded turn — enough for the file to be a
// session this package would accept.
func writeHeader(t *testing.T, geometryJSON string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	row := `{"type":"` + sessionevent.EventTypeSessionStart + `","sessionId":"s-1","model":"m"`
	if geometryJSON != "" {
		row += `,"` + sessionevent.FieldTerminalGeometry + `":` + geometryJSON
	}
	body := row + "}\n" +
		`{"type":"user","message":{"role":"user","content":"hello"}}` + "\n" +
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hi"}]}}` + "\n" +
		`{"type":"request"}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write the session: %v", err)
	}
	return path
}

// TestLoadRecordedGeometryReadsTheHeader is the read side of the recording's
// geometry: the numbers in session_start are what a replay draws at.
func TestLoadRecordedGeometryReadsTheHeader(t *testing.T) {
	path := writeHeader(t, `{"state":"measured","cols":100,"rows":30}`)
	geo, ok, err := LoadRecordedGeometry(path)
	if err != nil || !ok {
		t.Fatalf("LoadRecordedGeometry = (ok=%v, err=%v), want a geometry", ok, err)
	}
	if geo.Cols != 100 || geo.Rows != 30 || !geo.Known() {
		t.Fatalf("geometry = %+v, want 100x30 measured", geo)
	}
}

// TestLoadRecordedGeometryReportsSilenceAsAbsence is the OLD-SESSION case: a
// recording made before the field existed carries no geometry, and that must be
// reported as an ABSENCE (ok=false, no error) so the caller can say out loud
// which fallback it used — never as a zero size.
func TestLoadRecordedGeometryReportsSilenceAsAbsence(t *testing.T) {
	path := writeHeader(t, "")
	geo, ok, err := LoadRecordedGeometry(path)
	if err != nil {
		t.Fatalf("an old recording reported an error: %v", err)
	}
	if ok {
		t.Fatalf("an old recording claimed a geometry: %+v", geo)
	}
	if geo.Known() {
		t.Fatalf("the absence is Known(): %+v", geo)
	}
}

// TestLoadRecordedGeometryDeliversAnExplicitUnknown: a session that recorded
// "unknown" DID answer the question — the answer is that there was no terminal
// — so ok=true and Known()=false. Collapsing that into ok=false would lose the
// reason the run has to explain the fallback with.
func TestLoadRecordedGeometryDeliversAnExplicitUnknown(t *testing.T) {
	path := writeHeader(t, `{"state":"unknown","reason":"no standard descriptor is a terminal"}`)
	geo, ok, err := LoadRecordedGeometry(path)
	if err != nil || !ok {
		t.Fatalf("= (ok=%v, err=%v), want the recorded unknown delivered", ok, err)
	}
	if geo.Known() {
		t.Fatalf("the recorded unknown is Known(): %+v", geo)
	}
	if !strings.Contains(geo.Reason, "descriptor") {
		t.Fatalf("the reason was lost: %+v", geo)
	}
}

// TestLoadRecordedGeometryRefusesAUndecodableField: a present-but-unreadable
// geometry is NOT silence — it is a fact about the recording the caller must
// hear (rule 15), so it is an error.
func TestLoadRecordedGeometryRefusesAUndecodableField(t *testing.T) {
	path := writeHeader(t, `{"state":"estimated","cols":80,"rows":24}`)
	if _, _, err := LoadRecordedGeometry(path); err == nil {
		t.Fatal("an undecodable geometry field was read as silence")
	}
}

// TestLoadRecordedGeometryMissingFileIsAnError keeps the read honest for the
// caller that resolves a path itself.
func TestLoadRecordedGeometryMissingFileIsAnError(t *testing.T) {
	if _, _, err := LoadRecordedGeometry(filepath.Join(t.TempDir(), "nope.jsonl")); err == nil {
		t.Fatal("a missing recording was read as 'no geometry'")
	}
}
