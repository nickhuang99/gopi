// chat_voice_teardown_join_test.go — the teardown JOIN, asserted without a clock.
//
// THE DEFECT (measured in cmd/agent, task 01a13205): the capture loop used to
// decide whether it owned the join channel by READING the field back
// (`if m.state.loopDone != nil { defer close(m.state.loopDone) }`), while
// `shutdown` clears that field BEFORE it waits on the channel. When the loop
// reached its first statement after the clear it saw nil, registered no close at
// all, and returned having closed nothing — so `shutdown` waited out the whole
// `voiceChatFinishWait` (30 s) on a goroutine that had already exited. The suite
// paid that window once per voice case; the shape it produced was the flat-CPU
// Sl process the epic was filed about.
//
// WHY THIS CASE IS THE REGRESSION GUARD AND NOT A DURATION ASSERTION. A case that
// measured the teardown in wall-clock seconds would be a case about this host's
// scheduling — the interleaving that loses is exactly the one a fast machine
// happens to avoid. The fix removes the READ: the loop is HANDED the channel by
// its launcher, so the close no longer depends on any field's state. This case
// reproduces the teardown's ordering (the clear happens first) and calls the loop
// with a stop that is ALREADY closed, so the loop's exit is decided by the
// channel's state alone — no sleep, no race, no bound to be slow.
package main

import "testing"

// TestVoiceLoopClosesTheEntryJoinChannelItWasLaunchedWith pins the property
// shutdown's join depends on: the loop closes the channel it was GIVEN, whatever
// the mode's field says at that moment.
func TestVoiceLoopClosesTheEntryJoinChannelItWasLaunchedWith(t *testing.T) {
	m := newVoiceTeardownJoinMode()
	stop := make(chan struct{})
	// THE LOOP LEAVES AT ONCE, decided by a channel and not by a clock: the stop is
	// already closed, so the loop's first select returns on its stop arm.
	close(stop)

	loopDone := make(chan struct{})
	// THE TEARDOWN'S OWN ORDER, reproduced: shutdown clears the field while holding
	// the mutex and only THEN waits on the channel it captured. A loop that read
	// the field instead of using what it was handed finds nil here — which is the
	// measured lost race.
	m.state.mu.Lock()
	m.state.loopDone = nil
	m.state.mu.Unlock()

	m.loop(voiceTeardownJoinEntry, stop, loopDone)

	select {
	case <-loopDone:
	default:
		t.Fatal("the capture loop returned WITHOUT closing the join channel it was launched with — " +
			"its close is conditional on m.state.loopDone, which the teardown clears before waiting, " +
			"so shutdown pays the whole voiceChatFinishWait on a goroutine that has already returned " +
			"(the flat-CPU window this case guards)")
	}
}

// newVoiceTeardownJoinMode is the smallest carrier the loop's exit path needs: the
// closed stop means the loop returns before it reaches the recorder, the editor or
// the mode's own state machine, so nothing else has to be built.
func newVoiceTeardownJoinMode() *voiceChatMode {
	return &voiceChatMode{}
}

// voiceTeardownJoinEntry is the generation the case launches the loop under. The
// loop exits on the stop arm before it consults the generation, and the value is
// named rather than inlined so the case states which incarnation it starts
// (Rule 1).
const voiceTeardownJoinEntry = 1
