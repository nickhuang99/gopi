package uiclient

// screenmodel_real_turn_test.go — the AGENT-READABILITY proof, on a REAL turn.
//
// The readable screen model is the point of this surface: an agent cannot read
// a terminal, so the UI's state has to be recoverable as TEXT. The faithful
// instrument is not a synthetic frame list — it is the turn the engine actually
// produced. So this test feeds the CAPTURED HTTP RESPONSE BODY of a real turn
// (testdata/tool-call-turn.sse.b64, captured from a live `gopi web` server on
// this host) through the client's own readFrames — the same entry point
// Client.Turn uses — into the real model, and asserts on model.View():
//
//	client.Turn -> readFrames -> model.applyFrame -> model.View (the screen)
//
// It also DUMPS the screen (env GOPI_UI_SCREENMODEL_DUMP=<path>), so the
// transcript in the report is produced by the code under assertion rather than
// beside it.
//
// The fixture is stored base64-encoded: an SSE body legitimately contains raw
// newlines, and the encoding is what keeps an editor, a patch or a shell from
// reflowing the bytes the server actually wrote.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nickhuang99/gopi/internal/sessionevent"
)

const (
	// realTurnFixture is the captured body (base64).
	realTurnFixture = "testdata/tool-call-turn.sse.b64"
	// realTurnMinDeltaFrames is a READ FLOOR: the capture carried 44 delta
	// frames (one per model token fragment). A fixture that lost most of its
	// frames — a truncating copy, a reflowed paste — would let the screen
	// assertions below pass on a fragment of the turn, so the census is
	// asserted BEFORE the screen is.
	realTurnMinDeltaFrames = 40
	// realTurnToolFrames is the capture's tool-frame count (one call + one
	// result): the turn this test is about is a TOOL-CALL turn.
	realTurnToolFrames = 2
	// dumpPathEnv names the env var that asks this test to write the
	// screen-model dump it asserts on.
	dumpPathEnv = "GOPI_UI_SCREENMODEL_DUMP"
	// dumpPerm is the dump file's mode (evidence the operator can read).
	dumpPerm = 0o644
)

// loadRealTurn reads the captured turn and decodes it.
func loadRealTurn(t *testing.T) []byte {
	t.Helper()
	encoded, err := os.ReadFile(realTurnFixture)
	if err != nil {
		t.Fatalf("read %s: %v", realTurnFixture, err)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil {
		t.Fatalf("decode %s: %v", realTurnFixture, err)
	}
	if len(raw) == 0 {
		t.Fatalf("%s decoded to nothing", realTurnFixture)
	}
	return raw
}

// startTurnForReplay begins a turn the way the TUI does — the operator types
// and presses Enter — and returns the model with the turn in flight. The
// returned tea.Cmd is deliberately NOT executed: it would perform the HTTP
// request, and this test's subject is the RENDER path, whose input is the
// captured response body. Starting the turn matters because the model treats a
// delta that arrives outside a turn as a replayed stream and gives each
// fragment its own row (appendAssistant); only a started turn accumulates the
// deltas into one answer, which is what the real surface does.
func startTurnForReplay(t *testing.T, m model, prompt string) model {
	t.Helper()
	m.input = []rune(prompt)
	m.cursor = len(m.input)
	mi, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mi.(model)
	if !m.streaming {
		t.Fatal("Enter on a non-empty input did not start a turn — the replay would render a stream, not a turn")
	}
	if m.assistantIdx < 0 {
		t.Fatal("no assistant row was opened for the turn")
	}
	return m
}

// replayRealTurn starts a turn and pushes the captured body through the
// client's frame reader into it, returning the model's screen plus a frame
// census.
func replayRealTurn(t *testing.T, raw []byte) (string, map[string]int) {
	t.Helper()
	m := startTurnForReplay(t, newTestModel(t), "run the bash tool with exactly: head -3 go.mod")
	census := map[string]int{}
	n := 0
	sink := func(f Frame) error {
		census[f.Event]++
		n++
		m.applyFrame(f)
		return nil
	}
	if err := readFrames(bufio.NewReader(bytes.NewReader(raw)), sink, context.Background()); err != nil {
		t.Fatalf("replaying the captured turn: %v", err)
	}
	if n == 0 {
		t.Fatal("the captured turn produced no frames — the fixture or the reader is broken, and every screen assertion below would be vacuous")
	}
	return m.View(), census
}

// screenAssertion is one readable-state claim about the screen, recorded so the
// dump carries the same assertions that were exercised.
type screenAssertion struct {
	Name string `json:"name"`
	Want string `json:"want"`
	Pass bool   `json:"pass"`
}

// TestRealToolCallTurnScreenModelIsAgentReadable is the acceptance: a turn that
// INCLUDED a tool call renders a screen whose state an agent can assert on.
func TestRealToolCallTurnScreenModelIsAgentReadable(t *testing.T) {
	raw := loadRealTurn(t)
	view, census := replayRealTurn(t, raw)

	// 1. The fixture is a real turn, not a stub. The read floor guards every
	//    assertion below from passing vacuously.
	if census[EventDelta] < realTurnMinDeltaFrames {
		t.Fatalf("capture has %d delta frames, want >= %d — the fixture is a fragment, not the turn",
			census[EventDelta], realTurnMinDeltaFrames)
	}
	if census[EventTool] != realTurnToolFrames {
		t.Fatalf("capture has %d tool frames, want exactly %d: a call and its result",
			census[EventTool], realTurnToolFrames)
	}
	if census[EventDone] != 1 {
		t.Fatalf("capture has %d done frames, want exactly 1", census[EventDone])
	}

	// 2. The screen is TEXT a program can read: one ESC byte would mean the
	//    screen model leaked terminal control sequences instead of state.
	if i := strings.IndexByte(view, 0x1b); i >= 0 {
		t.Errorf("the screen model contains an ESC byte at offset %d — it is not readable text", i)
	}

	// 3. The assertions. The transcript WRAPS, so each claim is made against
	//    the screen flattened of whitespace: a wrap can move the break, never
	//    the characters. The screen is the MODEL's own output.
	//
	//    The two tool rows sit ABOVE the visible transcript: this test runs at
	//    the default 20-row viewport and the model streams its answer into ONE
	//    growing entry (44 delta fragments, the full final text), so the tail
	//    the terminal shows is the answer. Reading the tool activity therefore
	//    needs the same thing an OPERATOR needs to see it — a taller terminal —
	//    so the assertions run at a height that fits the whole turn. (The
	//    layout itself is the sibling tests' subject: ViewLayoutIsStable,
	//    ViewTruncatesToWidth, ScrollIsClamped.)
	wide := newTestModel(t)
	wide.height = 200
	wide = startTurnForReplay(t, wide, "run the bash tool with exactly: head -3 go.mod")
	sink := func(f Frame) error {
		wide.applyFrame(f)
		return nil
	}
	if err := readFrames(bufio.NewReader(bytes.NewReader(raw)), sink, context.Background()); err != nil {
		t.Fatalf("replaying the captured turn at full height: %v", err)
	}
	wideView := wide.View()
	checks := []struct {
		name   string
		want   string
		invert bool
	}{
		// The tool call and its result are on the screen as their own rows.
		{name: "tool call row", want: "callbashhead-3go.mod"},
		{name: "tool result row", want: "resultbashexit=0"},
		// The tool's OUTPUT — what the operator actually asked for — is in the
		// transcript (the model quoted the bash result back).
		{name: "tool output on screen", want: "github.com/nickhuang99/gopi"},
		// The turn ended, with the counters an agent uses to know WHERE it is.
		// The row's count is the REPL-turn quantity and names itself as one — the
		// expectation is built from the constant, so the word cannot drift from
		// the renderer's (Rule 14). `flat` below strips spaces, so the constant's
		// own space goes with them.
		{name: "status line at rest", want: "idle·" + strings.ReplaceAll(sessionevent.CounterUnitTurn, " ", "") + "1·toolevents2"},
		// A finished turn is not still streaming, and no fault was recorded.
		{name: "not still streaming", want: "streaming…", invert: true},
		{name: "no fault recorded", want: errorPrefix, invert: true},
	}
	flat := strings.Join(strings.Fields(wideView), "")
	results := make([]screenAssertion, 0, len(checks))
	for _, c := range checks {
		found := strings.Contains(flat, strings.Join(strings.Fields(c.want), ""))
		pass := found != c.invert
		results = append(results, screenAssertion{Name: c.name, Want: c.want, Pass: pass})
		if !pass {
			t.Errorf("%s: %q present = %v (want present = %v)", c.name, c.want, found, !c.invert)
		}
	}
	for _, r := range results {
		t.Logf("ASSERT %-24s %-30q pass=%v", r.Name, r.Want, r.Pass)
	}

	// 4. The dump, written from THIS path.
	if path := os.Getenv(dumpPathEnv); path != "" {
		body := "# gopi ui screen model — one REAL tool-call turn\n" +
			"# source: " + realTurnFixture + " (the captured HTTP response body of POST /api/chat,\n" +
			"#         taken from a live `gopi web` server on this host)\n" +
			"# frames:  " + formatCensus(census) + "\n" +
			"# assertions: " + formatResults(results) + "\n" +
			"# --- screen: model.View(), the model's OWN output ---\n" +
			"# The `ui` viewport is 20 transcript rows: the answer (44 streamed\n" +
			"# fragments accumulated into ONE entry) fills the tail, and the tool\n" +
			"# rows sit above it — exactly as an operator needs to scroll to see\n" +
			"# them. The marked region below is a taller terminal, where the whole\n" +
			"# turn is visible at once (the assertions ran against this one).\n" +
			"# === TERMINAL VIEW (20 transcript rows) ===\n" +
			dumpWrap(view) +
			"# === SAME TURN, FULL HEIGHT (the assertions' screen) ===\n" +
			dumpWrap(wideView) +
			"# --- end screen ---\n"
		if err := os.WriteFile(path, []byte(body), dumpPerm); err != nil {
			t.Fatalf("write the screen-model dump to %s: %v", path, err)
		}
		t.Logf("screen-model dump -> %s", path)
	}
}

// TestScreenModelReaderRefusesMalformedFrame is the inverse property: the
// reader is what makes the screen trustworthy, so a frame it cannot decode must
// become a reported FAULT — never assistant prose and never a silently dropped
// line (a dropped frame is how a screen model starts lying).
func TestScreenModelReaderRefusesMalformedFrame(t *testing.T) {
	m := newTestModel(t)
	m.applyFrame(Frame{Event: EventDelta, Data: `{"text":"before "}`})
	m.applyFrame(Frame{Event: "not-a-real-event", Data: `{"text":"PAYLOAD-MUST-NOT-RENDER"}`})
	view := m.View()
	if !strings.Contains(view, "before ") {
		t.Fatalf("the valid delta did not render:\n%s", view)
	}
	if strings.Contains(view, "PAYLOAD-MUST-NOT-RENDER") {
		t.Errorf("an unknown event's payload reached the screen as prose:\n%s", view)
	}
	if !strings.Contains(view, errorPrefix) {
		t.Errorf("an unknown event was not reported as a fault (no %q row):\n%s", errorPrefix, view)
	}
}

// TestRealTurnCountersMatchTheModelState closes the loop between the text an
// agent reads and the model's own accounting: the status line an agent parses
// must be derivable from the frames, and the turn must have been performed by a
// TOOL-CALL turn (2 tool events, 1 turn).
func TestRealTurnCountersMatchTheModelState(t *testing.T) {
	m := newTestModel(t)
	sink := func(f Frame) error {
		m.applyFrame(f)
		return nil
	}
	raw := loadRealTurn(t)
	if err := readFrames(bufio.NewReader(bytes.NewReader(raw)), sink, context.Background()); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if m.turns != 1 {
		t.Errorf("turns = %d, want 1 after the done frame", m.turns)
	}
	if m.toolEvents != realTurnToolFrames {
		t.Errorf("toolEvents = %d, want %d", m.toolEvents, realTurnToolFrames)
	}
	if m.streaming {
		t.Error("the model is still streaming after the turn's done frame")
	}
	if got := m.statusLine(); !strings.Contains(got, "idle") || !strings.Contains(got, sessionevent.CounterUnitTurn+" 1") {
		t.Errorf("statusLine() = %q, want an idle line at turns 1", got)
	}
}

// dumpWrap returns the screen verbatim plus a trailing newline — the dump keeps
// the transcript's own wrapping, because the LINE BREAKS are the layout an
// agent reads.
func dumpWrap(view string) string {
	if strings.HasSuffix(view, "\n") {
		return view
	}
	return view + "\n"
}

// formatCensus renders the frame census in a stable order.
func formatCensus(census map[string]int) string {
	keys := []string{EventDelta, EventTool, EventDone, EventError, EventQueued, EventCommand}
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		if census[k] > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", k, census[k]))
		}
	}
	return strings.Join(parts, " ")
}

// formatResults renders the exercised assertions as JSON for the dump.
func formatResults(results []screenAssertion) string {
	b, err := json.Marshal(results)
	if err != nil {
		return "[]"
	}
	return string(b)
}
