package main

// chat_screen_e2e_real_test.go — the REPL-layer FAR-turn acceptance, driven by a
// REAL recording rather than by a painted fixture.
//
// WHY THIS FILE IS SEPARATE, and why it is opt-in. The other e2e cases paint their
// own frames, so they can prove the plumbing but not that a far turn number
// reaches the artifact — there are only two turns in the script, so a request for
// a turn 60-something could only be satisfied by a locator that guesses. This case
// closes that gap with the only honest source: a stream the real renderer wrote,
// fed to the real REPL entry point (runChatWithInput), and a `/screen N` typed at
// it for a turn that is NEITHER the newest (which a bare `/screen` would pick) NOR
// the first (which is the edge). It skips without GOPI_REPLAY_SGR_TURN, because a
// recording is the whole point — the same contract as
// uiclient.TestSgrTurnLocatorOnARealRecording.
//
// WHAT IT DOES *NOT* PROVE: that a 60-turn CONVERSATION behaves this way. The
// frames are copied from a recording into a fresh run's stream; the run itself had
// two turns. So this is the REPL layer's far-number path, not a long session's.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/uiclient"
)

// chatScreenE2ERealTurnCopies is how many frames of the chosen span are appended.
// The span is copied WHOLE (that is the product claim — a turn is never truncated),
// so this is a floor for a sanity check rather than a limit.
const chatScreenE2ERealTurnMinFrames = 2

func TestChatScreenE2EFarTurnFromARealRecording(t *testing.T) {
	recording := strings.TrimSpace(os.Getenv("GOPI_REPLAY_SGR_TURN"))
	if recording == "" {
		t.Skip("GOPI_REPLAY_SGR_TURN is unset — no recorded stream to replay")
	}
	turns, err := uiclient.LocateSgrTurns(recording)
	if err != nil {
		t.Fatalf("LocateSgrTurns(%s): %v", recording, err)
	}
	if len(turns.Spans) < 3 {
		t.Skipf("%s carries %d turn(s) — a FAR turn needs a recording with a middle", recording, len(turns.Spans))
	}
	// THE FAR TURN IS THE SECOND-TO-LAST: not the newest (a bare `/screen` would
	// fetch that anyway, so choosing it would prove nothing about the argument),
	// and not the first (where a clamp to the boundary would look like a hit).
	far := turns.Spans[len(turns.Spans)-2]
	newest := turns.Spans[len(turns.Spans)-1]
	if far.Frames() < chatScreenE2ERealTurnMinFrames {
		t.Fatalf("the chosen span %+v holds %d frame(s)", far, far.Frames())
	}
	_, all, err := uiclient.ReadSgrFrames(mustOpen(t, recording))
	if err != nil {
		t.Fatalf("read the recording %s: %v", recording, err)
	}
	if far.EndFrame > len(all) {
		t.Fatalf("span %+v runs past the %d frames on disk", far, len(all))
	}
	span := all[far.StartFrame:far.EndFrame]

	paintPlan := func(paintFrame func([]byte), request int) {
		if request != 1 {
			return
		}
		for _, f := range span {
			paintFrame(f)
		}
	}
	input := chatScreenE2EPrimer + "/screen " + chatScreenItoa(far.Turn) + "\nwhat is on my screen?\n"
	h := runScreenE2EPlan(t, input, paintPlan)

	matches, err := filepath.Glob(filepath.Join(h.sessDir, "screen-turn*"+uiclient.ScreenStreamSuffix))
	if err != nil {
		t.Fatalf("glob the capture: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("found %d captures in %s (%v), want exactly one — console tail:\n%s",
			len(matches), h.sessDir, matches, chatScreenTail(h.console, 1200))
	}
	artifact := matches[0]
	wantName := "screen-turn" + chatScreenItoa(far.Turn) + uiclient.ScreenStreamSuffix
	if filepath.Base(artifact) != wantName {
		t.Errorf("the capture is named %q, want %q — the name must come from the turn the ARGUMENT selected",
			filepath.Base(artifact), wantName)
	}

	body := chatScreenE2EPayload(t, artifact)
	// THE CENTRAL ASSERTION: the artifact holds the turn the operator NAMED.
	if !bytes.Contains(body, []byte("turn "+chatScreenItoa(far.Turn)+" ")) {
		t.Errorf("the capture does not paint turn %d's own header — it holds: %s",
			far.Turn, chatScreenE2ETurnsIn(body))
	}
	// AND NOT THE NEWEST TURN: an artifact that names its turn but carries a later
	// one's header is the "wrong segment" failure, invisible to a byte count.
	if bytes.Contains(body, []byte("turn "+chatScreenItoa(newest.Turn)+" ")) {
		t.Errorf("the capture of turn %d also holds turn %d — the segment runs past its own turn",
			far.Turn, newest.Turn)
	}
	t.Logf("real recording: %d turn(s) %s in %d frames; /screen %d → %s holds %s",
		len(turns.Spans), turns.RangeWords(), turns.StreamFrames, far.Turn,
		filepath.Base(artifact), chatScreenE2ETurnsIn(body))

	// THE REFERENCE REACHES THE MODEL, and it is a REFERENCE: the note names the
	// path and carries no painted byte (the epic's hard ban).
	reqs := h.requests()
	if len(reqs) < 2 {
		t.Fatalf("the run sent %d request(s) (exit %d)", len(reqs), h.code)
	}
	last := reqs[len(reqs)-1]
	if !bytes.Contains(last, []byte(artifact)) {
		t.Errorf("the request after the capture does not name %q", artifact)
	}
	if bytes.Contains(last, []byte("turn "+chatScreenItoa(far.Turn)+" ")) {
		t.Errorf("the request body carries PAINTED CONTENT — the capture was inlined instead of referenced")
	}
}
