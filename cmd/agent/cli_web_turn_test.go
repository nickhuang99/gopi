package main

// cli_web_turn_test.go — THE REGRESSION GUARD for the turn banner.
//
// THE DEFECT THIS PINS is a number, so the assertion is about the number's
// AUTHORITY rather than about the page's markup: the ordinal the server puts on
// the wire for a turn must equal the ordinal the SESSION RECORD gives that same
// turn. Anything else (a page counter, a per-run counter, a re-counted history)
// passes a "does a banner appear?" test and still tells the reader the wrong
// turn — which is worse than no banner, because it looks authoritative.
//
// THE RECORD IS THE ORACLE, not a second copy of the arithmetic: the assertions
// below read the JSONL the run itself wrote and ask internal/gopilog (the same
// reader `gopi session log --turns` and `session replay --turn N` use) which
// turn each row opens. If the writer's predicate and the reader's ever drift,
// these tests go red on the drift instead of on a coincidence.

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/gopilog"
	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// turnBandFrame pulls the `turn` frame's number out of a raw SSE body, or 0
// when the body carries none.
func turnBandFrame(t *testing.T, body string) int {
	t.Helper()
	for _, pair := range sseEvents(body) {
		if pair[0] != webEventTurn {
			continue
		}
		// A GENERAL map, not map[string]int: the frame carries the ordinal AND
		// the moment the turn opened (a string), so a typed decode would fail on
		// the frame it is supposed to read. The ordinal is taken out by name.
		var d map[string]interface{}
		if err := json.Unmarshal([]byte(pair[1]), &d); err != nil {
			t.Fatalf("the %s frame is not JSON: %v (payload %q)", webEventTurn, err, pair[1])
		}
		n, _ := d[webTurnFrameField].(float64)
		return int(n)
	}
	return 0
}

// turnFrameCount counts how many `turn` frames a body carries (a turn must be
// announced ONCE, not once per delta).
func turnFrameCount(body string) int {
	n := 0
	for _, pair := range sseEvents(body) {
		if pair[0] == webEventTurn {
			n++
		}
	}
	return n
}

// doneSessionTurn pulls `session_turn` out of the terminal `done` frame.
func doneSessionTurn(t *testing.T, body string) int {
	t.Helper()
	for _, pair := range sseEvents(body) {
		if pair[0] != webEventDone {
			continue
		}
		var d map[string]interface{}
		if err := json.Unmarshal([]byte(pair[1]), &d); err != nil {
			t.Fatalf("the %s frame is not JSON: %v", webEventDone, err)
		}
		if v, ok := d[webSessionTurnField]; ok {
			f, _ := v.(float64)
			return int(f)
		}
	}
	return 0
}

// webServerWithSessionRecord builds a web server over a REAL session logger —
// the record is the oracle, so the test cannot use a nil log.
func webServerWithSessionRecord(t *testing.T, finalText string) (*webServer, *agent.Agent) {
	t.Helper()
	t.Setenv("GOPI_HOME", t.TempDir())
	mockSrv := mockStreamingServer(t, finalText)
	sl, err := agent.NewSessionLogger("deepseek-v4-pro", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	ag := &agent.Agent{
		Client:     &deepseek.Client{BaseURL: mockSrv.URL, APIKey: "k", HTTPClient: mockSrv.Client()},
		Ctx:        &agent.Context{},
		Model:      "deepseek-v4-pro",
		SessionLog: sl,
		Logger:     log.New(io.Discard, "", 0),
	}
	ag.PersistChatSession = true
	t.Cleanup(func() {
		mockSrv.Close()
		// Teardown of a fixture this test opened: the record it wrote was already
		// read through its own reader above, so a failed close here has no
		// assertion left to corrupt.
		sl.Close() //stylegate:ignore fixture teardown after a checked Sync; nothing downstream reads this handle again
	})
	ws := &webServer{addr: ":0", agent: ag, startedAt: time.Now()}
	return ws, ag
}

// TestWebTurnBannerMatchesTheSessionRecord is the epic's core assertion: every
// turn the page is told about carries the ordinal the RECORD gives that turn.
//
// THREE turns are driven through handleChat, and after each one the record's
// own reader is asked which turn the row it just wrote opened. The frame's
// number must equal it — 1, then 2, then 3 — and the terminal `done` frame must
// repeat it (so a client whose turn frame was dropped can still label the
// answer).
func TestWebTurnBannerMatchesTheSessionRecord(t *testing.T) {
	ws, ag := webServerWithSessionRecord(t, "answer")
	ws.turnBaseline = webTurnBaseline(ag.SessionLog.Path())

	for want := 1; want <= 3; want++ {
		body := postChat(t, ws, fmt.Sprintf("question %d", want), nil)

		got := turnBandFrame(t, body)
		if got != want {
			t.Fatalf("turn %d: the %s frame says %d, want %d", want, webEventTurn, got, want)
		}
		if n := turnFrameCount(body); n != 1 {
			t.Errorf("turn %d: the body carries %d %s frames, want exactly 1", want, n, webEventTurn)
		}
		if dt := doneSessionTurn(t, body); dt != want {
			t.Errorf("turn %d: %s.%s = %d, want %d", want, webEventDone, webSessionTurnField, dt, want)
		}

		// THE ORACLE: the record's own count after this turn. SYNC (not Close):
		// the writer buffers a turn's rows, and the loop below needs the record
		// readable NOW — closing it would make every later turn a write to a
		// closed file and the oracle would then be measuring nothing.
		if err := ag.SessionLog.Sync(); err != nil {
			t.Fatalf("turn %d: flush the session record: %v", want, err)
		}
		sess, err := gopilog.ParseFile(ag.SessionLog.Path())
		if err != nil {
			t.Fatalf("turn %d: read the session record: %v", want, err)
		}
		if got := gopilog.SessionTurns(sess); got != want {
			t.Fatalf("turn %d: the RECORD holds %d turn(s) — the frame and the record disagree", want, got)
		}
	}

	// THE ORDINALS THE RECORD REPLAYS carry the same numbers, which is what the
	// refreshed page draws its banners from.
	prompts, err := webSessionTurnPrompts(ag.SessionLog.Path())
	if err != nil {
		t.Fatalf("webSessionTurnPrompts: %v", err)
	}
	if len(prompts) != 3 {
		t.Fatalf("the record holds %d turn-opening prompts, want 3 (%q)", len(prompts), prompts)
	}
	for i, want := range []string{"question 1", "question 2", "question 3"} {
		if prompts[i] != want {
			t.Errorf("recorded turn %d opened with %q, want %q", i+1, prompts[i], want)
		}
	}
}

// TestWebTurnBannerOnAResumedSession pins the RESUME half: a server continuing a
// record that already holds N turns numbers its first new turn N+1, and the
// replay of the history it inherited carries 1..N.
//
// WITHOUT turnBaseline this is the defect the banner would introduce: the page
// would show "turn 1" over the 21st turn of the conversation.
func TestWebTurnBannerOnAResumedSession(t *testing.T) {
	t.Setenv("GOPI_HOME", t.TempDir())
	mockSrv := mockStreamingServer(t, "answer")
	defer mockSrv.Close()

	// Build a record that already holds 2 turns, by driving the REAL web path
	// twice — no hand-written JSONL, so the oracle is this code and not a
	// second opinion about what a turn row looks like.
	first, firstAgent := webServerWithSessionRecord(t, "answer")
	first.turnBaseline = webTurnBaseline(firstAgent.SessionLog.Path())
	postChat(t, first, "earlier one", nil)
	postChat(t, first, "earlier two", nil)
	path := firstAgent.SessionLog.Path()
	sessionID := firstAgent.SessionLog.SessionID()
	if err := firstAgent.SessionLog.Sync(); err != nil {
		t.Fatalf("flush the earlier record: %v", err)
	}
	// The close is the FLUSH the baseline reading below depends on: the ordinal
	// arithmetic is read back off the record on disk, so a close that failed to
	// flush would make the assertion measure a stale file rather than the turns
	// this run wrote.
	if err := firstAgent.SessionLog.Close(); err != nil {
		t.Fatalf("flush the earlier record before reading it back: %v", err)
	}

	baseline := webTurnBaseline(path)
	if baseline != 2 {
		t.Fatalf("webTurnBaseline(%s) = %d, want 2 (the record already holds two turns)", path, baseline)
	}

	// THE CONTINUED RUN: a fresh logger APPENDING to the same record — the
	// resumed shape, and the reason the ordinal arithmetic cannot come from the
	// writer alone: this logger has written nothing yet, so its own count is 0
	// while the record already holds two turns.
	resumed, err := agent.NewSessionLoggerForResumedRun("deepseek-v4-pro", "", sessionID, nil, sessionID)
	if err != nil {
		t.Fatalf("open the record appending (the resumed shape): %v", err)
	}
	t.Cleanup(func() {
		// Teardown of the resumed writer; its bytes were asserted through the
		// record reader before this point.
		resumed.Close() //stylegate:ignore fixture teardown of the resumed writer; its own turns were read back through the record
	})
	if got := resumed.TurnsSoFar(); got != 0 {
		t.Fatalf("the resumed writer reports %d turns before writing anything, want 0", got)
	}

	running := &webServer{
		addr:         ":0",
		startedAt:    time.Now(),
		turnBaseline: baseline,
		agent: &agent.Agent{
			Client:     &deepseek.Client{BaseURL: mockSrv.URL, APIKey: "k", HTTPClient: mockSrv.Client()},
			Ctx:        &agent.Context{},
			Model:      "deepseek-v4-pro",
			SessionLog: resumed,
			Logger:     log.New(io.Discard, "", 0),
		},
	}
	running.agent.PersistChatSession = true

	// The number the wire must carry is the record's NEXT turn.
	n, isTurn := running.webTurnOrdinal("the next question", "")
	if !isTurn {
		t.Fatal("a plain operator line was not counted as a turn opener")
	}
	sess, err := gopilog.ParseFile(path)
	if err != nil {
		t.Fatalf("read the record: %v", err)
	}
	want := gopilog.SessionTurns(sess) + 1
	if n != want {
		t.Fatalf("the resumed server announces turn %d, the record says the next turn is %d", n, want)
	}

	// AND THE REPLAY: the inherited history is numbered 1..N from the record.
	history := []deepseek.Message{
		{Role: "user", Content: "earlier one"},
		{Role: "assistant", Content: "a"},
		{Role: "user", Content: "earlier two"},
		{Role: "assistant", Content: "b"},
	}
	ordinals, terr := running.webTranscriptTurnOrdinals(history)
	if terr != nil {
		t.Fatalf("webTranscriptTurnOrdinals: %v", terr)
	}
	if len(ordinals) != 2 {
		t.Fatalf("the replay numbered %d rows, want 2 (one per turn-opening user row): %v", len(ordinals), ordinals)
	}
	if ordinals[0] != 1 || ordinals[2] != 2 {
		t.Errorf("replayed ordinals = %v, want the user rows at 0→1 and 2→2", ordinals)
	}
}

// TestWebTurnBannerSkipsNonTurnRows: a row the record charges no turn to opens
// no turn, so the server must send NO frame and put NO session_turn on `done`.
// A fabricated number here would be a number the record does not give that row.
//
// The carrier used is the framework's own inbox marker, which is the row shape
// that made the predicate necessary in the first place
// (sessionevent.IsTurnOpeningRow's own account).
func TestWebTurnBannerSkipsNonTurnRows(t *testing.T) {
	ws, ag := webServerWithSessionRecord(t, "answer")
	ws.turnBaseline = webTurnBaseline(ag.SessionLog.Path())

	carrier := sessionevent.HarnessMessagePrefix + "the operator's line"
	if _, isTurn := ws.webTurnOrdinal(carrier, ""); isTurn {
		t.Fatalf("a framework carrier row (%q) was counted as a turn opener", carrier)
	}
	// And the positive control, so a predicate that answered false everywhere
	// cannot pass the assertion above.
	if n, isTurn := ws.webTurnOrdinal("an ordinary line", ""); !isTurn || n != 1 {
		t.Fatalf("an ordinary operator line: n=%d isTurn=%v, want 1,true", n, isTurn)
	}
}

// TestWebTurnBandIsOnTheWireAndInThePage pins the Page half at the level a Go
// test can: the page declares the frame name and the field by the SAME strings
// the server sends (Rule 1b — one wire vocabulary, two carriers), and the band
// renderer exists and is driven by the frame rather than by a local counter.
func TestWebTurnBandIsOnTheWireAndInThePage(t *testing.T) {
	pairs := []struct {
		name         string
		server, page string
	}{
		{"turn event", webEventTurn, pageConstant(t, "EV_TURN")},
		{"done field", webSessionTurnField, pageConstant(t, "SESSION_TURN_FIELD")},
	}
	for _, p := range pairs {
		if p.server != p.page {
			t.Errorf("%s: server spells %q, the page spells %q", p.name, p.server, p.page)
		}
	}
	for _, want := range []string{
		"function addTurnBand(",
		"ev===EV_TURN",
		"es.addEventListener(EV_TURN",
		"m[SESSION_TURN_FIELD]",
		".turnband{",
	} {
		if !strings.Contains(webChatPage, want) {
			t.Errorf("the page does not carry %q — the banner has no reader", want)
		}
	}
}

// TestWebTranscriptCarriesTheSessionTurn drives the replay endpoint end to end:
// after two POSTed turns, /api/transcript's user entries carry the record's
// ordinals, so a refreshed page can redraw the banners it lost.
func TestWebTranscriptCarriesTheSessionTurn(t *testing.T) {
	ws, ag := webServerWithSessionRecord(t, "answer")
	ws.turnBaseline = webTurnBaseline(ag.SessionLog.Path())

	postChat(t, ws, "first question", nil)
	postChat(t, ws, "second question", nil)
	// Same reason as the baseline read above: the transcript handler serves the
	// record FROM DISK, so this close is the flush that makes the two turns
	// readable and a failure here would show up as a wrong transcript.
	if err := ag.SessionLog.Close(); err != nil {
		t.Fatalf("flush the record before the transcript is read from it: %v", err)
	}

	rec := httptest.NewRecorder()
	ws.handleTranscript(rec, httptest.NewRequest(http.MethodGet, webTranscriptPath, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", webTranscriptPath, rec.Code)
	}
	var doc struct {
		Messages []struct {
			Role        string `json:"role"`
			Content     string `json:"content"`
			SessionTurn int    `json:"session_turn"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("the transcript is not JSON: %v", err)
	}
	var got []int
	for _, m := range doc.Messages {
		if m.Role == "user" {
			got = append(got, m.SessionTurn)
		}
	}
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("the replayed user rows carry ordinals %v, want [1 2]", got)
	}
}

// TestWebTurnBaselineReadsTheRecord pins the measurement itself, including its
// honest degradation: an absent record is 0 and is reported, never guessed.
func TestWebTurnBaselineReadsTheRecord(t *testing.T) {
	if n := webTurnBaseline(""); n != 0 {
		t.Errorf("webTurnBaseline(no path) = %d, want 0", n)
	}
	missing := filepath.Join(t.TempDir(), "no-such-record.jsonl")
	if n := webTurnBaseline(missing); n != 0 {
		t.Errorf("webTurnBaseline(missing) = %d, want 0 (the refusal is reported, not substituted)", n)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("the fixture wrote %s — the measurement must not create a record", missing)
	}
}
