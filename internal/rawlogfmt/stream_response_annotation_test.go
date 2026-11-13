// The vocabulary for a SYNTHESIZED streamed complete response
// (StreamResponseAnnotation / StreamResponsePartialSuffix, written by
// internal/deepseek when a streamed call ends — bug 01a19bf1-ff52).
//
// The annotation is METADATA, and these tests pin the property the design rests
// on: it survives BOTH framings without touching a payload byte, so a reader can
// tell a derived body from a captured one in a new log and in a continued legacy
// one alike.
package rawlogfmt

import (
	"bytes"
	"testing"
)

// TestStreamResponseAnnotationRoundTripsInBothFramings asserts the annotation is
// carried by the header in each framing and comes back on decode — and that the
// payload is byte-identical to what the writer handed in (the encoder never
// edits a payload; that is the format's whole promise).
func TestStreamResponseAnnotationRoundTripsInBothFramings(t *testing.T) {
	payload := []byte(`{"id":"msg_a","content":[],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":3}}`)
	for _, ann := range []string{
		StreamResponseAnnotation,
		StreamResponseAnnotation + StreamResponsePartialSuffix,
	} {
		t.Run(ann, func(t *testing.T) {
			// FRAMED: the annotation is the header's third field.
			framed := EncodeAnnotated(TagResponse, ann, payload)
			tag, length, gotAnn, ok := ParseHeaderLineAnnotated(framed[:bytes.IndexByte(framed, RecordTerminator)])
			if !ok {
				t.Fatalf("framed header did not parse: %q", framed)
			}
			if tag != TagResponse || length != len(payload) || gotAnn != ann {
				t.Errorf("framed header = (%q,%d,%q), want (%q,%d,%q)", tag, length, gotAnn, TagResponse, len(payload), ann)
			}
			if !bytes.Contains(framed, payload) {
				t.Errorf("payload bytes are not verbatim in the framed record: %q", framed)
			}
			if len(ann) > maxAnnotationLen {
				t.Errorf("annotation %q exceeds maxAnnotationLen %d — the header bound would refuse it", ann, maxAnnotationLen)
			}

			// LEGACY: the annotation is the marker's parenthesised suffix, and a
			// continued old log must accept it too (a run appending to a legacy
			// file writes what that file's vocabulary can spell).
			legacy, err := EncodeLegacy(TagResponse, ann, payload)
			if err != nil {
				t.Fatalf("legacy framing refused the derived stream annotation %q: %v", ann, err)
			}
			line := string(legacy[:bytes.IndexByte(legacy, RecordTerminator)])
			if got, ok := LegacyTag(line); !ok || got != TagResponse {
				t.Errorf("LegacyTag(%q) = %q,%v, want %q,true", line, got, ok, TagResponse)
			}
			if got := LegacyAnnotation(line, MarkerResponse); got != ann {
				t.Errorf("LegacyAnnotation(%q) = %q, want %q", line, got, ann)
			}
			if !bytes.Contains(legacy, payload) {
				t.Errorf("payload bytes are not verbatim in the legacy record: %q", legacy)
			}
		})
	}
}

// TestPartialSuffixIsASuffixNotAWord asserts the shape a reader relies on: the
// partial spelling EXTENDS the complete one, so a prefix test accepts both and an
// exact test accepts only the complete one. A spelling that swapped the order
// would silently make every truncated stream look whole to a prefix reader.
func TestPartialSuffixIsASuffixNotAWord(t *testing.T) {
	partial := StreamResponseAnnotation + StreamResponsePartialSuffix
	if !bytes.HasPrefix([]byte(partial), []byte(StreamResponseAnnotation)) {
		t.Fatalf("%q does not begin with %q — a prefix reader would stop recognising truncated streams",
			partial, StreamResponseAnnotation)
	}
	if partial == StreamResponseAnnotation {
		t.Fatal("the partial spelling is identical to the complete one, so a reader cannot tell a broken stream from a whole body")
	}
	// It must not collide with the PROBE vocabulary: a probe response is a real
	// body the guard ladder read, not a synthesized one.
	if StreamResponseAnnotation == ProbeAnnotation || bytes.HasPrefix([]byte(ProbeStreamAnnotation), []byte(StreamResponseAnnotation)) {
		t.Errorf("the derived-stream annotation %q collides with the probe vocabulary (%q / %q)",
			StreamResponseAnnotation, ProbeAnnotation, ProbeStreamAnnotation)
	}
}
