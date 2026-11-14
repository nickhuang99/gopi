package main

// chat_input_classifier_test.go — the criterion that decides whether ONE input
// line is a command or the model's text (epic 01a104cb).
//
// THE DEFECT THIS FILE GUARDS. Every carrier used to ask `strings.HasPrefix(
// trimmed, "/")` and nothing else, so a pasted path ("/tmp/CN-result.gif") was
// refused with "unknown command" and never reached the model — and there was no
// second spelling to reach it with. The fix is one classifier (classifyChatLine,
// commands.go) that FOUR carriers ask, so the criterion is tested once and the
// carriers are tested for ASKING IT.
//
// What is asserted here, in the order the epic states it:
//
//  1. the criterion itself, shape by shape, in a table (the design is an
//     opinion and the table is where it is written down);
//  2. the four carriers agree on the same input — the two HTTP carriers
//     (web/term) and the REPL are driven through their REAL handlers here, and
//     `--once` through the real binary in testsuite/chat_once_slash_test.go;
//  3. the escape channel ("//word" → the literal "/word") reaches the model
//     with the slash intact — on the wire, not in the intent;
//  4. a true command and an unknown command are unchanged: the first still
//     dispatches, the second is still REFUSED and still never sent.

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/deepseek"
)

// TestClassifyChatLineCriterion is the criterion table: for every shape a user
// can type, which verdict the ONE classifier returns. The vocabulary is the REPL
// `gopi chat` (the largest of the four), so a case here also says what the other
// carriers do with the same line.
func TestClassifyChatLineCriterion(t *testing.T) {
	cases := []struct {
		name string
		line string
		kind chatLineKind
		text string // what the model is asked (kind text/escape)
		word string // the command word (kind command/unknown/escape)
	}{
		// TEXT BY ITS SHAPE — nothing to escape, nothing to learn.
		{"a pasted absolute path", "/tmp/CN-result.gif", chatLineText, "/tmp/CN-result.gif", ""},
		{"a path with one segment", "/etc", chatLineUnknown, "", "etc"},
		{"a regex the user anchored", "/^gopi/", chatLineText, "/^gopi/", ""},
		{"a sed-style pattern", "/go/", chatLineText, "/go/", ""},
		{"a lone slash", "/", chatLineText, "/", ""},
		{"a double slash alone", "//", chatLineText, "//", ""},
		{"a double-slash path", "//tmp/x", chatLineText, "//tmp/x", ""},
		{"a slash, then a space", "/ hello", chatLineText, "/ hello", ""},
		{"an ordinary sentence", "explain internal/agent", chatLineText, "explain internal/agent", ""},
		{"a bare command word", "quit", chatLineText, "quit", ""},
		{"leading whitespace, still a path", "   /tmp/x.gif", chatLineText, "   /tmp/x.gif", ""},

		// COMMANDS — a word the carrier accepts, or a word shaped like one.
		{"a real command", "/quit", chatLineCommand, "/quit", "quit"},
		{"a real command with an argument", "/task list", chatLineCommand, "/task list", "task"},
		{"a real command, mixed case", "/QUIT", chatLineCommand, "/QUIT", "quit"},
		{"a command-shaped word the carrier lacks", "/nonexistent", chatLineUnknown, "", "nonexistent"},
		{"an alias", "/t2i a cat", chatLineCommand, "/t2i a cat", "t2i"},

		// THE LITERAL-SEND ESCAPE — the one rewrite the classifier makes.
		{"the escape", "//quit", chatLineEscape, "/quit", "quit"},
		{"the escape, with a tail", "//nonexistent --flag", chatLineEscape, "/nonexistent --flag", "nonexistent"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyChatLine(tc.line, chatReplWordKnown)
			if got.kind != tc.kind {
				t.Fatalf("classifyChatLine(%q) kind = %v, want %v", tc.line, got.kind, tc.kind)
			}
			if tc.kind == chatLineText || tc.kind == chatLineEscape {
				if got.text != tc.text {
					t.Errorf("classifyChatLine(%q) text = %q, want %q", tc.line, got.text, tc.text)
				}
			}
			if tc.word != "" && got.word != tc.word {
				t.Errorf("classifyChatLine(%q) word = %q, want %q", tc.line, got.word, tc.word)
			}
		})
	}
}

// TestClassifyChatLineVocabularyIsPerCarrier: the SHAPE rule is shared, the
// VOCABULARY is not. `/quit` ends the chat REPL and names a word the shared
// table does not have (the web carrier has no session to end) — so the same line
// classifies as a command for one carrier and as a command it does not accept
// for the other. That is the honest difference the per-carrier predicate exists
// to express.
func TestClassifyChatLineVocabularyIsPerCarrier(t *testing.T) {
	repl := classifyChatLine("/quit", chatReplWordKnown)
	if repl.kind != chatLineCommand {
		t.Errorf("the REPL must accept /quit, got kind %v", repl.kind)
	}
	web := classifyChatLine("/quit", sharedSlashWordKnown)
	if web.kind != chatLineUnknown {
		t.Errorf("the shared table must not accept /quit (no session to end there), got kind %v", web.kind)
	}
	// The shared table's own words are commands for BOTH.
	for _, line := range []string{"/task list", "/help", "/log last", "/new"} {
		if got := classifyChatLine(line, sharedSlashWordKnown); got.kind != chatLineCommand {
			t.Errorf("%s: the shared table accepts it, got kind %v", line, got.kind)
		}
		if got := classifyChatLine(line, chatReplWordKnown); got.kind != chatLineCommand {
			t.Errorf("%s: the REPL accepts the shared table's words, got kind %v", line, got.kind)
		}
	}
}

// TestChatUnknownCommandRefusalNamesTheEscapeChannel: the refusal is the ONE
// place a user learns their line was read as a command, so it must name the
// channel that would send it literally — rendered from the same constant the
// classifier implements (chatSlashEscape), never a second spelling.
func TestChatUnknownCommandRefusalNamesTheEscapeChannel(t *testing.T) {
	err := chatUnknownCommandError("nonexistent")
	msg := err.Error()
	if !strings.Contains(msg, "unknown command") {
		t.Fatalf("the refusal no longer names the class:\n%s", msg)
	}
	if !strings.Contains(msg, chatSlashEscape+"nonexistent") {
		t.Errorf("the refusal does not teach the literal-send channel (%s%s):\n%s", chatSlashEscape, "nonexistent", msg)
	}
	// The channel it names is REAL: the classifier sends exactly that spelling.
	escaped := classifyChatLine(chatSlashEscape+"nonexistent", chatReplWordKnown)
	if escaped.kind != chatLineEscape || escaped.text != "/nonexistent" {
		t.Errorf("the refusal teaches %s%s, which the classifier does not honour (kind %v, text %q)",
			chatSlashEscape, "nonexistent", escaped.kind, escaped.text)
	}
	// And the vocabulary it lists is unchanged (the epic's no-regression limb).
	if !strings.Contains(msg, "/quit") || !strings.Contains(msg, "/help") {
		t.Errorf("the refusal's vocabulary lost its REPL-native words:\n%s", msg)
	}
}

// --- carrier 1: the chat REPL ------------------------------------------------

// TestReplCarrierSendsAPathToTheModel is the REPL's half of the four-carrier
// agreement, driven through the REAL runChatWithInput against a mock model
// (runChatBoundaryTurn — the production path minus the terminal).
func TestReplCarrierSendsAPathToTheModel(t *testing.T) {
	h := runChatBoundaryTurn(t, "/tmp/CN-result.gif", false)

	reqs := h.requests()
	if len(reqs) != 1 {
		t.Fatalf("the turn made %d model requests, want 1 — the path never left the console", len(reqs))
	}
	if texts := classifierWireUserTexts(t, reqs[0]); !classifierAnyContains(texts, "/tmp/CN-result.gif") {
		t.Errorf("the model was asked %v — the pasted path did not arrive as the message", texts)
	}
}

// TestReplCarrierEscapeSendsTheCommandShapedLineLiterally is the escape at work
// on the carrier whose vocabulary is richest: "//quit" is NOT a command (the
// session does not end), and the model receives "/quit" — one slash, verbatim.
func TestReplCarrierEscapeSendsTheCommandShapedLineLiterally(t *testing.T) {
	h := runChatBoundaryTurn(t, "//quit", false)

	reqs := h.requests()
	if len(reqs) != 1 {
		t.Fatalf("the turn made %d model requests, want 1 — the escaped line did not reach the model", len(reqs))
	}
	if texts := classifierWireUserTexts(t, reqs[0]); !classifierAnyContains(texts, "/quit") {
		t.Errorf("the model was asked %v — the literal-send channel lost the slash", texts)
	}
}

// TestReplCarrierStillRefusesAnUnknownCommand pins the no-regression limb: a
// command-SHAPED word the carrier lacks is refused and NEVER sent to the model
// (a typo must not silently become a question).
func TestReplCarrierStillRefusesAnUnknownCommand(t *testing.T) {
	h := runChatBoundaryTurn(t, "/nonexistent", false)

	if reqs := h.requests(); len(reqs) != 0 {
		t.Errorf("an unknown command produced %d model request(s), want 0 — it was sent instead of refused", len(reqs))
	}
}

// --- carriers 2 and 3: the two HTTP surfaces ---------------------------------

// TestHTTPCarriersAgreeWithTheClassifier drives the SAME input through the web
// handler and the terminal handler, and asserts both (a) route it to the model
// rather than to the command table and (b) hand the model the line verbatim.
func TestHTTPCarriersAgreeWithTheClassifier(t *testing.T) {
	const path = "/tmp/CN-result.gif"

	t.Run("web", func(t *testing.T) {
		ws, hits := classifierWebServer(t)
		body := postChat(t, ws, path, nil)
		if strings.Contains(body, "event: command") {
			t.Errorf("the browser carrier answered a pasted path as a command:\n%s", body)
		}
		if len(*hits) != 1 {
			t.Fatalf("the model was called %d time(s), want 1 — the path never left the carrier", len(*hits))
		}
		if texts := classifierWireUserTexts(t, (*hits)[0]); !classifierAnyContains(texts, path) {
			t.Errorf("the model was asked %v — the path did not arrive verbatim", texts)
		}
	})

	t.Run("term", func(t *testing.T) {
		ws, hits := classifierTermServer(t)
		rec := newStreamRecorder()
		ws.handleTermChat(rec, termFormRequest(t, path))
		if strings.Contains(rec.body(), termTurnFailedLine) {
			t.Errorf("the text-browser carrier failed a pasted path:\n%s", rec.body())
		}
		if len(*hits) != 1 {
			t.Fatalf("the model was called %d time(s), want 1 — the path never left the carrier", len(*hits))
		}
		if texts := classifierWireUserTexts(t, (*hits)[0]); !classifierAnyContains(texts, path) {
			t.Errorf("the model was asked %v — the path did not arrive verbatim", texts)
		}
	})
}

// TestHTTPCarriersStillDispatchARealCommand: the other direction of the same
// agreement — a line the table DOES accept is still a local command, and the
// model is never called for it.
func TestHTTPCarriersStillDispatchARealCommand(t *testing.T) {
	t.Setenv("GOPI_HOME", t.TempDir())
	t.Setenv("GOPI_MEMORY_DB", t.TempDir()+"/memory.db")

	ws, hits := classifierWebServer(t)
	body := postChat(t, ws, "/help", nil)
	if n := strings.Count(body, "event: command"); n != 1 {
		t.Errorf("got %d command frames for /help, want 1:\n%s", n, body)
	}
	if len(*hits) != 0 {
		t.Errorf("/help reached the model %d time(s), want 0", len(*hits))
	}

	wsT, hitsT := classifierTermServer(t)
	rec := newStreamRecorder()
	wsT.handleTermChat(rec, termFormRequest(t, "/help"))
	if !strings.Contains(rec.body(), "Slash commands") {
		t.Errorf("the text-browser carrier did not print /help's table:\n%s", rec.body())
	}
	if len(*hitsT) != 0 {
		t.Errorf("/help reached the model %d time(s) on the terminal carrier, want 0", len(*hitsT))
	}
}

// TestHTTPCarriersRefuseAnUnknownCommand: a command-shaped word the shared table
// lacks is answered as a refusal frame (web) / a turn-failure line (term), and
// the model is never called.
func TestHTTPCarriersRefuseAnUnknownCommand(t *testing.T) {
	t.Setenv("GOPI_HOME", t.TempDir())
	t.Setenv("GOPI_MEMORY_DB", t.TempDir()+"/memory.db")

	ws, hits := classifierWebServer(t)
	body := postChat(t, ws, "/nonexistent", nil)
	if !strings.Contains(body, "unknown command") {
		t.Errorf("the browser carrier did not refuse the unknown command:\n%s", body)
	}
	if len(*hits) != 0 {
		t.Errorf("an unknown command reached the model %d time(s), want 0", len(*hits))
	}

	wsT, hitsT := classifierTermServer(t)
	rec := newStreamRecorder()
	wsT.handleTermChat(rec, termFormRequest(t, "/nonexistent"))
	if !strings.Contains(rec.body(), "unknown command") {
		t.Errorf("the text-browser carrier did not refuse the unknown command:\n%s", rec.body())
	}
	if len(*hitsT) != 0 {
		t.Errorf("an unknown command reached the model %d time(s) on the terminal carrier, want 0", len(*hitsT))
	}
}

// --- harness -----------------------------------------------------------------

// classifierWebServer builds the browser carrier with a mock model that records
// every request body it is asked.
func classifierWebServer(t *testing.T) (*webServer, *[][]byte) {
	t.Helper()
	ws := newPlainWebServer(t)
	hits := &[][]byte{}
	classifierRecordModel(t, ws, hits)
	return ws, hits
}

// classifierTermServer builds the text-browser carrier the same way (its own
// handler, its own mock).
func classifierTermServer(t *testing.T) (*webServer, *[][]byte) {
	t.Helper()
	ws, mock := termServerForTest(t, false, nil)
	hits := &[][]byte{}
	// termServerForTest already installed a mock; replace its client with a
	// recording one so the assertion is about what THIS run sent.
	_ = mock
	classifierRecordModel(t, ws, hits)
	return ws, hits
}

// classifierRecordModel points the server's agent at a recording mock model.
func classifierRecordModel(t *testing.T, ws *webServer, hits *[][]byte) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body) //stylegate:ignore bounded read of a test request body; a short read is acceptable
		*hits = append(*hits, body)
		textSSE(w, "the mock answer")
	}))
	t.Cleanup(srv.Close)
	ws.agent.Client = &deepseek.Client{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
	ws.agent.Logger = log.New(io.Discard, "", 0)
}

// classifierWireUserTexts reads the `user` message texts out of one recorded
// request. A claim about what was SENT is made about the message list, never
// about the body as a whole: the body carries gopi's SYSTEM prompt, which
// documents this very surface, so a substring search would match text the user
// never typed (the trap memory #5868 records).
// classifierWireUserTexts reads the `user` message texts out of one recorded
// request, where `raw` is either the body bytes or the body's JSON string (the
// in-process web/typesuite harnesses hand over one or the other; MockLLM's
// RequestBody also accepts a 1-based request NUMBER, which is never valid JSON
// and is therefore never mistaken for a body — a silent misread would make the
// assertion vacuous).
func classifierWireUserTexts(t *testing.T, raw any) []string {
	t.Helper()
	var body []byte
	switch v := raw.(type) {
	case []byte:
		body = v
	case string:
		body = []byte(v)
	default:
		t.Fatalf("classifierWireUserTexts: want a request body (bytes or string), got %T", raw)
	}
	var req struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("the recorded request is not the JSON this test expects: %v", err)
	}
	var texts []string
	for _, m := range req.Messages {
		if m.Role != "user" {
			continue
		}
		var s string
		if err := json.Unmarshal(m.Content, &s); err == nil {
			texts = append(texts, s)
			continue
		}
		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(m.Content, &blocks); err != nil {
			continue
		}
		var joined []string
		for _, b := range blocks {
			joined = append(joined, b.Text)
		}
		texts = append(texts, strings.Join(joined, "\n"))
	}
	if len(texts) == 0 {
		t.Fatalf("the recorded request carries no user message: %s", string(body))
	}
	return texts
}

// classifierAnyContains reports whether any of the model's user messages carries
// the text — the message may be framed (the boundary's own framing), so the
// assertion is containment on a MESSAGE, not equality of the whole request.
func classifierAnyContains(texts []string, want string) bool {
	for _, s := range texts {
		if strings.Contains(s, want) {
			return true
		}
	}
	return false
}

// keep the agent import used on every build path (the harness above builds one).
var _ = agent.Agent{}
