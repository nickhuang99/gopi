package main

// chat_surface_key_e2e_test.go — the REAL stack, end to end: `gopi chat`'s own
// REPL (runChatWithInput), its own line editor on a real pty, its own
// chatSurface implementation, and a model whose ONLY act is action=key.
//
// WHY THIS CASE IS THE ONE THAT MATTERS (Rule 31). The unit cases prove the
// injector drives its own editor, and the wiring case proves the constructor
// installs the seam. Neither proves what a user's session actually does: that the
// tool's action=key reaches THIS RUN's editor and that the key then behaves as a
// keystroke there. So nothing is stubbed between the tool and the editor —
//
//	the agent  = the REAL Agent the REPL builds, with the REAL chatSurface
//	             installed by installChatSurface over THIS run's input layer
//	the input  = the REAL chatInput from newChatInputWithSource
//	the editor = the REAL lineedit editor with the REAL slash completer, on a pty
//	the model  = a mock that calls chat_surface action=key — the only fake here
//
// and every assertion is about the SCREEN the editor painted, read back from the
// paint stream: the menu an injected "/" opened, the selection an injected Down
// moved, the candidate an injected Enter accepted, the line an injected Enter
// submitted.
//
// THE WAITS HERE ARE ON THEIR CLAIM'S SUBJECT, and the reason is written out in
// devdoc/chat-surface-key-flake.md — a case whose wait asks "did the screen change"
// instead of "did the selection move" passes with the key it claims to test
// DELETED, and a reading range that reaches the status row compares two stopwatches.
// Both mistakes were made here. Read that document before relaxing a wait below.
//
// The turn's tool rounds run on a different goroutine from the REPL's read loop,
// which is the production arrangement: the model reasons while the user's editor
// sits at the prompt. That is also why this case is worth its cost — the key has
// to work while the turn that asked for it is still in flight.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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
	// keyE2EPtyCols / keyE2EPtyRows are the terminal the session runs on.
	keyE2EPtyCols = 90
	keyE2EPtyRows = 24
	// keyE2EMenuRune opens the real slash-command menu — a RUNE, which no named
	// key produces, and the byte action=type could never deliver as a keystroke.
	keyE2EMenuRune = "/"
	// keyE2EWait bounds every wait for an injected key to reach the screen.
	keyE2EWait = 8 * time.Second
	// keyE2EStep is that wait's polling interval.
	keyE2EStep = 5 * time.Millisecond
	// keyE2ETextAfter is the benign answer the rounds AFTER the injections give,
	// so the session can finish.
	keyE2ETextAfter = "the keys were injected."
	// keyE2EModel names the model the mock stands in for.
	keyE2EModel = "deepseek-v4-pro"
	// keyE2ESettlePolls is how many CONSECUTIVE identical readings of the
	// interaction rows count as "the editor has finished drawing". It is what
	// makes a baseline a picture of a finished frame rather than of a frame
	// still arriving: reading the screen while the menu's own paint is in
	// flight captures a PARTIAL menu, and any comparison against that baseline
	// is then satisfied by the rest of the same draw — i.e. by a repaint that
	// has nothing to do with the key being waited for. Two is the minimum that
	// can tell a finished frame from one being drawn; three leaves room for a
	// poll landing inside a single paint's own chunking.
	keyE2ESettlePolls = 3
)

// keyE2EInjectedKeys is the interaction the model drives, in ONE round: the rune
// that opens the completion menu and the Down that moves its selection. Both are
// charged against the session ceiling and delivered while the requesting turn is
// still running — the hard case, and the only one a live session is in.
//
// WHY NOT ALSO THE TWO ENTERS HERE: this case's subject is whether an injected key
// REACHES THE RUN'S OWN EDITOR as a keystroke, and the menu's open+move is the leg
// that can only be reached that way (the package case closes the accept+submit
// leg on the grid, and the wiring case covers Backspace). Adding a submit here
// would end the session in the middle of the assertions.
func keyE2EInjectedKeys() []string {
	return []string{keyE2EMenuRune, lineedit.KeyNameDown}
}

// keyE2EBootstrap is the session's FIRST line. It is NOT injected: the REPL runs
// its first model round only after a line has been submitted (cli_chat.go blocks
// on the line channel before any turn), so something has to open the session —
// and the thing that opens it is a real keyed line on the pty master, exactly as a
// user's first line is. Everything AFTER it is the model's own injection.
//
// It is a PLAIN line, not a slash command: a slash command is dispatched locally
// and never reaches the model (measured — a `/help` bootstrap printed the usage
// table and ran no turn at all, so there was nothing for the injections to ride).
const keyE2EBootstrap = "hello"

// keyE2ETypeLine types a line on the pty master THE WAY A KEYBOARD DOES: the
// characters first, then — once the editor's own paste verdict on them has
// expired — the Enter that submits.
//
// WHY THE TWO WRITES CANNOT BE ONE, and why this helper is the honest way to write
// a line in this file: writing "/help\r" in ONE syscall is five contiguous
// characters followed by a CR, which is exactly the shape of a PASTE. The editor
// classifies it as one (PasteBurstMinChars inside PasteBurstCharInterval), the CR
// therefore arrives as pasted CONTENT rather than as a submit, and the line is
// never sent — measured in this very case: the first version wrote the whole line
// at once and the session sat at "you> /help" until the wait expired. That is the
// same classifier the injected keys have to respect, which is the subject of this
// file (see internal/lineedit/inject.go's header).
//
// `session` is REQUIRED, and that is the fix this signature carries: the pause in
// the middle is not a duration, it is a question about the run's own editor
// (see keyE2EWaitEnterGap), so a caller that cannot name its editor cannot be
// served a correct pause — and the three cases that call this helper each hold
// their own session already (cmd/agent's pty cases over the producer's own input
// layer).
func keyE2ETypeLine(t *testing.T, session keyE2ESession, master *os.File, line string) {
	t.Helper()
	// THE CHILD MUST BE READING BEFORE ANYTHING IS TYPED. These cases write to a
	// pty master whose SLAVE is opened by the run under test, and they start that
	// run with `go runChatWithInput(...)` — so at this point the child may not have
	// entered raw mode or reached its first read yet, and the bytes written here
	// would sit in the terminal's input queue (or be consumed by line discipline)
	// instead of being read as this line. The observable is the PROMPT: the run
	// paints `you>` as the last thing before it reads, so its presence on the grid
	// is the fact "boot is over and the editor is waiting".
	//
	// It is bounded rather than awaited forever, and the bound is a WAIT's, not an
	// assertion's: a run that never paints its prompt is a real failure and this
	// says so with the grid attached. Without this wait the buffer poll below
	// carried the whole of boot inside a bound derived from a PASTE-POLICY
	// constant (PasteEnterSuppressWindow × 3), which is a fact about keystroke
	// bursts rather than about a process starting — measured under four concurrent
	// suites, where the child was still painting its startup banner when the poll
	// expired, and the case failed with the banner on screen and no prompt at all.
	keyE2EWaitPrompt(t, session)
	if _, err := master.WriteString(line); err != nil {
		t.Fatalf("write %q to the pty master: %v", line, err)
	}
	keyE2EWaitEnterGap(t, session, line)
	if _, err := master.WriteString(string([]byte{crByteForTest})); err != nil {
		t.Fatalf("write the Enter for %q to the pty master: %v", line, err)
	}
}

// keyE2EWaitPrompt waits until the run has painted its input prompt, i.e. until
// the editor is waiting for a line. See keyE2ETypeLine for why the write must not
// precede it.
func keyE2EWaitPrompt(t *testing.T, session keyE2ESession) {
	t.Helper()
	deadline := time.Now().Add(keyE2EBootGap)
	// The prompt is compared WITHOUT its trailing separator: Screen.Line() trims
	// the row's trailing blanks (the prompt is `you> ` plus the cursor), so the
	// painted row reads `you>`. Matching the untrimmed constant would wait for a
	// byte the grid never carries — measured: the prompt WAS on screen while this
	// wait expired on the trailing space.
	want := strings.TrimRight(chatPrompt, " ")
	for time.Now().Before(deadline) {
		if strings.Contains(session.render(), want) {
			return
		}
		time.Sleep(keyE2EEnterPollStep) //stylegate:allow-sleep bounded poll (keyE2EBootGap) for the run under test to paint its prompt, the observable that its read loop is waiting for a line
	}
	t.Fatalf("the run never painted its prompt %q within %v, so no line could be read by it\n--- grid ---\n%s",
		want, keyE2EBootGap, session.render())
}

// keyE2EBootGap bounds the wait for the run under test to reach its first read.
// It is a BOOT bound and is deliberately not derived from a paste constant: the
// thing being waited for is a process starting (exec, raw mode, layout, banner),
// which has nothing to do with how long a keystroke burst is treated as a paste.
const keyE2EBootGap = 10 * time.Second

// keyE2ESession is the one thing keyE2EWaitEnterGap needs from a live case: the
// editor the producer is actually reading from, plus enough to render a grid if the
// wait's own bound expires. Three sibling pty cases (this file's acceptance case,
// the bound case, the help-menu case) each build their own session type over the
// same producer, so the wait takes the interface rather than any one of them.
type keyE2ESession interface {
	// input is the live input layer of the run under test.
	input() *chatInput
	// render is the grid as text, for the failure message only.
	render() string
}

// keyE2EWaitEnterGap waits until the EDITOR's own read loop has taken the
// characters AND its paste verdict on them has expired, so the Enter about to be
// written is read as a submit.
//
// IT REPLACES A FIXED `time.Sleep(keyE2EEnterGap)` — a stylegate test-sleep — and
// the two waits below are the two facts that sleep was an estimate of, waited for
// in the order they can become true rather than guessed at once.
//
//	(a) THE CHARACTERS HAVE BEEN READ — this is a POLL, on an observable. The burst
//	    is armed by the READ LOOP (Value.NoteKeys, internal/lineedit/editor.go), not
//	    by the pty write, so until that read happens there is no state to ask about
//	    and the Enter would simply overtake the characters on the wire. Measured:
//	    with the burst flag read before the read loop had run, the flag is still
//	    false, the wait returns at once, and the submission lands BEFORE the rune —
//	    the acceptance case's first leg then times out waiting for a menu that can
//	    never open. The observable is the buffer, and it is a real one: the editor
//	    has read the line exactly when its value carries the line.
//	(b) THE WINDOW IS OUT — this part is NOT pollable from outside, and the reason
//	    is a public-surface fact, not laziness. `Value.BurstActive()` is not the
//	    predicate: it is a STICKY flag (armed at PasteBurstMinChars, cleared only by
//	    a real key or Enter) and it carries no clock at all. True to its own comment
//	    — "whether the input currently looks like a paste", the HINT a renderer may
//	    show — it stays true long after the window has closed, so polling it can
//	    only ever time out. Measured: it was still true after 360ms of polling with
//	    the line already in the buffer, and the helper failed with its own message.
//	    The window lives in `Value.suppressEnterAt`, which `Value.Enter` consults;
//	    that method is unexported, and the only public route to the same answer is
//	    calling `Value.Enter` — which submits the line this helper is about to
//	    submit. So the window is waited OUT, bounded by the editor's OWN constant
//	    rather than by a number invented here.
//
// (b) is what the sleep was for, and it is the honest half: it is a bounded wait
// for the editor's own declared window, not a guess at a comfortable margin. The
// marker carries that reason at the site.
func keyE2EWaitEnterGap(t *testing.T, session keyE2ESession, line string) {
	t.Helper()
	deadline := time.Now().Add(keyE2EEnterGap)
	for time.Now().Before(deadline) && !keyE2EEditorText(session, line) {
		time.Sleep(keyE2EEnterPollStep) //stylegate:allow-sleep bounded poll (keyE2EEnterGap) for the read loop to take the typed line into the editor's buffer — the observable that proves the paste-burst state exists at all before the window below is waited out
	}
	if !keyE2EEditorText(session, line) {
		t.Fatalf("the editor never took %q into its buffer within %v, so no Enter could submit it\n--- grid ---\n%s", line, keyE2EEnterGap, session.render())
	}
	time.Sleep(lineedit.PasteEnterSuppressWindow) //stylegate:allow-sleep bounded wait for the editor's OWN PasteEnterSuppressWindow to expire since the burst's last key: Value.suppressEnterAt (the predicate Value.Enter decides on) is unexported, and the only public route to it is calling Value.Enter, which would submit the line this helper is about to submit
}

// keyE2EEditorText reports whether the run's editor has taken `line` into its
// buffer — i.e. whether the read loop has consumed the characters written above.
func keyE2EEditorText(session keyE2ESession, line string) bool {
	in := session.input()
	return in != nil && in.editor != nil && strings.Contains(in.editor.Value().Text(), line)
}

// keyE2EEnterPollStep is the poll interval of the buffer wait. It is derived from
// the editor's intra-burst interval rather than spelled as a duration (Rule 1), so
// it stays a fraction of the window it is measuring.
const keyE2EEnterPollStep = lineedit.PasteBurstCharInterval

// keyE2EEnterGap bounds the buffer wait. It is derived from the editor's OWN
// suppression window (scaled), never spelled as a duration, so it stays valid if
// that policy changes.
const keyE2EEnterGap = lineedit.PasteEnterSuppressWindow * 3

// crByteForTest is CR (0x0d) — the byte a raw-mode terminal reports for Enter.
const crByteForTest = 0x0d

// keyE2EMockToolSSE streams ONE tool_use round carrying every injection.
func keyE2EMockToolSSE(w io.Writer, calls []string) {
	testutil.WriteClaudeSSE(w, "message_start", `{"type":"message_start","message":{"id":"m1","usage":{"input_tokens":10,"output_tokens":1}}}`)
	testutil.WriteClaudeSSE(w, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
	testutil.WriteClaudeSSE(w, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"injecting keys"}}`)
	testutil.WriteClaudeSSE(w, "content_block_stop", `{"type":"content_block_stop","index":0}`)
	for i, k := range calls {
		args, _ := json.Marshal(map[string]string{"action": agent.ChatSurfaceActionKey, "text": k}) //stylegate:ignore marshal of the key frame this fixture sends (a map of string literals), whose effect on the screen is asserted
		block := fmt.Sprintf(`{"type":"tool_use","id":"call_%02d","name":%q,"input":%s}`, i, toolnames.ToolChatSurface, args)
		testutil.WriteClaudeSSE(w, "content_block_start", fmt.Sprintf(`{"type":"content_block_start","index":%d,"content_block":%s}`, i+1, block))
		testutil.WriteClaudeSSE(w, "content_block_stop", fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, i+1))
	}
	testutil.WriteClaudeSSE(w, "message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":10,"output_tokens":7}}`)
	testutil.WriteClaudeSSE(w, "message_stop", `{"type":"message_stop"}`)
}

// keyE2EPaint is the paint sink: it records every byte the editor wrote and can
// render the CURRENT screen from them through the package's own vt100 model.
//
// It exists instead of the production ScreenDumper for one reason: a dumper
// writes an artifact and freezes frames, while this case needs to read the LIVE
// screen repeatedly. The rendering itself is not re-implemented — every byte is
// replayed into a fresh lineedit.Screen, so the grid inspected here is the same
// model the dump artifact is built from (Rule 10.2: the assertion is about cells,
// never about bytes).
type keyE2EPaint struct {
	mu   sync.Mutex
	data []byte
}

// Write records a paint chunk.
func (p *keyE2EPaint) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.data = append(p.data, b...)
	return len(b), nil
}

// grid replays everything painted so far into a fresh grid at the pty's size.
func (p *keyE2EPaint) grid() *lineedit.Screen {
	p.mu.Lock()
	snapshot := append([]byte(nil), p.data...)
	p.mu.Unlock()
	scr := lineedit.NewScreen(keyE2EPtyCols, keyE2EPtyRows)
	scr.Feed(snapshot)
	return scr
}

// rows renders the current screen, one cell-row per line.
func (p *keyE2EPaint) rows() string { return p.grid().Contents() }

// painted reports how many bytes the editor has painted so far, which is the
// editor's OWN statement that the run is still doing something. It is the second
// half of the closeout's progress pair (see keyRefusalProgress): the counters are
// read together, so "the run stopped" means neither the wire nor the console moved.
func (p *keyE2EPaint) painted() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.data)
}

// keyE2EHarness is the live session under test.
type keyE2EHarness struct {
	in     *chatInput
	paint  *keyE2EPaint
	term   *lineedit.Terminal
	pair   chatPtyPair
	done   chan int
	exited chan struct{}
	keys   []string
}

// rows renders the current grid.
func (h *keyE2EHarness) rows() string { return h.paint.rows() }

// waitFor polls the painted grid until cond holds, dumping it on failure.
func (h *keyE2EHarness) waitFor(t *testing.T, what string, cond func(string) bool) {
	t.Helper()
	deadline := time.Now().Add(keyE2EWait)
	for time.Now().Before(deadline) {
		if cond(h.rows()) {
			return
		}
		time.Sleep(keyE2EStep) //stylegate:allow-sleep bounded poll (keyE2EWait) for an injected keystroke to reach the painted grid
	}
	t.Fatalf("timed out waiting for %s\n--- grid ---\n%s", what, h.rows())
}

// editingRow is the layout's editing row, asked of the LAYOUT rather than assumed
// (EditorTop is the same field the sibling layout cases read).
func (h *keyE2EHarness) editingRow() int {
	l, ok := h.in.editor.Layout()
	if !ok {
		return -1
	}
	return l.EditorTop
}

// gridRows returns the rendered rows around the editing line.
func (h *keyE2EHarness) gridRows() []string { return strings.Split(h.rows(), "\n") }

// menuRows is how many rows the LAYOUT has reserved for the completion menu (0 =
// no menu on screen). It is asked of the layout rather than inferred from row
// text: the notice rows and the status row also carry text, so a reading based on
// "non-blank below the editing line" would call the banner a menu (measured — the
// first version of this case did exactly that).
func (h *keyE2EHarness) menuRows() int {
	l, ok := h.in.editor.Layout()
	if !ok {
		return 0
	}
	return l.MenuRows
}

// menuBottom is the EXCLUSIVE end of a reading range over the editing line and
// the menu's reserved rows: one past the menu's LAST row.
//
// It is clamped at the STATUS row, and BOTH halves of that sentence matter.
// The `+1` this replaces reached one row PAST the menu — onto the status row —
// so the range a case compared was not "the editing line plus the menu rows" but
// that range PLUS a row whose whole content is an elapsed clock, a live token
// count and a spinner. The claim then quietly became "the stopwatch read the same
// twice", which fails on its own with no key involved.
//
// THE MEASUREMENT, because this is a comparison of a real red run and not a
// theory: the sibling act case's failing leg printed a before/after pair that
// differed ONLY in that row —
//
//	before: deepseek-v4-p… ·  turn 1 · ⚙ chat_surface … · ctx 30≡ ·↑0↓10   responding
//	after:  deepseek-v4-p… ·  turn 1 · ⚙ chat_surface … · ctx 31≡ ·↑0↓20   responding
//
// — while its editing line and its menu rows were byte-identical. The key was
// never implicated; the range was.
func menuBottom(l lineedit.Layout) int {
	bottom := l.MenuTop + l.MenuRows
	if l.StatusRow > 0 && bottom > l.StatusRow {
		bottom = l.StatusRow
	}
	return bottom
}

// interaction is the reading scope of this case's claims: the editing line plus
// the rows the LAYOUT reserved for the menu. THE LAYOUT IS READ ONCE and both
// bounds come from that one reading — asking twice would let the menu open
// between the two calls and produce a range that never existed on screen.
func (h *keyE2EHarness) interaction() string {
	l, ok := h.in.editor.Layout()
	if !ok {
		return ""
	}
	return h.rowsRange(l.EditorTop, menuBottom(l))
}

// rowsRange renders grid rows [top, bottom) joined by "\n", right-trimmed.
func (h *keyE2EHarness) rowsRange(top, bottom int) string {
	if top < 0 {
		return ""
	}
	all := h.gridRows()
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

// settle waits until the INTERACTION rows stop changing across consecutive
// polls, so every claim about them is judged on a FINISHED frame.
//
// WHY THIS IS NOT OPTIONAL (this is the defect this helper closes, measured):
// the editor paints the menu and its own repaint arrives in several writes, so a
// reading taken as soon as `MenuRows > 0` observes a PARTIAL menu. The old LEG 2
// captured exactly such a reading as its baseline and then returned as soon as
// the screen differed from it — which the remainder of the menu's own draw
// satisfies. The case therefore passed WITHOUT evidence that the Down was ever
// read: with the Down removed from the injection entirely, the case still passed
// (measured, verbatim run recorded in the report).
func (h *keyE2EHarness) settle(t *testing.T, what string) {
	t.Helper()
	deadline := time.Now().Add(keyE2EWait)
	prev, stable := "", 0
	for time.Now().Before(deadline) {
		now := h.interaction()
		if now == prev {
			stable++
			if stable >= keyE2ESettlePolls {
				return
			}
		} else {
			stable = 0
			prev = now
		}
		time.Sleep(keyE2EStep) //stylegate:allow-sleep bounded poll (keyE2EWait) waiting for the painted grid to settle before asserting on it
	}
	t.Fatalf("the painted interaction rows never settled while waiting for %s\n--- grid ---\n%s", what, h.rows())
}

// editingLinePainted reports whether the editing line has been PAINTED on the row
// the layout currently gives it — i.e. whether the frame the layout describes has
// actually reached the screen.
//
// WHY THIS IS A CONDITION AND NOT A GUESS (measured, 2026-10-27). The editor
// publishes a NEW LAYOUT before it repaints the line onto the row that layout
// gives it, and the two are SEPARATE writes on the read loop:
//
//	region.layout()      ← SetMenuRows publishes EditorTop 22 → 17  (write A:
//	                        scroll region + the erased rows the band gained)
//	render.Redraw(...)   ← ONE write placing + drawing `you> /` on row 17 (write B)
//	paintMenu()          ← the candidates, below it
//
// A reader that looks at the grid BETWEEN A and B sees a layout that says row 17
// is the editing line while row 17 is still blank — the state this case used to
// read as "the injected rune never arrived". Measured by sampling the paint sink
// at every write boundary (keyE2EPaint.probe) over 6 runs: the layout publishes
// `menu=5, EditorTop=17` while that row reads "" and the NEXT write puts
// `you> /` there, byte-identical, 3 of 3 sampled transitions —
//
//	bytes=4868 menu=0 row=22 line="you> /"   ← before the publish, line on the OLD row
//	bytes=4880 menu=5 row=17 line=""         ← layout published, redraw not landed yet
//	bytes=4946 menu=5 row=17 line="you> /"   ← the redraw that fills it
//
// So the empty read is a HALF-PAINTED FRAME, never a missing rune: the buffer had
// the rune the whole time (it is what opened the menu).
//
// The PROMPT is the check rather than the buffer's text, and that is deliberate:
// Redraw composes placement + row text into ONE write (internal/lineedit/render.go),
// so a row that starts with the prompt is a row this frame has finished, while
// asking the buffer would read *Value across goroutines (the race devdoc/
// chat-surface-key-flake.md records). It is a WAIT ON THE SUBJECT — "the editing
// line is where the layout says it is" — not a retry of the assertion below: an
// injected rune that never reached the editor still fails that assertion, with the
// same message as before.
func (h *keyE2EHarness) editingLinePainted() bool {
	top := h.editingRow()
	if top < 0 {
		return false
	}
	rows := h.gridRows()
	if top >= len(rows) {
		return false
	}
	return strings.HasPrefix(strings.TrimRight(rows[top], " "), strings.TrimRight(chatPrompt, " "))
}

// waitEditingLinePainted waits (bounded, loudly) until the editing line is painted
// on the row the CURRENT layout gives it — the frame this case's claims are read
// from. See editingLinePainted for the measured tear it removes.
func (h *keyE2EHarness) waitEditingLinePainted(t *testing.T, what string) {
	t.Helper()
	deadline := time.Now().Add(keyE2EWait)
	for time.Now().Before(deadline) {
		if h.editingLinePainted() {
			return
		}
		time.Sleep(keyE2EStep) //stylegate:allow-sleep bounded poll (keyE2EWait) for the read loop's own redraw to land on the row the layout published
	}
	t.Fatalf("the editing line was never painted on row %d (the row the layout gives it) while waiting for %s\n--- grid ---\n%s",
		h.editingRow(), what, h.rows())
}

// menuActiveRow finds the menu row carrying the package's own ACTIVE marker, or
// -1 when no row does. It reads the MARKER from the package rather than
// retyping it (Rule 1b: a control that spelled ">" would keep passing after a
// re-glyph), and it is this case's proof that a selection move really moved
// something: on a one-item list Up/Down CLAMP, but here the menu has several
// candidates, so a Down that was decoded as a key must move the marker and a
// Down that was lost must not.
func (h *keyE2EHarness) menuActiveRow() int {
	l, ok := h.in.editor.Layout()
	if !ok || l.MenuRows == 0 {
		return -1
	}
	scr := h.paint.grid()
	for row := l.MenuTop; row < l.MenuTop+l.MenuRows; row++ {
		if strings.HasPrefix(scr.Line(row), lineedit.MenuMarkerActive()) {
			return row
		}
	}
	return -1
}

// editingLine is the editing row's text. The layout is read ONCE for the row
// bound and the grid is read once for its rows, so the two cannot describe
// different frames (the same tear the sibling act case records).
func (h *keyE2EHarness) editingLine() string {
	top := h.editingRow()
	if top < 0 {
		return ""
	}
	rows := h.gridRows()
	if top >= len(rows) {
		return ""
	}
	return strings.TrimRight(rows[top], " ")
}

// input is the live input layer of this run — the editor keyE2EWaitEnterGap asks
// about (see the keyE2ESession contract).
func (h *keyE2EHarness) input() *chatInput { return h.in }

// render is the grid as text, used only to make a failed wait's message show what
// was on screen.
func (h *keyE2EHarness) render() string { return h.rows() }

// TestChatSurfaceKey_DrivesTheRealREPLEditorOnARealPty is the epic's acceptance
// case: the full stack, one model round, four injected keys, and the resulting
// interaction read off the painted grid.
func TestChatSurfaceKey_DrivesTheRealREPLEditorOnARealPty(t *testing.T) {
	t.Setenv("GOPI_HOME", t.TempDir())
	t.Setenv("GOPI_MEMORY_DB", "")

	h := &keyE2EHarness{keys: keyE2EInjectedKeys(), done: make(chan int, 1), exited: make(chan struct{})}
	act := h.keys
	// ONE KEY PER MODEL ROUND, AND THE TEST HOLDS THE GATE BETWEEN THEM. This is
	// the structure that makes each act OBSERVABLE, and its absence was this case's
	// defect: when a single round carries both keys, the Down's effect may land
	// BEFORE the test has read the pre-Down frame — the menu is published from the
	// completer's WORKER goroutine, so whether the menu appears before or after the
	// Down is a scheduling question, not a property of the editor. The test then
	// baselines a frame the Down has already been applied to, and a wait for "the
	// screen CHANGED" can never be satisfied — measured as this case's intermittent
	// 8s timeout. The SSE body IS the gate: every round after the first blocks until
	// the test has asserted the previous act on the grid.
	gates := make([]chan struct{}, len(act)-1)
	for i := range gates {
		gates[i] = make(chan struct{})
	}
	// done releases a parked round when the TEST ends — including on the failure of
	// an earlier leg — so a red run reports the assertion that failed instead of
	// hanging the mock's handler and surfacing as a binary timeout.
	done := make(chan struct{})

	var bodiesMu sync.Mutex
	var bodies []string
	round := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)         //stylegate:ignore bounded read of a test request body; a short read is acceptable
		defer func() { _ = r.Body.Close() }() //stylegate:ignore the test's own request body close
		bodiesMu.Lock()
		bodies = append(bodies, string(body))
		round++
		n := round
		bodiesMu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		// Round n carries the n-th injected key; every round after the act answers
		// with text, so the transcript is deterministic and the session can end.
		if n <= len(act) {
			if n > 1 {
				select {
				case <-gates[n-2]:
				case <-h.exited:
				case <-done:
				}
			}
			keyE2EMockToolSSE(w, act[n-1:n])
			return
		}
		testutil.TextSSE(w, keyE2ETextAfter)
	}))
	t.Cleanup(srv.Close)
	// The gate release is registered BEFORE the run's teardown, so the cleanups run
	// in the order that cannot deadlock: teardown (stop the REPL, free its pty) →
	// release a parked round → close the pair → close the server. A red run reaching
	// t.Fatalf with a gated round parked would otherwise hold httptest's connection
	// open through srv.Close — a whole-binary timeout in place of the failing
	// assertion's own message.
	t.Cleanup(func() { close(done) })

	pair := chatOpenPtyPair(t, keyE2EPtyCols, keyE2EPtyRows)
	paint := &keyE2EPaint{}
	term, err := lineedit.Open(pair.Slave, paint)
	if err != nil {
		t.Fatalf("open the pty slave as a terminal: %v", err)
	}
	h.pair, h.term, h.paint = pair, term, paint

	// THE PRODUCTION INPUT LAYER — the same constructor the REPL calls, with the
	// real slash completer installed, over a real pty.
	in := newChatInputWithSource(term, paint, chatPrompt)
	in.editor.SetPalette(lineedit.BasicPalette)
	in.editor.SetCompleter(NewSlashCompleter())
	if len(NewSlashCompleter()(keyE2EMenuRune, 1, 5)) == 0 {
		t.Fatalf("the REAL slash completer offers no candidate for %q, so no menu could ever open in this case", keyE2EMenuRune)
	}
	if !in.editor.EnableRegion() {
		t.Fatalf("EnableRegion(pty %dx%d) = false: the acceptance case needs the real chat layout", keyE2EPtyCols, keyE2EPtyRows)
	}
	h.in = in
	// THE PAIR IS FREED ONLY AFTER THE RUN THAT READS IT HAS STOPPED. Cleanups run
	// LIFO and chatOpenPtyPair's own teardown was registered ABOVE, so this one
	// runs first: it joins the REPL's goroutine, and only then does the pair's
	// closeQuietly destroy the descriptors.
	//
	// Measured without it (2026-09-28, the answer-seam epic's own acceptance loop,
	// race-post-1): a run that reached t.Fatalf never executed the explicit
	// in.editor.Close() at the end of the case, so the editor's read loop was
	// still inside lineedit.Terminal.Size() when the fixture's deferred close
	// destroyed the same *os.File — one `WARNING: DATA RACE` block naming
	// closeQuietly (write) against Terminal.Size (read). That race is the
	// FIXTURE's, not the editor's: a descriptor must not be closed under a live
	// reader, and the ordering is the test's to state.
	//
	// The wait is BOUNDED and LOUD: a run that is genuinely hung is reported
	// (chatJoinReadLoop) instead of being waited out in silence — a silent
	// `select` cannot tell "the loop returned" from "the bound elapsed", which is
	// the whole confusion this order removes (Rule 15).
	//
	// THE TERMINAL IS CLOSED FIRST, and it is closed HERE rather than only at the
	// end of the case: if an assertion below fails, this cleanup is the only
	// teardown that runs, and a run whose editor is left reading a live pty holds
	// the pty and the server connection open. Stopped() asks the terminal whether
	// its stop path is already down, so the success path (which closed it by hand)
	// does not close it twice.
	t.Cleanup(func() {
		if !term.Stopped() {
			_ = term.Close() //stylegate:ignore the harness's own terminal teardown
		}
		chatJoinReadLoop(t, h.exited, func() string { return runState(h.exited) })
	})

	sl, err := agent.NewSessionLogger(keyE2EModel, "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	t.Cleanup(func() { sl.Close() }) //stylegate:ignore test teardown of a logger this test opened

	ag := &agent.Agent{
		Client:     &deepseek.Client{BaseURL: srv.URL, APIKey: "test-key", HTTPClient: srv.Client()},
		Ctx:        &agent.Context{},
		Model:      keyE2EModel,
		Timeout:    0,
		SessionLog: sl,
	}
	prevFactory := chatAgentFactory
	t.Cleanup(func() { chatAgentFactory = prevFactory })
	chatAgentFactory = func(string) (*agent.Agent, error) { return ag, nil }

	go func() {
		defer close(h.exited)
		h.done <- runChatWithInput([]string{"--tools"}, in)
	}()

	// BOOTSTRAP: one real keyed line on the pty master opens the session, which is
	// what puts a model round in flight for the injections below to ride. There is
	// no separate wait for "the first turn ran": with one key per round the first
	// round's own observable IS the first leg's (the injected rune opening the
	// menu), so waiting for it twice would be the same wait written twice.
	keyE2ETypeLine(t, h, pair.Master, keyE2EBootstrap)

	// ── LEG 1: the INJECTED rune opens the real completion menu ────────────
	// THE WAIT IS ON THE LAYOUT, AND THE LINE IS READ FROM A PAINTED FRAME. The
	// published `MenuRows > 0` says the layout has RESERVED the rows; it does not
	// say the editing line has been repainted onto the row that layout gives it,
	// and reading the grid between those two writes is what produced the empty
	// string this case used to report (editingLinePainted carries the measurement:
	// layout row 17 published, row 17 blank, `you> /` in the very next write).
	h.waitFor(t, "the completion menu to open below the editing line", func(string) bool { return h.menuRows() > 0 })
	h.waitEditingLinePainted(t, "the menu's frame to reach the editing line the layout published")
	if got := h.editingLine(); !strings.Contains(got, keyE2EMenuRune) {
		t.Fatalf("the editing row does not show the injected rune: %q\n--- grid ---\n%s", got, h.rows())
	}
	// The menu must offer MORE THAN ONE candidate, and that is a PREMISE this case
	// asserts rather than assumes. It is what makes a Down a real MOVE instead of a
	// clamp: the sibling act case drives a ONE-item menu, where Up/Down are inert by
	// design and the only observable is that nothing changed. On a one-item menu a
	// selection-move assertion would be unfalsifiable, so if the completer ever
	// stopped offering several candidates this case must say so rather than drift
	// into testing a clamp it never names.
	if got := h.menuRows(); got <= 1 {
		t.Fatalf("the menu reserved %d row(s) for candidates of %q, so the injected Down could only CLAMP and its selection move would be unobservable here — this case needs a menu with more than one candidate\n--- grid ---\n%s",
			got, keyE2EMenuRune, h.rows())
	}
	// THE BASELINE IS TAKEN FROM A SETTLED FRAME, and leg 2's claim is read from
	// the SAME accessor. Reading the interaction rows as soon as the menu's height
	// is published captures a PARTIAL menu — the editor paints it over several
	// writes — and a "the screen changed" predicate is then satisfied by the
	// remainder of that same draw, i.e. by a repaint that has nothing to do with
	// the Down. That is the intermittent failure this settle exists to close
	// (measured: with the Down removed from the injection entirely, the un-settled
	// case still passed).
	h.settle(t, "the menu's own paint to finish before it is used as the baseline")
	beforeMenuRow := h.menuActiveRow()
	if beforeMenuRow < 0 {
		t.Fatalf("the menu reserved %d row(s) but no row carries the selection marker %q\n--- grid ---\n%s",
			h.menuRows(), lineedit.MenuMarkerActive(), h.rows())
	}
	editingBefore := h.editingLine()

	// ── LEG 2: the INJECTED Down moves the SELECTION ───────────────────────
	// The baseline above is SAFE only because the Down's round is still parked on
	// the gate: nothing has injected the arrow yet, so the frame it describes is the
	// frame the Down will act on. Releasing the gate is what lets that round run.
	//
	// THE WAIT IS ON THE SUBJECT, and that is the whole difference from the version
	// this replaces. It waits for the ACTIVE marker to change ROW — the subject of
	// the claim — rather than for "the screen differs from a snapshot", which is a
	// PROXY the answer band's own streaming satisfies: measured, the old predicate
	// returned within milliseconds while the marker had not moved at all, because
	// the turn's text was still being painted.
	close(gates[0])
	menuRowsObserved := h.menuRows()
	h.waitFor(t, fmt.Sprintf("the injected Down to move the selection marker %q off row %d of the %d-candidate menu",
		lineedit.MenuMarkerActive(), beforeMenuRow, menuRowsObserved), func(string) bool {
		row := h.menuActiveRow()
		return row >= 0 && row != beforeMenuRow
	})
	if got := h.menuRows(); got == 0 {
		t.Fatalf("the menu vanished when the Down arrived — the arrow was not read as a motion\n--- grid ---\n%s", h.rows())
	}
	if got := h.editingLine(); strings.Contains(got, lineedit.KeyNameDown) {
		t.Fatalf("the Down arrived as TEXT: the editing row carries %q\n--- grid ---\n%s", got, h.rows())
	}
	// The move must be a MOVE and nothing else: an arrow that reached the decoder
	// leaves the editing line exactly as it was (it is a motion, not input). A row
	// that changed here would mean the bytes landed in the buffer.
	if got := h.editingLine(); got != editingBefore {
		t.Fatalf("the Down disturbed the editing line: was %q, now %q\n--- grid ---\n%s", editingBefore, got, h.rows())
	}
	// ── LEG 3: the editor still holds a LINE the injections produced ───────
	// The editing row carries the injected rune and is NOT the bootstrap text and
	// NOT empty: the bytes were read as an editing gesture, not pasted as content.
	if got := strings.TrimSpace(h.editingLine()); !strings.Contains(got, keyE2EMenuRune) {
		t.Fatalf("the editing row does not carry the injected rune after the interaction: %q\n--- grid ---\n%s", got, h.rows())
	}

	// END THE SESSION through the SAME seam, with the one key whose effect is to
	// close the editor (the editor reports Interrupted and the REPL leaves
	// cleanly). It is injected rather than typed on the master because it also
	// exercises the interrupt key end to end — and because the line editor's own
	// Ctrl-C is the supported way out of an editor holding a half-typed line.
	if res := h.in.injector.InjectKey(lineedit.KeyNameCtrlC); !res.Accepted {
		t.Fatalf("injecting ctrl-c refused: %s", res.Why)
	}
	select {
	case <-h.exited:
	case <-time.After(keyE2EWait):
		t.Fatalf("the REPL did not finish after /quit\n--- grid ---\n%s", h.rows())
	}
	// THE TEARDOWN ORDER (chat_pty_fixture_test.go states it once): editor, then
	// THE STOP, then the join, then the descriptors. The join below is what the
	// old order had first and silent — it looked like a join, but the loop had
	// already been ended by the pair's own close one line later, so the case was
	// measuring the descriptor's close on scheduling luck. Closing the TERMINAL
	// first is what makes the join a join.
	in.editor.Close()
	_ = term.Close() //stylegate:ignore the test's own terminal teardown
	chatJoinReadLoop(t, h.exited, func() string { return runState(h.exited) })
	_ = pair.Master.Close() //stylegate:ignore pty master teardown of the key e2e fixture, after the screen dump was captured

	// ── THE INJECTION REALLY HAPPENED (the positive half) ──────────────────
	// A run whose action=key calls were REFUSED would still reach this point with
	// a menu-less grid, so the TOOL RESULTS the model received are read back from
	// the mock's own record of the requests: the round after the injections carries
	// each result, and every one must report an injection rather than a refusal.
	bodiesMu.Lock()
	recorded := append([]string(nil), bodies...)
	bodiesMu.Unlock()
	if len(recorded) < 2 {
		t.Fatalf("the model saw %d round(s); the tool results never came back, so nothing confirms the injections", len(recorded))
	}
	results := recorded[len(recorded)-1]
	if n := strings.Count(results, "injected="); n < len(h.keys) {
		t.Fatalf("the tool results carry %d injection report(s), want at least %d (one per injected key) — a refusal would leave the count short:\n%s",
			n, len(h.keys), results)
	}
	// menuRowsObserved is read WHILE the menu was up: reading the layout again here
	// would report 0, because the editor has been closed by now (measured — the
	// first version of this line logged 0 row(s) and read as if the menu had never
	// opened).
	t.Logf("the model injected %v through chat_surface action=%s; the results came back as injections and the run's own editor had the menu reserved (%d row(s), %d row(s) of it painted)",
		h.keys, agent.ChatSurfaceActionKey, menuRowsObserved, h.menuRows())
}
