// commands_session_web_test.go — the WEB carrier's half of the portability
// census: /info and /context driven through the REAL route (/api/chat →
// handleChat → serveSlashCommand) and read back off the SSE wire, not by calling
// the command functions. That distinction is the epic's whole subject: the
// defect was that one carrier answered a word and the other REFUSED it, so a test
// that skipped the dispatch would have stayed green through the defect (Rule 31).
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/agent"
)

// webSlashServer builds the production shape: a web server whose ONE persistent
// agent holds a REAL session logger and raw logger, which is what `runWeb`'s
// agent factory produces. The older web fixtures leave both nil, so a command
// that describes the session would be judged against a server that does not
// exist — the fixture, not the command, would be the thing under test.
func webSlashServer(t *testing.T, tools bool, withRaw bool) (*webServer, func()) {
	t.Helper()
	ws, mockSrv := webServerForTest(t, "never reached")
	ws.tools = tools
	logger, err := agent.NewSessionLogger("", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	ws.agent.SessionLog = logger
	cleanup := func() {
		_ = logger.Close() //stylegate:ignore the fixture's session logger is closed at the end of the surface it describes
		mockSrv.Close()
	}
	if withRaw {
		rl := newSlashContextRawLog(t, 1)
		ws.agent.RawLog = rl
		inner := cleanup
		cleanup = func() {
			_ = rl.Close() //stylegate:ignore the fixture's raw logger is closed after its one record was read
			inner()
		}
	}
	return ws, cleanup
}

// loggerID is the fixture's live session UUID, read through the server the frame
// was produced by — so /info's claim is compared against the logger the REPL
// would also name, not against a value copied out of the block itself.
func loggerID(t *testing.T, ws *webServer) string {
	t.Helper()
	if ws.agent == nil || ws.agent.SessionLog == nil {
		t.Fatal("this fixture's server holds no session logger")
	}
	return ws.agent.SessionLog.SessionID()
}

// webCommandFrame posts one slash line and returns the `command` frame's decoded
// body. A frame this call cannot find is a failure: the page renders nothing
// when the server answers without one.
func webCommandFrame(t *testing.T, ws *webServer, line string) map[string]string {
	t.Helper()
	body, _ := json.Marshal(webChatRequest{Message: line}) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	req := httptest.NewRequest(http.MethodPost, webChatPath, strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	ws.handleChat(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST %s %q: status %d (body: %s)", webChatPath, line, rec.Code, rec.Body.String())
	}
	for _, ev := range sseEvents(rec.Body.String()) {
		if ev[0] != webEventCommand {
			continue
		}
		var got map[string]string
		if err := json.Unmarshal([]byte(ev[1]), &got); err != nil {
			t.Fatalf("decode %q frame for %q: %v (raw: %s)", webEventCommand, line, err, ev[1])
		}
		return got
	}
	t.Fatalf("%q produced no %q frame — the page would render nothing (body: %s)", line, webEventCommand, rec.Body.String())
	return nil
}

// TestWebSlashInfoAnswersFromTheServersOwnSession is the delivery proof for the
// browser: /info, which the web carrier REFUSED before this change ("unknown
// command"), now prints this server's session — its one persistent agent's UUID,
// the model, the ground and the tools flag.
func TestWebSlashInfoAnswersFromTheServersOwnSession(t *testing.T) {
	ws, done := webSlashServer(t, false, false)
	defer done()

	got := webCommandFrame(t, ws, "/info")
	if err, refused := got["error"]; refused && err != "" {
		t.Fatalf("/info is refused on the web carrier: %s", err)
	}
	out := got["output"]
	if strings.TrimSpace(out) == "" {
		t.Fatal("/info answered with an empty block")
	}
	// The block is the SESSION's, not a placeholder: the model this server's
	// agent was built with must appear in it.
	if !strings.Contains(out, ws.agent.Model) {
		t.Errorf("/info does not carry this server's model %q:\n%s", ws.agent.Model, out)
	}
	// The block ends with lines a reader PASTES, spelled with the CLI verbs'
	// constants (chatInfoReplayCommand): losing them would leave the UUID
	// unusable, which is the one thing the block exists for.
	if !strings.Contains(out, "gopi chat --resume "+loggerID(t, ws)) {
		t.Errorf("/info's block lost its replay command for this session's UUID:\n%s", out)
	}
	if !strings.Contains(out, loggerID(t, ws)) {
		t.Errorf("/info's block does not carry the live session UUID:\n%s", out)
	}
}

// TestWebSlashContextRefusesWithItsCauseOnANoLogServer: the browser gets the
// SAME refusal the console does, by name — not "unknown command" (the old
// behaviour) and not an empty file.
func TestWebSlashContextRefusesWithItsCauseOnANoLogServer(t *testing.T) {
	ws, done := webSlashServer(t, false, false)
	defer done()

	got := webCommandFrame(t, ws, "/context")
	msg, refused := got["error"]
	if !refused || msg == "" {
		t.Fatalf("/context must refuse on a server with no raw log, got output:\n%s", got["output"])
	}
	if strings.Contains(msg, "unknown command") {
		t.Fatalf("/context is still unknown to the web carrier: %s", msg)
	}
	// The cause is named (this fixture's agent holds no raw logger) — a reader
	// learns WHY, and the same sentence the console would print.
	if !strings.Contains(msg, "raw HTTP log") && !strings.Contains(msg, "log") {
		t.Errorf("/context's refusal does not name the missing input: %s", msg)
	}
}

// TestWebSlashHelpListsThePortableWords is the browser's discoverability proof:
// /help on the web carrier lists both words, so a browser user can find them
// without reading the source.
func TestWebSlashHelpListsThePortableWords(t *testing.T) {
	ws, done := webSlashServer(t, false, false)
	defer done()

	got := webCommandFrame(t, ws, "/help")
	page := got["output"]
	for _, word := range []string{chatInfoCommandName, chatContextCommandName} {
		if !strings.Contains(page, "/"+word) {
			t.Errorf("/help on the web carrier does not list /%s:\n%s", word, page)
		}
	}
}

// TestWebSlashInfoFollowsTheToolsFlag: the block is read at the moment the human
// asked, so a server started with --tools says so and one started without says
// so. A value captured once at startup would pass the first assertion and fail
// the second.
func TestWebSlashInfoFollowsTheToolsFlag(t *testing.T) {
	for _, on := range []bool{true, false} {
		ws, done := webSlashServer(t, on, false)
		got := webCommandFrame(t, ws, "/info")
		done()
		if got["error"] != "" {
			t.Fatalf("tools=%v: /info refused: %s", on, got["error"])
		}
		out := got["output"]
		// The row is the ONE renderer's value, asserted through the constant that
		// spells it rather than through a hand-typed string.
		want := chatInfoToolsValue(on)
		if !strings.Contains(out, want) {
			t.Errorf("tools=%v: the block does not report it (want %q):\n%s", on, want, out)
		}
	}
}
