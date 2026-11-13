package gopilog

// turnopened_stamp_test.go — THE ROW'S OWN MOMENT is what a banner reads.
//
// The chain a banner depends on is short and it is entirely mechanical: the
// reader stamps the turn-opening ROW's `timestamp` field onto the iteration that
// row opened (`Iteration.TurnOpenedAt`), and every display surface renders that
// value rather than a clock. So the two things worth pinning here are exactly
// the two ways the chain can silently break:
//
//  1. the value is the ROW's (a different row = a different moment), and
//  2. a row that recorded NO timestamp yields the ZERO value — not a time, and
//     not the moment of the next row that happened to carry one.
//
// Both arms fail if someone "simplifies" the plumbing by having the display ask
// time.Now(), because arm 1 would then be measuring the fixture's own age.

import (
	"testing"
	"time"
)

// TestTurnOpenedAtIsTheTurnOpeningRowsOwnTimestamp drives two turns whose
// timestamps are deliberately far apart and asserts each iteration carries ITS
// own row's moment.
func TestTurnOpenedAtIsTheTurnOpeningRowsOwnTimestamp(t *testing.T) {
	const first = "2026-11-08T12:31:20.5+08:00"
	const second = "2026-11-08T15:02:03.25+08:00"
	path := writeFixture(t, []string{
		`{"model":"deepseek-v4-pro","sessionId":"stamp-1","timestamp":"2026-11-08T12:31:00+08:00","type":"session_start","workdir":"/tmp/x"}`,
		`{"message":{"content":"first question","role":"user"},"promptId":"p1","timestamp":"` + first + `","type":"user"}`,
		`{"message":{"content":[{"text":"first answer","type":"text"}],"role":"assistant"},"timestamp":"2026-11-08T12:31:25+08:00","type":"assistant"}`,
		`{"message":{"content":"second question","role":"user"},"promptId":"p2","timestamp":"` + second + `","type":"user"}`,
		`{"message":{"content":[{"text":"second answer","type":"text"}],"role":"assistant"},"timestamp":"2026-11-08T15:02:10+08:00","type":"assistant"}`,
	})
	sess, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	bounds := SessionTurnBounds(sess)
	if len(bounds) != 2 {
		t.Fatalf("the fixture holds %d turn(s), want 2 (bounds %v)", len(bounds), bounds)
	}
	want := []string{first, second}
	for k, idx := range bounds {
		got := sess.Iterations[idx].TurnOpenedAt
		gt, err := time.Parse(time.RFC3339Nano, want[k])
		if err != nil {
			t.Fatalf("fixture timestamp %q does not parse: %v", want[k], err)
		}
		if !got.Equal(gt) {
			t.Fatalf("turn %d opened at %v, want the ROW's own %v — the moment must come from the record, not from a clock",
				k+1, got, gt)
		}
		// A SECOND READER of the same instant is the point of the field: the
		// zero value here would make every banner print its placeholder.
		if got.IsZero() {
			t.Fatalf("turn %d carries no moment", k+1)
		}
	}
}

// TestTurnOpenedAtIsZeroWhenTheRowRecordedNone — Rule 15's arm for this field. A
// user row written by a writer that stamped nothing (an older writer, a
// hand-edited record) yields the ZERO value, and the zero value is what makes
// the display print its explicit placeholder. Substituting the neighbouring
// row's timestamp would date a turn with another row's moment, which is worse
// than saying the time is unknown.
func TestTurnOpenedAtIsZeroWhenTheRowRecordedNone(t *testing.T) {
	path := writeFixture(t, []string{
		`{"model":"deepseek-v4-pro","sessionId":"stamp-2","timestamp":"2026-11-08T09:00:00+08:00","type":"session_start","workdir":"/tmp/x"}`,
		`{"message":{"content":"a question from an older writer","role":"user"},"promptId":"p1","type":"user"}`,
		`{"message":{"content":[{"text":"an answer","type":"text"}],"role":"assistant"},"timestamp":"2026-11-08T09:00:05+08:00","type":"assistant"}`,
	})
	sess, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	bounds := SessionTurnBounds(sess)
	if len(bounds) != 1 {
		t.Fatalf("the fixture holds %d turn(s), want 1 (bounds %v)", len(bounds), bounds)
	}
	if got := sess.Iterations[bounds[0]].TurnOpenedAt; !got.IsZero() {
		t.Fatalf("a row with no timestamp produced %v, want the zero value (the display then prints its placeholder)", got)
	}
}
