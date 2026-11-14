package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// The voice_input argument layer is what a user hits first, and every failure
// here should be a clear message rather than a stack of flag errors — so the
// parsing is tested independently of the engine (which needs 910MB of weights
// and 70s per call, far too slow for a unit test).

func TestVoiceParseFileMode(t *testing.T) {
	opts, err := parseVoiceArgs([]string{"clip.mp3"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.audio != "clip.mp3" {
		t.Errorf("audio = %q, want clip.mp3", opts.audio)
	}
	if opts.chunk != voiceDefaultChunkSec {
		t.Errorf("chunk = %d, want default %d", opts.chunk, voiceDefaultChunkSec)
	}
	if opts.server {
		t.Error("server should default to false")
	}
}

func TestVoiceParseChunk(t *testing.T) {
	opts, err := parseVoiceArgs([]string{"clip.wav", "--chunk", "30"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.chunk != 30 {
		t.Errorf("chunk = %d, want 30", opts.chunk)
	}
}

func TestVoiceParseRejectsBadChunk(t *testing.T) {
	// chunk=0 would mean "whole file in one window" — the exact configuration
	// measured to produce degenerate output and 8x slower runtime. Reject it
	// rather than let a typo silently pick the worst mode.
	for _, bad := range []string{"0", "-1", "abc", ""} {
		if _, err := parseVoiceArgs([]string{"clip.wav", "--chunk", bad}); err == nil {
			t.Errorf("--chunk %q should be rejected", bad)
		}
	}
}

func TestVoiceParseUnknownFlag(t *testing.T) {
	if _, err := parseVoiceArgs([]string{"clip.wav", "--nope", "x"}); err == nil {
		t.Error("unknown flag should be rejected")
	}
}

func TestVoiceParseMissingAudio(t *testing.T) {
	// The bare invocation is the CAPTURE default now (microphone → local
	// engine), so "no audio path" is no longer an error — it is the shape the
	// epic asks for, bounded by voiceLocalDefaultSeconds. What stays an error is
	// a shape that says "transcribe a file" without naming one, which is what
	// these two do: `--chunk N` only means something for a file.
	//
	// (The old contract — `parseVoiceArgs(nil)` returns "missing <audio-file>
	// argument" — is pinned by TestVoiceParseBareInvocationIsTheCaptureDefault
	// instead, which asserts what that invocation now DOES.)
	if _, err := parseVoiceArgs([]string{"--chunk", "15"}); err == nil {
		t.Error("--chunk without an audio file should be rejected — there is nothing to window")
	}
	if _, err := parseVoiceArgs([]string{"-"}); err != nil {
		t.Errorf("a positional is still the file shape, got %v", err)
	}
}

func TestVoiceParseServerStdio(t *testing.T) {
	opts, err := parseVoiceArgs([]string{"--server", "--stdio"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !opts.server || !opts.stdio {
		t.Errorf("server=%v stdio=%v, want both true", opts.server, opts.stdio)
	}
	if opts.audio != "" {
		t.Errorf("server mode needs no audio path, got %q", opts.audio)
	}
}

func TestVoiceParseServerHTTP(t *testing.T) {
	opts, err := parseVoiceArgs([]string{"--server", "--listen", ":9999"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.listen != ":9999" {
		t.Errorf("listen = %q, want :9999", opts.listen)
	}
}

func TestVoiceParseServerDefaultsListen(t *testing.T) {
	opts, err := parseVoiceArgs([]string{"--server"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.listen != voiceDefaultListen {
		t.Errorf("listen = %q, want default %q", opts.listen, voiceDefaultListen)
	}
}

// --stdio and --listen name two different transports. Silently preferring one
// would leave the user wondering why their server reads the wrong channel.
func TestVoiceParseRejectsStdioAndListen(t *testing.T) {
	_, err := parseVoiceArgs([]string{"--server", "--stdio", "--listen", ":7788"})
	if err == nil {
		t.Error("--stdio with --listen should be rejected")
	}
}

func TestVoiceModelPaths(t *testing.T) {
	enc, llm := voiceModelPaths("/models")
	if enc != "/models/"+voiceEncoderName {
		t.Errorf("encoder = %q, want %q", enc, "/models/"+voiceEncoderName)
	}
	if llm != "/models/"+voiceLLMName {
		t.Errorf("llm = %q, want %q", llm, "/models/"+voiceLLMName)
	}
}

func TestVoiceDepsReadyReportsMissing(t *testing.T) {
	dir := t.TempDir()
	// The local engine is named EXPLICITLY: the dependency walker's contract is
	// about the on-machine engine (0 weights are needed by the default cloud
	// one, which is the whole point of the flip), so a test that inherited the
	// default would be asserting the wrong engine's pre-flight.
	opts := &voiceOptions{
		engine:  config.AudioEngineLocal,
		runtime: filepath.Join(dir, "absent-binary"),
		models:  dir,
	}
	msg := voiceDepsReady(opts)
	if msg == "" {
		t.Fatal("expected a missing-dependency message")
	}
	if !strings.Contains(msg, "runtime") {
		t.Errorf("message should name the runtime, got %q", msg)
	}
}

// A raw f32 PCM stream carries no container signature, so it has to be
// wrapped or the decoder has nothing to sniff. The header has to be
// correct — a wrong byte rate makes the engine read at the wrong speed.
func TestVoiceWrapPCMAsWAV(t *testing.T) {
	pcm := make([]byte, 640) // 160 samples of f32
	wav := voiceWrapPCMAsWAV(pcm)

	if len(wav) != len(pcm)+44 {
		t.Fatalf("len = %d, want %d (44-byte header)", len(wav), len(pcm)+44)
	}
	if string(wav[0:4]) != "RIFF" {
		t.Errorf("missing RIFF magic, got %q", wav[0:4])
	}
	if string(wav[8:16]) != "WAVEfmt " {
		t.Errorf("bad fmt chunk, got %q", wav[8:16])
	}
	if string(wav[36:40]) != "data" {
		t.Errorf("missing data chunk, got %q", wav[36:40])
	}
	// format tag 3 = IEEE float, mono, 16 kHz
	if got := uint16(wav[20]) | uint16(wav[21])<<8; got != 3 {
		t.Errorf("format tag = %d, want 3 (IEEE float)", got)
	}
	if got := uint16(wav[22]) | uint16(wav[23])<<8; got != 1 {
		t.Errorf("channels = %d, want 1", got)
	}
	rate := uint32(wav[24]) | uint32(wav[25])<<8 | uint32(wav[26])<<16 | uint32(wav[27])<<24
	if rate != 16000 {
		t.Errorf("sample rate = %d, want 16000", rate)
	}
	// data length must equal the PCM we passed in
	dataLen := uint32(wav[40]) | uint32(wav[41])<<8 | uint32(wav[42])<<16 | uint32(wav[43])<<24
	if int(dataLen) != len(pcm) {
		t.Errorf("data length = %d, want %d", dataLen, len(pcm))
	}
	// the payload must survive untouched
	if string(wav[44:]) != string(pcm) {
		t.Error("PCM payload was modified")
	}
}

func TestVoiceMaterializeDetectsContainers(t *testing.T) {
	cases := map[string][]byte{
		"wav":  append([]byte("RIFF????WAVE"), make([]byte, 32)...),
		"mp3":  append([]byte("ID3\x03\x00"), make([]byte, 32)...),
		"ogg":  append([]byte("OggS\x00\x02"), make([]byte, 32)...),
		"flac": append([]byte("fLaC\x00\x00"), make([]byte, 32)...),
	}
	for name, raw := range cases {
		path, cleanup, err := voiceMaterializeAudio(raw)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		cleanup()
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s: cleanup did not remove %s", name, path)
		}
	}
}

func TestVoiceMaterializeWrapsRawPCM(t *testing.T) {
	// 16 kHz f32 silence: 640 bytes with no container magic at all.
	raw := make([]byte, 640)
	path, cleanup, err := voiceMaterializeAudio(raw)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(raw)+44 {
		t.Fatalf("len = %d, want %d — raw PCM should be wrapped in a WAV header",
			len(got), len(raw)+44)
	}
	if string(got[0:4]) != "RIFF" {
		t.Error("wrapped output is not RIFF")
	}
}

func TestVoiceReadAllRejectsOversize(t *testing.T) {
	big := strings.NewReader(strings.Repeat("x", 100))
	if _, err := voiceReadAll(big, 10); err == nil {
		t.Error("oversize input should be rejected")
	}
	small := strings.NewReader("hello")
	got, err := voiceReadAll(small, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("got %q", got)
	}
}

func TestVoiceLastLines(t *testing.T) {
	got := lastLines("a\nb\nc\nd\ne\n", 2)
	if got != "d | e" {
		t.Errorf("got %q, want %q", got, "d | e")
	}
	if got := lastLines("single", 3); got != "single" {
		t.Errorf("got %q", got)
	}
}
