package main

// chat_surface_key_refusal_e2e_test.go — the REFUSAL path at the REAL stack: a
// `gopi chat` REPL on a real pty, its own line editor, its own chatSurface, and a
// model whose only act is `chat_surface action=key`.
//
// WHY THIS CASE EXISTS BESIDE THE PACKAGE'S OWN (Rule 31). internal/lineedit's
// case proves the injector refuses a key spelling at its own boundary. This one
// proves the sentence the MODEL actually reads — the one the tool renders — and
// the property the epic is about: the operator's editing line is untouched. The
// two rungs are kept apart on purpose: a refusal that exists in the injector and
// is never reached from the production surface (cmd/agent's chatSurface.InjectKey
// → InjectKeyBounded) would leave the defect fully live on a real `gopi chat`.
//
// THE DISCRIMINATING PAIR. One round carries TWO calls: `ctrl-u` (a shape the
// vocabulary does not name — the silent-downgrade defect) and `q` (an ordinary
// rune). A case that only asserted "the refusal happened" would pass with the
// literal route broken as well, so both halves are asserted on the SAME grid:
// the rune must be in the editing line, the spelling must not be — anywhere.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nick/gopi/internal/agent"
	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/toolnames"
)

const (
	// keyRefusalPtyCols / keyRefusalPtyRows are the terminal the session runs on.
	keyRefusalPtyCols = 90
	keyRefusalPtyRows = 24
	// keyRefusalSpelling is the value the model asks for: the vocabulary's OWN
	// name form for a control byte the table does not name (0x15 = KillToStart).
	// It is the exact defect shape — six letters that must never become text.
	keyRefusalSpelling = "ctrl-u"
	// keyRefusalRune is the literal the same round carries: the positive control.
	keyRefusalRune = "q"
	// keyRefusalBootstrap is the one line the OPERATOR supplies. The REPL runs no
	// round before a line arrives.
	keyRefusalBootstrap = "hello"
	// keyRefusalAnswer is the benign answer of the round after the injections, so
	// the session reaches an idle prompt and the grid can be read.
	keyRefusalAnswer = "refusal leg done."
	// keyRefusalWait bounds every wait for the refusal to reach the transcript.
	// It is the RIGHT instrument for the three polls above, which ask "has this
	// round's effect reached the mock / the paint", and the WRONG one for the
	// closeout leg — see keyRefusalStallWindow.
	keyRefusalWait = 10 * time.Second
	// keyRefusalStep is that wait's polling interval.
	keyRefusalStep = 5 * time.Millisecond
	// keyRefusalStallWindow is the CLOSEOUT leg's criterion, and it is measured in
	// PROGRESS, never in elapsed time: the run gets this long with NEITHER counter
	// moving — no model round served AND not one byte painted — before the case
	// fails. This leg used to end on a fixed wall clock (keyRefusalWait, 10s), which
	// measures the host rather than the session; under a whole-package run the run
	// was still working through the turn the key had claimed when the window
	// expired and the case failed at Elapsed 10.78s (measured 2026-10-13, stream
	// run-1791875819-1765160.json: the failing case's own line, while the SAME tree
	// passed alone in 12.085s). The value is the package's reviewed
	// boundStallWindow, referenced here rather than given a second number: it is
	// ONE policy with one definition (Rule 14), and a bare literal would be a
	// second, silently-diverging answer to "how quiet is stuck".
	keyRefusalStallWindow = boundStallWindow
	// keyRefusalCeiling is the closeout leg's HANG DETECTOR, not its criterion, and
	// it is a second consumer of the same package constant for the same reason: a
	// run that never stops moving can never trip the stall window, so without a
	// ceiling the case would HANG cmd/agent instead of failing it — trading a flake
	// for a hang. A run that is still progressing when it expires is reported as a
	// livelock and is deliberately a DIFFERENT sentence from a stall.
	keyRefusalCeiling = boundCloseoutCeiling
)

// keyRefusalMockSSE streams the two rounds: the injection round, then the answer.
func keyRefusalMockSSE(w io.Writer, calls []string) {
	testutil.WriteClaudeSSE(w, "message_start", `{"type":"message_start","message":{"id":"m1","usage":{"input_tokens":10,"output_tokens":1}}}`)
	testutil.WriteClaudeSSE(w, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
	testutil.WriteClaudeSSE(w, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"asking for keys"}}`)
	testutil.WriteClaudeSSE(w, "content_block_stop", `{"type":"content_block_stop","index":0}`)
	for i, k := range calls {
		args, _ := json.Marshal(map[string]string{"action": agent.ChatSurfaceActionKey, "text": k}) //stylegate:ignore marshal of the refusal-case key frame; the refusal text on the dump is what is asserted
		block := fmt.Sprintf(`{"type":"tool_use","id":"call_%02d","name":%q,"input":%s}`, i, toolnames.ToolChatSurface, args)
		testutil.WriteClaudeSSE(w, "content_block_start", fmt.Sprintf(`{"type":"content_block_start","index":%d,"content_block":%s}`, i+1, block))
		testutil.WriteClaudeSSE(w, "content_block_stop", fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, i+1))
	}
	testutil.WriteClaudeSSE(w, "message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":10,"output_tokens":7}}`)
	testutil.WriteClaudeSSE(w, "message_stop", `{"type":"message_stop"}`)
}

// TestChatSurfaceKey_RefusesAKeySpellingOnTheRealStack is the refusal leg of the
// epic, at the stack a user actually runs.
//
// The three claims, in the order they are read off real state:
//
//  1. the round's tool_result says REFUSED, and it names the legal set;
//  2. the refused spelling is NOWHERE in the painted editing line — the property
//     the defect broke (Rule 15: a refused call must not also be a silent write);
//  3. the ordinary rune of the SAME round IS in the editing line — the positive
//     control that separates "the refusal works" from "the seam stopped working".
func TestChatSurfaceKey_RefusesAKeySpellingOnTheRealStack(t *testing.T) {
	t.Setenv("GOPI_HOME", t.TempDir())
	t.Setenv("GOPI_MEMORY_DB", "")

	calls := []string{keyRefusalSpelling, keyRefusalRune}
	var mu sync.Mutex
	var bodies []string
	rounds := func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(bodies)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)         //stylegate:ignore bounded read of a test request body; a short read is acceptable
		defer func() { _ = r.Body.Close() }() //stylegate:ignore the test's own request body close
		mu.Lock()
		bodies = append(bodies, string(body))
		n := len(bodies)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			keyRefusalMockSSE(w, calls)
			return
		}
		testutil.TextSSE(w, keyRefusalAnswer)
	}))
	t.Cleanup(srv.Close)

	pair := chatOpenPtyPair(t, keyRefusalPtyCols, keyRefusalPtyRows)
	paint := &keyE2EPaint{}
	term, err := lineedit.Open(pair.Slave, paint)
	if err != nil {
		t.Fatalf("open the pty slave as a terminal: %v", err)
	}
	// THE PRODUCTION INPUT LAYER — the constructor the REPL itself calls, so the
	// injector's wiring is the live one (a constructor that forgot the wrapper
	// would leave injector nil and this case would be testing nothing).
	in := newChatInputWithSource(term, paint, chatPrompt)
	if in.injector == nil {
		t.Fatalf("newChatInputWithSource built an input layer with NO injection seam: action=key would refuse on every live run")
	}
	in.editor.SetPalette(lineedit.BasicPalette)
	if !in.editor.EnableRegion() {
		t.Fatalf("EnableRegion(pty %dx%d) = false: the case needs the real chat layout", keyRefusalPtyCols, keyRefusalPtyRows)
	}

	exited := make(chan struct{})
	done := make(chan int, 1)

	sl, err := agent.NewSessionLogger("deepseek-v4-pro", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	t.Cleanup(func() { sl.Close() }) //stylegate:ignore test teardown of a logger this test opened

	ag := &agent.Agent{
		Client:     &deepseek.Client{BaseURL: srv.URL, APIKey: "test-key", HTTPClient: srv.Client()},
		Ctx:        &agent.Context{},
		Model:      "deepseek-v4-pro",
		Timeout:    0,
		SessionLog: sl,
	}
	prevFactory := chatAgentFactory
	t.Cleanup(func() { chatAgentFactory = prevFactory })
	chatAgentFactory = func(string) (*agent.Agent, error) { return ag, nil }

	go func() {
		defer close(exited)
		done <- runChatWithInput([]string{"--tools"}, in)
	}()

	// Teardown order, stated once in chat_pty_fixture_test.go: the editor, then the
	// TERMINAL (the stop), then the join, then the descriptors. Registered here so a
	// t.Fatalf below still runs it.
	t.Cleanup(func() {
		if !term.Stopped() {
			_ = term.Close() //stylegate:ignore the harness's own terminal teardown
		}
		chatJoinReadLoop(t, exited, func() string { return runState(exited) })
	})

	// BOOTSTRAP: one real keyed line on the pty master starts the single round.
	keyE2ETypeLine(t, &keyE2EGridAdapter{in: in, paint: paint}, pair.Master, keyRefusalBootstrap)

	// (1) THE REFUSAL REACHED THE MODEL. The second request carries the first
	// round's tool_result, which is where the tool's sentence lands.
	deadline := time.Now().Add(keyRefusalWait)
	for time.Now().Before(deadline) && rounds() < 2 {
		time.Sleep(keyRefusalStep) //stylegate:allow-sleep bounded poll (keyRefusalWait) for the round's tool result to reach the model's next request
	}
	if rounds() < 2 {
		t.Fatalf("the model saw %d round(s): the tool results never came back, so neither the refusal nor the injection can be read\n--- grid ---\n%s", rounds(), paint.rows())
	}
	mu.Lock()
	results := bodies[len(bodies)-1]
	mu.Unlock()
	if !strings.Contains(results, "refused") {
		t.Fatalf("the tool_result for %q does not say refused — the spelling was accepted (or answered without naming the outcome):\n%s", keyRefusalSpelling, results)
	}
	// The actionable half (Rule 15): the refusal must name the legal set, so the
	// model can pass a key that does work without spending a round guessing.
	for _, name := range lineedit.InjectableKeyNames() {
		if !strings.Contains(results, name) {
			t.Errorf("the refusal does not name the legal key %q — a model reading it cannot tell what to pass instead", name)
		}
	}

	// (2)/(3) THE PAINTED GRID, read after the second round so the injections have
	// been consumed at a prompt. Both halves are asserted on the SAME frame.
	//
	// THE RANGE IS PART OF THE CLAIM, and getting it wrong made this case fail on
	// its first run for the wrong reason (measured). "Is ctrl-u on the grid" over
	// the WHOLE screen is satisfied by the ANSWER BAND's own transcript — the band
	// paints the round's text, and the round's text contains the JSON
	// `{"action":"key","text":"ctrl-u"}`. That is the tool call being displayed,
	// not the operator's input line, and only the editing line's own rows can
	// answer this question. So the reading scope is the layout's editing rows,
	// asked of the LAYOUT once.
	settleDeadline := time.Now().Add(keyRefusalWait)
	for time.Now().Before(settleDeadline) {
		if strings.Contains(editingRows(in, paint), keyRefusalRune) {
			break
		}
		time.Sleep(keyRefusalStep) //stylegate:allow-sleep bounded poll (keyRefusalWait) for the injected rune to be painted on the editing line
	}
	line := editingRows(in, paint)
	if !strings.Contains(line, keyRefusalRune) {
		t.Fatalf("the LITERAL %q of the same round never reached the editing line — the refusal broke the byte route it must leave intact\nediting rows: %q\n--- grid ---\n%s", keyRefusalRune, line, paint.rows())
	}
	if strings.Contains(line, keyRefusalSpelling) {
		t.Fatalf("the refused spelling %q is ON THE EDITING LINE — it was typed into the operator's input\nediting rows: %q\n--- grid ---\n%s", keyRefusalSpelling, line, paint.rows())
	}
	// The family prefix alone is checked too: a partial write ("ctrl-" then "-u")
	// would still be the defect, and a substring check on the whole spelling could
	// miss it if the line wrapped between the two halves.
	if strings.Contains(line, "ctrl") {
		t.Fatalf("the letters of the refused spelling reached the editing line (a partial write is the same defect)\nediting rows: %q\n--- grid ---\n%s", line, paint.rows())
	}

	// END THE SESSION through the seam whose effect is to close the editor.
	if res := in.injector.InjectKey(lineedit.KeyNameCtrlC); !res.Accepted {
		t.Fatalf("injecting ctrl-c refused: %s", res.Why)
	}
	// THE CLOSEOUT LEG DECIDES ON PROGRESS, NEVER ON A CLOCK — the same criterion,
	// driven by the same function, as the sibling bound case's closeout
	// (chat_surface_bound_e2e_test.go's exitAfterInterrupt → waitForCloseoutProgress);
	// the devdoc of that rework is devdoc/chat-surface-bound-closeout-progress-wait.md.
	// This leg used to expire at a FIXED keyRefusalWait=10s whatever the run was doing,
	// which reports "the host was busy" as "the session was wedged" — the measured
	// failure: Elapsed 10.78s (that window plus the turn) while the SAME tree passed
	// the case alone in 12.085s. waitForCloseoutProgress returns the moment the run
	// leaves; otherwise it ends only after keyRefusalStallWindow in which NEITHER
	// counter moved (no model round served AND not one byte painted — both halves are
	// required, see that function's header). A run merely slowed by load therefore no
	// longer fails this case, while a genuinely wedged one still does, on its own
	// sentence; the ceiling is a HANG DETECTOR with a different sentence, so a
	// livelock fails this case instead of hanging the package. WHAT THE CASE CLAIMS IS
	// UNCHANGED AND NOT WEAKENED: one operator ctrl-c terminates the session.
	closeout := func() (served, painted int) { return rounds(), paint.painted() }
	outcome, longestGap := waitForCloseoutProgress(exited, closeout, keyRefusalStallWindow, keyRefusalCeiling, keyRefusalStep, nil)
	switch outcome {
	case closeoutLeft:
		t.Logf("the REPL left after the injected ctrl-c (%d model round(s) served, %d byte(s) painted; the longest no-progress gap this closeout saw was %v)",
			rounds(), paint.painted(), longestGap.Round(time.Millisecond))
	case closeoutStalled:
		t.Fatalf("the run STOPPED MAKING PROGRESS after the injected ctrl-c: neither counter moved for %v (bound keyRefusalStallWindow=%v, referenced from boundStallWindow) — %d model round(s) served and %d byte(s) painted, and the goroutine is %s. The criterion is progress, not elapsed time, so this is not a slow host: the run is neither moving nor leaving\n--- grid ---\n%s",
			longestGap.Round(time.Millisecond), keyRefusalStallWindow,
			rounds(), paint.painted(), runState(exited), paint.rows())
	case closeoutCeiling:
		t.Fatalf("the run was STILL MAKING PROGRESS when it hit the closeout ceiling keyRefusalCeiling=%v: %d model round(s) served and %d byte(s) painted, last movement %v ago, and the goroutine is %s. This is NOT a stall — the counters are moving, so the loop is running: what failed is that it does not terminate at all\n--- grid ---\n%s",
			keyRefusalCeiling, rounds(), paint.painted(), longestGap.Round(time.Millisecond), runState(exited), paint.rows())
	}
	in.editor.Close()
	_ = term.Close() //stylegate:ignore the test's own terminal teardown
	chatJoinReadLoop(t, exited, func() string { return runState(exited) })
	_ = pair.Master.Close() //stylegate:ignore pty master teardown of the refusal fixture, after the refusal was read off the screen

	t.Logf("action=%s asked for %q (refused, sentence named the legal set) and %q (injected); the grid carries the rune and no part of the spelling",
		agent.ChatSurfaceActionKey, keyRefusalSpelling, keyRefusalRune)
}

// keyE2EGridAdapter presents the run's input layer to the fixture helpers that
// expect the sibling case's session interface (keyE2ETypeLine and the prompt wait
// ask it for the editor's text and for a grid to dump on failure).
type keyE2EGridAdapter struct {
	in    *chatInput
	paint *keyE2EPaint
}

func (a *keyE2EGridAdapter) input() *chatInput { return a.in }
func (a *keyE2EGridAdapter) render() string    { return a.paint.rows() }

// editingRows renders ONLY the editing line's own rows, joined by "\n" — the
// reading scope of every claim this file makes about the operator's input.
//
// The layout is read ONCE for the bounds, and the grid is read once for the rows,
// so the range cannot describe a frame that never existed. The clamp at the status
// row is the sibling case's lesson applied here (menuBottom): a range that reaches
// one row past the editing line swallows a row whose content is an elapsed clock,
// and a claim about the input then quietly becomes a claim about the stopwatch.
// A run whose layout is not installed answers "" — an absence of scope, never a
// whole-screen reading standing in for it (Rule 15).
func editingRows(in *chatInput, paint *keyE2EPaint) string {
	l, ok := in.editor.Layout()
	if !ok {
		return ""
	}
	bottom := l.EditorTop + l.EditorRows
	if l.StatusRow > 0 && bottom > l.StatusRow {
		bottom = l.StatusRow
	}
	scr := paint.grid()
	rows := make([]string, 0, bottom-l.EditorTop)
	for r := l.EditorTop; r < bottom && r < keyRefusalPtyRows; r++ {
		rows = append(rows, strings.TrimRight(scr.Line(r), " "))
	}
	return strings.Join(rows, "\n")
}
