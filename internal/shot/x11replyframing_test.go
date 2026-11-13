package shot

import (
	"encoding/binary"
	"net"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Stray-frame injection: the other half of a real X11 stream.
//
// The fake X server used by the wire-layout tests answers one request with one
// reply. A REAL server interleaves frames the client never asked for: the
// MappingNotify (event type 34) it queues the moment a client fetches the
// keyboard mapping, ordinary input events, events delivered with the
// send-event bit set, and — once a connection has desynchronised — stale
// replies carrying another request's sequence. The reader must survive all of
// them by CLASSIFYING every frame and matching the reply's sequence against the
// request being awaited, never by assuming "the next 32 bytes are my answer".
//
// TODO(map): the production constants used here (x11EventTypeMappingNotify,
// x11MappingModifier/Keyboard/Pointer, x11FakeEventSequence) live in x11.go so
// a test can never disagree with the reader about what an event is.
// ---------------------------------------------------------------------------

const (
	// covStrayFocusWindowID is the window id the fake reports as focused where
	// a test needs a value that can only come from the real reply (the "did the
	// event get mistaken for the answer" check).
	covStrayFocusWindowID = 0x0000face

	// covStrayRequestedSeq is the sequence number the reply-framing tests force
	// onto the request under test by preloading the client's sequence counter
	// (the counter is private; preloading it is the only way to make an
	// out-of-range-sequence frame constructible at all). covStrayWrapSeq is the
	// value that makes the NEXT counter increment wrap to 1 — the boundary case
	// a naive "in-range" test would get wrong.
	covStrayRequestedSeq = 4
	covStrayWrapSeq      = 0xfffe

	// covStrayEventDetail is the detail octet of a synthetic input event
	// (keycode/button). It is irrelevant to the reader — the frame is drained
	// whatever it carries — so it only has to be non-zero and recognisable.
	covStrayEventDetail = 0x24

	// covStrayTypedText is the text the actuator-level tests type: two runes
	// the fake layout actually maps (it advertises XK_n / XK_N only), so the
	// expected XTestFakeInput count is unambiguous — one press and one release
	// per rune.
	covStrayTypedText  = "nn"
	covStrayTypedRunes = 2

	// covStrayStreamBudget bounds how long a framing assertion may take before
	// the test calls it a hang: it must stay far below the client's own read
	// deadline (x11IOTimeout), or a deadline bug would look like a slow test.
	covStrayStreamBudget = 2 * time.Second

	// covStrayResponsiveBarrier proves the connection is HEALTHY before a test
	// arms its stray frames: a round-trip that already succeeded means the
	// failure under test cannot be an unrelated broken connection.
	covStrayResponsiveBarrier = 1
)

// covEventFrame builds a 32-byte X11 event frame of the given type. Events
// carry no reply sequence (that field is x11FakeEventSequence = 0) and no
// payload: the reader reads 32 bytes and moves on — a non-zero length in
// [4:8] is exactly what a stale REPLY has, and is covered separately.
func covEventFrame(eventType, detail byte) []byte {
	frame := make([]byte, x11ReplyHeaderLen)
	frame[0] = eventType
	frame[1] = detail
	binary.LittleEndian.PutUint16(frame[2:4], x11FakeEventSequence)
	return frame
}

// covMappingNotifyFrame builds the MappingNotify frame the server queues for a
// client whose keyboard (or pointer) mapping changed; `request` is the field
// that names WHICH mapping (x11MappingModifier / x11MappingKeyboard /
// x11MappingPointer).
func covMappingNotifyFrame(request byte) []byte {
	return covEventFrame(x11EventTypeMappingNotify, request)
}

// covSendEventMappingNotifyFrame is the same frame delivered with the
// send-event bit set (0x80): the bit is part of the type byte, so a reader
// that compares header[0] to the event code without masking it treats a
// synthetic event as an unknown frame. The new keyboard-map case.
func covSendEventMappingNotifyFrame(request byte) []byte {
	frame := covMappingNotifyFrame(request)
	frame[0] |= x11SendEventMask
	return frame
}

// covStrayFocusReply builds the GetInputFocus reply with the given focus window
// and sequence number — the canned answer of the fake server, with the
// sequence field made explicit so a test can construct a MISMATCHED reply.
func covStrayFocusReply(seq uint16, focus uint32) []byte {
	reply := make([]byte, x11ReplyHeaderLen)
	reply[0] = x11ReplyType
	reply[1] = 0 // revert-to: None (the reply's only other field)
	binary.LittleEndian.PutUint16(reply[2:4], seq)
	binary.LittleEndian.PutUint32(reply[8:12], focus)
	return reply
}

// ---------------------------------------------------------------------------
// Fake-server extensions (stray frames + a mutable keyboard layout).
// ---------------------------------------------------------------------------

// setStrayFrames arms the frames the fake server writes IMMEDIATELY BEFORE its
// own reply to the next request it answers — the interleaving a real server
// produces when an event is queued before the reply is computed. Guarded by the
// server's mutex because the serving goroutine reads it (the race detector does
// not treat socket I/O as a happens-before edge).
func (s *covFakeXServer) setStrayFrames(frames ...[]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stray = frames
}

// strayFrames returns a copy of the armed stray frames.
func (s *covFakeXServer) strayFrames() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([][]byte, len(s.stray))
	for i, f := range s.stray {
		cp := make([]byte, len(f))
		copy(cp, f)
		out[i] = cp
	}
	return out
}

// setFocusReplySeq forces the sequence number the fake writes into its
// GetInputFocus reply, so a test can hand the client a reply that does NOT
// belong to the request it is awaiting.
func (s *covFakeXServer) setFocusReplySeq(seq uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.focusReplySeq = seq
	s.focusReplySeqSet = true
}

// setFocusReplyWindow forces the focused window id the fake reports — a value
// only the real reply can carry, so an assertion on it distinguishes "the
// reply was parsed" from "an event frame was zero-read as the answer".
func (s *covFakeXServer) setFocusReplyWindow(id uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.focusReplyWindow = id
}

// setLayout replaces the keyboard mapping the fake advertises (the behavioural
// half of "the server's mapping changed": after a MappingNotify the client must
// fetch THIS table, not reuse the cached keycodes).
func (s *covFakeXServer) setLayout(layout covFakeLayout) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.layout = layout
}

// ---------------------------------------------------------------------------
// Regression tests: reply framing vs interleaved frames.
// ---------------------------------------------------------------------------

// TestReadReplyDrainsMappingNotifyBeforeFocusReply is the deterministic
// reproduction of the filed defect (TestKnownBugTypeTextMappingNotifyDesync
// depends on the server actually queuing the event; this one guarantees it):
// a MappingNotify frame sits in front of the GetInputFocus reply, and the reply
// must still be delivered — with the honest focus value and without desyncing
// the connection.
func TestReadReplyDrainsMappingNotifyBeforeFocusReply(t *testing.T) {
	s := startCovFakeXServer(t, covUsFakeLayout())
	a := s.openActuator(t)
	s.setFocusReplyWindow(covStrayFocusWindowID)

	// Barrier: one full round-trip on a clean stream, so a later failure can
	// only come from the stray frame.
	if err := a.Click(1); err != nil {
		t.Fatalf("barrier Click on a clean stream: %v", err)
	}
	before := len(s.fakeInputEvents())

	s.setStrayFrames(covMappingNotifyFrame(x11MappingKeyboard))

	if err := a.TypeText(covStrayTypedText); err != nil {
		t.Fatalf("TypeText(%q) with a queued MappingNotify frame: %v — the event must be DRAINED, not returned as the GetInputFocus answer", covStrayTypedText, err)
	}
	if got := len(s.fakeInputEvents()) - before; got != covStrayTypedRunes*2 {
		t.Errorf("%d XTestFakeInput events for %q, want %d (one press + one release per rune) — typing must be unaffected by the drained event", got, covStrayTypedText, covStrayTypedRunes*2)
	}

	// The focus answer is the REAL reply, not the event frame.
	win, err := a.Focus()
	if err != nil {
		t.Fatalf("Focus() after the drained MappingNotify: %v", err)
	}
	if win != covStrayFocusWindowID {
		t.Errorf("Focus() = 0x%x, want 0x%x (bytes 8..11 of the GetInputFocus reply) — the event frame was read as the answer", win, covStrayFocusWindowID)
	}
}

// TestReadReplyDrainsAllInterleavedFrameKinds covers the frame classes a real
// server mixes in: a MappingNotify, an ordinary input event, and both delivered
// with the send-event bit set. All of them are EVENTS and must be discarded in
// order, leaving the single reply to be matched by sequence.
func TestReadReplyDrainsAllInterleavedFrameKinds(t *testing.T) {
	s := startCovFakeXServer(t, covUsFakeLayout())
	a := s.openActuator(t)
	s.setFocusReplyWindow(covStrayFocusWindowID)
	if err := a.Click(covStrayResponsiveBarrier); err != nil {
		t.Fatalf("barrier Click on a clean stream: %v", err)
	}

	s.setStrayFrames(
		covMappingNotifyFrame(x11MappingModifier),
		covEventFrame(xEventKeyPress, covStrayEventDetail),
		covEventFrame(xEventButtonPress, covStrayEventDetail),
		covSendEventMappingNotifyFrame(x11MappingKeyboard),
	)

	win, err := a.Focus()
	if err != nil {
		t.Fatalf("Focus() behind four interleaved event frames: %v", err)
	}
	if win != covStrayFocusWindowID {
		t.Errorf("Focus() = 0x%x, want 0x%x — one of the event frames was consumed as the reply", win, covStrayFocusWindowID)
	}

	// Not sticky: the stream is still frame-aligned afterwards.
	if win, err = a.Focus(); err != nil || win != covStrayFocusWindowID {
		t.Errorf("second Focus() = (0x%x, %v), want (0x%x, nil) — the connection must stay frame-aligned", win, err, covStrayFocusWindowID)
	}
}

// TestReadReplyDrainIsBounded pins the drain bound: a bounded number of events
// is recovered from, and one frame more than the bound is reported as a
// desynchronised connection — never an unbounded wait on a stream that will
// never deliver the reply (that would be a hang, the worst failure mode).
func TestReadReplyDrainIsBounded(t *testing.T) {
	cases := []struct {
		name    string
		frames  int
		wantErr bool
	}{
		{name: "at-the-bound-is-recovered", frames: x11MaxDrainedEvents, wantErr: false},
		{name: "one-past-the-bound-is-refused", frames: x11MaxDrainedEvents + 1, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := startCovFakeXServer(t, covUsFakeLayout())
			a := s.openActuator(t)
			s.setFocusReplyWindow(covStrayFocusWindowID)
			if err := a.Click(1); err != nil {
				t.Fatalf("barrier Click on a clean stream: %v", err)
			}
			frames := make([][]byte, tc.frames)
			for i := range frames {
				frames[i] = covMappingNotifyFrame(x11MappingKeyboard)
			}
			s.setStrayFrames(frames...)

			win, err := a.Focus()
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("Focus() behind %d event frames (the bound): %v", tc.frames, err)
				}
				if win != covStrayFocusWindowID {
					t.Errorf("Focus() = 0x%x, want 0x%x", win, covStrayFocusWindowID)
				}
				return
			}
			if err == nil {
				t.Fatalf("Focus() behind %d event frames was answered with 0x%x — a bounded drain must refuse instead of waiting forever", tc.frames, win)
			}
			if !strings.Contains(err.Error(), "desynchronised") {
				t.Errorf("Focus() error = %v, want it to name the desynchronised connection", err)
			}
		})
	}
}

// TestReadReplyRefusesOffSequenceReply pins the sequence gate: a reply whose
// sequence (header[2:4]) is not the request being awaited is refused, because
// returning it would answer a DIFFERENT request — the silent-wrong-answer class
// the whole fix exists to eliminate. The out-of-range case also drives the
// 16-bit wrap boundary: the fake writes a small, plausible-looking focus window
// (0xff00) as the sequence, which only a comparand that ignores the high byte
// could accept.
func TestReadReplyRefusesOffSequenceReply(t *testing.T) {
	cases := []struct {
		name     string
		preload  uint16 // counter value BEFORE the request increments it
		replySeq uint16
	}{
		{name: "previous-request-answer", preload: covStrayRequestedSeq - 1, replySeq: covStrayRequestedSeq - 1},
		{name: "focus-window-as-sequence", preload: covStrayRequestedSeq - 1, replySeq: covStrayFocusWindowID & 0xffff},
		{name: "wrap-boundary", preload: covStrayWrapSeq, replySeq: 0xff00},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := startCovFakeXServer(t, covUsFakeLayout())
			s.setFocusReplySeq(tc.replySeq)

			c, err := dialX11(s.display)
			if err != nil {
				t.Fatalf("dialX11(%s): %v", s.display, err)
			}
			defer c.close()
			req := make([]byte, 4)
			req[0] = x11OpGetInputFocus
			binary.LittleEndian.PutUint16(req[2:], 1)
			c.seq = tc.preload // the request below carries preload+1
			if err := c.request(req); err != nil {
				t.Fatalf("write GetInputFocus: %v", err)
			}
			header, _, err := c.readReply("GetInputFocus")
			if err == nil {
				t.Fatalf("readReply accepted a reply with sequence %d while request %d was awaited — a stale frame must never be returned as the answer", tc.replySeq, c.seq)
			}
			if !strings.Contains(err.Error(), "sequence") {
				t.Errorf("readReply error = %v, want it to name the sequence mismatch", err)
			}
			if header != nil {
				t.Errorf("readReply returned a header alongside the error: %v", header)
			}
		})
	}
}

// TestReadReplyAcceptsMatchingSequence is the positive control for the test
// above: with the fake echoing the REQUESTED sequence, the same preloaded-counter
// path returns the reply. Without it, the refusing test could be passing for the
// wrong reason (e.g. a harness that never lets any reply through).
func TestReadReplyAcceptsMatchingSequence(t *testing.T) {
	s := startCovFakeXServer(t, covUsFakeLayout())
	s.setFocusReplySeq(covStrayRequestedSeq)
	s.setFocusReplyWindow(covStrayFocusWindowID)

	c, err := dialX11(s.display)
	if err != nil {
		t.Fatalf("dialX11(%s): %v", s.display, err)
	}
	defer c.close()
	req := make([]byte, 4)
	req[0] = x11OpGetInputFocus
	binary.LittleEndian.PutUint16(req[2:], 1)
	c.seq = covStrayRequestedSeq - 1
	if err := c.request(req); err != nil {
		t.Fatalf("write GetInputFocus: %v", err)
	}
	header, _, err := c.readReply("GetInputFocus")
	if err != nil {
		t.Fatalf("readReply on a matching-sequence reply: %v", err)
	}
	if got := binary.NativeEndian.Uint32(header[8:12]); got != covStrayFocusWindowID {
		t.Errorf("focus = 0x%x, want 0x%x", got, covStrayFocusWindowID)
	}
}

// TestReadReplyFailsLoudlyOnTruncatedStream covers the third framing failure:
// the stream carries events and then DIES without ever delivering the reply
// (server crash / killed session). The bounded drain must surface that as an
// error instead of waiting for more frames — and it must do so immediately, so
// the assertion is bounded by a generous budget rather than by the read
// deadline (which the drain loop shares with the ordinary read).
func TestReadReplyFailsLoudlyOnTruncatedStream(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	c := &x11Client{conn: client}
	go func() {
		for i := 0; i < x11MaxDrainedEvents; i++ {
			if _, err := server.Write(covMappingNotifyFrame(x11MappingKeyboard)); err != nil {
				return
			}
		}
		server.Close() // the reply never comes //stylegate:ignore close of a net.Pipe end whose peer is the test's own client; Close on a pipe cannot fail
	}()
	done := make(chan error, 1)
	go func() {
		_, _, err := c.readReply("GetInputFocus")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("readReply returned nil for a stream that died before the reply")
		}
		if !strings.Contains(err.Error(), "read reply") {
			t.Errorf("readReply error = %v, want a read failure", err)
		}
	case <-time.After(covStrayStreamBudget):
		t.Fatalf("readReply neither answered nor failed within %s on a stream that closed", covStrayStreamBudget)
	}
}

// ---------------------------------------------------------------------------
// Keymap invalidation (the MappingNotify → keymap-dirty signal).
// ---------------------------------------------------------------------------

// TestMappingNotifyMarksKeymapDirtyAndRefreshes asserts the contract between
// the reader and the caller that caches keycodes: a drained MappingNotify for a
// keyboard (or modifier) mapping sets the dirty flag; the next keycode
// resolution refetches the mapping and clears it; a MappingNotify for the
// POINTER mapping, or any other event type, must NOT — refetching on every
// stray motion event would defeat the cache entirely.
func TestMappingNotifyMarksKeymapDirtyAndRefreshes(t *testing.T) {
	cases := []struct {
		name      string
		frame     []byte
		wantFresh bool
	}{
		{name: "keyboard-mapping", frame: covMappingNotifyFrame(x11MappingKeyboard), wantFresh: true},
		{name: "modifier-mapping", frame: covMappingNotifyFrame(x11MappingModifier), wantFresh: true},
		{name: "send-event-keyboard-mapping", frame: covSendEventMappingNotifyFrame(x11MappingKeyboard), wantFresh: true},
		{name: "pointer-mapping-does-not-invalidate", frame: covMappingNotifyFrame(x11MappingPointer), wantFresh: false},
		{name: "unrelated-event-does-not-invalidate", frame: covEventFrame(xEventKeyPress, covStrayEventDetail), wantFresh: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := startCovFakeXServer(t, covUsFakeLayout())
			a := s.openActuator(t)

			// Prime the cache and count the fetch.
			a.mu.Lock()
			if _, err := a.keycodeForLocked(covKeysymLowerN); err != nil {
				a.mu.Unlock()
				t.Fatalf("priming keycodeForLocked: %v", err)
			}
			a.mu.Unlock()
			primed := len(s.requestsOf(x11OpGetKeyboardMapping))

			s.setStrayFrames(tc.frame)
			if err := a.Click(1); err != nil {
				t.Fatalf("barrier Click behind a stray frame: %v", err)
			}

			a.mu.Lock()
			dirty := a.c.keymapDirty
			pos, err := a.keycodeForLocked(covKeysymLowerN)
			dirtyAfter := a.c.keymapDirty
			fetches := len(s.requestsOf(x11OpGetKeyboardMapping))
			a.mu.Unlock()

			if dirty != tc.wantFresh {
				t.Fatalf("keymapDirty after draining %s = %v, want %v", tc.name, dirty, tc.wantFresh)
			}
			if err != nil {
				t.Fatalf("keycodeForLocked after %s: %v", tc.name, err)
			}
			wantFetches := primed
			if tc.wantFresh {
				wantFetches++ // the stale cache must be refetched, not reused
			}
			if fetches != wantFetches {
				t.Errorf("GetKeyboardMapping requests = %d, want %d — the keymap cache was %s", fetches, wantFetches, map[bool]string{true: "not refreshed", false: "refreshed needlessly"}[tc.wantFresh])
			}
			if tc.wantFresh && dirtyAfter {
				t.Error("keymapDirty is still set after the mapping was refetched — every later keycode resolution would refetch again")
			}
			if pos.code != covKeycodeLowerN {
				t.Errorf("keycode for XK_n = %d, want %d", pos.code, covKeycodeLowerN)
			}
		})
	}
}

// TestKeymapRefreshPicksUpChangedMapping is the BEHAVIOURAL half of the
// invalidation: the server's mapping actually changes between two TypeText
// calls, and the second call must press the NEW keycode. A cache that survives
// a MappingNotify types the wrong character with no error anywhere — invisible
// data corruption, which is why this is asserted on the wire bytes rather than
// on the internal flag.
func TestKeymapRefreshPicksUpChangedMapping(t *testing.T) {
	changedCode := byte(covKeycodeLowerN + covStrayEventDetail)
	moved := covUsFakeLayout()
	moved[covKeysymLowerN] = struct {
		code  byte
		shift uint32
	}{code: changedCode}

	s := startCovFakeXServer(t, covUsFakeLayout())
	a := s.openActuator(t)

	const typed = "n"
	if err := a.TypeText(typed); err != nil {
		t.Fatalf("first TypeText(%q): %v", typed, err)
	}
	if got := s.fakeInputEvents(); len(got) == 0 || got[0].Detail != covKeycodeLowerN {
		t.Fatalf("first TypeText pressed keycode %v, want %d (the initial mapping)", got, covKeycodeLowerN)
	}

	// The server's mapping changes and it announces it.
	s.setLayout(moved)
	s.setStrayFrames(covMappingNotifyFrame(x11MappingKeyboard))
	before := len(s.fakeInputEvents())
	if _, err := a.Focus(); err != nil { // drains the MappingNotify frame
		t.Fatalf("Focus() behind the MappingNotify: %v", err)
	}

	if err := a.TypeText(typed); err != nil {
		t.Fatalf("second TypeText(%q): %v", typed, err)
	}
	got := s.fakeInputEvents()[before:]
	if len(got) != 2 {
		t.Fatalf("second TypeText produced %d injected events, want 2 (press+release)", len(got))
	}
	if got[0].Detail != changedCode {
		t.Errorf("second TypeText pressed keycode %d, want %d — the keycode cache survived a MappingNotify and typed with a stale mapping", got[0].Detail, changedCode)
	}
}

// TestTypeTextAfterMappingNotifyKeepsConnectionAligned is the actuator-level
// regression for the filed defect, on the fake server so the interleaving is
// guaranteed: the FIRST act verb on a connection whose GetKeyboardMapping
// registration queues a MappingNotify must succeed, and the connection must
// stay usable afterwards (the defect was sticky — every later round-trip
// answered with the stray frame).
func TestTypeTextAfterMappingNotifyKeepsConnectionAligned(t *testing.T) {
	s := startCovFakeXServer(t, covUsFakeLayout())
	a := s.openActuator(t)
	s.setFocusReplyWindow(covStrayFocusWindowID)

	s.setStrayFrames(covMappingNotifyFrame(x11MappingKeyboard))
	if err := a.TypeText(covStrayTypedText); err != nil {
		t.Fatalf("the FIRST TypeText on a connection that queues a MappingNotify failed: %v", err)
	}

	// No sticky shift: an independent round-trip still answers honestly.
	win, err := a.Focus()
	if err != nil {
		t.Fatalf("Focus() after TypeText: %v", err)
	}
	if win != covStrayFocusWindowID {
		t.Errorf("Focus() = 0x%x, want 0x%x", win, covStrayFocusWindowID)
	}
	if got := len(s.fakeInputEvents()); got != covStrayTypedRunes*2 {
		t.Errorf("%d injected events, want %d", got, covStrayTypedRunes*2)
	}
}
