package rundir

// rundir_segments_witness_test.go — THE RESUME WITNESS, three states.
//
// The defect: this package's whole-file scan counted headers and kept the LAST
// pid, but startEvent carried no field for `resumedFrom`, so a log that SAYS
// "this run continued that session" rendered as N bare segments. Measured on
// this profile before the field existed: 15 headers carry the key and
// `gopi session show` displayed none of them.
//
// THE THREE STATES ARE THE POINT (Rule 15/15a): "no witness on any header" is a
// fact about the WRITER, not a statement that the session was never resumed —
// every task-run retry on this tree appends a header it cannot witness. So the
// zero value must never read as a working answer.

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/nickhuang99/gopi/internal/sessionevent"
)

const (
	// witnessHeaderCount is the fixture's header count: FIRST fresh, then
	// witnessHeaderCount-1 witnessed resumes.
	witnessHeaderCount = 3
)

// witnessedLog writes witnessHeaderCount headers, from the SECOND on carrying
// the resume witness under the owner's own key.
func witnessedLog(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "run.jsonl")
	body := ""
	for i := 0; i < witnessHeaderCount; i++ {
		body += `{"type":"session_start","sessionId":"w","pid":` + itoaWitness(6000000+i) +
			`,"timestamp":"2026-10-29T09:0` + itoaWitness(i) + `:00Z"`
		if i > 0 {
			body += `,"resumedFrom":"w"`
		}
		body += "}\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// itoaWitness keeps the fixture free of a strconv import it would use once.
func itoaWitness(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

// TestScanSegmentsCountsWitnessedHeaders — the red→green core: a three-header
// log with two witnesses must report BOTH numbers and the carrying state.
func TestScanSegmentsCountsWitnessedHeaders(t *testing.T) {
	stamp, ok := ScanSegments(witnessedLog(t))
	if !ok {
		t.Fatal("ScanSegments reported ok=false on a readable fixture")
	}
	if stamp.Starts != witnessHeaderCount {
		t.Fatalf("Starts = %d, want %d", stamp.Starts, witnessHeaderCount)
	}
	if stamp.WitnessedStarts != witnessHeaderCount-1 {
		t.Errorf("WitnessedStarts = %d, want %d", stamp.WitnessedStarts, witnessHeaderCount-1)
	}
	if stamp.SegmentWitness != SegmentWitnessCarrying {
		t.Errorf("SegmentWitness = %v, want %v", stamp.SegmentWitness, SegmentWitnessCarrying)
	}
	if got := stamp.SegmentWitness.String(); got != segmentWitnessWordCarrying {
		t.Errorf("String() = %q, want %q", got, segmentWitnessWordCarrying)
	}
}

// TestScanSegmentsReportsAnUnwitnessedResumeAsAbsent — three headers, NO
// witness: the scan must say `absent`, not "never resumed" and not the zero
// "not counted" (which means nobody looked).
func TestScanSegmentsReportsAnUnwitnessedResumeAsAbsent(t *testing.T) {
	path := writeSegmentedLog(t, pidsSegments{PIDs: []int{segScanPID1, segScanPID2, segScanPID3}}, 0)
	stamp, ok := ScanSegments(path)
	if !ok {
		t.Fatal("ScanSegments reported ok=false on a readable fixture")
	}
	if stamp.Starts != 3 {
		t.Fatalf("Starts = %d, want 3", stamp.Starts)
	}
	if stamp.WitnessedStarts != 0 {
		t.Errorf("WitnessedStarts = %d, want 0 — the fixture carries no witness", stamp.WitnessedStarts)
	}
	if stamp.SegmentWitness != SegmentWitnessAbsent {
		t.Errorf("SegmentWitness = %v, want %v — headers were read and none carried a witness",
			stamp.SegmentWitness, SegmentWitnessAbsent)
	}
}

// TestSingleSegmentLogIsNotAnAbsentWitnessClaim — one header, no witness, is
// the ORDINARY case: it states no boundary at all, so it must not be promoted
// to the absent state (which would fire on every session and stop meaning
// anything).
func TestSingleSegmentLogIsNotAnAbsentWitnessClaim(t *testing.T) {
	stamp, ok := ScanSegments(writeSegmentedLog(t, pidsSegments{PIDs: []int{segScanPID1}}, 0))
	if !ok {
		t.Fatal("ScanSegments reported ok=false")
	}
	if stamp.Resumed() {
		t.Error("a single-segment log reports as resumed")
	}
	if stamp.SegmentWitness == SegmentWitnessCarrying {
		t.Error("a single-segment unwitnessed log reports as carrying")
	}
}

// TestStartEventCarriesTheWitnessKeys — in-package reflection (the struct is
// unexported here) pins each tag to the OWNER's constant: a struct tag cannot
// reference a constant, so the two would otherwise drift and the reader would
// silently stop seeing a key the writer still emits (Rule 13).
func TestStartEventCarriesTheWitnessKeys(t *testing.T) {
	tags := map[string]bool{}
	typ := reflect.TypeOf(startEvent{})
	for i := 0; i < typ.NumField(); i++ {
		tags[typ.Field(i).Tag.Get("json")] = true
	}
	for _, k := range []string{
		sessionevent.SessionStartFieldResumedFrom,
		sessionevent.SessionStartFieldResumedFromRawLog,
	} {
		if !tags[k] {
			t.Errorf("startEvent has no field tagged %q — the witness is unreadable again", k)
		}
	}
}
