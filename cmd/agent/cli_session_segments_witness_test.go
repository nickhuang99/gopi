package main

// cli_session_segments_witness_test.go — THE RESUME WITNESS ON BOTH FACES.
//
// The defect this pins: `internal/rundir`'s whole-file scan counted headers and
// kept the last pid, but carried no field for the `resumedFrom` key — so a log
// that DOES say "this run continued that session" rendered as N bare segments
// and the witness was invisible. Measured before this test existed: 15 headers
// in this profile carry `resumedFrom` and `gopi session show` reported zero of
// them.
//
// The fixture is the SMALLEST sample that states the question (Rule 10.4): three
// `session_start` headers, the second and third carrying the witness, ~a few
// hundred bytes, so a failure is visible by reading the fixture. The scan is
// driven for real (`rundir.ScanSegments` on the file) rather than by
// hand-filling the stamp — the chain that broke was scan → render, and a test
// that starts at the render (or at the stamp) would have stayed green through
// the whole defect.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/rundir"
	"github.com/nickhuang99/gopi/internal/sessionevent"
	"github.com/nickhuang99/gopi/internal/tools"
)

const (
	// segWitnessResumedHeaders is how many of the fixture's three headers carry
	// the witness — every one after the first, which is what a session resumed
	// twice looks like.
	segWitnessResumedHeaders = 2
	// segWitnessHeaders is the fixture's header count.
	segWitnessHeaders = 3
)

// writeWitnessedLog writes the fixture: three run segments, the 2nd and 3rd
// appending the resume witness keys through sessionevent's own constants.
func writeWitnessedLog(t *testing.T, dir string) string {
	t.Helper()
	base := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	runID := "seg-witness-run"
	var b strings.Builder
	for i := 0; i < segWitnessHeaders; i++ {
		at := base.Add(time.Duration(i) * time.Hour)
		b.WriteString(`{"type":"` + sessionevent.EventTypeSessionStart + `","sessionId":"` + runID +
			`","pid":` + strconv.Itoa(5200000+i) +
			`,"timestamp":"` + at.Format(time.RFC3339Nano) + `"`)
		if i > 0 {
			// THE WITNESS — the key this test is about, spelled from its owner.
			b.WriteString(`,"` + sessionevent.SessionStartFieldResumedFrom + `":"` + runID + `"`)
			b.WriteString(`,"` + sessionevent.SessionStartFieldResumedFromRawLog + `":"/tmp/raw.log"`)
		}
		b.WriteString("}\n")
		b.WriteString(`{"type":"` + sessionevent.EventTypeSessionEnd +
			`","exit_code":0,"reason":"interrupt","elapsed_sec":60,"timestamp":"` +
			at.Add(time.Minute).Format(time.RFC3339Nano) + `"}` + "\n")
	}
	path := filepath.Join(dir, runID+".jsonl")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
	return path
}

// TestSessionShowReportsTheResumeWitness is the red→green regression: the SCAN
// must count the witnessed headers, and BOTH faces must render that count.
func TestSessionShowReportsTheResumeWitness(t *testing.T) {
	path := writeWitnessedLog(t, t.TempDir())
	stamp, ok := rundir.ScanSegments(path)
	if !ok {
		t.Fatalf("ScanSegments(%s) reported ok=false on a readable fixture", path)
	}
	if stamp.Starts != segWitnessHeaders {
		t.Fatalf("Starts = %d, want %d", stamp.Starts, segWitnessHeaders)
	}
	if stamp.WitnessedStarts != segWitnessResumedHeaders {
		t.Errorf("WitnessedStarts = %d, want %d — the scan must count the headers that NAME a resume",
			stamp.WitnessedStarts, segWitnessResumedHeaders)
	}
	if stamp.SegmentWitness != rundir.SegmentWitnessCarrying {
		t.Errorf("SegmentWitness = %v, want %v (headers carried the key)",
			stamp.SegmentWitness, rundir.SegmentWitnessCarrying)
	}

	show := tools.SessionShow{
		SessionID:         "seg-witness-run",
		Segments:          stamp.Starts,
		SegmentsScanned:   true,
		SegmentWitness:    stamp.SegmentWitness,
		WitnessedSegments: stamp.WitnessedStarts,
	}

	// FACE 1 — the human page: the count AND the witness clause.
	text := renderSessionSegments(show)
	if !strings.Contains(text, strconv.Itoa(segWitnessHeaders)) {
		t.Errorf("page = %q, want the segment count %d", text, segWitnessHeaders)
	}
	wantClause := strings.Replace(sessionSegmentsWitnessedFmt, "%d",
		strconv.Itoa(segWitnessResumedHeaders), 1)
	if !strings.Contains(text, wantClause) {
		t.Errorf("page = %q, want the witness clause %q", text, wantClause)
	}

	// FACE 2 — the machine face: the same two facts under their own keys.
	payload, err := jsonSessionShow(show)
	if err != nil {
		t.Fatalf("jsonSessionShow: %v", err)
	}
	for _, want := range []string{
		`"segments": ` + strconv.Itoa(segWitnessHeaders),
		`"segments_resumed": ` + strconv.Itoa(segWitnessResumedHeaders),
		`"segment_witness": "` + rundir.SegmentWitnessCarrying.String() + `"`,
	} {
		if !strings.Contains(payload, want) {
			t.Errorf("json payload is missing %s\n%s", want, payload)
		}
	}
}

// TestSessionShowReportsAnAbsentWitnessAsAbsent — the OTHER half of the rule
// (15a): a log whose headers carry no witness is NOT a log that says "never
// resumed". The page must name the absence, and the JSON must distinguish it
// from the zero value it would otherwise share with "nothing was scanned".
func TestSessionShowReportsAnAbsentWitnessAsAbsent(t *testing.T) {
	// Built directly: the shape under test is the RENDERING of the state, and
	// the scan's own absent-state is pinned in internal/rundir.
	show := tools.SessionShow{
		SessionID:       "unwitnessed-run",
		Segments:        segWitnessHeaders,
		SegmentsScanned: true,
		SegmentWitness:  rundir.SegmentWitnessAbsent,
	}
	text := renderSessionSegments(show)
	if !strings.Contains(text, sessionSegmentsWitnessAbsent) {
		t.Errorf("page = %q, want the absent-witness clause %q", text, sessionSegmentsWitnessAbsent)
	}
	payload, err := jsonSessionShow(show)
	if err != nil {
		t.Fatalf("jsonSessionShow: %v", err)
	}
	if !strings.Contains(payload, `"segment_witness": "`+rundir.SegmentWitnessAbsent.String()+`"`) {
		t.Errorf("json payload does not name the absent witness\n%s", payload)
	}
	if strings.Contains(payload, `"segment_witness": "`+rundir.SegmentWitnessNotCounted.String()+`"`) {
		t.Errorf("an unwitnessed but SCANNED log must not publish the not-counted state\n%s", payload)
	}
}

// TestScanSegmentsReadsTheWritersOwnKey — the tag check, done BEHAVIOURALLY:
// the fixture spells the key from sessionevent's constant, so a tag that
// drifted from that constant would leave WitnessedStarts at 0 and this test
// red. (A struct tag cannot reference a constant, so drift is a real risk —
// Rule 13 — and the only honest check is that the reader still sees what the
// writer emits.)
func TestScanSegmentsReadsTheWritersOwnKey(t *testing.T) {
	path := writeWitnessedLog(t, t.TempDir())
	stamp, ok := rundir.ScanSegments(path)
	if !ok {
		t.Fatal("ScanSegments reported ok=false")
	}
	if stamp.WitnessedStarts == 0 {
		t.Errorf("the scan saw no witness although every non-first header carries %q — "+
			"startEvent's tag has drifted from the writer's key",
			sessionevent.SessionStartFieldResumedFrom)
	}
}

// TestRunPageRendersTheSegmentDisclosure — the RUN page renders the page for
// every non-chat session, and it printed NO segment line at all before this
// arm: a resumed task run (the shape this tree produces most) showed a bare pid
// and ended, with the disclosure the chat page carries simply absent. The scan
// ran; nothing rendered its answer.
func TestRunPageRendersTheSegmentDisclosure(t *testing.T) {
	show := tools.SessionShow{
		SessionID:         "run-witnessed",
		Kind:              tools.SessionKindRun,
		Mode:              tools.SessionModeRun,
		Segments:          segWitnessHeaders,
		SegmentsScanned:   true,
		SegmentWitness:    rundir.SegmentWitnessCarrying,
		WitnessedSegments: segWitnessResumedHeaders,
	}
	text := renderSessionRun(show)
	wantClause := strings.Replace(sessionSegmentsWitnessedFmt,
		"%d", strconv.Itoa(segWitnessResumedHeaders), 1)
	if !strings.Contains(text, sessionRuntimeSegmentsKey) {
		t.Errorf("run page carries no %q line at all:\n%s", sessionRuntimeSegmentsKey, text)
	}
	if !strings.Contains(text, wantClause) {
		t.Errorf("run page is missing the witness clause %q:\n%s", wantClause, text)
	}
}
