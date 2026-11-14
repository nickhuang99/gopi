package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/doubaosauc"
	"github.com/nickhuang99/gopi/internal/doubaostream"
)

// ── the FOURTH arm of the engine dispatch ──────────────────────────────
//
// The accepted-value LIST is asserted once, in cli_voice_stream_test.go
// (TestVoiceEngineValuesCarryEveryEngine): a second copy of that assertion would
// pass while the first went red, which is the drift Rule 13/14 exist to stop.
//
// These tests are about the SEAM, exactly like the streaming arm's: which engine
// a run resolves to, what the help claims, what the pre-flight refusal says, and
// where the credential comes from. The v3 protocol itself is tested in
// internal/doubaosauc; the live service cannot be a unit test, and the epic's
// acceptance run is in the task report.

// isolateSaucSettings points the settings resolver at a document of the caller's
// choosing and the key-file knob at a path that does not exist, so a test never
// reaches the operator's own credential.
func isolateSaucSettings(t *testing.T, body string) string {
	t.Helper()
	missing := filepath.Join(t.TempDir(), "no-credential.env")
	t.Setenv(config.AudioKeyFileEnv, missing)
	settings := filepath.Join(t.TempDir(), "settings.json")
	if body == "" {
		body = "{}\n"
	}
	if err := os.WriteFile(settings, []byte(body), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	t.Setenv(config.SettingsEnv, settings)
	return missing
}

// TestVoiceSaucEngineIsNeverTheDefault drives the resolver's real paths with the
// v3 engine nowhere in the environment.
func TestVoiceSaucEngineIsNeverTheDefault(t *testing.T) {
	t.Setenv(config.AudioEngineEnv, "")
	isolateSaucSettings(t, "")
	if got := voiceResolveEngine(&voiceOptions{}); got == config.AudioEngineSauc {
		t.Fatalf("settings choosing nothing → resolved %q, which must never be the default", got)
	}
}

// TestVoiceSaucEngineResolvesByEveryChannel is the other half: the value must
// actually select the arm, through all three rungs, and report its OWN label —
// a client that must know whether its audio was uploaded and on which protocol
// parses that label.
func TestVoiceSaucEngineResolvesByEveryChannel(t *testing.T) {
	t.Setenv(config.AudioEngineEnv, config.AudioEngineSauc)
	isolateSaucSettings(t, "")
	if got := voiceResolveEngine(&voiceOptions{}); got != config.AudioEngineSauc {
		t.Fatalf("the environment channel resolved %q, want %q", got, config.AudioEngineSauc)
	}

	t.Setenv(config.AudioEngineEnv, "")
	isolateSaucSettings(t, `{"`+config.AudioEngineSettingsKey+`":"`+config.AudioEngineSauc+`"}`+"\n")
	if got := voiceResolveEngine(&voiceOptions{}); got != config.AudioEngineSauc {
		t.Fatalf("the settings channel resolved %q, want %q", got, config.AudioEngineSauc)
	}

	opts := &voiceOptions{engine: config.AudioEngineSauc}
	if got := voiceResolveEngine(opts); got != config.AudioEngineSauc {
		t.Fatalf("the flag channel resolved %q, want %q", got, config.AudioEngineSauc)
	}
	if got := voiceEngineLabel(opts); got != voiceSaucEngineLabel {
		t.Errorf("label = %q, want %q", got, voiceSaucEngineLabel)
	}
	// The four engines must report four DIFFERENT labels.
	labels := map[string]string{}
	for _, e := range voiceEngineValues {
		labels[voiceEngineLabel(&voiceOptions{engine: e})] = e
	}
	if len(labels) != len(voiceEngineValues) {
		t.Errorf("the engines do not have %d distinct labels: %v", len(voiceEngineValues), labels)
	}
}

// TestVoiceSaucRefusalNamesBothCredentialShapes is the refusal the epic asks
// for: a run with no credential is refused BEFORE any audio moves, and the
// message names the TWO shapes and the FILE it looked for — because the shape
// mismatch is precisely what the vendor's 403 hides.
func TestVoiceSaucRefusalNamesBothCredentialShapes(t *testing.T) {
	missing := isolateSaucSettings(t, "")
	t.Setenv(config.AudioEngineEnv, "")
	opts := &voiceOptions{engine: config.AudioEngineSauc, audio: "probe.wav"}
	got := voiceSaucDepsReady(opts)
	if got == "" {
		t.Fatalf("a run with no credential passed the pre-flight check")
	}
	for _, want := range []string{
		missing,
		doubaosauc.HeaderAppKey,
		doubaosauc.HeaderAccessKey,
		// The way back to on-machine transcription is owed by every refusal on
		// this surface.
		config.AudioEngineLocal,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the refusal does not name %q: %s", want, got)
		}
	}
}

// TestVoiceSaucPreflightRejectsANonWSSEndpoint asserts the shape check: a
// configured endpoint that is not a wss:// URL is refused before a socket is
// opened, naming the knob.
func TestVoiceSaucPreflightRejectsANonWSSEndpoint(t *testing.T) {
	dir := t.TempDir()
	cred := filepath.Join(dir, "cred.env")
	if err := os.WriteFile(cred, []byte("APPID=app\nTOKEN=token\n"), 0o600); err != nil {
		t.Fatalf("write the credential: %v", err)
	}
	isolateSaucSettings(t, `{"`+config.AudioEngineParamSettingsKey+`":{"`+config.AudioEngineSauc+
		`":{"`+config.AudioParamKeyEndpoint+`":"https://openspeech.bytedance.com/api/v3/sauc/bigmodel_nostream"}}}`+"\n")
	t.Setenv(config.AudioKeyFileEnv, cred)
	got := voiceSaucDepsReady(&voiceOptions{engine: config.AudioEngineSauc})
	if !strings.Contains(got, voiceSaucURLSchemePrefix) {
		t.Fatalf("a non-wss endpoint was accepted: %q", got)
	}
}

// TestVoiceSaucCredentialComesFromTheExistingKeyFileChannel asserts the ONE knob
// decision: the v3 engine reads the SAME key-file rungs the other cloud arms do,
// so a host keeps one credential document rather than one per protocol.
func TestVoiceSaucCredentialComesFromTheExistingKeyFileChannel(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "custom.env")
	if err := os.WriteFile(file, []byte("APPID=1\nTOKEN=2\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	isolateSaucSettings(t, "")
	t.Setenv(config.AudioKeyFileEnv, file)
	if got := voiceSaucCredentialLabel(&voiceOptions{}); got != file {
		t.Errorf("the env rung resolved %q, want %q", got, file)
	}
	if got := voiceSaucCredentialLabel(&voiceOptions{doubaoKeyFile: file}); got != file {
		t.Errorf("the flag rung resolved %q, want %q", got, file)
	}
	// With no channel naming a file, the conventional location answers.
	t.Setenv(config.AudioKeyFileEnv, "")
	if got := voiceSaucCredentialLabel(&voiceOptions{}); !strings.HasSuffix(got,
		filepath.Join(doubaostream.DefaultCredentialDir, doubaostream.DefaultCredentialFile)) {
		t.Errorf("the default credential path is %q", got)
	}
}

// ── the credential's OWN rungs (2026-10-14) ────────────────────────────
//
// Until today the v3 pair could only live in the vendor's own file, so every
// test below this line was impossible to write: there was one rung. These are
// the four facts the epic asks for — the inline pair answers, a named file still
// outranks it, half a pair is refused BY NAME, and the fallback to the vendor's
// file is ANNOUNCED.

// saucInlineSettingsBody renders a settings document carrying the v3 pair (and
// nothing else), so a test states the knobs it means through the block's own
// constants rather than through a hand-typed JSON blob (Rule 1/13).
func saucInlineSettingsBody(appID, token string) string {
	return `{"` + config.AudioEngineParamSettingsKey + `":{"` + config.AudioEngineSauc + `":{` +
		`"` + config.AudioParamKeyAppID + `":"` + appID + `",` +
		`"` + config.AudioParamKeyToken + `":"` + token + `"}}}` + "\n"
}

// isolateSaucCredentialHome points $HOME at an empty directory, so the LEGACY
// rung (~/.doubao/v2_voice.env) is a path that does not exist unless the test
// puts one there. It is a helper of its own because the fallback tests and the
// no-fallback tests must not be able to reach the operator's own vendor file.
func isolateSaucCredentialHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

// T14a — the inline pair answers, and the banner names the SETTINGS path rather
// than a file. This is the whole point of the epic: a host with no
// ~/.doubao/v2_voice.env transcribes.
func TestVoiceSaucCredentialFromSettingsPair(t *testing.T) {
	isolateSaucCredentialHome(t)
	isolateSaucSettings(t, saucInlineSettingsBody("app-1", "token-1"))
	// The key-FILE channel is cleared: nothing may outrank the pair under test.
	t.Setenv(config.AudioKeyFileEnv, "")

	if got := voiceSaucCredentialLabel(&voiceOptions{}); got != voiceSaucInlineSettingsLabel() {
		t.Fatalf("the inline pair resolved %q, want %q", got, voiceSaucInlineSettingsLabel())
	}
	src := voiceSaucCredentialSource(&voiceOptions{})
	cred, label, err := src.resolve()
	if err != nil {
		t.Fatalf("the inline pair did not resolve: %v", err)
	}
	if cred.AppID != "app-1" || cred.Token != "token-1" {
		t.Errorf("the pair did not reach the request")
	}
	if label != voiceSaucInlineSettingsLabel() {
		t.Errorf("the origin label is %q", label)
	}
	if note := src.fallbackNote(); note != "" {
		t.Errorf("an inline pair must not announce a fallback: %s", note)
	}
	// The banner and the start line carry the origin, and neither may carry a
	// value.
	for _, line := range []string{
		voiceSaucPrivacyNotice(&voiceOptions{engine: config.AudioEngineSauc}),
	} {
		if !strings.Contains(line, voiceSaucInlineSettingsLabel()) {
			t.Errorf("the notice does not name the settings path: %s", line)
		}
		for _, leaked := range []string{"app-1", "token-1"} {
			if strings.Contains(line, leaked) {
				t.Errorf("the notice leaked a credential value: %s", line)
			}
		}
	}
}

// T14b — an explicitly NAMED file outranks the inline pair, and the operator
// learns which one answered.
func TestVoiceSaucNamedFileOutranksTheInlinePair(t *testing.T) {
	isolateSaucCredentialHome(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "named.env")
	if err := os.WriteFile(file, []byte("APPID=file-app\nTOKEN=file-token\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	isolateSaucSettings(t, saucInlineSettingsBody("app-1", "token-1"))

	src := voiceSaucCredentialSource(&voiceOptions{doubaoKeyFile: file})
	cred, label, err := src.resolve()
	if err != nil {
		t.Fatalf("the named file did not resolve: %v", err)
	}
	if label != file {
		t.Fatalf("the named file did not win: label %q", label)
	}
	if cred.AppID != "file-app" {
		t.Errorf("the request did not carry the file's pair")
	}
}

// T14c — HALF A PAIR IS REFUSED BY NAME, and the refusal names the missing half
// and does not fall back to any other rung.
func TestVoiceSaucHalfStatedPairIsRefusedByName(t *testing.T) {
	home := isolateSaucCredentialHome(t)
	// A COMPLETE legacy file is present, so a resolver that "completed" the
	// stated half from somewhere else would pass this test silently — the
	// refusal is only proven by refusing WHILE another rung could have answered.
	if err := os.MkdirAll(filepath.Join(home, doubaostream.DefaultCredentialDir), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(doubaostream.DefaultCredentialPath(home),
		[]byte("APPID=legacy-app\nTOKEN=legacy-token\n"), 0o600); err != nil {
		t.Fatalf("write the legacy file: %v", err)
	}
	isolateSaucSettings(t, saucInlineSettingsBody("app-1", ""))
	t.Setenv(config.AudioKeyFileEnv, "")

	src := voiceSaucCredentialSource(&voiceOptions{})
	_, label, err := src.resolve()
	if err == nil {
		t.Fatal("a half-stated pair resolved")
	}
	for _, want := range []string{config.AudioParamKeyToken, voiceSaucInlineSettingsLabel()} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "legacy-app") || strings.Contains(err.Error(), "legacy-token") {
		t.Errorf("the refusal leaked a credential value: %v", err)
	}
	// The PRE-FLIGHT check is the surface an operator actually meets, and it
	// must be the same sentence (Rule 2).
	missing := voiceSaucDepsReady(&voiceOptions{engine: config.AudioEngineSauc})
	if !strings.Contains(missing, config.AudioParamKeyToken) {
		t.Errorf("the pre-flight check did not carry the half-stated refusal: %q", missing)
	}
	_ = label
}

// T14d — the fallback to the vendor's own file is ANNOUNCED, once, naming the
// path and the settings that supersede it.
func TestVoiceSaucLegacyFallbackIsAnnounced(t *testing.T) {
	home := isolateSaucCredentialHome(t)
	if err := os.MkdirAll(filepath.Join(home, doubaostream.DefaultCredentialDir), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	legacy := doubaostream.DefaultCredentialPath(home)
	if err := os.WriteFile(legacy, []byte("APPID=legacy-app\nTOKEN=legacy-token\n"), 0o600); err != nil {
		t.Fatalf("write the legacy file: %v", err)
	}
	// A settings document with NO pair, and no key-file channel.
	isolateSaucSettings(t, "")
	t.Setenv(config.AudioKeyFileEnv, "")

	src := voiceSaucCredentialSource(&voiceOptions{})
	cred, label, err := src.resolve()
	if err != nil {
		t.Fatalf("the legacy file did not answer: %v", err)
	}
	if label != legacy || cred.AppID != "legacy-app" {
		t.Fatalf("the legacy rung resolved %q", label)
	}
	note := src.fallbackNote()
	if note == "" {
		t.Fatal("the fallback to the legacy doubao file was SILENT")
	}
	for _, want := range []string{legacy, voiceSaucInlineSettingsLabel()} {
		if !strings.Contains(note, want) {
			t.Errorf("the announcement does not name %q: %s", want, note)
		}
	}
	if strings.Contains(note, "legacy-app") || strings.Contains(note, "legacy-token") {
		t.Errorf("the announcement leaked a credential value: %s", note)
	}
	// NO fallback is announced when the pair is configured — the note is about
	// what THIS run reads, not about what exists on the host.
	isolateSaucSettings(t, saucInlineSettingsBody("app-1", "token-1"))
	if got := voiceSaucCredentialSource(&voiceOptions{}).fallbackNote(); got != "" {
		t.Errorf("an inline pair still announced the fallback: %s", got)
	}
}

// T14e — an explicitly named file that cannot be READ falls through to a
// complete inline pair, ANNOUNCED (never silent).
func TestVoiceSaucUnreadableNamedFileFallsThroughAnnounced(t *testing.T) {
	isolateSaucCredentialHome(t)
	missing := filepath.Join(t.TempDir(), "not-there.env")
	isolateSaucSettings(t, saucInlineSettingsBody("app-1", "token-1"))
	t.Setenv(config.AudioKeyFileEnv, missing)

	src := voiceSaucCredentialSource(&voiceOptions{})
	_, label, err := src.resolve()
	if err != nil {
		t.Fatalf("the inline pair did not answer: %v", err)
	}
	if label != voiceSaucInlineSettingsLabel() {
		t.Errorf("the substitution resolved %q", label)
	}
	note := src.substitutedNote()
	if note == "" {
		t.Fatal("the substitution was SILENT")
	}
	if !strings.Contains(note, missing) {
		t.Errorf("the note does not name the file that failed: %s", note)
	}
}

// TestVoiceSaucBlockDefaultsAreThePackagesOwn asserts Rule 2/14 for the two
// knobs of the new block: an unset block resolves to the PACKAGE's published
// default rather than to a second copy of the URL or the resource id typed here.
func TestVoiceSaucBlockDefaultsAreThePackagesOwn(t *testing.T) {
	isolateSaucSettings(t, "")
	if got := voiceSaucEffectiveEndpoint(); got != doubaosauc.DefaultEndpoint {
		t.Errorf("the default endpoint is %q, want the package's %q", got, doubaosauc.DefaultEndpoint)
	}
	if got := voiceSaucEffectiveResourceID(); got != doubaosauc.DefaultResourceID {
		t.Errorf("the default resource id is %q, want the package's %q", got, doubaosauc.DefaultResourceID)
	}
	isolateSaucSettings(t, `{"`+config.AudioEngineParamSettingsKey+`":{"`+config.AudioEngineSauc+`":{`+
		`"`+config.AudioParamKeyEndpoint+`":"wss://example.invalid/v3","`+config.AudioParamKeyResourceID+`":"volc.bigasr.sauc.duration"}}}`+"\n")
	if got := voiceSaucEffectiveEndpoint(); got != "wss://example.invalid/v3" {
		t.Errorf("the block's endpoint did not win: %q", got)
	}
	if got := voiceSaucEffectiveResourceID(); got != "volc.bigasr.sauc.duration" {
		t.Errorf("the block's resource id did not win: %q", got)
	}
}

// TestVoiceSaucHelpRendersItsPlaceholderAndItsShape asserts the help page: the
// sauc placeholder must be replaced (a literal "{engineSauc}" would ship), the
// engine must be named, and the page must state the credential SHAPE — the one
// fact an operator debugging a 403 cannot guess.
func TestVoiceSaucHelpRendersItsPlaceholderAndItsShape(t *testing.T) {
	help := voiceUsageText()
	if strings.Contains(help, voiceEngineSaucPlaceholder) {
		t.Errorf("the help still carries the placeholder %q", voiceEngineSaucPlaceholder)
	}
	if !strings.Contains(help, config.AudioEngineSauc) {
		t.Errorf("the help does not name the %s engine", config.AudioEngineSauc)
	}
	for _, want := range []string{doubaosauc.HeaderAppKey, doubaosauc.HeaderAccessKey, doubaosauc.DefaultResourceID} {
		if !strings.Contains(help, want) {
			t.Errorf("the help does not state %q", want)
		}
	}
}

// TestVoiceSaucPrivacyNoticeCarriesNoCredentialValue is the redaction contract
// at the CLI seam: the notice a run prints before uploading names the endpoint,
// the resource id and the credential FILE — and never a value from it.
func TestVoiceSaucPrivacyNoticeCarriesNoCredentialValue(t *testing.T) {
	const secret = "SECRET-TOKEN-9c1f"
	dir := t.TempDir()
	cred := filepath.Join(dir, "cred.env")
	if err := os.WriteFile(cred, []byte("APPID=app-7\nTOKEN="+secret+"\n"), 0o600); err != nil {
		t.Fatalf("write the credential: %v", err)
	}
	isolateSaucSettings(t, "")
	t.Setenv(config.AudioKeyFileEnv, cred)
	notice := voiceSaucPrivacyNotice(&voiceOptions{engine: config.AudioEngineSauc})
	if !strings.Contains(notice, cred) {
		t.Errorf("the notice does not name the credential file: %s", notice)
	}
	for _, leaked := range []string{secret, "app-7"} {
		if strings.Contains(notice, leaked) {
			t.Errorf("the notice leaked a credential value: %s", notice)
		}
	}
	for _, want := range []string{doubaosauc.DefaultEndpoint, doubaosauc.DefaultResourceID, config.AudioEngineSauc} {
		if !strings.Contains(notice, want) {
			t.Errorf("the notice does not state %q: %s", want, notice)
		}
	}
}
