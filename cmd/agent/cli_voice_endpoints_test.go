package main

// cli_voice_endpoints_test.go — the configurable-endpoint and inline-streaming
// credential knobs (task 01a11f90).
//
// WHAT THIS FILE PINS, and why each half is a regression guard rather than a
// restatement of the code:
//
//   - an UNCONFIGURED machine must produce byte-for-byte the request it
//     produced before these knobs existed (the published endpoints), because
//     the whole point of adding a knob is that saying nothing changes nothing;
//   - a configured machine must actually USE the configured value on each of
//     the three channels in precedence order (flag > env > settings), which is
//     the "未配置→默认 / 配置→采用" pair the epic asks for per knob;
//   - the batch channel's TWO URLs must come from ONE root, so a regional base
//     cannot send the submit to one host and poll the other's task id;
//   - the streaming channel's credential FILE keeps working (old deployments),
//     and the inline pair answers when no file is there (the new route), while
//     naming the knob when NEITHER exists — before any audio is sent.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/doubaoasr"
	"github.com/nickhuang99/gopi/internal/doubaostream"
)

// writeSettingsJSON writes a settings document into a temp dir and points the
// run at it. It is the ONE fixture the endpoint/credential tests share, so the
// three channels are exercised against the same document shape.
func writeSettingsJSON(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	t.Setenv(config.SettingsEnv, path)
	return path
}

// isolateVoiceHome points the default credential/legacy paths at an empty
// directory, so a test can never read the operator's real ~/.doubao (the
// anti-pattern this suite's HOME-aware default exists to make unnecessary).
func isolateVoiceHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

// TestDoubaoEndpointsUnsetResolveToThePublishedDefaults is the "未配置 → 默认"
// half for the endpoint knobs: with no settings document, no env and no flag,
// the batch options carry exactly the published URLs and the streaming arm
// carries exactly the published endpoint.
func TestDoubaoEndpointsUnsetResolveToThePublishedDefaults(t *testing.T) {
	isolateVoiceHome(t)
	writeSettingsJSON(t, `{"doubao_api_key": "inline-key-for-this-test"}`)
	for _, e := range []string{config.DoubaoBaseURLEnv, config.DoubaoStreamEndpointEnv} {
		t.Setenv(e, "")
	}

	do, err := voiceResolveDoubaoOpts(&voiceOptions{engine: config.AudioEngineRecording})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if do.SubmitURL != doubaoasr.DefaultSubmitURL {
		t.Errorf("unconfigured submit URL = %q, want the published %q", do.SubmitURL, doubaoasr.DefaultSubmitURL)
	}
	if do.QueryURL != doubaoasr.DefaultQueryURL {
		t.Errorf("unconfigured query URL = %q, want the published %q", do.QueryURL, doubaoasr.DefaultQueryURL)
	}
	if got := voiceStreamEffectiveEndpoint(&voiceOptions{}); got != doubaostream.DefaultEndpoint {
		t.Errorf("unconfigured stream endpoint = %q, want the published %q", got, doubaostream.DefaultEndpoint)
	}
}

// TestDoubaoBaseURLDerivesBothURLsPerChannel is the "配置 → 采用" half, run on
// each of the three channels in turn. The point is not that the string survives
// — it is that BOTH URLs move together, which is what a single root buys.
func TestDoubaoBaseURLDerivesBothURLsPerChannel(t *testing.T) {
	const settingsRoot = "https://cn.example.internal/api/v3/auc/bigmodel"
	const envRoot = "https://env.example.internal/api/v3/auc/bigmodel"
	const flagRoot = "https://flag.example.internal/api/v3/auc/bigmodel"

	t.Run("settings-only", func(t *testing.T) {
		isolateVoiceHome(t)
		writeSettingsJSON(t, `{"doubao_api_key": "k", "doubao_base_url": "`+settingsRoot+`"}`)
		t.Setenv(config.DoubaoBaseURLEnv, "")
		do, err := voiceResolveDoubaoOpts(&voiceOptions{engine: config.AudioEngineRecording})
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if do.SubmitURL != doubaoasr.SubmitURLFor(settingsRoot) || do.QueryURL != doubaoasr.QueryURLFor(settingsRoot) {
			t.Errorf("settings root %q gave submit=%q query=%q", settingsRoot, do.SubmitURL, do.QueryURL)
		}
		if strings.Contains(do.SubmitURL, "openspeech.bytedance.com") || strings.Contains(do.QueryURL, "openspeech.bytedance.com") {
			t.Errorf("the published host survived a configured root: %q / %q", do.SubmitURL, do.QueryURL)
		}
	})

	t.Run("env-only", func(t *testing.T) {
		isolateVoiceHome(t)
		writeSettingsJSON(t, `{"doubao_api_key": "k"}`)
		t.Setenv(config.DoubaoBaseURLEnv, envRoot)
		do, err := voiceResolveDoubaoOpts(&voiceOptions{engine: config.AudioEngineRecording})
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if do.SubmitURL != doubaoasr.SubmitURLFor(envRoot) || do.QueryURL != doubaoasr.QueryURLFor(envRoot) {
			t.Errorf("env root %q gave submit=%q query=%q", envRoot, do.SubmitURL, do.QueryURL)
		}
	})

	t.Run("flag-only", func(t *testing.T) {
		isolateVoiceHome(t)
		writeSettingsJSON(t, `{"doubao_api_key": "k"}`)
		t.Setenv(config.DoubaoBaseURLEnv, "")
		do, err := voiceResolveDoubaoOpts(&voiceOptions{engine: config.AudioEngineRecording, doubaoBaseURL: flagRoot})
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if do.SubmitURL != doubaoasr.SubmitURLFor(flagRoot) || do.QueryURL != doubaoasr.QueryURLFor(flagRoot) {
			t.Errorf("flag root %q gave submit=%q query=%q", flagRoot, do.SubmitURL, do.QueryURL)
		}
	})

	// The per-URL flags are MORE SPECIFIC than the root: a caller who names one
	// URL gets that URL, while the other keeps deriving from the root. Without
	// this arm the two knobs would silently have to be equal.
	t.Run("per-url-flag-outranks-the-root", func(t *testing.T) {
		isolateVoiceHome(t)
		writeSettingsJSON(t, `{"doubao_api_key": "k", "doubao_base_url": "`+settingsRoot+`"}`)
		t.Setenv(config.DoubaoBaseURLEnv, "")
		one := "https://only-submit.example.internal/submit"
		do, err := voiceResolveDoubaoOpts(&voiceOptions{engine: config.AudioEngineRecording, doubaoSubmit: one})
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if do.SubmitURL != one {
			t.Errorf("submit URL = %q, want the explicitly named %q", do.SubmitURL, one)
		}
		if do.QueryURL != doubaoasr.QueryURLFor(settingsRoot) {
			t.Errorf("query URL = %q, want the root-derived %q — naming one URL must not un-derive the other",
				do.QueryURL, doubaoasr.QueryURLFor(settingsRoot))
		}
	})
}

// TestDoubaoStreamEndpointDefaultAndConfigured is the streaming endpoint's
// "未配置→默认 / 配置→采用" pair, plus the shape refusal: the pre-flight check
// must name the knob when the configured value is not a WebSocket URL, before
// the microphone opens.
func TestDoubaoStreamEndpointDefaultAndConfigured(t *testing.T) {
	isolateVoiceHome(t)

	// A working credential, so the pre-flight failure below can only be about
	// the ENDPOINT — a test where two things are wrong proves neither.
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, doubaostream.DefaultCredentialDir), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	credPath := doubaostream.DefaultCredentialPath(home)
	if err := os.WriteFile(credPath, []byte("APPID=a\nTOKEN=b\n"), 0o600); err != nil {
		t.Fatalf("write credential: %v", err)
	}

	const want = "wss://stream.example.internal/api/v2/asr"

	writeSettingsJSON(t, `{"doubao_stream_endpoint": "`+want+`"}`)
	t.Setenv(config.DoubaoStreamEndpointEnv, "")
	if got := voiceStreamEffectiveEndpoint(&voiceOptions{}); got != want {
		t.Errorf("settings endpoint = %q, want %q", got, want)
	}
	if reason := voiceStreamDepsReady(&voiceOptions{}); reason != "" {
		t.Errorf("a configured, well-formed endpoint was refused: %s", reason)
	}

	// env outranks settings; the flag outranks both.
	const fromEnv = "wss://env.example.internal/api/v2/asr"
	t.Setenv(config.DoubaoStreamEndpointEnv, fromEnv)
	if got := voiceStreamEffectiveEndpoint(&voiceOptions{}); got != fromEnv {
		t.Errorf("env endpoint = %q, want %q", got, fromEnv)
	}
	const fromFlag = "wss://flag.example.internal/api/v2/asr"
	if got := voiceStreamEffectiveEndpoint(&voiceOptions{doubaoStreamEndpoint: fromFlag}); got != fromFlag {
		t.Errorf("flag endpoint = %q, want %q", got, fromFlag)
	}

	// Unset everywhere → the package's published endpoint.
	t.Setenv(config.DoubaoStreamEndpointEnv, "")
	writeSettingsJSON(t, `{}`)
	if got := voiceStreamEffectiveEndpoint(&voiceOptions{}); got != doubaostream.DefaultEndpoint {
		t.Errorf("unset endpoint = %q, want the published %q", got, doubaostream.DefaultEndpoint)
	}

	// A non-wss value is refused BY NAME, naming the knob that set it.
	t.Setenv(config.DoubaoStreamEndpointEnv, "https://not-a-websocket.example.internal/api/v2/asr")
	reason := voiceStreamDepsReady(&voiceOptions{})
	if reason == "" {
		t.Fatal("an https:// endpoint was accepted for a WebSocket channel")
	}
	for _, want := range []string{config.DoubaoStreamEndpointEnv, voiceFlagDoubaoStreamEndpoint} {
		if !strings.Contains(reason, want) {
			t.Errorf("the refusal does not name %q: %s", want, reason)
		}
	}
}

// TestStreamingCredentialFileStillWins is the BACKWARD-COMPATIBILITY half: a
// host that deployed the KEY=VALUE file (the only route before this change)
// must keep working, and the file's values must be the ones used even when an
// inline pair is also configured.
func TestStreamingCredentialFileStillWins(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, doubaostream.DefaultCredentialDir), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := doubaostream.DefaultCredentialPath(home)
	if err := os.WriteFile(path, []byte("APPID=from-file\nTOKEN=from-file\n"), 0o600); err != nil {
		t.Fatalf("write credential: %v", err)
	}
	// An inline pair is present TOO, so the assertion is about precedence and
	// not about the inline route being broken.
	writeSettingsJSON(t, `{"doubao_app_id": "from-settings", "doubao_token": "from-settings"}`)
	t.Setenv(config.DoubaoAppIDEnv, "")
	t.Setenv(config.DoubaoTokenEnv, "")

	cred, used, err := voiceStreamCredentialSource(&voiceOptions{}).load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if used != path {
		t.Errorf("credential came from %q, want the file %q — a deployed file must keep winning", used, path)
	}
	if cred.AppID != "from-file" || cred.Token != "from-file" {
		t.Errorf("credential values came from somewhere else (appid=%q)", cred.AppID)
	}
}

// TestStreamingCredentialInlinePairWithoutAnyFile is the NEW route, and the
// case the epic's acceptance calls "无文件但 settings 配了": no credential file
// exists anywhere, the settings document carries the APPID/TOKEN pair, and the
// run resolves to it — with the path reported as "" (nothing was read from a
// file) so every message can say so honestly.
func TestStreamingCredentialInlinePairWithoutAnyFile(t *testing.T) {
	isolateVoiceHome(t) // an EMPTY home: no ~/.doubao/v2_voice.env
	writeSettingsJSON(t, `{"doubao_app_id": "settings-appid", "doubao_token": "settings-token"}`)
	t.Setenv(config.DoubaoAppIDEnv, "")
	t.Setenv(config.DoubaoTokenEnv, "")

	cred, used, err := voiceStreamCredentialSource(&voiceOptions{}).load()
	if err != nil {
		t.Fatalf("inline pair was not accepted: %v", err)
	}
	if used != "" {
		t.Errorf("credential reported as read from %q — an inline pair comes from no file", used)
	}
	if cred.AppID != "settings-appid" || cred.Token != "settings-token" {
		t.Errorf("inline pair not adopted (appid=%q)", cred.AppID)
	}
	if label := voiceStreamCredentialLabel(used, config.DoubaoAppIDSettingsKey); strings.Contains(label, "settings-token") {
		t.Errorf("the credential label leaked a value: %q", label)
	}
	if reason := voiceStreamDepsReady(&voiceOptions{}); reason != "" {
		t.Errorf("the pre-flight check refused a configured inline pair: %s", reason)
	}

	// The env channels outrank the settings document, and either half of the
	// pair can come from there.
	t.Setenv(config.DoubaoAppIDEnv, "env-appid")
	cred, _, err = voiceStreamCredentialSource(&voiceOptions{}).load()
	if err != nil {
		t.Fatalf("mixed channels were refused: %v", err)
	}
	if cred.AppID != "env-appid" || cred.Token != "settings-token" {
		t.Errorf("env/settings mix resolved to appid=%q token-set=%v", cred.AppID, cred.Token != "")
	}
}

// TestStreamingCredentialMissingNamesBothChannelsAndSendsNothing is the
// Rule 15 half: with neither a file nor a complete inline pair, the refusal
// happens in the PRE-FLIGHT check (before any audio is opened), it names every
// knob that would fix it, and it carries no value.
func TestStreamingCredentialMissingNamesBothChannelsAndSendsNothing(t *testing.T) {
	isolateVoiceHome(t) // no file
	writeSettingsJSON(t, `{"doubao_app_id": "half-a-pair"}`)
	t.Setenv(config.DoubaoAppIDEnv, "")
	t.Setenv(config.DoubaoTokenEnv, "")

	reason := voiceStreamDepsReady(&voiceOptions{})
	if reason == "" {
		t.Fatal("an incomplete credential was reported as ready")
	}
	for _, want := range []string{
		config.DoubaoKeyFileSettingsKey,
		config.DoubaoAppIDSettingsKey,
		config.DoubaoTokenSettingsKey,
		config.DoubaoAppIDEnv,
		config.DoubaoTokenEnv,
	} {
		if !strings.Contains(reason, want) {
			t.Errorf("the refusal does not name %q:\n%s", want, reason)
		}
	}
	if strings.Contains(reason, "half-a-pair") {
		t.Errorf("the refusal printed a credential value:\n%s", reason)
	}
}

// TestVoiceHelpHasNoSplitDuplicatedFragment: `gopi tool voice_input -h` shipped
// a duplicated FRAGMENT split across two lines —
//
//	A missing key is refused BEFORE any audio is sent, and the
//	refuses BEFORE any audio is sent, and the
//	refusal names the way back to --engine local.
//
// — because the page is one long const accumulated by several edits. Whole-line
// and whole-page repetition checks are the WRONG instrument here (the page
// legitimately repeats a phrase, e.g. a flag's three channels, in two engine
// blocks, and it repeats the CER comparison prose): the defect's signature is
// narrower. A fragment was written TWICE IN A ROW on two ADJACENT lines, so the
// check is the longest run of consecutive words two neighbouring lines share.
//
// The red proof is in the test: the detector is run over the shipped fragment
// above, so a version of it that stopped firing would fail here rather than read
// green over any help text.
func TestVoiceHelpHasNoSplitDuplicatedFragment(t *testing.T) {
	const minSharedWords = 6

	if run := sharedWordRun(voiceHelpDuplicationProbe, minSharedWords); run == "" {
		t.Fatalf("the detector no longer fires on the defect it exists for (%d-word run over the shipped fragment)",
			minSharedWords)
	}

	lines := strings.Split(voiceUsageText(), "\n")
	for i := 0; i+1 < len(lines); i++ {
		pair := strings.TrimSpace(lines[i]) + "\n" + strings.TrimSpace(lines[i+1])
		if run := sharedWordRun(pair, minSharedWords); run != "" {
			t.Errorf("lines %d-%d of the help repeat a %d+-word fragment — a duplicated edit:\n  %q\n  next line: %q",
				i+1, i+2, minSharedWords, run, strings.TrimSpace(lines[i+1]))
		}
	}
}

// voiceHelpDuplicationProbe is the VERBATIM two-line fragment the help shipped
// (task 01a11f90's brief quotes it). It is a fixture, not documentation: it
// exists so the detector above is proven to fire on the real defect.
const voiceHelpDuplicationProbe = `A missing key is refused BEFORE any audio is sent, and the
refuses BEFORE any audio is sent, and the`

// sharedWordRun returns the longest run of consecutive words two adjacent lines
// share, when that run reaches minWords — "" otherwise. Runs shorter than the
// threshold are ordinary English ("the", "and the"), which is why the threshold
// is a parameter and the caller states it.
func sharedWordRun(text string, minWords int) string {
	lines := strings.Split(text, "\n")
	if len(lines) != 2 {
		return ""
	}
	a, b := strings.Fields(lines[0]), strings.Fields(lines[1])
	best := ""
	for i := range a {
		for j := range b {
			n := 0
			for i+n < len(a) && j+n < len(b) && a[i+n] == b[j+n] {
				n++
			}
			if n < minWords {
				continue
			}
			if run := strings.Join(a[i:i+n], " "); len(run) > len(best) {
				best = run
			}
		}
	}
	return best
}
