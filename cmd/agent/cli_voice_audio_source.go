package main

// cli_voice_audio_source.go — WHERE THE CAPTURE'S BYTES COME FROM (task 01a11f13).
//
// THE SEAM. Until now, "the microphone" and "a running recorder process" were the
// same statement: `openVoiceStagedStream` spawned arecord/parecord itself, so a
// capture could only be produced by a sound device on this host. That is why the
// window/boundary machinery (voiceStagedStream.Next → voicePCM16ToWAV →
// voiceStageCaptureAudio → engine) could never be exercised end to end without a
// real mic, and why a machine with no capture device could not run any of it.
//
// This file separates the two: `voiceAudioSource` is the seam —
//
//	Open() ──▶ Chunks() ──▶ (the stream's reader) ──▶ windows ──▶ engine
//	               Close()
//
// — and the two implementations are the two places bytes can come from:
//
//   - voiceStagedRecorderSource: the recorder PROCESS. Its argv is byte-for-byte
//     what this codebase already spawned (voiceLiveRecorderCommand), which is the
//     point: the production capture must not change behaviour by being moved
//     behind an interface. Only the file's ownership of the process moved with
//     it.
//   - voiceFileSource: a PCM/WAV FILE, delivered at the rate the audio was
//     spoken. The contract is the capture contract (16 kHz mono s16le,
//     voiceCaptureSampleRate/Channels/BitsPerSample), and a container whose
//     geometry is anything else is REFUSED BY NAME — a 44.1 kHz file fed to a
//     16 kHz consumer is not an error anywhere downstream, it is garbage that
//     transcribes to nothing (Rule 15: say what is wrong with the input).
//
// THE RATE IS THE WHOLE TRICK, and it is why this is not just "read the file".
// A file source that delivered the whole recording in one burst would make every
// window boundary arrive late (the bound fires first, on a wall clock) and would
// change the SHAPE of the run rather than its audio: the source's arrival shape
// is what the window builder reacts to. Delivering `voiceAudioSourceChunkMS` of
// audio per `voiceAudioSourceChunkMS` of wall clock makes a file capture the same
// shape as a live one — 100 ms granularity, the same order as the reader's own
// 4096-byte reads (128 ms) — while a test can turn the pacing OFF (rate=false) to
// run a 60-second fixture as fast as the CPU allows.
//
// WHAT IS NOT HERE: the capture DEVICE. A file run enumerates no device, selects
// nothing and probes nothing, so `--audio-source` works on a host with no sound
// card at all (see voiceStreamForOptions). Chat-side wiring and streaming-arm
// convergence are the parent epic's other two children and are deliberately not
// touched here.

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nick/gopi/internal/tools"
)

const (
	// voiceCaptureGeometryLayout names the capture contract in the words every
	// refusal uses, so "which geometry is required" is answered in ONE place
	// instead of being spelled afresh at each refusal (Rule 13/14).
	voiceCaptureGeometryLayout = "%d Hz mono signed %d-bit little-endian"

	// voiceAudioSourceChunkMS is how much audio the file source delivers per
	// tick (and therefore how much wall clock that tick represents). 100 ms is
	// the same order as the live reader's own read size (voiceCaptureReadChunk,
	// 128 ms of audio) — fine enough that a window boundary lands within one
	// chunk of where a live capture would have put it, coarse enough that a
	// 6-second window costs 60 ticks rather than 6000.
	voiceAudioSourceChunkMS = 100

	// voiceStagedUnboundedWindowSeconds is the window bound a read with NO byte
	// bound cuts at: the tool's "read the whole source" shape does not read the
	// stream in one gulp, it reads it as consecutive windows like every other
	// reader — so the tail of a finite source is handed over by the stream's own
	// flush rather than being cut off at a byte count (voiceStagedWindows).
	//
	// IT IS LARGER THAN ANY UTTERANCE A PERSON PRODUCES and it does not bound the
	// result: the windows are stitched back together, so the caller receives the
	// whole source. What it bounds is a window that contains NO pause at all —
	// past it the cut is taken inside speech and the span says so (span.Split),
	// exactly as the console's own windows do.
	voiceStagedUnboundedWindowSeconds = 3600

	// The WAV layout the file source reads back. Named rather than inline
	// because each offset is a statement about the container's structure and a
	// bare 34 in a slice expression says nothing about which field it is
	// (Rule 1).
	voiceWAVHeaderProbeBytes = 12
	voiceWAVFmtChunkEnd      = 36
	voiceWAVFormatTagOffset  = 20
	voiceWAVChannelsOffset   = 22
	voiceWAVRateOffset       = 24
	voiceWAVBitsOffset       = 34
	// voiceWAVScanLimit bounds the walk for the data chunk: a container with
	// junk ahead of its samples (an extended LIST chunk, a metadata block)
	// still yields them, while a file that is not a WAV at all terminates.
	voiceWAVScanLimit = 4096

	// voiceStagedStderrDrainWait bounds how long a refusal waits for the
	// recorder's LAST diagnostic line after its stdout has ended. The two pipes
	// close together when the child exits (same process, same kernel), so this
	// bound is generous by orders of magnitude, and it is finite so a recorder
	// that keeps stderr open can never hang the sentence that reports it.
	voiceStagedStderrDrainWait = 2 * time.Second

	// The two container signatures, and the sample chunk's id.
	voiceRIFFMagic    = "RIFF"
	voiceWAVEMagic    = "WAVE"
	voiceWAVDataMagic = "data"
)

// The derived values: the chunk length and the tick it represents. Derived from
// the capture contract so neither can drift from it (Rule 13).
//
// THE MILLISECOND DIVISOR IS AN INTEGER CONSTANT, and it is a second spelling of
// 1000 on purpose: voiceMSPerSecond is FLOAT (1000.0), and a byte count derived
// through it is a floating-point constant — which turns every `off +
// voiceAudioSourceChunkBytes` in the delivery loop into float64 arithmetic that
// only fails at the call, not at the definition. The divisor is named here rather
// than borrowed so the arithmetic stays in the integers the offsets live in.
const (
	voiceAudioSourceMSPerSecond   = 1000
	voiceAudioSourceChunkBytes    = voiceCaptureSampleRate * voiceCaptureBytesPerSample * voiceAudioSourceChunkMS / voiceAudioSourceMSPerSecond
	voiceAudioSourceChunkInterval = time.Duration(voiceAudioSourceChunkMS) * time.Millisecond
)

// voiceAudioSource is ONE place audio comes from, as the window builder sees it:
// a stream of chunks, a verdict on how it ended, and a teardown.
//
// IT IS AN INTERFACE RATHER THAN A STRUCT WITH A FLAG because the two shapes
// differ in what they OWN, not in what they do: one owns a process and a pipe,
// the other a file and a clock. The window builder below them is identical, and
// that identity is the property this seam exists to make testable.
type voiceAudioSource interface {
	// Open acquires the audio: spawns the recorder, or reads and validates the
	// file. A failure here is reported to the caller — the source is not usable.
	Open() error
	// Chunks is the audio, in arrival order. It is CLOSED when the source ends,
	// which is the only end-of-stream signal the reader has.
	//
	// IT IS HANDED OVER WHOLE rather than as a receive-only view, because the
	// STREAM's own hand-off is the same channel (`voiceStagedStream.chunks`) and a
	// scripted test source supplies that field directly: narrowing the view here
	// would make the test shape a second, parallel channel and the two would drift.
	// The consumer's discipline is unchanged — the source is the only writer.
	Chunks() chan []byte
	// Health reports how the source ended: nil while it is running or when it
	// ended cleanly, errVoiceStreamClosed when it was CLOSED under the reader
	// (the mode leaving — not a failure), and the real error otherwise. It is
	// the ONE answer to "why did the chunks stop", so the stream's two failure
	// verdicts cannot disagree.
	Health() error
	// Origin names the source in the words a refusal prints: a recorder and its
	// device, or a file path. A message that said "the recorder" for a file run
	// would send the operator to the wrong cause.
	Origin() string
	// Diagnostic is the source's OWN account of why it ended, rendered for a
	// refusal that would otherwise report a silent capture — the recorder's exit
	// status and the tail of its stderr — or the empty string when the source has
	// nothing to add (a file source, a source still running, a source that ended
	// cleanly).
	//
	// WHY IT IS AN INTERFACE METHOD RATHER THAN A READER OF ONE IMPLEMENTATION's
	// FIELD: the refusal is built by the WINDOW BUILDER, which holds a
	// voiceAudioSource and nothing else. A type assertion in that builder would be
	// a second, silent definition of which sources can explain themselves — and it
	// would leave the production recorder's stderr, which the process layer really
	// does collect, invisible to the one sentence the operator reads.
	Diagnostic() string
	// Close releases the source. It is IDEMPOTENT and safe on a source whose
	// Open failed.
	Close()
}

// voiceStagedRecorderSource is the PRODUCTION source: one recorder process, read
// continuously. It is the body of what used to be openVoiceStagedStream + the
// stream's reader, moved behind the interface with the argv UNCHANGED.
type voiceStagedRecorderSource struct {
	dev    voiceDevice
	binary string
	cmd    *exec.Cmd
	stdout io.ReadCloser
	cancel context.CancelFunc
	stderr voiceStderrBuffer

	// chunks is the reader→builder hand-off. It is buffered to
	// voiceStagedQueueChunks and CLOSED by the reader when its read ends (EOF or
	// the context), which is the builder's only end-of-stream signal.
	chunks chan []byte
	// done is closed by Close, and it is what releases a reader parked on a full
	// queue. It is a channel rather than a timer because the reader allocates
	// nothing per chunk: one channel, read once per wait, closed once.
	done chan struct{}

	// mu guards readErr, closed and firstSample.
	mu sync.Mutex
	// readErr is why the reader stopped reading, when it did not stop because
	// the stream was closed.
	readErr error
	// readEnded is set by the reader when its read loop is over, for ANY reason:
	// the recorder's stdout reached EOF (it exited), or the read failed. It is a
	// separate fact from readErr — a recorder that closed its stdout cleanly sets
	// this and leaves readErr nil — and it is what Diagnostic needs to tell a
	// finished child's stderr from one still being written.
	readEnded bool
	// stderrEnded is set by the stderr drain when the child's stderr reached EOF,
	// i.e. the recorder is gone and everything it said has been READ.
	stderrEnded bool
	// stderrDrained is CLOSED by the stderr drain at the same moment stderrEnded
	// is set, so a reader that needs the child's last line can WAIT for it rather
	// than sample it. See Diagnostic: sampling it read an empty buffer in 53 of
	// 200 runs.
	stderrDrained chan struct{}
	// stderrPipe is the child's stderr READ END, owned here rather than by
	// os/exec. Why it is ours: exec's own copy goroutine is INVISIBLE to this
	// struct, so with `cmd.Stderr = &s.stderr` the source had no way to learn
	// that the child's last words had landed. stdout reaching EOF says the child
	// closed ITS STDOUT — a different pipe — so a Diagnostic taken at that
	// instant could read an empty buffer while the refusal text was still in
	// flight. MEASURED 2026-10-16: 53 of 200 consecutive runs of
	// TestVoiceStagedRecorderDiagnosticSpeaksThePinnedLocale took exactly that
	// window and surfaced no diagnostic for a recorder that had exited 1 with
	// stderr. Owning the pipe makes its own EOF the second half of "the child is
	// over" (readerStopped).
	stderrPipe io.ReadCloser
	// closed is set by Close BEFORE the process is signalled, so Health can tell
	// "the mode left" apart from "the device failed".
	closed bool
	// firstSample is the moment the source produced its first sample, and it is
	// what the OPEN deadline is measured against: a device that never delivers
	// must fail by name rather than block the mode forever.
	firstSample time.Time
}

// Open spawns the recorder and starts reading it.
//
// The argv is the SAME one the one-shot capture uses (voiceRecorderCommand), so
// the pinned capture contract and the recorder choice cannot diverge between the
// two shapes; the difference is only that no window length is asked of the
// process — the window is the builder's decision, not the recorder's.
func (s *voiceStagedRecorderSource) Open() error {
	binary, args := voiceLiveRecorderCommand(s.dev)
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binary, args...)
	// SAME GROUP POLICY AS THE ONE-SHOT PATH (voiceRecorderSysProcAttr): the
	// live recorder must not sit in the chat's own process group either, or the
	// exposure the one-shot path closes is simply moved to this one.
	cmd.SysProcAttr = voiceRecorderSysProcAttr()
	// THE MESSAGE LOCALE IS PINNED, from the ONE owner of that policy
	// (internal/tools.ChildLocaleEnv; Rule 2/14). This child's stderr is CLASSIFIED
	// — voiceStagedRecorderNote / voiceRecorderHint match the same English literals
	// the one-shot path does, two of which are glibc strerror messages
	// (`Broken pipe`, `Device or resource busy`, cli_voice_client.go) — and gettext
	// translates both. Measured on this host against the REAL recorder, 2026-10-12:
	//
	//	$ arecord -D plughw:99,0 … 2>&1 >/dev/null          # LANG=C
	//	arecord: main:831: audio open error: Invalid argument
	//	$ env -u LC_ALL LANG=zh_CN.UTF-8 arecord -D plughw:99,0 … 2>&1 >/dev/null
	//	arecord: main:831: 音乐打开错误： 无效的参数
	//
	// An inherited `cmd.Env` (the zero value, which is what this seam had) hands the
	// child the parent's message catalogues, so on a zh_CN host the operator's one
	// diagnostic is unusable while the same run under LANG=C is readable.
	//
	// STRIP-THEN-APPEND, never append-beside: with a duplicated entry the child's
	// language is decided by the READER, not by one rule — glibc's gettext path
	// and Go's os.Getenv take the FIRST copy while dash and bash take the LAST
	// (tools.ChildLocaleDuplicateKeyRule, internal/tools/git_child_locale.go;
	// measured 2026-10-16), and an env carrying TWO values for the same selector
	// decides the child's language by which of those happens to look. Removing the
	// inherited entries is the half that does not depend on either.
	cmd.Env = tools.ChildLocaleEnv()
	s.binary = binary
	s.cmd = cmd
	s.cancel = cancel
	s.chunks = make(chan []byte, voiceStagedQueueChunks)
	s.done = make(chan struct{})
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("cannot read %s's diagnostics from %q: %w", binary, s.dev.ID, err)
	}
	s.stderrPipe = stderrPipe
	s.stderrDrained = make(chan struct{})
	go s.drainStderr(stderrPipe)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("cannot read %s's audio from %q: %w", binary, s.dev.ID, err)
	}
	s.stdout = stdout
	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("%s failed on %q: %w", binary, s.dev.ID, err)
	}
	go s.read()
	return nil
}

// Chunks hands back the reader's hand-off channel.
func (s *voiceStagedRecorderSource) Chunks() chan []byte { return s.chunks }

// Diagnostic renders the recorder's own account of a capture that ended without
// audio (Rule 15 — the child's failure is READ, not swallowed).
//
// THE RESTRAINT IS THE DECISION, and it is two conditions, both necessary:
//
//  1. the READER HAS STOPPED. That is the same fact the stream's own health reads
//     — the recorder's stdout reached EOF (with stderr captured), which is what a
//     recorder that REFUSED TO OPEN its device does immediately, and it is also
//     what a recorder that died mid-capture does. A recorder that is still
//     running, and one whose window WE closed, have no note: the first has not
//     failed yet, and the second's stderr is the ordinary output of a working
//     microphone, which repeating at every paused round would be noise.
//  2. there is something to quote. A silent exit adds nothing to a sentence that
//     already says no audio arrived.
//
// THE TAIL, NOT THE WHOLE STREAM: stderr accumulates for the life of the mode, and
// the useful part of a refusal is the last lines. voiceStagedRecorderNote owns the
// rendering (and reuses the hint builder the one-shot path uses, so the same cause
// reads the same way whether it was found by `voice_input` or by `/voice`).
//
// THE LOCALE IS PART OF THE CONTRACT, NOT COSMETIC. What this method quotes is a
// CHILD's diagnostic, and the strings the classifier matches are glibc strerror
// messages gettext translates. Measured on this host, 2026-10-12, against the REAL
// recorder, the same absent device:
//
//	$ arecord -D plughw:99,0 … -q 2>&1 >/dev/null            # LANG=C
//	arecord: main:831: audio open error: Invalid argument
//	$ env -u LC_ALL LANG=zh_CN.UTF-8 arecord -D plughw:99,0 … -q 2>&1 >/dev/null
//	arecord: main:831: 音乐打开错误： 无效的参数
//
// — which is why Open hands this child the environment from
// internal/tools.ChildLocaleEnv (Rule 14: ONE spelling of strip-then-append), and
// why this file carries the pin and the reader that quotes it in one commit: a
// diagnostic we print is only readable if the child that wrote it spoke a language
// we pinned.
func (s *voiceStagedRecorderSource) Diagnostic() string {
	s.mu.Lock()
	closed := s.closed
	readErr := s.readErr
	s.mu.Unlock()
	if closed || readErr != nil {
		// Closed under us (the mode left — not a failure), or the read failed for
		// a reason the stream's OWN health already reports: neither is a recorder
		// diagnostic, and appending one would be a second, louder verdict.
		return ""
	}
	// THE CHILD'S LAST LINE IS WAITED FOR, not sampled. stdout reaching EOF says
	// the recorder closed ITS STDOUT — a different pipe from the one its
	// diagnostics travel on — so a Diagnostic read at that instant can find an
	// EMPTY buffer while the child's explanation is still in flight through
	// exec's copy. MEASURED 2026-10-16: 53 of 200 runs of the behavioural arm
	// failed exactly that way (empty diagnostic for a recorder that had exited 1
	// with stderr), and a flake is a defect in the code, not in the clock.
	//
	// The wait is bounded because a refusal is synchronous and must not hang on a
	// recorder that never closes its stderr: past the bound the buffer is quoted
	// as it stands, which is the honest answer, not an invented one.
	if s.stderrDrained != nil {
		select {
		case <-s.stderrDrained:
		case <-time.After(voiceStagedStderrDrainWait):
		}
	}
	if !s.readerStopped() {
		return ""
	}
	return voiceStagedRecorderNote(s.stderr.String())
}

// readerStopped reports whether the recorder is OVER — both of its pipes have
// reached EOF: the reader has finished with stdout, and the stderr drain has read
// the child's last line. BOTH halves are required, and the stderr half is the one
// that was missing: so long as the stderr copy is in flight, the buffer can be
// empty while the child has already said why it refused. MEASURED 2026-10-16: an
// arm that took Diagnostic the moment stdout hit EOF read "" in 53 of 200 runs.
func (s *voiceStagedRecorderSource) readerStopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readEnded && s.stderrEnded
}

// drainStderr is the stderr reader goroutine: it copies the child's diagnostics
// into s.stderr and, on EOF, stamps the half of "the child is over" that stdout
// cannot speak for. It is the ONLY reader of that pipe (os/exec's own copy is not
// used — cmd.Stderr stays nil), so its EOF is a fact about the child, not a race
// with a copy goroutine this struct cannot see.
func (s *voiceStagedRecorderSource) drainStderr(pipe io.ReadCloser) {
	buf := make([]byte, voiceCaptureReadChunk)
	for {
		read, err := pipe.Read(buf)
		if read > 0 {
			_, _ = s.stderr.Write(buf[:read]) //stylegate:ignore the buffer has no failing path; the read error below is the one that decides the outcome
		}
		if err != nil {
			// Done reading (EOF on the child's exit, or a read error — either
			// way nothing more will arrive). Unblock the reader: a stderr pipe
			// whose child is gone must not hold the mode open.
			_ = pipe.Close() //stylegate:ignore the child is gone; the close releases the descriptor and cannot report anything a refusal would print
			s.mu.Lock()
			s.stderrEnded = true
			s.mu.Unlock()
			close(s.stderrDrained)
			return
		}
	}
}

// voiceStderrBuffer is the recorder's captured stderr, and it is a TYPE rather
// than a bytes.Buffer because TWO goroutines meet on it: os/exec's copy goroutine
// writes it for as long as the child lives, while a refusal on the caller's
// goroutine READS it. An unsynchronised bytes.Buffer there is a data race (and a
// torn read in the sentence the operator is handed), which is exactly the kind of
// failure that only shows up under -race on a host whose recorder is fast.
type voiceStderrBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// Write appends the child's output (the io.Writer os/exec copies into).
func (b *voiceStderrBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// String returns what the recorder has said so far.
func (b *voiceStderrBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// Origin names the recorder and the device it was told to open.
func (s *voiceStagedRecorderSource) Origin() string {
	return fmt.Sprintf("%s from %q", s.binary, s.dev.ID)
}

// read is the reader goroutine: it consumes the recorder's stdout as fast as the
// device produces it, for the whole life of the mode. It never stops for a round
// — that is the point of the staged shape — and it is the ONLY reader of the pipe.
func (s *voiceStagedRecorderSource) read() {
	defer close(s.chunks)
	defer func() {
		s.mu.Lock()
		s.readEnded = true
		s.mu.Unlock()
	}()
	buf := make([]byte, voiceCaptureReadChunk)
	for {
		read, err := s.stdout.Read(buf)
		if read > 0 {
			chunk := make([]byte, read)
			copy(chunk, buf[:read])
			s.noteFirstSample()
			select {
			case s.chunks <- chunk:
			case <-s.done:
				// NOBODY IS TAKING THE AUDIO and the queue is full: the mode has
				// left (Close closes this channel first). The read ends here
				// rather than parking forever on a channel no one will drain.
				s.recordReadErr(errVoiceStreamClosed)
				return
			}
		}
		if err != nil {
			if err != io.EOF {
				s.recordReadErr(err)
			}
			return
		}
	}
}

// firstSampleTime reports the moment the source produced its first sample, or
// the zero time while it has produced none.
func (s *voiceStagedRecorderSource) firstSampleTime() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.firstSample
}

// noteFirstSample stamps the moment the device first produced audio (once).
func (s *voiceStagedRecorderSource) noteFirstSample() {
	s.mu.Lock()
	if s.firstSample.IsZero() {
		s.firstSample = time.Now()
	}
	s.mu.Unlock()
}

// recordReadErr remembers why the read ended, unless the stream was closed.
func (s *voiceStagedRecorderSource) recordReadErr(err error) {
	s.mu.Lock()
	if !s.closed && s.readErr == nil {
		s.readErr = err
	}
	s.mu.Unlock()
}

// Health reports how the read ended: the mode leaving (quiet), a real read
// failure, or nil while it is still running.
func (s *voiceStagedRecorderSource) Health() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errVoiceStreamClosed
	}
	return s.readErr
}

// Close stops the source: the reader is released, the process is signalled and
// the recorder is reaped. IT IS IDEMPOTENT and it is the ONLY teardown path, so a
// mode that is left with a round in flight cannot leave the microphone held by a
// process nobody owns (Rule 15).
func (s *voiceStagedRecorderSource) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	if s.done != nil {
		close(s.done)
	}

	// The reader is unblocked FIRST (it may be parked on a full queue), then the
	// recorder is stopped by the same helper the one-shot path uses — so the
	// classification of "we closed its window" and the reap-or-refuse decision
	// stay ONE implementation.
	if s.stderrPipe != nil {
		// The child is stopped below; closing the read end here ALSO releases a
		// drain parked on a stderr pipe whose writer is gone, so no goroutine
		// outlives the source (Rule 15: no silent leak).
		_ = s.stderrPipe.Close() //stylegate:ignore the descriptor is released as part of teardown; a failure here has no reader to report to and the child's own outcome is what the caller sees
	}
	if s.cancel != nil {
		s.cancel()
	}
	if s.cmd != nil {
		_ = stopVoiceRecorder(s.cmd, s.stdout) //stylegate:ignore stopVoiceRecorder already reports its own failure through the recorder's classification; the source's Close has no second channel to report on
	}
}

// voiceFileSource is the FILE source: a PCM/WAV recording of the capture
// contract, delivered at the rate it was spoken.
type voiceFileSource struct {
	// path is the file the audio was read from — named in every message, because
	// "which source failed" is the first thing an operator needs.
	path string
	// pcm is the payload: the file's samples, decoded to the capture contract
	// and with the container stripped.
	pcm []byte
	// rate says whether the samples are paced to real time (production) or
	// handed over as fast as the reader takes them (a test turning the clock
	// off so a long fixture runs in a second). It is OFF in no production path.
	rate bool

	chunks chan []byte
	done   chan struct{}
}

// newVoiceFileSource builds a file source for path. `rate` paces the delivery to
// real time; a caller that wants the same SHAPE as a live capture keeps it true.
func newVoiceFileSource(path string, rate bool) *voiceFileSource {
	return &voiceFileSource{
		path:   path,
		rate:   rate,
		chunks: make(chan []byte, voiceStagedQueueChunks),
		done:   make(chan struct{}),
	}
}

// Open reads and validates the file, then starts delivering it.
//
// EVERY REFUSAL HERE IS BY NAME: the path, the geometry the file actually
// carries, and the geometry the capture contract requires. A file the decoder
// cannot use is a mistake worth one sentence, not a silent run of garbage.
func (s *voiceFileSource) Open() error {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return voiceAudioSourceOpenRefusal(s.path, err)
	}
	pcm, err := voiceSourcePCM(s.path, raw)
	if err != nil {
		return err
	}
	if len(pcm) < voiceMinCaptureBytes {
		return fmt.Errorf("--%s %s carries %d byte(s) — less than one %s sample, so there is nothing to capture",
			voiceFlagAudioSource, s.path, len(pcm), voiceCaptureGeometryText())
	}
	s.pcm = pcm
	go s.pump()
	return nil
}

// pump delivers the samples in chunks. The FIRST chunk goes out immediately (a
// source that slept before its first byte would pay an opening delay a live
// device does not), and every later one is separated by exactly the wall clock
// its audio represents — so the byte stream's SHAPE, not just its content, is
// the one a microphone produces.
func (s *voiceFileSource) pump() {
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
		if !s.rate {
			continue
		}
		select {
		case <-time.After(voiceAudioSourceChunkInterval):
		case <-s.done:
			return
		}
	}
}

// Chunks hands back the file's hand-off channel.
func (s *voiceFileSource) Chunks() chan []byte { return s.chunks }

// Health is nil: a file that was validated at Open cannot fail mid-read, and the
// end of the file is an ordinary end of stream, not a failure.
func (s *voiceFileSource) Health() error { return nil }

// Origin names the file, so a refusal about the audio says where it came from.
func (s *voiceFileSource) Origin() string { return fmt.Sprintf("the file %s", s.path) }

// Diagnostic is empty: a file that has no process behind it has no second account
// of why it ended. The refusals it CAN produce name the file, the geometry it
// carries and the geometry the capture contract requires, and those are built
// where the file is read — rendering them again here would be a second spelling
// of the same sentence.
func (s *voiceFileSource) Diagnostic() string { return "" }

// Close releases the pump. The samples are already in memory, so there is nothing
// to reap — but the channel must still be unblocked, or a pump parked on a full
// hand-off would outlive the run.
func (s *voiceFileSource) Close() {
	if s == nil || s.done == nil {
		return
	}
	select {
	case <-s.done:
	default:
		close(s.done)
	}
}

// voiceCapturePCM reads ONE bounded capture out of a source: the first `seconds`
// of its audio, or the whole source when `seconds` is not positive.
//
// IT IS THE TOOL'S READ SHAPE OVER THE WINDOW SCHEDULE, and that is what makes it
// the same reading as the console's: the windows are cut by voiceStagedWindows /
// voiceStagedStream.Next — the plan, the carry and the ledger the `/voice` loop
// uses — and this function only decides WHEN to stop asking (at the caller's byte
// bound) and how much of the last window the flag asked for (voiceTrimToSeconds).
// Before the convergence this function drained the source into one buffer itself,
// which is why the tool's own audio had no ledger and why "where does the audio
// end" was answered twice in one package.
//
// ONE NUMBER, ONE MEANING. For a live source the first sample's arrival is the
// device opening, and `seconds` bounds the audio; for a file the first sample is
// already there. Both are therefore read the same way here — the bound is on the
// AUDIO, not on a wall clock — and the only difference is the OPEN deadline, which
// a file never needs (its samples exist before the run starts).
func voiceCapturePCM(source voiceAudioSource, seconds int) ([]byte, error) {
	stream, err := newVoiceStagedStream(source)
	if err != nil {
		return nil, err
	}
	defer stream.Close()

	// A NON-POSITIVE BOUND IS "THE WHOLE SOURCE", and it still goes through the
	// schedule: the windows are cut at the console's own bound and then stitched,
	// so the end of the audio is the STREAM's own verdict (the flush that hands
	// over the tail) rather than a byte count decided here.
	bound := seconds
	if bound <= 0 {
		bound = voiceStagedUnboundedWindowSeconds
	}
	windows := newVoiceStagedWindows(stream, bound)
	want := 0
	if seconds > 0 {
		want = voiceBytesPerSeconds(seconds)
	}
	pcm, err := windows.ReadBounded(want)
	if err != nil && !isVoiceStreamEnd(err) {
		return nil, fmt.Errorf("%s: %w", source.Origin(), err)
	}
	if len(pcm) < voiceMinCaptureBytes {
		return nil, fmt.Errorf("%s produced no audio — less than one %s sample: the file is empty, or the device is reachable but silent (check the capture level is not muted)",
			source.Origin(), voiceCaptureGeometryText())
	}
	return voiceTrimToSeconds(pcm, seconds), nil
}

// voiceCaptureGeometryText names the capture contract as one phrase (built from
// the constants, so it cannot describe a geometry the capture does not use).
func voiceCaptureGeometryText() string {
	return fmt.Sprintf(voiceCaptureGeometryLayout, voiceCaptureSampleRate, voiceCaptureBitsPerSample)
}

// voiceSourcePCM returns the raw PCM of a source file: the WAV's sample payload
// when it is a WAV, and the file's own bytes when it is bare PCM.
//
// BARE PCM HAS NO HEADER TO CHECK, and that is stated rather than hidden: the
// command's contract is that a container-less file IS the capture contract (the
// same assumption `--server --stdio` makes about its stdin), so its geometry is
// taken as given. A WAV is the case where the file carries its own claim, and
// there the claim is CHECKED.
func voiceSourcePCM(path string, raw []byte) ([]byte, error) {
	if len(raw) >= voiceWAVHeaderProbeBytes &&
		string(raw[0:4]) == voiceRIFFMagic && string(raw[8:12]) == voiceWAVEMagic {
		return voiceSourceWAVPCM(path, raw)
	}
	return raw, nil
}

// THE SENTENCES AN OPEN FAILURE IS ANSWERED WITH (task 01a1222d).
//
// WHY THEY ARE CONSTANTS AND WHY THEY EXIST AT ALL. The refusal that prompted
// this was correct and useless at the same time: a caller spelled the session id
// of a path with its dashes removed, the dashed directory exists and the
// de-dashed one does not, and the run answered
//
//	--audio-source /home/nick/.gopi/sessions/<de-dashed>/artifacts/probe.wav:
//	open …: no such file or directory
//
// — a sentence true of EVERY component at once. Two readers followed it into a
// mechanism that did not exist (a wrapper script blamed for an ENOENT it did not
// cause; the task's report records the falsification). The path is not the
// defect: the sentence is. Naming WHICH component is absent answers the question
// the message exists to answer, and the near-miss hint names the directory the
// caller probably meant, which is the one line that would have ended that
// investigation immediately.
//
// These are NOT a resolution layer (Rule 35): nothing is retried against the
// near-miss, nothing is rewritten or normalised, and a path whose file exists
// never reaches them. It is wording — one reading of the path as given, and one
// directory listing of the deepest part of it that DOES exist — and it lives here
// so the two seams that refuse the same mistake, voiceFileSource.Open (the path
// the CHAT surface reaches) and voiceValidateAudioSource (the tool surface's
// parse-time check), cannot spell it two ways (Rule 13/14).
const (
	// voiceAudioSourceDirMissingNote names the DIRECTORY as the absent component.
	// The directory named is the FIRST component of the path that is not there,
	// which is the one the caller has to fix.
	voiceAudioSourceDirMissingNote = "the DIRECTORY in that path does not exist"
	// voiceAudioSourceFileMissingNote is the other half: every directory on the
	// path is there and only the last name is absent, so the file is what the
	// operator has to write or rename.
	voiceAudioSourceFileMissingNote = "every directory on that path exists — the FILE is what is missing"
	// voiceAudioSourceNearMissNote offers the sibling that does exist. A hint,
	// never a substitution: the run still refuses. It carries its own lead-in so
	// the hint reads as one clause after the cause instead of ending in "?.".
	voiceAudioSourceNearMissNote = "— did you mean"
	// voiceAudioSourceNearMissFloor is the shortest shared head that counts as a
	// near miss. 8 characters is long enough that an unrelated sibling of a busy
	// parent directory is not offered (session ids share their first 8 hex
	// characters only by accident), short enough to catch a component whose tail
	// was mistyped.
	voiceAudioSourceNearMissFloor = 8
	// voiceAudioSourceNearMissDropped are the characters a caller loses when a
	// spelled id sheds its punctuation: the measured spelling removed the dashes
	// of a session id, and the same accident drops underscores and dots.
	voiceAudioSourceNearMissDropped = "-_."
)

// voiceAudioSourceOpenRefusal builds the refusal for a path that could not be
// opened: it names the component that is missing and, when one exists, a
// near-miss sibling of that component.
//
// ONE DEFINITION for both seams (Rule 13/14). The cause is WRAPPED rather than
// flattened, so a caller branching on `errors.Is(err, fs.ErrNotExist)` keeps
// working (Rule 16).
func voiceAudioSourceOpenRefusal(path string, cause error) error {
	missing, ancestor := voiceAudioSourceFirstMissing(path)
	if missing == "" || ancestor == "" {
		// The path validates and the failure came from somewhere else (permissions,
		// a vanished mount): the sentence must not blame a component that IS there.
		// It quotes the path and the cause — the whole of what is known.
		return fmt.Errorf("--%s %s: %w", voiceFlagAudioSource, path, cause)
	}
	// WHICH COMPONENT IS ABSENT is decided by the missing component's POSITION,
	// not by its kind: only the path's LAST name can be "the file", every earlier
	// one is a directory on the way to it. (Deciding it from "its parent happens
	// to be the deepest directory that exists" mislabels a missing directory that
	// sits directly under an existing one — observed on the real pty.)
	sentence := fmt.Sprintf("%s (%s)", voiceAudioSourceDirMissingNote, missing)
	if missing == filepath.Join(ancestor, filepath.Base(path)) {
		sentence = fmt.Sprintf("%s (%s)", voiceAudioSourceFileMissingNote, ancestor)
	}
	// PUNCTUATION IS SOMEBODY ELSE'S: the chat surface appends its own note (and
	// its own period) after this text, so the hint ends UNPUNCTUATED — a "?" here
	// renders as "?. Nothing was recorded…" (measured on the first real-pty run
	// of this change).
	if near := voiceAudioSourceNearMiss(ancestor, filepath.Base(missing)); near != "" {
		return fmt.Errorf("--%s %s: %s. %w %s %s",
			voiceFlagAudioSource, path, sentence, cause, voiceAudioSourceNearMissNote, near)
	}
	return fmt.Errorf("--%s %s: %s. %w", voiceFlagAudioSource, path, sentence, cause)
}

// voiceAudioSourceFirstMissing walks the path's OWN components and returns the
// first that is absent together with the deepest directory that does exist, or
// ("", "") when every component is there.
//
// IT STATS WHAT IT WAS GIVEN, ONE COMPONENT AT A TIME — no cleaning of the
// caller's spelling beyond the join, no resolution, no climb to a canonical root:
// "which component did you get wrong" is a question about the spelling given.
func voiceAudioSourceFirstMissing(path string) (missing, deepestExisting string) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", ""
	}
	dir, name := filepath.Split(abs)
	dir = filepath.Clean(dir)
	ancestor := ""
	for _, part := range voiceAudioSourceComponents(dir) {
		candidate := filepath.Join(ancestor, part)
		if !voiceAudioSourceIsDir(candidate) {
			return candidate, ancestor
		}
		ancestor = candidate
	}
	if _, err := os.Lstat(abs); err == nil {
		return "", ""
	}
	return filepath.Join(ancestor, name), ancestor
}

// voiceAudioSourceComponents splits a cleaned absolute directory path into its
// names, root first. The root keeps its separator in the first element, which is
// what makes filepath.Join rebuild an absolute path instead of a relative one.
func voiceAudioSourceComponents(dir string) []string {
	parts := strings.Split(dir, string(filepath.Separator))
	if len(parts) > 0 && parts[0] == "" {
		parts[0] = string(filepath.Separator)
	}
	return parts
}

// voiceAudioSourceIsDir reports whether path IS a directory that exists.
func voiceAudioSourceIsDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// voiceAudioSourceNearMiss returns, among the entries of dir, a name that
// resembles want closely enough to be worth naming, or "".
//
// TWO SHAPES COUNT, both from the measured accident: a name that becomes want
// when punctuation is dropped (the de-dashed session id), and a name sharing a
// long head with want (a component mistyped in its tail). It reads ONE directory
// listing and nothing else, because its whole job is to improve a sentence an
// operator is already reading.
func voiceAudioSourceNearMiss(dir, want string) string {
	if want == "" {
		return ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	stripped := voiceAudioSourceStripped(want)
	best := ""
	for _, e := range entries {
		if !e.IsDir() || e.Name() == want {
			continue
		}
		name := e.Name()
		if voiceAudioSourceStripped(name) == stripped {
			return filepath.Join(dir, name)
		}
		if best == "" && voiceAudioSourceSharesHead(want, name) {
			best = filepath.Join(dir, name)
		}
	}
	return best
}

// voiceAudioSourceStripped removes the punctuation a caller drops.
func voiceAudioSourceStripped(name string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(voiceAudioSourceNearMissDropped, r) {
			return -1
		}
		return r
	}, name)
}

// voiceAudioSourceSharesHead reports whether two names share enough of a head to
// be worth offering as a near miss.
func voiceAudioSourceSharesHead(a, b string) bool {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	if n < voiceAudioSourceNearMissFloor {
		return false
	}
	return a[:voiceAudioSourceNearMissFloor] == b[:voiceAudioSourceNearMissFloor]
}

// voiceSourceWAVPCM validates a WAV's geometry and returns its samples.
//
// THE GEOMETRY CHECK IS THE POINT OF THIS FUNCTION. Everything downstream reads
// the bytes as 16 kHz mono s16le (voiceWindowPlan, voicePCM16ToWAV, the engine),
// so a container that says something else is not a near miss — it is a run whose
// every window is the wrong length and whose transcript is noise. The refusal
// quotes what the file says and what is required.
func voiceSourceWAVPCM(path string, raw []byte) ([]byte, error) {
	if len(raw) < voiceWAVFmtChunkEnd {
		return nil, fmt.Errorf("--%s %s is a %s container with only %d bytes — its format chunk is truncated",
			voiceFlagAudioSource, path, voiceWAVEMagic, len(raw))
	}
	formatTag := binary.LittleEndian.Uint16(raw[voiceWAVFormatTagOffset:])
	channels := int(binary.LittleEndian.Uint16(raw[voiceWAVChannelsOffset:]))
	rate := int(binary.LittleEndian.Uint32(raw[voiceWAVRateOffset:]))
	bits := int(binary.LittleEndian.Uint16(raw[voiceWAVBitsOffset:]))

	if formatTag != voiceWAVFormatPCM {
		return nil, fmt.Errorf("--%s %s carries WAV format tag %d — only uncompressed PCM (%d) can be read: %s is the capture contract",
			voiceFlagAudioSource, path, formatTag, voiceWAVFormatPCM, voiceCaptureGeometryText())
	}
	if rate != voiceCaptureSampleRate || channels != voiceCaptureChannels || bits != voiceCaptureBitsPerSample {
		return nil, fmt.Errorf("--%s %s is %d Hz, %d channel(s), %d-bit — the capture contract is %s; resample it (`ffmpeg -i %s -ar %d -ac %d -f s16le %s.pcm`) or record it with the recorder",
			voiceFlagAudioSource, path, rate, channels, bits, voiceCaptureGeometryText(),
			path, voiceCaptureSampleRate, voiceCaptureChannels, path)
	}
	return voiceWAVDataPayload(path, raw)
}

// voiceWAVDataPayload walks the container's chunks to the samples, so a WAV with
// a metadata chunk ahead of its audio still yields them.
func voiceWAVDataPayload(path string, raw []byte) ([]byte, error) {
	for off := voiceWAVFmtChunkEnd; off+8 <= len(raw) && off <= voiceWAVScanLimit; {
		size := int(binary.LittleEndian.Uint32(raw[off+4:]))
		if string(raw[off:off+4]) == voiceWAVDataMagic {
			start := off + 8
			if start > len(raw) {
				break
			}
			end := len(raw)
			if size > 0 && start+size < len(raw) {
				end = start + size
			}
			return raw[start:end], nil
		}
		// Chunks are word-aligned: an odd size is followed by one pad byte.
		off += 8 + size + size%2
	}
	return nil, fmt.Errorf("--%s %s is a %s container with no %q chunk — it holds no samples",
		voiceFlagAudioSource, path, voiceWAVEMagic, voiceWAVDataMagic)
}

// voiceAudioSourceFor builds the capture source a run will read from: the file
// the operator named, or the recorder process on the device that was selected.
//
// THE DEVICE IS NOT ENUMERATED FOR A FILE RUN, and that is deliberate: device
// enumeration and the liveness probe are how a MICROPHONE is chosen, and a file
// run chooses none. It is also what makes `--audio-source` usable on a host with
// no sound card at all.
//
// A NIL OPTIONS IS "NO FILE", NOT A PANIC (measured 2026-10-09, task 01a11f13's
// second child). Every caller of the seam's production entry point passes the
// options it happens to have, and `nil` is what the chat surface's two capture
// points passed while they had no channel of their own — so reading the field
// straight off the pointer made the PRODUCTION `/voice` path dereference nil and
// die at the moment the mode was entered, on the one path that never mentioned the
// flag at all. The nil is now the same statement the empty string is (no file), so
// a caller with nothing to say gets the recorder instead of a crash.
func voiceAudioSourceFor(opts *voiceOptions, dev *voiceDevice) (voiceAudioSource, error) {
	if opts != nil {
		if path := strings.TrimSpace(opts.audioSource); path != "" {
			// THE SHAPE DICTATES THE PACING (voiceSourcePacing's own note): this
			// chooser feeds the capture-path shapes, whose window bound is a wall
			// clock, so a file arrives at the rate it was spoken.
			return voiceFileSourceFor(path, voiceSourcePaced), nil
		}
	}
	if dev == nil {
		return nil, errVoiceStagedNotOpen
	}
	return &voiceStagedRecorderSource{dev: *dev}, nil
}

// openVoiceStagedStream opens ONE live capture: the source is chosen by opts and
// its reader is started.
//
// THE PRODUCTION SHAPE IS UNCHANGED. A run that names no --audio-source gets the
// recorder process on the device the caller already selected, with the same argv
// as before (voiceLiveRecorderCommand); what changed is that the process now
// lives behind the seam instead of inside this function.
//
// chatground is the ground the stream's own argument `run` was kept for: the
// enum/liveness policy has already run by the time a device reaches here.
func openVoiceStagedStream(opts *voiceOptions, dev *voiceDevice) (*voiceStagedStream, error) {
	source, err := voiceAudioSourceFor(opts, dev)
	if err != nil {
		return nil, err
	}
	return newVoiceStagedStream(source)
}

// newVoiceStagedStream builds the window builder over a source and OPENS it.
//
// THE HAND-OFF IS TAKEN AFTER Open(), AND THAT ORDER IS LOAD-BEARING (task
// 01a1215c). The production source DOES NOT HAVE A CHANNEL until it has opened:
// voiceStagedRecorderSource.Chunks() returns `s.chunks`, and that field is
// assigned inside Open() — `s.chunks = make(chan []byte, voiceStagedQueueChunks)`
// — because the channel is the reader goroutine's hand-off and there is no reader
// before the recorder process exists. Reading it BEFORE Open therefore captured
// the field's zero value, `nil`, and the builder spent the whole mode selecting
// on a nil channel: `Next` blocked past every deadline and the mode reported
// "produced no audio within 10s" about a device that was delivering the entire
// time (measured on sager, 2026-10-10: builder.chunks = 0x0 while the reader was
// writing to 0xc000218380, and the source's own firstSample was stamped 10 s
// earlier — see the task report).
//
// THE FILE SOURCE COULD NOT SHOW IT. voiceFileSource builds its channel in
// newVoiceFileSource, so for a file run the pre-Open read happened to be correct
// — which is why the seam's own equivalence tests (process arm vs file arm) were
// green while the production device path was dead. The invariant is the SOURCE's,
// not one implementation's: a source's channel exists once it is OPEN, so that is
// when the builder takes it.
func newVoiceStagedStream(source voiceAudioSource) (*voiceStagedStream, error) {
	s := &voiceStagedStream{
		source: source,
		done:   make(chan struct{}),
	}
	if dev, ok := source.(*voiceStagedRecorderSource); ok {
		s.dev = dev.dev
		s.binary = dev.binary
	}
	if err := source.Open(); err != nil {
		return nil, err
	}
	// THE ONE MOMENT THE SOURCE HAS A CHANNEL. See this function's doc: taking
	// it before Open captured the production source's nil field and left the
	// builder selecting on a nil channel for the whole mode.
	s.chunks = source.Chunks()
	// THE SOURCE'S IDENTITY IS TAKEN AFTER Open, for the same reason its channel
	// is (the sibling fix in this file's history, commit 1c6c5f7ab): a recorder
	// source is NAMED by the binary Open spawned, so reading Origin() before Open
	// yields the empty spelling — ` from "pulse:@DEFAULT_SOURCE@"` in the refusal
	// that reports a silent device, and the same gap in every [voice] line built
	// from sourceLabel(). The invariant belongs to the SOURCE (a source is
	// describable once it is OPEN), and it is read here, once, so no caller has to
	// know when to ask.
	s.sourceOrigin = source.Origin()
	return s, nil
}

// voiceSourceFirstSample asks the source when it produced its first sample. Only
// the recorder has a meaningful answer: a file's samples exist before the run
// starts, so its OPEN deadline is never armed.
func voiceSourceFirstSample(source voiceAudioSource) time.Time {
	if rec, ok := source.(*voiceStagedRecorderSource); ok {
		return rec.firstSampleTime()
	}
	return time.Now()
}

// voiceValidateAudioSource refuses a --audio-source value that cannot be a
// capture, at PARSE time.
//
// IT CHECKS EXISTENCE AND SHAPE, NOT CONTENT. Whether the bytes carry the capture
// contract is decided where the bytes are read (voiceSourceWAVPCM), because only
// then is there a stream to refuse; what is knowable up front — a path that is not
// there, a path that is a directory, an empty value — is refused here so a
// mistake never costs a run.
func voiceValidateAudioSource(path string) error {
	if path == "" {
		return fmt.Errorf("--%s requires a file path (e.g. --%s utterance.pcm — 16kHz mono s16le, or a WAV of the same geometry)",
			voiceFlagAudioSource, voiceFlagAudioSource)
	}
	fi, err := os.Stat(path)
	if err != nil {
		return voiceAudioSourceOpenRefusal(path, err)
	}
	if fi.IsDir() {
		return fmt.Errorf("--%s %s is a directory — name the file holding the audio", voiceFlagAudioSource, path)
	}
	return nil
}

// voiceAudioSourceShapes names the invocation shapes that CAPTURE, and it is the
// one statement of where --audio-source means something.
//
// WHY IT IS NOT LEGAL EVERYWHERE. The flag replaces the microphone. A shape that
// opens no microphone has nothing for it to replace, and accepting it there would
// be the exact failure this codebase refuses everywhere else: a flag that is read
// by nothing, so the operator believes the audio they named was used (Rule 15 — a
// no-op wearing an instruction). The three capture shapes are the bare invocation,
// --client, and (as its head) --client --stdio, because all three run the same
// recorder.
func voiceAudioSourceShapes(opts *voiceOptions) bool {
	return opts.client || opts.capture
}
