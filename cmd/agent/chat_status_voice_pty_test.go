// chat_status_voice_pty_test.go — the `/voice` field on the status row, judged on
// the REAL terminal: a pty, the real chat input layer, the real editor read loop
// as the only writer, and a scripted recorder standing in for the microphone.
//
// WHY THIS FILE EXISTS BESIDE THE UNIT CASE (Rule 31). chat_status_voice_test.go
// proves the field's COMPOSITION; this one proves it reaches the CELLS the
// operator's terminal shows, on the carrier the REPL actually builds. The seam
// between them — the status source's voice hook, the publish reaching the read
// loop, the field landing inside the live block's reserved cells — is exactly the
// part a unit test cannot see, and it is the part this feature adds.
//
// THE JUDGE IS THE GRID (Rule 10.2/ENGINEERING): every assertion reads the sink's
// vt100 cell grid back, never the bytes written and never an OCR pass.
package main

import (
	"strings"
	"sync"
	"testing"

	"github.com/nickhuang99/gopi/internal/lineedit"
)

// voiceStatusPtyWidth is the terminal this case gives the layout: the PIN, so the
// ALIGNED row is the form under test (a narrower terminal steps down the
// degradation ladder and would measure a different row than the one the field was
// budgeted for).
const voiceStatusPtyWidth = statusColumnsAtCols

// voiceStatusRows leaves room for the status row plus the mode's own notes.
const voiceStatusRows = 20

// voiceStatusHarness is the production carrier with the STATUS SOURCE wired the
// way the REPL wires it — including the voice hook, which is the connection this
// case is about.
type voiceStatusHarness struct {
	*voicePtyGrid
	src *chatStatusSource
	// mu guards live, which the test's goroutine writes and the editor's read
	// loop reads through the voice hook (the same cross-goroutine shape the
	// production seam has).
	mu   sync.Mutex
	live voiceChatLive
}

func (h *voiceStatusHarness) phase() voiceChatLive {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.live
}

func (h *voiceStatusHarness) setPhase(live voiceChatLive) {
	h.mu.Lock()
	h.live = live
	h.mu.Unlock()
	h.in.VoicePublishStatus()
}

// newVoiceStatusHarness builds the grid at the PIN and installs the turn's status
// row over a REAL chatStatusSource, with the voice hook plugged in exactly as
// cli_chat.go plugs it: the source holds a function, the harness holds the state.
func newVoiceStatusHarness(t *testing.T) *voiceStatusHarness {
	t.Helper()
	rec := &scriptedRecorder{desc: "status mic"}
	grid := newVoicePtyGridSize(t, rec, voiceStatusPtyWidth, voiceStatusRows)
	h := &voiceStatusHarness{voicePtyGrid: grid}
	src := newChatStatusSource("deepseek-flash", true)
	src.setVoiceFn(h.phase)
	h.src = src
	grid.in.SetStatus(src.status)
	if !grid.in.StatusNotifyEnabled() {
		t.Fatal("the harness supplied no wake seam: a publish could never reach the read loop, and every wait below would fail for the wrong reason")
	}
	// The row is painted at the prompt before anything else happens, so the
	// baseline this case compares against is a row the read loop really drew.
	grid.in.PublishStatus()
	grid.waitFor(t, "the status row at the prompt", func(rows string) bool {
		return strings.Contains(rows, statusLiveIdle)
	})
	return h
}

// statusRowOnGrid returns the grid row carrying the status line (the one that
// holds the live block), or "" when the row is not painted.
func (h *voiceStatusHarness) statusRowOnGrid() string {
	for _, row := range strings.Split(h.rows(), "\n") {
		if strings.Contains(row, statusLiveIdle) || strings.Contains(row, voiceChatLiveRecordingWord+" ") {
			return row
		}
	}
	return ""
}

// TestStatusVoiceFieldOnAPtyShowsThePhaseAndVanishesWhenTheModeEnds is the
// operator's claim end to end, judged on the CELLS of a real terminal:
//
//	the field appears when the microphone opens, follows the phase, and is GONE —
//	not blanked, not zeroed — when the mode ends, with the row's own width
//	unchanged at every step.
//
// The state is driven through the SAME reader the mode installs (the source's
// voice hook reads this harness's phase), so what is asserted is the wiring the
// REPL performs rather than a string handed straight to the composer.
func TestStatusVoiceFieldOnAPtyShowsThePhaseAndVanishesWhenTheModeEnds(t *testing.T) {
	h := newVoiceStatusHarness(t)

	// 1) THE BASELINE: the mode is off, the field is on no cell of the row.
	h.setPhase(voiceChatLiveIdle)
	base := h.statusRowOnGrid()
	if base == "" {
		t.Fatalf("no status row on the grid\n--- grid ---\n%s", h.rows())
	}
	for _, absent := range []string{voiceChatLiveRecordingWord, voiceChatLiveTranscribingWord} {
		if fieldOnRow(base, absent) {
			t.Fatalf("the baseline row already carries %q: %q", absent, base)
		}
	}

	// 2) CAPTURING: the field is on the row, and the row is no wider than the pin.
	h.setPhase(voiceChatLiveRecording)
	h.waitFor(t, "the capturing field on the row", func(string) bool {
		return fieldOnRow(h.statusRowOnGrid(), voiceChatLiveRecordingWord)
	})
	capturing := h.statusRowOnGrid()
	if got := lineedit.DisplayWidth(capturing); got > voiceStatusPtyWidth {
		t.Errorf("the capturing row is %d cells, past the %d the layout is composed for: %q", got, voiceStatusPtyWidth, capturing)
	}

	// 3) TRANSCRIBING: the field FOLLOWS the phase — the word changes, and it
	//    changes in place (the row does not gain cells moving from one phase to
	//    the next: both words are the same width by construction, and this is the
	//    screen-level check of that).
	h.setPhase(voiceChatLiveTranscribing)
	h.waitFor(t, "the transcribing field on the row", func(string) bool {
		return fieldOnRow(h.statusRowOnGrid(), voiceChatLiveTranscribingWord)
	})
	transcribing := h.statusRowOnGrid()
	if fieldOnRow(transcribing, voiceChatLiveRecordingWord) {
		t.Errorf("the row still says %q while the mode is transcribing: %q", voiceChatLiveRecordingWord, transcribing)
	}
	if got, want := lineedit.DisplayWidth(transcribing), lineedit.DisplayWidth(capturing); got != want {
		t.Errorf("the row changed width between the phases (%d → %d): the field's two words must occupy the same cells\n"+
			"capturing   %q\ntranscribing %q", want, got, capturing, transcribing)
	}

	// 4) THE MODE ENDS — Esc, or any exit. The field is GONE from the cells, and
	//    gone as a WHOLE: no leftover glyph, no blanked-out hole where it was.
	h.setPhase(voiceChatLiveIdle)
	h.waitFor(t, "the field to leave the row when the mode ends", func(string) bool {
		row := h.statusRowOnGrid()
		return row != "" && !fieldOnRow(row, voiceChatLiveRecordingWord) && !fieldOnRow(row, voiceChatLiveTranscribingWord)
	})
	after := h.statusRowOnGrid()
	if after == "" {
		t.Fatalf("the status row vanished with the mode\n--- grid ---\n%s", h.rows())
	}

	// 5) AND THE ROW IS THE ROW IT STARTED AS — the property that makes "the
	//    columns hold still" checkable: the state that follows the mode is the
	//    state that preceded it, cell for cell.
	if after != base {
		t.Errorf("the row after the mode ended is not the row before it began:\n before %q\n after  %q", base, after)
	}
}

// fieldOnRow reports whether word appears on the row as the VOICE FIELD — i.e.
// followed by the block's separator, which is the shape the composer emits
// ("rec · thinking"). It is deliberately not a bare substring test: a state word
// could contain the same letters, and a case that matched one would be asserting
// on the wrong cell.
func fieldOnRow(row, word string) bool {
	return strings.Contains(row, word+lineedit.StatusSep)
}
