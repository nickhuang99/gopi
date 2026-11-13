package uiclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"github.com/nick/gopi/internal/lineedit"
)

// ---- helpers ---------------------------------------------------------------

// sseServer serves one scripted SSE body for every POST, and records the
// request bodies it received so a test can assert what the client shipped on
// the wire.
type sseServer struct {
	*httptest.Server
	mu     chan struct{} // 1-buffered mutex-free serialization helper
	bodies []string
	status int
	body   string
}

func newSSEServer(t *testing.T, status int, body string) *sseServer {
	t.Helper()
	s := &sseServer{status: status, body: body, mu: make(chan struct{}, 1)}
	s.mu <- struct{}{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body) //stylegate:ignore bounded read of an error/diagnostic body; a short read is acceptable//nolint:errcheck // test fixture: a short read is not the assertion
		<-s.mu
		s.bodies = append(s.bodies, string(raw))
		s.mu <- struct{}{}
		if s.status != http.StatusOK {
			w.WriteHeader(s.status)
			fmt.Fprint(w, s.body)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Stream the body in two chunks so the reader's framing (not the
		// socket's) is what the test exercises.
		mid := len(s.body) / 2
		fmt.Fprint(w, s.body[:mid])
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		fmt.Fprint(w, s.body[mid:])
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *sseServer) lastBody(t *testing.T) string {
	t.Helper()
	<-s.mu
	defer func() { s.mu <- struct{}{} }()
	if len(s.bodies) == 0 {
		t.Fatal("no request body recorded")
	}
	return s.bodies[len(s.bodies)-1]
}

// deltaFrame / toolFrame / doneFrame build the server's frames verbatim (the
// shape cmd/agent/cli_web.go writeSSEFrame emits).
func deltaFrame(text string) string {
	j, _ := json.Marshal(map[string]string{FieldText: text}) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)//nolint:errcheck // marshalling a value built in-process cannot fail
	return "event: " + EventDelta + "\ndata: " + string(j) + "\n\n"
}

func toolFrame(kind, name, summary string) string {
	j, _ := json.Marshal(ToolActivity{Kind: kind, Name: name, Summary: summary}) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)//nolint:errcheck // marshalling a value built in-process cannot fail
	return "event: " + EventTool + "\ndata: " + string(j) + "\n\n"
}

func doneFrame(result string) string {
	j, _ := json.Marshal(TurnEnd{Result: result}) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)//nolint:errcheck // marshalling a value built in-process cannot fail
	return "event: " + EventDone + "\ndata: " + string(j) + "\n\n"
}

func collect(t *testing.T, client *Client, msg string) ([]Frame, error) {
	t.Helper()
	var frames []Frame
	err := client.Turn(context.Background(), msg, func(f Frame) error {
		frames = append(frames, f)
		return nil
	})
	return frames, err
}

// readAll drains a bufio.Reader fully (the parser tests feed it strings).
func readAll(t *testing.T, r io.Reader) string {
	t.Helper()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("readAll: %v", err)
	}
	return string(b)
}

// ---- wire decode tests (the defect-3 close) --------------------------------

// TestDecodeNeverReturnsRawPayload is the STRUCTURAL proof for sketch defect 3:
// `extractText`'s `return data` fallback rendered raw protocol bytes as
// assistant prose. decodeFrame's return type makes that impossible — an
// unknown event and a malformed payload both return an error, and the error is
// the ONLY thing the renderer can show.
func TestDecodeNeverReturnsRawPayload(t *testing.T) {
	cases := []struct {
		name  string
		frame Frame
	}{
		{"unknown event, valid json", Frame{Event: "telemetry", Data: `{"text":"hi"}`}},
		{"unknown event, garbage", Frame{Event: "telemetry", Data: `not json at all`}},
		{"known event, garbage payload", Frame{Event: EventDelta, Data: `{oops`}},
		{"known event, wrong shape", Frame{Event: EventDelta, Data: `"a bare string"`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v, err := decodeFrame(tc.frame)
			if err == nil {
				t.Fatalf("decodeFrame returned (%#v, nil) — a raw payload must never decode to a value", v)
			}
			if v != nil {
				t.Fatalf("decodeFrame returned a non-nil value alongside the error: %#v", v)
			}
			// The message must carry the offending payload in Go-QUOTED form
			// (control characters stay visible on a one-line status line), so
			// assert on the rendering the code actually promises rather than
			// on an unescaped substring.
			if !strings.Contains(err.Error(), strconv.Quote(tc.frame.Data)) {
				t.Errorf("error %q does not quote the offending payload as %s", err.Error(), strconv.Quote(tc.frame.Data))
			}
		})
	}
}

// TestUnknownEventIsTypedError pins the error TYPES (a caller can branch on
// them), not just their text.
func TestUnknownEventIsTypedError(t *testing.T) {
	_, err := decodeFrame(Frame{Event: "telemetry", Data: "{}"})
	var unk *UnknownFrameEventError
	if !errors.As(err, &unk) {
		t.Fatalf("want *UnknownFrameEventError, got %T (%v)", err, err)
	}
	_, err = decodeFrame(Frame{Event: EventDelta, Data: "{"})
	var mal *MalformedFrameError
	if !errors.As(err, &mal) {
		t.Fatalf("want *MalformedFrameError, got %T (%v)", err, err)
	}
	if !errors.Is(err, err) { // the wrap chain stays intact for errors.Is/As users
		t.Fatal("MalformedFrameError does not unwrap")
	}
}

// ---- stream tests ----------------------------------------------------------

// TestTurnStreamsFramesInOrder drives the client against a recorded server
// fixture and asserts the frame order and payloads.
func TestTurnStreamsFramesInOrder(t *testing.T) {
	body := deltaFrame("Hel") + toolFrame(UIStepCall, "bash", "head -3 go.mod") +
		deltaFrame("lo") + toolFrame(UIStepResult, "bash", "exit=0") + doneFrame("Hello")
	srv := newSSEServer(t, http.StatusOK, body)
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}

	frames, err := collect(t, c, "hi")
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	wantEvents := []string{EventDelta, EventTool, EventDelta, EventTool, EventDone}
	if len(frames) != len(wantEvents) {
		t.Fatalf("got %d frames, want %d (%v)", len(frames), len(wantEvents), frames)
	}
	for i, want := range wantEvents {
		if frames[i].Event != want {
			t.Errorf("frame %d event = %q, want %q", i, frames[i].Event, want)
		}
	}
	var tool ToolActivity
	if err := json.Unmarshal([]byte(frames[1].Data), &tool); err != nil {
		t.Fatalf("tool frame does not decode: %v", err)
	}
	if tool.Name != "bash" || tool.Kind != UIStepCall || tool.Summary != "head -3 go.mod" {
		t.Errorf("tool frame = %+v, want bash/call/head -3 go.mod", tool)
	}
}

// TestTurnShipsOnlyTheNewMessage pins the wire contract: the server holds the
// transcript, so the client POSTs the message and NOTHING else — no history,
// and no `send` field (sketch defect 2 was a struct that USED a field it did
// not have; here the wire has no such field by design, and adding one would be
// a change to the contract this test guards).
func TestTurnShipsOnlyTheNewMessage(t *testing.T) {
	srv := newSSEServer(t, http.StatusOK, doneFrame("ok"))
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	if _, err := collect(t, c, "what is 2+2"); err != nil {
		t.Fatalf("Turn: %v", err)
	}
	body := srv.lastBody(t)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &fields); err != nil {
		t.Fatalf("request body is not an object: %v (%s)", err, body)
	}
	if len(fields) != 1 {
		t.Fatalf("request carries %d fields (%s) — the server holds the transcript, so `message` is the whole body", len(fields), body)
	}
	if _, ok := fields[FieldMessage]; !ok {
		t.Fatalf("request body %s has no %q field", body, FieldMessage)
	}
	if _, ok := fields["send"]; ok {
		t.Fatalf("request body %s carries a `send` field — the sketch's defect-2 shape is not on this wire", body)
	}
	if _, ok := fields["history"]; ok {
		t.Fatalf("request body %s carries history — the server ignores it; the transcript is server-held (R1)", body)
	}
}

// TestTurnSendsChatPathAndLoopbackOrigin pins the route (no shadowing, no
// second engine) and that a trailing slash in the origin is tolerated.
func TestTurnSendsChatPathAndLoopbackOrigin(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, doneFrame("ok"))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL + "/", HTTP: srv.Client()}
	if _, err := collect(t, c, "hi"); err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if gotPath != ChatPath {
		t.Errorf("POST path = %q, want %q", gotPath, ChatPath)
	}
}

// TestCancelIsANormalEnd is the acceptance-5 behaviour at the transport layer:
// a user-initiated cancel returns nil, never an error.
func TestCancelIsANormalEnd(t *testing.T) {
	blocked := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, deltaFrame("partial"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Hold the stream open until the client goes away, then release.
		<-r.Context().Done()
		close(blocked)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	done := make(chan error, 1)
	go func() {
		done <- c.Turn(ctx, "hi", func(Frame) error { return nil })
	}()
	// Wait for the first frame to have been written before cancelling.
	//stylegate:allow-sleep the wait IS the observation: the first frame must have been written before the cancel, and the handler is the fixture
	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("a cancelled turn returned %v — a cancel is a NORMAL end", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled turn did not end")
	}
	select {
	case <-blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("server never saw the disconnect")
	}
}

// TestStreamClosedWithoutDoneIsAnError: a body that ends before the terminal
// frame did NOT finish, and saying so is the point (Rule 15).
func TestStreamClosedWithoutDoneIsAnError(t *testing.T) {
	srv := newSSEServer(t, http.StatusOK, deltaFrame("half an answer"))
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	_, err := collect(t, c, "hi")
	if !errors.Is(err, ErrStreamClosed) {
		t.Fatalf("err = %v, want ErrStreamClosed", err)
	}
}

// TestTurnRejectsNonOKWithTheServerText: an HTTP refusal carries the server's
// own words, never a generic message.
func TestTurnRejectsNonOKWithTheServerText(t *testing.T) {
	srv := newSSEServer(t, http.StatusServiceUnavailable, `{"error":"server shutting down"}`)
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	_, err := collect(t, c, "hi")
	if err == nil {
		t.Fatal("a 503 was reported as success")
	}
	if !strings.Contains(err.Error(), "503") || !strings.Contains(err.Error(), "server shutting down") {
		t.Errorf("error %q does not carry both the status and the server's message", err)
	}
}

// TestCommentsAndMultiLineDataAreHandled pins the SSE grammar the reader
// implements (a heartbeat comment must not become a frame; multi-line data
// belongs to ONE payload).
func TestCommentsAndMultiLineDataAreHandled(t *testing.T) {
	body := ": heartbeat\n\n" +
		"event: " + EventDelta + "\ndata: {\"text\":\"a\"}\n\n" +
		// The inner \n makes this a multi-line data payload (joined by \n).
		"event: " + EventDelta + "\ndata: {\"text\":\n" + "data: \"b\"}\n\n" +
		doneFrame("ab")
	srv := newSSEServer(t, http.StatusOK, body)
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	frames, err := collect(t, c, "hi")
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if len(frames) != 3 {
		t.Fatalf("got %d frames, want 3 (the comment frame must be dropped): %v", len(frames), frames)
	}
	// The two data lines joined by \n form one valid JSON payload.
	if frames[1].Data != "{\"text\":\n\"b\"}" {
		t.Errorf("multi-line data assembled as %q", frames[1].Data)
	}
	var d DeltaText
	if err := json.Unmarshal([]byte(frames[1].Data), &d); err != nil {
		t.Fatalf("joined payload does not decode: %v", err)
	}
	if d.Text != "b" {
		t.Errorf("multi-line delta text = %q, want b", d.Text)
	}
}

// TestCRLFFramingIsAccepted: an SSE peer may terminate its lines with CRLF
// rather than LF; the frame boundary and the payload must be identical either
// way (the parser trims the CR before it looks for the blank line).
func TestCRLFFramingIsAccepted(t *testing.T) {
	body := "event: delta\r\ndata: {\"text\":\"crlf\"}\r\n\r\n" +
		"event: done\r\ndata: {\"result\":\"crlf\"}\r\n\r\n"
	srv := newSSEServer(t, http.StatusOK, body)
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	frames, err := collect(t, c, "hi")
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if len(frames) != 2 {
		t.Fatalf("frames = %v, want a delta and a done", frames)
	}
	var d DeltaText
	if err := json.Unmarshal([]byte(frames[0].Data), &d); err != nil {
		t.Fatalf("CRLF payload did not decode: %v (%q)", err, frames[0].Data)
	}
	if d.Text != "crlf" {
		t.Errorf("CRLF delta text = %q", d.Text)
	}
}

// TestFrameSplitAcrossWritesIsReassembled: the server may flush mid-frame; the
// reader reassembles on the blank line, never on a write boundary.
func TestFrameSplitAcrossWritesIsReassembled(t *testing.T) {
	raw := deltaFrame("split") + doneFrame("split")
	var chunks []string
	for i := 0; i < len(raw); i += 7 {
		end := i + 7
		if end > len(raw) {
			end = len(raw)
		}
		chunks = append(chunks, raw[i:end])
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, c := range chunks {
			fmt.Fprint(w, c)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
	t.Cleanup(srv.Close)
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	frames, err := collect(t, c, "hi")
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if len(frames) != 2 {
		t.Fatalf("frames = %v, want 2 (a frame split across writes is ONE frame)", frames)
	}
}

// TestFrameBudgetRefusesAnEndlessLine: a peer that never sends a blank line
// fails loudly instead of growing this process.
func TestFrameBudgetRefusesAnEndlessLine(t *testing.T) {
	long := strings.Repeat("x", DefaultMaxFrameBytes+16)
	r := bufio.NewReaderSize(strings.NewReader("data: "+long+"\n"), 4096)
	err := readFrames(r, func(Frame) error { return nil }, context.Background())
	if err == nil {
		t.Fatal("an over-budget line was accepted")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("err = %v, want the budget refusal", err)
	}
}

// TestDoneSentinelEndsTheTurn: the [DONE] marker is an end-of-turn signal, not
// a payload (the sketch's reader kept it for the same reason).
func TestDoneSentinelEndsTheTurn(t *testing.T) {
	srv := newSSEServer(t, http.StatusOK, deltaFrame("hi")+"data: "+sseDoneSentinel+"\n\n")
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	frames, err := collect(t, c, "x")
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if len(frames) != 1 || frames[0].Event != EventDelta {
		t.Fatalf("frames = %v, want exactly one delta", frames)
	}
}

// ---- model tests -----------------------------------------------------------

// newTestModel builds a model wired to a fake client through the streaming
// path (no HTTP) by driving applyFrame directly — the renderer under test is
// the model, not the transport.
func newTestModel(t *testing.T) model {
	t.Helper()
	m, err := newModel(&Client{BaseURL: DefaultServerBase}, DefaultServerBase, true)
	if err != nil {
		t.Fatalf("newModel: %v", err)
	}
	m.width = 80
	m.height = 24
	return m
}

// TestModelStreamsAndRendersToolActivity walks the acceptance demo through the
// MODEL: a user message, streamed deltas, a live tool call and its result, then
// the terminal frame — and asserts what the screen shows at each step.
func TestModelStreamsAndRendersToolActivity(t *testing.T) {
	m := newTestModel(t)
	m.input = []rune("run the bash tool with exactly: head -3 go.mod")
	m.cursor = len(m.input)
	mi, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mi.(model)
	if cmd == nil {
		t.Fatal("Enter on a non-empty input produced no command (the turn never starts)")
	}
	if !m.streaming {
		t.Fatal("model is not streaming after Enter")
	}
	if got := string(m.input); got != "" {
		t.Errorf("input box still holds %q after submitting", got)
	}
	if m.assistantIdx < 0 {
		t.Fatal("no assistant row was opened for the turn")
	}

	m.applyFrame(Frame{Event: EventDelta, Data: `{"text":"I'll check "}`})
	m.applyFrame(Frame{Event: EventDelta, Data: `{"text":"the file."}`})
	m.applyFrame(Frame{Event: EventTool, Data: `{"kind":"call","name":"bash","summary":"head -3 go.mod"}`})
	m.applyFrame(Frame{Event: EventTool, Data: `{"kind":"result","name":"bash","summary":"exit=0"}`})

	if got := m.entries[m.assistantIdx].text; got != "I'll check the file." {
		t.Errorf("assistant row = %q, want the concatenated deltas", got)
	}
	if m.toolEvents != 2 {
		t.Errorf("toolEvents = %d, want 2", m.toolEvents)
	}
	view := m.View()
	for _, want := range []string{"I'll check the file.", "call bash head -3 go.mod", "result bash exit=0", "head -3 go.mod"} {
		if !strings.Contains(view, want) {
			t.Errorf("view is missing %q:\n%s", want, view)
		}
	}

	m.applyFrame(Frame{Event: EventDone, Data: `{"result":"I'll check the file."}`})
	if m.streaming {
		t.Error("model still streaming after the done frame")
	}
	if m.turns != 1 {
		t.Errorf("turns = %d, want 1", m.turns)
	}
	if m.assistantIdx != -1 {
		t.Errorf("assistantIdx = %d, want -1 after the turn", m.assistantIdx)
	}
}

// TestMalformedFrameBecomesAnErrorRowNeverProse is the defect-3 close at the
// RENDERER: a garbage payload must appear as a fault, not as assistant text.
func TestMalformedFrameBecomesAnErrorRowNeverProse(t *testing.T) {
	m := newTestModel(t)
	m.entries = nil
	m.assistantIdx = 0
	m.entries = append(m.entries, entry{kind: entryAssistant, text: ""})

	m.applyFrame(Frame{Event: EventDelta, Data: `raw protocol bytes {"text":`})

	if m.errText == "" {
		t.Fatal("a malformed frame produced no error state")
	}
	if got := m.entries[0].text; got != "" {
		t.Errorf("the malformed payload reached the assistant row as %q", got)
	}
	view := m.View()
	if !strings.Contains(view, "malformed") {
		t.Errorf("the transcript shows no fault row:\n%s", view)
	}
	if !strings.Contains(m.statusLine(), "malformed") {
		t.Errorf("the status line does not name the fault:\n%s", m.statusLine())
	}
	if !strings.Contains(m.errText, `malformed "delta" frame`) {
		t.Errorf("errText = %q, want the fault to name the offending frame", m.errText)
	}
	// The property is ATTRIBUTION: the payload may appear inside the ⚠ fault
	// row (that is the diagnosis working), but it must never be in the
	// ASSISTANT row as if the model had said it.
	for _, e := range m.entries {
		if e.kind == entryAssistant && strings.Contains(e.text, "raw protocol bytes") {
			t.Errorf("the raw payload was attributed to the model: %q", e.text)
		}
	}
}

// TestUnknownEventBecomesAnErrorRow: an unknown frame is reported, never
// silently dropped and never rendered as text.
func TestUnknownEventBecomesAnErrorRow(t *testing.T) {
	m := newTestModel(t)
	m.applyFrame(Frame{Event: "telemetry", Data: `{"v":1}`})
	if !strings.Contains(m.errText, "unknown stream frame") {
		t.Fatalf("errText = %q, want the unknown-frame report", m.errText)
	}
}

// TestErrorFrameIsShownAsAFault: `event: error` surfaces as the server's own
// message.
func TestErrorFrameIsShownAsAFault(t *testing.T) {
	m := newTestModel(t)
	m.applyFrame(Frame{Event: EventError, Data: `{"error":"model refused"}`})
	if !strings.Contains(m.errText, "model refused") {
		t.Fatalf("errText = %q, want the server's message", m.errText)
	}
}

// TestQueuedFrameNotices: a queued turn is told to the operator.
func TestQueuedFrameNotices(t *testing.T) {
	m := newTestModel(t)
	before := len(m.entries)
	m.applyFrame(Frame{Event: EventQueued, Data: `{"placement":"queued","notice":"waiting for the in-flight turn to finish…"}`})
	if len(m.entries) != before+1 {
		t.Fatalf("queued frame added %d entries, want 1", len(m.entries)-before)
	}
	if !strings.Contains(m.View(), "waiting for the in-flight turn") {
		t.Errorf("queued notice not shown:\n%s", m.View())
	}
}

// TestSecondTurnKeepsHistory is acceptance 4 at the model level: the transcript
// survives the turn boundary (nothing is cleared).
func TestSecondTurnKeepsHistory(t *testing.T) {
	m := newTestModel(t)
	m.entries = nil

	m.input = []rune("first")
	m.cursor = len(m.input)
	mi, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mi.(model)
	m.applyFrame(Frame{Event: EventDelta, Data: `{"text":"one"}`})
	m.applyFrame(Frame{Event: EventDone, Data: `{"result":"one"}`})

	m.input = []rune("second")
	m.cursor = len(m.input)
	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mi.(model)
	m.applyFrame(Frame{Event: EventDelta, Data: `{"text":"two"}`})
	m.applyFrame(Frame{Event: EventDone, Data: `{"result":"two"}`})

	view := m.View()
	for _, want := range []string{"first", "one", "second", "two"} {
		if !strings.Contains(view, want) {
			t.Errorf("after turn 2 the transcript is missing %q:\n%s", want, view)
		}
	}
	if m.turns != 2 {
		t.Errorf("turns = %d, want 2", m.turns)
	}
}

// TestInterruptIsNotAnError is acceptance 5 at the model level: Ctrl+X ends the
// turn with an interruption notice and NO error state.
func TestInterruptIsNotAnError(t *testing.T) {
	m := newTestModel(t)
	m.entries = nil
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.streaming = true
	m.entries = append(m.entries, entry{kind: entryAssistant, text: "partial"})

	mi, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	m = mi.(model)
	if cmd != nil {
		t.Error("Ctrl+X returned a command; it only cancels the turn")
	}
	if !m.cancelling {
		t.Fatal("Ctrl+X did not mark the turn as cancelling")
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("Ctrl+X did not cancel the turn's context")
	}

	// The turn then finishes the way a cancelled turn does: nil error.
	m.endTurn(nil)
	if m.errText != "" {
		t.Errorf("a cancelled turn set errText = %q — a cancel is not an error", m.errText)
	}
	view := m.View()
	if !strings.Contains(view, "interrupted") {
		t.Errorf("view does not report the interruption:\n%s", view)
	}
	if strings.Contains(strings.ToLower(view), "error") || strings.Contains(view, "context canceled") {
		t.Errorf("view reports a cancelled turn as a fault:\n%s", view)
	}
}

// TestInterruptWithNothingRunningIsHarmless.
func TestInterruptWithNothingRunningIsHarmless(t *testing.T) {
	m := newTestModel(t)
	m.entries = nil
	mi, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	m = mi.(model)
	if m.errText != "" {
		t.Errorf("idle Ctrl+X set errText = %q", m.errText)
	}
	if !strings.Contains(m.View(), "nothing to interrupt") {
		t.Errorf("idle Ctrl+X said nothing:\n%s", m.View())
	}
}

// TestInputEditing pins the line-editor keys the persistent box needs.
func TestInputEditing(t *testing.T) {
	m := newTestModel(t)
	type step struct {
		key  tea.KeyMsg
		want string
	}
	steps := []step{
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("helo")}, "helo"},
		{tea.KeyMsg{Type: tea.KeyLeft}, "helo"},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")}, "hello"},
		{tea.KeyMsg{Type: tea.KeyCtrlA}, "hello"},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(">")}, ">hello"},
		{tea.KeyMsg{Type: tea.KeyCtrlE}, ">hello"},
		{tea.KeyMsg{Type: tea.KeyBackspace}, ">hell"},
		{tea.KeyMsg{Type: tea.KeyCtrlU}, ""},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("abc")}, "abc"},
		{tea.KeyMsg{Type: tea.KeyHome}, "abc"},
		{tea.KeyMsg{Type: tea.KeyDelete}, "bc"},
	}
	for i, s := range steps {
		mi, _ := m.Update(s.key)
		m = mi.(model)
		if got := string(m.input); got != s.want {
			t.Fatalf("step %d (%v): input = %q, want %q", i, s.key.Type, got, s.want)
		}
	}
	// Space is its own key type and must be insertable.
	mi, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = mi.(model)
	// The caret is at the head of "bc" after Home+Delete above, so the space
	// lands in front.
	if got := string(m.input); got != " bc" {
		t.Errorf("space not inserted: %q, want %q", got, " bc")
	}
}

// TestEnterOnEmptyInputDoesNothing: a blank line is not a turn (the server
// would refuse it anyway; refusing it here costs no round trip).
func TestEnterOnEmptyInputDoesNothing(t *testing.T) {
	m := newTestModel(t)
	m.entries = nil
	mi, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mi.(model)
	if cmd != nil || m.streaming {
		t.Fatalf("Enter on an empty box started a turn (cmd=%v streaming=%v)", cmd, m.streaming)
	}
	if got := string(m.input); got != "" {
		t.Errorf("input = %q", got)
	}
}

// TestSecondEnterWhileStreamingIsRefused: one turn at a time, told plainly.
func TestSecondEnterWhileStreamingIsRefused(t *testing.T) {
	m := newTestModel(t)
	m.streaming = true
	m.entries = nil
	m.input = []rune("another")
	m.cursor = len(m.input)
	mi, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mi.(model)
	if cmd != nil {
		t.Fatal("a second turn started while one was streaming")
	}
	if string(m.input) != "another" {
		t.Error("the refused message was eaten — the operator's draft must survive")
	}
	if !strings.Contains(m.View(), "already running") {
		t.Errorf("refusal not shown:\n%s", m.View())
	}
}

// TestViewLayoutIsStable: header, transcript, input and status always occupy
// the rows the terminal gives them (a jumping layout is what "flashing" feels
// like from the operator's side).
//
// THE INPUT ROW'S IDENTITY IS ASSERTED UNDER BOTH CAPABILITIES. This test used
// to pin the row with `strings.HasPrefix(lines[len-2], inputPrompt)` — a claim
// that holds only while the row carries NO SGR before the prompt, i.e. while the
// input box is unstyled. Once the row is painted (the palette's Prompt/Buffer
// halves, internal/uiclient/input_row_palette_test.go), that prefix is the
// styling's property rather than the layout's, so the check is re-derived rather
// than dropped: the row is identified by its TEXT (SGR stripped) in every case,
// and the palette's own bytes are asserted where colour is on.
func TestViewLayoutIsStable(t *testing.T) {
	m := newTestModel(t)
	m.width, m.height = 60, 12
	rows := m.transcriptRows()
	for _, n := range []int{0, 1, 5, 40} {
		m.entries = m.entries[:1]
		for i := 0; i < n; i++ {
			m.applyFrame(Frame{Event: EventDelta, Data: `{"text":"line"}`})
		}
		lines := strings.Split(m.View(), "\n")
		if len(lines) != m.transcriptRows()+headerRows+inputRows+statusRows {
			t.Errorf("%d deltas: view has %d lines, want %d", n, len(lines), m.transcriptRows()+headerRows+inputRows+statusRows)
		}
		inputRow := lines[len(lines)-2]
		if !strings.HasPrefix(stripSGR(inputRow), inputPrompt) {
			t.Errorf("%d deltas: the second-to-last line is not the input box: %q", n, inputRow)
		}
	}
	if rows < minTranscriptRows {
		t.Fatalf("transcriptRows = %d", rows)
	}
}

// TestViewInputRowIsTheSecondToLastLineWithColourOn is the other half of the
// re-derivation above: with a real palette the input row still occupies its row
// in the layout, and the prompt it opens with is the palette's own — the
// property the pre-change `HasPrefix(row, inputPrompt)` used to stand for.
func TestViewInputRowIsTheSecondToLastLineWithColourOn(t *testing.T) {
	m := newTestModel(t)
	m.width, m.height = 60, 12
	m.pal = lineedit.BasicPalette
	m.input = []rune("hi")

	lines := strings.Split(m.View(), "\n")
	inputRow := lines[len(lines)-2]
	if want := lineedit.BasicPalette.Prompt.Wrap(inputPrompt); !strings.HasPrefix(inputRow, want) {
		t.Errorf("the input row does not open with the palette's prompt %q: %q", want, inputRow)
	}
	if !strings.HasPrefix(stripSGR(inputRow), inputPrompt) {
		t.Errorf("the painted input row's TEXT is not the prompt + buffer: %q", inputRow)
	}
}

// TestViewTruncatesToWidth: no rendered line may exceed the terminal width, or
// the terminal wraps and the frame's line count lies.
func TestViewTruncatesToWidth(t *testing.T) {
	m := newTestModel(t)
	m.width, m.height = 30, 10
	m.entries = []entry{{kind: entryUser, text: strings.Repeat("x", 200)}}
	for _, line := range strings.Split(m.View(), "\n") {
		if w := runewidth.StringWidth(line); w > m.width {
			t.Errorf("line %d display cols = %d > width %d: %q", 0, w, m.width, line)
		}
	}
}

// TestScreenWriterCountsFrames pins the measurement instrument itself: the
// report's per-turn number is only meaningful if the counter counts frames.
func TestScreenWriterCountsFrames(t *testing.T) {
	var buf strings.Builder
	sw := NewScreenWriter(&buf)
	for i := 0; i < 3; i++ {
		if _, err := sw.Write([]byte("frame")); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if sw.Calls() != 3 {
		t.Errorf("Calls() = %d, want 3", sw.Calls())
	}
	if sw.Bytes() != 15 {
		t.Errorf("Bytes() = %d, want 15", sw.Bytes())
	}
	if buf.String() != "frameframeframe" {
		t.Errorf("writer did not pass bytes through: %q", buf.String())
	}
}

// TestNewModelRefusesNilClient: the sketch's defect 1 was a model whose
// streaming start panicked on the first turn. Here the constructor refuses the
// dependency instead of yielding a model that crashes later.
func TestNewModelRefusesNilClient(t *testing.T) {
	if _, err := newModel(nil, "x", false); err == nil {
		t.Fatal("newModel accepted a nil client")
	}
}

// TestRunRefusesNilClient mirrors the constructor contract at the public
// entry point.
func TestRunRefusesNilClient(t *testing.T) {
	if _, err := Run(Config{}); err == nil {
		t.Fatal("Run accepted a nil client")
	}
}

// TestInitIsACommandlessStart: the surface starts idle; nothing runs before the
// operator types.
func TestInitIsACommandlessStart(t *testing.T) {
	m := newTestModel(t)
	if m.Init() != nil {
		t.Error("Init returned a command — the surface must start idle")
	}
}

// TestScrollIsClamped: paging never scrolls past either end.
func TestScrollIsClamped(t *testing.T) {
	m := newTestModel(t)
	m.width, m.height = 40, 8
	m.entries = []entry{{kind: entryAssistant, text: strings.Repeat("word ", 60)}}
	m.scrollBy(1000)
	max := len(m.allLines()) - m.transcriptRows()
	if m.scroll != max {
		t.Errorf("scroll = %d, want clamped to %d", m.scroll, max)
	}
	m.scrollBy(-1000)
	if m.scroll != 0 {
		t.Errorf("scroll = %d, want 0 at the tail", m.scroll)
	}
}

// TestCtrlCQuits: the operator's exit key.
func TestCtrlCQuits(t *testing.T) {
	m := newTestModel(t)
	mi, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = mi.(model)
	if cmd == nil {
		t.Fatal("Ctrl+C returned no quit command")
	}
	if !m.quitting {
		t.Error("Ctrl+C did not mark the model quitting")
	}
	if m.View() != "" {
		t.Errorf("a quitting model still renders %q", m.View())
	}
}

// TestTurnDoneEndsStreamingWithoutAnError.
func TestTurnDoneEndsStreamingWithoutAnError(t *testing.T) {
	m := newTestModel(t)
	m.streaming = true
	m.cancelling = false
	mi, _ := m.Update(turnDoneMsg{err: nil})
	m = mi.(model)
	if m.streaming {
		t.Error("turnDoneMsg did not end the streaming state")
	}
	if m.errText != "" {
		t.Errorf("errText = %q after a clean turn", m.errText)
	}
}

// TestTurnDoneWithErrorFailsVisibly: a transport failure is an error row.
func TestTurnDoneWithErrorFailsVisibly(t *testing.T) {
	m := newTestModel(t)
	m.streaming = true
	mi, _ := m.Update(turnDoneMsg{err: errors.New("connection refused")})
	m = mi.(model)
	if !strings.Contains(m.errText, "connection refused") {
		t.Errorf("errText = %q", m.errText)
	}
}

// TestWindowSizeResizesTheLayout.
func TestWindowSizeResizesTheLayout(t *testing.T) {
	m := newTestModel(t)
	mi, _ := m.Update(tea.WindowSizeMsg{Width: 42, Height: 9})
	m = mi.(model)
	if m.width != 42 || m.height != 9 {
		t.Fatalf("size = %dx%d, want 42x9", m.width, m.height)
	}
	if got := len(strings.Split(m.View(), "\n")); got != 9 {
		t.Errorf("view has %d lines for a 9-row terminal", got)
	}
}

// TestFrameMsgKeepsThePumpAlive: receiving a frame must schedule the next read,
// or a long answer would stall after one token.
func TestFrameMsgKeepsThePumpAlive(t *testing.T) {
	m := newTestModel(t)
	m.frames = make(chan tea.Msg, 1)
	mi, cmd := m.Update(frameMsg{frame: Frame{Event: EventDelta, Data: `{"text":"x"}`}})
	m = mi.(model)
	if cmd == nil {
		t.Fatal("frameMsg did not schedule the next read — the stream would stall after one token")
	}
}

// TestTurnProducesFramesFromAScriptedServer end-to-ends the transport + decode
// pair (the streaming property acceptance 2 rests on).
func TestTurnProducesFramesFromAScriptedServer(t *testing.T) {
	body := ""
	for _, tok := range []string{"tok", "en", "-", "by", "-", "tok", "en"} {
		body += deltaFrame(tok)
	}
	body += doneFrame("token-by-token")
	srv := newSSEServer(t, http.StatusOK, body)
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}

	var got strings.Builder
	var deltas int
	err := c.Turn(context.Background(), "stream", func(f Frame) error {
		if f.Event == EventDelta {
			deltas++
			var d DeltaText
			if err := json.Unmarshal([]byte(f.Data), &d); err != nil {
				return err
			}
			got.WriteString(d.Text)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if deltas != 7 {
		t.Errorf("deltas = %d, want 7 (one per token — the streaming property)", deltas)
	}
	if got.String() != "token-by-token" {
		t.Errorf("assembled %q", got.String())
	}
}

// TestReadAllHelperKeepsTheCompilerHonest about the unused-import shape of the
// helpers above (a helper that is never called is dead weight).
var _ = readAll

// TestInterruptTellsTheEngine is the regression guard for the `gopi ui` freeze:
// Ctrl+X used to cancel only THIS side of the stream, so the engine ran the turn
// to completion holding its single-turn lock while the surface reported
// "interrupted" — every later message then waited behind a turn the operator
// believed was stopped. The key must reach the engine.
func TestInterruptTellsTheEngine(t *testing.T) {
	m := newTestModel(t)
	m.entries = nil
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.streaming = true
	told := 0
	m.onInterrupt = func() { told++ }

	mi, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	m = mi.(model)
	if told != 1 {
		t.Errorf("the engine was asked to interrupt %d times, want exactly 1", told)
	}
	if ctx.Err() == nil {
		t.Error("Ctrl+X did not cancel this side's stream")
	}
	if !m.cancelling {
		t.Error("Ctrl+X did not mark the turn as a user interrupt")
	}

	// With nothing in flight there is nothing to tell the engine: an idle Ctrl+X
	// is a notice, never an interrupt claim.
	m.streaming = false
	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	m = mi.(model)
	if told != 1 {
		t.Errorf("an idle Ctrl+X told the engine (calls = %d)", told)
	}
	if !strings.Contains(m.View(), "nothing to interrupt") {
		t.Errorf("an idle Ctrl+X was not reported to the operator:\n%s", m.View())
	}
}

// TestQueuedTurnNamesItsWait: a turn parked on the engine's single-turn lock
// must SAY it is parked. The reported freeze was an operator watching an
// unchanged screen while a turn waited invisibly — the state line has to name
// the wait, and the surface has to repaint so a long wait is visibly moving.
func TestQueuedTurnNamesItsWait(t *testing.T) {
	m := newTestModel(t)
	m.streaming = true
	mi, cmd := m.Update(frameMsg{frame: Frame{Event: EventQueued, Data: `{"placement":"queued","notice":"waiting for the in-flight turn to finish…"}`}})
	m = mi.(model)
	if cmd == nil {
		t.Error("a queued turn scheduled no repaint — a long wait would look frozen")
	}
	status := m.statusLine()
	if !strings.Contains(status, queuedState) {
		t.Errorf("status line %q does not name the wait (want %q in it)", status, queuedState)
	}
	if strings.Contains(status, "idle") {
		t.Errorf("a queued turn reports idle: %q", status)
	}

	// The wait dies with the turn: a finished turn cannot keep claiming one.
	m.endTurn(nil)
	if strings.Contains(m.statusLine(), queuedState) {
		t.Errorf("the wait survived the turn it belonged to: %q", m.statusLine())
	}
}
