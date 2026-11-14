package uiclient

// capability_record_test.go — the colour-capability decision on the dump
// artifact, asserted at BOTH ends a reader has: the text record line (the fields
// a human or an agent greps) and the JSON twin (where the probe's own evidence
// rides, because the record line is a space-separated field list and the probe
// summary contains spaces).
//
// WHY THESE ASSERTIONS AND NOT OTHERS. The record is the artifact an agent reads
// back to understand a screen it did not watch, so the failure that matters is
// not "the field is absent" (the line simply lacks it) but "the field is PRESENT
// AND WRONG" — a record claiming a measurement nothing made, or a grid whose
// colours are attributed to the wrong evidence. So each test pins which evidence
// is recorded and that a sink nobody reported to records NOTHING.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/lineedit"
)

// capabilitySink is a dumper the tests can render a record line from: the path
// is a scratch file so no test writes into the operator's profile, and the
// identity is the minimum a record needs.
func capabilitySink(t *testing.T) *ScreenDumper {
	t.Helper()
	d, err := NewScreenDumper(DumpConfig{
		Path: filepath.Join(t.TempDir(), "capability.dump"),
		Cols: 80,
		Rows: 24,
		Identity: DumpIdentity{
			PID:              os.Getpid(),
			Target:           "self:chat",
			ColsRows:         [2]int{80, 24},
			TerminalColsRows: [2]int{80, 24},
			Started:          selfStamp(t),
		},
	})
	if err != nil {
		t.Fatalf("NewScreenDumper: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() }) //stylegate:ignore the test's own teardown
	return d
}

// sampleDump is the smallest screen the record path accepts: an empty grid.
func sampleDump() lineedit.ScreenDump {
	return lineedit.ScreenDump{Version: lineedit.DumpVersion, Cols: 80, Rows: 24}
}

// TestCapabilityRecordCarriesTheDecisionOnTheLine pins the four fields' presence
// AND their values on the text record — including the DECLARED depth when the
// measurement overruled it, which is the whole question such an artifact exists
// to answer.
func TestCapabilityRecordCarriesTheDecisionOnTheLine(t *testing.T) {
	d := capabilitySink(t)
	d.SetCapabilityReport(CapabilityReport{
		Source:        lineedit.DepthSourceMeasured,
		Depth:         lineedit.DepthNameTruecolor,
		DeclaredDepth: lineedit.DepthName256,
		Palette:       "hermes-dark",
		Probe:         "probe=full cpr=yes colors=256 rgb=yes bytes=52 unread=0 elapsed_ms=3",
	})

	line := d.provenanceLine(sampleDump())
	for _, want := range []string{
		CapabilityField + lineedit.DepthSourceMeasured,
		DepthField + lineedit.DepthNameTruecolor,
		DeclaredDepthField + lineedit.DepthName256,
		PaletteField + "hermes-dark",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("the record line does not carry %q:\n%s", want, line)
		}
	}
	// The record line is PARSED by splitting on spaces: a field value that
	// carried a space would silently become two fields and shift every later
	// one. The probe summary therefore must NOT be here.
	if strings.Contains(line, "probe=full") {
		t.Errorf("the record line carries the probe summary, which contains spaces and would shift the fields that follow it:\n%s", line)
	}
	rec, err := ParseDumpRecord(line)
	if err != nil {
		t.Fatalf("the record line with the capability fields does not parse: %v\n%s", err, line)
	}
	if rec.Geometry == "" {
		t.Errorf("the fields after the capability fields were lost (geometry now reads %q):\n%s", rec.Geometry, line)
	}
}

// TestCapabilityReportIsAbsentWhenNobodyReported pins the "no silent default"
// half: a sink nobody told records nothing at all — not a zero-valued decision
// dressed up as a measurement.
func TestCapabilityReportIsAbsentWhenNobodyReported(t *testing.T) {
	d := capabilitySink(t)
	line := d.provenanceLine(sampleDump())
	if strings.Contains(line, CapabilityField) || strings.Contains(line, DepthField) {
		t.Fatalf("a sink nobody reported to carries a capability field:\n%s", line)
	}
	if _, ok := d.CapabilityReport(); ok {
		t.Fatal("CapabilityReport() reported a decision nobody made")
	}
}

// TestCapabilityRidesTheJSONTwinWithItsProbeEvidence pins the twin: the same
// decision, plus the probe's own line — the evidence the text record cannot
// carry. A JSON reader must be able to re-derive the verdict, not only read it.
func TestCapabilityRidesTheJSONTwinWithItsProbeEvidence(t *testing.T) {
	const probe = "probe=full cpr=yes da1=\\e[?1;2c colors=256 rgb=yes bytes=52 unread=0 elapsed_ms=3"
	d := capabilitySink(t)
	d.SetCapabilityReport(CapabilityReport{
		Source:        lineedit.DepthSourceMeasured,
		Depth:         lineedit.DepthNameTruecolor,
		DeclaredDepth: lineedit.DepthNameBasic,
		Probe:         probe,
	})

	blob, err := json.Marshal(struct {
		Meta jsonFields `json:"meta"`
	}{Meta: d.jsonFields()})
	if err != nil {
		t.Fatalf("the twin does not marshal: %v", err)
	}
	var back struct {
		Meta struct {
			Capability *CapabilityReport `json:"capability"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(blob, &back); err != nil {
		t.Fatalf("the twin does not round-trip: %v", err)
	}
	if back.Meta.Capability == nil {
		t.Fatalf("the capability did not reach the JSON twin: %s", blob)
	}
	if back.Meta.Capability.Probe != probe {
		t.Errorf("the probe evidence round-tripped as %q, want %q", back.Meta.Capability.Probe, probe)
	}
	if back.Meta.Capability.Source != lineedit.DepthSourceMeasured {
		t.Errorf("the source round-tripped as %q", back.Meta.Capability.Source)
	}
}
