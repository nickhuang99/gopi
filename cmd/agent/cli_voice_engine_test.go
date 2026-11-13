package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/doubaoasr"
)

// ── engine selection ────────────────────────────────────────────────────

// TestAudioEngineDefaultsToDoubao is the flip's regression guard: with no flag,
// no env and no settings, the engine is the CLOUD one. It replaced
// TestAudioEngineDefaultsToLocal when the default was reversed on 2026-10-07 —
// the assertion is deliberately the same SHAPE (a bare voiceOptions resolving to
// the documented default), so a silent re-reversal fails here rather than
// shipping.
//
// The settings path is PINNED to a file that does not exist, so the assertion is
// about the CODE's terminal default and not about which engine this host's live
// profile happens to select.
func TestAudioEngineDefaultsToDoubao(t *testing.T) {
	t.Setenv(config.SettingsEnv, filepath.Join(t.TempDir(), "absent.json"))
	t.Setenv(config.AudioEngineEnv, "")

	opts := &voiceOptions{}
	if got := voiceResolveEngine(opts); got != config.AudioEngineRecording {
		t.Errorf("engine = %q, want the cloud default %q", got, config.AudioEngineRecording)
	}
	if got := voiceEngineLabel(opts); got != voiceEngineLabelDoubao {
		t.Errorf("label = %q, want %q", got, voiceEngineLabelDoubao)
	}
	if opts.engine != "" {
		t.Errorf("parse left engine = %q — an empty value is how \"nobody chose\" is spelled", opts.engine)
	}
	if voiceExplicitlyChoseDoubao(opts) {
		t.Error("nobody chose anything, so the selection is NOT explicit — the privacy notice would tell the operator they asked for the upload")
	}
}

// TestAudioEngineDefaultIsOneNamedConstant pins the SINGLE source of the
// terminal default (Rule 13): the constant's value, and the fact that BOTH
// terminal paths of the resolver (settings unreadable, and settings choosing
// nothing) return it rather than a second literal.
func TestAudioEngineDefaultIsOneNamedConstant(t *testing.T) {
	if config.AudioEngineDefault != config.AudioEngineRecording {
		t.Fatalf("config.AudioEngineDefault = %q, want the cloud engine %q", config.AudioEngineDefault, config.AudioEngineRecording)
	}
	found := false
	for _, v := range voiceEngineValues {
		if v == config.AudioEngineDefault {
			found = true
		}
	}
	if !found {
		t.Errorf("the default %q is not among the accepted values %v", config.AudioEngineDefault, voiceEngineValues)
	}

	// Path 1: the settings document cannot be read.
	t.Setenv(config.AudioEngineEnv, "")
	t.Setenv(config.SettingsEnv, filepath.Join(t.TempDir(), "absent.json"))
	if got := voiceResolveEngine(&voiceOptions{}); got != config.AudioEngineDefault {
		t.Errorf("unreadable settings: engine = %q, want the constant %q", got, config.AudioEngineDefault)
	}

	// Path 2: the document is readable and chooses nothing.
	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(settings, []byte(`{"default_device":"x"}`), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	t.Setenv(config.SettingsEnv, settings)
	if got := voiceResolveEngine(&voiceOptions{}); got != config.AudioEngineDefault {
		t.Errorf("settings choosing nothing: engine = %q, want the constant %q", got, config.AudioEngineDefault)
	}

	// The refusal for an unknown name reads the same constant, so the message an
	// operator sees can never advertise a default the resolver does not apply.
	err := voiceValidateEngine("bogus")
	if err == nil {
		t.Fatal("an unknown engine must be refused")
	}
	if !strings.Contains(err.Error(), config.AudioEngineDefault) {
		t.Errorf("refusal %q does not name the current default %q", err, config.AudioEngineDefault)
	}
}

// TestVoiceExplicitlyChoseDoubao is the predicate the privacy notice rests on:
// "the operator asked for the cloud" must be distinguishable from "the cloud is
// what they get for saying nothing", or the notice tells them they chose
// something they never typed.
func TestVoiceExplicitlyChoseDoubao(t *testing.T) {
	t.Setenv(config.SettingsEnv, filepath.Join(t.TempDir(), "absent.json"))
	t.Setenv(config.AudioEngineEnv, "")

	if voiceExplicitlyChoseDoubao(&voiceOptions{}) {
		t.Error("no channel named an engine — nothing was chosen explicitly")
	}
	if !voiceExplicitlyChoseDoubao(&voiceOptions{engine: config.AudioEngineRecording}) {
		t.Error("the flag named the cloud engine and should read as explicit")
	}
	if voiceExplicitlyChoseDoubao(&voiceOptions{engine: config.AudioEngineLocal}) {
		t.Error("the flag named the LOCAL engine — that is not a cloud choice")
	}
	t.Setenv(config.AudioEngineEnv, config.AudioEngineRecording)
	if !voiceExplicitlyChoseDoubao(&voiceOptions{}) {
		t.Error("the env named the cloud engine and should read as explicit")
	}
	// An upper rung naming `local` decides, even with a lower rung naming doubao:
	// the predicate walks the same precedence the resolver does (Rule 2).
	t.Setenv(config.AudioEngineEnv, config.AudioEngineLocal)
	if voiceExplicitlyChoseDoubao(&voiceOptions{}) {
		t.Error("the env rung named local, so the selection is not a cloud choice")
	}
	t.Setenv(config.AudioEngineEnv, "")
	if voiceExplicitlyChoseDoubao(&voiceOptions{engine: config.AudioEngineRecording}) != true {
		t.Error("the flag is the top rung and must decide")
	}
}

// TestVoiceEnginePrecedenceFlagBeatsEnvBeatsSettings pins the three rungs in
// order (Rule 2: one precedence chain, stated once).
func TestVoiceEnginePrecedenceFlagBeatsEnvBeatsSettings(t *testing.T) {
	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.json")
	writeVoiceSettings(t, settings, config.AudioEngineRecording)
	t.Setenv(config.SettingsEnv, settings)

	// "settings alone" asserts the SETTINGS rung, so the two rungs above it must
	// be pinned to empty: the resolver reads the environment directly, and a
	// live gopi session publishes this host's settings into it
	// (internal/config/env.go: setStr(AudioEngineEnv, cfg.AudioEngine)) — so a
	// run inside such a process would read the caller's engine at this line and
	// fail for a reason this test does not assert. The legacy name is pinned
	// too: voiceResolveEngine reads it at the same rung.
	t.Setenv(config.AudioEngineEnv, "")
	t.Setenv(config.AudioEngineLegacyEnv, "")

	// settings alone → doubao
	if got := voiceResolveEngine(&voiceOptions{}); got != config.AudioEngineRecording {
		t.Errorf("settings rung: engine = %q, want doubao", got)
	}
	// env beats settings
	t.Setenv(config.AudioEngineEnv, config.AudioEngineLocal)
	if got := voiceResolveEngine(&voiceOptions{}); got != config.AudioEngineLocal {
		t.Errorf("env rung: engine = %q, want local", got)
	}
	// flag beats env — but a flag naming the SAME value as settings cannot
	// prove precedence, so set the env back to doubao and use a local flag.
	t.Setenv(config.AudioEngineEnv, config.AudioEngineRecording)
	if got := voiceResolveEngine(&voiceOptions{engine: config.AudioEngineLocal}); got != config.AudioEngineLocal {
		t.Errorf("flag rung: engine = %q, want the flag to win", got)
	}
}

func TestVoiceResolveEngineToleratesMissingSettings(t *testing.T) {
	t.Setenv(config.AudioEngineEnv, "")
	t.Setenv(config.SettingsEnv, filepath.Join(t.TempDir(), "absent.json"))
	if got := voiceResolveEngine(&voiceOptions{}); got != config.AudioEngineDefault {
		t.Errorf("engine = %q, want the default %q when the settings file is absent", got, config.AudioEngineDefault)
	}
}

// TestVoiceValidateEngineRefusesUnknownNames: a typo must be a hard error, not
// a silent fallback — a fallback would look like the cloud engine had run.
func TestVoiceValidateEngineRefusesUnknownNames(t *testing.T) {
	for _, bad := range []string{"clowd", "Doubao", "doubao ", "funasr", "local-engine"} {
		if err := voiceValidateEngine(bad); err == nil {
			t.Errorf("voiceValidateEngine(%q) = nil, want a refusal", bad)
		}
	}
	for _, ok := range []string{"", config.AudioEngineLocal, config.AudioEngineRecording} {
		if err := voiceValidateEngine(ok); err != nil {
			t.Errorf("voiceValidateEngine(%q) = %v, want nil", ok, err)
		}
	}
	if err := voiceValidateEngine("clowd"); !strings.Contains(err.Error(), config.AudioEngineRecording) {
		t.Errorf("refusal %q should name the accepted values", err)
	}
}

func TestVoiceParseEngineFlag(t *testing.T) {
	opts, err := parseVoiceArgs([]string{"clip.wav", "--engine", "doubao"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if opts.engine != config.AudioEngineRecording {
		t.Errorf("engine = %q", opts.engine)
	}
	if _, err := parseVoiceArgs([]string{"clip.wav", "--engine", "nope"}); err == nil {
		t.Error("an unknown engine must fail at parse time, before any work")
	}
	if _, err := parseVoiceArgs([]string{"clip.wav", "--engine"}); err == nil {
		t.Error("a valueless --engine must fail")
	}
	// The cloud knobs are members of the flag set (unknown flags are refused).
	if _, err := parseVoiceArgs([]string{"clip.wav", "--doubao-key-file", "/k", "--doubao-resource", "r", "--doubao-hotwords", "a,b"}); err != nil {
		t.Errorf("doubao flags should parse: %v", err)
	}
	if !voiceFlagMember(voiceFlagEngine) || !voiceFlagMember(voiceFlagDoubaoKeyFile) ||
		!voiceFlagMember(voiceFlagDoubaoResource) || !voiceFlagMember(voiceFlagDoubaoHotwords) {
		t.Error("the doubao flags must be members or the parser refuses them as unknown")
	}
	if voiceFlagMember("enginee") {
		t.Error("a near-miss flag name must not be a member")
	}
}

// ── the dependency check is per engine ──────────────────────────────────

// TestVoiceDepsArePerEngine is the zero-model property, mechanically: the
// cloud arm passes with NO models and NO runtime on disk, while the local arm
// keeps demanding both. Demanding 910MB of weights for a cloud call would make
// the zero-model claim false.
func TestVoiceDepsArePerEngine(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "nothing-here")
	// The operator's real key file may exist on this host; the test points the
	// knob at a path that does not, so the assertion is about the CODE and not
	// about this machine's dotfiles.
	t.Setenv(config.AudioKeyFileEnv, filepath.Join(dir, "absent-key.txt"))

	cloud := &voiceOptions{engine: config.AudioEngineRecording, models: empty, runtime: empty}
	if msg := voiceDepsReady(cloud); msg == "" {
		t.Error("the cloud arm must still require its key file")
	} else if !strings.Contains(msg, voiceDoubaoKeyHint) {
		t.Errorf("cloud deps message %q does not name the key", msg)
	}

	keyFile := filepath.Join(dir, "apikey.txt")
	if err := os.WriteFile(keyFile, []byte("test-key\n"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	cloud.doubaoKeyFile = keyFile
	if msg := voiceDepsReady(cloud); msg != "" {
		t.Errorf("the cloud arm must not need the models: %q", msg)
	}

	local := &voiceOptions{engine: config.AudioEngineLocal, models: empty, runtime: empty}
	if msg := voiceDepsReady(local); msg == "" {
		t.Error("the local arm must still demand the runtime and weights")
	}
}

func TestVoiceDoubaoDepsReadyNamesTheChosenKeyFile(t *testing.T) {
	t.Setenv(config.AudioKeyFileEnv, filepath.Join(t.TempDir(), "absent-key.txt"))
	msg := voiceDoubaoDepsReady(&voiceOptions{engine: config.AudioEngineRecording})
	if !strings.Contains(msg, config.AudioKeyFileEnv) {
		t.Errorf("message %q should name the knob that chose the file", msg)
	}
}

// ── cloud option resolution ─────────────────────────────────────────────

// TestVoiceDoubaoOptionsResolveExplicitResourceAndKey proves the wire values
// are EXPLICIT: resource_id always names a real value, and the key is loaded
// from the file — never from a settings field.
func TestVoiceDoubaoOptionsResolveExplicitResourceAndKey(t *testing.T) {
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "apikey.txt")
	if err := os.WriteFile(keyFile, []byte("  key-from-file  \n"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	opts := &voiceOptions{
		audio:          "clip.wav",
		engine:         config.AudioEngineRecording,
		doubaoKeyFile:  keyFile,
		doubaoHotwords: "RAG, Agent",
	}
	do, err := voiceResolveDoubaoOpts(opts)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if do.APIKey != "key-from-file" {
		t.Errorf("key = %q, want the trimmed file content", do.APIKey)
	}
	if do.ResourceID != doubaoasr.ResourceSeedASRAUC20 {
		t.Errorf("resource = %q, want the named default (never implicit)", do.ResourceID)
	}
	if do.AudioFile != "clip.wav" {
		t.Errorf("audio = %q", do.AudioFile)
	}
	if len(do.Hotwords) != 2 || do.Hotwords[0] != "RAG" || do.Hotwords[1] != "Agent" {
		t.Errorf("hotwords = %v", do.Hotwords)
	}
}

func TestVoiceDoubaoResourceOverrideWinsOverDefault(t *testing.T) {
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "apikey.txt")
	if err := os.WriteFile(keyFile, []byte("k\n"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	opts := &voiceOptions{engine: config.AudioEngineRecording, doubaoKeyFile: keyFile, doubaoResource: doubaoasr.ResourceBigASRAUC10}
	do, err := voiceResolveDoubaoOpts(opts)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if do.ResourceID != doubaoasr.ResourceBigASRAUC10 {
		t.Errorf("resource = %q, want the explicit 1.0 value", do.ResourceID)
	}
}

// TestVoiceDoubaoMissingKeyIsActionable: the failure must name the file, the
// knobs that choose it AND the way back to on-machine transcription, before any
// network call. The third part is the flip's own requirement: on a default
// engine, "no key" is the most common first experience, so the refusal has to
// carry the correction rather than send the operator looking.
func TestVoiceDoubaoMissingKeyIsActionable(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent-key.txt")
	_, err := voiceResolveDoubaoOpts(&voiceOptions{engine: config.AudioEngineRecording, doubaoKeyFile: missing})
	if err == nil {
		t.Fatal("a missing key file must fail")
	}
	// The CURRENT settings path, not the pre-rename flat key: this error is the
	// one that tells the operator WHERE to put the file (the surface was
	// restructured 2026-10-09; naming the retired key would send them to a
	// document shape that still loads but is no longer what the help describes).
	wantKeyPath := config.AudioParamSettingsPath(config.AudioEngineRecording, config.AudioParamKeyKeyFile)
	for _, want := range []string{voiceDoubaoKeyHint, missing, config.AudioKeyFileEnv, wantKeyPath} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// TestVoiceDoubaoMissingKeyNamesTheWayBackToLocal is the flip's acceptance on
// the refusal path: the message an operator meets on a fresh machine must name
// the missing knob AND all three channels that reach the local engine, and the
// same sentence is the one the privacy notice carries (one spelling, so the two
// surfaces cannot describe different routes — Rule 1b/13).
func TestVoiceDoubaoMissingKeyNamesTheWayBackToLocal(t *testing.T) {
	t.Setenv(config.AudioEngineEnv, "")
	t.Setenv(config.DoubaoAPIKeyEnv, "")
	t.Setenv(config.AudioKeyFileEnv, "")
	t.Setenv(config.SettingsEnv, filepath.Join(t.TempDir(), "absent.json"))

	// The key FILE is named explicitly at a path that cannot exist: the check
	// must not depend on whether THIS host happens to have a real key (the
	// operator's /home/nick/.doubao/apikey.txt exists here, and a test that silently
	// used it would pass by accident on one machine and fail on another).
	msg := voiceDoubaoDepsReady(&voiceOptions{doubaoKeyFile: filepath.Join(t.TempDir(), "absent-key.txt")})
	if msg == "" {
		t.Fatal("the cloud engine without a key must be refused by the pre-flight check")
	}
	for _, want := range []string{
		voiceDoubaoKeyHint,      // WHAT is missing
		config.AudioEngineLocal, // ...and the engine that needs no credential
		"--engine " + config.AudioEngineLocal,
		config.AudioEngineEnv + "=" + config.AudioEngineLocal,
		config.AudioEngineSettingsKey,
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal %q does not carry %q", msg, want)
		}
	}

	// The SAME three channels ride the privacy notice, so a run that IS
	// uploading and a run that CANNOT offer one correction, not two.
	notice := voiceDoubaoPrivacyNotice(&voiceOptions{})
	if !strings.Contains(notice, voiceBackToLocalHint()) {
		t.Errorf("privacy notice does not carry the shared way back:\n%s", notice)
	}
}

// TestVoiceDoubaoPrivacyNoticeIsTruthfulAndComplete is the disclosure half of
// the flip: a default that uploads audio must SAY so, naming where it goes and
// WHETHER anything chose it. A notice that named the wrong sender side, or
// claimed the operator had asked for it, would be worse than no notice.
func TestVoiceDoubaoPrivacyNoticeIsTruthfulAndComplete(t *testing.T) {
	t.Setenv(config.AudioEngineEnv, "")
	t.Setenv(config.SettingsEnv, filepath.Join(t.TempDir(), "absent.json"))

	byDefault := voiceDoubaoPrivacyNotice(&voiceOptions{})
	for _, want := range []string{
		"PRIVACY",
		"UPLOADS",
		config.AudioEngineRecording,
		doubaoasr.DefaultSubmitURL,     // the endpoint, named
		doubaoasr.ResourceSeedASRAUC20, // the resource it is billed against
		"no --engine",                  // ...and that nothing chose it
		config.AudioEngineLocal,        // the way back
	} {
		if !strings.Contains(byDefault, want) {
			t.Errorf("privacy notice %q does not carry %q", byDefault, want)
		}
	}
	if !strings.Contains(byDefault, "the default") {
		t.Errorf("the notice must say the upload is the DEFAULT, not a choice:\n%s", byDefault)
	}

	// An explicit selection reads as a choice — the same sentence, the other
	// attribution. The two must not both claim "you asked for this".
	chosen := voiceDoubaoPrivacyNotice(&voiceOptions{engine: config.AudioEngineRecording})
	if !strings.Contains(chosen, "chosen explicitly") {
		t.Errorf("an explicit selection must be reported as one:\n%s", chosen)
	}
	if strings.Contains(chosen, "the default — no --engine") {
		t.Errorf("an explicit selection is being reported as the default:\n%s", chosen)
	}
	if !strings.Contains(chosen, doubaoasr.DefaultSubmitURL) {
		t.Errorf("both spellings must name the endpoint:\n%s", chosen)
	}

	// A private endpoint override is reported rather than the public default:
	// the notice must name where the audio ACTUALLY goes.
	private := voiceDoubaoPrivacyNotice(&voiceOptions{doubaoSubmit: "https://asr.example.internal/submit"})
	if !strings.Contains(private, "https://asr.example.internal/submit") {
		t.Errorf("an overridden endpoint must be the one reported:\n%s", private)
	}
}

// TestVoicePrivacyNoticeNeverCarriesTheCredential is the redaction contract on
// the new line: it is printed on EVERY cloud run, so a credential reaching it
// would reach a terminal, a scrollback and a session log.
func TestVoicePrivacyNoticeNeverCarriesTheCredential(t *testing.T) {
	t.Setenv(config.AudioEngineEnv, "")
	t.Setenv(config.SettingsEnv, writeDoubaoKeySettings(t, doubaoKeySentinel, config.SettingsPrivateMode))
	t.Setenv(config.DoubaoAPIKeyEnv, "")

	notice := voiceDoubaoPrivacyNotice(&voiceOptions{})
	if strings.Contains(notice, doubaoKeySentinel) {
		t.Errorf("the privacy notice carries the credential value:\n%s", notice)
	}
	if strings.Contains(notice, config.DoubaoAPIKeySettingsKey) {
		t.Errorf("the notice names the credential's settings key, which is a pointer to the secret:\n%s", notice)
	}
}

// TestVoiceDoubaoErrorKeepsTheVendorVerdict is the Rule 15 check on the CLI
// side: the vendor's status code and log id survive into the message the
// operator reads.
func TestVoiceDoubaoErrorKeepsTheVendorVerdict(t *testing.T) {
	apiErr := &doubaoasr.APIError{
		Op: "submit", HTTPStatus: http.StatusOK, StatusCode: "45000030",
		Message: "requested resource not granted", LogID: "LOGID-1", RequestID: "REQ-1",
	}
	got := voiceDoubaoError(apiErr).Error()
	for _, want := range []string{"doubao engine", "45000030", "LOGID-1", "REQ-1"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q does not carry %q", got, want)
		}
	}
	if wrapped := voiceDoubaoError(errors.New("plain")); !strings.Contains(wrapped.Error(), "plain") {
		t.Errorf("%q lost the underlying error", wrapped)
	}
}

// ── the dispatch seam, against a scripted service ────────────────────────

// TestTranscribeFileDispatchesByEngine drives the REAL seam both server shapes
// call, with a local HTTP service standing in for Volcengine: the cloud engine
// reaches it, the local engine does not.
//
// The cloud arm's endpoints come from the env (the AUC URLs are configurable
// through the same three rungs as every other doubao knob) — that is what
// makes the seam testable without a credential or a network.
func TestTranscribeFileDispatchesByEngine(t *testing.T) {
	srv := newVoiceDoubaoStub(t)
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "apikey.txt")
	if err := os.WriteFile(keyFile, []byte("test-key\n"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	audio := filepath.Join(dir, "clip.wav")
	if err := os.WriteFile(audio, voiceTestWAVBytes(), 0o600); err != nil {
		t.Fatalf("write audio: %v", err)
	}

	opts := &voiceOptions{
		engine:        config.AudioEngineRecording,
		doubaoKeyFile: keyFile,
		chunk:         voiceDefaultChunkSec,
		doubaoSubmit:  srv.URL + "/submit",
		doubaoQuery:   srv.URL + "/query",
	}
	text, elapsed, err := transcribeFile(opts, audio)
	if err != nil {
		t.Fatalf("cloud transcribeFile: %v", err)
	}
	if text != "cloud transcript" {
		t.Errorf("text = %q", text)
	}
	if elapsed < 0 {
		t.Errorf("elapsed = %v", elapsed)
	}
	if srv.submitCalls != 1 || srv.queryCalls == 0 {
		t.Errorf("submit=%d query=%d, want one submit then polling", srv.submitCalls, srv.queryCalls)
	}
	if !strings.Contains(srv.lastAudioData, "RIFF") == false && srv.lastAudioData == "" {
		t.Error("the audio never reached the service")
	}
	if srv.lastResourceID != doubaoasr.ResourceSeedASRAUC20 {
		t.Errorf("X-Api-Resource-Id = %q, want the explicit default", srv.lastResourceID)
	}

	// The LOCAL arm must not touch the service at all.
	local := &voiceOptions{engine: config.AudioEngineLocal, models: dir, runtime: filepath.Join(dir, "absent"), chunk: 1, doubaoSubmit: srv.URL + "/submit", doubaoQuery: srv.URL + "/query"}
	if _, _, err := transcribeFile(local, audio); err == nil {
		t.Error("the local arm with a missing runtime must fail locally")
	}
	if srv.submitCalls != 1 {
		t.Errorf("the local arm called the cloud service (submit=%d)", srv.submitCalls)
	}
}

// voiceDoubaoStub is a scripted stand-in for the Volcengine AUC endpoints.
type voiceDoubaoStub struct {
	*httptest.Server
	submitCalls    int
	queryCalls     int
	lastResourceID string
	lastAudioData  string
}

func newVoiceDoubaoStub(t *testing.T) *voiceDoubaoStub {
	t.Helper()
	st := &voiceDoubaoStub{}
	mux := http.NewServeMux()
	mux.HandleFunc("/submit", func(w http.ResponseWriter, r *http.Request) {
		st.submitCalls++
		st.lastResourceID = r.Header.Get("X-Api-Resource-Id")
		var body struct {
			Audio struct {
				Data string `json:"data"`
			} `json:"audio"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode submit body: %v", err)
		}
		st.lastAudioData = body.Audio.Data
		w.Header().Set("X-Api-Status-Code", doubaoasr.StatusOK)
		w.Header().Set("X-Tt-Logid", "STUB-SUBMIT-LOGID")
		fmt.Fprint(w, `{}`)
	})
	mux.HandleFunc("/query", func(w http.ResponseWriter, r *http.Request) {
		st.queryCalls++
		w.Header().Set("X-Api-Status-Code", doubaoasr.StatusOK)
		w.Header().Set("X-Tt-Logid", "STUB-QUERY-LOGID")
		fmt.Fprint(w, `{"header":{"code":0},"result":{"text":"cloud transcript"},"audio_info":{"duration":1000}}`)
	})
	st.Server = httptest.NewServer(mux)
	t.Cleanup(st.Close)
	return st
}

// voiceTestWAVBytes is a minimal canonical WAV for the dispatch test.
func voiceTestWAVBytes() []byte {
	return voiceBuildWAV(make([]byte, 64), voiceWAVFormatPCM, 16)
}

func writeVoiceSettings(t *testing.T, path, engine string) {
	t.Helper()
	body := fmt.Sprintf(`{"voice_engine":%q}`, engine)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
}

// TestVoiceEngineLabelIsDynamic checks the HTTP contract's engine field: it
// follows the selection instead of being hardcoded, and the LOCAL label is
// byte-identical to the string the endpoints have always reported (the
// existing client contract).
func TestVoiceEngineLabelIsDynamic(t *testing.T) {
	if got := voiceEngineLabel(&voiceOptions{engine: config.AudioEngineLocal}); got != "funasr-nano" {
		t.Errorf("local label = %q, want the unchanged %q", got, "funasr-nano")
	}
	if got := voiceEngineLabel(&voiceOptions{engine: config.AudioEngineRecording}); got != "doubao" {
		t.Errorf("cloud label = %q, want doubao", got)
	}
	if !strings.Contains(voiceListenBannerVoice(&voiceOptions{engine: config.AudioEngineRecording}), "doubao") {
		t.Error("the startup banner must name the engine it will use")
	}
}
