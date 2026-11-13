// flatten_test.go — the line-rendering contract. The load-bearing test is
// TestLineScannerMatchesBufioScannerOnLegacy: a LEGACY log read through
// LineScanner must produce the byte-identical line sequence a bufio.Scanner
// produced over the same file. That equality is what lets one walk serve both
// framings — if it ever breaks, every line-oriented reader in the system
// (round spans, verbatim views, response digests) silently renumbers.
package rawlogfmt

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

// fixtureRecs is the one record list both fixtures are built from, so the two
// files describe the SAME log in two framings (a test that wrote them separately
// could drift and would still pass).
type fixtureRec struct {
	tag     string
	ann     string
	payload string
}

func fixtureRecs() []fixtureRec {
	return []fixtureRec{
		{TagRequest, "", `{"model":"m"}`},
		{TagChunk, "", "event: content_block_delta\ndata: {\"delta\":{\"text\":\"hi\"}}\n"},
		{TagChunk, "", `{"choices":[]}`},
		// The glued HTTP-ERROR shape: the diagnostics' first bytes sit on the marker
		// line itself (internal/deepseek/minimal_core.go's non200RawLog passes the
		// direction with no newline), so those bytes are PAYLOAD.
		{TagError, "", "← streaming attempt — HTTP 500 (non-OK)\nbody line 1\n"},
		{TagProbeRequest, ProbeStreamAnnotation, `{"model":"m","stream":true}`},
		{TagResponse, "", `{"ok":true}`},
	}
}

// fixtureHeader is the creation header both fixtures open with. It is not a record
// (a `#` line carries no direction) but it IS a line of the file, and every
// line-oriented reader must still see it.
const fixtureHeader = "# gopi session raw HTTP log — created 2026-01-01T00:00:00Z pid=1 log=x.log"

// legacyFixture writes the records the way the legacy logger did.
func legacyFixture() string {
	var sb strings.Builder
	sb.WriteString(fixtureHeader + "\n")
	for _, r := range fixtureRecs() {
		marker, ok := LegacyMarker(r.tag, r.ann)
		if !ok {
			panic("no legacy marker for " + r.tag)
		}
		sb.WriteString(marker)
		sb.WriteString(r.payload)
		sb.WriteString(string(RecordTerminator))
	}
	return sb.String()
}

// framedFixture writes the SAME records in the length-prefixed framing.
func framedFixture(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	buf.WriteString(fixtureHeader + "\n")
	for _, r := range fixtureRecs() {
		buf.Write(EncodeAnnotated(r.tag, r.ann, []byte(r.payload)))
	}
	return buf.Bytes()
}

// scanLines returns every line a bufio.Scanner sees in b (the reader this system
// used before the framed framing existed).
func scanLines(t *testing.T, b []byte) []string {
	t.Helper()
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	var out []string
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan fixture: %v", err)
	}
	return out
}

// scanRecords walks body through a LineScanner, returning every line and the tag
// of the record it belongs to ("" for a line that belongs to no record: the
// creation header, a trim marker, a framing trailer).
func scanRecords(t *testing.T, body []byte, format Format) (lines, tags []string) {
	t.Helper()
	s := NewLineScannerFrom(bytes.NewReader(body), format)
	for s.Scan() {
		lines = append(lines, s.Text())
		tags = append(tags, s.Record().Tag)
	}
	if err := s.Err(); err != nil {
		t.Fatalf("LineScanner: %v", err)
	}
	return lines, tags
}

// TestLineScannerMatchesBufioScannerOnLegacy is the equivalence proof described in
// the file header: same lines, same order, byte-identical.
func TestLineScannerMatchesBufioScannerOnLegacy(t *testing.T) {
	body := []byte(legacyFixture())
	want := scanLines(t, body)
	got, _ := scanRecords(t, body, FormatLegacy)
	if len(got) != len(want) {
		t.Fatalf("line count: LineScanner %d, bufio.Scanner %d\n got %q\nwant %q", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d: LineScanner %q, bufio.Scanner %q", i+1, got[i], want[i])
		}
	}
}

// TestLineScannerFramedRendersHeaderThenPayloadLines pins the framed side line by
// line. The expected header text is DERIVED from the payload length, never
// hand-written: a hand-copied number would agree with itself and catch nothing.
func TestLineScannerFramedRendersHeaderThenPayloadLines(t *testing.T) {
	got, tags := scanRecords(t, framedFixture(t), FormatFramed)

	want := []string{fixtureHeader}
	for _, r := range fixtureRecs() {
		want = append(want, HeaderLineAnnotated(r.tag, len(r.payload), r.ann)[:len(HeaderLineAnnotated(r.tag, len(r.payload), r.ann))-1])
		lines, trailer := SplitFramingNewline([]byte(r.payload))
		want = append(want, lines...)
		if trailer {
			want = append(want, FramingTrailerLine())
		}
	}
	if len(got) != len(want) {
		t.Fatalf("line count = %d, want %d\n got %q\nwant %q", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d = %q, want %q\n got %q\nwant %q", i+1, got[i], want[i], got, want)
		}
	}
	// The creation header belongs to no record; every later line belongs to one.
	if tags[0] != "" {
		t.Fatalf("the creation header line must belong to no record, got tag %q", tags[0])
	}
	if tags[len(tags)-1] != TagResponse {
		t.Fatalf("the last line must belong to the response record, got tag %q", tags[len(tags)-1])
	}
}

// TestBothFramingsRecoverTheSameRecords is the format-independence claim at the
// RECORD level: the same log written in either framing decodes to the same
// (tag, annotation, payload) sequence. It is checked on BYTES rather than on
// rendered lines because the legacy writer glues an HTTP-ERROR marker onto its
// payload's first line — its LINE rendering legitimately differs by that glue
// while its payload bytes do not.
func TestBothFramingsRecoverTheSameRecords(t *testing.T) {
	type ev struct{ tag, ann, payload string }
	walk := func(t *testing.T, body []byte, format Format) []ev {
		t.Helper()
		r := NewReaderFrom(bytes.NewReader(body), format)
		var out []ev
		for {
			rec, err := r.Next()
			if err != nil {
				if errors.Is(err, io.EOF) {
					return out
				}
				t.Fatalf("decode: %v", err)
			}
			out = append(out, ev{rec.Tag, rec.Annotation, string(rec.Payload)})
		}
	}
	legacy := walk(t, []byte(legacyFixture()), FormatLegacy)
	framed := walk(t, framedFixture(t), FormatFramed)
	if len(legacy) != len(framed) {
		t.Fatalf("record count: legacy %d, framed %d", len(legacy), len(framed))
	}
	for i := range legacy {
		if legacy[i] != framed[i] {
			t.Fatalf("record %d:\n legacy %+v\n framed %+v", i+1, legacy[i], framed[i])
		}
	}
}

// TestFramedHeaderLengthIsThePayloadByteCount states the acceptance criterion a
// reader can check on any log: a framed header's length field equals the payload's
// actual byte count.
func TestFramedHeaderLengthIsThePayloadByteCount(t *testing.T) {
	r := NewReaderFrom(bytes.NewReader(framedFixture(t)), FormatFramed)
	recs := 0
	for {
		rec, err := r.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("decode: %v", err)
		}
		recs++
		header := HeaderText(rec, FormatFramed)
		_, stated, _, ok := ParseHeaderLineAnnotated([]byte(header))
		if !ok {
			t.Fatalf("record %d: header %q is not a framed header", recs, header)
		}
		if stated != len(rec.Payload) {
			t.Fatalf("record %d: header states %d payload bytes, decoded %d", recs, stated, len(rec.Payload))
		}
	}
	if recs != len(fixtureRecs()) {
		t.Fatalf("decoded %d records, want %d", recs, len(fixtureRecs()))
	}
}

// TestLineScannerRecordCarriesDirection is the point of the Record() accessor: a
// reader that must NOT branch on header TEXT (a framed header says `R 13`, a legacy
// one says `→ REQUEST`) asks the tag instead, and gets the same answer for the same
// event in either framing.
//
// Records are grouped by IDENTITY, not by tag: the fixture holds two consecutive
// chunks, and grouping by tag collapsed them into one (measured — the first version
// of this test did exactly that and reported [R C E P S] against the fixture's
// [R C C E P S]). A record's lines all carry that record, and the identity that
// distinguishes two same-tag records is their Offset.
func TestLineScannerRecordCarriesDirection(t *testing.T) {
	type ev struct {
		tag    string
		offset int64
		lines  int
	}
	walk := func(t *testing.T, body []byte, format Format) (events []ev, comments int) {
		t.Helper()
		s := NewLineScannerFrom(bytes.NewReader(body), format)
		var cur *ev
		for s.Scan() {
			rec := s.Record()
			if rec.HeaderLen == 0 {
				comments++
				cur = nil
				continue
			}
			if cur != nil && cur.tag == rec.Tag && cur.offset == rec.Offset {
				cur.lines++
				continue
			}
			events = append(events, ev{tag: rec.Tag, offset: rec.Offset, lines: 1})
			cur = &events[len(events)-1]
		}
		if err := s.Err(); err != nil {
			t.Fatalf("LineScanner: %v", err)
		}
		return events, comments
	}
	wantTags := make([]string, 0, len(fixtureRecs()))
	for _, r := range fixtureRecs() {
		wantTags = append(wantTags, r.tag)
	}
	for _, tc := range []struct {
		name   string
		body   []byte
		format Format
	}{
		{"legacy", []byte(legacyFixture()), FormatLegacy},
		{"framed", framedFixture(t), FormatFramed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events, comments := walk(t, tc.body, tc.format)
			got := make([]string, 0, len(events))
			for _, e := range events {
				got = append(got, e.tag)
			}
			if strings.Join(got, ",") != strings.Join(wantTags, ",") {
				t.Fatalf("tags = %v, want %v", got, wantTags)
			}
			if comments != 1 {
				t.Fatalf("lines belonging to no record = %d, want 1 (the creation header)", comments)
			}
			for _, e := range events {
				if e.lines < 1 {
					t.Fatalf("record %s at offset %d spans %d lines", e.tag, e.offset, e.lines)
				}
			}
			// Offsets must be strictly increasing: a decoder that replays a record
			// would show up here, and a reader that seeks by offset would then loop.
			for i := 1; i < len(events); i++ {
				if events[i].offset <= events[i-1].offset {
					t.Fatalf("record offsets are not increasing: %v", events)
				}
			}
		})
	}
}

// TestHeaderTextLegacyHTTPErrorDoesNotInventANewline pins the glued-marker
// rendering: the header text is the marker word alone, and the diagnostics are
// re-emitted exactly once — as payload, where the file has them.
func TestHeaderTextLegacyHTTPErrorDoesNotInventANewline(t *testing.T) {
	glued := Record{Tag: TagError, Payload: []byte("boom"), HeaderLen: len(MarkerHTTPError), HeaderTerminated: false}
	if got := HeaderText(glued, FormatLegacy); got != MarkerHTTPError {
		t.Fatalf("HeaderText = %q, want %q", got, MarkerHTTPError)
	}
	// A bare `HTTP-ERROR\n` (a hand-written or future writer's line) DOES carry its
	// terminator — the flag says which, not the tag.
	bare := glued
	bare.HeaderTerminated = true
	if got := HeaderText(bare, FormatLegacy); got != MarkerHTTPError {
		t.Fatalf("HeaderText = %q, want %q", got, MarkerHTTPError)
	}
}

// TestSplitFramingNewline pins the one-trailing-newline rule on every shape.
func TestSplitFramingNewline(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    []string
		trailer bool
	}{
		{"no newline", `{"a":1}`, []string{`{"a":1}`}, false},
		{"one trailing newline", "line\n", []string{"line"}, true},
		{"two lines", "a\nb\n", []string{"a", "b"}, true},
		{"internal blank line", "a\n\nb\n", []string{"a", "", "b"}, true},
		{"only a newline", "\n", []string{""}, true},
		{"empty", "", nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lines, trailer := SplitFramingNewline([]byte(tc.payload))
			if trailer != tc.trailer {
				t.Fatalf("trailer = %v, want %v", trailer, tc.trailer)
			}
			if strings.Join(lines, "|") != strings.Join(tc.want, "|") {
				t.Fatalf("lines = %q, want %q", lines, tc.want)
			}
		})
	}
}

// TestFramedPayloadContainingMarkerTextIsOneRecord is the whole point of the format
// change, stated as a rendering fact: a payload carrying the legacy marker
// vocabulary AND a framed-header lookalike is ONE record, and its lines contain
// that text without any of them opening a new record.
func TestFramedPayloadContainingMarkerTextIsOneRecord(t *testing.T) {
	nasty := "→ REQUEST\n← CHUNK\nHTTP-ERROR\nR 5\nC 0\n"
	body := EncodeAnnotated(TagRequest, "", []byte(nasty))

	s := NewLineScannerFrom(bytes.NewReader(body), FormatFramed)
	var lines []string
	offsets := map[int64]bool{}
	for s.Scan() {
		lines = append(lines, s.Text())
		offsets[s.Record().Offset] = true
	}
	if err := s.Err(); err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(offsets) != 1 {
		t.Fatalf("distinct record offsets = %d, want 1 (marker text inside a payload is data)", len(offsets))
	}
	// header line, the payload's 5 lines, then the framing newline.
	if len(lines) != 1+5+1 {
		t.Fatalf("line count = %d, want %d (%q)", len(lines), 1+5+1, lines)
	}
	header := HeaderLine(TagRequest, len(nasty))
	if want := strings.TrimSuffix(header, string(RecordTerminator)); lines[0] != want {
		t.Fatalf("header line = %q, want %q", lines[0], want)
	}
	want := []string{"→ REQUEST", "← CHUNK", "HTTP-ERROR", "R 5", "C 0", ""}
	for i := range want {
		if lines[i+1] != want[i] {
			t.Fatalf("payload line %d = %q, want %q (%q)", i+1, lines[i+1], want[i], lines)
		}
	}
}
