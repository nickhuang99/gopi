package main

// chat_screen_e2e_test.go — the epic's REAL-SESSION acceptance (Rule 31): the
// command typed into the actual REPL, and the ACTUAL request body of the turn
// that follows it.
//
// WHY THIS FILE EXISTS AT ALL, given chat_screen_test.go already covers the
// command's parts. Those cases call chatScreenHandleLine and the boundary
// directly — they prove the pieces behave, not that typing `/screen` in a live
// console produces a reference in a real request. The gap between the two is
// exactly where this feature can fail silently: the word could be un-dispatched,
// the note could be parked on the wrong session object, the injection could land
// on a message the request never carries. So this case drives
// runChatWithInput — the SAME entry the interactive and --once faces call — with
// a cooked stdin holding the two lines an operator would type, and then decodes
// what the model server actually received.
//
// THE ASSERTION IS AN ABSENCE, which is the epic's hard ban: the request body of
// the turn AFTER a capture contains the artifact's absolute path, its byte count
// and how to read it, and contains NO SGR payload byte. An absence is asserted
// against the painted content the fixture really drew, so it cannot pass on an
// empty file.

import (
	"bytes"
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

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
	"github.com/nickhuang99/gopi/internal/uiclient"
)

// chatScreenE2EPainted is the marker the fixture PICKS INTO THE SCREEN before the
// capture, so the absence assertion has real painted bytes to look for. It is a
// literal no part of the injected framing could contain by accident.
const chatScreenE2EPainted = "PAINTED-MARKER-E2E-7d41"

// screenE2EHarness is one driven console: the model requests it produced, in
// order, and the session directory it wrote into.
type screenE2EHarness struct {
	requests func() [][]byte
	sessDir  string
	console  string
	code     int
}

// runScreenE2E drives the REAL chat REPL over the given stdin lines.
//
// The recording is pre-armed at a path INSIDE the session directory and PRE-
// PAINTED before the run starts, because the console under test is a cooked
// reader (there is no renderer to paint the fixture's bytes for us). That is the
// honest shape for this question: `/screen` reads the file the run's stream
// handle names, and a run whose stream already holds frames is the state a real
// console is in at any instant after its first repaint.
func runScreenE2E(t *testing.T, input string) *screenE2EHarness {
	t.Helper()
	return runScreenE2EPlan(t, input, chatScreenE2EPaintOneTurn)
}

// chatScreenE2EPaintPlan paints the frames that arrive with request n (1-based).
// It is a PARAMETER because the two acceptance shapes need different recordings:
// the primer case needs one numbered turn, and the turn-selection case needs TWO
// (so "the bare form takes the NEWEST" has a newest to take, and so a segment that
// runs one turn too long is visible on the bytes).
type chatScreenE2EPaintPlan func(paintFrame func([]byte), request int)

// runScreenE2ETwoTurns runs the same REPL with a recording that grows a SECOND
// numbered turn when the second request arrives.
func runScreenE2ETwoTurns(t *testing.T, input string) *screenE2EHarness {
	t.Helper()
	return runScreenE2EPlan(t, input, chatScreenE2EPaintTwoTurns)
}

// runScreenE2EPlan is the one runner: the recording it hands the run is built by
// paint, call by call, so both fixtures share every other line of setup.
func runScreenE2EPlan(t *testing.T, input string, paint chatScreenE2EPaintPlan) *screenE2EHarness {
	t.Helper()
	h := &screenE2EHarness{}
	prevFactory := chatAgentFactory
	t.Cleanup(func() { chatAgentFactory = prevFactory })

	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	t.Setenv("GOPI_MEMORY_DB", "")

	// PRE-PAINT, AT THE ONLY MOMENT IT CAN HAPPEN. The run OPENS its recording with
	// O_TRUNC (uiclient.OpenSgrStream) a few lines into startup, so a fixture that
	// wrote frames before the call is wiped by the call — measured: the seed this
	// case used to write survived as `bytes=0 frames=0`. A real console is in the
	// same position in reverse: ITS frames are appended by the renderer WHILE the
	// run lives. So the frames are appended here, from inside the first model
	// request's handler — after the stream exists (the request cannot arrive until
	// the REPL is past its startup) and before the line that types `/screen` is
	// read (that line is second in the script). The bytes are the format's own
	// record (`len` \n payload), which is what makes this an honest simulation of
	// the writer rather than a second format.
	// THE PATH IS COMPOSED AFTER THE LOGGER EXISTS (see below), and this variable
	// is the handle the closure reads at CALL time — the handler cannot fire before
	// the assignment, because no request can be made before the run starts.
	var sgrPath string
	// paintFrame appends ONE frame to the live recording, in the format's own
	// record shape (`len` \n payload) — which is what makes this an honest
	// simulation of the writer rather than a second format.
	paintFrame := func(p []byte) {
		f, oerr := os.OpenFile(sgrPath, os.O_WRONLY|os.O_APPEND, 0o644)
		if oerr != nil {
			return // the stream is not open yet; the request that would have painted has not arrived
		}
		defer f.Close() //stylegate:ignore the fixture's own append handle; its close cannot change an assertion
		if _, werr := fmt.Fprintf(f, "%d\n", len(p)); werr != nil {
			return
		}
		if _, werr := f.Write(p); werr != nil {
			return
		}
	}
	// THE PAINT PLAN COMPOSES THE RENDERER'S OWN HEADER (see the two plans below):
	// `/screen` chooses a turn by that header row, so a recording that painted
	// bodies and no header has not one turn in it.

	var (
		mu   = make(chan struct{}, 1)
		seen [][]byte
	)
	mu <- struct{}{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body) //stylegate:ignore bounded read of a test request body; a short read is acceptable
		<-mu
		seen = append(seen, body)
		n := len(seen)
		mu <- struct{}{}
		paint(paintFrame, n)
		textSSE(w, "ok")
	}))
	t.Cleanup(srv.Close)

	sl, err := agent.NewSessionLogger(chatBoundaryTestModel, "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	t.Cleanup(func() { sl.Close() }) //stylegate:ignore test teardown of a logger this test opened
	h.sessDir = sessionlayout.RecordContainer(sl.Path())
	// THE DIRECTORY MUST BE KNOWN BEFORE A PATH IS COMPOSED FROM IT. An empty
	// sessDir makes filepath.Join return the BARE name, so the fixture would write
	// `seed.screen.stream` into the package's own source directory — measured: exactly
	// one such file landed in cmd/agent/ before this guard existed, untracked litter
	// inside the tree (Rule 30). A refusal here is cheaper than a sweep.
	if h.sessDir == "" {
		t.Fatalf("the session logger reported no directory — refusing to compose a capture path from nothing")
	}
	sgrPath = filepath.Join(h.sessDir, chatScreenE2ERecordingName)

	ag := &agent.Agent{
		Client:     &deepseek.Client{BaseURL: srv.URL, APIKey: "test-key", HTTPClient: srv.Client()},
		Ctx:        &agent.Context{},
		Model:      chatBoundaryTestModel,
		Timeout:    0,
		SessionLog: sl,
		Logger:     log.New(io.Discard, "", 0),
	}
	ag.PersistChatSession = true
	chatAgentFactory = func(string) (*agent.Agent, error) { return ag, nil }

	var console strings.Builder
	in, closeIn := newChatInputCooked(strings.NewReader(input), &console)
	// NOTICES GO TO THE INPUT'S ERROR SINK, not to flowOut (WriteNotice → errOut,
	// which is os.Stderr in a real run). Both are captured here because THIS test's
	// subject is what the operator READS: a refusal written to the process's stderr
	// and a capture note written to the flow are the same console to them, and a
	// test that captured only one would report "no refusal" for a command that
	// refused correctly.
	in.errOut = &console
	h.code = runChatWithInput([]string{"--no-editor", chatSgrStreamFlag + chatFlagValueSep + sgrPath}, in)
	closeIn()
	h.console = console.String()

	h.requests = func() [][]byte {
		<-mu
		out := append([][]byte{}, seen...)
		mu <- struct{}{}
		return out
	}
	return h
}

// chatScreenE2EPaintOneTurn is the primer plan: ONE numbered turn (61) with a
// body, painted when the first request arrives.
func chatScreenE2EPaintOneTurn(paintFrame func([]byte), request int) {
	if request != 1 {
		return
	}
	paintFrame([]byte("\x1b[22;1H" + lineedit.TurnHeader(lineedit.TurnInfo{Index: chatScreenE2EPaintedTurn, Prompt: "the primer turn"}, chatScreenE2EHeaderCols) + "\x1b[0m"))
	for i := 0; i < chatScreenE2EPaintedFrames; i++ {
		paintFrame([]byte("\x1b[1m" + chatScreenE2EPainted + "\x1b[0m\r\n"))
	}
}

// chatScreenE2EPaintTwoTurns paints turn 58 when the first request arrives and
// turn 59 when the second does — so at the instant `/screen 58` runs, 58 is the
// NEWEST turn, and at the instant the bare `/screen` runs, 59 is.
func chatScreenE2EPaintTwoTurns(paintFrame func([]byte), request int) {
	paintOne := func(turn int, prompt, body string) {
		paintFrame([]byte("\x1b[22;1H" + lineedit.TurnHeader(lineedit.TurnInfo{Index: turn, Prompt: prompt}, chatScreenE2EHeaderCols) + "\x1b[0m"))
		paintFrame([]byte("\x1b[22;1H" + body + "\r\n"))
	}
	switch request {
	case 1:
		paintOne(chatScreenE2ETurnFirst, "first", chatScreenE2ETurnFirstBody)
	case 2:
		paintOne(chatScreenE2ETurnSecond, "second", chatScreenE2ETurnSecondBody)
	}
}

const (
	// chatScreenE2ETurnFirst / chatScreenE2ETurnSecond are the two turns the
	// selection fixture paints. They are consecutive so "one turn too long" is a
	// single step away, and neither number appears in the script's own text.
	chatScreenE2ETurnFirst  = 58
	chatScreenE2ETurnSecond = 59
	// chatScreenE2ETurnFirstBody / chatScreenE2ETurnSecondBody are greppable
	// markers: a capture that holds the WRONG turn is visible as the other one's
	// marker, which a byte count would not show.
	chatScreenE2ETurnFirstBody  = "BODY-OF-THE-FIRST-TURN"
	chatScreenE2ETurnSecondBody = "BODY-OF-THE-SECOND-TURN"
	// chatScreenE2ERecordingName is the recording the fixture seeds.
	chatScreenE2ERecordingName = "seed.screen.stream"
	// chatScreenE2EPaintedFrames is how many painted BODY frames the fixture draws
	// — more than one, so the absence assertion is not satisfied by a single frame.
	chatScreenE2EPaintedFrames = 6
	// chatScreenE2EPaintedTurn is the turn number the fixture's header declares. It
	// is a number no other part of this run prints, so a capture that names it can
	// only have got it from the header.
	chatScreenE2EPaintedTurn = 61
	// chatScreenE2EHeaderCols is the width the fixture's header is rendered at —
	// the same width a real band uses, so the header is the undamaged shape.
	chatScreenE2EHeaderCols = 120
	// chatScreenE2EFramesInCapture is what a capture of that turn must hold: the
	// header frame plus the body frames, WHOLE (nothing is truncated any more).
	chatScreenE2EFramesInCapture = chatScreenE2EPaintedFrames + 1
	// chatScreenE2EPrimer is a REAL first turn, and it is load-bearing rather than
	// filler: the frames are appended by the fixture while a model request is in
	// flight (that is the only moment a live run's stream has content), so the
	// console must have completed one ordinary turn before `/screen` can capture
	// anything. A script that opened with `/screen` would be asking a run that has
	// painted nothing — which the command answers honestly ("nothing has been
	// painted yet"), and which is therefore not the state this acceptance is about.
	chatScreenE2EPrimer = "hello there\n"
)

// TestChatScreenE2ENextRequestCarriesThePathAndNoBytes is the epic's acceptance:
// a typed `/screen`, then a real turn, and the request body of that turn holds
// the reference and none of the painted content.
func TestChatScreenE2ENextRequestCarriesThePathAndNoBytes(t *testing.T) {
	h := runScreenE2E(t, chatScreenE2EPrimer+"/screen\nwhat is on my screen?\n")
	reqs := h.requests()
	if len(reqs) < 2 {
		t.Fatalf("the run sent %d request(s) (exit %d) — /screen must not consume a turn, but the line after it must", len(reqs), h.code)
	}
	// The capture artifact must exist on disk, in the run's own session directory.
	matches, err := filepath.Glob(filepath.Join(h.sessDir, "screen-turn*"+uiclient.ScreenStreamSuffix))
	if err != nil {
		t.Fatalf("glob the capture: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("found %d captures in %s (%v), want exactly one", len(matches), h.sessDir, matches)
	}
	artifact := matches[0]
	meta, _, rerr := uiclient.ReadSgrFrames(mustOpen(t, artifact))
	if rerr != nil {
		t.Fatalf("the capture is not a readable stream: %v", rerr)
	}
	if meta.Frames != chatScreenE2EFramesInCapture {
		t.Errorf("the capture holds %d frames, turn %d's span held %d (header + %d body frames, WHOLE)", meta.Frames, chatScreenE2EPaintedTurn, chatScreenE2EFramesInCapture, chatScreenE2EPaintedFrames)
	}
	// THE NAME AND THE CONTENT AGREE (the epic's measured defect): the artifact is
	// named for the turn its own bytes paint.
	wantName := "screen-turn" + chatScreenItoa(chatScreenE2EPaintedTurn) + uiclient.ScreenStreamSuffix
	if filepath.Base(artifact) != wantName {
		t.Errorf("the capture is named %q, want %q — the name comes from the located turn", filepath.Base(artifact), wantName)
	}

	// THE LAST REQUEST is the turn the operator typed AFTER the capture; the first
	// is the primer that let the fixture paint.
	body := reqs[len(reqs)-1]
	if !bytes.Contains(body, []byte(artifact)) {
		t.Errorf("the request body does not name the capture %q — the reference never reached the model", artifact)
	}
	if !bytes.Contains(body, []byte(chatScreenInjectionHeader)) {
		t.Errorf("the request body carries no %q marker", chatScreenInjectionHeader)
	}
	if !bytes.Contains(body, []byte("what is on my screen?")) {
		t.Errorf("the operator's own line is missing from the request body")
	}
	// THE BAN: none of the painted bytes ride along.
	if bytes.Contains(body, []byte(chatScreenE2EPainted)) {
		t.Errorf("the request body carries PAINTED CONTENT — the capture's bytes were inlined instead of referenced")
	}
	if bytes.Contains(body, []byte("\x1b[1m"+chatScreenE2EPainted)) {
		t.Errorf("the request body carries the SGR escape of a painted frame")
	}
	// And the reference states the file's size and how to read it.
	if !bytes.Contains(body, []byte("read tool")) {
		t.Errorf("the reference does not tell the model how to read the file")
	}
	sz := []byte(chatScreenItoa(int(meta.Bytes)))
	if !bytes.Contains(body, sz) {
		t.Errorf("the reference does not state the file's size (%d bytes)", meta.Bytes)
	}
}

// TestChatScreenE2EIsConsumedOnceAcrossTwoTurns pins the ONE-SHOT property across
// a real multi-turn run: the second turn carries no capture reference, so a
// single `/screen` cannot keep re-sending a stale path for the rest of a session.
func TestChatScreenE2EIsConsumedOnceAcrossTwoTurns(t *testing.T) {
	h := runScreenE2E(t, chatScreenE2EPrimer+"/screen\nfirst question\nsecond question\n")
	reqs := h.requests()
	if len(reqs) < 3 {
		t.Fatalf("the run sent %d requests, want the primer plus two (exit %d)", len(reqs), h.code)
	}
	// reqs[0] is the primer, reqs[1] the first turn after the capture.
	//
	// "CONSUMED ONCE" IS A STATEMENT ABOUT MESSAGES, NOT ABOUT REQUESTS, and the
	// difference is the whole reason this case reads the parsed body. Every turn
	// sends the WHOLE conversation, so the reference the first post-capture turn
	// carried is HISTORY from the second turn on — it must still be there (it is a
	// message the model was told, and deleting it would rewrite the record). What
	// must NOT happen is a SECOND copy appearing, which is what a note that was not
	// cleared would produce: the same parked text appended again, and again.
	if got := chatScreenReferenceMessages(t, reqs[1]); got != 1 {
		t.Errorf("the first post-capture turn carries the reference in %d message(s), want exactly 1", got)
	}
	for i, r := range reqs[2:] {
		if got := chatScreenReferenceMessages(t, r); got != 1 {
			t.Errorf("request %d carries the reference in %d messages, want exactly 1 (the one already in history) — a parked note that is not cleared appends a second copy every turn", i+3, got)
		}
	}
}

// chatScreenReferenceMessages counts the message bodies in one request that carry
// the capture marker, by decoding the request the way the endpoint reads it.
//
// It walks the JSON generically (a map of message objects) rather than through a
// typed decoder: the thing under test is the BYTES this feature put in a message,
// and a typed struct would silently drop a field it does not know about.
func chatScreenReferenceMessages(t *testing.T, body []byte) int {
	t.Helper()
	var req struct {
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("decode the request body: %v", err)
	}
	n := 0
	for _, m := range req.Messages {
		if bytes.Contains(m.Content, []byte(chatScreenInjectionHeader)) {
			n++
		}
	}
	return n
}

// TestChatScreenE2ETurnArgumentCapturesThatTurn is the epic's REPL-level
// acceptance for BOTH forms, driven through the real entry point: `/screen 58`
// writes a file NAMED for turn 58 that CONTAINS turn 58's painting, and a later
// bare `/screen` writes the newest turn's. The recording carries two numbered
// turns (58 and 59), each with its own greppable body, so "which turn did I get"
// is answered by the bytes rather than by the note.
func TestChatScreenE2ETurnArgumentCapturesThatTurn(t *testing.T) {
	h := runScreenE2ETwoTurns(t, chatScreenE2EPrimer+"/screen "+chatScreenItoa(chatScreenE2ETurnFirst)+"\nfirst question\n/screen\nsecond question\n")
	reqs := h.requests()
	if len(reqs) < 3 {
		t.Fatalf("the run sent %d request(s) (exit %d) — the primer plus two turns after two captures", len(reqs), h.code)
	}
	first := filepath.Join(h.sessDir, "screen-turn"+chatScreenItoa(chatScreenE2ETurnFirst)+uiclient.ScreenStreamSuffix)
	second := filepath.Join(h.sessDir, "screen-turn"+chatScreenItoa(chatScreenE2ETurnSecond)+uiclient.ScreenStreamSuffix)
	for _, path := range []string{first, second} {
		st, serr := os.Stat(path)
		if serr != nil {
			t.Fatalf("the capture %q was not written: %v", path, serr)
		}
		meta, frames, rerr := uiclient.ReadSgrFrames(mustOpen(t, path))
		if rerr != nil {
			t.Fatalf("the capture %q is unreadable: %v", path, rerr)
		}
		var body []byte
		for _, f := range frames {
			body = append(body, f...)
		}
		t.Logf("capture %s: %d bytes on disk, %d payload bytes, %d frame(s); turns named in the payload: %s",
			filepath.Base(path), st.Size(), meta.Bytes, meta.Frames, chatScreenE2ETurnsIn(body))
		if !bytes.Contains(body, []byte("turn "+chatScreenE2EBase(path))) {
			t.Errorf("the capture %q does not contain its own turn number in the payload", filepath.Base(path))
		}
	}
	// Turn 58's file must NOT carry turn 59's body, and vice versa: this is the
	// "one turn too long / wrong segment" failure the epic measured.
	b58 := chatScreenE2EPayload(t, first)
	b59 := chatScreenE2EPayload(t, second)
	if !bytes.Contains(b58, []byte(chatScreenE2ETurnFirstBody)) {
		t.Errorf("screen-turn%d does not hold turn %d's body", chatScreenE2ETurnFirst, chatScreenE2ETurnFirst)
	}
	if bytes.Contains(b58, []byte(chatScreenE2ETurnSecondBody)) {
		t.Errorf("screen-turn%d holds turn %d's body — the segment runs one turn too long", chatScreenE2ETurnFirst, chatScreenE2ETurnSecond)
	}
	if !bytes.Contains(b59, []byte(chatScreenE2ETurnSecondBody)) {
		t.Errorf("the bare /screen capture does not hold the newest turn's body")
	}
	// The ban still holds for both: no painted content in either request body.
	for i, r := range reqs[1:] {
		if bytes.Contains(r, []byte(chatScreenE2ETurnFirstBody)) || bytes.Contains(r, []byte(chatScreenE2ETurnSecondBody)) {
			t.Errorf("request %d carries PAINTED CONTENT — the capture was inlined instead of referenced", i+2)
		}
	}
}

// TestChatScreenE2ETurnArgumentIsRefusedByName drives the REFUSALS through the
// real REPL: the removed flag and the three malformed turns each produce a
// console line naming what was typed, and NONE of them writes a capture.
func TestChatScreenE2ETurnArgumentIsRefusedByName(t *testing.T) {
	bad := []string{chatScreenRemovedFullFlag, "58x", "-1", "0"}
	script := chatScreenE2EPrimer
	for _, arg := range bad {
		script += "/screen " + arg + "\n"
	}
	h := runScreenE2ETwoTurns(t, script)
	for _, arg := range bad {
		if !strings.Contains(h.console, "/screen: ") || !strings.Contains(h.console, arg) {
			t.Errorf("typing `/screen %s` produced no console refusal naming %q; console tail:\n%s", arg, arg, chatScreenTail(h.console, 1200))
		}
	}
	matches, globErr := filepath.Glob(filepath.Join(h.sessDir, "screen-turn*"+uiclient.ScreenStreamSuffix))
	if globErr != nil {
		// The pattern is built from constants, so a syntax error here would mean the
		// construction itself moved. An unmatched PATTERN is (nil, nil) and is not an
		// error at all, so this arm can never fire for "no capture was written" —
		// the absence assertion below stays the one that speaks for that.
		t.Fatalf("glob the turn artifacts under %s: %v", h.sessDir, globErr)
	}
	if len(matches) != 0 {
		t.Errorf("a REFUSED /screen wrote %v — a refused command must not file an artifact", matches)
	}
}

// chatScreenE2ETurnsIn lists the distinct turn numbers a payload names, so the
// test's log answers "which turn did this file really hold" from the bytes.
func chatScreenE2ETurnsIn(body []byte) string {
	seen := []string{}
	rest := string(body)
	for {
		i := strings.Index(rest, "turn ")
		if i < 0 {
			break
		}
		rest = rest[i+len("turn "):]
		j := 0
		for j < len(rest) && rest[j] >= '0' && rest[j] <= '9' {
			j++
		}
		if j == 0 {
			continue
		}
		n := rest[:j]
		dup := false
		for _, s := range seen {
			if s == n {
				dup = true
			}
		}
		if !dup {
			seen = append(seen, n)
		}
	}
	if len(seen) == 0 {
		return "(none)"
	}
	return strings.Join(seen, ",")
}

// chatScreenE2EBase is the turn number a capture's FILE NAME states.
func chatScreenE2EBase(path string) string {
	base := filepath.Base(path)
	base = strings.TrimPrefix(base, "screen-turn")
	return strings.TrimSuffix(base, uiclient.ScreenStreamSuffix)
}

// chatScreenE2EPayload reads a capture's payload bytes.
func chatScreenE2EPayload(t *testing.T, path string) []byte {
	t.Helper()
	_, frames, err := uiclient.ReadSgrFrames(mustOpen(t, path))
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	var out []byte
	for _, f := range frames {
		out = append(out, f...)
	}
	return out
}

// chatScreenTail returns the last n bytes of s, for a failure message.
func chatScreenTail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
