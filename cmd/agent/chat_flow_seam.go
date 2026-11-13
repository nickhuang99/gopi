package main

// chat_flow_seam.go — who owns the screen while gopi chat's reserved layout is
// installed, and what the AGENT's own flow output must do about it.
//
// The defect this closes (measured with the SGR-aware grid instrument, 100×30,
// task 01a0c0a7): the REPL's own text was already routed into the answer band
// (cmd/agent/lineedit_wire.go, WriteLines/WriteNotice), but the AGENT's output
// during a turn was not — and it does not come from this package's REPL
// branches. `internal/agent` streams the live reasoning block and writes its
// diagnostic notices to destinations of its own (os.Stderr, and a log.Logger
// built over it), and it has no idea a layout exists. With the reserved layout
// installed the terminal is inside a scrolling region with the cursor parked on
// the row the editor last drew, so those writes landed on the layout's rows: the
// reasoning block tore the band's own rows and a wrapped
// `[gopi] ℹ️ thinking budget … clamped …` notice overwrote the STATUS row
// MID-LINE — measured on the very turn that produced a perfectly good answer:
//
//	13|budget_tokens, so the harness aborts a round whose streamed thinking…
//	15|                                                    [think
//	16|ing] The user said "hello world". That's a trivial greeting…
//	29|deepseek-flash · iter 1 · tools  thinking…[gopi] ℹ️ thinking budget 16384 …
//
// Two fixes were possible and only one of them is right. The wrong one is to let
// the agent write to the terminal and hope it lands somewhere harmless — that is
// the two-writer defect this family of work exists to remove, and the
// status-publish tests in internal/lineedit pin it explicitly (a publish from
// any goroutine other than the read loop writes ZERO bytes). The right one is to
// give the agent's output the SAME single owner: the editor, which draws every
// row and owns the only writer on the descriptor. So the agent gets a seam
// (Agent.OnThinking, mirrored on the existing OnText), the seam is installed
// ONLY while this process may own the screen, and the agent's bytes never reach
// the terminal by a private path again.
//
// The flow path is untouched BY CONSTRUCTION, not by a second decision here:
// with no seam installed the agent writes os.Stderr exactly as it always did, so
// cooked input, --no-editor, a piped run and the testsuite's greps see the same
// bytes. The second half of that guarantee is the editor's own report —
// Editor.BandActive answers false when no layout is installed — so even an
// installed seam writes the historic bytes when the layout is not there.
//
// WHY the seam BUFFERS, and why the buffer is not a formatter: the reasoning
// stream arrives as fragments of one line (the wire sends a few tokens per
// delta), and a band row per fragment would stack `Let` / `me` / `think` on
// three rows — the text intact but unreadable, and a row per token is a repaint
// storm. So the seam holds the unfinished tail and commits WHOLE rows, exactly
// as the answer renderer does; the bytes themselves are never rewritten, because
// rewording the agent's own output would make the two carriers disagree about
// one stream (the seam test pins the byte-identity of the reasoning stream
// against the stderr path).

import (
	"io"
	"os"
	"strings"

	"github.com/nick/gopi/internal/agent"
	"github.com/nick/gopi/internal/lineedit"
)

// flowRowBufferCells bounds the un-committed tail the seam holds while it waits
// for a line break. It exists so a stream that never sends one (a very long
// reasoning paragraph) still shows progress and the buffer cannot grow without
// bound; the value is a few screen rows, so the committed block reads as the
// TTY would have drawn it. Named (Rule 1a): it is a policy, not an incidental
// number.
const flowRowBufferCells = 256

// chatFlowSeam carries the agent's chat-visible flow output — the live reasoning
// stream, its markers, and the `[gopi]` log lines that ride the same stream —
// through the terminal's single writer instead of straight to os.Stderr.
//
// It is installed for the WHOLE chat run, not per turn: every method resolves at
// write time whether the layout really owns the screen
// (chatInput.bandActive), so a turn boundary, a resize below the layout's floor
// and a chat run that never installed a layout all resolve themselves without
// this type tracking editor state.
type chatFlowSeam struct {
	// input is the REPL's input layer, i.e. the owner of the reserved layout.
	// Nil (no editor path at all) makes every write fall through to flow.
	input *chatInput
	// fallback is the historic destination: os.Stderr, the stream the agent's
	// flow output has always used.
	fallback io.Writer
	// pending is the unfinished tail of the reasoning stream (see the file
	// comment): text the seam has NOT handed to the band yet. Only the agent's
	// goroutine touches it.
	pending strings.Builder
}

// newChatFlowSeam builds the seam over the REPL's input layer. A nil input is
// legal and simply never claims the text (flow output then behaves as before).
func newChatFlowSeam(input *chatInput, fallback io.Writer) *chatFlowSeam {
	if fallback == nil {
		fallback = io.Discard
	}
	return &chatFlowSeam{input: input, fallback: fallback}
}

// Flow hands one chunk of the agent's live reasoning stream to the seam. It is
// the func installed as Agent.OnThinking, so the chunk arrives exactly as the
// agent emitted it (markers included) and is never split on newlines here — a
// fragment would come back wrapped at the wrong place.
func (s *chatFlowSeam) Flow(chunk string) {
	if s.input == nil || !s.input.bandActive() {
		// No layout: the historic stream, and nothing left buffered behind it.
		s.drainFallback()
		_, _ = io.WriteString(s.fallback, chunk) //stylegate:ignore a failed write to the fallback stream cannot be reported: the session is already degraded to the historic stream and there is no reader left to tell
		return
	}
	s.pending.WriteString(chunk)
	s.flushRows()
}

// Write is the seam as an io.Writer, which is what the agent's log.Logger is
// pointed at: one call is one log LINE (a `[gopi] …` notice), and a line is its
// own row — never spliced onto the reasoning text that may still be unfinished
// on the live row. Flushing first is what keeps that true.
func (s *chatFlowSeam) Write(p []byte) (int, error) {
	if s.input == nil || !s.input.bandActive() {
		s.drainFallback()
		return s.fallback.Write(p)
	}
	// The notice starts its own row: whatever the reasoning stream had not
	// finished is committed first, so the row cannot carry two sources.
	s.commit(s.pending.String())
	s.pending.Reset()
	s.writeRows(string(p))
	return len(p), nil
}

// Flush commits the unfinished tail. The REPL calls it at the end of a turn, so
// a reasoning block whose last line carried no break is not left invisible.
func (s *chatFlowSeam) Flush() {
	if s.input == nil || !s.input.bandActive() {
		s.drainFallback()
		return
	}
	s.commit(s.pending.String())
	s.pending.Reset()
}

// flushRows commits every COMPLETE line in the buffer and keeps the remainder,
// committing that too once it grows past flowRowBufferCells (a stream that never
// sends a break must still show its text).
func (s *chatFlowSeam) flushRows() {
	text := s.pending.String()
	last := strings.LastIndexByte(text, '\n')
	if last < 0 {
		if lineedit.DisplayWidth(text) >= flowRowBufferCells {
			s.commit(text)
			s.pending.Reset()
		}
		return
	}
	s.commit(text[:last])
	s.pending.Reset()
	s.pending.WriteString(text[last+1:])
}

// writeRows commits already-split text, one row per line.
func (s *chatFlowSeam) writeRows(text string) {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		// A notice that was only a break (the agent's "newline after streamed
		// text") asks for a row of its own, exactly as the flow path's bare
		// Fprintln did.
		s.commit("")
		return
	}
	for _, line := range strings.Split(text, "\n") {
		s.commit(line)
	}
}

// commit writes ONE row through the layout's owner, wrapped at the BAND's own
// width.
//
// The wrap happens HERE, at the producer, and not in the band's row loop: where
// the flow text this seam is handed gets split is the decision of whoever
// produced it, and it uses the ONE wrap policy (lineedit.WrapToCols) so the
// producer's rows and the band's own safety split can never disagree about
// where a row ends (Rule 14). Handing an over-wide row straight down was the
// residual this closes — measured on the 100x30 grid, the layout's re-cut
// landed inside the reasoning marker's SGR: the band's next row read literally
// `31m...` (the sequence's own parameters as text) and its rows came up short by
// the introducer's bytes, which the re-cut had counted as cells.
//
// A layout that is no longer on screen (taken down by a resize mid-turn) is not
// a reason to drop text: such a row goes to the historic stream instead — the
// same bytes the flow path would have written, one row later. With no layout the
// width is 0, WrapToCols returns the row unchanged as ONE row, and this is
// exactly the single historic write, byte for byte.
func (s *chatFlowSeam) commit(row string) {
	for _, line := range lineedit.WrapToCols(row, s.input.bandCols()) {
		if s.input.writeBandRow(line) {
			continue
		}
		_, _ = io.WriteString(s.fallback, line+"\n") //stylegate:ignore a failed write to the fallback stream cannot be reported: the session is already degraded to the historic stream and there is no reader left to tell
	}
}

// drainFallback empties the buffer onto the historic stream (used when the
// layout is gone and a caller switches to flow output).
func (s *chatFlowSeam) drainFallback() {
	if s.pending.Len() == 0 {
		return
	}
	text := s.pending.String()
	s.pending.Reset()
	_, _ = io.WriteString(s.fallback, text) //stylegate:ignore a failed write to the fallback stream cannot be reported: the session is already degraded to the historic stream and there is no reader left to tell
}

// installChatFlowSeam points the agent's chat-visible flow output at the
// layout's single writer and returns the seam (so the REPL can Flush it at a
// turn boundary) plus the restore that puts the agent back the way it was.
//
// It is called once for a chat run, and only when the editor path is live —
// i.e. only when this process may own the screen. BOTH destinations are
// re-pointed, because the epic measured two independent tears: the live
// reasoning stream (Agent.OnThinking) and the agent's log.Logger (the `[gopi]`
// notices, which overwrote the status row mid-line). The logger's previous
// writer is restored rather than assumed to be os.Stderr, so a caller that had
// redirected it keeps its redirect while the layout is not on screen.
func installChatFlowSeam(ag *agent.Agent, input *chatInput) (*chatFlowSeam, func()) {
	if ag == nil || input == nil || input.Mode() != chatInputEditor {
		return newChatFlowSeam(input, os.Stderr), func() {}
	}
	seam := newChatFlowSeam(input, os.Stderr)
	prevThinking := ag.OnThinking
	ag.OnThinking = seam.Flow

	var prevLog io.Writer
	if ag.Logger != nil {
		prevLog = ag.Logger.Writer()
		ag.Logger.SetOutput(seam)
	}
	restore := func() {
		ag.OnThinking = prevThinking
		if ag.Logger != nil && prevLog != nil {
			ag.Logger.SetOutput(prevLog)
		}
	}
	return seam, restore
}
