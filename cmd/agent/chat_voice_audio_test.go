package main

// chat_voice_audio_test.go — THE CHAT ARM AND THE SEAM'S FILE ARM ARE ONE ARM
// (task 01a11f13's second child).
//
// WHAT IT PINS, in one sentence: the same audio file, driven through the CHAT
// surface's `/voice` recorder and through the audio-source SEAM directly, produces
// ONE AND THE SAME window sequence and hands the engine ONE AND THE SAME bytes.
//
// WHY IT IS THE CASE THAT MATTERS, and not a restatement of the seam's own
// equivalence case (cli_voice_audio_source_test.go) — which compares a file source
// with a PROCESS-shaped source. That case proves the seam is source-agnostic; it
// says nothing about the chat surface, whose two capture points used to spell
// "the production recorder" as a hardcoded `nil` (chat_voice_recorder_staged.go's
// Select, chat_voice_live.go's Live). A chat wiring that decoded, paced or
// validated the file ITSELF would pass every seam case and still be a second
// implementation — the drift Rule 14 forbids. So this case compares the chat arm
// against the seam's file arm, and the only way both can agree is if the chat arm
// goes THROUGH the seam.
//
// THE CLOCK IS REAL HERE, and that is deliberate: the production chat path paces a
// file at the rate it was spoken (voiceFileSource's own `rate=true`), because a
// burst would make every wall-clock boundary in the window plan fire at the wrong
// moment. Both arms are therefore run PACED, with the same chunk geometry, and the
// fixture's windows are all closed by CONTENT (a trailing pause run) rather than by
// the bound — which is what makes the comparison robust: a bound-driven window
// depends on when a timer fired, a content-driven one depends only on the bytes.
// The bound is the chat mode's own (voiceChatWindowSeconds), so the run is the one
// an operator gets, and the fixture's phrases are well inside it.
//
// THE FIXTURE IS A WAV OF THE CAPTURE CONTRACT, written by the production packer
// (voicePCM16ToWAV), so the case also covers the geometry check a real operator's
// file goes through: a WAV that is not 16 kHz mono s16le is refused BY NAME
// (voiceSourceWAVPCM), and the second half of this file drives that refusal
// through the chat recorder rather than through the seam alone.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"github.com/nick/gopi/internal/heavytest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/testutil"
)

const (
	// chatVoiceAudioMaxWindows bounds the loops below: the fixture is known to
	// produce a handful of windows, and a loop that did not terminate would hang
	// the suite instead of failing it.
	chatVoiceAudioMaxWindows = 16

	// chatVoiceAudioForeignRate is the rate the second half declares: a WAV whose
	// geometry is not the capture contract must be refused by name, and 44.1 kHz
	// is what a music tool produces.
	chatVoiceAudioForeignRate = 44100
)

// chatVoiceAudioFixture writes a WAV of the capture contract into this run's
// scratch area and returns its path together with the PCM it carries.
//
// IT IS NOT t.TempDir(): this repository puts every device-backed artefact of a
// test in the scratch tree, off the `/` partition, through the ONE helper that
// knows how (testutil.TestDeviceScratchDir — Rule 22/32).
func chatVoiceAudioFixture(t *testing.T, name string) (string, []byte) {
	t.Helper()
	pcm := seamFixture()
	dir := testutil.TestDeviceScratchDir(t, "chat-voice-audio")
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, voicePCM16ToWAV(pcm), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path, pcm
}

// chatVoiceAudioEngine is the recorder's engine seam under this case: it RECORDS
// what the engine was handed (the exact bytes the production path stages, read
// back from the file voiceStageCaptureAudio wrote) and answers with text, so the
// schedule and the payload are observable without a 910 MB model.
func chatVoiceAudioEngine(t *testing.T, staged *[][]byte) func(*voiceOptions, string) (string, time.Duration, error) {
	t.Helper()
	return func(opts *voiceOptions, audioPath string) (string, time.Duration, error) {
		raw, err := os.ReadFile(audioPath)
		if err != nil {
			return "", 0, err
		}
		if opts == nil || strings.TrimSpace(opts.audioSource) == "" {
			// THE OPTIONS MUST CARRY THE SOURCE: the engine dispatch answers a FILE
			// arm and a DEVICE arm differently, and a `/voice` run over a file that
			// reached the device arm would open the very capture device this run
			// deliberately never touched. The case states that as its own failure,
			// not as a silent difference in payload.
			t.Errorf("the engine was called with no audio source on its options — a file run would take the DEVICE arm of the dispatch")
		}
		*staged = append(*staged, raw)
		return "transcript", 0, nil
	}
}

// TestChatVoiceFileArmMatchesTheSeamFileArmWindows is THE case: one file, two
// arms (the chat surface's recorder and the seam itself), one window sequence.
func TestChatVoiceFileArmMatchesTheSeamFileArmWindows(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	path, pcm := chatVoiceAudioFixture(t, "chat-arm.wav")

	// ---- THE CHAT ARM: the production constructor, the production Select/Round.
	rec := chatVoiceRecorder(path)
	if !strings.Contains(rec.audioSource, "chat-arm.wav") {
		t.Fatalf("chatVoiceRecorder did not carry the audio source: %q", rec.audioSource)
	}
	desc, _, err := rec.Select("")
	if err != nil {
		t.Fatalf("the chat recorder refused a file it was named: %v", err)
	}
	if !strings.Contains(desc, "chat-arm.wav") || !strings.Contains(desc, chatVoiceAudioDeviceID) {
		t.Errorf("the entered row does not name the file capture: %q", desc)
	}
	if rec.dev == nil || rec.dev.ID != chatVoiceAudioDeviceID {
		t.Errorf("the chat recorder's device for a file run is %+v — want the %q statement", rec.dev, chatVoiceAudioDeviceID)
	}
	var chatStaged [][]byte
	rec.engine = chatVoiceAudioEngine(t, &chatStaged)
	opts := &voiceOptions{}
	chatSpans := make([]voiceWindowSpan, 0, chatVoiceAudioMaxWindows)
	for i := 0; i < chatVoiceAudioMaxWindows; i++ {
		if _, rerr := rec.Round(opts, voiceChatWindowSeconds); rerr != nil {
			// THE END OF THE FILE IS AN ORDINARY END OF CAPTURE, not a failure: the
			// sentinel is what both of the mode's readers already treat as "the
			// capture ended" (chat_voice.go's loop, the recorder's readerFailure).
			if errors.Is(rerr, errVoiceStreamClosed) {
				break
			}
			t.Fatalf("chat arm, round %d: %v", i+1, rerr)
		}
		chatSpans = append(chatSpans, rec.LastWindow())
	}
	rec.Close()

	// ---- THE SEAM ARM: the same file, the same bound, through the seam's own file
	// source and window builder. Nothing here is chat-side.
	stream, err := newVoiceStagedStream(newVoiceFileSource(path, true))
	if err != nil {
		t.Fatalf("open the seam's file arm: %v", err)
	}
	defer stream.Close()
	var seamStaged [][]byte
	seamSpans := make([]voiceWindowSpan, 0, chatVoiceAudioMaxWindows)
	consumed := 0
	for i := 0; i < chatVoiceAudioMaxWindows; i++ {
		audio, span, nerr := stream.Next(voiceChatWindowSeconds)
		if nerr != nil {
			if errors.Is(nerr, errVoiceStreamClosed) {
				break
			}
			t.Fatalf("seam arm, window %d: %v", i+1, nerr)
		}
		sp, cleanup, serr := voiceStageCaptureAudio(voicePCM16ToWAV(audio))
		if serr != nil {
			t.Fatalf("seam arm, window %d: stage the audio: %v", i+1, serr)
		}
		raw, rerr := os.ReadFile(sp)
		cleanup()
		if rerr != nil {
			t.Fatalf("seam arm, window %d: read the staged audio: %v", i+1, rerr)
		}
		seamStaged = append(seamStaged, raw)
		seamSpans = append(seamSpans, span)
		consumed += len(audio)
		if consumed >= len(pcm) {
			break
		}
	}

	// ---- THE COMPARISON: the chat arm must be the seam arm, window for window.
	if len(chatSpans) < 2 {
		t.Fatalf("the fixture produced %d chat window(s) — the case cannot compare a sequence it never cut", len(chatSpans))
	}
	if len(chatSpans) != len(seamSpans) {
		t.Fatalf("the chat arm cut %d window(s) and the seam arm %d — the chat surface changed the boundary:\n chat %+v\n seam %+v",
			len(chatSpans), len(seamSpans), chatSpans, seamSpans)
	}
	for i := range chatSpans {
		if chatSpans[i] != seamSpans[i] {
			t.Errorf("window %d differs between the chat arm and the seam arm: chat %+v, seam %+v", i+1, chatSpans[i], seamSpans[i])
		}
	}
	if len(chatStaged) != len(seamStaged) {
		t.Fatalf("the chat arm handed the engine %d window(s) and the seam arm %d", len(chatStaged), len(seamStaged))
	}
	for i := range chatStaged {
		if !bytes.Equal(chatStaged[i], seamStaged[i]) {
			t.Errorf("window %d: the engine was handed different audio (%d bytes vs %d)", i+1, len(chatStaged[i]), len(seamStaged[i]))
		}
	}

	// ---- THE LEDGER TILES THE FILE: the windows cover the capture with no hole and
	// no overlap, which is the property the whole window plan exists for.
	var ledger voiceWindowLedger
	for _, span := range chatSpans {
		ledger.add(span)
	}
	if verr := ledger.verify(len(pcm)); verr != nil {
		t.Errorf("the chat arm's windows do not tile the file they were read from: %v", verr)
	}
}

// TestChatVoiceRecorderRefusesAForeignFileByName is the REFUSAL half: the chat
// recorder over a WAV whose geometry is not the capture contract must fail BY NAME
// at Select — before a mode exists, before a row claims a capture — and the
// sentence must be the seam's own (the path, the geometry found, the geometry
// required). A chat-side decoder would answer this differently, which is the
// second thing this case pins.
func TestChatVoiceRecorderRefusesAForeignFileByName(t *testing.T) {
	dir := testutil.TestDeviceScratchDir(t, "chat-voice-audio")
	path := filepath.Join(dir, "music-44k.wav")
	foreign := voiceBuildWAV(boundarySpeech(300), voiceWAVFormatPCM, voiceCaptureBitsPerSample)
	// Re-declare the rate: 44.1 kHz is what a music tool writes, and it is the
	// geometry the capture contract does not accept.
	binary.LittleEndian.PutUint32(foreign[voiceWAVRateOffset:], chatVoiceAudioForeignRate)
	if err := os.WriteFile(path, foreign, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}

	rec := chatVoiceRecorder(path)
	desc, _, err := rec.Select("")
	if err == nil {
		t.Fatalf("the chat recorder accepted a %d Hz WAV as a capture (it described it as %q)", chatVoiceAudioForeignRate, desc)
	}
	// HONEST SCOPE, stated rather than implied: the refusal is raised when the
	// SOURCE IS OPENED — openVoiceStagedStream → voiceFileSource.Open — which is
	// what Select does, so Select is where the operator sees it. The sentence is
	// the seam's (voiceSourceWAVPCM), so it names the rate the file carries and the
	// geometry the capture contract requires.
	msg := err.Error()
	if !strings.Contains(msg, "44100") || !strings.Contains(msg, voiceCaptureGeometryText()) {
		t.Errorf("the refusal does not name what the file carries and what the contract requires: %v", err)
	}
	if !strings.Contains(msg, voiceFlagAudioSource) {
		t.Errorf("the refusal does not name the flag whose contract was broken: %v", err)
	}
	rec.Close()
}

// TestVoiceAudioSourceForToleratesNilOptions is the REGRESSION GUARD for a defect
// this task's own run measured (2026-10-09): the seam's production entry point
// dereferenced its options pointer, and `nil` is exactly what the chat surface's
// capture points passed — so entering `/voice` on a real machine panicked before
// this case existed. The red proof is the panic; the green proof is the recorder.
func TestVoiceAudioSourceForToleratesNilOptions(t *testing.T) {
	dev := voiceDevice{ID: "pulse:@DEFAULT_SOURCE@"}
	src, err := voiceAudioSourceFor(nil, &dev)
	if err != nil {
		t.Fatalf("nil options must mean \"no file\", not a refusal: %v", err)
	}
	if _, ok := src.(*voiceStagedRecorderSource); !ok {
		t.Fatalf("nil options with a device must select the production recorder, got %T", src)
	}
	// …and the empty-options spelling is the SAME statement, so a caller that
	// builds the options struct instead of passing nil cannot take a third path.
	src, err = voiceAudioSourceFor(&voiceOptions{}, &dev)
	if err != nil {
		t.Fatalf("empty options must mean \"no file\": %v", err)
	}
	if _, ok := src.(*voiceStagedRecorderSource); !ok {
		t.Fatalf("empty options with a device must select the production recorder, got %T", src)
	}
	// NO DEVICE AND NO FILE is still the refusal it always was.
	if _, err := voiceAudioSourceFor(nil, nil); err == nil {
		t.Errorf("no file and no device must be refused, not silently accepted")
	}
}

// TestChatVoiceAudioChannelsResolveWithTheFlagsPrecedence pins the ONE reader of
// the two channels (chatVoiceAudioPath): the flag is this run's statement and wins
// over an exported variable, and neither speaking means the production capture.
func TestChatVoiceAudioChannelsResolveWithTheFlagsPrecedence(t *testing.T) {
	t.Setenv(chatVoiceAudioEnv, "/tmp/from-env.wav")
	if got := chatVoiceAudioPath(""); got != "/tmp/from-env.wav" {
		t.Errorf("with no flag the environment must answer: %q", got)
	}
	if got := chatVoiceAudioPath("/tmp/from-flag.wav"); got != "/tmp/from-flag.wav" {
		t.Errorf("the flag must win over the environment: %q", got)
	}
	t.Setenv(chatVoiceAudioEnv, "")
	if got := chatVoiceAudioPath(""); got != "" {
		t.Errorf("with neither channel speaking the production capture is meant: %q", got)
	}
}

// TestChatVoiceAudioIsRefusedWhereTheModeCannotExist pins the two command lines
// that were given an audio source and have no `/voice` mode to read it: a FILM
// (a driver — the process that reads audio is the one inside the private
// terminal) and a one-shot run (no prompt loop, no editor). Both are refusals by
// name rather than silent no-ops, and both are answered by ONE function so the
// two shapes cannot describe one mistake two ways.
func TestChatVoiceAudioIsRefusedWhereTheModeCannotExist(t *testing.T) {
	if err := chatVoiceAudioRefusal("", true, true); err != nil {
		t.Errorf("a run with no audio source must not be refused: %v", err)
	}
	film := chatVoiceAudioRefusal("a.wav", true, false)
	if film == nil {
		t.Fatal("a film run with an audio source must be refused")
	}
	if !strings.Contains(film.Error(), chatVoiceAudioEnv) || !strings.Contains(film.Error(), chatFlagFilm) {
		t.Errorf("the film refusal must name the route that DOES work (the env channel) and the flag that made it a film: %v", film)
	}
	once := chatVoiceAudioRefusal("a.wav", false, true)
	if once == nil {
		t.Fatal("a one-shot run with an audio source must be refused")
	}
	if !strings.Contains(once.Error(), chatVoiceAudioFlag) || !strings.Contains(once.Error(), chatOnceFlag) {
		t.Errorf("the one-shot refusal must name the flag and the switch that removed the mode: %v", once)
	}
	if film.Error() == once.Error() {
		t.Errorf("the two shapes must not be answered by one sentence — the caller would not know which one applies")
	}
}

// TestChatParseVoiceAudioArgRefusesABareFlag pins the parse shape: both spellings
// take a value, and a bare flag is a usage error — never a silent fall back to the
// microphone, which on a host with none would look like a broken capture rather
// than a mistyped command line.
func TestChatParseVoiceAudioArgRefusesABareFlag(t *testing.T) {
	if path, consumed, err := chatParseVoiceAudioArg(chatVoiceAudioFlag, "a.wav", true); !consumed || err != nil || path != "a.wav" {
		t.Errorf("spaced form: path=%q consumed=%v err=%v", path, consumed, err)
	}
	if path, consumed, err := chatParseVoiceAudioArg(chatVoiceAudioFlag+chatFlagValueSep+"b.wav", "", false); !consumed || err != nil || path != "b.wav" {
		t.Errorf("inline form: path=%q consumed=%v err=%v", path, consumed, err)
	}
	if _, consumed, err := chatParseVoiceAudioArg(chatVoiceAudioFlag, "", false); !consumed || err == nil {
		t.Errorf("a bare %s must be a usage error (consumed=%v, err=%v)", chatVoiceAudioFlag, consumed, err)
	}
	if _, consumed, err := chatParseVoiceAudioArg(chatVoiceAudioFlag, "--tools", true); !consumed || err == nil {
		t.Errorf("a bare %s followed by another flag must be a usage error (consumed=%v, err=%v)", chatVoiceAudioFlag, consumed, err)
	}
	if _, consumed, err := chatParseVoiceAudioArg("--tools", "", false); consumed || err != nil {
		t.Errorf("an unrelated word must not be consumed (consumed=%v, err=%v)", consumed, err)
	}
}
