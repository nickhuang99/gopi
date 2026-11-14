// The round model meets the STREAMED complete-response record
// (rawlogfmt.StreamResponseAnnotation, written by internal/deepseek when a
// streamed call ends — bug 01a19bf1-ff52).
//
// The two views answer different questions and must not be conflated:
//
//	the WIRE view  — what the server sent: a streamed round is R + C…C
//	the PAIRING view — what the call RETURNED, in the shape a non-streaming
//	                   response has, so one reader pairs request↔response
//
// A synthesized S record serves the second and must not rewrite the first. These
// tests pin that boundary, because getting it wrong is silent: the round still
// parses, it just claims to be something it was not.
package gopilog

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nickhuang99/gopi/internal/rawlogfmt"
)

// streamedRoundLog writes a one-request streamed round: a request asking for a
// stream, one SSE chunk, and (when derived is non-empty) a synthesized complete
// response carrying derived as its annotation.
func streamedRoundLog(t *testing.T, derived string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "streamed.raw.log")
	chunk := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_a\",\"usage\":{\"input_tokens\":5}}}\n"
	body := `{"id":"msg_a","content":[],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":3}}`
	var buf []byte
	buf = append(buf, rawlogfmt.Encode(rawlogfmt.TagRequest, []byte(`{"model":"m","stream":true,"messages":[]}`))...)
	buf = append(buf, rawlogfmt.Encode(rawlogfmt.TagChunk, []byte(chunk))...)
	if derived != "" {
		buf = append(buf, rawlogfmt.EncodeAnnotated(rawlogfmt.TagResponse, derived, []byte(body))...)
	}
	if err := os.WriteFile(path, buf, 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

// TestStreamedRoundKeepsItsWireShapeWithADerivedResponseRecord asserts the
// round model is UNCHANGED by the derived record: same Kind, same chunk count,
// same byte total. Without the dispatch arm that recognises the annotation, the
// synthesized record silently becomes the round's response — Kind flips
// "stream"→"response", the chunk count a reader uses to see the round was
// streamed disappears, and the bytes are charged twice (chunks + body).
func TestStreamedRoundKeepsItsWireShapeWithADerivedResponseRecord(t *testing.T) {
	wireOnly, err := ParseHTTPRounds(streamedRoundLog(t, ""))
	if err != nil {
		t.Fatalf("parse wire-only log: %v", err)
	}
	withDerived, err := ParseHTTPRounds(streamedRoundLog(t, rawlogfmt.StreamResponseAnnotation))
	if err != nil {
		t.Fatalf("parse log with a derived response record: %v", err)
	}
	if len(wireOnly) != 1 || len(withDerived) != 1 {
		t.Fatalf("expected 1 round each, got %d (wire-only) / %d (with derived)", len(wireOnly), len(withDerived))
	}
	a, b := wireOnly[0], withDerived[0]
	if b.Response.Kind != a.Response.Kind {
		t.Errorf("round kind changed by the derived record: %q with it, %q without it (the round model describes the WIRE; a derived body must not rewrite it)", b.Response.Kind, a.Response.Kind)
	}
	if b.Response.Kind != "stream" {
		t.Errorf("streamed round kind = %q, want %q", b.Response.Kind, "stream")
	}
	if b.Response.Chunks != a.Response.Chunks || b.Response.Chunks == 0 {
		t.Errorf("chunk count = %d with the derived record, %d without (both must count the WIRE chunks)", b.Response.Chunks, a.Response.Chunks)
	}
	if b.Response.Bytes != a.Response.Bytes {
		t.Errorf("response bytes = %d with the derived record, %d without — the derived body's bytes are already carried by the chunks it was folded from", b.Response.Bytes, a.Response.Bytes)
	}
}

// TestDerivedResponseRecordIsStillOneResponsePerRequest asserts the OTHER half:
// the record is recognised as a response when a reader is PAIRING (the raw
// renderer's per-round kind, the `--raw` index row), even though the round model
// keeps describing the wire. A reader that lost sight of it entirely would be
// back to the defect this record exists to fix.
func TestDerivedResponseRecordIsStillOneResponsePerRequest(t *testing.T) {
	records, err := readAllRecords(streamedRoundLog(t, rawlogfmt.StreamResponseAnnotation))
	if err != nil {
		t.Fatalf("read records: %v", err)
	}
	var responses int
	var annotation string
	for _, r := range records {
		if r.Tag == rawlogfmt.TagResponse {
			responses++
			annotation = r.Annotation
		}
	}
	if responses != 1 {
		t.Fatalf("response records = %d, want exactly 1 (one request, one response)", responses)
	}
	if !isDerivedStreamResponse(annotation) {
		t.Errorf("annotation %q is not recognised as a derived stream response — the pairing reader could not tell it from a captured body", annotation)
	}
	if got := rawlogfmt.StreamResponseAnnotation + rawlogfmt.StreamResponsePartialSuffix; !isDerivedStreamResponse(got) {
		t.Errorf("the partial spelling %q is not recognised either — a truncated stream's record must still pair", got)
	}
	if isDerivedStreamResponse(rawlogfmt.ProbeAnnotation) {
		t.Errorf("%q was mistaken for a derived stream response", rawlogfmt.ProbeAnnotation)
	}
}

// readAllRecords decodes a framed log through the package's own scanner, so this
// test reads what a real reader reads.
func readAllRecords(path string) ([]rawlogfmt.Record, error) {
	f, format, err := openRawLogForParse(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := rawlogfmt.NewLineScannerFrom(f, format)
	var out []rawlogfmt.Record
	for sc.Scan() {
		if sc.IsRecordHeader() {
			rec := sc.Record()
			out = append(out, rawlogfmt.Record{Tag: rec.Tag, Annotation: rec.Annotation, Payload: append([]byte(nil), rec.Payload...)})
		}
	}
	if err := sc.Err(); err != nil {
		return out, err
	}
	return out, nil
}
