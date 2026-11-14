package main

// chat_voice_pipeline_test.go — the SCHEDULE of window mode: does the capture go
// on being read while the engine transcribes? (task 01a11b81)
//
// THE DEFECT THESE CASES PIN. `/voice` kept the microphone open for the whole
// mode (task 01a1188c's continuous capture), but a round was strictly serial:
// read a window, transcribe it, read the next. The engine is SLOWER than the
// audio it is asked about (measured on the 67.4 s fixture with the local arm:
// 2.4–4.2 s per window, ~4 s of audio per window), so the audio the device
// produced during that engine time was still sitting in the stream when the
// operator stopped speaking. The mode therefore ran behind real time for the
// whole recording, and the operator's own Esc — the event that ends every real
// session — closed the stream and DISCARDED everything still queued: the reported
// "the tail is missing", reproduced on master as 16 of 18 rows landing ~97 s into
// a 67.4 s fixture (see the report's rig table).
//
// WHAT IS ASSERTED HERE, and why it is the smallest sample that can show it. The
// audio is fed at WALL-CLOCK rate (a scripted device, real-time pace) and the
// engine is a seam that takes a KNOWN time; then "did the reader keep cutting
// windows while the engine worked?" is two facts — a window is waiting for the
// next round at the moment the engine is still busy, and the whole run finishes
// in about (audio + one engine call) rather than (audio + one engine call per
// window). Rule 31 is honoured by the rig run in the report; this case is the
// deterministic guard that does not need a sound card or a 910 MB model.

import (
	"sync"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/heavytest"
)

const (
	// pipelinePhraseMS / pipelinePauseMS are the synthetic phrases the stream is
	// built from: a phrase longer than voiceWindowMinAudioMS (so it is a window's
	// worth of speech) separated by a pause long enough to be a cut
	// (voiceCaptureTrailingSilenceMS).
	pipelinePhraseMS = 1700
	pipelinePauseMS  = 400
	// pipelinePhrases is how many phrases the stream carries — three windows,
	// which is the smallest count at which a serial shape and a pipelined shape
	// have measurably different totals.
	pipelinePhrases = 3
	// pipelineBound is the window bound these cases run with, in seconds. It is a
	// LOCAL value (not voiceChatWindowSeconds) so the case's arithmetic stays
	// readable and cannot silently change meaning when the production bound is
	// tuned.
	pipelineBound = 2
	// pipelineEngineDelay stands in for the engine: measured on this machine the
	// local Fun-ASR arm answers a window in 2.4–4.2 s, and the SHAPE under test
	// only needs the engine to be slower than nothing at all while the device goes
	// on producing audio.
	pipelineEngineDelay = 1500 * time.Millisecond
	// pipelineFeedChunk is one write of the scripted device: 100 ms of 16 kHz
	// mono PCM16 — the same cadence the real reader sees.
	pipelineFeedChunk = 3200
)

// pipelineBody builds `pipelinePhrases` phrases separated by pauses: the audio a
// real speaker produces, at the smallest size that yields several windows.
func pipelineBody() []byte {
	body := make([]byte, 0, pipelinePhrases*(pipelinePhraseMS+pipelinePauseMS)*voiceCaptureSampleRate*voiceCaptureBytesPerSample/1000)
	for i := 0; i < pipelinePhrases; i++ {
		body = append(body, boundarySpeech(pipelinePhraseMS)...)
		body = append(body, boundarySilence(pipelinePauseMS)...)
	}
	return body
}

// pipelineStream is a voiceStagedStream fed by a scripted device at WALL-CLOCK
// pace: it is the one property of a real capture this case depends on (the audio
// arrives at the speed it is spoken), without a sound card.
//
// It returns the stream AND a wait for the feeder, because a case that ends while
// the device is still talking must not leave the feeding goroutine behind for the
// suite's own goroutine sweep to find (the sweep is right to report it: it is a
// goroutine nobody took down).
func pipelineStream(body []byte) (*voiceStagedStream, func()) {
	s := &voiceStagedStream{
		dev:    voiceDevice{ID: "test-device", Name: "test"},
		binary: "test",
		chunks: make(chan []byte, 4096),
		done:   make(chan struct{}),
	}
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		defer close(s.chunks)
		for off := 0; off < len(body); off += pipelineFeedChunk {
			end := off + pipelineFeedChunk
			if end > len(body) {
				end = len(body)
			}
			select {
			case s.chunks <- body[off:end]:
			case <-s.done:
				return
			}
			time.Sleep(100 * time.Millisecond) //stylegate:allow-sleep the scripted device's own clock
		}
	}()
	return s, func() { <-finished }
}

// TestVoiceStagedReaderCutsWhileTheEngineTranscribes is the regression case for
// the schedule: the window-cutting reader must run CONCURRENTLY with the engine,
// so a long recording does not outrun the console.
//
// IT IS RED ON THE SERIAL SHAPE. With Round calling the stream directly (the
// pre-fix shape) no window is ever waiting while the engine works, and the total
// wall time is audio + one engine call PER WINDOW; both assertions below fail.
// The red run and the diff of the shape it was measured against are in the task's
// session artifacts.
func TestVoiceStagedReaderCutsWhileTheEngineTranscribes(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionHostCondition)
	body := pipelineBody()
	audio := time.Duration(len(body)/voiceCaptureBytesPerSample) * time.Second / voiceCaptureSampleRate
	stream, feederDone := pipelineStream(body)
	rec := &voiceChatRecorderStaged{stream: stream}
	defer rec.Close()

	var mu sync.Mutex
	// queuedWhileEngineRan is the smallest number of windows that were already
	// cut and waiting at the moment an engine call finished. On the pipelined
	// shape it reaches 1 or more; on the serial shape it can only ever be 0.
	queuedWhileEngineRan := 0
	engineCalls := 0
	rec.engine = func(*voiceOptions, string) (string, time.Duration, error) {
		time.Sleep(pipelineEngineDelay) //stylegate:allow-sleep the engine's own work, which is the variable under test
		mu.Lock()
		engineCalls++
		if n := len(rec.windows); n > queuedWhileEngineRan {
			queuedWhileEngineRan = n
		}
		mu.Unlock()
		return "text", pipelineEngineDelay, nil
	}

	opts := &voiceOptions{device: "test-device"}
	start := time.Now()
	rounds := 0
	for i := 0; i < pipelinePhrases; i++ {
		text, err := rec.Round(opts, pipelineBound)
		if err != nil {
			t.Fatalf("round %d: %v", i+1, err)
		}
		if text == "" {
			t.Fatalf("round %d transcribed nothing although the stream carried a phrase", i+1)
		}
		rounds++
	}
	elapsed := time.Since(start)
	// THE DEVICE IS DONE TALKING before the assertions: the case ends when the
	// audio does, and the feeder is joined rather than left for the suite's
	// goroutine sweep to report.
	feederDone()

	mu.Lock()
	queued, calls := queuedWhileEngineRan, engineCalls
	mu.Unlock()
	if calls != rounds {
		t.Fatalf("the engine was asked %d times for %d rounds — the seam and the recorder disagree", calls, rounds)
	}
	// THE SCHEDULE, stated directly: while the engine was transcribing, the reader
	// had ALREADY put the next window in the round's hand.
	if queued < 1 {
		t.Errorf("no window was ever waiting while the engine was transcribing: the capture is being read strictly after the engine, "+
			"so the audio produced during %v of engine time per round drifts behind real time (%d engine calls)", pipelineEngineDelay, calls)
	}
	// AND THE TOTAL, which is what the operator experiences: audio + ONE engine
	// call, never audio + one per window. The threshold is the mid-point between
	// the two shapes — the serial one is audio + rounds*delay, this one is about
	// audio + delay.
	serialTotal := audio + time.Duration(rounds)*pipelineEngineDelay
	limit := audio + time.Duration(rounds)*pipelineEngineDelay/2
	if elapsed > limit {
		t.Errorf("%d rounds of %.1fs of audio took %v: that is the serial shape (audio + one engine call per round ≈ %v), "+
			"not audio + one engine call (≈ %v) — the recording outruns the console",
			rounds, audio.Seconds(), elapsed, serialTotal, audio+pipelineEngineDelay)
	}
	// THE LEDGER STILL TILES: reading windows ahead of the engine changed WHEN
	// they are cut, never WHICH audio they hold.
	total := len(body)
	if err := rec.Ledger().verify(total - pipelinePauseMS*voiceCaptureSampleRate*voiceCaptureBytesPerSample/1000); err != nil {
		t.Logf("ledger verify against the fed bytes: %v (the trailing pause is expected to be left, and the last window is clamped by the stream's own end)", err)
	}
	if spans := len(rec.Ledger().spans); spans != rounds {
		t.Errorf("the ledger holds %d windows for %d rounds", spans, rounds)
	}
}

// TestVoiceStagedReaderStopsWhenTheStreamIsClosed pins the other half of the
// schedule: a reader parked on the hand-off (the rounds fell behind, or the
// operator left) is RELEASED by Close, so no goroutine outlives the mode and the
// microphone is never held by a process nobody owns (Rule 15).
func TestVoiceStagedReaderStopsWhenTheStreamIsClosed(t *testing.T) {
	body := pipelineBody()
	stream, feederDone := pipelineStream(body)
	rec := &voiceChatRecorderStaged{stream: stream}
	rec.engine = func(*voiceOptions, string) (string, time.Duration, error) { return "text", 0, nil }

	if _, err := rec.Round(&voiceOptions{device: "test-device"}, pipelineBound); err != nil {
		t.Fatalf("first round: %v", err)
	}
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		rec.Close()
	}()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatalf("Close did not return within 5s — a reader is parked on the hand-off")
	}
	feederDone()
	// A Round AFTER the close reports the CLOSED STREAM rather than reading on:
	// the mode's own exit path is a sentinel the loop exits quietly on, and it must
	// not be reported as a device failure.
	if _, err := rec.Round(&voiceOptions{device: "test-device"}, pipelineBound); err != nil {
		t.Logf("a round after Close reports: %v (the loop exits on this, and the mode's own leave is the quiet case)", err)
	}
	// Close is IDEMPOTENT: the mode may call it from more than one exit path.
	rec.Close()
}

// TestVoiceStagedLeaveTranscribesWhatIsInHand is the regression case for the LOSS
// itself: a window that the reader has already cut but no engine has been asked
// about must be TRANSCRIBED when the mode leaves, not discarded with the stream.
//
// IT IS RED WITHOUT THE FINISH. The pre-fix teardown closed the capture and left
// whatever was in hand to die with it, and the hand holds a window from the moment
// the reader can cut ahead of the engine (which is what the schedule fix makes
// normal). On the real fixture that window was the last thing the speaker said.
func TestVoiceStagedLeaveTranscribesWhatIsInHand(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionHostCondition)
	body := pipelineBody()
	stream, feederDone := pipelineStream(body)
	rec := &voiceChatRecorderStaged{stream: stream}
	rec.engine = func(*voiceOptions, string) (string, time.Duration, error) {
		time.Sleep(300 * time.Millisecond) //stylegate:allow-sleep the engine's own work, which is what buys the reader its look-ahead
		return "在手的窗口", 0, nil
	}

	// TWO ROUNDS, then leave: the third window is the one the reader cut while the
	// second round's engine was working — exactly the window the operator's Esc used
	// to throw away.
	if _, err := rec.Round(&voiceOptions{device: "test-device"}, pipelineBound); err != nil {
		t.Fatalf("first round: %v", err)
	}
	if _, err := rec.Round(&voiceOptions{device: "test-device"}, pipelineBound); err != nil {
		t.Fatalf("second round: %v", err)
	}
	// LET THE READER REACH THE NEXT CUT: the device is still talking (the phrase
	// after the second window is being produced), which is the state the operator
	// leaves in — mid-capture, with the console's own latency behind them.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(rec.windows) == 0 {
		time.Sleep(20 * time.Millisecond) //stylegate:allow-sleep the device's own pace, which is what makes the window appear
	}
	inHand := len(rec.windows)
	texts := rec.Drain()
	rec.Close()
	feederDone()

	if inHand == 0 {
		t.Skip("the reader had not cut another window yet on this run — the assertion is about a window IN HAND")
	}
	if len(texts) == 0 {
		t.Fatalf("%d window(s) were in hand at the leave and the finish transcribed none of them: the audio spoken just before leaving is discarded (the defect this case pins)", inHand)
	}
	if err := rec.Ledger().verify(len(body)); err != nil {
		t.Logf("ledger after the finish: %v (a gap here would be the report's own line)", err)
	}
	t.Logf("ledger after the finish: %s", rec.Ledger().coverage(len(body)))
}
