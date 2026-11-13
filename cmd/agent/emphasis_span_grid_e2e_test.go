package main

// emphasis_span_grid_e2e_test.go — the inline-span defect at the FULL STACK, at the
// operator's own width.
//
// THE DEFECT (measured 2026-09-29 on a live 117-column chat). A `**…**` span's bold
// cells did not match the span's characters: the marked run started early, ended
// late, and the boundary landed in the middle of a multi-byte character. The shape
// that makes it reachable is the STREAM: the model's answer arrives in deltas, and a
// delta boundary can fall INSIDE a marker pair (`a\n` | `**b` | `**`). The row the
// band finally commits then carried BOTH the provisional paint and the decided text
// (`**b*b`), and nothing repaints a committed row.
//
// WHY THIS CASE IS AT THE FULL STACK RATHER THAN AT THE PACKAGE. The package gate
// (internal/lineedit/emphasis_span_grid_test.go) drives the renderer directly and is
// where the fix's regression lives. What it cannot show is the half that made this a
// reported defect: that a REAL `gopi chat` — the production REPL, the production read
// loop, the production input layer, a model answering over SSE in chunks — puts those
// cells on the console. So this case runs the real REPL on a real pty at 117 columns,
// has a mock model stream an answer whose emphasis pair is cut by a delta boundary,
// and reads the PAINTED GRID cell by cell.
//
// THE ASSERTION IS PER CELL, and against the tokenizer — never a string equality on
// the answer. The rows' CHARACTERS are right; it is their ATTRIBUTES that moved, so
// the case compares each painted cell's Bold flag against LineSpans' own decision for
// the source, and reports a text failure and a bold failure apart.

import (
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
)

const (
	// spanE2ECols / spanE2ERows are the operator's capture geometry. The width is
	// the reported one (the live dump read `cols=117`); a width the 80-column
	// default would mis-read is required, and the drift is wrap-related, so the
	// number is the case's subject rather than a convenience.
	spanE2ECols = 117
	spanE2ERows = 30
	// spanE2EModel answers over SSE.
	spanE2EModel = "deepseek-v4-pro"
	// spanE2EAnswer is the answer the MODEL streams: a short line, then a line whose
	// WHOLE content is an emphasis span. It is the operator's own language on purpose
	// — a CJK span is also the multi-byte case the report named — and it is chosen so
	// that the chunk below lands a delta boundary INSIDE the closing marker pair (see
	// spanE2EChunk): with `spanE2EChunk` bytes per frame and the closing pair's first
	// `*` at byte offset 8 of the second line, 8 mod 3 = 2 = chunk-1, so the pair is
	// split by construction rather than by luck.
	spanE2EAnswer = "方向\n**线索**"
	// spanE2EBootstrap is the one line an operator must supply: the REPL runs no
	// round before a line arrives, so nothing could answer without it.
	spanE2EBootstrap = "span grid"
	// spanE2EWait / spanE2EStep / spanE2EStablePolls bound the waits and decide when
	// the painted grid has settled, so a claim is judged on a FINISHED frame rather
	// than mid-draw.
	spanE2EWait        = 20 * time.Second
	spanE2EStep        = 5 * time.Millisecond
	spanE2EStablePolls = 4
	// spanE2ECloseWait WAS the teardown's silent fallback wait for the REPL to
	// leave. The teardown now joins through chatJoinReadLoop — a returned loop is
	// REPORTED, not waited out, and the four-step order lives in one place
	// (chat_pty_fixture_test.go) — so a bound here would only be a silent wait, and
	// a silent wait is the defect this epic removed. The constant went with it
	// rather than staying as an unused knob (Rule 15, Rule 1).
	// spanE2EChunk is the SSE frame size: TWO bytes, so a delta boundary falls inside
	// a marker pair by construction. The pair is `**` (two bytes) and the source line
	// is `**线索**`, so frames of two split it as `**|线|索|**` — the boundary lands
	// exactly at the pair's end, which is the reported shape. It is not a lucky
	// constant: the case was swept over 1..16 frame sizes against the UNFIXED renderer
	// and this is one of the sizes that reproduced the defect end to end (2, 5, 8 and
	// 16 all do; 1, 3 and 4 do not, because at those cadences the boundary happens to
	// miss the pair). Two is kept because it is the smallest reproducing cadence and a
	// realistic token cadence.
	spanE2EChunk = 2
)

// spanE2ECell is one painted cell: its rune and whether the terminal drew it Bold.
type spanE2ECell struct {
	r    rune
	bold bool
}

// spanE2ECells reads the whole grid as a flat cell sequence, folding away the turn's
// chrome: the echoed prompt row (RequestGutter, bold by palette design), the turn
// header's rule rows, and the two-cell block gutter every body row opens with.
func spanE2ECells(sc *lineedit.Screen) []spanE2ECell {
	var out []spanE2ECell
	inBlock := false
	for row := 0; row < sc.Rows; row++ {
		text, styles := sc.StyledLine(row)
		if text == "" {
			continue
		}
		if strings.HasPrefix(text, lineedit.RequestGutter) || strings.HasPrefix(text, lineedit.TurnRuleGlyph) {
			continue
		}
		// INSIDE THE TURN'S OWN BLOCK, and nowhere else. The band above the block
		// carries the banner, whose cells are as valid as any other's — and whose
		// text happens to contain the answer's own characters (measured: a scan of the
		// whole grid anchored on the answer's first rune matched a banner row and
		// reported "the drawn characters are wrong"). The turn's block is found by its
		// OWN opening glyph: collection starts at the first row that opens with the
		// marker and stops at the first row after it that opens with neither the
		// marker nor the continuation indent.
		if !inBlock {
			if !strings.HasPrefix(text, lineedit.TurnMarker) {
				continue
			}
			inBlock = true
		} else if !strings.HasPrefix(text, lineedit.TurnMarker) && !strings.HasPrefix(text, "  ") {
			break
		}
		runes := []rune(text)
		body := 0
		if strings.HasPrefix(text, lineedit.TurnMarker) || strings.HasPrefix(text, "  ") {
			body = len([]rune(lineedit.TurnMarker))
		}
		for i := body; i < len(runes); i++ {
			if runes[i] == []rune(lineedit.AnswerCutLabel)[0] {
				continue
			}
			out = append(out, spanE2ECell{r: runes[i], bold: styles[i].Bold})
		}
	}
	return out
}

// spanE2EWant is the answer as the FORMAT layer decided it, from the LIVE tokenizer —
// never a hand-written literal, which could agree with a renderer that shifted it.
func spanE2EWant(src string) []spanE2ECell {
	var out []spanE2ECell
	for _, line := range strings.Split(strings.TrimSuffix(src, "\n"), "\n") {
		for _, sp := range lineedit.LineSpans(line) {
			for _, r := range sp.Text {
				out = append(out, spanE2ECell{r: r, bold: sp.Emphasis == lineedit.EmphasisBold})
			}
		}
	}
	return out
}

// TestChatEmphasisSpanCellsMatchTheSourceAtTheOperatorWidth runs a REAL `gopi chat`
// on a 117-column pty, has the model stream an answer whose emphasis pair a delta
// boundary cuts, and asserts the painted cells against the tokenizer — rune by rune,
// flag by flag.
func TestChatEmphasisSpanCellsMatchTheSourceAtTheOperatorWidth(t *testing.T) {
	t.Setenv("GOPI_HOME", t.TempDir())
	t.Setenv("GOPI_MEMORY_DB", "")

	s := &helpMenuSession{exited: make(chan struct{})}

	var mu sync.Mutex
	round := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)         //stylegate:ignore bounded read of a test request body; a short read is acceptable
		defer func() { _ = r.Body.Close() }() //stylegate:ignore the test's own request body close
		_ = body
		w.Header().Set("Content-Type", "text/event-stream")
		mu.Lock()
		round++
		n := round
		mu.Unlock()
		if n == 1 {
			// Round 1 answers with the fixture, ONE BYTE PER FRAME, so a delta
			// boundary falls inside the `**` pair by construction.
			testutil.TextSSEChunked(w, spanE2EAnswer, spanE2EChunk)
			return
		}
		testutil.TextSSE(w, "standing by.")
	}))
	t.Cleanup(srv.Close)

	pair := chatOpenPtyPair(t, spanE2ECols, spanE2ERows)
	paint := &helpMenuGrid{}
	term, err := lineedit.Open(pair.Slave, paint)
	if err != nil {
		t.Fatalf("open the pty slave as a terminal: %v", err)
	}
	s.pair, s.term, s.paint = pair, term, paint

	// THE PRODUCTION INPUT LAYER: the same constructor the REPL calls, over a real
	// pty at the operator's width.
	in := newChatInputWithSource(term, paint, chatPrompt)
	in.editor.SetPalette(lineedit.BasicPalette)
	if !in.editor.EnableRegion() {
		t.Fatalf("EnableRegion(pty %dx%d) = false: the acceptance needs the real chat layout", spanE2ECols, spanE2ERows)
	}
	s.in = in

	sl, err := agent.NewSessionLogger(spanE2EModel, "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	t.Cleanup(func() { sl.Close() }) //stylegate:ignore test teardown of a logger this test opened

	ag := &agent.Agent{
		Client:     &deepseek.Client{BaseURL: srv.URL, APIKey: "test-key", HTTPClient: srv.Client()},
		Ctx:        &agent.Context{},
		Model:      spanE2EModel,
		Timeout:    0,
		SessionLog: sl,
	}
	prevFactory := chatAgentFactory
	t.Cleanup(func() { chatAgentFactory = prevFactory })
	chatAgentFactory = func(string) (*agent.Agent, error) { return ag, nil }

	// TEARDOWN IS REGISTERED, NOT SEQUENCED AT THE END, and it WAITS for the REPL to
	// finish. A red run reaches its t.Fatalf before any trailing close would execute,
	// and a live editor whose read loop is never unblocked keeps its pty — so the
	// red proof would surface as a binary timeout instead of the assertion that
	// failed. The wait matters as much as the close: closing the editor ends the read
	// loop, but the REPL still has to LEAVE (its chat row's close-out) before this
	// package's own close-out sweep measures the goroutine set — without the wait the
	// sweep intermittently reports the REPL's in-flight goroutine as parked, and a
	// PASSING case is reported as a FAIL (measured 2026-09-29: 1 red run in 10, with
	// `PASS` printed above the sweep's dump).
	// THE TEARDOWN ORDER (chat_pty_fixture_test.go states it once, with the
	// measurement): editor → THE STOP (the terminal) → the join → the descriptors.
	// The join here is bounded and LOUD: a loop that does not return after its
	// terminal was closed is reported, not waited out (Rule 15) — the silent
	// `select` this replaces could not tell a returned loop from an elapsed bound,
	// which is exactly the confusion the old order encoded.
	t.Cleanup(func() {
		if s.in != nil {
			s.in.editor.Close()
		}
		if !term.Stopped() {
			_ = term.Close() //stylegate:ignore the test's own terminal teardown
		}
		chatJoinReadLoop(t, s.exited, func() string { return runState(s.exited) })
		_ = pair.Master.Close() //stylegate:ignore the test's own pty master teardown
	})

	go func() {
		defer close(s.exited)
		_ = runChatWithInput([]string{"--tools"}, in)
	}()

	// BOOTSTRAP: the one line an operator supplies — the REPL runs no round before a
	// line arrives, so there would be nothing for the model's answer to ride.
	keyE2ETypeLine(t, s, pair.Master, spanE2EBootstrap)

	// THE MODEL'S ANSWER REACHES THE GRID, and the grid SETTLES: the claim is about the
	// cells of a finished frame, so the case waits for the answer's own text first and
	// then for two consecutive identical readings.
	s.waitFor(t, "the streamed answer to reach the grid", func(string) bool {
		return strings.Contains(s.rows(), spanE2EAnswerProbe())
	})
	s.settle(t, "the streamed answer's cells")

	// PER CELL: the painted runes and each one's Bold flag against the tokenizer.
	want := spanE2EWant(spanE2EAnswer)
	got := spanE2ECells(paint.screen())
	if len(got) == 0 {
		t.Fatalf("the grid painted no body cell at all\n--- grid ---\n%s", s.rows())
	}
	// Compare the BOLD decision per character of the answer, wherever it landed: the
	// grid may carry the turn's other chrome rows and the prompt echo, so walk the
	// painted cells looking for the answer's own sequence rather than asserting the
	// two runs are equal from index zero.
	start := -1
	for i := 0; i+len(want) <= len(got); i++ {
		if got[i].r == want[0].r {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("the grid does not spell the answer's first character %q anywhere\n--- grid ---\n%s",
			string(want[0].r), s.rows())
	}
	if len(got) < start+len(want) {
		t.Fatalf("the grid spells %d cell(s) from %q, the answer needs %d — the answer is TRUNCATED on screen\n--- grid ---\n%s",
			len(got)-start, string(want[0].r), len(want), s.rows())
	}
	for i := range want {
		if got[start+i].r != want[i].r {
			t.Fatalf("the grid's cell %d after %q is %q, the answer has %q — the drawn CHARACTERS are wrong\n--- grid ---\n%s",
				i, string(want[0].r), string(got[start+i].r), string(want[i].r), s.rows())
		}
	}
	plain, stray := 0, 0
	for i := range want {
		if got[start+i].bold != want[i].bold {
			if want[i].bold {
				t.Logf("cell %d %q: the source's span marks it BOLD, the terminal drew it plain", i, string(want[i].r))
				plain++
				continue
			}
			t.Logf("cell %d %q: the source's span leaves it plain, the terminal drew it BOLD", i, string(want[i].r))
			stray++
		}
	}
	if plain > 0 || stray > 0 {
		t.Fatalf("the emphasis span's cells do NOT match the source: %d of the span's characters came out plain and %d foreign cell(s) carry Bold —\n  the streamed answer %q must paint as:\n    %s\n--- grid ---\n%s",
			plain, stray, spanE2EAnswer, spanE2ESpell(want), s.rows())
	}
	t.Logf("the live REPL at %dx%d painted the model's answer %q with the span's cells exactly as the tokenizer decided: %s",
		spanE2ECols, spanE2ERows, spanE2EAnswer, spanE2ESpell(want))
	spanE2EEndSession(t, s)
}

// spanE2EEndSession ends the REPL through the SAME seam the sibling key case uses —
// the one key whose effect is to close the editor — and waits for the process to
// leave. Ending it IN THE CASE (not only in teardown) is what makes the wait bounded
// and the close-out deterministic: the read loop ends, the REPL leaves, and this
// package's goroutine sweep measures a settled set.
func spanE2EEndSession(t *testing.T, s *helpMenuSession) {
	t.Helper()
	if res := s.in.injector.InjectKey(lineedit.KeyNameCtrlC); !res.Accepted {
		t.Logf("injecting ctrl-c to end the session refused: %s (teardown still closes the editor)", res.Why)
	}
	select {
	case <-s.exited:
	case <-time.After(spanE2EWait):
		t.Logf("the REPL did not leave within %s after ctrl-c; teardown closes the editor", spanE2EWait)
	}
}

// spanE2EAnswerProbe is a fragment of the answer that only the ANSWER can put on the
// grid: the emphasis span's own text with its markers stripped, so a wait on it cannot
// be satisfied by the source markers themselves.
func spanE2EAnswerProbe() string {
	for _, sp := range lineedit.LineSpans(strings.Split(spanE2EAnswer, "\n")[1]) {
		if sp.Emphasis == lineedit.EmphasisBold {
			return sp.Text
		}
	}
	return "\u0000"
}

// spanE2ESpell renders the expected side for a failure message: the marked cells
// bracketed, so the report says WHICH cells the span is about.
func spanE2ESpell(want []spanE2ECell) string {
	var b strings.Builder
	inBold := false
	for _, c := range want {
		if c.bold != inBold {
			if c.bold {
				b.WriteString("[")
			} else {
				b.WriteString("]")
			}
			inBold = c.bold
		}
		b.WriteRune(c.r)
	}
	if inBold {
		b.WriteString("]")
	}
	return b.String()
}
