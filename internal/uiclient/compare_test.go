package uiclient

// compare_test.go — the containment core of the one-call check, on hand-built
// inputs (no files, no processes): the verdict vocabulary, the direction that
// localizes a loss, and the two refusals that keep a false answer from being
// delivered as a confident one.

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/lineedit"
)

// compareCandidate builds a readable candidate: painted (frames > 0), at the
// given geometry provenance, with the given grid rows.
func compareCandidate(geometry GeometrySource, rows ...string) Candidate {
	return Candidate{
		Path:    "/fixture/chat-screen.dump",
		Surface: SurfaceChat,
		State:   StateExited,
		Frames:  7,
		Record:  DumpRecord{Fields: map[string]string{dumpRecordGeometryField: string(geometry)}},
		Dump:    lineedit.ScreenDump{Cols: 117, Rows: 26, Lines: rows},
	}
}

func TestCompareTranscriptLine_Verdicts(t *testing.T) {
	const text = "中文与 latin 混排 —— dump 必须逐字符还原。"
	cases := []struct {
		name       string
		line       string
		cand       Candidate
		transcript []string
		want       CompareStatus
		wantInGrid bool
		wantInTr   bool
		wantReason string
	}{
		{
			name: "both sides have it",
			line: text, cand: compareCandidate(GeometrySourceLayout, "header", text),
			transcript: []string{text}, want: CompareMatch, wantInGrid: true, wantInTr: true,
		},
		{
			// The band writes a marker column to the LEFT of the model's text (and
			// a line that opens a turn and continues it carries two). The
			// comparison must see through it, or every answer line is reported as
			// a mismatch and the flag is useless for the case it exists for.
			name: "the band's marker column is not part of the text",
			line: text, cand: compareCandidate(GeometrySourceLayout,
				lineedit.TurnMarker+lineedit.TurnMarkerSplit+text, lineedit.ThinkingGutter+text),
			transcript: []string{text}, want: CompareMatch, wantInGrid: true, wantInTr: true,
		},
		{
			name: "the transcript has it and the grid does not",
			line: text, cand: compareCandidate(GeometrySourceLayout, "a different row entirely"),
			transcript: []string{text}, want: CompareMismatch, wantInTr: true,
			wantReason: "the paint lost it",
		},
		{
			name: "the grid has it and the transcript does not",
			line: text, cand: compareCandidate(GeometrySourceLayout, text),
			transcript: []string{"some other text"}, want: CompareMismatch, wantInGrid: true,
			wantReason: "the painted frame is not this session's text",
		},
		{
			// NEITHER side: a different answer from "one lost it", and blaming a
			// layer here would send a reader hunting a paint defect for a line
			// that is simply not the text under discussion.
			name: "neither side has it",
			line: "a line nobody produced", cand: compareCandidate(GeometrySourceLayout, text),
			transcript: []string{text}, want: CompareMismatch,
			wantReason: "no layer can be blamed",
		},
		{
			name: "no transcript at all is not a pass",
			line: text, cand: compareCandidate(GeometrySourceLayout, text),
			transcript: nil, want: CompareNoTranscript, wantInGrid: true,
		},
		{
			name: "an unpainted grid is refused",
			line: text, cand: Candidate{Record: DumpRecord{Fields: map[string]string{}}},
			transcript: []string{text}, want: CompareRefused, wantReason: "never interpreted a render frame",
		},
		{
			// The whole point of the geometry field: at a width nothing measured,
			// a containment answer would read the default's wraps as the screen's.
			name: "a fallback geometry is refused",
			line: text, cand: compareCandidate(GeometrySourceFallback, text),
			transcript: []string{text}, want: CompareRefused, wantReason: "FALLBACK",
		},
		{
			name: "a record that does not state its geometry is refused as a fallback",
			line: text, cand: compareCandidate("", text),
			transcript: []string{text}, want: CompareRefused, wantReason: "FALLBACK",
		},
		{
			name: "an empty line is refused",
			line: "", cand: compareCandidate(GeometrySourceLayout, text),
			transcript: []string{text}, want: CompareRefused, wantReason: "every row contains it",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := CompareTranscriptLine(tc.line, tc.cand, tc.transcript)
			if v.Status != tc.want {
				t.Fatalf("status = %q, want %q (reason: %s)", v.Status, tc.want, v.Reason)
			}
			if v.InGrid != tc.wantInGrid {
				t.Errorf("in_grid = %t, want %t", v.InGrid, tc.wantInGrid)
			}
			if v.InTranscript != tc.wantInTr {
				t.Errorf("in_transcript = %t, want %t", v.InTranscript, tc.wantInTr)
			}
			if tc.wantReason != "" && !strings.Contains(v.Reason, tc.wantReason) {
				t.Errorf("reason = %q, want it to contain %q", v.Reason, tc.wantReason)
			}
		})
	}
}

// TestCompareTranscriptLine_PrefixIsTheTruncationDiagnostic pins the number that
// separates "the band truncated it" (a large prefix) from "it never arrived" (0),
// counted in RUNES: a byte count over a CJK line would report a match for a line
// that diverged after the third character.
func TestCompareTranscriptLine_PrefixIsTheTruncationDiagnostic(t *testing.T) {
	const text = "中文与 latin 混排 —— dump 必须逐字符还原。"
	cand := compareCandidate(GeometrySourceLayout, "中文与 latin 混排 —— dump")
	v := CompareTranscriptLine(text, cand, []string{text})
	if v.Status != CompareMismatch {
		t.Fatalf("status = %q, want %q", v.Status, CompareMismatch)
	}
	if want := len([]rune("中文与 latin 混排 —— dump")); v.GridPrefixRunes != want {
		t.Fatalf("grid_prefix_runes = %d, want %d (a BYTE count would be %d)",
			v.GridPrefixRunes, want, len("中文与 latin 混排 —— dump"))
	}
}

// TestCompareTranscriptLine_GridRowIsFoundByItsOwnRow pins the row index the
// verdict reports: an agent acting on the answer needs to know WHERE on the grid
// the line was, and an off-by-one there sends it to the wrong row.
func TestCompareTranscriptLine_GridRowIsFoundByItsOwnRow(t *testing.T) {
	const text = "目标行"
	cand := compareCandidate(GeometrySourceLayout, "row zero", "row one", text, "row three")
	v := CompareTranscriptLine(text, cand, []string{text})
	if !v.InGrid || v.GridRow != 2 {
		t.Fatalf("in_grid=%t grid_row=%d, want true and 2", v.InGrid, v.GridRow)
	}
	// A miss reports -1, never 0: "not found" and "found on the first row" must
	// not share a value.
	miss := CompareTranscriptLine("absent", cand, []string{"absent"})
	if miss.InGrid || miss.GridRow != -1 {
		t.Fatalf("a miss reports in_grid=%t grid_row=%d, want false and -1", miss.InGrid, miss.GridRow)
	}
}
