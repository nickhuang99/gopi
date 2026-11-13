package main

// voice_module_defects_test.go — the four defects of task 01a11da05, one case
// each, written as the MINIMAL REPRO of the reported behaviour (Rule 5).
//
// Every case drives the same surface the operator drove:
//
//	A  the chat privacy row              → the production grid (chat_voice.go)
//	B  the capture-closed exit           → the production loop, over the real
//	                                       carrier, with a recorder that reports
//	                                       the SENTINEL
//	C  `gopi tool voice_input -s 5`      → the REAL binary, argv and exit status
//	D' the closing "transcribed …" line  → the real binary through the local
//	                                       engine, plus the format table every
//	                                       engine is rendered through
//
// The red side of each was observed before the fix (see the report's Ran:/Result:
// lines); nothing here asserts a behaviour the fix does not implement.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
)

// ===========================================================================
// A — ONE `[voice] ` prefix on the chat privacy row
// ===========================================================================

// TestChatVoicePrivacyRowCarriesOnePrefix is the defect case for A.
//
// WHAT WAS WRONG. The chat arm composed the cloud engine's row as
// `"[voice] %s\n"` around voiceDoubaoPrivacyNotice — which ALREADY begins with
// `[voice] ` (it is the same one function the CLI prints through, and the CLI
// prints it bare). The row therefore read `[voice] [voice] PRIVACY — …`.
//
// THE JUDGE IS THE GRID (Rule 10.2): the assertion reads the cells the
// operator's terminal shows, not the format string, so a second prefix can
// reappear at either end of the composition and still be caught.
//
// WHY THE CASE IS SEQUENCED. This case was FLAKY (measured 12 of 20 runs red on
// 2026-10-11, and red in the whole-package run) with "timed out waiting for the
// privacy row on the grid" — and the row is not slow to arrive. RE-MEASURED
// 2026-10-12 (row 01a12d65, 4 runs on this tree): the announcement fired at
// t=55-68us, and the row's text was gone from the 14-row grid within a few
// milliseconds, because the band that carries it scrolls. Production paces a
// window at voiceChatWindowSeconds (6s) and pays the microphone for it; the
// scripted recorder computes one in MICROSECONDS, so the loop answers a silent
// window the instant it is called and the only clock between two of them is the
// pacing gap (voiceChatWindowPauseSeconds, 1s). Measured: THREE silent windows
// inside the case's 3s bound, one second apart — each writing its own row into
// the answer band, and a band taller than the screen scrolls the FIRST row off,
// which is the privacy row. So the case was reading a scrolling window and
// calling its height a property of the notice.
//
// THE FIX HAS TWO HALVES, and both are about not racing the fixture:
//
//	(1) the recorder publishes a gate, so each round runs when the case releases
//	    it (armRoundGate) — the loop advances because the case said so, not while
//	    the case is still reading the screen it left behind. The screen a step is
//	    judged on is therefore COMPLETE and STILL, instead of mid-burst.
//	(2) the "was the row announced?" wait reads the ANNOUNCEMENT (the console's
//	    own notice tee), not the grid, while the PREFIX judgement still reads the
//	    grid — because that judgement is about how the row was COMPOSED, and the
//	    cells an operator sees are what show a doubled prefix.
//
// The assertion is not weaker than it was: the case still counts `[voice] `
// prefixes on the painted grid at exactly 1 and still fails on the doubled row.
// What changed is that it now controls when the count is taken.
func TestChatVoicePrivacyRowCarriesOnePrefix(t *testing.T) {
	const doubled = "[voice] [voice]"
	// THE PRODUCTION SENTENCE, rendered by the production function: a case that
	// typed its own copy of "PRIVACY …" would keep passing after the notice's
	// wording moved.
	notice := voiceDoubaoPrivacyNotice(&voiceOptions{})
	if !strings.HasPrefix(notice, "[voice] ") {
		t.Fatalf("the notice no longer carries its own prefix (%q) — this case's premise is gone: the call site must print it bare and the notice must own the prefix", notice)
	}

	rec := &scriptedRecorder{desc: "test mic"}
	// THE STEPS ARE RELEASED ONE AT A TIME (see the case comment): round 1 is held
	// shut until the privacy row has been judged.
	rec.armRoundGate()
	h := newVoicePtyGridTall(t, rec)
	h.mode.resolveOpts = func() (*voiceOptions, error) {
		return &voiceOptions{device: "test-device", engine: config.AudioEngineRecording}, nil
	}
	h.mode.depsMissing = func(*voiceOptions) string { return "" }
	h.enter(t)

	// THE ROW'S STABLE OPENING, derived from the production sentence rather than
	// re-typed. The FULL notice is not a usable wait subject: its back-to-local
	// hint ends with this run's own settings path, so waiting for the whole string
	// would be waiting for a temp directory's name. The first two words are the
	// part that identifies the row (`[voice] PRIVACY`) and they are exactly what
	// this case is about — the prefix.
	rowOpening := strings.Join(strings.Fields(notice)[:2], " ")

	// STEP 1 — THE ROW IS ANNOUNCED. This is the wait that used to time out: it
	// asks the console's announcement record, so the row being pushed off a short
	// screen by the rows after it can no longer be read as the row never having
	// been said.
	h.waitAnnounced(t, "the privacy row to be announced", rowOpening)

	// STEP 2 — THE SCREEN IS READ BEFORE ANYTHING ELSE CAN TOUCH IT. The loop has
	// reached the gate and is holding the round (the recorder counts the ask the
	// mode makes right before it waits), so the grid is the screen the ENTRY left:
	// complete, still, and not a burst's tail. The parked state is waited on rather
	// than assumed — a loop that ignored the gate would be caught by this wait
	// timing out, instead of racing the reading below.
	if !h.waitGateAsked(t, 1) {
		t.Fatalf("the capture loop never asked the recorder for a gate — the recorder's gate was not honoured, "+
			"so this case cannot hold a round still while it reads the screen (rounds run: %d)", rec.roundsRun())
	}
	rows := h.rows()
	if strings.Contains(rows, doubled) {
		t.Fatalf("the privacy row is prefixed TWICE — the call site re-typed the prefix the notice already carries (Rule 13)\n--- grid ---\n%s", rows)
	}
	if n := strings.Count(rows, "[voice] "); n != 1 {
		t.Errorf("the grid shows %d `[voice] ` prefixes, want exactly 1\n--- grid ---\n%s", n, rows)
	}
	// The grid SHOWS the row (the same claim the counting case makes), so a row
	// that was announced and never painted cannot pass this case — the two
	// readings are complementary: the grid says the operator can see it, the
	// announcement says the console said it whatever the screen did afterwards.
	// TestChatVoice_QuietWindowKeepsThePlainSilenceRow is where the band's own
	// scroll behaviour is judged, at a fixed height, on purpose.
	if !strings.Contains(strings.ReplaceAll(rows, "\n", ""), "PRIVACY") {
		t.Errorf("the announced privacy row is not on the grid the entry left\n--- grid ---\n%s", rows)
	}

	// STEP 3 — THE ROUND IS ALLOWED TO FINISH. The gate is closed only now, after
	// the grid has been read, so the round's own row lands on a screen the case has
	// already judged. The empty window is the harness recorder's own outcome, not
	// a claim about the fix.
	close(rec.armedGate(t))
	h.waitFor(t, "the first round to land", func(rows string) bool {
		return strings.Contains(rows, "no text in the last")
	})
	// The mode is left by the case's own cleanup (newVoicePtyGrid calls the
	// production shutdown), so nothing here depends on the loop's exit path.
}

// ===========================================================================
// B — the sentinel exit is quiet, and it does not stay ON either
// ===========================================================================

// TestChatVoice_CaptureClosedExitsQuietly is the defect case for B.
//
// WHAT WAS WRONG. errVoiceStreamClosed is the sentinel for "the capture was
// closed under this round" — the mode's OWN exit path. The recorder already
// answers nil for it (voiceChatRecorderStaged.readerFailure), but the loop, the
// other reader of the same sentinel, took the generic arm: the screen showed
// `voice mode FAILED — the voice capture stream was closed. The mode is OFF`
// and the exit was attributed to `OFF (a failed round)`, on a round nothing had
// failed.
//
// THE CASE ASSERTS BOTH HALVES, because "quiet" alone would be satisfiable by a
// mode that stayed ON over a microphone that no longer exists (the half-open
// state Rule 15 forbids): no FAILED row, NOT `a failed round`, and the mode is
// OFF.
func TestChatVoice_CaptureClosedExitsQuietly(t *testing.T) {
	rec := &scriptedRecorder{desc: "test mic", roundErr: errVoiceStreamClosed}
	h := newVoicePtyGrid(t, rec)
	h.mode.resolveOpts = voiceChatTestOptions
	h.mode.depsMissing = func(*voiceOptions) string { return "" }
	h.enter(t)
	h.waitFor(t, "the capture-closed exit", func(rows string) bool {
		return strings.Contains(rows, "voice mode OFF")
	})
	rows := h.rows()
	if strings.Contains(rows, "voice mode FAILED") {
		t.Errorf("the sentinel round reported FAILED — a closed capture is an exit, not a failure\n--- grid ---\n%s", rows)
	}
	if strings.Contains(rows, chatVoiceLeftByFailed) {
		t.Errorf("the exit was attributed to %q — nothing failed\n--- grid ---\n%s", chatVoiceLeftByFailed, rows)
	}
	if !strings.Contains(rows, chatVoiceLeftByCaptureEnded) {
		t.Errorf("the exit does not name why the mode left (%q)\n--- grid ---\n%s", chatVoiceLeftByCaptureEnded, rows)
	}
	if h.mode.active() {
		t.Error("the mode is still ON over a capture that ended — the status row would claim a microphone nobody is reading")
	}
	if got := rec.roundsRun(); got != 1 {
		t.Errorf("the loop ran %d round(s), want exactly 1 — a closed capture is not retried", got)
	}
}

// ===========================================================================
// C — an unknown SHORT flag is refused as a flag
// ===========================================================================

// TestVoiceUndeclaredShortFlagIsRefused is the defect case for C, driven on the
// REAL binary (Rule 31): the operator's own argv, the operator's own exit status.
//
// WHAT WAS WRONG. The shared parser only recognises the double-dash spelling, so
// `-s 5` reached parseVoiceArgs as a POSITIONAL. The invocation was then read as
// the file shape for a file named "-s", and the run died inside the engine with
// `read audio "-s": open -s: no such file or directory` — a refusal naming a file
// the operator never wrote, and never mentioning the flag they typed.
func TestVoiceUndeclaredShortFlagIsRefused(t *testing.T) {
	audio := writeVoiceTestClip(t)
	// THE REFUSALS ARE ASSERTED ON A PROFILE THIS CASE OWNS. `gopi tool
	// voice_input` with no engine flag resolves the engine from the operator's
	// settings, and the machine's own default differs between the local box and
	// the cloud one — so a case that inherited it would pass or fail by host. The
	// engine is PINNED here (Rule 1/2: the rung is named, not inferred), and the
	// refusal cases are refused BEFORE any engine work, which the `open -s`
	// negative assertion below is what proves.
	env := voiceModuleEnv(t, t.TempDir())

	cases := []struct {
		name    string
		args    []string
		want    []string
		wantNot string
	}{
		{
			name:    "the operator's own typo",
			args:    []string{"-s", "5"},
			want:    []string{"unknown flag -s", "--" + voiceFlagSeconds},
			wantNot: "open -s",
		},
		{
			name:    "no short spelling has a smuggle route",
			args:    []string{"-x"},
			want:    []string{"unknown flag -x"},
			wantNot: "open -x",
		},
		{
			name:    "a short flag beside a real file argument",
			args:    []string{audio, "-s", "5"},
			want:    []string{"unknown flag -s", "--" + voiceFlagSeconds},
			wantNot: "open -s",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, errOut, code := runVoiceChild(t, env, nil, c.args...)
			if code == 0 {
				t.Fatalf("`voice_input %v` exited 0 — an undeclared flag must be refused, not reinterpreted\nstderr:\n%s", c.args, errOut)
			}
			for _, want := range c.want {
				if !strings.Contains(errOut, want) {
					t.Errorf("stderr does not name %q — the refusal must answer with the flag the operator typed\nstderr:\n%s", want, errOut)
				}
			}
			if strings.Contains(errOut, c.wantNot) {
				t.Errorf("stderr still reports the CONSEQUENCE (%q) of reading the flag as a file — the mistake must be answered with the mistake\nstderr:\n%s", c.wantNot, errOut)
			}
		})
	}
}

// TestVoicePositionalShapesSurviveTheShortFlagRefusal is the negative control: the
// refusal must be about FLAG SHAPES only, and the shapes an operator really types
// must still parse. Without this, "refuse everything starting with a dash" would
// pass the case above while breaking the verb.
func TestVoicePositionalShapesSurviveTheShortFlagRefusal(t *testing.T) {
	// A PATH that begins with a dash is still a path: it is a value, not a flag,
	// and the caller's own file name must not be second-guessed.
	opts, err := parseVoiceArgs([]string{"-", "--engine", config.AudioEngineLocal})
	if err != nil {
		t.Fatalf("a lone `-` (the stdin convention) was refused: %v", err)
	}
	if opts.audio != "-" {
		t.Errorf("audio = %q, want the `-` positional", opts.audio)
	}
	// A NUMERIC positional is not a flag spelling either — `-3` is a value, and
	// the short-flag rule says so by its second character.
	if _, err := parseVoiceArgs([]string{"-3"}); err != nil {
		t.Errorf("`-3` was refused as a flag: %v", err)
	}
	// And the long form is untouched.
	if _, err := parseVoiceArgs([]string{"--" + voiceFlagSeconds, "5"}); err != nil {
		t.Errorf("--%s was refused: %v", voiceFlagSeconds, err)
	}
}

// voiceModuleEnv is the environment these cases run the binary in: a PRIVATE
// gopi profile (so nothing the case does can read or touch the operator's own
// settings) with the engine pinned to the local arm.
//
// It is the testutil.SnapshotEnv-style explicit pair rather than t.Setenv because
// the child is a separate PROCESS built from this one's environment: the pin has
// to be in the slice handed to exec, not in this process's own env.
func voiceModuleEnv(t *testing.T, home string) []string {
	t.Helper()
	return append(os.Environ(),
		config.GopiDirEnv+"="+home,
		config.AudioEngineEnv+"="+config.AudioEngineLocal,
	)
}

// writeVoiceTestClip writes a minimal WAV-bodied clip for the shapes that need
// a real path on disk (the file shape never opens the bytes itself — the ENGINE
// does, and the stub engine here accepts anything; the point of the file is that
// a positional is a PATH, so the refusal cases must run WITH one).
func writeVoiceTestClip(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "clip.wav")
	if err := os.WriteFile(path, []byte("RIFF????WAVEstub"), 0o600); err != nil {
		t.Fatalf("write clip: %v", err)
	}
	return path
}

// ===========================================================================
// The stub RECORDER rig — the capture boundary, held down on every host
// ===========================================================================

// voiceRigSource is the Pulse source name the stubbed enumerator lists.
//
// IT IS A NAME, NOT A LEVEL: the rig never claims a microphone exists on this
// machine, it claims that WHEN the enumerator answers, it answers with this
// source — which is the only thing the device policy reads. The prefix marks it
// as the rig's own, so a diagnostic that quotes the device names the fixture.
const voiceRigSource = "gopi-voice-rig-source"

// voiceRigDefaultSourceMarker is what the stubbed `pactl` answers
// `get-default-source` with: the ONE source it also lists, so the two answers
// cannot name different sources.
const voiceRigDefaultSourceMarker = voiceRigSource

// voiceRigStubShell is the interpreter the two stub scripts are written for
// (POSIX sh: no dependency of any kind, Rule 35).
const voiceRigStubShell = "/bin/sh"

// voiceRigStubMode makes the stub scripts executable.
const voiceRigStubMode = 0o700

// voiceRigLogEnv names the file the stubbed parecord appends its own argv to.
// The rig's proof that the STUB recorder ran (rather than this host's sound
// system, or nothing at all) is that file carrying the production parecord argv
// — `--raw --rate=16000 …`, which only a spawned production recorder writes.
const voiceRigLogEnv = "GOPI_VOICE_RIG_LOG"

// voiceRigPathEnv is the PATH assignment the rig's directory is prepended to —
// named (Rule 1) because the entry has to be the FIRST element for the stubbed
// binaries to win over this host's own.
const voiceRigPathEnv = "PATH"

// voiceRigInfoHeader is the header `pactl info` opens with. It is the marker
// voicePulseServerUp looks for, and that is why the stub prints it: WITHOUT a
// sound server the policy takes tier 3 (ALSA `default`) — which routes back into
// the very server that is not there — and the asserting tier would then be a
// different one on every host. With the server "up", tier 3 is skipped by the
// production rule itself and tier 4 (the first enumerated source) is reached
// deterministically.
const voiceRigInfoHeader = "Server Name: gopi-voice-rig"

// voiceRigPCMChunkSamples is one write of the stub recorder, in SAMPLES: 10 ms
// of the pinned geometry (voiceCaptureSampleRate/100 at
// voiceCaptureChannels, PCM16 = two bytes each), the same pacing
// fakeRecorderStreamsForever uses, so the byte arithmetic is exact.
const voiceRigPCMChunkSamples = (voiceCaptureSampleRate / 100) * voiceCaptureChannels

// voiceRigPCMChunkByte is the byte the stub writes for every sample: a
// non-silent level (0x4000 = quarter scale), because a silent stub would let a
// broken capture pass a byte-count-only check.
const voiceRigPCMChunkByte = 0x40

// voiceRigRecorderBody is the stubbed `parecord`: it has NO duration flag (the
// real one does not either) so it streams the pinned geometry until its stdout
// goes away, which is how production closes a parecord window (SIGINT + the
// closed read side, stopVoiceRecorder).
//
// The log file it appends to BEFORE the first sample is not decoration: it is
// what proves the stub, not this host's sound system, opened the window — the
// test asserts on it, so a rig that silently stopped being used cannot pass.
var voiceRigRecorderBody = "#!/usr/bin/env python3\n" +
	"import os, sys, time\n" +
	"log = os.environ.get(" + strconv.Quote(voiceRigLogEnv) + ")\n" +
	"if log:\n" +
	"    with open(log, 'a') as f:\n" +
	"        f.write(' '.join(sys.argv[1:]) + '\\n')\n" +
	"chunk = bytes([0x00, " + strconv.Itoa(voiceRigPCMChunkByte) + "]) * " +
	strconv.Itoa(voiceRigPCMChunkSamples) + "\n" +
	"while True:\n" +
	"    try:\n" +
	"        if os.write(1, chunk) != len(chunk):\n" +
	"            break\n" +
	"    except OSError:\n" +
	"        break\n" +
	"    time.sleep(0.01)\n"

// voiceRigPactlBody is the stubbed `pactl`: the three answers the device policy
// asks for, and nothing else. An unknown argv is a REFUSAL (exit 1 with the argv
// on stderr) rather than silence: a production question this rig does not model
// must be visible, not answered with an empty listing that reads as "no device".
var voiceRigPactlBody = "#!/bin/sh\n" +
	"case \"$*\" in\n" +
	"  \"" + voicePactlListFlag + " " + voicePactlShortFlag + " " + voicePactlSourcesArg + "\")\n" +
	"    printf '%s\\t%s\\n' 0 " + voiceRigSource + "\n" +
	"    exit 0;;\n" +
	"  \"" + voicePactlDefaultSourceArg + "\")\n" +
	"    printf '%s\\n' " + voiceRigDefaultSourceMarker + "\n" +
	"    exit 0;;\n" +
	"  \"" + voicePulseInfoArg + "\")\n" +
	"    printf '%s\\n' '" + voiceRigInfoHeader + "'\n" +
	"    exit 0;;\n" +
	"esac\n" +
	"printf 'pactl stub: unsupported argv: %s\\n' \"$*\" 1>&2\n" +
	"exit 1\n"

// voiceRigArecordBody is an `arecord` that FAILS: the ALSA arm is not modelled
// by this rig (no sound card is being claimed), and a failure is the honest
// answer — the policy records it as a note and enumerates the Pulse sources,
// which is the arm the test is about.
var voiceRigArecordBody = "#!/bin/sh\n" +
	"printf '%s\\n' 'arecord stub: no ALSA device in this rig' 1>&2\n" +
	"exit 1\n"

// voiceRecorderRig materialises the capture boundary and returns the PATH entry
// that makes production resolve to it, plus the log the stubbed recorder writes.
//
// THE RIG REPLACES THE RECORDER, NOT THE RUN. Everything above the recorder —
// device enumeration and its tier policy, the liveness probe, the argv, the
// window, the WAV, the engine hand-off, the closing line — stays the REAL
// binary's code. Only the two programs that would touch this host's sound
// system are replaced by two scripts in a temporary directory (Rule 35: they
// live under t.TempDir, i.e. the suite's scratch root, and are gone with the
// test; nothing is installed, nothing is left running).
//
// The hook is the environment, and it must reach the CHILD PROCESS: the PATH
// entry goes into the []string handed to exec (Rule 17 — t.Setenv would only
// move this process's own env), which is also how the engine stub is pinned.
func voiceRecorderRig(t *testing.T) (pathEntry, logPath string) {
	t.Helper()
	dir := t.TempDir()
	stubs := map[string]string{
		voiceParecordBinary: voiceRigRecorderBody,
		voicePactlBinary:    voiceRigPactlBody,
		voiceArecordBinary:  voiceRigArecordBody,
	}
	for name, body := range stubs {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), voiceRigStubMode); err != nil {
			t.Fatalf("write %s stub: %v", name, err)
		}
	}
	return dir, filepath.Join(t.TempDir(), "parecord-argv.log")
}

// voiceReadRigLog returns the recorder argv lines the stub recorded ("" when it
// was never started — which the caller must treat as "the stub did not run").
func voiceReadRigLog(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read rig log %s: %v", path, err)
	}
	return string(b)
}

// ===========================================================================
// D' — the closing line names the ENGINE, never "locally"
// ===========================================================================

// TestVoiceClosingLineNamesTheEngine is the defect case for D'.
//
// WHAT WAS WRONG. `transcribed locally in 2.5s` carried two meanings at once:
// "on this machine (not the far end of a --client hop)" and "with the local
// funasr engine (not the cloud one)". On a run whose OPENING row had just
// announced that the audio leaves this machine, the closing row read as its own
// contradiction — two independent readers hit it (task 01a11da05). The
// disclosure itself was correct and is NOT touched: what changes is that the
// closing line names the engine through the SAME label the announcing rows
// print, so the two ends of one run cannot name different engines.
func TestVoiceClosingLineNamesTheEngine(t *testing.T) {
	// The rendered rows, per engine — a table because the line must be TRUE on
	// every arm, including the cloud one the bare capture now defaults to.
	for _, engine := range []string{config.AudioEngineLocal, config.AudioEngineRecording, config.AudioEngineStreaming} {
		opts := &voiceOptions{engine: engine, chunk: voiceDefaultChunkSec, seconds: voiceClientDefaultSeconds}
		label := voiceEngineLabel(opts)
		line := fmt.Sprintf(voiceTranscribedByNote, label, 2.5, opts.chunk)
		if !strings.Contains(line, label) {
			t.Errorf("the %s closing line does not name the engine: %q", engine, line)
		}
		if strings.Contains(line, "locally") {
			t.Errorf("the %s closing line still says %q — the word carries two meanings and one of them is false on the cloud arm", engine, "locally")
		}
		empty := fmt.Sprintf(voiceEngineNoTextNote, label, 1.0, 15)
		if !strings.Contains(empty, label) || strings.Contains(empty, "the local engine") {
			t.Errorf("the %s empty-transcript row must name the engine, got %q", engine, empty)
		}
	}

	// AND ON THE REAL BINARY: a run that really transcribed must print the engine
	// it used. The shape is the BARE capture — the one that owns this line (the
	// file shape closes with `clip.wav in 0.1s`) — so the run exercises the
	// production path end to end: enumeration, the tier policy, the liveness
	// probe, the window, the WAV, the engine hand-off, the closing line.
	//
	// TWO SEAMS, BOTH DOCUMENTED, AND NEITHER IS A SKIP. The ENGINE is the
	// `--runtime` stub (a real funasr run would need the 910MB of weights to say
	// one sentence). The RECORDER is the rig below (pactl + parecord on the
	// child's PATH), because this case used to depend on THIS MACHINE HAVING A
	// MICROPHONE and the rig replaces that dependency instead of skipping the
	// case on a host without one: measured 2026-10-15, the bare capture exited 1
	// with `no capture devices found` on every sound-card-less host (headless /
	// VM / the client laptop), which made a permanent red indistinguishable from
	// a regression. Nothing is skipped and nothing else is stubbed: the device
	// the run announces is the rig's own enumerated source, and the recorder's
	// argv is the production one, asserted below.
	bin := coverBinForTest(t)
	models := voiceWriteModels(t)
	spool := t.TempDir()
	const wantText = "voice closing line stub"
	const captureSeconds = 2
	rigPath, rigLog := voiceRecorderRig(t)
	env := voiceStubEnv(t, spool,
		voiceStubTextEnv+"="+wantText,
		config.GopiDirEnv+"="+t.TempDir(),
		// The RECORDER is the rig (pactl + parecord + a refusing arecord), and
		// the rig directory goes FIRST on the child's PATH so production's own
		// `exec.Command("parecord", …)`/("pactl", …) resolves to it.
		voiceRigLogEnv+"="+rigLog,
		voiceRigPathEnv+"="+rigPath+string(os.PathListSeparator)+os.Getenv(voiceRigPathEnv),
	)
	args := voiceStubArgs(t, models, "--"+voiceFlagSeconds, strconv.Itoa(captureSeconds))
	_, errOut, code := runVoiceBinary(t, bin, env, nil, args...)
	if code != 0 {
		t.Fatalf("the bare capture exited %d\nstderr:\n%s", code, errOut)
	}
	// THE RIG PROVES IT RAN. Two readings, and neither is implied by the exit
	// code: the stubbed parecord logged its OWN argv (so the window really was
	// opened by the script, not by this host's sound system), and the selection
	// line names the stub's enumerated source (so the device came from the
	// stubbed enumerator through the policy, not from a real card).
	//
	// The selected ID is the SERVER-SIDE WILDCARD — production offers
	// `pulse:@DEFAULT_SOURCE@` first on purpose (it survives a server default
	// change) and the rig's `get-default-source` answers with the same source it
	// lists, so the wildcard and the concrete name are one device here.
	got := voiceReadRigLog(t, rigLog)
	if strings.TrimSpace(got) == "" {
		t.Fatalf("the stubbed recorder was never spawned (log %q is empty) — the run did not go through the rig\nstderr:\n%s", rigLog, errOut)
	}
	rigContract := fmt.Sprintf(voiceParecordRateFormat, voiceCaptureSampleRate)
	if !strings.Contains(got, voiceParecordRawFlag) || !strings.Contains(got, rigContract) {
		t.Errorf("the spawned recorder's argv is not the production parecord contract: %q", got)
	}
	wantLine := "recording " + strconv.Itoa(captureSeconds) + "s from " +
		voiceDeviceBackendPulse + voiceDeviceIDSeparator
	if !strings.Contains(errOut, wantLine) || !strings.Contains(errOut, voiceRigSource) {
		t.Errorf("the run did not announce the rig's device (want %q naming %q)\nstderr:\n%s",
			wantLine, voiceRigSource, errOut)
	}
	if !strings.Contains(errOut, "transcribed by "+voiceEngineLabelLocal) {
		t.Errorf("the closing line does not name the engine that ran (%q)\nstderr:\n%s", voiceEngineLabelLocal, errOut)
	}
	if strings.Contains(errOut, "locally") {
		t.Errorf("the real run still closes with %q\nstderr:\n%s", "locally", errOut)
	}
}
