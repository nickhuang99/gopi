package main

// cli_voice_audio_source_test.go — THE SEAM'S REGRESSION CASE (task 01a11f13).
//
// WHAT IT PINS, in one sentence: the audio SOURCE is the only thing that changed,
// so a capture fed from a FILE and a capture fed from a PROCESS must produce the
// SAME windows (voiceWindowSpan sequence), the SAME bytes handed to the engine,
// and a ledger that tiles the stream exactly.
//
// WHY IT IS THE CASE THAT MATTERS. Before the seam, "the microphone" and "a
// recorder process" were the same statement inside openVoiceStagedStream, so the
// window/boundary machinery could not be exercised without a capture device and
// every claim about it was a claim about a sound card. With the source behind an
// interface, the recording is the input, and the property above is the statement
// that the production path and the file path are ONE path.
//
// THE RED PROOF IS TWO-SHAPED, deliberately:
//
//   - Delete cli_voice_audio_source.go and this file does not COMPILE
//     (`undefined: newVoiceFileSource`): the case cannot pass in a tree where the
//     file source was never written, which is what "the test would go red" means
//     for a new capability.
//   - Break the file source's DELIVERY (push the whole recording as one chunk)
//     and the equivalence case fails on the spans: the comparison binds the real
//     implementation, not the type's existence.
//
// THE WALL CLOCK IS DELIBERATELY OUT OF THE COMPARISON. Two independently paced
// sources cannot share a schedule at the millisecond, and the window plan's own
// bound is a wall-clock budget (a window that reaches its bound is EXTENDED, which
// the span reports) — so comparing a paced run with an unpaced one would compare
// two different inputs and prove nothing. The equivalence case therefore runs BOTH
// sources unpaced, with the SAME chunk geometry, which is what makes the claim
// about the seam falsifiable instead of timing-dependent. That the production file
// source really is paced is pinned by its OWN case (TestVoiceAudioSourceFilePaces
// Delivery), the smallest sample that can show it: one chunk per 100 ms of audio.

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/testutil"
)

const (
	// seamSpeechMS / seamPauseMS are the fixture's phrase and pause. The pause is
	// above voiceCaptureTrailingSilenceMS (so it can close a window) and the
	// phrase is above voiceWindowMinPauseSpeechMS (so the pause qualifies without
	// needing the trailing-run exemption) — the case states its audio rather than
	// depending on a recording.
	seamSpeechMS = voiceWindowMinPauseSpeechMS + 100
	seamPauseMS  = 4 * voiceCaptureTrailingSilenceMS

	// seamWindowSeconds is the bound these cases run with. LOCAL, not
	// voiceChatWindowSeconds: the case's arithmetic stays readable and cannot
	// change meaning when the production bound is tuned.
	seamWindowSeconds = 2

	// seamMaxWindows bounds the loop that reads windows out of a finite fixture:
	// the fixture's windows are known to be three, and a loop that did not
	// terminate would hang the suite instead of failing it.
	seamMaxWindows = 16

	// seamForeignRate / seamForeignRateLow are the two rates a case declares to be
	// REFUSED. They are the two a real operator produces: 44.1 kHz (a music
	// tool's default) and 8 kHz (a telephone recording).
	seamForeignRate    = 44100
	seamForeignRateLow = 8000

	// seamPaceFixtureMS is the tiny fixture the pacing case measures: short
	// enough that the case costs a fraction of a second.
	seamPaceFixtureMS = 400
)

// seamFixture builds the audio both sources are fed: three phrases separated by
// pauses, which is enough for the window rules to cut TWICE and to leave a tail
// for the end-of-stream flush.
func seamFixture() []byte {
	var out []byte
	for i := 0; i < 3; i++ {
		out = append(out, boundarySpeech(seamSpeechMS)...)
		if i < 2 {
			out = append(out, boundarySilence(seamPauseMS)...)
		}
	}
	return out
}

// seamScratchFile writes bytes into THIS run's scratch area and returns the path.
//
// IT IS NOT t.TempDir(): this repository puts every device-backed artefact of a
// test in the scratch tree, off the / partition, through the ONE helper that
// knows how (testutil.TestDeviceScratchDir — Rule 22/32). A fixture written to
// t.TempDir() would be a second, unreviewed answer to "where may a test write".
func seamScratchFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	dir := testutil.TestDeviceScratchDir(t, "voice-audio-source")
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// seamProcessSource stands in for the RECORDER: it delivers the same audio the
// file source is given, in the same chunk geometry, so the only difference
// between the two runs is where the bytes came from.
//
// IT IMPLEMENTS voiceAudioSource, which is the point: the case drives the seam,
// not a private copy of it. A second, test-only path for "the process shape"
// would compare two things that production never runs.
type seamProcessSource struct {
	pcm    []byte
	origin string
	chunks chan []byte
	done   chan struct{}
}

func newSeamProcessSource(pcm []byte) *seamProcessSource {
	return &seamProcessSource{pcm: pcm, origin: "a scripted recorder", chunks: make(chan []byte, voiceStagedQueueChunks), done: make(chan struct{})}
}

func (s *seamProcessSource) Open() error {
	go func() {
		defer close(s.chunks)
		for off := 0; off < len(s.pcm); off += voiceAudioSourceChunkBytes {
			end := off + voiceAudioSourceChunkBytes
			if end > len(s.pcm) {
				end = len(s.pcm)
			}
			select {
			case s.chunks <- s.pcm[off:end]:
			case <-s.done:
				return
			}
		}
	}()
	return nil
}

func (s *seamProcessSource) Chunks() chan []byte { return s.chunks }
func (s *seamProcessSource) Health() error       { return nil }

// Diagnostic is empty: this seam's source is a scripted feed with no process
// behind it (voiceAudioSource.Diagnostic).
func (s *seamProcessSource) Diagnostic() string { return "" }
func (s *seamProcessSource) Origin() string     { return s.origin }
func (s *seamProcessSource) Close() {
	select {
	case <-s.done:
	default:
		close(s.done)
	}
}

// seamRun drives ONE source through the window builder and returns the windows
// and the bytes each window's staged WAV carried (the exact audio the engine is
// handed — voiceStageCaptureAudio is the production staging call).
func seamRun(t *testing.T, src voiceAudioSource, total int) ([]voiceWindowSpan, [][]byte) {
	t.Helper()
	stream, err := newVoiceStagedStream(src)
	if err != nil {
		t.Fatalf("open the stream: %v", err)
	}
	defer stream.Close()

	var spans []voiceWindowSpan
	var staged [][]byte
	consumed := 0
	for i := 0; i < seamMaxWindows; i++ {
		audio, span, err := stream.Next(seamWindowSeconds)
		if err != nil {
			t.Fatalf("window %d: %v", i+1, err)
		}
		if span.End-span.Start != len(audio) {
			t.Fatalf("window %d: the span covers %d bytes but %d were returned", i+1, span.End-span.Start, len(audio))
		}
		path, cleanup, err := voiceStageCaptureAudio(voicePCM16ToWAV(audio))
		if err != nil {
			t.Fatalf("window %d: stage the audio: %v", i+1, err)
		}
		raw, rerr := os.ReadFile(path)
		cleanup()
		if rerr != nil {
			t.Fatalf("window %d: read the staged audio: %v", i+1, rerr)
		}
		staged = append(staged, raw)
		spans = append(spans, span)
		consumed += len(audio)
		if consumed >= total {
			break
		}
	}
	if consumed != total {
		t.Fatalf("the windows covered %d of the fixture's %d bytes — the capture did not reach the end of the stream", consumed, total)
	}
	return spans, staged
}

// TestVoiceAudioSourceFileAndProcessAgreeOnWindows is THE case: the same audio
// through the file source and through a process-shaped source produces one and
// the same window sequence, one and the same engine payload, and a ledger that
// tiles the stream exactly.
func TestVoiceAudioSourceFileAndProcessAgreeOnWindows(t *testing.T) {
	fixture := seamFixture()
	path := seamScratchFile(t, "agree.pcm", fixture)

	fileSpans, fileStaged := seamRun(t, newVoiceFileSource(path, false), len(fixture))
	procSpans, procStaged := seamRun(t, newSeamProcessSource(fixture), len(fixture))

	if len(fileSpans) < 2 {
		t.Fatalf("the fixture produced %d window(s) — the case cannot compare a window sequence it never cut", len(fileSpans))
	}
	if len(fileSpans) != len(procSpans) {
		t.Fatalf("the file source produced %d window(s) and the process source %d — the source changed the boundary", len(fileSpans), len(procSpans))
	}
	for i := range fileSpans {
		if fileSpans[i] != procSpans[i] {
			t.Errorf("window %d differs by source: file %+v, process %+v", i+1, fileSpans[i], procSpans[i])
		}
	}
	if len(fileStaged) != len(procStaged) {
		t.Fatalf("the file source staged %d window(s) and the process source %d", len(fileStaged), len(procStaged))
	}
	for i := range fileStaged {
		if !bytes.Equal(fileStaged[i], procStaged[i]) {
			t.Errorf("window %d: the engine was handed different audio (%d bytes vs %d)", i+1, len(fileStaged[i]), len(procStaged[i]))
		}
	}

	// THE LEDGER TILES THE STREAM: the windows' spans cover the capture with no
	// hole and no overlap, which is the property the boundary machinery exists
	// for — and it is checked here on the FILE source, whose audio is a recording
	// whose length the case knows.
	var ledger voiceWindowLedger
	for _, span := range fileSpans {
		ledger.add(span)
	}
	if err := ledger.verify(len(fixture)); err != nil {
		t.Errorf("the file source's windows do not tile its own audio: %v", err)
	}
}

// TestVoiceAudioSourceFilePacesDelivery is the PACING case, on the smallest
// sample that can show it: the file source hands over one chunk per 100 ms of
// audio it carries, so a run over a file has the SHAPE of a live capture instead
// of the shape of a file copy.
func TestVoiceAudioSourceFilePacesDelivery(t *testing.T) {
	ms := seamPaceFixtureMS
	audio := boundarySpeech(ms)
	path := seamScratchFile(t, "pace.pcm", audio)

	src := newVoiceFileSource(path, true)
	if err := src.Open(); err != nil {
		t.Fatalf("open the file source: %v", err)
	}
	defer src.Close()

	wantChunks := (len(audio) + voiceAudioSourceChunkBytes - 1) / voiceAudioSourceChunkBytes
	start := time.Now()
	var got int
	for chunk := range src.Chunks() {
		got += len(chunk)
		if len(chunk) > voiceAudioSourceChunkBytes {
			t.Errorf("a chunk carried %d bytes — more than the %d of audio one tick represents", len(chunk), voiceAudioSourceChunkBytes)
		}
	}
	elapsed := time.Since(start)
	if got != len(audio) {
		t.Fatalf("the source delivered %d of %d bytes", got, len(audio))
	}
	// THE FLOOR IS THE CLAIM: `wantChunks-1` intervals of 100 ms must have passed,
	// because every chunk but the first waits for the audio it represents. The
	// ceiling is loose on purpose (a loaded machine may stretch it) — what must
	// not happen is a burst, which is the shape a file source degrades to.
	minElapsed := time.Duration(wantChunks-1) * voiceAudioSourceChunkInterval
	if elapsed < minElapsed {
		t.Errorf("the whole %dms fixture arrived in %v — the pacing is not applied (want at least %v)", ms, elapsed, minElapsed)
	}
	if elapsed > 4*time.Duration(ms)*time.Millisecond {
		t.Errorf("a %dms fixture took %v to deliver — the pacing is far slower than real time", ms, elapsed)
	}
}

// TestVoiceAudioSourceUnpacedIsNotPaced is the other half of the pacing contract:
// a caller that turns the clock OFF gets the audio as fast as it can be read,
// which is what lets a case cover seconds of audio in a fraction of a second (and
// what the two equivalence runs rely on).
func TestVoiceAudioSourceUnpacedIsNotPaced(t *testing.T) {
	audio := boundarySpeech(seamPaceFixtureMS)
	path := seamScratchFile(t, "burst.pcm", audio)

	src := newVoiceFileSource(path, false)
	if err := src.Open(); err != nil {
		t.Fatalf("open the file source: %v", err)
	}
	defer src.Close()

	start := time.Now()
	var got int
	for chunk := range src.Chunks() {
		got += len(chunk)
	}
	if got != len(audio) {
		t.Fatalf("the source delivered %d of %d bytes", got, len(audio))
	}
	if elapsed := time.Since(start); elapsed >= time.Duration(seamPaceFixtureMS)*time.Millisecond {
		t.Errorf("the unpaced source took %v to deliver %dms of audio — the clock was not turned off", elapsed, seamPaceFixtureMS)
	}
}

// TestVoiceAudioSourceRefusesForeignGeometry pins the refusals BY NAME: a WAV
// whose container declares a geometry other than the capture contract is not a
// near miss (the run would read 44.1 kHz stereo bytes as 16 kHz mono), so it is
// refused with what it declared and what is required.
func TestVoiceAudioSourceRefusesForeignGeometry(t *testing.T) {
	fixture := seamFixture()

	cases := []struct {
		name string
		data []byte
		want string
	}{
		{
			name: "44.1kHz stereo",
			data: seamWAV(fixture, seamForeignRate, 2, voiceCaptureBitsPerSample),
			want: "44100",
		},
		{
			name: "8kHz mono",
			data: seamWAV(fixture, seamForeignRateLow, voiceCaptureChannels, voiceCaptureBitsPerSample),
			want: "8000",
		},
		{
			name: "16kHz mono 8-bit",
			data: seamWAV(fixture, voiceCaptureSampleRate, voiceCaptureChannels, 8),
			want: "8-bit",
		},
		{
			name: "float samples",
			data: seamWAVFormat(fixture, voiceWAVFormatFloat, voiceCaptureSampleRate, voiceCaptureChannels, voiceCaptureBitsPerSample),
			want: "format tag",
		},
		{
			name: "truncated container",
			data: []byte(voiceRIFFMagic + "\x00\x00\x00\x00" + voiceWAVEMagic + "fmt "),
			want: "truncated",
		},
		{
			name: "no samples",
			data: seamWAVNoData(fixture),
			want: voiceWAVDataMagic,
		},
		{
			name: "empty file",
			data: nil,
			want: "nothing to capture",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := seamScratchFile(t, strings.ReplaceAll(tc.name, "/", "-")+".wav", tc.data)
			src := newVoiceFileSource(path, false)
			err := src.Open()
			if err == nil {
				src.Close()
				t.Fatalf("%s was ACCEPTED as a capture — the run would transcribe garbage", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the refusal does not name %q: %v", tc.want, err)
			}
			if !strings.Contains(err.Error(), path) {
				t.Errorf("the refusal does not name the file it read (%s): %v", path, err)
			}
		})
	}
}

// TestVoiceAudioSourceAcceptsTheCaptureContract is the green half of the refusals:
// a WAV of the contract's own geometry is read, and its samples are exactly the
// payload the file carried.
func TestVoiceAudioSourceAcceptsTheCaptureContract(t *testing.T) {
	fixture := seamFixture()
	path := seamScratchFile(t, "contract.wav", seamWAV(fixture, voiceCaptureSampleRate, voiceCaptureChannels, voiceCaptureBitsPerSample))

	src := newVoiceFileSource(path, false)
	if err := src.Open(); err != nil {
		t.Fatalf("a %s WAV was refused: %v", voiceCaptureGeometryText(), err)
	}
	defer src.Close()
	if !bytes.Equal(src.pcm, fixture) {
		t.Fatalf("the source read %d byte(s) of samples, want the file's %d", len(src.pcm), len(fixture))
	}
}

// TestVoiceParseRefusesAudioSourceOnShapesThatCaptureNothing pins the parse-time
// contract of the flag: --audio-source replaces the MICROPHONE, so a shape that
// opens none refuses it by name instead of accepting a flag nothing reads.
func TestVoiceParseRefusesAudioSourceOnShapesThatCaptureNothing(t *testing.T) {
	path := seamScratchFile(t, "parse.pcm", boundarySpeech(seamSpeechMS))
	flag := argvFlagPrefix + voiceFlagAudioSource

	// The shapes that RECORD accept it — and they must, or the flag would be
	// unreachable and the case above vacuous.
	for _, args := range [][]string{
		{flag, path},
		{argvFlagPrefix + voiceFlagClient, flag, path},
		{argvFlagPrefix + voiceFlagClient, argvFlagPrefix + voiceFlagStdio, flag, path},
	} {
		opts, err := parseVoiceArgs(args)
		if err != nil {
			t.Errorf("%v was refused: %v", args, err)
			continue
		}
		if opts.audioSource != path {
			t.Errorf("%v parsed audioSource=%q, want %q", args, opts.audioSource, path)
		}
	}

	// The shapes that do not record refuse it, and the refusal names the shape.
	for _, args := range [][]string{
		{argvFlagPrefix + voiceFlagListDevices, flag, path},
		{argvFlagPrefix + voiceFlagServer, flag, path},
		{argvFlagPrefix + voiceFlagServer, argvFlagPrefix + voiceFlagStdio, flag, path},
	} {
		if _, err := parseVoiceArgs(args); err == nil {
			t.Errorf("%v was accepted — the flag would be read by nothing", args)
		} else if !strings.Contains(err.Error(), voiceFlagAudioSource) {
			t.Errorf("%v: the refusal does not name --%s: %v", args, voiceFlagAudioSource, err)
		}
	}

	// A pointer that cannot be read is knowable BEFORE anything is opened, and the
	// refusal says so rather than being discovered as an empty capture.
	if _, err := parseVoiceArgs([]string{argvFlagPrefix + voiceFlagClient, flag, filepath.Join(filepath.Dir(path), "absent.pcm")}); err == nil {
		t.Error("a --audio-source pointing at nothing was accepted")
	}
	if _, err := parseVoiceArgs([]string{argvFlagPrefix + voiceFlagClient, flag, filepath.Dir(path)}); err == nil {
		t.Error("a --audio-source naming a directory was accepted")
	}
	if _, err := parseVoiceArgs([]string{argvFlagPrefix + voiceFlagClient, flag, ""}); err == nil {
		t.Error("an empty --audio-source was accepted")
	}
}

// TestVoiceCapturePCMReadsOneBoundedBlock pins the reader the one-block shapes
// (the bare capture and --client) use: it stops at --seconds of audio, and it
// trims a source that overruns the bound.
func TestVoiceCapturePCMReadsOneBoundedBlock(t *testing.T) {
	audio := boundarySpeech(seamSpeechMS * 4)
	path := seamScratchFile(t, "block.pcm", audio)

	src := newVoiceFileSource(path, false)
	pcm, err := voiceCapturePCM(src, seamWindowSeconds)
	if err != nil {
		t.Fatalf("read one block: %v", err)
	}
	want := voiceBytesPerSeconds(seamWindowSeconds)
	if len(pcm) != want {
		t.Fatalf("a %ds bound produced %d byte(s), want %d", seamWindowSeconds, len(pcm), want)
	}
	if !bytes.Equal(pcm, audio[:want]) {
		t.Fatal("the bounded block is not the head of the fixture")
	}

	// A source SHORTER than the bound is read whole rather than refused: a
	// recording shorter than --seconds is audio, not an error.
	short := boundarySpeech(seamSpeechMS)
	shortPath := seamScratchFile(t, "short.pcm", short)
	got, err := voiceCapturePCM(newVoiceFileSource(shortPath, false), seamWindowSeconds)
	if err != nil {
		t.Fatalf("a source shorter than the bound was refused: %v", err)
	}
	if !bytes.Equal(got, short) {
		t.Fatalf("a source shorter than the bound produced %d byte(s), want %d", len(got), len(short))
	}

	// A source with nothing in it is a refusal, not an empty capture — and the
	// refusal comes from where the emptiness is knowable (the source's own Open),
	// naming the file and the contract it did not reach.
	emptyPath := seamScratchFile(t, "empty.pcm", nil)
	if _, err := voiceCapturePCM(newVoiceFileSource(emptyPath, false), seamWindowSeconds); err == nil {
		t.Error("an empty source produced a capture")
	} else if !strings.Contains(err.Error(), emptyPath) || !strings.Contains(err.Error(), "nothing to capture") {
		t.Errorf("the empty-source refusal does not name the file and the reason: %v", err)
	}
}

// seamWAV builds a canonical WAV of an arbitrary geometry around raw samples, so a
// case can state the container it wants refused (the production builder always
// writes the capture contract and cannot express a foreign one — see voiceBuildWAV).
func seamWAV(pcm []byte, rate, channels, bits int) []byte {
	return seamWAVFormat(pcm, voiceWAVFormatPCM, rate, channels, bits)
}

// seamWAVFormat is seamWAV with the container's format tag spelled out.
func seamWAVFormat(pcm []byte, formatTag uint16, rate, channels, bits int) []byte {
	header := make([]byte, voiceWAVFmtChunkEnd)
	copy(header[0:4], voiceRIFFMagic)
	binary.LittleEndian.PutUint32(header[4:], uint32(voiceWAVFmtChunkEnd-8+len(pcm)))
	copy(header[8:12], voiceWAVEMagic)
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:], 16)
	binary.LittleEndian.PutUint16(header[20:], formatTag)
	binary.LittleEndian.PutUint16(header[22:], uint16(channels))
	binary.LittleEndian.PutUint32(header[24:], uint32(rate))
	binary.LittleEndian.PutUint32(header[28:], uint32(rate*channels*bits/8))
	binary.LittleEndian.PutUint16(header[32:], uint16(channels*bits/8))
	binary.LittleEndian.PutUint16(header[34:], uint16(bits))
	data := make([]byte, 8, 8+len(pcm))
	copy(data[0:4], voiceWAVDataMagic)
	binary.LittleEndian.PutUint32(data[4:], uint32(len(pcm)))
	return append(append(header, data...), pcm...)
}

// seamWAVNoData builds a container of the RIGHT geometry that carries no samples.
func seamWAVNoData(pcm []byte) []byte {
	full := seamWAV(pcm, voiceCaptureSampleRate, voiceCaptureChannels, voiceCaptureBitsPerSample)
	return full[:voiceWAVFmtChunkEnd]
}
