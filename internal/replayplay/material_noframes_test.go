package replayplay

// material_noframes_test.go — the REACHABILITY CONTRACT of the frame-less
// refusal (Rule 16).
//
// The production arm this pins is not reachable from LoadSession: every timeline
// shape still yields a round, and a session with no timeline returns from the
// earlier arm that names both absent artifacts. So noFramesRefusal carries the
// error SHAPING and is driven directly here — the same remedy a prior Rule 16 fix
// in this tree used (extract the shaping, then test it), and the reason the
// branch is verifiable at all.
//
// THE DISCRIMINATOR IS THE CHAIN, NOT THE SENTENCE. `%v` renders the inner
// error's TEXT exactly as `%w` does, so any assertion that string-matches the
// message passes on the broken code and is worthless. What changes between the
// two is unwrap-ability, so that is what is asserted.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/uiclient"
)

// TestNoFramesRefusalKeepsTheStreamErrorReachable drives the refusal with a REAL
// error from the real stream leg (a corrupt recording, not a hand-rolled value),
// so the test cannot pass over a seam that production never fills.
func TestNoFramesRefusalKeepsTheStreamErrorReachable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, filepath.Base(filepath.Clean(dir))+uiclient.ScreenStreamSuffix)
	if err := os.WriteFile(path, []byte("garbage\n"), 0o600); err != nil {
		t.Fatalf("write the corrupt recording: %v", err)
	}
	cols, rows, geo := 80, 24, GeometryFromFallback
	st, _, streamErr := loadStreamLeg(dir, StreamFromAuto, &cols, &rows, &geo)
	if streamErr == nil {
		t.Fatal("precondition: a corrupt recording was accepted by the stream leg")
	}
	if st != nil {
		t.Fatalf("precondition: an unreadable recording produced a stream %+v", st)
	}

	err := noFramesRefusal(dir, streamErr)
	if err == nil {
		t.Fatal("the refusal returned no error, so a session with nothing to replay played")
	}

	// (1) THE RED/GREEN SWITCH. `%w` makes the stream error a CAUSE; `%v` leaves
	// the chain empty and this is false. Nothing else in the test can substitute
	// for it.
	if !errors.Is(err, streamErr) {
		t.Fatalf("errors.Is cannot reach the stream error through the refusal — the chain is broken (Rule 16):\n  refusal: %v\n  stream:  %v", err, streamErr)
	}
	if errors.Unwrap(err) == nil {
		t.Fatalf("the refusal wraps nothing, so its cause is unreadable: %v", err)
	}

	// (2) THE MESSAGE STILL NAMES BOTH FACTS — the directory the caller asked
	// about, and the reason the empty material is a different finding from an
	// empty screen.
	for _, want := range []string{dir, "no other artifact carries a frame"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal does not name %q: %v", want, err)
		}
	}
	// …and it names the stream error's own text, so an operator reading only the
	// message still sees WHY the recording was unusable.
	if !strings.Contains(err.Error(), streamErr.Error()) {
		t.Fatalf("the refusal does not carry the stream error's text:\n  refusal: %v\n  stream:  %v", err, streamErr)
	}
}

// TestNoFramesRefusalWithoutAStreamErrorNamesOnlyTheEmptyMaterial pins the other
// arm: with no stream error the refusal must NOT claim a recording was unusable
// (a false finding is worse than a plain one — Rule 15).
func TestNoFramesRefusalWithoutAStreamErrorNamesOnlyTheEmptyMaterial(t *testing.T) {
	dir := "/tmp/a-session-with-nothing"
	err := noFramesRefusal(dir, nil)
	if err == nil {
		t.Fatal("the refusal returned no error")
	}
	if !strings.Contains(err.Error(), dir) {
		t.Fatalf("the refusal does not name the directory %q: %v", dir, err)
	}
	if strings.Contains(err.Error(), "could not be read") {
		t.Fatalf("a session with no stream error was told its stream could not be read: %v", err)
	}
}
