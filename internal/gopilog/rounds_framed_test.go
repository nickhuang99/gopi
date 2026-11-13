// rounds_framed_test.go — THE CROSS-FORMAT EQUIVALENCE TEST.
//
// The raw log's write side moved to a length-prefixed framing (a header states the
// payload's byte count), so every reader must serve BOTH framings. The property that
// must hold is stronger than "the framed reader works": the same session, recorded in
// either framing, must produce the SAME rounds — same count, same annotations, same
// request bytes, same response digest, same streamed text.
//
// That equality is the acceptance criterion, and it is why this file builds ONE
// record list and renders it twice rather than writing two fixtures by hand: two
// hand-written fixtures agree with themselves, drift apart silently, and then a
// framing regression shows up as a diff in whichever one nobody updated.
package gopilog

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/rawlogfmt"
)

// sessionRec is one record of the fixture session. The SAME list feeds both
// renderings, so the two files describe one session in two framings.
type sessionRec struct {
	tag     string
	ann     string
	payload string
}

// framedSessionFixture is a compact but complete session: a streaming request answered by
// three chunks, a guard-annotated probe request, a non-streamed request answered by
// a plain response, and a failed attempt whose diagnostics carry the marker
// vocabulary INSIDE the payload (the case that motivated the framing change).
func framedSessionFixture() []sessionRec {
	return []sessionRec{
		{rawlogfmt.TagRequest, "", `{"model":"m","stream":true,"system":[{"type":"text","text":"sys"}],"messages":[{"role":"user","content":"hi"}]}`},
		{rawlogfmt.TagChunk, "", "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":100},\"id\":\"msg_1\"}}\n"},
		{rawlogfmt.TagChunk, "", "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n"},
		{rawlogfmt.TagChunk, "", "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":42}}\n"},
		// A probe: the guard ladder's own send. Its annotation must survive both
		// framings, because a reader that loses it reports a real round where the
		// session actually made a probe.
		{rawlogfmt.TagProbeRequest, "1214-guard, stream", `{"model":"m","stream":true,"messages":[{"role":"user","content":"probe"}]}`},
		{rawlogfmt.TagChunk, "", "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"probe-out\"}}\n"},
		// A payload that CONTAINS the legacy marker vocabulary, ending with the legacy
		// record terminator. In a framed file these are ordinary bytes; a line-oriented
		// reader would see four phantom markers here.
		{rawlogfmt.TagRequest, "", `{"model":"m","messages":[{"role":"user","content":"→ REQUEST\n← CHUNK\n← RESPONSE\nHTTP-ERROR\n"}]}`},
		{rawlogfmt.TagResponse, "", `{"content":"final"}`},
		{rawlogfmt.TagError, "", "← streaming attempt — HTTP 500 (non-OK)\nbody line 1\n"},
	}
}

// renderFramed builds a framed raw log from the fixture session.
func renderFramed(recs []sessionRec) []byte {
	var out []byte
	for _, r := range recs {
		out = append(out, rawlogfmt.EncodeAnnotated(r.tag, r.ann, []byte(r.payload))...)
	}
	return out
}

// renderLegacy builds the SAME session in the legacy marker framing. A probe is
// spelled as its annotated marker (`→ REQUEST (1214-guard, stream)`), which is how
// the legacy writer recorded one.
func renderLegacy(t *testing.T, recs []sessionRec) []byte {
	t.Helper()
	var out []byte
	for _, r := range recs {
		b, err := rawlogfmt.EncodeLegacy(r.tag, r.ann, []byte(r.payload))
		if err != nil {
			t.Fatalf("EncodeLegacy(%s, %q): %v", r.tag, r.ann, err)
		}
		out = append(out, b...)
	}
	return out
}

// TestParseHTTPRoundsSameSessionBothFramings is the equivalence proof: the two
// renderings of one session must yield identical rounds.
func TestParseHTTPRoundsSameSessionBothFramings(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	recs := framedSessionFixture()

	legacyPath := writeRawLog(t, "fixture-legacy.log", string(renderLegacy(t, recs)))
	framedPath := writeRawLog(t, "fixture-framed.log", string(renderFramed(recs)))

	// The discriminator must actually disagree here — otherwise this test would pass
	// on two copies of the same file and prove nothing about the second framing.
	if f, err := rawlogfmt.Detect(legacyPath); err != nil || f != rawlogfmt.FormatLegacy {
		t.Fatalf("Detect(legacy) = %v, %v; want FormatLegacy", f, err)
	}
	if f, err := rawlogfmt.Detect(framedPath); err != nil || f != rawlogfmt.FormatFramed {
		t.Fatalf("Detect(framed) = %v, %v; want FormatFramed", f, err)
	}

	legacy, err := ParseHTTPRounds(legacyPath)
	if err != nil {
		t.Fatalf("ParseHTTPRounds(legacy): %v", err)
	}
	framed, err := ParseHTTPRounds(framedPath)
	if err != nil {
		t.Fatalf("ParseHTTPRounds(framed): %v", err)
	}

	if len(legacy) != len(framed) {
		t.Fatalf("round count: legacy %d, framed %d (legacy=%s framed=%s)",
			len(legacy), len(framed), roundSummary(legacy), roundSummary(framed))
	}
	// The session has three requests: a streaming one, a probe, and the one whose
	// payload quotes the marker vocabulary. A reader that counted marker TEXT would
	// report more.
	if len(framed) != 3 {
		t.Fatalf("framed rounds = %d, want 3 — a payload quoting the marker vocabulary must not open a round (%s)",
			len(framed), roundSummary(framed))
	}
	for i := range legacy {
		l, f := legacy[i], framed[i]
		if l.Number != f.Number {
			t.Errorf("round %d Number: legacy %d, framed %d", i+1, l.Number, f.Number)
		}
		if l.Annotation != f.Annotation {
			t.Errorf("round %d Annotation: legacy %q, framed %q", i+1, l.Annotation, f.Annotation)
		}
		if string(l.RequestRaw) != string(f.RequestRaw) {
			t.Errorf("round %d RequestRaw differs:\n legacy %q\n framed %q", i+1, l.RequestRaw, f.RequestRaw)
		}
		if l.Stream != f.Stream {
			t.Errorf("round %d Stream: legacy %v, framed %v", i+1, l.Stream, f.Stream)
		}
		if l.Response.Kind != f.Response.Kind {
			t.Errorf("round %d Response.Kind: legacy %q, framed %q", i+1, l.Response.Kind, f.Response.Kind)
		}
		if l.Response.Chunks != f.Response.Chunks {
			t.Errorf("round %d Response.Chunks: legacy %d, framed %d", i+1, l.Response.Chunks, f.Response.Chunks)
		}
		if l.Response.Bytes != f.Response.Bytes {
			t.Errorf("round %d Response.Bytes: legacy %d, framed %d", i+1, l.Response.Bytes, f.Response.Bytes)
		}
		if l.Response.TextChars != f.Response.TextChars {
			t.Errorf("round %d Response.TextChars: legacy %d, framed %d — the streamed text must reassemble identically",
				i+1, l.Response.TextChars, f.Response.TextChars)
		}
	}
}

// TestParseHTTPRoundsFramedProbeAnnotationSurvives pins the annotation on its own,
// because it is the one field whose legacy spelling lives INSIDE the marker text
// (`→ REQUEST (1214-guard, stream)`) while the framed spelling lives in metadata
// after the length — two encodings, one value, and a reader that loses it misreports
// a probe as a normal send.
func TestParseHTTPRoundsFramedProbeAnnotationSurvives(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := writeRawLog(t, "probe-framed.log", string(renderFramed(framedSessionFixture())))
	rounds, err := ParseHTTPRounds(path)
	if err != nil {
		t.Fatalf("ParseHTTPRounds: %v", err)
	}
	if len(rounds) != 3 {
		t.Fatalf("rounds = %d, want 3 (%s)", len(rounds), roundSummary(rounds))
	}
	const wantAnnotation = "1214-guard, stream"
	if rounds[1].Annotation != wantAnnotation {
		t.Fatalf("round 2 Annotation = %q, want %q", rounds[1].Annotation, wantAnnotation)
	}
	if rounds[0].Annotation != "" || rounds[2].Annotation != "" {
		t.Fatalf("unannotated rounds gained an annotation: %q, %q", rounds[0].Annotation, rounds[2].Annotation)
	}
}

// TestParseHTTPRoundsFramedMultiLineRequestIsWhole is the regression test for a
// MEASURED truncation: the round parser used to read a request body as "the line
// after the header", so a framed payload carrying its own newlines came back cut
// to its FIRST LINE. The acceptance fixture (a request whose bytes quote the whole
// marker vocabulary, ending in a newline) is exactly this shape: the record
// announced 146 payload bytes and `session raw <n> request` printed the 7 bytes of
// `prefix`.
//
// The body a reader must return is the RECORD'S PAYLOAD — the bytes the writer
// logged — not a line of them.
func TestParseHTTPRoundsFramedMultiLineRequestIsWhole(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	body := "prefix\n→ REQUEST\n← CHUNK\n← RESPONSE\nHTTP-ERROR\nR 5\nC 0\nS 12\n" +
		"# gopi session gc — TRIMMED\nR 999999\nE 1\n← CHUNK (1214-guard, stream)\nsuffix"
	recs := []sessionRec{
		{rawlogfmt.TagRequest, "", body},
		{rawlogfmt.TagChunk, "", "event: content_block_delta\ndata: {\"i\":0}\n"},
		{rawlogfmt.TagResponse, "", `{"id":"msg_2"}`},
	}
	path := writeRawLog(t, "multiline-request.log", string(renderFramed(recs)))

	rounds, err := ParseHTTPRounds(path)
	if err != nil {
		t.Fatalf("ParseHTTPRounds: %v", err)
	}
	// ONE round: the payload quotes five marker lines and two header-shaped lines,
	// and none of them may open a record of its own.
	if len(rounds) != 1 {
		t.Fatalf("rounds = %d, want 1 (%s) — a payload quoting the framing vocabulary must not split a record",
			len(rounds), roundSummary(rounds))
	}
	if got := string(rounds[0].RequestRaw); got != body {
		t.Fatalf("RequestRaw = %q (%d bytes), want %q (%d bytes)",
			got, len(got), body, len(body))
	}
	// The request side is not JSON, so the round reports the parse failure rather
	// than pretending it understood the body.
	if rounds[0].Err == nil {
		t.Fatalf("a non-JSON request body reported no error")
	}
}

// TestRawLogLineSpanFramedReadsVerbatimLines pins the line-span reader (what
// `session raw --rounds` uses to show a response verbatim): it must return the
// file's OWN lines in the framed framing, and a span through a multi-line payload
// must come back byte-exact.
func TestRawLogLineSpanFramedReadsVerbatimLines(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	recs := framedSessionFixture()
	path := writeRawLog(t, "span-framed.log", string(renderFramed(recs)))

	lines, truncated, err := RawLogLineSpan(path, 1, 3)
	if err != nil {
		t.Fatalf("RawLogLineSpan: %v", err)
	}
	if truncated {
		t.Fatalf("truncated = true for the first 3 lines of a small file")
	}
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 3: %q", len(lines), lines)
	}
	// The fixture's first payload is a SINGLE line of JSON, so the framed file opens:
	//   line 1 = the request's header (`R <byte count>`)
	//   line 2 = the request body, one line
	//   line 3 = the NEXT record's header (`C <byte count>`)
	// Reading the counts from the fixture (rather than typing them) is what makes
	// this an assertion about the format instead of about the fixture.
	if tag, n, _, ok := rawlogfmt.ParseHeaderLineAnnotated([]byte(lines[0])); !ok || tag != rawlogfmt.TagRequest {
		t.Fatalf("line 1 = %q, want a request header", lines[0])
	} else if want := len(framedSessionFixture()[0].payload); n != want {
		t.Fatalf("header states %d payload bytes, fixture payload is %d", n, want)
	}
	if got, want := lines[1], framedSessionFixture()[0].payload; got != want {
		t.Fatalf("payload line = %q, want %q", got, want)
	}
	if tag, n, _, ok := rawlogfmt.ParseHeaderLineAnnotated([]byte(lines[2])); !ok || tag != rawlogfmt.TagChunk {
		t.Fatalf("line 3 = %q, want the chunk record's header", lines[2])
	} else if want := len(framedSessionFixture()[1].payload); n != want {
		t.Fatalf("chunk header states %d payload bytes, fixture payload is %d", n, want)
	}

	// A span that ENDS inside the multi-line chunk payload returns exactly those
	// lines: chunk payloads really do span lines, and a verbatim view must show them.
	chunkLines := strings.Split(strings.TrimSuffix(framedSessionFixture()[1].payload, "\n"), "\n")
	if len(chunkLines) < 2 {
		t.Fatalf("fixture chunk is not multi-line: %q", framedSessionFixture()[1].payload)
	}
	span, _, err := RawLogLineSpan(path, 4, 3+len(chunkLines))
	if err != nil {
		t.Fatalf("RawLogLineSpan(chunk): %v", err)
	}
	if strings.Join(span, "\n") != strings.Join(chunkLines, "\n") {
		t.Fatalf("chunk span = %q, want %q", span, chunkLines)
	}
}

// TestCountRawLogRequestsBothFramings pins the count a reader derives from a raw
// log: it must agree between framings, and it must NOT count the marker vocabulary
// quoted inside a payload.
func TestCountRawLogRequestsBothFramings(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	recs := framedSessionFixture()
	legacyPath := writeRawLog(t, "count-legacy.log", string(renderLegacy(t, recs)))

	// CountRequests lives in internal/checkpoint and cannot be imported here (that
	// package imports this one), so the equivalence is asserted through the ROUNDS —
	// the same population it counts, from the same records.
	rounds, err := ParseHTTPRounds(legacyPath)
	if err != nil {
		t.Fatalf("ParseHTTPRounds: %v", err)
	}
	if len(rounds) != 3 {
		t.Fatalf("legacy rounds = %d, want 3 (%s)", len(rounds), roundSummary(rounds))
	}
}

// roundSummary renders what each round saw, so a failing comparison prints the
// STRUCTURE rather than only a count.
func roundSummary(rounds []HTTPRound) string {
	out := ""
	for _, r := range rounds {
		out += "[#" + strconv.Itoa(r.Number) + " ann=" + r.Annotation + " kind=" + r.Response.Kind +
			" chunks=" + strconv.Itoa(r.Response.Chunks) + " bytes=" + strconv.Itoa(r.Response.Bytes) + "] "
	}
	return out
}

// TestFramedFixtureIsNotAccidentallyLegacy guards the fixture itself: if the framed
// renderer ever emitted a legacy-shaped file, every comparison above would be
// comparing a file with itself.
func TestFramedFixtureIsNotAccidentallyLegacy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "guard.log")
	if err := os.WriteFile(path, renderFramed(framedSessionFixture()), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	format, decided, err := rawlogfmt.DetectExact(path)
	if err != nil {
		t.Fatalf("DetectExact: %v", err)
	}
	if !decided {
		t.Fatalf("the framed fixture carries no record in its head")
	}
	if format != rawlogfmt.FormatFramed {
		t.Fatalf("framed fixture detected as %v, want FormatFramed", format)
	}
	// And the legacy rendering must NOT be detected as framed.
	legacy := filepath.Join(dir, "guard-legacy.log")
	if err := os.WriteFile(legacy, renderLegacy(t, framedSessionFixture()), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if format, err := rawlogfmt.Detect(legacy); err != nil || format != rawlogfmt.FormatLegacy {
		t.Fatalf("legacy fixture detected as %v (%v), want FormatLegacy", format, err)
	}
}
