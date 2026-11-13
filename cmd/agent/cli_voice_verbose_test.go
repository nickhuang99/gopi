package main

// cli_voice_verbose_test.go — THE DEBUG SINK OF THE VOICE SURFACES (task
// 01a13500).
//
// WHAT IS PINNED, and what each case is FOR. The operator's report was that a
// streaming run printed its whole debugging stream unconditionally: one
// `[voice] partial N at T: …` line per partial (19 of them in one capture),
// the channel's counters, and the live-shape note — so the transcript, i.e. the
// ANSWER, arrived at the bottom of a telemetry dump. The fix makes that stream
// opt-in; these cases are what keeps it opt-in, because the failure mode is
// silent (nothing breaks when a debug line comes back, the output just gets
// noisy again):
//
//  1. THE PRINT GATES — with the switch off, no per-partial line, no counter
//     block, no shape note, and the credential plumbing line is not printed
//     while the PRIVACY disclosure still IS. With it on, all of them appear.
//     Asserted on the PRINTERS' real output (captured stderr), not on a
//     variable, because a gate that is read correctly and then ignored by a
//     `fmt.Fprintf` beside it is exactly how this defect would come back.
//  2. THE RESOLUTION — flag > env > settings, on the ONE reader
//     (config.ResolveAudioVerbose). Asserted through that function, not through
//     a second copy of the precedence.
//  3. THE PARSER — `--verbose` and `-v` both set the flag rung, and neither
//     leaks into the positionals (a `-v` read as the AUDIO FILE would make the
//     bare capture shape try to transcribe a file named "-v").
//
// IT IS NOT AN END-TO-END CASE, and the real run is where the acceptance
// criterion lives (Rule 31): a live service cannot be a unit test. What is here
// is the part a real run cannot isolate — that the gate is on the print and not
// somewhere near it.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/doubaostream"
)

// voiceVerboseOptions builds the options an arm would be called with, with the
// switch resolved the way runVoiceSubcommand resolves it (the flag rung folded
// through the ONE reader), so the printers see what a real run would hand them.
func voiceVerboseOptions(t *testing.T, flagSet bool) *voiceOptions {
	t.Helper()
	t.Setenv(config.AudioVerboseEnv, "")
	t.Setenv(config.SettingsEnv, filepath.Join(t.TempDir(), "absent.json"))
	opts := &voiceOptions{}
	opts.verbose, _ = config.ResolveAudioVerbose(flagSet)
	return opts
}

// TestVoiceDefaultPathPrintsNoPartialStream is THE regression guard for the
// operator's report: the default path must not print a per-partial line, and
// must not print the channel's counters either.
//
// RED WITHOUT THE FIX: on the pre-change tree both printers went straight to
// stderr with no gate at all, so the assertions below (count == 0) fail on the
// first partial — the measured shape of the defect, one line per partial for the
// whole utterance.
func TestVoiceDefaultPathPrintsNoPartialStream(t *testing.T) {
	got := voiceCaptureStderr(t, func() {
		voiceReportStreamPartial(doubaostream.Partial{Index: 1, Text: "你好"}, false)
		voiceReportStreamPartial(doubaostream.Partial{Index: 2, Text: "你好吗", Definite: true}, false)
		voiceReportStreamSummary(&voiceLiveResult{
			PartialCount: 2, AudioMS: 10347, LogID: "lg-1", ReqID: "rq-1",
		}, false)
	})
	if got != "" {
		t.Fatalf("the DEFAULT path printed the debug sink — the transcript would arrive under it:\n%s", got)
	}
}

// TestVoiceVerbosePrintsThePartialStream is the other half: the switch gives the
// stream back, LINE FOR LINE (the same counts, the same figures), so making it
// opt-in did not silently lose the evidence the streaming claim is made of.
func TestVoiceVerbosePrintsThePartialStream(t *testing.T) {
	got := voiceCaptureStderr(t, func() {
		voiceReportStreamPartial(doubaostream.Partial{Index: 1, Text: "你好"}, true)
		voiceReportStreamPartial(doubaostream.Partial{Index: 2, Text: "你好吗", Definite: true}, true)
		voiceReportStreamSummary(&voiceLiveResult{
			PartialCount: 2, AudioMS: 10347, LogID: "lg-1", ReqID: "rq-1",
		}, true)
	})
	for _, want := range []string{
		"[voice] " + voiceStreamPartialKindUngated + " 1 at ",
		"[voice] " + voiceStreamPartialKindDefinite + " 2 at ",
		"你好吗",
		"2 partial(s)",
		"logid lg-1",
		"reqid rq-1",
		"10347ms of audio sent",
	} {
		if !voiceLineCarries(got, want) {
			t.Errorf("the verbose stream is missing %q:\n%s", want, got)
		}
	}
}

// TestVoiceStreamStartKeepsTheDisclosureAndGatesThePlumbing is the split that
// matters most: the PRIVACY sentence — "the recording leaves this machine in
// real time" — is a DISCLOSURE and stays on the default path (the streaming
// engine is the one cloud channel whose upload the process-level announce
// cannot cover), while the packet cadence, the cluster and the credential's
// ORIGIN are plumbing and move behind the switch.
//
// A default run that dropped the disclosure would be LESS honest than the two
// sibling cloud engines, not quieter — that is why this case asserts the
// presence of one sentence and the absence of the other in the same output.
func TestVoiceStreamStartKeepsTheDisclosureAndGatesThePlumbing(t *testing.T) {
	cred := doubaostream.Credential{AppID: "inline-id", Token: "inline-token"}
	const path = "/home/nick/.doubao/v2_voice.env"

	quiet := voiceCaptureStderr(t, func() {
		voiceReportStreamStart(cred, path, doubaostream.DefaultChunkMillis, "wss://example.invalid/api/v2/asr", false)
	})
	if !voiceLineCarries(quiet, "PRIVACY") || !voiceLineCarries(quiet, "leaves this machine in real time") {
		t.Errorf("the default path dropped the streaming PRIVACY disclosure:\n%s", quiet)
	}
	if voiceLineCarries(quiet, doubaostream.DefaultCluster) {
		t.Errorf("the default path printed the channel's plumbing (cluster):\n%s", quiet)
	}
	if voiceLineCarries(quiet, path) {
		t.Errorf("the default path named the credential's origin — plumbing belongs to the debug sink:\n%s", quiet)
	}
	// The credential's VALUE is never printed on any path (Rule 15): the plumbing
	// line may name a file or a channel, and this is the assertion that keeps it
	// that way now that the line only appears under the switch.
	if voiceLineCarries(quiet, cred.Token) {
		t.Fatalf("the disclosure leaked the credential's value:\n%s", quiet)
	}

	loud := voiceCaptureStderr(t, func() {
		voiceReportStreamStart(cred, path, doubaostream.DefaultChunkMillis, "wss://example.invalid/api/v2/asr", true)
	})
	if !voiceLineCarries(loud, "PRIVACY") {
		t.Errorf("the verbose path lost the disclosure — the switch ADDS, it never replaces:\n%s", loud)
	}
	for _, want := range []string{
		doubaostream.DefaultCluster,
		path,
		"wss://example.invalid/api/v2/asr",
	} {
		if !voiceLineCarries(loud, want) {
			t.Errorf("the verbose plumbing line is missing %q:\n%s", want, loud)
		}
	}
	if voiceLineCarries(loud, cred.Token) {
		t.Fatalf("the plumbing line leaked the credential's value:\n%s", loud)
	}
}

// TestVoiceVerboseResolutionPrecedence pins the three channels in order, through
// the ONE reader (Rule 2). The settings leg is driven by a real settings
// document rather than by a struct field, because "the key is read" is the claim
// being made.
func TestVoiceVerboseResolutionPrecedence(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(cfgPath, []byte(`{"audio_verbose": true}`), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	// (1) THE FLAG OUTRANKS EVERYTHING. With both lower channels set ON, a run
	// without the flag is OFF — that direction is what makes the flag a rung
	// rather than a default.
	t.Run("flag wins over env and settings", func(t *testing.T) {
		t.Setenv(config.SettingsEnv, cfgPath)
		t.Setenv(config.AudioVerboseEnv, config.EnvBoolOn)
		if on, source := config.ResolveAudioVerbose(true); !on {
			t.Errorf("the flag rung was not honoured (source=%q)", source)
		}
	})
	t.Run("env wins over settings", func(t *testing.T) {
		t.Setenv(config.SettingsEnv, cfgPath)
		t.Setenv(config.AudioVerboseEnv, config.EnvBoolOn)
		on, source := config.ResolveAudioVerbose(false)
		if !on || source != config.AudioVerboseEnv {
			t.Errorf("env did not decide: on=%v source=%q, want on with source %q", on, source, config.AudioVerboseEnv)
		}
	})
	t.Run("env off beats settings on", func(t *testing.T) {
		t.Setenv(config.SettingsEnv, cfgPath)
		t.Setenv(config.AudioVerboseEnv, "off")
		if on, source := config.ResolveAudioVerbose(false); on || source != config.AudioVerboseEnv {
			t.Errorf("a declared OFF was not honoured: on=%v source=%q", on, source)
		}
	})
	t.Run("settings answers when nothing else does", func(t *testing.T) {
		t.Setenv(config.SettingsEnv, cfgPath)
		t.Setenv(config.AudioVerboseEnv, "")
		on, source := config.ResolveAudioVerbose(false)
		if !on || source != config.AudioVerboseSettingsKey {
			t.Errorf("the settings key did not decide: on=%v source=%q, want source %q", on, source, config.AudioVerboseSettingsKey)
		}
	})
	t.Run("nobody spoke means off", func(t *testing.T) {
		t.Setenv(config.SettingsEnv, filepath.Join(t.TempDir(), "absent.json"))
		t.Setenv(config.AudioVerboseEnv, "")
		if on, source := config.ResolveAudioVerbose(false); on {
			t.Errorf("the default is not OFF: on=%v source=%q", on, source)
		}
	})
}

// TestVoiceVerboseFlagSpellingsParse pins the parser half: both spellings set
// the flag rung, and `-v` never reaches the positionals — where this verb reads
// a lone positional as THE AUDIO FILE, so a leaked `-v` would turn the bare
// capture into the file shape ("read audio \"-v\": no such file").
func TestVoiceVerboseFlagSpellingsParse(t *testing.T) {
	for _, args := range [][]string{
		{"--" + voiceFlagVerbose},
		{voiceFlagVerboseShort},
		{voiceFlagVerboseShort, "clip.wav"},
	} {
		opts, err := parseVoiceArgs(args)
		if err != nil {
			t.Fatalf("parse %v: %v", args, err)
		}
		if !opts.verbose {
			t.Errorf("parse %v: the flag rung is not set", args)
		}
	}
	// The leak guard, stated as the shape it protects: a run that named NO file
	// stays a capture, and a run that named one keeps it.
	opts, err := parseVoiceArgs([]string{voiceFlagVerboseShort})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if opts.audio != "" {
		t.Errorf("`%s` was read as the audio file %q", voiceFlagVerboseShort, opts.audio)
	}
	if !opts.capture {
		t.Errorf("`%s` alone must stay the bare capture shape", voiceFlagVerboseShort)
	}
	opts, err = parseVoiceArgs([]string{voiceFlagVerboseShort, "clip.wav"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if opts.audio != "clip.wav" || opts.capture {
		t.Errorf("`%s clip.wav` lost the file: audio=%q capture=%v", voiceFlagVerboseShort, opts.audio, opts.capture)
	}
	// The default: no spelling, no stream.
	opts, err = parseVoiceArgs([]string{"clip.wav"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if opts.verbose {
		t.Error("a run that never asked for the debug sink has it on")
	}
}

// voiceLineCarries reports whether any LINE of s carries sub. It is deliberately
// not a bare strings.Contains: these rows are lines by contract (one row of
// output per row of report), and a substring test would accept a fragment
// assembled across two of them — e.g. a "partial" from one row and the text from
// the next. (The package's existing containsLine asserts whole-line equality,
// which is the wrong shape here: the rows being asserted carry a prefix, a
// figure and a text that varies.)
func voiceLineCarries(s, sub string) bool {
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, sub) {
			return true
		}
	}
	return false
}
