package rawlogfmt

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEncodeDecodeRoundTrip pins the format contract: a payload survives the
// framed encoding BYTE FOR BYTE, and the header states its exact length.
func TestEncodeDecodeRoundTrip(t *testing.T) {
	payloads := [][]byte{
		[]byte(`{"model":"deepseek-flash","messages":[]}`),
		[]byte("event: content_block_delta\ndata: {\"i\":0}\n"),
		[]byte(""),
		[]byte("→ REQUEST\n← CHUNK\nR 5\n"),
		bytes.Repeat([]byte("x"), 70000),
	}
	for _, p := range payloads {
		enc := Encode(TagRequest, p)
		header := HeaderLine(TagRequest, len(p))
		if !bytes.HasPrefix(enc, []byte(header)) {
			t.Fatalf("encoded record does not open with its header: %q", enc[:len(header)])
		}
		if got, want := len(enc), len(header)+len(p)+1; got != want {
			t.Fatalf("encoded length = %d, want %d", got, want)
		}
		// The framing newline is the ONLY byte Encode adds after the payload.
		if enc[len(enc)-1] != RecordTerminator {
			t.Fatalf("record does not end with the framing newline")
		}
		r := NewReaderFrom(bytes.NewReader(enc), FormatFramed)
		rec, err := r.Next()
		if err != nil {
			t.Fatalf("Next() on a fresh record: %v", err)
		}
		if !bytes.Equal(rec.Payload, p) {
			t.Fatalf("payload changed: got %d bytes, want %d", len(rec.Payload), len(p))
		}
		if !rec.Terminated {
			t.Fatalf("record reported as unterminated")
		}
		if _, err := r.Next(); !errors.Is(err, io.EOF) {
			t.Fatalf("second Next() = %v, want io.EOF", err)
		}
	}
}

// TestPayloadCarryingMarkerTextCannotAmbiguate is acceptance criterion 4: a
// payload that CONTAINS the legacy markers and a header-shaped line of its own is
// framed by its LENGTH, so it decodes to exactly its own bytes.
func TestPayloadCarryingMarkerTextCannotAmbiguate(t *testing.T) {
	nast := []byte("→ REQUEST\n← CHUNK\n← RESPONSE\nHTTP-ERROR\nR 5\nC 0\nS 12\n# gopi session gc — TRIMMED\n")
	log := append(Encode(TagRequest, nast), Encode(TagChunk, []byte("data: {}\n"))...)
	r := NewReaderFrom(bytes.NewReader(log), FormatFramed)
	first, err := r.Next()
	if err != nil {
		t.Fatalf("first record: %v", err)
	}
	if first.Tag != TagRequest || !bytes.Equal(first.Payload, nast) {
		t.Fatalf("first record = %q/%q, want the whole payload verbatim", first.Tag, first.Payload)
	}
	second, err := r.Next()
	if err != nil {
		t.Fatalf("second record: %v", err)
	}
	if second.Tag != TagChunk || string(second.Payload) != "data: {}\n" {
		t.Fatalf("second record = %q/%q", second.Tag, second.Payload)
	}
	if _, err := r.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("trailing Next() = %v, want io.EOF", err)
	}
}

// TestParseHeaderLineRejectsPayloadShapes pins the strictness that makes a framed
// header unambiguous: only one declared tag byte, one space, digits.
func TestParseHeaderLineRejectsPayloadShapes(t *testing.T) {
	accept := map[string]struct {
		tag  string
		size int
	}{
		"R 0\n":     {TagRequest, 0},
		"C 91\n":    {TagChunk, 91},
		"S 4\r\n":   {TagResponse, 4},
		"E 12\n":    {TagError, 12},
		"P 7\n":     {TagProbeRequest, 7},
		"R 00012\n": {TagRequest, 12},
	}
	for in, want := range accept {
		tag, n, ok := ParseHeaderLine([]byte(in))
		if !ok || tag != want.tag || n != want.size {
			t.Fatalf("ParseHeaderLine(%q) = (%q,%d,%v), want (%q,%d,true)", in, tag, n, ok, want.tag, want.size)
		}
	}
	for _, in := range []string{
		"→ REQUEST\n", "← CHUNK\n", "HTTP-ERROR", "data: R 5\n",
		"R  5\n", "RR 5\n", "R 5 \n", "R -5\n", "R 5x\n", "X 5\n",
		"R\n", "R \n", "R 1234567890123456789012\n", "",
	} {
		if _, _, ok := ParseHeaderLine([]byte(in)); ok {
			t.Fatalf("ParseHeaderLine(%q) accepted a non-header", in)
		}
	}
}

// TestLegacyMarkerMatchingIsWholeLine pins that a line CONTAINING marker text is
// not a marker — the legacy reader's only defence.
func TestLegacyMarkerMatchingIsWholeLine(t *testing.T) {
	yes := []string{MarkerRequest, MarkerResponse, MarkerChunk, MarkerHTTPError,
		MarkerRequest + " (1214-guard)", MarkerRequest + " (1214-guard, stream)"}
	for _, line := range yes {
		if !IsLegacyMarkerLine(line) {
			t.Fatalf("IsLegacyMarkerLine(%q) = false, want true", line)
		}
	}
	no := []string{"", "  " + MarkerRequest, MarkerRequest + " tail", "x" + MarkerRequest,
		MarkerRequest + "(1214-guard)", MarkerChunk + " (unclosed"}
	for _, line := range no {
		if IsLegacyMarkerLine(line) {
			t.Fatalf("IsLegacyMarkerLine(%q) = true, want false (payload text is not a marker)", line)
		}
	}
}

// writeLegacy writes a marker-framed log exactly the way the legacy logger did
// (direction line, payload, one newline) so the reader is exercised against the
// real on-disk shape rather than a re-imagining of it.
func writeLegacy(t *testing.T, dir string, records [][2]string) string {
	t.Helper()
	var sb strings.Builder
	sb.WriteString("# gopi session raw HTTP log — created 2026-10-29T21:35:18Z pid=1 session=x log=x.raw.log\n")
	for _, r := range records {
		sb.WriteString(r[0])
		sb.WriteString(r[1])
		sb.WriteString("\n")
	}
	p := filepath.Join(dir, "legacy.raw.log")
	if err := os.WriteFile(p, []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return p
}

// TestLegacyFramingDecodesExactly is the compat half of the contract: the legacy
// byte shape decodes to the same records the framed shape would, including the
// multi-line SSE event and the annotated probe marker.
func TestLegacyFramingDecodesExactly(t *testing.T) {
	dir := t.TempDir()
	path := writeLegacy(t, dir, [][2]string{
		{MarkerRequest + "\n", `{"model":"m"}`},
		{MarkerChunk + "\n", "event: a\ndata: {\"x\":1}\n"},
		{MarkerChunk + "\n", "event: b\n"},
		{MarkerResponse + "\n", `{"ok":true}`},
		{MarkerRequest + " (1214-guard, stream)" + "\n", `{"model":"guard"}`},
		{"HTTP-ERROR", "— ← messages — HTTP 500 (non-OK)\n{\"error\":\"x\"}"},
		{MarkerChunk + "\n", "data: {}\n"},
	})
	f, err := Detect(path)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if f != FormatLegacy {
		t.Fatalf("Detect = %v, want legacy", f)
	}
	r, err := OpenReader(path, f)
	if err != nil {
		t.Fatalf("OpenReader: %v", err)
	}
	type want struct {
		tag        string
		annotation string
		payload    string
	}
	wants := []want{
		{TagRequest, "", `{"model":"m"}`},
		{TagChunk, "", "event: a\ndata: {\"x\":1}\n"},
		{TagChunk, "", "event: b\n"},
		{TagResponse, "", `{"ok":true}`},
		{TagProbeRequest, "1214-guard, stream", `{"model":"guard"}`},
		{TagError, "", "— ← messages — HTTP 500 (non-OK)\n{\"error\":\"x\"}"},
		{TagChunk, "", "data: {}\n"},
	}
	for i, w := range wants {
		rec, err := r.Next()
		if err != nil {
			t.Fatalf("record %d: %v", i+1, err)
		}
		if rec.Tag != w.tag || rec.Annotation != w.annotation || string(rec.Payload) != w.payload {
			t.Fatalf("record %d = %q/%q/%q, want %q/%q/%q", i+1,
				rec.Tag, rec.Annotation, rec.Payload, w.tag, w.annotation, w.payload)
		}
	}
	if _, err := r.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("trailing Next() = %v, want io.EOF", err)
	}
}

// TestEncodeLegacyReproducesTheLegacyBytes pins the OTHER direction: a continued
// legacy file is appended to with bytes a legacy reader cannot tell from the
// file's own history.
func TestEncodeLegacyReproducesTheLegacyBytes(t *testing.T) {
	dir := t.TempDir()
	path := writeLegacy(t, dir, [][2]string{{MarkerRequest + "\n", `{"a":1}`}})
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("open for append: %v", err)
	}
	for _, rec := range []struct {
		tag, ann, payload string
	}{
		{TagRequest, "", `{"b":2}`},
		{TagProbeRequest, ProbeStreamAnnotation, `{"guard":true}`},
		{TagChunk, "", "data: x\n"},
		{TagError, "", "— ← messages — HTTP 500 (non-OK)\nbody"},
	} {
		b, err := EncodeLegacy(rec.tag, rec.ann, []byte(rec.payload))
		if err != nil {
			t.Fatalf("EncodeLegacy(%q): %v", rec.tag, err)
		}
		if _, err := f.Write(b); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	for _, want := range []string{
		MarkerRequest + "\n{\"a\":1}\n",
		MarkerRequest + "\n{\"b\":2}\n",
		MarkerRequest + " (1214-guard, stream)\n{\"guard\":true}\n",
		MarkerChunk + "\ndata: x\n\n",
		MarkerHTTPError + "— ← messages — HTTP 500 (non-OK)\nbody\n",
	} {
		if !bytes.Contains(raw, []byte(want)) {
			t.Fatalf("appended legacy bytes do not contain %q; file:\n%s", want, raw)
		}
	}
	// And the framing still reads back as legacy.
	if f, err := Detect(path); err != nil || f != FormatLegacy {
		t.Fatalf("Detect after appending legacy = %v/%v, want legacy", f, err)
	}
	// MEASURED LIMITATION, stated instead of papered over: the legacy framing
	// CANNOT detect a framed record appended into it. A legacy payload runs to the
	// next marker line, so appended framed bytes are absorbed into the previous
	// record's payload — this test pins that fact, and the WRITER is what prevents
	// it (a run continues a file in the file's OWN framing; see
	// TestEncodeLegacyNeverEmitsAFramedHeader).
	mixed, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("open for append: %v", err)
	}
	if _, err := mixed.Write(Encode(TagRequest, []byte(`{"c":3}`))); err != nil {
		t.Fatalf("append framed: %v", err)
	}
	// The append and the close are BOTH part of the fixture's setup, so a
	// failure in either one leaves the file in a state the assertions below
	// would misread as a reader defect. Checked, not discarded (Rule 15).
	if err := mixed.Close(); err != nil {
		t.Fatalf("close after append: %v", err)
	}
	r, err := OpenReader(path, FormatLegacy)
	if err != nil {
		t.Fatalf("OpenReader: %v", err)
	}
	var last string
	var recErr error
	for {
		rec, err := r.Next()
		if err != nil {
			recErr = err
			break
		}
		last = rec.Tag + ":" + string(rec.Payload)
	}
	if !errors.Is(recErr, io.EOF) {
		t.Fatalf("walk of a mixed file ended with %v, want io.EOF (legacy cannot detect the mixture)", recErr)
	}
	if !strings.Contains(last, `{"c":3}`) {
		t.Fatalf("the appended framed bytes did not land inside the last legacy payload; last = %q", last)
	}
	if f, err := Detect(path); err != nil || f != FormatLegacy {
		t.Fatalf("Detect(mixed) = %v/%v, want legacy (the FIRST record decides)", f, err)
	}
}

// TestEncodeLegacyNeverEmitsAFramedHeader is the WRITER-side guard that replaces
// the detection the legacy framing cannot provide: every legacy encoding starts
// with a marker line, so no legacy write can plant a framed header in a file.
func TestEncodeLegacyNeverEmitsAFramedHeader(t *testing.T) {
	for _, tag := range []string{TagRequest, TagProbeRequest, TagResponse, TagChunk, TagError} {
		for _, ann := range []string{"", ProbeAnnotation, ProbeStreamAnnotation} {
			b, err := EncodeLegacy(tag, ann, []byte(`{"payload":true}`))
			if err != nil {
				if tag == TagError && ann != "" {
					continue // refused by design: a diagnostics record carries no annotation
				}
				t.Fatalf("EncodeLegacy(%q,%q): %v", tag, ann, err)
			}
			firstLine, _, _ := bytes.Cut(b, []byte{RecordTerminator})
			if _, _, ok := ParseHeaderLine(append(firstLine, RecordTerminator)); ok {
				t.Fatalf("EncodeLegacy(%q,%q) opened with a FRAMED header %q", tag, ann, firstLine)
			}
			if !IsLegacyMarkerLine(string(firstLine)) {
				t.Fatalf("EncodeLegacy(%q,%q) did not open with a legacy marker: %q", tag, ann, firstLine)
			}
		}
	}
	if _, err := EncodeLegacy(TagError, ProbeAnnotation, nil); err == nil {
		t.Fatalf("EncodeLegacy(error, annotation) was accepted; it must be refused")
	}
}

// TestDetectIsExactForBothFramings pins the discriminator, including the header
// lines and blank lines both formats carry before their first record.
func TestDetectIsExactForBothFramings(t *testing.T) {
	dir := t.TempDir()
	framed := filepath.Join(dir, "framed.raw.log")
	body := append([]byte("# gopi session raw HTTP log — created 2026-11-02T00:00:00Z pid=1\n"), Encode(TagRequest, []byte(`{"a":1}`))...)
	body = append(body, []byte("# gopi session gc --trim-raw-log — TRIMMED at …\n")...)
	body = append(body, Encode(TagChunk, []byte("data: {}\n"))...)
	if err := os.WriteFile(framed, body, 0o644); err != nil {
		t.Fatalf("write framed: %v", err)
	}
	if f, err := Detect(framed); err != nil || f != FormatFramed {
		t.Fatalf("Detect(framed) = %v/%v, want framed", f, err)
	}
	legacy := writeLegacy(t, dir, [][2]string{{MarkerRequest + "\n", `{"a":1}`}})
	if f, err := Detect(legacy); err != nil || f != FormatLegacy {
		t.Fatalf("Detect(legacy) = %v/%v, want legacy", f, err)
	}
	empty := filepath.Join(dir, "empty.raw.log")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatalf("write empty: %v", err)
	}
	if f, err := Detect(empty); err != nil || f != FormatLegacy {
		t.Fatalf("Detect(empty) = %v/%v, want legacy (nothing to decode)", f, err)
	}
	headersOnly := filepath.Join(dir, "headers.raw.log")
	if err := os.WriteFile(headersOnly, []byte("# gopi session raw HTTP log — created x\n"), 0o644); err != nil {
		t.Fatalf("write headers-only: %v", err)
	}
	if f, err := Detect(headersOnly); err != nil || f != FormatLegacy {
		t.Fatalf("Detect(headers-only) = %v/%v, want legacy", f, err)
	}
}

// TestFramedTruncatedRecordIsReported pins Rule 15 at the framing level: a record
// whose header promises more bytes than the file holds is an ERROR, never a short
// payload presented as whole.
func TestFramedTruncatedRecordIsReported(t *testing.T) {
	enc := Encode(TagRequest, []byte("0123456789"))
	dir := t.TempDir()
	path := filepath.Join(dir, "trunc.raw.log")
	if err := os.WriteFile(path, enc[:len(enc)-4], 0o644); err != nil {
		t.Fatalf("write truncated: %v", err)
	}
	r, err := OpenReader(path, FormatFramed)
	if err != nil {
		t.Fatalf("OpenReader: %v", err)
	}
	rec, err := r.Next()
	if !errors.Is(err, ErrMalformedRecord) {
		t.Fatalf("Next() on a truncated record = %v, want ErrMalformedRecord", err)
	}
	if rec.Terminated {
		t.Fatalf("truncated record reported as terminated")
	}
}

// TestNextHeaderSkipWalksWithoutReadingPayloads pins that a walker can measure a
// framed log cheaply and still decode the record it wants.
func TestNextHeaderSkipWalksWithoutReadingPayloads(t *testing.T) {
	var log []byte
	log = append(log, Encode(TagRequest, []byte(`{"n":1}`))...)
	log = append(log, Encode(TagChunk, []byte("data: 1\n"))...)
	log = append(log, Encode(TagChunk, []byte("data: 2\n"))...)
	log = append(log, Encode(TagResponse, []byte(`{"done":true}`))...)
	r := NewReaderFrom(bytes.NewReader(log), FormatFramed)
	var tags []string
	for {
		h, err := r.NextHeader()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("NextHeader: %v", err)
		}
		tags = append(tags, h.Tag)
		if h.Tag == TagResponse {
			rec, err := r.Decode()
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if string(rec.Payload) != `{"done":true}` {
				t.Fatalf("Decode returned %q", rec.Payload)
			}
			continue
		}
		if err := r.Skip(); err != nil {
			t.Fatalf("Skip: %v", err)
		}
	}
	if got, want := strings.Join(tags, ","), "R,C,C,S"; got != want {
		t.Fatalf("walked tags = %s, want %s", got, want)
	}
}

// TestProbeTagAgreesAcrossFramings pins that one event reports one string from
// either framing.
func TestProbeTagAgreesAcrossFramings(t *testing.T) {
	if got := ProbeTag(TagRequest, ProbeStreamAnnotation); got != TagProbeRequest {
		t.Fatalf("ProbeTag(request, %q) = %q, want %q", ProbeStreamAnnotation, got, TagProbeRequest)
	}
	if got := ProbeTag(TagRequest, ""); got != TagRequest {
		t.Fatalf("ProbeTag(request, \"\") = %q, want %q", got, TagRequest)
	}
	if got := ProbeTag(TagChunk, "anything"); got != TagChunk {
		t.Fatalf("ProbeTag(chunk, …) = %q, want %q", got, TagChunk)
	}
}
