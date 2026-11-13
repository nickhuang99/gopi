//go:build linux

package lineedit

// paste_burst_pty_test.go — the paste-burst fix measured over a REAL pty, where
// the test acts as the terminal emulator.
//
// WHY A PTY. The classification is a property of how the KERNEL delivers
// keystrokes: a human's key is one read, a paste is one write (and therefore, in
// practice, one read). A test double can only assert what the double was told to
// pretend, so it cannot show that fast typing really is one key per read — which
// is exactly the premise the fix rests on. Both halves are measured here on the
// same descriptor and the same editor:
//
//	4 keys at PasteBurstCharInterval, then CR  → the line SUBMITS
//	ONE write of a multi-line block, then CR   → content: one message, newlines kept
//
// and the second case also carries the Rule 15 half: the suppressed Enter is
// announced, exactly ONCE for the burst.

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/sgr"
)

const (
	// pastePtyFastText is the four single-key reads: exactly
	// PasteBurstMinChars+1 characters, so "one read of these" WOULD be a paste
	// and "one key per read" must not be.
	pastePtyFastText = "abcd"
	// pastePtyBlock is ONE write carrying a multi-line block. Its two LFs are
	// the newlines that have to survive as content, and it is longer than
	// PasteBurstMinChars so the single read arms the burst.
	pastePtyBlock = "l1\nl2"
)

// writeKey writes s as ONE pty write — the emulator's keystroke — and then waits
// long enough that the NEXT write cannot be coalesced into the same read: the
// kernel gives a reader everything that is queued, so "one key per read" is a
// property of the pacing, not of the write call alone. The pace is
// PasteBurstCharInterval, the same interval the classifier's constant names, so
// the case delivers the cadence under test rather than a calmer one.
func writeKey(t *testing.T, peer *keyProtocolPeer, s string) {
	t.Helper()
	if _, err := peer.master.Write([]byte(s)); err != nil { //stylegate:ignore the emulator writing the keys a keyboard would send
		t.Fatalf("emulator write %q: %v", s, err)
	}
	time.Sleep(PasteBurstCharInterval) //stylegate:allow-sleep the delivery pace: two writes this far apart are two reads
}

// TestPasteBurstFastTypingSubmitsOverRealPTY is the acceptance case for the
// defect: four characters delivered 8 ms apart — the cadence a fast human, an IME
// commit or a key repeat produces — used to accumulate into a paste burst, and the
// Enter that followed became a newline with no explanation. The unit of the
// classification is now ONE read, so this line must submit.
func TestPasteBurstFastTypingSubmitsOverRealPTY(t *testing.T) {
	peer := &keyProtocolPeer{
		replyTo: sgr.KeyboardProtocolQuerySeq(),
		reply:   []byte(sgr.KeyboardProtocolReplySeq(sgr.KeyboardProtocolDisambiguateEscapeCodes)),
	}
	term, peer := newKeyProtocolRig(t, peer)
	defer func() {
		// deliberately ignored: the case's own teardown.
		_ = term.Close()
	}()
	peer.waitForSeq(t, sgr.KeyboardProtocolPushSeq(sgr.KeyboardProtocolDisambiguateEscapeCodes))

	var warns bytes.Buffer
	ed := NewChatEditor(term, io.Discard, promptForTest, nil, &warns, nil)
	pending := keyProtocolDriveAsync(ed)

	for _, r := range pastePtyFastText {
		writeKey(t, peer, string(r))
	}
	// The Enter, delivered as its own keystroke — the key the defect swallowed.
	writeKey(t, peer, "\r")

	var out keyProtocolRunOutcome
	select {
	case out = <-pending:
	case <-time.After(keyProtocolSubmitWait):
		t.Fatal("the Enter after four fast keys did not submit — it was read as pasted content (the defect)")
	}
	if out.err != nil {
		t.Fatalf("Run: %v", out.err)
	}
	if out.res.EOF || out.res.Interrupted {
		t.Fatalf("Run returned EOF=%v interrupted=%v, want a submitted line", out.res.EOF, out.res.Interrupted)
	}
	if got := strings.TrimRight(out.res.Line, "\n"); got != pastePtyFastText {
		t.Fatalf("the submitted line is %q, want %q", out.res.Line, pastePtyFastText)
	}
	if n := strings.Count(warns.String(), pasteBurstWarnMarker); n != 0 {
		t.Fatalf("%d paste warning(s) printed over fast typing that was never a paste: %q", n, warns.String())
	}
}

// TestPasteBurstOneWriteIsStillOneMessageOverRealPTY is the other half of the
// criterion, and the property the feature exists for: a block written in ONE go is
// ONE message. Its newlines stay in the buffer, the Enter that arrives inside the
// window is content rather than a submit, and — the Rule 15 half — the operator is
// told ONCE that an Enter was read as content.
func TestPasteBurstOneWriteIsStillOneMessageOverRealPTY(t *testing.T) {
	peer := &keyProtocolPeer{
		replyTo: sgr.KeyboardProtocolQuerySeq(),
		reply:   []byte(sgr.KeyboardProtocolReplySeq(sgr.KeyboardProtocolDisambiguateEscapeCodes)),
	}
	term, peer := newKeyProtocolRig(t, peer)
	defer func() {
		// deliberately ignored: the case's own teardown.
		_ = term.Close()
	}()
	peer.waitForSeq(t, sgr.KeyboardProtocolPushSeq(sgr.KeyboardProtocolDisambiguateEscapeCodes))

	var warns bytes.Buffer
	ed := NewChatEditor(term, io.Discard, promptForTest, nil, &warns, nil)
	pending := keyProtocolDriveAsync(ed)

	// ONE write: the block WITH its line endings, as a terminal hands a real
	// paste over. The two CRs are what a pasted multi-line text ends with, and
	// they arrive inside the same read — so they are content, not two submits.
	writeKey(t, peer, pastePtyBlock+"\r\r")
	select {
	case out := <-pending:
		t.Fatalf("an Enter inside the paste submitted the line: %+v (err %v) — a pasted block must be ONE message", out.res, out.err)
	case <-time.After(PasteEnterSuppressWindow):
	}

	// Past the window the same key sends it: one message, newlines intact.
	writeKey(t, peer, "\r")
	var out keyProtocolRunOutcome
	select {
	case out = <-pending:
	case <-time.After(keyProtocolSubmitWait):
		t.Fatal("the Enter past the window did not submit — the line is unsubmittable")
	}
	if out.err != nil {
		t.Fatalf("Run: %v", out.err)
	}
	want := pastePtyBlock + "\n\n"
	if out.res.Line != want {
		t.Fatalf("the submitted line is %q, want %q (the block and both suppressed newlines in ONE message)", out.res.Line, want)
	}

	// Rule 15: the operator was told, and told ONCE for the burst.
	if n := strings.Count(warns.String(), pasteBurstWarnMarker); n != 1 {
		t.Fatalf("%d paste warning(s) for ONE burst, want exactly 1: %q", n, warns.String())
	}
	// The message is deliberately SHORT (degradation_width_test.go owns the
	// width contract, and this same warning used to spend three rows of a
	// 101-column console — see warnPasteSuppressed): what must survive is that
	// the operator is told an Enter was kept as a newline, not submitted.
	if !strings.Contains(warns.String(), "Enter kept as newline") {
		t.Fatalf("the warning does not say what happened: %q", warns.String())
	}
}
