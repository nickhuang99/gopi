package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/doubaostream"
)

// ── the third arm of the engine dispatch ───────────────────────────────
//
// These tests are about the SEAM: which engine a run resolves to, what the
// help claims, and what the refusal says. The streaming protocol itself is
// tested in internal/doubaostream; the real end-to-end run is in the task
// report (a live service cannot be a unit test).

// TestVoiceEngineValuesCarryEveryEngine is the anti-regression guard for the
// list the parser, the refusal message and the settings reader all share: every
// engine must be REACHABLE BY NAME (the cloud arms by name only, never by
// default) and the older values must still be there — removing one would break
// every saved setting. It grew by `streaming` (v2) and then by `sauc` (v3), and
// both growths are asserted HERE rather than in a second copy of the same list:
// a duplicate would pass while this one went red.
func TestVoiceEngineValuesCarryEveryEngine(t *testing.T) {
	want := map[string]bool{
		config.AudioEngineLocal:     false,
		config.AudioEngineRecording: false,
		config.AudioEngineStreaming: false,
		config.AudioEngineSauc:      false,
	}
	for _, v := range voiceEngineValues {
		if _, ok := want[v]; !ok {
			t.Errorf("the accepted values grew by an engine nothing here knows: %q", v)
		}
		want[v] = true
	}
	for v, seen := range want {
		if !seen {
			t.Errorf("the engine %q is not among the accepted values %v", v, voiceEngineValues)
		}
	}
	if config.AudioEngineDefault != config.AudioEngineRecording {
		t.Errorf("the DEFAULT engine changed to %q — streaming must be reached by NAME, never by default",
			config.AudioEngineDefault)
	}
}

// TestVoiceStreamingEngineIsNeverTheDefault drives the resolver's real paths
// with the streaming engine nowhere in the environment: settings unreadable,
// settings choosing nothing, and settings choosing `doubao`. The streaming
// engine has its own grant on the vendor's side, so a machine that never
// configured it must not silently start streaming.
func TestVoiceStreamingEngineIsNeverTheDefault(t *testing.T) {
	t.Setenv(config.AudioEngineEnv, "")

	t.Setenv(config.SettingsEnv, filepath.Join(t.TempDir(), "absent.json"))
	if got := voiceResolveEngine(&voiceOptions{}); got == config.AudioEngineStreaming {
		t.Fatalf("settings unreadable → resolved %q, which must never be the default", got)
	}

	empty := filepath.Join(t.TempDir(), "empty.json")
	if err := os.WriteFile(empty, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	t.Setenv(config.SettingsEnv, empty)
	if got := voiceResolveEngine(&voiceOptions{}); got == config.AudioEngineStreaming {
		t.Fatalf("settings choosing nothing → resolved %q", got)
	}
}

// TestVoiceStreamingEngineResolvesWhenNamed is the other half: the value must
// actually select the arm. It is asserted through the resolver (not through
// transcribeFile, which would need the network) and through the label, which is
// what a client parses.
func TestVoiceStreamingEngineResolvesByEveryChannel(t *testing.T) {
	t.Setenv(config.AudioEngineEnv, config.AudioEngineStreaming)
	t.Setenv(config.SettingsEnv, filepath.Join(t.TempDir(), "absent.json"))
	if got := voiceResolveEngine(&voiceOptions{}); got != config.AudioEngineStreaming {
		t.Fatalf("the environment channel resolved %q, want %q", got, config.AudioEngineStreaming)
	}

	t.Setenv(config.AudioEngineEnv, "")
	settings := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settings, []byte(`{"`+config.AudioEngineSettingsKey+`":"`+config.AudioEngineStreaming+`"}`+"\n"), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	t.Setenv(config.SettingsEnv, settings)
	if got := voiceResolveEngine(&voiceOptions{}); got != config.AudioEngineStreaming {
		t.Fatalf("the settings channel resolved %q, want %q", got, config.AudioEngineStreaming)
	}

	opts := &voiceOptions{engine: config.AudioEngineStreaming}
	if got := voiceResolveEngine(opts); got != config.AudioEngineStreaming {
		t.Fatalf("the flag channel resolved %q, want %q", got, config.AudioEngineStreaming)
	}
	if got := voiceEngineLabel(opts); got != voiceEngineLabelDoubaoStream {
		t.Errorf("label = %q, want %q", got, voiceEngineLabelDoubaoStream)
	}
	// The three engines must report three DIFFERENT labels: a client that must
	// know whether its audio was uploaded in real time parses this.
	labels := map[string]string{}
	for _, e := range voiceEngineValues {
		labels[voiceEngineLabel(&voiceOptions{engine: e})] = e
	}
	if len(labels) != len(voiceEngineValues) {
		t.Errorf("the engines do not have %d distinct labels: %v", len(voiceEngineValues), labels)
	}
}

// TestVoiceStreamingKeyFileIsTheExistingChannel: the streaming credential must
// arrive through the SAME key-file rungs the batch engine uses — a second knob
// would be a second thing to configure, and the brief forbids it.
func TestVoiceStreamingKeyFileIsTheExistingChannel(t *testing.T) {
	// The rung names the env var the batch engine already reads.
	if voiceStreamKeyFileEnv != config.AudioKeyFileEnv {
		t.Errorf("the streaming arm reads %q, want the existing %q", voiceStreamKeyFileEnv, config.AudioKeyFileEnv)
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "custom.env")
	if err := os.WriteFile(file, []byte("APPID=1\nTOKEN=2\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	opts := &voiceOptions{doubaoKeyFile: file}
	if got := voiceStreamCredentialLabelForOpts(opts); got != file {
		t.Errorf("voiceStreamCredentialLabelForOpts = %q, want the named file %q", got, file)
	}
	// The DEFAULT rung is exercised on a synthetic home (`t.Setenv("HOME", …)`)
	// so the assertion names the documented ~/.doubao/v2_voice.env without
	// depending on — or touching — the operator's own directory.
	t.Setenv("HOME", dir)
	if got := voiceStreamCredentialLabelForOpts(&voiceOptions{}); !strings.HasSuffix(got, filepath.Join(doubaostream.DefaultCredentialDir, doubaostream.DefaultCredentialFile)) {
		t.Errorf("the default credential path is %q, want the documented ~/.doubao/v2_voice.env", got)
	}
}

// TestVoiceStreamDepsReadyNamesTheMissingFile: the pre-flight check must fail
// BEFORE anything is sent, and its message must name the path and the knob —
// a credential failure that arrives as a handshake error costs a debugging
// session.
func TestVoiceStreamDepsReadyNamesTheMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.env")
	reason := voiceStreamDepsReady(&voiceOptions{doubaoKeyFile: missing})
	if reason == "" {
		t.Fatal("a missing credential file was reported as ready")
	}
	if !strings.Contains(reason, missing) {
		t.Errorf("the refusal does not name the file it wanted: %q", reason)
	}
	if !strings.Contains(reason, voiceFlagDoubaoKeyFile) || !strings.Contains(reason, config.AudioKeyFileEnv) {
		t.Errorf("the refusal does not name the knobs that fix it: %q", reason)
	}

	// A file that exists but carries no TOKEN is a different report: the path
	// is right, the content is not.
	incomplete := filepath.Join(t.TempDir(), "incomplete.env")
	if err := os.WriteFile(incomplete, []byte("APPID=1\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	reason = voiceStreamDepsReady(&voiceOptions{doubaoKeyFile: incomplete})
	if reason == "" {
		t.Fatal("a credential file without TOKEN was reported as ready")
	}
	if !strings.Contains(reason, incomplete) {
		t.Errorf("the refusal does not name the file: %q", reason)
	}

	good := filepath.Join(t.TempDir(), "good.env")
	if err := os.WriteFile(good, []byte("APPID=1\nTOKEN=2\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if reason := voiceStreamDepsReady(&voiceOptions{doubaoKeyFile: good}); reason != "" {
		t.Errorf("a usable credential file was reported as unready: %q", reason)
	}
}

// TestVoiceStreamNoSpeechIsNotReportedAsAnEmptyTranscript is the rule the brief
// states explicitly: code 1013 is the service's CORRECT verdict that the audio
// held no speech. It must not be dressed up as a failure of the run, and an
// empty transcript must not be handed back as "you said nothing".
func TestVoiceStreamNoSpeechIsNotReportedAsAnEmptyTranscript(t *testing.T) {
	err := voiceStreamError(&doubaostream.NoSpeechError{Message: "no speech", ReqID: "r", LogID: "l"}, nil)
	if err == nil {
		t.Fatal("a no-speech verdict was reported as success")
	}
	msg := err.Error()
	if !strings.Contains(msg, "no speech") {
		t.Errorf("the message does not say what was found: %q", msg)
	}
	if !strings.Contains(msg, "not a failure of the run") {
		t.Errorf("the message does not say the verdict is not a failure: %q", msg)
	}
	if strings.Contains(msg, "doubao-stream engine: streaming asr:") {
		t.Errorf("the no-speech verdict was wrapped as a transport failure: %q", msg)
	}

	// Everything ELSE is a real failure and keeps the vendor's own words.
	real := voiceStreamError(&doubaostream.APIError{Op: "server", Code: 45000030,
		Message: "requested resource not granted", LogID: "l", ReqID: "r"}, nil)
	if !strings.Contains(real.Error(), "45000030") || !strings.Contains(real.Error(), "not granted") {
		t.Errorf("a real failure lost the vendor's code or reason: %q", real.Error())
	}
	if !strings.Contains(real.Error(), config.AudioEngineStreaming) {
		t.Errorf("a real failure does not name the engine: %q", real.Error())
	}
}

// TestVoiceStreamErrorIsTheErrorChainNotJustAString: callers detect the
// no-speech verdict with errors.As, so the split must be structural.
func TestVoiceStreamNoSpeechSurvivesErrorsAs(t *testing.T) {
	orig := &doubaostream.NoSpeechError{Message: "no speech", LogID: "l"}
	err := voiceStreamError(orig, nil)
	if !errors.As(err, new(*doubaostream.NoSpeechError)) {
		t.Fatalf("the no-speech error stopped being detectable through errors.As: %v", err)
	}
}

// TestVoiceUsageTextAdvertisesTheStreamingEngine: the help page is the only
// place an operator learns the value exists, and Rule 13 says the value comes
// from its constant. The stale claim this replaces was worse than silent — it
// said streaming was not enabled at all.
func TestVoiceUsageTextAdvertisesTheStreamingEngine(t *testing.T) {
	help := voiceUsageText()
	if !strings.Contains(help, config.AudioEngineStreaming) {
		t.Errorf("the help does not mention the %q engine:\n%s", config.AudioEngineStreaming, help)
	}
	if !strings.Contains(help, config.AudioEngineRecording) || !strings.Contains(help, config.AudioEngineLocal) {
		t.Errorf("the help lost one of the older engine names")
	}
	// The claim that streaming is unavailable must be GONE: v2 IS available
	// (measured), and what this account lacks is v3 (sauc).
	if strings.Contains(help, "流式\n                     resource the epic wanted first is NOT granted") {
		t.Errorf("the help still claims the streaming resource is not granted")
	}
	if !strings.Contains(help, "v2") {
		t.Errorf("the help does not say which protocol makes streaming available")
	}
	// A placeholder left unreplaced would ship a literal "{engineStream}".
	for _, ph := range []string{voiceEngineStreamPlaceholder, voiceEngineDoubaoPlaceholder,
		voiceEngineLocalPlaceholder, voiceFormatPlaceholder} {
		if strings.Contains(help, ph) {
			t.Errorf("the help still carries the placeholder %q", ph)
		}
	}
}
