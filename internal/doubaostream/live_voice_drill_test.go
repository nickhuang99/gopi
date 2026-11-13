// The LIVE drill: the real vendor endpoint, the real live-session code path, and
// a flow phase deliberately stretched past the budget the old reader armed.
//
// WHY IT EXISTS BESIDE THE UNIT REGRESSION. The unit tests prove the fix against
// a fake service on a loopback socket — deterministic, and the thing CI runs.
// What they cannot prove is the claim the bug report actually made: that a live
// `/voice` dictation survives a flow phase longer than the old 200 ms per-frame
// budget, against a service that really does answer asynchronously. This test
// makes that exact run: Open → Say every packet → sit quiet for several seconds
// (the old reader would have timed out ~20 times inside whatever frame happened
// to be in flight) → Finish → the transcript must arrive.
//
// IT IS OPT-IN, because it is a NETWORK test against a billed service with a
// credential on disk:
//
//	GOPI_LIVE_VOICE_DRILL=1 go test ./internal/doubaostream/ -run TestLiveVoiceDrill -v
//
// Optional paths (defaults are the ones this repo measured the bug with):
//
//	GOPI_LIVE_VOICE_CREDENTIAL  credential file (default <HOME>/.doubao/v2_voice.env)
//	GOPI_LIVE_VOICE_AUDIO       raw 16 kHz mono PCM16 (default the drill's own clip)
package doubaostream

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	// liveVoiceDrillEnv gates the drill; anything but "1" leaves it skipped.
	liveVoiceDrillEnv = "GOPI_LIVE_VOICE_DRILL"
	// liveVoiceCredentialEnv / liveVoiceAudioEnv override the two inputs.
	liveVoiceCredentialEnv = "GOPI_LIVE_VOICE_CREDENTIAL"
	liveVoiceAudioEnv      = "GOPI_LIVE_VOICE_AUDIO"
	// liveVoiceFlowStretch is how long the drill stays quiet AFTER the audio is
	// sent and BEFORE the end-of-stream flag. It is the whole point: several
	// seconds of an IDLE flow phase, which the pre-fix reader crossed with a
	// 200 ms deadline re-armed per read.
	liveVoiceFlowStretch = 3 * time.Second
	// liveVoiceExpectedText is the sentence the drill's clip says. Its absence
	// means the transcript was lost — which is what the desync did.
	liveVoiceExpectedText = "温度"
	// liveVoiceOpenTimeout / liveVoiceFinishTimeout bound the two waits.
	liveVoiceOpenTimeout   = 20 * time.Second
	liveVoiceFinishTimeout = 30 * time.Second
)

func TestLiveVoiceDrill(t *testing.T) {
	if os.Getenv(liveVoiceDrillEnv) != "1" {
		t.Skipf("set %s=1 to run the live dictation drill against the vendor endpoint", liveVoiceDrillEnv)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("home: %v", err)
	}
	credPath := firstNonEmpty(os.Getenv(liveVoiceCredentialEnv), DefaultCredentialPath(home))
	cred, err := LoadCredentials(credPath)
	if err != nil {
		t.Fatalf("credential: %v", err)
	}
	audioPath := firstNonEmpty(os.Getenv(liveVoiceAudioEnv),
		filepath.Join(home, ".gopi/sessions/01a11d7370a3730c84dabcc80db2985a/artifacts/speech16k.pcm"))
	pcm, err := os.ReadFile(audioPath)
	if err != nil {
		t.Fatalf("audio %q: %v", audioPath, err)
	}
	t.Logf("drill: %d bytes of PCM16 (%d ms of audio), credential %s", len(pcm), pcmDurationMS(len(pcm)), credPath)

	ctx, cancel := context.WithTimeout(context.Background(), liveVoiceOpenTimeout)
	defer cancel()
	sess, err := Open(ctx, Options{
		AppID: cred.AppID, Token: cred.Token,
		OnPartial: func(p Partial) { t.Logf("PARTIAL %d %q (definite=%v)", p.Index, p.Text, p.Definite) },
	})
	if err != nil {
		t.Fatalf("open the live session: %v", err)
	}
	defer func() { _ = sess.Close() }() //stylegate:ignore cleanup close of the drill's live session; the drill's own verdict is what the case reports
	t.Logf("session open: cluster=%s logid=%s", clusterOrDefault(""), sess.res.LogID)

	// The audio goes out at the cadence the capture would produce it.
	chunkBytes := chunkSampleCount(DefaultChunkMillis) * bytesPerSample16
	packetGap := time.Duration(DefaultChunkMillis) * time.Millisecond
	sent := 0
	for off := 0; off < len(pcm); off += chunkBytes {
		end := off + chunkBytes
		if end > len(pcm) {
			end = len(pcm)
		}
		started := time.Now()
		if err := sess.Say(pcm[off:end]); err != nil {
			t.Fatalf("Say at byte %d: %v", off, err)
		}
		sent++
		time.Sleep(packetGap - time.Since(started)) //stylegate:allow-sleep paces the frames to real time: the drill replays an utterance at speaking rate, so the gap IS the fixture
	}
	t.Logf("sent %d packets (%d ms of audio)", sent, pcmDurationMS(len(pcm)))

	// THE DRILL'S OWN STRETCH. Nothing is sent and nothing is expected: the
	// session is simply ALIVE and quiet, which is what a live dictation does
	// between an operator's words.
	t.Logf("flow phase: quiet for %v (the pre-fix reader armed a %v deadline per read)", liveVoiceFlowStretch, sessionFlowReadTimeout)
	time.Sleep(liveVoiceFlowStretch) //stylegate:allow-sleep the drill's quiet stretch: nothing is sent on purpose, which is the liveness condition under test

	done := make(chan struct{})
	var res *Result
	var ferr error
	go func() {
		defer close(done)
		res, ferr = sess.Finish(nil)
	}()
	select {
	case <-done:
	case <-time.After(liveVoiceFinishTimeout):
		t.Fatalf("Finish did not return within %v", liveVoiceFinishTimeout)
	}
	if res != nil {
		t.Logf("RESULT text=%q audio_ms=%d partials=%d elapsed=%v timed_out=%v",
			res.Text, res.AudioMS, sess.PartialCount(), res.Elapsed, sess.FinishTimedOut())
	}
	if ferr != nil {
		t.Fatalf("the live session failed: %v", ferr)
	}
	if strings.TrimSpace(res.Text) == "" {
		t.Fatalf("the live session produced no transcript (the audio was %d ms and the flow phase was quiet for %v)",
			pcmDurationMS(len(pcm)), liveVoiceFlowStretch)
	}
	if !strings.Contains(res.Text, liveVoiceExpectedText) {
		t.Fatalf("transcript = %q, want it to contain %q", res.Text, liveVoiceExpectedText)
	}
}
