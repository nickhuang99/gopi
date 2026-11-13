package sessionevent

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestTerminalGeometryUnknownIsNotAZeroSize is the rule-15a regression: the
// UNKNOWN shape must not survive a JSON round trip as a pair of zeros, because
// a reader would then have no way to tell "no terminal was measured" from "the
// terminal was 0 cells wide" (which does not exist, and which a film would
// build a zero-cell canvas out of).
func TestTerminalGeometryUnknownIsNotAZeroSize(t *testing.T) {
	unknown := UnknownTerminalGeometry(GeometryReasonNoTerminal)
	b, err := json.Marshal(unknown)
	if err != nil {
		t.Fatalf("marshal the unknown geometry: %v", err)
	}
	got := string(b)
	if strings.Contains(got, `"cols"`) || strings.Contains(got, `"rows"`) {
		t.Errorf("the unknown shape carries a NUMBER (%s) — a zero here reads as a size", got)
	}
	if !strings.Contains(got, `"state":"`+GeometryStateUnknown+`"`) {
		t.Errorf("the unknown shape does not carry its state: %s", got)
	}
	var back TerminalGeometry
	if uerr := json.Unmarshal(b, &back); uerr != nil {
		t.Fatalf("unmarshal the unknown geometry: %v", uerr)
	}
	if back.Known() {
		t.Errorf("the unknown geometry reports Known() after a round trip: %+v", back)
	}
	if back.State != GeometryStateUnknown || back.Reason != GeometryReasonNoTerminal {
		t.Errorf("round trip lost the state/reason: %+v", back)
	}
}

// TestTerminalGeometryMeasuredRoundTrips pins the measured half, so the two
// shapes are asserted against each other rather than one of them alone.
func TestTerminalGeometryMeasuredRoundTrips(t *testing.T) {
	measured := MeasuredTerminalGeometry(117, 26)
	if !measured.Known() {
		t.Fatalf("a real measurement reports Known()=false: %+v", measured)
	}
	b, err := json.Marshal(measured)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"state":"` + GeometryStateMeasured + `","cols":117,"rows":26}`
	if string(b) != want {
		t.Errorf("measured shape = %s, want %s", b, want)
	}
	var back TerminalGeometry
	if uerr := json.Unmarshal(b, &back); uerr != nil {
		t.Fatalf("unmarshal: %v", uerr)
	}
	if back.Cols != 117 || back.Rows != 26 || !back.Known() {
		t.Errorf("round trip lost the numbers: %+v", back)
	}
}

// TestMeasuredTerminalGeometryRefusesANonSize: a caller that measured 0 (or a
// negative) must not be able to store a fake measurement through the
// constructor — the geometry that reaches a session is either real or UNKNOWN.
func TestMeasuredTerminalGeometryRefusesANonSize(t *testing.T) {
	for _, tc := range []struct{ cols, rows int }{{0, 26}, {117, 0}, {-1, 26}, {117, -3}} {
		got := MeasuredTerminalGeometry(tc.cols, tc.rows)
		if got.Known() {
			t.Errorf("MeasuredTerminalGeometry(%d, %d) claims to be known: %+v", tc.cols, tc.rows, got)
		}
		if got.State != GeometryStateUnknown || got.Reason == "" {
			t.Errorf("MeasuredTerminalGeometry(%d, %d) = %+v, want an unknown carrying a reason", tc.cols, tc.rows, got)
		}
	}
}

// TestDecodeTerminalGeometrySeparatesSilenceFromNonsense is the second half of
// rule 15: "the field is absent" (ok=false, no error) and "the field carries
// something this package does not define" (an error) must not be collapsed —
// a reader that did would report a fallback the recording never asked for.
func TestDecodeTerminalGeometrySeparatesSilenceFromNonsense(t *testing.T) {
	if _, ok, err := DecodeTerminalGeometry(nil); ok || err != nil {
		t.Errorf("an absent field = (ok=%v, err=%v), want (false, nil)", ok, err)
	}
	if _, ok, err := DecodeTerminalGeometry(json.RawMessage(`{"state":"measured","cols":80,"rows":24}`)); !ok || err != nil {
		t.Errorf("a measured field = (ok=%v, err=%v), want (true, nil)", ok, err)
	}
	if g, ok, err := DecodeTerminalGeometry(json.RawMessage(`{"state":"unknown","reason":"x"}`)); !ok || err != nil || g.Known() {
		t.Errorf("an unknown field = (%+v, %v, %v), want the state delivered and NOT known", g, ok, err)
	}
	for _, raw := range []string{
		`{"cols":80,"rows":24}`,                   // no state at all
		`{"state":"guesswork","cols":1,"rows":1}`, // a state nobody defines
		`{"state":"measured"}`,                    // measured without numbers
		`{"state":"measured","cols":0,"rows":24}`, // measured, zero width
		`not json`,
	} {
		if _, ok, err := DecodeTerminalGeometry(json.RawMessage(raw)); err == nil {
			t.Errorf("DecodeTerminalGeometry(%s) = (ok=%v, err=nil) — useless data read as silence", raw, ok)
		}
	}
}

// TestTerminalGeometryStringNeverPrintsASizeForUnknown: the diagnostic sentence
// must not render an unmeasured geometry as "0x0", which a reader would read as
// a size.
func TestTerminalGeometryStringNeverPrintsASizeForUnknown(t *testing.T) {
	unknown := UnknownTerminalGeometry(GeometryReasonNoTerminal).String()
	if strings.Contains(unknown, "0x0") {
		t.Errorf("the unknown geometry prints as a size: %q", unknown)
	}
	if !strings.Contains(unknown, GeometryStateUnknown) {
		t.Errorf("the unknown geometry does not name its state: %q", unknown)
	}
	if got := MeasuredTerminalGeometry(80, 24).String(); got != "80x24" {
		t.Errorf("a measured geometry prints %q, want 80x24", got)
	}
}
