package uiclient

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/sessionevent"
)

// Layout constants. Every one of them is a named constant (Rule 1a): the
// three-row chrome (header, input, status), the scroll step, and the default
// viewport height used before the terminal reports its size.
const (
	// viewportDefaultHeight is the transcript's line budget before a
	// tea.WindowSizeMsg arrives (a terminal that never reports its size is
	// still usable).
	viewportDefaultHeight = 20
	// scrollStepLines is how many transcript lines one page-key moves.
	scrollStepLines = 5
	// headerRows / inputRows / statusRows are the chrome's line counts; the
	// transcript gets whatever is left of the terminal.
	headerRows = 1
	inputRows  = 1
	statusRows = 1
	// minTranscriptRows keeps the transcript visible on a tiny terminal.
	minTranscriptRows = 1
	// scrollTail is the scroll offset meaning "follow the newest line".
	scrollTail = 0
	// queuedFallbackNotice is shown when a `queued` frame arrives without the
	// server's notice text.
	queuedFallbackNotice = "queued behind the in-flight turn"
	// queuedState is the status line's word while this turn waits on the
	// server's single-turn lock. Naming the wait is the whole point: an
	// operator staring at an unchanged screen reads a silent wait as a dead
	// surface (the reported `gopi ui` freeze), and the wait can be long —
	// the engine holds that lock for the account of the turn in front.
	queuedState = "queued…"
	// queuedWaitTick is how often the status line is repainted with the
	// elapsed wait, so a long queue is visibly progressing rather than frozen.
	// It is the surface's own refresh; the wait itself is the engine's.
	queuedWaitTick = time.Second
	// busyTurnNotice is shown when Enter arrives while a turn is already
	// running: the server serializes turns (s.mu), so a second one would only
	// queue invisibly.
	busyTurnNotice = "a turn is already running — Ctrl+X stops it"
	// interruptedNotice labels a turn the operator stopped with Ctrl+X. It is
	// deliberately NOT an error row: a cancel is a normal end.
	interruptedNotice = "interrupted (Ctrl+X)"
	// lineBreak is the newline this package's string work splits on. It is a
	// named constant rather than a bare "\n" at the several sites that compare
	// against it (the span cursor's break runs, wrapText's paragraph split).
	lineBreak = "\n"

	promptCursor = "▏"
	// inputPrompt is the persistent input box's prompt.
	inputPrompt = "you> "
	// userPrefix / assistantPrefix / toolPrefix / errorPrefix label the
	// transcript's entries. `gopi web`'s page uses bubbles; a terminal gets
	// the same distinction from a prefix, which survives pipe/no-color
	// terminals.
	userPrefix      = "you> "
	assistantPrefix = "gopi> "
	toolPrefix      = "  ⚙ "
	errorPrefix     = "  ⚠ "
	noticePrefix    = "  · "
	// reasoningPrefix labels a live reasoning row. It is lineedit's OWN
	// reasoning gutter rather than a second spelling of the same idea: the chat
	// band and this surface mark "the model is thinking" with one glyph, and a
	// reader who has learned it on one carrier has learned it on both
	// (Rule 14 — one definition, referenced).
	reasoningPrefix = lineedit.ThinkingGutter
	// reasoningWindowLines is how many lines of live reasoning the transcript
	// KEEPS: the reasoning is an aside the reader watches, not a record they
	// scroll back through, so the row is a rolling window over the newest lines
	// and everything older is dropped as it arrives.
	//
	// WHY A WINDOW AT ALL, and not an ever-growing row: the reasoning stream is
	// the LONGEST stream a model produces (an order of magnitude more characters
	// than the answer), and a transcript that kept all of it would push the
	// ANSWER off the screen — the reasoning would bury the thing the reader
	// actually asked for.
	reasoningWindowLines = 6
	// reasoningRetainedBytes bounds what the ROW holds, in bytes: the display
	// window above is a handful of lines, and the raw text only has to be long
	// enough to fill it, so the tail is kept and the rest dropped as it arrives.
	//
	// WHY A BYTE BOUND AND NOT JUST THE LINE WINDOW: the window is applied to
	// DRAWN lines, which depend on the terminal width — a stream of one enormous
	// line (reasoning arrives in fragments with no newline at all) has no lines
	// to window until it is wrapped, so a row that stored the raw text whole
	// would grow without bound for the whole turn. This is the memory bound; the
	// line window is what the reader sees. It is set far above the window so the
	// two can never disagree about what is visible — trimming here is invisible.
	reasoningRetainedBytes = 4096
)

// ScreenWriter wraps an io.Writer and counts what the TUI actually put on it:
// the number of Write calls (frames the renderer flushed) and the bytes. It is
// the measurement behind the report's per-turn screen-write numbers — a
// MEASURED value, not a claimed property, which is why it is exported rather
// than a test-only helper.
type ScreenWriter struct {
	w     io.Writer
	calls atomic.Int64
	bytes atomic.Int64
}

// NewScreenWriter wraps w.
func NewScreenWriter(w io.Writer) *ScreenWriter { return &ScreenWriter{w: w} }

// Write implements io.Writer, counting the call.
func (s *ScreenWriter) Write(p []byte) (int, error) {
	n, err := s.w.Write(p)
	s.calls.Add(1)
	s.bytes.Add(int64(n))
	return n, err
}

// Calls reports how many Write calls the renderer made (one per flushed frame).
func (s *ScreenWriter) Calls() int64 { return s.calls.Load() }

// Bytes reports how many bytes the renderer wrote.
func (s *ScreenWriter) Bytes() int64 { return s.bytes.Load() }

// entryKind labels one transcript row's origin.
type entryKind int

const (
	// entryUser is one of the operator's own messages.
	entryUser entryKind = iota
	// entryAssistant is model text (streamed, then revised in place).
	entryAssistant
	// entryTool is one live tool activity (call or result).
	entryTool
	// entryError is a fault the UI must show as a fault.
	entryError
	// entryNotice is UI-authored information (queued, interrupted, reset).
	entryNotice
	// entryReasoning is one live REASONING line — the model's own aside, drawn
	// with its own gutter and its own (quieter) emphasis. It is not
	// entryAssistant and must never become it: an assistant row is the ANSWER
	// (it carries the answer's own prefix and emphasis, and it is what a reader
	// quotes back), while this row is display-only content that no
	// conversation ever carries.
	entryReasoning
)

// answerSpan is one emphasis run of a transcript row's text: the text as the
// terminal must draw it (the format's markers already removed by the ONE
// tokenizer, internal/lineedit) and how strongly it is emphasised.
//
// The KIND is the tokenizer's own vocabulary — lineedit.EmphasisNone /
// lineedit.EmphasisBold — rather than a second enumeration here: `gopi ui` and
// the banded editor must agree on what a marker MEANS, and two spellings of
// "bold" is exactly the drift Rule 14 forbids. The wording ("span", "emphasis",
// "bold") is shared; the translation into terminal bytes is this surface's own
// (see lineedit.SpanStyle — the ONE mapping from an emphasis to a style).
type answerSpan struct {
	text string
	em   lineedit.Emphasis
}

// entry is one transcript row.
type entry struct {
	kind entryKind
	text string
	// spans, when non-empty, is the row's text as emphasised runs: the text the
	// row DRAWS, with the intra-line markers removed (the format's decision,
	// taken by lineedit's tokenizer). It is empty for every row that carries no
	// emphasis — a tool row, a notice, an answer with no marker — and the row is
	// then written exactly as its plain text.
	//
	// It is deliberately NOT built for every row: the marker's spelling lives in
	// the tokenizer, so a caller asks it (lineedit.DisplaySpans) and a row with
	// no marker pays nothing.
	spans []answerSpan
}

// model is the Bubble Tea model. It is returned BY VALUE from newModel and
// then held by the Program, so every mutation happens inside Update (which
// receives *model) — no field is ever assigned through a copy. The `send`
// injection the sketch relied on is gone entirely: frames travel on a channel
// that a tea.Cmd reads, so no closure needs a *Program.
type model struct {
	// client is the SSE consumer (nil would be a programming error; the
	// constructor refuses it).
	client *Client
	// target is the server origin shown in the header.
	target string
	// tools mirrors the server's --tools switch, for the header only.
	tools bool

	entries   []entry
	input     []rune
	cursor    int
	width     int
	height    int
	scroll    int  // lines scrolled up from the tail (0 = follow)
	streaming bool // a turn is in flight
	// assistantIdx is the transcript index of the streaming assistant row
	// (-1 when no turn is running); deltas append to it in place.
	assistantIdx int
	// reasoningIdx is the transcript index of the CURRENT turn's rolling
	// reasoning row, or -1 when this turn has none. One row per turn (never one
	// per fragment): the stream arrives in hundreds of tiny pieces and a row per
	// piece is a transcript that scrolls itself off the screen.
	reasoningIdx int
	// cancelling records that the operator asked to stop, so the turn's
	// normal end is labelled "interrupted" instead of "error".
	cancelling bool
	// onInterrupt asks the ENGINE to abandon the turn this surface is showing.
	// A local cancel only ends this side of the stream: the engine runs the
	// turn to completion and holds its single-turn lock, which is what leaves
	// an operator waiting with nothing on screen (see Config.OnInterrupt).
	// nil = no engine hook (a test or a client with no interrupt route).
	onInterrupt func()
	// onFrameGeometry, when set, is told the geometry each rendered frame was
	// laid out for (see Config.OnFrameGeometry). It is called from View — the
	// ONE place the model composes a frame — so the number reported is the one
	// THIS frame used, never a hint from startup.
	onFrameGeometry func(cols, rows int)
	// queuedAt is when the current turn was told it is waiting on the engine's
	// single-turn lock (zero = not queued). It is what turns a silent wait into
	// a named one: the status line reports queued… plus how long.
	queuedAt time.Time
	// queuedTicking records that the wait's repaint chain is already running,
	// so a stream of frames cannot start one chain per frame.
	queuedTicking bool
	// ended records that this turn has ALREADY concluded — the server's
	// terminal frame ends the turn, and the transport's return (one message
	// later) is then only cleanup. Without it a turn would be counted twice
	// and its outcome reported twice.
	ended bool
	// cancel aborts the in-flight turn (nil when idle).
	cancel context.CancelFunc
	// frames is the live channel the current turn's frames arrive on; nil
	// when idle. Closing it ends the turn's message pump.
	frames chan tea.Msg
	// turns counts completed turns (status line).
	turns int
	// screen is the optional output counter (nil when the caller did not
	// wrap its writer); turnMark is the counter's value at the turn's start,
	// turnFrames the per-turn deltas.
	screen     *ScreenWriter
	turnMark   int64
	turnFrames []int64
	// toolEvents counts tool frames seen in the current run (status line).
	toolEvents int
	// errText is the last fault, shown in the status line; empty = none.
	errText string
	// pal is the emphasis table the transcript draws with. It is supplied by the
	// caller (run.go, from the surface's ONE capability decision) and defaults to
	// the monochrome table, so a model nobody configured writes plain text.
	pal lineedit.Palette
	// quitting ends the program.
	quitting bool
}

// newModel builds the initial model. It refuses a nil client (a zero
// dependency would make the first turn panic — the sketch's defect-1 class of
// failure is designed out by construction, not by discipline).
func newModel(c *Client, target string, tools bool) (model, error) {
	if c == nil {
		return model{}, fmt.Errorf("uiclient: nil client")
	}
	m := model{
		client:       c,
		target:       target,
		tools:        tools,
		assistantIdx: -1,
		reasoningIdx: -1,
		width:        80,
		height:       viewportDefaultHeight + headerRows + inputRows + statusRows,
	}
	m.entries = append(m.entries, entry{kind: entryNotice, text: "gopi ui — type a message and press Enter. Ctrl+X interrupts a turn, Ctrl+C quits."})
	return m, nil
}

// Init implements tea.Model.
func (m model) Init() tea.Cmd { return nil }

// queuedTickMsg asks the status line to repaint while this turn waits on the
// engine's single-turn lock (see queuedWaitTick).
type queuedTickMsg struct{}

// queuedTick schedules the next wait repaint. It carries the model's OWN wait
// state, so the tick chain dies with the wait instead of outliving the turn.
func queuedTick() tea.Cmd {
	return tea.Tick(queuedWaitTick, func(time.Time) tea.Msg { return queuedTickMsg{} })
}

// frameMsg is one decoded frame pushed by the turn's goroutine.
type frameMsg struct{ frame Frame }

// turnDoneMsg ends the message pump: the turn finished (err == nil) or failed.
type turnDoneMsg struct{ err error }

// pump reads the NEXT message from the turn's channel as a tea.Cmd. This is
// the whole concurrency design: the streaming goroutine owns the HTTP body and
// only pushes messages; every model mutation happens in Update on the Program's
// own goroutine. No shared mutable state, no p.Send, no value-copy hazard.
func pump(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

// startTurn begins one turn: it clears the input, appends the operator's row,
// opens an assistant row and launches the streaming goroutine.
func (m *model) startTurn() tea.Cmd {
	msg := strings.TrimSpace(string(m.input))
	if msg == "" {
		return nil
	}
	m.input = m.input[:0]
	m.cursor = 0
	m.errText = ""
	m.scroll = scrollTail
	m.entries = append(m.entries, entry{kind: entryUser, text: msg})
	m.entries = append(m.entries, entry{kind: entryAssistant, text: ""})
	m.assistantIdx = len(m.entries) - 1
	m.reasoningIdx = -1
	m.streaming = true
	m.cancelling = false
	m.ended = false
	if m.screen != nil {
		m.turnMark = m.screen.Calls()
	}

	ctx, cancel := context.WithCancel(context.Background()) //nolint:govet // cancel is stored on the model and called by Ctrl+X / quit
	m.cancel = cancel

	ch := make(chan tea.Msg, frameChanBuffer)
	m.frames = ch
	client := m.client
	go func() {
		err := client.Turn(ctx, msg, func(f Frame) error {
			ch <- frameMsg{frame: f}
			return nil
		})
		ch <- turnDoneMsg{err: err}
		close(ch)
	}()
	return pump(ch)
}

// frameChanBuffer is the streaming channel's capacity: enough for a burst of
// deltas so the HTTP reader never blocks on a slow render, small enough that a
// huge answer applies back-pressure instead of buffering in memory.
const frameChanBuffer = 64

// Update implements tea.Model. It is the ONLY place the model mutates.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case frameMsg:
		m.applyFrame(msg.frame)
		cmds := []tea.Cmd{}
		if m.frames != nil {
			cmds = append(cmds, pump(m.frames))
		}
		// A queued frame started a wait: schedule its repaint ONCE (a tick
		// chain per wait, not one per frame).
		if !m.queuedAt.IsZero() && !m.queuedTicking {
			m.queuedTicking = true
			cmds = append(cmds, queuedTick())
		}
		return m, tea.Batch(cmds...)

	case turnDoneMsg:
		m.endTurn(msg.err)
		return m, nil

	case queuedTickMsg:
		// Repaint only while the wait is still real: the chain ends when the
		// turn concludes (endTurn clears the state) or when the frames start
		// arriving, so a finished turn cannot leave a tick running.
		if m.streaming && !m.queuedAt.IsZero() {
			return m, queuedTick()
		}
		return m, nil
	}
	return m, nil
}

// handleKey is the input surface: a line editor for the persistent input box,
// page keys for the transcript, Enter to run a turn, Ctrl+X to interrupt,
// Ctrl+C to quit.
func (m model) handleKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.Type {
	case tea.KeyCtrlC:
		m.quitting = true
		if m.cancel != nil {
			m.cancel()
		}
		return m, tea.Quit

	case tea.KeyCtrlX:
		return m.interrupt()

	case tea.KeyEnter:
		// Enter submits; a newline inside the message is not a thing a
		// single-line input can carry, so the wire never sees one. While a
		// turn runs the draft is KEPT (never eaten) and the operator is told
		// why nothing happened.
		if m.streaming {
			if strings.TrimSpace(string(m.input)) != "" {
				m.entries = append(m.entries, entry{kind: entryNotice, text: busyTurnNotice})
			}
			return m, nil
		}
		return m, m.startTurn()

	case tea.KeyBackspace:
		if m.cursor > 0 {
			m.input = append(m.input[:m.cursor-1], m.input[m.cursor:]...)
			m.cursor--
		}
		return m, nil

	case tea.KeyDelete:
		if m.cursor < len(m.input) {
			m.input = append(m.input[:m.cursor], m.input[m.cursor+1:]...)
		}
		return m, nil

	case tea.KeyLeft:
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil

	case tea.KeyRight:
		if m.cursor < len(m.input) {
			m.cursor++
		}
		return m, nil

	case tea.KeyHome, tea.KeyCtrlA:
		m.cursor = 0
		return m, nil

	case tea.KeyEnd, tea.KeyCtrlE:
		m.cursor = len(m.input)
		return m, nil

	case tea.KeyCtrlU:
		m.input = m.input[:0]
		m.cursor = 0
		return m, nil

	case tea.KeyPgUp:
		m.scrollBy(scrollStepLines)
		return m, nil

	case tea.KeyPgDown:
		m.scrollBy(-scrollStepLines)
		return m, nil

	case tea.KeySpace:
		m.insertRunes([]rune(" "))
		return m, nil

	case tea.KeyRunes:
		m.insertRunes(k.Runes)
		return m, nil
	}
	return m, nil
}

// insertRunes inserts a rune run at the caret.
func (m *model) insertRunes(r []rune) {
	if len(r) == 0 {
		return
	}
	out := make([]rune, 0, len(m.input)+len(r))
	out = append(out, m.input[:m.cursor]...)
	out = append(out, r...)
	out = append(out, m.input[m.cursor:]...)
	m.input = out
	m.cursor += len(r)
}

// interrupt aborts the in-flight turn (Ctrl+X). A cancel is a NORMAL end: the
// UI records that the operator asked, marks the assistant row, and the turn's
// own completion path writes no error (acceptance 5).
//
// The ENGINE is told too (onInterrupt), and that is what makes the key honest:
// cancelling this side of the stream cannot stop the turn behind it — the engine
// runs it to completion and holds its single-turn lock — so a surface that only
// cancelled locally would report "interrupted" while the abandoned turn kept
// every later message waiting.
func (m model) interrupt() (tea.Model, tea.Cmd) {
	if !m.streaming || m.cancel == nil {
		m.entries = append(m.entries, entry{kind: entryNotice, text: "nothing to interrupt"})
		return m, nil
	}
	m.cancelling = true
	m.cancel()
	if m.onInterrupt != nil {
		m.onInterrupt()
	}
	return m, nil
}

// scrollBy moves the transcript view; 0 is the tail (follow) and the value is
// clamped to the renderable range.
func (m *model) scrollBy(delta int) {
	max := len(m.allLines()) - m.transcriptRows()
	if max < 0 {
		max = 0
	}
	m.scroll += delta
	if m.scroll < 0 {
		m.scroll = 0
	}
	if m.scroll > max {
		m.scroll = max
	}
}

// applyFrame renders one decoded frame. Decoding is strict: a frame whose
// payload does not decode, or whose event this client does not know, becomes a
// visible error row — NEVER assistant prose (sketch defect 3, closed here).
func (m *model) applyFrame(f Frame) {
	v, err := decodeFrame(f)
	if err != nil {
		m.fail(err)
		return
	}
	switch t := v.(type) {
	case DeltaText:
		m.appendAssistant(t.Text)
	case ThinkingText:
		m.appendReasoning(t.Text)
	case ToolActivity:
		m.toolEvents++
		m.entries = append(m.entries, entry{kind: entryTool, text: formatToolActivity(t)})
	case CommandResult:
		m.entries = append(m.entries, entry{kind: entryTool, text: formatCommandResult(t)})
	case QueuedNotice:
		text := t.Notice
		if text == "" {
			text = queuedFallbackNotice
		}
		m.entries = append(m.entries, entry{kind: entryNotice, text: text})
		// Record the wait so the status line NAMES it (queued… + elapsed)
		// instead of showing an idle-looking line while this turn sits on the
		// engine's single-turn lock, and start the repaint that keeps the
		// elapsed time moving.
		m.queuedAt = time.Now()
	case TurnEnd:
		if m.assistantIdx >= 0 && m.entries[m.assistantIdx].text == "" && t.Result != "" {
			// No deltas arrived (a non-streaming upstream): show the final
			// text rather than an empty answer.
			m.entries[m.assistantIdx].text = t.Result
		}
		if t.Error != "" {
			m.endTurn(fmt.Errorf("%s", t.Error))
			return
		}
		m.endTurn(nil)
	case error:
		// `event: error` decodes to its carrier-level cause.
		m.fail(t)
	default:
		m.fail(&UnknownFrameEventError{Event: f.Event, Data: f.Data})
	}
}

// appendAssistant appends streamed text to the turn's assistant row.
//
// The row's SPANS are rebuilt from the row's whole text on every delta, because
// a marker can arrive in halves: the text so far may say `**bo` and the next
// delta `ld**`, which is bold — and a row whose spans were frozen at the first
// delta would keep painting `**bo` for ever. Rebuilding is what the banded
// editor's renderer does at the line's break (see AnswerRenderer.computeRows),
// applied per frame: the tokenizer is pure, so asking again is cheap and the
// answer is stable for the text it is asked about.
//
// Deleting the markers cannot lose the reader's content: the SPANS carry the text
// without them, and renderSpans draws that text through the same wrap the plain
// path uses.
func (m *model) appendAssistant(text string) {
	if m.assistantIdx < 0 {
		// A delta outside a turn (a replayed stream): keep it visibly apart
		// from an answer rather than merging it into one.
		m.entries = append(m.entries, assistantEntry(text))
		return
	}
	e := &m.entries[m.assistantIdx]
	e.text += text
	e.spans = spansFor(e.text)
}

// appendReasoning appends one RAW reasoning fragment to the turn's rolling
// reasoning row, opening that row at the turn's first fragment.
//
// WHERE THE ROW SITS — ABOVE the assistant row, not below it. The reasoning of a
// turn belongs to the turn that is being produced, and the answer is that turn's
// conclusion: with the reasoning row inserted after the (still empty) assistant
// row, a reader watching the display would see the aside printed under the answer
// it precedes, which reads as the answer having been said first.
//
// The ROW is trimmed rather than the display alone (see
// reasoningRetainedBytes): a fragment stream is unbounded and the row's tail is
// all the window can ever show.
func (m *model) appendReasoning(text string) {
	// A fragment can arrive with the turn's own end (the engine flushes the
	// block boundary through the same path): an empty one must not OPEN a row,
	// or a turn whose reasoning was empty would leave a bare gutter behind.
	if text == "" {
		return
	}
	if m.reasoningIdx < 0 {
		m.entries = append(m.entries, entry{kind: entryReasoning})
		m.reasoningIdx = len(m.entries) - 1
		// The row was appended AFTER the streaming answer row, so the two are
		// swapped to put the aside above the answer it precedes. Keeping the
		// indices straight here — at the ONE place a row is inserted — is what
		// keeps appendAssistant from writing the answer into the reasoning row.
		if m.assistantIdx >= 0 {
			m.entries[m.assistantIdx], m.entries[m.reasoningIdx] = m.entries[m.reasoningIdx], m.entries[m.assistantIdx]
			m.reasoningIdx, m.assistantIdx = m.assistantIdx, m.reasoningIdx
		}
	}
	e := &m.entries[m.reasoningIdx]
	e.text += text
	if len(e.text) > reasoningRetainedBytes {
		// Cut on a RUNE boundary: the retained tail is displayed, and half a
		// multi-byte character at its head would render as a replacement glyph.
		e.text = tailRunes(e.text, reasoningRetainedBytes)
	}
}

// tailRunes keeps the last max bytes of s, moved forward to the next rune
// boundary so the result is valid UTF-8.
func tailRunes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := len(s) - max
	for cut < len(s) && !utf8.RuneStart(s[cut]) {
		cut++
	}
	return s[cut:]
}

// assistantEntry builds a finished assistant row from text.
func assistantEntry(text string) entry {
	return entry{kind: entryAssistant, text: text, spans: spansFor(text)}
}

// spansFor tokenizes an assistant row's text into emphasis runs, or returns nil
// when the row carries no emphasis. nil (rather than a one-span list) is the
// signal that the row is plain text: the renderer then writes the raw text and
// the row is byte-identical to what it was before this layer existed.
func spansFor(text string) []answerSpan {
	if !lineedit.HasBoldMarker(text) {
		return nil
	}
	spans := lineedit.DisplaySpans(text)
	out := make([]answerSpan, 0, len(spans))
	for _, sp := range spans {
		out = append(out, answerSpan{text: sp.Text, em: sp.Emphasis})
	}
	return out
}

// fail records a fault: the status line carries it, and the transcript keeps a
// row where the operator will see it.
func (m *model) fail(err error) {
	if err == nil {
		return
	}
	m.errText = err.Error()
	m.entries = append(m.entries, entry{kind: entryError, text: err.Error()})
}

// endTurn is the ONE place a turn concludes, whichever end arrived first: the
// server's terminal `done` frame (the ordinary path) or the transport returning
// (a cancel, a dropped connection). A user-initiated cancel is reported as an
// interruption, never as an error; a turn is counted exactly once.
func (m *model) endTurn(err error) {
	if m.ended {
		if err != nil && m.errText == "" {
			// A transport fault that surfaced after the turn's own end is
			// still reported, but it does not re-count the turn.
			m.fail(err)
		}
		return
	}
	m.ended = true
	m.turns++
	if m.screen != nil {
		m.turnFrames = append(m.turnFrames, m.screen.Calls()-m.turnMark)
		m.turnMark = m.screen.Calls()
	}
	switch {
	case m.cancelling:
		m.entries = append(m.entries, entry{kind: entryNotice, text: interruptedNotice})
	case err != nil:
		m.fail(err)
	}
	m.cancelling = false
	m.assistantIdx = -1
	m.reasoningIdx = -1
	m.streaming = false
	m.cancel = nil
	m.frames = nil
	// The turn is over: a wait that was still being reported goes with it, and
	// its repaint chain ends at the next tick instead of outliving the turn.
	m.queuedAt = time.Time{}
	m.queuedTicking = false
	m.cursor = len(m.input)
}

// stats reports what this session measured (the per-turn screen-write deltas).
func (m model) stats() Stats {
	return Stats{TurnFrames: append([]int64(nil), m.turnFrames...), Turns: m.turns}
}

// View implements tea.Model: a header, the transcript tail, the input box and
// the status line.
func (m model) View() string {
	// The frame's OWN geometry is reported here — the one place a frame is
	// composed — so an observer of this paint stream (the screen dump) sizes its
	// grid to what the model laid out for, including a WindowSizeMsg that
	// arrived after startup. The hook is nil in every caller that does not dump.
	if m.onFrameGeometry != nil {
		m.onFrameGeometry(m.width, m.height)
	}
	if m.quitting {
		return ""
	}
	rows := m.transcriptRows()
	lines := m.allLines()
	end := len(lines) - m.scroll
	if end > len(lines) {
		end = len(lines)
	}
	start := end - rows
	if start < 0 {
		start = 0
	}
	visible := lines[start:end]

	out := make([]string, 0, rows+headerRows+inputRows+statusRows)
	out = append(out, m.headerLine())
	out = append(out, visible...)
	for len(out) < headerRows+rows {
		// Pad so the input box and status line keep their place while the
		// transcript is short (a stable layout, not a jumping one).
		out = append(out, "")
	}
	out = append(out, m.inputLine())
	out = append(out, m.statusLine())
	return strings.Join(out, "\n")
}

// transcriptRows is the transcript's line budget for the current terminal.
func (m model) transcriptRows() int {
	rows := m.height - headerRows - inputRows - statusRows
	if rows < minTranscriptRows {
		rows = minTranscriptRows
	}
	return rows
}

// headerLine is the fixed first row.
func (m model) headerLine() string {
	tools := "tools off"
	if m.tools {
		tools = "tools on"
	}
	return truncateToWidth(fmt.Sprintf("gopi ui → %s  (%s)", m.target, tools), m.width)
}

// inputLine renders the persistent input box with the caret, in the palette's
// two halves: the prompt under Palette.Prompt, and everything after it — the
// typed buffer with its inline caret cell — under Palette.Buffer.
//
// THE LAYOUT IS DECIDED ON THE PLAIN TEXT, AND THE SGR IS APPLIED AFTERWARDS.
// truncateToWidth measures display cells, so it must run on the unstyled line
// (an escape byte is not a cell, and letting one into the measurement is the
// row-shear this repo has already paid for). The cut is therefore taken first
// and the surviving halves are painted second.
//
// The split is the prompt's own width in cells, clamped to what survived the
// cut: a narrow window that cuts INSIDE the prompt leaves the whole row as
// prompt, and a cut inside the buffer leaves the prompt intact and the buffer
// short. Nothing is measured twice — inputPromptCells is derived from the
// prompt itself, so the boundary cannot drift from the text it splits.
func (m model) inputLine() string {
	before := string(m.input[:m.cursor])
	at := promptCursor
	after := string(m.input[m.cursor:])
	line := truncateToWidth(inputPrompt+before+at+after, m.width)
	n := inputPromptCells
	if n > lineedit.DisplayWidth(line) {
		n = lineedit.DisplayWidth(line)
	}
	prompt, buffer := splitCells(line, n)
	return m.pal.Prompt.Wrap(prompt) + m.pal.Buffer.Wrap(buffer)
}

// inputPromptCells is the prompt's width in display CELLS — the boundary the
// input row's two emphasis halves are cut on. Derived from the prompt string
// rather than spelled, so the two cannot disagree if the prompt changes.
var inputPromptCells = lineedit.DisplayWidth(inputPrompt)

// splitCells cuts s before the rune whose cells would cross n, returning the
// prefix that fits in n cells and the rest. The rule is truncateToWidth's own
// (stop BEFORE a rune that would exceed the budget), so the prefix is exactly
// the cells a window of n would have kept — and a wide glyph that would
// straddle the boundary stays whole with the second half, because half of a
// glyph is not a cell this row can draw.
func splitCells(s string, n int) (head, tail string) {
	w := 0
	for i, r := range s {
		if w+runewidth.RuneWidth(r) > n {
			return s[:i], s[i:]
		}
		w += runewidth.RuneWidth(r)
	}
	return s, ""
}

// statusLine is the one-line state: streaming/queued/idle, turn and tool
// counts, and the last fault.
func (m model) statusLine() string {
	state := "idle"
	switch {
	case m.streaming && m.cancelling:
		state = "stopping…"
	case m.streaming && !m.queuedAt.IsZero():
		// The engine has this turn waiting on its single-turn lock. Say so, and
		// say for how long: an unnamed wait is what an operator reads as a
		// frozen surface.
		state = fmt.Sprintf("%s %ds", queuedState, int(time.Since(m.queuedAt).Seconds()))
	case m.streaming:
		state = "streaming…"
	}
	// m.turns counts the turns this live session has RUN so far (one per turn the
	// engine finished; the fail path does not re-count a turn that errored). That
	// is THE turn count — the same quantity `chats.turn_count` records and the
	// same word every session surface prints — because an input is an input
	// whatever face it arrived on (the operator's ruling, 2026-10-27). The row
	// therefore prints it under the bare word (sessionevent.CounterUnitTurn) and
	// no qualifier is invented for it (Rule 14).
	parts := []string{state, fmt.Sprintf("%s %d", sessionevent.CounterUnitTurn, m.turns), fmt.Sprintf("tool events %d", m.toolEvents)}
	line := strings.Join(parts, " · ")
	if m.errText != "" {
		line += " · " + errorPrefix + m.errText
	}
	return truncateToWidth(line, m.width)
}

// allLines wraps every transcript entry to the current width.
func (m model) allLines() []string {
	width := m.width
	if width < 1 {
		width = 1
	}
	var out []string
	for _, e := range m.entries {
		out = append(out, m.wrapEntry(e, width)...)
	}
	return out
}

// wrapEntry renders one entry as indented, wrapped lines, applying the
// capability's emphasis to the row's runs. It is a MODEL method because the
// emphasis table is the model's — the capability decision itself is the caller's
// (run.go, Config.Palette).
func (m model) wrapEntry(e entry, width int) []string {
	prefix := ""
	switch e.kind {
	case entryUser:
		prefix = userPrefix
	case entryAssistant:
		prefix = assistantPrefix
	case entryTool:
		prefix = toolPrefix
	case entryError:
		prefix = errorPrefix
	case entryNotice:
		prefix = noticePrefix
	case entryReasoning:
		prefix = reasoningPrefix
	}
	body := e.plainText()
	if body == "" {
		return []string{strings.TrimRight(prefix, " ")}
	}
	lines := wrapText(body, width, len([]rune(prefix)))
	if e.kind == entryReasoning && len(lines) > reasoningWindowLines {
		// The rolling window: the NEWEST lines are the ones a reader watching
		// "the model is thinking" is watching. It is applied to the WRAPPED lines
		// rather than to the raw text because width decides what a line is, and a
		// window taken before the wrap would show a different amount of the
		// stream on a narrow terminal than on a wide one for the same input.
		lines = lines[len(lines)-reasoningWindowLines:]
	}
	indent := strings.Repeat(" ", runewidth.StringWidth(prefix))
	// ONE cursor for the whole row: a run the wrap splits keeps its emphasis on
	// the next line (the cursor remembers how much of the run is already drawn).
	cur := spanCursor{spans: e.spans}
	// The row's own emphasis, empty for every row but the reasoning one — see
	// Palette.Reasoning for why the reasoning row is the one that carries it (the
	// reader must be able to tell the model's aside from its answer at a glance).
	var rowStyle lineedit.Style
	if e.kind == entryReasoning {
		rowStyle = m.pal.Reasoning
	}
	var b strings.Builder
	for i, l := range lines {
		if i > 0 {
			b.WriteByte('\n')
			renderSpanLine(&b, &cur, l, indent, m.pal, rowStyle)
			continue
		}
		renderSpanLine(&b, &cur, l, prefix, m.pal, rowStyle)
	}
	return strings.Split(b.String(), "\n")
}

// plainText is the text the row DRAWS: its raw text when the row carries no
// emphasis, and its spans' text (the markers removed) when it does. Every
// measurement in this file works on this string, so the markers never occupy a
// cell — the raw text is kept only as the source the spans were tokenized from.
func (e entry) plainText() string {
	if len(e.spans) == 0 {
		return e.text
	}
	var b strings.Builder
	for _, sp := range e.spans {
		b.WriteString(sp.text)
	}
	return b.String()
}

// spanCursor walks a transcript row's emphasis runs in order, remembering how
// much of the current run has been written. A row's runs are consumed across
// SEVERAL wrapped lines (a run is cut wherever the wrap cuts the text), so the
// position in the run list is state that outlives one line — hence a cursor
// rather than an index re-derived per line.
//
// It is a value the caller copies per render: the row's own span slice is never
// mutated (a second render of the same frame — every frame re-renders the whole
// visible transcript — must see the same runs).
type spanCursor struct {
	spans []answerSpan
	i     int // the run being written
	off   int // how many bytes of spans[i] are already written
}

// done reports whether every run has been consumed.
func (c spanCursor) done() bool { return c.i >= len(c.spans) }

// run returns the current run.
func (c spanCursor) run() answerSpan { return c.spans[c.i] }

// rest returns the current run's remaining text.
func (c spanCursor) rest() string { return c.run().text[c.off:] }

// take consumes n bytes of the current run, advancing to the next run when this
// one is exhausted.
func (c *spanCursor) take(n int) {
	c.off += n
	if c.off >= len(c.run().text) {
		c.i++
		c.off = 0
	}
}

// skipBreak consumes a line-break run. The row's runs cover its text INCLUDING
// the newlines (that is what makes the runs' concatenation the row's text), but
// the wrapped lines handed to renderSpanLine never contain one — wrapText split
// the text there — so a break run is passed over rather than written.
func (c *spanCursor) skipBreak() bool {
	if c.done() || c.rest() != lineBreak {
		return false
	}
	c.i++
	c.off = 0
	return true
}

// renderSpanLine writes ONE wrapped line of a transcript row into out, applying
// the row's emphasis: each run of the line is drawn through lineedit.SpanStyle,
// and the line's text is the same characters the plain path would have written
// (the runs ARE that text, minus the markers).
//
// The line is matched against the runs by BYTE COUNT, never by searching for a
// run's text inside it: the wrap can cut a run anywhere, and a search would
// mistake a repeated word for the run's start. A line is a slice of the row's
// display text, so counting is exact — and the cursor carries the position from
// one line to the next.
func renderSpanLine(out *strings.Builder, cur *spanCursor, line, indent string, pal lineedit.Palette, plain lineedit.Style) {
	out.WriteString(indent)
	rest := line
	for len(rest) > 0 {
		if cur.done() {
			// The runs do not cover the line (they always should — and a row with
			// NO runs lands here for the whole line, which is the ordinary path for
			// every plain row). The remainder is written as plain text rather than
			// dropped: losing a reader's characters to a bookkeeping mismatch is the
			// one failure this path must not have.
			//
			// `plain` is the ROW's own style — empty for every row that has none,
			// which is why passing it changes no existing row's bytes (Style.Wrap
			// returns its text untouched when the style is empty) and why a row that
			// DOES have one (the reasoning row) gets it on the text that carries no
			// run of its own.
			break
		}
		if cur.skipBreak() {
			continue
		}
		n := len(cur.rest())
		if n > len(rest) {
			n = len(rest)
		}
		out.WriteString(lineedit.SpanStyle(pal, cur.run().em).Wrap(rest[:n]))
		rest = rest[n:]
		cur.take(n)
	}
	if rest != "" {
		out.WriteString(plain.Wrap(rest))
	}
}

// wrapText hard-wraps text to width display cells, honouring existing
// newlines. It wraps on the rune boundary only when a single cell-exceeding
// word cannot be broken further (a hard cut), so a long path stays readable.
func wrapText(text string, width, indent int) []string {
	avail := width - indent
	if avail < 1 {
		avail = 1
	}
	var out []string
	for _, para := range strings.Split(text, "\n") {
		if para == "" {
			out = append(out, "")
			continue
		}
		var line strings.Builder
		lineW := 0
		for _, r := range para {
			rw := runewidth.RuneWidth(r)
			if lineW+rw > avail && lineW > 0 {
				out = append(out, line.String())
				line.Reset()
				lineW = 0
			}
			line.WriteRune(r)
			lineW += rw
		}
		out = append(out, line.String())
	}
	return out
}

// truncateToWidth cuts a rendered line to the terminal width on a rune
// boundary, so the frame's line count stays what the layout promised.
func truncateToWidth(s string, width int) string {
	if width < 1 {
		return s
	}
	w := 0
	for i, r := range s {
		rw := runewidth.RuneWidth(r)
		if w+rw > width {
			return s[:i]
		}
		w += rw
	}
	return s
}

// formatToolActivity renders one live tool frame: the kind, the tool name and
// the server's own digest of the call/result. The summary is what makes the
// activity readable while the turn is still running (acceptance 3).
func formatToolActivity(v ToolActivity) string {
	label := v.Kind
	if label == "" {
		label = UIStepCall
	}
	return fmt.Sprintf("%s %s %s", label, v.Name, v.Summary)
}

// formatCommandResult renders a locally-executed slash command's frame.
func formatCommandResult(v CommandResult) string {
	if v.Error != "" {
		return fmt.Sprintf("%s → %s", v.Command, v.Error)
	}
	return fmt.Sprintf("%s → %s", v.Command, v.Output)
}
