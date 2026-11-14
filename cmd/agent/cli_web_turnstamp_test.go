package main

// cli_web_turnstamp_test.go — THE BANNER'S MOMENT, pinned against the record.
//
// cli_web_turn_test.go pins the ORDINAL's authority; this file pins the other
// half of the same banner: the moment. The criterion is identical and so is the
// oracle — the value on the wire must be the timestamp of the row that OPENED
// that turn, read back out of the JSONL through internal/gopilog. A number the
// page computes from its own arrival time, or from `time.Now()` at render time,
// passes a "does a time appear?" test and still dates the turn wrongly (and, on
// a resumed or replayed session, lands on the wrong DAY) — which is exactly the
// failure the historical-value rule exists to prevent.
//
// THE THREE ARMS:
//
//  1. the `turn` frame carries the record's moment for the turn it opens, and
//     the terminal `done` frame REPEATS it (a client whose turn frame a
//     reconnect dropped must still be able to label its answer);
//  2. the moment travels on the record's own row, i.e. the writer stamped the
//     row with the SAME instant the page was told — one mint, two readers;
//  3. the transcript replay carries it per turn, so a refreshed page redraws
//     the historic moments rather than today's.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/gopilog"
	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// turnFrameStamp pulls `session_turn_opened_at` out of a raw SSE body's `turn`
// frame (or "" when the frame carries none).
func turnFrameStamp(t *testing.T, body string) string {
	t.Helper()
	for _, pair := range sseEvents(body) {
		if pair[0] != webEventTurn {
			continue
		}
		var d map[string]interface{}
		if err := json.Unmarshal([]byte(pair[1]), &d); err != nil {
			t.Fatalf("the %s frame is not JSON: %v (payload %q)", webEventTurn, err, pair[1])
		}
		s, _ := d[webSessionTurnOpenedField].(string)
		return s
	}
	return ""
}

// doneFrameStamp pulls `session_turn_opened_at` out of the terminal `done` frame.
func doneFrameStamp(t *testing.T, body string) string {
	t.Helper()
	for _, pair := range sseEvents(body) {
		if pair[0] != webEventDone {
			continue
		}
		var d map[string]interface{}
		if err := json.Unmarshal([]byte(pair[1]), &d); err != nil {
			t.Fatalf("the %s frame is not JSON: %v (payload %q)", webEventDone, err, pair[1])
		}
		s, _ := d[webSessionTurnOpenedField].(string)
		return s
	}
	return ""
}

// TestWebTurnBannerCarriesTheRecordsMoment is arms 1 and 2.
//
// The oracle is read AFTER the turn, from the record: the timestamp of the `user`
// row that opened the turn, rendered through the SAME shared formatter the page's
// string comes from. Equal strings therefore mean "the page was told the record's
// moment", which is the only thing this banner may claim.
func TestWebTurnBannerCarriesTheRecordsMoment(t *testing.T) {
	ws, ag := webServerWithSessionRecord(t, "answer")
	ws.turnBaseline = webTurnBaseline(ag.SessionLog.Path())

	// THREE turns, each far enough apart that a page-side clock could be caught
	// printing a later second than the row's.
	for want := 1; want <= 3; want++ {
		body := postChat(t, ws, fmt.Sprintf("stamped question %d", want), nil)

		frameStamp := turnFrameStamp(t, body)
		if frameStamp == "" {
			t.Fatalf("turn %d: the %s frame carries no %s", want, webEventTurn, webSessionTurnOpenedField)
		}
		if frameStamp == sessionevent.TurnStampPlaceholder {
			t.Fatalf("turn %d: the frame carries the placeholder although the turn has a record behind it", want)
		}
		if got := doneFrameStamp(t, body); got != frameStamp {
			t.Fatalf("turn %d: the %s frame says %q, the %s frame says %q — one turn cannot open at two moments",
				want, webEventTurn, frameStamp, webEventDone, got)
		}

		// THE ORACLE: the record's own row for this turn.
		if err := ag.SessionLog.Sync(); err != nil {
			t.Fatalf("turn %d: flush the session record: %v", want, err)
		}
		sess, err := gopilog.ParseFile(ag.SessionLog.Path())
		if err != nil {
			t.Fatalf("turn %d: read the session record: %v", want, err)
		}
		bounds := gopilog.SessionTurnBounds(sess)
		if len(bounds) != want {
			t.Fatalf("turn %d: the record holds %d turn(s)", want, len(bounds))
		}
		rowAt := sess.Iterations[bounds[want-1]].TurnOpenedAt
		if rowAt.IsZero() {
			t.Fatalf("turn %d: the record's turn-opening row carries no timestamp — the banner's value has no source", want)
		}
		wantStamp := sessionevent.FormatTurnStamp(rowAt)
		if frameStamp != wantStamp {
			t.Fatalf("turn %d: the frame says %q, the record's row says %q (row timestamp %s) — the banner must show the ROW's moment",
				want, frameStamp, wantStamp, rowAt.Format(time.RFC3339Nano))
		}
	}
}

// TestWebTranscriptReplayCarriesEachTurnsMoment is arm 3: a refreshed page draws
// its banners from /api/transcript, so each turn-opening row must carry ITS OWN
// moment — the historic one, not the moment of the refresh.
func TestWebTranscriptReplayCarriesEachTurnsMoment(t *testing.T) {
	ws, ag := webServerWithSessionRecord(t, "answer")
	ws.turnBaseline = webTurnBaseline(ag.SessionLog.Path())

	for want := 1; want <= 2; want++ {
		postChat(t, ws, fmt.Sprintf("replayed question %d", want), nil)
	}
	if err := ag.SessionLog.Sync(); err != nil {
		t.Fatalf("flush the session record: %v", err)
	}

	rec := httptest.NewRecorder()
	ws.handleTranscript(rec, httptest.NewRequest(http.MethodGet, "/api/transcript", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/transcript returned %d: %s", rec.Code, rec.Body.String())
	}
	var doc struct {
		Messages []struct {
			Role                string `json:"role"`
			Content             string `json:"content"`
			SessionTurn         int    `json:"session_turn"`
			SessionTurnOpenedAt string `json:"session_turn_opened_at"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("the transcript is not JSON: %v", err)
	}
	entries := doc.Messages

	// The record's own moments, in order — the oracle.
	sess, err := gopilog.ParseFile(ag.SessionLog.Path())
	if err != nil {
		t.Fatalf("read the session record: %v", err)
	}
	bounds := gopilog.SessionTurnBounds(sess)
	if len(bounds) != 2 {
		t.Fatalf("the record holds %d turn(s), want 2", len(bounds))
	}

	seen := 0
	for _, e := range entries {
		if e.SessionTurn == 0 {
			continue
		}
		if e.SessionTurn > len(bounds) {
			t.Fatalf("a replayed row is numbered turn %d, the record holds %d", e.SessionTurn, len(bounds))
		}
		wantStamp := sessionevent.FormatTurnStamp(sess.Iterations[bounds[e.SessionTurn-1]].TurnOpenedAt)
		if e.SessionTurnOpenedAt != wantStamp {
			t.Fatalf("replayed turn %d carries the moment %q, the record's row says %q",
				e.SessionTurn, e.SessionTurnOpenedAt, wantStamp)
		}
		seen++
	}
	if seen == 0 {
		t.Fatalf("no replayed row carried a session turn — the arm measured nothing")
	}
}
