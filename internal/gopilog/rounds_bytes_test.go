// rounds_bytes_test.go — how many bytes a round's response side reports.
//
// This file exists because the number was WRONG in a way no test could see: the
// response arm counted the record's payload, and then the arm that consumes that
// same payload's LINES counted it again. Both arms were added in the same change
// (the record's payload is the right thing to read; the line accumulator was left
// running), so every response body was reported at roughly DOUBLE its size while the
// suite stayed green — the framed/legacy equivalence test compares the two framings
// to EACH OTHER, and both were wrong by the same rule.
//
// MEASURED on a real 1.79 GB legacy session log, 2026-11-02: round 102's response
// body is 3356 bytes and `session raw --rounds` reported the round as 9.7 KB where
// the baseline binary reported 6.4 KB — the difference being exactly (len(payload)+1)
// counted on top of the per-line total.
package gopilog

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/rawlogfmt"
)

// TestResponseBytesAreCountedOnce pins the arithmetic against the payload the test
// itself planted, so the expected number is derived from the body rather than copied
// from previous output.
func TestResponseBytesAreCountedOnce(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const body = `{"id":"msg_1","content":[{"type":"text","text":"hello"}]}`
	recs := []sessionRec{
		{rawlogfmt.TagRequest, "", `{"model":"m","max_tokens":16}`},
		{rawlogfmt.TagResponse, "", body},
	}
	path := writeRawLog(t, "bytes-once.log", string(renderFramed(recs)))

	rounds, err := ParseHTTPRounds(path)
	if err != nil {
		t.Fatalf("ParseHTTPRounds: %v", err)
	}
	if len(rounds) != 1 {
		t.Fatalf("rounds = %d, want 1 (%s)", len(rounds), roundSummary(rounds))
	}
	// The response side is the body's bytes plus the one line terminator the counter
	// has always charged for a line-oriented body (see the response arm's comment).
	want := len(body) + 1
	if got := rounds[0].Response.Bytes; got != want {
		t.Fatalf("Response.Bytes = %d, want %d (body %d bytes + its line terminator) — "+
			"a double count reports %d", got, want, len(body), len(body)+1+len(body)+1)
	}
}

// TestResponseBytesCountAMultiLineBodyOnce extends the same claim to a body that
// spans several lines, which is the shape the framed framing makes ordinary: every
// line must be charged once, and the record's own payload must not ALSO be charged
// on top of them.
func TestResponseBytesCountAMultiLineBodyOnce(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const body = "line one\nline two\nline three"
	recs := []sessionRec{
		{rawlogfmt.TagRequest, "", `{"model":"m"}`},
		{rawlogfmt.TagResponse, "", body},
	}
	path := writeRawLog(t, "bytes-multiline.log", string(renderFramed(recs)))

	rounds, err := ParseHTTPRounds(path)
	if err != nil {
		t.Fatalf("ParseHTTPRounds: %v", err)
	}
	if len(rounds) != 1 {
		t.Fatalf("rounds = %d, want 1 (%s)", len(rounds), roundSummary(rounds))
	}
	// Three lines, each charged len(line)+1.
	want := 0
	for _, line := range strings.Split(body, "\n") {
		want += len(line) + 1
	}
	if got := rounds[0].Response.Bytes; got != want {
		t.Fatalf("Response.Bytes = %d, want %d (%d lines charged once each)", got, want, len(strings.Split(body, "\n")))
	}
}
