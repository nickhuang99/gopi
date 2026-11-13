package main

// cli_voice_first_text_test.go — the FIRST-TEXT REPORT's contract, pinned.
//
// THE DEFECT THIS GUARDS (the red path): the bare capture printed
// "[voice] transcribed by doubao in 2.8s" and nothing else, so the operator could
// not tell what they actually waited through — a fixed recording window sits in
// front of that figure and never appeared anywhere. And the opposite failure is
// worse than the omission: a run whose engine returned NO text reported a
// duration beside it, which reads as a very fast answer to an utterance that was
// never recognised at all.

import (
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
)

// TestVoiceFirstTextReportNamesTheSplit is the report's primary contract: the
// total, and the two halves that explain it.
func TestVoiceFirstTextReportNamesTheSplit(t *testing.T) {
	opts := &voiceOptions{engine: config.AudioEngineRecording}
	got := voiceFirstTextReportLine(opts, voiceFirstTextReport{
		FirstTextMS: 14500, WindowMS: 12000, EngineMS: 2500,
	})
	for _, want := range []string{
		voiceFirstTextReportLabel, // "[voice] first text"
		voiceEngineLabelDoubao,    // which engine answered
		"14.5s",                   // the total the operator waited
		"capture window 12.0s",    // the fixed half
		"engine/channel 2.5s",     // the engine's own half
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report = %q, want it to carry %q", got, want)
		}
	}
	// The pre-fix shape: the line must NOT be only the engine's duration, which is
	// what "transcribed by … in 2.5s" already said.
	if strings.Contains(got, "transcribed by") {
		t.Errorf("report = %q: it must state the first text, not restate the engine line", got)
	}
}

// TestVoiceFirstTextAbsenceIsNeverAZeroDuration is the Rule 15 half: a run with
// no first text says so, and does NOT print 0 as if it had measured one.
func TestVoiceFirstTextAbsenceIsNeverAZeroDuration(t *testing.T) {
	opts := &voiceOptions{engine: config.AudioEngineRecording}
	const absence = "the engine returned no text for the 6.0s window"
	got := voiceFirstTextReportLine(opts, voiceFirstTextReport{Missing: absence})
	if !strings.Contains(got, "none") || !strings.Contains(got, absence) {
		t.Errorf("report = %q, want it to state the absence and name the reason", got)
	}
	if strings.Contains(got, "engine/channel") {
		t.Errorf("report = %q: a run that measured nothing must not carry a split", got)
	}
	// The WAIT is still truthful information and stays — but it is never called a
	// first text.
	waited := voiceFirstTextReportLine(opts, voiceFirstTextReport{
		FirstTextMS: 8800, WindowMS: 6100, EngineMS: 2700, Missing: absence,
	})
	if !strings.Contains(waited, "none") {
		t.Errorf("report = %q, want the absence stated even when the wait is known", waited)
	}
	if !strings.Contains(waited, "8.8s") {
		t.Errorf("report = %q, want the wait the run actually cost to be reported", waited)
	}
}

// TestVoiceFirstTextHandshakeRemainderIsNamed pins the third term: the split the
// chain reports must ACCOUNT for the total, so a run whose two halves leave a
// remainder names it instead of quietly dropping it.
func TestVoiceFirstTextHandshakeRemainderIsNamed(t *testing.T) {
	opts := &voiceOptions{engine: config.AudioEngineStreaming}
	got := voiceFirstTextReportLine(opts, voiceFirstTextReport{
		FirstTextMS: 3000, WindowMS: 0, EngineMS: 187, Streamed: true,
	})
	if !strings.Contains(got, "startup + channel handshake") {
		t.Errorf("report = %q, want the remainder named (it is the channel's handshake)", got)
	}
	// NOISE IS NOT NAMED. A remainder inside the floor is the two halves being read
	// a call apart, and a line that reported it would teach a reader to skip the
	// sentence that exists for the runs where the remainder IS the story.
	quiet := voiceFirstTextReportLine(opts, voiceFirstTextReport{
		FirstTextMS: 2600, WindowMS: 0, EngineMS: 2500, Streamed: true,
	})
	if strings.Contains(quiet, "startup + channel handshake") {
		t.Errorf("report = %q: a sub-floor remainder must not be reported", quiet)
	}
}

// TestVoiceFirstTextSubSecondFiguresStayInMilliseconds is why the unit switches
// at all: 187ms of channel latency rendered as "0.2s" would be the rounding away
// of the very number the comparison is made of.
func TestVoiceFirstTextSubSecondFiguresStayInMilliseconds(t *testing.T) {
	cases := []struct {
		ms   int
		want string
	}{
		{0, "0ms"},
		{187, "187ms"},
		{voiceFirstTextSubSecondCutoffMS - 1, "1999ms"},
		{voiceFirstTextSubSecondCutoffMS, "2.0s"},
		{12000, "12.0s"},
	}
	for _, c := range cases {
		if got := voiceSplitPartsMS(c.ms); got != c.want {
			t.Errorf("voiceSplitPartsMS(%d) = %q, want %q", c.ms, got, c.want)
		}
	}
}

// TestVoiceFirstTextStreamingNamesThePartial is the measurement's own honesty
// requirement: on the streaming channel the figure is a PARTIAL's arrival, which
// is a different event from "the engine returned", and the line says which.
func TestVoiceFirstTextStreamingNamesThePartial(t *testing.T) {
	opts := &voiceOptions{engine: config.AudioEngineStreaming}
	got := voiceFirstTextReportLine(opts, voiceFirstTextReport{
		FirstTextMS: 1203, EngineMS: 136, FinalMS: 2700, Streamed: true,
	})
	if !strings.Contains(got, "first partial") {
		t.Errorf("report = %q, want the figure labelled as a partial", got)
	}
	if !strings.Contains(got, "final transcript at 2.7s") {
		t.Errorf("report = %q, want the final transcript's own instant beside it", got)
	}
	if !strings.Contains(got, voiceEngineLabelDoubaoStream) {
		t.Errorf("report = %q, want the streaming engine named, not the batch one", got)
	}
}

// TestVoiceFirstTextWindowIsMeasuredNotTheRequestedSeconds is the property that
// makes the split a measurement: 12 requested seconds and 12.1 measured are two
// statements, and the report carries the one the operator lived through.
func TestVoiceFirstTextWindowIsMeasuredNotTheRequestedSeconds(t *testing.T) {
	opts := &voiceOptions{engine: config.AudioEngineRecording, seconds: 12}
	got := voiceFirstTextReportLine(opts, voiceFirstTextReport{
		FirstTextMS: 14600, WindowMS: 12100, EngineMS: 2500,
	})
	if !strings.Contains(got, "capture window 12.1s") {
		t.Errorf("report = %q, want the MEASURED window, not the requested 12s", got)
	}
}

// TestVoiceFirstTextChainNeverClaimsLessThanItsEngine is the arithmetic the line
// must never break: the total CANNOT be smaller than an engine duration the run
// observed, or the terms read as a sum that does not add up.
//
// MEASURED (the bug this pins): the first partial's wall stamp and the channel's
// own latency are read a call apart, so a run produced "first text at 212ms =
// capture window 0ms + engine/channel 214ms".
func TestVoiceFirstTextChainNeverClaimsLessThanItsEngine(t *testing.T) {
	opts := &voiceOptions{engine: config.AudioEngineStreaming}
	got := voiceFirstTextReportLine(opts, voiceFirstTextReport{
		FirstTextMS: 136, WindowMS: 0, EngineMS: 136, Streamed: true,
	})
	if !strings.Contains(got, "engine/channel 136ms") {
		t.Errorf("report = %q, want the total to be at least the engine's own figure", got)
	}
	// A REMAINDER TERM IS NEVER NEGATIVE, and a zero one is not printed either: the
	// chain stops at the figures that were observed.
	if strings.Contains(got, "startup + channel handshake") {
		t.Errorf("report = %q: there is nothing left over, so no remainder term belongs here", got)
	}
	if strings.Contains(got, "capture window") {
		t.Errorf("report = %q: a run that captured nothing must not carry a window term", got)
	}
}

// TestVoiceFeederSendFailureIsReported is the feeder's half of Rule 15: a run
// whose send side broke must be able to say so, because "the service answered
// nothing" and "this end stopped talking" produce the same empty transcript and
// are two different incidents.
func TestVoiceFeederSendFailureIsReported(t *testing.T) {
	feed := &voiceLiveFeeder{}
	if err := feed.sendFailure(); err != nil {
		t.Fatalf("a fresh feeder reports no failure, got %v", err)
	}
	first := stringError("the socket went away")
	feed.noteErr(first)
	second := stringError("a later, uninteresting failure")
	feed.noteErr(second)
	if got := feed.sendFailure(); got != first {
		t.Errorf("sendFailure() = %v, want the FIRST failure %v (the cause)", got, first)
	}
}

// TestVoiceRunClockMeasuresFromItsOrigin pins the clock's meaning: the figures are
// measured from the origin it was given (the process's start), not from the moment
// the clock was built.
func TestVoiceRunClockMeasuresFromItsOrigin(t *testing.T) {
	origin := time.Now()
	clock := newVoiceRunClock(origin)
	if got := clock.since(origin.Add(2500 * time.Millisecond)); got != 2500*time.Millisecond {
		t.Errorf("since(+2.5s) = %v, want 2.5s", got)
	}
	if got := clock.since(time.Time{}); got != 0 {
		t.Errorf("since(zero time) = %v, want 0 (nothing was stamped)", got)
	}
	// The origin is the PROCESS's start, so it is never after the clock and never
	// zero in a real run.
	if voiceRunOrigin().IsZero() {
		t.Error("voiceRunOrigin() is the zero time — the report would be measured from nowhere")
	}
}

// stringError is a one-line error so the feeder test reads as the cause/effect it
// is, without a table of scaffolding.
type stringError string

func (e stringError) Error() string { return string(e) }
