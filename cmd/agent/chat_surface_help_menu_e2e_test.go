package main

// chat_surface_help_menu_e2e_test.go — THE OPERATOR'S OWN ACT, reproduced with
// zero keystrokes from an operator: a live `gopi chat` REPL runs its own line
// editor on its own pty, and the ONLY actor is a mock model whose whole output is
// chat_surface action=key. The keys it injects are exactly the operator's manual
// sequence —
//
//	inject "/h"      (a PREFIX, not a command: /help and nothing else matches, so
//	                  the menu opens with ONE row and the note row under it)
//	inject "down"    (Arrow-Down: on a one-item list the selection CLAMPS, so the
//	                  grid must come back INTACT rather than mangled)
//	inject "enter"   (the menu owns Enter: it ACCEPTS /help, and the editing row
//	                  carries the completed command)
//
// and every assertion is read off the PAINTED CELL GRID (Rule 10.2), never off the
// bytes and never off the editor's struct fields: "the menu opened", "the sole item
// is /help", "the Down left the screen intact" are statements about cells.
//
// WHY THIS CASE EXISTS ON TOP OF chat_surface_key_e2e_test.go. That case proves an
// injected key REACHES the run's own editor as a keystroke (the rune opens the menu,
// Down moves a MULTI-item selection). It cannot prove the operator's act, because
// the sequence's outcome there is a menu with several items — and the reported defect
// this epic exists for was measured on a ONE-item list, where Up/Down are inert and
// the third key (Enter) has to mean ACCEPT rather than submit. The three keys here
// are the operator's own manual verification, step for step.
//
// ONE KEY PER MODEL ROUND, AND THE TEST HOLDS THE GATE BETWEEN THEM. The keys are
// injected by the model (the deliverable's "zero operator keystrokes"), but the acts
// must be OBSERVED one at a time: a model that fires all three keys in one round
// leaves the test racing the injections, and "the grid was intact after the Down" is
// not observable if the Enter may already have landed. So each round after the first
// BLOCKS until the test releases it, having asserted the previous act on the grid.
// The SSE body is the gate — the mock is still the only actor, and the assertions run
// on a settled screen with nothing in flight behind them.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/toolnames"
)

const (
	// helpMenuCols / helpMenuRows are the terminal the acceptance runs on — wide
	// enough that no menu row is truncated (a truncated row would turn the "sole
	// item" assertion into a statement about the scissors).
	helpMenuCols = 100
	helpMenuRows = 30
	// helpMenuPrefix is the operator's first key sequence: a PREFIX whose only
	// match is /help. It is deliberately not the whole word — the case is about
	// the MENU, and "/h" is what the operator actually typed.
	helpMenuPrefix = "/h"
	// helpMenuCommand is the one word that prefix matches, as the menu must offer
	// it: the slash-prefixed spelling the completer publishes.
	helpMenuCommand = "/help"
	// helpMenuWait bounds every wait for an injected key to reach the painted grid.
	// It is generous because the keys are PACED (one per read, a paste window
	// apart) and a pty round trip sits between two of them.
	helpMenuWait = 10 * time.Second
	// helpMenuStep is that wait's polling interval.
	helpMenuStep = 5 * time.Millisecond
	// helpMenuStablePolls is how many consecutive identical samples count as "the
	// grid has settled" (see settle). Two is the minimum that can distinguish a
	// finished frame from one still being drawn; three leaves room for a poll that
	// lands inside a single paint's own chunking.
	helpMenuStablePolls = 3
	// helpMenuModel names the model the mock stands in for.
	helpMenuModel = "deepseek-v4-pro"
	// helpMenuBootstrap is the session's FIRST line — typed on the pty master (the
	// one line a real user must supply: the REPL runs no round before a line
	// arrives, so there is nothing for an injection to ride until then) and NOT a
	// slash command, because a slash command is dispatched locally and would never
	// open a model round at all.
	helpMenuBootstrap = "hello"
	// helpMenuAnswer is what the rounds AFTER the last injection answer with, so
	// the turn drains and the transcript is deterministic.
	helpMenuAnswer = "standing by."
)

// helpMenuActKeys is the operator's act, in order. It is a FUNCTION rather than a
// shared variable so no case can mutate the slice another case reads.
func helpMenuActKeys() []string {
	return []string{helpMenuPrefix, lineedit.KeyNameDown, lineedit.KeyNameEnter}
}

// helpMenuRoundSSE streams ONE round of tool_use blocks — a single key in its own
// model round, which is what makes each act separately observable (see the file
// header). Every round carries the text delta the mock's own prose would have.
func helpMenuRoundSSE(w io.Writer, key string, round int) {
	testutil.WriteClaudeSSE(w, "message_start", `{"type":"message_start","message":{"id":"m1","usage":{"input_tokens":10,"output_tokens":1}}}`)
	testutil.WriteClaudeSSE(w, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
	testutil.WriteClaudeSSE(w, "content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"driving the menu (%d)"}}`, round))
	testutil.WriteClaudeSSE(w, "content_block_stop", `{"type":"content_block_stop","index":0}`)
	args, _ := json.Marshal(map[string]string{"action": agent.ChatSurfaceActionKey, "text": key}) //stylegate:ignore marshal of the help-menu key frame the fixture sends on purpose; a marshal error would be a test-authoring bug
	block := fmt.Sprintf(`{"type":"tool_use","id":"call_%02d","name":%q,"input":%s}`, round, toolnames.ToolChatSurface, args)
	testutil.WriteClaudeSSE(w, "content_block_start", fmt.Sprintf(`{"type":"content_block_start","index":1,"content_block":%s}`, block))
	testutil.WriteClaudeSSE(w, "content_block_stop", `{"type":"content_block_stop","index":1}`)
	testutil.WriteClaudeSSE(w, "message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":10,"output_tokens":7}}`)
	testutil.WriteClaudeSSE(w, "message_stop", `{"type":"message_stop"}`)
}

// helpMenuGrid is the acceptance's live view: every byte the editor painted,
// replayed into the package's OWN vt100 model, so the assertions read cells.
type helpMenuGrid struct {
	mu   sync.Mutex
	data []byte
}

// Write records a paint chunk.
func (g *helpMenuGrid) Write(b []byte) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.data = append(g.data, b...)
	return len(b), nil
}

// screen replays everything painted so far into a fresh grid at the pty's size.
func (g *helpMenuGrid) screen() *lineedit.Screen {
	g.mu.Lock()
	snapshot := append([]byte(nil), g.data...)
	g.mu.Unlock()
	scr := lineedit.NewScreen(helpMenuCols, helpMenuRows)
	scr.Feed(snapshot)
	return scr
}

// rows renders the current grid, one cell-row per line.
func (g *helpMenuGrid) rows() string { return g.screen().Contents() }

// painted copies the bytes painted SO FAR. It is the "did that key produce any
// write at all" instrument: a key whose effect is invisible in the cells (a clamped
// arrow on a one-item list) still repaints, so the arrival of NEW bytes is what
// separates "consumed and clamped" from "never arrived".
func (g *helpMenuGrid) painted() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.data)
}

// helpMenuSession is the live run under test.
type helpMenuSession struct {
	in     *chatInput
	paint  *helpMenuGrid
	pair   chatPtyPair
	term   *lineedit.Terminal
	exited chan struct{}
	// ledger is the announcement record this session's run is observed through,
	// when the case installs one (chatInput.announce). Nil means the case makes no
	// exact-count claim — the painted stream is then the only record, and it is the
	// right one for "did this wording reach the console" (see sawNoticeOnce).
	ledger announcementLedger
}

// rows renders the current grid.
func (s *helpMenuSession) rows() string { return s.paint.rows() }

// input is the live input layer of this run — the editor keyE2EWaitEnterGap asks
// about (see the keyE2ESession contract in chat_surface_key_e2e_test.go). It is the
// same producer-side input object the sibling key case exposes, never a copy.
func (s *helpMenuSession) input() *chatInput { return s.in }

// render is the grid as text, used only to make a failed wait's message show what
// was on screen.
func (s *helpMenuSession) render() string { return s.rows() }

// waitFor polls the painted grid until cond holds, dumping it on failure — the
// failure output IS the evidence, so a red case shows exactly what was on screen.
func (s *helpMenuSession) waitFor(t *testing.T, what string, cond func(string) bool) {
	t.Helper()
	deadline := time.Now().Add(helpMenuWait)
	for time.Now().Before(deadline) {
		if cond(s.rows()) {
			return
		}
		time.Sleep(helpMenuStep) //stylegate:allow-sleep bounded poll (helpMenuWait) for an injected keystroke to reach the painted grid
	}
	t.Fatalf("timed out waiting for %s\n--- grid ---\n%s", what, s.rows())
}

// settle waits until the painted grid stops changing across consecutive polls, so a
// claim about the cells is judged on a FINISHED frame rather than mid-draw. Without
// it the comparison is a race with the editor's own async repaint (measured: the
// "Down left the grid intact" leg failed intermittently because it read the screen
// while the clamped arrow's repaint was still arriving — the same bytes, read too
// early). The bound is helpMenuWait; a grid that never settles is a real failure.
func (s *helpMenuSession) settle(t *testing.T, what string) {
	t.Helper()
	deadline := time.Now().Add(helpMenuWait)
	prev, stable := "", 0
	for time.Now().Before(deadline) {
		now := s.rows()
		if now == prev {
			stable++
			if stable >= helpMenuStablePolls {
				return
			}
		} else {
			stable = 0
			prev = now
		}
		time.Sleep(helpMenuStep) //stylegate:allow-sleep bounded poll (helpMenuWait) waiting for the painted grid to settle before asserting on it
	}
	t.Fatalf("the painted grid never settled while waiting for %s\n--- grid ---\n%s", what, s.rows())
}

// layout is the editor's published layout, or a zero Layout when none is live.
func (s *helpMenuSession) layout() lineedit.Layout {
	l, ok := s.in.editor.Layout()
	if !ok {
		return lineedit.Layout{EditorTop: -1, MenuTop: -1}
	}
	return l
}

// editingLine is the editing row's painted text.
func (s *helpMenuSession) editingLine() string {
	return s.rowsRange(s.layout().EditorTop, s.layout().EditorTop+1)
}

// rowsRange renders grid rows [top, bottom) joined by "\n" — the READING SCOPE of
// this case's claims. It excludes the status row, which carries an elapsed clock by
// design: including it would make "the grid is intact" a statement about a stopwatch
// (measured — the first version compared whole grids and the clock broke the
// equality). The claim is about the menu and the editing line, so it is read there.
func (s *helpMenuSession) rowsRange(top, bottom int) string {
	if top < 0 {
		return ""
	}
	all := strings.Split(s.rows(), "\n")
	if bottom > len(all) {
		bottom = len(all)
	}
	if top >= bottom {
		return ""
	}
	out := make([]string, 0, bottom-top)
	for _, row := range all[top:bottom] {
		out = append(out, strings.TrimRight(row, " "))
	}
	return strings.Join(out, "\n")
}

// menuRows is how many rows the LAYOUT reserved for the completion menu (0 = no
// menu on screen). It is asked of the layout rather than inferred from row text: the
// notices, the answer and the status row all carry text, so "a non-blank row below
// the editing line" would call the answer a menu (measured — the sibling case's first
// version did exactly that).
func (s *helpMenuSession) menuRows() int { return s.layout().MenuRows }

// interaction is the reading scope of every claim here: the editing line plus the
// rows the menu reserved. THE LAYOUT IS READ ONCE and both bounds come from that one
// reading — asking twice would let the menu open between the two calls and produce a
// range that never existed on screen (measured: the "Down left the grid intact"
// comparison failed intermittently on exactly that tear).
// THE END IS CLAMPED AT THE STATUS ROW, and that clamp is a fix, not tidiness. The
// `+1` this replaces reached one row PAST the menu — onto the status row — so the
// compared range was "the editing line plus the menu rows PLUS a row whose content
// is an elapsed clock, a live token count and a spinner". The claim then became
// "the stopwatch read the same twice", true or false with no key involved.
// MEASURED on a real red run of this very case: the before/after pair it printed
// differed ONLY in that row —
//
//	before: … · ctx 30≡ ·↑0↓10   responding
//	after:  … · ctx 31≡ ·↑0↓20   responding
//
// — while the editing line and every menu row were byte-identical. The Down had
// clamped exactly as the one-item menu says it must; the RANGE was what lied.
func (s *helpMenuSession) interaction() string {
	l := s.layout()
	return s.rowsRange(l.EditorTop, s.menuBottom(l))
}

// menuBottom is the EXCLUSIVE end of a reading over the editing line and the
// menu's reserved rows, clamped at the status row (see interaction).
func (s *helpMenuSession) menuBottom(l lineedit.Layout) int {
	bottom := l.MenuTop + l.MenuRows
	if l.StatusRow > 0 && bottom > l.StatusRow {
		bottom = l.StatusRow
	}
	return bottom
}

// interactionWith is interaction() against a layout the caller already read, so a
// claim and its baseline are taken from the SAME reading.
func (s *helpMenuSession) interactionWith(l lineedit.Layout) string {
	return s.rowsRange(l.EditorTop, s.menuBottom(l))
}

// activeMenuRow finds the menu row carrying the ACTIVE marker, or -1 when no row
// does. The marker is the package's own constant (Rule 1b: a control that retyped
// ">" would keep passing after a re-glyph).
func (s *helpMenuSession) activeMenuRow() int {
	l := s.layout()
	if l.MenuRows == 0 {
		return -1
	}
	scr := s.paint.screen()
	for row := l.MenuTop; row < l.MenuTop+l.MenuRows; row++ {
		if strings.HasPrefix(scr.Line(row), lineedit.MenuMarkerActive()) {
			return row
		}
	}
	return -1
}

// noteRow finds the row carrying the one-candidate menu's stated reason, or -1.
func (s *helpMenuSession) noteRow() int {
	scr := s.paint.screen()
	for row := 0; row < helpMenuRows; row++ {
		if strings.Contains(scr.Line(row), lineedit.CompletionMenuNote()) {
			return row
		}
	}
	return -1
}

// TestChatSurfaceKey_OperatorHelpMenuActDrivenOnlyByTheModel is THIS epic's
// acceptance: the operator's exact manual act — "/h", Arrow-Down, Enter — driven
// with zero operator keystrokes by a model whose only act is action=key, with the
// painted cell grid as the verdict at every step.
func TestChatSurfaceKey_OperatorHelpMenuActDrivenOnlyByTheModel(t *testing.T) {
	t.Setenv("GOPI_HOME", t.TempDir())
	t.Setenv("GOPI_MEMORY_DB", "")

	s := &helpMenuSession{exited: make(chan struct{})}
	act := helpMenuActKeys()

	// THE GATES: gate[i] is released by the test once it has asserted the act the
	// round BEFORE key i produced. Round 1 needs no gate (nothing precedes it).
	gates := []chan struct{}{make(chan struct{}), make(chan struct{})}
	// done releases a gated round when the TEST ends (including on the failure of an
	// earlier leg), so a red run reports the assertion that failed instead of hanging
	// the mock's handler and timing the whole binary out — measured: without it the
	// red proof for the text path surfaced as "panic: test timed out" rather than as
	// the failing assertion's own message.
	done := make(chan struct{})

	var bodiesMu sync.Mutex
	var bodies []string
	round := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)         //stylegate:ignore bounded read of a test request body; a short read is acceptable
		defer func() { _ = r.Body.Close() }() //stylegate:ignore the test's own request body close
		bodiesMu.Lock()
		bodies = append(bodies, string(body))
		bodiesMu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")

		// ONE KEY PER ROUND: round 1 injects the prefix, rounds 2 and 3 wait for the
		// test's gate and then inject Down and Enter, and every later round answers
		// with text so the turn drains.
		bodiesMu.Lock()
		round++
		n := round
		bodiesMu.Unlock()
		switch {
		case n <= len(act):
			if n > 1 {
				select {
				case <-gates[n-2]:
				case <-s.exited:
				case <-done:
				}
			}
			helpMenuRoundSSE(w, act[n-1], n)
		default:
			testutil.TextSSE(w, helpMenuAnswer)
		}
	}))
	t.Cleanup(srv.Close)
	// The gate release is registered BEFORE the run's teardown, so the cleanups run in
	// the order that cannot deadlock: teardown (stop the REPL, free its pty) →
	// release a parked mock round → close the pair → close the server. A red run
	// reaching t.Fatalf with a gated round parked would otherwise hold httptest's
	// connection open through srv.Close — measured as "httptest.Server blocked in
	// Close" and a whole-binary timeout instead of the failing assertion's message.
	t.Cleanup(func() { close(done) })

	pair := chatOpenPtyPair(t, helpMenuCols, helpMenuRows)
	paint := &helpMenuGrid{}
	term, err := lineedit.Open(pair.Slave, paint)
	if err != nil {
		t.Fatalf("open the pty slave as a terminal: %v", err)
	}
	s.pair, s.term, s.paint = pair, term, paint

	// THE PRODUCTION INPUT LAYER: the same constructor the REPL calls, with the REAL
	// slash completer installed, over a real pty.
	in := newChatInputWithSource(term, paint, chatPrompt)
	in.editor.SetPalette(lineedit.BasicPalette)
	in.editor.SetCompleter(NewSlashCompleter())
	if !in.editor.EnableRegion() {
		t.Fatalf("EnableRegion(pty %dx%d) = false: the acceptance needs the real chat layout", helpMenuCols, helpMenuRows)
	}
	s.in = in

	// THE PREMISE, asserted before anything is injected: the REAL completer offers
	// exactly ONE candidate for "/h", and it is /help. Without this the "sole item"
	// assertion could hold for the wrong reason (a completer offering nothing opens no
	// menu at all — which is why the case also asserts that a menu OPENED).
	cands := NewSlashCompleter()(helpMenuPrefix, len(helpMenuPrefix), 10)
	if len(cands) != 1 || cands[0].Text != helpMenuCommand {
		t.Fatalf("the REAL slash completer offers %+v for prefix %q; this acceptance is about a ONE-item menu whose sole row is %q",
			cands, helpMenuPrefix, helpMenuCommand)
	}

	sl, err := agent.NewSessionLogger(helpMenuModel, "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	t.Cleanup(func() { sl.Close() }) //stylegate:ignore test teardown of a logger this test opened

	ag := &agent.Agent{
		Client:     &deepseek.Client{BaseURL: srv.URL, APIKey: "test-key", HTTPClient: srv.Client()},
		Ctx:        &agent.Context{},
		Model:      helpMenuModel,
		Timeout:    0,
		SessionLog: sl,
	}
	prevFactory := chatAgentFactory
	t.Cleanup(func() { chatAgentFactory = prevFactory })
	chatAgentFactory = func(string) (*agent.Agent, error) { return ag, nil }

	// TEARDOWN IS REGISTERED, NOT SEQUENCED AT THE END. A red run reaches its t.Fatalf
	// before any trailing close would execute, and a live editor whose read loop is
	// never unblocked keeps its pty (and the server connection) open — so the red
	// proof would surface as a binary timeout rather than as the assertion that
	// failed. Registered here (last of the cleanup set) it runs FIRST.
	t.Cleanup(func() {
		if s.in != nil {
			s.in.editor.Close()
		}
		// THE TERMINAL IS CLOSED BEFORE the REPL is joined: the read loop is parked
		// in ReadEvent ON THE TERMINAL, so the terminal's own stop path is what ends
		// it (the ORDER is stated once in chat_pty_fixture_test.go). The old order
		// joined first and closed the terminal after — the join then observed the
		// exit of the pair's own close, on scheduling luck, and the silent `select`
		// around it could not tell a returned loop from an elapsed bound.
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

	// BOOTSTRAP: one real keyed line on the pty master — the only line an operator
	// must supply, because the REPL runs no round before a line arrives and there
	// would be nothing for an injection to ride.
	keyE2ETypeLine(t, s, pair.Master, helpMenuBootstrap)

	// ── LEG 1: the INJECTED "/h" opens the menu, whose SOLE item is /help ──────
	s.waitFor(t, "the completion menu to open", func(string) bool { return s.menuRows() > 0 })
	// THE PAINTED CELLS ARE WAITED FOR, one leg at a time, because each of these is
	// the read loop's own work arriving a frame after the state it answers: the
	// layout RESERVES the menu's rows (menuRows, above) before the loop has painted
	// them, and the injected prefix reaches the editing row on the same loop. Read
	// the instant the reservation exists, each is a sample of a screen mid-draw —
	// measured under four concurrent suites, where the reservation matched the
	// layout's arithmetic while the grid still carried neither the prefix nor the
	// menu's own row. The claims are unchanged; only the instant is.
	s.waitFor(t, "the injected prefix to be painted on the editing row", func(string) bool {
		return strings.Contains(s.editingLine(), helpMenuPrefix)
	})
	s.waitFor(t, "a menu row to carry the selection marker "+strconv.Quote(lineedit.MenuMarkerActive()), func(string) bool {
		return s.activeMenuRow() >= 0
	})
	// THE SOLE ITEM, read off the cells: the command is offered, and the list is ONE
	// candidate plus the note row (the layout's own arithmetic says so).
	s.waitFor(t, "the painted menu to offer "+strconv.Quote(helpMenuCommand), func(string) bool {
		return strings.Contains(s.interaction(), helpMenuCommand)
	})
	if got, want := s.menuRows(), 1+lineedit.CompletionNoteRow(); got != want {
		t.Fatalf("the menu reserved %d row(s), want %d (one candidate + the one-candidate note row) — the prefix must match exactly one command\n--- grid ---\n%s",
			got, want, s.rows())
	}
	// The one-candidate menu STATES why Up/Down cannot move, on a row of its own —
	// which is what makes the Down below a stated clamp rather than an inert key.
	//
	// IT IS WAITED FOR, not sampled, and the distinction is the whole family this
	// epic is about. menuRows() above reads the LAYOUT (the rows the editor
	// RESERVED for the menu); the note row is PAINTED into them by the read loop,
	// one frame later. Asserting the cells at the instant the reservation exists
	// reads a screen the loop had not finished drawing: measured under four
	// concurrent suites — the reservation matched `1+CompletionNoteRow()` while
	// this grid carried no note row at all, the menu's own row still unwritten.
	// The claim is unchanged (a one-item menu MUST state it), and waitFor dumps
	// the grid if it never arrives, so a real regression still fails loudly.
	s.waitFor(t, "the one-item menu to state "+strconv.Quote(lineedit.CompletionMenuNote())+" on its own painted row", func(string) bool {
		return s.noteRow() >= 0
	})
	s.settle(t, "the prefix's menu to finish drawing")
	// The baseline and the post-Down reading are taken through the SAME accessor, and
	// the menu's rows are part of the reading: the claim is "the one-item menu and the
	// editing line are exactly as they were", so both readings describe the same range.
	afterPrefix := s.interaction()

	// ── LEG 2: the INJECTED Arrow-Down leaves the grid INTACT ────────────────
	// On a one-item list the selection CLAMPS: the claim is NOT "the screen changed"
	// (it must not) but "the screen did not degrade". Two halves, and both are
	// measured: the key ARRIVED (MenuSelect repaints unconditionally — the buffer did
	// not change, so the read loop's own repaint gate would not fire — so a consumed
	// press produces new bytes even when the cells come back identical), and the
	// cells are unchanged.
	writesBefore := s.paint.painted()
	close(gates[0])
	s.waitFor(t, "the injected Down to be consumed (its repaint reaches the pty)", func(string) bool {
		return s.paint.painted() > writesBefore
	})
	s.settle(t, "the clamped Down's repaint to finish")
	if got := s.interaction(); got != afterPrefix {
		t.Fatalf("the Down changed the menu/editing rows on a ONE-item menu (it must clamp, not mangle)\n--- before ---\n%s\n--- after ---\n%s",
			afterPrefix, got)
	}
	if s.activeMenuRow() < 0 {
		t.Fatalf("the sole item lost its selection marker after Down\n--- grid ---\n%s", s.rows())
	}
	if got := s.editingLine(); !strings.Contains(got, helpMenuPrefix) {
		t.Fatalf("the Down disturbed the editing line: %q\n--- grid ---\n%s", got, s.rows())
	}

	// ── LEG 3: the INJECTED Enter is the menu's ACCEPT, read off the grid ─────
	// The menu owns Enter (an open menu means "take this one"), so the line becomes
	// the command — and NOT a submit: the editing row carries /help in full.
	close(gates[1])
	s.waitFor(t, "the injected Enter to complete the lone candidate onto the editing row", func(string) bool {
		return strings.Contains(s.editingLine(), helpMenuCommand)
	})
	if got := strings.TrimSpace(s.editingLine()); !strings.HasSuffix(got, helpMenuCommand) {
		t.Fatalf("the editing row after Enter = %q, want it to carry %q", got, helpMenuCommand)
	}
	// The menu re-publishes candidates for the COMPLETED line (the editor's own
	// documented behaviour: "/task leaves a menu offering exactly /task"), so the sole
	// offer is now the full command — which is also the proof that the accept did not
	// throw the list away.
	if !strings.Contains(s.interaction(), helpMenuCommand) {
		t.Fatalf("after the accept the grid no longer offers %q\n--- grid ---\n%s", helpMenuCommand, s.rows())
	}

	// ── THE INJECTION REALLY HAPPENED (the positive half) ────────────────────
	// A run whose action=key calls were all REFUSED would have opened no menu and
	// failed earlier — but the tool RESULTS are the direct statement of what the tool
	// did, so they are read back from the mock's own record of the requests: each
	// round after an injection carries that key's result, and every one must report an
	// injection rather than a refusal.
	s.waitFor(t, "the turn carrying the tool results to complete", func(string) bool {
		return strings.Contains(s.rows(), helpMenuAnswer)
	})
	bodiesMu.Lock()
	recorded := append([]string(nil), bodies...)
	bodiesMu.Unlock()
	if len(recorded) < len(act)+1 {
		t.Fatalf("the model saw %d round(s), want at least %d (one per injected key, plus the answer)", len(recorded), len(act)+1)
	}
	// THE COUNT IS READ FROM THE LAST REQUEST, never summed over all of them: each
	// request replays the conversation, so the tool results ACCUMULATE in the history
	// and a sum reports 1+2+3 for a three-key act (measured — the first version of
	// this check did exactly that and read as if keys were duplicated).
	last := recorded[len(recorded)-1]
	injections := strings.Count(last, "injected=")
	refusals := strings.Count(last, "action=key refused")
	if injections != len(act) {
		t.Fatalf("the final request carries %d injection report(s) in its tool results, want exactly %d (one per key of the act) — a refusal leaves the count short:\n%s", injections, len(act), last)
	}
	if refusals != 0 {
		t.Fatalf("an injected key was refused %d time(s):\n%s", refusals, last)
	}

	// END THE SESSION through the SAME seam, with the one key whose effect is to close
	// the editor (Result.Interrupted → the REPL leaves cleanly). It is injected rather
	// than typed on the master, so the whole interaction — including the way out —
	// stays on the path under test.
	if res := s.in.injector.InjectKey(lineedit.KeyNameCtrlC); !res.Accepted {
		t.Fatalf("injecting ctrl-c refused: %s", res.Why)
	}
	select {
	case <-s.exited:
	case <-time.After(helpMenuWait):
		t.Fatalf("the REPL did not finish after the injected ctrl-c\n--- grid ---\n%s", s.rows())
	}

	t.Logf("the model drove the operator's act %v through chat_surface action=%s with zero operator keystrokes: %q opened a %d-row menu (sole item %q + the note row %q), Down clamped and left the interaction rows byte-identical, Enter accepted and left %q on the editing row — %d injection report(s), 0 refusals, across %d model round(s)",
		act, agent.ChatSurfaceActionKey, helpMenuPrefix, 1+lineedit.CompletionNoteRow(), helpMenuCommand,
		lineedit.CompletionMenuNote(), helpMenuCommand, injections, len(recorded))
}
