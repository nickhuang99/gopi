package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/inputsource"
	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/replaymode"
	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
	"github.com/nickhuang99/gopi/internal/uiclient"
)

// Chat input path selection. The raw-mode editor is entered ONLY when stdin is
// a terminal; every other shape (a pipe from `echo`, a file redirect, a test
// harness, --no-editor) keeps the cooked reader byte-for-byte identical to the
// pre-editor behavior. These constants name the two modes so the branch reads as
// a decision, not as a condition.
const (
	// chatInputCooked reads completed lines with bufio (today's behavior).
	chatInputCooked = "cooked"
	// chatInputEditor drives the raw-mode line editor.
	chatInputEditor = "editor"
)

// The FOUR measured reasons a chat run has no editor-backed layout, named once
// (Rule 1b/13/14) because they are decided in newChatInputWithPalette and READ
// by every consumer built on the layout — currently the replay driver's refusal
// (cmd/agent/chat_replay.go), which must state WHICH one it met.
//
// The list is exactly as long as the branch set in newChatInputWithPalette, and
// that is the invariant a test pins: adding a fifth way to lose the layout
// without naming its reason is the silent degradation this field exists to end.
const (
	// chatNoEditorReasonNoEditor is the operator's own decision (`--no-editor`).
	chatNoEditorReasonNoEditor = chatFlagNoEditor + " was passed"
	// chatNoEditorReasonPipedStdin is stdin that is not a terminal at all
	// (a pipe, a redirect, a test harness).
	chatNoEditorReasonPipedStdin = "stdin is not a terminal (piped or redirected input)"
	// chatNoEditorReasonTooSmall is a real terminal whose height cannot hold
	// the reserved layout: the editor exists but no region does. The threshold
	// itself is NOT spelled here — the branch that refuses prints the number
	// through chatRegionMinRows(), the ONE definition (internal/lineedit), and
	// a second spelling of it would be a second answer to "how many rows are
	// enough".
	chatNoEditorReasonTooSmall = "the terminal is too small for the chat layout"
	// chatNoEditorReasonTerminalRefused is a terminal that IS one, but could
	// not be opened for raw mode (the probe/pty refused). It is the fourth
	// cause, and the reason the previous sentence had to enumerate a menu
	// instead of naming one.
	chatNoEditorReasonTerminalRefused = "the terminal could not be opened for raw mode"
	// chatNoEditorReasonNoInputLayer is the case of NO layer at all (a fixture
	// that built none, a path that returned early). Named rather than
	// dereferenced, because a nil check that panics is not a report (Rule 19).
	chatNoEditorReasonNoInputLayer = "this run has no input layer at all"
	// chatNoEditorReasonUnrecorded is a layer that exists but carries no
	// recorded cause. It states the gap instead of guessing a cause — the
	// whole point of a measured reason is that it is not invented (Rule 15).
	chatNoEditorReasonUnrecorded = "the input layer records no cause"
)

// chatInputNoLayoutReason reports WHY this input layer has no editor-backed
// layout — the measured cause, as one of the chatNoEditorReason* constants.
//
// IT IS THE ONE READER of chatInput.noLayoutReason, and it exists as a function
// rather than a field read because a NIL layer (a fixture, a run that never
// built one) must still answer a sentence: "no input layer at all" is a cause
// too, and the alternative — dereferencing the field at the call site — is a
// panic in the arm that is supposed to be REPORTING a degradation (Rule 19).
//
// A layer that carries no recorded reason is reported as such rather than
// guessed at: the zero value names no cause, and inventing one here would be
// the menu-of-possibilities defect this whole mechanism removes (Rule 15).
func chatInputNoLayoutReason(in *chatInput) string {
	if in == nil {
		return chatNoEditorReasonNoInputLayer
	}
	if in.noLayoutReason != "" {
		return in.noLayoutReason
	}
	return chatNoEditorReasonUnrecorded
}

// chatHistoryFileName is a local alias for the config-owned history file name.
// It exists so this file never spells the name (Rule 1b): the value comes from
// the single definition in internal/config, which the editor's writer and the
// next session's reader both join.
const chatHistoryFileName = config.ChatHistoryFileName

// stdinFdMin is the lowest descriptor number that can carry terminal state.
const stdinFdMin = 0

// chatPasteStore resolves the paste store for the run's OWN session directory:
// a long paste spilled by the editor lands at
// <session dir>/paste/paste-<stamp>-<n>.txt, beside the run's session log and
// raw log — one directory per run, so a session's spills belong to the session
// that produced them and go away with it (no index, no sweep, no new key).
//
// THE STORE STAMPS THE HUMAN FACE (inputsource.Human). Everything this store
// writes came in through the operator's own terminal — the editor is the input
// LINE, and the only face that reaches it is the person typing or pasting. The
// model's own line does not pass through it (chat_surface.go hands it straight to
// the seam) and neither does another process's (`gopi notify` writes an inbox
// record). So the reference the editor renders says source=human, and a reader
// can tell it from a notify delivery's.
//
// The directory is taken from the session logger itself (the JSONL path's own
// directory) rather than re-derived: sessionDirForID is what decides that path,
// and a second derivation here would be a second answer to "where is this run's
// session directory" that could disagree with the log's.
//
// A nil logger (session logging failed, best-effort) yields a nil store: the
// editor then keeps the historic behaviour — the paste is typed into the buffer —
// rather than writing files somewhere nothing owns.
func chatPasteStore(sl *agent.SessionLogger) *lineedit.PasteStore {
	if dir := chatSessionDir(sl); dir != "" {
		return lineedit.NewPasteStoreFor(dir, inputsource.Human)
	}
	return nil
}

// chatSessionDir is the ONE derivation of the run's session directory from its
// session logger, and both halves of the spill vocabulary read it here.
//
// The two halves are the editor's (a long paste spilled as it is READ —
// PasteStore.Spill) and the message boundary's (an assembled over-threshold line
// spilled by cmd/agent/chat_boundary.go). They are the same vocabulary — one
// file layout, one reference spelling — so they must agree about the directory
// as well: a boundary that re-derived the directory could spill a line into a
// directory the editor's reference never names, and the model's `read` would
// then resolve to nothing. Rule 14's shape, applied to the directory.
//
// The directory is the JSONL path's OWN directory rather than a second
// derivation from sessionDirForID, so a second answer to "where is this run's
// session directory" cannot exist — and the answer itself is taken from the ONE
// definition of that rule (sessionlayout.RecordContainer, PROJECT.md Rules 13/14),
// never re-derived here. An empty string is the honest answer for a
// logger that failed to open (best-effort): the callers then keep their historic
// behaviour rather than filing bytes somewhere nothing owns.
func chatSessionDir(sl *agent.SessionLogger) string {
	if sl == nil {
		return ""
	}
	path := strings.TrimSpace(sl.Path())
	if path == "" {
		return ""
	}
	return sessionlayout.RecordContainer(path)
}

// SetPasteStore installs the run's paste store on the raw-mode editor: from then
// on a read that fills the read buffer is SPILLED to that store's directory and
// replaced on the input line by its reference (lineedit/pastestore.go). It is
// installed AFTER the input layer is built, because the store's directory is the
// SESSION directory and the session log is opened before the editor — and it is a
// no-op on the cooked path, which has no editor and no buffer to keep small.
func (ci *chatInput) SetPasteStore(s *lineedit.PasteStore) {
	if ci != nil && ci.editor != nil {
		ci.editor.SetPasteStore(s)
	}
}

// chatInput is the chat REPL's line source: one implementation over the cooked
// reader and one over the raw-mode editor, so the REPL loop does not care which
// is active.
type chatInput struct {
	// Editor mode.
	editor *lineedit.Editor

	// term is the raw-mode terminal the editor drives, kept so the
	// per-turn-interrupt seam can reach the SIGNAL half of Ctrl+C (see
	// SetInterrupt). Nil on the cooked path — where there is no raw mode and
	// Ctrl+C is a real SIGINT, handled by the REPL's own signal arm.
	term *lineedit.Terminal

	// Cooked mode.
	reader *bufio.Reader

	// errOut is where the input layer reports its own degradations (an editor
	// that could not enter raw mode, or a terminal too small for the reserved
	// layout). It is the Rule 15 landing spot: the user is told, always.
	errOut io.Writer

	// noLayoutReason names WHY this run has no editor-backed layout, in the
	// vocabulary of the one place that knows (newChatInputWithPalette decides
	// it): the value is always one of the four chatNoEditorReason* constants,
	// or "" on a run whose layout IS installed.
	//
	// IT EXISTS BECAUSE "no layout was installed" HAS FOUR CAUSES AND ONLY ONE
	// OF THEM IS A CHOICE. A consumer that is built ON the layout — `gopi chat
	// --replay` films the chat LAYOUT, so its whole playback control layer
	// lives at the editor's key seam — could previously say only "piped input,
	// --no-editor, or a terminal too small", a menu of three, because the mode
	// string ("cooked") cannot tell a pipe from a flag from a short terminal,
	// and the fourth cause (a terminal that answered, but could not be opened
	// for raw mode) was not in the menu at all. This field carries the MEASURED
	// cause instead, so a reader is told WHICH one it was rather than being
	// handed the list (Rule 15). chatInputNoLayoutReason is its ONE reader.
	noLayoutReason string

	// flowOut is where the FLOW path writes the caller's text — os.Stdout, the
	// destination the historic `fmt.Println(out)` used, so a piped script reads
	// exactly the bytes it always did. It is deliberately NOT the editor's own
	// writer (that is os.Stderr: the editor draws the screen there, while
	// stdout stays the script-readable stream). Nil means os.Stdout.
	flowOut io.Writer

	// pal is the emphasis table the run resolved ONCE (runChatWithInput): the
	// editor owns the surfaces inside the layout, and the REPL owns the rows it
	// hands to the BAND (WriteToolRow), so both must draw from the same table —
	// a second resolution would be a second capability decision with its own
	// downgrade notice.
	pal lineedit.Palette

	// tool is the tool-activity row's emphasis table (term_tool_row.go), resolved
	// at the SAME moment as pal and from the SAME decision: a monochrome palette
	// means the row's styles are the zero table, so the row emits no sequence at
	// all rather than resets. It lives here because this struct is what the REPL
	// hands the band, and a second resolution at a call site would be a second
	// capability decision (see pal's own comment).
	tool termToolStyles

	// measured is the terminal's OWN answer about itself (lineedit's capability
	// probe), captured when the editor opened the terminal and kept here because
	// it is the evidence behind the table above: the palette was chosen from it,
	// and the dump record quotes it beside the environment's declaration so a
	// reader can see WHICH of the two decided. Nil-ness is expressed by
	// Measurement().Measured() — a probe that read nothing is still a fact worth
	// recording ("asked, nothing came back"), which is why this is a struct and
	// not a pointer.
	measured lineedit.CapabilityMeasurement

	// cap is the capability the table above was drawn from — the declaration and
	// the measurement together, kept so the run can REPORT which evidence
	// decided the depth (lineedit.CapabilityReport). A surface that reported the
	// table without this could only say "256 colours" while being unable to say
	// whether the terminal said so or the environment did.
	cap lineedit.Capability

	// dumper is the screen-dump sink this input's paint stream is teed into, when
	// the run asked for one (`--dump-screen`). It is held HERE rather than only by
	// the REPL because the close-out is the SECOND writer of this stream: taking
	// the layout down (region.Reset, the scroll-region reset, the cursor show)
	// paints the EXIT, and a dump that interpreted those bytes would describe a
	// terminal nobody was looking at. Freezing before them is the difference
	// between an artifact of the screen and an artifact of the teardown.
	dumper *uiclient.ScreenDumper

	// injector is the key-injection seam of the ACTIVE editor, nil on the cooked
	// path (and on the editor path when the source could not be wrapped). It is
	// held here because the chat_surface tool reaches the surface through this
	// layer: `key` injects into the same Source the editor reads, so the editor's
	// own decoder decides what the bytes mean — the same path a keyboard feeds.
	injector *lineedit.Injector

	// announce is teed the text of every notice this layer announces
	// (WriteNotice), IN ADDITION to errOut, when a caller installs one. It is a
	// CEILING-AND-ANNOUNCEMENT seam, not a second console: the notice still goes
	// to errOut unchanged, so a run's bytes are identical with or without it.
	//
	// WHY IT EXISTS. "How many times did the console ANNOUNCE this notice?" is a
	// question about an EVENT, and a run's only other record of it — the painted
	// byte stream — merges two writers into one (the notice's own band draw, and
	// the editor's incremental repaint of the rows it tracks), so a row announced
	// once can appear there twice and a repaint can never appear here. A caller
	// that must state an exact count therefore counts HERE, at the announcement,
	// and the painted stream keeps the claim it is good at ("this wording reached
	// the console"). cmd/agent's bound case is that caller
	// (chat_surface_bound_e2e_test.go: countingWriter + sawNoticeOnce).
	announce io.Writer

	// redact, when set, masks a row THIS LAYER composes before it reaches either
	// destination (chat_replay_redact.go). Held as a function rather than as the
	// driver, because this layer must not know what a beat protocol is — the
	// replay arms it, and a plain run leaves it nil and gets its bytes back
	// unchanged.
	//
	// It is set AFTER the layer is built (the driver needs the layer first), so a
	// row composed before the arming — the historic one-liner — is NOT masked;
	// that is deliberate and harmless: the one-liner is a constant in this
	// program, carries no run of its own, and prints before any pass could exist.
	redact func(string) string

	// replayFaces is the queue of RECORDED faces for the lines a replay driver is
	// about to type, in the order it will type them (see noteReplayFace).
	//
	// WHY THIS EXISTS AT ALL, AND WHY IT IS A QUEUE (measured 2026-11-04, session
	// 0b51ae4a-fa82-41ba-8147-1ba3ebc92476). A replay re-enters recorded lines as
	// KEYSTROKES, and the arm that turns a keystroke into a turn's face
	// (chatSealSource) answers `human` for every one of them BY CONSTRUCTION. The
	// recording, meanwhile, holds turns that arrived from `gopi notify` — complete
	// inputs whose face is `notify`. So the replayed line carried the wrong face,
	// the surface's own predicate denied it a turn, and the counter that advances
	// only on a turn-opening row froze: 7 bands numbered 1,2,2,2,2,2,2 on the
	// filmed screen against 1..7 on the screen being re-run.
	//
	// THE FIX IS NOT "TRUST THE TEXT" AND NOT "SUSPEND THE PREDICATE": the face a
	// row was RECORDED under rides in from replaydrive.Turn.Source, is pushed here
	// by the driver as it types, and is handed to the predicate the REPL already
	// uses — so the number a band prints and the row that opened it are judged by
	// ONE rule over ONE face (Rule 14). A replay of a `human` line still gets
	// `human`, because that is what the recording says.
	replayFaces []string
	// replayFacesMu guards replayFaces: the driver PUSHES from its own goroutine
	// while the feeder POPS from the reader's.
	replayFacesMu sync.Mutex

	// replayMoments is the queue of RECORDED opening moments for the lines a
	// replay driver is about to type, paired with replayFaces entry for entry
	// (see noteReplayMoment).
	//
	// WHY IT EXISTS. A live REPL mints a turn's moment from its own clock, and on
	// a LIVE run that is the truth. Replayed, it is the same lie the keystrokes
	// tell one field over: measured 2026-11-08, replaying session 6b01dd46
	// (whose first turn opened 2026-11-07 12:11:17) painted
	// `turn 1 · 2026-11-08 15:59:32` — TODAY, on a recording a day old. The
	// recording already holds the moment (replaydrive.Turn.OpenedAt, read from
	// the row's own timestamp), so the surface asks the record rather than the
	// clock, and a replay of last week prints last week.
	//
	// THE ZERO VALUE IS CARRIED, NOT SKIPPED, which is where this queue differs
	// from replayFaces: "the row recorded no moment" is a fact the banner must
	// STATE (sessionevent.TurnStampPlaceholder), while "the row recorded no face"
	// is a fact the reader answers with the arm's own default. Dropping a zero
	// moment here would silently hand the turn to the clock — the exact defect.
	replayMoments []time.Time
	// replayMomentsMu guards replayMoments, for the same two-goroutine reason.
	replayMomentsMu sync.Mutex
}

// noteReplayFace queues the face a replay driver is about to type ONE line under.
// It is called by the driver immediately before the keystrokes of that line, so
// the queue's order is the order the lines reach the reader.
//
// A face this vocabulary cannot name is not queued: an empty value is the honest
// answer for "the recording did not say", and the reader then falls through to the
// arm's own face rather than being handed a fabricated one (Rule 15a).
func (in *chatInput) noteReplayFace(face string) {
	if face == "" {
		return
	}
	in.replayFacesMu.Lock()
	in.replayFaces = append(in.replayFaces, face)
	in.replayFacesMu.Unlock()
}

// takeReplayFace pops the face queued for the line the reader just read, or ""
// when nothing was queued — which is every run that is not a replay, and the
// replay's own trailing `/quit`.
//
// IT IS POPPED ON THE READ, NEVER ON THE KEYSTROKE, and that pairing is what makes
// the queue exact: one push per turn the driver types, one pop per line the REPL
// receives, both in the same order.
func (in *chatInput) takeReplayFace() string {
	in.replayFacesMu.Lock()
	defer in.replayFacesMu.Unlock()
	if len(in.replayFaces) == 0 {
		return ""
	}
	face := in.replayFaces[0]
	in.replayFaces = in.replayFaces[1:]
	return face
}

// noteReplayMoment queues the moment a replay driver recorded for the ONE line
// it is about to type, in the same call sequence and therefore the same order as
// noteReplayFace (the two queues are popped together by takeReplayLine).
//
// IT QUEUES THE ZERO VALUE ON PURPOSE: a recording whose turn-opening row
// carried no readable timestamp must reach the banner as an ABSENCE, so the band
// prints the shared placeholder. Skipping it here would leave the turn with
// whatever the live arm minted — the clock — and a replayed old session would
// then state today (Rule 15; the measured defect is on Turn.OpenedAt).
func (in *chatInput) noteReplayMoment(at time.Time) {
	in.replayMomentsMu.Lock()
	in.replayMoments = append(in.replayMoments, at)
	in.replayMomentsMu.Unlock()
}

// takeReplayMoment pops the moment queued for the line the reader just read, and
// reports whether a replay driver queued one for THIS read at all.
//
// THE bool IS NOT THE MOMENT'S ZERO-NESS, and conflating the two is the bug this
// signature exists to prevent: a recorded turn with no timestamp must still count
// as a replay line (so the surface states the placeholder), while a line the
// DRIVER never typed (the trailing `/quit`, and every line of an ordinary run)
// must not be stamped at all. The queue's non-emptiness is the only honest
// answer to "did a replay push a moment for this read?" — so it is returned
// separately rather than inferred from the value.
//
// IT IS POPPED ON THE READ, NEVER ON THE KEYSTROKE, for the reason takeReplayFace
// gives: one push per turn the driver types, one pop per line the REPL receives,
// both in the same order.
func (in *chatInput) takeReplayMoment() (time.Time, bool) {
	in.replayMomentsMu.Lock()
	defer in.replayMomentsMu.Unlock()
	if len(in.replayMoments) == 0 {
		return time.Time{}, false
	}
	at := in.replayMoments[0]
	in.replayMoments = in.replayMoments[1:]
	return at, true
}

// chatInputMode reports which mode is active ("cooked" or "editor") — the REPL
// prints it, so the user knows why their arrow keys do (or do not) edit.
func (in *chatInput) Mode() string {
	if in.editor != nil {
		return chatInputEditor
	}
	return chatInputCooked
}

// keyboardProtocol reports the keyboard negotiation the raw-mode terminal
// performed (chat_keyboard_protocol.go prints what it decides). A cooked run has
// no terminal and therefore no negotiation: it answers "not probed" rather than
// borrowing a verdict it never measured (Rule 15).
func (in *chatInput) keyboardProtocol() (state lineedit.KeyboardProtocol, flags int, answered bool) {
	if in.term == nil {
		return lineedit.KeyboardProtocolUnavailable, 0, false
	}
	flags, answered = in.term.KeyboardProtocolFlags()
	return in.term.KeyboardProtocol(), flags, answered
}

// ReadLine returns the next line, or ok=false at end of input (EOF, Ctrl+D, or
// Ctrl+C at the prompt — the REPL treats both as "exit cleanly", which is what
// it did before the editor existed).
//
// kind says WHICH end this was, because the caller records it and the record
// must not collapse three different events into one (task 01a163f8). It is the
// zero value whenever ok is true, and it is what the editor measured — never an
// inference from the reader's own state:
//
//	lineedit.EOFKindKey      Ctrl+D on an empty line (a human asked to stop)
//	lineedit.EOFKindSource   the SOURCE ended: a closed stdin, a pipe whose
//	                         writer exited, a terminal that went away
//	chatEndKindInterrupt     Ctrl+C at the prompt
//	chatEndKindCookedEnd     the cooked path reached end of input (no editor, so
//	                         the byte-level cause — Ctrl+D vs a closed pipe — is
//	                         not knowable here; said as its own word rather than
//	                         guessed, Rule 15)
func (in *chatInput) ReadLine() (line string, ok bool, kind string, err error) {
	if in.editor != nil {
		res, err := in.editor.Run()
		if err != nil {
			return "", false, "", err
		}
		if res.EOF {
			return "", false, res.EOFKind, nil
		}
		if res.Interrupted {
			return "", false, chatEndKindInterrupt, nil
		}
		return res.Line, true, "", nil
	}
	// Deliberate parity with the pre-editor reader: ANY read error — including
	// io.EOF carrying a final partial line — ends the input. The old feeder
	// checked err before using the line, so a trailing line without a newline
	// was dropped; reproducing that exactly is the point of the cooked path
	// (it must stay byte-identical, not merely similar).
	line, err = in.reader.ReadString('\n')
	if err != nil {
		return "", false, chatEndKindCookedEnd, nil
	}
	return line, true, "", nil
}

// The end-of-input kinds this layer adds to lineedit's own vocabulary: the two
// events the editor cannot name, because they are facts about the REPL rather
// than about the key stream. One definition site (Rule 1b) — the reader writes
// them, the REPL's close-out maps them to a session_end reason.
const (
	// chatEndKindInterrupt is Ctrl+C at the prompt with no turn in flight.
	chatEndKindInterrupt = "interrupt"
	// chatEndKindCookedEnd is end of input on the COOKED path: with no editor
	// there is no key stream to ask, so "which byte ended it" is genuinely
	// unknown and gets its own word instead of borrowing Ctrl+D's.
	chatEndKindCookedEnd = "cooked_end"
)

// Answer returns the streamed-answer renderer the REPL feeds each turn's deltas
// to. It is nil on the cooked path (and on a terminal that could not host the
// reserved layout): the REPL then writes the answer in flow with the historic
// buffered renderer, byte-for-byte as before A5.
func (in *chatInput) Answer() *lineedit.AnswerRenderer {
	if in.editor == nil {
		return nil
	}
	return in.editor.Answer()
}

// RecordEditorDegradations points the EDITOR's degradation reports at a RECORD
// as well as the operator's terminal.
//
// WHY THE EDITOR NEEDS ITS OWN ROAD HERE (measured, task 01a18990). The input
// layer binds the editor's errOut to os.Stderr, while routeChatDiagnostics
// redirects only the AGENT's logger — so a chat run's console.log never carried
// the editor's own sentences, and `grep 'read loop is stalled' console.log`
// returned 0 after a real stall. That is a silent failure of the REPORT (Rule
// 15): the one session where the stall mattered is the one whose record cannot
// show it, and the next reader concludes it never happened.
//
// The writer is the session's diagnostics file, opened by routeChatDiagnostics.
// It is a RECORD and never the flow seam: a degradation that queued a band row
// would re-enter the very wait it is reporting on. Nil keeps the historic
// stderr-only behaviour byte for byte.
func (in *chatInput) RecordEditorDegradations(w io.Writer) {
	if in == nil || in.editor == nil || w == nil {
		return
	}
	in.editor.SetDiagSink(func(sentence string) {
		_, _ = io.WriteString(w, sentence) //stylegate:ignore a record's write failure must not disturb the run the sentence is about, and there is nothing above it to report to
	})
}

// SetInterrupt installs the per-turn interrupt seam: how the input layer learns
// whether a turn is in flight (`active`) and how it cancels that turn
// (`interrupt`, told WHICH input carried the key, and reporting whether it
// really claimed it).
//
// IT INSTALLS BOTH HALVES ON BOTH ROUTES, and that is the whole point. A
// raw-mode terminal clears ISIG, so Ctrl+C arrives as the 0x03 byte and is
// classified in the editor's read loop; a cooked input (piped stdin, --no-editor,
// or a terminal that could not enter raw mode) leaves ISIG on, so Ctrl+C is a
// real SIGINT. One hardware key, two transports — and the meaning is decided in
// ONE place (the caller's `interrupt`), so the two can never drift apart. That
// is also why the origin travels WITH the call: the editor names the key it
// decoded (Ctrl+C, or a lone Escape), and this half names the signal, so the
// marker and the session record can say which one the operator actually used.
//
// The signal half ALWAYS CLAIMS a terminating signal on this surface. With a
// turn in flight the claim is the per-turn interrupt (unchanged). At the prompt
// it is the graceful closeout — the same ending the Ctrl+C KEY gets one layer
// up, and the one this function used to leave to the fatal re-raise.
//
// WHY THE PROMPT HALF HAD TO CHANGE (measured, 2026-10-22, session b9936006).
// A raw-mode terminal clears ISIG, so the Ctrl+C KEY is the 0x03 byte and never
// becomes a signal — but a real SIGINT (kill -INT, a process-group delivery, an
// ssh teardown) DOES arrive here. This handler answered `false` whenever no turn
// was in flight, and false means lineedit's CAPTURE SIGNALS FATAL PATH:
// restore the attributes, signal.Reset, re-raise. That ran on the editor's own
// goroutine and won the race against the REPL's closeout by three milliseconds —
// measured on this branch's binary, a `kill -INT` at the prompt produced
// `exit_code: -2`, ZERO session_end records, no dump and no provenance file,
// while `kill -HUP` and `kill -TERM` (which never pass through CaptureSignals)
// produced exit 1, one session_end naming the signal, and a `signal` dump.
//
// So the surface had two meanings for one signal and only one of them left a
// record. Claiming here does NOT create a second closeout path: the REPL's OWN
// watcher (cli_chat.go, signal.Notify over chatGracefulSignals) is registered for
// SIGINT, Go delivers one delivery to EVERY registered channel, and that watcher
// closes interruptCh — which is what wakes the prompt select and breaks the loop
// to finishChat. `return true` only stops lineedit from killing the process
// before that unwind can run; the terminal is restored by the closeout's own
// input.CloseOut(), which is the same restore the fatal path was doing by hand.
func (in *chatInput) SetInterrupt(active func() bool, interrupt func(lineedit.KeyOrigin) bool) {
	if in.editor != nil {
		in.editor.SetMinTurnActive(active)
		in.editor.SetInterruptTurn(interrupt)
	}
	if in.term == nil {
		return
	}
	in.term.SetInterruptHandler(func() bool {
		if !active() {
			// AT THE PROMPT: the signal means "end this session", and the claim
			// is what lets the REPL's own watcher deliver that ending instead of
			// letting CaptureSignals re-raise and take the process out first.
			return true
		}
		interrupt(lineedit.OriginSignal)
		return true
	})
}

// SetStatus installs the one-line status source (model / iteration / tokens) and
// the wake seam a publish uses. The wake comes from the SOURCE when it can
// supply one (the terminal does; a scripted test source does): without it a
// publish still reaches the screen, but only on the next keystroke — which
// StatusNotifyEnabled reports so the REPL can say so.
func (in *chatInput) SetStatus(fn func() lineedit.Status) {
	if in.editor == nil {
		return
	}
	in.editor.SetStatus(fn)
	// The seam comes from the ONE resolver (lineedit.SourceNotifier), which knows
	// both spellings a source may use — asking bare `src.(Notifier)` is what made
	// production's wake pipe UNREACHABLE, because the terminal's notifier is
	// deliberately spelled WakeNotifier (raw.go). With no seam installed the
	// editor's read loop parks in an unbounded read and a publish can only reach
	// the screen when a keystroke wakes the loop.
	if n := lineedit.SourceNotifier(in.editor.Source()); n != nil {
		in.editor.SetNotification(n)
	}
}

// PublishStatus announces that the status source's values changed. It performs
// NO terminal I/O (the editor's read loop owns every write — see
// lineedit.Editor.PublishStatus): the REPL therefore never interleaves a cursor
// placement with the editor's own repaint.
func (in *chatInput) PublishStatus() {
	if in.editor != nil {
		in.editor.PublishStatus()
	}
}

// StatusNotifyEnabled reports whether a publish actually reaches the editor's
// blocked read. False means the status row refreshes on the next keystroke —
// the caller announces that (Rule 15) instead of leaving the user to wonder.
func (in *chatInput) StatusNotifyEnabled() bool {
	return in.editor != nil && in.editor.NotifyEnabled()
}

// WriteLines prints the REPL's own text (the /help panel, a slash command's
// output) so it never touches the reserved layout's geometry.
//
// With a reserved layout installed this MUST NOT be a plain write to stdout:
// the terminal is inside a scrolling region and the cursor is parked on the row
// the editor last drew, so flow output lands on the editing row and every LF
// scrolls the band up by one — the panel paints itself over its own lines (the
// torn /help render this exists to fix; region.WriteLines carries the measured
// screen).
//
// The reserved path streams the text into the answer band through the editor
// (which confines every row with an absolute placement and one region scroll,
// the same path a committed answer line takes). The cooked path, and a terminal
// too small for the layout, keep the historic flow output BYTE-FOR-BYTE: there
// is no layout whose rows could be overwritten, and scripts read that stream.
// CloseOut erases the reserved layout's rows and parks the cursor, so the REPL's
// final line (its own "bye.") — or, on the cooked path, the shell's own prompt —
// starts at the screen's first cell with nothing stale above or around it. It
// reports whether the screen WAS owned by the layout: with no layout there is
// nothing of ours on the screen, and the caller keeps its historic bytes.
//
// WHY the REPL needs this at all, when the editor's own Close() runs on the way
// out: Close() is deferred, so it runs AFTER the goodbye is written. That order
// can only tell the terminal to erase, and the goodbye it just printed goes with
// the erasure — the user sees a blank screen, which is worse than the stale band
// this exists to remove. Erase FIRST, then say goodbye: the operator's own
// request ("when quit/exit, workarea is not cleaned up", attachment
// tmp/ui_cleanup.txt) is satisfied and the goodbye survives.
func (in *chatInput) CloseOut() bool {
	// Freeze the observation FIRST: the bytes below hand the terminal back, and
	// they are not the screen the operator read. Close is idempotent (the REPL's
	// own deferred finalize re-freezes the same frame), so this ordering costs
	// nothing and is the only one that yields the painted screen.
	if in.dumper != nil {
		if err := in.dumper.Close(); err != nil {
			fmt.Fprintf(in.errOut, "gopi chat: the screen dump could not be finalized: %v\n", err)
		}
	}
	if in.editor == nil {
		return false
	}
	return in.editor.CloseOut()
}

// hasDumpSink reports whether this run has a screen dump to photograph. The beat
// protocol (chat_replay_beats.go) REFUSES to start without one: a ledger whose
// beats carry no grid is a film of nothing, and the driver would wait on frames
// nobody writes.
func (in *chatInput) hasDumpSink() bool { return in != nil && in.dumper != nil }

// CopyDumpTo copies the settled dump to dst, creating dst's directory. It is the
// beat protocol's per-beat snapshot: the sink's own file is a live window onto the
// surface and is overwritten by the next beat, while the film needs one grid per
// frame (a frame's mask boxes are only valid on the grid that was on screen when
// it was grabbed).
//
// It SETTLES first, for the same reason internal/replay does: the copy must be the
// state at this beat, not the last state a paint happened to leave behind.
func (in *chatInput) CopyDumpTo(dst string) error {
	if in == nil || in.dumper == nil {
		return errors.New("this run has no screen dump sink")
	}
	in.SettleDump()
	if err := scratch.MkdirAllWithSuiteRunOwnerPerm(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(dst), err)
	}
	src, err := os.Open(in.dumper.Path())
	if err != nil {
		return fmt.Errorf("open the dump %s: %w", in.dumper.Path(), err)
	}
	// The reader cannot read a descriptor the copy above has not already drained
	// and closed; src is open for the whole function, so this is teardown of our
	// own descriptor on the success path and of the copy's on the failure path.
	defer func() { _ = src.Close() }() //stylegate:ignore read-only input descriptor whose reader has already drained it; a close error cannot change the copied bytes
	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	if _, err := io.Copy(out, src); err != nil {
		_ = out.Close() //stylegate:ignore the copy failure is the one being returned; the close is teardown of a file it could not fill, and a close error would only bury it
		return fmt.Errorf("copy the dump to %s: %w", dst, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close %s: %w", dst, err)
	}
	return nil
}

// SettleDump installs the CURRENT painted frame as the dump's state, for a
// caller that has reached a point where the screen is final — the end of a
// turn's paint.
//
// THE DEFECT IT CLOSES (measured 2026-09-27, task 01a0e433). The sink used to be
// written ONLY when the paint stream changed, and a chat REPL paints its answer
// from the turn's goroutine and then goes quiet: the next paint is the next
// turn, i.e. the next keystroke. An armed run that was merely OBSERVED therefore
// froze its artifact on whatever frame the throttle happened to catch mid-turn —
// measured `frames=35 dumps=28 … iter 0 · ready`, the answer absent from the
// grid, byte-identical for 10s over 40 samples. The only way an agent could make
// the answer appear was to TYPE, and a typed repaint can be caught mid-paint —
// and THAT is where the artifact's literal escape-sequence cells were first
// READ (`…1449-150m`, `…1451-1524;104H`). The frame boundary is where the defect
// is reached, not what produces it (corrected 2026-09-28, task 01a0e81b): the
// interpreter reads every escape of every frame, and what painted the parameters
// was the carry policy in internal/lineedit/screen.go (holdPending) truncating a
// held sequence to its last bytes and so dropping the introducer. See
// internal/lineedit/screen_introducer_lost_test.go for the pty-free repro.
//
// WHY THE TURN BOUNDARY IS THE RIGHT MOMENT. By the time the REPL calls this,
// renderer.Finish/EndTurn have committed the answer's tail and the flow seam has
// been flushed — and all of those write SYNCHRONOUSLY into this same paint
// stream, on the turn's own goroutine. So the frame this installs is the whole
// turn, and it is installed without waiting for anything: no timer, no input,
// no second observation path.
//
// A nil sink (no --dump-screen) is a no-op, and the failure is reported on the
// run's own error channel rather than swallowed (Rule 15).
func (in *chatInput) SettleDump() {
	if in == nil || in.dumper == nil {
		return
	}
	// The turn's own writes arrive on TWO roads: the flow seam's band frames
	// (waited for at the seam) and the ANSWER renderer's calls, which an editor
	// with a read loop applies on that loop (see lineedit/answerqueue.go). The
	// frame this installs is the turn only once BOTH have landed — otherwise the
	// dump freezes a grid the loop has not finished applying, and the artifact
	// describes a screen the operator never saw.
	if in.editor != nil {
		in.editor.WaitAnswerDrawn()
	}
	if err := in.dumper.Settle(); err != nil {
		fmt.Fprintf(in.errOut, "gopi chat: the screen dump could not be settled at the turn boundary: %v\n", err)
	}
}

// VoiceInsertLine appends one transcript to the editing line, through the
// editor's own insert road (internal/lineedit: the text is QUEUED and the read
// loop applies it, so no goroutine here becomes a second writer of the line).
//
// It reports whether the road ACCEPTED the text. False means it did not — no
// editor (the cooked path), or the road's backlog is full — and the CALLER
// states that (the voice mode reports it and leaves the mode; Rule 15: a
// transcript that could not land is never silently dropped).
func (in *chatInput) VoiceInsertLine(text string) bool {
	if in == nil || in.editor == nil {
		return false
	}
	// IT IS THE DICTATION ROAD, not the ordinary append: the runes this call
	// inserts carry their provenance into the buffer, which is what lets the
	// submitted line say "this was dictated" by MEASURING the line rather than by
	// remembering that the mode was on (chat_voice_input.go, valuevoice.go).
	return in.editor.InsertDictationAtEnd(text)
}

// VoiceRunes reports how many runes still on the editing line carry dictation
// provenance — the ONE reading behind this carrier's voice-input prefix.
//
// IT IS SAID AS A COUNT rather than "was voice mode open", and the difference is
// the whole requirement: an operator who dictates a sentence, deletes it and
// types their own is submitting pure typing, and the buffer's own mutations are
// what make the count fall to zero. A carrier that asked the mode instead would
// mark that line as dictated and tell the model something about the text that is
// not true.
//
// A nil input, or a cooked run with no editor, answers 0 — there is no line to
// measure, and "no dictated characters" is the honest reading of no line
// (Rule 15: the prefix is absent rather than guessed at).
func (in *chatInput) VoiceRunes() int {
	if in == nil || in.editor == nil {
		return 0
	}
	return in.editor.Value().VoiceRunes()
}

// VoiceNotice writes one of the voice mode's own rows to the console. It is the
// SAME single-writer road as every other REPL row (WriteNotice): under the
// reserved layout the row is committed to the band by the read loop, so the
// status row and the editing line are not torn by a foreign write.
func (in *chatInput) VoiceNotice(text string) {
	if in == nil {
		return
	}
	in.WriteNotice(text)
}

// VoiceNoticeStyled is the SAME road with the run's system voice: the rows are
// produced by term_note_row.go from the text and the table this input resolved at
// startup (termToolStylesFor / termToolStylesFromPalette), so `/voice` explains
// itself in the ONE emphasis this project gives a surface's remarks about itself
// — dim, low-contrast, and never on the wide/noisy stress a failure would take.
//
// WHY IT TAKES THE TEXT AND NOT THE TABLE: the table is decided ONCE, here, from
// the run's palette; a caller that supplied its own would be a second capability
// decision and could disagree with the tool rows beside it.
func (in *chatInput) VoiceNoticeStyled(text string) {
	if in == nil {
		return
	}
	// THE CUT IS THE HISTORIC ONE. Before this change a `/voice` row went through
	// WriteNotice → Editor.WriteLines → region.writeBandLine, i.e. a HARD column
	// cut with no continuation marker; the note path instead breaks at a word or
	// a path separator and opens the continuation with `↳ ` (cutNoteRow). That
	// difference is a LAYOUT change on top of the emphasis, and the requirement
	// is emphasis ONLY — measured 2026-10-22 (Rule 31): four voice PTY cases went
	// red because the flattened row had gained markers and lost break spaces.
	// So a voice row is cut hard and carries no marker, exactly as the panel
	// path cut it, and the emphasis is the one thing this change adds.
	in.WriteNoticeRuns(termNoteRows(text, in.tool), in.errWriter(), true)
}

// VoiceEditor returns the editor the voice mode installs its key gate on, or nil
// on a carrier with no editor. It is the mode's ONE dependency check: a nil here
// is what makes `/voice` refuse to start instead of recording into nothing.
func (in *chatInput) VoiceEditor() *lineedit.Editor {
	if in == nil {
		return nil
	}
	return in.editor
}

// VoiceDraft publishes the LIVE dictation arm's draft segment: the text a
// streaming recogniser has produced SO FAR, which the editing line draws after
// the operator's own text and REPLACES as the recogniser refines it
// (internal/lineedit/draft.go).
//
// IT IS THE SAME ROAD VoiceInsertLine takes and the same discipline applies: the
// text is QUEUED by the producer and applied by the read loop, so the arm never
// becomes a second writer of the editing model. The difference is only WHAT is
// queued — a draft the operator does not own yet, versus text that is theirs.
//
// A FALSE IS THE HONEST ANSWER for a carrier with no editor: there is no line to
// draw a draft in, and a caller that does not hear that would keep updating a
// segment nobody can see (Rule 15).
func (in *chatInput) VoiceDraft(text string) bool {
	if in == nil || in.editor == nil {
		return false
	}
	return in.editor.SetDraft(text, false)
}

// VoiceSettle hands a draft over as FINAL: the text is moved into the operator's
// own line through the ordinary insert road and the draft segment is cleared in
// the same pass, so the sentence never appears twice (draft.go).
//
// done=true is what distinguishes this from VoiceDraft, and the two travel on the
// same call because they are ONE producer's decision: the recogniser declaring
// its sentence finished is the same event as the text becoming the operator's.
func (in *chatInput) VoiceSettle(text string) bool {
	if in == nil || in.editor == nil {
		return false
	}
	return in.editor.SetDraft(text, true)
}

// VoicePublishStatus wakes the status row's repaint after the voice mode moved
// its phase. It is the SAME publish road every other status change takes —
// PublishStatus is a flag plus a wake, never a terminal write, so the editor's
// read loop stays the only writer on the fd (see that method's note) — and it is
// exposed to the mode rather than reached around it because the mode is built
// over the voiceChatSession seam, not over *chatInput.
//
// A nil receiver is a no-op (the cooked path builds no status source and has no
// row to repaint), which is the same "nothing to do, and nothing broken"
// contract VoiceNotice keeps.
func (in *chatInput) VoicePublishStatus() {
	if in == nil {
		return
	}
	in.PublishStatus()
}

// WriteLines streams the caller's own text (the REPL's /help panel, a slash
// command's table) into the answer band, or falls back to the historic flow
// write when no layout is installed.
func (in *chatInput) WriteLines(text string) {
	text = in.masked(text)
	if in.editor != nil && in.editor.WriteLines(text) {
		return
	}
	fmt.Fprint(in.flowWriter(), text)
	// A trailing newline is the flow writer's own business — the historic
	// `fmt.Println(out)` printed one, and the reserved path drops trailing
	// breaks instead. Keeping it here means piped output is unchanged.
	if !strings.HasSuffix(text, "\n") {
		fmt.Fprint(in.flowWriter(), "\n")
	}
}

// WriteNotice prints one of the REPL's OWN one-line notices (the `/new`
// confirmation, the `/tools` toggle) so it is READABLE in the reserved layout.
//
// It is WriteLines' sibling and exists because the two families have different
// historic streams, not different layouts: the /help panel and the slash-command
// tables were `fmt.Println(out)` — stdout, the script-readable stream — while
// these notices were `fmt.Fprint(os.Stderr, …)`, the stream the REPL's own UI
// uses and the one the testsuite greps. A single method would have to pick one
// of the two and would therefore change the piped bytes of the other family.
//
// WHY the reserved path routes it at all: with the layout installed, a flowed
// stderr write lands on the row the editor last drew (the editing row) and the
// editor's own repaint then ERASES it — measured on a fixed checkout with the
// SGR instrument, `/new` printed `[gopi] conversation cleared.` into the raw PTY
// stream and the screen showed a bare `you>`: the user toggled tools and read
// nothing. So the notice takes the answer band, exactly as the panel does.
//
// The cooked/flow path is BYTE-IDENTICAL to the historic stderr write.
//
// IT IS ALSO THE ANNOUNCEMENT SEAM, and the only one: every notice the REPL puts
// on the operator's console goes through this method, and an installed observer
// (announce, WriteNotice's tee) is handed the text as it is announced. That is
// the ONE place a notice is counted as an event; nothing in the editor's repaint
// path reaches it, which is what makes an exact announcement count expressible
// (see the announce field).
func (in *chatInput) WriteNotice(text string) {
	text = in.masked(text)
	if in.announce != nil {
		// Deliberately discarded: the observer is an INSTRUMENT (a counter), and a
		// failing instrument must not change the console it observes — the notice
		// is written below whatever happens here, and a counter that missed a
		// write reports its own shortfall, because the count IS its output.
		_, _ = io.WriteString(in.announce, text) //stylegate:ignore an instrument's write failure must not disturb the notice it observes
	}
	if in.editor != nil && in.editor.WriteLines(text) {
		return
	}
	fmt.Fprint(in.errWriter(), text)
	if !strings.HasSuffix(text, "\n") {
		fmt.Fprint(in.errWriter(), "\n")
	}
}

// WriteNoticeRuns prints one of the REPL's own notices WITH its emphasis: the
// caller hands the rows already partitioned into cells (term_note_row.go), and
// this method decides HOW they reach the operator.
//
// IT IS WriteNotice'S SIBLING, NOT A SECOND ROAD. Under the reserved layout the
// rows are committed to the answer band by the read loop (Editor.NoteRuns), so
// the status row and the editing line are not torn by a foreign write. With no
// layout the caller's cells are drawn as their TEXT and written to the SAME
// stream (errWriter) the historic notice used — which is what keeps a cooked
// run's bytes exactly what they were, and a monochrome run's bytes free of any
// sequence (the cells carry no escape: the SGR is applied by the drawer, and a
// Colour=false table applies none).
//
// It reports whether a layout claimed the rows; false means the text went out on
// the historic stream, which is the same contract WriteToolRow keeps.
func (in *chatInput) WriteNoticeRuns(rows []termRow, flow io.Writer, copied bool) bool {
	if in == nil || len(rows) == 0 {
		return false
	}
	var plain strings.Builder
	runs := make([]lineedit.NoteRun, 0, len(rows))
	for _, row := range rows {
		text := in.masked(row.Text())
		if in.announce != nil {
			// Deliberately discarded: the observer is an INSTRUMENT (a counter), and a
			// failing instrument must not change the console it observes.
			_, _ = io.WriteString(in.announce, text) //stylegate:ignore an instrument's write failure must not disturb the notice it observes
		}
		plain.WriteString(text)
		plain.WriteString("\n")
		if !row.Colour {
			// A monochrome table: every cell is drawn raw, so the run list carries no
			// style and the band emits no sequence (the same zero-byte property
			// termRow.Row keeps).
			runs = append(runs, lineedit.NoteRun{Text: text + "\n"})
			continue
		}
		for _, c := range row.Render {
			if c.Text == "" {
				continue
			}
			runs = append(runs, lineedit.NoteRun{Text: in.masked(c.Text), Style: lineedit.StyleFromSGR(c.Style)})
		}
		// The row boundary is the band's own (one call, one row), so the run list
		// keeps a newline between rows and splitNoteRuns cuts on it.
		if len(runs) > 0 {
			last := &runs[len(runs)-1]
			last.Text += "\n"
		}
	}
	if in.editor != nil {
		// ONE commit per row: the copyable form and the ordinary form are two
		// ENTRY POINTS onto the same queue, so exactly one of them may be called —
		// calling both (the first version did) queues every row twice.
		if copied {
			// A block whose LINES the operator selects and pastes (the /info block) is
			// committed without the wrapped row's continuation marker: the glyph would
			// otherwise land inside the cells a selection carries and the paste would
			// not run — the same rule answerRow.copied keeps for the turn gutter.
			if in.editor.NoteRunsCopied(runs) {
				return true
			}
		} else if in.editor.NoteRuns(runs) {
			return true
		}
	}
	// THE FLOW PATH KEEPS ITS OWN STREAM, per CALLER, and that is not a
	// convenience: the two families this method serves have HISTORICALLY
	// different destinations — `/info`'s block was `fmt.Println` (stdout, the
	// script-readable stream) while a `/voice` notice was
	// `fmt.Fprint(os.Stderr, …)` (the stream the REPL's UI and the testsuite
	// grep). Collapsing them into one would change the piped bytes of the other
	// family, which is the one thing this change may not do; a caller therefore
	// names the writer its own historic path used (flowWriter / errWriter).
	if flow == nil {
		flow = in.errWriter()
	}
	fmt.Fprint(flow, plain.String())
	return false
}

// WriteToolRow commits ONE durable tool-activity row into the answer band (the
// "┊ 💻 $ …" line the reference TUI keeps in its transcript), so the record of
// what the agent DID survives the turn that did it.
//
// WHY IT IS NOT THE STATUS ROW. The REPL already reports a running tool on the
// status line, and that row is CLEARED when the turn ends (cmd/agent's
// clearTool) — so the transcript keeps no record that a tool ever ran, and a
// reader cannot tell which command produced the answer. The status row answers
// "what is happening NOW"; this answers "what happened". Both are drawn from the
// SAME palette fields and the same line text, so the two surfaces cannot
// describe one call differently.
//
// It reports false when no reserved layout owns the screen: the caller then
// keeps the historic bytes (the flow path's own writer), because the cooked
// path's byte stream IS its contract and a band row would be a new byte in it.
func (in *chatInput) WriteToolRow(line string) bool {
	if in.editor == nil {
		return false
	}
	return in.editor.Note(line, in.pal.ToolCall)
}

// WriteToolRowRuns commits ONE tool-activity row whose parts carry DIFFERENT
// emphasis — the shape cmd/agent's termToolRow produces (a marker, a tool name
// and the command it marks are three roles, not one line under one style).
//
// It is the same road WriteToolRow takes (the deliverable is a ROW either way):
// queued on the answer renderer, committed by the read loop, reporting false
// when no reserved layout owns the screen. What changes is only where the
// emphasis comes from, and the caller converts its cell styles ONCE at this
// seam (lineedit.StyleFromSGR) so the band never parses an SGR sequence.
//
// The runs' visible text is what the row shows; the caller keeps the COMMAND as
// its own value (termRow.CopyText), which is what makes the row's payload
// copyable whatever the renderer did to the marker column.
func (in *chatInput) WriteToolRowRuns(runs []lineedit.NoteRun) bool {
	if in.editor == nil {
		return false
	}
	return in.editor.NoteRuns(runs)
}

// masked applies this run's redaction pass to a row the SURFACE composes itself
// (chat_replay_redact.go). It exists because not every identifying run arrives as
// recorded content: a notice is composed at draw time and can carry this run's
// own session directory — the `chat_surface` arming line names the identity this
// run publishes to its children, and the notice family is written straight to the
// band. A filmed run must mask those too, and the film's checker greps the dumped
// grids, which is where a notice lands.
//
// It is nil-safe and is a NO-OP for a run with no beat protocol: a plain
// `gopi chat` has no pass, and its bytes are unchanged.
func (in *chatInput) masked(text string) string {
	if in == nil || in.redact == nil {
		return text
	}
	return in.redact(text)
}

// errWriter is the stream the REPL's own notices go to: os.Stderr, the
// destination the historic `fmt.Fprint(os.Stderr, …)` used and the stream the
// testsuite greps for those messages. Production always assigns it; a bare
// chatInput in a test falls back to the same place.
func (in *chatInput) errWriter() io.Writer {
	if in.errOut != nil {
		return in.errOut
	}
	return os.Stderr
}

// flowWriter is the stream the flow path writes to: stdout, the destination the
// historic `fmt.Println` used, so piping `gopi chat` keeps capturing the help
// text exactly as before.
func (in *chatInput) flowWriter() io.Writer {
	if in.flowOut != nil {
		return in.flowOut
	}
	return os.Stdout
}

// The AGENT's own flow output (the live reasoning stream, the `[gopi]` notices)
// reaches the layout through these two methods. WHY it needs its own entry
// point: it was never the REPL's own text. The REPL's panel and notices are
// composed as whole lines by this process (WriteLines / WriteNotice); the
// agent's flow output arrives from another interface — `internal/agent` writes
// it to destinations of its own (os.Stderr and a log.Logger over it), one Fprint
// per chunk, with no idea what a band row is. Letting it reach the terminal
// directly is what the epic measured: the reasoning block landed on the band's
// own rows and the `[gopi] ℹ️ thinking budget …` notice overwrote the STATUS row
// mid-line.
//
// bandActive is the ONE definition of "does the reserved layout own the screen"
// (Rule 14): the seam's two destinations branch on it, so they cannot disagree
// about which one is in charge.
func (in *chatInput) bandActive() bool {
	return in.editor != nil && in.editor.BandActive()
}

// writeBandRow hands one already-split row of the agent's flow output to the
// editor, which draws it on the band and repaints the chrome (the editing line
// and the status row keep their own rows — the measured defect was a notice
// landing on the status row mid-line). It reports whether the layout claimed the
// row; false means the caller writes the historic bytes, so no row is ever
// dropped on a layout that was taken down mid-turn.
func (in *chatInput) writeBandRow(row string) bool {
	return in.editor != nil && in.editor.WriteBandRow(row)
}

// bandGeometry is the reserved layout's own geometry — the width the band
// draws at and the screen's height — which is what the startup screen must fit
// (it is committed INTO the band). It returns zeros when no layout owns the
// screen: the caller then stops rather than drawing a box at a geometry the
// band never used.
func (in *chatInput) bandGeometry() (cols, rows int) {
	if in.editor == nil {
		return 0, 0
	}
	cols, rows, ok := in.editor.LayoutGeometry()
	if !ok {
		return 0, 0
	}
	return cols, rows
}

// bandCols is the band's drawing width in cells (0 when no layout is installed).
// It is the width the seam wraps an over-wide flow row to BEFORE handing it
// over: the split column is the producer's decision, and the band's own row
// loop is not a second wrapper (Rule 14 — one wrap policy, one definition).
func (in *chatInput) bandCols() int {
	if in.editor == nil {
		return 0
	}
	return in.editor.BandCols()
}

// Close releases the input layer's reserved layout and background state.
func (in *chatInput) Close() {
	if in.editor != nil {
		in.editor.Close()
	}
}

// newChatInput builds the line source for one chat run.
//
// The editor is used only when stdin is a terminal AND the caller did not ask
// for the cooked path (--no-editor). Everything else — including a terminal
// whose termios cannot be read — degrades to the cooked reader, which is also
// what the tests and the piped usage exercise.
//
// The returned cleanup restores the terminal; it is a no-op in cooked mode. A
// failure to ENTER raw mode is not an error worth aborting a chat session for,
// so it degrades and says so on stderr.
func newChatInput(in *os.File, out io.Writer, prompt string, noEditor bool, noColor bool) (*chatInput, func()) {
	return newChatInputWithCompleter(in, out, prompt, noEditor, noColor, NewSlashCompleter())
}

// newChatInputWithCompleter is newChatInput with the completion source injected,
// so the A7 menu can be driven by a stub (tests) or by the real slash-command +
// file completer (production).
func newChatInputWithCompleter(in *os.File, out io.Writer, prompt string, noEditor bool, noColor bool, completer lineedit.Completer) (*chatInput, func()) {
	return newChatInputWithDumper(in, out, prompt, noEditor, noColor, completer, nil)
}

// newChatInputWithDumper is newChatInputWithCompleter plus the screen-dump sink:
// when one is given, the editor's PAINT STREAM is teed into it (the editor
// writes the reserved layout's bytes to out, and the sink interprets the same
// bytes beside them). A nil sink returns the editor's writer UNCHANGED, so the
// no-flag run is byte-identical by construction rather than by review.
//
// WHY THE TEE IS HERE, at the editor's own writer, and not at the answer
// renderer: the editor's read loop is the layout's ONLY writer (the band, the
// editing line, the status row and the menu all arrive through it), so this is
// the one place that sees the screen the operator actually gets — and it needs
// no second render path, no hook and no new layer (Rule 3).
func newChatInputWithDumper(in *os.File, out io.Writer, prompt string, noEditor bool, noColor bool, completer lineedit.Completer, dumper *uiclient.ScreenDumper) (*chatInput, func()) {
	return newChatInputWithPalette(in, out, prompt, noEditor, completer, dumper, lineedit.CapabilityForEnv(noColor, os.Stderr))
}

// newChatInputWithPalette is newChatInputWithDumper with the emphasis table
// SUPPLIED instead of resolved here. The distinction is not cosmetic: resolving
// per call site means TWO resolutions inside one `gopi chat` run (the banner
// resolves once, this builder resolves again), and a capability that decides
// anything on stderr — the palette downgrade notice does — then says the same
// thing twice, on a terminal whose reader is reading it. The caller resolves
// ONCE and hands the table down (see runChatWithInput), so the run has one
// decision, one notice, and no second place that could colour a byte after the
// decision said no.
//
// THE STAGES OF THAT ONE DECISION, and where its ONE notice is said. This
// builder is not a step of the decision; it is where the decision FINISHES. It
// resolves the environment's declaration first (the table every surface draws
// with until the terminal answers), asks the terminal, and re-derives the table
// from the answer — and the run's sentence belongs to the LAST of those, not to
// each. That is why both resolutions take the NOTICED form
// (lineedit.PaletteForCapabilityNoticed): the declaration's sentence is HELD,
// discarded the moment the terminal overrules its table (a sentence about a
// table no pixel was drawn with is a lie), and what survives is written exactly
// once, at this function's single exit — before the first paint, so the layout
// cannot write over it.
//
// Measured 2026-10-04 (task 01a10e2b): the announcing form at each of these
// three sites printed ONE decision's notice three times into the operator's
// terminal (cli_chat.go's banner resolution, this builder's declaration, this
// builder's measurement), and the run's own screen carried palette256 — i.e.
// two of the three sentences described a table the run did not draw.
func newChatInputWithPalette(in *os.File, out io.Writer, prompt string, noEditor bool, completer lineedit.Completer, dumper *uiclient.ScreenDumper, declared lineedit.Capability) (*chatInput, func()) {
	// The capability the environment DECLARED is the caller's (`declared`); the
	// terminal's own answer is folded into it below, so "measurement first,
	// declaration as the fallback" is decided in ONE place
	// (lineedit.Capability.WithMeasurement) rather than here. Until a terminal
	// answers, the declared table is what everything draws with — and the
	// sentence that resolution owes is HELD, not said, because the next stage
	// may overrule the table it is about.
	pal, notice := lineedit.PaletteForCapabilityNoticed(declared)
	// noLayout is WHY this run will have no editor-backed layout, resolved
	// HERE because this function is the one place that decides it. Each arm
	// below sets the reason it met, and the reason rides every input layer it
	// returns — so a consumer built on the layout (the replay driver) can name
	// the cause instead of listing the possibilities (Rule 15). It stays "" on
	// a run whose layout really is installed.
	noLayout := chatNoEditorReasonNoEditor
	if !noEditor {
		noLayout = chatNoEditorReasonPipedStdin
	}
	// The paint stream the editor writes to: the caller's stream, or that stream
	// teed into the dump sink (a nil sink = the SAME writer, not a wrapper).
	paint := chatDumpWriter(out, dumper)
	// The sink's geometry binding: nil when no dump was asked for, otherwise the
	// function that hands the sink the geometry the LAYOUT installs (and the
	// fallback fact when no layout exists).
	geomObs := chatGeomObserver(dumper)
	if !noEditor && lineedit.IsTerminal(in) {
		term, err := lineedit.Open(in, paint)
		if err == nil {
			// A TERMINAL DID OPEN: whatever the arm below decides, the
			// "no terminal / no editor" reasons are no longer true, so the
			// reason starts empty and only the layout's own refusal sets it.
			noLayout = ""
			// THE MEASUREMENT, and it is taken HERE — after the terminal is open
			// and BEFORE anything is painted, so no query byte can land on a
			// screen the reader is already reading. `declared` was resolved from
			// the environment by the caller; the terminal's own answers now get
			// their say, and the table is re-derived when they disagree with the
			// declaration.
			//
			// THE ASKING IS NOT WRITTEN HERE. lineedit owns the one place that
			// knows "a terminal is open, so ask it" —
			// lineedit.CapabilityForOpenTerminal — and this call site is now its
			// ONLY caller: the second one was the self-drawn replay renderer's
			// own copy, abolished with the `replay` verb on 2026-10-03. A
			// probe written here would be a second copy of the sequence, the
			// gate on the colour switch and the fold's order — which is exactly
			// how the replay path came to classify a whole film without ever
			// asking the terminal it was drawing on (the measured defect
			// CapabilityForOpenTerminal's comment records).
			measured, measurement := lineedit.CapabilityForOpenTerminal(declared, term)
			if notice := lineedit.CapabilityNotice(measured); notice != "" {
				// A DISAGREEMENT between the terminal and the environment is
				// ANNOUNCED (Rule 15): it changes which table is drawn, and the
				// operator is the one who can fix the cause. It is printed
				// BEFORE the chosen table is installed, so the sentence is never
				// written over by the layout's first paint.
				// It rides the DIAGNOSTIC stream (chat_stream_split.go): the
				// layout's own stream is the one a filmer photographs, and a
				// sentence about the colour decision is not part of the screen.
				fmt.Fprint(chatDiagnosticStream(), notice)
			}
			// The table is re-derived from the answer, and so is the sentence:
			// the measurement IS the run's decision, so the held declaration
			// notice is replaced by whatever this resolution owes (often
			// nothing at all — the terminal that answered truecolor gained the
			// table the operator asked for).
			mPal, mNotice := lineedit.PaletteForCapabilityNoticed(measured)
			pal, notice = mPal, mNotice
			// THE RUN'S ONE SENTENCE, said once, BEFORE the table is installed
			// and before the layout's first paint — so the operator reads the
			// reason for the screen they are about to get, and no frame writes
			// over it. It rides the DIAGNOSTIC stream (chat_stream_split.go).
			writePaletteNotice(notice)
			unsubscribe := term.CaptureSignals(os.Interrupt, os.Interrupt)
			// THE WINDOW BETWEEN THE TWO REGISTRATIONS IS ITSELF A DEFECT, so it
			// is closed HERE rather than left to SetInterrupt (which the REPL
			// calls a few hundred lines later, after the banner, the geometry,
			// the dump and the history load).
			//
			// CaptureSignals registers SIGINT WITH THE FATAL PATH: with no
			// target installed (`sigTarget` nil) a signal means "restore the
			// terminal, signal.Reset, re-raise" — the process dies on the
			// default disposition and writes no session_end. Measured on this
			// branch: a SIGINT delivered within a few milliseconds of the
			// banner exiting produced `exit_code: -1` and ZERO session_end,
			// i.e. exactly the session-b9936006 shape this epic exists to
			// remove, just from a narrower window.
			//
			// The provisional target CLAIMS unconditionally, and claiming is
			// safe because the REPL's own watcher is ALREADY registered
			// (cli_chat.go's signal.Notify over chatGracefulSignals runs before
			// the input layer is built — this function is called from inside
			// it): Go relays one delivery to every registered channel, so the
			// watcher closes interruptCh whatever this seam answers. What the
			// claim buys is that lineedit does not kill the process before the
			// REPL can reach its close-out. SetInterrupt REPLACES this with the
			// turn-aware handler once the REPL can answer "is a turn in flight".
			term.SetInterruptHandler(func() bool { return true })
			// A REPLAY LOADS NO HISTORY AND WRITES NONE (internal/replaymode):
			// chatHistoryPath answers "" for the run, and the fact is ANNOUNCED
			// here rather than left to be inferred from a file that did not grow
			// (Rule 15 — the operator's ↑ list is exactly what this protects, so
			// the run says it refused it).
			if replaymode.Active() {
				replaymode.Noticef("%s\n", replaymode.Refuse("append the recorded prompts to "+chatHistoryFileName))
			}
			hist := lineedit.LoadHistory(chatHistoryPath(), 0)
			// KEY INJECTION is installed HERE, in front of the terminal and
			// behind the editor: the Source decorator is the one position from
			// which raw bytes can be handed to the editor as a keystroke, and it
			// is a decorator precisely so the editor's decoder — the thing that
			// decides what a byte means — is unchanged. The ceiling comes from
			// chatReplayInjectCeiling(), ONE reader over the two quantities this
			// process may charge a keystroke against: the self-driven loop's
			// declared bound (chat_surface's, unchanged) or — when `--replay`
			// named a recording — the budget derived from that recording's own
			// length (chat_replay_budget.go). Resolving it here rather than at
			// each caller keeps ONE definition of the number that is charged and
			// the number that refuses.
			inj := lineedit.NewInjector(term, chatReplayInjectCeiling())
			ed := lineedit.NewChatEditor(inj, paint, prompt, hist, os.Stderr, completer)
			ed.SetPalette(pal)
			// The dump's grid follows the LAYOUT, not a second probe: the
			// observer is told the region's geometry the instant it is installed
			// (and again on every resize the layout follows), so `grid=` in the
			// artifact and the frame the terminal received are the same geometry
			// by construction. On a terminal too small for the reserved band
			// there IS no layout geometry, and the observer reports the fallback
			// instead of a default dressed up as a measurement (Rule 15).
			ed.SetGeometryObserver(geomObs)
			// The wake seam is the TERMINAL's own: it exposes itself as a
			// lineedit.Notifier (WakeNotifier), and the editor installs whatever
			// its source offers. One seam serves BOTH producers now — the REPL's
			// status publish and the completion worker's candidate list — so a
			// publish can INTERRUPT the editor's blocked read instead of waiting
			// for the next keystroke. Without it SetStatus's type assertion finds
			// no Notifier, the editor keeps its no-op sink, and StatusNotifyEnabled
			// reports the degradation so the REPL says so (Rule 15).
			cleanup := func() {
				ed.Close()
				unsubscribe()
				if err := term.Close(); err != nil {
					fmt.Fprintf(os.Stderr, "[gopi] editor: %v\n", err)
				}
			}
			// THE IDEAL GEOMETRY IS APPLIED HERE — after the editor exists (it
			// carries the canvas) and before the layout installs, which is the one
			// window where both are true: the canvas must be in force when
			// EnableRegion reads its width, and a sentence about a degraded
			// geometry must reach the operator before the layout takes the rows
			// it is written on. ONE resolution of the knob
			// (config.ChatIdealGeometry), read by this surface and by the
			// replay/beat filmer alike.
			chatApplyIdealGeometry(ed, term, chatDiagnosticStream())
			// THE LAYOUT'S OWN VERDICT IS TAKEN BEFORE THE LAYER IS BUILT, and
			// that order is the whole reason this arm is correct: `ci` below
			// captures noLayoutReason BY VALUE, so a refusal reported after the
			// struct exists would leave the layer claiming a layout it does not
			// have — measured on a real run (`--no-editor` reported "records no
			// cause"; a too-small terminal would have reported the same). One
			// decision, taken once, then carried.
			regionOK := ed.EnableRegion()
			if !regionOK {
				// Rule 15: the layout change is ANNOUNCED, never silent — the
				// user gets the old full-screen behaviour and is told why.
				fmt.Fprintf(chatDiagnosticStream(), "[gopi] terminal too small for the chat layout (%d rows) — the answer will scroll normally and there is no status line.\n", chatRegionMinRows())
				noLayout = chatNoEditorReasonTooSmall
				// …and the DUMP's geometry is announced with the same voice: on
				// this path there is no layout geometry, so a grid exists at the
				// default size only, and a reader must never mistake it for the
				// layout's.
				fmt.Fprintf(chatDiagnosticStream(), chatDumpGeomNotice, chatDumpNoLayoutReason, "the dump")
			}
			ci := &chatInput{editor: ed, term: term, errOut: os.Stderr, flowOut: os.Stdout, dumper: dumper, pal: pal, tool: termToolStylesFromPalette(pal), cap: measured, measured: measurement, injector: inj, noLayoutReason: noLayout}
			// THE CONSUMER'S VIEW OF "IS A LAYOUT INSTALLED" IS THE EDITOR'S OWN
			// VERDICT, never a second reading of noLayoutReason: bandActive()
			// answers it, chatInputNoLayoutReason explains it when it is false.
			// The two must agree, and the order above is what makes them.
			return ci, cleanup
		}
		fmt.Fprintf(os.Stderr, "[gopi] editor unavailable (%v) — falling back to plain line input.\n", err)
		noLayout = chatNoEditorReasonTerminalRefused
	}
	// The cooked path has no reserved layout and no paint stream to observe: the
	// sink stays at frames=0, and the record says so. The flag is honoured as far
	// as the surface allows, and the degradation is ANNOUNCED rather than
	// silently leaving a blank grid a reader would misread as "the screen was
	// empty".
	if dumper != nil {
		fmt.Fprint(os.Stderr, chatDumpUnpaintedNotice)
	}
	// THE COOKED PATH'S ONE SENTENCE. There was no terminal to ask, so the
	// declaration's resolution IS this run's decision and the notice it held is
	// owed to the operator (a `gopi chat` under --no-editor, or on a stdin that
	// is not a terminal, still prints the reason its screen is the table it is).
	writePaletteNotice(notice)
	return &chatInput{reader: bufio.NewReader(in), errOut: os.Stderr, flowOut: os.Stdout, noLayoutReason: noLayout}, func() {}
}

// writePaletteNotice says the run's downgrade sentence on the DIAGNOSTIC stream,
// and says nothing when there is none. It is a function rather than an inline
// Fprint because the empty case is an ASSERTION — most runs owe no sentence —
// and a call site that forgot it would either print an empty line or silently
// drop a Rule 15 notice; here both spellings go through one branch.
func writePaletteNotice(notice string) {
	if notice == "" {
		return
	}
	fmt.Fprint(chatDiagnosticStream(), notice)
}

// chatRegionMinRows reports the terminal height below which the reserved layout
// is not installed (the value lives in internal/lineedit so the layout and its
// documentation cannot drift).
func chatRegionMinRows() int { return lineedit.RowsMinUsable() }

// newChatInputCooked builds the COOKED input layer over an arbitrary reader,
// with the flow path pinned to the caller's stream. It is the seam the one-shot
// face (`gopi chat --once`, chat_once.go) drives: that face must read its
// prompt-and-then-EOF from a buffer rather than a file, and must observe the
// answer while the REPL writes it, neither of which `newChatInput`'s
// *os.File + stderr signature can express. It is the SAME cooked layer the
// piped `gopi chat` builds (no editor, stderr for the REPL's own notices) — one
// constructor, so the bytes cannot differ.
//
// The returned cleanup is a no-op: the cooked path holds no terminal state. It
// exists so a caller can `defer cleanup()` without branching on the mode.
func newChatInputCooked(in io.Reader, flowOut io.Writer) (*chatInput, func()) {
	return &chatInput{reader: bufio.NewReader(in), errOut: os.Stderr, flowOut: flowOut}, func() {}
}

// newChatInputWithSource builds an editor-backed input over an arbitrary
// lineedit.Source. It is the seam the REPL's tests drive: the paste-burst
// decision (a newline inside a paste is content, never a submit) is then
// asserted end to end without needing a controlling terminal, while the real
// terminal path is the same constructor minus the source override.
//
// IT WRAPS THE SOURCE IN THE KEY INJECTOR, exactly as the production path does:
// key injection is installed where the editor's own Source is, so a test that
// drives this constructor is driving the SAME path a `gopi chat` run has — the
// editor's decoder, with an injector in front of whatever source it reads. A
// constructor that skipped the wrapper would make the injector a production-only
// seam, which is precisely the "unreachable in the shape they call it" defect
// Rule 31 exists to catch.
// Palette returns the emphasis table this input layer will actually draw with —
// the terminal's measured decision when one was made, the declared one
// otherwise. The REPL reads it AFTER construction for the surfaces it owns
// itself (the banner, the band's tool rows), so the whole run draws from ONE
// table even when the probe overruled the declaration.
func (ci *chatInput) Palette() lineedit.Palette {
	if ci == nil {
		return lineedit.MonoPalette
	}
	return ci.pal
}

// Capability returns the capability this input layer decided with (declaration
// plus measurement), for a reporter that must say WHICH evidence chose the
// colours.
func (ci *chatInput) Capability() lineedit.Capability {
	if ci == nil {
		return lineedit.Capability{}
	}
	return ci.cap
}

// Measurement returns the terminal's own answer as this run read it (the zero
// value when no probe ran — a cooked path, a non-terminal, --no-editor).
func (ci *chatInput) Measurement() lineedit.CapabilityMeasurement {
	if ci == nil {
		return lineedit.CapabilityMeasurement{}
	}
	return ci.measured
}

func newChatInputWithSource(src lineedit.Source, out io.Writer, prompt string) *chatInput {
	inj := lineedit.NewInjector(src, chatReplayInjectCeiling())
	return &chatInput{
		editor:   lineedit.NewChatEditor(inj, out, prompt, lineedit.NewHistory(0, ""), os.Stderr, nil),
		errOut:   os.Stderr,
		injector: inj,
	}
}

// newChatInputWithTerminal builds an editor-backed input over an ALREADY-OPENED
// raw-mode terminal, keeping the terminal handle on the input layer.
//
// It is the same construction `newChatInputWithPalette` performs, minus the
// pieces that constructor owns for a RUN (the palette, the dump sink, the
// signal subscription, the teardown), and it exists for the same reason
// newChatInputWithSource does: a caller that holds the terminal — a case
// measuring what the REPL says about the keyboard negotiation, or any future
// caller that opens its own — must be able to build the input layer OVER that
// terminal instead of a lookalike. The terminal is what carries the
// negotiation's verdict (Terminal.KeyboardProtocol), so a seam that dropped it
// would make the verdict unreadable from the input layer.
func newChatInputWithTerminal(term *lineedit.Terminal, out io.Writer, prompt string) *chatInput {
	inj := lineedit.NewInjector(term, chatReplayInjectCeiling())
	return &chatInput{
		editor:   lineedit.NewChatEditor(inj, out, prompt, lineedit.NewHistory(0, ""), os.Stderr, nil),
		term:     term,
		errOut:   os.Stderr,
		injector: inj,
	}
}

// chatHistoryPath is the per-home history file the editor recalls from and
// appends to. A home that cannot be resolved yields "" — the editor then keeps
// its history in memory for the session instead of guessing a location (and
// LoadHistory treats an empty path as memory-only).
//
// A REPLAY GETS "" (internal/replaymode — measured 2026-10-12). The history file
// is the operator's OWN recall list under the profile root, and a replay TYPES
// the recording's prompts into the real editor — every one of which the editor
// then records as if the operator had typed it. The measured run appended 3840
// bytes of a week-old conversation to `~/.gopi/chat_history`, so the operator's
// next ↑ would offer somebody else's prompts. "" is the answer the editor
// already documents for "which history file?": history lives in memory for this
// run and nothing is written.
//
// WHY THE EMPTY STRING AND NOT A SCRATCH PATH: a replay's history is never read
// back — the recording supplies every line this run types — so a file for it
// would be an artifact with no reader (Rule 35) sitting in a scratch tree that
// nothing sweeps for. The editor's memory-only mode is the existing behaviour
// for exactly this shape, not a new one.
func chatHistoryPath() string {
	if replaymode.Active() {
		return ""
	}
	dir, err := config.GopiDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, chatHistoryFileName)
}

// Slash / file completion (A7). The menu is opened by typing "/" at the start
// of a line (slash commands) or "@" anywhere (file paths, relative to the
// working directory), and its candidates are computed by this completer — which
// the editor calls on its WORKER goroutine, never on the read/redraw path.
const (
	// completionSlashPrefix opens the slash-command menu.
	completionSlashPrefix = "/"
	// completionFilePrefix opens the file-path menu.
	completionFilePrefix = "@"
	// completionFileScanMax bounds one directory scan. A directory with more
	// entries than this still completes (the list is capped), but the scan
	// never walks the whole tree.
	completionFileScanMax = 200
	// completionDirSuffix marks a candidate that is itself a directory.
	completionDirSuffix = string(os.PathSeparator)
	// completionHiddenPrefix hides dot-files from the menu unless the user
	// typed the dot (the same convention as a shell's glob).
	completionHiddenPrefix = "."
)

// NewSlashCompleter builds the production completer: slash commands from the
// shared command table (cmd/agent/commands.go — the SAME registry the REPL
// dispatches through, so the menu can never offer a command that does not
// exist), and file paths after "@".
func NewSlashCompleter() lineedit.Completer {
	return func(line string, cursor, limit int) []lineedit.Candidate {
		word := currentWord(line, cursor)
		switch {
		case strings.HasPrefix(word, completionSlashPrefix):
			return slashCandidates(word, limit)
		case strings.HasPrefix(word, completionFilePrefix):
			return fileCandidates(strings.TrimPrefix(word, completionFilePrefix), limit)
		}
		return nil
	}
}

// slashCandidates lists the command table's words matching prefix. The table is
// the single source of truth (unknown commands are impossible by construction);
// the REPL-native session commands are added explicitly because they mutate the
// session rather than the registry.
func slashCandidates(prefix string, limit int) []lineedit.Candidate {
	seen := map[string]bool{}
	var out []lineedit.Candidate
	add := func(name, hint string) {
		if seen[name] {
			return
		}
		seen[name] = true
		out = append(out, lineedit.Candidate{Text: name, Hint: hint})
	}
	for _, c := range tableCandidates() {
		add(c.Text, c.Hint)
	}
	for _, c := range sessionCandidates() {
		add(c.Text, c.Hint)
	}
	matches := make([]lineedit.Candidate, 0, len(out))
	for _, c := range out {
		if strings.HasPrefix(c.Text, prefix) {
			matches = append(matches, c)
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		return len(matches[i].Text) < len(matches[j].Text)
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}
	return matches
}

// chatSessionHint is the menu's fallback label for a REPL-native session command
// that has no shared-table entry and no alias.
const chatSessionHint = "session"

// chatSessionHints describes /quit to the MENU. It is deliberately not the
// usage block's first line ("Usage: /quit") — a menu row reads as
// `/quit (session)`, a category, and the operator's question was what this
// command IS relative to /exit. The alias note below answers that; the hint
// says what the command does. /help quit remains the authority for the usage
// text, and candidateHint still prefers the registry's own summary for every
// word the registry owns.
var chatSessionHints = map[string]string{
	quitCommandName:        "end the session and exit",
	toolsCommandName:       "toggle tools on/off",
	helpCommandName:        "help for one command",
	chatQueueCommandName:   "queue a prompt for the next turn",
	chatInfoCommandName:    "this session's id, paths and mode",
	chatScreenCommandName:  "capture the screen as a file for the next turn",
	chatContextCommandName: "export the context the model received, to a file",
}

// chatSessionAliasNote describes a spelling that is the SAME behaviour as another
// word. It is the answer to the operator's own question about `/quit` and
// `/exit` (attachment tmp/ui_exit.txt — "what is the difference?"): none, they
// are one command, and the menu must say so ON the row instead of offering two
// rows that read as two actions.
func chatSessionAliasNote(other string) string {
	return chatAliasNotePrefix + completionSlashPrefix + other
}

// chatAliasNotePrefix is the vocabulary the alias note is written with. Named so
// the menu row and the test that asserts it cannot spell the note differently.
const chatAliasNotePrefix = "alias of "

// chatSessionAliases maps each spelling of a one-behaviour-two-spellings pair to
// its other spelling, in BOTH directions: the pair is symmetric in the code (no
// canonical form is declared for quit/exit, and treating one as primary would be
// a claim the dispatcher does not make), so the note is symmetric too.
var chatSessionAliases = map[string]string{
	quitCommandName:     exitCommandName,
	exitCommandName:     quitCommandName,
	webClearAliasName:   webClearCommandName,
	webClearCommandName: webClearAliasName,
}

// tableCandidates is the menu's view of the shared command table: one candidate
// per registry word, hinted with that command's own one-line summary, so the
// menu and /help say the same thing about a command.
func tableCandidates() []lineedit.Candidate {
	out := make([]lineedit.Candidate, 0, len(slashCommands))
	for _, name := range slashCommandNames() {
		out = append(out, candidateFor(name))
	}
	return out
}

// sessionCandidates is the menu's view of the REPL's own session commands — the
// same words the turn loop switches on (its switch remains the dispatcher).
func sessionCandidates() []lineedit.Candidate {
	out := make([]lineedit.Candidate, 0, len(chatSessionWords))
	for _, name := range chatSessionWords {
		out = append(out, candidateFor(name))
	}
	return out
}

// candidateFor builds one menu row for a command word. The alias note has
// precedence over every hint: on the row for `/quit`, "alias of: /exit" is the
// fact the user needs, and a category or a usage line would be printed beside it
// rather than instead of it.
func candidateFor(name string) lineedit.Candidate {
	c := lineedit.Candidate{Text: completionSlashPrefix + name, Hint: candidateHint(name)}
	if alias, ok := chatSessionAliases[name]; ok {
		c.Hint = chatSessionAliasNote(alias)
	}
	return c
}

// candidateHint is the one-line hint for a menu row: the shared table's own
// summary where the registry owns the word, and the session family's own label
// for the REPL-native commands the registry cannot see.
func candidateHint(name string) string {
	if first := slashCandidateHint(name); first != "" {
		return first
	}
	if own, ok := chatSessionHints[name]; ok {
		return own
	}
	return chatSessionHint
}

// chatSessionWords are the REPL's own session command spellings — canonical
// words AND their aliases — as the menu's definition of that family, in ONE
// place: a candidate cannot be offered that the turn loop's switch does not
// accept. `/new` is deliberately absent: the shared table owns that word (so it
// is already offered, with this family's alias note attached by
// chatSessionAliases), and a second entry would be the duplicate row the alias
// note exists to remove.
//
// `/info` AND `/context` LEFT THIS LIST ON 2026-11-08 (epic 01a1b818-e02f). They
// were REPL-native words only because the table could not see the facts they
// print; the table can now (CommandContext.Session, commands_session.go), so the
// TABLE owns the words — one owner for the vocabulary, the /help rows and the
// completion hints. The REPL's switch still intercepts both BEFORE the table
// dispatch, and that arm is the CONSOLE SEAM, not a second ownership: it renders
// the same bytes through WriteNoticeRuns so /info's labels draw dim (/new has the
// same shape). TestSlashInfoMatchesTheReplBlock asserts the two doors agree.
//
// The words are REFERENCES, not literals (Rule 13): quit/exit/clear/tools/help
// name the same constants the dispatcher switches on and the commands.go
// registry prints.
var chatSessionWords = []string{quitCommandName, exitCommandName, webClearAliasName, toolsCommandName, helpCommandName, chatQueueCommandName, chatVoiceCommandName, chatScreenCommandName}

// slashCandidateHint uses the command's own one-line summary as the menu hint —
// the help table and the menu therefore say the same thing about a command.
func slashCandidateHint(name string) string {
	c, ok := slashCommands[name]
	if !ok {
		return ""
	}
	return strings.SplitN(strings.TrimSpace(c.Usage), "\n", 2)[0]
}

// fileCandidates lists filesystem entries matching the (possibly partial) path
// argument. The scan is bounded (completionFileScanMax entries) and errors are
// reported as NO candidates rather than a menu — a completion menu is an offer,
// not a report; the user's next keystroke is the retry.
func fileCandidates(arg string, limit int) []lineedit.Candidate {
	dir, base := filepath.Split(arg)
	scanDir := dir
	if scanDir == "" {
		scanDir = "."
	}
	entries, err := os.ReadDir(scanDir)
	if err != nil {
		return nil
	}
	var out []lineedit.Candidate
	for i, e := range entries {
		if i >= completionFileScanMax || len(out) >= limit {
			break
		}
		name := e.Name()
		if strings.HasPrefix(name, completionHiddenPrefix) && !strings.HasPrefix(base, completionHiddenPrefix) {
			continue
		}
		if !strings.HasPrefix(name, base) {
			continue
		}
		// The candidate carries the PREFIX it completes: the menu replaces the
		// whole typed word, so a candidate without "@" would silently delete
		// the marker that opened the file menu (the next keystroke would then
		// match the slash/file rules against a bare path).
		text := completionFilePrefix + dir + name
		hint := ""
		if e.IsDir() {
			text += completionDirSuffix
			hint = "dir"
		}
		out = append(out, lineedit.Candidate{Text: text, Hint: hint})
	}
	return out
}

// currentWord returns the whitespace-delimited word ending at the cursor — the
// span a completion replaces. It mirrors lineedit's own span rule so the menu's
// replacement and the completer's view of the line agree.
func currentWord(line string, cursor int) string {
	runes := []rune(line)
	if cursor > len(runes) {
		cursor = len(runes)
	}
	from := cursor
	for from > 0 {
		r := runes[from-1]
		if r == ' ' || r == '\t' || r == '\n' {
			break
		}
		from--
	}
	return string(runes[from:cursor])
}
