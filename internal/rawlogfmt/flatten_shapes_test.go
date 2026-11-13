// flatten_shapes_test.go — the shape MATRIX the line-rendering contract needed and
// did not have.
//
// TestLineScannerMatchesBufioScannerOnLegacy was green through a defect that broke
// real users (see below), because its one fixture gave every record the SAME tail
// shape: a payload that does not end in a newline. A fixture that never varies the
// dimension it is supposed to pin cannot fail on that dimension — the one-trailing-
// newline rule is entirely about how a record ENDS, so a fixture whose records all
// end alike tests one cell of the rule and calls it the matrix.
//
// THE DEFECT THIS PINS (measured on a real 1.79 GB legacy session log, 2026-11-02):
// the legacy decoder trimmed the record's own closing terminator off the payload but
// never reported that it had, and the renderer decided from that same flag whether
// to put the terminator back. Every legacy record therefore rendered as if it were a
// truncated framed tail: blank-separated SSE events lost their separator line and a
// record whose payload is EMPTY gained a line the file does not have — `session raw
// --rounds` reported the last round's END as 7 where the file's own last line is 6.
package rawlogfmt

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// tailShapes is one record shape per way a record can END, which is the dimension
// the one-trailing-newline rule lives on. Every shape is written in both framings
// and must render the same lines as a bufio.Scanner over the legacy file.
func tailShapes() []struct {
	name    string
	tag     string
	payload string
} {
	return []struct {
		name    string
		tag     string
		payload string
	}{
		// The shape the old fixture had, and the ONLY one it had.
		{"payload without a newline", TagRequest, `{"a":1}`},
		// An SSE event ends with a blank line, so its payload ends in a newline and
		// the file carries one FURTHER terminator. Fusing the two lost the separator.
		{"payload ending in a newline", TagChunk, "event: message_start\ndata: {}\n"},
		// A multi-line body whose last line also ends in a newline.
		{"multi-line body ending in a newline", TagResponse, `{"k":"v"}` + "\n\n"},
		// THE ONE THAT BROKE: an empty payload. The legacy writer emitted the marker
		// line and nothing else — one line in the file — and the reader invented a
		// second.
		{"empty payload", TagRequest, ""},
		// The glued HTTP-ERROR shape: the marker carries no terminator of its own and
		// the diagnostics' first bytes are payload.
		{"glued HTTP-ERROR", TagError, "← streaming attempt — HTTP 500 (non-OK)\nbody\n"},
	}
}

// TestLineScannerMatchesBufioScannerOnEveryTailShape is the equivalence claim over
// the SHAPE matrix: for each way a record can end, the lines LineScanner hands out
// are byte-identical to what bufio.Scanner saw in the legacy file.
func TestLineScannerMatchesBufioScannerOnEveryTailShape(t *testing.T) {
	for _, shape := range tailShapes() {
		t.Run(shape.name, func(t *testing.T) {
			legacy := legacyBodyFor(t, shape.tag, shape.payload)
			want := scanLines(t, []byte(legacy))
			got, _ := scanRecords(t, []byte(legacy), FormatLegacy)
			if len(got) != len(want) {
				t.Fatalf("line count: LineScanner %d, bufio.Scanner %d\n got %q\nwant %q", len(got), len(want), got, want)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("line %d: LineScanner %q, bufio.Scanner %q\n got %q\nwant %q", i+1, got[i], want[i], got, want)
				}
			}
		})
	}
}

// TestBothFramingsRenderTheSameLinesForEveryTailShape states the format-independent
// half: the same record written either way renders the same PAYLOAD lines. This is
// the property a consumer relies on when it re-reads a line span from a log whose
// framing it does not know.
//
// The comparison starts at line 1, because line 0 is the record's HEADER and its
// text is the one thing the framings legitimately spell differently (`→ REQUEST` vs
// `R 412`) — that is what HeaderText exists to render per framing. Two further
// differences are by construction, not by permission:
//
//   - the glued HTTP-ERROR: in a legacy file its diagnostics' first bytes ride the
//     marker line itself (that is the legacy writer's own shape), so its first
//     payload line carries the glue;
//   - an EMPTY payload: a framed record still spends its framing newline, so a
//     length-prefixed `R 0` record occupies two file lines where a legacy marker
//     occupies one. That is the framing's own cost and predates this file.
func TestBothFramingsRenderTheSameLinesForEveryTailShape(t *testing.T) {
	for _, shape := range tailShapes() {
		t.Run(shape.name, func(t *testing.T) {
			if shape.payload == "" {
				// The empty-payload case is asserted by its own test below, per format.
				return
			}
			legacyLines, _ := scanRecords(t, []byte(legacyBodyFor(t, shape.tag, shape.payload)), FormatLegacy)
			framedLines, _ := scanRecords(t, EncodeAnnotated(shape.tag, "", []byte(shape.payload)), FormatFramed)
			// The legacy body carries the creation header, which belongs to no record;
			// the framed body here does not. Line 0 is therefore the creation header on
			// one side and the record's own header on the other, so it is dropped on
			// both — and payloadLines then drops each record's header line too.
			if len(legacyLines) > 0 && legacyLines[0] == fixtureHeader {
				legacyLines = legacyLines[1:]
			}

			got, want := payloadLines(framedLines), payloadLines(legacyLines)
			if shape.tag == TagError && len(want) > 0 {
				// In a legacy file the diagnostics' first bytes are GLUED onto the
				// marker line, so the record's own "header line" is the marker word plus
				// the start of the payload. Dropping the header therefore drops payload
				// bytes too; put them back where the framed side has them.
				if trimmed := strings.TrimPrefix(legacyLines[0], string(MarkerHTTPError)); trimmed != legacyLines[0] {
					want = append([]string{trimmed}, want...)
				}
			}
			if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
				t.Fatalf("payload lines differ:\n legacy %q\n framed %q", want, got)
			}
		})
	}
}

// payloadLines drops the record's header line (line 0), which each framing spells
// in its own vocabulary.
func payloadLines(lines []string) []string {
	if len(lines) == 0 {
		return nil
	}
	return lines[1:]
}

// TestEmptyPayloadRecordOccupiesExactlyOneLine pins the measured user-visible
// symptom directly, without a comparison: a record whose payload is empty occupies
// exactly the lines the FILE holds, and a line-span view that says otherwise
// renumbers everything after it.
//
// The two framings answer differently here, and both answers are the file's own
// bytes: a legacy marker line is ONE line, while a length-prefixed header still
// spends its framing newline, so `R 0` is two file lines. What must NEVER happen in
// either is a line the file does not have — which is what the legacy side did
// (MEASURED: `session raw --rounds` reported the last round's END as 7 where the
// file's own last line is 6).
func TestEmptyPayloadRecordOccupiesExactlyOneLine(t *testing.T) {
	for _, tc := range []struct {
		name   string
		body   string
		format Format
		want   int
	}{
		{"legacy", fixtureHeader + "\n" + string(MarkerRequest) + string(RecordTerminator), FormatLegacy, 2},
		{"framed", fixtureHeader + "\n" + string(Encode(TagRequest, nil)), FormatFramed, 3},
	} {
		s := NewLineScannerFrom(bytes.NewReader([]byte(tc.body)), tc.format)
		lines := 0
		for s.Scan() {
			lines++
		}
		if err := s.Err(); err != nil {
			t.Fatalf("%s: walk: %v", tc.name, err)
		}
		if lines != tc.want {
			t.Fatalf("%s: %d lines, want %d (the creation header + the record's own file lines)", tc.name, lines, tc.want)
		}
	}
}

// TestLegacyDecoderReportsTheTrimmedTerminator pins the DECODER half of the defect:
// the reader trims the record's closing terminator off the payload, and it must say
// so, because the renderer decides from that answer whether to put it back.
func TestLegacyDecoderReportsTheTrimmedTerminator(t *testing.T) {
	body := legacyBodyFor(t, TagRequest, "")
	r := NewReaderFrom(bytes.NewReader([]byte(body)), FormatLegacy)
	rec, err := r.Next()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !rec.Terminated {
		t.Fatalf("a legacy record closed by a terminator reported Terminated=false, so the renderer cannot know the terminator was trimmed")
	}
	// EOF is correct here: the file holds exactly one record.
	if _, err := r.Next(); err == nil || !strings.Contains(err.Error(), io.EOF.Error()) {
		if err != io.EOF {
			t.Fatalf("second record: %v, want EOF", err)
		}
	}
}

// legacyBodyFor writes one record the way the legacy logger did: the creation header,
// then the marker (with its own terminator), then the payload verbatim, then the
// terminator that closes the record.
func legacyBodyFor(t *testing.T, tag, payload string) string {
	t.Helper()
	marker, ok := LegacyMarker(tag, "")
	if !ok {
		t.Fatalf("no legacy marker for tag %q", tag)
	}
	return fixtureHeader + "\n" + marker + payload + string(RecordTerminator)
}
