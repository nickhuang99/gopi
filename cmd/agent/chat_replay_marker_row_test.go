package main

// chat_replay_marker_row_test.go — the marker-row half of the replay's
// "no help text" contract, driven through the REAL install seam.
//
// THE DEFECT THIS PINS (measured 2026-10-22 on a real pty): a replay printed
// `… PgUp/PgDn scroll · Alt+↑/↓ one row` on every frame the driver typed — 103
// occurrences in one run, 33 of them after the first key press — because
// internal/lineedit draws that sentence into the editing line's MARKER row
// whenever the buffer outgrows the window, and a replay's buffer always does
// (a whole recorded turn is typed into it). The sentence is the line editor's
// own key advertisement, and the replay's control layer claims EVERY typed key
// before the editor sees it, so the keys it named cannot do anything there.
//
// THE TEST ASKS THE SURFACE, NOT THE CONSTANT: it installs the control layer on a
// real editor (install is the production call) and reads the editor's own answer
// with the reader beside the writer (WindowKeyRow). A test that asserted a
// constant was absent would pass on a build whose renderer ignored the flag —
// which is exactly what happened on the first attempt at this fix.

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/lineedit"
)

// TestChatReplayInstallTurnsTheMarkerKeyRowOff pins the ONE act that removes the
// key sentence from the replay's screen.
func TestChatReplayInstallTurnsTheMarkerKeyRowOff(t *testing.T) {
	ed := lineedit.NewEditor(nil, &strings.Builder{}, "you> ", nil)
	if !ed.WindowKeyRow() {
		t.Fatalf("a fresh editor already has the marker key row off — the default must be the historic behaviour, or every other surface loses a sentence it relies on")
	}
	c := newChatReplayControl(nil)
	c.install(ed)
	if ed.WindowKeyRow() {
		t.Fatalf("the replay installed its control layer and the editing line still advertises the scroll keys: a watcher is told `PgUp/PgDn scroll` on a surface that claims every typed key before the editor sees it")
	}
	if !ed.InputLocked() {
		t.Fatalf("the replay's install left the editing line writable — the two halves of install must be taken together")
	}
}

// TestChatReplayMarkerKeyRowComesBackWithTheLine: releaseLine hands the editing
// line back when the replay ends, and the key row comes back with it — the
// marker chrome belongs to the REPL the operator returns to, and a replay that
// silently stripped it for the rest of the process would be a second defect
// dressed as a fix.
func TestChatReplayMarkerKeyRowComesBackWithTheLine(t *testing.T) {
	ed := lineedit.NewEditor(nil, &strings.Builder{}, "you> ", nil)
	c := newChatReplayControl(nil)
	c.install(ed)
	if ed.WindowKeyRow() {
		t.Fatalf("fixture: install did not turn the key row off")
	}
	// markClosing is the ONE moment the replay stops owning the surface: it flips
	// the mode bit and releases the line (chat_replay_control.go).
	c.markClosing()
	if !ed.WindowKeyRow() {
		t.Fatalf("the replay ended and the editing line never got its marker key row back — the REPL the operator returns to has been quietly changed by a surface that no longer owns it")
	}
	if ed.InputLocked() {
		t.Fatalf("the replay ended and the line is still held — the first characters typed at the returned prompt would be dropped")
	}
}
