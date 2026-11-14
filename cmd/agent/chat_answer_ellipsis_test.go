package main

// The RED/GREEN pair for the render-time ellipsis backstop
// (chat_answer_ellipsis.go). RED is the case the epic is about: a reply body
// that carries U+2026 reaches the renderer, and before this backstop nothing in
// the render path noticed it — the prompt had forbidden it (43 mountings in the
// measured session) and the character was drawn anyway. GREEN is the other half,
// and it is not decoration: a detector that fires on a clean body is worse than
// none, because the report is the only signal there is.
//
// Two properties are asserted together with the report, because both are the
// epic's contract and either alone would pass a broken implementation:
//
//   - the report is LOCATABLE (turn + rune offset + excerpt), and
//   - the bytes drawn are the bytes the model sent, unchanged. A backstop that
//     substituted something for the ellipsis would pass a "did we find it?" test
//     while destroying the artifact the operator pastes out of the grid.

import (
	"fmt"
	"strings"
	"testing"
)

// ellipsisHarness writes an answer body through the REAL turn renderer (the one
// the turn loop builds) and returns what the operator would have on screen plus
// the notice channel the report rides.
type ellipsisHarness struct {
	out     strings.Builder // the drawn answer (cooked path: the flow writer)
	notices strings.Builder // the notice channel (input.WriteNotice)
	in      *chatInput      // the REPL's input, the seam chatReportAnswerEllipses takes
	r       *chatTurnRenderer
}

func newEllipsisHarness(t *testing.T, turn int) *ellipsisHarness {
	t.Helper()
	h := &ellipsisHarness{}
	in := &chatInput{flowOut: &h.out, errOut: &h.notices}
	h.in = in
	h.r = newChatTurnRenderer(in)
	h.r.BeginTurn(turn, "the operator's line")
	return h
}

// write streams the body in the cadence the measured run used: tiny fragments,
// so the detector is exercised on the shape a token stream actually has.
func (h *ellipsisHarness) write(parts ...string) {
	for _, p := range parts {
		h.r.Write(p)
	}
}

// TestChatAnswerEllipsisIsReported_RED is the RED half: a body carrying `…` is
// REPORTED, with the turn, the rune offset and an excerpt — and the drawn bytes
// are untouched.
func TestChatAnswerEllipsisIsReported_RED(t *testing.T) {
	// The measured body (session 8616b46c's raw.log, verbatim shape): the model's
	// own U+2026 lands right after a path it had begun to write.
	const (
		head  = "the file is `COMMANDS."
		elide = "…"
		tail  = "\n\nmd` then the rest"
	)
	wantOffset := 0
	for range []rune(head) {
		wantOffset++
	}

	h := newEllipsisHarness(t, 7)
	// Split INSIDE the ellipsis as well: the character is three bytes, and a
	// detector that decoded each fragment alone would miss a stream that split it.
	h.write(head, elide[:1], elide[1:], tail)
	h.r.Finish()

	drawn := h.out.String()
	wantDrawn := head + elide + tail
	if drawn != wantDrawn {
		t.Fatalf("the renderer altered the answer body:\n got %q\nwant %q\n"+
			"(the backstop must REPORT an ellipsis, never substitute for it)", drawn, wantDrawn)
	}

	hits := h.r.Ellipses()
	if len(hits) != 1 {
		t.Fatalf("a body carrying one U+2026 produced %d hits, want 1: %+v", len(hits), hits)
	}
	if hits[0].Turn != 7 {
		t.Fatalf("the hit names turn %d, want 7 — a report without the turn cannot be "+
			"traced back to the body it came from", hits[0].Turn)
	}
	if hits[0].Offset != wantOffset {
		t.Fatalf("the hit names rune offset %d, want %d (the rune offset of U+2026 in "+
			"the turn's body)", hits[0].Offset, wantOffset)
	}
	if !strings.Contains(hits[0].Excerpt, elide) {
		t.Fatalf("the excerpt does not quote the character: %q", hits[0].Excerpt)
	}
	if !strings.Contains(hits[0].Excerpt, "COMMANDS.") {
		t.Fatalf("the excerpt does not quote the text around the hit, so a reader cannot "+
			"find it: %q", hits[0].Excerpt)
	}
	if n := strings.Count(hits[0].Excerpt, "\n"); n != 0 {
		t.Fatalf("the excerpt spans %d newlines; it must stay ONE line so the report is "+
			"one row on screen: %q", n, hits[0].Excerpt)
	}

	// THE REPORT LINE: what the operator (and the session's console.log) sees.
	var report strings.Builder
	if n := reportChatAnswerEllipses(&report, hits, 0, 7); n != 1 {
		t.Fatalf("the report wrote %d lines for one hit, want 1: %q", n, report.String())
	}
	for _, want := range []string{
		chatAnswerEllipsisReportPrefix,
		"turn 7",
		fmt.Sprintf("rune offset %d", wantOffset),
		"NOT altered",
	} {
		if !strings.Contains(report.String(), want) {
			t.Fatalf("the report line does not carry %q:\n%s", want, report.String())
		}
	}
}

// TestChatAnswerEllipsisIsQuietOnACleanBody_GREEN is the GREEN half: an
// ellipsis-free body — including the ASCII spelling the model legitimately
// uses — is NOT reported, and the report writes nothing at all.
func TestChatAnswerEllipsisIsQuietOnACleanBody_GREEN(t *testing.T) {
	h := newEllipsisHarness(t, 3)
	// `./...` is the ASCII spelling and it is NOT the defect: it is an ordinary
	// token (`go test ./...`), and a report on it would be the noise that gets a
	// real notice skimmed past. Only U+2026 is reported.
	h.write("run `go test ./...` and read the output", "\n", "then report every line")
	h.r.Finish()

	if hits := h.r.Ellipses(); len(hits) != 0 {
		t.Fatalf("a clean body produced %d reports: %+v", len(hits), hits)
	}
	var report strings.Builder
	if n := reportChatAnswerEllipses(&report, nil, 0, 0); n != 0 {
		t.Fatalf("an empty hit list wrote %d lines: %q", n, report.String())
	}
	// And the REPL seam writes nothing either — the operator's channel stays
	// silent on a clean turn.
	if n := chatReportAnswerEllipses(nil, h.r); n != 0 {
		t.Fatalf("the REPL seam wrote %d lines for a clean body", n)
	}
	if got := h.notices.String(); got != "" {
		t.Fatalf("a clean turn announced something on the notice channel: %q", got)
	}
}

// TestChatAnswerEllipsisReportAggregatesOneTurn pins two halves of the same
// contract, and they pull in opposite directions on purpose.
//
// HALF ONE (this epic, 2026-11-09): a turn whose ellipses are ALL prose prints
// NOTHING. The detector still records every one of them — the budget is on the
// REPORT, never on the detection — but the operator's band carries no line,
// because a rhetorical ellipsis cost him nothing to read past and a line per turn
// is the noise that gets a real notice skimmed past. This is the file header's
// "LOW budget = 0 lines" finally matching the code: before this, the summary line
// alone still printed, so LOW got a line after all.
//
// HALF TWO (the rule the first half must not break): once a turn DOES speak, it
// speaks about the WHOLE turn — every count it did not print as its own line is
// still stated as a number (Rule 15: a report may compress, it may not lose).
func TestChatAnswerEllipsisReportAggregatesOneTurn(t *testing.T) {
	const hits_ = 11
	h := newEllipsisHarness(t, 1)
	for i := 0; i < hits_; i++ {
		// A rhetorical ellipsis with a separator after it: prose, the LOW class,
		// which is exactly the class that must never get a line — not even the
		// summary.
		h.write("wait … ", "then continue. ")
	}
	h.r.Finish()

	hits := h.r.Ellipses()
	if len(hits) != hits_ {
		t.Fatalf("the detector recorded %d hits, want %d — the SILENCE is a budget on the "+
			"REPORT, never on the detection", len(hits), hits_)
	}
	if sev := hits[0].Severity; sev != chatAnswerEllipsisSeverityOrdinary {
		t.Fatalf("a standalone prose ellipsis was classified %q, want %q — a rhetorical "+
			"ellipsis cut no pasted unit and must not be escalated",
			sev, chatAnswerEllipsisSeverityOrdinary)
	}
	for _, hit := range hits {
		if hit.Severity != chatAnswerEllipsisSeverityOrdinary {
			t.Fatalf("one of the fixture's ellipses classified %q, want %q: %+v",
				hit.Severity, chatAnswerEllipsisSeverityOrdinary, hit)
		}
	}
	var report strings.Builder
	if n := reportChatAnswerEllipses(&report, hits, 0, 1); n != 0 {
		t.Fatalf("a turn with %d prose hit(s) and no cut label wrote %d report line(s), "+
			"want 0 — this is the noise the epic is about:\n%s", hits_, n, report.String())
	}
	if report.Len() != 0 {
		t.Fatalf("the silent turn still wrote bytes: %q", report.String())
	}
	// And the REPL seam is silent too: the operator's channel is the thing that
	// must stay empty, not just the io.Writer a test hands the reporter.
	if n := chatReportAnswerEllipses(h.in, h.r); n != 0 {
		t.Fatalf("the REPL seam wrote %d line(s) for a prose-only turn", n)
	}
	if got := h.notices.String(); got != "" {
		t.Fatalf("a prose-only turn announced something on the notice channel: %q", got)
	}

	// HALF TWO: the same turn PLUS one genuinely truncated pasted unit DOES speak,
	// and then the summary states the total — the prose is compressed, never lost.
	h2 := newEllipsisHarness(t, 2)
	for i := 0; i < hits_; i++ {
		h2.write("wait … ", "then continue. ")
	}
	h2.write("and the row is task/01a16033-dd15-… so continue there")
	h2.r.Finish()

	hits2 := h2.r.Ellipses()
	if len(hits2) != hits_+1 {
		t.Fatalf("the mixed body produced %d hits, want %d", len(hits2), hits_+1)
	}
	var mixed strings.Builder
	n := reportChatAnswerEllipses(&mixed, hits2, 0, 2)
	if n > chatAnswerEllipsisTruncatedLinesPerTurn+1 {
		t.Fatalf("a turn with %d hits wrote %d report lines, want at most %d (one detail + "+
			"one summary):\n%s", len(hits2), n, chatAnswerEllipsisTruncatedLinesPerTurn+1,
			mixed.String())
	}
	if n == 0 {
		t.Fatalf("a turn carrying a cut pasted unit wrote NOTHING:\n%s", mixed.String())
	}
	if !strings.Contains(mixed.String(), fmt.Sprintf("%d hit(s)", hits_+1)) {
		t.Fatalf("the report does not state the turn's hit count, so aggregation lost the "+
			"record:\n%s", mixed.String())
	}
	if !strings.Contains(mixed.String(), fmt.Sprintf("%d %s", hits_, chatAnswerEllipsisSeverityOrdinary)) {
		t.Fatalf("the report does not state the %d prose hits it did not print a line for:\n%s",
			hits_, mixed.String())
	}
}

// TestChatAnswerEllipsisTruncatedUnitIsNamed_RED is the criterion's RED half: the
// ellipsis falls INSIDE a token an operator must paste, so the report names the
// KIND and the token's own text — the thing the old line never said (it printed a
// rune offset and a flattened excerpt, and left the reader to reconstruct the
// token).
func TestChatAnswerEllipsisTruncatedUnitIsNamed_RED(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		kind string
	}{
		{
			name: "a workspace path",
			body: "the worktree is /home/user/workspace/01a15bb9-…-r1 and that is where it landed",
			kind: chatAnswerEllipsisUnitPath,
		},
		{
			name: "a task branch",
			body: "I pushed to task/01a16033-dd15-… this morning",
			kind: chatAnswerEllipsisUnitBranch,
		},
		{
			name: "a bare task id",
			body: "the row 01a16033-dd15-…-9411-9401b400aebd is pending",
			kind: chatAnswerEllipsisUnitIdentifier,
		},
		{
			name: "a flag",
			body: "run gopi worker --parent-task-id=01a1603… now",
			kind: chatAnswerEllipsisUnitCommand,
		},
		{
			name: "a URL",
			body: "see https://example.com/a/very/long/pa… for the evidence",
			kind: chatAnswerEllipsisUnitURL,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newEllipsisHarness(t, 2)
			h.write(tc.body)
			h.r.Finish()
			hits := h.r.Ellipses()
			if len(hits) != 1 {
				t.Fatalf("the body carries exactly one U+2026; the watch recorded %d: %+v",
					len(hits), hits)
			}
			got := hits[0]
			if got.Severity != chatAnswerEllipsisSeverityTruncated {
				t.Fatalf("an ellipsis INSIDE %s was classified %q, want %q — the criterion is "+
					"whether a pastable unit was cut, not whether the character appeared: %+v",
					tc.kind, got.Severity, chatAnswerEllipsisSeverityTruncated, got)
			}
			if got.UnitKind != tc.kind {
				t.Fatalf("the cut token was classified %q, want %q (token %q)",
					got.UnitKind, tc.kind, got.Unit)
			}
			var report strings.Builder
			n := reportChatAnswerEllipses(&report, hits, 0, 2)
			line := report.String()
			if n == 0 || !strings.Contains(line, chatAnswerEllipsisSeverityTruncated) {
				t.Fatalf("the report does not escalate the cut pasted unit:\n%s", line)
			}
			if !strings.Contains(line, strings.Trim(got.Unit, chatAnswerEllipsisUnitWrappers)) {
				t.Fatalf("the report does not name the truncated token %q — a reader cannot "+
					"complete what they were not shown:\n%s", got.Unit, line)
			}
			// THE HIGH LINE CARRIES THE SAME ANCHOR as the summary's LOW locator. The
			// merge of fix/ellipsis-offset-anchoring decided this deliberately: master
			// had already reduced the report to one detail line plus one summary, and
			// the branch's whole point (name the body the offset is measured on) has to
			// survive in whichever row the character landed in — otherwise a pasted
			// unit cut in a multi-message turn still invites the reader to compare
			// against the last round's wire text, which is the reported defect.
			wantAnchor := fmt.Sprintf("rune offset %d of %d", got.Offset, got.BodyRunes)
			// The HIGH row is LOGGED for the same reason the summary row is: a devdoc
			// example that no test in the tree can re-emit is a claim with no reader's
			// route to verify it (devdoc/chat-ellipsis-offset-basis.md quotes this line).
			t.Logf("%s: %s", tc.kind, strings.TrimRight(line, "\n"))
			if !strings.Contains(line, wantAnchor) {
				t.Fatalf("the escalated line does not anchor its offset (%q missing) — an "+
					"offset with no body named is the defect this guard exists for:\n%s",
					wantAnchor, line)
			}
		})
	}
}

// TestChatAnswerEllipsisProseOnlyTurnIsSilent is deliverable 2's first arm, kept
// MINIMAL on purpose so the contract is legible from the name alone: a turn whose
// every U+2026 is prose writes ZERO bytes to the operator's channel.
//
// It is the regression the epic exists for. Before it, this turn produced
//
//	[gopi] chat answer body: rendered U+2026 — turn 3: 2 hit(s): 0 HIGH …, 2 LOW prose …
//
// once per turn, with no switch and no silence condition — the file header
// promised "LOW = 0 lines" and the code gave LOW the summary line anyway. The
// detector's own record is asserted in the same test, because the silence is a
// budget on the REPORT and a change that made the DETECTOR quiet would pass this
// arm while deleting the backstop (session 8616b46c is why it stays).
func TestChatAnswerEllipsisProseOnlyTurnIsSilent(t *testing.T) {
	h := newEllipsisHarness(t, 3)
	h.write("the net is slow … anyway. ", "before … after. ")
	h.r.Finish()

	hits := h.r.Ellipses()
	if len(hits) != 2 {
		t.Fatalf("the detector recorded %d hit(s), want 2 — silence is a budget on the "+
			"REPORT, never on the detection: %+v", len(hits), hits)
	}
	for i, hit := range hits {
		if hit.Severity != chatAnswerEllipsisSeverityOrdinary || !hit.unitKnown {
			t.Fatalf("hit %d is not the prose class this arm is about: %+v", i, hit)
		}
	}
	var report strings.Builder
	if n := reportChatAnswerEllipses(&report, hits, 0, 3); n != 0 || report.Len() != 0 {
		t.Fatalf("a prose-only turn wrote %d line(s), want 0:\n%s", n, report.String())
	}
	if n := chatReportAnswerEllipses(h.in, h.r); n != 0 {
		t.Fatalf("the REPL seam wrote %d line(s) for a prose-only turn", n)
	}
	if got := h.notices.String(); got != "" {
		t.Fatalf("the operator's notice channel carried %q for a turn whose ellipses were all "+
			"prose", got)
	}
}

// TestChatAnswerEllipsisCutUnitSpeaksWithItsAnchor is deliverable 2's second arm:
// ONE ellipsis that cuts a pasted unit produces at least one line, and that line
// carries the two locators a reader needs to go and find it — the turn and the
// rune offset ANCHORED in the body it was measured on.
func TestChatAnswerEllipsisCutUnitSpeaksWithItsAnchor(t *testing.T) {
	const body = "pushed to task/01a16033-dd15-… this morning"
	h := newEllipsisHarness(t, 6)
	h.write(body)
	h.r.Finish()

	hits := h.r.Ellipses()
	if len(hits) != 1 {
		t.Fatalf("the body carries exactly one U+2026; the watch recorded %d: %+v", len(hits), hits)
	}
	hit := hits[0]
	if hit.Severity != chatAnswerEllipsisSeverityTruncated {
		t.Fatalf("a cut pasted unit was classified %q, want %q", hit.Severity,
			chatAnswerEllipsisSeverityTruncated)
	}
	wantOffset := 0
	for _, r := range body {
		if r == chatAnswerEllipsisRune {
			break
		}
		wantOffset++
	}
	var report strings.Builder
	n := reportChatAnswerEllipses(&report, hits, 0, 6)
	if n < 1 {
		t.Fatalf("a cut pasted unit wrote %d line(s), want at least 1", n)
	}
	out := report.String()
	t.Logf("the HIGH line the operator sees:\n%s", strings.TrimRight(out, "\n"))
	for _, want := range []string{
		chatAnswerEllipsisSeverityTruncated,
		chatAnswerEllipsisTruncatedWord,
		fmt.Sprintf("turn %d", 6),
		fmt.Sprintf("rune offset %d of %d", wantOffset, len([]rune(body))),
		"NOT altered",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("the line does not carry %q:\n%s", want, out)
		}
	}
	// And the operator's channel is where it lands — the report is for him, not
	// for a log.
	if n := chatReportAnswerEllipses(h.in, h.r); n < 1 {
		t.Fatalf("the REPL seam wrote %d line(s) for a cut pasted unit", n)
	}
	if got := h.notices.String(); !strings.Contains(got, chatAnswerEllipsisSeverityTruncated) {
		t.Fatalf("the operator's notice channel did not carry the HIGH line: %q", got)
	}
}

// TestChatAnswerEllipsisUnitKinds is the classifier's own table, including the
// cases that MUST stay silent: an English word, a piece of punctuation, and a
// token the body ends inside (unknowable, and therefore counted as undecidable
// rather than quietly downgraded to prose).
func TestChatAnswerEllipsisUnitKinds(t *testing.T) {
	for _, tc := range []struct {
		token string
		want  string
	}{
		{"x/y/…", chatAnswerEllipsisUnitPath},
		{"task/01a16033-…", chatAnswerEllipsisUnitBranch},
		{"--repo=/home/user/…", chatAnswerEllipsisUnitCommand},
		{"https://x.test/a…", chatAnswerEllipsisUnitURL},
		{"01a16033-dd15-…", chatAnswerEllipsisUnitIdentifier},
		{"`pool.go`…", chatAnswerEllipsisUnitIdentifier},
		{"wait…", ""},
		{"the…", ""},
		{"…", ""},
		{"not…", ""},
	} {
		if got := chatAnswerEllipsisUnitKind(tc.token); got != tc.want {
			t.Fatalf("chatAnswerEllipsisUnitKind(%q) = %q, want %q", tc.token, got, tc.want)
		}
	}
}
